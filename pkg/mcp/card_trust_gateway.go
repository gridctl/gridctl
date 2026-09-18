package mcp

import (
	"context"
	"errors"

	"github.com/gridctl/gridctl/pkg/logging"
)

// SetCardPinStorage installs mandatory card trust storage before registration.
// Unlike SetSchemaVerifier this is independent of optional tool pinning.
func (g *Gateway) SetCardPinStorage(ctx context.Context, storage CardPinStorage) error {
	service := g.CardTrust()
	if err := service.lock(ctx); err != nil {
		return err
	}
	defer service.unlock()
	if len(service.entries) != 0 {
		return errors.New("a2a: card pin storage already in use")
	}
	service.storage = storage
	return nil
}

// CardTrust returns the gateway-owned mandatory card trust service.
func (g *Gateway) CardTrust() *CardTrustService {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.cardTrust
}

// Mandatory verification precedes the legacy verifier, which may auto-pin new
// records. In particular, it must never fill in a missing identity as a side
// effect of ordinary tool verification.
func (g *Gateway) verifyClientPins(ctx context.Context, name string, client AgentClient) error {
	tools := client.Tools()
	source, mandatory := client.(PinSnapshotSource)
	if mandatory {
		snapshot := source.PinSnapshot()
		if err := g.cardTrust.Observe(ctx, name, snapshot); err != nil {
			return err
		}
		tools = snapshot.Records()
	}
	if !g.pinningEnabledForServer(name) {
		return nil
	}
	drifts, err := g.schemaVerifier.VerifyOrPin(name, tools)
	if err != nil {
		if mandatory {
			return errors.New("a2a: tool pin verification failed")
		}
		g.logger.Warn("pins: verification failed", "server", name, "error", err)
		return nil
	}
	if mandatory {
		generated := make([]SchemaDrift, 0, len(drifts))
		for _, drift := range drifts {
			if drift.Name != "_agent_card" && drift.Name != "_agent_identity" {
				// Descriptions and findings can contain arbitrary card content.
				// The pins API retains review evidence; diagnostics get names only.
				generated = append(generated, SchemaDrift{Name: logging.RedactString(drift.Name)})
			}
		}
		drifts = generated
	}
	g.handlePinDrift(name, drifts)
	return nil
}
