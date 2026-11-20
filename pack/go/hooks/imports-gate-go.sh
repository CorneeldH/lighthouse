#!/bin/bash
# Stop hook: run `lighthouse imports check --lang go` at the end of a turn, so an import
# of a module that no manifest declares is caught while the session still has the
# context (#383).
# Matcher: "" — runs on every Stop.
# Blocking: exit 2 with the findings on stderr sends them back to the model, which fixes them
# in the same session. Each finding line holds its rule ID (`<file>:<line>: <ID> <import> — <why>`).
#
# Runs on every Stop, but checks only a checkout in which the turn touched a relevant path (an
# edit, a Bash write or a commit in the turn: `lighthouse turn-roots --match`, #54, #80). The
# rest gets `not checked`. For a touched checkout, `--files` carries the touched paths
# (`turn-roots --files`), so the check stays within the projects of this turn; a touched
# checkout without a path line (doubt) is checked without `--files`.
#
# Could not check (no jq, git or lighthouse, a lighthouse too old to know `imports`, exit 3,
# exit 1 without a finding line, a turn whose checkout cannot be determined): exit 0 with a
# systemMessage `imports-gate-go: COULD NOT CHECK — <reason>`. Blocking there would keep the session
# from stopping; staying silent would read as "all imports declared".
# While the usage alarm is up (#74) the gate says `not checked` and exits 0: the usage gate has
# the session wrap up. `[imports] check = false` turns the hook off, without a message.

HOOK_NAME="imports-gate-go"
# The rule IDs the gate blocks on; the rest of its findings are advice. Read by the config
# audit, so keep this one plain assignment line.
BLOCKING_RULES="indirect-import,missing-require"

# A message to the user, without blocking. Without jq the JSON is built by hand:
# the reasons below hold no quote or backslash.
report() {
  if command -v jq >/dev/null 2>&1; then
    jq -cn --arg m "$1" '{systemMessage: $m}'
  else
    printf '{"systemMessage":"%s"}\n' "$1"
  fi
  exit 0
}

cannot_check() {
  report "$HOOK_NAME: COULD NOT CHECK — $1 Nothing was checked for undeclared imports; that is not a green light."
}

for tool in jq git lighthouse; do
  command -v "$tool" >/dev/null 2>&1 || cannot_check "$tool is missing."
done

INPUT=$(cat)
if ! ACTIVE=$(printf '%s' "$INPUT" | jq -r '.stop_hook_active // false' 2>/dev/null); then
  cannot_check "the payload is not valid JSON."
fi

# Claude Code re-invokes the model when a Stop hook blocks. Without this guard
# a failing check would loop forever.
if [ "$ACTIVE" = "true" ]; then
  exit 0
fi

# The repository is found through LIGHTHOUSE_REPO_ROOT or git, never from this file's own path.
LIB_ROOT="${LIGHTHOUSE_REPO_ROOT:-$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null)}"

# The opt-out: `[imports] check = false`. Any other answer (unset, an older lighthouse that does
# not know the key) leaves the check on; the check itself reads the key per checkout too.
if [ "$(LIGHTHOUSE_REPO_ROOT="$LIB_ROOT" lighthouse config imports.check 2>/dev/null)" = "false" ]; then
  exit 0
fi

# The usage alarm (#74): `lighthouse usage alarm` prints the valid alarm and exits 0.
# Without a valid alarm the check runs as usual.
# An older lighthouse prints help and exits 0 for an unknown subcommand: only a line with
# exactly the alarm shape counts, anything else is no alarm.
if ALARM_LINE=$(lighthouse usage alarm 2>/dev/null) \
  && read -r ALARM_KIND _ ALARM_LOCAL ALARM_AT ALARM_REST <<<"$ALARM_LINE" \
  && [[ "$ALARM_KIND" =~ ^(venster|week)$ && -n "$ALARM_LOCAL" && -z "$ALARM_REST" \
    && "$ALARM_AT" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]]; then
  report "$HOOK_NAME: not checked — the usage alarm is up until ${ALARM_LOCAL//_/ }; the usage gate wraps up this session."
fi

# What makes a path a Go path (fnmatch on the path relative to the root; `*` also
# matches `/`).
PATTERNS=(
  '*.go' go.mod go.sum
)
MATCH_ARGS=()
for pattern in "${PATTERNS[@]}"; do
  MATCH_ARGS+=(--match "$pattern")
done

