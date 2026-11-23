# Afhankelijkheden sorteren op prio-gewicht — implementatieplan

> **Voor agentic workers:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkbox-syntax (`- [ ]`).

**Doel:** de kolommen van de weergave *Afhankelijkheden* (Prioritering-tab) sorteren op prio-gewicht, gewicht of minste kruisingen, met een schakelaar die per browser onthouden wordt.

**Architectuur:** `buildGraph` in `derive.ts` krijgt per knoop `weight` (nieuw, lokaal berekend) plus `factor` en `prioWeight` (uit `/api/priorities`, niet nagerekend). `layoutGraph` krijgt een sorteermodus als parameter en sorteert daarmee de kolom. De modus staat in `Prefs` (`depsOrder`), gaat via `ViewProps` naar `DepsView` en wordt gekozen met een knoppengroep in `Toolbar`, naar het patroon van `msOrder`.

**Tech stack:** React, TypeScript, vitest, dagre (`web/`).

**Issue:** #445. **Spec:** geen aparte spec; de issue-tekst en milestone #16 zijn de bron. Het prio-gewicht zelf komt van #509 (`internal/prioweight/prioweight.go`, al gemerged; `Issue.factor` en `Issue.prio_weight` staan al in `web/src/api/lighthouse.ts`).

## Globale beperkingen

- Gewicht van een issue = som over alle issues die direct of via andere issues erop wachten van `2^(k-1)`, met `k` de **langste** afstand in stappen; elk wachtend issue telt één keer; een cyclus is één groep (alle leden tellen mee op de afstand van hun component), zoals de bestaande `waiting`-telling.
- `derive.ts` rekent het prio-gewicht **niet** na: het leest `issue.prio_weight` en `issue.factor`.
- Volgorde per modus: `prio` = prio-gewicht ↓, dan eigen factor ↓, dan dagre-volgorde; `weight` = gewicht ↓, dan dagre-volgorde; `crossings` = zoals nu (dagre-y, dan nummer).
- Standaard `prio`. De keuze staat in `localStorage` (sleutel `lighthouse.priorities`); zonder `localStorage` blijft `prio` de standaard en werkt de schakelaar nog.
- UI-tekst en changelog zijn Engels (bestaande UI is Engels); plan, docs en commitberichten zijn Nederlands.
- Geen wijziging in Go, `pack/` of OpenAPI: geen `init --upgrade` nodig.
- Voor elke commit: `lighthouse imports check` naast `[test_gate] command` (draait automatisch in `commit-test-gate`). Verificatie vanuit `web/`: `npm run lint && npm run typecheck && npm test`.

## Review focus

- Externe knopen (`blank()` in `derive.ts`) hebben geen factor/prio-gewicht: moeten 0 krijgen en niet crashen in de sortering.
- Gelijke sleutels over alle modi: de volgorde blijft deterministisch (dagre-y, dan nummer).
- Een diepe keten (k tot enkele tientallen): `2^(k-1)` blijft een eindig getal en de sortering werkt.
- Corrupte of vreemde waarde van `depsOrder` in `localStorage`: terug naar `prio`.
- Zelf-lus en cyclus met een uitgaande tak: gewicht telt de leden van de cyclus één keer.

## Bestandsstructuur

- `web/src/lib/priorities/derive.ts` — `GNode` + `buildGraph`: gewicht, factor, prioWeight.
- `web/src/lib/priorities/prefs.ts` — `Prefs.depsOrder`, validatie, standaard.
- `web/src/components/priorities/layout.ts` — `layoutGraph(nodes, edges, order)`.
- `web/src/components/priorities/types.ts`, `PrioritiesTab.tsx`, `Toolbar.tsx`, `DepsView.tsx`, `NodeCard.tsx` — schakelaar en pil.
- `docs/priorities.md`, `changelog.d/issue-445-afhankelijkheden-prio-gewicht.md`.

---

### Taak 1: gewicht, factor en prio-gewicht op `GNode`

**Bestanden:**
- Wijzig: `web/src/lib/priorities/derive.ts` (`GNode` l.6-22, `blank()` l.129-145, knopen vullen l.150-162, afrondende lus l.230-236)
- Test: `web/src/lib/priorities/derive.test.ts`; wijzig ook de `GNode`-literal in `web/src/components/priorities/layout.test.ts:18`

**Interfaces:**
- Produceert: `GNode.weight: number`, `GNode.factor: number`, `GNode.prioWeight: number`. Externe knopen: alle drie 0. Latere taken gebruiken deze namen.

