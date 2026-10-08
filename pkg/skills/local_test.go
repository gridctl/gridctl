package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"log/slog"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteLockFile_LocalStampsVersion6(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skills.lock.yaml")
	require.NoError(t, WriteLockFile(path, &LockFile{Sources: map[string]LockedSource{
		"pcap": {Kind: SourceKindLocal, Repo: "/tmp/pcap", Skills: map[string]LockedSkill{
			"pcap": {Path: ".", TreeHash: "sha256:abc"},
		}},
	}}))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "version: 6")

	gitPath := filepath.Join(dir, "git.lock.yaml")
	require.NoError(t, WriteLockFile(gitPath, &LockFile{Sources: map[string]LockedSource{
		"repo": {Repo: "https://github.com/acme/skills", CommitSHA: "abc", Skills: map[string]LockedSkill{
			"one": {Path: "one", ContentHash: "hash"},
		}},
	}}))
	gitData, err := os.ReadFile(gitPath)
	require.NoError(t, err)
	assert.NotContains(t, string(gitData), "version: 6")
	assert.Contains(t, string(gitData), "version: 1")

	newer := filepath.Join(dir, "newer.lock.yaml")
	require.NoError(t, os.WriteFile(newer, []byte("version: 7\nsources: {}\n"), 0o644))
	_, err = ReadLockFile(newer)
	require.ErrorIs(t, err, ErrNewerImportLockVersion)
}

func TestSkillTreeHash_SortedAndExcludesSidecar(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("skill"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "scripts"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scripts", "run.sh"), []byte("echo hi"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".origin.json"), []byte("{}"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md.pre-local"), []byte("old"), 0o644))

	hash, err := SkillTreeHash(context.Background(), dir)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(hash, "sha256:"))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "scripts", "run.sh"), []byte("echo bye"), 0o644))
	changed, err := SkillTreeHash(context.Background(), dir)
	require.NoError(t, err)
	assert.NotEqual(t, hash, changed)
}

func TestCombineTrackedSourceHash_DomainSeparated(t *testing.T) {
	skills := map[string]LockedSkill{"a": {TreeHash: "sha256:1"}}
	agents := map[string]LockedAgent{"a": {ContentHash: "sha256:1"}}
	one := CombineTrackedSourceHash(skills, agents)
	swapped := CombineTrackedSourceHash(map[string]LockedSkill{"a": {TreeHash: "other"}}, agents)
	assert.NotEqual(t, one, swapped)
	assert.Equal(t, one, CombineTrackedSourceHash(skills, agents))
	assert.True(t, strings.HasPrefix(one, "sha256:"))
}

