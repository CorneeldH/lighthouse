# Repokaart: stand Upgrade — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** De repokaart (`docs/site/repo.html`) krijgt een vierde stand **Upgrade** die per rij toont wat `init --upgrade` doet en waar `--force` nodig is, bewaakt door een test.

**Architecture:** Data in `docs/site/data/repo.json` (`upgrade`, `force` per rij, `upgradeNote` bovenaan); `repoEntries` in `docs/site/map.js` geeft ze door, `renderRepo` toont ze alleen in de stand Upgrade. De test leidt `force` af uit `internal/initcmd/testdata/places.golden`.

**Tech Stack:** vanilla JS (ES-modules), CSS, `node --test`.

**Spec:** [2026-11-22-issue-192-repokaart-upgrade-design.md](../specs/2026-11-22-issue-192-repokaart-upgrade-design.md)

## Global Constraints

- Teksten op de site zijn Engels; changelog-fragment en artefacten Nederlands.
- Geen wijziging aan `init`/`reconcile`; geen legenda, geen zones, geen nieuwe rijen (`CLAUDE.md` en `.lighthouse/lib/` blijven van de kaart).
- `force` is `true` precies als de soorten van de `place` in `places.golden` een van `file`, `block`, `hook`, `permission` bevatten; rij zonder `place` (`host.toml`) en soort `init` (`lighthouse.toml`, `manifest.toml`) is `false`.
- Chip `--force` heeft voor schermlezers de tekst "refuses while you edited or deleted it; --force overwrites".
- Cache-busting `?v=` is één token over alle `.js`/`.css`: na wijziging `node docs/site/test/version.mjs` draaien.
- Voor elke commit: `[test_gate] command` en `lighthouse imports check`.

## Review Focus

- Bestand van het pack erbij in `places.golden` zonder `force`-waarde → test moet rood (task 1: `forceProblems` leidt af uit golden, geen hardcoded lijst).
- Rij zonder `upgrade`-tekst of met lege/witruimte-tekst → test rood (task 1).
- Stand Upgrade dimt niets; terugschakelen naar andere stand verbergt tekst, chip en regel weer (task 2).
- `upgradeNote` ontbreekt in data → `renderRepo` mag niet crashen of "undefined" tonen (task 2).
- Smal scherm: tekst valt onder de rij, geen horizontale scroll (task 2, handmatig in de browser).

---

### Task 1: Data en bewaking

**Files:**
- Modify: `docs/site/data/repo.json`
- Modify: `docs/site/test/repo.test.mjs`

**Interfaces:**
- Produces: elke `rows[]` in `repo.json` heeft `upgrade: string` (niet leeg) en `force: boolean`; top-level `upgradeNote: string`; `subtitle` noemt de upgrade.

- [ ] **Step 1: Schrijf de falende tests** in `repo.test.mjs`
  - `forceProblems(data, gold): string[]` (nieuw, naast `actProblems`): per rij met `r.place in gold` is `want = [...kinds].some(k => FORCE_KINDS.has(k))`, `FORCE_KINDS = new Set(["file","block","hook","permission"])`; rij zonder `place` wil `false`. Probleem: `` `${r.id}: force ${r.force}, expected ${want}` ``. Gesorteerd teruggeven.
  - `upgradeProblems(data)`: `` `${r.id}: no upgrade text` `` voor elke rij waarvan `typeof r.upgrade !== "string" || !r.upgrade.trim()`; plus `"no upgradeNote"` als `data.upgradeNote` leeg is.
  - Tests `"force matches the golden"` en `"every row says what upgrade does"` die `[]` verwachten.
  - Cases in de `cases`-tabel: `lighthouse-toml` `force = true` → `["lighthouse-toml: force true, expected false"]`; idem `manifest`; `workflows` `force = false` → `["workflows: force false, expected true"]`; `claude-settings` `force = false` → `["claude-settings: force false, expected true"]`; `delete row(data,"agents-md").upgrade` → `["agents-md: no upgrade text"]`.
- [ ] **Step 2: Draai `cd docs/site && node --test test/repo.test.mjs`** — verwacht FAIL (`force true, expected …`/`no upgrade text` op alle rijen).
- [ ] **Step 3: Vul `repo.json`.** Per rij `upgrade` en `force` volgens de schets in de spec (sectie "Schets"; Engelse teksten herleidbaar tot "Wat de code doet"): `force: true` voor `agents-md`, `claude-hooks`, `claude-skills`, `claude-agents`, `claude-settings`, `agents-skills`, `lighthouse-scripts`, `headings`, `pr-template`, `workflows`; `false` voor `lighthouse-toml`, `manifest`, `host-toml`. `upgradeNote`: `"One edited part stops the whole upgrade; --force overwrites every edited part at once."` `subtitle`: noemt ook de upgrade, bv. `"Everything lighthouse init puts in your repo, the files of yours it edits, and what an upgrade does with each."`
- [ ] **Step 4: Pas het bestaande geval aan:** in `"entries refuse an unknown mode, kind and act"` gebruik `"bogus"` i.p.v. `"upgrade"` (`unknown mode: bogus`). De testnaam blijft exact `"entries refuse an unknown mode, kind and act"`; alleen het argument verandert.
- [ ] **Step 5: Draai de test** — verwacht PASS.
- [ ] **Step 6: Commit** (`feat(site): repokaart-data met upgrade en force (#192)`), `git add docs/site/data/repo.json docs/site/test/repo.test.mjs`.

