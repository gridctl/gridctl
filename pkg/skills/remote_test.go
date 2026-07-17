package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeSkillFile(t *testing.T, root, dir, content string) {
	t.Helper()
	skillDir := filepath.Join(root, dir)
	require.NoError(t, os.MkdirAll(skillDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644))
}

// TestDiscoverSkillsReportsMalformed verifies that unparseable SKILL.md
// files are reported instead of silently dropped. Regression test for the
// bug where a repo full of parse failures surfaced as "no SKILL.md files
// found in repository".
func TestDiscoverSkillsReportsMalformed(t *testing.T) {
	root := t.TempDir()

	writeSkillFile(t, root, "skills/good", `---
name: good-skill
description: A valid skill
---

Body.
`)
	writeSkillFile(t, root, "skills/broken", `---
name: [unclosed
description: broken yaml
---

Body.
`)

	discovered, malformed, err := discoverSkills(root, root)
	require.NoError(t, err)

	require.Len(t, discovered, 1)
	assert.Equal(t, "good-skill", discovered[0].Name)

	require.Len(t, malformed, 1)
	assert.Equal(t, filepath.Join("skills", "broken", "SKILL.md"), malformed[0].Path)
	assert.Contains(t, malformed[0].Err, "parsing frontmatter")
}

// TestDiscoverSkillsNestedMetadata verifies discovery tolerates the
// openclaw/ClawHub convention of nesting objects under metadata.
func TestDiscoverSkillsNestedMetadata(t *testing.T) {
	root := t.TempDir()

	writeSkillFile(t, root, "skills/openclaw-style", `---
name: openclaw-style
description: Skill with nested metadata
metadata:
  author: samber
  openclaw:
    emoji: "X"
    requires:
      bins:
        - go
---

Body.
`)

	discovered, malformed, err := discoverSkills(root, root)
	require.NoError(t, err)
	assert.Empty(t, malformed)
	require.Len(t, discovered, 1)
	assert.Equal(t, "openclaw-style", discovered[0].Name)
	assert.Equal(t, "samber", discovered[0].Skill.Metadata["author"])
	assert.NotEmpty(t, discovered[0].Skill.Metadata["openclaw"])
}
