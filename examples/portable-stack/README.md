# Portable stack

This example shows how to write a `stack.yaml` that's safe to commit to git
and portable across environments. Every per-environment value lives in the
gridctl variable store on the host running gridctl; the stack file itself
references variables by name only.

The unified variable store distinguishes secrets (encrypted at rest, redacted
in logs) from plaintext configuration (visible in logs, e.g. region names or
account IDs). Both live under `${var:KEY}` syntax — the on-disk metadata
decides how they're handled at runtime.

## Setup

```bash
# Non-sensitive — appears unredacted in logs.
gridctl var set REGION us-east-1 --plaintext
gridctl var set CLUSTER_ID prod-cluster-42 --plaintext
gridctl var set AWS_ACCOUNT_ID 123456789012 --plaintext

# Secrets — Article XII default; redacted in logs.
gridctl var set DB_PASSWORD                    # interactive
gridctl var set AWS_SECRET_ACCESS_KEY          # interactive

# Verify metadata.
gridctl var list
# Key                     Type    Visibility   Set
# REGION                  string  plaintext
# CLUSTER_ID              string  plaintext
# AWS_ACCOUNT_ID          string  plaintext
# DB_PASSWORD             string  secret
# AWS_SECRET_ACCESS_KEY   string  secret
```

## Apply

```bash
gridctl apply examples/portable-stack/stack.yaml
```

The same stack file works against any environment — switch by re-setting the
variable values on the host (or by using variable sets for grouped switches).
