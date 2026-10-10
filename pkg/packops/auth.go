package packops

import (
	"fmt"

	gitpkg "github.com/gridctl/gridctl/pkg/git"
	"github.com/gridctl/gridctl/pkg/pack"
	"github.com/gridctl/gridctl/pkg/skills"
)

// resolveSourceAuth applies the per-source auth order: an explicit caller
// override, stored auth on the member lock source, the manifest auth block,
// the caller's ssh-key when the source URL is SSH and nothing above applied,
// then ambient. A token or vault reference on the primary pack auth is never
// consulted. Token material is resolved here and is not returned for logging.
func resolveSourceAuth(packName, sourceName string, spec pack.Source, override *skills.AuthConfig, caller skills.AuthConfig, lf *skills.LockFile, resolver skills.CredentialResolver) (skills.AuthConfig, error) {
	if override != nil {
		return *override, nil
	}
	if lf != nil {
		if src, ok := lf.Sources[packName+"/"+sourceName]; ok {
			stored := src.StoredAuth()
			if stored.CredentialRef != "" || stored.Method != "" || stored.SSHKeyPath != "" {
				return skills.ResolveStoredAuth(stored, resolver)
			}
		}
	}
	if spec.Auth != nil {
		return manifestSourceAuth(sourceName, spec.Auth, resolver)
	}
	if caller.Method == "ssh-key" && caller.SSHKeyPath != "" && gitpkg.DetectProtocol(spec.Repo) == gitpkg.ProtocolSSH {
		return caller, nil
	}
	return skills.AuthConfig{}, nil
}

func manifestSourceAuth(sourceName string, auth *pack.SourceAuth, resolver skills.CredentialResolver) (skills.AuthConfig, error) {
	switch auth.Method {
	case "ssh-key":
		// A manifest cannot carry a key path. ssh-key without one means
		// the agent, with the declared user.
		return skills.AuthConfig{Method: "ssh-agent", SSHUser: auth.SSHUser}, nil
	case "ssh-agent":
		return skills.AuthConfig{Method: "ssh-agent", SSHUser: auth.SSHUser}, nil
	case "token":
		if auth.CredentialRef == "" {
			return skills.AuthConfig{}, fmt.Errorf("source %q auth method token requires credential_ref", sourceName)
		}
		if resolver == nil {
			return skills.AuthConfig{}, fmt.Errorf("source %q credential %q requires a resolver", sourceName, auth.CredentialRef)
		}
		token, err := resolver(auth.CredentialRef)
		if err != nil {
			return skills.AuthConfig{}, fmt.Errorf("source %q: %w", sourceName, err)
		}
		return skills.AuthConfig{Method: "token", Token: token, CredentialRef: auth.CredentialRef}, nil
	default:
		return skills.AuthConfig{}, fmt.Errorf("source %q auth.method %q must be ssh-key, ssh-agent, or token", sourceName, auth.Method)
	}
}
