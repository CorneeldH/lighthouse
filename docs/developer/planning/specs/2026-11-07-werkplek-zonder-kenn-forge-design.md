# Ontwerp: werkplek-adapter zonder kenn-forge

Issue: [#253](https://github.com/CorneeldH/lighthouse/issues/253) · Milestone:
[Werkplek zonder kenn-forge](https://github.com/CorneeldH/lighthouse/milestone/8) · Datum: 2026-11-07 ·
Bron: [M3-spec](2026-11-07-m3-tweede-omgeving-design.md), *Nieuwe milestones*.

## Aanleiding en doel

`lighthouse tick` start runs alleen in een kenn-forge-workspace: `internal/cli/tick.go` bouwt altijd
een `kennforge.Adapter`. De tweede gebruiker van M3 heeft geen kenn-forge, en SRC (M4) ook niet. Dit
ontwerp voegt een tweede implementatie van `workplace.Workplace` toe, de **lokale adapter**: een
git-worktree op de host met een headless `claude -p`. Daarmee start `tick` runs op een gewone
Linux-server.

Succes: op een server zonder kenn-forge brengt `tick` een issue in `Controller` tot een gestarte run,
meet die run elke tick, stopt hem bij een time-out, en ruimt zijn worktree op als het issue van het
board is. Ook na een crash van `tick` zelf.

## Doelen, niet-doelen, anti-doelen

**Doelen**

- Een lokale adapter met alle acht methoden van `Workplace` en de optionele `Transcripts`.
- `host.toml` kiest de adapter; een bestaande `host.toml` werkt ongewijzigd.
- Eén gedeelde contract-suite die beide adapters haalt.
- Stoppen en opruimen zonder `systemctl` en zonder `podman`, ook na een crash van `tick`.
- `docs/workplace.md` beschrijft beide adapters.

**Niet-doelen**

- Live meekijken (`tmux attach`) en een permissie-prompt met de hand beantwoorden. Daarvoor is tmux
  nodig; dat kan later een derde adapter worden.
- Een container per run (valt af door AC 4: geen `podman`).
- Het Software-profiel met zijn devcontainer: de lokale adapter draait elke run op de host.
- De controller-knoppen van #19 (#255), `paia-forge-` naar de configuratie (#254), `host-check` (#256).
- Windows. De server is Linux.

**Anti-doelen** (zou mislukt zijn, ook als het werkt)

- Een run die na de tick sterft omdat systemd de cgroup van de tick opruimt, zonder dat iemand het ziet.
- Een proces van een run (dev-server, testrun) dat na `Stop` of `Remove` doorloopt in een worktree die
  weg is.
- Een `Kill` die een interactieve sessie van de gebruiker raakt, ook in dezelfde worktree.
- Een `Prepare` of `Remove` die commits of werk weggooit dat niet gepusht is.
- Een gedrag van `kennforge.Adapter` dat verandert door de gedeelde suite of de verhuizingen.

## Randvoorwaarden

- **Linux** met `/proc`, `git`, `setsid` (util-linux, met `-f`) en `claude` op het `PATH` van de tick.
  Dezelfde gebruiker draait `tick` en `claude`, met dezelfde `HOME`: het transcript staat onder
  `~/.claude/projects`, en de adapter leest `/proc/<pid>/environ` alleen van eigen processen.
- De `Workplace`-interface en de sentinels (`ErrNotAccepted`, `ErrUncertain`, `ErrDirty`, `ErrGone`)
  blijven zoals ze zijn. Decide, apply, `Launch`, `AdoptSessions`, `Cleanup` en `Sweep` veranderen niet.
- Alles wat de adapter van buiten nodig heeft, is injecteerbaar (AC 3). Git en de launch lopen via de
  bestaande `workplace.Runner`. Processen (zoeken, starttijd, signalen) lopen via een nieuw interface
  `Procs` (zie *Processen van een run*) *(eigen keuze na spec-review, r1-f13)*.
- Specs en documentatie in het Nederlands; code, namen en commentaar in het Engels.

## Aanpakken

Overwogen, met wat elk kost:

1. **tmux** (`lh-<session-id>`): een interactieve claude in een tmux-sessie. Je kunt meekijken met
   `attach` en een permissie-prompt beantwoorden; regel 16 en Kill-voor-resume houden hun betekenis.
   Maar zonder forge meet niemand de stand: een interactieve claude leeft na zijn antwoord door, dus er
   is een nieuwe hook nodig die `working`/`waiting`/`done` schrijft. Een interactieve claude in een
   nieuwe worktree vraagt eerst "Do you trust the files in this folder?" en blijft daar hangen, tenzij
   de adapter `~/.claude.json` vooraf bewerkt (broos). En hij heeft een pty nodig.
2. **Headless `claude -p`, losgekoppeld met `setsid -f`**: een beëindigd proces is `done`, een levend
   proces `working`. Geen nieuwe hook, geen vertrouwensvraag, geen pty. Wie wil meekijken, leest het
   transcript, of hervat de sessie na afloop met `claude --resume <sid>`.
3. **Een container per run**: valt af door AC 4 (`podman`).

**Gekozen: 2, headless `claude -p`** (de gebruiker, 2026-11-07: "claude -p voor nu"). De spike van
2026-11-07 bevestigde dat `claude -p "/ping hallo"` een project-skill uitvoert met argument.

Voor de contracttests (AC 3) waren er twee wegen: **A** één gedeelde suite in
`internal/workplace/workplacetest` die beide adapters draaien, of **B** een eigen suite voor de lokale
adapter die de gevallen van kenn-forge nabouwt. **Gekozen: A** (de gebruiker, 2026-11-07): M4 krijgt
een derde adapter, en twee suites groeien uit elkaar.

## Ontwerp

### Configuratie (`host.toml`)

```toml
[dispatch]
workplace = "local"   # "kenn-forge" (standaard) of "local"
```

- Zonder `workplace` geldt `kenn-forge`: een bestaande `host.toml` met `[dispatch.forge]` geeft
  precies de adapter van nu (test, AC 2).
- Een andere waarde stopt `tick` met een fout die de sleutel noemt, zoals elke ongeldige waarde
  onder `[dispatch]`. `workplace` komt in de lijst van bekende sleutels, zodat de check op onbekende
  sleutels hem niet weigert.
- Met `local` leest de tick `[dispatch.forge]` niet, en is `target_key` onder
  `[dispatch.profiles.<naam>]` niet nodig. `mcp_config` van het profiel geldt wel.
- `tickEnv.workplace` in `internal/cli/tick.go` kiest op `cfg.Workplace` tussen
  `kennforge.Adapter` en `local.Adapter`.

### Package en vorm

`internal/workplace/local`, type `Adapter`:

```go
type Adapter struct {
    RepoDir       string // main checkout of the repo (repoconfig.Repo.Dir)
    DefaultBranch string // [forge] default_branch
    StateDir      string // ${XDG_STATE_HOME}/lighthouse/dispatch, as now
    Run           workplace.Runner
    Procs         Procs               // nil: the /proc implementation
    Home          string              // "": os.UserHomeDir
    Sleep         func(time.Duration) // nil: time.Sleep
}
```

Op schijf, onder `StateDir/local/<repo-slug>/` (*mijn keuze*: per repo, zodat twee repo's met
dezelfde branchnaam elkaar niet raken):

- `workspaces/<id>.json`: `{id, path, branch, adopted, created_at}`. `adopted` is `true` voor een
  worktree die de adapter niet zelf maakte (zie *Prepare*).
- `sessions/<id>/<key>.json`: per launch `{key, session_id, mode, started_at}`.
- `sessions/<id>/<key>.pid`: twee getallen, de pid en de starttijd van claude (veld 22 van
  `/proc/<pid>/stat`). Het kind schrijft dit bestand zelf, vóór de `exec` naar claude; zie *Spawn*.
- `logs/<key>.log`: stdout en stderr van `claude -p`.

Elk bestand dat de adapter schrijft, atomair (tempbestand en rename), bestanden `0600`, mappen `0700`,
zoals de carrier nu.

**Workspace-ID** (*mijn keuze*): `local-<slug>-<6 hex>`, met `<slug>` de branchnaam waarin elk teken
buiten `[A-Za-z0-9._-]` een `-` wordt, en 6 willekeurige hex-tekens per aanmaak. Dat voldoet aan
`safeIDPattern`. Het ID moet per aanmaak vers zijn, zoals bij kenn-forge: `runs.Workspaces`
(`internal/runs/workspaces.go`) groepeert op `workspace_id` met `HAVING count(workspace_removed_at) = 0`.
Een nieuwe worktree op een branch die eerder is opgeruimd, zou met hetzelfde ID nooit meer in de sweep
komen. Het voorvoegsel `local-` laat de adapter een ID van kenn-forge herkennen (zie *Overstap tussen
adapters*).

**Sessiesleutel** (*mijn keuze*): `<session_id>.<8 hex>`, willekeurig per launch. Een hervatting deelt
de `session_id` maar krijgt zo een eigen sleutel; `Own` en `AdoptSessions` werken op de sleutel, zoals
bij forge. Geen volgnummer: na `Remove` of `Discard` begint dat opnieuw, en een hergebruikte sleutel
telt in `AdoptSessions` als al genomen.

**Bestaat de worktree nog?** Meerdere methoden vragen dat. Het antwoord: het pad van het record staat
in `git -C <RepoDir> worktree list --porcelain`, na `git worktree prune`. Een record waarvan het pad er
niet in staat, hoort bij een **verdwenen worktree** *(eigen keuze na spec-review, r1-f7)*. Er is
één uitzondering: zolang een sessie-record van de workspace nog processen heeft (`Marked(sid)` is
niet leeg), is de workspace niet verdwenen. Dan geeft de adapter hem gewoon terug: `Lookup` met het
pad, `Sessions` met de stand van claude. Decide stopt bij een verdwenen workspace niets
(`internal/dispatch/decide.go`: `a.Stop = !c.done() && !c.state.Gone`), en `Cleanup` roept dan
alleen `Discard` aan. Zonder deze uitzondering zou een claude met zijn dev-server doorwerken zonder
record dat hem nog vindt. Met de uitzondering ziet decide de run als levend, en stopt regel 17 of 19
hem. Pas daarna wordt de workspace `ErrGone`. Is `Marked` niet te meten, dan is dat een fout en geen
`ErrGone` *(eigen keuze na spec-review, r2-f1)*.

### Processen van een run

*(eigen keuze na spec-review, r1-f1, r1-f2, r1-f13)*

Claude Code start elk Bash-commando in een eigen sessie (`setsid`). Gemeten op deze host: claude had
`sid 1387`, zijn Bash-kind `sid 1103603`. Een sessie of procesgroep van claude bereikt dus niet wat
claude startte. Wel erft elk kind de omgeving van claude, en de carrier zet daarin
`LIGHTHOUSE_CONTROLLER_RUN=<session_id>`. Die variabele is het merk van de run.

```go
// Procs is the process side of the local adapter; tests inject a fake.
type Procs interface {
    // Marked lists the pids of this user whose environment holds
    // LIGHTHOUSE_CONTROLLER_RUN=<sid>. A process that ends during the scan,
    // or an environ that is not ours to read, is skipped; an unreadable /proc
    // is an error.
    Marked(sid string) ([]int, error)
    // StartTime is field 22 of /proc/<pid>/stat; ok is false when pid is gone.
    StartTime(pid int) (start uint64, ok bool, err error)
    // Signal sends sig to pid; a pid that is gone is no error.
    Signal(pid int, sig syscall.Signal) error
}
```

- **Claude leeft** als de pid uit het pid-bestand bestaat met dezelfde starttijd. Dat sluit een
  hergebruikte pid uit. Een `tail -f` van het transcript of een `claude --resume <sid>` van de
  gebruiker houdt de run zo niet in leven: die hebben een andere pid en geen merk.
- **De processen van een run** zijn `Marked(sid)`: claude en alles wat hij startte (Bash-commando's,
  testruns, dev-servers, MCP-servers), ook als claude zelf al weg is.
- Een interactieve sessie van de gebruiker draagt het merk niet en valt er dus buiten. Een proces dat
  zijn omgeving wist (`env -i`) of naar een andere gebruiker of container gaat, ontsnapt. Dat is de
  grens van dit ontwerp; `docs/workplace.md` noemt hem.

### Prepare(branch)

1. `git -C <RepoDir> worktree prune`, dan `git worktree list --porcelain`. Git staat maar één
   worktree per branch toe *(eigen keuze na spec-review, r1-f5)*.
   - Staat de branch in de **hoofdcheckout** (de eerste regel, `<RepoDir>` zelf): een fout,
     `branch <b> is checked out in the main checkout <RepoDir>`. De adapter draait nooit in de
     checkout van de gebruiker.
   - Staat de branch in een andere worktree: houd het workspace-record met dat pad, of schrijf er een
     met een vers ID. Heeft de adapter de worktree niet zelf gemaakt, dan krijgt het record
     `adopted: true`. Ga naar stap 3 met `created = false`.
2. Niet gevonden: `git -C <RepoDir> fetch origin`. Verwijder eerst elk record met hetzelfde pad (een
   overblijfsel van een verdwenen worktree) *(eigen keuze na spec-review, r1-f7)*. Dan, met
   `<pad>` = `<RepoDir>/.claude/worktrees/<id>` *(eigen keuze na spec-review, r1-f10: het pad volgt
   het ID, zodat `a/b` en `a-b` niet botsen)*:
   - bestaat de lokale branch al: `git worktree add <pad> <branch>`, en `created = false`
     *(eigen keuze na spec-review, r1-f4: een bestaande branch kan eigen commits hebben, en
     `created = true` zou `checkout -B` doen)*;
   - anders, bestaat `origin/<branch>`: `git worktree add <pad> -b <branch> --track origin/<branch>`,
     en `created = true`;
   - anders: `git worktree add --no-track <pad> -b <branch> origin/<DefaultBranch>`, en
     `created = true` *(eigen keuze na spec-review, r1-f11: zonder `--no-track` wordt
     `origin/<DefaultBranch>` de upstream, en weigert een kale `git push`)*.

   Schrijf het record. De plek onder `.claude/worktrees/` is *mijn keuze*: dezelfde plek als de
   handmatige worktrees uit `AGENTS.md`, zodat `/lighthouse-audit-worktrees` ze ziet.
3. Dezelfde `followRemote`-regels als `kennforge.Adapter` nu: nooit eigen commits laten vallen, alleen
   fast-forward, en een gevonden workspace op een andere branch alleen omzetten als HEAD niets eigens
   heeft. *Mijn keuze:* `followRemote` en `checkoutRemote` verhuizen naar het package `workplace`, en
   beide adapters roepen ze aan. Ze krijgen de git-timeout als parameter, en beide adapters geven
   60 s mee, zoals kenn-forge nu. De `gitTimeout` van 30 s in `internal/workplace/git.go` blijft
   voor `Dirty` en `Unpushed` *(eigen keuze na spec-review, r1-f9)*. De bestaande
   `TestPrepare…`-tests van kenn-forge bewaken dat zijn gedrag gelijk blijft.

Elke git-opdracht loopt via `Run` (`git -C <dir> …`).

### Spawn(ws, l)

1. `workplace.NewCarrier(l, StateDir, now)` valideert de `LaunchSpec` en bouwt de args. Een fout is
   `ErrNotAccepted`. De adapter gebruikt `Carrier.Args` en `Carrier.Env`, maar schrijft geen
   carrier-bestand: er is geen forge-target dat hem leest.
2. `sh -c 'command -v claude'` via `Run`. Een exitcode die niet 0 is, of een `Run`-fout, is
   `ErrNotAccepted` *(eigen keuze na spec-review, r1-f8: `command` is een ingebouwd commando van de
   shell)*. Anders zou `setsid -f` met 0 terugkeren en het kind meteen sterven.
3. Heeft `Marked(sid)` al processen, dan is dat `ErrNotAccepted` (*mijn keuze*: twee claudes op één
   sessie is de toestand van #206; `Launch` doet vóór een hervatting al een `Kill`).
4. Schrijf het sessie-record (sleutel `<sid>.<8 hex>`). Mislukt dat: `ErrNotAccepted`.
5. Start, via `Run`, één opdracht die direct terugkeert. Het losgekoppelde kind schrijft eerst zijn
   eigen pid en starttijd naar het pid-bestand, en doet dan `exec` naar `claude`. Een `exec` houdt pid
   en starttijd gelijk, dus het bestand beschrijft claude *(eigen keuze na spec-review, r1-f3)*. Een
   schets:

   ```sh
   sh -c 'cd "$1" || exit 97; log=$2; pidf=$3; shift 3
          exec setsid -f sh -c "<schrijf \$\$ en veld 22 van /proc/\$\$/stat atomair naar \"$pidf\">; exec \"\$@\"" \
            sh "$@" </dev/null >>"$log" 2>&1' \
      sh <ws.Path> <logpad> <pidpad> env LIGHTHOUSE_CONTROLLER_RUN=<sid> LIGHTHOUSE_STATE_DIR=<StateDir> \
      claude -p <Carrier.Args…>
   ```

   Het kind mag de pipes van de `Runner` niet vasthouden: stdin komt uit `/dev/null`, stdout en
   stderr gaan naar het log. Anders wacht `ExecRunner` op de pipe en wordt de launch onzeker. De
   precieze vorm van de opdracht is aan de bouwer. De eisen: losgekoppeld (een eigen `setsid`-sessie,
   geen kind van de tick), cwd de worktree, de omgeving van de tick plus de twee variabelen van de
   carrier, `-p` vóór de args van de carrier, en pid en starttijd in het pid-bestand vóór de `exec`.
6. Uitkomst:
   - exitcode 0: wacht tot het pid-bestand er is (tot 10 s, met `Sleep`). Komt het: geaccepteerd. Geef
     `Session{Key, WorkspaceID, SessionID}` terug. Komt het niet, dan `ErrUncertain`.
   - een andere exitcode (cd mislukt, `setsid` ontbreekt): `ErrNotAccepted`. Verwijder het
     sessie-record.
   - een `Run`-fout (time-out, niet te starten): wacht op het pid-bestand zoals hierboven. Komt het,
     dan is de launch geaccepteerd. Anders `ErrUncertain`, en het record blijft zonder pid-bestand.
     Nooit een tweede launch.

   Een claude die direct na de `exec` sterft (een verlopen login, een slechte `--resume`), heeft
   een pid-bestand. Hij is dus geaccepteerd, en `Sessions` ziet hem als `done`. Decide komt dan bij
   regel 10 (*nooit een assistent-beurt*, `start_failed`).

**De omgeving van de run** (de gebruiker, 2026-11-07: "erven"): de run erft de omgeving van de
tick, dus ook `GH_TOKEN`/`GITHUB_TOKEN`, met de scopes `repo` en `project`. De agent heeft een token
nodig om te pushen en een PR te maken. Bij kenn-forge geeft de forge-unit de omgeving; hier is er
geen tweede plek. Een eigen token per run (bijvoorbeeld `[dispatch] run_token_file`) is afgewogen en
valt af: het kost een tweede token voor de gebruiker. `docs/workplace.md` noemt dat de run de
rechten van de dispatcher heeft.

### Sessions(workspaceID)

- Geen workspace-record, of een verdwenen worktree: `ErrGone` *(eigen keuze na spec-review, r1-f7)*.
- Een record zonder pid-bestand komt niet in de lijst. Zo blijft een onzekere launch zonder proces
  onzichtbaar. `AdoptSessions` adopteert hem dan niet, en decide komt bij regel 15 (*start onzeker*)
  met `Discard`. Anders zou zo'n launch `done` lijken en eindigen als *geen handoff of vraag*.
- Per ander record een `Session` met `Key`, `SessionID` en `UpdatedAt` (de `started_at` van het
  record). `State` is `"working"` als claude leeft (pid en starttijd uit het pid-bestand), anders
  `"done"`. Processen die claude achterliet, maken de run niet `working`: `Stop` en `Kill` ruimen
  ze op. Kan de adapter de stand niet meten (een fout van `Procs`), dan is dat een fout voor de hele
  lijst. Niet te meten is nooit `done`.
- `waiting` komt niet voor: een `-p`-proces wacht niet op invoer.

**Vreemde sessies.** De lokale adapter kent alleen de sessies die hij zelf startte. Een interactieve
claude van de gebruiker in dezelfde worktree ziet `Busy` en `ForeignLive` dus niet. *Mijn keuze:* dat
accepteren voor worktrees die de adapter zelf maakte. Een geadopteerde worktree verwijdert de adapter
nooit; zie *Remove* *(eigen keuze na spec-review, r1-f14)*.

### Stop(workspaceID, sessionIDs) en Kill(workspaceID, sessionIDs)

*(eigen keuze na spec-review, r1-f1, r1-f2)*

Bij de lokale adapter doen beide hetzelfde: er is geen unit om te stoppen. Een lege `session_id` is
een fout vóór elke opdracht. Per gegeven `session_id`:

1. `Marked(sid)`. Leeg: klaar, geen fout. Dat geldt ook als claude zelf al weg is maar zijn
   dev-server nog draait: die heeft het merk, dus wordt hij gevonden.
2. SIGTERM naar elke gevonden pid.
3. Tot 5 keer, met 1 s ertussen, opnieuw `Marked(sid)`. Leeg: klaar.
4. Nog niet leeg: SIGKILL naar wat er over is, en nog één keer kijken (*mijn keuze*: een dev-server
   negeert SIGTERM soms). Dan nog niet leeg: een fout die de pids noemt.

Een `Marked` die niet te meten is, is een fout, nooit "weg". Kenn-forge houdt zijn `killSession` op
`pgrep -f <sid>`. Wat beide delen (de herhaallus, de foutteksten) mag de bouwer naar `workplace`
halen.

Na een crash van de tick werkt dit ongewijzigd. Het merk zit in de processen zelf, en pid-bestand en
records staan op schijf. De tick heeft geen kind-relatie nodig (AC 4).

### Remove(workspaceID)

*(eigen keuze na spec-review, r1-f6, r1-f7, r1-f14)*

1. Geen record: geen fout (zoals een 404 nu).
2. Een verdwenen worktree (zonder processen, per definitie), of een record met `adopted: true`:
   verwijder alleen de records en logs, geen fout. Een geadopteerde worktree is van de gebruiker; de
   adapter laat hem staan. Heeft een sessie-record van een geadopteerde workspace nog processen,
   dan eerst `Kill` op die sessies; mislukt dat, dan een fout en blijven de records *(eigen keuze
   na spec-review, r2-f1)*.
3. `workplace.Dirty` op het pad. Vuil (wijzigingen of untracked bestanden): `ErrDirty`, en record en
   worktree blijven. Niet te meten: een gewone fout.
4. `git -C <RepoDir> worktree remove <pad>` zonder `--force`. Git geeft exit 128 voor elke weigering
   (vuil, vergrendeld, geen worktree), en `ExecRunner` geeft de stderr dan niet door. Een exitcode die
   niet 0 is, is daarom een gewone fout met die code, geen `ErrDirty`.
5. Verwijder het workspace-record, de sessie-records, de pid-bestanden en de logs.
6. De lokale branch blijft staan (*mijn keuze*: na een squash-merge ziet `git branch -d` hem als
   niet gemerged, en `-D` zou ongepushte commits kunnen weggooien; `Cleanup` heeft ongepusht werk
   dan al uitgesloten, maar de branch kost niets).

`Cleanup` doet vóór `Remove` al zijn eigen checks en een `Stop` (`internal/workplace/lifecycle.go`).
`Remove` blijft toch zelf veilig.

### Lookup(workspaceID)

Lees het record. Geen record, of een verdwenen worktree: `ErrGone` (regel 8, *werkplek verdwenen*).
Anders `Workspace{ID, Path, Branch}`, met de branch uit `worktree list`, zodat een met de hand
omgezette branch klopt.

### Discard(workspaceID)

Vergeet een launch waarvan de agent nooit verscheen: verwijder de sessie-records van de workspace
zonder pid-bestand. Is de worktree verdwenen, dan ook het workspace-record en de andere records:
`Cleanup` roept bij `ErrGone` alleen `Discard` aan *(eigen keuze na spec-review, r1-f7)*. Niets te
vergeten is geen fout. Er is geen laat startend target, dus verder is er niets te doen.

### Transcript(workspaceID, sessionID)

Zoals het host-pad van `kennforge.Adapter`: `workplace.ReadTranscript(ProjectDir(home, path), sid)`,
met het pad uit het record. *Mijn keuze:* `hostTranscriptDir` verhuist ook naar `workplace`.

### Overstap tussen adapters

Zet iemand `workplace = "local"` op een host met runs of workspaces van kenn-forge in de database,
dan mag de lokale adapter die niet als `ErrGone` zien. Anders markeert `Cleanup` ze als verwijderd
terwijl de forge-workspace bestaat, en krijgt een open run *werkplek verdwenen*. *Mijn keuze:* elke
methode met een workspace-ID zonder voorvoegsel `local-` geeft een gewone fout:
`workspace <id> is not a local workspace`. Die fout is geen `ErrGone`. `Cleanup` houdt de workspace
dan als leftover, en de meting van de run faalt luid op stderr.

Andersom geldt hetzelfde: `kennforge.Adapter` geeft voor een ID dat met `local-` begint een gewone
fout, en vraagt forge niets *(eigen keuze na spec-review, r1-f15)*. Forge-ID's beginnen nooit zo,
dus voor kenn-forge zelf verandert er niets. De overstap zelf (runs afsluiten, workspaces opruimen)
blijft met de hand.

### Wat er aan de afloop verandert

Decide verandert niet. Wel vuren sommige regels anders, omdat `-p` anders loopt dan een interactieve
claude:

| Regel | Bij de lokale adapter |
|---|---|
| 1-3, 6, 6b, 7 (`End`, `Kill`) | Claude is meestal al weg; `Kill` ruimt op wat hij achterliet. |
| 9 `Unknown command:` | Vuurt niet. `-p` antwoordt op een onbekend commando met gewone tekst (spike 2026-11-07). De run eindigt `done` zonder PR of handoff, en regel 14 geeft *geen handoff of vraag*. |
| 10 nooit een assistent-beurt | Vangt een claude die direct na de start sterft. |
| 16 permissie-prompt | Vuurt niet. In `-p` wordt een permissie geweigerd in plaats van gevraagd; de hook `permission-check` schrijft niets. Een run die daarna vastloopt, valt onder 17. |
| 18 zonder transcript | Vuurt niet: de stand is nooit `waiting`. Een hangend proces zonder transcript valt onder 19 (`max_runtime`). |
| Kill vóór resume (#206) | Blijft; vindt meestal niets. |

`docs/tick.md` (*Afloop*) krijgt een alinea met deze tabel.

### De cgroup van de tick

Start een systemd-service de tick, dan doodt systemd na afloop alle processen in de cgroup van de
unit (`KillMode=control-group`). `setsid` ontsnapt daar niet aan: de run zou sterven met de tick.

*Mijn keuze:*

- `deploy/systemd/` krijgt `lighthouse-tick.service` (`Type=oneshot`, `KillMode=process`) en
  `lighthouse-tick.timer`. `KillMode=process` doodt alleen het hoofdproces van de tick.
- De unit krijgt **geen** hardening als `ProtectHome`, `ProtectSystem` of `PrivateTmp`, anders dan
  `deploy/systemd/lighthouse.service`. De runs erven de namespace van de tick, en moeten in de
  worktree, in `~/.claude` en in de statemap schrijven *(eigen keuze na spec-review, r1-f12)*.
- De unit en de cron-regel leveren wat tick en claude nodig hebben: `XDG_RUNTIME_DIR` (tick stopt
  zonder, `internal/cli/tick.go`), `HOME`, `GH_TOKEN` (bijvoorbeeld via `EnvironmentFile`), en een
  `PATH` met `claude` (vaak `~/.local/bin`). Cron zet `XDG_RUNTIME_DIR` niet: de cron-regel zet hem
  zelf (`/run/user/<uid>`) *(eigen keuze na spec-review, r1-f12)*.
- `docs/install.md` en `docs/tick.md` beschrijven de unit en cron.
- Een integratietest toont met `systemd-run --user --wait -p KillMode=process` dat een
  `setsid -f`-kind de unit overleeft. Zonder user-systemd slaat de test zichzelf over tijdens de
  run, zoals `stop_systemd_test.go`.

### De gedeelde contract-suite (AC 3)

Package `internal/workplace/workplacetest`, alleen voor tests:

```go
// Fixture puts one adapter into a state and hands it out. Each method arranges the world
// before the call under test; the suite never looks inside the adapter.
type Fixture interface {
    Workplace() workplace.Workplace
    Workspace(t *testing.T, branch string) workplace.Workspace // prepared, clean
    MakeDirty(t *testing.T, ws workplace.Workspace)
    Forget(t *testing.T, ws workplace.Workspace)               // the workplace no longer knows it
    Live(t *testing.T, sid string)                             // a process of sid runs
    Survivor(t *testing.T, sid string)                         // ... and outlives every signal
    Unmeasurable(t *testing.T)                                 // processes cannot be measured
    SpawnTimesOut(t *testing.T)                                // the launch outcome is unknown
    Launches() int                                             // launches sent so far
}

func Run(t *testing.T, newFixture func(t *testing.T) Fixture)
```

De vorm van `Fixture` is aan de bouwer; de gevallen niet. `Launches` telt per adapter: bij kenn-forge
de POST's naar `agent-handoffs`, bij de lokale adapter de launch-opdrachten via `Run`
*(eigen keuze na spec-review, r1-f13)*. Elk geval is een subtest en draait tegen beide adapters:

1. `Prepare` twee keer op dezelfde branch geeft hetzelfde ID en een pad.
2. `Lookup` van een bekende workspace geeft pad en branch; van een vergeten workspace `ErrGone`.
3. `Sessions` van een vergeten workspace is `ErrGone`.
4. `Spawn` met een ongeldige `LaunchSpec` (geen UUID, prompt met `-`) is `ErrNotAccepted`, en daarna
   staat er geen nieuwe sessie.
5. `Spawn` geaccepteerd: `Sessions` bevat de sleutel met de `session_id` van de launch.
6. `Spawn` onzeker is `ErrUncertain`, en `Launches` is 1, nooit 2.
7. `Kill` en `Stop` met een lege sessie-id: een fout, en er is niets gemeten of gestuurd.
8. `Kill` van een sessie zonder proces: geen fout, geen signaal.
9. `Kill` van een overlevend proces: een fout.
10. `Kill` als processen niet te meten zijn: een fout, nooit "weg".
11. `Kill` van één sessie laat een andere sessie in dezelfde workspace leven.
12. `Remove` van een vuile workspace is `ErrDirty`, en `Lookup` vindt hem daarna nog.
13. `Remove` van een vergeten workspace en `Remove` twee keer: geen fout.
14. `Discard` zonder iets te vergeten, en twee keer: geen fout.
15. Een sessie waarvan het proces eindigde, heeft stand `Done`; een levende niet.

De HTTP-specifieke tests van kenn-forge (5xx, 3xx, redirect, token, `podman`, systemd-unit) blijven in
`internal/workplace/kennforge`. Gevallen die daar nu staan en in de lijst hierboven vallen, gaan
naar de suite; de bouwer verwijdert de dubbele.

**Fixtures.** Kenn-forge: de `httptest`-server en de nep-`Runner` die zijn tests nu gebruiken. Lokaal:
een echte git-repo met een bare `origin` in `t.TempDir()`, een `Runner` die `git` echt uitvoert en de
launch naspeelt (hij schrijft het pid-bestand), en een nep-`Procs`. Gevallen die kenn-forge niet kan
naspelen (bijvoorbeeld 15 als forge geen `done` geeft), slaat zijn fixture over met een reden; de
lokale fixture slaat niets over.

Daarnaast heeft de lokale adapter eigen tests voor wat niet in het contract staat:

- de vorm van de launch-opdracht (cwd, `-p`, `</dev/null`, de twee variabelen);
- een vers ID en pad na `Remove` op dezelfde branch, en `a/b` naast `a-b`;
- een bestaande lokale branch met eigen commits die `Prepare` niet verplaatst;
- een branch in de hoofdcheckout die `Prepare` weigert, en een verdwenen worktree die `prune`
  opruimt;
- een launch zonder pid-bestand die niet in `Sessions` staat;
- een pid-bestand met een andere starttijd dat `done` geeft;
- een `Remove` op een verdwenen of geadopteerde worktree, en een exit 128 die geen `ErrDirty` is;
- een weggehaalde worktree met een levende run: `Lookup` en `Sessions` geven hem terug (`working`),
  `Discard` laat zijn records staan, en pas na `Kill` is hij `ErrGone`;
- een ID zonder `local-` dat geen `ErrGone` is, en in `kennforge` een `local-`-ID dat geen
  `ErrGone` is;
- het herlezen van de records na een "crash": een nieuwe `Adapter` op dezelfde `StateDir`.

En één integratietest met echte processen, die zichzelf overslaat zonder `setsid`. Die start
via de echte `/proc`-`Procs` een nep-claude (een shell met het merk in zijn omgeving). De nep-claude
start zelf met `setsid` een kleinkind en eindigt dan. `Kill` moet daarna ook dat kleinkind beëindigen
*(eigen keuze na spec-review, r1-f1, r1-f2)*.

### Documentatie

- `docs/workplace.md`: een kop per adapter. Wat nu over kenn-forge staat, gaat onder *kenn-forge*; een
  nieuwe kop *Lokaal* beschrijft de acht methoden, de bestanden onder de statemap, het merk en zijn
  grens, de omgeving van de run (met de rechten van de dispatcher) en de afloopverschillen.
- `docs/tick.md`: `[dispatch] workplace`, de afloopverschillen (zie de tabel hierboven) en de cgroup.
- `docs/install.md`: de tick-unit en -timer met `KillMode=process`, zonder hardening, en cron.
- `changelog.d/`: een fragment.

## Wat bij de bouwer blijft

- De precieze shell-vorm van de launch-opdracht, binnen de eisen van *Spawn* stap 5.
- De vorm van `Fixture` en `Procs`, zolang de gevallen en het merk blijven.
- De volgorde van de verhuizingen naar `workplace` (`followRemote`, `hostTranscriptDir`), zolang
  kenn-forge na elke stap groen blijft.
- Of de tests van kenn-forge die in de suite opgaan, in dezelfde commit verdwijnen of erna.

## Open punten

- **Live meekijken.** Valt buiten dit ontwerp. Wil de tweede gebruiker het, dan is tmux een derde
  adapter, die dezelfde suite haalt.
