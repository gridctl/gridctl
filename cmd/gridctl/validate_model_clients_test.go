package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
)

// modelClientsHome stages a scratch home whose registry holds one agent
// declaring a model, so the agents scope resolves a preference.
func modelClientsHome(t *testing.T) []string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".gridctl", "registry", "agents", "reviewer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: reviewer\ndescription: Reviews\nmodel: opus\n---\n\nReview.\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return append(withoutHomeEnv(t), "HOME="+home, "GRIDCTL_HOME="+home)
}

func modelClientsStack(block string) string {
	return `name: prefs
mcp-servers:
  - name: s
    command: ["sh", "-c", "sleep 1"]
model_preferences:
` + block
}

func validateIssues(t *testing.T, out string) []config.ValidationIssue {
	t.Helper()
	var result config.ValidationResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	return result.Issues
}

func TestValidateModelPreferenceClientsCLI(t *testing.T) {
	bin := buildValidateBinary(t)
	env := modelClientsHome(t)

	run := func(t *testing.T, block string) ([]config.ValidationIssue, int) {
		t.Helper()
		stack := writeValidateStack(t, modelClientsStack(block))
		out, stderr, code := runValidateBinEnv(t, bin, env, stack, "--format", "json")
		if strings.TrimSpace(out) == "" {
			t.Fatalf("no output (exit %d): %s", code, stderr)
		}
		return validateIssues(t, out), code
	}
	unhonored := func(issues []config.ValidationIssue) []string {
		var out []string
		for _, iss := range issues {
			if strings.Contains(iss.Message, "model-preference-unhonored") {
				out = append(out, iss.Message)
			}
		}
		return out
	}

	t.Run("every rendered target covered exits zero", func(t *testing.T) {
		issues, code := run(t, `  agents:
    rewrite: true
    default: sonnet
    clients:
      opencode:
        default: anthropic/claude-sonnet-5-5
      copilot:
        default: gpt-5
      gemini:
        default: inherit
`)
		if code != 0 || len(unhonored(issues)) != 0 {
			t.Fatalf("exit = %d, issues = %+v", code, issues)
		}
	})

	t.Run("only opencode covered exits two naming the rest", func(t *testing.T) {
		issues, code := run(t, `  agents:
    rewrite: true
    default: sonnet
    clients:
      opencode:
        default: anthropic/claude-sonnet-5-5
`)
		lines := unhonored(issues)
		if code != 2 || len(lines) != 1 {
			t.Fatalf("exit = %d, unhonored = %v", code, lines)
		}
		if !strings.Contains(lines[0], "copilot") || !strings.Contains(lines[0], "gemini") || strings.Contains(lines[0], "opencode") {
			t.Errorf("line should name copilot and gemini only: %q", lines[0])
		}
		if !strings.Contains(lines[0], "clients.<slug>") {
			t.Errorf("line should name the remedy: %q", lines[0])
		}
	})

	for name, tc := range map[string]struct{ block, field string }{
		"claude-code slug": {"  agents:\n    clients:\n      claude-code:\n        default: sonnet\n", "model_preferences.agents.clients.claude-code"},
		"unknown slug":     {"  agents:\n    clients:\n      cursor:\n        default: x\n", "model_preferences.agents.clients.cursor"},
		"skills placement": {"  skills:\n    clients:\n      opencode:\n        default: a/b\n", "model_preferences.skills.clients"},
		"empty value":      {"  agents:\n    clients:\n      copilot:\n        overrides:\n          reviewer: \"\"\n", "model_preferences.agents.clients.copilot.overrides.reviewer"},
	} {
		t.Run(name+" exits one", func(t *testing.T) {
			issues, code := run(t, tc.block)
			if code != 1 {
				t.Fatalf("exit = %d, issues = %+v", code, issues)
			}
			found := false
			for _, iss := range issues {
				if iss.Field == tc.field && iss.Severity == config.SeverityError {
					found = true
				}
			}
			if !found {
				t.Fatalf("no error on %s: %+v", tc.field, issues)
			}
		})
	}

	t.Run("opencode value without provider exits two", func(t *testing.T) {
		issues, code := run(t, "  agents:\n    clients:\n      opencode:\n        default: claude-sonnet\n")
		if code != 2 {
			t.Fatalf("exit = %d, issues = %+v", code, issues)
		}
		found := false
		for _, iss := range issues {
			if iss.Field == "model_preferences.agents.clients.opencode.default" && iss.Severity == config.SeverityWarning &&
				strings.Contains(iss.Message, "model-preference-client-value-shape") {
				found = true
			}
		}
		if !found {
			t.Fatalf("no shape warning: %+v", issues)
		}
	})

	t.Run("pre-existing findings stay advisory", func(t *testing.T) {
		issues, code := run(t, `  skills:
    default: fastest
  agents:
    rewrite: true
    default: sonnet
`)
		if code != 2 {
			t.Fatalf("exit = %d, want 2 (warnings only): %+v", code, issues)
		}
		var alias, info, unh bool
		for _, iss := range issues {
			switch {
			case strings.Contains(iss.Message, "model-preference-unknown-alias"):
				alias = iss.Severity == config.SeverityWarning
			case strings.HasPrefix(iss.Message, "projection rewrite enabled"):
				info = iss.Severity == config.SeverityInfo
			case strings.Contains(iss.Message, "model-preference-unhonored"):
				unh = iss.Severity == config.SeverityWarning
			}
			if iss.Severity == config.SeverityError {
				t.Errorf("unexpected error: %+v", iss)
			}
		}
		if !alias || !info || !unh {
			t.Fatalf("severities changed (alias=%v info=%v unhonored=%v): %+v", alias, info, unh, issues)
		}
	})
}
