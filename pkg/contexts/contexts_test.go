package contexts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestManager returns a Manager rooted at a temp home with the given
// client detect dirs pre-created.
func newTestManager(t *testing.T, detectDirs ...string) *Manager {
	t.Helper()
	home := t.TempDir()
	for _, d := range detectDirs {
		if err := os.MkdirAll(filepath.Join(home, d), 0755); err != nil {
			t.Fatal(err)
		}
	}
	return NewManagerWithHome(home)
}

func initCanonical(t *testing.T, m *Manager, content string) {
	t.Helper()
	if err := m.SaveCanonical(content); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func statusOf(t *testing.T, m *Manager, slug string) ClientStatus {
	t.Helper()
	statuses, err := m.Statuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, cs := range statuses {
		if cs.Slug == slug {
			return cs
		}
	}
	t.Fatalf("no status for slug %q", slug)
	return ClientStatus{}
}

func TestTargetsTableSanity(t *testing.T) {
	seen := map[string]bool{}
	for _, tgt := range Targets() {
		if seen[tgt.Slug] {
			t.Errorf("duplicate slug %q", tgt.Slug)
		}
		seen[tgt.Slug] = true
		if tgt.Name == "" || tgt.Strategy == "" {
			t.Errorf("%s: missing name or strategy", tgt.Slug)
		}
		if len(tgt.DetectDirs) == 0 {
			t.Errorf("%s: no detect dirs", tgt.Slug)
		}
		for _, goos := range []string{"darwin", "linux"} {
			if tgt.Paths[goos] == "" {
				t.Errorf("%s: no path for %s", tgt.Slug, goos)
			}
		}
	}
	for _, u := range Unsupported() {
		if seen[u.Slug] {
			t.Errorf("slug %q is both supported and unsupported", u.Slug)
		}
		if u.Reason == "" {
			t.Errorf("%s: unsupported without a reason", u.Slug)
		}
	}
}

func TestInitFromTemplateRefusesOverwrite(t *testing.T) {
	m := newTestManager(t)
	if err := m.InitFromTemplate(false); err != nil {
		t.Fatal(err)
	}
	if !m.HasCanonical() {
		t.Fatal("canonical file not created")
	}
	content, err := m.CanonicalContent()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "Global Agent Context") {
		t.Errorf("template content missing heading: %q", content)
	}

	if err := m.InitFromTemplate(false); err == nil {
		t.Fatal("expected overwrite refusal")
	}
	if err := m.InitFromTemplate(true); err != nil {
		t.Fatalf("force overwrite failed: %v", err)
	}
}

func TestInitFromClientStripsManagedContent(t *testing.T) {
	m := newTestManager(t, ".gemini")
	target := filepath.Join(m.home, ".gemini", "GEMINI.md")
	writeFile(t, target, shimLine(m.CanonicalPath())+"\n\n# My hand-written rules\n\n- Be terse.\n")

	if err := m.InitFromClient("gemini", false); err != nil {
		t.Fatal(err)
	}
	content, err := m.CanonicalContent()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(content, "@"+m.CanonicalPath()) {
		t.Errorf("shim line leaked into canonical: %q", content)
	}
	if !strings.Contains(content, "# My hand-written rules") {
		t.Errorf("user content missing from canonical: %q", content)
	}
}

func TestInitFromClientImportPathDiffersFromWritePath(t *testing.T) {
	m := newTestManager(t, ".claude")
	// claude-code imports from CLAUDE.md but writes to rules/gridctl.md.
	writeFile(t, filepath.Join(m.home, ".claude", "CLAUDE.md"), "# Personal prefs\n")

	if err := m.InitFromClient("claude-code", false); err != nil {
		t.Fatal(err)
	}
	content, _ := m.CanonicalContent()
	if !strings.Contains(content, "# Personal prefs") {
		t.Errorf("import missed CLAUDE.md content: %q", content)
	}
}

func TestScanReportsExistingFiles(t *testing.T) {
	m := newTestManager(t, ".gemini")
	writeFile(t, filepath.Join(m.home, ".gemini", "GEMINI.md"), "hello\n")

	var geminiEntry *ScanEntry
	for _, e := range m.Scan() {
		if e.Slug == "gemini" {
			cp := e
			geminiEntry = &cp
		}
	}
	if geminiEntry == nil {
		t.Fatal("gemini missing from scan")
	}
	if !geminiEntry.Exists || geminiEntry.Size != 6 {
		t.Errorf("unexpected scan entry: %+v", geminiEntry)
	}
}

