package lorca

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// UI interface allows talking to the HTML5 UI from Go.
type UI interface {
	Load(url string) error
	Bounds() (Bounds, error)
	SetBounds(Bounds) error
	Bind(name string, f interface{}) error
	Eval(js string) Value
	Done() <-chan struct{}
	Close() error
	GetDebugPort() int
	// SetBlockBackNavigation controls whether navigations away from the loaded
	// app URL (e.g. the user pressing Back) are intercepted and redirected back.
	// Call with true after Load() to prevent the browser from showing a blank
	// page when the user presses Back or uses a keyboard shortcut.
	SetBlockBackNavigation(enable bool)
	// SetAppUserModelID sets a Windows App User Model ID on the browser window
	// so it is grouped separately from other browser instances in the taskbar.
	// No-op on non-Windows platforms and for the Chrome backend.
	SetAppUserModelID(id string)
}

type ui struct {
	browser browserImpl
	relay   *relay
	tmpDir  string
}

var defaultChromeArgs = []string{
	"--disable-background-networking",
	"--disable-background-timer-throttling",
	"--disable-backgrounding-occluded-windows",
	"--disable-breakpad",
	"--disable-crash-reporter",
	"--disable-client-side-phishing-detection",
	"--disable-default-apps",
	"--disable-dev-shm-usage",
	"--disable-infobars",
	"--disable-extensions",
	"--disable-features=site-per-process,BlockInsecurePrivateNetworkRequests,PrivateNetworkAccessChecks",
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
	//"--enable-automation", https://github.com/zserge/lorca/issues/167
	"--password-store=basic",
	"--use-mock-keychain",
	"--remote-allow-origins=*",
}

// BrowserHint tells NewWithBrowser which browser backend to use.
type BrowserHint string

const (
	// BrowserAuto selects the backend by inspecting the resolved binary path.
	BrowserAuto BrowserHint = "auto"
	// BrowserChrome selects the Chromium-family CDP backend.
	BrowserChrome BrowserHint = "chrome"
	// BrowserFirefox selects the Firefox WebDriver BiDi backend.
	BrowserFirefox BrowserHint = "firefox"
)

// NewWithBrowser is like New but lets the caller specify which browser backend
// to use. hint == BrowserAuto inspects the resolved binary name; a path
// containing "firefox" (case-insensitive) selects the Firefox backend.
// appName is an optional human-readable name shown in the Firefox tab strip
// (via CSS ::before); pass an empty string to omit the label.
func NewWithBrowser(url, dir, preferPath string, width, height int, hint BrowserHint, appName, appIconPath string, customArgs ...string) (UI, error) {
	if url == "" {
		url = "data:text/html,<html></html>"
	}
	tmpDir := ""
	if dir == "" {
		name, err := os.MkdirTemp("", "lorca")
		if err != nil {
			return nil, err
		}
		dir, tmpDir = name, name
	}

	r, err := newRelay()
	if err != nil {
		return nil, err
	}

	// Resolve binary and select backend.
	var binary string
	useFirefox := false
	switch hint {
	case BrowserFirefox:
		binary = LocateFirefox(preferPath)
		if binary == "" {
			r.close()
			return nil, errors.New("lorca: no Firefox binary found")
		}
		useFirefox = true
	default: // BrowserChrome or BrowserAuto
		binary = ChromeExecutable(preferPath)
		if hint == BrowserAuto {
			useFirefox = strings.Contains(strings.ToLower(binary), "firefox")
		}
	}

	var browser browserImpl
	if useFirefox {
		if err := setupFirefoxProfile(dir, appName, appIconPath); err != nil {
			fmt.Fprintf(os.Stderr, "lorca: firefox profile setup: %v\n", err)
		}
		args := append(append([]string{}, defaultFirefoxArgs...),
			"--profile", dir,
		)
		if width > 0 {
			args = append(args, fmt.Sprintf("--width=%d", width))
		}
		if height > 0 {
			args = append(args, fmt.Sprintf("--height=%d", height))
		}
		args = append(args, customArgs...)
		args = append(args, url)
		browser, err = newFirefoxWithArgs(binary, appIconPath, args...)
	} else {
		args := append(append([]string{}, defaultChromeArgs...),
			fmt.Sprintf("--app=%s", url),
			fmt.Sprintf("--user-data-dir=%s", dir),
			fmt.Sprintf("--window-size=%d,%d", width, height),
		)
		args = append(args, customArgs...)
		browser, err = newChromeWithArgs(binary, args...)
	}
	if err != nil {
		r.close()
		return nil, err
	}

	if err := browser.injectScript(r.bootstrapScript()); err != nil {
		r.close()
		browser.kill()
		<-browser.done()
		return nil, err
	}

	return &ui{browser: browser, relay: r, tmpDir: tmpDir}, nil
}

