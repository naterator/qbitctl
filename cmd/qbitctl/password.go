package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

const (
	stdinModeAnnotation = "qbitctl/stdin"
	stdinInteractive    = "interactive"
	maxPasswordBytes    = 4096
)

func resolvePassword(cmd *cobra.Command, opts *CLIOptions, passStdin bool) error {
	if cmd.Flags().Changed("pass") {
		// Cobra checks flag groups after the pre-run hook. Reject the conflict
		// here so it cannot cause a read from stdin.
		if cmd.Flags().Changed("pass-stdin") {
			return errors.New("--pass and --pass-stdin are mutually exclusive")
		}
		return nil
	}
	if !passStdin {
		opts.Pass = os.Getenv("QBITCTL_PASSWORD")
		return nil
	}
	if cmd.Annotations[stdinModeAnnotation] == stdinInteractive {
		return fmt.Errorf("--pass-stdin cannot be used with %s, which reads from the terminal", cmd.CommandPath())
	}
	stdin := cmd.InOrStdin()
	if tuiTerminal(stdin) {
		return errors.New("--pass-stdin expects the password on piped input")
	}
	password, err := readPasswordLine(stdin)
	if err != nil {
		return err
	}
	opts.Pass = password
	return nil
}

func readPasswordLine(in io.Reader) (string, error) {
	// Allow a full-length password followed by CRLF and stop at the first line.
	const lineBytes = maxPasswordBytes + 2
	line, err := bufio.NewReaderSize(io.LimitReader(in, lineBytes), lineBytes).ReadSlice('\n')
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return "", errors.New("could not read the password from stdin")
	}
	password := strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r")
	if len(password) > maxPasswordBytes {
		return "", fmt.Errorf("password read from stdin exceeds %d bytes", maxPasswordBytes)
	}
	if password == "" {
		return "", errors.New("no password supplied on stdin")
	}
	return password, nil
}
