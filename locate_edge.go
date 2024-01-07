package lorca

import (
	"os"
	"runtime"
)

// EdgeExecutable returns a string which points to the preferred Edge
// executable file.
var EdgeExecutable = LocateEdge

// LocateEdge returns a path to the Edge binary, or an empty string if
// Edge installation is not found.
func LocateEdge() string {

	// If env variable "LORCACHROME" specified and it exists
	if path, ok := os.LookupEnv("LORCAEDGE"); ok {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	var paths []string
	switch runtime.GOOS {
	case "darwin":
		paths = []string{
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		}
	case "windows":
		paths = []string{
      os.Getenv("ProgramFiles") + "/Microsoft/Edge/Application/msedge.exe",
			os.Getenv("ProgramFiles(x86)") + "/Microsoft/Edge/Application/msedge.exe",
		}
	default:
		paths = []string{
      "/usr/bin/microsoft-edge-stable",
      "/usr/bin/microsoft-edge",
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

