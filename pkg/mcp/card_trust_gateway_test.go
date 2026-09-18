package mcp

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

type gatewayPinSnapshot struct {
	card string
	gen  uint64
}

func (s gatewayPinSnapshot) Records() []Tool {
	return []Tool{{Name: "_agent_card", Description: s.card}, {Name: "_agent_identity", Description: "identity"}, {Name: "send"}, {Name: "hidden-by-whitelist"}}
}
func (s gatewayPinSnapshot) Hash() string       { return s.card }
func (s gatewayPinSnapshot) Generation() uint64 { return s.gen }

type gatewaySnapshotClient struct {
	AgentClient
	snapshot PinSnapshot
}

func (c *gatewaySnapshotClient) Tools() []Tool            { return []Tool{{Name: "send"}} }
func (c *gatewaySnapshotClient) PinSnapshot() PinSnapshot { return c.snapshot }

type gatewayCardStorage struct{ fail bool }

func (s *gatewayCardStorage) VerifyCard(context.Context, string, PinSnapshot) (CardPinDecision, error) {
	if s.fail {
		return CardPinDecision{}, errors.New("storage failed")
	}
	return CardPinDecision{Trusted: true}, nil
}
func (s *gatewayCardStorage) CheckCard(ctx context.Context, name string, snapshot PinSnapshot) (CardPinDecision, error) {
	return s.VerifyCard(ctx, name, snapshot)
}
func (s *gatewayCardStorage) ApproveCard(context.Context, string, PinSnapshot) error { return nil }

type gatewayToolVerifier struct {
	tools []Tool
	calls int
	err   error
}

func (v *gatewayToolVerifier) VerifyOrPin(_ string, tools []Tool) ([]SchemaDrift, error) {
	v.calls++
	v.tools = tools
	return []SchemaDrift{{Name: "hidden-by-whitelist", OldDescription: "private-card-description"}}, v.err
}

func TestGateway_VerifySnapshotPins(t *testing.T) {
	for _, mode := range []string{"off", "opt-out", "warn", "block"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			g := NewGateway()
			defer g.Close()
			var logs bytes.Buffer
			g.logger = slog.New(slog.NewTextHandler(&logs, nil))
			storage := &gatewayCardStorage{}
			if err := g.SetCardPinStorage(ctx, storage); err != nil {
				t.Fatal(err)
			}
			verifier := &gatewayToolVerifier{}
			if mode != "off" {
				g.SetSchemaVerifier(verifier, mode)
			}
			if mode == "opt-out" {
				disabled := false
				g.serverMeta["agent"] = MCPServerConfig{PinSchemas: &disabled}
			}
			first := gatewayPinSnapshot{card: "first", gen: 1}
			client := &gatewaySnapshotClient{snapshot: first}
			if err := g.verifyClientPins(ctx, "agent", client); err == nil || verifier.calls != 0 {
				t.Fatal("unregistered trust reached legacy verifier")
			}
			retired := 0
			if _, err := g.CardTrust().Register(ctx, "agent", first, func(context.Context) (PinSnapshot, error) { return client.snapshot, nil }, func() { retired++ }); err != nil {
				t.Fatal(err)
			}
			if err := g.verifyClientPins(ctx, "agent", client); err != nil {
				t.Fatal(err)
			}
			if mode == "warn" || mode == "block" {
				if len(verifier.tools) != 4 {
					t.Fatal("verification used filtered inventory")
				}
			} else if verifier.calls != 0 {
				t.Fatal("disabled legacy verification ran")
			}
			if g.blockedServers["agent"] != (mode == "block") {
				t.Fatal("tool drift policy changed")
			}
			if strings.Contains(logs.String(), "private-card-description") {
				t.Fatal("card content logged")
			}
			client.snapshot = gatewayPinSnapshot{card: "changed", gen: 1}
			calls := verifier.calls
			if err := g.verifyClientPins(ctx, "agent", client); err == nil || retired != 1 || verifier.calls != calls {
				t.Fatal("card drift reached legacy verifier")
			}
			pending, err := g.CardTrust().Snapshot(ctx, "agent")
			if err != nil || pending.Hash() != "changed" {
				t.Fatal("pending evidence unavailable")
			}
			if err := g.CardTrust().Approve(ctx, "agent", "changed"); err != nil {
				t.Fatal(err)
			}
			if g.blockedServers["agent"] != (mode == "block") {
				t.Fatal("card approval cleared unrelated block")
			}
			storage.fail = true
			if err := g.verifyClientPins(ctx, "agent", client); err == nil {
				t.Fatal("storage error admitted")
			}
			if _, err := g.CardTrust().Approved(ctx, "agent", 1); err == nil {
				t.Fatal("storage error retained admission")
			}
		})
	}
}

func TestGateway_CardTrustTeardown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "unregister", true: "shutdown"}[shutdown], func(t *testing.T) {
			ctx := context.Background()
			g := NewGateway()
			defer g.Close()
			if err := g.SetCardPinStorage(ctx, &gatewayCardStorage{}); err != nil {
				t.Fatal(err)
			}
			first := gatewayPinSnapshot{card: "first", gen: 1}
			entered, release := make(chan struct{}), make(chan struct{})
			retired := 0
			fetch := func(context.Context) (PinSnapshot, error) { close(entered); <-release; return first, nil }
			if _, err := g.CardTrust().Register(ctx, "agent", first, fetch, func() { retired++ }); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- g.CardTrust().Approve(ctx, "agent", first.Hash()) }()
			<-entered
			if shutdown {
				g.Close()
			} else if err := g.UnregisterMCPServerContext(ctx, "agent"); err != nil {
				t.Fatal(err)
			}
			close(release)
			if err := <-done; err == nil {
				t.Fatal("late approval revived retired registration")
			}
			if retired != 1 {
				t.Fatal("authority not retired exactly once")
			}
			entry := g.cardTrust.entries["agent"]
			if entry.refetch != nil || entry.retire != nil || entry.pending != nil || entry.approved != nil {
				t.Fatal("teardown retained callbacks or snapshots")
			}
			if _, err := g.CardTrust().Snapshot(ctx, "agent"); err == nil {
				t.Fatal("retired snapshot still live")
			}
			next := gatewayPinSnapshot{card: "first", gen: 2}
			_, err := g.CardTrust().Register(ctx, "agent", next, fetch, func() {})
			if (err != nil) != shutdown {
				t.Fatalf("replacement after teardown: %v", err)
			}
		})
	}
}
