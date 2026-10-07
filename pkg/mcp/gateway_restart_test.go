package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/gridctl/gridctl/pkg/execution"
	"github.com/gridctl/gridctl/pkg/logging"
	"go.uber.org/mock/gomock"
)

func installExitedStdio(t *testing.T, name, policy, containerID string, exit *ContainerExit, inspectErr, restartErr error) (*Gateway, *reconnectableClient, *restartRecorder, *atomic.Int32) {
	t.Helper()
	ctrl := gomock.NewController(t)
	g := NewGateway()
	var reconnects atomic.Int32
	mock := setupMockAgentClient(ctrl, name, []Tool{{Name: "tool1"}})
	client := &reconnectableClient{
		AgentClient:   mock,
		containerIDFn: func() string { return containerID },
		pingFn:        func(context.Context) error { return fmt.Errorf("connection refused") },
		reconnectFn: func(context.Context) error {
			reconnects.Add(1)
			return nil
		},
		inspectFn: func(context.Context) (*ContainerExit, error) {
			return exit, inspectErr
		},
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: name, Transport: TransportStdio, RestartPolicy: policy, ContainerID: containerID})
	rec := &restartRecorder{err: restartErr}
	g.SetDockerClient(rec)
	return g, client, rec, &reconnects
}

func TestGateway_HealthMonitor_RestartsExitedContainer(t *testing.T) {
	g, client, rec, reconnects := installExitedStdio(t, "svc", "", "cid-1", &ContainerExit{Code: 3, Status: "exited"}, nil, nil)
	client.reconnectFn = func(context.Context) error {
		if !rec.restarted.Load() {
			t.Error("Reconnect ran before ContainerRestart")
		}
		reconnects.Add(1)
		return nil
	}
	logBuffer := logging.NewLogBuffer(20)
	g.SetLogger(slog.New(logging.NewBufferHandler(logBuffer, nil)))

	g.checkHealth(context.Background())

	if rec.calls.Load() != 1 || !rec.restarted.Load() {
		t.Fatalf("ContainerRestart calls = %d", rec.calls.Load())
	}
	if got, _ := rec.id.Load().(string); got != "cid-1" {
		t.Fatalf("container ID = %q", got)
	}
	if rec.timeout.Load() != stdioContainerStopTimeout {
		t.Fatalf("stop timeout = %d, want %d", rec.timeout.Load(), stdioContainerStopTimeout)
	}
	if !rec.sawDeadline.Load() {
		t.Fatal("monitor restart must bound the runtime call")
	}
	if reconnects.Load() != 1 {
		t.Fatalf("Reconnect calls = %d", reconnects.Load())
	}
	replicas := g.ReplicaStatuses("svc")
	if len(replicas) != 1 {
		t.Fatalf("replicas = %#v", replicas)
	}
	got := replicas[0]
	if got.State != "healthy" || !got.Healthy || got.RestartAttempts != 0 || got.Exit != nil || got.LastError != "" || got.ContainerRestarts != 1 {
		t.Fatalf("replica = %#v", got)
	}
	if got.RestartPolicy != "always" {
		t.Fatalf("restartPolicy = %q", got.RestartPolicy)
	}
	found := false
	for _, entry := range logBuffer.GetRecent(20) {
		if entry.Message == "restarted exited container" && entry.Attrs["exit_code"] == int64(3) && entry.Attrs["container_restarts"] == int64(1) {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing restart log: %#v", logBuffer.GetRecent(20))
	}
}

func TestGateway_HealthMonitor_ContainerRestartFailureAdvancesBackoff(t *testing.T) {
	g, client, rec, reconnects := installExitedStdio(t, "svc", "always", "cid-1", &ContainerExit{Code: 3, Status: "exited"}, nil, fmt.Errorf("daemon busy"))
	client.reconnectFn = func(context.Context) error {
		reconnects.Add(1)
		return nil
	}
	g.checkHealth(context.Background())

	if reconnects.Load() != 0 {
		t.Fatal("Reconnect ran after a failed container restart")
	}
	replicas := g.ReplicaStatuses("svc")
	if len(replicas) != 1 {
		t.Fatalf("replicas = %#v", replicas)
	}
	got := replicas[0]
	if got.State != "restarting" || got.RestartAttempts != 1 || got.NextRetryAt == nil || got.Exit == nil || got.Exit.Code != 3 || got.ContainerRestarts != 1 {
		t.Fatalf("replica = %#v", got)
	}
	if rec.calls.Load() != 1 {
		t.Fatalf("ContainerRestart calls = %d", rec.calls.Load())
	}
}

func TestGateway_HealthMonitor_RestartPolicyNo(t *testing.T) {
	g, client, rec, reconnects := installExitedStdio(t, "svc", "no", "cid-1", &ContainerExit{Code: 3, Status: "exited"}, nil, nil)
	set := g.Router().GetReplicaSet("svc")
	set.Replicas()[0].AddContainerRestart()
	set.Replicas()[0].AddContainerRestart()
	g.checkHealth(context.Background())

	if rec.calls.Load() != 0 || reconnects.Load() != 0 {
		t.Fatalf("restart calls = %d, reconnects = %d", rec.calls.Load(), reconnects.Load())
	}
	replicas := g.ReplicaStatuses("svc")
	got := replicas[0]
	if got.State != "unhealthy" || !got.RestartExhausted || got.ContainerRestarts != 2 {
		t.Fatalf("replica = %#v", got)
	}
	if got.LastError == "" || got.LastError != restartPolicyNoReason("svc") {
		t.Fatalf("lastError = %q", got.LastError)
	}

	client.pingFn = func(context.Context) error { return nil }
	g.checkHealth(context.Background())
	replicas = g.ReplicaStatuses("svc")
	got = replicas[0]
	if got.RestartExhausted || got.ContainerRestarts != 2 || !got.Healthy {
		t.Fatalf("successful ping = %#v", got)
	}
}

func TestGateway_HealthMonitor_RestartPolicyOverflowIsNotUnbounded(t *testing.T) {
	g, _, rec, reconnects := installExitedStdio(t, "svc", "on-failure:4294967296", "cid-1", &ContainerExit{Code: 3, Status: "exited"}, nil, nil)
	g.checkHealth(context.Background())
	if rec.calls.Load() != 0 || reconnects.Load() != 0 {
		t.Fatalf("overflow policy restarted: calls=%d reconnects=%d", rec.calls.Load(), reconnects.Load())
	}
	got := g.ReplicaStatuses("svc")[0]
	if got.State != "unhealthy" || !got.RestartExhausted {
		t.Fatalf("replica = %#v", got)
	}
}

func TestGateway_HealthMonitor_RestartPolicyOnFailureBudget(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	rec := &restartRecorder{}
	g.SetDockerClient(rec)

	var up atomic.Bool
	mock := setupMockAgentClient(ctrl, "svc", []Tool{{Name: "tool1"}})
	inner := &reconnectableClient{
		AgentClient:   mock,
		containerIDFn: func() string { return "cid-1" },
		pingFn: func(context.Context) error {
			if up.Load() {
				return nil
			}
			return fmt.Errorf("connection refused")
		},
		reconnectFn: func(context.Context) error {
			up.Store(true)
			return nil
		},
		inspectFn: func(context.Context) (*ContainerExit, error) {
			return &ContainerExit{Code: 3, Status: "exited"}, nil
		},
	}
	client := &executionClient{
		AgentClient: inner,
		config: MCPServerConfig{
			Name:        "svc",
			Transport:   TransportStdio,
			ContainerID: "cid-1",
			Execution:   &execution.ExecutionConfig{},
			ExecutionBeforeStart: func(context.Context) error {
				return nil
			},
		},
		check: func(context.Context) (*execution.Report, error) {
			return &execution.Report{Eligible: true, Outcome: "observed"}, nil
		},
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{
		Name:          "svc",
		Transport:     TransportStdio,
		ContainerID:   "cid-1",
		RestartPolicy: "on-failure:2",
		Execution:     &execution.ExecutionConfig{},
	})

	recoverOutage := func() {
		t.Helper()
		up.Store(false)
		before := rec.calls.Load()
		g.checkHealth(context.Background())
		if rec.calls.Load() != before+1 {
			t.Fatalf("restart calls = %d, want %d", rec.calls.Load(), before+1)
		}
		got := g.ReplicaStatuses("svc")[0]
		if got.State != "healthy" || got.RestartAttempts != 0 || got.Exit != nil || got.LastError != "" {
			t.Fatalf("recovered replica = %#v", got)
		}
	}
	recoverOutage()
	recoverOutage()
	if g.ReplicaStatuses("svc")[0].ContainerRestarts != 2 {
		t.Fatalf("containerRestarts = %d", g.ReplicaStatuses("svc")[0].ContainerRestarts)
	}

	up.Store(false)
	before := rec.calls.Load()
	g.checkHealth(context.Background())
	got := g.ReplicaStatuses("svc")[0]
	if rec.calls.Load() != before {
		t.Fatal("budget refusal restarted the container")
	}
	if got.State != "unhealthy" || !got.RestartExhausted || got.ContainerRestarts != 2 {
		t.Fatalf("exhausted replica = %#v", got)
	}
	if got.LastError != restartBudgetReason("svc", "on-failure:2", 2) {
		t.Fatalf("lastError = %q", got.LastError)
	}

	if err := g.RestartMCPServer(context.Background(), "svc"); err != nil {
		t.Fatalf("RestartMCPServer: %v", err)
	}
	got = g.ReplicaStatuses("svc")[0]
	if got.RestartExhausted || got.ContainerRestarts != 0 {
		t.Fatalf("manual restart did not clear budget: %#v", got)
	}
}

func TestGateway_HealthMonitor_RestartPolicyOnFailureSkipsCleanExit(t *testing.T) {
	g, _, rec, reconnects := installExitedStdio(t, "svc", "on-failure", "cid-1", &ContainerExit{Code: 0, Status: "exited"}, nil, nil)
	g.checkHealth(context.Background())
	if rec.calls.Load() != 0 || reconnects.Load() != 0 {
		t.Fatalf("clean exit restarted: calls=%d reconnects=%d", rec.calls.Load(), reconnects.Load())
	}
	got := g.ReplicaStatuses("svc")[0]
	if got.State != "unhealthy" || !got.RestartExhausted || got.LastError != cleanExitRestartReason {
		t.Fatalf("replica = %#v", got)
	}

	g, _, rec, reconnects = installExitedStdio(t, "always", "always", "cid-2", &ContainerExit{Code: 0, Status: "exited"}, nil, nil)
	g.checkHealth(context.Background())
	if rec.calls.Load() != 1 || reconnects.Load() != 1 {
		t.Fatalf("always did not restart a clean exit: calls=%d reconnects=%d", rec.calls.Load(), reconnects.Load())
	}
}

func TestGateway_HealthMonitor_RestartPolicyOnFailureRestartsOOM(t *testing.T) {
	g, _, rec, _ := installExitedStdio(t, "svc", "on-failure", "cid-1", &ContainerExit{Code: 0, OOMKilled: true, Status: "exited"}, nil, nil)
	g.checkHealth(context.Background())
	if rec.calls.Load() != 1 {
		t.Fatal("OOM exit was not restarted")
	}
	if g.ReplicaStatuses("svc")[0].RestartExhausted {
		t.Fatal("OOM exit was treated as terminal")
	}
}

func TestGateway_HealthMonitor_RestartSkipsAutoscaled(t *testing.T) {
	g, _, rec, reconnects := installExitedStdio(t, "svc", "always", "cid-1", &ContainerExit{Code: 3, Status: "exited"}, nil, nil)
	g.autoMu.Lock()
	g.autoscalers["svc"] = &Autoscaler{}
	g.autoMu.Unlock()
	g.checkHealth(context.Background())
	if rec.calls.Load() != 0 {
		t.Fatal("autoscaled server restarted its container")
	}
	if reconnects.Load() != 1 {
		t.Fatalf("autoscaled reconnects = %d, want the existing reconnect path", reconnects.Load())
	}
	if got := g.ReplicaStatuses("svc"); len(got) != 1 || got[0].RestartPolicy != "" {
		t.Fatalf("autoscaled restartPolicy = %#v, want omitted", got)
	}
}

func TestGateway_HealthMonitor_RemovedContainerIsTerminal(t *testing.T) {
	g, _, rec, reconnects := installExitedStdio(t, "svc", "always", "cid-1", nil, fmt.Errorf("inspecting container: %w", errdefs.ErrNotFound), nil)
	g.checkHealth(context.Background())
	g.checkHealth(context.Background())
	if rec.calls.Load() != 0 || reconnects.Load() != 0 {
		t.Fatalf("removed container retried: calls=%d reconnects=%d", rec.calls.Load(), reconnects.Load())
	}
	got := g.ReplicaStatuses("svc")[0]
	if got.State != "unhealthy" || !got.RestartExhausted || got.RestartAttempts != 0 || got.NextRetryAt != nil {
		t.Fatalf("replica = %#v", got)
	}
	if got.LastError != removedContainerReason {
		t.Fatalf("lastError = %q", got.LastError)
	}
}

func TestGateway_HealthMonitor_RestartNotFoundIsTerminal(t *testing.T) {
	// This covers the race between inspect and restart, not removal before the tick.
	g, _, rec, reconnects := installExitedStdio(t, "svc", "always", "cid-1", &ContainerExit{Code: 3, Status: "exited"}, nil, errdefs.ErrNotFound)
	g.checkHealth(context.Background())
	if rec.calls.Load() != 1 || reconnects.Load() != 0 {
		t.Fatalf("calls=%d reconnects=%d", rec.calls.Load(), reconnects.Load())
	}
	got := g.ReplicaStatuses("svc")[0]
	if got.State != "unhealthy" || !got.RestartExhausted || got.RestartAttempts != 0 || got.LastError != removedContainerReason {
		t.Fatalf("replica = %#v", got)
	}
}

func TestGateway_HealthMonitor_NoDockerClientKeepsReconnect(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	var reconnects atomic.Int32
	mock := setupMockAgentClient(ctrl, "svc", []Tool{{Name: "tool1"}})
	client := &reconnectableClient{
		AgentClient:   mock,
		containerIDFn: func() string { return "cid-1" },
		pingFn:        func(context.Context) error { return fmt.Errorf("connection refused") },
		reconnectFn: func(context.Context) error {
			reconnects.Add(1)
			return fmt.Errorf("container not running")
		},
		inspectFn: func(context.Context) (*ContainerExit, error) {
			return &ContainerExit{Code: 3, Status: "exited"}, nil
		},
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "svc", Transport: TransportStdio})
	g.checkHealth(context.Background())
	if reconnects.Load() != 1 {
		t.Fatalf("reconnects = %d", reconnects.Load())
	}
	if g.ReplicaStatuses("svc")[0].Exit == nil || g.ReplicaStatuses("svc")[0].Exit.Code != 3 {
		t.Fatalf("exit evidence lost: %#v", g.ReplicaStatuses("svc"))
	}
}
