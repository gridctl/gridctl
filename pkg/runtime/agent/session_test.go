package agent_test

import (
	"context"
	"sync"
	"testing"

	"github.com/gridctl/gridctl/pkg/runtime/agent"
)

func TestNewSession(t *testing.T) {
	s := agent.NewSession("test-id")
	if s.ID != "test-id" {
		t.Fatalf("expected ID %q, got %q", "test-id", s.ID)
	}
	if s.Events() == nil {
		t.Fatal("Events channel should not be nil")
	}
	if s.WriteChan() == nil {
		t.Fatal("WriteChan should not be nil")
	}
}

func TestStartAndFinishInference(t *testing.T) {
	s := agent.NewSession("s1")

	cancel := func() {}
	if !s.StartInference(cancel) {
		t.Fatal("first StartInference should return true")
	}
	if s.StartInference(cancel) {
		t.Fatal("second StartInference should return false (already active)")
	}

	s.FinishInference()
	if !s.StartInference(cancel) {
		t.Fatal("StartInference after FinishInference should return true")
	}
}

func TestCancelInference(t *testing.T) {
	s := agent.NewSession("s2")
	cancelled := false
	cancel := func() { cancelled = true }

	s.StartInference(cancel)
	s.Cancel()

	if !cancelled {
		t.Fatal("Cancel should have called the cancel func")
	}
	// Should be able to start a new inference after cancel
	if !s.StartInference(func() {}) {
		t.Fatal("StartInference after Cancel should return true")
	}
}

func TestAddMessageAndHistory(t *testing.T) {
	s := agent.NewSession("s3")
	s.AddMessage("user", "hello")
	s.AddMessage("assistant", "world")

	h := s.History()
	if len(h) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(h))
	}
	if h[0].Role != "user" || h[0].Content != "hello" {
		t.Errorf("unexpected first message: %+v", h[0])
	}
	if h[1].Role != "assistant" || h[1].Content != "world" {
		t.Errorf("unexpected second message: %+v", h[1])
	}
}

func TestHistorySnapshot(t *testing.T) {
	s := agent.NewSession("s4")
	s.AddMessage("user", "msg1")

	snap := s.History()
	// Mutating the snapshot must not affect the session history
	snap[0].Content = "mutated"

	h := s.History()
	if h[0].Content != "msg1" {
		t.Fatal("History snapshot should be a copy, not a reference")
	}
}

func TestResetHistory(t *testing.T) {
	s := agent.NewSession("s5")
	s.AddMessage("user", "hello")
	s.ResetHistory()

	if len(s.History()) != 0 {
		t.Fatal("history should be empty after ResetHistory")
	}
}

func TestSendDropsWhenFull(t *testing.T) {
	s := agent.NewSession("s6")
	// Fill the buffer (capacity 512)
	for i := 0; i < 600; i++ {
		s.Send(agent.LLMEvent{Type: agent.EventTypeToken})
	}
	// Should not deadlock — Send drops events when buffer is full
}

func TestSessionRegistryGetOrCreate(t *testing.T) {
	reg := agent.NewSessionRegistry()

	s1 := reg.GetOrCreate("abc")
	s2 := reg.GetOrCreate("abc")
	if s1 != s2 {
		t.Fatal("GetOrCreate should return the same session for the same ID")
	}

	s3 := reg.GetOrCreate("xyz")
	if s3 == s1 {
		t.Fatal("GetOrCreate should return different sessions for different IDs")
	}
}

func TestSessionRegistryGet(t *testing.T) {
	reg := agent.NewSessionRegistry()

	_, ok := reg.Get("nonexistent")
	if ok {
		t.Fatal("Get on missing ID should return false")
	}

	reg.GetOrCreate("exists")
	s, ok := reg.Get("exists")
	if !ok || s == nil {
		t.Fatal("Get should return the created session")
	}
}

func TestSessionRegistryDelete(t *testing.T) {
	reg := agent.NewSessionRegistry()

	cancelled := false
	s := reg.GetOrCreate("del")
	s.StartInference(func() { cancelled = true })

	reg.Delete("del")
	if !cancelled {
		t.Fatal("Delete should cancel active inference")
	}

	_, ok := reg.Get("del")
	if ok {
		t.Fatal("session should be removed after Delete")
	}
}

