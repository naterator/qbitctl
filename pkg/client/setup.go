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
	"strconv"
	"strings"

	"github.com/naterator/qbitctl/internal/configfile"
)

// Setup is the non-secret portion of the active qbitctl configuration.
// HasPassword deliberately reports presence only; callers that need to
// authenticate should use ConnectAndSaveSetup instead of reading config.json.
type Setup struct {
	Path        string
	Profile     string
	URL         string
	User        string
	CAFile      string
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
	path, err := configfile.ResolvePath(opts.ConfigPath)
	if err != nil {
		return Setup{}, err
	}

	object, err := configfile.Read(path)
	if err != nil {
		return Setup{}, err
	}
	selectedName, selectedObject, _, err := selectProfileObject(object, opts.Profile)
	if err != nil {
		return Setup{}, err
	}
	setup := Setup{Path: path, Profile: selectedName}
	if value, ok := configValue(selectedObject, "url"); ok {
		setup.URL, err = decodeSetupString("url", value)
		if err != nil {
			return Setup{}, err
		}
	}
	if value, ok := configValue(selectedObject, "user"); ok {
		setup.User, err = decodeSetupString("user", value)
		if err != nil {
			return Setup{}, err
		}
	}
	if strings.TrimSpace(opts.CAFile) == "" {
		if value, ok := configValue(selectedObject, caFileConfigKey); ok {
			setup.CAFile, err = decodeSetupString(caFileConfigKey, value)
			if err != nil {
				return Setup{}, err
			}
			setup.CAFile = strings.TrimSpace(setup.CAFile)
		}
	}
	if value, ok := configValue(selectedObject, "password"); ok {
		password, decodeErr := decodeSetupString("password", value)
		if decodeErr != nil {
			return Setup{}, decodeErr
		}
		setup.HasPassword = password != ""
	}
	setup.TUIDefault, err = readTUIDefault(object)
	if err != nil {
		return Setup{}, err
	}

	// Command-line values are intentional overrides, but empty flags must not
	// erase values discovered from the selected config file.
	if opts.URL != "" {
		setup.URL = opts.URL
	}
	if opts.User != "" {
		setup.User = opts.User
	}
	if opts.CAFile != "" {
		setup.CAFile = strings.TrimSpace(opts.CAFile)
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

// ReadTUIDefault reads the saved startup preference across profiles.
func ReadTUIDefault(path string) (bool, error) {
	object, err := configfile.Read(path)
	if err != nil {
		return false, err
	}
	return readTUIDefault(object)
}

func readTUIDefault(object configfile.Object) (bool, error) {
	var enabled bool
	if value, ok := configValue(object, "ui"); ok {
		var ui configfile.Object
		if err := json.Unmarshal(value, &ui); err != nil || ui == nil {
			if err == nil {
				err = errors.New("section is not an object")
			}
			return false, fmt.Errorf("decode config field %q: %w", "ui", err)
		}
		if value, ok := configValue(ui, "tui_default"); ok {
			if err := json.Unmarshal(value, &enabled); err != nil {
				return false, fmt.Errorf("decode config field %q: %w", "ui.tui_default", err)
			}
		}
	}
	return enabled, nil
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
	return connectAndSaveSetupWithCAProfile(ctx, path, "", creds, "", tuiDefault, false)
}

// ConnectAndSaveSetupWithCA validates and authenticates a setup using the
// supplied PEM CA bundle before saving credentials and the bundle path.
// Existing callers can continue using ConnectAndSaveSetup when they do not
// need to change the trust configuration.
func ConnectAndSaveSetupWithCA(ctx context.Context, path string, creds Credentials, caFile string, tuiDefault bool) (*App, error) {
	return connectAndSaveSetupWithCAProfile(ctx, path, "", creds, caFile, &tuiDefault, false)
}

func connectAndSaveSetupWithCA(ctx context.Context, path string, creds Credentials, caFile string, tuiDefault *bool) (*App, error) {
	return connectAndSaveSetupWithCAProfile(ctx, path, "", creds, caFile, tuiDefault, false)
}

// ConnectAndSaveProfileSetup authenticates a named profile and atomically
// stores its credentials. The profile argument is session-specific; it does
// not change the startup default. It is also suitable for adding a new
// profile, in which case the supplied password must be explicit.
func ConnectAndSaveProfileSetup(ctx context.Context, path, profile string, creds Credentials, caFile string) (*App, error) {
	profile, err := normalizeProfileName(profile)
	if err != nil {
		return nil, err
	}
	return connectAndSaveSetupWithCAProfile(ctx, path, profile, creds, caFile, nil, false)
}

// ConnectAndAddProfileSetup authenticates a new profile and performs an
// add-only atomic write after login. A concurrent writer cannot turn an add
// into an accidental overwrite; AddProfileConfig rechecks membership while it
// holds the config transaction lock.
func ConnectAndAddProfileSetup(ctx context.Context, path, profile string, creds Credentials, caFile string) (*App, error) {
	profile, err := normalizeProfileName(profile)
	if err != nil {
		return nil, err
	}
	return connectAndSaveSetupWithCAProfile(ctx, path, profile, creds, caFile, nil, true)
}

func connectAndSaveSetupWithCAProfile(ctx context.Context, path, profile string, creds Credentials, caFile string, tuiDefault *bool, addOnly bool) (*App, error) {
	profile = strings.TrimSpace(profile)
	if profile != "" {
		if _, err := normalizeProfileName(profile); err != nil {
			return nil, err
		}
	}
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
		savedURL, savedPassword, snapshot, err := readSavedPasswordForProfile(path, profile)
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
	caFile = strings.TrimSpace(caFile)
	caConfigured := caFile != ""
	if !caConfigured {
		var readErr error
		caFile, readErr = caFileForConnection(path, profile, creds.URL)
		if readErr != nil {
			return nil, readErr
		}
	} else {
		caFile, err = normalizeSavedCAFile(caFile)
		if err != nil {
			return nil, err
		}
	}
	if caFile != "" {
		if _, err := loadCAPool(caFile); err != nil {
			code := ExitFile
			if caConfigured {
				code = ExitBadArgs
			}
			return nil, codedErrf(code, "Invalid CA file '%s': %v", caFile, err)
		}
	}

	if ctx == nil {
		ctx = context.Background()
	}
	app := &App{
		Ctx:         ctx,
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		configPath:  path,
		creds:       creds,
		profileName: profile,
		CAFile:      caFile,
	}
	app.client, err = newClientWithTimeoutAndCA(creds, io.Discard, 0, caFile)
	if err != nil {
		code := ExitFile
		if caConfigured {
			code = ExitBadArgs
		}
		return nil, codedErrf(code, "Failed to configure CA file '%s': %v", caFile, err)
	}

	if err := app.client.loginContext(app.context()); err != nil {
		app.Logout()
		return nil, codedErrWrapf(ExitLoginFail, err, "Login failed: %v", err)
	}
	if err := app.CheckCompatibility(); err != nil {
		app.Logout()
		return nil, err
	}
	var caToSave *string
	if caConfigured {
		caToSave = &caFile
	}
	var saveErr error
	if addOnly {
		saveErr = AddProfileConfig(path, profile, creds, caFile)
	} else {
		app.profileName, saveErr = writeConnectionConfig(path, profile, creds, caToSave, tuiDefault, savedSnapshot, true)
	}
	if saveErr != nil {
		app.Logout()
		if errors.Is(saveErr, ErrSetupChanged) {
			return nil, saveErr
		}
		return nil, codedErrf(ExitFile, "Failed to save config file: %v", saveErr)
	}
	return app, nil
}

// readSavedPassword reads an existing password for reuse without invoking
// loadAuthFile, which would migrate or rewrite the config before connection
// succeeds. Errors are deliberately generic so malformed or encrypted values
// cannot echo password-like strings to the setup UI.
type setupAuthSnapshot struct {
	profile  string
	url      json.RawMessage
	password json.RawMessage
}

func readSavedPassword(path string) (string, string, *setupAuthSnapshot, error) {
	return readSavedPasswordForProfile(path, "")
}

func readSavedPasswordForProfile(path, profile string) (string, string, *setupAuthSnapshot, error) {
	object, err := configfile.Read(path)
	if err != nil {
		return "", "", nil, err
	}
	selectedName, selectedObject, _, err := selectProfileObject(object, profile)
	if err != nil {
		return "", "", nil, err
	}
	var savedURL string
	var savedURLRaw json.RawMessage
	if value, ok := configValue(selectedObject, "url"); ok {
		savedURL, err = decodeSetupString("url", value)
		if err != nil {
			return "", "", nil, err
		}
		savedURLRaw = append(json.RawMessage(nil), value...)
	}
	value, ok := configValue(selectedObject, "password")
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
		profile:  selectedName,
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

func writeConnectionConfig(path, profile string, creds Credentials, caFile *string, tuiDefault *bool, snapshot *setupAuthSnapshot, nameDefault bool) (string, error) {
	encodedPassword, err := encryptPassword(creds.Pass)
	if err != nil {
		return "", err
	}
	urlValue, err := json.Marshal(creds.URL)
	if err != nil {
		return "", err
	}
	userValue, err := json.Marshal(creds.User)
	if err != nil {
		return "", err
	}
	passwordValue, err := json.Marshal(encodedPassword)
	if err != nil {
		return "", err
	}
	if caFile != nil {
		normalized, normalizeErr := normalizeSavedCAFile(*caFile)
		if normalizeErr != nil {
			return "", normalizeErr
		}
		caFile = &normalized
	}
	var savedProfile string
	err = configfile.Update(path, func(object configfile.Object) (bool, error) {
		selectedName, selected, profiles, named, err := profileWriteTarget(object, strings.TrimSpace(profile))
		if err != nil {
			return false, err
		}
		if snapshot != nil {
			currentURL, urlOK := configValue(selected, "url")
			currentPassword, passwordOK := configValue(selected, "password")
			if selectedName != snapshot.profile || !urlOK || !passwordOK ||
				!bytes.Equal(currentURL, snapshot.url) || !bytes.Equal(currentPassword, snapshot.password) {
				return false, ErrSetupChanged
			}
		}
		setConfigValue(selected, "url", urlValue)
		setConfigValue(selected, "user", userValue)
		setConfigValue(selected, "password", passwordValue)
		if caFile != nil {
			if strings.TrimSpace(*caFile) == "" {
				for existing := range selected {
					if strings.EqualFold(existing, caFileConfigKey) {
						delete(selected, existing)
					}
				}
			} else {
				encodedCA, err := json.Marshal(strings.TrimSpace(*caFile))
				if err != nil {
					return false, fmt.Errorf("encode config field %q: %w", caFileConfigKey, err)
				}
				setConfigValue(selected, caFileConfigKey, encodedCA)
			}
		}
		savedProfile = selectedName
		if named {
			encodedProfile, err := json.Marshal(selected)
			if err != nil {
				return false, err
			}
			profiles[selectedName] = encodedProfile
			if err := writeProfiles(object, profiles); err != nil {
				return false, err
			}
		} else if nameDefault {
			if _, _, err := migrateLegacyProfiles(object); err != nil {
				return false, err
			}
			savedProfile = DefaultProfileName
		}
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
	return savedProfile, err
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
	return InteractiveConfigProfileContext(ctx, in, out, hiddenInput, path, "")
}

// InteractiveConfigProfileContext is the profile-aware setup flow used by
// `qbitctl profile setup [NAME]`. With no name, it edits the startup profile
// or saves the connection as "default" in a first-run or legacy config.
func InteractiveConfigProfileContext(ctx context.Context, in io.Reader, out io.Writer, hiddenInput HiddenInputFunc, path, profile string) (string, error) {
	profilePath, err := configfile.ResolvePath(path)
	if err != nil {
		return "", err
	}
	setup, err := readSetup(Options{ConfigPath: profilePath, Profile: profile})
	if err != nil {
		return "", err
	}
	reader := bufio.NewReader(in)
	creds, err := PromptProfileCredentials(reader, out, hiddenInput, Credentials{}, setup)
	if err != nil {
		return "", err
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

	app, err := connectAndSaveSetupWithCAProfile(ctx, selectedPath, profile, creds, "", nil, false)
	if err != nil {
		return "", err
	}
	app.Logout()
	return selectedPath, nil
}

// PromptProfileCredentials collects missing fields and leaves supplied fields
// intact. The default setup provides optional URL and username suggestions.
func PromptProfileCredentials(reader *bufio.Reader, out io.Writer, hiddenInput HiddenInputFunc, creds Credentials, defaults Setup) (Credentials, error) {
	if hiddenInput == nil {
		hiddenInput = func(fn func() (string, error)) (string, error) { return fn() }
	}
	var err error
	if creds.URL == "" {
		urlDefault := defaultConfigURL
		if strings.TrimSpace(defaults.URL) != "" {
			normalized, validationErr := ValidateConfigURL(defaults.URL)
			if validationErr != nil {
				fmt.Fprintf(out, "[WARN] Saved URL is invalid (%v). Enter a new URL.\n", validationErr)
			} else {
				urlDefault = normalized
			}
		}
		creds.URL, err = promptValidatedLine(out, reader, fmt.Sprintf("URL [%s]: ", urlDefault), urlDefault, ValidateConfigURL)
		if err != nil {
			return Credentials{}, err
		}
	}
	if creds.User == "" {
		userDefault := defaults.User
		if userDefault == "" {
			userDefault = "admin"
		}
		creds.User, err = promptLineTo(out, reader, fmt.Sprintf("Username [%s]: ", userDefault), userDefault)
		if err != nil {
			return Credentials{}, err
		}
	}
	if creds.Pass == "" {
		creds.Pass, err = hiddenInput(func() (string, error) { return promptLineTo(out, reader, "Password: ", "") })
		if err != nil {
			return Credentials{}, err
		}
	}
	if creds.Pass == "" {
		return Credentials{}, ErrSetupPasswordRequired
	}
	return creds, nil
}
