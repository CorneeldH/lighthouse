# Afhankelijkhedengraaf: vormentaal en volwaardige kaarten — implementatieplan

> **Voor agentic workers:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkbox-syntax (`- [ ]`).

**Doel:** De kaarten in de afhankelijkhedengraaf tonen de toestand met dezelfde vormentaal als de milestone-balk, en issues zonder afhankelijkheden krijgen dezelfde kaart in een raster onder de graaf.

**Architectuur:** Eén nieuwe component `NodeCard` tekent een kaart (210×38) uit een `GNode`; de graaf plaatst hem absoluut, het raster in de reguliere flow. `issueState()` in `derive.ts` blijft de enige afleiding (al aanwezig als `GNode.state` en `GNode.needsHuman`); alleen de weergave verandert. De CSS-klassen `.lh-stripe` en `.lh-cross` en de variabele `--lh-seg-todo` bestaan al.

**Tech Stack:** React, TypeScript, Tailwind, vitest + Testing Library (`web/`).

**Spec:** [`2026-11-20-statusmodel-en-statusschema-design.md`](../specs/2026-11-20-statusmodel-en-statusschema-design.md), secties *Vormentaal* en *Afhankelijkhedengraaf*. Issue #438, milestone #16.

## Global Constraints

- Stippelrand = mens nodig: 1,5 px, gestippeld, `var(--foreground)`; niet in milestone-kleur.
- Externe kaart (andere repo of buiten het filter): doorgetrokken grijze rand (`border-lh-grey`), nooit gestippeld.
- Kaartmaat blijft `NODE_W`×`NODE_H` (210×38) uit `web/src/components/priorities/layout.ts`.
- De gele pil `picked up`, `bg-lh-yellow` en de navy stippelrand (`border-lh-navy` als toestandsmarkering) komen niet meer voor.
- Geen nieuw label, geen nieuwe Status, niet schrijven vanuit de web-UI (No-Gos van milestone #16).
- `issueState()` en `internal/issuestate` blijven ongewijzigd.
- Artefacten (docs, changelog, commitberichten) in het Nederlands; code, commentaar en testnamen in het Engels.
- Voor elke commit naast `[test_gate] command` ook `lighthouse imports check`.

## Review Focus

- Een `Controller`-issue zonder run is actief (gestreepte kleurstrook): test in Taak 1.
- Een los issue op `In progress` is actief en staat in het raster, niet als chip: Taak 2.
- Actief én mens nodig: gestreepte strook én stippelrand tegelijk: Taak 1.
- Een geblokkeerd issue (open `blocked_by`) dat los hangt kan niet bestaan; een geblokkeerd issue in de graaf toont het kruis bij het nummer: Taak 1.
- Hover- en ketting-markering (`data-dimmed`, `chain`) en de `waiting`-pil blijven werken op de nieuwe kaart: Taak 1.

---

### Taak 1: `NodeCard` met de vormentaal, in de graaf

**Files:**
- Create: `web/src/components/priorities/NodeCard.tsx`
- Modify: `web/src/components/priorities/DepsView.tsx` (de `<a>` in `model.nodes.map`, r. ~143-210; de `bg-lh-yellow`-pil weg)
- Test: `web/src/components/priorities/DepsView.test.tsx`

**Interfaces:**
- Produces: `NodeCard({ node, color, grey, label, testId, dimmed, highlighted, style, onPointerDown, onPointerEnter, onMouseEnter, onMouseLeave, onClick }): JSX.Element` — `node: GNode`, `color: string` (milestone-kleur of `NO_MILESTONE_COLOR`), `grey: boolean` (extern), `label: string` (`#nr` of `repo#nr`), `dimmed`/`highlighted: boolean`, `style?: CSSProperties`, de overige props zijn de bestaande handlers van de `<a>`. Rendert een `<a>` met `data-testid={testId}`, `data-state={node.state}`, `data-needs-human`, `data-external`, `data-dimmed`, `data-depth`, `data-cycle`.
- Consumes: `GNode` uit `@/lib/priorities/derive`; `NODE_W`, `NODE_H` uit `./layout`.

- [ ] **Stap 1: Schrijf de falende tests** in `DepsView.test.tsx`, ter vervanging van de test `marks an active issue picked up…`:
  - `marks a Controller issue without a run as active`: issue 1 met `status: 'Controller'` en een afhankelijkheid van 2 → `node(`${A}#1`)` heeft `data-state="active"`, de kleurstrook (`getByTestId('card-strip')` binnen de kaart) heeft klasse `lh-stripe`; geen tekst `picked up`.
  - `marks an In progress issue as active`: idem met `status: 'In progress'`.
  - `draws a ready card with a solid strip, and no dashed outline`: `data-state="ready"`, strook zonder `lh-stripe`, `data-needs-human="false"`.
  - `draws needs human as a dashed outline, also on an active card`: issue met `label: 'needs_decision'` → `data-needs-human="true"`; en een actief issue met `open_question: true` → `data-state="active"` én `data-needs-human="true"`; de kaart heeft `style.outline` met `dashed`.
  - `puts a cross at the number of a blocked card`: issue 2 met afhankelijkheid van 1 → `within(node(`${A}#2`)).getByTestId('card-cross')` bestaat; kaart 1 heeft geen `card-cross`.
  - `gives an external card a solid grey border`: bestaande test op `:69` (grijs gestippeld extern) wordt: klasse `border-lh-grey`, niet `border-dashed`.
  - Pas `:85` en andere asserties aan die op `border-dashed`/`picked up` staan; behoud de `waiting-pill`-test en de `data-dimmed`-tests.

- [ ] **Stap 2: Draai de tests, zie ze falen**

Run: `cd web && npx vitest run src/components/priorities/DepsView.test.tsx`
Verwacht: FAIL op `card-strip`, `card-cross`, `data-state` (nog niet aanwezig).

- [ ] **Stap 3: Implementeer `NodeCard`** in `NodeCard.tsx`. Aanpak: de linkerstrook (`w-1.5`, `testid card-strip`) is `lh-stripe` met `--ms-color` bij `active`, anders effen `color`; `needsHuman` zet `outline: '1.5px dashed var(--foreground)'` met `outlineOffset: -1`; `blocked` zet vóór het nummer een klein kruis (`testid card-cross`, klasse `lh-cross`, 12×12); een externe kaart krijgt `border border-lh-grey` (doorgetrokken), anders `border border-border`. Kopieer de bestaande kaartinhoud (nummer, titel, `cycle`, `waiting`-pil) ongewijzigd uit `DepsView.tsx`; verwijder de gele pil en de navy stippelrand.

- [ ] **Stap 4: Gebruik `NodeCard` in `DepsView.tsx`** voor `model.nodes`; behoud `href`-logica (`grey && n.url === ''`), `dimmed`, `chain` en de handlers. Verwijder niet-gebruikte imports.

- [ ] **Stap 5: Draai de tests, zie ze slagen**

Run: `cd web && npx vitest run src/components/priorities src/lib/priorities`
Verwacht: PASS.

- [ ] **Stap 6: Commit**

```bash
git add web/src/components/priorities/NodeCard.tsx web/src/components/priorities/DepsView.tsx web/src/components/priorities/DepsView.test.tsx
git commit -m "feat(web): graafkaart tekent de toestand met de vormentaal van de balk (#438)"
```

### Taak 2: Raster met volwaardige kaarten voor issues zonder afhankelijkheden

**Files:**
- Modify: `web/src/components/priorities/DepsView.tsx` (blok `No open dependencies`, r. ~228-250)
- Test: `web/src/components/priorities/DepsView.test.tsx`

**Interfaces:**
- Consumes: `NodeCard` uit Taak 1.

- [ ] **Stap 1: Schrijf de falende tests**:
  - `shows a loose issue as a full card with number, title and state`: `view(CHAIN, [A])` → `screen.getByTestId(`loose-${A}#5`)` bevat `#5` en `title-5`, heeft `data-state="ready"` en maat `width: 210px; height: 38px`.
  - `marks a loose In progress issue as active`: los issue met `status: 'In progress'` → `data-state="active"`, strook met `lh-stripe`.
  - `marks a loose Controller issue without a run as active`: idem met `status: 'Controller'`.
  - Pas de bestaande tests op `:117` en `:163` aan op de nieuwe kaart (testid `loose-<key>` blijft behouden, zodat verwijzingen blijven kloppen).

- [ ] **Stap 2: Draai de tests, zie ze falen**

Run: `cd web && npx vitest run src/components/priorities/DepsView.test.tsx`
Verwacht: FAIL (chip heeft alleen het nummer, geen `data-state`).

- [ ] **Stap 3: Vervang de chips** door `NodeCard` in een raster: `grid` met `gridTemplateColumns: 'repeat(auto-fill, 210px)'` en `gap-2.5`, `testId={`loose-${n.key}`}`, `grey={false}` (`loose` bevat alleen issues in het filter), `dimmed={false}`, zonder `style`-positie. Behoud de kop `No open dependencies ({loose.length})`. De kaart en zijn handlers zijn dezelfde component als in de graaf, zodat de twee renderpaden niet uiteenlopen.

- [ ] **Stap 4: Draai de tests, zie ze slagen**

Run: `cd web && npx vitest run src/components/priorities src/lib/priorities`
Verwacht: PASS.

- [ ] **Stap 5: Commit**

```bash
git add web/src/components/priorities/DepsView.tsx web/src/components/priorities/DepsView.test.tsx
git commit -m "feat(web): issues zonder afhankelijkheden krijgen een volwaardige kaart in een raster (#438)"
```

### Taak 3: Legenda, documentatie en changelog

**Files:**
- Modify: `web/src/components/priorities/Toolbar.tsx:109-129` (alleen als de legenda de kaartvorm niet dekt), `web/src/components/priorities/PrioritiesTab.test.tsx:181-193`
- Modify: `docs/priorities.md` (*Afgeleide begrippen*, r. ~126-135, en de beperking bij r. ~163)
- Create: `changelog.d/issue-438-afhankelijkhedengraaf-vormentaal.md`

- [ ] **Stap 1: Controleer de legenda.** De legenda toont al de vijf toestanden met `Block` (#437). Controleer dat zij kaart en balk beide dekken (tekst *kleurstrook* of vergelijkbaar); breid de uitleg aan alleen als dat ontbreekt. Test in `PrioritiesTab.test.tsx`: de legenda bevat geen `picked up` en geen `bg-lh-yellow` (bestaat al op `:193`); voeg toe dat de legenda de toestanden *actief*, *oppakbaar*, *mens nodig* en *geblokkeerd* noemt als dat nog niet zo is.

- [ ] **Stap 2: Werk `docs/priorities.md` bij** (Nederlands): de graaf toont de toestand met kleurstrook (gestreept actief, effen oppakbaar), stippelrand (mens nodig), kruis bij het nummer (geblokkeerd) en een doorgetrokken grijze rand voor een externe kaart; issues zonder afhankelijkheden staan als kaart in een raster onder de graaf. Relatieve links naar de spec.

- [ ] **Stap 3: Schrijf het changelog-fragment** `changelog.d/issue-438-afhankelijkhedengraaf-vormentaal.md`:

```markdown
---
type: changed
---
De afhankelijkhedengraaf in de Prioritering-tab tekent de toestand met dezelfde vormentaal als de milestone-balk, en issues zonder afhankelijkheden krijgen een volwaardige kaart in een raster onder de graaf.
```

  Controleer het `type`-veld tegen een bestaand fragment in `changelog.d/` en de toegestane waarden.

- [ ] **Stap 4: Volledige verificatie**

Run: `cd web && npm run lint && npm run typecheck && npm test` (controleer de scriptnamen in `web/package.json`; de gate in `lighthouse.toml` `[test_gate]` is leidend), daarna `lighthouse imports check`.
Verwacht: alles groen. Zoek met `git grep -n "picked up\|bg-lh-yellow" web/src` en zie dat alleen negatieve asserties overblijven.

- [ ] **Stap 5: Commit**

```bash
git add web/src docs/priorities.md changelog.d/issue-438-afhankelijkhedengraaf-vormentaal.md
git commit -m "docs(web): legenda en priorities.md volgen de vormentaal van de graaf (#438)"
```

## Opmerkingen voor de uitvoerder

- De UI is nooit in een browser gecontroleerd (zie `docs/priorities.md`); jsdom bewijst klassen en attributen, niet de weergave. Vermeld dat in de PR.
- Staat de reden dat #381, #383, #406, #407 en #1779 niet als actief verschenen niet in de weergave maar in de data (`status` komt niet als `Controller` binnen), meld dat als bevinding in de PR, buiten dit plan.
