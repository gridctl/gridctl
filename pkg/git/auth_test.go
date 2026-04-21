package git

import (
	"errors"
	"os"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

func TestDetectProtocol(t *testing.T) {
	cases := []struct {
		url  string
		want Protocol
	}{
		{"", ProtocolUnknown},
		{"https://github.com/org/repo", ProtocolHTTPS},
		{"http://gitlab.internal/org/repo", ProtocolHTTPS},
		{"ssh://git@github.com/org/repo", ProtocolSSH},
		{"git@github.com:org/repo.git", ProtocolSSH},
		{"user.name@host-1.example:path/repo", ProtocolSSH},
		{"/var/local/cache/repo", ProtocolLocal},
		{"./local-repo", ProtocolLocal},
		{"../sibling-repo", ProtocolLocal},
		{"file:///var/repos/r", ProtocolLocal},
		{"~/my-repo", ProtocolLocal},
		{"garbage", ProtocolUnknown},
	}
	for _, c := range cases {
		if got := DetectProtocol(c.url); got != c.want {
			t.Errorf("DetectProtocol(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

func TestNoAuth(t *testing.T) {
	auth, err := NoAuth{}.AuthFor("https://example.com/repo")
	if err != nil || auth != nil {
		t.Errorf("NoAuth should return (nil, nil), got (%v, %v)", auth, err)
	}
}

func TestHTTPSTokenAuth_EmptyToken(t *testing.T) {
	_, err := HTTPSTokenAuth{}.AuthFor("https://example.com/repo")
	if !errors.Is(err, ErrEmptyToken) {
		t.Errorf("expected ErrEmptyToken, got %v", err)
	}
}

func TestHTTPSTokenAuth_WrongProtocol(t *testing.T) {
	_, err := HTTPSTokenAuth{Token: "x"}.AuthFor("git@github.com:a/b")
	if !errors.Is(err, ErrProtocolMismatch) {
		t.Errorf("expected ErrProtocolMismatch, got %v", err)
	}
}

func TestHTTPSTokenAuth_HappyPath(t *testing.T) {
	auth, err := HTTPSTokenAuth{Token: "abc123"}.AuthFor("https://github.com/a/b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ba, ok := auth.(*http.BasicAuth)
	if !ok {
		t.Fatalf("expected *http.BasicAuth, got %T", auth)
	}
	if ba.Username != "abc123" || ba.Password != "" {
		t.Errorf("unexpected basic auth: user=%q pass=%q", ba.Username, ba.Password)
	}
}

func TestSSHAgentAuth_WrongProtocol(t *testing.T) {
	_, err := SSHAgentAuth{}.AuthFor("https://github.com/a/b")
	if !errors.Is(err, ErrProtocolMismatch) {
		t.Errorf("expected ErrProtocolMismatch, got %v", err)
	}
}

func TestSSHAgentAuth_MissingAgent(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	_, err := SSHAgentAuth{}.AuthFor("git@github.com:a/b")
	if !errors.Is(err, ErrSSHAgentMissing) {
		t.Errorf("expected ErrSSHAgentMissing, got %v", err)
	}
}

func TestSSHAgentAuth_HappyPath(t *testing.T) {
	if os.Getenv("SSH_AUTH_SOCK") == "" {
		t.Skip("SSH_AUTH_SOCK unset; skipping agent-backed test")
	}
	if _, err := (SSHAgentAuth{}).AuthFor("git@github.com:a/b"); err != nil {
		t.Errorf("unexpected error when agent is available: %v", err)
	}
}

func TestSSHKeyFileAuth_WrongProtocol(t *testing.T) {
	_, err := SSHKeyFileAuth{KeyPath: "/dev/null"}.AuthFor("https://github.com/a/b")
	if !errors.Is(err, ErrProtocolMismatch) {
		t.Errorf("expected ErrProtocolMismatch, got %v", err)
	}
}

func TestSSHKeyFileAuth_MissingFile(t *testing.T) {
	_, err := SSHKeyFileAuth{KeyPath: "/definitely/does/not/exist/keyfile"}.AuthFor("git@github.com:a/b")
	if err == nil {
		t.Error("expected error loading nonexistent key file")
	}
}
