# Een gepushte branch bijwerken met main via merge — implementatieplan

> **Voor agents:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkboxen (`- [ ]`).

**Doel:** één regel in het gedeelde instructieblok zegt hoe een branch met `origin/<default branch>` wordt bijgewerkt (gepusht: merge; niet gepusht: rebase mag), en `lighthouse-write-handoff` en `lighthouse-fix-pr` volgen die regel.

**Architectuur:** de regel komt als één bullet in `pack/core/instructions/AGENTS.md` (het managed block, dat `init` in elke lighthouse-repo schrijft). Reden volgens `/lighthouse-place-instruction`: de regel is cross-cutting, hangt aan geen pad of bestandstype, is geen procedure en hoeft niet afgedwongen (een hook kan een bedoelde rebase van een nog niet gepushte branch niet onderscheiden). De skills verwijzen ernaar en herhalen hem niet. De installed kopieën volgen uit `init --upgrade`.

**Tech stack:** Markdown in `pack/core`, Go (`internal/packs/budget_test.go`), `go run ./cmd/lighthouse init --upgrade`.

**Issue:** #179. Geen spec; de issuetekst is de spec.

## Beslissing: laag en woordbudget

De bullet komt in het managed block. Het `core`-blok staat op 303 woorden bij een plafond van 311; de bullet kost 42 woorden (`wc -w`). Het plafond omhoog is een bewuste keuze in deze PR (zie de comment in `budget_test.go`): `blockCeilings["core"]` en `blocksCeiling` gaan allebei met het gemeten aantal extra woorden omhoog, niet met een ruime marge.

## Globale randvoorwaarden

- Instructies (pack, skills) in het Engels; dit plan, commits en de PR in het Nederlands.
- Regeltekst, letterlijk: `- Update a branch with `origin/<default branch>` by `git merge` once it is on origin, never by rebase plus force-push; rebase only a branch that is not pushed yet. Every PR is squash-merged, so the merge commit never reaches the default branch.`
- Elke wijziging in een skill raakt beide varianten: `pack/core/skills/fix-pr/claude/` en `.../agents/`; `write-handoff` heeft één bron.
- Dogfood: na elke wijziging onder `pack/` `go run ./cmd/lighthouse init --upgrade`; de tweede run print `No changes.`
- Elke PR een fragment in `changelog.d/` (`docs/releasing.md`).

## Review Focus

1. Handoff voor een branch die achter staat én niet gepusht is → rebase-commando blijft toegestaan, niet altijd merge.
2. `fix-pr` start altijd op een gepushte branch (er is een PR) → alleen merge, nooit rebase.
3. Mergeconflict bij het mergen in `fix-pr` → conflicten één keer oplossen, `git merge --continue`, geen `git rebase` in hetzelfde blok als de diagnose (bestaande regel blijft).
4. Het woordbudget: de test faalt als het plafond niet met precies de toegevoegde woorden is verhoogd.
5. `.claude/skills` en `.agents/skills` lopen uiteen → `check-instructions.sh` (check 3 en 11) faalt.

## Taak 1: de regel in het managed block

**Bestanden:**
- Wijzig: `pack/core/instructions/AGENTS.md` (na de bullet over de test gate)
- Wijzig: `internal/packs/budget_test.go:15,18`
- Wijzig: `tests/test_instruction_files.py:28-33` (tweede woordbudget: `<= 303` en de docstring)
- Gegenereerd: `AGENTS.md` (managed block), via `init --upgrade`

**Interfaces:**
- Produceert: de bullet die taak 2 en 3 noemen als "de regel in *Lighthouse: core*".

- [ ] **Stap 1: zet de regel uit Globale randvoorwaarden als bullet in `pack/core/instructions/AGENTS.md`**, direct na de `Test gate`-bullet.
- [ ] **Stap 2: draai `go test ./internal/packs/...`.** Verwacht: `TestInstructionBlocksWithinCeiling` faalt op het `core`-blok (rood bewijst dat de test de bullet ziet).
- [ ] **Stap 3: verhoog `blockCeilings["core"]` en `blocksCeiling` met het verschil tussen het gemeten en het oude aantal** (`wc -w pack/core/instructions/AGENTS.md` plus de markers die de test telt; lees het aantal uit de testmelding).
- [ ] **Stap 3b: verhoog ook de `303` in `tests/test_instruction_files.py` (assert en docstring)** met het aantal toegevoegde woorden volgens `wc -w pack/core/instructions/AGENTS.md` (nu 303; dit is een ander telsysteem dan de Go-test, die ook markers telt).
- [ ] **Stap 4: `go test ./internal/packs/...` en `uv run pytest -q tests/test_instruction_files.py`** → PASS.
- [ ] **Stap 5: `go run ./cmd/lighthouse init --upgrade`**, daarna nogmaals → `No changes.`
- [ ] **Stap 6: commit** `feat(core): een gepushte branch werk je bij met merge, niet met rebase en force-push (#179)` met `pack/core/instructions/AGENTS.md`, `internal/packs/budget_test.go`, `tests/test_instruction_files.py` en de gegenereerde kopieën.

