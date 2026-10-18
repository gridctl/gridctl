package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeOverride_RejectsDisallowedFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stack.override.yaml")
	cases := []struct {
		name string
		body string
		want string
	}{
		{"image", "mcp-servers:\n  demo:\n    image: alpine\n", "mcp-servers.demo.image is not overridable"},
		{"add server is a field", "mcp-servers:\n  demo:\n    command: [true]\n", "mcp-servers.demo.command is not overridable"},
		{"top level", "gateway:\n  bind: 0.0.0.0\n", `top-level key "gateway" is not allowed`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeOverride([]byte(tc.body), path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestApplyOverride_MergesAllowlist(t *testing.T) {
	stack := &Stack{MCPServers: []MCPServer{{
		Name:    "demo",
		Image:   "alpine",
		Enabled: boolPtr(false),
		Env:     map[string]string{"A": "1", "B": "2"},
		Headers: map[string]string{"X-From": "base"},
	}}}
	ov, err := DecodeOverride([]byte("mcp-servers:\n  demo:\n    enabled: true\n    env:\n      B: override\n      C: new\n    headers:\n      X-Extra: yes\n    replicas: 2\n"), "override.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyOverride(stack, ov, "override.yaml"); err != nil {
		t.Fatal(err)
	}
	srv := stack.MCPServers[0]
	if !srv.IsEnabled() {
		t.Fatal("enabled was not applied")
	}
	if srv.Env["A"] != "1" || srv.Env["B"] != "override" || srv.Env["C"] != "new" {
		t.Fatalf("env = %v", srv.Env)
	}
	if srv.Headers["X-From"] != "base" || srv.Headers["X-Extra"] != "yes" {
		t.Fatalf("headers = %v", srv.Headers)
	}
	if srv.Replicas != 2 {
		t.Fatalf("replicas = %d", srv.Replicas)
	}
	if srv.Image != "alpine" {
		t.Fatal("image changed")
	}
	if stack.OverridePath != "override.yaml" {
		t.Fatalf("path = %s", stack.OverridePath)
	}
	if len(stack.Overrides["demo"]) == 0 {
		t.Fatal("expected recorded fields")
	}
}

func TestApplyOverride_UnknownServer(t *testing.T) {
	stack := &Stack{MCPServers: []MCPServer{{Name: "demo", Image: "alpine"}}}
	ov, err := DecodeOverride([]byte("mcp-servers:\n  missing:\n    enabled: false\n"), "override.yaml")
	if err != nil {
		t.Fatal(err)
	}
	err = ApplyOverride(stack, ov, "override.yaml")
	if err == nil || !strings.Contains(err.Error(), "mcp-servers.missing does not exist") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateStackFile_OverrideExtendsWarning(t *testing.T) {
	dir := t.TempDir()
	stackPath := filepath.Join(dir, "stack.yaml")
	overridePath := filepath.Join(dir, OverrideFileName)
	if err := os.WriteFile(stackPath, []byte("name: child\nextends: parent.yaml\nnetwork:\n  name: n\nmcp-servers:\n  - name: local\n    command: [/bin/true]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overridePath, []byte("mcp-servers:\n  parent-only:\n    enabled: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, result, err := ValidateStackFile(stackPath)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, issue := range result.Issues {
		if issue.Severity == SeverityWarning && strings.Contains(issue.Message, "parent-only") {
			found = true
		}
		if issue.Severity == SeverityError && strings.Contains(issue.Message, "parent-only") {
			t.Fatalf("extends file should warn, not error: %+v", issue)
		}
	}
	if !found {
		t.Fatalf("issues = %+v", result.Issues)
	}
}

func TestDiscoverOverride_MissingExplicit(t *testing.T) {
	_, _, err := DiscoverOverride(filepath.Join(t.TempDir(), "stack.yaml"), OverrideSelection{
		Path:     filepath.Join(t.TempDir(), "missing.yaml"),
		Required: true,
	})
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("err = %v", err)
	}
}

func TestExportNotices_NamesDiscoveredOverride(t *testing.T) {
	dir := t.TempDir()
	stackPath := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(stackPath, []byte("name: demo\nnetwork:\n  name: n\nmcp-servers:\n  - name: local\n    command: [/bin/true]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, OverrideFileName), []byte("mcp-servers:\n  local:\n    enabled: false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stack, _, err := ExportStack(t.Context(), stackPath)
	if err != nil {
		t.Fatal(err)
	}
	if stack.MCPServers[0].Enabled != nil {
		t.Fatal("default export must not merge the override")
	}
	notice := ExportNotices(stack)
	if !strings.Contains(notice, "pass --with-override") {
		t.Fatalf("notice = %s", notice)
	}
	merged, _, err := ExportWithOverride(t.Context(), stackPath, filepath.Join(dir, OverrideFileName))
	if err != nil {
		t.Fatal(err)
	}
	if merged.MCPServers[0].IsEnabled() {
		t.Fatal("with-override should disable the server")
	}
}

func TestOverridableFieldsMatchStruct(t *testing.T) {
	fields := OverridableServerFields()
	want := map[string]bool{
		"enabled": true, "env": true, "headers": true, "output_format": true,
		"ping_timeout": true, "ready_timeout": true, "replicas": true,
		"restart": true, "tools": true,
	}
	if len(fields) != len(want) {
		t.Fatalf("fields = %v", fields)
	}
	for _, field := range fields {
		if !want[field] {
			t.Fatalf("unexpected field %s", field)
		}
	}
}
