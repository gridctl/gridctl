# Project Status

Gridctl is pre-1.0 software. This page tracks the stability tier of each feature surface and lists currently known limitations.

**Stability tiers**:

- **Stable** - production-ready. Backward-compatible changes only within the `0.x` line; breaking changes ride a clearly-labeled release.
- **Experimental** - usable but the API, CLI surface, or output shape may change without notice. Pin a version if you build automation on top of it.

Nearly every shipped feature surface is Stable as of the release candidate; the table below marks the exceptions. The Experimental tier also covers features that ship dark behind the `experimental:` feature-flag registry (see [Config Schema](config-schema.md#experimental-feature-flags)), though a surface can be Experimental for stability reasons without being flag-gated (the model routing policy is: its CLI is on by default, but the upstream LiteLLM schema it renders is still evolving).

Current as of **v0.1.0-rc.3 plus `[Unreleased]`** (see [CHANGELOG.md](../CHANGELOG.md) for release-by-release detail).

## Feature stability

| Feature | Status | Compatibility |
|---------|--------|---------------|
| MCP gateway (stdio, SSE, HTTP) | Stable | Backward compatible in 0.x |
| Container orchestration (Docker) | Stable | Backward compatible in 0.x |
| Generated Python source containers (PyPI, git, local) | Stable | Opt-in through `source.runtime: python` or `source.type: pypi`; existing Dockerfile sources are unchanged |
| Config schema (servers, resources) | Stable | Backward compatible in 0.x |
| Auth middleware (bearer, API key) | Stable | Backward compatible in 0.x |
| Hot reload | Stable | Backward compatible in 0.x |
| Vault secrets | Stable | Backward compatible in 0.x |
| Web UI | Stable | No API guarantee (internal) |
| Output format conversion | Stable | Backward compatible in 0.x |
| Token usage metrics | Stable | Backward compatible in 0.x |
| Stack validation (validate) | Stable | Backward compatible in 0.x |
| Stack planning (plan) | Stable | Backward compatible in 0.x |
| Static replicas | Stable | Backward compatible in 0.x |
| Reactive autoscaling | Stable | Backward compatible in 0.x |
| Code mode | Stable | Backward compatible in 0.x |
| Podman runtime | Stable | Backward compatible in 0.x |
| Skills registry (prompt-only) | Stable | Backward compatible in 0.x |
| Library workspace (UI) | Stable | No API guarantee (internal) |
| Stack export (export) | Stable | Backward compatible in 0.x |
| Spec drift detection | Stable | No API guarantee (internal) |
| Visual spec builder | Stable | No API guarantee (internal) |
| Skills import (skill add) | Stable | Backward compatible in 0.x |
| Skill projection (skill project) | Stable | Backward compatible in 0.x |
| Agent kind (skill add / skill project --kind agent) | Stable | Distinct from the removed Agent IDE below; backward compatible in 0.x |
| Multi-client agent renders (opencode, copilot, gemini) | Stable | Lossy by design - each dialect drops keys it cannot express; backward compatible in 0.x |
| Packs (pack add / apply / status / remove) | Stable | Manifest schema `gridctl.dev/v1`; `v1alpha1` still accepted |
| Global context sync (ctx) | Stable | Backward compatible in 0.x |
| Rules fragment library (ctx add / list / rm, fragments mode) | Stable | Backward compatible in 0.x |
| Skill governance pins (skill pins, skills: policy) | Stable | Backward compatible in 0.x |
| Model preferences (model_preferences: block, projection rewrite) | Stable | Backward compatible in 0.x |
| Model routing policy (gridctl models, LiteLLM + OpenCode projection) | Experimental | Renderer pinned to LiteLLM v1.94+ Auto Router v2; the upstream auto-router schema is still evolving. The web UI's Model routing dialog and `/api/project/models` endpoints inherit this tier |
| Distributed tracing | Stable | Backward compatible in 0.x |
| Usage observability (token metrics, optimize) | Stable | Backward compatible in 0.x |
| Telemetry persistence | Stable | Backward compatible in 0.x |
| Server catalog (search, add) | Stable | Backward compatible in 0.x |
| Client config import (import) | Stable | Backward compatible in 0.x |
| Machine reset (`gridctl reset`, reset REST + web UI dialog) | Stable | Backward compatible in 0.x |
| Home directory override (`GRIDCTL_HOME`, `--home`) | Stable | Backward compatible in 0.x |
| MCP protocol generation dual-stack (handshake + 2026-07-28 stateless) | Stable | Both generations served and auto-negotiated per client and per server |
| Declarative client linking (`link:`) | Stable | Backward compatible in 0.x |
| Wiring ownership (link / unlink recording, project) | Stable | Backward compatible in 0.x |
| Downstream OAuth brokering (auth) | Stable | Backward compatible in 0.x |
| TOFU schema pinning (pins) | Stable | Backward compatible in 0.x |
| Tool-poisoning scan | Stable | Backward compatible in 0.x |
| Tool groups | Stable | Backward compatible in 0.x |
| Per-client access scoping | Stable | Backward compatible in 0.x |
| Rate limits | Stable | Backward compatible in 0.x |
| Dollar-cost layer (pricing, model attribution, budgets) | Removed in v0.1.x | The gateway cannot observe actual spend; token metrics remain (see [Usage Observability](usage-observability.md)) |
| Typed skill SDK (Go, TS) | Removed in v0.1.x | Replaced by prompt-only skills |
| Go plugin skill loader | Removed in v0.1.x | Replaced by prompt-only skills |
| Agent IDE (`gridctl agent dev`) | Removed in v0.1.x | Use the Library workspace instead |
| Multi-agent orchestrator (A2A) | Removed in v0.1.x | Use an external agent runtime (LangGraph, CrewAI, AutoGen, OpenAI Agents SDK) over gridctl as the MCP gateway |
| JSONL run ledger + resume | Removed in v0.1.x | - |
| LLM provider abstraction | Removed in v0.1.x | Was internal to the playground |

## Known limitations

- Podman rootless multi-container networking requires `netavark` and `aardvark-dns` (Podman 4.0+); `pasta`/`slirp4netns` are egress-only transports and are not used for inter-container communication.
- Generated Python package sources support the official public PyPI index only. Private indexes require a custom Dockerfile.
- Code mode sandbox has no filesystem access (by design).
- Skills registry is local-only with no remote discovery.
- Agents and packs are first-class in the web UI: the Library's Agents segment covers catalog, editing, and per-client projection over the agents REST endpoints, and its Packs segment covers the full pack lifecycle (import, apply, status, remove) over the pack REST endpoints.
- Agent renders for OpenCode, Copilot, and Gemini CLI are lossy by design: each dialect drops frontmatter keys it cannot express, and `skill project status` names the dropped keys per row. Claude Code receives the canonical bytes verbatim.
- Global context sync covers 12 of 16 linkable clients; Claude Desktop, Cursor, AnythingLLM, and LM Studio expose no writable global context file, and Windsurf caps `global_rules.md` at 6,000 characters.
- Antigravity's skills and global-context paths rest on unofficial sourcing rather than published documentation. Those targets are marked `unofficial` in `ctx status` and `skill project status`; the projection itself is supported, but the path may move without an upstream release note.
- Web UI requires a modern browser (no IE11 support).

---

Back to the [docs index](README.md) or the [project README](../README.md).
