#!/usr/bin/env bash
# Integration test voor de kwaliteitsratel (#562, #623, #806).
#
# Wat hier gepind wordt, en waarom:
#   - Groen op een no-op. Een gate die op zijn eigen repo rood staat wordt
#     binnen een week uitgezet; dit is de test die dat onmogelijk maakt.
#   - Rood op een ingevoegde `except Exception: pass` (S110 stijgt) en op een
#     kale `except:` (E722, nulregel). Dat is #562 AC 6 letterlijk.
#   - Rood ook mét een `# noqa` voor precies die regel. Zonder dit geval is
#     --ignore-noqa een bewering in het ontwerp en geen eigenschap.
#   - Rood op een functie met cognitief 16-25: de teller `cognitive_over_15` uit
#     [complexipy] stijgt (#1192), en een run laat geen complexipy-rommel achter.
#   - Rood op een functie boven cyclomatisch 20 of cognitief 25, óók met een
#     `# noqa: C901` resp. `# noqa: complexipy` achter de `def`: de twee harde
#     grenzen kennen geen baseline en geen ontsnapping (#1192). Ook niet via een
#     complexipy-exclude in pyproject.toml of (.)complexipy.toml, en niet via
#     een hogere `max-complexity` onder [tool.ruff.lint.mccabe].
#   - Determinisme: twee runs op dezelfde boom geven hetzelfde getal (#562 AC 2).
#   - De provenance-tabel uit §6.1: meerdere refs, local_sha != HEAD, vuile
#     working tree, untracked .py, ref-deletie.
#   - Dat pre-push géén suite meer draait (#806). Een gate die de volle suite mét
#     coverage draait was een exact duplicaat van de CI `test`-job op dezelfde
#     machine; twee gelijktijdige pushes legden de Beelink om.
set -u

REPO_ROOT="$(git rev-parse --show-toplevel)"
RATCHET_DIR="$REPO_ROOT/scripts/quality"
HOOK="$REPO_ROOT/scripts/hooks/pre-push-quality.sh"
ZERO="0000000000000000000000000000000000000000"
REMOTE_URL="https://example.invalid/repo.git"

if ! command -v uv >/dev/null 2>&1; then
  if [ -n "${CI:-}" ]; then
    echo "FAIL: uv not on PATH; CI must install it before running this test"
    exit 1
  fi
  echo "SKIP: uv not on PATH (local run)"
  exit 0
fi

failures=0
OUT=""

check() { # name want(zero|nonzero) actual_rc
  local name="$1" want="$2" rc="$3" ok=0
  case "$want" in
    zero) [ "$rc" -eq 0 ] && ok=1 ;;
    nonzero) [ "$rc" -ne 0 ] && ok=1 ;;
  esac
  if [ "$ok" -eq 1 ]; then
    echo "PASS  $name"
  else
    echo "FAIL  $name (rc=$rc, wanted $want)"
    echo "--- output ---"; echo "$OUT"; echo "--------------"
    failures=$((failures + 1))
  fi
}

# check() alleen zegt "nonzero rc" — dat is ook waar als het script crasht
# (rc=127, command not found). check_contains bindt een PASS aan een marker die
# alleen de echte bevinding produceert.
check_contains() { # name marker
  case "$OUT" in
    *"$2"*) echo "PASS  $1" ;;
    *)
      echo "FAIL  $1 (output did not mention '$2')"
      echo "--- output ---"; echo "$OUT"; echo "--------------"
      failures=$((failures + 1))
      ;;
  esac
}

# Spiegelbeeld van check_contains, voor een arm die er niet meer hoort te zijn.
# Alleen bruikbaar NAAST een positieve check (zie §7): de afwezigheid van een
# signaal is op zichzelf geen bewijs — het script kan ook gecrasht zijn vóór het
# punt waar de marker geproduceerd zou worden.
check_not_contains() { # name marker
  case "$OUT" in
    *"$2"*)
      echo "FAIL  $1 (output mentioned '$2')"
      echo "--- output ---"; echo "$OUT"; echo "--------------"
      failures=$((failures + 1))
      ;;
    *) echo "PASS  $1" ;;
  esac
}

