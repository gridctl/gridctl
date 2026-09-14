package secreport

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPrivacyCanariesExcluded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	body := strings.Join([]string{
		`version: "1"`,
		`name: demo`,
		`gateway:`,
		`  auth:`,
		`    type: bearer`,
		`    token: canary-gateway-token`,
		`    header: X-Canary`,
		`mcp-servers:`,
		`  - name: fetch`,
		`    image: example/fetch:1`,
		`    command: ["canary-command"]`,
		`    env:`,
		`      PASSWORD: canary-env-value`,
		`    source:`,
		`      type: git`,
		`      url: "https://canary-user:canary-pass@evil.example/repo.git?token=canary-query#canary-frag"`,
		`      ref: main`,
	}, "\n")
	if err := os.WriteFile(path, []byte(body+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Load(context.Background(), SourceRef{Kind: SourceFile, Value: path}, &countingDoer{})
	if err != nil {
		t.Fatal(err)
	}
	raw := reportString(report)
	for _, canary := range []string{
		"canary-gateway-token", "X-Canary", "canary-command", "canary-env-value",
		"canary-user", "canary-pass", "canary-query", "canary-frag", "PASSWORD",
	} {
		if strings.Contains(raw, canary) {
			t.Fatalf("leaked %q", canary)
		}
	}
}

func TestSnapshotDiscardsStoredExplanationAndUnknownActions(t *testing.T) {
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	report := Assemble(now, Inputs{Source: SourceIdentity{Kind: SourceFile, Display: "stack.yaml"}, Stack: &StackView{Name: "demo", Gateway: &GatewayDeclView{}, SetMembers: map[string][]string{}}})
	if len(report.Checks) == 0 {
		t.Fatal("expected checks")
	}
	report.Checks[0].Explanation = "canary-free-text-error"
	report.Checks[0].Actions = []Action{{ID: "open", Label: "Open", Path: "https://evil.example"}}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseSnapshot(data)
	if err != nil {
		t.Fatal(err)
	}
	raw := reportString(parsed)
	if strings.Contains(raw, "canary-free-text-error") {
		t.Fatal("trusted stored explanation")
	}
	if strings.Contains(raw, "https://evil.example") {
		t.Fatal("arbitrary action destination survived")
	}
}

func TestIdentifierBoundNotRedaction(t *testing.T) {
	long := strings.Repeat("a", 200)
	got := SanitizeIdentifier(long)
	if len([]rune(got)) != identifierMaxRunes {
		t.Fatalf("len=%d", len([]rune(got)))
	}
}
