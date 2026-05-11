package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// TestRunAgentBuild_GoWindowsPlatformGate fakes goosProbe to confirm
// the Windows pre-flight error fires before the toolchain is invoked.
// Lets the platform gate be exercised on any host without skipping
// the test on non-Windows CI.
func TestRunAgentBuild_GoWindowsPlatformGate(t *testing.T) {
	home := setTempHomeAgent(t)
	t.Cleanup(resetAgentFlagsForTest)

	origProbe := goosProbe
	goosProbe = func() string { return "windows" }
	t.Cleanup(func() { goosProbe = origProbe })

	writeRegistrySkill(t, home, "build-go-windows", "package main\nfunc RegisterSkill() {}\n", "go")

	err := runAgentBuild("build-go-windows")
	if err == nil {
		t.Fatal("expected platform-unsupported error on Windows")
	}
	if !strings.Contains(err.Error(), "Linux or macOS") {
		t.Errorf("expected platform error mentioning Linux/macOS, got: %v", err)
	}
	if strings.Contains(err.Error(), "plugin.Open") || strings.Contains(err.Error(), "buildmode") {
		t.Errorf("toolchain output leaked through pre-flight gate: %v", err)
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

func TestRunAgentInit_LangGoWritesGoScaffold(t *testing.T) {
	resetAgentInitFlagsForTest(t)
	t.Cleanup(func() { resetAgentInitFlagsForTest(t) })

	dir := t.TempDir()
	if err := agentInitCmd.Flags().Set("lang", "go"); err != nil {
		t.Fatalf("set lang: %v", err)
	}
	if err := runAgentInit(agentInitCmd, []string{dir}); err != nil {
		t.Fatalf("runAgentInit(--lang go): %v", err)
	}
	for _, name := range []string{"SKILL.md", "skill.go", "skill_test.go"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected %s on disk: %v", name, err)
		}
	}
	for _, name := range []string{"skill.ts", "agent.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("expected no %s for go flavor, got err=%v", name, err)
		}
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

// findGridctlRepoRoot walks upward from this test source file until it
// hits the gridctl repo's go.mod. Used to wire a local replace
// directive into the scaffolded skill's go.mod for the build-path
// regression test, mirroring scaffold_go_compile_test.go.
func findGridctlRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		mod := filepath.Join(dir, "go.mod")
		if info, err := os.Stat(mod); err == nil && !info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found walking up from test source")
		}
		dir = parent
	}
}

// TestRunAgentBuild_GoCompilesPluginAndWritesManifest scaffolds a
// real Go skill into a temp registry, plumbs a self-contained go.mod
// with a local replace directive, and runs runAgentBuild end-to-end.
// Asserts that skill.so + manifest.json land on disk and the manifest
// carries handler="go", source_hash, go_version (matching
// runtime.Version), and go_mod_hash (matching a fresh hash of the
// resolved go.mod). Skipped under -short and on Windows (no plugin
// support) and when the go toolchain is not in PATH.
func TestRunAgentBuild_GoCompilesPluginAndWritesManifest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping go plugin build in -short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("Go plugins are not supported on Windows")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go toolchain not in PATH: %v", err)
	}

	// Capture the user's real GOMODCACHE before overriding HOME so
	// 'go mod tidy' inside the test reuses the warm shared cache
	// instead of writing into the soon-to-be-deleted temp HOME (the
	// module cache files are read-only — RemoveAll on the temp HOME
	// fails permissions on cleanup).
	modCacheCmd := exec.Command("go", "env", "GOMODCACHE")
	modCacheOut, err := modCacheCmd.Output()
	if err != nil {
		t.Fatalf("go env GOMODCACHE: %v", err)
	}
	modCache := strings.TrimSpace(string(modCacheOut))
	t.Setenv("GOMODCACHE", modCache)

	home := setTempHomeAgent(t)
	t.Cleanup(resetAgentFlagsForTest)

	skillDir := filepath.Join(home, ".gridctl", "registry", "skills", "build-go-real")
	if _, err := scaffold.Scaffold(skillDir, scaffold.Options{Language: "go", SkillName: "build-go-real"}); err != nil {
		t.Fatalf("Scaffold(go): %v", err)
	}

	repoRoot := findGridctlRepoRoot(t)
	goMod := "module buildgoreal\n\n" +
		"go 1.26\n\n" +
		"require github.com/gridctl/gridctl v0.0.0\n\n" +
		"replace github.com/gridctl/gridctl => " + repoRoot + "\n"
	goModPath := filepath.Join(skillDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte(goMod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = skillDir
	tidy.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOMODCACHE="+modCache)
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy:\n%s\nerr: %v", out, err)
	}

	if err := runAgentBuild("build-go-real"); err != nil {
		t.Fatalf("runAgentBuild(go): %v", err)
	}

	soPath := filepath.Join(skillDir, "dist", "skill.so")
	manifestPath := filepath.Join(skillDir, "dist", "manifest.json")
	if _, err := os.Stat(soPath); err != nil {
		t.Errorf("skill.so not written: %v", err)
	}
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("manifest.json not written: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if got := manifest["handler"]; got != "go" {
		t.Errorf("manifest handler = %v, want 'go'", got)
	}
	if got := manifest["handler_path"]; got != "skill.so" {
		t.Errorf("manifest handler_path = %v, want 'skill.so'", got)
	}
	if got := manifest["go_version"]; got != runtime.Version() {
		t.Errorf("manifest go_version = %v, want %s", got, runtime.Version())
	}
	if _, ok := manifest["source_hash"].(string); !ok {
		t.Error("manifest source_hash missing or not a string")
	}
	freshHash, ok := manifest["go_mod_hash"].(string)
	if !ok {
		t.Fatal("manifest go_mod_hash missing or not a string")
	}
	wantBytes, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("re-read go.mod: %v", err)
	}
	wantSum := sha256.Sum256(wantBytes)
	if want := hex.EncodeToString(wantSum[:]); want != freshHash {
		t.Errorf("go_mod_hash = %s, want %s", freshHash, want)
	}
}