# --- fixture ------------------------------------------------------------------
# Een miniatuurrepo met dezelfde vorm als deze: .ruff-version, .complexipy-version, een baseline, een
# quality-pakket, src/ en tests/. De baseline staat op de stand van de fixture,
# zodat een INGEVOEGDE overtreding de enige stijging is.
make_fixture() { # -> print het pad
  local dir
  dir="$(mktemp -d)"
  mkdir -p "$dir/scripts/quality" "$dir/scripts/hooks" "$dir/src" "$dir/tests"
  cp "$RATCHET_DIR"/*.py "$dir/scripts/quality/"
  cp "$HOOK" "$dir/scripts/hooks/"
  cp "$REPO_ROOT/.ruff-version" "$dir/.ruff-version"
  cp "$REPO_ROOT/.complexipy-version" "$dir/.complexipy-version"

  cat > "$dir/pyproject.toml" <<'TOML'
[project]
name = "ratchet-fixture"
version = "0"
requires-python = ">=3.12"

[tool.ruff.lint]
extend-select = ["E722", "B006", "F403", "F811", "B023", "PT012", "PGH003", "PGH004",
                 "C901", "TRY400", "F401", "F841", "S110", "S112", "PT011"]

[tool.ruff.lint.flake8-pytest-style]
raises-require-match-for = ["Exception", "BaseException"]
TOML

  cat > "$dir/quality-baseline.toml" <<'TOML'
[ruff]
C901   = 0
TRY400 = 0
F401   = 0
F841   = 0
S110   = 0
PT011  = 0
S112   = 0

[complexipy]
cognitive_over_15 = 0

[coverage]
min_diff_pct = 70
min_total_pct = 0
TOML

  cat > "$dir/src/app.py" <<'PY'
"""Schone module: geen enkele van de vijftien regels raakt hem."""


def add(a: int, b: int) -> int:
    return a + b
PY
  cat > "$dir/tests/test_app.py" <<'PY'
from src.app import add


def test_add() -> None:
    assert add(1, 2) == 3
PY

  git -C "$dir" init -q -b main
  git -C "$dir" config user.email test@example.com
  git -C "$dir" config user.name Test
  git -C "$dir" config commit.gpgsign false
  git -C "$dir" add -A
  git -C "$dir" commit -qm "schone fixture"
  printf '%s' "$dir"
}

# Een run schrijft zijn complexipy-uitvoer en -cache naar een tijdelijke map
# (Review Focus 3). Naast een positieve check van de run zelf: zonder dat de
# ratel echt liep, zegt een lege status niets.
check_no_litter() { # dir
  local litter
  litter="$(git -C "$1" status --porcelain --ignored | grep complexipy || true)"
  if [ -z "$litter" ]; then
    echo "PASS  de run laat geen complexipy-rommel achter in de werkboom"
  else
    echo "FAIL  de run laat complexipy-rommel achter in de werkboom"
    echo "$litter"; failures=$((failures + 1))
  fi
}

run_ratchet() { # dir -> zet OUT, geeft rc terug; toetst ook de werkboom
  OUT="$( cd "$1" && python3 scripts/quality/ratchet.py ruff 2>&1 )"
  local rc=$?
  check_no_litter "$1"
  return "$rc"
}

# --- 1. groen op een schone boom ----------------------------------------------
FIX="$(make_fixture)"
run_ratchet "$FIX"; check "een schone boom is groen" zero $?
check_contains "de groene regel noemt ook complexipy" "complexipy ok (0 functies cognitief > 15)"
if [ -e "$FIX/complexipy-results.json" ] || [ -e "$FIX/.complexipy_cache" ]; then
  echo "FAIL  de ratel laat complexipy-rommel achter in de werkboom"
  ls -a "$FIX"; failures=$((failures + 1))
else
  echo "PASS  de ratel laat geen complexipy-resultaten of -cache in de werkboom"
fi

# --- 2. determinisme (#562 AC 2) ----------------------------------------------
run_ratchet "$FIX"; FIRST="$OUT"
run_ratchet "$FIX"; SECOND="$OUT"
if [ "$FIRST" = "$SECOND" ]; then
  echo "PASS  twee runs op dezelfde boom geven hetzelfde getal"
else
  echo "FAIL  twee runs geven verschillende uitvoer"; failures=$((failures + 1))
fi
rm -rf "$FIX"

# --- 3. een ingevoegde `except Exception: pass` laat S110 stijgen --------------
FIX="$(make_fixture)"
cat >> "$FIX/src/app.py" <<'PY'


def swallow() -> None:
    try:
        add(1, 2)
    except Exception:
        pass
PY
run_ratchet "$FIX"; check "een nieuwe try-except-pass is rood" nonzero $?
check_contains "de melding noemt S110 en de stijging" "S110: 0 -> 1"
rm -rf "$FIX"

# --- 4. een kale `except:` is een nulregel-hit ---------------------------------
# Let op: `except:\n    raise` (kale except die meteen opnieuw opgooit) telt bij
# ruff 0.16.4 NIET als E722 — ruff behandelt dat patroon als een no-op
# (TRY203, niet in onze set) en zwijgt over E722 juist dán. Geverifieerd los van
# deze fixture: `except: pass` triggert E722 wél, `except: raise` nooit, met en
# zonder omringende functie. `pass` in de body is dus nodig om de bedoelde
# nulregel-hit daadwerkelijk te produceren.
FIX="$(make_fixture)"
cat >> "$FIX/src/app.py" <<'PY'


def bare() -> None:
    try:
        add(1, 2)
    except:
        pass
PY
run_ratchet "$FIX"; check "een kale except is rood" nonzero $?
check_contains "de melding noemt E722 als nulregel" "E722"
check_contains "een nulregel biedt geen bump" "geen bump"
rm -rf "$FIX"

# --- 5. onderdrukkingsbestendigheid: # noqa helpt niet ------------------------
FIX="$(make_fixture)"
cat >> "$FIX/src/app.py" <<'PY'


def swallow() -> None:
    try:
        add(1, 2)
    except Exception:  # noqa: S110
        pass
PY
run_ratchet "$FIX"; check "een # noqa op de overtreding houdt de gate rood" nonzero $?
check_contains "de noqa verlaagt de teller niet" "S110: 0 -> 1"
rm -rf "$FIX"

# --- 5b. een functie met cognitief 16-25 laat de teller stijgen (#1192) ---------
# Zes geneste if-niveaus meten cognitief 21 (vijf geven 15, niet > 15). Het is de
# teller en niet een harde grens: de functie blijft onder de 25.
FIX="$(make_fixture)"
cat >> "$FIX/src/app.py" <<'PY'


def nested(a: int, b: int, c: int, d: int, e: int, f: int) -> int:
    if a:
        if b:
            if c:
                if d:
                    if e:
                        if f:
                            return 1
    return 0
PY
run_ratchet "$FIX"; check "een functie met cognitief 21 is rood" nonzero $?
check_contains "de melding noemt de stijgende teller" "cognitive_over_15: 0 -> 1"
check_contains "de melding noemt de functie" "src/app.py::nested"
rm -rf "$FIX"

# --- 5c. de twee harde grenzen: een noqa helpt niet (#1192) ---------------------
# 19 elif-takken plus de if zijn 20 beslissende takken: ruff 0.16.4 telt dan 21.
# Meetbaar vooraf met `uvx ruff@0.16.4 check <bestand> --select C901 --config
# lint.mccabe.max-complexity=20 --ignore-noqa`. Geen baseline-bump mogelijk.
FIX="$(make_fixture)"
{
  printf '\n\ndef chain(x: int) -> int:  # noqa: C901\n'
  printf '    if x == 0:\n        return 0\n'
  for i in $(seq 1 19); do printf '    elif x == %s:\n        return %s\n' "$i" "$i"; done
  printf '    return -1\n'
} >> "$FIX/src/app.py"
run_ratchet "$FIX"; check "een functie boven cyclomatisch 20 met # noqa: C901 is rood" nonzero $?
check_contains "de melding noemt de cyclomatische score en de grens" "cyclomatisch 21 > 20"
check_contains "de harde grens biedt geen bump" "geen bump"
rm -rf "$FIX"

# Dezelfde keten, nu met max-complexity = 25 in pyproject.toml: ruff meldt C901
# alleen boven die drempel, dus zonder de --config-pin van de ratel verdween 21.
# De tellers staan op 1, zodat alleen de harde grens dit rood kan maken.
FIX="$(make_fixture)"
printf '\n[tool.ruff.lint.mccabe]\nmax-complexity = 25\n' >> "$FIX/pyproject.toml"
sed -i -e 's/^C901   = 0/C901   = 1/' -e 's/^cognitive_over_15 = 0/cognitive_over_15 = 1/' \
  "$FIX/quality-baseline.toml"
git -C "$FIX" commit -qam "mccabe-drempel 25, tellers op 1"
{
  printf '\n\ndef chain(x: int) -> int:\n'
  printf '    if x == 0:\n        return 0\n'
  for i in $(seq 1 19); do printf '    elif x == %s:\n        return %s\n' "$i" "$i"; done
  printf '    return -1\n'
} >> "$FIX/src/app.py"
run_ratchet "$FIX"; check "een hogere mccabe-drempel in pyproject houdt cyclomatisch 21 rood" nonzero $?
check_contains "de melding noemt de cyclomatische score ondanks de drempel" "cyclomatisch 21 > 20"
rm -rf "$FIX"

# Zeven geneste if-niveaus meten cognitief 28, de bovengrens is 25.
FIX="$(make_fixture)"
cat >> "$FIX/src/app.py" <<'PY'


def nested(a: int, b: int, c: int, d: int, e: int, f: int, g: int) -> int:  # noqa: complexipy
    if a:
        if b:
            if c:
                if d:
                    if e:
                        if f:
                            if g:
                                return 1
    return 0
PY
run_ratchet "$FIX"; check "een functie boven cognitief 25 met # noqa: complexipy is rood" nonzero $?
check_contains "de melding noemt de cognitieve score en de grens" "cognitief 28 > 25"
check_contains "de melding noemt de functie" "src/app.py::nested"
rm -rf "$FIX"

# complexipy leest exclude-config uit zijn cwd, en --no-ignore stopt dat niet.
# Een exclude in pyproject.toml of een (.)complexipy.toml over precies het
# overtredende bestand mag de functie niet uit de harde grens halen (#1192).
# Zonder die config is deze fixture identiek aan het geval hierboven.
FIX="$(make_fixture)"
printf '\n[tool.complexipy]\nexclude = ["app.py", "src/app.py"]\n' >> "$FIX/pyproject.toml"
printf 'exclude = ["app.py", "src/app.py"]\n' > "$FIX/complexipy.toml"
cp "$FIX/complexipy.toml" "$FIX/.complexipy.toml"
git -C "$FIX" add -A && git -C "$FIX" commit -qm "complexipy-exclude"
cat >> "$FIX/src/app.py" <<'PY'


def nested(a: int, b: int, c: int, d: int, e: int, f: int, g: int) -> int:
    if a:
        if b:
            if c:
                if d:
                    if e:
                        if f:
                            if g:
                                return 1
    return 0
PY
run_ratchet "$FIX"; check "een complexipy-exclude over het bestand houdt cognitief 28 rood" nonzero $?
check_contains "de geëxcludeerde functie staat toch in de melding" "cognitief 28 > 25"
rm -rf "$FIX"

# --- 6. provenance (§6.1) ------------------------------------------------------
run_hook() { # dir stdin -> zet OUT, geeft rc terug
  OUT="$( printf '%s' "$2" | ( cd "$1" && bash scripts/hooks/pre-push-quality.sh origin "$REMOTE_URL" ) 2>&1 )"
}

FIX="$(make_fixture)"
SHA="$(git -C "$FIX" rev-parse HEAD)"

run_hook "$FIX" "refs/heads/main $SHA refs/heads/main $ZERO
"
check "de normale vorm (één ref, sha == HEAD, schone boom) is groen" zero $?

run_hook "$FIX" "refs/heads/main $SHA refs/heads/main $ZERO
refs/heads/other $SHA refs/heads/other $ZERO
"
check "meer dan één non-deletie-ref is rood" nonzero $?
check_contains "de melding zegt waarom meerdere refs niet gaan" "meer dan één ref"

run_hook "$FIX" "refs/heads/main $ZERO refs/heads/main $SHA
"
check "een ref-deletie wordt overgeslagen" zero $?

OTHER="$(git -C "$FIX" rev-parse HEAD~0)"
git -C "$FIX" commit -q --allow-empty -m "tweede commit"
run_hook "$FIX" "refs/heads/main $OTHER refs/heads/main $ZERO
"
check "een gepushte sha die niet HEAD is, is rood" nonzero $?
check_contains "de melding noemt beide sha's" "HEAD is"

git -C "$FIX" reset -q --hard HEAD~1
echo "# vuil" >> "$FIX/src/app.py"
run_hook "$FIX" "refs/heads/main $SHA refs/heads/main $ZERO
"
check "een working tree die in src/ afwijkt is rood" nonzero $?
check_contains "de melding noemt de afwijkende working tree" "wijkt af van"
git -C "$FIX" checkout -q -- src/app.py

# Bevinding 4: quality-baseline.toml bepaalt het oordeel, niet alleen src/ en
# tests/. Een ongecommite bump zou de ruff-arm tegen opgehoogde getallen laten
# meten terwijl de gepushte boom die bump niet draagt — provenance moet dat
# ook op dit bestand toetsen, niet alleen op de gemeten code.
echo "" >> "$FIX/quality-baseline.toml"
run_hook "$FIX" "refs/heads/main $SHA refs/heads/main $ZERO
"
check "een working tree die in quality-baseline.toml afwijkt is rood" nonzero $?
check_contains "de melding noemt quality-baseline.toml" "quality-baseline.toml"
git -C "$FIX" checkout -q -- quality-baseline.toml

touch "$FIX/rommel.txt"
run_hook "$FIX" "refs/heads/main $SHA refs/heads/main $ZERO
"
check "ongerelateerde rommel buiten src/ en tests/ blokkeert niet" zero $?
rm -f "$FIX/rommel.txt"

touch "$FIX/src/nieuw.py"
run_hook "$FIX" "refs/heads/main $SHA refs/heads/main $ZERO
"
check "een untracked .py onder src/ is rood" nonzero $?
check_contains "de melding noemt het bestand" "src/nieuw.py"
rm -f "$FIX/src/nieuw.py"

# Lege stdin blijft rood — groen zou hier "niets te keuren" betekenen terwijl er
# van alles de deur uit kan gaan. De melding noemt sinds die dag wél de oorzaak
# die het vaakst klopt: git laat refs die hij zelf al afwees (non-fast-forward,
# of niets te pushen) weg uit wat de hook op stdin krijgt. In de normale keten
# vangt de dispatcher dit al af; deze aanroep gaat er rechtstreeks langs.
run_hook "$FIX" ""
check "lege stdin is rood" nonzero $?
check_contains "de melding noemt nul ref-regels" "nul ref-regels op stdin"
check_contains "de melding noemt git's eigen afwijzing als oorzaak" "git elke ref zelf al af"
rm -rf "$FIX"

# --- 7. pre-push draait geen suite meer (#806) --------------------------------
# Hier hing tot #806 een coverage-arm: bij een src/- of tests/-wijziging draaide
# de volle unit-suite mét coverage, ~3 minuten, het zwaarste dat de Beelink op
# geheugen kent. Dat was een exact duplicaat van de CI `test`-job op dezelfde
# machine (één self-hosted runner), en twee gelijktijdige pushes legden de host
# om. De arm is eruit; CI beoordeelt de dekking op de PR. Wat hier gepind wordt
# is dat hij niet stilletjes terugkomt.
#
# Het bewijs is positief, niet "de marker ontbreekt": deze fixture heeft geen
# origin/main én geen tests/unit/-map, dus een teruggekeerde arm valt over wat
# van die twee hij het eerst raakt — beide gegarandeerd rood. Wat de conclusie
# draagt is de rc: die kan alleen 0 zijn omdat `ratchet.py ruff` het laatste
# commando is. check_not_contains eronder zegt alleen wélke arm het was als het
# toch misgaat; hij draagt de conclusie niet alleen.
FIX="$(make_fixture)"
echo "# losse notitie" > "$FIX/NOTES.md"
git -C "$FIX" add NOTES.md
git -C "$FIX" commit -qm "alleen een markdownbestand"
SHA="$(git -C "$FIX" rev-parse HEAD)"
run_hook "$FIX" "refs/heads/main $SHA refs/heads/main $ZERO
"
check "een push die src/ noch tests/ raakt is groen" zero $?

cat >> "$FIX/src/app.py" <<'PY'


def touched() -> int:
    return 1
PY
git -C "$FIX" add src/app.py
git -C "$FIX" commit -qm "raakt src/"
SHA="$(git -C "$FIX" rev-parse HEAD)"
run_hook "$FIX" "refs/heads/main $SHA refs/heads/main $ZERO
"
check "een push die src/ WEL raakt is óók groen — er draait geen suite meer" zero $?
check_not_contains "en de suite is aantoonbaar niet gestart" "de suite faalt"
rm -rf "$FIX"

# --- 8. de gate heeft geen basis-ref meer nodig -------------------------------
# De merge-base tegen origin/main bestond alleen om de coverage-diff te kunnen
# bepalen. Zonder die arm mag een repo die origin/main helemaal niet kent gewoon
# groen pushen — vóór #806 was dat rood met "geen merge-base met origin/main".
# Deze fixture zet dat ref bewust niet (net als §7 hierboven). Dit geval is
# verder identiek aan het tweede geval van §7; zijn enige eigen waarde is de
# string-pin op "geen merge-base" — bewust geen dubbeling om weg te gooien.
FIX="$(make_fixture)"
cat >> "$FIX/src/app.py" <<'PY'


def also_touched() -> int:
    return 2
PY
git -C "$FIX" add src/app.py
git -C "$FIX" commit -qm "raakt src/, zonder origin/main"
SHA="$(git -C "$FIX" rev-parse HEAD)"
run_hook "$FIX" "refs/heads/main $SHA refs/heads/main $ZERO
"
check "een src/-push zonder origin/main is groen" zero $?
check_not_contains "en noemt geen merge-base meer" "geen merge-base"
rm -rf "$FIX"

if [ "$failures" -eq 0 ]; then
  echo "OK"; exit 0
fi
echo "FAIL ($failures failing check(s))"; exit 1
