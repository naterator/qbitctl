//go:build !plan9

package tui

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/naterator/qbitctl/pkg/client"
)

func benchmarkTorrents(count int) map[string]client.TorrentInfo {
	torrents := make(map[string]client.TorrentInfo, count)
	for i := 0; i < count; i++ {
		hash := fmt.Sprintf("%040x", i+1)
		torrents[hash] = client.TorrentInfo{
			Hash: hash, Name: fmt.Sprintf("Torrent %05d", i),
			State: "downloading", Progress: float64(i%100) / 100,
		}
	}
	return torrents
}

func benchmarkMainDataBody(b *testing.B, count int) []byte {
	b.Helper()
	fields := make(map[string]map[string]any, count)
	for i := 0; i < count; i++ {
		hash := fmt.Sprintf("%040x", i+1)
		fields[hash] = map[string]any{
			"name":     fmt.Sprintf("Torrent %05d", i),
			"state":    "downloading",
			"progress": float64(i%100) / 100,
		}
	}
	body, err := json.Marshal(mainDataResponse{RID: int64Pointer(1), FullUpdate: true, Torrents: rawTorrentFields(fields)})
	if err != nil {
		b.Fatal(err)
	}
	return body
}

func int64Pointer(value int64) *int64 { return &value }

func rawTorrentFields(fields map[string]map[string]any) map[string]map[string]json.RawMessage {
	result := make(map[string]map[string]json.RawMessage, len(fields))
	for hash, values := range fields {
		encoded := make(map[string]json.RawMessage, len(values))
		for key, value := range values {
			data, err := json.Marshal(value)
			if err != nil {
				panic(err)
			}
			encoded[key] = data
		}
		result[hash] = encoded
	}
	return result
}

func BenchmarkRebuildQueue5k(b *testing.B) {
	torrents := benchmarkTorrents(5000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		list := mainDataTorrentList(torrents)
		if len(list) != len(torrents) {
			b.Fatalf("mainDataTorrentList returned %d torrents, want %d", len(list), len(torrents))
		}
	}
}

func BenchmarkMainDataMerge5k(b *testing.B) {
	body := benchmarkMainDataBody(b, 5000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		adapter := &tuiClient{}
		list, err := adapter.applyMainData(body)
		if err != nil {
			b.Fatal(err)
		}
		if len(list) != 5000 {
			b.Fatalf("applyMainData returned %d torrents, want 5000", len(list))
		}
	}
}
