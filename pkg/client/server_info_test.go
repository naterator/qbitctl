package client

import (
	"bytes"
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
