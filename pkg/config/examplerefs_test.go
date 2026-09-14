package config

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestExampleStacks_PinnedOrExcepted(t *testing.T) {
	root := repoRoot(t)
	exceptions := loadReferenceExceptions(t, filepath.Join(root, "examples", "reference-exceptions.txt"))
	var stacks []string
	err := filepath.WalkDir(filepath.Join(root, "examples"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".yaml" {
			return nil
		}
		base := filepath.Base(path)
		switch base {
		case "skills.yaml", "gridctl-pack.yaml", "gateway-remote.yaml":
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if strings.Contains(rel, "/model-policy/") {
			return nil
		}
		stacks = append(stacks, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(stacks) == 0 {
		t.Fatal("no example stacks found")
	}
	for path := range exceptions {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Errorf("exception path missing: %s", path)
		}
	}
	for _, rel := range stacks {
		indexed, err := ParseStackIndex(context.Background(), filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("%s: parse: %v", rel, err)
			continue
		}
		for _, issue := range DiagnoseMutableRefs(indexed) {
			if !MaintenanceRefFinding(issue) {
				continue
			}
			if referenceExceptionAllows(exceptions, rel, issue.Field) {
				continue
			}
			t.Errorf("%s: unpinned or unassessed selector at %s: %s", rel, issue.Field, issue.Message)
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

func loadReferenceExceptions(t *testing.T, path string) map[string]map[string]bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		file, field, _ := strings.Cut(line, " ")
		file = strings.TrimSpace(file)
		field = strings.TrimSpace(field)
		if out[file] == nil {
			out[file] = map[string]bool{}
		}
		if field == "" {
			out[file]["*"] = true
			continue
		}
		out[file][field] = true
	}
	if len(out) == 0 {
		t.Fatal("no exceptions loaded")
	}
	return out
}

func referenceExceptionAllows(exceptions map[string]map[string]bool, file, field string) bool {
	fields, ok := exceptions[file]
	if !ok {
		return false
	}
	return fields["*"] || fields[field]
}

func TestMaintenanceRefFinding(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	tests := []struct {
		name     string
		stack    *Stack
		field    string
		enforced bool
		excepted bool
	}{
		{
			name:     "invalid digest",
			stack:    &Stack{MCPServers: []MCPServer{{Name: "s", Image: "alpine@sha256:abcd"}}},
			field:    "mcp-servers[0].image",
			enforced: true,
		},
		{
			name:     "unsupported option",
			stack:    &Stack{MCPServers: []MCPServer{{Name: "s", Command: []string{"npx", "--prefix", "/tmp", "pkg@1.0.0"}}}},
			field:    "mcp-servers[0].command",
			enforced: true,
		},
		{
			name:     "unsupported launcher",
			stack:    &Stack{MCPServers: []MCPServer{{Name: "s", Command: []string{"npm", "exec", "pkg@1.2.3"}}}},
			field:    "mcp-servers[0].command",
			enforced: true,
		},
		{
			name:     "accepted pin",
			stack:    &Stack{MCPServers: []MCPServer{{Name: "s", Image: "alpine:3.22@" + digest}}},
			field:    "mcp-servers[0].image",
			enforced: false,
		},
		{
			name:     "placeholder",
			stack:    &Stack{MCPServers: []MCPServer{{Name: "s", Image: "ghcr.io/org/ai-mcp-server:latest"}}},
			field:    "mcp-servers[0].image",
			enforced: true,
			excepted: true,
		},
		{
			name:     "host wrapper",
			stack:    &Stack{MCPServers: []MCPServer{{Name: "s", Command: []string{"sh", "-c", "sleep 1"}}}},
			field:    "mcp-servers[0].command",
			enforced: false,
		},
		{
			name:     "local path",
			stack:    &Stack{MCPServers: []MCPServer{{Name: "s", Command: []string{"npx", "./tool"}}}},
			field:    "mcp-servers[0].command",
			enforced: false,
		},
		{
			name:     "unrelated warning ignored",
			stack:    &Stack{MCPServers: []MCPServer{{Name: "s", Image: "alpine:3.22@" + digest}}},
			field:    "skills",
			enforced: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "unrelated warning ignored" {
				issue := ValidationIssue{Field: "skills", Message: "model-preference-unknown-alias: x", Severity: SeverityWarning}
				if MaintenanceRefFinding(issue) {
					t.Fatal("unrelated warning must not be enforced")
				}
				return
			}
			issues := DiagnoseMutableRefs(tt.stack)
			var found ValidationIssue
			var ok bool
			for _, issue := range issues {
				if issue.Field == tt.field {
					found = issue
					ok = true
					break
				}
			}
			if tt.enforced && !ok {
				t.Fatalf("missing finding for %s", tt.field)
			}
			if !tt.enforced && ok && MaintenanceRefFinding(found) {
				t.Fatalf("unexpected enforced finding: %+v", found)
			}
			if tt.enforced && !MaintenanceRefFinding(found) {
				t.Fatalf("not enforced: %+v", found)
			}
			if tt.excepted {
				exceptions := map[string]map[string]bool{"stack.yaml": {tt.field: true}}
				if !referenceExceptionAllows(exceptions, "stack.yaml", tt.field) {
					t.Fatal("expected exception to allow placeholder")
				}
			}
		})
	}
}

func TestREADMESetupSnippet_PinsMCPRemote(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "mcp-remote@0.14.2") {
		t.Fatal("README Claude Desktop setup must pin mcp-remote to the reviewed release")
	}
	if strings.Contains(text, `"args": ["-y", "mcp-remote",`) {
		t.Fatal("README still has an unpinned mcp-remote selector")
	}
}

func TestCheckExampleRefsScript(t *testing.T) {
	script := filepath.Join(repoRoot(t), "scripts", "check-example-refs.sh")
	digest := "sha256:" + strings.Repeat("a", 64)
	pinnedJSON := `{"valid":true,"errorCount":0,"warningCount":0,"issues":[]}`
	invalidDigestJSON := `{"valid":true,"errorCount":0,"warningCount":0,"issues":[{"field":"mcp-servers[0].image","message":"reference-not-assessed: digest syntax is invalid; it was not classified.","severity":"info"}]}`
	mutableJSON := `{"valid":true,"errorCount":0,"warningCount":1,"issues":[{"field":"mcp-servers[0].image","message":"mutable-image-reference: version tags can change; use a reviewed digest. Publisher trust and platform support were not checked.","severity":"warning"}]}`
	unrelatedJSON := `{"valid":true,"errorCount":0,"warningCount":1,"issues":[{"field":"skills","message":"model-preference-unknown-alias: leftover","severity":"warning"}]}`
	errorJSON := `{"valid":false,"errorCount":1,"warningCount":0,"issues":[{"field":"name","message":"name is required","severity":"error"}]}`
	unsupportedJSON := `{"valid":true,"errorCount":0,"warningCount":0,"issues":[{"field":"mcp-servers[0].command","message":"reference-not-assessed: launcher options are unsupported; it was not classified.","severity":"info"}]}`
	wrapperJSON := `{"valid":true,"errorCount":0,"warningCount":0,"issues":[{"field":"mcp-servers[0].command","message":"reference-not-assessed: command wrapper is unsupported; it was not classified.","severity":"info"}]}`

	tests := []struct {
		name       string
		stdout     string
		rc         int
		exceptions string
		wantFail   bool
	}{
		{name: "accepted pin", stdout: pinnedJSON, rc: 0, exceptions: "# none\n", wantFail: false},
		{name: "invalid digest", stdout: invalidDigestJSON, rc: 0, exceptions: "# none\n", wantFail: true},
		{name: "invalid digest excepted", stdout: invalidDigestJSON, rc: 0, exceptions: "examples/stack.yaml mcp-servers[0].image\n", wantFail: false},
		{name: "unsupported option", stdout: unsupportedJSON, rc: 0, exceptions: "# none\n", wantFail: true},
		{name: "host wrapper permitted", stdout: wrapperJSON, rc: 0, exceptions: "# none\n", wantFail: false},
		{name: "permitted placeholder", stdout: mutableJSON, rc: 2, exceptions: "examples/stack.yaml mcp-servers[0].image\n", wantFail: false},
		{name: "unexcepted mutable", stdout: mutableJSON, rc: 2, exceptions: "# none\n", wantFail: true},
		{name: "unrelated warning", stdout: unrelatedJSON, rc: 2, exceptions: "# none\n", wantFail: false},
		{name: "validation errors", stdout: errorJSON, rc: 1, exceptions: "# none\n", wantFail: true},
		{name: "malformed json", stdout: "not-json", rc: 0, exceptions: "# none\n", wantFail: true},
		{name: "unexpected exit", stdout: pinnedJSON, rc: 3, exceptions: "# none\n", wantFail: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			examples := filepath.Join(root, "examples")
			if err := os.MkdirAll(examples, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(examples, "stack.yaml"), []byte("name: t\nmcp-servers:\n  - name: s\n    image: alpine:3.22@"+digest+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(examples, "reference-exceptions.txt"), []byte(tt.exceptions), 0o600); err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(root, "gridctl")
			scriptBody := "#!/bin/sh\ncat <<'EOF'\n" + tt.stdout + "\nEOF\nexit " + strconv.Itoa(tt.rc) + "\n"
			if err := os.WriteFile(bin, []byte(scriptBody), 0o700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", script, bin)
			cmd.Env = append(os.Environ(), "GRIDCTL_CHECK_ROOT="+root)
			out, err := cmd.CombinedOutput()
			if tt.wantFail && err == nil {
				t.Fatalf("expected failure\n%s", out)
			}
			if !tt.wantFail && err != nil {
				t.Fatalf("unexpected failure: %v\n%s", err, out)
			}
		})
	}
}
