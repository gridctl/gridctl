# 🔐 Access Control

Examples demonstrating tool filtering and per-client scoping for least-privilege access.

## 📄 Examples

| File | Description |
|------|-------------|
| `tool-filtering.yaml` | Server-level tool filtering (applies to all clients) |
| `read-only-secrets.yaml` | Per-tool `policy:` block that blocks `vault__*` and allows `vault__list_paths` |
| `per-client-scoping.yaml` | Per-client allow-lists via the `clients:` block |

## 💡 Concepts

### Server-Level Filtering

The `tools` field on an MCP server whitelists which tools are exposed to all clients. Tools not in the list are loaded for the management UI's catalog, but they are absent from listings and rejected on `tools/call`:

```yaml
mcp-servers:
  - name: restricted-server
    image: my-mcp:latest
    port: 3000
    tools: ["read", "list"]   # Only these tools are listed and callable
```

This is the primary mechanism for reducing context window consumption and preventing tool confusion.

### Per-Client Scoping

The top-level `clients:` block restricts which servers and tools each *connecting client* can reach, independent of the global server-level filter. It follows Kubernetes NetworkPolicy semantics: omitting the block keeps today's behavior (every client sees everything), while adding it opts into least-privilege per client.

```yaml
clients:
  default: deny            # unlisted clients see nothing (or set "allow")
  profiles:
    cursor:
      servers: [github]    # cursor reaches only the github server
    claude-code:
      tools:               # claude-code reaches only these prefixed tools
        - github__search-repos
```

The scope is enforced on `tools/list`, `tools/call`, and the code-mode search/execute surface. Downstream prompts and resources follow server membership and are not narrowed by a tool whitelist. Bind a linked client to a profile with a stable identifier:

```bash
gridctl link cursor --client-id cursor
```

This example uses self-declared client identifiers, so scoping is a guardrail for cooperating clients, not authentication against a hostile client. With `gateway.auth.type: oauth`, the verified identity claim selects profiles instead; map raw, case-sensitive subjects through `aliases:`. The linked `--client-id` then becomes display-only. Configured authentication is required on every operational request, including grouped MCP endpoints. See [gateway authentication](../../docs/config-schema.md#auth).

See [docs/config-schema.md](../../docs/config-schema.md#clients-per-client-access-scoping) for the full reference, including client-identity reconciliation and reload semantics.

### Per-tool call policy

The top-level `policy:` block blocks or allows a listed tool by canonical name. It does not hide the tool. The last matching rule wins, so a broad block can keep one exception. See [Policy](../../docs/config-schema.md#policy-per-tool-call-policy).

## Verifying the policy

`read-only-secrets.yaml` puts the policy on a `vault` server with no `tools:`
whitelist, so a blocked call is a policy denial rather than the whitelist's
unknown-tool outcome. The alpine placeholder does not speak MCP. Substitute
a real Vault MCP image, apply, and then:

```bash
gridctl call vault__list_paths                  # exit 0
gridctl call vault__read_secret                 # exit 1; prints "tool blocked by policy"
gridctl call vault__read_secret --format json   # outcome.reason is policy_blocked; outcome.gate is "policy"
```

A raw stateless call returns an in-band `isError` result, not JSON-RPC `-32602`. Expect HTTP 200 and text beginning `Error: tool blocked by policy (rule 1: vault__*)`:

```bash
curl -s localhost:8180/mcp \
  -H 'content-type: application/json' \
  -H 'MCP-Protocol-Version: 2026-07-28' \
  -H 'Mcp-Method: tools/call' \
  -H 'Mcp-Name: vault__read_secret' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"vault__read_secret","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}'
```

The optional layered form keeps both controls. Add `tools: [list_paths]` beside the same policy. In that form `vault__read_secret` returns `unknown tool` from the whitelist, and the policy denial becomes observable only after the tool is exposed again.

## Verifying the whitelist

After `gridctl apply` on a server whose `tools:` list exposes `list_paths`
and hides `read_secret`:

```bash
gridctl call vault__list_paths   # exit 0
gridctl call vault__read_secret  # exit 1

# Stateless tools/call. Expect JSON-RPC -32602, message "Unknown tool: vault__read_secret".
curl -s localhost:8180/mcp \
  -H 'content-type: application/json' \
  -H 'MCP-Protocol-Version: 2026-07-28' \
  -H 'Mcp-Method: tools/call' \
  -H 'Mcp-Name: vault__read_secret' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"vault__read_secret","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}'
```

Hidden and unknown tools use the same wire error. A POST without the
stateless headers is handshake traffic and does not return that error.
When `gateway.auth` is set, the `curl` request also needs that credential.
The example files in this directory do not start a Vault server; the
commands apply to a stack that exposes `list_paths` and hides `read_secret`.

`gridctl validate` now refuses a group `tools:` or `overrides` entry that
names a hidden tool. Drop the reference, or add the tool back to the
server whitelist. A profile `tools:` entry or a group `exclude` entry that
names one is a warning.

## 💻 Usage

```bash
gridctl apply examples/access-control/tool-filtering.yaml
gridctl apply examples/access-control/read-only-secrets.yaml
gridctl apply examples/access-control/per-client-scoping.yaml
```
