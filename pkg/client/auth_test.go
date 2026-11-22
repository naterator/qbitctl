package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func withMockMasterKey(t *testing.T) {
	t.Helper()
	t.Setenv("QBITCTL_MASTER_KEY", "mock-secret-for-testing")
}

func withUnavailableMasterKey(t *testing.T) {
	t.Helper()
	t.Setenv("QBITCTL_MASTER_KEY", "")
	oldGet, oldSet := masterSecretGet, masterSecretSet
	masterSecretGet = func(string, string) (string, error) {
		return "", errors.New("test keyring unavailable")
	}
	masterSecretSet = func(string, string, string) error {
		t.Error("attempted to replace the master key after a keyring read error")
		return errors.New("test keyring unavailable")
	}
	t.Cleanup(func() {
		masterSecretGet, masterSecretSet = oldGet, oldSet
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
	oldUserProfile := os.Getenv("USERPROFILE")
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("setenv HOME failed: %v", err)
	}
	if err := os.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config")); err != nil {
		t.Fatalf("setenv XDG_CONFIG_HOME failed: %v", err)
	}
	if err := os.Setenv("USERPROFILE", home); err != nil {
		t.Fatalf("setenv USERPROFILE failed: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Setenv("HOME", oldHome); err != nil {
			t.Fatalf("restore HOME failed: %v", err)
		}
		if err := os.Setenv("XDG_CONFIG_HOME", oldXDG); err != nil {
			t.Fatalf("restore XDG_CONFIG_HOME failed: %v", err)
		}
		if err := os.Setenv("USERPROFILE", oldUserProfile); err != nil {
			t.Fatalf("restore USERPROFILE failed: %v", err)
		}
	})
}

