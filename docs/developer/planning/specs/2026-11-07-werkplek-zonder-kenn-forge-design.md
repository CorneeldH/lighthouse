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
- Een `Kill` die een interactieve sessie van de gebruiker in dezelfde worktree raakt.
- Een `Remove` die werk weggooit dat niet gecommit of niet gepusht is.
- Een gedrag van `kennforge.Adapter` dat verandert door de gedeelde suite.

## Randvoorwaarden

- **Linux** met `git`, `setsid` (util-linux, met `-f`), `pgrep` en `pkill` (procps), en `claude` op het
  `PATH` van de tick. Dezelfde gebruiker draait `tick` en `claude`, met dezelfde `HOME`: het transcript
  staat onder `~/.claude/projects`.
- De `Workplace`-interface en de sentinels (`ErrNotAccepted`, `ErrUncertain`, `ErrDirty`, `ErrGone`)
  blijven zoals ze zijn. Decide, apply, `Launch`, `AdoptSessions`, `Cleanup` en `Sweep` veranderen niet.
- Alles wat de adapter van buiten nodig heeft, loopt via de bestaande `workplace.Runner`, zodat de
  tests het kunnen injecteren (AC 3). Ook de launch.
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
    Home          string              // "": os.UserHomeDir
    Sleep         func(time.Duration) // nil: time.Sleep
}
```

Op schijf, onder `StateDir/local/<repo-slug>/` (*mijn keuze*: per repo, zodat twee repo's met
dezelfde branchnaam elkaar niet raken):

- `workspaces/<id>.json`: `{id, path, branch, created_at}`.
- `sessions/<id>/<key>.json`: per launch `{key, session_id, mode, started_at, pid, pid_start}`.
  `pid` en `pid_start` (de starttijd van het proces, uit `/proc/<pid>/stat` of `ps -o lstart=`) staan
  er pas in als `Spawn` het proces zag; zie *Spawn*.
- `logs/<key>.log`: stdout en stderr van `claude -p`.

Elk bestand atomair (tempbestand en rename), bestanden `0600`, mappen `0700`, zoals de carrier nu.

**Workspace-ID** (*mijn keuze*): `local-<slug>-<6 hex>`, met `<slug>` de branchnaam waarin elk teken
buiten `[A-Za-z0-9._-]` een `-` wordt, en 6 willekeurige hex-tekens per aanmaak. Dat voldoet aan
`safeIDPattern`. Het ID moet per aanmaak vers zijn, zoals bij kenn-forge: `runs.Workspaces`
(`internal/runs/workspaces.go`) groepeert op `workspace_id` met `HAVING count(workspace_removed_at) = 0`.
Een nieuwe worktree op een branch die eerder is opgeruimd, zou met hetzelfde ID nooit meer in de sweep
komen. Het voorvoegsel `local-` laat de adapter een ID van kenn-forge herkennen (zie *Overstap van
kenn-forge*).

**Sessiesleutel** (*mijn keuze*): `<session_id>.<8 hex>`, willekeurig per launch. Een hervatting deelt
de `session_id` maar krijgt zo een eigen sleutel; `Own` en `AdoptSessions` werken op de sleutel, zoals
bij forge. Geen volgnummer: na `Remove` of `Discard` begint dat opnieuw, en een hergebruikte sleutel
telt in `AdoptSessions` als al genomen.

### Prepare(branch)

1. Zoek een bestaande worktree op de branch met `git -C <RepoDir> worktree list --porcelain`. Git
   staat maar één worktree per branch toe, dus ook een worktree die de gebruiker zelf maakte, telt.
   Gevonden: houd het workspace-record met dat pad, of schrijf er een met een vers ID, en ga naar
   stap 3 met `created = false`.
2. Niet gevonden: `git -C <RepoDir> fetch origin`, dan
   - bestaat `origin/<branch>`: `git worktree add <pad> -b <branch> --track origin/<branch>`;
   - anders: `git worktree add <pad> -b <branch> origin/<DefaultBranch>`.

   `<pad>` is `<RepoDir>/.claude/worktrees/<slug>` (*mijn keuze*: dezelfde plek als de handmatige
   worktrees uit `AGENTS.md`, zodat `/lighthouse-audit-worktrees` ze ziet). Bestaat de lokale branch
   al zonder worktree, dan `git worktree add <pad> <branch>` zonder `-b`, en daarna stap 3. Schrijf
   het record, `created = true`.
3. Dezelfde `followRemote`-regels als `kennforge.Adapter` nu: nooit eigen commits laten vallen, alleen
   fast-forward, en een gevonden workspace op een andere branch alleen omzetten als HEAD niets eigens
   heeft. *Mijn keuze:* `followRemote` en `checkoutRemote` verhuizen naar het package `workplace`
   (naast `git.go`), en beide adapters roepen ze aan. Het gedrag van kenn-forge blijft gelijk; zijn
   bestaande `TestPrepare…`-tests bewaken dat.

Elke git-opdracht loopt via `Run` (`git -C <dir> …`), met de time-out van nu (60 s).

### Spawn(ws, l)

1. `workplace.NewCarrier(l, StateDir, now)` valideert de `LaunchSpec` en bouwt de args. Een fout is
   `ErrNotAccepted`. De adapter gebruikt `Carrier.Args` en `Carrier.Env`, maar schrijft geen
   carrier-bestand: er is geen forge-target dat hem leest.
2. Is `claude` niet te vinden (`command -v claude` via `Run`), dan `ErrNotAccepted`. Anders zou
   `setsid -f` met 0 terugkeren en het kind meteen sterven.
3. Lees de sessies van de workspace. Leeft er al een run-proces met deze `session_id`, dan is dat
   `ErrNotAccepted` (*mijn keuze*: twee claudes op één sessie is de toestand van #206).
4. Schrijf het sessie-record (sleutel `<sid>.<8 hex>`, nog zonder `pid`). Mislukt dat:
   `ErrNotAccepted`.
5. Start, via `Run`, één opdracht die direct terugkeert. Het losgekoppelde kind schrijft eerst zijn
   eigen pid naar een pid-bestand naast het record, en doet dan `exec` naar `claude`. Zo is die pid
   de pid van claude, en ook het nummer van de nieuwe `setsid`-sessie. Een schets:

   ```sh
   sh -c 'cd "$1" || exit 97; log=$2; pidf=$3; shift 3
          exec setsid -f sh -c "echo \$\$ >\"$pidf\"; exec \"\$@\"" sh "$@" </dev/null >>"$log" 2>&1' \
      sh <ws.Path> <logpad> <pidpad> env LIGHTHOUSE_CONTROLLER_RUN=<sid> LIGHTHOUSE_STATE_DIR=<StateDir> \
      claude -p <Carrier.Args…>
   ```

   Het kind mag de pipes van de `Runner` niet vasthouden: stdin komt uit `/dev/null`, stdout en
   stderr gaan naar het log. Anders wacht `ExecRunner` op de pipe en wordt de launch onzeker. De
   precieze vorm van de opdracht is aan de bouwer. De eisen: losgekoppeld (een eigen `setsid`-sessie,
   geen kind van de tick), cwd de worktree, de omgeving van de tick plus de twee variabelen van de
   carrier, `-p` vóór de args van de carrier, en de pid van claude in het pid-bestand.
6. Uitkomst:
   - exitcode 0: wacht tot het pid-bestand er is (tot 10 s, met `Sleep`). Lees de starttijd van die
     pid en schrijf `pid` en `pid_start` in het record. Dan is de launch geaccepteerd: geef
     `Session{Key, WorkspaceID, SessionID}` terug. Komt het pid-bestand niet, dan `ErrUncertain`.
   - een andere exitcode (cd mislukt, `setsid` ontbreekt): `ErrNotAccepted`. Verwijder het
     sessie-record.
   - een `Run`-fout (time-out, niet te starten): wacht op het pid-bestand zoals hierboven. Komt het,
     dan is de launch geaccepteerd. Anders `ErrUncertain`, en het record blijft zonder `pid`.
     Nooit een tweede launch.

Een record zonder `pid` is een launch die niemand zag. `Sessions` laat het weg; zie daar.

**De omgeving van de run** (de gebruiker, 2026-11-07: "erven"): de run erft de omgeving van de
tick, dus ook `GH_TOKEN`/`GITHUB_TOKEN`, met de scopes `repo` en `project`. De agent heeft een token
nodig om te pushen en een PR te maken. Bij kenn-forge geeft de forge-unit de omgeving; hier is er
geen tweede plek. Een eigen token per run (bijvoorbeeld `[dispatch] run_token_file`) is afgewogen en
valt af: het kost een tweede token voor de gebruiker. `docs/workplace.md` noemt dat de run de
rechten van de dispatcher heeft.

### Sessions(workspaceID)

- Geen workspace-record: `ErrGone`.
- Een record zonder `pid` komt niet in de lijst, zolang het pid-bestand ontbreekt. Verschijnt het
  pid-bestand later alsnog, dan vult `Sessions` het record aan. Zo blijft een onzekere launch
  zonder proces onzichtbaar. `AdoptSessions` adopteert hem dan niet, en decide komt bij regel 15
  (*start onzeker*) met `Discard`. Anders zou zo'n launch `done` lijken en eindigen als
  *geen handoff of vraag*.
- Per ander record een `Session` met `Key`, `SessionID` en `UpdatedAt` (de `started_at` van het
  record). `State` is `"working"` zolang het proces `pid` leeft met dezelfde `pid_start`, en anders
  `"done"`. De starttijd sluit een hergebruikte pid uit. Het proces is herkend aan zijn pid, niet
  aan `pgrep -f <sid>`. Een `tail -f` van het transcript of een `claude --resume <sid>` van de
  gebruiker draagt ook de sid, maar houdt de run zo niet in leven. Kan de adapter de stand niet
  meten (een fout van `Run`, `/proc` onleesbaar), dan is dat een fout voor de hele lijst. Niet te
  meten is nooit `done`.
- `waiting` komt niet voor: een `-p`-proces wacht niet op invoer.

**Vreemde sessies.** De lokale adapter kent alleen de sessies die hij zelf startte. Een interactieve
claude van de gebruiker in dezelfde worktree ziet `Busy` en `ForeignLive` dus niet. *Mijn keuze:* dat
accepteren. `Cleanup` controleert daarna nog vuil en ongepusht werk, en `Kill` werkt op
sessie-id, dus raakt die sessie nooit.

### Stop(workspaceID, sessionIDs) en Kill(workspaceID, sessionIDs)

Bij de lokale adapter doen beide hetzelfde: er is geen unit om te stoppen. Een lege `session_id` is
een fout vóór elke opdracht. Per gegeven `session_id` beëindigt de adapter de `setsid`-sessie van elk
record met die `session_id` en een `pid`. Dat is claude **met alles wat hij startte**: Bash-commando's,
testruns, dev-servers en MCP-servers. Die dragen de sid niet in hun opdrachtregel, maar zitten wel in
de sessie. Zo staat er bij `Remove` niets meer te draaien in de worktree. Zoals bij kenn-forge's
`Stop` blijft niets van de run over.

De stappen zijn `pgrep -s <pid>`, dan `pkill -s <pid>`, dan tot 5 keer met 1 s ertussen kijken of
de sessie leeg is. Daarna, als de sessie na 5 s nog niet leeg is, één keer `pkill -KILL -s <pid>` en
nog eens kijken (*mijn keuze*: een dev-server negeert SIGTERM soms). Een lege sessie is geen fout,
een overlevend proces wel, en een `pgrep` die niet te meten is ook. Leeft claude zelf niet meer
(andere `pid_start`, of geen proces), dan kan het sessienummer van een ander zijn. De adapter
stuurt dan geen signaal aan de sessie.

Een interactieve sessie van de gebruiker, ook `claude --resume <sid>`, zit in een eigen sessie en
blijft dus buiten schot.

*Mijn keuze:* kenn-forge houdt zijn `killSession` op `pgrep -f <sid>`. De lokale adapter krijgt een
eigen variant op de sessie. Wat ze delen (de herhaallus, de foutteksten) mag de bouwer naar
`workplace` halen.

Na een crash van de tick werkt dit ongewijzigd. De tick vindt het proces via pid en starttijd in het
record op schijf, niet via een kind-relatie (AC 4).

### Remove(workspaceID)

1. Geen record en geen worktree: geen fout (zoals een 404 nu).
2. `git -C <RepoDir> worktree remove <pad>` zonder `--force`. Git weigert een worktree met
   wijzigingen of untracked bestanden: dat is `ErrDirty`, en record en worktree blijven.
3. Daarna `git worktree prune`, en verwijder het workspace-record, de sessie-records en de logs.
4. De lokale branch blijft staan (*mijn keuze*: na een squash-merge ziet `git branch -d` hem als
   niet gemerged, en `-D` zou ongepushte commits kunnen weggooien; `Cleanup` heeft ongepusht werk
   dan al uitgesloten, maar de branch kost niets).

`Cleanup` doet vóór `Remove` al zijn eigen checks (vreemde sessie, `Dirty`, `Unpushed`). `Remove`
blijft toch zelf veilig, omdat git het weigert.

### Lookup(workspaceID)

Lees het record. Geen record, of een pad dat `git worktree list --porcelain` niet meer kent: `ErrGone`
(regel 8, *werkplek verdwenen*). Anders `Workspace{ID, Path, Branch}`, met de branch uit
`worktree list`, zodat een met de hand omgezette branch klopt.

### Discard(workspaceID)

Vergeet een launch waarvan de agent nooit verscheen: verwijder de sessie-records van de workspace
zonder `pid` en zonder pid-bestand. Niets te vergeten is geen fout. Een record met een `pid` blijft.
Er is geen laat startend target, dus verder is er niets te doen.

### Overstap van kenn-forge

Zet iemand `workplace = "local"` op een host met runs of workspaces van kenn-forge in de database,
dan mag de lokale adapter die niet als `ErrGone` zien. Anders markeert `Cleanup` ze als verwijderd
terwijl de forge-workspace bestaat, en krijgt een open run *werkplek verdwenen*. *Mijn keuze:* elke
methode met een workspace-ID zonder voorvoegsel `local-` geeft een gewone fout:
`workspace <id> is not a local workspace`. Die fout is geen `ErrGone`. `Cleanup` houdt de workspace
dan als leftover, en de meting van de run faalt luid op stderr. De overstap zelf (forge-runs afsluiten,
workspaces opruimen) blijft met de hand.

### Transcript(workspaceID, sessionID)

Zoals het host-pad van `kennforge.Adapter`: `workplace.ReadTranscript(ProjectDir(home, path), sid)`,
met het pad uit het record. *Mijn keuze:* `hostTranscriptDir` verhuist ook naar `workplace`.

### Wat er aan de afloop verandert

Decide verandert niet. Wel vuren sommige regels anders, omdat `-p` anders loopt dan een interactieve
claude:

| Regel | Bij de lokale adapter |
|---|---|
| 1-3, 6, 6b, 7 (`End`, `Kill`) | Het proces is meestal al weg; `Kill` vindt niets en is geen fout. |
| 9 `Unknown command:` | Vuurt niet. `-p` antwoordt op een onbekend commando met gewone tekst (spike 2026-11-07). De run eindigt `done` zonder PR of handoff, en regel 14 geeft *geen handoff of vraag*. |
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
- `docs/install.md` en `docs/tick.md` beschrijven dat, en cron als alternatief.
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
    Live(t *testing.T, sid string)                             // a process with sid runs
    Survivor(t *testing.T, sid string)                         // ... and outlives pkill
    Unmeasurable(t *testing.T)                                 // pgrep cannot run
    SpawnTimesOut(t *testing.T)                                // the launch outcome is unknown
    Commands() []string                                        // what the Runner was asked, in order
}

func Run(t *testing.T, newFixture func(t *testing.T) Fixture)
```

