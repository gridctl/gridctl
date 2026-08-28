# 🔒 Variable Store

Manage secrets and configuration with `gridctl var` instead of exporting environment variables or hardcoding values in stack files.

> [!NOTE]
> The variable store is not a production secrets manager - it's a local development tool. Instead of scattering API keys across shell profiles, `.env` files, and `export` statements that inevitably end up in the wrong place, the store gives you a single place to keep secrets and config on your machine. If you're deploying to production, use a proper secrets manager like HashiCorp Vault, AWS Secrets Manager, or whatever your platform provides.

## 📄 Examples

| File | Description |
|------|-------------|
| `var-basic.yaml` | Reference individual variables and declare value-free prerequisites |
| `var-sets.yaml` | Auto-inject grouped variables via variable sets |
| `var-sets-scoped.yaml` | Scope a set to named servers and resources (least privilege) |
| `vault-basic.yaml` | Deprecated `${vault:KEY}` syntax (kept as a regression example) |
| `vault-sets.yaml` | Deprecated variable-set example using the `vault` aliases |

> [!WARNING]
> `gridctl vault …` and the `${vault:KEY}` reference syntax are deprecated aliases for `gridctl var …` and `${var:KEY}`. Both resolve through the same store during the beta cycle and are removed at v1.0. New stacks should use `var`.

## 💡 Concepts

### How It Works

Variables are stored locally in `~/.gridctl/vault/` and referenced in stack YAML using `${var:KEY}` syntax. When you deploy a stack, gridctl resolves the references and injects them as environment variables into your containers - keeping values out of your stack files and version control.

```yaml
env:
  GITHUB_PERSONAL_ACCESS_TOKEN: "${var:GITHUB_TOKEN}"
  OPENAI_API_KEY: "${var:OPENAI_API_KEY}"
  LOG_LEVEL: "${var:LOG_LEVEL}"   # Plaintext variables work the same way
```

### Secrets vs Plaintext

The store is unified: it holds both secrets and non-sensitive configuration. Entries are secret by default (encrypted at rest when locked, redacted in logs). Use `--plaintext` for config that should stay legible in logs and the web UI.

```bash
gridctl var set OPENAI_API_KEY                         # secret (default)
gridctl var set LOG_LEVEL --value info --plaintext     # plaintext config
```

### Storing Variables

Set them one at a time:

```bash
# Interactive (prompts for hidden input)
gridctl var set OPENAI_API_KEY

# Non-interactive with --value flag
gridctl var set OPENAI_API_KEY --value "sk-proj-..."

# Piped input
echo "ghp_abc123..." | gridctl var set GITHUB_TOKEN

# Database connection string
gridctl var set DATABASE_URL --value "postgres://admin:pass@db.example.com:5432/myapp"
```

Key names must follow the pattern `[a-zA-Z_][a-zA-Z0-9_]*` (like environment variable names). Use `--type <string|json|list|number|bool>` to tag and validate the value's shape.

Names beginning with `GRIDCTL_`, plus `OP_CONNECT_TOKEN` and
`OP_SERVICE_ACCOUNT_TOKEN`, are reserved for gridctl bootstrap and control-plane
credentials. Store downstream credentials under application-specific names.

### Declaring Requirements

A stack can document the variables it expects without storing values or making
the declaration part of expansion:

```yaml
variables:
  GITHUB_TOKEN:
    required: true
    secret: true
    type: string
    description: Token used by the GitHub MCP server
    docs: https://github.com/github/github-mcp-server
```

Declarations are advisory. They appear in validation diagnostics and
`gridctl var explain`, but they never write the store or make `apply` prompt
for a value.

### Inspecting and Delivering Variables

```bash
# Show value-free metadata, resolution, declarations, and stack consumers.
gridctl var explain GITHUB_TOKEN --file examples/secrets-vault/var-basic.yaml

# Overlay an explicit key or set onto a direct child process.
gridctl var run --only GITHUB_TOKEN -- gh auth status
gridctl var run --set production -- ./service

# Check working-tree files or changed staged blobs for exact stored values.
gridctl var scan .
gridctl var scan --staged
```

`var run` redacts exact secret values from non-TTY output by default. `var scan`
is an exact-value check, not a pattern or history scanner; pair it with tools
such as Gitleaks or TruffleHog when those checks are required.

### Bulk Import

Import multiple variables at once from `.env` or `.json` files:

```bash
gridctl var import .env
gridctl var import secrets.json
```

Where `.env` looks like:

```
STRIPE_SECRET_KEY=sk_live_your_stripe_key_here
SENDGRID_API_KEY=SG.your_sendgrid_key_here

# @public            — store as plaintext (default is secret)
# @type=list         — record the variable's type
# @description="service tags"
# @docs="https://example.com/service-tags"
TAGS=app,backend,prod
```

### Variable Sets

Group related variables together and inject them into all containers automatically:

```bash
# Create a set
gridctl var sets create production

# Add variables to the set
gridctl var set DB_HOST --value "db.example.com" --set production --plaintext
gridctl var set DB_PASSWORD --value "s3cur3Pa55" --set production
gridctl var set API_KEY --value "ak_prod_..." --set production
```

Then reference the set in your stack YAML - all variables in the set are injected into every container's environment:

```yaml
secrets:
  sets:
    - production

mcp-servers:
  - name: backend
    image: ghcr.io/org/backend-mcp:latest
    port: 3000
    env:
      LOG_LEVEL: "info"   # Explicit values take precedence over set values
```

> The stack field is named `secrets.sets` for backward compatibility; set membership is type-agnostic and works for both secrets and plaintext variables.

#### Scoping a set to named workloads

A bare set name injects every member into every server and every resource. To
give a workload only what it needs, write the entry as a mapping instead:

```yaml
secrets:
  sets:
    - shared                  # every server and resource

    - name: github-creds      # only the github server
      servers:
        - github

    - name: db-creds          # only the postgres resource
      resources:
        - postgres
```

A scoped entry reaches exactly what it names and nothing else, so
`servers: [github]` injects into the `github` server and into no resources at
all. Naming a server or resource the stack does not declare is a validation
error, so a typo fails the apply instead of quietly withholding a credential.

Scoping is opt-in per entry. An entry left as a bare name keeps the original
fan-out, so existing stacks behave exactly as before. See
`var-sets-scoped.yaml` for a complete stack.

Mistakes fail closed. Writing `server:` instead of `servers:` is rejected
rather than quietly ignored, and `servers: []` means "no workloads" rather
than "all workloads", so a half-finished scope never widens access.

The Variables workspace shows the result: a scoped set's variables list the
workloads they actually reach, and each one links to that node on the Stack
canvas.

### Encryption

Protect the store with passphrase-based encryption (XChaCha20-Poly1305 + Argon2id):

```bash
gridctl var lock       # Encrypt with a passphrase
gridctl var unlock     # Decrypt for the session
```

Set `GRIDCTL_VAULT_PASSPHRASE` to provide the passphrase non-interactively.

## 💻 Usage

```bash
# Store required variables first
gridctl var set GITHUB_TOKEN
gridctl var set OPENAI_API_KEY
gridctl var set LOG_LEVEL --value info --plaintext

# Deploy the basic example
gridctl apply examples/secrets-vault/var-basic.yaml

# Or set up a variable set and deploy
gridctl var sets create production
gridctl var set DB_HOST --value "db.example.com" --set production --plaintext
gridctl var set DB_PASSWORD --set production
gridctl var set API_KEY --set production
gridctl apply examples/secrets-vault/var-sets.yaml
```
