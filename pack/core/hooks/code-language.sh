#!/bin/bash
# PostToolUse hook: code must be written in English (#26).
# Matcher: Edit|Write, filters itself on .py, .sh, .bash and .go. Blocking:
# exit 2 + stderr. bash-write-gates calls this same script for files a Bash
# call wrote.
#
# Counterpart: the AGENTS.md rule on the language of code carries norm and
# rationale up front; this hook checks the written file afterwards. Dutch
# comments, docstrings and names are the findings; string literals are not
# read. A line that is deliberately not English gets `lighthouse: allow-language`.
#
# Off without `[project] code_language` in the lighthouse.toml of the git root
# of the written file: the config is read from that root, not from the cwd, so
# a file in another repo is judged by that repo. No git repository, or no
# lighthouse.toml, is off as well.
#
# NOT fail-open: without jq, git or python3, with a payload that is not JSON,
# a path that cannot be resolved, a git that breaks, or a checker that fails
# (a file with a syntax error, bad config, not UTF-8), nothing was checked; a
# silent exit 0 would read as "the code is English". Every such exit is exit 2
# with `code-language: COULD NOT CHECK — <reason>` on the first line (the token
# bash_writes.classify reads, besluit 18).

cannot_check() {
  {
    echo "code-language: COULD NOT CHECK — $1"
    echo ""
    echo "This hook checks that code is written in English."
    echo "Nothing was checked; that is not a green light."
    if [ -n "${2:-}" ]; then
      echo ""
      echo "$2"
    fi
  } >&2
  exit 2
}

MISSING_TOOLS=""
command -v jq >/dev/null 2>&1 || MISSING_TOOLS="$MISSING_TOOLS jq"
command -v git >/dev/null 2>&1 || MISSING_TOOLS="$MISSING_TOOLS git"
command -v python3 >/dev/null 2>&1 || MISSING_TOOLS="$MISSING_TOOLS python3"
if [ -n "$MISSING_TOOLS" ]; then
  cannot_check "missing tooling:$MISSING_TOOLS"
fi

# `realpath -m` only exists in GNU coreutils; the BSD realpath on macOS
# rejects the flag.
canon_path() {
  realpath -m "$1" 2>/dev/null && return 0
  python3 -c 'import os, sys; print(os.path.realpath(sys.argv[1]))' "$1" 2>/dev/null
}

INPUT=$(cat)
if ! FILE_PATH=$(printf '%s' "$INPUT" | jq -r '.tool_input.file_path // empty' 2>/dev/null); then
  cannot_check "the payload is not valid JSON."
fi
if [ -z "$FILE_PATH" ]; then
  exit 0
fi

case "$FILE_PATH" in
  *.py|*.sh|*.bash|*.go) ;;
  *) exit 0 ;;
esac

CWD=$(printf '%s' "$INPUT" | jq -r '.cwd // empty')
case "$FILE_PATH" in
  /*) ABS="$FILE_PATH" ;;
  *)  ABS="${CWD:-$PWD}/$FILE_PATH" ;;
esac
CANON=$(canon_path "$ABS") || CANON=""
if [ -z "$CANON" ]; then
  cannot_check "cannot resolve '$ABS' (no 'realpath -m' and no python3)."
fi
ABS="$CANON"
if [ ! -f "$ABS" ]; then
  exit 0
fi

# "Not a git repository" is a real outcome and stays silent; any other git
# failure is not. LC_ALL=C: git translates the text matched here.
DIR=$(dirname "$ABS")
if ! ROOT=$(LC_ALL=C git -C "$DIR" rev-parse --show-toplevel 2>&1); then
  case "$ROOT" in
    *"not a git repository"*) exit 0 ;;
  esac
  cannot_check "git could not read '$DIR'." "$ROOT"
fi
if [ -z "$ROOT" ]; then
  cannot_check "git gave no repository root for '$DIR'."
fi

# The checker lives in the lighthouse lib of this repo. Found through
# LIGHTHOUSE_REPO_ROOT or git, never from this file's own path.
LIB_ROOT="${LIGHTHOUSE_REPO_ROOT:-$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null)}"
STDERR_FILE=$(mktemp) || cannot_check "cannot create a temporary file."
trap 'rm -f "$STDERR_FILE"' EXIT
OUTPUT=$(python3 "$LIB_ROOT/.lighthouse/lib/lighthouse_lib/code_language.py" "$ABS" "$ROOT" 2>"$STDERR_FILE")
RC=$?
case "$RC" in
  0) exit 0 ;;
  1)
    printf '%s\n' "$OUTPUT" >&2
    exit 2
    ;;
  *)
    DETAIL=$(cat "$STDERR_FILE")
    # The checker's own stderr already starts with the token; pass it on as is.
    case "$DETAIL" in
      "code-language: COULD NOT CHECK — "*)
        printf '%s\n' "$DETAIL" >&2
        exit 2
        ;;
    esac
    cannot_check "the language checker failed (exit $RC)." "$DETAIL"
    ;;
esac
