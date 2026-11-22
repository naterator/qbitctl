package main

import (
	"os"
	"testing"
)

func TestDisableTerminalEchoRejectsRegularFile(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "not-a-terminal")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	if restore, err := disableTerminalEcho(file); err == nil {
		if restore != nil {
			_ = restore()
		}
		t.Fatal("disableTerminalEcho accepted a regular file")
	}
}
