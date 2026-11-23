package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	qbt "github.com/naterator/qbitctl/pkg/client"
)

func TestRenderCategoriesUsesStableJSONShapeAndFriendlyPaths(t *testing.T) {
	categories := []qbt.Category{
		{Name: "Linux\nISO", SavePath: "/downloads/linux", DownloadPath: json.RawMessage(`"/incomplete/linux"`)},
		{Name: "Default", SavePath: "/downloads/default", DownloadPath: json.RawMessage(`false`)},
	}
	var table bytes.Buffer
	if err := renderCategories(&table, categories, false); err != nil {
		t.Fatal(err)
	}
	if got := table.String(); !strings.Contains(got, "NAME") || !strings.Contains(got, "SAVE PATH") || !strings.Contains(got, "Linux ISO") || !strings.Contains(got, "default") || !strings.Contains(got, "/incomplete/linux") {
		t.Fatalf("category table = %q", got)
	}

	var encoded bytes.Buffer
	if err := renderCategories(&encoded, categories[:1], true); err != nil {
		t.Fatal(err)
	}
	var object []map[string]json.RawMessage
	if err := json.Unmarshal(encoded.Bytes(), &object); err != nil {
		t.Fatalf("category JSON = %q: %v", encoded.String(), err)
	}
	if _, ok := object[0]["save_path"]; !ok {
		t.Fatalf("category JSON omitted stable save_path field: %s", encoded.String())
	}
	if _, ok := object[0]["savePath"]; ok {
		t.Fatalf("category JSON exposed qB wire savePath instead of stable save_path: %s", encoded.String())
	}
}

func TestRenderTagsUsesStableJSONAndSanitizedText(t *testing.T) {
	var table bytes.Buffer
	if err := renderTags(&table, []string{"alpha\nrelease", "zeta"}, false); err != nil {
		t.Fatal(err)
	}
	if got, want := table.String(), "TAG\nalpha release\nzeta\n"; got != want {
		t.Fatalf("tag table = %q, want %q", got, want)
	}
	var encoded bytes.Buffer
	if err := renderTags(&encoded, []string{"alpha", "zeta"}, true); err != nil {
		t.Fatal(err)
	}
	if got, want := encoded.String(), "[\"alpha\",\"zeta\"]\n"; got != want {
		t.Fatalf("tag JSON = %q, want %q", got, want)
	}
}

func TestCategoryListCommandJSONUsesCatalogShape(t *testing.T) {
	server := newQBTestServer(t)
	server.Categories["Linux"] = map[string]any{
		"name": "Linux", "savePath": "/downloads/linux", "download_path": "/incomplete/linux",
	}
	withQBServerClientFactory(t, server)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--url", server.creds().URL, "--user", "admin", "--pass", "secret", "categories", "--json"})
	stdout, stderr := captureOutput(t, func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("categories --json: %v", err)
		}
	})
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	var categories []qbt.Category
	if err := json.Unmarshal([]byte(stdout), &categories); err != nil {
		t.Fatalf("decode categories JSON %q: %v", stdout, err)
	}
	if len(categories) != 1 || categories[0].Name != "Linux" || categories[0].SavePath != "/downloads/linux" {
		t.Fatalf("categories JSON = %#v", categories)
	}
}

func TestTagAddResolvesPrefixesAndUsesAssignmentEndpoint(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Hash: hash, Name: "Example"}}
	withQBServerClientFactory(t, server)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--url", server.creds().URL, "--user", "admin", "--pass", "secret", "tags", "add", "release", hash[:8]})
	stdout, stderr := captureOutput(t, func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("tags add: %v", err)
		}
	})
	if stdout != "" || stderr != "" {
		t.Fatalf("unexpected tag add output stdout=%q stderr=%q", stdout, stderr)
	}
	forms := server.Forms["/api/v2/torrents/addTags"]
	if len(forms) != 1 || forms[0].Get("hashes") != hash || forms[0].Get("tags") != "release" {
		t.Fatalf("addTags forms = %#v", forms)
	}
}
