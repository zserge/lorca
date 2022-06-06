package lorca

import (
	"os"
	"runtime"
)

// GoogleChromeExecutable returns a string which points to the preferred
// Google Chrome executable file.
var GoogleChromeExecutable = LocateGoogleChrome

// LocateGoogleChrome returns a path to the Google Chrome binary, or an empty
// string if the Google Chrome installation is not found.
func LocateGoogleChrome() string {

	// If env variable "LORCACHROME" specified and it exists
	if path, ok := os.LookupEnv("LORCAGOOGLECHROME"); ok {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	var paths []string
	switch runtime.GOOS {
	case "darwin":
		paths = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/google-chrome",
		}
	case "windows":
		paths = []string{
			os.Getenv("LocalAppData") + "/Google/Chrome/Application/chrome.exe",
			os.Getenv("ProgramFiles") + "/Google/Chrome/Application/chrome.exe",
			os.Getenv("ProgramFiles(x86)") + "/Google/Chrome/Application/chrome.exe",
		}
	default:
		paths = []string{
			"/usr/bin/google-chrome-stable",
			"/usr/bin/google-chrome",
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
