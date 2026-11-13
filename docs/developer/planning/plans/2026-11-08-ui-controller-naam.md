# Webpagina lighthouse met sectie Controller — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** De kop leest "lighthouse" met de versie als badge, en alles over de automatische runs staat onder een sectiekop "Controller".

**Architecture:** Een nieuw component `ControllerSection` omvat de bestaande lijsten en draagt de tick-leeftijd, die uit `Header` verhuist. `App` zet `UsagePanel` erboven. Geen router, geen tabs.

**Tech Stack:** React 19, TypeScript, Tailwind, TanStack Query, Vitest + Testing Library, Playwright.

**Spec:** [2026-11-08-ui-controller-naam-design.md](../specs/2026-11-08-ui-controller-naam-design.md)

## Global Constraints

- Sectiekop exact `Controller`; browsertitel exact `lighthouse · Controller` (middelpunt U+00B7).
- `UsagePanel` blijft boven de sectie, buiten de sectie.
- Volgorde binnen de sectie ongewijzigd: `Summary`, `QuestionList`, `ReturnedList`, `RunningBlock`, `ParkedBlock`, `QueueBlock`, `NightBlock`, `CleanupList`.
- "updated … ago" en Refresh blijven in `Header`; "tick … ago" staat alleen in de sectiekop.
- Geen router, geen tabs, geen `document.title` in code.
- Alleen bestaande `lh-*`-kleuren.
- UI-teksten, README en changelog-fragment in het Engels (zoals de bestaande); commits en dit plan in het Nederlands.
- Elke commit groen op `[test_gate] command` (lint, typecheck, test onder `web/`).

## Review Focus

- Versie laadt niet of faalt (`/api/health` 500): kop toont "lighthouse" zonder lege badge en zonder "undefined". Bestaande test `shows an alert on status 500 …` dekt het; Task 2 houdt hem groen.
- Geen runs en geen deferred starts: de sectiekop toont geen "tick". Test in Task 1.
- `/api/runs` faalt: de sectiekop staat er toch, zonder tick. Test in Task 1.
- Smal scherm: de sectiekop en de tick lopen over zonder horizontale scroll (`flex-wrap`). Visuele controle in Task 1 stap 6.
- Schermlezer: de sectie is één landmark met naam "Controller", en de `Summary`-links (`#questions` …) werken nog. Test in Task 1 en de bestaande e2e.

---

### Task 1: ControllerSection met de tick, en de browsertitel

**Files:**
- Create: `web/src/components/ControllerSection.tsx`
- Create: `web/src/components/ControllerSection.test.tsx`
- Modify: `web/src/components/Header.tsx` (tick weg; `age` naar `format.ts`)
- Modify: `web/src/components/Header.test.tsx` (de twee tick-tests verhuizen)
- Modify: `web/src/lib/format.ts`, `web/src/lib/format.test.ts`
- Modify: `web/src/App.tsx`
- Modify: `web/index.html`
- Modify: `web/e2e/ui.e2e.ts`

**Interfaces:**
- Produces: `formatAgo(now: number, then: number): string` in `@/lib/format` — `` `${formatAge(Math.max(0, (now - then) / 1000))} ago` ``, de huidige `age` uit `Header.tsx`.
- Produces: `ControllerSection({ children }: { children: ReactNode }): JSX.Element` — `<section aria-labelledby="controller-heading">` met een kopregel (`h2 id="controller-heading"` "Controller", rechts "tick … ago") en daaronder `children`.

- [ ] **Stap 1: Schrijf de falende tests**

`format.test.ts`: `formatAgo(130_000, 10_000)` geeft `'2 min ago'`; `formatAgo(0, 5_000)` (toekomst) geeft `'< 1 min ago'`.

`ControllerSection.test.tsx` (zelfde `json`/`stubGlobal`-opzet als `Header.test.tsx`, render binnen `QueryClientProvider` met `newQueryClient()`, kind `<p>child</p>`):
- `it('is a region named Controller around its children')`: `screen.getByRole('region', { name: 'Controller' })` bevat `child`; `getByRole('heading', { level: 2, name: 'Controller' })` bestaat.
- `it('shows the age of the newest tick over runs and deferred starts')`: verhuisd uit `Header.test.tsx`, zelfde stub, verwacht `'tick 2 min ago'`.
- `it('shows no tick without runs or deferred starts')`: verhuisd; wacht tot `queryKeys.runs` data `{ runs: [], deferred: [] }` heeft, dan `queryByText(/tick/)` is `null`.
- `it('shows the heading without a tick when /api/runs fails')`: `/api/runs` geeft 500; heading "Controller" staat er, `queryByText(/tick/)` is `null` nadat de query faalt (`waitFor` op `client.getQueryState(queryKeys.runs)?.status === 'error'`).

`Header.test.tsx`: verwijder de twee tick-tests; voeg toe `it('shows no tick, even with runs')`: stub met runs als in de verhuisde test, wacht op `'1.2.3'` en op runs-data, dan `queryByText(/tick/)` is `null`.

