package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/registry"
)

func clientsStack(mp *ModelPreferencesConfig) *Stack {
	return &Stack{
		Version: "1", Name: "x",
		Network:          Network{Name: "net"},
		MCPServers:       []MCPServer{{Name: "s1", URL: "https://api.example.com/mcp"}},
		ModelPreferences: mp,
	}
}

func issuesByField(r *ValidationResult) map[string]ValidationIssue {
	out := map[string]ValidationIssue{}
	for _, iss := range r.Issues {
		out[iss.Field] = iss
	}
	return out
}

func TestModelPreferences_ClientsParseAndCompile(t *testing.T) {
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
  agents:
    rewrite: true
    default: sonnet
    clients:
      opencode:
        default: anthropic/claude-sonnet-5-5
        overrides:
          reviewer: anthropic/claude-opus-5-5#high
      gemini:
        default: inherit
`)
	stack, err := LoadStack(filepath.Join(dir, "stack.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	_, agents := stack.ModelPolicies()
	if v, res := agents.ResolveClient("opencode", "reviewer"); v != "anthropic/claude-opus-5-5#high" || res != registry.ResolutionOverride {
		t.Errorf("opencode override = (%q, %q)", v, res)
	}
	if v, res := agents.ResolveClient("opencode", "other"); v != "anthropic/claude-sonnet-5-5" || res != registry.ResolutionDefault {
		t.Errorf("opencode default = (%q, %q)", v, res)
	}
	if v, _ := agents.ResolveClient("gemini", "x"); v != "inherit" {
		t.Errorf("gemini default = %q", v)
	}
	if v, _ := agents.ResolveClient("copilot", "x"); v != "" {
		t.Errorf("copilot has no value, got %q", v)
	}
	// The top-level policy is untouched by the client block.
	if agents.Default != "sonnet" || !agents.Rewrite {
		t.Errorf("top-level policy = %+v", agents)
	}
}

func TestModelPreferences_ClientsNilCompilesWithoutMap(t *testing.T) {
	p := (&ModelPreferenceScope{Rewrite: true}).Compile()
	if p.Clients != nil {
		t.Fatalf("absent clients must compile to a nil map, got %+v", p.Clients)
	}
	if v, _ := p.ResolveClient("opencode", "a"); v != "" {
		t.Fatalf("nil clients resolved %q", v)
	}
	p = (&ModelPreferenceScope{Clients: map[string]*ModelPreferenceClient{"opencode": nil}}).Compile()
	if v, _ := p.ResolveClient("opencode", "a"); v != "" {
		t.Fatalf("nil client entry resolved %q", v)
	}
}

func TestModelPreferences_ClientsUnknownKeyRejected(t *testing.T) {
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
  agents:
    clients:
      opencode:
        model: a/b
`)
	if _, err := LoadStack(filepath.Join(dir, "stack.yaml")); err == nil {
		t.Fatal("unknown key inside a client entry must be rejected by strict decoding")
	}
}

func TestModelPreferences_ClientsStructuralErrors(t *testing.T) {
	tests := []struct {
		name  string
		mp    *ModelPreferencesConfig
		field string
	}{
		{
			name:  "claude-code slug",
			mp:    &ModelPreferencesConfig{Agents: &ModelPreferenceScope{Clients: map[string]*ModelPreferenceClient{"claude-code": {Default: "sonnet"}}}},
			field: "model_preferences.agents.clients.claude-code",
		},
		{
			name:  "unknown slug",
			mp:    &ModelPreferencesConfig{Agents: &ModelPreferenceScope{Clients: map[string]*ModelPreferenceClient{"cursor": {Default: "x"}}}},
			field: "model_preferences.agents.clients.cursor",
		},
		{
			name:  "skills placement",
			mp:    &ModelPreferencesConfig{Skills: &ModelPreferenceScope{Clients: map[string]*ModelPreferenceClient{"opencode": {Default: "a/b"}}}},
			field: "model_preferences.skills.clients",
		},
		{
			name:  "empty override value",
			mp:    &ModelPreferencesConfig{Agents: &ModelPreferenceScope{Clients: map[string]*ModelPreferenceClient{"copilot": {Default: "gpt-5", Overrides: map[string]string{"reviewer": ""}}}}},
			field: "model_preferences.agents.clients.copilot.overrides.reviewer",
		},
		{
			name:  "whitespace default",
			mp:    &ModelPreferencesConfig{Agents: &ModelPreferenceScope{Clients: map[string]*ModelPreferenceClient{"gemini": {Default: "  "}}}},
			field: "model_preferences.agents.clients.gemini.default",
		},
		{
			name:  "entry with no value",
			mp:    &ModelPreferencesConfig{Agents: &ModelPreferenceScope{Clients: map[string]*ModelPreferenceClient{"gemini": {}}}},
			field: "model_preferences.agents.clients.gemini",
		},
		{
			name:  "null entry",
			mp:    &ModelPreferencesConfig{Agents: &ModelPreferenceScope{Clients: map[string]*ModelPreferenceClient{"opencode": nil}}},
			field: "model_preferences.agents.clients.opencode",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := ValidateWithIssues(clientsStack(tt.mp))
			iss, ok := issuesByField(r)[tt.field]
			if !ok || iss.Severity != SeverityError {
				t.Fatalf("want an error on %s, got %+v", tt.field, r.Issues)
			}
			if r.Valid || r.ErrorCount == 0 {
				t.Fatalf("structural client errors must fail validation: %+v", r)
			}
			if err := Validate(clientsStack(tt.mp)); err == nil || !strings.Contains(err.Error(), tt.field) {
				t.Fatalf("Validate must name %s (blocking apply and reload), got %v", tt.field, err)
			}
		})
	}
}

