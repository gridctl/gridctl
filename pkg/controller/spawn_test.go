package controller

import (
	"context"
	"errors"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runtime"
)

// fakeAgentClient is a minimal AgentClient stub used only to give the Spawner
// a non-nil return value. Spawners do not call any methods on the returned
// client themselves, so the stub's methods are unlikely to execute.
type fakeAgentClient struct {
	name string
}

func (f *fakeAgentClient) Name() string                             { return f.name }
func (f *fakeAgentClient) Initialize(context.Context) error         { return nil }
func (f *fakeAgentClient) RefreshTools(context.Context) error       { return nil }
func (f *fakeAgentClient) Tools() []mcp.Tool                         { return nil }
func (f *fakeAgentClient) IsInitialized() bool                      { return true }
func (f *fakeAgentClient) ServerInfo() mcp.ServerInfo                { return mcp.ServerInfo{Name: f.name} }
func (f *fakeAgentClient) CallTool(context.Context, string, map[string]any) (*mcp.ToolCallResult, error) {
	return &mcp.ToolCallResult{}, nil
}

// fakeBuilder is a minimal ClientBuilder that returns a pre-made AgentClient
// and records every template it was called with, so tests can verify that
// the Spawner passed through the expected transport configuration.
type fakeBuilder struct {
	lastCfg   mcp.MCPServerConfig
	buildErr  error
	callCount int
}

func (f *fakeBuilder) BuildAgentClient(_ context.Context, cfg mcp.MCPServerConfig) (mcp.AgentClient, error) {
	f.callCount++
	f.lastCfg = cfg
	if f.buildErr != nil {
		return nil, f.buildErr
	}
	return &fakeAgentClient{name: cfg.Name}, nil
}

func newFakeBuilder(_ *testing.T) *fakeBuilder { return &fakeBuilder{} }

func TestProcessSpawner_Spawn_DelegatesToBuilder(t *testing.T) {
	b := newFakeBuilder(t)
	template := mcp.MCPServerConfig{
		Name: "proc", LocalProcess: true, Command: []string{"/bin/true"},
		Env: map[string]string{"K": "V"},
	}
	sp := NewProcessSpawner(b, template)

	c, err := sp.Spawn(context.Background())
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if c == nil {
		t.Fatal("Spawn returned nil client")
	}
	if b.callCount != 1 {
		t.Errorf("builder called %d times, want 1", b.callCount)
	}
	if b.lastCfg.Name != "proc" || !b.lastCfg.LocalProcess {
		t.Errorf("builder received wrong template: %+v", b.lastCfg)
	}
	if b.lastCfg.Env["K"] != "V" {
		t.Errorf("env not propagated: %v", b.lastCfg.Env)
	}
}

func TestProcessSpawner_Spawn_PropagatesBuildError(t *testing.T) {
	b := newFakeBuilder(t)
	b.buildErr = errors.New("kaboom")
	sp := NewProcessSpawner(b, mcp.MCPServerConfig{Name: "x", LocalProcess: true})
	if _, err := sp.Spawn(context.Background()); err == nil {
		t.Fatal("expected error from builder to surface")
	}
}

func TestProcessSpawner_Reap_NilReplicaIsNoop(t *testing.T) {
	sp := NewProcessSpawner(newFakeBuilder(t), mcp.MCPServerConfig{Name: "x"})
	if err := sp.Reap(context.Background(), nil); err != nil {
		t.Errorf("Reap(nil) = %v, want nil", err)
	}
}

func TestProcessSpawner_NilBuilder_RejectsSpawn(t *testing.T) {
	sp := NewProcessSpawner(nil, mcp.MCPServerConfig{Name: "x", LocalProcess: true})
	if _, err := sp.Spawn(context.Background()); err == nil {
		t.Fatal("Spawn with nil builder should error")
	}
}

func TestSSHSpawner_Spawn_PassesSSHTemplate(t *testing.T) {
	b := newFakeBuilder(t)
	template := mcp.MCPServerConfig{
		Name: "remote", SSH: true,
		SSHHost: "10.0.0.1", SSHUser: "mcp",
		Command: []string{"/opt/mcp-server"},
	}
	sp := NewSSHSpawner(b, template)

	if _, err := sp.Spawn(context.Background()); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if !b.lastCfg.SSH || b.lastCfg.SSHHost != "10.0.0.1" {
		t.Errorf("SSH template not forwarded: %+v", b.lastCfg)
	}
}

