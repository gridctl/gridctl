package pins

import "github.com/gridctl/gridctl/pkg/mcp"

// GatewayAdapter wraps PinStore to implement mcp.SchemaVerifier.
// It bridges the pins package to the gateway without creating an import cycle:
// pkg/pins already imports pkg/mcp for the Tool type, so pkg/mcp cannot import
// pkg/pins in return. The gateway holds a mcp.SchemaVerifier interface, and
// callers wire it with a GatewayAdapter at startup.
type GatewayAdapter struct {
	store *PinStore
}

// NewGatewayAdapter creates a GatewayAdapter backed by the given PinStore.
func NewGatewayAdapter(ps *PinStore) *GatewayAdapter {
	return &GatewayAdapter{store: ps}
}

// VerifyOrPin implements mcp.SchemaVerifier.
// It delegates to PinStore.VerifyOrPin and converts the result into the
// mcp.SchemaDrift slice that the gateway consumes.
func (a *GatewayAdapter) VerifyOrPin(serverName string, tools []mcp.Tool) ([]mcp.SchemaDrift, error) {
	result, err := a.store.VerifyOrPin(serverName, tools)
	if err != nil {
		return nil, err
	}
	if len(result.ModifiedTools) == 0 {
		return nil, nil
	}
	drifts := make([]mcp.SchemaDrift, len(result.ModifiedTools))
	for i, d := range result.ModifiedTools {
		drifts[i] = mcp.SchemaDrift{
			Name:           d.Name,
			OldHash:        d.OldHash,
			NewHash:        d.NewHash,
			OldDescription: d.OldDescription,
			NewDescription: d.NewDescription,
		}
	}
	return drifts, nil
}
