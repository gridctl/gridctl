# Deferred discovery verification

Deferred tool discovery (`gateway.tool_discovery`, `?discovery=`) has fixture
coverage in `tests/integration/deferred_discovery_test.go`. Real-host
interoperability and small-model usefulness are an exact-head gate before
merge, not a CI gate. This record does not establish that a host selected the
two tools, or that a small model can use them.

## Procedure

Use a separate `GRIDCTL_HOME` and port. Do not reuse the operator's daemon
on `:8180`. Do not set `insecure_allow_unauthenticated`.

Build the binary from the PR head and record the commit and hash:

```bash
git rev-parse HEAD
task build
sha256sum ./gridctl
```

Fixture stack (`/tmp/deferred-discovery.yaml`):

```yaml
version: "1"
name: deferred-discovery-accept
gateway:
  tool_discovery:
    mode: deferred
runs:
  enabled: true
mcp-servers:
  - name: alpha
    command: ["./examples/_mock-servers/mock-mcp-server/mock-mcp-server", "-port", "9101"]
    transport: http
    url: http://127.0.0.1:9101/mcp
  - name: beta
    command: ["./examples/_mock-servers/mock-mcp-server/mock-mcp-server", "-port", "9102"]
    transport: http
    url: http://127.0.0.1:9102/mcp
```

Start the mocks, then:

```bash
export GRIDCTL_HOME=/tmp/gridctl-deferred-home
./gridctl apply -f /tmp/deferred-discovery.yaml -p 8280
```

Link or configure one host:

- Goose: `./gridctl link goose --discovery deferred -p 8280`
- LM Studio: `./gridctl link lmstudio --discovery deferred -p 8280`
- Open WebUI: add an admin tool-server URL with `?discovery=deferred` against a gateway bound beyond loopback with `gateway.auth` and `gateway.allowed_origins` set

Prompt: ask the model to find a tool that adds two numbers and use it.

Capture:

- the `tools/list` the host received, if the host exposes it
- the transcript, limited to the tool-call sequence (no tokens or API keys)
- `./gridctl runs list --format json` showing an outer attempt at stage `discovery` with reason `search_tools`, an outer attempt with reason `call_tool`, and an inner attempt whose `parent_attempt_id` is the `call_tool` attempt and whose tool is `alpha__add`

## Evidence

Pending.

Blocker: no operator-authorized host run against this PR head. One of the following is required before merge: install Goose and provide a local-model provider endpoint, model name, and API key; authorize an Open WebUI admin tool-server entry pointing at a gateway bound beyond loopback with `gateway.auth` and `gateway.allowed_origins` set; or run LM Studio on a machine with a desktop and link it with `gridctl link lmstudio --discovery deferred`. Do not install a host, change host configuration, bind beyond loopback, or use paid inference without that authorization. A changed head needs new evidence.

| Field | Value |
|-------|-------|
| Head | Pending |
| Binary SHA-256 | Pending |
| Host and version | Pending |
| Model identity | Pending |
| Date | Pending |
| Outcomes | Pending |
| Limitations | Real-host interoperability is required before merge. Small-model usefulness remains if only a hosted model is available. |
