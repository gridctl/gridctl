# 🔗 OpenAPI

Turn any REST API with an OpenAPI specification into MCP tools - no container, no code, no custom server.

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
| `openapi-auth.yaml` | Bearer, custom header, query parameter, OAuth2 client credentials, and HTTP Basic authentication, plus mTLS |

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

**Bearer token** - reads token from an environment variable:

```yaml
openapi:
  spec: https://api.example.com/openapi.json
  auth:
    type: bearer
    tokenEnv: API_TOKEN            # Sends: Authorization: Bearer <$API_TOKEN>
```

**Custom header** - sends any header name with a value from an environment variable:

```yaml
openapi:
  spec: https://api.example.com/openapi.json
  auth:
    type: header
    header: X-API-Key              # Header name
    valueEnv: API_KEY              # Sends: X-API-Key: <$API_KEY>
```

**Query parameter** - appends an API key to every request URL:

```yaml
openapi:
  spec: https://api.example.com/openapi.json
  auth:
    type: query
    paramName: appid               # Appends: ?appid=<$OPENWEATHER_API_KEY>
    valueEnv: OPENWEATHER_API_KEY
```

**OAuth2 client credentials** - fetches a short-lived token from `tokenUrl` and injects it as a bearer header; tokens are cached and refreshed on expiry:

```yaml
openapi:
  spec: https://api.example.com/openapi.json
  auth:
    type: oauth2
    clientIdEnv: OAUTH_CLIENT_ID
    clientSecretEnv: OAUTH_CLIENT_SECRET
    tokenUrl: https://auth.example.com/oauth/token
    scopes: [read:data, write:data]
```

**HTTP Basic** - sends `Authorization: Basic <base64(username:password)>`:

```yaml
openapi:
  spec: https://api.example.com/openapi.json
  auth:
    type: basic
    usernameEnv: API_USERNAME
    passwordEnv: API_PASSWORD
```

**mTLS** - the `tls:` block is transport-layer and combines with any auth type:

```yaml
openapi:
  spec: https://secure-api.example.com/openapi.json
  auth:
    type: bearer
    tokenEnv: SECURE_API_TOKEN
  tls:
    certFile: ~/.gridctl/certs/client.pem
    keyFile: ~/.gridctl/certs/client-key.pem
    caFile: ~/.gridctl/certs/ca.pem
```

All patterns are exercised in `openapi-auth.yaml`; see [`docs/config-schema.md`](../../docs/config-schema.md) for the full field reference.

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

Use the raw `operationId` from the spec, not the generated tool name. Tool names are sanitized to `[a-zA-Z0-9_-]`, so an operation like `pets.list` is advertised as `pets_list` while the filter still matches `pets.list`. Listing the sanitized name matches nothing and quietly exposes every operation.

Filtering here is generation-time: excluded operations never become tools, which is what keeps a large spec from flooding a client's tool list. It is also not reversible from the runtime `tools` whitelist, so treat it as the exposure decision. In the web UI, the create-server wizard's Operations Filter loads the spec and lets you pick operations by ID, path, method, or tag instead of writing the list by hand.

### Environment Variable Expansion

Local spec files support `${VAR}` and `${VAR:-default}` syntax for dynamic values. Disable with the `--no-expand` flag on `gridctl apply`.

## 💡 When to Use OpenAPI vs Other Transports

| Approach | When to Use |
|----------|-------------|
| **OpenAPI** (`openapi:`) | You have a REST API with an OpenAPI spec and want instant MCP tools |
| **External URL** (`url:`) | The API already runs an MCP server |
| **Container** (`image:`) | You need a custom MCP server in Docker |
| **Local Process** (`command:`) | You have an MCP server binary on the host |

## 💻 Usage

```bash
gridctl apply examples/openapi/openapi-basic.yaml
gridctl apply examples/openapi/openapi-auth.yaml
```
