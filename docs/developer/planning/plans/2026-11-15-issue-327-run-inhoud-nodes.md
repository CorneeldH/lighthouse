# Run-inhoud op een andere kenn-forge-node — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** Een run op een andere kenn-forge-node gedraagt zich voor lighthouse als een run op `self`: dezelfde drager met `--session-id`, dezelfde transcript- en permissiemeting, dezelfde `FollowRemote`, dezelfde git-metingen bij het opruimen, werkende `Stop`/`Kill`, `Relocate` en usage-rem.

**Architectuur:** Naast de hub (levenscyclus) krijgt een node een tweede kanaal: `lighthouse serve` op de node biedt benoemde, met een token beschermde routes onder `/api/node/v1/`. De kenn-forge-adapter met `Node != ""` roept ze aan als de adapter met `Node == ""` lokaal zou handelen; `serve` voert dat uit met een gewone adapter voor zijn eigen forge. Meten gebeurt op de tick met dezelfde code als op `self` (`ReadTranscript` op een spiegel, `ParsePermission`); alleen de git-metingen draaien op de node.

**Tech stack:** Go (stdlib, huma, cobra, `net/http/httptest`), `internal/workplace/workplacetest`, `internal/fixturesetup`-stijl tempmap-repo's.

**Spec:** [`2026-11-15-issue-327-run-inhoud-nodes-design.md`](../specs/2026-11-15-issue-327-run-inhoud-nodes-design.md). Bouwt op [`2026-11-13-issue-187-forge-nodes-design.md`](../specs/2026-11-13-issue-187-forge-nodes-design.md). Issue #327.

## Globale eisen

- Alle routes onder `/api/node/v1/`; `Authorization: Bearer <token>`, vergeleken met `subtle.ConstantTimeCompare`. Zonder header 401, verkeerd token 403, zonder `[node]` in host.toml 404.
- Node-API-versie `1` (`GET info`), eenmaal per tick en per node opgevraagd; een andere versie is een fout met beide versies erin, nooit een stille terugval.
- Nieuwe sleutels per node: `serve_url` (`https`, of `http` naar loopback) en `serve_token_file` (absoluut); beide verplicht. Op de node: `[node] token_file`. Het token wordt per verzoek gelezen; ontbrekend of leeg is een fout van dat verzoek.
- Het pad komt altijd van de forge van de node (`GET /api/v1/workspaces/{id}` → `worktree_path`), na `filepath.EvalSymlinks` gelijk aan `git -C <pad> rev-parse --show-toplevel`. De tick stuurt nooit een pad.
- Veilige namen: workspace-id `safeIDPattern`, sessie-id `safeSession` (carrier: `uuidPattern`), `merged_head` leeg of `^[0-9a-f]{40}$`; anders 400 vóór elke handeling. Profiel: leeg of onbekend → 400 (routes `transcript`, `stop`, `kill`).
- Onbereikbaar of 5xx van `serve` is "niet meetbaar", nooit "weg", "stil" of "schoon". 401/403 is een configuratiefout: gelogd met de node, de run niet meetbaar.
- 404-codes van `serve` dragen een machineleesbare `code`: `workspace_gone`, `no_transcript`; de tick beeldt daarop af, nooit op de status alleen.
- Geen hostnaam, node-ID of machine-opzet in code of fixtures; tests gebruiken minstens twee nodes met willekeurige id's.
- Niets verandert voor `self`: een lege `unit_prefix` blijft een fout van `Stop` op de adapter met `Node == ""`.
- Go volgt `/lighthouse-go-standard`, tests `/lighthouse-test-standard`; alles compileert met `GOOS=darwin` en `GOOS=windows`. Conventional Commits in het Nederlands. Testgate (`[test_gate] command`) na elke commit groen; rood los je op met een nieuwe commit, nooit `--no-verify`. Instructietekst van skills blijft Engels; artefacten zijn Nederlands.

## Review-focus

Wat de spec impliceert maar geen taak-test vanzelf dekt; elke regel heeft een test in de genoemde taak.

1. **Pad met symlink of buiten de worktree** (macOS `/var` → `/private/var`; een `worktree_path` dat niet de toplevel van zijn repo is): geen git-aanroep op een ander pad, en geen 500 bij een geldige symlink. Taak 2.
2. **Een node-marker van een gesloten run** blijft anders elke tick meetellen in de rem: `drop_marker` wist op de node waar hij vandaan kwam. Taak 11.
3. **Twee gelijktijdige transcriptophalingen** (tick en `lighthouse run`): spiegel gelijk aan het bestand op de node, geen dubbel bijgeschreven delta. Taak 10.
4. **Bestand op de node korter dan de spiegel** (rotatie, nieuwe sessie met hetzelfde id): spiegel wordt vervangen, geen verminkte meting. Taak 5 en 10.
5. **`serve` antwoordt even niet bij `launch`, `git` of `markers`**: geen tweede spawn, workspace blijft staan, rem van die node dicht. Taak 9, 11 en 12.

