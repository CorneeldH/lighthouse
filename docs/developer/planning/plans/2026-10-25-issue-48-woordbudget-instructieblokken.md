# Woordbudget van de instructieblokken — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** `lighthouse init` en `init --upgrade` tonen vóór de vraag om akkoord wat de instructieblokken aan woorden toevoegen en waar `CLAUDE.md` + `AGENTS.md` dan uitkomt tegenover het plafond, en een test in lighthouse bewaakt de omvang van de blokken zelf.

**Architectuur:** Eén teller voor alles: een nieuw pakket `internal/wordcount` dat `wc -w` draait op stdin, dezelfde teller als check 10 van `check-instructions.sh`. `initcmd` telt de geplande inhoud van `CLAUDE.md` en `AGENTS.md` (niet die op schijf), leest het plafond via de bestaande `packs.Lookup` (de python-lezer, die de voorgestelde `lighthouse.toml` gebruikt) en meldt het budget tussen de diff en de vraag. De blokplafonds staan in één testbestand in `internal/packs`.

**Tech stack:** Go (`os/exec`, `testing`), bash (`check-instructions.sh`), python3 (`lighthouse_lib.config`).

**Spec:** issue #48 (geen apart specbestand; de acceptatiecriteria staan in de issue).

## Global Constraints

- Meldregel letterlijk in deze vorm: `instruction blocks: +95 words; CLAUDE.md + AGENTS.md becomes 3089, ceiling 3000` (het teken altijd, ook `+0`).
- Boven het plafond is het een waarschuwing, geen stop: na akkoord (of met `--yes`) schrijft `init` gewoon.
- Telling = check 10: dezelfde bestanden (`CLAUDE.md`, plus `AGENTS.md` als dat bestaat), dezelfde teller (`wc -w`), opgeteld; `@AGENTS.md` wordt niet uitgevouwen.
- Plafond uit `project.instructions_word_ceiling` (default 3000 in `pack/core/lib/config.py`), nooit een tweede default in Go.
- Zonder `python3`, zonder `wc` of zonder `CLAUDE.md` na de run: een regel `instruction blocks: budget not measurable: <reden>` op stderr; nooit zwijgen.
- `No changes.` blijft exact de hele uitvoer als er niets verandert (`ci.yml` vergelijkt letterlijk).
- Uitvoer en meldingen van het programma in het Engels; code-commentaar volgt de omringende code; commitberichten in het Nederlands.
- Go volgt `/lighthouse-go-writing`; tests volgen `/lighthouse-testing` (bij elke test: welke productiewijziging hem rood maakt).

## Review Focus

1. **`CLAUDE.md` zonder `AGENTS.md`-wijziging** (alleen een skill verandert) — verwacht: geen budgetregel, want de blokken veranderen niets. Test in taak 2 (`TestBudgetSilentWhenInstructionsUnchanged`).
2. **Eerste `init` in een lege repo** — `CLAUDE.md` bestaat nog niet maar staat in het plan (`@AGENTS.md`, 1 woord); verwacht: telling over de geplande inhoud, oud totaal 0. Test in taak 2 (`TestBudgetCountsPlannedFiles`).
3. **`AGENTS.md` bestaat al, `CLAUDE.md` niet** — `init` maakt `CLAUDE.md` dan niet aan, check 10 slaat over; verwacht: `budget not measurable: CLAUDE.md does not exist`. Test in taak 2 (`TestBudgetNotMeasurableWithoutClaudeMD`).
4. **Bestaande tests zonder `Lookup`** gebruiken de echte `pythonLookup` op `testPacks()` zonder `core/lib`; die krijgen nu een `not measurable`-regel op stderr. Verwacht: tests die een lege stderr eisen, krijgen `stubLookup` met een plafond, niet een versoepelde assert. Taak 2, stap 5.
5. **Witruimte waar `wc -w` en `strings.Fields` verschillen** (NBSP, tab, CRLF, `—`) — verwacht: init telt wat check 10 telt. Test in taak 3 (`TestBudgetMatchesCheck10`), met de mutatie naar `strings.Fields` als controle.

---

### Taak 1: `internal/wordcount` — de teller van check 10

**Files:**
- Create: `internal/wordcount/wordcount.go`
- Test: `internal/wordcount/wordcount_test.go`