func TestDiscoverLocal_NameMismatchAndManagedCopy(t *testing.T) {
	root := t.TempDir()
	writeSkillDir(t, filepath.Join(root, "alpha"), "front-name", "alpha body")
	managed := filepath.Join(root, "copied")
	writeSkillDir(t, managed, "copied", "copied body")
	require.NoError(t, os.WriteFile(filepath.Join(managed, ".origin.json"), []byte("{}\n"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(root, "agents"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "agents", "reviewer.md"), []byte(importableAgent), 0o644))
	require.NoError(t, os.Symlink(filepath.Join(root, "alpha"), filepath.Join(root, "linked")))

	disc, err := DiscoverLocal(context.Background(), root, "", slog.Default())
	require.NoError(t, err)
	require.Len(t, disc.Result.Skills, 1)
	assert.Equal(t, "alpha", disc.Result.Skills[0].Name)
	require.Len(t, disc.Result.Agents, 1)
	assert.Equal(t, "reviewer", disc.Result.Agents[0].Name)
	joined := strings.Join(disc.Warnings, "\n")
	assert.Contains(t, joined, `name mismatch: frontmatter "front-name", directory "alpha"`)
	assert.Contains(t, joined, "skipping managed copy")
	assert.Contains(t, joined, "skipping symlink")
}

func TestImport_LocalDirectoryRecordsTreeHash(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")
	root := filepath.Join(home, "src")
	writeSkillDir(t, filepath.Join(root, "alpha"), "alpha", "alpha body")
	writeSkillDir(t, filepath.Join(root, "beta"), "beta", "beta body")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "agents"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "agents", "reviewer.md"), []byte(importableAgent), 0o644))

	imp := NewImporter(store, regDir, lockPath, slog.Default())
	result, err := imp.Import(context.Background(), ImportOptions{Repo: root, Kind: SourceKindLocal, Trust: true})
	require.NoError(t, err)
	assert.Len(t, result.Imported, 2)
	assert.Len(t, result.ImportedAgents, 1)

	origin, err := ReadOrigin(filepath.Join(regDir, "skills", "alpha"))
	require.NoError(t, err)
	assert.Equal(t, SourceKindLocal, origin.Kind)
	assert.Equal(t, root, origin.Repo)
	assert.Empty(t, origin.CommitSHA)
	assert.Empty(t, origin.Ref)
	assert.NotEmpty(t, origin.ContentHash)
	assert.Empty(t, origin.Client)

	lf, err := ReadLockFile(lockPath)
	require.NoError(t, err)
	src := lf.Sources[filepath.Base(root)]
	assert.True(t, src.IsLocal())
	assert.Empty(t, src.CommitSHA)
	assert.NotEmpty(t, src.Skills["alpha"].TreeHash)
	assert.True(t, strings.HasPrefix(src.Skills["alpha"].TreeHash, "sha256:"))
	assert.Equal(t, CombineTrackedSourceHash(src.Skills, src.Agents), src.ContentHash)

	raw, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "version: 6")
}

func TestImport_LocalNameUsesDirectoryGitKeepsFrontmatter(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")
	root := filepath.Join(home, "src", "widget")
	writeSkillDir(t, root, "other-name", "body")
	imp := NewImporter(store, regDir, lockPath, slog.Default())
	result, err := imp.Import(context.Background(), ImportOptions{Repo: root, Kind: SourceKindLocal, Trust: true})
	require.NoError(t, err)
	require.Len(t, result.Imported, 1)
	assert.Equal(t, "widget", result.Imported[0].Name)
	assert.Contains(t, strings.Join(result.Warnings, "\n"), `installed as "widget"`)

	repo := initRepoWithSkillContent(t, map[string]string{
		"widget/SKILL.md": "---\nname: other-name\ndescription: git name\n---\n\nBody.\n",
	})
	gitResult, err := imp.Import(context.Background(), ImportOptions{Repo: repo, Trust: true})
	require.NoError(t, err)
	require.Len(t, gitResult.Imported, 1)
	assert.Equal(t, "other-name", gitResult.Imported[0].Name)
}

func TestImport_CancelledContextWritesNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")
	root := filepath.Join(home, "src")
	writeSkillDir(t, root, "alpha", "body")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	imp := NewImporter(store, regDir, lockPath, slog.Default())
	_, err := imp.Import(ctx, ImportOptions{Repo: root, Kind: SourceKindLocal, Trust: true})
	require.ErrorIs(t, err, context.Canceled)
	_, statErr := os.Stat(filepath.Join(regDir, "skills", "alpha"))
	assert.True(t, os.IsNotExist(statErr))
}

