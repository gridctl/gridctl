package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"log/slog"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImport_LocalRenameRecordsLockAndUpdates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")
	root := filepath.Join(home, "src", "widget")
	writeSkillDir(t, root, "widget", "body")
	imp := NewImporter(store, regDir, lockPath, slog.Default())

	result, err := imp.Import(context.Background(), ImportOptions{
		Repo: root, Kind: SourceKindLocal, Trust: true, Rename: "renamed-widget",
	})
	require.NoError(t, err)
	require.Len(t, result.Imported, 1)
	assert.Equal(t, "renamed-widget", result.Imported[0].Name)

	lf, err := ReadLockFile(lockPath)
	require.NoError(t, err)
	src, ok := lf.Sources["widget"]
	require.True(t, ok)
	assert.Contains(t, src.Skills, "renamed-widget")
	assert.NotEqual(t, CombineTrackedSourceHash(nil, nil), src.ContentHash)

	updated, err := imp.Update(context.Background(), "renamed-widget", false, false, true)
	require.NoError(t, err)
	require.NotEmpty(t, updated.Warnings)
	assert.Contains(t, updated.Warnings[0], "already up to date")
}

func TestImport_LocalCanDropRequiresFullDiscovery(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")
	root := filepath.Join(home, "src")
	writeSkillDir(t, filepath.Join(root, "alpha"), "alpha", "a")
	writeSkillDir(t, filepath.Join(root, "beta"), "beta", "b")
	imp := NewImporter(store, regDir, lockPath, slog.Default())
	_, err := imp.Import(context.Background(), ImportOptions{Repo: root, Kind: SourceKindLocal, Trust: true})
	require.NoError(t, err)

	require.NoError(t, os.RemoveAll(filepath.Join(root, "beta")))
	_, err = imp.Import(context.Background(), ImportOptions{
		Repo: root, Kind: SourceKindLocal, Trust: true, Selected: []string{"alpha"},
	})
	require.NoError(t, err)
	lf, err := ReadLockFile(lockPath)
	require.NoError(t, err)
	assert.Contains(t, lf.Sources["src"].Skills, "beta")

	_, err = imp.Import(context.Background(), ImportOptions{Repo: root, Kind: SourceKindLocal, Trust: true})
	require.NoError(t, err)
	lf, err = ReadLockFile(lockPath)
	require.NoError(t, err)
	assert.Contains(t, lf.Sources["src"].Skills, "alpha")
	assert.NotContains(t, lf.Sources["src"].Skills, "beta")
	assert.Equal(t, CombineTrackedSourceHash(lf.Sources["src"].Skills, lf.Sources["src"].Agents), lf.Sources["src"].ContentHash)
}

func TestImport_GitSourceNameSurvivesUpdate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")
	local := filepath.Join(home, "local", "skills")
	writeSkillDir(t, filepath.Join(local, "widget"), "widget", "local")
	imp := NewImporter(store, regDir, lockPath, slog.Default())
	_, err := imp.Import(context.Background(), ImportOptions{Repo: local, Kind: SourceKindLocal, Trust: true})
	require.NoError(t, err)

	repo := filepath.Join(home, "git", "skills")
	initNamedGitSkill(t, repo, "git-skill", "v1")
	_, err = imp.Import(context.Background(), ImportOptions{Repo: repo, Ref: "master", Trust: true})
	require.ErrorIs(t, err, ErrSourceConflict)
	assert.Contains(t, err.Error(), "--source-name")

	_, err = imp.Import(context.Background(), ImportOptions{
		Repo: repo, Ref: "master", Trust: true, SourceName: "repo-skills",
	})
	require.NoError(t, err)

	_, err = imp.Update(context.Background(), "git-skill", false, true, true)
	require.NoError(t, err)
	lf, err := ReadLockFile(lockPath)
	require.NoError(t, err)
	assert.Len(t, lf.Sources, 2)
	assert.Contains(t, lf.Sources, "skills")
	assert.Contains(t, lf.Sources["repo-skills"].Skills, "git-skill")
	assert.Contains(t, lf.Sources["skills"].Skills, "widget")
}

