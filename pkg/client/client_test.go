package client

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestClientLoginFailureOnNon200(t *testing.T) {
	server := newQBTestServer(t)
	server.StatusQueue["/api/v2/auth/login"] = []int{http.StatusUnauthorized}
	withQBServerClientFactory(t, server)

	err := newClient(server.creds(), os.Stderr).login()
	if err == nil {
		t.Fatal("login unexpectedly succeeded")
	}
}

func TestClientRequestReloginOnForbidden(t *testing.T) {
	server := newQBTestServer(t)
	server.StatusQueue["/api/v2/torrents/info"] = []int{http.StatusForbidden}
	server.Torrents = []TorrentInfo{{Name: "Ubuntu", Hash: "0123456789abcdef0123456789abcdef01234567"}}
	withQBServerClientFactory(t, server)

	client := newClient(server.creds(), os.Stderr)
	body, err := client.requestContext(context.Background(), http.MethodGet, "/api/v2/torrents/info", nil)
	if err != nil {
		t.Fatalf("request returned error: %v", err)
	}
	if server.LoginCalls != 1 {
		t.Fatalf("loginCalls = %d, want 1", server.LoginCalls)
	}
	if !strings.Contains(string(body), "Ubuntu") {
		t.Fatalf("request body = %q, want torrent JSON", string(body))
	}
}
