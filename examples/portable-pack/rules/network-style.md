---
description: Network engineering conventions
paths:
  - "**/*.yaml"
  - "**/*.yml"
---

# Network engineering conventions

- Name devices `<site>-<role>-<index>` (for example `sfo-leaf-01`).
- Every config change references a ticket in its commit message.
- Prefer declarative intent files over imperative CLI snippets in runbooks.
