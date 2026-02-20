# 🔗 OpenAPI

Turn any REST API with an OpenAPI specification into MCP tools — no container, no code, no custom server.

## How It Works

Point Gridctl at an OpenAPI spec (URL or local file) and each operation becomes an MCP tool:

```yaml
mcp-servers:
  - name: my-api
    openapi:
      spec: https://api.example.com/openapi.json
```

When deployed, Gridctl:
1. Loads the OpenAPI spec
2. Converts each `operationId` into an MCP tool with proper JSON Schema input
3. Proxies tool calls as real HTTP requests to the API
4. Returns responses as MCP tool results

## 📄 Examples

| File | Description |
|------|-------------|
| `openapi-basic.yaml` | Load a public API spec, with and without operation filtering |
| `openapi-auth.yaml` | Bearer token and API key authentication patterns |

## 🔧 Configuration Reference

### Spec Source

URL or local file path (JSON or YAML):

```yaml
openapi:
  spec: https://api.example.com/openapi.json    # Remote URL
  # spec: ./specs/my-api.yaml                   # Local file (relative to stack dir)
```

### Base URL Override

Override the server URL from the spec:

```yaml
openapi:
  spec: ./api-spec.json
  baseUrl: https://staging.example.com     # Use staging instead of production
```

### Authentication

**Bearer token** — reads token from an environment variable:

```yaml
openapi:
  spec: https://api.example.com/openapi.json
  auth:
    type: bearer
    tokenEnv: API_TOKEN            # Sends: Authorization: Bearer <$API_TOKEN>
```

**Custom header** — sends any header name with a value from an environment variable:

```yaml
openapi:
  spec: https://api.example.com/openapi.json
  auth:
    type: header
    header: X-API-Key              # Header name
    valueEnv: API_KEY              # Sends: X-API-Key: <$API_KEY>
```

### Operation Filtering

Control which API operations become MCP tools:

```yaml
openapi:
  spec: https://api.example.com/openapi.json
  operations:
    include: ["getUser", "listItems"]     # Whitelist: only these operations
    # OR
    exclude: ["deleteUser", "dropTable"]  # Blacklist: everything except these
```

> [!NOTE]
> You cannot use both `include` and `exclude` on the same server. Pick one.

### Environment Variable Expansion

Local spec files support `${VAR}` and `${VAR:-default}` syntax for dynamic values. Disable with the `--no-expand` flag on `gridctl deploy`.

## 💡 When to Use OpenAPI vs Other Transports

| Approach | When to Use |
|----------|-------------|
| **OpenAPI** (`openapi:`) | You have a REST API with an OpenAPI spec and want instant MCP tools |
| **External URL** (`url:`) | The API already runs an MCP server |
| **Container** (`image:`) | You need a custom MCP server in Docker |
| **Local Process** (`command:`) | You have an MCP server binary on the host |

## 💻 Usage

```bash
gridctl deploy examples/openapi/openapi-basic.yaml
gridctl deploy examples/openapi/openapi-auth.yaml
```
