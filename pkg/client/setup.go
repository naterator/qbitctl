package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/naterator/qbitctl/internal/configfile"
)

// Setup is the non-secret portion of the active qbitctl configuration.
// HasPassword deliberately reports presence only; callers that need to
// authenticate should use ConnectAndSaveSetup instead of reading config.json.
type Setup struct {
	Path        string
	URL         string
	User        string
	HasPassword bool
	TUIDefault  bool
}

// ErrSetupPasswordRequired indicates that a setup cannot authenticate without
// a password. It is returned when no password was supplied and no reusable
// password is available for the same server origin.
var ErrSetupPasswordRequired = errors.New("password required to connect")

// ErrSetupInputEnded indicates that setup input ended before a valid value
// could be entered, or that the prompt exceeded its bounded retry budget.
var ErrSetupInputEnded = errors.New("setup input ended before a valid value was entered")

// ErrPasswordRequired is a concise alias for callers that do not need to
// distinguish setup-specific password prompts.
var ErrPasswordRequired = ErrSetupPasswordRequired

// ErrSetupChanged indicates that the config changed while a saved password
// was being validated. The caller should reread the setup rather than
// overwriting the newer credentials.
var ErrSetupChanged = errors.New("setup changed while connecting")

// Ready reports whether the setup has a save target and enough metadata to
// attempt a login.
func (s Setup) Ready() bool {
	return strings.TrimSpace(s.Path) != "" && strings.TrimSpace(s.URL) != "" && strings.TrimSpace(s.User) != "" && s.HasPassword
}

// ReadSetup returns the active configuration's metadata without decrypting,
// migrating, or rewriting its password. Explicit --config paths are selected
// even when they do not exist yet so the setup UI can create a first config.
func ReadSetup(opts Options) (Setup, error) {
	setup, err := readSetup(opts)
	setup.URL = redactSetupURL(setup.URL)
	return setup, err
}

func readSetup(opts Options) (Setup, error) {
	path := opts.ConfigPath
	if path == "" {
		for _, candidate := range defaultAuthSearchPaths() {
			info, err := os.Stat(candidate)
			if err == nil {
				if info.IsDir() {
					return Setup{}, fmt.Errorf("config path %q is a directory", candidate)
				}
				path = candidate
				break
			}
			if !errors.Is(err, os.ErrNotExist) {
				return Setup{}, fmt.Errorf("inspect config path %q: %w", candidate, err)
			}
		}
		if path == "" {
			path = defaultAuthPath()
		}
	}

	object, err := configfile.Read(path)
	if err != nil {
		return Setup{}, err
	}
	setup := Setup{Path: path}
	if value, ok := configValue(object, "url"); ok {
		setup.URL, err = decodeSetupString("url", value)
		if err != nil {
			return Setup{}, err
		}
	}
	if value, ok := configValue(object, "user"); ok {
		setup.User, err = decodeSetupString("user", value)
		if err != nil {
			return Setup{}, err
		}
	}
	if value, ok := configValue(object, "password"); ok {
		password, decodeErr := decodeSetupString("password", value)
		if decodeErr != nil {
			return Setup{}, decodeErr
		}
		setup.HasPassword = password != ""
	}
	if value, ok := configValue(object, "ui"); ok {
		var ui configfile.Object
		if err := json.Unmarshal(value, &ui); err != nil || ui == nil {
			if err == nil {
				err = errors.New("section is not an object")
			}
			return Setup{}, fmt.Errorf("decode config field %q: %w", "ui", err)
		}
		if value, ok := configValue(ui, "tui_default"); ok {
			if err := json.Unmarshal(value, &setup.TUIDefault); err != nil {
				return Setup{}, fmt.Errorf("decode config field %q: %w", "ui.tui_default", err)
			}
		}
	}

	// Command-line values are intentional overrides, but empty flags must not
	// erase values discovered from the selected config file.
	if opts.URL != "" {
		setup.URL = opts.URL
	}
	if opts.User != "" {
		setup.User = opts.User
	}
	if opts.Pass != "" {
		setup.HasPassword = true
	}
	return setup, nil
}

func decodeSetupString(field string, value json.RawMessage) (string, error) {
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return "", fmt.Errorf("decode config field %q: %w", field, err)
	}
	return text, nil
}

// redactSetupURL removes embedded URL userinfo from metadata displayed by the
// setup UI. A malformed address is hidden rather than echoed in an error or
// prompt, since it may contain a password-like value.
func redactSetupURL(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.User != nil {
		if err != nil {
			return ""
		}
		parsed.User = nil
	}
	return parsed.String()
}

