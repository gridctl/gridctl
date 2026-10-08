package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/internal/importer"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/gridctl/gridctl/pkg/vault"
)

// fakeImportClient implements provisioner.ClientProvisioner over an in-memory
// entry list, so scan tests never touch real client configs.
type fakeImportClient struct {
	slug    string
	entries []provisioner.ServerEntry
	listErr error
}

func (f *fakeImportClient) Name() string                               { return f.slug }
func (f *fakeImportClient) Slug() string                               { return f.slug }
func (f *fakeImportClient) Detect() (string, bool)                     { return "/fake/" + f.slug, true }
func (f *fakeImportClient) NeedsBridge() bool                          { return false }
func (f *fakeImportClient) IsLinked(string, string) (bool, error)      { return false, nil }
func (f *fakeImportClient) Link(string, provisioner.LinkOptions) error { return nil }
func (f *fakeImportClient) Unlink(string, string) error                { return nil }
func (f *fakeImportClient) ListServers(string) ([]provisioner.ServerEntry, error) {
	return f.entries, f.listErr
}

func fakeSource(c *fakeImportClient, scope string) importSource {
	return importSource{
		Slugs:   []string{c.slug},
		Path:    "/fake/" + c.slug,
		Scope:   scope,
		Entries: c.entries,
		ListErr: c.listErr,
	}
}

const testStackYAML = `# my stack
name: teststack
network:
  name: teststack-net # keep this comment
mcp-servers:
  - name: existing
    image: alpine
    port: 3000
`

func TestScanForCandidates_FiltersDedupesAndWarns(t *testing.T) {
	github := map[string]any{"command": "npx", "args": []any{"-y", "server-github"}}
	scope := []importSource{
		fakeSource(&fakeImportClient{slug: "claude", entries: []provisioner.ServerEntry{
			{Name: "github", Raw: github},
			{Name: "gridctl", Raw: map[string]any{"url": "http://localhost:8180/sse"}},
		}}, "user"),
		fakeSource(&fakeImportClient{slug: "cursor", entries: []provisioner.ServerEntry{
			{Name: "github", Raw: github},
			{Name: "sockets", Raw: map[string]any{"type": "websocket", "url": "wss://x"}},
		}}, "user"),
		fakeSource(&fakeImportClient{slug: "broken", listErr: os.ErrPermission}, "user"),
	}

	importable, skipped := scanForCandidates(output.New(), scope)

	if len(importable) != 1 || importable[0].Name != "github" {
		t.Fatalf("importable = %+v, want one deduped github", importable)
	}
	if got := importable[0].FoundIn; len(got) != 2 || got[0] != "claude" || got[1] != "cursor" {
		t.Errorf("provenance = %v", got)
	}
	if importable[0].Source != "claude" || importable[0].SourcePath != "/fake/claude" {
		t.Errorf("source path = %q from %q", importable[0].SourcePath, importable[0].Source)
	}

	reasons := map[string]string{}
	for _, s := range skipped {
		reasons[s.Name] = s.SkipReason
	}
	if reasons["gridctl"] != importer.SkipGatewaySelfEntry {
		t.Errorf("gateway entry skip = %q", reasons["gridctl"])
	}
	if reasons["sockets"] != importer.SkipUnsupported {
		t.Errorf("websocket skip = %q", reasons["sockets"])
	}
}

