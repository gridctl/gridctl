package reload

import (
	"context"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/stretchr/testify/require"
)

type a2aResetSpy struct{ resets int }

func (s *a2aResetSpy) ResetServerPins(string) error                            { s.resets++; return nil }
func (*a2aResetSpy) VerifyOrPin(string, []mcp.Tool) ([]mcp.SchemaDrift, error) { return nil, nil }

func TestHandler_A2APreservesPinsWithoutRuntime(t *testing.T) {
	for _, field := range []string{"timeout", "token", "include", "identity", "remove"} {
		t.Run(field, func(t *testing.T) {
			old := config.MCPServer{Name: "agent", A2A: &config.A2AConfig{Card: "https://example.com/card"}}
			next := old
			a := *old.A2A
			next.A2A = &a
			switch field {
			case "timeout":
				a.Timeout = "2m"
			case "token":
				a.Auth = &config.A2AAuth{Type: "bearer", Token: "${var:ROTATED}"}
			case "include":
				a.Include = []string{"help"}
			case "identity":
				a.Endpoint = "https://example.com/new-agent"
			}
			g := mcp.NewGateway()
			spy := &a2aResetSpy{}
			g.SetSchemaVerifier(spy, "block")
			h := NewHandler("stack.yaml", &config.Stack{Name: "test", MCPServers: []config.MCPServer{old}}, g, nil, 8180, 9000, nil, nil)
			registered := 0
			h.SetRegisterServerFunc(func(_ context.Context, server config.MCPServer, replicas []ReplicaRuntime, _ string) error {
				registered++
				require.True(t, server.IsA2A())
				require.Len(t, replicas, 1)
				require.Empty(t, replicas[0])
				return nil
			})
			diff := MCPServerDiff{Modified: []MCPServerChange{{Name: old.Name, Old: old, New: next}}}
			if field == "remove" {
				diff = MCPServerDiff{Removed: []config.MCPServer{old}}
			}
			result := &ReloadResult{}
			require.NoError(t, h.applyMCPServerChanges(t.Context(), diff, &config.Stack{Name: "test", MCPServers: []config.MCPServer{next}}, result))
			require.Empty(t, result.Errors)
			require.Zero(t, spy.resets)
			if field != "remove" {
				require.Equal(t, 1, registered)
			}
		})
	}
}
