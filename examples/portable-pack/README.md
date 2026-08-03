# Portable pack example

A minimal pack repo: skills, agents, rule fragments, and a `gridctl-pack.yaml` manifest that imports and applies them as one unit.

```bash
gridctl pack add <this-repo-url>
gridctl pack apply network-eng
gridctl pack status
gridctl pack remove network-eng
```

The `rules:` selection is opt-in (an empty list means none). Installing a pack with rules activates the global-context fragments mode if it is not already on; an existing canonical AGENTS.md is migrated to `fragments/00-default.md` with a backup and an explicit message. Removal retracts only resources tagged with this pack's name; your own skills, agents, and fragments are never touched.
