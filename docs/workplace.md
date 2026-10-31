# Werkplek

De werkplek is de kant van de dispatcher die met kenn-forge praat: een workspace klaarzetten, daar
een agent in starten, de sessies lezen, alles stoppen en de workspace opruimen. De code staat in
`internal/workplace` (interface, regels, levenscyclus) en `internal/workplace/kennforge` (de
HTTP-adapter). Het ontwerp staat in de [dispatcher-spec](developer/planning/specs/2026-10-27-m2-dispatcher-design.md)
(§6); de tick zelf staat in [tick.md](tick.md).

## De interface

`workplace.Workplace` heeft vijf methoden: `Prepare(branch)` geeft een klare workspace,
`Spawn(ws, launch)` start de agent en geeft zijn sessie, `Sessions(workspaceID)` leest de
agentsessies, `Stop(workspaceID, sessionIDs)` beëindigt de processen en `Remove(workspaceID)` verwijdert
de workspace. Elke subprocess-aanroep (git, systemctl, podman) loopt via een injecteerbare
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
  als `HEAD` een voorouder is van `origin/<branch>`.
- Staat de workspace op een andere branch (bijvoorbeeld `main`), dan geldt de rev-list-regel: pas als
  `HEAD` geen commit heeft die op geen enkele `origin`-ref staat, schakelt `Prepare` om. Een
  fast-forward zou daar die andere branch verplaatsen; lokale `main` gaat dus nooit mee.

## Het dragercontract (paia#1661)

`Spawn` schrijft vóór de handoff een drager: een JSON-bestand dat het forge-target leest om de agent te
starten. Het contract:

- **Pad:** `<statemap>/carriers/<workspace-id>.json`. Het workspace-id moet een veilige bestandsnaam zijn
  (`[A-Za-z0-9][A-Za-z0-9._-]*`); anders weigert `Spawn`.
- **Rechten:** mode `0600`, de map `0700`. Het bestand wordt atomair geschreven (tempbestand en rename).
- **Velden:** `version` (nu `1`), `repo`, `issue`, `session_id` (een UUID v4 in kleine letters), `mode`
  (`start` of `resume`), `profile`, `args`, `env`, `created_at` (RFC 3339, UTC).
- **`args`**, in deze volgorde:
  1. `--permission-mode auto`
  2. `--strict-mcp-config` (staat in elke run)
  3. `--mcp-config <pad>`, alleen als het profiel een MCP-config heeft (absoluut pad)
  4. `--append-system-prompt-file <pad>` (absoluut pad)
  5. `--session-id <uuid>` bij `start`, `--resume <uuid>` bij `resume`
  6. `--model sonnet`, alleen op de native lane
  7. de prompt als laatste argument
- **`env`:** precies `LIGHTHOUSE_CONTROLLER_RUN` (de session-id van de run) en `LIGHTHOUSE_STATE_DIR`.
- **Nooit een token.** De drager heeft er geen veld voor en `env` bevat er geen. Het forge-token blijft
  in `[dispatch.forge] token_file` en wordt per verzoek gelezen.
- **Het target verbruikt de drager** en voert precies `args` uit, niets toevoegen of weglaten.
  Verbruikt het target hem niet, dan blijft hij staan; lighthouse wist hem alleen bij een niet-aangenomen
  spawn (zie hieronder) of als het zelf opruimt.

## Spawn: drie uitkomsten

`Spawn` stuurt `POST /api/v1/workspaces/{id}/runtime/agent-handoffs` met `{"target_key", "message"}` en
verstuurt nooit een tweede POST. Er zijn drie uitkomsten:

1. **Aangenomen.** Het antwoord heeft een `session.key`. `Spawn` geeft de sessie terug.
2. **Niet aangenomen** (`workplace.ErrNotAccepted`). Er draait geen agent en de drager is gewist. Dat
   geldt voor: geen target voor het profiel, een ongeldige drager, een mislukte schrijfactie, een
   mislukte sessielezing vóór de POST, een 4xx of 3xx (een redirect wordt nooit gevolgd), en een 5xx
   waarna geen nieuwe sessie verschijnt.
3. **Onzeker** (`workplace.ErrUncertain`). Een time-out (30 s) of verbroken verbinding: er kan een agent
   draaien. De drager blijft staan. Nooit een tweede spawn.

