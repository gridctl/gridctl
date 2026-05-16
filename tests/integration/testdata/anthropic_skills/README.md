# Anthropic Skills Compatibility Fixture

Vendored copy of a real prompt-only skill from
[anthropics/skills](https://github.com/anthropics/skills) used by
`anthropic_skill_compat_test.go` to assert that gridctl's registry walker
loads, validates, and surfaces the body of an upstream agentskills.io skill
without modification.

| File | Source | License |
|------|--------|---------|
| `brand-guidelines/SKILL.md` | `skills/brand-guidelines/SKILL.md` | Apache-2.0 (see `LICENSE.txt`) |
| `brand-guidelines/LICENSE.txt` | `skills/brand-guidelines/LICENSE.txt` | Apache-2.0 |

The fixture is checked in verbatim - do not modify it. Refresh by re-vending
from upstream when the agentskills.io spec evolves.
