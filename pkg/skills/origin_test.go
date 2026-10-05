package skills

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOriginReadWrite(t *testing.T) {
	dir := t.TempDir()

	origin := &Origin{
		Repo:                     "https://github.com/org/repo",
		Ref:                      "main",
		Path:                     "skills/deploy",
		CommitSHA:                "abc123def456",
		ImportedAt:               time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
		ContentHash:              "deadbeef",
		SupportingFilesInstalled: true,
	}

	// Write
	require.NoError(t, WriteOrigin(dir, origin))

	// Verify file exists
	assert.FileExists(t, filepath.Join(dir, ".origin.json"))

	// Read back
	got, err := ReadOrigin(dir)
	require.NoError(t, err)
	assert.Equal(t, origin.Repo, got.Repo)
	assert.Equal(t, origin.Ref, got.Ref)
	assert.Equal(t, origin.Path, got.Path)
	assert.Equal(t, origin.CommitSHA, got.CommitSHA)
	assert.Equal(t, origin.ContentHash, got.ContentHash)
	assert.True(t, got.SupportingFilesInstalled)
}

func TestHasOrigin(t *testing.T) {
	dir := t.TempDir()

	// No origin file
	assert.False(t, HasOrigin(dir))

	// Write origin
	origin := &Origin{Repo: "https://github.com/org/repo", CommitSHA: "abc123"}
	require.NoError(t, WriteOrigin(dir, origin))
	assert.True(t, HasOrigin(dir))
}

func TestDeleteOrigin(t *testing.T) {
	dir := t.TempDir()

	origin := &Origin{Repo: "https://github.com/org/repo", CommitSHA: "abc123"}
	require.NoError(t, WriteOrigin(dir, origin))
	assert.True(t, HasOrigin(dir))

	require.NoError(t, DeleteOrigin(dir))
	assert.False(t, HasOrigin(dir))

	// Deleting non-existent is not an error
	require.NoError(t, DeleteOrigin(dir))
}

func TestReadOriginNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadOrigin(dir)
	assert.Error(t, err)
}

func TestReadOriginInvalid(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".origin.json"), []byte("not json"), 0644))
	_, err := ReadOrigin(dir)
	assert.Error(t, err)
}

func TestOrigin_CredentialRefRoundTrip(t *testing.T) {
	dir := t.TempDir()

	origin := &Origin{
		Repo:          "https://github.com/org/private",
		Ref:           "main",
		CommitSHA:     "deadbeef",
		ImportedAt:    time.Now().UTC(),
		ContentHash:   "aabbcc",
		CredentialRef: "${vault:GIT_TOKEN}",
	}
	require.NoError(t, WriteOrigin(dir, origin))

	// Verify the token REFERENCE is persisted but no raw value ever is.
	raw, err := os.ReadFile(filepath.Join(dir, ".origin.json"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "${vault:GIT_TOKEN}")

	got, err := ReadOrigin(dir)
	require.NoError(t, err)
	assert.Equal(t, "${vault:GIT_TOKEN}", got.CredentialRef)
}

func TestOrigin_NoCredentialRefOmitted(t *testing.T) {
	dir := t.TempDir()

	origin := &Origin{
		Repo:        "https://github.com/org/public",
		Ref:         "main",
		CommitSHA:   "abc",
		ImportedAt:  time.Now().UTC(),
		ContentHash: "hash",
	}
	require.NoError(t, WriteOrigin(dir, origin))

	raw, err := os.ReadFile(filepath.Join(dir, ".origin.json"))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "credentialRef")
}

func TestOrigin_SSHKeyAuthRoundTrip(t *testing.T) {
	dir := t.TempDir()
	const passphrase = "super-secret-passphrase"

	origin := &Origin{
		Repo:       "ssh://git@127.0.0.1/x.git",
		Ref:        "main",
		CommitSHA:  "abc",
		AuthMethod: "ssh-key",
		SSHUser:    "git",
		SSHKeyPath: "/abs/key",
	}
	require.NoError(t, WriteOrigin(dir, origin))

	raw, err := os.ReadFile(filepath.Join(dir, ".origin.json"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"authMethod": "ssh-key"`)
	assert.Contains(t, string(raw), `"sshKeyPath": "/abs/key"`)
	assert.NotContains(t, string(raw), passphrase)
	assert.NotContains(t, string(raw), "sshPassphrase")

	got, err := ReadOrigin(dir)
	require.NoError(t, err)
	assert.Equal(t, "ssh-key", got.AuthMethod)
	assert.Equal(t, "git", got.SSHUser)
	assert.Equal(t, "/abs/key", got.SSHKeyPath)
	assert.Equal(t, StoredAuth{Method: "ssh-key", SSHUser: "git", SSHKeyPath: "/abs/key"}, got.StoredAuth())
}
