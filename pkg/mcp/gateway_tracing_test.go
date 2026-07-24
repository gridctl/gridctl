package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/mock/gomock"

	"github.com/gridctl/gridctl/pkg/tracing"
)

// TestHandleToolsCall_singleTraceTree pins the tool-call span topology: one
// trace per call, with mcp.routing and mcp.client.call_tool parented under a
// root span that carries the resolved server and tool. Regression test for
// each gateway sub-span minting its own single-span root trace.
func TestHandleToolsCall_singleTraceTree(t *testing.T) {
	buf := tracing.NewBuffer(10, time.Hour)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(buf)),
	)
	old := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(old) })

	ctrl := gomock.NewController(t)
	g := NewGateway()
	github := setupMockAgentClient(ctrl, "github", []Tool{
		{Name: "create_issue", Description: "Creates an issue", InputSchema: json.RawMessage(`{}`)},
	})
	g.Router().AddClient(github)
	g.Router().RefreshTools()

	github.EXPECT().CallTool(gomock.Any(), "create_issue", gomock.Any()).Return(
		&ToolCallResult{Content: []Content{NewTextContent("ok")}}, nil,
	).Times(1)

	res, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: "github__create_issue"})
	if err != nil {
		t.Fatalf("HandleToolsCall: %v", err)
	}
	if res.IsError {
		t.Fatalf("HandleToolsCall returned error result: %+v", res)
	}

	if got := buf.Count(); got != 1 {
		t.Fatalf("trace count = %d, want exactly 1 trace for one tool call", got)
	}
	tr := buf.GetRecent(1)[0]
	if tr.SpanCount < 3 {
		t.Fatalf("SpanCount = %d, want >= 3 (root, routing, call_tool); spans: %+v", tr.SpanCount, tr.Spans)
	}
	if tr.ServerName != "github" {
		t.Errorf("ServerName = %q, want %q", tr.ServerName, "github")
	}
	if tr.Operation != "github › create_issue" {
		t.Errorf("Operation = %q, want %q", tr.Operation, "github › create_issue")
	}

	if tr.Tool != "create_issue" {
		t.Errorf("Tool = %q, want bare %q", tr.Tool, "create_issue")
	}

	rootID := ""
	for _, sp := range tr.Spans {
		if sp.ParentID == "" {
			rootID = sp.SpanID
		}
	}
	if rootID == "" {
		t.Fatal("no root span in trace record")
	}
	seen := map[string]bool{}
	for _, sp := range tr.Spans {
		switch sp.Name {
		case "mcp.routing", "mcp.client.call_tool":
			seen[sp.Name] = true
			if sp.ParentID != rootID {
				t.Errorf("span %q ParentID = %q, want root %q", sp.Name, sp.ParentID, rootID)
			}
		}
	}
	for _, want := range []string{"mcp.routing", "mcp.client.call_tool"} {
		if !seen[want] {
			t.Errorf("trace is missing child span %q", want)
		}
	}
}

// TestHandleToolsCall_bareToolOnRoutingFailure pins the tool attribute for
// calls that never route: the trace record must carry the bare tool name, not
// the client-supplied server-prefixed form. Regression test for error traces
// displaying "github__create_issue" in the Tool column.
func TestHandleToolsCall_bareToolOnRoutingFailure(t *testing.T) {
	buf := tracing.NewBuffer(10, time.Hour)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(buf)),
	)
	old := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(old) })

	g := NewGateway()

	res, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: "github__create_issue"})
	if err != nil {
		t.Fatalf("HandleToolsCall: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error result for unroutable tool")
	}

	if got := buf.Count(); got != 1 {
		t.Fatalf("trace count = %d, want 1", got)
	}
	tr := buf.GetRecent(1)[0]
	if tr.Tool != "create_issue" {
		t.Errorf("Tool = %q, want bare %q on the pre-routing failure path", tr.Tool, "create_issue")
	}
}
