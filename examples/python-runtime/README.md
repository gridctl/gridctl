# Python MCP runtime base examples

These stacks derive a stdio MCP server from Gridctl's Python runtime base.
The base is a foundation image, not a ready server and not the Gridctl
gateway. Generated Python 3.10-3.13 images are unchanged; they do not switch
to this base automatically.

Build the base locally before apply. Do not treat a mutable tag or an
unpublished digest as a supported release.

```bash
docker build -t gridctl-mcp-runtime-python:local images/mcp-runtime-python
gridctl validate examples/python-runtime/locked.yaml
gridctl apply examples/python-runtime/locked.yaml
gridctl status --replicas
gridctl destroy examples/python-runtime/locked.yaml
```

Validation returns desired `pending` settings with `eligible: false`. After
admission, replica evidence is separate from MCP health.

`hashed.yaml` is the same fixture installed with a hashed wheel instead of
`uv.lock`. Both Dockerfiles install during the build, copy a matching venv
onto the runtime base, and execute `mcp-echo-fixture` directly. Compilers and
uv stay in the build stage.

Transport is explicit stdio. The hardened profile uses UID/GID 10001, a
read-only root, default `/tmp` scratch, and `network: none`. HOME is `/tmp`
so it lands on that scratch. Mount `/data` only when you need persistence.
Do not bind-mount over `/app` or the virtualenv.

Required matrix: Linux amd64 and arm64, native execution (not a multiarch
manifest), Docker on GitHub-hosted `ubuntu-24.04` and `ubuntu-24.04-arm`
runners. Those hosted jobs have not established a supported public release.
Podman is covered only when the integration suite actually runs on that
runtime. Untested combinations are unsupported.

See [Python MCP runtime base](../../docs/mcp-runtime-python.md) for ownership,
writable-state, evidence layers, and publication policy.
