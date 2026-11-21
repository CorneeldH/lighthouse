#!/usr/bin/env bash
# PostToolUse hook: shellcheck on every edited shell script.
# Matcher: Edit|Write. Counterpart of the shell-standard skill.
#
# Why this hook is NOT fail-open: skipping silently is itself the error the
# hook must prevent. shellcheck is pinned, sha256-verified and lives in the
# shared tool cache, so "I could not look" is a defect and not a normal
# state. Missing tool = fail loudly (exit 2, first line
# `shell-lint-check: COULD NOT CHECK — …`), never exit 0.
#
# Two blocking thresholds, deliberately two different sets:
#   - the directories in `shell.strict_dirs` (default `.claude/hooks`): everything,
#     down to style.
#   - all other .sh: error + warning. Info and style findings in the rest
#     of a repo would block every edit on debt that has nothing to do with
#     the change.
#
# The reasons this hook blocks on. A finding blocks as shellcheck prints it
# (`SC2164 (warning)`). Each SC code is also a floor: when the configuration read
# by shellcheck for the edited script (a .shellcheckrc above it, in $HOME or
# $XDG_CONFIG_HOME, SHELLCHECK_OPTS, a code range in `disable=`) switches one off, the
# hook blocks with `[shell-lint-check/<code>]`, also on a clean script. `strict-style` is
# no SC code but the threshold of the strict directories: a block there that holds only
# style and info findings carries `[shell-lint-check/strict-style]`. It has no floor.
BLOCKING_RULES="SC2164,SC2046,SC2115,SC2155,SC2034,strict-style"
# The IDs of BLOCKING_RULES that are no shellcheck code.
NON_LINTER_RULES="strict-style"
set -uo pipefail

fail() { # reason...
  echo "shell-lint-check: COULD NOT CHECK — $*" >&2
  exit 2
}

INPUT=$(cat)

# A payload without `.sh"` or `.bash"` cannot name a shell file: nothing to check,
# with or without jq. This filter only skips; whatever it lets through is read by
# jq below, so a stray match costs at most a COULD NOT CHECK, never a missed file.
case "$INPUT" in
  *'.sh"'*|*'.bash"'*) ;;
  *) exit 0 ;;
esac

# jq reads the payload. Without it the edited path cannot be read, and failing
# loudly is the only honest answer: guessing at JSON with grep is the other side
# of the same error.
command -v jq >/dev/null 2>&1 || fail "jq is not present, so the edited path cannot be read."
FILE_PATH=$(printf '%s' "$INPUT" | jq -r '.tool_input.file_path // empty')

# No path in the payload: this tool call touched no file. Nothing to do.
[ -n "$FILE_PATH" ] || exit 0

case "$FILE_PATH" in
  *.sh|*.bash) ;;
  *) exit 0 ;;
esac

# Deleted or renamed between the edit and the hook: nothing to check.
[ -f "$FILE_PATH" ] || exit 0

# The lighthouse root: LIGHTHOUSE_REPO_ROOT, else the tree of the EDITED
# file, and only then that of the hook. $CLAUDE_PROJECT_DIR points to the
# main checkout, and an edit in a worktree would then be checked with the
# script of another branch.
FILE_DIR="$(dirname "$FILE_PATH")"
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

ROOT=""
searched=""
for root in ${candidates[@]+"${candidates[@]}"}; do
  searched="$searched $root"
  if [ -x "$root/.lighthouse/scripts/shellcheck-repo.sh" ]; then
    ROOT="$root"
    break
  fi
done
if [ -z "$ROOT" ]; then
  fail "no .lighthouse/scripts/shellcheck-repo.sh found (searched:${searched:- no lighthouse root})."
fi
SCRIPT="$ROOT/.lighthouse/scripts/shellcheck-repo.sh"

# The strict directories are repo-relative: compare the path within the git tree
# of the file, else within the lighthouse root.
command -v lighthouse >/dev/null 2>&1 || fail "lighthouse not on PATH"
if ! STRICT_DIRS=$(cd "$ROOT" && lighthouse config shell.strict_dirs 2>&1); then
  fail "shell.strict_dirs could not be read: ${STRICT_DIRS:-no message}"
