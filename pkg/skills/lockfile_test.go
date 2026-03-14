package skills

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLockFileReadWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skills.lock.yaml")

	lf := &LockFile{
		Sources: map[string]LockedSource{
			"my-skills": {
				Repo:      "https://github.com/org/skills",
				Ref:       "main",
				CommitSHA: "abc123",
				FetchedAt: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
				Skills: map[string]LockedSkill{
					"deploy": {Path: "skills/deploy", ContentHash: "hash1"},
					"lint":   {Path: "skills/lint", ContentHash: "hash2"},
				},
			},
		},
	}

	require.NoError(t, WriteLockFile(path, lf))
	assert.FileExists(t, path)

	got, err := ReadLockFile(path)
	require.NoError(t, err)
	assert.Len(t, got.Sources, 1)

	src := got.Sources["my-skills"]
	assert.Equal(t, "https://github.com/org/skills", src.Repo)
	assert.Equal(t, "abc123", src.CommitSHA)
	assert.Len(t, src.Skills, 2)
	assert.Equal(t, "hash1", src.Skills["deploy"].ContentHash)
}

func TestLockFileReadNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.yaml")

	lf, err := ReadLockFile(path)
	require.NoError(t, err)
	assert.NotNil(t, lf)
	assert.Empty(t, lf.Sources)
}

func TestLockFileSetRemoveSource(t *testing.T) {
	lf := &LockFile{Sources: make(map[string]LockedSource)}

	lf.SetSource("test", LockedSource{
		Repo:      "https://github.com/org/test",
		CommitSHA: "abc",
	})
	assert.Len(t, lf.Sources, 1)

	lf.RemoveSource("test")
	assert.Empty(t, lf.Sources)
}

func TestLockFileRemoveSkill(t *testing.T) {
	lf := &LockFile{
		Sources: map[string]LockedSource{
			"src": {
				Skills: map[string]LockedSkill{
					"skill-a": {ContentHash: "a"},
					"skill-b": {ContentHash: "b"},
				},
			},
		},
	}

	// Remove one skill
	lf.RemoveSkill("skill-a")
	assert.Len(t, lf.Sources["src"].Skills, 1)

	// Remove last skill should remove source
	lf.RemoveSkill("skill-b")
	assert.Empty(t, lf.Sources)
}

func TestLockFileFindSkillSource(t *testing.T) {
	lf := &LockFile{
		Sources: map[string]LockedSource{
			"src-a": {
				Skills: map[string]LockedSkill{
					"skill-1": {},
				},
			},
			"src-b": {
				Skills: map[string]LockedSkill{
					"skill-2": {},
				},
			},
		},
	}

	name, src, found := lf.FindSkillSource("skill-2")
	assert.True(t, found)
	assert.Equal(t, "src-b", name)
	assert.NotNil(t, src)

	_, _, found = lf.FindSkillSource("nonexistent")
	assert.False(t, found)
}

func TestLockFileInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	require.NoError(t, os.WriteFile(path, []byte("not: [valid: yaml"), 0644))

	_, err := ReadLockFile(path)
	assert.Error(t, err)
}

func TestLockFilePath(t *testing.T) {
	p := LockFilePath()
	assert.Contains(t, p, ".gridctl")
	assert.Contains(t, p, "skills.lock.yaml")
}

func TestLockFileWithFingerprint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skills.lock.yaml")

	fp := &Fingerprint{
		ContentHash: "abc123",
		ToolsHash:   "def456",
		Tools:       []string{"tool-a", "tool-b"},
		WorkflowLen: 3,
	}

	lf := &LockFile{
		Sources: map[string]LockedSource{
			"test-src": {
				Repo:      "https://github.com/org/skills",
				Ref:       "main",
				CommitSHA: "sha1",
				FetchedAt: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				Skills: map[string]LockedSkill{
					"my-skill": {
						Path:        "skills/my-skill",
						ContentHash: "hash1",
						Fingerprint: fp,
					},
				},
			},
		},
	}

	require.NoError(t, WriteLockFile(path, lf))

	got, err := ReadLockFile(path)
	require.NoError(t, err)

	skill := got.Sources["test-src"].Skills["my-skill"]
	require.NotNil(t, skill.Fingerprint)
	assert.Equal(t, "abc123", skill.Fingerprint.ContentHash)
	assert.Equal(t, "def456", skill.Fingerprint.ToolsHash)
	assert.Equal(t, []string{"tool-a", "tool-b"}, skill.Fingerprint.Tools)
	assert.Equal(t, 3, skill.Fingerprint.WorkflowLen)
}

func TestLockFileSetSourceInitializesMap(t *testing.T) {
	lf := &LockFile{}
	lf.SetSource("new", LockedSource{Repo: "https://example.com/repo"})
	assert.Len(t, lf.Sources, 1)
	assert.Equal(t, "https://example.com/repo", lf.Sources["new"].Repo)
}
