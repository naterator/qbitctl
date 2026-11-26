package configfile

import (
	"bytes"
	"encoding/json"
)

func ReadUIPreference(path, section string) (json.RawMessage, error) {
	root, err := Read(path)
	if err != nil {
		return nil, err
	}
	ui, err := root.Child("ui")
	if err != nil {
		return nil, err
	}
	return ui[section], nil
}

func UpdateUISection(path, section string, edit func(Object) (bool, error)) error {
	return Update(path, func(root Object) (bool, error) {
		ui, err := root.Child("ui")
		if err != nil {
			return false, err
		}
		fields, err := ui.Child(section)
		if err != nil {
			return false, err
		}
		changed, err := edit(fields)
		if err != nil || !changed {
			return false, err
		}
		update, err := json.Marshal(fields)
		if err != nil {
			return false, err
		}
		ui[section] = update
		root["ui"], err = json.Marshal(ui)
		return err == nil, err
	})
}

func WriteUISection(path, section string, values map[string]string) error {
	return UpdateUISection(path, section, func(fields Object) (bool, error) {
		changed := false
		for key, value := range values {
			raw, _ := json.Marshal(value)
			if !bytes.Equal(fields[key], raw) {
				fields[key] = raw
				changed = true
			}
		}
		return changed, nil
	})
}
