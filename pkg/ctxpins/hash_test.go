package ctxpins

import (
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/skillpins"
)

func TestHash_LineEndingInvariant(t *testing.T) {
	base := []byte("# rules\n\nPrefer clarity.\n")
	variants := [][]byte{
		base,
		[]byte(strings.ReplaceAll(string(base), "\n", "\r\n")),
		[]byte(strings.TrimRight(string(base), "\n")),
		append(append([]byte{}, base...), '\n', '\n'),
	}
	want := Hash(base)
	for i, raw := range variants {
		got := Hash(raw)
		if got != want {
			t.Errorf("variant %d hash = %s, want %s", i, got, want)
		}
		if !strings.HasPrefix(got, "c1:") {
			t.Errorf("variant %d hash %s missing c1: prefix", i, got)
		}
	}
}

func TestCompositeHash_MatchesSkillPins(t *testing.T) {
	hash := Hash([]byte("body\n"))
	if got, want := CompositeHash(hash), skillpins.CompositeHash(hash, nil); got != want {
		t.Fatalf("CompositeHash = %s, want %s", got, want)
	}
}
