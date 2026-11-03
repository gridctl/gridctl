package ctxpins

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/gridctl/gridctl/pkg/skillpins"
)

// schemePrefix marks digests computed under the current context hash
// scheme: raw fragment bytes after CRLF normalization and trailing-newline
// trim. The prefix travels with every stored digest so a future scheme
// change never presents as drift.
const schemePrefix = "c1:"

// Hash returns the pin digest of raw fragment bytes. Normalization matches
// the projection lockfile's canonical content hash (CRLF to LF, trailing
// newlines trimmed) so stale and pin drift agree on whether content changed.
func Hash(raw []byte) string {
	normalized := strings.ReplaceAll(string(raw), "\r\n", "\n")
	normalized = strings.TrimRight(normalized, "\n")
	sum := sha256.Sum256([]byte(normalized))
	return schemePrefix + hex.EncodeToString(sum[:])
}

// CompositeHash folds one fragment hash into the approval-binding
// fingerprint shared with skill pins. --expect values are comparable
// across kinds because both call skillpins.CompositeHash.
func CompositeHash(hash string) string {
	return skillpins.CompositeHash(hash, nil)
}
