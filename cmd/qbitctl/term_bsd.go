//go:build darwin || freebsd || netbsd || openbsd

package main

import (
	"os"
	"syscall"
	"unsafe"
)

func disableTerminalEcho(file *os.File) (func() error, error) {
	var state syscall.Termios
	fd := file.Fd()
	if _, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, fd, uintptr(syscall.TIOCGETA), uintptr(unsafe.Pointer(&state)), 0, 0, 0); errno != 0 {
		return nil, errno
	}

	restored := state
	state.Lflag &^= syscall.ECHO
	if _, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, fd, uintptr(syscall.TIOCSETA), uintptr(unsafe.Pointer(&state)), 0, 0, 0); errno != 0 {
		return nil, errno
	}

	return func() error {
		if _, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, fd, uintptr(syscall.TIOCSETA), uintptr(unsafe.Pointer(&restored)), 0, 0, 0); errno != 0 {
			return errno
		}
		return nil
	}, nil
}
