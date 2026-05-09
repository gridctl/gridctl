package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
)

func TestNewGraph_RoundtripThroughPublicSurface(t *testing.T) {
	t.Parallel()

	g := NewGraph[string, string]()
	if err := g.AddPassthroughNode("only"); err != nil {
		t.Fatalf("AddPassthroughNode: %v", err)
	}
	if err := g.AddEdge(START, "only"); err != nil {
		t.Fatalf("AddEdge START → only: %v", err)
	}
	if err := g.AddEdge("only", END); err != nil {
		t.Fatalf("AddEdge only → END: %v", err)
	}

	ctx := context.Background()
	r, err := g.Compile(ctx)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	got, err := r.Invoke(ctx, "ping")
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if got != "ping" {
		t.Errorf("Invoke = %q, want %q", got, "ping")
	}
}

func TestStreamReaderFromSlice_PublicSurface(t *testing.T) {
	t.Parallel()

	sr := StreamReaderFromSlice([]string{"a", "b"})
	defer sr.Close()

	first, err := sr.Recv()
	if err != nil {
		t.Fatalf("Recv first: %v", err)
	}
	if first != "a" {
		t.Errorf("first = %q, want %q", first, "a")
	}
	second, err := sr.Recv()
	if err != nil {
		t.Fatalf("Recv second: %v", err)
	}
	if second != "b" {
		t.Errorf("second = %q, want %q", second, "b")
	}
	if _, err := sr.Recv(); !errors.Is(err, io.EOF) {
		t.Errorf("Recv after drain: err = %v, want io.EOF", err)
	}
}

func TestToolInfo_JSONRoundtrip(t *testing.T) {
	t.Parallel()

	in := ToolInfo{
		Name:        "search",
		Description: "Search the corpus.",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}
	encoded, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out ToolInfo
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.Name != in.Name {
		t.Errorf("Name = %q, want %q", out.Name, in.Name)
	}
	if out.Description != in.Description {
		t.Errorf("Description = %q, want %q", out.Description, in.Description)
	}
	if string(out.InputSchema) != string(in.InputSchema) {
		t.Errorf("InputSchema = %q, want %q", out.InputSchema, in.InputSchema)
	}
}

func TestToolInfo_OmitsEmptyFields(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(ToolInfo{Name: "x"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := string(encoded)
	if got != `{"name":"x"}` {
		t.Errorf("Marshal = %q, want %q", got, `{"name":"x"}`)
	}
}

// stubChatModel exists to verify that the gridctl-shaped ChatModel
// interface can be satisfied by a Phase B-style implementation without
// depending on eino's model.ChatModel directly.
type stubChatModel struct{}

func (stubChatModel) Generate(_ context.Context, _ ChatRequest) (ChatResponse, error) {
	return ChatResponse{}, nil
}

func (stubChatModel) Stream(_ context.Context, _ ChatRequest) (*StreamReader[ChatChunk], error) {
	return StreamReaderFromSlice([]ChatChunk{{}}), nil
}

func TestChatModel_InterfaceShape(t *testing.T) {
	t.Parallel()

	var m ChatModel = stubChatModel{}

	if _, err := m.Generate(context.Background(), ChatRequest{}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	sr, err := m.Stream(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer sr.Close()
	if _, err := sr.Recv(); err != nil {
		t.Fatalf("Recv: %v", err)
	}
}
