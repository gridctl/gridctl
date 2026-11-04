#!/bin/sh
# Deny git push --force and git push -f, including when the flag follows
# the remote. --force-with-lease and --force-if-includes are not denied.
# Claude Code runs this script; gridctl never does.
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

case "$rest" in
  *'git push'*'--force'*|*'git push'*' -f'*)
    printf '%s\n' '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"git push --force is blocked by the network-eng pack"}}'
    ;;
esac
exit 0
