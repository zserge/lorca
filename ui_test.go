package lorca

import (
	"math/rand"
	"strconv"
	"testing"
)

func TestEval(t *testing.T) {
	ui, err := New("", "/tmp/chrome-test-dir", 480, 320, "--headless")
	if err != nil {
		t.Fatal(err)
	}
	defer ui.Close()

	if n := ui.Eval(`2+3`).Int(); n != 5 {
		t.Fatal(n)
	}

	if s := ui.Eval(`"foo" + "bar"`).String(); s != "foobar" {
		t.Fatal(s)
	}

	if a := ui.Eval(`[1,2,3].map(n => n *2)`).Array(); a[0].Int() != 2 || a[1].Int() != 4 || a[2].Int() != 6 {
		t.Fatal(a)
	}

	// XXX this probably should be unquoted?
	if err := ui.Eval(`throw "fail"`).Err(); err.Error() != `"fail"` {
		t.Fatal(err)
	}
}

func TestBind(t *testing.T) {
	ui, err := New("", "/tmp/chrome-test-dir", 480, 320, "--headless")
	if err != nil {
		t.Fatal(err)
	}
	defer ui.Close()

	if err := ui.Bind("add", func(a, b int) int { return a + b }); err != nil {
		t.Fatal(err)
	}
	if err := ui.Bind("rand", func() int { return rand.Int() }); err != nil {
		t.Fatal(err)
	}
	if err := ui.Bind("strlen", func(s string) int { return len(s) }); err != nil {
		t.Fatal(err)
	}
	if err := ui.Bind("atoi", func(s string) (int, error) { return strconv.Atoi(s) }); err != nil {
		t.Fatal(err)
	}
	if err := ui.Bind("shouldFail", "hello"); err == nil {
		t.Fail()
	}

	if n := ui.Eval(`add(2,3)`); n.Int() != 5 {
		t.Fatal(n)
	}
	if n := ui.Eval(`add(2,3,4)`); n.Err() == nil {
		t.Fatal(n)
	}
	if n := ui.Eval(`add(2)`); n.Err() == nil {
		t.Fatal(n)
	}
	if n := ui.Eval(`add("hello", "world")`); n.Err() == nil {
		t.Fatal(n)
	}
	if n := ui.Eval(`rand()`); n.Err() != nil {
		t.Fatal(n)
	}
	if n := ui.Eval(`rand(100)`); n.Err() == nil {
		t.Fatal(n)
	}
	if n := ui.Eval(`strlen('foo')`); n.Int() != 3 {
		t.Fatal(n)
	}
	if n := ui.Eval(`strlen(123)`); n.Err() == nil {
		t.Fatal(n)
	}
	if n := ui.Eval(`atoi('123')`); n.Int() != 123 {
		t.Fatal(n)
	}
	if n := ui.Eval(`atoi('hello')`); n.Err() == nil {
		t.Fatal(n)
	}
}
