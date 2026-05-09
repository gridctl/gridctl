package gateway

import (
	"context"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/agent"
)

type fakeProvider struct {
	name      string
	lastModel string
}

func (f *fakeProvider) Generate(_ context.Context, req agent.ChatRequest) (agent.ChatResponse, error) {
	f.lastModel = req.Model
	return agent.ChatResponse{Model: req.Model, Content: f.name}, nil
}

func (f *fakeProvider) Stream(_ context.Context, req agent.ChatRequest) (*agent.StreamReader[agent.ChatChunk], error) {
	f.lastModel = req.Model
	return agent.StreamReaderFromSlice([]agent.ChatChunk{{Delta: f.name}}), nil
}

func TestNew_RequiresRouteOrFallback(t *testing.T) {
	t.Parallel()
	if _, err := New(); err == nil {
		t.Errorf("New() with no options should error")
	}
}

func TestProvider_RoutesByPrefix(t *testing.T) {
	t.Parallel()
	a := &fakeProvider{name: "anthropic"}
	o := &fakeProvider{name: "openai"}

	p, err := New(WithRoute("claude-", a), WithRoute("gpt-", o))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := p.Generate(context.Background(), agent.ChatRequest{Model: "claude-3-5-haiku"})
	if err != nil || resp.Content != "anthropic" {
		t.Errorf("claude routing failed: resp=%+v err=%v", resp, err)
	}

	resp, err = p.Generate(context.Background(), agent.ChatRequest{Model: "gpt-4o"})
	if err != nil || resp.Content != "openai" {
		t.Errorf("gpt routing failed: resp=%+v err=%v", resp, err)
	}
}

func TestProvider_UnmatchedRoute(t *testing.T) {
	t.Parallel()
	a := &fakeProvider{name: "anthropic"}
	p, _ := New(WithRoute("claude-", a))

	_, err := p.Generate(context.Background(), agent.ChatRequest{Model: "gemini-2"})
	if err == nil {
		t.Errorf("expected unmatched-model error")
	}
	if !strings.Contains(err.Error(), "no route") {
		t.Errorf("error did not mention no-route: %v", err)
	}
}

func TestProvider_FallbackUsedWhenNoMatch(t *testing.T) {
	t.Parallel()
	a := &fakeProvider{name: "anthropic"}
	fb := &fakeProvider{name: "fallback"}
	p, _ := New(WithRoute("claude-", a), WithFallback(fb))

	resp, err := p.Generate(context.Background(), agent.ChatRequest{Model: "gemini-2"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Content != "fallback" {
		t.Errorf("Content = %q, want fallback", resp.Content)
	}
}

func TestProvider_RequiresModel(t *testing.T) {
	t.Parallel()
	a := &fakeProvider{name: "x"}
	p, _ := New(WithRoute("claude-", a))
	_, err := p.Generate(context.Background(), agent.ChatRequest{})
	if err == nil {
		t.Errorf("expected error for empty model")
	}
}

func TestProvider_StreamRoutes(t *testing.T) {
	t.Parallel()
	a := &fakeProvider{name: "anthropic"}
	p, _ := New(WithRoute("claude-", a))

	stream, err := p.Stream(context.Background(), agent.ChatRequest{Model: "claude-x"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer stream.Close()
	chunk, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if chunk.Delta != "anthropic" {
		t.Errorf("chunk = %+v, want anthropic", chunk)
	}
	if a.lastModel != "claude-x" {
		t.Errorf("lastModel = %q", a.lastModel)
	}
}

func TestProvider_RoutesSnapshotIsCopy(t *testing.T) {
	t.Parallel()
	a := &fakeProvider{name: "x"}
	p, _ := New(WithRoute("claude-", a))
	rs := p.Routes()
	_ = append(rs, Route{Prefix: "evil-"}) //nolint:ineffassign // intentionally drop the appended slice
	if len(p.Routes()) != 1 {
		t.Errorf("Routes() snapshot was not copied")
	}
}
