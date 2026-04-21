package skills

import (
	"errors"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport/http"

	gitpkg "github.com/gridctl/gridctl/pkg/git"
)

func TestBuildAuther(t *testing.T) {
	cases := []struct {
		name    string
		cfg     AuthConfig
		wantErr bool
	}{
		{"empty method returns NoAuth", AuthConfig{}, false},
		{"explicit none", AuthConfig{Method: "none"}, false},
		{"token", AuthConfig{Method: "token", Token: "abc"}, false},
		{"ssh-agent", AuthConfig{Method: "ssh-agent", SSHUser: "git"}, false},
		{"ssh-key", AuthConfig{Method: "ssh-key", SSHKeyPath: "/does/not/exist"}, false},
		{"unknown method", AuthConfig{Method: "oauth"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			auther, err := BuildAuther(c.cfg)
			if c.wantErr {
				if err == nil {
					t.Errorf("expected error for %q, got %v", c.cfg.Method, auther)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if auther == nil {
				t.Fatal("expected non-nil Auther")
			}
		})
	}
}

func TestResolveAuther_ExplicitWins(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "env-token")
	auther, err := resolveAuther(AuthConfig{Method: "token", Token: "explicit"}, "https://example.com/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	auth, err := auther.AuthFor("https://example.com/repo")
	if err != nil {
		t.Fatalf("AuthFor: %v", err)
	}
	ba, ok := auth.(*http.BasicAuth)
	if !ok {
		t.Fatalf("expected *http.BasicAuth, got %T", auth)
	}
	if ba.Username != "explicit" {
		t.Errorf("expected explicit token to win over env, got %q", ba.Username)
	}
}

func TestResolveAuther_GitHubTokenFallback(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "env-token")
	auther, err := resolveAuther(AuthConfig{}, "https://github.com/org/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	auth, err := auther.AuthFor("https://github.com/org/repo")
	if err != nil {
		t.Fatalf("AuthFor: %v", err)
	}
	ba, ok := auth.(*http.BasicAuth)
	if !ok {
		t.Fatalf("expected *http.BasicAuth, got %T", auth)
	}
	if ba.Username != "env-token" {
		t.Errorf("expected GITHUB_TOKEN fallback, got %q", ba.Username)
	}
}

func TestResolveAuther_NoFallbackForSSH(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "env-token")
	auther, err := resolveAuther(AuthConfig{}, "git@github.com:org/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	auth, err := auther.AuthFor("git@github.com:org/repo")
	if err != nil {
		t.Fatalf("AuthFor: %v", err)
	}
	if auth != nil {
		t.Errorf("expected NoAuth for SSH URL with no explicit config, got %T", auth)
	}
}

func TestResolveAuther_NoFallbackForPublic(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	auther, err := resolveAuther(AuthConfig{}, "https://github.com/public/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	auth, err := auther.AuthFor("https://github.com/public/repo")
	if err != nil {
		t.Fatalf("AuthFor: %v", err)
	}
	if auth != nil {
		t.Errorf("expected NoAuth when GITHUB_TOKEN unset, got %T", auth)
	}
}

func TestAuthFromOrigin_NoRef(t *testing.T) {
	imp := &Importer{}
	cfg, err := imp.authFromOrigin(&Origin{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Method != "" {
		t.Errorf("expected zero AuthConfig, got method %q", cfg.Method)
	}
}

func TestAuthFromOrigin_MissingResolver(t *testing.T) {
	imp := &Importer{}
	_, err := imp.authFromOrigin(&Origin{CredentialRef: "${vault:GIT_TOKEN}"})
	if !errors.Is(err, gitpkg.ErrAuthFailed) {
		t.Errorf("expected ErrAuthFailed, got %v", err)
	}
}

func TestAuthFromOrigin_ResolverReturnsToken(t *testing.T) {
	imp := &Importer{}
	imp.SetCredentialResolver(func(ref string) (string, error) {
		if ref != "${vault:GIT_TOKEN}" {
			return "", errors.New("unexpected ref")
		}
		return "resolved-secret", nil
	})
	cfg, err := imp.authFromOrigin(&Origin{CredentialRef: "${vault:GIT_TOKEN}"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Method != "token" || cfg.Token != "resolved-secret" {
		t.Errorf("unexpected AuthConfig: %+v", cfg)
	}
	if cfg.CredentialRef != "${vault:GIT_TOKEN}" {
		t.Errorf("expected CredentialRef preserved, got %q", cfg.CredentialRef)
	}
}

func TestAuthFromOrigin_EmptyResolvedValue(t *testing.T) {
	imp := &Importer{}
	imp.SetCredentialResolver(func(string) (string, error) { return "", nil })
	_, err := imp.authFromOrigin(&Origin{CredentialRef: "${vault:GIT_TOKEN}"})
	if !errors.Is(err, gitpkg.ErrEmptyToken) {
		t.Errorf("expected ErrEmptyToken, got %v", err)
	}
}

func TestSourceAuth_ToAuthConfig(t *testing.T) {
	var nilAuth *SourceAuth
	if cfg := nilAuth.ToAuthConfig(); cfg.Method != "" {
		t.Errorf("nil SourceAuth should yield zero AuthConfig, got %+v", cfg)
	}
	a := &SourceAuth{
		Method:        "token",
		CredentialRef: "${vault:X}",
		SSHUser:       "gitlab-ci",
		SSHKeyPath:    "/keys/id",
	}
	got := a.ToAuthConfig()
	if got.Method != "token" || got.CredentialRef != "${vault:X}" ||
		got.SSHUser != "gitlab-ci" || got.SSHKeyPath != "/keys/id" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if got.Token != "" {
		t.Error("SourceAuth should never carry a raw Token")
	}
}
