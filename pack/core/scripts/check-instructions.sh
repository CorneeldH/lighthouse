#!/usr/bin/env bash
# Dertien deterministische checks over de instructielaag van deze repo.
#
# Laag 2 van de gate-spec (commit-tijd): geen netwerk, geen LLM, geen state.
# Het maakt de instructielaag zelf meetbaar -- het verschil tussen een norm en
# een wens.
#
# Uitvoercontract, drie uitkomsten en niet twee:
#   ok            de check heeft gemeten en niets gevonden
#   FOUT          de check heeft gemeten en iets gevonden -> exit 1
#   OVERGESLAGEN  de check kon hier niet meten. Exit 0, maar luid: de
#                 afwezigheid van een signaal is geen negatief resultaat.
#
# Draaien:  scripts/hooks/check-instructies.sh
# Testen:   uv run pytest tests/unit/test_check_instructies.py -x --tb=short
set -uo pipefail

ROOT="${CHECK_INSTRUCTIES_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)}"
CLAUDE_SKILLS="$ROOT/.claude/skills"
AGENTS_SKILLS="$ROOT/.agents/skills"
PERSONAL_SKILLS="${CHECK_INSTRUCTIES_PERSONAL:-$HOME/.claude/skills}"
PERSONAL_AGENTS="${CHECK_INSTRUCTIES_PERSONAL_AGENTS:-$HOME/.claude/agents}"

findings=0

fail() {
  printf 'FOUT  %s\n' "$1"
  findings=$((findings + 1))
}

skip() { printf 'OVERGESLAGEN  %s\n' "$1"; }
ok() { printf 'ok    %s\n' "$1"; }

# sha256sum bestaat niet op macOS. Deze repo wordt op de Beelink EN op een Mac
# bewerkt, en een gate die op een van de twee stil niets doet is geen gate.
if command -v sha256sum >/dev/null 2>&1; then
  SHA_CMD="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
  SHA_CMD="shasum -a 256"
else
  printf 'FOUT  geen sha256sum en geen shasum op deze machine; de duplicaatcheck kan niet draaien\n'
  exit 1
fi

# python3 ontbreekt zelden, maar check 8 (pairing) parseert settings.json ermee
# en kan zonder python3 niet meten of een hookpad geregistreerd staat. Anders
# dan sha256sum/shasum raakt dit maar één check -- vandaar OVERGESLAGEN op die
# ene check in plaats van een script-brede FOUT.
if command -v python3 >/dev/null 2>&1; then
  HAVE_PYTHON3=1
else
  HAVE_PYTHON3=0
fi

# 'wc' hoort tot coreutils en ontbreekt zelden, maar check 10 (omvang) kan
# zonder niet meten of de instructies binnen het plafond blijven. Zelfde behandeling
# als python3 hierboven: dit raakt maar één check, dus OVERGESLAGEN op die ene
# check in plaats van een script-brede FOUT.
if command -v wc >/dev/null 2>&1; then
  HAVE_WC=1
else
  HAVE_WC=0
fi

# Woordental waarboven de altijd geladen instructies (CLAUDE.md plus het
# AGENTS.md dat het importeert) weer behang worden. Aanvang van de herverdeling
# was 3046 woorden (#42); dit plafond ligt daar net onder, zodat het geheel
# niet ongemerkt terugkruipt.
PLAFOND="${CHECK_INSTRUCTIES_PLAFOND:-3000}"

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
# `frontmatter_field` werkt op een bestandspad, zodat check 12 hem ook op een
# agent-`.md` kan zetten; `skill_field` blijft de wrapper voor een skillmap.
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
  local naam dir dubbel gevonden=0
  local regels=""
  while IFS= read -r dir; do
    naam="$(skill_field "$CLAUDE_SKILLS/$dir" name)"
    [ -n "$naam" ] && regels+="$naam"$'\t'"$dir"$'\n'
  done < <(skill_dirs "$CLAUDE_SKILLS")

  while IFS= read -r dubbel; do
    [ -z "$dubbel" ] && continue
    gevonden=1
    fail "dubbele naam '$dubbel' geclaimd door: $(printf '%s' "$regels" |
      awk -F'\t' -v n="$dubbel" '$1 == n { printf "%s ", $2 }')"
  done < <(printf '%s' "$regels" | cut -f1 | LC_ALL=C sort | uniq -d)

  [ "$gevonden" -eq 0 ] && ok "geen dubbel geclaimde namen"
  return 0
}