func TestSyncDedicatedFileCreatesManagedFile(t *testing.T) {
	m := newTestManager(t, ".claude")
	initCanonical(t, m, "# Rules\n\n- Be kind.\n")

	res, err := m.SyncClient(context.Background(), "claude-code", SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionCreated {
		t.Fatalf("action = %q, want created", res.Action)
	}
	content := readFile(t, res.TargetPath)
	if !strings.HasPrefix(content, fileHeader) {
		t.Errorf("missing managed header: %q", content)
	}
	if !strings.Contains(content, "- Be kind.") {
		t.Errorf("canonical body missing: %q", content)
	}
	if got := statusOf(t, m, "claude-code").State; got != StateInSync {
		t.Errorf("state = %q, want in-sync", got)
	}
}

func TestSyncDedicatedFileWithFrontmatter(t *testing.T) {
	m := newTestManager(t, ".copilot")
	initCanonical(t, m, "# Rules\n")

	res, err := m.SyncClient(context.Background(), "vscode", SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	content := readFile(t, res.TargetPath)
	if !strings.HasPrefix(content, "---\napplyTo: \"**\"\n---\n") {
		t.Errorf("frontmatter missing: %q", content)
	}
}

func TestSyncClientUnavailableErrors(t *testing.T) {
	m := newTestManager(t) // no detect dirs at all
	initCanonical(t, m, "# Rules\n")

	_, err := m.SyncClient(context.Background(), "claude-code", SyncOptions{})
	if err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("expected not-initialized error, got %v", err)
	}
}

func TestSyncUnknownAndUnsupportedClients(t *testing.T) {
	m := newTestManager(t)
	initCanonical(t, m, "# Rules\n")

	if _, err := m.SyncClient(context.Background(), "nope", SyncOptions{}); err == nil {
		t.Error("expected unknown-client error")
	}
	_, err := m.SyncClient(context.Background(), "cursor", SyncOptions{})
	if err == nil || !strings.Contains(err.Error(), "app-internal storage") {
		t.Errorf("expected unsupported reason, got %v", err)
	}
}

func TestSyncShimPreservesUserContent(t *testing.T) {
	m := newTestManager(t, ".gemini")
	initCanonical(t, m, "# Rules\n")
	target := filepath.Join(m.home, ".gemini", "GEMINI.md")
	userBody := "# Gemini-only prefs\n\n- Use flash for summaries.\n"
	writeFile(t, target, userBody)

	res, err := m.SyncClient(context.Background(), "gemini", SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionUpdated {
		t.Fatalf("action = %q, want updated", res.Action)
	}
	content := readFile(t, target)
	if !strings.HasPrefix(content, shimLine(m.CanonicalPath())+"\n") {
		t.Errorf("shim line not first: %q", content)
	}
	if !strings.Contains(content, userBody) {
		t.Errorf("user content altered: %q", content)
	}

	// Idempotent: second sync is unchanged.
	res2, err := m.SyncClient(context.Background(), "gemini", SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Action != ActionUnchanged {
		t.Errorf("re-sync action = %q, want unchanged", res2.Action)
	}

	// Shims never go stale: canonical edits flow through the reference.
	initCanonical(t, m, "# Rules v2\n")
	if got := statusOf(t, m, "gemini").State; got != StateInSync {
		t.Errorf("shim state after canonical edit = %q, want in-sync", got)
	}
}

func TestSyncBlockAppendsAndPreservesUserContent(t *testing.T) {
	m := newTestManager(t, ".config/opencode")
	initCanonical(t, m, "# Rules\n\n- One.\n")
	target := filepath.Join(m.home, ".config", "opencode", "AGENTS.md")
	writeFile(t, target, "# User section above\n")

	if _, err := m.SyncClient(context.Background(), "opencode", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	content := readFile(t, target)
	if !strings.HasPrefix(content, "# User section above\n") {
		t.Errorf("user prefix altered: %q", content)
	}
	if !strings.Contains(content, beginMarker) || !strings.Contains(content, endMarker) {
		t.Errorf("markers missing: %q", content)
	}

	// Canonical change replaces only the block.
	initCanonical(t, m, "# Rules\n\n- Two.\n")
	if got := statusOf(t, m, "opencode").State; got != StateStale {
		t.Errorf("state = %q, want stale", got)
	}
	if _, err := m.SyncClient(context.Background(), "opencode", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	content = readFile(t, target)
	if !strings.HasPrefix(content, "# User section above\n") {
		t.Errorf("user prefix altered on re-sync: %q", content)
	}
	if !strings.Contains(content, "- Two.") || strings.Contains(content, "- One.") {
		t.Errorf("block not replaced: %q", content)
	}
}

func TestSyncBlockCreatesFileWhenAbsent(t *testing.T) {
	m := newTestManager(t, ".config/zed")
	initCanonical(t, m, "# Rules\n")

	res, err := m.SyncClient(context.Background(), "zed", SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionCreated {
		t.Fatalf("action = %q, want created", res.Action)
	}
	content := readFile(t, res.TargetPath)
	if !strings.HasPrefix(content, beginMarker) {
		t.Errorf("created file should start with the managed block: %q", content)
	}
}

func TestSyncWindsurfCapRefusesOversizedContent(t *testing.T) {
	m := newTestManager(t, ".codeium/windsurf")
	initCanonical(t, m, strings.Repeat("x", 7000))

	res, err := m.SyncClient(context.Background(), "windsurf", SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionError || !strings.Contains(res.Error, "limit is 6000") {
		t.Fatalf("expected over-cap error, got %+v", res)
	}
	if _, statErr := os.Stat(res.TargetPath); !os.IsNotExist(statErr) {
		t.Error("oversized target should not have been written")
	}
}

func TestDriftDetectionAndForceOverwrite(t *testing.T) {
	m := newTestManager(t, ".config/opencode")
	initCanonical(t, m, "# Rules\n")
	target := filepath.Join(m.home, ".config", "opencode", "AGENTS.md")
	if _, err := m.SyncClient(context.Background(), "opencode", SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	// Hand-edit inside the managed block.
	edited := strings.Replace(readFile(t, target), "# Rules", "# Rules EDITED", 1)
	writeFile(t, target, edited)

	if got := statusOf(t, m, "opencode").State; got != StateDrifted {
		t.Fatalf("state = %q, want drifted", got)
	}

	res, err := m.SyncClient(context.Background(), "opencode", SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionSkippedDrift {
		t.Fatalf("action = %q, want skipped-drift", res.Action)
	}
	if strings.Contains(readFile(t, target), "# Rules\n") && !strings.Contains(readFile(t, target), "EDITED") {
		t.Error("drifted target was overwritten without force")
	}

	res, err = m.SyncClient(context.Background(), "opencode", SyncOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionUpdated {
		t.Fatalf("force action = %q, want updated", res.Action)
	}
	if strings.Contains(readFile(t, target), "EDITED") {
		t.Error("force sync did not restore managed content")
	}
	if got := statusOf(t, m, "opencode").State; got != StateInSync {
		t.Errorf("state after force = %q, want in-sync", got)
	}
}

func TestAdoptPullsEditIntoCanonical(t *testing.T) {
	m := newTestManager(t, ".config/opencode", ".config/zed")
	initCanonical(t, m, "# Rules\n")
	ctx := context.Background()
	if _, err := m.SyncClient(ctx, "opencode", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SyncClient(ctx, "zed", SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(m.home, ".config", "opencode", "AGENTS.md")
	edited := strings.Replace(readFile(t, target), "# Rules", "# Rules ADOPTME", 1)
	writeFile(t, target, edited)

	if err := m.Adopt(ctx, "opencode"); err != nil {
		t.Fatal(err)
	}
	canonical, _ := m.CanonicalContent()
	if !strings.Contains(canonical, "ADOPTME") {
		t.Errorf("edit not adopted into canonical: %q", canonical)
	}
	if got := statusOf(t, m, "opencode").State; got != StateInSync {
		t.Errorf("adopted client state = %q, want in-sync", got)
	}
	// The other synced client is now behind the canon.
	if got := statusOf(t, m, "zed").State; got != StateStale {
		t.Errorf("other client state = %q, want stale", got)
	}
}

func TestAdoptShimIsRejected(t *testing.T) {
	m := newTestManager(t, ".gemini")
	initCanonical(t, m, "# Rules\n")
	if _, err := m.SyncClient(context.Background(), "gemini", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	err := m.Adopt(context.Background(), "gemini")
	if err == nil || !strings.Contains(err.Error(), "import shim") {
		t.Fatalf("expected shim rejection, got %v", err)
	}
}

func TestUnsyncRemovesArtifactsAndPreservesUserContent(t *testing.T) {
	m := newTestManager(t, ".claude", ".gemini", ".config/opencode")
	initCanonical(t, m, "# Rules\n")
	ctx := context.Background()

	geminiTarget := filepath.Join(m.home, ".gemini", "GEMINI.md")
	userBody := "# Mine\n\n- Keep this.\n"
	writeFile(t, geminiTarget, userBody)

	for _, slug := range []string{"claude-code", "gemini", "opencode"} {
		if _, err := m.SyncClient(ctx, slug, SyncOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	results, err := m.UnsyncAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("unsynced %d clients, want 3", len(results))
	}

	// Dedicated file removed.
	if _, err := os.Stat(filepath.Join(m.home, ".claude", "rules", "gridctl.md")); !os.IsNotExist(err) {
		t.Error("dedicated file survived unsync")
	}
	// User-owned shim target keeps its content, loses the shim line.
	content := readFile(t, geminiTarget)
	if strings.Contains(content, "@"+m.CanonicalPath()) {
		t.Errorf("shim line survived unsync: %q", content)
	}
	if !strings.Contains(content, "- Keep this.") {
		t.Errorf("user content lost on unsync: %q", content)
	}
	// gridctl-created block file removed entirely.
	if _, err := os.Stat(filepath.Join(m.home, ".config", "opencode", "AGENTS.md")); !os.IsNotExist(err) {
		t.Error("gridctl-created block file survived unsync")
	}

	for _, slug := range []string{"claude-code", "gemini", "opencode"} {
		if got := statusOf(t, m, slug).State; got != StateNeverSynced {
			t.Errorf("%s state = %q, want never-synced", slug, got)
		}
	}
}

func TestUnsyncBlockKeepsUserFile(t *testing.T) {
	m := newTestManager(t, ".config/opencode")
	initCanonical(t, m, "# Rules\n")
	target := filepath.Join(m.home, ".config", "opencode", "AGENTS.md")
	writeFile(t, target, "# User stuff\n")
	ctx := context.Background()
	if _, err := m.SyncClient(ctx, "opencode", SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	res, err := m.Unsync(ctx, "opencode")
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != "removed-region" {
		t.Fatalf("action = %q, want removed-region", res.Action)
	}
	content := readFile(t, target)
	if strings.Contains(content, beginMarker) {
		t.Errorf("block survived unsync: %q", content)
	}
	if !strings.Contains(content, "# User stuff") {
		t.Errorf("user content lost: %q", content)
	}
}

func TestCorruptBlockRefusedThenRepairedWithForce(t *testing.T) {
	m := newTestManager(t, ".config/opencode")
	initCanonical(t, m, "# Rules\n")
	target := filepath.Join(m.home, ".config", "opencode", "AGENTS.md")
	ctx := context.Background()
	if _, err := m.SyncClient(ctx, "opencode", SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	// Remove the END marker: corrupt.
	writeFile(t, target, strings.Replace(readFile(t, target), endMarker, "", 1))

	res, err := m.SyncClient(ctx, "opencode", SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionSkippedDrift || !strings.Contains(res.Error, "corrupt") {
		t.Fatalf("expected corrupt-block skip, got %+v", res)
	}

	res, err = m.SyncClient(ctx, "opencode", SyncOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionUpdated {
		t.Fatalf("force repair action = %q, want updated", res.Action)
	}
	if _, found, berr := extractBlockInner(readFile(t, target)); berr != nil || !found {
		t.Errorf("block not repaired: found=%v err=%v", found, berr)
	}
}

func TestTargetMissingState(t *testing.T) {
	m := newTestManager(t, ".claude")
	initCanonical(t, m, "# Rules\n")
	ctx := context.Background()
	res, err := m.SyncClient(ctx, "claude-code", SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(res.TargetPath); err != nil {
		t.Fatal(err)
	}
	if got := statusOf(t, m, "claude-code").State; got != StateTargetMissing {
		t.Errorf("state = %q, want target-missing", got)
	}
	statuses, err := m.Statuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !NeedsSync(statuses) {
		t.Error("NeedsSync should be true with a missing target")
	}
}

func TestSyncAllSkipsUnavailable(t *testing.T) {
	m := newTestManager(t, ".claude") // only claude-code available
	initCanonical(t, m, "# Rules\n")

	results, err := m.SyncAll(context.Background(), SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]string{}
	for _, r := range results {
		actions[r.Slug] = r.Action
	}
	if actions["claude-code"] != ActionCreated {
		t.Errorf("claude-code action = %q, want created", actions["claude-code"])
	}
	if actions["gemini"] != ActionSkippedUnavailable {
		t.Errorf("gemini action = %q, want skipped-unavailable", actions["gemini"])
	}
}

func TestSyncDryRunWritesNothing(t *testing.T) {
	m := newTestManager(t, ".claude")
	initCanonical(t, m, "# Rules\n")

	res, err := m.SyncClient(context.Background(), "claude-code", SyncOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionWouldCreate {
		t.Fatalf("action = %q, want would-create", res.Action)
	}
	if res.Diff == "" {
		t.Error("dry run should include a diff")
	}
	if _, statErr := os.Stat(res.TargetPath); !os.IsNotExist(statErr) {
		t.Error("dry run wrote the target")
	}
	if got := statusOf(t, m, "claude-code").State; got != StateNeverSynced {
		t.Errorf("state after dry run = %q, want never-synced", got)
	}
}

func TestDiffShowsCanonicalVsManagedContent(t *testing.T) {
	m := newTestManager(t, ".config/opencode")
	initCanonical(t, m, "# Rules\n\n- One.\n")
	ctx := context.Background()
	target := filepath.Join(m.home, ".config", "opencode", "AGENTS.md")
	if _, err := m.SyncClient(ctx, "opencode", SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	diff, err := m.Diff(ctx, "opencode")
	if err != nil {
		t.Fatal(err)
	}
	if diff != "" {
		t.Errorf("in-sync diff should be empty, got %q", diff)
	}

	writeFile(t, target, strings.Replace(readFile(t, target), "- One.", "- One edited.", 1))
	diff, err = m.Diff(ctx, "opencode")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "-- One.") || !strings.Contains(diff, "+- One edited.") {
		t.Errorf("diff missing expected hunks: %q", diff)
	}
}

func TestLockFileNewerVersionRejected(t *testing.T) {
	m := newTestManager(t)
	initCanonical(t, m, "# Rules\n")
	writeFile(t, m.lockPath(), "version: 99\nscope: global\nclients: {}\n")

	_, err := m.Statuses(context.Background())
	if err == nil || !strings.Contains(err.Error(), "newer gridctl") {
		t.Fatalf("expected newer-version error, got %v", err)
	}
}

func TestBackupsCreatedAndPruned(t *testing.T) {
	m := newTestManager(t, ".claude")
	ctx := context.Background()
	target := filepath.Join(m.home, ".claude", "rules", "gridctl.md")

	for i := 0; i < 6; i++ {
		initCanonical(t, m, strings.Repeat("x", i+1)+"\n")
		if _, err := m.SyncClient(ctx, "claude-code", SyncOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil {
		t.Fatal(err)
	}
	backups := 0
	for _, e := range entries {
		if strings.Contains(e.Name(), backupSuffix) {
			backups++
		}
	}
	if backups == 0 || backups > maxBackups {
		t.Errorf("backup count = %d, want 1..%d", backups, maxBackups)
	}
}

func TestCRLFBlockStillExtracts(t *testing.T) {
	content := "user\r\n" + beginMarker + "\r\n" + blockHeader + "\r\n\r\nbody\r\n" + endMarker + "\r\n"
	inner, found, err := extractBlockInner(content)
	if err != nil || !found {
		t.Fatalf("extract failed: found=%v err=%v", found, err)
	}
	if inner != "body" {
		t.Errorf("inner = %q, want body", inner)
	}
}

func TestNewManager(t *testing.T) {
	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(m.CanonicalPath(), filepath.Join(".gridctl", "context", "AGENTS.md")) {
		t.Errorf("unexpected canonical path: %s", m.CanonicalPath())
	}
}

func TestInitFromFile(t *testing.T) {
	m := newTestManager(t)
	src := filepath.Join(t.TempDir(), "prefs.md")
	writeFile(t, src, "# From a file\n")

	if err := m.InitFromFile(src, false); err != nil {
		t.Fatal(err)
	}
	content, _ := m.CanonicalContent()
	if !strings.Contains(content, "# From a file") {
		t.Errorf("content missing: %q", content)
	}
	if err := m.InitFromFile(src, false); err == nil {
		t.Fatal("expected overwrite refusal")
	}
}

func TestSaveCanonicalRejectsMarkerStrings(t *testing.T) {
	m := newTestManager(t)
	for _, bad := range []string{beginMarker, endMarker, headerPrefix + " -->"} {
		err := m.SaveCanonical("# Rules\n\n" + bad + "\n")
		if err == nil || !strings.Contains(err.Error(), "must not contain") {
			t.Errorf("marker %q accepted into canonical content (err=%v)", bad, err)
		}
	}
}

func TestProseMentionOfMarkerIsNotABoundary(t *testing.T) {
	m := newTestManager(t, ".config/opencode")
	initCanonical(t, m, "# Rules\n")
	target := filepath.Join(m.home, ".config", "opencode", "AGENTS.md")
	// The marker text appears mid-line in user prose; it must not read as
	// a block boundary.
	userProse := "note: gridctl uses " + beginMarker + " markers\n\nmy important notes here\n"
	writeFile(t, target, userProse)
	ctx := context.Background()

	if _, err := m.SyncClient(ctx, "opencode", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := statusOf(t, m, "opencode").State; got != StateInSync {
		t.Fatalf("state = %q, want in-sync", got)
	}
	content := readFile(t, target)
	if !strings.Contains(content, "my important notes here") {
		t.Errorf("user prose destroyed: %q", content)
	}

	// Even a force re-sync must preserve the prose above the real block.
	initCanonical(t, m, "# Rules v2\n")
	if _, err := m.SyncClient(ctx, "opencode", SyncOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	content = readFile(t, target)
	if !strings.Contains(content, "my important notes here") {
		t.Errorf("user prose destroyed by force sync: %q", content)
	}
	if !strings.Contains(content, "- Rules v2") && !strings.Contains(content, "# Rules v2") {
		t.Errorf("block not updated: %q", content)
	}
}

func TestDuplicatedBlockReportsCorrupt(t *testing.T) {
	m := newTestManager(t, ".config/opencode")
	initCanonical(t, m, "# Rules\n")
	target := filepath.Join(m.home, ".config", "opencode", "AGENTS.md")
	ctx := context.Background()
	if _, err := m.SyncClient(ctx, "opencode", SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	// User copy-pastes the whole block: two BEGIN markers.
	content := readFile(t, target)
	writeFile(t, target, content+"\n"+content)

	cs := statusOf(t, m, "opencode")
	if cs.State != StateDrifted || !strings.Contains(cs.Detail, "corrupt") {
		t.Fatalf("duplicated block: state=%q detail=%q, want drifted/corrupt", cs.State, cs.Detail)
	}
	res, err := m.SyncClient(ctx, "opencode", SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionSkippedDrift {
		t.Errorf("action = %q, want skipped-drift", res.Action)
	}
}

func TestSyncDedicatedPreexistingUntrackedRequiresForce(t *testing.T) {
	m := newTestManager(t, ".claude")
	initCanonical(t, m, "# Rules\n")
	target := filepath.Join(m.home, ".claude", "rules", "gridctl.md")
	writeFile(t, target, "# A file the user made with our name\n")
	ctx := context.Background()

	res, err := m.SyncClient(ctx, "claude-code", SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionSkippedDrift || !strings.Contains(res.Error, "not tracked") {
		t.Fatalf("expected untracked skip, got %+v", res)
	}
	if !strings.Contains(readFile(t, target), "user made") {
		t.Error("untracked file was overwritten without force")
	}

	res, err = m.SyncClient(ctx, "claude-code", SyncOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionUpdated {
		t.Errorf("force action = %q, want updated", res.Action)
	}
}

func TestShimInsertPreservesCRLF(t *testing.T) {
	m := newTestManager(t, ".gemini")
	initCanonical(t, m, "# Rules\n")
	target := filepath.Join(m.home, ".gemini", "GEMINI.md")
	writeFile(t, target, "# CRLF file\r\n\r\n- keep endings\r\n")

	if _, err := m.SyncClient(context.Background(), "gemini", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	content := readFile(t, target)
	if !strings.Contains(content, "- keep endings\r\n") {
		t.Errorf("CRLF endings rewritten: %q", content)
	}
	if got := statusOf(t, m, "gemini").State; got != StateInSync {
		t.Errorf("state = %q, want in-sync", got)
	}
}

func TestWindsurfCapCountsRunesNotBytes(t *testing.T) {
	m := newTestManager(t, ".codeium/windsurf")
	// 2,500 two-byte runes: 5,000 bytes over the naive byte count once the
	// block chrome is added, but well under 6,000 characters.
	initCanonical(t, m, strings.Repeat("é", 2500))

	res, err := m.SyncClient(context.Background(), "windsurf", SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionCreated {
		t.Fatalf("multibyte content under the rune cap was refused: %+v", res)
	}
}

func TestCanonicalMissingAfterSyncNeedsAttention(t *testing.T) {
	m := newTestManager(t, ".claude")
	initCanonical(t, m, "# Rules\n")
	ctx := context.Background()
	if _, err := m.SyncClient(ctx, "claude-code", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(m.CanonicalPath()); err != nil {
		t.Fatal(err)
	}

	cs := statusOf(t, m, "claude-code")
	if cs.State != StateStale || !strings.Contains(cs.Detail, "canonical") {
		t.Errorf("state=%q detail=%q, want stale with canonical-missing detail", cs.State, cs.Detail)
	}
}

func TestAdoptAndUnsyncOfUnsupportedClientNameTheReason(t *testing.T) {
	m := newTestManager(t)
	initCanonical(t, m, "# Rules\n")
	ctx := context.Background()

	if err := m.Adopt(ctx, "cursor"); err == nil || !strings.Contains(err.Error(), "app-internal storage") {
		t.Errorf("adopt cursor error = %v, want unsupported reason", err)
	}
	if _, err := m.Unsync(ctx, "cursor"); err == nil || !strings.Contains(err.Error(), "app-internal storage") {
		t.Errorf("unsync cursor error = %v, want unsupported reason", err)
	}
}

func TestConcurrentSyncAllIsSerialized(t *testing.T) {
	m := newTestManager(t, ".claude", ".gemini", ".config/opencode")
	initCanonical(t, m, "# Rules\n")
	ctx := context.Background()

	done := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() {
			_, err := m.SyncAll(ctx, SyncOptions{})
			done <- err
		}()
	}
	for i := 0; i < 4; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	// Every synced client must have a consistent lock entry afterward.
	for _, slug := range []string{"claude-code", "gemini", "opencode"} {
		if got := statusOf(t, m, slug).State; got != StateInSync {
			t.Errorf("%s state = %q after concurrent syncs, want in-sync", slug, got)
		}
	}
}

func TestStatusesIncludeUnsupportedClients(t *testing.T) {
	m := newTestManager(t)
	statuses, err := m.Statuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	bySlug := map[string]ClientStatus{}
	for _, cs := range statuses {
		bySlug[cs.Slug] = cs
	}
	for _, slug := range []string{"claude", "cursor", "anythingllm"} {
		cs, ok := bySlug[slug]
		if !ok {
			t.Errorf("%s missing from statuses", slug)
			continue
		}
		if cs.State != StateUnsupported || cs.Detail == "" {
			t.Errorf("%s: state=%q detail=%q", slug, cs.State, cs.Detail)
		}
	}
}

// TestGrokIsSupportedBlockTarget is a regression test: Grok Build was once
// listed unsupported ("no documented global instruction file"), but Grok
// reads global rules from ~/.grok/AGENTS.md (documented in its bundled user
// guide and verified with `grok inspect`).
func TestGrokIsSupportedBlockTarget(t *testing.T) {
	tgt, ok := FindTarget("grok")
	if !ok {
		t.Fatal("grok must be a supported target")
	}
	if tgt.Strategy != StrategyBlock {
		t.Errorf("Strategy = %q, want %q", tgt.Strategy, StrategyBlock)
	}
	for _, goos := range []string{"darwin", "linux", "windows"} {
		if tgt.Paths[goos] != "~/.grok/AGENTS.md" {
			t.Errorf("Paths[%s] = %q, want %q", goos, tgt.Paths[goos], "~/.grok/AGENTS.md")
		}
	}
	if tgt.MaxChars != 0 {
		t.Errorf("MaxChars = %d, want 0 (Grok documents no cap)", tgt.MaxChars)
	}
	if tgt.Experimental {
		t.Error("grok must not be experimental; path comes from first-party docs")
	}
	if _, stillUnsupported := findUnsupported("grok"); stillUnsupported {
		t.Error("grok must no longer be listed unsupported")
	}
}
