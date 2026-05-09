package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setTempHomeAgent(t *testing.T) string {
	t.Helper()
	orig := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", orig) })
	dir := t.TempDir()
	os.Setenv("HOME", dir)
	return dir
}

func resetAgentFlagsForTest() {
	agentValidateFormat = "text"
	agentBuildFormat = "text"
	agentBuildOutDir = ""
}

func writeRegistrySkill(t *testing.T, home, name, body, handlerLang string) {
	t.Helper()
	dir := filepath.Join(home, ".gridctl", "registry", "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	skillMD := "---\nname: " + name + "\ndescription: fixture\nstate: active\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if handlerLang == "ts" {
		if err := os.WriteFile(filepath.Join(dir, "skill.ts"), []byte(body), 0o644); err != nil {
			t.Fatalf("write skill.ts: %v", err)
		}
	}
	if handlerLang == "go" {
		if err := os.WriteFile(filepath.Join(dir, "skill.go"), []byte(body), 0o644); err != nil {
			t.Fatalf("write skill.go: %v", err)
		}
	}
}

func TestRunAgentValidate_TS(t *testing.T) {
	home := setTempHomeAgent(t)
	t.Cleanup(resetAgentFlagsForTest)

	body := "export default async function (input) { return { ok: true }; }"
	writeRegistrySkill(t, home, "valid-ts", body, "ts")

	if err := runAgentValidate("valid-ts"); err != nil {
		t.Fatalf("validate clean TS skill: %v", err)
	}
}

func TestRunAgentValidate_TSTranspileFailure(t *testing.T) {
	home := setTempHomeAgent(t)
	t.Cleanup(resetAgentFlagsForTest)

	// Intentional syntax error
	body := "export default async function (input { return; }"
	writeRegistrySkill(t, home, "broken-ts", body, "ts")

	err := runAgentValidate("broken-ts")
	if err == nil {
		t.Fatal("expected error for broken TS handler")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("expected 'invalid' in error, got: %v", err)
	}
}

func TestRunAgentBuild_TSWritesManifest(t *testing.T) {
	home := setTempHomeAgent(t)
	t.Cleanup(resetAgentFlagsForTest)

	body := "export default async function (input) { return { ok: true }; }"
	writeRegistrySkill(t, home, "build-ts", body, "ts")

	if err := runAgentBuild("build-ts"); err != nil {
		t.Fatalf("build TS skill: %v", err)
	}

	skillDir := filepath.Join(home, ".gridctl", "registry", "skills", "build-ts")
	manifestPath := filepath.Join(skillDir, "dist", "manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Errorf("manifest not written: %v", err)
	}
	jsPath := filepath.Join(skillDir, "dist", "skill.js")
	if _, err := os.Stat(jsPath); err != nil {
		t.Errorf("transpiled JS not written: %v", err)
	}
}

func TestRunAgentBuild_GoIsDeferred(t *testing.T) {
	home := setTempHomeAgent(t)
	t.Cleanup(resetAgentFlagsForTest)

	writeRegistrySkill(t, home, "build-go", "package main\n", "go")

	err := runAgentBuild("build-go")
	if err == nil {
		t.Fatal("expected deferred error for Go skill")
	}
	if !strings.Contains(err.Error(), "Phase H") {
		t.Errorf("expected 'Phase H' note in error, got: %v", err)
	}
}

func TestRunAgentBuild_MarkdownOnlyReturnsError(t *testing.T) {
	home := setTempHomeAgent(t)
	t.Cleanup(resetAgentFlagsForTest)

	writeRegistrySkill(t, home, "md-only", "", "")

	err := runAgentBuild("md-only")
	if err == nil {
		t.Fatal("expected error for markdown-only skill")
	}
}