## Taak 2: `lighthouse-write-handoff`

**Bestanden:**
- Wijzig: `pack/core/skills/write-handoff/SKILL.md:327-328` (bullet *No assumptions about the branch*)
- Gegenereerd: `.claude/skills/lighthouse-write-handoff/SKILL.md`, `.agents/skills/lighthouse-write-handoff/SKILL.md`

**Interfaces:**
- Consumeert: de regel uit taak 1; de skill noemt hem en herhaalt hem niet.

- [ ] **Stap 1: vervang "say so with the rebase command" door** een zin die zegt: staat de branch achter `origin/<default branch>`, geef dan het commando dat bij de regel in *Lighthouse: core* past (`git merge origin/<default branch>` als de branch op origin staat, `git rebase origin/<default branch>` als niet). Controle of de branch gepusht is: `git ls-remote --exit-code --heads origin <branch>` (exit 0 = gepusht).
- [ ] **Stap 2: `init --upgrade`** en controleer met `git diff --stat` dat alleen de twee kopieën meeveranderen.
- [ ] **Stap 3: commit** `fix(write-handoff): bij een gepushte branch achter main het merge-commando geven (#179)`.

## Taak 3: `lighthouse-fix-pr`

**Bestanden:**
- Wijzig: `pack/core/skills/fix-pr/claude/SKILL.md` en `pack/core/skills/fix-pr/agents/SKILL.md`: alleen de bullet *A merge conflict gives no fail check* in *Known pitfalls*
- Gegenereerd: `.claude/skills/lighthouse-fix-pr/SKILL.md`, `.agents/skills/lighthouse-fix-pr/SKILL.md`

**Interfaces:**
- Consumeert: de regel uit taak 1.

- [ ] **Stap 1: voeg in beide varianten, onder de Known pitfalls-bullet over `DIRTY`, een zin toe**: een PR heeft een gepushte branch, dus los een conflict op met `git merge origin/<default branch>` volgens de regel in *Lighthouse: core*; los de conflicten één keer op, commit de merge en push gewoon (geen force). Geen herhaling van de regel zelf. De bestaande ❌ `git rebase` in hetzelfde blok als de diagnose blijft staan.
- [ ] **Stap 2: `init --upgrade`**, beide varianten identiek op de bekende `dangerouslyDisableSandbox`-regels na.
- [ ] **Stap 3: `.lighthouse/scripts/check-instructions.sh`** → groen (mirror-checks 3 en 11, woordplafond check 10).
- [ ] **Stap 4: commit** `fix(fix-pr): een mergeconflict oplossen met merge, niet met rebase (#179)`.

## Taak 4: afronding

**Bestanden:**
- Maak: `changelog.d/issue-179-een-gepushte-branch-bijwerken-met-main.md`

- [ ] **Stap 1: schrijf het fragment**: `type:` `changed`, in het Engels, 1–2 zinnen (een gepushte branch wordt bijgewerkt met merge, niet rebase; handoff en fix-pr volgen). Formaat uit `docs/releasing.md`.
- [ ] **Stap 2: `go test ./...`, `uv run pytest -q`, `go run ./cmd/lighthouse init --upgrade` (→ `No changes.`) en `.lighthouse/scripts/check-instructions.sh`** allemaal groen.
- [ ] **Stap 3: commit** `docs(changelog): fragment voor #179`.

## Zelfreview

- AC 1 → taak 1 · AC 2 → taak 2 · AC 3 → taak 3 · AC 4 → stap 5 van taak 1 plus elke `init --upgrade` en stap 2 van taak 4.
- Geen test beweert de skilltekst; dat blijft leeswerk bij de review (de scout meldde dit ook).
