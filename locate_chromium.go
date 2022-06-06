package lorca

import (
	"os"
	"runtime"
)

// ChromeExecutable returns a string which points to the preferred Chromium
// executable file.
var ChromiumExecutable = LocateChromium

// LocateChromium returns a path to the Chromium binary, or an empty string if
// the Chromium installation is not found.
func LocateChromium() string {

	// If env variable "LORCACHROME" specified and it exists
	if path, ok := os.LookupEnv("LORCACHROMIUM"); ok {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	var paths []string
	switch runtime.GOOS {
	case "darwin":
		paths = []string{
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
		}
	case "windows":
		paths = []string{
			os.Getenv("LocalAppData") + "/Chromium/Application/chrome.exe",
			os.Getenv("ProgramFiles") + "/Chromium/Application/chrome.exe",
			os.Getenv("ProgramFiles(x86)") + "/Chromium/Application/chrome.exe",
		}
	default:
		paths = []string{
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/snap/bin/chromium",
		}
	}

	for _, path := range paths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		return path
	}
	return ""
}

