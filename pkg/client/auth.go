package client

import (
	"bufio"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/naterator/qbitctl/internal/configfile"
	"github.com/zalando/go-keyring"
)

const (
	defaultAuthDir   = "qbitctl"
	defaultAuthFile  = "config.json"
	defaultConfigURL = "http://localhost:8080"
	masterKeyService = "qbitctl"
	masterKeyUser    = "master-key"
)

var errUnsupportedPasswordFormat = errors.New("config file uses an unsupported password format")
var errReenterEncryptedPassword = fmt.Errorf("%w; run qbitctl config to re-enter credentials", errUnsupportedPasswordFormat)
var errMacKeychainUnavailable = errors.New("macOS Keychain is unavailable")
var errMasterKeyUnavailable = errors.New("qbitctl master key is unavailable")

// Keep keyring access behind package-local functions so credential loading can
// be tested without depending on a particular desktop keyring backend. The
// production values are the go-keyring implementations.
var (
	masterSecretGet = keyring.Get
	masterSecretSet = keyring.Set
)

// CodedError is an error that carries an exit code for CLI use.
type CodedError struct {
	Code    int
	Message string
}

func (e *CodedError) Error() string { return e.Message }

func codedErrf(code int, format string, args ...any) *CodedError {
	return &CodedError{Code: code, Message: fmt.Sprintf(format, args...)}
}

type configFile struct {
	URL      string `json:"url"`
	User     string `json:"user"`
	Password string `json:"password"`
}

func xdgConfigHome() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config")
}

func defaultAuthPath() string {
	base := xdgConfigHome()
	if base == "" {
		return filepath.Join(".", defaultAuthFile)
	}
	return filepath.Join(base, defaultAuthDir, defaultAuthFile)
}

func getMasterSecret() (string, error) {
	if s := os.Getenv("QBITCTL_MASTER_KEY"); s != "" {
		return s, nil
	}
	if runtime.GOOS == "darwin" && os.Getenv("SSH_CONNECTION") != "" {
		return readMacMasterSecretOverSSH(masterSecretGet, readMacMasterSecretFromLoginSession)
	}
	return masterSecretGet(masterKeyService, masterKeyUser)
}

func getOrCreateMasterKey() ([]byte, string, error) {
	secret, err := getMasterSecret()
	if err == nil {
		sum := sha256.Sum256([]byte(secret))
		return sum[:], "v2", nil
	}
	if !errors.Is(err, keyring.ErrNotFound) {
		unavailable := errMasterKeyUnavailable
		if runtime.GOOS == "darwin" {
			unavailable = errMacKeychainUnavailable
		}
		return nil, "", fmt.Errorf("%w: read qbitctl master key: %v", unavailable, err)
	}

	// Try to create one if it's missing
	newSecret := make([]byte, 32)
	if _, err := rand.Read(newSecret); err != nil {
		return nil, "", err
	}
	secret = hex.EncodeToString(newSecret)
	if runtime.GOOS == "darwin" {
		secret, err = saveMacMasterSecret(secret, runMacSecurity, getMasterSecret)
		if err != nil {
			return nil, "", err
		}
	} else if err := masterSecretSet(masterKeyService, masterKeyUser, secret); err != nil {
		// New saves must never silently fall back to a predictable encryption key.
		return nil, "", fmt.Errorf("%w: store qbitctl master key: %v", errMasterKeyUnavailable, err)
	}

	sum := sha256.Sum256([]byte(secret))
	return sum[:], "v2", nil
}

func encryptPassword(password string) (string, error) {
	key, version, err := getOrCreateMasterKey()
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nil, nonce, []byte(password), nil)
	return "enc:" + version + ":" + hex.EncodeToString(nonce) + ":" + hex.EncodeToString(ciphertext), nil
}

