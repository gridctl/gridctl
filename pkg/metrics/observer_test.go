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

	obs.ObserveToolCall("test-server", args, result)

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

func TestObserver_NilResult(t *testing.T) {
	counter := token.NewHeuristicCounter(4)
	acc := NewAccumulator(100)
	obs := NewObserver(counter, acc)

	obs.ObserveToolCall("test-server", map[string]any{"key": "val"}, nil)

	snap := acc.Snapshot()
	if snap.Session.InputTokens == 0 {
		t.Error("expected non-zero input tokens even with nil result")
	}
	if snap.Session.OutputTokens != 0 {
		t.Errorf("expected 0 output tokens for nil result, got %d", snap.Session.OutputTokens)
	}
}
