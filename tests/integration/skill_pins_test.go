//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/reload"
	"github.com/gridctl/gridctl/pkg/skillpins"
)

// TestSkillPins_DiskWatchLifecycle exercises the skill-pin TOFU lifecycle
// against the real DirWatcher and registry: first sight pins silently, an
// out-of-band edit flips the skill to pin drift on the watcher's refresh,
// drift persists across refreshes (never auto-approved), and approve
// re-pins. Filesystem-only; no container runtime needed.
func TestSkillPins_DiskWatchLifecycle(t *testing.T) {
	registryDir := t.TempDir()
	store := registry.NewStore(registryDir)
	srv := registry.New(store)
	ps := skillpins.NewWithPath(t.TempDir(), "integration")

	writeSkill(t, registryDir, "pinned-skill", "active")
	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// Mirror the production refresh sequence (controller.refreshRegistry +
	// syncSkillPins): reload the store, then run the pin sync. The pin file
	// lives outside the watched tree, so pin writes cannot re-trigger the
	// watcher.
	refresh := func() error {
		if err := srv.RefreshTools(context.Background()); err != nil {
			return err
		}
		_, err := ps.Sync(srv.Store())
		return err
	}
	if err := refresh(); err != nil {
		t.Fatalf("initial refresh: %v", err)
	}

	pin, ok := ps.Get("pinned-skill")
	if !ok || pin.Status != skillpins.StatusPinned {
		t.Fatalf("first sight did not pin silently: %+v", pin)
	}

	skillsDir := filepath.Join(store.Dir(), "skills")
	watcher := reload.NewDirWatcher(skillsDir, refresh)
	watcher.SetDebounce(100 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = watcher.Watch(ctx) }()
	time.Sleep(150 * time.Millisecond)

	// Out-of-band edit: the reported supply-chain scenario.
	path := filepath.Join(skillsDir, "pinned-skill", "SKILL.md")
	edited := "---\nname: pinned-skill\ndescription: pinned-skill skill\nstate: active\n---\n\n# pinned-skill\n\nBody, now changed.\n"
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatalf("editing skill: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pin, ok := ps.Get("pinned-skill"); ok && pin.Status == skillpins.StatusDrift {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	pin, _ = ps.Get("pinned-skill")
	if pin.Status != skillpins.StatusDrift {
		t.Fatalf("edit did not surface as pin drift within deadline: %+v", pin)
	}

	// Drift persists across another explicit refresh — never auto-approved.
	if err := refresh(); err != nil {
		t.Fatalf("refresh with drift: %v", err)
	}
	if pin, _ = ps.Get("pinned-skill"); pin.Status != skillpins.StatusDrift {
		t.Fatalf("drift auto-cleared by refresh: %+v", pin)
	}

	// Approve with the reviewed composite hash re-pins.
	composite, err := ps.CurrentCompositeHash("pinned-skill", srv.Store())
	if err != nil {
		t.Fatalf("composite hash: %v", err)
	}
	if err := ps.Approve("pinned-skill", srv.Store(), composite, ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := refresh(); err != nil {
		t.Fatalf("post-approve refresh: %v", err)
	}
	if pin, _ = ps.Get("pinned-skill"); pin.Status != skillpins.StatusPinned {
		t.Fatalf("approved skill still drifted: %+v", pin)
	}
}
