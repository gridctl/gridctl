package mcp

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type failingCapabilityRandom struct{}

func (failingCapabilityRandom) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestCapabilityAuthority_RandomFailureRollsBack(t *testing.T) {
	for _, available := range []int64{0, 32, 48} {
		s := NewCapabilityStore()
		s.random = io.MultiReader(io.LimitReader(rand.Reader, available), failingCapabilityRandom{})
		g := authorityGeneration(t, s, "agent", "")
		if _, err := g.admit(context.Background(), capabilitySend, "", ""); !errors.Is(err, errCapabilityRandom) {
			t.Fatal(err)
		}
		if s.used != (capabilityCounts{}) || len(g.roots) != 0 || len(g.reservations) != 0 {
			t.Fatalf("partial RNG failure retained state: %+v", s.used)
		}
	}
}

func TestCapabilityAuthority_ProtocolAdmission(t *testing.T) {
	for _, response := range []capabilityResponse{
		{contextID: "foreign", taskID: "task", state: "working"},
		{contextID: "context", taskID: "foreign", state: "working"},
		{contextID: "context", taskID: "task", state: "unknown"},
		{contextID: "context", taskID: "task", state: "working", references: []capabilityReference{{taskID: "foreign"}}},
		{contextID: "context", taskID: "task", state: "working", references: []capabilityReference{{contextID: "foreign"}}},
		{contextID: "context", taskID: "task", state: "working", references: []capabilityReference{{contextID: string([]byte{0xff})}}},
	} {
		g := authorityGeneration(t, NewCapabilityStore(), "agent", "")
		d := authoritySeed(t, g)
		op := authoritySend(t, g, d.contextHandle, d.taskHandle)
		if _, err := op.commit(response); !errors.Is(err, errCapabilityConflict) {
			t.Fatal(err)
		}
		if op.task.state != "input-required" || !op.task.uncertain || len(g.tasks) != 1 || len(g.contexts) != 1 {
			t.Fatal("invalid response altered authority")
		}
	}
}

func TestCapabilityAuthority_NamespacesAndMessageReservations(t *testing.T) {
	for _, profile := range []string{"", "bedrock"} {
		g := authorityGeneration(t, NewCapabilityStore(), "agent", profile)
		a := authoritySeed(t, g)
		firstDigest, _ := capabilityDigest(a.taskHandle, "t")
		first := g.tasks[firstDigest].root
		op := authoritySend(t, g, "", "")
		if first.session == op.root.session {
			t.Fatal("sessions shared")
		}
		_, err := op.commit(capabilityResponse{contextID: "context", taskID: "task", state: "working"})
		if profile == "" && !errors.Is(err, errCapabilityConflict) {
			t.Fatal(err)
		}
		if profile == "bedrock" && err != nil {
			t.Fatal(err)
		}
	}
	g := authorityGeneration(t, NewCapabilityStore(), "agent", "")
	if _, err := authoritySend(t, g, "", "").commit(capabilityResponse{}); err != nil {
		t.Fatal(err)
	}
	if g.used != (capabilityCounts{}) || len(g.roots) != 0 {
		t.Fatal(g.used)
	}
	d, err := authoritySend(t, g, "", "").commit(capabilityResponse{contextID: "only-context"})
	if err != nil {
		t.Fatal(err)
	}
	if d.contextHandle == "" || d.taskHandle != "" || g.used != (capabilityCounts{roots: 1, tombstones: 1}) {
		t.Fatal("context-only message accounting")
	}
	if _, err := authoritySend(t, g, d.contextHandle, "").commit(capabilityResponse{contextID: "only-context"}); err != nil {
		t.Fatal(err)
	}
	if g.used != (capabilityCounts{roots: 1, tombstones: 1}) {
		t.Fatal(g.used)
	}
}