**Interfaces:**
- Produces: `func Count(b []byte) (int, error)` — draait `wc -w` met `b` op stdin; `var ErrNoWC = errors.New("'wc' is missing")` als `exec.LookPath("wc")` faalt; een fout met de uitvoer erin als `wc` geen getal geeft.

- [ ] **Stap 1: Schrijf de falende tests**

`TestCountMatchesWC`: tabel met `""` → 0, `"een twee\tdrie\n"` → 3, `"a\r\nb  c"` → 3, `"<!-- lighthouse:begin core -->\n"` → 4. Rood bij: een teller die regels of tekens telt.
`TestCountWithoutWC`: `t.Setenv("PATH", t.TempDir())`; `errors.Is(err, ErrNoWC)`. Rood bij: een fallback op `strings.Fields` in plaats van een fout.

- [ ] **Stap 2: Draai ze, verwacht FAIL** — `go test ./internal/wordcount/` → `undefined: Count`.
- [ ] **Stap 3: Implementeer `Count`** — `exec.Command("wc", "-w")`, `Stdin = bytes.NewReader(b)`, `strconv.Atoi(strings.TrimSpace(out))`. Geen eigen telling.
- [ ] **Stap 4: Draai ze, verwacht PASS** — `go test ./internal/wordcount/`.
- [ ] **Stap 5: Commit** — `feat(wordcount): teller gelijk aan check 10 (#48)`.

### Taak 2: budgetmelding in `init`

**Files:**
- Create: `internal/initcmd/budget.go`
- Modify: `internal/initcmd/initcmd.go` (`Options`, `Run`, `execute` rond de vraag, regels ~195-202)
- Test: `internal/initcmd/budget_test.go`; aanpassingen in `internal/initcmd/initcmd_test.go` waar nodig

**Interfaces:**
- Consumes: `wordcount.Count`, `wordcount.ErrNoWC` (taak 1); `packs.Lookup`; `planned` en `map[string]*planned` uit `buildPlan`.
- Produces:
  - `Options.CountWords func([]byte) (int, error)` — nil betekent `wordcount.Count` (gezet in `Run`).
  - `func instructionWords(plan map[string]*planned, after bool, count func([]byte) (int, error)) (label string, words int, err error)` — telt `CLAUDE.md` en, als het bestaat, `AGENTS.md`, vóór (`old`, alleen als `existed`) of na (`new`, niet bij `delete`). Label `CLAUDE.md` of `CLAUDE.md + AGENTS.md`, zoals check 10. Na de run zonder `CLAUDE.md`: `errNoClaudeMD` (`CLAUDE.md does not exist`). Vóór de run zonder `CLAUDE.md`: 0 woorden, geen fout.
  - `func reportBudget(o Options, plan map[string]*planned, lookup packs.Lookup)` — schrijft de meldregel naar `o.Out`, waarschuwing en `not measurable` naar `o.Err`. Taak 3 gebruikt `instructionWords`.

- [ ] **Stap 1: Schrijf de falende tests** (alle met `stubLookup(map[string]string{"project.instructions_word_ceiling": "<n>"})` en de echte `wordcount.Count`)

