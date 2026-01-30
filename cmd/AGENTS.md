# Gridctl CLI Guide

## CLI Usage

```bash
# Start a stack (runs as daemon, returns immediately)
./gridctl deploy examples/getting-started/agent-basic.yaml

# Start with options
./gridctl deploy stack.yaml --port 8180 --no-cache

# Run in foreground with verbose output (for debugging)
./gridctl deploy stack.yaml --foreground

# Check running gateways and containers
./gridctl status

# Stop a specific stack (gateway + containers)
./gridctl destroy examples/getting-started/agent-basic.yaml
```

## Command Reference

### `gridctl deploy <stack.yaml>`

Starts containers and MCP gateway for a stack.

| Flag | Short | Description |
|------|-------|-------------|
| `--foreground` | `-f` | Run in foreground with verbose output (don't daemonize) |
| `--port` | `-p` | Port for MCP gateway (default: 8180) |
| `--no-cache` | | Force rebuild of source-based images |
| `--verbose` | `-v` | Print full stack as JSON |

### `gridctl destroy <stack.yaml>`

Stops the gateway daemon and removes all containers for a stack.

### `gridctl status`

Shows running gateways and containers.

| Flag | Short | Description |
|------|-------|-------------|
| `--stack` | `-t` | Filter by stack name |

## Daemon Mode

By default, `gridctl deploy` runs the MCP gateway as a background daemon:
- Waits until all MCP servers are initialized before returning (up to 60s timeout)
- State stored in `~/.gridctl/state/{name}.json`
- Logs written to `~/.gridctl/logs/{name}.log`
- Use `--foreground` (-f) to run interactively with verbose output

## State Files

Gridctl stores daemon state in `~/.gridctl/`:

```
~/.gridctl/
├── state/              # Daemon state files
│   └── {name}.json     # PID, port, start time per stack
├── logs/               # Daemon log files
│   └── {name}.log      # stdout/stderr from daemon
└── cache/              # Build cache
    └── ...             # Git repos, Docker contexts
```
