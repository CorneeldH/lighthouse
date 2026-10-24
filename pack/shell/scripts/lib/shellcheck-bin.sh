#!/usr/bin/env bash
# Gedeelde shellcheck-provisioning. Wordt ge-*sourced*, niet uitgevoerd.
#
# Bestaansreden: de shellcheck-gate mag nooit stil overslaan. Een hook die zijn
# tool niet vindt en dan exit 0 doet, meldt "schoon" terwijl hij niets gekeken
# heeft — precies de foutklasse uit #650. Daarom geeft resolve_shellcheck() drie
# uitkomsten terug, niet twee: gevonden (rc 0, pad op stdout), of niet te
# gebruiken (rc 4, reden op stderr). rc 1 is voorbehouden aan "bevindingen".
#
# Licentie: shellcheck is GPL-3.0 — toegestaan onder de copyleft-regel in
# CLAUDE.md als ongewijzigde losse binary achter een CLI-grens, dev-only: nooit
# mee in een productimage, nooit code of regels eruit overnemen.

# Gepind. Deze waarde staat OOK in scripts/hooks/workflow-audit.sh; die twee
# mogen niet uiteenlopen, want dan meet de hook iets anders dan de CI-lane.
# tests/unit/test_quality_hooks.py::test_shellcheck_pin_matches_workflow_audit
# bewaakt dat.
SHELLCHECK_PIN=0.11.0

# Exitcode voor "kon niet controleren". Bewust geen 0 en geen 1.
SHELLCHECK_UNAVAILABLE=4

_sc_platform() {
  case "$(uname -s)/$(uname -m)" in
    Linux/x86_64) echo linux/x86_64 ;;
    Linux/aarch64) echo linux/aarch64 ;;
    Darwin/arm64) echo darwin/arm64 ;;
    *) return 1 ;;
  esac
}