func TestInitAuthPrefersConfigPathAndAppliesOverrides(t *testing.T) {
	withMockMasterKey(t)
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
	withMockMasterKey(t)
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

func TestAuthWritesPreserveUIAndUnknownConfigFields(t *testing.T) {
	withMockMasterKey(t)
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{"url":"http://localhost:8080","user":"admin","password":"secret","ui":{"queue":{"sort":"progress-desc"}},"future":{"enabled":true}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config file failed: %v", err)
	}

	if err := saveAuthFile(path, Credentials{URL: "http://new-host:8080", User: "new-user", Pass: "new-secret"}); err != nil {
		t.Fatalf("saveAuthFile returned error: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config file failed: %v", err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("saved config is not valid JSON: %v", err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, object["ui"]); err != nil {
		t.Fatalf("decode saved ui settings: %v", err)
	}
	if compact.String() != `{"queue":{"sort":"progress-desc"}}` {
		t.Fatalf("saveAuthFile replaced ui settings: %s", object["ui"])
	}
	compact.Reset()
	if err := json.Compact(&compact, object["future"]); err != nil {
		t.Fatalf("decode saved unknown fields: %v", err)
	}
	if compact.String() != `{"enabled":true}` {
		t.Fatalf("saveAuthFile dropped unknown fields: %s", object["future"])
	}

	if _, _, err := loadAuthFile(path, io.Discard); err != nil {
		t.Fatalf("loadAuthFile returned error: %v", err)
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migrated config failed: %v", err)
	}
	var migrated map[string]json.RawMessage
	if err := json.Unmarshal(raw, &migrated); err != nil {
		t.Fatalf("migrated config is not valid JSON: %v", err)
	}
	compact.Reset()
	if err := json.Compact(&compact, migrated["ui"]); err != nil {
		t.Fatalf("decode migrated ui settings: %v", err)
	}
	if compact.String() != `{"queue":{"sort":"progress-desc"}}` {
		t.Fatalf("password migration replaced ui settings: %s", migrated["ui"])
	}
	compact.Reset()
	if err := json.Compact(&compact, migrated["future"]); err != nil {
		t.Fatalf("decode migrated unknown fields: %v", err)
	}
	if compact.String() != `{"enabled":true}` {
		t.Fatalf("password migration dropped unknown fields: %s", migrated["future"])
	}
}

func TestLoadAuthFileAcceptsMixedCaseAuthFieldsAndMigratesPassword(t *testing.T) {
	withMockMasterKey(t)
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{"URL":"http://localhost:8080","User":"admin","Password":"secret","ui":{"queue":{"sort":"name-asc"}}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config file failed: %v", err)
	}
	got, ok, err := loadAuthFile(path, io.Discard)
	if err != nil || !ok {
		t.Fatalf("loadAuthFile = %#v, %v, %v", got, ok, err)
	}
	if got != (Credentials{URL: "http://localhost:8080", User: "admin", Pass: "secret"}) {
		t.Fatalf("mixed-case credentials = %#v", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migrated config failed: %v", err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("migrated config is not valid JSON: %v", err)
	}
	// Password migration is intentionally conditional and only rewrites the
	// password field. Rewriting URL/user here could clobber newer credentials
	// written after the initial read. The decoder still accepts encoding/json's
	// case-insensitive legacy field spellings, and the migrated password uses
	// the canonical key.
	if _, ok := object["password"]; !ok {
		t.Fatalf("migrated config missing canonical password: %s", raw)
	}
	if _, ok := object["Password"]; ok {
		t.Fatalf("migrated config retained mixed-case Password: %s", raw)
	}
	for _, key := range []string{"URL", "User"} {
		if _, ok := object[key]; !ok {
			t.Fatalf("password-only migration unexpectedly rewrote %s: %s", key, raw)
		}
	}
	for _, key := range []string{"url", "user"} {
		if _, ok := object[key]; ok {
			t.Fatalf("password-only migration added duplicate canonical %s: %s", key, raw)
		}
	}
}

func TestPasswordMigrationDoesNotReplaceNewerPassword(t *testing.T) {
	withMockMasterKey(t)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"url":"http://localhost:8080","user":"admin","password":"newer"}`), 0o600); err != nil {
		t.Fatalf("write config file failed: %v", err)
	}
	encoded, err := encryptPassword("old")
	if err != nil {
		t.Fatalf("encryptPassword returned error: %v", err)
	}
	if err := migratePassword(path, "old", encoded); err != nil {
		t.Fatalf("migratePassword returned error: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config file failed: %v", err)
	}
	if strings.Contains(string(raw), encoded) || !strings.Contains(string(raw), `"password":"newer"`) {
		t.Fatalf("migration replaced newer password: %s", raw)
	}
}

func TestInitAuthWithPathReportsSelectedOrDefaultConfigPath(t *testing.T) {
	withMockMasterKey(t)
	tmp := t.TempDir()
	withWorkingDirectory(t, tmp)
	withHomeDirectory(t, filepath.Join(tmp, "home"))

	path := filepath.Join(tmp, "selected.json")
	creds := Credentials{URL: "http://selected:8080", User: "user", Pass: "secret"}
	if err := saveAuthFile(path, creds); err != nil {
		t.Fatalf("saveAuthFile returned error: %v", err)
	}
	got, selected, err := initAuthWithPath(Options{ConfigPath: path}, io.Discard)
	if err != nil || got != creds || selected != path {
		t.Fatalf("explicit config selection = %#v, %q, %v", got, selected, err)
	}

	cli := Credentials{URL: "http://cli:8080", User: "cli", Pass: "secret"}
	got, selected, err = initAuthWithPath(Options{URL: cli.URL, User: cli.User, Pass: cli.Pass}, io.Discard)
	if err != nil || got != cli {
		t.Fatalf("CLI config selection = %#v, %q, %v", got, selected, err)
	}
	if selected != defaultAuthPath() {
		t.Fatalf("CLI default config path = %q, want %q", selected, defaultAuthPath())
	}
}

func TestInitAuthWithPathRequiresPasswordForChangedOrigin(t *testing.T) {
	withMockMasterKey(t)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := saveAuthFile(path, Credentials{URL: "http://old.example:8080/qbt", User: "admin", Pass: "secret"}); err != nil {
		t.Fatalf("saveAuthFile returned error: %v", err)
	}

	got, selected, err := initAuthWithPath(Options{ConfigPath: path, URL: "https://new.example:8080/qbt", User: "admin"}, io.Discard)
	if err == nil {
		t.Fatalf("initAuthWithPath unexpectedly succeeded with credentials %#v", got)
	}
	var coded *CodedError
	if !errors.As(err, &coded) || coded.Code != ExitBadArgs {
		t.Fatalf("changed-origin error = %v, want CodedError with ExitBadArgs", err)
	}
	if !strings.Contains(err.Error(), "Password required") {
		t.Fatalf("changed-origin error = %v, want password guidance", err)
	}
	if selected != path {
		t.Fatalf("selected path = %q, want %q", selected, path)
	}
}

func TestInitAuthWithPathReusesPasswordOnlyForSameOrigin(t *testing.T) {
	withMockMasterKey(t)
	path := filepath.Join(t.TempDir(), "config.json")
	saved := Credentials{URL: "http://old.example/qbt", User: "admin", Pass: "secret"}
	if err := saveAuthFile(path, saved); err != nil {
		t.Fatalf("saveAuthFile returned error: %v", err)
	}

	got, _, err := initAuthWithPath(Options{ConfigPath: path, URL: "http://old.example:80/other", User: "override"}, io.Discard)
	if err != nil {
		t.Fatalf("same-origin override returned error: %v", err)
	}
	if got.URL != "http://old.example:80/other" || got.User != "override" || got.Pass != saved.Pass {
		t.Fatalf("same-origin override credentials = %#v", got)
	}

	got, _, err = initAuthWithPath(Options{ConfigPath: path, URL: "https://new.example:8443/qbt", User: "override", Pass: "new-secret"}, io.Discard)
	if err != nil {
		t.Fatalf("explicit-password changed-origin override returned error: %v", err)
	}
	if got.URL != "https://new.example:8443/qbt" || got.User != "override" || got.Pass != "new-secret" {
		t.Fatalf("explicit-password override credentials = %#v", got)
	}
}

func TestInitAuthValidatesDirectURLOverrides(t *testing.T) {
	tmp := t.TempDir()
	withWorkingDirectory(t, tmp)
	withHomeDirectory(t, filepath.Join(tmp, "home"))
	for _, value := range []string{
		"http://:8080",
		"http://qbt.test/?foo=bar",
		"http://qbt.test/?",
		"http://qbt.test/#fragment",
		"http://qbt.test/#",
		"http://admin:secret@qbt.test",
	} {
		t.Run(value, func(t *testing.T) {
			_, err := initAuth(Options{URL: value, User: "admin", Pass: "secret"}, io.Discard)
			if err == nil {
				t.Fatal("initAuth unexpectedly accepted invalid URL")
			}
			var coded *CodedError
			if !errors.As(err, &coded) || coded.Code != ExitBadArgs {
				t.Fatalf("initAuth error = %v, want ExitBadArgs", err)
			}
		})
	}
}

func TestValidateConfigURLPreservesProxyPath(t *testing.T) {
	got, err := ValidateConfigURL("https://qbt.example.test/qbt/base///")
	if err != nil {
		t.Fatalf("ValidateConfigURL returned error: %v", err)
	}
	if got != "https://qbt.example.test/qbt/base" {
		t.Fatalf("ValidateConfigURL = %q, want proxy path preserved", got)
	}
}

func TestClientConstructorsRejectNilOptions(t *testing.T) {
	constructors := []struct {
		name string
		call func() error
	}{
		{name: "background", call: func() error {
			_, err := NewClient(nil)
			return err
		}},
		{name: "context", call: func() error {
			_, err := NewClientContext(context.Background(), nil)
			return err
		}},
		{name: "output", call: func() error {
			_, err := NewClientWithOutput(context.Background(), nil, io.Discard, io.Discard)
			return err
		}},
	}
	for _, constructor := range constructors {
		t.Run(constructor.name, func(t *testing.T) {
			err := constructor.call()
			if err == nil {
				t.Fatal("constructor unexpectedly succeeded")
			}
			var coded *CodedError
			if !errors.As(err, &coded) || coded.Code != ExitBadArgs {
				t.Fatalf("constructor error = %v, want ExitBadArgs", err)
			}
		})
	}
}

func TestLoadAuthFileRejectsUnsupportedEncryptedPasswordWithoutDecrypting(t *testing.T) {
	withUnavailableMasterKey(t)
	path := filepath.Join(t.TempDir(), "config.json")
	original := `{"url":"http://localhost:8080","user":"admin","password":"enc:v1:not-decrypted:payload"}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("write config file failed: %v", err)
	}

	_, ok, err := loadAuthFile(path, io.Discard)
	if !ok {
		t.Fatal("loadAuthFile reported missing file")
	}
	if !errors.Is(err, errUnsupportedPasswordFormat) {
		t.Fatalf("loadAuthFile err = %v, want unsupported-password error", err)
	}
	if !strings.Contains(err.Error(), "qbitctl config") {
		t.Fatalf("loadAuthFile err = %v, want reconfiguration guidance", err)
	}

	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read config file failed: %v", readErr)
	}
	if string(raw) != original {
		t.Fatalf("legacy config was rewritten: %s", raw)
	}
}

func TestLoadAuthFileMissingAndInvalidEncryptedFormats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-config.json")
	creds, ok, err := loadAuthFile(path, io.Discard)
	if err != nil || ok || creds != (Credentials{}) {
		t.Fatalf("missing loadAuthFile = %#v, %v, %v", creds, ok, err)
	}

	invalid := filepath.Join(t.TempDir(), "invalid-config.json")
	content := `{"url":"http://localhost:8080","user":"admin","password":"enc:v3:zz:zz"}`
	if err := os.WriteFile(invalid, []byte(content), 0o600); err != nil {
		t.Fatalf("write invalid config file failed: %v", err)
	}
	_, ok, err = loadAuthFile(invalid, io.Discard)
	if !ok || err == nil {
		t.Fatalf("invalid encrypted loadAuthFile = ok:%v err:%v", ok, err)
	}
}

