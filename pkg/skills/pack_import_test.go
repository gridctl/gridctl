package skills

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const packSkillMD = `---
name: %s
description: Test skill
---

Do the thing.
`

const packAgentMD = `---
name: %s
description: Test agent
---

Review things.
`

// packFixtureRepo builds a local repo with two skills and two agents.
func packFixtureRepo(t *testing.T) string {
	t.Helper()
	return initRepoWithSkillContent(t, map[string]string{
		"skills/alpha/SKILL.md": sprintfName(packSkillMD, "alpha"),
		"skills/beta/SKILL.md":  sprintfName(packSkillMD, "beta"),
		"agents/reviewer.md":    sprintfName(packAgentMD, "reviewer"),
		"agents/tester.md":      sprintfName(packAgentMD, "tester"),
	})
}

func sprintfName(tpl, name string) string {
	out := ""
	for i := 0; i < len(tpl); i++ {
		if tpl[i] == '%' && i+1 < len(tpl) && tpl[i+1] == 's' {
			out += name
			i++
			continue
		}
		out += string(tpl[i])
	}
	return out
}

func TestImport_SelectedAgentsImportsExactly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")
	imp := NewImporter(store, regDir, lockPath, slog.Default())

	repo := packFixtureRepo(t)
	result, err := imp.Import(ImportOptions{
		Repo:           repo,
		Selected:       []string{"alpha"},
		SelectedAgents: []string{"reviewer"},
	})
	require.NoError(t, err)

	require.Len(t, result.Imported, 1)
	require.Equal(t, "alpha", result.Imported[0].Name)
	require.Len(t, result.ImportedAgents, 1)
	require.Equal(t, "reviewer", result.ImportedAgents[0].Name)

	if _, err := GetAgent(regDir, "tester"); err == nil {
		t.Error("unselected agent was imported")
	}

	lf, err := ReadLockFile(lockPath)
	require.NoError(t, err)
	_, src, ok := lf.FindAgentSource("reviewer")
	require.True(t, ok)
	require.Len(t, src.Agents, 1)
}

func TestImport_LegacySelectionStillSkipsAgents(t *testing.T) {
	// Article IX: a skill selection without SelectedAgents keeps the
	// existing contract (agents not on offer, none imported).
	t.Setenv("HOME", t.TempDir())
	store, regDir := setupTestRegistry(t)
	imp := NewImporter(store, regDir, filepath.Join(regDir, "skills.lock.yaml"), slog.Default())

	result, err := imp.Import(ImportOptions{
		Repo:     packFixtureRepo(t),
		Selected: []string{"alpha"},
	})
	require.NoError(t, err)
	require.Empty(t, result.ImportedAgents)
	if _, err := GetAgent(regDir, "reviewer"); err == nil {
		t.Error("legacy skill selection must not import agents")
	}
}

func TestLockedPack_RoundTripAndVersionStamping(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skills.lock.yaml")

	// No packs: written at version 1 (downgrade freedom preserved).
	lf := &LockFile{Sources: map[string]LockedSource{
		"plain": {Repo: "https://example.com/r", Skills: map[string]LockedSkill{"a": {}}},
	}}
	require.NoError(t, WriteLockFile(path, lf))
	data, _ := os.ReadFile(path)
	require.Contains(t, string(data), "version: 1")

	// With a pack: version 2 so older binaries refuse instead of
	// silently dropping the record.
	lf.SetSource("packsrc", LockedSource{
		Repo:   "https://example.com/p",
		Skills: map[string]LockedSkill{"alpha": {}},
		Pack: &LockedPack{
			Name: "team-pack", Version: "1.0.0", Wiring: true,
			Skills: []string{"alpha"}, Agents: []string{"reviewer"},
			Unresolved: []string{"ghost"},
		},
	})
	require.NoError(t, WriteLockFile(path, lf))
	data, _ = os.ReadFile(path)
	require.Contains(t, string(data), "version: 2")

	back, err := ReadLockFile(path)
	require.NoError(t, err)
	srcName, src, ok := back.FindPackSource("team-pack")
	require.True(t, ok)
	require.Equal(t, "packsrc", srcName)
	require.Equal(t, []string{"alpha"}, src.Pack.Skills)
	require.Equal(t, []string{"ghost"}, src.Pack.Unresolved)

	// Declarations require version 3, and survive a round trip without values.
	required := true
	src.Pack.Variables = map[string]LockedVariableDeclaration{
		"TOKEN": {Required: &required, Type: "string", Description: "API token"},
	}
	lf.SetSource(srcName, *src)
	require.NoError(t, WriteLockFile(path, lf))
	data, _ = os.ReadFile(path)
	require.Contains(t, string(data), "version: 3")
	back, err = ReadLockFile(path)
	require.NoError(t, err)
	_, src, ok = back.FindPackSource("team-pack")
	require.True(t, ok)
	require.Equal(t, "API token", src.Pack.Variables["TOKEN"].Description)
}

