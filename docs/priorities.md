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

## Wat het tabblad toont

`lighthouse serve` toont naast de overview een tabblad Prioritering (`#prioritering`) met drie
weergaven: een afhankelijkhedengraaf van open issues, een milestone-weergave met voortgang
(`Done` van `Done + Open`) en een weergave van issues zonder milestone. De server bouwt de
gegevens in een eigen refresher, los van de overview, elke **300 s** (`PrioritiesInterval`) en
levert ze op `/api/priorities`. De pagina leest alleen; ze schrijft niets terug naar GitHub.

## Afgeleide begrippen

De SPA leidt deze begrippen af in `web/src/lib/priorities/derive.ts`; de server levert ze niet.

- **Actief.** Een issue is actief als er een open of geparkeerde run op staat, of als de
  bordstatus `In progress` is.
- **Bergop / bergaf.** Een actief issue is bergop als de fase van zijn run een van
  `brainstorming`, `start-issue`, `spec-review`, `writing-plans` of `plan-review` is: het
  denkwerk vóór de uitvoering. Elke andere fase, of een issue dat alleen op het bord actief is,
  is bergaf.
- **Wachtenden.** Per issue het aantal open issues dat er (direct of via ketens) op wacht. Een
  blokkade in een cyclus telt als cyclus en krijgt geen diepte; zo blijft de graaf eindig.
- **Shaping.** Een milestone is in shaping als hij geen open of gesloten issues heeft, of als een
  regel van de milestonebeschrijving (zonder hoofdletters en omringende spaties) `Status:
  shaping` is. Shaping-milestones staan apart van de lopende.

## Palet en controle

Het kleurpalet van de milestones is **niet gevalideerd** met de dataviz-validator. De
gebruikersinterface is **niet visueel gecontroleerd in een browser**: alleen de unit- en
componenttests en de build zijn gedraaid. Er is geen e2e-test: `web/e2e/fake-github.ts` kent geen
GraphQL-route die zonder wijziging van de bestaande tests uit te breiden is.
