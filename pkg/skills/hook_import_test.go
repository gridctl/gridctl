package skills

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportHooks_SkipsFindingsWithoutTrust(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	reg := t.TempDir()
	src := t.TempDir()
	writeHookScript(t, src, "scripts/run.sh", "#!/bin/sh\nchmod 777 /tmp/x\n", 0o755)
	raw := []byte(`{"name":"block","description":"d","dialect":"claude-code","hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"./scripts/run.sh"}]}]}}`)
	require.NoError(t, os.WriteFile(filepath.Join(src, "hook.json"), raw, 0o644))
	def, err := ParseHookJSON(raw, src)
	require.NoError(t, err)

	imp := NewImporter(registry.NewStore(reg), reg, filepath.Join(home, "skills.lock.yaml"), slog.New(slog.DiscardHandler))
	result := &ImportResult{}
	clone := &CloneResult{Hooks: []DiscoveredHook{{
		Name: "block", Dir: src, Definition: def, Path: "hooks/block",
	}}}
	err = imp.importHooks(t.Context(), clone, ImportOptions{
		ImportHooks:   true,
		SelectedHooks: []string{"block"},
		Repo:          "https://example.com/pack",
	}, result)
	require.NoError(t, err)
	require.Len(t, result.SkippedHooks, 1)
	assert.Contains(t, result.SkippedHooks[0].Reason, "security findings")
	_, err = os.Stat(HookDir(reg, "block"))
	assert.True(t, os.IsNotExist(err))
}

func TestDiscoverHooks_NameMismatchAndSymlink(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "hooks", "alpha")
	writeHookScript(t, dir, "scripts/run.sh", "#!/bin/sh\nexit 0\n", 0o755)
	body := `{"name":"beta","description":"d","dialect":"claude-code","hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"./scripts/run.sh"}]}]}}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hook.json"), []byte(body), 0o644))

	hooks, malformed, err := discoverHooks(root, root)
	require.NoError(t, err)
	assert.Empty(t, hooks)
	require.NotEmpty(t, malformed)
	assert.Contains(t, malformed[0].Err, "does not match directory")

	linkDir := filepath.Join(root, "hooks", "gamma")
	require.NoError(t, os.MkdirAll(linkDir, 0o755))
	require.NoError(t, os.Symlink(filepath.Join(dir, "hook.json"), filepath.Join(linkDir, "hook.json")))
	_, malformed, err = discoverHooks(root, root)
	require.NoError(t, err)
	found := false
	for _, m := range malformed {
		if strings.Contains(m.Err, "regular file") {
			found = true
		}
	}
	assert.True(t, found, "malformed = %+v", malformed)
}
