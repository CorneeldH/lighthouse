# Ontwerp: de test-gate na de commit, alleen op wat de commit raakt

Issue: #80 · Datum: 2026-10-27 · Herschrijft #80; de keuzes hieronder gaan voor op de oude tekst
van het issue.

## Aanleiding en doel

Elke commit in deze repo wacht ruim drie minuten op de test-gate. Gemeten op 2026-10-27, 32 cores:

| Deel van `[test_gate] command` | Duur |
|---|---|
| `uv run pytest -q` (2865 tests, serieel, geen `xdist`) | 162 s |
| `go test -race -shuffle=on ./...` | 28 s, waarvan 27 s in `internal/usage` |

personal_ai_agent (paia) is sneller doordat het snoeit (`tests/unit/`, zonder `slow`) en parallel
draait (`-n auto --maxprocesses 4`). Het kijkt niet naar wat er veranderd is.

Het doel: de gate test **alleen wat de commit raakt**, een kleine subset in seconden. CI test het
geheel, net zoals CI na een PR het geheel test. Dat geldt voor elke repo met lighthouse, en deze
repo stapt zelf over.

## Besloten in het gesprek

1. **De tests draaien na de commit, niet ervoor.** Na de commit staat vast wat erin zit. Het
   probleem van `git add X && git commit` en `git commit -a` in één Bash-call verdwijnt daarmee:
   vóór de call is X nog niet staged.
2. **Een rode commit mag op de branch landen.** De hook meldt rood aan de agent, en die herstelt
   het in een volgende commit. `main-commit-guard` houdt `main` dicht. De squash-merge en CI houden
   de rode tussencommit van `main` af.
3. **Te weinig testen is acceptabel.** Een wijziging die de selectie mist, vangt CI. Er is geen
   regel "pad in geen enkel domein: draai alles" meer. Python krimpt in deze repo; een grove
   selectie voor Python is goed genoeg.
4. **Aanpak A: de hook geeft de lijst door, het project kiest de tests.** De hook bepaalt de
   gewijzigde bestanden en geeft ze aan `[test_gate] command`. Elk taalpakket levert een
   *selector*: een script dat uit die lijst de tests van zijn taal kiest en draait. Een project
   zet de selectors in zijn commando.
5. **Een commit naar de default branch blijft vooraf getest.** Met `.claude/allow-main` kan een
   commit op `main` landen, en met `git commit && git push` in één call staat hij op origin voordat
   een hook na de commit iets meldt. `ci.yml` draait alleen op `pull_request`, en `main` heeft op
   2026-10-27 geen branch protection of ruleset (`gh api …/branches/main/protection`: 404). Voor
   een commit met `TO_MAIN` draait `pre` daarom de gate zoals nu: vooraf, blokkerend, het hele
   commando.
6. **Generiek én voor deze repo.** De functie zit in `pack/core` en de taalpakketten. Lighthouse
   gebruikt ze zelf (dogfood). paia krijgt ze bij de volgende upgrade.

## Afgewogen aanpakken

| Aanpak | Afweging | Uitkomst |
|---|---|---|
| **A. De hook geeft de lijst door, selectors per taalpakket** | Kleine kern in de hook; per taal uitbreidbaar; een project zonder selector merkt niets | **Gekozen** |
| B. Selectieregels in de config (glob naar commando-sjabloon) | Niets te scripten, maar de hook moet Go-packages kennen en de config groeit uit tot een mini-taal | Afgevallen |
| C. `pytest-testmon` plus Go-selectie in de hook | Ziet alleen Python-coverage; mist wijzigingen in shell en markdown | Afgevallen |

Ook overwogen en afgevallen: alleen paia's trucs overnemen (`xdist` met een plafond, trage tests
eruit). Dat haalt de tijd omlaag, maar een commit test dan nog steeds alles. `xdist` blijft wel
nuttig binnen een selector die veel Python-tests kiest; dat is aan de bouwer.

## Scope

