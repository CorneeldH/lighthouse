#!/usr/bin/env bash
# Measures lighthouse on one host the way a user meets it (#90): `init --yes` in a fresh
# repository, then the core hooks with the payloads Claude Code sends them, read from the
# registrations `init` wrote. A measurement, not a gate: every step runs, a failed step does not
# stop the next, and the script exits 0. It prints one markdown table, row per step: expected,
# rc, verdict and the first line of stderr. On Linux every row must be a pass; the
# windows-measure workflow runs it on native Windows (Git Bash).
#
# Usage: ci/windows-measure.sh <lighthouse binary> <empty work dir>
set -u

BIN=$1
WORK=$2
# run.sh, which runs main-commit-gate and worktree-check, looks up `lighthouse` on the PATH.
BIN_DIR=$(cd "$(dirname "$BIN")" && pwd)
REPO="$WORK/repo"
ROWS=()

# row <step> <expected> <rc> <pass:0|1> <stderr file>
row() {
  local first verdict n
  n=${1%%.*}
  mkdir -p "$WORK/steps"
  cp "$OUT" "$WORK/steps/$n.out" 2>/dev/null
  cp "$5" "$WORK/steps/$n.err" 2>/dev/null
  first=$(grep -m1 -v '^[[:space:]]*$' "$5" 2>/dev/null | cut -c1-160 | tr '|' '/')
  if [ "$4" = 1 ]; then verdict=pass; else verdict=FAIL; fi
  ROWS+=("| $1 | $2 | $3 | $verdict | ${first:+\`$first\`} |")
}

# hook <event> <registration substring> <bash command in the payload>: runs the one registered
# command through bash with CLAUDE_PROJECT_DIR set, the directory of BIN first on the PATH and the
# payload on stdin; sets RC, OUT, ERR.
# The payload cwd is $REPO, or $HOOK_CWD when set.
hook() {
  local cmd payload
  OUT="$WORK/out.txt"
  ERR="$WORK/err.txt"
  : >"$OUT"
  : >"$ERR"
  cmd=$(jq -r --arg ev "$1" --arg name "$2" \
    '[.hooks[$ev][] | select(.matcher == "Bash" or .matcher == null or .matcher == "") | .hooks[].command | select(contains($name))] | first // empty' \
    "$REPO/.claude/settings.json" 2>>"$ERR")
  if [ -z "$cmd" ]; then
    echo "no registration for $2 under $1" >>"$ERR"
    RC=127
    return
  fi
  payload=$(jq -cn --arg ev "$1" --arg cwd "${HOOK_CWD:-$REPO}" --arg c "$3" \
    '{session_id: "measure", hook_event_name: $ev, tool_name: "Bash", cwd: $cwd, tool_input: {command: $c}, tool_response: {stdout: "", stderr: "", interrupted: false}}')
  (cd "$REPO" && printf '%s' "$payload" | PATH="$BIN_DIR:$PATH" CLAUDE_PROJECT_DIR="$REPO" \
    LIGHTHOUSE_HOST_TOML="$WORK/host.toml" XDG_RUNTIME_DIR="$WORK" \
    bash -c "$cmd") >"$OUT" 2>"$ERR"
  RC=$?
}

# ok <rc wanted> [text that must be in stdout or stderr]: also fails on COULD NOT CHECK.
ok() {
  [ "$RC" = "$1" ] || return 1
  ! grep -q 'COULD NOT CHECK' "$OUT" "$ERR" || return 1
  [ -z "${2:-}" ] || cat "$OUT" "$ERR" | grep -qF "$2"
}

mark() { if "$@"; then echo 1; else echo 0; fi; }

ENV="$WORK/env.txt"
{
  uname -s
  bash --version | head -1
  git --version
  for p in python3 python py uv jq; do
    printf '%s: %s\n' "$p" "$(command -v "$p" || echo missing)"
  done
  python3 --version 2>&1 || true
} >"$ENV" 2>&1

mkdir -p "$REPO"
git -C "$REPO" init -q -b main
git -C "$REPO" config user.email measure@example.invalid
git -C "$REPO" config user.name measure
git -C "$REPO" config commit.gpgsign false
echo x >"$REPO/README.md"
git -C "$REPO" add README.md
git -C "$REPO" commit -q -m init

