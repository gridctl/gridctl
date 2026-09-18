package pins

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
)

func storeSnapshot(t *testing.T, generation uint64, card, endpoint string) mcp.PinSnapshot {
	t.Helper()
	s, err := NewCardSnapshot(generation, CardIdentity{Card: "https://agent.example/card", Endpoint: endpoint}, []byte(card), []mcp.Tool{{Name: "send"}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCardStore_TrustAndRestart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	s := NewWithPath(dir, "test")
	ctx := context.Background()
	first := storeSnapshot(t, 1, "first", "")
	d, err := s.VerifyCard(ctx, "agent", first)
	if err != nil || !d.Trusted || !d.FirstUse || d.NewIdentity {
		t.Fatalf("first use: %+v %v", d, err)
	}
	if d, err := s.CheckCard(ctx, "agent", first); err != nil || !d.Trusted || d.FirstUse {
		t.Fatalf("read-only check: %+v %v", d, err)
	}
	if d, err := s.CheckCard(ctx, "absent", first); err != nil || d.Trusted {
		t.Fatalf("read-only check created trust: %+v %v", d, err)
	}
	// A new store must read approved evidence without relying on Load.
	s = NewWithPath(dir, "test")
	d, err = s.VerifyCard(ctx, "agent", storeSnapshot(t, 2, "first", ""))
	if err != nil || !d.Trusted || d.FirstUse {
		t.Fatalf("restart: %+v %v", d, err)
	}
	changed := storeSnapshot(t, 2, "changed bytes", "")
	d, err = s.VerifyCard(ctx, "agent", changed)
	if err != nil || d.Trusted {
		t.Fatalf("drift trusted: %+v %v", d, err)
	}
	old, _ := s.GetServer("agent")
	if old.ServerHash != first.Hash() {
		t.Fatal("verification replaced approved evidence")
	}
	if err := s.ApproveCard(ctx, "agent", changed); err != nil {
		t.Fatal(err)
	}
	d, err = s.VerifyCard(ctx, "agent", changed)
	if err != nil || !d.Trusted {
		t.Fatalf("approval: %+v %v", d, err)
	}
	d, err = s.VerifyCard(ctx, "agent", storeSnapshot(t, 3, "new identity", "https://agent.example/other?version=2"))
	if err != nil || !d.Trusted || !d.FirstUse || !d.NewIdentity {
		t.Fatalf("identity replacement: %+v %v", d, err)
	}
}

func TestCardStore_MigrationAndFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	s := NewWithPath(t.TempDir(), "test")
	if _, err := s.VerifyOrPin("agent", []mcp.Tool{{Name: "send"}}); err != nil {
		t.Fatal(err)
	}
	snapshot := storeSnapshot(t, 1, "card", "")
	d, err := s.VerifyCard(ctx, "agent", snapshot)
	if err != nil || d.Trusted || d.FirstUse {
		t.Fatalf("missing identity silently migrated: %+v %v", d, err)
	}
	before, _ := s.GetServer("agent")
	// Force rename failure after candidate construction. Approved memory must
	// remain unchanged, including the legacy migration record.
	if err := os.Mkdir(s.path+".tmp", 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.ApproveCard(ctx, "agent", snapshot); err == nil {
		t.Fatal("write failure approved")
	}
	after, _ := s.GetServer("agent")
	if after.ServerHash != before.ServerHash {
		t.Fatal("failed write published candidate")
	}
	for _, data := range []string{"{", `{"version":"999"}`, `{"version":"2","servers":{"agent":null}}`, `{"version":"2","servers":{"agent":{"tools":{"send":null}}}}`} {
		if err := os.WriteFile(s.path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.VerifyCard(ctx, "agent", snapshot); err == nil {
			t.Fatalf("invalid store accepted: %s", data)
		}
	}
	bad := NewWithPath(filepath.Join(s.path, "child"), "test")
	if _, err := bad.VerifyCard(ctx, "agent", snapshot); err == nil {
		t.Fatal("unreadable store accepted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.VerifyCard(canceled, "agent", snapshot); err == nil {
		t.Fatal("canceled transaction accepted")
	}
}

func TestCardStore_LegacyRecoveryCannotEraseFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := NewWithPath(t.TempDir(), "test")
	if err := os.WriteFile(s.path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Load(); err == nil {
		t.Fatal("corrupt load accepted")
	}
	// Legacy behavior remains available, but its empty-store recovery must not
	// make lost card trust silently usable in this process.
	if _, err := s.VerifyOrPin("ordinary", []mcp.Tool{{Name: "send"}}); err != nil {
		t.Fatal(err)
	}
	snapshot := storeSnapshot(t, 1, "card", "")
	if _, err := s.VerifyCard(context.Background(), "agent", snapshot); err == nil {
		t.Fatal("legacy recovery erased mandatory load failure")
	}
	if err := s.ApproveCard(context.Background(), "agent", snapshot); err == nil {
		t.Fatal("approval bypassed mandatory load failure")
	}
}
