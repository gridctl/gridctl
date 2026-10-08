package importer

import (
	"errors"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/provisioner"
)

func TestMapEntryWithOptions_EmptyMatchesMapEntry(t *testing.T) {
	raw := map[string]any{
		"command": "npx -y server",
		"env":     map[string]any{"REGION": "us-west", "API_KEY": "${env:TOKEN}"},
		"cwd":     "./synthetic-workspace",
		"url":     "",
	}
	left, lw, lerr := MapEntry("cursor", provisioner.ServerEntry{Name: "srv", Raw: raw})
	right, rw, rerr := MapEntryWithOptions("cursor", provisioner.ServerEntry{Name: "srv", Raw: raw}, MapOptions{})
	if (lerr == nil) != (rerr == nil) || left.URL != right.URL || strings.Join(left.Command, "\x00") != strings.Join(right.Command, "\x00") {
		t.Fatalf("empty options diverged: %+v %v vs %+v %v", left, lerr, right, rerr)
	}
	if strings.Join(lw, "\n") != strings.Join(rw, "\n") {
		t.Fatalf("warnings diverged:\n%v\n%v", lw, rw)
	}
	if strings.Contains(strings.Join(left.Command, " "), "synthetic-workspace") {
		t.Fatal("cwd leaked into the command")
	}
}

