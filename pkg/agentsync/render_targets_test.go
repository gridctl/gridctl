package agentsync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/skills"
)

// enableRenderTargets stages the detect dirs so opencode, copilot, and
// gemini become available targets under the test home.
func enableRenderTargets(t *testing.T, home string) {
	t.Helper()
	for _, dir := range []string{".config/opencode", ".copilot", ".gemini"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func renderedPath(home, slug, name string) string {
	switch slug {
	case "opencode":
		return filepath.Join(home, ".config", "opencode", "agents", name+".md")
	case "copilot":
		return filepath.Join(home, ".copilot", "agents", name+".agent.md")
	case "gemini":
		return filepath.Join(home, ".gemini", "agents", name+".md")
	}
	return projectedPath(home, name)
}

func TestSync_RendersToAllDetectedTargets(t *testing.T) {
	mgr, home, _ := newTestManager(t, "alpha")
	enableRenderTargets(t, home)
	ctx := context.Background()

	results, err := mgr.Sync(ctx, nil, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 {
		t.Fatalf("results = %d rows, want 4 (one per target): %+v", len(results), results)
	}

	// Identity target stays verbatim; rendered targets differ and parse
	// as their dialect (frontmatter present, description carried).
	canon := agentContent("alpha", "Review the code.")
	for _, slug := range []string{"claude-code", "opencode", "copilot", "gemini"} {
		data, err := os.ReadFile(renderedPath(home, slug, "alpha"))
		if err != nil {
			t.Fatalf("%s projection missing: %v", slug, err)
		}
		if slug == "claude-code" {
			if string(data) != canon {
				t.Errorf("claude-code must be verbatim")
			}
			continue
		}
		if !strings.Contains(string(data), "description: Reviews things") {
			t.Errorf("%s render lacks description:\n%s", slug, data)
		}
	}

	// Re-sync is unchanged across every target (render determinism).
	results, err = mgr.Sync(ctx, nil, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Action != ActionUnchanged {
			t.Errorf("re-sync %s → %s = %q, want unchanged", r.Agent, r.Client, r.Action)
		}
	}
}

func TestSync_CanonEditGoesStaleOnEveryTarget(t *testing.T) {
	mgr, home, registryDir := newTestManager(t, "alpha")
	enableRenderTargets(t, home)
	ctx := context.Background()
	if _, err := mgr.Sync(ctx, nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	writeAgent(t, registryDir, "alpha", agentContent("alpha", "Review the code twice."))

	statuses, err := mgr.Statuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 4 {
		t.Fatalf("statuses = %d, want 4", len(statuses))
	}
	for _, s := range statuses {
		if s.State != StateStale {
			t.Errorf("%s state = %q, want stale after canon edit", s.Client, s.State)
		}
	}

	// Sync refreshes every target back to in-sync.
	if _, err := mgr.Sync(ctx, nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	statuses, _ = mgr.Statuses(ctx)
	for _, s := range statuses {
		if s.State != StateInSync {
			t.Errorf("%s state = %q after refresh, want in-sync", s.Client, s.State)
		}
	}
}

func TestSync_HandEditedRenderedFileDrifts(t *testing.T) {
	mgr, home, _ := newTestManager(t, "alpha")
	enableRenderTargets(t, home)
	ctx := context.Background()
	if _, err := mgr.Sync(ctx, nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	dest := renderedPath(home, "opencode", "alpha")
	if err := os.WriteFile(dest, []byte("---\ndescription: mine now\nmode: subagent\n---\n\nEdited.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	statuses, err := mgr.Statuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range statuses {
		want := StateInSync
		if s.Client == "opencode" {
			want = StateDrifted
		}
		if s.State != want {
			t.Errorf("%s state = %q, want %q", s.Client, s.State, want)
		}
		wantRender := "lossy"
		if s.Client == "claude-code" {
			wantRender = "identity"
		}
		if s.Render != wantRender {
			t.Errorf("%s render = %q, want %q", s.Client, s.Render, wantRender)
		}
	}

	// A bare sync skips the drifted target and touches nothing.
	results, err := mgr.Sync(ctx, nil, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Client == "opencode" && r.Action != ActionSkippedDrift {
			t.Errorf("drifted rendered target action = %q, want skipped-drift", r.Action)
		}
	}
	data, _ := os.ReadFile(dest)
	if !strings.Contains(string(data), "mine now") {
		t.Error("drifted rendered file was overwritten without --force")
	}

	// Force rewrites it.
	if _, err := mgr.Sync(ctx, nil, SyncOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(dest)
	if strings.Contains(string(data), "mine now") {
		t.Error("--force did not rewrite the drifted rendered file")
	}
}

func TestAdopt_RefusedOnRenderedTargets(t *testing.T) {
	mgr, home, _ := newTestManager(t, "alpha")
	enableRenderTargets(t, home)
	ctx := context.Background()
	if _, err := mgr.Sync(ctx, nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	for _, slug := range []string{"opencode", "copilot", "gemini"} {
		_, err := mgr.Adopt(ctx, "alpha", slug)
		var refusal *AdoptRefusal
		if !errors.As(err, &refusal) {
			t.Fatalf("%s adopt: err = %v, want AdoptRefusal", slug, err)
		}
		if !strings.Contains(err.Error(), "claude-code") {
			t.Errorf("%s refusal lacks identity-target redirect: %v", slug, err)
		}
	}

	// The identity target still adopts.
	dest := projectedPath(home, "alpha")
	edited := agentContent("alpha", "Review the code, adopted edit.")
	if err := os.WriteFile(dest, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := mgr.Adopt(ctx, "alpha", "claude-code")
	if err != nil {
		t.Fatalf("claude-code adopt: %v", err)
	}
	if !res.Changed {
		t.Error("adopt did not register the edit")
	}
}

func TestSync_LossyDetailSurfaces(t *testing.T) {
	mgr, home, registryDir := newTestManager(t)
	enableRenderTargets(t, home)
	writeAgent(t, registryDir, "alpha",
		"---\nname: alpha\ndescription: Reviews things\ntools: Read, Bash\nmodel: sonnet\n---\n\nBody.\n")
	ctx := context.Background()

	results, err := mgr.Sync(ctx, nil, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	byClient := map[string]SyncResult{}
	for _, r := range results {
		byClient[r.Client] = r
	}
	if d := byClient["claude-code"].Detail; d != "" {
		t.Errorf("identity target must have no lossy detail, got %q", d)
	}
	if d := byClient["opencode"].Detail; !strings.Contains(d, "model") || !strings.Contains(d, "tools") {
		t.Errorf("opencode detail = %q, want model and tools listed", d)
	}
	if d := byClient["copilot"].Detail; !strings.Contains(d, "model") || strings.Contains(d, "tools") {
		t.Errorf("copilot detail = %q, want model dropped but tools carried", d)
	}
}

func TestSync_DryRunCarriesLossyDetail(t *testing.T) {
	mgr, home, _ := newTestManager(t, "alpha")
	enableRenderTargets(t, home)

	results, err := mgr.Sync(context.Background(), nil, SyncOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Action != ActionWouldCopy {
			t.Errorf("%s dry-run action = %q, want would-copy", r.Client, r.Action)
		}
	}
	// Nothing written.
	if _, err := os.Stat(renderedPath(home, "opencode", "alpha")); !os.IsNotExist(err) {
		t.Error("dry-run wrote a rendered file")
	}
}

func TestUnsync_RemovesPerTargetFileNames(t *testing.T) {
	mgr, home, _ := newTestManager(t, "alpha")
	enableRenderTargets(t, home)
	ctx := context.Background()
	if _, err := mgr.Sync(ctx, nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err := mgr.Unsync(ctx, []string{"alpha"}, UnsyncOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"claude-code", "opencode", "copilot", "gemini"} {
		if _, err := os.Stat(renderedPath(home, slug, "alpha")); !os.IsNotExist(err) {
			t.Errorf("%s projection still on disk after unsync", slug)
		}
	}
}

func TestRenderedOutputIsValidYAMLFrontmatter(t *testing.T) {
	mgr, home, _ := newTestManager(t, "alpha")
	enableRenderTargets(t, home)
	if _, err := mgr.Sync(context.Background(), nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"opencode", "copilot", "gemini"} {
		data, err := os.ReadFile(renderedPath(home, slug, "alpha"))
		if err != nil {
			t.Fatal(err)
		}
		parts := strings.SplitN(string(data), "---", 3)
		if len(parts) < 3 {
			t.Fatalf("%s output has no frontmatter block:\n%s", slug, data)
		}
		var out map[string]any
		if err := yaml.Unmarshal([]byte(parts[1]), &out); err != nil {
			t.Errorf("%s frontmatter is not valid YAML: %v", slug, err)
		}
	}
}

func TestSync_RendererOutputChangeReachesDisk(t *testing.T) {
	// Simulates a gridctl upgrade whose renderer emits different bytes:
	// the destination holds the OLD render and the lock records its hash
	// (no drift), but the new render differs. Sync must rewrite the file;
	// recording the new hash while leaving old bytes on disk would
	// manufacture permanent false drift.
	mgr, home, _ := newTestManager(t, "alpha")
	enableRenderTargets(t, home)
	ctx := context.Background()
	if _, err := mgr.Sync(ctx, nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	dest := renderedPath(home, "opencode", "alpha")
	oldRender := []byte("---\ndescription: Reviews things\nmode: subagent\nlegacy: emission\n---\n\nReview the code.\n")
	if err := os.WriteFile(dest, oldRender, 0o644); err != nil {
		t.Fatal(err)
	}
	// Make the lock agree that the old render is what gridctl wrote.
	err := mgr.store.Mutate(ctx, false, func(pl *project.Lock) error {
		lf := viewFromLock(pl)
		e := lf.entry("alpha", "opencode")
		e.InstalledHash = contentHash(oldRender)
		lf.set("alpha", "opencode", e)
		return saveView(pl, lf)
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := mgr.Sync(ctx, nil, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Client == "opencode" && r.Action != ActionUpdated {
			t.Fatalf("renderer-change sync action = %q, want updated", r.Action)
		}
	}
	data, _ := os.ReadFile(dest)
	if strings.Contains(string(data), "legacy: emission") {
		t.Fatal("new render never reached disk")
	}
	statuses, _ := mgr.Statuses(ctx)
	for _, s := range statuses {
		if s.Client == "opencode" && s.State != StateInSync {
			t.Errorf("post-upgrade state = %q, want in-sync", s.State)
		}
	}
}

func TestRender_FlowUnsafeToolItemsQuoted(t *testing.T) {
	src := strings.Replace(testAgentMD, "tools: Read, Grep, Bash",
		`tools: ["a[b]", "Edit, Write", Read]`, 1)
	def, err := skills.ParseAgentMD([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	got, err := renderCopilot(def)
	if err != nil {
		t.Fatal(err)
	}
	back, err := skills.ParseAgentMD(got.Bytes)
	if err != nil {
		t.Fatalf("rendered output does not parse: %v\n%s", err, got.Bytes)
	}
	node, ok := back.ExtraByKey("tools")
	if !ok {
		t.Fatal("tools missing from rendered output")
	}
	var tools []string
	if err := node.Decode(&tools); err != nil {
		t.Fatal(err)
	}
	want := []string{"a[b]", "Edit, Write", "Read"}
	if strings.Join(tools, "|") != strings.Join(want, "|") {
		t.Errorf("tools = %v, want %v (items must survive flow quoting)", tools, want)
	}
}

func TestSync_NamelessCanonRendersDirectoryName(t *testing.T) {
	mgr, home, registryDir := newTestManager(t)
	enableRenderTargets(t, home)
	// Valid canonical file: description only, no name key.
	writeAgent(t, registryDir, "reviewer", "---\ndescription: Reviews things\n---\n\nBody.\n")

	if _, err := mgr.Sync(context.Background(), nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(renderedPath(home, "copilot", "reviewer"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "name: reviewer\n") {
		t.Errorf("rendered name must fall back to the store directory name:\n%s", data)
	}
}

func TestSync_UnchangedAndDriftRowsCarryNoLossyDetail(t *testing.T) {
	mgr, home, _ := newTestManager(t, "alpha")
	enableRenderTargets(t, home)
	ctx := context.Background()
	if _, err := mgr.Sync(ctx, nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	// Unchanged re-sync: no detail noise.
	results, _ := mgr.Sync(ctx, nil, SyncOptions{})
	for _, r := range results {
		if r.Action == ActionUnchanged && r.Detail != "" {
			t.Errorf("%s unchanged row carries detail %q", r.Client, r.Detail)
		}
	}
}
