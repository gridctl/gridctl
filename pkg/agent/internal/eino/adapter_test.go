package eino

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestNewGraph_Compile_Invoke_Passthrough(t *testing.T) {
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

	got, err := r.Invoke(ctx, "hello")
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if got != "hello" {
		t.Errorf("Invoke output = %q, want %q", got, "hello")
	}
}

func TestNewGraph_Compile_Invoke_Lambda(t *testing.T) {
	t.Parallel()

	g := NewGraph[string, string]()
	if err := g.AddLambdaNode("upper", func(_ context.Context, in string) (string, error) {
		return strings.ToUpper(in), nil
	}); err != nil {
		t.Fatalf("AddLambdaNode: %v", err)
	}
	if err := g.AddEdge(START, "upper"); err != nil {
		t.Fatalf("AddEdge START → upper: %v", err)
	}
	if err := g.AddEdge("upper", END); err != nil {
		t.Fatalf("AddEdge upper → END: %v", err)
	}

	ctx := context.Background()
	r, err := g.Compile(ctx)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	got, err := r.Invoke(ctx, "hello")
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if got != "HELLO" {
		t.Errorf("Invoke output = %q, want %q", got, "HELLO")
	}
}

func TestRunnable_Stream(t *testing.T) {
	t.Parallel()

	g := NewGraph[string, string]()
	if err := g.AddLambdaNode("echo", func(_ context.Context, in string) (string, error) {
		return in, nil
	}); err != nil {
		t.Fatalf("AddLambdaNode: %v", err)
	}
	if err := g.AddEdge(START, "echo"); err != nil {
		t.Fatalf("AddEdge START → echo: %v", err)
	}
	if err := g.AddEdge("echo", END); err != nil {
		t.Fatalf("AddEdge echo → END: %v", err)
	}

	ctx := context.Background()
	r, err := g.Compile(ctx)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	sr, err := r.Stream(ctx, "world")
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer sr.Close()

	var got []string
	for {
		chunk, err := sr.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		got = append(got, chunk)
	}
	if len(got) == 0 {
		t.Fatal("expected at least one streamed chunk; got 0")
	}
	joined := strings.Join(got, "")
	if joined != "world" {
		t.Errorf("streamed output = %q, want %q", joined, "world")
	}
}

func TestCompile_MissingEdges(t *testing.T) {
	t.Parallel()

	g := NewGraph[string, string]()
	if err := g.AddPassthroughNode("orphan"); err != nil {
		t.Fatalf("AddPassthroughNode: %v", err)
	}

	ctx := context.Background()
	_, err := g.Compile(ctx)
	if err == nil {
		t.Fatal("Compile of an unwired graph: expected error, got nil")
	}
}

func TestStreamReaderFromSlice_DrainsInOrder(t *testing.T) {
	t.Parallel()

	want := []int{1, 2, 3}
	sr := StreamReaderFromSlice(want)
	defer sr.Close()

	var got []int
	for {
		v, err := sr.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		got = append(got, v)
	}

	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestStreamReaderFromSlice_EmptyReturnsEOF(t *testing.T) {
	t.Parallel()

	sr := StreamReaderFromSlice[int](nil)
	defer sr.Close()

	if _, err := sr.Recv(); !errors.Is(err, io.EOF) {
		t.Errorf("Recv on empty stream: err = %v, want io.EOF", err)
	}
}

func TestStreamReader_CloseIsIdempotent(t *testing.T) {
	t.Parallel()

	sr := StreamReaderFromSlice([]int{1})
	sr.Close()
	sr.Close() // second call must not panic
}

func TestLambda_PropagatesError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom")
	g := NewGraph[string, string]()
	if err := g.AddLambdaNode("explode", func(_ context.Context, _ string) (string, error) {
		return "", wantErr
	}); err != nil {
		t.Fatalf("AddLambdaNode: %v", err)
	}
	if err := g.AddEdge(START, "explode"); err != nil {
		t.Fatalf("AddEdge START → explode: %v", err)
	}
	if err := g.AddEdge("explode", END); err != nil {
		t.Fatalf("AddEdge explode → END: %v", err)
	}

	ctx := context.Background()
	r, err := g.Compile(ctx)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if _, err := r.Invoke(ctx, "x"); !errors.Is(err, wantErr) {
		t.Errorf("Invoke error = %v, want %v wrapped", err, wantErr)
	}
}
