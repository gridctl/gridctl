package git

import (
	"errors"
	"fmt"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport"
)

func TestClassifyError(t *testing.T) {
	cases := []struct {
		name string
		in   error
		want error // nil = expect input returned unchanged
	}{
		{"nil", nil, nil},
		{"auth required", transport.ErrAuthenticationRequired, ErrAuthRequired},
		{"auth failed", transport.ErrAuthorizationFailed, ErrAuthFailed},
		{"not found", transport.ErrRepositoryNotFound, ErrNotFound},
		{"wrapped auth required", fmt.Errorf("upload-pack: %w", transport.ErrAuthenticationRequired), ErrAuthRequired},
		{"knownhosts mismatch", errors.New("knownhosts: key mismatch for host github.com"), ErrHostKeyMismatch},
		{"ssh agent missing (socket)", errors.New("dial unix $SSH_AUTH_SOCK: no such file"), ErrSSHAgentMissing},
		{"ssh agent missing (text)", errors.New("failed to connect to SSH agent"), ErrSSHAgentMissing},
		{"unknown", errors.New("random network fail"), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ClassifyError(c.in)
			if c.in == nil {
				if got != nil {
					t.Errorf("ClassifyError(nil) = %v, want nil", got)
				}
				return
			}
			if c.want == nil {
				// Unknown errors should round-trip unchanged.
				if got != c.in {
					t.Errorf("expected input returned unchanged, got %v", got)
				}
				return
			}
			if !errors.Is(got, c.want) {
				t.Errorf("expected classified error to satisfy errors.Is(%v), got %v", c.want, got)
			}
			// Original cause must also remain reachable.
			if !errors.Is(got, c.in) {
				t.Errorf("expected classified error to still wrap %v, got %v", c.in, got)
			}
		})
	}
}

func TestSentinelsAreDistinct(t *testing.T) {
	sentinels := []error{
		ErrAuthRequired, ErrAuthFailed, ErrNotFound,
		ErrHostKeyMismatch, ErrSSHAgentMissing,
		ErrEmptyToken, ErrProtocolMismatch,
	}
	seen := make(map[error]struct{}, len(sentinels))
	for _, s := range sentinels {
		if _, dup := seen[s]; dup {
			t.Errorf("duplicate sentinel identity: %v", s)
		}
		seen[s] = struct{}{}
	}
}
