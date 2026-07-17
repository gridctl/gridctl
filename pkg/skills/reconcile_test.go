package skills

import (
	"context"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/builder"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShortSHA(t *testing.T) {
	assert.Equal(t, "", ShortSHA(""))
	assert.Equal(t, "abc", ShortSHA("abc"))
	assert.Equal(t, "abcd1234", ShortSHA("abcd1234"))
	assert.Equal(t, "abcd1234", ShortSHA("abcd12345678"))
}

// TestImporter_Update_ForceColdCacheNoPanic is a regression test: with the
// repo cache evicted, FetchAndCompare returns an empty SHA, and a forced
// update must not panic on SHA formatting (Constitution Article V).
func TestImporter_Update_ForceColdCacheNoPanic(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")
	repoDir, _ := initSkillRepo(t, "# Test\n\nv1.\n")
	imp := NewImporter(store, regDir, lockPath, slog.Default())
	_, err := imp.Import(ImportOptions{Repo: repoDir, Ref: "master", Trust: true})
	require.NoError(t, err)

	// Evict the clone cache so FetchAndCompare yields an empty SHA.
	cacheDir, err := builder.ReposCacheDir()
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(cacheDir))

	require.NotPanics(t, func() {
		_, _ = imp.Update("test-skill", false, true)
	})
}

// driftSkill edits the on-disk SKILL.md of an imported skill so DetectDrift
// reports it as locally edited, and returns the edited content.
func driftSkill(t *testing.T, store *registry.Store, skillName, marker string) string {
	t.Helper()
	sk, err := store.GetSkill(skillName)
	require.NoError(t, err)
	dirName := sk.Dir
	if dirName == "" {
		dirName = sk.Name
	}
	path := filepath.Join(store.Dir(), "skills", dirName, "SKILL.md")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	edited := string(data) + "\n\n" + marker + "\n"
	require.NoError(t, os.WriteFile(path, []byte(edited), 0644))
	return edited
}

// TestImporter_AdvanceTracking_SkipPreservesFileAndInstalledHash is the central
// guarantee of the skip path: advancing tracking records the new upstream SHA
// in the origin and lock file, but the on-disk SKILL.md and its InstalledHash
// are untouched, so the skill still reads as drifted.
func TestImporter_AdvanceTracking_SkipPreservesFileAndInstalledHash(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")

	repoDir, repo := initSkillRepo(t, "# Test\n\nv1.\n")
	imp := NewImporter(store, regDir, lockPath, slog.Default())

	_, err := imp.Import(ImportOptions{Repo: repoDir, Ref: "master", Trust: true})
	require.NoError(t, err)

	skillDir := imp.skillDir("test-skill")
	originBefore, err := ReadOrigin(skillDir)
	require.NoError(t, err)
	require.NotEmpty(t, originBefore.InstalledHash)
	oldSHA := originBefore.CommitSHA

	// Local edit → drift.
	edited := driftSkill(t, store, "test-skill", "Local customization.")
	drifted, err := DetectDrift(context.Background(), store, lockPath, "")
	require.NoError(t, err)
	require.Contains(t, drifted, "test-skill")

	// Upstream moves on.
	commitChange(t, repo, repoDir, "# Test\n\nv2 upstream.\n")
	newSHA, changed, err := FetchAndCompare(repoDir, "master", oldSHA, AuthConfig{}, slog.Default())
	require.NoError(t, err)
	require.True(t, changed)
	require.NotEqual(t, oldSHA, newSHA)

	// Skip-advance.
	require.NoError(t, imp.AdvanceTracking(context.Background(), "test-skill", newSHA))

	// Tracking advanced...
	originAfter, err := ReadOrigin(skillDir)
	require.NoError(t, err)
	assert.Equal(t, newSHA, originAfter.CommitSHA, "origin commit SHA advanced")
	assert.Equal(t, originBefore.InstalledHash, originAfter.InstalledHash, "InstalledHash untouched")

	lf, err := ReadLockFile(lockPath)
	require.NoError(t, err)
	_, src, found := lf.FindSkillSource("test-skill")
	require.True(t, found)
	assert.Equal(t, newSHA, src.CommitSHA, "lock commit SHA advanced")
	assert.Equal(t, newSHA, src.ContentHash, "lock content hash advanced")

	// ...but the file and the drift signal are preserved.
	onDisk, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	require.NoError(t, err)
	assert.Equal(t, edited, string(onDisk), "on-disk SKILL.md must be untouched")

	driftedAfter, err := DetectDrift(context.Background(), store, lockPath, "")
	require.NoError(t, err)
	assert.Contains(t, driftedAfter, "test-skill", "drift must remain visible after a skip")
}

func TestImporter_AdvanceTracking_RequiresSHA(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")
	repoDir, _ := initSkillRepo(t, "# Test\n\nv1.\n")
	imp := NewImporter(store, regDir, lockPath, slog.Default())
	_, err := imp.Import(ImportOptions{Repo: repoDir, Ref: "master", Trust: true})
	require.NoError(t, err)

	assert.Error(t, imp.AdvanceTracking(context.Background(), "test-skill", ""))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.Error(t, imp.AdvanceTracking(ctx, "test-skill", "deadbeef"))
}

