package reload

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runtime"
)

type recordingReloadBuilder struct {
	calls []runtime.BuildOptions
	err   error
}

func (b *recordingReloadBuilder) Build(_ context.Context, opts runtime.BuildOptions) (*runtime.BuildResult, error) {
	b.calls = append(b.calls, opts)
	if b.err != nil {
		return nil, b.err
	}
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
	newServer.Volumes = []string{"/host/data:/data:ro"}
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
	if len(rt.started[0].Volumes) != 1 || rt.started[0].Volumes[0] != "/host/data:/data:ro" {
		t.Fatalf("Start volumes = %v, want source server mount", rt.started[0].Volumes)
	}
	if len(rt.ensured) != 0 {
		t.Fatalf("EnsureImage calls = %v, want none for source build", rt.ensured)
	}
	if len(rt.stopped) != 1 || len(rt.removed) != 1 {
		t.Fatalf("replacement did not stop and remove existing container: stopped=%v removed=%v", rt.stopped, rt.removed)
	}
}

func TestHandler_ReloadRetriesChangedSourceAfterPrepareFailure(t *testing.T) {
	oldYAML := `
name: demo
network:
  name: demo-net
mcp-servers:
  - name: source
    source:
      type: git
      url: https://example.com/source.git
      ref: commit-a
    port: 3000
`
	stackPath := writeStackFile(t, oldYAML)
	oldStack, err := config.LoadStack(stackPath)
	if err != nil {
		t.Fatalf("LoadStack(old): %v", err)
	}
	newYAML := `
name: demo
network:
  name: demo-net
mcp-servers:
  - name: source
    source:
      type: git
      url: https://example.com/source.git
      ref: commit-b
    port: 3000
`
	if err := os.WriteFile(stackPath, []byte(newYAML), 0644); err != nil {
		t.Fatalf("write changed stack: %v", err)
	}

	rt := newRecordingReloadRuntime()
	rt.existsFn = func(context.Context, string) (bool, runtime.WorkloadID, error) {
		return true, "existing-source", nil
	}
	builder := &recordingReloadBuilder{err: errors.New("build failed")}
	gateway := mcp.NewGateway()
	handler := NewHandler(stackPath, oldStack, gateway, runtime.NewOrchestrator(rt, builder), 8180, 9000, nil, nil)
	handler.SetRegisterServerFunc(func(context.Context, config.MCPServer, []ReplicaRuntime, string) error { return nil })

	result, err := handler.Reload(context.Background())
	if err != nil {
		t.Fatalf("first Reload: %v", err)
	}
	if result.Success {
		t.Fatal("first Reload succeeded after build failure")
	}
	if got := handler.CurrentConfig().MCPServers[0].Source.Ref; got != "commit-a" {
		t.Fatalf("current source ref = %q, want old ref commit-a", got)
	}
	if len(rt.stopped) != 0 || len(rt.removed) != 0 || len(rt.started) != 0 {
		t.Fatalf("old workload changed after prepare failure: stopped=%v removed=%v started=%v", rt.stopped, rt.removed, rt.started)
	}
	if statuses := gateway.Status(); len(statuses) != 0 {
		t.Fatalf("prepare failure polluted gateway status: %+v", statuses)
	}

	builder.err = nil
	result, err = handler.Reload(context.Background())
	if err != nil {
		t.Fatalf("second Reload: %v", err)
	}
	if !result.Success {
		t.Fatalf("second Reload failed: %s (%v)", result.Message, result.Errors)
	}
	if len(builder.calls) != 2 {
		t.Fatalf("Build calls = %d, want retry on unchanged desired stack", len(builder.calls))
	}
	if got := handler.CurrentConfig().MCPServers[0].Source.Ref; got != "commit-b" {
		t.Fatalf("current source ref = %q, want applied ref commit-b", got)
	}
	if len(rt.stopped) != 1 || len(rt.removed) != 1 || len(rt.started) != 1 {
		t.Fatalf("successful retry did not replace workload: stopped=%v removed=%v started=%v", rt.stopped, rt.removed, rt.started)
	}
}
