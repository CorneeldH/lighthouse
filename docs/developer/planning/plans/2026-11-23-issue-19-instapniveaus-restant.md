# Instapniveaus, restant (AC 1, 5, 6) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** De drie open acceptatiecriteria van #19 afbouwen na #532: een test die bewijst dat een niveau geen permissies raakt (AC 6), een gebruikersdocument met het waarom per niveau (AC 1) en in `docs/install.md` wanneer je naar het volgende niveau gaat (AC 5).

**Architecture:** Alleen een test en documentatie; het mechanisme (`pack/core/levels.toml`, `internal/initcmd/levels.go`) is af en blijft ongemoeid. De test draait `init` per niveau en vergelijkt de geschreven `permissions.allow`. Het document `docs/levels.md` is Engels en volgt de ladder-tabel van de spec.

**Tech Stack:** Go (`testing`, `testing/fstest`), Markdown.

**Spec:** [2026-11-22-issue-19-instapniveaus-design.md](../specs/2026-11-22-issue-19-instapniveaus-design.md) (ladder r.122-136, permissions r.234)

## Global Constraints

- Artefacten (plan, commitberichten) zijn Nederlands; `docs/install.md` en het nieuwe `docs/levels.md` zijn Engels, zoals de installatiegids (AC 5 zegt "Engels").
- Testcode en commentaar onder `internal/**` zijn Engels (`code_language_paths`).
- Links in `.md` zijn relatief en wijzen naar een bestaand doel (`md-link-check`).
- Een niveau zet nooit een kernregel uit (`main-commit-gate`, `worktree-check`, `pr-create-gate`) en belooft dat ook niet; alleen een override mag dat, met waarschuwing (`docs/install.md:100-102`).
- Een niveau zegt niet dat het volgende niveau "binnen bereik" is (`lighthouse level` doet dat ook niet, `changelog.d/issue-19-instapniveaus-level.md`).
- Geen wijziging onder `pack/`, dus geen `init --upgrade`-dogfood nodig.
- Vóór elke commit: `[test_gate] command` uit `lighthouse.toml` en `lighthouse imports check`.

## Review Focus

- Een niveau dat `permissions.allow` van de repo zelf (`Bash(ls)`) wist of dubbelt: de test zet een bestaande regel en eist dat die blijft en niet vermenigvuldigt.
- `init --upgrade --level standard` na `light`: de permissies zijn na de overgang gelijk aan die vooraf (geen regel erbij of weg).
- Een niveau dat de permission-unit van core laat vallen doordat `Component` hem niet als base ziet: de test leest de echte `pack/core/permissions.json`, geen eigen fixturetekst, zodat een lege fixture hem niet tautologisch groen maakt.

---

### Task 1: Test dat geen niveau permissies raakt (AC 6)

**Files:**
- Modify: `internal/initcmd/levels_test.go` (nieuwe testfunctie na `TestBlockingNeverGainsWorktreeCheck`)
- Test: `internal/initcmd/levels_test.go`

**Interfaces:**
- Consumes: `levelPacks() fstest.MapFS`, `initLevels(t, root, tomlText string, mod func(*Options)) result`, `newRepo`, `write`, `read`, `file` (alle in `internal/initcmd`); echte `../../pack/core/permissions.json` (`{"allow": [...]}`).
- Produces: `TestLevelsNeverChangePermissions` (geen later gebruik).

- [ ] **Step 1: Schrijf de test `TestLevelsNeverChangePermissions`** in `internal/initcmd/levels_test.go`, met een commentaarregel erboven in het patroon van de buren: `// Red if a level adds, drops or reorders a permissions.allow rule (spec: permissions are the same at every level).` Opzet: lees `../../pack/core/permissions.json` en parse `allow` (`encoding/json`, `[]string`); faal als die lijst leeg is. Voor elk niveau in `light`, `standard`, `strict` (subtest per niveau): `root := newRepo(t)`, schrijf `.claude/settings.json` met `permissions.allow` = `["Bash(ls)"]` (patroon `projectSettings`, `initcmd_test.go:~699`), draai `initLevels(t, root, "level = \""+level+"\"\n", mod)` waarbij `mod` `o.Packs` uitbreidt met `core/permissions.json` (de echte bytes, zet in de `levelPacks()`-map). Parse daarna `.claude/settings.json` en eis dat `permissions.allow` exact gelijk is aan `["Bash(ls)"] + <de echte allow-lijst>` (volgorde zoals `settings.ApplyAllow` die schrijft; lees die volgorde uit de eerste run en vergelijk de andere niveaus daarmee). Vergelijk de drie niveaus onderling met `slices.Equal`. Voeg een extra subtest `upgrade` toe: één repo, zelfde setup (bestaande regel `Bash(ls)`, echte permissions-bytes in de packs), `initLevels` op `light`, lees `permissions.allow`, draai daarna dezelfde `init --upgrade --level standard` (en daarna `strict`) via het patroon van `TestUpgradeLevelFlagSwitchesAndRemovesUnits` (`levels_test.go:~435`) met dezelfde packs, en eis dat `permissions.allow` na elke overgang byte-gelijk is aan die vooraf. `TestUpgradeLevelFlagSwitchesAndRemovesUnits` zelf dekt dit niet: `levelPacks()` bevat geen `permissions.json`.
- [ ] **Step 2: Draai en verwacht groen, daarna de mutatiecheck:** `cd <worktree> && go test ./internal/initcmd/ -run TestLevelsNeverChangePermissions -count=1` geeft PASS. Gebruik `-v` en eis de regel `--- PASS: TestLevelsNeverChangePermissions`. Mutatiecheck: voeg tijdelijk bovenaan de lus in `selectUnits` (`internal/initcmd/levels.go`) `if u.Kind == packs.KindPermission { continue }` toe en zie dat de test op elk niveau faalt (kernregels ontbreken). Draai alleen die mutatie terug met `git checkout -- internal/initcmd/levels.go` (NOOIT `git checkout -- internal/`: dat wist ook de nog niet gecommitte test) en bevestig opnieuw de `--- PASS`-regel.
- [ ] **Step 3: Draai `go test ./internal/initcmd/ ./internal/packs/ -count=1`, `[test_gate] command` en `lighthouse imports check`;** verwacht alles groen.
- [ ] **Step 4: Commit**

