package client

import (
	"strings"
	"testing"
)

func TestCategoryCatalogSortsAndEditPreservesDownloadPath(t *testing.T) {
	server := newQBTestServer(t)
	server.Categories["zeta"] = map[string]any{"name": "zeta", "savePath": "/downloads/z", "download_path": "/incomplete/z"}
	server.Categories["Alpha"] = map[string]any{"name": "Alpha", "savePath": "/downloads/a", "download_path": false}
	app := server.newApp()

	categories, err := app.GetCategories()
	if err != nil {
		t.Fatalf("GetCategories: %v", err)
	}
	if len(categories) != 2 || categories[0].Name != "Alpha" || categories[1].Name != "zeta" {
		t.Fatalf("categories = %#v, want stable case-insensitive order", categories)
	}
	if err := app.EditCategory("zeta", "/downloads/new"); err != nil {
		t.Fatalf("EditCategory: %v", err)
	}
	form := server.Forms["/api/v2/torrents/editCategory"][0]
	if form.Get("category") != "zeta" || form.Get("savePath") != "/downloads/new" || form.Get("downloadPathEnabled") != "true" || form.Get("downloadPath") != "/incomplete/z" {
		t.Fatalf("editCategory form = %#v, want preserved download path", form)
	}
	enabled := false
	newDownloadPath := ""
	if err := app.EditCategoryWithPaths("zeta", "", &newDownloadPath, &enabled); err != nil {
		t.Fatalf("download-only EditCategoryWithPaths: %v", err)
	}
	form = server.Forms["/api/v2/torrents/editCategory"][1]
	if form.Get("savePath") != "/downloads/new" || form.Get("downloadPathEnabled") != "false" || form.Get("downloadPath") != "" {
		t.Fatalf("download-only edit form = %#v, want preserved save path and disabled override", form)
	}
}

func TestCategoryAndTagMutationsUseDistinctAPIs(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	app := server.newApp()
	enabled := true
	downloadPath := "/incomplete/linux"
	if err := app.CreateCategoryWithPaths("linux", "/downloads/linux", &downloadPath, &enabled); err != nil {
		t.Fatalf("CreateCategoryWithPaths: %v", err)
	}
	createCategory := server.Forms["/api/v2/torrents/createCategory"][0]
	if createCategory.Get("category") != "linux" || createCategory.Get("savePath") != "/downloads/linux" || createCategory.Get("downloadPathEnabled") != "true" || createCategory.Get("downloadPath") != downloadPath {
		t.Fatalf("createCategory form = %#v", createCategory)
	}
	if err := app.RemoveCategories("linux", "old"); err != nil {
		t.Fatalf("RemoveCategories: %v", err)
	}
	if got := server.Forms["/api/v2/torrents/removeCategories"][0].Get("categories"); got != "linux\nold" {
		t.Fatalf("remove categories = %q", got)
	}
	if err := app.CreateTags("Linux", "iso"); err != nil {
		t.Fatalf("CreateTags: %v", err)
	}
	if got := server.Forms["/api/v2/torrents/createTags"][0].Get("tags"); got != "Linux,iso" {
		t.Fatalf("create tags = %q", got)
	}
	if err := app.DeleteTags("iso"); err != nil {
		t.Fatalf("DeleteTags: %v", err)
	}
	if got := server.Forms["/api/v2/torrents/deleteTags"][0].Get("tags"); got != "iso" {
		t.Fatalf("delete tags = %q", got)
	}
	if err := app.AddTorrentTags(hash, "Linux,iso"); err != nil {
		t.Fatalf("AddTorrentTags: %v", err)
	}
	if err := app.RemoveTorrentTags(hash, "iso"); err != nil {
		t.Fatalf("RemoveTorrentTags: %v", err)
	}
	if got := server.Forms["/api/v2/torrents/addTags"][0].Get("hashes"); got != hash {
		t.Fatalf("add torrent tags hashes = %q", got)
	}
	if got := server.Forms["/api/v2/torrents/removeTags"][0].Get("hashes"); got != hash {
		t.Fatalf("remove torrent tags hashes = %q", got)
	}
}

func TestGetTagsSortsAndValidatesTagSeparators(t *testing.T) {
	server := newQBTestServer(t)
	server.Tags = []string{"zeta", "Alpha", "alpha", ""}
	app := server.newApp()
	tags, err := app.GetTags()
	if err != nil {
		t.Fatalf("GetTags: %v", err)
	}
	if strings.Join(tags, ",") != "Alpha,alpha,zeta" {
		t.Fatalf("tags = %#v, want stable sorted unique tags", tags)
	}
	if err := app.CreateTags("bad,tag"); err == nil {
		t.Fatal("CreateTags accepted a comma-containing tag")
	}
	if len(server.Forms["/api/v2/torrents/createTags"]) != 0 {
		t.Fatal("invalid tag request reached server")
	}
}
