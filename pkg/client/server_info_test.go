package client

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestShowServerInfo(t *testing.T) {
	server := newQBTestServer(t)
	app := server.newApp()
	var buf bytes.Buffer
	app.Stdout = &buf

	if err := app.ShowServerInfo(); err != nil {
		t.Fatalf("ShowServerInfo err = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "v5.0.0") {
		t.Fatalf("output missing app version: %q", out)
	}
	if !strings.Contains(out, "2.11.0") {
		t.Fatalf("output missing API version: %q", out)
	}
	if !strings.Contains(out, "connected") {
		t.Fatalf("output missing connection status: %q", out)
	}
}

func TestShowServerInfoAsJSON(t *testing.T) {
	server := newQBTestServer(t)
	app := server.newApp()
	var buf bytes.Buffer
	app.Stdout = &buf

	if err := app.ShowServerInfoAsJSON(); err != nil {
		t.Fatalf("ShowServerInfoAsJSON err = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "v5.0.0") || !strings.Contains(out, "connected") {
		t.Fatalf("JSON output = %q", out)
	}
}

func TestShowServerInfoJSON(t *testing.T) {
	server := newQBTestServer(t)
	app := server.newApp()
	var buf bytes.Buffer
	app.Stdout = &buf

	if err := app.ShowServerInfoJSON(); err != nil {
		t.Fatalf("ShowServerInfoJSON err = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "connected") || !strings.Contains(out, "1024000") {
		t.Fatalf("raw JSON output = %q", out)
	}
}

func TestCheckCompatibilityRejectsUnsupportedVersions(t *testing.T) {
	for _, tt := range []struct {
		name       string
		appVersion string
		apiVersion string
	}{
		{name: "old qBittorrent", appVersion: "v4.6.7", apiVersion: "2.11.0"},
		{name: "old WebAPI", appVersion: "v5.0.0", apiVersion: "2.10.4"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := newQBTestServer(t)
			server.AppVersion = tt.appVersion
			server.APIVersion = tt.apiVersion
			app := server.newApp()

			err := app.CheckCompatibility()
			if err == nil {
				t.Fatal("CheckCompatibility unexpectedly succeeded")
			}
			var coded *CodedError
			if !errors.As(err, &coded) || coded.Code != ExitLoginFail {
				t.Fatalf("CheckCompatibility err = %v, want ExitLoginFail", err)
			}
		})
	}
}

func TestNewClientChecksCompatibility(t *testing.T) {
	server := newQBTestServer(t)
	server.APIVersion = "2.10.4"
	withQBServerClientFactory(t, server)

	_, err := NewClient(&Options{
		URL:  server.creds().URL,
		User: "admin",
		Pass: "secret",
	})
	if err == nil {
		t.Fatal("NewClient unexpectedly succeeded with unsupported WebAPI version")
	}
	var coded *CodedError
	if !errors.As(err, &coded) || coded.Code != ExitLoginFail {
		t.Fatalf("NewClient err = %v, want ExitLoginFail", err)
	}
	if server.LoginCalls != 1 {
		t.Fatalf("loginCalls = %d, want 1", server.LoginCalls)
	}
}

func TestCompareDottedVersion(t *testing.T) {
	tests := []struct {
		a    string
		b    string
		want int
	}{
		{a: "v5.0.0", b: "5.0.0", want: 0},
		{a: "5.0.1", b: "5.0.0", want: 1},
		{a: "4.6.7", b: "5.0.0", want: -1},
		{a: "2.11.0", b: "2.10.4", want: 1},
	}
	for _, tt := range tests {
		got := compareDottedVersion(tt.a, tt.b)
		if got != tt.want {
			t.Fatalf("compareDottedVersion(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}
