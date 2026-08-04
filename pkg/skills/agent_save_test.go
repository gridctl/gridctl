package skills

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveAgent_WritesVerbatim(t *testing.T) {
	dir := t.TempDir()
	// Deliberately awkward input: vendor keys, unusual key order, no
	// trailing newline. SaveAgent must write these bytes untouched;
	// identity projection depends on byte-level fidelity.
	raw := []byte("---\nmodel: opus\nname: code-reviewer\nx-vendor:\n  nested: [1, 2]\ndescription: Reviews code\ntools: Read, Grep\n---\nBody line.")

	def, err := SaveAgent(dir, "code-reviewer", raw)
	if err != nil {
		t.Fatalf("SaveAgent: %v", err)
	}
	if def.Name != "code-reviewer" {
		t.Errorf("Name = %q", def.Name)
	}
	wantKeys := []string{"model", "x-vendor", "tools"}
	if len(def.Extra) != len(wantKeys) {
		t.Fatalf("Extra has %d keys, want %d", len(def.Extra), len(wantKeys))
	}
	for i, want := range wantKeys {
		if def.Extra[i].Key != want {
			t.Errorf("Extra[%d].Key = %q, want %q (order must be preserved)", i, def.Extra[i].Key, want)
		}
	}

	got, err := os.ReadFile(filepath.Join(AgentDir(dir, "code-reviewer"), "AGENT.md"))
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Errorf("written bytes differ from input:\n got: %q\nwant: %q", got, raw)
	}

	// A second save round-trips through GetAgent identically.
	a, err := GetAgent(dir, "code-reviewer")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if _, err := SaveAgent(dir, "code-reviewer", a.Definition.Raw); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	got2, _ := os.ReadFile(filepath.Join(AgentDir(dir, "code-reviewer"), "AGENT.md"))
	if !bytes.Equal(got2, raw) {
		t.Errorf("re-save changed bytes")
	}
}

func TestSaveAgent_Rejections(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]struct {
		name    string
		raw     string
		wantErr string
	}{
		"invalid name":       {"Bad_Name", "---\ndescription: d\n---\n", "must be lowercase"},
		"missing fm":         {"agent", "# README\n", "missing frontmatter"},
		"no description":     {"agent", "---\nname: agent\n---\nbody\n", "no description"},
		"rename via fm name": {"agent", "---\nname: other\ndescription: d\n---\nbody\n", "renames are not supported"},
	}
	for label, tc := range cases {
		if _, err := SaveAgent(dir, tc.name, []byte(tc.raw)); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: err = %v, want containing %q", label, err, tc.wantErr)
		}
	}
	if entries, _ := os.ReadDir(AgentsRoot(dir)); len(entries) > 0 {
		t.Errorf("rejected saves must not leave files behind: %v", entries)
	}
}
