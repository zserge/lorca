package lorca

import (
	"strings"
	"testing"
	"time"
)

func TestChromeInjectScript(t *testing.T) {
	c, err := newChromeWithArgs(ChromeExecutable(""), "--user-data-dir=/tmp", "--headless", "--remote-debugging-port=0", "--remote-allow-origins=*")
	if err != nil {
		t.Fatal(err)
	}
	defer c.kill()
	if err := c.injectScript(`window.__injected = 42`); err != nil {
		t.Fatal(err)
	}
	result, err := c.eval(`window.__injected`)
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != `42` {
		t.Fatalf("expected 42, got %s", result)
	}
}

func TestChromeDone(t *testing.T) {
	c, err := newChromeWithArgs(ChromeExecutable(""), "--user-data-dir=/tmp", "--headless", "--remote-debugging-port=0", "--remote-allow-origins=*")
	if err != nil {
		t.Fatal(err)
	}
	c.kill()
	select {
	case <-c.done():
		// ok - channel closed after kill
	case <-time.After(5 * time.Second):
		t.Fatal("done() channel did not close within 5s after kill")
	}
}

func TestChromeEval(t *testing.T) {
	c, err := newChromeWithArgs(ChromeExecutable(""), "--user-data-dir=/tmp", "--headless", "--remote-debugging-port=0", "--remote-allow-origins=*")
	if err != nil {
		t.Fatal(err)
	}
	defer c.kill()

	for _, test := range []struct {
		Expr   string
		Result string
		Error  string
	}{
		{Expr: ``, Result: ``},
		{Expr: `42`, Result: `42`},
		{Expr: `2+3`, Result: `5`},
		{Expr: `(() => ({x: 5, y: 7}))()`, Result: `{"x":5,"y":7}`},
		{Expr: `(() => ([1,'foo',false]))()`, Result: `[1,"foo",false]`},
		{Expr: `((a, b) => a*b)(3, 7)`, Result: `21`},
		{Expr: `Promise.resolve(42)`, Result: `42`},
		{Expr: `Promise.reject('foo')`, Error: `"foo"`},
		{Expr: `throw "bar"`, Error: `"bar"`},
		{Expr: `2+`, Error: `SyntaxError: Unexpected end of input`},
	} {
		result, err := c.eval(test.Expr)
		if err != nil {
			if err.Error() != test.Error {
				t.Fatal(test.Expr, err, test.Error)
			}
		} else if string(result) != test.Result {
			t.Fatal(test.Expr, string(result), test.Result)
		}
	}
}

func TestChromeLoad(t *testing.T) {
	c, err := newChromeWithArgs(ChromeExecutable(""), "--user-data-dir=/tmp", "--headless", "--remote-debugging-port=0", "--remote-allow-origins=*")
	if err != nil {
		t.Fatal(err)
	}
	defer c.kill()
	if err := c.load("data:text/html,<html><body>Hello</body></html>"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		url, err := c.eval(`window.location.href`)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(string(url), `"data:text/html,`) {
			break
		}
	}
	if res, err := c.eval(`document.body ? document.body.innerText :
			new Promise(res => window.onload = () => res(document.body.innerText))`); err != nil {
		t.Fatal(err)
	} else if string(res) != `"Hello"` {
		t.Fatal(res)
	}
}

