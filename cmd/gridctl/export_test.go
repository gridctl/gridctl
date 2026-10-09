package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestExportStackFile_Contract(t *testing.T) {
	oldFormat, oldDir := exportFormat, exportOutputDir
	t.Cleanup(func() { exportFormat, exportOutputDir = oldFormat, oldDir })
	t.Setenv("EXPORT_PROBE", "synthetic-cli-canary")
	for _, format := range []string{"yaml", "json"} {
		for _, value := range []string{"${EXPORT_PROBE}", "$EXPORT_PROBE", "prefix-${var:EXPORT_PROBE}-${vault:MISSING}", "synthetic-inline-canary", "${EXPORT_PROBE:-synthetic-default-canary}"} {
			t.Run(format+value, func(t *testing.T) {
				exportFormat, exportOutputDir = format, ""
				path := filepath.Join(t.TempDir(), "source.yaml")
				source := "name: export\ngateway:\n  auth:\n    token: " + value + "\n"
				require.NoError(t, os.WriteFile(path, []byte(source), 0600))
				var stdout, stderr bytes.Buffer
				err := exportStackFile(context.Background(), path, &stdout, &stderr)
				if value == "synthetic-inline-canary" || value == "${EXPORT_PROBE:-synthetic-default-canary}" {
					require.Error(t, err)
					require.NotContains(t, err.Error(), "canary")
					require.Empty(t, stdout.String())
				} else {
					require.NoError(t, err)
					require.Contains(t, stdout.String(), value)
					require.NotContains(t, stdout.String(), "synthetic-cli-canary")
					require.Contains(t, stderr.String(), "Review authored literals")
					var doc any
					if format == "json" {
						require.NoError(t, json.Unmarshal(stdout.Bytes(), &doc))
					} else {
						require.NoError(t, yaml.Unmarshal(stdout.Bytes(), &doc))
					}
				}
				data, err := os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, source, string(data))
			})
		}
	}
}

func TestRunExport_IsolatedState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	t.Setenv("EXPORT_PROBE", "synthetic-process-canary")
	path := filepath.Join(home, "stack.yaml")
	require.NoError(t, os.WriteFile(path, []byte("name: test\ngateway: {auth: {token: '${EXPORT_PROBE}'}}\n"), 0600))
	st := &state.DaemonState{StackName: "test", StackFile: path, PID: os.Getpid(), AuthToken: "synthetic-state-canary"}
	require.NoError(t, state.Save(st))
	statePath, err := state.StatePath("test")
	require.NoError(t, err)
	before, err := os.ReadFile(statePath)
	require.NoError(t, err)
	oldFormat, oldDir := exportFormat, exportOutputDir
	t.Cleanup(func() { exportFormat, exportOutputDir = oldFormat, oldDir })
	exportFormat, exportOutputDir = "json", ""
	var stdout, stderr bytes.Buffer
	require.NoError(t, runExportContext(context.Background(), &stdout, &stderr))
	require.NotContains(t, stdout.String(), "canary")
	require.Contains(t, stdout.String(), "${EXPORT_PROBE}")
	if os.Getenv("GRIDCTL_EXPORT_TEST_BINARY") != "" {
		binary, err := filepath.Abs("../../gridctl")
		require.NoError(t, err)
		cmd := exec.CommandContext(context.Background(), binary, "export", "--format", "json")
		stdout.Reset()
		stderr.Reset()
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		require.NoError(t, cmd.Run(), stderr.String())
		require.NotContains(t, stdout.String(), "canary")
		require.Contains(t, stdout.String(), "${EXPORT_PROBE}")
		require.True(t, json.Valid(stdout.Bytes()))
	}
	after, err := os.ReadFile(statePath)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestExportStackFile_SidecarPreflight(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	oldFormat, oldDir := exportFormat, exportOutputDir
	t.Cleanup(func() { exportFormat, exportOutputDir = oldFormat, oldDir })
	exportFormat, exportOutputDir = "yaml", filepath.Join(home, "out")
	path := filepath.Join(home, "source.yaml")
	require.NoError(t, os.WriteFile(path, []byte("name: test\n"), 0600))
	require.NoError(t, os.MkdirAll(filepath.Dir(skills.LockFilePath()), 0700))
	for _, repo := range []string{"https://synthetic-user:synthetic-sidecar-canary@example.test/repo", "https://example.test/repo", "git@example.test:repo"} {
		data, err := yaml.Marshal(skills.LockFile{Sources: map[string]skills.LockedSource{"test": {Repo: repo}}})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(skills.LockFilePath(), data, 0600))
		var stdout, stderr bytes.Buffer
		err = exportStackFile(context.Background(), path, &stdout, &stderr)
		if repo == "https://synthetic-user:synthetic-sidecar-canary@example.test/repo" {
			require.Error(t, err)
			require.NotContains(t, err.Error(), "canary")
			require.NoDirExists(t, exportOutputDir)
		} else {
			require.NoError(t, err)
			require.FileExists(t, filepath.Join(exportOutputDir, "stack.yaml"))
			require.FileExists(t, filepath.Join(exportOutputDir, "skills.yaml"))
		}
		require.Empty(t, stdout.String())
		after, err := os.ReadFile(skills.LockFilePath())
		require.NoError(t, err)
		require.Equal(t, data, after)
	}
}

