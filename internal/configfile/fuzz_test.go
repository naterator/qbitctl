package configfile

import "testing"

func FuzzDecodeConfig(f *testing.F) {
	for _, seed := range []string{`{}`, `{"url":"https://example.test"}`, `null`, `[]`, `{"unterminated":`, ""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		object, err := decode("fuzz", []byte(input))
		if err == nil && object == nil {
			t.Fatal("decode returned a nil object without an error")
		}
	})
}
