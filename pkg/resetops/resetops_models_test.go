package resetops

import (
	"context"
	"testing"

	"github.com/gridctl/gridctl/pkg/modelsync"
)

type fakeModels struct {
	statuses []modelsync.Status
	unsyncs  int
	force    bool
}

func (f *fakeModels) Statuses(context.Context) ([]modelsync.Status, error) {
	return f.statuses, nil
}

func (f *fakeModels) Unsync(_ context.Context, opts modelsync.UnsyncOptions) ([]modelsync.UnsyncResult, error) {
	f.unsyncs++
	f.force = opts.Force
	var out []modelsync.UnsyncResult
	for _, s := range f.statuses {
		action := "removed"
		if s.State == modelsync.StateDrifted && !opts.Force {
			action = "kept-drift"
		}
		out = append(out, modelsync.UnsyncResult{Target: s.Target, Client: s.Client, Path: s.Path, Action: action})
	}
	return out, nil
}

func TestPreview_ModelsRows(t *testing.T) {
	home := sandboxHome(t)
	m := &Managers{
		Home: home,
		Models: &fakeModels{statuses: []modelsync.Status{
			{Target: "litellm-fragment", Client: "litellm", State: modelsync.StateInSync, Path: home + "/f.yaml"},
			{Target: "opencode", Client: "opencode", State: modelsync.StateDrifted, Path: home + "/oc.json"},
			{Target: "litellm-include", Client: "litellm", State: modelsync.StateNeverSynced},
		}},
	}
	doc, err := m.Preview(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	var wouldRemove, keptDrift, total int
	for _, r := range doc.Rows {
		if r.Kind != "models" {
			continue
		}
		total++
		switch r.Action {
		case ActionWouldRemove:
			wouldRemove++
		case ActionKeptDrift:
			keptDrift++
		}
	}
	if total != 2 || wouldRemove != 1 || keptDrift != 1 {
		t.Errorf("models rows: total=%d would-remove=%d kept-drift=%d (%+v)", total, wouldRemove, keptDrift, doc.Rows)
	}
	for _, k := range doc.Kept {
		if k == "models/opencode" {
			return
		}
	}
	t.Errorf("kept list missing models/opencode: %v", doc.Kept)
}

func TestExecute_ModelsUnsyncDriven(t *testing.T) {
	home := sandboxHome(t)
	fm := &fakeModels{statuses: []modelsync.Status{
		{Target: "litellm-fragment", Client: "litellm", State: modelsync.StateInSync, Path: home + "/f.yaml"},
	}}
	m := &Managers{Home: home, Models: fm}

	doc, err := m.Execute(context.Background(), Options{Force: true}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if fm.unsyncs != 1 || !fm.force {
		t.Errorf("Unsync calls=%d force=%v, want 1/true", fm.unsyncs, fm.force)
	}
	var found bool
	for _, r := range doc.Rows {
		if r.Kind == "models" && r.Action == "removed" {
			found = true
		}
	}
	if !found {
		t.Errorf("execute rows missing models removal: %+v", doc.Rows)
	}
}

func TestExecute_ModelsSkippedWhenNothingSynced(t *testing.T) {
	home := sandboxHome(t)
	fm := &fakeModels{statuses: []modelsync.Status{
		{Target: "litellm-fragment", Client: "litellm", State: modelsync.StateNeverSynced},
	}}
	m := &Managers{Home: home, Models: fm}
	if _, err := m.Execute(context.Background(), Options{}, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if fm.unsyncs != 0 {
		t.Errorf("Unsync must not run when nothing is synced, got %d calls", fm.unsyncs)
	}
}
