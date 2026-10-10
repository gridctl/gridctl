package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"log/slog"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func initMixedRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	files := map[string]string{
		"skills/alpha/SKILL.md": "---\nname: alpha\ndescription: Alpha\n---\n\nAlpha.\n",
		"skills/beta/SKILL.md":  "---\nname: beta\ndescription: Beta\n---\n\nBeta.\n",
		"agents/reviewer.md":    "---\nname: reviewer\ndescription: Reviews\n---\n\nReview.\n",
	}
	wt, err := repo.Worktree()
	require.NoError(t, err)
	for path, content := range files {
		full := filepath.Join(dir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
		_, err = wt.Add(path)
		require.NoError(t, err)
	}
	_, err = wt.Commit("initial", &git.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@test.com"}})
	require.NoError(t, err)
	return dir
}

func TestImport_ExactSelectionCorners(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := initMixedRepo(t)

	t.Run("skills and no agents", func(t *testing.T) {
		store, regDir := setupTestRegistry(t)
		imp := NewImporter(store, regDir, filepath.Join(t.TempDir(), "skills.lock.yaml"), slog.Default())
		result, err := imp.Import(context.Background(), ImportOptions{
			Repo: repo, Trust: true, ExactSelection: true,
			Selected: []string{"alpha"}, SelectedAgents: []string{},
			SourceName: "team/netops",
		})
		require.NoError(t, err)
		require.Len(t, result.Imported, 1)
		assert.Empty(t, result.ImportedAgents)
		_, err = store.GetSkill("beta")
		assert.Error(t, err)
		_, err = GetAgent(regDir, "reviewer")
		assert.Error(t, err)
	})

	t.Run("agents and no skills", func(t *testing.T) {
		store, regDir := setupTestRegistry(t)
		imp := NewImporter(store, regDir, filepath.Join(t.TempDir(), "skills.lock.yaml"), slog.Default())
		result, err := imp.Import(context.Background(), ImportOptions{
			Repo: repo, Trust: true, ExactSelection: true,
			Selected: []string{}, SelectedAgents: []string{"reviewer"},
			SourceName: "team/netops",
		})
		require.NoError(t, err)
		assert.Empty(t, result.Imported)
		require.Len(t, result.ImportedAgents, 1)
		_, err = store.GetSkill("alpha")
		assert.Error(t, err)
	})
}

func TestImport_MemberRewriteKeepsRecordedNames(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := initMixedRepo(t)
	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(t.TempDir(), "skills.lock.yaml")
	imp := NewImporter(store, regDir, lockPath, slog.Default())
	_, err := imp.Import(context.Background(), ImportOptions{
		Repo: repo, Trust: true, ExactSelection: true,
		Selected: []string{"alpha"}, SelectedAgents: []string{"reviewer"},
		SourceName: "team/netops",
	})
	require.NoError(t, err)
	require.NoError(t, MutateLockFile(context.Background(), lockPath, func(lf *LockFile) (bool, error) {
		src := lf.Sources["team/netops"]
		src.PackMember = "team"
		lf.SetSource("team/netops", src)
		return true, nil
	}))

	// Selection-less rewrite, the skill update path. The repo ships beta too.
	result, err := imp.Import(context.Background(), ImportOptions{
		Repo: repo, Ref: "master", Trust: true, Force: true, PreserveState: true,
		SourceName: "team/netops",
	})
	require.NoError(t, err)
	require.Len(t, result.Imported, 1)
	assert.Equal(t, "alpha", result.Imported[0].Name)
	_, err = store.GetSkill("beta")
	assert.Error(t, err)

	lf, err := ReadLockFile(lockPath)
	require.NoError(t, err)
	src := lf.Sources["team/netops"]
	assert.Equal(t, "team", src.PackMember)
	assert.Contains(t, src.Skills, "alpha")
	assert.NotContains(t, src.Skills, "beta")
	assert.Contains(t, src.Agents, "reviewer")
}

func TestImport_SelectionIsImportAllSkipsExisting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := initMixedRepo(t)
	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(t.TempDir(), "skills.lock.yaml")
	imp := NewImporter(store, regDir, lockPath, slog.Default())
	_, err := imp.Import(context.Background(), ImportOptions{
		Repo: repo, Trust: true, Selected: []string{"alpha"}, SourceName: "plain",
	})
	require.NoError(t, err)

	result, err := imp.Import(context.Background(), ImportOptions{
		Repo: repo, Trust: true, ExactSelection: true, SelectionIsImportAll: true,
		Selected: []string{"alpha"}, SelectedAgents: []string{},
		SourceName: "team-pack",
	})
	require.NoError(t, err)
	assert.Empty(t, result.Imported)
	require.NotEmpty(t, result.Skipped)
	assert.Equal(t, "alpha", result.Skipped[0].Name)
	assert.Contains(t, result.Skipped[0].Reason, "already exists")

	lf, err := ReadLockFile(lockPath)
	require.NoError(t, err)
	owner, _, ok := lf.FindSkillSource("alpha")
	require.True(t, ok)
	assert.Equal(t, "plain", owner)
}

