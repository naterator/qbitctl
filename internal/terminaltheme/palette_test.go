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
