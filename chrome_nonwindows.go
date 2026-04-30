//go:build !windows

package lorca

import "os"

func killProcessTree(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