func TestDecryptPasswordRejectsMalformedNonceWithoutPanicking(t *testing.T) {
	withMockMasterKey(t)
	for _, value := range []string{
		"enc:v2::00000000000000000000000000000000",
		"enc:v2:00:00000000000000000000000000000000",
		"enc:v2:00000000000000000000000000000000:00000000000000000000000000000000",
	} {
		t.Run(value, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("decryptPassword panicked for malformed nonce: %v", recovered)
				}
			}()
			if _, err := decryptPassword(value); err == nil || !strings.Contains(err.Error(), "nonce") {
				t.Fatalf("decryptPassword(%q) error = %v, want nonce error", value, err)
			}
		})
	}
}

func TestLoadAuthFileReadsPlaintextCredentialsWithoutKeyringAndDoesNotRewrite(t *testing.T) {
	withUnavailableMasterKey(t)
	path := filepath.Join(t.TempDir(), "config.json")
	original := `{"url":"http://localhost:8080","user":"admin","password":"secret"}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("write config file failed: %v", err)
	}

	var info strings.Builder
	got, ok, err := loadAuthFile(path, &info)
	if err != nil {
		t.Fatalf("loadAuthFile returned error: %v", err)
	}
	if !ok || got != (Credentials{URL: "http://localhost:8080", User: "admin", Pass: "secret"}) {
		t.Fatalf("loadAuthFile = %#v, %v", got, ok)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config file failed: %v", err)
	}
	if string(after) != original {
		t.Fatalf("plaintext config was rewritten without a keyring: %s", after)
	}
	if !strings.Contains(info.String(), "QBITCTL_MASTER_KEY") {
		t.Fatalf("migration hint = %q, want QBITCTL_MASTER_KEY guidance", info.String())
	}
}

func TestEncryptPasswordRequiresPrivateMasterKeyWhenKeyringUnavailable(t *testing.T) {
	withUnavailableMasterKey(t)
	encoded, err := encryptPassword("secret")
	if err == nil {
		t.Fatalf("encryptPassword unexpectedly succeeded with value %q", encoded)
	}
	if !errors.Is(err, errMasterKeyUnavailable) && !errors.Is(err, errMacKeychainUnavailable) {
		t.Fatalf("encryptPassword error = %v, want unavailable-key error", err)
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

func TestReadPromptLineReportsEnded(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantValue string
		wantEnded bool
	}{
		{name: "empty EOF", input: "", wantEnded: true},
		{name: "final line", input: "x", wantValue: "x", wantEnded: true},
		{name: "newline", input: "x\n", wantValue: "x", wantEnded: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, err := readPromptLine(io.Discard, bufio.NewReader(strings.NewReader(test.input)), "Prompt: ")
			ended := errors.Is(err, io.EOF)
			if err != nil && !ended {
				t.Fatalf("readPromptLine returned error: %v", err)
			}
			if value != test.wantValue || ended != test.wantEnded {
				t.Fatalf("readPromptLine = (%q, %t), want (%q, %t)", value, ended, test.wantValue, test.wantEnded)
			}
		})
	}
}

func TestPromptValidatedLineStopsAtEOFAfterInvalidEntry(t *testing.T) {
	var output strings.Builder
	_, err := promptValidatedLine(&output, bufio.NewReader(strings.NewReader("ftp://x\n")), "URL [ftp://x]: ", "ftp://x", ValidateConfigURL)
	if !errors.Is(err, ErrSetupInputEnded) {
		t.Fatalf("promptValidatedLine error = %v, want ErrSetupInputEnded", err)
	}
	if count := strings.Count(output.String(), "[ERROR]"); count != 1 {
		t.Fatalf("validation error count = %d, want one: %q", count, output.String())
	}
}

func TestPromptValidatedLineLimitsInvalidAttempts(t *testing.T) {
	var output strings.Builder
	_, err := promptValidatedLine(&output, bufio.NewReader(strings.NewReader(strings.Repeat("bad\n", 6))), "URL [bad]: ", "bad", ValidateConfigURL)
	if !errors.Is(err, ErrSetupInputEnded) {
		t.Fatalf("promptValidatedLine error = %v, want ErrSetupInputEnded", err)
	}
	if count := strings.Count(output.String(), "[ERROR]"); count != 4 {
		t.Fatalf("validation error count = %d, want four before the fifth bounded attempt: %q", count, output.String())
	}
}

func TestInteractiveConfigValidatesURLAndSaves(t *testing.T) {
	withMockMasterKey(t)
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	creds := server.creds()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "config.json")
	input := strings.Join([]string{
		"localhost:8080\n",
		creds.URL + "/\n",
		creds.User + "\n",
		creds.Pass + "\n",
		path + "\n",
	}, "")

	var output strings.Builder
	rc := InteractiveConfig(strings.NewReader(input), &output, nil)
	if rc != ExitOK {
		t.Fatalf("InteractiveConfig rc = %d, want %d. Output: %s", rc, ExitOK, output.String())
	}
	if !strings.Contains(output.String(), "[ERROR] URL must include a scheme and host") {
		t.Fatalf("interactive output = %q", output.String())
	}
	saved, ok, err := loadAuthFile(path, io.Discard)
	if err != nil || !ok || saved != creds || server.LoginCalls != 1 {
		t.Fatalf("saved valid credentials = %t, lookup success = %t, login calls = %d, error = %v", saved == creds, ok, server.LoginCalls, err)
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
	if runtime.GOOS == "windows" {
		return
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
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("config file perms = %v, want 0600", info.Mode().Perm())
	}
}
