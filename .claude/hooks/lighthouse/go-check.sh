#!/bin/bash
# lighthouse: go@dev
# Stop hook: run `golangci-lint run ./...` at the end of a turn.
# Matcher: "" — runs on every Stop.
# Blocking: exit 2 with stderr sends the findings back to the model, which fixes
# them in the same session with the context still warm.
#
# Deliberately NOT a PostToolUse hook: a package often does not compile halfway
# through a change, so per-edit feedback would burn turns on half-finished state.
#
# Runs on every Stop, but lints only a checkout in which the turn touched a Go
# path: an edit, a Bash write or a commit in the turn (`turn_roots.py --match`,
# #80). The rest gets `not checked`. Go work may already be committed in the turn,
# and then no edit is left to hook on: the commit counts.
#
# Looks at every checkout the turn wrote in (turn_roots.py, #54), not the `cwd` of
# the payload: that is the shell's directory at Stop, and a session in the main
# checkout that edits a worktree stops there.
#
# No separate `go vet`: `govet` is in the default linter set of golangci-lint, and
# a stand-alone vet does not read `.golangci.yml`, so it would also ignore
# `issues.new-from-rev`.
#
# A build that fails blocks too: golangci-lint exits 1 for it, the same code as for
# a finding, and it is mostly code that does not compile.
#
# Could not check (no jq, git, python3, go or golangci-lint, a pin that does not
# match, a configuration that does not verify, a golangci-lint that could not lint,
# a turn whose checkout cannot be determined): exit 0 with a systemMessage
# `go-check: COULD NOT CHECK — <reason>`. Blocking there would keep the session
# from stopping; staying silent would read as "lint-clean".
# While the usage alarm is up (#74) the gate says `not checked` and exits 0: the
# usage guard has the session wrap up, and fixing a finding costs budget that is
# no longer there. Without `usage-guard/alarm.sh` next to this file (no pack/core)
# there is no alarm to read and the check runs as before.
# `go` is required but never called here: golangci-lint needs it to load packages.
# A checkout without a root go.mod reports `not checked` and exits 0, before the
# go and golangci-lint check, so it never blocks and is never silently green.

HOOK_NAME="go-check"

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
  report "$HOOK_NAME: COULD NOT CHECK — $1 Nothing was linted; that is not a green light."
}

for tool in jq git python3; do
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

