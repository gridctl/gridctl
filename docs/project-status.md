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
| Skill acceptance criteria (test) | Experimental | May change without notice |
| Stack export (export) | Experimental | May change without notice |
| Spec drift detection | Experimental | May change without notice |
| Visual spec builder | Experimental | May change without notice |
| Skills import (skill add) | Experimental | May change without notice |
| Distributed tracing | Experimental | May change without notice |
| Typed skill SDK (Go, TS) | Experimental | May change without notice |
| Go plugin skill loader | Experimental | May change without notice |
| Agent IDE (`gridctl agent dev`) | Experimental | May change without notice |
| JSONL run ledger + resume | Experimental | May change without notice |
| Multi-agent orchestrator | Experimental | May change without notice |
| LLM provider abstraction | Experimental | May change without notice |
| Cost observability | Experimental | May change without notice |

## Known limitations

- Podman rootless multi-container networking requires `netavark` and `aardvark-dns` (Podman 4.0+); `pasta`/`slirp4netns` are egress-only transports and are not used for inter-container communication.
- Code mode sandbox has no filesystem access (by design).
- Skills registry is local-only with no remote discovery.
- Web UI requires a modern browser (no IE11 support).

---

Back to the [docs index](README.md) or the [project README](../README.md).
