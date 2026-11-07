# Tick

`lighthouse tick` is de dispatcher-tick: een eigen proces dat een timer of cron periodiek start.
Het is een korte run, geen daemon. Wie issues vooraf routeert en naar `Controller` zet, leest
[triage.md](triage.md). Het ontwerp staat in
[de dispatcher-spec](developer/planning/specs/2026-10-27-m2-dispatcher-design.md) (§2 *Configuratie*,
§3 *De tick*).

## Wat tick doet

1. **Slot.** Neemt `${XDG_RUNTIME_DIR}/lighthouse/tick.lock`. Staat het slot nog vast, dan schrijft
   tick `lighthouse: previous tick still running` naar stderr en eindigt met exitcode 0. Zonder
   `XDG_RUNTIME_DIR` stopt tick met `tick: XDG_RUNTIME_DIR is not set: cannot take tick.lock`; bij een
   relatief pad met `tick: XDG_RUNTIME_DIR is not an absolute path: cannot take tick.lock`.
2. **Token.** Vereist `GH_TOKEN` of `GITHUB_TOKEN`. Zonder stopt tick met
   `tick: no GitHub token: set GH_TOKEN or GITHUB_TOKEN`. Het token heeft de scopes `repo` en
   `project` nodig: lighthouse leest en schrijft er comments, reacties en het board mee. Gebruik een
   classic token, of een fine-grained token dat elke repo dekt waarvan issues op het board staan:
   een item dat het token niet mag lezen kan een GraphQL-fout geven, en die slaat de hele repo over. De
   login van het token zelf heeft push-toegang tot de repo nodig (anders is de collaborator-permissie
   niet te lezen) en een rol in `[dispatch] answer_permissions`: zonder die rol vallen ook de eigen
   🤖-comments van lighthouse weg, zoals Gestart en Hervat.
3. **Configuratie.** Leest `[dispatch]` uit `host.toml` (`$LIGHTHOUSE_HOST_TOML`, anders
   `~/.config/lighthouse/host.toml`). Een ongeldige waarde stopt tick met een fout die de sleutel noemt. Een
   onbekende sleutel onder `[dispatch]` (een typfout als `max_runtme`) stopt tick ook, met een fout die
   de sleutel noemt; sleutels buiten `[dispatch]` blijven genegeerd. Ontbreekt `host.toml`, dan gelden de
   standaardwaarden en zijn er geen repos: tick schrijft dan `lighthouse: no repos in [dispatch] of <pad>`
   (zonder pad: `lighthouse: no host.toml: no repos to check`) naar stderr en eindigt met exitcode 0.
4. **Database.** Opent de SQLite-database naast `serve`
   (`${XDG_STATE_HOME:-~/.local/state}/lighthouse/lighthouse.db`, of `--db <pad>`). Daar staat de
   runstaat: claim, pogingen en vrijgeven.
5. **Controle per repo.** Leest het `lighthouse.toml` van elke repo in `[dispatch] repos`. Voldoet
   een repo niet, dan slaat tick hem luid over met `lighthouse: skip <pad>: <redenen>` op stderr en
   gaat door met de rest.
