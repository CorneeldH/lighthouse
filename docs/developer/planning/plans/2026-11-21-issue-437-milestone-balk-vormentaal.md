# Milestone-balk: één vormentaal per toestand — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** De milestone-balk in de Prioritering-tab tekent de vijf toestanden uit de spec met één
vormentaal, met issuenummer, hover en klik, en `GET /api/priorities` levert de data daarvoor (#437).

**Architecture:** Drie lagen, onderaan beginnen. (1) `forge.Planning` leest per issue de labels en
per open milestone de gesloten issues. (2) `priorities.Build` zet dat om naar het blokkadelabel als
`[labels]`-sleutel, de vlag open 🤖 Vraag en `closed`-issues, en `openapi.json` plus de client
worden opnieuw gegenereerd. (3) De web-UI krijgt `issueState()` als enige afleiding, een gegroepeerde
`MilestoneRow` en een herschreven `MilestonesView` met nieuwe tokens en legenda. De Go-regel
`internal/issuestate` (#433) is het model; beide lezen dezelfde JSON-fixture.

**Tech Stack:** Go (`internal/forge`, `internal/priorities`, `internal/server`, `internal/cli`),
React + TypeScript + Vitest in `web/`, `openapi.json` met een gegenereerde client.

**Spec:** [de statusmodel-spec](../specs/2026-11-20-statusmodel-en-statusschema-design.md).
Relevant: *Toestanden*, *Data*, *Vormentaal*, *Milestone-balk*. De afhankelijkhedengraaf is een
eigen spec-sectie en valt buiten dit plan.

## Global Constraints

- Code, namen, comments en testnamen zijn Engels (`code_language = "en"`); plan, commits,
  changelog en PR zijn Nederlands.
- Geen nieuw label, geen nieuwe Status, niet schrijven vanuit de web-UI (No-Gos van milestone 16).
- Stippelrand: 1,5 px, gestippeld, `--foreground`, nooit in milestone-kleur.
- Nummer in een blokje vanaf 28 px breedte; daaronder alleen hover. Hover is `#nr titel`.
- Groepsvolgorde: af, actief, oppakbaar, mens nodig, geblokkeerd. Binnen een groep werkvolgorde
  (diepte in de blocker-graaf, dan component, dan nummer); af op nummer.
- Outside blockers: dezelfde vormentaal, **doorgetrokken** rand in de kleur van hun eigen milestone.
- Niet-geshapete milestone (0 issues of `Status: shaping`): één markering, grijze stippelrand om
  de hele balk, grijs percentage met `voorlopig`, bij 0 issues de tekst `nog geen issues`.
- Geel, de pil `picked up`, de navy stippelrand, de grijze streepbalk en de witte waas verdwijnen.
- `Of` uit `internal/issuestate` is het model voor `issueState()`, niet `GateZero`.
- Go volgt `/lighthouse-go-standard`, tests volgen `/lighthouse-test-standard`. Voor elke commit
  `lighthouse imports check` naast `[test_gate] command`. Elke commit is Conventional en atomair.
- De PR krijgt een fragment in `changelog.d/`. Niets onder `pack/` verandert, dus
  `init --upgrade` moet aan het eind `No changes.` geven.

## Review Focus

- Een milestone met meer dan 100 gesloten issues: de forge-read geeft een fout in plaats van een
  afgekapte lijst (zelfde regel als `blockedBy`). Test in Taak 1.
- Een issue met `Controller` én een open blocker blijft actief; een gesloten issue met een label
  blijft af. Gedekt door de gedeelde fixture in Taak 3.
- Een gesloten milestone-issue dat als *not planned* sloot staat niet in `closed` en niet in
  `Done`, dus telling en blokjes komen overeen. Test in Taak 1.
- Een repo zonder eigen `[labels]`-sleutel (lege labelnaam) geeft nooit een blokkadelabel. Test in
  Taak 2.
- Een milestone waar alle issues af zijn (100 %) of geen enkel: geen deling door nul, geen lege
  groepen als aparte tussenruimte. Test in Taak 4.

---

### Task 1: forge.Planning leest labels en gesloten issues

**Files:**
- Modify: `internal/forge/planning.go` (`PlanIssue`, `MilestoneCount`, `planIssuesQuery`,
  `planMilestonesQuery`, `planIssues`, `planMilestones`)
- Modify: `internal/forge/gitlab_planning.go`
- Modify: `internal/forge/forgesuite/suite.go` (rond regel 388)
- Modify: `internal/forge/forgetest/server.go` (de fake-GitHub levert nu per issue `labels` en per
  milestone de gesloten issues met `stateReason`; nu serveert `planMilestones` alleen
  nummer/titel/staat, rond `:802-806` en `:951-958`)
- Test: `internal/forge/planning_test.go`, `internal/forge/gitlab_planning_test.go`

**Interfaces:**
- Produces: `PlanIssue.Labels []string` (labelnamen, nooit nil) en
  `MilestoneCount.Closed []ClosedIssue` met
  `type ClosedIssue struct { Number int; Title, URL string }`, op nummer oplopend, nooit nil.
  Op GitHub bevat `Closed` alleen issues gesloten als *completed*, zodat `len(Closed) == Done`;
  `Done` volgt daar uit `len(Closed)`, niet uit de zoek-telling, zodat blokjes en percentage niet
  uiteen kunnen lopen. Op GitLab telt elk gesloten issue als `Done` (`gitlab_planning.go:14`,
  `NotPlannedCountsAsDone` in de suite), dus daar is `Closed` alle gesloten issues; de suite
  vertakt per forge zoals `wantDone` al doet.

- [ ] **Step 1: Test.** `TestPlanningReadsLabels`: een open issue met labels `a`, `b` geeft
  `Labels == []string{"a","b"}`, zonder labels `[]string{}`. `TestPlanningClosedIssues`: een
  milestone met twee completed-issues, één not-planned en één PR geeft `Closed` met precies de twee
  completed-issues en `Done == 2`. `TestPlanningClosedTruncated`: `hasNextPage: true` op de
  gesloten-issueslijst geeft een fout die de milestone noemt.
- [ ] **Step 2: Draai ze** (`go test ./internal/forge/ -run Planning`); verwacht FAIL (veld bestaat niet).
- [ ] **Step 3: Implementeer** in `planning.go`: `labels(first: 50){ nodes { name } }` op de
  issuesquery (`pageInfo` bij afkapping is een fout) en
  `issues(first: 100, states: CLOSED){ pageInfo{hasNextPage} nodes{number title url stateReason} }`
  op de milestonequery, in Go gefilterd op `stateReason == "COMPLETED"`. Op
  GitHub haal je `Done` uit `len(Closed)` en verdwijnt de `is:closed reason:completed`-zoektelling
  uit `planning.go:207-228`; zorg dat `len(Closed) == Done` in de tests, en noteer in de doc-comment van `Planning` dat de
  querykosten stijgen.
- [ ] **Step 4:** Doe hetzelfde voor GitLab in `gitlab_planning.go` (labels zitten daar op het
  issue; gesloten issues via de milestone-issues met `state=closed`). Breid de gedeelde
  contract-suite in `forgesuite/suite.go` uit met beide velden, zodat GitHub en GitLab dezelfde
  eis delen.
- [ ] **Step 5: Draai** `go test -race ./internal/forge/...`; verwacht PASS. Werk ook de
  planning-kostennotitie in de priorities-documentatie bij als de meting verandert.
- [ ] **Step 6: Commit** `feat(forge): planning leest labels en gesloten issues (#437)`.

### Task 2: priorities.Build en de API

**Files:**
- Modify: `internal/priorities/priorities.go` (`Source`, `Issue`, `Milestone`, `Build`)
- Modify: `internal/cli/serve.go` (`startPriorities`, regel 267-280, en waar `Source` wordt gevuld,
  `:210-213`; `forgeReposWith`, `:123-143`, en het `--repo`-pad, `:117`)
- Modify: `internal/overview/overview.go` (`RepoSource` krijgt `Labels repoconfig.Labels`) en
  `internal/repoconfig/repoconfig.go` (geëxporteerde `DefaultLabels()` naast `defaultString`,
  `:205`)
- Modify: `web/openapi.json`, `web/src/api/lighthouse.ts` (regenereren)
- Test: `internal/priorities/priorities_test.go`, `internal/server/priorities_test.go`,
  `internal/cli/serve_test.go`

**Interfaces:**
- Consumes: Task 1 (`PlanIssue.Labels`, `MilestoneCount.Closed`); `repoconfig.Labels`;
  `overview.Overview.Questions` (`Repo`, `Number`).
- Produces JSON: per issue `label` (de `[labels]`-sleutel, bv. `needs_shaping`, `""` zonder) en
  `open_question` (bool); per milestone `closed: [{number, title, url}]`.
  `Build(ctx, srcs, ov overview.Overview) ([]Repo, error)`: neemt nu de hele overview
  (statuses én questions). `Source` krijgt `Labels repoconfig.Labels`.

- [ ] **Step 1: Test** in `priorities_test.go`: een issue met label `needs-shaping` en
  `Labels{NeedsShaping: "needs-shaping"}` geeft `label == "needs_shaping"`; bij meerdere
  blokkadelabels wint de rang uit `labels.md` (hergebruik de rangfunctie uit `internal/issuestate`,
  exporteer die zo nodig als `issuestate.BlockingKey(names []string, l repoconfig.Labels) string`
  in plaats van de tabel te kopiëren). Een lege labelnaam in de config matcht nooit. Een vraag in
  `ov.Questions` voor `repo#nr` zet `open_question`. `Closed` komt door.
- [ ] **Step 2: Draai** `go test ./internal/priorities/`; verwacht FAIL.
- [ ] **Step 3: Implementeer** `Build` met een `map[string]bool` voor vragen naast de
  statusmap, volgens het patroon van `priorities.go:80-93`. Pas `startPriorities` aan zodat hij
  `ov` doorgeeft. `serve()` ziet de repo-config nu niet: `RepoSource` heeft geen labels en
  `forgeReposWith` gooit `repoconfig.Repo` weg na het bouwen. Neem `Labels` dus op in
  `RepoSource`, vul ze in `forgeReposWith`, en gebruik `repoconfig.DefaultLabels()` in het
  `--repo`-pad (`serve.go:117`, waar `repoconfig.Repo{Slug, Kind}` lege labels heeft). Vul dan
  `Source.Labels` in `serve.go:210-213` uit `r.Labels`.
- [ ] **Step 4: Server- en serve-tests** bijwerken (`fakeForge`, `countingSource`), een serve-test
  die voor beide paden (config en `--repo`) een niet-lege labelsleutel end-to-end controleert, en
  een endpointtest die de nieuwe JSON-velden en `[]` (nooit `null`) voor `closed` controleert.
- [ ] **Step 5: Regenereer** eerst `web/openapi.json` (die schrijft `TestOpenAPISnapshot`, niet
  `npm run gen`): `go test ./internal/server -run TestOpenAPISnapshot -update`; dan de client:
  `cd web && npm run gen`; controleer
  `git status --porcelain -- src/api` toont alleen de verwachte wijziging.
- [ ] **Step 6: Draai** `go test -race ./internal/priorities/ ./internal/server/ ./internal/cli/`;
  verwacht PASS. **Commit** `feat(priorities): label, open vraag en gesloten issues in de API (#437)`.

### Task 3: issueState() in derive.ts

**Files:**
- Modify: `web/src/lib/priorities/derive.ts` (vervang `isActive`, `derive.ts:43-45`, en zet
  `GNode.active`, `derive.ts:124-132`, om op de toestand)
- Test: `web/src/lib/priorities/derive.test.ts`

**Interfaces:**
- Produces: `type IssueStateName = 'done'|'active'|'needs_human'|'blocked'|'ready'` en
  `issueState(issue: Issue, runs: Run[], repo: string): { state: IssueStateName; needsHuman: boolean }`.
  `needs_human` is de eigen toestand (rij 2 en 4 van de tabel); `needsHuman` is de vlag, ook bij
  `active`. Een issue in `issues` is open, dus `done` geeft deze functie alleen voor de gesloten
  blokjes van Taak 4 (`closed` = `true` via een eigen overload `closedState()` is niet nodig: Taak 4
  zet `done` zelf).
- Consumes: `Issue.label`, `Issue.open_question`, `Issue.status`, `Issue.blocked_by`, `liveRun`.

- [ ] **Step 1: Test.** `derive.test.ts` leest `tests/pack/fixtures/issue-state/cases.json`
  (als JSON-import met `resolveJsonModule` in `web/tsconfig.app.json`; geen `node:fs`, want
  `tsconfig.app.json` heeft alleen `types: ["vite/client"]` en `npm run typecheck` zou op
  TS2307 falen. Staat Vite de import buiten `web/` niet toe, zet dan `server.fs.allow` voor het
  workspace-root en kopieer niets) en draait per case `issueState()`. De case-velden (`labels`, `status`, `run`,
  `blockers`, `open_question`) worden vertaald naar `Issue` en `Run`; de labelnamen in de fixture
  zijn al sleutels, dus `label` is de sleutel met de laagste rang. Gesloten cases (`closed`)
  worden overgeslagen en in Taak 4 gedekt. Verwacht per case `want.state` en `want.needs_human`
  (afwezig = false), met `ready` → `ready`, `needs_human` → `needs_human`.
- [ ] **Step 2: Draai** `cd web && npm test -- derive`; verwacht FAIL (`issueState` bestaat niet).
- [ ] **Step 3: Implementeer** `issueState` volgens de rijen 1 tot 5 van *Toestanden*:
  actief = live run of Status `In progress`/`Controller`; dan `needs_shaping`/`needs_decision`;
  dan blokkadelabel `blocked_*` of een blocker in `blocked_by`; dan `needs_hands`/`needs_testing`;
  anders `ready`. De vlag is `label` in `needs_*` of `open_question`, alleen als de toestand
  `active` is. Vervang `isActive` en alle aanroepen (`MilestonesView.tsx`, `DepsView.tsx`,
  `UnassignedView.tsx`) door `issueState(...).state === 'active'` waar zij een boolean willen. `isActive` heeft alleen
  aanroepen in `derive.ts:122` en `derive.test.ts:5,127-140`: vervang dat testblok.
- [ ] **Step 4: Draai** `npm test`, `npm run typecheck`, `npm run lint`; verwacht PASS. **Commit**
  `feat(web): issueState() volgt de toestandstabel van de spec (#437)`.

### Task 4: gegroepeerde MilestoneRow

**Files:**
- Modify: `web/src/lib/priorities/derive.ts` (`MilestoneRow` krijgt `blocks`, `shaping` en
  `percent` erbij; de oude `active`, `todo` en `order` blijven staan tot Taak 5 hun laatste lezer
  (`MilestonesView.tsx:20,96,107,127-130`) vervangt, anders faalt `tsc -b` bij deze commit; `derive.ts:215-289`, `isShaping`,
  `derive.ts:210-213`)
- Test: `web/src/lib/priorities/derive.test.ts`

**Interfaces:**
- Consumes: Task 2 (`Milestone.closed`), Task 3 (`issueState`).
- Produces: `MilestoneRow.blocks: Block[]` met
  `type Block = { nr: number; repo: string; title: string; url: string; state: IssueStateName; needsHuman: boolean }`,
  in groepsvolgorde (Global Constraints). `MilestoneRow.shaping: boolean`, `percent: number`.
  De `to do:`-regel leest `blocks` in dezelfde volgorde.

- [ ] **Step 1: Test.** Een milestone met issues in alle vijf toestanden geeft `blocks` in de
  volgorde af, actief, oppakbaar, mens nodig, geblokkeerd; binnen oppakbaar de werkvolgorde;
  `done` op nummer. Een outside blocker uit een andere repo heeft geen issuedata (`blank()` in
  `derive.ts`, `external: true`); die krijgt de toestand `ready` en een eigen rand. Een `Controller`-issue staat bij actief. Een actief issue met een open vraag
  heeft `needsHuman`. Een milestone zonder issues of met `Status: shaping` geeft `shaping: true`;
  bij 0 open en 0 gesloten is `percent` 0, niet `NaN`.
- [ ] **Step 2: Draai** `npm test -- derive`; verwacht FAIL.
- [ ] **Step 3: Implementeer** de groepering en voeg de groepering toe naast de `active`/`todo`-splitsing; houd de
  bestaande werkvolgorde-sortering en pas die per groep toe.
- [ ] **Step 4: Draai** de web-tests; verwacht PASS (pas `MilestonesView.test.tsx` en
  `PrioritiesTab.test.tsx` mee aan het nieuwe `MilestoneRow`). **Commit**
  `feat(web): milestone-rij groepeert blokjes per toestand (#437)`.

### Task 5: balk, tokens en legenda

**Files:**
- Modify: `web/src/components/priorities/MilestonesView.tsx` en `derive.ts` (verwijder de oude
  `active`, `todo`, `order` van `MilestoneRow`) (`:6-10`, `:20-22`, `:47-71`,
  `:96-120`)
- Modify: `web/src/components/priorities/Toolbar.tsx` (legenda, `:73-74`)
- Modify: `web/src/index.css` (tokens, licht `:93-107`, donker `:144-157`)
- Test: `web/src/components/priorities/MilestonesView.test.tsx`

**Interfaces:**
- Consumes: Task 4 (`MilestoneRow.blocks`, `shaping`, `percent`).
- Produces: één `Block`-component met `data-state` (`done|active|ready|blocked`) en
  `data-needs-human`, zodat tests op toestand kunnen zoeken zonder CSS.

- [ ] **Step 1: Test.** Per toestand een blokje met het juiste `data-state`; `needs_human` en een
  actief blokje met vlag hebben `data-needs-human="true"`; een blokje breder dan 28 px toont
  `#nr`, een smaller niet maar het heeft nog steeds `title="#nr titel"`; een klik opent `url`
  (`window.open` of een `<a>`, volg wat de view nu voor links doet); `shaping` toont `voorlopig`
  en de grijze stippelrand, en `nog geen issues` bij 0 issues. De legenda toont vijf toestanden en
  de shaping-rand en niet meer `picked up`. De breedte meet de component met een `ResizeObserver` op de balk
  (blokbreedte = balkbreedte × aandeel); jsdom heeft er geen, dus stub `ResizeObserver` en
  `offsetWidth`/`getBoundingClientRect` in de test, en toets ook dat het getal verdwijnt bij een
  smallere container. Kies de drempel als constante `MIN_LABEL_WIDTH = 28`.
- [ ] **Step 2: Draai** `npm test -- MilestonesView Toolbar`; verwacht FAIL.
- [ ] **Step 3: Implementeer.** Verwijder `--lh-yellow-pale`, `--lh-hatch-*` en
  `--lh-shaping-*` uit `index.css` en `MilestonesView.tsx`, na een grep over heel `web/src`, en
  alleen voor zover niets anders ze gebruikt. **Laat `--lh-yellow` staan**: het is het merkaccent
  (`index.css:51`, `Header.tsx:35,40,57`, `Tabs.tsx:43`, `RunRow.tsx:18,51`, `RunBlocks.tsx:127`,
  `ControllerSection.tsx:14`, `tone.ts:9`, gereserveerd in `palette.test.ts:7`); verwijder alleen
  het gebruik ervan in `MilestonesView.tsx` en de legenda. `DepsView.tsx` blijft buiten scope: laat zijn tokens staan en noem dat in de commit. Voeg tokens
  toe voor de streep (`repeating-linear-gradient` van milestone-kleur en transparant) en het kruis,
  voor licht én donker. Mens nodig: `outline`/`border` 1,5 px dashed `var(--foreground)` over
  elke toestand. Outside blockers: dezelfde `Block` met `border: 1.5px solid <kleur eigen milestone>`.
- [ ] **Step 4: Draai** `npm run lint && npm run typecheck && npm test`; verwacht PASS. Controleer
  licht en donker met de dev-server of een screenshot, want de tests bewijzen het uiterlijk niet.
  **Commit** `feat(web): milestone-balk tekent de vijf toestanden met één vormentaal (#437)`.

### Task 6: afronden

**Files:**
- Create: `changelog.d/<fragment>.md` (volg het formaat van de bestaande fragmenten)
- Modify: documentatie van de Prioritering-tab voor zover zij geel of `picked up` noemt
  (`grep -rn "picked up" docs web/src`)

- [ ] **Step 1:** Schrijf het changelog-fragment (Nederlands) en werk de documentatie bij.
- [ ] **Step 2:** Draai de volledige gate: `[test_gate] command` uit `lighthouse.toml`,
  `lighthouse imports check` en `go run ./cmd/lighthouse init --upgrade`; verwacht groen en
  `No changes.`
- [ ] **Step 3: Commit** `docs(priorities): changelog en documentatie voor de milestone-balk (#437)`.
