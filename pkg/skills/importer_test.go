package skills

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

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

// initSkillRepo creates a local git repo with a SKILL.md and returns its path
// plus the worktree handle so the test can commit further changes.
func initSkillRepo(t *testing.T, body string) (string, *git.Repository) {
	t.Helper()

	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	require.NoError(t, err)

	skillContent := `---
name: test-skill
description: A test skill for state preservation
state: active
---

` + body
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillContent), 0644))

	wt, err := repo.Worktree()
	require.NoError(t, err)
	_, err = wt.Add("SKILL.md")
	require.NoError(t, err)
	_, err = wt.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@test.com"},
	})
	require.NoError(t, err)

	return dir, repo
}

// commitChange writes new SKILL.md content and creates a follow-up commit so
// that FetchAndCompare reports an available update.
func commitChange(t *testing.T, repo *git.Repository, dir, body string) {
	t.Helper()

	skillContent := `---
name: test-skill
description: A test skill for state preservation (v2)
state: active
---

` + body
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillContent), 0644))

	wt, err := repo.Worktree()
	require.NoError(t, err)
	_, err = wt.Add("SKILL.md")
	require.NoError(t, err)
	_, err = wt.Commit("update", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@test.com"},
	})
	require.NoError(t, err)
}

// TestImporter_Update_PreservesState verifies that re-syncing a source does
// not silently re-activate a skill the user disabled. Regression test for
// the bug where Importer.Update called Import(Force: true) which clobbered
// the user-set State.
func TestImporter_Update_PreservesState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")

	repoDir, repo := initSkillRepo(t, "# Test\n\nFirst version.\n")

	imp := NewImporter(store, regDir, lockPath, slog.Default())

	// Initial import → skill should be active. Pin to "master" (go-git's
	// default branch) so FetchAndCompare can resolve via origin/master after
	// a subsequent fetch.
	result, err := imp.Import(ImportOptions{Repo: repoDir, Ref: "master", Trust: true})
	require.NoError(t, err)
	require.Len(t, result.Imported, 1)

	sk, err := store.GetSkill("test-skill")
	require.NoError(t, err)
	assert.Equal(t, registry.StateActive, sk.State)

	// User disables the skill.
	sk.State = registry.StateDisabled
	require.NoError(t, store.SaveSkill(sk))

	// Push a new upstream commit so Update will re-import (rather than
	// short-circuit on "already up to date").
	commitChange(t, repo, repoDir, "# Test\n\nSecond version.\n")

	// Sync. The bug under test would reset State to active here.
	updateResult, err := imp.Update("test-skill", false, false)
	require.NoError(t, err)
	require.Len(t, updateResult.Imported, 1, "expected a re-import after upstream change")

	sk, err = store.GetSkill("test-skill")
	require.NoError(t, err)
	assert.Equal(t, registry.StateDisabled, sk.State,
		"disabled skill should remain disabled after sync")
}

// TestImporter_Import_NoPreserveStateResetsState confirms that the default
// Import path (PreserveState=false, used by `gridctl skill add`) still
// (re)sets state to active. Sanity check that the preservation flag is
// scoped to Update.
func TestImporter_Import_NoPreserveStateResetsState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")

	repoDir, _ := initSkillRepo(t, "# Test\n\nBody.\n")

	imp := NewImporter(store, regDir, lockPath, slog.Default())

	// Initial import.
	_, err := imp.Import(ImportOptions{Repo: repoDir, Trust: true})
	require.NoError(t, err)

	sk, err := store.GetSkill("test-skill")
	require.NoError(t, err)
	sk.State = registry.StateDisabled
	require.NoError(t, store.SaveSkill(sk))

	// Re-import without PreserveState: state should be reset to active.
	_, err = imp.Import(ImportOptions{Repo: repoDir, Trust: true, Force: true})
	require.NoError(t, err)

	sk, err = store.GetSkill("test-skill")
	require.NoError(t, err)
	assert.Equal(t, registry.StateActive, sk.State)
}

