//+build !windows

package lorca

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
)

func messageBox(title, text string) bool {
	if runtime.GOOS == "linux" {
		err := exec.Command("zenity", "--question", "--title", title, "--text", text).Run()
		if err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				return exitError.Sys().(syscall.WaitStatus).ExitStatus() == 0
			}
		}
	} else if runtime.GOOS == "darwin" {
		script := `set T to button returned of ` +
			`(display dialog "%s" with title "%s" buttons {"No", "Yes"} default button "Yes")`
		out, err := exec.Command("osascript", "-e", fmt.Sprintf(script, text, title)).Output()
		if err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				return exitError.Sys().(syscall.WaitStatus).ExitStatus() == 0
			}
		}
		return strings.TrimSpace(string(out)) == "Yes"
	}
	return false
}
