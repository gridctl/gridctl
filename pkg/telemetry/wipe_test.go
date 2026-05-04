package telemetry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupWipeFixture(t *testing.T, stack string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".gridctl", "telemetry", stack)

	for _, server := range []string{"github", "filesystem"} {
		srvDir := filepath.Join(root, server)
		require.NoError(t, os.MkdirAll(srvDir, 0o700))
		for _, sig := range []string{"logs", "metrics", "traces"} {
			require.NoError(t, os.WriteFile(filepath.Join(srvDir, sig+".jsonl"), []byte("{}\n"), 0o600))
		}
		// One rotated sibling for logs.
		require.NoError(t, os.WriteFile(filepath.Join(srvDir, "logs-2026-04-30T12-00-00.000.jsonl"), []byte("{}\n"), 0o600))
	}
	return root
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	assert.NoError(t, err, "expected to exist: %s", path)
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "expected to be removed: %s (err=%v)", path, err)
}

func TestWipe_SingleSignalForServer(t *testing.T) {
	root := setupWipeFixture(t, "demo")

	require.NoError(t, Wipe("demo", "github", "logs"))

	mustNotExist(t, filepath.Join(root, "github", "logs.jsonl"))
	mustNotExist(t, filepath.Join(root, "github", "logs-2026-04-30T12-00-00.000.jsonl"))
	// Other signals for github survived.
	mustExist(t, filepath.Join(root, "github", "metrics.jsonl"))
	mustExist(t, filepath.Join(root, "github", "traces.jsonl"))
	// Other server is untouched.
	mustExist(t, filepath.Join(root, "filesystem", "logs.jsonl"))
}

func TestWipe_AllSignalsForServer(t *testing.T) {
	root := setupWipeFixture(t, "demo")

	require.NoError(t, Wipe("demo", "github", ""))

	for _, sig := range []string{"logs", "metrics", "traces"} {
		mustNotExist(t, filepath.Join(root, "github", sig+".jsonl"))
	}
	mustNotExist(t, filepath.Join(root, "github", "logs-2026-04-30T12-00-00.000.jsonl"))
	// Filesystem untouched.
	mustExist(t, filepath.Join(root, "filesystem", "metrics.jsonl"))
}

func TestWipe_AllServersAllSignals(t *testing.T) {
	root := setupWipeFixture(t, "demo")

	require.NoError(t, Wipe("demo", "", ""))

	for _, server := range []string{"github", "filesystem"} {
		for _, sig := range []string{"logs", "metrics", "traces"} {
			mustNotExist(t, filepath.Join(root, server, sig+".jsonl"))
		}
	}
}

func TestWipe_SignalAcrossAllServers(t *testing.T) {
	root := setupWipeFixture(t, "demo")

	require.NoError(t, Wipe("demo", "", "metrics"))

	mustNotExist(t, filepath.Join(root, "github", "metrics.jsonl"))
	mustNotExist(t, filepath.Join(root, "filesystem", "metrics.jsonl"))
	// Other signals survived.
	mustExist(t, filepath.Join(root, "github", "logs.jsonl"))
	mustExist(t, filepath.Join(root, "filesystem", "traces.jsonl"))
}

func TestWipe_MissingStackIsNoOp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	assert.NoError(t, Wipe("never-existed", "", ""))
}

func TestWipe_RequiresStackName(t *testing.T) {
	err := Wipe("", "anything", "logs")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stack name is required")
}

func TestWipe_LeavesNonTelemetryFilesAlone(t *testing.T) {
	root := setupWipeFixture(t, "demo")
	stray := filepath.Join(root, "github", "stray.txt")
	require.NoError(t, os.WriteFile(stray, []byte("keep me"), 0o600))

	require.NoError(t, Wipe("demo", "github", ""))

	mustNotExist(t, filepath.Join(root, "github", "logs.jsonl"))
	mustExist(t, stray)
}
