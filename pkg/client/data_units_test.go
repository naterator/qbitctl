package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/naterator/qbitctl/internal/configfile"
)

func TestParseDataUnits(t *testing.T) {
	for _, tt := range []struct {
		name string
		raw  string
		want DataUnits
		ok   bool
	}{
		{name: "empty defaults to human", raw: "", want: DataUnitsHuman, ok: true},
		{name: "human", raw: "human", want: DataUnitsHuman, ok: true},
		{name: "trimmed human", raw: " HUMAN ", want: DataUnitsHuman, ok: true},
		{name: "bytes", raw: "bytes", want: DataUnitsBytes, ok: true},
		{name: "trimmed bytes", raw: " BYTES ", want: DataUnitsBytes, ok: true},
		{name: "unknown", raw: "decimal", ok: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDataUnits(tt.raw)
			if (err == nil) != tt.ok {
				t.Fatalf("ParseDataUnits(%q) error = %v, want success: %v", tt.raw, err, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("ParseDataUnits(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestFormatSizeUsesConfiguredUnits(t *testing.T) {
	max := int64(math.MaxInt64)
	for _, tt := range []struct {
		name  string
		units DataUnits
		value int64
		want  string
	}{
		{name: "zero value units", units: "", value: 1536, want: "1.5 KiB"},
		{name: "human", units: DataUnitsHuman, value: 1536, want: "1.5 KiB"},
		{name: "human max", units: DataUnitsHuman, value: max, want: "8192.0 PiB"},
		{name: "bytes", units: DataUnitsBytes, value: 1536, want: "1536 B"},
		{name: "bytes max", units: DataUnitsBytes, value: max, want: "9223372036854775807 B"},
		{name: "human unavailable", units: DataUnitsHuman, value: -1, want: "?"},
		{name: "bytes unavailable", units: DataUnitsBytes, value: -1, want: "?"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.units.FormatSize(tt.value); got != tt.want {
				t.Fatalf("FormatSize(%d) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestFormatRateUsesConfiguredUnits(t *testing.T) {
	for _, tt := range []struct {
		name  string
		units DataUnits
		value int64
		want  string
	}{
		{name: "human", units: DataUnitsHuman, value: 1536, want: "1.5 KiB/s"},
		{name: "zero value units", units: "", value: 1536, want: "1.5 KiB/s"},
		{name: "bytes", units: DataUnitsBytes, value: 1536, want: "1536 B/s"},
		{name: "bytes max", units: DataUnitsBytes, value: math.MaxInt64, want: "9223372036854775807 B/s"},
		{name: "human unavailable", units: DataUnitsHuman, value: -1, want: "?"},
		{name: "bytes unavailable", units: DataUnitsBytes, value: -1, want: "?"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.units.FormatRate(tt.value); got != tt.want {
				t.Fatalf("FormatRate(%d) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestReadAndSetDataUnitsPreserveConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if got, err := ReadDataUnits(path); err != nil || got != DataUnitsHuman {
		t.Fatalf("missing data units = %q, %v; want human", got, err)
	}
	if err := configfile.Update(path, func(object configfile.Object) (bool, error) {
		object["url"] = json.RawMessage(`"http://qbt.test"`)
		object["future"] = json.RawMessage(`{"keep":true}`)
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := SetDataUnits(path, DataUnitsBytes); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadDataUnits(path); err != nil || got != DataUnitsBytes {
		t.Fatalf("saved data units = %q, %v; want bytes", got, err)
	}
	object, err := configfile.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"url", "future", dataUnitsConfigKey} {
		if _, ok := object[key]; !ok {
			t.Fatalf("SetDataUnits dropped %q", key)
		}
	}
	if err := SetDataUnits(path, ""); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadDataUnits(path); err != nil || got != DataUnitsHuman {
		t.Fatalf("empty data units = %q, %v; want human", got, err)
	}
}

func TestReadDataUnitsRejectsInvalidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"data_units":"decimal"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDataUnits(path); err == nil {
		t.Fatal("invalid data units unexpectedly succeeded")
	}
	if err := SetDataUnits(path, DataUnits("decimal")); err == nil {
		t.Fatal("invalid data units write unexpectedly succeeded")
	}
}

func TestNewClientUsesConfigDataUnitsAndOverride(t *testing.T) {
	withMockMasterKey(t)
	server := newQBTestServer(t)
	server.AppVersion = "v5.3.0"
	server.APIVersion = "2.16.2"
	withQBServerClientFactory(t, server)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := saveAuthFile(path, server.creds()); err != nil {
		t.Fatal(err)
	}
	if err := SetDataUnits(path, DataUnitsBytes); err != nil {
		t.Fatal(err)
	}
	app, err := NewClientWithOutput(context.Background(), &Options{ConfigPath: path}, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("NewClientWithOutput(config) returned error: %v", err)
	}
	if app.DataUnits != DataUnitsBytes {
		t.Fatalf("configured DataUnits = %q, want bytes", app.DataUnits)
	}
	app.Logout()

	server = newQBTestServer(t)
	server.AppVersion = "v5.3.0"
	server.APIVersion = "2.16.2"
	withQBServerClientFactory(t, server)
	if err := saveAuthFile(path, server.creds()); err != nil {
		t.Fatal(err)
	}
	app, err = NewClientWithOutput(context.Background(), &Options{ConfigPath: path, DataUnits: DataUnitsHuman}, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("NewClientWithOutput(override) returned error: %v", err)
	}
	if app.DataUnits != DataUnitsHuman {
		t.Fatalf("override DataUnits = %q, want human", app.DataUnits)
	}
	app.Logout()
}

func TestNewClientIgnoresBrokenOptionalDataUnits(t *testing.T) {
	withMockMasterKey(t)
	server := newQBTestServer(t)
	server.AppVersion = "v5.3.0"
	server.APIVersion = "2.16.2"
	withQBServerClientFactory(t, server)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := saveAuthFile(path, server.creds()); err != nil {
		t.Fatal(err)
	}
	if err := configfile.Update(path, func(object configfile.Object) (bool, error) {
		object[dataUnitsConfigKey] = json.RawMessage(`"not-a-unit"`)
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	app, err := NewClientWithOutput(context.Background(), &Options{ConfigPath: path}, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("broken optional preference blocked auth: %v", err)
	}
	if app.DataUnits != DataUnitsHuman {
		t.Fatalf("broken preference DataUnits = %q, want human", app.DataUnits)
	}
	app.Logout()
}

func TestNewClientRejectsInvalidDataUnitsOverride(t *testing.T) {
	withMockMasterKey(t)
	server := newQBTestServer(t)
	server.AppVersion = "v5.3.0"
	server.APIVersion = "2.16.2"
	withQBServerClientFactory(t, server)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := saveAuthFile(path, server.creds()); err != nil {
		t.Fatal(err)
	}
	_, err := NewClientWithOutput(context.Background(), &Options{ConfigPath: path, DataUnits: DataUnits("decimal")}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("invalid data units override unexpectedly succeeded")
	}
	var coded *CodedError
	if !errors.As(err, &coded) || coded.Code != ExitBadArgs {
		t.Fatalf("invalid override error = %v, want ExitBadArgs", err)
	}
}
