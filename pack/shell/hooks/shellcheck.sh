#!/usr/bin/env bash
# PostToolUse hook: shellcheck op elk bewerkt shellscript.
# Matcher: Edit|Write. Tegenhanger van de skill paia-shell-gates (#650).
#
# Waarom deze hook NIET fail-open is, anders dan ruff-format.sh:
# die hook slaat stil over als ruff niet te starten is, omdat "de omgeving is
# stuk" daar niet als "de code is fout" mag lezen. Hier is stil overslaan zelf
# de fout die de hook moet voorkomen: shellcheck is gepind, sha256-geverifieerd
# en staat in de gedeelde toolcache, dus "ik kon niet kijken" is een defect en
# geen normale toestand. Ontbrekende tool = luid falen (exit 2), nooit exit 0.
#
# Twee blokkeerdrempels, met opzet twee verschillende sets:
#   - gate-shell (scripts/hooks/, tests/integration/, .claude/hooks/): alles,
#     tot en met style. Die set staat repo-breed op nul.
#   - alle overige .sh: error + warning. Ook nul, maar de 26 info- en 31
#     style-bevindingen in de rest van de repo zouden elke edit blokkeren op
#     schuld die niets met de wijziging te maken heeft.
set -uo pipefail

fail() { # reden...
  echo "shellcheck-gate: $*" >&2
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
  fail "jq noch python3 aanwezig, dus het bewerkte pad is niet te lezen." \
       "Er is NIET gecontroleerd."
fi

# Geen pad in de payload: deze tool-call raakte geen bestand. Niets te doen.
[ -n "$FILE_PATH" ] || exit 0

case "$FILE_PATH" in
  *.sh|*.bash) ;;
  *) exit 0 ;;
esac

# Verwijderd of hernoemd tussen edit en hook: niets om te controleren.
[ -f "$FILE_PATH" ] || exit 0

# Volgorde is belangrijk. De boom van het BEWERKTE bestand wint van
# $CLAUDE_PROJECT_DIR: die laatste wijst naar de hoofdcheckout, en een edit in
# een worktree zou dan met het script van een andere branch gecontroleerd worden
# - dezelfde verkeerde-boom-fout als #610 defect 3. Valt het bestand buiten een
# git-repo (een tmp-fixture), dan de boom van de hook zelf.
HOOK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
candidates=()
if root="$(git -C "$(dirname "$FILE_PATH")" rev-parse --show-toplevel 2>/dev/null)"; then
  candidates+=("$root")
fi
candidates+=("$HOOK_DIR/../.." "${CLAUDE_PROJECT_DIR:-}")

SCRIPT=""
for root in "${candidates[@]}"; do
  [ -n "$root" ] || continue
  if [ -x "$root/scripts/hooks/shellcheck-repo.sh" ]; then
    SCRIPT="$root/scripts/hooks/shellcheck-repo.sh"
    break
  fi
done
if [ -z "$SCRIPT" ]; then
  fail "shellcheck-repo.sh niet gevonden (gezocht in: ${candidates[*]})." \
       "Er is NIET gecontroleerd."
fi

# Beide vormen: een payload kan een absoluut pad dragen of een repo-relatief.
severity=warning
case "$FILE_PATH" in
  */scripts/hooks/*|*/tests/integration/*|*/.claude/hooks/*) severity=style ;;
  scripts/hooks/*|tests/integration/*|.claude/hooks/*) severity=style ;;
esac

out=$("$SCRIPT" --severity "$severity" -- "$FILE_PATH" 2>&1)
rc=$?

case "$rc" in
  0) exit 0 ;;
  1)
    echo "shellcheck-gate: los dit op in de code. Moet een regel echt uit, zet er" >&2
    echo "een reden bij: '# shellcheck disable=SCxxxx <reden>' op de regel erboven." >&2
    echo "Stil onderdrukken niet." >&2
    echo "" >&2
    echo "$out" >&2
    exit 2
    ;;
  *)
    fail "shellcheck-repo.sh gaf rc=$rc - er is NIET gecontroleerd:" "$out"
    ;;
esac
