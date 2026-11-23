package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	qbt "github.com/naterator/qbitctl/pkg/client"
)

func TestInvalidOutputAndFieldValuesFailBeforeAuth(t *testing.T) {
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	tests := []struct {
		name string
		args []string
		pass bool
	}{
		{name: "invalid fields", args: []string{"list", "--fields", "nope"}},
		{name: "invalid template", args: []string{"show", fullHash, "--template", "{{.Name"}},
		{name: "conflicting list formats", args: []string{"list", "-j", "-J"}},
		{name: "conflicting show formats", args: []string{"show", fullHash, "-t", "x", "--fields", "name"}},
		{name: "conflicting info formats", args: []string{"info", "-j", "-J"}},
		{name: "conflicting info formats leave stdin unread", args: []string{"info", "--json", "--server-json", "--pass-stdin"}, pass: true},
		{name: "conflicting get formats", args: []string{"get", "name", fullHash, "-j", "-J"}},
		{name: "no header without fields", args: []string{"list", "--no-header"}},
		{name: "invalid list state", args: []string{"list", "--state", "not-a-state"}},
		{name: "raw json cannot use local list state", args: []string{"list", "--server-json", "--state", "queued"}},
		{name: "unknown get field", args: []string{"get", "nmae", fullHash}},
		{name: "unknown set field", args: []string{"set", "nmae", "x", fullHash}},
		{name: "invalid transfer limit", args: []string{"set", "up-limit", "12q", fullHash}},
		{name: "invalid ratio", args: []string{"set", "ratio-limit", "-1", fullHash}},
		{name: "invalid toggle", args: []string{"set", "seqdl", "maybe", fullHash}},
		{name: "partial seed time", args: []string{"set", "seedtime-limit", "30s", fullHash}},
		{name: "stdin remains unread", args: []string{"set", "nmae", "x", fullHash, "--pass-stdin"}, pass: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newQBTestServer(t)
			withQBServerClientFactory(t, server)
			cmd := newRootCmd()
			var input *strings.Reader
			if tt.pass {
				input = strings.NewReader("fixture-secret\nremaining")
				cmd.SetIn(input)
			}
			cmd.SetArgs(tt.args)
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			err := cmd.Execute()
			var coded *qbt.CodedError
			if !errors.As(err, &coded) || coded.Code != qbt.ExitBadArgs {
				t.Fatalf("error = %v, want ExitBadArgs", err)
			}
			if server.LoginCalls != 0 || server.InfoCalls != 0 {
				t.Fatalf("invalid args authenticated: logins=%d info=%d", server.LoginCalls, server.InfoCalls)
			}
			if tt.pass && input.Len() != len("fixture-secret\nremaining") {
				t.Fatalf("invalid args consumed stdin: %d bytes remain", input.Len())
			}
		})
	}
}
