#!/bin/bash
# PreToolUse hook: draai de unit-suite vóór elke `git commit`.
# Matcher: Bash. Blokkerend: exit 2 + stderr. Geslaagd: exit 0 + JSON op stdout.
#
# Tegenhanger (pairing-regel #624): CLAUDE.md, sectie Hooks, punt "Test-gate",
# draagt de norm vooraf; deze hook dwingt hem af op het commit-moment.
#
# De commitdetectie is die van main-commit-guard.sh (lib/git_invocations.py):
# een commit na `cd`, `;`, `|`, `&&` of met `git -C <pad>` wordt gezien, en de
# boom die gecommit wordt is de boom die getest wordt — niet de `.cwd` uit de
# payload en niet $CLAUDE_PROJECT_DIR. Alleen repo's van dit project (zelfde
# `--git-common-dir` als $CLAUDE_PROJECT_DIR: de hoofdcheckout plus elke
# worktree) worden getest; een andere repo, zoals de vault in ~/notes-git,
# wordt zichtbaar overgeslagen.
#
# NIET fail-open (paia-shell-gates): zonder tooling, bij een commando dat niet
# te parsen is, bij een pad dat niet te herleiden is, bij een map die nog niet
# bestaat en bij een git die stukloopt is er niet getest — en dat is geen
# groen licht. Die gevallen blokkeren luid met KON NIET CONTROLEREN.
#
# Bewuste asymmetrie met de guard: main-commit-guard.sh honoreert
# `.claude/allow-main` bij een onherleidbaar pad of een kapotte git; deze hook
# niet. `allow-main` staat een commit op main toe, geen commit zonder tests.
# Dezelfde parser geeft op dezelfde invoer dus bewust twee uitkomsten.
#
# Waarom een eigen deadline: een command-hook waarvan de timeout verloopt,
# blokkeert de tool-call niet ("don't count on a stalled hook to act as a
# gate", https://code.claude.com/docs/en/hooks.md) — een timeout is dus
# fail-open. De hook stopt daarom zelf: één deadline van 270 s voor het hele
# commando (lib/deadline.py, kill van de procesgroep, grace 10 s), ruim
# binnen de hook-timeout van 300 s in .claude/settings.json.
#
# Waarom slots (#1184): zonder rem draaien er tot zeven suites tegelijk, elk
# met `-n auto`, en dat sloopt de host. De gate neemt daarom vóór de lus één van
# twee slots (`flock` op slot-1 of slot-2 in een lockmap, via lib/gate_slot.py)
# en draait pytest met hoogstens 4 workers: nooit meer dan 2 x 4 tegelijk. Het
# slot staat open als fd 8 of 9 tot de hook eindigt, en `deadline.py` erft hem:
# wordt de hook gedood terwijl pytest loopt, dan blijft het slot bezet tot de
# deadline (+ grace) van `deadline.py`. De vaste nummers 8 en 9 zijn bewust:
# `exec {var}>>` is bash 4.1+ en macOS' /bin/bash is 3.2 — daar zou de hook op
# die regel sterven met een exit die geen 2 is, en de commit ging ongetest door.
# Het slot komt vóór de lus en geldt voor alle wortels: de deadline is ook één
# voor de hele lus, en het wachten telt daarin mee. Een commit in een ander
# project vraagt geen slot (er is niets te testen). Pytest krijgt een eigen
# `--basetemp`, dat de EXIT-trap opruimt: zo lekt een gedode suite geen /tmp.
#
# Waarom een rem (#1184): de slots begrenzen het aantal workers, niet het
# geheugen. Is `gates.slice` geïnstalleerd (MemoryHigh/MemoryMax), dan draait
# pytest binnen die slice: `systemd-run --user --scope --slice=gates.slice`.
# Een scope (geen service) voert het commando zelf uit: hetzelfde PID, dezelfde
# procesgroep, dus de groep-kill van `deadline.py` blijft werken. `choom -n 800`
# erbovenop, want een scope kan geen `OOMScoreAdjust` zetten: zonder hoge score
# kiest de kernel bij een tekort eerder de sessie dan de gate. De test of de
# rem er is, kijkt naar `FragmentPath` en `MemoryMax`, niet naar `LoadState`:
# systemd synthetiseert elke slice-naam en meldt dan `loaded`, ook zonder
# unitbestand, en `systemd-run --slice=` naar zo'n slice draait zonder plafond.
# Zonder rem (Mac, devcontainer) wordt er nog steeds getest, met de slots, en
# zegt de output waarom. De tellers in `gates.slice/memory.events` vóór en ná
# een run maken van een OOM-kill of een geheugendruk-deadline een eigen melding
# in plaats van "tests failed". Spec: docs/developer/planning/specs/2026-10-03-test-gate-rem-design.md.

