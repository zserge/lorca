package lorca

import "testing"

func TestChrome(t *testing.T) {
	c, err := newChromeWithArgs(ChromeExecutable(), "--user-data-dir=/tmp", "--headless", "--remote-debugging-port=0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.kill()
	v, err := c.eval(`42`)
	if err != nil {
		t.Fatal(err)
	}
	if v != 42.0 {
		t.Fatal(v)
	}
}