- [ ] **Stap 2: Draai de tests, verwacht rood**

Run: `cd web && npx vitest run src/components/ControllerSection.test.tsx src/components/Header.test.tsx src/lib/format.test.ts`
Expected: FAIL — `ControllerSection` en `formatAgo` bestaan niet; Header toont nog een tick.

- [ ] **Stap 3: Implementeer**

- `formatAgo` in `format.ts`; `Header` gebruikt hem voor "updated … ago" en verliest `useRuns`, `tickAt` en de tick-`span`.
- `ControllerSection`: tick-berekening uit de oude `Header` (max van `Date.parse(r.at)` over `runs` en `deferred`, `NaN` gefilterd), met `useNow()`. Kopregel `flex flex-wrap items-baseline justify-between gap-2`; tick in `text-sm` met `aria-live="polite"`.
- `App.tsx`: `<Header />`, `<UsagePanel />`, dan `<ControllerSection>` met de acht onderdelen in de volgorde uit Global Constraints. De sectie krijgt `space-y-6` zodat de afstand gelijk blijft.
- `web/index.html`: `<title>lighthouse · Controller</title>`. `internal/web/placeholder/index.html` blijft `lighthouse`: die pagina is niet de Controller.
- `ui.e2e.ts`, test `full: …`: na `page.goto` `await expect(page).toHaveTitle('lighthouse · Controller')` en `await expect(page.getByRole('region', { name: 'Controller' })).toBeVisible()`.

- [ ] **Stap 4: Draai de tests, verwacht groen**

Run: `cd web && npm run lint && npm run typecheck && npm test`
Expected: alles PASS.

- [ ] **Stap 5: Draai de e2e**

Run: `cd web && npm run e2e`
Expected: PASS. Faalt de start van `lighthouse serve` op de omgeving (geen Go-build, poort), meld dat met de foutregel; CI draait hem ook.

- [ ] **Stap 6: Kijk op smal scherm**

Start de dev-server zoals `web/README.md` beschrijft, of `npm run e2e` met `web/e2e/screenshot.mjs`; bekijk op 375 px breed. Verwacht: kopregel loopt over, geen horizontale scroll.

- [ ] **Stap 7: Commit**

```bash
git add web/src web/index.html web/e2e/ui.e2e.ts
git commit -m "feat(web): sectie Controller met de tick, titel lighthouse · Controller"
```

### Task 2: Versie als badge in de kop

**Files:**
- Modify: `web/src/components/Header.tsx`
- Modify: `web/src/components/Header.test.tsx`

**Interfaces:**
- Consumes: niets uit Task 1 behalve dat de tick al weg is uit `Header`.

- [ ] **Stap 1: Schrijf de falende test**

In `it('shows the version')`: naast `findByText('1.2.3')` ook `expect(screen.getByRole('heading', { level: 1 }).textContent).toBe('lighthouse')` — de versie staat niet meer in de `h1`.

- [ ] **Stap 2: Draai de test, verwacht rood**

Run: `cd web && npx vitest run src/components/Header.test.tsx`
Expected: FAIL — `textContent` is `'lighthouse1.2.3'`.

- [ ] **Stap 3: Implementeer**

`h1` bevat alleen `lighthouse`. Ernaast, in dezelfde `flex items-center gap-2`-rij, de versie als `<span>` met `rounded-full bg-lh-yellow px-2 py-0.5 text-xs font-medium text-lh-navy`, alleen als `data` er is. De alert bij een fout blijft onder de rij.

- [ ] **Stap 4: Draai de tests, verwacht groen**

Run: `cd web && npm run lint && npm run typecheck && npm test`
Expected: alles PASS, ook `shows an alert on status 500 without "lighthouse undefined"`.

- [ ] **Stap 5: Commit**

```bash
git add web/src/components/Header.tsx web/src/components/Header.test.tsx
git commit -m "feat(web): versie als badge naast lighthouse"
```

### Task 3: README en changelog

**Files:**
- Modify: `README.md:25`
- Create: `changelog.d/fix-ui-controller-naam.md`

- [ ] **Stap 1: README**

Regel 25 wordt (Engels, zoals de README): "The page has the Claude and Codex usage windows on top, and below them the Controller section: the open questions, the returned issues and the runs of the dispatcher." De screenshot `docs/images/ui-m2.png` blijft staan (de bouwer mag hem vernieuwen met `web/e2e/screenshot.mjs`).

- [ ] **Stap 2: Changelog-fragment**

```markdown
---
type: changed
---
The `lighthouse serve` page is named lighthouse, with the version as a badge in the header.
Everything about the automatic runs sits under the Controller section heading, which also shows
the age of the last tick. The browser title is `lighthouse · Controller`.
```

`changed` is geldig volgens `docs/releasing.md`.

- [ ] **Stap 3: Commit**

```bash
git add README.md changelog.d/fix-ui-controller-naam.md
git commit -m "docs: README en changelog voor de sectie Controller"
```
