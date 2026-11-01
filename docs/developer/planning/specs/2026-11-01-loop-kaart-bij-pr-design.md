# De loop-kaart verandert in de PR die het werk levert

## Aanleiding

#119 (roborev vóór de PR, fix-pr vanuit de dispatcher) is gemerged en gesloten, maar
`docs/site/loop.html` toont de chip `roborev` en de handoff `pr-and-ci → fix-pr` nog als gepland.
De detector bestond: `ci/chain_roadmap.py` geeft sinds het sluiten van #119

```
roborev: #119 is not an open issue labelled chain
pr-and-ci->fix-pr: #119 is not an open issue labelled chain
```

maar de workflow `chain-roadmap` draait op een pull request alleen als die `chain.json` raakt, en
PR #197 raakte die niet. Daarna ging hij rood op het `issues`-event op `main`, waar rood niets
blokkeert en niemand kijkt. Er is een detector, geen slot.

## Doel

Een issue dat een stap, handoff of route van de loop verandert, staat op de kaart op het moment
dat het werk landt: in dezelfde PR, niet erna.

- De kaart heet overal *loop*: label `loop`, data `docs/site/data/loop.json`.
- `write-issue` zet het label `loop` na een kruiscontrole tegen de kaart.
- `write-pr` werkt de kaart bij voor elk `Closes #N` met dat label.
- Een CI-check op elke PR maakt de PR rood als een `Closes` met label `loop` de kaart niet bijwerkt.
- De achterstand van #119 wordt hersteld.

## Niet-doelen

- **Geen geplande items bij het aanmaken van een issue.** Een nieuw `loop`-issue krijgt alleen het
  label; het kaartitem komt mee met de PR die het werk levert. De richting "elk open `loop`-issue
  staat op de kaart" vervalt. De roadmaplaag komt terug in een vervolgissue (zie onder).
- **Geen labels voor de andere kaarten.** `packs.json`, `session.json`, `config.json`, `repo.json`
  en de referentiepagina zijn al aan code gekoppeld door `tests/test_site_*.py`; die worden rood
  in de PR zelf. `session.json` heeft geen enkel `planned` item. Een `session`-label komt pas als
  die kaart een roadmaplaag krijgt.
- **Geen config-key en geen generieke gate in `pack/core`** (aanpak B hieronder).
- **Geen bot-commit na de merge**: dat botst met "niets direct op `main`".

## Anti-doelen

- Een skill in `pack/core` die `loop.json` of het label `loop` bij naam kent. De skills draaien in
  elke repo met lighthouse; de loop-kaart bestaat alleen hier.
- Een check die weer alleen op `main` rood wordt.

## Aanpakken

- **A — het taxonomiebestand legt de koppeling (gekozen).** `docs/developer/labels.md` noemt per
  vrij label het document dat een sluitende PR bijwerkt. `write-issue` leest dat bestand al via
  `[paths] labels`; `write-pr` gaat het ook lezen. Het slot is een CI-check in alleen deze
  repo. Generiek zonder nieuwe config; in andere repo's blijft het een instructie zonder slot.
- **B — config-key plus generieke gate.** `[docs] label_maps` in `lighthouse.toml`, een script in
  `pack/core` dat `write-pr` en `pr-create-gate` aanroepen. Elke repo een hard slot, maar een
  nieuwe key in `config.py`, een rij in `config.json` met test, een script en een
  hookuitbreiding. Kan later, als een andere repo zo'n kaart krijgt.
- **C — alleen deze repo.** CI-check en een regel in `AGENTS.md`, geen packwijziging. Goedkoopst,
  maar de skills weten van niets en je merkt het pas aan rode CI.

Corneel koos A.

## Ontwerp

### 1. Hernoemen: chain → loop

| Was | Wordt |
|---|---|
| GitHub-label `chain` | `loop` (`gh label edit chain --name loop`; issues houden het label) |
| `docs/site/data/chain.json` | `docs/site/data/loop.json` |
| `ci/chain_roadmap.py` | `ci/loop_roadmap.py` |
| `.github/workflows/chain-roadmap.yml` | `.github/workflows/loop-roadmap.yml` |
| `tests/test_chain_roadmap.py` | `tests/test_loop_roadmap.py` |
| `tests/test_site_chain.py` | `tests/test_site_loop.py` |

Mee: elke verwijzing in `docs/site/loop.js`, `tests/_site_node.py`, de andere
`tests/test_site_*.py`, `tests/test_agent_guide.py`, `tests/fixtures/site/map.json` en
`docs/developer/labels.md`. `docs/site/chain.html` blijft de redirect naar `loop.html`.
Historische stukken (`CHANGELOG.md`, bestaande specs en plannen) blijven zoals ze zijn.

Het hernoemen van het GitHub-label is een wijziging buiten de repo. Die gebeurt na de merge, of
vlak ervoor: de nieuwe check zoekt `loop`, de oude `chain`. De volgorde staat in het plan.

### 2. `labels.md`: het document per vrij label

De sectie over het vrije label wordt een tabel die `write-issue` en `write-pr` lezen:

| Label | Kleur | Wanneer | Document |
|---|---|---|---|
| `loop` | `F4D74B` | Voegt een stap, handoff of route van de loop toe of verandert die | `docs/site/data/loop.json` |

"Wanneer" is de toets voor `write-issue`. "Document" is wat `write-pr` bijwerkt. Een vrij label
zonder document (de GitHub-standaardlabels) laat `write-pr` ongemoeid. De uitleg eronder zegt wat
"bijwerken" voor `loop.json` betekent: een `planned` item met het issuenummer wordt `works` en
verliest `issue`; een nieuwe stap of handoff komt erbij als `works`, met `source`.

