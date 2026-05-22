# Project Status

Gridctl is pre-1.0 software. This page tracks the stability tier of each feature surface and lists currently known limitations.

**Stability tiers**:

- **Stable** - production-ready. Backward-compatible changes only within the `0.x` line; breaking changes ride a clearly-labeled release.
- **Experimental** - usable but the API, CLI surface, or output shape may change without notice. Pin a version if you build automation on top of it.

Last updated: **v0.1.0-beta.9** (see [CHANGELOG.md](../CHANGELOG.md) for release-by-release detail).

## Feature stability

| Feature | Status | Compatibility |
|---------|--------|---------------|
| MCP gateway (stdio, SSE, HTTP) | Stable | Backward compatible in 0.x |
| Container orchestration (Docker) | Stable | Backward compatible in 0.x |
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
| Reactive autoscaling | Experimental | May change without notice |
| Code mode | Experimental | May change without notice |
| Podman runtime | Stable | Backward compatible in 0.x |
| Skills registry (prompt-only) | Stable | Backward compatible in 0.x |
| Library workspace (UI) | Stable | No API guarantee (internal) |
| Stack export (export) | Experimental | May change without notice |
| Spec drift detection | Experimental | May change without notice |
| Visual spec builder | Experimental | May change without notice |
| Skills import (skill add) | Experimental | May change without notice |
| Distributed tracing | Experimental | May change without notice |
| Multi-agent orchestrator | Experimental | May change without notice |
| Cost observability | Experimental | May change without notice |
| Typed skill SDK (Go, TS) | Removed in v0.1.x | Replaced by prompt-only skills |
| Go plugin skill loader | Removed in v0.1.x | Replaced by prompt-only skills |
| Agent IDE (`gridctl agent dev`) | Removed in v0.1.x | Use the Library workspace instead |
| JSONL run ledger + resume | Removed in v0.1.x | — |
| LLM provider abstraction | Removed in v0.1.x | Was internal to the playground |

## Known limitations

- Podman rootless multi-container networking requires `netavark` and `aardvark-dns` (Podman 4.0+); `pasta`/`slirp4netns` are egress-only transports and are not used for inter-container communication.
- Code mode sandbox has no filesystem access (by design).
- Skills registry is local-only with no remote discovery.
- Web UI requires a modern browser (no IE11 support).

---

Back to the [docs index](README.md) or the [project README](../README.md).
