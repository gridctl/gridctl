//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/reload"
)

// writeSkill writes a minimal valid SKILL.md into <registryDir>/skills/<name>/.
func writeSkill(t *testing.T, registryDir, name, state string) {
	t.Helper()
	skillDir := filepath.Join(registryDir, "skills", name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: " + name + " skill\nstate: " + state + "\n---\n\n# " + name + "\n\nBody.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

// TestRegistryDiskWatch_PicksUpOutOfBandSkill exercises the real DirWatcher
// wired to a real registry.Server and mcp.Gateway router, reproducing the bug
// where a skill written directly to the registry directory while the daemon is
// running could be validated but not activated or seen by the gateway. After
// the fix, the watcher refreshes the store and the router so the skill becomes
// available without a restart. This test is filesystem-only and does not need a
// container runtime.
func TestRegistryDiskWatch_PicksUpOutOfBandSkill(t *testing.T) {
	registryDir := t.TempDir()
	store := registry.NewStore(registryDir)
	srv := registry.New(store)
	gw := mcp.NewGateway()

	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// Mirror the production refresh sequence (controller.refreshRegistry): reload
	// the store from disk, then add/remove the registry client and refresh tools.
	refresh := func() error {
		if err := srv.RefreshTools(context.Background()); err != nil {
			return err
		}
		if srv.HasContent() {
			gw.Router().AddClient(srv)
		} else {
			gw.Router().RemoveClient("registry")
		}
		gw.Router().RefreshTools()
		return nil
	}

	skillsDir := filepath.Join(store.Dir(), "skills")
	watcher := reload.NewDirWatcher(skillsDir, refresh)
	watcher.SetDebounce(100 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = watcher.Watch(ctx) }()
	time.Sleep(150 * time.Millisecond)

	// Precondition: the skill is not yet known to the running gateway.
	if _, err := srv.Store().GetSkill("node-state-snapshot"); err == nil {
		t.Fatal("precondition: skill should not exist before it is written")
	}
	if gw.Router().GetClient("registry") != nil {
		t.Fatal("precondition: registry client should be absent with no skills")
	}

	// Write a skill directly to disk, out-of-band — the reported scenario.
	writeSkill(t, registryDir, "node-state-snapshot", "active")

	// The watcher should reconcile it within the debounce window.
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if _, lastErr = srv.Store().GetSkill("node-state-snapshot"); lastErr == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("skill not picked up by watcher within deadline: %v", lastErr)
	}

	if gw.Router().GetClient("registry") == nil {
		t.Error("expected registry client registered with the router after the skill appeared")
	}
}

// TestRegistryDiskWatch_DebouncesMultiFileSkill verifies that writing a skill
// made of several files in quick succession results in a consistent final state
// (the skill is present), exercising the debounce path against real components.
func TestRegistryDiskWatch_DebouncesMultiFileSkill(t *testing.T) {
	registryDir := t.TempDir()
	store := registry.NewStore(registryDir)
	srv := registry.New(store)

	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	var refreshes int
	refresh := func() error {
		refreshes++
		return srv.RefreshTools(context.Background())
	}

	skillsDir := filepath.Join(store.Dir(), "skills")
	watcher := reload.NewDirWatcher(skillsDir, refresh)
	watcher.SetDebounce(150 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = watcher.Watch(ctx) }()
	time.Sleep(150 * time.Millisecond)

	skillDir := filepath.Join(registryDir, "skills", "multi")
	if err := os.MkdirAll(filepath.Join(skillDir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := []string{"SKILL.md", "scripts/run.sh", "reference.md"}
	content := "---\nname: multi\ndescription: multi skill\nstate: active\n---\n\n# multi\n\nBody.\n"
	for _, f := range files {
		payload := []byte("x")
		if f == "SKILL.md" {
			payload = []byte(content)
		}
		if err := os.WriteFile(filepath.Join(skillDir, f), payload, 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(15 * time.Millisecond)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := srv.Store().GetSkill("multi"); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := srv.Store().GetSkill("multi"); err != nil {
		t.Fatalf("multi-file skill not reconciled: %v", err)
	}
	if refreshes == 0 {
		t.Error("expected at least one debounced refresh")
	}
}
