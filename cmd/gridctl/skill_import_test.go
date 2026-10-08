package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSkillImport_ExitContract(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	var stdout, stderr bytes.Buffer
	if code := runSkillImport(context.Background(), &stdout, &stderr, "nope", skillImportConfig{}); code != 1 {
		t.Fatalf("unknown client code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "supported: agents, claude-code, opencode") {
		t.Fatalf("stderr = %s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runSkillImport(context.Background(), &stdout, &stderr, "claude-code", skillImportConfig{DryRun: true, Kind: "skill,agent"}); code != 0 {
		t.Fatalf("empty dry-run code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "no skills or agents found") {
		t.Fatalf("stdout = %s", stdout.String())
	}

	skillRoot := filepath.Join(home, ".claude", "skills", "pcap-analysis")
	if err := os.MkdirAll(skillRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte("---\nname: pcap-analysis\ndescription: Analyze packets\n---\n\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runSkillImport(context.Background(), &stdout, &stderr, "claude-code", skillImportConfig{Kind: "skill,agent"}); code != 1 {
		t.Fatalf("no tty code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--all") {
		t.Fatalf("stderr = %s", stderr.String())
	}

	skillDir := filepath.Join(home, ".config", "opencode", "agents")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "review.md"), []byte("---\nmode: primary\n---\n\nReview.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code := runSkillImport(context.Background(), &stdout, &stderr, "opencode", skillImportConfig{
		Kind: "skill,agent", All: true, Format: "json",
	})
	if code != 0 {
		t.Fatalf("policy skip code = %d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = runSkillImport(context.Background(), &stdout, &stderr, "opencode", skillImportConfig{
		Kind: "agent", Select: []string{"review"},
	})
	if code != 1 {
		t.Fatalf("selected skip code = %d stdout=%s", code, stdout.String())
	}
}
