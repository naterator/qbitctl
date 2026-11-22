package searchresults

import "testing"

func FuzzParseSize(f *testing.F) {
	for _, seed := range []string{"0", "1KB", "1.5 MiB", ".25gb", "18446744073709551616", "bad"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		value, err := ParseSize(input)
		if err != nil {
			return
		}
		if value < 0 {
			t.Fatalf("ParseSize(%q) returned a negative size %d", input, value)
		}
	})
}
