package lorca

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

type UI interface {
	Load(url string) error
	Bind(name string, f interface{}) error
	Eval(js string) Value
	Close() error
	Done() <-chan struct{}
}

type ui struct {
	chrome *chrome
	done   chan struct{}
}

var ChromeExecutable = func() string { return "chromium-browser" }

var defaultChromeArgs = []string{
	"--disable-background-networking",
	"--disable-background-timer-throttling",
	"--disable-backgrounding-occluded-windows",
	"--disable-breakpad",
	"--disable-client-side-phishing-detection",
	"--disable-default-apps",
	"--disable-dev-shm-usage",
	"--disable-extensions",
	"--disable-features=site-per-process",
	"--disable-hang-monitor",
	"--disable-ipc-flooding-protection",
	"--disable-popup-blocking",
	"--disable-prompt-on-repost",
	"--disable-renderer-backgrounding",
	"--disable-sync",
	"--disable-translate",
	"--metrics-recording-only",
	"--no-first-run",
	"--safebrowsing-disable-auto-update",
	"--enable-automation",
	"--password-store=basic",
	"--use-mock-keychain",
}

func New(url string, dir string, width, height int, customArgs ...string) (UI, error) {
	if url == "" {
		url = "about:blank"
	}
	args := append(defaultChromeArgs, fmt.Sprintf("--app=%s", url))
	args = append(args, fmt.Sprintf("--user-data-dir=%s", dir))
	args = append(args, fmt.Sprintf("--window-size=%d,%d", width, height))
	args = append(args, customArgs...)
	args = append(args, "--remote-debugging-port=0")

	chrome, err := newChromeWithArgs(ChromeExecutable(), args...)
	done := make(chan struct{})
	if err != nil {
		return nil, err
	}

	go func() {
		chrome.cmd.Wait()
		close(done)
	}()
	return &ui{chrome: chrome, done: done}, nil
}

func (u *ui) Done() <-chan struct{} {
	return u.done
}

func (u *ui) Close() error {
	return u.chrome.kill()
}

func (u *ui) Load(url string) error { return u.chrome.load(url) }

func (u *ui) Bind(name string, f interface{}) error {
	v := reflect.ValueOf(f)
	// f must be a function
	if v.Kind() != reflect.Func {
		return errors.New("only functions can be bound")
	}
	// f must return either value and error or just error
	if n := v.Type().NumOut(); n > 2 {
		return errors.New("function may only return a value or a value+error")
	}

	return u.chrome.bind(name, func(raw []json.RawMessage) (interface{}, error) {
		if len(raw) != v.Type().NumIn() {
			return nil, errors.New("function arguments mismatch")
		}
		args := []reflect.Value{}
		for i := range raw {
			arg := reflect.New(v.Type().In(i))
			if err := json.Unmarshal(raw[i], arg.Interface()); err != nil {
				return nil, err
			}
			args = append(args, arg.Elem())
		}
		errorType := reflect.TypeOf((*error)(nil)).Elem()
		res := v.Call(args)
		switch len(res) {
		case 0:
			// No results from the function, just return nil
			return nil, nil
		case 1:
			// One result may be a value, or an error
			if res[0].Type().Implements(errorType) {
				return nil, res[0].Interface().(error)
			} else {
				return res[0].Interface(), nil
			}
		case 2:
			// Two results: first one is value, second is error
			if !res[1].Type().Implements(errorType) {
				return nil, errors.New("second return value must be an error")
			}
			if res[1].Interface() == nil {
				return res[0].Interface(), nil
			} else {
				return res[0].Interface(), res[1].Interface().(error)
			}
		default:
			return nil, errors.New("unexpected number of return values")
		}
	})
}

func (u *ui) Eval(js string) Value {
	v, err := u.chrome.eval(js)
	return value{err: err, raw: v}
}
