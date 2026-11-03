package contexts

import (
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/ctxpins"
	"github.com/gridctl/gridctl/pkg/project"
)

// TestPinHashParity asserts that ctxpins.Hash and the lockfile's canonical
// content hash agree on the unprefixed digest. The two prefixes differ
// (c1: versus sha256:) so a scheme change on either side cannot be mistaken
// for the other, but the bytes they digest must stay the same.
func TestPinHashParity(t *testing.T) {
	inputs := [][]byte{
		[]byte("# rules\n"),
		[]byte("# rules\r\n"),
		[]byte("# rules"),
		[]byte("# rules\n\n\n"),
		[]byte("line\r\nline\r\n\r\n"),
	}
	for _, raw := range inputs {
		pin := strings.TrimPrefix(ctxpins.Hash(raw), "c1:")
		lock := strings.TrimPrefix(canonicalContentHash(string(raw)), project.HashScheme)
		if pin != lock {
			t.Fatalf("hash mismatch for %q\npin  %s\nlock %s", raw, pin, lock)
		}
	}
}