func TestSessionRegistryConcurrentAccess(t *testing.T) {
	reg := agent.NewSessionRegistry()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := "session"
			reg.GetOrCreate(id)
			reg.Get(id)
		}(i)
	}
	wg.Wait()
}

func TestSessionRegistryGetOrCreate_DoubleCheck(t *testing.T) {
	// Force many concurrent goroutines to race through GetOrCreate for the same
	// key, maximising the chance that the double-check inside the write lock is hit.
	reg := agent.NewSessionRegistry()
	const n = 500
	results := make([]*agent.TestFlightSession, n)
	ready := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-ready // wait for all goroutines to be ready before racing
			results[idx] = reg.GetOrCreate("race-key")
		}(i)
	}
	close(ready) // release all goroutines simultaneously
	wg.Wait()
	// Every goroutine must receive the exact same session pointer.
	for i := 1; i < n; i++ {
		if results[i] != results[0] {
			t.Fatal("GetOrCreate returned different sessions for the same key")
		}
	}
}

// mockLLMClient is a minimal LLMClient for testing.
type mockLLMClient struct {
	streamFn func(ctx context.Context, events chan<- agent.LLMEvent) (string, []agent.Message, error)
}

func (m *mockLLMClient) Stream(ctx context.Context, systemPrompt string, history []agent.Message, tools []agent.Tool, caller agent.ToolCaller, events chan<- agent.LLMEvent) (string, []agent.Message, error) {
	if m.streamFn != nil {
		return m.streamFn(ctx, events)
	}
	events <- agent.LLMEvent{Type: agent.EventTypeDone}
	return "response", nil, nil
}

func (m *mockLLMClient) Close() error { return nil }

func TestLLMClientInterface(t *testing.T) {
	var _ agent.LLMClient = &mockLLMClient{}
}

func TestAddTurn(t *testing.T) {
	s := agent.NewSession("s7")
	s.AddTurn(agent.Message{
		Role: "assistant",
		ToolCalls: []agent.ToolCallBlock{
			{ID: "tc1", Name: "my_tool", Arguments: `{"key":"val"}`},
		},
	})
	s.AddTurn(agent.Message{
		Role:       "tool",
		ToolCallID: "tc1",
		Content:    "tool output",
	})
	s.AddMessage("assistant", "final answer")

	h := s.History()
	if len(h) != 3 {
		t.Fatalf("expected 3 turns, got %d", len(h))
	}
	if len(h[0].ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(h[0].ToolCalls))
	}
	if h[0].ToolCalls[0].Name != "my_tool" {
		t.Errorf("unexpected tool name: %s", h[0].ToolCalls[0].Name)
	}
	if h[1].Role != "tool" || h[1].ToolCallID != "tc1" {
		t.Errorf("unexpected tool result: %+v", h[1])
	}
	if h[2].Content != "final answer" {
		t.Errorf("unexpected final response: %s", h[2].Content)
	}
}

func TestToolCallBlockFields(t *testing.T) {
	tc := agent.ToolCallBlock{
		ID:        "id1",
		Name:      "search",
		Arguments: `{"query":"test"}`,
	}
	if tc.ID != "id1" || tc.Name != "search" || tc.Arguments != `{"query":"test"}` {
		t.Errorf("unexpected ToolCallBlock: %+v", tc)
	}
}

func TestMessageWithRawParam(t *testing.T) {
	raw := []byte(`{"role":"assistant","content":[{"type":"tool_use","id":"x","name":"foo","input":{}}]}`)
	s := agent.NewSession("s8")
	s.AddTurn(agent.Message{
		Role:     "assistant",
		RawParam: raw,
		ToolCalls: []agent.ToolCallBlock{
			{ID: "x", Name: "foo", Arguments: "{}"},
		},
	})
	h := s.History()
	if len(h[0].RawParam) == 0 {
		t.Fatal("RawParam should be preserved in history")
	}
}

func TestResetHistoryCreatesNewChannel(t *testing.T) {
	s := agent.NewSession("s9")
	ch1 := s.Events()
	s.ResetHistory()
	ch2 := s.Events()
	if ch1 == ch2 {
		t.Fatal("ResetHistory should create a new event channel")
	}
}