# The usage alarm (#74): the rules live in usage-guard/alarm.sh, installed by
# pack/core next to this file. A missing helper or an invalid alarm checks as usual.
case "${BASH_SOURCE[0]}" in
  */*) HOOKS_DIR="${BASH_SOURCE[0]%/*}" ;;
  *) HOOKS_DIR="." ;;
esac
# shellcheck source=/dev/null
if [ -f "$HOOKS_DIR/usage-guard/alarm.sh" ] && . "$HOOKS_DIR/usage-guard/alarm.sh" && usage_alarm_valid; then
  report "$HOOK_NAME: not checked — the usage alarm is up until $USAGE_ALARM_LOCAL; the usage guard wraps up this session."
fi

# The lib lives in the lighthouse lib of this repo. Found through
# LIGHTHOUSE_REPO_ROOT or git, never from this file's own path.
LIB_ROOT="${LIGHTHOUSE_REPO_ROOT:-$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null)}"

# What makes a path a Go path (fnmatch on the path relative to the root; `*` also
# matches `/`).
GO_PATTERNS=(
  '*.go' go.mod go.sum go.work go.work.sum
  .golangci.yml .golangci.yaml .golangci.toml .golangci.json .golangci-version
)
MATCH_ARGS=()
for pattern in "${GO_PATTERNS[@]}"; do
  MATCH_ARGS+=(--match "$pattern")
done

# The checkouts this turn wrote in, one per line: `touched<TAB>root` or
# `untouched<TAB>root` (#54, #80).
if ! ROOTS=$(printf '%s' "$INPUT" | LIGHTHOUSE_REPO_ROOT="$LIB_ROOT" python3 "$LIB_ROOT/.lighthouse/lib/lighthouse_lib/turn_roots.py" "${MATCH_ARGS[@]}" 2>&1); then
  # \134 is the backslash in octal: a literal one at the end of the set reads as an escaped quote.
  cannot_check "the checkout of this turn cannot be determined: $(printf '%s' "$ROOTS" | tr '\n"\134' '   ')"
fi

# One checkout, in a subshell: `report` and `cannot_check` end only that one.
check_root() (
  ROOT="$1"
  VERDICT="$2"
  cd "$ROOT" 2>/dev/null || cannot_check "the checkout $ROOT does not exist."

  if [ "$VERDICT" = "untouched" ]; then
    report "$HOOK_NAME: not checked — the turn touched no Go path in $ROOT."
  fi

  if [ ! -f "$ROOT/go.mod" ]; then
    report "$HOOK_NAME: not checked — no go.mod in the repository root $ROOT (go-check lints only a root module)."
  fi

  for tool in go golangci-lint; do
    command -v "$tool" >/dev/null 2>&1 || cannot_check "$tool is missing."
  done

  # The pin, when there is one: a golangci-lint of another version lints by other
  # rules. `version --short` prints it without the leading v.
  if [ -f "$ROOT/.golangci-version" ]; then
    WANT=$(tr -d '[:space:]' <"$ROOT/.golangci-version")
    WANT="${WANT#v}"
    HAVE=$(golangci-lint version --short 2>/dev/null | tr -d '[:space:]')
    HAVE="${HAVE#v}"
    if [ "$HAVE" != "$WANT" ]; then
      cannot_check "golangci-lint $HAVE runs; .golangci-version in $ROOT pins $WANT."
    fi
  fi

  # `config verify`: 0 = valid, 6 = no configuration file (lint with the defaults),
  # anything else = a configuration that cannot be read.
  VERIFY=$(golangci-lint config verify 2>&1)
  VERIFY_RC=$?
  if [ "$VERIFY_RC" -ne 0 ] && [ "$VERIFY_RC" -ne 6 ]; then
    # \134 is the backslash in octal: a literal one at the end of the set reads as an escaped quote.
    cannot_check "the golangci-lint configuration in $ROOT does not verify (exit $VERIFY_RC): $(printf '%s' "$VERIFY" | head -3 | tr '\n"\134' '   ')"
  fi

  # `run` exits 1 on findings (a vet finding and a build that fails included); any
  # other non-zero code means it could not lint.
  OUT=$(golangci-lint run ./... 2>&1)
  RC=$?
  if [ "$RC" -ne 0 ] && [ "$RC" -ne 1 ]; then
    # \134 is the backslash in octal: a literal one at the end of the set reads as an escaped quote.
    cannot_check "golangci-lint exited $RC in $ROOT: $(printf '%s' "$OUT" | head -3 | tr '\n"\134' '   ')"
  fi
  if [ "$RC" -eq 1 ]; then
    echo "$HOOK_NAME: golangci-lint finds problems in $ROOT." >&2
    echo "Fix them in the code — do not add a //nolint over a finding. If you really want a linter off, ask." >&2
    echo "" >&2
    echo "$OUT" >&2
    exit 2
  fi
  exit 0
)

# Every checkout gets its check. A finding in one blocks; a message from another
# still reaches the reader.
ERRS=$(mktemp) || cannot_check "mktemp failed."
trap 'rm -f "$ERRS"' EXIT
MESSAGES=""
BLOCKED=0
TAB=$(printf '\t')
while IFS= read -r line; do
  [ -n "$line" ] || continue
  verdict="${line%%"$TAB"*}"
  root="${line#*"$TAB"}"
  MSG=$(check_root "$root" "$verdict" 2>>"$ERRS")
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