---

### Taak 1: Configuratie — `serve_url`, `serve_token_file`, `[node]`

**Files:**
- Modify: `internal/hostconfig/hostconfig.go` (struct `Node`, `LoadDispatch`)
- Create: `internal/hostconfig/node.go` (+ `node_test.go`)
- Test: `internal/hostconfig/hostconfig_test.go`

**Interfaces:**
- Produces: `hostconfig.Node{NodeID string; Slots int; ServeURL, ServeTokenFile string}`; `type NodeServe struct{ TokenFile string }`; `func LoadNode(path string) (NodeServe, bool, error)` (`bool` = `[node]` aanwezig); `func ValidateNodeForge(d Dispatch) error` (tokenbestand van `[dispatch.forge]` bestaat en is niet leeg).

- [ ] **Step 1: Schrijf falende tests** in `hostconfig_test.go`: `TestNodeRequiresServeKeys` (zonder `serve_url` of zonder `serve_token_file` een fout met de sleutel `dispatch.nodes.<naam>.serve_url` resp. `.serve_token_file` en de stap "add"), `TestServeURLScheme` (`https://x` ok, `http://127.0.0.1:1` ok, `http://example.test` fout, `ftp://…` fout), `TestServeTokenFileMustBeAbsolute`.
- [ ] **Step 2: Draai ze** — `go test ./internal/hostconfig -run 'TestNode|TestServe'` — FAIL.
- [ ] **Step 3: Implementeer** de twee velden in `Node` en in de raw-struct van `LoadDispatch`; valideer na de bestaande node-controles; foutteksten in dezelfde vorm als `dispatch.nodes.<naam>.slots: missing`.
- [ ] **Step 4: Tests voor `[node]`** (`node_test.go`): `TestLoadNodeAbsentIsFalse`, `TestLoadNodeTokenFileMustBeAbsolute`, `TestValidateNodeForgeNeedsToken` (bestand ontbreekt of leeg → fout met sleutel `dispatch.forge.token_file`; `DefaultDispatch` vult `url` en `token_file` altijd). Implementeer `LoadNode` en `ValidateNodeForge`; `LoadDispatch` blijft sleutels buiten `dispatch` negeren.
- [ ] **Step 5: Draai `go test ./internal/hostconfig`** — PASS.
- [ ] **Step 6: Commit** — `feat(hostconfig): serve_url, serve_token_file en [node] (#327)`.

---

### Taak 2: Het node-API-contract en de kern van `serve`

**Files:**
- Create: `internal/nodeapi/nodeapi.go` (wire-types, routepaden, `Version = 1`, foutcodes)
- Create: `internal/nodeserve/handler.go`, `internal/nodeserve/resolve.go`, `internal/nodeserve/handler_test.go`
- Modify: `internal/server/server.go` (`Deps.Node http.Handler`, gemount op `/api/node/`)

**Interfaces:**
- Produces: `nodeapi.Version`, `nodeapi.CodeWorkspaceGone = "workspace_gone"`, `nodeapi.CodeNoTranscript = "no_transcript"`, `nodeapi.Problem{Status int; Code, Detail string}`, `nodeapi.Info{Version int; Binary, Platform string}`; `nodeserve.Deps{StateDir string; TokenFile string; Forge kennforge.Config; Home string; Run workplace.Runner; Now func() time.Time}`; `func nodeserve.New(d Deps) http.Handler`; intern `func (h *handler) resolve(ctx, id string) (workplace.Workspace, error)` dat het pad opzoekt en toetst.
- Consumes: Taak 1 (`LoadNode`).

