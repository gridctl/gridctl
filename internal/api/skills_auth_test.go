package api

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	gitpkg "github.com/gridctl/gridctl/pkg/git"
	"github.com/gridctl/gridctl/pkg/vault"
)

func TestAuthRequest_ToAuthConfig_Empty(t *testing.T) {
	var r *AuthRequest
	cfg, err := r.toAuthConfig(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Method != "" {
		t.Errorf("expected zero AuthConfig for nil request, got %+v", cfg)
	}
}

func TestAuthRequest_ToAuthConfig_InferToken(t *testing.T) {
	r := &AuthRequest{Token: "abc"}
	cfg, err := r.toAuthConfig(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Method != "token" || cfg.Token != "abc" {
		t.Errorf("unexpected AuthConfig: %+v", cfg)
	}
}

func TestAuthRequest_ToAuthConfig_InferSSHKey(t *testing.T) {
	r := &AuthRequest{SSHKeyPath: "/keys/id"}
	cfg, err := r.toAuthConfig(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Method != "ssh-key" || cfg.SSHKeyPath != "/keys/id" {
		t.Errorf("unexpected AuthConfig: %+v", cfg)
	}
}

func TestAuthRequest_ToAuthConfig_ResolveCredentialRef(t *testing.T) {
	v := vault.NewStore(t.TempDir())
	if err := v.Load(); err != nil {
		t.Fatalf("vault load: %v", err)
	}
	if err := v.Set("GIT_TOKEN", "secret-abc"); err != nil {
		t.Fatalf("vault set: %v", err)
	}

	r := &AuthRequest{CredentialRef: "${vault:GIT_TOKEN}"}
	cfg, err := r.toAuthConfig(v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Method != "token" {
		t.Errorf("expected method=token, got %q", cfg.Method)
	}
	if cfg.Token != "secret-abc" {
		t.Errorf("expected resolved token, got %q", cfg.Token)
	}
	if cfg.CredentialRef != "${vault:GIT_TOKEN}" {
		t.Errorf("expected CredentialRef preserved, got %q", cfg.CredentialRef)
	}
}

func TestAuthRequest_ToAuthConfig_UnresolvedRef(t *testing.T) {
	v := vault.NewStore(t.TempDir())
	if err := v.Load(); err != nil {
		t.Fatalf("vault load: %v", err)
	}

	r := &AuthRequest{CredentialRef: "${vault:MISSING}"}
	_, err := r.toAuthConfig(v)
	if err == nil {
		t.Fatal("expected error for missing vault key")
	}
}

func TestResolveCredentialRef_NoVault(t *testing.T) {
	_, err := resolveCredentialRef("${vault:X}", nil)
	if err == nil {
		t.Error("expected error when vault is nil")
	}
}

func TestGitErrorStatus(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"auth required", fmt.Errorf("%w: x", gitpkg.ErrAuthRequired), http.StatusUnauthorized},
		{"auth failed", fmt.Errorf("%w: x", gitpkg.ErrAuthFailed), http.StatusUnauthorized},
		{"not found", fmt.Errorf("%w: x", gitpkg.ErrNotFound), http.StatusNotFound},
		{"protocol mismatch", fmt.Errorf("%w: x", gitpkg.ErrProtocolMismatch), http.StatusBadRequest},
		{"empty token", fmt.Errorf("%w: x", gitpkg.ErrEmptyToken), http.StatusBadRequest},
		{"host key mismatch", fmt.Errorf("%w: x", gitpkg.ErrHostKeyMismatch), http.StatusBadRequest},
		{"other", errors.New("some random failure"), http.StatusInternalServerError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := gitErrorStatus(c.err); got != c.want {
				t.Errorf("gitErrorStatus(%v) = %d, want %d", c.err, got, c.want)
			}
		})
	}
}

