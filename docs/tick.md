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
2. **Token.** Het token komt per repo uit `GH_TOKEN` of `GITHUB_TOKEN` (github) of uit
   `GITLAB_TOKEN` (gitlab), of uit de variabele die `[forge] token_env` van die repo noemt. Een repo
   zonder token krijgt een skip-regel, net als een repo waarvan de forge om een andere reden niet opent.
   Tick stopt alleen als geen enkele repo een forge krijgt, en dan met de eerste tokenfout, bijvoorbeeld
   `tick: no GitHub token: set GH_TOKEN or GITHUB_TOKEN`, de variabele die `[forge] token_env` van die
   repo noemt, of `GITLAB_TOKEN`. `--forge-url` en `LIGHTHOUSE_FORGE_URL`
   gelden alleen voor GitHub-repo's. Het token heeft de scopes `repo` en
   `project` nodig: lighthouse leest en schrijft er comments, reacties en het board mee. Om een
   groene eigen PR te mergen (regel 7) heeft het token ook *Contents: write* nodig. Gebruik een
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
   Na apply schrijft tick per repo waarvan de precheck slaagde ook de stand van zijn runs in `tick_state`
   (soort `run`, sleutel het issuenummer), elke tick, ook als de gather niet aan de beurt is. Een
   run die apply in deze tick start of afsluit, staat er dus na deze tick al in of al niet meer in. Per
   open run en per geparkeerde run (met een marker die hem nog hervat) de titel van het issue van
   het board, `open` of `parked`, het commando, de fase (afgeleid uit het commando, bijvoorbeeld `sdd` of `spec-review`), voor een
   subagent-driven-development-run de huidige taak, het aantal taken en de stap (`build`, `review` of
   `final-review`, gelezen uit het plan en het ledger `progress.md` in de werkmap van de run; leeg als
   die niet te lezen zijn), de trigger, het profiel, de workspace, `started_at`,
   de forge-stand die de precheck mat (`working`, `waiting` of `done`; leeg als tick hem niet kon
   meten, voor een run die deze tick startte en voor een geparkeerde run) en de
   tijd van de tick. Een issue met een open én een geparkeerde run staat er één keer, als `open`.
   Mislukt de precheck, dan blijven de rijen van de vorige tick staan. `serve` leest ze voor
   `GET /api/runs` en de blokken *Running runs*, *Parked runs*, *Queue* en *Night*, zonder de forge (zie [install.md](install.md)).
7. **Gather.** Is de repo aan de beurt, dan leest tick de comments van elk item en bouwt de snapshot:
   de items in Controller of On hold, plus elk item met een open run in een andere status, met hun velden, voorgangers en PR's (de PR's die het issue sluiten
   plus de PR's op de branch van de laatste run van het issue, ook als die run al dicht is, met hun
   checks), hun comments en de open runs. Van een mergeklare PR bij een open run leest tick ook de
   PR-comments, voor de roborev-stand (`""`, `processed` of `open`). Zo blijft een gemergde PR die het issue niet sluit zichtbaar.
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

**De merge.** Regel 7 voert apply uit als een squash-merge (`merge_method: squash`) met de gemeten
head-SHA, zonder commit-titel en zonder branch-opruiming. Gaat de merge door, dan volgt de 🤖 Klaar met
`note_merged`. Drie soorten fout:

- 409 of "Base branch was modified": de head verschoof. Apply schrijft een `deferred`-regel en doet niets;
  de volgende tick meet opnieuw.
- Een weigering van GitHub (een verplichte review, een branch-bescherming): apply noteert de fout met de
  head-SHA op de open run (`MergeSHA`, `MergeError`). De volgende tick kiest fix-pr (regel 13, oorzaak
  *merge mislukt*), en is de sleutel gebruikt, dan gaat het issue terug.
- Een andere fout (netwerk, 5xx): een mislukte actie, dus exitcode 1.