6. **Precheck.** Per bedienbare repo leest tick het board, de open runs en de runhistorie, en rekent
   daaruit een vingerafdruk: per item in Controller of On hold, of met een open run, de Status, Priority en de `updatedAt`
   van item en issue, de voorgangers van een On hold-item, de PR's van een item met een open run, en
   de open runs zelf met de stand van hun forge-sessie (`working`, `waiting` of `done`; een sessie die
   tick niet kan lezen telt als niet meetbaar), de resume-markers met `controller: true` waarvan de
   laatste run van de sessie in deze repo staat, en het einde van de pauze (zie *De rem*). De markers
   leest tick één keer per tick voor alle repo's; een onleesbare markermap stopt de tick, want dan
   kan noch de rem noch de afloop (regel 5) kijken. Per open run met een werkplek meet tick ook de
   afloop: de laatste gebeurtenis in het transcript (subagents meegeteld, per draad), of de sessie
   nog een eerste commando mist (`Unknown command:`) of nooit een assistent-beurt had, en de laatste
   permissie-prompt (`permission.json`, door de hook `permission-check` geschreven). Bij het
   Software-profiel haalt de werkplek het transcript uit de devcontainer, zodat ook daar een
   onbekend commando binnen één tick `start_failed` wordt (#237). Is er (nog) geen transcript, dan
   is de run niet meetbaar: geen fout, en de afloop valt terug op de forge-stand.
   Een run die tick niet kan meten (stand of afloop) schrijft
   `lighthouse: measure <repo>: run <id>: <fout>` naar stderr; de andere runs worden wel gemeten. Een
   sessie die de tick zonder sleutel aantreft (een onzekere start) zoekt `workplace.AdoptSessions`
   op; mislukt dat zoeken, dan schrijft tick `lighthouse: measure <repo>: <fout>` naar stderr en meet
   de rest toch. Vindt het haar niet, dan is dat aan decide (regel 15). Een nieuwe comment telt mee
   via de `updatedAt` van het issue; een sessie die klaar is via haar stand. Tick slaat de volledige gather
   alleen over als de vingerafdruk gelijk is aan de vorige, de vorige volledige gather jonger is dan
   30 minuten, en er sinds die gather geen tijdgrens viel. Tijdgrenzen zijn per open run
   `started_at + max_runtime` en `started_at + max_wait`, `max_wait` na zijn laatste activiteit en na
   zijn permissie-prompt, per marker zijn `reset_at`, en het begin van het `night`-venster.
   Activiteit en prompts staan niet in de vingerafdruk: ze veranderen elke tick, en deze grenzen laten
   tick kijken als hun `max_wait` verloopt.
   Vingerafdruk en tijd van de laatste volledige gather staan in de tabel `tick_state` van de database.
   Na de precheck schrijft tick per bedienbare repo ook de stand van zijn runs in `tick_state`
   (soort `run`, sleutel het issuenummer), elke tick, ook als de gather niet aan de beurt is: per
   open run en per geparkeerde run (met een marker die hem nog hervat) de titel van het issue van
   het board, `open` of `parked`, het commando, de trigger, het profiel, de workspace, `started_at`,
   de gemeten forge-stand (`working`, `waiting` of `done`; leeg als tick hem niet kon meten) en de
   tijd van de tick. Een issue met een open én een geparkeerde run staat er één keer, als `open`.
   Mislukt de precheck, dan blijven de rijen van de vorige tick staan. `serve` leest ze voor
   `GET /api/runs` en de kaart *Running runs*, zonder de forge (zie [install.md](install.md)).
7. **Gather.** Is de repo aan de beurt, dan leest tick de comments van elk item en bouwt de snapshot:
   de items in Controller of On hold, plus elk item met een open run in een andere status, met hun velden, voorgangers en PR's (de PR's die het issue sluiten
   plus de PR's op de branch van de laatste run van het issue, ook als die run al dicht is, met hun
   checks), hun comments en de open runs. Zo blijft een gemergde PR die het issue niet sluit zichtbaar.
   Een item met een open run buiten Controller en On hold doet mee om die run af te sluiten:
   `lighthouse-close-issue` zet het item op Done voordat tick de closecheck-run ziet eindigen. Voor
   zo'n item start of hervat tick niets, en hij zet het niet terug naar Controller. Alleen comments van vertrouwde auteurs komen erin: hun rol op de repo (`role_name` van de
   collaborator-permissie) staat in `[dispatch] answer_permissions`. Een 🤖-comment van een andere
   auteur valt weg met een regel op stderr. Een 🚀 telt alleen als de login van het token hem zette;
   die login vraagt tick hoogstens één keer per tick op, en niet als geen repo aan de beurt is. Na de
   comments leest tick de runhistorie (zie *De volgorde van een start*) en de skills in
   `.claude/skills/` van de main checkout. Na de gather schrijft tick
   `lighthouse: gathered <repo>: <n> items (<reden>)` naar stderr; de reden is `first`, `changed`,
   `stale` of `boundary`. Een fout van GitHub (ook een GraphQL-fout in een antwoord met status 200)
   slaat de repo luid over met `lighthouse: skip <pad>: <fout>` en bewaart niets, zodat de volgende
   tick opnieuw kijkt. Een issue met meer dan 10 voorgangers, of een voorganger met meer dan 5
   sluitende PR's, slaat de repo ook luid over: zo'n lijst leest tick niet half.