func TestUpdate_LocalUnchangedChangedAndMissing(t *testing.T) {
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

	same, err := imp.Update(context.Background(), "alpha", false, false, true)
	require.NoError(t, err)
	assert.Contains(t, strings.Join(same.Warnings, "\n"), "already up to date")

	before, err := ReadLockFile(lockPath)
	require.NoError(t, err)
	prevHash := before.Sources["alpha"].ContentHash

	require.NoError(t, os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("---\nname: alpha\ndescription: Alpha skill\n---\n\nchanged\n"), 0o644))
	dry, err := imp.Update(context.Background(), "alpha", true, false, true)
	require.NoError(t, err)
	assert.Contains(t, strings.Join(dry.Warnings, "\n"), "update available (local content changed)")
	afterDry, err := ReadLockFile(lockPath)
	require.NoError(t, err)
	assert.Equal(t, prevHash, afterDry.Sources["alpha"].ContentHash)

	updated, err := imp.Update(context.Background(), "alpha", false, false, true)
	require.NoError(t, err)
	assert.NotEmpty(t, updated.Imported)
	after, err := ReadLockFile(lockPath)
	require.NoError(t, err)
	assert.NotEqual(t, prevHash, after.Sources["alpha"].ContentHash)
	assert.Empty(t, after.Sources["alpha"].CommitSHA)

	require.NoError(t, os.RemoveAll(root))
	_, err = imp.Update(context.Background(), "alpha", false, false, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no longer exists")
	unchanged, err := ReadLockFile(lockPath)
	require.NoError(t, err)
	assert.Equal(t, after.Sources["alpha"].ContentHash, unchanged.Sources["alpha"].ContentHash)
}

func TestPin_LocalRefused(t *testing.T) {
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
	err = imp.Pin("alpha", "v1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "local sources have no refs to pin")
}

func TestAdvanceTracking_RefusesLocal(t *testing.T) {
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
	err = imp.AdvanceTracking(context.Background(), "alpha", "deadbeef")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "local sources have no commit to advance")
}

func TestImport_LocalSelectPreservesSibling(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")
	root := filepath.Join(home, "src")
	writeSkillDir(t, filepath.Join(root, "alpha"), "alpha", "a")
	writeSkillDir(t, filepath.Join(root, "beta"), "beta", "b")
	imp := NewImporter(store, regDir, lockPath, slog.Default())
	_, err := imp.Import(context.Background(), ImportOptions{Repo: root, Kind: SourceKindLocal, Trust: true, Selected: []string{"alpha"}})
	require.NoError(t, err)
	_, err = imp.Import(context.Background(), ImportOptions{Repo: root, Kind: SourceKindLocal, Trust: true, Selected: []string{"beta"}})
	require.NoError(t, err)
	lf, err := ReadLockFile(lockPath)
	require.NoError(t, err)
	src := lf.Sources["src"]
	assert.Contains(t, src.Skills, "alpha")
	assert.Contains(t, src.Skills, "beta")
}

func TestImport_LocalCannotOverwriteOtherOwnerWithoutForce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")
	first := filepath.Join(home, "one", "alpha")
	second := filepath.Join(home, "two", "alpha")
	writeSkillDir(t, first, "alpha", "one")
	writeSkillDir(t, second, "alpha", "two")
	imp := NewImporter(store, regDir, lockPath, slog.Default())
	_, err := imp.Import(context.Background(), ImportOptions{Repo: first, Kind: SourceKindLocal, Trust: true})
	require.NoError(t, err)
	skipped, err := imp.Import(context.Background(), ImportOptions{Repo: second, Kind: SourceKindLocal, Trust: true, SourceName: "other"})
	require.NoError(t, err)
	require.NotEmpty(t, skipped.Skipped)
	assert.Contains(t, skipped.Skipped[0].Reason, "--force")
	lf, err := ReadLockFile(lockPath)
	require.NoError(t, err)
	assert.Contains(t, lf.Sources["alpha"].Skills, "alpha")
	_, other := lf.Sources["other"]
	assert.False(t, other)
}

