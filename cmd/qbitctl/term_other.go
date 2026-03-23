//go:build !linux && !darwin && !freebsd && !netbsd && !openbsd && !windows

package main

import (
	"fmt"
	"os"
)

func disableTerminalEcho(file *os.File) (func() error, error) {
	return nil, fmt.Errorf("terminal echo control is unavailable on this platform")
}
