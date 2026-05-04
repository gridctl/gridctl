package git

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	gossh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

// Protocol describes how a git URL is accessed.
type Protocol string

const (
	ProtocolHTTPS   Protocol = "https"
	ProtocolSSH     Protocol = "ssh"
	ProtocolLocal   Protocol = "local"
	ProtocolUnknown Protocol = "unknown"
)

// scpSyntax matches SCP-style git URLs (user@host:path).
var scpSyntax = regexp.MustCompile(`^[a-zA-Z0-9._-]+@[a-zA-Z0-9._-]+:`)

// DetectProtocol classifies a git remote URL by its scheme.
func DetectProtocol(url string) Protocol {
	switch {
	case url == "":
		return ProtocolUnknown
	case strings.HasPrefix(url, "https://"), strings.HasPrefix(url, "http://"):
		return ProtocolHTTPS
	case strings.HasPrefix(url, "ssh://"):
		return ProtocolSSH
	case scpSyntax.MatchString(url):
		return ProtocolSSH
	case strings.HasPrefix(url, "file://"),
		strings.HasPrefix(url, "/"),
		strings.HasPrefix(url, "./"),
		strings.HasPrefix(url, "../"),
		strings.HasPrefix(url, "~/"):
		return ProtocolLocal
	default:
		return ProtocolUnknown
	}
}

// Auther returns a transport.AuthMethod appropriate for the given URL.
// A nil AuthMethod with a nil error means "no credentials required"; a
// non-nil error surfaces misconfiguration (wrong protocol, missing agent,
// empty token) so callers don't silently fall through to an unauthenticated
// clone that then fails with a cryptic "repository not found".
type Auther interface {
	AuthFor(url string) (transport.AuthMethod, error)
}

// NoAuth produces no credentials for any URL. Use it for public repositories
// or when ambient go-git mechanisms handle auth transparently.
type NoAuth struct{}

// AuthFor always returns (nil, nil).
func (NoAuth) AuthFor(string) (transport.AuthMethod, error) { return nil, nil }

// HTTPSTokenAuth sends a token as HTTP basic-auth using the literal username
// "x-access-token" with the token in the password slot — the shape required
// by GitHub App installation tokens and accepted uniformly by GitHub PATs,
// GitLab, and Bitbucket.
type HTTPSTokenAuth struct {
	Token string
}

// AuthFor returns an *http.BasicAuth carrying the token, or an error if the
// token is empty or the URL is not HTTPS.
func (a HTTPSTokenAuth) AuthFor(url string) (transport.AuthMethod, error) {
	if a.Token == "" {
		return nil, ErrEmptyToken
	}
	if p := DetectProtocol(url); p != ProtocolHTTPS {
		return nil, fmt.Errorf("%w: HTTPSTokenAuth requires an https:// URL, got %q (%s)", ErrProtocolMismatch, url, p)
	}
	return &http.BasicAuth{Username: "x-access-token", Password: a.Token}, nil
}

// SSHAgentAuth delegates authentication to the user's running ssh-agent.
// The User field defaults to "git" when empty.
type SSHAgentAuth struct {
	User string
}

// AuthFor returns a PublicKeysCallback backed by the ambient ssh-agent, or
// ErrSSHAgentMissing if no agent socket is available.
func (a SSHAgentAuth) AuthFor(url string) (transport.AuthMethod, error) {
	if p := DetectProtocol(url); p != ProtocolSSH {
		return nil, fmt.Errorf("%w: SSHAgentAuth requires an ssh:// or user@host:path URL, got %q (%s)", ErrProtocolMismatch, url, p)
	}
	if os.Getenv("SSH_AUTH_SOCK") == "" {
		return nil, ErrSSHAgentMissing
	}
	user := a.User
	if user == "" {
		user = gossh.DefaultUsername
	}
	cb, err := gossh.NewSSHAgentAuth(user)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSSHAgentMissing, err)
	}
	return cb, nil
}

// SSHKeyFileAuth authenticates using an on-disk private key file. The User
// field defaults to "git" when empty.
//
// Host-key verification uses go-git's default behavior for this PR; a
// stricter TOFU / accept-new host-key policy and explicit KnownHostsPath
// handling will be wired through in a follow-up change. KnownHostsPath is
// reserved for that future work.
type SSHKeyFileAuth struct {
	User           string
	KeyPath        string
	Passphrase     string
	KnownHostsPath string // reserved for a follow-up change
}

// AuthFor loads the private key file and returns an SSH PublicKeys auth.
func (a SSHKeyFileAuth) AuthFor(url string) (transport.AuthMethod, error) {
	if p := DetectProtocol(url); p != ProtocolSSH {
		return nil, fmt.Errorf("%w: SSHKeyFileAuth requires an ssh:// or user@host:path URL, got %q (%s)", ErrProtocolMismatch, url, p)
	}
	user := a.User
	if user == "" {
		user = gossh.DefaultUsername
	}
	auth, err := gossh.NewPublicKeysFromFile(user, a.KeyPath, a.Passphrase)
	if err != nil {
		return nil, fmt.Errorf("loading ssh key from %q: %w", a.KeyPath, err)
	}
	return auth, nil
}