HOOK_NAME="Test-gate"
UNRESOLVED="<onbekend>"
NEW_DIR="<nieuw>"
# Bewust niet verlaagd na #989: de deadline is een fail-closed noodrem, geen
# prestatiebudget — parallel draaien maakt de suite sneller, niet de deadline
# krapper. Zie docs/developer/testing.md, sectie Pre-commit test gate.
DEADLINE_SECONDS=270
# Wachtplafond voor een vrij slot, binnen de deadline: W = 270 - p95(run bij
# -n 4) - 20, gemeten in #1184 (Taak 0). PAIA_GATE_SLOT_WAIT: alleen voor tests.
SLOT_WAIT="${PAIA_GATE_SLOT_WAIT:-180}"

MISSING_TOOLS=""
command -v jq >/dev/null 2>&1 || MISSING_TOOLS="$MISSING_TOOLS jq"
command -v python3 >/dev/null 2>&1 || MISSING_TOOLS="$MISSING_TOOLS python3"
command -v git >/dev/null 2>&1 || MISSING_TOOLS="$MISSING_TOOLS git"
command -v cat >/dev/null 2>&1 || MISSING_TOOLS="$MISSING_TOOLS cat"
if [ -n "$MISSING_TOOLS" ]; then
  {
    echo "$HOOK_NAME: KON NIET CONTROLEREN — ontbrekende tooling:$MISSING_TOOLS"
    echo ""
    echo "Deze hook draait de unit-suite vóór elke commit."
    echo "Zonder die tooling is er niet getest; dat is geen groen licht."
  } >&2
  exit 2
fi

# Absoluut, want de testrun gebeurt vanuit de projectwortel.
HOOK_DIR=$(cd "$(dirname "$0")" && pwd)
LIB="$HOOK_DIR/lib"

INPUT=$(cat)
if ! CMD=$(printf '%s' "$INPUT" | jq -r '.tool_input.command // empty'); then
  {
    echo "$HOOK_NAME: KON NIET CONTROLEREN — de payload is geen geldige JSON."
    echo ""
    echo "Zonder het commando is er niet getest; dat is geen groen licht."
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
    echo "Er staat wél een 'git commit' in dit commando. 'Niet kunnen testen' is"
    echo "geen groen licht, dus dit gaat niet stil door."
  } >&2
  exit 2
}

# Stromen gescheiden houden: stderr van parser en git hoort niet in de
# bevindingen, die regel voor regel als velden gelezen worden.
ERR_FILE=$(mktemp 2>/dev/null) || ERR_FILE="${TMPDIR:-/tmp}/pre-commit-tests.$$"
# Eén trap: een tweede `trap … EXIT` verving de eerste. BT is het basetemp van
# pytest, leeg tot er een is.
BT=""
trap 'rm -f "$ERR_FILE"; [ -z "$BT" ] || rm -rf "$BT"' EXIT

FINDINGS=$(printf '%s' "$CMD" | python3 "$LIB/git_invocations.py" --report-new-dirs "$CWD" 2>"$ERR_FILE")
STATUS=$?

if [ "$STATUS" -eq 3 ]; then
  # Tweestapsregel van de guard, hier op `git` én `commit` (push is niet van
  # deze hook): staat dat er niet in de ruwe tekst, dan is er niets te testen
  # en legt één losse apostrof niets lam.
  case "$CMD" in
    *git*) ;;
    *) exit 0 ;;
  esac
  case "$CMD" in
    *commit*) ;;
    *) exit 0 ;;
  esac
  block "het commando is niet te parsen (open quote). Splits het commando op."
fi
if [ "$STATUS" -ne 0 ]; then
  {
    echo "$HOOK_NAME: KON NIET CONTROLEREN — de commandoparser faalde."
    echo ""
    cat "$ERR_FILE" 2>/dev/null
  } >&2
  exit 2
