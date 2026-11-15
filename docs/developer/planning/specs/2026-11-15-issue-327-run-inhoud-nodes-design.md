# Ontwerp: run-inhoud op een andere kenn-forge-node

Issue: [#327](https://github.com/CorneeldH/lighthouse/issues/327) · Datum: 2026-11-15 · Bouwt op
[#187](https://github.com/CorneeldH/lighthouse/issues/187) en zijn
[spec](2026-11-13-issue-187-forge-nodes-design.md). Sub-issue in paia:
[paia#1764](https://github.com/CorneeldH/personal_ai_agent/issues/1764) (de launcher op macOS), samen
op te pakken.

## Aanleiding en doel

#187 laat de dispatcher via de hub met meerdere kenn-forge-nodes werken. Een run op een node die niet
`self` is, krijgt een workspace en een gestarte target. Lighthouse kan die run daarna niet volgen.
Vijf dingen lopen nu via bestanden of git op de tick-host, en forge vervoert ze niet: de drager (met
daarin `--session-id`), het transcript, `permission.json`, `FollowRemote` bij Prepare en de
git-metingen van de sweep. Daardoor wacht elke echte run op een andere node, zoals de runs van paia die
op de Mac moeten draaien.

Bij het uitwerken kwamen er nog twee bij die #187 ook op een node overslaat: `Relocate` bij een start
(`internal/dispatch/apply.go`, kijkt met `os.Stat` in de worktree) en de usage-rem van een run (de hook
`usage-gate` leest de pauzevlag en het alarm op de host waar hij draait, en een geparkeerde run schrijft
zijn hervatmarker daar; de tick leest markers alleen van zijn eigen schijf, `controllerMarkers` in
`internal/cli/brake.go`).

Doel: een run op een andere node gedraagt zich voor lighthouse precies als een run op `self`. Dezelfde
drager, dezelfde `--session-id`, dezelfde transcriptmeting, dezelfde regel voor eigen werk en
dezelfde metingen bij het opruimen.

## Besluiten van de gebruiker

1. **Aanpak B: de controller staat los van forge.** Claude draait op elke node precies als op `self`:
   zelfde launcher, zelfde drager, zelfde `--session-id` en zelfde transcriptmeting. De ACP-lane van
   forge (de agent als forge-chat) valt af.
2. **Het tweede kanaal is pull.** De tick haalt op en zet neer. Op elke node draait `lighthouse serve`
   als service, met een token. Een node stuurt niets uit zichzelf naar de tick-host.
3. **Git loopt ook via `serve` op de node**, met dezelfde Go-code als op `self`. Niet via de git-velden
   van forge.
4. **Het macOS-pad van de launcher is paia#1764**, een sub-issue van #327 dat tegelijk loopt.
5. **De usage-rem hoort erbij.** Een run op een node ziet pauze en alarm zoals op `self`, en de tick
   hervat een geparkeerde run op een node zoals op `self`.

## Doelen, niet-doelen, anti-doelen

**Doelen**

1. Een run op een andere node start met de argumenten en de `session_id` van lighthouse. Lighthouse
   herkent de sessie als de zijne (AC 1).
2. De tick meet transcript en permissie-prompt van zo'n run, met dezelfde code als op `self` (AC 2).
3. Prepare zet de workspace op de remote branch, met dezelfde regel voor eigen werk als op `self`
   (AC 3).
4. De sweep ruimt een workspace op een andere node alleen op na dezelfde metingen voor vuil en
   niet-gepusht als op `self` (AC 4).
5. `Stop` en `Kill` beëindigen de claude van een run op een andere node. Zonder dat wacht een
   hervatting voor altijd (#206), en blijft `liveOwn` een volgende start tegenhouden.
6. De usage-rem werkt op een node: de hook `usage-gate` ziet de pauzevlag en het alarm van de tick, en
   een run die parkeert, wordt hervat en telt mee in de `slots` van zijn node.
7. `docs/workplace.md` beschrijft de route; `docs/install.md` beschrijft `serve` op een node, ook op
   macOS.

**Niet-doelen**

- De launcher op macOS. Dat is paia#1764. Lighthouse levert de drager op de node en de contracten;
  paia verbruikt hem.
- De Mac als spoke van de hub (paia#1059). Dat is een voorwaarde voor een echte run, geen werk van
  lighthouse.
- Een geheugenrem op een andere node. Dat blijft zoals in #187: alleen `slots`.
- Een kanaal waarin een node uit zichzelf naar de tick-host stuurt (push).
- Een algemene route om opdrachten uit te voeren (`exec`). `serve` biedt alleen benoemde handelingen.
- Iets veranderen aan een run op `self`.

**Anti-doelen** (zou mislukt zijn, ook als het werkt)

- Een onbereikbare node of `serve` die als "weg", "stil" of "schoon" telt. Onbereikbaar is altijd
  "niet meetbaar" of onzeker.
- Een route op `serve` die zonder token werkt, of die leest of schrijft buiten vaste, benoemde plekken:
  de worktree die de forge van de node voor de workspace meldt, de statemap van de node
  (`carriers/`, `runs/<sessie>/`), de transcriptmap van de sessie, en voor de usage-rem de map van
  `PAUSE` en de alarmspiegel (`lighthouse/controller`) en de markermap (`park.MarkerDir`).
- Een tweede spawn doordat een node of `serve` even niet antwoordt.
- Een meting op een node die met andere code rekent dan op `self`.
- Een hostnaam, node-ID of machine-opzet in code of fixtures.

## Beperkingen

- De hub blijft de enige weg voor de levenscyclus van de workspace (lijst, aanmaken, handoff,
  `DELETE`). Dat blijft zoals #187 het bouwde.
- Op elke node die niet `self` is, draait dezelfde `lighthouse`-binary als `serve`. Zijn versie kan
  afwijken van die van de tick.
- `serve` op de node praat met de forge van die node zelf (zijn eigen `[dispatch.forge]`, met het token
  van die forge). Zo kent hij het `worktree_path` van een workspace zonder het van de tick te hoeven
  geloven.
- De Mac heeft geen systemd. Wat op `self` via `systemctl` gaat, moet op een node zonder systemd anders.
- `lighthouse ask` (uit de runinstructies) moet op de node draaien: de binary op het `PATH` van de
  agent en een GitHub-token. Dat regelt de opzet van de node, niet de code van deze spec.

## Aanpakken

**A. De ACP-lane van forge.** De agent draait als forge-chat. Forge vervoert transcript, stop en
permissie. Afgevallen: lighthouse geeft op een node zijn argumentcontract op, en de controller gaat
afhangen van forge.

**B-push. Alleen de tick-host draait `serve`.** De launcher haalt de drager op, de hooks sturen
permissie en transcript terug. Afgevallen: een push die mislukt, ziet eruit als stilte, en dan
beslissen de stilteregel en `max_runtime` op een onjuiste meting. Opvangen vraagt een hartslag of een
tweede bron. Bovendien rekent de node dan de transcriptmeting uit, of moet hij megabytes per hook
versturen.

**B-pull. Op elke node draait `serve` en de tick haalt op (gekozen).** Een onbereikbare node is "niet
meetbaar", en dat geval kent de tick al. De meting gebeurt op de tick met dezelfde code als op `self`.
De prijs: een service, een token en een `tailscale serve`-ingang per node.

**Git: via forge of via `serve`.** Forge levert `POST …/workspaces/{id}/pull` en de velden
`worktree_dirty`, `commits_ahead` en `push_state.pushed`. Dat is niet dezelfde regel. `pull` wisselt niet
van branch; `commits_ahead` telt tegen de upstream, zonder `git cherry`, dus na een force-push lijkt
alles eigen werk; de velden zijn een gecachte meting (`enrichment_refreshed_at`); en de uitzondering
voor de head van een gemergede PR ontbreekt. Gekozen: via `serve`, met `workplace.FollowRemote`,
`workplace.Dirty` en `workplace.Unpushed` op de node.

## Ontwerp

### Overzicht

Een run op een andere node gebruikt twee kanalen:

```
tick (self) ──▶ hub (forge) ──fleet──▶ forge op de node      levenscyclus: lijst, aanmaken, handoff, DELETE
tick (self) ──HTTPS + token──▶ lighthouse serve op de node   wat op self lokaal gebeurt
```

De kenn-forge-adapter met `Node != ""` (uit #187) houdt zijn hub-routes. Alles wat hij op `self` lokaal
doet, laat hij op een node over aan `serve` van die node. Op de node voert `serve` dat uit met een
kenn-forge-adapter voor zijn eigen forge (`Node == ""`), dus met dezelfde code als de tick op `self`.

### Configuratie

**Op de tick-host** (`host.toml`), per node twee nieuwe, verplichte sleutels:

```toml
[dispatch.nodes.<naam>]
node_id = "<node-ID van forge>"
slots = 1
serve_url = "https://<ingang van de node>"   # nieuw: de tailscale-serve-ingang van serve op de node
serve_token_file = "/home/<user>/.config/lighthouse/node-<naam>.token"   # nieuw, absoluut
```

- Zonder `serve_url` of `serve_token_file` is de node-config ongeldig: fout met de sleutel, zoals de
  andere fouten van #187. *(Keuze van de ontwerper: een node zonder `serve` kan geen echte run draaien,
  en een run die half werkt, is erger dan een run die niet start.)*
- **Migratie van #187.** Een bestaande `[dispatch.nodes.*]` zonder deze sleutels geeft vanaf de
  volgende tick die fout, en `lighthouse serve` start dan ook niet (beide laden `LoadDispatch`). De fout
  noemt de sleutel en de stap (de twee sleutels toevoegen). Het changelog-fragment en `docs/tick.md`
  noemen dit als brekende wijziging. Een run die onder #187 op een node open stond, wordt zoals elke
  andere run op die node gemeten via `serve`. De sweep wist de achtergebleven tick-kant-drager
  (`carriers/<node>/<id>.json`) van een gesloten run van die node eenmalig, omdat `Discard` nu naar
  `serve` gaat. *(Eigen keuze na spec-review, r1-f4.)*
- `serve_url` moet `https` zijn, of `http` naar een loopback-adres (voor de tests). *(Keuze van de
  ontwerper.)*
- Het token wordt per verzoek gelezen, net als het forge-token. Een ontbrekend of leeg tokenbestand is
  een fout van dat verzoek.
- `mcp_config` van een profiel met `node` is een pad **op die node**. De tick leest het niet.

**Op de node** (`host.toml` van de node):

```toml
[dispatch.forge]          # de forge van de node zelf, zoals op self
url = "http://127.0.0.1:8091"
token_file = "/Users/<user>/.kenn/forge/auth_token"   # absoluut: niets breidt ~ uit
unit_prefix = "…"         # alleen waar systemd de runs in units zet

[node]                    # nieuw: zet de node-routes van serve aan
token_file = "/Users/<user>/.config/lighthouse/node.token"
```

- Zonder `[node]` heeft `serve` geen node-routes (404), zoals nu. De Beelink als tick-host heeft
  ze dus niet.
- Met `[node]` en zonder bruikbaar `[dispatch.forge]` start `serve` niet: fout met de sleutel.
  *Bruikbaar* = het tokenbestand bestaat en is niet leeg (`DefaultDispatch` vult `url` en `token_file`
  altijd, dus de tabel ontbreekt nooit). *(Eigen keuze na spec-review, r1-f10.)*
- De statemap van de node volgt dezelfde regel als op de tick
  (`${XDG_STATE_HOME:-$HOME/.local/state}/lighthouse/dispatch`). Daar lezen de launcher (paia) en de hook
  `permission-check`.

### De node-routes van `serve`

Alle routes staan onder `/api/node/v1/`, vragen `Authorization: Bearer <token>` (vergeleken in
constante tijd) en noemen een workspace bij zijn forge-id. Ze zijn alleen bedoeld voor de tick.

| Route | Doet op de node | Methode van de adapter op de tick |
|---|---|---|
| `GET  info` | versie van de binary, versie van de node-API, platform | (bij elke eerste aanroep per tick) |
| `PUT  workspaces/{id}/launch` | runinstructies en drager schrijven | `Spawn`, vóór de handoff |
| `DELETE workspaces/{id}/carrier` | drager wissen | `Spawn` (niet aangenomen), `Discard`, `Remove` |
| `POST workspaces/{id}/follow` | `workplace.FollowRemote` | `Prepare` |
| `GET  workspaces/{id}/sessions` | `Sessions` van de forge van de node (`/agent-sessions`) | `Sessions` |
| `GET  workspaces/{id}/transcript/{session}?from=…` | de nieuwe bytes van elk transcriptbestand van de sessie, als tar | `Transcript` |
| `POST workspaces/{id}/relocate` | `dispatch.Relocate` op het commando van de start | apply, vóór de claim |
| `GET  runs/{session}/permission` | de ruwe `permission.json` | de permissiemeting van de tick |
| `GET  workspaces/{id}/git?merged_head=<sha>` | `workplace.Dirty` en `workplace.Unpushed` | `Cleanup` |
| `POST workspaces/{id}/stop` | `Stop` met de gegeven sessie-id's | `Stop` |
| `POST workspaces/{id}/kill` | `Kill` met de gegeven sessie-id's | `Kill` |
| `PUT  usage` | `PAUSE` en de alarmspiegel van de tick schrijven, of wissen | elke tick, per node |
| `GET  markers` | de controller-markers van de node (`park.ReadMarkers`) | `readHostFacts` (rem en hervatten) |
| `DELETE markers/{session}?key=<key>` | `park.RemoveMarker` op de node | apply, na een hervatting |

Gemeenschappelijk:

- **Het pad komt van de node.** De routes die de worktree raken (`follow`, `relocate`, `sessions`,
  `transcript`, `git`, `stop`, `kill`) zoeken het `worktree_path` van de workspace op bij de eigen
  forge van de node (`GET /api/v1/workspaces/{id}`). `serve` controleert dat het absoluut is en dat
  `git -C <pad> rev-parse --show-toplevel` hetzelfde pad geeft, beide na `filepath.EvalSymlinks` (op
  macOS geeft git `/private/var/…` voor `/var/…`). *(Keuze van de ontwerper.)* De tick stuurt nooit
  een pad. Een 404 van de eigen forge is een 404 van de route (`workspace gone`), en de tick behandelt
  hem zoals een 404 van de hub (zie *Storingen*).
- **De statemap-routes zoeken niets op.** `launch`, `carrier` en `permission` raken alleen de statemap
  van de node, op het workspace-id of het sessie-id. Ze werken ook als forge de workspace niet meer
  kent: `Remove` en het `ErrGone`-pad van `Cleanup` wissen de drager pas nadat de workspace weg is.
  *(Keuze van de ontwerper.)*
- **Veilige namen.** Workspace-id en sessie-id volgen dezelfde patronen als nu (`safeIDPattern`, de
  UUID van de drager, `safeSession`). Een ander id: 400, vóór elke handeling. `merged_head` is leeg of
  `^[0-9a-f]{40}$`, anders 400, want het gaat als argument naar git. *(Eigen keuze na spec-review,
  r1-f6.)*
- **Het profiel komt van de tick.** De adapter van de node kent het profiel van de run niet, en `Stop`
  (`UnitName(prefix, profile, dir)`) en `Kill` (de tak `software`: de devcontainer) hangen ervan af. Elke
  route waarvan de adaptermethode `Profile` leest (`transcript`, `stop`, `kill`) krijgt het profiel mee:
  als query bij `transcript`, in de body bij `stop` en `kill`. `serve` toetst het aan de bekende
  profielen van de adapter en weigert een leeg of onbekend profiel met 400, zodat een node nooit stil
  op de host zoekt naar een claude die in de container draait. Een test: `Kill` op een node met het
  profiel `software` doodt binnen de container. *(Eigen keuze na spec-review, r1-f1.)*
- **Fouten zijn gegevens.** Een `*workplace.OwnWorkError` uit `follow` komt terug als 409 met zijn
  velden (workspace, aantal eigen commits, vuil). De tick maakt er weer een `*workplace.OwnWorkError`
  van, zodat de dispatcher hem afhandelt zoals op `self` (`returned`, `reason_own_work`).
- **Geen meting op de node.** `transcript` en `permission` geven ruwe bestanden. De tick rekent ze uit
  met `ReadTranscript` op zijn spiegel (zie *Transcript*) en met de parser van `dispatch.ReadPermission`. Zo rekent een
  node met een andere versie nooit anders. *(Keuze van de ontwerper.)* `git` geeft twee booleans, want
  daar is de meting de git-aanroep zelf. Die moet op de node draaien.

### `launch`: drager en runinstructies op de node

Op `self` schrijft de tick `<statemap>/runs/<sessie>/instructions.md` en de drager in zijn eigen
statemap. De drager noemt absolute paden (`--append-system-prompt-file`, `--mcp-config`). Op een node
moeten die paden van de node zijn.

- De tick stuurt naar `PUT …/launch`: de velden van de `LaunchSpec` (repo, issue, sessie, mode,
  profiel, prompt, native, het pad van `mcp_config`) en de inhoud van de runinstructies.
- `serve` schrijft de runinstructies in zijn eigen statemap. Daarna bouwt hij de drager met
  `workplace.NewCarrier` en schrijft hem met `workplace.WriteCarrier` naar
  `<statemap van de node>/carriers/<workspace-id>.json`: het pad dat de launcher op de node leest, zonder
  `node`-submap, want op de node zelf is dit `self`. Rechten en atomair schrijven blijven zoals in het
  dragercontract.
- `serve` geeft de geschreven drager terug. De tick schrijft niets naar zijn eigen
  `carriers/<node>/`. *(Keuze van de ontwerper: de drager van #187 op de tick-host leest niemand.)*

`Spawn` op een node wordt:

1. Een fout bij `launch` (verbinding, 4xx, 5xx, time-out): `ErrNotAccepted`. Er is nog geen handoff
   verstuurd. Bij een time-out of 5xx van `launch` stuurt de tick daarna een `DELETE …/carrier`; lukt
   dat niet, dan komt die fout erbij, zoals nu bij `RemoveCarrier`.
2. De handoff via de hub, met de drie uitkomsten van nu.
3. Niet aangenomen: `DELETE …/carrier`. Onzeker: de drager blijft staan.

De herlezing na een 5xx of een onzekere handoff gebruikt `Sessions` via `serve` (hieronder), met de
regel van nu: een nieuwe key met het `session_id` van de run, of nog zonder `session_id`.

### `Sessions`: zoals op `self`

Op `self` leest `Sessions` `/api/v1/workspaces/{id}/agent-sessions`, met `session_id`, de stand en
`updated_at`. De hub heeft die route niet in de fleet, en `/runtime` kent geen `session_id` (#187).
Op een node leest `serve` daarom `/agent-sessions` bij zijn eigen forge en geeft dezelfde velden terug.
*(Keuze van de ontwerper.)*

Gevolg: op een node gelden weer dezelfde regels als op `self`. `Own`, `Busy`, `AdoptSessions` en `Stand`
krijgen het `session_id`, `Working` en `SessionAt`. De beperkingen uit *Een run op een andere node* in
`docs/workplace.md` (altijd `Waiting`, `SessionAt` nul, een onzekere spawn blijft vreemd) vervallen.

`Prepare`, `Lookup` en `Remove` blijven via de hub. De bereikbaarheidsregel van #187 voor een 404 op de
hub blijft gelden.

### `Transcript` en permissie

- `Transcript` op een node: `serve` bepaalt de transcriptmap met dezelfde code als op `self`
  (`HostTranscriptDir`, of de devcontainer bij `software`) en stuurt `<sessie>.jsonl` en
  `<sessie>/subagents` als tar. Het lezen vanaf een offset is nieuwe code, op de host en in de
  container: `containerScript` stuurt nu hele bestanden en `untarTranscript` pakt uit in een
  verse tijdelijke map. Alleen `ReadTranscript` blijft de gedeelde meting. *(Eigen keuze na
  spec-review, r1-f5.)* Staat de sessie er niet, dan is het 404 `no transcript`, en de twee 404's dragen een
  eigen machineleesbare `code` (`workspace_gone`, `no_transcript`) waarop de tick afbeeldt (nooit op
  de status alleen), en de tick
  maakt er `fs.ErrNotExist` van, zoals nu. Het profiel geeft de tick mee als query.
- **Een spiegel op de tick, die alleen aanvult.** `ReadTranscript` leest niet alleen een staart:
  `scanWork` (achtergrondwerk, open beurt) en `scanStart` (eerste assistent-beurt, onbekend commando)
  lezen het hoofdtranscript van begin tot eind. Het hele transcript moet dus mee, maar niet elke tick
  opnieuw. De tick houdt per sessie een spiegel in zijn statemap
  (`<statemap>/mirror/<node_id>/<sessie>/`, map 0700, bestanden 0600). Hij vraagt per bestand alleen de
  bytes vanaf de lengte van zijn spiegel (`?from=<bestand>:<offset>,…`). `serve` antwoordt per bestand
  met zijn huidige lengte en de nieuwe bytes. Is een bestand op de node korter dan de spiegel, dan
  stuurt `serve` het hele bestand en vervangt de tick zijn kopie. De tick neemt per sessie een `flock` op de
  spiegelmap voor hij leest en bijschrijft, want `lighthouse run` neemt geen `tick.lock` en meet
  ook transcripten: twee processen mogen nooit dezelfde delta bijschrijven. Een test met twee
  gelijktijdige ophalingen geeft een spiegel gelijk aan het bestand op de node. *(Eigen keuze na
  spec-review, r1-f3.)* Daarna draait de tick
  `ReadTranscript` op de spiegelmap, zonder enige wijziging aan `ReadTranscript`. Claude schrijft
  transcripten alleen aan het eind bij. *(Keuze van de ontwerper.)* De spiegel verdwijnt als de run
  sluit, en de sweep ruimt achtergebleven spiegels van gesloten runs op.
- De permissiemeting in `runProbe.measure` leest nu `dispatch.ReadPermission(m.stateDir, …)`. Voor een
  run op een node leest de tick hem via `serve`. Geen bestand: `nil`, zoals nu.
  *(Aan de bouwer: of dat een optionele interface op de `Workplace` wordt of een tak in `runProbe`.)*

### `Prepare`: de remote branch volgen

Na het vinden of aanmaken van de workspace via de hub (zoals #187) roept de tick `POST …/follow` aan met
`branch` en `created`. `serve` voert `workplace.FollowRemote` uit op het pad dat hij zelf opzocht. De
uitkomst is dezelfde als op `self`: omgeschakeld, fast-forward, of een `*workplace.OwnWorkError`.

### `Cleanup` en de sweep

`Cleanup` stopt nu op een node meteen, vóór `Lookup`, `Sessions` en `Stop`
(`not measurable: node …: no local git`, `internal/workplace/lifecycle.go`). Die vroege uitstap vervalt.
Op een node doorloopt `Cleanup` dan dezelfde tien stappen als op `self`: `Lookup` en `Remove` via de
hub, `Sessions` en `Stop` via `serve`, en in stap 6 en 7 `Dirty` en `Unpushed` via
`GET …/git?merged_head=…`. Een fout van `serve` wordt `not measurable: <fout>`, zoals een git-fout
nu. Het pad uit stap 1 gebruikt `Cleanup` op een node alleen om "geen pad" te herkennen; `serve` zoekt
zijn eigen pad op.

*(Aan de bouwer: hoe `Cleanup` aan de metingen komt, bijvoorbeeld een optionele interface op de
`Workplace` voor `Dirty`/`Unpushed`, met de huidige lokale git als implementatie op `self`.)*

### `Stop` en `Kill`

`serve` voert `Stop` en `Kill` uit met de kenn-forge-adapter van zijn eigen forge, dus met dezelfde
stappen als op `self`. Een leeg sessie-id weigert `serve` vóór elke aanroep, zoals nu.

Op een node zonder systemd heeft `Stop` geen unit. Het signaal is de config van de node: zonder
`unit_prefix` in zijn `[dispatch.forge]` doet de `stop`-route hetzelfde als `kill` (per sessie-id
`pgrep -f`, `pkill -f` en tot vijf keer `pgrep` om te bevestigen). Die terugval zit alleen in de
handler van `serve`. De adapter zelf verandert niet: op `self` blijft een lege `unit_prefix` een fout
van `Stop`, zodat een verkeerd ingestelde tick-host nooit stil naar `Kill` zakt. *(Keuze van de
ontwerper.)*

`remoteStop` uit #187, dat niets stopt, vervalt. Ook de tak `liveOwn` in `apply.go` (een start op een
node wacht op elke levende eigen sessie) vervalt: met `Kill` via `serve` gelden `Busy` en `Earlier` op
een node zoals op `self`. *(Keuze van de ontwerper.)*

### `Relocate`

Bij een start zoekt `dispatch.Relocate` het pad uit het startcommando (een spec of plan) op in de
worktree, met `os.Stat`. #187 slaat dat op een node over. Op een node stuurt apply het commando naar
`POST …/relocate`, en `serve` draait `dispatch.Relocate` op zijn eigen pad. Een fout van `serve` is een
`relocErr`, zoals een fout van `Relocate` nu.

### De usage-rem op een node

Op `self` leest de hook `usage-gate` van een controller-run twee bestanden op de host waar hij draait:
`PAUSE` (`park.PauseFile`) en de alarmspiegel in dezelfde map (`usage.Paths.Mirror`). Parkeert de run,
dan schrijft hij een marker in `park.MarkerDir`. De tick leest die markers in `readHostFacts`
(`controllerMarkers`), hervat de run na het venster en wist de marker met `park.RemoveMarker`.

Op een node blijven de hook en de markers ongewijzigd. Alleen de tick haalt en zet:

- **Pauze en alarm naar de node.** Elke tick stuurt de tick naar elke node de inhoud van zijn eigen
  `PAUSE` en alarmspiegel (`PUT …/usage`), of "geen" als het bestand er niet is. `serve` schrijft ze
  atomair op dezelfde plek op de node, of wist ze. De hook op de node leest dan precies wat hij op
  `self` zou lezen: eerst het eigen alarm van de node (`p.Alarm()`), pas daarna de spiegel, zoals
  `CurrentAlarm`. Die volgorde blijft zo. *(Eigen keuze na spec-review, r1-f9.)* De bytes gaan ongewijzigd mee: de geldigheid (de `until` van een pauze, de reset
  van een alarm) staat in de bestanden zelf.
- **Markers van de node.** `readHostFacts` leest naast de eigen markermap de markers van elke node
  (`GET …/markers`). Een marker van een node telt voor de rem en voor hervatten zoals een eigen marker.
  De bijbehorende run staat al met zijn `node` in de runstaat, en een hervatting erft die node (#187).
- **Marker wissen.** `readHostFacts` onthoudt per marker de node waar hij vandaan komt (een map van
  sessie naar node). Elk pad dat een marker wist, de hervatting én `drop_marker` (een marker van een
  gesloten of vastgelopen run, dat `Decide` uitgeeft zonder node), wist hem op die node
  (`DELETE …/markers/{sessie}?key=…`) in plaats van in de lokale `MarkerDir`. Anders blijft de
  marker van een gesloten run op `mac` staan en telt de rem hem elke tick mee. Een test: een
  node-marker van een gesloten issue is na één tick van de node verdwenen. *(Eigen keuze na
  spec-review, r1-f2.)* met dezelfde sleutelregel als `park.RemoveMarker`. Een fout
  is, net als nu, een logregel en geen fout van de actie: de volgende tick laat de marker weer vallen.

Een node die niet antwoordt bij `PUT …/usage`, houdt de pauze en het alarm van zijn laatste geslaagde
keer. Een pauze of alarm verloopt vanzelf op de tijd in het bestand. Een nieuwe pauze komt dan pas aan
als de node weer bereikbaar is. Dat is dezelfde grens als bij de metingen: wat de tick niet bereikt,
kan hij niet sturen. *(Keuze van de ontwerper.)* Een node die niet antwoordt bij `GET …/markers`:
zijn geparkeerde runs tellen deze tick niet mee, en de rem van die node gaat dicht tot de markers weer
leesbaar zijn (een veld `NodeSlots.Unmeasured`; `readHostFacts` dient `tick` en `lighthouse run`
allebei), zodat een onbekende bezetting nooit een extra start toelaat. *(Keuze van de
ontwerper.)*

### Storingen

| Situatie | Uitkomst |
|---|---|
| `serve` onbereikbaar of 5xx bij `Sessions`, `Transcript` of permissie | De run is deze tick niet meetbaar. Nooit "weg", nooit "stil". |
| `serve` onbereikbaar bij `launch` | `ErrNotAccepted`: geen handoff, de run sluit als `start_failed`, daarna `retry`. |
| `serve` onbereikbaar bij `follow` | Prepare faalt, zoals een onbereikbare node in #187. |
| `serve` onbereikbaar bij `git` | `not measurable: …`; de workspace blijft staan. |
| `serve` onbereikbaar bij `stop` of `kill` | Een fout, zoals een `pkill` die faalt op `self`. |
| 404 `workspace gone` van `serve` | Telt als weg alleen als de hub dezelfde workspace ook niet kent en de node bereikbaar meldt (de regel van #187). Anders onzeker. |
| 401 of 403 | Een configuratiefout. De tick logt hem met de node, en de run is niet meetbaar. |
| Een andere versie van de node-API (`info`) | Een fout voor elke node-aanroep van die tick, met beide versies erin. |

### Versie

`GET info` geeft de versie van de node-API (`1`) en de versie van de binary. De adapter vraagt hem
eenmaal per tick en per node op. Een andere node-API-versie is een fout, nooit een stille terugval.
Een andere binary-versie bij dezelfde API-versie is toegestaan.

### `serve` op een node

- `deploy/launchd/` krijgt een voorbeeld van een launchd-agent voor `lighthouse serve`, naast
  `deploy/systemd/lighthouse.service`. `serve` luistert op loopback; de ingang is `tailscale serve`.
- `docs/install.md` beschrijft `serve` op een node: `[node] token_file`, `[dispatch.forge]` van de
  node, de ingang, en dat `lighthouse ask` op het `PATH` van de agent moet staan.
- `docs/host-assumptions.md`: de rij van `lighthouse.service` krijgt de launchd-variant erbij.

## Tests

- Een nep-node: een `httptest`-server met de node-routes, naast de nep-hub van #187. Minstens twee
  nodes, met willekeurige id's.
- De contract-suite (`workplacetest`) draait tegen een adapter op een andere node met een echte `serve`
  (de node-handler) boven een nep-forge en een git-repo in een tempmap.
- **AC 1:** `Spawn` op een node schrijft de drager in de statemap van de node, met de `session_id` van de
  run en de paden van de node, vóór de handoff. Een mislukte `launch` stuurt geen handoff. `Sessions`
  geeft het `session_id`, en `Own`/`AdoptSessions` herkennen de sessie.
- **AC 2:** `Transcript` en de permissiemeting op een node geven hetzelfde resultaat als op `self` voor
  dezelfde bestanden. `serve` onbereikbaar: niet meetbaar, geen fout van de tick.
- **AC 3:** `Prepare` op een node: een workspace op een andere branch zonder eigen werk wordt omgezet;
  met eigen werk komt een `*workplace.OwnWorkError` met dezelfde velden terug.
- **AC 4:** de sweep op een node: vuil, niet gepusht en gemergede head geven dezelfde uitkomst als op
  `self`. `serve` onbereikbaar: de workspace blijft staan.
- `serve`: zonder token 401, met een verkeerd token 403, zonder `[node]` 404. Een workspace-id dat de
  forge van de node niet kent: 404, zonder git-aanroep. Een onveilig id: 400.
- `Stop` op een node zonder `unit_prefix` valt terug op `Kill`; de adapter op `self` met een lege
  `unit_prefix` blijft een fout geven.
- Transcriptspiegel: een tweede tick haalt alleen de nieuwe bytes op; een bestand dat op de node
  korter werd, wordt opnieuw geheel opgehaald; `ReadTranscript` op de spiegel geeft hetzelfde als op
  de bron.
- Usage: een `PAUSE` op de tick staat na een tick ook op de node, en een gewiste `PAUSE` is daar weg.
  Een marker op de node leidt tot een hervatting op die node, en daarna is de marker op de node weg.
  Een node waarvan de markers niet leesbaar zijn, krijgt deze tick geen start.
- `Relocate` op een node vindt het pad in de worktree van de node.
- Geen hostnaam, node-ID of machine-opzet in code of fixtures.

## Documentatie

- `docs/workplace.md`: *Een run op een andere node* herschreven. Twee kanalen, de node-routes, en de
  beperkingen die vervallen.
- `docs/tick.md`: `serve_url` en `serve_token_file` onder *Nodes*.
- `docs/install.md` en `docs/host-assumptions.md`: zie *`serve` op een node*.
- De spec van #187: een verwijzing in *Vervolg* naar deze spec.
- Een fragment in `changelog.d/`.

## Aan de bouwer

- De volgorde van de stappen en de verdeling over commits.
- De vorm van de interfaces op de tick: optionele interfaces op `Workplace` of takken in de adapter,
  zolang de code voor `self` niet verandert.
- De JSON-vorm van de routes (huma, zoals de bestaande routes).
- De velden van `/agent-sessions` van forge: meet ze tegen de forge die nu draait, niet tegen het
  geheugen.
- Hoe `info` per tick wordt bewaard.
