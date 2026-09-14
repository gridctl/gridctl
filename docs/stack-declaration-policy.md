# Stack Declaration Policy

`gridctl validate <stack.yaml> --policy <policy.yaml>` evaluates captured stack declarations against a finite, versioned rule set. It does not authorize deployment, inspect runtime state, or replace ordinary `gridctl validate`.

Exit zero means the assessed declarations satisfy the selected requirements for the captured bytes. It does not mean later `apply`, REST edits, watcher reload, or external execution are blocked or safe.

## Invocation

```bash
gridctl validate stack.yaml --policy ./policies/production.yaml
gridctl validate stack.yaml --policy ./policies/production.yaml --format json
```

`--policy` is opt-in. Omitting it leaves ordinary validation unchanged, including `--check-mutable-refs`. Passing the flag with an empty, missing, or invalid policy file fails and does not fall back to ordinary validation.

Exit codes match `validate`: `0` accepted with no warnings, `1` input or evaluation errors, rejected requirements, or indeterminate results, `2` accepted with warnings only. Errors take precedence.

JSON uses `--format json` or `--json` and emits one versioned document on stdout, including ordinary policy and input failures where a report can still be built. Text and JSON never print progress on stdout. An encoding or write failure returns a non-success status and does not claim a complete report.

## Policy file

The policy is a separate YAML document. Stack YAML cannot select, disable, or exempt rules. There is no working-directory discovery, environment override, plugin, or expression language.

```yaml
version: "1"
enabled:
  - explicit-image-digests
  - deny-local-command-servers
  - deny-ssh-servers
  - schema-pinning-block
  - nonempty-server-tool-lists
```

`version` must be the string `"1"`. `enabled` must be a nonempty sequence of known rule IDs with no duplicates. Unknown fields, extra documents, interpolations, includes, and inheritance are rejected. Old checkers reject unsupported versions and unknown rules instead of evaluating a subset.

The report includes checker version `1`, the policy version, a SHA-256 digest of the policy bytes, the enabled rule IDs, and coverage counts. The digest identifies those bytes. It is not a signature, trusted origin, or proof that the policy was independently reviewed.

Keep the top-level stack `policy:` key unused. That namespace is reserved for a separate runtime-authority feature.

## Candidate inputs

Policy mode reads only:

- the operator-selected policy file
- the entry stack file
- bounded local `extends` parents under the entry stack directory (the candidate root)

It does not read source directories, Dockerfiles, OpenAPI specs, credential files, runtime sockets, user home configuration, or vault/registry state. Referenced paths are parsed as data.

The entry stack directory is the candidate root. Absolute, remote, home-relative, and interpolating `extends` paths are rejected. Escapes from the root, symbolic links, cycles, non-regular files, missing parents, multiple YAML documents, duplicate keys, and exceeded byte/depth limits fail the whole request. There is no partial child-only result. A candidate `extends` path must not open the selected policy file.

Each permitted file is captured once into bounded immutable bytes and evaluated from those bytes. That is not an atomic snapshot of a concurrently mutating directory. Authoritative CI should pin a revision-backed input tree.

Inheritance follows existing child-wins whole-server and whole-resource merge, plus whole-block top-level `gateway` inheritance when the child omits it. There is no security-specific deep merge. Dynamic names that make membership or override collisions uncertain make the relevant rules indeterminate.

## Rules

Applicability is per subject. A mixed stack may pass a narrowly scoped rule when excluded subjects stay visible. An evaluation with no applicable checks is an error. There is no all-N/A bypass.

### `explicit-image-digests`

Every explicit MCP-server and supporting-resource `image` must be a syntactically valid literal digest reference. Tags and missing digests are violations. Dynamic expressions are unknown. Invalid declarations are input errors. Source-built and other non-image server kinds are not applicable, with a reason. Source-built exclusions are counted on success. No registry lookup or output-artifact inference is performed. A passing result does not claim that all artifacts are immutable.

### `deny-local-command-servers`

Rejects classified command-only local MCP execution. A command override on a container and `source.type: local` are not local process execution. A known SSH server passes this narrow prohibition and is not certified safe.

### `deny-ssh-servers`

Independently rejects declared SSH execution. URL and OpenAPI entries are not treated as SSH. Commands are not parsed to infer remote hosting.

### `schema-pinning-enabled`

Evaluates effective declared enablement using current runtime semantics: pinning defaults to enabled, global disablement wins, and `pin_schemas: true` does not install a verifier when global pinning is off. Per-server `false` disables that server. Unclear applicability does not pass.

### `schema-pinning-block`

Requires effective pinning enabled and action `block` for each applicable MCP server. It is meaningful without also selecting `schema-pinning-enabled`. Default `warn` does not satisfy block. Scan enablement and `scan_ignore` do not imply pin enforcement.

### `nonempty-server-tool-lists`

Requires a nonempty `tools` list of nonempty literal exact names for applicable MCP servers. Nil or empty lists fail. Reference-bearing names are unknown. Client, group, and OpenAPI operation-filter lists are not substitutes. Literal `*` and names containing `*` are exact names under current semantics, not allow-all globs. Those entries emit a `literal-tool-name-not-pattern` warning without echoing the value. The structural rule result is unchanged. Tool existence and call authorization remain unverified.

## Outcomes

Each result is `pass`, `violation`, `unknown`, or `not_applicable` with a reason code. Input errors are separate diagnostics. Applicable unknowns and unknown applicability prevent acceptance. Known violations stay distinct from unknowns even though both use exit `1`.

JSON fields include `accepted`, `evaluation_complete`, `status`, `warnings`, `coverage`, `results`, and `diagnostics`. `status` is `accepted`, `rejected`, `indeterminate`, or `error`.

Findings use generated source aliases (`input-0` is the entry stack) and structural paths such as `mcp-servers[1].image`. They do not include raw YAML, commands, URLs, secret or default values, parser excerpts, or arbitrary exception text. Operator-selected filenames and identifiers may themselves be sensitive; retained display is bounded. Candidate credential values are never hashed as evidence.

## CI

Treat the checker binary, workflow, and policy as independently trusted from candidate stack data. Do not build or execute the checker from untrusted pull-request code, run pull-request scripts or local actions, mount runtime sockets, or supply secrets to this check.

Do not use `pull_request_target` or `workflow_run` to execute untrusted candidates. Pin reviewed actions and checker artifacts, and use least permissions. Policy, workflow, and tool updates need independent review or protected sources.

A policy file in the same pull request is not automatically trusted. Copy or restore the reviewed policy from a protected ref if the workflow must not honor a PR-supplied policy.

Strict CI should accept exit `0` only. If warning exit `2` is deliberately allowed, fail on every other status; do not use `|| true` or unconditional `continue-on-error`. Report-only rollout can be a nonrequired workflow. There is no evaluator bypass flag.

This check does not prevent later edits, ordinary apply, REST changes, watcher reload, or external execution. Required-check and policy authority are workflow responsibilities, not properties of a report digest.

See [the example workflow](../examples/stack-declaration-policy/ci-workflow.yml).

## Related

- [CLI reference](cli-reference.md#stack-declaration-policy)
- [Configuration reference](config-schema.md)
- [Mutable reference diagnostics](cli-reference.md#mutable-reference-diagnostics) (ordinary validate, not this evaluator)
