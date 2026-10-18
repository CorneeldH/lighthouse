#!/bin/bash
# PreToolUse hook: toets de body van elke `gh pr create` (#1091).
# Matcher: Bash. Blokkerend: exit 2 + stderr. Geslaagd: exit 0, meldingen als
# JSON op stdout.
#
# Tegenhanger: paia-verify-pr
# De skill draagt de norm vooraf (body bouwen met Closes-bewijs); deze hook
# dwingt hem af op het moment van `gh pr create`: een body zonder
# `Closes #N` + Evidence-tabel, `Refs #N — <reden>` of `No issue: <reden>`
# blokkeert. Ontwerp: docs/developer/planning/specs/2026-10-03-pr-verifieert-issue-design.md,
# sectie 3. De toetsing zelf staat in lib/pr_body_gate.py.
#
# NIET fail-open (paia-shell-gates): zonder tooling, bij een commando dat niet
# te parsen is of bij een parser die stukloopt is er niet getoetst — dat is
# geen groen licht en blokkeert met KON NIET CONTROLEREN. De enige luide
# doorlaat is de AC-telling (gh weg of stuk): de lib meldt dat op stdout en de
# hook geeft die melding door aan gebruiker en model.
#
# De lib-deadline (DEADLINE_S = 12 s in lib/pr_body_gate.py) moet onder de hook-timeout
# (15 s in .claude/settings.json) blijven: een verlopen hook laat de tool-call door.

HOOK_NAME="pr-create-gate"

MISSING_TOOLS=""
command -v jq >/dev/null 2>&1 || MISSING_TOOLS="$MISSING_TOOLS jq"
command -v python3 >/dev/null 2>&1 || MISSING_TOOLS="$MISSING_TOOLS python3"
if [ -n "$MISSING_TOOLS" ]; then
  {
    echo "$HOOK_NAME: KON NIET CONTROLEREN — ontbrekende tooling:$MISSING_TOOLS"
    echo ""
    echo "Deze hook toetst de body van elke 'gh pr create'."
    echo "Zonder die tooling is er niet getoetst; dat is geen groen licht."
  } >&2
  exit 2
fi

HOOK_DIR=$(cd "$(dirname "$0")" && pwd)
LIB="$HOOK_DIR/lib"

INPUT=$(cat)
if ! CMD=$(printf '%s' "$INPUT" | jq -r '.tool_input.command // empty'); then
  {
    echo "$HOOK_NAME: KON NIET CONTROLEREN — de payload is geen geldige JSON."
    echo ""
    echo "Zonder het commando is er niet getoetst; dat is geen groen licht."
  } >&2
  exit 2
fi
if [ -z "$CMD" ]; then
  exit 0
fi
CWD=$(printf '%s' "$INPUT" | jq -r '.cwd // empty')
[ -n "$CWD" ] || CWD="$PWD"

block() {
  {
    echo "$HOOK_NAME: KON NIET CONTROLEREN — $1"
    echo ""
    echo "Er staat mogelijk een 'gh pr create' in dit commando. 'Niet kunnen"
    echo "controleren' is geen groen licht, dus dit gaat niet stil door."
  } >&2
  exit 2
}

# De stderr van de lib (de blokkadetekst) gaat ongefilterd door.
OUT=$(printf '%s' "$CMD" | python3 "$LIB/pr_body_gate.py" "$CWD")
STATUS=$?

case "$STATUS" in
  0)
    if [ -n "$OUT" ]; then
      # additionalContext bereikt het model, systemMessage de gebruiker.
      jq -cn --arg m "$OUT" '{systemMessage: $m, hookSpecificOutput: {hookEventName: "PreToolUse", additionalContext: $m}}'
    fi
    exit 0
    ;;
  2)
    exit 2
    ;;
  3)
    # Tweestapsregel: staat `gh` én `pr` niet in de ruwe tekst, dan is er niets
    # te toetsen en legt één losse apostrof niets lam.
    case "$CMD" in
      *gh*) ;;
      *) exit 0 ;;
    esac
    case "$CMD" in
      *pr*) ;;
      *) exit 0 ;;
    esac
    block "het commando is niet te parsen (open quote). Splits het commando op."
    ;;
  *)
    block "de body-parser faalde (rc $STATUS)."
    ;;
esac
