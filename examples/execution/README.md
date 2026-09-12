# MCP execution example

This stack runs a small stdio echo server in an ordinary third-party Python image. It needs no Gridctl base image. Run from the repository root with a Linux client, a local Unix daemon endpoint, and safely observable instance-bound `/proc` and cgroup v2 state:

```bash
gridctl validate examples/execution/stack.yaml --format json
gridctl apply examples/execution/stack.yaml
gridctl status --replicas
gridctl status --json
gridctl destroy examples/execution/stack.yaml
```

Validation returns desired `pending` settings with `eligible: false`. After admission, the active replica must report eligible `observed` evidence independently of MCP health. This example uses UID/GID 65534, no network, a read-only root, and the finite resource/scratch defaults. Initial image pull still needs registry access. Use `apply --foreground` when supervising the process directly.

See [execution controls](../../docs/execution.md) for required evidence, writable state, local environment hygiene, and unsupported environments. The server is a demonstration fixture, not a maintained package server.
