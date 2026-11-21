# Herbeoordeel het core-blok en de packinstructies — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** Elke regel van het core-blok en de packinstructies (go, python, shell, node) krijgt een oordeel (houden, inkorten, verplaatsen, schrappen) met één zin reden en een toetssteen; het oordeel wordt toegepast en de woordtelling daalt, voor en na genoteerd.

**Architectuur:** Eerst een beoordelingstabel als document (het bewijs per regel), dan de bronnen onder `pack/*/instructions/AGENTS.md` aanpassen, daarna de tests die woorden vastpinnen en de plafonds bijstellen, en tot slot de dogfood-kopie (`init --upgrade`) en het changelogfragment. De tabel is de bron van waarheid: een regel verdwijnt of krimpt pas als de tabel het zegt.

**Tech stack:** Markdown, Go (`internal/packs`, `internal/packcontract`), Python-tests in `tests/pack/`, `lighthouse check-instructions`, `lighthouse init --upgrade`.

**Spec:** issue #390 (geen apart specbestand; de acceptatiecriteria staan in de issue).

## Global Constraints

- Toetssteen per regel, letterlijk uit de issue: een regel blijft alleen als (a) een hook of check hem draagt, (b) hij een feit bevat dat het model niet kan afleiden (pad, pin, label), of (c) er aantoonbaar recent een fout op gemaakt is, met bewijs uit een transcript of issue.
- Nadruk (vet, hoofdletters, "altijd", "never") blijft alleen bij regels met bewijs onder (c); onder (a) en (b) wordt de nadruk weggehaald.
- Uitkomsten: precies `houden`, `inkorten`, `verplaatsen` of `schrappen`, met één zin reden.
- Instructietekst (de blokken) is Engels; de tabel, dit plan en de PR zijn Nederlands (`[project] language = nl`).
- Telling = `wc -w` op `CLAUDE.md` + `AGENTS.md` (check 10, plafond 3000); voor-meting uit dit plan: `wc -w AGENTS.md CLAUDE.md` = 868 + 339 = **1207**; bronblokken: core 365, go 65, node 29, python 75, shell 46 (`wc -w pack/*/instructions/AGENTS.md`).
- Een `verplaatsen` komt alleen met een ontvangende skill of doc die de regel werkelijk al niet draagt; de laag kiest `/lighthouse-place-instruction`.
- Wijzigingen onder `pack/` draaien `go run ./cmd/lighthouse init --upgrade` en committen de geïnstalleerde kopie; CI eist `No changes.`
- Nooit `--no-verify`; rood na een commit herstel je in een nieuwe commit.

## Review Focus

1. **Een regel die een test letterlijk eist** (`gofmt`, `govet`, `go-lint-gate`, `COULD NOT CHECK`, `quality-baseline.toml`, de regel `- \`/lighthouse-upgrade\`: `) — verwacht: de test wordt bewust aangepast met de reden in de commit, nooit stilzwijgend rood of verwijderd. Taak 3.
2. **Het skilllijstje (core regels 15-31)** — verwacht: pas weg of korter als de beschrijvingen in de skillcatalogus het zelfde vertellen voor Codex en OpenCode; de upgradetest pint één regel. Taak 1 beslist, taak 3 past aan.
3. **Een gedropte regel waar een hook alleen blokkeert zonder uitleg** (bv. `allow-main`-ontsnapping, `--no-verify`) — verwacht: de hookmelding draagt de uitleg, anders blijft de regel (toetssteen a vraagt dat de hook hem *draagt*, niet dat hij bestaat). Taak 1 leest de hookmelding.
4. **Python- en shellblok zijn niet in deze repo geïnstalleerd** — verwacht: hun voor/na-telling komt uit de bron (`wc -w pack/<x>/instructions/AGENTS.md`), niet uit check 10. Taak 4.
5. **Plafond niet meegezakt** — verwacht: de blokplafonds in `internal/packs/budget_test.go` volgen de nieuwe tellingen, zodat een latere groei weer rood wordt. Taak 3.

---

### Taak 1: De beoordelingstabel

**Files:**
- Create: `docs/developer/planning/specs/2026-11-21-issue-390-beoordelingstabel.md`

