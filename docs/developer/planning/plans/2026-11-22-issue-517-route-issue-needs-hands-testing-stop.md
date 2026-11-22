# route-issue: needs_hands en needs_testing zijn een stop — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `lighthouse-route-issue` stopt bij `labels.needs_hands` en `labels.needs_testing` en meldt welke handeling of welk oordeel van de mens eerst nodig is; `MarkerHolds` volgt dezelfde betekenis.

**Architecture:** Alleen tekst en één Go-regel. De skill (`pack/core/skills/route-issue/SKILL.md`) krijgt een stoplijn voor de twee labels, de labelvolgorde volgt de rangregel van `write-issue` 3c; een skillcontracttest pint de stop; `MarkerHolds` laat een marker met die labels niet meer vervallen zolang het label blijft staan. Daarna `init --upgrade` en een changelog-fragment.

**Tech Stack:** Go, Markdown-skills, `internal/packcontract` (contracttests).

**Spec:** [`2026-11-20-statusmodel-en-statusschema-design.md`](../specs/2026-11-20-statusmodel-en-statusschema-design.md); issue #517.

## Global Constraints

- Skills zijn Engels, plan en artefacten Nederlands (`[project] language = nl`).
- Geen nieuw label, geen nieuwe Status (milestone-no-gos).
- Rangregel (`docs/developer/labels.md`, `write-issue` 3c), laagste rang wint: 1 `needs_shaping`, 2 `needs_decision`, 3 `blocked_issue`, 4 `blocked_external`, 5 `needs_hands`, 6 `needs_testing`.
- De regelvorm `#<nr> heading:gate_line <n>[<a|b|c>]: <route> — <reden>` blijft; `lighthouse measure-start-issue-kpi` leest hem.
- De Gate 0-zinnen die `internal/packcontract/issueflow_test.go:46-51` pint, blijven letterlijk staan (o.a. "Only **unknown** stays a stop at gate 0").
- Het tellen van label-rijen (`issueflow_test.go:153-168`) verwacht exact 6 rijen `| \`labels.` tussen `## A blocking label` en `## Non-negotiable`; de kop moet daar blijven beginnen met `## A blocking label`.
- De slotalinea die begint met "The two blocked labels hold the plan" en zijn gepinde zinnen (`issueflow_test.go:83-93`: `controller run`, ``route on with `— blocked: <label>` ``, en niet `wait for: <label>`) blijft staan; Stap 3 herschrijft alleen de alinea "The other four describe what has to happen *in* the session".
- Commit atomair, Conventional Commits; vóór elke commit `lighthouse imports check` naast `[test_gate] command`.

## Review Focus

- Issue met zowel `needs_hands` als `needs_shaping`: laagste rang (`needs_shaping`) wint, dus 4c, geen stop. Pin in Taak 1 (test op de rangvolgorde van de tabel).
- Issue met `needs_testing` én een open blocker: de blokkade wordt wel gemeld, maar de stop blijft; geen route uitvoeren. Pin in Taak 1 (tekst noemt het).
- `lighthouse-start-issue` kreeg een stoplijn en zou die als gate 4 uitvoeren. Pin in Taak 2.
- Een marker met `needs_hands` waarvan het label is weggehaald, moet wel vervallen. Pin in Taak 3.

---

### Task 1: Skilltekst en contracttest route-issue

**Files:**
- Modify: `pack/core/skills/route-issue/SKILL.md` (Labels-intro r.34-38; sectie `## A blocking label steers the triage, it does not stop it` r.209-229; Non-negotiable r.239; de kop van de sectie blijft beginnen met `## A blocking label`)
- Test: `internal/packcontract/issueflow_test.go`

**Interfaces:**
- Produces: stoplijnvorm `#<nr> heading:gate_line <n>[<a|b>]: stop — <label>: <handeling of oordeel van de mens>`; Taak 2 verwijst ernaar.

- [ ] **Step 1: Schrijf de falende test** `TestRouteIssueNeedsHandsAndTestingStop` in `issueflow_test.go`, volgens het patroon van de naburige tests (`variants`, `section`, `flat`, `wantAll`, `wantNone`; commentaar `// Classification: fails when …`). Sectie: `section(t, body, "## A blocking label", "## Non-negotiable")`. Asserts op de platgeslagen sectie:
  - pin per tabelrij, met `lineStarting` op `"| `labels.needs_hands`"` resp. `"| `labels.needs_testing`"`: de rij bevat `"**stop**"` en `"executes nothing"`, de needs_hands-rij `"which action of the human is needed first"`, de needs_testing-rij `"which judgement of the human is needed first"`. Geen gecombineerde zin over de hele sectie: de test pint precies de tekst die Stap 3 voorschrijft.
  - `wantNone` op `"execute up to the hand movement"` en `"let the human judge whether it does what was meant"` (de oude betekenis).
  - Op de Non-negotiable-sectie (`section(t, body, "## Non-negotiable", "## Known pitfalls")`): `wantNone` op `"treat a `labels.needs_*` label as a stop"`; `wantAll` op `"`labels.needs_hands`"` en `"`labels.needs_testing`"` in een ✅-regel.
  - Rangvolgorde: de byte-posities van de zes `` `labels.<key>` ``-rijen in de tabelsectie staan oplopend in de volgorde `needs_shaping, needs_decision, blocked_issue, blocked_external, needs_hands, needs_testing` (zoek per rij `"| `labels.<key>`"`).
  - Test heet `TestRouteIssueNeedsHandsAndTestingStop`, runt per variant.

