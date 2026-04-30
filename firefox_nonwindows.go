//go:build !windows

package lorca

import "os"

// killFirefoxProcessTree kills the Firefox launcher process. On Windows, the real
// implementation also kills orphaned child processes left by the launcher stub.
func killFirefoxProcessTree(pid int, state *os.ProcessState) {
	if state == nil || !state.Exited() {
		killProcessTree(pid)
	}
}

func applyFirefoxWindowIcon(_ int, _ string) {
	// applyFirefoxWindowIcon is a no-op on non-Windows platforms.
}

func applyFirefoxWindowAUMID(_ int, _ string) {
	// applyFirefoxWindowAUMID is a no-op on non-Windows platforms.
}
