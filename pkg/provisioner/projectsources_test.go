package provisioner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"
)

func TestProjectSources_TableOrder(t *testing.T) {
	got := ProjectSources()
	want := []struct {
		rel  string
		key  string
		slug string
	}{
		{".mcp.json", "mcpServers", "claude-code"},
		{filepath.Join(".cursor", "mcp.json"), "mcpServers", "cursor"},
		{filepath.Join(".vscode", "mcp.json"), "servers", "vscode"},
		{filepath.Join(".roo", "mcp.json"), "mcpServers", "roo"},
		{filepath.Join(".gemini", "settings.json"), "mcpServers", "gemini"},
		{filepath.Join(".zed", "settings.json"), "context_servers", "zed"},
		{"opencode.jsonc", "mcp", "opencode"},
		{"opencode.json", "mcp", "opencode"},
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	if len(got[0].Clients) != 2 || got[0].Clients[0] != "claude-code" || got[0].Clients[1] != "vscode" {
		t.Fatalf(".mcp.json clients = %v", got[0].Clients)
	}
	for i, row := range want {
		if got[i].RelPath != row.rel || got[i].ContainerKey != row.key || got[i].Scope != "project" {
			t.Fatalf("row %d = %+v, want rel %s key %s", i, got[i], row.rel, row.key)
		}
		if got[i].Clients[0] != row.slug {
			t.Fatalf("row %d first client = %s, want %s", i, got[i].Clients[0], row.slug)
		}
	}
}

func TestDiscoverProjectSources_WalkAndStop(t *testing.T) {
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	sub := filepath.Join(repo, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := gogit.PlainInit(repo, false); err != nil {
		t.Fatal(err)
	}
	above := filepath.Join(parent, ".mcp.json")
	writeImportFile(t, above, `{"mcpServers":{"above":{"command":"above"}}}`)
	rootFile := filepath.Join(repo, ".mcp.json")
	writeImportFile(t, rootFile, `{"mcpServers":{"root":{"command":"root"}}}`)
	writeImportFile(t, filepath.Join(repo, "opencode.jsonc"), `{"mcp":{"c":{"command":"c"}}}`)
	writeImportFile(t, filepath.Join(repo, "opencode.json"), `{"mcp":{"j":{"command":"j"}}}`)
	writeImportFile(t, filepath.Join(repo, ".cursor", "mcp.json"), `{"mcpServers":{"cur":{"command":"cur"}}}`)

	t.Run("subdirectory finds root and stops", func(t *testing.T) {
		got, err := DiscoverProjectSources(t.Context(), sub, nil)
		if err != nil {
			t.Fatal(err)
		}
		var paths []string
		for _, f := range got {
			if strings.HasPrefix(f.Path, parent+string(os.PathSeparator)) && !strings.HasPrefix(f.Path, repo+string(os.PathSeparator)) && f.Path != repo {
				t.Fatalf("walk left the repo: %s", f.Path)
			}
			paths = append(paths, f.Path)
		}
		if !containsPath(paths, rootFile) || containsPath(paths, above) {
			t.Fatalf("paths = %v", paths)
		}
		jsonc, json := -1, -1
		for i, f := range got {
			if f.Path == filepath.Join(repo, "opencode.jsonc") {
				jsonc = i
			}
			if f.Path == filepath.Join(repo, "opencode.json") {
				json = i
			}
			if f.Depth != 1 {
				t.Fatalf("depth from sub = %d for %s", f.Depth, f.Path)
			}
		}
		if jsonc < 0 || json < 0 || jsonc > json {
			t.Fatalf("opencode order jsonc=%d json=%d", jsonc, json)
		}
	})

	t.Run("sibling is not inside the repo", func(t *testing.T) {
		sibling := filepath.Join(parent, "sibling")
		if err := os.MkdirAll(sibling, 0o755); err != nil {
			t.Fatal(err)
		}
		got, err := DiscoverProjectSources(t.Context(), sibling, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range got {
			if strings.HasPrefix(f.Path, repo+string(os.PathSeparator)) {
				t.Fatalf("sibling walk entered the repo: %s", f.Path)
			}
		}
	})

	t.Run("no git root scans only the start directory", func(t *testing.T) {
		loose := t.TempDir()
		nested := filepath.Join(loose, "nested")
		if err := os.MkdirAll(nested, 0o755); err != nil {
			t.Fatal(err)
		}
		parentFile := filepath.Join(loose, ".mcp.json")
		writeImportFile(t, parentFile, `{"mcpServers":{"p":{"command":"p"}}}`)
		walk, err := ProjectWalk(t.Context(), nested)
		if err != nil {
			t.Fatal(err)
		}
		got, err := DiscoverProjectSources(t.Context(), nested, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(walk) == 1 {
			if len(got) != 0 {
				t.Fatalf("start-only walk found %+v", got)
			}
		} else if !walkContains(walk, loose) {
			for _, f := range got {
				if f.Path == parentFile {
					t.Fatalf("file outside the walk was returned: %s", f.Path)
				}
			}
		}
		got, err = DiscoverProjectSources(t.Context(), loose, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Path != parentFile || got[0].Depth != 0 {
			t.Fatalf("start dir = %+v", got)
		}
	})

	t.Run("client filter intersects shared file", func(t *testing.T) {
		got, err := DiscoverProjectSources(t.Context(), repo, []string{"vscode"})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Path != rootFile {
			t.Fatalf("vscode filter = %+v", got)
		}
		if len(got[0].Clients) != 1 || got[0].Clients[0] != "vscode" {
			t.Fatalf("intersection = %v", got[0].Clients)
		}
	})
}

func TestDiscoverProjectSources_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := DiscoverProjectSources(ctx, t.TempDir(), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestReadProjectSource_Statuses(t *testing.T) {
	dir := t.TempDir()
	src := ProjectSourceFile{Dir: dir, ContainerKey: "mcpServers", Scope: "project", Clients: []string{"cursor"}}

	t.Run("missing", func(t *testing.T) {
		src.Path = filepath.Join(dir, "absent.json")
		got, err := ReadProjectSource(t.Context(), src)
		if err != nil || got.Status != OpenCodeImportMissing {
			t.Fatalf("got %+v err %v", got, err)
		}
	})
	t.Run("directory", func(t *testing.T) {
		src.Path = dir
		got, err := ReadProjectSource(t.Context(), src)
		if err != nil || got.Status != OpenCodeImportUnreadable || !strings.Contains(got.Detail, "directory") {
			t.Fatalf("got %+v err %v", got, err)
		}
	})
	t.Run("malformed hides contents", func(t *testing.T) {
		src.Path = filepath.Join(dir, "bad.json")
		writeImportFile(t, src.Path, "{not-json synthetic-secret-marker")
		got, err := ReadProjectSource(t.Context(), src)
		if err != nil || got.Status != OpenCodeImportMalformed {
			t.Fatalf("got %+v err %v", got, err)
		}
		if strings.Contains(got.Detail, "synthetic-secret-marker") || strings.Contains(got.Detail, "not-json") {
			t.Fatalf("detail disclosed contents: %s", got.Detail)
		}
	})
	t.Run("empty and selected", func(t *testing.T) {
		src.Path = filepath.Join(dir, "empty.json")
		writeImportFile(t, src.Path, " \n")
		got, err := ReadProjectSource(t.Context(), src)
		if err != nil || got.Status != OpenCodeImportEmpty {
			t.Fatalf("empty = %+v err %v", got, err)
		}
		src.Path = filepath.Join(dir, ".mcp.json")
		writeImportFile(t, src.Path, "{\n// c\n\"mcpServers\":{\"docs\":{\"command\":\"echo\"},},\n}")
		got, err = ReadProjectSource(t.Context(), src)
		if err != nil || got.Status != OpenCodeImportSelected || len(got.Entries) != 1 || got.Entries[0].Name != "docs" {
			t.Fatalf("selected = %+v err %v", got, err)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := ReadProjectSource(ctx, src)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestClaudeCodeLocalScope_PathMatch(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "Repo")
	other := filepath.Join(dir, "other")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".claude.json")
	raw, err := json.Marshal(map[string]any{
		"projects": map[string]any{
			repo:  map[string]any{"mcpServers": map[string]any{"local": map[string]any{"command": "local-bin"}}},
			other: map[string]any{"mcpServers": map[string]any{"nope": map[string]any{"command": "no"}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeImportFile(t, path, string(raw))

	got, err := ClaudeCodeLocalScope(t.Context(), path, []string{repo})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Status != OpenCodeImportSelected || got[0].Dir != filepath.Clean(repo) || len(got[0].Entries) != 1 || got[0].Entries[0].Name != "local" {
		t.Fatalf("match = %+v", got)
	}
	if got[0].Entries[0].Name == "nope" {
		t.Fatal("other directory leaked")
	}

	got, err = ClaudeCodeLocalScope(t.Context(), path, []string{filepath.Join(dir, "missing")})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("unmatched dir yielded %+v", got)
	}

	missing, err := ClaudeCodeLocalScope(t.Context(), filepath.Join(dir, "absent.json"), []string{repo})
	if err != nil || len(missing) != 1 || missing[0].Status != OpenCodeImportMissing || len(missing[0].Entries) != 0 {
		t.Fatalf("missing = %+v err %v", missing, err)
	}

	writeImportFile(t, filepath.Join(dir, "bad.json"), "{not-json secret-marker")
	bad, err := ClaudeCodeLocalScope(t.Context(), filepath.Join(dir, "bad.json"), []string{repo})
	if err != nil || len(bad) != 1 || bad[0].Status != OpenCodeImportMalformed || strings.Contains(bad[0].Detail, "secret-marker") {
		t.Fatalf("malformed = %+v err %v", bad, err)
	}
}

func TestProjectPathEqual_WindowsFold(t *testing.T) {
	if runtime.GOOS != "windows" {
		if projectPathEqual(`C:\Repo`, `c:\repo`) {
			t.Fatal("non-Windows comparison folded case")
		}
		return
	}
	if !projectPathEqual(`C:\Repo`, `c:\repo`) {
		t.Fatal("Windows comparison did not fold case")
	}
}

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}

func walkContains(walk []string, dir string) bool {
	dir = filepath.Clean(dir)
	for _, d := range walk {
		if filepath.Clean(d) == dir {
			return true
		}
	}
	return false
}