func TestModelPreferences_ClientValueShape(t *testing.T) {
	mp := &ModelPreferencesConfig{Agents: &ModelPreferenceScope{
		Rewrite: true,
		Clients: map[string]*ModelPreferenceClient{
			"opencode": {
				Default: "claude-sonnet",
				Overrides: map[string]string{
					"ok":      "anthropic/claude-opus-5-5#high",
					"plain":   "openrouter/meta/llama-4",
					"spaced":  "anthropic/claude opus",
					"novar":   "anthropic/claude-opus-5-5#",
					"noprov":  "/claude",
					"nomodel": "anthropic/",
				},
			},
			// Copilot and Gemini values only need to be non-empty, and
			// never trip the Claude alias check.
			"copilot": {Default: "GPT-5 (copilot)"},
			"gemini":  {Default: "inherit"},
		},
	}}
	r := ValidateWithIssues(clientsStack(mp))
	if r.ErrorCount != 0 {
		t.Fatalf("shape findings must be advisory, got errors: %+v", r.Issues)
	}
	byField := issuesByField(r)
	wantShape := []string{
		"model_preferences.agents.clients.opencode.default",
		"model_preferences.agents.clients.opencode.overrides.spaced",
		"model_preferences.agents.clients.opencode.overrides.novar",
		"model_preferences.agents.clients.opencode.overrides.noprov",
		"model_preferences.agents.clients.opencode.overrides.nomodel",
	}
	for _, f := range wantShape {
		iss, ok := byField[f]
		if !ok || iss.Severity != SeverityWarning || !strings.Contains(iss.Message, "model-preference-client-value-shape") {
			t.Errorf("want shape warning on %s, got %+v", f, iss)
		}
	}
	def := byField["model_preferences.agents.clients.opencode.default"]
	if !strings.Contains(def.Message, `"claude-sonnet" is not shaped like provider/model[#variant]; run 'opencode models' for exact IDs`) {
		t.Errorf("shape message = %q", def.Message)
	}
	for _, f := range []string{
		"model_preferences.agents.clients.opencode.overrides.ok",
		"model_preferences.agents.clients.opencode.overrides.plain",
		"model_preferences.agents.clients.copilot.default",
		"model_preferences.agents.clients.gemini.default",
	} {
		if iss, ok := byField[f]; ok {
			t.Errorf("unexpected finding on %s: %+v", f, iss)
		}
	}
	for _, iss := range r.Issues {
		if strings.Contains(iss.Message, "model-preference-unknown-alias") {
			t.Errorf("client values must never be alias-checked: %+v", iss)
		}
	}
	if r.WarningCount != len(wantShape) {
		t.Errorf("warning count = %d, want %d: %+v", r.WarningCount, len(wantShape), r.Issues)
	}
	info, ok := byField["model_preferences.agents"]
	if !ok || info.Severity != SeverityInfo || !strings.HasSuffix(info.Message, ", client values for: copilot, gemini, opencode") {
		t.Errorf("rewrite info line should list client slugs, got %+v", info)
	}
}

// Pre-existing findings keep their severities: the clients structural
// errors are the only non-advisory model preference findings.
func TestModelPreferences_ExistingFindingsStayAdvisory(t *testing.T) {
	mp := &ModelPreferencesConfig{
		Skills: &ModelPreferenceScope{Rewrite: true, Default: "fastest"},
		Agents: &ModelPreferenceScope{
			Rewrite:   true,
			Default:   "sonnet",
			Overrides: map[string]string{"ghost": "gpt-4o"},
			Clients:   map[string]*ModelPreferenceClient{"opencode": {Default: "anthropic/claude-sonnet-5-5"}},
		},
	}
	r := ValidateWithIssues(clientsStack(mp))
	base := ValidateWithIssues(clientsStack(nil))
	if r.ErrorCount != base.ErrorCount || !r.Valid {
		t.Fatalf("valid clients plus pre-existing findings must add no errors: %+v", r.Issues)
	}
	sev := map[string]IssueSeverity{}
	for _, iss := range r.Issues {
		switch {
		case strings.Contains(iss.Message, "model-preference-unknown-alias"):
			sev["alias:"+iss.Field] = iss.Severity
		case strings.HasPrefix(iss.Message, "projection rewrite enabled"):
			sev["info:"+iss.Field] = iss.Severity
		}
	}
	want := map[string]IssueSeverity{
		"alias:model_preferences.skills.default":         SeverityWarning,
		"alias:model_preferences.agents.overrides.ghost": SeverityWarning,
		"info:model_preferences.skills":                  SeverityInfo,
		"info:model_preferences.agents":                  SeverityInfo,
	}
	for k, v := range want {
		if sev[k] != v {
			t.Errorf("%s severity = %q, want %q", k, sev[k], v)
		}
	}
}

// LoadStack (apply, reload, and the daemon) refuses a structural
// clients error rather than loading a policy that can never apply.
func TestModelPreferences_ClientsStructuralErrorBlocksLoad(t *testing.T) {
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
  agents:
    rewrite: true
    clients:
      claude-code:
        default: sonnet
`)
	_, err := LoadStack(filepath.Join(dir, "stack.yaml"))
	if err == nil || !strings.Contains(err.Error(), "model_preferences.agents.clients.claude-code") {
		t.Fatalf("LoadStack must refuse a structural clients error, got %v", err)
	}
}
