//go:build integration

package integration

import (
	"errors"
	"testing"

	"github.com/gridctl/gridctl/pkg/builder"
	"github.com/gridctl/gridctl/pkg/config"
	gitpkg "github.com/gridctl/gridctl/pkg/git"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/runtime"
)

// These tests mirror skills_private_git_test.go but exercise the MCP server
// source build path: config.SourceAuth → runtime.AuthForSource → resolved
// transport.AuthMethod → builder.CloneOrUpdate → gitpkg.Clone. The helpers
// initPrivateBareRepo / startAuthedGitHTTPServer / isolateGridctlHome live
// in skills_private_git_test.go (same package, same build tag).

func TestMCP_PrivateHTTPS_NoAuth_ReturnsAuthRequired(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	isolateGridctlHome(t)

	bareParent, bareName := initPrivateBareRepo(t)
	srv := startAuthedGitHTTPServer(t, bareParent, privateRepoValidToken)

	repoURL := srv.URL + "/" + bareName

	// No SourceAuth at all — the public-repo path. Must fail with the
	// classified ErrAuthRequired so callers can distinguish it from a
	// network or DNS error.
	_, err := builder.CloneOrUpdate(repoURL, "", nil, logging.NewDiscardLogger())
	if err == nil {
		t.Fatal("expected error cloning private repo without auth")
	}
	if !errors.Is(gitpkg.ClassifyError(err), gitpkg.ErrAuthRequired) {
		t.Errorf("expected ErrAuthRequired, got %v", err)
	}
}

func TestMCP_PrivateHTTPS_WrongToken_ReturnsAuthFailed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	isolateGridctlHome(t)

	bareParent, bareName := initPrivateBareRepo(t)
	srv := startAuthedGitHTTPServer(t, bareParent, privateRepoValidToken)

	repoURL := srv.URL + "/" + bareName

	// Resolver returns the wrong token; expect 403 → ErrAuthFailed.
	resolver := func(string) (string, error) { return "not-the-right-token", nil }
	auth, err := runtime.AuthForSource(
		&config.SourceAuth{Method: "token", CredentialRef: "${vault:GIT_TOKEN}"},
		repoURL,
		resolver,
	)
	if err != nil {
		t.Fatalf("AuthForSource (wrong token): %v", err)
	}

	_, err = builder.CloneOrUpdate(repoURL, "", auth, logging.NewDiscardLogger())
	if err == nil {
		t.Fatal("expected error cloning with wrong token")
	}
	if !errors.Is(gitpkg.ClassifyError(err), gitpkg.ErrAuthFailed) {
		t.Errorf("expected ErrAuthFailed, got %v", err)
	}
}

func TestMCP_PrivateHTTPS_ValidToken_Succeeds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	isolateGridctlHome(t)

	bareParent, bareName := initPrivateBareRepo(t)
	srv := startAuthedGitHTTPServer(t, bareParent, privateRepoValidToken)

	repoURL := srv.URL + "/" + bareName

	resolver := func(ref string) (string, error) {
		if ref != "${vault:GIT_TOKEN}" {
			return "", errors.New("unexpected ref")
		}
		return privateRepoValidToken, nil
	}
	auth, err := runtime.AuthForSource(
		&config.SourceAuth{Method: "token", CredentialRef: "${vault:GIT_TOKEN}"},
		repoURL,
		resolver,
	)
	if err != nil {
		t.Fatalf("AuthForSource: %v", err)
	}
	if auth == nil {
		t.Fatal("expected non-nil auth method for token+credential_ref")
	}

	path, err := builder.CloneOrUpdate(repoURL, "", auth, logging.NewDiscardLogger())
	if err != nil {
		t.Fatalf("CloneOrUpdate with valid auth: %v", err)
	}
	if path == "" {
		t.Error("expected non-empty clone path")
	}
}
