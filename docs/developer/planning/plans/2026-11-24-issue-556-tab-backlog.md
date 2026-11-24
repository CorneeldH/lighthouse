# Tab Backlog Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** De tab *Without milestone* wordt de eigen tab *Backlog*, met per regel het statusblokje uit #555 en een onderstreept nummer bij needs shaping en needs human (#556).

**Architecture:** Alleen web. `ViewId` krijgt `'backlog'` in plaats van `'unassigned'`; `loadPrefs` zet een bewaarde `'unassigned'` om naar `'backlog'`. `UnassignedView` wordt `BacklogView` en hergebruikt `Block` zoals `NodeCard` dat doet. De inhoud van de lijst (`withoutMilestone()` in `derive.ts`) blijft ongewijzigd. De spec volgt de gewijzigde koers.

**Tech Stack:** React, TypeScript, Vitest, Testing Library (`web/`).

**Spec:** [`2026-11-23-prioritering-zes-statussen-design.md`](../specs/2026-11-23-prioritering-zes-statussen-design.md), secties 5 en 7 en *Opdeling*. De spec is door de koers uit issue #556 deels achterhaald; taak 4 werkt haar bij.

## Global Constraints

- Code, commentaar en testnamen in het Engels; spec, plan, changelog en commitberichten in het Nederlands, tenzij de bestaande bestanden anders doen (`changelog.d/` is Engels, zie het voorbeeld `issue-375-inzet-kosten-tokens.md`).
- `ViewId` is exact `'deps' | 'milestones' | 'backlog' | 'status'`. `'status'` komt van #557; dit issue voegt `'backlog'` toe en laat `'status'` aan #557 (wie als tweede merget, voegt samen).
- Tabvolgorde: Dependencies · Milestones · Backlog · Status. De tab heet exact `Backlog`.
- Geen nieuw label, geen nieuwe Status, geen schrijven vanuit de web-UI, geen modelsuggesties (#439).
- Nooit `--no-verify`. Voor elke commit: `[test_gate] command` uit `lighthouse.toml` en `lighthouse imports check`.
- De legenda-regel *Without milestone* (PrioritiesTab.tsx, legenda) is een legendalabel, geen tabnaam, en blijft.

## Review Focus

- Bewaarde `view: 'unassigned'` in `localStorage` opent *Backlog*, niet *Dependencies* (de huidige stille terugval).
- Rij met `needs_shaping`/`needs_human`: nummer onderstreept; andere statussen niet.
- Rij in een gesloten milestone: `<titel> · closed` én blokje blijven gelijktijdig zichtbaar.
- Lege backlog: één lege-staat-regel, geen lege `<ul>`; het getal op de tab is 0.
- Getal op de tab is gelijk aan het aantal regels bij een repo-filter dat een deel uitsluit.

## Bestanden

- Hernoem: `web/src/components/priorities/UnassignedView.tsx` → `BacklogView.tsx`, `UnassignedView.test.tsx` → `BacklogView.test.tsx`
- Wijzig: `web/src/lib/priorities/prefs.ts`, `web/src/lib/priorities/prefs.test.ts`
- Wijzig: `web/src/components/priorities/Toolbar.tsx`, `PrioritiesTab.tsx`, `PrioritiesTab.test.tsx`, `types.ts` (alleen een commentaar)
- Wijzig: `docs/developer/planning/specs/2026-11-23-prioritering-zes-statussen-design.md`, `docs/priorities.md`
- Maak: `changelog.d/issue-556-tab-backlog.md`

---

### Task 1: `ViewId` en voorkeur-migratie

**Files:**
- Modify: `web/src/lib/priorities/prefs.ts`
- Test: `web/src/lib/priorities/prefs.test.ts`

**Interfaces:**
- Produces: `type ViewId = 'deps' | 'milestones' | 'backlog'`; `VIEWS` bevat `'backlog'`; `loadPrefs()` geeft `view: 'backlog'` voor een bewaarde `'unassigned'`.

- [ ] **Step 1: Schrijf de falende tests** in `prefs.test.ts`: `it('opens Backlog for a stored view "unassigned"')` (zet `localStorage` `lighthouse.priorities` op `{"view":"unassigned"}`, verwacht `loadPrefs().view === 'backlog'`) en `it('keeps a stored view "backlog"')`. Pas de bestaande test op regel ~39 aan van `'unassigned'` naar `'backlog'`.
- [ ] **Step 2: Draai** `cd web && npx vitest run src/lib/priorities/prefs.test.ts` — verwacht FAIL (typecheck/waarde).
- [ ] **Step 3: Implementeer** in `prefs.ts`: `ViewId` en `VIEWS` met `'backlog'` in plaats van `'unassigned'`; in `loadPrefs`, vóór de `VIEWS.includes`-check, `const view = p.view === 'unassigned' ? 'backlog' : p.view` en valideer die. Commentaar: legacy value from before the tab was renamed.
- [ ] **Step 4: Draai** dezelfde test — verwacht PASS. (De rest van de web-typecheck is nu rood; taak 2 herstelt dat in dezelfde reeks. Commit daarom pas na taak 2.)

### Task 2: Tab *Backlog* en `BacklogView` met statusblokje

**Files:**
- Rename + modify: `UnassignedView.tsx` → `BacklogView.tsx`, `UnassignedView.test.tsx` → `BacklogView.test.tsx` (`git mv`)
- Modify: `Toolbar.tsx:40-44`, `PrioritiesTab.tsx:36-63,83,96`, `types.ts:5`, `PrioritiesTab.test.tsx`

**Interfaces:**
- Consumes: `ViewId` uit taak 1; `Block` (`state`, `color`, `title`, `testId`, `className`) uit `Block.tsx`; `UnassignedRow` en `withoutMilestone()` uit `derive.ts` (ongewijzigd).
- Produces: `export function BacklogView(props: ViewProps): JSX.Element` met testids `view-backlog` (container), `backlog-row` (per regel), `backlog-status` (het blokje) en `data-underlined="true"` op het nummer bij `needs_shaping`/`needs_human`.

- [ ] **Step 1: Schrijf de falende tests** in `BacklogView.test.tsx` (hernoemde bestand; pas bestaande testids en imports aan):
  - `it('shows a status block per row, with the state of the node')`: rij voor issue 5 bevat `backlog-status` met `data-state`-equivalent van `node.state` (kijk hoe `Block.test.tsx` de state uitleest en gebruik dat).
  - `it('underlines the number only for needs_shaping and needs_human')`: twee issues, één `needs_label: true`-variant (zie de fixture `iss`), assert `data-underlined` aanwezig op de ene, afwezig op de andere.
  - `it('shows an empty-state line, no list, without issues')`: geen `<ul>`, tekst `No open issues in the backlog.`.
  - `it('keeps "<title> · closed" next to the block for an issue in a closed milestone')`.
  Pas in `PrioritiesTab.test.tsx` alle tabnamen aan van `Without milestone` naar `Backlog` (regels ~82, 187, 249-252, 325-331); regel ~227-233 (legenda) blijft. Voeg `it('shows the row count on the Backlog tab')` toe met een repo-filter dat een deel uitsluit.
- [ ] **Step 2: Draai** `cd web && npx vitest run src/components/priorities` — verwacht FAIL.
- [ ] **Step 3: Implementeer.** `git mv` beide bestanden. In `BacklogView.tsx`: component en testids hernoemen; vóór het nummer `<Block testId="backlog-status" className="size-3.5 flex-none" state={node.state} color={…} title={node.title} />`, het nummer krijgt de onderstreping als in `NodeCard.tsx:72-75` (zelfde `UNDERLINED`-logica, `data-underlined`). Kleur: dezelfde bron als `NodeCard` (de kleur van de gesloten milestone uit `colors`, anders de grijze *Without milestone*-kleur); volg hoe `PrioritiesTab` `colors` voor `inViewMilestones.unassigned` vult. Lege-staat-tekst: `No open issues in the backlog.` Relatie-pil en `· closed` blijven zoals ze zijn. In `Toolbar.tsx`: `{ id: 'backlog', label: 'Backlog' }`. In `PrioritiesTab.tsx`: de sleutel `unassigned` in `hasUnassigned`, `counts`, `inViewMilestones` en de view-switch wordt `backlog`; het getal blijft `unassigned.length`. Commentaar in `types.ts:5` bijwerken.
- [ ] **Step 4: Draai** `cd web && npx vitest run src/components/priorities src/lib/priorities && npm run typecheck && npm run lint` — verwacht alles groen.
- [ ] **Step 5: Commit** (taak 1 en 2 samen, want de typecheck hangt aan beide):

```bash
git add web/src
git commit -m "feat(web): tab Without milestone heet Backlog, met statusblokje per regel (#556)"
```

### Task 3: Documentatie en changelog

**Files:**
- Modify: `docs/priorities.md` (regels ~105 en ~169)
- Create: `changelog.d/issue-556-tab-backlog.md`

- [ ] **Step 1: Pas** `docs/priorities.md` aan: waar de tab *Without milestone* of de inhoud ervan genoemd wordt, noem *Backlog*, de regelopbouw en dat een bewaarde oude voorkeur *Backlog* opent. Controleer ook de wording van de vermelding bij *Milestones*.
- [ ] **Step 2: Maak** `changelog.d/issue-556-tab-backlog.md` in het formaat van het voorbeeld (`type: changed`, Engelse zin, `(#556)`): the *Without milestone* tab is now *Backlog* and shows a status block per row.
- [ ] **Step 3: Controleer** `.claude/hooks/lighthouse/run.sh md-link-check` slaagt (geen absolute of dode links) en commit:

```bash
git add docs/priorities.md changelog.d/issue-556-tab-backlog.md
git commit -m "docs(priorities): tab Backlog in de gebruikersdocumentatie en changelog (#556)"
```

### Task 4: Spec bijwerken

**Files:**
- Modify: `docs/developer/planning/specs/2026-11-23-prioritering-zes-statussen-design.md`

- [ ] **Step 1: Herschrijf in het Nederlands**, zonder de besluitgeschiedenis weg te poetsen (één regel *Koers gewijzigd 2026-11-24, besluit Corneel*): doelen-regel (~35-36): vier tabs **Dependencies · Milestones · Backlog · Status**; sectie 5: titel *Backlog*, de tab blijft een eigen tab, de lijst met blokje, nummer, titel, relatie en `<titel> · closed` (de rij *Without milestone* in de balk, het inklappen na 5 en de dichte rand vervallen); de plek voor #439 staat in de tab Backlog met een knop om suggesties direct op te vragen; sectie 7: `ViewId` is `'deps' | 'milestones' | 'backlog' | 'status'` en een bewaarde `'unassigned'` opent *Backlog*; *Opdeling* rij b: *de tab Without milestone heet Backlog en blijft eigen tab; sectie 5 en 7 volgen*; "Keuzes" 5, 9 en 11 en de regels ~145 en ~209-217 (kanban en legenda die de rij *Without milestone* noemen) in lijn met de nieuwe tekst, waar ze de rij in Milestones veronderstellen.
- [ ] **Step 2: Controleer** `git grep -n "Without milestone" docs/developer/planning/specs/2026-11-23-prioritering-zes-statussen-design.md`: elke overgebleven treffer is bewust (legenda, kanban-chip). `md-link-check` slaagt.
- [ ] **Step 3: Commit**

```bash
git add docs/developer/planning/specs/2026-11-23-prioritering-zes-statussen-design.md
git commit -m "docs(spec): tab Backlog in plaats van een blok onder Milestones (#556)"
```

### Task 5: Eindcontrole

- [ ] **Step 1: Draai** `cd web && npm test && npm run typecheck && npm run lint`, daarna `[test_gate] command` uit `lighthouse.toml` en `lighthouse imports check` vanaf de root — alles groen.
- [ ] **Step 2: Loop de acceptatiecriteria van #556 na** tegen de diff (tab, inhoud, regel, `ViewId`, lege staat, spec); controleer of `web/dist` of schermafbeeldingen in de repo staan en herbouwd moeten worden (`git ls-files web/dist docs | grep -i -E 'dist|png'`).
