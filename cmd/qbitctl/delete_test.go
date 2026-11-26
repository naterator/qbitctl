package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestDeleteBatchAndRedirectedDoNotPrompt(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	for _, batch := range []bool{false, true} {
		name := "redirected CLI"
		if batch {
			name = "batch"
		}
		t.Run(name, func(t *testing.T) {
			server := newQBTestServer(t)
			server.Torrents = []TorrentInfo{{Name: "Example", Hash: hash}}
			withQBServerClientFactory(t, server)
			cmd := newRootCmd()
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			args := []string{"--url", server.creds().URL, "--user", "admin", "--pass", "secret"}
			if batch {
				cmd.SetArgs(append(args, "batch"))
				cmd.SetIn(strings.NewReader("delete " + hash + "\nversion\n"))
			} else {
				cmd.SetArgs(append(args, "delete", hash))
				cmd.SetIn(strings.NewReader(""))
			}
			if err := cmd.Execute(); err != nil {
				t.Fatalf("execute: %v, stderr: %s", err, &stderr)
			}
			wantOutput := ""
			if batch {
				wantOutput = "qbitctl " + qbitctlVersion + "\n"
			}
			if stdout.String() != wantOutput || stderr.Len() != 0 {
				t.Fatalf("stdout = %q, want %q; stderr = %q", &stdout, wantOutput, &stderr)
			}
			forms := server.Forms["/api/v2/torrents/delete"]
			if len(forms) != 1 || forms[0].Get("hashes") != hash || forms[0].Get("deleteFiles") != "true" {
				t.Fatalf("delete forms = %v", forms)
			}
			if server.LoginCalls != 1 {
				t.Fatalf("login calls = %d, want 1", server.LoginCalls)
			}
		})
	}
}
