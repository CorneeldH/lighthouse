#!/usr/bin/env bash
# Run shellcheck on the shell of this repo, or on the files you pass.
#
# Without arguments it runs the *strict set*: the shell in `shell.strict_dirs`
# from lighthouse.toml (default `.claude/hooks`), the shell that itself guards
# gates. That set stands at zero findings at the lowest severity (style) - that is
# KPI row 1 of the shell-standard skill.
#
# `--check-floor <code,...> [dir]` checks only that those codes are on for a script in
# dir; the shell-lint-check hook runs it before the lint run (see check_floor below).
#
# Exit codes, and this is the whole point of this script:
#   0  checked, nothing found
#   1  checked, findings
#   2  called wrongly
#   4  NOT checked (shellcheck is missing or fails) - neither green nor red
#
# Three outcomes, not two. "Could not look" is not "found nothing".
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/shellcheck-bin.sh
. "$HERE/lib/shellcheck-bin.sh"

usage() {
  echo "usage: $0 [--severity error|warning|info|style] [file ...]" >&2
  echo "       $0 --check-floor <code,code,...> [dir]" >&2
  echo "  without files: the strict set (shell.strict_dirs in lighthouse.toml)" >&2
}

# The smallest script that makes shellcheck report code, on stdout; rc 1 for a code
# this script has no probe for.
# shellcheck disable=SC2016 # each probe is source text for shellcheck, never expanded here
floor_probe() { # code
  case "$1" in
    SC2164) printf '%s\n' 'cd /tmp' ;;
    SC2046) printf '%s\n' 'touch $(date +%s)' ;;
    SC2115) printf '%s\n' 'd=$1' 'rm -rf "$d/"*' ;;
    SC2155) printf '%s\n' 'export stamp="$(date)"' ;;
    SC2034) printf '%s\n' 'unused=1' ;;
    *) return 1 ;;
  esac
}

# --check-floor: is every code of the list on for a script in dir (default: the current
# directory)? Each probe goes to shellcheck on stdin from dir, so shellcheck reads the
# configuration it would read for a script there: the .shellcheckrc above it, the one in
# $HOME or $XDG_CONFIG_HOME, SHELLCHECK_OPTS, code ranges in `disable=`. The answer is the
# effective result, not a reading of a configuration file, and there is no --norc: that
# would also drop the checks an adopter switched on.
#   0  every code fires
#   1  checked, a code is off: the missing codes on stdout, one per line
#   2  called wrongly (no list, a code without a probe)
#   4  NOT checked (shellcheck missing or failing)
check_floor() { # codes [dir]
  local codes="$1" dir="${2:-.}" code out rc list=() missing=()
  [ -n "$codes" ] || { usage; exit 2; }
  IFS=, read -ra list <<<"$codes"
  for code in "${list[@]}"; do
    floor_probe "$code" >/dev/null || {
      echo "shellcheck-repo: --check-floor has no probe for $code" >&2
      exit 2
    }
  done
  # Resolved here, before the cd: the shellcheck of the lint run is resolved from the
  # caller's directory too, also for a script outside any git repository.
  SHELLCHECK="$(resolve_shellcheck)" || exit "$SHELLCHECK_UNAVAILABLE"
  cd "$dir" || exit 2
  for code in "${list[@]}"; do
    # -S warning as in the lint run outside the strict dirs: a severity from
    # SHELLCHECK_OPTS gives way to it there too.
    # A here-string, not a pipe: under pipefail a shellcheck that never reads stdin would
    # make the writer die of SIGPIPE and look like rc 141.
    probe=$(floor_probe "$code")
    out=$("$SHELLCHECK" -s bash -x -S warning -f gcc - <<<"$probe")
    rc=$?
    case "$rc" in
      0|1) ;;
      *)
        echo "shellcheck-repo: shellcheck exited with rc=$rc on the floor probe of $code, so the floor was not checked" >&2
        exit "$SHELLCHECK_UNAVAILABLE"
        ;;
    esac
    case "$out" in
      *"[$code]"*) ;;
      *) missing+=("$code") ;;
    esac
  done
  [ "${#missing[@]}" -eq 0 ] && exit 0
  printf '%s\n' "${missing[@]}"
  exit 1
}

severity=style
files=()
while [ "$#" -gt 0 ]; do
  case "$1" in
    --check-floor)
      [ "$#" -ge 2 ] && [ "$#" -le 3 ] || { usage; exit 2; }
      check_floor "$2" "${3:-}"
      ;;
    --severity)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      case "$2" in
        error|warning|info|style) severity="$2" ;;
        *) usage; exit 2 ;;
      esac
      shift 2
      ;;
    -h|--help) usage; exit 0 ;;
    --) shift; files+=("$@"); break ;;
    -*) usage; exit 2 ;;
    *) files+=("$1"); shift ;;
  esac
done

SHELLCHECK="$(resolve_shellcheck)" || exit "$SHELLCHECK_UNAVAILABLE"

if [ "${#files[@]}" -eq 0 ]; then
  REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null)" || {
    echo "shellcheck-repo: not in a git repository and no files given" >&2
    exit 2
  }
  cd "$REPO_ROOT" || exit 2
  # The directories where the bar is highest, because a bug in a gate is quieter
  # than a bug in ordinary code: it reports green.
  LIB_ROOT="${LIGHTHOUSE_REPO_ROOT:-$REPO_ROOT}"
  if ! dirs_out="$(cd "$LIB_ROOT" && lighthouse config shell.strict_dirs 2>&1)"; then
    echo "shellcheck-repo: shell.strict_dirs could not be read: $dirs_out" >&2
    exit "$SHELLCHECK_UNAVAILABLE"
  fi
  specs=()
  while IFS= read -r dir; do
    [ -n "$dir" ] && specs+=("${dir%/}/*.sh")
  done <<EOF
$dirs_out
EOF
  # git ls-files instead of a glob: no empty expansion that gets passed on as a literal
  # file name, and recursive into subdirectories.
  # --others --exclude-standard is not optional: without those two the gate
  # does not see a brand-new, not yet tracked hook at all and reports green
  # about a set that the new file was not in.
  if [ "${#specs[@]}" -gt 0 ]; then
    while IFS= read -r f; do
      files+=("$f")
    done < <(git ls-files --cached --others --exclude-standard -- "${specs[@]}")
  fi
  if [ "${#files[@]}" -eq 0 ]; then
    echo "shellcheck-repo: no .sh found in the strict set - this is not green" >&2
    exit 2
  fi
fi

# -x: follow `source`/`.` into the sourced file. Without it shellcheck reports
# SC1091 ("not following") on every script that loads lib/ — a finding about the
# check itself instead of about the code.
# The rc of shellcheck itself: 0 clean, 1 findings. Any other rc (2 a file that
# it could not read, 3 and 4 wrong options, or a crash) is not checked.
"$SHELLCHECK" -x -S "$severity" "${files[@]}"
rc=$?
case "$rc" in
  0|1) exit "$rc" ;;
esac
echo "shellcheck-repo: shellcheck exited with rc=$rc, so nothing was checked" >&2
exit "$SHELLCHECK_UNAVAILABLE"
