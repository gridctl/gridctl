# Security Evidence Report

`gridctl doctor --security` produces a passive, value-free evidence report. It does not scan, probe, build, pull, resolve secrets, or change pins. Exit zero means no established failures among the documented fail predicates, not that the stack is secure. The report is not a certification, trust score, or complete security assessment.

## Source selection

Source selection is explicit and has no discovery or fallback:

```text
gridctl doctor --security --source file:stack.yaml --json
gridctl doctor --security --source snapshot:security-report.json --json
gridctl doctor --security --source gateway:http://localhost:8180 --json
```

`--source` is rejected unless `--security` is set. Ordinary `gridctl doctor` flags, text, quiet behavior, and JSON schema are unchanged.

| Kind | Behavior |
|------|----------|
| `file:` | Offline parse of authored stack YAML without environment expansion, secret resolution, builds, or registry lookups. Local `extends` may be followed. Optional producer snapshots (pins, execution, startup) are unknown. |
| `snapshot:` | Offline re-render of a versioned report DTO from this feature. Unknown fields are rejected. Original timestamps are preserved. The result is supplied historical evidence, not a fresh verification. |
| `gateway:` | Authenticated GET of `/api/security-report` only. Redirects and credential-bearing URLs are rejected. CLI credentials come from recorded daemon state, not the URL. |

Unreadable, invalid, or unsupported primary input exits `2`. Missing optional evidence yields unknown checks and partial coverage, not an empty stack substitute.

`--json -q` still emits the full DTO. Human quiet output keeps failures, warnings, unknowns, source identity, and coverage.

## Predicate inventory

Coverage "complete" refers only to this named inventory, not to complete security knowledge.

| Predicate | What it reports |
|-----------|-----------------|
| `pin.schema.store` | Pin store snapshot availability and declared enablement |
| `pin.schema.baseline` | Stored pin record presence (absence is a configuration fact, not a policy violation) |
| `pin.schema.continuity` | Stored pin status. Drift is a known negative observation |
| `pin.schema.scheme` | Current versus legacy hash scheme coverage |
| `pin.scan.coverage` | Declared scan configuration; scan time/ruleset may be unknown |
| `pin.scan.findings` | Stored advisory findings. No findings without coverage is unknown, not a clean scan |
| `skill.pin.baseline` | Skill pin presence, scoped to skill subjects |
| `skill.pin.continuity` | Skill pin drift |
| `source.declared` | Authored source or image identity |
| `source.build_digest` | Declared build-input digest (not authenticated verification) |
| `source.image_observed` | Observed image reference when a snapshot supplies it |
| `source.manifest` | Registry manifest digest when a bound producer supplies it |
| `source.signature` | Downstream signatures; missing evidence is unknown, not unsigned |
| `var.completeness` | Set membership completeness. Nil membership is unknown, not empty |
| `var.references` | Reference-site and consumer counts without resolving values |
| `gateway.auth.declared` | Auth/bind declarations. A saved token does not prove route enforcement |
| `gateway.auth.startup` | Active startup snapshot when available |
| `execution.availability` | Per-replica execution evidence availability |
| `execution.enforcement` | Recorded execution outcome. Container predicates are N/A for non-container subjects |

Fail predicates (exit `1`): `pin.schema.continuity` when stored status is drift, `skill.pin.continuity` when stored status is drift, and `execution.enforcement` when recorded outcome is failed, refused, or ineligible. Heuristic findings are warnings. Pins are not explicit approvals.

## Allowlisted output

Every fact, message, location, and action is allowlisted. Bounded identifiers (subject names, tool names, package/version, producer names, digest/revision identifiers) may still contain operator-authored secrets; this report cannot detect them. Truncation bounds length; it is not redaction.

Excluded from output and action links: raw config, credential fields and default operands, command arrays, scan snippets and decoded text, raw errors and free text, URL userinfo/query/fragment values, and arbitrary provenance URLs. Actions are links to existing review surfaces such as View pins. There is no approval, auto-fix, or generated secret-bearing command.

## API and UI

`GET /api/security-report` is protected by configured gateway authentication and Host rules. It assembles in-memory and stored snapshots without container inspect, scanners, secret getters, or pin mutation. The UI shows the report on existing gateway and server detail surfaces, including SourceProvenance. Refresh re-reads this passive report and does not imply evidence was re-observed.

See the [partial-state examples](../examples/security-evidence/).