#80 levert:

1. **De gate na de commit** in `pack/core` (zie *De hook*).
2. **Het contract** tussen hook en commando (zie *Contract*).
3. **Een selector in `pack/go` en in `pack/python`**, die een project met de hand in
   `[test_gate] command` zet (zie *Selectors*).
4. **Stop-checks per domein.** `go-check.sh` (`pack/go`) en `ty-check.sh` (`pack/python`) slaan
   over als de beurt geen pad in hun domein raakte, en melden dat. Dit is het oude criterium van
   #80 en blijft staan.
5. **`pack/node`** met de frontendchecks van #68 en een selector volgens hetzelfde contract. Het
   gate-deel van het lokale script van #68 (`ci/domains.py gate`) verdwijnt. Dit is het oude criterium van #80 en blijft staan. #68 is
   bijna klaar; de bouwer begint met 1 tot en met 4.
6. **De overstap van deze repo**: `[test_gate] command` gebruikt de selectors, en
   `go run ./cmd/lighthouse init --upgrade` geeft `No changes.`
7. **Tekst die mee moet**: de core-regel "`pre-commit-tests` runs `[test_gate] command` before
   every commit" in `pack/core/instructions/AGENTS.md`, de lijst *Hooks* in `CLAUDE.md`, en de
   verwijzingen in de skills `fix-pr`, `verify-pr`, `go-writing` en `python-writing`.

