package controller

import (
	"context"
	"errors"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
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
