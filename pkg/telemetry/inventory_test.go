package telemetry

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// telemetryDirForTest returns the per-stack telemetry root inside a temp HOME
// so callers can stage files matching the daemon's on-disk layout.
func telemetryDirForTest(t *testing.T, stack string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	d := filepath.Join(home, ".gridctl", "telemetry", stack)
	require.NoError(t, os.MkdirAll(d, 0o700))
	return d
}

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, make([]byte, size), 0o600))
}

func TestMatchSignal(t *testing.T) {
	cases := map[string]string{
		"logs.jsonl":                               "logs",
		"metrics.jsonl":                            "metrics",
		"traces.jsonl":                             "traces",
		"logs-2026-04-30T12-00-00.000.jsonl":       "logs",
		"metrics-2026-04-30T12-00-00.000.jsonl.gz": "metrics",
		"traces-2026-04-30T12-00-00.000.jsonl":     "traces",
		"random.txt":                               "",
		"jsonl":                                    "",
		"logs.jsonlx":                              "",
		"loggish.jsonl":                            "",
		// User-created files that look like signals but lack the lumberjack
		// timestamp must NOT match — Wipe would otherwise delete them.
		"logs-backup.jsonl":     "",
		"logs-foo.jsonl":        "",
		"logs-2026-bad.jsonl":   "",
		"logs-2026-04-30.jsonl": "",
	}
	for name, want := range cases {
		assert.Equal(t, want, matchSignal(name), "match for %q", name)
	}
}

func TestInventory_EmptyStack(t *testing.T) {
	telemetryDirForTest(t, "demo") // creates the dir but no files
	records, err := Inventory("demo", "")
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestInventory_MissingStackDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	records, err := Inventory("never-existed", "")
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestInventory_NoStackName(t *testing.T) {
	records, err := Inventory("", "")
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestInventory_AggregatesRotatedFiles(t *testing.T) {
	stackDir := telemetryDirForTest(t, "demo")
	srvDir := filepath.Join(stackDir, "github")

	writeFile(t, filepath.Join(srvDir, "logs.jsonl"), 100)
	writeFile(t, filepath.Join(srvDir, "logs-2026-04-30T12-00-00.000.jsonl"), 200)
	writeFile(t, filepath.Join(srvDir, "logs-2026-04-29T12-00-00.000.jsonl.gz"), 50)
	writeFile(t, filepath.Join(srvDir, "metrics.jsonl"), 25)
	writeFile(t, filepath.Join(srvDir, "stray.txt"), 999) // ignored

	// Make rotated logs older than the active file so OldestTime picks them up.
	old := time.Now().Add(-48 * time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(srvDir, "logs-2026-04-29T12-00-00.000.jsonl.gz"), old, old))

	records, err := Inventory("demo", "")
	require.NoError(t, err)
	require.Len(t, records, 2)

	// Ordering: logs before metrics (canonical signal order).
	assert.Equal(t, "logs", records[0].Signal)
	assert.Equal(t, "metrics", records[1].Signal)

	logs := records[0]
	assert.Equal(t, "github", logs.Server)
	assert.Equal(t, int64(350), logs.SizeBytes)
	assert.Equal(t, 3, logs.FileCount)
	assert.True(t, logs.OldestTime.Before(logs.NewestTime) || logs.OldestTime.Equal(logs.NewestTime))
	assert.Equal(t, filepath.Join(srvDir, "logs.jsonl"), logs.Path)

	metrics := records[1]
	assert.Equal(t, int64(25), metrics.SizeBytes)
	assert.Equal(t, 1, metrics.FileCount)
}

func TestInventory_FiltersByServer(t *testing.T) {
	stackDir := telemetryDirForTest(t, "demo")
	writeFile(t, filepath.Join(stackDir, "github", "logs.jsonl"), 10)
	writeFile(t, filepath.Join(stackDir, "filesystem", "logs.jsonl"), 20)

	records, err := Inventory("demo", "filesystem")
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "filesystem", records[0].Server)
	assert.Equal(t, int64(20), records[0].SizeBytes)
}

func TestInventory_StableServerOrdering(t *testing.T) {
	stackDir := telemetryDirForTest(t, "demo")
	writeFile(t, filepath.Join(stackDir, "zeta", "logs.jsonl"), 1)
	writeFile(t, filepath.Join(stackDir, "alpha", "logs.jsonl"), 1)
	writeFile(t, filepath.Join(stackDir, "mid", "logs.jsonl"), 1)

	records, err := Inventory("demo", "")
	require.NoError(t, err)
	require.Len(t, records, 3)
	assert.Equal(t, "alpha", records[0].Server)
	assert.Equal(t, "mid", records[1].Server)
	assert.Equal(t, "zeta", records[2].Server)
}

func TestIsValidSignal(t *testing.T) {
	for _, s := range []string{"logs", "metrics", "traces"} {
		assert.True(t, IsValidSignal(s), s)
	}
	for _, s := range []string{"", "log", "Logs", "all", "garbage"} {
		assert.False(t, IsValidSignal(s), s)
	}
}