func TestSSHSpawner_Reap_NilReplicaIsNoop(t *testing.T) {
	sp := NewSSHSpawner(newFakeBuilder(t), mcp.MCPServerConfig{Name: "x"})
	if err := sp.Reap(context.Background(), nil); err != nil {
		t.Errorf("Reap(nil) = %v, want nil", err)
	}
}

func TestAtomicPortAllocator_Monotonic(t *testing.T) {
	p := NewAtomicPortAllocator(9000)
	got := []int{p.Allocate(), p.Allocate(), p.Allocate()}
	want := []int{9001, 9002, 9003}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("Allocate[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestAtomicPortAllocator_ConcurrentUniqueness(t *testing.T) {
	p := NewAtomicPortAllocator(10000)
	const workers, per = 16, 64
	out := make(chan int, workers*per)
	for i := 0; i < workers; i++ {
		go func() {
			for j := 0; j < per; j++ {
				out <- p.Allocate()
			}
		}()
	}
	seen := make(map[int]bool, workers*per)
	for i := 0; i < workers*per; i++ {
		v := <-out
		if seen[v] {
			t.Fatalf("duplicate port %d from concurrent Allocate", v)
		}
		seen[v] = true
	}
}

// closableFakeAgentClient lets tests exercise the Reap path where the client
// implements io.Closer (the non-nil, non-closer branch is covered by the
// stock fakeAgentClient).
type closableFakeAgentClient struct {
	fakeAgentClient
	closed   bool
	closeErr error
}

func (c *closableFakeAgentClient) Close() error {
	c.closed = true
	return c.closeErr
}

func TestProcessSpawner_Reap_ClosesCloserClient(t *testing.T) {
	sp := NewProcessSpawner(newFakeBuilder(t), mcp.MCPServerConfig{Name: "p"})
	c := &closableFakeAgentClient{fakeAgentClient: fakeAgentClient{name: "p"}}
	set := mcp.NewReplicaSet("p", mcp.ReplicaPolicyRoundRobin, []mcp.AgentClient{c})
	rep := set.Replicas()[0]

	if err := sp.Reap(context.Background(), rep); err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if !c.closed {
		t.Error("Close was not called on the replica client")
	}
}

func TestSSHSpawner_Reap_ClosesCloserClient(t *testing.T) {
	sp := NewSSHSpawner(newFakeBuilder(t), mcp.MCPServerConfig{Name: "s"})
	c := &closableFakeAgentClient{fakeAgentClient: fakeAgentClient{name: "s"}}
	set := mcp.NewReplicaSet("s", mcp.ReplicaPolicyRoundRobin, []mcp.AgentClient{c})
	rep := set.Replicas()[0]

	if err := sp.Reap(context.Background(), rep); err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if !c.closed {
		t.Error("Close was not called on the replica client")
	}
}

// stubContainerRuntime implements just enough of runtime.WorkloadRuntime to
// drive ContainerSpawner through its Spawn/Reap paths. Unused methods inherit
// from the embedded nil interface and will panic if called, which surfaces
// accidental reliance on real runtime behaviour.
type stubContainerRuntime struct {
	runtime.WorkloadRuntime
	startCalls  []runtime.WorkloadConfig
	startStatus runtime.WorkloadStatus
	startErr    error
	stopCalls   []runtime.WorkloadID
	stopErr     error
	removeCalls []runtime.WorkloadID
	removeErr   error
}

func (s *stubContainerRuntime) Start(_ context.Context, cfg runtime.WorkloadConfig) (*runtime.WorkloadStatus, error) {
	s.startCalls = append(s.startCalls, cfg)
	if s.startErr != nil {
		return nil, s.startErr
	}
	status := s.startStatus
	if status.Name == "" {
		status.Name = cfg.Name
	}
	return &status, nil
}

func (s *stubContainerRuntime) Stop(_ context.Context, id runtime.WorkloadID) error {
	s.stopCalls = append(s.stopCalls, id)
	return s.stopErr
}

func (s *stubContainerRuntime) Remove(_ context.Context, id runtime.WorkloadID) error {
	s.removeCalls = append(s.removeCalls, id)
	return s.removeErr
}

func TestContainerSpawner_Spawn_NilRuntimeErrors(t *testing.T) {
	sp := NewContainerSpawner(ContainerSpawnerOptions{
		Builder:   newFakeBuilder(t),
		Server:    config.MCPServer{Name: "x"},
		Transport: "http",
		Ports:     NewAtomicPortAllocator(9000),
	})
	if _, err := sp.Spawn(context.Background()); err == nil {
		t.Fatal("expected error when runtime is nil")
	}
}

func TestContainerSpawner_Spawn_HTTP_BuildsEndpointAndReaps(t *testing.T) {
	rt := &stubContainerRuntime{startStatus: runtime.WorkloadStatus{ID: "c-1", HostPort: 9555}}
	b := newFakeBuilder(t)
	sp := NewContainerSpawner(ContainerSpawnerOptions{
		Builder:   b,
		Runtime:   rt,
		Stack:     "demo",
		Server:    config.MCPServer{Name: "svc", Port: 3000, Transport: "http"},
		Network:   "demo-net",
		Image:     "demo/svc:latest",
		Transport: "http",
		Ports:     NewAtomicPortAllocator(9000),
	})

	c, err := sp.Spawn(context.Background())
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if c == nil {
		t.Fatal("Spawn returned nil client")
	}
	if len(rt.startCalls) != 1 {
		t.Fatalf("Start calls = %d, want 1", len(rt.startCalls))
	}
	got := rt.startCalls[0]
	if got.Stack != "demo" || got.NetworkName != "demo-net" || got.Image != "demo/svc:latest" {
		t.Errorf("Start cfg missing fields: %+v", got)
	}
	if got.Labels["gridctl.mcp-server"] != "svc" || got.Labels["gridctl.stack"] != "demo" {
		t.Errorf("labels wrong: %v", got.Labels)
	}
	if b.lastCfg.Transport != mcp.TransportHTTP {
		t.Errorf("builder transport = %q, want http", b.lastCfg.Transport)
	}
	if b.lastCfg.Endpoint == "" {
		t.Errorf("http replica missing endpoint: %+v", b.lastCfg)
	}

	set := mcp.NewReplicaSet("svc", mcp.ReplicaPolicyRoundRobin, []mcp.AgentClient{c})
	if err := sp.Reap(context.Background(), set.Replicas()[0]); err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if len(rt.stopCalls) != 1 || rt.stopCalls[0] != "c-1" {
		t.Errorf("stop calls = %v", rt.stopCalls)
	}
	if len(rt.removeCalls) != 1 || rt.removeCalls[0] != "c-1" {
		t.Errorf("remove calls = %v", rt.removeCalls)
	}
}

func TestContainerSpawner_Spawn_Stdio_UsesContainerID(t *testing.T) {
	rt := &stubContainerRuntime{startStatus: runtime.WorkloadStatus{ID: "stdio-1"}}
	b := newFakeBuilder(t)
	sp := NewContainerSpawner(ContainerSpawnerOptions{
		Builder:   b,
		Runtime:   rt,
		Server:    config.MCPServer{Name: "stdio-svc"},
		Transport: "stdio",
		Ports:     NewAtomicPortAllocator(9000),
	})

	if _, err := sp.Spawn(context.Background()); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if b.lastCfg.Transport != mcp.TransportStdio {
		t.Errorf("transport = %q, want stdio", b.lastCfg.Transport)
	}
	if b.lastCfg.ContainerID != "stdio-1" {
		t.Errorf("ContainerID = %q, want stdio-1", b.lastCfg.ContainerID)
	}
	if b.lastCfg.Endpoint != "" {
		t.Errorf("stdio replica should not set Endpoint, got %q", b.lastCfg.Endpoint)
	}
}

func TestContainerSpawner_Spawn_StartError_NoWorkloadTracked(t *testing.T) {
	rt := &stubContainerRuntime{startErr: errors.New("daemon down")}
	sp := NewContainerSpawner(ContainerSpawnerOptions{
		Builder:   newFakeBuilder(t),
		Runtime:   rt,
		Server:    config.MCPServer{Name: "svc"},
		Transport: "http",
		Ports:     NewAtomicPortAllocator(9000),
	})
	if _, err := sp.Spawn(context.Background()); err == nil {
		t.Fatal("expected Spawn to surface runtime.Start error")
	}
}

func TestContainerSpawner_Spawn_BuildError_CleansUpContainer(t *testing.T) {
	rt := &stubContainerRuntime{startStatus: runtime.WorkloadStatus{ID: "c-2", HostPort: 9700}}
	b := newFakeBuilder(t)
	b.buildErr = errors.New("client build failed")
	sp := NewContainerSpawner(ContainerSpawnerOptions{
		Builder:   b,
		Runtime:   rt,
		Server:    config.MCPServer{Name: "svc"},
		Transport: "http",
		Ports:     NewAtomicPortAllocator(9000),
	})

	if _, err := sp.Spawn(context.Background()); err == nil {
		t.Fatal("expected Spawn to surface build error")
	}
	if len(rt.stopCalls) != 1 || rt.stopCalls[0] != "c-2" {
		t.Errorf("cleanup stop calls = %v", rt.stopCalls)
	}
	if len(rt.removeCalls) != 1 || rt.removeCalls[0] != "c-2" {
		t.Errorf("cleanup remove calls = %v", rt.removeCalls)
	}
}

func TestContainerSpawner_Reap_NilReplicaIsNoop(t *testing.T) {
	sp := NewContainerSpawner(ContainerSpawnerOptions{
		Builder:   newFakeBuilder(t),
		Runtime:   &stubContainerRuntime{},
		Server:    config.MCPServer{Name: "svc"},
		Transport: "http",
		Ports:     NewAtomicPortAllocator(9000),
	})
	if err := sp.Reap(context.Background(), nil); err != nil {
		t.Errorf("Reap(nil) = %v, want nil", err)
	}
}

func TestContainerSpawner_Reap_UntrackedClientIsNoop(t *testing.T) {
	rt := &stubContainerRuntime{}
	sp := NewContainerSpawner(ContainerSpawnerOptions{
		Builder:   newFakeBuilder(t),
		Runtime:   rt,
		Server:    config.MCPServer{Name: "svc"},
		Transport: "http",
		Ports:     NewAtomicPortAllocator(9000),
	})
	set := mcp.NewReplicaSet("svc", mcp.ReplicaPolicyRoundRobin, []mcp.AgentClient{&fakeAgentClient{name: "svc"}})
	if err := sp.Reap(context.Background(), set.Replicas()[0]); err != nil {
		t.Errorf("Reap untracked = %v, want nil", err)
	}
	if len(rt.stopCalls) != 0 || len(rt.removeCalls) != 0 {
		t.Errorf("untracked replica should not call runtime: stop=%v remove=%v", rt.stopCalls, rt.removeCalls)
	}
}

func TestToAutoscalePolicy_MapsFieldsAndDefaults(t *testing.T) {
	a := &config.AutoscaleConfig{
		Min: 1, Max: 5, TargetInFlight: 2,
		ScaleUpAfter: "45s", ScaleDownAfter: "3m",
		WarmPool: 1, IdleToZero: true,
	}
	p := toAutoscalePolicy(a)
	if p.Min != 1 || p.Max != 5 || p.TargetInFlight != 2 || p.WarmPool != 1 || !p.IdleToZero {
		t.Errorf("field copy mismatch: %+v", p)
	}
	if p.ScaleUpAfter.String() != "45s" || p.ScaleDownAfter.String() != "3m0s" {
		t.Errorf("resolved durations wrong: up=%v down=%v", p.ScaleUpAfter, p.ScaleDownAfter)
	}

	// Empty durations should fall back to the config defaults (30s / 5m).
	bare := toAutoscalePolicy(&config.AutoscaleConfig{Min: 1, Max: 2, TargetInFlight: 1})
	if bare.ScaleUpAfter.String() != "30s" || bare.ScaleDownAfter.String() != "5m0s" {
		t.Errorf("default durations wrong: up=%v down=%v", bare.ScaleUpAfter, bare.ScaleDownAfter)
	}
}

func TestStackNameOrEmpty(t *testing.T) {
	if got := stackNameOrEmpty(nil); got != "" {
		t.Errorf("nil stack: got %q, want empty string", got)
	}
	if got := stackNameOrEmpty(&config.Stack{Name: "prod"}); got != "prod" {
		t.Errorf("named stack: got %q, want prod", got)
	}
}
