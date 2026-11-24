# Controller-knoppen in de instapniveaus Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `spec_review` en `plan_review` in `[controller]` krijgen hun standaard uit het geïnstalleerde niveau, zodat een handoff naar een uitgezette review-skill wordt overgeslagen in plaats van teruggegeven met `reason_skill_missing` (issue #255).

**Architecture:** `repoconfig.LoadFile` leest `.lighthouse/components.toml` via `levels.ReadInstalled` alleen wanneer een review-sleutel in `[controller]` ontbreekt, en zet de knop op "skill is niet `off`". Een onleesbaar bestand komt in `Repo.ControllerErr`, dat `Dispatchable` meeneemt. `bij_rondegrens` krijgt `"doorgaan"` als standaard. Daarna volgen de docs en een test die de echte `pack/core/levels.toml` aan de docs-tabel vastlegt.

**Tech Stack:** Go (`internal/repoconfig`, `internal/levels`, `internal/dispatch`), Markdown-docs.

**Spec:** [2026-11-24-controller-knoppen-instapniveaus-design.md](../specs/2026-11-24-controller-knoppen-instapniveaus-design.md)

## Global Constraints

- Volgorde per knop, eerste die er is wint: losse sleutel in `[controller]` → `skill:review-spec` (voor `spec_review`) of `skill:review-plan` (voor `plan_review`) is niet `off` → `true`.
- Geen `components.toml`: alles aan (`levels.ReadInstalled` geeft `found == false`), dus `true`.
- `bij_rondegrens`: `"doorgaan"` op elk niveau; een leeg veld wordt `"doorgaan"`. Alleen `"vraag"` verandert gedrag.
- `components.toml` wordt alleen gelezen als `[controller]` een tabel is (`md.Type("controller") == "Hash"`) én `spec_review` of `plan_review` ontbreekt.
- Een onleesbare `components.toml` laat `Load` slagen; de fout gaat in `Repo.ControllerErr` met het volle pad `filepath.Join(dir, levels.InstalledPath)`. Nooit stil terugvallen op `true`.
- Geen nieuwe knop, geen `plekken`, geen auto-antwoordknop, geen waarschuwing bij een losse sleutel die botst met het niveau.
- Resultaat per niveau, met de pack van nu: `light` en `standard` → `spec_review=false`, `plan_review=false`; `strict` → `true`, `true`; `bij_rondegrens = "doorgaan"` overal.
- Artefacten (docs, commitberichten, testnamen in commentaar) Nederlands; identifiers en code-commentaar zoals de omliggende code. `[project] code_language` is niet nodig hier: volg de stijl van het bestand.
- Voor elke commit: `[test_gate] command` uit `lighthouse.toml` en `lighthouse imports check`.
- Elke PR krijgt een fragment in `changelog.d/` (Taak 3).

## Review Focus

- `[controller]` ontbreekt maar `components.toml` is er: `Controller` blijft `nil`, `components.toml` wordt niet gelezen, `Load` slaagt. Test in Taak 1.
- `components.toml` met een syntaxfout en beide review-sleutels los gezet: niet gelezen, dus geen `ControllerErr`. Test in Taak 1.
- `components.toml` met syntaxfout en één review-sleutel ontbrekend: `Load` slaagt, `ControllerErr` noemt het volle pad, `Dispatchable` is rood, en de knoppen staan niet stil op `true` voor de berekening telt (de fout is de melding). Test in Taak 1.
- `[components]`-override in `lighthouse.toml` die `skill:review-spec` aanzet op `light`: `components.toml` toont `on` (niet `off`), dus `spec_review = true`. Test in Taak 1.
- Lege `bij_rondegrens` met `[controller]` aanwezig: wordt `"doorgaan"`; bestaande tests die `""` verwachten gaan mee. Test in Taak 1.

---

### Task 1: Knoppen uit het niveau in `repoconfig`

**Files:**
- Modify: `internal/repoconfig/repoconfig.go` (`Repo`, `Controller`, `LoadFile`, `Dispatchable`)
- Modify: `internal/repoconfig/repoconfig_test.go` (bestaande `AtRoundLimit`-verwachtingen; nieuwe tests)

**Interfaces:**
- Produces: `Repo.ControllerErr error` (veld). `Controller.SpecReview`, `Controller.PlanReview` en `Controller.AtRoundLimit` houden hun type; alleen de standaarden veranderen. `Dispatchable(r)` bevat `r.ControllerErr` in zijn gejoinde fout.
- Consumes: `levels.ReadInstalled(root string) (levels.Installed, bool, error)`, `levels.InstalledPath`, `Installed.Stand(component string) levels.Stand`, `levels.StandOff`.

- [ ] **Step 1: Schrijf de falende tests in `internal/repoconfig/repoconfig_test.go`**

