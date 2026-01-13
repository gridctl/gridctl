package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClient_ParseSSEResponse_Notifications(t *testing.T) {
	// Simulate an SSE stream with a notification followed by a result
	sseBody := `event: message
data: {"jsonrpc":"2.0","method":"notifications/message","params":{"level":"info","data":{"msg":"some log"}}}

event: message
data: {"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"success"}]}}
`

	client := &Client{}
	resp, err := client.parseSSEResponse(strings.NewReader(sseBody))
	if err != nil {
		t.Fatalf("parseSSEResponse failed: %v", err)
	}

	if resp.ID == nil {
		t.Fatal("expected response to have ID")
	}

	// Verify it picked the result, not the notification
	var result map[string]any
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// Check content
	// {"content":[{"type":"text","text":"success"}]}
	content, ok := result["content"].([]any)
	if !ok {
		t.Fatalf("expected content array")
	}
	if len(content) != 1 {
		t.Fatalf("expected 1 content item")
	}
}

func TestClient_ParseSSEResponse_OnlyNotification(t *testing.T) {
	// Simulate an SSE stream with only a notification
	sseBody := `event: message
data: {"jsonrpc":"2.0","method":"notifications/message","params":{"level":"info"}}
`

	client := &Client{}
	_, err := client.parseSSEResponse(strings.NewReader(sseBody))
	if err == nil {
		t.Fatal("expected error when no response with ID is found")
	}
	if !strings.Contains(err.Error(), "no response with ID") {
		t.Errorf("expected error message 'no response with ID', got: %v", err)
	}
}

func TestClient_ParseSSEResponse_MalformedData(t *testing.T) {
	// Simulate malformed data lines
	sseBody := `event: message
data: not-json

event: message
data: {"jsonrpc":"2.0","id":1,"result":{}}
`

	client := &Client{}
	resp, err := client.parseSSEResponse(strings.NewReader(sseBody))
	if err != nil {
		t.Fatalf("parseSSEResponse failed with malformed data skipped: %v", err)
	}
	if resp.ID == nil {
		t.Fatal("expected valid response despite previous malformed line")
	}
}
