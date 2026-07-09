package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/gridctl/gridctl/pkg/logging"
)

// The MCP spec allows CallToolResult.structuredContent alongside content, and
// Tool.outputSchema alongside inputSchema. The gateway must forward both
// byte-for-byte, exactly as it already does for text content and isError —
// upstream servers own the shapes.

func TestToolCallResult_StructuredContentRoundTrip(t *testing.T) {
	// Wire shape in (as an upstream server sends it) …
	upstream := []byte(`{"content":[{"type":"text","text":"hello"}],"structuredContent":{"status":"ok","score":7}}`)
	var result ToolCallResult
	if err := json.Unmarshal(upstream, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.StructuredContent) == 0 {
		t.Fatal("structuredContent lost on unmarshal")
	}

	// … and back out (as the gateway re-encodes it for the client).
	out, err := json.Marshal(&result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(out, &wire); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if string(wire["structuredContent"]) != `{"status":"ok","score":7}` {
		t.Errorf("structuredContent not preserved on the wire: %s", wire["structuredContent"])
	}
}

func TestToolCallResult_StructuredContentOmittedWhenAbsent(t *testing.T) {
	out, err := json.Marshal(&ToolCallResult{Content: []Content{NewTextContent("x")}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(out, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := wire["structuredContent"]; present {
		t.Error("structuredContent must be omitted when the upstream result has none")
	}
}

func TestGateway_CallTool_StructuredContentPassthrough(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	ctx := context.Background()

	structured := json.RawMessage(`{"status":"ok","verdict":{"result":"pass"}}`)
	client := setupMockAgentClient(ctrl, "agent1", []Tool{
		{Name: "probe", Description: "Probe tool"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		&ToolCallResult{
			Content:           []Content{NewTextContent("hello")},
			StructuredContent: structured,
		}, nil,
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	result, err := g.CallTool(ctx, "agent1__probe", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result.StructuredContent) != string(structured) {
		t.Errorf("structuredContent not passed through the gateway: %s", result.StructuredContent)
	}
	if result.Content[0].Text != "hello" {
		t.Errorf("text content changed: %+v", result.Content)
	}
}

func TestGateway_CallTool_StructuredContentDroppedOverLimit(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetLogger(logging.NewDiscardLogger())
	g.SetMaxToolResultBytes(100)
	ctx := context.Background()

	oversized := json.RawMessage(`{"blob":"` + strings.Repeat("a", 500) + `"}`)
	client := setupMockAgentClient(ctrl, "agent1", []Tool{
		{Name: "probe", Description: "Probe tool"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		&ToolCallResult{
			Content:           []Content{NewTextContent("ok")},
			StructuredContent: oversized,
		}, nil,
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "agent1"})

	result, err := g.HandleToolsCall(ctx, ToolCallParams{Name: "agent1__probe", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StructuredContent != nil {
		t.Errorf("oversized structuredContent must be dropped, got %d bytes", len(result.StructuredContent))
	}
	notice := result.Content[len(result.Content)-1].Text
	if !strings.Contains(notice, "structuredContent dropped") {
		t.Errorf("expected a drop notice in content, got: %s", notice)
	}
}

func TestGateway_CallTool_StructuredContentKeptUnderLimit(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetLogger(logging.NewDiscardLogger())
	g.SetMaxToolResultBytes(1000)
	ctx := context.Background()

	structured := json.RawMessage(`{"status":"ok"}`)
	client := setupMockAgentClient(ctrl, "agent1", []Tool{
		{Name: "probe", Description: "Probe tool"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		&ToolCallResult{
			Content:           []Content{NewTextContent("ok")},
			StructuredContent: structured,
		}, nil,
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "agent1"})

	result, err := g.HandleToolsCall(ctx, ToolCallParams{Name: "agent1__probe", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result.StructuredContent) != string(structured) {
		t.Errorf("structuredContent under the limit must pass through unchanged: %s", result.StructuredContent)
	}
	if len(result.Content) != 1 {
		t.Errorf("no drop notice expected, got content: %+v", result.Content)
	}
}

func TestSandbox_ToolCall_StructuredContentPreferred(t *testing.T) {
	sandbox := NewSandbox(5 * time.Second)
	caller := &mockToolCaller{
		callFn: func(context.Context, string, map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{
				Content:           []Content{NewTextContent("Verdict: pass (score 7)")},
				StructuredContent: json.RawMessage(`{"verdict":"pass","score":7}`),
			}, nil
		},
	}
	allowedTools := []Tool{{Name: "myserver__probe"}}

	code := `var r = mcp.callTool("myserver", "probe", {}); r.verdict + ":" + r.score;`
	result, err := sandbox.Execute(context.Background(), code, caller, allowedTools)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Value != `"pass:7"` {
		t.Errorf("expected structuredContent fields, got %s", result.Value)
	}
}

func TestSandbox_ToolCall_TextFallbackWithoutStructuredContent(t *testing.T) {
	sandbox := NewSandbox(5 * time.Second)
	caller := &mockToolCaller{
		callFn: func(context.Context, string, map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{Content: []Content{NewTextContent(`{"items":[1,2,3]}`)}}, nil
		},
	}
	allowedTools := []Tool{{Name: "myserver__probe"}}

	code := `mcp.callTool("myserver", "probe", {}).items.length;`
	result, err := sandbox.Execute(context.Background(), code, caller, allowedTools)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Value != "3" {
		t.Errorf("expected text-content fallback to still parse, got %s", result.Value)
	}
}

func TestRouter_ToolOutputSchemaPreserved(t *testing.T) {
	ctrl := gomock.NewController(t)
	r := NewRouter()
	outputSchema := json.RawMessage(`{"type":"object","properties":{"status":{"type":"string"}}}`)
	client := setupMockAgentClient(ctrl, "agent1", []Tool{
		{Name: "probe", Description: "Probe tool", OutputSchema: outputSchema},
	})
	r.AddClient(client)
	r.RefreshTools()

	for _, tools := range map[string][]Tool{
		"AggregatedTools": r.AggregatedTools(),
		"CatalogTools":    r.CatalogTools(),
	} {
		if len(tools) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(tools))
		}
		if string(tools[0].OutputSchema) != string(outputSchema) {
			t.Errorf("outputSchema not preserved: %s", tools[0].OutputSchema)
		}
	}
}
