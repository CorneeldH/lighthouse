# Dispatcher over meerdere kenn-forge-nodes — implementatieplan

> **Voor agentische uitvoerders:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development
> (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken
> checkboxen (`- [ ]`).

**Doel:** de dispatcher kiest per issue een profiel (locatielabel) en per profiel een kenn-forge-node,
legt beide vast in de runstaat, en bedient elke node via de fleet-routes van de hub.

**Architectuur:** `[dispatch.nodes.<naam>]` in `host.toml` en `[controller.locations]` in
`lighthouse.toml` voeden een pure keuze in `Decide`. Elke start- en hervat-actie krijgt `Profile` en
`Node`. `runs` bewaart de node. De kenn-forge-adapter krijgt een `Node` en zet bij een node die niet
`self` is `/api/v1/fleet/hosts/{node_id}` voor zijn paden. De rem telt `slots` per node.

**Tech stack:** Go, SQLite (`modernc.org/sqlite`), `net/http/httptest`, BurntSushi TOML.

**Spec:** [2026-11-13-issue-187-forge-nodes-design.md](../specs/2026-11-13-issue-187-forge-nodes-design.md)

## Globale eisen

- `self` is de node van `[dispatch.forge]`. In Go en in de database is `self` de lege string `""`. De
  migratie geeft bestaande rijen `''`. (De spec schrijft `DEFAULT 'self'`; de lege string is dezelfde
  betekenis en houdt de golden fixtures en JSON met `omitempty` stabiel.) In `host.toml` is de naam
  `self` gereserveerd.
- Voor node `""` blijven alle huidige paden, `systemctl`, `podman`, `FollowRemote` en de git-metingen
  ongewijzigd. Een `host.toml` zonder `[dispatch.nodes]` en een repo zonder `location_prefix` gedragen
  zich exact als nu.
- Geen hostnaam, node-ID of machine-opzet in code of fixtures. Gebruik in tests id's als `node-a`,
  `node-b`.
- Fleet-pad: `/api/v1/fleet/hosts/{url.PathEscape(node_id)}` + het pad na `/api/v1`.
- Een 404 op een node die niet `self` is, telt alleen als weg als `/api/v1/fleet/snapshot` die node
  bereikbaar meldt; anders `workplace.ErrUncertain` of `workplace.ErrNotMeasurable`.
- Uitsteltekst bij een onbekende locatie: `locatie <waarde> onbekend`; bij meer dan één label:
  `meer dan één locatielabel`.
- Go volgt `/lighthouse-go-standard`, tests `/lighthouse-test-standard`. Na elke taak:
  `go test -race ./...` groen, `golangci-lint run` zonder bevindingen.
- Alle commits Conventional Commits, Nederlands, met `(#187)`.

## Review-focus

1. **Een label wijzigt tijdens een run.** Verwacht: profiel en node van de open run veranderen niet;
   een hervatting gaat naar dezelfde node. Test in taak 5 (`TestPlacementResumeKeepsNode`).
2. **De hub antwoordt 404 voor een spoke die offline is.** Verwacht: geen `ErrGone`, geen sluiting,
   geen tweede start. Test in taak 8 (`TestRemoteNotFoundUnreachableIsUncertain`).
3. **Twee nodes met dezelfde workspace-id.** Verwacht: Remove op de ene raakt drager en leftover van
   de andere niet. Tests in taak 3 en 7.
4. **Een profiel verliest zijn node in `host.toml` terwijl er nog open runs op staan.** Verwacht: de
   rem stelt nieuwe starts op die node uit met "node … not configured"; Stop en Remove van de open run
   gaan nog naar zijn `node_id` (de adapter heeft alleen het id nodig). Test in taak 6 en 9.
5. **Een locatielabel met hoofdletters of spaties** (`1-loc:Mac`). Verwacht: exacte vergelijking na de
   prefix, dus onbekend en uitgesteld; geen stille val terug. Test in taak 5.

---

### Taak 1: nodes in `host.toml`

**Bestanden:**
- Wijzig: `internal/hostconfig/hostconfig.go` (`Dispatch`, `Profile`, `LoadDispatch`)
- Test: `internal/hostconfig/hostconfig_test.go`

**Interfaces:**
- Produceert:
  - `type Node struct { NodeID string; Slots int }`
  - `Dispatch.Nodes map[string]Node` (sleutel = naam uit `[dispatch.nodes.<naam>]`; nil zonder nodes)
  - `Profile.Node string` (`toml:"node"`, naam van een node; `""` = self)
  - `func (d Dispatch) NodeID(profile string) string` — `node_id` van de node van het profiel, `""`
    voor self of een onbekend profiel
  - `func (d Dispatch) NodeName(nodeID string) string` — de naam bij een `node_id`, `"self"` voor `""`,
    het id zelf als het onbekend is

- [ ] **Stap 1: falende tests.** `TestLoadDispatchNodes`: twee nodes `a` (`node_id = "node-a"`,
  `slots = 1`) en `b`, profiel `p` met `node = "a"`; verwacht `d.Nodes["a"] == Node{"node-a", 1}` en
  `d.NodeID("p") == "node-a"`, `d.NodeID("software") == ""`. Tabeltest `TestLoadDispatchNodeErrors`,
  elke rij een fout die de sleutel noemt: ontbrekend `node_id` (`dispatch.nodes.a.node_id`),
  `slots = 0` (`dispatch.nodes.a.slots`), dubbel `node_id`, naam `self`, `node_id = "self"`,
  onbekende sleutel onder een node, profiel met `node = "x"` zonder node `x`
  (`dispatch.profiles.p.node`). De bestaande tests op regel 91, 199, 221-251 blijven ongewijzigd groen.
- [ ] **Stap 2:** `go test ./internal/hostconfig/ -run 'Nodes|NodeErrors'` — FAIL.
- [ ] **Stap 3:** voeg `Nodes` toe aan de raw-struct (`map[string]struct{ NodeID *string
  \`toml:"node_id"\`; Slots *int \`toml:"slots"\` }`) en valideer na het decoderen, in dezelfde stijl
  als de bestaande foutmeldingen. Implementeer `NodeID` en `NodeName`.
- [ ] **Stap 4:** `go test -race ./internal/hostconfig/` — PASS.
- [ ] **Stap 5:** commit `feat(hostconfig): nodes en node per profiel in host.toml (#187)`.

### Taak 2: `[controller.locations]` in `lighthouse.toml`

**Bestanden:**
- Wijzig: `internal/repoconfig/repoconfig.go` (`Controller`, `rawController`, `Load`)
- Test: `internal/repoconfig/repoconfig_test.go`

**Interfaces:**
- Produceert: `Controller.Locations map[string]string` (waarde na de prefix → profielnaam; nil zonder
  tabel).

- [ ] **Stap 1: falende test** `TestControllerLocations`: `[controller.locations] mac = "mac"`,
  `container = "software"` geeft die map; zonder tabel `nil`; een lege profielnaam is een fout die
  `controller.locations.<waarde>` noemt.
- [ ] **Stap 2:** draai de test — FAIL.
- [ ] **Stap 3:** `Locations map[string]string \`toml:"locations"\`` in `rawController`, doorgeven in
  `Load`.
- [ ] **Stap 4:** `go test -race ./internal/repoconfig/` — PASS.
- [ ] **Stap 5:** commit `feat(repoconfig): locatie naar profiel per repo (#187)`.

### Taak 3: node in de runstaat

**Bestanden:**
- Maak: `internal/store/migrations/0007_runs_node.sql`
- Wijzig: `internal/runs/runs.go`, `internal/runs/history.go`, `internal/runs/workspaces.go`
- Test: `internal/runs/runs_test.go`, `internal/runs/workspaces_test.go`, `internal/store/migrate_test.go`
  (pint `user_version` op 6, `migrate_test.go:81`: wordt 7; `TestMigrationNodeDefault` hoort hier, want
  hij heeft de niet-geëxporteerde `store.migrate` nodig)

**Interfaces:**
- Produceert:
  - `Claim.Node`, `Row.Node`, `OpenRun.Node` (`json:"node,omitempty"`), allemaal `string`, `""` = self
  - `WorkspaceRef{Issue int; Node, WorkspaceID, Branch, Profile string}`
  - `Workspaces(ctx, db, repo)` groepeert op `(node, workspace_id)`
  - `MarkWorkspaceRemoved(ctx, db, repo, node, workspaceID string, now time.Time) error`

- [ ] **Stap 1: falende tests.** `TestTakeStoresNode`: `Take` met `Node: "node-a"`, daarna
  `Open` en `History` geven `Node == "node-a"`. `TestWorkspacesPerNode`: twee runs met
  `workspace_id = "w1"` op node `""` en `"node-a"` geven twee refs; `MarkWorkspaceRemoved(…, "node-a",
  "w1", …)` laat de ref van `""` staan. `TestMigrationNodeDefault`: een rij van vóór de migratie leest
  `Node == ""`.
- [ ] **Stap 2:** `go test ./internal/runs/` — FAIL.
- [ ] **Stap 3:** migratie: `ALTER TABLE runs ADD COLUMN node TEXT NOT NULL DEFAULT '';`. Voeg `node`
  toe aan de INSERT van `Take`, aan `rowColumns`, aan `scanOpen`, en aan de GROUP BY en de UPDATE in
  `workspaces.go`. Pas de aanroepers van `MarkWorkspaceRemoved` aan met `ref.Node`.
- [ ] **Stap 4:** `go test -race ./...` — PASS.
- [ ] **Stap 5:** commit `feat(runs): node van een run in de runstaat (#187)`.

### Taak 4: labels van het issue op het board-item

**Bestanden:**
- Wijzig: `internal/forge/board.go` (query en `BoardItem`), `internal/dispatch/snapshot.go` (`Item`),
  `internal/dispatch/gather.go:102`
- Test: `internal/forge/board_test.go`, `internal/dispatch/gather_test.go`

**Interfaces:**
- Produceert: `BoardItem.Labels []string`, `Item.Labels []string` (`json:"labels,omitempty"`).

- [ ] **Stap 1: falende tests.** Board: een GraphQL-antwoord met `labels.nodes [{name:"1-loc:mac"}]`
  geeft `Labels == []string{"1-loc:mac"}`. Gather: dat label komt in `Item.Labels`.
- [ ] **Stap 2:** draai beide — FAIL.
- [ ] **Stap 3:** `labels(first: 20) { nodes { name } pageInfo { hasNextPage } }` in de
  `... on Issue`-selectie (elke geneste lijst vraagt `pageInfo`, `board.go:76-78`); `hasNextPage` is een
  fout, geen stille afkap, zoals bij de andere lijsten. Doorgeven in `gather.go`.
- [ ] **Stap 4:** `go test -race ./internal/forge/ ./internal/dispatch/` — PASS.
- [ ] **Stap 5:** commit `feat(dispatch): labels van het issue in de snapshot (#187)`.

### Taak 5: keuze van profiel en node in `Decide`

**Bestanden:**
- Wijzig: `internal/dispatch/decide.go` (`Input`, `RepoInput`, `Action`, `planned`, `issueCtx.planned`)
- Maak: `internal/dispatch/placement.go`
- Test: `internal/dispatch/placement_test.go`, nieuwe fixtures in `internal/dispatch/testdata/decide/`

**Interfaces:**
- Consumeert: `Item.Labels` (taak 4), `Row.Node` en `Row.Profile` (taak 3), `Controller.Locations`
  (taak 2).
- Produceert:
  - `Input.ProfileNodes map[string]string` (`json:"profile_nodes,omitempty"`; profiel → `node_id`)
  - `RepoInput.LocationPrefix string` (`json:"location_prefix,omitempty"`)
  - `Action.Profile string` (`json:"profile,omitempty"`), `Action.Node string`
    (`json:"node,omitempty"`)
  - `func (c issueCtx) placement(a Action) (profile, node, deferReason string)`

Regel van `placement`, in deze volgorde:
1. `a.Kind == KindResume`: de rij in `History` met `SessionID == a.SessionID` → zijn profiel en node.
2. Er is een rij met `TriggerKey == a.TriggerKey` (retry): de laatste daarvan → zijn profiel en node.
3. Labels met prefix `LocationPrefix` (alleen als die niet leeg is): nul → `Controller.Profile`;
   twee of meer → `deferReason = "meer dan één locatielabel"`; één → de waarde na de prefix in
   `Controller.Locations`, of `deferReason = "locatie <waarde> onbekend"`.
4. `node = ProfileNodes[profile]`.

`issueCtx.planned` roept `placement` aan. Bij een `deferReason` geeft hij `c.deferred(reason)`; een
plaatsings-defer van een fix-pr-start telt toch mee in `wanted` van `Decide` (`decide.go:119-123`, de
rem doet dit ook), anders sluit `settleUnstartedFixPR` de run als Returned 'PR red' in plaats van hem
open te houden. Voeg een test toe: fix-pr-start met twee locatielabels → defer, run blijft open.
Anders
zet hij `a.Profile`, `a.Node` en `software: a.Profile == ProfileSoftware`, en `node: a.Node` op een
nieuw veld `planned.node`.

- [ ] **Stap 1: falende tests** (tabel, `TestPlacement`): geen prefix → `Controller.Profile`, node
  `""`; label `1-loc:mac` met `mac = "mac"` en `ProfileNodes{"mac": "node-a"}` → `("mac", "node-a",
  "")`; `1-loc:beelink` zonder mapping → defer `locatie beelink onbekend`; twee labels → defer;
  `1-loc:Mac` → defer `locatie Mac onbekend`. `TestPlacementResumeKeepsNode`: history-rij met
  session `s1`, profiel `mac`, node `node-a`; issue heeft nu label `container`; resume van `s1` →
  `("mac", "node-a")`. `TestPlacementRetryKeepsNode`: idem via `TriggerKey`.
  Fixture `placement-location-mac.json`: een start met label en mapping geeft een start-actie met
  `profile` en `node`.
- [ ] **Stap 2:** `go test ./internal/dispatch/ -run 'Placement|TestDecide'` — FAIL.
- [ ] **Stap 3:** implementeer `placement` en de velden.
- [ ] **Stap 4: bestaande fixtures.** Elke start- en hervat-actie in de verwachte uitvoer krijgt nu
  `"profile"`. Voeg dat veld toe met een eenmalig script (niet committen) dat per fixture
  `controller.profile` uit de invoer neemt. Controle: `git diff --stat` noemt alleen
  `testdata/decide/*.json`, en in `git diff internal/dispatch/testdata` is elke `+`-regel een
  `"profile"`-regel of een regel die alleen een komma kreeg.
- [ ] **Stap 5:** `go test -race ./internal/dispatch/` — PASS.
- [ ] **Stap 6:** commit `feat(dispatch): profiel en node per issue via het locatielabel (#187)`.

### Taak 6: rem per node

**Bestanden:**
- Wijzig: `internal/dispatch/brake.go`, `internal/cli/brake.go` (`readHostFacts`)
- Test: `internal/dispatch/brake_test.go`, fixtures `testdata/decide/brake-node-*.json`,
  `internal/cli/brake_test.go`

**Interfaces:**
- Consumeert: `planned.node` (taak 5), `OpenRun.Node` (taak 3), `Dispatch.Nodes` (taak 1).
- Produceert: `Brake.Nodes map[string]NodeSlots` (`json:"nodes,omitempty"`, sleutel `node_id`),
  `type NodeSlots struct { Name string \`json:"name"\`; Slots int \`json:"slots"\`; InUse int
  \`json:"in_use"\` }`.

Regels voor `closed` en `apply`:
- `p.node == ""`: zoals nu (geheugen, `Slots`, `InUse`). `passed` telt alleen starts op `""`.
- `p.node != ""`: geen geheugenrem. `n, ok := b.Nodes[p.node]`; niet ok →
  `fmt.Sprintf("slots: node %s not configured", p.node)`; anders
  `n.InUse+admittedOn[p.node] >= n.Slots` → `fmt.Sprintf("slots: %d of %d in use on %s",
  n.InUse+admittedOn[p.node], n.Slots, n.Name)`. De lokale uitsteltekst is al Engels
  (`slots: n of m in use`); de nodevariant volgt die vorm en noemt de node, wat de spec vraagt
  (`slots vol op <naam>`), in de bestaande taal van de uitsteltekst.
- Usage, pauze en `max_software` gelden zoals nu voor elke node.

- [ ] **Stap 1: falende tests.** Fixture `brake-node-full.json`: node `node-a` met `slots 1`,
  `in_use 1`; een start op `node-a` en een op `""` → de eerste wordt defer `slots: 1 of 1 in use on a`,
  de tweede gaat door. Fixture `brake-node-unknown.json`. `TestReadHostFactsPerNode`: open runs op
  `""` en `node-a` tellen apart.
- [ ] **Stap 2:** draai — FAIL.
- [ ] **Stap 3:** implementeer. `readHostFacts` vult `Nodes` uit `cfg.Nodes` (naam en slots) en telt
  `InUse` per `node_id`.
- [ ] **Stap 4:** `go test -race ./internal/dispatch/ ./internal/cli/` — PASS.
- [ ] **Stap 5:** commit `feat(dispatch): slots per node in de rem (#187)`.

### Taak 7: drager, leftovers, sweep en cleanup per node

**Bestanden:**
- Wijzig: `internal/workplace/carrier.go`, `leftovers.go`, `lifecycle.go` (`Candidate`, `Cleanup`,
  `AdoptSessions`), `sweep.go`, `internal/workplace/local/adapter.go` (aanroepen met node `""`), en `internal/cli/tick.go`
  (de aanroepen op regel 303 en 514 krijgen hier een tussenliggende `Place`:
  `func(node, profile string) workplace.Workplace { return env.workplace(cfg, repo, dir) }`, zodat
  `internal/cli` compileert; taak 9 vervangt die door de echte keuze per node)
- Test: de bijbehorende `_test.go`

**Interfaces:**
- Consumeert: `WorkspaceRef.Node`/`Profile` (taak 3).
- Produceert:
  - `CarrierPath(stateDir, node, workspaceID string) string` — node `""`:
    `<state>/carriers/<id>.json`; anders `<state>/carriers/<node>/<id>.json`. `checkWorkspaceID`
    geldt ook voor node.
  - `WriteCarrier(stateDir, node, workspaceID string, c Carrier)`, `RemoveCarrier(stateDir, node,
    workspaceID string)`
  - `Leftover.Node string` (`json:"node,omitempty"`); sleutel in `tick_state`: `workspaceID` voor
    `""`, anders `node + "/" + workspaceID`. `ClearLeftover(ctx, db, repo, node, workspaceID string)`.
  - `Candidate.Node string`
  - `type Place func(node, profile string) Workplace`
  - `Sweep(ctx, db, place Place, run Runner, in SweepInput, now)` en
    `AdoptSessions(ctx, db, place Place, repo string)`: per ref of open run de workplace van zijn
    node en profiel.

- [ ] **Stap 1: falende tests.** `TestCarrierPathPerNode`; `TestRemoveCarrierOtherNodeUntouched`
  (zelfde id op `""` en `node-a`); `TestLeftoverPerNode` (zelfde id, twee rijen, `ClearLeftover` op
  één); `TestCleanupRemoteSkipsGit` (de remote-controle staat direct na de `hasOpenRun`-guard, vóór `Lookup`,
  `Sessions` en `Stop`; `hasOpenRun` vergelijkt `(node, workspace_id)`, `lifecycle.go:123`): `Candidate.Node = "node-a"` met een `Runner` die faalt op elke
  aanroep → geen aanroep, niet verwijderd, leftover met reden `not measurable: node node-a: no local
  git`; `TestSweepUsesNodeOfRef`: een `Place` die per node een andere fake teruggeeft; Remove komt bij
  de fake van de node van de ref.
- [ ] **Stap 2:** `go test ./internal/workplace/...` — FAIL.
- [ ] **Stap 3:** implementeer. De kenn-forge-adapter roept `WriteCarrier`/`RemoveCarrier` aan met
  `a.Node` (veld komt in taak 8; hier eerst `""`).
- [ ] **Stap 4:** `go test -race ./...` — PASS.
- [ ] **Stap 5:** commit `feat(workplace): drager, leftovers en opruimen per node (#187)`.

### Taak 8: kenn-forge-adapter op een node via de hub

**Bestanden:**
- Wijzig: `internal/workplace/kennforge/adapter.go`, `stop.go`, `transcript.go`
- Maak: `internal/workplace/kennforge/fleet.go`, `internal/workplace/kennforge/fakehub_test.go`
- Test: `internal/workplace/kennforge/fleet_test.go`, `contract_test.go`

**Interfaces:**
- Consumeert: `CarrierPath(…, node, …)` (taak 7).
- Produceert:
  - `Adapter.Node string` (`node_id`; `""` = self)
  - `func (a *Adapter) path(p string) string` — `p` ongewijzigd voor `""`, anders
    `"/api/v1/fleet/hosts/" + url.PathEscape(a.Node) + strings.TrimPrefix(p, "/api/v1")`. Elke
    `a.Client.do` gebruikt `a.path(…)`.
  - `func (a *Adapter) nodeReachable(ctx context.Context) (bool, error)` — leest
    `/api/v1/fleet/snapshot` (altijd zonder `path`).
  - `func newFakeHub(t *testing.T, nodes map[string]*fakeForge) *httptest.Server` — stuurt
    `/api/v1/fleet/hosts/{id}/…` door naar `nodes[id]`, met het pad terug onder `/api/v1`, en
    serveert `/api/v1/fleet/snapshot` met een bereikbaarheid per node die de test kan zetten.

Per methode bij `a.Node != ""`:
- Prepare: lijst en maak via `path`; **geen** `FollowRemote`.
- Sessions: `GET path(wsPath(id))+"/runtime"`. Meet de JSON-vorm in `kenn-io/forge` tag `v0.7.0`
  (`internal/server/workspaceapi`, de handler van `GET /workspaces/{id}/runtime`). Leg veldnamen en
  een voorbeeldantwoord vast in een commentaar boven de wire-struct, met het bronpad.
- Stop en Kill: per session-id de `key` via Sessions; `DELETE
  path(wsPath(id))+"/runtime/sessions/"+url.PathEscape(key)`; een session-id zonder sessie is al weg
  (de spec accepteert dit: tot #327 ziet lighthouse zijn eigen agent op een andere node als vreemd en
  stuurt Stop geen DELETE). Geeft `/runtime` in v0.7.0 geen `session_id` per sessie, leg dat vast in
  het commentaar boven de wire-struct en laat `Sessions` een lege `SessionID` geven. Dan toetst
  `TestRemoteStopDeletesSession` dat er géén DELETE gaat; heeft de gemeten vorm het veld wel, dan
  toetst hij de DELETE met de juiste key. Een mislukte Sessions is een fout.
- Een 404 van `Sessions`, `Lookup` of `Remove`: alleen `ErrGone` of "al weg" als `nodeReachable`
  `true` geeft (meet ook de route `/api/v1/fleet/snapshot` en het bereikbaarheidsveld in forge v0.7.0 en
  leg ze vast in een commentaar, zoals voor `/runtime`); anders `fmt.Errorf("node %s: 404 not confirmed: %w", a.Node, workplace.ErrUncertain)`.
- Transcript: `fmt.Errorf("transcript on node %s: %w", a.Node, workplace.ErrNotMeasurable)`.

- [ ] **Stap 1: falende tests.** `TestFleetPath`. Met `newFakeHub` en twee nodes `node-a`, `node-b`:
  `TestRemotePrepareCreatesOnItsNode` (workspace staat in fake `node-a`, niet in `node-b`, en de
  `Runner` krijgt geen git-aanroep); `TestRemoteSpawnHandoffOnItsNode`; `TestRemoteStopDeletesSession`
  (DELETE met de juiste key bij `node-a`, geen `systemctl`/`podman` in de `Runner`);
  `TestRemoteNotFoundUnreachableIsUncertain` (fake meldt `node-a` onbereikbaar, Lookup geeft 404 →
  `ErrUncertain`, niet `ErrGone`); `TestRemoteNotFoundReachableIsGone`;
  `TestRemoteTranscriptNotMeasurable`. Contract-suite: draai `workplacetest` ook tegen een adapter met
  `Node: "node-a"` achter de fake hub; de suite-gevallen die lokale git of een transcript vragen,
  worden voor die variant overgeslagen met `t.Skip` en een reden die naar #327 wijst. Dat zijn ook de
  gevallen die op `SessionID` in `Sessions` (`suite.go:145`, `:279-314`) of op processignalen,
  overlevenden en niet-meetbare processen (`suite.go:196-236`) leunen, voor zover forge's
  `/runtime` die niet geeft. Maak de nep-hub nooit rijker dan de gemeten v0.7.0-vorm.
- [ ] **Stap 2:** `go test ./internal/workplace/kennforge/` — FAIL.
- [ ] **Stap 3:** implementeer in `fleet.go` (`path`, `nodeReachable`, de 404-regel) en de
  methoden.
- [ ] **Stap 4:** `go test -race ./internal/workplace/...` — PASS.
- [ ] **Stap 5:** commit `feat(kennforge): werkplek op een andere node via de fleet-routes (#187)`.

### Taak 9: bedrading in `apply` en `tick`

**Bestanden:**
- Wijzig: `internal/dispatch/apply.go` (`Env`, `release`, `start`), `internal/cli/tick.go`
  (`tickEnv.Workplace`, `workplace`, `places`, sweep op regel 303, `AdoptSessions` op regel 514,
  `measure` op regel 529-538, `decideInput`, `repoInput`)
- Test: `internal/dispatch/apply_test.go`, `internal/cli/tick_test.go`, `internal/cli/run_test.go`
  (`env.Workplace` op regel 227 en 277 krijgt de nieuwe signatuur)

**Interfaces:**
- Consumeert: alles hierboven.
- Produceert:
  - `Env.Workplace func(repo, node, profile string) workplace.Workplace`
  - `Env.MCPConfig func(profile string) string` (vervangt `Env.Profile`)
  - `tickEnv.Workplace func(repo repoconfig.Repo, node, profile string) workplace.Workplace` (testnaad)
  - `func (e tickEnv) workplace(cfg hostconfig.Dispatch, repo repoconfig.Repo, stateDir, node, profile
    string) workplace.Workplace` — zet `Node` en `Profile` op de adapter

`start` gebruikt `a.Profile` en `a.Node` voor `Workplace`, `MCPConfig` en `runs.Claim`. `release`
gebruikt `r.Node` en `r.Profile` van de open run. `tick` bouwt de adapters lazy in een map met sleutel
`struct{ repo, node, profile string }`. `decideInput` vult `ProfileNodes` uit `cfg.Profiles` en
`cfg.NodeID`. `repoInput` (regel 612-631) vult `RepoInput.LocationPrefix` uit
`pre.Repo.Labels.LocationPrefix`; `decideInput` krijgt alleen `[]dispatch.RepoInput` en kan het niet
lezen. `measure` bouwt één `runProbe` per `(r.Node, r.Profile)` via dezelfde lazy `places`-map, niet
één per repo: een run op `node-a` meten tegen `self` geeft 404, dus `ErrGone` en `RunState.Gone`, en
`Decide` sluit de open run en start het issue opnieuw.

- [ ] **Stap 1: falende tests.** `TestApplyStartOnNode`: een start-actie met `Node: "node-a"` vraagt
  `Workplace(repo, "node-a", "mac")` en de run heeft `Node == "node-a"`. `TestTickRestartStopsOnNode`:
  tick 1 start een run op `node-a` (fake hub, twee nodes); de database wordt gesloten en opnieuw
  geopend; tick 2 met een time-out sluit de run en stuurt niets naar `node-b` of naar `self`. Heeft de
  gemeten `/runtime`-vorm een `session_id` per sessie, dan gaat de DELETE naar `node-a`; heeft ze die
  niet (zie taak 8), dan gaat er geen DELETE naar `node-a` en toetst de test dat. Zelfde voorwaarde
  als `TestRemoteStopDeletesSession`; de nep-hub blijft op de gemeten vorm. `TestTickUnreachableNodeDoesNotBlockOther`: fake `node-a` geeft 503 op Prepare; een start op
  `""` in dezelfde tick slaagt; er is geen run op `node-a` (`apply.start` roept `Prepare` aan vóór `runs.Take`,
  `apply.go:263-307`), de repo staat als apply-fout in de uitvoer en de fingerprint is niet opgeslagen. `TestMeasureUsesNodeOfRun`: een open run op `node-a` waarvan de workspace op `self` ontbreekt wordt
  niet `Gone`. `TestTickSpawnTimeoutNoSecondPost`:
  time-out op de handoff bij `node-a` → één POST in die tick en in de volgende.
- [ ] **Stap 2:** `go test ./internal/dispatch/ ./internal/cli/` — FAIL.
- [ ] **Stap 3:** implementeer; pas de typeassertie op `*kennforge.Adapter` in `tick_test.go:1726,1739`
  aan.
- [ ] **Stap 4:** `go test -race ./...` en `golangci-lint run` — PASS, geen bevindingen.
- [ ] **Stap 5:** commit `feat(tick): runs op de node van hun profiel (#187)`.

### Taak 10: documentatie en changelog

**Bestanden:**
- Wijzig: `docs/workplace.md`, `docs/tick.md` (rond regel 342 en 428-453), `docs/host-assumptions.md`
  (rond regel 35-37), `docs/tick.md` (sectie `[controller]` rond regel 461) voor `[controller.locations]`
- Maak: `changelog.d/issue-187-forge-nodes.md` (naam `<branch>.md` en Engels, `docs/releasing.md:57`; er is
  geen bestaand fragment om te kopiëren)

- [ ] **Stap 1:** `docs/tick.md`: `[dispatch.nodes.<naam>]`, `node` in een profiel, de keuzeregel
  (locatielabel → profiel → node; hervatten en retry erven), de rem per node, de uitstelteksten.
- [ ] **Stap 2:** `docs/workplace.md`: fleet-paden, wat op een andere node anders werkt (geen
  `FollowRemote`, Stop via DELETE, 404-regel, transcript niet meetbaar, sweep ruimt niet op), het pad
  van de drager per node, en dat echte runs op een andere node wachten op #327.
- [ ] **Stap 3:** `docs/host-assumptions.md`: forge v0.7.0 of later op de hub voor een node die niet
  `self` is. De lighthouse.toml-doc: `[controller.locations]`.
- [ ] **Stap 4:** `.claude/hooks/lighthouse/run.sh md-link-check` slaagt via de hook; `go test -race
  ./...` groen.
- [ ] **Stap 5:** commit `docs(dispatcher): nodes, keuzeregel en rem per node (#187)`.