func TestCapabilityAuthority_TaskWithoutContextCannotUpgrade(t *testing.T) {
	g := authorityGeneration(t, NewCapabilityStore(), "agent", "")
	d, err := authoritySend(t, g, "", "").commit(capabilityResponse{taskID: "orphan", state: "working"})
	if err != nil {
		t.Fatal(err)
	}
	if d.contextHandle != "" || d.taskHandle == "" {
		t.Fatal("task granted context")
	}
	get, err := g.admit(context.Background(), capabilityGet, "", d.taskHandle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := get.dispatch(); err != nil {
		t.Fatal(err)
	}
	if _, err := get.commit(capabilityResponse{contextID: "new-context", taskID: "orphan", state: "completed"}); !errors.Is(err, errCapabilityConflict) {
		t.Fatal(err)
	}
	if len(g.contexts) != 0 {
		t.Fatal("upgraded task authority")
	}
}

func TestCapabilityAuthority_KnownUncertaintyReconciliation(t *testing.T) {
	s := NewCapabilityStore()
	now := time.Now()
	s.now = func() time.Time { return now }
	g := authorityGeneration(t, s, "agent", "")
	d := authoritySeed(t, g)
	authoritySend(t, g, d.contextHandle, d.taskHandle).fail(true)
	now = now.Add(11 * time.Minute)
	for _, state := range []string{"working", "input-required"} {
		get, err := g.admit(context.Background(), capabilityGet, "", d.taskHandle)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := get.dispatch(); err != nil {
			t.Fatal(err)
		}
		if _, err := get.commit(capabilityResponse{contextID: "context", taskID: "task", state: state}); err != nil {
			t.Fatal(err)
		}
		if state == "working" {
			if _, err := g.admit(context.Background(), capabilitySend, d.contextHandle, d.taskHandle); !errors.Is(err, errCapabilityUncertain) {
				t.Fatal(err)
			}
			cancel, err := g.admit(context.Background(), capabilityCancel, "", d.taskHandle)
			if err != nil {
				t.Fatal(err)
			}
			cancel.fail(false)
		}
	}
	authoritySend(t, g, d.contextHandle, d.taskHandle).fail(false)
}

func TestCapabilityAuthority_RetirementCancelsBothSlots(t *testing.T) {
	s := NewCapabilityStore()
	g := authorityGeneration(t, s, "agent", "")
	d := authoritySeed(t, g)
	send := authoritySend(t, g, d.contextHandle, d.taskHandle)
	cancel, err := g.admit(context.Background(), capabilityCancel, "", d.taskHandle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cancel.dispatch(); err != nil {
		t.Fatal(err)
	}
	other := authorityGeneration(t, s, "other", "")
	otherDelivery := authoritySeed(t, other)
	before := s.used
	replacement := authorityGeneration(t, s, "agent", "")
	if send.ctx.Err() == nil || cancel.ctx.Err() == nil || s.used != before {
		t.Fatal("retirement failed to cancel or released pending accounting")
	}
	if _, err := replacement.admit(context.Background(), capabilityGet, "", d.taskHandle); !errors.Is(err, errCapabilityUnavailable) {
		t.Fatal(err)
	}
	if _, err := send.commit(capabilityResponse{contextID: "context", taskID: "task", state: "working"}); !errors.Is(err, errCapabilityUnavailable) {
		t.Fatal(err)
	}
	if s.used != before {
		t.Fatal("released before both callbacks drained")
	}
	cancel.fail(true)
	if s.used != other.used || g.used != (capabilityCounts{}) || len(g.tasks) != 0 || len(g.remoteIDs) != 0 {
		t.Fatal("teardown accounting")
	}
	get, err := other.admit(context.Background(), capabilityGet, "", otherDelivery.taskHandle)
	if err != nil {
		t.Fatal(err)
	}
	get.fail(false)
}

func TestCapabilityAuthority_FailedAttemptBudget(t *testing.T) {
	s := NewCapabilityStore()
	now := time.Now()
	s.now = func() time.Time { return now }
	g := authorityGeneration(t, s, "agent", "")
	d := authoritySeed(t, g)
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			if _, err := g.admit(context.Background(), capabilityGet, "", "invalid"); !errors.Is(err, errCapabilityUnavailable) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if _, err := g.admit(context.Background(), capabilityGet, "", d.taskHandle); !errors.Is(err, errCapabilityUnavailable) {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	get, err := g.admit(context.Background(), capabilityGet, "", d.taskHandle)
	if err != nil {
		t.Fatal(err)
	}
	get.fail(false)
}

func TestCapabilityAuthority_AdmissionRechecksAndNoRetries(t *testing.T) {
	s := NewCapabilityStore()
	now := time.Now()
	s.now = func() time.Time { return now }
	g := authorityGeneration(t, s, "agent", "")
	d := authoritySeed(t, g)
	op, err := g.admit(context.Background(), capabilityGet, "", d.taskHandle)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(24 * time.Hour)
	if _, err := op.dispatch(); !errors.Is(err, errCapabilityUnavailable) {
		t.Fatal(err)
	}
	op.fail(false)
	fresh := authoritySend(t, g, "", "")
	if _, err := fresh.dispatch(); !errors.Is(err, errCapabilityUnavailable) {
		t.Fatal(err)
	}
	fresh.fail(false)
	if _, err := fresh.commit(capabilityResponse{}); !errors.Is(err, errCapabilityUnavailable) {
		t.Fatal(err)
	}
	if g.used.roots != 0 || g.used.tasks != 0 {
		t.Fatal(g.used)
	}
}

func TestCapabilityAuthority_GatewayUnregister(t *testing.T) {
	gateway := NewGateway()
	g := authorityGeneration(t, gateway.capabilities, "agent", "")
	d := authoritySeed(t, g)
	if err := gateway.UnregisterMCPServerContext(context.Background(), "agent"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.admit(context.Background(), capabilityGet, "", d.taskHandle); !errors.Is(err, errCapabilityUnavailable) {
		t.Fatal(err)
	}
	if gateway.capabilities.used != (capabilityCounts{}) {
		t.Fatal("unregister retained capacity")
	}
}

func TestCapabilityAuthority_PairsSiblingsAndPendingHandles(t *testing.T) {
	g := authorityGeneration(t, NewCapabilityStore(), "agent", "")
	d := authoritySeed(t, g)
	sibling, err := authoritySend(t, g, d.contextHandle, "").commit(capabilityResponse{contextID: "context", taskID: "sibling", state: "input-required"})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := g.admit(context.Background(), capabilitySend, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.admit(context.Background(), capabilityGet, "", pending.taskHandle); !errors.Is(err, errCapabilityUnavailable) {
		t.Fatal("pending handle authorized work")
	}
	if _, err := pending.dispatch(); err != nil {
		t.Fatal(err)
	}
	other, err := pending.commit(capabilityResponse{contextID: "other", taskID: "other", state: "input-required"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.admit(context.Background(), capabilitySend, d.contextHandle, other.taskHandle); !errors.Is(err, errCapabilityUnavailable) {
		t.Fatal("mismatched pair authorized work")
	}
	send := authoritySend(t, g, d.contextHandle, d.taskHandle)
	if _, err := g.admit(context.Background(), capabilityCancel, "", sibling.taskHandle); !errors.Is(err, errCapabilityInProgress) {
		t.Fatal("sibling cancel overlapped send")
	}
	send.fail(false)
}

func TestCapabilityAuthority_ConflictsAreAtomic(t *testing.T) {
	for _, response := range []capabilityResponse{
		{contextID: "new-context", taskID: "task", state: "working"},
		{contextID: "context", taskID: "new-task", state: "working"},
		{contextID: strings.Repeat("x", 1025)},
		{taskID: strings.Repeat("x", 1025), state: "working"},
		{contextID: string([]byte{0xff})},
	} {
		g := authorityGeneration(t, NewCapabilityStore(), "agent", "")
		d := authoritySeed(t, g)
		if _, err := authoritySend(t, g, "", "").commit(response); !errors.Is(err, errCapabilityConflict) {
			t.Fatal(err)
		}
		if len(g.contexts) != 1 || len(g.tasks) != 1 || len(g.remoteIDs) != 2 {
			t.Fatal("partial identity commit")
		}
		get, err := g.admit(context.Background(), capabilityGet, "", d.taskHandle)
		if err != nil {
			t.Fatal("existing authority damaged", err)
		}
		get.fail(false)
	}
}

func TestCapabilityAuthority_CancelFailuresRequireReconciliation(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		g := authorityGeneration(t, NewCapabilityStore(), "agent", "")
		d := authoritySeed(t, g)
		send := authoritySend(t, g, d.contextHandle, d.taskHandle)
		cancel, err := g.admit(context.Background(), capabilityCancel, "", d.taskHandle)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := cancel.dispatch(); err != nil {
			t.Fatal(err)
		}
		cancel.fail(unknown)
		if _, err := send.commit(capabilityResponse{contextID: "context", taskID: "task", state: "input-required"}); !errors.Is(err, errCapabilitySuperseded) {
			t.Fatal(err)
		}
		if _, err := g.admit(context.Background(), capabilitySend, d.contextHandle, d.taskHandle); !errors.Is(err, errCapabilityUncertain) {
			t.Fatal("failed cancel cleared uncertainty")
		}
	}
}

func TestCapabilityAuthority_UTCAndAbsoluteExpiry(t *testing.T) {
	s := NewCapabilityStore()
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.FixedZone("fixture", -7*60*60))
	s.now = func() time.Time { return now }
	g := authorityGeneration(t, s, "agent", "")
	d := authoritySeed(t, g)
	if d.contextExpires.Location() != time.UTC || d.taskExpires.Location() != time.UTC {
		t.Fatal("expiry is not UTC")
	}
	now = now.Add(23 * time.Hour)
	get, err := g.admit(context.Background(), capabilityGet, "", d.taskHandle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := get.dispatch(); err != nil {
		t.Fatal(err)
	}
	later, err := get.commit(capabilityResponse{contextID: "context", taskID: "task", state: "input-required"})
	if err != nil || !later.taskExpires.Equal(d.taskExpires) {
		t.Fatal("expiry was renewed", err)
	}
	now = now.Add(time.Hour)
	if _, err := g.admit(context.Background(), capabilitySend, d.contextHandle, ""); !errors.Is(err, errCapabilityUnavailable) {
		t.Fatal(err)
	}
	if _, err := g.admit(context.Background(), capabilityGet, "", d.taskHandle); !errors.Is(err, errCapabilityUnavailable) {
		t.Fatal(err)
	}
}
