# Tick

`lighthouse tick` is de dispatcher-tick: een eigen proces dat een timer of cron periodiek start.
Het is een korte run, geen daemon. Het ontwerp staat in
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
   daaruit een vingerafdruk: per item in Controller of On hold de Status, Priority en de `updatedAt`
   van item en issue, de voorgangers van een On hold-item, de PR's van een item met een open run, en
   de open runs zelf met de stand van hun forge-sessie (`working`, `waiting` of `done`; een sessie die
   tick niet kan lezen telt als niet meetbaar, en een fout bij het lezen schrijft
   `lighthouse: stand <repo>: <fout>` naar stderr). Per open run met een werkplek meet tick ook de
   afloop: de laatste gebeurtenis in het transcript (subagents meegeteld, per draad), of de sessie
   nog een eerste commando mist (`Unknown command:`) of nooit een assistent-beurt had, de laatste
   permissie-prompt (`permission.json`, door de hook `permission-check` geschreven) en een
   hervat-markering. Staat het transcript niet op deze host (het Software-profiel in een
   devcontainer), dan is de run niet meetbaar: geen fout, en de afloop valt terug op de forge-stand.
   Een run die tick niet kan meten schrijft `lighthouse: measure <repo>: run <id>: <fout>` naar stderr;
   de andere runs worden wel gemeten. Een sessie die de tick zonder sleutel aantreft (een onzekere
   start) zoekt `workplace.AdoptSessions` op; vindt het haar niet, dan is dat aan decide (regel 15). Een nieuwe comment telt mee via de `updatedAt` van het
   issue; een sessie die klaar is via haar stand. Tick slaat de volledige gather
   alleen over als de vingerafdruk gelijk is aan de vorige, de vorige volledige gather jonger is dan
   30 minuten, en er sinds die gather geen tijdgrens viel. Tijdgrenzen zijn per open run
   `started_at + max_runtime` en `started_at + max_wait`, en het begin van het `night`-venster.
   Vingerafdruk en tijd van de laatste volledige gather staan in de tabel `tick_state` van de database.
