# Werkplek

De werkplek is de kant van de dispatcher die met kenn-forge praat: een workspace klaarzetten, daar
een agent in starten, de sessies lezen, alles stoppen en de workspace opruimen. De code staat in
`internal/workplace` (interface, regels, levenscyclus) en `internal/workplace/kennforge` (de
HTTP-adapter). Het ontwerp staat in de [dispatcher-spec](developer/planning/specs/2026-10-27-m2-dispatcher-design.md)
(§6); de tick zelf staat in [tick.md](tick.md).

## De interface

`workplace.Workplace` heeft acht methoden: `Prepare(branch)` geeft een klare workspace,
`Spawn(ws, launch)` start de agent en geeft zijn sessie, `Sessions(workspaceID)` leest de
agentsessies, `Stop(workspaceID, sessionIDs)` beëindigt de processen, `Kill(workspaceID, sessionIDs)`
beëindigt alleen de claude van die sessies, `Remove(workspaceID)` verwijdert de workspace,
`Lookup(workspaceID)` leest het pad (`worktree_path`) en de branch (`git_head_ref`) van een workspace, en
`Discard(workspaceID)` vergeet een start waarvan de agent nooit verscheen (de adapter wist dan de
drager). Een workspace die forge niet meer kent (een 404 op de sessielijst of op `Lookup`) is
`workplace.ErrGone`; een workspace zonder `worktree_path` is voor `Lookup` een fout. Elke
subprocess-aanroep (git, systemctl, podman) loopt via een injecteerbare
`workplace.Runner`. Een onbekende exitcode of time-out is een fout, nooit een negatief signaal:
onmeetbaar is niet "weg" en niet "schoon".

## Prepare

`Prepare` leest alle workspaces van forge. Forge toont die van elke repo, dus een bestaande workspace
telt alleen als `repo.provider` gelijk is aan `[forge] kind` en `repo.repo_path` gelijk aan
`[forge] repo`, zonder hoofdlettergevoeligheid (forge geeft `repo_path` in kleine letters). Daarbinnen
telt de workspace waarvan `git_head_ref` de issue-branch is. Is er geen, dan maakt `Prepare` er een met
`POST /api/v1/repo/<kind>/<repo>/workspaces` en wacht tot de status `ready` is (standaard 60 s).

Daarna volgt de workspace `origin/<branch>`, als die branch bestaat:

- Een nieuwe workspace komt bij forge op `origin/HEAD`, niet op de branch: `Prepare` doet
  `checkout -B <branch> --track origin/<branch>`.
- Een gevonden workspace kan werk van een eerdere run bevatten, dus nooit `checkout -B` eroverheen.
  Staat de workspace op de issue-branch, dan volgt alleen een fast-forward (`merge --ff-only`), en alleen
  als `HEAD` een voorouder is van `origin/<branch>`. Is `HEAD` geen voorouder, dan geldt de
  rev-list-regel hieronder.
- Staat de workspace op een andere branch (bijvoorbeeld `main`), dan geldt de rev-list-regel: pas als
  `HEAD` geen commit heeft die op geen enkele `origin`-ref staat, schakelt `Prepare` om. Een
  fast-forward zou daar die andere branch verplaatsen; lokale `main` gaat dus nooit mee.

## Het dragercontract (paia#1661)

`Spawn` schrijft vóór de handoff een drager: een JSON-bestand dat het forge-target leest om de agent te
starten. Het contract:

- **Pad:** `<statemap>/carriers/<workspace-id>.json`. De statemap is een absoluut pad. Het workspace-id
  moet een veilige bestandsnaam zijn (`[A-Za-z0-9][A-Za-z0-9._-]*`); anders weigert `Spawn`.
- **Rechten:** mode `0600`, de map `0700`, ook als de map al bestond met ruimere rechten. Het bestand
  wordt atomair geschreven (tempbestand en rename).
- **Velden:** `version` (nu `1`), `repo`, `issue`, `session_id` (een UUID v4 in kleine letters), `mode`
  (`start` of `resume`), `profile`, `args`, `env`, `created_at` (RFC 3339, UTC).
