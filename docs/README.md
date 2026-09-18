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
| [Release Verification](release-verification.md) | Authenticate archives before installation, inspect scoped inventories, and understand release coverage and maintainer recovery |
| [Quick Start](../README.md#-quick-start) | Apply your first stack in three commands |

## References

| Document | Description |
|----------|-------------|
| [CLI Reference](cli-reference.md) | Every `gridctl` command, grouped by domain - stack lifecycle, catalog, live tools, LLM clients, packs, wiring ownership, global context, groups, skills, variables, pins, server authorization, traces, runs, optimize, limits, telemetry, system |
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
| [MCP execution controls](execution.md) | Opt-in container restrictions, local environment hygiene, and per-replica evidence |
| [Python MCP runtime base](mcp-runtime-python.md) | Downstream Python 3.12 foundation image, derived-image recipes, and publication policy |
| [Usage Observability](usage-observability.md) | Token and call metrics, tokenizer options, format savings, `gridctl optimize` heuristics, and opt-in run records |
| [Capability and sensitive-call primitives](capability-primitives.md) | Internal shared authority accounting, generation lifecycle, payload-free observations, and diagnostic privacy |

## Operations

| Document | Description |
|----------|-------------|
| [Project Status](project-status.md) | Per-feature stability tiers and currently known limitations |
| [Practical Security Guide](security/practical-guide.md) | Operator workflow for verified installation, private deployment, credentials, execution, content review, and diagnostic privacy |
| [Security Threat Model](security/threat-model.md) | Current trust boundaries, security defaults, source/test evidence, and residual risks |
| [Security Evidence Report](security-evidence.md) | Passive `doctor --security` / `/api/security-report` scope, unknowns, and exit-zero limits |
| [Stack Declaration Policy](stack-declaration-policy.md) | Offline `validate --policy` checks for captured declarations; not runtime admission |
| [Adversarial Regression Gates](adversarial-regression-gates.md) | Scenario index and post-suite execution accounting; not comprehensive security testing |
| [Troubleshooting](troubleshooting.md) | Common errors and resolutions - runtime, networking, vault, hot reload |

## Quick Links

- [Examples](../examples/) - example stacks and repos (transports, generated Python sources, OpenAPI, skills registry, portable pack, variables, tracing, run records, autoscale, code mode, access control, declarative linking)
- [Contributing](../CONTRIBUTING.md) - development setup and conventions
- [Changelog](../CHANGELOG.md) - release history
