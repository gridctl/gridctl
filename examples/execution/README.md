# MCP execution example

This stack runs a small stdio echo server in an ordinary third-party Python image. It needs no Gridctl base image. On a supported Linux daemon environment:

```bash
gridctl apply examples/execution/stack.yaml
gridctl status --replicas
gridctl status --json
gridctl destroy examples/execution/stack.yaml
```

See [execution controls](../../docs/execution.md) for required evidence, writable state, local environment hygiene, and unsupported environments. The server is a demonstration fixture, not a maintained package server.
