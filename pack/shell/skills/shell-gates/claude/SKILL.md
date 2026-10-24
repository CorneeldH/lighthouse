---
name: paia-shell-gates
description: Use when writing or changing shell that guards something — hooks in .claude/hooks/ or scripts/hooks/, pre-push/CI gate scripts, and the integration tests that assert on them. Carries the norm "de afwezigheid van een signaal is geen negatief resultaat" plus a KPI table with commands that run in this repo. LET OP — gaat het om gewone Python of pytest, dan is dit niet de skill; gaat het om het opzetten van een nieuwe workflow-YAML, gebruik dan scripts/hooks/workflow-audit.sh.
metadata:
  binding: hard
  origin: own
  domain: factory
---

# Shell die iets bewaakt

## Domein

Shell waarvan een ander mens of een andere agent de exitcode gelooft: de hooks in
`.claude/hooks/`, de gate-scripts in `scripts/hooks/`, en de integratietests in
`tests/integration/` die daarop asserteren. Gewone hulpscripts in `scripts/` vallen er
half onder — daar geldt alleen de warning-lat, zie KPI-rij 2.

Buiten scope: Python (`ruff`/`ty` hebben hun eigen hooks), workflow-YAML
(`scripts/hooks/workflow-audit.sh`).

## De norm

**De afwezigheid van een signaal wordt behandeld als een negatief resultaat.**

"Niets gevonden" en "kon niet kijken" komen er allebei uit als groen. Dat is één
foutklasse, en in #609/#610 leverde hij zes defecten op die géén van de bestaande
controles achteraf ving — pytest groen, gate-test groen, code zag er goed uit.

| # | Wat er stond | Wat het deed |
|---|---|---|
| 1 | `check "... is blocked" nonzero "$?"` | `PASS` terwijl het script niet bestond (rc=127 is ook nonzero) |
| 2 | `die()` binnen `$( )` | sha256-mismatch printte de fout en exitte alsnog **0**; liet `-shellcheck ""` achter, wat actionlint leest als "integratie uit" |
| 3 | hook-shim met `$(git rev-parse --show-toplevel)/scripts/...` | `.git/` is gedeeld over worktrees; vanaf elke andere branch rc=127 |
| 4 | `git diff ... 2>/dev/null \| grep -q` | falende diff → lege output → "geen wijziging" → push ongeaudit door |
| 5 | testfixture zonder `origin` | de check "brand-new branch" raakte nooit het merge-base-pad dat hij toetste |
| 6 | `--online` zonder token | logde `(online)`, draaide stil offline, rc=0 |

Vier van de zes zaten in het **plan**, niet in de uitvoering. Een review-lane achteraf is
dus niet de plek waar dit opgelost hoort te worden; deze skill is de plek vooraf, de
shellcheck-hook de plek erna.

**Drie uitkomsten, niet twee.** Elk gate-script hier geeft terug: 0 = gecontroleerd en
schoon, 1 = gecontroleerd en bevindingen, 4 = **niet gecontroleerd**. Die derde is een
geldig antwoord en mag nooit als geslaagd gepresenteerd worden.

## KPIs

Elke rij is op deze repo gedraaid; de kolom "gemeten" is de uitkomst op
2026-08-23, worktree `issue-650-shellcheck-gate`.

| Norm | Meetcommando | Doel | Gemeten |
|---|---|---|---|
| Gate-shell is schoon tot en met `style` | `bash scripts/hooks/shellcheck-repo.sh` | rc 0 | rc 0 (15 bestanden) |
| Alle shell in de repo is schoon op `error`+`warning` | `git ls-files --cached --others --exclude-standard '*.sh' \| xargs bash scripts/hooks/shellcheck-repo.sh --severity warning --` | rc 0 | rc 0 (63 bestanden) |
| De gate stond één keer rood om de juiste reden | `uv run pytest tests/unit/test_quality_hooks.py -k "shellcheck or gate_assert or registered or gitignored"` | alles groen | 13 passed |
| Geen gate-shell valt buiten git | `git check-ignore $(git ls-files --cached --others --exclude-standard -- 'scripts/hooks/*.sh' 'tests/integration/*.sh' '.claude/hooks/*.sh')` | rc 1 | rc 1 |

