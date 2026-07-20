package mcp

import (
	"context"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"
)

// stubGate is a CallGate with a scripted decision that records its calls.
type stubGate struct {
	name     string
	decision GateDecision
	calls    []GateCall
}

func (s *stubGate) Name() string { return s.name }

func (s *stubGate) CheckToolCall(_ context.Context, call GateCall) GateDecision {
	s.calls = append(s.calls, call)
	return s.decision
}

// settleRecorder captures CostSettler invocations.
type settleRecorder struct {
	calls []GateCall
	costs []float64
}

func (r *settleRecorder) SettleToolCallCost(_ context.Context, call GateCall, costUSD float64) {
	r.calls = append(r.calls, call)
	r.costs = append(r.costs, costUSD)
}

// summaryObserver is a ClientObserver returning a fixed summary.
type summaryObserver struct{ summary ToolCallSummary }

func (o *summaryObserver) ObserveToolCall(string, int, map[string]any, *ToolCallResult) {}

func (o *summaryObserver) ObserveToolCallWithClient(context.Context, ToolCallObservation) ToolCallSummary {
	return o.summary
}

func newGateTestGateway(t *testing.T, denyDownstream bool) (*Gateway, *MockAgentClient) {
	t.Helper()
	ctrl := gomock.NewController(t)
	g := NewGateway()
	client := setupMockAgentClient(ctrl, "github", []Tool{{Name: "search", Description: "Search"}})
	call := client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(context.Context, string, map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{Content: []Content{NewTextContent("ok")}}, nil
		},
	)
	if denyDownstream {
		call.Times(0)
	} else {
		call.AnyTimes()
	}
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	return g, client
}

func TestCallGates_DenyShortCircuitsBeforeDownstream(t *testing.T) {
	g, _ := newGateTestGateway(t, true)
	deny := &stubGate{name: "budgets", decision: GateDeny("Budget exceeded: do not retry.")}
	g.SetCallGates([]CallGate{deny})

	ctx := WithClientAccessID(context.Background(), "claude-code")
	result, err := g.HandleToolsCall(ctx, ToolCallParams{Name: "github__search"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("denied call must return IsError result")
	}
	if got := result.Content[0].Text; got != "Budget exceeded: do not retry." {
		t.Errorf("denial text = %q", got)
	}
	if len(deny.calls) != 1 {
		t.Fatalf("gate invocations = %d, want 1", len(deny.calls))
	}
	call := deny.calls[0]
	if call.PrefixedTool != "github__search" || call.ServerName != "github" || call.ClientAccessID != "claude-code" {
		t.Errorf("GateCall = %+v", call)
	}
}

func TestCallGates_FirstDenialWins(t *testing.T) {
	g, _ := newGateTestGateway(t, true)
	first := &stubGate{name: "rate-limits", decision: GateDeny("Rate limit exceeded.")}
	second := &stubGate{name: "budgets", decision: GateDeny("Budget exceeded.")}
	g.SetCallGates([]CallGate{first, second})

	result, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: "github__search"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content[0].Text, "Rate limit") {
		t.Errorf("expected first gate's message, got %q", result.Content[0].Text)
	}
	if len(second.calls) != 0 {
		t.Error("second gate must not run after the first denies")
	}
}

func TestCallGates_AllowPassesThrough(t *testing.T) {
	g, _ := newGateTestGateway(t, false)
	allow := &stubGate{name: "rate-limits", decision: GateAllow()}
	g.SetCallGates([]CallGate{allow})

	result, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: "github__search"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("allowed call errored: %s", result.Content[0].Text)
	}
	if len(allow.calls) != 1 {
		t.Errorf("gate invocations = %d, want 1", len(allow.calls))
	}
}

func TestCallGates_NoGatesNilPath(t *testing.T) {
	g, _ := newGateTestGateway(t, false)
	g.SetCallGates(nil)

	result, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: "github__search"})
	if err != nil || result.IsError {
		t.Fatalf("nil-gates call failed: err=%v result=%+v", err, result)
	}
}

func TestCostSettler_InvokedWithPricedCost(t *testing.T) {
	g, _ := newGateTestGateway(t, false)
	g.SetToolCallObserver(&summaryObserver{summary: ToolCallSummary{CostUSD: 0.0125, HasCost: true}})
	rec := &settleRecorder{}
	g.SetCostSettler(rec)

	ctx := WithClientAccessID(context.Background(), "claude-code")
	if _, err := g.HandleToolsCall(ctx, ToolCallParams{Name: "github__search"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("settler invocations = %d, want 1", len(rec.calls))
	}
	if rec.costs[0] != 0.0125 {
		t.Errorf("settled cost = %v, want 0.0125", rec.costs[0])
	}
	if rec.calls[0].ServerName != "github" || rec.calls[0].ClientAccessID != "claude-code" {
		t.Errorf("settled call = %+v", rec.calls[0])
	}
}

func TestCostSettler_SkippedWhenUnpriced(t *testing.T) {
	g, _ := newGateTestGateway(t, false)
	g.SetToolCallObserver(&summaryObserver{summary: ToolCallSummary{InputTokens: 10}})
	rec := &settleRecorder{}
	g.SetCostSettler(rec)

	if _, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: "github__search"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("unpriced call settled %d times, want 0 (attribution gap)", len(rec.calls))
	}
}
