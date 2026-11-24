# Tab Status Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Een tab *Status* op de Prioritering-pagina die de open issues als kanban toont, met een kolom per status en een filter op milestone (#557).

**Architecture:** Alleen web. `GNode` krijgt een `runner`-veld (`controller` of `interactive`). Een nieuwe pure functie `statusBoard()` in `derive.ts` verdeelt de issues over de zes kolommen, sorteert ze en past het milestonefilter toe. `StatusView` tekent het resultaat met `Block` uit #555. `Prefs` krijgt `milestones: string[] | null` (null = alles aan), net als `repos`. De Go-kant, `cases.json` en de API blijven ongewijzigd: `Issue.prio_weight` en `Milestone.closed` volstaan.

**Tech Stack:** React, TypeScript, Vitest, Testing Library (`web/`).

**Spec:** [`2026-11-23-prioritering-zes-statussen-design.md`](../specs/2026-11-23-prioritering-zes-statussen-design.md), secties 6 en 7. Bouwt op #555 (statussen, vormentaal) en #556 (tab Backlog), beide gemerged.

## Global Constraints

- Code, commentaar, UI-teksten en testnamen in het Engels; plan, spec en commitberichten in het Nederlands. `changelog.d/` is Engels (voorbeeld: `issue-556-tab-backlog.md`).
- `ViewId` wordt `'deps' | 'milestones' | 'backlog' | 'status'`; tabvolgorde Dependencies · Milestones · Backlog · Status. De tab heet exact `Status`.
- Kolomvolgorde (gelijk aan `STATES` in `Toolbar.tsx`): blocked · needs shaping · to do · active · needs human · done; statusnamen `blocked`, `needs_shaping`, `ready`, `active`, `needs_human`, `done` als `IssueStateName`.
- Volgorde per kolom: `prioWeight` aflopend, daarna nummer **oplopend** (spec sectie 6). Kolom *done*: nummer aflopend, 5 zichtbaar, de rest ingeklapt.
- Een open issue zonder milestone of in een gesloten milestone telt als *Backlog* (grijze streep, `NO_MILESTONE_COLOR`), conform `withoutMilestone()`.
- Het filter op milestone staat alleen op de tab Status. Opslagvorm: `string[] | null` in `lighthouse.priorities`, sleutels `<repo>#m<nr>` en `backlog`; `null` = alles aan; een bewaarde sleutel die niet meer bestaat telt niet mee (zoals bij `repos`).
- Geen nieuw label, geen nieuwe Status, geen schrijven vanuit de web-UI, geen slepen, geen modelsuggesties.
- Gate-commando's: draai eerst `cd web && npm ci` als `web/node_modules` ontbreekt. Gebruik `go run ./cmd/lighthouse imports check` (de `lighthouse` op PATH geeft soms `COULD NOT CHECK`). Leun voor `[test_gate] command` op de uitvoer van `commit-test-gate` na elke commit.
- Nooit `--no-verify`. Voor elke commit: `[test_gate] command` uit `lighthouse.toml` en `lighthouse imports check`.

## Review Focus

- Issue in een gesloten milestone: kaart staat onder de chip *Backlog* met grijze streep, niet onder een milestonechip.
- Bewaard milestonefilter met een sleutel van een milestone die niet meer bestaat: genegeerd; blijft er niets geldigs over, dan staat alles aan (zoals bij `repos`).
- Kaart in *needs shaping* of *needs human* met een live run of Status `Controller`/`In progress`: krijgt toch `controller`/`interactive`; zonder run en zonder zulke Status krijgt hij geen label.
- Issue met run én Status `In progress`: `controller` wint.
- Getal op de tab telt open issues na repo- én milestonefilter, zonder *done*; lege kolom toont het aantal 0 en een lege-staatregel, geen lege `<ul>`.
- *done* met precies 5 of minder issues: geen inklapknop; bij 6 of meer toont de knop het aantal verborgen (`+ N more`).

## Bestanden

- Wijzig: `web/src/lib/priorities/derive.ts`, `derive.test.ts`
- Wijzig: `web/src/lib/priorities/prefs.ts`, `prefs.test.ts`
- Maak: `web/src/components/priorities/StatusView.tsx`, `StatusView.test.tsx`
- Wijzig: `web/src/components/priorities/Toolbar.tsx`, `PrioritiesTab.tsx`, `PrioritiesTab.test.tsx`, `types.ts`
- Wijzig: `docs/priorities.md`
- Maak: `changelog.d/issue-557-tab-status.md`