```bash
git add internal/initcmd/levels_test.go
git commit -m "test(init): geen instapniveau raakt permissions.allow (#19)"
```

### Task 2: Gebruikersdocument en opschaalcriterium (AC 1 en 5)

**Files:**
- Create: `docs/levels.md` (Engels)
- Modify: `docs/install.md` (sectie *Start small*, r.78-102: link naar `levels.md` en een nieuwe bullet *When to scale up*)
- Create: `changelog.d/issue-19-instapniveaus-doc.md`

**Interfaces:**
- Consumes: de ladder-tabel in de spec (r.122-136) en `pack/*/levels.toml` (core, go, python, node, shell, expo) als bron van de standen.
- Produces: `docs/levels.md` met het anker `## What each level skips and why`, waar `install.md` naar linkt.

- [ ] **Step 1: Schrijf `docs/levels.md`.** Kop `# Setup levels`, korte inleiding (drie niveaus, `lighthouse level` leest, `lighthouse init --upgrade --level <naam>` schakelt) met een relatieve link terug naar de sectie *Start small* van `install.md` (zelfde map, dus `install.md#start-small`). Sectie `## What each level skips and why`: één tabel per niveau, of één tabel met kolommen `Component`, `light`, `standard`, `strict`, `Why it is off or logged on light`, met de rijen van de spec-ladder vertaald naar het Engels (kernregels altijd `on`; `commit-test-gate` off op `light`; `md-link-check` en `code-language-check` `log`; taalpakket-hooks, onderhoudsskills (`write-issue`, `fix-pr`, `audit-worktrees`, `audit-agent-config`, `test-standard`) en de review-skills `review-spec` en `review-plan` met hun reden; vlag `labels` uit/aan/aan). Eindig met een alinea die zegt dat permissies op elk niveau gelijk zijn en geen niveau een permissie- of securityvraag beantwoordt, en dat alleen een override een kernregel uitzet (met waarschuwing). Controleer elke stand tegen alle `pack/*/levels.toml` voordat je hem opschrijft; wijkt een bestand af van de spec, geldt het bestand. Neem expliciet op: `skill:map-planned` (off op light, on daarna; staat niet in de spec-ladder) en dat de `*-imports-gate`-hooks (go, python, node) op elk niveau `on` zijn, terwijl alleen lint/format-hooks van taalpakketten (bv. `gofmt-check`, `go-lint-gate`, `skill:go-standard`) op light off staan; zeg dus niet dat alle taalpakket-hooks op light off zijn.
- [ ] **Step 2: Pas `docs/install.md` aan.** In *Start small*: na de regel over `lighthouse level` een verwijzing de zin "See Setup levels (`levels.md`, als markdownlink) for what each level skips and why." en een bullet `- **When to scale up.**` die criteria geeft, zonder te beloven dat het volgende niveau "binnen bereik" is: naar `standard` zodra de eerste PR's zonder het bijhouden van issues of reviews pijn doen of je wilt dat `commit-test-gate` na elke commit draait; naar `strict` zodra je de tweede-modelfamilie-reviews van spec en plan wilt (die vragen Codex). Koppel het `log`-criterium (`lighthouse level` toont voor `md-link-check` en `code-language-check` weinig meldingen) alleen aan de stap `light` naar `standard`: op `standard` staat geen component meer op `log`. Houd de bullet kort (3-4 zinnen) in dezelfde toon als de buren.
- [ ] **Step 3: Schrijf het changelog-fragment** `changelog.d/issue-19-instapniveaus-doc.md` in het formaat van de buren (`---` / `type: added` / `---` en één Engelse regel): `docs/levels.md` explains per setup level what it skips and why, and the install guide says when to scale up to the next level.
- [ ] **Step 4: Controleer links en lint:** laat de `PostToolUse`-hook `md-link-check` het doen bij het schrijven, of pipe per bestand een hook-payload (`{"cwd":"<worktree>","tool_input":{"file_path":"<abs pad>"}}`) op stdin naar `.claude/hooks/lighthouse/run.sh md-link-check` (het script neemt geen bestandsargumenten) en `[test_gate] command`; verwacht geen blokkade. Controleer met `grep -n "levels.md" docs/install.md` dat de link er staat.
- [ ] **Step 5: Commit**

```bash
git add docs/levels.md docs/install.md changelog.d/issue-19-instapniveaus-doc.md
git commit -m "docs(levels): waarom per instapniveau en wanneer opschalen (#19)"
```

## Self-review

- **Spec-dekking:** AC 1 → Task 2 stap 1; AC 5 → Task 2 stap 2; AC 6 → Task 1. AC 2, 3, 4, 7 zijn al aantoonbaar klaar (after-merge check van #532).
- **Review Focus:** regel 1 en 3 zitten in Task 1 stap 1 (bestaande regel `Bash(ls)`, echte permissions-bytes); regel 2 wordt gedekt door de subtest `upgrade` in Task 1 stap 1 (`TestUpgradeLevelFlagSwitchesAndRemovesUnits` bevat geen permissions en dekt het niet).
- **Typeconsistentie:** alle genoemde helpers bestaan al in `internal/initcmd`.
