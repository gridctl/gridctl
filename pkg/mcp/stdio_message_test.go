package mcp

import (
	"encoding/json"
	"testing"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
)

func TestClassifyStdioMessage_ServerMessages(t *testing.T) {
	tests := []struct {
		name       string
		message    string
		wantKind   stdioMessageKind
		wantID     string
		wantError  int
		wantResult string
	}{
		{
			name:       "ping request",
			message:    `{"jsonrpc":"2.0","id":1,"method":"ping"}`,
			wantKind:   stdioRequest,
			wantID:     `1`,
			wantResult: `{}`,
		},
		{
			name:      "unsupported request with string ID",
			message:   `{"jsonrpc":"2.0","id":"server-1","method":"sampling/createMessage"}`,
			wantKind:  stdioRequest,
			wantID:    `"server-1"`,
			wantError: jsonrpc.MethodNotFound,
		},
		{
			name:     "notification",
			message:  `{"jsonrpc":"2.0","method":"notifications/cancelled"}`,
			wantKind: stdioNotification,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message, err := classifyStdioMessage([]byte(tt.message))
			if err != nil {
				t.Fatalf("classify message: %v", err)
			}
			if message.kind != tt.wantKind {
				t.Fatalf("kind = %d, want %d", message.kind, tt.wantKind)
			}
			if tt.wantKind == stdioNotification {
				if message.reply != nil || message.response != nil {
					t.Fatal("notification produced output")
				}
				return
			}
			if message.reply == nil || message.reply.ID == nil {
				t.Fatal("request did not produce a reply with an ID")
			}
			if got := string(*message.reply.ID); got != tt.wantID {
				t.Errorf("reply ID = %s, want %s", got, tt.wantID)
			}
			if tt.wantError != 0 {
				if message.reply.Error == nil || message.reply.Error.Code != tt.wantError {
					t.Fatalf("reply error = %#v, want code %d", message.reply.Error, tt.wantError)
				}
				if message.reply.Error.Message != "Method not found" {
					t.Errorf("reply error message = %q", message.reply.Error.Message)
				}
				return
			}
			if got := string(message.reply.Result); got != tt.wantResult {
				t.Errorf("reply result = %s, want %s", got, tt.wantResult)
			}
		})
	}
}

func TestClassifyStdioMessage_Response(t *testing.T) {
	message, err := classifyStdioMessage([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	if err != nil {
		t.Fatalf("classify response: %v", err)
	}
	if message.kind != stdioResponse || message.response == nil {
		t.Fatalf("classified response = %#v", message)
	}
	if message.reply != nil {
		t.Fatalf("response produced reply: %#v", message.reply)
	}
	var result map[string]bool
	if err := json.Unmarshal(message.response.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !result["ok"] {
		t.Fatalf("response result = %#v", result)
	}
}

func TestClassifyStdioMessage_InvalidJSON(t *testing.T) {
	if _, err := classifyStdioMessage([]byte(`not-json`)); err == nil {
		t.Fatal("invalid JSON classified without error")
	}
}