**Interfaces:**
- Produces: een tabel met kolommen `Blok | Regel (eerste woorden) | Uitkomst | Toetssteen (a/b/c/geen) | Nadruk | Reden (één zin) | Bewijs`, één rij per regel van `pack/core/instructions/AGENTS.md:3-13`, de 15 skillregels (mag als één groep met één reden per skill die de catalogus al beschrijft, maar dan expliciet), `pack/go` (3 regels), `pack/python` (4), `pack/shell` (2), `pack/node` (1). Taak 2 en 3 lezen alleen deze tabel.

- [ ] **Step 1: Bepaal per regel de toetssteen.** Voor (a): open de hook onder `pack/core/hooks/` (`main-commit-gate`, `commit-test-gate`, `pr-create-gate`, `worktree-check`) en `pack/go|python|shell|node/hooks/` en citeer de meldingstekst die de regel al draagt. Voor (b): noem het feit (pad, pin, label, configsleutel). Voor (c): zoek bewijs met `lighthouse forge issue list --state all --search "<term>"` en `git log --oneline --grep '<term>'`; een regel zonder vindbaar bewijs krijgt toetssteen `geen` en dus `schrappen` of `inkorten`.
- [ ] **Step 2: Vul de kolom `Nadruk`** met elke vette, hoofdletter- of "never/always"-formulering van de regel en of bewijs (c) ze rechtvaardigt; zonder (c) staat de uitkomst op `inkorten` of de nadruk gaat eruit.
- [ ] **Step 3: Schrijf het document** met bovenaan de voor-meting (`wc -w AGENTS.md CLAUDE.md` en de vijf bronblokken, uitvoer erbij) en onderaan een lege sectie `## Na-meting` die taak 4 vult. Relatieve links naar de bronbestanden; `md-link-check` blokkeert absolute.
- [ ] **Step 4: Verifieer** — `grep -c '^| ' docs/developer/planning/specs/2026-11-21-issue-390-beoordelingstabel.md` is gelijk aan 1 kopregel + 1 scheiding + het aantal regels (13 core + skillgroep(en) + 3 + 4 + 2 + 1 = ten minste 23 rijen); `grep -E 'houden|inkorten|verplaatsen|schrappen'` geeft op elke rij een uitkomst; geen rij met lege Reden.
- [ ] **Step 5: Commit**

```bash
git add docs/developer/planning/specs/2026-11-21-issue-390-beoordelingstabel.md
git commit -m "docs(planning): beoordelingstabel core-blok en packinstructies (#390)"
```

### Taak 2: De tabel toepassen op de bronblokken

**Files:**
- Modify: `pack/core/instructions/AGENTS.md`, `pack/go/instructions/AGENTS.md`, `pack/python/instructions/AGENTS.md`, `pack/shell/instructions/AGENTS.md`, `pack/node/instructions/AGENTS.md`
- Modify (alleen bij `verplaatsen`): de ontvangende skill onder `pack/core/skills/<naam>/SKILL.md` of een doc onder `docs/`

**Interfaces:**
- Consumes: de tabel uit taak 1.
- Produces: bronblokken waarvan elke overgebleven regel een `houden` of `inkorten` in de tabel heeft; de kopregel `## Lighthouse: <naam>` blijft ongewijzigd (`agentsmd.Set` zoekt erop).

- [ ] **Step 1: Pas de blokken aan** precies volgens de tabel: `schrappen` = regel weg, `inkorten` = herschreven tekst uit de tabel, `verplaatsen` = regel naar de ontvangende skill/doc (controleer eerst dat die hem niet al bevat) en weg uit het blok. Zonder (c)-bewijs geen vet, hoofdletters of "never/always".
- [ ] **Step 2: Controleer de verplaatsingen** — `go run ./cmd/lighthouse check-instructions` is groen (mirror, overlap, binding: hard-hooks).
- [ ] **Step 3: Meet** — `wc -w pack/*/instructions/AGENTS.md`: elk blok is gelijk of lager dan de voor-meting en het totaal is lager; leg de getallen vast voor taak 4.
- [ ] **Step 4: Commit** (één commit per logische groep: core, daarna de packs)

```bash
git add pack/core pack/go pack/python pack/shell pack/node
git commit -m "refactor(pack): core-blok en packinstructies ingekort volgens de beoordelingstabel (#390)"
```

### Taak 3: Tests en plafonds bijstellen

