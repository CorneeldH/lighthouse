# Richting per repo: strategie, uitgangspunten en areas, eerst in lighthouse zelf

Issue: #372. Verslag van het onderzoek: [richting-verslag](2026-11-22-issue-372-richting-verslag.md).
Milestone: *Systeemoverzicht: kaarten en richting die mens en model lezen*. Leunt op #476
([ontwerp](2026-11-22-issue-476-gepland-op-de-kaart-design.md), gemerged in #494).

## Aanleiding

Lighthouse heeft geen strategie of uitgangspunten in de repo. De README zegt "Roadmap: the milestones
of this repository" en verwijst voor scope naar een spec in personal_ai_agent. De richting bestaat
wel, maar verspreid over 61 specs en 18 milestones (verslag §9).

Drie lezers hebben een bron nodig om te toetsen of een issue bij de richting past:

- `lighthouse-write-issue` stap 3f, als een issue een vrij label met een document krijgt (#476);
- de geplande job `map-planned` uit #476, voor issues die buiten `write-issue` om ontstonden;
- later de suggesties op het Prioritering-tabblad (#439). Die zijn een no-go in deze milestone; dit
  ontwerp levert alleen de bron.

#476 bouwt de toets al. `write-issue` en `map-planned` lezen `paths.tenets` en `paths.areas` (*Leidend
beleid* en *Bewust niet*), en slaan stil over als een sleutel ontbreekt. Dit ontwerp levert de
documenten waar die toets op leunt, en de vorm ervan.

## Doel

Lighthouse heeft een korte, citeerbare set richtingsdocumenten zonder status, die een mens en een
model lezen op het moment dat er iets beslist wordt. Een test bewaakt de vorm.

## Besluiten van Corneel (gesprek 2026-11-22)

1. **Geen roadmapdocument.** De roadmap is rollend en bestaat uit de milestones met hun rangorde
   (#468). Documenten leggen alleen richting vast, nooit status.
2. **Drie soorten documenten:** strategie, uitgangspunten, en een bestand per area.
3. **De inhoud** komt uit bewijs (de beslissingen in de specs en milestones), daarna uit een gesprek
   waarin het model per onderwerp zegt wat het bewijs suggereert. Dat gesprek is gevoerd; de uitkomst
   staat in *Inhoud* hieronder.
4. **Eén product.** Lighthouse is een software factory die software maakt. De uitgangspunten gelden
   dus voor het product en voor het werk eraan. (In paia liepen een PKM en een software factory door
   elkaar; dat was een probleem.)
5. **De diagnose** volgt de presentatie *Drie soorten AI-verandering*: lighthouse is een type-3-product
   (AI-native: het proces opnieuw ontwerpen rond de technologie), en wat een engineer met agents
   begrenst, is intent, taste en context, niet code.
6. **De optionele feature voor elke repo** (`init --upgrade` maakt de documenten aan, plus uitleg voor
   gebruikers) gaat naar een **volgende milestone**, niet naar deze.
7. **Een periodieke herziening** van de richting wordt een eigen geplande job, `direction`, in een **volgende
   milestone**. Een schrijfmoment bij het sluiten van een milestone komt er nu **niet**.
8. **Geen typering** (sneller hetzelfde, nieuw product, AI-native) in de toets van een issue: te veel.
9. **Geen *Nabij doel* in de lighthouse-areas.** Dat is status, en de roadmap zijn de milestones.
   `close-issue` stap 5 slaat over als het area-bestand of de kop ontbreekt (§4).
10. **Goedkeuring van de inhoud.** Gebouwd in een interactieve sessie, niet door de controller. De PR
    blijft draft tot Corneel de inhoud goedkeurt; de sleutels in `lighthouse.toml` gaan in dezelfde PR,
    zodat er één schakelmoment is (§7).

## Niet-doelen

- Geen modelsuggesties op het Prioritering-tabblad (#439, no-go van de milestone).
- Geen werk in personal_ai_agent.
- Geen sjablonen of `init`-wijziging voor andere repo's (besluit 6).
- Geen geplande herziening en geen schrijfmoment bij het sluiten van een milestone (besluit 7).
- De spanningen uit het verslag (§9: "merget nooit", de UI die schrijft, eigen gereedschap tegenover
  generiek) lost dit ontwerp niet op. Het maakt ze zichtbaar.

## Anti-doelen

- Een document met status: issuelijsten, datums, "nu bezig". Dat veroudert binnen weken (verslag §1).
- Richting in altijd-geladen context (`AGENTS.md`, `CLAUDE.md`): kost tokens zonder winst (verslag §7).
- Uitgangspunten die niets beslissen: elk heeft een tegendeel dat iemand redelijkerwijs zou kiezen,
  en minstens twee beslissingen als bewijs.
- Een tweede drager van de vuistregel per area: die blijft alleen in `docs/developer/labels.md`.
- Een skill of Go-code in `pack/core` die een lighthouse-specifiek pad of een areanaam kent.

## Aanpakken

**Roadmap.**

- *Geen document (gekozen).* De milestones en hun rangorde zijn de roadmap.
- *Een Now/Next/Later-bestand.* Afgevallen: het heeft een schrijver nodig, en anders verloopt het als
  de ROADMAP van paia.

**Hoe de inhoud ontstaat.**

- *Afleiden uit bewijs (A).* Gekozen, als eerste stap.
- *Concept uit README en labels (B).* Afgevallen: zonder bewijs komen er platitudes uit.
- *Interview (C).* Gekozen, als tweede stap, met wat A suggereerde als voorstel per vraag.

**Herziening.**

- *Een geplande job (A).* Gekozen, voor een volgende milestone.
- *Een schrijfmoment bij het sluiten van een milestone (B).* Niet nu.

**De optionele feature voor elke repo.**

- *In dit blok.* Afgevallen: de milestone is al Large, en een vorm die nog verandert, kost elke repo
  een conflict bij `--upgrade`.
- *Een volgende milestone (gekozen).* De vorm bewijst zich eerst in lighthouse.

## Ontwerp

### 1. De documenten

Alle drie in het Nederlands (`[project] language = nl`), zonder status en zonder datums.

**`docs/developer/strategie.md`** — de kern van lighthouse als product, naar Rumelt. Koppen, in deze
volgorde:

| Kop | Inhoud |
|---|---|
| `## Diagnose` | wat een engineer met agents begrenst; één alinea plus de vijf begrippen |
| `## Voor wie` | de doelgroepen, in volgorde |
| `## Leidend beleid` | 3 tot 5 bullets |
| `## Bewust niet` | bullets met een ID `**S1**`, `**S2**`, … |
| `## Roadmap` | één zin: de milestones in hun rangorde zijn de roadmap; geen lijst |

**`docs/developer/uitgangspunten.md`** — de tenets.

- Een korte inleiding met de voorrangsregel: een harde regel (`AGENTS.md`, een hook, een gate) wint
  altijd van een uitgangspunt; bij een conflict tussen uitgangspunten wint de hogere.
- Per uitgangspunt een kop `## <letter>. <X> boven <Y>`, met precies één keer ` boven `. Daaronder één
  zin waarom, eventueel een zin over de reikwijdte (zoals bij A, C en G in §2), en een regel *Bewijs*
  met links naar minstens twee specs of milestones *(eigen keuze na Fable-review, f9)*.
- Links zijn relatief vanaf het doelbestand: vanuit `docs/developer/uitgangspunten.md` wordt
  `2026-…-design.md` dus `planning/specs/2026-…-design.md`, vanuit `docs/developer/areas/<area>.md`
  `../planning/specs/…` *(f10)*.
- De volgorde in het bestand is de voorrang: wie hoger staat, wint. De letters liggen vast en hoeven
  na een herschikking niet meer oplopend te staan *(eigen keuze)*. Een vervallen uitgangspunt houdt zijn
  letter, met `(vervallen)` in de kop.

**`docs/developer/areas/<area>.md`** — één bestand per `2-area:`-label in `docs/developer/labels.md`,
nu acht: `core`, `packs`, `init`, `server`, `dispatcher`, `usage`, `product`, `meta`. Geen
`index.md`: de vuistregel blijft in `labels.md`. Koppen, in deze volgorde:

| Kop | Inhoud |
|---|---|
| `## Crux` | één alinea: het ene probleem van deze area |
| `## Leidend beleid` | 3 tot 5 bullets |
| `## Bewust niet` | bullets met een ID `**N1**`, `**N2**`, …, elk met `→ <area>` als het bij een buur hoort |

Geen `## Nabij doel` (besluit 9).

**Koppen.** De lijsten van koppen zijn gesloten: geen andere `##`-kop, dus ook geen `## Nabij doel` in
een lighthouse-area *(f9)*. De test (§5) controleert voorlopig de Nederlandse koppen letterlijk; koppen
die in elke taal werken (`heading:<key>`), komen met de feature voor elke repo (#500) *(eigen keuze)*.
Tot dan vinden `write-issue` en `map-planned` *Crux*, *Leidend beleid* en *Bewust niet* op hun
betekenis, zoals #476 nu al doet *(f7)*. Uitzondering: de kop *Nabij doel* krijgt nu al een sleutel,
omdat `close-issue` hem moet herkennen om over te slaan (§4).

**Citeren.** Een skill of een mens citeert `uitgangspunt C`, `strategie/S2` of `core/N1`. Zo is een
toets naar zijn bron terug te volgen.

### 2. De inhoud

Afgesproken in het gesprek; de bouwer neemt deze tekst over en zet hem in de vorm van §1.

**Diagnose.**

> Agents schrijven de code. Wat een engineer met agents begrenst, is niet de code maar:
>
> - **intent**: weten wat je wilt en dat helder maken;
> - **taste**: weten hoe het werk moet gebeuren;
> - **context**: de juiste informatie op het juiste moment;
> - **aandacht**: hoofdpunten eerst, inzoomen waar nodig;
> - **telemetrie**: meten hoe modellen en harnesses worden ingezet, om dat te optimaliseren.
>
> Lighthouse maakt die expliciet en zet ze in de keten. Wie taste kan uitleggen en in de keten
> vastlegt, kan op het proces vertrouwen.

**Voor wie**, in deze volgorde: Corneel; een bevriende ontwikkelaar; collega's bij CEDA; iedereen.

**Uitgangspunten**, in volgorde van voorrang. De kop is het deel tot en met de eerste `<Y>`; een tweede
zin (A: vertrouwen per soort wijziging; C: "ook in metingen …"; G: de reikwijdte) gaat in de body *(f9)*.
De links hieronder zijn relatief vanaf deze spec; in de documenten worden ze herschreven (§1).

| | Uitgangspunt | Waarom (één zin, concept) | Bewijs (minstens) |
|---|---|---|---|
| A | De mens bepaalt intent en taste, de keten doet de rest op bewijs, boven de mens als ritueel goedkeurder. Vertrouwen geldt per soort wijziging en is intrekbaar. | Een goedkeuring die de uitkomst niet verandert, kost aandacht en levert niets. | [#294](2026-11-08-issue-294-tick-merget-design.md), [#121](2026-11-16-issue-121-geplande-runs-design.md), [pins](2026-11-15-issue-325-pins-design.md) |
| B | Het doel van een praktijk boven zijn menselijke vorm | Lighthouse is AI-native: een praktijk blijft om wat hij bereikt, niet omdat mensen hem zo deden. | [repo zonder Python](2026-11-16-repo-zonder-python-design.md), [handoffs](2026-11-01-handoffs-write-pr-close-issue-design.md), [gate na commit](2026-10-27-gate-na-commit-design.md) |
| C | Luid "kon niet controleren" boven stil groen, ook in metingen: een eerlijk label boven een mooie metriek | Een gate die stil stopt met kijken, ziet eruit als schone code. | [zonder Python](2026-11-07-zonder-python-design.md), [canary per regel](2026-11-19-canary-per-regel-design.md), [kaarten](2026-10-27-kaarten-design.md) |
| D | Een check boven een oordeel of een herinnering: wat te controleren is, wordt een check, en een fout wordt er een | Een check geeft op elke machine hetzelfde oordeel; een herinnering niet. | [releasebeleid](2026-10-27-releasebeleid-design.md), [leesgrens triage](2026-11-01-triage-leesgrens-design.md), [beoordelingstabel](2026-11-21-issue-390-beoordelingstabel.md) |
| E | Eén bron boven een kopie die kan afdrijven | Twee bronnen lopen stil uit elkaar. | [forge via REST](2026-11-17-forge-github-rest-design.md), [werkplek zonder kenn-forge](2026-11-07-werkplek-zonder-kenn-forge-design.md), [milestoneprioriteit](2026-11-20-milestone-prioriteit-design.md) |
| F | Overzicht eerst, inzoomen waar nodig, boven alles tegelijk | Aandacht is schaars. | [systeemkaart](2026-10-31-systeemkaart-design.md), [plattegrond na init](2026-11-01-plattegrond-na-init-design.md), [prioritering](2026-11-16-prioritering-tabblad-design.md) |
| G | Weinig onderdelen die bewezen iets toevoegen boven meer machinerie. Dit geldt voor *wat* we bouwen, niet voor hoe grondig een gate controleert. | Elk onderdeel kost context, tokens en uitleg. | [spec-adversary opgeheven](2026-10-31-issue-143-spec-adversary-opheffen.md), [repo zonder Python](2026-11-16-repo-zonder-python-design.md), [website-download](2026-10-31-website-download-design.md) |

Waarom deze volgorde: A en B zeggen wat lighthouse is, C tot en met G hoe de keten dat waarmaakt. C
staat niet in conflict met A: zonder bewijs geldt A niet. F staat boven G: een overzichtslaag mag
erbij, ook als dat een onderdeel meer is.

Niet opgenomen, met reden (in het verslag): de SDP-toets, harness-neutraliteit en "zelflerend" als kern
(platitudes); "een vaardigheid voor elke repo boven maatwerk" (botst met "eerst eigen gereedschap", en
de volgorde van *Voor wie* dekt het); "review door een ander model" (al beleid, hoort in `core`).

**Leidend beleid en Bewust niet van de strategie** — concept uit het bewijs (verslag §9); Corneel keurt
het in de PR:

- Leidend beleid: vertrouwen bouwen op gemeten, deterministisch bewijs, zodat de keten doorloopt tot
  de merge; de mens houdt instructies, configuratie en promotie; het issue op de forge is de gedeelde
  staat; lighthouse levert het vermogen, elke repo past het toe.
- Bewust niet: productmonitoring; hosting op SDP of SRC; een tweede database; een tweede harness voor
  runs; een eigen sandbox; automatische promotie van model, skill of configuratie; een derde forge; de
  dispatcher op Windows of macOS; werk upstream uit eigen beweging.

**Areas** — concept uit het bewijs; de bouwer schrijft per area crux, beleid en Bewust niet met
citaten naar de specs, en Corneel keurt ze in de PR.

| Area | Crux (concept) |
|---|---|
| `core` | Een gate moet onbemand te vertrouwen zijn: nooit stil groen, nooit te omzeilen. |
| `packs` | Poorten per taal die bewezen vangen, zonder ze te versoepelen. |
| `init` | Installeren en upgraden zonder iets van de gebruiker kapot te maken. |
| `server` | De waarheid snel tonen, zonder verouderde data te verbergen. |
| `dispatcher` | Onbemand betrouwbaar draaien binnen de rem, zonder dubbele start. |
| `usage` | Het gebruiksvenster bewaken over sessies en runs. *Dun bewijs.* |
| `product` | Iemand die niet Corneel is, installeert zonder hulp en landt een PR. |
| `meta` | De repo als snelste lus voor agents, en als dogfood. |

Waar het bewijs dun is (`usage`; weinig afgevallen alternatieven bij `product`), zegt het bestand dat
in de crux niet; de bouwer meldt het in de PR.

### 3. Configuratie

- `lighthouse.toml` krijgt onder `[paths]`: `tenets = "docs/developer/uitgangspunten.md"`,
  `areas = "docs/developer/areas"` en de nieuwe sleutel `strategy = "docs/developer/strategie.md"`.
- `paths.strategy` is een nieuwe `kStr` zonder standaard in `internal/config/config.go`, naast
  `paths.tenets` en `paths.areas`.
- Mee: `tests/pack/fixtures/config-schema.json`, `tests/pack/fixtures/config/{defaults,full}/expect.json`,
  de invoer `tests/pack/fixtures/config/full/lighthouse.toml` (krijgt `strategy` onder `[paths]`; de
  fixture `defaults` houdt het geval "niet gezet") *(eigen keuze na spec-review, r1-f2)*, en een regel in `docs/site/data/config.json` met zijn `readers` (`docs/site/test/config.test.mjs`
  eist dat elke sleutel daar staat en dat de lezers hem echt noemen). De lezers van `paths.strategy`:
  `internal/repoconfig/repoconfig.go` en `pack/core/skills/write-issue`; de skill `map-planned` noemt de
  sleutel niet en telt dus niet. `paths.strategy` komt **niet** in de `UNREAD`-set: hij heeft lezers in
  dezelfde PR *(f4)*. De `about` van `paths.areas` (`config.json`, nu "near goal") volgt §4.
- `paths.history` blijft ongebruikt; dit ontwerp zet hem niet.

### 4. De lezers

Generiek in `pack/core`: geen pad, geen areanaam. Zonder sleutel slaat elke lezer stil over; een repo
zonder deze documenten merkt niets. Een sleutel die wél gezet is maar naar een bestand of map wijst die
niet bestaat, is een configfout en geen "niets": `write-issue` zegt het in het gesprek, de job
`map-planned` faalt luid in zijn poort (uitgangspunt C) *(f2)*.

- **`write-issue`, de pitch van een milestone.** Nu staat er: "If the project has tenets
  (`paths.tenets`), the pitch names the crux of its main area" — maar de crux staat onder `paths.areas`,
  en een lighthouse-milestone is een thema, geen area. Nieuw: met `paths.areas` noemt de pitch de crux
  van de area('s) waar de issues van de milestone het meest onder vallen (hun `labels.area_prefix`-label),
  en met `paths.strategy` toetst hij de milestone aan *Diagnose* en *Bewust niet* (`strategie/S<n>`).
  Twijfel wordt een vraag in het gesprek *(f7)*.
- **`write-issue` 3f, de toets "willen we het?" uit #476.** Leest naast de tenets en het area-bestand
  ook `paths.strategy`, en noemt bij de vraag het ID dat het oordeel draagt (`core/N2`,
  `strategie/S1`, `uitgangspunt G`).
- **`map-planned` (#476).** Dezelfde bron, maar de skill heeft geen Bash en leest alleen paden uit het
  corpus. Dat betekent *(f2)*:
  - `repoconfig.Paths` en de ruwe struct (`internal/repoconfig/repoconfig.go`, beide; `Paths(raw.Paths)`
    is een structconversie) krijgen `Strategy`; `TestLoadReadsPaths` volgt.
  - Het corpus (`internal/scheduled/mapplanned/corpus.go`, `buildCorpus`) krijgt `strategy` en
    `area_prefix` (uit `labels.area_prefix`), zodat de skill een issue aan `<areas>/<area>.md` koppelt
    zonder te gokken; `TestStageWritesTheCorpus` volgt.
  - De poort (Go) controleert dat een gezet pad bestaat; zo niet, dan faalt hij met de naam (zie boven).
  - De skill (`pack/core/skills/map-planned/SKILL.md`): het corpusschema krijgt `strategy` en
    `area_prefix`; stap 4 draait als `tenets`, `areas` of `strategy` niet leeg is; de vraag over de
    wens noemt het ID dat het oordeel draagt. `internal/packcontract/mapplanned_test.go` (pint nu
    `` `tenets` `` en `` `areas` ``) volgt, net als de corpusbeschrijving in `docs/tick.md`.
  - Een twijfel over de wens blijft de vraag met de eigen kop van #476 (`🗺 Kaart · map-planned`),
    **niet** `🤖 Vraag`: een kop die zo begint, leest `internal/protocol` als vraag van de keten (ontwerp
    van #476, §3). Het AC van #372 volgt dat al.

- **`close-issue` stap 5.** Leest `paths.areas` al, en leidt de area af uit het deel vóór `:` in de
  titel van de gesloten milestone. Bij lighthouse is dat geen area ("Systeemoverzicht: …"), en veel
  titels hebben geen `:` ("M3 — Tweede omgeving", "GitLab-forge"). Nieuw, en vóór de `git worktree add`
  van die stap *(f6)*: geen `:` in de titel, bestaat `<paths.areas>/<area>.md` niet (de area in kleine
  letters: "Product" wordt `product.md`), of heeft het bestand geen kop *Nabij doel*, dan meldt de stap
  `areas: skipped — <reden>` en stopt hij. De kop krijgt een sleutel in de bron `pack/core/headings.toml`
  (bijvoorbeeld `area_near_goal`, `nl`: *Nabij doel*, `en`: *Near goal*), zodat de skill hem in elke taal
  herkent; de geïnstalleerde kopie `.lighthouse/headings.toml` komt via `init --upgrade` mee in dezelfde
  commit *(eigen keuze na spec-review, r1-f1)*. `internal/packcontract/closeissue_test.go` (stap 5) volgt. Deze wijziging landt uiterlijk in de
  PR die `paths.areas` zet; anders opent de eerstvolgende gesloten milestone een PR op een verkeerd bestand.

#476 staat op `main` (PR #494): de lezers in `write-issue` 3f en `map-planned` (`internal/scheduled/mapplanned/`,
`repoconfig.Paths`) bestaan, en dit ontwerp breidt ze uit.

### 5. De test

Een Go-test in deze repo, in CI, die faalt met `pad:regel: melding` en alle fouten tegelijk geeft
(naar het voorbeeld van de contracttest van [qualm](https://github.com/tools4imps/qualm)). Hij leest de
paden uit `lighthouse.toml` en de area-tabel uit `labels.md`:

1. Elk `2-area:<x>` heeft `<paths.areas>/<x>.md`; elk bestand in die map hoort bij een label.
2. Elk area-bestand heeft precies de koppen van §1, in die volgorde, en geen andere `##`-kop.
3. Elke bullet onder *Bewust niet* heeft precies één ID van de juiste vorm (`**N<n>**`), de ID's zijn
   uniek binnen het bestand, en elke `→ <area>` noemt een bestaande area *(eigen keuze na spec-review,
   r1-f3)*.
4. `uitgangspunten.md`: elke kop heeft de vorm `## <letter>. … boven …` met precies één ` boven `;
   letters uniek, zonder gat in A tot en met de hoogste letter (een vervallen letter telt mee). De
   volgorde in het bestand is vrij. Elk uitgangspunt dat niet vervallen is, heeft een regel *Bewijs*
   met minstens twee links *(f9)*.
5. `strategie.md`: precies de koppen van §1, in die volgorde; elke bullet onder *Bewust niet* heeft
   precies één ID `**S<n>**`, en de ID's zijn uniek.
6. Een sleutel die niet gezet is, of een bestand dat niet te lezen is: de test faalt met de naam ervan.
   In deze repo zijn de sleutels gezet, dus "niet gezet" is hier een fout, geen reden om over te slaan.

De test controleert de vorm, niet of een issue past: dat oordeel hoort bij `write-issue`, de job of een
mens (verslag §8).

**De area-tabel lezen.** De parser van #476 (`internal/scheduled/mapplanned/taxonomy.go`) leest alleen
tabellen met `Label`, `Document` en `Planned`, niet de area-tabel. De test hergebruikt
`internal/labels.ParseTaxonomy` en filtert op `labels.area_prefix`; geen tweede parser (uitgangspunt E)
*(f3)*.

**De test moet ook draaien als alleen de documenten veranderen** *(f1)*. Nu valt `docs/` buiten het
go-domein van CI (`ci/domains/domains.go`, `notGoDirs`), en `go-test` draait alleen bij een wijziging in
dat domein; de commit-gate kiest pakketten alleen op gewijzigde Go-bestanden. Een PR die alleen
`areas/core.md` breekt, zou dus groen mergen: precies het stille groen van uitgangspunt C. Daarom gaan
`docs/developer/strategie.md`, `docs/developer/uitgangspunten.md`, `docs/developer/labels.md` en de map
`docs/developer/areas/` in het go-domein: `goDocs` krijgt ondersteuning voor een prefix, met rijen in
`ci/domains/domains_test.go` en het commentaar boven `goDocs` bijgewerkt *(eigen keuze, boven een aparte
workflow zoals `ci/loop_roadmap`: één mechanisme, uitgangspunt G)*.

### 6. README, docs, dogfood, changelog

- README: de regel "Roadmap: the milestones of this repository" krijgt een link naar
  `docs/developer/strategie.md` (in het Nederlands). De sectie *Design* met de link naar de scope-spec
  in personal_ai_agent wijst voortaan naar dat document.
- Onder `pack/` veranderen: `write-issue` (`claude/` en `agents/`), `close-issue` (`claude/` en
  `agents/`, stap 5), `map-planned/SKILL.md` (één bestand, zonder varianten) en `pack/core/headings.toml`.
  Na elke wijziging daar: `go run ./cmd/lighthouse init --upgrade` en de geïnstalleerde kopieën
  (`.claude/skills/`, `.agents/skills/`, `.lighthouse/headings.toml`) meecommitten; CI eist
  `No changes.` *(f8)*
- Docs die volgen: de corpusbeschrijving van `map-planned` in `docs/tick.md`, en de `about` van
  `paths.areas` en de nieuwe regel voor `paths.strategy` in `docs/site/data/config.json`.
- Volgorde van de commits, want `commit-test-gate` draait per commit *(f8)*: eerst de sleutel in het
  schema (anders faalt `TestConfigKeysKnown` op de skilltekst), dan de skills; de documenten en de
  sleutels in `lighthouse.toml` vóór of samen met de vormtest (die faalt op "niet gezet").
- Twee fragmenten in `changelog.d/`, want een fragment heeft één `type:` (`docs/releasing.md`):
  `<branch>.md` met `added` (de documenten, `paths.strategy`, de vormtest) en `<branch>-2.md` met
  `changed` (`write-issue`, `map-planned` en `close-issue` lezen de richting) *(f5)*.

### 7. Issues en milestone

Gedaan na goedkeuring van dit ontwerp (2026-11-22), via `/lighthouse-write-issue`:

- **#500, de optionele feature voor elke repo.** `init --upgrade` maakt sjablonen aan voor strategie,
  uitgangspunten en areas als ze ontbreken (zoals `labels.md` nu), plus uitleg in de docs voor
  gebruikers. Milestone *Product: eerste versie voor anderen* (Corneels keus, al raakt het de DoD daar
  niet); geblokkeerd door #372.
- **#501, de job `direction`.** Een geplande job naar het voorbeeld van `reflect`: een poort in Go
  (genoeg nieuwe specs of gesloten milestones sinds de vorige ronde), een skill die de beslissingen
  naast de uitgangspunten en areas legt, en een PR met een voorstel. Backlog: de DoD van
  *Explore/exploit* raakt hij niet.
- **#499, de tools van Impatient Programming** (flinch en praxis integreren, ideeën uit qualm, covenant
  en exhale), in milestone *Kwaliteitspoorten per taalpack*. Voortgekomen uit het onderzoek (verslag §8).
- **De pitch van deze milestone:** de bullet onder *Nog niet vastgelegd* heeft `→ #500`.
- **#372 zelf:** de twee AC onder *Optioneel voor elke repo* staan nu in #500; het AC van de job noemt de
  kop van #476 in plaats van "🤖 Vraag". Het label `3-needs-shaping` is eraf.

### 8. Hoe het landt

- **Eén PR** *(eigen keuze, nu #476 op `main` staat)*: de documenten, `paths.strategy` in het schema, de
  sleutels in `lighthouse.toml`, de wijziging in `close-issue` stap 5, de test met het go-domein in CI,
  de README, en de lezers (de pitch, 3f en `map-planned`). Gebouwd in een interactieve sessie, niet door de controller. De PR
  blijft **draft** tot Corneel de inhoud heeft goedgekeurd: de merge zet meteen echte lezers aan
  (`write-issue` leest `paths.tenets` al, `close-issue` en `map-planned` lezen `paths.areas`), en de tick
  merget een groene PR anders zelf (besluit 10).

## Aan de bouwer
- De precieze tekst van *Waarom* per uitgangspunt en de area-bestanden, met citaten naar de specs. De
  bouwer gebruikt het bewijs uit het verslag (§9).
- De plek van de test (hij hergebruikt `internal/labels.ParseTaxonomy`, §5).
- Het bewijs per area: §9 van het verslag geeft patronen, niet per area. De bouwer zoekt per
  area-bestand de beslissingen in de specs en milestones op.
