package lorca

import (
	"fmt"
	"io/ioutil"
)

var AdditionalChromiumArgs = []string{
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
	"--disable-windows10-custom-titlebar",
	"--metrics-recording-only",
	"--no-first-run",
	"--no-default-browser-check",
	"--safebrowsing-disable-auto-update",
	"--password-store=basic",
	"--use-mock-keychain",
}

// NewHtml5 returns a new HTML5 UI for the given executable URL, user profile
// directory, window size and other options passed to the browser engine. If
// URL is an empty string - a blank page is displayed. If user profile directory
// is an empty string - a temporary directory is created and it will be removed on
// ui.Close(). You might want to use "--headless" custom CLI argument to test
// your UI code.
func NewHtml5(executable, url, dir string, width, height int, additionalArgs ...string) (UI, error) {
	if url == "" {
		url = "data:text/html,<html></html>"
	}
	tmpDir := ""
	if dir == "" {
		name, err := ioutil.TempDir("", "lorca")
		if err != nil {
			return nil, err
		}
		dir, tmpDir = name, name
	}
	args := append([]string{}, fmt.Sprintf("--app=%s", url))
	args = append(args, fmt.Sprintf("--user-data-dir=%s", dir))
	args = append(args, fmt.Sprintf("--window-size=%d,%d", width, height))
	args = append(args, additionalArgs...)
	args = append(args, "--remote-debugging-port=0")

	chrome, err := newChromeWithArgs(ChromiumExecutable(), args...)
	done := make(chan struct{})
	if err != nil {
		return nil, err
	}

	go func() {
		chrome.cmd.Wait()
		close(done)
	}()
	return &ui{chrome: chrome, done: done, tmpDir: tmpDir}, nil
}

// NewChromium returns a new Chromium HTML5 UI.
func NewChromium(url, dir string, width, height int, additionalArgs ...string) (UI, error) {
	return NewHtml5(ChromiumExecutable(), url, dir, width, height, additionalArgs...)
}

// NewEdge returns a new Edge HTML5 UI.
func NewEdge(url, dir string, width, height int, additionalArgs ...string) (UI, error) {
	return NewHtml5(EdgeExecutable(), url, dir, width, height, additionalArgs...)
}

// NewGoogleChrome returns a new Google Chrome HTML5 UI.
func NewGoogleChrome(url, dir string, width, height int, additionalArgs ...string) (UI, error) {
	return NewHtml5(GoogleChromeExecutable(), url, dir, width, height, additionalArgs...)
}
