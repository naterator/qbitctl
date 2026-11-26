package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/naterator/qbitctl/internal/configfile"
)

// DefaultProfileName is the name used for the first interactive connection
// and for a migrated single-server config.
const DefaultProfileName = "default"

const (
	profilesConfigKey       = "profiles"
	defaultProfileConfigKey = "default_profile"
	profileCAFileKey        = "ca_file"
	profileSearchKey        = "default_search_plugin"
)

var profileNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ProfileSummary is non-secret metadata for one saved server profile.
type ProfileSummary struct {
	Name         string
	URL          string
	User         string
	CAFile       string
	SearchPlugin string
	HasPassword  bool
	Default      bool
}

func validateProfileName(name string) error {
	name = strings.TrimSpace(name)
	if !profileNamePattern.MatchString(name) {
		return fmt.Errorf("invalid profile name %q (use 1-64 letters, numbers, dots, underscores, or hyphens)", name)
	}
	return nil
}

func normalizeProfileName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if err := validateProfileName(name); err != nil {
		return "", err
	}
	return name, nil
}

// ValidateProfileName validates the stable name used by CLI and config APIs.
func ValidateProfileName(name string) error { return validateProfileName(name) }

// ValidateAndNormalizeProfileName trims user input and returns the exact map
// key that profile selection APIs use.
func ValidateAndNormalizeProfileName(name string) (string, error) {
	return normalizeProfileName(name)
}

func profileObject(root configfile.Object) (configfile.Object, bool, error) {
	raw, ok := configValue(root, profilesConfigKey)
	if !ok {
		return nil, false, nil
	}
	var profiles configfile.Object
	if err := json.Unmarshal(raw, &profiles); err != nil || profiles == nil {
		if err == nil {
			err = errors.New("section is not an object")
		}
		return nil, true, fmt.Errorf("decode config field %q: %w", profilesConfigKey, err)
	}
	return profiles, len(profiles) > 0, nil
}

