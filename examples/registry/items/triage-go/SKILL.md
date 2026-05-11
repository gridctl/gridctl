---
name: triage-go
description: Classify an incident description against a severity matrix and return the first runbook step (Go handler)
tags:
  - sre
  - incident-response
  - typed-skill
state: active
---

# Incident Triage (Go)

The Go-flavored counterpart to `triage-ts`. Same severity matrix, same
contract, different runtime — built as a Go plugin via
`gridctl agent build triage-go` and loaded by the gateway at start.

## Severity matrix

- **sev1** — customer-facing outage, error budget burn ≥10x, or data loss path. Page primary + secondary, open the war room.
- **sev2** — significant degradation: elevated latency, partial outage, single-region failure, or one canary class fully broken. Page primary, notify stakeholders.
- **sev3** — bounded impact, workaround exists, or non-customer-facing breakage. Acknowledge, file a ticket, fix during business hours.
- **sev4** — internal-only, no user impact, or self-healing. Log it; no page.

## Decision rules

1. Customer impact named in the description → at least **sev2**; whole-user-base blast radius → **sev1**.
2. Single replica, single host, or self-healing condition → **sev3**.
3. When in doubt between adjacent severities, pick the higher one.
4. The `next_action` field MUST name a command, runbook section, or tool. "Investigate" is not an action.

## Output

`severity` (sev1 / sev2 / sev3 / sev4), `next_action` (one sentence), `rationale` (one sentence).
