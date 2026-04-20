package api

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const exampleStack = `version: "1"
name: example
network:
  name: example-net
  driver: bridge
mcp-servers:
  - name: github
    url: https://api.github.com/mcp
    transport: http
    env:
      GITHUB_TOKEN: "${vault:GITHUB_TOKEN}" # do not expand on write
  - name: fs  # filesystem MCP server
    image: gridctl/fs:latest
    port: 3100
    tools:
      - read_file
      - write_file
`

func TestPatchServerTools_ReplacesOnlyTargetTools(t *testing.T) {
	out, err := patchServerTools([]byte(exampleStack), "fs", []string{"read_file"})
	require.NoError(t, err)

	s := string(out)
	// Target server's tools shrank to one entry.
	assert.Contains(t, s, "tools:\n      - read_file\n")
	assert.NotContains(t, s, "- write_file")

	// Untouched fields on the other server remain intact, including the
	// vault reference (must never be expanded) and the line comment.
	assert.Contains(t, s, "GITHUB_TOKEN: \"${vault:GITHUB_TOKEN}\"")
	assert.Contains(t, s, "do not expand on write")
	assert.Contains(t, s, "filesystem MCP server")
}

func TestPatchServerTools_PreservesInlineCommentOnOtherServer(t *testing.T) {
	out, err := patchServerTools([]byte(exampleStack), "fs", []string{"read_file", "stat_file"})
	require.NoError(t, err)

	s := string(out)
	// The unrelated server still carries its inline + line comments.
	assert.Contains(t, s, "filesystem MCP server")
	assert.Contains(t, s, "do not expand on write")
	// Target tools updated.
	assert.Contains(t, s, "- read_file")
	assert.Contains(t, s, "- stat_file")
}

func TestPatchServerTools_InsertsToolsWhenMissing(t *testing.T) {
	out, err := patchServerTools([]byte(exampleStack), "github", []string{"list_issues"})
	require.NoError(t, err)

	s := string(out)
	// tools: was not originally present on "github"; should now appear once.
	assert.Equal(t, 2, strings.Count(s, "tools:"), "each server should have exactly one tools: block")
	// Whitelist is attached to the github block, above fs.
	gh := strings.Index(s, "- name: github")
	fs := strings.Index(s, "- name: fs")
	tools := strings.Index(s, "- list_issues")
	assert.Greater(t, tools, gh)
	assert.Less(t, tools, fs)
}

func TestPatchServerTools_EmptyListRemovesField(t *testing.T) {
	out, err := patchServerTools([]byte(exampleStack), "fs", nil)
	require.NoError(t, err)

	s := string(out)
	// Only the github-appended tools: would remain if we ran that first, so
	// starting from the base we expect no tools: at all.
	assert.NotContains(t, s, "tools:")
	// The server entry is otherwise intact.
	assert.Contains(t, s, "- name: fs")
	assert.Contains(t, s, "image: gridctl/fs:latest")
}

func TestPatchServerTools_UnknownServerReturnsSentinel(t *testing.T) {
	_, err := patchServerTools([]byte(exampleStack), "nope", []string{"x"})
	assert.ErrorIs(t, err, errServerNotFound)
}

func TestSetServerTools_AtomicAndPersisted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.WriteFile(path, []byte(exampleStack), 0o600))

	require.NoError(t, setServerTools(path, "fs", []string{"read_file"}))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "- read_file")
	assert.NotContains(t, string(data), "- write_file")

	// No stray tmp files left behind by a successful write.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.Contains(e.Name(), ".tmp."), "leftover temp file: %s", e.Name())
	}
}

func TestSetServerTools_ConflictWhenDiskChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.WriteFile(path, []byte(exampleStack), 0o600))

	// Simulate an external editor writing to the file between the initial
	// read and the pre-write verification read.
	swapBetweenReadsHook(func() {
		_ = os.WriteFile(path, []byte(exampleStack+"\n# touched externally\n"), 0o600)
	})
	defer swapBetweenReadsHook(nil)

	err := setServerTools(path, "fs", []string{"read_file"})
	assert.ErrorIs(t, err, errStackModified)

	// Caller's intended write must not have landed.
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "touched externally")
	assert.Contains(t, string(data), "- write_file", "original fs tools should still be present")
}

func TestSetServerTools_EmptyPath(t *testing.T) {
	err := setServerTools("", "fs", []string{"x"})
	assert.ErrorIs(t, err, errStackFileEmpty)
}

func TestSetServerTools_UnknownServer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.WriteFile(path, []byte(exampleStack), 0o600))

	err := setServerTools(path, "missing", []string{"x"})
	assert.True(t, errors.Is(err, errServerNotFound))
}

func TestAtomicWrite_LeavesOriginalOnWriteFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	original := []byte("original\n")
	require.NoError(t, os.WriteFile(path, original, 0o600))

	// atomicWrite with a directory path that doesn't exist should fail before
	// touching the original file.
	err := atomicWrite(filepath.Join(dir, "nonexistent-subdir", "stack.yaml"), []byte("new"))
	assert.Error(t, err)

	// Original is untouched.
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, original, data)
}
