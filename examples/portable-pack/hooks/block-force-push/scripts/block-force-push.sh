#!/bin/sh
# Deny git push --force and git push -f, including when the flag follows
# the remote. --force-with-lease and --force-if-includes are not denied.
# The client runs this script; gridctl never does.
#
# The deny follows the portable recipe in docs/hooks.md: one JSON object
# on stdout carrying both the flat decision/reason pair and the nested
# hookSpecificOutput block, the reason on stderr, and exit 2. A call that
# is not denied exits 0 with no output, because some clients deny on any
# non-zero exit.
input=$(cat)

rest=$input
while :; do
  case "$rest" in
    *'--force-with-lease'*)
      rest=${rest%%--force-with-lease*}${rest#*--force-with-lease}
      ;;
    *'--force-if-includes'*)
      rest=${rest%%--force-if-includes*}${rest#*--force-if-includes}
      ;;
    *)
      break
      ;;
  esac
done

reason='git push --force is blocked by the network-eng pack'
case "$rest" in
  *'git push'*'--force'*|*'git push'*' -f'*)
    printf '{"decision":"deny","reason":"%s","hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"%s"}}\n' "$reason" "$reason"
    printf '%s\n' "$reason" >&2
    exit 2
    ;;
esac
exit 0
