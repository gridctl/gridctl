package skills

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestRegistry(t *testing.T) (*registry.Store, string) {
	dir := t.TempDir()
	store := registry.NewStore(dir)
	require.NoError(t, store.Load())
	return store, dir
}

func createTestSkill(t *testing.T, store *registry.Store, name string) {
	sk := &registry.AgentSkill{
		Name:        name,
		Description: "Test skill " + name,
		State:       registry.StateActive,
	}
	require.NoError(t, store.SaveSkill(sk))
}

func TestImporterRemove(t *testing.T) {
	store, dir := setupTestRegistry(t)
	lockPath := filepath.Join(dir, "skills.lock.yaml")

	// Create a skill with origin
	createTestSkill(t, store, "test-skill")
	skillDir := filepath.Join(dir, "skills", "test-skill")
	require.NoError(t, WriteOrigin(skillDir, &Origin{
		Repo:      "https://github.com/org/repo",
		CommitSHA: "abc123",
	}))

	// Write lock entry
	lf := &LockFile{Sources: map[string]LockedSource{
		"repo": {
			Skills: map[string]LockedSkill{
				"test-skill": {ContentHash: "hash"},
			},
		},
	}}
	require.NoError(t, WriteLockFile(lockPath, lf))

	imp := NewImporter(store, dir, lockPath, slog.Default())

	// Remove
	require.NoError(t, imp.Remove("test-skill"))

	// Verify skill is gone
	_, err := store.GetSkill("test-skill")
	assert.Error(t, err)

	// Verify origin is gone
	assert.False(t, HasOrigin(skillDir))

	// Verify lock entry is gone
	lf2, err := ReadLockFile(lockPath)
	require.NoError(t, err)
	assert.Empty(t, lf2.Sources)
}

func TestImporterPin(t *testing.T) {
	store, dir := setupTestRegistry(t)
	lockPath := filepath.Join(dir, "skills.lock.yaml")

	createTestSkill(t, store, "pinnable")
	skillDir := filepath.Join(dir, "skills", "pinnable")
	require.NoError(t, WriteOrigin(skillDir, &Origin{
		Repo:      "https://github.com/org/repo",
		Ref:       "main",
		CommitSHA: "abc123",
	}))

	lf := &LockFile{Sources: map[string]LockedSource{
		"repo": {
			Ref: "main",
			Skills: map[string]LockedSkill{
				"pinnable": {},
			},
		},
	}}
	require.NoError(t, WriteLockFile(lockPath, lf))

	imp := NewImporter(store, dir, lockPath, slog.Default())
	require.NoError(t, imp.Pin("pinnable", "v1.0.0"))

	// Check origin was updated
	origin, err := ReadOrigin(skillDir)
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", origin.Ref)
}

func TestImporterInfo(t *testing.T) {
	store, dir := setupTestRegistry(t)
	lockPath := filepath.Join(dir, "skills.lock.yaml")

	// Local skill
	createTestSkill(t, store, "local-skill")
	imp := NewImporter(store, dir, lockPath, slog.Default())

	info, err := imp.Info("local-skill")
	require.NoError(t, err)
	assert.False(t, info.IsRemote)
	assert.Equal(t, "local-skill", info.Name)

	// Remote skill
	createTestSkill(t, store, "remote-skill")
	skillDir := filepath.Join(dir, "skills", "remote-skill")
	require.NoError(t, WriteOrigin(skillDir, &Origin{
		Repo:      "https://github.com/org/repo",
		Ref:       "main",
		CommitSHA: "abc123def456789012345678901234567890abcd",
	}))

	info, err = imp.Info("remote-skill")
	require.NoError(t, err)
	assert.True(t, info.IsRemote)
	assert.Equal(t, "https://github.com/org/repo", info.Origin.Repo)
}

func TestImporterInfoNotFound(t *testing.T) {
	store, dir := setupTestRegistry(t)
	lockPath := filepath.Join(dir, "skills.lock.yaml")

	imp := NewImporter(store, dir, lockPath, slog.Default())
	_, err := imp.Info("nonexistent")
	assert.Error(t, err)
}

func TestImporterPinNoOrigin(t *testing.T) {
	store, dir := setupTestRegistry(t)
	lockPath := filepath.Join(dir, "skills.lock.yaml")

	createTestSkill(t, store, "local-only")
	imp := NewImporter(store, dir, lockPath, slog.Default())

	err := imp.Pin("local-only", "v1.0.0")
	assert.Error(t, err)
}

func TestSafeRepoPath(t *testing.T) {
	tests := []struct {
		path    string
		wantErr bool
	}{
		{"skills/deploy", false},
		{"../../../etc/passwd", true},
		{"/absolute/path", true},
		{"valid/nested/path", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			err := SafeRepoPath(tt.path)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestContentHashFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello world"), 0644))

	hash1, err := ContentHashFile(path)
	require.NoError(t, err)
	assert.NotEmpty(t, hash1)

	// Same content = same hash
	path2 := filepath.Join(dir, "test2.txt")
	require.NoError(t, os.WriteFile(path2, []byte("hello world"), 0644))
	hash2, err := ContentHashFile(path2)
	require.NoError(t, err)
	assert.Equal(t, hash1, hash2)

	// Different content = different hash
	path3 := filepath.Join(dir, "test3.txt")
	require.NoError(t, os.WriteFile(path3, []byte("different"), 0644))
	hash3, err := ContentHashFile(path3)
	require.NoError(t, err)
	assert.NotEqual(t, hash1, hash3)
}