// SetTUIDefault changes only ui.tui_default. It intentionally does not
// require credentials, allowing the startup preference to be changed before
// the first successful connection.
func SetTUIDefault(path string, enabled bool) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("config path is empty")
	}
	value, err := json.Marshal(enabled)
	if err != nil {
		return err
	}
	return configfile.Update(path, func(object configfile.Object) (bool, error) {
		ui := configfile.Object{}
		if raw, ok := configValue(object, "ui"); ok {
			if err := json.Unmarshal(raw, &ui); err != nil || ui == nil {
				if err == nil {
					err = errors.New("section is not an object")
				}
				return false, fmt.Errorf("decode config field %q: %w", "ui", err)
			}
		}
		setConfigValue(ui, "tui_default", value)
		encodedUI, err := json.Marshal(ui)
		if err != nil {
			return false, fmt.Errorf("encode config field %q: %w", "ui", err)
		}
		setConfigValue(object, "ui", encodedUI)
		return true, nil
	})
}

// ConnectAndSaveSetup validates and authenticates a setup, verifies the
// server/API compatibility contract, and only then atomically saves the
// credentials and startup preference. All connection diagnostics are
// discarded because this is used by the interactive setup UI.
func ConnectAndSaveSetup(ctx context.Context, path string, creds Credentials, tuiDefault bool) (*App, error) {
	return connectAndSaveSetup(ctx, path, creds, &tuiDefault)
}

func connectAndSaveSetup(ctx context.Context, path string, creds Credentials, tuiDefault *bool) (*App, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("config path is empty")
	}
	normalizedURL, err := ValidateConfigURL(creds.URL)
	if err != nil {
		return nil, err
	}
	creds.URL = normalizedURL

	var savedSnapshot *setupAuthSnapshot
	if creds.Pass == "" {
		savedURL, savedPassword, snapshot, err := readSavedPassword(path)
		if err != nil {
			return nil, err
		}
		if savedPassword == "" {
			return nil, ErrSetupPasswordRequired
		}
		if !sameCredentialOrigin(savedURL, normalizedURL) {
			return nil, fmt.Errorf("%w: server address changed", ErrSetupPasswordRequired)
		}
		creds.Pass = savedPassword
		savedSnapshot = snapshot
	}
	if creds.Pass == "" {
		return nil, ErrSetupPasswordRequired
	}

	if ctx == nil {
		ctx = context.Background()
	}
	app := &App{
		Ctx:        ctx,
		Stdout:     io.Discard,
		Stderr:     io.Discard,
		configPath: path,
		creds:      creds,
	}
	app.client = newClient(creds, io.Discard)

	if err := app.client.loginContext(app.context()); err != nil {
		app.Logout()
		return nil, codedErrf(ExitLoginFail, "Login failed: %v", err)
	}
	if err := app.CheckCompatibility(); err != nil {
		app.Logout()
		return nil, err
	}
	if err := saveSetupConfig(path, creds, tuiDefault, savedSnapshot); err != nil {
		app.Logout()
		if errors.Is(err, ErrSetupChanged) {
			return nil, err
		}
		return nil, codedErrf(ExitFile, "Failed to save config file: %v", err)
	}
	return app, nil
}

// readSavedPassword reads an existing password for reuse without invoking
// loadAuthFile, which would migrate or rewrite the config before connection
// succeeds. Errors are deliberately generic so malformed or encrypted values
// cannot echo password-like strings to the setup UI.
type setupAuthSnapshot struct {
	url      json.RawMessage
	password json.RawMessage
}

func readSavedPassword(path string) (string, string, *setupAuthSnapshot, error) {
	object, err := configfile.Read(path)
	if err != nil {
		return "", "", nil, err
	}
	var savedURL string
	var savedURLRaw json.RawMessage
	if value, ok := configValue(object, "url"); ok {
		savedURL, err = decodeSetupString("url", value)
		if err != nil {
			return "", "", nil, err
		}
		savedURLRaw = append(json.RawMessage(nil), value...)
	}
	value, ok := configValue(object, "password")
	if !ok {
		return savedURL, "", nil, nil
	}
	password, err := decodeSetupString("password", value)
	if err != nil {
		return "", "", nil, err
	}
	if password == "" {
		return savedURL, "", nil, nil
	}
	snapshot := &setupAuthSnapshot{
		url:      savedURLRaw,
		password: append(json.RawMessage(nil), value...),
	}
	if strings.HasPrefix(password, "enc:v2:") {
		password, err = decryptPassword(password)
		if err != nil {
			return "", "", nil, errors.New("saved password could not be decrypted")
		}
	} else if strings.HasPrefix(password, "enc:") {
		return "", "", nil, errReenterEncryptedPassword
	} else if looksLikeOpaqueCiphertext(password) {
		return "", "", nil, errUnsupportedPasswordFormat
	}
	return savedURL, password, snapshot, nil
}

