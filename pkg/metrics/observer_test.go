package metrics

import (
	"context"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/token"
)

func TestObserver_ObserveToolCall(t *testing.T) {
	counter := token.NewHeuristicCounter(4)
	acc := NewAccumulator(100)
	obs := NewObserver(counter, acc)

	args := map[string]any{"query": "hello world"}
	result := &mcp.ToolCallResult{
		Content: []mcp.Content{
			mcp.NewTextContent("This is the response text from the tool."),
		},
	}

	obs.ObserveToolCall("test-server", -1, args, result)

	snap := acc.Snapshot()
	if snap.Session.InputTokens == 0 {
		t.Error("expected non-zero input tokens")
	}
	if snap.Session.OutputTokens == 0 {
		t.Error("expected non-zero output tokens")
	}
	if snap.Session.TotalTokens != snap.Session.InputTokens+snap.Session.OutputTokens {
		t.Error("total should equal input + output")
	}

	serverTokens, ok := snap.PerServer["test-server"]
	if !ok {
		t.Fatal("expected test-server in per-server metrics")
	}
	if serverTokens.TotalTokens != snap.Session.TotalTokens {
		t.Error("server total should equal session total for single server")
	}
}

func TestObserver_ObservePromptGet(t *testing.T) {
	counter := token.NewHeuristicCounter(4)
	acc := NewAccumulator(100)
	obs := NewObserver(counter, acc)

	obs.ObservePromptGet(mcp.PromptGetObservation{PromptName: "code-review", ClientID: "claude-code"})
	obs.ObservePromptGet(mcp.PromptGetObservation{PromptName: "code-review"})

	snap := acc.PromptUsageSnapshot()
	if got := snap["code-review"].Calls; got != 2 {
		t.Errorf("code-review calls = %d, want 2", got)
	}
	// Prompt serving must never appear in tool usage (Tools Audit Mode).
	if tu := acc.ToolUsageSnapshot(); tu != nil {
		t.Errorf("prompt get leaked into tool usage: %v", tu)
	}
	// And it must not record any token/cost usage.
	if s := acc.Snapshot(); s.Session.TotalTokens != 0 {
		t.Errorf("prompt get recorded tokens = %d, want 0", s.Session.TotalTokens)
	}
}

func TestObserver_PerReplica(t *testing.T) {
	counter := token.NewHeuristicCounter(4)
	acc := NewAccumulator(100)
	obs := NewObserver(counter, acc)

	args := map[string]any{"query": "hello"}
	result := &mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent("response")}}

	obs.ObserveToolCall("multi", 0, args, result)
	obs.ObserveToolCall("multi", 1, args, result)
	obs.ObserveToolCall("multi", 1, args, result)

	snap := acc.Snapshot()
	serverTotal, ok := snap.PerServer["multi"]
	if !ok {
		t.Fatal("expected per-server entry for multi")
	}
	replicaMap, ok := snap.PerReplica["multi"]
	if !ok {
		t.Fatalf("expected per-replica entry for multi; got %+v", snap.PerReplica)
	}
	if len(replicaMap) != 2 {
		t.Fatalf("expected 2 replicas, got %d", len(replicaMap))
	}
	if replicaMap[1].TotalTokens != 2*replicaMap[0].TotalTokens {
		t.Errorf("replica 1 should have 2× the tokens of replica 0; got %d vs %d",
			replicaMap[1].TotalTokens, replicaMap[0].TotalTokens)
	}
	if sum := replicaMap[0].TotalTokens + replicaMap[1].TotalTokens; sum != serverTotal.TotalTokens {
		t.Errorf("replica totals should sum to server total: %d vs %d", sum, serverTotal.TotalTokens)
	}
}

// TestObserver_PerToolAttribution verifies the client-aware path records
// per-tool call counts and tokens for every call.
func TestObserver_PerToolAttribution(t *testing.T) {
	counter := token.NewHeuristicCounter(4)
	acc := NewAccumulator(100)
	obs := NewObserver(counter, acc)

	args := map[string]any{"q": "hello"}
	result := &mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent("response")}}

	obs.ObserveToolCallWithClient(context.Background(), mcp.ToolCallObservation{
		ServerName: "github", ReplicaID: -1, ClientID: "client-a", ToolName: "create_issue",
		Arguments: args, Result: result,
	})
	obs.ObserveToolCallWithClient(context.Background(), mcp.ToolCallObservation{
		ServerName: "atlassian", ReplicaID: -1, ClientID: "client-a", ToolName: "read_file",
		Arguments: args, Result: result,
	})

	snap := acc.ToolUsageSnapshot()

	inputTokens := int64(token.CountJSON(counter, args))
	outputTokens := int64(counter.Count("response"))
	for server, tool := range map[string]string{"github": "create_issue", "atlassian": "read_file"} {
		stat := snap[server][tool]
		if stat.Calls != 1 {
			t.Errorf("%s/%s Calls = %d, want 1", server, tool, stat.Calls)
		}
		if stat.InputTokens != inputTokens || stat.OutputTokens != outputTokens {
			t.Errorf("%s/%s tokens = %d/%d, want %d/%d", server, tool, stat.InputTokens, stat.OutputTokens, inputTokens, outputTokens)
		}
	}
}

