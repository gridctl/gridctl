# Declarative client linking

This example declares its LLM clients in the stack file. Instead of running
`gridctl link <client>` on every machine, the `link:` block makes linking part
of `gridctl apply`:

```bash
gridctl apply stack.yaml
```

Once the gateway is healthy, apply links each declared client exactly as
`gridctl link` would: Claude Desktop and Claude Code get the full gateway
endpoint, and Cursor gets the `dev` group's endpoint under the entry name
`gridctl-dev`, bound to a `clients:` access profile via `client_id`.

Reconcile is additive and idempotent. Already-linked clients are silent
no-ops; clients not installed on this machine warn and skip, so the same
committed file works for every teammate. Removing an entry never unlinks
anything — removal stays explicit:

```bash
gridctl unlink cursor --name gridctl-dev   # one client
gridctl destroy stack.yaml --unlink        # everything this stack declared
```

Preview what apply would do with `gridctl plan stack.yaml` (the declared
client links print in their own section), or manage the same state from the
web UI's Connections workspace, which shows declared/detected/linked status
per client with a diff preview before any config write.
