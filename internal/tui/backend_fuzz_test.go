//go:build !plan9

package tui

import (
	"reflect"
	"testing"
)

func FuzzMainDataMerge(f *testing.F) {
	for _, seed := range []string{
		`{"rid":1,"full_update":true,"torrents":{"abc":{"name":"Ubuntu","size":1}}}`,
		`{"rid":2,"full_update":true,"torrents":{},"torrents_removed":[]}`,
		`{"rid":3,"full_update":false,"torrents":{"abc":{"progress":0.5}}}`,
		`null`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		left := &tuiClient{}
		leftResult, leftErr := left.applyMainData([]byte(input))
		if leftErr != nil {
			return
		}
		right := &tuiClient{}
		rightResult, rightErr := right.applyMainData([]byte(input))
		if rightErr != nil {
			t.Fatalf("same maindata input returned different errors: left=nil, right=%v", rightErr)
		}
		if !reflect.DeepEqual(leftResult, rightResult) {
			t.Fatalf("maindata merge is not deterministic: %#v != %#v", leftResult, rightResult)
		}
	})
}
