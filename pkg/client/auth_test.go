package client

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withMockMasterKey(t *testing.T) {
	t.Helper()
	old := os.Getenv("QBITCTL_MASTER_KEY")
	os.Setenv("QBITCTL_MASTER_KEY", "mock-secret-for-testing")
	t.Cleanup(func() {
		os.Setenv("QBITCTL_MASTER_KEY", old)
	})
}

func withWorkingDirectory(t *testing.T, dir string) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore chdir failed: %v", err)
		}
	})
}

func withHomeDirectory(t *testing.T, home string) {
	t.Helper()
	oldHome := os.Getenv("HOME")
	oldXDG := os.Getenv("XDG_CONFIG_HOME")
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("setenv HOME failed: %v", err)
	}
	if err := os.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config")); err != nil {
		t.Fatalf("setenv XDG_CONFIG_HOME failed: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Setenv("HOME", oldHome); err != nil {
			t.Fatalf("restore HOME failed: %v", err)
		}
		if err := os.Setenv("XDG_CONFIG_HOME", oldXDG); err != nil {
			t.Fatalf("restore XDG_CONFIG_HOME failed: %v", err)
		}
	})
}

func TestInitAuthPrefersConfigPathAndAppliesOverrides(t *testing.T) {
	tmp := t.TempDir()
	cwd := filepath.Join(tmp, "cwd")
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("mkdir cwd failed: %v", err)
	}
	withWorkingDirectory(t, cwd)
	withHomeDirectory(t, home)

	configCreds := Credentials{URL: "http://config:8080", User: "config-user", Pass: "config-pass"}
	if err := saveAuthFile(filepath.Join(tmp, "custom-config.json"), configCreds); err != nil {
		t.Fatalf("saveAuthFile config failed: %v", err)
	}
	if err := saveAuthFile(filepath.Join(cwd, defaultAuthFile), Credentials{URL: "http://cwd:8080", User: "cwd-user", Pass: "cwd-pass"}); err != nil {
		t.Fatalf("saveAuthFile cwd failed: %v", err)
	}
	if err := saveAuthFile(filepath.Join(home, ".config", defaultAuthDir, defaultAuthFile), Credentials{URL: "http://home:8080", User: "home-user", Pass: "home-pass"}); err != nil {
		t.Fatalf("saveAuthFile home failed: %v", err)
	}

	got, err := initAuth(Options{
		ConfigPath: filepath.Join(tmp, "custom-config.json"),
		User:       "override-user",
		Pass:       "override-pass",
	}, io.Discard)
	if err != nil {
		t.Fatalf("initAuth returned error: %v", err)
	}
	if got.URL != configCreds.URL {
		t.Fatalf("initAuth URL = %q, want %q", got.URL, configCreds.URL)
	}
	if got.User != "override-user" || got.Pass != "override-pass" {
		t.Fatalf("initAuth overrides = %#v", got)
	}
}

func TestInitAuthFallsBackToHomeBeforeCurrentDirectory(t *testing.T) {
	tmp := t.TempDir()
	cwd := filepath.Join(tmp, "cwd")
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("mkdir cwd failed: %v", err)
	}
	withWorkingDirectory(t, cwd)
	withHomeDirectory(t, home)

	cwdCreds := Credentials{URL: "http://cwd:8080", User: "cwd-user", Pass: "cwd-pass"}
	homeCreds := Credentials{URL: "http://home:8080", User: "home-user", Pass: "home-pass"}
	if err := saveAuthFile(filepath.Join(cwd, defaultAuthFile), cwdCreds); err != nil {
		t.Fatalf("saveAuthFile cwd failed: %v", err)
	}
	if err := saveAuthFile(filepath.Join(home, ".config", defaultAuthDir, defaultAuthFile), homeCreds); err != nil {
		t.Fatalf("saveAuthFile home failed: %v", err)
	}

	got, err := initAuth(Options{}, io.Discard)
	if err != nil {
		t.Fatalf("initAuth returned error: %v", err)
	}
	if got != homeCreds {
		t.Fatalf("initAuth creds = %#v, want %#v", got, homeCreds)
	}
}

