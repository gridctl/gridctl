package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestDiagnoseDeclarations_ReportsDeterministicAdvisories(t *testing.T) {
	required := true
	plaintext := false
	stack := &Stack{
		Variables: map[string]VariableDeclaration{
			"MISSING": {Required: &required},
			"TOKEN":   {Secret: &plaintext, Type: "json"},
		},
		References: ReferenceIndex{"MISSING": {{Kind: RefKindStack, Field: "name"}}},
	}
	diagnostics := DiagnoseDeclarations(stack, map[string]VariableMetadata{
		"TOKEN": {Type: "string", Secret: true, Deprecated: "use NEW_TOKEN"},
	}, false)
	want := []string{"required_unset", "declared_unused", "deprecated", "sensitivity_mismatch", "type_mismatch"}
	if len(diagnostics) != len(want) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for i, code := range want {
		if diagnostics[i].Code != code {
			t.Fatalf("diagnostics[%d].Code = %q, want %q", i, diagnostics[i].Code, code)
		}
	}
}

func TestParseStackIndex_DoesNotExpandReturnedValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stack.yaml")
	content := `name: test
mcp-servers:
  - name: example
    url: https://example.invalid
    env:
      TOKEN: ${var:TOKEN:-literal-default}
      AMBIENT: $PARSE_INDEX_AMBIENT
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PARSE_INDEX_AMBIENT", "must-not-expand")
	stack, err := ParseStackIndex(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got := stack.MCPServers[0].Env["TOKEN"]; got != "${var:TOKEN:-literal-default}" {
		t.Fatalf("TOKEN = %q", got)
	}
	if got := stack.MCPServers[0].Env["AMBIENT"]; got != "$PARSE_INDEX_AMBIENT" {
		t.Fatalf("AMBIENT = %q", got)
	}
	if len(stack.References["TOKEN"]) != 1 {
		t.Fatalf("TOKEN references = %#v", stack.References["TOKEN"])
	}
}

func TestParseStackIndex_RejectsSpecialAndOversizeFiles(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "stack.yaml")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseStackIndex(context.Background(), fifo); err == nil {
		t.Fatal("expected fifo rejection")
	}
	big := filepath.Join(dir, "big.yaml")
	if err := os.WriteFile(big, []byte(strings.Repeat("a", maxIndexFileBytes+2)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseStackIndex(context.Background(), big); err == nil {
		t.Fatal("expected oversize rejection")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ParseStackIndex(ctx, filepath.Join(dir, "missing.yaml")); err == nil {
		t.Fatal("expected cancellation")
	}
}
