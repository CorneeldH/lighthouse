# Richting per repo: strategie, uitgangspunten en areas, eerst in lighthouse zelf

Issue: #372. Verslag van het onderzoek: [richting-verslag](2026-11-22-issue-372-richting-verslag.md).
Milestone: *Systeemoverzicht: kaarten en richting die mens en model lezen*. Leunt op #476
([ontwerp](https://github.com/CorneeldH/lighthouse/blob/issue-476-kaart-bij-aanmaken/docs/developer/planning/specs/2026-11-22-issue-476-gepland-op-de-kaart-design.md),
nog niet op `main`).

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
- Per uitgangspunt een kop `## <letter>. <X> boven <Y>`, één zin waarom, en een regel *Bewijs* met
  links naar minstens twee specs of milestones.
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

**Koppen.** De test (§5) controleert voorlopig de Nederlandse koppen letterlijk; koppen die in elke taal
werken (`heading:<key>`), komen met de feature voor elke repo *(eigen keuze)*. Uitzondering: de kop
*Nabij doel* krijgt nu al een sleutel, omdat `close-issue` hem moet herkennen (§4).

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

**Uitgangspunten**, in volgorde van voorrang:

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
  eist dat elke sleutel daar staat en dat de lezers hem echt noemen).
- `paths.history` blijft ongebruikt; dit ontwerp zet hem niet.

### 4. De lezers

Generiek in `pack/core`: geen pad, geen areanaam. Zonder sleutel of zonder bestand slaat elke lezer
stil over; een repo zonder deze documenten merkt niets.

- **`write-issue`, de pitch van een milestone.** Leest naast `paths.tenets` (al zo) ook
  `paths.strategy`: past de milestone bij de diagnose, en raakt hij een `strategie/S<n>`? Twijfel
  wordt een vraag in het gesprek.
- **`write-issue` 3f, de toets "willen we het?" uit #476.** Leest naast de tenets en het area-bestand
  ook `paths.strategy`, en noemt bij de vraag het ID dat het oordeel draagt (`core/N2`,
  `strategie/S1`, `uitgangspunt G`).
