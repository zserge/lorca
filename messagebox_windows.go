//go:build windows
// +build windows

package lorca

import (
	"syscall"
	"unsafe"
)

func messageBox(title, text string) bool {
	user32 := syscall.NewLazyDLL("user32.dll")
	messageBoxW := user32.NewProc("MessageBoxW")
	mbYesNo := 0x00000004
	mbIconQuestion := 0x00000020
	idYes := 6
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	textPtr, _ := syscall.UTF16PtrFromString(text)
	ret, _, _ := messageBoxW.Call(0, uintptr(unsafe.Pointer(textPtr)),
		uintptr(unsafe.Pointer(titlePtr)), uintptr(uint(mbYesNo|mbIconQuestion)))
	return int(ret) == idYes
}
