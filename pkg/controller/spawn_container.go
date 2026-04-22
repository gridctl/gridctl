package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runtime"
)

// PortAllocator hands out unique host ports for new container replicas.
// Implementations must be safe to call from the autoscaler's goroutine.
type PortAllocator interface {
	Allocate() int
}

// atomicPortAllocator is a simple monotonic allocator starting at basePort.
// Production deployments can swap this for one that tracks releases, but a
// monotonic allocator matches the orchestrator's existing behaviour (host
// ports are freed when the container exits; OS reclaims them eventually).
type atomicPortAllocator struct {
	next atomic.Int32
}

// NewAtomicPortAllocator returns a PortAllocator starting at base+1 on the
// first call. base is typically the basePort handed to Orchestrator.Up plus
// the number of ports already assigned during the initial bring-up.
func NewAtomicPortAllocator(base int) PortAllocator {
	p := &atomicPortAllocator{}
	p.next.Store(int32(base))
	return p
}

// Allocate returns the next host port, incrementing the internal counter
// atomically. Safe to call from any goroutine.
func (p *atomicPortAllocator) Allocate() int {
	return int(p.next.Add(1))
}

// ContainerSpawner spawns container-backed MCP replicas (stdio or HTTP). Each
// Spawn starts a new container named `<stack>-<server>-replica-<id>`, waits
// for it via the same BuildAgentClient path the static bring-up uses, and
// tracks the workload id so Reap can stop + remove the container.
type ContainerSpawner struct {
	builder   ClientBuilder
	rt        runtime.WorkloadRuntime
	stack     string
	server    config.MCPServer
	network   string
	image     string
	transport string
	ports     PortAllocator
	logger    *slog.Logger

	idCounter atomic.Int64

	mu       sync.Mutex
	workloads map[mcp.AgentClient]runtime.WorkloadID // client -> container id, for Reap
}

// ContainerSpawnerOptions bundles everything a ContainerSpawner needs. Kept
// as a struct so callers don't accumulate positional arguments.
type ContainerSpawnerOptions struct {
	Builder   ClientBuilder
	Runtime   runtime.WorkloadRuntime
	Stack     string             // stack.Name
	Server    config.MCPServer   // full server config (command, env, port, etc.)
	Network   string             // resolved network name
	Image     string             // pre-built image (source-based workloads pre-tag as gridctl-<stack>-<server>:latest)
	Transport string             // "http" | "stdio" | "sse"
	Ports     PortAllocator
	Logger    *slog.Logger
	InitialID int // next replica id to assign (typically set.Size() at register time)
}

// NewContainerSpawner constructs a ContainerSpawner from explicit options.
func NewContainerSpawner(opts ContainerSpawnerOptions) *ContainerSpawner {
	logger := opts.Logger
	if logger == nil {
		logger = logging.NewDiscardLogger()
	}
	cs := &ContainerSpawner{
		builder:   opts.Builder,
		rt:        opts.Runtime,
		stack:     opts.Stack,
		server:    opts.Server,
		network:   opts.Network,
		image:     opts.Image,
		transport: opts.Transport,
		ports:     opts.Ports,
		logger:    logger,
		workloads: make(map[mcp.AgentClient]runtime.WorkloadID),
	}
	cs.idCounter.Store(int64(opts.InitialID))
	return cs
}