### Task 2: Stand Upgrade in de renderer

**Files:**
- Modify: `docs/site/map.js` (`REPO_MODES`, `repoEntries`, `repoRow`, `renderRepo`)
- Modify: `docs/site/map.css` (bij `.repo-row`, regel ~1540)
- Modify: `docs/site/repo.html` (statische ondertitel regel 14, "Wait, but why?" regel 26)
- Modify: `docs/site/test/repo.test.mjs`
- Modify (gegenereerd): alle pagina's met `?v=` via `node docs/site/test/version.mjs`
- Create: `changelog.d/issue-192-repokaart-upgrade.md`

**Interfaces:**
- Consumes: velden uit task 1.
- Produces: `REPO_MODES` bevat `{ id: "upgrade", label: "Upgrade" }` als vierde; `repoEntries(data, mode)` rijen krijgen `upgrade: string` en `force: boolean`, en `dim` is `false` voor `mode === "upgrade"`; `repoEntries` bevat ook `upgradeNote` niet — `renderRepo` leest `data.upgradeNote`.

- [ ] **Step 1: Schrijf falende tests** in `repo.test.mjs`:
  - `"upgrade dims nothing"`: `entryRows("upgrade")` heeft geen `dim`.
  - `"rows carry the upgrade text"`: `entryRows("everything")[".claude/settings.json"]` heeft `force === true` en `upgrade` gelijk aan `row(load("repo.json"),"claude-settings").upgrade`; `["lighthouse.toml"].force === false`.
  - `"entries refuse an unknown mode, kind and act"` blijft (zelfde test als Task 1 Step 4, geen tweede test); `m.REPO_MODES.map(x => x.id)` deepEqual `["everything","adds","edits","upgrade"]`.
- [ ] **Step 2: Draai de test** — verwacht FAIL.
- [ ] **Step 3: `map.js`.** `REPO_MODES` + `upgrade`; `repoEntries`: rij-object krijgt `upgrade: row.upgrade`, `force: row.force`; `dim: mode !== "everything" && mode !== "upgrade" && row.act !== mode`. `repoRow(row)`: voeg `<span class="repo-upgrade" hidden>` toe met de tekst en, als `row.force`, BINNEN die span een `<code class="repo-force">--force</code>` (kind van `.repo-upgrade`, geen sibling, zodat `hidden` beide verbergt) met `.sr-only`-uitleg (spec-tekst, zie Global Constraints). `renderRepo`: `<p class="repo-upgrade-note" hidden>` met `data.upgradeNote ?? ""` boven de boom; `select(mode)` zet `hidden` op de `.repo-upgrade` en de regel naar `mode !== "upgrade"`. Wijzig `aria-label` van de schakelaar niet.
- [ ] **Step 4: `map.css`.** `.repo-upgrade` dim tekstkleur, `.repo-force` chip-opmaak naar bestaand chip/code-patroon; onder de bestaande telefoon-`@media` (regel ~1622): `.repo-upgrade` op eigen regel (`display:block`), geen horizontale scroll.
- [ ] **Step 5: `repo.html`.** Ondertitel gelijk aan `subtitle` uit task 1; "Wait, but why?" noemt ook wat een upgrade met de gebieden doet (één zin).
- [ ] **Step 6: `node docs/site/test/version.mjs`**, daarna `cd docs/site && node --test test/` — verwacht alles PASS (incl. `assets.test.mjs`).
- [ ] **Step 7: Browsercheck** (`go run ./tools/servedir docs/site`): stand Upgrade toont tekst, chips en regel; andere standen niet; ~375px breed zonder horizontale scroll; achteraf server stoppen. Is er geen browser beschikbaar: meld in de PR dat deze stap niet is geverifieerd, vink hem niet af.
- [ ] **Step 8: Changelog-fragment** `changelog.d/issue-192-repokaart-upgrade.md`, frontmatter volgens `changelog.d/issue-369-site-node-test.md` (`type:` bepaal uit `docs/releasing.md`), één Nederlandse zin: de repokaart heeft een stand Upgrade die per rij toont wat `init --upgrade` doet en waar `--force` nodig is.
- [ ] **Step 9: Commit** (`feat(site): stand Upgrade op de repokaart (#192)`) met alle gewijzigde bestanden.

## Self-review

- Spec-dekking: data+`upgradeNote`+`subtitle` (T1), `REPO_MODES`/`repoEntries`/`renderRepo`/css (T2), `repo.html` + versietoken (T2), tests beide richtingen (T1), changelog (T2). Geen pack-wijziging, dus geen `init --upgrade`-dogfood nodig.
- Types: `force`/`upgrade`/`upgradeNote` overal dezelfde namen.
