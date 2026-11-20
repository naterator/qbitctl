package client

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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
