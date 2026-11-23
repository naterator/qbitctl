package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	qbt "github.com/naterator/qbitctl/pkg/client"
	"github.com/spf13/cobra"
)

func TestParseBatchLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []string
		err  bool
	}{
		{name: "plain", line: "info --json", want: []string{"info", "--json"}},
		{name: "quotes and escapes", line: `search "two words" 'and more' two\ words`, want: []string{"search", "two words", "and more", "two words"}},
		{name: "comment", line: "list # ignored", want: []string{"list"}},
		{name: "literal hash", line: "search a#b", want: []string{"search", "a#b"}},
		{name: "empty quoted argument", line: `set category "" HASH`, want: []string{"set", "category", "", "HASH"}},
		{name: "unterminated quote", line: `search "missing`, err: true},
		{name: "trailing escape", line: `search trailing\`, err: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseBatchLine(tt.line)
			if (err != nil) != tt.err {
				t.Fatalf("parseBatchLine err = %v, want error=%v", err, tt.err)
			}
			if strings.Join(got, "\x00") != strings.Join(tt.want, "\x00") {
				t.Fatalf("parseBatchLine = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestBatchValidatesParsedCommandsAndConnectionFlags(t *testing.T) {
	for _, tt := range []struct {
		line string
		want string
	}{
		{line: "ver"},
		{line: "qbitctl.exe version"},
		{line: "-u http://example.test info", want: "--url"},
		{line: "info -Uadmin", want: "--user"},
		{line: "info --pass=secret", want: "--pass"},
		{line: "--http-timeout 90s info", want: "--http-timeout"},
		{line: "batch", want: "not supported"},
		{line: "tui", want: "not supported"},
		{line: "config write", want: "not supported"},
		{line: "update", want: "not supported"},
	} {
		t.Run(tt.line, func(t *testing.T) {
			var stderr bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(io.Discard)
			cmd.SetErr(&stderr)
			err := executeBatchLine(cmd, &App{Ctx: t.Context()}, tt.line)
			if (err != nil) != (tt.want != "") || !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf("batch line returned %v, stderr %q; want %q", err, &stderr, tt.want)
			}
		})
	}
}

func TestBatchReusesOneSessionAndContinuesWithOriginalExitCode(t *testing.T) {
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	var paths []string
	restore := qbt.SetHTTPClientFactoryForTesting(func(jar http.CookieJar) *http.Client {
		return &http.Client{Jar: jar, Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			paths = append(paths, req.URL.Path)
			return server.roundTrip(req), nil
		})}
	})
	t.Cleanup(restore)
	server.StatusQueue["/api/v2/transfer/info"] = []int{500, 200}
	creds := server.creds()
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--url", creds.URL, "--user", creds.User, "--pass", creds.Pass, "batch", "--continue-on-error"})
	cmd.SetIn(strings.NewReader("info --server-json\ninfo --server-json\n"))
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	var result exitError
	err := cmd.ExecuteContext(t.Context())
	if !errors.As(err, &result) || result.code != exitFetchFail || !strings.Contains(stderr.String(), "batch line 1") {
		t.Fatalf("batch failed with %v, output %q, stderr %q", err, &stdout, &stderr)
	}
	want := []string{"/api/v2/auth/login", "/api/v2/app/version", "/api/v2/app/webapiVersion",
		"/api/v2/transfer/info", "/api/v2/transfer/info", "/api/v2/auth/logout"}
	if !slices.Equal(paths, want) || stdout.Len() == 0 {
		t.Fatalf("batch requests = %v, output = %q", paths, &stdout)
	}
}

func TestAuthenticatedRunReusesBatchApp(t *testing.T) {
	app := &App{}
	var out bytes.Buffer
	cmd := &cobra.Command{Use: "test"}
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(withBatchApp(context.Background(), app))
	called := false
	handler := authenticatedRun(&CLIOptions{}, func(got *App, _ []string) error {
		called = got == app
		_, _ = got.Stdout.Write([]byte("ok"))
		return nil
	})
	if err := handler(cmd, nil); err != nil {
		t.Fatalf("authenticatedRun err = %v", err)
	}
	if !called || out.String() != "ok" {
		t.Fatalf("batch app reuse called=%v output=%q", called, out.String())
	}
}
