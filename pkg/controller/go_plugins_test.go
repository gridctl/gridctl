package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/agent/skill"
	"github.com/gridctl/gridctl/pkg/registry"
)

func TestCheckGoSkillGuardrails_VersionMismatch(t *testing.T) {
	t.Parallel()
	m := &goSkillManifest{GoVersion: "go1.20.0"}
	ok, reason := checkGoSkillGuardrails(m, "go1.26.3", "", false)
	if ok {
		t.Fatal("expected guardrail to fail on version mismatch")
	}
	if !strings.Contains(reason, "go1.20.0") || !strings.Contains(reason, "go1.26.3") {
		t.Errorf("reason %q should name both versions", reason)
	}
}

func TestCheckGoSkillGuardrails_VersionMatch(t *testing.T) {
	t.Parallel()
	m := &goSkillManifest{GoVersion: "go1.26.3"}
	ok, reason := checkGoSkillGuardrails(m, "go1.26.3", "", false)
	if !ok {
		t.Fatalf("expected guardrail to pass; reason=%q", reason)
	}
}

func TestCheckGoSkillGuardrails_ModHashMismatch(t *testing.T) {
	t.Parallel()
	m := &goSkillManifest{GoVersion: "go1.26.3", GoModHash: "deadbeef"}
	ok, reason := checkGoSkillGuardrails(m, "go1.26.3", "cafef00d", true)
	if ok {
		t.Fatal("expected guardrail to fail on go.mod hash mismatch")
	}
	if !strings.Contains(reason, "go.mod") {
		t.Errorf("reason %q should mention go.mod", reason)
	}
}

// TestCheckGoSkillGuardrails_ManifestMissingModHash documents Pitfall 9:
// a manifest written without go_mod_hash (build path could not find a
// go.mod parent) loads on the go_version check alone. Treating this as
// "skip the plugin" would render standalone scaffolds unloadable —
// the field is a guardrail, not a gate.
func TestCheckGoSkillGuardrails_ManifestMissingModHash(t *testing.T) {
	t.Parallel()
	m := &goSkillManifest{GoVersion: "go1.26.3"}
	ok, reason := checkGoSkillGuardrails(m, "go1.26.3", "cafef00d", true)
	if !ok {
		t.Fatalf("expected pass when manifest omits go_mod_hash; reason=%q", reason)
	}
}

// TestCheckGoSkillGuardrails_DaemonMissingModHash is the symmetric
// case: an installed daemon binary that cannot locate its own go.mod
// loads plugins on the go_version match alone. Otherwise installed
// daemons could never load any plugin — same shape as Pitfall 9 in
// reverse.
func TestCheckGoSkillGuardrails_DaemonMissingModHash(t *testing.T) {
	t.Parallel()
	m := &goSkillManifest{GoVersion: "go1.26.3", GoModHash: "deadbeef"}
	ok, reason := checkGoSkillGuardrails(m, "go1.26.3", "", false)
	if !ok {
		t.Fatalf("expected pass when daemon cannot resolve go.mod; reason=%q", reason)
	}
}

func TestReadGoSkillManifest_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	want := map[string]any{
		"name":         "triage",
		"handler":      "go",
		"go_version":   "go1.26.3",
		"go_mod_hash":  "abc123",
		"source_hash":  "def456",
		"handler_path": "skill.so",
		"extra":        "tolerated",
	}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	got, err := readGoSkillManifest(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Name != "triage" || got.GoVersion != "go1.26.3" || got.GoModHash != "abc123" {
		t.Errorf("manifest decoded incorrectly: %+v", got)
	}
}

func TestReadGoSkillManifest_Missing(t *testing.T) {
	t.Parallel()
	_, err := readGoSkillManifest(filepath.Join(t.TempDir(), "manifest.json"))
	if err == nil {
		t.Fatal("expected error for missing manifest")
	}
}

