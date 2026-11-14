# Ontwerp: dispatcher en werkplek over meerdere kenn-forge-nodes

> **Correctie na meting aan forge v0.7.0:** de snapshotroute is `/api/v1/snapshot?include_peers=true`, niet
> `/api/v1/fleet/snapshot`. Een node is bereikbaar als zijn `nodeID` in `hosts` staat en `reachable` waar
> is; ontbreekt hij, dan is hij onbereikbaar. Verder: `/runtime` kent geen `session_id`, dus `Stop` en
> `Kill` sturen op een node geen `DELETE` en een levende sessie laat start en hervatting wachten (#327).
> Zie [workplace.md](../../../workplace.md#een-run-op-een-andere-node).

Issue: [#187](https://github.com/CorneeldH/lighthouse/issues/187) · Datum: 2026-11-13 · Bouwt op
[#113](https://github.com/CorneeldH/lighthouse/issues/113) (de werkplek-adapter voor één forge).

## Aanleiding en doel

De dispatcher kent één kenn-forge: `[dispatch.forge]` in `host.toml`. De kenn-forge-adapter
(`internal/workplace/kennforge`) neemt aan dat die forge op dezelfde host draait als `lighthouse tick`.
kenn-forge kan sinds v0.3.0 een federated fleet vormen: één **hub** en meerdere **spokes**. Elke node
houdt zijn eigen workspaces, git en processen. De hub stuurt acties door naar de node die de workspace
bezit.

Waarom: runs moeten op meer dan één machine kunnen draaien, gekozen via profielen. Voor lighthouse
zelf draait alles op de Beelink met het software-profiel. Voor paia moeten sommige runs echt op de Mac
draaien. De code mag geen specifieke opzet kennen: een willekeurig aantal nodes, elk met eigen
capaciteit.

Uitgangspunt van de gebruiker: **zoveel mogelijk via forge**. Lighthouse praat alleen met de hub en
gebruikt de fleet-routes van forge. Er komt geen eigen kanaal naar een node.

Succes: een profiel wijst naar een node. Een run van dat profiel krijgt zijn workspace op die node, via
de hub. Stoppen, hervatten en opruimen gaan naar dezelfde node, ook na een herstart van
`lighthouse tick`. Een bestaande `host.toml` werkt ongewijzigd.

## Wat forge levert (verkend in `kenn-io/forge`, main na v0.7.0)

- **Fleet-routes op de hub:** `/api/v1/fleet/hosts/{host_key}/…`, met `host_key` = node-ID van forge
  (`self` is de hub zelf). De hub proxyt naar de eigenaar
  (`internal/server/fleetapi/fleet_proxy.go`). Relevant:
  - `GET  …/workspaces` en `GET|DELETE …/workspaces/{id}`
  - `POST …/repo/{provider}/{owner}/{name}/workspaces`
  - `POST …/workspaces/{id}/runtime/agent-handoffs`
  - `GET  …/workspaces/{id}/runtime`
  - `DELETE …/workspaces/{id}/runtime/sessions/{session_key}`
  - `POST …/workspaces/{id}/pull`
- **Geen route voor `/workspaces/{id}/agent-sessions`.** Die gebruikt `Sessions` nu. Op een andere
  node moet `Sessions` uit `…/workspaces/{id}/runtime` lezen.
- **Een sessie stoppen kan via de API** (`DELETE …/runtime/sessions/{key}`). De opmerking in
  `stop.go` dat forge geen stop-endpoint heeft, is achterhaald voor de fleet.
- **Spokes achter NAT** (zoals de Mac) houden een tunnel naar de hub open (v0.6). De hub bereikt ze
  dus wel; lighthouse hoeft een spoke nooit rechtstreeks te bereiken.
- **Workspaces aanmaken op een andere node** vanuit de hub kan vanaf v0.7.0. Hier draait nu v0.5.1.
- **De snapshot per node** (`/api/v1/snapshot?include_peers=true`) geeft platform (`linux`/`macos`),
  bereikbaarheid en tmux-sessies. **Geen geheugen of CPU.**
- **De handoff** draagt alleen `target_key`, `message` en `acp_options`. `message` gaat als prompt
  naar de agent, niet naar de target-launcher.
- Er is geen roadmap-bestand. De richting volgens de releases: alles via de hub, leden declareren in
  `config.toml` (v0.7), devboxes als apart soort uitvoerplek die alleen de hub bedient.

## Doelen, niet-doelen, anti-doelen

**Doelen**

1. `host.toml` beschrijft meer dan één node. Zonder nodes werkt alles zoals nu.
2. Per issue een profiel, via het locatielabel. Een profiel kan een node noemen. De dispatcher legt
   profiel en node vast in de runstaat.
3. Prepare, Spawn, Sessions, Lookup, Stop, Kill, Remove en de sweep werken op de node van de run, via
   de fleet-routes van de hub.
4. `slots` geldt per node. Een volle of onbereikbare node houdt geen start op een andere node tegen.
5. Tests tegen een nep-hub die doorstuurt naar minstens twee `httptest`-nodes.
6. `docs/workplace.md`, `docs/tick.md` en `docs/host-assumptions.md` beschrijven nodes, keuzeregel en
   rem per node.

**Niet-doelen** (naar een vervolgissue, zie *Vervolg*)

- **De run-inhoud op een andere node.** Drie dingen lopen nu via bestanden op de tick-host, en forge
  vervoert ze niet:
  - de drager (`<statemap>/carriers/<id>.json`, met `args`, `env` en de paden naar
    `--mcp-config` en `--append-system-prompt-file`);
  - het transcript (`podman exec` of een lokale map);
  - `permission.json` van de hook `permission-check`.
- **`FollowRemote` op een andere node.** De git-stap van Prepare draait lokaal op het worktree-pad.
  Op een andere node slaat Prepare hem over (zie *Ontwerp*, Prepare).
- **Een geheugenrem op een andere node.** Forge meet daar geen geheugen. Een vaste `slots` is
  genoeg (besluit van de gebruiker).
- **Meerdere losse forges zonder hub.** Het issue noemt "achter één hub of los". Met "zoveel
  mogelijk forge" valt los af: lighthouse praat met één forge, de hub.
- Devboxes als uitvoerplek.
- Automatisch uitwijken naar een andere node.

**Anti-doelen** (zou mislukt zijn, ook als het werkt)

- Een run die op de verkeerde node start omdat zijn eigen node vol of onbereikbaar is. "Moet echt op
  de Mac" betekent wachten, niet uitwijken.
- Een tweede start van dezelfde run doordat een node even niet antwoordt.
- Stop, Kill of Remove die na een herstart van `tick` naar de hub zelf gaan in plaats van naar de node
  van de run.
- Een hostnaam, node-ID of machine-opzet in code of fixtures.

## Beperkingen

- Fleet-routes bestaan alleen op een forge met fleet. Lighthouse gebruikt ze alleen voor een node
  die niet `self` is. Voor `self` blijven de huidige `/api/v1/…`-paden, zodat een losse forge zonder
  fleet (en v0.5.1) ongewijzigd werkt.
- Een node die niet `self` is, vereist forge v0.7.0 of later op de hub. `docs/host-assumptions.md`
  noemt dat.
- `[dispatch.forge]` blijft de enige forge waar lighthouse mee praat: de hub of een losse forge.
  `token_file` is het token van die forge. Voor andere nodes is geen eigen token nodig; de hub
  autoriseert de proxy.
- De geheugenrem meet `/proc/meminfo` van de tick-host. Die host is de host van `self`. De geheugenrem
  geldt dus alleen voor `self`.

## Aanpakken

**A. Hub-routes nu, run-inhoud in een vervolgissue (gekozen).** #187 regelt alles wat de hub kan.
Alle acceptatiecriteria zijn met `httptest` te toetsen. Een vervolgissue regelt de run-inhoud op een
andere node. Nadeel: de Mac draait pas echte runs na dat vervolg.

**B. Alles in #187, met een eigen kanaal.** De launcher op de node haalt de drager op bij
`lighthouse serve`, en de hook stuurt `permission.json` terug. Afgevallen: een tweede kanaal naast
forge, tegen "zoveel mogelijk forge", en het issue wordt veel groter.

**C. Op een andere node de ACP-lane van forge.** De agent draait als forge-chat. Transcript, stop en
permissie lopen via de chat-API. Afgevallen voor #187: een nieuwe manier om agents te draaien, en
onbekend of `--permission-mode`, MCP-config en `--session-id` via `acp_options` kunnen. Wel de eerste
kandidaat voor het vervolg.

De gebruiker koos A.

## Ontwerp

### Configuratie (`host.toml`)

```toml
[dispatch]
slots = 4                 # capaciteit van self, zoals nu

[dispatch.forge]          # de hub, of een losse forge; ongewijzigd
url = "http://127.0.0.1:8091"
token_file = "~/.kenn/forge/auth_token"
unit_prefix = "…"

[dispatch.nodes.<naam>]   # nieuw: een node van de fleet, niet de hub zelf
node_id = "<node-ID van forge>"
slots = 1

[dispatch.profiles.<profiel>]
target_key = "…"
node = "<naam>"           # nieuw, optioneel; zonder: self
```

- `<naam>` is een label van lighthouse, alleen in `host.toml`.
- `node_id` is verplicht en is de `host_key` in de fleet-routes. Lighthouse bewaart in de runstaat
  het `node_id`, niet de naam: een hernoemd label breekt geen open run.
- `slots` is verplicht en minstens 1.
- De naam `self` is gereserveerd: dat is `[dispatch.forge]` zelf, met de `slots` uit `[dispatch]`.
- Een onbekende sleutel, een ontbrekend `node_id`, een dubbel `node_id`, een `node_id` gelijk aan
  `self`, of een profiel met een `node` die niet bestaat: fout met de sleutel erin, zoals nu.
- `max_software` blijft één getal voor alle nodes: het software-profiel draait in de devcontainer van
  de Beelink.

### Keuzeregel

De keuze is per issue, niet per repo. Twee stappen:

1. **Issue → profiel (repo, `lighthouse.toml`).** Het locatielabel van het issue (prefix
   `labels.location_prefix`, in paia `1-loc:`) kiest het profiel via een nieuwe tabel:

   ```toml
   [controller]
   profile = "software"        # zonder locatielabel, zoals nu

   [controller.locations]      # nieuw: waarde na de prefix → profiel uit host.toml
   container = "software"
   beelink = "host"
   mac = "mac"
   ```

2. **Profiel → node (host, `host.toml`).** De `node` van het profiel. Zonder `node`: `self`.

Regels:

- Geen locatielabel, of geen `labels.location_prefix` in de repo: het profiel uit
  `[controller] profile`, zoals nu. Een repo zonder locatielabels (zoals lighthouse) werkt dus
  ongewijzigd.
- Een locatielabel waarvan de waarde niet in `[controller.locations]` staat, of meer dan één
  locatielabel: de run start niet. Decide stelt hem uit met de reden (`locatie <waarde> onbekend`,
  `meer dan één locatielabel`). Nooit stil terugvallen op het standaardprofiel: "moet echt op de
  Mac" mag niet op de Beelink belanden. *(Keuze van de ontwerper.)*
- Een profiel uit `[controller.locations]` dat niet in `host.toml` staat: dezelfde fout als nu bij
  een onbekend `[controller] profile`.
- Het board levert nu geen labels. De board-query (`internal/forge/board.go`) haalt de labels van
  het issue op, en `dispatch.Item` krijgt `Labels`.

Het profiel wordt zo een eigenschap van de run, niet van de repo. Wat nu `repo.Controller.Profile`
leest, leest het profiel van de run: de software-telling in decide
(`internal/dispatch/decide.go:1315`), `Env.Profile` in apply, en het `Profile`-veld van de
kenn-forge-adapter (Stop en Transcript kiezen daarop de devcontainer). De adapters in de
`places`-map worden daarom gesleuteld op `(repo, node, profiel)`.

De dispatcher legt profiel en `node_id` vast bij de claim (`Take`). Een label dat verandert terwijl
de run loopt, verandert zijn profiel en node niet.

Hervatten en `retry` maken een nieuwe rij. Die rij erft de node van de rij waar hij op volgt: bij
hervatten de rij met dezelfde `session_id`, bij `retry` de vorige rij met dezelfde `trigger_key`.
Daar staan de sessie en de workspace. Elke andere nieuwe start, ook fix-pr, krijgt profiel en node
volgens het label en de config op dat moment. Krijgt een profiel een andere node, dan blijft een
open run dus op zijn oude node. Hervatten en `retry` erven ook het profiel. *(Keuze van de
ontwerper.)*

`Decide` moet de node van elke geplande start en hervatting kennen, want de rem per node heeft hem
nodig. De invoer van `Decide` krijgt daarom de node van het profiel en, per run in de historie, zijn
node.

### Runstaat

- Nieuwe kolom `runs.node TEXT NOT NULL DEFAULT 'self'` in een nieuwe migratie. Bestaande rijen worden
  `self`.
- `Claim` krijgt `Node`. `SetWorkplace` blijft. Alle lookups op `workspace_id` worden lookups op
  `(node, workspace_id)`, want een id is alleen per node uniek (`internal/runs/workspaces.go`).

### Werkplek per node

- `dispatch.Env.Workplace(repo)` wordt `Workplace(repo, node)`. De `places`-map in
  `internal/cli/tick.go` wordt gesleuteld op `(repo, node)`. De sweep (`tick.go:303`) doet hetzelfde.
- De kenn-forge-adapter krijgt een veld `Node`. Bij `self` gebruikt hij de huidige paden, bij een
  andere node het voorvoegsel `/api/v1/fleet/hosts/{node_id}`. Er blijft één `Client` met het token
  van `[dispatch.forge]`.
- Per methode, voor een node die niet `self` is:
  - **Prepare:** lijst en maak workspaces via de fleet-routes. Sla `FollowRemote` over: het
    worktree-pad bestaat niet op de tick-host. Documenteer het gat (zie *Vervolg*).
  - **Spawn:** handoff via de fleet-route. De drager wordt geschreven zoals nu. Op een andere node
    leest niemand hem; dat is het vervolg.
  - **Sessions:** lees `…/workspaces/{id}/runtime` in plaats van `/agent-sessions`, en geef per
    sessie de forge-`key` en de claude-`session_id`.
  - **Stop:** zoek per gegeven session-id de forge-`key` op via Sessions, en stuur
    `DELETE …/runtime/sessions/{key}`. Geen `systemctl` en geen `podman`. Een session-id zonder
    sessie in forge is al weg. Een session-id die niet te meten is (Sessions faalt), is een fout,
    nooit "weg".
  - **Kill:** idem, alleen voor de gegeven sessies.
  - **Remove:** `DELETE …/workspaces/{id}` via de fleet-route; de drager weg zoals nu.
  - **Lookup:** `GET …/workspaces/{id}` via de fleet-route.
  - **Transcript:** geeft een fout "niet beschikbaar op node <id>". De tick behandelt die als "niet
    gemeten", zoals een ontbrekend transcript nu.
- Voor `self` verandert niets: `systemctl`, `podman` en `FollowRemote` blijven zoals ze zijn.

**Sessie-identiteit blijft de claude-`session_id`.** De regels voor eigenaarschap (`Own`, `Busy`,
`Earlier`, `AdoptSessions`) en de `Workplace`-interface blijven op `session_id` werken. Op een andere
node krijgt de agent pas een `--session-id` als de drager daar aankomt (het vervolg). Tot dan herkent
lighthouse de eigen agent op een andere node alleen aan de forge-key van `Spawn` (een onzekere spawn
blijft vreemd): `busy` stelt latere starts uit, de sweep
laat de workspace staan, en Stop vindt niets om te stoppen. Dat is veilig, en het is de reden dat
een andere node pas na het vervolg echte runs draait. *(Keuze van de ontwerper: geen tweede
identiteit naast `session_id` voor een overgangsfase.)*

**404 op een fleet-route.** Forge kan via de hub "workspace weg" en "node onbereikbaar" niet altijd
onderscheiden. Op een node die niet `self` is, geldt een 404 daarom alleen als weg (`ErrGone`, "al
verwijderd") als `/api/v1/snapshot?include_peers=true` die node op dat moment als bereikbaar meldt. Anders is de
uitkomst onzeker (`ErrUncertain` of "niet meetbaar"), nooit weg. *(Keuze van de ontwerper.)*

**Sweep op een andere node.** `workplace.Cleanup` meet `Dirty` en `Unpushed` met lokale git op het
worktree-pad. Op een andere node bestaat dat pad niet op de tick-host. Als het pad toevallig wel
bestaat, meet git de verkeerde worktree. Op een node die niet `self` is, slaat Cleanup die metingen
daarom over en noteert de workspace als "niet meetbaar". Hij ruimt hem niet op. Opruimen op een
andere node komt met het vervolg. *(Keuze van de ontwerper.)*

**Drager en leftover-sleutels per node.** Een workspace-id is alleen per node uniek. Voor een node
die niet `self` is, wordt het pad van de drager `<statemap>/carriers/<node_id>/<workspace-id>.json`.
`self` houdt het huidige pad. De leftover-rijen in `tick_state` krijgen de node in hun sleutel; een
bestaande sleutel zonder node is `self`. *(Keuze van de ontwerper.)*

### Rem per node

- `slots` telt per node: open en geparkeerde runs met `node = X` tegen de `slots` van X.
  `readHostFacts` (`internal/cli/brake.go`) telt per node. `Brake` krijgt de bezetting en `slots` per
  node.
- `apply` (`internal/dispatch/brake.go`) houdt per node bij hoeveel starts het al toeliet. Een
  gesloten rem op node X zet alleen de starts van X om in een uitstel. Starts op andere nodes gaan
  door.
- Geheugen, usage, pauze en `max_software` blijven zoals nu. Geheugen geldt alleen voor `self`.
- De uitsteltekst noemt de node: `slots vol op <naam>`.

### Onbereikbare node

- Er is geen aparte bereikbaarheidscheck vooraf. Een onbereikbare node faalt in Prepare met een
  fout, en de run sluit zoals een mislukte Prepare nu (`start_failed`, daarna `retry`).
- Een fout bij één run houdt de andere starts in dezelfde tick niet tegen.
- Een onbereikbare node in Spawn valt onder de bestaande `ErrUncertain`-regel: de run blijft staan,
  en er komt geen tweede POST. Dat is AC 4, tweede helft.

## Tests

- Een nep-hub (`httptest`) die `/api/v1/fleet/hosts/{id}/…` doorstuurt naar een nep-forge per node.
  De nep-forge is de bestaande `newFakeForge` (`kennforge/adapter_test.go`). Minstens twee nodes,
  met willekeurige id's.
- De contract-suite (`workplacetest`) draait ook tegen een adapter op een andere node.
- Config: een `host.toml` zonder nodes geeft hetzelfde resultaat als nu (de bestaande tests blijven
  groen), en de foutgevallen hierboven geven elk een fout met de sleutel.
- Keuze: een issue met locatielabel `mac` krijgt het profiel uit `[controller.locations]` en de node
  van dat profiel. Zonder label: `[controller] profile`. Onbekende waarde of twee labels: uitstel
  met reden, geen start. Een repo zonder `location_prefix` gedraagt zich als nu.
- Dispatcher: een profiel met `node` zet die node in de runstaat. Na een nieuwe `Open` van de database
  (herstart van `tick`) gaan Stop en Remove naar die node.
- Rem: node A vol, node B vrij: de start op B gaat door, die op A wordt uitgesteld.
- Onbereikbare node: Prepare faalt op A, de start op B in dezelfde tick gaat door. Een time-out in
  Spawn op A geeft geen tweede POST.
- Een 404 op node A terwijl de snapshot A als onbereikbaar meldt: de run sluit niet en start niet
  opnieuw.
- Hervatten en `retry` erven de node van de vorige rij, ook nadat het profiel een andere node kreeg.
- Twee nodes met hetzelfde workspace-id: Remove op de ene laat de drager en de leftover-rij van de
  andere staan.
- Sweep op een andere node: geen lokale git-aanroep, de workspace blijft staan.

## Documentatie

- `docs/workplace.md`: nodes, fleet-routes, welke methoden op een andere node anders werken, en het
  gat (drager, transcript, permissie, `FollowRemote`).
- `docs/tick.md`: `[dispatch.nodes.<naam>]`, `node` in een profiel, de keuzeregel, de rem per node.
- De docs van `lighthouse.toml` (waar `[controller] profile` nu staat): `[controller.locations]`.
- `docs/host-assumptions.md`: forge v0.7.0 of later op de hub voor een node die niet `self` is.
- Een fragment in `changelog.d/`.

## Vervolg

[#327](https://github.com/CorneeldH/lighthouse/issues/327): **de run-inhoud op een andere node.** Drager (en daarmee `--session-id`), transcript,
`permission.json`, het volgen van de remote branch bij Prepare, en de git-metingen van de sweep. Eerste kandidaat: de ACP-lane van forge (aanpak C). Tot dat issue klaar is,
start een run op een andere node wel een workspace en een target, maar zonder de argumenten van
lighthouse.

## Aan de bouwer

- De volgorde van de stappen en de verdeling over commits.
- De vorm van de nep-hub en hoe de bestaande tests hem hergebruiken.
- De exacte JSON-velden van `…/workspaces/{id}/runtime`. Meet ze tegen de forge-bron van v0.7.0, niet
  tegen het geheugen.
- Of `Brake` per node een map of een lijst wordt, zolang de golden fixtures in
  `internal/dispatch/testdata/decide/brake-*.json` leesbaar blijven.
