package contexts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakePinner struct {
	syncs   int
	renames [][2]string
	drift   []string
	syncErr error
}

func (f *fakePinner) Sync(_ context.Context, _ []PinDocument) (*PinSyncResult, error) {
	f.syncs++
	if f.syncErr != nil {
		return nil, f.syncErr
	}
	return &PinSyncResult{Drifted: append([]string(nil), f.drift...)}, nil
}

func (f *fakePinner) Rename(_ context.Context, from, to string) error {
	f.renames = append(f.renames, [2]string{from, to})
	return nil
}

func TestPinner_SyncAllAndNamedClientNotDryRun(t *testing.T) {
	m := newTestManager(t, ".claude")
	initCanonical(t, m, "# Canon\n")
	p := &fakePinner{drift: []string{canonicalFileName}}
	m.SetPinner(p)
	ctx := context.Background()

	results, err := m.SyncAll(ctx, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if p.syncs != 1 {
		t.Fatalf("syncs after SyncAll = %d", p.syncs)
	}
	marked := 0
	for _, r := range results {
		if r.PinDrift {
			marked++
			if r.Detail == "" || !strings.Contains(r.Detail, "pin drift: "+canonicalFileName) {
				t.Fatalf("detail = %q", r.Detail)
			}
		}
	}
	if marked == 0 {
		t.Fatal("no row marked pin_drift")
	}

	if _, err := m.SyncAll(ctx, SyncOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if p.syncs != 1 {
		t.Fatalf("dry-run pinned: syncs = %d", p.syncs)
	}

	if _, err := m.SyncClientDetailed(ctx, "claude-code", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if p.syncs != 2 {
		t.Fatalf("syncs after named client = %d", p.syncs)
	}
	if _, err := m.SyncClientDetailed(ctx, "claude-code", SyncOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if p.syncs != 2 {
		t.Fatalf("named dry-run pinned: syncs = %d", p.syncs)
	}
}

func TestPinner_ErrorDoesNotFailSync(t *testing.T) {
	m := newTestManager(t, ".claude")
	initCanonical(t, m, "# Canon\n")
	m.SetPinner(&fakePinner{syncErr: errors.New("pin store down")})
	if _, err := m.SyncAll(context.Background(), SyncOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestPinner_FragmentRowsMarkedSelectively(t *testing.T) {
	m := newTestManager(t, ".claude")
	initCanonical(t, m, "# Canon\n")
	if _, err := m.AddFragment("style", "be concise\n"); err != nil {
		t.Fatal(err)
	}
	p := &fakePinner{drift: []string{"style"}}
	m.SetPinner(p)
	results, err := m.SyncAll(context.Background(), SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var styleMarked, otherMarked bool
	for _, r := range results {
		if r.Fragment == "style" && r.PinDrift {
			styleMarked = true
		}
		if r.Fragment != "" && r.Fragment != "style" && r.PinDrift {
			otherMarked = true
		}
	}
	if !styleMarked {
		t.Fatal("style row was not marked")
	}
	if otherMarked {
		t.Fatal("unrelated fragment row was marked")
	}
}

func TestActivateFragments_RenamesPin(t *testing.T) {
	m := newTestManager(t)
	initCanonical(t, m, "# Canon\n\n\n")
	p := &fakePinner{}
	m.SetPinner(p)
	res, err := m.AddFragment("style", "rules\n")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Migrated {
		t.Fatal("expected migration")
	}
	if len(p.renames) != 1 || p.renames[0] != [2]string{canonicalFileName, migratedFragmentName} {
		t.Fatalf("renames = %v", p.renames)
	}
}

func TestPinDocuments_BothModesAndReadError(t *testing.T) {
	m := newTestManager(t)
	if err := os.MkdirAll(m.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.CanonicalPath(), []byte("# Canon\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	docs, err := m.PinDocuments(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Name != canonicalFileName || string(docs[0].Raw) != "# Canon\r\n" {
		t.Fatalf("single-file docs = %+v", docs)
	}

	if _, err := m.AddFragment("style", "---\ndescription: style\n---\n\nbody\n"); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(m.FragmentsDir(), "broken.md")
	if err := os.WriteFile(bad, []byte("---\npaths: [\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	docs, err = m.PinDocuments(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 3 {
		t.Fatalf("fragment docs = %d, want 3 (including the unreadable one)", len(docs))
	}
	var sawErr bool
	for _, d := range docs {
		if d.Name == "broken" {
			sawErr = d.ReadErr != nil && len(d.Raw) > 0
		}
		if d.Name == "style" && (d.Description != "style" || d.ReadErr != nil) {
			t.Fatalf("style doc = %+v", d)
		}
	}
	if !sawErr {
		t.Fatal("unreadable fragment did not carry ReadErr")
	}
}