func TestExportStackFile_ConfigFileNotice(t *testing.T) {
	oldFormat, oldDir := exportFormat, exportOutputDir
	t.Cleanup(func() { exportFormat, exportOutputDir = oldFormat, oldDir })
	withFile := "name: export\nmcp-servers:\n  - name: prometheus\n    image: alpine\n    transport: stdio\n    configs:\n      - target: /etc/app.yaml\n        file: ./extra.yaml\n"
	withoutFile := "name: export\nmcp-servers:\n  - name: prometheus\n    image: alpine\n    transport: stdio\n    configs:\n      - target: /etc/app.yaml\n        content: alpha\n"

	t.Run("stdout", func(t *testing.T) {
		exportFormat, exportOutputDir = "yaml", ""
		path := filepath.Join(t.TempDir(), "stack.yaml")
		require.NoError(t, os.WriteFile(path, []byte(withFile), 0600))
		var stdout, stderr bytes.Buffer
		require.NoError(t, exportStackFile(context.Background(), path, &stdout, &stderr))
		lines := strings.Split(strings.TrimRight(stderr.String(), "\n"), "\n")
		require.Len(t, lines, 2)
		require.Equal(t, config.ExportConfigFilesNotice, lines[1])
		require.NotContains(t, stdout.String(), config.ExportConfigFilesNotice)

		require.NoError(t, os.WriteFile(path, []byte(withoutFile), 0600))
		stdout.Reset()
		stderr.Reset()
		require.NoError(t, exportStackFile(context.Background(), path, &stdout, &stderr))
		require.NotContains(t, stderr.String(), config.ExportConfigFilesNotice)
	})

	t.Run("directory", func(t *testing.T) {
		dir := t.TempDir()
		exportFormat, exportOutputDir = "yaml", filepath.Join(dir, "out")
		path := filepath.Join(dir, "stack.yaml")
		require.NoError(t, os.WriteFile(path, []byte(withFile), 0600))
		var stdout, stderr bytes.Buffer
		require.NoError(t, exportStackFile(context.Background(), path, &stdout, &stderr))
		require.Contains(t, stderr.String(), config.ExportConfigFilesNotice)
		require.Empty(t, stdout.String())
	})
}

func TestWriteExportArtifacts_ProtectSourcesAndPartialFailure(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.yaml")
	require.NoError(t, os.WriteFile(source, []byte("original"), 0600))
	alias := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.Symlink(source, alias))
	var stderr bytes.Buffer
	err := writeExportArtifacts(context.Background(), dir, []string{source}, []exportArtifact{{"stack.yaml", []byte("replacement")}}, config.ExportNotice, &stderr)
	require.ErrorContains(t, err, "overwrite")
	data, err := os.ReadFile(source)
	require.NoError(t, err)
	require.Equal(t, "original", string(data))
	other := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(other, "skills.yaml"), 0700))
	err = writeExportArtifacts(context.Background(), other, []string{source}, []exportArtifact{{"stack.yaml", []byte("name: test")}, {"skills.yaml", []byte("sources: []")}}, config.ExportNotice, &stderr)
	require.ErrorContains(t, err, "already written: [stack.yaml]")
	require.FileExists(t, filepath.Join(other, "stack.yaml"))
}

func TestExportStackFile_AncestorAndMalformedPreflight(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	oldFormat, oldDir := exportFormat, exportOutputDir
	t.Cleanup(func() { exportFormat, exportOutputDir = oldFormat, oldDir })
	exportFormat, exportOutputDir = "yaml", home
	parent := filepath.Join(home, "stack.yaml")
	child := filepath.Join(home, "child.yaml")
	require.NoError(t, os.WriteFile(parent, []byte("name: parent\n"), 0600))
	require.NoError(t, os.WriteFile(child, []byte("name: child\nextends: stack.yaml\n"), 0600))
	var stdout, stderr bytes.Buffer
	err := exportStackFile(context.Background(), child, &stdout, &stderr)
	require.ErrorContains(t, err, "overwrite")
	data, err := os.ReadFile(parent)
	require.NoError(t, err)
	require.Equal(t, "name: parent\n", string(data))
	exportOutputDir = filepath.Join(home, "not-created")
	require.NoError(t, os.WriteFile(child, []byte("name: [synthetic-syntax-canary\n"), 0600))
	err = exportStackFile(context.Background(), child, &stdout, &stderr)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "canary")
	require.NoDirExists(t, exportOutputDir)
	require.Empty(t, stdout.String())
}
