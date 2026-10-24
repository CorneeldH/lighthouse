#!/usr/bin/env bash
# Draai shellcheck op de shell van deze repo, of op de bestanden die je meegeeft.
#
# Zonder argumenten draait hij de *gate-set*: de shell die zelf poorten bewaakt
# (scripts/hooks/, tests/integration/, .claude/hooks/). Die set staat op nul
# bevindingen op de laagste ernst (style) - dat is KPI-rij 1 van de skill
# paia-shell-gates.
#
# Exitcodes, en dit is het hele punt van dit script:
#   0  gecontroleerd, niets gevonden
#   1  gecontroleerd, wel bevindingen
#   2  verkeerd aangeroepen
#   4  NIET gecontroleerd (shellcheck ontbreekt) - geen groen, geen rood
#
# Drie uitkomsten, niet twee. "Kon niet kijken" is geen "niets gevonden": zie
# docs/developer/planning/specs/2026-08-23-shellcheck-gate-design.md en #650.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/shellcheck-bin.sh
. "$HERE/lib/shellcheck-bin.sh"

# De drie mappen die poorten bewaken. Hier is de lat het hoogst, want een fout
# in een gate is stiller dan een fout in gewone code: hij meldt groen.
GATE_DIRS=(scripts/hooks tests/integration .claude/hooks)

usage() {
  echo "usage: $0 [--severity error|warning|info|style] [file ...]" >&2
  echo "  zonder bestanden: de gate-set ($(IFS=' '; echo "${GATE_DIRS[*]}"))" >&2
}

severity=style
files=()
while [ "$#" -gt 0 ]; do
  case "$1" in
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
    echo "shellcheck-repo: niet in een git-repo en geen bestanden meegegeven" >&2
    exit 2
  }
  cd "$REPO_ROOT" || exit 2
  # git ls-files in plaats van een glob: geen lege expansie die als letterlijke
  # bestandsnaam doorgegeven wordt, en recursief in submappen.
  # --others --exclude-standard is niet optioneel: zonder die twee ziet de gate
  # een gloednieuwe, nog niet getrackte hook helemaal niet en meldt hij groen
  # over een set waar het nieuwe bestand niet in zat.
  while IFS= read -r f; do
    files+=("$f")
  done < <(git ls-files --cached --others --exclude-standard -- "${GATE_DIRS[@]/%//*.sh}")
  if [ "${#files[@]}" -eq 0 ]; then
    echo "shellcheck-repo: geen .sh in de gate-set gevonden - dit is geen groen" >&2
    exit 2
  fi
fi

# -x: volg `source`/`.` naar het gesourcete bestand. Zonder dit meldt shellcheck
# SC1091 ("not following") op elk script dat lib/ laadt — een bevinding over de
# controle zelf in plaats van over de code.
"$SHELLCHECK" -x -S "$severity" "${files[@]}" || exit 1
exit 0
