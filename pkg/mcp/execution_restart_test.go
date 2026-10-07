package mcp

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/gridctl/gridctl/pkg/execution"
	"go.uber.org/mock/gomock"
)

func TestGateway_HealthMonitor_HardenedReplicaRestartsThroughAdmission(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	rec := &restartRecorder{}
	g.SetDockerClient(rec)
	var admitted atomic.Bool
	mock := setupMockAgentClient(ctrl, "svc", []Tool{{Name: "tool1"}})
	inner := &reconnectableClient{
		AgentClient:   mock,
		containerIDFn: func() string { return "cid-h" },
		pingFn:        func(context.Context) error { return fmt.Errorf("connection refused") },
		reconnectFn:   func(context.Context) error { return nil },
		inspectFn: func(context.Context) (*ContainerExit, error) {
			return &ContainerExit{Code: 3, Status: "exited"}, nil
		},
	}
	client := &executionClient{
		AgentClient: inner,
		config: MCPServerConfig{
			Name:        "svc",
			Transport:   TransportStdio,
			ContainerID: "cid-h",
			ExecutionBeforeStart: func(context.Context) error {
				if rec.restarted.Load() {
					t.Error("ExecutionBeforeStart ran after ContainerRestart")
				}
				admitted.Store(true)
				return nil
			},
		},
		check: func(context.Context) (*execution.Report, error) {
			return &execution.Report{Eligible: true, Outcome: "observed"}, nil
		},
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "svc", Transport: TransportStdio, ContainerID: "cid-h"})
	g.checkHealth(context.Background())

	if !admitted.Load() || rec.calls.Load() != 1 {
		t.Fatalf("admitted=%v calls=%d", admitted.Load(), rec.calls.Load())
	}
	if rec.timeout.Load() != 5 {
		t.Fatalf("stop timeout = %d, want 5", rec.timeout.Load())
	}
	got := g.ReplicaStatuses("svc")[0]
	if got.State != "healthy" || got.RestartAttempts != 0 || got.Exit != nil || got.LastError != "" || got.ContainerRestarts != 1 {
		t.Fatalf("recovered replica = %#v", got)
	}

	rec = &restartRecorder{}
	g.SetDockerClient(rec)
	client.config.ExecutionBeforeStart = func(context.Context) error {
		return fmt.Errorf("execution.runtime: no running instance")
	}
	inner.pingFn = func(context.Context) error { return fmt.Errorf("connection refused") }
	g.checkHealth(context.Background())
	if rec.calls.Load() != 0 {
		t.Fatal("failing admission restarted the container")
	}
	got = g.ReplicaStatuses("svc")[0]
	if got.State != "restarting" || got.RestartAttempts != 1 || got.NextRetryAt == nil || got.Exit == nil {
		t.Fatalf("admission failure = %#v", got)
	}
}

func TestGateway_HealthMonitor_HardenedHTTPWrapperNeverRestarts(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	rec := &restartRecorder{}
	g.SetDockerClient(rec)
	mock := setupMockAgentClient(ctrl, "svc", []Tool{{Name: "tool1"}})
	inner := &reconnectableClient{
		AgentClient: mock,
		pingFn:      func(context.Context) error { return fmt.Errorf("connection refused") },
		reconnectFn: func(context.Context) error { return nil },
		inspectFn: func(context.Context) (*ContainerExit, error) {
			return &ContainerExit{Code: 3, Status: "exited"}, nil
		},
	}
	client := &executionClient{
		AgentClient: inner,
		config:      MCPServerConfig{Name: "svc", Transport: TransportHTTP, ExecutionBeforeStart: func(context.Context) error { return nil }},
		check: func(context.Context) (*execution.Report, error) {
			return &execution.Report{Eligible: true}, nil
		},
	}
	if client.ContainerID() != "" {
		t.Fatalf("ContainerID = %q, want empty", client.ContainerID())
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "svc", Transport: TransportHTTP})
	g.checkHealth(context.Background())
	if rec.calls.Load() != 0 {
		t.Fatal("hardened HTTP wrapper restarted a container")
	}
}