// TestGoModHashFor_WalksUp confirms the go.mod walk-up resolves a
// go.mod two parents above the handler and hashes its bytes.
func TestGoModHashFor_WalksUp(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "skill")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := []byte("module example\n\ngo 1.26\n")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), body, 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	handler := filepath.Join(deep, "skill.go")
	if err := os.WriteFile(handler, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write skill.go: %v", err)
	}

	got, ok := goModHashFor(handler)
	if !ok {
		t.Fatal("goModHashFor returned ok=false")
	}
	want := sha256.Sum256(body)
	if got != hex.EncodeToString(want[:]) {
		t.Errorf("hash = %s, want %s", got, hex.EncodeToString(want[:]))
	}
}

// TestGoModHashFor_NoGoMod confirms that a handler with no go.mod
// anywhere in the parent chain returns ok=false (so the caller can
// log+skip rather than fail the build).
func TestGoModHashFor_NoGoMod(t *testing.T) {
	root := t.TempDir()
	handler := filepath.Join(root, "skill.go")
	if err := os.WriteFile(handler, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write skill.go: %v", err)
	}
	if _, ok := goModHashFor(handler); ok {
		t.Error("expected ok=false when no go.mod is present")
	}
}

// TestValidateGoSkillSymbol_OK accepts a parseable skill.go that
// declares the canonical RegisterSkill plugin entry point.
func TestValidateGoSkillSymbol_OK(t *testing.T) {
	dir := t.TempDir()
	src := `package main

import "github.com/gridctl/gridctl/pkg/agent/skill"

func RegisterSkill(reg *skill.Registry) error { return nil }
`
	path := filepath.Join(dir, "skill.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write skill.go: %v", err)
	}
	if err := validateGoSkillSymbol(path); err != nil {
		t.Errorf("expected nil error for valid RegisterSkill, got: %v", err)
	}
}

// TestValidateGoSkillSymbol_Missing flags a skill.go that omits the
// plugin entry point — the most common copy-paste mistake the Pitfall
// #5 in the brief calls out. The error message must name the symbol
// so the operator can fix it without a build round-trip.
func TestValidateGoSkillSymbol_Missing(t *testing.T) {
	dir := t.TempDir()
	src := `package main

func New() {}
`
	path := filepath.Join(dir, "skill.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write skill.go: %v", err)
	}
	err := validateGoSkillSymbol(path)
	if err == nil {
		t.Fatal("expected error when RegisterSkill is missing")
	}
	if !strings.Contains(err.Error(), "RegisterSkill") {
		t.Errorf("expected error to name RegisterSkill, got: %v", err)
	}
}

