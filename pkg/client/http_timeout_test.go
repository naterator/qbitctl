package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/naterator/qbitctl/internal/qbtest"
)

func TestHTTPTimeoutProfiles(t *testing.T) {
	defaults, err := httpTimeoutsFor(0)
	if err != nil {
		t.Fatal(err)
	}
	if defaults.request != 15*time.Second || defaults.version != 15*time.Second || defaults.login != 15*time.Second {
		t.Fatalf("short request defaults = %#v, want 15 seconds", defaults)
	}
	if defaults.list != 60*time.Second {
		t.Fatalf("list timeout = %s, want 60s", defaults.list)
	}
	if defaults.upload != 2*time.Minute {
		t.Fatalf("upload timeout = %s, want 2m", defaults.upload)
	}

	override, err := httpTimeoutsFor(37 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]time.Duration{
		"request": override.request,
		"list":    override.list,
		"upload":  override.upload,
		"login":   override.login,
		"version": override.version,
	} {
		if value != 37*time.Second {
			t.Errorf("override %s = %s, want 37s", name, value)
		}
	}
	if _, err := httpTimeoutsFor(-time.Second); err == nil {
		t.Fatal("negative override unexpectedly succeeded")
	}
}

func TestHTTPTimeoutConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := SetHTTPTimeout(path, 37*time.Second); err != nil {
		t.Fatal(err)
	}
	got, err := ReadHTTPTimeout(path)
	if err != nil || got != 37*time.Second {
		t.Fatalf("ReadHTTPTimeout = %s, %v; want 37s", got, err)
	}
	if err := SetHTTPTimeout(path, 0); err != nil {
		t.Fatal(err)
	}
	got, err = ReadHTTPTimeout(path)
	if err != nil || got != 0 {
		t.Fatalf("cleared ReadHTTPTimeout = %s, %v; want zero", got, err)
	}
}

func TestNewClientUsesHTTPTimeoutConfigAndCLIOverride(t *testing.T) {
	withMockMasterKey(t)
	server := newQBTestServer(t)
	server.AppVersion = "v5.3.0"
	server.APIVersion = "2.16.2"
	withQBServerClientFactory(t, server)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := saveAuthFile(path, server.creds()); err != nil {
		t.Fatal(err)
	}
	if err := SetHTTPTimeout(path, 37*time.Second); err != nil {
		t.Fatal(err)
	}
	app, err := NewClientWithOutput(context.Background(), &Options{ConfigPath: path}, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("NewClientWithOutput(config) returned error: %v", err)
	}
	if got := app.client.httpTimeouts.request; got != 37*time.Second {
		t.Fatalf("configured request timeout = %s, want 37s", got)
	}
	app.Logout()

	app, err = NewClientWithOutput(context.Background(), &Options{ConfigPath: path, HTTPTimeout: 23 * time.Second}, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("NewClientWithOutput(override) returned error: %v", err)
	}
	if got := app.client.httpTimeouts.request; got != 23*time.Second {
		t.Fatalf("CLI request timeout = %s, want 23s", got)
	}
	app.Logout()
}

func TestHTTPTimeoutUsesLongerBudgetForFullQueue(t *testing.T) {
	c := newClient(Credentials{URL: "http://qbt.test"}, io.Discard)
	var normalDeadline, listDeadline time.Time
	c.httpClient.Transport = qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		deadline, ok := req.Context().Deadline()
		if !ok {
			t.Fatal("request has no deadline")
		}
		if err := req.Context().Err(); err != nil {
			t.Fatalf("request context canceled before transport response: %v", err)
		}
		if req.URL.Path == "/api/v2/torrents/info" {
			listDeadline = deadline
		} else {
			normalDeadline = deadline
		}
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header), Request: req}, nil
	})
	started := time.Now()
	if _, err := c.requestContext(context.Background(), http.MethodGet, "/api/v2/transfer/info", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.requestContext(context.Background(), http.MethodGet, "/api/v2/torrents/info", nil); err != nil {
		t.Fatal(err)
	}
	if remaining := time.Until(normalDeadline); remaining < 14*time.Second || remaining > 15*time.Second {
		t.Fatalf("normal request deadline has %s remaining, want about 15s", remaining)
	}
	if remaining := time.Until(listDeadline); remaining < 59*time.Second || remaining > 60*time.Second {
		t.Fatalf("full-list deadline has %s remaining, want about 60s", remaining)
	}
	if time.Since(started) > time.Second {
		t.Fatal("immediate request was preempted by its timeout")
	}
}

func TestHTTPTimeoutPreservesEarlierCallerDeadline(t *testing.T) {
	c := newClient(Credentials{URL: "http://qbt.test"}, io.Discard)
	c.httpTimeouts.request = time.Minute
	called := make(chan struct{})
	c.httpClient.Transport = qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		close(called)
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := c.requestContext(ctx, http.MethodGet, "/api/v2/transfer/info", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("request error = %v, want caller deadline", err)
	}
	select {
	case <-called:
	default:
		t.Fatal("transport was not called")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("request waited %s after caller deadline", elapsed)
	}
}

func TestHTTPTimeoutBoundsLoginAndUploadContexts(t *testing.T) {
	c := newClient(Credentials{URL: "http://qbt.test", User: "admin", Pass: "secret"}, io.Discard)
	c.httpTimeouts.login = 20 * time.Millisecond
	c.httpTimeouts.upload = 20 * time.Millisecond
	c.httpClient.Transport = qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	if err := c.loginContext(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("login error = %v, want deadline exceeded", err)
	}
	if _, _, err := c.doBodyRequestContext(context.Background(), http.MethodPost, "/api/v2/torrents/add", []byte("payload"), "text/plain"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("upload error = %v, want deadline exceeded", err)
	}
}