func TestImport_DiscoveredSkipsSecondClone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store, regDir := setupTestRegistry(t)
	imp := NewImporter(store, regDir, filepath.Join(regDir, "skills.lock.yaml"), slog.Default())

	repo := packFixtureRepo(t)
	clone, err := CloneAndDiscover(repo, "", "", AuthConfig{}, slog.Default())
	require.NoError(t, err)

	result, err := imp.Import(ImportOptions{
		Repo:           repo,
		Selected:       []string{"alpha", "beta"},
		SelectedAgents: []string{"reviewer", "tester"},
		Discovered:     clone,
	})
	require.NoError(t, err)
	require.Len(t, result.Imported, 2)
	require.Len(t, result.ImportedAgents, 2)
}

func TestImport_SourceRewritePreservesPackRecord(t *testing.T) {
	// H1 regression: skill update re-imports the source with Force; the
	// pack record must survive the SetSource rewrite.
	t.Setenv("HOME", t.TempDir())
	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")
	imp := NewImporter(store, regDir, lockPath, slog.Default())

	repo := packFixtureRepo(t)
	_, err := imp.Import(ImportOptions{Repo: repo})
	require.NoError(t, err)

	lf, err := ReadLockFile(lockPath)
	require.NoError(t, err)
	src := lf.Sources[RepoToName(repo)]
	src.Pack = &LockedPack{Name: "team-pack", Skills: []string{"alpha"}}
	lf.SetSource(RepoToName(repo), src)
	require.NoError(t, WriteLockFile(lockPath, lf))

	// Force re-import (the Update path).
	_, err = imp.Import(ImportOptions{Repo: repo, Force: true, PreserveState: true})
	require.NoError(t, err)

	back, err := ReadLockFile(lockPath)
	require.NoError(t, err)
	_, packSrc, ok := back.FindPackSource("team-pack")
	require.True(t, ok, "pack record wiped by source re-import")
	require.Equal(t, []string{"alpha"}, packSrc.Pack.Skills)
}

func TestImport_SelectedAgentsPreservesSiblingLockEntries(t *testing.T) {
	// M4 regression: selecting one agent from a source must not drop the
	// source's other agents from the lock.
	t.Setenv("HOME", t.TempDir())
	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")
	imp := NewImporter(store, regDir, lockPath, slog.Default())

	repo := packFixtureRepo(t)
	_, err := imp.Import(ImportOptions{Repo: repo}) // both agents in
	require.NoError(t, err)

	_, err = imp.Import(ImportOptions{Repo: repo, Selected: []string{"alpha"}, SelectedAgents: []string{"reviewer"}})
	require.NoError(t, err)

	lf, err := ReadLockFile(lockPath)
	require.NoError(t, err)
	src := lf.Sources[RepoToName(repo)]
	require.Contains(t, src.Agents, "tester", "sibling agent lock entry dropped")
	require.Contains(t, src.Agents, "reviewer")
}

func TestImport_SelectedAgentCrossSourceNeedsForce(t *testing.T) {
	// M5 regression: a pack selection must not silently overwrite an
	// agent another source owns.
	t.Setenv("HOME", t.TempDir())
	store, regDir := setupTestRegistry(t)
	lockPath := filepath.Join(regDir, "skills.lock.yaml")
	imp := NewImporter(store, regDir, lockPath, slog.Default())

	repoA := initRepoWithSkillContent(t, map[string]string{
		"agents/reviewer.md": sprintfName(packAgentMD, "reviewer"),
	})
	_, err := imp.Import(ImportOptions{Repo: repoA})
	require.NoError(t, err)

	repoB := initRepoWithSkillContent(t, map[string]string{
		"skills/alpha/SKILL.md": sprintfName(packSkillMD, "alpha"),
		"agents/reviewer.md":    "---\nname: reviewer\ndescription: Different agent\n---\n\nOther.\n",
	})
	result, err := imp.Import(ImportOptions{Repo: repoB, Selected: []string{"alpha"}, SelectedAgents: []string{"reviewer"}})
	require.NoError(t, err)
	require.Empty(t, result.ImportedAgents, "cross-source agent overwritten without --force")
	require.NotEmpty(t, result.SkippedAgents)
}
