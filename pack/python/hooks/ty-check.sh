#!/bin/bash
# Stop hook: run `ty check` on the directories in python.source_dirs at the end
# of a turn.
# Matcher: "" — runs on every Stop.
# Blocking: exit 2 with stderr sends the diagnostics back to the model, which
# fixes them in the same session with the context still warm.
#
# Deliberately NOT a PostToolUse hook: a type error is often only judgeable once
# several files are done, so per-edit feedback would burn turns on half-finished
# state.
#
# Could not check (no jq or uvx, no repository, no venv, a ty that does not
# start, a configuration that cannot be read): exit 0 with a systemMessage
# `ty-check: COULD NOT CHECK — <reason>` (ontwerpkeuze 13). Blocking there would
# keep the session from stopping; staying silent would read as "type-clean".
# `python.source_dirs = []` turns the check off, without a message.

# The pin of ty. The python-writing skill names the same version.
TY_VERSION="0.0.72"
HOOK_NAME="ty-check"

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
  report "$HOOK_NAME: COULD NOT CHECK — $1 Nothing was type-checked; that is not a green light."
}

for tool in jq uvx git python3; do
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

# Worktree-aware, as in pre-commit-tests.sh: test the checkout the work happened
# in, not necessarily the main one.
CWD=$(printf '%s' "$INPUT" | jq -r '.cwd // empty')
[ -z "$CWD" ] && CWD="$CLAUDE_PROJECT_DIR"
cd "$CWD" 2>/dev/null || cd "$CLAUDE_PROJECT_DIR" 2>/dev/null || cannot_check "the directory $CWD does not exist."

ROOT=$(git rev-parse --show-toplevel 2>/dev/null)
[ -n "$ROOT" ] && cd "$ROOT" || cannot_check "not inside a git repository ($CWD)."

# The lib lives in the lighthouse lib of this repo. Found through
# LIGHTHOUSE_REPO_ROOT or git, never from this file's own path.
LIB_ROOT="${LIGHTHOUSE_REPO_ROOT:-$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null)}"
if ! DIRS_RAW=$(LIGHTHOUSE_REPO_ROOT="$LIB_ROOT" python3 "$LIB_ROOT/.lighthouse/lib/lighthouse_lib/config.py" python.source_dirs 2>&1); then
  cannot_check "the configuration cannot be read (python.source_dirs): $(printf '%s' "$DIRS_RAW" | tr '\n"\\' "   ")."
fi

# An empty list turns the check off.
[ -z "$DIRS_RAW" ] && exit 0

# The directories that exist; one per line in the config output.
DIRS=()
while IFS= read -r dir; do
  [ -n "$dir" ] && [ -d "$dir" ] && DIRS+=("$dir")
done <<EOF
$DIRS_RAW
EOF
if [ "${#DIRS[@]}" -eq 0 ]; then
  report "$HOOK_NAME: not checked — no directory from python.source_dirs exists (set it in lighthouse.toml; [] turns the check off)"
fi
WHERE="${DIRS[*]}"

# ty needs a venv to resolve third-party imports. Without one every import falls
# over. A fresh worktree has no .venv until the first `uv run`, so fall back to
# the main checkout's.
VENV=""
if [ -d "$ROOT/.venv" ]; then
  VENV="$ROOT/.venv"
elif [ -n "$CLAUDE_PROJECT_DIR" ] && [ -d "$CLAUDE_PROJECT_DIR/.venv" ]; then
  VENV="$CLAUDE_PROJECT_DIR/.venv"
else
  cannot_check "no .venv in $ROOT or the main checkout; run uv sync first."
fi

# `uvx ty@version check` returns the same exit 1 for "found diagnostics" and for
# "could not resolve the tool" — the exit code alone can't tell them apart.
# Probe separately: a tool that can't even start is not a verdict on the code.
if ! uvx "ty@$TY_VERSION" --version >/dev/null 2>&1; then
  cannot_check "ty@$TY_VERSION does not start (offline, or the pin does not resolve)."
fi

# `--color never` pins the format the code recognition below anchors on. Color
# puts an ANSI escape before `error[`, and then the line anchor no longer
# matches.
# ty exits 1 on diagnostics and 2 when it could not check (an invalid [tool.ty], a
# bad argument): only 1 is a verdict on the code.
OUT=$(uvx "ty@$TY_VERSION" check --color never --python "$VENV" "${DIRS[@]}" 2>&1)
TY_RC=$?
if [ "$TY_RC" -ne 0 ] && [ "$TY_RC" -ne 1 ]; then
  cannot_check "ty exited $TY_RC (an invalid [tool.ty] in pyproject.toml or ty.toml?): $(printf '%s' "$OUT" | head -5 | tr '\n' ' ')"
fi
if [ "$TY_RC" -eq 1 ]; then
  # Which rule codes came out? ty puts them at the line start as
  # `error[<code>]:` or `warning[<code>]:`. Empty = a failure without
  # diagnostics; that falls through to the plain block.
  CODES=$(printf '%s\n' "$OUT" |
    sed -nE 's/^(error|warning)\[([a-z0-9-]+)\].*/\2/p' | sort -u)

  # A set of only `unresolved-import` points to the environment rather than the
  # code: a venv behind on uv.lock drops a package that is simply in
  # [project].dependencies. "Fix them in the code" sends the reader down the
  # wrong track then.
  #
  # Deliberately exit 2 and not a message: `unresolved-import` is also exactly
  # the signal that someone added an import without declaring the dependency.
  # That must not drop silently. Both branches are actionable at once (run
  # `uv sync`, or add the dependency). It cannot loop: the stop_hook_active
  # guard at the top lets the hook fire once per turn.
  if [ "$CODES" = "unresolved-import" ]; then
    echo "$HOOK_NAME: ty finds only unresolvable imports in $WHERE, no type errors." >&2
    echo "That points to a skewed environment rather than wrong code." >&2
    echo "" >&2
    echo "Check your venv first:  uv sync  (with the extras you use)." >&2
    echo "If the venv is right and the import stays unresolvable, the" >&2
    echo "dependency is missing in pyproject.toml — add it there. Do not" >&2
    echo "remove the import from the code." >&2
    echo "" >&2
    echo "$OUT" >&2
    exit 2
  fi

  echo "$HOOK_NAME: ty finds errors in $WHERE. Fix them in the code — do not" >&2
  echo "turn off a ty rule and do not paste a bare # type: ignore over it." >&2
  echo "" >&2
  echo "$OUT" >&2
  exit 2
fi

exit 0