func profileNames(profiles configfile.Object) []string {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func configuredDefaultProfile(root configfile.Object, profiles configfile.Object) string {
	if raw, ok := configValue(root, defaultProfileConfigKey); ok {
		var value string
		if json.Unmarshal(raw, &value) == nil && strings.TrimSpace(value) != "" {
			if _, exists := profiles[value]; exists {
				return value
			}
		}
	}
	names := profileNames(profiles)
	if len(names) > 0 {
		return names[0]
	}
	return ""
}

// selectProfileObject resolves the requested profile, returning the legacy
// root object when named profiles are not present. The requested "default"
// selector remains compatible with old single-server config files.
func selectProfileObject(root configfile.Object, requested string) (name string, profile configfile.Object, named bool, err error) {
	profiles, named, err := profileObject(root)
	if err != nil {
		return "", nil, named, err
	}
	requested = strings.TrimSpace(requested)
	if !named {
		if requested != "" && requested != DefaultProfileName {
			return "", nil, false, fmt.Errorf("profile %q is not configured", requested)
		}
		return "", root, false, nil
	}
	name = requested
	if name == "" {
		name = configuredDefaultProfile(root, profiles)
	}
	raw, ok := profiles[name]
	if !ok {
		return "", nil, true, fmt.Errorf("profile %q is not configured", name)
	}
	if err := json.Unmarshal(raw, &profile); err != nil || profile == nil {
		if err == nil {
			err = errors.New("section is not an object")
		}
		return "", nil, true, fmt.Errorf("decode profile %q: %w", name, err)
	}
	return name, profile, true, nil
}

func profileString(object configfile.Object, key string) (string, error) {
	raw, ok := configValue(object, key)
	if !ok {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("decode config field %q: %w", key, err)
	}
	return value, nil
}

func profileHasPassword(object configfile.Object) (bool, error) {
	value, err := profileString(object, "password")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(value) != "", nil
}

func profileMetadataFromObject(name string, object configfile.Object, isDefault bool) (ProfileSummary, error) {
	urlValue, err := profileString(object, "url")
	if err != nil {
		return ProfileSummary{}, err
	}
	userValue, err := profileString(object, "user")
	if err != nil {
		return ProfileSummary{}, err
	}
	caFile, err := profileString(object, profileCAFileKey)
	if err != nil {
		return ProfileSummary{}, err
	}
	searchPlugin, err := profileString(object, profileSearchKey)
	if err != nil {
		return ProfileSummary{}, err
	}
	hasPassword, err := profileHasPassword(object)
	if err != nil {
		return ProfileSummary{}, err
	}
	return ProfileSummary{Name: name, URL: redactSetupURL(urlValue), User: userValue, CAFile: caFile, SearchPlugin: searchPlugin, HasPassword: hasPassword, Default: isDefault}, nil
}

// ReadProfiles returns non-secret profile metadata and the explicit startup
// default. Legacy single-server configs return an implicit default profile.
func ReadProfiles(path string) ([]ProfileSummary, string, error) {
	if strings.TrimSpace(path) == "" {
		return nil, "", errors.New("config path is empty")
	}
	root, err := configfile.Read(path)
	if err != nil {
		return nil, "", err
	}
	profiles, named, err := profileObject(root)
	if err != nil {
		return nil, "", err
	}
	if !named {
		url, err := profileString(root, "url")
		if err != nil {
			return nil, "", err
		}
		if strings.TrimSpace(url) == "" {
			return nil, "", nil
		}
		legacy, err := profileMetadataFromObject(DefaultProfileName, root, true)
		if err != nil {
			return nil, "", err
		}
		return []ProfileSummary{legacy}, DefaultProfileName, nil
	}
	defaultName := configuredDefaultProfile(root, profiles)
	names := profileNames(profiles)
	result := make([]ProfileSummary, 0, len(names))
	for _, name := range names {
		var object configfile.Object
		if err := json.Unmarshal(profiles[name], &object); err != nil || object == nil {
			if err == nil {
				err = errors.New("section is not an object")
			}
			return nil, "", fmt.Errorf("decode profile %q: %w", name, err)
		}
		metadata, err := profileMetadataFromObject(name, object, name == defaultName)
		if err != nil {
			return nil, "", err
		}
		result = append(result, metadata)
	}
	return result, defaultName, nil
}

func migrateLegacyProfiles(root configfile.Object) (configfile.Object, bool, error) {
	profiles, named, err := profileObject(root)
	if err != nil {
		return nil, named, err
	}
	if named {
		return profiles, true, nil
	}
	profiles = configfile.Object{}
	legacy := configfile.Object{}
	for _, key := range []string{"url", "user", "password", profileCAFileKey, profileSearchKey} {
		if raw, ok := configValue(root, key); ok {
			legacy[key] = append(json.RawMessage(nil), raw...)
			deleteConfigValue(root, key)
		}
	}
	// A legacy config may still contain a plaintext password. Do not move that
	// value into a profile verbatim: profile expansion is also an opportunity to
	// retain the existing at-rest protection guarantee. If the keyring is
	// unavailable, fail the transaction rather than silently weakening it.
	if raw, ok := legacy["password"]; ok {
		var password string
		if err := json.Unmarshal(raw, &password); err != nil {
			return nil, false, fmt.Errorf("decode legacy password: %w", err)
		}
		if password != "" && !strings.HasPrefix(password, "enc:") {
			encrypted, err := encryptPassword(password)
			if err != nil {
				return nil, false, fmt.Errorf("encrypt legacy password: %w", err)
			}
			legacy["password"], err = json.Marshal(encrypted)
			if err != nil {
				return nil, false, fmt.Errorf("encode encrypted legacy password: %w", err)
			}
		}
	}
	if len(legacy) > 0 {
		encoded, err := json.Marshal(legacy)
		if err != nil {
			return nil, false, err
		}
		profiles[DefaultProfileName] = encoded
		defaultValue, _ := json.Marshal(DefaultProfileName)
		setConfigValue(root, defaultProfileConfigKey, defaultValue)
	}
	encodedProfiles, err := json.Marshal(profiles)
	if err != nil {
		return nil, false, err
	}
	setConfigValue(root, profilesConfigKey, encodedProfiles)
	return profiles, true, nil
}

func writeProfiles(root configfile.Object, profiles configfile.Object) error {
	encoded, err := json.Marshal(profiles)
	if err != nil {
		return err
	}
	setConfigValue(root, profilesConfigKey, encoded)
	return nil
}

// profileWriteTarget resolves the requested profile exactly as authentication
// does. With no explicit selector, a named config writes its startup profile;
// a legacy flat config writes the root object. The selected profile map is
// decoded independently so updating it cannot merge nested keys into root.
func profileWriteTarget(root configfile.Object, requested string) (name string, selected configfile.Object, profiles configfile.Object, named bool, err error) {
	name, selected, named, err = selectProfileObject(root, requested)
	if err != nil {
		return "", nil, nil, false, err
	}
	if !named {
		return "", root, nil, false, nil
	}
	profiles, _, err = profileObject(root)
	if err != nil {
		return "", nil, nil, true, err
	}
	return name, selected, profiles, true, nil
}

func profileObjectForWrite(root configfile.Object, name string) (configfile.Object, error) {
	if _, err := normalizeProfileName(name); err != nil {
		return nil, err
	}
	profiles, _, err := migrateLegacyProfiles(root)
	if err != nil {
		return nil, err
	}
	return profiles, nil
}

func setDefaultProfileInObject(root configfile.Object, name string) {
	value, _ := json.Marshal(name)
	setConfigValue(root, defaultProfileConfigKey, value)
}

// AddProfileConfig stores a new encrypted profile. It migrates legacy
// credentials only when the first additional profile is added.
func AddProfileConfig(path, name string, creds Credentials, caFile string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("config path is empty")
	}
	normalizedName, err := normalizeProfileName(name)
	if err != nil {
		return err
	}
	caFile = strings.TrimSpace(caFile)
	if caFile != "" {
		caFile, err = normalizeSavedCAFile(caFile)
		if err != nil {
			return err
		}
		if _, err := loadCAPool(caFile); err != nil {
			return fmt.Errorf("invalid CA file %q: %w", caFile, err)
		}
	}
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
	return configfile.Update(path, func(root configfile.Object) (bool, error) {
		profiles, err := profileObjectForWrite(root, normalizedName)
		if err != nil {
			return false, err
		}
		// Capture the effective default before inserting the new profile. The
		// helper falls back to the first existing profile, so evaluating it
		// after insertion would make an empty config look configured and omit
		// the explicit startup default.
		currentDefault := configuredDefaultProfile(root, profiles)
		if _, exists := profiles[normalizedName]; exists {
			return false, fmt.Errorf("profile %q already exists", normalizedName)
		}
		profile := configfile.Object{"url": urlValue, "user": userValue, "password": passwordValue}
		if caFile != "" {
			caValue, marshalErr := json.Marshal(caFile)
			if marshalErr != nil {
				return false, marshalErr
			}
			profile[profileCAFileKey] = caValue
		}
		encodedProfile, err := json.Marshal(profile)
		if err != nil {
			return false, err
		}
		profiles[normalizedName] = encodedProfile
		if currentDefault == "" {
			setDefaultProfileInObject(root, normalizedName)
		}
		if err := writeProfiles(root, profiles); err != nil {
			return false, err
		}
		return true, nil
	})
}