- [ ] **Stap 1: schrijf de falende tests** in `derive.test.ts` (fixtures `R` en `n(g, key)` volgen zoals bij de `waiting`-tests l.46-98):
  - `weight: A met vijf issues op 1 stap is 5, B met twee op 1 en twee op 2 stappen is 6`
  - `weight: A→B→C geeft A = 3 (B op 1, C op 2, ook als C ook direct op A wacht)`
  - `weight: een cyclus telt als één groep` (cyclus X↔Y met Z erachter: X en Y hebben gewicht 1; een issue dat op de cyclus wacht telt beide leden: P→X, X↔Y ⇒ P = 2)
  - `weight: een extern blanco knoop heeft gewicht, factor en prioWeight naar eigen waarde 0 voor factor/prioWeight`
  - `factor en prioWeight komen uit issue.factor en issue.prio_weight zonder herberekening` (fixture met `prio_weight: 10`, `factor: 3` ⇒ node heeft 10 en 3)
- [ ] **Stap 2: draai de tests, verwacht falen.** `cd web && npx vitest run src/lib/priorities/derive.test.ts` → FAIL (`weight` is `undefined`).
- [ ] **Stap 3: implementeer.** Voeg de drie velden toe aan `GNode` en `blank()` (alle 0); kopieer `issue.factor` en `issue.prio_weight` in de knoop (`factor`, `prioWeight`). Bereken `weight` per component `c` in de afrondende stap: een langste-afstand-doorloop over `cadj` (de gecondenseerde DAG) vanuit `c`, bijvoorbeeld door in topologische volgorde `dist[d] = max(dist[d], dist[c'] + 1)` te relaxeren over de bereikbare componenten (afstand 1 voor directe opvolgers), en dan `weight[c] = Σ members[d].length * 2 ** (dist[d] - 1)` over elke bereikbare `d ≠ c`. Zelfde `O(C*(C+E))` als de `waiting`-lus; één keer per fetch.
- [ ] **Stap 4: pas `layout.test.ts:18` aan** (de `GNode`-literal krijgt `weight: 0, factor: 0, prioWeight: 0`) en eventuele andere `GNode`-literals die de typecheck meldt.
- [ ] **Stap 5: draai** `cd web && npm run typecheck && npx vitest run src/lib/priorities src/components/priorities/layout.test.ts` → PASS.
- [ ] **Stap 6: commit** `feat(web): gewicht, factor en prio-gewicht per knoop in de afhankelijkhedengraaf (#445)`.

### Taak 2: sorteermodus in `layoutGraph`

**Bestanden:**
- Wijzig: `web/src/components/priorities/layout.ts:36-58` (de kolomsortering l.52)
- Test: `web/src/components/priorities/layout.test.ts` (l.30-49 als voorbeeld)

**Interfaces:**
- Consumeert: `GNode.weight/factor/prioWeight` (taak 1).
- Produceert: `export type DepsOrder = 'prio' | 'weight' | 'crossings'` (in `layout.ts`) en `layoutGraph(nodes: GNode[], edges: Array<{from: NodeKey; to: NodeKey}>, order: DepsOrder = 'crossings'): LaidGraph`. De standaardwaarde `'crossings'` houdt bestaande aanroepen en tests ongewijzigd.

- [ ] **Stap 1: schrijf de falende tests** in `layout.test.ts`, elk met één kolom van drie knopen met bewust verschillende sleutels (de dagre-volgorde = nummervolgorde bij gelijke sleutels):
  - `prio: kolom staat op prioWeight aflopend, dan factor aflopend, dan dagre`
  - `weight: kolom staat op weight aflopend, bij gelijk gewicht in dagre-volgorde`
  - `crossings: de volgorde is die van dagre, ongeacht weight en prioWeight`
  - `prio: gelijke prioWeight én factor valt terug op dagre-volgorde`
- [ ] **Stap 2: draai, verwacht falen.** `cd web && npx vitest run src/components/priorities/layout.test.ts`.
- [ ] **Stap 3: implementeer** `DepsOrder` en de parameter; vervang de sortering l.52 door een vergelijker per modus die altijd eindigt op `(g.node(a.key)?.y ?? 0) - (g.node(b.key)?.y ?? 0) || a.number - b.number`.
- [ ] **Stap 4: draai** dezelfde test plus `npm run typecheck` → PASS.
- [ ] **Stap 5: commit** `feat(web): sorteermodus voor de kolommen van de afhankelijkhedengraaf (#445)`.

### Taak 3: schakelaar, voorkeur en pil

**Bestanden:**
- Wijzig: `web/src/lib/priorities/prefs.ts` (`Prefs`, `loadPrefs`), `web/src/components/priorities/types.ts` (`ViewProps`), `PrioritiesTab.tsx` (l.23, 62-65, 87, 96-106), `Toolbar.tsx` (l.19-20, 37-38, 75-82), `DepsView.tsx` (l.31-55), `NodeCard.tsx` (l.86-94)
- Test: `prefs.test.ts`, `PrioritiesTab.test.tsx` (patroon l.50, 105, 207, 250), `DepsView.test.tsx`

**Interfaces:**
- Consumeert: `DepsOrder` en `layoutGraph(..., order)` (taak 2); `GNode.weight/factor/prioWeight` (taak 1).
- Produceert: `Prefs.depsOrder: DepsOrder` (standaard `'prio'`); `ViewProps.depsOrder?: DepsOrder`; Toolbar-props `depsOrder: DepsOrder` en `onDepsOrder: (o: DepsOrder) => void`.