func TestImporter_BackupSkillFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")
	repoDir, _ := initSkillRepo(t, "# Test\n\nv1.\n")
	imp := NewImporter(store, regDir, lockPath, slog.Default())
	_, err := imp.Import(ImportOptions{Repo: repoDir, Ref: "master", Trust: true})
	require.NoError(t, err)

	original := driftSkill(t, store, "test-skill", "Edited before backup.")

	name, err := imp.BackupSkillFile(context.Background(), "test-skill", "abc1234")
	require.NoError(t, err)
	assert.Equal(t, "SKILL.md.pre-abc1234", name)

	skillDir := imp.skillDir("test-skill")
	backup, err := os.ReadFile(filepath.Join(skillDir, name))
	require.NoError(t, err)
	assert.Equal(t, original, string(backup), "backup captures the pre-overwrite content")

	// Empty SHA falls back to a "local" suffix.
	name2, err := imp.BackupSkillFile(context.Background(), "test-skill", "")
	require.NoError(t, err)
	assert.Equal(t, "SKILL.md.pre-local", name2)
}

func TestImporter_BackupSkillFile_MissingIsNoOp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store, regDir := setupTestRegistry(t)
	imp := NewImporter(store, regDir, filepath.Join(regDir, "skills.lock.yaml"), slog.Default())

	name, err := imp.BackupSkillFile(context.Background(), "does-not-exist", "sha")
	require.NoError(t, err)
	assert.Empty(t, name)
}

func TestImporter_Detach(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")
	repoDir, _ := initSkillRepo(t, "# Test\n\nv1.\n")
	imp := NewImporter(store, regDir, lockPath, slog.Default())
	_, err := imp.Import(ImportOptions{Repo: repoDir, Ref: "master", Trust: true})
	require.NoError(t, err)

	skillDir := imp.skillDir("test-skill")
	require.True(t, HasOrigin(skillDir))

	require.NoError(t, imp.Detach(context.Background(), "test-skill"))

	assert.False(t, HasOrigin(skillDir), "origin sidecar removed")
	_, err = os.Stat(filepath.Join(skillDir, "SKILL.md"))
	assert.NoError(t, err, "SKILL.md remains on disk")
	_, err = store.GetSkill("test-skill")
	assert.NoError(t, err, "skill remains in the registry")

	lf, err := ReadLockFile(lockPath)
	require.NoError(t, err)
	_, _, found := lf.FindSkillSource("test-skill")
	assert.False(t, found, "lock entry removed")
}

func TestImporter_Diff(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")
	repoDir, repo := initSkillRepo(t, "# Test\n\nv1 body.\n")
	imp := NewImporter(store, regDir, lockPath, slog.Default())
	_, err := imp.Import(ImportOptions{Repo: repoDir, Ref: "master", Trust: true})
	require.NoError(t, err)

	edited := driftSkill(t, store, "test-skill", "Local note.")
	commitChange(t, repo, repoDir, "# Test\n\nv2 body.\n")

	diff, err := imp.Diff(context.Background(), "test-skill")
	require.NoError(t, err)
	assert.Equal(t, "test-skill", diff.Skill)
	assert.Equal(t, edited, diff.Local, "local side is the current on-disk file")
	assert.True(t, diff.Drifted, "edited skill reports drift")
	assert.Contains(t, diff.Upstream, "v2 body.", "upstream side reflects the latest commit")
	assert.NotContains(t, diff.Upstream, "Local note.", "upstream side excludes local edits")

	// Diff must not mutate on-disk content or tracking.
	onDisk, err := os.ReadFile(filepath.Join(imp.skillDir("test-skill"), "SKILL.md"))
	require.NoError(t, err)
	assert.Equal(t, edited, string(onDisk))
	origin, err := ReadOrigin(imp.skillDir("test-skill"))
	require.NoError(t, err)
	assert.False(t, strings.Contains(origin.CommitSHA, " "))
}

func TestImporter_Diff_NoOrigin(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store, regDir := setupTestRegistry(t)
	createTestSkill(t, store, "local-only")
	imp := NewImporter(store, regDir, filepath.Join(regDir, "skills.lock.yaml"), slog.Default())

	_, err := imp.Diff(context.Background(), "local-only")
	assert.Error(t, err, "a skill without an origin cannot be diffed")
}

// TestImporterDiff_UpstreamMalformedSurfacesParseError verifies that when
// the upstream SKILL.md no longer parses, Diff reports the parse failure
// instead of the misleading "not found at upstream path".
func TestImporterDiff_UpstreamMalformedSurfacesParseError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")
	repoDir, repo := initSkillRepo(t, "# Test\n\nv1.\n")
	imp := NewImporter(store, regDir, lockPath, slog.Default())
	_, err := imp.Import(ImportOptions{Repo: repoDir, Ref: "master", Trust: true})
	require.NoError(t, err)

	// Upstream pushes a SKILL.md with broken frontmatter.
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "SKILL.md"),
		[]byte("---\nname: [unclosed\ndescription: broken\n---\n\nBody.\n"), 0644))
	wt, err := repo.Worktree()
	require.NoError(t, err)
	_, err = wt.Add("SKILL.md")
	require.NoError(t, err)
	_, err = wt.Commit("break frontmatter", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@test.com"},
	})
	require.NoError(t, err)

	_, err = imp.Diff(context.Background(), "test-skill")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "not found at upstream path")
	assert.Contains(t, err.Error(), "failed to parse")
}
