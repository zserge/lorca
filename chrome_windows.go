//go:build windows

package lorca

import (
	"os/exec"
	"strconv"
)

func killProcessTree(pid int) error {
	return exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid)).Run()
}
