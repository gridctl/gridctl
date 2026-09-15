package runs

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxIdentifierBytes is the maximum stored length of a target or label
// selector. Oversized values are omitted with a fixed indicator.
const MaxIdentifierBytes = 128

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// BoundIdentifier returns a stored selector and an omission indicator.
// Valid values are returned as-is. Malformed or oversized values yield an
// empty selector and a fixed indicator; the rejected text is discarded.
func BoundIdentifier(value string) (stored, omitted string) {
	if value == "" {
		return "", ""
	}
	if len(value) > MaxIdentifierBytes {
		return "", OmittedOversized
	}
	if !utf8.ValidString(value) {
		return "", OmittedMalformed
	}
	if !validSelector(value) {
		return "", OmittedMalformed
	}
	return value, ""
}

func validSelector(value string) bool {
	if strings.Contains(value, "://") {
		return false
	}
	for _, r := range value {
		if r == '/' || r == '\\' {
			return false
		}
		if r < 32 || r == 127 {
			return false
		}
		if unicode.IsSpace(r) {
			return false
		}
		if r == '"' || r == '\'' || r == '`' {
			return false
		}
	}
	return true
}
