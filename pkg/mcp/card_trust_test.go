package mcp_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/pins"
)

func trustSnapshot(t *testing.T, generation uint64, body string) mcp.PinSnapshot {
	t.Helper()
	s, err := pins.NewCardSnapshot(generation, pins.CardIdentity{Card: "https://agent.example/card"}, []byte(body), []mcp.Tool{{Name: "send"}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCardTrustService_Lifecycle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	store := pins.NewWithPath(t.TempDir(), "test")
	service := mcp.NewCardTrustService(store)
	first, changed := trustSnapshot(t, 1, "first"), trustSnapshot(t, 1, "changed")
	var retired, fetched atomic.Int32
	fresh := first
	fetch := func(context.Context) (mcp.PinSnapshot, error) { fetched.Add(1); return fresh, nil }
	d, err := service.Register(ctx, "agent", first, fetch, func() { retired.Add(1) })
	if err != nil || !d.FirstUse || !d.Trusted {
		t.Fatalf("register: %+v %v", d, err)
	}
	if _, err := service.Approved(ctx, "agent", 1); err != nil {
		t.Fatal(err)
	}
	if err := service.Observe(ctx, "agent", changed); err == nil {
		t.Fatal("drift allowed")
	}
	if retired.Load() != 1 {
		t.Fatal("drift did not retire authority")
	}
	pending, err := service.Snapshot(ctx, "agent")
	if err != nil || pending.Hash() != changed.Hash() {
		t.Fatal("pending candidate not visible")
	}
	persisted, _ := store.GetServer("agent")
	if persisted.ServerHash != first.Hash() {
		t.Fatal("pending replaced approved pins")
	}
	if _, err := service.Approved(ctx, "agent", 1); err == nil {
		t.Fatal("blocked generation admitted")
	}
	for _, expected := range []string{"", first.Hash()} {
		if err := service.Approve(ctx, "agent", expected); err == nil {
			t.Fatal("unbound/stale approval accepted")
		}
	}
	if fetched.Load() != 0 {
		t.Fatal("invalid approval fetched card")
	}
	fresh = changed
	if err := service.Approve(ctx, "agent", changed.Hash()); err != nil {
		t.Fatal(err)
	}
	if fetched.Load() != 1 {
		t.Fatal("approval did not fetch unconditionally")
	}
	if _, err := service.Approved(ctx, "agent", 1); err != nil {
		t.Fatal(err)
	}
	if err := service.Observe(ctx, "agent", changed); err != nil {
		t.Fatalf("approved bytes reblocked: %v", err)
	}
	if err := service.Observe(ctx, "agent", first); err == nil || retired.Load() != 2 {
		t.Fatal("second drift did not retire new authority")
	}
	if err := service.Unregister(ctx, "agent", 1); err != nil {
		t.Fatal(err)
	}
	if err := service.Approve(ctx, "agent", first.Hash()); err == nil {
		t.Fatal("unregistered source approved")
	}
}

func TestCardTrustService_ApprovalRaces(t *testing.T) {
	for _, race := range []string{"candidate", "replacement", "unregister", "changed-fetch"} {
		t.Run(race, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			ctx := context.Background()
			store := pins.NewWithPath(t.TempDir(), "test")
			service := mcp.NewCardTrustService(store)
			first, changed := trustSnapshot(t, 1, "first"), trustSnapshot(t, 1, "changed")
			entered, release := make(chan struct{}), make(chan struct{})
			fetch := func(context.Context) (mcp.PinSnapshot, error) {
				close(entered)
				<-release
				if race == "changed-fetch" {
					return first, nil
				}
				return changed, nil
			}
			if _, err := service.Register(ctx, "agent", first, fetch, func() {}); err != nil {
				t.Fatal(err)
			}
			if err := service.Observe(ctx, "agent", changed); err == nil {
				t.Fatal("drift accepted")
			}
			done := make(chan error, 1)
			go func() { done <- service.Approve(ctx, "agent", changed.Hash()) }()
			<-entered
			generation := uint64(1)
			switch race {
			case "candidate":
				if err := service.Observe(ctx, "agent", trustSnapshot(t, 1, "third")); err == nil {
					t.Fatal("new drift accepted")
				}
			case "replacement":
				generation = 2
				_, err := service.Register(ctx, "agent", trustSnapshot(t, 2, "replacement"), fetch, func() {})
				if err != nil {
					t.Fatal(err)
				}
			case "unregister":
				if err := service.Unregister(ctx, "agent", 1); err != nil {
					t.Fatal(err)
				}
			}
			close(release)
			if err := <-done; err == nil {
				t.Fatal("racing approval succeeded")
			}
			if _, err := service.Approved(ctx, "agent", generation); err == nil {
				t.Fatal("racing approval removed block")
			}
			persisted, _ := store.GetServer("agent")
			if persisted.ServerHash != first.Hash() {
				t.Fatal("stale approval persisted")
			}
		})
	}
}

func TestCardTrustService_UnavailableAndStale(t *testing.T) {
	ctx := context.Background()
	service := mcp.NewCardTrustService(nil)
	snapshot := trustSnapshot(t, 1, "card")
	fetch := func(context.Context) (mcp.PinSnapshot, error) { return snapshot, nil }
	if _, err := service.Register(ctx, "agent", snapshot, fetch, func() {}); err == nil {
		t.Fatal("missing storage accepted")
	}
	if _, err := service.Register(ctx, "agent", snapshot, fetch, func() {}); err == nil {
		t.Fatal("generation reused")
	}
	if _, err := service.Approved(ctx, "agent", 1); err == nil {
		t.Fatal("unavailable storage allowed dispatch")
	}
	if err := service.Observe(ctx, "agent", snapshot); err == nil {
		t.Fatal("unavailable storage allowed refresh")
	}
	if _, err := service.Snapshot(ctx, "absent"); err == nil {
		t.Fatal("absent snapshot accepted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := service.Snapshot(canceled, "agent"); err == nil {
		t.Fatal("canceled wait accepted")
	}
}

func TestGateway_CardStorageCannotReplaceLiveTrust(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	gateway := mcp.NewGateway()
	service := gateway.CardTrust()
	if service == nil {
		t.Fatal("gateway omitted mandatory trust service")
	}
	if err := gateway.SetCardPinStorage(ctx, pins.NewWithPath(t.TempDir(), "test")); err != nil {
		t.Fatal(err)
	}
	snapshot := trustSnapshot(t, 1, "card")
	if _, err := service.Register(ctx, "agent", snapshot, func(context.Context) (mcp.PinSnapshot, error) { return snapshot, nil }, func() {}); err != nil {
		t.Fatal(err)
	}
	if err := gateway.SetCardPinStorage(ctx, nil); err == nil {
		t.Fatal("live trust storage replaced")
	}
	if gateway.CardTrust() != service {
		t.Fatal("gateway trust service identity changed")
	}
	if _, err := service.Approved(ctx, "agent", 1); err != nil {
		t.Fatal(err)
	}
}
