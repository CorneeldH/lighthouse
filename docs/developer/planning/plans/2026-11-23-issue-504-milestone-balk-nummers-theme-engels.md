# Milestone-balk: nummers, thema-knop en Engelse UI-tekst — implementatieplan

> **Voor agentic workers:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkbox-syntaxis (`- [ ]`).

**Doel:** Nummers in blokjes zonder `#` en leesbaar in licht en donker, een knop systeem/licht/donker in de header, en alle UI-tekst in `web/src` Engels.

**Architectuur:** Het nummer komt op een ondoorzichtige lichte chip met donkere tekst (tokens `--lh-chip-bg` / `--lh-chip-fg`, in beide thema's gelijk), zodat het contrast niet van de milestonekleur afhangt. Het thema is een `data-theme`-attribuut op `<html>` (`light` of `dark`; afwezig = systeem); de `dark`-variant en het donkere tokenblok reageren op dat attribuut en, zonder attribuut, op `prefers-color-scheme`. De Engelse tekst is een gerichte vervanging per bestand, met de tests mee.

**Tech stack:** React, Tailwind v4 (`@custom-variant`), Vitest + Testing Library, Playwright (e2e), lucide-react.

**Spec:** geen aparte spec; de issue (#504) is de bron. Besluiten die de issue openlaat staan onder *Besluiten*.

## Besluiten

- Graafkaart (`DepsView.tsx`): AC 1 eist het nummer zonder `#` ook in de graaf. Zonder repo-voorvoegsel alleen `nr`; met voorvoegsel (of een grijze externe node) `repo nr`, gescheiden door een spatie, dus nooit een `#` (het hover-`title` blijft `#nr titel`).
- Oude bladwijzer `#prioritering` krijgt geen redirect: de grep-eis (AC 7) sluit de oude term in `web/src` uit, en het dashboard is een lokaal hulpmiddel.
- Naamgeving: tab `Priorities`, hash `#priorities`, tab-id `'priorities'`.

## Globale randvoorwaarden

- Code, UI-tekst en commentaar in `web/src` zijn Engels; artefacten (dit plan, commits, PR) Nederlands.
- Hover-titel blijft `#nr titel` (`MilestonesView.tsx:143,154,180`).
- Contrast nummer/chip ≥ 4,5:1 in licht en donker, voor de toestanden `done`, `active`, `ready`, `blocked` en `needsHuman`.
- Standen: `system` (standaard), `light`, `dark`; opslag in `localStorage`, key `lighthouse.theme`, lezen/schrijven in try/catch (patroon `web/src/lib/priorities/prefs.ts:13-34`).
- Voor elke commit: `cd web && npm run test && npm run lint && npm run typecheck`, en `lighthouse imports check` naast `[test_gate] command`.

## Review Focus

- `localStorage` geeft een fout of een onbekende waarde (`"purple"`): de knop valt terug op `system`, geen crash.
- Stand `system` zonder opgeslagen waarde mag geen `data-theme` zetten (anders volgt de pagina `prefers-color-scheme` niet meer).
- De twee donkere tokenblokken in `index.css` (attribuut en media-query) lopen uiteen: een test vergelijkt ze.
- Nummer met meer cijfers (`#1234`) in een smal blokje: chip blijft binnen het blokje (`overflow-hidden` blijft).
- Geblokkeerd blokje (`lh-cross`) en actief blokje (`lh-stripe`): de chip dekt de achtergrond volledig af.

---

### Taak 1: UI-tekst Engels

**Bestanden:**
- Wijzig: `web/src/components/Tabs.tsx:3,7,11`, `web/src/App.tsx:15`, `web/src/components/priorities/Toolbar.tsx:37-40,73`, `web/src/components/priorities/MilestonesView.tsx:42,58,59,68,69,122,127,172,205`, `web/src/lib/queries.ts:68,104,106`
- Test: `Tabs.test.tsx`, `priorities/PrioritiesTab.test.tsx`, `priorities/MilestonesView.test.tsx`, `lib/queries.test.tsx` (plekken in de scout: `Tabs.test.tsx:16-38`, `PrioritiesTab.test.tsx:121,231-253,272,283,286`, `MilestonesView.test.tsx:220-243,297-357`, `queries.test.tsx:10,273-274,362`)

**Interfaces:**
- Produceert: `TabId = 'controller' | 'priorities'`; hash `#priorities`; `RANK_CONFLICT_TEXT` (Engels, exacte tekst: `The order was just changed elsewhere; the list has been refreshed.`).

- [ ] **Stap 1: Tests omzetten naar de Engelse namen (rood)**
  Pas de bestaande tests aan: tabnaam `Priorities`, hash `#priorities`, schakelaar `Computed` | `Priority`, groep `Order`, knoppen `Up` / `Down` met aria-label `… up` / `… down`, title `place in the repo`, sr-only `, place N`, `provisional`, `no issues yet`, conflicttekst en de `Opslaan mislukt`-meldingen (`queries.test.tsx:291` en de andere) als `Save failed: …` / `Save failed: server unreachable`.
- [ ] **Stap 2: Draai de tests, verwacht rood**
  Run: `cd web && npx vitest run src/components src/lib/queries.test.tsx` — verwacht FAIL op de oude namen.
- [ ] **Stap 3: Vervang de tekst in de bestanden hierboven**
  Alleen strings, `title`, `aria-label`, sr-only en de tab-id; commentaar dat naar `stand Berekend` / `stand Prioriteit` verwijst wordt `Computed mode` / `Priority mode`.
- [ ] **Stap 4: Sweep op verdwaalde Nederlandse tekst**
  Run: `cd web && git grep -nEi "prioritering|prioriteit|berekend|volgorde|omhoog|omlaag|plaats|voorlopig|nog geen|opslaan|onbereikbaar|ververst|mislukt" -- src ':!*.test.*'` — verwacht alleen Engelse treffers (`Priority`) of niets. Lees daarna de overige componenten in `web/src/components` en `web/src/lib` door op andere Nederlandse tekst en vervang die ook; noteer in de commit wat er extra bij kwam.
- [ ] **Stap 5: Tests groen, commit**
  Run: `cd web && npm run test && npm run lint && npm run typecheck` — verwacht PASS.
  ```bash
  git add web/src
  git commit -m "feat(web): alle UI-tekst Engels, tab Priorities (#504)"
  ```

### Taak 2: Nummer zonder `#` op een lichte chip

**Bestanden:**
- Wijzig: `web/src/components/priorities/Block.tsx:46`, `web/src/components/priorities/DepsView.tsx:139,171`, `web/src/components/priorities/MilestonesView.tsx:194` (to-do-lijst), `web/src/index.css` (tokens in `:root` en in het donkere blok)
- Test: `priorities/Block.test.tsx`, `priorities/MilestonesView.test.tsx:144,163,209`, `priorities/DepsView.test.tsx:48`, nieuw `web/src/lib/contrast.test.ts`

**Interfaces:**
- Produceert: CSS-tokens `--lh-chip-bg: #f4f5f8` en `--lh-chip-fg: #14213d` (in licht én donker dezelfde waarde); `contrastRatio(fg: string, bg: string): number` als testhelper in `contrast.test.ts` (hex in, WCAG-verhouding uit).

- [ ] **Stap 1: Tests aanpassen en toevoegen (rood)**
  - `MilestonesView.test.tsx`: verwacht chips `['1','2']`, `'1'` en en regel 209 `'to do:324'` (nu `'to do:#3#2#4'`; de to-do-lijst rendert zijn eigen `#{b.nr}` en wordt in stap 3 aangepast); de titels (`#nr titel`) blijven ongewijzigd.
  - `DepsView.test.tsx:48`: `getByText('1')`; pas het voorvoegselgeval aan naar `getByText(`${A} 1`)` en assert dat geen label een `#` bevat.
  - `contrast.test.ts`: leest `index.css`, haalt `--lh-chip-bg` en `--lh-chip-fg` uit `:root` én uit het donkere blok, en asserteert `contrastRatio(fg, bg) >= 4.5` voor allebei. De CSS wordt gelezen met `import css from '../index.css?raw'` (typt via `vite/client`; assert eerst dat de string niet leeg is). Een Block-test (`Block.test.tsx`, nieuw of bestaand) asserteert dat de gerenderde chip de klassen `bg-[var(--lh-chip-bg)]` en `text-[var(--lh-chip-fg)]` draagt en geen `bg-background/70`, voor `done`, `active`, `ready`, `blocked` en `needsHuman`.
- [ ] **Stap 2: Draai, verwacht rood**
  Run: `cd web && npx vitest run src/components/priorities src/lib/contrast.test.ts`
- [ ] **Stap 3: Implementeer**
  `Block.tsx`: chip toont `{label}` zonder `#`, klassen `rounded-sm bg-[var(--lh-chip-bg)] px-0.5 text-[var(--lh-chip-fg)]`. `DepsView.tsx`: label is `String(n.number)` zonder voorvoegsel, `` `${n.repo} ${n.number}` `` met voorvoegsel of `grey`. `MilestonesView.tsx:194`: `{b.nr}` in plaats van `#{b.nr}` in de to-do-lijst. Tokens toevoegen aan `:root` en aan het donkere blok.
- [ ] **Stap 4: Tests groen, commit**
  ```bash
  git add web/src
  git commit -m "feat(web): issuenummer zonder # op lichte chip met contrast >= 4,5 (#504)"
  ```

### Taak 3: Thema-knop in de header

**Bestanden:**
- Maak: `web/src/lib/theme.ts`, `web/src/lib/theme.test.ts`, `web/src/components/ThemeToggle.tsx`, `web/src/components/ThemeToggle.test.tsx`
- Wijzig: `web/src/index.css:6-7` (regel 6 is het commentaar "the page has no toggle", dat vervalt) en het donkere tokenblok (`:106-151`), `web/src/components/Header.tsx:55-64` (naast Refresh), `web/index.html` (inline script tegen flits; controleer eerst of de bundel een CSP heeft), `web/e2e/ui.e2e.ts:144-158`
- Test: `web/src/components/Header.test.tsx`

**Interfaces:**
- Produceert in `theme.ts`: `type ThemeMode = 'system' | 'light' | 'dark'`; `THEME_KEY = 'lighthouse.theme'`; `loadTheme(): ThemeMode` (onbekend of fout → `'system'`); `saveTheme(mode: ThemeMode): void`; `applyTheme(mode: ThemeMode): void` (zet `document.documentElement.dataset.theme` bij `light`/`dark`, verwijdert het attribuut bij `system`).
- Produceert: `ThemeToggle(): JSX.Element` — groep `role="group"` `aria-label="Theme"` met drie knoppen `System`, `Light`, `Dark` (`aria-pressed`), icons `Monitor`, `Sun`, `Moon` uit lucide-react.

- [ ] **Stap 1: Schrijf de falende tests**
  - `theme.test.ts`: `loadTheme()` geeft `'system'` zonder opslag, `'dark'` na `saveTheme('dark')`, `'system'` bij waarde `"purple"` en bij een `localStorage` die gooit; `applyTheme('dark')` zet `data-theme="dark"`, `applyTheme('system')` verwijdert het attribuut.
  - `ThemeToggle.test.tsx`: standaard is `System` ingedrukt; klik op `Dark` zet `data-theme="dark"` en `localStorage['lighthouse.theme'] === 'dark'`; opnieuw renderen na een opgeslagen `light` toont `Light` ingedrukt (bewaren na herladen).
  - `Header.test.tsx`: de header bevat de groep `Theme`.
  - `contrast.test.ts` (uit taak 2) uitbreiden: de declaraties van het donkere blok met `[data-theme="dark"]` en dat met `@media (prefers-color-scheme: dark)` zijn identiek.
- [ ] **Stap 2: Draai, verwacht rood**
  Run: `cd web && npx vitest run src/lib src/components/ThemeToggle.test.tsx src/components/Header.test.tsx`
- [ ] **Stap 3: Implementeer `theme.ts` en `ThemeToggle.tsx`, en plaats de knop in `Header.tsx`**
  Patroon van `prefs.ts` voor opslag; componentstate geïnitialiseerd met `loadTheme()`, bij wijziging `saveTheme` + `applyTheme`; ook bij eerste render `applyTheme(loadTheme())` in een effect.
- [ ] **Stap 4: CSS**
  `@custom-variant dark` krijgt twee regels: `:where([data-theme="dark"], [data-theme="dark"] *)` en, binnen `@media (prefers-color-scheme: dark)`, `:where(:root:not([data-theme="light"]) *)`. Het donkere tokenblok staat twee keer: onder `:root[data-theme="dark"]` en onder `@media (prefers-color-scheme: dark) { :root:not([data-theme="light"]) { … } }`, met `color-scheme: dark` in beide.
- [ ] **Stap 5: Inline script in `web/index.html`**
  Zet vóór de eerste paint `data-theme` uit `localStorage['lighthouse.theme']` als de waarde `light` of `dark` is; in try/catch.
- [ ] **Stap 6: e2e**
  Voeg aan `ui.e2e.ts` toe: stand `Dark` bij emulatie `light` geeft achtergrond `rgb(15, 20, 32)`; stand `Light` bij emulatie `dark` geeft `oklch(1 0 0)`-equivalent; herladen behoudt de keuze. In beide forced-standen ook de Controller-kop (`dark:`-consument, `ControllerSection.tsx:15`): `rgb(232, 234, 240)` bij `Dark`, `rgb(20, 33, 61)` bij `Light`; dat bewijst dat de `@custom-variant dark` op `data-theme` reageert. Bestaande systeem-asserties blijven.
- [ ] **Stap 7: Verifieer en commit**
  Run: `cd web && npm run test && npm run lint && npm run typecheck && npm run build`, daarna vanaf de repo-root `go build -o /tmp/lh ./cmd/lighthouse` en `cd web && npx playwright install chromium && LIGHTHOUSE_BIN=/tmp/lh npm run e2e` (zie `web/README.md`, *End-to-end tests*; zonder `LIGHTHOUSE_BIN` stopt Playwright direct) — verwacht PASS.
  ```bash
  git add web
  git commit -m "feat(web): knop systeem/licht/donker in de header (#504)"
  ```

### Taak 4: Documentatie en changelog

**Bestanden:**
- Wijzig: `README.md:33`, `docs/priorities.md:1,3,48,57-59`
- Maak: `changelog.d/issue-504-milestone-balk-nummers-zonder-en-beter.md`

- [ ] **Stap 1: Docs bijwerken**
  `README.md`: de dashboard-zin noemt de knop systeem/licht/donker rechtsboven. `docs/priorities.md` (Nederlands): tabnaam `Priorities`, hash `#priorities`, schakelaar `Computed` | `Priority`, knoppen `Up` / `Down`.
- [ ] **Stap 2: Changelogfragment**
  Frontmatter en formaat zoals `changelog.d/issue-19-instapniveaus-level.md`, `type: changed`, één Engelse zin: `Dashboard: issue numbers without # on a light chip, a system/light/dark switch in the header, all UI text in English.`
- [ ] **Stap 3: Links controleren en commit**
  Run: `.claude/hooks/lighthouse/run.sh md-link-check` is een hook; controleer handmatig dat alle links in de gewijzigde `.md` relatief zijn en bestaan.
  ```bash
  git add README.md docs changelog.d
  git commit -m "docs(web): thema-knop en Engelse namen van tab en schakelaar (#504)"
  ```

## Zelfreview

- AC 1 → taak 2 · AC 2 → taak 2 (contrasttest) · AC 3, 4, 5 → taak 3 · AC 6 → taak 1 (stap 4 sweep) · AC 7 → taak 1–3 · DoD documentatie → taak 4.
- Typeconsistentie: `ThemeMode`, `THEME_KEY`, `loadTheme`, `saveTheme`, `applyTheme` komen alleen uit taak 3 en worden alleen daar gebruikt.
- Open risico: of `index.html` een CSP heeft die het inline script blokkeert; de uitvoerder controleert dat in stap 5 en valt anders terug op `applyTheme` in `main.tsx` vóór `createRoot`.
