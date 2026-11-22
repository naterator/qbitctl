package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/naterator/qbitctl/internal/qbtest"
	qbt "github.com/naterator/qbitctl/pkg/client"
)

func TestConfigWritePasswordFromStdin(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"first line", "fixture-secret\nextra\n", "fixture-secret"},
		{"CRLF and spaces", "  fixture-secret  \r\n", "  fixture-secret  "},
		{"no newline", "fixture-secret", "fixture-secret"},
		{"maximum length", strings.Repeat("x", maxPasswordBytes) + "\r\n", strings.Repeat("x", maxPasswordBytes)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, seen := recordPasswordLogins(t)
			t.Setenv("QBITCTL_PASSWORD", "fixture-environment")
			path := filepath.Join(t.TempDir(), "config.json")
			out, err := runPasswordCLI(strings.NewReader(tc.input), "--url", server.creds().URL, "config", "write", "--pass-stdin", "--output", path)
			if err != nil || !strings.Contains(out, "Saved config") || strings.Contains(out, tc.want) {
				t.Fatalf("config write did not report a successful, redacted save: %v", err)
			}
			t.Setenv("QBITCTL_PASSWORD", "")
			out, err = runPasswordCLI(nil, "--config", path, "list")
			if err != nil || strings.Contains(out, tc.want) {
				t.Fatalf("saved config did not authenticate without echoing the password: %v", err)
			}
			if len(*seen) != 1 || (*seen)[0] != tc.want {
				t.Fatal("saved password did not round-trip exactly")
			}
		})
	}
}

func TestPasswordStdinRejectsInvalidInput(t *testing.T) {
	server, _ := recordPasswordLogins(t)
	longLine := strings.Repeat("x", maxPasswordBytes+1)
	tests := []struct {
		name, input, want string
		args              []string
		leavesInput       bool
	}{
		{"empty", "", "no password supplied", []string{"list"}, false},
		{"blank first line", "\r\nfixture-secret\n", "no password supplied", []string{"list"}, false},
		{"too long", longLine, "exceeds 4096 bytes", []string{"list"}, false},
		{"conflicting flags", "fixture-secret\n", "mutually exclusive", []string{"config", "write", "-p", "fixture-flag"}, true},
		{"text setup", "fixture-secret\n", "cannot be used with qbitctl config", []string{"config"}, true},
		{"TUI", "fixture-secret\n", "cannot be used with qbitctl tui", []string{"tui"}, true},
		{"bare command", "fixture-secret\n", "cannot be used with qbitctl", nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := strings.NewReader(tc.input)
			out, err := runPasswordCLI(in, append([]string{"--pass-stdin"}, tc.args...)...)
			var exit exitError
			if !errors.As(err, &exit) || exit.code != exitBadArgs || !strings.Contains(out, tc.want) {
				t.Fatalf("expected bad-arguments failure containing %q, got %v", tc.want, err)
			}
			if strings.Contains(out, "fixture-secret") || strings.Contains(out, "fixture-flag") || strings.Contains(out, longLine[:32]) {
				t.Fatal("password appeared in output")
			}
			if tc.leavesInput && in.Len() != len(tc.input) {
				t.Fatal("invalid flag use consumed stdin")
			}
			if server.LoginCalls != 0 {
				t.Fatal("invalid password option attempted to log in")
			}
		})
	}
}

func TestPasswordEnvironmentPrecedence(t *testing.T) {
	server, seen := recordPasswordLogins(t)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := qbt.ExecuteConfigWrite(CLIOptions{URL: "http://old.test", Pass: "fixture-saved"}, path, nil); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, env, want string
		args            []string
	}{
		{"no saved password", "fixture-environment", "fixture-environment", []string{"--url", server.creds().URL, "--user", "admin"}},
		{"changed origin", "fixture-environment", "fixture-environment", []string{"--config", path, "--url", server.creds().URL}},
		{"flag overrides environment", "fixture-environment", "fixture-flag", []string{"--config", path, "-p", "fixture-flag"}},
		{"empty environment", "", "fixture-saved", []string{"--config", path}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("QBITCTL_PASSWORD", tc.env)
			before := len(*seen)
			out, err := runPasswordCLI(nil, append([]string{"list"}, tc.args...)...)
			if err != nil || strings.Contains(out, tc.want) {
				t.Fatalf("list did not authenticate without echoing the password: %v", err)
			}
			if len(*seen) != before+1 || (*seen)[before] != tc.want {
				t.Fatal("login did not use the expected password source")
			}
		})
	}
}

func TestPasswordStdinHelp(t *testing.T) {
	out, err := runPasswordCLI(nil, "--help")
	if err != nil || !strings.Contains(out, "--pass-stdin") || !strings.Contains(out, "QBITCTL_PASSWORD") {
		t.Fatalf("help does not list both password inputs: %v", err)
	}
}

func TestConfigWriteMissingPasswordListsInputOptions(t *testing.T) {
	t.Setenv("QBITCTL_PASSWORD", "")
	out, err := runPasswordCLI(nil, "config", "write")
	var exit exitError
	if !errors.As(err, &exit) || exit.code != exitBadArgs || !strings.Contains(out, "--pass-stdin, QBITCTL_PASSWORD, or --pass") {
		t.Fatalf("missing password did not report the supported inputs: %v", err)
	}
}

func runPasswordCLI(in io.Reader, args ...string) (string, error) {
	cmd := newRootCmd()
	cmd.SetIn(in)
	cmd.SetArgs(args)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()
	return out.String(), err
}

func recordPasswordLogins(t *testing.T) (*qbTestServer, *[]string) {
	t.Helper()
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	t.Setenv("QBITCTL_MASTER_KEY", "fixture-master-key")
	var passwords []string
	restore := qbt.SetHTTPClientFactoryForTesting(func(jar http.CookieJar) *http.Client {
		return &http.Client{
			Jar: jar,
			Transport: qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/api/v2/auth/login" {
					if err := req.ParseForm(); err != nil {
						return nil, err
					}
					passwords = append(passwords, req.Form.Get("password"))
				}
				return server.roundTrip(req), nil
			}),
		}
	})
	t.Cleanup(restore)
	return server, &passwords
}
