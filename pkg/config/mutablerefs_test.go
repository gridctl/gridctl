package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/depcheck"
)

func TestDiagnoseMutableRefs_Table(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	stack := &Stack{
		MCPServers: []MCPServer{
			{Name: "tagged", Image: "nginx:1.21.0"},
			{Name: "digest", Image: "nginx@" + digest},
			{Name: "port", Image: "localhost:5000/foo:tag"},
			{Name: "tagdigest", Image: "nginx:1.21.0@" + digest},
			{Name: "baddigest", Image: "nginx@sha256:abcd"},
			{Name: "variable", Image: "${var:IMAGE}"},
			{Name: "npx-exact", Command: []string{"npx", "-y", "@scope/pkg@1.2.3"}},
			{Name: "npx-float", Command: []string{"npx", "-y", "pkg@latest"}},
			{Name: "npx-partial", Command: []string{"npx", "-y", "@scope/pkg@1.2.x"}},
			{Name: "npx-missing", Command: []string{"npx", "pkg"}},
			{Name: "uvx-from", Command: []string{"uvx", "--from", "pkg==1.2.3", "pkg"}},
			{Name: "uvx-range", Command: []string{"uvx", "pkg>=1.0"}},
			{Name: "wrapper", Command: []string{"npm", "exec", "pkg@1.2.3"}},
			{Name: "local", Command: []string{"npx", "./tool"}},
			{Name: "unknown-opt", Command: []string{"npx", "--prefix", "/tmp", "pkg@1.0.0"}},
			{Name: "host", Command: []string{"sh", "-c", "echo s3cret-token"}},
			{Name: "dynamic", Command: []string{"${var:CMD}", "pkg"}},
			{Name: "local-bin", Command: []string{"../_mock-servers/local-stdio-server/mock-stdio-server"}},
			{Name: "npx-multi", Command: []string{"npx", "--package=unpinned", "--package=pkg@1.2.3", "pkg"}},
		},
		Resources: []Resource{
			{Name: "db", Image: "postgres:16"},
			{Name: "pinned", Image: "postgres:16@" + digest},
		},
	}
	issues := DiagnoseMutableRefs(stack)
	byField := map[string]ValidationIssue{}
	for _, issue := range issues {
		if _, exists := byField[issue.Field]; exists {
			t.Errorf("duplicate field %q", issue.Field)
		}
		byField[issue.Field] = issue
	}

	wantWarn := []string{
		"mcp-servers[0].image",
		"mcp-servers[2].image",
		"mcp-servers[7].command",
		"mcp-servers[8].command",
		"mcp-servers[9].command",
		"mcp-servers[11].command",
		"mcp-servers[18].command",
		"resources[0].image",
	}
	for _, field := range wantWarn {
		issue, ok := byField[field]
		if !ok {
			t.Errorf("missing warning for %s", field)
			continue
		}
		if issue.Severity != SeverityWarning {
			t.Errorf("%s severity = %s", field, issue.Severity)
		}
		if !strings.HasPrefix(issue.Message, prefixMutableImage) && !strings.HasPrefix(issue.Message, prefixMutablePkg) {
			t.Errorf("%s message = %q", field, issue.Message)
		}
	}
	wantInfo := []string{
		"mcp-servers[4].image",
		"mcp-servers[5].image",
		"mcp-servers[12].command",
		"mcp-servers[13].command",
		"mcp-servers[14].command",
		"mcp-servers[15].command",
		"mcp-servers[16].command",
		"mcp-servers[17].command",
	}
	for _, field := range wantInfo {
		issue, ok := byField[field]
		if !ok {
			t.Errorf("missing info for %s", field)
			continue
		}
		if issue.Severity != SeverityInfo {
			t.Errorf("%s severity = %s", field, issue.Severity)
		}
		if !strings.HasPrefix(issue.Message, prefixNotAssessed) {
			t.Errorf("%s message = %q", field, issue.Message)
		}
	}
	for _, field := range []string{"mcp-servers[1].image", "mcp-servers[3].image", "mcp-servers[6].command", "mcp-servers[10].command", "resources[1].image"} {
		if _, ok := byField[field]; ok {
			t.Errorf("pinned selector %s should not emit a finding", field)
		}
	}
	if issue := byField["mcp-servers[15].command"]; !strings.Contains(issue.Message, "command wrapper is unsupported") {
		t.Errorf("host wrapper message = %q", issue.Message)
	}
	if issue := byField["mcp-servers[12].command"]; !strings.Contains(issue.Message, "launcher is unsupported") {
		t.Errorf("npm wrapper message = %q", issue.Message)
	}

	again := DiagnoseMutableRefs(stack)
	if len(again) != len(issues) {
		t.Fatalf("second call length %d, want %d", len(again), len(issues))
	}
	for i := range issues {
		if again[i] != issues[i] {
			t.Fatalf("order drifted at %d: %+v vs %+v", i, again[i], issues[i])
		}
	}
	var seenRes bool
	for _, issue := range issues {
		if strings.HasPrefix(issue.Field, "resources") {
			seenRes = true
		} else if seenRes {
			t.Fatalf("mcp-servers finding after resources: %s", issue.Field)
		}
		if strings.Contains(issue.Message, "s3cret-token") || strings.Contains(issue.Field, "s3cret") {
			t.Errorf("finding leaked secret material: %+v", issue)
		}
		if strings.Contains(issue.Message, "${var:") || strings.Contains(issue.Message, "nginx:1.21") {
			t.Errorf("finding leaked selector text: %+v", issue)
		}
	}
}

