# Gridctl Constitution

This document contains the immutable governance articles for the gridctl project. These articles are **non-negotiable** and **not subject to per-feature override**. They apply to every commit, PR, and release regardless of urgency, scope, or contributor preference. This document does not change without an explicit constitutional amendment (a dedicated PR that modifies only this file, reviewed by a maintainer).

---

## Article I - Library-First Architecture

Gridctl prefers mature, permissively-licensed (Apache 2.0 or MIT) Go libraries over bespoke implementations for foundational concerns where the alternative is reinventing a graph runtime, sandbox, or schema validator. The standard library remains the default for incidental utilities. Justification for each new top-level dependency still belongs in the PR that adds it.

## Article II - Dependency Minimalism

External dependencies MUST be justified by functionality, maturity, or correctness gains the project would otherwise have to reproduce. Transitive footprint and license posture remain part of the review criteria.

## Article III - Test-First Development

Every exported behavior MUST be covered by a test before a PR is merged: a new feature ships with tests that exercise its documented behavior, a behavior change updates them, and a bug fix includes a regression test that fails without the fix. Tests are not optional documentation - they are the contract. A trivial accessor, constructor, or pass-through does not need its own test when the behavior it exposes is already covered. A feature without tests is not complete; a bug fix without a regression test is not fixed.

## Article IV - No Mocks in Integration Tests

Integration tests MUST exercise real dependencies - real Docker clients, real containers, real network connections. Mocks MUST NOT be used in `tests/integration/`. A passing integration test suite with mocked dependencies provides false assurance. Unit tests may use mocks; integration tests MUST NOT. All integration tests MUST run with the `-race` flag; a test suite that passes without race detection is not a complete integration test.

A real process or HTTP server that stands in for an external service - a mock MCP server, a stub secret-provider command, a local OAuth issuer - is not a mock under this article when the real service cannot run in CI, provided the transport, protocol, and process boundary gridctl exercises are real. Faking gridctl's own clients, transports, runtime interfaces, or filesystem is. Where a stand-in is used, the PR MUST name the acceptance against the real service that remains manual or scheduled.

## Article V - Error Propagation, Not Panic

Library code MUST return errors to callers. `panic` MUST NOT be used in library code (`pkg/`, `internal/`). Panics terminate the process without giving callers a chance to handle failures gracefully. CLI entry points (`cmd/`) may panic only during initialization, before any user-visible operation begins.

## Article VI - Context Propagation

Any function that performs I/O, blocks on a channel, or calls an external service MUST accept a `context.Context` as its first parameter. Cancellation and deadline propagation are not optional. Functions MUST NOT ignore a cancelled context.

## Article VII - No Secrets in Version Control

API keys, passwords, tokens, private keys, and any credential MUST NOT be committed to the repository under any circumstances - not in code, not in comments, not in example files, not in tests. Use environment variable references (e.g., `${ENV_VAR}`, `${var:KEY}`) in all configuration examples.

## Article VIII - Semantic Versioning

The public CLI interface, the stack YAML schema, and the MCP wire protocol are versioned artifacts. Breaking changes MUST NOT be introduced in patch or minor releases. A breaking change requires a major version increment and MUST be announced in CHANGELOG.md before release.

"Breaking" means a change that causes a previously valid stack file or documented invocation to fail, or that changes a machine-readable contract: the stack YAML schema; `--format json` and other documented structured output; exit codes; REST request and response shapes; MCP wire behavior; and the documented formats of files gridctl writes for other programs to read, such as exports, lockfiles, and projected client entries. Human-readable text output, log wording, diagnostic phrasing, and the web UI are not versioned contracts and MAY change in any release with a changelog entry. A correction that brings behavior in line with its existing documentation is a bug fix, not a breaking change, and its changelog entry MUST say so.

During a release-candidate series for a major version, a change that would otherwise require that major version ships in the next candidate with a changelog entry; the candidate series is the announcement window.