_sc_tool_dir() {
  local common root
  root="$(git rev-parse --show-toplevel 2>/dev/null)" || return 1
  common="$(git rev-parse --git-common-dir 2>/dev/null)" || return 1
  case "$common" in
    /*) ;;
    *) common="$root/$common" ;;
  esac
  # Zelfde map als workflow-audit.sh: een cache, een download, gedeeld over
  # worktrees omdat --git-common-dir naar de hoofd-.git wijst.
  echo "$common/workflow-audit-tools"
}

_sc_is_pinned() { # bin
  [ -x "$1" ] && "$1" --version 2>/dev/null | grep -qx "version: $SHELLCHECK_PIN"
}

_sc_download() { # tool_dir -> pad op stdout, of rc != 0
  local tool_dir="$1" plat url sha tmp got bin="$1/shellcheck"
  plat="$(_sc_platform)" || {
    echo "shellcheck: geen release voor $(uname -s)/$(uname -m)" >&2
    return 1
  }
  case "$plat" in
    linux/x86_64)  url="shellcheck-v${SHELLCHECK_PIN}.linux.x86_64.tar.xz";   sha=8c3be12b05d5c177a04c29e3c78ce89ac86f1595681cab149b65b97c4e227198 ;;
    linux/aarch64) url="shellcheck-v${SHELLCHECK_PIN}.linux.aarch64.tar.xz";  sha=12b331c1d2db6b9eb13cfca64306b1b157a86eb69db83023e261eaa7e7c14588 ;;
    darwin/arm64)  url="shellcheck-v${SHELLCHECK_PIN}.darwin.aarch64.tar.xz"; sha=56affdd8de5527894dca6dc3d7e0a99a873b0f004d7aabc30ae407d3f48b0a79 ;;
  esac
  tmp="$(mktemp -d)" || return 1
  if ! curl -fsSL -o "$tmp/s.txz" \
       "https://github.com/koalaman/shellcheck/releases/download/v${SHELLCHECK_PIN}/${url}"; then
    echo "shellcheck: download mislukt (offline?)" >&2
    rm -rf "$tmp"
    return 1
  fi
  got="$(sha256sum "$tmp/s.txz" | cut -d' ' -f1)"
  if [ "$got" != "$sha" ]; then
    echo "shellcheck: sha256 klopt niet (kreeg $got, wilde $sha)" >&2
    rm -rf "$tmp"
    return 1
  fi
  mkdir -p "$tool_dir"
  if ! tar -xJf "$tmp/s.txz" -C "$tool_dir" --strip-components=1 \
        "shellcheck-v${SHELLCHECK_PIN}/shellcheck"; then
    echo "shellcheck: uitpakken mislukt" >&2
    rm -rf "$tmp"
    return 1
  fi
  chmod 0755 "$bin"
  rm -rf "$tmp"
  echo "$bin"
}

# Print het pad naar een bruikbare shellcheck op stdout en geeft 0 terug, of
# print de reden op stderr en geeft $SHELLCHECK_UNAVAILABLE terug.
#
# Volgorde: expliciete override -> de gepinde binary in de gedeelde toolcache ->
# een *gepind* exemplaar op PATH -> download van de gepinde release -> als
# laatste redmiddel een afwijkende versie op PATH, met een melding.
#
# Waarom de pin vóór PATH gaat, en niet andersom: de gate-set staat op nul
# bevindingen ONDER DE PIN, en een oudere shellcheck kent de disable-codes van
# vandaag niet. 0.9.0 — de versie die op ubuntu-latest voorgeïnstalleerd staat —
# meldt SC2317 op precies de trap-cleanup die 0.11.0 via `disable=SC2329`
# onderdrukt: dezelfde boom, twee uitkomsten, afhankelijk van wat er toevallig
# op de runner staat. Een afwijkende versie is dus geen gelijkwaardig
# alternatief voor de pin, hooguit beter dan helemaal niet kijken.
#
# Let op bij het herschrijven van deze comments: een regel die begint met
# "# shellcheck " leest shellcheck als een directive en levert SC1072/SC1073 op.
resolve_shellcheck() {
  local bin tool_dir path_bin path_version

  if [ -n "${SHELLCHECK_BIN:-}" ]; then
    if [ -x "$SHELLCHECK_BIN" ]; then
      echo "$SHELLCHECK_BIN"
      return 0
    fi
    echo "shellcheck: SHELLCHECK_BIN=$SHELLCHECK_BIN is niet uitvoerbaar" >&2
    return "$SHELLCHECK_UNAVAILABLE"
  fi

  if tool_dir="$(_sc_tool_dir)"; then
    bin="$tool_dir/shellcheck"
    if _sc_is_pinned "$bin"; then
      echo "$bin"
      return 0
    fi
  fi

  path_bin=""
  path_version=""
  if bin="$(command -v shellcheck 2>/dev/null)" && [ -x "$bin" ]; then
    path_bin="$bin"
    path_version="$("$path_bin" --version 2>/dev/null | sed -n 's/^version: //p')"
    if [ "$path_version" = "$SHELLCHECK_PIN" ]; then
      echo "$path_bin"
      return 0
    fi
  fi

  if [ -n "${tool_dir:-}" ] && bin="$(_sc_download "$tool_dir")"; then
    echo "$bin"
    return 0
  fi

  # De pin was niet te halen. Een afwijkende versie is dan nog altijd beter dan
  # blind zijn, maar de melding moet erbij: de uitkomst is niet die van de gate.
  if [ -n "$path_bin" ]; then
    echo "shellcheck: PATH-versie ${path_version:-onbekend} wijkt af van de pin $SHELLCHECK_PIN en de pin was niet op te halen" >&2
    echo "$path_bin"
    return 0
  fi

  echo "shellcheck: niet gevonden en niet op te halen - er is dus NIET gecontroleerd." >&2
  return "$SHELLCHECK_UNAVAILABLE"
}
