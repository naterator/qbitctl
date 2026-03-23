//go:build linux

package main

import (
	"os"
	"syscall"
	"unsafe"
)

func disableTerminalEcho(file *os.File) (func() error, error) {
	var state syscall.Termios
	fd := file.Fd()
	if _, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, fd, uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&state)), 0, 0, 0); errno != 0 {
		return nil, errno
	}

	restored := state
	state.Lflag &^= syscall.ECHO
	if _, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, fd, uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&state)), 0, 0, 0); errno != 0 {
		return nil, errno
	}

	return func() error {
		if _, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, fd, uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&restored)), 0, 0, 0); errno != 0 {
			return errno
		}
		return nil
	}, nil
}