De derde rij is de belangrijkste en de minst vanzelfsprekende: hij bevat
`test_shellcheck_hook_blocks_a_deliberately_broken_script` en
`test_shellcheck_hook_fails_loud_when_the_tool_is_missing`. Beide zijn met een mutatie
getoetst — mechanisme slopen, test moet vallen, herstellen. Een test die groen blijft na
die mutatie meet niets.

### Twee rijen die zijn geschrapt

Bewust vermeld, want een KPI-tabel die doet alsof ze de hele foutklasse dekt, is zelf een
instantie van die foutklasse.

- **"Geen ongemotiveerde foutonderdrukking: 0 treffers op `2>/dev/null`."** Gemeten: 36
  treffers in de drie gate-mappen, 25 na het wegfilteren van `|| true`- en
  `command -v`-vormen. Vrijwel allemaal onschuldig (versieprobes waarbij de `grep`
  erachter de eigenlijke assertie is, globs). De gevaarlijke variant — defect 4 — is met
  grep niet van de onschuldige te onderscheiden; het verschil is semantisch: *neemt een
  lege uitkomst de toegeeflijke tak?* Geschrapt als KPI, staat hieronder als
  non-negotiable.
- **"Elke blokkerende assertie toetst óók de uitvoer."** Gemeten: 12 kale
  `check ... nonzero`-regels in `tests/integration/`, waarvan 3 terecht (een negatieve
  `grep` waar er geen uitvoer ís). Te ruisgevoelig. In plaats van de KPI is de oorzaak
  mechanisch weggenomen: `tests/integration/lib/gate-assert.sh` telt rc 126 en 127 niet
  meer als bevinding, dus defect 1 kan niet meer optreden. Dat is sterker dan een teller.

## Non-negotiable

Deze drie zijn niet greppable. Ze staan hier los van de KPI-tabel omdat er geen
meetcommando voor bestaat — niet omdat ze minder gelden.

- **Een gate is niet af tot je hem één keer om de juiste reden rood hebt zien staan.**
  Breek het ding onder test, kijk of de assertie valt, herstel. Dit is wat defect 2 en 6
  boven water haalde. ✅ mutatie draaien en de FAIL laten zien · ❌ "de test zou nu moeten
  slagen".
- **Onderscheid "gecontroleerd en niets gevonden" van "kon niet controleren".** Elke
  exitcode die je negeert, elke lege output waaruit je een conclusie trekt. ✅ `rc=4` en
  een aparte tak · ❌ `2>/dev/null | grep -q` waarvan de lege uitkomst "geen wijziging"
  betekent.
- **Een test die een codepad noemt, moet dat pad ook raken.** Defect 5 was groen en
  toetste niets. ✅ de fixture zo bouwen dat het pad aantoonbaar geraakt wordt (een marker
  in de uitvoer) · ❌ vertrouwen op de naam van de test.

## De pairing

| Helft | Draagt | Wanneer |
|---|---|---|
| deze skill | de norm, de zes defecten, de meetcommando's | terwijl je schrijft |
| `.claude/hooks/shellcheck.sh` | shellcheck op elk bewerkt `*.sh`, blokkerend | direct na de edit |

De hook is met opzet **niet fail-open**, anders dan `ruff-format.sh` en `ty-check.sh`.
Daar mag "de omgeving is stuk" niet lezen als "de code is fout". Hier is stil overslaan
zélf de fout die de gate moet voorkomen: shellcheck is gepind, sha256-geverifieerd en
staat in de gedeelde toolcache, dus "ik kon niet kijken" is een defect en geen normale
toestand. Ontbrekende tool ⇒ exit 2 met de reden, nooit exit 0.

Twee blokkeerdrempels, met opzet twee verschillende sets (zelfde redenering als
`BLOCKING_RULES` in `ruff-format.sh`): gate-mappen tot en met `style`, de rest op
`error`+`warning`. De 26 info- en 31 style-bevindingen in de overige scripts zouden
anders elke edit blokkeren op schuld die niets met de wijziging te maken heeft.

## Bekende valkuilen

