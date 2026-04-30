package lorca

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func dialRelay(t *testing.T, port int) *websocket.Conn {
	t.Helper()
	ws, err := websocket.Dial(fmt.Sprintf("ws://127.0.0.1:%d/", port), "", "http://127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	ws.SetDeadline(time.Now().Add(5 * time.Second))
	return ws
}

type regMsg struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type resMsg struct {
	Type   string          `json:"type"`
	Name   string          `json:"name"`
	Seq    int             `json:"seq"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

// TestRelayReplay verifies that bindings registered BEFORE a client connects
// are replayed as register messages when the client connects.
func TestRelayReplay(t *testing.T) {
	r, err := newRelay()
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()

	if err := r.bind("greet", func(args []json.RawMessage) (interface{}, error) {
		return "hello", nil
	}); err != nil {
		t.Fatal(err)
	}

	ws := dialRelay(t, r.port)
	defer ws.Close()

	var msg regMsg
	if err := websocket.JSON.Receive(ws, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Type != "register" || msg.Name != "greet" {
		t.Fatalf("expected {register greet}, got %+v", msg)
	}
}

// TestRelayRegisterAfterConnect verifies that binding registered AFTER a client
// connects sends a register message to the already-connected client.
func TestRelayRegisterAfterConnect(t *testing.T) {
	r, err := newRelay()
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()

	ws := dialRelay(t, r.port)
	defer ws.Close()

	if err := r.bind("add", func(args []json.RawMessage) (interface{}, error) {
		var a, b int
		if err := json.Unmarshal(args[0], &a); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(args[1], &b); err != nil {
			return nil, err
		}
		return a + b, nil
	}); err != nil {
		t.Fatal(err)
	}

	var msg regMsg
	if err := websocket.JSON.Receive(ws, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Type != "register" || msg.Name != "add" {
		t.Fatalf("expected {register add}, got %+v", msg)
	}
}

// TestRelayCallDispatch verifies that sending a call message invokes the
// binding and returns a result message.
func TestRelayCallDispatch(t *testing.T) {
	r, err := newRelay()
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()

	if err := r.bind("add", func(args []json.RawMessage) (interface{}, error) {
		var a, b int
		json.Unmarshal(args[0], &a)
		json.Unmarshal(args[1], &b)
		return a + b, nil
	}); err != nil {
		t.Fatal(err)
	}

	ws := dialRelay(t, r.port)
	defer ws.Close()

	// Drain the register message
	var reg regMsg
	if err := websocket.JSON.Receive(ws, &reg); err != nil {
		t.Fatal(err)
	}

	// Send a call
	if err := websocket.JSON.Send(ws, map[string]interface{}{
		"name": "add",
		"seq":  1,
		"args": []interface{}{2, 3},
	}); err != nil {
		t.Fatal(err)
	}

	var res resMsg
	if err := websocket.JSON.Receive(ws, &res); err != nil {
		t.Fatal(err)
	}
	if res.Type != "result" || res.Seq != 1 || string(res.Result) != "5" || res.Error != "" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

// TestRelayCallError verifies that a binding that returns an error sends back
// an error field in the result message.
func TestRelayCallError(t *testing.T) {
	r, err := newRelay()
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()

	if err := r.bind("fail", func(args []json.RawMessage) (interface{}, error) {
		return nil, errors.New("something went wrong")
	}); err != nil {
		t.Fatal(err)
	}

	ws := dialRelay(t, r.port)
	defer ws.Close()

	var reg regMsg
	if err := websocket.JSON.Receive(ws, &reg); err != nil {
		t.Fatal(err)
	}

	if err := websocket.JSON.Send(ws, map[string]interface{}{
		"name": "fail",
		"seq":  7,
		"args": []interface{}{},
	}); err != nil {
		t.Fatal(err)
	}

	var res resMsg
	if err := websocket.JSON.Receive(ws, &res); err != nil {
		t.Fatal(err)
	}
	if res.Type != "result" || res.Seq != 7 || res.Error != "something went wrong" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

// TestRelayRebind verifies that calling bind() with an existing name updates
// the handler but does NOT send a second register message.
func TestRelayRebind(t *testing.T) {
	r, err := newRelay()
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()

	var called atomic.Int32
	mkHandler := func(v int32) bindingFunc {
		return func(args []json.RawMessage) (interface{}, error) {
			called.Store(v)
			return v, nil
		}
	}

	r.bind("fn", mkHandler(1))

	ws := dialRelay(t, r.port)
	defer ws.Close()

	// Drain the first register
	var reg regMsg
	if err := websocket.JSON.Receive(ws, &reg); err != nil {
		t.Fatal(err)
	}

	// Rebind -must NOT send another register message to the client
	r.bind("fn", mkHandler(2))

	// Send a call; result should come from handler v=2
	if err := websocket.JSON.Send(ws, map[string]interface{}{
		"name": "fn",
		"seq":  1,
		"args": []interface{}{},
	}); err != nil {
		t.Fatal(err)
	}

	var res resMsg
	if err := websocket.JSON.Receive(ws, &res); err != nil {
		t.Fatal(err)
	}
	// Explicitly confirm the relay sent a result (not a second register message).
	if res.Type != "result" {
		t.Fatalf("expected result message, got type=%q (possible spurious register)", res.Type)
	}
	if string(res.Result) != "2" {
		t.Fatalf("expected handler v=2 result, got %s", res.Result)
	}
	if v := called.Load(); v != 2 {
		t.Fatalf("expected called=2, got %d", v)
	}
}

// TestBootstrapNoSandboxRealmFunctions guards against regressing to
// new window.Function. Under Firefox's BiDi preload sandbox, new window.Function
// creates a function whose realm is determined by the call site (the sandbox),
// not by the constructor's origin. Page code then throws "Permission denied to
// access property 'length'" when it inspects those sandbox-realm functions via
// Firefox's Xray wrapper -breaking WebSocket event dispatch and binding calls.
// window.eval("...") is the correct fix: it evaluates code in the page realm.
func TestBootstrapNoSandboxRealmFunctions(t *testing.T) {
	if strings.Contains(bootstrapTemplate, "new window.Function") {
		t.Error("bootstrapTemplate must not use new window.Function: functions created " +
			"this way run in the preload sandbox realm, not page realm. " +
			"Use window.eval(\"...\") instead.")
	}
	if strings.Contains(bootstrapTemplate, "new Function(") {
		t.Error("bootstrapTemplate must not use bare new Function(...): same sandbox-realm issue. " +
			"Use window.eval(\"...\") instead.")
	}
}

// TestBootstrapNoWindowEval guards against re-introducing window.eval into the
// bootstrap template.  window.eval() was tried (Approach 4) to escape the
// Firefox BiDi preload sandbox realm, but confirmed not to work: functions
// created via window.eval from a sandbox still run in the sandbox realm and
// cause "Permission denied to access property 'length'" when page-realm code
// introspects them.  The bootstrap must NOT use window.eval.
func TestBootstrapNoWindowEval(t *testing.T) {
	if strings.Contains(bootstrapTemplate, `window.eval(`) {
		t.Error("bootstrapTemplate must not use window.eval(): it does not escape " +
			"the Firefox preload sandbox realm (Approach 4, confirmed non-fix). " +
			"Use bindingScript injection via injectBinding instead.")
	}
}