func saveSetupConfig(path string, creds Credentials, tuiDefault *bool, snapshot *setupAuthSnapshot) error {
	encodedPassword, err := encryptPassword(creds.Pass)
	if err != nil {
		return err
	}
	urlValue, err := json.Marshal(creds.URL)
	if err != nil {
		return err
	}
	userValue, err := json.Marshal(creds.User)
	if err != nil {
		return err
	}
	passwordValue, err := json.Marshal(encodedPassword)
	if err != nil {
		return err
	}
	return configfile.Update(path, func(object configfile.Object) (bool, error) {
		if snapshot != nil {
			currentURL, urlOK := configValue(object, "url")
			currentPassword, passwordOK := configValue(object, "password")
			if !urlOK || !passwordOK || !bytes.Equal(currentURL, snapshot.url) || !bytes.Equal(currentPassword, snapshot.password) {
				return false, ErrSetupChanged
			}
		}
		setConfigValue(object, "url", urlValue)
		setConfigValue(object, "user", userValue)
		setConfigValue(object, "password", passwordValue)
		if tuiDefault == nil {
			return true, nil
		}

		ui := configfile.Object{}
		if raw, ok := configValue(object, "ui"); ok {
			if err := json.Unmarshal(raw, &ui); err != nil || ui == nil {
				if err == nil {
					err = errors.New("section is not an object")
				}
				return false, fmt.Errorf("decode config field %q: %w", "ui", err)
			}
		}
		setConfigValue(ui, "tui_default", json.RawMessage(strconv.FormatBool(*tuiDefault)))
		encodedUI, err := json.Marshal(ui)
		if err != nil {
			return false, fmt.Errorf("encode config field %q: %w", "ui", err)
		}
		setConfigValue(object, "ui", encodedUI)
		return true, nil
	})
}

// InteractiveConfigAt validates credentials and saves them to the given path.
// An empty path uses the default profile without prompting for a path.
func InteractiveConfigAt(in io.Reader, out io.Writer, hiddenInput HiddenInputFunc, path string) (string, error) {
	if path == "" {
		path = defaultAuthPath()
	}
	return InteractiveConfigContext(context.Background(), in, out, hiddenInput, path)
}

// InteractiveConfigContext runs the text setup flow, validates the credentials
// against the server, and only then persists them. A non-empty path is used
// exactly and skips the legacy save-path prompt; an empty path retains that
// prompt with the default config path as its value.
func InteractiveConfigContext(ctx context.Context, in io.Reader, out io.Writer, hiddenInput HiddenInputFunc, path string) (string, error) {
	if hiddenInput == nil {
		hiddenInput = func(fn func() (string, error)) (string, error) { return fn() }
	}

	profilePath := path
	if profilePath == "" {
		profilePath = defaultAuthPath()
	}
	profile, err := readSetup(Options{ConfigPath: profilePath})
	if err != nil {
		return "", err
	}
	urlDefault := defaultConfigURL
	if strings.TrimSpace(profile.URL) != "" {
		normalizedURL, validationErr := ValidateConfigURL(profile.URL)
		if validationErr != nil {
			fmt.Fprintf(out, "[WARN] Saved URL is invalid (%v). Enter a new URL.\n", validationErr)
		} else {
			urlDefault = normalizedURL
		}
	}
	userDefault := profile.User
	if userDefault == "" {
		userDefault = "admin"
	}

	reader := bufio.NewReader(in)
	urlValue, err := promptValidatedLine(out, reader, fmt.Sprintf("URL [%s]: ", urlDefault), urlDefault, ValidateConfigURL)
	if err != nil {
		return "", err
	}
	userValue, err := promptLineTo(out, reader, fmt.Sprintf("Username [%s]: ", userDefault), userDefault)
	if err != nil {
		return "", err
	}
	passwordValue, err := hiddenInput(func() (string, error) {
		return promptLineTo(out, reader, "Password: ", "")
	})
	if err != nil {
		return "", err
	}
	if passwordValue == "" {
		return "", ErrSetupPasswordRequired
	}

	selectedPath := path
	if selectedPath == "" {
		selectedPath, err = promptLineTo(out, reader, fmt.Sprintf("Save path [%s]: ", profilePath), profilePath)
		if err != nil {
			return "", err
		}
	}
	if strings.TrimSpace(selectedPath) == "" {
		return "", errors.New("config path is empty")
	}

	app, err := connectAndSaveSetup(ctx, selectedPath, Credentials{
		URL:  urlValue,
		User: userValue,
		Pass: passwordValue,
	}, nil)
	if err != nil {
		return "", err
	}
	app.Logout()
	return selectedPath, nil
}
