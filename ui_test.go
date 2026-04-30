package lorca

import (
	"errors"
	"math/rand"
	"os"
	"strconv"
	"testing"
)

func TestEval(t *testing.T) {
	ui, err := New("", "", "", 480, 320, "", "--headless")
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
	ui, err := New("", "", "", 480, 320, "", "--headless")
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

func TestFunctionReturnTypes(t *testing.T) {
	ui, err := New("", "", "", 480, 320, "", "--headless")
	if err != nil {
		t.Fatal(err)
	}
	defer ui.Close()

	if err := ui.Bind("noResults", func() { /* Left empty to imply return : S1023 */ }); err != nil {
		t.Fatal(err)
	}
	if err := ui.Bind("oneNonNilResult", func() interface{} { return 1 }); err != nil {
		t.Fatal(err)
	}
	if err := ui.Bind("oneNilResult", func() interface{} { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := ui.Bind("oneNonNilErrorResult", func() error { return errors.New("error") }); err != nil {
		t.Fatal(err)
	}
	if err := ui.Bind("oneNilErrorResult", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := ui.Bind("twoResultsNonNilError", func() (interface{}, error) { return nil, errors.New("error") }); err != nil {
		t.Fatal(err)
	}
	if err := ui.Bind("twoResultsNilError", func() (interface{}, error) { return 1, nil }); err != nil {
		t.Fatal(err)
	}
	if err := ui.Bind("twoResultsBothNonNil", func() (interface{}, error) { return 1, errors.New("error") }); err != nil {
		t.Fatal(err)
	}
	if err := ui.Bind("twoResultsBothNil", func() (interface{}, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}

	if v := ui.Eval(`noResults()`); v.Err() != nil {
		t.Fatal(v)
	}
	if v := ui.Eval(`oneNonNilResult()`); v.Int() != 1 {
		t.Fatal(v)
	}
	if v := ui.Eval(`oneNilResult()`); v.Err() != nil {
		t.Fatal(v)
	}
	if v := ui.Eval(`oneNonNilErrorResult()`); v.Err() == nil {
		t.Fatal(v)
	}
	if v := ui.Eval(`oneNilErrorResult()`); v.Err() != nil {
		t.Fatal(v)
	}
	if v := ui.Eval(`twoResultsNonNilError()`); v.Err() == nil {
		t.Fatal(v)
	}
	if v := ui.Eval(`twoResultsNilError()`); v.Err() != nil || v.Int() != 1 {
		t.Fatal(v)
	}
	if v := ui.Eval(`twoResultsBothNonNil()`); v.Err() == nil {
		t.Fatal(v)
	}
	if v := ui.Eval(`twoResultsBothNil()`); v.Err() != nil {
		t.Fatal(v)
	}
}

func TestNewWithBrowserChrome(t *testing.T) {
	ui, err := NewWithBrowser("", "", "", 480, 320, BrowserChrome, "", "--headless")
	if err != nil {
		t.Fatal(err)
	}
	defer ui.Close()
	if n := ui.Eval(`2+3`).Int(); n != 5 {
		t.Fatalf("expected 5, got %d", n)
	}
}

func TestNewWithBrowserAuto(t *testing.T) {
	ui, err := NewWithBrowser("", "", "", 480, 320, BrowserAuto, "", "--headless")
	if err != nil {
		t.Fatal(err)
	}
	defer ui.Close()
	if n := ui.Eval(`2+3`).Int(); n != 5 {
		t.Fatalf("expected 5, got %d", n)
	}
}

func TestNewWithBrowserFirefox(t *testing.T) {
	ffPath, ok := os.LookupEnv("LORCAFIREFOX")
	if !ok || ffPath == "" {
		t.Skip("LORCAFIREFOX not set")
	}
	ui, err := NewWithBrowser("", "", ffPath, 480, 320, BrowserFirefox, "", "--headless")
	if err != nil {
		t.Fatal(err)
	}
	defer ui.Close()
	if n := ui.Eval(`2+3`).Int(); n != 5 {
		t.Fatalf("expected 5, got %d", n)
	}
}