De vorm van `Fixture` is aan de bouwer; de gevallen niet. Elk geval is een subtest. Elk geval draait
tegen beide adapters:

1. `Prepare` twee keer op dezelfde branch geeft hetzelfde ID en een pad.
2. `Lookup` van een bekende workspace geeft pad en branch; van een vergeten workspace `ErrGone`.
3. `Sessions` van een vergeten workspace is `ErrGone`.
4. `Spawn` met een ongeldige `LaunchSpec` (geen UUID, prompt met `-`) is `ErrNotAccepted`, en daarna
   staat er geen nieuwe sessie.
5. `Spawn` geaccepteerd: `Sessions` bevat de sleutel met de `session_id` van de launch.
6. `Spawn` onzeker is `ErrUncertain`, en de `Runner` kreeg één launch, nooit twee.
7. `Kill` en `Stop` met een lege sessie-id: een fout, en de `Runner` kreeg geen opdracht.
8. `Kill` van een sessie zonder proces: geen fout, geen signaal.
9. `Kill` van een overlevend proces: een fout.
10. `Kill` als `pgrep` niet te meten is: een fout, nooit "weg".
11. `Kill` van één sessie laat een andere sessie in dezelfde workspace leven.
12. `Remove` van een vuile workspace is `ErrDirty`, en `Lookup` vindt hem daarna nog.
13. `Remove` van een vergeten workspace en `Remove` twee keer: geen fout.
14. `Discard` zonder iets te vergeten, en twee keer: geen fout.
15. Een sessie waarvan het proces eindigde, heeft stand `Done`; een levende niet.

