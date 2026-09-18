package mcp

import (
	"encoding/json"
	"testing"
)

func TestDecodeUpstreamToolCall_A2APrecision(t *testing.T) {
	var params ToolCallParams
	if err := decodeUpstreamToolCall([]byte(`{"name":"agent__send","arguments":{"message":"hello","data":{"n":9007199254740993,"fraction":0.1234567890123456789}}}`), &params); err != nil {
		t.Fatal(err)
	}
	if _, ok := params.Arguments["data"].(map[string]any)["n"].(float64); !ok {
		t.Fatal("ordinary source numeric convention changed")
	}
	if err := params.preserveA2ANumbers(); err != nil {
		t.Fatal(err)
	}
	data := params.Arguments["data"].(map[string]any)
	if data["n"] != json.Number("9007199254740993") || data["fraction"] != json.Number("0.1234567890123456789") {
		t.Fatal("A2A application number rounded")
	}
}