fi

COMMITS=""
while IFS=$'\t' read -r ACTION TARGET TO_MAIN RAW; do
  if [ "$ACTION" = "commit" ]; then
    COMMITS="$COMMITS$ACTION"$'\t'"$TARGET"$'\t'"$TO_MAIN"$'\t'"$RAW"$'\n'
  fi
done <<< "$FINDINGS"
if [ -z "$COMMITS" ]; then
  exit 0
fi

printf '%s' "$CMD" | python3 "$LIB/no_verify.py" 2>"$ERR_FILE"
STATUS=$?
if [ "$STATUS" -ne 0 ] && [ "$STATUS" -ne 2 ]; then
  {
    echo "$HOOK_NAME: KON NIET CONTROLEREN — de --no-verify-check faalde."
    echo ""
    cat "$ERR_FILE" 2>/dev/null
  } >&2
  exit 2
fi
if [ "$STATUS" -eq 2 ]; then
  echo "Commit-gate: --no-verify is niet toegestaan in deze repo." >&2
  echo "" >&2
  echo "De testgate is een Claude Code PreToolUse-hook, geen git-hook, dus de" >&2
  echo "vlag slaat vandaag niets over — maar zodra er git-hooks bijkomen wel." >&2
  echo "Faalt er iets: repareer dat. Moet het echt overgeslagen, vraag het." >&2
  exit 2
fi

# Per commit de repowortel. Unieke wortels, gescheiden door een newline.
ROOTS=""
while IFS=$'\t' read -r ACTION TARGET TO_MAIN RAW; do
  [ -n "$ACTION" ] || continue
  if [ "$TARGET" = "$UNRESOLVED" ]; then
    block "het pad '$RAW' is niet te herleiden. Schrijf het pad voluit."
  fi
  if [ "$TARGET" = "$NEW_DIR" ]; then
    block "'$RAW' bestaat nog niet. Commit in een aparte aanroep, nadat de map bestaat."
  fi

  ROOT=$(git -C "$TARGET" rev-parse --show-toplevel 2>"$ERR_FILE") || ROOT=""
  GIT_MESSAGE=$(cat "$ERR_FILE" 2>/dev/null)
  if [ -z "$ROOT" ]; then
    # '$TARGET is geen repo' is een echte uitkomst: niets te testen. Een git
    # die stukloopt (dubious ownership, kapotte installatie) is dat niet.
    case "$GIT_MESSAGE" in
      *"not a git repository"*|*"Not a git repository"*|*"No such file or directory"*|*"cannot change to"*)
        continue
        ;;
      *)
        block "git kon '$TARGET' niet uitlezen: ${GIT_MESSAGE:-geen melding}."
        ;;
    esac
  fi

  case $'\n'"$ROOTS" in
    *$'\n'"$ROOT"$'\n'*) ;;
    *) ROOTS="$ROOTS$ROOT"$'\n' ;;
  esac
done <<< "$COMMITS"

if [ -z "$ROOTS" ]; then
  exit 0
fi

# `--git-common-dir`, absoluut; zelfde foutcontract voor referentie en doel.
common_dir() {
  local dir="$1" out message
  out=$(git -C "$dir" rev-parse --path-format=absolute --git-common-dir 2>"$ERR_FILE") || out=""
  message=$(cat "$ERR_FILE" 2>/dev/null)
  if [ -z "$out" ]; then
    block "git kon de gedeelde git-map van '$dir' niet bepalen: ${message:-geen melding}."
  fi
  printf '%s' "$out"
}

REF_DIR="${CLAUDE_PROJECT_DIR:-$HOOK_DIR}"
# In een command substitution eindigt `block` alleen de subshell; de lege
# uitvoer vangt dat, en de melding staat dan al op stderr.
REF_COMMON=$(common_dir "$REF_DIR") || exit 2

MSG=""
add_message() {
  if [ -n "$MSG" ]; then
    MSG="$MSG"$'\n'"$1"
  else
    MSG="$1"
  fi
}

PROJECT_ROOTS=""
while IFS= read -r ROOT; do
  [ -n "$ROOT" ] || continue
  COMMON=$(common_dir "$ROOT") || exit 2
  if [ "$COMMON" = "$REF_COMMON" ]; then
    PROJECT_ROOTS="$PROJECT_ROOTS$ROOT"$'\n'
  else
    add_message "$HOOK_NAME: $ROOT overgeslagen — ander project"
  fi