### 3. `write-issue`: kruiscontrole bij het labelen

In stap 3 komt na 3c een stap 3d, *Free labels with a document*. Leest het taxonomiebestand een
vrij label met een document, dan opent de skill dat document en toetst het issue aan de
"Wanneer"-kolom: raakt het een stap, handoff of route die erin staat, of voegt het er een toe?
Zo ja, het label. Geen taxonomiebestand of geen document in de tabel: de stap valt weg. De skill
noemt geen label en geen pad bij naam.

### 4. `write-pr`: de kaart in dezelfde PR

Een nieuwe sectie na *V3. Choose the form*, vóór *Changelog fragment*: *Documents of the linked
issues*. De kaart volgt de code, niet het sluiten van het issue: #119 ging met `Refs #119` (PR
#197) en werd met de hand gesloten. Voor elk `Closes #N` én elk `Refs #N`:

1. de labels van #N (`gh issue view N --json labels`);
2. voor elk label met een document in het taxonomiebestand: levert de branch de stap, handoff of
   route waar het issue over gaat? Zo ja, werk het document bij zoals de uitleg in dat bestand
   zegt, commit en push vóór `gh pr create`. Bij `Closes` is het antwoord ja; bij `Refs` oordeelt
   de sessie op de diff en de AC. Nee: niets doen, geen melding in de body;
3. een re-run schrijft niets twee keer: staat er geen `planned` item meer met #N en raakt de
   branch het document al, dan is de stap klaar.

Geen taxonomiebestand, of geen label met een document: de sectie valt weg, zonder melding in de
body.

Na de wijziging in `pack/core`: `go run ./cmd/lighthouse init --upgrade` en de geïnstalleerde
kopie mee committen (dogfood).

### 5. De check: rood in de PR

`ci/loop_roadmap.py` krijgt twee modi.

- **PR-modus** (`pull_request` met `opened`, `synchronize`, `reopened` en `edited`, zonder
  padfilter; `edited` omdat `Closes` in de body staat). De issues die de PR sluit komen uit
  GraphQL (`closingIssuesReferences` van de PR). Voor elk gesloten issue met label `loop`:
  - geen `planned` item in `loop.json` noemt het nog;
  - de PR raakt `docs/site/data/loop.json`.
  Een PR die geen `loop`-issue sluit, is groen zonder verdere check. `Refs` controleert de check
  niet: daar oordeelt `write-pr`, en het vangnet op `main` vangt een vergeten item zodra het issue
  dichtgaat.
- **De foutmelding zegt wat te doen**: het item (`roborev`, `pr-and-ci->fix-pr`), het issue, en
  dat `docs/developer/labels.md` beschrijft hoe `loop.json` wordt bijgewerkt. Zo kan
  `/lighthouse-fix-pr`, die de dispatcher op een rode PR start, het herstellen zonder de
  taxonomie te kennen. *(eigen keuze)*
- **Geen verplichte status-check.** `main` heeft geen branch protection; de check is een rood
  signaal in de PR, zoals de rest van CI hier. *(eigen keuze)*
- **Hoofdmodus** (`push` naar `main`, `issues`, `workflow_dispatch`): elk `planned` item noemt
  een open issue met label `loop`. Dat is het vangnet voor een issue dat met de hand dichtging.
  De omgekeerde richting (elk open `loop`-issue op de kaart) vervalt.

Exit-codes blijven: 0 ok, 1 een oordeel, 2 `COULD NOT CHECK`. De workflow krijgt
`pull-requests: read`.

`tests/test_site_loop.py` staat nu al `planned` met alleen `milestone` toe; `loop_roadmap.py`
meldt zo'n item als "planned without an issue". Dat verschil blijft in deze wijziging; het
vervolgissue beslist erover.

### 6. Herstel van #119

In `loop.json`:

- chip `roborev`: `status` wordt `works`, `issue` vervalt, `source` wordt
  `pack/core/scripts/roborev_gate.py`;
- handoff `pr-and-ci → fix-pr`: `status` wordt `works`, `issue` vervalt.

`test_works_handoffs_follow_the_handoff_table` gaat ervan uit dat elke `works`-handoff in de
routeringstabel van `write-handoff` staat. Deze handoff loopt via de dispatcher (#119 AC 5:
"De dispatcher start bij een rode PR … `/lighthouse-fix-pr`"), niet via een handoffblok. De test
krijgt dat onderscheid; hoe, laat het ontwerp aan de bouwer.

### 7. Changelog

Een fragment in `changelog.d/`, type `changed` voor `write-pr` en `write-issue`.

## Vervolgissue

Na goedkeuring van dit ontwerp, met `/lighthouse-write-issue`: *Een `loop`-issue op een
milestone staat als `planned` op de loop-kaart.* Het brengt de roadmaplaag terug, maar alleen voor
werk dat op een milestone staat, en beslist over `planned` met alleen `milestone`.

## Beperkingen

- Artefacten in het Nederlands; skills, hookteksten en code in het Engels.
- `write-pr` en `write-issue` blijven generiek: geen labelnaam, geen pad.
- Dogfood: wijziging onder `pack/` → `init --upgrade`, CI eist `No changes.`
- Werk in de worktree `.claude/worktrees/loop-kaart-bij-pr` op branch `fix-loop-kaart-bij-pr`.

## Aan de bouwer

- De precieze opmaak van de tabel in `labels.md`, zolang beide skills hem zonder gissen lezen.
- De GraphQL-query en de fixtures voor de PR-modus.
- Hoe `test_works_handoffs_follow_the_handoff_table` een dispatcher-handoff van een
  handoffblok onderscheidt.
