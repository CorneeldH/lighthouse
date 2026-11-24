#!/usr/bin/env bash
# PostToolUse hook: PSScriptAnalyzer on every edited PowerShell script (.ps1, .psm1).
# Matcher: Edit|Write. Counterpart of pack/shell's shell-lint-check.
#
# Why this hook is NOT fail-open: skipping silently is itself the error the hook must
# prevent. Missing pwsh, a missing PSScriptAnalyzer module or a helper that does not answer
# = fail loudly (exit 2, first line `powershell-lint-check: COULD NOT CHECK — …`), never exit 0.
#
# The rules this hook blocks on. The hook passes them explicitly (-IncludeRule), so a
# PSScriptAnalyzerSettings.psd1 of the repository cannot switch one off: no floor check.
# A finding blocks as `[powershell-lint-check/<Rule>]`.
BLOCKING_RULES="PSAvoidUsingInvokeExpression,PSAvoidUsingPlainTextForPassword,PSAvoidUsingConvertToSecureStringWithPlainText,PSAvoidUsingEmptyCatchBlock,PSUseDeclaredVarsMoreThanAssignments"
set -uo pipefail

fail() { # reason...
  echo "powershell-lint-check: COULD NOT CHECK — $*" >&2
  exit 2
}

INPUT=$(cat)

# A payload without `.ps1"` or `.psm1"` cannot name a PowerShell file: nothing to check,
# with or without jq. This filter only skips; whatever it lets through is read by jq
# below, so a stray match costs at most a COULD NOT CHECK, never a missed file.
case "$INPUT" in
  *'.ps1"'*|*'.psm1"'*) ;;
  *) exit 0 ;;
esac

command -v jq >/dev/null 2>&1 || fail "jq is not present, so the edited path cannot be read."
FILE_PATH=$(printf '%s' "$INPUT" | jq -r '.tool_input.file_path // empty')

# No path in the payload: this tool call touched no file. Nothing to do.
[ -n "$FILE_PATH" ] || exit 0

case "$FILE_PATH" in
  *.ps1|*.psm1) ;;
  *) exit 0 ;;
esac

# A Windows payload names the file with backslashes, which dirname does not split: it would
# answer `.`, the hook's working directory (the main checkout). The forward-slash form finds
# the directory of the file; pwsh gets the payload path itself whenever that one exists.
SLASH_PATH="${FILE_PATH//\\//}"

# Deleted or renamed between the edit and the hook: nothing to check.
if [ ! -f "$FILE_PATH" ]; then
  [ -f "$SLASH_PATH" ] || exit 0
  FILE_PATH="$SLASH_PATH"
fi

# The lighthouse root: LIGHTHOUSE_REPO_ROOT, else the tree of the EDITED file, and only
# then that of the hook. $CLAUDE_PROJECT_DIR points to the main checkout, and an edit in a
# worktree would then be checked with the script of another branch.
FILE_DIR="$(dirname "$SLASH_PATH")"
FILE_ROOT="$(git -C "$FILE_DIR" rev-parse --show-toplevel 2>/dev/null)" || FILE_ROOT=""
candidates=()
if [ -n "${LIGHTHOUSE_REPO_ROOT:-}" ]; then
  candidates+=("$LIGHTHOUSE_REPO_ROOT")
else
  [ -z "$FILE_ROOT" ] || candidates+=("$FILE_ROOT")
  if hook_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null)"; then
    candidates+=("$hook_root")
  fi
fi

# The helper runs through `pwsh -File`: it needs to exist, not to be executable.
ROOT=""
searched=""
for root in ${candidates[@]+"${candidates[@]}"}; do
  searched="$searched $root"
  if [ -f "$root/.lighthouse/scripts/psscriptanalyzer-repo.ps1" ]; then
    ROOT="$root"
    break
  fi
done
if [ -z "$ROOT" ]; then
  fail "no .lighthouse/scripts/psscriptanalyzer-repo.ps1 found (searched:${searched:- no lighthouse root})."
fi
SCRIPT="$ROOT/.lighthouse/scripts/psscriptanalyzer-repo.ps1"

command -v pwsh >/dev/null 2>&1 || fail "pwsh is not on PATH, so PSScriptAnalyzer cannot run. Install PowerShell 7 and the PSScriptAnalyzer module."

# Every argument is its own word: a path with spaces or parentheses reaches pwsh whole.
# Payload paths are absolute, so none starts with `-` and none is read as a parameter name.
args=(-Path "$FILE_PATH" -Rules "$BLOCKING_RULES")
# A pinned module version (CI): the helper loads exactly that one or fails with rc 3.
[ -z "${LIGHTHOUSE_PSSA_VERSION:-}" ] || args+=(-Version "$LIGHTHOUSE_PSSA_VERSION")

ERR_FILE="$(mktemp)" || fail "mktemp failed."
out=$(pwsh -NoProfile -NonInteractive -File "$SCRIPT" "${args[@]}" 2>"$ERR_FILE")
rc=$?
err="$(cat "$ERR_FILE")"
rm -f "$ERR_FILE"

case "$rc" in
  0)
    exit 0
    ;;
  10)
    # Findings: each line is `Rule<TAB>Line<TAB>Message`. rc 10 without one such line is no
    # proof of a finding, and no block on it: COULD NOT CHECK.
    report=""
    tab=$'\t'
    while IFS= read -r line; do
      line="${line%$'\r'}"
      case "$line" in
        *"$tab"*"$tab"*) ;;
        *) continue ;;
      esac
      rule="${line%%"$tab"*}"
      rest="${line#*"$tab"}"
      lineno="${rest%%"$tab"*}"
      msg="${rest#*"$tab"}"
      [[ "$rule" =~ ^[A-Za-z0-9]+$ ]] || continue
      [[ "$lineno" =~ ^[0-9]+$ ]] || continue
      report="${report}[powershell-lint-check/$rule] $FILE_PATH:$lineno: $msg"$'\n'
    done <<EOF
$out
EOF
    [ -n "$report" ] || fail "psscriptanalyzer-repo.ps1 returned rc=10 without a readable finding:" "$err" "$out"
    printf '%s' "$report" >&2
    echo "" >&2
    echo "powershell-lint-check: fix this in the code. A suppression needs a reason:" >&2
    echo "[Diagnostics.CodeAnalysis.SuppressMessageAttribute('<Rule>', '', Justification = '<reason>')]." >&2
    echo "No silent suppression." >&2
    [ -z "$err" ] || echo "$err" >&2
    exit 2
    ;;
  *)
    fail "psscriptanalyzer-repo.ps1 returned rc=$rc, nothing was checked:" "$err" "$out"
    ;;
esac