done <<< "$ROOTS"

# additionalContext bereikt het model, systemMessage de gebruiker: een
# zwijgende gate mag niet ogen als een tevreden gate.
emit_message_and_exit() {
  if [ -n "$MSG" ]; then
    jq -cn --arg m "$MSG" '{systemMessage: $m, hookSpecificOutput: {hookEventName: "PreToolUse", additionalContext: $m}}'
  fi
  exit 0
}

# Geen project-wortel (alleen een ander project): niets te testen, dus ook geen
# slot en geen wachttijd.
if [ -z "$PROJECT_ROOTS" ]; then
  emit_message_and_exit
fi

# Eén `date`-aanroep, vóór het slot: de wachttijd telt mee in de deadline.
DEADLINE=$(( $(date +%s) + DEADLINE_SECONDS ))

slot_failed() {
  {
    echo "$HOOK_NAME: KON NIET CONTROLEREN — gate-slot niet te nemen: $1"
    echo ""
    echo "Zonder slot zou deze suite ongeremd naast de andere gates draaien."
    echo "Niet kunnen testen is geen groen licht."
  } >&2
  exit 2
}

if [ -n "${PAIA_GATE_SLOT_DIR:-}" ]; then
  SLOT_DIR="$PAIA_GATE_SLOT_DIR"
elif [ -d "/run/user/$UID" ]; then
  SLOT_DIR="/run/user/$UID/paia-gate-slots"
else
  SLOT_DIR="/tmp/paia-gate-slots-$UID"
fi
# SC2174: alleen de lockmap zelf moet 700 zijn; de ouder (/run/user/$UID of /tmp) bestaat al.
# shellcheck disable=SC2174
if ! MKDIR_OUT=$(mkdir -p -m 700 "$SLOT_DIR" 2>&1); then
  slot_failed "lockmap $SLOT_DIR niet aan te maken ($MKDIR_OUT)"
fi

# Een mislukte `exec`-redirect stopt een niet-interactieve bash niet (gemeten);
# de `if` ziet hem. Vaste fd's 8 en 9: zie de kop over bash 3.2.
if ! exec 8>>"$SLOT_DIR/slot-1" 9>>"$SLOT_DIR/slot-2"; then
  slot_failed "lockbestanden in $SLOT_DIR niet te openen"
fi

SLOT=$(python3 "$LIB/gate_slot.py" --fd 8 --fd 9 --wait "$SLOT_WAIT" 2>"$ERR_FILE")
SLOT_STATUS=$?
if [ "$SLOT_STATUS" -eq 75 ]; then
  {
    echo "$HOOK_NAME: KON NIET CONTROLEREN — beide gate-slots bezet na $SLOT_WAIT s"
    echo ""
    echo "Twee andere gates draaien al. Probeer de commit zo opnieuw."
    echo "Niet kunnen testen is geen groen licht."
  } >&2
  exit 2
fi
if [ "$SLOT_STATUS" -ne 0 ]; then
  slot_failed "gate_slot.py gaf exit $SLOT_STATUS: $(cat "$ERR_FILE" 2>/dev/null)"
fi
# Sluit de fd van het slot dat níet gepakt is; de andere blijft open tot de
# hook eindigt, en `deadline.py` erft hem.
case "$SLOT" in
  1) exec 9>&- ;;
  2) exec 8>&- ;;
  *) slot_failed "gate_slot.py gaf exit 0 zonder geldig slotnummer: '$SLOT'" ;;
esac

if ! BT=$(mktemp -d 2>"$ERR_FILE"); then
  BT=""
  {
    echo "$HOOK_NAME: KON NIET CONTROLEREN — geen basetemp: $(cat "$ERR_FILE" 2>/dev/null)"
    echo ""
    echo "Niet kunnen testen is geen groen licht."
  } >&2
  exit 2
fi

# VS Code-`claude` draait in de logind-scope zonder XDG_RUNTIME_DIR en soms
# zonder DBUS_SESSION_BUS_ADDRESS; zonder bus faalt `systemctl --user` en valt
# de rem stil uit op precies het pad waar hij nodig is.
if [ -z "${XDG_RUNTIME_DIR:-}" ] && [ -z "${DBUS_SESSION_BUS_ADDRESS:-}" ] \
  && [ -d "/run/user/$UID" ]; then
  export XDG_RUNTIME_DIR="/run/user/$UID"