func TestWriteImportedServers_PreservesCommentsAndBacksUp(t *testing.T) {
	dir := t.TempDir()
	stackPath := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(stackPath, []byte(testStackYAML), 0644); err != nil {
		t.Fatal(err)
	}

	server, _, err := importer.MapEntry("claude", provisioner.ServerEntry{
		Name: "github",
		Raw:  map[string]any{"command": "npx", "args": []any{"-y", "server-github"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	backup, err := writeImportedServers(stackPath, []importer.Candidate{{Name: "github", Server: server}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if backup == "" {
		t.Error("expected a backup path")
	}
	if _, err := os.Stat(backup); err != nil {
		t.Errorf("backup file missing: %v", err)
	}

	updated, err := os.ReadFile(stackPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(updated)
	for _, want := range []string{"# my stack", "# keep this comment", "name: github", "transport: stdio"} {
		if !strings.Contains(text, want) {
			t.Errorf("updated stack missing %q:\n%s", want, text)
		}
	}
}

func TestWriteImportedServers_ValidationFailureWritesNothing(t *testing.T) {
	dir := t.TempDir()
	stackPath := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(stackPath, []byte(testStackYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Duplicate of the existing server name: post-append validation rejects.
	server, _, err := importer.MapEntry("claude", provisioner.ServerEntry{
		Name: "existing",
		Raw:  map[string]any{"command": "echo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeImportedServers(stackPath, []importer.Candidate{{Name: "existing", Server: server}}, nil); err == nil {
		t.Fatal("expected validation failure")
	}
	after, _ := os.ReadFile(stackPath)
	if string(after) != testStackYAML {
		t.Error("stack file must be untouched after a validation failure")
	}
	if entries, _ := filepath.Glob(stackPath + ".gridctl-backup-*"); len(entries) != 0 {
		t.Error("no backup should exist when nothing was written")
	}
}

func TestWriteImportedServers_Overwrite(t *testing.T) {
	dir := t.TempDir()
	stackPath := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(stackPath, []byte(testStackYAML), 0644); err != nil {
		t.Fatal(err)
	}

	server, _, err := importer.MapEntry("claude", provisioner.ServerEntry{
		Name: "existing",
		Raw:  map[string]any{"url": "https://replacement.example.com/mcp"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeImportedServers(stackPath, []importer.Candidate{{Name: "existing", Server: server}}, []string{"existing"}); err != nil {
		t.Fatal(err)
	}
	text, _ := os.ReadFile(stackPath)
	if !strings.Contains(string(text), "replacement.example.com/mcp") {
		t.Errorf("replacement entry missing:\n%s", text)
	}
	if strings.Contains(string(text), "image: alpine") {
		t.Errorf("old entry survived the overwrite:\n%s", text)
	}
}

func TestStoreSecret_SuffixesOnConflict(t *testing.T) {
	store := vault.NewStore(t.TempDir())
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}

	key, err := storeSecret(store, "GITHUB_TOKEN", "value-1")
	if err != nil {
		t.Fatal(err)
	}
	if key != "GITHUB_TOKEN" {
		t.Errorf("first store = %q", key)
	}

	// Same value reuses the key; a different value gets a suffix.
	key, err = storeSecret(store, "GITHUB_TOKEN", "value-1")
	if err != nil || key != "GITHUB_TOKEN" {
		t.Errorf("identical value should reuse key, got %q err=%v", key, err)
	}
	key, err = storeSecret(store, "GITHUB_TOKEN", "value-2")
	if err != nil || key != "GITHUB_TOKEN_2" {
		t.Errorf("conflicting value should suffix, got %q err=%v", key, err)
	}

	v, ok := store.GetVariable("GITHUB_TOKEN_2")
	if !ok || !v.IsSecret || v.Value != "value-2" {
		t.Errorf("stored variable = %+v", v)
	}
}

func TestStackServerNames(t *testing.T) {
	names, err := stackServerNames([]byte(testStackYAML))
	if err != nil {
		t.Fatal(err)
	}
	if !names["existing"] || len(names) != 1 {
		t.Errorf("names = %v", names)
	}
	// A stack referencing unset ${VAR}s must still yield names.
	if _, err := stackServerNames([]byte("mcp-servers:\n  - name: a\n    env: {K: \"${UNSET_VAR}\"}\n")); err != nil {
		t.Errorf("var references must not break name extraction: %v", err)
	}
}

func TestSelectCandidates_AllAndInteractive(t *testing.T) {
	candidates := []importer.Candidate{{Name: "a"}, {Name: "b"}}

	origAll, origYes := importAll, importYes
	origSelector := importSelector
	t.Cleanup(func() { importAll, importYes = origAll, origYes; importSelector = origSelector })

	importAll, importYes = true, false
	got, err := selectCandidates(candidates)
	if err != nil || len(got) != 2 {
		t.Errorf("--all selection = %v err=%v", got, err)
	}

	importAll = false
	importSelector = func(c []importer.Candidate) ([]int, error) { return []int{1}, nil }
	got, err = selectCandidates(candidates)
	if err != nil || len(got) != 1 || got[0].Name != "b" {
		t.Errorf("interactive selection = %v err=%v", got, err)
	}
}

func TestImportDoc_JSONShape(t *testing.T) {
	doc := importDoc{
		SchemaVersion: importJSONSchemaVersion,
		StackFile:     "stack.yaml",
		BackupPath:    "stack.yaml.gridctl-backup-x",
		Servers: []importServerDoc{{
			Name: "github", Imported: true, FoundIn: []string{"claude", "cursor"}, Source: "claude",
			Secrets: []importSecretDoc{{Key: "GITHUB_TOKEN", Action: "vaulted", Var: "GITHUB_TOKEN"}},
		}},
		Summary: importSummaryDoc{Found: 1, Imported: 1},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, key := range []string{`"schema_version":1`, `"stack_file"`, `"backup_path"`, `"found_in"`, `"secrets"`, `"action":"vaulted"`, `"summary"`} {
		if !strings.Contains(text, key) {
			t.Errorf("JSON missing %s in:\n%s", key, text)
		}
	}
	// The contract: secret VALUES are never part of the document shape.
	if strings.Contains(text, "value") {
		t.Errorf("secret value field leaked into JSON shape:\n%s", text)
	}
}

func TestScanForCandidates_ScopePrecedence(t *testing.T) {
	localDef := map[string]any{"command": "local-bin"}
	projectDef := map[string]any{"command": "project-bin"}
	userDef := map[string]any{"command": "user-bin"}
	same := map[string]any{"command": "same-bin"}

	t.Run("local beats project", func(t *testing.T) {
		got, skipped := scanForCandidates(output.New(), []importSource{
			{
				Slugs: []string{"claude-code"}, Path: "/home/.claude.json", Scope: "local",
				Entries: []provisioner.ServerEntry{{Name: "x", Raw: localDef}},
			},
			{
				Slugs: []string{"claude-code", "vscode"}, Path: "/repo/.mcp.json", Scope: "project",
				Entries: []provisioner.ServerEntry{{Name: "x", Raw: projectDef}},
			},
		})
		if len(got) != 2 || len(got[0].Server.Command) == 0 || got[0].Server.Command[0] != "local-bin" {
			t.Fatalf("winner = %+v", got)
		}
		if len(got[0].Origins) != 1 || got[0].Origins[0].Scope != "local" {
			t.Fatalf("origins = %+v", got[0].Origins)
		}
		if len(skipped) != 0 {
			t.Fatalf("dedupe must keep the loser importable, skipped = %+v", skipped)
		}
		if !strings.Contains(strings.Join(got[1].Warnings, "\n"), "claude-code (local)") {
			t.Fatalf("warning = %v", got[1].Warnings)
		}
	})

	t.Run("project beats user and identical defs merge", func(t *testing.T) {
		got, _ := scanForCandidates(output.New(), []importSource{
			{
				Slugs: []string{"cursor"}, Path: "/repo/.cursor/mcp.json", Scope: "project",
				Entries: []provisioner.ServerEntry{{Name: "github", Raw: same}},
			},
			{
				Slugs: []string{"cursor"}, Path: "/home/.cursor/mcp.json", Scope: "user",
				Entries: []provisioner.ServerEntry{{Name: "github", Raw: same}},
			},
		})
		if len(got) != 1 {
			t.Fatalf("merged = %d", len(got))
		}
		doc := serverDoc(got[0], true, nil)
		if len(doc.Origins) != 2 || len(doc.Scopes) != 2 || doc.Scopes[0] != "project" || doc.Scopes[1] != "user" {
			t.Fatalf("doc = %+v", doc)
		}
		if len(doc.SourcePaths) != 2 || doc.SourcePaths[0] != "/repo/.cursor/mcp.json" {
			t.Fatalf("paths = %v", doc.SourcePaths)
		}
		if formatProvenance(got[0]) != "cursor (project+user)" {
			t.Fatalf("label = %s", formatProvenance(got[0]))
		}
	})

	t.Run("different user def stays a second candidate", func(t *testing.T) {
		got, _ := scanForCandidates(output.New(), []importSource{
			{
				Slugs: []string{"cursor"}, Path: "/repo/.cursor/mcp.json", Scope: "project",
				Entries: []provisioner.ServerEntry{{Name: "github", Raw: projectDef}},
			},
			{
				Slugs: []string{"cursor"}, Path: "/home/.cursor/mcp.json", Scope: "user",
				Entries: []provisioner.ServerEntry{{Name: "github", Raw: userDef}},
			},
		})
		if len(got) != 2 || got[0].Server.Command[0] != "project-bin" {
			t.Fatalf("candidates = %+v", got)
		}
		warning := strings.Join(got[1].Warnings, "\n")
		if !strings.Contains(warning, "cursor (user)") || !strings.Contains(warning, "cursor (project)") {
			t.Fatalf("warning = %s", warning)
		}
	})
}
