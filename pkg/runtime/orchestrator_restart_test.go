package runtime

import (
	"context"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"go.uber.org/mock/gomock"
)

// TestOrchestrator_Up_MCPServer_RestartsStoppedContainer reproduces the bug
// where `apply` silently handed a stopped container to the MCP client
// builder instead of restarting it first: startMCPServer's "exists" branch
// returned early on Exists() alone, without ever checking run state or
// calling Start, so a container left Exited by a previous shutdown was
// never brought back up. The gateway then hung on the stdio initialize
// handshake against a process that wasn't running.
func TestOrchestrator_Up_MCPServer_RestartsStoppedContainer(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := NewMockWorkloadRuntime(ctrl)
	mockBuilder := &MockBuilder{}

	mockRT.EXPECT().Ping(gomock.Any()).Return(nil).AnyTimes()
	mockRT.EXPECT().Close().Return(nil).AnyTimes()
	mockRT.EXPECT().EnsureNetwork(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	existingID := WorkloadID("existing-github-container")
	mockRT.EXPECT().Exists(gomock.Any(), gomock.Any()).Return(true, existingID, nil).AnyTimes()

	// The container exists but is stopped, matching `docker ps -a` showing
	// "Exited (0)" after a previous gridctl shutdown.
	mockRT.EXPECT().Status(gomock.Any(), existingID).Return(&WorkloadStatus{
		ID:    existingID,
		State: WorkloadStateStopped,
		Image: "ghcr.io/github/github-mcp-server:latest",
	}, nil).Times(1)

	mockRT.EXPECT().Start(gomock.Any(), gomock.Any()).Return(&WorkloadStatus{
		ID:    existingID,
		State: WorkloadStateRunning,
	}, nil).Times(1)

	mockRT.EXPECT().GetHostPort(gomock.Any(), existingID, gomock.Any()).Return(0, nil).AnyTimes()

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	topo := &config.Stack{
		Version: "1",
		Name:    "test-restart",
		Network: config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{
			{
				Name:      "github",
				Image:     "ghcr.io/github/github-mcp-server:latest",
				Transport: "stdio",
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
}
