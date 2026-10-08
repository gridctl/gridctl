package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientLocationsAndKnownRoots(t *testing.T) {
	rows, ok := ClientLocations("claude-code")
	require.True(t, ok)
	require.Len(t, rows, 2)
	assert.Equal(t, "claude-code", rows[0].SourceName)
	assert.Equal(t, ResourceKindSkill, rows[0].Kind)
	assert.Equal(t, "claude-code-agents", rows[1].SourceName)
	assert.Equal(t, ResourceKindAgent, rows[1].Kind)

	_, ok = ClientLocations("missing")
	assert.False(t, ok)
	assert.Equal(t, []string{"agents", "claude-code", "opencode"}, SupportedImportClients())

	home := t.TempDir()
	assert.NotEmpty(t, KnownSkillRoots(home))
	assert.NotEmpty(t, KnownAgentRoots(home))

	claude := filepath.Join(home, ".claude", "skills")
	require.NoError(t, os.MkdirAll(claude, 0o755))
	assert.Equal(t, "claude-code", KnownSourceName(home, claude))
	child := filepath.Join(claude, "pcap-analysis")
	require.NoError(t, os.MkdirAll(child, 0o755))
	assert.Equal(t, "", KnownSourceName(home, child))
	assert.Equal(t, "claude-code-agents", KnownSourceName(home, filepath.Join(home, ".claude", "agents")))
}