fi
ABS="$(cd "$FILE_DIR" && pwd -P)/${FILE_PATH##*/}"
REL=""
for base in "$FILE_ROOT" "$ROOT"; do
  [ -n "$base" ] || continue
  base="$(cd "$base" && pwd -P)"
  case "$ABS" in
    "$base"/*) REL="${ABS#"$base"/}"; break ;;
  esac
done

severity=warning
while IFS= read -r dir; do
  dir="${dir%/}"
  [ -n "$dir" ] && [ -n "$REL" ] || continue
  case "$REL" in
    "$dir"/*) severity=style; break ;;
  esac
done <<EOF
$STRICT_DIRS
EOF

# stderr separately: a deviating shellcheck version reports itself there, also at rc 0.
ERR_FILE="$(mktemp)" || fail "mktemp failed."

# The floor, before the lint run: the SC codes of BLOCKING_RULES are on for a script in
# FILE_DIR, as shellcheck sees that directory. shellcheck itself is resolved from this
# directory, as for the lint run.
floor=""
IFS=, read -ra RULES <<<"$BLOCKING_RULES"
for id in "${RULES[@]}"; do
  case ",$NON_LINTER_RULES," in *",$id,"*) continue ;; esac
  floor="${floor:+$floor,}$id"
done
missing=$("$SCRIPT" --check-floor "$floor" "$FILE_DIR" 2>"$ERR_FILE")
rc=$?
case "$rc" in
  0) ;;
  1)
    [ -n "$missing" ] || { rm -f "$ERR_FILE"; fail "shellcheck-repo.sh --check-floor returned rc=1 without naming a code."; }
    while IFS= read -r code; do
      echo "[shell-lint-check/$code] shell-lint-check: $code is switched off for $FILE_PATH (a .shellcheckrc above it or in \$HOME or \$XDG_CONFIG_HOME, SHELLCHECK_OPTS, or a code range in disable=); this hook needs it on." >&2
    done <<EOF
$missing
EOF
    # The notice of a divergent shellcheck, as in the lint run below.
    [ ! -s "$ERR_FILE" ] || cat "$ERR_FILE" >&2
    echo "Switch the check back on and fix the code. Existing debt goes behind '# shellcheck disable=SCxxxx <reason>' on the line above it, not behind a check that is off." >&2
    rm -f "$ERR_FILE"
    exit 2
    ;;
  *)
    err="$(cat "$ERR_FILE")"
    rm -f "$ERR_FILE"
    fail "shellcheck-repo.sh --check-floor returned rc=$rc, nothing was checked:" "$err" "$missing"
    ;;
esac

out=$("$SCRIPT" --severity "$severity" -- "$FILE_PATH" 2>"$ERR_FILE")
rc=$?
err="$(cat "$ERR_FILE")"
rm -f "$ERR_FILE"

case "$rc" in
  0)
    # Clean, but possibly with a different version than the pin: not silent.
    if [ -n "$err" ]; then
      msg="shell-lint-check: $err"
      jq -cn --arg m "$msg" '{systemMessage: $m}'
    fi
    exit 0
    ;;
  1)
    # The strict threshold is the reason when the run went down to style and the same
    # file is clean at warning: only style and info findings.
    tag=""
    if [ "$severity" = style ] && "$SCRIPT" --severity warning -- "$FILE_PATH" >/dev/null 2>&1; then
      tag="[shell-lint-check/strict-style] "
    fi
    echo "${tag}shell-lint-check: fix this in the code. If a rule really has to go, give a reason:" >&2
    echo "'# shellcheck disable=SCxxxx <reason>' on the line above." >&2
    echo "No silent suppression." >&2
    echo "" >&2
    [ -z "$err" ] || echo "$err" >&2
    echo "$out" >&2
    exit 2
    ;;
  *)
    fail "shellcheck-repo.sh returned rc=$rc, nothing was checked:" "$err" "$out"
    ;;
esac