---

### Task 1: `runner` op `GNode`

**Files:**
- Modify: `web/src/lib/priorities/derive.ts` (`GNode`, `blank`, `buildGraph`)
- Test: `web/src/lib/priorities/derive.test.ts`

**Interfaces:**
- Produces: `GNode.runner: 'controller' | 'interactive' | null`. `controller` bij een `liveRun` (`open`/`parked`) of Status `Controller`; `interactive` bij Status `In progress` zonder zo'n run; anders `null`. Externe nodes: `null`.

- [ ] **Step 0: Installeer** `cd web && npm ci` (als `node_modules` ontbreekt).
- [ ] **Step 1: Schrijf de falende tests** in `derive.test.ts` (hergebruik de fixtures van de bestaande `buildGraph`-tests): `it('marks a live run as controller')`, `it('marks status Controller as controller')`, `it('marks In progress without a run as interactive')`, `it('lets a live run win over status In progress')`, `it('gives a to-do issue and an external node no runner')`, `it('keeps the runner on a needs_shaping issue with a live run')`.
- [ ] **Step 2: Draai** `cd web && npx vitest run src/lib/priorities/derive.test.ts` — verwacht FAIL.
- [ ] **Step 3: Implementeer** `runner` in `GNode`, `blank()` (`null`) en `buildGraph` naast `active`, met de variabele `run` die er al staat.
- [ ] **Step 4: Draai** dezelfde test plus `npm run typecheck` — verwacht PASS.
- [ ] **Step 5: Commit**

```bash
git add web/src/lib/priorities/derive.ts web/src/lib/priorities/derive.test.ts
git commit -m "feat(web): runner (controller of interactive) per issue in de graaf (#557)"
```

### Task 2: Voorkeuren: `'status'` en milestonefilter

**Files:**
- Modify: `web/src/lib/priorities/prefs.ts`
- Test: `web/src/lib/priorities/prefs.test.ts`

**Interfaces:**
- Produces: `ViewId` bevat `'status'`; `Prefs.milestones: string[] | null` (default `null`); `loadPrefs()` leest `milestones` alleen als array van strings; `savePrefs` bewaart het.

- [ ] **Step 1: Schrijf de falende tests**: `it('keeps a stored view "status"')`, `it('defaults the milestone filter to null (all on)')`, `it('loads a stored milestone filter')`, `it('ignores a milestone filter that is not a string array')`.
- [ ] **Step 2: Draai** `cd web && npx vitest run src/lib/priorities/prefs.test.ts` — verwacht FAIL.
- [ ] **Step 3: Implementeer** in `prefs.ts`: `'status'` in `ViewId` en `VIEWS`; veld `milestones` in `Prefs` en in de defaults van `loadPrefs`, gelezen zoals `repos`.
- [ ] **Step 4: Draai** dezelfde test — verwacht PASS. (De typecheck is rood tot taak 5: `Record<ViewId, …>` in `PrioritiesTab.tsx` mist `status`. Commit daarom samen met taak 5, of zet in `PrioritiesTab.tsx` hier al een voorlopige `status`-sleutel.)

### Task 3: `statusBoard()`

**Files:**
- Modify: `web/src/lib/priorities/derive.ts`
- Test: `web/src/lib/priorities/derive.test.ts`

**Interfaces:**
- Consumes: `GNode.runner` (taak 1); `Repo.milestones[].closed` (`ClosedIssue`).
- Produces:

