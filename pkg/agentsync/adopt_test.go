package agentsync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAdopt_PullsEditIntoCanon(t *testing.T) {
	mgr, home, registryDir := newTestManager(t, "alpha")
	ctx := context.Background()
	if _, err := mgr.Sync(ctx, nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	edited := "---\nname: alpha\ndescription: Reviews things\n---\n\nEdited by hand.\n"
	dest := projectedPath(home, "alpha")
	if err := os.WriteFile(dest, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := mgr.Adopt(ctx, "alpha", "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("Changed = false for a real edit")
	}
	canon, err := os.ReadFile(filepath.Join(registryDir, "agents", "alpha", "AGENT.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(canon) != edited {
		t.Error("canonical AGENT.md does not carry the adopted edit verbatim")
	}
	if res.BackupFile == "" {
		t.Fatal("no store-side backup recorded")
	}
	if _, err := os.Stat(res.BackupFile); err != nil {
		t.Fatalf("backup file missing: %v", err)
	}

	// The pair returns to in-sync.
	statuses, err := mgr.Statuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].State != StateInSync {
		t.Fatalf("post-adopt statuses = %+v, want in-sync", statuses)
	}

	// The force-resync writes bytes identical to the projected file, so
	// it must not burn a client-side backup rotation slot on a duplicate.
	backupDir := filepath.Join(home, ".gridctl", "project-backups", "agent", "claude-code", "alpha")
	if entries, err := os.ReadDir(backupDir); err == nil && len(entries) > 0 {
		t.Errorf("adopt's force-resync wrote a redundant client-side backup: %v", entries)
	}
}

func TestAdopt_UnchangedRefreshesHashes(t *testing.T) {
	mgr, _, _ := newTestManager(t, "alpha")
	ctx := context.Background()
	if _, err := mgr.Sync(ctx, nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	res, err := mgr.Adopt(ctx, "alpha", "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.BackupFile != "" {
		t.Fatalf("res = %+v, want unchanged with no backup", res)
	}
}

func TestAdopt_Refusals(t *testing.T) {
	mgr, home, _ := newTestManager(t, "alpha")
	ctx := context.Background()

	// Not projected.
	if _, err := mgr.Adopt(ctx, "alpha", "claude-code"); !errors.Is(err, ErrNotProjected) {
		t.Fatalf("err = %v, want ErrNotProjected", err)
	}
	// Unknown client.
	if _, err := mgr.Adopt(ctx, "alpha", "nope"); !errors.Is(err, ErrUnknownClient) {
		t.Fatalf("err = %v, want ErrUnknownClient", err)
	}

	if _, err := mgr.Sync(ctx, nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	dest := projectedPath(home, "alpha")

	// Empty projected content.
	if err := os.WriteFile(dest, []byte("  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var refusal *AdoptRefusal
	if _, err := mgr.Adopt(ctx, "alpha", "claude-code"); !errors.As(err, &refusal) {
		t.Fatalf("err = %v, want AdoptRefusal for empty content", err)
	}

	// Rename attempt.
	if err := os.WriteFile(dest, []byte("---\nname: other\ndescription: d\n---\n\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Adopt(ctx, "alpha", "claude-code"); !errors.As(err, &refusal) {
		t.Fatalf("err = %v, want AdoptRefusal for rename", err)
	}

	// Gone destination.
	if err := os.Remove(dest); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Adopt(ctx, "alpha", "claude-code"); !errors.As(err, &refusal) {
		t.Fatalf("err = %v, want AdoptRefusal for missing file", err)
	}
}
