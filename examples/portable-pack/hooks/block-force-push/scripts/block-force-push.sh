#!/bin/sh
# Deny git push --force. Claude Code runs this script; gridctl never does.
input=$(cat)
case "$input" in
  *'git push --force'*|*'git push -f'*)
    printf '%s\n' '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"git push --force is blocked by the network-eng pack"}}'
    ;;
esac
exit 0
