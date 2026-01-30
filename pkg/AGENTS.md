# Gridctl Go Package Guide

## Go Conventions

- Use standard library when possible
- Error handling: return errors, don't panic
- Logging: use `log/slog` with `SetLogger()` pattern (silent by default, enable via CLI flags)
- Context: pass context.Context for cancellation
- Testing: table-driven tests preferred
- Interfaces: define interfaces for external dependencies (like Docker) to enable mocking

## Interface Patterns

### External Dependencies

Define interfaces for external dependencies to enable testing:

```go
// pkg/dockerclient/interface.go
type DockerClient interface {
    ContainerCreate(ctx context.Context, ...) (container.CreateResponse, error)
    ContainerStart(ctx context.Context, containerID string, opts container.StartOptions) error
    // ...
}
```

### Protocol Clients

MCP and A2A clients implement the `AgentClient` interface:

```go
// pkg/mcp/types.go
type AgentClient interface {
    Initialize(ctx context.Context) error
    ListTools(ctx context.Context) ([]Tool, error)
    CallTool(ctx context.Context, name string, args map[string]any) (*ToolResult, error)
    Close() error
}
```

## Package Overview

| Package | Purpose |
|---------|---------|
| `pkg/config` | Stack YAML parsing and validation |
| `pkg/mcp` | MCP protocol implementation (see [pkg/mcp/AGENTS.md](mcp/AGENTS.md)) |
| `pkg/a2a` | A2A protocol implementation |
| `pkg/runtime` | Workload orchestration (runtime-agnostic) |
| `pkg/runtime/docker` | Docker runtime implementation |
| `pkg/builder` | Image building from source |
| `pkg/state` | Daemon state management |
| `pkg/adapter` | Protocol adapters (A2A client) |
| `pkg/dockerclient` | Docker client interface for mocking |
| `pkg/logging` | Logging utilities |

## Error Handling

Return errors with context using `fmt.Errorf`:

```go
if err != nil {
    return fmt.Errorf("failed to start container %s: %w", name, err)
}
```

## Logging

Use `log/slog` with the `SetLogger()` pattern:

```go
var logger *slog.Logger = slog.New(slog.DiscardHandler{})

func SetLogger(l *slog.Logger) {
    logger = l
}
```

This keeps packages silent by default but allows the CLI to enable logging via flags.
