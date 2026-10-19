package skills

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdate_RefusesLockedSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")
	imp := NewImporter(store, regDir, lockPath, slog.Default())
	repo, _ := initSkillRepo(t, "# Test\n\nFirst.\n")
	_, err := imp.Import(context.Background(), ImportOptions{Repo: repo, Ref: "master", Trust: true})
	require.NoError(t, err)
	require.NoError(t, MutateLockFile(context.Background(), lockPath, func(lf *LockFile) (bool, error) {
		src := lf.Sources[RepoToName(repo)]
		src.LockedBy = "netops-team"
		lf.SetSource(RepoToName(repo), src)
		return true, nil
	}))
	_, err = imp.Update(context.Background(), "test-skill", false, false, true)
	require.ErrorIs(t, err, ErrSourceLocked)
	require.Contains(t, err.Error(), "source is locked by netops-team")
	_, err = imp.Update(context.Background(), "test-skill", false, false, true, true)
	require.NoError(t, err)
}
