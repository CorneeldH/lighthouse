# Procesverslag van lighthouse zelf Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Het dagverslag van #44 staat aan in de `lighthouse.toml` van deze repo, het verslag van lighthouse zelf heeft een vaste plek onder `docs/developer/`, en die plek en de roadmap verwijzen naar elkaar.

**Architecture:** Alleen configuratie en documentatie; er komt geen code bij. `[report]` krijgt `sink = "file"` met `dir = "docs/developer/verslag"`, zodat de generator naar `docs/developer/verslag/<periode>/<label>.md` schrijft. Een met de hand geschreven `docs/developer/verslag/README.md` is de ingang van het procesverslag en linkt naar de roadmap; die roadmap is `## Roadmap` in `docs/developer/strategie.md` en linkt terug.

**Tech Stack:** `lighthouse.toml`, markdown, Go-test in `internal/repoconfig`.

**Spec:** geen aparte spec; de acceptatiecriteria van #128 en [docs/report.md](../../../report.md) (ontwerp van #44 in [de spec](../specs/2026-11-16-issue-44-verslag-design.md)).

## Global Constraints

- `[project] language = "nl"`: de documenten zijn Nederlands; het changelog-fragment en alles onder `internal/**` is Engels.
- Een link in een `.md` is relatief en wijst naar een bestaand doel (`md-link-check`).
- `dir` is relatief aan de repo-root en binnen de repo (docs/report.md, *Aanzetten*).
- Geen roadmapdocument: `areas/meta.md` N2 sluit dat uit. De roadmap blijft de milestones in rangorde op de forge.
- Elke PR voegt een fragment toe aan `changelog.d/`.

## Review Focus

- Een foute `[report]` laat `Load` niet falen (docs/report.md): een test op de eigen `lighthouse.toml` moet `ReportErr == nil` eisen, anders blijft een typefout onopgemerkt tot `lighthouse report` draait.
- `dir` zonder bestaande map: de generator maakt `docs/developer/verslag/day/` zelf aan; de map hoeft er niet te zijn. De README staat er wel, dus de map bestaat.
- #102 is geen bestand in de repo (de kaart/systeemkaart); AC3 wordt daarom gelezen als de roadmap van `strategie.md`. Dit staat in de README zelf vastgelegd.
- Het commando commit niets en er is geen `[[schedule]]` voor het verslag; de README zegt dat een mens of een latere taak het bestand commit.

---

### Task 1: Dagverslag aan in `lighthouse.toml`

**Files:**
- Modify: `lighthouse.toml` (nieuwe tabel `[report]`, vóór `[[schedule]]`)
- Test: `internal/repoconfig/repo_own_report_test.go`

**Interfaces:**
- Consumes: `repoconfig.Load(path string) (*Repo, error)` met `Report` en `ReportErr` (internal/repoconfig/repoconfig.go:37-40); de helper `repoRoot(t)` uit `internal/repopins/repo_test.go:15` is een voorbeeld voor het vinden van de repo-root, kopieer het patroon (loop omhoog tot `go.mod`).
- Produces: `[report]` met `enabled = true`, `sink = "file"`, `dir = "docs/developer/verslag"`.

- [ ] **Step 1: Schrijf de falende test** `TestOwnLighthouseTomlEnablesReport` in `internal/repoconfig/repo_own_report_test.go`: laadt `<repo-root>/lighthouse.toml` en eist `ReportErr == nil` en `Report == Report{Enabled: true, Sink: "file", Dir: "docs/developer/verslag"}`. Vang een `Load`-fout met `t.Fatalf`.
- [ ] **Step 2: Draai** `go test ./internal/repoconfig/ -run TestOwnLighthouseTomlEnablesReport` — verwacht FAIL (`Enabled` false).
- [ ] **Step 3: Voeg de tabel toe** in `lighthouse.toml`, met een commentaarregel erboven in de stijl van de andere tabellen: `# Het procesverslag van lighthouse zelf (docs/report.md, docs/developer/verslag/README.md).`, dan de drie sleutels hierboven.
- [ ] **Step 4: Draai** dezelfde test — verwacht PASS. Draai ook `node --test "docs/site/test/*.test.mjs"` (de gate doet dat bij een wijziging in `lighthouse.toml`) — verwacht groen.
- [ ] **Step 5: Commit** `feat(report): dagverslag aan in de eigen lighthouse.toml (#128)`; draai eerst `lighthouse imports check`.

### Task 2: Vaste plek en kruislinks

**Files:**
- Create: `docs/developer/verslag/README.md`
- Modify: `docs/developer/strategie.md` (sectie `## Roadmap`, na regel 78)
- Modify: `docs/report.md` (één zin onder *Aanzetten*)

**Interfaces:**
- Consumes: de `dir` uit Task 1.
- Produces: het anker `docs/developer/strategie.md#roadmap` en de README als ingang.

- [ ] **Step 1: Schrijf `docs/developer/verslag/README.md`** (Nederlands) met: wat dit is (laag 2: het procesverslag van lighthouse zelf, boven de tickets; laag 1 is de tool, laag 3 de showcase, die buiten #128 valt: #89 en #93); waar de gegenereerde verslagen staan (`day/`, `week/`, `month/` onder deze map, `<label>.md`) en hoe ze komen (`lighthouse report day`; het commando commit niets, een mens commit het bestand); een link naar `de roadmap` (`../strategie.md#roadmap`) met de zin dat de roadmap de milestones in rangorde op de forge zijn en er bewust geen roadmapdocument is (N2 in `../areas/meta.md`); een link naar `lighthouse report` (`../../report.md`).
- [ ] **Step 2: Link terug.** In `docs/developer/strategie.md` onder `## Roadmap` een zin: het verslag van wat er gebeurd is staat bij `het procesverslag` (`verslag/README.md`).
- [ ] **Step 3: Link vanuit `docs/report.md`.** Onder *Aanzetten* één zin: de eigen configuratie van lighthouse staat in `lighthouse.toml` en schrijft naar `het procesverslag` (`developer/verslag/README.md`).
- [ ] **Step 4: Verifieer de links.** De hook `md-link-check` blokkeert een kapotte link bij het schrijven; draai daarnaast `go run ./cmd/lighthouse init --upgrade` — verwacht `No changes.`
- [ ] **Step 5: Verifieer het commando.** `go run ./cmd/lighthouse report day --repo .` met `GH_TOKEN` — verwacht een bestand `docs/developer/verslag/day/<datum>.md`. Is er geen token, noteer dan `niet gemeten: geen token` in de PR; commit het gegenereerde bestand niet.
- [ ] **Step 6: Commit** `docs(report): vaste plek voor het procesverslag met kruislinks naar de roadmap (#128)`.

### Task 3: Changelog

**Files:**
- Create: `changelog.d/issue-128-procesverslag.md`

- [ ] **Step 1: Schrijf het fragment**, frontmatter `type: added`, één Engelse zin: `This repository now enables its own daily report and gives the process report a fixed place under docs/developer/verslag.`
- [ ] **Step 2: Commit** `docs(changelog): fragment voor #128`.

## Self-review

- AC1 → Task 1; AC2 → Task 1 (`dir`) en Task 2 (README); AC3 → Task 2 (beide richtingen, via `strategie.md#roadmap`).
- Open punt voor de reviewer: #102 is geen repo-bestand; de keuze om de roadmap als `strategie.md#roadmap` te lezen staat in Review Focus en in de README.