func decryptPassword(value string) (string, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 4 || parts[0] != "enc" {
		return "", fmt.Errorf("invalid encrypted password format")
	}

	if parts[1] != "v2" {
		return "", errReenterEncryptedPassword
	}

	secret, err := getMasterSecret()
	if err != nil {
		return "", fmt.Errorf("failed to retrieve master key from keyring: %w. Set QBITCTL_MASTER_KEY if keyring is unavailable", err)
	}
	key := sha256.Sum256([]byte(secret))

	nonce, err := hex.DecodeString(parts[2])
	if err != nil {
		return "", fmt.Errorf("decode encrypted password nonce: %w", err)
	}

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(nonce) != gcm.NonceSize() {
		return "", fmt.Errorf("invalid encrypted password nonce length: got %d, want %d", len(nonce), gcm.NonceSize())
	}

	ciphertext, err := hex.DecodeString(parts[3])
	if err != nil {
		return "", fmt.Errorf("decode encrypted password ciphertext: %w", err)
	}
	if len(ciphertext) < gcm.Overhead() {
		return "", fmt.Errorf("encrypted password ciphertext is too short: got %d bytes, want at least %d", len(ciphertext), gcm.Overhead())
	}

	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plain), nil
}

func saveAuthFile(path string, creds Credentials) error {
	if path == "" {
		return fmt.Errorf("invalid path")
	}

	encodedPass, err := encryptPassword(creds.Pass)
	if err != nil {
		return err
	}

	return configfile.Update(path, func(object configfile.Object) (bool, error) {
		urlValue, err := json.Marshal(creds.URL)
		if err != nil {
			return false, err
		}
		userValue, err := json.Marshal(creds.User)
		if err != nil {
			return false, err
		}
		passwordValue, err := json.Marshal(encodedPass)
		if err != nil {
			return false, err
		}
		setConfigValue(object, "url", urlValue)
		setConfigValue(object, "user", userValue)
		setConfigValue(object, "password", passwordValue)
		return true, nil
	})
}

func setConfigValue(object configfile.Object, key string, value json.RawMessage) {
	for existing := range object {
		if strings.EqualFold(existing, key) {
			delete(object, existing)
		}
	}
	object[key] = value
}

func configValue(object configfile.Object, key string) (json.RawMessage, bool) {
	if value, ok := object[key]; ok {
		return value, true
	}
	for existing, value := range object {
		if strings.EqualFold(existing, key) {
			return value, true
		}
	}
	return nil, false
}

func migratePassword(path, previousPassword, encodedPassword string) error {
	encoded, err := json.Marshal(encodedPassword)
	if err != nil {
		return err
	}
	return configfile.Update(path, func(object configfile.Object) (bool, error) {
		current, ok := configValue(object, "password")
		if !ok {
			return false, nil
		}
		var currentPassword string
		if err := json.Unmarshal(current, &currentPassword); err != nil {
			return false, fmt.Errorf("decode current password during migration: %w", err)
		}
		// Another writer may have replaced the credentials after this process
		// read them. Preserve that newer value and let the next login migrate it
		// if needed.
		if currentPassword != previousPassword {
			return false, nil
		}
		setConfigValue(object, "password", encoded)
		return true, nil
	})
}

func warnLoosePermissions(path string, infoOut io.Writer) {
	// Windows reports synthetic Unix mode bits rather than the file's ACL.
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		fmt.Fprintf(infoOut, "[WARN] Config file %s has permissions %04o; recommended 0600. Fix with: chmod 600 %s\n", path, perm, path)
	}
}

func loadAuthFile(path string, infoOut io.Writer) (Credentials, bool, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Credentials{}, false, nil
		}
		return Credentials{}, false, err
	}

	if infoOut != nil {
		warnLoosePermissions(path, infoOut)
	}

	object, err := configfile.Read(path)
	if err != nil {
		return Credentials{}, true, err
	}
	diskConfig, err := decodeConfigFile(object)
	if err != nil {
		return Credentials{}, true, err
	}

	var creds Credentials
	creds.URL = diskConfig.URL
	creds.User = diskConfig.User

	switch value := diskConfig.Password; {
	case strings.HasPrefix(value, "enc:v2:"):
		password, err := decryptPassword(value)
		if err != nil {
			return Credentials{}, true, err
		}
		creds.Pass = password
	case strings.HasPrefix(value, "enc:"):
		return Credentials{}, true, errReenterEncryptedPassword
	case looksLikeOpaqueCiphertext(value):
		return Credentials{}, true, errUnsupportedPasswordFormat
	default:
		creds.Pass = value
		if value != "" {
			encoded, encodeErr := encryptPassword(value)
			if encodeErr != nil && !isMasterKeyUnavailable(encodeErr) {
				return Credentials{}, true, fmt.Errorf("rewrite plaintext config file: %w", encodeErr)
			}
			if encodeErr == nil {
				if migrateErr := migratePassword(path, value, encoded); migrateErr != nil {
					return Credentials{}, true, fmt.Errorf("rewrite plaintext config file: %w", migrateErr)
				}
				if infoOut != nil {
					fmt.Fprintf(infoOut, "[INFO] Migrated plaintext password in config file: %s\n", path)
				}
			} else if infoOut != nil {
				fmt.Fprintf(infoOut, "[WARN] Kept plaintext credentials in %s; set QBITCTL_MASTER_KEY or enable the keyring to encrypt them.\n", path)
			}
		}
	}

	return creds, true, nil
}

