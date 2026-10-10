package runtime

import (
	"strings"
	"testing"
)

func TestImageReferenceMatches(t *testing.T) {
	digest := strings.Repeat("a", 64)
	other := strings.Repeat("b", 64)
	for _, tc := range []struct {
		current, requested string
		equal              bool
	}{
		{"docker.io/library/alpine:3.22", "alpine:3.22", true},
		{"docker.io/library/alpine:latest", "alpine", true},
		{"docker.io/library/alpine:latest", "alpine:latest", true},
		{"ghcr.io/example/alpine:3.22", "alpine:3.22", false},
		{"docker.io/library/alpine:3.21", "alpine:3.22", false},
		{"docker.io/library/alpine:latest", "alpine:3.22", false},
		{"invalid reference", "alpine:3.22", false},
		{strings.Repeat("a", 64), strings.Repeat("a", 64), true},
		{"alpine@sha256:" + digest, "docker.io/library/alpine@sha256:" + digest, true},
		{"alpine@sha256:" + digest, "alpine@sha256:" + other, false},
		{"", "", false},
		{"", "alpine:latest", false},
	} {
		if got := ImageReferenceMatches(tc.current, tc.requested); got != tc.equal {
			t.Errorf("ImageReferenceMatches(%q, %q) = %v, want %v", tc.current, tc.requested, got, tc.equal)
		}
	}
}
