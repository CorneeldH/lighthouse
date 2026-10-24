#!/bin/bash
# PostToolUse hook: auto-run ruff format + check --fix on edited Python files.
# Matcher: Edit|Write — runs after every file edit.
# Pass 1 (format + --fix) is non-blocking. Pass 2 blocks (exit 2) on the eight
# BLOCKING_RULES below — repo-wide zero, so they can't false-positive on
# legacy code.

if ! command -v jq &>/dev/null; then
  exit 0
fi

INPUT=$(cat)
FILE_PATH=$(echo "$INPUT" | jq -r '.tool_input.file_path // empty')

if [ -z "$FILE_PATH" ]; then
  exit 0
fi

# Only process Python files
case "$FILE_PATH" in
  *.py) ;;
  *) exit 0 ;;
esac

if [ ! -f "$FILE_PATH" ]; then
  exit 0
fi

# De ruff-versie staat op één plek: .ruff-version in de repowortel. De ratel
# (scripts/quality/ratchet.py) leest hetzelfde bestand, en CI leest het
# transitief via de ratel. Ongepind was prima zolang ruff alleen autofixte;
# zodra dezelfde ruff ook telt, verschuift een release de getallen.
#
# Lukt het niet (hook draait buiten een repo, bestand weg), dan valt hij terug
# op ongepind in plaats van te stoppen: dit is een autofixer en die hoort
# fail-open te zijn.
ROOT="$(git rev-parse --show-toplevel 2>/dev/null)"
RUFF_VERSION=""
if [ -n "$ROOT" ] && [ -f "$ROOT/.ruff-version" ]; then
  RUFF_VERSION="$(tr -d '[:space:]' < "$ROOT/.ruff-version")"
fi

# Prefer uvx ruff (not in project venv), fall back to ruff
if command -v uvx &>/dev/null; then
  if [ -n "$RUFF_VERSION" ]; then
    RUFF="uvx ruff@$RUFF_VERSION"
  else
    RUFF="uvx ruff"
  fi
elif command -v ruff &>/dev/null; then
  RUFF="ruff"
else
  exit 0
fi

# Hooks fail open: a broken environment (offline, unfetchable ruff, no cache)
# must never read as "the code is wrong". `uvx ruff check` returns the same
# exit 1 for "found violations" and for "could not resolve the tool" — the
# exit code alone can't tell them apart. Probe separately: if the tool can't
# even start, skip silently rather than blame the code.
if ! $RUFF --version >/dev/null 2>&1; then
  exit 0
fi

# Pass 1 — best effort, stil. Brede default-selectie (ruff 0.16 selecteert veel
# meer dan E4/E7/E9/F): format + alles wat autofixbaar is.
$RUFF format "$FILE_PATH" 2>/dev/null || true
$RUFF check --fix "$FILE_PATH" 2>/dev/null || true

# Pass 2 — blokkerend, op de acht regels die repo-breed op nul staan (gemeten
# 2026-08-22 op src/ + tests/). Bewust NIET de volle selectie: die telt 1487
# hits, waarvan 1154 niet autofixbaar, dus elke edit in een bestaand bestand
# zou blokkeren. Deze acht kunnen niet vals-positief zijn op legacy code, en
# PGH003/PGH004 sluiten precies de ontsnappingsroute af (kale `# type: ignore`
# / kale `# noqa` in plaats van de fout oplossen).
#
# Deze lijst staat OOK in pyproject.toml onder [tool.ruff.lint] extend-select,
# zodat een handmatige `uv run ruff check` en de IDE dezelfde regels zien.
# Blijf hem hier expliciet noemen: #562 zette er zeven TELLENDE regels bij (160
# bestaande hits) en een kale `$RUFF check` zou dan opeens blokkeren op legacy-
# schuld in elk bestand dat je aanraakt. De gelinte set en de blokkerende set
# zijn met opzet twee verschillende sets.
# tests/unit/test_quality_hooks.py::test_blocking_rules_are_selected_in_pyproject
# bewaakt dat deze set een deelverzameling van de config blijft.
BLOCKING_RULES="E722,B006,F403,F811,B023,PT012,PGH003,PGH004"

if ! out=$($RUFF check --select "$BLOCKING_RULES" "$FILE_PATH" 2>&1); then
  echo "Lint-gate: los dit op in de code — versoepel geen regel en plak er geen" >&2
  echo "# noqa / # type: ignore overheen. Wil je een regel echt uit, vraag het." >&2
  echo "" >&2
  echo "$out" >&2
  exit 2
fi

exit 0