func TestDiagnoseMutableRefs_DoesNotRewriteOrExpand(t *testing.T) {
	stack := &Stack{
		MCPServers: []MCPServer{{
			Name:    "s",
			Image:   "${var:IMAGE:-alpine:latest}",
			Command: []string{"npx", "-y", "${var:PKG}"},
			Env:     map[string]string{"TOKEN": "s3cret-token"},
		}},
	}
	copyImage := stack.MCPServers[0].Image
	issues := DiagnoseMutableRefs(stack)
	if stack.MCPServers[0].Image != copyImage {
		t.Fatal("rewrote image")
	}
	if stack.MCPServers[0].Env["TOKEN"] != "s3cret-token" {
		t.Fatal("rewrote env")
	}
	if len(issues) != 2 {
		t.Fatalf("issues = %d, want 2", len(issues))
	}
	for _, issue := range issues {
		if issue.Severity != SeverityInfo {
			t.Errorf("variable selector severity = %s", issue.Severity)
		}
		if strings.Contains(issue.Message, "s3cret-token") || strings.Contains(issue.Message, "alpine:latest") {
			t.Errorf("leaked value: %q", issue.Message)
		}
	}
}

func TestDiagnoseMutableRefs_NilAndEmpty(t *testing.T) {
	if DiagnoseMutableRefs(nil) != nil {
		t.Fatal("nil stack")
	}
	if issues := DiagnoseMutableRefs(&Stack{}); len(issues) != 0 {
		t.Fatalf("empty stack issues = %#v", issues)
	}
}

func TestAppendMutableRefIssues_CountsWarningsOnly(t *testing.T) {
	result := &ValidationResult{Valid: true}
	AppendMutableRefIssues(result, []ValidationIssue{
		{Field: "a", Message: msgMutableImage, Severity: SeverityWarning},
		{Field: "b", Message: prefixNotAssessed + "x", Severity: SeverityInfo},
	})
	if !result.Valid {
		t.Fatal("must not change validity")
	}
	if result.WarningCount != 1 {
		t.Fatalf("WarningCount = %d", result.WarningCount)
	}
	if result.ErrorCount != 0 {
		t.Fatalf("ErrorCount = %d", result.ErrorCount)
	}
	if len(result.Issues) != 2 {
		t.Fatalf("Issues = %d", len(result.Issues))
	}
}