// TestImporter_Import_PreserveStateNewSkillDefaultsActive verifies that when
// PreserveState=true is passed but the skill doesn't yet exist, the default
// (StateActive / StateDraft) still applies. Preservation only kicks in when
// there is existing state to preserve.
func TestImporter_Import_PreserveStateNewSkillDefaultsActive(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")

	repoDir, _ := initSkillRepo(t, "# Test\n\nBody.\n")

	imp := NewImporter(store, regDir, lockPath, slog.Default())

	// First-time import with PreserveState. No existing skill to preserve.
	_, err := imp.Import(ImportOptions{Repo: repoDir, Trust: true, PreserveState: true})
	require.NoError(t, err)

	sk, err := store.GetSkill("test-skill")
	require.NoError(t, err)
	assert.Equal(t, registry.StateActive, sk.State)
}

// TestImporter_Update_ConcurrentSourcesPreserveLockfile verifies that
// concurrent Update calls against different sources both land in the lock
// file. Pre-fix, the read-modify-write window was unguarded and the last
// writer would silently drop the other source's entries. Run under -race.
func TestImporter_Update_ConcurrentSourcesPreserveLockfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")

	// Two independent local repos with distinct skill names so they don't
	// clobber each other in the registry.
	repoA := initSkillRepoNamed(t, "skill-a", "# A\n\nv1.\n")
	repoB := initSkillRepoNamed(t, "skill-b", "# B\n\nv1.\n")

	imp := NewImporter(store, regDir, lockPath, slog.Default())

	// Initial imports so both sources exist in the lock file.
	_, err := imp.Import(ImportOptions{Repo: repoA.dir, Ref: "master", Trust: true})
	require.NoError(t, err)
	_, err = imp.Import(ImportOptions{Repo: repoB.dir, Ref: "master", Trust: true})
	require.NoError(t, err)

	// Push new commits to both so Update will re-import them.
	commitChangeNamed(t, repoA, "skill-a", "# A\n\nv2.\n")
	commitChangeNamed(t, repoB, "skill-b", "# B\n\nv2.\n")

	// Concurrent updates.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = imp.Update("skill-a", false, false)
	}()
	go func() {
		defer wg.Done()
		_, _ = imp.Update("skill-b", false, false)
	}()
	wg.Wait()

	lf, err := ReadLockFile(lockPath)
	require.NoError(t, err)
	assert.Len(t, lf.Sources, 2, "both sources must survive concurrent Update")
	for srcName, src := range lf.Sources {
		assert.NotEmpty(t, src.Skills, "source %q lost its skills under concurrent Update", srcName)
	}
}

// initSkillRepoNamed is initSkillRepo with a configurable skill name.
type testRepo struct {
	dir  string
	repo *git.Repository
}

func initSkillRepoNamed(t *testing.T, skillName, body string) testRepo {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	require.NoError(t, err)

	content := "---\nname: " + skillName + "\ndescription: test\nstate: active\n---\n\n" + body
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644))

	wt, err := repo.Worktree()
	require.NoError(t, err)
	_, err = wt.Add("SKILL.md")
	require.NoError(t, err)
	_, err = wt.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@test.com"},
	})
	require.NoError(t, err)

	return testRepo{dir: dir, repo: repo}
}

func commitChangeNamed(t *testing.T, r testRepo, skillName, body string) {
	t.Helper()
	content := "---\nname: " + skillName + "\ndescription: test v2\nstate: active\n---\n\n" + body
	require.NoError(t, os.WriteFile(filepath.Join(r.dir, "SKILL.md"), []byte(content), 0644))
	wt, err := r.repo.Worktree()
	require.NoError(t, err)
	_, err = wt.Add("SKILL.md")
	require.NoError(t, err)
	_, err = wt.Commit("update", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@test.com"},
	})
	require.NoError(t, err)
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
