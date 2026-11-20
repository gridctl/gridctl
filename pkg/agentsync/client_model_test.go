package agentsync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skills"
)

const (
	sonnetID = "anthropic/claude-sonnet-5-5"
	opusID   = "anthropic/claude-opus-5-5#high"
)

// clientPolicy is a rewrite policy carrying only an OpenCode client
// default: no top-level default or overrides.
func clientPolicy(def string, overrides map[string]string) *registry.ModelPolicy {
	return &registry.ModelPolicy{
		Rewrite: true,
		Clients: map[string]registry.ClientModelPolicy{
			"opencode": {Default: def, Overrides: overrides},
		},
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

func resultFor(t *testing.T, results []SyncResult, client string) SyncResult {
	t.Helper()
	for _, r := range results {
		if r.Client == client {
			return r
		}
	}
	t.Fatalf("no %s result in %+v", client, results)
	return SyncResult{}
}

func clientStatus(t *testing.T, mgr *Manager, client string) ProjectionStatus {
	t.Helper()
	statuses, err := mgr.Statuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range statuses {
		if s.Client == client {
			return s
		}
	}
	t.Fatalf("no %s status in %+v", client, statuses)
	return ProjectionStatus{}
}

func entryFor(t *testing.T, mgr *Manager, agent, client string) *Entry {
	t.Helper()
	lf, err := mgr.loadView(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	e := lf.entry(agent, client)
	if e == nil {
		t.Fatalf("no lock entry for %s/%s", agent, client)
	}
	return e
}

// passThroughRender is the pre-feature render of one canonical file.
func passThroughRender(t *testing.T, slug, name, canonical string) string {
	t.Helper()
	def, err := skills.ParseAgentMD([]byte(canonical))
	if err != nil {
		t.Fatal(err)
	}
	def.Name = name
	tgt, _ := FindTarget(slug)
	r, err := tgt.Render(def, RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return string(r.Bytes)
}

func TestRender_ClientModelKeyOrder(t *testing.T) {
	def := parseTestAgent(t)
	opts := RenderOptions{Model: opusID}

	got, err := renderOpenCode(def, opts)
	if err != nil {
		t.Fatal(err)
	}
	want := "---\ndescription: Reviews pull requests for style and correctness\nmode: subagent\nmodel: " + opusID + "\n---\n\nReview the diff carefully.\n\nFocus on correctness first.\n"
	if string(got.Bytes) != want {
		t.Errorf("opencode:\n%s\nwant:\n%s", got.Bytes, want)
	}
	if strings.Join(got.Dropped, ",") != "color,hooks,tools" {
		t.Errorf("opencode dropped = %v, model must not be listed once written", got.Dropped)
	}

	for _, render := range []RenderFunc{renderCopilot, renderGemini} {
		got, err := render(def, RenderOptions{Model: "gpt-5"})
		if err != nil {
			t.Fatal(err)
		}
		want := "---\nname: reviewer\ndescription: Reviews pull requests for style and correctness\nmodel: gpt-5\ntools: [Read, Grep, Bash]\n---\n\n"
		if !strings.HasPrefix(string(got.Bytes), want) {
			t.Errorf("render:\n%s\nwant prefix:\n%s", got.Bytes, want)
		}
		if strings.Join(got.Dropped, ",") != "color,hooks" {
			t.Errorf("dropped = %v", got.Dropped)
		}
	}

	// Without tools the model key still follows description.
	noTools, err := skills.ParseAgentMD([]byte("---\nname: a\ndescription: d\n---\n\nB.\n"))
	if err != nil {
		t.Fatal(err)
	}
	got, err = renderGemini(noTools, RenderOptions{Model: "inherit"})
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Bytes) != "---\nname: a\ndescription: d\nmodel: inherit\n---\n\nB.\n" {
		t.Errorf("gemini without tools:\n%s", got.Bytes)
	}
}

func TestRender_ClientModelQuotedAndRoundTrips(t *testing.T) {
	def := parseTestAgent(t)
	value := "acme/model: fast #1"
	for _, tgt := range Targets() {
		if tgt.Render == nil {
			continue
		}
		got, err := tgt.Render(def, RenderOptions{Model: value})
		if err != nil {
			t.Fatal(err)
		}
		back, err := skills.ParseAgentMD(got.Bytes)
		if err != nil {
			t.Fatalf("%s render does not parse: %v\n%s", tgt.Slug, err, got.Bytes)
		}
		if m, ok := back.DeclaredModel(); !ok || m != value {
			t.Errorf("%s model round-trip = %q, want %q", tgt.Slug, m, value)
		}
		again, _ := tgt.Render(def, RenderOptions{Model: value})
		if string(again.Bytes) != string(got.Bytes) {
			t.Errorf("%s render with options is not deterministic", tgt.Slug)
		}
	}
}

// Every agent target that renders a dialect must be a valid clients
// key, and every clients key must name a rendering target.
func TestRenderedAgentTargets_MatchTargetTable(t *testing.T) {
	var rendered []string
	for _, tgt := range Targets() {
		if tgt.Render != nil {
			rendered = append(rendered, tgt.Slug)
		}
	}
	sort.Strings(rendered)
	if got := registry.RenderedAgentTargets(); strings.Join(got, ",") != strings.Join(rendered, ",") {
		t.Fatalf("registry.RenderedAgentTargets() = %v, target table renders %v; update pkg/registry/honor.go", got, rendered)
	}
}

func TestClientModel_AppliedRecordedAndReported(t *testing.T) {
	mgr, home, registryDir := newTestManager(t)
	enableRenderTargets(t, home)
	canonical := "---\nname: reviewer\ndescription: Reviews things\nmodel: opus\ncolor: blue\n---\n\nReview.\n"
	writeAgent(t, registryDir, "reviewer", canonical)
	mgr.SetModelPolicy(clientPolicy(sonnetID, nil))
	ctx := context.Background()

	results, err := mgr.Sync(ctx, nil, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}

	oc := resultFor(t, results, "opencode")
	if oc.Action != ActionCopied {
		t.Fatalf("opencode action = %q", oc.Action)
	}
	wantDetail := `model preference "` + sonnetID + `" applied (policy; author declared "opus" is not carried); lossy opencode render: dropped frontmatter keys color`
	if oc.Detail != wantDetail {
		t.Errorf("opencode detail:\n got %q\nwant %q", oc.Detail, wantDetail)
	}

	data := readFile(t, renderedPath(home, "opencode", "reviewer"))
	if !strings.Contains(data, "mode: subagent\nmodel: "+sonnetID+"\n---\n") {
		t.Fatalf("model key not between mode and the closing delimiter:\n%s", data)
	}
	back, err := skills.ParseAgentMD([]byte(data))
	if err != nil {
		t.Fatalf("rendered file must parse: %v", err)
	}
	if m, _ := back.DeclaredModel(); m != sonnetID {
		t.Errorf("parsed model = %q", m)
	}
	if e := entryFor(t, mgr, "reviewer", "opencode"); e.ModelValue != sonnetID {
		t.Errorf("opencode model_value = %q", e.ModelValue)
	}

	// Targets without a client value render exactly as before and record
	// no model value; the author's model still drops there.
	for _, slug := range []string{"copilot", "gemini"} {
		if got := readFile(t, renderedPath(home, slug, "reviewer")); got != passThroughRender(t, slug, "reviewer", canonical) {
			t.Errorf("%s render changed without a client value:\n%s", slug, got)
		}
		if e := entryFor(t, mgr, "reviewer", slug); e.ModelValue != "" {
			t.Errorf("%s pass-through must record no model_value, got %q", slug, e.ModelValue)
		}
		if d := resultFor(t, results, slug).Detail; !strings.Contains(d, "model") || strings.Contains(d, "applied") {
			t.Errorf("%s detail = %q", slug, d)
		}
	}

	// The identity copy is untouched by a client-only policy.
	if got := readFile(t, projectedPath(home, "reviewer")); got != canonical {
		t.Errorf("identity projection must stay byte-identical:\n%s", got)
	}
	if e := entryFor(t, mgr, "reviewer", "claude-code"); e.ModelValue != "" {
		t.Errorf("identity model_value = %q", e.ModelValue)
	}

	st := clientStatus(t, mgr, "opencode")
	if st.State != StateInSync || st.ModelValue != sonnetID {
		t.Errorf("opencode status = %+v", st)
	}

	// A re-sync is unchanged.
	results, err = mgr.Sync(ctx, nil, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Action != ActionUnchanged {
			t.Errorf("re-sync %s = %q", r.Client, r.Action)
		}
	}
}

func TestClientModel_DetailWithoutDeclarationOrDrops(t *testing.T) {
	mgr, home, _ := newTestManager(t, "alpha")
	enableRenderTargets(t, home)
	mgr.SetModelPolicy(clientPolicy(sonnetID, nil))
	results, err := mgr.Sync(context.Background(), nil, SyncOptions{Clients: []string{"opencode"}, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	r := resultFor(t, results, "opencode")
	if r.Action != ActionWouldCopy || r.Detail != `model preference "`+sonnetID+`" applied (policy)` {
		t.Errorf("dry-run row = %+v", r)
	}
}

func TestClientModel_EqualToAuthorStillWritten(t *testing.T) {
	mgr, home, registryDir := newTestManager(t)
	enableRenderTargets(t, home)
	writeAgent(t, registryDir, "a", agentContentWithModel("a", "inherit"))
	mgr.SetModelPolicy(&registry.ModelPolicy{
		Rewrite: true,
		Clients: map[string]registry.ClientModelPolicy{"gemini": {Default: "inherit"}},
	})
	if _, err := mgr.Sync(context.Background(), nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, renderedPath(home, "gemini", "a")); !strings.Contains(got, "\nmodel: inherit\n") {
		t.Fatalf("a client value equal to the author's alias must still be written:\n%s", got)
	}
	if got := readFile(t, projectedPath(home, "a")); got != agentContentWithModel("a", "inherit") {
		t.Fatalf("identity must be untouched:\n%s", got)
	}
}

func TestClientModel_RewriteFalseIsPassThrough(t *testing.T) {
	mgr, home, _ := newTestManager(t, "alpha")
	enableRenderTargets(t, home)
	pol := clientPolicy(sonnetID, nil)
	pol.Rewrite = false
	mgr.SetModelPolicy(pol)
	if _, err := mgr.Sync(context.Background(), nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	canonical := agentContent("alpha", "Review the code.")
	if got := readFile(t, renderedPath(home, "opencode", "alpha")); got != passThroughRender(t, "opencode", "alpha", canonical) {
		t.Fatalf("rewrite: false must render byte-identical output:\n%s", got)
	}
	if st := clientStatus(t, mgr, "opencode"); st.State != StateInSync {
		t.Fatalf("status = %+v", st)
	}
}

func TestClientModel_PolicyChangeGoesStaleThenResyncs(t *testing.T) {
	mgr, home, _ := newTestManager(t, "alpha")
	enableRenderTargets(t, home)
	ctx := context.Background()
	mgr.SetModelPolicy(clientPolicy(sonnetID, nil))
	if _, err := mgr.Sync(ctx, nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	mgr.SetModelPolicy(clientPolicy(sonnetID, map[string]string{"alpha": opusID}))
	st := clientStatus(t, mgr, "opencode")
	if st.State != StateStale || st.Detail != "model preference policy changed; run 'gridctl skill project sync --kind agent'" {
		t.Fatalf("override edit should read stale with the policy detail, got %+v", st)
	}
	for _, slug := range []string{"claude-code", "copilot", "gemini"} {
		if s := clientStatus(t, mgr, slug); s.State != StateInSync {
			t.Errorf("%s unaffected by the opencode edit, got %+v", slug, s)
		}
	}

	results, err := mgr.Sync(ctx, nil, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r := resultFor(t, results, "opencode"); r.Action != ActionUpdated {
		t.Fatalf("policy-aware sync action = %q, want updated", r.Action)
	}
	if got := readFile(t, renderedPath(home, "opencode", "alpha")); !strings.Contains(got, "model: "+opusID+"\n") {
		t.Fatalf("new value not written:\n%s", got)
	}
	if e := entryFor(t, mgr, "alpha", "opencode"); e.ModelValue != opusID {
		t.Errorf("model_value = %q", e.ModelValue)
	}
	if st := clientStatus(t, mgr, "opencode"); st.State != StateInSync {
		t.Errorf("post-sync status = %+v", st)
	}
}

func TestClientModel_ValueAddedReadsPolicyStale(t *testing.T) {
	mgr, home, _ := newTestManager(t, "alpha")
	enableRenderTargets(t, home)
	if _, err := mgr.Sync(context.Background(), nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	mgr.SetModelPolicy(clientPolicy(sonnetID, nil))
	if st := clientStatus(t, mgr, "opencode"); st.State != StateStale || !strings.HasPrefix(st.Detail, "model preference policy changed") {
		t.Fatalf("a newly added client value should read stale (policy), got %+v", st)
	}
}

func TestClientModel_RendererChangeUnderLoadedPolicyReadsGenericStale(t *testing.T) {
	for _, tc := range []struct {
		name string
		pol  *registry.ModelPolicy
	}{
		{"empty policy", &registry.ModelPolicy{}},
		{"client value", clientPolicy(sonnetID, nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr, home, _ := newTestManager(t, "alpha")
			enableRenderTargets(t, home)
			ctx := context.Background()
			mgr.SetModelPolicy(tc.pol)
			if _, err := mgr.Sync(ctx, nil, SyncOptions{}); err != nil {
				t.Fatal(err)
			}

			// Simulate an upgrade whose renderer emits different bytes: the
			// destination holds an older render the lock agrees on.
			dest := renderedPath(home, "opencode", "alpha")
			oldRender := []byte(strings.Replace(readFile(t, dest), "mode: subagent\n", "mode: subagent\nlegacy: emission\n", 1))
			if err := os.WriteFile(dest, oldRender, 0o644); err != nil {
				t.Fatal(err)
			}
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

			st := clientStatus(t, mgr, "opencode")
			if st.State != StateStale || st.Detail != "projection output changed; run 'gridctl skill project sync --kind agent'" {
				t.Fatalf("renderer change under a loaded policy = %+v, want stale with the generic detail", st)
			}

			// Without stack context the existing branch is unchanged.
			mgr.SetModelPolicy(nil)
			if st := clientStatus(t, mgr, "opencode"); st.State != StateInSync {
				t.Fatalf("nil-policy status = %+v, want in-sync", st)
			}

			mgr.SetModelPolicy(tc.pol)
			if _, err := mgr.Sync(ctx, nil, SyncOptions{}); err != nil {
				t.Fatal(err)
			}
			if st := clientStatus(t, mgr, "opencode"); st.State != StateInSync {
				t.Fatalf("post-sync status = %+v", st)
			}
		})
	}
}

func TestClientModel_RemovalReconcilesBack(t *testing.T) {
	for _, tc := range []struct {
		name  string
		after *registry.ModelPolicy
	}{
		{"clients removed", &registry.ModelPolicy{Rewrite: true}},
		{"rewrite false", func() *registry.ModelPolicy { p := clientPolicy(sonnetID, nil); p.Rewrite = false; return p }()},
		{"block deleted", &registry.ModelPolicy{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr, home, _ := newTestManager(t, "alpha")
			enableRenderTargets(t, home)
			ctx := context.Background()
			mgr.SetModelPolicy(clientPolicy(sonnetID, nil))
			if _, err := mgr.Sync(ctx, nil, SyncOptions{}); err != nil {
				t.Fatal(err)
			}

			mgr.SetModelPolicy(tc.after)
			if st := clientStatus(t, mgr, "opencode"); st.State != StateStale || !strings.HasPrefix(st.Detail, "model preference policy changed") {
				t.Fatalf("removed value should read stale (policy), got %+v", st)
			}
			results, err := mgr.Sync(ctx, nil, SyncOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if r := resultFor(t, results, "opencode"); r.Action != ActionUpdated || r.BackupPath != "" {
				t.Fatalf("reconcile-back row = %+v", r)
			}
			want := passThroughRender(t, "opencode", "alpha", agentContent("alpha", "Review the code."))
			if got := readFile(t, renderedPath(home, "opencode", "alpha")); got != want {
				t.Fatalf("reconcile-back must restore the pre-feature render:\n%s", got)
			}
			if e := entryFor(t, mgr, "alpha", "opencode"); e.ModelValue != "" {
				t.Errorf("model_value must clear, got %q", e.ModelValue)
			}
		})
	}
}

func TestClientModel_DaemonReconcileApplies(t *testing.T) {
	mgr, home, _ := newTestManager(t, "alpha")
	enableRenderTargets(t, home)
	ctx := context.Background()
	if _, err := mgr.Sync(ctx, nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	// The daemon installs the compiled policy (startup or hot reload) and
	// reconciles the recorded set.
	mgr.SetModelPolicy(clientPolicy(sonnetID, nil))
	results, err := mgr.Reconcile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if r := resultFor(t, results, "opencode"); r.Action != ActionUpdated {
		t.Fatalf("reconcile row = %+v", r)
	}
	if got := readFile(t, renderedPath(home, "opencode", "alpha")); !strings.Contains(got, "model: "+sonnetID+"\n") {
		t.Fatalf("reconcile did not apply the client value:\n%s", got)
	}

	// A clients edit picked up by hot reload reaches disk on the next
	// reconcile.
	mgr.SetModelPolicy(clientPolicy(opusID, nil))
	if _, err := mgr.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, renderedPath(home, "opencode", "alpha")); !strings.Contains(got, "model: "+opusID+"\n") {
		t.Fatalf("reconcile did not apply the edited value:\n%s", got)
	}
}

func TestClientModel_StacklessPreservesThenForceReverts(t *testing.T) {
	mgr, home, _ := newTestManager(t, "alpha")
	enableRenderTargets(t, home)
	ctx := context.Background()
	mgr.SetModelPolicy(clientPolicy(sonnetID, nil))
	if _, err := mgr.Sync(ctx, nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	dest := renderedPath(home, "opencode", "alpha")
	rewritten := readFile(t, dest)

	mgr.SetModelPolicy(nil)
	results, err := mgr.Sync(ctx, nil, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r := resultFor(t, results, "opencode")
	if r.Action != ActionUnchanged || r.Detail != "model policy: unknown (no stack loaded); rewritten projection preserved" {
		t.Fatalf("stackless sync row = %+v", r)
	}
	if readFile(t, dest) != rewritten {
		t.Fatal("stackless sync must leave the rewritten file untouched")
	}
	st := clientStatus(t, mgr, "opencode")
	if st.State != StateInSync || st.Detail != "model policy: unknown (no stack loaded); rewritten projection preserved" || st.ModelValue != sonnetID {
		t.Fatalf("stackless status = %+v", st)
	}

	// --force re-renders without the key; a clean file needs no backup
	// (its bytes are reproducible from the canonical plus the policy).
	results, err = mgr.Sync(ctx, nil, SyncOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	r = resultFor(t, results, "opencode")
	if r.Action != ActionUpdated || r.BackupPath != "" {
		t.Fatalf("forced stackless revert row = %+v, want updated with no backup", r)
	}
	if got := readFile(t, dest); strings.Contains(got, "model:") {
		t.Fatalf("--force must re-render without the key:\n%s", got)
	}
	if e := entryFor(t, mgr, "alpha", "opencode"); e.ModelValue != "" {
		t.Errorf("model_value must clear, got %q", e.ModelValue)
	}
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "project-backups", "agent", "opencode", "alpha")); !os.IsNotExist(err) {
		t.Errorf("no backup directory expected for a clean forced revert: %v", err)
	}
}

func TestClientModel_StacklessDriftSkippedThenForceBacksUp(t *testing.T) {
	mgr, home, _ := newTestManager(t, "alpha")
	enableRenderTargets(t, home)
	ctx := context.Background()
	mgr.SetModelPolicy(clientPolicy(sonnetID, nil))
	if _, err := mgr.Sync(ctx, nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	dest := renderedPath(home, "opencode", "alpha")
	edited := strings.Replace(readFile(t, dest), "Review the code.", "Mine now.", 1)
	if err := os.WriteFile(dest, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr.SetModelPolicy(nil)
	results, err := mgr.Sync(ctx, nil, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r := resultFor(t, results, "opencode"); r.Action != ActionSkippedDrift {
		t.Fatalf("stackless drifted row = %+v", r)
	}

	results, err = mgr.Sync(ctx, nil, SyncOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	r := resultFor(t, results, "opencode")
	wantDir := filepath.Join(home, ".gridctl", "project-backups", "agent", "opencode", "alpha")
	if r.Action != ActionUpdated || filepath.Dir(r.BackupPath) != wantDir {
		t.Fatalf("forced drifted row = %+v, want a backup under %s", r, wantDir)
	}
	if readFile(t, r.BackupPath) != edited {
		t.Error("backup must hold the hand-edited bytes")
	}
}

func TestClientModel_HandEditDriftsUnderPolicy(t *testing.T) {
	mgr, home, _ := newTestManager(t, "alpha")
	enableRenderTargets(t, home)
	ctx := context.Background()
	mgr.SetModelPolicy(clientPolicy(sonnetID, nil))
	if _, err := mgr.Sync(ctx, nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	dest := renderedPath(home, "opencode", "alpha")
	edited := strings.Replace(readFile(t, dest), sonnetID, "openai/gpt-5", 1)
	if err := os.WriteFile(dest, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	if st := clientStatus(t, mgr, "opencode"); st.State != StateDrifted {
		t.Fatalf("hand edit status = %+v, want drifted", st)
	}
	results, err := mgr.Sync(ctx, nil, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r := resultFor(t, results, "opencode"); r.Action != ActionSkippedDrift {
		t.Fatalf("policy-aware sync over drift = %+v, want skipped-drift", r)
	}
	if readFile(t, dest) != edited {
		t.Fatal("drifted file must not be written without --force")
	}

	results, err = mgr.Sync(ctx, nil, SyncOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	r := resultFor(t, results, "opencode")
	if r.Action != ActionUpdated || !strings.HasPrefix(r.BackupPath, filepath.Join(home, ".gridctl", "project-backups", "agent", "opencode", "alpha")+string(filepath.Separator)) {
		t.Fatalf("forced row = %+v, want a backup", r)
	}
	if got := readFile(t, dest); !strings.Contains(got, "model: "+sonnetID+"\n") {
		t.Fatalf("--force must restore the policy render:\n%s", got)
	}
}

func TestClientModel_AdoptStillRefused(t *testing.T) {
	mgr, home, _ := newTestManager(t, "alpha")
	enableRenderTargets(t, home)
	ctx := context.Background()
	mgr.SetModelPolicy(clientPolicy(sonnetID, nil))
	if _, err := mgr.Sync(ctx, nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err := mgr.Adopt(ctx, "alpha", "opencode")
	var refusal *AdoptRefusal
	if !errors.As(err, &refusal) || !strings.Contains(err.Error(), "lossy opencode render") {
		t.Fatalf("adopt on a rendered target must keep the lossy-render refusal, got %v", err)
	}
}
