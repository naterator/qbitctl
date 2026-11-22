package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/naterator/qbitctl/internal/qbtest"
	"github.com/naterator/qbitctl/internal/searchresults"
	qbt "github.com/naterator/qbitctl/pkg/client"
)

func TestRenderSearchResultsKeepsPipedOutputStable(t *testing.T) {
	results := []qbt.SearchResult{{
		ID:       123,
		Name:     "Ubuntu\nDesktop",
		Size:     1536,
		Seeders:  10,
		Leechers: 2,
		Source:   "tracker.example",
	}}
	var output bytes.Buffer
	if err := renderSearchTable(&output, results, false, 0); err != nil {
		t.Fatal(err)
	}
	want := "ID\tName\tSize (bytes)\tSeeders\tLeechers\tSource\n123\tUbuntu Desktop\t1536\t10\t2\ttracker.example\n"
	if output.String() != want {
		t.Fatalf("piped output = %q, want %q", output.String(), want)
	}
}

func TestRenderSearchResultsUsesHumanSizeAndTerminalWidth(t *testing.T) {
	results := []qbt.SearchResult{{
		ID:     1,
		Name:   "A very long release title that should be clipped",
		Size:   1536,
		Source: "tracker.example",
	}}
	var output bytes.Buffer
	if err := renderSearchTable(&output, results, true, 50); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	if !strings.Contains(text, "Size") || !strings.Contains(text, "1.5 KiB") {
		t.Fatalf("terminal output = %q, want aligned human-readable size", text)
	}
	if strings.Contains(text, results[0].Name) {
		t.Fatalf("terminal output did not truncate the title: %q", text)
	}
	if !strings.Contains(text, "…") {
		t.Fatalf("terminal output omitted the truncation marker: %q", text)
	}
}

func TestRenderSearchPluginsUsesFullNameFallbackAndSanitizesRemoteText(t *testing.T) {
	plugins := []qbt.SearchPlugin{
		{Name: "jackett", Enabled: true},
		{Name: "id\twith-control", FullName: "Name\nwith-control", Enabled: false},
	}
	var output bytes.Buffer
	if err := renderSearchPlugins(&output, plugins, false); err != nil {
		t.Fatal(err)
	}
	want := "Name\tID\tEnabled\n" +
		"jackett\tjackett\ttrue\n" +
		"Name with-control\tid with-control\tfalse\n"
	if output.String() != want {
		t.Fatalf("plugin table = %q, want %q", output.String(), want)
	}
}

func TestParseSearchCommandOptions(t *testing.T) {
	options, err := parseSearchCommandOptions("seeders-desc", 10, true, "500MB", true, "2GiB", true, "ubuntu tracker", 5)
	if err != nil {
		t.Fatal(err)
	}
	if options.sort != searchresults.SeedersDesc || options.limit != 5 || options.filter.MinSeeders == nil || *options.filter.MinSeeders != 10 {
		t.Fatalf("parsed options = %#v", options)
	}
	if _, err := parseSearchCommandOptions("not-a-sort", 0, false, "", false, "", false, "", 0); err == nil {
		t.Fatal("invalid sort was accepted")
	} else {
		var coded *qbt.CodedError
		if !errors.As(err, &coded) || coded.Code != exitBadArgs {
			t.Fatalf("invalid sort error = %v, want ExitBadArgs", err)
		}
	}
	if _, err := parseSearchCommandOptions("original", 0, false, "5 parsecs", true, "", false, "", 0); err == nil {
		t.Fatal("invalid minimum size was accepted")
	}
	if _, err := parseSearchCommandOptions("original", 0, false, "4GiB", true, "2GiB", true, "", 0); err == nil {
		t.Fatal("inverted size range was accepted")
	}
}

type countingReader struct {
	reader io.Reader
	reads  int
}

func (r *countingReader) Read(p []byte) (int, error) {
	r.reads++
	return r.reader.Read(p)
}

func TestSearchInvalidOptionsFailBeforeAuthAndPasswordRead(t *testing.T) {
	var loginCalls int
	restore := qbt.SetHTTPClientFactoryForTesting(func(jar http.CookieJar) *http.Client {
		return &http.Client{Jar: jar, Transport: qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/api/v2/auth/login" {
				loginCalls++
			}
			return nil, errors.New("search validation should prevent authentication")
		})}
	})
	t.Cleanup(restore)

	for _, args := range [][]string{
		{"--url", "http://qbt.test", "--user", "admin", "--pass-stdin", "search", "Ubuntu", "--sort", "invalid"},
		{"--url", "http://qbt.test", "--user", "admin", "--pass-stdin", "search", "Ubuntu", "--min-size", "5 parsecs"},
	} {
		reader := &countingReader{reader: strings.NewReader("secret\nremaining")}
		root := newRootCmd()
		root.SetIn(reader)
		root.SetArgs(args)
		err := root.ExecuteContext(t.Context())
		var coded *qbt.CodedError
		if !errors.As(err, &coded) || coded.Code != exitBadArgs {
			t.Fatalf("invalid search args returned %v, want ExitBadArgs", err)
		}
		if reader.reads != 0 {
			t.Fatalf("invalid search args read password stdin %d times", reader.reads)
		}
	}
	if loginCalls != 0 {
		t.Fatalf("invalid search args attempted %d logins", loginCalls)
	}
}