- `TestBudgetCountsPlannedFiles`: lege repo, `init` met `"y\n"`. De regel `instruction blocks: +<a> words; CLAUDE.md + AGENTS.md becomes <1+a>, ceiling 3000` staat in `r.out` **vóór** `Apply these changes? [y/N] `, met `a` = `wc -w` van de geschreven `AGENTS.md` (in de test via `wordcount.Count` op het bestand). Rood bij: tellen van de schijf vóór de run (geeft 0), of de regel na de vraag zetten.
- `TestBudgetWarnsOverCeilingAndStillWrites`: plafond `"5"`, `yes`. `r.err` bevat `warning: CLAUDE.md + AGENTS.md is <n-5> words over the ceiling of 5; trim it or raise [project] instructions_word_ceiling`; `rc == 0` en `AGENTS.md` bestaat. Rood bij: `return` of fout bij overschrijding.
- `TestBudgetSilentWhenInstructionsUnchanged`: na `init`, een `--upgrade` waarbij alleen een skill verandert (bestaand patroon voor een gewijzigde pack-unit gebruiken): geen `instruction blocks:` in `r.out` of `r.err`. Na een `--upgrade` zonder wijziging: `r.out == "No changes.\n"`. Rood bij: de regel ook zonder wijziging van `CLAUDE.md`/`AGENTS.md`.
- `TestBudgetNotMeasurableWithoutPython`: `Lookup` geeft `errNoPython`; `r.err` bevat `instruction blocks: budget not measurable: python3 is not available`; `init` schrijft na akkoord. Rood bij: stil overslaan.
- `TestBudgetNotMeasurableWithoutClaudeMD`: repo met alleen een `AGENTS.md` (een regel tekst); `r.err` bevat `budget not measurable: CLAUDE.md does not exist`.
- `TestBudgetNotMeasurableWithoutWC`: `CountWords` geeft `wordcount.ErrNoWC`; `r.err` bevat `budget not measurable: 'wc' is missing`.
- `TestBudgetCeilingNotANumber`: plafond `"veel"`; `r.err` bevat `budget not measurable: project.instructions_word_ceiling is "veel", not a number`.

- [ ] **Stap 2: Draai ze, verwacht FAIL** — `go test ./internal/initcmd/ -run Budget`.
- [ ] **Stap 3: Implementeer** `instructionWords` en `reportBudget` in `budget.go`; roep `reportBudget(o, plan, lookup)` aan in `execute` direct na de `printDiff`-lus en vóór `ask`, alleen in de tak met wijzigingen. `reportBudget` doet niets als noch `plan["CLAUDE.md"]` noch `plan["AGENTS.md"]` `changes()` geeft. Volgorde: eerst tellen (vóór en na), dan het plafond opvragen, zodat een ontbrekende `CLAUDE.md` de eerste reden is. Een `!ok` uit de lookup: `project.instructions_word_ceiling is not set`. Delta = `AGENTS.md`-woorden na min vóór (`%+d`); totaal = na. Waarschuwing alleen als totaal > plafond.
- [ ] **Stap 4: Draai ze, verwacht PASS** — `go test ./internal/initcmd/ -run Budget`.
- [ ] **Stap 5: Volledige pakketrun** — `go test -shuffle=on ./internal/initcmd/`. Een bestaande test die nu struikelt over de nieuwe stderr-regel (Review Focus 4) krijgt `stubLookup` met `"project.instructions_word_ceiling": "3000"`; een test die `r.err == ""` eist, wordt niet versoepeld.
- [ ] **Stap 6: Commit** — `feat(init): toon het woordbudget van de instructieblokken vóór het akkoord (#48)`.

### Taak 3: pariteit met check 10

**Files:**
- Test: `internal/initcmd/budget_parity_test.go`

**Interfaces:**
- Consumes: `instructionWords` (taak 2); het geïnstalleerde script `../../.lighthouse/scripts/check-instructions.sh` (dogfood houdt het gelijk aan `pack/core/scripts/`; de pack-bron vindt `config.py` niet op `../lib/lighthouse_lib/`).

- [ ] **Stap 1: Schrijf de test** `TestBudgetMatchesCheck10`: een tijdelijke repo met `CLAUDE.md` = `"@AGENTS.md\nClaude rest — met\ttab\r\n"` en `AGENTS.md` = drie blokken met markers, dubbele spaties, een NBSP en `—`. Draai `bash <script>` met env `LIGHTHOUSE_REPO_ROOT=<repo>`, `LIGHTHOUSE_TOML=<tmp>/lighthouse.toml` (`[project]\ninstructions_word_ceiling = 100000\n`), `CHECK_INSTRUCTIONS_PERSONAL=<tmp>/personal`. Haal `counts (\d+) words` uit de regel met `CLAUDE.md + AGENTS.md`. Bouw `plan` met `planned{old: b, new: b, existed: true}` voor beide en vergelijk met `instructionWords(plan, true, wordcount.Count)`, ook het label. `t.Skip` alleen als `bash` of `python3` ontbreekt, met die reden.
- [ ] **Stap 2: Draai hem, verwacht PASS** — `go test ./internal/initcmd/ -run Parity -v`.
- [ ] **Stap 3: Mutatiecontrole** — vervang tijdelijk in `wordcount.Count` de `wc`-aanroep door `len(strings.Fields(string(b)))`; verwacht FAIL op de NBSP. Faalt hij niet (de `wc` van deze machine telt NBSP als scheiding), voeg dan een teken toe waar ze wel verschillen (`\u0085`) en noteer welk in een commentaar. Zet de mutatie terug.
- [ ] **Stap 4: Commit** — `test(init): budgettelling gelijk aan check 10 (#48)`.