fi

# Rem beschikbaar? De eerste treffer is de reden. `show` meldt KEY=VALUE-regels
# in de volgorde van systemd, niet die van de `-p`-argumenten.
REM_REASON=""
if ! command -v systemctl >/dev/null 2>&1; then
  REM_REASON="geen systemctl"
elif ! command -v systemd-run >/dev/null 2>&1; then
  REM_REASON="geen systemd-run"
elif ! command -v choom >/dev/null 2>&1; then
  REM_REASON="geen choom"
elif ! SHOW=$(systemctl --user show gates.slice -p FragmentPath -p MemoryMax 2>"$ERR_FILE"); then
  REM_REASON="systemctl --user faalde"
else
  FRAGMENT=""
  MEMORY_MAX=""
  while IFS='=' read -r KEY VALUE; do
    case "$KEY" in
      FragmentPath) FRAGMENT="$VALUE" ;;
      MemoryMax) MEMORY_MAX="$VALUE" ;;
    esac
  done <<< "$SHOW"
  if [ -z "$FRAGMENT" ]; then
    REM_REASON="gates.slice niet geïnstalleerd"
  elif [ -z "$MEMORY_MAX" ] || [ "$MEMORY_MAX" = "infinity" ]; then
    REM_REASON="gates.slice zonder MemoryMax"
  fi
fi

WRAP=()
if [ -n "$REM_REASON" ]; then
  add_message "$HOOK_NAME: rem niet beschikbaar: $REM_REASON"
else
  WRAP=(systemd-run --user --scope --slice=gates.slice --quiet -- choom -n 800 --)
fi

# De tellers van gates.slice. PAIA_GATE_CGROUP_ROOT: alleen voor tests.
EVENTS="${PAIA_GATE_CGROUP_ROOT:-/sys/fs/cgroup/user.slice/user-$UID.slice/user@$UID.service}/gates.slice/memory.events"

# Waarde van sleutel $1 in memory.events; 0 als het bestand of de sleutel ontbreekt.
event_count() {
  local key value
  if [ -r "$EVENTS" ]; then
    while read -r key value; do
      if [ "$key" = "$1" ]; then
        case "$value" in
          ''|*[!0-9]*) ;;
          *) printf '%s' "$value"; return 0 ;;
        esac
      fi
    done < "$EVENTS"
  fi
  printf '0'
}

# Ná min vóór; een negatieve delta (slice opnieuw aangemaakt) telt als geen delta.
delta() {
  local d=$(( $2 - $1 ))
  [ "$d" -gt 0 ] || d=0
  printf '%s' "$d"
}

