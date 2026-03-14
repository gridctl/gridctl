package skills

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestShouldCheckUpdates(t *testing.T) {
	origNoCheck := os.Getenv("GRIDCTL_NO_SKILL_UPDATE_CHECK")
	origCI := os.Getenv("CI")
	defer func() {
		os.Setenv("GRIDCTL_NO_SKILL_UPDATE_CHECK", origNoCheck)
		os.Setenv("CI", origCI)
	}()

	// Default: should check
	os.Unsetenv("GRIDCTL_NO_SKILL_UPDATE_CHECK")
	os.Unsetenv("CI")
	assert.True(t, ShouldCheckUpdates())

	// Disabled via env var
	os.Setenv("GRIDCTL_NO_SKILL_UPDATE_CHECK", "1")
	assert.False(t, ShouldCheckUpdates())

	// Disabled in CI
	os.Unsetenv("GRIDCTL_NO_SKILL_UPDATE_CHECK")
	os.Setenv("CI", "true")
	assert.False(t, ShouldCheckUpdates())
}

func TestFormatUpdateNotice(t *testing.T) {
	// When no cache exists, returns empty
	result := FormatUpdateNotice()
	// Either empty or a notice — just ensure no panic
	_ = result
}

func TestUpdateStatus_WriteAndRead(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "skill-updates.yaml")

	status := &UpdateStatus{
		CheckedAt: time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC),
		Updates: map[string]SkillUpdate{
			"test-skill": {
				CurrentSHA: "abc123",
				LatestSHA:  "def456",
				Repo:       "https://github.com/example/skills",
				Ref:        "main",
			},
		},
	}

	// Write
	data, err := yaml.Marshal(status)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(cachePath), 0755))
	require.NoError(t, os.WriteFile(cachePath, data, 0644))

	// Read back
	readData, err := os.ReadFile(cachePath)
	require.NoError(t, err)

	var readStatus UpdateStatus
	require.NoError(t, yaml.Unmarshal(readData, &readStatus))
	assert.Equal(t, 1, len(readStatus.Updates))
	assert.Equal(t, "def456", readStatus.Updates["test-skill"].LatestSHA)
}

func TestUpdateStatus_EmptyUpdates(t *testing.T) {
	status := &UpdateStatus{
		CheckedAt: time.Now().UTC(),
		Updates:   map[string]SkillUpdate{},
	}
	data, err := yaml.Marshal(status)
	require.NoError(t, err)

	var read UpdateStatus
	require.NoError(t, yaml.Unmarshal(data, &read))
	assert.Empty(t, read.Updates)
}

func TestUpdateStatus_WithErrors(t *testing.T) {
	status := &UpdateStatus{
		CheckedAt: time.Now().UTC(),
		Updates:   map[string]SkillUpdate{},
		Errors:    []string{"repo-a: network timeout", "repo-b: auth failed"},
	}
	data, err := yaml.Marshal(status)
	require.NoError(t, err)

	var read UpdateStatus
	require.NoError(t, yaml.Unmarshal(data, &read))
	assert.Len(t, read.Errors, 2)
	assert.Contains(t, read.Errors[0], "network timeout")
}

func TestUpdateCachePath(t *testing.T) {
	p := UpdateCachePath()
	assert.Contains(t, p, ".gridctl")
	assert.Contains(t, p, "skill-updates.yaml")
}

func TestCheckUpdatesBackground_DisabledInCI(t *testing.T) {
	origCI := os.Getenv("CI")
	defer os.Setenv("CI", origCI)
	os.Setenv("CI", "true")

	// Should return immediately without starting goroutine
	// No panic = pass
	CheckUpdatesBackground("/nonexistent", nil)
}

func TestCheckUpdatesBackground_DisabledByEnv(t *testing.T) {
	orig := os.Getenv("GRIDCTL_NO_SKILL_UPDATE_CHECK")
	defer os.Setenv("GRIDCTL_NO_SKILL_UPDATE_CHECK", orig)
	os.Setenv("GRIDCTL_NO_SKILL_UPDATE_CHECK", "1")

	// Should return immediately
	CheckUpdatesBackground("/nonexistent", nil)
}
