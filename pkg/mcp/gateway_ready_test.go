package mcp

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGateway_ExpectedRegistration_StatusRow(t *testing.T) {
	g := NewGateway()
	if got := g.AwaitingRegistration(); got == nil || len(got) != 0 {
		t.Fatalf("AwaitingRegistration() = %#v, want empty non-nil slice", got)
	}

	g.ExpectRegistration(
		ExpectedRegistration{Name: ""},
		ExpectedRegistration{Name: "zeta", External: true},
		ExpectedRegistration{Name: "echo", LocalProcess: true},
	)
	if got := g.AwaitingRegistration(); len(got) != 2 || got[0] != "echo" || got[1] != "zeta" {
		t.Fatalf("AwaitingRegistration() = %v, want [echo zeta]", got)
	}

	var row MCPServerStatus
	found := false
	for _, status := range g.Status() {
		if status.Name == "echo" {
			row = status
			found = true
		}
	}
	if !found {
		t.Fatal("Status() missing awaiting row echo")
	}
	if row.Initialized {
		t.Error("Initialized = true, want false")
	}
	if row.RegistrationFailed {
		t.Error("RegistrationFailed = true, want false")
	}
	if row.Healthy != nil {
		t.Errorf("Healthy = %v, want nil", *row.Healthy)
	}
	if !row.LocalProcess || row.External || row.SSH || row.OpenAPI || row.A2A {
		t.Errorf("kind flags = %+v, want LocalProcess only", row)
	}
	if row.Tools == nil || len(row.Tools) != 0 {
		t.Errorf("Tools = %#v, want empty non-nil slice", row.Tools)
	}

	// A name already in serverMeta keeps that row. The awaiting copy must
	// not add a second one.
	g.SetServerMeta(MCPServerConfig{Name: "echo", Transport: TransportHTTP})
	echoRows := 0
	for _, status := range g.Status() {
		if status.Name == "echo" {
			echoRows++
		}
	}
	if echoRows != 1 {
		t.Fatalf("Status() echo rows = %d, want 1", echoRows)
	}
}

func TestGateway_ExpectedRegistration_FailureDoesNotResolve(t *testing.T) {
	g := NewGateway()
	g.ExpectRegistration(ExpectedRegistration{Name: "echo", LocalProcess: true})

	g.RecordRegistrationFailure("echo", errors.New("boom"))
	g.RecordRegistrationFailure("echo", &NeedsAuthError{Server: "echo"})
	g.ClearRegistrationFailure("echo")

	if got := g.AwaitingRegistration(); len(got) != 1 || got[0] != "echo" {
		t.Fatalf("AwaitingRegistration() = %v, want [echo]", got)
	}
}

func TestGateway_ExpectedRegistration_ResolveAndUnregister(t *testing.T) {
	g := NewGateway()
	g.ExpectRegistration(ExpectedRegistration{Name: "echo", LocalProcess: true})
	g.ResolveExpectedRegistration("echo")
	if got := g.AwaitingRegistration(); len(got) != 0 {
		t.Fatalf("after resolve, awaiting = %v", got)
	}

	g.ExpectRegistration(ExpectedRegistration{Name: "echo", LocalProcess: true})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	if err := g.UnregisterMCPServerContext(ctx, "echo"); err != nil {
		t.Fatalf("UnregisterMCPServerContext: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("unregister blocked for %s while a declaration was outstanding", elapsed)
	}
	if got := g.AwaitingRegistration(); len(got) != 0 {
		t.Fatalf("after UnregisterMCPServerContext, awaiting = %v", got)
	}

	g.ExpectRegistration(ExpectedRegistration{Name: "echo", LocalProcess: true})
	g.UnregisterMCPServer("echo")
	if got := g.AwaitingRegistration(); len(got) != 0 {
		t.Fatalf("after UnregisterMCPServer, awaiting = %v", got)
	}
}

func TestGateway_ExpectedRegistration_RegisterClears(t *testing.T) {
	srv := httptest.NewServer(pendingTestMCPHandler())
	defer srv.Close()

	g := NewGateway()
	g.ExpectRegistration(ExpectedRegistration{Name: "echo", External: true, Transport: TransportHTTP})
	cfg := MCPServerConfig{
		Name:               "echo",
		Transport:          TransportHTTP,
		Endpoint:           srv.URL,
		External:           true,
		ReadyTimeout:       2 * time.Second,
		ProtocolGeneration: GenerationHandshake,
	}
	if err := g.RegisterMCPReplicaSet(context.Background(), "echo", ReplicaPolicyRoundRobin, []MCPServerConfig{cfg}); err != nil {
		t.Fatalf("RegisterMCPReplicaSet: %v", err)
	}
	t.Cleanup(func() { g.UnregisterMCPServer("echo") })
	if got := g.AwaitingRegistration(); len(got) != 0 {
		t.Fatalf("successful registration left awaiting %v", got)
	}
}

func TestGateway_ExpectedRegistration_RedeclarationReplacesFlags(t *testing.T) {
	g := NewGateway()
	// A leftover a2aDeclarations entry must not supply A2A for the
	// transient row. That map is filled only by RegisterMCPReplicaSet.
	g.pendingMu.Lock()
	g.a2aDeclarations = map[string]bool{"echo": true}
	g.pendingMu.Unlock()

	g.ExpectRegistration(ExpectedRegistration{Name: "echo", LocalProcess: true, Transport: TransportStdio})
	g.ExpectRegistration(ExpectedRegistration{Name: "echo", External: true, Transport: TransportSSE})

	var row MCPServerStatus
	for _, status := range g.Status() {
		if status.Name == "echo" {
			row = status
		}
	}
	if row.Name != "echo" {
		t.Fatal("Status() missing redeclared echo")
	}
	if !row.External || row.LocalProcess || row.A2A || row.Transport != TransportSSE {
		t.Errorf("row = %+v, want External transport sse and A2A false", row)
	}
}
