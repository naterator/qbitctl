package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileSetupStopsAtEOF(t *testing.T) {
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	t.Setenv("QBITCTL_PASSWORD", "")

	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte(`{"url":"localhost:9999","user":"admin","password":""}`), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, input, message string
	}{
		{"invalid saved URL", "", "password required"},
		{"invalid entry", "ftp://x\n", "setup input ended before a valid value was entered"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newRootCmd()
			root.SetArgs([]string{"--config", path, "profile", "setup"})
			root.SetIn(&setupInput{reader: strings.NewReader(tc.input), readsLeft: 8})
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)

			err := root.Execute()
			var exit exitError
			if !errors.As(err, &exit) || exit.code != exitBadArgs || !strings.Contains(stderr.String(), tc.message) {
				t.Fatalf("config returned %v with stderr %q, want exit %d and %q", err, stderr.String(), exitBadArgs, tc.message)
			}
			if strings.Count(stderr.String(), "[ERROR]") > 2 || stderr.Len() > 1024 {
				t.Fatalf("unexpected prompt output: %q / %q", stdout.String(), stderr.String())
			}
			if server.LoginCalls != 0 {
				t.Fatalf("login calls = %d, want zero", server.LoginCalls)
			}
		})
	}
}

// setupInput keeps a regression that repeatedly reads EOF from hanging the test.
type setupInput struct {
	reader    io.Reader
	readsLeft int
}

func (in *setupInput) Read(p []byte) (int, error) {
	if in.readsLeft == 0 {
		return 0, errors.New("config kept reading from exhausted input")
	}
	in.readsLeft--
	return in.reader.Read(p)
}
