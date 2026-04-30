package lorca

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"

	"golang.org/x/net/websocket"
)

// bindingScript returns the JS that installs window[name] as a relay-backed function.
// Send logic is inlined (not delegated to window.__lorcaSend) so the function holds no
// sandbox-realm references; Firefox Xray allows method calls on sandbox objects but
// throws "Permission denied to access property 'length'" when page code sees sandbox functions.
func bindingScript(name string) string {
	body := `var args = Array.prototype.slice.call(arguments); ` +
		`var seq = (window['` + name + `']._seq = (window['` + name + `']._seq || 0) + 1); ` +
		`return new Promise(function(resolve, reject) { ` +
		`window.__lorcaPending.set('` + name + `:' + seq, {resolve: resolve, reject: reject}); ` +
		`var _m = JSON.stringify({name: '` + name + `', seq: seq, args: args}); ` +
		`if (window.__lorcaOpen) { window.__lorcaWS.send(_m); } else { window.__lorcaQueue.push(_m); } ` +
		`});`
	// IIFE preserves _seq across re-evals so in-flight calls are not orphaned.
	return `(function() { var _s = (window['` + name + `'] && window['` + name + `']._seq) || 0; ` +
		`window['` + name + `'] = function() { ` + body + ` }; ` +
		`window['` + name + `']._seq = _s; })()`
}

// bootstrapTemplate sets up the relay WebSocket and lorca messaging primitives.
// window.__lorcaSend is intentionally absent: Firefox Xray allows method calls on
// sandbox objects but throws on function .length access. __lorcaSetupWS is kept
// in IIFE scope (not on window) for the same reason.
const bootstrapTemplate = `(function() {
  var _proto = window.location && window.location.protocol
  if (_proto && _proto !== 'http:' && _proto !== 'https:' && _proto !== 'data:') { return }
  if (_proto !== 'data:') { var _orig = window.location.origin; if (!_orig || _orig === 'null') { return } }
  if (window.__lorcaWS && window.__lorcaWS.readyState <= 1) { return }
  window.__lorcaPending = new Map()
  window.__lorcaQueue = []
  window.__lorcaOpen = false
  function __lorcaSetupWS() {
    var ws = new WebSocket('ws://127.0.0.1:__RELAY_PORT__')
    window.__lorcaWS = ws
    ws.onopen = function() { window.__lorcaOpen = true; for (var i = 0; i < window.__lorcaQueue.length; i++) { ws.send(window.__lorcaQueue[i]) } window.__lorcaQueue = [] }
    ws.onmessage = function(e) { var msg = JSON.parse(e.data); if (msg.type === 'result') { var cb = window.__lorcaPending.get(msg.name + ':' + msg.seq); if (cb) { if (msg.error) { cb.reject(new Error(msg.error)); } else { cb.resolve(msg.result); } window.__lorcaPending.delete(msg.name + ':' + msg.seq); } } }
    ws.onclose = function() { window.__lorcaOpen = false; window.__lorcaPending.forEach(function(cb) { cb.reject(new Error('relay reconnecting')) }); window.__lorcaPending = new Map(); window.__lorcaQueue = []; setTimeout(function() { if (!window.__lorcaWS || window.__lorcaWS.readyState > 1) { __lorcaSetupWS() } }, 500) }
  }
  __lorcaSetupWS()
})()`

type relay struct {
	mu       sync.Mutex   // guards bindings, names, client (held briefly)
	writeMu  sync.Mutex   // serialises WebSocket writes (can be held longer)
	bindings map[string]bindingFunc
	names    []string
	client   *websocket.Conn
	port     int
	ln       net.Listener
	server   *http.Server
}

func newRelay() (*relay, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	r := &relay{
		port:     ln.Addr().(*net.TCPAddr).Port,
		ln:       ln,
		bindings: map[string]bindingFunc{},
	}
	// Use a custom Server with no origin check so that pages loaded from
	// data: URIs (null origin) and file: URIs can connect to the relay.
	wsServer := websocket.Server{
		Handler: websocket.Handler(r.handleClient),
		Handshake: func(cfg *websocket.Config, req *http.Request) error {
			return nil // accept any origin
		},
	}
	mux := http.NewServeMux()
	mux.Handle("/", wsServer)
	r.server = &http.Server{Handler: mux}
	go r.server.Serve(ln)
	return r, nil
}

