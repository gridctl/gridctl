package varrun

import (
	"bytes"
	"testing"
)

func TestNewRedactor_SplitAndPrefixMatches(t *testing.T) {
	var out bytes.Buffer
	r := NewRedactor(&out, []string{"abcdefgh", "abcdefghij", "bcdefghi"})
	for _, part := range []string{"xabc", "defgh", "ij ybcdef", "ghi z"} {
		if _, err := r.Write([]byte(part)); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "x[REDACTED] y[REDACTED] z"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
