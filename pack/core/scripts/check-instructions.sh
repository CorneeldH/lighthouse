#!/usr/bin/env bash
# De generieke deterministische checks over de instructielaag van een repo:
# 1, 2, 3, 4, 5, 7, 8, 10 en 11. De nummers komen uit de bron; 6, 9, 12 en 13
# zijn projecteigen en zitten hier niet in.
#
# Laag 2 van de gate-spec (commit-tijd): geen netwerk, geen LLM, geen state.
# Het maakt de instructielaag zelf meetbaar -- het verschil tussen een norm en
# een wens.
#
# Uitvoercontract, drie uitkomsten en niet twee:
#   ok       de check heeft gemeten en niets gevonden
#   FAIL     de check heeft gemeten en iets gevonden -> exit 1
#   SKIPPED  de check kon hier niet meten. Exit 0, maar luid: de
#            afwezigheid van een signaal is geen negatief resultaat.
# Kan het script als geheel niet meten (geen repo-root, kapotte
# lighthouse.toml), dan `check-instructions: COULD NOT CHECK — <reden>` op
# stderr en exit 2.
#
# Draaien:  .lighthouse/scripts/check-instructions.sh
set -uo pipefail

could_not_check() {
  printf 'check-instructions: COULD NOT CHECK — %s\n' "$1" >&2
  exit 2
}

# De repo-root: `LIGHTHOUSE_REPO_ROOT`, anders git. Niet de map van dit script:
# dat staat geïnstalleerd in `.lighthouse/scripts/`, en de configlezer kiest
# dezelfde root.
ROOT="${LIGHTHOUSE_REPO_ROOT:-}"
if [ -z "$ROOT" ]; then
  ROOT="$(git rev-parse --show-toplevel 2>/dev/null)" ||
    could_not_check "no repository root: LIGHTHOUSE_REPO_ROOT is not set and git rev-parse --show-toplevel failed"
fi
[ -d "$ROOT" ] || could_not_check "repository root '$ROOT' is not a directory"
CLAUDE_SKILLS="$ROOT/.claude/skills"
AGENTS_SKILLS="$ROOT/.agents/skills"
PERSONAL_SKILLS="${CHECK_INSTRUCTIONS_PERSONAL:-$HOME/.claude/skills}"
CONFIG_PY="$(dirname "${BASH_SOURCE[0]}")/../lib/lighthouse_lib/config.py"

findings=0

fail() {
  printf 'FAIL  %s\n' "$1"
  findings=$((findings + 1))
}

skip() { printf 'SKIPPED  %s\n' "$1"; }
ok() { printf 'ok    %s\n' "$1"; }

# sha256sum bestaat niet op macOS. Een repo wordt op Linux EN op een Mac
# bewerkt, en een gate die op een van de twee stil niets doet is geen gate.
if command -v sha256sum >/dev/null 2>&1; then
  SHA_CMD="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
  SHA_CMD="shasum -a 256"
else
  printf 'FAIL  no sha256sum and no shasum on this machine; the duplicate check cannot run\n'
  exit 1
fi

# python3 ontbreekt zelden, maar check 8 (pairing) parseert settings.json ermee
# en kan zonder python3 niet meten of een hookpad geregistreerd staat; checks 10
# en 11 lezen er lighthouse.toml mee. Anders dan sha256sum/shasum raakt dit maar
# enkele checks -- vandaar SKIPPED op die checks in plaats van een script-brede FAIL.
if command -v python3 >/dev/null 2>&1; then
  HAVE_PYTHON3=1
else
  HAVE_PYTHON3=0
fi

# 'wc' hoort tot coreutils en ontbreekt zelden, maar check 10 (omvang) kan
# zonder niet meten of de instructies binnen het plafond blijven. Zelfde behandeling
# als python3 hierboven: dit raakt maar één check, dus SKIPPED op die ene
# check in plaats van een script-brede FAIL.
if command -v wc >/dev/null 2>&1; then
  HAVE_WC=1
else
  HAVE_WC=0
fi

# Een sleutel uit lighthouse.toml via de configlezer van deze installatie.
# Zet CONFIG_WAARDE en geeft de rc van de lezer: 0 waarde, 1 niet gezet. Een
# kapotte config (rc 2) stopt het hele script: elke uitkomst daarna zou op een
# gok staan. Zonder python3 geeft hij 3; de check die de waarde nodig heeft,
# meldt dan SKIPPED.
config_value() {
  local uit rc
  [ "$HAVE_PYTHON3" -eq 1 ] || return 3
  uit="$(LIGHTHOUSE_REPO_ROOT="$ROOT" python3 "$CONFIG_PY" "$1" 2>&1)"
  rc=$?
  case "$rc" in
    0) CONFIG_WAARDE="$uit" ;;
    1) CONFIG_WAARDE="" ;;
    *) could_not_check "$uit" ;;
  esac
  return "$rc"
}

