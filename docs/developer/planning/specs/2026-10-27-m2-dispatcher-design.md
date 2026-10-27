# Ontwerp: M2 — de dispatcher in Go

Milestone: [M2 — Dispatcher](https://github.com/CorneeldH/lighthouse/milestone/3) · Datum:
2026-10-27 · Bugs: #31, #34, #35, #56, #85 (deel 1) · Bouwt op M1: `internal/store`,
`internal/forge`, `internal/overview`, `internal/usage` (#64, #65) en de UI van #69.

## Aanleiding en doel

De controller van personal_ai_agent (paia) draait als Python en shell: ±11.600 regels in
`src/controller/` en `scripts/controller_*`. M2 brengt de dispatcher naar de lighthouse-binary.
**Het is geen port.** Besloten in het gesprek: we bouwen in delen opnieuw op wat we nu weten en
willen, en laten weg wat de forge-werkplek overbodig maakt of wat later kan. De Python dient als
naslag voor gedrag en randgevallen, niet als blauwdruk.

Na M2 roept een timer `lighthouse tick` aan. Die bedient meerdere repo's, start runs als
kenn-forge-workspace, volgt ze via het `🤖`-protocol op het issue, en de UI toont de lopende
runs.

## Vaste punten (uit de pitch, niet heropend)

- **De claim is de open runrij.** `CREATE UNIQUE INDEX runs_open_issue ON runs(repo, issue)
  WHERE ended_at IS NULL`. Claimen is één `INSERT` in een `BEGIN IMMEDIATE`-transactie, vóór de
  launch. Een unique-violation betekent "al geclaimd": er start niets. `tick` en `run` gebruiken
  dezelfde claimfunctie.
- **Vrijgeven.** De afloop zet `ended_at`, en een mislukte launch ook. Een claim zonder levende
  werkplek sluit de afloop af.
- **Eén tick tegelijk:** een niet-blokkerende `flock` op `${XDG_RUNTIME_DIR}/lighthouse/tick.lock`.
  Is die bezet, dan meldt `tick` "vorige tick loopt nog" en stopt met exit 0. `run` neemt dit
  slot niet.
- **Database kwijt:** een open run zonder runstaat wordt teruggegeven, nooit dubbel gestart.
- **Werkplek:** een kenn-forge-workspace is de eerste adapter. Elke run krijgt
  `--strict-mcp-config` (geen `merge_pull_request` voor een run) en een bovengrens op de looptijd
  (8 uur).
- **No-Gos** van de pitch: lighthouse merget nooit, schrijft nooit zonder akkoord in
  `AGENTS.md`/`CLAUDE.md` van een projectrepo, geen tweede database, geen tweede harness.

## Scope

### In M2

1. **Runstaat en claim** in SQLite, met `tick` als eigen proces naast `serve` (*Aanpakken*).
2. **Meerdere repo's** vanuit `host.toml`, met één gedeelde rem op de host.
3. **De keten**: Handoff → start, Vraag → antwoord → Hervat, groene PR → Klaar, mislukking →
   Teruggegeven met een eerlijke reden. Plus `when: night` en de nacontrole met
   `/lighthouse-close-issue`.
4. **De rem**: usage-pauze en hervatten na de reset, de pauzevlag, geheugen, plekken.
5. **Blokkades**: een geblokkeerd uitvoerend issue naar On hold en terug (#56).
6. **De werkplek**: de kenn-forge-adapter, met opruimen (#31, #85 deel 1).
7. **Zicht**: `GET /api/runs` en de lopende runs in de UI; het overzicht laat issues buiten
   Controller weg (#85 deel 2).
8. **Overstap**: de timer roept `lighthouse tick` aan; de Python-controller gaat weg uit paia.

### Niet in M2 (besloten in het gesprek)

| Onderdeel | Waarom eruit | Waar het heen gaat |
|---|---|---|
| Headless-runner (`systemd-run claude -p`) en classify | Forge is de enige werkplek; een forge-run post zijn Vraag zelf | Weg |
| Stapelen en herbaseren | Wachten op de merge van de voorganger is goed genoeg | Weg; een geblokkeerd issue gaat naar On hold |
| Automatische fix-pr en roborev | Later | Nieuw lighthouse-issue, overgezet van CorneeldH/personal_ai_agent#1442 |
| Triage en `auto_aanbevolen` | Eigen functie, geen dispatch | Eén nieuw lighthouse-issue zonder milestone, met paia-milestone #67 als bewijsbron |
| Geplande runs (codebase-map, reflect) | Draaien nu als eigen timers, los van de controller | Nieuw lighthouse-issue zonder milestone |
| De wachter-timer (elke 3 min, vingerafdruk) | Een tick met een goedkope precheck doet hetzelfde | Weg (*De tick*) |
| `apply-fouten.json` | Workaround tegen een rood journal | Vervangen door *Herhaalde fouten* |
| Nederlandse legacy-sleutels en `/paia-`-aliassen | Bestaan alleen voor oude comments in paia | `internal/protocol` leest de legacy-sleutels al; de dispatcher schrijft alleen canonieke |

## Aanpakken

De keuze die de bouw bepaalt: wie opent de database?

1. **`tick` als eigen proces** — *gekozen*. De timer roept `lighthouse tick` aan; `run`, `tick`
   en `serve` openen dezelfde SQLite-database in WAL. Dit volgt de pitch letterlijk en de
   dispatcher draait ook zonder `serve`. Kost: `store` krijgt een tweede manier van openen en de
   migraties moeten veilig zijn bij twee processen.
2. **De tick binnen `serve`** (een POST van de timer, of een eigen ticker). Eén proces en geen
   slotprobleem, maar zonder `serve` start er niets, er is meteen een schrijvende endpoint met
   CSRF-bescherming nodig, en de pitch moet herschreven worden. Afgewezen.

Verder mijn keuzes, in het gesprek voorgelegd zonder bezwaar:

- **Gather → decide → apply.** Gather bouwt een snapshot (een Go-struct, als JSON te dumpen),
  decide is een pure functie van snapshot naar acties, apply voert de acties uit. Decide wordt
  getest met snapshot-fixtures, zoals `bundle.json` → `besluit.json` nu.
- **Lighthouse schrijft zelf naar GitHub**, via REST en GraphQL in Go, door `internal/forge` uit
  te breiden. Geen `gh` als subproces.
- **De werkplek is één smalle interface**, met kenn-forge als enige implementatie.

## Ontwerp

### 1. Database en processen

**Openen.** `store.Open` neemt nu altijd de exclusieve `flock` op `<db>.lock`, en die houdt een
tweede proces buiten. Dat slot wordt het **serve-slot**: alleen `serve` neemt het, met de
bestaande melding. `tick` en `run` openen de database met een tweede functie zonder dat slot
(naam aan de bouwer). Beide paden zetten dezelfde pragma's, met `busy_timeout` vóór
`journal_mode(WAL)`: anders faalt een gelijktijdige eerste open met `SQLITE_BUSY` vóór de
migratielock *(eigen keuze na spec-review, r1-f18)*. `lighthouse ask` opent de database niet
(*Launch*).

**Migraties bij twee processen.** De runner draait elke migratie in een transactie die direct de
schrijflock neemt (`BEGIN IMMEDIATE`; in `modernc.org/sqlite` via `_txlock=immediate` op de DSN
of een expliciete `BEGIN IMMEDIATE`). Binnen die transactie leest hij `user_version` opnieuw en
slaat hij een migratie over die een ander proces al deed. Een database die nieuwer is dan de
binary blijft geweigerd (`ErrSchemaTooNew`), ook voor `tick`.

**Schema** (`0002_runs.sql`; kolomnamen aan de bouwer, de semantiek ligt vast):

| Kolom | Betekenis |
|---|---|
| `id` | Eigen id van de rij |
| `repo`, `issue` | `owner/name` en issuenummer |
| `session_id` | De Claude-sessie. Lighthouse genereert hem bij een start zelf en schrijft hem in de claim-`INSERT`, vóór de spawn (`--session-id`). Een hervatting krijgt een nieuwe rij met dezelfde `session_id` |
| `mode` | `start` of `resume` |
| `trigger` | Waarom deze run: `handoff`, `answer`, `usage`, `closecheck`, `manual`, met de sleutel van de trigger (comment-id, marker) |
| `attempt` | De hoeveelste poging voor deze trigger (1 of 2; *Dedup*) |
| `command`, `prompt` | Het commando uit de Handoff en de volledige prompt |
| `branch`, `workspace_id`, `workplace_session`, `profile` | Werkplekgegevens |
| `started_at`, `ended_at` | `ended_at IS NULL` is de claim |
| `outcome`, `reason` | Zie *Afloop*; `reason` is de tekst die ook op het issue komt |

Plus de partiële unieke index uit de pitch. Eén rij is één launch: een Vraag sluit de rij, het
antwoord opent een nieuwe. Zo blijft de claim simpel en is de historie per poging gratis.

Een tweede tabel houdt de **herhaalde fouten** bij (*Herhaalde fouten*). Een derde houdt de
**tickstand** bij: de vingerafdruk per repo, de uitgestelde starts met hun reden, en de stand
van elke open run volgens de laatste tick. `serve` is een eigen proces en leest `GET /api/runs`
daaruit *(eigen keuze na spec-review, r1-f11)*. Meer tabellen komen er in M2 niet.

### 2. Configuratie

**`host.toml`**, nieuwe tabel `[dispatch]`:

| Sleutel | Standaard | Doel |
|---|---|---|
| `repos` | `[]` | Paden van de main checkouts. Elke repo levert zijn eigen `lighthouse.toml`; `[forge] repo` is de identiteit |
| `slots` | 4 | Maximum aantal lopende plus geparkeerde runs op de host, over alle repo's |
| `max_software` | 2 | Maximum aantal runs tegelijk in het Software-profiel (gedeelde devcontainer) |
| `min_mem_available_gb` | 8 | Rem: `MemAvailable − n × reserve ≥ min` |
| `reserve_per_run_gb` | 3 | Reserve per lopende run |
| `night` | `"21:00-08:00"` | Venster voor `when: night`, in de lokale tijdzone van de host |
| `max_runtime` | `"8h"` | Bovengrens per run |
| `max_wait` | `"1h"` | Grens voor een stille run (*Afloop*) |
| `answer_permissions` | `["admin", "maintain"]` | Wie een Vraag mag beantwoorden (*Antwoorden*) |

`[dispatch.forge]`: `url` (standaard `http://127.0.0.1:8091`) en `token_file` (standaard
`~/.kenn/forge/auth_token`). `[dispatch.profiles.<naam>]` per rechtenprofiel: `target_key` (het
forge-agent-target, nu `paia-software` en `paia-infra`) en `mcp_config` (*Launch*). De
usage-drempels blijven in `[usage]` van #64.

**`lighthouse.toml`** per repo:

- `[forge.board]` met de sleutels die `pack/core/lib/config.py` al kent (`project_id`,
  `project_number`, `status_field_id`, `status_options` met `Done`, plus `priority_field_id` en
  `priority_options` zoals in paia). Nog geen Go-code leest die tabel, en lighthouse zelf heeft
  hem niet: issue 1 bouwt de lezer en zet het blok van board *Lighthouse* (project 2, met alle
  vijf statussen) in `lighthouse.toml`. De dispatcher eist de opties `Controller` en `On hold`
  en het Priority-veld; hij zet zelf geen andere status (r1-f15). Ontbreekt er één, dan slaat de tick die repo
  over met een luide fout. *Mijn keuze.*
- `[controller]`, de tabel die de skills al lezen. `handoff`, `start-issue`, de
  review-skills en `config.py` (`needs_controller`) herkennen controller-modus aan
  `[controller]` en lezen `controller.bij_rondegrens`; de dispatcher volgt dat en verzint
  geen tweede tabel *(eigen keuze na spec-review, r1-f6)*. Erbij komen `profile` (standaard
  `software`), `spec_review` en `plan_review` (standaard `true`); `bij_rondegrens` blijft
  (de waarden zoals in paia, met een eigen ingebed instructiebestand). *Mijn keuze:* het profiel geldt
  per repo; een profiel per issue kan later.
- `[usage_guard] controller = true` (en `resume = true`). Zonder die vlag geeft de usage-guard
  een controller-run het gewone blok en schrijft hij geen `controller: true`-marker, zodat er
  niets te hervatten valt (`docs/usage-guard.md`, *Contract*) *(eigen keuze na spec-review,
  r1-f7)*. Lighthouse zet beide aan in zijn eigen `lighthouse.toml` zodra het zichzelf bedient,
  en `docs/usage-guard.md` verliest de zin "Lighthouse zelf zet ze niet aan".

`init` schrijft `[controller]` niet: een repo die er niet in `host.toml` staat, wordt niet
bediend. De tick slaat een repo zonder `[controller]` of zonder `[usage_guard] controller = true`
over met een luide fout.

### 3. De tick

`lighthouse tick` (exit 0 tenzij hij niet kan checken; dan exit 1 met de reden):

1. `flock` op `tick.lock` (vast punt).
2. **Precheck per repo.** Een goedkope vingerafdruk: de items in kolom Controller en On hold met
   hun `updatedAt`, de open runrijen, de resume-markers, de pauzevlag, de forge-sessiestand van
   de open runs, de PR-stand (state, draft, mergeable, checks) van de issues met een open run,
   en de stand van de `blocked_by`-voorgangers van de items in On hold. Gelijk aan de vorige en
   geen tijdgrens bereikt (8 uur, 1 uur, `reset_at`, begin van de nacht), dan slaat de tick die
   repo over. Is de laatste volledige gather ouder dan 30 minuten, dan gathert hij toch *(eigen
   keuze na spec-review, r1-f4)*. De vorige vingerafdruk staat in de tickstand. Wordt een actie
   uitgesteld, dan bewaart de tick de nieuwe vingerafdruk niet, zodat de volgende tick het
   opnieuw probeert.
3. **Gather** per repo die verder moet: de snapshot.
4. **Decide**: één functie over alle snapshots samen, want de rem is gedeeld. Uitkomst: een lijst
   acties.
5. **Apply**: de acties in volgorde. Een start claimt eerst; mislukt de launch, dan sluit hij de
   claim met `outcome = start_failed` (*Dedup* zegt of er een tweede poging komt). Een
   samengestelde actie (een 🤖-comment plus een board-wijziging) post altijd eerst de comment.
   De volgende tick leidt de gewenste boardstand af uit de laatste 🤖-comment en maakt een half
   gelukte overgang af *(eigen keuze na spec-review, r1-f5)*.

**Token.** `tick` en `run` lezen het GitHub-token zoals `serve` (`GH_TOKEN` of `GITHUB_TOKEN`);
de systemd-unit van de timer zet het via een `EnvironmentFile` (host-deploy). Het token moet
issues en comments schrijven en het Projects-v2-board van de eigenaar wijzigen (scope
`project`). Ontbreekt het, dan geeft `tick` exit 1 met die reden *(eigen keuze na spec-review,
r1-f19)*.

**De timer.** `controller-tick.timer` (host-deploy, buiten lighthouse) roept elke 3 minuten
`lighthouse tick` aan. Dat vervangt de wachter en de uurlijkse vangnet-tick. *Mijn keuze*: de
frequentie van de wachter, omdat de precheck goedkoop is.

**`lighthouse run <repo>#<issue>`**: dezelfde gather en decide voor dat ene issue, zonder
`tick.lock` en zonder precheck. Het respecteert de pauzevlag en de usage-rem, niet de
plekkenrem: een mens die er een start, beslist zelf. *Mijn keuze.* `run` voert alleen een start
of hervatting uit, achter de claim; elke andere actie (Teruggegeven, board, 🚀, Geparkeerd)
laat het aan de tick, zodat twee processen die nooit dubbel posten *(eigen keuze na
spec-review, r1-f22)*.

**Snapshot per repo** (wat gather leest):

- Board: items in Controller en On hold, met Status en Priority (GraphQL `projectV2`).
- Per issue: comments (met id, auteur, reacties), `blocked_by`-afhankelijkheden, en de PR die
  het issue sluit of op zijn branch staat (state, draft, mergeable, head-sha, checks).
- `git fetch origin main` in de main checkout en de sha van `origin/main`.
- De open runrijen en de resume-markers voor deze repo.
- De forge-workspaces en hun agent-sessies.
- Hostbreed, één keer per tick: `MemAvailable`, het usage-venster, de pauzevlag.

### 4. Decide: de regels

Volgorde per tick; een issue krijgt hoogstens één startactie.

1. **Open runs** (rij zonder `ended_at`): de afloop (*Afloop*).
2. **Wezen**: een issue waarvan de **laatste** 🤖-comment een Gestart of Hervat is, met een
   `session_id` (de footer) die in **geen enkele** rij van `runs` staat. Dat kan alleen als de
   database kwijt is, of bij de overstap (*Overstap*, die daarom eerst leegloopt). →
   Teruggegeven, reden "runstaat kwijt". Een geparkeerde, vragende of overgedragen run heeft wel
   een (dichte) rij en is dus geen wees. Nooit dubbel starten (vast punt).
3. **Usage-hervattingen**: een marker met `"controller": true` waarvan `reset_at` voorbij is
   → hervat met prompt `ga verder`. Telt niet als nieuwe plek, want de geparkeerde run bezette
   er al een.
4. **Antwoorden** (*Antwoorden*) → hervat met de vraag en het antwoord als prompt; post 🤖
   Hervat.
5. **Nacontrole**: een `Refs`-PR van het issue is gemerged, het issue is open en staat in
   Controller, en er stond een 🤖 Gestart vóór de merge → start `/lighthouse-close-issue N` in
   de workspace van het issue. Een `Closes`-PR sluit het issue bij de merge al; daar doet de
   tick niets (ingang C van close-issue is aan de mens) *(eigen keuze na spec-review, r1-f10)*.
   Het issue blijft in Controller tot die run klaar is, zodat een Vraag of Teruggegeven van die
   run in het overzicht komt (r1-f15). De run-instructies laten de run eerst
   `git fetch` doen en tegen `origin/main` toetsen. *Mijn keuze:* paia wacht tot `main` gelijk is
   aan `origin/main`, maar dat leunt op een auto-deploy die `main` doorspoelt. Lighthouse heeft
   die niet, en de workspace heeft de main checkout niet nodig. Daarmee vervallen ook de
   paia-checks "main verschoven zonder push" en "vuile main checkout".
6. **Handoffs**: een issue in Controller zonder 🤖-comment → `/lighthouse-start-issue N` op een
   nieuwe branch `issue-N-<slug>`. Een Handoff zonder 🚀 → het commando uit `next`.
   - Het commando moet op de allowlist staan, met het argument in de juiste vorm (zoals
     `beoordeel_commando` nu). De uitkomsten van paia blijven (*mijn keuze*):

     | Oordeel | Actie |
     |---|---|
     | Einde van de keten (`next` leeg of een eindcommando) | Niets posten; geen run |
     | Heeft brainstorm nodig | Teruggegeven "heeft brainstorm nodig"; blijft in Controller (r1-f15) |
     | `executing-plans` zonder `lane: native` | Teruggegeven "vraagt interactieve uitvoering"; blijft in Controller |
     | `lane: native` | Start met `--model sonnet` |
     | Skill niet in de worktree | Teruggegeven "skill niet geïnstalleerd in de worktree" |
     | Ander commando of verkeerd argument | Teruggegeven "commando niet toegestaan: …" |

   - `spec_review = false` of `plan_review = false` slaat de reviewstap over (`review_sprong`).
   - `when: night` wacht tot het venster `night` begint.
   - Spec-, plan- en reviewfasen starten ook bij een open blokkeerder. Voor een uitvoerende fase:
     *Blokkades*.
7. **Blokkades** (#56).
8. **Opruimen** (#31, #85).

**De rem**, getoetst vóór elke start of hervatting (behalve de usage-hervatting bij de
plekkenrem):

| Rem | Regel | Uitsteltekst |
|---|---|---|
| Pauzevlag | Geldige `PAUSE` → niets start; geen uitstel gemeld | — |
| Usage | Venster gemeten, sessie < `halt_pct_session` en week < `halt_pct_week`. "Gemeten" is een usage-cache die niet ouder is dan twee keer de TTL; de tick ververst hem met `Refresh(false)`, en `Busy` met zo'n verse cache telt als gemeten. `Meter.Tick` meet alleen terwijl er een claude-proces loopt, dus daar leunt de rem niet op *(eigen keuze na spec-review, r1-f14)*. Niet meetbaar → niets start | `usage` |
| Geheugen | `MemAvailable − n × reserve_per_run_gb ≥ min_mem_available_gb` | `geheugen` |
| Plekken | Open plus geparkeerde runs < `slots` | `plekken` |
| Software | Software-runs < `max_software` | `software` |
| Werkplek bezet | *Werkplek bezet* | `werkplek-bezet` |

**Volgorde over repo's heen**: Priority P0 → P2, dan de oudste trigger (de `created_at` van de
Handoff of het antwoord). *Mijn keuze*: issuenummers van verschillende repo's zijn niet
vergelijkbaar.

**Dedup.** Elke trigger heeft een sleutel (`handoff:<comment-id>`, `answer:<comment-id>`,
`usage:<marker>:<reset_at>`, `closecheck:<pr>`, `start:<issue>`). Een trigger is één logische
start; elke launch is een poging, met een eigen rij en `attempt` *(eigen keuze na spec-review,
r1-f1)*:

- Een trigger met een rij waarvan de uitkomst niet `start_failed` is, start nooit opnieuw.
- Een trigger met één `start_failed`-rij krijgt één tweede poging. Dat geldt voor een launch die
  forge niet aannam en voor een startfout uit *Afloop* (#34).
- Een tweede `start_failed` sluit de trigger: 🤖 Teruggegeven met de reden van die poging.
- Een onzekere spawn is geen `start_failed`: de rij blijft open tot de afloop beslist
  (*Launch*).

🚀 is weergave, geen dedup. Waar een regel naar 🚀 kijkt (een Handoff zonder 🚀, een antwoord
zonder 🚀), telt alleen een 🚀 van de login van het token; in een publieke repo kan iedereen een
reactie zetten *(eigen keuze na spec-review, r1-f8)*.

### 5. Antwoorden en vertrouwde auteurs

Een antwoord is de eerste comment na de nieuwste open 🤖 Vraag die:

- geen 🤖-kop heeft en geen 🚀 draagt;
- geschreven is door een auteur met een permissie in `answer_permissions`. De tick vraagt dat op
  via `GET /repos/{repo}/collaborators/{user}/permission`, leest het veld `role_name` (het veld
  `permission` kent geen `maintain`) en onthoudt het per tick *(eigen keuze na spec-review,
  r1-f9)*.

Een comment van een andere auteur telt niet als antwoord en krijgt geen 🚀. Het overzicht toont
de Vraag dan nog als open. Reden (besluit in het gesprek): het antwoord wordt letterlijk de
prompt van de hervatte run, en in een publieke repo is elke comment anders een open deur. Dit
neemt CorneeldH/personal_ai_agent#1524 mee.

**Dezelfde regel voor elke 🤖-comment** (*mijn keuze*, als verlenging van dat besluit). Ook een
Handoff (`next` wordt het commando), Vraag, Gestart, Hervat en Geparkeerd telt alleen als de
auteur in `answer_permissions` valt. Lighthouse is publiek, en een vreemde Handoff is een
grotere deur dan een vreemd antwoord. Een 🤖-comment van een andere auteur negeert de tick, en
hij logt één regel. Paia toetst nergens een auteur; dat ging goed omdat die repo privé is.

**Eigen comments.** Lighthouse post met het GitHub-token van de host. "Eigen" (voor Geparkeerd
bij *Blokkades*) betekent: de 🤖-kop klopt en de auteur is de login van dat token
(`GET /user`, één keer per tick). Gebruikt de host het token van de gebruiker zelf, dan zijn
eigen comments en die van de gebruiker niet uit elkaar te houden op auteur. Daarom telt ook
de kop.

Bij een hervatting zet apply 🚀 op het antwoord en post 🤖 Hervat.

### 6. Launch en de werkplek-interface

**De interface** (namen aan de bouwer) heeft precies wat decide en apply nodig hebben:

- *Prepare*: zoek of maak de workspace voor een issue en branch, en wacht tot hij klaar is.
- *Spawn*: start een agent met een launch-opdracht (hieronder), met de `session_id` die al in de
  claim staat, en geef de forge-sessie terug, of een uitkomst "niet aangenomen" of "onzeker".
- *Sessions*: alle sessies van een workspace met hun stand, met de Claude-`session_id` waar die
  te zien is.
- *Stop*: stop een sessie.
- *Remove*: verwijder een workspace, met "vuil" als eigen uitkomst.

**kenn-forge-adapter.** HTTP op `[dispatch.forge] url` met bearer-token, zonder proxy en zonder
redirects, zoals `src/controller/forge_api.py` nu: `GET /api/v1/workspaces`,
`POST /api/v1/repo/<kind>/<repo>/workspaces`, `GET …/{id}`, `GET …/{id}/agent-sessions`,
`POST …/{id}/runtime/agent-handoffs`, `DELETE …/{id}`, `GET /api/v1/settings`. De spawn-uitkomst
volgt de driedeling van `forge_api.py` *(eigen keuze na spec-review, r1-f17)*:

- **niet aangenomen**: een weigering of een 5xx. De rij sluit als `start_failed` (*Dedup*).
- **onzeker**: een time-out, een verbroken verbinding, of een antwoord zonder `session.key`. Dan
  nooit een tweede agent starten. De rij blijft open; de volgende ticks zoeken de sessie op
  workspace en `session_id`. Verschijnt hij niet binnen `max_wait`, dan sluit de rij als
  `returned`, reden "start onzeker: geen sessie gevonden" *(eigen keuze na spec-review,
  r1-f2)*.
- **aangenomen**: de forge-sessie gaat in de rij.

Forge heeft geen endpoint om een sessie te stoppen. *Stop* gaat daarom, zoals in `forge_ops.py`,
via `systemctl --user stop <unit>` en `podman exec … pkill`. Dat is een verplicht deel van de
adapter, met een test; zonder Stop dwingen de bovengrens en de stilte-regel niets af *(eigen
keuze na spec-review, r1-f12)*. De overige workarounds uit `forge_ops.py` (branch eerst pushen,
`_volg_remote`) neemt de bouwer mee waar ze nog nodig zijn, met een test die de reden vastlegt.

**Launch-opdracht.** Forge start de agent via een agent-target, nu `scripts/forge_agent.sh` in
paia met een profiel. Lighthouse schrijft vóór de spawn een **drager**: een JSON-bestand per
workspace in de statemap van lighthouse, met een versieveld. Daarin staan:

- de volledige argumenten voor `claude`: `--permission-mode auto`, `--strict-mcp-config` en
  `--mcp-config <profiel.mcp_config>` (alleen als het profiel er een heeft),
  `--append-system-prompt-file <run-instructies>`, `--session-id` of `--resume`, `--model sonnet`
  bij `lane: native`, en de prompt;
- de omgeving: `LIGHTHOUSE_CONTROLLER_RUN=<session_id>` (het contract van de usage-guard) en het
  pad naar de statemap. Nooit een token.

Het agent-target verbruikt de drager en voert precies die argumenten uit. **Besluit:**
`--strict-mcp-config` geldt voor elke run, ook in het Software-profiel. Wat een profiel aan
MCP-servers nodig heeft, staat als expliciete lijst in `mcp_config`; dat vervangt de geërfde
container-set (paia-spec forge-runs, besluit 8). De **bovengrens** dwingt de tick af: een run
ouder dan `max_runtime` wordt gestopt (*Afloop*). Het target mag daarnaast een eigen grens
zetten (`RuntimeMaxSec`).

De run-instructies (nu `controller_forge_instructies.md`) worden een ingebed bestand in de
binary. De tick schrijft ze per run in de statemap.

**`lighthouse ask`** vervangt `scripts/controller_vraag.py`: een run post er zijn 🤖 Vraag mee.
Het commando valideert de vorm (maximaal 4 vragen met 2 tot 6 opties) met dezelfde parser die de
tick leest, en post daarna. Het draait in de werkplek, niet op de host: het opent de database
niet en post met het GitHub-token dat de run al heeft voor `gh` en zijn PR, uit de omgeving
(`GH_TOKEN`, `GITHUB_TOKEN` of `gh auth token`). Het issue en de repo leest het uit de drager.
Lukt de post niet, dan geeft het exit 1 met de reden, zodat de run het ziet *(eigen keuze na
spec-review, r1-f3)*. De naam is Engels, want `cmd/**` valt onder `code_language` (r1-f20).

**De starter blijft in paia** (besluit in het gesprek, "A voor nu"). Lighthouse gebruikt de
bestaande targets `paia-software` en `paia-infra`; `scripts/forge_agent.sh` blijft host-deploy
in paia. Vóór de eerste lighthouse-tick landt in paia één PR (*Overstap*, stap 0):

- `forge_agent.sh` leest naast de oude drager ook de lighthouse-drager, herkenbaar aan pad en
  versieveld. Zolang beide controllers draaien, verbruikt elk alleen de eigen drager.
- De devcontainer mount de statemap van lighthouse: de drager en de run-instructies alleen
  lezen, de runmap (voor de `Notification`-hook) schrijfbaar, plus het pad van de pauzevlag.
  De resume-map is al gemount.
- De devcontainer krijgt Go (besluit in het gesprek), zodat de issues van lighthouse op het
  Software-profiel draaien.

Een eigen starter in lighthouse, met eigen forge-targets, komt in M3: een tweede gebruiker heeft
geen paia.

### 7. Afloop van een run

Elke tick bekijkt elke open rij. Uitkomsten en de reden op het issue:

| Situatie | Uitkomst | Reden op 🤖 Teruggegeven |
|---|---|---|
| Nieuwe 🤖 Handoff van deze sessie | `handoff` (rij dicht, de Handoff-regel neemt over) | — |
| Eigen 🤖 Vraag gepost | `question` | — (de Vraag staat er al) |
| Geparkeerd: resume-marker voor deze sessie | `parked` | — |
| PR gemerged | `done` | — |
| PR groen (checks groen, niet draft, mergeable) en de sessie is klaar | `done`, post 🤖 Klaar | — |
| PR gesloten zonder merge, of draft | `returned` | "PR gesloten" / "PR is draft" |
| PR rood en de sessie is klaar | `returned` | "PR rood" (fix-pr volgt later) |
| **Startfout (#34)**: transcript met `Unknown command:`, of een sessie die klaar is zonder één assistant-beurt | `start_failed`; *Dedup* geeft één tweede poging, de tweede `start_failed` wordt Teruggegeven | "startfout: `<commando>` onbekend in de sessie" of "startfout: sessie deed niets" |
| Onzekere spawn, binnen `max_wait` geen sessie gevonden | `returned` | "start onzeker: geen sessie gevonden" |
| **Wacht op permissie (#35)** | `returned` na `max_wait` | "wacht op permissie: `<tool>` `<pad>`" |
| Stil: geen transcript-event, ook niet van een subagent, gedurende `max_wait` | `returned`, sessie gestopt | "geen activiteit in `max_wait`" |
| Ouder dan `max_runtime` | `returned`, sessie gestopt | "time-out (`max_runtime`)" |
| Sessie klaar zonder Handoff, Vraag of PR | `returned` | "geen handoff of vraag" |
| Werkplek weg, rij nog open | `returned` | "werkplek verdwenen" |

**#35 in detail.** Een controller-run die op een permissie-prompt staat, moet dat zichtbaar
maken. `pack/core` krijgt een `Notification`-hook. In een controller-run
(`LIGHTHOUSE_CONTROLLER_RUN` gezet) schrijft die bij `permission_prompt` een bestand in de
statemap van de run, met het tool en het pad uit de notificatie. De afloop leest dat bestand:
de reden noemt het tool en het pad, en de wachtklok gaat in op het moment van de prompt. Of de
hook de prompt meteen weigert, zodat de agent het ziet, beslist de bouwer na een meting. Het
bewijs moet wel laten zien dat een achtergrond-subagent zo'n bestand oplevert.

**Activiteit.** "Stil" meet de tijd sinds het laatste event in de transcripten van de sessie,
ook die van de subagents (`subagents/*.jsonl`). Een hoofdthread die zijn beurt beëindigt terwijl
een subagent werkt, is dus niet stil (#35, derde verwachting). De transcriptmap volgt uit de
cwd van de workspace. Voor het Software-profiel ligt die in het volume van de devcontainer; de
adapter levert het pad, of "niet meetbaar". Niet meetbaar valt terug op de forge-stand, en de
reden zegt dat.

**#34 buiten de dispatcher.** De oorzaak (een plugin-autoupdate in de container) hoort bij de
provisioning van de devcontainer (CorneeldH/personal_ai_agent#1463), niet in M2.

### 8. Werkplek bezet en opruimen (#31, #85)

**Bezet.** Een workspace is bezet als één van zijn sessies:

- hoort bij een open runrij en volgens de afloop nog loopt; of
- niet van lighthouse is (een interactieve sessie) en `working` is, of in een wachtende stand
  staat met een transcript-event in het laatste `max_wait`.

Een vreemde sessie die langer dan `max_wait` stil in `idle` of een andere wachtende stand staat,
telt niet meer als bezet. Lighthouse stopt haar niet: een interactieve sessie van de gebruiker
blijft ongemoeid (paia-keuze 11). Het overzicht meldt een uitgestelde start met de reden:
"werkplek bezet: sessie `<id>`, `<stand>` sinds hh:mm".

**Opruimen.** De tick verwijdert de workspace van een issue dat **niet meer in Controller
staat**, open of gesloten, als:

- er geen open runrij voor is;
- er geen vreemde sessie in staat, ook geen stille: een stille interactieve sessie telt niet
  als bezet voor een start, maar haar workspace verdwijnt niet onder haar *(eigen keuze na
  spec-review, r1-f16)*;
- `git status --porcelain` in de workspace leeg is;
- de branch geen niet-gepushte commits heeft.

Is de sessie- of een git-voorwaarde niet vervuld, of geeft forge 409, dan blijft de workspace
staan en staat hij in het overzicht onder *Werkplekken om op te ruimen*, met issue,
workspace-id en pad. Een lopende run wordt nooit afgebroken voor het opruimen. Of een
workspace waar een draaiende dienst naar wijst moet blijven, toetst lighthouse niet. De regel
"geen niet-gepushte commits en geen lokale wijzigingen" dekt het geval van #945 (de gebruiker
werkt er zelf in). Dat is een keuze voor de spec-review.

### 9. Blokkades (#56)

Een **uitvoerend** issue (een Handoff waarvan `next` niet in de spec-, plan- of reviewfase valt)
met een actieve `blocked_by`-voorganger, dus open en zonder gemergde PR:

- **Parkeren.** Eerst een comment `🤖 Geparkeerd` met de voorganger(s) en hun board-status,
  dan board naar On hold (*De tick*, Apply). Een issue in Controller met een eigen Geparkeerd als
  laatste 🤖-comment en een nog actieve voorganger gaat dus alsnog naar On hold. Dat is een nieuwe kind `kind_parked` in `pack/core/headings.toml` (nl
  "Geparkeerd", en "Parked") en in `internal/protocol`. *Mijn keuze*: direct parkeren, ook als de
  voorganger zelf in Controller staat. Hij start dan vanzelf, en het geparkeerde issue komt
  terug zodra hij gemerged is.
- **Terugzetten.** De tick leest ook On hold. Een issue met als laatste 🤖-comment een eigen
  Geparkeerd, waarvan geen voorganger meer actief is (gemerged, of gesloten), gaat terug naar
  Controller. De volgende tick start de laatste Handoff. Een voorganger die gesloten is zonder
  merge, telt als vervallen: dat is GitHubs eigen betekenis van een gesloten blokkeerder.
- **Menselijk On hold.** Een issue in On hold zonder eigen Geparkeerd als laatste 🤖-comment
  haalt lighthouse nooit terug.

Het stapelgeval van #56 vervalt, omdat stapelen uit M2 is.

### 10. Herhaalde fouten

Een actie die mislukt (een post, een board-wijziging, een spawn), schrijft lighthouse met haar
sleutel en de fout in de tabel van *Database*. Mislukt dezelfde actie met dezelfde fout in een
volgende tick, dan logt de tick één regel "nog steeds: …", zonder exit 1. Een nieuwe fout of een
nieuwe actie geeft wel exit 1. Een geslaagde actie wist haar regel. Zo vervangt dit
`apply-fouten.json`.

### 11. Pauze

`lighthouse pause [--until HH:MM]` en `lighthouse pause off` schrijven en wissen de pauzevlag
op het pad uit het contract van de usage-guard (`docs/usage-guard.md`, *Contract*). De regels
van `src/controller/pauze.py` blijven: standaard tot 21:00 vandaag; 's nachts is `--until`
verplicht; het einde ligt binnen 24 uur; zomertijd telt mee. De usage-guard parkeert lopende
runs al op die vlag (#66), mits de repo `[usage_guard] controller = true` heeft (*Configuratie*).
De tick hervat ze via de markers.

### 12. Zicht

- **`GET /api/runs`** (Huma, alleen lezen): de open runrijen en de geparkeerde runs, met repo,
  issue, titel, commando, trigger, start, profiel, workspace en de stand volgens de laatste
  tick. Plus de uitgestelde starts met hun reden.
- **Meerdere repo's in `serve`** (*mijn keuze*). Staat er `[dispatch] repos` in `host.toml`,
  dan leest `serve` die repo's en hun `[forge.board]`. Elk item in `/api/questions`,
  `/api/returned` en `/api/runs` krijgt een veld `repo`. Zonder `[dispatch]` blijft het gedrag
  van M1 (één repo uit `--repo`).
- **UI**: een kaart *Lopende runs* naast de kaarten van #69, en de repo bij elk item.
- **Overzicht (#85 deel 2)**: `internal/overview` neemt alleen issues mee die in Controller
  staan. Een issue buiten Controller komt nooit onder *Open vragen* of *Teruggegeven*. Een
  achtergebleven workspace komt hoogstens onder *Werkplekken om op te ruimen*. Daarom haalt
  lighthouse zelf een issue alleen uit Controller om het te parkeren; een Teruggegeven of een
  nacontrole laat het in Controller, en de gebruiker haalt het eruit (r1-f15).

### 13. Overstap

0. In paia: de PR uit *Launch* (dubbele drager in `forge_agent.sh`, mounts en Go in de
   devcontainer). Zonder die PR start lighthouse geen agent.
1. Lighthouse draait `tick` naast de Python-controller, alleen voor lighthouse zelf
   (Software-profiel). De Python-controller bedient lighthouse niet.
2. De end-to-end-proef uit de DoD, op lighthouse: handoff, start, vraag, antwoord, hervat,
   groene PR, en een geforceerde usage-pauze met twee runs.
3. paia loopt leeg: de pauzevlag staat, en er wordt gewacht tot geen paia-issue meer een open,
   geparkeerde of vragende run van de Python-controller heeft. Anders geeft de wezenregel elke
   lopende paia-run terug als "runstaat kwijt" *(eigen keuze na spec-review, r1-f13)*. paia
   krijgt `[usage_guard] controller = true` al; `[controller]` breidt uit met `profile`.
4. paia gaat over: `controller-tick.timer` roept `lighthouse tick` aan, `forge_agent.sh` leest de
   nieuwe drager, en `src/controller/` en `scripts/controller_{tick,launch,classify}.sh` gaan weg.
   Ook `controller_{gather,apply,forge,state,triage,vraag,auto}.py` gaan weg, zodra hun afnemers
   over zijn: triage en `auto_aanbevolen` hebben dan geen afnemer meer tot hun eigen issue landt.
   Dat werk gebeurt in paia, met een PR daar.

## Indeling in issues

Een voorstel voor `/lighthouse-write-issue`; de volgorde is de bouwvolgorde.

1. **Runstaat, claim en `tick`-skelet.** Serve-slot los van openen, migraties bij twee
   processen, `0002_runs.sql` met de herhaalde-fouten- en de tickstandtabel, claim, pogingen en
   vrijgeven, `tick.lock`, `host.toml [dispatch]` met meerdere repo's, de Go-lezer van
   `[forge.board]` en `[controller]`, het board-blok in `lighthouse.toml`, de tokenbron.
2. **Forge schrijven en de snapshot.** REST- en GraphQL-schrijven (comments, reacties,
   board-status), `blocked_by`, PR-stand, de permissie-check voor antwoorden, gather en de
   precheck.
3. **kenn-forge-werkplek.** De interface, de adapter, de drager, `--strict-mcp-config` met
   `mcp_config` per profiel, de bovengrens. Bezet en opruimen (#31, #85 deel 1). Het bewijs
   vergt de paia-PR van stap 0 van de overstap; dat issue hoort in paia.
4. **De keten in decide en apply.** Handoff, start, Vraag, antwoord met allowlist, Hervat,
   Klaar, `when: night`, nacontrole, dedup, `lighthouse ask`, de run-instructies.
5. **Afloop.** Elke regel uit *Afloop*, met #34 en #35. De `Notification`-hook in `pack/core`,
   en het meten van activiteit over subagent-transcripten.
6. **De rem.** Usage, geheugen, plekken, software, `lighthouse pause`, en de usage-hervatting
   via markers. `[usage_guard] controller = true` in `lighthouse.toml` en de zin in
   `docs/usage-guard.md`.
7. **Blokkades (#56)** en het overzicht alleen voor Controller-issues (#85 deel 2).
8. **Zicht en overstap.** `GET /api/runs`, de UI-kaart, de end-to-end-proef en de overstap in
   paia, met het leeglopen vóór de omschakeling.

De bestaande bugs vallen zo in: #31 en #85 in issue 3 en 7, #34 en #35 in 5, #56 in 7.

Daarnaast, buiten M2: roborev en fix-pr (overgezet van paia#1442), triage met
`auto_aanbevolen`, en de geplande runs.

## Wat aan de bouwer is

- Pakket- en typenamen, en de verdeling over `internal/…`.
- De precieze vorm van de vingerafdruk, zolang de regels uit *De tick* gelden.
- Welke workarounds uit `forge_ops.py` nog nodig zijn, met een test die de reden vastlegt.
- Of de `Notification`-hook de prompt ook weigert (*Afloop*), na een meting.
- De JSON-vorm van de drager, met een versieveld.

## Teststrategie

- **Decide** met snapshot-fixtures: per regel uit *Decide*, *Afloop*, *Blokkades* en *Bezet*
  een fixture met de verwachte acties. #558 (een verlaten `idle`-sessie), #1463 (`Unknown
  command`), #818 (permissie-prompt van een subagent), #464 en #757 (geblokkeerd), en #945 (een
  open issue buiten Controller met een levende workspace) worden fixtures naar hun echte
  toestand.
- **Claim**: twee processen claimen tegelijk hetzelfde issue, en precies één wint. Een mislukte
  launch geeft de claim vrij. Een tweede `tick` met een bezet `tick.lock` geeft exit 0 met de
  melding.
- **Migraties**: twee processen openen tegelijk een verse database; het schema is daarna één
  keer gemigreerd.
- **Forge en kenn-forge** met `httptest`-servers: een 5xx op de spawn start geen tweede agent;
  409 bij verwijderen laat de workspace staan.
- **Antwoorden**: een comment van een auteur zonder `admin`/`maintain` hervat niets.
- **Hook**: de `Notification`-hook schrijft alleen in een controller-run.
- **End-to-end**: de DoD-proef van de milestone, met bewijs in het issue van stap 8.
