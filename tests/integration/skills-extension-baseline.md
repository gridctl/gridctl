# Skills extension compatibility baseline

The registry implementation has these accepted compatibility differences from
SEP-2640. They are documented behavior, not suppressed conformance failures.

| Baseline entry | Difference | Reason |
|---|---|---|
| `registry-nested-uri` | A nested skill is published at `skill://registry/<child>/SKILL.md`, rather than below its parent's URI prefix. | Registry names are independent identities. Hosts must use the URI returned by `skills/list`. |
| `registry-nested-manifest` | A parent's manifest and directory listing exclude a nested skill's files, although the extension specifies their inclusion. | The served file set matches the existing skill-pin boundary. The child's own manifest covers its files. |
| `registry-legacy-resource` | `resources/list` retains `skills://registry/<name>` beside the new `skill://registry/<name>/SKILL.md` entry. | Existing clients keep their documented resource URI. |

On October 20, 2026, `@modelcontextprotocol/conformance@0.2.0-alpha.12` passed
`sep-2640-skills-enumeration` (32 checks), `sep-2640-skills-manifest` (six checks),
and `sep-2640-skills-directory` (seven checks) against a real race-built daemon
with an active registry skill and a supporting directory. No expected-failure
suppression was used. That fixture includes the legacy resource entry; nested
skill boundaries have separate provider tests.

The canonical document fetched from `resources/read` passed `skills-ref validate`
from `agentskills/agentskills` revision
`69ef37e9424c0a7ea9dd2293b559e43ec8176379`. This validates the served acceptance
fixture, not every document an operator may import.
