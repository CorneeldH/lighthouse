# Ontwerp: zelflerende lighthouse — telemetrie, kwaliteitspoorten en explore/exploit

Datum: 2026-11-16 · Bron: de paia-milestones
[#52 kwaliteitspoorten](https://github.com/CorneeldH/personal_ai_agent/milestone/52),
[#63 telemetrie](https://github.com/CorneeldH/personal_ai_agent/milestone/63), en hun vervolgen
[#67](https://github.com/CorneeldH/personal_ai_agent/milestone/67) en
[#68](https://github.com/CorneeldH/personal_ai_agent/milestone/68).

## Aanleiding en doel

Lighthouse ontwikkelt code: in paia, in zichzelf, en straks in de repo's van anderen. Een kern van
lighthouse is dat het bijhoudt hoe goed dat gaat, per onderdeel van de keten, en het systeem daarop
bijstelt. Zo wordt het een zelflerend systeem.

De concrete vraag: **hoeveel voegt een onderdeel toe, en wanneer?** Neem de planreview. Vangt hij
iets? Bij welke plangrootte, welke repo, welk model? Wat kost hij? En dezelfde vraag voor het model,
de harness, de lane en een skillversie. Op het antwoord stellen we het systeem bij (explore/exploit).

Dat werk staat nu in paia, in twee milestones die voor paia zijn geschreven: paden, scripts en
experimenten van de Beelink. Dit ontwerp verdeelt het opnieuw, met één regel: **lighthouse levert de
vaardigheid, elke repo past haar toe.** Paia is de eerste klant, niet de eigenaar.

Succes:

- Lighthouse laat per onderdeel zien wat het vond, wat ermee gebeurde, wat het kostte en hoe het
  traject afliep, uitgesplitst naar context (repo, plangrootte, model, harness).
- Lighthouse kan daarna een gecontroleerde vergelijking draaien en een besluit vastleggen, zonder
  nieuw gegevensmodel.
- Elk open issue van paia #52 en #63 heeft een bestemming: een lighthouse-vaardigheid, paia-toepassing,
  of gesloten met reden.

## Doelen, niet-doelen, anti-doelen

**Doelen**

- Drie lighthouse-milestones met een volledige beschrijving in de vorm van de bestaande
  (Problem, Appetite, Solution, Risks, Definition of Done, No-Gos):
  1. *Telemetrie: wat voegt elk onderdeel toe* — waarnemen.
  2. *Kwaliteitspoorten per taalpack* — betrouwbare poorten, en kwaliteit als meetsignaal.
  3. *Explore/exploit* — bijstellen; geparkeerd tot milestone 1 gegevens levert.
- Per milestone de issues, geschreven als lighthouse-vaardigheid, met een verwijzing naar het
  paia-issue waar het vandaan komt.
- Telemetrie is een lighthouse-functie voor elke gebruiker, niet een experiment op één host.
- Het gegevensmodel draagt vanaf het begin per run zijn **indeling** (zie *Aanpakken*), zodat
  schaduwmetingen en varianten later configuratie zijn, geen nieuw schema.

**Niet-doelen**

- Het bouwen zelf. Dit ontwerp levert milestones en issues; elk issue krijgt bij uitvoering zijn
  eigen spec of plan via de gewone route.
- Paia-toepassingswerk uitvoeren (zoals #1199, of de 175 ruff-bevindingen).
- Productmonitoring: gebruik of tevredenheid van wat een repo oplevert. De factory-grens van
  [M1](https://github.com/CorneeldH/lighthouse/milestone/2) blijft gelden.
- Superpowers automatisch bijwerken (deel 1 van paia #1087). Dat is hostbeheer, geen lighthouse-taak.
- Semantisch agentgeheugen of kennisextractie zoals in Lore. Hooguit later een explore-vraag.

**Anti-doelen** (mislukt, ook als het werkt)

- Een tweede opslag naast `lighthouse.db` voor dezelfde gegevens.
- Een dashboard dat "de review voegt X toe" laat lezen als oorzaak, terwijl het alleen waarneming is.
- Telemetrie die alleen werkt met kenn-forge, Codex-quotum, roborev of de Beelink.
- Een poort die stil stopt met kijken en er dan uitziet als schone code.
- Paia-issues één-op-één gekopieerd, met paia-paden en paia-scripts erin.
- Een model, skill of instelling die automatisch wordt gepromoveerd zonder menselijk besluit.

## Randvoorwaarden

- **Opslag.** `lighthouse.db` (SQLite, `internal/store`, genummerde migraties in
  `internal/store/migrations/`). De tabel `runs` legt per dispatcher-run trigger, poging, profiel,
  start, eind, uitkomst (`start_failed`, `handoff`, `question`, `parked`, `done`, `returned`) en reden
  vast. Model, harness, tokens en kosten ontbreken.
- **Transcripts.** `workplace.Transcript` is per run te lezen, in beide adapters (kenn-forge en
  lokaal). `internal/kpi` parset transcripts al om skillgedrag te meten.
- **Reviewrecords.** Plan- en specreview schrijven een record via `lighthouse plan-review-record`
  (`--kind plan|spec`, `internal/review/record.go`). Het record is een bestand in de datamap
  (`<kind>-review/<keten>/…`), niet in `lighthouse.db`, en wordt alleen geschreven op de host uit
  `host.toml` (`HostRefusal`); elders weigert het subcommando.
- **UI.** `internal/web` en `internal/server` serveren de lighthouse-UI.
- **Harnesses.** Claude Code draait de hooks; Codex en OpenCode niet (`AGENTS.md`, *Outside Claude
  Code*). Een meting die op een hook leunt, heeft daar geen bron.
- **Taalpacks.** `pack/` kent `core`, `go`, `python`, `node`, `expo` en `shell`.
- **Zonder Python.** Milestone [*Lighthouse zonder Python*](https://github.com/CorneeldH/lighthouse/milestone/10)
  zet de Python van `pack/` over naar Go, maar laat het Python-pack zelf buiten (een Python-repo heeft
  Python). Nieuwe meet- en poortlogica komt als Go-subcommando. Alleen een uitbreiding van de
  bestaande Python-poort (`pack/python/scripts/`) voor Python-repo's mag in Python *(eigen keuze)*.
- **Volume.** In 15 dagen 211 dispatcher-runs (162 in lighthouse, 49 in paia), zo'n 15 à 30 per
  werkdag. Genoeg om grote verschillen te zien, te weinig voor kleine.
- Artefacten in het Nederlands; code, namen en commentaar in het Engels.

## Aanpakken

### Hoe meet je wat een onderdeel toevoegt

- **A. Waarnemen.** Per traject vastleggen welke onderdelen draaiden, met welke versie en welk model,
  wat elk vond, wat ermee gebeurde, en hoe het afliep. Kost niets extra en loopt mee met gewoon werk.
  Maar het blijft correlatie: een review draait juist bij lastige issues.
- **B. Schaduw.** Een onderdeel draait, maar zijn uitkomst gaat niet terug naar de sessie. Achteraf
  vergelijk je wat het vond met wat na de merge misging. Zuiver voor reviews; kost quotum, en werkt
  niet voor model of harness.
- **C. Gecontroleerd variëren.** Per run lot de dispatcher één dimensie binnen een budget: review aan
  of uit, model X of Y. Het enige dat oorzaak laat zien; vraagt volume en een stopregel.

**Gekozen (door de gebruiker): A als fundament, ontworpen voor B en C.** Elke run draagt een indeling:
`standaard`, `schaduw:<onderdeel>` of `variant:<dimensie>=<waarde>`. Milestone 1 schrijft alleen
`standaard`, maar het veld bestaat. Een schaduwmeting op de reviews wordt daarmee configuratie, en
milestone 3 is "de dispatcher gaat loten".

### Waar het werk landt

- **Splitsen naar repo** (wat in `src/` van paia speelt, blijft in paia). Afgewezen: lighthouse moet
  juist paia en elke andere repo helpen met hun eigen code.
- **Splitsen naar vaardigheid tegenover toepassing** (gekozen). Wat een mechanisme is, wordt een
  lighthouse-issue. Wat dat mechanisme op één repo toepast (de bevindingen wegwerken, een typfout,
  een configrefactor), blijft een issue in die repo.

### Hoe het overgaat *(eigen keuze)*

- **`gh issue transfer`**: behoudt de geschiedenis, maar neemt paia-paden, -scripts en -labels mee
  die in lighthouse niet bestaan.
- **Nieuw issue met herkomstverwijzing** (gekozen). Het nieuwe issue beschrijft de vaardigheid in
  lighthouse-termen en linkt het paia-issue. Het paia-issue wordt gesloten met een verwijzing, of
  ingekort tot zijn toepassingsdeel. De geschiedenis blijft leesbaar in paia.

## De lus

Waarnemen, oordelen, bijstellen. Milestone 1 levert de waarneming. Milestone 2 maakt de poorten
betrouwbaar (een poort die liegt, vervuilt de meting) en levert kwaliteit als signaal. Milestone 3
sluit de lus.

## Milestone 1 — Telemetrie: wat voegt elk onderdeel toe

**Probleem.** Lighthouse weet per run alleen wanneer hij liep en hoe hij eindigde. Niet welk model of
welke harness, niet wat hij kostte, niet wat de reviews vonden, en niet of de PR na de merge hersteld
moest worden. Daardoor is "voegt de planreview iets toe?" niet te beantwoorden.

**Begrippen.**

- **Traject**: issue, eventueel spec en plan, de runs (ook hervattingen), de PR, de merge en het
  herstel daarna. De eenheid van analyse is het traject, niet de run: een plan loopt vaak over
  meerdere runs (paia #1495).
- **Bronnen van een traject (gekozen door de gebruiker)**: dispatcher-runs plus interactieve Claude
  Code-sessies, gekoppeld aan het issue via branch en worktree. Zo tellen interactieve spec- en
  planreviews mee, en heeft een gebruiker zonder dispatcher ook telemetrie. Codex- en
  OpenCode-sessies komen in milestone 3, met de harness-vergelijking; tot dan toont de UI ze als
  niet gedekt.
- **Onderdeel**: een stap die aan, uit of anders kan: specreview, planreview, de taakreview van SDD,
  de eindreview, de test-gate, de lane, het model, de harness, een skillversie.
- **Inzet per run**: model en effort, harness en versie, superpowers-versie, lighthouse- en
  packversie, en een hash van de gebruikte skills.
- **Indeling per run**: zie *Aanpakken*.
- **Bevinding**: wat een reviewonderdeel meldt, met ernst en besluit (overgenomen, afgewezen,
  bewust open).
- **Uitkomst**: hoe het traject eindigde, mensvragen en antwoorden, kosten en tokens per model,
  doorlooptijd met wachttijd apart, en herstel na merge.
- **Herstel na merge**: `fix`-commits en reverts die binnen 7 dagen dezelfde bestanden raken,
  bug-issues die naar de PR verwijzen, en `fix-pr`-rondes (de maat van paia #1082).

**Issues.**

| # | Issue | Herkomst |
|---|---|---|
| T1 | Gegevensmodel voor traject, inzet, indeling, bevinding en uitkomst in `lighthouse.db`; idempotente schrijvers, bron en herkomst per record | paia #67 (opslag), #1495 |
| T2 | Kosten, tokens en model per run en per interactieve Claude Code-sessie uit het transcript; een sessie hoort bij een traject via branch en worktree | paia #1495 |
| T3 | Bevindingen van de reviewonderdelen aan het traject koppelen: plan- en specreview-records, taakreview uit de SDD-ledger. `lighthouse.db` indexeert de recordbestanden (verwijzing plus inhoudshash), de bestanden blijven de bron; de hostbeperking van `plan-review-record` vervalt, zodat elke gebruiker records heeft *(eigen keuze)* | paia #993 (pijplijndeel) |
| T4 | Herstel na merge per PR | paia #1082 (stap 1 en 2) |
| T5 | Menswerk per traject: vragen, antwoorden, handmatige overname, wachttijd | paia #67 (punt 4) |
| T6 | Telemetrie in de lighthouse-UI: per onderdeel gevonden, overgenomen, kosten en uitkomst, per context; tijdlijn met versie- en modelwissels; ontbrekende dekking zichtbaar; elke waarneming gemarkeerd als correlatie | paia #1088 |
| T7 | Versiewissel als markering: een nieuwe superpowers-, model- of harnessversie verschijnt op de tijdlijn en stelt een meting voor. Bouwt op lighthouse #325 (pins en achterstand-lane); T7 voegt alleen de markering en het voorstel toe *(eigen keuze)* | paia #1087 (deel 2, alleen het signaal) |

**Appetite.** Large (5-6 dagen). Past T1 tot en met T7 niet, dan splitst de shaping; geen stille
scopegroei.

**Definition of Done (schets).**

- Een echt traject in lighthouse en een in paia zijn van issue tot herstel na merge te volgen, met
  inzet, bevindingen en kosten. Minstens één daarvan bevat een interactieve sessie naast
  dispatcher-runs.
- Een week gewoon werk wordt zonder losse opdracht verzameld; een traject zonder koppeling staat er
  met reden.
- De UI beantwoordt voor de planreview: hoe vaak vond hij iets, hoe vaak werd het overgenomen, wat
  kostte hij, en hoe liepen trajecten met en zonder hem af, met de waarschuwing dat dit correlatie is.
- Elk record heeft een indeling; alle runs in deze milestone zijn `standaard`.

**No-Gos.** Geen tweede opslag. Codex, roborev of kenn-forge zijn geen voorwaarde. Geen automatische
wijziging aan model, skill of configuratie. Geen productmonitoring.

## Milestone 2 — Kwaliteitspoorten per taalpack

**Probleem.** Lighthouse installeert per taal poorten, maar:

- van een blokkerende regel is niet bewezen dat de poort hem vangt (paia #1285: van de acht
  `BLOCKING_RULES` hebben er twee een canary);
- het pack controleert niet of de linter echt is aangesloten: in de dependencies, en als CI-stap die
  faalt op een bevinding (paia #470);
- structurele assen ontbreken of bestaan alleen in de Python-ratel: complexiteit per functie,
  duplicatie, importlagen, ongedeclareerde directe imports (paia #590, #644, #1192, #1286, #1287);
  `.golangci.yml` van lighthouse zelf staat op `default: standard`, zonder een van die assen;
- periodieke ontwerpmetingen (change amplification, hotspots) en de architectuurscan draaien alleen
  als iemand eraan denkt (paia #1193, #1205).

**Twee soorten.** Een **poort** is hard, blokkeert en heeft een canary. Een **meting** is periodiek,
blokkeert niets, schrijft een trend naar `lighthouse.db` (signaal voor milestone 1) en levert per
ronde hoogstens één beslisissue.

**Issues.**

| # | Issue | Herkomst |
|---|---|---|
| K1 | Canary per blokkerende regel in elke pack-hook, afgeleid van de regellijst van de hook zelf | paia #1285 |
| K2 | Linter aangesloten: per pack een check dat de linter in de dependencies staat en CI faalt op een bevinding | paia #470 |
| K3 | Directe imports gedeclareerd, per pack | paia #590 |
| K4 | Complexiteitsgrens per functie in elk pack: Go (ook deze repo), Node en Expo; Python bestaat | paia #1192, #1194 |
| K5 | Periodieke ontwerpmetingen als lighthouse-taak: duplicatie, importlagen, change amplification, hotspots | paia #1193, #1286, #1287 |
| K6 | Architectuurscan periodiek en onbemand, met precies één beslisissue | paia #1205 |
| K7 | Coverage-uitsluiting alleen met schuldissue; string-duplicatie als ratel-as | paia #644, #667 |

**Appetite.** Large (5-6 dagen). K1 tot en met K3 eerst: zij maken bestaande poorten betrouwbaar.
K5 en K6 hebben een schema nodig: lighthouse #121 (geplande runs) blokkeert ze, ze krijgen geen
eigen trigger *(eigen keuze)*.

**No-Gos.** Geen lint- of typeconfiguratie versoepelen om een poort groen te krijgen. Geen nieuwe
Python buiten `pack/python/`. Een nieuw gereedschap is gepind en de licentie is getoetst.

## Milestone 3 — Explore/exploit (geparkeerd)

**Probleem.** Milestone 1 laat zien wat er gebeurt, maar kiest niet. Er komt geen stabiele eindversie:
modellen, skills en harnesses blijven veranderen.

**Oplossing (schets).** De dispatcher deelt runs in binnen een budget: eerst `schaduw:<review>` om te
meten wat een review vangt (de kern van paia #1087 en #993), daarna `variant:` voor model, lane of
harness (paia #1495, Native tegenover SDD). Elke vergelijking heeft vooraf een hypothese, drempel,
budget en stopregel, en eindigt in een menselijk besluit: behouden, aanpassen, breder inzetten,
verder onderzoeken of stoppen. Codex- en OpenCode-sessies komen hier als traject-bron bij. Een extra
harness krijgt een controlematrix met positieve en negatieve
proeven; dezelfde instructietekst is geen bewijs (paia #1126, lighthouse #103). Lighthouse #168
(`auto_aanbevolen`) is een afnemer.

**Status.** Milestone met beschrijving, zonder appetite-start. Hij wordt actief zodra milestone 1 een
week gegevens heeft. Issues: het ontwerp (paia #1553) en de multi-harness-slice (paia #1126).

## Overzettabel

Uitgevoerd op 2026-11-16: [milestone 12](https://github.com/CorneeldH/lighthouse/milestone/12), [milestone 13](https://github.com/CorneeldH/lighthouse/milestone/13), [milestone 14](https://github.com/CorneeldH/lighthouse/milestone/14); de [voortgangstabel](../plans/2026-11-16-zelflerende-lighthouse-voortgang.md) houdt per artefact de stand bij.

| paia | Kort | Bestemming | Lighthouse |
|---|---|---|---|
| #1495 | Lane, model en kosten per run | T1, T2; de vergelijking Native/SDD naar milestone 3 | [#373](https://github.com/CorneeldH/lighthouse/issues/373), [#375](https://github.com/CorneeldH/lighthouse/issues/375), [#388](https://github.com/CorneeldH/lighthouse/issues/388) |
| #1088 | Dashboard | T6 | [#379](https://github.com/CorneeldH/lighthouse/issues/379) |
| #1087 | Herhaal review-experimenten | T7 (signaal) en milestone 3 (meting); auto-update vervalt | [#380](https://github.com/CorneeldH/lighthouse/issues/380), [#388](https://github.com/CorneeldH/lighthouse/issues/388) |
| #1082 | Herstel na merge | T4; de retro-review naar milestone 3 | [#377](https://github.com/CorneeldH/lighthouse/issues/377), [#388](https://github.com/CorneeldH/lighthouse/issues/388) |
| #993 | Lekken uit plan en spec | T3 (pijplijn); de meting naar milestone 3 | [#376](https://github.com/CorneeldH/lighthouse/issues/376), [#388](https://github.com/CorneeldH/lighthouse/issues/388) |
| #1395 | Instructies herbeoordelen | Los lighthouse-issue voor het `pack/core`-blok, zonder milestone; paia-deel blijft in paia | [#390](https://github.com/CorneeldH/lighthouse/issues/390) |
| #1285 | Canary per regel | K1 | [#381](https://github.com/CorneeldH/lighthouse/issues/381) |
| #470 | ruff aansluiten | K2; de bevindingen wegwerken blijft in paia | [#382](https://github.com/CorneeldH/lighthouse/issues/382) |
| #590 | Transitieve imports | K3; de vijf dependencies blijven in paia | [#383](https://github.com/CorneeldH/lighthouse/issues/383) |
| #1194 | TS-complexiteitslint | K4; de config van `apps/` blijft in paia | [#384](https://github.com/CorneeldH/lighthouse/issues/384) |
| #1193, #1286, #1287 | Ontwerpmetingen, importlagen, duplicatie | K5; paia's contracten in `pyproject.toml` blijven in paia | [#385](https://github.com/CorneeldH/lighthouse/issues/385) |
| #1205 | Architectuurscan | K6 | [#386](https://github.com/CorneeldH/lighthouse/issues/386) |
| #644, #667 | String-duplicatie, coverage `src/web/pages/` | K7; `pending` en de NiceGUI-testaanpak blijven in paia | [#387](https://github.com/CorneeldH/lighthouse/issues/387) |
| #1199 | Moduleconfig | Blijft in paia | — |
| #1553, #1126 | Explore/exploit, multi-harness | Milestone 3 | [#388](https://github.com/CorneeldH/lighthouse/issues/388), [#389](https://github.com/CorneeldH/lighthouse/issues/389) |

Bestaande lighthouse-issues *(eigen keuze)*:

| lighthouse | Kort | Relatie |
|---|---|---|
| #121 | Geplande runs | Blokkeert K5 en K6; blijft zonder milestone |
| #325 | Pins en achterstand-lane | Basis van T7; blijft zonder milestone |
| #103 | Kaart per harness | Naar milestone 3 |
| #168 | `auto_aanbevolen` | Naar milestone 3, als afnemer |
| #44, #128 | Dag- en procesverslag | Verwant aan T6, niet erin: een verslag is tekst per periode, T6 is de meet-UI; een verslag mag later uit de telemetrie lezen |

Milestones in paia: #63, #67 en #68 sluiten met een verwijzing naar de lighthouse-milestones. #52
heet voortaan *paia: kwaliteitspoorten toepassen* en houdt de toepassingsissues, die een
lighthouse-release afnemen.

## Volgorde

1. De drie milestones in lighthouse, met beschrijving.
2. De issues T1–T7, K1–K7, het losse #1395-issue en de twee issues van milestone 3, via
   `/lighthouse-write-issue`, elk met herkomstverwijzing.
3. Paia opruimen: issues sluiten of inkorten met verwijzing, milestones sluiten of hernoemen.

Stap 3 pas na stap 2, zodat elke verwijzing naar een bestaand issue wijst.

## Aan de bouwer

- Het exacte schema en de migratienummers van T1.
- Hoe T2 kosten berekent: uit het resultaatbericht van de run, of uit tokens maal een prijstabel.
- Per pack het gereedschap van K2 tot en met K5, mits gepind en met getoetste licentie.
- De verdeling van K-issues over meerdere issues als één issue te groot blijkt.
