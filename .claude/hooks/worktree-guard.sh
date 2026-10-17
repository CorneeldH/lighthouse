#!/bin/bash
# Bootstrap copy of personal_ai_agent@231f6e84 .claude/hooks/worktree-guard.sh; lighthouse init replaces it in M0 (b).
# PreToolUse hook: warn at a write into the main checkout while it is on `main`.
# Matcher: Edit|Write. NOT blocking: exit 0 + {"systemMessage": ...} on stdout.
#
# Counterpart: the worktree rule in AGENTS.md carries the norm and its
# rationale up front; this hook is the reminder at the moment of the call.
#
# Warning on purpose: read-only work and an explicit "on main" from the user
# are legitimate, and a hook cannot tell them apart. The hard line is drawn
# by main-commit-guard.sh, which covers the commit moment. This hook is the
# early signal — it fires at the first edit, not at the commit.
#
# Fails loud, not open (M0 spec, risk *Fail-open guards*): the paia original
# exited 0 without a word when jq was missing, so a fresh machine got no
# reminder and no sign that there was none. A guard that cannot look now
# says COULD NOT CHECK. Still exit 0: a warning hook never blocks.

warn_uncheckable() {
  MSG="Worktree guard: COULD NOT CHECK — $1

Nothing was checked, so this is no sign that the write is in a worktree.
Convention: work that produces a commit starts in a worktree on its own branch
(git worktree add .claude/worktrees/<name> -b issue-<nr>-<slug>)."
  jq -cn --arg m "$MSG" '{systemMessage: $m}'
  exit 0
}

MISSING_TOOLS=""
command -v jq >/dev/null 2>&1 || MISSING_TOOLS="$MISSING_TOOLS jq"
command -v git >/dev/null 2>&1 || MISSING_TOOLS="$MISSING_TOOLS git"
command -v cat >/dev/null 2>&1 || MISSING_TOOLS="$MISSING_TOOLS cat"
if [ -n "$MISSING_TOOLS" ]; then
  # No jq to escape with: the only variable part is a list of plain tool names.
  printf '{"systemMessage":"Worktree guard: COULD NOT CHECK — missing tooling:%s. Nothing was checked, so this is no sign that the write is in a worktree."}\n' "$MISSING_TOOLS"
  exit 0
fi

# `realpath -m` only exists in GNU coreutils; the BSD realpath on macOS
# rejects the flag. The flag is needed because the target of a Write does
# not have to exist yet.
canon_path() {
  realpath -m "$1" 2>/dev/null && return 0
  python3 -c 'import os, sys; print(os.path.realpath(sys.argv[1]))' "$1" 2>/dev/null
}

INPUT=$(cat)
if ! FILE_PATH=$(printf '%s' "$INPUT" | jq -r '.tool_input.file_path // empty' 2>/dev/null); then
  warn_uncheckable "the payload is not valid JSON."
fi

if [ -z "$FILE_PATH" ]; then
  exit 0
fi

CWD=$(printf '%s' "$INPUT" | jq -r '.cwd // empty')
case "$FILE_PATH" in
  /*) ABS="$FILE_PATH" ;;
  *)  ABS="${CWD:-$PWD}/$FILE_PATH" ;;
esac
CANON=$(canon_path "$ABS") || CANON=""
if [ -z "$CANON" ]; then
  warn_uncheckable "cannot resolve '$ABS' (no 'realpath -m' and no python3)."
fi
ABS="$CANON"

# Check the TARGET FILE, not the cwd. The documented failure is a subagent
# with a worktree cwd editing an absolute path in the main checkout.
DIR=$(dirname "$ABS")
while [ ! -d "$DIR" ] && [ "$DIR" != "/" ]; do
  DIR=$(dirname "$DIR")
done

# A linked worktree has its own --git-dir but shares --git-common-dir.
# `cd`+`pwd -P` instead of --path-format=absolute: the latter needs git 2.31.
# Failing here means the target is not in a git repo: a real outcome, silent.
GIT_DIR=$(cd "$DIR" 2>/dev/null && cd "$(git rev-parse --git-dir 2>/dev/null)" 2>/dev/null && pwd -P) || exit 0
GIT_COMMON=$(cd "$DIR" 2>/dev/null && cd "$(git rev-parse --git-common-dir 2>/dev/null)" 2>/dev/null && pwd -P) || exit 0
if [ -z "$GIT_DIR" ] || [ -z "$GIT_COMMON" ]; then
  exit 0
fi
if [ "$GIT_DIR" != "$GIT_COMMON" ]; then
  exit 0
fi

BRANCH=$(git -C "$DIR" branch --show-current 2>/dev/null)
if [ "$BRANCH" != "main" ]; then
  exit 0
fi

MSG="Worktree guard: this writes into the main checkout while it is on 'main'.

  $ABS

Convention: work that produces a commit starts in a worktree on its own
branch (git worktree add .claude/worktrees/<name> -b issue-<nr>-<slug>).
Exception: read-only work, or an explicit 'on main' from the user.

Note: committing on main is blocked (main-commit-guard.sh). Move this work
now, not later."

jq -cn --arg m "$MSG" '{systemMessage: $m}'
exit 0