func isMasterKeyUnavailable(err error) bool {
	return errors.Is(err, errMasterKeyUnavailable) || errors.Is(err, errMacKeychainUnavailable)
}

func decodeConfigFile(object configfile.Object) (configFile, error) {
	var diskConfig configFile
	for key, target := range map[string]*string{
		"url":      &diskConfig.URL,
		"user":     &diskConfig.User,
		"password": &diskConfig.Password,
	} {
		value, ok := configValue(object, key)
		if !ok {
			continue
		}
		if err := json.Unmarshal(value, target); err != nil {
			return configFile{}, fmt.Errorf("decode config field %q: %w", key, err)
		}
	}
	return diskConfig, nil
}

func canAttemptLogin(creds Credentials) bool {
	return creds.User != "" && creds.Pass != "" && creds.URL != ""
}

func defaultAuthSearchPaths() []string {
	homePath := defaultAuthPath()
	if homePath == defaultAuthFile {
		return []string{homePath}
	}
	return []string{homePath, defaultAuthFile}
}

func initAuth(opts Options, infoOut io.Writer) (Credentials, error) {
	creds, _, err := initAuthWithPath(opts, infoOut)
	return creds, err
}

func initAuthWithPath(opts Options, infoOut io.Writer) (Credentials, string, error) {
	cli := Credentials{
		URL:  opts.URL,
		User: opts.User,
		Pass: opts.Pass,
	}
	configPath := opts.ConfigPath
	var creds Credentials
	loaded := false
	selectedPath := ""

	if configPath != "" {
		fileInfo, err := os.Stat(configPath)
		if err != nil || fileInfo.IsDir() {
			return Credentials{}, configPath, codedErrf(ExitBadArgs, "Config file '%s' does not exist or is not readable", configPath)
		}

		loadedCreds, ok, err := loadAuthFile(configPath, infoOut)
		if err != nil {
			if errors.Is(err, errUnsupportedPasswordFormat) {
				return Credentials{}, configPath, codedErrf(ExitFile, "%v. Run qbitctl config to rewrite %s", err, configPath)
			}
			return Credentials{}, configPath, codedErrf(ExitFile, "Failed to load config file '%s': %v", configPath, err)
		}
		if ok {
			creds = loadedCreds
			loaded = true
			selectedPath = configPath
		}
	}

	if !loaded {
		for _, path := range defaultAuthSearchPaths() {
			loadedCreds, ok, err := loadAuthFile(path, infoOut)
			if err != nil {
				if errors.Is(err, errUnsupportedPasswordFormat) {
					return Credentials{}, path, codedErrf(ExitFile, "%v. Run qbitctl config to rewrite %s", err, path)
				}
				return Credentials{}, path, codedErrf(ExitFile, "Failed to load %s: %v", path, err)
			}
			if ok {
				creds = loadedCreds
				loaded = true
				selectedPath = path
				break
			}
		}
	}

	// A saved password is scoped to the server origin it was entered for. Do
	// not silently send it to a different host or scheme when a caller only
	// overrides --url; require an explicit password for that transition.
	if loaded && cli.URL != "" && cli.Pass == "" && creds.Pass != "" && !sameCredentialOrigin(creds.URL, cli.URL) {
		return Credentials{}, selectedPath, codedErrf(ExitBadArgs, "Password required when changing server origin")
	}

	if cli.User != "" {
		creds.User = cli.User
	}
	if cli.Pass != "" {
		creds.Pass = cli.Pass
	}
	if cli.URL != "" {
		creds.URL = cli.URL
	}

	if creds.URL != "" {
		normalizedURL, err := ValidateConfigURL(creds.URL)
		if err != nil {
			code := ExitFile
			if cli.URL != "" {
				code = ExitBadArgs
			}
			return Credentials{}, selectedPath, codedErrf(code, "%v", err)
		}
		creds.URL = normalizedURL
	}

	if !canAttemptLogin(creds) {
		return Credentials{}, selectedPath, codedErrf(ExitFile, "No credentials supplied")
	}

	if selectedPath == "" {
		selectedPath = defaultAuthPath()
	}
	return creds, selectedPath, nil
}