# --- Check 2: byte-identiek paar -------------------------------------------
# Twee skill-mappen met identieke inhoud. Het `generate-slides-retro` /
# `generate_slides_retro`-geval: zelfde md5, en de tweede kost een volle
# description in elke sessie.
check_byte_identiek() {
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
      fail "byte-identiek paar in $(basename "$(dirname "$wortel")")/skills: $(printf '%s' "$regels" |
        awk -F'\t' -v h="$dubbel" '$1 == h { printf "%s ", $2 }')"
    done < <(printf '%s' "$regels" | cut -f1 | LC_ALL=C sort | uniq -d)
  done

  [ "$gevonden" -eq 0 ] && ok "geen byte-identieke paren binnen een skillmap"
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
    fail "spiegel ontbreekt: '$ontbreekt' staat niet in beide van .claude/skills/ en .agents/skills/"
  done < <(LC_ALL=C comm -3 <(skill_dirs "$CLAUDE_SKILLS") <(skill_dirs "$AGENTS_SKILLS") | tr -d '\t')

  [ "$gevonden" -eq 0 ] && ok "spiegel .claude/skills <-> .agents/skills is gelijk"
  return 0
}

# --- Check 11: spiegel-inhoud (#981) ---------------------------------------
# Check 3 ziet alleen namen; de inhoud van een paar liep ongemerkt uiteen. Deze
# check vergelijkt elk bestand van elk paar. Wat per harnas bewust verschilt
# ("Claude Code" tegenover "Codex", een OpenCode-alinea) staat in
# SPIEGEL_UITZONDERINGEN, in dezelfde vorm als spiegel_verschillen() print.
# Geen regelnummers in die vorm: een gelijke edit aan beide kanten verschuift
# de verschillen maar verandert ze niet, en mag dus niet rood worden.
SPIEGEL_UITZONDERINGEN="$ROOT/scripts/hooks/spiegel-uitzonderingen.txt"

# Print per verschillend bestand een kop en de afwijkende regels (`<` = .claude,
# `>` = .agents). Exit 4 als diff een paar niet kon vergelijken: dat is "kon
# niet meten", nooit "geen verschil".
spiegel_verschillen() {
  local naam rel links rechts uit rc lijst_links lijst_rechts
  while IFS= read -r naam; do
    [ -z "$naam" ] && continue
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
      # `git diff`, niet `diff`: GNU diff (Beelink) en de FreeBSD-diff van macOS
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
  local actueel verwacht=""
  if ! actueel="$(spiegel_verschillen)"; then
    skip "spiegel-inhoud: diff kon een skillpaar niet vergelijken, dus de inhoud is niet gemeten"
    return 0
  fi
  [ -f "$SPIEGEL_UITZONDERINGEN" ] && verwacht="$(cat "$SPIEGEL_UITZONDERINGEN")"

  if [ "$actueel" = "$verwacht" ]; then
    ok "spiegel-inhoud gelijk, op de vastgelegde uitzonderingen na"
    return 0
  fi

  fail "spiegel-inhoud wijkt af van scripts/hooks/spiegel-uitzonderingen.txt (- vastgelegd, + nu; < .claude, > .agents):"
  diff -u <(printf '%s\n' "$verwacht") <(printf '%s\n' "$actueel") | tail -n +3 | sed 's/^/      /'
  printf '      Trek de andere kant gelijk. Is het verschil bewust per harnas, leg het dan vast met\n'
  printf '      scripts/hooks/check-instructies.sh --print-spiegel-uitzonderingen > scripts/hooks/spiegel-uitzonderingen.txt\n'
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
    PAIA_STOPWOORDEN="$(printf '%s' "$STOPWOORDEN" | tr '\n' ' ')" awk '
      BEGIN { stop = ENVIRON["PAIA_STOPWOORDEN"] }
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
      fail "overlap zonder afgrenzende clausule: '${namen[i]}' en '${namen[j]}' delen $gedeeld triggerwoorden"
    done
  done

  [ "$gevonden" -eq 0 ] && ok "geen onafgegrensde overlap tussen descriptions"
  return 0
}

# --- Check 5: schaduwende personal skill ------------------------------------
# Personal skills OVERSCHADUWEN gelijknamige project-skills -- omgekeerd aan
# settings.json. Een naam die in beide staat betekent dat de project-versie
# nooit laadt, en dat gebeurt stil.
check_schaduw() {
  if [ ! -d "$PERSONAL_SKILLS" ]; then
    skip "schaduwcheck: '$PERSONAL_SKILLS' bestaat niet op deze machine, dus er valt hier niets te meten"
    return 0
  fi

  local naam gevonden=0
  while IFS= read -r naam; do
    [ -z "$naam" ] && continue
    gevonden=1
    fail "schaduw: '$naam' staat in $PERSONAL_SKILLS en in .claude/skills/; de personal-versie wint"
  done < <(LC_ALL=C comm -12 <(skill_dirs "$PERSONAL_SKILLS") <(skill_dirs "$CLAUDE_SKILLS"))

  [ "$gevonden" -eq 0 ] && ok "geen personal skill schaduwt een project-skill"
  return 0
}