Hulpfunctie `writeInstalled(t, dir, components map[string]levels.Stand)` die `.lighthouse/components.toml` schrijft met `levels.Installed{Level: "x", Since: "2026-01-01T00:00:00Z", Components: …}.Marshal()`; de `write`-helper van het bestand maakt het `lighthouse.toml` in een tempdir, gebruik zijn dir. Tests:

- `TestLoadReviewKnobsFollowInstalled` (tabeltest, `[controller]` zonder review-sleutels): `skill:review-spec`/`skill:review-plan` beide `off` → `SpecReview=false, PlanReview=false`; alleen plan `off` → `true, false`; beide `on` → `true, true`; geen `components.toml` → `true, true`; elke rij ook `AtRoundLimit == "doorgaan"`.
- `TestLoadReviewKeyBeatsLevel`: `light`-componenten + `spec_review = true` → `SpecReview=true, PlanReview=false`; `strict` (geen off) + `plan_review = false` → `SpecReview=true, PlanReview=false`.
- `TestLoadReviewOverrideOn`: `components.toml` zonder de twee review-skills op `off` (zoals na een `[components]`-override naar `on`) → `true, true`.
- `TestLoadBrokenComponentsSetsControllerErr`: `components.toml` bevat `level = [`; `[controller]` zonder review-sleutels → `Load` geeft `err == nil`, `r.ControllerErr != nil`, `strings.Contains(r.ControllerErr.Error(), filepath.Join(dir, ".lighthouse/components.toml"))`, en `Dispatchable(r)` (een repo met geldig board en `usage_guard`, zoals de bestaande dispatchable-tests) bevat die tekst.
- `TestLoadComponentsNotReadWhenNotNeeded`: kapotte `components.toml` én (a) geen `[controller]`, (b) `[controller]` met `spec_review` en `plan_review` beide gezet → `ControllerErr == nil`, `Dispatchable` geeft niets over `components.toml`.
- Pas `TestLoad`/`TestLoadDefaults` aan: lege `bij_rondegrens` → `"doorgaan"`; een expliciete `"stop"` blijft `"stop"`.

- [ ] **Step 2: Run de tests, controleer dat ze falen**

Run: `go test ./internal/repoconfig -run 'ReviewKnobs|ReviewKey|ReviewOverride|BrokenComponents|NotReadWhenNotNeeded|LoadDefaults' -v`
Expected: FAIL (`ControllerErr` bestaat niet / knoppen blijven `true`).

- [ ] **Step 3: Implementeer in `internal/repoconfig/repoconfig.go`**

- Voeg `ControllerErr error` toe aan `Repo`, naar het patroon van `TriageErr` (commentaar: onleesbare `components.toml`, `Load` faalt er niet op, `Dispatchable` meldt het).
- Importeer `github.com/CorneeldH/lighthouse/internal/levels`.
- In het `[controller]`-blok van `LoadFile`: als `c.SpecReview == nil || c.PlanReview == nil`, roep `levels.ReadInstalled(dir)` aan. Bij een fout: `r.ControllerErr = fmt.Errorf("%s: %w", filepath.Join(dir, levels.InstalledPath), err)` (let op dat `ReadInstalled` zelf al `InstalledPath` in zijn fout zet; het volle pad moet er wél in staan, dus wikkel zo dat het pad niet dubbel leest maar het volle pad voorkomt) en laat de ontbrekende knoppen `true`. Anders: ontbrekende `SpecReview` = `installed.Stand("skill:review-spec") != levels.StandOff`, ontbrekende `PlanReview` idem met `skill:review-plan`. Een expliciete sleutel wint altijd.
- `AtRoundLimit: cmp.Or(c.AtRoundLimit, "doorgaan")`.
- Werk de doc-comment van `Controller` bij (defaults komen uit het niveau, zie spec).
- In `Dispatchable`: `if r.ControllerErr != nil { errs = append(errs, r.ControllerErr) }`.

- [ ] **Step 4: Run de tests, controleer dat ze slagen**

Run: `go test ./internal/repoconfig ./internal/dispatch ./internal/cli -race`
Expected: PASS. Faalt een test op `AtRoundLimit == ""`, pas zijn verwachting aan naar `"doorgaan"` (spec, *Aan de bouwer*).

- [ ] **Step 5: Controleer imports en commit**

Run: `lighthouse imports check`
Expected: geen fout. Zo wel (`repoconfig` mag `levels` niet importeren), vervang de import door een kleine lezer van `components.toml` in `repoconfig` met dezelfde gedragstests; geen gedragsverschil.

```bash
git add internal/repoconfig
git commit -m "feat(repoconfig): review-knoppen volgen het geïnstalleerde niveau (#255)"
```

---

### Task 2: De dispatcher slaat de review over op een `light`-repo, en de pack legt de tabel vast

**Files:**
- Create: `internal/repoconfig/levels_pack_test.go`
- Modify: `internal/dispatch/judge_test.go` (nieuwe test naast `TestReviewSkip`)

