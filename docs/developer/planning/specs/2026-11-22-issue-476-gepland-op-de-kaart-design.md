# Een issue met een vrij label met document komt als gepland item op de kaart

Issue: #476. Voorganger: [de loop-kaart bij de PR](2026-11-01-loop-kaart-bij-pr-design.md) (#219,
#233). Landingskant in personal_ai_agent: CorneeldH/personal_ai_agent#1798.

## Aanleiding

Een vrij label met een document (`docs/developer/labels.md`, *Vrije labels met een document*) koppelt
nu alleen het **landen** van werk aan het document: `write-issue` zet het label, `write-pr` werkt het
document bij in de PR die het werk levert. Wat gepland is, staat pas op een kaart als iemand het met
de hand toevoegt. Voor de `loop`-kaart vangt `ci/loop_roadmap.py` een ontbrekend `planned` item, maar
pas op `main` en alleen voor issues met een milestone. Generiek doet lighthouse niets.

## Doel

Een open issue met een vrij label met document staat als gepland item in dat document, voor elke repo
met zo'n rij, zonder directe commit op `main`. Bij het plaatsen toetst lighthouse twee vragen:

1. **Waar hoort het?** Onder welk onderdeel, welke stap of welke handoff van de kaart.
2. **Willen we het?** Past het bij de richting van de repo (tenets, areas)?

Vraag 1 gaat over de kaart, niet over het werk: wacht het antwoord, dan wacht alleen het item, en de
controller pakt het issue gewoon op. Vraag 2 gaat over het werk: is het antwoord niet ja, dan wacht
het issue zelf (`labels.needs_decision`).

## Niet-doelen

