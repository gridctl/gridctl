package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCapabilityError_Retryable(t *testing.T) {
	for _, category := range []*capabilityError{errCapabilityUnavailable, errCapabilityCapacity, errCapabilityInProgress, errCapabilitySuperseded, errCapabilityUncertain, errCapabilityConflict, errCapabilityRandom} {
		if category.Retryable() != (category == errCapabilityInProgress) || category.Error() == "" {
			t.Fatal("incorrect safe error classification")
		}
	}
}

func authorityGeneration(t *testing.T, s *CapabilityStore, server, profile string) *capabilityGeneration {
	t.Helper()
	g, err := s.boundGeneration(capabilityIdentity{server: server, card: "https://agent.invalid/card", endpoint: "https://agent.invalid/rpc", dialect: "1.0", profile: profile, cardDigest: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func authoritySend(t *testing.T, g *capabilityGeneration, contextHandle, taskHandle string) *capabilityOperation {
	t.Helper()
	op, err := g.admit(context.Background(), capabilitySend, contextHandle, taskHandle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := op.dispatch(); err != nil {
		t.Fatal(err)
	}
	return op
}

func authoritySeed(t *testing.T, g *capabilityGeneration) capabilityDelivery {
	t.Helper()
	d, err := authoritySend(t, g, "", "").commit(capabilityResponse{contextID: "context", taskID: "task", state: "input-required"})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestCapabilityAuthority_BindingAndUniformDenials(t *testing.T) {
	s := NewCapabilityStore()
	g := authorityGeneration(t, s, "agent", "")
	d := authoritySeed(t, g)
	if len(d.contextHandle) != 52 || len(d.taskHandle) != 52 {
		t.Fatal("handle length")
	}
	for _, value := range []string{"", d.contextHandle, d.taskHandle[:51], "gca2a_t2_" + d.taskHandle[9:], strings.Repeat("x", 52)} {
		if _, err := g.admit(context.Background(), capabilityGet, "", value); !errors.Is(err, errCapabilityUnavailable) {
			t.Fatalf("denial: %v", err)
		}
	}
	other := authorityGeneration(t, s, "other", "")
	if _, err := other.admit(context.Background(), capabilityGet, "", d.taskHandle); !errors.Is(err, errCapabilityUnavailable) {
		t.Fatal(err)
	}
	op, err := g.admit(context.Background(), capabilityGet, "", d.taskHandle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = op.dispatch(); err != nil {
		t.Fatal(err)
	}
	got, err := op.commit(capabilityResponse{contextID: "context", taskID: "task", state: "working"})
	if err != nil || got.contextHandle != "" || got.taskHandle != d.taskHandle {
		t.Fatalf("get authority or echo mismatch: %v", err)
	}
	if _, err := g.admit(context.Background(), capabilitySend, "", d.taskHandle); !errors.Is(err, errCapabilityUnavailable) {
		t.Fatal(err)
	}
}

func TestCapabilityAuthority_CancelSupersedesBothOrders(t *testing.T) {
	for _, cancelFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "cancel-first", false: "send-first"}[cancelFirst], func(t *testing.T) {
			g := authorityGeneration(t, NewCapabilityStore(), "agent", "bedrock")
			d := authoritySeed(t, g)
			send := authoritySend(t, g, d.contextHandle, d.taskHandle)
			cancel, err := g.admit(context.Background(), capabilityCancel, "", d.taskHandle)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := cancel.dispatch(); err != nil {
				t.Fatal(err)
			}
			for _, kind := range []capabilityOperationKind{capabilitySend, capabilityGet, capabilityCancel} {
				ch := ""
				if kind == capabilitySend {
					ch = d.contextHandle
				}
				if _, err := g.admit(context.Background(), kind, ch, d.taskHandle); !errors.Is(err, errCapabilityInProgress) {
					t.Fatalf("overlap: %v", err)
				}
			}
			finishSend := func() {
				_, e := send.commit(capabilityResponse{contextID: "context", taskID: "task", state: "working"})
				if !errors.Is(e, errCapabilitySuperseded) {
					t.Fatal(e)
				}
			}
			finishCancel := func() {
				_, e := cancel.commit(capabilityResponse{contextID: "context", taskID: "task", state: "canceled"})
				if e != nil {
					t.Fatal(e)
				}
			}
			if cancelFirst {
				finishCancel()
				finishSend()
			} else {
				finishSend()
				finishCancel()
			}
			if _, err := g.admit(context.Background(), capabilitySend, d.contextHandle, ""); !errors.Is(err, errCapabilityUncertain) {
				t.Fatal(err)
			}
			get, err := g.admit(context.Background(), capabilityGet, "", d.taskHandle)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := get.dispatch(); err != nil {
				t.Fatal(err)
			}
			if _, err := get.commit(capabilityResponse{contextID: "context", taskID: "task", state: "canceled"}); err != nil {
				t.Fatal(err)
			}
			if _, err := g.admit(context.Background(), capabilitySend, d.contextHandle, d.taskHandle); !errors.Is(err, errCapabilityUnavailable) {
				t.Fatal(err)
			}
			op := authoritySend(t, g, d.contextHandle, "")
			op.fail(false)
		})
	}
}

func TestCapabilityAuthority_ExpiryAndTombstones(t *testing.T) {
	s := NewCapabilityStore()
	now := time.Now()
	s.now = func() time.Time { return now }
	g := authorityGeneration(t, s, "agent", "")
	d := authoritySeed(t, g)
	op := authoritySend(t, g, d.contextHandle, d.taskHandle)
	now = now.Add(24 * time.Hour)
	if _, err := op.commit(capabilityResponse{contextID: "context", taskID: "task", state: "completed"}); !errors.Is(err, errCapabilityUnavailable) {
		t.Fatal(err)
	}
	if _, err := g.admit(context.Background(), capabilityGet, "", d.taskHandle); !errors.Is(err, errCapabilityUnavailable) {
		t.Fatal(err)
	}
	if g.used.roots != 0 || g.used.tasks != 0 || g.used.tombstones != 2 {
		t.Fatalf("counts: %+v", g.used)
	}
	if _, err := authoritySend(t, g, "", "").commit(capabilityResponse{contextID: "context", taskID: "other", state: "working"}); !errors.Is(err, errCapabilityConflict) {
		t.Fatal(err)
	}
}

func TestCapabilityAuthority_UncertaintyDoesNotExpireAuthority(t *testing.T) {
	s := NewCapabilityStore()
	now := time.Now()
	s.now = func() time.Time { return now }
	g := authorityGeneration(t, s, "agent", "")
	d := authoritySeed(t, g)
	authoritySend(t, g, d.contextHandle, "").fail(true)
	now = now.Add(11 * time.Minute)
	get, err := g.admit(context.Background(), capabilityGet, "", d.taskHandle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := get.dispatch(); err != nil {
		t.Fatal(err)
	}
	if _, err := get.commit(capabilityResponse{contextID: "context", taskID: "task", state: "completed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.admit(context.Background(), capabilitySend, d.contextHandle, ""); !errors.Is(err, errCapabilityUncertain) {
		t.Fatal(err)
	}
	fresh := authoritySend(t, g, "", "")
	fresh.fail(true)
	if g.used.roots != 2 {
		t.Fatal(g.used)
	}
	now = now.Add(11 * time.Minute)
	g.store.sweep()
	if g.used.roots != 1 {
		t.Fatal(g.used)
	}
}
