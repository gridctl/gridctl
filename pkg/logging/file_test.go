package logging

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFileHandler_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	h, err := NewFileHandler(path, FileOpts{})
	require.NoError(t, err)
	require.NotNil(t, h)

	// Write a record and verify it appears in the file.
	logger := slog.New(h)
	logger.Info("test message", "key", "value")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "test message")
	assert.Contains(t, string(data), `"key":"value"`)
}

func TestNewFileHandler_ErrorOnMissingDirectory(t *testing.T) {
	path := "/nonexistent-dir-gridctl-test/gridctl.log"

	_, err := NewFileHandler(path, FileOpts{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestNewFileHandler_ErrorOnUnwritablePath(t *testing.T) {
	dir := t.TempDir()

	// Make directory read-only.
	require.NoError(t, os.Chmod(dir, 0555))
	t.Cleanup(func() { os.Chmod(dir, 0755) })

	path := filepath.Join(dir, "gridctl.log")
	_, err := NewFileHandler(path, FileOpts{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot open log file")
}

func TestNewFileHandler_ApppendsOnReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	// First handler — write one entry.
	h1, err := NewFileHandler(path, FileOpts{})
	require.NoError(t, err)
	slog.New(h1).Info("first entry")

	// Second handler pointing to the same file — write another entry.
	h2, err := NewFileHandler(path, FileOpts{})
	require.NoError(t, err)
	slog.New(h2).Info("second entry")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "first entry")
	assert.Contains(t, content, "second entry")
}

func TestNewMultiHandler_FansOut(t *testing.T) {
	dir := t.TempDir()
	path1 := filepath.Join(dir, "out1.log")
	path2 := filepath.Join(dir, "out2.log")

	h1, err := NewFileHandler(path1, FileOpts{})
	require.NoError(t, err)
	h2, err := NewFileHandler(path2, FileOpts{})
	require.NoError(t, err)

	multi := NewMultiHandler(h1, h2)
	logger := slog.New(multi)
	logger.Info("broadcast message")

	for _, path := range []string{path1, path2} {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(data), "broadcast message",
			"expected log entry in %s", path)
	}
}

func TestNewMultiHandler_Enabled(t *testing.T) {
	// A handler that is always disabled.
	disabled := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1000})
	// A handler that is always enabled.
	enabled := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})

	multi := NewMultiHandler(disabled, enabled)
	assert.True(t, multi.Enabled(context.Background(), slog.LevelInfo),
		"multi should be enabled when any inner handler is enabled")

	allDisabled := NewMultiHandler(disabled)
	assert.False(t, allDisabled.Enabled(context.Background(), slog.LevelInfo),
		"multi should be disabled when all inner handlers are disabled")
}

func TestNewMultiHandler_WithAttrs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	h, err := NewFileHandler(path, FileOpts{})
	require.NoError(t, err)

	multi := NewMultiHandler(h)
	withAttrs := multi.WithAttrs([]slog.Attr{slog.String("env", "test")})
	slog.New(withAttrs).Info("attr message")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"env":"test"`)
}

func TestNewMultiHandler_WithGroup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	h, err := NewFileHandler(path, FileOpts{})
	require.NoError(t, err)

	multi := NewMultiHandler(h)
	withGroup := multi.WithGroup("grp")
	slog.New(withGroup).Info("group message", "k", "v")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "grp")
}

func TestNewFileHandler_JSONFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	h, err := NewFileHandler(path, FileOpts{})
	require.NoError(t, err)
	slog.New(h).Info("json check", "num", 42)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	line := strings.TrimSpace(string(data))
	// Should be a valid JSON line.
	assert.True(t, strings.HasPrefix(line, "{"), "log line should be JSON object")
	assert.Contains(t, line, `"msg":"json check"`)
	assert.Contains(t, line, `"num":42`)
	// Timestamp key should be "ts", not "time".
	assert.Contains(t, line, `"ts":`)
	assert.NotContains(t, line, `"time":`)
}
