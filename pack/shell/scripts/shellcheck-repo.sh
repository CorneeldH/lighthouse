#!/usr/bin/env bash
# Draai shellcheck op de shell van deze repo, of op de bestanden die je meegeeft.
#
# Zonder argumenten draait hij de *strenge set*: de shell in `shell.strict_dirs`
# uit lighthouse.toml (default `.claude/hooks`), de shell die zelf poorten
# bewaakt. Die set staat op nul bevindingen op de laagste ernst (style) - dat is
# KPI-rij 1 van de skill shell-gates.
#
# Exitcodes, en dit is het hele punt van dit script:
#   0  gecontroleerd, niets gevonden
#   1  gecontroleerd, wel bevindingen
#   2  verkeerd aangeroepen
#   4  NIET gecontroleerd (shellcheck ontbreekt of faalt) - geen groen, geen rood
#
# Drie uitkomsten, niet twee. "Kon niet kijken" is geen "niets gevonden".
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/shellcheck-bin.sh
. "$HERE/lib/shellcheck-bin.sh"

usage() {
  echo "usage: $0 [--severity error|warning|info|style] [file ...]" >&2
  echo "  without files: the strict set (shell.strict_dirs in lighthouse.toml)" >&2
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
    echo "shellcheck-repo: not in a git repository and no files given" >&2
    exit 2
  }
  cd "$REPO_ROOT" || exit 2
  # De mappen waar de lat het hoogst ligt, want een fout in een gate is stiller
  # dan een fout in gewone code: hij meldt groen.
  LIB_ROOT="${LIGHTHOUSE_REPO_ROOT:-$REPO_ROOT}"
  if ! dirs_out="$(LIGHTHOUSE_REPO_ROOT="$LIB_ROOT" python3 "$LIB_ROOT/.lighthouse/lib/lighthouse_lib/config.py" shell.strict_dirs 2>&1)"; then
    echo "shellcheck-repo: shell.strict_dirs could not be read: $dirs_out" >&2
    exit "$SHELLCHECK_UNAVAILABLE"
  fi
  specs=()
  while IFS= read -r dir; do
    [ -n "$dir" ] && specs+=("${dir%/}/*.sh")
  done <<EOF
$dirs_out
EOF
  # git ls-files in plaats van een glob: geen lege expansie die als letterlijke
  # bestandsnaam doorgegeven wordt, en recursief in submappen.
  # --others --exclude-standard is niet optioneel: zonder die twee ziet de gate
  # een gloednieuwe, nog niet getrackte hook helemaal niet en meldt hij groen
  # over een set waar het nieuwe bestand niet in zat.
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

# -x: volg `source`/`.` naar het gesourcete bestand. Zonder dit meldt shellcheck
# SC1091 ("not following") op elk script dat lib/ laadt — een bevinding over de
# controle zelf in plaats van over de code.
# De rc van shellcheck zelf: 0 schoon, 1 bevindingen. Elke andere rc (2 een bestand dat
# hij niet kon lezen, 3 en 4 verkeerde opties, of een crash) is niet gecontroleerd.
"$SHELLCHECK" -x -S "$severity" "${files[@]}"
rc=$?
case "$rc" in
  0|1) exit "$rc" ;;
esac
echo "shellcheck-repo: shellcheck exited with rc=$rc, so nothing was checked" >&2
exit "$SHELLCHECK_UNAVAILABLE"
