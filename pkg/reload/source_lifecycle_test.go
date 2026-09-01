package reload

import (
	"context"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runtime"
)

type recordingReloadBuilder struct {
	calls []runtime.BuildOptions
}

func (b *recordingReloadBuilder) Build(_ context.Context, opts runtime.BuildOptions) (*runtime.BuildResult, error) {
	b.calls = append(b.calls, opts)
	return &runtime.BuildResult{ImageTag: "gridctl-source:" + opts.Ref}, nil
}

func TestComputeDiff_SourceRefChangeRestartsServer(t *testing.T) {
	oldServer := config.MCPServer{
		Name:   "source",
		Source: &config.Source{Type: "git", URL: "https://example.com/source.git", Ref: "commit-a"},
	}
	newServer := oldServer
	newServer.Source = &config.Source{Type: "git", URL: "https://example.com/source.git", Ref: "commit-b"}

	diff := ComputeDiff(
		&config.Stack{MCPServers: []config.MCPServer{oldServer}},
		&config.Stack{MCPServers: []config.MCPServer{newServer}},
	)

	if len(diff.MCPServers.Modified) != 1 {
		t.Fatalf("Modified = %d, want 1 for source ref change", len(diff.MCPServers.Modified))
	}
}

func TestHandler_StartMCPServer_BuildsChangedSourceBeforeReplacement(t *testing.T) {
	t.Skip("pending source lifecycle reconciliation during reload")

	rt := newMockWorkloadRuntime()
	builder := &recordingReloadBuilder{}
	orch := runtime.NewOrchestrator(rt, builder)
	handler := NewHandler("stack.yaml", &config.Stack{Name: "demo"}, mcp.NewGateway(), orch, 8180, 9000, nil, nil)
	server := config.MCPServer{
		Name:   "source",
		Source: &config.Source{Type: "git", URL: "https://example.com/source.git", Ref: "commit-b"},
		Port:   3000,
	}
	stack := &config.Stack{Name: "demo", Network: config.Network{Name: "demo-net"}}

	if err := handler.startMCPServer(context.Background(), server, stack); err != nil {
		t.Fatalf("startMCPServer: %v", err)
	}
	if len(builder.calls) != 1 {
		t.Fatalf("Build calls = %d, want 1 before replacement start", len(builder.calls))
	}
}
