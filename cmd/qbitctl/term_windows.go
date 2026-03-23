//go:build windows

package main

import (
	"os"
	"syscall"
)

const windowsEnableEchoInput = 0x0004

var (
	windowsKernel32     = syscall.NewLazyDLL("kernel32.dll")
	procSetConsoleModeW = windowsKernel32.NewProc("SetConsoleMode")
)

func disableTerminalEcho(file *os.File) (func() error, error) {
	handle := syscall.Handle(file.Fd())

	var mode uint32
	if err := syscall.GetConsoleMode(handle, &mode); err != nil {
		return nil, err
	}

	restored := mode
	mode &^= windowsEnableEchoInput
	if mode != restored {
		if err := setConsoleMode(handle, mode); err != nil {
			return nil, err
		}
	}

	return func() error {
		if mode == restored {
			return nil
		}
		return setConsoleMode(handle, restored)
	}, nil
}

func setConsoleMode(handle syscall.Handle, mode uint32) error {
	r1, _, err := procSetConsoleModeW.Call(uintptr(handle), uintptr(mode))
	if r1 != 0 {
		return nil
	}
	if err != syscall.Errno(0) {
		return err
	}
	return syscall.EINVAL
}
