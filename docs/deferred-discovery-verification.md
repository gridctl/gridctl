# Deferred discovery verification

Deferred tool discovery (`gateway.tool_discovery`, `?discovery=`) has fixture
coverage in `tests/integration/deferred_discovery_test.go`. Real-host
interoperability and small-model usefulness are an exact-head gate before
merge, not a CI gate. This record does not establish that a host selected the
two tools, or that a small model can use them.

## Procedure

Run the commands from the repository root. Use a separate `GRIDCTL_HOME`
and port. Do not reuse the operator's daemon on `:8180`. Do not set
`insecure_allow_unauthenticated`. Keep `GRIDCTL_HOME` exported for every
`./gridctl` command below, including `runs` and `destroy`.

Build the binary from the PR head and record the commit and hash:

```bash
git rev-parse HEAD
task build
sha256sum ./gridctl
```

The acceptance mock is the HTTP server in
`examples/_mock-servers/mock-mcp-server`. It is not a stdio local process.
Start two copies beside the gateway. Do not put `command` and `url` on the
same server. If `9101` or `9102` is taken, change the port in both the
command and the stack URL.

```bash
go build -o /tmp/mock-mcp-server ./examples/_mock-servers/mock-mcp-server
/tmp/mock-mcp-server -port 9101
/tmp/mock-mcp-server -port 9102
```

Leave both processes running. The default protocol is the handshake
generation, which is what Goose and LM Studio speak. Do not pass
`-protocol 2026-07-28` unless the host is a stateless client.

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
    url: http://127.0.0.1:9101/mcp
    transport: http
  - name: beta
    url: http://127.0.0.1:9102/mcp
    transport: http
```

`./gridctl validate /tmp/deferred-discovery.yaml` exits 2 with the usual
no-auth warning. That is not a schema error. The bind stays loopback.

```bash
export GRIDCTL_HOME=/tmp/gridctl-deferred-home
./gridctl apply /tmp/deferred-discovery.yaml --port 8280
```

Apply daemonizes. Stop it later with
`./gridctl destroy deferred-discovery-accept` under the same `GRIDCTL_HOME`.

Link or configure one host. The sample stack above is loopback. Do not reuse
it for a bind beyond loopback.

- Goose: `./gridctl link goose --discovery deferred --port 8280`
- LM Studio: `./gridctl link lmstudio --discovery deferred --port 8280`
- Open WebUI: add an admin tool-server URL with `?discovery=deferred` against a separate gateway bound beyond loopback with `gateway.auth` and `gateway.allowed_origins` set

Prompt: ask the model to find a tool that adds two numbers and use it.
The mock tool is `alpha__add` (`Adds two numbers together`).

Capture:

- the `tools/list` the host received, if the host exposes it
- the transcript, limited to the tool-call sequence (no tokens or API keys)
- `./gridctl runs list --format json -s deferred-discovery-accept` showing an outer attempt at stage `discovery` with reason `search_tools`, an outer attempt with reason `call_tool`, and an inner attempt whose `parent_attempt_id` is the `call_tool` attempt and whose tool is `alpha__add`

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
