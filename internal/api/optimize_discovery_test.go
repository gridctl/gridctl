package api

import (
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
)

func TestComputeSchemaTokensDeferredModeKeepsCatalog(t *testing.T) {
	g := mcp.NewGateway()
	t.Cleanup(g.Close)
	g.Router().AddClient(newMockAgentClient("alpha", []mcp.Tool{{
		Name:        "echo",
		Description: "echo",
		InputSchema: []byte(`{"type":"object","properties":{"message":{"type":"string"}}}`),
	}}))
	g.Router().RefreshTools()
	g.SetToolDiscovery(mcp.DiscoveryDeferred, nil)
	pins, perTool := computeSchemaTokens(g)
	if pins["alpha"].SchemaTokens == 0 {
		t.Fatalf("deferred mode dropped schema tokens: %+v", pins)
	}
	if perTool["alpha"]["echo"] == 0 {
		t.Fatalf("per-tool tokens missing: %+v", perTool)
	}
}