- [ ] **Stap 1: schrijf de falende tests.**
  - `prefs.test.ts`: `depsOrder staat standaard op prio`; `een opgeslagen weight of crossings wordt teruggelezen`; `een onbekende waarde valt terug op prio`; `een kapotte of geblokkeerde localStorage geeft prio` (bestaand patroon voor throwing localStorage).
  - `PrioritiesTab.test.tsx`: `de schakelaar Prio-weight · Weight · Fewest crossings staat in de weergave Dependencies met Prio-weight ingedrukt`; `klikken op Weight schrijft depsOrder: "weight" naar localStorage en herordent de kolom`; `zonder localStorage werkt de schakelaar nog`; de schakelaar is er niet bij de weergaven Milestones en Without milestone.
  - `DepsView.test.tsx`: `de pil toont het aantal wachtende issues; de title is "3 wait · weight 3 · factor 3 · prio-weight 10"` voor een knoop met die waarden.
- [ ] **Stap 2: draai, verwacht falen.** `cd web && npx vitest run src/lib/priorities/prefs.test.ts src/components/priorities`.
- [ ] **Stap 3: implementeer.** `prefs.ts`: `depsOrder` met validatie zoals `msOrder` (l.20), standaard `'prio'`. `Toolbar.tsx`: tweede knoppengroep (`role="group"`, `aria-label="Sort"`) met `aria-pressed`, alleen bij `view === 'deps'`, labels `Prio-weight`, `Weight`, `Fewest crossings`, in die volgorde. `PrioritiesTab.tsx`: `depsOrder` in de gedeelde `props` en `update({...prefs, depsOrder})`. `DepsView.tsx`: geef `depsOrder` door aan `layoutGraph` en neem hem op in de `useMemo`-afhankelijkheden. `NodeCard.tsx`: de pil blijft bij `waiting >= 2` met het aantal als inhoud; de `title` wordt `${waiting} wait · weight ${weight} · factor ${factor} · prio-weight ${prioWeight}`.
- [ ] **Stap 4: draai de hele set vanuit `web/`:** `npm run lint && npm run typecheck && npm test` → PASS. Controleer bestaande tests die de oude pil-`title` (`issues wait on this`) of de `Prefs`-vorm vastpinnen en pas die aan.
- [ ] **Stap 5: commit** `feat(web): schakelaar prio-gewicht · gewicht · minste kruisingen in de afhankelijkhedengraaf (#445)`.

### Taak 4: documentatie en changelog

**Bestanden:**
- Wijzig: `docs/priorities.md` (alinea *Wachtenden* l.127-129 en de alinea *Prio-gewicht* l.106-114: verwijs naar de sortering in de graaf)
- Maak: `changelog.d/issue-445-afhankelijkheden-prio-gewicht.md`, vorm als `changelog.d/issue-382-linter-aangesloten.md` (front matter `type: added`, één Engelse regel)

- [ ] **Stap 1: werk `docs/priorities.md` bij** (Nederlands): een alinea *Sortering van de graaf* met de gewichtsregel (2^(k-1), langste afstand, cyclus = één groep), de drie standen en hun volgorde, de standaard `Prio-weight`, de bewaring in `localStorage`, de inhoud van de pil-hover, en dat het prio-gewicht uit `/api/priorities` komt (#509) terwijl het gewicht in `derive.ts` over de hele graaf telt, externe knopen en andere repo's inbegrepen. Links relatief.
- [ ] **Stap 2: schrijf het changelogfragment**, bijvoorbeeld: `The Dependencies view sorts each column by prio-weight by default, with a switch to plain weight or fewest crossings; the pill hover shows weight, factor and prio-weight.`
- [ ] **Stap 3: draai** `lighthouse imports check` en `.claude/hooks/lighthouse/run.sh md-link-check` is automatisch via de hook; controleer dat er geen foutmelding komt.
- [ ] **Stap 4: commit** `docs(priorities): sortering van de afhankelijkhedengraaf op prio-gewicht (#445)`.

---

## Zelfreview

- AC1 (gewicht + tests A=5/B=6, A→B→C, cyclus): taak 1. AC2 (prio-gewicht uit API): taak 1, geen herberekening. AC3 (volgorde per modus): taak 2. AC4 (schakelaar, standaard, `localStorage`): taak 3. AC5 (pil en hover): taak 3.
- DoD "documentation updated": taak 4.
- Typeconsistentie: `DepsOrder` (taak 2) wordt in taak 3 gebruikt voor `Prefs.depsOrder`, `ViewProps.depsOrder`, `Toolbar`; `weight`, `factor`, `prioWeight` op `GNode` (taak 1) in taak 2 en 3.
- Open punt voor de uitvoerder: de pil-drempel `waiting >= 2` blijft zoals nu; de issue-tekst noemt geen nieuwe drempel.
