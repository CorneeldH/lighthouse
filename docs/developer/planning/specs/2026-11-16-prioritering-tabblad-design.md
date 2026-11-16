# Prioritering-tabblad in de controller — ontwerp

Issue: [#374](https://github.com/CorneeldH/lighthouse/issues/374). Mockup:
[2026-11-16-prioritering-tabblad-mockup.html](2026-11-16-prioritering-tabblad-mockup.html). Open
het bestand in een browser; de schakelaar bovenin wisselt tussen de drie weergaven. De data erin
is de echte stand van 16 november, behalve de modelsuggesties in *Zonder milestone*, die horen
bij stap 2.

## Waarom

De controller-pagina laat zien wat er nu draait, niet wat er nog moet en in welke volgorde. Wie
prioriteit kiest, moet de `blocked_by`-relaties nu zelf uitzoeken op GitHub. Die ketens zijn
diep. Op 16 november houdt #285 veertien issues in drie milestones tegen, vier lagen diep
(#285 → #360 → #361…#365 → #370).

Een milestone is een afgerond geheel. Het tabblad moet laten zien wat er vóór een milestone nog
moet gebeuren, en waar de meeste afhankelijkheid zit. Zo kun je kiezen wat eerst gaat.

## Doelen

Een tweede tabblad, **Prioritering**, naast de huidige controller-pagina. Het heeft één
schakelaar tussen drie weergaven en een repo-filter. Alle drie de weergaven zijn verplicht.

1. **Afhankelijkheden — waar zit de meeste afhankelijkheid?**
2. **Milestones — welke milestone is het snelst af?**
3. **Zonder milestone — wat hoort nog nergens bij?**

## Niet-doelen (deze stap)

- **Schrijven naar de forge.** Geen milestone toewijzen, geen milestone maken, geen slepen. Dat is
  stap 3. De server blijft alleen-lezen: geen `POST`, geen CSRF-bescherming nodig.
- **Modelsuggesties** bij *Zonder milestone* (issues groeperen, milestone voorstellen). Dat is
  stap 2, en die wacht op [#372](https://github.com/CorneeldH/lighthouse/issues/372) (roadmap,
  strategie en tenets per repo).
- **Shaping afleiden of vragen.** Of een milestone nog in shaping zit, markeert de gebruiker in
  deze stap zelf (zie Milestones).
- **Prioriteit zetten** (het board-veld Priority). Het tabblad helpt kiezen; zetten gebeurt nog op
  het board.
- **Gesloten issues in de graaf.** De graaf toont alleen open werk.

## Anti-doelen

- Een graaf die bij groei onleesbaar wordt: kruisende pijlen, overlappende nodes, titels die
  niemand kan lezen. Het moet er goed uitzien; dat was een expliciete eis.
- Een percentage dat voortgang suggereert die er niet is. Daarom de shaping-markering (Shape Up,
  hoofdstuk 13: taken tellen liegt zolang het werk bergop is).
- Het tabblad maakt de bestaande controller-pagina trager of laat hem stuk gaan. Een mislukte
  prioriteringsbouw mag de vragen, runs en cleanup niet raken.

## Aanpak — gekozen en verworpen

Wat in alle varianten gelijk is: de server bouwt de data en de SPA tekent. Het verschil zit in de
layout van de graaf.

| | Aanpak | Afweging |
|---|---|---|
| **A (gekozen)** | `@dagrejs/dagre` berekent de layout; React en SVG tekenen nodes en pijlen zelf | Klein (~30 KB). Gelaagde layout met zo min mogelijk kruisingen. De look blijft volledig van ons en past bij de mockup |
| B | Eigen layout: kolom = ketendiepte, simpele sortering | Geen dependency, maar het minimaliseren van kruisingen bouw je dan zelf zodra er meer ketens zijn |
| C | React Flow (`@xyflow/react`) | Zoomen, pannen en slepen kant-en-klaar, maar zwaar, met een eigen look. Slepen hoort in stap 3 bij de lijst, niet in de graaf |

Corneel koos A.

## Ontwerp

### Data: wat de server bouwt

Per gelezen repo (de repo's die `lighthouse serve` nu al leest, zie `forgeRepos` in
`internal/cli/serve.go`):

- **Open issues**: nummer, titel, URL, milestone (nummer en titel) en de open `blocked_by`-relaties.
  Een blocker kan in een andere repo liggen; die relatie bewaart de repo van de blocker.
- **Milestones**: alle open milestones, met titel, beschrijving, aantal open issues en aantal
  gesloten issues. Een milestone zonder issues telt ook mee.
- **Board-status** per issue, als de repo een board heeft (`BoardItems`, bestaat al).
- **Actieve runs** uit de controller-store (`dispatch.RunView`: `Repo`, `Issue`, `State`, `Phase`),
  zoals `/api/runs` ze al levert.

Op GitHub haalt één GraphQL-query per repo de open issues met milestone en `blockedBy` op, met
paginering. Dus niet één REST-call per issue. Op GitLab komt hetzelfde uit de bestaande
issue-endpoints plus de `links` van een issue (één REST-call per open issue, achter elkaar binnen
`BuildTimeout`). De bouwer legt een call- en tijdbudget vast, of kiest GitLab GraphQL met de
linked-items-widget; zonder haalbaar budget faalt de bouw voor die repo zichtbaar, niet stil.
*(r1-f7)* De forge-interface (`internal/forge/forge.go`)
krijgt hiervoor een methode die GitHub en GitLab allebei implementeren.

**De milestone-telling telt alleen issues, geen pull requests.** De milestones-API van GitHub
(`open_issues`, `closed_issues`) telt PR's met de milestone mee. Op 16 november was
`closed_issues=26` bij "Lighthouse zonder Python" in werkelijkheid 12 issues en 14 PR's. Dat
verdubbelt het percentage ongeveer, en dat is precies wat het anti-doel verbiedt. Op GitHub komt
de telling daarom uit GraphQL, per milestone en per soort een `search(type: ISSUE, query: "repo:…
milestone:\"…\" is:issue is:closed reason:completed") { issueCount }` (en `is:open` voor de open
issues). `milestone.issues(states: …) { totalCount }` kan niet op `stateReason` filteren, dus
daarmee is de not-planned-regel hieronder niet te halen. De bouwer controleert de zoekqualifiers
tegen de live API. *(own choice after spec review, r1-f2)*
Op GitLab bestaat geen telling in de milestones-API; daar komt hij uit
`/projects/:id/issues_statistics?milestone=…` (merge requests zijn daar geen issues). Alleen
tellingen: de server haalt geen gesloten issues per milestone op. *(own choice after spec review,
r1-f6)*

**Gesloten als "not planned"** (GitHub `stateReason: NOT_PLANNED`, ook duplicaten) telt niet als
klaar en valt helemaal buiten de balk. Het is geen gedaan werk en ook geen werk dat nog moet.
*Mijn keuze.* Op GitLab bestaat dat onderscheid niet; daar telt elk gesloten issue als klaar.

De milestonegetallen in de mockup komen nog uit de milestones-API en tellen PR's dus mee. Ze zijn
te hoog; de vorm klopt wel.

### Server

- Een eigen `Refresher`, naast die van de vragen en returned. Een mislukte bouw raakt zo de andere
  endpoints niet (anti-doel). `Refresher` en `BuildFunc` zijn nu vast getypt op `overview.Overview`
  (`internal/server/refresher.go`); de bouwer maakt ze generiek of voegt een tweede type toe.
  *(r1-f5)*
- **Interval 300 s, niet 90 s.** De bouw deelt het uurbudget van het GitHub-token met de overview
  (`ControllerIssues` leest `BoardItems` per bouw) en de tick; een bordpagina kost ~53 punten
  (`internal/forge/board.go`). Met 90 s komt de nieuwe bouw alleen al op ~40 × 53 punten per repo
  per uur voor het bord, naast de issuesquery. Het tabblad toont feiten die op die schaal
  veranderen; 300 s is genoeg. De bouwer rekent het uurtotaal na en legt het vast in het plan.
  *(own choice after spec review, r1-f1)*
- Of een repo een board heeft, komt mee in de bron die de bouw krijgt (uit `r.Board` in
  `forgeReposWith`, `internal/cli/serve.go`). `BoardItems` faalt op een repo zonder board en dat
  zou de hele bouw laten falen; zonder board blijft de board-status leeg. *(r1-f8)*
- Eén endpoint `GET /api/priorities`, via huma zoals de andere, met een OpenAPI-snapshot en een
  orval-gegenereerde client (`web/src/api/lighthouse.ts`). Het levert de ruwe feiten per repo:
  issues, relaties, milestones, status en actieve runs. Het rekenwerk (diepte, aantal wachtenden,
  sortering) gebeurt in de SPA. Het filter op repo werkt dan zonder nieuwe request.
- Foutafhandeling zoals `forgeError`: 503 als de forge niet geconfigureerd is, 502 als de bouw
  mislukt. Faalt één repo, dan faalt de bouw, zoals bij `BuildAll`.

### Afgeleide begrippen (één definitie, overal gebruikt)

- **Actief**: er is een run met `State` open of parked op het issue, of de board-status is
  `In progress`. *Mijn keuze: een geparkeerde run telt als actief, want het werk is begonnen.*
- **Bergop / bergaf** (alleen voor actief): op basis van `RunView.Phase`.
  - bergop: `brainstorming`, `start-issue`, `spec-review`, `writing-plans`, `plan-review`
  - bergaf: `sdd`, `executing-plans`, `write-pr`, `fix-pr`, `close-issue`
  - Actief zonder run (alleen board-status `In progress`) of met een onbekende fase telt als
    bergaf. *Mijn keuze: dan weet niemand of het bergop is, en vol geel is het neutrale "bezig".*
- **Nog niet opgepakt**: open en niet actief.
- **Wachtenden van een issue**: het aantal open issues dat er transitief achter wacht, over alle
  gelezen repo's.
- **Cycli.** Een cyclus in `blocked_by` (ook over repo's, en op GitLab zijn links vrije koppelingen)
  mag de berekening niet laten hangen. Issues in dezelfde cyclus (sterk samenhangende component)
  staan in één kolom met een zichtbare markering *cyclus*; wachtenden telt elk issue één keer; de
  werkvolgorde onder een milestone zet een cyclus bij elkaar, op nummer. *(own choice after spec
  review, r1-f3)*
- **Het repo-filter verbergt alleen.** Diepte (de kolom), wachtenden en blockers van buiten worden
  één keer berekend over alle gelezen repo's. Het filter verandert geen getal. Een issue in kolom
  *Kan nu* kan dus echt nu, ook als zijn blocker in een weggefilterde repo ligt. *Mijn keuze.*
- **Shaping**: een milestone zit in shaping als zijn beschrijving een regel bevat die precies
  `Status: shaping` is (hoofdletters negeren, spaties eromheen negeren). Een milestone zonder
  issues geldt altijd als shaping. *Mijn keuze voor de vorm: een zichtbare regel, zodat je op
  GitHub ziet wat het scherm ziet.* Later kan het scherm dit vragen of afleiden.

### SPA: tabbladen

- De huidige pagina wordt tabblad **Controller**; daarnaast komt **Prioritering**. De tabnavigatie
  staat onder de header, in de stijl van de mockup: een navy lijn met een gele onderstreping voor
  het actieve tabblad.
- Het actieve tabblad staat in de URL-hash (`#prioritering`), zonder routerdependency. Ververs je
  de pagina, dan blijf je op hetzelfde tabblad.
- Het Prioritering-tabblad is breder dan de controller-kolom (`max-w-3xl`). De graaf heeft ruimte
  nodig; de mockup gebruikt ~1180 px. Op een smal scherm scrollt de graaf horizontaal binnen zijn
  kaart, niet de hele pagina.
- Bovenin: een segmentschakelaar *Afhankelijkheden · Milestones · Zonder milestone*, elk met een
  aantal. Rechts het repo-filter als chips; standaard staan alle repo's aan. De keuze van weergave
  en filter staat in `localStorage` (in try/catch; zonder werkt alles ook).
- Onder de toolbar: een legenda met de milestonekleuren en het teken voor opgepakt.
- Licht en donker volgen het systeem, zoals de rest van de SPA (`index.css`).

### Milestonekleuren

**Kleur alleen voor wat in beeld is** (Corneel koos dit, voor nu). `serve` leest meerdere repo's,
met samen tientallen milestones (op 16 november: 10 in lighthouse, 21 in paia). Een vaste kleur
per milestone maakt de legenda onleesbaar. Daarom:

- Alleen de milestones die in de huidige weergave en met het huidige filter voorkomen, krijgen een
  kleur. In de graaf zijn dat de milestones met minstens één node. In de Milestones-weergave zijn
  het de getoonde rijen.
- Ze krijgen de kleuren van het palet op volgorde van (repo, milestonenummer). De legenda toont
  precies die milestones, met de repo als prefix zodra er meer dan één repo in beeld is.
- Bevat het beeld meer milestones dan het palet kleuren heeft, dan herhaalt het palet zich. De
  legenda blijft dan wel kloppen.
- *Geen milestone* is altijd grijs.
- Bekend nadeel, geaccepteerd: dezelfde milestone kan een andere kleur krijgen als je het filter
  of de weergave wisselt.

Het palet heeft acht kleuren en bouwt op de `--lh-*`-kleuren, maar geel, grijs en navy zijn
gereserveerd voor status (actief, nog niet opgepakt/geen milestone, opgepakt-rand en wachtenden-pil).
De acht milestonekleuren zijn andere tinten: afgeleid van blue, purple, green en orange (en
lichte/donkere varianten daarvan), zodat een klaar-segment nooit op een status-segment lijkt.
*(r1-f4)* Het moet in licht en donker leesbaar
zijn; de `dataviz`-skill heeft een validator.

### Weergave 1 — Afhankelijkheden

- **Nodes**: alle open issues met minstens één open relatie (blocker of geblokkeerd), uit de
  gefilterde repo's. Een node is een kaartje (~210 × 38 px) met een gekleurde balk links, het
  nummer vet en de titel afgekapt. Over meerdere repo's krijgt het nummer de repo als prefix
  (`repo#12`).
- **Opgepakt**: een gestippelde navy rand en een geel label "opgepakt".
- **Wachtenden**: een navy pil met het aantal wachtenden, vanaf 2.
- **Pijlen**: van blocker naar geblokkeerd issue, als vloeiende curve met een pijlpunt.
- **Layout**: dagre, van links naar rechts. De kolomkoppen zeggen *Kan nu · Wacht op 1 stap ·
  Wacht op 2 stappen …*. Kolom = langste keten ernaartoe.
- **Blocker buiten de gelezen repo's**, of weggefilterd: een grijze, gestippelde node met
  `repo#nr`. *Mijn keuze: de pijl blijft zichtbaar, zodat "kan nu" niet liegt.*
- **Aanwijzen** (hover, en tik op touch): de hele keten van dat issue licht op, stroomop- en
  stroomafwaarts, en de rest dimt. Klik opent het issue op de forge.
- **Onder de graaf**: *Geen open afhankelijkheden (N)* — de overige open issues als rij kleine
  pillen met nummer en de milestonekleur; de titel staat in de tooltip.

### Weergave 2 — Milestones

Eén rij per open milestone:

- **Links**: kleurvlak, naam, en een regel `N klaar · N actief · N nog niet`. Komen er blockers
  van buiten bij: `· N blocker van buiten`.
- **Midden**: een balk tot 100%, met één segment per issue van de milestone (open plus gesloten),
  allemaal even breed.
  - klaar = de milestonekleur
  - actief bergaf = geel (`--lh-yellow`)
  - actief bergop = geel gearceerd
  - nog niet opgepakt = lichtgrijs
  - Volgorde: klaar, dan actief, dan nog niet. Aanwijzen van een open segment toont nummer en
    titel; een klaar-segment toont alleen "klaar". *Mijn keuze: zo blijft de bouw één telling per
    milestone, zonder alle gesloten issues op te halen.*
- **Blockers van buiten**: open issues buiten de milestone die een open issue van de milestone
  transitief blokkeren. Ze staan als gestippelde blokjes met hun nummer in een vaste kolom vóór
  de balk, buiten de 100%, zodat alle balken uitlijnen.
- **Onder de balk**: `te doen:` gevolgd door de open issues van de milestone zelf, in werkvolgorde
  (topologisch). Blockers van buiten staan hier niet; die staan al vóór de balk.
- **Rechts**: het percentage klaar en `N te doen`. N telt alleen de eigen open issues.
- **Shaping**:
  - zonder issues: een gearceerde grijze balk "nog niet geshaped — percentage zegt nog niets",
    rechts `—` en "eerst shapen";
  - met issues: de balk blijft, met een gestippelde, gearceerde laag eroverheen ("nog in shaping —
    lijst kan nog groeien"). Het percentage wordt grijs met "voorlopig".
- **Sortering**: het minste te doen bovenaan. Shaping-milestones zonder issues staan onderaan.
  *Mijn keuze bij gelijkstand: hoogste percentage klaar eerst.*

### Weergave 3 — Zonder milestone

- Een lijst van de open issues zonder milestone. Een open issue in een gesloten milestone telt hier
  ook mee, met de gesloten milestone genoemd, zodat open werk niet uit beeld valt. *(own choice
  after spec review, r1-f9)* Per issue: nummer, titel, en de relatie als
  blauwe pil (`blokkeert #128`, `wacht op #44`).
- Geen sleepvakken en geen suggesties in deze stap. De ruimte rechts blijft leeg, of de lijst
  neemt de volle breedte. *Mijn keuze: de volle breedte; stap 2 en 3 maken er twee kolommen van,
  zoals de mockup.*
- Sortering: eerst issues met een relatie, dan op nummer aflopend (nieuwste eerst). *Mijn keuze.*

### Lege en foutstanden

- Laden: het skelet van de kaart met "Laden…", zoals `Panel`.
- Fout: de tekst van `forgeErrorText` in de kaart. De andere tabbladen blijven werken.
- Geen relaties, of geen issues zonder milestone: één zin in de kaart, geen lege graaf.

## Beperkingen

- Lighthouse is in deze stap alleen-lezen (`internal/server`: alleen `GET`).
- `serve` luistert standaard op `127.0.0.1:8127`.
- De SPA wordt ingebed in de binary (`internal/web/dist`). De build is niet gecommit.
- Een nieuwe npm-dependency komt met een exacte versie (`package.json` pint alles) en via
  `npm ci`.
- Taal: de UI-tekst van de SPA is nu Engels ("Open questions", "Running runs"). Dit tabblad volgt
  dat. De Nederlandse teksten in dit document en in de mockup zijn alleen illustratie.

## Aan de bouwer

- De naam en de precieze vorm van de forge-methode, en of `BoardItems` daarin opgaat of los
  blijft.
- De GraphQL-query en de GitLab-route, binnen de puntenbudgetten. Zie de opmerking bij
  `BoardQuery` over de kosten van geneste `first:`.
- Hoe de SPA de dagre-uitvoer omzet in React-componenten, en de precieze maten.
- Hoe de tests de afgeleide begrippen dekken: actief, bergop en bergaf, wachtenden, shaping,
  blockers van buiten, sortering. Volg `/lighthouse-test-standard`.

## Vervolg

- **Stap 2**: modelsuggesties bij *Zonder milestone*: groepen (ook overlappend), past bij milestone
  X, nieuwe milestone, mogelijk achterhaald. Volgens het triage-patroon: Go verzamelt, één
  read-only `claude -p` oordeelt, het model wijzigt niets. Wacht op #372.
- **Stap 3**: slepen naar een milestone en een milestone maken. Het eerste schrijfpad van de SPA,
  met CSRF-bescherming en een uitbreiding van de forge-interface (milestone zetten en maken).
- **Later**: shaping vragen of afleiden in plaats van de markering `Status: shaping`.
