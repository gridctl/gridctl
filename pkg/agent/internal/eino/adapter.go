// Package eino is the boundary between gridctl's agent runtime and the
// upstream cloudwego/eino library. Every reference to github.com/cloudwego/eino
// in the gridctl tree lives in this directory; the rest of pkg/agent/
// imports only the gridctl-shaped types defined here.
//
// The boundary is enforced by scripts/check-eino-boundary.sh, run from CI.
// Treat the constraint as load-bearing: it is what keeps an eino swap a
// 1–2 week project rather than a multi-month rewrite. New eino types are
// wrapped here before crossing out, never re-exported as-is.
//
// The package is import-private (under internal/) so callers always go
// through the public re-exports in pkg/agent.
package eino

import (
	"context"

	einocompose "github.com/cloudwego/eino/compose"
	einoschema "github.com/cloudwego/eino/schema"
)

// START is the implicit graph entry vertex. Connect a node from START to
// receive the graph's input as that node's input.
const START = einocompose.START

// END is the implicit graph exit vertex. Connect a node to END to make its
// output the graph's output.
const END = einocompose.END

// Graph is a typed graph composition. Nodes are added via AddLambdaNode or
// AddPassthroughNode; edges are wired with AddEdge. Compile produces a
// Runnable that executes the composition.
//
// Phase A wraps only the surface needed to validate the boundary; later
// phases extend it (state nodes, branches, sub-graphs) as the runtime
// needs them.
type Graph[I, O any] struct {
	inner *einocompose.Graph[I, O]
}

// NewGraph creates an empty typed graph keyed by its input and output
// types. The graph must be wired START → ... → END before Compile.
func NewGraph[I, O any]() *Graph[I, O] {
	return &Graph[I, O]{inner: einocompose.NewGraph[I, O]()}
}

// AddLambdaNode registers a function-shaped node under the given name.
// The function is invoked once per traversal with the upstream node's
// output as input and produces the downstream node's input.
func (g *Graph[I, O]) AddLambdaNode(name string, fn func(ctx context.Context, in I) (O, error)) error {
	return g.inner.AddLambdaNode(name, einocompose.InvokableLambda(fn))
}

// AddPassthroughNode registers a node that forwards its input unchanged.
// Useful for boundary smoke tests and for graph topologies whose only
// purpose is to gate execution.
func (g *Graph[I, O]) AddPassthroughNode(name string) error {
	return g.inner.AddPassthroughNode(name)
}

// AddEdge connects two named nodes along the graph's data flow. Use
// START as the source to consume the graph's input; use END as the
// destination to surface a node's output as the graph's output.
func (g *Graph[I, O]) AddEdge(from, to string) error {
	return g.inner.AddEdge(from, to)
}

// Compile produces a Runnable that can Invoke or Stream the graph. The
// graph must be acyclic and fully connected from START to END; otherwise
// Compile returns an error describing the missing wiring.
func (g *Graph[I, O]) Compile(ctx context.Context) (*Runnable[I, O], error) {
	r, err := g.inner.Compile(ctx)
	if err != nil {
		return nil, err
	}
	return &Runnable[I, O]{inner: r}, nil
}

// Runnable is a compiled, executable graph. Phase A exposes Invoke and
// Stream; Collect and Transform from the upstream Runnable interface are
// withheld from the v1 surface until a concrete caller needs them.
type Runnable[I, O any] struct {
	inner einocompose.Runnable[I, O]
}

// Invoke runs the graph synchronously and returns its final output.
func (r *Runnable[I, O]) Invoke(ctx context.Context, in I) (O, error) {
	return r.inner.Invoke(ctx, in)
}

// Stream runs the graph and returns a StreamReader emitting per-chunk
// outputs. The caller is responsible for calling Close on the reader
// when done; abandoning the reader leaks the underlying channel.
func (r *Runnable[I, O]) Stream(ctx context.Context, in I) (*StreamReader[O], error) {
	sr, err := r.inner.Stream(ctx, in)
	if err != nil {
		return nil, err
	}
	return &StreamReader[O]{inner: sr}, nil
}

// StreamReader is a single-consumer reader over a typed chunk stream.
// EOF semantics match the upstream library: Recv returns io.EOF on
// stream completion. Close is safe to call multiple times.
type StreamReader[T any] struct {
	inner *einoschema.StreamReader[T]
}

// Recv returns the next chunk in the stream. It returns io.EOF when the
// stream is closed and no further chunks are available.
func (s *StreamReader[T]) Recv() (T, error) {
	return s.inner.Recv()
}

// Close releases resources held by the stream.
func (s *StreamReader[T]) Close() {
	s.inner.Close()
}

// StreamReaderFromSlice wraps a finite slice of items as a StreamReader.
// It is the bridge Phase B provider adapters will use to expose
// non-streaming responses through the streaming interface, and it gives
// tests a stream they can fully drain without a goroutine.
func StreamReaderFromSlice[T any](items []T) *StreamReader[T] {
	return &StreamReader[T]{inner: einoschema.StreamReaderFromArray(items)}
}
