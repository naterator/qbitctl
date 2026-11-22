package client

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestSaveMacMasterSecretRequiresDefaultKeychain(t *testing.T) {
	_, err := saveMacMasterSecret("generated-key", func(input string, args ...string) ([]byte, error) {
		if input != "" || !slices.Equal(args, []string{"default-keychain", "-d", "user"}) {
			t.Fatalf("unexpected security command %v", args)
		}
		return nil, errors.New("no default Keychain")
	}, func() (string, error) {
		t.Fatal("unexpected master-key lookup after preflight failed")
		return "", nil
	})
	if !errors.Is(err, errMacKeychainUnavailable) {
		t.Fatalf("missing default Keychain error = %v", err)
	}
}

func TestSaveMacMasterSecretTargetsDefaultAndVerifiesReadback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test login.keychain-db")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name  string
		saved string
	}{
		{name: "new key", saved: "generated-key"},
		{name: "concurrent key", saved: "existing-key"},
		{name: "write did not persist"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var attemptedWrite bool
			got, err := saveMacMasterSecret("generated-key", func(input string, args ...string) ([]byte, error) {
				if slices.Equal(args, []string{"default-keychain", "-d", "user"}) {
					return []byte("    \"" + path + "\"\n"), nil
				}
				want := "add-generic-password -s qbitctl -a master-key -w generated-key \"" + strings.ReplaceAll(path, `\`, `\\`) + "\""
				if !slices.Equal(args, []string{"-i"}) || strings.TrimSpace(input) != want {
					t.Fatal("expected a create-only write to the explicit Keychain path via stdin")
				}
				attemptedWrite = true
				if tt.saved == "existing-key" {
					return nil, errors.New("duplicate item")
				}
				return nil, nil
			}, func() (string, error) {
				if !attemptedWrite {
					t.Fatal("checked the saved key before attempting the write")
				}
				return tt.saved, nil
			})
			if tt.saved == "" {
				if !errors.Is(err, errMacKeychainUnavailable) {
					t.Fatalf("unverified write error = %v", err)
				}
			} else if err != nil || got != tt.saved {
				t.Fatalf("did not use the verified key: %v", err)
			}
		})
	}
}

type testSecurityExitCode int

func (e testSecurityExitCode) Error() string { return "synthetic security command failure" }
func (e testSecurityExitCode) ExitCode() int { return int(e) }

func TestReadMacMasterSecretOverSSH(t *testing.T) {
	const loginSecret = "synthetic-login-key"
	denied := testSecurityExitCode(36)
	backendErr := errors.New("synthetic Keychain backend failure")
	loginErr := errors.New("synthetic failure with " + loginSecret)

	for _, test := range []struct {
		name         string
		directSecret string
		directErr    error
		loginErr     error
		wantSecret   string
		wantErr      error
		wantRetry    bool
	}{
		{name: "direct success", directSecret: "direct-key", wantSecret: "direct-key"},
		{name: "missing item", directErr: keyring.ErrNotFound, wantErr: keyring.ErrNotFound},
		{name: "unrelated error", directErr: backendErr, wantErr: backendErr},
		{name: "other exit code", directErr: testSecurityExitCode(35), wantErr: testSecurityExitCode(35)},
		{name: "interaction unavailable", directErr: denied, wantSecret: loginSecret, wantRetry: true},
		{name: "login session unavailable", directErr: denied, loginErr: loginErr, wantErr: denied, wantRetry: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			didRetry := false
			got, err := readMacMasterSecretOverSSH(func(service, account string) (string, error) {
				if service != masterKeyService || account != masterKeyUser {
					t.Fatalf("read unexpected Keychain item %q/%q", service, account)
				}
				return test.directSecret, test.directErr
			}, func() (string, error) {
				if didRetry {
					t.Fatal("retried the login session more than once")
				}
				didRetry = true
				if test.loginErr != nil {
					return "", test.loginErr
				}
				return loginSecret, nil
			})
			if got != test.wantSecret || !errors.Is(err, test.wantErr) || didRetry != test.wantRetry {
				t.Fatalf("got key = %t, error = %v, retried = %t", got != "", err, didRetry)
			}
			if test.loginErr != nil {
				if !errors.Is(err, errMacKeychainUnavailable) || strings.Contains(err.Error(), loginSecret) {
					t.Fatal("failed login session should report a generic Keychain error")
				}
			}
		})
	}
}

func TestReadMacKeychainReply(t *testing.T) {
	const secret = "synthetic-keychain-secret"
	hexSecret := "go-keyring-encoded:" + hex.EncodeToString([]byte(secret))
	base64Secret := "go-keyring-base64:" + base64.StdEncoding.EncodeToString([]byte(secret))

	tests := []struct {
		name      string
		reply     string
		want      string
		wantError bool
	}{
		{name: "raw secret", reply: secret + "\x000\n", want: secret},
		{name: "legacy hex secret", reply: hexSecret + "\x000\n", want: secret},
		{name: "legacy base64 secret", reply: base64Secret + "\x000\n", want: secret},
		{name: "nonzero status", reply: secret + "\x0036\n", wantError: true},
		{name: "invalid legacy encoding", reply: "go-keyring-base64:!\x000\n", wantError: true},
		{name: "empty secret", reply: "\x000\n", wantError: true},
		{name: "missing status", reply: secret + "\x00\n", wantError: true},
		{name: "missing separator", reply: secret + "\n", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := readMacKeychainReply(strings.NewReader(test.reply))
			if test.wantError {
				if err == nil {
					t.Fatalf("readMacKeychainReply unexpectedly succeeded with %q", got)
				}
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("readMacKeychainReply error leaked secret: %v", err)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("readMacKeychainReply = %q, %v; want %q", got, err, test.want)
			}
		})
	}

	t.Run("bounded input", func(t *testing.T) {
		tooLarge := strings.Repeat("x", 1<<20) + "\x000\n"
		if got, err := readMacKeychainReply(strings.NewReader(tooLarge)); err == nil {
			t.Fatalf("readMacKeychainReply accepted oversized response %q", got)
		}
	})
}
