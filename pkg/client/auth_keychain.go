package client

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func runMacSecurity(input string, args ...string) ([]byte, error) {
	cmd := exec.Command("/usr/bin/security", args...)
	cmd.Stdin = strings.NewReader(input)
	return cmd.Output()
}

func saveMacMasterSecret(secret string, run func(string, ...string) ([]byte, error), read func() (string, error)) (string, error) {
	output, err := run("", "default-keychain", "-d", "user")
	path := strings.TrimSpace(string(output))
	if err != nil || len(path) < 2 || path[0] != '"' || path[len(path)-1] != '"' {
		return "", fmt.Errorf("%w: no default user Keychain; open Keychain Access or set QBITCTL_MASTER_KEY", errMacKeychainUnavailable)
	}
	path = path[1 : len(path)-1]
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\r\n") {
		return "", fmt.Errorf("%w: invalid default user Keychain path", errMacKeychainUnavailable)
	}
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: default user Keychain file could not be opened", errMacKeychainUnavailable)
	}

	// Keep the secret off the process argument list. An explicit Keychain
	// avoids the system's default-Keychain creation dialog. Omit -U so a
	// concurrent invocation cannot replace an existing encryption key.
	quotedPath := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(path)
	command := fmt.Sprintf("add-generic-password -s %s -a %s -w %s \"%s\"\n", masterKeyService, masterKeyUser, secret, quotedPath)
	_, writeErr := run(command, "-i")
	stored, err := read()
	if err == nil && stored != "" {
		return stored, nil
	}
	if writeErr != nil {
		return "", fmt.Errorf("%w: save qbitctl master key: %v", errMacKeychainUnavailable, writeErr)
	}
	return "", fmt.Errorf("%w: could not verify the saved qbitctl master key", errMacKeychainUnavailable)
}
