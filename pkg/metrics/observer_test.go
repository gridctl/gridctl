package metrics

import (
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