- **Een commentaarregel die begint met `# shellcheck ` is een directive.**
  `# shellcheck is GPL-3.0. ...` levert SC1072/SC1073 op. Zet er iets vóór
  (`# Licentie: shellcheck ...`). Stond zo in `workflow-audit.sh` en meteen daarna in de
  eerste versie van `lib/shellcheck-bin.sh`.
- **`# shellcheck source=...` hoort direct boven de `.`-regel.** Staat er
  `set -a; . "$f"; set +a` op één regel, dan slaat de directive op `set -a` en vuurt
  SC1090 alsnog. Splits de regel.
- **`git ls-files` ziet alleen getrackte bestanden.** Een gloednieuwe hook valt daar
  buiten en de gate meldt groen over een set waar hij niet in zat. Altijd
  `--cached --others --exclude-standard`.
- **En dat is nog niet genoeg als het pad zélf genegeerd is.** De standaard-Python
  `.gitignore` bevat een onaangelijnde `lib/`, die elke map met die naam matcht —
  `scripts/hooks/lib/` en `tests/integration/lib/` vielen zo stil buiten git. Vandaar
  KPI-rij 4.
- **`$(git rev-parse --show-toplevel)` vanuit `.git/` wijst naar de verkeerde boom.**
  `.git/` is gedeeld over worktrees (defect 3). Gebruik `$CLAUDE_PROJECT_DIR`, of leid af
  van het pad dat je binnenkreeg.
- **`die()` binnen `$( )` doodt de subshell, niet het script.** `X="$(f)" || exit "$?"`,
  en controleer daarna of `X` niet leeg is.

## macOS: drie GNU-aannames die CI per constructie niet vangt

De runner is Linux (bash 5, GNU coreutils, gawk), de Mac niet (bash **3.2.57**, BSD
realpath, BSD awk). Een hook kan dus repo-breed groen zijn en op de Mac stuk staan —
zo landde #741 met vier kapotte hooks en 13 rode tests die alleen op darwin vielen.
Er is geen macOS-runner; deze drie zijn de hele bekende verzameling, dus kijk ze
met de hand na.

- **`realpath -m` bestaat alleen in GNU coreutils.** BSD antwoordt
  `realpath: illegal option -- m`, en `X=$(realpath -m "$p") || exit 0` maakt daar
  stil fail-open van — defect 4, in de hook die het gat moest dichten. Gebruik de
  `canon_path()`-helper uit `vault-write-guard.sh` (`realpath -m`, anders
  `python3 -c 'import os, sys; print(os.path.realpath(sys.argv[1]))'`). De vlag is
  niet te missen: het doelpad van een `Write` hoeft nog niet te bestaan.
- **Geen `VAR=$(cat <<'EOF' … EOF\n)`.** Bash 3.2 leest een heredoc binnen een
  command substitution als shell, niet als heredoc; elke backtick in de body — en
  een ingebedde Python-parser staat vol `` `git commit` `` in zijn docstrings — wordt
  dan zélf een substitutie. Resultaat: `unexpected EOF while looking for matching \``.
  In een PreToolUse-hook is dat fataal: hij parset niet en blokkeert daarmee **elk**
  Bash-commando in de sessie. Schrijf `IFS= read -r -d '' VAR <<'EOF' || true`.
- **`awk -v` slikt geen newline.** BSD awk stopt met `awk: newline in string`. Geef
  een meerregelige waarde via de omgeving door (`FOO="$bar" awk 'BEGIN { x =
  ENVIRON["FOO"] }'`) en normaliseer hem met `tr '\n' ' '` — een `index(s, " " w " ")`
  vindt een woord aan het regeleinde anders sowieso nooit.

Toets, op de Mac zelf: `bash -n <script>` (bash 3.2 is `/bin/bash`) plus de
bijbehorende testmodule. Groen op de runner zegt hier niets.

## Herkomst

Eigen materiaal. De vorm (KPI-tabel + non-negotiables) komt van de CEDA-skills in
`cedanl/ceda-workshop-starter`; het ontwerp staat in
`docs/developer/planning/specs/2026-08-23-shellcheck-gate-design.md` en
`docs/developer/planning/specs/2026-08-22-instructielagen-rules-skills-hooks-design.md`.
