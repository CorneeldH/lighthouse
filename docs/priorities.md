# Priorities: wat het tabblad leest

Het tabblad Priorities leest per repo één `forge.Planning` (`internal/forge/planning.go`).
Dit document legt vast wat die lezing opvraagt, wat ze kost en wat live is gecontroleerd.

## Wat `Planning` leest (GitHub)

`Planning` gebruikt geen GraphQL: alleen REST, met conditionele requests (`ETag`), zodat een
ongewijzigde pagina als `304` terugkomt en niet meetelt voor de rate limit. Een leeg
GraphQL-budget laat het tabblad dus niet falen.

1. Issues: `issues?state=open` in pagina's van 100, pull requests overgeslagen. Elk issue draagt
   zijn milestone en labels mee (een open issue in een gesloten milestone houdt
   `Milestone.State == "closed"`). `PlanIssue.BlockedBy` komt uit `issue_dependencies_summary` in
   die lijst; heeft een issue blockers, dan leest `Planning` ze met één `blocked_by`-request per
   issue. Alleen **open** blockers komen erin: een gesloten blocker blokkeert niets.
2. Milestones: alle **open** milestones, ook die zonder issues.
3. Per open milestone één `issues?state=closed&milestone=<n>`; Go houdt de issues met
   `state_reason == completed` over in `MilestoneCount.Closed`, en `Done` is `len(Closed)`.
   `Open` is het aantal open issues (zonder pull requests) uit de lijst van punt 1.

`Done` telt alleen issues gesloten als *completed* (een oud issue zonder `state_reason` telt
als klaar, zoals in `report.go`). REST `closed_issues` telt *not planned* mee en
wordt daarom niet gebruikt. Pull requests en issues gesloten als *not planned* tellen in geen van
beide: niet in `Done` en niet in `Closed`, zodat de blokjes en het percentage niet uiteen kunnen
lopen.

## Kosten van één bouw (GitHub)

