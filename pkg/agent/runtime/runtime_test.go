package runtime

import (
	"context"
	"testing"

	"github.com/gridctl/gridctl/pkg/agent"
	"github.com/gridctl/gridctl/pkg/agent/compose"
	"github.com/gridctl/gridctl/pkg/agent/persist"
	"github.com/gridctl/gridctl/pkg/agent/sandbox"
	"github.com/gridctl/gridctl/pkg/mcp"
)

// stubChatModel satisfies agent.ChatModel without exercising any real
// LLM provider — Runtime only stores and returns the value, so no
// methods are called in this test.
type stubChatModel struct{}

func (stubChatModel) Generate(_ context.Context, _ agent.ChatRequest) (agent.ChatResponse, error) {
	return agent.ChatResponse{}, nil
}
func (stubChatModel) Stream(_ context.Context, _ agent.ChatRequest) (*agent.StreamReader[agent.ChatChunk], error) {
	return nil, nil
}

func TestRuntime_NewRuntimeStoresLoadBearingComponents(t *testing.T) {
	t.Parallel()
	store := persist.NewStore(t.TempDir())
	reg := compose.NewRegistry()
	sb := sandbox.New(0)

	rt := NewRuntime(store, reg, sb)

	if got := rt.RunStore(); got != store {
		t.Errorf("RunStore() = %p, want %p", got, store)
	}
	if got := rt.ApprovalRegistry(); got != reg {
		t.Errorf("ApprovalRegistry() = %p, want %p", got, reg)
	}
	if got := rt.Sandbox(); got != sb {
		t.Errorf("Sandbox() = %p, want %p", got, sb)
	}
	if got := rt.ChatModel(); got != nil {
		t.Errorf("ChatModel() = %v, want nil before SetChatModel", got)
	}
	if got := rt.DevServer(); got != nil {
		t.Errorf("DevServer() = %v, want nil before SetDevServer", got)
	}
}

func TestRuntime_SetChatModelLatePlugIn(t *testing.T) {
	t.Parallel()
	rt := NewRuntime(persist.NewStore(t.TempDir()), compose.NewRegistry(), sandbox.New(0))

	model := stubChatModel{}
	rt.SetChatModel(model)
	if got := rt.ChatModel(); got != model {
		t.Errorf("ChatModel() = %v, want stub model after SetChatModel", got)
	}

	// Replace with nil so consumers can detach a stale provider.
	rt.SetChatModel(nil)
	if got := rt.ChatModel(); got != nil {
		t.Errorf("ChatModel() = %v after SetChatModel(nil), want nil", got)
	}
}

func TestRuntime_AgentRuntimeMarkerSatisfiesGatewayContract(t *testing.T) {
	t.Parallel()
	// Asserting against the real mcp.AgentRuntime interface (rather
	// than a local copy) means a future signature drift fails this
	// test at compile time — even though the gateway also enforces it.
	rt := NewRuntime(persist.NewStore(t.TempDir()), compose.NewRegistry(), sandbox.New(0))
	var _ mcp.AgentRuntime = rt
}
