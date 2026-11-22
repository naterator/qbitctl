//go:build darwin

package client

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestMacMasterSecretKeychain is deliberately opt-in because it talks to the
// developer's login Keychain. It uses the same create-only and read-back path
// as production with a unique service, so the production qbitctl item is
// never touched.
func TestMacMasterSecretKeychain(t *testing.T) {
	if os.Getenv("QBITCTL_TEST_KEYCHAIN") != "1" {
		t.Skip("set QBITCTL_TEST_KEYCHAIN=1 to exercise the login Keychain")
	}
	service := fmt.Sprintf("qbitctl-opt-in-%d", time.Now().UnixNano())
	user := "master-key"
	t.Cleanup(func() {
		_, _ = runMacSecurity("", "delete-generic-password", "-s", service, "-a", user)
	})
	read := func() (string, error) {
		output, err := runMacSecurity("", "find-generic-password", "-s", service, "-a", user, "-w")
		return strings.TrimSpace(string(output)), err
	}

	secret, err := saveMacMasterSecretFor("qbitctl-opt-in-test-secret", service, user, runMacSecurity, read)
	if err != nil {
		if errors.Is(err, errMacKeychainUnavailable) {
			t.Fatalf("Keychain integration is unavailable: %v", err)
		}
		t.Fatal(err)
	}
	if secret == "" {
		t.Fatal("Keychain read-back returned an empty master secret")
	}
}