Een bouw kost geen GraphQL-punten. Hij leest één issuelijst (pagina's van 100), de open
milestones, per open milestone één lijst gesloten issues en per issue met blockers één
`blocked_by`-lijst. Elk request is conditioneel (`ETag`): een tweede bouw zonder wijzigingen
krijgt overal `304`, dat niet meetelt voor de rate limit. Heeft de issuelijst van de echte API
geen `issue_dependencies_summary`, dan leest `Planning` de `blocked_by`-lijst van elk issue: dat
is correct maar duurder. De zoekqualifiers en de GraphQL-kostenmeting van #437 zijn vervallen
met de queries waar ze over gingen.

## Twee kostenwijzigingen

- `BuildAll` leest het bord nu één keer per overview-bouw voor repo's die wel een bord hebben maar
  geen statusoptie voor de Controller: ongeveer 53 punten, bij ongeveer 40 bouwen per uur per
  repo. Voorheen werd dat bord daar nooit gelezen.
- GitLab-budget per bouw: 1 + ceil(open/100) issuepagina's, één links-aanroep per open issue en
  één `/issues?state=closed`-aanroep per pagina gesloten issues per open milestone.

## Wat het tabblad toont

`lighthouse serve` toont naast de overview een tabblad Priorities (`#priorities`) met drie
weergaven: een afhankelijkhedengraaf van open issues, een milestone-weergave met voortgang
(`Done` van `Done + Open`, waarbij `Open` het aantal open issues is uit de lijst open issues van `Planning.Issues`, per milestone geteld) en een weergave van issues zonder milestone. De server bouwt de
gegevens in een eigen refresher, los van de overview, elke **300 s** (`PrioritiesInterval`) en
levert ze op `/api/priorities`. De pagina leest, op één plek na: de rang van een milestone
(zie het volgende kopje).

## De rang van een milestone schrijven

De weergave Milestones heeft een schakelaar `Computed` / `Priority`. In `Computed` geldt de
berekende volgorde. In `Priority` staan milestones met een rang bovenaan, op rang, en de
milestones zonder rang daaronder in de berekende volgorde. De knoppen `Up` en `Down` wijzigen
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
  bordstatus `In progress` of `Controller` is (zoals `issueState` en de
  [spec](developer/planning/specs/2026-11-20-statusmodel-en-statusschema-design.md)).
- **Toestand van een issue.** `issueState` kiest de eerste passende regel: *actief* (live run,
  bordstatus `In progress` of `Controller`; met de vlag *mens nodig* bij een `needs_*`-label of
  een open vraag), *mens nodig* (`needs_shaping` of `needs_decision`), *geblokkeerd* (label
  `blocked_*` of open blockers), *mens nodig* (`needs_hands` of `needs_testing`), anders
  *oppakbaar*. Gesloten issues zijn *af* (`closed`). De milestone-balk tekent die vijf toestanden
  met één vormentaal en groepeert de blokjes per toestand. De API levert daarvoor per issue
  `label` (het label met de laagste rang), `needs_label` (er staat een `needs_*`-label op, welk
  ook), `open_question`, `prio_weight` en `factor` (zie *Prio-gewicht*), en per milestone de
  gesloten issues (`closed`).
- **Prio-gewicht.** De regel van #445 (`internal/prioweight`). Elke open milestone met een rang
  (`Priority: N`) krijgt een factor: bij M zulke milestones is dat M min de plaats, vanaf 0,
  waarop zijn rang voor het eerst staat in de oplopend gesorteerde rangen; gelijke rangen delen
  een factor, een milestone zonder rang heeft factor 0. `factor` is de factor van de eigen
  milestone van het issue. `prio_weight` is die factor plus, voor elk open issue van dezelfde
  repo dat (direct of via een keten) op dit issue wacht, diens factor maal 2^(k-1), met k de
  langste afstand in stappen; een cyclus telt als één groep. Een blokkeerder in een andere repo
  telt niet mee. De tick en de triage ordenen met hetzelfde gewicht (zie
  [`tick.md`](tick.md), *De rem*, en [`triage.md`](triage.md)).
- **Vormentaal van de graaf.** De graaf tekent de toestand van een issue op de kaart met
  dezelfde vormentaal als de balk: een kleurstrook (gestreept bij *actief*, effen bij
  elke andere toestand), een stippelrand van 1,5 px in de voorgrondkleur bij *mens nodig*, een kruis bij
  het nummer bij *geblokkeerd* en een doorgetrokken grijze rand voor een externe kaart (andere
  repo of buiten het filter), nooit gestippeld. Issues zonder afhankelijkheden staan als
  dezelfde kaart in een raster onder de graaf. Zie de
  [spec](developer/planning/specs/2026-11-20-statusmodel-en-statusschema-design.md) en het
  [plan](developer/planning/plans/2026-11-22-issue-438-afhankelijkhedengraaf-vormentaal.md).
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
componenttests en de build zijn gedraaid. Er is geen e2e-test: `web/e2e/fake-github.ts` kent de REST-routes
van `Planning` (milestones, dependencies) nog niet en is zonder wijziging van de bestaande tests niet uit te breiden.

## Bekende beperkingen

- GitLab-groepsmilestones worden niet opgesomd (alleen projectmilestones). Zulke issues
  verdwijnen uit Milestones en uit Without milestone.
- GitLab leest de gesloten issues van een milestone op milestonetitel (`/issues?state=closed&milestone=<titel>`).
- Lange pijlen in de graaf delen één baan per overgeslagen kolom.
- De kaartvorm van de graaf (kleurstrook, stippelrand, kruis) is alleen met jsdom getest, niet in een browser bekeken.
- Het palet is niet gevalideerd met de dataviz-validator.
- De gebruikersinterface is niet visueel in een browser gecontroleerd.
- De e2e-test is overgeslagen: `fake-github.ts` kent de REST-routes van `Planning` nog niet.
