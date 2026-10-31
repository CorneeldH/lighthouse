# Werkplek

De werkplek is de kant van de dispatcher die met kenn-forge praat: een workspace klaarzetten, daar
een agent in starten, de sessies lezen, alles stoppen en de workspace opruimen. De code staat in
`internal/workplace` (interface, regels, levenscyclus) en `internal/workplace/kennforge` (de
HTTP-adapter). Het ontwerp staat in de [dispatcher-spec](developer/planning/specs/2026-10-27-m2-dispatcher-design.md)
(§6); de tick zelf staat in [tick.md](tick.md).

## De interface

`workplace.Workplace` heeft zes methoden: `Prepare(branch)` geeft een klare workspace,
`Spawn(ws, launch)` start de agent en geeft zijn sessie, `Sessions(workspaceID)` leest de
agentsessies, `Stop(workspaceID, sessionIDs)` beëindigt de processen, `Remove(workspaceID)` verwijdert
de workspace en `Discard(workspaceID)` vergeet een start waarvan de agent nooit verscheen (de adapter
wist dan de drager). Een workspace die forge niet meer kent (een 404 op de sessielijst) is
`workplace.ErrGone`. Elke subprocess-aanroep (git, systemctl, podman) loopt via een injecteerbare
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
  3. `--mcp-config <pad>`, alleen als het profiel een MCP-config heeft (absoluut pad)
  4. `--append-system-prompt-file <pad>` (absoluut pad)
  5. `--session-id <uuid>` bij `start`, `--resume <uuid>` bij `resume`
  6. `--model sonnet`, alleen op de native lane
  7. de prompt als laatste argument. Hij mag niet met `-` beginnen (claude zou hem als vlag lezen) en
     geen NUL-byte bevatten; anders weigert `Spawn`.
- **`env`:** precies `LIGHTHOUSE_CONTROLLER_RUN` (de session-id van de run) en `LIGHTHOUSE_STATE_DIR`.
- **Nooit een token.** De drager heeft er geen veld voor en `env` bevat er geen. Het forge-token blijft
  in `[dispatch.forge] token_file` en wordt per verzoek gelezen.
- **Het target verbruikt de drager** en voert precies `args` uit, niets toevoegen of weglaten.
  Verbruikt het target hem niet, dan blijft hij staan tot lighthouse hem wist. Lighthouse wist hem
  precies in deze gevallen:
  1. een niet-aangenomen spawn (`ErrNotAccepted`, zie hieronder);
  2. `CheckOpen` geeft een run terug met `start uncertain: no session found` (`Discard`), zodat een laat
     startend target niets meer start;
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
rij al `start_failed` (of door `CheckOpen` teruggegeven als "start onzeker"), zonder
`workplace_session`. Zijn key staat dus op geen enkele rij. Zo'n sessie telt als **vreemd**: bezet
zolang ze `working` is of binnen `max_wait` actief, en `ForeignLive` houdt de workspace staan. Het
opruimen stopt haar nooit.

## Launch en CheckOpen

`Launch` loopt na `runs.Take`: `Prepare`, `runs.SetWorkplace`, `Spawn`, `runs.SetSession`. De uitkomst:

- `Accepted`: de rij heeft de sessiekey.
- `NotAccepted`: de rij is gesloten als `start_failed`, met de fout van `Prepare` of `Spawn` als reden.
  Mislukt `SetWorkplace`, dan sluit `Launch` de rij ook als `start_failed` (er is niets gestart, en een
  rij zonder `workspace_id` zou nooit sluiten).
