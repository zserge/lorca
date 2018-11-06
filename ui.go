package lorca

import (
	"fmt"
	"log"
)

type UI interface {
	Load(url string) error
	Bind(name string, f func([]interface{}) (interface{}, error)) error
	Eval(js string) (interface{}, error)
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

	for method, args := range map[string]h{
		"Page.enable":                    nil,
		"Target.setAutoAttach":           h{"autoAttach": true, "waitForDebuggerOnStart": false},
		"Page.setLifecycleEventsEnabled": h{"enabled": true},
		"Network.enable":                 nil,
		"Runtime.enable":                 nil,
		"Security.enable":                nil,
		"Performance.enable":             nil,
		"Log.enable":                     nil,
	} {
		if v, err := chrome.send(method, args); err != nil {
			chrome.kill()
			chrome.cmd.Wait()
			return nil, err
		} else {
			log.Println(method, v)
		}
	}
	go func() {
		log.Println("wait started")
		chrome.cmd.Wait()
		log.Println("wait done")
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
func (u *ui) Bind(name string, f func([]interface{}) (interface{}, error)) error {
	return u.chrome.bind(name, f)
}
func (u *ui) Eval(js string) (interface{}, error) { return u.chrome.eval(js) }
