package config

import "github.com/gridctl/gridctl/pkg/execution"

type ExecutionConfig = execution.ExecutionConfig
type ExecutionContract = execution.ExecutionContract
type ExecutionTmpfs = execution.ExecutionTmpfs
type ExecutionMount = execution.ExecutionMount

// ResolveExecution validates execution against the final server configuration.
func ResolveExecution(server MCPServer) (*ExecutionContract, error) {
	return execution.ResolveExecution(execution.Server{
		Execution: server.Execution, Local: server.IsLocalProcess(),
		External: server.IsExternal() || server.IsSSH() || server.IsOpenAPI(),
		Command:  server.Command, Transport: server.Transport, Network: server.Network,
		Port: server.Port, Volumes: server.Volumes,
	})
}
