# Zes statussen en één vormentaal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `issuestate.Of`, `cases.json` en `issueState()` geven zes statussen, en balk, legenda en graafkaart tekenen ze met één gedeelde component (issue #555, deel a van de spec).

**Architecture:** Eerst de regel in Go met de gedeelde gevallen, daarna dezelfde regel in `derive.ts`, dan de gedeelde `Block` met zes vormen, daarna `NodeCard` en de legenda die hem gebruiken. Elke taak laat de testsuite groen achter. De vlag `NeedsHuman` verdwijnt overal; de status zegt het al.

**Tech Stack:** Go (`internal/issuestate`, `internal/cli`), React + TypeScript + Tailwind + vitest (`web/`).

**Spec:** [2026-11-23-prioritering-zes-statussen-design.md](../specs/2026-11-23-prioritering-zes-statussen-design.md), secties 1 tot en met 4. Mockup: [2026-11-23-prioritering-zes-statussen-mockup.html](../specs/2026-11-23-prioritering-zes-statussen-mockup.html).

## Global Constraints

- Zes statussen, vaste legenda-volgorde: `blocked` · `needs shaping` · `to do` · `active` · `needs human` · `done`. Interne waarden: `blocked`, `needs_shaping`, `ready`, `active`, `needs_human`, `done`; `ready` blijft intern `ready`, alleen de UI-tekst is `to do`.
- Balkvolgorde (omgekeerd): done · needs human · active · to do · needs shaping · blocked.
- Nummer onderstreept (`text-decoration: underline`, 2px dik, 2px offset) bij `needs_shaping` en `needs_human`, nergens anders.
- Kruis: SVG-achtergrond met `preserveAspectRatio="none"` en `vector-effect="non-scaling-stroke"`, kleur `--lh-cross`, op `--lh-seg-todo`.
- Needs shaping: `1.5px dashed` in `--foreground`. Needs human: strepen met `inset 0 0 0 2px` in `--foreground`.
- Geen `#` vóór een nummer in UI-tekst, ook niet in hover-titels. Sleutels als `${repo}#${nr}` en de CLI-regel `state: #<nr> <status>` houden hun `#`.
- UI-tekst Engels (#504), artefacten Nederlands. Geen geel. Geen nieuw label, geen nieuwe Status, niets schrijven vanuit de web-UI.
- `GateZero` blijft zoals het is (een blocked-label wint daar bewust van `needs_shaping`).
- Voor elke commit: `[test_gate] command` uit `lighthouse.toml` en `lighthouse imports check`.
- Webcommando's draaien in `web/`: `cd web && …`. Voor de eerste webtaak bestaat `web/node_modules` niet in de worktree: activeer Node uit `web/.nvmrc` en draai `cd web && npm ci` (Task 2, Step 0). `npm run build` typecheckt niet; de webverificatie is `cd web && npm run typecheck && npm run lint && npm run test` (dezelfde segmenten als de gate).

## Review Focus

- Actief issue met `needs_shaping` én een open vraag: wordt `needs_shaping` (rij 2 wint van rij 3). Test in Taak 1 en 2.
- Actief issue met alleen `blocked_issue`: blijft `active` (geen needs-label). Bestaand geval blijft staan.
- Open blocker + `needs_shaping` zonder run: `needs_shaping`, niet `blocked`. Bestaand geval, nu met nieuwe uitkomst.
- Externe graafkaart (`grey`): geen statusblokje, grijze rand, nummer niet onderstreept. Test in Taak 4.
- Milestone in shaping: stippelrand om de balk blijft, geen legenda-item. Test in Taak 5.

---

### Task 1: Zes statussen in Go en `cases.json`

**Files:**
- Modify: `internal/issuestate/state.go:17-76`, `internal/issuestate/cmd.go:88-108`
- Modify: `tests/pack/fixtures/issue-state/cases.json`
- Test: `internal/issuestate/state_test.go`, `internal/cli/state_test.go:105-106,246-252`

**Interfaces:**
- Produces: `State` constant `NeedsShaping State = "needs_shaping"`; `Result` wordt `struct{ State State }` zonder `NeedsHuman`; `Of(in Input, l repoconfig.Labels) Result` volgt de tabel uit sectie 1. `HasNeedsLabel` blijft bestaan (andere gebruikers).
- Produces: `cases.json` rijen met `want: {"state": "..."}` en zonder `needs_human`-veld.

- [ ] **Step 1: Pas `cases.json` aan (rood maken).** Elke rij van de tabel heeft minstens één geval. Wijzig de verwachtingen: `needs_shaping` alleen → `needs_shaping`; `needs_shaping` met open blocker of met `blocked_issue` → `needs_shaping`; `needs_decision`, `needs_hands`, `needs_testing` → `needs_human`. Vervang de vier "flags a human" gevallen door: actief + `needs_hands` → `needs_human`; actief (Controller) + `needs_shaping` → `needs_shaping`; actief + open vraag → `needs_human`; actief + `needs_shaping` + open vraag → `needs_shaping`; actief + `blocked_issue` + `needs_hands` → `needs_human`; actief + `needs_decision` → `needs_human`. "active with blocked label does not flag" en "ready with open question" blijven (`active`, `ready`).
- [ ] **Step 2: Pas `stateCase.Want` in `state_test.go` aan** tot alleen `State`, en `want := Result{State: State(c.Want.State)}`. Pas de comment bij `state_test.go:93` aan als die `needs_human` noemt. Run `go test ./internal/issuestate/...`; verwacht FAIL op de gewijzigde gevallen (en een compilefout tot Step 3).
- [ ] **Step 3: Implementeer** in `state.go`: constante `NeedsShaping`; `Result` zonder `NeedsHuman`; in `Of` de actieve tak: `has(l.NeedsShaping)` → `NeedsShaping`; anders `in.OpenQuestion || has(l.NeedsDecision, l.NeedsHands, l.NeedsTesting)` → `NeedsHuman`; anders `Active`. Niet-actieve takken: `has(l.NeedsShaping)` → `NeedsShaping`; `has(l.NeedsDecision)` → `NeedsHuman`; dan blocked; dan hands/testing → `NeedsHuman`; anders `Ready`. Werk de package- en functiecomments bij.
- [ ] **Step 4: Pas `Show` aan** in `cmd.go`: print alleen `state: #<nr> <state>`; haal `, needs human` en de verwijzing in de doc comment weg. Werk `internal/cli/state_test.go` bij (ook regel 106, `not on the board` met `3-needs-shaping`: wordt `state: #7 needs_shaping`): `state: #7 active, needs human` wordt de status die de regel geeft (`state: #7 needs_human` voor een actief issue met `needs_hands`), en geen test verwacht nog `, needs human`.
- [ ] **Step 5: Run** `go test ./internal/issuestate/... ./internal/cli/... -race`; verwacht PASS. Run `go vet ./...` en `lighthouse imports check`.
- [ ] **Step 6: Nog niet committen.** `derive.test.ts` leest `cases.json` ook en is nu rood; Taak 1 en 2 gaan samen in één commit (einde Taak 2).

### Task 2: Dezelfde regel in `derive.ts`

**Files:**
- Modify: `web/src/lib/priorities/derive.ts:15-16,52-77,145,159-170,280-284,349-365`
- Test: `web/src/lib/priorities/derive.test.ts:1,203-234,242,246,377-413`, `web/src/lib/priorities/layout.test.ts:15` (geen `needsHuman` meer in de `GNode`), `web/src/components/priorities/DepsView.test.tsx:78-92` (knoop 3 wordt `needs_human`, geen `data-needs-human`), `web/src/components/priorities/MilestonesView.test.tsx:96-129` (nieuwe volgorde, `needs_human` via `shapeOf`)

**Interfaces:**
- Produces: `type IssueStateName = 'done' | 'needs_shaping' | 'needs_human' | 'active' | 'ready' | 'blocked'`; `issueState(issue: Issue, runs: Run[], repo: string): IssueStateName` (geen object meer); `GNode` en `BlockData` zonder `needsHuman`; `GROUP_ORDER: IssueStateName[] = ['done', 'needs_human', 'active', 'ready', 'needs_shaping', 'blocked']`.

- [ ] **Step 0: Installeer de webafhankelijkheden.** `cd web && npm ci` met Node uit `web/.nvmrc`; controleer dat `web/node_modules/.bin/vitest` bestaat.
- [ ] **Step 1: Pas `derive.test.ts` aan.** De cases-test vergelijkt `issueState(...)` met `want.state` (geen `needsHuman` meer). Pas de groepsvolgorde-test aan naar `GROUP_ORDER` hierboven (blokken in de balk: done, needs_human, active, ready, needs_shaping, blocked). Pas ook de tests aan die de semantiek afdwingt: `derive.test.ts:242,246` (geen `.state` meer), `layout.test.ts:15`, `DepsView.test.tsx:78-92` en `MilestonesView.test.tsx:96-129` (zie Files). Run `cd web && npx vitest run src/lib/priorities`; verwacht FAIL.
- [ ] **Step 2: Implementeer `issueState`** met dezelfde rijvolgorde als Go: actief → `issue.label === 'needs_shaping'` geeft `needs_shaping`, anders `issue.needs_label || issue.open_question` geeft `needs_human`, anders `active`; niet actief: `needs_shaping` → `needs_shaping`, `needs_decision` → `needs_human`, blocked, `needs_hands`/`needs_testing` → `needs_human`, anders `ready`. Verwijder `needsHuman` uit `GNode`, `BlockData`, `buildGraph` en de groepering (`derive.ts:145,170,281,349,359`); werk `GROUP_ORDER` bij en de filter op `done` op regel 365 blijft.
- [ ] **Step 3: Houd de build groen.** Componenten die `GNode.needsHuman` of `issueState().needsHuman` lezen (`Block`-aanroepen in `MilestonesView`, `NodeCard`, `Toolbar`) breken de typecheck. Pas ze hier minimaal aan: geef `state` door en laat de vorm voorlopig zoals die was (de nieuwe vormen komen in Taak 3 tot 5). Run `cd web && npm run typecheck && npm run lint && npm run test` en `go test ./...`; verwacht PASS.
- [ ] **Step 4: Commit Taak 1 en 2 samen** met `lighthouse imports check` en de gate: `feat(issuestate): zes statussen in Go en web (#555)`.

### Task 3: `Block` met zes vormen, balk en hover-titels

**Files:**
- Modify: `web/src/components/priorities/Block.tsx`, `web/src/index.css:224-232`
- Modify: `web/src/components/priorities/MilestonesView.tsx:29-31,140,143,154,165-167,178,180`, `web/src/components/priorities/UnassignedView.tsx:7,25`, `web/src/components/priorities/Toolbar.tsx:26-31,150-157` (minimaal: `needsHuman`-prop weg, zodat de typecheck groen blijft; de legenda zelf volgt in Taak 5)
- Test: `web/src/components/priorities/MilestonesView.test.tsx`, `web/src/components/priorities/Block.test.tsx` (bestaat al: chip-kleurgevallen houden, kolom `needsHuman` weg, vormgevallen erbij), `web/src/components/priorities/UnassignedView.test.tsx:42,50-58` (geen `#`), `web/src/components/priorities/PrioritiesTab.test.tsx:178-190` (legenda: `data-state`, geen `data-needs-human`)

**Interfaces:**
- Produces: `type BlockState = IssueStateName` (importeer uit `derive.ts`); `Block` props `{ state: BlockState; color: string; title: string; href?: string; label?: number | string; borderColor?: string; testId?: string; className?: string }` (geen `needsHuman`). `Block` zet `data-state`, geen `data-needs-human`.
- Produces: `SHAPE: Record<BlockState, { className: string; style: (c: string) => CSSProperties }>` met de vormen uit Global Constraints; het nummer in het chipje is onderstreept voor `needs_shaping` en `needs_human` (`data-underlined="true"` op het nummer-element, de klasse `underline underline-offset-2 decoration-2`).
- Produces: CSS-klasse `.lh-cross` wordt een SVG-achtergrond (`url("data:image/svg+xml,…")` met twee `<line>`s van hoek tot hoek, `preserveAspectRatio="none"`, `vector-effect="non-scaling-stroke"`, `stroke` uit `--lh-cross` — gebruik een inline SVG-element in `Block` als de kleurvariabele niet in een data-URI past).

- [ ] **Step 1: Pas `Block.test.tsx` aan.** Per status: `data-state` klopt; `needs_shaping` heeft een `1.5px dashed` rand en onderstreept nummer; `needs_human` heeft de `lh-stripe`-klasse, de `inset 0 0 0 2px` box-shadow en onderstreept nummer; `blocked` bevat een `<svg>` met `preserveAspectRatio="none"` en `vector-effect="non-scaling-stroke"` op de lijnen; `done`, `ready`, `active` hebben geen onderstreept nummer; `needs_shaping` mét `borderColor` houdt de gestippelde rand (zie Step 2). Run `cd web && npx vitest run src/components/priorities/Block.test.tsx`; verwacht FAIL.
- [ ] **Step 2: Implementeer `Block`** volgens de Interfaces. Verwijder `needsHuman` en `data-needs-human`. `Block` zet `borderColor` na de vorm als effen rand (`Block.tsx:40-44`); laat `needs_shaping` zijn stippellijn daarom met `outline` (offset `-1.5px`) tekenen zodat de gestippelde vorm ook in de kolom van externe blokkers (die altijd `borderColor` krijgt) blijft.
- [ ] **Step 3: Werk `MilestonesView`** bij: `shapeOf` vervalt, blokken krijgen `state={b.state}`; verwijder `needsHuman`-props. De hover-titels op regel 143, 154 en 180 en in `UnassignedView.tsx:7` zonder `#` (`540 Titel`, `blocks 12`, `waits on 12`), en het zichtbare nummer op `UnassignedView.tsx:25` (`{node.number}` zonder `#`). Pas `Toolbar.tsx` minimaal aan (zie Files). De stippelrand om de balk bij `row.shaping` (regel 165-167) blijft ongewijzigd. Pas de metaregel (`meta()`) aan zodat de telling klopt met de zes statussen.
- [ ] **Step 4: Pas `MilestonesView.test.tsx`, `UnassignedView.test.tsx` en `PrioritiesTab.test.tsx` aan:** balkvolgorde done · needs human · active · to do · needs shaping · blocked (regel 96), geen `#` in titels of nummers, shaping-balk houdt zijn rand. Run `cd web && npx vitest run src/components/priorities && npm run typecheck`; verwacht PASS.
- [ ] **Step 5: Commit** `feat(web): Block tekent zes statussen met vectorkruis en onderstreping (#555)`.

### Task 4: Graafkaart met één statussignaal

**Files:**
- Modify: `web/src/components/priorities/NodeCard.tsx:24-84`, `web/src/components/priorities/DepsView.tsx:139,171`
- Test: `web/src/components/priorities/DepsView.test.tsx:59-92,200-208`

**Interfaces:**
- Consumes: `Block` uit Taak 3.
- Produces: `NodeCard` rendert, in deze volgorde: de streep links (`data-testid="card-strip"`, altijd effen milestonekleur, nooit `lh-stripe`), een statusblokje (`<Block testId="card-status" className="size-3.5 flex-none" …>` van 14 × 14 px, alleen als `!grey`), het nummer (onderstreept bij `needs_shaping` en `needs_human`, zonder `#`), de titel, `cycle` en het wachtpilletje. Geen `card-cross`, geen `outline`.

- [ ] **Step 1: Pas `DepsView.test.tsx` aan (rood):** `card-strip` heeft nooit `lh-stripe`; elke status heeft een `card-status` met de juiste `data-state`; een externe kaart (`data-external="true"`) heeft geen `card-status` en houdt `border-lh-grey`; het nummer is onderstreept bij `needs_shaping` en `needs_human`; er is geen `data-needs-human` en geen `card-cross`; hover-titels en labels bevatten geen `#`. Run `cd web && npx vitest run src/components/priorities/DepsView.test.tsx`; verwacht FAIL.
- [ ] **Step 2: Implementeer** in `NodeCard.tsx`; verwijder `showsNeedsHuman`, `active`/`blocked`-takken, `data-needs-human` en de outline. Controleer in `DepsView.tsx` dat `label` zonder `#` wordt opgebouwd (regel 139 en 171 doen dat al; zoek ook naar `title=` met `#`).
- [ ] **Step 3: Run** `cd web && npx vitest run src/components/priorities && npm run typecheck`; verwacht PASS.
- [ ] **Step 4: Commit** `feat(web): graafkaart met één statussignaal vóór het nummer (#555)`.

### Task 5: Legenda met twee regels

**Files:**
- Modify: `web/src/components/priorities/Toolbar.tsx:26-31,141-165`, `web/src/components/priorities/PrioritiesTab.tsx:73,103`
- Test: `web/src/components/priorities/PrioritiesTab.test.tsx` (en een Toolbar-test als die bestaat)

**Interfaces:**
- Produces: `STATES: Array<{ key: IssueStateName; label: string }>` in volgorde blocked, needs shaping, to do, active, needs human, done, met labels `blocked`, `needs shaping`, `to do`, `active`, `needs human`, `done`.
- Produces: legenda als twee `<ul aria-label="Milestones">` en `<ul aria-label="Status">`, elk voorafgegaan door een kopje in kleine hoofdletters (`Milestones`, `Status`). De statusregel gebruikt `Block` met `color="var(--lh-grey)"` en `testId="legend-block"`; het label van `needs shaping` en `needs human` is onderstreept. Het item `legend-shaping` ("not shaped yet") en "needs a human" vervallen.

- [ ] **Step 1: Schrijf of pas tests aan (rood):** op elk van de drie tabbladen bestaat een regel *Milestones* en een regel *Status*; de statusregel heeft zes items met exact de zes labels in die volgorde; `legend-shaping` bestaat niet meer; de labels `needs shaping` en `needs human` zijn onderstreept; een milestone in shaping houdt zijn stippelrand om de balk zonder eigen legenda-item. Run `cd web && npx vitest run src/components/priorities`; verwacht FAIL.
- [ ] **Step 2: Implementeer** in `Toolbar.tsx`; `legend` blijft de milestoneregel (kleur per milestone, plus *Without milestone* in grijs zodra issues zonder milestone in beeld zijn — bouw dat item in `PrioritiesTab.tsx` zonder de view-logica te veranderen).
- [ ] **Step 3: Run** `cd web && npm run typecheck && npm run lint && npm run test`; verwacht PASS en geen `needsHuman`-verwijzingen meer (`grep -rn "needsHuman\|needs-human" web/src` geeft niets, behalve `needs_human` als statuswaarde).
- [ ] **Step 4: Commit** `feat(web): legenda met kopjes Milestones en Status (#555)`.

### Task 6: Afronding

**Files:**
- Create: `changelog.d/feat-zes-statussen-vormentaal.md`
- Modify: `CHANGELOG.md` alleen als de releaseflow dat vraagt (zie [releasing.md](../../../releasing.md)); anders niet.

- [ ] **Step 1: Schrijf het fragment** in het formaat van `changelog.d/fix-prioritering-statussen.md` (frontmatter `type: changed`, daarna één tot drie zinnen in het Engels, zoals [releasing.md](../../../releasing.md) vraagt): zes statussen in `lighthouse state show`, balk, legenda en graafkaart, `, needs human` vervalt (#555).
- [ ] **Step 2: Zoek achtergebleven verwijzingen:** `grep -rn "NeedsHuman\|needs human\|not shaped yet" --include=*.go --include=*.ts --include=*.tsx --include=*.md . | grep -v CHANGELOG | grep -v planning/` en pas docs aan die de oude uitvoer of legenda beschrijven, in elk geval `docs/priorities.md` (§ afgeleide begrippen: toestand van een issue en vormentaal van de graaf, regels 97-120), dat Nederlands is en de zoekterm hierboven niet raakt (zoek ook op `mens nodig`).
- [ ] **Step 3: Draai de volledige gate:** `[test_gate] command`, `lighthouse imports check`, en `go run ./cmd/lighthouse init --upgrade` (verwacht `No changes.`).
- [ ] **Step 4: Visuele controle** tegen de mockup in een browser (jsdom tekent de vormen niet): de zes vormen in legenda, balk en graafkaart; het kruis is glad in een breed blok; licht en donker. Gebruik `cd web && npm run screenshot` (`web/e2e/screenshot.mjs`, brede viewport, licht en donker); is er geen browser, noteer dan dat dit een controle bij de PR is. Noteer het resultaat in het commitbericht of de PR.
- [ ] **Step 5: Commit** `docs(changelog): zes statussen en één vormentaal (#555)`.