Dropping support for a protocol generation the gateway previously accepted is a breaking change under this article. Which generations are supported at a given time is defined by the project's protocol support policy, not this document.

## Article IX - Stack YAML Backward Compatibility

Every new field added to the `stack.yaml` schema (MCP servers, transports, networks, variables, pins, clients, groups, limits, link, skills, experimental) MUST be optional with a documented default that preserves existing behavior. A stack file valid against version N MUST remain valid and produce equivalent behavior under version N+1 unless the major version has been incremented. Removing or renaming a field is a breaking change. This guarantee covers the `stack.yaml` schema only - skill definition formats are not in scope. The `workflow:` block in `SKILL.md` is no longer supported.

## Article X - Machine-Parseable CLI Output

All gridctl commands that produce structured data MUST support machine-readable output (JSON by default, or a documented format flag). Human-readable formatting MUST NOT be the only output mode for data that scripts or other tools may consume. Exit codes MUST be meaningful: 0 for success, non-zero for failure, consistently across all commands.

## Article XI - No Silent Failures

Operations MUST NOT silently discard errors. If an error cannot be handled, it MUST be logged and, where appropriate, propagated to the caller or surfaced to the user. A function that swallows an error without documentation of why it is safe to do so violates this article.

## Article XII - Secure Defaults

Security-sensitive configuration MUST default to the most restrictive safe option. The gateway MUST bind to loopback by default. A listener reachable beyond loopback MUST require authentication and an explicit cross-origin allowlist, and the gateway MUST refuse to start on such a listener without them unless an explicitly named insecure override is set. Loopback without configured authentication is the documented local default and is not a production path. An encrypted variable store MUST load locked; documentation MUST recommend encryption for any store that holds credentials. Features that bypass security controls MUST require explicit opt-in flags.

## Article XIII - Minimal Attack Surface

Gridctl MUST NOT expose functionality beyond what is required for its stated purpose. Internal APIs MUST NOT be reachable from external networks without explicit configuration. Every network-facing endpoint MUST have a documented purpose and ownership.

## Article XIV - Structured Logging

All log output MUST use `log/slog` with structured fields. `fmt.Println` and `log.Printf` MUST NOT be used in library code. Log levels MUST be meaningful: DEBUG for internal state useful to developers, INFO for user-visible operational events, WARN for recoverable anomalies, ERROR for failures requiring attention.

## Article XV - Changelog Discipline

Every user-visible change - new feature, behavior change, deprecation, or bug fix - MUST be recorded in CHANGELOG.md before the PR is merged. Changelog entries belong in the `[Unreleased]` section and MUST follow the Keep a Changelog format. A PR that omits its changelog entry is not complete.

## Article XVI - Ownership Before Mutation

Gridctl writes into files and directories owned by the user's clients: skill directories, agent context files, and client configuration. Every such destination MUST be claimed before it is changed. Gridctl MUST NOT create, modify, or delete a destination unless a lockfile records gridctl's ownership of that file, directory, region, or key, or the user explicitly forces the operation. A forced write to an unmanaged destination MUST take a backup first. Ownership MUST be recorded, never inferred; heuristic recognition of gridctl-shaped entries may assist one-time migration of pre-lockfile state only. Automatic reconciliation MUST NOT force-overwrite drift: a destination the user has hand-edited is changed only by an explicit, user-invoked operation. Removal MUST delete only artifacts gridctl created; in shared files, removal touches only the gridctl-owned region or key and MUST NOT rewrite sibling content. Credential material MUST NOT be written to any destination gridctl manages on the user's behalf, extending Article VII from the repository to every projected path.

## Article XVII - Versioned State Files

Every persistent state file gridctl owns - lockfiles, the pin store, manifests, and state snapshots - MUST carry an explicit schema version. Readers MUST refuse a file with a newer schema version with a clear error rather than guess at its meaning. Writers MUST migrate older versions forward explicitly; migration MUST be idempotent and MUST NOT discard data it does not understand.