func TestInitAuthReturnsErrorWhenNoCredentialsExist(t *testing.T) {
	tmp := t.TempDir()
	withWorkingDirectory(t, tmp)
	withHomeDirectory(t, filepath.Join(tmp, "home"))

	_, err := initAuth(Options{}, io.Discard)
	if err == nil {
		t.Fatal("initAuth unexpectedly succeeded")
	}
	var coded *CodedError
	if !errors.As(err, &coded) || coded.Code != ExitFile {
		t.Fatalf("initAuth err = %v, want CodedError with ExitFile", err)
	}
}

func TestLoadAuthFileRejectsUnsupportedPasswordFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{"url":"http://localhost:8080","user":"admin","password":"0123456789abcdef0123456789abcdef01234567"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config file failed: %v", err)
	}

	_, ok, err := loadAuthFile(path, io.Discard)
	if !ok {
		t.Fatal("loadAuthFile reported missing file")
	}
	if !errors.Is(err, errUnsupportedPasswordFormat) {
		t.Fatalf("loadAuthFile err = %v, want %v", err, errUnsupportedPasswordFormat)
	}
}

func TestLoadAuthFileMigratesPlaintextPasswordToEncryptedConfig(t *testing.T) {
	withMockMasterKey(t)
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{"url":"http://localhost:8080","user":"admin","password":"secret"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config file failed: %v", err)
	}

	creds, ok, err := loadAuthFile(path, io.Discard)
	if err != nil {
		t.Fatalf("loadAuthFile returned error: %v", err)
	}
	if !ok {
		t.Fatal("loadAuthFile reported missing file")
	}
	if creds.URL != "http://localhost:8080" || creds.User != "admin" || creds.Pass != "secret" {
		t.Fatalf("loadAuthFile creds = %#v", creds)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	var disk configFile
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("saved config is not valid JSON: %v", err)
	}
	if !strings.HasPrefix(disk.Password, "enc:v2:") {
		t.Fatalf("password was not migrated to v2 encrypted format: %q", disk.Password)
	}
	if disk.Password == "secret" {
		t.Fatal("password remained in cleartext on disk")
	}
}

func TestLoadAuthFileMigratesV1ToV2(t *testing.T) {
	withMockMasterKey(t)
	path := filepath.Join(t.TempDir(), "config.json")

	// Manual v1 encryption
	block, _ := aes.NewCipher(deriveAuthKeyV1())
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	ciphertext := gcm.Seal(nil, nonce, []byte("secret"), nil)
	v1Value := "enc:v1:" + hex.EncodeToString(nonce) + ":" + hex.EncodeToString(ciphertext)

	content := `{"url":"http://localhost:8080","user":"admin","password":"` + v1Value + `"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config file failed: %v", err)
	}

	got, ok, err := loadAuthFile(path, io.Discard)
	if err != nil {
		t.Fatalf("loadAuthFile returned error: %v", err)
	}
	if !ok {
		t.Fatal("loadAuthFile reported missing file")
	}
	if got.Pass != "secret" {
		t.Fatalf("loadAuthFile decrypted = %q, want secret", got.Pass)
	}

	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `"enc:v2:`) {
		t.Fatalf("config was not migrated to v2: %s", string(raw))
	}
}

func TestLoadAuthFileMissingAndInvalidEncryptedFormats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-config.json")
	creds, ok, err := loadAuthFile(path, io.Discard)
	if err != nil || ok || creds != (Credentials{}) {
		t.Fatalf("missing loadAuthFile = %#v, %v, %v", creds, ok, err)
	}

	invalid := filepath.Join(t.TempDir(), "invalid-config.json")
	content := `{"url":"http://localhost:8080","user":"admin","password":"enc:v1:zz:zz"}`
	if err := os.WriteFile(invalid, []byte(content), 0o600); err != nil {
		t.Fatalf("write invalid config file failed: %v", err)
	}
	_, ok, err = loadAuthFile(invalid, io.Discard)
	if !ok || err == nil {
		t.Fatalf("invalid encrypted loadAuthFile = ok:%v err:%v", ok, err)
	}
}

func TestPromptLineDefaultAndQuit(t *testing.T) {
	got, err := promptLineTo(io.Discard, bufio.NewReader(strings.NewReader("\n")), "Prompt: ", "default")
	if err != nil {
		t.Fatalf("promptLine default returned error: %v", err)
	}
	if got != "default" {
		t.Fatalf("promptLine default = %q, want %q", got, "default")
	}

	_, err = promptLineTo(io.Discard, bufio.NewReader(strings.NewReader("quit\n")), "Prompt: ", "default")
	if err == nil {
		t.Fatal("promptLine quit unexpectedly succeeded")
	}
}

