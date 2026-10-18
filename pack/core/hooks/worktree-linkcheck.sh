#!/bin/bash
# PostToolUse hook: markdown-links moeten relatief zijn én bestaan (#886).
# Matcher: Edit|Write, self-filtert op .md. Blokkerend: exit 2 + stderr.
#
# Tegenhanger (pairing-regel #624): de linkregel in AGENTS.md draagt norm en
# rationale vooraf; deze hook toetst het geschreven bestand achteraf.
#
# Norm (#886, AC 2): een relatieve markdown-link moet bestaan, opgelost tegen
# de map van het bestand — overal, ook een link naar een bestand dat alleen op
# deze branch staat (een plan dat naar een net gecommitte spec linkt is geen
# probleem meer). Een absolute link is altijd een probleem, ongeacht bestaan:
# dood op de gerenderde site en zodra de worktree weg is.
#
# De naam `worktree-linkcheck.sh` blijft, ook al toetst de hook niet langer
# specifiek de worktree-situatie: hernoemen schuift alleen de registratie in
# .claude/settings.json en check 8 (scripts/hooks/check-instructies.sh) mee,
# zonder inhoudelijke winst. De chathelft van de AGENTS.md-regel "Links:
# bestand relatief, chat absoluut" — in chat het volledige absolute pad —
# blijft zonder hook: die gaat over prose in een chatantwoord, niet over een
# bestand dat deze hook ziet.
#
# Bewust geen projectfilter, anders dan pre-commit-tests.sh (#1014): die gate
# filtert omdat hij de suite van déze repo draait. Deze hook toetst een bestand
# tegen de wortel van zijn eigen repo; zijn oordeel klopt dus in elke repo, en
# een filter liet daar een dode link stil passeren. Een map buiten elke
# git-repo heeft geen wortel om tegen op te lossen en valt buiten de toets.
#
# NIET fail-open (paia-shell-gates): zonder jq of python3, of als de
# linkparser faalt, is er niet gekeken; een stille exit 0 leest dan als "alle
# links zijn goed".

MISSING_TOOLS=""
command -v jq >/dev/null 2>&1 || MISSING_TOOLS="$MISSING_TOOLS jq"
command -v python3 >/dev/null 2>&1 || MISSING_TOOLS="$MISSING_TOOLS python3"
if [ -n "$MISSING_TOOLS" ]; then
  {
    echo "Linkcheck: KON NIET CONTROLEREN — ontbrekende tooling:$MISSING_TOOLS"
    echo ""
    echo "Deze hook toetst of markdown-links relatief zijn en bestaan."
    echo "Zonder die tooling is er niet gekeken; dat is geen groen licht."
  } >&2
  exit 2
fi

# `realpath -m` zit alleen in GNU coreutils; de BSD-realpath op macOS weigert
# de vlag. Zie vault-write-guard.sh voor de volledige redenering.
canon_path() {
  realpath -m "$1" 2>/dev/null && return 0
  python3 -c 'import os, sys; print(os.path.realpath(sys.argv[1]))' "$1" 2>/dev/null
}

INPUT=$(cat)
FILE_PATH=$(echo "$INPUT" | jq -r '.tool_input.file_path // empty')
if [ -z "$FILE_PATH" ]; then
  exit 0
fi

case "$FILE_PATH" in
  *.md|*.markdown) ;;
  *) exit 0 ;;
esac

CWD=$(echo "$INPUT" | jq -r '.cwd // empty')
case "$FILE_PATH" in
  /*) ABS="$FILE_PATH" ;;
  *)  ABS="${CWD:-$PWD}/$FILE_PATH" ;;
esac
ABS=$(canon_path "$ABS") || exit 0
if [ ! -f "$ABS" ]; then
  exit 0
fi

DIR=$(dirname "$ABS")
ROOT=$(git -C "$DIR" rev-parse --show-toplevel 2>/dev/null) || exit 0
if [ -z "$ROOT" ]; then
  exit 0
fi

# Bewijs dat dit codepad geraakt is. Zonder marker toetst een groene test niet
# of er gekeken is of dat de hook er vroeg uit stapte.
if [ -n "${PAIA_LINKCHECK_TRACE:-}" ]; then
  echo "linkcheck: toetst in $ROOT" >&2
fi

# Geen `|| exit 0` en geen `2>/dev/null`: een klappende parser die als "geen
# problemen gevonden" wordt gelezen is defect 4 uit paia-shell-gates.
if ! PROBLEMS=$(python3 "$(dirname "$0")/lib/md_links.py" "$ABS" "$ROOT" 2>&1); then
  {
    echo "Linkcheck: KON NIET CONTROLEREN — de linkparser faalde."
    echo ""
    echo "$PROBLEMS"
  } >&2
  exit 2
fi
if [ -z "$PROBLEMS" ]; then
  exit 0
fi

MESSAGE=""
while IFS=$'\t' read -r KIND TARGET; do
  [ -n "$KIND" ] || continue
  case "$KIND" in
    dood)
      REDEN="bestaat niet, opgelost tegen de map van het bestand"
      ;;
    absoluut)
      REDEN="een absoluut pad is dood op de gerenderde site en zodra de worktree weg is; gebruik een relatief pad"
      ;;
    *)
      REDEN="onbekend soort probleem"
      ;;
  esac
  MESSAGE="$MESSAGE  $TARGET — $REDEN"$'\n'
done <<< "$PROBLEMS"

{
  echo "Linkcheck: markdown-link(s) die niet relatief-en-bestaand zijn."
  echo ""
  printf '%s' "$MESSAGE"
  echo "In: $ABS"
} >&2
exit 2