func TestMapEntryWithOptions_Placeholders(t *testing.T) {
	owner := "/home/u/My Project"
	server, warnings, err := MapEntryWithOptions("cursor", provisioner.ServerEntry{
		Name: "tools",
		Raw: map[string]any{
			"command": "python",
			"args":    []any{"${workspaceFolder}/tools/server.py"},
			"env":     map[string]any{"CONFIG_PATH": "${workspaceFolder}/cfg", "API_KEY": "${workspaceFolder}/token"},
			"url":     "",
		},
	}, MapOptions{OwnerDir: owner, Scope: "project"})
	if err != nil {
		t.Fatal(err)
	}
	wantCmd := []string{"python", owner + "/tools/server.py"}
	if strings.Join(server.Command, "\x00") != strings.Join(wantCmd, "\x00") {
		t.Fatalf("command = %#v", server.Command)
	}
	if server.Env["CONFIG_PATH"] != owner+"/cfg" || server.Env["API_KEY"] != owner+"/token" {
		t.Fatalf("env = %#v", server.Env)
	}
	if got := ClassifySecretKeys(server.Env); len(got) != 1 || got[0] != "API_KEY" {
		t.Fatalf("substituted secret keys = %v", got)
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "resolved ${workspaceFolder} to "+owner) || strings.Count(joined, "resolved ${workspaceFolder}") != 1 {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestMapEntryWithOptions_CommandSpaceAndUnsupported(t *testing.T) {
	owner := "/home/u/My Project"
	server, _, err := MapEntryWithOptions("claude-code", provisioner.ServerEntry{
		Name: "bin",
		Raw:  map[string]any{"command": "${CLAUDE_PROJECT_DIR:-.}/bin/server --flag"},
	}, MapOptions{OwnerDir: owner, Scope: "project"})
	if err != nil {
		t.Fatal(err)
	}
	if len(server.Command) != 2 || server.Command[0] != owner+"/bin/server" || server.Command[1] != "--flag" {
		t.Fatalf("command = %#v", server.Command)
	}

	_, _, err = MapEntryWithOptions("vscode", provisioner.ServerEntry{
		Name: "input",
		Raw:  map[string]any{"command": "node", "args": []any{"${input:token}"}},
	}, MapOptions{OwnerDir: owner, Scope: "project"})
	var me *MapError
	if !errors.As(err, &me) || me.Reason != SkipNativeExpression || strings.Contains(me.Detail, "token") || !strings.Contains(me.Detail, "${input:...}") {
		t.Fatalf("input skip = %v", err)
	}

	_, _, err = MapEntryWithOptions("cursor", provisioner.ServerEntry{
		Name: "hdr",
		Raw:  map[string]any{"url": "https://example.test/mcp", "headers": map[string]any{"X-Token": "${input:secret-value}"}},
	}, MapOptions{OwnerDir: owner, Scope: "project"})
	if !errors.As(err, &me) || strings.Contains(me.Detail, "secret-value") {
		t.Fatalf("header skip = %v", err)
	}
}

func TestMapEntryWithOptions_OpenCodeNativeArray(t *testing.T) {
	owner := "/home/u/My Project"
	server, warnings, err := MapEntryWithOptions("opencode", provisioner.ServerEntry{
		Name: "s",
		Raw:  map[string]any{"command": []any{"node", "${workspaceFolder}/s.js"}, "environment": map[string]any{"REGION": "{env:REGION}"}},
	}, MapOptions{OwnerDir: owner, Scope: "project"})
	if err != nil {
		t.Fatal(err)
	}
	if len(server.Command) != 2 || server.Command[0] != "node" || server.Command[1] != owner+"/s.js" {
		t.Fatalf("argv = %#v", server.Command)
	}
	if server.Env["REGION"] != "${REGION}" {
		t.Fatalf("env = %#v", server.Env)
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "${workspaceFolder}") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestMapEntryWithOptions_CwdByScope(t *testing.T) {
	raw := map[string]any{"command": "echo", "cwd": "./x"}
	_, _, err := MapEntryWithOptions("gemini", provisioner.ServerEntry{Name: "g", Raw: raw}, MapOptions{Scope: "project"})
	var me *MapError
	if !errors.As(err, &me) || me.Reason != SkipUntransferredOption || me.Detail != "working directory cannot be transferred to the stack" || strings.Contains(me.Detail, "./x") {
		t.Fatalf("project cwd = %v", err)
	}
	_, _, err = MapEntryWithOptions("gemini", provisioner.ServerEntry{Name: "g", Raw: raw}, MapOptions{Scope: "local"})
	if !errors.As(err, &me) || me.Reason != SkipUntransferredOption {
		t.Fatalf("local cwd = %v", err)
	}
	server, warnings, err := MapEntryWithOptions("gemini", provisioner.ServerEntry{Name: "g", Raw: raw}, MapOptions{Scope: "user"})
	if err != nil || len(server.Command) != 1 {
		t.Fatalf("user cwd import = %+v err %v", server, err)
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "cwd was not transferred") || strings.Contains(strings.Join(warnings, "\n"), "./x") {
		t.Fatalf("user warning = %v", warnings)
	}
	_, _, err = MapEntryWithOptions("opencode", provisioner.ServerEntry{Name: "o", Raw: raw}, MapOptions{Scope: "user"})
	if !errors.As(err, &me) || me.Reason != SkipUntransferredOption || strings.Contains(me.Detail, "./x") {
		t.Fatalf("opencode user cwd = %v", err)
	}
	_, _, err = MapEntryWithOptions("opencode", provisioner.ServerEntry{Name: "o", Raw: raw}, MapOptions{Scope: "project"})
	if !errors.As(err, &me) || me.Reason != SkipUntransferredOption {
		t.Fatalf("opencode project cwd = %v", err)
	}
}

func TestDedupe_MergesOriginsAndNamesScopes(t *testing.T) {
	server, _, err := MapEntry("cursor", provisioner.ServerEntry{Name: "github", Raw: map[string]any{"command": "same"}})
	if err != nil {
		t.Fatal(err)
	}
	project := Candidate{
		Name: "github", Server: server, Source: "cursor", FoundIn: []string{"cursor"},
		SourcePath: "/repo/.cursor/mcp.json", SourcePaths: []string{"/repo/.cursor/mcp.json"},
		Origins: []Origin{{Client: "cursor", Scope: "project", Path: "/repo/.cursor/mcp.json"}},
	}
	user := Candidate{
		Name: "github", Server: server, Source: "cursor", FoundIn: []string{"cursor"},
		SourcePath: "/home/.cursor/mcp.json", SourcePaths: []string{"/home/.cursor/mcp.json"},
		Origins: []Origin{{Client: "cursor", Scope: "user", Path: "/home/.cursor/mcp.json"}},
	}
	out := Dedupe([]Candidate{project, user})
	if len(out) != 1 {
		t.Fatalf("identical definitions = %d", len(out))
	}
	if len(out[0].Origins) != 2 || out[0].SourcePath != "/repo/.cursor/mcp.json" || len(out[0].SourcePaths) != 2 {
		t.Fatalf("merged = %+v", out[0])
	}
	scopes := out[0].Scopes()
	if len(scopes) != 2 || scopes[0] != "project" || scopes[1] != "user" {
		t.Fatalf("scopes = %v", scopes)
	}
	if got := out[0].ScopesFor("cursor"); len(got) != 2 {
		t.Fatalf("scopes for cursor = %v", got)
	}

	other, _, err := MapEntry("cursor", provisioner.ServerEntry{Name: "github", Raw: map[string]any{"command": "other"}})
	if err != nil {
		t.Fatal(err)
	}
	user.Server = other
	out = Dedupe([]Candidate{project, user})
	if len(out) != 2 {
		t.Fatalf("different definitions = %d", len(out))
	}
	warning := strings.Join(out[1].Warnings, "\n")
	if !strings.Contains(warning, "cursor (user)") || !strings.Contains(warning, "cursor (project)") {
		t.Fatalf("warning = %s", warning)
	}
	if out[0].ProvenanceLabel() != "cursor (project)" {
		t.Fatalf("label = %s", out[0].ProvenanceLabel())
	}
}
