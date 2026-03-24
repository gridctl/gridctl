# Gridctl Constitution

This document contains the immutable governance articles for the gridctl project. These articles are **non-negotiable** and **not subject to per-feature override**. They apply to every commit, PR, and release regardless of urgency, scope, or contributor preference. Where AGENTS.md provides mutable architectural guidance that evolves with the project, this document does not change without an explicit constitutional amendment (a dedicated PR that modifies only this file, reviewed by a maintainer).

---

## Article I — Library-First Architecture

Gridctl MUST prefer the Go standard library over external dependencies. A new external dependency MUST NOT be introduced when the standard library can achieve the same result with reasonable code. Every external dependency is a supply-chain risk and a maintenance burden; its introduction requires explicit justification in the PR that adds it.

## Article II — Dependency Minimalism

External dependencies MUST be justified by functionality that the standard library demonstrably cannot provide. Dependencies MUST NOT be added for convenience, style preference, or to reduce line count when equivalent stdlib patterns exist. Transitive dependencies count toward this obligation.

## Article III — Test-First Development

All exported functions and methods MUST have corresponding tests before a PR is merged. Tests are not optional documentation — they are the contract. A feature without tests is not complete; a bug fix without a regression test is not fixed.

## Article IV — No Mocks in Integration Tests

Integration tests MUST exercise real dependencies — real Docker clients, real containers, real network connections. Mocks MUST NOT be used in `tests/integration/`. A passing integration test suite with mocked dependencies provides false assurance. Unit tests may use mocks; integration tests MUST NOT. All integration tests MUST run with the `-race` flag; a test suite that passes without race detection is not a complete integration test.

## Article V — Error Propagation, Not Panic

Library code MUST return errors to callers. `panic` MUST NOT be used in library code (`pkg/`, `internal/`). Panics terminate the process without giving callers a chance to handle failures gracefully. CLI entry points (`cmd/`) may panic only during initialization, before any user-visible operation begins.

## Article VI — Context Propagation

Any function that performs I/O, blocks on a channel, or calls an external service MUST accept a `context.Context` as its first parameter. Cancellation and deadline propagation are not optional. Functions MUST NOT ignore a cancelled context.

## Article VII — No Secrets in Version Control

API keys, passwords, tokens, private keys, and any credential MUST NOT be committed to the repository under any circumstances — not in code, not in comments, not in example files, not in tests. Use environment variable references (e.g., `${ENV_VAR}`, `${vault:KEY}`) in all configuration examples.

## Article VIII — Semantic Versioning

The public CLI interface and stack YAML schema are versioned artifacts. Breaking changes MUST NOT be introduced in patch or minor releases. A breaking change requires a major version increment and MUST be announced in CHANGELOG.md before release. "Breaking" means any change that causes a previously valid invocation or stack file to fail or produce different output.

## Article IX — Stack YAML Backward Compatibility

Every new field added to the stack YAML schema MUST be optional with a documented default that preserves existing behavior. A stack file valid against version N MUST remain valid and produce equivalent behavior under version N+1 unless the major version has been incremented. Removing or renaming a field is a breaking change.

## Article X — Machine-Parseable CLI Output

All gridctl commands that produce structured data MUST support machine-readable output (JSON by default, or a documented format flag). Human-readable formatting MUST NOT be the only output mode for data that scripts or other tools may consume. Exit codes MUST be meaningful: 0 for success, non-zero for failure, consistently across all commands.

## Article XI — No Silent Failures

Operations MUST NOT silently discard errors. If an error cannot be handled, it MUST be logged and, where appropriate, propagated to the caller or surfaced to the user. A function that swallows an error without documentation of why it is safe to do so violates this article.

## Article XII — Secure Defaults

Security-sensitive configuration MUST default to the most restrictive safe option. The vault MUST be locked by default. Authentication MUST be opt-out, not opt-in. CORS MUST default to explicit allowlists in production paths, not wildcards. Features that bypass security controls MUST require explicit opt-in flags.

## Article XIII — Minimal Attack Surface

Gridctl MUST NOT expose functionality beyond what is required for its stated purpose. Internal APIs MUST NOT be reachable from external networks without explicit configuration. Every network-facing endpoint MUST have a documented purpose and ownership.

## Article XIV — Structured Logging

All log output MUST use `log/slog` with structured fields. `fmt.Println` and `log.Printf` MUST NOT be used in library code. Log levels MUST be meaningful: DEBUG for internal state useful to developers, INFO for user-visible operational events, WARN for recoverable anomalies, ERROR for failures requiring attention.

## Article XV — Changelog Discipline

Every user-visible change — new feature, behavior change, deprecation, or bug fix — MUST be recorded in CHANGELOG.md before the PR is merged. Changelog entries belong in the `[Unreleased]` section and MUST follow the Keep a Changelog format. A PR that omits its changelog entry is not complete.
