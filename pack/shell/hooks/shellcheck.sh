#!/usr/bin/env bash
# PostToolUse hook: shellcheck op elk bewerkt shellscript.
# Matcher: Edit|Write. Tegenhanger van de skill shell-gates.
#
# Waarom deze hook NIET fail-open is: stil overslaan is hier zelf de fout die de
# hook moet voorkomen. shellcheck is gepind, sha256-geverifieerd en staat in de
# gedeelde toolcache, dus "ik kon niet kijken" is een defect en geen normale
# toestand. Ontbrekende tool = luid falen (exit 2, eerste regel
# `shellcheck: COULD NOT CHECK — …`), nooit exit 0.
#
# Twee blokkeerdrempels, met opzet twee verschillende sets:
#   - de mappen in `shell.strict_dirs` (default `.claude/hooks`): alles, tot en
#     met style.
#   - alle overige .sh: error + warning. Info- en style-bevindingen in de rest
#     van een repo zouden elke edit blokkeren op schuld die niets met de
#     wijziging te maken heeft.
set -uo pipefail

fail() { # reden...
  echo "shellcheck: COULD NOT CHECK — $*" >&2
  exit 2
}

INPUT=$(cat)

# Twee parsers, want deze hook draait op ELKE Edit/Write - ook op .py en .md.
# Luid falen omdat jq ontbreekt zou dan de hele sessie blokkeren op iets dat
# niets met shell te maken heeft. Zelf JSON uit elkaar peuteren met grep is de
# andere kant van dezelfde fout, dus: jq, anders python3, en pas als beide
# ontbreken luid falen.
if command -v jq >/dev/null 2>&1; then
  FILE_PATH=$(printf '%s' "$INPUT" | jq -r '.tool_input.file_path // empty')
elif command -v python3 >/dev/null 2>&1; then
  FILE_PATH=$(printf '%s' "$INPUT" |
    python3 -c 'import json,sys; print(json.load(sys.stdin).get("tool_input",{}).get("file_path",""))')
else
  fail "neither jq nor python3 is present, so the edited path cannot be read."
fi

# Geen pad in de payload: deze tool-call raakte geen bestand. Niets te doen.
[ -n "$FILE_PATH" ] || exit 0

case "$FILE_PATH" in
  *.sh|*.bash) ;;
  *) exit 0 ;;
esac

# Verwijderd of hernoemd tussen edit en hook: niets om te controleren.
[ -f "$FILE_PATH" ] || exit 0

# De lighthouse-root: LIGHTHOUSE_REPO_ROOT, anders de boom van het BEWERKTE
# bestand, en pas daarna die van de hook. $CLAUDE_PROJECT_DIR wijst naar de
# hoofdcheckout, en een edit in een worktree zou dan met het script van een
# andere branch gecontroleerd worden.
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

# De strenge mappen zijn repo-relatief: vergelijk het pad binnen de git-boom van
# het bestand, anders binnen de lighthouse-root.
if ! STRICT_DIRS=$(LIGHTHOUSE_REPO_ROOT="$ROOT" python3 "$ROOT/.lighthouse/lib/lighthouse_lib/config.py" shell.strict_dirs 2>&1); then
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

# stderr apart: een afwijkende shellcheck-versie meldt zich daar, ook bij rc 0.
ERR_FILE="$(mktemp)" || fail "mktemp failed."
out=$("$SCRIPT" --severity "$severity" -- "$FILE_PATH" 2>"$ERR_FILE")
rc=$?
err="$(cat "$ERR_FILE")"
rm -f "$ERR_FILE"

case "$rc" in
  0)
    # Schoon, maar mogelijk met een andere versie dan de pin: niet stil.
    if [ -n "$err" ]; then
      msg="shellcheck: $err"
      if command -v jq >/dev/null 2>&1; then
        jq -cn --arg m "$msg" '{systemMessage: $m}'
      else
        MSG="$msg" python3 -c 'import json,os; print(json.dumps({"systemMessage": os.environ["MSG"]}))'
      fi
    fi
    exit 0
    ;;
  1)
    echo "shellcheck: fix this in the code. If a rule really has to go, give a reason:" >&2
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
