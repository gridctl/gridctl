package skills

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/require"
)

func TestCloneAtCommit_AncestorOfMovedBranch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)

	remote := initMovedBranchRepo(t)
	first := remote.older

	got, err := CloneAtCommit(remote.path, first, "", AuthConfig{}, slog.Default())
	require.NoError(t, err)
	require.Equal(t, first, got.CommitSHA)
	require.NotEqual(t, remote.tip, got.CommitSHA)

	// A second call hits the cache and must still land on the locked commit,
	// not the branch tip a depth-1 ref clone would return.
	again, err := CloneAtCommit(remote.path, first, "", AuthConfig{}, slog.Default())
	require.NoError(t, err)
	require.Equal(t, first, again.CommitSHA)
}

func TestCloneAtCommit_Unreachable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)

	remote := initMovedBranchRepo(t)
	missing := "0123456789abcdef0123456789abcdef01234567"
	_, err := CloneAtCommit(remote.path, missing, "", AuthConfig{}, slog.Default())
	require.Error(t, err)
	require.ErrorIs(t, err, ErrCommitUnreachable)
	require.NotErrorIs(t, err, os.ErrNotExist)
}

func TestCloneAtCommit_DoesNotReportTransportAsUnreachable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	_, err := CloneAtCommit(filepath.Join(t.TempDir(), "missing.git"), "0123456789abcdef0123456789abcdef01234567", "", AuthConfig{}, slog.Default())
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrCommitUnreachable), "err = %v", err)
}

type movedRepo struct {
	path  string
	older string
	tip   string
}

func initMovedBranchRepo(t *testing.T) movedRepo {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	writeSkillFile(t, dir, ".", "---\nname: locked-skill\ndescription: locked\n---\n\nbody\n")
	wt, err := repo.Worktree()
	require.NoError(t, err)
	_, err = wt.Add("SKILL.md")
	require.NoError(t, err)
	older, err := wt.Commit("older", &git.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@test.com"}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: locked-skill\ndescription: locked\n---\n\nnewer\n"), 0o644))
	_, err = wt.Add("SKILL.md")
	require.NoError(t, err)
	tip, err := wt.Commit("tip", &git.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@test.com"}})
	require.NoError(t, err)
	return movedRepo{path: dir, older: older.String(), tip: tip.String()}
}
