# Security evidence examples

These fixtures show partial security evidence reports. They are not a green happy path and do not certify a stack.

```bash
gridctl doctor --security --source file:examples/security-evidence/stack.yaml --json
gridctl doctor --security --source snapshot:examples/security-evidence/snapshot-partial.json
```

`stack.yaml` declares a container image server (`fetch`) with a published port, a remote URL server (`remote`), and gateway auth with an unresolved `${var:GATEWAY_TOKEN}` reference. File mode does not expand that value, does not load pin stores, and reports unknown for optional producers.

`snapshot-partial.json` is a saved `gridctl.security-report.v1` document with mixed fail, warn, unknown, not-applicable, suppressed, and stale findings. Re-rendering it preserves `generated_at` and treats the content as historical evidence. Imported verification claims stay declared source assertions.