// TestValidateGoSkillSymbol_WrongShape flags a RegisterSkill with the
// wrong signature (no parameter), so the operator hits a clear
// validate error rather than a confusing plugin.Lookup failure later.
func TestValidateGoSkillSymbol_WrongShape(t *testing.T) {
	dir := t.TempDir()
	src := `package main

func RegisterSkill() error { return nil }
`
	path := filepath.Join(dir, "skill.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write skill.go: %v", err)
	}
	err := validateGoSkillSymbol(path)
	if err == nil {
		t.Fatal("expected error for RegisterSkill with wrong signature")
	}
	if !strings.Contains(err.Error(), "*skill.Registry") {
		t.Errorf("expected error to reference *skill.Registry, got: %v", err)
	}
}

// TestRunAgentValidate_GoMissingRegisterSkill asserts the validate
// command surfaces the symbol-check failure with a 'is invalid' error
// and reports the underlying RegisterSkill diagnostic.
func TestRunAgentValidate_GoMissingRegisterSkill(t *testing.T) {
	home := setTempHomeAgent(t)
	t.Cleanup(resetAgentFlagsForTest)

	body := "package main\n\nfunc New() {}\n"
	writeRegistrySkill(t, home, "broken-go", body, "go")

	err := runAgentValidate("broken-go")
	if err == nil {
		t.Fatal("expected validate failure for go skill missing RegisterSkill")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("expected 'invalid' in error, got: %v", err)
	}
}

// TestRunAgentValidate_GoOK confirms a Go-handler skill with a valid
// RegisterSkill declaration passes validate (handler check is static —
// no go build is invoked).
func TestRunAgentValidate_GoOK(t *testing.T) {
	home := setTempHomeAgent(t)
	t.Cleanup(resetAgentFlagsForTest)

	body := `package main

import "github.com/gridctl/gridctl/pkg/agent/skill"

func RegisterSkill(reg *skill.Registry) error { return nil }
`
	writeRegistrySkill(t, home, "valid-go", body, "go")

	if err := runAgentValidate("valid-go"); err != nil {
		t.Fatalf("validate clean go skill: %v", err)
	}
}

// TestRunAgentInit_JSONContract_EmptyCreatedIsArray locks down the
// `--format json` contract: when every starter file already exists,
// `created` must serialize as `[]`, never `null`. The .([]any) type
// assertion is the load-bearing check — a nil slice marshals to JSON
// null and the assertion fails with ok=false.
func TestRunAgentInit_JSONContract_EmptyCreatedIsArray(t *testing.T) {
	resetAgentInitFlagsForTest(t)
	t.Cleanup(func() { resetAgentInitFlagsForTest(t) })

	dir := t.TempDir()

	if err := runAgentInit(agentInitCmd, []string{dir}); err != nil {
		t.Fatalf("first runAgentInit (populate): %v", err)
	}

	resetAgentInitFlagsForTest(t)
	var buf bytes.Buffer
	agentInitCmd.SetOut(&buf)
	agentInitFormat = "json"
	if err := runAgentInit(agentInitCmd, []string{dir}); err != nil {
		t.Fatalf("second runAgentInit (json): %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal output %q: %v", buf.String(), err)
	}

	created, ok := got["created"].([]any)
	if !ok {
		t.Fatalf(`"created" must be a JSON array, got %T (value=%v) — null breaks consumer pipelines`, got["created"], got["created"])
	}
	if len(created) != 0 {
		t.Errorf(`"created" must be empty when all files were skipped, got %v`, created)
	}
}

// TestRunAgentInit_JSONContract_EmptySkippedIsArray is the symmetric
// case: a fresh directory has nothing to skip, so `skipped` must
// serialize as `[]`. Same nil-slice failure mode as the created case.
func TestRunAgentInit_JSONContract_EmptySkippedIsArray(t *testing.T) {
	resetAgentInitFlagsForTest(t)
	t.Cleanup(func() { resetAgentInitFlagsForTest(t) })

	dir := t.TempDir()

	var buf bytes.Buffer
	agentInitCmd.SetOut(&buf)
	agentInitFormat = "json"
	if err := runAgentInit(agentInitCmd, []string{dir}); err != nil {
		t.Fatalf("runAgentInit (json, fresh dir): %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal output %q: %v", buf.String(), err)
	}

	skipped, ok := got["skipped"].([]any)
	if !ok {
		t.Fatalf(`"skipped" must be a JSON array, got %T (value=%v) — null breaks consumer pipelines`, got["skipped"], got["skipped"])
	}
	if len(skipped) != 0 {
		t.Errorf(`"skipped" must be empty when all files were created, got %v`, skipped)
	}
}

