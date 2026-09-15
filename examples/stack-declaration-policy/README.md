# Stack declaration policy

Offline checks for captured stack declarations. This is not runtime admission and does not replace `gridctl validate` without `--policy`.

```bash
gridctl validate examples/stack-declaration-policy/stack.yaml \
  --policy examples/stack-declaration-policy/policy.yaml
```

`stack.yaml` is the candidate. `policy.yaml` is the operator-selected rule list. In CI, treat the checker binary, workflow, and policy as independently trusted from pull-request YAML. `ci-workflow.yml` is a reusable-workflow sketch: pin the invocation and `actions/checkout` SHAs, restore policy from a protected revision, and verify the checker digest in a directory outside the candidate tree. It is not a required repository check.

Acceptance means the captured declarations satisfy the selected requirements. It does not authorize later apply, REST edits, reload, or execution. Source-built servers, if added, stay excluded from digest claims rather than counting as immutable artifacts.

See [stack declaration policy](../../docs/stack-declaration-policy.md).
