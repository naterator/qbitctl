//go:build !plan9

package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/naterator/qbitctl/pkg/client"
)

func TestSearchQueryAndProviderShareResultsHeading(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.page = searchPage
	m.query = "Ubuntu 24.04"
	m.searchPlugin = "prowlarr"
	m.replaceSearchResults([]client.SearchResult{{ID: 1, Name: "Ubuntu ISO"}})

	for _, width := range []int{76, 116} {
		header := m.viewHeader(width)
		if aboveDivider := ansi.Strip(strings.Join(header, "\n")); strings.Contains(aboveDivider, "Ubuntu") || strings.Contains(aboveDivider, "Provider:") {
			t.Fatalf("query and provider must be below the divider: %q", aboveDivider)
		}
		results := m.viewSearch(width, 12)
		line := ansi.Strip(results[0])
		if !strings.HasSuffix(line, "Provider: Prowlarr ") || ansi.StringWidth(line) != width {
			t.Fatalf("heading at width %d = %q, want the provider right-aligned below the divider", width, line)
		}
		if !strings.HasPrefix(line, " Search results   Query: Ubuntu 24.04") {
			t.Fatalf("heading at width %d = %q, want query on the left", width, line)
		}
		if summary := ansi.Strip(results[1]); !strings.Contains(summary, "1 result") {
			t.Fatalf("body at width %d = %q, want the result summary immediately after the heading", width, summary)
		}
	}

	body := strings.Join(m.viewSearchList(100, 12), "\n")
	if strings.Contains(body, "Provider:") {
		t.Fatalf("search body still contains provider row: %q", body)
	}

	m.searchPlugin = strings.Repeat("日本語", 30)
	if line := ansi.Strip(m.viewSearch(36, 12)[0]); !strings.HasSuffix(line, "… ") || ansi.StringWidth(line) != 36 {
		t.Fatalf("compact heading = %q, want truncated provider at the right edge", line)
	}
}
