package builder

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParsePythonProject_PyProject(t *testing.T) {
	dir := t.TempDir()
	content := `[project]
name = "demo-server"
version = "1.2.3"
requires-python = ">=3.11,<3.13"
[project.scripts]
zebra = "demo:zebra"
alpha = "demo:main"
`
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "uv.lock"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	metadata, err := ParsePythonProject(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Name != "demo-server" || metadata.Version != "1.2.3" || !metadata.HasUVLock || metadata.SourceFile != "pyproject.toml" {
		t.Fatalf("metadata = %+v", metadata)
	}
	if !reflect.DeepEqual(metadata.ConsoleScripts, []string{"alpha", "zebra"}) {
		t.Fatalf("scripts = %v", metadata.ConsoleScripts)
	}
}

func TestParsePythonProject_SetupPyDoesNotExecute(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "executed")
	content := "import pathlib\npathlib.Path(" + strconvQuote(marker) + ").write_text('bad')\nsetup(name='demo', version='1.0', python_requires='>=3.10', entry_points={'console_scripts': ['demo = demo:main']})\n"
	if err := os.WriteFile(filepath.Join(dir, "setup.py"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	metadata, err := ParsePythonProject(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Name != "demo" || !reflect.DeepEqual(metadata.ConsoleScripts, []string{"demo"}) {
		t.Fatalf("metadata = %+v", metadata)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("setup.py was executed: %v", err)
	}
}

func TestParsePythonProject_MissingErrorContract(t *testing.T) {
	_, err := ParsePythonProject(context.Background(), t.TempDir())
	if err == nil || !strings.HasPrefix(err.Error(), "No Dockerfile or Python project metadata was found in") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveConsoleCommand(t *testing.T) {
	got, err := ResolveConsoleCommand(nil, "demo-server", []string{"other", "demo_server"})
	if err != nil || !reflect.DeepEqual(got, []string{"demo_server"}) {
		t.Fatalf("command = %v, error = %v", got, err)
	}
	_, err = ResolveConsoleCommand(nil, "demo", []string{"zebra", "alpha"})
	if err == nil || err.Error() != "This package provides commands: alpha, zebra. Set the server command to one of them." {
		t.Fatalf("error = %v", err)
	}
}

func strconvQuote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}
