# Portable pack example

A minimal pack repo: skills, agents, a Claude Code hook, rule fragments, a local-process `stack.yaml`, and a `gridctl-pack.yaml` manifest that imports and applies them as one unit. `hooks/block-force-push` is a `PreToolUse` Bash hook that denies `git push --force` and `git push -f`, including when the flag follows the remote, by printing a `permissionDecision` of `deny`. It does not deny `--force-with-lease` or `--force-if-includes`. Hooks are opt-in and pack-repository only. `requires.gridctl` is `>=1.0.0-rc.5`, the release that projects hooks. `incident-triage` declares that hook under `metadata.requires.hooks`. gridctl stores, scans, and places the script. It does not run it. Claude Code does, after `pack apply` or `skill project sync --kind hook`. `pack apply` starts the stack from the pinned checkout before wiring clients. The stack command is a local placeholder so the example needs no image pin. It is not an MCP server. The manifest comments a `sources:` example that selects a skill from a public repository pinned by SHA. That block is not active. Uncomment it only after replacing the SHA with a commit you have reviewed. Token flags on `pack add` apply to this repository only.

```bash
gridctl pack add <this-repo-url>
gridctl pack apply network-eng
gridctl pack status
gridctl pack remove network-eng
```

## Lock file

`gridctl pack lock` writes `gridctl-pack.lock.yaml` next to the manifest. This example's `sources:` block is commented out, so the lock has no sources. The file becomes meaningful once sources are declared: two machines that add the same pack commit then record the same source commits. `gridctl pack lock --check` exits 1 when the lock is missing or the source set changed, and 0 when it matches.

The `rules:` selection is opt-in (an empty list means none). Installing a pack with rules activates the global-context fragments mode if it is not already on; an existing canonical AGENTS.md is migrated to `fragments/00-default.md` with a backup and an explicit message. Removal retracts only resources tagged with this pack's name; your own skills, agents, and fragments are never touched.