- **`map-planned` (#476).** Dezelfde bron. De `[paths]`-sleutels die #476 aan `repoconfig.Repo`
  toevoegt (`labels`, `tenets`, `areas`), krijgen `strategy` erbij. Een twijfel over de wens wordt
  de vraag met de eigen kop van #476 (`🗺 Kaart · map-planned`), **niet** `🤖 Vraag`: een kop die zo
  begint, leest `internal/protocol` als vraag van de keten (zie het ontwerp van #476, §3). Het AC van
  #372 dat "🤖 Vraag" noemt, volgt daarom #476.

- **`close-issue` stap 5.** Leest `paths.areas` al, en leidt de area af uit het deel vóór `:` in de
  titel van de gesloten milestone. Bij lighthouse is dat geen area ("Systeemoverzicht: …"). Nieuw: bestaat
  `<paths.areas>/<area>.md` niet, of heeft het geen kop *Nabij doel*, dan meldt de stap
  `areas: skipped — <reden>` en stopt hij. De kop krijgt een sleutel in de bron `pack/core/headings.toml`
  (bijvoorbeeld `area_near_goal`, `nl`: *Nabij doel*, `en`: *Near goal*), zodat de skill hem in elke taal
  herkent; de geïnstalleerde kopie `.lighthouse/headings.toml` komt via `init --upgrade` mee in dezelfde
  commit *(eigen keuze na spec-review, r1-f1)*. `internal/packcontract/closeissue_test.go` (stap 5) volgt. Deze wijziging landt uiterlijk in de
  PR die `paths.areas` zet; anders opent de eerstvolgende gesloten milestone een PR op een verkeerd bestand.

De lezers in `write-issue` 3f en `map-planned` landen pas als #476 op `main` staat. De documenten, de
configuratie, de wijziging in `close-issue` en de test (§5) hangen daar niet van af.

### 5. De test

Een Go-test in deze repo, in CI, die faalt met `pad:regel: melding` en alle fouten tegelijk geeft
(naar het voorbeeld van de contracttest van [qualm](https://github.com/tools4imps/qualm)). Hij leest de
paden uit `lighthouse.toml` en de area-tabel uit `labels.md`:

1. Elk `2-area:<x>` heeft `<paths.areas>/<x>.md`; elk bestand in die map hoort bij een label.
2. Elk area-bestand heeft de koppen van §1 in die volgorde.
3. Elke bullet onder *Bewust niet* heeft precies één ID van de juiste vorm (`**N<n>**`), de ID's zijn
   uniek binnen het bestand, en elke `→ <area>` noemt een bestaande area *(eigen keuze na spec-review,
   r1-f3)*.
4. `uitgangspunten.md`: elke kop heeft de vorm `## <letter>. … boven …`; letters uniek, zonder gat in
   A tot en met de hoogste letter (een vervallen letter telt mee). De volgorde in het bestand is vrij.
5. `strategie.md`: de koppen van §1 in die volgorde; elke bullet onder *Bewust niet* heeft precies één
   ID `**S<n>**`, en de ID's zijn uniek.
6. Een sleutel die niet gezet is, of een bestand dat niet te lezen is: de test faalt met de naam ervan.
   In deze repo zijn de sleutels gezet, dus "niet gezet" is hier een fout, geen reden om over te slaan.

De test controleert de vorm, niet of een issue past: dat oordeel hoort bij `write-issue`, de job of een
mens (verslag §8). Waar de test staat en of hij de tabelparser van #476 hergebruikt, laat dit ontwerp
aan de bouwer; een tweede parser voor dezelfde tabel is er niet nodig (uitgangspunt E).

### 6. README, docs, dogfood, changelog

- README: de regel "Roadmap: the milestones of this repository" krijgt een link naar
  `docs/developer/strategie.md` (in het Nederlands). De sectie *Design* met de link naar de scope-spec
  in personal_ai_agent wijst voortaan naar dat document.
- De wijziging in `write-issue` valt onder `pack/`: `go run ./cmd/lighthouse init --upgrade` en de
  geïnstalleerde kopie (`.claude/skills/`, `.agents/skills/`) meecommitten; CI eist `No changes.`
  Beide varianten (`claude/` en `agents/`) veranderen samen.
- Een fragment in `changelog.d/`: `added` (de documenten en `paths.strategy`), `changed` (`write-issue`
  leest de strategie).

### 7. Issues en milestone

Na goedkeuring van dit ontwerp, via `/lighthouse-write-issue`:

- **Nieuw issue: de optionele feature voor elke repo.** `init --upgrade` maakt sjablonen aan voor
  strategie, uitgangspunten en areas als ze ontbreken (zoals `labels.md` nu), plus uitleg in de docs
  voor gebruikers. Voorstel: milestone *Product: eerste versie voor anderen*.
- **Nieuw issue: de job `direction`.** Een geplande job naar het voorbeeld van `reflect`: een poort in Go
  (genoeg nieuwe specs of gesloten milestones sinds de vorige ronde), een skill die de beslissingen
  naast de uitgangspunten en areas legt, en een PR met een voorstel. Voorstel: milestone
  *Explore/exploit: lighthouse stelt zichzelf bij*, die ook de telemetrie bevat die de job later leest.
- **De pitch van deze milestone:** de bullet onder *Nog niet vastgelegd* krijgt `→ #<nr>` van het eerste
  nieuwe issue.
- **#372 zelf:** de twee AC onder *Optioneel voor elke repo* verhuizen naar het nieuwe issue; het AC van
  de job krijgt de kop van #476 in plaats van "🤖 Vraag". Het label `3-needs-shaping` gaat eraf.

### 8. Hoe het landt

- **PR 1, los van #476:** de documenten, `paths.strategy` in het schema, de sleutels in `lighthouse.toml`,
  de wijziging in `close-issue` stap 5, de test, de README en de pitch-lezer in `write-issue`. Gebouwd in
  een interactieve sessie, niet door de controller. De PR blijft **draft** tot Corneel de inhoud heeft
  goedgekeurd: de merge zet meteen echte lezers aan (`write-issue` leest `paths.tenets` al, `close-issue`
  leest `paths.areas`), en de tick merget een groene PR anders zelf (besluit 10).
- **PR 2, na de merge van #476:** `write-issue` 3f en `map-planned` lezen ook `paths.strategy` en noemen
  het ID dat het oordeel draagt.

## Aan de bouwer
- De precieze tekst van *Waarom* per uitgangspunt en de area-bestanden, met citaten naar de specs. De
  bouwer gebruikt het bewijs uit het verslag (§9).
- De plek van de test, en of hij de tabelparser van #476 hergebruikt.
- `paths.strategy` in de `UNREAD`-set van `docs/site/test/config.test.mjs` zolang geen lezer hem noemt.
- Het bewijs per area: §9 van het verslag geeft patronen, niet per area. De bouwer zoekt per
  area-bestand de beslissingen in de specs en milestones op.
