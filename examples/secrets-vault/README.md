# 🔒 Secrets Vault

Manage secrets with `gridctl vault` instead of exporting environment variables or hardcoding values in stack files.

> [!NOTE]
> The vault is not a production secrets manager — it's a local development tool. Instead of scattering API keys across shell profiles, `.env` files, and `export` statements that inevitably end up in the wrong place, the vault gives you a single place to store and reference secrets on your machine. If you're deploying to production, use a proper secrets manager like HashiCorp Vault, AWS Secrets Manager, or whatever your platform provides.

## 📄 Examples

| File | Description |
|------|-------------|
| `vault-basic.yaml` | Reference individual secrets with `${vault:KEY}` syntax |
| `vault-sets.yaml` | Auto-inject grouped secrets via variable sets |

## 💡 Concepts

### How It Works

Secrets are stored locally in `~/.gridctl/vault/` and referenced in stack YAML using `${vault:KEY}` syntax. When you deploy a stack, gridctl resolves vault references and injects them as environment variables into your containers — keeping secrets out of your stack files and version control.

```yaml
env:
  GITHUB_PERSONAL_ACCESS_TOKEN: "${vault:GITHUB_TOKEN}"
  OPENAI_API_KEY: "${vault:OPENAI_API_KEY}"
  LOG_LEVEL: "info"   # Non-secret values work as usual
```

### Storing Secrets

Secrets are key-value pairs. Set them one at a time:

```bash
# Interactive (prompts for hidden input)
gridctl vault set OPENAI_API_KEY

# Non-interactive with --value flag
gridctl vault set OPENAI_API_KEY --value "sk-proj-..."

# Piped input
echo "ghp_abc123..." | gridctl vault set GITHUB_TOKEN

# Database connection string
gridctl vault set DATABASE_URL --value "postgres://admin:pass@db.example.com:5432/myapp"
```

Key names must follow the pattern `[a-zA-Z_][a-zA-Z0-9_]*` (like environment variable names).

### Bulk Import

Import multiple secrets at once from `.env` or `.json` files:

```bash
gridctl vault import .env
gridctl vault import secrets.json
```

Where `.env` looks like:

```
STRIPE_SECRET_KEY=sk_live_your_stripe_key_here
SENDGRID_API_KEY=SG.your_sendgrid_key_here
```

### Variable Sets

Group related secrets together and inject them into all containers automatically:

```bash
# Create a set
gridctl vault sets create production

# Add secrets to the set
gridctl vault set DB_HOST --value "db.example.com" --set production
gridctl vault set DB_PASSWORD --value "s3cur3Pa55" --set production
gridctl vault set API_KEY --value "ak_prod_..." --set production
```

Then reference the set in your stack YAML — all secrets in the set are injected into every container's environment:

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

### Encryption

Protect stored secrets with passphrase-based encryption (XChaCha20-Poly1305 + Argon2id):

```bash
gridctl vault lock       # Encrypt with a passphrase
gridctl vault unlock     # Decrypt for the session
```

Set `GRIDCTL_VAULT_PASSPHRASE` to provide the passphrase non-interactively.

## 💻 Usage

```bash
# Store required secrets first
gridctl vault set GITHUB_TOKEN
gridctl vault set OPENAI_API_KEY

# Deploy the basic example
gridctl apply examples/secrets-vault/vault-basic.yaml

# Or set up variable sets and deploy
gridctl vault sets create production
gridctl vault set DB_HOST --value "db.example.com" --set production
gridctl vault set DB_PASSWORD --set production
gridctl vault set API_KEY --set production
gridctl apply examples/secrets-vault/vault-sets.yaml
```