- `Uncertain`: `Launch` geeft **tegelijk een fout** terug, alleen als diagnose (de oorzaak van de twijfel).
  De rij blijft open zonder sessie. De aanroeper (#114) mag voor dit paar nooit opnieuw spawnen.

`Launch` toetst `Busy` niet zelf. De aanroeper (decide, #114) toetst `Busy` vóór elke start, ook vóór
een tweede poging.

`CheckOpen` handelt de open runs met een workspace af:

- Een run zonder sessiekey zoekt zijn `session_id` in de sessies. Een sessie die al eigen is van een
  rij van het issue (`own.Has`), telt niet: een resume-run deelt het `session_id` van de eerdere run, en
  diens sessie kan nog in de lijst staan. Gevonden: de key wordt vastgelegd. Niet gevonden na `max_wait`: de run wordt
  teruggegeven met de reden `start uncertain: no session found`, en `Discard` wist de drager. Mislukt
  `Discard`, dan meldt `CheckOpen` die fout en geeft de run toch terug.
- Een run ouder dan `max_runtime` wordt eerst gestopt (`Stop`) en dan teruggegeven met de reden
  `timeout (max_runtime)`. Lukt `Stop` niet, dan blijft de run open.

De redenen zijn Engels (code-taal onder `internal/**`). Het issue noemt de eerste "start onzeker: geen
sessie gevonden"; de vertaling naar de projecttaal hoort bij het posten in #114.

## Stop

`Stop(workspaceID, sessionIDs)` leest het `worktree_path` van de workspace en bepaalt de unit:
`paia-forge-<profiel>-<basename van het pad>`, waarbij elke byte buiten `[A-Za-z0-9_-]` een streepje
wordt (een multibyte teken geeft er dus meer). Dan:

1. `systemctl --user stop <unit>`; de exitcode telt niet (een unit die er niet is, faalt).
2. `systemctl --user is-active <unit>` beslist: alleen `inactive` of `failed` is gestopt. Alles anders is
   een fout.
3. Alleen bij het profiel `software` draait claude in de gedeelde devcontainer, buiten de cgroup van de
   unit, en forge heeft geen stop-endpoint. Dan zoekt `Stop` de containers via
   `podman ps --filter label=devcontainer.local_folder=<main checkout>` en doet per container en per
   sessie-id `pgrep -f <id>` (rc 0 draait, rc 1 weg, de rest een fout), `pkill -f <id>` en tot vijf keer
   een `pgrep` om te bevestigen.

`Stop` weigert een leeg sessie-id vóór elke aanroep: `pgrep -f ""` en `pkill -f ""` raken elk proces in
de gedeelde devcontainer, ook vreemde sessies.

## Eigen en vreemd

`runs.SessionKeys` geeft per rij van het issue (open en gesloten) met een `workplace_session` die key en
het `session_id` van de rij. `workplace.Own.Has` beslist daarmee of een sessie **eigen** is. Daarvoor
moeten twee dingen gelden:

1. haar key staat op een rij van het issue;
2. haar `session_id` bij forge is leeg, of gelijk aan het `session_id` van die rij.

Elke andere sessie is **vreemd**. `Busy`, `ForeignLive`, de stopselectie van `Cleanup` en de zoektocht van
`CheckOpen` gebruiken alle vier deze ene regel.

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
een kandidaat die nog in de controller staat of waarvoor een run open is: een lopende run wordt nooit
afgebroken. Daarna, in deze volgorde, en elke stap die de workspace moet laten staan is een
**achterblijver** (`Leftover`, in `tick_state` met soort `cleanup`, per repo en workspace):

1. Het pad. Geen absoluut pad: `not measurable: no workspace path`, zonder `Stop` en zonder `Remove`.
   `git -C ""` of een relatief pad zou de werkmap van het proces meten.
2. Sessies lezen. Kent forge de workspace niet meer (`ErrGone`, een 404), dan telt hij als verwijderd,
   net als na een 404 op `Remove`: de drager wordt gewist en het achterblijver-record verdwijnt. Lukt het
   wissen van de drager niet, dan blijft hij staan als `not measurable: discard: …`. Andere fout:
   `not measurable: sessions: …`.
3. Eigen keys lezen (`runs.SessionKeys`). Mislukt: `not measurable: session keys: …`.
4. Een niet-`done` vreemde sessie (`ForeignLive`, ook een stille): `foreign session <key>`. Een
   vreemde sessie stopt lighthouse nooit; de workspace blijft onder haar staan.
5. De eigen sessies (`own.Has`) stoppen (`Stop`, met hun `session_id`'s). `Stop` beëindigt de hele systemd-unit van
   de worktree, dus dat gebeurt pas nadat stap 4 geen vreemde sessie vond. Het gebeurt vóór de git-checks:
   een eigen agent die nog werkt, kan na de check niets meer committen. Een eigen sessie zonder
   `session_id` die niet `done` is, is niet te stoppen: `not measurable: own session <key> has no
   session id`. Mislukt `Stop`: `not measurable: stop: …`.
6. Een vuile worktree: `uncommitted changes`. Onder git-status vallen ook untracked bestanden.
7. Commits op geen enkele `origin`-ref: `unpushed commits`. Is `HEAD` gelijk aan de head-SHA van de
   gemergede PR (`MergedHead`), dan telt dat niet: elke PR wordt squash-merged en GitHub verwijdert de
   branch, dus zonder deze uitzondering bleef elke afgeronde workspace staan.
8. Een fout bij git: `not measurable: <fout>`. Een worktree die niet meetbaar is, is nooit "schoon".
9. `Remove`. Een 409 van forge is `forge refused: dirty worktree (409)`. Een andere fout wordt
   `not measurable: remove: <fout>`, en `Cleanup` geeft die fout ook terug. Een 404 telt als al weg.
   Na een geslaagde `DELETE` of een 404 wist de adapter de drager; lukt dat niet, dan is dat een fout van
   `Remove`.

Een vuile of niet-gepushte workspace heeft dus zijn eigen sessies gestopt en blijft staan. Is de
workspace verwijderd, dan verdwijnt ook zijn achterblijver-record.

## Wie roept dit aan

Dit werk levert alleen de bouwstenen. Decide en apply roepen `Busy`, `Launch`, `CheckOpen` en `Cleanup`
aan in #114 en #115. De aanroeper toetst `Busy` vóór elke start, ook vóór een tweede poging. #115 levert de
`activity` (laatste transcript-gebeurtenis per sessiekey), en #112 en #114 leveren `InController` en
`MergedHead` van de kandidaat.
