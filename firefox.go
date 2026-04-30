package lorca

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var defaultFirefoxArgs = []string{
	"--no-remote",
	"--new-instance",
}

// bidiValueToJSON converts a WebDriver BiDi serialized value to plain JSON.
// BiDi wraps every value in a {"type":"...", "value":...} envelope; this
// function unwraps it recursively so callers get standard json.RawMessage.
func bidiValueToJSON(v json.RawMessage) (json.RawMessage, error) {
	var wrapper struct {
		Type  string          `json:"type"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(v, &wrapper); err != nil {
		return nil, err
	}
	switch wrapper.Type {
	case "string", "number", "boolean":
		return wrapper.Value, nil
	case "null", "undefined":
		return json.RawMessage("null"), nil
	case "array":
		var items []json.RawMessage
		if err := json.Unmarshal(wrapper.Value, &items); err != nil {
			return nil, err
		}
		converted := make([]json.RawMessage, len(items))
		for i, item := range items {
			c, err := bidiValueToJSON(item)
			if err != nil {
				return nil, err
			}
			converted[i] = c
		}
		b, err := json.Marshal(converted)
		return json.RawMessage(b), err
	case "object":
		// BiDi object value is [[keyString, bidiValue], ...]
		var pairs [][2]json.RawMessage
		if err := json.Unmarshal(wrapper.Value, &pairs); err != nil {
			return nil, err
		}
		obj := make(map[string]json.RawMessage, len(pairs))
		for _, pair := range pairs {
			var key string
			if err := json.Unmarshal(pair[0], &key); err != nil {
				return nil, err
			}
			val, err := bidiValueToJSON(pair[1])
			if err != nil {
				return nil, err
			}
			obj[key] = val
		}
		b, err := json.Marshal(obj)
		return json.RawMessage(b), err
	default:
		return json.RawMessage("null"), nil
	}
}

// bidiConn is a minimal WebSocket client for the Firefox BiDi protocol.
// We avoid golang.org/x/net/websocket because it always sends an Origin header
// that Firefox's /session endpoint rejects with 400 Bad Request.
type bidiConn struct {
	conn    net.Conn
	br      *bufio.Reader
	writeMu sync.Mutex
}

func newBidiConn(wsLoc *url.URL) (*bidiConn, error) {
	addr := wsLoc.Host
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, err
	}
	bc := &bidiConn{conn: conn, br: bufio.NewReader(conn)}
	if err := bc.handshake(wsLoc.Host, wsLoc.Path); err != nil {
		conn.Close()
		return nil, err
	}
	return bc, nil
}

func (c *bidiConn) handshake(host, path string) error {
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		return err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)

	req := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Sec-WebSocket-Protocol: webdriver-bidi\r\n" +
		"\r\n"

	c.conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := c.conn.Write([]byte(req)); err != nil {
		return err
	}

	// Read status line.
	statusLine, err := c.br.ReadString('\n')
	if err != nil {
		return fmt.Errorf("firefox: BiDi handshake read: %w", err)
	}
	if !strings.HasPrefix(statusLine, "HTTP/1.1 101") {
		return fmt.Errorf("firefox: BiDi handshake: %s", strings.TrimSpace(statusLine))
	}
	// Drain the rest of the HTTP headers.
	for {
		line, err := c.br.ReadString('\n')
		if err != nil {
			return fmt.Errorf("firefox: BiDi handshake drain: %w", err)
		}
		if line == "\r\n" {
			break
		}
	}
	c.conn.SetDeadline(time.Time{})
	return nil
}

// writeFrame sends a masked WebSocket frame (RFC 6455 §5.3: all client frames must be masked).
func (c *bidiConn) writeFrame(opcode byte, payload []byte) error {
	plen := len(payload)
	var header []byte
	header = append(header, 0x80|opcode) // FIN=1, RSV=0

	var maskKey [4]byte
	rand.Read(maskKey[:])

	switch {
	case plen <= 125:
		header = append(header, byte(0x80|plen))
	case plen <= 65535:
		header = append(header, 0xFE, byte(plen>>8), byte(plen))
	default:
		header = append(header, 0xFF,
			0, 0, 0, 0,
			byte(plen>>24), byte(plen>>16), byte(plen>>8), byte(plen))
	}
	header = append(header, maskKey[:]...)

	masked := make([]byte, plen)
	for i, b := range payload {
		masked[i] = b ^ maskKey[i%4]
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err := c.conn.Write(append(header, masked...))
	return err
}

// Send marshals v to JSON and sends it as a WebSocket text frame.
func (c *bidiConn) Send(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.writeFrame(0x1, data)
}

// Receive reads the next WebSocket text/binary frame and unmarshals its
// payload into v. Control frames (ping/pong/close) are handled inline.
func (c *bidiConn) Receive(v interface{}) error {
	for {
		hdr := make([]byte, 2)
		if _, err := io.ReadFull(c.br, hdr); err != nil {
			return err
		}

		opcode := hdr[0] & 0x0F
		isMasked := hdr[1]&0x80 != 0
		plen := int(hdr[1] & 0x7F)

		switch plen {
		case 126:
			ext := make([]byte, 2)
			if _, err := io.ReadFull(c.br, ext); err != nil {
				return err
			}
			plen = int(ext[0])<<8 | int(ext[1])
		case 127:
			ext := make([]byte, 8)
			if _, err := io.ReadFull(c.br, ext); err != nil {
				return err
			}
			// Lower 32 bits suffice; payloads > 4 GB are not expected.
			plen = int(ext[4])<<24 | int(ext[5])<<16 | int(ext[6])<<8 | int(ext[7])
		}

		var maskKey [4]byte
		if isMasked {
			if _, err := io.ReadFull(c.br, maskKey[:]); err != nil {
				return err
			}
		}

		payload := make([]byte, plen)
		if _, err := io.ReadFull(c.br, payload); err != nil {
			return err
		}
		if isMasked {
			for i := range payload {
				payload[i] ^= maskKey[i%4]
			}
		}

		switch opcode {
		case 0x8: // close
			return io.EOF
		case 0x9: // ping -reply with pong
			c.writeFrame(0xA, payload)
			continue
		case 0xA: // pong -ignore
			continue
		case 0x0, 0x1, 0x2: // continuation, text, binary
			return json.Unmarshal(payload, v)
		default:
			continue // unknown opcode; skip
		}
	}
}

// Close closes the underlying TCP connection.
func (c *bidiConn) Close() error {
	return c.conn.Close()
}

type firefox struct {
	sync.Mutex
	cmd            *exec.Cmd
	bidi           *bidiConn
	id             int32
	context        string // WebDriver BiDi browsing context ID
	pending        map[int]chan result
	doneC          chan struct{}
	doneOnce       sync.Once
	watchdogDoneC  chan struct{}
	watchdogOnce   sync.Once
	lastBounds     Bounds
	debugPort      int
	loadScripts    []string // scripts re-eval'd in page realm on every browsingContext.load
	appURL         string   // URL set by load(); used to redirect back-navigation
	blockBackNav   bool     // when true, navigations away from appURL are redirected back
}

// closeDone closes doneC exactly once, signalling that the real Firefox process is gone.
// Called by readLoop so Done() reflects BiDi connection drop, not the launcher stub exit.
func (f *firefox) closeDone() {
	f.doneOnce.Do(func() { close(f.doneC) })
}

func newFirefoxWithArgs(binary string, iconPath string, args ...string) (*firefox, error) {
	f := &firefox{
		id:      1, // 0 used for session.new, 1 for getTree during init; send() increments before use
		pending: map[int]chan result{},
	}

	debugPort, err := getFreePort()
	if err != nil {
		return nil, err
	}
	f.debugPort = debugPort

	args = append(args, fmt.Sprintf("--remote-debugging-port=%d", debugPort))
	f.cmd = exec.Command(binary, args...)
	if err := f.cmd.Start(); err != nil {
		return nil, err
	}

	// Poll /json/version until Firefox is ready.
	startTime := time.Now()
	var res *http.Response
	for {
		res, err = http.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", debugPort))
		if err == nil {
			break
		}
		if time.Since(startTime) > 5*time.Second {
			killProcessTree(f.cmd.Process.Pid)
			return nil, fmt.Errorf("firefox: failed to reach /json/version within 5 seconds: %w", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		killProcessTree(f.cmd.Process.Pid)
		return nil, err
	}

	// Firefox returns HTML (not JSON) at /json/version; ignore parse errors.
	// Use WebSocketDebuggerUrl if present, otherwise fall back to /session.
	var ver browserVersion
	_ = json.Unmarshal(body, &ver)

	var wsLoc *url.URL
	if ver.WebSocketDebuggerUrl != "" {
		wsLoc, err = url.Parse(ver.WebSocketDebuggerUrl)
		if err != nil {
			killProcessTree(f.cmd.Process.Pid)
			return nil, err
		}
	} else {
		wsLoc = &url.URL{
			Scheme: "ws",
			Host:   fmt.Sprintf("127.0.0.1:%d", debugPort),
			Path:   "/session",
		}
	}

	f.bidi, err = newBidiConn(wsLoc)
	if err != nil {
		killProcessTree(f.cmd.Process.Pid)
		return nil, err
	}

	// Activate WebDriver BiDi session.
	if err := f.bidi.Send(h{"id": 0, "method": "session.new", "params": h{"capabilities": h{}}}); err != nil {
		f.bidi.Close()
		killProcessTree(f.cmd.Process.Pid)
		return nil, err
	}
	for {
		var m struct {
			ID int `json:"id"`
		}
		if err := f.bidi.Receive(&m); err != nil {
			f.bidi.Close()
			killProcessTree(f.cmd.Process.Pid)
			return nil, fmt.Errorf("firefox: waiting for session.new response: %w", err)
		}
		if m.ID == 0 {
			break
		}
	}

	// Get the initial browsing context ID.
	if err := f.bidi.Send(h{"id": 1, "method": "browsingContext.getTree", "params": h{}}); err != nil {
		f.bidi.Close()
		killProcessTree(f.cmd.Process.Pid)
		return nil, err
	}
	for {
		var m struct {
			ID     int `json:"id"`
			Result struct {
				Contexts []struct {
					Context string `json:"context"`
				} `json:"contexts"`
			} `json:"result"`
		}
		if err := f.bidi.Receive(&m); err != nil {
			f.bidi.Close()
			killProcessTree(f.cmd.Process.Pid)
			return nil, fmt.Errorf("firefox: waiting for browsingContext.getTree response: %w", err)
		}
		if m.ID == 1 {
			if len(m.Result.Contexts) == 0 {
				f.bidi.Close()
				killProcessTree(f.cmd.Process.Pid)
				return nil, errors.New("firefox: no browsing contexts found")
			}
			f.context = m.Result.Contexts[0].Context
			break
		}
	}

	f.doneC = make(chan struct{})
	f.watchdogDoneC = make(chan struct{})
	go func() {
		// On Windows, firefox.exe is a launcher stub that exits immediately.
		// doneC is closed by readLoop (BiDi drop), not here.
		err := f.cmd.Wait()
		log.Printf("lorca/firefox: launcher process exited err=%v state=%v", err, f.cmd.ProcessState)
	}()
	go f.readLoop()
	go f.contextWatchdog()

	// Subscribe to events needed for tab management, script injection, and nav blocking.
	if _, err := f.send("session.subscribe", h{"events": []string{
		"log.entryAdded",
		"browsingContext.navigationStarted",
		"browsingContext.load",
		"browsingContext.contextCreated",
		"browsingContext.contextDestroyed",
		"script.realmCreated",
	}}); err != nil {
		log.Printf("lorca/firefox: session.subscribe failed: %v", err)
	}

	// Apply the host executable's icon to Firefox's window on platforms that
	// support it.  Runs in a background goroutine to avoid blocking startup.
	go applyFirefoxWindowIcon(f.cmd.Process.Pid, iconPath)

	return f, nil
}

func (f *firefox) send(method string, params h) (json.RawMessage, error) {
	id := int(atomic.AddInt32(&f.id, 1))
	resc := make(chan result, 1)
	f.Lock()
	f.pending[id] = resc
	f.Unlock()

	err := f.bidi.Send(h{"id": id, "method": method, "params": params})
	if err != nil {
		f.Lock()
		delete(f.pending, id)
		f.Unlock()
		return nil, err
	}
	res := <-resc
	return res.Value, res.Err
}

// sendNoWait sends a BiDi command without waiting for a response (response is discarded).
// Safe to call from within readLoop because bidiConn.Send uses a separate write mutex.
func (f *firefox) sendNoWait(method string, params h) {
	id := int(atomic.AddInt32(&f.id, 1))
	// Intentionally no f.pending entry -response is dropped.
	_ = f.bidi.Send(h{"id": id, "method": method, "params": params})
}

// bidiMsg is every message Firefox sends over BiDi. Error responses use a top-level
// "error" string code and a separate "message" string (not a nested object).
type bidiMsg struct {
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   string          `json:"error"`   // BiDi error code string, e.g. "unknown error"
	Message string          `json:"message"` // BiDi error message string
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func (f *firefox) readLoop() {
	defer f.closeDone()
	for {
		var m bidiMsg
		if err := f.bidi.Receive(&m); err != nil {
			log.Printf("lorca/firefox: readLoop exiting: %v", err)
			return
		}
		if m.Method != "" {
			switch m.Method {
			case "browsingContext.contextCreated":
				// lorca is single-page: close any new top-level context immediately.
				// Two-pronged: sendNoWait for speed, then getTree in a goroutine in
				// case Firefox replaced the context ID before our close arrived.
				var ctxParams struct {
					Context string `json:"context"`
					Parent  string `json:"parent"`
				}
				if err := json.Unmarshal(m.Params, &ctxParams); err == nil {
					f.Lock()
					mainCtx := f.context
					f.Unlock()
					if ctxParams.Parent == "" && ctxParams.Context != mainCtx {
						// contextCreated is firing reliably — the watchdog is no longer needed.
						f.stopWatchdog()
						f.sendNoWait("browsingContext.close", h{
							"context":      ctxParams.Context,
							"promptUnload": false,
						})
						go func(main string) {
							f.closeStrayContexts(main)
							// Re-activate main context; without this Firefox may leave the content area blank.
							if _, err := f.send("browsingContext.activate", h{"context": main}); err != nil {
								log.Printf("lorca/firefox: activate main context: %v (falling back to window.focus)", err)
								f.sendNoWait("script.evaluate", h{
									"expression":      "window.focus(); void 0",
									"awaitPromise":    false,
									"target":          h{"context": main},
									"resultOwnership": "none",
								})
							}
						}(mainCtx)
					}
				}
			case "browsingContext.contextDestroyed":
				params := struct {
					Context string `json:"context"`
				}{}
				json.Unmarshal(m.Params, &params)
				if params.Context == f.context {
					log.Printf("lorca/firefox: main context destroyed - killing")
					f.kill()
					return
				}
				// Stray context destroyed; re-activate main so it gets focus.
				go func() {
					f.Lock()
					main := f.context
					f.Unlock()
					if _, err := f.send("browsingContext.activate", h{"context": main}); err != nil {
						f.sendNoWait("script.evaluate", h{
							"expression":      "window.focus(); void 0",
							"awaitPromise":    false,
							"target":          h{"context": main},
							"resultOwnership": "none",
						})
					}
				}()
			case "log.entryAdded":
				// console output from the page - silently ignored
			case "browsingContext.navigationStarted":
				var navParams struct {
					Context string `json:"context"`
					URL     string `json:"url"`
				}
				if err := json.Unmarshal(m.Params, &navParams); err == nil {
					f.Lock()
					appURL := f.appURL
					blockBackNav := f.blockBackNav
					f.Unlock()
					// If back-nav blocking is enabled and a navigation away from the
					// app URL is detected (e.g. the user pressed Back), redirect back.
					if blockBackNav && appURL != "" && navParams.Context == f.context && !strings.HasPrefix(navParams.URL, appURL) {
						go func(redirectURL string) {
							if _, err := f.send("browsingContext.navigate", h{
								"url":     redirectURL,
								"context": f.context,
								"wait":    "none",
							}); err != nil {
								log.Printf("lorca/firefox: redirect to appURL failed: %v", err)
							}
						}(appURL)
					}
				}
			case "browsingContext.load":
				// Re-eval all loadScripts in the page realm as a single call (belt-and-suspenders
				// after realmCreated). Goroutine required: f.eval blocks on readLoop response.
				var loadParams struct {
					Context string `json:"context"`
				}
				json.Unmarshal(m.Params, &loadParams)
				if loadParams.Context == f.context {
					f.Lock()
					scripts := append([]string(nil), f.loadScripts...)
					f.Unlock()
					if len(scripts) > 0 {
						go func(scripts []string) {
							if _, err := f.eval(strings.Join(scripts, ";\n")); err != nil {
								log.Printf("lorca/firefox: post-load eval error: %v", err)
							}
						}(scripts)
					}
				}
			case "script.realmCreated":
				// When the real-origin window realm is created, immediately fire all
				// loadScripts via sendNoWait to win the race against page scripts.
				// Only targets the page realm (sandbox=="" guard protects against future
				// addPreloadScript use). browsingContext.load is the unconditional fallback.
				var realmParams struct {
					Realm   string `json:"realm"`
					Origin  string `json:"origin"`
					Context string `json:"context"`
					Type    string `json:"type"`
					Sandbox string `json:"sandbox"`
				}
				if err := json.Unmarshal(m.Params, &realmParams); err == nil &&
					realmParams.Sandbox == "" &&
					realmParams.Context == f.context &&
					realmParams.Type == "window" &&
					realmParams.Origin != "" && realmParams.Origin != "null" {
					f.Lock()
					scripts := append([]string(nil), f.loadScripts...)
					f.Unlock()
					if len(scripts) > 0 {
						f.sendNoWait("script.evaluate", h{
							"expression":      strings.Join(scripts, ";\n"),
							"awaitPromise":    false,
							"target":          h{"context": f.context},
							"resultOwnership": "none",
						})
					}
				}
			}
			continue
		}
		// Response -route to pending channel.
		f.Lock()
		ch, ok := f.pending[m.ID]
		delete(f.pending, m.ID)
		f.Unlock()
		if !ok {
			continue
		}
		if m.Error != "" {
			msg := m.Message
			if msg == "" {
				msg = m.Error
			}
			log.Printf("lorca/firefox: BiDi error id=%d error=%q message=%q", m.ID, m.Error, m.Message)
			ch <- result{Err: errors.New(msg)}
		} else {
			ch <- result{Value: m.Result}
		}
	}
}

func (f *firefox) eval(expr string) (json.RawMessage, error) {
	raw, err := f.send("script.evaluate", h{
		"expression":      expr,
		"awaitPromise":    true,
		"target":          h{"context": f.context},
		"resultOwnership": "root",
	})
	if err != nil {
		return nil, err
	}

	var evalResult struct {
		Type             string          `json:"type"`
		Result           json.RawMessage `json:"result"`
		ExceptionDetails struct {
			Text      string `json:"text"`
			Exception struct {
				Type        string          `json:"type"`
				Value       json.RawMessage `json:"value"`
				Description string          `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(raw, &evalResult); err != nil {
		return nil, err
	}
	if evalResult.Type == "exception" {
		ex := evalResult.ExceptionDetails.Exception
		if len(ex.Value) > 0 {
			if unwrapped, err2 := bidiValueToJSON(ex.Value); err2 == nil {
				return nil, errors.New(string(unwrapped))
			}
			return nil, errors.New(string(ex.Value))
		}
		if ex.Description != "" {
			return nil, errors.New(ex.Description)
		}
		return nil, errors.New(evalResult.ExceptionDetails.Text)
	}
	if len(evalResult.Result) == 0 {
		return json.RawMessage("null"), nil
	}
	return bidiValueToJSON(evalResult.Result)
}

func (f *firefox) load(url string) error {
	_, err := f.send("browsingContext.navigate", h{
		"url":     url,
		"context": f.context,
		"wait":    "none",
	})
	if err == nil {
		f.Lock()
		f.appURL = url
		f.Unlock()
	}
	return err
}

func (f *firefox) setBlockBackNavigation(enable bool) {
	f.Lock()
	f.blockBackNav = enable
	f.Unlock()
}

// stopWatchdog signals the contextWatchdog goroutine to exit (idempotent via sync.Once).
func (f *firefox) stopWatchdog() {
	f.watchdogOnce.Do(func() { close(f.watchdogDoneC) })
}

// contextWatchdog polls getTree to close stray contexts. It is a fallback for a
// Firefox BiDi bug where contextCreated is not fired for the first Ctrl+T/Ctrl+N tab.
// Once a stray is found, contextCreated becomes reliable and the watchdog stops itself.
func (f *firefox) contextWatchdog() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-f.doneC:
			return
		case <-f.watchdogDoneC:
			return
		case <-ticker.C:
			f.Lock()
			main := f.context
			f.Unlock()
			if f.closeStrayContexts(main) {
				// A stray was found and closed; contextCreated is now reliable.
				f.stopWatchdog()
				return
			}
		}
	}
}

