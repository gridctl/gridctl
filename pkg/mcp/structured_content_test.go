package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"go.uber.org/mock/gomock"
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