8. **Decide.** Eén keer per tick, over alle gegatherde repo's samen, beslist tick wat er moet
   gebeuren: een run sluiten, een issue teruggeven, een 🚀 herstellen, een issue parkeren of
   terugzetten (zie *Blokkades*), en per issue hoogstens één start of hervatting. Decide is puur: het
   leest alleen de snapshots, de runhistorie, de standen, de metingen, de markers en de klok. De
   regels staan in de spec (§4, §7); de afloop van een open run staat in *Afloop* hieronder.
9. **Apply.** Tick voert de acties in volgorde uit. Een mislukte actie houdt de rest niet tegen. Een
   uitgestelde start (buiten het `night`-venster, of een bezette werkplek)
   schrijft `lighthouse: deferred <repo>#<n>: <reden>` naar stderr. Een repo met een mislukte actie
   krijgt `lighthouse: apply <repo>: <fouten>`, en tick eindigt dan met exitcode 1. Wat apply
   onderweg meldt (een claim die een ander proces al had, een onzekere launch) staat ook op stderr,
   met `lighthouse: ` ervoor.
10. **Opruimen.** Na apply ruimt tick de forge-workspaces op van issues die niet meer op `Controller`
    of `On hold` staan, zonder open run, zonder levende vreemde sessie, schoon en gepusht. Dat gebeurt
    elke tick, ook voor een repo die niet *due* was, maar alleen voor een repo waarvan de precheck
    slaagde. Een verwijderde workspace geeft `lighthouse: removed workspace <id> of <repo>#<n>`. Een
    workspace die moet blijven, geeft geen regel: hij staat onder *Workspaces to clean up*. Een fout
    (een `Remove` die geen 409 is, een record dat niet weg te schrijven is) geeft
    `lighthouse: cleanup <repo>: <fouten>`, en tick eindigt met exitcode 1. De regels staan in
    [workplace.md](workplace.md), *Opruimen* en *Wie roept dit aan*.

**De vingerafdruk na apply.** Tick bewaart de vingerafdruk van de precheck pas na apply, en alleen
voor een repo zonder uitgestelde en zonder mislukte actie (spec §3 stap 2). Zo probeert de volgende
tick het opnieuw, ook als op het board niets veranderde: een start die op de nacht wacht, begint in
de eerste tick binnen het venster.

**De volgorde van een start.** Eerst bereidt apply de werkplek voor en kijkt of die bezet is, dan
claimt het de run (`runs.Take`), dan post het de 🤖 Gestart of Hervat, dan de 🚀 op de comment die
de start uitlokte, en als laatste start het de agent. Mislukt het schrijven van de run-instructies
of de post, dan sluit apply de run als `start_failed` en start er niets. `lighthouse run` neemt
geen `tick.lock`, maar volgt dezelfde volgorde: zijn rij staat in de database vóór zijn Gestart op
het issue. Daarom leest tick de runhistorie pas ná de comments. Wie de Gestart ziet, ziet zo ook de
rij, en tick geeft een verse run nooit terug als wees (`runstaat kwijt`). Claimen tick en `run`
dezelfde trigger tegelijk, dan wint één `runs.Take`; de ander post niets.