**Interfaces:**
- Consumes: `repoconfig.Load(dir string) (Repo, error)`, `dispatch.ReviewSkip(next string, c repoconfig.Controller) (command, skipKey string)`, `levels.Parse`, `levels.Resolve`, `Installed.Marshal`.

- [ ] **Step 1: Schrijf de falende tests**

`internal/repoconfig/levels_pack_test.go`, `TestPackLevelsDeriveControllerKnobs`: leest `../../pack/core/levels.toml`, voor elk niveau in `File.Order` (`light`, `standard`, `strict`) `levels.Parse` + `levels.Resolve([]levels.File{f}, level, nil)`, schrijft `levels.Installed{Level: level, Since: "2026-01-01T00:00:00Z", Components: resolved.Components}.Marshal()` naar `<tmp>/.lighthouse/components.toml` naast een minimaal `lighthouse.toml` (`[forge] repo = "o/r"` + `[controller]`), `Load`, en vergelijkt met de tabel uit de spec: `light` `false,false,"doorgaan"`; `standard` `false,false,"doorgaan"`; `strict` `true,true,"doorgaan"`. Foutmelding noemt: "docs/levels.md, sectie Controller-knoppen per niveau, moet mee".

`internal/dispatch/judge_test.go`, `TestReviewSkipOnLightRepo`: schrijf in een tempdir een `lighthouse.toml` (`[forge] repo = "o/r"`, `[controller]`) en een `.lighthouse/components.toml` met `"skill:review-spec" = "off"` en `"skill:review-plan" = "off"`; `Load`; `ReviewSkip("/lighthouse-review-spec /a/b.md", *r.Controller)` geeft `"/superpowers:writing-plans /a/b.md"` en key `"skip_spec_review"`; hetzelfde voor `/lighthouse-review-plan /a/p.md` → `"/superpowers:subagent-driven-development /a/p.md"`, `"skip_plan_review"`.

- [ ] **Step 2: Run, controleer dat ze falen of slagen**

Run: `go test ./internal/repoconfig ./internal/dispatch -run 'PackLevels|OnLightRepo' -v`
Expected: PASS na Taak 1 (de gedragsverandering zit daar). Controleer dat `TestPackLevelsDeriveControllerKnobs` rood wordt wanneer je tijdelijk `"skill:review-spec" = "off"` uit `[standard.components]` haalt, en zet het terug.

- [ ] **Step 3: Commit**

```bash
git add internal/repoconfig/levels_pack_test.go internal/dispatch/judge_test.go
git commit -m "test(repoconfig): niveaus uit de echte pack leggen de controller-knoppen vast (#255)"
```

---

### Task 3: Docs en changelog

**Files:**
- Modify: `docs/levels.md` (nieuwe sectie na *What each level skips and why*, en de permissiezin)
- Modify: `docs/tick.md` (*Vereisten per repo*, rond regel 1108)
- Create: `changelog.d/issue-255-controller-knoppen-niveaus.md`

- [ ] **Step 1: Schrijf `docs/levels.md`**

Voeg een sectie `## Controller-knoppen per niveau` toe (de rest van het bestand is Engels; `[project] language` is `nl` voor artefacten, volg de taal van `docs/levels.md`: Engels) met: de tabel *Resultaat per niveau* uit de spec (`light`, `standard`, `strict` × `spec_review`, `plan_review`, `bij_rondegrens`, met de *Waarom*-kolom en de `fix-pr`-opmerking voor `light`); de volgorde losse sleutel in `[controller]` → niveau (`skill:review-spec`/`skill:review-plan` niet `off`) → `true`; dat een repo zonder `components.toml` alles aan houdt; dat een losse `spec_review = true` op een niveau waar `skill:review-spec` uit staat de run terug laat geven met `reason_skill_missing`; waarom `plekken` (`slots` in `host.toml [dispatch]`) er niet in zit. Breid de zin over permissies uit: geen niveau beantwoordt ook een controller-vraag automatisch.

- [ ] **Step 2: Schrijf de regel in `docs/tick.md` en het fragment**

In *Vereisten per repo* één regel: de standaard van `spec_review` en `plan_review` komt uit het niveau, met een relatieve link naar `levels.md#controller-knoppen-per-niveau` (pas het anker aan op de gekozen kop). `changelog.d/issue-255-controller-knoppen-niveaus.md`, naar het formaat van `changelog.d/issue-555-zes-statussen-in-het-statusmodel-en-n.md`: `type: changed`, en een zin dat `spec_review` en `plan_review` hun standaard uit het niveau halen en `bij_rondegrens` standaard `"doorgaan"` is (#255).

- [ ] **Step 3: Verifieer en commit**

Run: `go test ./... 2>&1 | tail -5` en `go run ./cmd/lighthouse init --upgrade` (Expected: `No changes.`; zo niet, commit de bijgewerkte installatie mee).
Expected: tests PASS; de `md-link-check`-hook meldt geen kapotte links.

```bash
git add docs changelog.d
git commit -m "docs(levels): controller-knoppen per niveau (#255)"
```