while IFS= read -r ROOT; do
  [ -n "$ROOT" ] || continue
  OOM_DELTA=0
  HIGH_DELTA=0
  if [ -z "$REM_REASON" ]; then
    OOM_BEFORE=$(event_count oom_kill)
    HIGH_BEFORE=$(event_count high)
  fi
  TEST_OUTPUT=$(cd "$ROOT" && python3 "$LIB/deadline.py" "$DEADLINE" -- \
    ${WRAP[@]+"${WRAP[@]}"} \
    uv run pytest tests/unit/ -m 'not hardware_audio and not slow' --tb=short -q \
    -n auto --maxprocesses 4 --basetemp="$BT" 2>&1)
  STATUS=$?
  if [ -z "$REM_REASON" ]; then
    OOM_DELTA=$(delta "$OOM_BEFORE" "$(event_count oom_kill)")
    HIGH_DELTA=$(delta "$HIGH_BEFORE" "$(event_count high)")
  fi
  # De rijen gelden van boven naar beneden, de eerste treffer wint (spec §4).
  # Een groene run blijft groen, ook bij een delta van een buurgate.
  if [ "$STATUS" -eq 0 ]; then
    :
  elif [ "$OOM_DELTA" -gt 0 ]; then
    {
      echo "$HOOK_NAME: KON NIET CONTROLEREN — gate afgeremd (oom-kill in gates.slice tijdens deze run)."
      echo ""
      echo "Wortel: $ROOT. Niet kunnen testen is geen groen licht."
      echo ""
      echo "$TEST_OUTPUT"
    } >&2
    exit 2
  elif [ -z "$REM_REASON" ] && {
    [[ "$TEST_OUTPUT" == *"Failed to start transient scope unit"* ]] \
      || [[ "$TEST_OUTPUT" == *"Failed to connect to bus"* ]] \
      || [[ "$TEST_OUTPUT" == *"Failed to create bus connection"* ]]
  }; then
    {
      echo "$HOOK_NAME: KON NIET CONTROLEREN — systemd-run startte niet."
      echo ""
      echo "Wortel: $ROOT. Niet kunnen testen is geen groen licht."
      echo ""
      echo "$TEST_OUTPUT"
    } >&2
    exit 2
  fi
  if [ "$STATUS" -eq 124 ]; then
    {
      echo "$HOOK_NAME: KON NIET CONTROLEREN — testsuite liep over de deadline van $DEADLINE_SECONDS s."
      if [ "$HIGH_DELTA" -gt 0 ]; then
        echo "gates.slice stond onder geheugendruk"
      fi
      echo ""
      echo "Wortel: $ROOT. Een afgebroken suite is geen groen licht."
      echo ""
      echo "$TEST_OUTPUT"
    } >&2
    exit 2
  fi
  if [ "$STATUS" -eq 127 ]; then
    # lib/deadline.py kon het commando niet starten (meestal: geen `uv` op
    # PATH). Dan is er niet getest — iets anders dan "tests falen".
    {
      echo "$HOOK_NAME: KON NIET CONTROLEREN — de testsuite kon niet starten (uv niet gevonden?)."
      echo ""
      echo "Wortel: $ROOT. Niet kunnen testen is geen groen licht."
      echo ""
      echo "$TEST_OUTPUT"
    } >&2
    exit 2
  fi
  if [ "$STATUS" -ge 128 ]; then
    # Een SIGKILL op pytest onder uv geeft 137, op uv via deadline.py 247
    # (sys.exit(-9)): n = exit - 128, of 256 - exit boven de 192.
    if [ "$STATUS" -le 192 ]; then
      SIGNAL_NR=$(( STATUS - 128 ))
    else
      SIGNAL_NR=$(( 256 - STATUS ))
    fi
    # Zonder rem is er geen gates.slice en dus geen systemd-oomd daarin.
    if [ -n "$REM_REASON" ]; then
      KILL_HINT="rem niet beschikbaar: $REM_REASON"
    else
      KILL_HINT="systemd-oomd in gates.slice?"
    fi
    {
      echo "$HOOK_NAME: KON NIET CONTROLEREN — gate gedood door signaal $SIGNAL_NR ($KILL_HINT)."
      echo ""
      echo "Wortel: $ROOT. Niet kunnen testen is geen groen licht."
      echo ""
      echo "$TEST_OUTPUT"
    } >&2
    exit 2
  fi
  case "$STATUS" in
    3|4|5)
      # pytest 3 (interne fout), 4 (gebruiksfout), 5 (niets verzameld): er is
      # niets getest. Blokkeren, maar niet als "tests falen" — dat stuurt de
      # agent op zoek naar een kapotte test die er niet is.
      {
        echo "$HOOK_NAME: KON NIET CONTROLEREN — pytest stopte met exitcode $STATUS zonder de tests te draaien."
        echo ""
        echo "Wortel: $ROOT. Niet kunnen testen is geen groen licht."
        case "$TEST_OUTPUT" in
          *"unrecognized arguments: -n"*)
            echo ""
            echo "pytest-xdist ontbreekt in deze boom (\`-n\` onbekend): rebase de branch op origin/main."
            ;;
        esac
        echo ""
        echo "$TEST_OUTPUT"
      } >&2
      exit 2
      ;;
  esac
  if [ "$STATUS" -ne 0 ]; then
    {
      echo "Pre-commit test gate: tests failed. Fix failing tests before committing."
      echo ""
      echo "$TEST_OUTPUT"
    } >&2
    exit 2
  fi
  LAST=""
  while IFS= read -r LINE; do
    [ -n "${LINE//[[:space:]]/}" ] && LAST="$LINE"
  done <<< "$TEST_OUTPUT"
  add_message "$HOOK_NAME: $ROOT — $LAST"
done <<< "$PROJECT_ROOTS"

emit_message_and_exit
