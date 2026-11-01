#!/usr/bin/env bash
# Scan every push for secrets before it leaves this machine (#194).
# Documentation: docs/secret-scan.md (#198).
#
# Why this exists. lighthouse records agent sessions with entire: a checkpoint is a
# verbatim copy of the transcript, so it holds everything an agent read, a gitignored
# .env included. entire 0.11.2 did not redact a GitHub PAT in an assistant message
# (measured in #194). CorneeldH/lighthouse is public, so checkpoints go to a separate
# private repo, and this hook scans everything a push carries, to any remote.
#
# How entire pushes, measured on 0.11.2 (#194, with GIT_TRACE and a fixture):
#
#   - entire's own line in .git/hooks/pre-push pushes the checkpoint refs itself, with
#     `git push --no-verify --porcelain <checkpoint remote> refs/entire/checkpoints/...`.
#     --no-verify skips every pre-push hook, so no hook ever sees those refs, and a
#     hook chained after entire's line (pre-push.pre-entire) runs only after they left.
#   - With strategy_options.push_sessions false that line pushes nothing. Then the
#     only way out for a checkpoint is a plain `git push`, which does pass this hook.
#
# Hence three rules on top of the scan:
#
#   1. tools/hooks/install.sh puts this hook ABOVE entire's line, not in the chain.
#   2. While entire may push by itself (enabled, push_sessions not false), every push
#      is refused: the checkpoints would leave unscanned.
#   3. entire's refs go only to the checkpoint remote: refs/entire/ (the git-refs
#      backend, one ref per checkpoint) and refs/heads/entire/ (shadow branches and
#      the legacy entire/checkpoints/v1). Match on these prefixes, on both sides of
#      the ref line: a fixed ref name once matched nothing and the hook passed
#      everything (personal_ai_agent #455).
#
# What is scanned, in two modes (#198), each gitleaks run bounded by
# tools/hooks/checkpoint_scan.py (systemd scope with a memory ceiling, no swap, a
# CPU quota; on 2026-11-01 an unbounded gitleaks over every checkpoint pushed the
# host into swap):
#
#   - refs/entire/* (by local or remote ref), the checkpoints: `checkpoint_scan.py
#     scan`, every blob the remote lacks, once, minus the blobs a ledger already saw
#     clean. Above 256 MB unscanned the hook refuses and points to
#     `checkpoint_scan.py push`.
#   - Every other ref, refs/heads/entire/* included (shadow branches sit on the work
#     branch, so blob mode would read its whole history): the commits the remote
#     lacks, not the tree of the tip. A secret added in one commit and removed in
#     the next stays reachable on the remote after the push (the branch,
#     refs/pull/N/head), so the range is what counts. All those refs go into one
#     `gitleaks git` run: `<local shas> ^<remote shas> --not --remotes=<remote>`.
#
# A memory ceiling hit or a scan that could not complete blocks the push, with its
# own message rather than "found a secret".
#
# Lessons taken over from personal_ai_agent:
#
#   - Without gitleaks the push is refused; only a remote that is a local path may pass
#     unscanned.
#   - One scan, one config, the same .gitleaks.toml as CI. A second mode without
#     --config was red on every push there.
#   - A secret in tool output is in the local git objects at once. This hook keeps it
#     off the remote; rotating the secret is the real fix.
set -euo pipefail

# Resolve the config from the script's own location, not the caller's cwd: the hook
# runs from whichever worktree pushes, or from the main checkout's copy (install.sh).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
GITLEAKS_CONFIG="$SCRIPT_DIR/../../.gitleaks.toml"
CHECKPOINT_SCAN="$SCRIPT_DIR/checkpoint_scan.py"

remote="${1:-}"
url="${2:-}"

die() {
  echo "pre-push: $*" >&2
  exit 1
}

# A remote is "local" only when its URL is a plain filesystem path: a throwaway test
# or backup clone. An empty or unknown URL counts as a real remote.
remote_is_local() {
  case "$url" in
    file://*|/*|./*|../*) return 0 ;;
    *) return 1 ;;
  esac
}

toplevel="$(git rev-parse --show-toplevel)" || die "COULD NOT CHECK — not inside a work tree."

# Without the module no scan is bounded, and entire's settings are read through it:
# refuse rather than run gitleaks unbounded.
[ -f "$CHECKPOINT_SCAN" ] || die "cannot find $CHECKPOINT_SCAN — every scan runs bounded through it."
command -v python3 >/dev/null 2>&1 || die "COULD NOT CHECK — python3 is needed to read .entire/settings.json."
# entire's effective settings (.entire/settings.json, .entire/settings.local.json on
# top) and the checkpoint-remote rule live in the module, which `checkpoint_scan.py
# push` shares: two lines, whether entire may push by itself and whether this push
# goes to the checkpoint remote (by the remote's name when entire has a checkpoint
# push remote, else by the URL of the checkpoint repo).
settings="$(python3 -I "$CHECKPOINT_SCAN" entire-remote --toplevel "$toplevel" "$remote" "$url")" ||
  die "COULD NOT CHECK — could not read $toplevel/.entire/settings*.json."
{
  read -r entire_self_push
  read -r checkpoint_remote
} <<<"$settings"

if [ "$entire_self_push" = "yes" ]; then
  echo "pre-push: BLOCKED — entire may push checkpoints by itself here." >&2
  echo "          It does so with 'git push --no-verify', past this scan (#194)." >&2
  echo "          Set strategy_options.push_sessions to false in .entire/settings.json." >&2
  exit 1