OUT="$WORK/out.txt"
ERR="$WORK/err.txt"
(cd "$REPO" && "$BIN" init --yes) >"$OUT" 2>"$ERR"
RC=$?
# Not `ok`: the diff init prints carries the skills, whose text names COULD NOT CHECK.
init_ok() { [ "$RC" = 0 ] && [ "$(head -1 "$OUT")" = "Packages: core" ]; }
row "1. \`lighthouse init --yes\`" "rc 0, \`Packages: core\`" "$RC" "$(mark init_ok)" "$ERR"

git -C "$REPO" add -A >/dev/null 2>&1
git -C "$REPO" commit -q -m "lighthouse init" >/dev/null 2>&1

hook PreToolUse "run.sh main-commit-gate" "git commit -m x"
row "2. main-commit-gate, commit on \`main\`" "rc 2, blocked" "$RC" "$(mark ok 2 'Main-commit gate: blocked')" "$ERR"

# Claude Code on Windows sends the cwd as `C:\...`: a directory the command creates below it is
# still in the repo, so a commit there is on main too.
HOOK_CWD=$(cygpath -w "$REPO" 2>/dev/null || printf '%s' "$REPO")
hook PreToolUse "run.sh main-commit-gate" "mkdir new && cd new && git commit -m x"
HOOK_CWD=
row "2b. main-commit-gate, commit in a fresh dir, Windows cwd" "rc 2, blocked" "$RC" "$(mark ok 2 'Main-commit gate: blocked')" "$ERR"

hook PreToolUse "commit-test-gate.sh pre" "git commit -m x"
row "3. commit-test-gate pre, on \`main\`" "rc 0, \`nothing was tested\`" "$RC" "$(mark ok 0 'nothing was tested')" "$ERR"

git -C "$REPO" checkout -q -b issue-1-x
hook PreToolUse "run.sh main-commit-gate" "git commit -m x"
row "4. main-commit-gate, commit on a branch" "rc 0" "$RC" "$(mark ok 0)" "$ERR"

hook PreToolUse "commit-test-gate.sh pre" "git commit -m x"
row "5. commit-test-gate pre, on a branch" "rc 0" "$RC" "$(mark ok 0)" "$ERR"
echo y >"$REPO/a.txt"
git -C "$REPO" add a.txt
git -C "$REPO" commit -q -m "a" >/dev/null 2>&1
hook PostToolUse "commit-test-gate.sh post" "git commit -m x"
row "6. commit-test-gate post, after a commit" "rc 0" "$RC" "$(mark ok 0)" "$ERR"

hook PreToolUse "run.sh bash-write-check pre" "echo z > b.txt"
row "7. bash-write-check pre" "rc 0" "$RC" "$(mark ok 0)" "$ERR"
echo z >"$REPO/b.txt"
hook PostToolUse "run.sh bash-write-check post" "echo z > b.txt"
row "8. bash-write-check post" "rc 0" "$RC" "$(mark ok 0)" "$ERR"

hook PreToolUse "run.sh pr-create-gate" "gh pr create --title x --body x"
row "9. pr-create-gate, body without issue line" "rc 2" "$RC" "$(mark ok 2)" "$ERR"
hook PreToolUse "run.sh pr-create-gate" "gh pr create --title x --body 'No issue: a measurement on Windows'"
row "10. pr-create-gate, \`No issue:\` body" "rc 0" "$RC" "$(mark ok 0)" "$ERR"

# With a [test_gate] command, post runs it on the commit's files, inside a gate slot.
printf '\n[test_gate]\ncommand = "git --version"\n' >>"$REPO/lighthouse.toml"
git -C "$REPO" commit -q -am "test gate" >/dev/null 2>&1
hook PreToolUse "commit-test-gate.sh pre" "git commit -m x"
echo c >"$REPO/c.txt"
git -C "$REPO" add c.txt
git -C "$REPO" commit -q -m "c" >/dev/null 2>&1
hook PostToolUse "commit-test-gate.sh post" "git commit -m x"
row "11. commit-test-gate post, with \`[test_gate] command\`" "rc 0, \`git version\`" "$RC" "$(mark ok 0 'git version')" "$ERR"

echo "### Host"
echo
echo '```'
cat "$ENV"
echo '```'
echo
echo "| Step | Expected | rc | Verdict | First stderr line |"
echo "|---|---|---|---|---|"
printf '%s\n' "${ROWS[@]}"
exit 0