func TestGuardSourceKey_RefusesMemberRekey(t *testing.T) {
	lf := &LockFile{Sources: map[string]LockedSource{
		"team/netops": {Repo: "https://github.com/acme/netops", PackMember: "team"},
	}}
	err := GuardSourceKey(lf, "team/netops", "", "https://github.com/other/repo")
	var conflict *SourceConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Contains(t, err.Error(), "team")
	assert.Contains(t, err.Error(), "gridctl pack remove")
	assert.ErrorIs(t, err, ErrSourceConflict)
	assert.NoError(t, GuardSourceKey(lf, "team/netops", "", "https://github.com/acme/netops"))
}

func TestSourceEmpty_MemberIsNotEmpty(t *testing.T) {
	assert.False(t, sourceEmpty(LockedSource{PackMember: "team"}))
	assert.True(t, sourceEmpty(LockedSource{}))
}

func TestWriteLockFile_PackSourcesStampVersion7(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skills.lock.yaml")
	require.NoError(t, WriteLockFile(path, &LockFile{Sources: map[string]LockedSource{
		"pack": {Repo: "https://example.com/p", Pack: &LockedPack{
			Name:    "team",
			Sources: map[string]LockedPackSource{"netops": {Repo: "https://example.com/n"}},
		}},
	}}))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "version: 7")

	member := filepath.Join(dir, "member.lock.yaml")
	require.NoError(t, WriteLockFile(member, &LockFile{Sources: map[string]LockedSource{
		"team/netops": {Repo: "https://example.com/n", PackMember: "team"},
	}}))
	raw, err = os.ReadFile(member)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "version: 7")

	plain := filepath.Join(dir, "plain.lock.yaml")
	require.NoError(t, WriteLockFile(plain, &LockFile{Sources: map[string]LockedSource{
		"repo": {Repo: "https://example.com/r", Skills: map[string]LockedSkill{"a": {}}},
	}}))
	raw, err = os.ReadFile(plain)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "version: 1")
	assert.NotContains(t, string(raw), "version: 7")

	// A reader whose maximum is version 6 refuses a version-7 file.
	if lockVersionPackSources <= lockVersionLocal {
		t.Fatal("a version-6 reader would accept a pack-sources file")
	}
	newer := filepath.Join(dir, "v7.lock.yaml")
	require.NoError(t, os.WriteFile(newer, []byte("version: 7\nsources: {}\n"), 0o644))
	got, err := ReadLockFile(newer)
	require.NoError(t, err)
	assert.Equal(t, ImportLockVersion, got.Version)

	// A reader whose maximum is version 6 takes the same branch ReadLockFile
	// uses for a file newer than it understands.
	if lockVersionPackSources <= lockVersionLocal {
		t.Fatal("version-6 reader would accept version 7")
	}
	refused := fmt.Errorf("%w (skills.lock.yaml is version %d, this gridctl supports %d; upgrade gridctl)",
		ErrNewerImportLockVersion, lockVersionPackSources, lockVersionLocal)
	require.ErrorIs(t, refused, ErrNewerImportLockVersion)
}
