package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
)

func TestInitScaffoldValidates(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer

	if err := runInit(&buf, dir, "demo", "minimal", false); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	stackPath := filepath.Join(dir, "stack.yaml")
	stack, err := config.LoadStack(stackPath)
	if err != nil {
		t.Fatalf("scaffold does not load: %v", err)
	}
	if err := config.Validate(stack); err != nil {
		t.Fatalf("scaffold does not validate: %v", err)
	}
	if stack.Name != "demo" {
		t.Errorf("stack name = %q, want demo", stack.Name)
	}
	if stack.Network.Name != "demo-net" {
		t.Errorf("network name = %q, want demo-net", stack.Network.Name)
	}
	if len(stack.MCPServers) != 1 || stack.MCPServers[0].Name != "everything" {
		t.Errorf("scaffold should declare exactly the example server, got %+v", stack.MCPServers)
	}

	out := buf.String()
	for _, want := range []string{"Next steps", "gridctl apply", "gridctl link"} {
		if !strings.Contains(out, want) {
			t.Errorf("init output missing %q:\n%s", want, out)
		}
	}
}

func TestInitRefusesOverwriteWithoutForce(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer

	if err := runInit(&buf, dir, "demo", "minimal", false); err != nil {
		t.Fatalf("first runInit: %v", err)
	}
	err := runInit(&buf, dir, "demo", "minimal", false)
	if err == nil {
		t.Fatal("second runInit should refuse to overwrite")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("overwrite error should name --force: %v", err)
	}
	if err := runInit(&buf, dir, "other", "minimal", true); err != nil {
		t.Fatalf("runInit --force: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "stack.yaml"))
	if err != nil {
		t.Fatalf("reading scaffold: %v", err)
	}
	if !strings.Contains(string(data), "name: other") {
		t.Error("--force did not overwrite the scaffold")
	}
}

func TestInitSkillsExample(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer

	if err := runInit(&buf, dir, "demo", "skills", false); err != nil {
		t.Fatalf("runInit skills: %v", err)
	}
	skill, err := os.ReadFile(filepath.Join(dir, "skills", "getting-started", "SKILL.md"))
	if err != nil {
		t.Fatalf("skills example missing SKILL.md: %v", err)
	}
	for _, want := range []string{"name: getting-started", "description:"} {
		if !strings.Contains(string(skill), want) {
			t.Errorf("SKILL.md missing %q", want)
		}
	}
}

func TestInitRejectsUnknownExample(t *testing.T) {
	var buf bytes.Buffer
	err := runInit(&buf, t.TempDir(), "demo", "bogus", false)
	if err == nil || !strings.Contains(err.Error(), "--example") {
		t.Errorf("unknown example should error naming --example, got %v", err)
	}
}

func TestInitRejectsUnsafeName(t *testing.T) {
	// An explicit --name lands verbatim in the YAML template; anything
	// that could break parsing or inject keys must be rejected up front.
	for _, name := range []string{"demo: v1", "a#b", "x\ny", "a b"} {
		var buf bytes.Buffer
		err := runInit(&buf, t.TempDir(), name, "minimal", false)
		if err == nil || !strings.Contains(err.Error(), "--name") {
			t.Errorf("runInit(name=%q) should error naming --name, got %v", name, err)
		}
	}
}

func TestDefaultStackName(t *testing.T) {
	tests := []struct {
		dir  string
		want string
	}{
		{"/tmp/My Stack", "my-stack"},
		{"/tmp/api_v2", "api_v2"},
		{"/tmp/demo", "demo"},
	}
	for _, tt := range tests {
		if got := defaultStackName(tt.dir); got != tt.want {
			t.Errorf("defaultStackName(%q) = %q, want %q", tt.dir, got, tt.want)
		}
	}
}

func TestMissingStackFileHint(t *testing.T) {
	_, err := config.LoadStack("/no/such/stack.yaml")
	if err == nil {
		t.Fatal("expected load error")
	}
	if !isMissingStackFile(err) {
		t.Errorf("isMissingStackFile should recognize %v", err)
	}
	var buf bytes.Buffer
	printCLIError(&buf, nil, err)
	if !strings.Contains(buf.String(), "gridctl init") {
		t.Errorf("missing-stack error should hint at gridctl init:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "Usage:") {
		t.Errorf("runtime error must not dump usage:\n%s", buf.String())
	}
}