- **Geen paia in deze wijziging.** Eerst lighthouse; personal_ai_agent zet het later aan met een
  `[[schedule]]`-blok en een kolom in zijn `labels.md` (na #1798).
- **Geen productvisie voor lighthouse zelf.** Die komt met #372. Tot dan heeft lighthouse geen
  `paths.tenets` of `paths.areas`: `write-issue` stelt vraag 2 aan Corneel zonder bron, en de job
  stelt hem hier niet.
- **Geen labelcontrole op alle open issues.** De job kijkt alleen naar issues die het label al dragen;
  of een issue zonder label het label had moeten krijgen, blijft bij `write-issue` 3f.
- **Geen wijziging aan de landingskant.** `write-pr` *Documents of the linked issues* blijft zoals hij is.

## Anti-doelen

- Een skill of Go-code in `pack/core` of `internal/` die een labelnaam (`loop`, `kaart`) of een pad
  (`loop.json`, `kaart.yaml`) kent.
- Een `main` die tot een dag rood staat omdat de job nog niet draaide.
- Een agent die zelf op de forge schrijft (comments, labels): een geplande run heeft geen token.

## Aanpakken

- **A — een dagelijkse geplande job (gekozen).** Een nieuwe job in het bestaande `scheduled.Job`
  (`internal/scheduled/scheduled.go`). De poort in Go zoekt open issues die nog niet in hun document
  staan; alleen dan start een agent. Die zet de items erin; de tick commit, opent één PR en merget hem
  als hij groen is. `Allowed` beperkt de run tot de documenten uit het taxonomiebestand. Kost: een item
  verschijnt tot een dag later, en het werkt alleen in een repo die de tick draait.
- **B — `write-pr` neemt ontbrekende items mee in elke PR.** Afgevallen: parallelle controller-runs
  voegen hetzelfde item toe (merge-conflicten, extra fix-pr-runs), en een PR krijgt wijzigingen die er
  niet bij horen.
- **C — `write-issue` opent een eigen PR per issue.** Afgevallen: per issue een PR, een CI-run en een
  merge door Corneel; bij een reeks issues ruis.

Corneel koos A, dagelijks.

## Ontwerp

### 1. `labels.md`: de kolom `Planned`

De tabel *Vrije labels met een document* krijgt een kolom `Planned`, met één van twee waarden:

| Waarde | Betekenis |
|---|---|
| `always` | elk open issue met het label hoort als gepland item in het document |
| `milestone` | alleen een open issue met het label én een milestone |

Een lege cel of een rij zonder de kolom: het label heeft geen aanmaakkant, en de job slaat het over.
Een andere waarde is een fout die de job meldt (zie 3). Voor deze repo:

| Label | Kleur | When | Document | Planned |
|---|---|---|---|---|
| `loop` | `F4D74B` | … | `docs/site/data/loop.json` | `milestone` |

Onder de tabel zegt de uitleg per document wat "als gepland toevoegen" betekent, zoals de uitleg van
het bijwerken er nu staat. Dat geldt ook voor een issue dat een bestaand onderdeel verandert: hoe de
kaart dat toont, verschilt per kaart en staat in de uitleg, niet in de skill. In paia krijgt een
bestaand onderdeel bijvoorbeeld *deels* met `issue`. De skill en de job lezen die uitleg; geen van
beide kent hem.

Voor `loop.json` in deze wijziging *(eigen keuze, uit drie opties)*: alleen een **nieuwe** stap,
handoff of route komt erbij als `planned` item met `issue` (`milestone` mag erbij). Een issue dat een
bestaande stap, handoff of route verandert, krijgt geen gepland item; dat onderdeel blijft `works` tot
de leverende PR, zoals nu. De job toetst zo'n issue wel (plek, en of het nieuw is) maar schrijft niets.
Een wijziging zichtbaar maken op de loop-kaart, zoals de paia-kaarten met *deels* doen, kan later: dan
moeten `docs/site/map.js`, `ci/loop_roadmap.py` en de regel voor `write-pr` mee.

Zo'n issue komt nooit in het document, dus de poort zou het elke dag opnieuw zien. Daarom heeft de
plek twee vaste waarden naast een gewone plek: *geen item* (het issue verandert een bestaand
onderdeel, en de uitleg zegt dat dat geen item geeft) en *niet op de kaart* (een mens besliste dat).
Een issue met een van die twee telt voor de poort als afgehandeld (zie 3, *Afgehandeld*).

Het document is JSON of YAML, en een gepland item noemt zijn issue in een veld `issue` met het
nummer als waarde. Dat is de enige vormeis die de poort stelt *(eigen keuze, zie 3)*.

### 2. `write-issue`: de toets bij het labelen

Stap 3f krijgt een tweede deel. Krijgt een issue een label met een `Planned`-waarde, en valt het
issue daaronder (bij `milestone`: de milestone uit 3d), dan toetst de skill:

1. **Waar hoort het?** Het document en de uitleg geven de kandidaten. Een nieuw onderdeel op de kaart
   is altijd een kandidaat, en *geen item* ook (een wijziging aan een bestaand onderdeel, als de
   uitleg zegt dat dat geen item geeft). Is de plek eenduidig, dan noemt de skill hem; anders vraagt
   hij het.
2. **Willen we het?** De skill vraagt het altijd, in hetzelfde gesprek, ook zonder tenets of areas.
   Geven `lighthouse config paths.tenets` of `paths.areas` een bestand of map, dan toetst de skill
   het issue eerst aan de tenets en aan *Leidend beleid* en *Bewust niet* van zijn area, en zegt hij
   bij de vraag wat daar uitkwam. Corneel koos dat deze toets in `write-issue` zit, bij de mens.

De plek gaat in de body van het issue onder een vaste kop, `heading:map_placement`, in één regel: een
plek, of de vaste waarde *geen item*. De kop komt in `.lighthouse/headings.toml` (`nl`: *Op de kaart*)
*(eigen keuze)*. Is het antwoord op vraag 2 niet ja (twijfel, of "later beslissen"), dan krijgt het
issue `labels.needs_decision`, met de reden in de body. Is het nee, dan komt er geen issue, of het
label met document gaat er niet op.

Een headless run (geen gesprek) schrijft alleen een eenduidige plek en stelt vraag 2 niet; dan laat
hij de kop weg en doet de job beide later.

`write-issue` schrijft nooit in het document en raakt geen git.

### 3. De job `map-planned`

Een nieuwe job in `internal/scheduled/mapplanned/`, geregistreerd met `Register` en toegevoegd aan
`scheduleJobs` in `internal/repoconfig/schedule.go` *(naam: eigen keuze)*.

**Poort (Go, zonder agent).** Op `origin/<standaardbranch>`:

1. `paths.labels` niet gezet, het bestand bestaat niet, of geen rij met `Document` en `Planned`:
   `skip` (*no planned documents*).
2. Per rij: de open issues met het label (bij `milestone`: met een milestone).
3. Een issue staat al in het document als een veld `issue` de waarde N heeft (`"issue": N` in JSON,
   `issue: N` in YAML). Een document dat niet parseert, of geen JSON of YAML is: de poort faalt met
   de naam van het document; de tick meldt het zoals een poortfout van `map-refresh`.
4. **Afgehandeld** telt niet mee: een issue met een beslissing van de job (*geen item*) of een
   antwoord *niet op de kaart* (zie *Vragen*). De poort leest alleen comments van de job, niet de
   body: *geen item* uit `heading:map_placement` wordt bij de eerste run een beslissing van de job,
   één agent-run per issue.
5. Een issue met een open vraag van de job zonder antwoord telt niet mee.
6. Blijft er niets over: `skip` (*every planned issue is on its map*). Anders draait de agent; de
   `Score` is het aantal issues.

**Stage.** Schrijft een corpus naar een stagingmap in de werkplek: per issue titel, body, labels,
milestone, het label en zijn document, en het antwoord op een eerdere vraag van de job als dat er is.
De agent erft geen forge-token en leest de issues dus uit dit corpus, niet van de forge.

**De agent.** Start met een nieuwe skill `lighthouse-map-planned` (in `pack/core`, generiek). Die leest
het taxonomiebestand, de uitleg per document, het corpus, en `paths.tenets`/`paths.areas` als ze er
zijn. Per issue:

- plek eenduidig (uit `heading:map_placement`, uit een antwoord, of uit het document zelf): zet het
  item in het document zoals de uitleg zegt;
- plek eenduidig *geen item*: schrijf een beslissing in het resultaatbestand van de stagingmap, zodat
  de poort het issue voortaan overslaat;
- plek onduidelijk: schrijf een vraag in het resultaatbestand, met de kandidaten als opties en een
  aanbevolen optie; raak het document voor dat issue niet aan;
- het issue heeft geen `heading:map_placement` (het kwam niet via `write-issue`) en de wens is
  twijfelachtig tegen tenets of areas: een vraag zoals hierboven, gemarkeerd als *wens*; het item
  komt er niet in tot het antwoord.

**Allowed.** Alleen de documenten uit de kolom `Document` en de stagingmap.

**Cleanup, Check, Finish.** `Cleanup` leest het resultaatbestand, post de vragen en beslissingen (zie
*Vragen*) en haalt dan de stagingmap weg. Dat gebeurt in `Cleanup` en niet in `Finish`: een run die
geen document wijzigt, eindigt in `Postcheck` na de insluiting (`internal/scheduled/postcheck.go`,
`len(paths) == 0`) en bereikt `Finish` nooit. `Cleanup` kan bij een herhaalde postcheck twee keer
draaien; hij post daarom niet voor een issue dat al een open comment van de job heeft. `Check` toetst
dat elk gewijzigd document nog parseert, en dat elk nieuw item een `issue` uit het corpus noemt. Geen
documentwijziging: `done` zonder PR, ook als er comments gepost zijn. Anders volgt de PR zoals bij
elke geplande run.

**Forge in de job.** `scheduled.Env` heeft nu geen forge. Hij krijgt `Forge forge.Forge` en
`Trusted func(ctx, login) (bool, error)` (de `answer_permissions` van de host, via
`dispatch.NewTrust`), gezet in `planRepo` en in `scheduledJob` van `internal/cli/tick_scheduled.go`.
`repoconfig.Repo` krijgt de `[paths]`-sleutels `labels`, `tenets` en `areas`.

**Vragen en beslissingen.** Beide zijn een comment op het issue, gepost vanuit Go na de run, met het
token van de repo, in de projecttaal. De kop is **geen protocolkop**: een kop die begint met
`🤖 Vraag` leest `internal/protocol` als vraag van de keten (`kindOf` in `header.go`). Die verbergt dan
een Handoff in `lastTurn`, en `newIssue` weigert het issue (`internal/dispatch/decide.go`). De job
gebruikt daarom een eigen kop, `🗺 Kaart · map-planned` (`en`: `🗺 Map · map-planned`), met daaronder
een machineregel `<!-- lighthouse:map-planned kind=<question|decision> topic=<place|wish> off=<letter> -->`
*(vorm: eigen keuze)*. `off` is de letter van *niet op de kaart*. Een test toetst dat
`protocol.ParseHeader` de comment niet herkent. De keten van het issue loopt zo door.

- **Een vraag over de plek** wacht alleen het item: geen label. De opties zijn letters, zoals bij
  `lighthouse ask`; *nieuw onderdeel op de kaart* en *niet op de kaart* zijn er altijd bij. Het
  antwoord is een comment van een rol uit `[dispatch] answer_permissions`, nieuwer dan de vraag, die
  met de letter begint. De poort leest die letter in Go: *niet op de kaart* is afgehandeld, elke
  andere letter (of een vrij antwoord) neemt het issue weer mee, en Stage zet het antwoord in het
  corpus.
- **Een vraag over de wens** wacht het werk: `Cleanup` zet ook `labels.needs_decision` met
  `issuestate.Apply` (de rangregel van `labels.md` geldt, zoals bij `lighthouse state label`). Het label
  haalt de mens eraf die beslist; de job haalt het niet weg. De poort neemt het issue weer mee zodra
  er een antwoord is.
- **Een beslissing** (*geen item*) is een comment met dezelfde kop en de vaste waarde; de poort leest
  hem als afgehandeld.

Een vraag blijft staan tot er een antwoord is; de job stelt hem niet opnieuw. Telt er meer dan één
vertrouwd antwoord, dan geldt het eerste dat met een letter begint, en zonder zo'n antwoord het
laatste vrije. Een wens die met een letter beantwoord is, wordt niet opnieuw gevraagd; alleen een
vrij antwoord zonder letter op een wensvraag laat de agent de wensvraag opnieuw stellen. De wenstoets
draait alleen voor een issue zonder antwoord: een antwoord op een plekvraag betekent dat de wens al
vaststaat.

### 4. De check van de `loop`-kaart

De hoofdmodus van `ci/loop_roadmap.py` laat de richting *elk open `loop`-issue met een milestone staat
als `planned` item op de kaart* vallen: de job is daar eigenaar van. De andere richting blijft: elk
`planned` item noemt een open issue met label `loop`. De PR-modus blijft gelijk. De workflow
`loop-roadmap` draait niet meer op `milestoned` en `demilestoned`. De uitleg in `labels.md` en de
docstring van het script volgen; `tests/test_loop_roadmap.py` toetst dat een milestoned `loop`-issue
zonder item de hoofdmodus groen laat.

### 5. Aanzetten in deze repo

`lighthouse.toml` krijgt:

```toml
[[schedule]]
name = "map-planned"
job  = "map-planned"
cron = "0 5 * * *"
```

### 6. Docs, dogfood, changelog

- `docs/tick.md`: een sectie *Job `map-planned`* naast `map-refresh` en `reflect`; `docs/install.md`:
  de job in *Scheduled runs*.
- `docs/developer/labels.md`: de kolom en de uitleg.
- Wijziging onder `pack/`: `go run ./cmd/lighthouse init --upgrade` en de geïnstalleerde kopie
  meecommitten; CI eist `No changes.`
- Een fragment in `changelog.d/`, type `added` (de job) en `changed` (`write-issue`).

### 7. Het issue

AC 1 van #476 zegt "via `write-issue`". Na goedkeuring van dit ontwerp wordt dat: `write-issue` zet
het label en de plek, de job `map-planned` zet het item in het document, via een PR van de tick.

## Beperkingen

- Artefacten in het Nederlands; code, skills en hookteksten in het Engels.
- `write-issue`, de skill `lighthouse-map-planned` en de job blijven generiek: geen labelnaam, geen pad.
- Zonder taxonomiebestand of zonder rij met `Planned` verandert er niets, ook niet aan `write-issue`.
- Werk in de worktree `.claude/worktrees/issue-476-kaart-bij-aanmaken`, branch
  `issue-476-kaart-bij-aanmaken`.

## Aan de bouwer

- Hoe de poort de tabel uit `labels.md` leest (dezelfde lezer als `write-pr` en `write-issue`
  aannemen, maar die is proza; in Go een tabelparser op de kolomkoppen).
- Hoe de vragen de `Cleanup` overleven.
- De vorm van het resultaatbestand en van het corpus, en de precieze kop van de job-comments.
- Of de PR-titel en -body van de geplande run (`chore: geplande run …`) volstaan.
