# Anthropic Skills Compatibility Fixture

`brand-guidelines/` is a vendored copy of a real prompt-only skill from
[anthropics/skills](https://github.com/anthropics/skills), used by
`anthropic_skill_compat_test.go` to assert that gridctl's registry walker
loads, validates, and surfaces the body of an upstream agentskills.io skill
without modification. `writing-notes/` is not from that repository.

| File | Source | License |
|------|--------|---------|
| `brand-guidelines/SKILL.md` | `skills/brand-guidelines/SKILL.md` | Apache-2.0 (see `LICENSE.txt`) |
| `brand-guidelines/LICENSE.txt` | `skills/brand-guidelines/LICENSE.txt` | Apache-2.0 |
| `writing-notes/SKILL.md` | local gridctl fixture for `conformance_test.go` | repository license |

`brand-guidelines/` is checked in verbatim. Do not modify it. Refresh it by
re-vending from upstream when the agentskills.io spec evolves.
`writing-notes/SKILL.md` is a local fixture. The conformance test copies it
and sets `state: active` so the fleet daemon has a second active registry skill.
