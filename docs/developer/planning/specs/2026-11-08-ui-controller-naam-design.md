# Naam van de webpagina: lighthouse, met een sectie Controller

## Doel en waarom

De kop van de webpagina leest als "lighthouse dev". "dev" is de versie van een build die geen
release is (`useVersion`, [Header.tsx](../../../../web/src/components/Header.tsx)), maar staat er
als deel van de naam. De pagina toont bovendien alleen het controller-deel van lighthouse (de
automatische runs), terwijl "lighthouse" ook de packs van `lighthouse init` omvat.

Besloten (eigenaar, 2026-11-08): de pagina wordt later breder dan de runs, dus de app blijft
**lighthouse**. Het deel dat ze nu toont heet **Controller**, het woord dat de board-status,
`[controller.triage]`, `LIGHTHOUSE_CONTROLLER_RUN` en de docs al gebruiken. Na M2 verdwijnt de
Python-controller en houdt het woord één betekenis.

## Doelen

- De kop toont "lighthouse" met de versie als aparte badge, niet als doorlopende tekst.
- Een zichtbare sectiekop "Controller" boven alles wat bij de automatische runs hoort.
- De browsertitel is `lighthouse · Controller`.
- De README beschrijft de pagina als de Controller-sectie van lighthouse.

## Non-goals

- Tabs of routing. Die komen pas met een tweede sectie.
- Een andere naam voor de board-status, `[controller.triage]`, env-vars of docs.
- De betekenis van "controller" in personal_ai_agent opruimen; dat gaat met M2.

## Anti-goals

- Een kop die opnieuw moet veranderen zodra de pagina breder wordt ("lighthouse controller").
- Een lege tab of placeholder voor wat nog niet bestaat.

## Aanpakken

- **A. Alleen een sectiekop, geen tabs.** Gekozen: kleinste wijziging, en de structuur voor
  later wordt pas gekozen als het tweede deel bekend is.
- **B. Nu al tabs met één tab "Controller".** Legt routing vast voor één tab, voor een tweede
  deel waarvan de vorm nog onbekend is.
- **C. "lighthouse controller" in de kop.** Botst met de keuze dat de app lighthouse blijft.

## Ontwerp

```
lighthouse [dev]                                  updated 1m ago   [Refresh]
┌ Usage ─────────────────────────────────────────────────────────────────┐
Controller                                                    tick 3m ago
[Questions][Returned][Running][Parked][Queue][Night][To clean up]
Questions … Returned … Running … Parked … Queue … Night … To clean up
```

- `UsagePanel` blijft boven de sectie: usage en de usage-gate gelden voor heel lighthouse.
- Onder de kop "Controller": `Summary`, `QuestionList`, `ReturnedList`, de runblokken en
  `CleanupList`, in de huidige volgorde.
- De sectie is een landmark (`<section aria-labelledby>` met een `h2`).

## Calls van de bouwer (Claude), te controleren

- De versie als badge in de kop.
- "tick … ago" verhuist van de kop naar de Controller-kop: de tick is van de dispatcher, niet van
  heel lighthouse. "updated … ago" en Refresh blijven in de kop; die gelden voor de hele pagina.
- De browsertitel staat statisch in `web/index.html`; geen `document.title` in code.

## Aan de bouwer

- De vorm van de badge en de stijl van de sectiekop, binnen de bestaande `lh-*`-kleuren.
- De screenshot in de README (`docs/images/ui-m2.png`) vernieuwen of laten staan.
