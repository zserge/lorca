package lorca

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/websocket"
)

type h = map[string]any

// Result is a struct for the resulting value of the JS expression or an error.
type result struct {
	Value json.RawMessage
	Err   error
}

type bindingFunc func(args []json.RawMessage) (any, error)

// Msg is a struct for incoming messages (results and async events)
type msg struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type chrome struct {
	sync.Mutex
	wsMu         sync.Mutex // serializes websocket writes
	cmd          *exec.Cmd
	ws           *websocket.Conn
	id           int32
	target       string
	session      string
	window       int
	pending      map[int]chan result
	debugPort    int
	doneC        chan struct{}
	appURL       string // URL set by load(); used to redirect back-navigation
	blockBackNav bool   // when true, navigations away from appURL are redirected back
}

type browserVersion struct {
	Browser              string `json:"Browser"`
	ProtocolVersion      string `json:"Protocol-Version"`
	UserAgent            string `json:"User-Agent"`
	V8Version            string `json:"V8-Version"`
	WebkitVersion        string `json:"Webkit-Version"`
	WebSocketDebuggerUrl string `json:"webSocketDebuggerUrl"`
}

func getFreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func newChromeWithArgs(chromeBinary string, args ...string) (*chrome, error) {
	// The first two IDs are used internally during the initialization
	c := &chrome{
		id:      2,
		pending: map[int]chan result{},
	}

	debugPort, err := getFreePort()
	if err != nil {
		return nil, err
	}

	// Start chrome process
	args = append(args, fmt.Sprintf("--remote-debugging-port=%d", debugPort))
	c.cmd = exec.Command(chromeBinary, args...)
	if err := c.cmd.Start(); err != nil {
		return nil, err
	}

	// Retry mechanism
	startTime := time.Now()
	var res *http.Response
	for {
		res, err = http.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", debugPort))
		if err == nil {
			break
		}
		if time.Since(startTime) > 5*time.Second {
			return nil, fmt.Errorf("failed to reach /json/version within 5 seconds: %w", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	c.debugPort = debugPort

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	browserVer := &browserVersion{}

	err = json.Unmarshal(body, &browserVer)
	if err != nil {
		return nil, err
	}

	wsURL := browserVer.WebSocketDebuggerUrl

	// Open a websocket
	c.ws, err = websocket.Dial(wsURL, "", "http://127.0.0.1")
	if err != nil {
		c.kill()
		return nil, err
	}

	// Find target and initialize session
	c.target, err = c.findTarget()
	if err != nil {
		c.kill()
		return nil, err
	}

	c.session, err = c.startSession(c.target)
	if err != nil {
		c.kill()
		return nil, err
	}
	c.doneC = make(chan struct{})
	go func() { c.cmd.Wait(); close(c.doneC) }()
	go c.readLoop()

	for method, args := range map[string]h{
		"Page.enable":          nil,
		"Target.setAutoAttach": {"autoAttach": true, "waitForDebuggerOnStart": false},
		"Network.enable":       nil,
		"Runtime.enable":       nil,
		"Security.enable":      nil,
		"Performance.enable":   nil,
		"Log.enable":           nil,
	} {
		if _, err := c.send(method, args); err != nil {
			c.kill()
			return nil, err
		}
	}

	if !contains(args, "--headless") {
		win, err := c.getWindowForTarget(c.target)
		if err != nil {
			c.kill()
			return nil, err
		}
		c.window = win.WindowID
	}

	return c, nil
}

func (c *chrome) findTarget() (string, error) {
	err := websocket.JSON.Send(c.ws, h{
		"id": 0, "method": "Target.setDiscoverTargets", "params": h{"discover": true},
	})
	if err != nil {
		return "", err
	}
	for {
		m := msg{}
		if err = websocket.JSON.Receive(c.ws, &m); err != nil {
			return "", err
		} else if m.Method == "Target.targetCreated" {
			target := struct {
				TargetInfo struct {
					Type string `json:"type"`
					ID   string `json:"targetId"`
				} `json:"targetInfo"`
			}{}
			if err := json.Unmarshal(m.Params, &target); err != nil {
				return "", err
			} else if target.TargetInfo.Type == "page" {
				return target.TargetInfo.ID, nil
			}
		}
	}
}

func (c *chrome) startSession(target string) (string, error) {
	err := websocket.JSON.Send(c.ws, h{
		"id": 1, "method": "Target.attachToTarget", "params": h{"targetId": target},
	})
	if err != nil {
		return "", err
	}
	for {
		m := msg{}
		if err = websocket.JSON.Receive(c.ws, &m); err != nil {
			return "", err
		} else if m.ID == 1 {
			if m.Error != nil {
				return "", errors.New("Target error: " + string(m.Error))
			}
			session := struct {
				ID string `json:"sessionId"`
			}{}
			if err := json.Unmarshal(m.Result, &session); err != nil {
				return "", err
			}
			return session.ID, nil
		}
	}
}

// WindowState defines the state of the Chrome window, possible values are
// "normal", "maximized", "minimized" and "fullscreen".
type WindowState string

const (
	// WindowStateNormal defines a normal state of the browser window
	WindowStateNormal WindowState = "normal"
	// WindowStateMaximized defines a maximized state of the browser window
	WindowStateMaximized WindowState = "maximized"
	// WindowStateMinimized defines a minimized state of the browser window
	WindowStateMinimized WindowState = "minimized"
	// WindowStateFullscreen defines a fullscreen state of the browser window
	WindowStateFullscreen WindowState = "fullscreen"
)

// Bounds defines settable window properties.
type Bounds struct {
	Left        int         `json:"left"`
	Top         int         `json:"top"`
	Width       int         `json:"width"`
	Height      int         `json:"height"`
	WindowState WindowState `json:"windowState"`
}

type windowTargetMessage struct {
	WindowID int    `json:"windowId"`
	Bounds   Bounds `json:"bounds"`
}

func (c *chrome) getWindowForTarget(target string) (windowTargetMessage, error) {
	var m windowTargetMessage
	msg, err := c.send("Browser.getWindowForTarget", h{"targetId": target})
	if err != nil {
		return m, err
	}
	err = json.Unmarshal(msg, &m)
	return m, err
}

type targetMessageTemplate struct {
	ID     int    `json:"id"`
	Method string `json:"method"`
	Params struct {
		Name    string `json:"name"`
		Payload string `json:"payload"`
		ID      int    `json:"executionContextId"`
		Args    []struct {
			Type  string      `json:"type"`
			Value interface{} `json:"value"`
		} `json:"args"`
	} `json:"params"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
	Result json.RawMessage `json:"result"`
}

type targetMessage struct {
	targetMessageTemplate
	Result struct {
		Result struct {
			Type        string          `json:"type"`
			Subtype     string          `json:"subtype"`
			Description string          `json:"description"`
			Value       json.RawMessage `json:"value"`
			ObjectID    string          `json:"objectId"`
		} `json:"result"`
		Exception struct {
			Exception struct {
				Type        string          `json:"type"`
				Subtype     string          `json:"subtype"`
				Description string          `json:"description"`
				Value       json.RawMessage `json:"value"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	} `json:"result"`
}

func (c *chrome) readLoop() {
	for {
		m := msg{}
		if err := websocket.JSON.Receive(c.ws, &m); err != nil {
			return
		}

		if m.Method == "Target.receivedMessageFromTarget" {
			params := struct {
				SessionID string `json:"sessionId"`
				Message   string `json:"message"`
			}{}
			json.Unmarshal(m.Params, &params)
			if params.SessionID != c.session {
				continue
			}
			res := targetMessage{}
			json.Unmarshal([]byte(params.Message), &res)

			if res.ID == 0 && res.Method == "Runtime.consoleAPICalled" || res.Method == "Runtime.exceptionThrown" {
				log.Println(params.Message)
			}

			if res.Method == "Page.frameNavigated" {
				c.Lock()
				blockBackNav := c.blockBackNav
				appURL := c.appURL
				c.Unlock()
				if blockBackNav && appURL != "" {
					var navEvent struct {
						Params struct {
							Frame struct {
								URL string `json:"url"`
							} `json:"frame"`
						} `json:"params"`
					}
					if json.Unmarshal([]byte(params.Message), &navEvent) == nil {
						navURL := navEvent.Params.Frame.URL
						if !strings.HasPrefix(navURL, appURL) {
							go func(redirectURL string) {
								if _, err := c.send("Page.navigate", h{"url": redirectURL}); err != nil {
									log.Printf("lorca/chrome: redirect to appURL failed: %v", err)
								}
							}(appURL)
						}
					}
				}
			}

			c.Lock()
			resc, ok := c.pending[res.ID]
			delete(c.pending, res.ID)
			c.Unlock()

			if !ok {
				continue
			}

			if res.Error.Message != "" {
				resc <- result{Err: errors.New(res.Error.Message)}
			} else if res.Result.Exception.Exception.Value != nil {
				resc <- result{Err: errors.New(string(res.Result.Exception.Exception.Value))}
			} else if res.Result.Exception.Exception.Subtype == "error" {
				resc <- result{Err: errors.New(res.Result.Exception.Exception.Description)}
			} else if res.Result.Result.Type == "object" && res.Result.Result.Subtype == "error" {
				resc <- result{Err: errors.New(res.Result.Result.Description)}
			} else if res.Result.Result.Type != "" {
				resc <- result{Value: res.Result.Result.Value}
			} else {
				res := targetMessageTemplate{}
				json.Unmarshal([]byte(params.Message), &res)
				resc <- result{Value: res.Result}
			}
		} else if m.Method == "Target.targetDestroyed" {
			params := struct {
				TargetID string `json:"targetId"`
			}{}
			json.Unmarshal(m.Params, &params)
			if params.TargetID == c.target {
				c.kill()
				return
			}
		}
	}
}

func (c *chrome) send(method string, params h) (json.RawMessage, error) {
	id := atomic.AddInt32(&c.id, 1)
	b, err := json.Marshal(h{"id": int(id), "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	resc := make(chan result)
	c.Lock()
	c.pending[int(id)] = resc
	c.Unlock()

	c.wsMu.Lock()
	err = websocket.JSON.Send(c.ws, h{
		"id":     int(id),
		"method": "Target.sendMessageToTarget",
		"params": h{"message": string(b), "sessionId": c.session},
	})
	c.wsMu.Unlock()
	if err != nil {
		return nil, err
	}
	res := <-resc
	return res.Value, res.Err
}

func (c *chrome) load(url string) error {
	_, err := c.send("Page.navigate", h{"url": url})
	if err == nil {
		c.Lock()
		c.appURL = url
		c.Unlock()
	}
	return err
}

func (c *chrome) setBlockBackNavigation(enable bool) {
	c.Lock()
	c.blockBackNav = enable
	c.Unlock()
}

func (c *chrome) setAppUserModelID(_ string) {}

func (c *chrome) eval(expr string) (json.RawMessage, error) {
	return c.send("Runtime.evaluate", h{"expression": expr, "awaitPromise": true, "returnByValue": true})
}

func (c *chrome) setBounds(b Bounds) error {
	if b.WindowState == "" {
		b.WindowState = WindowStateNormal
	}
	param := h{"windowId": c.window, "bounds": b}
	if b.WindowState != WindowStateNormal {
		param["bounds"] = h{"windowState": b.WindowState}
	}
	_, err := c.send("Browser.setWindowBounds", param)
	return err
}

func (c *chrome) bounds() (Bounds, error) {
	result, err := c.send("Browser.getWindowBounds", h{"windowId": c.window})
	if err != nil {
		return Bounds{}, err
	}
	bounds := struct {
		Bounds Bounds `json:"bounds"`
	}{}
	err = json.Unmarshal(result, &bounds)
	return bounds.Bounds, err
}

func (c *chrome) pdf(width, height int) ([]byte, error) {
	result, err := c.send("Page.printToPDF", h{
		"paperWidth":  float32(width) / 96,
		"paperHeight": float32(height) / 96,
	})
	if err != nil {
		return nil, err
	}
	pdf := struct {
		Data []byte `json:"data"`
	}{}
	err = json.Unmarshal(result, &pdf)
	return pdf.Data, err
}

func (c *chrome) png(x, y, width, height int, bg uint32, scale float32) ([]byte, error) {
	if x == 0 && y == 0 && width == 0 && height == 0 {
		// By default either use SVG size if it's an SVG, or use A4 page size
		bounds, err := c.eval(`document.rootElement ? [document.rootElement.x.baseVal.value, document.rootElement.y.baseVal.value, document.rootElement.width.baseVal.value, document.rootElement.height.baseVal.value] : [0,0,816,1056]`)
		if err != nil {
			return nil, err
		}
		rect := make([]int, 4)
		if err := json.Unmarshal(bounds, &rect); err != nil {
			return nil, err
		}
		x, y, width, height = rect[0], rect[1], rect[2], rect[3]
	}

	_, err := c.send("Emulation.setDefaultBackgroundColorOverride", h{
		"color": h{
			"r": (bg >> 16) & 0xff,
			"g": (bg >> 8) & 0xff,
			"b": bg & 0xff,
			"a": (bg >> 24) & 0xff,
		},
	})
	if err != nil {
		return nil, err
	}
	result, err := c.send("Page.captureScreenshot", h{
		"clip": h{
			"x": x, "y": y, "width": width, "height": height, "scale": scale,
		},
	})
	if err != nil {
		return nil, err
	}
	pdf := struct {
		Data []byte `json:"data"`
	}{}
	err = json.Unmarshal(result, &pdf)
	return pdf.Data, err
}

func (c *chrome) kill() {
	if c.ws != nil {
		c.ws.Close()
	}
	c.Lock()
	for _, ch := range c.pending {
		ch <- result{Err: errors.New("chrome closed")}
	}
	c.pending = map[int]chan result{}
	c.Unlock()

	if state := c.cmd.ProcessState; state == nil || !state.Exited() {
		killProcessTree(c.cmd.Process.Pid)
	}
}

func (c *chrome) done() <-chan struct{} { return c.doneC }

func (c *chrome) injectScript(js string) error {
	if _, err := c.send("Page.addScriptToEvaluateOnNewDocument", h{"source": js}); err != nil {
		return err
	}
	_, err := c.eval(js)
	return err
}

// evalNoWait sends a Runtime.evaluate command without waiting for the
// response.  The response is received by readLoop and silently dropped
// (no entry in c.pending).  Used for fire-and-forget evals where
// correctness is ensured by another path (e.g. addScriptToEvaluateOnNewDocument).
func (c *chrome) evalNoWait(expr string) {
	id := int(atomic.AddInt32(&c.id, 1))
	b, _ := json.Marshal(h{"id": id, "method": "Runtime.evaluate", "params": h{"expression": expr, "awaitPromise": false}})
	c.wsMu.Lock()
	websocket.JSON.Send(c.ws, h{ //nolint:errcheck
		"id":     id,
		"method": "Target.sendMessageToTarget",
		"params": h{"message": string(b), "sessionId": c.session},
	})
	c.wsMu.Unlock()
}

func (c *chrome) injectBinding(name string) error {
	script := bindingScript(name)
	// Register the binding script for all future page loads (authoritative path).
	if _, err := c.send("Page.addScriptToEvaluateOnNewDocument", h{"source": script}); err != nil {
		return err
	}
	// Fire-and-forget eval on the current page.  With 50+ bindings registered
	// at startup, blocking on each eval adds up.  The addScriptToEvaluateOnNewDocument
	// above ensures the binding is installed on the next navigation; this eval
	// is only a convenience for the currently-loaded page.
	c.evalNoWait(script)
	return nil
}

func contains(arr []string, x string) bool {
	for _, n := range arr {
		if x == n {
			return true
		}
	}
	return false
}
