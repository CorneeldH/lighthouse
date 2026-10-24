#!/bin/bash
# Stop hook: run `ty check src/` at the end of a turn.
# Matcher: "" — runs on every Stop.
# Blocking: exit 2 with stderr sends the diagnostics back to the model, which
# fixes them in the same session with the context still warm.
#
# Deliberately NOT a PostToolUse hook: a type error is often only judgeable once
# several files are done, so per-edit feedback would burn turns on half-finished
# state.

# Keep in sync with the `ty==` pin in pyproject.toml [dependency-groups] dev.
# tests/unit/test_quality_hooks.py::test_ty_pin_matches_hook guards this.
TY_VERSION="0.0.72"

if ! command -v jq &>/dev/null || ! command -v uvx &>/dev/null; then
  exit 0
fi

INPUT=$(cat)

# Claude Code re-invokes the model when a Stop hook blocks. Without this guard
# a failing check would loop forever.
if [ "$(echo "$INPUT" | jq -r '.stop_hook_active // false')" = "true" ]; then
  exit 0
fi

# Worktree-aware, same pattern as pre-commit-tests.sh (issue #327): test the
# checkout the work happened in, not necessarily the main one.
CWD=$(echo "$INPUT" | jq -r '.cwd // empty')
[ -z "$CWD" ] && CWD="$CLAUDE_PROJECT_DIR"
cd "$CWD" 2>/dev/null || cd "$CLAUDE_PROJECT_DIR" || exit 0

ROOT=$(git rev-parse --show-toplevel 2>/dev/null)
[ -n "$ROOT" ] && cd "$ROOT" || exit 0

[ -d "src" ] || exit 0

# ty needs a venv to resolve third-party imports. Without one it reports 444
# diagnostics instead of 0 — every import falls over. A fresh worktree has no
# .venv until the first `uv run`, so fall back to the main checkout's.
VENV=""
if [ -d "$ROOT/.venv" ]; then
  VENV="$ROOT/.venv"
elif [ -n "$CLAUDE_PROJECT_DIR" ] && [ -d "$CLAUDE_PROJECT_DIR/.venv" ]; then
  VENV="$CLAUDE_PROJECT_DIR/.venv"
else
  exit 0
fi

# Hooks fail open: a broken environment (offline, unfetchable pin, no cache)
# must never read as "the code is wrong". `uvx ty@version check` returns the
# same exit 1 for "found diagnostics" and for "could not resolve the tool" —
# the exit code alone can't tell them apart. Probe separately: if the pinned
# version can't even be started, skip silently rather than blame the code.
if ! uvx "ty@$TY_VERSION" --version >/dev/null 2>&1; then
  exit 0
fi

# `--color never` pint het formaat waar de codeherkenning hieronder op ankert.
# Kleur zet een ANSI-escape vóór `error[`, en dan matcht de regel-anker niet meer
# — de venv-tak valt dan stil terug op de gewone blokkade. ty 0.0.72 kleurt uit
# zichzelf alleen bij een tty (FORCE_COLOR/CLICOLOR_FORCE negeert het, gemeten),
# dus dit dekt een toekomstige default, niet een bestaand lek.
if ! OUT=$(uvx "ty@$TY_VERSION" check --color never --python "$VENV" src/ 2>&1); then
  # Welke regelcodes kwamen eruit? ty zet ze aan het regelbegin als
  # `error[<code>]:` of `warning[<code>]:`. Leeg = een uitval die geen
  # diagnostics opleverde; die valt door naar de gewone blokkade.
  CODES=$(printf '%s\n' "$OUT" |
    sed -nE 's/^(error|warning)\[([a-z0-9-]+)\].*/\2/p' | sort -u)

  # Bestaat de set uitsluitend uit `unresolved-import`, dan is de omgeving de
  # waarschijnlijke oorzaak en niet de code: een venv die achterloopt op
  # uv.lock laat een pakket dat gewoon in [project].dependencies staat
  # verdwijnen (gemeten 2026-08-23 met `markdownify`). "Los ze op in de code"
  # stuurt de lezer dan het verkeerde spoor op.
  #
  # Bewust exit 2 en niet exit 0, anders dan het fail-open-pad hierboven: de
  # tegenkracht is dat `unresolved-import` óók precies het signaal is dat
  # iemand een import toevoegde zonder de dependency te declareren. Dat mag
  # niet stil wegvallen. Beide takken zijn bovendien meteen uitvoerbaar
  # (`uv sync` draaien, of de dependency toevoegen), dus blokkeren levert werk
  # op in plaats van een doodlopende melding. Een lus kan het niet worden: de
  # stop_hook_active-guard bovenin laat de hook maar één keer per beurt vuren.
  if [ "$CODES" = "unresolved-import" ]; then
    echo "Type-gate: ty vindt alleen onvindbare imports in src/, geen typefouten." >&2
    echo "Dat wijst eerder op een scheve omgeving dan op foute code." >&2
    echo "" >&2
    echo "Controleer eerst je venv:  uv sync  (mét de extra's die je gebruikt," >&2
    echo "op de Mac --extra all; op Linux faalt die op de macOS-only rumps)." >&2
    echo "Klopt de venv en blijft de import onvindbaar, dan ontbreekt de" >&2
    echo "dependency in pyproject.toml — voeg hem daar toe. Sloop de import niet" >&2
    echo "uit de code." >&2
    echo "" >&2
    echo "$OUT" >&2
    exit 2
  fi

  echo "Type-gate: ty vindt fouten in src/. Los ze op in de code — zet geen" >&2
  echo "ty-regel uit en plak er geen kale # type: ignore overheen." >&2
  echo "" >&2
  echo "$OUT" >&2
  exit 2
fi

exit 0
