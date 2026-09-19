package mcp

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestA2AClient_UncertaintySurvivesDeliveredLease(t *testing.T) {
	for _, turn := range []string{"resume", "context-only"} {
		t.Run(turn, func(t *testing.T) {
			f := newA2AUnitFixture(t, "1.0", false)
			c := f.client(t, "", 0)
			now := time.Now()
			c.store.mu.Lock()
			c.store.now = func() time.Time { return now }
			c.store.mu.Unlock()
			first := a2aUnitCall(t, c, "send", map[string]any{"message": "start"})
			args := map[string]any{"message": "continue", "context_handle": first["context_handle"]}
			if turn == "resume" {
				args["task_handle"] = first["task_handle"]
			}
			entered := make(chan struct{})
			f.mu.Lock()
			f.onRPC = func(_ http.ResponseWriter, r *http.Request, _ map[string]any) {
				close(entered)
				<-r.Context().Done()
			}
			f.mu.Unlock()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := c.CallTool(ctx, "send", args)
				done <- err
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("mutation never reached the remote")
			}
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatal("ambiguous mutation lost cancellation identity")
			}
			c.store.mu.Lock()
			now = now.Add(11 * time.Minute)
			c.store.sweepLocked()
			c.store.mu.Unlock()
			for _, state := range []string{"working", "input-required"} {
				f.mu.Lock()
				f.onRPC = func(w http.ResponseWriter, _ *http.Request, req map[string]any) {
					f.respond(w, req, state, "task", "context")
				}
				f.mu.Unlock()
				if got := a2aUnitCall(t, c, "send", args); got["error"] != "operation_uncertain" {
					t.Fatal("lease expiry or working read reauthorized mutation")
				}
				if got := a2aUnitCall(t, c, "task_get", map[string]any{"task_handle": first["task_handle"]}); got["state"] != state {
					t.Fatal("uncertain known task was not readable")
				}
			}
			got := a2aUnitCall(t, c, "send", args)
			if turn == "context-only" {
				if got["error"] != "operation_uncertain" {
					t.Fatal("known task read reconciled unknown new work")
				}
				if got := a2aUnitCall(t, c, "task_cancel", map[string]any{"task_handle": first["task_handle"]}); got["task_handle"] != first["task_handle"] {
					t.Fatal("unknown new work disabled explicit known-task cancel")
				}
				if got := a2aUnitCall(t, c, "send", args); got["error"] != "operation_uncertain" {
					t.Fatal("cancel reconciled unknown new work")
				}
			} else if got["task_handle"] != first["task_handle"] {
				t.Fatal("interrupted observation failed to reconcile known task")
			}
		})
	}
}

func TestA2AClient_InFlightExpiryCannotPublish(t *testing.T) {
	for _, version := range []string{"0.3", "1.0"} {
		t.Run(version, func(t *testing.T) {
			f := newA2AUnitFixture(t, version, false)
			c := f.client(t, "", 0)
			now := time.Now()
			c.store.mu.Lock()
			c.store.now = func() time.Time { return now }
			c.store.mu.Unlock()
			first := a2aUnitCall(t, c, "send", map[string]any{"message": "start"})
			entered, release := make(chan struct{}), make(chan struct{})
			f.mu.Lock()
			f.onRPC = func(w http.ResponseWriter, r *http.Request, req map[string]any) {
				close(entered)
				select {
				case <-release:
					f.respond(w, req, "completed", "task", "context")
				case <-r.Context().Done():
				}
			}
			f.mu.Unlock()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			done := make(chan *ToolCallResult, 1)
			go func() {
				result, _ := c.CallTool(ctx, "send", map[string]any{"message": "resume", "context_handle": first["context_handle"], "task_handle": first["task_handle"]})
				done <- result
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("resume never reached remote")
			}
			c.store.mu.Lock()
			now = now.Add(24 * time.Hour)
			c.store.mu.Unlock()
			close(release)
			if result := <-done; result == nil || !result.IsError {
				t.Fatal("expired in-flight operation published success")
			}
			before := f.calls.Load()
			if got := a2aUnitCall(t, c, "task_get", map[string]any{"task_handle": first["task_handle"]}); got["error"] != "capability_unavailable" || f.calls.Load() != before {
				t.Fatal("expired child task reached remote")
			}
			c.store.mu.Lock()
			defer c.store.mu.Unlock()
			if c.store.used != (capabilityCounts{tombstones: 2}) {
				t.Fatal("expiry failed to reclaim live capacity or discarded collision protection")
			}
		})
	}
}

func TestA2AClient_AdaptersShareGlobalBudget(t *testing.T) {
	f := newA2AUnitFixture(t, "1.0", true)
	first := f.client(t, "", 0)
	second, err := newA2AClient(t.Context(), "second", first.cfg, first.store, first.trust, first.builder, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := second.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	first.store.mu.Lock()
	first.store.globalLimit = capabilityCounts{roots: 1, tasks: 1, tombstones: 2}
	first.store.mu.Unlock()
	owned := a2aUnitCall(t, first, "send", map[string]any{"message": "first"})
	beforeGET, beforeRPC := f.gets.Load(), f.calls.Load()
	if got := a2aUnitCall(t, second, "send", map[string]any{"message": "second"}); got["error"] != "capability_capacity_exhausted" || f.gets.Load() != beforeGET || f.calls.Load() != beforeRPC {
		t.Fatal("second adapter bypassed the gateway budget")
	}
	if got := a2aUnitCall(t, first, "task_get", map[string]any{"task_handle": owned["task_handle"]}); got["task_handle"] != owned["task_handle"] {
		t.Fatal("capacity refusal evicted existing authority")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if got := a2aUnitCall(t, second, "send", map[string]any{"message": "after teardown"}); got["task_handle"] == nil {
		t.Fatal("retired adapter failed to release shared capacity")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	first.store.mu.Lock()
	defer first.store.mu.Unlock()
	if first.store.used != (capabilityCounts{roots: 1, tasks: 1, tombstones: 2}) {
		t.Fatal("repeated teardown debited unrelated authority")
	}
}

func TestA2AClient_RegisteredObserverVariants(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		f := newA2AUnitFixture(t, "1.0", false)
		g := NewGateway()
		defer g.Close()
		if err := g.SetCardPinStorage(t.Context(), &a2aUnitPins{}); err != nil {
			t.Fatal(err)
		}
		if err := g.RegisterMCPServer(t.Context(), MCPServerConfig{Name: "agent", A2A: true, A2AConfig: &A2AClientConfig{Card: f.server.URL}}); err != nil {
			t.Fatal(err)
		}
		observer := &sensitiveObserverSpy{}
		legacyObserver := legacySensitiveObserver{called: make(chan struct{}, 1)}
		if legacy {
			g.SetToolCallObserver(legacyObserver)
		} else {
			g.SetToolCallObserver(observer)
		}
		result, err := g.CallTool(t.Context(), "agent__send", map[string]any{"message": "hello"})
		if err != nil || result == nil || result.IsError || f.calls.Load() != 1 {
			t.Fatal("registered adapter did not dispatch")
		}
		select {
		case <-legacyObserver.called:
			t.Fatal("registered adapter called a legacy payload observer")
		default:
		}
		if observer.legacy.count() != 0 || observer.rich.count() != 0 || !legacy && (len(observer.safe) != 1 || observer.safe[0].Usage.OutputTokens == 0) {
			t.Fatal("registered adapter did not select payload-free observation")
		}
	}
}
