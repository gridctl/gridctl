package runtime

import (
	"context"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
)

const pendingSourceLifecycle = "pending source lifecycle reconciliation"

type recordingSourceBuilder struct {
	calls []BuildOptions
}

func (b *recordingSourceBuilder) Build(_ context.Context, opts BuildOptions) (*BuildResult, error) {
	b.calls = append(b.calls, opts)
	ref := opts.Ref
	if ref == "" {
		ref = "resolved"
	}
	return &BuildResult{ImageTag: "gridctl-source:" + ref}, nil
}

type sourceLifecycleRuntime struct {
	WorkloadRuntime
	statuses map[string]*WorkloadStatus
	started  []WorkloadConfig
	stopped  []WorkloadID
	removed  []WorkloadID
}

func newSourceLifecycleRuntime() *sourceLifecycleRuntime {
	return &sourceLifecycleRuntime{statuses: make(map[string]*WorkloadStatus)}
}

func (r *sourceLifecycleRuntime) Ping(context.Context) error { return nil }

func (r *sourceLifecycleRuntime) EnsureNetwork(context.Context, string, NetworkOptions) error {
	return nil
}

func (r *sourceLifecycleRuntime) Exists(_ context.Context, name string) (bool, WorkloadID, error) {
	status, ok := r.statuses[name]
	if !ok {
		return false, "", nil
	}
	return true, status.ID, nil
}

func (r *sourceLifecycleRuntime) Status(_ context.Context, id WorkloadID) (*WorkloadStatus, error) {
	for _, status := range r.statuses {
		if status.ID == id {
			return status, nil
		}
	}
	return nil, ErrWorkloadNotFound
}

func (r *sourceLifecycleRuntime) GetHostPort(ctx context.Context, id WorkloadID, _ int) (int, error) {
	status, err := r.Status(ctx, id)
	if err != nil {
		return 0, err
	}
	return status.HostPort, nil
}

func (r *sourceLifecycleRuntime) Start(_ context.Context, cfg WorkloadConfig) (*WorkloadStatus, error) {
	r.started = append(r.started, cfg)
	name := "gridctl-" + cfg.Stack + "-" + cfg.Name
	status := &WorkloadStatus{
		ID:       WorkloadID("id-" + cfg.Name),
		Name:     cfg.Name,
		Stack:    cfg.Stack,
		Type:     cfg.Type,
		State:    WorkloadStateRunning,
		HostPort: cfg.HostPort,
		Image:    cfg.Image,
	}
	r.statuses[name] = status
	return status, nil
}

func (r *sourceLifecycleRuntime) Stop(_ context.Context, id WorkloadID) error {
	r.stopped = append(r.stopped, id)
	return nil
}

func (r *sourceLifecycleRuntime) Remove(_ context.Context, id WorkloadID) error {
	r.removed = append(r.removed, id)
	for name, status := range r.statuses {
		if status.ID == id {
			delete(r.statuses, name)
		}
	}
	return nil
}

func sourceLifecycleStack(ref string, replicas int, autoscale *config.AutoscaleConfig) *config.Stack {
	return &config.Stack{
		Name:    "demo",
		Network: config.Network{Name: "demo-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{{
			Name:      "source",
			Source:    &config.Source{Type: "git", URL: "https://example.com/source.git", Ref: ref},
			Port:      3000,
			Replicas:  replicas,
			Autoscale: autoscale,
		}},
	}
}

func TestOrchestrator_Up_SourceRefChangeReplacesStaleContainer(t *testing.T) {
	t.Skip(pendingSourceLifecycle)

	rt := newSourceLifecycleRuntime()
	builder := &recordingSourceBuilder{}
	orch := NewOrchestrator(rt, builder)

	stack := sourceLifecycleStack("commit-a", 0, nil)
	if _, err := orch.Up(context.Background(), stack, UpOptions{BasePort: 9000}); err != nil {
		t.Fatalf("first Up: %v", err)
	}
	stack.MCPServers[0].Source.Ref = "commit-b"
	if _, err := orch.Up(context.Background(), stack, UpOptions{BasePort: 9000}); err != nil {
		t.Fatalf("second Up: %v", err)
	}

	if len(builder.calls) != 2 {
		t.Fatalf("Build calls = %d, want 2", len(builder.calls))
	}
	if len(rt.started) != 2 {
		t.Fatalf("Start calls = %d, want 2", len(rt.started))
	}
	if rt.started[0].Image == rt.started[1].Image {
		t.Fatalf("ref change reused image %q", rt.started[0].Image)
	}
	if len(rt.removed) != 1 {
		t.Errorf("Remove calls = %d, want 1 stale container", len(rt.removed))
	}
}

func TestOrchestrator_Up_ResolvesSourceBeforeExistingContainerReuse(t *testing.T) {
	t.Skip(pendingSourceLifecycle)

	rt := newSourceLifecycleRuntime()
	rt.statuses["gridctl-demo-source"] = &WorkloadStatus{
		ID:       "existing-source",
		State:    WorkloadStateRunning,
		HostPort: 9000,
		Image:    "gridctl-source:commit-a",
	}
	builder := &recordingSourceBuilder{}
	orch := NewOrchestrator(rt, builder)

	if _, err := orch.Up(context.Background(), sourceLifecycleStack("commit-a", 0, nil), UpOptions{BasePort: 9000}); err != nil {
		t.Fatalf("Up: %v", err)
	}

	if len(builder.calls) != 1 {
		t.Fatalf("Build calls = %d, want 1 before reuse check", len(builder.calls))
	}
	if len(rt.started) != 0 {
		t.Errorf("matching existing container was replaced: %d Start calls", len(rt.started))
	}
}

func TestOrchestrator_Up_TwoSourceReplicasBuildOnce(t *testing.T) {
	t.Skip(pendingSourceLifecycle)

	rt := newSourceLifecycleRuntime()
	builder := &recordingSourceBuilder{}
	orch := NewOrchestrator(rt, builder)

	if _, err := orch.Up(context.Background(), sourceLifecycleStack("commit-a", 2, nil), UpOptions{BasePort: 9000}); err != nil {
		t.Fatalf("Up: %v", err)
	}

	if len(builder.calls) != 1 {
		t.Fatalf("Build calls = %d, want 1 per logical server", len(builder.calls))
	}
	if len(rt.started) != 2 {
		t.Fatalf("Start calls = %d, want 2 replicas", len(rt.started))
	}
	if rt.started[0].Image != rt.started[1].Image {
		t.Errorf("replica images differ: %q and %q", rt.started[0].Image, rt.started[1].Image)
	}
}

func TestOrchestrator_Up_AutoscaledSourceBuildsBeforeRegistration(t *testing.T) {
	t.Skip(pendingSourceLifecycle)

	rt := newSourceLifecycleRuntime()
	builder := &recordingSourceBuilder{}
	orch := NewOrchestrator(rt, builder)
	stack := sourceLifecycleStack("commit-a", 0, &config.AutoscaleConfig{
		Min:            0,
		Max:            2,
		TargetInFlight: 1,
	})

	result, err := orch.Up(context.Background(), stack, UpOptions{BasePort: 9000})
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	if len(builder.calls) != 1 {
		t.Fatalf("Build calls = %d, want 1 before autoscaler registration", len(builder.calls))
	}
	if len(rt.started) != 0 {
		t.Errorf("orchestrator started %d autoscaled replicas", len(rt.started))
	}
	if len(result.MCPServers) != 1 {
		t.Fatalf("result servers = %d, want 1", len(result.MCPServers))
	}
}
