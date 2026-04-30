package lorca

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ChromeExecutable returns a string which points to the preferred Chrome
// executable file.
var ChromeExecutable = LocateChrome

// LocateChrome returns a path to the Chrome binary, or an empty string if
// Chrome installation is not found.
func LocateChrome(preferPath string) string {
	// If preferPath is specified and it exists
	if preferPath != "" {
		if _, err := os.Stat(preferPath); err == nil {
			return preferPath
		}
	}

	// If env variable "LORCACHROME" specified and it exists
	if path, ok := os.LookupEnv("LORCACHROME"); ok {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	for _, path := range platformBrowserPaths() {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// FindAllBrowsers returns paths to all browser executables found on the system.
// preferPath is checked first; LORCACHROME env var second; then all platform paths.
// The first entry, if any, is the recommended default (same selection as LocateChrome).
func FindAllBrowsers(preferPath string) []string {
	var result []string
	seen := map[string]bool{}

	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			result = append(result, p)
		}
	}

	if preferPath != "" {
		if _, err := os.Stat(preferPath); err == nil {
			add(preferPath)
		}
	}
	if path, ok := os.LookupEnv("LORCACHROME"); ok {
		if _, err := os.Stat(path); err == nil {
			add(path)
		}
	}
	for _, path := range platformBrowserPaths() {
		if _, err := os.Stat(path); err == nil {
			add(path)
		}
	}
	return result
}

// platformBrowserPaths returns the ordered list of well-known browser paths for
// the current OS.
func platformBrowserPaths() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			//Chrome
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/google-chrome",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			//Brave
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
			//Opera
			"/Applications/Opera.app/Contents/MacOS/Opera",
			//Vivaldi
			"/Applications/Vivaldi.app/Contents/MacOS/Vivaldi",
			//Edge (why)
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			// Firefox
			"/Applications/Firefox.app/Contents/MacOS/firefox",
			"/Applications/Firefox Developer Edition.app/Contents/MacOS/firefox",
		}
	case "windows":
		return []string{
			//Chrome
			os.Getenv("LocalAppData") + "/Google/Chrome/Application/chrome.exe",
			os.Getenv("ProgramFiles") + "/Google/Chrome/Application/chrome.exe",
			os.Getenv("ProgramFiles(x86)") + "/Google/Chrome/Application/chrome.exe",
			os.Getenv("LocalAppData") + "/Chromium/Application/chrome.exe",
			os.Getenv("ProgramFiles") + "/Chromium/Application/chrome.exe",
			os.Getenv("ProgramFiles(x86)") + "/Chromium/Application/chrome.exe",
			//Opera
			os.Getenv("LocalAppData") + "/Programs/Opera/launcher.exe",
			os.Getenv("LocalAppData") + "/Programs/Opera/opera.exe",
			os.Getenv("ProgramFiles") + "/Opera/launcher.exe",
			os.Getenv("ProgramFiles") + "/Opera/opera.exe",
			//Brave
			os.Getenv("LocalAppData") + "/BraveSoftware/Brave-Browser/Application/brave.exe",
			os.Getenv("ProgramFiles") + "/BraveSoftware/Brave-Browser/Application/brave.exe",
			//Vivaldi
			os.Getenv("LocalAppData") + "/Vivaldi/Application/vivaldi.exe",
			os.Getenv("ProgramFiles") + "/Vivaldi/Application/vivaldi.exe",
			//Edge
			os.Getenv("ProgramFiles(x86)") + "/Microsoft/Edge/Application/msedge.exe",
			os.Getenv("ProgramFiles") + "/Microsoft/Edge/Application/msedge.exe",
			// Firefox
			os.Getenv("LocalAppData") + "/Mozilla Firefox/firefox.exe",
			os.Getenv("ProgramFiles") + "/Mozilla Firefox/firefox.exe",
			os.Getenv("ProgramFiles(x86)") + "/Mozilla Firefox/firefox.exe",
		}
	default:
		return []string{
			// Chrome / Chromium
			"/usr/bin/google-chrome-stable",
			"/usr/bin/google-chrome",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/snap/bin/chromium",
			// Opera
			"/usr/bin/opera",
			"/snap/bin/opera",
			// Brave
			"/usr/bin/brave-browser",
			"/usr/bin/brave-browser-stable",
			"/snap/bin/brave",
			// Vivaldi
			"/usr/bin/vivaldi",
			"/usr/bin/vivaldi-stable",
			// Edge
			"/usr/bin/microsoft-edge",
			"/usr/bin/microsoft-edge-stable",
			"/snap/bin/microsoft-edge",
			// Firefox
			"/usr/bin/firefox",
			"/usr/bin/firefox-esr",
			"/snap/bin/firefox",
		}
	}
}

// LocateFirefox returns a path to a Firefox binary, or an empty string if
// none is found. It checks preferPath, then the LORCAFIREFOX env var, then
// well-known platform paths.
func LocateFirefox(preferPath string) string {
	if preferPath != "" {
		if _, err := os.Stat(preferPath); err == nil {
			return preferPath
		}
	}
	if path, ok := os.LookupEnv("LORCAFIREFOX"); ok {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	var paths []string
	switch runtime.GOOS {
	case "darwin":
		paths = []string{
			"/Applications/Firefox.app/Contents/MacOS/firefox",
			"/Applications/Firefox Developer Edition.app/Contents/MacOS/firefox",
		}
	case "windows":
		paths = []string{
			os.Getenv("LocalAppData") + "/Mozilla Firefox/firefox.exe",
			os.Getenv("ProgramFiles") + "/Mozilla Firefox/firefox.exe",
			os.Getenv("ProgramFiles(x86)") + "/Mozilla Firefox/firefox.exe",
		}
	default:
		paths = []string{
			"/usr/bin/firefox",
			"/usr/bin/firefox-esr",
			"/snap/bin/firefox",
		}
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// PromptDownload asks user if he wants to download and install Chrome, and
// opens a download web page if the user agrees.
func PromptDownload() {
	title := "Chrome not found"
	text := "No Chrome/Chromium installation was found. Would you like to download and install it now?"

	// Ask user for confirmation
	if !messageBox(title, text) {
		return
	}

	// Open download page
	url := "https://www.google.com/chrome/"
	switch runtime.GOOS {
	case "linux":
		exec.Command("xdg-open", url).Run()
	case "darwin":
		exec.Command("open", url).Run()
	case "windows":
		r := strings.NewReplacer("&", "^&")
		exec.Command("cmd", "/c", "start", r.Replace(url)).Run()
	}
}
