# Prioritering: wat het tabblad leest

Het tabblad Prioritering leest per repo één `forge.Planning` (`internal/forge/planning.go`).
Dit document legt vast wat die lezing opvraagt, wat ze kost en wat live is gecontroleerd.

## Wat `Planning` leest (GitHub)

Drie soorten GraphQL-query's per bouw, alle achter elkaar:

1. `Planning`: de **open** issues in pagina's van 100 (`number title url`, `milestone { number
   title state description }`, `blockedBy(first: 20)`). Alleen **open** blockers komen in
   `PlanIssue.BlockedBy`: een gesloten blocker blokkeert niets. `blockedBy` vraagt `pageInfo`;
   een issue met meer dan 20 blockers geeft een fout (`blockedBy truncated at 20`), geen stille
   afkapping. Een open issue in een gesloten milestone houdt `Milestone.State == "closed"`.
2. `PlanMilestones`: alle **open** milestones (`milestones(states: OPEN)`), pagina's van 100,
   ook milestones zonder issues.
3. `PlanCounts`: per open milestone twee `search(type: ISSUE)`-aliassen in één query (maximaal
   50 milestones per query):
   - `repo:<r> milestone:"<titel>" is:issue is:open` wordt `MilestoneCount.Open`;
   - `repo:<r> milestone:"<titel>" is:issue is:closed reason:completed` wordt `MilestoneCount.Done`.

Een `hasNextPage` met een lege of ongewijzigde `endCursor` is een fout (`pagination stuck`),
zoals bij `BoardItems`: de bouw faalt zichtbaar in plaats van te blijven lussen.

`Done` telt alleen issues gesloten als `completed`. Pull requests (zonder `is:issue` telt de
zoekopdracht ze mee) en issues gesloten als `not planned` tellen in geen van beide.

## Controle van de zoekqualifiers tegen de live API

Gemeten op 2026-11-17 met `gh api graphql` tegen `CorneeldH/lighthouse`, milestone
`M3 — Tweede omgeving` (spaties en een em-dash in de titel):

| Zoekopdracht | `issueCount` |
|---|---|
| `is:issue is:open` | 2 |
| `is:issue is:closed` | 4 |
| `is:issue is:closed reason:completed` | 4 |
| `is:issue is:closed reason:"not planned"` | 0 |
| `is:pr` | 4 |

- `milestone:"<titel>"` met spaties en em-dash werkt; `is:issue` sluit de 4 pull requests uit
  (de REST-teller van de milestone geeft 8 gesloten: 4 issues plus 4 PR's).
- `reason:completed` werkt als qualifier op `is:closed`.
- Titel met `"` of `\`: **niet geverifieerd.** GitHub documenteert geen escape in een gequote
  qualifier en er bestaat in deze repo geen milestone met zo'n titel om tegen te toetsen. De
  bouw weigert zo'n titel daarom met een fout die de milestone noemt (`a quote or backslash in
  the title cannot be searched`), zodat een verkeerde telling nooit stil blijft. Is de escape
  later live bewezen, dan vervalt die weigering in `planCounts`.

## Kosten van één bouw

Berekend (uit het plan): 12 bouwen per uur. Per repo kost een pagina van 100 issues met
`blockedBy(first: 20)` ongeveer 21 punten, en elke `PlanCounts`-query 1 punt. Voor een repo met
21 milestones: ongeveer 12 x (21 + 42) = 760 punten per uur, naast ongeveer 2100 per uur voor
het bord van de overview; samen binnen de 5000 per uur.

Gemeten met `rateLimit { cost }` op 2026-11-17, `CorneeldH/lighthouse` (57 open issues, 11 open
milestones, 22 search-aliassen): elke van de drie query's kostte **1 punt**, dus **3 punten per
bouw**, ruim onder de berekende ongeveer 21 punten voor de issuespagina. De berekende getallen
zijn dus een bovengrens; de meting op een repo met meer dan 100 issues is niet gedaan.

## Twee kostenwijzigingen

- `BuildAll` leest het bord nu één keer per overview-bouw voor repo's die wel een bord hebben maar
  geen statusoptie voor de Controller: ongeveer 53 punten, bij ongeveer 40 bouwen per uur per
  repo. Voorheen werd dat bord daar nooit gelezen.
- GitLab-budget per bouw: 1 + ceil(open/100) issuepagina's, één links-aanroep per open issue en
  één `issues_statistics`-aanroep per open milestone.

## Wat het tabblad toont

`lighthouse serve` toont naast de overview een tabblad Prioritering (`#prioritering`) met drie
weergaven: een afhankelijkhedengraaf van open issues, een milestone-weergave met voortgang
(`Done` van `Done + Open`, waarbij `Open` hier het aantal open issues is dat `Planning.Issues` per milestone levert; de SPA negeert de `Open` van de zoekopdracht) en een weergave van issues zonder milestone. De server bouwt de
gegevens in een eigen refresher, los van de overview, elke **300 s** (`PrioritiesInterval`) en
levert ze op `/api/priorities`. De pagina leest, op één plek na: de rang van een milestone
(zie het volgende kopje).

## De rang van een milestone schrijven

