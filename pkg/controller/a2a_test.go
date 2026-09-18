package controller

import (
	"context"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestA2A_StatusAndRegistrarWithoutRuntime(t *testing.T) {
	s := config.MCPServer{Name: "agent", A2A: &config.A2AConfig{Card: "https://example.com/card", Include: []string{"help"}, Auth: &config.A2AAuth{Type: "bearer", Token: "${var:A2A_TOKEN}"}}}
	stack := &config.Stack{Name: "test", MCPServers: []config.MCPServer{s}}
	result, err := getRunningContainers(context.Background(), nil, stack)
	require.NoError(t, err)
	require.Len(t, result.MCPServers, 1)
	require.True(t, result.MCPServers[0].A2A)
	require.Empty(t, result.MCPServers[0].Endpoint)
	require.Empty(t, result.MCPServers[0].WorkloadID)
	summaries := BuildWorkloadSummaries(stack, result)
	require.Len(t, summaries, 1)
	require.Equal(t, "a2a", summaries[0].Transport)
	r := &ServerRegistrar{}
	bulk := r.buildReplicaConfigs(result.MCPServers[0], s, "stack.yaml")
	single := r.buildConfigFromMCPServer(s, 0, "", "stack.yaml")
	require.Len(t, bulk, 1)
	require.Equal(t, single, bulk[0])
	require.True(t, single.A2A)
	require.Equal(t, 5*time.Minute, single.A2AConfig.Timeout)
	require.Equal(t, "auto", single.A2AConfig.Dialect)
	require.Equal(t, s.A2A.Auth.Token, single.A2AConfig.Token)
	single.A2AConfig.Include[0] = "changed"
	require.Equal(t, "help", s.A2A.Include[0])

	s.A2A.Timeout = "2m"
	s.Execution = &config.ExecutionConfig{Mode: "hardened"}
	single = r.buildConfigFromMCPServer(s, 0, "", "stack.yaml")
	require.Equal(t, 2*time.Minute, single.A2AConfig.Timeout)
	require.NotNil(t, single.Execution)
	require.Nil(t, single.ExecutionCheck)
}
