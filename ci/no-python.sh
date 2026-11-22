#!/usr/bin/env bash
# Proves that lighthouse needs no Python (#285): on a PATH without python*, `init --yes` with the
# core, go, node, shell and expo packs, then every hook registration that init wrote, each with
# its payload from tests/pack/fixtures/hooks/<event>/<hook>[-<stand>].json. Last, no installed
# file may name the Python 3 interpreter. A gate, unlike ci/windows-measure.sh: every check runs, and each failure
# is one line on stderr.
#
# Usage: ci/no-python.sh <lighthouse binary> <empty work dir>
# Exit: 0 all green, 1 a check failed, 2 wrong usage.
# Test seams: NO_PYTHON_FIXTURES replaces the fixture directory; NO_PYTHON_EXTRA is a file
# copied into the repo before the interpreter-name grep.
set -u

# The needle of this gate, built here so that this script does not name it itself (#370).
py=python
needle="${py}3"

if [ "$#" -ne 2 ] || [ ! -x "$1" ] || [ ! -d "$2" ] || [ -n "$(ls -A "$2")" ]; then
  echo "usage: $0 <lighthouse binary> <empty work dir>" >&2
  exit 2
fi

HERE=$(cd "$(dirname "$0")" && pwd)
BIN="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
WORK=$(cd "$2" && pwd)
REPO="$WORK/repo"
FIXTURES=${NO_PYTHON_FIXTURES:-$HERE/../tests/pack/fixtures/hooks}
FIXTURES=$(cd "$FIXTURES" && pwd) || exit 2
TRANSCRIPT="$FIXTURES/transcript.jsonl"
FAILED=0

fail() {
  echo "no-python: $*" >&2
  FAILED=1
}

# Resolved before the PATH changes: jq, git and the rest are read from the toolbox afterwards.
JQ=$(command -v jq) || { echo "no-python: jq is missing" >&2; exit 2; }

# 1. A toolbox with every executable on the PATH except python* and another lighthouse: the first
# hit per name wins, as on the PATH itself. The hooks call `lighthouse`, so BIN gets that name in
# a directory of its own, whatever its file is called.
TOOLBOX="$WORK/toolbox"
BIN_DIR="$WORK/bin"
mkdir -p "$TOOLBOX" "$BIN_DIR"
ln -sf "$BIN" "$BIN_DIR/lighthouse"
IFS=: read -r -a path_dirs <<<"$PATH"
for dir in "${path_dirs[@]}"; do
  [ -d "$dir" ] || continue
  for exe in "$dir"/*; do
    name=${exe##*/}
    case "$name" in python* | lighthouse) continue ;; esac
    [ -f "$exe" ] && [ -x "$exe" ] || continue
    [ -e "$TOOLBOX/$name" ] || [ -L "$TOOLBOX/$name" ] || ln -sf "$exe" "$TOOLBOX/$name"
  done
done
export PATH="$BIN_DIR:$TOOLBOX"
export HOME="$WORK"
export LIGHTHOUSE_HOST_TOML="$WORK/host.toml"
export XDG_RUNTIME_DIR="$WORK"
if command -v "$needle" >/dev/null 2>&1; then
  echo "no-python: $needle is still on the PATH: $(command -v "$needle")" >&2
  exit 1
fi

# 2. A repository every pack recognises, one commit, then init.
mkdir -p "$REPO"
git -C "$REPO" init -q -b main
git -C "$REPO" config user.email no-python@example.invalid
git -C "$REPO" config user.name no-python
git -C "$REPO" config commit.gpgsign false
printf 'module example.com/nopython\n\ngo 1.22\n' >"$REPO/go.mod"
printf 'package main\n\nfunc main() {}\n' >"$REPO/main.go"
printf '{"name": "nopython", "version": "1.0.0"}\n' >"$REPO/package.json"
printf '{"expo": {"name": "nopython"}}\n' >"$REPO/app.json"
printf '#!/usr/bin/env bash\necho hello\n' >"$REPO/tool.sh"
chmod +x "$REPO/tool.sh"
printf '# nopython\n\nA repository without Python.\n' >"$REPO/README.md"
git -C "$REPO" add -A
git -C "$REPO" commit -q -m init