Buiten scope: CI. `ci.yml` blijft elk geraakt domein helemaal testen (zie *Verhouding tot #68*).

## De hook

`pre-commit-tests.sh` wordt `commit-tests.sh` met twee standen, naar het model van
`bash-write-gates.sh` *(eigen keuze: de oude naam klopt niet meer)*:

- **`commit-tests.sh pre`** (PreToolUse `Bash`):
  - blokkeert `--no-verify`, zoals nu (`no_verify.py`); dat moet vóór de call;
  - legt per root die gecommit wordt de huidige `HEAD` vast, met dezelfde commitdetectie als nu
    (`git_invocations.py`). Een root zonder `HEAD` (eerste commit) krijgt de lege boom als basis.
  - Draait geen tests en blokkeert verder niets, behalve bij een commit met `TO_MAIN`: die test
    hij vooraf zoals nu (besluit 5).
- **`commit-tests.sh post`** (PostToolUse **en** PostToolUseFailure `Bash`):
  - vergelijkt per vastgelegde root de `HEAD` nu met de basis. Is hij niet verschoven, dan is er
    niets gecommit: stil exit 0.
  - Is hij verschoven, dan bepaalt hij de bestanden van `basis..HEAD` en draait hij
    `[test_gate] command` in die root, met het contract hieronder.
  - Rood is exit 2 met de uitvoer naar de agent, ook als het commando niet als tool-fout eindigde.
  - PostToolUseFailure hoort erbij: bij `git commit -m x && false` faalt de call, maar de commit
    staat er wel.

Wat blijft zoals het is: slots, de deadline, de geheugenrem (`gate_slice`),
`unchecked_exit_codes`, de skip van een andere repo, en "niet fail-open": een post die niet kan
testen, blokkeert met `COULD NOT CHECK`. Wat verdwijnt is het blokkeren van de commit zelf.

Het moment verandert de betekenis van exit 2. Vóór de commit hield exit 2 de commit tegen; na de
commit is het een melding die de agent moet verwerken. De commit staat er al.

Randgevallen *(eigen keuzes)*:

| Geval | Gedrag |
|---|---|
| Meerdere commits in één call | `basis..HEAD` dekt ze allemaal |
| `--amend` | `basis..HEAD` is alleen het verschil met de oude commit; te weinig is acceptabel |
| Rename, ook over een domeingrens | Beide paden in de lijst (`--no-renames`) |
| Verwijderd bestand | Staat in de lijst; de selector gaat ermee om |
| Merge-commit | `basis..HEAD`, dus alles wat de merge binnenhaalde |
| `HEAD` verschoof zonder commit (`git reset`, `checkout`) | Wordt alleen gezien als het commando ook een `git commit` bevatte; dan geldt `basis..HEAD`. Acceptabel |
| Geen selector levert tests | Groen, met een zichtbare regel dat niets getest is en waarom |
| Bash-call met `run_in_background` | `post` komt vóór de commit en ziet geen verschoven `HEAD`. Zichtbare melding dat de commit niet getest is, geen stille pass |

## Contract

De hook draait `bash -c "$COMMAND"` in de root, met naast `LIGHTHOUSE_GATE_TMP`:

| Variabele | Inhoud |
|---|---|
| `LIGHTHOUSE_GATE_BASE` | de basis-commit (of de lege boom) |
| `LIGHTHOUSE_GATE_HEAD` | de nieuwe `HEAD` |
| `LIGHTHOUSE_GATE_FILES` | pad naar een bestand in `LIGHTHOUSE_GATE_TMP` met de gewijzigde paden, één per regel, relatief aan de root |

Een commando dat de variabelen negeert, test zoals nu, alleen dan na de commit. Zo blijft een
project zonder selector werken: paia's huidige commando draait na de upgrade gewoon de hele
unit-suite, tot paia selectors kiest.

## Selectors

Een selector is een script uit een taalpakket, aangeroepen vanuit `[test_gate] command`. Hij leest
`LIGHTHOUSE_GATE_FILES`, kiest de tests van zijn taal en draait ze. Raakt de lijst zijn domein
niet, dan eindigt hij met 0 en één regel uitleg. Exitcodes volgen het commando dat hij draait;
`unchecked_exit_codes` van het project blijft gelden *(eigen keuze)*. "Niets geselecteerd"
handelt de selector zelf af met exit 0, zodat pytest nooit zonder bestanden start en exit 5
alleen nog betekent dat een gekozen bestand geen tests had.

Zonder `LIGHTHOUSE_GATE_FILES` (Codex of OpenCode, of met de hand in een shell) test een selector
zijn hele domein *(eigen keuze)*. De regel in *Outside Claude Code* blijft dan waar: wie
`[test_gate] command` met de hand draait, test zoals nu.

**Inbouw in het commando** *(eigen keuze)*: met de hand, per project. `init` voegt
`lighthouse.toml`-fragmenten niet samen (`initcmd.go`: "comes from both package …") en verandert
een bestaand commando bij `--upgrade` niet. `pack/go` krijgt dus geen fragment voor
`[test_gate] command`; het fragment van `pack/python` blijft zoals het is. De skills `go-writing`
en `python-writing` beschrijven hoe je een selector in je commando zet. Deze repo en paia passen
hun commando zelf aan. Fragmenten samenvoegen in `init` valt buiten #80.

**Go** (`pack/go`):

- een gewijzigd `.go`-bestand geeft zijn package;
- een gewijzigd niet-Go-bestand geeft het package dat het insluit, via `EmbedFiles` en
  `TestEmbedFiles` uit `go list -json`. Zo test een wijziging in `pack/core/hooks/*.sh` het
  package `pack`;
- **alleen die packages**, niet wie ervan afhangt. Een importeur die niet meer compileert, vangt
  `go-check` bij Stop; gedrag dat via-via breekt, vangt CI. Gemeten op 2026-10-27, met `-race` en
  zonder cache: één package kost ~1,2 s, met de directe importeurs erbij 1,5–6 s. Uitzondering is
  `internal/usage` met 27 s;
- `go.mod` of `go.sum` alleen gewijzigd: geen packages, dat vangt CI;
- draait `go test` op die set. Vlaggen zoals `-race` geeft het project mee.

De gate gebruikt geen `-shuffle=on`. Die vlag zet Go's testcache uit: een herhaalde
`go test -race ./...` kost dan 27,6 s in plaats van 0,19 s. CI houdt `-shuffle=on`.

**Python** (`pack/python`):

- een gewijzigd testbestand (`test_*.py`) draait zelf;
- een ander gewijzigd bestand `X.py`, `X.sh` of `X` (zonder extensie) koppelt aan `test_X.py`.
  De naam van een bestand met een streepje telt ook in de vorm met een underscore. Bijvoorbeeld:
  `pre-commit-tests.sh` koppelt aan `test_pre_commit_tests.py`;
- testmappen uit de config, standaard `tests/`;
- draait `pytest` op die bestanden, met de vlaggen die het project meegeeft.

De conventie mist bijvoorbeeld markdown die een test leest (`test_instruction_files.py`). Dat is
besluit 3.

**Node** (`pack/node`): hetzelfde contract voor de frontend van #68. De bouwer werkt de selectie
uit als #68 er is.

Voor deze repo wordt het commando ongeveer
`<go-selector> -race && <python-selector> -q`. Of de selectors na elkaar draaien of
allebei draaien en de uitkomst combineren, is aan de bouwer *(zie onder)*.

## Verhouding tot #68 (PR 105)

PR 105 (open op 2026-10-27, branch `issue-68-spa-skelet`) zet in deze repo
`[test_gate] command = "python3 ci/domains.py gate"`. `ci/domains.py` deelt de gewijzigde paden in
drie domeinen in (`web`, `python`, `go`) en draait per geraakt domein het hele domein:

- **python:** `uv run pytest -q`
- **go:** `go test -race -shuffle=on ./...`
- **web:** `npm run lint`, `typecheck` en `test`, plus een API-driftcheck (`npm run gen` en
  vergelijken met de worktree)

Dat is selectie per domein, vóór de commit, op staged, unstaged en untracked paden. Elk pad buiten
`web/` raakt zowel `python` als `go`, dus een commit zonder frontend kost na PR 105 nog steeds
ruim drie minuten.

Wat #80 ermee doet:

- **`ci/domains.py gate` verdwijnt.** Het commando van deze repo wordt de selectors van de
  pakketten. `unchecked_exit_codes = [3]` (geen node, geen `web/node_modules`) gaat mee naar de
  node-selector.
- **`ci/domains.py classify` blijft.** CI gebruikt het als filter per job in `ci.yml`. Besluit
  "CI test het geheel" betekent daarom: CI test elk geraakt domein helemaal. #80 verandert niets
  aan CI. Waar `ci/domains.py` na #80 woont, is aan de bouwer.
- **De node-selector neemt de webchecks over**, inclusief de API-driftcheck en de paden buiten
  `web/` die ze raken (`_WEB_TOO`, `_GO_TOO`). Binnen de webchecks kan `vitest related <files>`
  de tests kiezen; lint en typecheck blijven hele-domeinchecks. Dat is aan de bouwer.
- **Volgorde:** #80 bouwt na de merge van PR 105, of rebaset erop. `tests/test_ci_domains_pin.py`
  en `tests/test_domains.py` uit PR 105 gaan mee met wat er van `ci/domains.py` overblijft.

## Aan de bouwer

- De vorm van de selectors (bash of Python, plaats onder `.lighthouse/`, aanroep) en hoe het
  commando meerdere selectors combineert.
- Waar de basis tussen `pre` en `post` bewaard wordt (de datadir, zoals `bash_writes.py`), en wat
  er gebeurt met een basis die achterblijft omdat `post` nooit kwam.
- Hoe `init --upgrade` de oude registratie en het oude bestand `pre-commit-tests.sh` opruimt.
  Dat moet werken voor deze repo en voor paia. Of de huidige upgrade dat al kan, is niet
  nagekeken.
- Hoe de Stop-checks weten welke paden de beurt raakte.
- `xdist` met een plafond in de Python-selector, als een selectie groot uitvalt.
- De tests van `internal/usage` (27 s; `TestCodexAppServerRejects` 6,3 s,
  `TestUsageCommandTimeout` 5,2 s): buiten #80.
