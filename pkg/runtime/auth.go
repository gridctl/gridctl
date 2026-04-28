package runtime

import (
	"fmt"

	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/gridctl/gridctl/pkg/config"
	gitpkg "github.com/gridctl/gridctl/pkg/git"
)

// AuthForSource turns a declarative SourceAuth block into a ready-to-use
// transport.AuthMethod. CredentialRef is resolved via the supplied resolver
// (typically vault-backed); raw tokens are produced only in memory and never
// persisted. A nil block, or Method == "" or "none", yields (nil, nil) — the
// public-repo path. The resolver may be nil only when no CredentialRef is
// present; otherwise we fail fast rather than silently fall through to an
// unauthenticated clone that 401s with a confusing error.
func AuthForSource(auth *config.SourceAuth, url string, resolver CredentialResolver) (transport.AuthMethod, error) {
	if auth == nil {
		return nil, nil
	}
	switch auth.Method {
	case "", "none":
		return nil, nil
	case "token":
		token, err := resolveCredentialRef(auth.CredentialRef, resolver)
		if err != nil {
			return nil, err
		}
		return gitpkg.HTTPSTokenAuth{Token: token}.AuthFor(url)
	case "ssh-agent":
		return gitpkg.SSHAgentAuth{User: auth.SSHUser}.AuthFor(url)
	case "ssh-key":
		return gitpkg.SSHKeyFileAuth{User: auth.SSHUser, KeyPath: auth.SSHKeyPath}.AuthFor(url)
	default:
		return nil, fmt.Errorf("unknown source.auth.method %q", auth.Method)
	}
}

func resolveCredentialRef(ref string, resolver CredentialResolver) (string, error) {
	if ref == "" {
		return "", nil
	}
	if resolver == nil {
		return "", fmt.Errorf("source.auth.credential_ref %q is set but no credential resolver is configured (is the vault unlocked?)", ref)
	}
	token, err := resolver(ref)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", ref, err)
	}
	return token, nil
}