- **`args`**, in deze volgorde:
  1. `--permission-mode auto`
  2. `--strict-mcp-config` (staat in elke run)
  3. `--allowedTools=Read(~/.claude/plugins/cache/**)` (staat in elke run). Een skill leest zijn eigen
     referentiebestanden uit de plugin-cache, buiten de worktree. In `auto` kan de classifier die `Read`
     met een permissieprompt stilzetten, en die beantwoordt niemand (#246). Eén argument met `=`, want
     de vlag neemt meer waarden en zou anders het volgende argument opslokken.
  4. `--mcp-config <pad>`, alleen als het profiel een MCP-config heeft (absoluut pad)
  5. `--append-system-prompt-file <pad>` (absoluut pad)
  6. `--session-id <uuid>` bij `start`, `--resume <uuid>` bij `resume`
  7. `--model sonnet`, alleen op de native lane
  8. de prompt als laatste argument. Hij mag niet met `-` beginnen (claude zou hem als vlag lezen) en
     geen NUL-byte bevatten; anders weigert `Spawn`.
- **`env`:** precies `LIGHTHOUSE_CONTROLLER_RUN` (de session-id van de run) en `LIGHTHOUSE_STATE_DIR`. Met die twee schrijft de hook
  `permission-check` een permissie-prompt van de sessie naar `<statemap>/runs/<sessie>/permission.json`;
  de tick leest dat bestand (zie *Afloop* in [tick.md](tick.md)).
- **Nooit een token.** De drager heeft er geen veld voor en `env` bevat er geen. Het forge-token blijft
  in `[dispatch.forge] token_file` en wordt per verzoek gelezen.
- **Het target verbruikt de drager** en voert precies `args` uit, niets toevoegen of weglaten.
  Verbruikt het target hem niet, dan blijft hij staan tot lighthouse hem wist. Lighthouse wist hem
  precies in deze gevallen:
  1. een niet-aangenomen spawn (`ErrNotAccepted`, zie hieronder);
  2. decide geeft een run terug met "start onzeker: geen sessie gevonden" (`Discard`, zie *Afloop* in
     [tick.md](tick.md)), zodat een laat startend target niets meer start;
  3. `Remove` na een geslaagde `DELETE` of een 404;
  4. `Cleanup` ziet dat forge de workspace niet meer kent (`ErrGone`).

  Na een onzekere spawn (`ErrUncertain`) blijft de drager staan: er kan een agent draaien.

## Spawn: drie uitkomsten

`Spawn` stuurt `POST /api/v1/workspaces/{id}/runtime/agent-handoffs` met `{"target_key", "message"}` en
verstuurt nooit een tweede POST. Er zijn drie uitkomsten:

1. **Aangenomen.** Het antwoord heeft een `session.key`. `Spawn` geeft de sessie terug.
2. **Niet aangenomen** (`workplace.ErrNotAccepted`). Er draait geen agent en de drager is gewist. Dat
   geldt voor: geen target voor het profiel, een ongeldige drager, een mislukte schrijfactie, een
   mislukte sessielezing vóór de POST, een token-bestand dat ontbreekt of leeg is, een verbinding die
   niet tot stand komt (de dial faalt, er is niets verstuurd), een 4xx of 3xx (een redirect wordt nooit
   gevolgd), en een 5xx waarna de herlezing slaagt en geen nieuwe sessie van deze run toont.
3. **Onzeker** (`workplace.ErrUncertain`). Er kan een agent draaien. De drager blijft staan. Nooit een
   tweede spawn. Dat geldt voor: een time-out (30 s), een verbinding die breekt nadat het verzoek
   verstuurd is, een antwoord dat niet te lezen is of geen JSON is, een 2xx zonder `session.key`, en een
   5xx waarna de herlezing mislukt.

**De herlezing.** Na een 5xx en na elke onzekere uitkomst leest `Spawn` de sessies één keer opnieuw. Een
sessie telt alleen als ze nieuw is (een key die vóór de POST niet bestond) en van deze run: met het
`session_id` van de run (die wint), of nog zonder `session_id`. Een nieuwe key met een ander `session_id`
is bewezen niet van ons, want de drager geeft `--session-id` mee. Vindt de herlezing zo'n sessie, dan is
de spawn aangenomen. Zo niet:

- na een 5xx met een geslaagde herlezing: niet aangenomen, en de adapter wist de drager, zodat een laat
  startend target hem niet meer verbruikt;
- na een 5xx met een mislukte herlezing: onzeker. Of er een agent startte, is niet te meten, en
  onmeetbaar is nooit "niet aangenomen";
- na een onzekere uitkomst: onzeker.

**Het 5xx-besluit.** Spec §6 zegt dat een 5xx "niet aangenomen" is. Maar de agent kan gestart zijn vóór
de 5xx; daarom de herlezing.

**Een laat startende agent.** Verschijnt de agent van een eerdere poging pas na de herlezing, dan is zijn
rij al `start_failed` (of door decide teruggegeven als "start onzeker"), zonder
`workplace_session`. Zijn key staat dus op geen enkele rij. Zo'n sessie telt als **vreemd**: bezet
zolang ze `working` is of binnen `max_wait` actief, en `ForeignLive` houdt de workspace staan. Het
opruimen stopt haar nooit.

## Launch en AdoptSessions

`Launch` loopt na `runs.Take`: `Prepare`, `runs.SetWorkplace`, `Spawn`, `runs.SetSession`. De uitkomst:

- `Accepted`: de rij heeft de sessiekey.
- `NotAccepted`: de rij is gesloten als `start_failed`, met de fout van `Prepare` of `Spawn` als reden.
  Mislukt `SetWorkplace`, dan sluit `Launch` de rij ook als `start_failed` (er is niets gestart, en een
  rij zonder `workspace_id` zou nooit sluiten). `Launch` geeft die reden terug; apply zet hem op de
  logregel van de tick en in de 🤖 Teruggegeven onder de Gestart (zie [tick.md](tick.md)).
- `Uncertain`: `Launch` geeft **tegelijk een fout** terug, alleen als diagnose (de oorzaak van de twijfel).
  De rij blijft open zonder sessie. De aanroeper (#114) mag voor dit paar nooit opnieuw spawnen.

`Launch` toetst `Busy` niet zelf. De aanroeper (decide, #114) toetst `Busy` vóór elke start, ook vóór
een tweede poging.

`AdoptSessions(ctx, db, w, repo)` loopt aan het begin van de precheck van de tick. Het legt alleen de
sessiekey vast van een open run met een workspace maar zonder key (een onzekere start), als zijn
`session_id` in de sessies staat. Een sessie die al eigen is van een rij van het issue (`own.Has`) telt
niet: een resume-run deelt het `session_id` van de eerdere run, en diens sessie kan nog in de lijst
staan. Het sluit, stopt en vergeet nooit iets, en een run die het niet kan opzoeken houdt zijn lege key;
de fouten worden samengevoegd en de andere runs worden wel opgezocht. Wat er met een sessie moet die
niet verschijnt (start onzeker) of die te lang loopt (`max_runtime`), beslist decide; zie *Afloop* in
[tick.md](tick.md).

## Transcripten

`workplace.Transcripts` is een optionele interface naast `Workplace`:
`Transcript(ctx, workspaceID, sessionID)` leest het transcript van één sessie, zoals
`workplace.ReadTranscript` hieronder. De map is `~/.claude/projects/<cwd, elk teken buiten
[A-Za-z0-9] als '-'>` (`workplace.ProjectDir`). Een 404 van forge blijft `workplace.ErrGone`.

- Profiel `software`: claude draait in de gedeelde devcontainer en houdt zijn transcripten in een
  volume dat de host niet kan lezen (eigenaar een subuid, mode 700). De adapter haalt daarom de
  bestanden van de sessie uit de container: per devcontainer (dezelfde `podman ps` als bij `Stop`)
  één `podman exec <c> sh -c <script>`, dat `<sessie>.jsonl` en `<sessie>/subagents` als tar
  uitvoert, of exit 3 geeft als de sessie daar niet staat. De adapter pakt alleen die bestanden uit
  in een tijdelijke map, leest ze en ruimt de map op. Staat de sessie in geen container, dan
  wikkelt de fout `fs.ErrNotExist`; een falende `podman` is een fout van de tick (#237).
  Met de hand: zoek de container met `podman ps --filter label=devcontainer.local_folder` en lees
  `podman exec <container> sh -c 'cat "$HOME/.claude/projects/<map>/<sessie>.jsonl"'`, waarbij
  `<map>` de projectmap uit `workplace.ProjectDir` is.
- Elk ander profiel: de map op deze host. Staat die er niet, dan is dat `workplace.ErrNotMeasurable`:
  de run is niet meetbaar, en dat is geen fout van de tick.

`workplace.ReadTranscript(dir, sessionID)` leest `<sessie>.jsonl` en de subagent-transcripten onder
`<sessie>/subagents` en geeft een `Transcript`: de laatste gebeurtenis over alle draden (`LastEvent`),
per draad de laatste (`Threads`; `""` is de hoofdthread), het commando waarop de sessie niet startte
(`UnknownCommand`) en of er een assistent-beurt was (`AssistantTurn`). Een kapotte of afgebroken laatste
regel telt niet mee. Ontbreekt het hoofdtranscript, dan wikkelt de fout `fs.ErrNotExist`; heeft geen
draad een regel met tijd, dan is het `ErrNotMeasurable`.

## Stop

`Stop(workspaceID, sessionIDs)` leest het `worktree_path` van de workspace en bepaalt de unit:
`paia-forge-<profiel>-<basename van het pad>`, waarbij elke byte buiten `[A-Za-z0-9_-]` een streepje
wordt (een multibyte teken geeft er dus meer). Dan:

1. `systemctl --user stop <unit>`; de exitcode telt niet (een unit die er niet is, faalt).
2. `systemctl --user is-active <unit>` beslist: alleen `inactive` of `failed` is gestopt. Alles anders is
   een fout.
3. Alleen bij het profiel `software` draait claude in de gedeelde devcontainer, buiten de cgroup van de
   unit, en forge heeft geen stop-endpoint. Dan zoekt `Stop` in elke devcontainer, via
   `podman ps --filter label=devcontainer.local_folder` (alleen de sleutel). De gedeelde container hoort
   bij de checkout van de launcher, en dat kan een andere repo zijn dan die van de run (#237); een
   filter op de hoofdcheckout van de repo vond dan niets en stopte stil niets. Het sessie-id is een
   verse UUID, dus een vreemd proces past er niet op. Daarna doet `Stop` per container en per
   sessie-id `pgrep -f <id>` (rc 0 draait, rc 1 weg, de rest een fout), `pkill -f <id>` en tot vijf keer
   een `pgrep` om te bevestigen.

`Stop` weigert een leeg sessie-id vóór elke aanroep: `pgrep -f ""` en `pkill -f ""` raken elk proces in
de gedeelde devcontainer, ook vreemde sessies.

## Kill

Een claude blijft na zijn laatste antwoord draaien, ook als forge zijn sessie `done` noemt. Een hervatting
van dezelfde sessie (`--resume <sessie>`) weigert forge zolang die oude claude leeft: een 500
`submit initial message failed` (#206). Ook een nieuwe start in dezelfde worktree faalt zo: de unit van
de oude run draait door zolang zijn claude leeft, en de launcher start de nieuwe met dezelfde
unitnaam. `systemd-run` weigert die (`Unit … was already loaded`), de pane sterft, en forge geeft
dezelfde 500 (#237). `Kill(workspaceID, sessionIDs)` beëindigt daarom alleen de
claude-processen van de gegeven sessies, op sessie-id, en raakt de unit nooit: een vreemde sessie in
dezelfde worktree draait door.

- Profiel `software`: per container (dezelfde `podman ps` als bij `Stop`) en per sessie-id `pgrep -f <id>`,
  `pkill -f <id>` en tot vijf keer een `pgrep` om te bevestigen.
- Elk ander profiel: dezelfde stappen op deze host, zonder `podman exec`.

Draait er niets met dat id, dan is dat geen fout. Een onmeetbare `pgrep` of een proces dat `pkill`
overleeft wel. Ook `Kill` weigert een leeg sessie-id vóór elke aanroep.

De tick roept `Kill` op twee plekken aan (zie *Afloop* in [tick.md](tick.md)): bij het sluiten van een run
als `done`, `handoff` of `question`, en vlak vóór de handoff van een hervatting, na de claim.

## Eigen en vreemd

`runs.SessionKeys` geeft per rij van het issue (open en gesloten) met een `workplace_session` die key en
het `session_id` van de rij. `workplace.Own.Has` beslist daarmee of een sessie **eigen** is. Daarvoor
moeten twee dingen gelden:

1. haar key staat op een rij van het issue;
2. haar `session_id` bij forge is leeg, of gelijk aan het `session_id` van die rij.

Elke andere sessie is **vreemd**. `Busy`, `ForeignLive`, de stopselectie van `Cleanup` en de zoektocht van
`AdoptSessions` gebruiken alle vier deze ene regel.

Waarom de key: een resume-run krijgt een nieuwe rij met hetzelfde `session_id` als de eerdere run.
Mislukt de start van zo'n resume zonder key, dan zou een late agent met dat `session_id` op het
`session_id` alleen als eigen gesloten sessie tellen: `Busy` zag dan geen bezetting (een tweede spawn) en
`Cleanup` stopte haar. Op de key telt ze als vreemd.

Waarom ook het `session_id`: de herlezing na een spawn neemt een nieuwe key zonder `session_id` aan en
legt die vast. Was dat in werkelijkheid een vreemde agent die tussen de voorlezing en de herlezing
startte, dan vult forge later haar eigen `session_id` in. Dat wijkt af van dat van de rij, dus telt ze
vanaf dan als vreemd. Op de key alleen bleef ze voor altijd eigen, en `Cleanup` zou de hele unit met de
vreemde agent erin stoppen.

## Bezet (#31)

`workplace.Busy(sessions, own, open, activity, now, maxWait)` geeft de eerste sessie die de werkplek
bezet maakt. `own` komt uit `runs.SessionKeys`; `open` bevat de `session_id`'s van de open runs.

- Een sessie van een **open** run (op `session_id`, als eerste getoetst) is bezet zolang ze niet `done`
  is.
- Een sessie die eigen is (`own.Has`, zie hierboven), is een **gesloten eigen** sessie en telt niet: dat
  is geen reden om te wachten.
- Elke andere sessie is **vreemd** (ook een laat startende, zie hierboven): bezet als ze `working` is, of
  `waiting` en haar laatste activiteit onbekend of jonger dan `max_wait` is. De activiteit komt uit het
  transcript van de sessie en valt terug op `updated_at` van forge. `done` is nooit bezet.

`BusyReason` geeft de tekst voor de gebruiker; een lege state heet daar `unknown`. De reden bestaat omdat
een tweede agent in dezelfde worktree het werk van de eerste stuk kan maken.

`ForeignLive(sessions, own)` geeft de eerste sessie die niet eigen is en niet `done`, ook een stille.

## Opruimen (#85)

`Cleanup(candidate)` verwijdert een workspace alleen als er niets mee verloren gaat. Een kandidaat zonder
workspace-id is een fout van de aanroeper: `Cleanup` geeft een fout en legt niets vast. Het doet niets bij
een kandidaat die nog in de controller staat, of waarvan het issue of de workspace een open run heeft: een
lopende run wordt nooit afgebroken. Daarna, in deze volgorde, en elke stap die de workspace moet laten staan is een
**achterblijver** (`Leftover`, in `tick_state` met soort `cleanup`, per repo en workspace):

1. Het pad. Heeft de kandidaat geen pad, dan zoekt `Cleanup` het op met `Lookup`. Een 404 daar telt als
   verwijderd, net als in stap 2; een andere fout wordt `not measurable: lookup: …`. Geen absoluut pad:
   `not measurable: no workspace path`, zonder `Stop` en zonder `Remove`. `git -C ""` of een relatief
   pad zou de werkmap van het proces meten.
2. Sessies lezen. Kent forge de workspace niet meer (`ErrGone`, een 404), dan telt hij als verwijderd,
   net als na een 404 op `Remove`: de drager wordt gewist en het achterblijver-record verdwijnt. Lukt het
   wissen van de drager niet, dan blijft hij staan als `not measurable: discard: …`. Andere fout:
   `not measurable: sessions: …`.
3. Eigen keys lezen (`runs.SessionKeys`). Mislukt: `not measurable: session keys: …`.
4. Een niet-`done` vreemde sessie (`ForeignLive`, ook een stille): `foreign session <key>`. Een
   vreemde sessie stopt lighthouse nooit; de workspace blijft onder haar staan.
5. De eigen sessies (`own.Has`) stoppen (`Stop`, met hun `session_id`'s). Vlak ervoor leest `Cleanup` de
   open runs opnieuw: een run die `lighthouse run` sinds de eerste check claimde, telt zijn agent hier als
   eigen, en `Stop` zou hem beëindigen; dan blijft alles staan. `Stop` beëindigt de hele systemd-unit van
   de worktree, dus dat gebeurt pas nadat stap 4 geen vreemde sessie vond. Het gebeurt vóór de git-checks:
   een eigen agent die nog werkt, kan na de check niets meer committen. Een eigen sessie zonder
   `session_id` die niet `done` is, is niet te stoppen: `not measurable: own session <key> has no
   session id`. Mislukt `Stop`: `not measurable: stop: …`.
6. Een vuile worktree: `uncommitted changes`. Onder git-status vallen ook untracked bestanden.
7. Commits op geen enkele `origin`-ref: `unpushed commits`. Is `HEAD` gelijk aan de head-SHA van de
   gemergede PR (`MergedHead`), dan telt dat niet: elke PR wordt squash-merged en GitHub verwijdert de
   branch, dus zonder deze uitzondering bleef elke afgeronde workspace staan.
8. Een fout bij git: `not measurable: <fout>`. Een worktree die niet meetbaar is, is nooit "schoon".
9. Opnieuw de open runs. `lighthouse run` neemt geen `tick.lock`: een run die tijdens de git-checks
   geclaimd werd, op het issue of op de workspace, houdt de workspace. Onleesbaar:
   `not measurable: open runs: …`. Eén venster blijft open: een `lighthouse run` die de workspace met
   `Prepare` al vond maar zijn run nog niet claimde, ziet `Cleanup` niet. Die start mislukt dan als
   `start_failed`, en de volgende tick doet de tweede poging in een nieuwe workspace. Er gaat geen werk
   verloren: de workspace was schoon en gepusht.
10. `Remove`. Een 409 van forge is `forge refused: dirty worktree (409)`. Een andere fout wordt
   `not measurable: remove: <fout>`, en `Cleanup` geeft die fout ook terug. Een 404 telt als al weg.
   Na een geslaagde `DELETE` of een 404 wist de adapter de drager; lukt dat niet, dan is dat een fout van
   `Remove`.

Een vuile of niet-gepushte workspace heeft dus zijn eigen sessies gestopt en blijft staan. Is de
workspace verwijderd, dan verdwijnt ook zijn achterblijver-record.

## Wie roept dit aan

Apply (`internal/dispatch`) roept `Busy` en `Launch` aan, voor `lighthouse tick` en `lighthouse run`
(zie [tick.md](tick.md)). Apply toetst `Busy` vóór `runs.Take`, dus vóór elke start en ook vóór een
tweede poging, en start de agent met `Launch` als laatste stap. De instructies van de run schrijft
apply naar `<statemap>/runs/<sessie>/instructions.md` (bestand 0600, map 0700); de statemap is
`${XDG_STATE_HOME:-~/.local/state}/lighthouse/dispatch`. `AdoptSessions` loopt in de precheck van de
tick, en de `activity` (laatste transcript-gebeurtenis per sessiekey) komt uit de transcriptmeting.
`Sweep` roept `Cleanup` aan, elke tick na apply, voor elke repo waarvan de precheck slaagde: ook als de
repo niet *due* is, want een vuile workspace die de gebruiker later opruimt verandert de vingerafdruk
niet. Een repo zonder leesbaar board slaat de tick over, dus van geen enkel issue is dan bekend dat het
de controller verliet en wordt niets verwijderd. De kandidaten komen uit de runrijen
(`runs.Workspaces`): één per workspace, met het issue en de branch van de laatste rij. Per kandidaat:

- Staat het issue op `Controller` of `On hold`, of heeft het een open run, dan blijft de workspace
  staan en verdwijnt een oud achterblijver-record. On hold telt als in de controller: de tick zet een
  geparkeerd issue zelf terug.
- Anders zoekt `Sweep` de head-SHA van de laatst gemergede PR van de branch op (`MergedHead`; een PR uit
  een fork telt niet). Mislukt dat, dan is de workspace een achterblijver met
  `not measurable: merged PR: …`.
- Dan `Cleanup`, met een kandidaat zonder pad: `Cleanup` zoekt het op.

Een verwijderde workspace (ook een 404) krijgt `workspace_removed_at` op al zijn runrijen en wordt
daarna niet meer getoetst. De fout van één kandidaat houdt de andere niet tegen.
