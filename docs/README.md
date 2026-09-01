# Documentation

Guides and references for gridctl.

## Learning Path

New to gridctl? Read in this order:

1. **[Installation](installation.md)** - get the binary on your machine
2. **[Quick Start](../README.md#-quick-start)** - apply your first stack in three commands
3. **[Configuration Reference](config-schema.md)** - the shape of `stack.yaml`
4. **[Skills](skills.md)** - serve skills to upstream MCP clients and project skills and agents onto disk
5. **[Packs](packs.md)** - import skills, agents, rules, and wiring as one unit from a git repo
6. **[Scaling](scaling.md)** and **[Usage Observability](usage-observability.md)** - operate at volume
7. **[Troubleshooting](troubleshooting.md)** - when something goes wrong

## Getting Started

| Document | Description |
|----------|-------------|
| [Installation](installation.md) | One-liner install, package managers, container runtime detection, Podman setup, updating, uninstalling |
| [Quick Start](../README.md#-quick-start) | Apply your first stack in three commands |

## References

| Document | Description |
|----------|-------------|
| [CLI Reference](cli-reference.md) | Every `gridctl` command, grouped by domain - stack lifecycle, catalog, LLM clients, packs, wiring ownership, global context, groups, skills, variables, pins, server authorization, traces, optimize, limits, telemetry, system |
| [Configuration Reference](config-schema.md) | Every field in `stack.yaml` - server types, generated Python sources, networks, resources, auth, variables |
| [REST API Reference](api-reference.md) | Gateway endpoints, request/response formats, authentication |

## Guides

| Document | Description |
|----------|-------------|
| [Skills](skills.md) | Author `SKILL.md` files, serve them as MCP prompts, import agents alongside them, and project both onto disk for file-reading clients via `gridctl skill project` |
| [Packs](packs.md) | One `gridctl-pack.yaml` manifest importing skills, agents, rule fragments, and wiring as a unit, with tag-exact removal |
| [Tools Workspace](tools-workspace.md) | Curate the exposed tool surface - whitelists, Audit Mode, annotation hints, fleet actions, per-client access, and groups |
| [Global Context Sync](global-context.md) | Manage the global context (one canonical AGENTS.md, or an opt-in rule fragment library with per-client assembly) via `gridctl ctx`, the web UI, or the REST API |
| [Scaling stdio servers](scaling.md) | Run multiple replicas of a single MCP server - policies, trade-offs, observability |
| [Usage Observability](usage-observability.md) | Token and call metrics, tokenizer options, format savings, and the `gridctl optimize` heuristics |

## Operations

| Document | Description |
|----------|-------------|
| [Project Status](project-status.md) | Per-feature stability tiers and currently known limitations |
| [Troubleshooting](troubleshooting.md) | Common errors and resolutions - runtime, networking, vault, hot reload |

## Quick Links

- [Examples](../examples/) - example stacks and repos (transports, generated Python sources, OpenAPI, skills registry, portable pack, variables, tracing, autoscale, code mode, access control, declarative linking)
- [Contributing](../CONTRIBUTING.md) - development setup and conventions
- [Changelog](../CHANGELOG.md) - release history
