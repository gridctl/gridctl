package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/gridctl/gridctl/pkg/execution"
)

// ExecutionReport describes local hygiene without claiming native confinement.
func (c *ProcessClient) ExecutionReport() *execution.Report {
	c.procMu.Lock()
	defer c.procMu.Unlock()
	mode := "unsandboxed-local"
	if c.remote {
		mode = "external-ssh; remote cleanup unverified"
	}
	report := &execution.Report{Mode: mode, Outcome: "configured", Runtime: "process", DaemonRootless: "not-applicable", UserNamespace: "not-applicable", ObservedAt: time.Now().UTC()}
	if c.cmd != nil && c.cmd.Process != nil && !c.exited {
		report.Instance = strconv.Itoa(c.cmd.Process.Pid)
	}
	lookup := "ambient_path"
	if c.execution != nil {
		report.Revision, lookup = c.execution.Revision, c.execution.Lookup
	}
	names := make([]string, 0, len(c.env))
	for _, entry := range c.env {
		name, _, _ := strings.Cut(entry, "=")
		names = append(names, name)
	}
	report.Controls = []execution.Control{
		{Field: "environment_names", Requested: strings.Join(names, ", "), Outcome: "configured", Source: "filtered launch environment; names only"},
		{Field: "lookup", Requested: lookup, Outcome: "configured", Source: "launch configuration; executable identity unverified"},
	}
	state := "not-started"
	if c.started {
		state = "running"
	} else if c.cmd != nil && c.cmd.Process != nil && !c.exited {
		state = "stopping"
	}
	if c.exited {
		state = "exited"
	}
	report.Controls = append(report.Controls, execution.Control{Field: "process_state", Requested: "direct child lifecycle", Observed: state, Outcome: "observed", Source: "process waiter"})
	if c.exited && c.cmd != nil && c.cmd.ProcessState != nil {
		report.Controls = append(report.Controls, execution.Control{Field: "exit_code", Requested: "direct child exit", Observed: strconv.Itoa(c.cmd.ProcessState.ExitCode()), Outcome: "observed", Source: "process waiter"})
	}
	return report
}

type executionClient struct {
	AgentClient
	config        MCPServerConfig
	check         func(context.Context) (*execution.Report, error)
	mu            sync.Mutex
	report        *execution.Report
	closed        bool
	operationGate chan struct{}
}

type executionPhaseError struct {
	phase string
	cause error
}

func (e *executionPhaseError) Error() string {
	return "execution." + e.phase + ": downstream failure under the selected contract; check declared writable state and bootstrap cache/network requirements"
}
func (e *executionPhaseError) Unwrap() error { return e.cause }

func (g *Gateway) restartExecutionReplicas(ctx context.Context, name string, set *ReplicaSet) error {
	replicas := set.Replicas()
	if len(replicas) == 0 {
		if scaler := g.GetAutoscaler(name); scaler != nil {
			return scaler.TriggerColdStart(ctx)
		}
		return fmt.Errorf("execution: no active replica to restart")
	}
	for _, replica := range replicas {
		replica.SetHealthy(false)
	}
	for _, replica := range replicas {
		client, ok := replica.Client().(*executionClient)
		if !ok || client.config.ExecutionBeforeStart == nil || client.config.ContainerID == "" || g.dockerCli == nil {
			return fmt.Errorf("execution: replica recovery admission unavailable")
		}
		if err := g.restartExecutionReplica(ctx, client); err != nil {
			return err
		}
		if err := g.finishReplicaRecovery(ctx, name, set, replica); err != nil {
			return err
		}
	}
	g.router.RefreshTools()
	return nil
}

func (g *Gateway) finishReplicaRecovery(ctx context.Context, name string, set *ReplicaSet, replica *Replica) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	g.pendingMu.Lock()
	defer g.pendingMu.Unlock()
	if g.router.GetReplicaSet(name) != set {
		return fmt.Errorf("execution: replica revision retired during recovery")
	}
	if g.pinningEnabledForServer(name) {
		drifts, err := g.schemaVerifier.VerifyOrPin(name, replica.Client().Tools())
		if err != nil {
			return fmt.Errorf("execution: schema verification after recovery failed")
		}
		g.handlePinDrift(name, drifts)
	}
	replica.MarkStarted(time.Now())
	replica.SetHealthy(true)
	return nil
}

func (g *Gateway) restartProcessReplicas(ctx context.Context, name string, set *ReplicaSet) error {
	replicas := set.Replicas()
	if len(replicas) == 0 {
		if scaler := g.GetAutoscaler(name); scaler != nil {
			return scaler.TriggerColdStart(ctx)
		}
		return fmt.Errorf("execution: no active process replica")
	}
	for _, replica := range replicas {
		replica.SetHealthy(false)
	}
	for _, replica := range replicas {
		client, ok := replica.Client().(*ProcessClient)
		if !ok {
			return fmt.Errorf("execution: process recovery unavailable")
		}
		if err := client.Reconnect(ctx); err != nil {
			return err
		}
		if err := g.finishReplicaRecovery(ctx, name, set, replica); err != nil {
			return err
		}
	}
	g.router.RefreshTools()
	return nil
}

