package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/agentsync"
	"github.com/gridctl/gridctl/pkg/registry"
)

func writeTestAgent(t *testing.T, regDir, name, content string) {
	t.Helper()
	dir := filepath.Join(regDir, "agents", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The text status table labels every agent row whose installed bytes
// carry a model policy rewrite (identity and rendered alike) as
// "copy (model policy)"; the JSON channel stays "copy".
func TestRunSkillProjectStatus_AgentModelPolicyLabel(t *testing.T) {
	skillMgr, home := newSkillProjectTestManager(t)
	for _, dir := range []string{".config/opencode", ".copilot", ".gemini"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	regDir := filepath.Join(home, ".gridctl", "registry")
	writeTestAgent(t, regDir, "reviewer", "---\nname: reviewer\ndescription: Reviews\nmodel: opus\n---\n\nReview.\n")
	agentMgr := agentsync.NewManagerWithHome(home, regDir)
	agentMgr.SetModelPolicy(&registry.ModelPolicy{
		Rewrite:   true,
		Overrides: map[string]string{"reviewer": "sonnet"},
		Clients: map[string]registry.ClientModelPolicy{
			"opencode": {Default: "anthropic/claude-sonnet-5-5"},
		},
	})
	ctx := context.Background()
	if _, err := agentMgr.Sync(ctx, nil, agentsync.SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if exit := runSkillProjectStatus(ctx, &out, &errOut, skillMgr, agentMgr, nil, "", true); exit != ctxExitOK {
		t.Fatalf("exit = %d, stderr: %s\n%s", exit, errOut.String(), out.String())
	}
	channels := map[string]string{}
	for _, line := range strings.Split(out.String(), "\n") {
		if !strings.Contains(line, "reviewer (agent)") {
			continue
		}
		for _, client := range []string{"claude-code", "opencode", "copilot", "gemini"} {
			if strings.Contains(line, " "+client+" ") {
				channels[client] = line
			}
		}
	}
	for client, labeled := range map[string]bool{"claude-code": true, "opencode": true, "copilot": false, "gemini": false} {
		line, ok := channels[client]
		if !ok {
			t.Fatalf("no %s agent row in:\n%s", client, out.String())
		}
		if got := strings.Contains(line, "copy (model policy)"); got != labeled {
			t.Errorf("%s row labeled = %v, want %v: %q", client, got, labeled, line)
		}
		if !strings.Contains(line, "in-sync") {
			t.Errorf("%s row should be in-sync: %q", client, line)
		}
	}

	out.Reset()
	if exit := runSkillProjectStatus(ctx, &out, &errOut, skillMgr, agentMgr, nil, "json", false); exit != ctxExitOK {
		t.Fatalf("json exit = %d", exit)
	}
	var doc struct {
		Projections []map[string]any `json:"projections"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, row := range doc.Projections {
		if row["kind"] != "agent" {
			continue
		}
		seen++
		if row["channel"] != "copy" {
			t.Errorf("JSON channel must stay copy, got %v", row["channel"])
		}
		want := map[string]any{"claude-code": "sonnet", "opencode": "anthropic/claude-sonnet-5-5"}[row["client"].(string)]
		if row["model_value"] != want {
			t.Errorf("%v model_value = %v, want %v", row["client"], row["model_value"], want)
		}
	}
	if seen != 4 {
		t.Fatalf("agent rows = %d, want 4", seen)
	}
}

func TestSyncModelPolicies_ClientsStructuralErrorRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	stack := `version: "1"
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
`
	if err := os.WriteFile(path, []byte(stack), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := syncModelPolicies(path)
	if err == nil || !strings.Contains(err.Error(), "model_preferences.agents.clients.claude-code") {
		t.Fatalf("expected the clients error to refuse the sync, got %v", err)
	}

	good := strings.Replace(stack, "claude-code:\n        default: sonnet", "opencode:\n        default: anthropic/claude-sonnet-5-5", 1)
	if err := os.WriteFile(path, []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	_, agentPol, err := syncModelPolicies(path)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := agentPol.ResolveClient("opencode", "any"); v != "anthropic/claude-sonnet-5-5" {
		t.Fatalf("compiled client value = %q", v)
	}
}