# --- Check 12: aanwezigheid van personal skills en agents (#905) ------------
# Check 5 vangt een NAAMSBOTSING: een personal skill die een project-skill
# overschaduwt. Dit is iets anders: elke personal skill of agent die Claude
# Code laadt kost tokens in elke sessie, ook zonder botsing. Daarom telt
# aanwezigheid zelf, ongeacht naam. De ontsnapping is alleen de exacte waarde
# CHECK_INSTRUCTIES_PERSONAL_OK=1; "ja" of "true" is een typfout, geen akkoord.
#
# `skills/synced/` is de claude.ai-account-sync: beheerd door die sync, niet
# door de gebruiker; de archiefroute is daar niet de knop. Hij telt dus niet
# mee voor de uitkomst, maar aantal en tokens worden wel luid gemeld.
#
# `find -H`: een PERSONAL_SKILLS of PERSONAL_AGENTS die zelf een symlink is
# (dotfiles-opzet) moet gevolgd worden, anders meet `find` niets en volgt een
# valse `ok`. Alleen het startpunt: submappen volgen we niet (geen -L, lusrisico),
# dus een symlink naar een submap onder agents/ wordt niet binnengegaan.

# Vind in een map; vult AANW_LIJST (gesorteerd) en geeft de exitstatus van
# find terug, zodat een onleesbare submap geen stille `ok` wordt. Een pijp
# naar sort zou die status verbergen.
aanwezigheid_find() {
  local uit rc
  uit="$(find -H "$@")"
  rc=$?
  AANW_LIJST="$(printf '%s\n' "$uit" | LC_ALL=C sort)"
  return "$rc"
}