# Woordental waarboven de altijd geladen instructies (CLAUDE.md plus het
# AGENTS.md dat het importeert) weer behang worden: `[project]
# instructions_word_ceiling`, default 3000. Leeg zonder python3.
PLAFOND=""
config_value project.instructions_word_ceiling && PLAFOND="$CONFIG_WAARDE"

# Het bestand met de vastgelegde verschillen van check 11: `[paths]
# mirror_exceptions`, relatief aan de repo-root. Optioneel: niet gezet betekent
# geen uitzonderingen. SPIEGEL_CONFIG is 0 zonder python3 (niet te lezen).
SPIEGEL_UITZONDERINGEN=""
SPIEGEL_CONFIG=1
config_value paths.mirror_exceptions
case "$?" in
  0)
    case "$CONFIG_WAARDE" in
      /*) SPIEGEL_UITZONDERINGEN="$CONFIG_WAARDE" ;;
      *) SPIEGEL_UITZONDERINGEN="$ROOT/$CONFIG_WAARDE" ;;
    esac
    ;;
  3) SPIEGEL_CONFIG=0 ;;
esac

# Een frontmatter-veld uit een SKILL.md. Leeg als het veld ontbreekt OF geen
# waarde draagt: `upstream:` zonder waarde moet als "ontbreekt" tellen, anders
# is de herkomstkop met een lege regel te omzeilen.
#
# Draagt ook een YAML-gevouwen (`>`) of literale (`|`) blokscalar: zonder deze
# tak leest `index($0, f ": ") == 1` bij `description: >` alleen de letterlijke
# ">" van diezelfde regel, en de echte tekst op de ingesprongen vervolgregels
# blijft ongelezen -- check 4 (overlap) vergelijkt dan tegen ">" in plaats van
# de description en vindt nooit gedeelde triggerwoorden.
#
# `frontmatter_field` werkt op een bestandspad; `skill_field` is de wrapper
# voor een skillmap.
frontmatter_field() {
  local file="$1" field="$2"
  [ -f "$file" ] || return 0
  awk -v f="$field" '
    NR == 1 && $0 == "---" { inside = 1; next }
    inside && collecting {
      if ($0 == "---" || ($0 !~ /^[ \t]/ && $0 != "")) {
        collecting = 0
        print buf
        exit
      }
      regel = $0
      sub(/^[ \t]+/, "", regel)
      if (regel == "") {
        buf = buf "\n"
      } else if (buf == "" || buf ~ /\n$/) {
        buf = buf regel
      } else if (vouw) {
        buf = buf " " regel
      } else {
        buf = buf "\n" regel
      }
      next
    }
    inside && $0 == "---" { exit }
    inside && index($0, f ": ") == 1 {
      waarde = substr($0, length(f) + 3)
      if (waarde ~ /^[>|][-+0-9]*[ \t]*$/) {
        collecting = 1
        vouw = (substr(waarde, 1, 1) == ">")
        buf = ""
        next
      }
      print waarde
      exit
    }
    END { if (collecting) print buf }
  ' "$file"
}

skill_field() { frontmatter_field "$1/SKILL.md" "$2"; }

# Een sleutel onder het `metadata:`-blok van een SKILL.md. De Agent Skills-spec
# staat top-level maar zes velden toe (name, description, license, compatibility,
# metadata, allowed-tools); al het andere hoort hieronder. Leeg als de sleutel
# ontbreekt OF geen waarde draagt: `upstream:` zonder waarde moet als
# "ontbreekt" tellen, anders is de herkomstkop met een lege regel te omzeilen.
skill_meta() {
  local file="$1/SKILL.md" field="$2"
  [ -f "$file" ] || return 0
  awk -v f="$field" '
    NR == 1 && $0 == "---" { inside = 1; next }
    inside && $0 == "---" { exit }
    inside && $0 ~ /^metadata:[ \t]*$/ { in_meta = 1; next }
    in_meta && NF && $0 !~ /^[ \t]/ { in_meta = 0 }
    in_meta && match($0, "^[ \t]+" f ": ") { print substr($0, RLENGTH + 1); exit }
  ' "$file"
}

# Mapnamen (niet paden) van de skills onder een wortel, gesorteerd. `-printf` is
# GNU-only, vandaar de basename-lus. Een wortel die niet bestaat levert niets op
# en is geen fout: dat oordeel hoort bij de check, niet bij de helper.
# LET OP: elke `comm` die dit voedt moet OOK LC_ALL=C draaien. Sorteert de
# helper op C en vergelijkt comm onder nl_NL, dan klaagt comm op stderr
# ("file 1 is not in sorted order") en is de uitkomst niet te vertrouwen.
skill_dirs() {
  [ -d "$1" ] || return 0
  find "$1" -mindepth 1 -maxdepth 1 -type d -exec basename {} \; | LC_ALL=C sort
}

# Inhoudshash over een skill-map: relatieve padnamen plus bestandsinhoud, in
# vaste volgorde. Het relatieve pad zit er bewust in -- dan telt een hernoemd
# bestand ook als verschil. `sort -z` is GNU-only, dus de lus leest gewone
# regels; een skill-map met een newline in een bestandsnaam bestaat niet en zou
# de spec al schenden.
dir_hash() {
  (
    cd "$1" || exit 1
    find . -type f | LC_ALL=C sort | while IFS= read -r f; do
      printf '%s\n' "$f"
      cat "$f"
    done
  ) | $SHA_CMD | awk '{ print $1 }'
}

# --- Check 1: naam dubbel geclaimd -----------------------------------------
# Twee directories die dezelfde `name:` in hun frontmatter dragen. Claude
# laadt er dan een en zwijgt over de ander.
check_dubbele_naam() {
  if [ ! -d "$CLAUDE_SKILLS" ]; then
    skip "duplicate names: no .claude/skills, so there is nothing to measure here"
    return 0
  fi
  local naam dir dubbel gevonden=0
  local regels=""
  while IFS= read -r dir; do
    naam="$(skill_field "$CLAUDE_SKILLS/$dir" name)"
    [ -n "$naam" ] && regels+="$naam"$'\t'"$dir"$'\n'
  done < <(skill_dirs "$CLAUDE_SKILLS")

  while IFS= read -r dubbel; do
    [ -z "$dubbel" ] && continue
    gevonden=1
    fail "duplicate name '$dubbel' claimed by: $(printf '%s' "$regels" |
      awk -F'\t' -v n="$dubbel" '$1 == n { printf "%s ", $2 }')"
  done < <(printf '%s' "$regels" | cut -f1 | LC_ALL=C sort | uniq -d)

  [ "$gevonden" -eq 0 ] && ok "no names claimed twice"
  return 0
}

# --- Check 2: byte-identiek paar -------------------------------------------
# Twee skill-mappen met identieke inhoud. Het `generate-slides-retro` /
# `generate_slides_retro`-geval: zelfde md5, en de tweede kost een volle
# description in elke sessie.
check_byte_identiek() {
  if [ ! -d "$CLAUDE_SKILLS" ]; then
    skip "byte-identical: no .claude/skills, so there is nothing to measure here"
    return 0
  fi
  local wortel dir hash dubbel regels gevonden=0
  # Per wortel, NOOIT over de spiegel heen. `.claude/skills/x` en
  # `.agents/skills/x` zijn per ontwerp een paar; bij een gevendorde externe
  # skill is de spiegel bovendien verbatim en dus letterlijk identiek. Die
  # over een kam scheren zou elke externe skill als duplicaat melden.
  for wortel in "$CLAUDE_SKILLS" "$AGENTS_SKILLS"; do
    regels=""
    while IFS= read -r dir; do
      hash="$(dir_hash "$wortel/$dir")"
      regels+="$hash"$'\t'"$dir"$'\n'
    done < <(skill_dirs "$wortel")

    while IFS= read -r dubbel; do
      [ -z "$dubbel" ] && continue
      gevonden=1
      fail "byte-identical pair in $(basename "$(dirname "$wortel")")/skills: $(printf '%s' "$regels" |
        awk -F'\t' -v h="$dubbel" '$1 == h { printf "%s ", $2 }')"
    done < <(printf '%s' "$regels" | cut -f1 | LC_ALL=C sort | uniq -d)
  done

  [ "$gevonden" -eq 0 ] && ok "no byte-identical pairs within a skill root"
  return 0
}

# --- Check 3: spiegelbreuk --------------------------------------------------
# `.claude/skills/` en `.agents/skills/` horen dezelfde namen te dragen. De
# spiegel beweegt in dezelfde gereviewde wijziging mee; deze check maakt dat afdwingbaar.
check_spiegel() {
  local ontbreekt gevonden=0
  while IFS= read -r ontbreekt; do
    [ -z "$ontbreekt" ] && continue
    gevonden=1
    fail "mirror missing: '$ontbreekt' is not in both .claude/skills/ and .agents/skills/"
  done < <(LC_ALL=C comm -3 <(skill_dirs "$CLAUDE_SKILLS") <(skill_dirs "$AGENTS_SKILLS") | tr -d '\t')

  [ "$gevonden" -eq 0 ] && ok "mirror .claude/skills <-> .agents/skills matches"
  return 0
}

# --- Check 11: spiegel-inhoud (#981) ---------------------------------------
# Check 3 ziet alleen namen; de inhoud van een paar liep ongemerkt uiteen. Deze
# check vergelijkt elk bestand van elk paar. Wat per harnas bewust verschilt
# ("Claude Code" tegenover "Codex", een OpenCode-alinea) staat in
# SPIEGEL_UITZONDERINGEN, in dezelfde vorm als spiegel_verschillen() print.
# Geen regelnummers in die vorm: een gelijke edit aan beide kanten verschuift
# de verschillen maar verandert ze niet, en mag dus niet rood worden.
#
# Een skillmap waarvan een `SKILL.md` de lighthouse-marker draagt
# (`metadata.lighthouse`), slaat de check over: die is van het pakket, dat zijn
# twee varianten zelf beheert, en `lighthouse init` legt ze neer.

# Print per verschillend bestand een kop en de afwijkende regels (`<` = .claude,
# `>` = .agents). Exit 4 als diff een paar niet kon vergelijken: dat is "kon
# niet meten", nooit "geen verschil".
spiegel_verschillen() {
  local naam rel links rechts uit rc lijst_links lijst_rechts
  while IFS= read -r naam; do
    [ -z "$naam" ] && continue
    if [ -n "$(skill_meta "$CLAUDE_SKILLS/$naam" lighthouse)" ] ||
      [ -n "$(skill_meta "$AGENTS_SKILLS/$naam" lighthouse)" ]; then
      continue
    fi
    # Los van de lus en met rc-check: een map die cd of find niet kan lezen,
    # levert anders een lege lijst op, en die leest als "geen verschil".
    lijst_links="$(cd "$CLAUDE_SKILLS/$naam" && find . -type f)" || return 4
    lijst_rechts="$(cd "$AGENTS_SKILLS/$naam" && find . -type f)" || return 4
    while IFS= read -r rel; do
      [ -z "$rel" ] && continue
      links="$CLAUDE_SKILLS/$naam/$rel"
      rechts="$AGENTS_SKILLS/$naam/$rel"
      [ -f "$links" ] || links=/dev/null
      [ -f "$rechts" ] || rechts=/dev/null
      # `git diff`, niet `diff`: GNU diff (Linux) en de FreeBSD-diff van macOS
      # lijnen dezelfde hunks anders uit, en dan wijkt deze uitvoer per machine af
      # van het vastgelegde bestand. Git met vast algoritme is op beide gelijk.
      uit="$(git diff --no-index --no-color --no-ext-diff --no-textconv -U0 \
        --diff-algorithm=myers --no-indent-heuristic -- "$links" "$rechts")"
      rc=$?
      case "$rc" in
        0) continue ;;
        1) ;;
        *) return 4 ;;
      esac
      printf '=== %s/%s\n' "$naam" "$rel"
      # Unified naar de `<`/`>`-vorm; de kop vóór de eerste `@@` valt weg.
      # Een binair verschil levert geen hunks; toon dan de melding zelf.
      printf '%s\n' "$uit" | awk '
        /^@@/ { hunk = 1; next }
        !hunk { next }
        /^-/  { print "< " substr($0, 2); n++; next }
        /^\+/ { print "> " substr($0, 2); n++ }
        END   { exit n ? 0 : 1 }
      ' || printf '%s\n' "$uit"
    done < <(printf '%s\n%s\n' "$lijst_links" "$lijst_rechts" | sed 's|^\./||' | LC_ALL=C sort -u)
  done < <(LC_ALL=C comm -12 <(skill_dirs "$CLAUDE_SKILLS") <(skill_dirs "$AGENTS_SKILLS"))
  return 0
}

check_spiegel_inhoud() {
  local actueel verwacht="" bron rel
  if [ "$SPIEGEL_CONFIG" -eq 0 ]; then
    skip "mirror-content: python3 is missing on this machine, so [paths] mirror_exceptions cannot be read"
    return 0
  fi
  if ! actueel="$(spiegel_verschillen)"; then
    skip "mirror-content: diff could not compare a skill pair, so the content was not measured"
    return 0
  fi
  [ -n "$SPIEGEL_UITZONDERINGEN" ] && [ -f "$SPIEGEL_UITZONDERINGEN" ] &&
    verwacht="$(cat "$SPIEGEL_UITZONDERINGEN")"

  if [ "$actueel" = "$verwacht" ]; then
    ok "mirror-content matches, apart from the recorded exceptions"
    return 0
  fi

  if [ -n "$SPIEGEL_UITZONDERINGEN" ]; then
    rel="${SPIEGEL_UITZONDERINGEN#"$ROOT"/}"
    bron="$rel"
  else
    rel="<file>"
    bron="no recorded exceptions ([paths] mirror_exceptions is not set)"
  fi
  fail "mirror-content differs from $bron (- recorded, + now; < .claude, > .agents):"
  diff -u <(printf '%s\n' "$verwacht") <(printf '%s\n' "$actueel") | tail -n +3 | sed 's/^/      /'
  printf '      Bring the other side in line. If the difference is deliberate per harness, record it with\n'
  if [ -z "$SPIEGEL_UITZONDERINGEN" ]; then
    printf '      [paths] mirror_exceptions = "<file>" in lighthouse.toml, and then\n'
  fi
  printf '      .lighthouse/scripts/check-instructions.sh --print-mirror-exceptions > %s\n' "$rel"
  return 0
}

# Woorden die in vrijwel elke description staan en dus niets onderscheiden.
STOPWOORDEN=" gebruik wanneer welke wordt worden binnen deze dit die daar want
 zodat alleen altijd nooit iemand iedere elke andere anders about after before
 which where their there these those would could should using while whenever
 without within always never asks user users something anything wants needs "

# Triggerwoorden van een description: kleingeschreven, >= 5 tekens, geen
# stopwoord, uniek en gesorteerd.
# De lijst gaat via de omgeving en niet via `-v`: de BSD-awk die macOS
# meelevert weigert een newline in een `-v`-waarde ("awk: newline in string")
# en viel dan om, waarna deze check zonder resultaat doorliep. `tr` maakt er
# één regel van — dat repareert meteen een stille tweede fout, want een
# stopwoord aan het eind van een regel werd door `index(stop, " " $0 " ")`
# nooit gevonden.
trigger_tokens() {
  printf '%s\n' "$1" |
    tr '[:upper:]' '[:lower:]' |
    tr -cs 'a-z0-9' '\n' |
    LIGHTHOUSE_STOPWORDS="$(printf '%s' "$STOPWOORDEN" | tr '\n' ' ')" awk '
      BEGIN { stop = ENVIRON["LIGHTHOUSE_STOPWORDS"] }
      length($0) >= 5 && index(stop, " " $0 " ") == 0 { print }
    ' |
    LC_ALL=C sort -u
}

# Draagt deze description een afgrenzende clausule? Twee eisen tegelijk: een
# scope-woord ALS los woord, en een andere skill bij naam. Elk van beide alleen
# is precies de valkuil waar CEDA's validator op stukliep: een kaal `buiten`
# vuurt binnen "buitenwereld", en "gebruik dan deze skill" verbreedt de trigger
# juist in plaats van hem af te grenzen.
has_exclusion_clause() {
  local desc eigen woord scope=0
  desc="$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')"
  eigen="$2"
  for woord in "let op" "distinct from" "in plaats van" "gebruik dan" \
    "niet de skill" "niet voor" "nooit voor" "instead" "not for"; do
    if printf '%s' "$desc" |
      grep -qE "(^|[^a-z])${woord// /[[:space:]]}([^a-z]|\$)"; then
      scope=1
      break
    fi
  done
  [ "$scope" -eq 1 ] || return 1

  # Een gekoppeld token van >= 6 tekens dat niet de eigen naam is.
  printf '%s' "$desc" |
    grep -oE '[a-z0-9]+(-[a-z0-9]+)+' |
    awk -v eigen="$eigen" '
      length($0) >= 6 && $0 != eigen { found = 1 }
      END { exit found ? 0 : 1 }'
}

# --- Check 4: overlap zonder afgrenzende clausule ---------------------------
# Twee descriptions die dezelfde triggerwoorden delen zonder dat een van beide
# afgrenst. Het model kiest dan willekeurig; de fix is een clausule in die
# description, niet een hogere drempel.
DREMPEL_OVERLAP=5

check_overlap() {
  local -a namen=() descs=()
  local dir i j gedeeld gevonden=0
  while IFS= read -r dir; do
    namen+=("$dir")
    descs+=("$(skill_field "$CLAUDE_SKILLS/$dir" description)")
  done < <(skill_dirs "$CLAUDE_SKILLS")

  for ((i = 0; i < ${#namen[@]}; i++)); do
    for ((j = i + 1; j < ${#namen[@]}; j++)); do
      gedeeld="$(LC_ALL=C comm -12 \
        <(trigger_tokens "${descs[i]}") \
        <(trigger_tokens "${descs[j]}") | wc -l)"
      [ "$gedeeld" -lt "$DREMPEL_OVERLAP" ] && continue
      has_exclusion_clause "${descs[i]}" "${namen[i]}" && continue
      has_exclusion_clause "${descs[j]}" "${namen[j]}" && continue
      gevonden=1
      fail "overlap without a bounding clause: '${namen[i]}' and '${namen[j]}' share $gedeeld trigger words"
    done
  done

  [ "$gevonden" -eq 0 ] && ok "no unbounded overlap between descriptions"
  return 0
}

# --- Check 5: schaduwende personal skill ------------------------------------
# Personal skills OVERSCHADUWEN gelijknamige project-skills -- omgekeerd aan
# settings.json. Een naam die in beide staat betekent dat de project-versie
# nooit laadt, en dat gebeurt stil.
check_schaduw() {
  if [ ! -d "$PERSONAL_SKILLS" ]; then
    skip "shadow: '$PERSONAL_SKILLS' does not exist on this machine, so there is nothing to measure here"
    return 0
  fi

  local naam gevonden=0
  while IFS= read -r naam; do
    [ -z "$naam" ] && continue
    gevonden=1
    fail "shadow: '$naam' is in $PERSONAL_SKILLS and in .claude/skills/; the personal version wins"
  done < <(LC_ALL=C comm -12 <(skill_dirs "$PERSONAL_SKILLS") <(skill_dirs "$CLAUDE_SKILLS"))

  [ "$gevonden" -eq 0 ] && ok "no personal skill shadows a project skill"
  return 0
}

# --- Check 7: elke rule is pad-gescoped --------------------------------------
# Een rule zonder `paths:` laadt elke sessie en kost dus hetzelfde als
# CLAUDE.md. De winst van rules zit volledig in de scoping; zonder die regel is
# het CLAUDE.md met extra stappen.
check_scoping() {
  local rules="$ROOT/.claude/rules" bestand gevonden=0

  if [ ! -d "$rules" ]; then
    skip "scoping: '$rules' does not exist, so there is no rule to check here"
    return 0
  fi

  while IFS= read -r bestand; do
    [ -z "$bestand" ] && continue
    # `paths:` moet in de frontmatter staan (niet ergens in de body) EN een
    # niet-lege waarde dragen -- ofwel inline (`paths: src/**`), ofwel als
    # lijst op de eerstvolgende regel. Een kale `paths:` of `paths: []`
    # matcht geen enkel pad en telt dus als "ontbreekt", net als elders in dit
    # script (skill_field()/skill_meta() behandelen een leeg veld ook als
    # afwezig).
    if ! awk '
      NR == 1 && $0 == "---" { inside = 1; next }
      inside && $0 == "---" { exit }
      inside && index($0, "paths:") == 1 {
        rest = substr($0, 7)
        gsub(/^[ \t]+/, "", rest)
        gsub(/[ \t]+$/, "", rest)
        if (rest != "" && rest != "[]") { found = 1; exit }
        # Lege regels en YAML-commentaarregels tussen `paths:` en de lijst
        # zijn geldige YAML en mogen de check niet laten falen -- vandaar een
        # lus die ze overslaat in plaats van precies één volgende regel te
        # bekijken. Het randgeval blijft staan: de sluitende "---" is noch
        # leeg, noch commentaar, dus die stopt de lus zonder found te zetten.
        while ((getline nextline) > 0) {
          trimmed = nextline
          gsub(/^[ \t]+/, "", trimmed)
          gsub(/[ \t]+$/, "", trimmed)
          if (trimmed == "" || trimmed ~ /^#/) { continue }
          # "- " (koppelteken + spatie), niet kaal "-": anders leest de
          # sluitende frontmatter-delimiter "---" ook als lijstitem.
          if (substr(trimmed, 1, 2) == "- ") { found = 1 }
          break
        }
        exit
      }
      END { exit found ? 0 : 1 }
    ' "$bestand"; then
      gevonden=1
      fail "scoping: '$(basename "$bestand")' has no paths: block with a non-empty value in its frontmatter"
    fi
  done < <(find "$rules" -maxdepth 1 -type f -name '*.md' | LC_ALL=C sort)

  [ "$gevonden" -eq 0 ] && ok "every rule is path-scoped"
  return 0
}

# Is dit hookpad geregistreerd in settings.json -- specifiek in het
# `hooks`-blok (nooit in `permissions.deny` of elders), en als een heel
# padsegment in een `command`-veld, niet als toevallige substring van een
# langere bestandsnaam (`streng.sh.disabled` mag niet meetellen voor
# `streng.sh`). `grep -qF` op het hele bestand bewees alleen "komt als
# substring voor", niet "is geregistreerd" -- vandaar de JSON-parse.
#
# Drie exitcodes, niet twee: 0 gevonden, 1 niet gevonden (settings.json was
# wel geldige JSON en een object), 2 settings.json is kapot of geen object.
# check_pairing() gebruikt exitcode 2 om een eigen, onderscheidende melding te
# geven -- de oorzaak is dan settings.json, niet de skill-body.
hook_geregistreerd() {
  local settings="$1" hook="$2"
  python3 -c '
import json, re, sys

settings_path, hook = sys.argv[1], sys.argv[2]

# Padgrenzen aan beide kanten: het teken vlak vóór en vlak ná de match mag
# geen pad-/bestandsnaamteken zijn. Dat verwerpt zowel een langere prefix
# (".claude/hooks/other-streng.sh") als een langere suffix
# (".claude/hooks/streng.sh.disabled").
pattern = re.compile(r"(?<![A-Za-z0-9_.-])" + re.escape(hook) + r"(?![A-Za-z0-9_.-])")


def bevat_match(node):
    if isinstance(node, dict):
        for key, value in node.items():
            if key == "command" and isinstance(value, str) and pattern.search(value):
                return True
            if bevat_match(value):
                return True
        return False
    if isinstance(node, list):
        return any(bevat_match(item) for item in node)
    return False


try:
    with open(settings_path, encoding="utf-8") as f:
        data = json.load(f)
    found = bevat_match(data.get("hooks", {}))
except (OSError, json.JSONDecodeError, AttributeError):
    # AttributeError: settings.json is geldige JSON maar geen object (een
    # lijst of een kale string bijvoorbeeld) -- .get() bestaat dan niet.
    # Exitcode 2, niet 1: dit is een andere bevinding dan "hook niet gevonden".
    sys.exit(2)

sys.exit(0 if found else 1)
' "$settings" "$hook"
}

# --- Check 8: pairing --------------------------------------------------------
# Hook en skill/rule zijn één regel op twee momenten. `binding: hard` is de
# claim dat er een afdwinging bestaat; deze check maakt die claim toetsbaar.
# Geen vierde frontmatter-veld: de skill noemt zijn hook in de body, en die
# hook moet bestaan én in settings.json geregistreerd staan. Een hookscript dat
# nergens geregistreerd is, draait nooit. Het pad mag in een submap van
# `.claude/hooks/` staan: `lighthouse init` legt de zijne in `.claude/hooks/lighthouse/`.
check_pairing() {
  local settings="$ROOT/.claude/settings.json"
  local dir binding hooks hook gedekt gevonden=0 hard=0 overgeslagen=0
  local overslag_reden="" settings_bestaat=1 settings_kapot rc

  [ -f "$settings" ] || settings_bestaat=0

  while IFS= read -r dir; do
    binding="$(skill_meta "$CLAUDE_SKILLS/$dir" binding)"
    [ "$binding" = "hard" ] || continue
    hard=$((hard + 1))

    hooks="$(grep -oE '\.claude/hooks/([A-Za-z0-9_-]+/)*[A-Za-z0-9_-]+\.sh' \
      "$CLAUDE_SKILLS/$dir/SKILL.md" 2>/dev/null | LC_ALL=C sort -u)"

    # Geen enkel hookpad in de body genoemd: dat is een gemeten bevinding,
    # onafhankelijk van settings.json of python3. Pas als er wél een pad
    # genoemd is, wordt registratie de resterende vraag.
    if [ -z "$hooks" ]; then
      gevonden=1
      fail "pairing: '$dir' carries 'binding: hard' but names no counterpart that exists and is registered in settings.json"
      continue
    fi

    # `continue`, niet `return 0`: een ontbrekende voorwaarde maakt alléén de
    # registratie van DEZE skill onmeetbaar. Een `return 0` hier brak eerder
    # de hele lus af en liet elke alfabetisch latere hard-skill ongemeten --
    # inclusief een skill zonder enig hookpad, wat wél een gemeten FAIL is.
    if [ "$settings_bestaat" -eq 0 ]; then
      overgeslagen=1
      overslag_reden="pairing: '$settings' does not exist, so registration cannot be checked here"
      continue
    fi

    if [ "$HAVE_PYTHON3" -eq 0 ]; then
      overgeslagen=1
      overslag_reden="pairing: python3 is missing on this machine, so registration cannot be checked here"
      continue
    fi

    gedekt=0
    settings_kapot=0
    while IFS= read -r hook; do
      [ -z "$hook" ] && continue
      [ -f "$ROOT/$hook" ] || continue
      hook_geregistreerd "$settings" "$hook"
      rc=$?
      if [ "$rc" -eq 0 ]; then
        gedekt=1
        break
      elif [ "$rc" -eq 2 ]; then
        settings_kapot=1
      fi
    done <<<"$hooks"

    if [ "$gedekt" -eq 0 ]; then
      gevonden=1
      # Kapotte/geen-object settings.json is een FAIL, geen SKIPPED: het
      # bestand bestaat en is leesbaar, dus dit ís meetbaar -- en de oorzaak
      # is settings.json, niet de skill-body. Zonder deze tak kreeg de lezer
      # dezelfde "noemt geen tegenhanger"-boodschap en ging de skill-body
      # repareren in plaats van settings.json.
      if [ "$settings_kapot" -eq 1 ]; then
        fail "pairing: '$settings' is not valid JSON (or not an object), so '$dir' cannot be checked for a registered hook"
      else
        fail "pairing: '$dir' carries 'binding: hard' but names no counterpart that exists and is registered in settings.json"
      fi
    fi
  done < <(skill_dirs "$CLAUDE_SKILLS")

  if [ "$hard" -eq 0 ]; then
    ok "no skill claims 'binding: hard'"
    return 0
  fi

  # Onvoorwaardelijk: een niet-meetbare skill hoort altijd luid gemeld te
  # worden, ook naast een FAIL elders. Eerder onderdrukte een `elif` op
  # gevonden deze regel zodra er ook maar één FAIL viel.
  [ "$overgeslagen" -eq 1 ] && skip "$overslag_reden"
  [ "$gevonden" -eq 0 ] && [ "$overgeslagen" -eq 0 ] && ok "every binding: hard has a registered hook"
  return 0
}

# --- Check 10: omvang van CLAUDE.md + AGENTS.md -----------------------------
# Zonder teller kruipt het woordental gewoon weer omhoog. CLAUDE.md is een
# `@AGENTS.md`-import plus de Claude-rest: alleen CLAUDE.md tellen zou een
# placebo zijn, dus telt de check wat Claude Code werkelijk laadt -- beide.
check_omvang() {
  local claude="$ROOT/CLAUDE.md" agents="$ROOT/AGENTS.md"
  local label="CLAUDE.md" woorden=0 n bestand
  local -a bestanden=("$claude")

  if [ "$HAVE_WC" -eq 0 ]; then
    skip "size: 'wc' is missing on this machine, so CLAUDE.md cannot be counted here"
    return 0
  fi

  if [ ! -f "$claude" ]; then
    skip "size: '$claude' does not exist, so there is nothing to count here"
    return 0
  fi

  if [ -z "$PLAFOND" ]; then
    skip "size: python3 is missing on this machine, so [project] instructions_word_ceiling cannot be read"
    return 0
  fi

  if [ -f "$agents" ]; then
    bestanden+=("$agents")
    label="CLAUDE.md + AGENTS.md"
  fi

  for bestand in "${bestanden[@]}"; do
    if [ ! -r "$bestand" ]; then
      skip "size: '$bestand' is not readable, so there is nothing to count here"
      return 0
    fi

    # `wc -w` op een onleesbaar bestand levert een lege stdout: zonder deze
    # validatie gooit `[ "" -gt "$PLAFOND" ]` "integer expression expected" op
    # stderr, valt de if-tak stil door naar ok(), en meldt de gate "0 woorden,
    # binnen het plafond" over een bestand dat hij niet gemeten heeft.
    n="$(wc -w <"$bestand" 2>/dev/null | tr -d '[:space:]')"
    if ! [[ "$n" =~ ^[0-9]+$ ]]; then
      skip "size: 'wc' could not count '$bestand', so there is nothing to count here"
      return 0
    fi
    woorden=$((woorden + n))
  done

  if [ "$woorden" -gt "$PLAFOND" ]; then
    fail "size: $label counts $woorden words, ceiling is $PLAFOND"
    return 0
  fi

  ok "$label counts $woorden words, within the ceiling of $PLAFOND"
  return 0
}

if [ "${1:-}" = "--print-mirror-exceptions" ]; then
  spiegel_verschillen || {
    printf 'SKIPPED  diff could not compare a skill pair\n' >&2
    exit 4
  }
  exit 0
fi

check_dubbele_naam
check_byte_identiek
check_spiegel
check_spiegel_inhoud
check_overlap
check_schaduw
check_scoping
check_pairing
check_omvang

if [ "$findings" -gt 0 ]; then
  printf '\n%d finding(s).\n' "$findings"
  exit 1
fi
exit 0
