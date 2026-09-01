package controller

import (
	"context"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runtime"
)

func TestServerRegistrar_AutoscaledSourceUsesResolvedImage(t *testing.T) {
	rt := &stubContainerRuntime{startStatus: runtime.WorkloadStatus{ID: "source-1"}}
	registrar := NewServerRegistrar(mcp.NewGateway(), false)
	registrar.SetRuntime(rt)
	server := config.MCPServer{
		Name:      "source",
		Source:    &config.Source{Type: "git", URL: "https://example.com/source.git", Ref: "commit-a"},
		Transport: "stdio",
		Volumes:   []string{"/host/data:/data:ro"},
		Autoscale: &config.AutoscaleConfig{Min: 1, Max: 1, TargetInFlight: 1},
	}
	stack := &config.Stack{Name: "demo", Network: config.Network{Name: "demo-net"}, MCPServers: []config.MCPServer{server}}
	desiredImage := "gridctl-source:commit-a"
	result := &runtime.UpResult{MCPServers: []runtime.MCPServerResult{{Name: server.Name, Image: desiredImage}}}

	registrar.RegisterAll(context.Background(), result, stack, "stack.yaml")
	if len(rt.startCalls) != 1 {
		t.Fatalf("Start calls = %d, want 1 initial replica", len(rt.startCalls))
	}
	if rt.startCalls[0].Image != desiredImage {
		t.Fatalf("Start image = %q, want resolved image %q", rt.startCalls[0].Image, desiredImage)
	}
	if rt.startCalls[0].Image == "gridctl-demo-source:latest" {
		t.Fatalf("Start reconstructed mutable source image %q", rt.startCalls[0].Image)
	}
	if len(rt.startCalls[0].Volumes) != 1 || rt.startCalls[0].Volumes[0] != "/host/data:/data:ro" {
		t.Fatalf("Start volumes = %v, want source server mount", rt.startCalls[0].Volumes)
	}
}
