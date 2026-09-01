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

type recordingReloadRuntime struct {
	*mockWorkloadRuntime
	ensured []string
	started []runtime.WorkloadConfig
	stopped []runtime.WorkloadID
	removed []runtime.WorkloadID
}

func newRecordingReloadRuntime() *recordingReloadRuntime {
	return &recordingReloadRuntime{mockWorkloadRuntime: newMockWorkloadRuntime()}
}

func (r *recordingReloadRuntime) EnsureImage(_ context.Context, image string) error {
	r.ensured = append(r.ensured, image)
	return nil
}

func (r *recordingReloadRuntime) Start(ctx context.Context, cfg runtime.WorkloadConfig) (*runtime.WorkloadStatus, error) {
	r.started = append(r.started, cfg)
	return r.mockWorkloadRuntime.Start(ctx, cfg)
}

func (r *recordingReloadRuntime) Stop(_ context.Context, id runtime.WorkloadID) error {
	r.stopped = append(r.stopped, id)
	return nil
}

func (r *recordingReloadRuntime) Remove(_ context.Context, id runtime.WorkloadID) error {
	r.removed = append(r.removed, id)
	return nil
}

func sourceRefChange() (config.MCPServer, config.MCPServer) {
	oldServer := config.MCPServer{
		Name:   "source",
		Source: &config.Source{Type: "git", URL: "https://example.com/source.git", Ref: "commit-a"},
		Port:   3000,
	}
	newServer := oldServer
	newServer.Source = &config.Source{Type: "git", URL: "https://example.com/source.git", Ref: "commit-b"}
	return oldServer, newServer
}

func runSourceRefReload(t *testing.T) (*recordingReloadRuntime, *recordingReloadBuilder) {
	t.Helper()

	oldServer, newServer := sourceRefChange()
	rt := newRecordingReloadRuntime()
	rt.existsFn = func(context.Context, string) (bool, runtime.WorkloadID, error) {
		return true, "existing-source", nil
	}
	builder := &recordingReloadBuilder{}
	orch := runtime.NewOrchestrator(rt, builder)
	oldStack := &config.Stack{Name: "demo", Network: config.Network{Name: "demo-net"}, MCPServers: []config.MCPServer{oldServer}}
	newStack := &config.Stack{Name: "demo", Network: config.Network{Name: "demo-net"}, MCPServers: []config.MCPServer{newServer}}
	handler := NewHandler("stack.yaml", oldStack, mcp.NewGateway(), orch, 8180, 9000, nil, nil)
	handler.SetRegisterServerFunc(func(context.Context, config.MCPServer, []ReplicaRuntime, string) error { return nil })
	diff := ComputeDiff(oldStack, newStack)
	result := &ReloadResult{}

	if err := handler.applyMCPServerChanges(context.Background(), diff.MCPServers, newStack, result); err != nil {
		t.Fatalf("applyMCPServerChanges: %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("reload errors: %v", result.Errors)
	}
	return rt, builder
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

func TestHandler_ReloadBuildsChangedSourceBeforeReplacement(t *testing.T) {
	t.Skip("pending source lifecycle reconciliation during reload")

	rt, builder := runSourceRefReload(t)
	if len(builder.calls) != 1 {
		t.Fatalf("Build calls = %d, want 1 before replacement start", len(builder.calls))
	}
	if builder.calls[0].Ref != "commit-b" {
		t.Fatalf("Build ref = %q, want commit-b", builder.calls[0].Ref)
	}
	if len(rt.started) != 1 {
		t.Fatalf("Start calls = %d, want 1 replacement", len(rt.started))
	}
	if rt.started[0].Image != "gridctl-source:commit-b" {
		t.Fatalf("Start image = %q, want build result", rt.started[0].Image)
	}
	if rt.started[0].Image == "gridctl-demo-source:latest" {
		t.Fatalf("Start used mutable source image %q", rt.started[0].Image)
	}
	if len(rt.ensured) != 0 {
		t.Fatalf("EnsureImage calls = %v, want none for source build", rt.ensured)
	}
	if len(rt.stopped) != 1 || len(rt.removed) != 1 {
		t.Fatalf("replacement did not stop and remove existing container: stopped=%v removed=%v", rt.stopped, rt.removed)
	}
}

func TestHandler_CurrentReloadReusesMutableSourceImage(t *testing.T) {
	rt, builder := runSourceRefReload(t)

	if len(builder.calls) != 0 {
		t.Fatalf("Build calls = %d, want current stale count 0", len(builder.calls))
	}
	if len(rt.ensured) != 1 || rt.ensured[0] != "gridctl-demo-source:latest" {
		t.Fatalf("EnsureImage calls = %v, want current mutable source image", rt.ensured)
	}
	if len(rt.started) != 1 || rt.started[0].Image != "gridctl-demo-source:latest" {
		t.Fatalf("Start calls = %+v, want current mutable source image", rt.started)
	}
}
