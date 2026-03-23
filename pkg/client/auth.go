package client

import (
	"bufio"
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
	"strings"

	"github.com/zalando/go-keyring"
)

const (
	defaultAuthDir   = "qbitctl"
	defaultAuthFile  = "config.json"
	defaultConfigURL = "http://localhost:8080"
	masterKeyService = "qbitctl"
	masterKeyUser    = "master-key"
	v1HardcodedKey   = "encv1:40FC81BE491B5946F16245A7C918B59F158D758E"
)

var errUnsupportedPasswordFormat = errors.New("config file uses an unsupported password format")

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

func deriveAuthKeyV1() []byte {
	sum := sha256.Sum256([]byte(v1HardcodedKey))
	return sum[:]
}

func getMasterSecret() (string, error) {
	if s := os.Getenv("QBITCTL_MASTER_KEY"); s != "" {
		return s, nil
	}
	return keyring.Get(masterKeyService, masterKeyUser)
}

func getOrCreateMasterKey() ([]byte, string, error) {
	secret, err := getMasterSecret()
	if err == nil {
		sum := sha256.Sum256([]byte(secret))
		return sum[:], "v2", nil
	}

	// Try to create one if it's missing
	newSecret := make([]byte, 32)
	if _, err := rand.Read(newSecret); err != nil {
		return nil, "", err
	}
	secret = hex.EncodeToString(newSecret)
	if err := keyring.Set(masterKeyService, masterKeyUser, secret); err != nil {
		// If keyring is unavailable, fall back to v1 hardcoded key
		return deriveAuthKeyV1(), "v1", nil
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

	var key []byte
	switch parts[1] {
	case "v1":
		key = deriveAuthKeyV1()
	case "v2":
		secret, err := getMasterSecret()
		if err != nil {
			return "", fmt.Errorf("failed to retrieve master key from keyring: %w. Set QBITCTL_MASTER_KEY if keyring is unavailable", err)
		}
		sum := sha256.Sum256([]byte(secret))
		key = sum[:]
	default:
		return "", fmt.Errorf("unknown encryption version: %s", parts[1])
	}

	nonce, err := hex.DecodeString(parts[2])
	if err != nil {
		return "", err
	}

	ciphertext, err := hex.DecodeString(parts[3])
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

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	content, err := json.MarshalIndent(configFile{
		URL:      creds.URL,
		User:     creds.User,
		Password: encodedPass,
	}, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false

	return os.Chmod(path, 0o600)
}

func loadAuthFile(path string, infoOut io.Writer) (Credentials, bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Credentials{}, false, nil
		}
		return Credentials{}, false, err
	}

	var diskConfig configFile
	if err := json.Unmarshal(content, &diskConfig); err != nil {
		return Credentials{}, true, err
	}

	var creds Credentials
	creds.URL = diskConfig.URL
	creds.User = diskConfig.User

	switch value := diskConfig.Password; {
	case strings.HasPrefix(value, "enc:v1:"):
		password, err := decryptPassword(value)
		if err != nil {
			return Credentials{}, true, err
		}
		creds.Pass = password
		// Migrate to latest version (keyring if possible)
		if err := saveAuthFile(path, creds); err != nil {
			return Credentials{}, true, fmt.Errorf("migrate v1 config: %w", err)
		}
		// if infoOut != nil {
		// 	fmt.Fprintf(infoOut, "[INFO] Migrated v1 encrypted password to latest format in config file: %s\n", path)
		// }
	case strings.HasPrefix(value, "enc:v2:"):
		password, err := decryptPassword(value)
		if err != nil {
			return Credentials{}, true, err
		}
		creds.Pass = password
	case looksLikeOpaqueCiphertext(value):
		return Credentials{}, true, errUnsupportedPasswordFormat
	default:
		creds.Pass = value
		if value != "" {
			if err := saveAuthFile(path, creds); err != nil {
				return Credentials{}, true, fmt.Errorf("rewrite plaintext config file: %w", err)
			}
			if infoOut != nil {
				fmt.Fprintf(infoOut, "[INFO] Migrated plaintext password in config file: %s\n", path)
			}
		}
	}

	return creds, true, nil
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
	cli := Credentials{
		URL:  opts.URL,
		User: opts.User,
		Pass: opts.Pass,
	}
	configPath := opts.ConfigPath
	var creds Credentials
	loaded := false

	if configPath != "" {
		fileInfo, err := os.Stat(configPath)
		if err != nil || fileInfo.IsDir() {
			return Credentials{}, codedErrf(ExitBadArgs, "Config file '%s' does not exist or is not readable", configPath)
		}

		loadedCreds, ok, err := loadAuthFile(configPath, infoOut)
		if err != nil {
			if errors.Is(err, errUnsupportedPasswordFormat) {
				return Credentials{}, codedErrf(ExitFile, "%v. Run qbitctl config to rewrite %s", err, configPath)
			}
			return Credentials{}, codedErrf(ExitFile, "Failed to load config file '%s': %v", configPath, err)
		}
		if ok {
			creds = loadedCreds
			loaded = true
		}
	}

	if !loaded {
		for _, path := range defaultAuthSearchPaths() {
			loadedCreds, ok, err := loadAuthFile(path, infoOut)
			if err != nil {
				if errors.Is(err, errUnsupportedPasswordFormat) {
					return Credentials{}, codedErrf(ExitFile, "%v. Run qbitctl config to rewrite %s", err, path)
				}
				return Credentials{}, codedErrf(ExitFile, "Failed to load %s: %v", path, err)
			}
			if ok {
				creds = loadedCreds
				loaded = true
				break
			}
		}
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

	if !canAttemptLogin(creds) {
		return Credentials{}, codedErrf(ExitFile, "No credentials supplied")
	}

	return creds, nil
}

func NewClient(opts *Options) (*App, error) {
	app := &App{}
	newAppDefaults(app)

	creds, err := initAuth(*opts, app.Stderr)
	if err != nil {
		return nil, err
	}

	app.creds = creds
	app.client = newClient(creds, app.Stderr)

	if err := app.client.login(); err != nil {
		return nil, codedErrf(ExitLoginFail, "Login failed: %v", err)
	}

	return app, nil
}

func ExecuteConfigWrite(opts Options, outputPath string, infoOut io.Writer) error {
	if opts.Pass == "" {
		return codedErrf(ExitBadArgs, "Password required for non-interactive config write. Use --pass")
	}

	urlValue := opts.URL
	if urlValue == "" {
		urlValue = defaultConfigURL
	}
	normalizedURL, err := normalizeConfigURL(urlValue)
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

func normalizeConfigURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("URL must include a scheme and host, for example %s", defaultConfigURL)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("URL scheme must be http or https")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func promptLineTo(out io.Writer, reader *bufio.Reader, prompt, defaultValue string) (string, error) {
	fmt.Fprint(out, prompt)
	text, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	text = strings.TrimRight(text, "\r\n")
	if text == "quit" {
		return "", fmt.Errorf("quit")
	}
	if text == "" {
		return defaultValue, nil
	}
	return text, nil
}

func promptValidatedLine(out io.Writer, reader *bufio.Reader, prompt, defaultValue string, validate func(string) (string, error)) (string, error) {
	for {
		value, err := promptLineTo(out, reader, prompt, defaultValue)
		if err != nil {
			return "", err
		}
		normalized, err := validate(value)
		if err == nil {
			return normalized, nil
		}
		fmt.Fprintf(out, "[ERROR] %v\n", err)
	}
}

// HiddenInputFunc is a function that wraps password input to hide terminal echo.
// It receives a prompt function and should call it, returning the result.
// A nil value means no echo hiding (the prompt function is called directly).
type HiddenInputFunc func(fn func() (string, error)) (string, error)

func runInteractiveConfig(reader *bufio.Reader, out io.Writer, hiddenInput HiddenInputFunc, savePath string, saveFn func(string, Credentials) error) int {
	urlValue, err := promptValidatedLine(out, reader, fmt.Sprintf("URL [%s]: ", defaultConfigURL), defaultConfigURL, normalizeConfigURL)
	if err != nil {
		return ExitBadArgs
	}

	userValue, err := promptLineTo(out, reader, "Username [admin]: ", "admin")
	if err != nil {
		return ExitBadArgs
	}

	passwordValue, err := hiddenInput(func() (string, error) {
		return promptLineTo(out, reader, "Password: ", "")
	})
	if err != nil {
		return ExitBadArgs
	}
	if passwordValue == "" {
		fmt.Fprintln(out, "Empty password, not saving config file.")
		return ExitBadArgs
	}

	inputPath, err := promptLineTo(out, reader, fmt.Sprintf("Save path [%s]: ", savePath), savePath)
	if err != nil {
		return ExitBadArgs
	}

	if err := saveFn(inputPath, Credentials{URL: urlValue, User: userValue, Pass: passwordValue}); err != nil {
		fmt.Fprintf(out, "[ERROR] Failed to save config file: %v\n", err)
		return ExitFile
	}

	fmt.Fprintf(out, "Saved config to %s\n", inputPath)
	return ExitOK
}

func InteractiveConfig(in io.Reader, out io.Writer, hiddenInput HiddenInputFunc) int {
	if hiddenInput == nil {
		hiddenInput = func(fn func() (string, error)) (string, error) { return fn() }
	}
	return runInteractiveConfig(bufio.NewReader(in), out, hiddenInput, defaultAuthPath(), saveAuthFile)
}