func TestImport_ForcedGitTransferReleasesLocalOwnership(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")
	root := filepath.Join(home, "mine")
	writeSkillDir(t, filepath.Join(root, "one"), "one", "local")
	writeSkillDir(t, filepath.Join(root, "keep"), "keep", "stay")
	imp := NewImporter(store, regDir, lockPath, slog.Default())
	_, err := imp.Import(context.Background(), ImportOptions{Repo: root, Kind: SourceKindLocal, Trust: true})
	require.NoError(t, err)
	require.NoError(t, MutateLockFile(context.Background(), lockPath, func(lf *LockFile) (bool, error) {
		src := lf.Sources["mine"]
		src.Pack = &LockedPack{Name: "kept-pack"}
		lf.Sources["mine"] = src
		return true, nil
	}))

	repo := initRepoWithSkillContent(t, map[string]string{
		"SKILL.md": "---\nname: one\ndescription: one skill\n---\n\nfrom git\n",
	})
	_, err = imp.Import(context.Background(), ImportOptions{
		Repo: repo, Ref: "master", Trust: true, Force: true, SourceName: "upstream",
	})
	require.NoError(t, err)
	lf, err := ReadLockFile(lockPath)
	require.NoError(t, err)
	mine := lf.Sources["mine"]
	assert.NotContains(t, mine.Skills, "one")
	assert.Contains(t, mine.Skills, "keep")
	require.NotNil(t, mine.Pack)
	assert.Equal(t, "kept-pack", mine.Pack.Name)
	assert.Equal(t, CombineTrackedSourceHash(mine.Skills, mine.Agents), mine.ContentHash)
	assert.Contains(t, lf.Sources["upstream"].Skills, "one")
}

func TestImport_PackCannotReplaceLocalOwner(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")
	root := filepath.Join(home, "local", "alpha")
	writeSkillDir(t, root, "alpha", "local body")
	imp := NewImporter(store, regDir, lockPath, slog.Default())
	_, err := imp.Import(context.Background(), ImportOptions{Repo: root, Kind: SourceKindLocal, Trust: true})
	require.NoError(t, err)
	before, err := os.ReadFile(filepath.Join(regDir, "skills", "alpha", "SKILL.md"))
	require.NoError(t, err)

	_, err = imp.Import(context.Background(), ImportOptions{
		Repo:       "https://example.invalid/pack.git",
		PackImport: true,
		SourceName: "pack-src",
		Selected:   []string{"alpha"},
		Discovered: &CloneResult{
			RepoPath: t.TempDir(),
			Skills: []DiscoveredSkill{{
				Name: "alpha",
				Path: "alpha",
			}},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "local source")
	after, err := os.ReadFile(filepath.Join(regDir, "skills", "alpha", "SKILL.md"))
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after))
	lf, err := ReadLockFile(lockPath)
	require.NoError(t, err)
	assert.NotContains(t, lf.Sources, "pack-src")
	assert.Contains(t, lf.Sources["alpha"].Skills, "alpha")
}

func TestUpdate_LocalAmbiguousOwnerRefuses(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")
	root := filepath.Join(home, "src", "alpha")
	writeSkillDir(t, root, "alpha", "body")
	imp := NewImporter(store, regDir, lockPath, slog.Default())
	_, err := imp.Import(context.Background(), ImportOptions{Repo: root, Kind: SourceKindLocal, Trust: true})
	require.NoError(t, err)
	require.NoError(t, MutateLockFile(context.Background(), lockPath, func(lf *LockFile) (bool, error) {
		src := lf.Sources["alpha"]
		lf.Sources["other"] = LockedSource{
			Kind:   SourceKindLocal,
			Repo:   filepath.Join(home, "other"),
			Skills: map[string]LockedSkill{"alpha": src.Skills["alpha"]},
		}
		return true, nil
	}))
	before, err := ReadLockFile(lockPath)
	require.NoError(t, err)

	_, err = imp.Update(context.Background(), "alpha", false, true, true)
	require.ErrorIs(t, err, ErrAmbiguousOwner)
	after, err := ReadLockFile(lockPath)
	require.NoError(t, err)
	assert.Equal(t, before.Sources["alpha"].ContentHash, after.Sources["alpha"].ContentHash)
	assert.Contains(t, after.Sources["other"].Skills, "alpha")
}

func TestResolveLocalRoot_SymlinkAndFile(t *testing.T) {
	home := t.TempDir()
	real := filepath.Join(home, "real")
	require.NoError(t, os.MkdirAll(real, 0o755))
	link := filepath.Join(home, "link")
	require.NoError(t, os.Symlink(real, link))
	got, err := ResolveLocalRoot(link)
	require.NoError(t, err)
	assert.Equal(t, real, got)

	file := filepath.Join(home, "file.txt")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
	_, err = ResolveLocalRoot(file)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
	_, err = ResolveLocalRoot(filepath.Join(home, "missing"))
	require.Error(t, err)
}

func initNamedGitSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	repo, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	content := "---\nname: " + name + "\ndescription: " + name + " skill\n---\n\n" + body + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644))
	wt, err := repo.Worktree()
	require.NoError(t, err)
	_, err = wt.Add("SKILL.md")
	require.NoError(t, err)
	_, err = wt.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@test.com"},
	})
	require.NoError(t, err)
}