# The checkouts this turn wrote in: `touched<TAB>root` or `untouched<TAB>root` per checkout, then
# `file<TAB>root<TAB>path` per touched path (#54, #80, #383).
if ! ROOTS=$(printf '%s' "$INPUT" | LIGHTHOUSE_REPO_ROOT="$LIB_ROOT" lighthouse turn-roots "${MATCH_ARGS[@]}" --files 2>&1); then
  # \134 is the backslash in octal: a literal one at the end of the set reads as an escaped quote.
  cannot_check "the checkout of this turn cannot be determined: $(printf '%s' "$ROOTS" | tr '\n"\134' '   ')"
fi

TAB=$(printf '\t')
ERR_FILE=$(mktemp) || cannot_check "mktemp failed."
ERRS=$(mktemp) || cannot_check "mktemp failed."
trap 'rm -f "$ERRS" "$ERR_FILE"' EXIT

# One checkout, in a subshell: `report` and `cannot_check` end only that one.
check_root() (
  ROOT="$1"
  VERDICT="$2"
  cd "$ROOT" 2>/dev/null || cannot_check "the checkout $ROOT does not exist."

  if [ "$VERDICT" = "untouched" ]; then
    report "$HOOK_NAME: not checked — the turn touched no Go path in $ROOT."
  fi

  # One --files per touched path; none (doubt) checks the whole checkout. A path is read as
  # is: it may hold spaces.
  FILES_ARGS=()
  while IFS="$TAB" read -r kind froot fpath; do
    if [ "$kind" = "file" ] && [ "$froot" = "$ROOT" ] && [ -n "$fpath" ]; then
      FILES_ARGS+=(--files "$fpath")
    fi
  done <<EOF
$ROOTS
EOF

  OUT=$(LIGHTHOUSE_REPO_ROOT="$ROOT" lighthouse imports check --lang go "${FILES_ARGS[@]}" 2>"$ERR_FILE")
  RC=$?
  ERR=$(cat "$ERR_FILE")

  if [ "$RC" -eq 0 ]; then
    exit 0
  fi

  # A finding line: `<file>:<line>: <ID> <import> — <why>`. Exit 1 without one (an older
  # lighthouse that does not know `imports` says `unknown command` on stderr) is no verdict.
  FINDINGS=$(printf '%s\n' "$OUT" | grep -E '^.+:[0-9]+: [^ ]+ ' | grep -v '^COULD NOT CHECK')
  if [ "$RC" -eq 1 ] && [ -n "$FINDINGS" ]; then
    echo "$HOOK_NAME: imports in $ROOT that no manifest declares (blocking rules: $BLOCKING_RULES)." >&2
    echo "Declare the dependency in its manifest — do not remove the import to silence the check." >&2
    echo "" >&2
    printf '%s\n' "$OUT" >&2
    [ -n "$ERR" ] && printf '%s\n' "$ERR" >&2
    exit 2
  fi

  # \134 is the backslash in octal: a literal one at the end of the set reads as an escaped quote.
  WHY=$(printf '%s %s' "$ERR" "$OUT" | head -c 300 | tr '\n"\134' '   ')
  cannot_check "lighthouse imports check ended with exit $RC in $ROOT: $WHY"
)

# Every checkout gets its check. A finding in one blocks; a message from another
# still reaches the reader.
MESSAGES=""
BLOCKED=0
while IFS="$TAB" read -r kind root _; do
  [ "$kind" = "touched" ] || [ "$kind" = "untouched" ] || continue
  MSG=$(check_root "$root" "$kind" 2>>"$ERRS")
  RC=$?
  if [ "$RC" -eq 2 ]; then
    BLOCKED=1
  elif [ "$RC" -ne 0 ]; then
    MSG=$(jq -cn --arg m "$HOOK_NAME: COULD NOT CHECK — the check of $root ended with exit $RC." '{systemMessage: $m}')
  fi
  if [ -n "$MSG" ]; then
    MESSAGES="${MESSAGES:+$MESSAGES
}$(printf '%s' "$MSG" | jq -r '.systemMessage')"
  fi
done <<EOF
$ROOTS
EOF

if [ "$BLOCKED" -eq 1 ]; then
  cat "$ERRS" >&2
  [ -n "$MESSAGES" ] && printf '\n%s\n' "$MESSAGES" >&2
  exit 2
fi
[ -n "$MESSAGES" ] && report "$MESSAGES"
exit 0
