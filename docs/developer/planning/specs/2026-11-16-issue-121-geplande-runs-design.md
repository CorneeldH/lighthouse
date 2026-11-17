# Ontwerp: geplande runs in lighthouse (#121)

Issue: [#121](https://github.com/CorneeldH/lighthouse/issues/121) · Datum: 2026-11-16 · Bouwt op
[M2, de dispatcher](2026-10-27-m2-dispatcher-design.md) (die de geplande runs onder *Niet in M2*
zette) en volgt [Lighthouse zonder Python](2026-11-07-zonder-python-design.md).

## Aanleiding en doel

In personal_ai_agent (paia) draaien twee agentjobs als eigen systemd-timers, los van de
controller:

| Job | Timer | Script | Wat |
|---|---|---|---|
| codebase-map-refresh | `OnCalendar=*-*-01 04:00`, `Persistent=true` | `scripts/codebase_map_refresh.py` (597 regels) | meet drift, draait `/gsd-map-codebase` (fast of full), spiegelt de zeven documenten naar de docs-site, commit en pusht naar `main` |
| reflect-skills | `OnCalendar=Mon 01:00`, alleen de eerste week van een iteratie van 14 dagen | `scripts/reflect_skills_run.py` (1146 regels) | telt nieuwe sessies, bouwt een corpus uit `~/.claude/projects`, draait `/paia-reflect-skills` onder containment, commit en pusht één document naar `main` |

Ze vallen buiten de rem van de dispatcher (usage, geheugen, plekken). Ze concurreren dus blind met
dispatcher-runs om dezelfde subscription, en ze leven als Python naast een binary die juist van
Python af wil.

**Doel:** lighthouse plant en draait zulke jobs zelf. Een geplande run start op een schema, in een
werkplek, onder dezelfde rem als een dispatcher-run, en staat in `GET /api/runs`. De twee jobs
worden ingebouwde Go-jobs van de binary; de paia-timers en -scripts gaan weg.

**Klaar als** de vier AC van #121 gelden:

1. Een geplande run is per repo in te stellen (welk commando, welk schema).
2. De tick start een geplande run alleen binnen de rem, en nooit twee tegelijk voor hetzelfde
   schema.
3. De codebase-map-refresh en reflect draaien zo voor paia; hun eigen timers in paia gaan weg.
4. De lopende geplande runs staan in `GET /api/runs`.

## Doelen, niet-doelen, anti-doelen

**Doelen**

- Een generieke planner in de tick: schema's per repo, inhaal van één gemist slot, de rem van
  de dispatcher, hooguit één open run per schema.
- Twee ingebouwde Go-jobs, `map-refresh` en `reflect`, met het gedrag van de paia-scripts als
  referentie. Ze zijn niet aan paia gebonden: paden, commando's en drempels zijn parameters.
- De uitkomst van een run gaat via een PR, en die wordt gemerged zodra hij groen is.
- Alles in Go, onder `lighthouse`. Geen nieuwe `.py`; shell hooguit als dun omhulsel.

**Niet-doelen**

- Willekeurige shell-commando's als job. Een schema noemt een ingebouwde job; een nieuwe soort
  job is een nieuwe Go-job (zie *Aanpakken*, B).
- Een eigen melder (ntfy). Een mislukte run is een rij in de DB en staat in `/api/runs` en de
  web-UI. De ntfy-melding van paia vervalt.
- Een port van `/gsd-map-codebase` of `/paia-reflect-skills`. Die skills blijven waar ze zijn;
  lighthouse roept ze aan.
- Andere jobs uit paia (deep-sweep, backups).

**Anti-doelen.** Dit telt als mislukt, ook als het technisch werkt:

- Een geplande run die de rem omzeilt, of twee runs van hetzelfde schema tegelijk.
- Een half geschreven map of een reflect-document dat landt: een gedeeltelijke uitkomst is erger
  dan geen (paia #439). Alles of niets.
- Een reflect-run die buiten zijn ene uitvoerpad schrijft en toch landt. Het corpus is
  onvertrouwde tekst en de skill heeft Write.
- Een geplande run die direct op `main` landt, buiten een PR om.
- Een stil ontbrekende run: een schema dat niet draait zonder dat `/api/runs` of de log het laat
  zien.

## Aanpakken

**Eerste keuze: waar de joblogica komt.**

- **A. Alleen een generieke planner.** Een schema noemt een commando, lighthouse start dat onder
  de rem. paia houdt zijn Python. Afgewezen: de Python blijft, en het lost de push naar `main`
  niet op.
- **B. Planner plus beide jobs in Go.** **Gekozen** (gebruiker). Past bij *Lighthouse zonder
  Python* en brengt de uitkomst via een PR.

**Tweede keuze: hoe een job draait.**

1. **Tick-gedreven, over meerdere ticks.** **Gekozen** (gebruiker). De tick draait de Go-poort,
   maakt een werkplek en start de agent met `Workplace.Spawn`; een latere tick ziet de sessie
   klaar, doet de nacontrole, opent de PR en merget hem. Past in de run-tabel, de sweep, de
   forge-werkplek en `/api/runs`, en overleeft een herstart. Kost een toestand over ticks heen.
2. **De tick start één Go-proces `lighthouse scheduled run <naam>`** dat alles lineair doet, zoals
   de paia-scripts. Eenvoudiger, maar gaat langs `Workplace` heen: geen forge-zicht, eigen
   procesbeheer naast de sweep. Afgewezen.
3. **Een eigen timer per job** die zelf de rem leest. Afgewezen: AC 2 zegt dat de tick start.

**Derde keuze: wat er met de PR gebeurt.** **Auto-merge als hij groen is** (gebruiker), zoals
regel 7 voor dispatcher-runs. Afgewogen tegen een PR die op een mens wacht en tegen een keuze per
schema; auto-merge houdt het huidige gedrag (de uitkomst landt zonder mens) en hergebruikt de
bestaande merge.

## Ontwerp

### 1. Configuratie

Per repo in `lighthouse.toml`, een lijst `[[schedule]]`:

```toml
[[schedule]]
name = "codebase-map"
job  = "map-refresh"
cron = "0 4 1 * *"

[schedule.params]
command        = "/gsd-map-codebase"
map_dir        = ".planning/codebase"
publish_dir    = "docs/developer/codebase"
documents      = ["STACK", "INTEGRATIONS", "ARCHITECTURE", "STRUCTURE", "CONVENTIONS", "TESTING", "CONCERNS"]
drift_paths    = ["src", "apps", "scripts", "deploy", "tests"]

[[schedule]]
name   = "reflect"
job    = "reflect"
cron   = "0 1 * * MON"
weeks  = 2
anchor = "2026-10-12"

[schedule.params]
command    = "/paia-reflect-skills"
output_dir = "docs/developer/reflect"
```

| Sleutel | Verplicht | Betekenis |
|---|---|---|
| `name` | ja | sleutel van het schema, uniek per repo; `[a-z0-9-]+`, want hij komt in een branchnaam |
| `job` | ja | `map-refresh` of `reflect`; een onbekende job is een configfout |
| `cron` | ja | vijf velden, in de lokale tijd van de host |
| `weeks`, `anchor` | samen, of geen van beide | alleen slots in de week van `anchor` en elke `weeks` weken daarna tellen; vervangt `is_iteration_week` (de week loopt van maandag tot en met zondag) |
| `profile` | nee | het profiel van de run; standaard `[controller] profile`. Telt mee in de rem zoals bij een issue-run (een `software`-profiel ook tegen `max_software`) *(eigen keuze)* |
| `params` | nee | job-specifiek, zie §5 en §6; elke parameter heeft een standaard |

De doelrepo moet de PR van een geplande run groen kunnen krijgen met alleen de toegestane paden
(bv. een repo met een changelog-check in CI heeft een configuratie nodig die dat toelaat); anders sluit
elke periode een rode PR als `failed`.

`repoconfig` leest en valideert het blok bij het laden; een fout maakt de repo-config ongeldig,
zoals elke andere configfout. Cron in plaats van systemd-`OnCalendar`: dat vraagt in Go een eigen
parser, en cron dekt beide schema's met het filter `weeks`/`anchor` erbij.

### 2. De planner in de tick

Per repo en per schema, elke tick, na het gather en vóór de nieuwe issue-starts:

1. **Aan de beurt?** Het laatste cron-slot op of vóór nu (door het filter) is later dan het slot
   het laatste afgehandelde slot van dit schema. Een slot is afgehandeld als `schedule_slots` het als
   `skipped` kent, of als de runs-rijen van `trigger_key` `scheduled:<name>:<slot>` een echte uitkomst
   hebben (`done` of `failed`) of twee pogingen (§3). De toestand is dus afgeleid uit de `runs`-rijen;
   er is geen aparte `started`-stand die los van de run-rij kan achterlopen *(eigen keuze na spec-review, r1-f2)*.
   Nooit afgehandeld: het laatste slot telt als aan de beurt.
2. **Inhalen.** Liggen er meerdere slots open (host uit), dan draait er één run, voor het laatste
   slot. Zo werkt `Persistent=true` ook.
3. **Open run?** Heeft dit schema een open run, dan niets. De afhandeling wacht tot hij sluit.
4. **Poort.** De Go-poort van de job draait (§4). Zegt die `skip`, dan noteert de planner het slot
   als `skipped` met de reden, één logregel, geen run.
5. **Rem.** Dezelfde `Brake` als een issue-start: pauze, usage, geheugen, plekken, software. Een
   geplande run telt als plek. Houdt de rem hem tegen, dan blijft het slot open en probeert de
   volgende tick het opnieuw; de poort draait dan opnieuw (die is goedkoop).
6. **Start.** Claim een run-rij (§3), maak de werkplek op branch
   `scheduled/<name>-<slot>-<poging>` (het slot als volledige tijdstempel `YYYYMMDDTHHMM`, zodat een
   tweede poging en een schema met meer slots per dag een eigen branch en werkplek krijgen *(eigen keuze
   na spec-review, r1-f10)*), start de agent (§4). Het claimen van de run-rij is de enige schrijfactie
   die het slot in gang zet; een tick die tussen claim en start sterft, laat een open run-rij achter die
   de sweep en de regels van §4 afhandelen, nooit een tweede run voor hetzelfde slot.

**Alleen op de tick-host** *(eigen keuze)*. Een geplande run krijgt altijd node `""`, nooit een
andere node. De nacontrole (§4) heeft lokale git in de workspace nodig, en de sessies van reflect
staan op deze host. Een `Workplace` met exec- of bestandsmethoden voor andere nodes valt buiten
dit ontwerp.

**Tijdelijk mislukt** *(eigen keuze)*. Eindigt de run zonder echte uitkomst, dan sluit hij met de nieuwe uitkomst `retry`
(niet `returned` of `parked`), gaat het slot weer open en probeert een volgende tick het met een verse
start. `runs.Take` telt voor trigger `scheduled` elke eerdere run van de `trigger_key` als poging
(niet alleen `start_failed`): de sleutel is besteed na een `done` of `failed`, of na twee pogingen
*(eigen keuze na spec-review, r1-f1)*. Dat geldt voor start mislukt,
start onzeker, stilte, looptijd, en geparkeerd door de usage-guard. Er komen maximaal 2 pogingen
per slot (`attempt` 1 en 2, zoals bij een issue-run). Daarna is het slot `failed` en wacht het
schema op zijn volgende slot. Er is geen hervatting van een geplande run: een verse start is
eenvoudiger, en de werkplek is van de run. Een `failed` uit de nacontrole of de PR is een echte
uitkomst: dan is het slot afgehandeld, zonder tweede poging.

Het volgende aan de beurt komende slot is een tijdgrens voor de precheck, naast de bestaande
grenzen die de gather-skip doorbreken; anders ziet de planner een slot pas bij de volgende verouderde gather.

Een geplande run gaat vóór nieuwe issue-starts in dezelfde tick: hij komt hooguit eens per periode
voor, en anders houdt een volle wachtrij hem eindeloos tegen. Hervattingen en fix-pr-runs van open
issue-runs gaan niet voor hem opzij; de rem telt ze gewoon.

### 3. Run-model en zicht

Eén `runs`-tabel, via een nieuwe migratie (tabel herbouwen, zoals `0004`):

- `issue` wordt nullable; een nieuwe kolom `schedule TEXT` (leeg voor een issue-run).
- Precies één van beide is gezet (`CHECK`).
- `trigger` krijgt `scheduled`; `trigger_key` is `scheduled:<name>:<slot>`.
- `outcome` krijgt `failed` voor een geplande run die de nacontrole of de PR niet haalde, en `retry`
  voor een tijdelijk mislukte (§2). Ook de `start_sha` van de start komt als kolom op de run (§4,
  nacontrole).
- Naast `runs_open_issue` (nu partieel op `issue IS NOT NULL`) een uniek index
  `runs_open_schedule ON runs(repo, schedule) WHERE ended_at IS NULL AND schedule IS NOT NULL`.
  Dat is de DB-kant van "nooit twee tegelijk".
- Een tabel `schedule_slots(repo, schedule, slot, outcome, reason, at)` voor alleen de `skip`-afhandeling
  uit §2 (`outcome` is `skipped`); de rest van de afhandeling leest uit de `runs`-rijen.

`RunView` krijgt `Schedule` (leeg voor een issue-run). `saveRunView` bewaart een geplande run onder
de `tick_state`-sleutel `scheduled:<name>` (een issue-run onder zijn issuenummer), en `RunViews`
ontdubbelt alleen issue-runs op issuenummer; twee open geplande runs in één repo blijven dus beide
zichtbaar (test: twee open geplande runs, AC 4); `Issue` en `Title` blijven, leeg voor een
geplande run. `GET /api/runs` geeft ze in dezelfde lijst. `web/openapi.json` en de gegenereerde
client worden opnieuw gemaakt; de web-UI toont een geplande run met zijn schemanaam in plaats van
een issue.

Alles wat nu een issue van een run verwacht (`OpenAll`, `CountRun`, `runs.Open`, `runs.Workspaces`,
de history- en brief-lezers die `issue` in een Go-`int` scannen, decide, apply, de sweep en
`saveRunView`) moet een geplande run aankunnen of overslaan. Een geplande run post nooit een 🤖
comment: er is geen issue.

### 4. Launch en afloop

**Launch.** De agent start via `Workplace.Spawn` met:

- `Prompt`: het slash-commando van de job, met zijn argumenten.
- `InstructionsFile`: een per run geschreven bestand met de onbemand-regels van de job (geen
  vragen, niet committen, niet pushen, welke paden de run mag schrijven). Dat veld bestaat al en
  wordt `--append-system-prompt-file`.
- Een nieuwe `LaunchSpec.Tools`: de toolset van de job. Lege lijst = het huidige gedrag. De
  carrier zet hem om in de vlag die de **beschikbare** tools beperkt. Let op: dispatcher-runs
  starten met `--permission-mode auto`, en daaronder keurt `--allowedTools` alleen vooraf goed
  en weigert niets. De bouwer kiest en test de vlag die Bash echt weghaalt (`--tools` of
  `--disallowedTools`).
- Geen MCP *(eigen keuze)*: een geplande run krijgt geen `MCPConfig`, ook als het profiel er een
  heeft, en de carrier houdt `--strict-mcp-config`. Een MCP-tool (GitHub, mail) is een handeling
  die de containment op `git status` niet ziet. De test bewijst dat er geen MCP-server laadt.

**Afloop.** Decide krijgt een eigen tak voor geplande runs, met dezelfde metingen. Gather en de
precheck-vingerafdruk nemen daarvoor de PR's van de branch van elke open geplande run mee (checks,
mergeability, roborev), niet alleen die van een bordissue; zo is de PR van een open geplande run
zichtbaar zonder bord-item *(r1-f3)*. De nacontrole is hervatbaar *(r1-f4)*: bestaat de branch al op
origin of heeft hij een PR, dan hergebruikt de volgende tick die in plaats van opnieuw te committen.

`max_runtime` (regel 19) geldt voor de agentfase, tot en met de nacontrole. Daarna loopt de PR-fase met
een eigen grens, `pr_timeout` (standaard 60 minuten): een PR die dan niet gemerged is, wordt gesloten en
de run `failed`, een echte uitkomst, geen tweede poging. Een pauze of een dichte rem tijdens de PR-fase
telt mee in die 60 minuten *(r1-f6)*.

| Situatie | Uitkomst |
|---|---|
| Sessie klaar, nog geen PR | nacontrole (§4, hieronder); geslaagd → commit, push, PR; niets veranderd → `done` zonder PR; mislukt → `failed` met reden |
| Eigen PR groen, rem open | squash-merge op de gemeten head (de predicaat van regel 7), daarna `Forge.DeleteBranch`, `done` |
| Merge geweigerd (`*MergeRefused`, bv. verplichte review) | PR sluiten, branch verwijderen, `failed` met de reden van de forge |
| Head verschoven (`ErrHeadMoved`) | niets, de volgende tick meet opnieuw |
| Eigen PR rood of met conflict | PR sluiten, branch verwijderen, `failed` met reden; geen fix-pr-run (de volgende periode probeert het opnieuw) |
| Eigen PR gesloten zonder merge | branch verwijderen, `failed` |
| Regels 9, 10, 15–19, en parkeren door de usage-guard | run sluiten; het slot gaat weer open voor de tweede poging (§2, *Tijdelijk mislukt*). Een PR van deze run wordt gesloten en zijn branch verwijderd |

*(eigen keuze)* Een mislukte PR sluiten voorkomt dat er elke periode een nieuwe naast een oude
blijft staan. De reden staat in de run-rij en in `/api/runs`. Daarvoor komen er
`Forge.ClosePR` en `Forge.DeleteBranch` bij.

Daarna ruimt de sweep de werkplek op. *(eigen keuze)* Een geplande run zonder open rij wordt
**geforceerd** weggegooid, ook als de werkboom vuil of niet gepusht is. De belofte "er gaat niets
verloren" van de sweep geldt voor issue-werkplekken. In een geplande werkplek staat niets wat
bewaard moet worden: wat telt, is gecommit en gepusht of mislukt met een reden in de DB, en het
transcript blijft bestaan. De sweep-tests bewijzen dat een vuile issue-werkplek blijft staan en
een vuile geplande werkplek weggaat. De merge van regel 7 ruimt geen branch op (`docs/tick.md`), dus
de geplande tak roept na een geslaagde merge zelf `Forge.DeleteBranch` aan.

**Nacontrole**, in de werkplek, in deze volgorde. Elke stap die faalt, maakt de run `failed`;
er komt niets op een branch.

1. **HEAD-guard.** Staat HEAD niet meer op `start_sha` (de commit van de start, op de run bewaard), dan committe de agent zelf:
   `git reset --mixed` naar de start, de bestanden blijven staan.
2. **Containment.** `git status --porcelain` toont alleen paden die de job mag schrijven.
3. **Volledigheid**, per job (§5, §6).
4. **Afronden** per job: spiegelen, `index.md` schrijven.
5. **Secret-scan** over alle gewijzigde bestanden, met de patroonset van de paia-scripts. Op een
   repo met gitleaks in CI is de PR-check een tweede laag; de scan in Go is de eerste en hangt
   daar niet van af.
6. **Commit** van alleen de toegestane paden (pathspec), push van de branch, PR via een nieuwe
   `Forge.CreatePR(ctx, branch, title, body)`. De PR-body is `No issue: geplande run <name>
   (<slot>)`. De push heeft schrijfrechten nodig in de omgeving van de tick; de bouwer kiest de bron
   (bv. het forge-token als HTTPS-credential) en test hem, want de tick pusht nu nergens.

Een restore zoals in de paia-scripts is niet nodig: de werkplek is van de run en gaat weg.

### 5. Job `map-refresh`

Gedrag: `scripts/codebase_map_refresh.py` in paia.

| Param | Standaard |
|---|---|
| `command` | `/gsd-map-codebase` |
| `fast_args` | `--fast --focus tech+arch` |
| `map_dir` | `.planning/codebase` |
| `publish_dir` | leeg (geen spiegel) |
| `documents` | de zeven van paia |
| `min_lines` | `20` |
| `drift_paths` | `["src", "apps", "scripts", "deploy", "tests"]` |
| `drift_low`, `drift_high`, `new_dir_weight` | `25`, `150`, `10` |
| `tools` | `Read Write Glob Grep Bash Agent` |

- **Poort**, op `origin/<default>`: basis is de laatste commit die `map_dir` raakte. Score =
  gewijzigde bestanden onder `drift_paths` sinds de basis + `new_dir_weight` × nieuwe mappen
  onder `drift_paths`. Onder `drift_low` → `skip`; tot `drift_high` → fast; daarboven → full.
  Nooit gemapt → full.
- **Containment:** alleen `map_dir` en `publish_dir`.
- **Volledigheid:** elk document uit `documents` bestaat onder `map_dir` met ≥ `min_lines`
  regels.
- **Afronden:** kopieer de documenten naar `publish_dir` (een kopie, geen symlink) en schrijf
  daar `index.md` (modus, score, basis, datum). Is `map_dir` na de run byte-gelijk aan HEAD (vergeleken vóór het spiegelen en het schrijven van `index.md`), dan geen PR.
- De looptijdgrens is `max_runtime` uit `host.toml`; paia gebruikte 45 en 120 minuten. Een job mag
  eigen `max_runtime_fast` en `max_runtime_full` als parameter hebben, met 45 en 120 minuten als standaard.

### 6. Job `reflect`

Gedrag: `scripts/reflect_skills_run.py` in paia.

| Param | Standaard |
|---|---|
| `command` | `/paia-reflect-skills` |
| `output_dir` | `docs/developer/reflect` |
| `min_sessions` | `20` |
| `window_min_days`, `window_max_days` | `14`, `90` |
| `min_lines` | `20` |
| `max_corpus_bytes`, `max_prompt_chars` | `400000`, `400` |
| `tools` | `Read Glob Grep Write` (geen Bash) |

- **Welke sessies** *(besluit gebruiker)*: zowel interactieve sessies als controller-sessies van
  de repo, met een harde scheiding tussen die twee. Verwachting van de gebruiker: op termijn zit
  de uitvoer helemaal in de controller, en brainstorm, spec en bugdetectie blijven interactief.
  Reflect moet daarom per soort patronen kunnen zien, niet één hoop.
  - *Vindplaats:* `~/.claude/projects/<gecodeerde repo>`, de worktree-mappen ervan
    (`--claude-worktrees-`) en de kenn-forge-werkplekmappen van de repo
    (`-kenn-forge-worktrees-…-<repo>-…`). De bouwer leidt de laatste af uit de werkplek-adapter,
    niet uit een losse regex.
  - *Soort:* een sessie is **controller** als haar sessie-id in `runs.session_id` staat (elke
    trigger behalve `scheduled`), anders **interactief**. De DB is de bron, niet het pad: een
    interactieve sessie in een kenn-forge-werkplek blijft interactief.
  - *Uitgesloten:* sessies van geplande runs (`trigger = scheduled`), dus ook de eigen eerdere
    reflect-rondes.
- **Poort:** tel de sessiebestanden van beide soorten samen, nieuwer dan de laatste ronde (de
  datum van het jongste document in `output_dir`), binnen het venster. Onder `min_sessions` →
  `skip`. De poortregel in de log noemt beide aantallen.
- **Corpus:** Go extraheert de gebruikersprompts (de extractie en `SKIP_PATTERNS` van paia,
  afgeleid van claude-reflect, MIT; de herkomst gaat mee als commentaar) en schrijft één
  corpusbestand in de werkplek, op `.lighthouse/run/reflect-corpus.md`, met twee secties:
  *Interactief* en *Controller*. Elke sessie staat onder haar soort. *(eigen keuze)* Binnen
  *Controller* worden identieke prompts samengevoegd met een telling, want die herhalen vaste
  commando's. Het budget `max_corpus_bytes` gaat half om half over de twee secties; wat de ene
  niet gebruikt, gaat naar de andere. Zo verdringt herhaling in de controller de interactieve
  prompts niet. Het slash-commando krijgt `--corpus` en
  `--out <output_dir>/<datum>-skill-kandidaten.md`. Go haalt het corpus vóór de nacontrole weg.
  De skill (`/paia-reflect-skills`) moet de twee secties apart wegen; dat is een wijziging in paia
  (§7).
- **Containment:** precies het ene uitvoerbestand plus `index.md` onder `output_dir`; elk ander
  bestand, ook onder `output_dir`, is een overtreding.
- **Volledigheid:** het document heeft ≥ `min_lines` regels.
- **Afronden:** schrijf `output_dir/index.md` met de lijst van rondes.

### 7. Overstap in paia

Paia's eigen werk, als laatste stap, in een aparte PR in personal_ai_agent:

1. De twee `[[schedule]]`-blokken in paia's `lighthouse.toml`, en `/paia-reflect-skills` leert
   de secties *Interactief* en *Controller* apart te wegen (§6).
2. Een eerste geplande run van elk, zichtbaar in `/api/runs`, met een PR die landt.
3. Daarna weg: `deploy/beelink/codebase-map-refresh.{service,timer}`,
   `deploy/beelink/reflect-skills.{service,timer}`, `scripts/codebase_map_refresh.py`,
   `scripts/reflect_skills_run.py` en hun tests, en de vermelding in `deploy/beelink/manifest.toml`.
4. Op de Beelink: `systemctl --user disable --now codebase-map-refresh.timer
   reflect-skills.timer`.

De docs-site op de Beelink serveert de paia-checkout; na een merge moet die checkout de map
binnenhalen. Hoe paia dat nu voor andere merges doet, geldt hier ook; dit ontwerp verandert dat
niet.

### 8. Documentatie

- `docs/tick.md`: een sectie *Geplande runs* (config, planner, afloop) en de nieuwe rijen in
  *Zicht*.
- `docs/install.md`: het `[[schedule]]`-blok bij de repo-vereisten.
- Een fragment in `changelog.d/` per PR.

## Indeling in PR's

1. **Run-model en planner:** migratie, `repoconfig`, de planner in de tick, decide en apply voor
   geplande runs, `LaunchSpec.Tools`, `Forge.CreatePR`, `ClosePR` en `DeleteBranch`, de
   geforceerde sweep, `RunView` en de web-client. Met een
   testjob in de tests, nog geen echte job.
2. **`map-refresh`.**
3. **`reflect`.**
4. **paia** (andere repo, §7). Sluit AC 3.

#121 sluit na PR 4; PR 1–3 dragen `Refs #121`.

## Wat aan de bouwer is

- Pakket- en typenamen; een voorstel is `internal/scheduled` met een interface per job (`Gate`,
  `Launch`, `Check`, `Finish`).
- De cron-parser: een kleine eigen parser of een afhankelijkheid; de go-standard weegt.
- De precieze vorm van `schedule_slots` en de migratie, zolang §2 en §3 gelden.
- De vlag voor `LaunchSpec.Tools` (zie §4), met een test die bewijst dat Bash weg is.

## Teststrategie

- **Planner:** tabeltests met een nep-klok: slot aan de beurt, filter `weeks`/`anchor`, inhaal
  van meerdere gemiste slots (één run), `skipped` telt als afgehandeld, de rem dicht laat het
  slot open, een open run blokkeert een tweede.
- **DB:** de migratie op een DB met bestaande runs; het unieke index weigert een tweede open run
  van hetzelfde schema.
- **Afloop:** decide-tests per rij van de tabel in §4, met de fakes van `tickEnv`.
- **Nacontrole:** in een echte git-repo in `t.TempDir()`: een eigen commit van de agent, een pad
  buiten de toegestane, een ontbrekend of te kort document, een secret, en een geslaagd geval met
  alleen de toegestane paden in de commit.
- **Jobs:** de drift-score en de route; de sessieteller en het venster; de corpus-extractie op
  fixture-transcripten, met dezelfde fixtures als de paia-tests waar die er zijn; de soort van
  een sessie uit de DB (een interactieve sessie in een kenn-forge-map blijft interactief, een
  geplande run telt niet mee); het samenvoegen van identieke controller-prompts en de verdeling
  van het budget.
- **Tijdelijk mislukt:** een eerste poging die mislukt, opent het slot; een tweede sluit het als
  `failed`.
- **API:** `GET /api/runs` geeft een geplande run met `schedule`; de drift-check van de
  gegenereerde client blijft groen.
- Bewijs voor AC 3 is een echte run in paia (§7), niet een test.
