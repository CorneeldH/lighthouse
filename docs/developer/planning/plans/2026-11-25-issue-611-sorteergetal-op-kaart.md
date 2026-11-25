# Sorteergetal op de kaart in Dependencies — implementatieplan

> **Voor agentic workers:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkbox-syntax (`- [ ]`).

**Doel:** elke kaart in de weergave *Dependencies* toont als pil het getal waarop de gekozen sortering (*Sort*) rangschikt, zodat te zien is waarom een kaart boven een andere staat.

**Architectuur:** `NodeCard` krijgt de prop `depsOrder` en laat de bestaande wachtenden-pil één getal tonen dat van de stand afhangt (*Prio-weight* → `prioWeight`, *Weight* → `weight`, *Fewest crossings* → `waiting` vanaf 2). Er is nooit meer dan één pil. `DepsView` geeft `depsOrder` door aan beide plekken waar het een `NodeCard` rendert. Sortering, schakelaar en opslag veranderen niet.

**Tech Stack:** React/TypeScript, vitest + Testing Library.

**Spec:** geen aparte spec; het issue [#611](https://github.com/CorneeldH/lighthouse/issues/611) met zijn acceptatiecriteria is de spec.

## Globale randvoorwaarden

- Een niet-opgegeven `depsOrder` in `NodeCard` telt als `'crossings'` (zelfde standaard als `layoutGraph` in `layout.ts:41`); de bestaande tests in `DepsView.test.tsx` (hun `view()` geeft geen `depsOrder`) blijven daardoor groen.
- Hoogstens één pil per kaart; bij *Prio-weight* en *Weight* vervangt het sorteergetal de wachtenden-pil, het komt er niet naast.
- Geen pil bij waarde 0 (bij *Fewest crossings*: bij minder dan 2 wachtenden).
- De tooltip (`title`) blijft in elke stand `N wait · weight W · factor F · prio-weight P`, ook als de pil een ander getal toont.
- Buiten bereik: `layout.ts`, `Toolbar.tsx`, `prefs.ts`, `derive.ts` en alles aan de serverkant. Geen nieuw label, geen nieuwe status, niet schrijven vanuit de web-UI (No-Gos van milestone 16).
- Code en commentaar in `web/` in het Engels; docs, changelog-fragment, commits en PR in het Nederlands.
- Voor elke commit: `[test_gate] command` uit `lighthouse.toml` en `lighthouse imports check`. Nooit `--no-verify`.
- Preflight: `cd web && npm ci` als `web/node_modules` ontbreekt.

## Review Focus

- Een externe knoop heeft `prioWeight` 0 (`derive.ts`): bij *Prio-weight* geen pil, geen crash.
- Een kaart in het blok *No open dependencies* (`loose-…`) heeft 0 wachtenden en gewicht 0, maar kan een `prio_weight` > 0 hebben: bij *Prio-weight* toont hij toch de pil.
- Wisselen van sortering met dezelfde graaf (re-render met andere `depsOrder`) wisselt het getal zonder nieuwe data.
- Bij `weight` > 0 maar `waiting` < 2 (een blokkade met één directe afhankelijke: waiting 1, weight 1) verdwijnt de pil niet bij *Weight*.
- Een vreemde of ontbrekende `depsOrder` valt terug op de wachtenden-pil, geen lege pil.

---

### Taak 1: Sorteergetal op de kaart

**Bestanden:**
- Wijzig: `web/src/components/priorities/NodeCard.tsx` (Props regel 6-23, pil regel 83-91)
- Wijzig: `web/src/components/priorities/DepsView.tsx` (twee `NodeCard`-aanroepen, rond regel 134 en 166)
- Test: `web/src/components/priorities/DepsView.test.tsx` (pil-tests rond regel 124-140; helper `view()` regel 24-31)

**Interfaces:**
- Consumeert: `DepsOrder = 'prio' | 'weight' | 'crossings'` uit `./layout`; `GNode.waiting`, `GNode.weight`, `GNode.prioWeight` uit `@/lib/priorities/derive`.
- Produceert: `NodeCard` Props krijgt `depsOrder?: DepsOrder`. De pil krijgt `data-testid="card-pill"` (was `waiting-pill`). Helper in `NodeCard.tsx`: `pillValue(node: GNode, order: DepsOrder): number` — het getal van de stand, 0 betekent geen pil.

- [ ] **Stap 1: Schrijf de falende tests in `DepsView.test.tsx`**

  `view()` krijgt een vierde parameter `depsOrder?: DepsOrder` die als prop naar `DepsView` gaat. Hernoem in de bestaande pil-tests `waiting-pill` naar `card-pill`. Voeg toe (Nederlandse beschrijvingen, naar het patroon van de bestaande `it(...)`):
  - `bij Prio-weight toont de kaart het prio-gewicht, en geen pil bij 0`: één issue met `prio_weight: 10`, één met 0 → pil `'10'` resp. geen `card-pill`.
  - `bij Weight toont de kaart het gewicht, en geen pil bij 0`: keten `iss(1), iss(2,[[A,1]])` → #1 pil `'1'`, #2 geen pil.
  - `bij Fewest crossings blijft de pil het aantal wachtenden vanaf 2` (expliciet `'crossings'`).
  - `een kaart heeft in elke stand hoogstens één pil`: voor elk van de drie standen `within(node(...)).queryAllByTestId('card-pill').length <= 1` én `queryByTestId('waiting-pill')` is null, op een kaart met 3 wachtenden en `prio_weight` > 0.
  - `de tooltip noemt in elke stand wachtenden, gewicht, factor en prio-gewicht`: titel gelijk aan `3 wait · weight 3 · factor 3 · prio-weight 10` in alle drie de standen.
  - `wisselen van sortering wisselt het getal zonder herladen`: laat `view()` naast het render-resultaat de `graph`, `colors` en `selected` teruggeven en roep `rerender(<DepsView … depsOrder="weight" />)` aan met dezelfde waarden (niet `view()` twee keer, dat mount een nieuwe boom); pilTekst van #1 wisselt van `'10'` (prio) naar `'3'` (weight).
  - `een kaart in No open dependencies toont het prio-gewicht bij Prio-weight`: testid `loose-${A}#1`, `prio_weight: 7` → pil `'7'`.

- [ ] **Stap 2: Draai de tests, controleer dat ze falen**

  Run: `cd web && npx vitest run src/components/priorities/DepsView.test.tsx`
  Verwacht: FAIL (geen `card-pill`, `depsOrder` bestaat niet op `NodeCard`).

- [ ] **Stap 3: Implementeer in `NodeCard.tsx`**

  Voeg `depsOrder?: DepsOrder` toe aan `Props` (import het type uit `./layout`). Voeg `pillValue(node, order)` toe: `'prio'` → `node.prioWeight`, `'weight'` → `node.weight`, anders (ook `undefined`) → `node.waiting >= 2 ? node.waiting : 0`. Vervang het huidige `node.waiting >= 2`-blok door één pil die rendert als `pillValue(node, depsOrder ?? 'crossings') > 0`, met testid `card-pill`, de ongewijzigde `title`-string en dezelfde klassen; de inhoud is het getal.

- [ ] **Stap 4: Geef `depsOrder` door in `DepsView.tsx`**

  Voeg `depsOrder={depsOrder}` toe aan beide `NodeCard`-aanroepen (graaf en *No open dependencies*).

- [ ] **Stap 5: Draai tests, typecheck en lint**

  Run: `cd web && npx vitest run src/components/priorities && npm run typecheck && npm run lint`
  Verwacht: alles PASS.

- [ ] **Stap 6: Commit**

  ```bash
  git add web/src/components/priorities/NodeCard.tsx web/src/components/priorities/DepsView.tsx web/src/components/priorities/DepsView.test.tsx
  git commit -m "feat(web): kaart in Dependencies toont het getal van de gekozen sortering (#611)"
  ```

### Taak 2: Documentatie en changelog

**Bestanden:**
- Wijzig: `docs/priorities.md` (alinea *Sortering van de graaf*, laatste zin over de pil, rond regel 151-152)
- Maak: `changelog.d/issue-611-sorteergetal-op-kaart.md`

**Interfaces:**
- Consumeert: het gedrag uit taak 1.

- [ ] **Stap 1: Pas `docs/priorities.md` aan**

  Vervang de zin over de pil met het aantal wachtenden door een beschrijving: elke kaart toont één pil met het getal van de gekozen sortering: bij *Prio-weight* het prio-gewicht, bij *Weight* het gewicht (beide alleen als > 0), bij *Fewest crossings* het aantal wachtenden vanaf 2. Het sorteergetal vervangt de wachtenden-pil, er staat nooit meer dan één pil op een kaart. De tooltip toont in elke stand `N wait · weight W · factor F · prio-weight P`. Wisselen van *Sort* wisselt het getal zonder herladen.

- [ ] **Stap 2: Maak het changelog-fragment** (formaat van `changelog.d/issue-380-versiewissel.md`)

  ```markdown
  ---
  type: added
  ---
  Elke kaart in de Dependencies-weergave toont het getal van de gekozen sortering (prio-weight, weight, of het aantal wachtenden vanaf 2), zodat zichtbaar is waarom een kaart boven een andere staat (#611).
  ```

- [ ] **Stap 3: Controleer links en commit**

  Run: `.claude/hooks/lighthouse/run.sh md-link-check` is een hook en draait vanzelf bij de Edit; controleer geen foutmelding.

  ```bash
  git add docs/priorities.md changelog.d/issue-611-sorteergetal-op-kaart.md
  git commit -m "docs(priorities): sorteergetal op de kaart beschreven (#611)"
  ```