// New returns a new HTML5 UI for the given URL, user profile directory, window
// size and other options passed to the browser engine. If URL is an empty
// string - a blank page is displayed. If user profile directory is an empty
// string - a temporary directory is created and it will be removed on
// ui.Close(). appName is an optional human-readable application name shown in
// the Firefox tab strip area; pass an empty string to omit it. appIconPath is
// an optional path to a .ico file used to set the window icon when running
// under Firefox; pass an empty string to fall back to PE resource 1
// (goversioninfo convention) or to skip icon setup. You might want to use
// "--headless" custom CLI argument to test your UI code.
func New(url, dir, preferPath string, width, height int, appName, appIconPath string, customArgs ...string) (UI, error) {
	return NewWithBrowser(url, dir, preferPath, width, height, BrowserAuto, appName, appIconPath, customArgs...)
}

func (u *ui) Done() <-chan struct{} {
	return u.browser.done()
}

func (u *ui) Close() error {
	u.relay.close()
	// ignore err, as the browser process might be already dead, when user closes the window.
	u.browser.kill()
	<-u.browser.done()
	if u.tmpDir != "" {
		if err := os.RemoveAll(u.tmpDir); err != nil {
			return err
		}
	}
	return nil
}

func (u *ui) Load(url string) error { return u.browser.load(url) }

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

	if err := u.relay.bind(name, func(raw []json.RawMessage) (interface{}, error) {
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
				if res[0].Interface() != nil {
					return nil, res[0].Interface().(error)
				}
				return nil, nil
			}
			return res[0].Interface(), nil
		case 2:
			// Two results: first one is value, second is error
			if !res[1].Type().Implements(errorType) {
				return nil, errors.New("second return value must be an error")
			}
			if res[1].Interface() == nil {
				return res[0].Interface(), nil
			}
			return res[0].Interface(), res[1].Interface().(error)
		default:
			return nil, errors.New("unexpected number of return values")
		}
	}); err != nil {
		return err
	}
	// Install the binding on the current page immediately AND register it for
	// all future page loads via addScriptToEvaluateOnNewDocument. This ensures
	// bound functions are available synchronously before any page JS runs,
	// avoiding the race where a page mounts before the relay WebSocket has
	// delivered its register messages.
	return u.browser.injectBinding(name)
}

func (u *ui) Eval(js string) Value {
	v, err := u.browser.eval(js)
	return value{err: err, raw: v}
}

func (u *ui) SetBounds(b Bounds) error {
	return u.browser.setBounds(b)
}

func (u *ui) Bounds() (Bounds, error) {
	return u.browser.bounds()
}

func (u *ui) GetDebugPort() int {
	switch b := u.browser.(type) {
	case *chrome:
		return b.debugPort
	case *firefox:
		return b.debugPort
	default:
		return 0
	}
}

func (u *ui) SetBlockBackNavigation(enable bool) {
	u.browser.setBlockBackNavigation(enable)
}

func (u *ui) SetAppUserModelID(id string) {
	u.browser.setAppUserModelID(id)
}

