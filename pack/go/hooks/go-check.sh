#!/bin/bash
# Stop hook: run `golangci-lint run ./...` at the end of a turn.
# Matcher: "" — runs on every Stop.
# Blocking: exit 2 with stderr sends the findings back to the model, which fixes
# them in the same session with the context still warm.
#
# Deliberately NOT a PostToolUse hook: a package often does not compile halfway
# through a change, so per-edit feedback would burn turns on half-finished state.
#
# Runs on every Stop, not only after a Go edit: Go work may already be committed
# in the turn, and then no edit is left to hook on.
#
# No separate `go vet`: `govet` is in the default linter set of golangci-lint, and
# a stand-alone vet does not read `.golangci.yml`, so it would also ignore
# `issues.new-from-rev`.
#
# A build that fails blocks too: golangci-lint exits 1 for it, the same code as for
# a finding, and it is mostly code that does not compile.
#
# Could not check (no jq, git, go or golangci-lint, a pin that does not match, a
# configuration that does not verify, a golangci-lint that could not lint): exit 0
# with a systemMessage `go-check: COULD NOT CHECK — <reason>`. Blocking there would
# keep the session from stopping; staying silent would read as "lint-clean".
# `go` is required but never called here: golangci-lint needs it to load packages.
# A repository without a root go.mod reports `not checked` and exits 0, before the
# go and golangci-lint check, so it never blocks and is never silently green.
#
# Limit: the `cd` follows the `cwd` of the Stop payload, so work in a worktree from
# a session in the main checkout checks the wrong tree, as `ty-check` does. The fix
# for both is #54.

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

for tool in jq git; do
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

# Worktree-aware, as in pre-commit-tests.sh: lint the checkout the work happened
# in, not necessarily the main one.
CWD=$(printf '%s' "$INPUT" | jq -r '.cwd // empty')
[ -z "$CWD" ] && CWD="$CLAUDE_PROJECT_DIR"
cd "$CWD" 2>/dev/null || cd "$CLAUDE_PROJECT_DIR" 2>/dev/null || cannot_check "the directory $CWD does not exist."

ROOT=$(git rev-parse --show-toplevel 2>/dev/null)
if [ -z "$ROOT" ] || ! cd "$ROOT"; then
  cannot_check "not inside a git repository ($CWD)."
fi

if [ ! -f "$ROOT/go.mod" ]; then
  report "$HOOK_NAME: not checked — no go.mod in the repository root (go-check lints only a root module)."
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
    cannot_check "golangci-lint $HAVE runs; .golangci-version pins $WANT."
  fi
fi

# `config verify`: 0 = valid, 6 = no configuration file (lint with the defaults),
# anything else = a configuration that cannot be read.
VERIFY=$(golangci-lint config verify 2>&1)
VERIFY_RC=$?
if [ "$VERIFY_RC" -ne 0 ] && [ "$VERIFY_RC" -ne 6 ]; then
  # \134 is the backslash in octal: a literal one at the end of the set reads as an escaped quote.
  cannot_check "the golangci-lint configuration does not verify (exit $VERIFY_RC): $(printf '%s' "$VERIFY" | head -3 | tr '\n"\134' '   ')"
fi

# `run` exits 1 on findings (a vet finding and a build that fails included); any
# other non-zero code means it could not lint.
OUT=$(golangci-lint run ./... 2>&1)
RC=$?
if [ "$RC" -ne 0 ] && [ "$RC" -ne 1 ]; then
  # \134 is the backslash in octal: a literal one at the end of the set reads as an escaped quote.
  cannot_check "golangci-lint exited $RC: $(printf '%s' "$OUT" | head -3 | tr '\n"\134' '   ')"
fi
if [ "$RC" -eq 1 ]; then
  echo "$HOOK_NAME: golangci-lint finds problems." >&2
  echo "Fix them in the code — do not add a //nolint over a finding. If you really want a linter off, ask." >&2
  echo "" >&2
  echo "$OUT" >&2
  exit 2
fi

exit 0
