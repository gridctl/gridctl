package skills

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectDrift_NoDriftAfterImport(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")

	repoDir, _ := initSkillRepo(t, "# Test\n\nBody.\n")

	imp := NewImporter(store, regDir, lockPath, slog.Default())
	_, err := imp.Import(ImportOptions{Repo: repoDir, Trust: true})
	require.NoError(t, err)

	drifted, err := DetectDrift(context.Background(), store, lockPath, "")
	require.NoError(t, err)
	assert.Empty(t, drifted, "fresh import should report no drift")
}

func TestDetectDrift_DetectsEditedSkill(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")

	repoDir, _ := initSkillRepo(t, "# Test\n\nBody.\n")

	imp := NewImporter(store, regDir, lockPath, slog.Default())
	_, err := imp.Import(ImportOptions{Repo: repoDir, Trust: true})
	require.NoError(t, err)

	// Tamper with the installed SKILL.md.
	skillFile := filepath.Join(regDir, "skills", "test-skill", "SKILL.md")
	current, err := os.ReadFile(skillFile)
	require.NoError(t, err)
	tampered := append(current, []byte("\n\n<!-- local edit -->\n")...)
	require.NoError(t, os.WriteFile(skillFile, tampered, 0644))

	drifted, err := DetectDrift(context.Background(), store, lockPath, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"test-skill"}, drifted)
}

func TestDetectDrift_FailsOpenWithoutInstalledHash(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")

	// Seed a skill the legacy way: no InstalledHash in origin.
	createTestSkill(t, store, "legacy-skill")
	skillDir := filepath.Join(regDir, "skills", "legacy-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("body"), 0644))
	require.NoError(t, WriteOrigin(skillDir, &Origin{
		Repo:        "https://github.com/org/repo",
		Ref:         "main",
		CommitSHA:   "abc123",
		ContentHash: "old-upstream-hash",
		// InstalledHash intentionally empty.
	}))

	drifted, err := DetectDrift(context.Background(), store, lockPath, "")
	require.NoError(t, err)
	assert.Empty(t, drifted, "missing InstalledHash should not be reported as drift")
}

func TestDetectDrift_PerSource(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")

	repoDir, _ := initSkillRepo(t, "# Test\n\nBody.\n")

	imp := NewImporter(store, regDir, lockPath, slog.Default())
	_, err := imp.Import(ImportOptions{Repo: repoDir, Trust: true})
	require.NoError(t, err)

	// Tamper.
	skillFile := filepath.Join(regDir, "skills", "test-skill", "SKILL.md")
	require.NoError(t, os.WriteFile(skillFile, []byte("tampered"), 0644))

	srcName := filepath.Base(repoDir)
	drifted, err := DetectDrift(context.Background(), store, lockPath, srcName)
	require.NoError(t, err)
	assert.Equal(t, []string{"test-skill"}, drifted)

	// Unknown source name → error.
	_, err = DetectDrift(context.Background(), store, lockPath, "no-such-source")
	assert.Error(t, err)
}
