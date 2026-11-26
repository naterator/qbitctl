package terminaltheme

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsForMissingOrUnknownTheme(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	for _, tt := range []struct {
		name    string
		config  string
		wantErr bool
	}{
		{name: "missing file"},
		{name: "missing setting", config: "{}"},
		{name: "unknown theme", config: `{"ui":{"theme":"not-a-theme"}}`},
		{name: "malformed theme", config: `{"ui":{"theme":{"id":"ocean"}}}`, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.config != "" {
				if err := os.WriteFile(path, []byte(tt.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := Load(path)
			if got != Default || (err != nil) != tt.wantErr {
				t.Fatalf("Load = %q, %v (want %q, error %t)", got, err, Default, tt.wantErr)
			}
		})
	}
}

func TestReassignedPalettesAndLegacyNordID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"ui":{"theme":"nord"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if id, err := Load(path); err != nil || id != "tookie" {
		t.Fatalf("legacy Nord = %q, %v, want tookie", id, err)
	}
	if Known("nord") || !Known("tookie") {
		t.Fatal("theme catalog should offer Tookie")
	}
	const opaque = 0xffff
	if r, g, b, a := Lookup(Default).Canvas.RGBA(); r != 0x2e2e || g != 0x3434 || b != 0x4040 || a != opaque {
		t.Fatal("Default did not inherit the former Nord canvas")
	}
	if r, g, b, a := Lookup("tookie").Canvas.RGBA(); r != 0x1d1d || g != 0x2525 || b != 0x3131 || a != opaque {
		t.Fatal("Tookie did not inherit the former Default canvas")
	}
}