**Het pad in het commando.** Een handoff die in een andere checkout is geschreven, noemt zijn spec of
plan daar, bijvoorbeeld in een worktree onder de hoofdcheckout. De run kan dat pad niet lezen: zijn
devcontainer mount alleen de forge-worktrees (#237). Ligt het pad-argument van een start buiten de
worktree van de run, dan vervangt apply het door de langste staart van minstens twee delen die daar
als bestand bestaat (`dispatch.Relocate`). Dat geldt voor de Gestart, de rij en de prompt. Bestaat
geen staart, dan sluit apply de run na de claim als `start_failed`, met reden
`handoff path: <pad> is not in the worktree …`, en start er niets.

**Een geweigerde of onzekere launch.** Weigert de werkplek de launch, dan meldt apply
`<repo>#<n>: launch of run <id> not accepted: <reden>` op stderr, op één regel. Onder de 🤖 Gestart
post het een 🤖 Teruggegeven *start mislukt: <reden>* met de sessie van de run, en de run is gesloten
als `start_failed`. De volgende tick start een tweede poging, met een nieuwe Gestart of Hervat; tot
dan toont de UI het issue als teruggegeven. Een hervatting na de usage-reset krijgt de Teruggegeven
pas na haar tweede weigering: haar tweede poging post geen kop, dus een eerdere Teruggegeven bleef de
laatste kop terwijl de agent draait. Een onzekere launch geeft `<repo>#<n>: launch of run <id> uncertain:
<fout>` en post niets op het issue: er kan een agent draaien. Decide beslist later over de open run.

Run-instructies en dragers staan onder `${XDG_STATE_HOME:-~/.local/state}/lighthouse/dispatch`. De
forge-kant (workspace, agent starten, bezet, opruimen) staat in [workplace.md](workplace.md). Een
Vraag aan de gebruiker vanuit de werkplek post `lighthouse ask`, zie [ask.md](ask.md).

## Afloop

Per open run beslist decide elke tick wat er van de run wordt, op de metingen van stap 6. De
volgorde is vast en de eerste treffer wint. Het ontwerp staat in
[de dispatcher-spec](developer/planning/specs/2026-10-27-m2-dispatcher-design.md) (§7). "Klaar" is
een forge-stand `done`; "activiteit" is de laatste gebeurtenis in het transcript (alle draden), en
zonder transcript de tijd van de forge-sessie. Een PR telt alleen als hij van deze run is: een
gemergde of gesloten PR van vóór `started_at` niet. `max_wait` en `max_runtime` komen uit
`host.toml`; staat een limiet op nul, dan staan de regels die haar gebruiken uit.

| # | Situatie | Uitkomst | Reden op de 🤖 Teruggegeven |
|---|---|---|---|
| 1-3 | Klaar-comment, Handoff (niet het einde van de keten), Vraag | zoals voorheen, en de claude van de run beëindigen | |
| 4 | Een eigen 🤖 Teruggegeven van deze sessie staat er al | rij `returned`; de sessie stoppen als ze niet klaar is en de werkplek er nog is, zonder sessie de launch vergeten | |
| 5 | Resume-marker (`controller: true`) van deze sessie, geschreven op of na `started_at` van de run | rij `parked`, sessie blijft staan | |
| 6 | PR van deze run gemerged | rij `done`, de claude van de run beëindigen | |
| 6b | Klaar en het issue is `CLOSED` | rij `done`, geen post (een close-check-run die zijn issue sloot), de claude van de run beëindigen | |
| 7 | Groene PR en klaar | 🤖 Klaar, de claude van de run beëindigen | |
| 8 | Werkplek verdwenen | Teruggegeven | werkplek verdwenen |
| 9 | `Unknown command:` in het transcript | rij `start_failed`, sessie stoppen als ze niet klaar is | |
| 10 | Klaar, maar nooit een assistent-beurt | rij `start_failed` | |
| 11 | PR van deze run gesloten zonder merge, en geen open PR | Teruggegeven, sessie stoppen als ze niet klaar is | PR gesloten |
| 12 | Open PR is draft | Teruggegeven, sessie stoppen als ze niet klaar is | PR is draft |
| 13 | Klaar en een open PR met rode checks | Teruggegeven | PR rood |
| 14 | Klaar, geen PR, geen Handoff of Vraag | Teruggegeven | geen handoff of vraag |
| 15 | `max_wait` > 0, run gemeten maar zonder sessie, ouder dan `max_wait` | Teruggegeven, launch vergeten | start onzeker: geen sessie gevonden |
| 16 | `max_wait` > 0, permissie-prompt van deze run (niet van vóór `started_at`) waarna zijn draad `max_wait` lang niets deed | Teruggegeven, sessie blijft staan | wacht op permissie (met tool en pad) |
| 17 | `max_wait` > 0, niet klaar, geen openstaande permissie-prompt, geen activiteit in `max_wait` (geteld vanaf `started_at` op zijn vroegst) | Teruggegeven, sessie stoppen | geen activiteit in max_wait |
| 18 | Idem, maar zonder transcript: stand `waiting` en de forge-sessie `max_wait` oud, ook geteld vanaf `started_at` op zijn vroegst (zonder forge-tijd niet) | Teruggegeven, sessie stoppen | geen activiteit in max_wait (forge-stand; transcript niet meetbaar) |
| 19 | `max_runtime` > 0, run ouder dan `max_runtime` | Teruggegeven, sessie stoppen | time-out (max_runtime) |

Verschillen met de spec-tabel, bewust:

- Wacht op permissie (16) stopt de sessie niet: de prompt kan nog beantwoord worden. Een nieuwe
  start van het issue in dezelfde workspace beëindigt haar claude wel, na de claim: de nieuwe
  Handoff vervangt de openstaande prompt, en zolang die claude leeft weigert forge de start (#263,
  zie *Afloop*). Alleen
  activiteit van de draad van de prompt, nieuwer dan de prompt, telt als beantwoord; een subagent die
  verder werkt houdt de hoofdthread niet in leven. De hook schrijft `at` in milliseconden, zoals het
  transcript: de `tool_use` achter de prompt valt meestal in dezelfde seconde. Zolang een prompt
  openstaat, gelden 17 en 18 niet: een wachtende sessie schrijft niets.
- Een resume hergebruikt de sessie, en daarmee haar transcript en `permission.json`. Wat van vóór
  `started_at` van de run is, telt daarom niet als prompt of als laatste activiteit. Zo ook de
  marker: een marker van vóór `started_at` hoort bij een eerdere parkering (de usage-hervatting die
  hem beantwoordde kon hem niet verwijderen) en parkeert de hervatte run niet opnieuw. Parkeert de
  usage-guard de hervatte run zelf, dan schrijft hij een nieuwe marker met een nieuwe `created_at`.
- Regel 15 geldt alleen voor een run die tick heeft gemeten. Een storing bij forge geeft dus nooit
  een Teruggegeven voor een run die misschien draait.
- Regel 6b: een sessie die klaar is op een gesloten issue laat geen Handoff, Vraag of PR achter.

Apply voert de uitkomst uit. Een Teruggegeven post eerst en sluit de rij dan als `returned`; een
`close` met `Stop` stopt de sessie voor elke uitkomst, ook voor `start_failed`. Stoppen gaat op
de sessie-id van de run, niet op de forge-sleutel: de werkplek zoekt het proces op die id. Een stop
of vergeet op een rij zonder sessie of werkplek slaat apply over. Mislukt het stoppen na de post, dan
ziet de volgende tick regel 4 en post niets opnieuw: ze stopt of vergeet en sluit alleen. Een
`start_failed` krijgt de bestaande tweede poging. Een geweigerde launch post apply zelf al een
Teruggegeven met de oorzaak (*start mislukt: …*); na twee mislukte pogingen post tick die alleen als
de post van apply mislukte.

Een claude blijft na zijn laatste antwoord draaien, ook bij een forge-stand `done`, en forge weigert een
hervatting van zijn sessie zolang hij leeft (#206). Een run die sluit als `done`, `handoff` of
`question` (regels 1-3, 6, 6b, 7 en de fix-pr-handoff van een rode PR) draagt daarom `End`: apply
beëindigt met `Kill` alleen de claude van die sessie-id, nooit de unit, zodat een interactieve sessie in
dezelfde worktree doordraait. Een run zonder sessie-id slaat apply over. Mislukt `Kill`, dan blijft de
rij open en probeert de volgende tick het opnieuw; een 🤖 Klaar staat er dan al, en regel 1 sluit. Een
start of hervatting doet na de claim nog een `Kill`, vóór de 🤖 Gestart of Hervat en de launch: op
elke sessie in de workspace van een gesloten eigen run, in welke forge-stand ook, en bij een
hervatting ook op haar eigen sessie. Ook een geparkeerde run, een eerdere mislukte `Kill` of een run
die met *wacht op permissie* (16) is teruggegeven laat zo geen claude achter die de handoff blokkeert
(#263). Mislukt die `Kill`, dan sluit de run als `start_failed` (*end earlier claude: …*), zonder post.

## Exitcodes

| Code | Wanneer |
|---|---|
| 0 | Alle repo's zijn bedienbaar en hun precheck, gather en acties lukten (een uitgestelde start telt als gelukt), of een vorige tick draait nog |
| 1 | Minstens één repo overgeslagen (niet bedienbaar, of de gather mislukte), een mislukte actie in apply, een mislukte opruiming, of een fout (geen `XDG_RUNTIME_DIR`, geen token, ongeldige `host.toml`, database niet te openen) |

## Blokkades

Een voorganger is *actief* als hij `OPEN` is en geen gemergde PR heeft. Gesloten zonder merge telt als
vervallen. Een issue is *uitvoerend* als zijn laatste Handoff een `next` heeft buiten de spec-, plan-
en review-fase.

- **Parkeren.** Heeft een uitvoerend issue in Controller een actieve voorganger, dan parkeert tick het.
  Eerst komt de comment `🤖 Geparkeerd` (`🤖 Parked`) met één regel
  `blocked_by: <repo>#<n> (<boardstatus>), …`; staat de voorganger niet op hetzelfde board, dan staat
  er geen status tussen haakjes. Daarna gaat het issue op het board naar On hold. Een issue dat nog in
  de spec-, plan- of reviewfase zit, blijft staan; de blokkade hindert pas bij uitvoering.
- **Afmaken.** Slaagt de comment maar mislukt de boardwijziging, dan eindigt tick met exitcode 1 en
  bewaart hij de vingerafdruk van de repo niet. De volgende tick ziet zijn eigen Geparkeerd als laatste
  beurt en zet het issue alleen op On hold (`hold`). Hij post nooit een tweede Geparkeerd.
- **Terugzetten.** Een issue in On hold dat `OPEN` is, waarvan de laatste 🤖-comment (Rebased niet
  meegeteld) een eigen Geparkeerd is (auteur is de login van het token) en dat geen actieve voorganger
  meer heeft, gaat terug naar Controller (`unpark`). De volgende tick start de laatste Handoff.
- **Menselijk On hold.** Een issue dat een mens op On hold zette (de laatste 🤖-comment is geen
  Geparkeerd, of er staat er een van een andere auteur) raakt lighthouse niet aan. Lighthouse haalt een
  issue alleen uit Controller om het te parkeren.

## `lighthouse gather`

```bash
lighthouse gather <repo-dir> [--db <pad>]
```

Bouwt de snapshot van één repo zoals tick dat doet, en schrijft hem als JSON naar stdout (twee
spaties inspringing, `"version": 1`, tijden in UTC). Het gebruikt hetzelfde token, dezelfde
`host.toml` en dezelfde database als tick, maar neemt geen `tick.lock`, draait geen precheck en
bewaart geen stand in `tick_state`. De repo moet bedienbaar zijn (zie *Vereisten per repo*).

De uitvoer op een nep-GitHub is de fixture
[`internal/dispatch/testdata/snapshot.json`](../internal/dispatch/testdata/snapshot.json): daarop
wordt decide getest.

## `lighthouse run`

```bash
lighthouse run <owner/name>#<issue> [--db <pad>]
```

Start of hervat één issue nu, zoals de volgende tick dat zou doen. De repo moet in
`[dispatch] repos` staan (gezocht op de `repo` uit zijn `lighthouse.toml`) en bedienbaar zijn; anders
stopt `run` met exitcode 1, bijvoorbeeld met `run: <owner/name> is not in [dispatch] repos of <pad>`.
Hetzelfde token, dezelfde `host.toml` en dezelfde database als tick.

`run` neemt geen `tick.lock` en draait geen precheck: het doet een volledige gather van de repo, dan
decide, en voert alleen de acties van dat issue uit van de soort `close`, `defer`, `start` of `resume`.
De `close` gaat mee, want een open run die in deze beslissing sluit (bijvoorbeeld op een 🤖 Vraag met
een antwoord), zou de claim van de hervatting anders laten mislukken. Een Teruggegeven, een Klaar en
een 🚀-herstel laat `run` aan de tick. Hetzelfde geldt voor parkeren, afmaken en terugzetten (zie
*Blokkades*): een uitvoerend issue met een actieve voorganger geeft in `run` dus `nothing to start`. `run` bewaart geen vingerafdruk in `tick_state` en ruimt geen workspaces op:
dat doet alleen de tick.

Het eindigt met één regel op stderr, alle vier met exitcode 0:

- `lighthouse: started <repo>#<n>`;
- `lighthouse: deferred <repo>#<n>: <reden>` (buiten het `night`-venster, of een bezette werkplek);
- `lighthouse: not accepted <repo>#<n>`: de werkplek weigerde de launch. Onder de 🤖 Gestart staat
  een Teruggegeven met de reden, de run is gesloten als `start_failed`, en de volgende tick behandelt het als elke mislukte start
  (een tweede poging, na twee mislukte pogingen een Teruggegeven);
- `lighthouse: nothing to start for <repo>#<n>`, ook als de tick de claim van dezelfde trigger net
  won: dan post `run` niets (zie *De volgorde van een start*).

Een mislukte actie geeft `lighthouse: apply <repo>: <fouten>` en exitcode 1. Wat apply onderweg meldt,
staat ervoor op stderr, zoals bij tick.

## De rem

Voor elke start of hervatting die decide kiest, toetst tick één gedeelde rem over alle repo's van de
host. Per kandidaat, in deze volgorde; de eerste dichte rem geeft de uitsteltekst:

| Rem | Dicht als | Uitsteltekst |
|---|---|---|
| Pauze | de pauzevlag geldt (zie *Pauze*) | geen: de start valt weg, zonder defer |
| Usage | niet gemeten, of `session_pct` >= `halt_pct_session`, of `week_pct` >= `halt_pct_week` | `usage: not measured` of `usage: session 87%, week 40%` |
| Geheugen | `MemAvailable` niet gemeten, of `MemAvailable - n x reserve_per_run_gb < min_mem_available_gb` | `memory: MemAvailable not measured` of `memory: MemAvailable 9.5 GiB - 1 x 3 GiB < 8 GiB` |
| Plekken | `in_use` plus de al toegelaten starts >= `slots` | `slots: 4 of 4 in use` |
| Software | profiel `software`, en `software_in_use` plus de al toegelaten software-starts >= `max_software` | `software: 2 of 2 in use` |

- De rem houdt alleen starts en hervattingen tegen. De afloop van open runs (sluiten, Teruggegeven,
  stoppen, 🤖 Klaar, zie *Afloop*) gaat door, ook tijdens een pauze of bij een dichte usage-rem.
- `n` bij Geheugen is het aantal starts en hervattingen dat deze tick al door de rem kwam: een net
  gestarte run reserveert van dezelfde meting.
- `in_use` komt uit de database en geldt voor de hele host: de open runs over alle repo's, plus de
  geparkeerde runs (een marker met `controller: true` waarvan de laatste run van de sessie nog
  hervatbaar is). `software_in_use` telt daarvan de runs met profiel `software`. Een run die in deze
  tick sluit, telt nog mee; de volgende tick ziet de plek vrij.
- Een usage-hervatting (de geparkeerde run na de reset, trigger `usage`) slaat Plekken en Software
  over en telt er niet in mee: de run zit al in `in_use`. Hij telt wel mee in `n` van Geheugen.
- Een marker met `pause: true` hervat zodra er geen pauze meer geldt, ook vóór zijn `reset_at`; de
  toets `reset_at <= nu` slaat tick bij hem over.
- Een marker die nooit meer hervat (de laatste run sloot als Handoff, Vraag of Klaar, de herkansing
  is op, de laatste run sloot als `parked` maar de sleutel van de marker is al gebruikt, of het
  issue is gesloten of uit Controller en On hold) ruimt tick op, anders bleef hij in
  `in_use` tellen. Een marker van een repo die deze tick niet gathert, blijft staan.
- De uitgestelde starts staan met hun titel en reden in de tickstand (`tick_state`, soort
  `deferred`); de kaart *Running runs* toont ze onder *Deferred starts*. Bij een
  pauze valt de start weg zonder defer, zodat tick zijn vingerafdruk bewaart; de pauzevlag zit zelf
  in de vingerafdruk.
- [`lighthouse run`](#lighthouse-run) toetst alleen Pauze en Usage. Geheugen, Plekken en Software
  slaat het over: wie een issue met de hand start, beslist zelf.

De markers en de pauzevlag staan beschreven in [usage-gate.md](usage-gate.md) (*Contract*).

## Pauze

```bash
lighthouse pause [--until HH:MM]
lighthouse pause off
```

`lighthouse pause` schrijft de pauzevlag: tot het einde ervan start de tick zelf niets meer. Een
lopende controller-run gaat niet door: de usage-gate-hook toetst de vlag in stand `tool`, dus de run
parkeert bij zijn volgende tool-call van de hoofdthread (zie
[usage-gate.md](usage-gate.md#de-pauzevlag)). Een geparkeerde run hervat na het einde van de pauze
(of na `pause off`). Tot die tijd staat hij als `parked` in
`GET /api/runs` en op de kaart *Running runs* (zie [install.md](install.md#running-runs)).

- Zonder `--until` duurt de pauze tot het eerstvolgende begin van `[dispatch] night` (21:00 bij de
  standaard): vandaag, of morgen als dat begin vandaag al voorbij is. Een lege `night` (begin gelijk
  aan einde) heeft geen begin; dan is `--until` verplicht.
- Tijdens `night` is `--until` verplicht; de fout stelt de eerstvolgende hele uren voor.
- `--until HH:MM` is het eerstvolgende moment met die klokstand, binnen 24 uur. Een klokstand die
  verder weg ligt, of die niet bestaat (de uren die de zomertijd overslaat), is een fout.
- Zomertijd telt mee: de klok is de lokale wandtijd, en bij de dubbele uren van het najaar geldt het
  eerste moment na nu.
- `lighthouse pause off` verwijdert de vlag; zonder vlag meldt het `lighthouse: no pause`.

Het pad van de vlag staat in [usage-gate.md](usage-gate.md) (*Contract*).

## `[dispatch]` in `host.toml`

Elke sleutel is optioneel; dit zijn de standaarden.

```toml
[dispatch]
repos = ["/home/me/repositories/lighthouse"]  # absolute paden van de main checkouts; standaard []
slots = 4                    # lopende plus geparkeerde runs op de host
max_software = 2             # runs tegelijk in het Software-profiel
min_mem_available_gb = 8     # rem: MemAvailable - n x reserve >= min
reserve_per_run_gb = 3       # reserve per lopende run
night = "21:00-08:00"        # venster voor `when: night`, lokale tijd
max_runtime = "8h"           # bovengrens per run
max_wait = "1h"              # grens voor een stille run, een onzekere start of een permissie-prompt zonder antwoord
answer_permissions = ["admin", "maintain"]

[dispatch.forge]
url = "http://127.0.0.1:8091"
token_file = "/home/me/.kenn/forge/auth_token"   # standaard: <home>/.kenn/forge/auth_token
```

`slots` en `max_software` zijn minstens 1, `min_mem_available_gb` en `reserve_per_run_gb` minstens 0.
`repos` mag alleen absolute paden bevatten. Per rechtenprofiel kan `[dispatch.profiles.<naam>]` staan;
de spec beschrijft de sleutels.

## Vereisten per repo

Het `lighthouse.toml` van elke repo in `repos` heeft:

- een `[controller]`-tabel;
- `[usage_guard]` met `controller = true`;
- `[forge.board]` met `project_number` (positief), `project_id`, `status_field_id` en
  `priority_field_id`, en onder `status_options` minstens `Controller` en `On hold`.

Ontbreekt iets, dan staan alle ontbrekende sleutels, bij naam, in één `skip`-regel.

## Zicht

Wat de tick gestart heeft, zie je terug in `GET /api/runs` en op de kaart *Running runs* van
`lighthouse serve`: de open en geparkeerde runs en de uitgestelde starts, uit de tickstand. Zie
[install.md](install.md#running-runs) voor het gebruik.
