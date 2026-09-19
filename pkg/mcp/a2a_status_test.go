package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestA2AClient_Status(t *testing.T) {
	f := newA2AUnitFixture(t, "1.0", true)
	c := f.client(t, "bedrock", 0)
	first := a2aUnitCall(t, c, "send", map[string]any{"message": "hello"})
	status := c.status()
	if status.CardTrust != "approved" || status.Dialect != "1.0" || status.LiveRoots != 1 || status.LiveTasks != 1 || status.UncertainRoots != 0 {
		t.Fatalf("unexpected safe status: %+v", status)
	}
	body, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	secrets := []string{first["task_handle"].(string), first["context_handle"].(string), f.discovery, f.sessions[0], f.server.URL}
	f.mu.Unlock()
	for _, secret := range secrets {
		if strings.Contains(string(body), secret) {
			t.Fatal("status disclosed private authority")
		}
	}
	c.store.mu.Lock()
	for root := range c.authority.roots {
		root.unknownWork = true
	}
	c.store.mu.Unlock()
	if c.status().UncertainRoots != 1 {
		t.Fatal("uncertain root absent")
	}
	c.store.mu.Lock()
	for root := range c.authority.roots {
		root.expires = time.Now().Add(-time.Second)
	}
	c.store.mu.Unlock()
	if status := c.status(); status.LiveRoots != 0 || status.LiveTasks != 0 || status.ExpiredRoots != 1 {
		t.Fatalf("expired authority counted as live: %+v", status)
	}
	f.cardRevision.Add(1)
	if err := c.RefreshTools(context.Background()); err == nil {
		t.Fatal("expected drift")
	}
	if status := c.status(); status.CardTrust != "approval_required" || status.LiveRoots != 0 {
		t.Fatalf("retired authority counted as live: %+v", status)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if c.status().CardTrust != "unavailable" {
		t.Fatal("closed adapter reported trust")
	}
}

func TestA2AClient_HealthDoesNotDelayApproval(t *testing.T) {
	ctx := context.Background()
	f := newA2AUnitFixture(t, "0.3", true)
	g := NewGateway()
	t.Cleanup(g.Close)
	if err := g.SetCardPinStorage(ctx, &a2aUnitPins{}); err != nil {
		t.Fatal(err)
	}
	if err := g.RegisterMCPServer(ctx, MCPServerConfig{Name: "agent", A2A: true, A2AConfig: &A2AClientConfig{Card: f.server.URL}}); err != nil {
		t.Fatal(err)
	}
	c := g.router.GetClient("agent").(*A2AClient)
	f.cardRevision.Add(1)
	if err := c.RefreshTools(ctx); err == nil {
		t.Fatal("expected drift")
	}
	g.checkHealth(ctx)
	if status := g.Status(); len(status) != 1 || status[0].A2AStatus.CardTrust != "approval_required" {
		t.Fatalf("missing trust status: %+v", status)
	}
	if err := g.cardTrust.Approve(ctx, "agent", c.PinSnapshot().Hash()); err != nil {
		t.Fatal(err)
	}
	result, err := g.CallTool(ctx, "agent__send", map[string]any{"message": "approved"})
	if err != nil || result.IsError {
		t.Fatalf("approval requires another health tick: %v, %+v", err, result)
	}
}

func TestGateway_A2AStatusSanitizesDiagnosticNames(t *testing.T) {
	f := newA2AUnitFixture(t, "1.0", true)
	g := NewGateway()
	t.Cleanup(g.Close)
	handle := sensitiveSentinel(t)
	if err := g.SetCardPinStorage(t.Context(), &a2aUnitPins{}); err != nil {
		t.Fatal(err)
	}
	if err := g.RegisterMCPServer(t.Context(), MCPServerConfig{Name: "a", A2A: true, Tools: []string{"skill-" + handle}, A2AConfig: &A2AClientConfig{Card: f.server.URL}}); err != nil {
		t.Fatal(err)
	}
	client := g.router.GetClient("a").(*A2AClient)
	client.SetTools([]Tool{{Name: "skill-" + handle}})
	status, err := json.Marshal(g.Status())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(status), handle) {
		t.Fatal("diagnostic status retained capability-shaped tool/whitelist names")
	}
	if client.Tools()[0].Name != "skill-"+handle {
		t.Fatal("diagnostic sanitation changed callable tool identity")
	}
}
