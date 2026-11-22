package searchresults

import (
	"slices"
	"testing"

	"github.com/naterator/qbitctl/pkg/client"
)

func TestParseSort(t *testing.T) {
	for _, want := range []Sort{Original, SeedersDesc, SeedersAsc, SizeDesc, SizeAsc} {
		got, err := ParseSort(" " + string(want) + " ")
		if err != nil || got != want {
			t.Errorf("ParseSort(%q) = %q, %v; want %q", want, got, err, want)
		}
	}
	if _, err := ParseSort("popular"); err == nil {
		t.Fatal("ParseSort accepted an unknown order")
	}
}

func TestSortStabilityAndUnknownMetrics(t *testing.T) {
	results := []client.SearchResult{
		{ID: 1, Name: "first", Seeders: 10, Size: 100},
		{ID: 2, Name: "second", Seeders: 10, Size: -1},
		{ID: 3, Name: "unknown", Seeders: -1, Size: 500},
		{ID: 4, Name: "third", Seeders: 5, Size: 200},
	}
	for _, tt := range []struct {
		sort Sort
		ids  []int64
	}{
		{SeedersDesc, []int64{1, 2, 4, 3}},
		{SeedersAsc, []int64{4, 1, 2, 3}},
		{SizeDesc, []int64{3, 4, 1, 2}},
		{SizeAsc, []int64{1, 4, 3, 2}},
		{Original, []int64{1, 2, 3, 4}},
	} {
		got := Apply(results, Filter{}, tt.sort, 0)
		ids := make([]int64, 0, len(got))
		for _, result := range got {
			ids = append(ids, result.ID)
		}
		if !slices.Equal(ids, tt.ids) {
			t.Errorf("%s order = %v, want %v", tt.sort, ids, tt.ids)
		}
	}
	if results[0].ID != 1 {
		t.Fatal("Apply modified the input slice")
	}
}

func TestParseSize(t *testing.T) {
	for _, tt := range []struct {
		raw  string
		want int64
	}{
		{"42", 42},
		{"42.0 B", 42},
		{"1.5 mb", 1_500_000},
		{"1.5 MiB", 1_572_864},
		{"1.2 GiB", 1_288_490_189},
		{".5 GiB", 536_870_912},
		{"2 GiB", 2 << 30},
	} {
		got, err := ParseSize(tt.raw)
		if err != nil || got != tt.want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", tt.raw, got, err, tt.want)
		}
	}
	for _, raw := range []string{"", "-1", "1e3", "1 EB", "0.5 B", "8 EiB"} {
		if _, err := ParseSize(raw); err == nil {
			t.Errorf("ParseSize(%q) accepted invalid input", raw)
		}
	}
}

func TestFilterAndApply(t *testing.T) {
	minSeeders, minSize, maxSize := int64(10), int64(2<<30), int64(4<<30)
	filter := Filter{Query: "UBUNTU tracker 002", MinSeeders: &minSeeders, MinSize: &minSize, MaxSize: &maxSize}
	results := []client.SearchResult{
		{ID: 1002, Name: "Ubuntu Server", Source: "tracker.example", Seeders: 12, Size: 3 << 30},
		{ID: 1001, Name: "Ubuntu Desktop", Source: "tracker.example", Seeders: 20, Size: 3 << 30},
		{ID: 1003, Name: "Ubuntu Server", Source: "other.example", Seeders: 30, Size: 5 << 30},
	}
	if err := filter.Validate(); err != nil {
		t.Fatal(err)
	}
	got := Apply(results, filter, SeedersDesc, 0)
	if len(got) != 1 || got[0].ID != 1002 {
		t.Fatalf("filtered results = %#v, want result 1002", got)
	}
	if !(Filter{Query: "1002"}).Matches(results[0]) {
		t.Fatal("test setup did not exercise ID matching")
	}
}

func TestFilterValidationAndLimit(t *testing.T) {
	min, max := int64(5), int64(4)
	if err := (Filter{MinSize: &min, MaxSize: &max}).Validate(); err == nil {
		t.Fatal("Filter.Validate accepted an inverted range")
	}
	results := []client.SearchResult{{ID: 1}, {ID: 2}, {ID: 3}}
	if got := Apply(results, Filter{}, Original, 2); len(got) != 2 || got[1].ID != 2 {
		t.Fatalf("limited results = %#v", got)
	}
	if got := Apply(results, Filter{}, Original, 0); len(got) != 3 {
		t.Fatalf("zero limit returned %d results", len(got))
	}
}

func TestFormatSize(t *testing.T) {
	for _, tt := range []struct {
		value int64
		want  string
	}{
		{-1, "?"},
		{42, "42 B"},
		{1536, "1.5 KiB"},
		{2 << 30, "2.0 GiB"},
	} {
		if got := FormatSize(tt.value); got != tt.want {
			t.Errorf("FormatSize(%d) = %q, want %q", tt.value, got, tt.want)
		}
	}
}
