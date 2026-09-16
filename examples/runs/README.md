# Run records

This stack enables metadata-only persisted dispatch records. Recording is best-effort and does not capture argument or result values. The stack has no MCP servers, so apply starts a gateway with empty history until you add a server and issue tool calls.

```bash
gridctl apply -f examples/runs/stack.yaml
gridctl runs list --stack runs-example --format json
gridctl runs wipe --stack runs-example -y
```

Open the web UI Traces workspace and select the Runs tab. See [Usage Observability](../../docs/usage-observability.md#run-records).