func TestResolveSourceName_ClientSuffixAndExplicit(t *testing.T) {
	lf := &LockFile{Sources: map[string]LockedSource{
		"skills": {Kind: SourceKindLocal, Repo: "/tmp/a/skills"},
	}}
	name, err := ResolveSourceName(lf, SourceNameInput{Kind: SourceKindLocal, Root: "/tmp/b/skills", ClientImport: true})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(name, "skills-"))
	assert.Len(t, strings.TrimPrefix(name, "skills-"), 8)

	again, err := ResolveSourceName(lf, SourceNameInput{Kind: SourceKindLocal, Root: "/tmp/a/skills", Explicit: "other"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already tracks")
	_ = again

	_, err = ResolveSourceName(lf, SourceNameInput{Kind: SourceKindLocal, Root: "/tmp/c/skills"})
	require.ErrorIs(t, err, ErrSourceConflict)
	assert.Contains(t, err.Error(), "--source-name")
}

func TestInsideGridctlHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	inside := filepath.Join(home, ".gridctl", "registry")
	require.NoError(t, os.MkdirAll(inside, 0o755))
	ok, err := InsideGridctlHome(inside)
	require.NoError(t, err)
	assert.True(t, ok)
	outside := filepath.Join(home, "skills")
	require.NoError(t, os.MkdirAll(outside, 0o755))
	ok, err = InsideGridctlHome(outside)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestCheckUpdatesBackground_SkipsLocal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	reg := filepath.Join(home, "registry")
	require.NoError(t, os.MkdirAll(filepath.Join(reg, "skills", "alpha"), 0o755))
	require.NoError(t, WriteOrigin(filepath.Join(reg, "skills", "alpha"), &Origin{
		Kind: SourceKindLocal, Repo: filepath.Join(home, "src"), ContentHash: "abc",
	}))
	status := checkAllUpdates(reg, slog.Default())
	assert.Empty(t, status.Updates)
	assert.Empty(t, status.Errors)
}

func writeSkillDir(t *testing.T, dir, name, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	content := "---\nname: " + name + "\ndescription: " + name + " skill\n---\n\n" + body + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644))
}

func TestImport_RefusesGridctlHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	store, regDir := setupTestRegistry(t)
	root := filepath.Join(home, ".gridctl", "packs", "skill")
	writeSkillDir(t, root, "skill", "body")
	imp := NewImporter(store, regDir, filepath.Join(regDir, "skills.lock.yaml"), slog.Default())
	_, err := imp.Import(context.Background(), ImportOptions{Repo: root, Kind: SourceKindLocal, Trust: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to import from inside the gridctl home")
	_, statErr := store.GetSkill("skill")
	assert.Error(t, statErr)
}

func TestGuardSourceKey_GitCannotReplaceLocal(t *testing.T) {
	lf := &LockFile{Sources: map[string]LockedSource{
		"skills": {Kind: SourceKindLocal, Repo: "/tmp/skills"},
	}}
	err := GuardSourceKey(lf, "skills", "", "https://github.com/acme/skills")
	require.ErrorIs(t, err, ErrSourceConflict)
	assert.Contains(t, err.Error(), "--source-name")
}

func TestLocalSourceHasUpdate_IgnoresSibling(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	root := filepath.Join(home, "src")
	writeSkillDir(t, filepath.Join(root, "alpha"), "alpha", "body")
	hash, err := SkillTreeHash(context.Background(), filepath.Join(root, "alpha"))
	require.NoError(t, err)
	src := LockedSource{
		Kind: SourceKindLocal,
		Repo: root,
		Skills: map[string]LockedSkill{
			"alpha": {Path: "alpha", TreeHash: hash},
		},
	}
	writeSkillDir(t, filepath.Join(root, "beta"), "beta", "new sibling")
	changed, err := LocalSourceHasUpdate(context.Background(), src)
	require.NoError(t, err)
	assert.False(t, changed)
	require.NoError(t, os.Mkdir(filepath.Join(root, "alpha", "scripts"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "alpha", "scripts", "run.sh"), []byte("echo"), 0o644))
	changed, err = LocalSourceHasUpdate(context.Background(), src)
	require.NoError(t, err)
	assert.True(t, changed)
}