// setupFirefoxProfile writes userChrome.css and user.js into the Firefox
// profile directory so the browser launches with no navigation toolbar or tab
// strip, matching the clean app-mode appearance that Chrome provides via --app.
// appName, if non-empty, is displayed as a label in the tab strip area via a
// CSS ::before pseudo-element on #TabsToolbar. iconPath, if non-empty, is the
// path to a .ico file; a PNG image is extracted from it and shown to the left
// of the label.
func setupFirefoxProfile(dir, appName, iconPath string) error {
	chromeDir := filepath.Join(dir, "chrome")
	if err := os.MkdirAll(chromeDir, 0755); err != nil {
		return err
	}
	// Hide nav/bookmarks/menu bars. #TabsToolbar is kept (not hidden) because on
	// Windows it hosts the min/max/close buttons and window drag region.
	css := "#nav-bar { display: none !important; }\n" +
		"#PersonalToolbar { display: none !important; }\n" +
		"#toolbar-menubar { display: none !important; }\n" +
		// Hide tab scrollbox contents; keep #TabsToolbar for window controls.
		"#tabbrowser-arrowscrollbox { display: none !important; }\n" +
		".tabbrowser-tab { display: none !important; }\n" +
		"#new-tab-button { display: none !important; }\n" +
		".tabs-newtab-button { display: none !important; }\n" +
		"toolbarbutton[command=\"cmd_newNavigatorTab\"] { display: none !important; }\n" +
		"#alltabs-button { display: none !important; }\n" +
		"#firefox-view-button { display: none !important; }\n" +
		// toolbarseparator is toolbar-only (not menus), safe to hide globally.
		"toolbarseparator { display: none !important; }\n" +
		"toolbarspring { display: none !important; }\n" +
		// Collapse #tabbrowser-tabs via max-width/overflow instead of display:none;
		// display:none breaks Firefox's internal tab-switching state (gray content area).
		// Inline borders/padding also need zeroing - max-width:0+overflow:hidden won't suppress them.
		"#tabbrowser-tabs { flex: none !important; -moz-box-flex: 0 !important; " +
		"max-width: 0 !important; min-width: 0 !important; " +
		"max-height: 0 !important; overflow: hidden !important; " +
		"border: none !important; border-inline-start: none !important; " +
		"padding: 0 !important; padding-inline-start: 0 !important; " +
		"margin: 0 !important; margin-inline-start: 0 !important; }\n" +
		// XUL splitters reserve space even with display:none; width:0 is required too.
		"#vertical-pinned-tabs-splitter { display: none !important; " +
		"width: 0 !important; min-width: 0 !important; }\n" +
		".titlebar-placeholder { display: none !important; }\n" +
		".titlebar-spacer { display: none !important; }\n" +
		// Sidebar: both pre-131 panel and 131+ revamp launcher. Splitters need width:0 too.
		"#sidebar-main { display: none !important; width: 0 !important; min-width: 0 !important; }\n" +
		"#sidebar-box { display: none !important; width: 0 !important; min-width: 0 !important; }\n" +
		"#sidebar-splitter, .sidebar-splitter { display: none !important; width: 0 !important; min-width: 0 !important; }\n" +
		"#browser > splitter { display: none !important; width: 0 !important; min-width: 0 !important; }\n" +
		"#sidebar-button { display: none !important; }\n" +
		// Disable new-tab/new-window shortcuts; command-attr selectors cover CustomizableUI re-insertions.
		"#key_newNavigatorTab { display: none !important; }\n" +
		"#key_newNavigatorTabNoEvent { display: none !important; }\n" +
		"#key_newNavigatorWindow { display: none !important; }\n" +
		"key[command=\"cmd_newNavigatorTab\"] { display: none !important; }\n" +
		"key[command=\"cmd_newNavigatorTabNoEvent\"] { display: none !important; }\n" +
		"key[command=\"cmd_newNavigatorWindow\"] { display: none !important; }\n" +
		// Suppress toolbar/tab-strip context menus.
		"#toolbar-context-menu { display: none !important; }\n" +
		"#tabContextMenu { display: none !important; }\n" +
		// Page context menu: hide bookmark-star and AI chatbot (plus adjacent separators).
		"#context-bookmarkpage { display: none !important; }\n" +
		"#context-ask-chat { display: none !important; }\n" +
		"menuseparator:has(+ #context-ask-chat) { display: none !important; }\n" +
		"#context-ask-chat + menuseparator { display: none !important; }\n"

	// #TabsToolbar: CSS flex so ::before flex:1 is honoured (XUL box ignores it on generated content).
	// min-height:0 prevents --tabstrip-min-height (44px) from leaving a gap above caption buttons.
	css += "#TabsToolbar { display: flex !important; align-items: center !important; min-height: 0 !important; }\n" +
		".toolbar-items { display: none !important; }\n"

	if appName != "" {
		// Escape for CSS string literal.
		escapedName := strings.ReplaceAll(appName, `\`, `\\`)
		escapedName = strings.ReplaceAll(escapedName, `"`, `\"`)

		// Extract smallest PNG from .ico; background-image allows explicit sizing unlike content:url().
		iconCSS := ""
		paddingStart := "8px"
		if iconPath != "" {
			if png := extractSmallPNGFromICO(iconPath); png != nil {
				iconFile := filepath.Join(chromeDir, "app-icon.png")
				if os.WriteFile(iconFile, png, 0644) == nil {
					// 8px gap + 16px icon + 6px gap = 30px total padding-start.
					iconCSS = "background-image: url(\"app-icon.png\"); " +
						"background-size: 16px 16px; " +
						"background-repeat: no-repeat; " +
						"background-position: 8px center; "
					paddingStart = "30px"
				}
			}
		}

		css += "#TabsToolbar::before { content: \"" + escapedName + "\"; " +
			"color: rgba(255,255,255,.85); font-size: 13px; " +
			"flex: 1; -moz-box-flex: 1; align-self: center; " +
			"padding-inline-start: " + paddingStart + "; " +
			iconCSS +
			"-moz-window-dragging: drag; }\n"
	}
	if err := os.WriteFile(filepath.Join(chromeDir, "userChrome.css"), []byte(css), 0644); err != nil {
		return err
	}
	userJS := "user_pref(\"toolkit.legacyUserProfileCustomizations.stylesheets\", true);\n" +
		// Disable the 131+ sidebar revamp; without this it persists even when #sidebar-main is hidden.
		"user_pref(\"sidebar.revamp\", false);\n" +
		"user_pref(\"sidebar.main.tools\", \"\");\n" +
		"user_pref(\"sidebar.verticalTabs\", false);\n" +
		"user_pref(\"sidebar.visibility\", \"hide-sidebar\");\n" +
		"user_pref(\"browser.ml.chat.enabled\", false);\n"
	// Enable Firefox devtools when LORCA_DEVTOOLS is set; lost on every fresh-profile launch.
	if os.Getenv("LORCA_DEVTOOLS") != "" {
		userJS += "user_pref(\"devtools.chrome.enabled\", true);\n" +
			"user_pref(\"devtools.debugger.remote-enabled\", true);\n"
	}
	return os.WriteFile(filepath.Join(dir, "user.js"), []byte(userJS), 0644)
}

// extractSmallPNGFromICO parses an ICO file and returns the raw bytes of the
// smallest PNG-encoded image it contains, preferring 16x16.  Modern .ico files
// embed PNG images directly; older BMP-only ICOs return nil.
func extractSmallPNGFromICO(path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil || len(data) < 6 {
		return nil
	}
	// ICO header: reserved(2) + type(2, must be 1) + count(2)
	if binary.LittleEndian.Uint16(data[0:2]) != 0 || binary.LittleEndian.Uint16(data[2:4]) != 1 {
		return nil
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))

	var best []byte
	bestSize := 0
	// Each ICONDIRENTRY is 16 bytes starting at offset 6.
	for i := 0; i < count; i++ {
		base := 6 + i*16
		if base+16 > len(data) {
			break
		}
		w := int(data[base])   // 0 encodes 256
		h := int(data[base+1]) // 0 encodes 256
		imgSize := int(binary.LittleEndian.Uint32(data[base+8 : base+12]))
		imgOffset := int(binary.LittleEndian.Uint32(data[base+12 : base+16]))
		if imgOffset < 0 || imgSize < 8 || imgOffset+imgSize > len(data) {
			continue
		}
		img := data[imgOffset : imgOffset+imgSize]
		// PNG magic: \x89 P N G \r \n \x1a \n
		if img[0] != 0x89 || img[1] != 'P' || img[2] != 'N' || img[3] != 'G' {
			continue
		}
		size := w
		if h > size {
			size = h
		}
		if size == 0 {
			size = 256
		}
		if size == 16 {
			return img // exact match
		}
		if best == nil || size < bestSize {
			best = img
			bestSize = size
		}
	}
	return best
}