7. **Gather.** Is de repo aan de beurt, dan leest tick de comments van elk item en bouwt de snapshot:
   de items in Controller of On hold met hun velden, voorgangers en PR's (de PR's die het issue sluiten
   plus de PR's op de branch van de laatste run van het issue, ook als die run al dicht is, met hun
   checks), hun comments en de open runs. Zo blijft een gemergde PR die het issue niet sluit zichtbaar. Alleen
   comments van vertrouwde auteurs komen erin: hun rol op de repo (`role_name` van de
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
   gebeuren: een run sluiten, een issue teruggeven, een 🚀 herstellen, en per issue hoogstens één
   start of hervatting. Decide is puur: het leest alleen de snapshots, de runhistorie, de standen en
   de klok. De regels staan in de spec (§4, §7); de afloop van een open run staat in *Afloop* hieronder.
9. **Apply.** Tick voert de acties in volgorde uit. Een mislukte actie houdt de rest niet tegen. Een
   uitgestelde start (buiten het `night`-venster, een open voorganger, of een bezette werkplek)
   schrijft `lighthouse: deferred <repo>#<n>: <reden>` naar stderr. Een repo met een mislukte actie
   krijgt `lighthouse: apply <repo>: <fouten>`, en tick eindigt dan met exitcode 1. Wat apply
   onderweg meldt (een claim die een ander proces al had, een onzekere launch) staat ook op stderr,
   met `lighthouse: ` ervoor.

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
| 1-3 | Klaar-comment, Handoff (niet het einde van de keten), Vraag | zoals voorheen | |
| 4 | Een eigen 🤖 Teruggegeven van deze sessie staat er al | rij `returned`; de sessie stoppen als ze niet klaar is, zonder sessie de launch vergeten | |
| 5 | Hervat-markering (`Parked`) | rij `parked` | |
| 6 | PR van deze run gemerged | rij `done` | |
| 6b | Klaar en het issue is `CLOSED` | rij `done`, geen post (een close-check-run die zijn issue sloot) | |
| 7 | Groene PR en klaar | 🤖 Klaar | |
| 8 | Werkplek verdwenen | Teruggegeven | werkplek verdwenen |
| 9 | `Unknown command:` in het transcript | rij `start_failed`, sessie stoppen als ze niet klaar is | |
| 10 | Klaar, maar nooit een assistent-beurt | rij `start_failed` | |
| 11 | PR van deze run gesloten zonder merge | Teruggegeven, sessie stoppen als ze niet klaar is | PR gesloten |
| 12 | Open PR is draft | Teruggegeven, sessie stoppen als ze niet klaar is | PR is draft |
| 13 | Klaar en een open PR met rode checks | Teruggegeven | PR rood |
| 14 | Klaar, geen PR, geen Handoff of Vraag | Teruggegeven | geen handoff of vraag |
| 15 | `max_wait` > 0, run gemeten maar zonder sessie, ouder dan `max_wait` | Teruggegeven, launch vergeten | start onzeker: geen sessie gevonden |
| 16 | `max_wait` > 0, permissie-prompt waarna zijn draad `max_wait` lang niets deed | Teruggegeven, sessie blijft staan | wacht op permissie (met tool en pad) |
| 17 | `max_wait` > 0, niet klaar, geen activiteit in `max_wait` | Teruggegeven, sessie stoppen | geen activiteit in max_wait |
| 18 | Idem, maar zonder transcript: stand `waiting` en de forge-sessie `max_wait` oud | Teruggegeven, sessie stoppen | geen activiteit in max_wait (forge-stand; transcript niet meetbaar) |
| 19 | `max_runtime` > 0, run ouder dan `max_runtime` | Teruggegeven, sessie stoppen | time-out (max_runtime) |

Verschillen met de spec-tabel, bewust:

- Wacht op permissie (16) stopt de sessie niet: de prompt kan nog beantwoord worden. Alleen
  activiteit van de draad van de prompt, nieuwer dan de prompt, telt als beantwoord; een subagent die
  verder werkt houdt de hoofdthread niet in leven.
- Regel 15 geldt alleen voor een run die tick heeft gemeten. Een storing bij forge geeft dus nooit
  een Teruggegeven voor een run die misschien draait.
- Regel 6b: een sessie die klaar is op een gesloten issue laat geen Handoff, Vraag of PR achter.

Apply voert de uitkomst uit. Een Teruggegeven post eerst en sluit de rij dan als `returned`; een
`close` met `Stop` stopt de sessie voor elke uitkomst, ook voor `start_failed`. Een stop of
vergeet op een rij zonder sessie of werkplek slaat apply over. Mislukt het stoppen na de post, dan
ziet de volgende tick regel 4 en post niets opnieuw: ze stopt of vergeet en sluit alleen. Een
`start_failed` zonder Teruggegeven krijgt de bestaande tweede poging; na twee mislukte pogingen geeft
tick het issue terug met de oorzaak (*start mislukt: …*).

## Exitcodes

| Code | Wanneer |
|---|---|
| 0 | Alle repo's zijn bedienbaar en hun precheck, gather en acties lukten (een uitgestelde start telt als gelukt), of een vorige tick draait nog |
| 1 | Minstens één repo overgeslagen (niet bedienbaar, of de gather mislukte), een mislukte actie in apply, of een fout (geen `XDG_RUNTIME_DIR`, geen token, ongeldige `host.toml`, database niet te openen) |

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
een 🚀-herstel laat `run` aan de tick. `run` bewaart geen vingerafdruk in `tick_state`.

Het eindigt met één regel op stderr, alle vier met exitcode 0:

- `lighthouse: started <repo>#<n>`;
- `lighthouse: deferred <repo>#<n>: <reden>` (buiten het `night`-venster, een open voorganger, of een
  bezette werkplek);
- `lighthouse: not accepted <repo>#<n>`: de werkplek weigerde de launch. De 🤖 Gestart blijft staan,
  de run is gesloten als `start_failed`, en de volgende tick behandelt het als elke mislukte start
  (een tweede poging, na twee mislukte pogingen een Teruggegeven);
- `lighthouse: nothing to start for <repo>#<n>`, ook als de tick de claim van dezelfde trigger net
  won: dan post `run` niets (zie *De volgorde van een start*).

Een mislukte actie geeft `lighthouse: apply <repo>: <fouten>` en exitcode 1. Wat apply onderweg meldt,
staat ervoor op stderr, zoals bij tick.

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
