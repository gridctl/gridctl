package pack

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/skills"
)

func TestExamplePackManifest_ParsesAndRequiresFloor(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "examples", "portable-pack", "gridctl-pack.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Hooks) != 1 || m.Hooks[0] != "block-force-push" {
		t.Fatalf("hooks = %v", m.Hooks)
	}
	if m.Requires == nil || m.Requires.Gridctl != ">=1.0.0-rc.5" {
		t.Fatalf("requires = %+v", m.Requires)
	}
	err = config.CheckRequires(m.Requires.Gridctl)
	var syntax *config.RequiresError
	if errors.As(err, &syntax) && syntax.Syntax {
		t.Fatalf("CheckRequires refused the constraint syntax: %v", err)
	}

	hookDir := filepath.Join(root, "examples", "portable-pack", "hooks", "block-force-push")
	raw, err := os.ReadFile(filepath.Join(hookDir, "hook.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := skills.ParseHookJSON(raw, hookDir); err != nil {
		t.Fatal(err)
	}
}

func TestExamplePackHook_DeniesForcePush(t *testing.T) {
	script := filepath.Join(repoRoot(t), "examples", "portable-pack", "hooks", "block-force-push", "scripts", "block-force-push.sh")
	cases := []struct {
		command string
		deny    bool
	}{
		{`git push --force`, true},
		{`git push origin main --force`, true},
		{`git push origin -f`, true},
		{`git push -f`, true},
		{`git push --force-with-lease`, false},
		{`git push origin main --force-if-includes`, false},
		{`git push origin main`, false},
		{`git status --force`, false},
	}
	for _, tc := range cases {
		cmd := exec.CommandContext(t.Context(), script)
		cmd.Stdin = strings.NewReader(`{"tool_input":{"command":"` + tc.command + `"}}`)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", tc.command, err, out)
		}
		denied := strings.Contains(string(out), `"permissionDecision":"deny"`)
		if denied != tc.deny {
			t.Fatalf("%s deny = %v, output %s", tc.command, denied, out)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
