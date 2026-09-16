//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// enableVirtualTerminal turns on ANSI escape processing on Windows 10+ consoles.
func enableVirtualTerminal() {
	const enableVirtualTerminalProcessing = 0x0004
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getStdHandle := kernel32.NewProc("GetStdHandle")
	getConsoleMode := kernel32.NewProc("GetConsoleMode")
	setConsoleMode := kernel32.NewProc("SetConsoleMode")

	// STD_OUTPUT_HANDLE = -11, STD_ERROR_HANDLE = -12 as uint32 DWORD values.
	handles := []uintptr{0xFFFFFFF5, 0xFFFFFFF4}
	for _, fd := range handles {
		h, _, _ := getStdHandle.Call(fd)
		if h == 0 || h == ^uintptr(0) {
			continue
		}
		var mode uint32
		r, _, _ := getConsoleMode.Call(h, uintptr(unsafe.Pointer(&mode)))
		if r == 0 {
			continue
		}
		mode |= enableVirtualTerminalProcessing
		setConsoleMode.Call(h, uintptr(mode))
	}
}
