package lorca

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func skipIfNoFirefox(t *testing.T) string {
	t.Helper()
	path, ok := os.LookupEnv("LORCAFIREFOX")
	if !ok || path == "" {
		t.Skip("LORCAFIREFOX not set; skipping Firefox integration tests")
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("LORCAFIREFOX=%s not found; skipping Firefox integration tests", path)
	}
	return path
}

func TestBidiValueToJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"string", `{"type":"string","value":"hello"}`, `"hello"`},
		{"number int", `{"type":"number","value":42}`, `42`},
		{"number float", `{"type":"number","value":3.14}`, `3.14`},
		{"boolean true", `{"type":"boolean","value":true}`, `true`},
		{"boolean false", `{"type":"boolean","value":false}`, `false`},
		{"null", `{"type":"null"}`, `null`},
		{"undefined", `{"type":"undefined"}`, `null`},
		{"array", `{"type":"array","value":[{"type":"string","value":"a"},{"type":"number","value":1}]}`, `["a",1]`},
		{"object", `{"type":"object","value":[["key",{"type":"string","value":"val"}]]}`, `{"key":"val"}`},
		{"nested", `{"type":"array","value":[{"type":"object","value":[["x",{"type":"number","value":1}]]}]}`, `[{"x":1}]`},
		{"unknown type", `{"type":"symbol"}`, `null`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := bidiValueToJSON(json.RawMessage(tt.input))
			if err != nil {
				t.Fatal(err)
			}
			if string(result) != tt.expected {
				t.Fatalf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestFirefoxNew(t *testing.T) {
	binary := skipIfNoFirefox(t)
	dir := t.TempDir()
	f, err := newFirefoxWithArgs(binary, "", "--headless", "--profile", dir)
	if err != nil {
		t.Fatal(err)
	}
	f.kill()
	select {
	case <-f.done():
	case <-time.After(5 * time.Second):
		t.Fatal("done() did not close after kill")
	}
}

func TestFirefoxEval(t *testing.T) {
	binary := skipIfNoFirefox(t)
	dir := t.TempDir()
	f, err := newFirefoxWithArgs(binary, "", "--headless", "--profile", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { f.kill(); <-f.done() }()

	for _, tc := range []struct {
		expr   string
		result string
		errMsg string
	}{
		{expr: `42`, result: `42`},
		{expr: `"hello"`, result: `"hello"`},
		{expr: `2+3`, result: `5`},
		{expr: `[1,2,3]`, result: `[1,2,3]`},
		{expr: `({x:1,y:2})`, result: `{"x":1,"y":2}`},
		{expr: `Promise.resolve(7)`, result: `7`},
		{expr: `throw "fail"`, errMsg: `"fail"`},
	} {
		result, err := f.eval(tc.expr)
		if tc.errMsg != "" {
			if err == nil || err.Error() != tc.errMsg {
				t.Fatalf("%s: expected error %q, got %v", tc.expr, tc.errMsg, err)
			}
		} else if err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.expr, err)
		} else if string(result) != tc.result {
			t.Fatalf("%s: expected %s, got %s", tc.expr, tc.result, string(result))
		}
	}
}

func TestFirefoxLoad(t *testing.T) {
	binary := skipIfNoFirefox(t)
	dir := t.TempDir()
	f, err := newFirefoxWithArgs(binary, "", "--headless", "--profile", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { f.kill(); <-f.done() }()

	if err := f.load("data:text/html,<html><body>Hello</body></html>"); err != nil {
		t.Fatal(err)
	}
	result, err := f.eval(`document.body.innerText`)
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != `"Hello"` {
		t.Fatalf("expected %q, got %s", "Hello", result)
	}
}

func TestFirefoxInjectScript(t *testing.T) {
	binary := skipIfNoFirefox(t)
	dir := t.TempDir()
	f, err := newFirefoxWithArgs(binary, "", "--headless", "--profile", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { f.kill(); <-f.done() }()

	if err := f.injectScript(`window.__injected = 99`); err != nil {
		t.Fatal(err)
	}
	result, err := f.eval(`window.__injected`)
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != `99` {
		t.Fatalf("expected 99, got %s", result)
	}
}

func TestFirefoxBounds(t *testing.T) {
	binary := skipIfNoFirefox(t)
	dir := t.TempDir()
	f, err := newFirefoxWithArgs(binary, "", "--headless", "--profile", dir, "--window-size=800,600")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { f.kill(); <-f.done() }()

	b, err := f.bounds()
	if err != nil {
		t.Fatal(err)
	}
	if b.Width == 0 || b.Height == 0 {
		t.Fatalf("expected non-zero bounds, got %+v", b)
	}
}

func TestFirefoxSetBounds(t *testing.T) {
	binary := skipIfNoFirefox(t)
	dir := t.TempDir()
	f, err := newFirefoxWithArgs(binary, "", "--headless", "--profile", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { f.kill(); <-f.done() }()

	if err := f.setBounds(Bounds{Width: 1024, Height: 768}); err != nil {
		t.Fatal(err)
	}
	b, err := f.bounds()
	if err != nil {
		t.Fatal(err)
	}
	if b.Width != 1024 || b.Height != 768 {
		t.Fatalf("expected 1024x768, got %dx%d", b.Width, b.Height)
	}
}

// ---------------------------------------------------------------------------
// Pure-Go structural tests -no browser required
// ---------------------------------------------------------------------------

// TestBindingScriptInvariants checks properties of the JS produced by
// bindingScript without starting a browser. Three invariants must hold:
//
//  1. No double quotes in the output -the code is embedded as the argument to
//     window.eval("...") inside the preload functionDeclaration string; a double
//     quote would close the outer JS string and produce a syntax error.
//
//  2. Binding is created as a plain function expression, not via new Function -
//     new Function called from the preload sandbox produces a sandbox-realm
//     function, which causes "Permission denied to access property 'length'"
//     when page code (Vue, WebSocket internals) tries to call it.
//
//  3. The function body references window.__lorcaPending and window.__lorcaSend,
//     the page-realm state set up by the bootstrap, so calls are routed through
//     the relay.
func TestBindingScriptInvariants(t *testing.T) {
	for _, name := range []string{"add", "getPlayerData", "myBinding123", "x"} {
		t.Run(name, func(t *testing.T) {
			code := bindingScript(name)

			if strings.Contains(code, `"`) {
				t.Errorf("output contains double quotes; must be safely embeddable "+
					"inside window.eval(\"...\") without escaping:\n%s", code)
			}

			want := "window['" + name + "'] = function()"
			if !strings.Contains(code, want) {
				t.Errorf("expected pattern %q in output:\n%s", want, code)
			}

			if strings.Contains(code, "new window.Function") || strings.Contains(code, "new Function(") {
				t.Errorf("output must not use new Function (creates sandbox-realm function):\n%s", code)
			}

			if !strings.Contains(code, "window.__lorcaPending") {
				t.Errorf("output does not reference window.__lorcaPending:\n%s", code)
			}
			// window.__lorcaSend has been intentionally removed: on Firefox, a
			// sandbox-realm function assigned to window causes
			// "Permission denied to access property 'length'" when page-realm
			// code (e.g. Vue) introspects it via Xray.  bindingScript inlines
			// the send using window.__lorcaWS.send() / window.__lorcaQueue.push()
			// instead (method calls on Xray-wrapped objects are permitted).
			if strings.Contains(code, "window.__lorcaSend") {
				t.Errorf("output must NOT reference window.__lorcaSend (sandbox-realm function):\n%s", code)
			}
			if !strings.Contains(code, "window.__lorcaWS.send") {
				t.Errorf("output does not reference window.__lorcaWS.send:\n%s", code)
			}
			if !strings.Contains(code, "window.__lorcaQueue.push") {
				t.Errorf("output does not reference window.__lorcaQueue.push:\n%s", code)
			}

			// Wrapping in window.eval("...") must add exactly two double quotes -
			// the delimiters -and no others from the code itself.
			wrapped := `window.eval("` + code + `")`
			if n := strings.Count(wrapped, `"`); n != 2 {
				t.Errorf("wrapped code has %d double quotes, want 2 (the window.eval delimiters only)", n)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Firefox integration tests -require LORCAFIREFOX env var
// ---------------------------------------------------------------------------

// TestFirefoxBootstrapFunctionsPageRealm verifies that the bootstrap's onopen
// and onmessage handlers are page-realm functions after a navigation (when the
// preload fires). It checks that .length is readable on each handler from
// page-realm code (script.evaluate). When functions are sandbox-realm,
// Firefox's Xray wrapper throws "Permission denied to access property 'length'"
// whenever page code inspects them.
func TestFirefoxBootstrapFunctionsPageRealm(t *testing.T) {
	binary := skipIfNoFirefox(t)
	r, err := newRelay()
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()

	f, err := newFirefoxWithArgs(binary, "", "--headless", "--profile", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { f.kill(); <-f.done() }()

	if err := f.injectScript(r.bootstrapScript()); err != nil {
		t.Fatal(err)
	}
	// Navigate to a data: URL so the bootstrap's protocol guard allows the script to run.
	if err := f.load("data:text/html,<html></html>"); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		expr string
		want string
		desc string
	}{
		{`typeof window.__lorcaWS`, `"object"`, "__lorcaWS must be set"},
		{`typeof window.__lorcaPending`, `"object"`, "__lorcaPending must be set"},
		{`typeof window.__lorcaQueue`, `"object"`, "__lorcaQueue must be set"},
		{`typeof window.__lorcaWS.onopen`, `"function"`, "onopen must be set"},
		{`typeof window.__lorcaWS.onmessage`, `"function"`, "onmessage must be set"},
		// .length access: page-realm functions expose it; sandbox-realm functions
		// throw "Permission denied" when page code accesses .length via Xray.
		{`typeof window.__lorcaWS.onopen.length`, `"number"`, "onopen.length readable (page-realm)"},
		{`typeof window.__lorcaWS.onmessage.length`, `"number"`, "onmessage.length readable (page-realm)"},
	} {
		result, err := f.eval(c.expr)
		if err != nil {
			t.Fatalf("%s: eval(%q) error (Permission Denied indicates sandbox-realm function): %v",
				c.desc, c.expr, err)
		}
		if string(result) != c.want {
			t.Fatalf("%s: eval(%q) = %s, want %s", c.desc, c.expr, result, c.want)
		}
	}
}

// TestFirefoxBindingScriptPageRealm verifies that injectBinding creates
// window['name'] as a page-realm function. The .length check mirrors what
// Firefox's WebSocket event dispatch and Vue's call sites do before invoking
// the function; sandbox-realm functions throw "Permission denied" at that point.
func TestFirefoxBindingScriptPageRealm(t *testing.T) {
	binary := skipIfNoFirefox(t)
	r, err := newRelay()
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()

	f, err := newFirefoxWithArgs(binary, "", "--headless", "--profile", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { f.kill(); <-f.done() }()

	if err := f.injectScript(r.bootstrapScript()); err != nil {
		t.Fatal(err)
	}
	if err := f.injectBinding("testBinding"); err != nil {
		t.Fatal(err)
	}
	if err := f.load("data:text/html,<html></html>"); err != nil {
		t.Fatal(err)
	}

	result, err := f.eval(`typeof window.testBinding.length`)
	if err != nil {
		t.Fatalf("window.testBinding.length threw (indicates sandbox-realm function, not page-realm): %v", err)
	}
	if string(result) != `"number"` {
		t.Fatalf("expected testBinding.length to be a number, got typeof=%s", result)
	}
}

// TestFirefoxBindingBasic is an end-to-end test for a single Go->JS->Go binding
// call under Firefox. This is the scenario that silently hangs when bindings
// land in sandbox realm: the JS Promise never resolves, Eval never returns.
func TestFirefoxBindingBasic(t *testing.T) {
	binary := skipIfNoFirefox(t)
	ui, err := NewWithBrowser("data:text/html,<html></html>", t.TempDir(),
		binary, 480, 320, BrowserFirefox, "", "--headless")
	if err != nil {
		t.Fatal(err)
	}
	defer ui.Close()

	if err := ui.Bind("add", func(a, b int) int { return a + b }); err != nil {
		t.Fatal(err)
	}
	v := ui.Eval(`add(2, 3)`)
	if v.Err() != nil {
		t.Fatalf("binding call failed: %v", v.Err())
	}
	if v.Int() != 5 {
		t.Fatalf("expected 5, got %d", v.Int())
	}
}

// TestFirefoxBindingMultiple verifies that several bindings registered on the
// same UI instance all work -each gets its own preload script and its own
// entry in the relay dispatch table.
func TestFirefoxBindingMultiple(t *testing.T) {
	binary := skipIfNoFirefox(t)
	ui, err := NewWithBrowser("data:text/html,<html></html>", t.TempDir(),
		binary, 480, 320, BrowserFirefox, "", "--headless")
	if err != nil {
		t.Fatal(err)
	}
	defer ui.Close()

	if err := ui.Bind("double", func(n int) int { return n * 2 }); err != nil {
		t.Fatal(err)
	}
	if err := ui.Bind("sum", func(a, b, c int) int { return a + b + c }); err != nil {
		t.Fatal(err)
	}
	if err := ui.Bind("neg", func(n int) int { return -n }); err != nil {
		t.Fatal(err)
	}

	if v := ui.Eval(`double(7)`); v.Err() != nil || v.Int() != 14 {
		t.Fatalf("double(7): got %d, err %v", v.Int(), v.Err())
	}
	if v := ui.Eval(`sum(1, 2, 3)`); v.Err() != nil || v.Int() != 6 {
		t.Fatalf("sum(1,2,3): got %d, err %v", v.Int(), v.Err())
	}
	if v := ui.Eval(`neg(5)`); v.Err() != nil || v.Int() != -5 {
		t.Fatalf("neg(5): got %d, err %v", v.Int(), v.Err())
	}
}

// TestFirefoxBindingAfterNavigation verifies that bindings registered before a
// page navigation are re-established on the new document via the per-binding
// preload script, without needing another Bind call from Go.
func TestFirefoxBindingAfterNavigation(t *testing.T) {
	binary := skipIfNoFirefox(t)
	ui, err := NewWithBrowser("data:text/html,<html>page1</html>", t.TempDir(),
		binary, 480, 320, BrowserFirefox, "", "--headless")
	if err != nil {
		t.Fatal(err)
	}
	defer ui.Close()

	if err := ui.Bind("mul", func(a, b int) int { return a * b }); err != nil {
		t.Fatal(err)
	}
	if v := ui.Eval(`mul(3, 4)`); v.Err() != nil || v.Int() != 12 {
		t.Fatalf("before nav: mul(3,4) = %d, err %v", v.Int(), v.Err())
	}

	if err := ui.Load("data:text/html,<html>page2</html>"); err != nil {
		t.Fatal(err)
	}

	if v := ui.Eval(`mul(5, 6)`); v.Err() != nil || v.Int() != 30 {
		t.Fatalf("after nav: mul(5,6) = %d, err %v", v.Int(), v.Err())
	}
}

// TestFirefoxBindingError verifies that when a Go binding returns an error the
// JS Promise is rejected and ui.Eval surfaces that error.
func TestFirefoxBindingError(t *testing.T) {
	binary := skipIfNoFirefox(t)
	ui, err := NewWithBrowser("data:text/html,<html></html>", t.TempDir(),
		binary, 480, 320, BrowserFirefox, "", "--headless")
	if err != nil {
		t.Fatal(err)
	}
	defer ui.Close()

	if err := ui.Bind("fail", func() error { return errors.New("binding error") }); err != nil {
		t.Fatal(err)
	}
	if v := ui.Eval(`fail()`); v.Err() == nil {
		t.Fatal("expected error from failing binding, got nil")
	}
}
