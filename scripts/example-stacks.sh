# Shared list of runnable example stacks. Source this file; do not execute it.
#
#   example_stacks <root>
#
# Prints the sorted absolute paths of runnable example stacks under
# <root>/examples. Callers strip "<root>/" for display. Used by
# scripts/validate-examples.sh and scripts/check-example-refs.sh.
#
# Add an exclusion here when a new YAML file under examples/ is not a stack.
#
# skills.yaml is a skill-source list and gridctl-pack.yaml a pack manifest
# (different schemas); neither is a stack.
#
# gateway-remote.yaml is excluded because it is the one example that
# requires a secret: a non-loopback bind refuses to start without
# gateway.auth.token, and the variable it references cannot exist in CI, so
# validation reads the unset value as a missing required field. Giving it a
# default would ship a working credential in a public repo, which is worse
# than not validating the file.
#
# examples/stack-declaration-policy/policy.yaml is a declaration-policy
# document (different schema), not a stack. stack.yaml in that directory
# remains validated.
#
# examples/booster-pack/servers.yaml is a stack fragment, not a runnable
# stack. Fragments cannot declare name or network, so stack validation
# rejects the file on purpose. gridctl-pack.yaml in that directory remains
# excluded by name.
#
# examples/tracing/otel-collector-config.yaml is a collector config, not a
# stack. otel-collector.yaml in that directory remains validated.
#
# Path exclusions carry the "$root/" prefix because find prints paths under
# the root it was given; a root-relative pattern would never match.

example_stacks() {
  local root="$1"
  find "$root/examples" -name "*.yaml" \
    -not -name "skills.yaml" \
    -not -name "gridctl-pack.yaml" \
    -not -name "gateway-remote.yaml" \
    -not -path "$root/examples/stack-declaration-policy/policy.yaml" \
    -not -path "$root/examples/booster-pack/servers.yaml" \
    -not -path "$root/examples/tracing/otel-collector-config.yaml" | sort
}