**Files:**
- Modify: `internal/packs/budget_test.go:19,22`; `internal/packcontract/upgrade_test.go:211-212`; `tests/pack/go/test_go_pack.py:18-24`; `tests/pack/python/test_python_pack.py:20-33`; `tests/pack/shell/test_shell_gates.py:143-148`; `tests/pack/node/test_node_pack.py:26`

**Interfaces:**
- Consumes: de bronblokken uit taak 2.

- [ ] **Step 1: Draai de pinnende tests** — `go test ./internal/packs/ ./internal/packcontract/ ./internal/initcmd/ ./internal/checkinstr/` en `uv run pytest -q tests/pack/go tests/pack/python tests/pack/shell tests/pack/node`. Verwacht: alleen tests die een nu geschrapte of herschreven woordsoort pinnen zijn rood.
- [ ] **Step 2: Per rode test beslissen** — komt het vastgepinde woord nog voor in een behouden regel, herstel dan de tekst; is de regel bewust weg, pas dan de test aan (verwijder alleen de needle, niet de test) en zeg de reden in de commit. Elke aanpassing volgt `/lighthouse-test-standard`: de test blijft falen als het gedrag breekt dat hij bewaakt.
- [ ] **Step 3: Zak de plafonds** in `blockCeilings` en `blocksCeiling` naar de nieuwe tellingen (hooguit enkele woorden marge, geen ruime buffer); bij een bewust verlaagd plafond de `#390`-regel in het commentaar bij `internal/packs/budget_test.go:16-18` bijwerken.
- [ ] **Step 4: Verifieer** — dezelfde twee commando's als stap 1 zijn groen; `go test ./internal/packs/ -run TestInstructionBlocksWithinCeiling -v` toont de nieuwe tellingen.
- [ ] **Step 5: Commit**

```bash
git add internal/packs internal/packcontract tests/pack
git commit -m "test(pack): plafonds en vastgepinde woorden volgen de ingekorte blokken (#390)"
```

### Taak 4: Dogfood, na-meting, documentatie, changelog

**Files:**
- Modify: `AGENTS.md` (beheerde blokken), eventueel `.claude/skills/`, `.agents/skills/` (alleen als een skill een regel ontving)
- Modify: `docs/developer/planning/specs/2026-11-21-issue-390-beoordelingstabel.md` (sectie `## Na-meting`)
- Create: `changelog.d/issue-390-herbeoordeel-core-blok.md`

- [ ] **Step 1: Installeer de bron** — `go run ./cmd/lighthouse init --upgrade`, daarna nogmaals: de tweede run geeft exact `No changes.`
- [ ] **Step 2: Meet na** — `wc -w AGENTS.md CLAUDE.md` is lager dan 1207; `go run ./cmd/lighthouse check-instructions` is groen. Zet voor en na (installatie én de vijf bronblokken) in `## Na-meting`.
- [ ] **Step 3: Documentatie** — werk elke doc of skill bij die een geschrapte of verplaatste regel noemde (`grep -rn '<geschrapte zin>' docs pack`); de budgetregel in `pack/core/skills/place-instruction/SKILL.md:245-247` alleen als de plafonds uit taak 3 daar genoemd staan.
- [ ] **Step 4: Changelogfragment** met frontmatter `type: internal` (zoals `changelog.d/issue-366-blackbox-tests.md`) en één Nederlandse zin: het core-blok en de packinstructies zijn ingekort (woordtelling 1207 naar N).
- [ ] **Step 5: Volledige poort** — het `[test_gate] command` uit `lighthouse.toml:44` en `go run ./cmd/lighthouse imports check` zijn groen.
- [ ] **Step 6: Commit**

```bash
git add AGENTS.md .claude .agents docs changelog.d
git commit -m "chore(pack): geïnstalleerde kopie na herbeoordeling core-blok (#390)"
```

## Zelfreview

- AC1 en AC2 → taak 1; AC3 → taak 1 (kolom Nadruk) en 2 (nadruk eruit); AC4 → taak 2 stap 3, taak 4 stap 2; AC5 → taak 4 stap 1. DoD *Gereviewd* → `/lighthouse-review-plan` en de PR; *Documentatie* → taak 4 stap 3; *keten loopt door* → taak 4 stap 5 plus de controller-run van deze issue zelf.
- Open voor de uitvoerder: of het skilllijstje blijft (Review Focus 2); dat beslist de tabel, niet dit plan.
