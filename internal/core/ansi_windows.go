//go:build windows

package core

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32                        = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode              = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode              = kernel32.NewProc("SetConsoleMode")
	enableVirtualTerminalProcessing = uint32(0x0004)
)

func init() {
	enableANSIOnHandle(os.Stdout.Fd())
	enableANSIOnHandle(os.Stderr.Fd())
}

func enableANSIOnHandle(handle uintptr) {
	var mode uint32
	r, _, _ := procGetConsoleMode.Call(handle, uintptr(unsafe.Pointer(&mode)))
	if r == 0 {
		return
	}
	_, _, _ = procSetConsoleMode.Call(handle, uintptr(mode|enableVirtualTerminalProcessing))
}