// sameCredentialOrigin compares the parts of a URL that scope a Web UI
// session. Default HTTP(S) ports are equivalent to their explicit forms, while
// paths remain intentionally out of the comparison for reverse proxies.
func sameCredentialOrigin(left, right string) bool {
	leftURL, leftErr := url.Parse(strings.TrimSpace(left))
	rightURL, rightErr := url.Parse(strings.TrimSpace(right))
	if leftErr != nil || rightErr != nil || leftURL.Scheme == "" || leftURL.Host == "" || rightURL.Scheme == "" || rightURL.Host == "" {
		return false
	}
	if !strings.EqualFold(leftURL.Scheme, rightURL.Scheme) || !strings.EqualFold(leftURL.Hostname(), rightURL.Hostname()) {
		return false
	}
	return credentialPort(leftURL) == credentialPort(rightURL)
}

func credentialPort(value *url.URL) string {
	port := value.Port()
	if port == "" {
		switch strings.ToLower(value.Scheme) {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}
	if numeric, err := strconv.Atoi(port); err == nil {
		return strconv.Itoa(numeric)
	}
	return port
}

// NewClient authenticates using a background context. Call Logout when finished.
func NewClient(opts *Options) (*App, error) {
	return NewClientContext(context.Background(), opts)
}

// NewClientContext authenticates and uses ctx for the app's requests.
// Call Logout when finished with the returned app.
func NewClientContext(ctx context.Context, opts *Options) (*App, error) {
	return NewClientWithOutput(ctx, opts, nil, nil)
}

// NewClientWithOutput authenticates with caller-provided output writers. A nil
// writer uses the corresponding standard stream. Call Logout when finished.
func NewClientWithOutput(ctx context.Context, opts *Options, stdout, stderr io.Writer) (*App, error) {
	if opts == nil {
		return nil, codedErrf(ExitBadArgs, "Client options are required")
	}
	app := &App{Ctx: ctx, Stdout: stdout, Stderr: stderr}
	newAppDefaults(app)

	creds, configPath, err := initAuthWithPath(*opts, app.Stderr)
	if err != nil {
		return nil, err
	}

	app.creds = creds
	app.configPath = configPath
	// A malformed optional display preference must not prevent authentication.
	app.DataUnits = DataUnitsHuman
	if configured, readErr := ReadDataUnits(configPath); readErr == nil {
		app.DataUnits = configured
	}
	if opts.DataUnits != "" {
		configured, parseErr := ParseDataUnits(string(opts.DataUnits))
		if parseErr != nil {
			return nil, codedErrf(ExitBadArgs, "%v", parseErr)
		}
		app.DataUnits = configured
	}
	app.client = newClient(creds, app.Stderr)

	if err := app.client.loginContext(app.context()); err != nil {
		app.Logout()
		return nil, codedErrWrapf(ExitLoginFail, err, "Login failed: %v", err)
	}
	if err := app.CheckCompatibility(); err != nil {
		app.Logout()
		return nil, err
	}

	return app, nil
}

// Logout attempts to release the app's session, even if its context is canceled.
func (a *App) Logout() {
	a.client.logout()
}

func ExecuteConfigWrite(opts Options, outputPath string, infoOut io.Writer) error {
	if opts.Pass == "" {
		return codedErrf(ExitBadArgs, "Password required for non-interactive config write. Use --pass-stdin, QBITCTL_PASSWORD, or --pass")
	}

	urlValue := opts.URL
	if urlValue == "" {
		urlValue = defaultConfigURL
	}
	normalizedURL, err := ValidateConfigURL(urlValue)
	if err != nil {
		return codedErrf(ExitBadArgs, "%v", err)
	}

	userValue := opts.User
	if userValue == "" {
		userValue = "admin"
	}

	if outputPath == "" {
		if opts.ConfigPath != "" {
			outputPath = opts.ConfigPath
		} else {
			outputPath = defaultAuthPath()
		}
	}

	if err := saveAuthFile(outputPath, Credentials{
		URL:  normalizedURL,
		User: userValue,
		Pass: opts.Pass,
	}); err != nil {
		return codedErrf(ExitFile, "Failed to save config file: %v", err)
	}

	if infoOut != nil {
		fmt.Fprintf(infoOut, "[INFO] Saved config to %s\n", outputPath)
	}
	return nil
}

// ValidateConfigURL validates and normalizes a qBittorrent Web UI base URL.
// It rejects URL components that would interfere with API endpoint joining.
func ValidateConfigURL(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", errors.New("invalid server URL")
	}
	if parsed.User != nil {
		return "", errors.New("URL must not include embedded credentials")
	}
	if parsed.Scheme == "" || parsed.Hostname() == "" {
		return "", fmt.Errorf("URL must include a scheme and host, for example %s", defaultConfigURL)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("URL scheme must be http or https")
	}
	if parsed.RawQuery != "" || parsed.ForceQuery {
		return "", errors.New("URL must not include a query")
	}
	if parsed.Fragment != "" || strings.Contains(trimmed, "#") {
		return "", errors.New("URL must not include a fragment")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

// readPromptLine preserves io.EOF alongside any final unterminated input.
func readPromptLine(out io.Writer, reader *bufio.Reader, prompt string) (string, error) {
	fmt.Fprint(out, prompt)
	text, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	text = strings.TrimRight(text, "\r\n")
	if text == "quit" {
		return "", errors.New("quit")
	}
	return text, err
}

func promptLineTo(out io.Writer, reader *bufio.Reader, prompt, defaultValue string) (string, error) {
	value, err := readPromptLine(out, reader, prompt)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if value == "" {
		return defaultValue, nil
	}
	return value, nil
}

func promptValidatedLine(out io.Writer, reader *bufio.Reader, prompt, defaultValue string, validate func(string) (string, error)) (string, error) {
	const maxPromptAttempts = 5
	var previousValidationErr error
	for attempt := 1; ; attempt++ {
		value, err := readPromptLine(out, reader, prompt)
		ended := errors.Is(err, io.EOF)
		if err != nil && !ended {
			return "", err
		}
		if value == "" {
			if ended && previousValidationErr != nil {
				return "", fmt.Errorf("%w: %v", ErrSetupInputEnded, previousValidationErr)
			}
			value = defaultValue
		}
		normalized, err := validate(value)
		if err == nil {
			return normalized, nil
		}
		previousValidationErr = err
		if ended {
			return "", fmt.Errorf("%w: %v", ErrSetupInputEnded, err)
		}
		if attempt >= maxPromptAttempts {
			return "", fmt.Errorf("%w: invalid value after %d attempts: %v", ErrSetupInputEnded, maxPromptAttempts, err)
		}
		fmt.Fprintf(out, "[ERROR] %v\n", err)
	}
}

// HiddenInputFunc is a function that wraps password input to hide terminal echo.
// It receives a prompt function and should call it, returning the result.
// A nil value means no echo hiding (the prompt function is called directly).
type HiddenInputFunc func(fn func() (string, error)) (string, error)

// InteractiveConfig validates and saves a profile using the text prompts.
func InteractiveConfig(in io.Reader, out io.Writer, hiddenInput HiddenInputFunc) int {
	path, err := InteractiveConfigContext(context.Background(), in, out, hiddenInput, "")
	if err != nil {
		fmt.Fprintf(out, "[ERROR] %v\n", err)
		var coded *CodedError
		if errors.As(err, &coded) {
			return coded.Code
		}
		return ExitBadArgs
	}
	fmt.Fprintf(out, "Saved config to %s\n", path)
	return ExitOK
}
