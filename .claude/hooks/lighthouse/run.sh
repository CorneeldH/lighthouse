#!/bin/sh
# lighthouse: core@dev
# run.sh <hook> [stand]: runs `lighthouse hook <hook> [stand]` with the payload on stdin.
#
# At `version = "dev"` in lighthouse's own checkout (cmd/lighthouse/main.go exists) it prefers
# .lighthouse/bin/lighthouse, built from this checkout in the background whenever the binary is
# missing or its stamp is not HEAD; `dev` elsewhere counts as a release. A hook never waits for
# that build: it runs on the previous checkout binary, or on `lighthouse` from the PATH.
# Without any binary, or when the binary does not know the hook, the hooks in `blocking`
# block (exit 2, message on stderr) and the others pass with a systemMessage.
# Once the binary runs, its stdout, stderr and exit code pass through untouched.

here=$(cd -P "$(dirname "$0")" && pwd) || exit 1
root=$(cd -P "$here/../../.." && pwd) || exit 1
bin="$root/.lighthouse/bin"
manifest="$root/.lighthouse/manifest.toml"
hook="${1:-}"

dev=false
never_block=false
if grep -q '^version = "dev"' "$manifest" 2>/dev/null && [ -f "$root/cmd/lighthouse/main.go" ]; then
  dev=true
fi

is_blocking() {
  [ "$never_block" = true ] && return 1
  # Without the list every hook counts as blocking: fail closed.
  [ -f "$here/blocking" ] || return 0
  grep -qxF -- "$hook" "$here/blocking"
}

# json_string: stdin as one JSON string, without the trailing newline.
json_string() {
  tab=$(printf '\t')
  sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' -e "s/$tab/\\\\t/g" |
    awk 'BEGIN { printf "%s", "\"" } NR > 1 { printf "%s", "\\n" } { printf "%s", $0 } END { printf "%s", "\"" }'
}

# fail <text>: the hook could not run; block or warn per the list.
fail() {
  if is_blocking; then
    printf '%s\n' "$1" >&2
    exit 2
  fi
  printf '{"systemMessage": %s}\n' "$(printf '%s\n' "$1" | json_string)"
  exit 0
}

remedy() {
  if [ "$dev" = true ]; then
    printf '%s' "this checkout has version = \"dev\": run go install ./cmd/lighthouse here (a build into .lighthouse/bin/ starts by itself when go is on the PATH), or create .lighthouse/bin/use-path to use the lighthouse on the PATH"
    return
  fi
  # The one place the module path stands, allowed in ci/literal-allowlist.txt.
  module=github.com/CorneeldH/lighthouse/cmd/lighthouse
  version=$(sed -n 's/^version = "v*\([^"]*\)"$/\1/p' "$manifest" 2>/dev/null | head -n 1)
  if [ -n "$version" ] && [ "$version" != dev ]; then
    printf '%s' "install v$version: go install $module@v$version, or download it from the release"
  else
    printf '%s' "install it: go install $module@latest, or download it from the release"
  fi
}

# ref <file> <h:mm>: give <file> the mtime of now minus h:mm. A POSIX TZ offset shifts `date`,
# so no `date -d` or `date -r` is needed; `find -newer` then compares to the second.
ref() {
  TZ=UTC0 touch -t "$(TZ="UTC$2" date +%Y%m%d%H%M.%S)" "$1" 2>/dev/null
}

# The build runs detached: stdin, stdout and stderr on /dev/null, its own session where setsid
# exists (Linux), nohup otherwise (macOS). It writes to a temporary file, moves it into place,
# then writes the stamp, and only then removes the lock.
# shellcheck disable=SC2016 # expanded by the detached sh, from its positional parameters
BUILD='cd "$1" || exit 1
tmp="$2/lighthouse.tmp.$$"
if go build -o "$tmp" ./cmd/lighthouse >"$2/build.log" 2>&1; then
  mv -f "$tmp" "$2/lighthouse" && printf "%s\n" "$3" >"$2/stamp.tmp" && mv -f "$2/stamp.tmp" "$2/stamp" && rm -f "$2/build.failed"
else
  printf "%s\n" "$3" >"$2/build.failed"
  rm -f "$tmp"
fi
rmdir "$2/build.lock"'

start_build() {
  mkdir -p "$bin" 2>/dev/null || return 0
  # A failed build for this HEAD pauses new builds for 60 seconds.
  if [ -f "$bin/build.failed" ] && [ "$(cat "$bin/build.failed" 2>/dev/null)" = "$head" ] &&
    ref "$bin/pause.ref" 0:01 &&
    [ -n "$(find "$bin/build.failed" -newer "$bin/pause.ref" 2>/dev/null)" ]; then
    return 0
  fi
  if ! mkdir "$bin/build.lock" 2>/dev/null; then
    # Someone else builds, unless the lock is older than 120 seconds: then it was abandoned.
    ref "$bin/lock.ref" 0:02 || return 0
    [ -n "$(find "$bin/build.lock" -prune ! -newer "$bin/lock.ref" 2>/dev/null)" ] || return 0
    rm -rf "$bin/build.lock"
    mkdir "$bin/build.lock" 2>/dev/null || return 0
  fi
  if command -v setsid >/dev/null 2>&1; then
    setsid sh -c "$BUILD" sh "$root" "$bin" "$head" </dev/null >/dev/null 2>&1 &
  else
    nohup sh -c "$BUILD" sh "$root" "$bin" "$head" </dev/null >/dev/null 2>&1 &
  fi
}

LH=
if [ "$dev" = true ] && [ ! -e "$bin/use-path" ] && command -v go >/dev/null 2>&1; then
  head=$(git -C "$root" rev-parse HEAD 2>/dev/null)
  if [ -n "$head" ] && { [ ! -x "$bin/lighthouse" ] || [ "$(cat "$bin/stamp" 2>/dev/null)" != "$head" ]; }; then
    start_build
  fi
  if [ -x "$bin/lighthouse" ]; then
    LH="$bin/lighthouse"
  fi
fi
if [ -z "$LH" ]; then
  LH=$(command -v lighthouse 2>/dev/null)
fi
if [ -z "$LH" ]; then
  fail "COULD NOT CHECK: lighthouse not on PATH — $(remedy)"
fi
# The binary finds the repo by its cwd otherwise, and a hook may run from anywhere.
LIGHTHOUSE_REPO_ROOT="${LIGHTHOUSE_REPO_ROOT:-$root}"
export LIGHTHOUSE_REPO_ROOT

err=$(mktemp "${TMPDIR:-/tmp}/lighthouse-run.XXXXXX" 2>/dev/null) || err=
if [ -z "$err" ]; then
  # No place for stderr: run without the mapping rather than not at all.
  exec "$LH" hook "$@"
fi
"$LH" hook "$@" 2>"$err"
rc=$?
# An unknown hook: the binary is older than the registration. At dev that is the checkout
# binary of the previous HEAD, or the PATH binary while the first build runs: warn, never block.
if [ "$rc" -eq 3 ]; then
  text=$(cat "$err")
  rm -f "$err"
  never_block=$dev
  fail "$text"
fi
if [ "$rc" -eq 1 ] && grep -q 'unknown command "hook"' "$err"; then
  rm -f "$err"
  never_block=$dev
  fail "COULD NOT CHECK: lighthouse has no hook command ($LH is older than this repo) — $(remedy)"
fi
cat "$err" >&2
rm -f "$err"
exit "$rc"