De weergave Milestones heeft een schakelaar *Berekend* / *Prioriteit*. In *Berekend* geldt de
berekende volgorde. In *Prioriteit* staan milestones met een rang bovenaan, op rang, en de
milestones zonder rang daaronder in de berekende volgorde. De knoppen Omhoog en Omlaag wijzigen
de rang.

- **De regel.** De rang staat in de beschrijving van de milestone, als een regel
  `Priority: N` (N een geheel getal vanaf 1, hoofdletters en omringende spaties doen er niet
  toe). Telt de beschrijving meer geldige regels, dan geldt de eerste. De server schrijft die
  regel; staat hij er niet, dan komt hij onderaan, na een lege regel. De Go-code
  (`internal/priorities/rank.go`) en de SPA (`web/src/lib/priorities/rank.ts`) lezen en
  schrijven hem op dezelfde manier.
- **Het endpoint.** `POST /api/priorities/milestone-ranks` met `{repo, changes: [{number, from,
  to}]}`; de SPA berekent welke milestones een nieuwe rang krijgen, de server rekent geen
  volgorde uit. Hij leest elke milestone vers van de forge en schrijft pas als elke `from`
  nog klopt, en werkt na elke schrijfactie de cache bij. Antwoorden: 200 met `{written}`; 403
  bij een ontbrekende of verkeerde `Origin`; 404 voor een onbekende repo; 409 als een rang op
  de forge anders is dan `from` (niets geschreven; de cache wordt bijgewerkt); 415 bij een
  andere `Content-Type` dan `application/json`; 422 bij een ongeldige body; 502 of 503 bij een
  forgefout of een niet-geconfigureerde forge. Mislukt een schrijfactie halverwege, dan noemt
  de melding de al geschreven milestones (`written=[...]`). Een onbekend milestonenummer geeft
  502 (een leesfout van de forge), geen 404. De server controleert alleen de milestones die hij
  schrijft: geeft iemand intussen een derde milestone een rang, dan kan een gelijke stand
  ontstaan; de weergaven lossen die deterministisch op (berekende volgorde), de server rekent
  zelf geen volgorde uit.
- **`Origin` moet gelijk zijn aan `Host`.** Het endpoint schrijft op de forge met de rechten van
  de server, dus weigert het een verzoek waarvan de host[:poort] van `Origin` niet die van
  `Host` is. Zo kan een andere website in dezelfde browser het niet aanroepen.
- **Devproxy.** De Vite-devproxy staat daarom op `changeOrigin: false` (`web/vite.config.ts`):
  met `true` zou de proxy `Host` herschrijven naar de backend en antwoordt het endpoint 403.
- **Optimistisch.** De pagina toont de nieuwe volgorde meteen, zet de knoppen uit tot het
  antwoord er is, en draait de wijziging terug bij een fout. Bij een 409 verschijnt een
  melding en toont de lijst wat de forge nu heeft.

## Afgeleide begrippen

De SPA leidt deze begrippen af in `web/src/lib/priorities/derive.ts`; de server levert ze niet.

- **Actief.** Een issue is actief als er een open of geparkeerde run op staat, of als de
  bordstatus `In progress` is.
- **Bergop / bergaf.** Een actief issue is bergop als de fase van zijn run een van
  `brainstorming`, `start-issue`, `spec-review`, `writing-plans` of `plan-review` is: het
  denkwerk vóór de uitvoering. Elke andere fase, of een issue dat alleen op het bord actief is,
  is bergaf.
- **Wachtenden.** Per issue het aantal open issues dat er (direct of via ketens) op wacht. De
  leden van een cyclus vormen samen één component en krijgen de diepte van dat component, in
  één kolom; zo blijft de graaf eindig.
- **Shaping.** Een milestone is in shaping als hij geen open issues heeft en geen issues die als
  `completed` zijn gesloten (`isShaping`: nul issues), of als een regel van de
  milestonebeschrijving (zonder hoofdletters en omringende spaties) `Status: shaping` is. Een
  milestone met nul issues en een lege beschrijving is dus ook shaping. Alleen lege milestones
  (`empty`) sorteren onderaan; een shaping-milestone mét issues sorteert gewoon mee.

## Palet en controle

Het kleurpalet van de milestones is **niet gevalideerd** met de dataviz-validator. De
gebruikersinterface is **niet visueel gecontroleerd in een browser**: alleen de unit- en
componenttests en de build zijn gedraaid. Er is geen e2e-test: `web/e2e/fake-github.ts` kent geen
GraphQL-route die zonder wijziging van de bestaande tests uit te breiden is.

## Bekende beperkingen

- Een `"` of `\` in een milestonetitel laat de hele bouw falen; de refresher probeert het elke
  60 s opnieuw, op GitHub tegen ongeveer 1 punt per query.
- GitLab-groepsmilestones worden niet opgesomd (alleen projectmilestones). Zulke issues
  verdwijnen uit Milestones en uit Zonder milestone.
- GitLab `issues_statistics` koppelt op milestonetitel.
- Lange pijlen in de graaf delen één baan per overgeslagen kolom.
- Het palet is niet gevalideerd met de dataviz-validator.
- De gebruikersinterface is niet visueel in een browser gecontroleerd.
- De e2e-test is overgeslagen: `fake-github.ts` heeft geen GraphQL-route.