fi

is_checkpoint_remote() {
  [ "$checkpoint_remote" = "yes" ]
}

is_entire_ref() {
  case "$1" in
    refs/entire/*|refs/heads/entire/*) return 0 ;;
    *) return 1 ;;
  esac
}

# The checkpoints proper (entire's git-refs backend), scanned per blob. Not
# refs/heads/entire/*: those go through the commit range.
is_checkpoint_ref() {
  case "$1" in
    refs/entire/*) return 0 ;;
    *) return 1 ;;
  esac
}

# <ref>=<sha> per checkpoint ref, for checkpoint_scan.py scan.
checkpoints=()
tips=()
excludes=()
labels=()
seen=" "
while read -r local_ref local_sha remote_ref remote_sha; do
  [ -n "${local_ref:-}" ] || continue
  # Ref deletion: the local sha is all zeros, nothing is sent.
  case "$local_sha" in
    *[!0]*) ;;
    *) continue ;;
  esac
  if is_entire_ref "$local_ref" || is_entire_ref "${remote_ref:-}"; then
    if ! is_checkpoint_remote; then
      echo "pre-push: BLOCKED — $local_ref -> $remote_ref is an entire ref, and '$remote' ($url) is not the checkpoint remote." >&2
      echo "          Checkpoints hold agent transcripts; they go only to the private checkpoint remote." >&2
      exit 1
    fi
  fi
  if is_checkpoint_ref "$local_ref" || is_checkpoint_ref "${remote_ref:-}"; then
    checkpoints+=("$local_ref=$local_sha")
    continue
  fi
  labels+=("$local_ref -> ${remote_ref:-?}")
  case "$seen" in *" $local_sha "*) ;; *) tips+=("$local_sha"); seen="$seen$local_sha " ;; esac
  # What the remote already has needs no second look. A remote sha we do not have
  # locally (a force push over unfetched work) cannot be excluded; --remotes still is.
  case "${remote_sha:-}" in
    *[!0]*) git cat-file -e "$remote_sha^{commit}" 2>/dev/null && excludes+=("^$remote_sha") ;;
  esac
done

refs=$(( ${#checkpoints[@]} + ${#tips[@]} ))
[ "$refs" -gt 0 ] || exit 0

if ! command -v gitleaks >/dev/null 2>&1; then
  echo "pre-push: gitleaks is not installed, so this push cannot be scanned for secrets." >&2
  echo "          install: https://github.com/gitleaks/gitleaks#installing (CI pins 8.30.1)" >&2
  if remote_is_local; then
    echo "pre-push: remote '$remote' is a local path — allowing the push unscanned." >&2
    exit 0
  fi
  die "refusing to push $refs ref(s) to '$remote' ($url)."
fi

# Without the config the scan is noisier than CI's, so a missing config is an error
# rather than a silent fallback.
[ -f "$GITLEAKS_CONFIG" ] || die "cannot find $GITLEAKS_CONFIG — scans must use the same config as CI."

# A scan that passed says nothing, except why it had no memory ceiling.
say_no_ceiling() {
  local line
  while IFS= read -r line; do
    case "$line" in "secret-scan: no memory ceiling"*) echo "$line" >&2 ;; esac
  done <<<"$1"
}

# rc 1: findings (or gitleaks failed the way it fails on findings); 2 and anything
# else: the scan did not complete, a memory ceiling hit included.
block() {
  local rc="$1" what="$2" out="$3"
  shift 3
  if [ "$rc" -eq 1 ]; then
    echo "pre-push: BLOCKED — gitleaks found a secret (or could not scan) in the $what this push sends:" >&2
    [ "$#" -eq 0 ] || printf '          %s\n' "$@" >&2
    printf '%s\n' "$out" >&2
    echo "          Rotate the secret, then rewrite the commit or drop the checkpoint before pushing." >&2
    echo "          A false positive belongs in .gitleaks.toml, so CI agrees with this hook." >&2
  else
    case "$out" in
      # A refusal of the module's own (the bulk volume): it says what to do.
      "pre-push: BLOCKED"*) printf '%s\n' "$out" >&2 ;;
      *)
        echo "pre-push: BLOCKED — the scan did not complete:" >&2
        printf '%s\n' "$out" >&2
        ;;
    esac
  fi
  exit 1
}

if [ "${#checkpoints[@]}" -gt 0 ]; then
  rc=0
  out="$(python3 -I "$CHECKPOINT_SCAN" scan --remote "$remote" --config "$GITLEAKS_CONFIG" \
    "${checkpoints[@]}" 2>&1)" || rc=$?
  [ "$rc" -eq 0 ] || block "$rc" "checkpoints" "$out"
  say_no_ceiling "$out"
fi

if [ "${#tips[@]}" -gt 0 ]; then
  log_opts="${tips[*]}"
  [ "${#excludes[@]}" -eq 0 ] || log_opts="$log_opts ${excludes[*]}"
  log_opts="$log_opts --not --remotes=$remote"

  rc=0
  out="$(python3 -I "$CHECKPOINT_SCAN" run-bounded -- \
    gitleaks git "$toplevel" --log-opts="$log_opts" --config "$GITLEAKS_CONFIG" \
    --no-banner --redact --exit-code 1 2>&1)" || rc=$?
  [ "$rc" -eq 0 ] || block "$rc" "commits" "$out" "${labels[@]}"
  say_no_ceiling "$out"
fi
exit 0
