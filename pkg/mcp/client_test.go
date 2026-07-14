package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
)

func TestClient_SendsNegotiatedProtocolVersionHeader(t *testing.T) {
	var mu sync.Mutex
	headersByMethod := make(map[string]string)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonrpc.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		mu.Lock()
		headersByMethod[req.Method] = r.Header.Get("MCP-Protocol-Version")
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			result := InitializeResult{
				ProtocolVersion: "2025-06-18",
				ServerInfo:      ServerInfo{Name: "test", Version: "1.0"},
			}
			_ = json.NewEncoder(w).Encode(jsonrpc.NewSuccessResponse(req.ID, result))
		case "tools/list":
			_ = json.NewEncoder(w).Encode(jsonrpc.NewSuccessResponse(req.ID, ToolsListResult{}))
		default:
			_ = json.NewEncoder(w).Encode(jsonrpc.NewSuccessResponse(req.ID, nil))
		}
	}))
	defer ts.Close()

	c := NewClient("test", ts.URL)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := c.RefreshTools(context.Background()); err != nil {
		t.Fatalf("RefreshTools: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if v := headersByMethod["initialize"]; v != "" {
		t.Errorf("initialize must not carry a negotiated version header, got %q", v)
	}
	if v := headersByMethod["tools/list"]; v != "2025-06-18" {
		t.Errorf("expected post-initialize requests to carry the negotiated version, got %q", v)
	}
}

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
