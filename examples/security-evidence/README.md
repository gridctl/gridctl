# Security evidence examples

These fixtures show partial security evidence reports. They are not a green happy path and do not certify a stack.

```bash
gridctl doctor --security --source file:examples/security-evidence/stack.yaml --json
gridctl doctor --security --source snapshot:examples/security-evidence/snapshot-partial.json
```

`stack.yaml` declares one container server and gateway auth by reference. File mode does not resolve the token, does not load pin stores, and reports unknown for optional producers.

`snapshot-partial.json` is a saved `gridctl.security-report.v1` document with mixed pass, warn, unknown, not-applicable, and suppressed findings. Re-rendering it preserves `generated_at` and treats the content as historical evidence.