func TestValidateWithIssues_DoesNotIncludeMutableRefFindings(t *testing.T) {
	stack := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		Gateway: &GatewayConfig{Auth: &AuthConfig{Type: "bearer", Token: "secret"}},
		MCPServers: []MCPServer{
			{Name: "s1", Image: "alpine:latest", Port: 3000},
		},
	}
	result := ValidateWithIssues(stack)
	for _, issue := range result.Issues {
		if strings.Contains(issue.Message, "mutable-image-reference") || strings.Contains(issue.Message, "reference-not-assessed") {
			t.Fatalf("default validation included advisory ref finding: %+v", issue)
		}
	}
	if result.WarningCount != 0 {
		t.Fatalf("WarningCount = %d", result.WarningCount)
	}
}

func TestValidateStackFile_ExpansionUnchanged(t *testing.T) {
	t.Setenv("MUTABLEREF_IMAGE", "alpine:latest")
	path := filepath.Join(t.TempDir(), "stack.yaml")
	content := `name: test
network:
  name: net
mcp-servers:
  - name: s1
    image: ${MUTABLEREF_IMAGE}
    port: 3000
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	stack, result, err := ValidateStackFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if stack.MCPServers[0].Image != "alpine:latest" {
		t.Fatalf("expanded image = %q", stack.MCPServers[0].Image)
	}
	for _, issue := range result.Issues {
		if strings.Contains(issue.Message, "mutable-image-reference") {
			t.Fatalf("ValidateStackFile ran mutable-ref checks: %+v", issue)
		}
	}
}

func TestDiagnoseMutableRefs_UsesRawSelectorsNotExpansion(t *testing.T) {
	t.Setenv("MUTABLEREF_RAW", "alpine:latest")
	path := filepath.Join(t.TempDir(), "stack.yaml")
	content := `name: test
mcp-servers:
  - name: s1
    image: ${MUTABLEREF_RAW}
    port: 3000
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	indexed, err := ParseStackIndex(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if indexed.MCPServers[0].Image != "${MUTABLEREF_RAW}" {
		t.Fatalf("raw image = %q", indexed.MCPServers[0].Image)
	}
	issues := DiagnoseMutableRefs(indexed)
	if len(issues) != 1 || issues[0].Severity != SeverityInfo {
		t.Fatalf("issues = %#v", issues)
	}
	if issues[0].Field != "mcp-servers[0].image" {
		t.Fatalf("field = %q", issues[0].Field)
	}
}

func TestFindingFor_PinnedHasNoIssue(t *testing.T) {
	issue, ok := findingFor("mcp-servers[0].image", depcheck.Result{Kind: depcheck.KindImage, Status: depcheck.StatusPinned, Reason: "digest"})
	if ok {
		t.Fatalf("unexpected issue %+v", issue)
	}
}

func TestCoverageLimitationMessage(t *testing.T) {
	msg := CoverageLimitationMessage()
	if !strings.HasPrefix(msg, "reference-coverage:") {
		t.Fatalf("message = %q", msg)
	}
	if strings.Contains(msg, "authenticated") && strings.Contains(strings.ToLower(msg), "complete coverage") {
		t.Fatal("must not claim complete coverage")
	}
}

func TestMutableRefFindingJSONShape(t *testing.T) {
	issue := ValidationIssue{
		Field:    "resources[0].image",
		Message:  msgMutableImage,
		Severity: SeverityWarning,
	}
	raw, err := json.Marshal(issue)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["field"] != "resources[0].image" || got["severity"] != "warning" {
		t.Fatalf("json = %s", raw)
	}
	if !strings.HasPrefix(got["message"], "mutable-image-reference:") {
		t.Fatalf("message = %q", got["message"])
	}
	info := ValidationIssue{
		Field:    "mcp-servers[0].command",
		Message:  prefixNotAssessed + "selector contains a variable; it was not classified.",
		Severity: SeverityInfo,
	}
	raw, err = json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["severity"] != "info" {
		t.Fatalf("info severity = %q", got["severity"])
	}
}