```ts
export interface StatusCard {
  key: NodeKey; repo: string; number: number; title: string; url: string
  milestone: MilestoneRef | null   // alleen gezet voor een open milestone; anders null
  runner: 'controller' | 'interactive' | null
  state: IssueStateName
}
export interface StatusColumn { state: IssueStateName; cards: StatusCard[] }
export const MILESTONE_FILTER_BACKLOG = 'backlog'
export const milestoneKey = (repo: string, number: number): string => `${repo}#m${number}`
// filter: null = alles aan; sleutels van milestoneKey() en MILESTONE_FILTER_BACKLOG.
export function statusBoard(repos: Repo[], graph: Graph, filter: ReadonlySet<string> | null): { columns: StatusColumn[]; open: number }
```

`columns` heeft altijd zes kolommen in de vaste volgorde; `open` is het aantal kaarten in de vijf niet-done kolommen na het filter. De kolom *done* bevat de `closed`-issues van de milestones in `repos` (de aanroeper geeft de repos in beeld door), gefilterd op dezelfde sleutel, gesorteerd op nummer aflopend, met `state: 'done'` en `runner: null`. `repos` dient ook voor *done*; de open issues komen uit `graph.nodes` (niet `external`, en `repo` aanwezig in `repos`).

- [ ] **Step 1: Schrijf de falende tests** in `derive.test.ts`: zes kolommen in volgorde; sortering op `prioWeight` aflopend en bij gelijk gewicht nummer oplopend; een issue zonder milestone en een issue in een gesloten milestone staan onder sleutel `backlog` met `milestone: null`; filter `null` toont alles; een filterset met alleen `repo#m3` verbergt andere milestones en backlog; een filtersleutel van een onbekende milestone verandert niets aan de rest; *done* op nummer aflopend, gefilterd op milestone, telt niet mee in `open`; `open` volgt het filter.
- [ ] **Step 2: Draai** `cd web && npx vitest run src/lib/priorities/derive.test.ts` — verwacht FAIL.
- [ ] **Step 3: Implementeer** `statusBoard` in `derive.ts`; hergebruik de milestone-gesloten-regel van `withoutMilestone` (`state.toLowerCase() !== 'open'`) via een kleine gedeelde hulpfunctie `isOpenMilestone(ref: MilestoneRef | null): boolean`, zodat beide dezelfde regel gebruiken.
- [ ] **Step 4: Draai** dezelfde test — verwacht PASS.
- [ ] **Step 5: Commit**

```bash
git add web/src/lib/priorities/derive.ts web/src/lib/priorities/derive.test.ts
git commit -m "feat(web): statusBoard verdeelt issues over zes kolommen met milestonefilter (#557)"
```

### Task 4: `StatusView`

**Files:**
- Create: `web/src/components/priorities/StatusView.tsx`, `StatusView.test.tsx`
- Modify: `web/src/components/priorities/types.ts` (`ViewProps.milestoneFilter?: ReadonlySet<string> | null`; commentaar "Shared by … StatusView")

**Interfaces:**
- Consumes: `statusBoard`, `milestoneKey`, `StatusCard` (taak 3); `Block` (`state`, `color`, `title`, `testId`, `className`); `NO_MILESTONE_COLOR`.
- Produces: `export function StatusView(props: ViewProps): JSX.Element` met testids `view-status`, `status-column-<state>` (state = `IssueStateName`), `status-count-<state>`, `status-card` (met `data-number`), `status-runner` (tekst `controller` of `interactive`), `status-more` (de inklapknop), en `data-underlined="true"` op het nummer bij `needs_shaping`/`needs_human`.

- [ ] **Step 1: Schrijf de falende tests** in `StatusView.test.tsx` (fixtures als `BacklogView.test.tsx`): zes kolommen in volgorde met kopje (blokje, naam, aantal); een kaart linkt naar het issue en toont streep in de milestonekleur (grijs zonder milestone), nummer en titel; onderstreept nummer alleen bij needs shaping/needs human; `status-runner` toont `controller` of `interactive` of ontbreekt; *done* toont 5 kaarten en `status-more` met `+ 2 more` bij 7, en na een klik alle 7; bij 5 geen `status-more`; lege kolom toont lege-staatregel, geen `<ul>`; het milestonefilter (`milestoneFilter` prop) verbergt kaarten van een niet-gekozen milestone.
- [ ] **Step 2: Draai** `cd web && npx vitest run src/components/priorities/StatusView.test.tsx` — verwacht FAIL.
- [ ] **Step 3: Implementeer** `StatusView.tsx`: kolommen naast elkaar in een horizontaal scrollbare container; kaart volgt de opbouw van `Row` in `BacklogView.tsx` (streep in de kleur uit `colors.get(milestoneKey(...)) ?? NO_MILESTONE_COLOR`, onderstreping zoals daar); titel op één afgekapte regel; inklapstaat van *done* in lokale `useState`. Afmetingen en inklapvorm mogen de bouwer kiezen (spec *Aan de bouwer*).
- [ ] **Step 4: Draai** dezelfde test plus `npm run typecheck` — verwacht PASS.
- [ ] **Step 5: Commit** (samen met taak 5 als de typecheck daar op hangt).

