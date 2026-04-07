package runtime

import (
	"context"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"go.uber.org/mock/gomock"
)

// TestOrchestrator_Up_ExternalServer tests that external MCP servers are handled correctly.
// External servers should not create containers but should be registered in the result.
func TestOrchestrator_Up_ExternalServer(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT, tracker := setupDefaultRuntime(ctrl)
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	topo := &config.Stack{
		Version: "1",
		Name:    "test-external",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		MCPServers: []config.MCPServer{
			{
				Name: "external-api",
				URL:  "https://api.example.com/mcp",
			},
			{
				Name:  "container-server",
				Image: "mcp-server:latest",
				Port:  3000,
			},
		},
	}

	ctx := context.Background()
	result, err := orch.Up(ctx, topo, UpOptions{BasePort: 9000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.MCPServers) != 2 {
		t.Fatalf("expected 2 MCP servers, got %d", len(result.MCPServers))
	}

	var externalServer *MCPServerResult
	var containerServer *MCPServerResult
	for i := range result.MCPServers {
		switch result.MCPServers[i].Name {
		case "external-api":
			externalServer = &result.MCPServers[i]
		case "container-server":
			containerServer = &result.MCPServers[i]
		}
	}

	if externalServer == nil {
		t.Fatal("external-api server not found in result")
	}
	if !externalServer.External {
		t.Error("expected External flag to be true for external-api")
	}
	if externalServer.URL != "https://api.example.com/mcp" {
		t.Errorf("expected URL 'https://api.example.com/mcp', got %q", externalServer.URL)
	}
	if externalServer.WorkloadID != "" {
		t.Errorf("expected empty WorkloadID for external server, got %q", externalServer.WorkloadID)
	}

	if containerServer == nil {
		t.Fatal("container-server not found in result")
	}
	if containerServer.External {
		t.Error("expected External flag to be false for container-server")
	}
	if containerServer.WorkloadID == "" {
		t.Error("expected non-empty WorkloadID for container server")
	}

	if len(tracker.startedWorkloads) != 1 {
		t.Errorf("expected 1 workload started, got %d", len(tracker.startedWorkloads))
	}
	if tracker.startedWorkloads[0].Name != "container-server" {
		t.Errorf("expected started workload 'container-server', got %q", tracker.startedWorkloads[0].Name)
	}
}

// TestOrchestrator_Up_LocalProcessServer tests that local process MCP servers are handled correctly.
// Local process servers should not create containers but should be registered in the result.
func TestOrchestrator_Up_LocalProcessServer(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT, tracker := setupDefaultRuntime(ctrl)
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	topo := &config.Stack{
		Version: "1",
		Name:    "test-local",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		MCPServers: []config.MCPServer{
			{
				Name:    "local-server",
				Command: []string{"npx", "-y", "@modelcontextprotocol/server-filesystem"},
			},
			{
				Name:  "container-server",
				Image: "mcp-server:latest",
				Port:  3000,
			},
		},
	}

	ctx := context.Background()
	result, err := orch.Up(ctx, topo, UpOptions{BasePort: 9000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.MCPServers) != 2 {
		t.Fatalf("expected 2 MCP servers, got %d", len(result.MCPServers))
	}

	var localServer *MCPServerResult
	for i := range result.MCPServers {
		if result.MCPServers[i].Name == "local-server" {
			localServer = &result.MCPServers[i]
			break
		}
	}

	if localServer == nil {
		t.Fatal("local-server not found in result")
	}
	if !localServer.LocalProcess {
		t.Error("expected LocalProcess flag to be true")
	}
	if len(localServer.Command) != 3 {
		t.Errorf("expected 3 command args, got %d", len(localServer.Command))
	}
	if localServer.WorkloadID != "" {
		t.Errorf("expected empty WorkloadID for local process server, got %q", localServer.WorkloadID)
	}

	if len(tracker.startedWorkloads) != 1 {
		t.Errorf("expected 1 workload started, got %d", len(tracker.startedWorkloads))
	}
}

// TestOrchestrator_Up_SSHServer tests that SSH-based MCP servers are handled correctly.
func TestOrchestrator_Up_SSHServer(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := NewMockWorkloadRuntime(ctrl)
	// No ping needed: stack has no container workloads
	mockRT.EXPECT().Ping(gomock.Any()).Times(0)
	mockRT.EXPECT().Close().Return(nil).AnyTimes()
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	topo := &config.Stack{
		Version: "1",
		Name:    "test-ssh",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		MCPServers: []config.MCPServer{
			{
				Name:    "ssh-server",
				Command: []string{"python", "-m", "mcp_server"},
				SSH: &config.SSHConfig{
					Host:         "remote.example.com",
					User:         "deploy",
					Port:         22,
					IdentityFile: "~/.ssh/id_rsa",
				},
			},
		},
	}

	ctx := context.Background()
	result, err := orch.Up(ctx, topo, UpOptions{BasePort: 9000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.MCPServers) != 1 {
		t.Fatalf("expected 1 MCP server, got %d", len(result.MCPServers))
	}

	sshServer := result.MCPServers[0]
	if !sshServer.SSH {
		t.Error("expected SSH flag to be true")
	}
	if sshServer.SSHHost != "remote.example.com" {
		t.Errorf("expected SSHHost 'remote.example.com', got %q", sshServer.SSHHost)
	}
	if sshServer.SSHUser != "deploy" {
		t.Errorf("expected SSHUser 'deploy', got %q", sshServer.SSHUser)
	}
	if sshServer.SSHPort != 22 {
		t.Errorf("expected SSHPort 22, got %d", sshServer.SSHPort)
	}
	if sshServer.WorkloadID != "" {
		t.Errorf("expected empty WorkloadID for SSH server, got %q", sshServer.WorkloadID)
	}
}

// TestOrchestrator_Up_MixedServerTypes tests a stack with all server types.
func TestOrchestrator_Up_MixedServerTypes(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT, tracker := setupDefaultRuntime(ctrl)
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	topo := &config.Stack{
		Version: "1",
		Name:    "test-mixed",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		MCPServers: []config.MCPServer{
			{
				Name: "external-api",
				URL:  "https://api.example.com/mcp",
			},
			{
				Name:    "local-server",
				Command: []string{"node", "server.js"},
			},
			{
				Name:    "ssh-server",
				Command: []string{"python", "server.py"},
				SSH: &config.SSHConfig{
					Host: "remote.example.com",
					User: "deploy",
				},
			},
			{
				Name:  "container-1",
				Image: "mcp-server:v1",
				Port:  3000,
			},
			{
				Name:  "container-2",
				Image: "mcp-server:v2",
				Port:  3001,
			},
		},
	}

	ctx := context.Background()
	result, err := orch.Up(ctx, topo, UpOptions{BasePort: 9000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.MCPServers) != 5 {
		t.Fatalf("expected 5 MCP servers, got %d", len(result.MCPServers))
	}

	var external, local, ssh, container int
	for _, s := range result.MCPServers {
		if s.External {
			external++
		} else if s.LocalProcess {
			local++
		} else if s.SSH {
			ssh++
		} else {
			container++
		}
	}

	if external != 1 {
		t.Errorf("expected 1 external server, got %d", external)
	}
	if local != 1 {
		t.Errorf("expected 1 local server, got %d", local)
	}
	if ssh != 1 {
		t.Errorf("expected 1 SSH server, got %d", ssh)
	}
	if container != 2 {
		t.Errorf("expected 2 container servers, got %d", container)
	}

	if len(tracker.startedWorkloads) != 2 {
		t.Errorf("expected 2 workloads started, got %d", len(tracker.startedWorkloads))
	}
}

// TestOrchestrator_Up_BasePortConfiguration tests that BasePort is correctly used.
func TestOrchestrator_Up_BasePortConfiguration(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT, _ := setupDefaultRuntime(ctrl)
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	topo := &config.Stack{
		Version: "1",
		Name:    "test-ports",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "mcp:v1", Port: 3000},
			{Name: "server2", Image: "mcp:v2", Port: 3001},
			{Name: "server3", Image: "mcp:v3", Port: 3002},
		},
	}

	ctx := context.Background()
	result, err := orch.Up(ctx, topo, UpOptions{BasePort: 10000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedPorts := []int{10000, 10001, 10002}
	for i, server := range result.MCPServers {
		if server.HostPort != expectedPorts[i] {
			t.Errorf("server %d: expected port %d, got %d", i, expectedPorts[i], server.HostPort)
		}
	}
}

// TestOrchestrator_Up_DefaultBasePort tests that default BasePort (9000) is used when not specified.
func TestOrchestrator_Up_DefaultBasePort(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT, _ := setupDefaultRuntime(ctrl)
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	topo := &config.Stack{
		Version: "1",
		Name:    "test-default-port",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "mcp:v1", Port: 3000},
		},
	}

	ctx := context.Background()
	result, err := orch.Up(ctx, topo, UpOptions{BasePort: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.MCPServers[0].HostPort != 9000 {
		t.Errorf("expected default port 9000, got %d", result.MCPServers[0].HostPort)
	}
}