- [ ] **Step 2: Draai de test, verwacht FAIL**

  Run: `go test ./internal/packcontract/ -run TestRouteIssueNeedsHandsAndTestingStop -v`
  Expected: FAIL (oude tekst).

- [ ] **Step 3: Pas `SKILL.md` aan**
  - Labels-intro: noem `needs_hands`/`needs_testing` als "the next step is a human's", in rangvolgorde van de rangregel.
  - Tabel: rijen herordenen naar de rangregel (rij-tekst van `blocked_*` en `needs_shaping`/`needs_decision` ongewijzigd). Rij `needs_hands`: **stop** — the next step is a human's; executes nothing; report which action of the human is needed first (where human hands are needed) in `#<nr> heading:gate_line <n>[<a|b>]: stop — <label>: <action>`, with `<n>` the gate where the triage would have stopped. Rij `needs_testing`: idem met "which judgement of the human is needed first". Noem: rangregel wint (een hoger label stuurt de route, bv. `needs_shaping` → 4c), en bij een open blocker meldt de reden dat ook maar de stop blijft (Gate 0 meldt de blokkade en "route on"; de route komt dan bij de stop in de labeltabel uit, zodat blokkade en stop beide in de regel staan).
  - Sectiekop mag een andere tekst krijgen mits hij met `## A blocking label` begint; slotalinea ("The other four describe what has to happen *in* the session") herschrijven: `needs_shaping` en `needs_decision` beschrijven wat *in* de sessie moet gebeuren, `needs_hands` en `needs_testing` stoppen de route.
  - Non-negotiable: in de ✅/❌-regel van `blocked_*` het `treat a labels.needs_* label as a stop` verwijderen; nieuwe regel ✅ stop and report what the human has to do first at `labels.needs_hands`/`labels.needs_testing` · ❌ execute any step of the route there.
  - Gate 0 en de door de tests gepinde zinnen blijven staan.

- [ ] **Step 4: Draai de pakettests, verwacht PASS**

  Run: `go test ./internal/packcontract/ -v 2>&1 | tail -20`
  Expected: PASS, ook de bestaande tests (label-rijen = 6, Gate 0-zinnen).

- [ ] **Step 5: Commit** — `lighthouse imports check`, dan
  `git add pack/core/skills/route-issue/SKILL.md internal/packcontract/issueflow_test.go` en `git commit -m "feat(route-issue): needs_hands en needs_testing zijn een stop (#517)"`.

### Task 2: start-issue voert een stoplijn niet uit

**Files:**
- Modify: `pack/core/skills/start-issue/claude/SKILL.md:164-171` (routetabel) en stap 3 (de zin "Skip the step on a `labels.blocked_*` label …"), en dezelfde plekken in `pack/core/skills/start-issue/agents/SKILL.md`
- Test: `internal/packcontract/` (bestaande start-issue-test in `skills_test.go` of `issueflow_test.go`; zoek met `grep -n "start-issue" internal/packcontract/*_test.go`, anders nieuwe test naast `TestRouteIssueNeedsHandsAndTestingStop`)

**Interfaces:**
- Consumes: stoplijnvorm uit Taak 1.

- [ ] **Step 1: Schrijf de falende test** `TestStartIssueStopsOnNeedsStopLine`: in elke variant van start-issue staat in de uitvoeringstabel een rij voor route `stop` met de woorden `"stop"` en `"no handoff"`... pin exacte zin die je in stap 3 schrijft: ``"route `stop` (`labels.needs_hands` or `labels.needs_testing`)"`` en `"report what the human has to do first"`.
- [ ] **Step 2: Draai, verwacht FAIL** — `go test ./internal/packcontract/ -run TestStartIssueStopsOnNeedsStopLine -v`
- [ ] **Step 3: Voeg in beide variants een tabelrij toe**: line with route `stop` (`labels.needs_hands` or `labels.needs_testing`) → report what the human has to do first and stop; no execution, no spec, no plan; in a controller run post no handoff (the controller sends such an issue to `RowInteractive`).
  Extra, in beide variants: (a) breid in stap 3 de skip-zin voor `lighthouse board-start` uit naar `labels.needs_hands` en `labels.needs_testing` (de volgende stap is van een mens; het bord blijft op `To do`); (b) zeg in de nieuwe rij dat de stop vóór het blok *Blocked?* gaat (dus ook bij `— blocker #<x> open` geen handoff) en dat bij een stop op 4b geen scout uitgaat. Pin (a) en (b) in `TestStartIssueStopsOnNeedsStopLine`.