- [ ] **Step 1: Schrijf falende tests** (`handler_test.go`, nep-forge via `httptest`, git-repo in `t.TempDir()`): `TestNoTokenIs401`, `TestWrongTokenIs403`, `TestEmptyTokenFileIsError` (fout van dat verzoek, niet 200), `TestInfoReportsVersionOne`, `TestUnknownWorkspaceIs404WithCode` (forge-404 → 404 met `code == "workspace_gone"`, géén git-aanroep: tel runner-aanroepen), `TestUnsafeIDIs400BeforeAnyCall`, `TestResolveRejectsRelativePath`, `TestResolveRejectsPathNotToplevel`, `TestResolveFollowsSymlink` (worktree via symlink gemeld; toplevel `EvalSymlinks`-gelijk → ok).
- [ ] **Step 2: Draai** `go test ./internal/nodeserve` — FAIL (pakket ontbreekt).
- [ ] **Step 3: Implementeer** `nodeapi` en `nodeserve.New` met `net/http` + huma-conventies van `internal/server` (zelfde `OperationID`-stijl); auth-middleware leest het token per verzoek. De forge-client is `kennforge.Client` van de node (zelfde `kennforge.New(url, tokenFile)`).
- [ ] **Step 4: Mount** in `server.New`: `if d.Node != nil { mux.Handle("/api/node/", d.Node) }`; zonder blijft `/api/` 404. Test `TestNodeRoutesAbsentWithoutNode` in `internal/server`. Werk de OpenAPI-snapshot bij als `openapi_snapshot_test.go` dat vraagt.
- [ ] **Step 5: Draai** `go test ./internal/nodeserve ./internal/server` — PASS.
- [ ] **Step 6: Commit** — `feat(serve): node-API-kern: auth, info en padherleiding (#327)`.

---

### Taak 3: Statemap-routes — `launch`, `carrier`, `permission`

**Files:**
- Modify: `internal/nodeserve/handler.go`; Create: `internal/nodeserve/statemap.go`, `statemap_test.go`
- Modify: `internal/dispatch/instructions.go` (schrijven van `runs/<sessie>/instructions.md` als herbruikbare functie `WriteInstructions(stateDir, sessionID string, content []byte) (path string, err error)` als die er nog niet zo is)

**Interfaces:**
- Produces: `nodeapi.LaunchRequest{Repo string; Issue int; SessionID, Mode, Profile, Prompt string; Native bool; MCPConfig string; Instructions string}`, antwoord `workplace.Carrier`; `PUT workspaces/{id}/launch`, `DELETE workspaces/{id}/carrier`, `GET runs/{session}/permission` (ruwe bytes, 404 zonder bestand).
- Consumes: Taak 2 (`New`, namen-controle).

- [ ] **Step 1: Tests**: `TestLaunchWritesCarrierAtNodeStateDir` (drager staat in `<statemap>/carriers/<id>.json`, zonder `node`-submap, 0600, `--session-id <sessie>` en `--append-system-prompt-file` verwijst naar een bestaand pad in de statemap van de node), `TestLaunchResumeUsesResumeFlag`, `TestLaunchRejectsNonUUIDSession` (400), `TestLaunchWorksWhenForgeForgotWorkspace` (statemap-routes zoeken niets op), `TestCarrierDeleteIsIdempotent`, `TestPermissionReturnsRawBytes`, `TestPermissionMissingIs404`, `TestPermissionUnsafeSessionIs400`.
- [ ] **Step 2: Draai** — FAIL.
- [ ] **Step 3: Implementeer** de drie routes met `workplace.NewCarrier`/`WriteCarrier(stateDir, "", id, c)`/`RemoveCarrier`; `mcp_config` is een pad van de node en wordt ongewijzigd doorgegeven. De route geeft de geschreven drager terug.
- [ ] **Step 4: Draai** `go test ./internal/nodeserve ./internal/dispatch` — PASS.
- [ ] **Step 5: Commit** — `feat(serve): launch, carrier en permission op de node (#327)`.

---

### Taak 4: Git-routes — `follow`, `relocate`, `git`, `sessions`

**Files:**
- Create: `internal/nodeserve/gitroutes.go`, `gitroutes_test.go`
- Modify: `internal/workplace/lifecycle.go` (exporteer niets nieuws; `Dirty`/`Unpushed` bestaan al in `internal/workplace/git.go`)

**Interfaces:**
- Produces: `POST workspaces/{id}/follow` body `{branch string, created bool}` → 200 of 409 `nodeapi.OwnWork{Workspace workplace.Workspace; Commits int; Dirty bool}`; `POST workspaces/{id}/relocate` body `{command}` → `{command}`; `GET workspaces/{id}/git?merged_head=` → `{dirty, unpushed bool}`; `GET workspaces/{id}/sessions` → `[]nodeapi.Session{Key, State, SessionID string; UpdatedAt string}`.
- Consumes: Taak 2 (`resolve`), `workplace.FollowRemote`, `workplace.Dirty`, `workplace.Unpushed`, `dispatch.Relocate`.

