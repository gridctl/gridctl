package wiring

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/provisioner"
)

// fakeClient is a minimal mcpServers-shaped provisioner whose config
// lives at an injected path, so ownership decisions are exercised
// without touching real client trees.
type fakeClient struct {
	slug       string
	configPath string
	detected   bool
	bridge     bool
}

func (f *fakeClient) Name() string      { return "Fake " + f.slug }
func (f *fakeClient) Slug() string      { return f.slug }
func (f *fakeClient) NeedsBridge() bool { return f.bridge }

func (f *fakeClient) Detect() (string, bool) {
	if !f.detected {
		return "", false
	}
	return f.configPath, true
}

func (f *fakeClient) readConfig() (map[string]any, error) {
	data, err := os.ReadFile(f.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func (f *fakeClient) servers(data map[string]any) map[string]any {
	servers, _ := data["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	return servers
}

func (f *fakeClient) IsLinked(configPath, serverName string) (bool, error) {
	data, err := f.readConfig()
	if err != nil {
		return false, err
	}
	_, ok := f.servers(data)[serverName]
	return ok, nil
}

func (f *fakeClient) Link(configPath string, opts provisioner.LinkOptions) error {
	data, err := f.readConfig()
	if err != nil {
		return err
	}
	servers := f.servers(data)
	servers[opts.ServerName] = f.PlannedEntry(opts)
	data["mcpServers"] = servers
	return f.write(data)
}

func (f *fakeClient) Unlink(configPath, serverName string) error {
	data, err := f.readConfig()
	if err != nil {
		return err
	}
	servers := f.servers(data)
	delete(servers, serverName)
	data["mcpServers"] = servers
	return f.write(data)
}

func (f *fakeClient) ListServers(configPath string) ([]provisioner.ServerEntry, error) {
	data, err := f.readConfig()
	if err != nil {
		return nil, err
	}
	var out []provisioner.ServerEntry
	for name, v := range f.servers(data) {
		raw, _ := v.(map[string]any)
		out = append(out, provisioner.ServerEntry{Name: name, Raw: raw})
	}
	return out, nil
}

func (f *fakeClient) PlannedEntry(opts provisioner.LinkOptions) map[string]any {
	return map[string]any{"url": opts.GatewayURL}
}

func (f *fakeClient) write(data map[string]any) error {
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(f.configPath, out, 0o644)
}

func (f *fakeClient) setEntry(t *testing.T, name string, value map[string]any) {
	t.Helper()
	data, err := f.readConfig()
	if err != nil {
		t.Fatal(err)
	}
	servers := f.servers(data)
	servers[name] = value
	data["mcpServers"] = servers
	if err := f.write(data); err != nil {
		t.Fatal(err)
	}
}

func (f *fakeClient) entryValue(t *testing.T, name string) (map[string]any, bool) {
	t.Helper()
	data, err := f.readConfig()
	if err != nil {
		t.Fatal(err)
	}
	v, ok := f.servers(data)[name].(map[string]any)
	return v, ok
}

func testManager(t *testing.T) (*Manager, *fakeClient) {
	t.Helper()
	home := t.TempDir()
	fake := &fakeClient{slug: "fake", configPath: filepath.Join(home, "fake-config.json"), detected: true}
	return NewManagerWith(home, provisioner.NewRegistryWith(fake)), fake
}

func linkOpts(name string) provisioner.LinkOptions {
	url := provisioner.GatewayHTTPURL(8180)
	return provisioner.LinkOptions{GatewayURL: url, Port: 8180, ServerName: name}
}

func mustLink(t *testing.T, m *Manager, f *fakeClient, opts provisioner.LinkOptions) Result {
	t.Helper()
	res, err := m.LinkClient(context.Background(), f, f.configPath, opts)
	if err != nil {
		t.Fatalf("LinkClient: %v", err)
	}
	return res
}

func TestLinkClient_RecordsOwnership(t *testing.T) {
	m, f := testManager(t)

	res := mustLink(t, m, f, linkOpts("gridctl"))
	if res.Action != ActionLinked {
		t.Fatalf("action = %q, want linked", res.Action)
	}

	l, err := project.NewStore(m.home).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	e := l.Get(project.KindWiring, "fake", "gridctl")
	if e == nil {
		t.Fatal("no wiring entry recorded")
	}
	if e.Path != f.configPath+"#gridctl" {
		t.Errorf("composite path = %q", e.Path)
	}
	if e.ConfigPath != f.configPath {
		t.Errorf("config path = %q", e.ConfigPath)
	}
	if len(e.Hashes) != 1 || !strings.HasPrefix(e.Hashes[0], project.HashScheme) {
		t.Errorf("hashes = %v", e.Hashes)
	}

	// Relink is a recorded no-op.
	if res := mustLink(t, m, f, linkOpts("gridctl")); res.Action != ActionUnchanged {
		t.Errorf("relink action = %q, want unchanged", res.Action)
	}
}

func TestLinkClient_AdoptsIdenticalForeignEntry(t *testing.T) {
	m, f := testManager(t)
	opts := linkOpts("gridctl")
	f.setEntry(t, "gridctl", f.PlannedEntry(opts))

	res := mustLink(t, m, f, opts)
	if res.Action != ActionAdopted {
		t.Fatalf("action = %q, want adopted", res.Action)
	}

	l, _ := project.NewStore(m.home).Load(context.Background())
	e := l.Get(project.KindWiring, "fake", "gridctl")
	if e == nil || e.CreatedByGridctl {
		t.Fatalf("adopted entry = %+v, want recorded with CreatedByGridctl=false", e)
	}
}

func TestLinkClient_ForeignEntryRefusedWithoutForce(t *testing.T) {
	m, f := testManager(t)
	foreign := map[string]any{"url": "https://example.com/mcp"}
	f.setEntry(t, "gridctl", foreign)

	res := mustLink(t, m, f, linkOpts("gridctl"))
	if res.Action != ActionSkippedForeign {
		t.Fatalf("action = %q, want skipped-foreign", res.Action)
	}
	if v, _ := f.entryValue(t, "gridctl"); v["url"] != "https://example.com/mcp" {
		t.Error("foreign entry was modified")
	}

	forced := linkOpts("gridctl")
	forced.Force = true
	if res := mustLink(t, m, f, forced); res.Action != ActionUpdated {
		t.Fatalf("forced action = %q, want updated", res.Action)
	}
	if v, _ := f.entryValue(t, "gridctl"); v["url"] != forced.GatewayURL {
		t.Error("forced link did not rewrite the entry")
	}
}

func TestLinkClient_LegacyLocalhostEntryGetsMigrationHint(t *testing.T) {
	m, f := testManager(t)
	f.setEntry(t, "gridctl", map[string]any{"url": "http://localhost:8180/sse"})

	res := mustLink(t, m, f, linkOpts("gridctl"))
	if res.Action != ActionSkippedForeign {
		t.Fatalf("action = %q, want skipped-foreign", res.Action)
	}
	if !strings.Contains(res.Detail, "before ownership recording") {
		t.Errorf("detail lacks migration hint: %q", res.Detail)
	}
}

func TestLinkClient_DriftRefusedWithoutForce(t *testing.T) {
	m, f := testManager(t)
	mustLink(t, m, f, linkOpts("gridctl"))
	f.setEntry(t, "gridctl", map[string]any{"url": "http://localhost:9999/mcp", "env": map[string]any{"KEY": "edited"}})

	res := mustLink(t, m, f, linkOpts("gridctl"))
	if res.Action != ActionSkippedDrift {
		t.Fatalf("action = %q, want skipped-drift", res.Action)
	}

	forced := linkOpts("gridctl")
	forced.Force = true
	if res := mustLink(t, m, f, forced); res.Action != ActionUpdated {
		t.Fatalf("forced action = %q, want updated", res.Action)
	}
}

func TestUnlinkClient_RemovesEntryAndRecord(t *testing.T) {
	m, f := testManager(t)
	f.setEntry(t, "keepme", map[string]any{"url": "https://example.com/other"})
	mustLink(t, m, f, linkOpts("gridctl"))

	res, err := m.UnlinkClient(context.Background(), f, f.configPath, "gridctl", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionRemoved {
		t.Fatalf("action = %q, want removed", res.Action)
	}
	if _, ok := f.entryValue(t, "gridctl"); ok {
		t.Error("entry still present after unlink")
	}
	if v, ok := f.entryValue(t, "keepme"); !ok || v["url"] != "https://example.com/other" {
		t.Error("sibling entry did not survive unlink")
	}

	res, err = m.UnlinkClient(context.Background(), f, f.configPath, "gridctl", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionNotLinked {
		t.Errorf("repeat unlink action = %q, want not-linked", res.Action)
	}
}

func TestUnlinkClient_NeverDeletesForeign(t *testing.T) {
	m, f := testManager(t)
	f.setEntry(t, "gridctl", map[string]any{"url": "https://example.com/mine"})

	for _, force := range []bool{false, true} {
		res, err := m.UnlinkClient(context.Background(), f, f.configPath, "gridctl", force, false)
		if err != nil {
			t.Fatal(err)
		}
		if res.Action != ActionSkippedForeign {
			t.Fatalf("force=%v action = %q, want skipped-foreign", force, res.Action)
		}
	}
	if _, ok := f.entryValue(t, "gridctl"); !ok {
		t.Fatal("foreign entry was deleted")
	}
}

func TestUnlinkClient_DriftRefusedThenForced(t *testing.T) {
	m, f := testManager(t)
	mustLink(t, m, f, linkOpts("gridctl"))
	f.setEntry(t, "gridctl", map[string]any{"url": "http://localhost:9999/mcp"})

	res, err := m.UnlinkClient(context.Background(), f, f.configPath, "gridctl", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionSkippedDrift {
		t.Fatalf("action = %q, want skipped-drift", res.Action)
	}

	res, err = m.UnlinkClient(context.Background(), f, f.configPath, "gridctl", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionRemoved {
		t.Fatalf("forced action = %q, want removed", res.Action)
	}
}

func TestUnlinkClient_PurgesRecordWhenKeyAlreadyGone(t *testing.T) {
	m, f := testManager(t)
	mustLink(t, m, f, linkOpts("gridctl"))
	if err := f.Unlink(f.configPath, "gridctl"); err != nil { // external removal
		t.Fatal(err)
	}

	res, err := m.UnlinkClient(context.Background(), f, f.configPath, "gridctl", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionAlreadyGone {
		t.Fatalf("action = %q, want already-gone", res.Action)
	}
	l, _ := project.NewStore(m.home).Load(context.Background())
	if l.Get(project.KindWiring, "fake", "gridctl") != nil {
		t.Error("stale record survived; relink would trip over it")
	}
}

func TestAdopt_RecordsCurrentValue(t *testing.T) {
	m, f := testManager(t)
	mustLink(t, m, f, linkOpts("gridctl"))
	edited := map[string]any{"url": "http://localhost:9999/mcp", "headers": map[string]any{"X-Env": "prod"}}
	f.setEntry(t, "gridctl", edited)

	res, err := m.Adopt(context.Background(), "fake", "gridctl")
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionAdopted {
		t.Fatalf("action = %q, want adopted", res.Action)
	}

	// The edit is now recognized: unlink no longer refuses.
	ures, err := m.UnlinkClient(context.Background(), f, f.configPath, "gridctl", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if ures.Action != ActionRemoved {
		t.Errorf("post-adopt unlink = %q, want removed", ures.Action)
	}
}

func TestAdopt_RefusesMissingEntry(t *testing.T) {
	m, f := testManager(t)
	_ = f
	if _, err := m.Adopt(context.Background(), "fake", "gridctl"); err == nil {
		t.Fatal("expected error adopting a missing entry")
	}
}

func TestGroupLinks_TwoEntriesOneFile(t *testing.T) {
	m, f := testManager(t)
	mustLink(t, m, f, linkOpts("gridctl"))

	group := provisioner.LinkOptions{
		GatewayURL: provisioner.GroupGatewayHTTPURL(8180, "dev"),
		Port:       8180, ServerName: "gridctl-dev", Group: "dev",
	}
	if res := mustLink(t, m, f, group); res.Action != ActionLinked {
		t.Fatalf("group link action = %q", res.Action)
	}

	l, _ := project.NewStore(m.home).Load(context.Background())
	if l.Get(project.KindWiring, "fake", "gridctl") == nil || l.Get(project.KindWiring, "fake", "gridctl-dev") == nil {
		t.Fatal("expected two recorded entries in one config file")
	}
}

func TestStatuses_StateMatrix(t *testing.T) {
	m, f := testManager(t)
	ctx := context.Background()
	opts := StatusOptions{Port: 8180}

	// missing: detected, nothing recorded, nothing present.
	rows, err := m.Statuses(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].State != StateMissing {
		t.Fatalf("rows = %+v, want one missing row", rows)
	}
	if NeedsAttention(rows) {
		t.Error("missing rows are advisory and must not need attention")
	}

	// in-sync after link.
	mustLink(t, m, f, linkOpts("gridctl"))
	rows, _ = m.Statuses(ctx, opts)
	if len(rows) != 1 || rows[0].State != StateInSync {
		t.Fatalf("rows = %+v, want one in-sync row", rows)
	}

	// stale when the gateway port moves.
	rows, _ = m.Statuses(ctx, StatusOptions{Port: 9999})
	if rows[0].State != StateStale {
		t.Errorf("state = %q, want stale on port change", rows[0].State)
	}

	// drifted on hand edit.
	f.setEntry(t, "gridctl", map[string]any{"url": "http://localhost:8180/mcp", "extra": true})
	rows, _ = m.Statuses(ctx, opts)
	if rows[0].State != StateDrifted {
		t.Errorf("state = %q, want drifted", rows[0].State)
	}

	// target-missing with key-gone vs file-gone detail.
	if err := f.Unlink(f.configPath, "gridctl"); err != nil {
		t.Fatal(err)
	}
	rows, _ = m.Statuses(ctx, opts)
	if rows[0].State != StateTargetMissing || !strings.Contains(rows[0].Detail, "file still present") {
		t.Errorf("row = %+v, want target-missing with key-gone detail", rows[0])
	}
	if err := os.Remove(f.configPath); err != nil {
		t.Fatal(err)
	}
	rows, _ = m.Statuses(ctx, opts)
	if rows[0].State != StateTargetMissing || !strings.Contains(rows[0].Detail, "no longer exists") {
		t.Errorf("row = %+v, want target-missing with file-gone detail", rows[0])
	}
}

func TestStatuses_ForeignRow(t *testing.T) {
	m, f := testManager(t)
	f.setEntry(t, "gridctl", map[string]any{"url": "http://localhost:8180/sse"})

	rows, err := m.Statuses(context.Background(), StatusOptions{Port: 8180})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].State != StateForeign {
		t.Fatalf("rows = %+v, want one foreign row", rows)
	}
	if !strings.Contains(rows[0].Detail, "before ownership recording") {
		t.Errorf("foreign detail lacks migration hint: %q", rows[0].Detail)
	}
}

func TestSync_LinksDetectedClients(t *testing.T) {
	m, f := testManager(t)
	results, err := m.Sync(context.Background(), SyncOptions{
		ServerName: "gridctl",
		GatewayURL: provisioner.GatewayHTTPURL(8180),
		Port:       8180,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Action != ActionLinked {
		t.Fatalf("results = %+v", results)
	}
	if v, ok := f.entryValue(t, "gridctl"); !ok || v["url"] != "http://localhost:8180/mcp" {
		t.Errorf("entry = %v", v)
	}
	if HasFailures(results) {
		t.Error("clean sync must not report failures")
	}
}

func TestValueHash_FormatIndependent(t *testing.T) {
	// A YAML decode yields int; a JSON decode yields float64. Both must
	// hash identically, or a client rewriting its file false-drifts.
	a := map[string]any{"timeout": 300, "url": "http://localhost:8180/mcp"}
	b := map[string]any{"url": "http://localhost:8180/mcp", "timeout": float64(300)}
	ha, err := ValueHash(a)
	if err != nil {
		t.Fatal(err)
	}
	hb, err := ValueHash(b)
	if err != nil {
		t.Fatal(err)
	}
	if ha != hb {
		t.Errorf("hashes differ: %s vs %s", ha, hb)
	}

	c := map[string]any{"nested": map[any]any{"k": "v"}}
	if _, err := ValueHash(c); err != nil {
		t.Errorf("map[any]any keys must normalize: %v", err)
	}
}

func TestAppendHash_DedupesAndCaps(t *testing.T) {
	var hashes []string
	for i := 0; i < 8; i++ {
		hashes = appendHash(hashes, string(rune('a'+i)))
	}
	if len(hashes) != maxHashHistory {
		t.Fatalf("len = %d, want %d", len(hashes), maxHashHistory)
	}
	hashes = appendHash(hashes, hashes[0])
	if len(hashes) != maxHashHistory {
		t.Errorf("dedupe grew the list: %v", hashes)
	}
	if hashes[len(hashes)-1] != "d" {
		t.Errorf("re-appended hash should be newest: %v", hashes)
	}
}

func TestHashHistory_OldShapeStillRecognized(t *testing.T) {
	m, f := testManager(t)
	mustLink(t, m, f, linkOpts("gridctl"))

	// A newer gridctl writes a different shape (URL changed): update.
	next := linkOpts("gridctl")
	next.GatewayURL = "http://localhost:8180/mcp?client=ci"
	if res := mustLink(t, m, f, next); res.Action != ActionUpdated {
		t.Fatalf("action = %q, want updated", res.Action)
	}

	// The file still holds the OLD shape somewhere else? Roll the file
	// back to the first shape (simulates a client restoring a backup):
	// both hashes are in history, so this is not drift.
	f.setEntry(t, "gridctl", map[string]any{"url": "http://localhost:8180/mcp"})
	res, err := m.UnlinkClient(context.Background(), f, f.configPath, "gridctl", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionRemoved {
		t.Errorf("action = %q, want removed (old shape is still ours)", res.Action)
	}
}