De HTTP-specifieke tests van kenn-forge (5xx, 3xx, redirect, token, `podman`, systemd-unit) blijven in
`internal/workplace/kennforge`. Gevallen die daar nu staan en in de lijst hierboven vallen, gaan
naar de suite; de bouwer verwijdert de dubbele.

**Fixtures.** Kenn-forge: de `httptest`-server en de nep-`Runner` die zijn tests nu gebruiken. Lokaal:
een echte git-repo met een bare `origin` in `t.TempDir()`, en een `Runner` die `git` echt uitvoert en
`setsid`, `pgrep` en `pkill` naspeelt. Gevallen die kenn-forge niet kan naspelen (bijvoorbeeld 15 als
forge geen `done` geeft), slaat zijn fixture over met een reden; de lokale fixture slaat niets over.

Daarnaast heeft de lokale adapter eigen tests voor wat niet in het contract staat: de vorm van de
launch-opdracht (cwd, `-p`, `</dev/null`, de twee variabelen), het ID-conflict van `a/b` en `a-b`, een
bestaande lokale branch zonder worktree, een vers ID na `Remove` op dezelfde branch, een launch
zonder pid-bestand die niet in `Sessions` staat, een `tail -f <sid>.jsonl` die de stand niet
`working` houdt, een `Kill` die ook een kleinkind in de sessie beëindigt, een ID zonder `local-`
dat geen `ErrGone` is, en het herlezen van de records na een "crash" (een nieuwe `Adapter` op
dezelfde `StateDir`).

### Documentatie

- `docs/workplace.md`: een kop per adapter. Wat nu over kenn-forge staat, gaat onder *kenn-forge*; een
  nieuwe kop *Lokaal* beschrijft de acht methoden, de bestanden onder de statemap, de omgeving van de
  run en de afloopverschillen.
- `docs/tick.md`: `[dispatch] workplace`, de afloopverschillen (zie de tabel hierboven) en de cgroup.
- `docs/install.md`: de tick-unit en -timer met `KillMode=process`, en cron.
- `changelog.d/`: een fragment.

## Wat bij de bouwer blijft

- De precieze shell-vorm van de launch-opdracht, binnen de eisen van *Spawn* stap 4.
- De vorm van `Fixture` en hoe de lokale fixture `setsid`/`pgrep`/`pkill` naspeelt.
- De volgorde van de verhuizingen naar `workplace` (`followRemote`, `killSession`,
  `hostTranscriptDir`), zolang kenn-forge na elke stap groen blijft.
- Of de tests van kenn-forge die in de suite opgaan, in dezelfde commit verdwijnen of erna.

## Open punten

- **Live meekijken.** Valt buiten dit ontwerp. Wil de tweede gebruiker het, dan is tmux een derde
  adapter, die dezelfde suite haalt.