// Spawn starts a new container and returns a ready-to-serve AgentClient.
// On any failure after container start but before client initialisation,
// the spawned container is stopped and removed so no orphan containers
// linger. Matches the "defer cleanup()" requirement in the spec.
func (c *ContainerSpawner) Spawn(ctx context.Context) (mcp.AgentClient, error) {
	if c.rt == nil {
		return nil, fmt.Errorf("container spawner %q: runtime unavailable", c.server.Name)
	}
	replicaID := int(c.idCounter.Add(1) - 1)
	name := runtime.ReplicaContainerName(c.stack, c.server.Name, replicaID, 2) // >1 to force suffix
	hostPort := c.ports.Allocate()

	cfg := runtime.WorkloadConfig{
		Name:        name,
		Stack:       c.stack,
		Type:        runtime.WorkloadTypeMCPServer,
		Image:       c.image,
		Command:     c.server.Command,
		Env:         c.server.Env,
		NetworkName: c.network,
		ExposedPort: c.server.Port,
		HostPort:    hostPort,
		Transport:   c.transport,
		Labels: map[string]string{
			"gridctl.managed":    "true",
			"gridctl.stack":      c.stack,
			"gridctl.mcp-server": c.server.Name,
		},
	}

	c.logger.Info("autoscale spawn: starting container",
		"server", c.server.Name, "replica_id", replicaID, "container", name, "host_port", hostPort)

	status, err := c.rt.Start(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("start container %s: %w", name, err)
	}
	actualHostPort := status.HostPort
	if actualHostPort == 0 {
		actualHostPort = hostPort
	}

	// Any failure from here on should tear down the container we just started.
	cleaned := false
	cleanup := func() {
		if cleaned {
			return
		}
		cleaned = true
		if err := c.rt.Stop(context.Background(), status.ID); err != nil {
			c.logger.Warn("autoscale spawn cleanup: stop failed", "container", name, "error", err)
		}
		if err := c.rt.Remove(context.Background(), status.ID); err != nil {
			c.logger.Warn("autoscale spawn cleanup: remove failed", "container", name, "error", err)
		}
	}

	client, err := c.builder.BuildAgentClient(ctx, c.buildClientConfig(actualHostPort, status.ID))
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("build client %s: %w", name, err)
	}

	// Track the container id so Reap can stop + remove it later.
	c.mu.Lock()
	c.workloads[client] = status.ID
	c.mu.Unlock()
	return client, nil
}

// Reap closes the client and removes the backing container. Callers pass the
// full Replica (not the bare client) so the scaler can log per-replica id.
func (c *ContainerSpawner) Reap(ctx context.Context, r *mcp.Replica) error {
	if r == nil {
		return nil
	}
	client := r.Client()
	if closer, ok := client.(io.Closer); ok {
		if err := closer.Close(); err != nil {
			c.logger.Warn("autoscale reap: client close", "replica_id", r.ID(), "error", err)
		}
	}

	c.mu.Lock()
	id, ok := c.workloads[client]
	if ok {
		delete(c.workloads, client)
	}
	c.mu.Unlock()

	if !ok || c.rt == nil {
		return nil
	}
	var errs []error
	if err := c.rt.Stop(ctx, id); err != nil {
		c.logger.Warn("autoscale reap: stop failed", "replica_id", r.ID(), "container", id, "error", err)
		errs = append(errs, fmt.Errorf("stop %s: %w", id, err))
	}
	if err := c.rt.Remove(ctx, id); err != nil {
		errs = append(errs, fmt.Errorf("remove %s: %w", id, err))
	}
	return errors.Join(errs...)
}

// buildClientConfig assembles the MCPServerConfig passed to BuildAgentClient.
// stdio containers consume containerID; http/sse consume the endpoint URL.
func (c *ContainerSpawner) buildClientConfig(hostPort int, id runtime.WorkloadID) mcp.MCPServerConfig {
	cfg := mcp.MCPServerConfig{
		Name:         c.server.Name,
		Transport:    mcp.Transport(c.transport),
		Tools:        c.server.Tools,
		OutputFormat: c.server.OutputFormat,
		PinSchemas:   c.server.PinSchemas,
		ReadyTimeout: c.server.ResolvedReadyTimeout(),
	}
	if cfg.Transport == "" {
		cfg.Transport = mcp.TransportHTTP
	}
	if cfg.Transport == mcp.TransportStdio {
		cfg.ContainerID = string(id)
	} else {
		cfg.Endpoint = fmt.Sprintf("http://localhost:%d/mcp", hostPort)
	}
	return cfg
}
