package client

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// security returns its OSStatus as an 8-bit process exit code. The low
// byte of errSecInteractionNotAllowed (-25308) is 36.
const macKeychainInteractionNotAllowed = 36

func readMacMasterSecretOverSSH(read func(string, string) (string, error), loginRead func() (string, error)) (string, error) {
	secret, err := read(masterKeyService, masterKeyUser)
	var status interface{ ExitCode() int }
	if !errors.As(err, &status) || status.ExitCode() != macKeychainInteractionNotAllowed {
		return secret, err
	}
	secret, loginErr := loginRead()
	if loginErr != nil {
		return "", fmt.Errorf("%w: could not read the master key in the login session: %w", errMacKeychainUnavailable, err)
	}
	return secret, nil
}

// The LaunchAgent reads the existing item in the same user's GUI login
// session. Its stdout and exit status go directly to a private local socket.
const macKeychainLoginJob = `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>RunAtLoad</key><true/>
<key>ProgramArguments</key><array>
<string>/bin/sh</string><string>-c</string>
<string>{
    /usr/bin/security find-generic-password -s "$1" -a "$2" -w 2&gt;/dev/null
    printf '\000%%s\n' "$?"
} | /usr/bin/nc -U -w 3 "$3"</string>
<string>qbitctl-keychain</string><string>%s</string><string>%s</string><string>%s</string>
</array>
</dict></plist>`

func readMacMasterSecretFromLoginSession() (string, error) {
	uid := os.Geteuid()
	if uid <= 0 {
		return "", errors.New("no current user login session")
	}
	// Keep the Unix socket path short even if TMPDIR is long. MkdirTemp
	// creates a private 0700 directory, and no key material is stored there.
	dir, err := os.MkdirTemp("/tmp", "qbitctl-keychain-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	socketPath := filepath.Join(dir, "reply.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		return "", err
	}
	defer listener.Close()

	label := "com.github.naterator." + filepath.Base(dir)
	plistPath := filepath.Join(dir, "lookup.plist")
	plist := fmt.Sprintf(macKeychainLoginJob, label, masterKeyService, masterKeyUser, socketPath)
	if err := os.WriteFile(plistPath, []byte(plist), 0o600); err != nil {
		return "", err
	}

	domain := fmt.Sprintf("gui/%d", uid)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = exec.CommandContext(cleanup, "/bin/launchctl", "bootout", domain+"/"+label).Run()
	}()
	if err := exec.CommandContext(ctx, "/bin/launchctl", "bootstrap", domain, plistPath).Run(); err != nil {
		return "", fmt.Errorf("could not start the same-user login job: %w", err)
	}
	if err := listener.SetDeadline(deadline); err != nil {
		return "", err
	}
	conn, err := listener.AcceptUnix()
	if err != nil {
		return "", fmt.Errorf("waiting for the login session: %w", err)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(deadline); err != nil {
		return "", err
	}
	return readMacKeychainReply(conn)
}

func readMacKeychainReply(input io.Reader) (string, error) {
	reader := bufio.NewReader(io.LimitReader(input, 8*1024))
	output, err := reader.ReadString(0)
	if err != nil {
		return "", errors.New("incomplete login session Keychain reply")
	}
	status, err := reader.ReadString('\n')
	if err != nil || status != "0\n" {
		return "", errors.New("the login session could not read the Keychain item")
	}
	secret := strings.TrimSpace(output[:len(output)-1])
	// Match go-keyring's decoder so older qbitctl installs keep using the
	// same encryption key when macOS requires the login-session lookup.
	var decoded []byte
	if value, ok := strings.CutPrefix(secret, "go-keyring-encoded:"); ok {
		decoded, err = hex.DecodeString(value)
		secret = string(decoded)
	} else if value, ok := strings.CutPrefix(secret, "go-keyring-base64:"); ok {
		decoded, err = base64.StdEncoding.DecodeString(value)
		secret = string(decoded)
	}
	if err != nil || secret == "" {
		return "", errors.New("the login session returned an invalid Keychain item")
	}
	return secret, nil
}
