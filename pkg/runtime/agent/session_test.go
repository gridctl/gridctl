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

// mockLLMClient is a minimal LLMClient for testing.
type mockLLMClient struct {
	streamFn func(ctx context.Context, events chan<- agent.LLMEvent) (string, error)
}

func (m *mockLLMClient) Stream(ctx context.Context, systemPrompt string, history []agent.Message, tools []agent.Tool, caller agent.ToolCaller, events chan<- agent.LLMEvent) (string, error) {
	if m.streamFn != nil {
		return m.streamFn(ctx, events)
	}
	events <- agent.LLMEvent{Type: agent.EventTypeDone}
	return "response", nil
}

func (m *mockLLMClient) Close() error { return nil }

func TestLLMClientInterface(t *testing.T) {
	var _ agent.LLMClient = &mockLLMClient{}
}
