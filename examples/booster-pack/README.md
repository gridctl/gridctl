# Booster pack example

A fragment pack. It does not start its own gateway. `gridctl pack apply booster-pack` composes `servers.yaml` onto the `network-eng` pack's pinned stack and starts, replaces, or reloads that base daemon. Add `network-eng` first. Leave `wiring` off: the base pack owns the gateway entry.

```bash
gridctl pack add <network-eng-repo>
gridctl pack apply network-eng
gridctl pack add <this-repo-url>
gridctl pack apply booster-pack
```

The composed file is `~/.gridctl/packs/network-eng/composed/stack.yaml`. A later apply with no fragment change is unchanged. `pack remove booster-pack` reloads the base, or replaces it back onto the pinned stack when this was the last booster.