func (r *relay) bootstrapScript() string {
	return strings.ReplaceAll(bootstrapTemplate, "__RELAY_PORT__", fmt.Sprintf("%d", r.port))
}

// bind registers name -> f. Re-registering an existing name only updates the handler
// (no register message; a second register would reset the JS-side seq counter).
func (r *relay) bind(name string, f bindingFunc) error {
	r.mu.Lock()
	_, exists := r.bindings[name]
	r.bindings[name] = f
	if exists {
		r.mu.Unlock()
		return nil
	}
	r.names = append(r.names, name)
	client := r.client
	r.mu.Unlock()

	if client == nil {
		return nil
	}
	r.writeMu.Lock()
	err := websocket.JSON.Send(client, map[string]string{"type": "register", "name": name})
	r.writeMu.Unlock()
	return err
}

func (r *relay) handleClient(ws *websocket.Conn) {
	r.mu.Lock()
	old := r.client
	r.client = ws
	names := make([]string, len(r.names))
	copy(names, r.names)
	r.mu.Unlock()

	log.Printf("lorca/relay: page connected, replaying %d binding(s)", len(names))

	r.writeMu.Lock()
	for _, name := range names {
		if err := websocket.JSON.Send(ws, map[string]string{"type": "register", "name": name}); err != nil {
			log.Printf("lorca/relay: send register(%s) failed: %v", name, err)
			r.writeMu.Unlock()
			if old != nil {
				old.Close()
			}
			return
		}
	}
	r.writeMu.Unlock()

	if old != nil {
		old.Close()
	}

	type callMsg struct {
		Name string            `json:"name"`
		Seq  int               `json:"seq"`
		Args []json.RawMessage `json:"args"`
	}
	type resultMsg struct {
		Type   string           `json:"type"`
		Name   string           `json:"name"`
		Seq    int              `json:"seq"`
		Result *json.RawMessage `json:"result,omitempty"`
		Error  string           `json:"error,omitempty"`
	}

	for {
		var call callMsg
		if err := websocket.JSON.Receive(ws, &call); err != nil {
			log.Printf("lorca/relay: client read error (disconnect?): %v", err)
			break
		}
		log.Printf("lorca/relay: call %s seq=%d", call.Name, call.Seq)
		r.mu.Lock()
		f, ok := r.bindings[call.Name]
		r.mu.Unlock()
		if !ok {
			log.Printf("lorca/relay: no binding for %q", call.Name)
			continue
		}
		name, seq, args := call.Name, call.Seq, call.Args
		go func() {
			msg := resultMsg{Type: "result", Name: name, Seq: seq}
			res, err := f(args)
			if err != nil {
				msg.Error = err.Error()
			} else if b, err2 := json.Marshal(res); err2 != nil {
				msg.Error = err2.Error()
			} else {
				raw := json.RawMessage(b)
				msg.Result = &raw
			}
			msgBytes, marshalErr := json.Marshal(msg)
			if marshalErr != nil {
				log.Printf("lorca/relay: marshal envelope error for %s seq=%d: %v", name, seq, marshalErr)
				return
			}
			r.mu.Lock()
			client := r.client
			r.mu.Unlock()
			if client != ws {
				return // result for a navigated-away page
			}
			r.writeMu.Lock()
			defer r.writeMu.Unlock()
			// Re-verify client under mu in case it changed while waiting for writeMu.
			r.mu.Lock()
			active := r.client == ws
			r.mu.Unlock()
			if active {
				if err := websocket.Message.Send(client, string(msgBytes)); err != nil {
					log.Printf("lorca/relay: send error for %s seq=%d: %v", name, seq, err)
					client.Close()
				}
			}
		}()
	}

	r.mu.Lock()
	if r.client == ws {
		r.client = nil
	}
	r.mu.Unlock()
}

func (r *relay) close() {
	r.server.Close()
	r.mu.Lock()
	client := r.client
	r.client = nil
	r.mu.Unlock()
	if client != nil {
		client.Close()
	}
}
