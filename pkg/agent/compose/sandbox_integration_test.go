package compose_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/agent/compose"
	"github.com/gridctl/gridctl/pkg/agent/persist"
	"github.com/gridctl/gridctl/pkg/agent/sandbox"
)

// TestApprovalGateInTSSandbox exercises the Phase E end-to-end path:
// a TS skill calls approval(prompt); the gate persists the request,
// emits the notification, blocks until a CLI/web/MCP consumer
// resolves it via the registry, then records the response. The shape
// matches what the daemon wires at apply time — a per-run Gate, a
// process-wide Registry, and a SlogNotifier as the audit-line floor.
func TestApprovalGateInTSSandbox(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := persist.NewStore(dir)
	defer store.CloseAll() //nolint:errcheck // best-effort

	registry := compose.NewRegistry()
	runID := persist.NewRunID()
	rec, err := store.OpenWriter(runID)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	defer rec.Close() //nolint:errcheck // best-effort

	if _, err := rec.Record(persist.EventRunStarted, persist.RunStartedPayload{Skill: "demo", Flavor: "ts"}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	gate, err := compose.NewGate(runID, rec, registry, &compose.SlogNotifier{}, compose.WithTimeout(2*time.Second), compose.WithSkill("demo"))
	if err != nil {
		t.Fatalf("NewGate: %v", err)
	}

	// Resolve the approval out of band as soon as the gate registers.
	resolved := make(chan struct{})
	go func() {
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if list := registry.List(); len(list) == 1 {
				_ = registry.Resolve(list[0].ID, true, "looks good", "web")
				close(resolved)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Errorf("gate never registered")
		close(resolved)
	}()

	src := `
		export default async function () {
			const decision = await approval("ship it?");
			return { approved: decision.approved, reason: decision.reason };
		}
	`
	sb := sandbox.New(3 * time.Second)
	res, err := sb.Execute(context.Background(), src, map[string]any{}, sandbox.Bindings{Approver: gate})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	<-resolved

	var out struct {
		Approved bool   `json:"approved"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(res.Value), &out); err != nil {
		t.Fatalf("decode value: %v (raw=%q)", err, res.Value)
	}
	if !out.Approved || out.Reason != "looks good" {
		t.Fatalf("unexpected decision: %+v", out)
	}

	if err := rec.Close(); err != nil {
		t.Fatalf("rec.Close: %v", err)
	}
	events, err := store.Read(runID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// Expect run_started + approval_request + approval_response.
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[1].Type != persist.EventApprovalRequest || events[2].Type != persist.EventApprovalResponse {
		t.Fatalf("unexpected event sequence: %+v", events)
	}
}
