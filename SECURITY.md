# Security Policy

This document describes how to report security vulnerabilities in gridctl and how the project handles disclosure. If you have found a security issue, follow the process below. **Do not open a public GitHub issue** — not through the issue templates and not through any other public channel.

## Reporting a Vulnerability

Use GitHub's private vulnerability reporting feature:

1. Go to the [Security tab](https://github.com/gridctl/gridctl/security) of this repository
2. Click **"Report a vulnerability"**
3. Fill in the report form with as much detail as possible

Include in your report:

- A description of the vulnerability and its potential impact
- Steps to reproduce or a proof-of-concept (where possible)
- The version of gridctl affected
- Any relevant configuration or environment details

The issue template system is for bugs and feature requests only — it is not appropriate for security reports. Do not open a public issue or pull request disclosing the vulnerability.

## Response Timeline

- **Initial acknowledgment**: within 14 days of submission
- **Fix timeline**: best effort; severity and complexity determine the timeline

## Disclosure Policy

When a vulnerability is confirmed:

1. A fix is prepared on a private branch
2. The fix is released as a patch version
3. A GitHub Security Advisory is published after the patch is available

Coordinated disclosure is preferred. Reporters are credited in the advisory unless they request anonymity.

## Security Design

Gridctl is built with security as a constitutional principle. [Article XII](CONSTITUTION.md#article-xii--secure-defaults) requires that all security-sensitive configuration defaults to the most restrictive safe option. [Article XIII](CONSTITUTION.md#article-xiii--minimal-attack-surface) requires that gridctl not expose functionality beyond its stated purpose and that every network-facing endpoint has a documented purpose and ownership. These commitments apply to every release.