func (g *Gateway) restartExecutionReplica(ctx context.Context, client *executionClient) error {
	release, err := client.operation(ctx)
	if err != nil {
		return err
	}
	defer release()
	client.mu.Lock()
	closed := client.closed
	client.report = nil
	client.mu.Unlock()
	if closed {
		return fmt.Errorf("execution: instance retired")
	}
	if err := client.config.ExecutionBeforeStart(ctx); err != nil {
		return err
	}
	if closer, ok := client.AgentClient.(io.Closer); ok {
		if err := closer.Close(); err != nil {
			return fmt.Errorf("execution: previous transport cleanup failed")
		}
	}
	timeout := 5
	if err := g.dockerCli.ContainerRestart(ctx, client.config.ContainerID, container.StopOptions{Timeout: &timeout}); err != nil {
		return fmt.Errorf("execution: replica engine restart failed")
	}
	return client.reconnectLocked(ctx)
}

func (c *executionClient) operation(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.operationGate == nil {
		c.operationGate = make(chan struct{}, 1)
		c.operationGate <- struct{}{}
	}
	gate := c.operationGate
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-gate:
		if err := ctx.Err(); err != nil {
			gate <- struct{}{}
			return nil, err
		}
		return func() { gate <- struct{}{} }, nil
	}
}

func (c *executionClient) ProtocolVersion() string { return protocolVersionOf(c.AgentClient) }

func (c *executionClient) Initialize(ctx context.Context) error {
	if err := c.admit(ctx); err != nil {
		return err
	}
	return c.AgentClient.Initialize(ctx)
}

func (c *executionClient) RefreshTools(ctx context.Context) error {
	if err := c.admit(ctx); err != nil {
		return err
	}
	return c.AgentClient.RefreshTools(ctx)
}

func (c *executionClient) admit(ctx context.Context) error {
	release, err := c.operation(ctx)
	if err != nil {
		return err
	}
	defer release()
	return c.admitLocked(ctx)
}

func (c *executionClient) admitLocked(ctx context.Context) error {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return fmt.Errorf("execution: instance retired")
	}
	report, err := c.check(ctx)
	if err != nil && ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		return err
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("execution: instance retired")
	}
	if c.report == nil || report == nil || !report.ObservedAt.Before(c.report.ObservedAt) {
		c.report = report
	}
	latest := c.report
	c.mu.Unlock()
	if err != nil {
		return err
	}
	if report == nil || !report.Eligible || latest == nil || !latest.Eligible {
		return fmt.Errorf("execution: required enforcement evidence unavailable")
	}
	return nil
}

func (c *executionClient) ExecutionReport() *execution.Report {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.report == nil {
		return nil
	}
	copy := *c.report
	copy.Controls = append([]execution.Control(nil), copy.Controls...)
	return &copy
}

func (c *executionClient) ExecutionEligible() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closed && c.report != nil && c.report.Eligible
}

func (c *executionClient) CallTool(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
	if err := c.admit(ctx); err != nil {
		return nil, err
	}
	return c.AgentClient.CallTool(ctx, name, arguments)
}

func (c *executionClient) Ping(ctx context.Context) error {
	if err := c.admit(ctx); err != nil {
		return err
	}
	if p, ok := c.AgentClient.(Pingable); ok {
		if err := p.Ping(ctx); err != nil {
			return &executionPhaseError{phase: "ping", cause: err}
		}
	}
	return nil
}

func (c *executionClient) Reconnect(ctx context.Context) error {
	release, err := c.operation(ctx)
	if err != nil {
		return err
	}
	defer release()
	return c.reconnectLocked(ctx)
}

func (c *executionClient) reconnectLocked(ctx context.Context) error {
	if err := c.admitLocked(ctx); err != nil {
		return err
	}
	if r, ok := c.AgentClient.(Reconnectable); ok {
		if err := r.Reconnect(ctx); err != nil {
			return &executionPhaseError{phase: "reconnect", cause: err}
		}
		if err := c.admitLocked(ctx); err != nil {
			closeAgentClient(c.AgentClient)
			return err
		}
		return nil
	}
	return fmt.Errorf("execution: transport cannot reconnect")
}

func (c *executionClient) Close() error {
	c.mu.Lock()
	c.closed = true
	c.report = nil
	c.mu.Unlock()
	if closer, ok := c.AgentClient.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (c *executionClient) RelayRaw(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	if err := c.admit(ctx); err != nil {
		return nil, err
	}
	if r, ok := c.AgentClient.(rawRelayer); ok {
		return r.RelayRaw(ctx, method, params)
	}
	return nil, fmt.Errorf("execution: transport does not support raw relay")
}

func (c *executionClient) Era() ProtocolEra {
	if source, ok := c.AgentClient.(interface{ Era() ProtocolEra }); ok {
		return source.Era()
	}
	return EraHandshake
}

func (c *executionClient) DownstreamCapabilities() Capabilities {
	if source, ok := c.AgentClient.(interface{ DownstreamCapabilities() Capabilities }); ok {
		return source.DownstreamCapabilities()
	}
	return Capabilities{}
}

func (c *executionClient) ListCacheMeta() (*int64, string) {
	if source, ok := c.AgentClient.(listCacheMetaSource); ok {
		return source.ListCacheMeta()
	}
	return nil, CacheScopePrivate
}

func (c *executionClient) AllTools() []Tool {
	if source, ok := c.AgentClient.(allToolsSource); ok {
		return source.AllTools()
	}
	return c.Tools()
}
