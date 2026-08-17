package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/vault"
)

// vaultWithToken wires a loaded vault holding one git token onto srv, so
// credentialRef resolution has something to resolve against.
func vaultWithToken(t *testing.T, srv *Server, key, value string) {
	t.Helper()
	v := vault.NewStore(t.TempDir())
	if err := v.Load(); err != nil {
		t.Fatalf("vault load: %v", err)
	}
	if err := v.Set(key, value); err != nil {
		t.Fatalf("vault set: %v", err)
	}
	srv.SetVaultStore(v)
}

// seedLockedSource records a source with a credential reference without going
// through an import, which is the only way to set one against a local fixture:
// toAuthConfig infers method "token" from a credentialRef, and an HTTPS token
// auther correctly refuses a local path.
func seedLockedSource(t *testing.T, repo, credentialRef string) {
	t.Helper()
	err := skills.MutateLockFile(context.Background(), skills.LockFilePath(), func(lf *skills.LockFile) (bool, error) {
		lf.SetSource(skills.RepoToName(repo), skills.LockedSource{
			Repo:          repo,
			CredentialRef: credentialRef,
		})
		return true, nil
	})
	if err != nil {
		t.Fatalf("seed lockfile: %v", err)
	}
}

func TestHandlePackPreview_AuthBlockReachesTheClone(t *testing.T) {
	srv, _ := setupPackTestServer(t)
	vaultWithToken(t, srv, "GIT_TOKEN", "resolved-secret")
	repo := packRepoFixture(t, packTestManifest, nil)

	body := `{"repo":` + jsonQuote(repo) + `,"auth":{"credentialRef":"${vault:GIT_TOKEN}"}}`
	rec := doJSON(t, srv, http.MethodPost, "/api/packs/preview", body)

	// The vault reference resolved (no vault error) and the resulting token
	// auther reached the clone, which refuses a local path.
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "protocol mismatch") {
		t.Fatalf("expected the resolved token to reach the clone, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "resolved-secret") {
		t.Errorf("response leaked the resolved token: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "vault key") {
		t.Errorf("reference failed to resolve: %s", rec.Body.String())
	}
}

func TestHandlePackAdd_AuthBlockReachesTheClone(t *testing.T) {
	srv, _ := setupPackTestServer(t)
	const secret = "ghp_restSecretMustNotLeak"
	vaultWithToken(t, srv, "GIT_TOKEN", secret)
	repo := packRepoFixture(t, packTestManifest, nil)

	body := `{"repo":` + jsonQuote(repo) + `,"auth":{"credentialRef":"${vault:GIT_TOKEN}"}}`
	rec := doJSON(t, srv, http.MethodPost, "/api/packs", body)

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "protocol mismatch") {
		t.Fatalf("expected the resolved token to reach the clone, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), secret) {
		t.Errorf("response leaked the resolved token: %s", rec.Body.String())
	}
}

func TestHandlePackPreview_UnresolvableRefIs400(t *testing.T) {
	srv, _ := setupPackTestServer(t)
	vaultWithToken(t, srv, "GIT_TOKEN", "resolved-secret")
	repo := packRepoFixture(t, packTestManifest, nil)

	body := `{"repo":` + jsonQuote(repo) + `,"auth":{"credentialRef":"${vault:NOPE}"}}`
	rec := doJSON(t, srv, http.MethodPost, "/api/packs/preview", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("preview = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "NOPE") {
		t.Errorf("error should name the missing key, got %s", rec.Body.String())
	}
}

// The stored-reference fallback is what lets the update dialog preview an
// already-imported private pack with no user input.
func TestHandlePackPreview_FallsBackToStoredCredentialRef(t *testing.T) {
	srv, _ := setupPackTestServer(t)
	vaultWithToken(t, srv, "GIT_TOKEN", "resolved-secret")
	repo := packRepoFixture(t, packTestManifest, nil)
	seedLockedSource(t, repo, "${vault:GIT_TOKEN}")

	if got := srv.storedPackCredentialRef(repo); got != "${vault:GIT_TOKEN}" {
		t.Fatalf("storedPackCredentialRef = %q, want the recorded reference", got)
	}

	// No auth block at all: the handler must find and resolve the stored
	// reference itself, which again means the clone sees a token auther.
	rec := doJSON(t, srv, http.MethodPost, "/api/packs/preview", `{"repo":`+jsonQuote(repo)+`}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "protocol mismatch") {
		t.Fatalf("stored reference did not reach the clone, got %d: %s", rec.Code, rec.Body.String())
	}
}

// An empty auth object is a present request, so it must suppress the stored
// reference rather than silently reusing it, mirroring resolveCheckAuth on the
// skills side. Here that means the clone proceeds unauthenticated and succeeds.
func TestHandlePackPreview_EmptyAuthObjectSuppressesStoredRef(t *testing.T) {
	srv, _ := setupPackTestServer(t)
	vaultWithToken(t, srv, "GIT_TOKEN", "resolved-secret")
	repo := packRepoFixture(t, packTestManifest, nil)
	seedLockedSource(t, repo, "${vault:GIT_TOKEN}")

	rec := doJSON(t, srv, http.MethodPost, "/api/packs/preview",
		`{"repo":`+jsonQuote(repo)+`,"auth":{}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview = %d, want 200 (stored ref should be suppressed): %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "team-pack") {
		t.Errorf("expected the resolved pack, got %s", rec.Body.String())
	}
}

func TestStoredPackCredentialRef_UnknownRepoIsEmpty(t *testing.T) {
	srv, _ := setupPackTestServer(t)

	if got := srv.storedPackCredentialRef("https://github.com/never/imported"); got != "" {
		t.Errorf("storedPackCredentialRef = %q, want empty for an unknown repo", got)
	}
	if got := srv.storedPackCredentialRef(""); got != "" {
		t.Errorf(`storedPackCredentialRef("") = %q, want empty`, got)
	}
}

// Article IX in spirit: a request with no auth field behaves exactly as it did
// before the field existed.
func TestHandlePackAdd_NoAuthBlockIsUnchanged(t *testing.T) {
	srv, _ := setupPackTestServer(t)
	repo := packRepoFixture(t, packTestManifest, nil)

	rec := doJSON(t, srv, http.MethodPost, "/api/packs", `{"repo":`+jsonQuote(repo)+`}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add = %d: %s", rec.Code, rec.Body.String())
	}

	lf, err := skills.ReadLockFile(skills.LockFilePath())
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
	}
	if src := lf.Sources[skills.RepoToName(repo)]; src.CredentialRef != "" {
		t.Errorf("CredentialRef = %q, want empty when no auth was supplied", src.CredentialRef)
	}
}
