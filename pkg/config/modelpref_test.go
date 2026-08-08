package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestModelPreferences_ParseAndValidate(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "stack.yaml"), `
version: "1"
name: prefs
network:
  name: net
mcp-servers:
  - name: s1
    url: https://api.example.com/mcp
model_preferences:
  skills:
    rewrite: true
    default: sonnet
    overrides:
      incident-triage: opus
      simple-formatter: haiku
  agents:
    default: sonnet
`)
	stack, err := LoadStack(filepath.Join(dir, "stack.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	mp := stack.ModelPreferences
	if mp == nil || mp.Skills == nil || mp.Agents == nil {
		t.Fatalf("model_preferences not parsed: %+v", mp)
	}
	if !mp.Skills.Rewrite || mp.Skills.Default != "sonnet" {
		t.Errorf("skills scope = %+v", mp.Skills)
	}
	if mp.Skills.Overrides["incident-triage"] != "opus" {
		t.Errorf("overrides = %+v", mp.Skills.Overrides)
	}
	if mp.Agents.Rewrite {
		t.Error("agents rewrite should default false")
	}
}

func TestModelPreferences_OmittedBlockIsNil(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "stack.yaml"), `
version: "1"
name: plain
network:
  name: net
mcp-servers:
  - name: s1
    url: https://api.example.com/mcp
`)
	stack, err := LoadStack(filepath.Join(dir, "stack.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if stack.ModelPreferences != nil {
		t.Fatalf("omitted block must stay nil (Article IX), got %+v", stack.ModelPreferences)
	}
}

func TestModelPreferences_UnknownAliasWarns(t *testing.T) {
	s := &Stack{
		Version: "1", Name: "x",
		MCPServers: []MCPServer{{Name: "s1", URL: "https://api.example.com/mcp"}},
		ModelPreferences: &ModelPreferencesConfig{
			Skills: &ModelPreferenceScope{
				Default:   "fastest",
				Overrides: map[string]string{"a-skill": "gpt-4o", "b-skill": "opus"},
			},
		},
	}
	r := ValidateWithIssues(s)
	var warned []string
	for _, iss := range r.Issues {
		if strings.Contains(iss.Message, "model-preference-unknown-alias") {
			warned = append(warned, iss.Field)
		}
	}
	if len(warned) != 2 {
		t.Fatalf("expected 2 unknown-alias warnings (fastest, gpt-4o), got %d: %v", len(warned), warned)
	}
	for _, f := range warned {
		if !strings.HasPrefix(f, "model_preferences.skills.") {
			t.Errorf("warning field %q should be under model_preferences.skills", f)
		}
	}
	// Advisory only: the block must add zero errors over the same stack
	// without it.
	base := *s
	base.ModelPreferences = nil
	if r.ErrorCount != ValidateWithIssues(&base).ErrorCount {
		t.Fatalf("model preference findings must never add errors")
	}
}

func TestModelPreferences_RewriteSurfacesAsInfo(t *testing.T) {
	s := &Stack{
		Version: "1", Name: "x",
		MCPServers: []MCPServer{{Name: "s1", URL: "https://api.example.com/mcp"}},
		ModelPreferences: &ModelPreferencesConfig{
			Skills: &ModelPreferenceScope{Rewrite: true, Default: "sonnet"},
		},
	}
	r := ValidateWithIssues(s)
	found := false
	for _, iss := range r.Issues {
		if iss.Field == "model_preferences.skills" && iss.Severity == SeverityInfo {
			found = true
			if !strings.Contains(iss.Message, "copy channel") {
				t.Errorf("info line should name the channel consequence, got %q", iss.Message)
			}
		}
	}
	if !found {
		t.Fatal("rewrite: true should surface an info issue")
	}
}

func TestLoadStack_Extends_ModelPreferencesNotInherited(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "base.yaml"), `
version: "1"
name: base
network:
  name: net
mcp-servers:
  - name: s1
    url: https://api.example.com/mcp
model_preferences:
  skills:
    rewrite: true
    default: haiku
`)
	writeFile(t, filepath.Join(dir, "child.yaml"), `
version: "1"
name: child
extends: ./base.yaml
mcp-servers:
  - name: s2
    url: https://api2.example.com/mcp
`)
	stack, err := LoadStack(filepath.Join(dir, "child.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if stack.ModelPreferences != nil {
		t.Fatalf("model_preferences must not be inherited across extends (matching clients/groups/limits), got %+v", stack.ModelPreferences)
	}
}