// closeStrayContexts closes every top-level context that is not mainCtx, returning
// true if at least one was closed. Uses getTree to avoid "no such frame" races.
func (f *firefox) closeStrayContexts(mainCtx string) bool {
	raw, err := f.send("browsingContext.getTree", h{})
	if err != nil {
		return false
	}
	var tree struct {
		Contexts []struct {
			Context string `json:"context"`
		} `json:"contexts"`
	}
	if err := json.Unmarshal(raw, &tree); err != nil {
		return false
	}
	closed := false
	for _, ctx := range tree.Contexts {
		if ctx.Context == mainCtx {
			continue
		}
		if _, err := f.send("browsingContext.close", h{
			"context":      ctx.Context,
			"promptUnload": false,
		}); err != nil {
			log.Printf("lorca/firefox: closeStrayContexts: close %s failed: %v", ctx.Context, err)
		} else {
			closed = true
		}
	}
	return closed
}

func (f *firefox) setAppUserModelID(id string) {
	go applyFirefoxWindowAUMID(f.cmd.Process.Pid, id)
}

func (f *firefox) injectScript(js string) error {
	// Scripts must run in the page window realm, not a BiDi preload sandbox realm.
	// Preload sandbox objects trigger Firefox Xray "Permission denied to access property
	// 'length'" when Vue's reactivity system introspects them. Store in loadScripts
	// instead; realmCreated and browsingContext.load re-eval it in the page realm.
	f.Lock()
	// Prepend so bootstrap runs before binding scripts that depend on window.__lorcaWS.
	f.loadScripts = append([]string{js}, f.loadScripts...)
	f.Unlock()
	_, err := f.eval(js)
	if err != nil {
		log.Printf("lorca/firefox injectScript eval error: %v", err)
	}
	return err
}