- [ ] **Step 4: Draai `go test ./internal/packcontract/`, verwacht PASS.**
- [ ] **Step 5: Commit** — `feat(start-issue): een stoplijn van route-issue wordt niet uitgevoerd (#517)`.

### Task 3: MarkerHolds volgt de nieuwe betekenis

**Files:**
- Modify: `internal/triage/facts.go:110-113`
- Test: `internal/triage/facts_test.go:145-146` (`TestMarkerHolds`)

Besluit: een `needs_hands`/`needs_testing`-label is nu een mens-stop, net als `needs_decision`. De uitzondering die zo'n marker altijd liet vervallen, stamt uit de tijd dat het issue met die labels naar `Controller` ging (#182); `outcome.go:131-143` stuurt elk `needs_*`-issue nu naar `RowInteractive`. Let op: geen enkel huidig pad schrijft een marker met `Label` = `needs_hands`/`needs_testing` (`outcome.go`: `Label` komt alleen uit `g0.Label` (blocked_*) en RowDesign (needs_shaping/needs_decision); RowInteractive schrijft een marker zonder `Label` en zonder `Stop`, en die houdt altijd, `facts.go` `if !m.Stop`). De wijziging raakt dus alleen oude (#182-tijd) markers; de reden voor AC3 is: de uitzondering is dood voor nieuwe markers en past niet meer bij de betekenis. De test gebruikt daarom met opzet synthetische markers en pint alleen dat contract, geen end-to-end gedrag van triage. Verwijderen: de marker houdt dan zolang het label staat en vervalt als het label weg is (via de bestaande laatste regel `slices.Contains(f.Labels, m.Label)`).

- [ ] **Step 1: Pas de test aan (falend):** in `TestMarkerHolds` de twee cases `needs-hands label lapsed` en `needs-testing label lapsed` hernoemen naar `needs-hands label stays` / `needs-testing label stays` met `want: true`, en twee cases toevoegen `needs-hands label removed` / `needs-testing label removed` (`f.Labels = nil`, `want: false`). Markers met `protocolMarker("4a", nil, "3-needs-hands")`; controleer of `protocolMarker` `Stop` zet (anders `Stop: true` meegeven), want zonder stop houdt de marker altijd.
- [ ] **Step 2: Draai, verwacht FAIL** — `go test ./internal/triage/ -run TestMarkerHolds -v` (twee `stays`-cases rood).
- [ ] **Step 3: Verwijder het `if m.Label != "" && (m.Label == l.NeedsHands || …) { return false }`-blok** in `MarkerHolds`; werk de doc-comment bij met één zin dat `needs_*` een stop is die vervalt als het label weg is. Controleer dat `outcome_test.go:404-405` en de rest van `./internal/triage/` groen blijven.
- [ ] **Step 4: Draai `go test ./internal/triage/ ./internal/cli/`, verwacht PASS.**
- [ ] **Step 5: Commit** — `fix(triage): marker met needs_hands of needs_testing houdt zolang het label staat (#517)`.

### Task 4: Dogfood, changelog en verificatie

**Files:**
- Create: `changelog.d/issue-517-route-issue-needs-hands-stop.md`
- Modify (gegenereerd): `.claude/skills/lighthouse-route-issue/SKILL.md`, `.agents/skills/lighthouse-route-issue/SKILL.md` en de start-issue-kopieën

- [ ] **Step 1: Fragment** met front matter `type: changed` en één Engelse zin: `lighthouse-route-issue now stops at needs_hands and needs_testing and reports what the human has to do first; the triage marker for those labels holds while the label stays.` (formaat: `changelog.d/issue-512-start-issue-already-done-controller-run.md`).
- [ ] **Step 2:** `go run ./cmd/lighthouse init --upgrade`, dan nogmaals; verwacht `No changes.` bij de tweede.
- [ ] **Step 3:** volledige gate: `[test_gate] command` uit `lighthouse.toml` en `lighthouse imports check`; verwacht groen.
- [ ] **Step 4: Commit** — `git add changelog.d .claude .agents` plus eventuele andere door init gewijzigde paden; `git commit -m "chore(pack): installeer route-issue en start-issue, changelog (#517)"`.

## Zelfreview

- Spec/AC-dekking: AC1 (stop) Taak 1+2; AC2 (rangvolgorde) Taak 1; AC3 (`MarkerHolds`) Taak 3; AC4 (contracttest op `needs_hands`) Taak 1; AC5 (init) en AC6 (fragment) Taak 4.
- `docs/developer/labels.md` r.130-133 volgt de nieuwe betekenis al; geen wijziging. Controleer in Taak 4 stap 3 met `grep -rn "needs_hands" docs/*.md` of een andere doc de oude betekenis noemt.
