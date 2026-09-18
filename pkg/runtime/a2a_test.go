package runtime

import (
	"context"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestOrchestrator_Up_A2AWithoutRuntime(t *testing.T) {
	o := NewOrchestrator(nil, nil)
	s := &config.Stack{Name: "test", MCPServers: []config.MCPServer{{Name: "agent", A2A: &config.A2AConfig{Card: "https://example.com/card"}}}}
	result, err := o.Up(context.Background(), s, UpOptions{})
	require.NoError(t, err)
	require.Len(t, result.MCPServers, 1)
	a := result.MCPServers[0]
	require.True(t, a.A2A)
	require.Equal(t, s.MCPServers[0].A2A, a.A2AConfig)
	require.Empty(t, a.WorkloadID)
	require.Empty(t, a.Endpoint)
	require.Len(t, a.Replicas, 1)
}
