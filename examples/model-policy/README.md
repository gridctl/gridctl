# Model routing policy

This example pairs a `gridctl models` policy with the human-owned LiteLLM
config it targets. The policy declares which backend serves which complexity
tier; gridctl renders the auto-router fragment and wires everything up:

```bash
mkdir -p ~/.gridctl/models ~/.litellm
cp litellm-config.yaml ~/.litellm/config.yaml
cp policy.yaml ~/.gridctl/models/policy.yaml

gridctl models validate
gridctl models sync
# restart LiteLLM (sync prints the exact hint), then:
gridctl models ack-restart
gridctl models status
```

| File | Description |
|---|---|
| `policy.yaml` | The routing policy: backends by reference, four tiers, weights tuned for coding agents, and the OpenCode client wiring. |
| `litellm-config.yaml` | A human-owned LiteLLM config the way it looks before the first sync: its own `model_list`, `router_settings`, and comments. Sync adds one `include:` line and the `gridctl-models.yaml` fragment next to it; everything else survives byte-for-byte. |

Two things this example demonstrates on purpose:

- Backends live once, in `litellm-config.yaml`'s `model_list`. The policy
  references them by name and the rendered fragment defines only the router;
  a backend copied into the fragment would silently load-balance against the
  original (LiteLLM's `include:` extends `model_list` across files).
- `router_settings` and fallbacks stay in the parent config. An included
  fragment silently replaces top-level maps, so `gridctl models validate`
  refuses them anywhere near the policy.

If you already run LiteLLM, skip the copies and scaffold from reality:

```bash
gridctl models init --from-litellm ~/.litellm/config.yaml
```

See [docs/model-policy.md](../../docs/model-policy.md) for the full schema,
the ownership model, and the restart contract.
