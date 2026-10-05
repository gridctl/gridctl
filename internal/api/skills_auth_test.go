package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gitpkg "github.com/gridctl/gridctl/pkg/git"
	"github.com/gridctl/gridctl/pkg/skills"
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

func TestAuthRequest_ToAuthConfig_RejectsRelativeSSHKeyPath(t *testing.T) {
	r := &AuthRequest{Method: "ssh-key", SSHKeyPath: "keys/id_ed25519"}
	_, err := r.toAuthConfig(nil)
	if err == nil {
		t.Fatal("expected an error for a relative sshKeyPath")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("error = %q, want it to require an absolute path", err.Error())
	}
}

func TestResolveCheckAuth_StoredSSHKey(t *testing.T) {
	const passphrase = "env-pass"
	t.Setenv("GRIDCTL_SSH_KEY_PASSPHRASE", passphrase)
	srv := &Server{}

	cfg, err := srv.resolveCheckAuth(nil, skills.StoredAuth{
		Method: "ssh-key", SSHUser: "git", SSHKeyPath: "/abs/key",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Method != "ssh-key" || cfg.SSHKeyPath != "/abs/key" || cfg.SSHUser != "git" {
		t.Fatalf("stored ssh-key auth = %+v", cfg)
	}
	if cfg.SSHPassphrase != passphrase {
		t.Fatalf("passphrase = %q, want the environment value", cfg.SSHPassphrase)
	}

	v := vault.NewStore(t.TempDir())
	if err := v.Load(); err != nil {
		t.Fatalf("vault load: %v", err)
	}
	if err := v.Set("GIT_TOKEN", "secret-abc"); err != nil {
		t.Fatalf("vault set: %v", err)
	}
	srv.SetVaultStore(v)
	cfg, err = srv.resolveCheckAuth(nil, skills.StoredAuth{
		Method: "ssh-key", SSHKeyPath: "/abs/key", CredentialRef: "${vault:GIT_TOKEN}",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Method != "token" || cfg.Token != "secret-abc" || cfg.CredentialRef != "${vault:GIT_TOKEN}" {
		t.Fatalf("credential ref should win, got %+v", cfg)
	}
	if cfg.SSHKeyPath != "" {
		t.Fatalf("ssh path should not survive a winning credential ref, got %+v", cfg)
	}
}

func TestResolveCheckAuth_StoredRefStillResolves(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	v := vault.NewStore(t.TempDir())
	if err := v.Load(); err != nil {
		t.Fatalf("vault load: %v", err)
	}
	if err := v.Set("GIT_TOKEN", "secret-abc"); err != nil {
		t.Fatalf("vault set: %v", err)
	}
	srv.SetVaultStore(v)

	const repo = "https://github.com/acme/private-skills"
	err := skills.MutateLockFile(context.Background(), srv.lockFilePath(), func(lf *skills.LockFile) (bool, error) {
		lf.SetSource(skills.RepoToName(repo), skills.LockedSource{
			Repo:          repo,
			Ref:           "main",
			CommitSHA:     "abc",
			CredentialRef: "${var:GIT_TOKEN}",
		})
		return true, nil
	})
	if err != nil {
		t.Fatalf("seed lockfile: %v", err)
	}

	lf, err := skills.ReadLockFile(srv.lockFilePath())
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
	}
	src := lf.Sources[skills.RepoToName(repo)]
	cfg, err := srv.resolveCheckAuth(nil, src.StoredAuth())
	if err != nil {
		t.Fatalf("resolveCheckAuth: %v", err)
	}
	if cfg.Method != "token" || cfg.Token != "secret-abc" || cfg.CredentialRef != "${var:GIT_TOKEN}" {
		t.Fatalf("stored ref = %+v, want token auth with the vault value", cfg)
	}

	rec := httptest.NewRecorder()
	req := loopbackRequest(http.MethodPost, "/api/skills/sources/"+skills.RepoToName(repo)+"/check", nil)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("check = %d, want 200 (stored ref must resolve, not 400): %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "vault key") || strings.Contains(rec.Body.String(), "secret-abc") {
		t.Fatalf("check body leaked a resolution failure or the token: %s", rec.Body.String())
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

// A skill clone that fails with no reachable ssh-agent must carry the same
// structured remedy the pack endpoints send. Before this, every skill call
// site passed an empty repo into writeGitErrorForRepo, so `code` arrived but
// `httpsEquivalent` never did — leaving the client with a cause and no fix,
// while docs/troubleshooting.md said otherwise.
func TestHandleSkillSourcePreview_SSHAgentCarriesHTTPSEquivalent(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	srv, _ := setupRegistryTestServer(t)

	rec := doJSON(t, srv, http.MethodPost, "/api/skills/sources/private/preview",
		`{"repo":"git@github.com:acme/private-skills.git"}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["code"] != "ssh_agent_unavailable" {
		t.Errorf("code = %q, want ssh_agent_unavailable", body["code"])
	}
	if body["httpsEquivalent"] != "https://github.com/acme/private-skills" {
		t.Errorf("httpsEquivalent = %q, want the rewritten URL", body["httpsEquivalent"])
	}
	if strings.Contains(body["error"], "not-specified") {
		t.Errorf("raw go-git string reached the client: %q", body["error"])
	}
}

// An HTTPS skill repo has no SSH URL to rewrite, so the field must be absent
// rather than empty — a client keys the action off its presence.
func TestHandleSkillSourcePreview_HTTPSRepoOmitsEquivalent(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	srv, _ := setupRegistryTestServer(t)

	rec := doJSON(t, srv, http.MethodPost, "/api/skills/sources/nope/preview",
		`{"repo":"https://127.0.0.1:1/acme/nope.git"}`)

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["httpsEquivalent"]; ok {
		t.Errorf("httpsEquivalent present for an HTTPS repo: %q", body["httpsEquivalent"])
	}
}