- [ ] **Step 1: Tests** (echte repo's in tempmappen met een bare `origin`): `TestFollowSwitchesBranchWithoutOwnWork`, `TestFollowOwnWorkIs409WithFields` (aantal commits, dirty), `TestFollowFastForwardsFoundWorkspace`, `TestGitReportsDirtyAndUnpushed`, `TestGitMergedHeadExemptsUnpushed` (zelfde uitkomst als `workplace.Unpushed` direct), `TestGitMergedHeadMustBe40Hex` (400), `TestRelocateFindsPathInNodeWorktree`, `TestRelocateErrorIs4xxWithDetail`, `TestSessionsMirrorsForgeAgentSessions` (meet de velden tegen de nep-forge, in dezelfde vorm als `kennforge.Adapter.Sessions`; een forge-404 → 404 `workspace_gone`).
- [ ] **Step 2: Draai** — FAIL.
- [ ] **Step 3: Implementeer** met `workplace.ExecRunner()` (`Deps.Run`). `sessions` hergebruikt de lezing van `Adapter.Sessions` voor `Node == ""`: haal die uit in een exporteerbare `(*Adapter).Sessions` van een adapter met `Node: ""` die `nodeserve` zelf bouwt.
- [ ] **Step 4: Draai** `go test ./internal/nodeserve` — PASS.
- [ ] **Step 5: Commit** — `feat(serve): follow, relocate, git en sessions op de node (#327)`.

---

### Taak 5: Transcript-delta

**Files:**
- Create: `internal/workplace/transcriptdelta.go`, `transcriptdelta_test.go`
- Create: `internal/nodeserve/transcript.go`, `transcript_test.go`
- Modify: `internal/workplace/kennforge/transcript.go` (`containerScript` krijgt offsets)

**Interfaces:**
- Produces: `workplace.FileDelta{Name string; Size int64; From int64; Data []byte}`; `func workplace.ReadDelta(dir, sessionID string, from map[string]int64) ([]FileDelta, error)` (bestanden: `<sessie>.jsonl` en `<sessie>/subagents/*.jsonl`; is `Size < from[Name]` dan `From = 0` en `Data` het hele bestand); `GET workspaces/{id}/transcript/{session}?profile=…&from=<bestand>:<offset>,…` → tar waarvan elk lid de bytes vanaf `from` bevat plus een header met `Size` en `From` (PAX-records `LIGHTHOUSE.size`, `LIGHTHOUSE.from`); 404 `no_transcript` als `<sessie>.jsonl` ontbreekt.
- Consumes: `workplace.HostTranscriptDir`, `ProjectDir`, de container-tak van `kennforge` (profiel `software`).

- [ ] **Step 1: Tests**: `TestReadDeltaFromOffset` (alleen nieuwe bytes), `TestReadDeltaShorterFileReturnsWholeFile`, `TestReadDeltaIgnoresForeignFiles` (`../`, andere sessie), `TestTranscriptRouteTarHeadersCarrySizeAndFrom`, `TestTranscriptRouteNoFileIs404NoTranscript`, `TestTranscriptRouteUnknownProfileIs400`, `TestContainerScriptReadsFromOffset` (nep-`podman` via Runner: het script start `tail -c +<offset+1>`; de tak voor profiel `software` gebruikt de container, nooit de host).
- [ ] **Step 2: Draai** — FAIL.
- [ ] **Step 3: Implementeer** `ReadDelta` (gelezen lengte vastleggen vóór het lezen, zodat `Size` bij `Data` hoort) en de route; de container-tak krijgt `containerScript` met `$3` als lijst van `naam:offset`, de tar-uitvoer wordt met dezelfde `sessionFile`-filter uitgepakt.
- [ ] **Step 4: Draai** `go test ./internal/workplace/... ./internal/nodeserve` — PASS.
- [ ] **Step 5: Commit** — `feat(serve): transcript-delta als tar, op host en in container (#327)`.

---

### Taak 6: `stop`, `kill`, `usage`, `markers`

**Files:**
- Create: `internal/nodeserve/proc.go`, `usage.go` (+ tests)
- Modify: `internal/workplace/kennforge/stop.go` (niets aan de adapter-semantiek; alleen een exporteerbare `(*Adapter).KillSessions` is niet nodig: de handler roept `Kill` aan)

**Interfaces:**
- Produces: `POST workspaces/{id}/stop` en `/kill` body `{profile string; session_ids []string}`; `PUT usage` body `{pause *string; alarm *string}` (ruwe bestandsinhoud, `nil` = wissen); `GET markers` → `[]park.Marker`; `DELETE markers/{session}?key=<key>`.
- Consumes: Taak 2; `park.PauseFile`, `park.MarkerDir`, `usage.Paths.Mirror`, `park.ReadMarkers`, `park.RemoveMarker`, `workplace.WriteFileAtomic`.

- [ ] **Step 1: Tests**: `TestKillOnSoftwareProfileKillsInsideContainer` (nep-`podman`; geen host-`pkill`), `TestKillEmptySessionIDIs400BeforeAnyCall`, `TestKillUnknownProfileIs400`, `TestStopWithoutUnitPrefixFallsBackToKill` (runner ziet `pgrep -f`, `pkill -f` en tot vijf keer `pgrep`; geen `systemctl`), `TestStopWithUnitPrefixUsesSystemctl`, `TestUsagePutWritesPauseAndAlarmAtomically`, `TestUsagePutNilRemovesFiles`, `TestMarkersListsNodeMarkers`, `TestMarkerDeleteHonoursKey` (verkeerde `key` laat de marker staan).
- [ ] **Step 2: Draai** — FAIL.
- [ ] **Step 3: Implementeer** de handlers met een adapter voor de eigen forge (`Node: ""`). De `stop`-terugval zit alleen in de handler: `if d.Forge.UnitPrefix == "" { return adapter.Kill(...) }`.
- [ ] **Step 4: Test op de adapter zelf** (`stop_test.go`): `TestStopSelfEmptyUnitPrefixStaysError` blijft groen en is ongewijzigd.
- [ ] **Step 5: Draai** `go test ./internal/nodeserve ./internal/workplace/kennforge` — PASS.
- [ ] **Step 6: Commit** — `feat(serve): stop, kill, usage en markers op de node (#327)`.

---

### Taak 7: `lighthouse serve` zet de node-routes aan

**Files:**
- Modify: `internal/cli/serve.go`, `internal/cli/serve_test.go`
- Create: `deploy/launchd/com.lighthouse.serve.plist` (voorbeeld; `serve` op loopback, ingang via `tailscale serve`)
- Modify: `docs/host-assumptions.md` (rij `lighthouse.service` krijgt de launchd-variant)

**Interfaces:**
- Consumes: Taak 1 (`LoadNode`, `ValidateNodeForge`), Taak 2 (`nodeserve.New`).

- [ ] **Step 1: Tests**: `TestServeWithoutNodeSectionHasNoNodeRoutes` (404), `TestServeWithNodeSectionAndNoForgeTokenFails` (start niet; fout noemt `dispatch.forge.token_file`), `TestServeWithNodeSectionMountsRoutes` (401 zonder token), `TestServeFailsOnNodeWithoutServeKeys` (migratie van #187: de fout noemt de sleutel en de stap).
- [ ] **Step 2: Draai** — FAIL.
- [ ] **Step 3: Implementeer** in de `RunE` van `newServeCommand`: na `LoadDispatch`, `LoadNode`; bij `present` `ValidateNodeForge` en `server.Deps.Node = nodeserve.New(...)` met de statemap volgens `tickEnv.stateDir()`.
- [ ] **Step 4: Draai** `go test ./internal/cli -run Serve` — PASS.
- [ ] **Step 5: Commit** — `feat(serve): node-routes aan met [node] in host.toml (#327)`.

---

### Taak 8: De client op de tick

**Files:**
- Create: `internal/workplace/kennforge/nodeclient.go`, `nodeclient_test.go`
- Modify: `internal/workplace/kennforge/adapter.go` (veld `Serve *NodeClient`)

**Interfaces:**
- Produces: `type NodeClient struct{ URL, TokenFile string; HTTP *http.Client; ... }`; `func NewNodeClient(url, tokenFile string) *NodeClient`; methoden die de routes van taak 3–6 spiegelen: `Info(ctx) (nodeapi.Info, error)`, `Launch(ctx, id, nodeapi.LaunchRequest) (workplace.Carrier, error)`, `DeleteCarrier`, `Follow(ctx, id, branch string, created bool) error` (409 → `*workplace.OwnWorkError`), `Sessions`, `Transcript(ctx, id, session, profile string, from map[string]int64) ([]workplace.FileDelta, error)`, `Relocate`, `Permission(ctx, session) ([]byte, error)` (404 → `nil, nil`), `Git(ctx, id, mergedHead string) (dirty, unpushed bool, err error)`, `Stop`, `Kill`, `PutUsage`, `Markers`, `DeleteMarker`. Een `*NodeError{Status int; Code string}` met `errors.As`. `Info` wordt eenmaal per `NodeClient` bewaard; de tick bouwt per tick een nieuwe client (het bewaren per tick is dus per instantie).

- [ ] **Step 1: Tests** met een `httptest`-server: `TestInfoVersionMismatchIsErrorWithBothVersions`, `TestTokenReadPerRequest` (wijzig het bestand tussen twee aanroepen), `TestMissingTokenFileIsError`, `TestOwnWork409BecomesOwnWorkError`, `TestWorkspaceGoneCodeMaps` (404 `workspace_gone` → `*NodeError` met `Code`), `TestUnreachableIsNotGone` (verbindingsfout is nooit `workplace.ErrGone`), `TestPermission404IsNil`, `TestHTTPScheme` (nep-url met `http://example.test` geweigerd door `NewNodeClient`-validatie niet nodig: dat doet Taak 1; de client weigert wel een redirect naar een ander schema).
- [ ] **Step 2: Draai** — FAIL.
- [ ] **Step 3: Implementeer** de client; elke methode controleert eerst `Info` (eenmaal per instantie).
- [ ] **Step 4: Draai** `go test ./internal/workplace/kennforge -run NodeClient` — PASS.
- [ ] **Step 5: Commit** — `feat(kennforge): client voor de node-API (#327)`.

---

### Taak 9: De adapter op een node gebruikt `serve`

**Files:**
- Modify: `internal/workplace/kennforge/adapter.go`, `stop.go`, `fleet.go`, `transcript.go`; `internal/workplace/workplace.go` (optionele interfaces)
- Modify: `internal/workplace/kennforge/contract_test.go`; Create: `internal/workplace/kennforge/node_contract_test.go`
- Delete: `remoteStop` en `remoteSessions` in `fleet.go`

**Interfaces:**
- Produces in `internal/workplace/workplace.go`:
  - `type GitMeter interface { Dirty(ctx context.Context, workspaceID string) (bool, error); Unpushed(ctx context.Context, workspaceID, mergedHead string) (bool, error) }`
  - `type Relocator interface { Relocate(ctx context.Context, workspaceID, command string) (string, error) }`
  - `type PermissionReader interface { Permission(ctx context.Context, sessionID string) ([]byte, error) }` (`nil, nil` = geen bestand)
  Alleen de adapter met `Serve != nil` implementeert ze; de adapter op `self` en de lokale adapter niet.
- Consumes: Taak 8.

- [ ] **Step 1: Test de contract-suite tegen een echte `nodeserve`**: `node_contract_test.go` bouwt een `Fixture` met nep-hub (bestaande `fakehub_test.go`), `nodeserve.New` boven een nep-forge en een git-repo in een tempmap, en draait `workplacetest.Run`. Voeg aan `workplacetest.Cases` toe wat nog mist als de suite die gevallen nog niet kent: `KillEndsAgentOnNodeAdapter`.
- [ ] **Step 2: Gedragstests** (`adapter_test.go`): `TestSpawnOnNodeWritesCarrierBeforeHandoff` (drager op de node met `session_id` en node-paden; hub ziet de handoff pas daarna), `TestSpawnLaunchFailureSendsNoHandoff` (`ErrNotAccepted`, geen POST), `TestSpawnLaunch5xxDeletesCarrier`, `TestSpawnUncertainKeepsCarrier`, `TestSessionsOnNodeCarrySessionID` (`Own`/`AdoptSessions` herkennen de sessie), `TestPrepareOnNodeFollowsRemote` en `TestPrepareOnNodeOwnWorkReturnsOwnWorkError` (zelfde velden als op `self`), `TestPrepareFollowUnreachableFails`, `TestStopOnNodeEndsClaude` (via `serve`), `TestDiscardAndRemoveDeleteNodeCarrier`, `TestRemoveGoneNeedsHubConfirmation` (404 van `serve` telt alleen als weg als de hub de workspace ook niet kent en de node bereikbaar meldt; anders `ErrUncertain`), `TestAuthFailureIsNotMeasurable`.
- [ ] **Step 3: Draai** — FAIL.
- [ ] **Step 4: Implementeer** de takken: `if a.Serve != nil` in `Spawn`, `Prepare`, `Sessions`, `Stop`, `Kill`, `Discard`; `Prepare` roept na de hub `Serve.Follow`; `Dirty`, `Unpushed`, `Relocate`, `Permission` roepen `Serve` aan. De adapter schrijft op een node nooit naar `StateDir/carriers/<node>/`. Verwijder `remoteStop`, `remoteSessions` en de `ErrNotMeasurable`-tak van `Transcript` (die komt in Taak 10).
- [ ] **Step 5: Draai** `go test ./internal/workplace/...` — PASS (ook de bestaande #187-tests die `remoteStop` pinden: pas ze aan of verwijder ze met een regel in de commit).
- [ ] **Step 6: Commit** — `feat(kennforge): een run op een node loopt via serve (#327)`.

---

### Taak 10: Transcriptspiegel op de tick

**Files:**
- Create: `internal/workplace/kennforge/mirror.go`, `mirror_test.go`
- Modify: `internal/workplace/kennforge/transcript.go` (de tak `a.Serve != nil`)

**Interfaces:**
- Produces: `func (a *Adapter) mirrorDir(sessionID string) string` = `<StateDir>/mirror/<node_id>/<sessie>/` (map 0700, bestanden 0600); `Transcript` op een node: `flock` op de map, bestandslengtes van de spiegel als `from`, deltas bijschrijven (of vervangen bij `From == 0` met kleinere `Size`), dan `workplace.ReadTranscript(mirror, sessionID)` zonder wijziging aan `ReadTranscript`; `func RemoveMirror(stateDir, nodeID, sessionID string) error`; `func SweepMirrors(stateDir string, keep func(nodeID, sessionID string) bool) error`.
- Consumes: `internal/flock`, Taak 5, Taak 8.

- [ ] **Step 1: Tests**: `TestSecondTickFetchesOnlyNewBytes` (de nep-`serve` registreert `from`), `TestShorterNodeFileReplacesMirror`, `TestReadTranscriptOnMirrorEqualsSource` (zelfde `Transcript` als `ReadTranscript` op de bronmap, ook `scanWork`/`scanStart`-gevallen), `TestConcurrentFetchesLeaveMirrorEqualToSource` (twee goroutines, mirror byte-gelijk), `TestNoTranscriptIsNotExist` (404 `no_transcript` → `fs.ErrNotExist`), `TestServeUnreachableIsNotMeasurableNotError`, `TestSweepMirrorsRemovesClosedRuns`.
- [ ] **Step 2: Draai** — FAIL.
- [ ] **Step 3: Implementeer** met een `flock` per sessiemap die over lezen én bijschrijven loopt.
- [ ] **Step 4: Draai** `go test -race ./internal/workplace/kennforge -run Mirror` — PASS.
- [ ] **Step 5: Commit** — `feat(kennforge): transcriptspiegel voor runs op een node (#327)`.

---

### Taak 11: Dispatch — `Cleanup`, `Relocate`, `liveOwn`, markers

**Files:**
- Modify: `internal/workplace/lifecycle.go`, `internal/dispatch/apply.go`, `internal/dispatch/brake.go`, `internal/dispatch/runstate.go`, `internal/park/marker.go`
- Test: `lifecycle_test.go`, `apply_test.go`, `brake_test.go`, `runstate_test.go`

**Interfaces:**
- Produces: `dispatch.ParsePermission(data []byte) (*Permission, error)` (`ReadPermission` roept hem aan); `dispatch.NodeSlots.Unmeasured bool` (rem van die node dicht met de reden `markers not readable`); `dispatch.Env.RemoveMarker func(node, session, key string) error` (nil → `park.RemoveMarker` in `MarkerDir`, zoals nu).
- Consumes: `workplace.GitMeter`, `workplace.Relocator`.

- [ ] **Step 1: Tests**: `TestCleanupOnNodeRunsSameStepsAsSelf` (nep-`GitMeter`; vuil, niet gepusht, gemergede head), `TestCleanupOnNodeGitUnreachableKeeps` (`not measurable: …`, workspace blijft), `TestCleanupNodeWithoutGitMeterKeeps`, `TestApplyRelocatesOnNode` (via `Relocator`; fout → `relocErr`), `TestBusyOnNodeLikeSelf` (geen `liveOwn`: een levende eigen sessie is `Busy`/`Earlier` zoals op `self`), `TestDropMarkerDeletesOnOriginatingNode`, `TestResumeRemovesMarkerOnNode`, `TestMarkerRemovalFailureIsLogNotError`, `TestBrakeClosesNodeWithUnmeasuredMarkers`, `TestParsePermissionSameAsReadPermission`.
- [ ] **Step 2: Draai** — FAIL.
- [ ] **Step 3: Implementeer**: verwijder de vroege uitstap `no local git` in `Cleanup` en de test die hem pinde; `Cleanup` meet via `w.(GitMeter)` als `c.Node != ""`, anders met de lokale `run`. Verwijder `liveOwn` en zijn aanroep; de `a.Node == ""`-guard rond `Relocate` wordt de `Relocator`-tak. Voeg `Unmeasured` toe aan `nodeClosed`.
- [ ] **Step 4: Draai** `go test ./internal/workplace ./internal/dispatch ./internal/park` — PASS.
- [ ] **Step 5: Commit** — `feat(dispatch): Cleanup, Relocate en markers op een node (#327)`.

---

### Taak 12: Tick en `lighthouse run` bedraden

**Files:**
- Modify: `internal/cli/tick.go`, `internal/cli/brake.go`, `internal/cli/run.go`
- Test: `tick_nodes_test.go`, `brake_test.go`, `run_test.go`

**Interfaces:**
- Consumes: Taak 1, 8, 9, 10, 11.

- [ ] **Step 1: Tests**: `TestWorkplaceOnNodeGetsServeClient` (`tickEnv.workplace` zet `Serve` uit `cfg.Nodes`), `TestRunProbeReadsPermissionViaServe` (zelfde uitkomst als bestand op `self`; geen bestand → `nil`; `serve` onbereikbaar → run niet meetbaar, geen tickfout), `TestTickPushesPauseAndAlarmToEveryNode` en `TestTickPushesNilWhenPauseCleared` (`PUT usage` per node, elke tick), `TestUsagePushFailureKeepsLastState` (geen tickfout), `TestReadHostFactsCountsNodeMarkers` (marker van een node telt voor rem en hervatten), `TestReadHostFactsNodeMarkersUnreadableSetsUnmeasured` (een node waarvan `GET markers` faalt krijgt deze tick geen start; zowel `tick` als `lighthouse run`), `TestNodeMarkerOfClosedIssueGoneAfterOneTick`, `TestResumeOnNodeRemovesMarkerThere`, `TestSweepRemovesStaleTickSideCarriersAndMirrors` (`carriers/<node>/<id>.json` van een gesloten node-run eenmalig gewist).
- [ ] **Step 2: Draai** — FAIL.
- [ ] **Step 3: Implementeer**: `workplace()` bouwt de `NodeClient` per node; `runProbe.measure` gebruikt `workplace.PermissionReader` met `dispatch.ParsePermission` als de workplace hem heeft; `readHostFacts` leest `GET markers` per node en onthoudt `map[sessie]node` voor `Env.RemoveMarker`; de tick roept `PutUsage` aan voor elke node, na `readHostFacts`; sweep ruimt oude `carriers/<node>/` en spiegels op.
- [ ] **Step 4: Draai** `go test -race ./internal/cli` — PASS.
- [ ] **Step 5: Commit** — `feat(tick): usage-rem, permissie en markers op een node (#327)`.

---

### Taak 13: Documentatie en changelog

**Files:**
- Modify: `docs/workplace.md` (*Een run op een andere node* herschreven: twee kanalen, routes, vervallen beperkingen), `docs/tick.md` (*Nodes*: `serve_url`, `serve_token_file`, brekende wijziging), `docs/install.md` (`serve` op een node, `[node] token_file`, `[dispatch.forge]`, ingang, `lighthouse ask` op het `PATH`, macOS/launchd), `docs/developer/planning/specs/2026-11-13-issue-187-forge-nodes-design.md` (verwijzing in *Vervolg*)
- Create: `changelog.d/issue-327-run-inhoud-nodes.md` (`type: breaking` als het formaat dat kent, anders het type dat de bestaande fragmenten voor een brekende wijziging gebruiken; lees `docs/releasing.md`)

- [ ] **Step 1: Schrijf de docs** en controleer relatieve links; `md-link-check` blokkeert kapotte links.
- [ ] **Step 2: Draai** `go run ./cmd/lighthouse init --upgrade` — verwacht `No changes.` (er verandert niets onder `pack/`).
- [ ] **Step 3: Draai de volledige gate** — `[test_gate] command` uit `lighthouse.toml` en `GOOS=darwin go build ./...`, `GOOS=windows go build ./...` — PASS.
- [ ] **Step 4: Commit** — `docs(nodes): run-inhoud op een node, serve op de node, brekende config (#327)`.

---

## Zelfcontrole

- **Dekking van de spec:** AC 1 (taak 3, 9), AC 2 (taak 5, 10, 12), AC 3 (taak 4, 9), AC 4 (taak 4, 11), `Stop`/`Kill` (taak 6, 9, 11), usage-rem (taak 6, 11, 12), `Relocate` (taak 4, 11), configuratie en migratie (taak 1, 7, 12), versie (taak 8), `serve` op een node en launchd (taak 7, 13), documentatie (taak 13).
- **Open voor de bouwer (zoals de spec):** volgorde binnen een taak, JSON-vorm per route (huma), veldnamen van `/agent-sessions` meten tegen de draaiende forge (taak 4), de wijze waarop `info` per tick wordt bewaard (hier: per `NodeClient`-instantie).
- **Niet in dit plan:** de launcher op macOS (paia#1764), de Mac als spoke (paia#1059), geheugenrem op een node, push van een node, een algemene `exec`.
