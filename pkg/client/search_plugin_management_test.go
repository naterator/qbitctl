package client

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestSearchPluginManagementRequests(t *testing.T) {
	tests := []struct {
		name string
		call func(*App) error
		path string
		want url.Values
	}{
		{
			name: "install",
			call: func(app *App) error {
				return app.InstallSearchPlugin("https://plugins.example/jackett.py")
			},
			path: "/api/v2/search/installPlugin",
			want: url.Values{"sources": {"https://plugins.example/jackett.py"}},
		},
		{
			name: "enable",
			call: func(app *App) error { return app.EnableSearchPlugin("jackett") },
			path: "/api/v2/search/enablePlugin",
			want: url.Values{"names": {"jackett"}, "enable": {"true"}},
		},
		{
			name: "disable",
			call: func(app *App) error { return app.DisableSearchPlugin("jackett") },
			path: "/api/v2/search/enablePlugin",
			want: url.Values{"names": {"jackett"}, "enable": {"false"}},
		},
		{
			name: "uninstall",
			call: func(app *App) error { return app.UninstallSearchPlugin("jackett") },
			path: "/api/v2/search/uninstallPlugin",
			want: url.Values{"names": {"jackett"}},
		},
		{
			name: "update",
			call: func(app *App) error { return app.UpdateSearchPlugins() },
			path: "/api/v2/search/updatePlugins",
			want: url.Values{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotMethod, gotPath string
			var got url.Values
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.Path
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				got = r.Form
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(server.Close)
			creds := Credentials{URL: server.URL, User: "admin", Pass: "secret"}
			app := &App{creds: creds}
			newAppDefaults(app)
			app.client = newClient(creds, io.Discard)

			if err := tt.call(app); err != nil {
				t.Fatalf("management call returned error: %v", err)
			}
			if gotMethod != http.MethodPost || gotPath != tt.path {
				t.Fatalf("request = %s %s, want POST %s", gotMethod, gotPath, tt.path)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("form = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSearchPluginManagementValidatesInputs(t *testing.T) {
	app := &App{client: newClient(Credentials{URL: "https://qbt.test"}, io.Discard)}
	for _, tt := range []struct {
		name string
		call func() error
	}{
		{name: "empty install source", call: func() error { return app.InstallSearchPlugin("") }},
		{name: "http install source", call: func() error { return app.InstallSearchPlugin("http://plugins.example/a.py") }},
		{name: "relative install source", call: func() error { return app.InstallSearchPlugin("a.py") }},
		{name: "source userinfo", call: func() error { return app.InstallSearchPlugin("https://user:pass@plugins.example/a.py") }},
		{name: "empty plugin ID", call: func() error { return app.EnableSearchPlugin(" ") }},
		{name: "pipe plugin ID", call: func() error { return app.DisableSearchPlugin("a|b") }},
		{name: "control plugin ID", call: func() error { return app.UninstallSearchPlugin("a\x00b") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			if err == nil {
				t.Fatal("call unexpectedly succeeded")
			}
			var coded *CodedError
			if !errors.As(err, &coded) || coded.Code != ExitBadArgs {
				t.Fatalf("error = %v, want ExitBadArgs", err)
			}
		})
	}
}

func TestSearchPluginManagementWrapsServerErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/search/uninstallPlugin" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "not allowed")
	}))
	t.Cleanup(server.Close)
	creds := Credentials{URL: server.URL, User: "admin", Pass: "secret"}
	app := &App{creds: creds}
	newAppDefaults(app)
	app.client = newClient(creds, io.Discard)
	err := app.UninstallSearchPlugin("jackett")
	if err == nil || !strings.Contains(err.Error(), "Failed to uninstall search plugin") {
		t.Fatalf("error = %v, want wrapped action error", err)
	}
	var coded *CodedError
	if !errors.As(err, &coded) || coded.Code != ExitSetFail {
		t.Fatalf("error = %v, want ExitSetFail", err)
	}
}
