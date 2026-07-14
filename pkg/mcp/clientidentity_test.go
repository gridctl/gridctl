package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientAccessIDContext(t *testing.T) {
	ctx := context.Background()
	if got := ClientAccessIDFromContext(ctx); got != "" {
		t.Errorf("empty context should return \"\", got %q", got)
	}
	// Empty id is a no-op (does not store).
	if got := ClientAccessIDFromContext(WithClientAccessID(ctx, "")); got != "" {
		t.Errorf("WithClientAccessID(\"\") should be a no-op, got %q", got)
	}
	ctx = WithClientAccessID(ctx, "cursor")
	if got := ClientAccessIDFromContext(ctx); got != "cursor" {
		t.Errorf("got %q, want cursor", got)
	}
	// Access id and telemetry client id are independent dimensions.
	ctx = WithClientID(ctx, "claude-desktop")
	if got := ClientAccessIDFromContext(ctx); got != "cursor" {
		t.Errorf("access id should be unaffected by WithClientID, got %q", got)
	}
	if got := ClientIDFromContext(ctx); got != "claude-desktop" {
		t.Errorf("client id = %q, want claude-desktop", got)
	}
}

func TestClientAccessIDFromRequest(t *testing.T) {
	tests := []struct {
		name   string
		url    string
		header string
		want   string
	}{
		{"query param", "http://x/mcp?client=team-bot", "", "team-bot"},
		{"header fallback", "http://x/mcp", "team-bot", "team-bot"},
		{"query wins over header", "http://x/mcp?client=from-query", "from-header", "from-query"},
		{"neither present", "http://x/mcp", "", ""},
		{"blank query falls through to header", "http://x/mcp?client=", "from-header", "from-header"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, tt.url, nil)
			if tt.header != "" {
				r.Header.Set(ClientAccessIDHeader, tt.header)
			}
			if got := clientAccessIDFromRequest(r); got != tt.want {
				t.Errorf("clientAccessIDFromRequest() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSessionAccessIDReconciliation covers the wire-vs-config identity
// reconciliation: the explicit link-time identifier wins, otherwise the
// normalized clientInfo.name is used, and both are normalized to a single form.
func TestSessionAccessIDReconciliation(t *testing.T) {
	tests := []struct {
		name         string
		clientName   string
		explicitID   string
		wantClientID string
		wantAccessID string
	}{
		{
			name:         "no explicit id falls back to normalized client name",
			clientName:   "Claude Code",
			explicitID:   "",
			wantClientID: "claude-code",
			wantAccessID: "claude-code",
		},
		{
			name:         "explicit id overrides the wire name",
			clientName:   "Claude Code",
			explicitID:   "team-bot",
			wantClientID: "claude-code",
			wantAccessID: "team-bot",
		},
		{
			name:         "explicit id is normalized",
			clientName:   "Cursor",
			explicitID:   "Team Bot",
			wantClientID: "cursor",
			wantAccessID: "team-bot",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewSessionManager()
			sess := m.Create(ClientInfo{Name: tt.clientName}, tt.explicitID, MCPProtocolVersion)
			if sess.ClientID != tt.wantClientID {
				t.Errorf("ClientID = %q, want %q", sess.ClientID, tt.wantClientID)
			}
			if sess.AccessID != tt.wantAccessID {
				t.Errorf("AccessID = %q, want %q", sess.AccessID, tt.wantAccessID)
			}
		})
	}
}