**Het 5xx-besluit.** Spec §6 zegt dat een 5xx "niet aangenomen" is. Maar de agent kan gestart zijn vóór
de 5xx. Daarom leest `Spawn` na een 5xx de sessies één keer opnieuw. Staat er een nieuwe sessie (een key
die vóór de POST niet bestond) met het `session_id` van de run, of nog zonder `session_id`, dan is de
spawn aangenomen. Anders is hij niet aangenomen en wist de adapter de drager, zodat een laat startend
target hem niet meer verbruikt. Ook bij een time-out leest `Spawn` één keer opnieuw; daar telt elke nieuwe
key, maar een sessie van deze run wint.

**Een laat startende agent.** Verschijnt de agent van een eerdere poging pas na de herlezing, dan is zijn
rij al `start_failed` (of door `CheckOpen` teruggegeven als "start onzeker"), zonder
`workplace_session`. `runs.SessionIDs` laat elke gesloten run zonder `workplace_session` weg, niet alleen
`start_failed`. Zo'n sessie telt dus als **vreemd**: bezet zolang ze `working` is of binnen `max_wait`
actief, en `ForeignLive` houdt de workspace staan. Het opruimen stopt haar nooit.

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

- Een run zonder sessiekey zoekt zijn `session_id` in de sessies. Gevonden: de key wordt vastgelegd.
  Niet gevonden na `max_wait`: de run wordt teruggegeven met de reden `start uncertain: no session found`.
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

## Bezet (#31)

`workplace.Busy(sessions, own, open, activity, now, maxWait)` geeft de eerste sessie die de werkplek
bezet maakt:

- Een sessie van een **open** run is bezet zolang ze niet `done` is.
- Een sessie van een **gesloten eigen** run telt niet: dat is geen reden om te wachten.
- Een **vreemde** sessie (ook een laat startende, zie hierboven) is bezet als ze `working` is, of
  `waiting` en haar laatste activiteit onbekend of jonger dan `max_wait` is. De activiteit komt uit het
  transcript van de sessie en valt terug op `updated_at` van forge. `done` is nooit bezet.

`BusyReason` geeft de tekst voor de gebruiker. De reden bestaat omdat een tweede agent in dezelfde
worktree het werk van de eerste stuk kan maken.

## Opruimen (#85)

`Cleanup(candidate)` verwijdert een workspace alleen als er niets mee verloren gaat. Het doet niets bij
een kandidaat die nog in de controller staat of waarvoor een run open is: een lopende run wordt nooit
afgebroken. Daarna, in deze volgorde, en elke stap die de workspace moet laten staan is een
**achterblijver** (`Leftover`, in `tick_state` met soort `cleanup`, per repo en workspace):

1. Sessies lezen. Mislukt: `not measurable: sessions: …`.
2. Eigen sessie-ids lezen (`runs.SessionIDs`). Mislukt: `not measurable: session ids: …`.
3. Een niet-`done` vreemde sessie (`ForeignLive`, ook een stille): `foreign session <key>`. Een
   vreemde sessie stopt lighthouse nooit; de workspace blijft onder haar staan.
4. Een vuile worktree: `uncommitted changes`. Onder git-status vallen ook untracked bestanden.
5. Commits op geen enkele `origin`-ref: `unpushed commits`. Is `HEAD` gelijk aan de head-SHA van de
   gemergede PR (`MergedHead`), dan telt dat niet: elke PR wordt squash-merged en GitHub verwijdert de
   branch, dus zonder deze uitzondering bleef elke afgeronde workspace staan.
6. Een fout bij git: `not measurable: <fout>`. Een worktree die niet meetbaar is, is nooit "schoon".
7. De sessies van eigen runs stoppen (`Stop`). Mislukt: `not measurable: stop: …`.
8. `Remove`. Een 409 van forge is `forge refused: dirty worktree (409)`. Een andere fout wordt
   `not measurable: remove: <fout>`, en `Cleanup` geeft die fout ook terug. Een 404 telt als al weg.

Is de workspace verwijderd, dan verdwijnt ook zijn achterblijver-record.

## Wie roept dit aan

Dit werk levert alleen de bouwstenen. Decide en apply roepen `Busy`, `Launch`, `CheckOpen` en `Cleanup`
aan in #114 en #115. De aanroeper toetst `Busy` vóór elke start, ook vóór een tweede poging. #115 levert de
`activity` (laatste transcript-gebeurtenis per sessiekey), en #112 en #114 leveren `InController` en
`MergedHead` van de kandidaat.
