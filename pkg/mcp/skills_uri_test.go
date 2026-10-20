package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSkillURI_RoundTrip(t *testing.T) {
	for _, path := range []string{"", "SKILL.md", "scripts/run.sh", "notes with spaces.md", "x/#notes?.md"} {
		uri := SkillURI("demo", path)
		name, got, ok := parseSkillURI(uri)
		if !ok || name != "demo" || got != path {
			t.Fatalf("parse %q = %q, %q, %v", uri, name, got, ok)
		}
	}
}

func TestSkillURI_RejectsAmbiguousPaths(t *testing.T) {
	for _, uri := range []string{
		"skill://other/demo/SKILL.md", "skills://registry/demo", "skill://registry/", "skill://registry/Demo",
		"skill://registry/-demo", "skill://registry/demo--name", "skill://registry/" + strings.Repeat("a", 65),
		"skill://registry/demo/", "skill://registry/demo//file", "skill://registry/demo/../other/SKILL.md",
		"skill://registry/demo/./file", "skill://registry/demo/%2e%2e/file", "skill://registry/demo/%2Fetc",
		"skill://registry/demo/%00", "skill://registry/demo/a%5Cb", "skill://registry/demo/SKILL.md?query",
		"skill://registry/demo/SKILL.md#fragment", "skill://registry/demo/%53KILL.md",
	} {
		if _, _, ok := parseSkillURI(uri); ok {
			t.Errorf("accepted %q", uri)
		}
	}
}

func TestSkillEntry_Marshal(t *testing.T) {
	entry := SkillEntry{Name: "demo", URI: SkillURI("demo", "SKILL.md"), Frontmatter: json.RawMessage(`{"name":"demo","description":"Demo"}`)}
	for _, dynamic := range []bool{false, true} {
		entry.Dynamic = dynamic
		data, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 {
			t.Fatalf("unexpected fields: %s", data)
		}
		if dynamic {
			if got["resources"] != "dynamic" {
				t.Fatalf("got %s", data)
			}
		} else if files, ok := got["resources"].([]any); !ok || len(files) != 0 {
			t.Fatalf("got %s", data)
		}
	}
}
