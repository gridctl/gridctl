package mcp

import (
	"context"
	"testing"

	"github.com/gridctl/gridctl/pkg/execution"
	"github.com/stretchr/testify/require"
)

func TestGateway_A2ARequiresStorageAndConfiguration(t *testing.T) {
	g := NewGateway()
	for _, contract := range []*execution.ExecutionConfig{nil, {Mode: "hardened"}} {
		cfg := MCPServerConfig{Name: "agent", A2A: true, A2AConfig: &A2AClientConfig{Card: "http://127.0.0.1:1/card"}, Execution: contract}
		cfg.ExecutionCheck = func(context.Context) (*execution.Report, error) {
			t.Fatal("external source attempted container admission")
			return nil, nil
		}
		client, err := g.buildAgentClient(context.Background(), cfg)
		require.Nil(t, client)
		if contract == nil {
			require.ErrorContains(t, err, "card_pin_storage_unavailable")
		} else {
			require.ErrorContains(t, err, "execution: required admission unavailable")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := g.buildAgentClient(ctx, MCPServerConfig{A2A: true})
	require.ErrorIs(t, err, context.Canceled)
	err = g.RegisterMCPServer(t.Context(), MCPServerConfig{Name: "agent", A2A: true})
	require.ErrorContains(t, err, "configuration_required")
	g.RecordRegistrationFailure("agent", err)
	statuses := g.Status()
	require.Len(t, statuses, 1)
	require.True(t, statuses[0].A2A)
	require.True(t, statuses[0].RegistrationFailed)
	require.Empty(t, statuses[0].Endpoint)
	require.ErrorContains(t, g.RegisterAutoscaler(t.Context(), MCPServerConfig{A2A: true}, "", nil, AutoscalePolicy{}), "autoscale is unsupported")
}