# Tekens die een skill in de sessie-context kost: naam (frontmatter, anders de
# mapnaam) plus description.
aanwezigheid_skill_tekens() {
  local pad="$1" naam beschrijving
  naam="$(frontmatter_field "$pad/SKILL.md" name)"
  [ -z "$naam" ] && naam="${pad##*/}"
  beschrijving="$(frontmatter_field "$pad/SKILL.md" description)"
  echo $((${#naam} + ${#beschrijving}))
}

check_aanwezigheid() {
  local map
  for map in "$PERSONAL_SKILLS" "$PERSONAL_AGENTS"; do
    # Een map die niet bestaat is gemeten: daar laadt niets. Bestaat hij maar
    # is hij niet te lezen, dan weten we het niet -- dat is geen "ok".
    if [ -e "$map" ] && { [ ! -r "$map" ] || [ ! -x "$map" ]; }; then
      skip "aanwezigheid: '$map' bestaat maar is niet leesbaar, dus er valt hier niets te tellen"
      return 0
    fi
  done

  local aantal_skills=0 aantal_agents=0 tekens=0 aanwezig="" pad naam beschrijving
  if [ -d "$PERSONAL_SKILLS" ]; then
    if ! aanwezigheid_find "$PERSONAL_SKILLS" -mindepth 1 -maxdepth 1 ! -name '.*' ! -name synced \( -type d -o -type l \); then
      skip "aanwezigheid: find faalde op '$PERSONAL_SKILLS' (onleesbare submap?), dus er valt hier niets te tellen"
      return 0
    fi
    while IFS= read -r pad; do
      [ -z "$pad" ] && continue
      aantal_skills=$((aantal_skills + 1))
      aanwezig="${aanwezig:+$aanwezig, }${pad##*/}"
      tekens=$((tekens + $(aanwezigheid_skill_tekens "$pad")))
    done <<< "$AANW_LIJST"
  fi
  if [ -d "$PERSONAL_AGENTS" ]; then
    if ! aanwezigheid_find "$PERSONAL_AGENTS" -mindepth 1 -name '.*' -prune -o -name '*.md' \( -type f -o -type l \) -print; then
      skip "aanwezigheid: find faalde op '$PERSONAL_AGENTS' (onleesbare submap?), dus er valt hier niets te tellen"
      return 0
    fi
    while IFS= read -r pad; do
      [ -z "$pad" ] && continue
      naam="$(frontmatter_field "$pad" name)"
      beschrijving="$(frontmatter_field "$pad" description)"
      tekens=$((tekens + ${#naam} + ${#beschrijving}))
      naam="${pad##*/}"
      naam="${naam%.md}"
      aantal_agents=$((aantal_agents + 1))
      aanwezig="${aanwezig:+$aanwezig, }$naam"
    done <<< "$AANW_LIJST"
  fi

  # Zelfde CHARS_PER_TOKEN = 4 als scripts/measure_skill_surface.py.
  local tokens=$((tekens / 4))
  if [ $((aantal_skills + aantal_agents)) -eq 0 ]; then
    ok "aanwezigheid: geen personal skills of agents"
  elif [ "${CHECK_INSTRUCTIES_PERSONAL_OK:-}" = "1" ]; then
    ok "aanwezigheid: $aantal_skills personal skill(s), $aantal_agents personal agent(s), ~$tokens tokens per sessie, toegestaan via CHECK_INSTRUCTIES_PERSONAL_OK=1"
  else
    fail "aanwezigheid: $aantal_skills personal skill(s) in $PERSONAL_SKILLS, $aantal_agents personal agent(s) in $PERSONAL_AGENTS, ~$tokens tokens in elke sessie: $aanwezig. Verplaats ze naar ~/.claude/skills-archief/<datum>/, of sta ze tijdelijk toe met CHECK_INSTRUCTIES_PERSONAL_OK=1"
  fi

  # synced/<account>/<skill>: niet meegeteld voor de uitkomst, wel gemeten.
  if [ -d "$PERSONAL_SKILLS/synced" ]; then
    local aantal_synced=0 tekens_synced=0 account accounts
    if ! aanwezigheid_find "$PERSONAL_SKILLS/synced" -mindepth 1 -maxdepth 1 ! -name '.*' \( -type d -o -type l \); then
      skip "aanwezigheid: find faalde op '$PERSONAL_SKILLS/synced' (onleesbare submap?), dus synced/ is niet gemeten"
      return 0
    fi
    accounts="$AANW_LIJST"
    while IFS= read -r account; do
      [ -z "$account" ] && continue
      if ! aanwezigheid_find "$account" -mindepth 1 -maxdepth 1 ! -name '.*' \( -type d -o -type l \); then
        skip "aanwezigheid: find faalde op '$account' (onleesbare submap?), dus synced/ is niet gemeten"
        return 0
      fi
      while IFS= read -r pad; do
        [ -z "$pad" ] && continue
        aantal_synced=$((aantal_synced + 1))
        tekens_synced=$((tekens_synced + $(aanwezigheid_skill_tekens "$pad")))
      done <<< "$AANW_LIJST"
    done <<< "$accounts"
    ok "aanwezigheid: $PERSONAL_SKILLS/synced/ niet meegeteld: $aantal_synced skill(s), ~$((tekens_synced / 4)) tokens (claude.ai-sync, beheer via claude.ai)"
  fi
  return 0
}

# --- Check 6: naamcontract, met vrijstelling voor origin: external ----------
# Alles in .claude/skills/ draagt een `paia-`-voorvoegsel. Uitzondering: een
# gevendorde externe skill. Hernoemen breekt daar de updateroute (de folderhash
# verwijst naar een pad) en de attributie. De vrijstelling geldt alleen als hij
# aantoonbaar is -- upstream EN license gevuld.
check_naamcontract() {
  local dir origin upstream licentie gevonden=0
  while IFS= read -r dir; do
    case "$dir" in
      paia-*) continue ;;
    esac
    origin="$(skill_meta "$CLAUDE_SKILLS/$dir" origin)"
    if [ "$origin" != "external" ]; then
      gevonden=1
      fail "naamcontract: '$dir' heeft geen 'paia-'-voorvoegsel en geen 'origin: external'"
      continue
    fi
    upstream="$(skill_meta "$CLAUDE_SKILLS/$dir" upstream)"
    licentie="$(skill_field "$CLAUDE_SKILLS/$dir" license)"
    if [ -z "$upstream" ] || [ -z "$licentie" ]; then
      gevonden=1
      fail "herkomstkop: '$dir' claimt 'origin: external' maar mist 'upstream' of 'license'; de vrijstelling is niet aantoonbaar"
    fi
  done < <(skill_dirs "$CLAUDE_SKILLS")

  [ "$gevonden" -eq 0 ] && ok "naamcontract gerespecteerd (origin: external vrijgesteld)"
  return 0
}

# --- Check 7: elke rule is pad-gescoped --------------------------------------
# Een rule zonder `paths:` laadt elke sessie en kost dus hetzelfde als
# CLAUDE.md. De winst van rules zit volledig in de scoping; zonder die regel is
# het CLAUDE.md met extra stappen.
check_scoping() {
  local rules="$ROOT/.claude/rules" bestand gevonden=0

  if [ ! -d "$rules" ]; then
    skip "scoping: '$rules' bestaat niet, dus er valt hier geen rule te toetsen"
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
      fail "scoping: '$(basename "$bestand")' heeft geen paths:-blok met een niet-lege waarde in zijn frontmatter"
    fi
  done < <(find "$rules" -maxdepth 1 -type f -name '*.md' | LC_ALL=C sort)

  [ "$gevonden" -eq 0 ] && ok "elke rule is pad-gescoped"
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
# nergens geregistreerd is, draait nooit.
check_pairing() {
  local settings="$ROOT/.claude/settings.json"
  local dir binding hooks hook gedekt gevonden=0 hard=0 overgeslagen=0
  local overslag_reden="" settings_bestaat=1 settings_kapot rc

  [ -f "$settings" ] || settings_bestaat=0

  while IFS= read -r dir; do
    binding="$(skill_meta "$CLAUDE_SKILLS/$dir" binding)"
    [ "$binding" = "hard" ] || continue
    hard=$((hard + 1))

    hooks="$(grep -oE '\.claude/hooks/[A-Za-z0-9_-]+\.sh' \
      "$CLAUDE_SKILLS/$dir/SKILL.md" 2>/dev/null | LC_ALL=C sort -u)"

    # Geen enkel hookpad in de body genoemd: dat is een gemeten bevinding,
    # onafhankelijk van settings.json of python3. Pas als er wél een pad
    # genoemd is, wordt registratie de resterende vraag.
    if [ -z "$hooks" ]; then
      gevonden=1
      fail "pairing: '$dir' draagt 'binding: hard' maar noemt geen tegenhanger die bestaat én in settings.json staat"
      continue
    fi

    # `continue`, niet `return 0`: een ontbrekende voorwaarde maakt alléén de
    # registratie van DEZE skill onmeetbaar. Een `return 0` hier brak eerder
    # de hele lus af en liet elke alfabetisch latere hard-skill ongemeten --
    # inclusief een skill zonder enig hookpad, wat wél een gemeten FOUT is.
    if [ "$settings_bestaat" -eq 0 ]; then
      overgeslagen=1
      overslag_reden="pairing: '$settings' bestaat niet, dus registratie is hier niet te toetsen"
      continue
    fi

    if [ "$HAVE_PYTHON3" -eq 0 ]; then
      overgeslagen=1
      overslag_reden="pairing: python3 ontbreekt op deze machine, dus registratie is hier niet te toetsen"
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
      # Kapotte/geen-object settings.json is een FOUT, geen OVERGESLAGEN: het
      # bestand bestaat en is leesbaar, dus dit ís meetbaar -- en de oorzaak
      # is settings.json, niet de skill-body. Zonder deze tak kreeg de lezer
      # dezelfde "noemt geen tegenhanger"-boodschap en ging de skill-body
      # repareren in plaats van settings.json.
      if [ "$settings_kapot" -eq 1 ]; then
        fail "pairing: '$settings' is geen geldige JSON (of geen object), dus '$dir' kan niet getoetst worden op een geregistreerde hook"
      else
        fail "pairing: '$dir' draagt 'binding: hard' maar noemt geen tegenhanger die bestaat én in settings.json staat"
      fi
    fi
  done < <(skill_dirs "$CLAUDE_SKILLS")

  if [ "$hard" -eq 0 ]; then
    ok "geen skill claimt 'binding: hard'"
    return 0
  fi

  # Onvoorwaardelijk: een niet-meetbare skill hoort altijd luid gemeld te
  # worden, ook naast een FOUT elders. Eerder onderdrukte een `elif` op
  # gevonden deze regel zodra er ook maar één FOUT viel.
  [ "$overgeslagen" -eq 1 ] && skip "$overslag_reden"
  [ "$gevonden" -eq 0 ] && [ "$overgeslagen" -eq 0 ] && ok "elke binding: hard heeft een geregistreerde hook"
  return 0
}

# Licentienamen die als vermelding tellen. Bewust kort: dit is een
# aanwezigheidstoets, geen SPDX-parser. Copyleft staat er niet bij omdat een
# gevendorde AGPL/GPL-skill hier sowieso niet mag landen.
#
# Structurele grens, geen letter-grens. Een eerdere versie gebruikte een
# letter-grens ((^|[^A-Za-z])...([^A-Za-z]|$)); die loste de zes gevallen
# hierboven op, maar opende een nieuw gat: een licentienaam in een pad matcht
# een letter-grens ook -- github.com/foo/mit-tools, apache/airflow,
# github.com/CorneeldH/werken-mit-claude, foo/bsd-tools, owner/repo cc0-fork
# gaven allemaal ten onrechte 'ok'. Een echte licentievermelding staat achter
# een `(` of een `,` (zie de twee vendorde skills hieronder); een licentienaam
# in een pad staat achter een `/` of een `-`. Vandaar het anker op `[(,]`
# i.p.v. op willekeurige niet-letters: dat onderscheidt de twee gevallen waar
# een letter-grens dat niet kan.
#
# Gevolg, met opzet: `upstream: owner/repo MIT` -- een kale licentienaam
# zonder `(` of `,` ervoor -- telt vanaf nu NIET meer als vermelding en geeft
# FOUT. Schrijf 'm als `(MIT)` of `, MIT`. Verruim dit anker niet terug naar
# een letter-grens: dat maakt elk pad met een licentienaam erin weer geldig.
#
# Rechterkant blijft een niet-letter-grens ([^A-Za-z]|$): dat is waar 'MIT'
# in 'MITRE' of 'ISC' in 'ISCSI' stopt, en een `(`/`,` aan de rechterkant is
# hier niet nodig -- '(MIT,' en '(MIT)' hebben allebei al de linker `(`.
#
# `-i` erbij zodat kleine letters ('(mit)', '(apache-2.0)') ook meetellen;
# met het structurele anker is dat veilig, want een licentienaam in een pad
# staat nooit direct achter een `(` of een `,`.
LICENTIE_PATROON='[(,][[:space:]]*(MIT|Apache|BSD|ISC|MPL|HPND|Unlicense|CC0)([^A-Za-z]|$)'

# De vijf vormen die `source:` mag dragen (zie stap 5 van paia-instrumenteren).
# `self` en een pad-vorm hebben geen patroon: `self` is een letterlijke
# vergelijking en alles wat geen van de vier andere vormen is, wordt als
# repo-pad gelezen en moet dan bestaan. Dat is bewust de restcategorie -- een
# pad-detectie op '/' of '.' zou een vrije tekst ("uit mijn hoofd") stil
# doorlaten.
SOURCE_ISSUE='^#[0-9]+$'
SOURCE_COMMIT='^[0-9a-f]{7,40}$'
SOURCE_URL='^https?://'

# De pin op de upstream-versie: altijd de volle 40-tekens-SHA, kleine letters.
UPSTREAM_COMMIT='^[0-9a-f]{40}$'

# --- Check 9: herkomst, repo-breed ------------------------------------------
# Check 6 toetst upstream+license alleen op skills ZONDER paia--voorvoegsel,
# als voorwaarde voor de naamcontract-vrijstelling. Een paia--skill met
# `origin: extended` glipt daar doorheen. Zonder upstream is een overgenomen
# skill over drie maanden niet bij te werken; zonder licentie mag hij hier niet
# staan (gehost product, kopiëren is de handeling die copyleft laat overslaan).
# `upstream-commit` (en bij `external` ook `upstream-path`) maakt de pin
# machine-leesbaar: de bumplane (#1140) kan zo de kopie tegen upstream diffen en
# een versiesprong wordt een reviewbare diff in plaats van een gok.
check_herkomst() {
  local dir origin upstream licentie commit pad bron gevonden=0 bron_gevonden=0
  while IFS= read -r dir; do
    origin="$(skill_meta "$CLAUDE_SKILLS/$dir" origin)"
    case "$origin" in
      extended | external) ;;
      *) continue ;;
    esac

    upstream="$(skill_meta "$CLAUDE_SKILLS/$dir" upstream)"
    if [ -z "$upstream" ]; then
      gevonden=1
      fail "herkomst: '$dir' draagt 'origin: $origin' zonder upstream; de bron is niet terug te vinden"
      continue
    fi

    licentie="$(skill_field "$CLAUDE_SKILLS/$dir" license)"
    if [ -z "$licentie" ] && ! printf '%s' "$upstream" | grep -qiE "$LICENTIE_PATROON"; then
      gevonden=1
      fail "herkomst: '$dir' draagt 'origin: $origin' zonder licentievermelding, niet als license:-veld en niet in de upstream-regel; schrijf 'm als '(MIT)' of ', MIT' -- achter een haakje of komma, anders telt hij niet"
    fi

    # Quotes horen niet bij de waarde (zie de source-lus); strip ze voor de toets.
    commit="$(skill_meta "$CLAUDE_SKILLS/$dir" upstream-commit)"
    commit="${commit%\"}"
    commit="${commit#\"}"
    commit="${commit%\'}"
    commit="${commit#\'}"
    if [ -z "$commit" ]; then
      gevonden=1
      fail "herkomst: '$dir' draagt 'origin: $origin' zonder upstream-commit; niet te zien op welke upstream-versie de kopie staat"
    elif ! printf '%s' "$commit" | grep -qE "$UPSTREAM_COMMIT"; then
      gevonden=1
      fail "herkomst: '$dir' draagt upstream-commit '$commit' -- geen volledige 40-tekens-SHA in kleine letters"
    fi

    if [ "$origin" = external ]; then
      pad="$(skill_meta "$CLAUDE_SKILLS/$dir" upstream-path)"
      pad="${pad%\"}"
      pad="${pad#\"}"
      pad="${pad%\'}"
      pad="${pad#\'}"
      if [ -z "$pad" ]; then
        gevonden=1
        fail "herkomst: '$dir' draagt 'origin: external' zonder upstream-path; de bron binnen de upstream-repo is niet eenduidig"
      elif [ "${pad#/}" != "$pad" ]; then
        gevonden=1
        fail "herkomst: '$dir' draagt upstream-path '$pad' met voorloop-/; het pad is relatief binnen de upstream-repo"
      fi
    fi
  done < <(skill_dirs "$CLAUDE_SKILLS")

  [ "$gevonden" -eq 0 ] && ok "elke overgenomen skill draagt herkomst, commit en licentie"

  # Tweede lus, over ALLE skills en niet alleen de overgenomen: de vorm van
  # `source`. Afwezigheid is geen fout -- er is geen backfill, dus de 33
  # bestaande skills dragen het veld niet. Wat dit vangt is de fout die anders
  # stil blijft: een `source` die naar een verplaatst of hernoemd bestand wijst,
  # want niets anders leest dat pad ooit.
  while IFS= read -r dir; do
    bron="$(skill_meta "$CLAUDE_SKILLS/$dir" source)"
    [ -z "$bron" ] && continue

    # Een `#`-waarde hoort geciteerd ("#914"), anders leest YAML hem als
    # commentaar. Die quotes horen niet bij de waarde; strip ze voor de
    # vormtoets. Ongequoteerd komt hier ook binnen -- dat gat dicht de
    # YAML-kant (test_frontmatter_is_valid_yaml eist een string-waarde,
    # en `source: #914` parseert naar null).
    bron="${bron%\"}"
    bron="${bron#\"}"
    bron="${bron%\'}"
    bron="${bron#\'}"

    if [ "$bron" = "self" ] ||
      printf '%s' "$bron" | grep -qE "$SOURCE_ISSUE" ||
      printf '%s' "$bron" | grep -qE "$SOURCE_URL" ||
      printf '%s' "$bron" | grep -qE "$SOURCE_COMMIT"; then
      continue
    fi

    if [ ! -e "$ROOT/$bron" ]; then
      bron_gevonden=1
      fail "herkomst: '$dir' draagt 'source: $bron' -- geen issue (#123), geen commit-sha, geen https-URL en geen 'self', en als pad bestaat het niet in de repo"
    fi
  done < <(skill_dirs "$CLAUDE_SKILLS")

  [ "$bron_gevonden" -eq 0 ] && ok "elke source wijst ergens heen"
  return 0
}

# --- Check 10: omvang van CLAUDE.md + AGENTS.md -----------------------------
# De DoD van milestone #42, permanent gemaakt. Zonder teller kruipt het
# woordental gewoon weer omhoog -- dat is precies wat er tussen 2026-08-22 en
# 2026-09-02 gebeurde: 3046 -> 5053. CLAUDE.md is sinds de samenvoeging een
# `@AGENTS.md`-import plus de Claude-rest: alleen CLAUDE.md tellen zou een
# placebo zijn, dus telt de check wat Claude Code werkelijk laadt -- beide.
check_omvang() {
  local claude="$ROOT/CLAUDE.md" agents="$ROOT/AGENTS.md"
  local label="CLAUDE.md" woorden=0 n bestand
  local -a bestanden=("$claude")

  if [ "$HAVE_WC" -eq 0 ]; then
    skip "omvang: 'wc' ontbreekt op deze machine, dus CLAUDE.md is hier niet te tellen"
    return 0
  fi

  if [ ! -f "$claude" ]; then
    skip "omvang: '$claude' bestaat niet, dus er valt hier niets te tellen"
    return 0
  fi

  if [ -f "$agents" ]; then
    bestanden+=("$agents")
    label="CLAUDE.md + AGENTS.md"
  fi

  for bestand in "${bestanden[@]}"; do
    if [ ! -r "$bestand" ]; then
      skip "omvang: '$bestand' is niet leesbaar, dus er valt hier niets te tellen"
      return 0
    fi

    # `wc -w` op een onleesbaar bestand levert een lege stdout: zonder deze
    # validatie gooit `[ "" -gt "$PLAFOND" ]` "integer expression expected" op
    # stderr, valt de if-tak stil door naar ok(), en meldt de gate "0 woorden,
    # binnen het plafond" over een bestand dat hij niet gemeten heeft.
    n="$(wc -w <"$bestand" 2>/dev/null | tr -d '[:space:]')"
    if ! [[ "$n" =~ ^[0-9]+$ ]]; then
      skip "omvang: 'wc' kon '$bestand' niet tellen, dus er valt hier niets te tellen"
      return 0
    fi
    woorden=$((woorden + n))
  done

  if [ "$woorden" -gt "$PLAFOND" ]; then
    fail "omvang: $label telt $woorden woorden, plafond is $PLAFOND"
    return 0
  fi

  ok "$label telt $woorden woorden, binnen het plafond van $PLAFOND"
  return 0
}

# --- Check 13: domein (#1161) -----------------------------------------------
# Milestone #58 wil dat elk pad in de repo bij precies een domein hoort, en de
# SKILL.md-frontmatter is voor een skill de enige drager (tenet J): geen tweede
# register dat uit de pas kan lopen. Anders dan `source` (check 9, afwezigheid
# is daar geen fout) is dit veld verplicht voor elke skill, want een skill
# zonder domein hoort bij geen enkel pad en valt dus buiten de indeling. Het
# veld staat onder `metadata:` (zie skill_meta); een top-level `domain:` telt
# als afwezig. Beide spiegels worden gelezen; dat ze hetzelfde domein dragen
# bewaakt check 11. De vier waarden staan ook in tests/unit/test_instructielagen.py
# (DOMEINEN) -- houd ze gelijk.
check_domein() {
  local wortel rel dir domein gevonden=0 overgeslagen=0
  for wortel in "$CLAUDE_SKILLS" "$AGENTS_SKILLS"; do
    rel="${wortel#"$ROOT"/}"
    while IFS= read -r dir; do
      # Een map zonder SKILL.md is het oordeel van een andere check.
      [ -e "$wortel/$dir/SKILL.md" ] || continue
      if [ ! -r "$wortel/$dir/SKILL.md" ]; then
        skip "domein: '$rel/$dir' niet leesbaar"
        overgeslagen=1
        continue
      fi

      domein="$(skill_meta "$wortel/$dir" domain)"
      # Eerst afsluitende witruimte, dan de quotes (zie de source-lus van check 9):
      # `domain: "inhoud" ` met een spatie na de quote blijft zo geldig. Alleen
      # één bijpassend paar telt als YAML-quoting: `"'factory'"` is voor een
      # parser de waarde `'factory'`, en die hoort als onbekend te falen.
      domein="${domein%"${domein##*[![:space:]]}"}"
      case "$domein" in
        \"*\") domein="${domein#\"}" domein="${domein%\"}" ;;
        \'*\') domein="${domein#\'}" domein="${domein%\'}" ;;
      esac

      case "$domein" in
        applicatie | inhoud | factory | infra) ;;
        "")
          gevonden=1
          fail "domein: '$rel/$dir' mist metadata.domain; kies applicatie, inhoud, factory of infra"
          ;;
        *)
          gevonden=1
          fail "domein: '$rel/$dir' draagt '$domein' -- geen van applicatie, inhoud, factory, infra"
          ;;
      esac
    done < <(skill_dirs "$wortel")
  done

  [ "$gevonden" -eq 0 ] && [ "$overgeslagen" -eq 0 ] && ok "elke skill in beide spiegels draagt een domein"
  return 0
}

if [ "${1:-}" = "--print-spiegel-uitzonderingen" ]; then
  spiegel_verschillen || {
    printf 'OVERGESLAGEN  diff kon een skillpaar niet vergelijken\n' >&2
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
check_aanwezigheid
check_naamcontract
check_scoping
check_pairing
check_herkomst
check_omvang
check_domein

if [ "$findings" -gt 0 ]; then
  printf '\n%d bevinding(en).\n' "$findings"
  exit 1
fi
exit 0