func (f *firefox) injectBinding(name string) error {
	code := bindingScript(name)

	// Same page-realm constraint as injectScript: store in loadScripts so
	// realmCreated and browsingContext.load install it on every navigation.
	f.Lock()
	f.loadScripts = append(f.loadScripts, code)
	f.Unlock()

	// Fire-and-forget eval on current page so the binding is available immediately.
	// Non-blocking (sendNoWait) because 50+ bindings at startup would add >10s of lag.
	f.sendNoWait("script.evaluate", h{
		"expression":      code,
		"awaitPromise":    false,
		"target":          h{"context": f.context},
		"resultOwnership": "none",
	})
	return nil
}

func (f *firefox) setBounds(b Bounds) error {
	if b.Left != 0 {
		log.Printf("lorca/firefox: SetBounds Left=%d not supported", b.Left)
	}
	if b.Top != 0 {
		log.Printf("lorca/firefox: SetBounds Top=%d not supported", b.Top)
	}
	if b.WindowState != "" && b.WindowState != WindowStateNormal {
		log.Printf("lorca/firefox: SetBounds WindowState=%q not supported", b.WindowState)
	}
	if b.Width > 0 || b.Height > 0 {
		// window.resizeTo sets the outer window dimensions without locking the
		// viewport (unlike browsingContext.setViewport which prevents resizing).
		if _, err := f.eval(fmt.Sprintf("window.resizeTo(%d,%d)", b.Width, b.Height)); err != nil {
			return err
		}
		f.Lock()
		f.lastBounds.Width = b.Width
		f.lastBounds.Height = b.Height
		f.Unlock()
	}
	return nil
}

func (f *firefox) bounds() (Bounds, error) {
	raw, err := f.eval("[window.innerWidth, window.innerHeight]")
	if err != nil {
		return Bounds{}, err
	}
	var dims [2]int
	if err := json.Unmarshal(raw, &dims); err != nil {
		return Bounds{}, err
	}
	return Bounds{Width: dims[0], Height: dims[1]}, nil
}

func (f *firefox) kill() {
	log.Printf("lorca/firefox: kill() called")
	if f.bidi != nil {
		f.sendNoWait("browser.close", h{})
		time.Sleep(150 * time.Millisecond) // brief window for graceful exit
		f.bidi.Close()
	}
	f.Lock()
	for _, ch := range f.pending {
		ch <- result{Err: errors.New("firefox closed")}
	}
	f.pending = map[int]chan result{}
	f.Unlock()

	killFirefoxProcessTree(f.cmd.Process.Pid, f.cmd.ProcessState)
}

func (f *firefox) done() <-chan struct{} { return f.doneC }