**De workflow-push.** Raakt een branch `.github/workflows/`, dan pusht een run niet: GitHub weigert dat
met het token van tick, en een run krijgt nooit een token met `workflow`-scope
(`workflow-push-gate` blokkeert zo'n `git push` in een controller-run). De run draait roborev over de
ongepushte range en post een 🤖 Handoff met `next: push-workflows` (en, zolang er nog geen PR is,
`after: /lighthouse-write-pr <nr>` en de `roborev:`-regel van `lighthouse roborev-gate body-line
--push`). `Judge` kent `push-workflows` als eigen verdict; het staat niet in de lijst van
slash-commando's.

- **De route.** Decide maakt van die handoff een `push`-actie onder dezelfde voorwaarden als een gewone
  handoff-start: de trigger is nog niet gebruikt, geen actieve blokkade en de rem is open (zie hieronder).
  Daarbovenop moet `[dispatch.workflow_push]` bestaan, de run die de handoff schreef op deze host
  gedraaid hebben en de `after` door `Judge` komen. Anders post tick een 🤖 Teruggegeven met
  *workflow-pusher staat uit: [dispatch.workflow_push] ontbreekt in host.toml* of *de run die deze
  handoff schreef draaide op een andere host dan de pusher*, of een handoff zonder geldige `after` met
  *handoff onleesbaar*: nooit een stille skip.
- **De start.** Apply claimt een rij in `runs` (command `push-workflows`, zonder sessie, zonder
  werkplek, met de trigger-sleutel van de handoff), zet 🚀 op de handoff en start
  `lighthouse push-workflows` losgekoppeld met `setsid`, met een opgeschoonde omgeving: `PATH`, `HOME`,
  `XDG_RUNTIME_DIR` en het forge-token (als `GH_TOKEN`, om te lezen en te posten), en het pad naar
  database, pauzevlag, kloon, state-map en `host.toml` als vlag. Het workflow-token staat nooit in de
  omgeving van tick, van een run of van een reviewer; alleen het ene `git push` krijgt het, als
  `http.extraHeader` in de omgeving van dat commando, nooit in argv. De stdout en stderr van de pusher
  gaan naar `/dev/null`: de uitkomst staat in zijn comment en op zijn rij, niet in een logbestand.
  Alleen tick start een pusher; `lighthouse run` doet dat niet (zie *`lighthouse run`*).
- **De vloer.** In deze volgorde, en de eerste rode stap stopt alles: de worktree is die van de run die de
  handoff schreef en staat op `branch` (nooit de default branch), schoon, een fast-forward van
  `origin/<branch>`, alleen commits van deze branch, en een roborev-record dat precies de gepushte SHA
  dekt; dan de rem niet op pauze; dan de policy op de workflow-diff (actions op een SHA van 40 hex,
  een expliciet `permissions`-blok, geen `pull_request_target` of `workflow_run`) en `actionlint` en
  `zizmor`, gemeten tegen de merge-base zodat een bestaande bevinding niet elke wijziging blokkeert.
  De pusher werkt in een eigen bare kloon (`<state>/push/<owner>/<name>.git`) onder een `flock`
  en leest de worktree van de run alleen met `rev-parse` en `status`, met een eigen git-dir. Ontbreekt
  `bwrap`, `actionlint` of `zizmor` op `PATH`, dan is de stap rood met `COULD NOT CHECK`, nooit groen.
- **De review.** Workflows die alleen `schedule` hebben, of `push` op precies de default branch, zijn
  post-merge en gaan na de vloer zonder review. Al het andere is pre-merge (ook `workflow_dispatch` en
  `workflow_call`) en gaat eerst langs de reviewers van `[dispatch.workflow_push]`. Met `review =
  "shadow"` (de standaard) draaien ze en staat hun oordeel in de Vraag, maar beslissen ze niet: ook bij
  unaniem `approve` komt de Vraag. Met `"enforce"` pusht de pusher alleen als elke reviewer `approve` geeft;
  een `reject`, ongeldige JSON, een time-out of een exit ≠ 0 telt als `reject`. Minder dan twee
  reviewers met een eigen onderwerp keurt nooit goed.
- **De uitkomst.** Op de bouwroute (nog geen PR) post de pusher een 🤖 Handoff zonder sessie met
  `next: <after>`, `range: <oud>..<nieuw>` en `workflows: <bestanden>`, en de rij sluit als `handoff`:
  de keten gaat verder met `/lighthouse-write-pr`. Op de fix-pr-route (de PR bestaat) post hij een
  🤖-comment met `branch`, `range` en `workflows`, en de rij blijft open als eigenaar van de PR (zie
  *Afloop van een push-rij*). Rood is een 🤖 Vraag, en de rij sluit als `question`.
- **De Vraag.** De kop is `🤖 Vraag · push-workflows`, met de velden `sha` (de SHA waarvoor een antwoord
  toestemming geeft, `none` zolang geen review die SHA liet zien), `worktree`, `branch`, `after` en
  `step`, en geen sessie-voet. De tekst heeft de rode stap met zijn reden, het oordeel per reviewer,
  de workflow-diff in een codeblok (een hek langer dan elke reeks backticks in de diff) en
  `cd <worktree> && git push -u origin HEAD`. Is de diff te lang voor een comment (65535 tekens),
  dan staat de volledige diff als bijlage in `<state>/push/<owner>/<name>/<sha>-<run>.diff` (mode
  `0600`), niet in de worktree: die is van de run. Een Vraag adviseert nooit een token-scope te
  verruimen; weigert GitHub het token, dan zegt de Vraag alleen dat de forge het token voor deze wijziging
  weigerde.
- **Het antwoord `push` is toestemming.** Voor precies de `sha` uit de laatste Vraag van de pusher. Tick
  start de pusher opnieuw met `--permit <sha>`; hij draait de vloer opnieuw, en alleen als de SHA
  niet verschoof gaat hij zonder review door, zoals bij een geslaagde push. Toestemming heft alleen de
  review op (een `reject` of shadow mode), nooit een rode vloer, policy of lint. Een Vraag met `sha: none`,
  of een Vraag waarna de worktree verschoof, geeft geen toestemming: een antwoord `push` start dan niets
  of geeft dezelfde Vraag opnieuw. Elk ander antwoord hervat niets, want er is geen sessie. Bekende
  grens: een run post met hetzelfde account als de gebruiker en kan dus `push` antwoorden; dan valt de
  review weg en blijft de vloer staan. De roborev-records zijn door de run beheerd: check 5 is hygiëne
  tegen een vergeten of verouderde review, geen vertrouwensgrens. GitHub's eigen scope-controle blijft
  de grens.
- **De merge door de pusher.** Of GitHub een API-merge van een PR die `.github/workflows/` wijzigt weigert
  voor een token zonder `workflow`-scope, is niet geverifieerd (taak 1 van het plan zonder
  workflow-credential overgeslagen). Daarom bestaat de route, en gebruikt tick hem alleen als er een
  pusher-comment bestaat voor de head-branch van de PR: dan start regel 7 niet de merge van tick maar
  `lighthouse push-workflows --merge --pr <n> --sha <head>`. Zonder zo'n comment merget tick zelf, zoals
  voorheen. De pusher leest de gepushte SHA uit zijn eigen rijen in de database (nooit uit een
  comment), eist dat die een voorouder is van de PR-head en dat `gepusht..head` geen pad onder
  `.github/workflows/` raakt (op de bouwroute volgt na de push nog een changelog-fragment), en merget
  dan met het workflow-token. Hij post een 🤖 Klaar zonder sessie met `note_merged` en sluit zijn rij
  als `done`. Weigert hij, dan post hij een Vraag met `gh pr merge <n> --squash --match-head-commit
  <head>`. Is `[dispatch.workflow_push]` weg, dan gaat het issue terug met *workflow-pusher staat uit*; is de
  sleutel `merge:<pr>:<head>` al gebruikt, dan met *merge mislukt*.

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
een forge-stand `done` zonder werk na de Stop in het transcript: geen lopend achtergrondwerk
(`Background` is 0) en geen beurt na de laatste Stop (`TurnOpen` is onwaar), zie
[workplace.md](workplace.md#transcripten). Forge zet een sessie op `done` bij de Stop-hook, ook
als claude haar beurt beëindigt om op een Bash-taak met `run_in_background` of een async subagent
te wachten, en houdt die stand terwijl claude een nieuwe beurt neemt op de melding van dat werk.
Zo'n sessie is niet klaar en valt onder de stilte- en looptijdregels 17 en 19 (#270).
"Activiteit" is de laatste gebeurtenis in het transcript (alle draden), en
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
| 7 | Klaar en een groene eigen PR (niet draft, `MERGEABLE`, checks groen, roborev verwerkt of afwezig), de rem open (zie *De rem*) | squash-merge op de gemeten head, 🤖 Klaar met `note_merged`, de claude van de run beëindigen | |
| 7b | Klaar en een groene vreemde PR, geen open eigen PR | 🤖 Klaar zoals voorheen, de claude van de run beëindigen | |
| 8 | Werkplek verdwenen | Teruggegeven | werkplek verdwenen |
| 9 | `Unknown command:` in het transcript | rij `start_failed`, sessie stoppen als ze niet klaar is | |
| 10 | Klaar, maar nooit een assistent-beurt | rij `start_failed` | |
| 11 | PR van deze run gesloten zonder merge, en geen open PR | Teruggegeven, sessie stoppen als ze niet klaar is | PR gesloten |
| 12 | Open PR is draft | Teruggegeven, sessie stoppen als ze niet klaar is | PR is draft |
| 13 | Klaar en een eigen PR met een van vier oorzaken: rode checks, een merge-conflict, een onverwerkte roborev-review of een mislukte merge | fix-pr-start (sleutel `fixpr:<pr>:<sha>`, plafond 3 runs per PR); is het plafond bereikt, of is de sleutel al gebruikt, of is er een actieve blokkade, dan Teruggegeven; houdt de rem de start tegen, dan blijft de run open (zie hieronder) | per oorzaak *PR rood*, *PR heeft een merge-conflict*, *PR heeft een onverwerkte roborev-review* of *merge mislukt*; bij het plafond met ` na 3 fix-pr-runs` erachter (*onverwerkte roborev-review na 3 fix-pr-runs*) |
| 13b | Klaar en alleen een vreemde PR die rood is of een merge-conflict heeft (geen eigen open PR met een oorzaak) | Teruggegeven, sessie stoppen als ze niet klaar is | *PR rood* resp. *PR heeft een merge-conflict* |
| 14 | Klaar, geen PR, geen Handoff of Vraag | Teruggegeven | geen handoff of vraag |
| 15 | `max_wait` > 0, run gemeten maar zonder sessie, ouder dan `max_wait` | Teruggegeven, launch vergeten | start onzeker: geen sessie gevonden |
| 16 | `max_wait` > 0, permissie-prompt van deze run (niet van vóór `started_at`) waarna zijn draad `max_wait` lang niets deed | Teruggegeven, sessie blijft staan | wacht op permissie (met tool en pad) |
| 17 | `max_wait` > 0, niet klaar, geen openstaande permissie-prompt, geen activiteit in `max_wait` (geteld vanaf `started_at` op zijn vroegst) | Teruggegeven, sessie stoppen | geen activiteit in max_wait |
| 18 | Idem, maar zonder transcript: stand `waiting` en de forge-sessie `max_wait` oud, ook geteld vanaf `started_at` op zijn vroegst (zonder forge-tijd niet) | Teruggegeven, sessie stoppen | geen activiteit in max_wait (forge-stand; transcript niet meetbaar) |
| 19 | `max_runtime` > 0, run ouder dan `max_runtime` | Teruggegeven, sessie stoppen | time-out (max_runtime) |

Regel 13: houdt de rem de fix-pr-start tegen (uitstel of weigering), dan vervalt de afsluiting van de run: ze blijft open en de volgende tick vindt dezelfde oorzaak opnieuw. Alleen een actieve blokkade (de issue-blokkade van de start), een al gebruikte sleutel of het plafond geeft Teruggegeven.

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
`question` (regels 1-3, 6, 6b, 7 (merge), 7b en de fix-pr-handoff van regel 13) draagt daarom `End`: apply
beëindigt met `Kill` alleen de claude van die sessie-id, nooit de unit, zodat een interactieve sessie in
dezelfde worktree doordraait. Een run zonder sessie-id slaat apply over. Mislukt `Kill`, dan blijft de
rij open en probeert de volgende tick het opnieuw; een 🤖 Klaar staat er dan al, en regel 1 sluit. Een
start of hervatting doet na de claim nog een `Kill`, vóór de 🤖 Gestart of Hervat en de launch: op
elke sessie in de workspace van een gesloten eigen run, in welke forge-stand ook, en bij een
hervatting ook op haar eigen sessie. Ook een geparkeerde run, een eerdere mislukte `Kill` of een run
die met *wacht op permissie* (16) is teruggegeven laat zo geen claude achter die de handoff blokkeert
(#263). Mislukt die `Kill`, dan sluit de run als `start_failed` (*end earlier claude: …*), zonder post.

### Afloop van een push-rij

Een push-rij (command `push-workflows`) heeft geen werkplek en geen transcript. De regels die daarop
meten (8-10 en 14-19) gelden er niet voor; de pusher sluit zijn eigen rij (`handoff`, `question` of
`done`). Ze telt niet mee voor `slots` en `max_software` en houdt geen geheugenreserve vast: er draait
geen sessie, ook niet terwijl de rij op CI wacht.

- **Pusher stil.** Staat de rij na `[dispatch.workflow_push] timeout` (standaard 30 minuten) nog open
  zonder gepushte SHA, dan komt een 🤖 Teruggegeven met *pusher reageerde niet*.
- **Gepusht (fix-pr-route).** Zodra de pusher de gepushte SHA op de rij schreef, is de rij eigenaar van de
  PR en gelden de regels 6, 7 en 13 alsof het een Klaar-run met een eigen PR is: gemerged wordt `done`,
  groen wordt gemerged (zie *De merge door de pusher* hierboven), rood wordt een fix-pr-start. Een
  workflow-fix kost zo geen extra fix-pr-run bovenop het plafond van 3.
- **Afwijkingen van de spec, bewust.** Een gepushte rij zonder PR, op een issue dat open blijft, gaat
  terug na dezelfde timeout met *de pusher pushte, maar er kwam geen handoff of PR*. Een gepushte rij
  sluit ook als haar PR gesloten wordt zonder merge of als het issue met de hand gesloten wordt (6b); de
  spec noemt alleen 6, 7 en 13, maar dan bleef het issue voor altijd ongestart. En de merge door de
  pusher leest de gepushte SHA uit de eigen rijen van de pusher, niet uit zijn comment.
- **Start mislukt.** Sluit de rij met `start_failed` (de pusher kon niet starten), dan krijgt ze geen
  sessie-retry: de eerstvolgende tick start de pusher opnieuw, en na de tweede mislukking komt de Vraag
  of Teruggegeven.

### Afloop met de lokale werkplek

Decide verandert niet, maar `claude -p` loopt anders dan een interactieve claude. Bij
`workplace = "local"` vuren sommige regels anders:

| Regel | Bij de lokale adapter |
|---|---|
| 1-3, 6, 6b, 7, 7b (`End`, `Kill`) | Claude is meestal al weg; `Kill` ruimt op wat hij achterliet. |
| 9 `Unknown command:` | Vuurt niet. `-p` antwoordt op een onbekend commando met gewone tekst. De run eindigt `done` zonder PR of handoff, en regel 14 geeft *geen handoff of vraag*. |
| 10 nooit een assistent-beurt | Vangt een claude die direct na de start sterft. |
| 16 permissie-prompt | Vuurt niet. In `-p` wordt een permissie geweigerd in plaats van gevraagd; de hook `permission-check` schrijft niets. Een run die daarna vastloopt, valt onder 17. |
| 18 zonder transcript | Vuurt niet: de stand is nooit `waiting`. Een hangend proces zonder transcript valt onder 19 (`max_runtime`). |
| Kill vóór resume (#206) | Blijft; vindt meestal niets. |

Stoppen en opruimen gaan op het merk `LIGHTHOUSE_CONTROLLER_RUN` van de run, niet op de unit: `Remove`
beëindigt ook wat een klare claude achterliet, zoals een dev-server (zie [workplace.md](workplace.md#lokaal)).

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

`run` start nooit een pusher: ook een handoff `next: push-workflows` laat het aan de tick, net als het
parkeren en het terugzetten.

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

- Een run op een node (zie *Nodes* hieronder) kent een eigen rem: geen geheugentoets (het geheugen is
  niet van deze host) en de plekken van die node in plaats van `slots`. De pauze, usage en software
  gelden voor elke run. Uitstelteksten: `slots: 2 of 2 in use on <naam>` en, als het profiel zijn node
  uit `host.toml` kwijt is terwijl er nog runs op staan, `slots: node <node_id> not configured`. Een
  geparkeerde hervatting op een node telt mee maar wordt niet door de plekkenrem tegengehouden.
- De rem houdt alleen starts en hervattingen tegen. De afloop van open runs (sluiten, Teruggegeven,
  stoppen, 🤖 Klaar, zie *Afloop*) gaat door, ook tijdens een pauze of bij een dichte usage-rem.
  Twee uitzonderingen. De eerste is de merge van regel 7. Een pauze, een item buiten `Controller`
  en een actieve blokkeerder houden de merge tegen (net als `UNKNOWN` of checks `PENDING`), en de
  run blijft dan open. Alleen de time-out (regel 19, bij `max_runtime` > 0) geeft hem dan nog
  terug, met *time-out (max_runtime)*. De tweede is de fix-pr-handoff van regel 13. Stelt de rem
  de fix-pr-start uit, of houdt de pauze hem tegen, dan sluit de run niet. Hij blijft open tot de
  start doorgaat, in de tick waarin de rem opengaat.
- Een push (handoff `push-workflows`, het antwoord `push`, of de merge door de pusher) toetst alleen
  Pauze en Usage, net als een gewone handoff-start: Geheugen, Plekken en Software slaat hij over.
  Is de rem dicht, dan blijft de handoff of de merge-rij open en probeert de volgende tick het opnieuw.
  De pusher zelf controleert de pauzevlag ook, als stap 6 van zijn vloer.
- `n` bij Geheugen is het aantal starts en hervattingen dat deze tick al door de rem kwam: een net
  gestarte run reserveert van dezelfde meting.
- `in_use` komt uit de database en geldt voor de hele host: de open runs over alle repo's, plus de
  geparkeerde runs (een marker met `controller: true` waarvan de laatste run van de sessie nog
  hervatbaar is). `software_in_use` telt daarvan de runs met profiel `software`.
- Een run die in deze tick sluit, geeft zijn plek al aan de starts van deze tick: een plek van de
  host, op een node de plek van die node, en bij profiel `software` ook zijn software-plek. Drie
  sluitingen geven niets vrij. Een geparkeerde run blijft in `in_use` tot zijn usage-hervatting.
  De fix-pr-handoff van regel 13 geeft niets vrij voor een ander: de fix-pr-start erft de plek
  (zie hieronder). Een merge
  (regel 7) laat de run open als GitHub hem weigert; zijn plek komt pas de tick erna vrij. Mislukt
  de stop van een sessie, dan draait er één tick een run boven `slots` of `max_software`. De
  volgende tick telt beide, laat geen start door en stopt de sessie alsnog (na een Teruggegeven via
  regel 4).
- Een usage-hervatting (de geparkeerde run na de reset, trigger `usage`) slaat Plekken en Software
  over en telt er niet in mee: de run zit al in `in_use`. Hij telt wel mee in `n` van Geheugen.
- Hetzelfde geldt voor de fix-pr-start van regel 13, als hij op de node en met het profiel van de
  run komt die in dezelfde tick sluit: die run zit al in `in_use` (of in de plekken van zijn
  node). Telde hij als nieuwe run, dan hielden open runs die op een rode PR wachten bij volle
  plekken elkaars plek vast: geen start, dus geen afsluiting (#334). Plaatst de keuzeregel de
  start op een andere node, of met een ander profiel (software of niet), dan telt hij als nieuwe
  run: de plek van de sluitende run komt daar niet vrij.
- Een marker met `pause: true` hervat zodra er geen pauze meer geldt, ook vóór zijn `reset_at`; de
  toets `reset_at <= nu` slaat tick bij hem over.
- Een marker die nooit meer hervat (de laatste run sloot als Handoff, Vraag of Klaar, de herkansing
  is op, de laatste run sloot als `parked` maar de sleutel van de marker is al gebruikt, of het
  issue is gesloten of uit Controller en On hold) ruimt tick op, anders bleef hij in
  `in_use` tellen. Een marker van een repo die deze tick niet gathert, blijft staan.
- De uitgestelde starts staan met hun titel en reden in de tickstand (`tick_state`, soort
  `deferred`); de blokken *Queue* (elke reden behalve `night`) en *Night* tonen ze. Bij een
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
`GET /api/runs` en in het blok *Parked runs* (zie [install.md](install.md#running-runs)).

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

## De tick onder systemd of cron

Met de lokale werkplek start de tick de runs zelf, losgekoppeld, en laat ze doorlopen als hij klaar is.
Draait de tick in een systemd-service met de standaard `KillMode=control-group`, dan doodt systemd na
afloop elk proces in de cgroup van de unit, en `setsid` ontsnapt daar niet aan: elke run zou met de tick
sterven. De tick-unit [`lighthouse-tick.service`](../deploy/systemd/lighthouse-tick.service) (met de
[timer](../deploy/systemd/lighthouse-tick.timer), elke 3 minuten) staat daarom op `KillMode=process`: alleen
de tick zelf wordt gestopt. Ze heeft ook geen hardening (`ProtectHome`, `ProtectSystem`,
`NoNewPrivileges`), want die zou voor elke run gelden. Een operator ziet bij elke tick terwijl er runs
leven in de journal `Found left-over process … in control group while starting unit. Ignoring.`: dat is
verwacht, de runs van een eerdere tick leven nog in dezelfde cgroup.

Voor `XDG_RUNTIME_DIR` (tick stopt zonder), `HOME`, `GH_TOKEN` en een `PATH` met `claude` zorgt de unit zelf;
de installatie staat in [install.md](install.md). Onder cron zet de regel `XDG_RUNTIME_DIR` zelf en leest
hij `forge.env`:

```text
*/5 * * * * export XDG_RUNTIME_DIR=/run/user/$(id -u) PATH="$HOME/.local/bin:$HOME/go/bin:/usr/local/bin:/usr/local/go/bin:/usr/bin:/bin"; set -a; . "$HOME/.config/lighthouse/forge.env"; set +a; lighthouse tick
```

## `[dispatch]` in `host.toml`

Elke sleutel is optioneel; dit zijn de standaarden.

```toml
[dispatch]
workplace = "kenn-forge"     # of "local"; met local geen [dispatch.forge] en geen target_key
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
url = "http://127.0.0.1:8091"  # waar lighthouse de forge-API aanspreekt
web_url = ""                   # link naar het forge-dashboard op de webpagina, bv. een tailscale-serve-adres; leeg: url
token_file = "/home/me/.kenn/forge/auth_token"   # standaard: <home>/.kenn/forge/auth_token
unit_prefix = "paia-forge-"    # prefix van de systemd-unit per run; verplicht met kenn-forge, geen standaard
```

`unit_prefix` is letterlijk (met slotstreepje); zonder de sleutel faalt elke `Stop` met een fout die
`dispatch.forge.unit_prefix` noemt. Met `workplace = "local"` leest tick `[dispatch.forge]` niet en is `target_key` onder
`[dispatch.profiles.<naam>]` niet nodig; `mcp_config` van het profiel geldt wel. Een andere waarde dan
`kenn-forge` of `local` stopt tick met een fout die de sleutel noemt. Zie *Lokaal* in
[workplace.md](workplace.md#lokaal), *Afloop met de lokale werkplek* hierboven en, voor de tick als systemd-unit of cron-taak,
*De tick onder systemd of cron* hieronder.

### `[dispatch.workflow_push]`

Zonder deze tabel is de route uit: elke handoff `next: push-workflows` geeft een 🤖 Teruggegeven.

```toml
[dispatch.workflow_push]
token_file = "~/.kenn/workflow-push.token"   # verplicht; mode precies 0600; zie install.md
review     = "shadow"                         # shadow (standaard) | enforce
timeout    = "30m"                            # standaard; de pusher gebruikt 0,9 x timeout

[[dispatch.workflow_push.reviewers]]
topic   = "supply-chain"            # supply-chain | secrets | injection; elk onderwerp één keer
command = ["codex", "exec", "--sandbox", "read-only", "--output-last-message", "{out}", "-"]
binds   = ["~/.codex/auth.json"]    # alleen-lezen paden; nooit onder ~/.config/lighthouse
env     = []                        # namen van variabelen die doorgaan, nooit GH_TOKEN of GITHUB_TOKEN

[[dispatch.workflow_push.reviewers]]
topic   = "injection"
command = ["claude", "-p", "--tools", ""]
```

- `token_file` krijgt `~` uitgebreid. Het bestand moet een gewoon bestand zijn (geen symlink) met mode
  precies `0600` (`0400` wordt geweigerd), met één token. De pusher leest het pas na een groene vloer en
  review.
- `reviewers`: nul, of twee of drie met elk een eigen `topic`. Eén reviewer is een fout bij het laden van
  `host.toml`. Zonder reviewers gaat een post-merge-wijziging door en vraagt een pre-merge-wijziging
  altijd. De prompts zitten in de lighthouse-binary (`embed`), nooit in de branch.
- `command` krijgt de prompt en de diff op stdin. `{out}` wordt het pad van een uitvoerbestand; staat het er
  niet in, dan is stdout de uitvoer. Het oordeel is één JSON-object `{"verdict":
  "approve"|"reject", "findings": [{"file","line","issue","evidence"}]}` uit dat bestand of dat laatste
  bericht, niet uit een eventstroom.
- `claude -p --tools ""` is de tool-loze vorm (bevestigd aan de hand van `claude --help`), zonder `{out}`:
  stdout is de uitvoer. De vlaggen van `codex exec` zijn **niet geverifieerd**: `codex` staat niet op de
  ontwikkelmachine. Het codex-voorbeeld is het voorbeeld uit de spec, geen geteste configuratie; controleer
  de vlaggen met `codex exec --help` voor je erop vertrouwt.
- Elke reviewer draait onder `bwrap` met een lege tijdelijke map als enige schrijfbare bind, de binary en
  zijn bibliotheken alleen-lezen, en alleen wat `binds` en `env` toestaan. De pusher dwingt dat zelf af en
  vertrouwt de vlaggen van het commando niet. Een bind mag het tokenbestand, de forge-tokenmap, `~/.config/lighthouse`, `~/.config/gh`,
  `~/.kenn/forge`, de state-map van de pusher of de kloon niet raken, en moet bestaan: anders weigert
  de pusher de review.
- **Het netwerk blijft aan** (`--share-net`): een reviewer is een model-CLI en moet zijn API bereiken.
  Dat deelt ook abstracte Unix-sockets met de host. Een reviewer met tools kan zijn eigen gebonden
  authenticatiebestanden en `env`-waarden lezen en over het netwerk versturen. Daarom staat er in de
  sandbox alleen de diff en de alleen-lezen paden uit `binds`, nooit een token, en daarom zijn
  tool-loze reviewers (`--tools ""`) de bedoeling. De pusher scrubt de waarden van het forge-token, het
  workflow-token en elke `env`-waarde van 8 tekens of meer uit alle reviewer-tekst voor die in een
  comment komt.
- `actionlint` en `zizmor` zijn niet getest tegen een echte repo: hun JSON- en exitcodeformaten zijn
  gecontroleerd tegen nep-uitvoer. Draai de pusher één keer met de hand op een echte repo voor je erop
  vertrouwt.

**Tijdsbudgetten.** Van `timeout` gaat een kwart naar het wachten op de `flock` van de kloon (7 minuten 30
bij 30 minuten) en de helft naar elke reviewer (15 minuten; de reviewers draaien parallel). Het laatste
kwart is de marge voor vloer, policy, lint en push. Het geheel rekent op 0,9 x `timeout`, zodat de
pusher zijn eigen Vraag post vóór tick de rij teruggeeft met *pusher reageerde niet*.

### Nodes

Met `[dispatch.nodes.<naam>]` draait een run op een andere kenn-forge-node dan die van `[dispatch.forge]`.
Die eigen node heet `self` (in code en database de lege string); de naam `self` is gereserveerd.

```toml
[dispatch.nodes.mac]
node_id = "node-a"   # het node-id op de fleet-hub; verplicht, uniek, niet "self"
slots = 2            # runs tegelijk op deze node; verplicht, minstens 1

[dispatch.profiles.mac-software]
node = "mac"         # naam van een [dispatch.nodes]-tabel; leeg: self
```

Zonder `[dispatch.nodes]` is er niets anders dan voorheen. Een `node` die geen tabel heeft, is een
fout bij het laden van `host.toml`. De hub (forge v0.7.0 of later) bereikt de node via de fleet-routes
`/api/v1/fleet/hosts/<node_id>/...`; zie *kenn-forge* in [workplace.md](workplace.md#een-run-op-een-andere-node).

**Keuzeregel.** Een nieuwe start kiest locatielabel, dan profiel, dan node:

1. De repo heeft `[labels] location_prefix` én `[controller.locations]`, en het issue draagt precies
   één label met die prefix: de waarde erna (exact, hoofdlettergevoelig) wordt opgezocht in
   `[controller.locations]` van `lighthouse.toml` (`<waarde> = "<profiel>"`). Dat profiel is het
   profiel van de run.
2. Zonder locatielabel, zonder `location_prefix` of zonder `[controller.locations]`, is het profiel
   `[controller] profile`.
3. De node is de `node` van dat profiel in `host.toml`; geen `node` is `self`.

Een hervatting en een retry kiezen niet opnieuw: ze erven profiel en node van de rij van hun eerdere run
(de hervatting van de rij met hetzelfde `session_id`, de retry van de rij met dezelfde trigger). Een label
dat tijdens een run wijzigt, verplaatst die run dus niet. Een locatielabel dat niet in
`[controller.locations]` staat, en meer dan één locatielabel, stellen de start uit, met
`locatie <waarde> onbekend` en `meer dan één locatielabel`. `[controller.locations]` zonder
`[controller]` is een fout.

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

Optioneel, voor een run op een andere node (zie *Nodes*): `[labels] location_prefix` (bv. `"loc:"`) en

```toml
[controller.locations]
mac = "mac-software"   # waarde na de prefix = profiel uit [dispatch.profiles] van host.toml
```

Een lege profielnaam is een fout; de tabel vereist `[controller]`. Een repo zonder `location_prefix`
gedraagt zich als voorheen, en een repo met `location_prefix` maar zonder `[controller.locations]` ook:
hij negeert locatielabels, ook twee tegelijk. Een upgrade is daarom veilig. Pas de tabel maakt het
label bindend: een onbekende waarde of twee labels stellen de start dan uit.

## Zicht

Wat de tick gestart heeft, zie je terug in `GET /api/runs` en in de blokken *Running runs*, *Parked runs*, *Queue* en *Night* van
`lighthouse serve`: de open en geparkeerde runs en de uitgestelde starts, uit de tickstand. Zie
[install.md](install.md#running-runs) voor het gebruik.