### Task 5: Tab, chips en bedrading

**Files:**
- Modify: `web/src/components/priorities/Toolbar.tsx`, `PrioritiesTab.tsx`, `PrioritiesTab.test.tsx`

**Interfaces:**
- Consumes: `StatusView`, `statusBoard`, `milestoneKey`, `MILESTONE_FILTER_BACKLOG`, `Prefs.milestones`.
- Produces: tab `{ id: 'status', label: 'Status' }` na Backlog; `Toolbar` krijgt props `milestoneChips: Array<{ key: string; label: string; color: string }>`, `milestoneSelected: ReadonlySet<string>` en `onToggleMilestone: (key: string) => void`, alleen getoond bij `view === 'status'` in een groep `role="group" aria-label="Milestone filter"` met `aria-pressed` per chip (zelfde stijl als de repo-chips).

- [ ] **Step 1: Schrijf de falende tests** in `PrioritiesTab.test.tsx`: tab `Status` bestaat en opent `view-status`; het getal op de tab is het aantal open issues in beeld, zonder *done*; chips voor elke open milestone plus `Backlog`, standaard allemaal aan; een chip uitzetten verbergt zijn kaarten, werkt het getal bij en bewaart `milestones` in `localStorage` `lighthouse.priorities`; een bewaarde sleutel van een bestaande milestone wordt hersteld; een bewaarde sleutel van een niet-bestaande milestone telt niet mee; de milestonechips staan niet op de tabs Dependencies, Milestones en Backlog.
- [ ] **Step 2: Draai** `cd web && npx vitest run src/components/priorities` — verwacht FAIL.
- [ ] **Step 3: Implementeer** in `PrioritiesTab.tsx`: sleutel `status` in `hasUnassigned` (`false`), `counts` (`statusBoard(...).open`), `inViewMilestones` (open milestones van de repos in beeld, als bij `milestones`); een geldige-selectieset `milestoneSelected` afgeleid van `prefs.milestones` met dezelfde regels als `selected` voor repos; `toggleMilestone`; `View` kiest `StatusView` voor `'status'`; geef `milestoneFilter` mee in `props`. In `Toolbar.tsx`: tab, chips en `counts`. Chipkleur uit `colors` (`NO_MILESTONE_COLOR` voor *Backlog*).
- [ ] **Step 4: Draai** `cd web && npx vitest run src/components/priorities src/lib/priorities && npm run typecheck && npm run lint` — verwacht alles groen.
- [ ] **Step 5: Commit** (taken 2, 4 en 5 samen, want de typecheck hangt aan alle drie):

```bash
git add web/src
git commit -m "feat(web): tab Status met kanban en milestonefilter (#557)"
```

### Task 6: Documentatie en changelog

**Files:**
- Modify: `docs/priorities.md` (paragraaf "Wat het tabblad toont", rond regels 46-53, en de legenda-/statusbullets rond regel 105)
- Create: `changelog.d/issue-557-tab-status.md`

- [ ] **Step 1: Pas** `docs/priorities.md` aan: noem de vierde tab *Status* (zes kolommen, kaart, `controller`/`interactive`, volgorde, *done* met 5 zichtbaar, milestonefilter dat per browser wordt bewaard, het getal op de tab).
- [ ] **Step 2: Maak** `changelog.d/issue-557-tab-status.md` (`type: changed`): the Priorities page has a *Status* tab: a kanban with a column per status and a filter on milestone (#557).
- [ ] **Step 3: Controleer** met de hand dat nieuwe links relatief zijn en bestaan en commit:

```bash
git add docs/priorities.md changelog.d/issue-557-tab-status.md
git commit -m "docs(priorities): tab Status in de gebruikersdocumentatie en changelog (#557)"
```

### Task 7: Eindcontrole

- [ ] **Step 1: Draai** `cd web && npm test && npm run typecheck && npm run lint`, daarna `[test_gate] command` en `go run ./cmd/lighthouse imports check` vanaf de root — alles groen (de uitvoer van `commit-test-gate` na de laatste commit telt).
- [ ] **Step 2: Loop de acceptatiecriteria van #557 na** tegen de diff (zes kolommen, kaart, runner, sortering, *done*, filter en opslag, getal op de tab); controleer of `web/dist` of schermafbeeldingen in de repo staan en herbouwd moeten worden (`git ls-files web/dist docs | grep -i -E 'dist|png'`).
