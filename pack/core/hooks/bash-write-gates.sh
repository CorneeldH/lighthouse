#!/usr/bin/env bash
# Pre/PostToolUse hook op matcher Bash: de drie Edit|Write-gates (shellcheck.sh,
# ruff-format.sh, worktree-linkcheck.sh) zien ook wat een Bash-call schrijft
# (#871). Dunne wrapper; de logica staat in lib/bash_writes.py.
#
# Tegenhanger (pairing-regel #624): de normen van de drie gates dragen hun
# eigen tegenhangers vooraf (skill paia-shell-gates, .claude/rules/python.md,
# de linkregel in AGENTS.md); CLAUDE.md, sectie Hooks, noemt deze hook voor
# Bash-schrijfwerk. Deze hook toetst achteraf wat een Bash-call schreef.
# Contract en bewust niet gedekt: docs/developer/instructie-hooks.md.
#
#   bash-write-gates.sh pre   PreToolUse: nullijn en marker. Altijd exit 0 - een
#                             PreToolUse exit 2 blokkeert de Bash-call zelf.
#   bash-write-gates.sh post  PostToolUse en PostToolUseFailure: vuile bestanden
#                             door hun gate. Exit 0 of 2.
#
# NIET fail-open (paia-shell-gates): zonder python3, zonder lib/bash_writes.py,
# of als de dispatcher met iets anders dan 0 of 2 eindigt, is er niet gekeken. Dat is exit 2 met
# KON NIET CONTROLEREN, naar stderr en als additionalContext naar stdout -
# hetzelfde uitvoerkanaal als de dispatcher zelf.
set -u

MODE="${1:-}"

# De hook erft de cwd van de Bash-call; de lib ligt naast dit script.
HOOK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB="$HOOK_DIR/lib/bash_writes.py"

# Eerst in een variabele: de payload gaat naar python3, en bij een wrapperfout
# is het event er nog uit te halen zonder python.
INPUT="$(cat)"

loud() { # melding - vaste tekst plus een getal, dus geen JSON-escaping nodig
  local event
  event="$(printf '%s\n' "$INPUT" |
    sed -n 's/.*"hook_event_name" *: *"\([A-Za-z]*\)".*/\1/p')"
  event="${event%%$'\n'*}"
  [ -n "$event" ] || event="PostToolUse"
  printf '%s\n' "$1" >&2
  printf '{"hookSpecificOutput":{"hookEventName":"%s","additionalContext":"%s"}}\n' \
    "$event" "$1"
  exit 2
}

case "$MODE" in
  pre)
    if command -v python3 >/dev/null 2>&1 && [ -f "$LIB" ]; then
      printf '%s' "$INPUT" | python3 "$LIB" pre >/dev/null 2>&1
    fi
    exit 0
    ;;
  post)
    command -v python3 >/dev/null 2>&1 ||
      loud "bash-write-gates: KON NIET CONTROLEREN — python3 ontbreekt"
    # Zonder lib eindigt python3 met rc 2: dat zou als een oordeel doorgaan.
    [ -f "$LIB" ] ||
      loud "bash-write-gates: KON NIET CONTROLEREN — lib/bash_writes.py ontbreekt"
    out="$(printf '%s' "$INPUT" | python3 "$LIB" post)"
    rc=$?
    case "$rc" in
      0 | 2)
        [ -z "$out" ] || printf '%s\n' "$out"
        exit "$rc"
        ;;
      *)
        loud "bash-write-gates: KON NIET CONTROLEREN — dispatcher faalde (rc=$rc)"
        ;;
    esac
    ;;
  *)
    loud "bash-write-gates: KON NIET CONTROLEREN — onbekende modus, verwacht pre of post"
    ;;
esac