// TestObserver_LegacyPathRecordsNoPhantomTool verifies the legacy observer
// entry point (no tool name) never creates a "" tool entry.
func TestObserver_LegacyPathRecordsNoPhantomTool(t *testing.T) {
	counter := token.NewHeuristicCounter(4)
	acc := NewAccumulator(100)
	obs := NewObserver(counter, acc)

	args := map[string]any{"q": "hello"}
	result := &mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent("response")}}
	obs.ObserveToolCall("server-a", -1, args, result)

	if tokens := acc.Snapshot().PerServer["server-a"].TotalTokens; tokens <= 0 {
		t.Fatalf("per-server tokens = %v, want >0", tokens)
	}
	if snap := acc.ToolUsageSnapshot(); snap != nil {
		t.Errorf("legacy path must not create per-tool entries; got %v", snap)
	}
}

// TestObserver_ImplementsClientObserver guarantees the Observer satisfies
// the ClientObserver interface; the gateway type-asserts on this to opt
// into synchronous, client-aware dispatch.
func TestObserver_ImplementsClientObserver(t *testing.T) {
	var _ mcp.ClientObserver = (*Observer)(nil)
}

// TestObserver_ObserveToolCallWithClient_AttributesPerClient verifies that
// the ClientObserver entry point routes tokens to the per-client maps
// without breaking session/per-server aggregates, and surfaces cache token
// counts from CallUsage on the summary.
func TestObserver_ObserveToolCallWithClient_AttributesPerClient(t *testing.T) {
	counter := token.NewHeuristicCounter(4)
	acc := NewAccumulator(100)
	obs := NewObserver(counter, acc)

	args := map[string]any{"q": "hello"}
	result := &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.NewTextContent("answer")},
		Usage:   &mcp.CallUsage{CacheReadTokens: 12, CacheCreationTokens: 3},
	}
	summary := obs.ObserveToolCallWithClient(context.Background(), mcp.ToolCallObservation{
		ServerName: "server-a",
		ReplicaID:  -1,
		ClientID:   "claude-code",
		ToolName:   "demo",
		Arguments:  args,
		Result:     result,
	})

	if summary.InputTokens == 0 || summary.OutputTokens == 0 {
		t.Errorf("expected non-zero token counts; got %+v", summary)
	}
	if summary.CacheReadTokens != 12 || summary.CacheCreationTokens != 3 {
		t.Errorf("cache token counts = %d/%d, want 12/3", summary.CacheReadTokens, summary.CacheCreationTokens)
	}

	tokens := acc.Snapshot()
	clientTokens, ok := tokens.PerClient["claude-code"]
	if !ok {
		t.Fatal("expected per-client tokens for claude-code")
	}
	if clientTokens.TotalTokens != tokens.Session.TotalTokens {
		t.Errorf("per-client tokens (%d) should equal session (%d) for single client",
			clientTokens.TotalTokens, tokens.Session.TotalTokens)
	}
}

// TestObserver_ObserveToolCallWithClient_EmptyClientNoAttribution covers
// the case where a tool call carries no session attribution: tokens still
// record under session/per-server, but per-client maps stay empty so
// anonymous traffic does not pollute attribution dimensions.
func TestObserver_ObserveToolCallWithClient_EmptyClientNoAttribution(t *testing.T) {
	counter := token.NewHeuristicCounter(4)
	acc := NewAccumulator(100)
	obs := NewObserver(counter, acc)

	obs.ObserveToolCallWithClient(context.Background(), mcp.ToolCallObservation{
		ServerName: "server-a",
		ReplicaID:  -1,
		ClientID:   "",
		Arguments:  map[string]any{"q": 1},
		Result:     &mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent("a")}},
	})

	snap := acc.Snapshot()
	if len(snap.PerClient) != 0 {
		t.Errorf("expected no per-client entries; got %v", snap.PerClient)
	}
	if snap.Session.TotalTokens == 0 {
		t.Error("session totals should still update when client is unknown")
	}
}

// TestObserver_ObserveToolCallWithClient_SummaryMatchesLegacyPath ensures
// the legacy ObserveToolCall path and the new ObserveToolCallWithClient
// path record identical aggregates for the same input — only attribution
// dimensions differ.
func TestObserver_ObserveToolCallWithClient_SummaryMatchesLegacyPath(t *testing.T) {
	counter := token.NewHeuristicCounter(4)
	args := map[string]any{"q": "x"}
	result := &mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent("ok")}}

	accLegacy := NewAccumulator(100)
	NewObserver(counter, accLegacy).ObserveToolCall("s", -1, args, result)

	accV2 := NewAccumulator(100)
	NewObserver(counter, accV2).ObserveToolCallWithClient(context.Background(), mcp.ToolCallObservation{
		ServerName: "s",
		ReplicaID:  -1,
		Arguments:  args,
		Result:     result,
	})

	if accLegacy.Snapshot().Session != accV2.Snapshot().Session {
		t.Errorf("session token snapshots diverged: legacy=%v v2=%v",
			accLegacy.Snapshot().Session, accV2.Snapshot().Session)
	}
}

func TestObserver_NilResult(t *testing.T) {
	counter := token.NewHeuristicCounter(4)
	acc := NewAccumulator(100)
	obs := NewObserver(counter, acc)

	obs.ObserveToolCall("test-server", -1, map[string]any{"key": "val"}, nil)

	snap := acc.Snapshot()
	if snap.Session.InputTokens == 0 {
		t.Error("expected non-zero input tokens even with nil result")
	}
	if snap.Session.OutputTokens != 0 {
		t.Errorf("expected 0 output tokens for nil result, got %d", snap.Session.OutputTokens)
	}
}