if ! (cd "$REPO" && "$BIN" init --yes --pack core --pack go --pack node --pack shell --pack expo) \
  >"$WORK/init.out" 2>"$WORK/init.err"; then
  fail "lighthouse init failed: $(head -3 "$WORK/init.err" | tr '\n' ' ')"
fi
for pack in core go node shell expo; do
  grep -q "^packages = .*\"$pack\"" "$REPO/.lighthouse/manifest.toml" 2>/dev/null \
    || fail "manifest.toml does not name the pack $pack"
done
[ -f "$REPO/.claude/settings.json" ] || { fail "init wrote no .claude/settings.json"; exit 1; }

# 3. Every registration, with its fixture.
# shellcheck disable=SC2016 # $ev is a jq variable, not a shell one
registrations=$("$JQ" -r '.hooks | to_entries[] | .key as $ev | .value[] | .hooks[] | [$ev, .command] | @tsv' \
  "$REPO/.claude/settings.json") || { fail "cannot read the registrations"; exit 1; }
[ -n "$registrations" ] || fail "settings.json holds no hook registration"
while IFS=$'\t' read -r event cmd; do
  [ -n "$event" ] || continue
  # `run.sh <name> [stand]` or `…/<name>.sh`.
  case "$cmd" in
    *run.sh\ *)
      read -r name stand <<<"${cmd##*run.sh }"
      ;;
    *.sh)
      name=${cmd##*/}
      name=${name%.sh}
      stand=
      ;;
    *)
      fail "$event: cannot name the hook of $cmd"
      continue
      ;;
  esac
  fixture="$FIXTURES/$event/$name${stand:+-$stand}.json"
  if [ ! -f "$fixture" ]; then
    fail "$event $name${stand:+ $stand}: no fixture $fixture"
    continue
  fi
  payload=$(sed -e "s|@REPO@|$REPO|g" -e "s|@TRANSCRIPT@|$TRANSCRIPT|g" "$fixture")
  out="$WORK/hook.out"
  err="$WORK/hook.err"
  (cd "$REPO" && printf '%s' "$payload" | CLAUDE_PROJECT_DIR="$REPO" bash -c "$cmd") >"$out" 2>"$err"
  rc=$?
  first=$(cat "$out" "$err" | grep -m1 -v '^[[:space:]]*$' | cut -c1-200)
  if [ "$rc" -ne 0 ]; then
    fail "$event $name${stand:+ $stand}: exit $rc: $first"
  elif grep -q -e 'COULD NOT CHECK' -e 'command not found' "$out" "$err"; then
    fail "$event $name${stand:+ $stand}: $(grep -h -m1 -e 'COULD NOT CHECK' -e 'command not found' "$out" "$err" | cut -c1-200)"
  else
    echo "ok $event $name${stand:+ $stand}${first:+: $first}"
  fi
done <<<"$registrations"

# 4. No file of the initialised repo names the interpreter.
if [ -n "${NO_PYTHON_EXTRA:-}" ]; then
  cp "$NO_PYTHON_EXTRA" "$REPO/" || fail "cannot copy $NO_PYTHON_EXTRA"
fi
hits=$(grep -rn "$needle" --exclude-dir=.git "$REPO")
grep_rc=$?
if [ "$grep_rc" -ge 2 ]; then
  fail "the $needle search failed (grep exit $grep_rc)"
fi
while IFS= read -r hit; do
  [ -n "$hit" ] || continue
  fail "$needle in $(printf '%s' "$hit" | cut -d: -f1,2)"
done <<<"$hits"

exit "$FAILED"