### Taak 4: plafonds per blok en voor alle blokken

**Files:**
- Create: `internal/packs/budget_test.go`

**Interfaces:**
- Consumes: `wordcount.Count` (taak 1), `agentsmd.Set`, `pack.Files`.

- [ ] **Stap 1: Meet de huidige maten** — voor elk `pack/*/instructions/AGENTS.md`: tel `agentsmd.Set(nil, <pakket>, body)` met `wc -w` (markers tellen mee: 4 woorden per marker, 8 per blok). Verwacht bij de huidige stand: `core` 263, `go` 50, `python` 57, `shell` 54; samen 424. Wijkt een meting af, dan geldt de meting.
- [ ] **Stap 2: Schrijf de test** `TestInstructionBlocksWithinCeiling`:
  - `var blockCeilings = map[string]int{"core": 263, "go": 50, "python": 57, "shell": 54}` en `const blocksCeiling = 424`: de enige plek voor deze plafonds, met een commentaar dat een blok verwijst naar een skill of rule en geen uitleg draagt, en dat een verhoging een bewuste keuze in de PR is.
  - Loop over alle pakketten in `pack.Files` (`fs.ReadDir(pack.Files, ".")`); een pakket zonder `instructions/AGENTS.md` (zoals `expo`) telt 0 woorden.
  - Een blok zonder vermelding in `blockCeilings` faalt: `block %q has no ceiling in blockCeilings`.
  - Een blok boven zijn plafond faalt: `block %q counts %d words, ceiling %d: trim it to pointers to a skill or rule`.
  - De som van alle blokken (de zwaarste combinatie: alle pakketten) boven `blocksCeiling` faalt.
  - Een vermelding in `blockCeilings` zonder blok faalt (een dood plafond).
- [ ] **Stap 3: Draai hem, verwacht PASS** — `go test ./internal/packs/ -run InstructionBlocks -v`.
- [ ] **Stap 4: Mutatiecontrole** — voeg tijdelijk één woord toe aan `pack/python/instructions/AGENTS.md`; verwacht FAIL op `python`. Verlaag tijdelijk `blocksCeiling` met 1; verwacht FAIL op de som. Zet beide terug.
- [ ] **Stap 5: Commit** — `test(packs): plafond per instructieblok en voor alle blokken samen (#48)`.

### Taak 5: documentatie, dogfood en de volledige gate

**Files:**
- Modify: `pack/core/skills/instrument/SKILL.md:223` (de KPI-rij: noem dat `init` het budget vooraf meldt)
- Modify: de geïnstalleerde kopie via `init --upgrade`

- [ ] **Stap 1: Werk de KPI-rij bij** — achter de meetcel: `; \`lighthouse init\` and \`init --upgrade\` report the budget before writing`. Draai `uv run pytest -q tests/pack/test_instrument_skill.py`; verwacht PASS.
- [ ] **Stap 2: Dogfood** — `go run ./cmd/lighthouse init --upgrade` (akkoord), daarna `go run ./cmd/lighthouse init --upgrade </dev/null`; verwacht exact `No changes.`.
- [ ] **Stap 3: Volledige gate** — `uv run pytest -q && go test -shuffle=on ./...`; verwacht groen. `golangci-lint run ./...`; verwacht geen bevindingen.
- [ ] **Stap 4: Handmatige proef** — in een tijdelijke repo `go run ./cmd/lighthouse init` met `[project] instructions_word_ceiling = 100` in een vooraf geschreven `lighthouse.toml`; verwacht de meldregel en de waarschuwing vóór `Apply these changes? [y/N]`. Plak de uitvoer in de PR als bewijs.
- [ ] **Stap 5: Commit** — `docs(instrument): budget van init in de KPI-rij (#48)` (met de geïnstalleerde kopie).