func TestRunInteractiveConfigValidatesURLAndSaves(t *testing.T) {
	withMockMasterKey(t)
	input := strings.Join([]string{
		"localhost:8080\n",
		"https://qb.example.com/base/\n",
		"alice\n",
		"secret\n",
		"\n",
	}, "")

	var output strings.Builder
	var savedPath string
	var savedCreds Credentials
	rc := runInteractiveConfig(
		bufio.NewReader(strings.NewReader(input)),
		&output,
		func(fn func() (string, error)) (string, error) { return fn() },
		filepath.Join(t.TempDir(), "config.json"),
		func(path string, creds Credentials) error {
			savedPath = path
			savedCreds = creds
			return nil
		},
	)
	if rc != ExitOK {
		t.Fatalf("runInteractiveConfig rc = %d, want %d", rc, ExitOK)
	}
	if !strings.Contains(output.String(), "[ERROR] URL must include a scheme and host") {
		t.Fatalf("interactive output = %q", output.String())
	}
	if !strings.HasSuffix(savedPath, "config.json") {
		t.Fatalf("savedPath = %q", savedPath)
	}
	if savedCreds.URL != "https://qb.example.com/base" {
		t.Fatalf("saved URL = %q, want https://qb.example.com/base", savedCreds.URL)
	}
	if savedCreds.User != "alice" || savedCreds.Pass != "secret" {
		t.Fatalf("saved creds = %#v", savedCreds)
	}
}

func TestWarnLoosePermissions(t *testing.T) {
	withMockMasterKey(t)
	path := filepath.Join(t.TempDir(), "config.json")
	creds := Credentials{URL: "http://localhost:8080", User: "admin", Pass: "secret"}
	if err := saveAuthFile(path, creds); err != nil {
		t.Fatalf("saveAuthFile failed: %v", err)
	}

	// With 0600, no warning expected
	var out strings.Builder
	_, _, err := loadAuthFile(path, &out)
	if err != nil {
		t.Fatalf("loadAuthFile err = %v", err)
	}
	if strings.Contains(out.String(), "[WARN]") {
		t.Fatalf("unexpected warning for 0600 perms: %q", out.String())
	}

	// Loosen permissions to 0644
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod failed: %v", err)
	}
	out.Reset()
	_, _, err = loadAuthFile(path, &out)
	if err != nil {
		t.Fatalf("loadAuthFile err = %v", err)
	}
	if !strings.Contains(out.String(), "[WARN]") || !strings.Contains(out.String(), "0644") {
		t.Fatalf("expected permission warning, got: %q", out.String())
	}
}

func TestEncryptDecryptPassword(t *testing.T) {
	withMockMasterKey(t)
	encoded, err := encryptPassword("secret")
	if err != nil {
		t.Fatalf("encryptPassword returned error: %v", err)
	}

	if !strings.HasPrefix(encoded, "enc:v2:") {
		t.Fatalf("encoded = %q, want enc:v2: prefix", encoded)
	}

	decoded, err := decryptPassword(encoded)
	if err != nil {
		t.Fatalf("decryptPassword returned error: %v", err)
	}

	if decoded != "secret" {
		t.Fatalf("decryptPassword = %q, want %q", decoded, "secret")
	}
}

func TestSaveAndLoadAuthFile(t *testing.T) {
	withMockMasterKey(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	creds := Credentials{
		URL:  "http://localhost:8080",
		User: "admin",
		Pass: "secret",
	}

	if err := saveAuthFile(path, creds); err != nil {
		t.Fatalf("saveAuthFile returned error: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	var disk configFile
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("saved config is not valid JSON: %v", err)
	}
	if disk.URL != creds.URL || disk.User != creds.User || disk.Password == "" {
		t.Fatalf("saved disk config = %#v", disk)
	}

	loaded, ok, err := loadAuthFile(path, io.Discard)
	if err != nil {
		t.Fatalf("loadAuthFile returned error: %v", err)
	}
	if !ok {
		t.Fatal("loadAuthFile reported missing file")
	}
	if loaded.URL != creds.URL || loaded.User != creds.User || loaded.Pass != creds.Pass {
		t.Fatalf("loadAuthFile = %#v, want %#v", loaded, creds)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config file perms = %v, want 0600", info.Mode().Perm())
	}
}
