package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
		{"ssh agent missing", fmt.Errorf("%w: x", gitpkg.ErrSSHAgentMissing), http.StatusUnprocessableEntity},
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

func TestWriteGitErrorForRepo_SSHAgentCarriesCodeAndHTTPSEquivalent(t *testing.T) {
	rec := httptest.NewRecorder()
	writeGitErrorForRepo(rec, "Pack preview failed: ", "git@github.com:acme/pack.git",
		fmt.Errorf("%w: SSH_AUTH_SOCK is unset", gitpkg.ErrSSHAgentMissing))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["code"] != "ssh_agent_unavailable" {
		t.Errorf("code = %q, want ssh_agent_unavailable", body["code"])
	}
	if body["httpsEquivalent"] != "https://github.com/acme/pack" {
		t.Errorf("httpsEquivalent = %q, want https://github.com/acme/pack", body["httpsEquivalent"])
	}
	// A client that only reads "error" must keep working.
	if !strings.HasPrefix(body["error"], "Pack preview failed: ") {
		t.Errorf("error should keep the caller's prefix, got %q", body["error"])
	}
	// The raw library string must not be what the user is shown.
	if strings.Contains(body["error"], "not-specified") {
		t.Errorf("error leaked the raw go-git string: %q", body["error"])
	}
}

func TestWriteGitErrorForRepo_HTTPSRepoOmitsEquivalent(t *testing.T) {
	rec := httptest.NewRecorder()
	writeGitErrorForRepo(rec, "Pack preview failed: ", "https://github.com/acme/pack",
		fmt.Errorf("%w: x", gitpkg.ErrSSHAgentMissing))

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if _, ok := body["httpsEquivalent"]; ok {
		t.Errorf("an HTTPS repo has no HTTPS equivalent to offer, got %q", body["httpsEquivalent"])
	}
	if body["code"] != "ssh_agent_unavailable" {
		t.Errorf("code should still be set, got %q", body["code"])
	}
}

func TestWriteGitError_UnclassifiedHasNoCode(t *testing.T) {
	rec := httptest.NewRecorder()
	writeGitError(rec, "Import failed: ", errors.New("disk on fire"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if _, ok := body["code"]; ok {
		t.Errorf("unclassified errors should carry no code, got %q", body["code"])
	}
}

func TestWriteGitError_RedactsEmbeddedToken(t *testing.T) {
	rec := httptest.NewRecorder()
	leak := "ghp_" + strings.Repeat("a", 40)
	writeGitError(rec, "Import failed: ", fmt.Errorf("%w: https://%s@github.com/acme/p", gitpkg.ErrAuthFailed, leak))

	if strings.Contains(rec.Body.String(), leak) {
		t.Errorf("response body leaked the token: %s", rec.Body.String())
	}
}