func TestReadGoSkillManifest_InvalidJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := readGoSkillManifest(path)
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestGoModHashWalk_FindsGoMod(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	modPath := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(modPath, []byte("module test\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	hash, ok := goModHashWalk(nested)
	if !ok {
		t.Fatal("expected to find go.mod by walking up")
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
	hash2, _ := goModHashWalk(dir)
	if hash != hash2 {
		t.Errorf("expected stable hash regardless of starting subdir, got %q vs %q", hash, hash2)
	}
}

func TestGoModHashWalk_NoGoMod(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, ok := goModHashWalk(dir); ok {
		t.Fatal("expected ok=false when no go.mod is reachable")
	}
}

// TestWrapDefinitionWithBody verifies the re-decoration the loader
// performs after a plugin's RegisterSkill populates the staging
// registry: ctx.SkillBody() must return the on-disk SKILL.md body
// rather than the body="" the plugin's Define call captured. Without
// this, the hybrid pattern silently breaks across the plugin boundary.
func TestWrapDefinitionWithBody(t *testing.T) {
	t.Parallel()
	const want = "# runbook\nseverity: page on err > 5%\n"

	type in struct{}
	type out struct{ Body string }
	def, err := skill.Define[in, out]("triage", "test", "",
		func(rc skill.RunContext, _ in) (out, error) {
			return out{Body: rc.SkillBody()}, nil
		})
	if err != nil {
		t.Fatalf("Define: %v", err)
	}

	// Sanity: without the wrap, body is empty.
	res, err := def.Invoker(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("orig invoker: %v", err)
	}
	if !strings.Contains(res.Content[0].Text, `"Body":""`) {
		t.Fatalf("orig invoker should see empty body, got %q", res.Content[0].Text)
	}

	wrapped := wrapDefinitionWithBody(def, want)
	res, err = wrapped.Invoker(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("wrapped invoker: %v", err)
	}
	// encoding/json HTML-escapes `>` to `>`, so compare against
	// a substring that survives the escape rather than the raw body.
	if !strings.Contains(res.Content[0].Text, `# runbook`) ||
		!strings.Contains(res.Content[0].Text, `severity: page on err`) {
		t.Errorf("wrapped invoker missing body override, got %q", res.Content[0].Text)
	}

	// The wrap must not mutate the source Definition.
	res, err = def.Invoker(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("orig after wrap: %v", err)
	}
	if !strings.Contains(res.Content[0].Text, `"Body":""`) {
		t.Errorf("wrap mutated the source Definition's invoker: %q", res.Content[0].Text)
	}
}

// TestLoadGoSkillPlugins_SkipsNonGoSkills confirms the loader does not
// touch TS or prompt-only skills — they never get a .so lookup and
// never produce a warning.
func TestLoadGoSkillPlugins_SkipsNonGoSkills(t *testing.T) {
	t.Parallel()
	regDir := t.TempDir()
	tsDir := filepath.Join(regDir, "skills", "ts-only")
	if err := os.MkdirAll(tsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tsDir, "SKILL.md"),
		[]byte("---\nname: ts-only\ndescription: ts\nstate: active\n---\nbody\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tsDir, "skill.ts"),
		[]byte("export default async () => ({});"), 0o644); err != nil {
		t.Fatalf("write skill.ts: %v", err)
	}

	store := registry.NewStore(regDir)
	if err := store.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	reg := skill.NewRegistry()

	logs := captureLogs(t, func(logger *slog.Logger) {
		loadGoSkillPlugins(store, reg, logger)
	})
	if logs != "" {
		t.Errorf("expected no warnings for non-go skill, got: %s", logs)
	}
	if defs := reg.List(); len(defs) != 0 {
		t.Errorf("expected empty registry, got %d defs", len(defs))
	}
}

// TestLoadGoSkillPlugins_MissingSO_WarnsAndContinues confirms the
// loader's per-skill failure isolation: a Go skill on disk with no
// compiled artifact warns once and the loop continues. Real builds
// always emit the .so beside the manifest; this is the "operator
// dropped a skill.go without running agent build" path.
func TestLoadGoSkillPlugins_MissingSO_WarnsAndContinues(t *testing.T) {
	t.Parallel()
	regDir := t.TempDir()
	goDir := filepath.Join(regDir, "skills", "needs-build")
	if err := os.MkdirAll(goDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(goDir, "SKILL.md"),
		[]byte("---\nname: needs-build\ndescription: go\nstate: active\n---\nbody\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(goDir, "skill.go"),
		[]byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write skill.go: %v", err)
	}

	store := registry.NewStore(regDir)
	if err := store.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	reg := skill.NewRegistry()
	logs := captureLogs(t, func(logger *slog.Logger) {
		loadGoSkillPlugins(store, reg, logger)
	})
	if !strings.Contains(logs, "skill.so missing") {
		t.Errorf("expected skill.so-missing warning, got: %s", logs)
	}
	if defs := reg.List(); len(defs) != 0 {
		t.Errorf("expected empty registry, got %d defs", len(defs))
	}
}

// TestLoadGoSkillPlugins_ManifestVersionMismatch is the
// guardrail-fired-actionable-warn path the brief explicitly calls
// out: deliberately mutate manifest.json's go_version and confirm the
// loader emits the rebuild instruction and skips the plugin.
func TestLoadGoSkillPlugins_ManifestVersionMismatch(t *testing.T) {
	t.Parallel()
	regDir := t.TempDir()
	goDir := filepath.Join(regDir, "skills", "stale")
	distDir := filepath.Join(goDir, "dist")
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(goDir, "SKILL.md"),
		[]byte("---\nname: stale\ndescription: go\nstate: active\n---\nbody\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(goDir, "skill.go"),
		[]byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write skill.go: %v", err)
	}
	// Empty .so file is enough — the loader must fail at the manifest
	// gate before ever reaching plugin.Open.
	if err := os.WriteFile(filepath.Join(distDir, "skill.so"), []byte("not a real plugin"), 0o644); err != nil {
		t.Fatalf("write skill.so: %v", err)
	}
	manifest := map[string]any{
		"name":         "stale",
		"handler":      "go",
		"handler_path": "skill.so",
		"go_version":   "go0.0.0-deliberately-wrong",
	}
	mb, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(distDir, "manifest.json"), mb, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	store := registry.NewStore(regDir)
	if err := store.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	reg := skill.NewRegistry()
	logs := captureLogs(t, func(logger *slog.Logger) {
		loadGoSkillPlugins(store, reg, logger)
	})
	if !strings.Contains(logs, "guardrail mismatch") {
		t.Errorf("expected guardrail-mismatch warning, got: %s", logs)
	}
	if !strings.Contains(logs, "gridctl agent build") {
		t.Errorf("expected rebuild hint, got: %s", logs)
	}
	if !strings.Contains(logs, runtime.Version()) {
		t.Errorf("expected daemon version %q in warning, got: %s", runtime.Version(), logs)
	}
	if defs := reg.List(); len(defs) != 0 {
		t.Errorf("expected empty registry, got %d defs", len(defs))
	}
}

// TestLoadGoSkillPlugins_ManifestMissing covers the "operator wiped
// or never produced manifest.json" path. Same shape as the version
// mismatch path: warn + skip + continue.
func TestLoadGoSkillPlugins_ManifestMissing(t *testing.T) {
	t.Parallel()
	regDir := t.TempDir()
	goDir := filepath.Join(regDir, "skills", "nomanifest")
	distDir := filepath.Join(goDir, "dist")
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(goDir, "SKILL.md"),
		[]byte("---\nname: nomanifest\ndescription: go\nstate: active\n---\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(goDir, "skill.go"),
		[]byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write skill.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(distDir, "skill.so"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write skill.so: %v", err)
	}

	store := registry.NewStore(regDir)
	if err := store.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	reg := skill.NewRegistry()
	logs := captureLogs(t, func(logger *slog.Logger) {
		loadGoSkillPlugins(store, reg, logger)
	})
	if !strings.Contains(logs, "manifest read failed") {
		t.Errorf("expected manifest-read warning, got: %s", logs)
	}
	if defs := reg.List(); len(defs) != 0 {
		t.Errorf("expected empty registry, got %d defs", len(defs))
	}
}

// TestLoadGoSkillPlugins_GuardrailPassesPluginOpenFails verifies the
// loader gets past the manifest gate (matching go_version, no
// go_mod_hash field — Pitfall 9) and then reports the plugin-open
// failure. Building a real plugin in a unit test is too expensive;
// the invalid-bytes path proves the loader does not short-circuit
// before plugin.Open and that one broken plugin does not block the
// loop.
func TestLoadGoSkillPlugins_GuardrailPassesPluginOpenFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("plugin.Open unavailable on windows")
	}
	t.Parallel()

	regDir := t.TempDir()
	goDir := filepath.Join(regDir, "skills", "bad-plugin")
	distDir := filepath.Join(goDir, "dist")
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(goDir, "SKILL.md"),
		[]byte("---\nname: bad-plugin\ndescription: go\nstate: active\n---\nbody prose\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(goDir, "skill.go"),
		[]byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write skill.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(distDir, "skill.so"),
		[]byte("not a real plugin"), 0o644); err != nil {
		t.Fatalf("write skill.so: %v", err)
	}
	manifest := map[string]any{
		"name":         "bad-plugin",
		"handler":      "go",
		"handler_path": "skill.so",
		"go_version":   runtime.Version(),
		// No go_mod_hash — Pitfall 9: missing field reads as "skip the
		// check", so this passes the gate.
	}
	mb, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(distDir, "manifest.json"), mb, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	store := registry.NewStore(regDir)
	if err := store.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	reg := skill.NewRegistry()
	logs := captureLogs(t, func(logger *slog.Logger) {
		loadGoSkillPlugins(store, reg, logger)
	})
	if !strings.Contains(logs, "plugin load failed") {
		t.Errorf("expected plugin-load-failed warning (proves loader got past manifest gate), got: %s", logs)
	}
	if strings.Contains(logs, "guardrail mismatch") {
		t.Errorf("loader should NOT report guardrail mismatch when versions match, got: %s", logs)
	}
}

// TestLoadGoSkillPlugins_NilStore_NoOp guards against a panic when the
// builder is built in an unusual order — the loader must tolerate nil
// inputs without faulting gateway start.
func TestLoadGoSkillPlugins_NilStore_NoOp(t *testing.T) {
	t.Parallel()
	loadGoSkillPlugins(nil, skill.NewRegistry(), slog.Default())
	loadGoSkillPlugins(registry.NewStore(t.TempDir()), nil, slog.Default())
}

// captureLogs runs fn with a logger that writes to a buffer and
// returns the captured text. Tests use this to assert which warnings
// the loader emits for each skip path.
func captureLogs(t *testing.T, fn func(*slog.Logger)) string {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	fn(logger)
	return buf.String()
}

// Compile-time check: *skill.Registry must satisfy the
// registry.SkillRegistry interface — that contract is what makes
// SetSkillRegistry callable with the loader's output. If the interface
// drifts, the loader breaks at gateway build time, not at first call.
var _ registry.SkillRegistry = (*skill.Registry)(nil)
