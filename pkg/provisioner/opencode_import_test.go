package provisioner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDiscoverOpenCodeImport_Automatic(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	jsonPath, jsoncPath := openCodeFixturePaths(t, home)
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("OPENCODE_CONFIG", filepath.Join(t.TempDir(), "override.json"))
	dir := filepath.Dir(jsonPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xdg, "opencode.json"), []byte(`{"mcp":{"xdg":{"type":"remote","url":"https://xdg.example/mcp"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cwdFile := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(cwdFile, []byte(`{"mcp":{"cwd":{"type":"remote","url":"https://cwd.example/mcp"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("missing", func(t *testing.T) {
		sel, err := DiscoverOpenCodeImport(t.Context(), "")
		if err != nil {
			t.Fatal(err)
		}
		if sel.Path != "" || sel.JSONPath != jsonPath || sel.JSONCPath != jsoncPath {
			t.Fatalf("selection = %+v", sel)
		}
		assertPolicy(t, sel.Notes)
		if strings.Contains(strings.Join(sel.Notes, " "), xdg) || strings.Contains(sel.Path, cwdFile) {
			t.Fatalf("automatic discovery left the Gridctl home: %+v", sel)
		}
	})

	t.Run("json only", func(t *testing.T) {
		writeImportFile(t, jsonPath, `{"mcp":{"docs":{"type":"remote","url":"https://docs.example.com/mcp"}}}`)
		sel, err := DiscoverOpenCodeImport(t.Context(), "")
		if err != nil {
			t.Fatal(err)
		}
		if sel.Path != jsonPath || sel.BothPresent || sel.JSONAbsent {
			t.Fatalf("selection = %+v", sel)
		}
		assertPolicy(t, sel.Notes)
		os.Remove(jsonPath)
	})

	t.Run("jsonc only", func(t *testing.T) {
		writeImportFile(t, jsoncPath, "{\n// comment\n\"mcp\":{\"docs\":{\"type\":\"remote\",\"url\":\"https://docs.example.com/mcp\"},},\n}")
		sel, err := DiscoverOpenCodeImport(t.Context(), "")
		if err != nil {
			t.Fatal(err)
		}
		if sel.Path != jsoncPath || !sel.JSONAbsent || sel.BothPresent {
			t.Fatalf("selection = %+v", sel)
		}
		if !strings.Contains(strings.Join(sel.Notes, "\n"), jsonPath) {
			t.Fatalf("notes omitted absent JSON path: %v", sel.Notes)
		}
		assertPolicy(t, sel.Notes)
		os.Remove(jsoncPath)
	})

	t.Run("both files prefer json", func(t *testing.T) {
		writeImportFile(t, jsonPath, `{"mcp":{"fromjson":{"type":"remote","url":"https://json.example/mcp"}}}`)
		writeImportFile(t, jsoncPath, `{"mcp":{"fromjsonc":{"type":"remote","url":"https://jsonc.example/mcp"}}}`)
		sel, err := DiscoverOpenCodeImport(t.Context(), "")
		if err != nil {
			t.Fatal(err)
		}
		if sel.Path != jsonPath || !sel.BothPresent {
			t.Fatalf("selection = %+v", sel)
		}
		joined := strings.Join(sel.Notes, "\n")
		if !strings.Contains(joined, jsonPath) || !strings.Contains(joined, jsoncPath) || !strings.Contains(joined, "was not merged") {
			t.Fatalf("notes = %v", sel.Notes)
		}
		assertPolicy(t, sel.Notes)
	})
}

func TestDiscoverOpenCodeImport_ExplicitIgnoresSiblings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	_, jsoncPath := openCodeFixturePaths(t, home)
	dir := filepath.Dir(jsoncPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sibling := jsoncPath
	writeImportFile(t, sibling, `{"mcp":{"sibling":{"type":"remote","url":"https://sibling.example/mcp"}}}`)
	explicit := filepath.Join(t.TempDir(), "project.jsonc")
	sel, err := DiscoverOpenCodeImport(t.Context(), explicit)
	if err != nil {
		t.Fatal(err)
	}
	if sel.Path != explicit || !sel.Explicit || sel.BothPresent {
		t.Fatalf("selection = %+v", sel)
	}
	if strings.Contains(strings.Join(sel.Notes, " "), sibling) {
		t.Fatalf("explicit discovery mentioned a sibling: %v", sel.Notes)
	}
}

func TestDiscoverOpenCodeImport_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := DiscoverOpenCodeImport(ctx, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestReadOpenCodeImport_Statuses(t *testing.T) {
	dir := t.TempDir()
	t.Run("missing", func(t *testing.T) {
		got, err := ReadOpenCodeImport(t.Context(), filepath.Join(dir, "absent.json"))
		if err != nil || got.Status != OpenCodeImportMissing || !strings.Contains(got.Detail, "does not exist") {
			t.Fatalf("got %+v err %v", got, err)
		}
	})
	t.Run("directory", func(t *testing.T) {
		got, err := ReadOpenCodeImport(t.Context(), dir)
		if err != nil || got.Status != OpenCodeImportUnreadable || !strings.Contains(got.Detail, "directory") {
			t.Fatalf("got %+v err %v", got, err)
		}
	})
	t.Run("empty", func(t *testing.T) {
		path := filepath.Join(dir, "empty.json")
		writeImportFile(t, path, " \n")
		got, err := ReadOpenCodeImport(t.Context(), path)
		if err != nil || got.Status != OpenCodeImportEmpty {
			t.Fatalf("got %+v err %v", got, err)
		}
	})
	t.Run("empty mcp", func(t *testing.T) {
		path := filepath.Join(dir, "nomcp.json")
		writeImportFile(t, path, `{"mcp":{}}`)
		got, err := ReadOpenCodeImport(t.Context(), path)
		if err != nil || got.Status != OpenCodeImportEmpty || !strings.Contains(got.Detail, "no MCP server") {
			t.Fatalf("got %+v err %v", got, err)
		}
	})
	t.Run("malformed does not mention contents", func(t *testing.T) {
		path := filepath.Join(dir, "bad.json")
		writeImportFile(t, path, "{not-json synthetic-secret-marker")
		got, err := ReadOpenCodeImport(t.Context(), path)
		if err != nil || got.Status != OpenCodeImportMalformed {
			t.Fatalf("got %+v err %v", got, err)
		}
		if strings.Contains(got.Detail, "synthetic-secret-marker") || strings.Contains(got.Detail, "not-json") {
			t.Fatalf("detail disclosed contents: %s", got.Detail)
		}
	})
	t.Run("jsonc bom comments and trailing comma", func(t *testing.T) {
		path := filepath.Join(dir, "opencode.jsonc")
		body := "\xef\xbb\xbf{\n// comment\n\"mcp\":{\"docs\":{\"type\":\"remote\",\"url\":\"https://docs.example.com/mcp\"},},\n}"
		writeImportFile(t, path, body)
		got, err := ReadOpenCodeImport(t.Context(), path)
		if err != nil || got.Status != OpenCodeImportSelected || len(got.Entries) != 1 || got.Entries[0].Name != "docs" {
			t.Fatalf("got %+v err %v", got, err)
		}
	})
	t.Run("malformed json is not replaced by sibling", func(t *testing.T) {
		jsonPath := filepath.Join(dir, "pair.json")
		jsoncPath := filepath.Join(dir, "pair.jsonc")
		writeImportFile(t, jsonPath, "{")
		writeImportFile(t, jsoncPath, `{"mcp":{"fromjsonc":{"type":"remote","url":"https://jsonc.example/mcp"}}}`)
		got, err := ReadOpenCodeImport(t.Context(), jsonPath)
		if err != nil || got.Status != OpenCodeImportMalformed || len(got.Entries) != 0 {
			t.Fatalf("got %+v err %v", got, err)
		}
	})
}

func TestReadOpenCodeImport_Unreadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode 000 is not an unreadable fixture on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can read mode 000")
	}
	path := filepath.Join(t.TempDir(), "opencode.json")
	writeImportFile(t, path, `{"mcp":{"docs":{"type":"remote","url":"https://docs.example.com/mcp"}}}`)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	got, err := ReadOpenCodeImport(t.Context(), path)
	if err != nil || got.Status != OpenCodeImportUnreadable {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestReadOpenCodeImport_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := ReadOpenCodeImport(ctx, filepath.Join(t.TempDir(), "opencode.json"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenCode_Detect_JSONCOnlyAndBothStillReturnJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	jsonPath, jsoncPath := openCodeFixturePaths(t, home)
	if err := os.MkdirAll(filepath.Dir(jsonPath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeImportFile(t, jsoncPath, `{"mcp":{}}`)

	o := newOpenCode()
	path, found := o.Detect()
	if !found || path != jsonPath {
		t.Fatalf("JSONC-only Detect = %q found=%v, want %q", path, found, jsonPath)
	}
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Fatal("Detect must return the .json path even when that file is absent")
	}

	writeImportFile(t, jsonPath, `{"mcp":{}}`)
	path, found = o.Detect()
	if !found || path != jsonPath {
		t.Fatalf("both-files Detect = %q found=%v, want %q", path, found, jsonPath)
	}
}

func openCodeFixturePaths(t *testing.T, home string) (jsonPath, jsoncPath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		appData := filepath.Join(home, "AppData", "Roaming")
		t.Setenv("APPDATA", appData)
		jsonPath = filepath.Join(appData, "opencode", "opencode.json")
	} else {
		jsonPath = filepath.Join(home, ".config", "opencode", "opencode.json")
	}
	return jsonPath, strings.TrimSuffix(jsonPath, ".json") + ".jsonc"
}

func writeImportFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertPolicy(t *testing.T, notes []string) {
	t.Helper()
	joined := strings.Join(notes, "\n")
	for _, want := range []string{
		OpenCodeImportPolicy,
		"config.json",
		"prefers opencode.jsonc for writes",
		"compatibility-preserving single-file choice",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("notes missing %q:\n%s", want, joined)
		}
	}
}
