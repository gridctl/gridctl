package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/agent/dev/scaffold"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/spf13/pflag"
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

func resetAgentInitFlagsForTest(t *testing.T) {
	t.Helper()
	agentInitName = "hello-ts"
	agentInitDir = ""
	agentInitForce = false
	agentInitFormat = "text"
	agentInitLang = "ts"
	agentInitPromptOnly = false
	// Reset cobra's "changed" tracking so prior tests' Set() calls do
	// not bleed across invocations.
	agentInitCmd.Flags().VisitAll(func(f *pflag.Flag) { f.Changed = false })
	agentInitCmd.SetOut(io.Discard)
	agentInitCmd.SetErr(io.Discard)
}

// TestRunAgentInit_PromptOnlyFlavor scaffolds a prompt-only skill via
// the cmd-level runAgentInit function and verifies registry.Store.Load
// picks up the result — proving the flag plumbing wires through to a
// loadable skill on disk.
func TestRunAgentInit_PromptOnlyFlavor(t *testing.T) {
	resetAgentInitFlagsForTest(t)
	t.Cleanup(func() { resetAgentInitFlagsForTest(t) })

	regRoot := t.TempDir()
	skillDir := filepath.Join(regRoot, "skills", "demo-prompt")

	agentInitName = "demo-prompt"
	if err := agentInitCmd.Flags().Set("prompt-only", "true"); err != nil {
		t.Fatalf("set prompt-only: %v", err)
	}
	if err := runAgentInit(agentInitCmd, []string{skillDir}); err != nil {
		t.Fatalf("runAgentInit: %v", err)
	}

	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md not written: %v", err)
	}
	for _, name := range []string{"skill.ts", "skill.go", "agent.json"} {
		if _, err := os.Stat(filepath.Join(skillDir, name)); !os.IsNotExist(err) {
			t.Errorf("expected no %s for prompt-only flavor (err=%v)", name, err)
		}
	}

	store := registry.NewStore(regRoot)
	if err := store.Load(); err != nil {
		t.Fatalf("registry Load after prompt-only scaffold: %v", err)
	}
	sk, err := store.GetSkill("demo-prompt")
	if err != nil {
		t.Fatalf("GetSkill: %v", err)
	}
	if sk.HandlerLanguage != "" {
		t.Errorf("expected empty HandlerLanguage (prompt-only), got %q", sk.HandlerLanguage)
	}
	if sk.State != registry.StateActive {
		t.Errorf("expected active state, got %s", sk.State)
	}
}

func TestRunAgentInit_DefaultIsTSAndUnchanged(t *testing.T) {
	resetAgentInitFlagsForTest(t)
	t.Cleanup(func() { resetAgentInitFlagsForTest(t) })

	dir := t.TempDir()
	agentInitName = "demo-ts"
	if err := runAgentInit(agentInitCmd, []string{dir}); err != nil {
		t.Fatalf("runAgentInit (default): %v", err)
	}
	for _, name := range []string{"SKILL.md", "skill.ts", "agent.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected %s on disk for TS default scaffold: %v", name, err)
		}
	}
}

func TestRunAgentInit_PromptOnlyAndLangAreMutuallyExclusive(t *testing.T) {
	resetAgentInitFlagsForTest(t)
	t.Cleanup(func() { resetAgentInitFlagsForTest(t) })

	dir := t.TempDir()
	if err := agentInitCmd.Flags().Set("prompt-only", "true"); err != nil {
		t.Fatalf("set prompt-only: %v", err)
	}
	if err := agentInitCmd.Flags().Set("lang", "go"); err != nil {
		t.Fatalf("set lang: %v", err)
	}
	err := runAgentInit(agentInitCmd, []string{dir})
	if err == nil {
		t.Fatal("expected error when --prompt-only and --lang are combined")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected mutually-exclusive error, got: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("expected empty dir after pre-flight error, got %d entries", len(entries))
	}
}

func TestRunAgentInit_LangGoIsDeferredToPhase2(t *testing.T) {
	resetAgentInitFlagsForTest(t)
	t.Cleanup(func() { resetAgentInitFlagsForTest(t) })

	dir := t.TempDir()
	if err := agentInitCmd.Flags().Set("lang", "go"); err != nil {
		t.Fatalf("set lang: %v", err)
	}
	err := runAgentInit(agentInitCmd, []string{dir})
	if err == nil {
		t.Fatal("expected error for --lang go in Phase 1 (Phase 2 wires the body)")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("expected 'not yet implemented' note, got: %v", err)
	}
}

func TestRunAgentInit_UnknownLangRejected(t *testing.T) {
	resetAgentInitFlagsForTest(t)
	t.Cleanup(func() { resetAgentInitFlagsForTest(t) })

	dir := t.TempDir()
	if err := agentInitCmd.Flags().Set("lang", "rust"); err != nil {
		t.Fatalf("set lang: %v", err)
	}
	err := runAgentInit(agentInitCmd, []string{dir})
	if err == nil {
		t.Fatal("expected error for --lang rust")
	}
	if !strings.Contains(err.Error(), "unsupported --lang") {
		t.Errorf("expected unsupported-lang error, got: %v", err)
	}
}

// Sanity: the scaffold library's prompt-only output is byte-identical
// regardless of who invokes it (CLI vs library). Locks in the contract
// the agent init command exercises.
func TestScaffoldLibraryPromptFlavorReproducible(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	if _, err := scaffold.Scaffold(dirA, scaffold.Options{Language: "prompt", SkillName: "x"}); err != nil {
		t.Fatalf("Scaffold A: %v", err)
	}
	if _, err := scaffold.Scaffold(dirB, scaffold.Options{Language: "prompt", SkillName: "x"}); err != nil {
		t.Fatalf("Scaffold B: %v", err)
	}
	a, _ := os.ReadFile(filepath.Join(dirA, "SKILL.md"))
	b, _ := os.ReadFile(filepath.Join(dirB, "SKILL.md"))
	if string(a) != string(b) {
		t.Errorf("prompt-only SKILL.md is not deterministic")
	}
}
