package runtime

import (
	"errors"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/gridctl/gridctl/pkg/config"
	gitpkg "github.com/gridctl/gridctl/pkg/git"
)

func TestAuthForSource_Nil(t *testing.T) {
	got, err := AuthForSource(nil, "https://example.com/repo.git", nil)
	if err != nil {
		t.Fatalf("nil SourceAuth should not error: %v", err)
	}
	if got != nil {
		t.Errorf("nil SourceAuth should yield nil AuthMethod, got %T", got)
	}
}

func TestAuthForSource_NoneMethod(t *testing.T) {
	for _, method := range []string{"", "none"} {
		got, err := AuthForSource(&config.SourceAuth{Method: method}, "https://example.com/repo.git", nil)
		if err != nil {
			t.Errorf("method %q should not error: %v", method, err)
		}
		if got != nil {
			t.Errorf("method %q should yield nil AuthMethod, got %T", method, got)
		}
	}
}

func TestAuthForSource_Token_ResolvesCredentialRef(t *testing.T) {
	resolver := func(ref string) (string, error) {
		if ref != "${vault:GIT_TOKEN}" {
			return "", errors.New("unexpected ref")
		}
		return "ghp_real_token", nil
	}
	auth := &config.SourceAuth{Method: "token", CredentialRef: "${vault:GIT_TOKEN}"}

	got, err := AuthForSource(auth, "https://example.com/repo.git", resolver)
	if err != nil {
		t.Fatalf("AuthForSource: %v", err)
	}
	basic, ok := got.(*http.BasicAuth)
	if !ok {
		t.Fatalf("expected *http.BasicAuth, got %T", got)
	}
	if basic.Username != "ghp_real_token" {
		t.Errorf("username: got %q, want resolved token", basic.Username)
	}
}

func TestAuthForSource_Token_MissingResolver(t *testing.T) {
	auth := &config.SourceAuth{Method: "token", CredentialRef: "${vault:GIT_TOKEN}"}
	_, err := AuthForSource(auth, "https://example.com/repo.git", nil)
	if err == nil {
		t.Fatal("expected error when credential_ref is set but no resolver is configured")
	}
}

func TestAuthForSource_Token_EmptyRef(t *testing.T) {
	// An empty CredentialRef with method=token surfaces ErrEmptyToken from the
	// auth primitive — clear signal to fix the config rather than a confusing 401.
	auth := &config.SourceAuth{Method: "token"}
	_, err := AuthForSource(auth, "https://example.com/repo.git", nil)
	if !errors.Is(err, gitpkg.ErrEmptyToken) {
		t.Errorf("expected ErrEmptyToken, got %v", err)
	}
}

func TestAuthForSource_ResolverError(t *testing.T) {
	resolver := func(string) (string, error) { return "", errors.New("vault locked") }
	auth := &config.SourceAuth{Method: "token", CredentialRef: "${vault:GIT_TOKEN}"}

	_, err := AuthForSource(auth, "https://example.com/repo.git", resolver)
	if err == nil {
		t.Fatal("expected resolver error to propagate")
	}
}

func TestAuthForSource_UnknownMethod(t *testing.T) {
	auth := &config.SourceAuth{Method: "magic"}
	_, err := AuthForSource(auth, "https://example.com/repo.git", nil)
	if err == nil {
		t.Fatal("expected error for unknown method")
	}
}
