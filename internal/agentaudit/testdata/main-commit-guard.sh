#!/usr/bin/env bash
# A project's own guard, written after the description in issue #47: it refuses a
# git commit straight onto main, so work happens on a branch.
input=$(cat)
command=$(printf '%s' "$input" | jq -r '.tool_input.command // empty')
case "$command" in
  *"git commit"*) ;;
  *) exit 0 ;;
esac
branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null)
if [ "$branch" = "main" ]; then
  echo "Refused: no commits on main. Create a branch first." >&2
  exit 2
fi
exit 0
