package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"log/slog"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnumerateClient_SkipsAndResolves(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	grid := filepath.Join(home, ".gridctl")
	require.NoError(t, os.MkdirAll(grid, 0o755))

	claudeSkills := filepath.Join(home, ".claude", "skills")
	writeSkillDir(t, filepath.Join(claudeSkills, "pcap-analysis"), "pcap-analysis", "body")
	writeSkillDir(t, filepath.Join(claudeSkills, "release-notes"), "release-notes", "body")
	require.NoError(t, os.WriteFile(filepath.Join(claudeSkills, "release-notes", ".origin.json"), []byte("{}\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(claudeSkills, "synced", "inner"), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(grid, "missing"), filepath.Join(claudeSkills, "dangling")))
	inside := filepath.Join(grid, "registry", "copied")
	writeSkillDir(t, inside, "copied", "body")
	require.NoError(t, os.Symlink(inside, filepath.Join(claudeSkills, "from-home")))

	outside := filepath.Join(home, "outside", "linked-skill")
	writeSkillDir(t, outside, "linked-skill", "body")
	require.NoError(t, os.Symlink(outside, filepath.Join(claudeSkills, "linked-skill")))

	projected := filepath.Join(claudeSkills, "projected")
	writeSkillDir(t, projected, "projected", "body")
	lockYAML := "version: 1\nrevision: 2\nprojections:\n  - kind: skill\n    client: claude-code\n    source: projected\n    path: " + projected + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(grid, "project.lock.yaml"), []byte(lockYAML), 0o644))

	agentsDir := filepath.Join(home, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "reviewer.md"), []byte(importableAgent), 0o644))

	candidates, err := EnumerateClient(context.Background(), "claude-code", home)
	require.NoError(t, err)
	byName := map[string]ClientCandidate{}
	for _, c := range candidates {
		byName[c.Kind+"/"+c.Name] = c
	}
	assert.Equal(t, "", byName["skill/pcap-analysis"].SkipReason)
	assert.Equal(t, "claude-code", byName["skill/pcap-analysis"].Source)
	assert.Equal(t, skipGridctlProjection, byName["skill/release-notes"].SkipReason)
	assert.Equal(t, skipGridctlProjection, byName["skill/projected"].SkipReason)
	assert.Equal(t, skipDanglingSymlink, byName["skill/dangling"].SkipReason)
	assert.Equal(t, skipGridctlProjection, byName["skill/from-home"].SkipReason)
	assert.Equal(t, skipClaudeSyncDir, byName["skill/synced"].SkipReason)
	assert.Equal(t, "", byName["skill/linked-skill"].SkipReason)
	assert.Equal(t, outside, byName["skill/linked-skill"].Root)
	assert.Equal(t, "skill", byName["skill/linked-skill"].Kind)
	assert.Equal(t, "", byName["agent/reviewer"].SkipReason)
	assert.Equal(t, "claude-code-agents", byName["agent/reviewer"].Source)
}

func TestEnumerateClient_OpenCodeDialectSkipped(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	dir := filepath.Join(home, ".config", "opencode", "agents")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "review.md"), []byte("---\nmode: primary\n---\n\nReview.\n"), 0o644))
	candidates, err := EnumerateClient(context.Background(), "opencode", home)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	assert.Equal(t, "OpenCode agent dialect is not imported in this release", candidates[0].SkipReason)
}

func TestApplyClientImport_ReusesResolvedSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	agentsRoot := filepath.Join(home, ".agents", "skills", "foo")
	writeSkillDir(t, agentsRoot, "foo", "body")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude", "skills"), 0o755))
	require.NoError(t, os.Symlink(agentsRoot, filepath.Join(home, ".claude", "skills", "foo")))

	store, regDir := setupTestRegistry(t)
	imp := NewImporter(store, regDir, filepath.Join(regDir, "skills.lock.yaml"), slog.Default())
	first, err := EnumerateClient(context.Background(), "claude-code", home)
	require.NoError(t, err)
	_, err = ApplyClientImport(context.Background(), imp, ClientImportOptions{Trust: true}, first)
	require.NoError(t, err)
	second, err := EnumerateClient(context.Background(), "agents", home)
	require.NoError(t, err)
	result, err := ApplyClientImport(context.Background(), imp, ClientImportOptions{Trust: true}, second)
	require.NoError(t, err)
	require.NotEmpty(t, result.Entries)
	assert.Equal(t, "imported", result.Entries[0].Action)

	lf, err := ReadLockFile(imp.lockPath)
	require.NoError(t, err)
	assert.Contains(t, lf.Sources, "agents")
	assert.NotContains(t, lf.Sources, "agents-agents")
	origin, err := ReadOrigin(filepath.Join(regDir, "skills", "foo"))
	require.NoError(t, err)
	assert.Equal(t, "agents", origin.Client)
	assert.Equal(t, agentsRoot, origin.Location)
}

func TestProjectionHint_DirectChild(t *testing.T) {
	home := t.TempDir()
	child := filepath.Join(home, ".claude", "skills", "pcap-analysis")
	require.NoError(t, os.MkdirAll(child, 0o755))
	plain := filepath.Join(home, "skills", "pcap-analysis")
	require.NoError(t, os.MkdirAll(plain, 0o755))
	clients, err := ProjectionHintClients(context.Background(), home, []string{child})
	require.NoError(t, err)
	assert.Equal(t, []string{"claude-code"}, clients)
	clients, err = ProjectionHintClients(context.Background(), home, []string{plain})
	require.NoError(t, err)
	assert.Empty(t, clients)
}