// SetDefaultProfile records the startup default without changing the current
// session's --profile selection.
func SetDefaultProfile(path, name string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("config path is empty")
	}
	normalizedName, err := normalizeProfileName(name)
	if err != nil {
		return err
	}
	return configfile.Update(path, func(root configfile.Object) (bool, error) {
		profiles, named, err := profileObject(root)
		if err != nil {
			return false, err
		}
		if !named {
			url, err := profileString(root, "url")
			if err != nil {
				return false, err
			}
			if normalizedName != DefaultProfileName || strings.TrimSpace(url) == "" {
				return false, fmt.Errorf("profile %q is not configured; add a named profile first", name)
			}
			// The legacy connection is listed as "default"; make it a named
			// profile in the same transaction as the startup selection.
			_, _, err = migrateLegacyProfiles(root)
			return err == nil, err
		}
		if _, ok := profiles[normalizedName]; !ok {
			return false, fmt.Errorf("profile %q is not configured", normalizedName)
		}
		setDefaultProfileInObject(root, normalizedName)
		return true, nil
	})
}

// RemoveProfile removes a named profile and selects the first remaining
// profile if the startup default was removed.
func RemoveProfile(path, name string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("config path is empty")
	}
	normalizedName, err := normalizeProfileName(name)
	if err != nil {
		return err
	}
	return configfile.Update(path, func(root configfile.Object) (bool, error) {
		profiles, named, err := profileObject(root)
		if err != nil {
			return false, err
		}
		if !named {
			return false, fmt.Errorf("profile %q is not configured", name)
		}
		if _, ok := profiles[normalizedName]; !ok {
			return false, fmt.Errorf("profile %q is not configured", normalizedName)
		}
		currentDefault := configuredDefaultProfile(root, profiles)
		delete(profiles, normalizedName)
		if len(profiles) == 0 {
			deleteConfigValue(root, profilesConfigKey)
			deleteConfigValue(root, defaultProfileConfigKey)
			return true, nil
		}
		if currentDefault == normalizedName || configuredDefaultProfile(root, profiles) == "" {
			setDefaultProfileInObject(root, profileNames(profiles)[0])
		}
		if err := writeProfiles(root, profiles); err != nil {
			return false, err
		}
		return true, nil
	})
}
