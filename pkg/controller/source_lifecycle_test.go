package controller

import (
	"context"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runtime"
)

func TestServerRegistrar_AutoscaledSourceUsesResolvedImage(t *testing.T) {
	t.Skip("pending resolved source image plumbing for autoscaled servers")

	rt := &stubContainerRuntime{startStatus: runtime.WorkloadStatus{ID: "source-1"}}
	registrar := NewServerRegistrar(mcp.NewGateway(), false)
	registrar.SetRuntime(rt)
	server := config.MCPServer{
		Name:      "source",
		Image:     "gridctl-source:commit-a",
		Source:    &config.Source{Type: "git", URL: "https://example.com/source.git", Ref: "commit-a"},
		Transport: "stdio",
		Autoscale: &config.AutoscaleConfig{Min: 1, Max: 1, TargetInFlight: 1},
	}
	stack := &config.Stack{Name: "demo", Network: config.Network{Name: "demo-net"}}

	if err := registrar.registerAutoscaled(context.Background(), server, stack, "stack.yaml", 9000); err != nil {
		t.Fatalf("registerAutoscaled: %v", err)
	}
	if len(rt.startCalls) != 1 {
		t.Fatalf("Start calls = %d, want 1 initial replica", len(rt.startCalls))
	}
	if rt.startCalls[0].Image != server.Image {
		t.Fatalf("Start image = %q, want resolved image %q", rt.startCalls[0].Image, server.Image)
	}
	if rt.startCalls[0].Image == "gridctl-demo-source:latest" {
		t.Fatalf("Start reconstructed mutable source image %q", rt.startCalls[0].Image)
	}
}
