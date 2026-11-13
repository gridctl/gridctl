package pack

import (
	"encoding/json"
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
	const reason = "git push --force is blocked by the network-eng pack"
	for _, tc := range cases {
		cmd := exec.CommandContext(t.Context(), script)
		cmd.Stdin = strings.NewReader(`{"tool_input":{"command":"` + tc.command + `"}}`)
		var stdout, stderr strings.Builder
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		code := 0
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else if err != nil {
			t.Fatalf("%s: %v", tc.command, err)
		}
		if !tc.deny {
			// Allow is exit 0 with no output: some clients deny on any
			// non-zero exit or on unexpected output.
			if code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
				t.Fatalf("%s: allow exited %d, stdout %q, stderr %q", tc.command, code, stdout.String(), stderr.String())
			}
			continue
		}
		if code != 2 {
			t.Fatalf("%s: deny exited %d, want 2", tc.command, code)
		}
		if strings.TrimSpace(stderr.String()) != reason {
			t.Fatalf("%s: stderr = %q, want the reason", tc.command, stderr.String())
		}
		var out struct {
			Decision           string `json:"decision"`
			Reason             string `json:"reason"`
			HookSpecificOutput struct {
				HookEventName            string `json:"hookEventName"`
				PermissionDecision       string `json:"permissionDecision"`
				PermissionDecisionReason string `json:"permissionDecisionReason"`
			} `json:"hookSpecificOutput"`
		}
		if strings.Count(strings.TrimSpace(stdout.String()), "\n") != 0 {
			t.Fatalf("%s: stdout must be one JSON object on one line: %q", tc.command, stdout.String())
		}
		if err := json.Unmarshal([]byte(stdout.String()), &out); err != nil {
			t.Fatalf("%s: stdout is not JSON: %v\n%s", tc.command, err, stdout.String())
		}
		if out.Decision != "deny" || out.Reason != reason ||
			out.HookSpecificOutput.HookEventName != "PreToolUse" ||
			out.HookSpecificOutput.PermissionDecision != "deny" ||
			out.HookSpecificOutput.PermissionDecisionReason != reason {
			t.Fatalf("%s: deny object = %+v", tc.command, out)
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
