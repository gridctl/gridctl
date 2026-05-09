# Third-Party Dependencies

This document records the provenance of major third-party libraries that
gridctl depends on as Go modules and how each is integrated. Routine
transitive dependencies pulled in by the libraries listed here are
covered by their own license terms in `go.sum` and not enumerated
individually.

## cloudwego/eino

- **Project**: [github.com/cloudwego/eino](https://github.com/cloudwego/eino)
- **License**: Apache License 2.0
- **Version pinned**: `v0.8.13`
- **Purpose**: Typed graph composition library. Eino provides the
  `Graph[I, O]` / `Runnable[I, O]` / `StreamReader[T]` primitives the
  gridctl agent runtime is built on (see `pkg/agent/`).
- **Integration boundary**: Every reference to `github.com/cloudwego/eino`
  in the gridctl tree lives in `pkg/agent/internal/eino/`. The rest of
  `pkg/agent/` and all other packages import only the gridctl-shaped
  wrappers exposed from `pkg/agent`. The constraint is enforced in CI by
  `scripts/check-eino-boundary.sh`.
- **Why we depend on it as a module rather than vendor it**: Article I
  of `CONSTITUTION.md` was amended to allow mature, permissively-licensed
  Go libraries for foundational concerns where the alternative is
  reinventing a graph runtime. Depending on Eino as a module is roughly
  twice as fast as vendoring + pruning ~30k LOC, has lower 12-month
  maintenance cost, and remains reversible: if the upstream API becomes
  intolerable, only the adapter layer is rewritten.
- **Version posture**: Pinned to the latest stable tag (`v0.8.x`).
  Alpha lines (`v0.9.0-alpha.*`) are not adopted automatically.
  Bumps to alpha tags require an explicit PR.
