# Documentation

Guides and references for gridctl.

## Learning Path

New to gridctl? Read in this order:

1. **[Installation](installation.md)** - get the binary on your machine
2. **[Quick Start](../README.md#-quick-start)** - apply your first stack in three commands
3. **[Configuration Reference](config-schema.md)** - the shape of `stack.yaml`
4. **[Skills](skills.md)** - serve prompt-only skills to upstream MCP clients
5. **[Scaling](scaling.md)** and **[Cost Observability](cost-observability.md)** - operate at volume
6. **[Troubleshooting](troubleshooting.md)** - when something goes wrong

## Getting Started

| Document | Description |
|----------|-------------|
| [Installation](installation.md) | One-liner install, package managers, container runtime detection, Podman setup, updating, uninstalling |
| [Quick Start](../README.md#-quick-start) | Apply your first stack in three commands |

## References

| Document | Description |
|----------|-------------|
| [CLI Reference](cli-reference.md) | Every `gridctl` command, grouped by domain - stack lifecycle, skills, variables, optimize, telemetry, traces, upgrade |
| [Configuration Reference](config-schema.md) | Every field in `stack.yaml` - server types, networks, resources, auth, variables |
| [REST API Reference](api-reference.md) | Gateway endpoints, request/response formats, authentication |

## Guides

| Document | Description |
|----------|-------------|
| [Skills](skills.md) | Author `SKILL.md` files and serve them as MCP prompts to upstream clients via the Library workspace or `gridctl skill *` |
| [Global Context Sync](global-context.md) | Manage one canonical global AGENTS.md and project it into each linked client's global context file via `gridctl ctx` or the Library workspace |
| [Scaling stdio servers](scaling.md) | Run multiple replicas of a single MCP server - policies, trade-offs, observability |
| [Cost Observability](cost-observability.md) | LLM pricing, per-client attribution, and the `gridctl optimize` heuristics |

## Operations

| Document | Description |
|----------|-------------|
| [Project Status](project-status.md) | Per-feature stability tiers and currently known limitations |
| [Troubleshooting](troubleshooting.md) | Common errors and resolutions - runtime, networking, vault, hot reload |

## Quick Links

- [Examples](../examples/) - 25+ example stacks (transports, OpenAPI, skills registry, vault, tracing, autoscale)
- [Contributing](../CONTRIBUTING.md) - development setup and conventions
- [Changelog](../CHANGELOG.md) - release history
