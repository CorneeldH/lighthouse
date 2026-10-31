# kenn-forge-werkplek: adapter, drager, stop, bezet en opruimen — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** De werkplek van de dispatcher: één smalle interface met kenn-forge als adapter, de drager waarmee een run start, een Stop die echt stopt, en de regels voor bezet (#31) en opruimen (#85 deel 1), met *Werkplekken om op te ruimen* in API en UI.

**Architecture:** Nieuw pakket `internal/workplace` met de interface (`Prepare`, `Spawn`, `Sessions`, `Stop`, `Remove`), de typen, de drager, de pure regels voor bezet en opruimen, de git-checks en de levenscyclus die runrijen en werkplek verbindt (`Launch`, `CheckOpen`, `Cleanup`). Subpakket `internal/workplace/kennforge` is de adapter: een HTTP-client zonder proxy en zonder redirects, en Stop via `systemctl --user` en `podman exec`. `internal/runs` krijgt de setters voor de werkplekkolommen. De achtergebleven workspaces staan in `tick_state` (nieuwe soort `cleanup`, migratie `0003`), en `serve` toont ze via `GET /api/cleanup` en een UI-lijst. Decide en apply roepen de levenscyclus pas aan in #114/#115; dit issue levert de bouwstenen met tests.

**Tech Stack:** Go, `net/http` + `net/http/httptest`, `os/exec`, `modernc.org/sqlite`, Huma v2; web: React + TanStack Query + orval + vitest.

**Spec:** [`docs/developer/planning/specs/2026-10-27-m2-dispatcher-design.md`](../specs/2026-10-27-m2-dispatcher-design.md), §6 *Launch en de werkplek-interface*, §7 *Afloop* (rijen "Onzekere spawn" en "Ouder dan `max_runtime`"), §8 *Werkplek bezet en opruimen*, *Wat aan de bouwer is* · Issue #113. Naslag voor gedrag (niet als blauwdruk): `personal_ai_agent/src/controller/forge_api.py`, `forge.py`, `forge_ops.py`.

## Global Constraints

- Go volgt `/lighthouse-go-writing`; tests volgen `/lighthouse-testing` (classificatiezin vóór de eerste edit, mutatiecheck vóór "klaar").
- Code-taal Engels onder `internal/**`, `cmd/**`, `web/**`-code: namen, commentaar, foutmeldingen, redenen. Plan, docs, commits en PR in het Nederlands.
- Forge-HTTP: bearer-token uit `[dispatch.forge] token_file`, **per verzoek gelezen** (rotatie). `Transport.Proxy = nil` (een `http_proxy` in de omgeving telt niet) en `CheckRedirect` volgt nooit (`http.ErrUseLastResponse`; een 3xx is een weigering). Time-out per verzoek via de context: 15 s, spawn 30 s; `http.Client.Timeout` blijft 0, anders kapt hij de spawn op 15 s af. Ids in paden met `url.PathEscape`.
- Endpoints (spec §6): `GET /api/v1/workspaces`, `POST /api/v1/repo/<kind>/<repo>/workspaces` (body `{"branch": …}`), `GET /api/v1/workspaces/{id}`, `GET …/{id}/agent-sessions`, `POST …/{id}/runtime/agent-handoffs` (body `{"target_key": …, "message": …}`, antwoord `session.key`), `DELETE …/{id}`. `<kind>`/`<repo>` uit `[forge] kind` en `repo` van de repo, **onge-escaped** in het pad (zoals paia: `CorneeldH/lighthouse` houdt zijn `/`).
- De drager bevat **nooit** een token: geen veld, geen omgevingsvariabele met een token.
- `--strict-mcp-config` staat in elke run; `--mcp-config <profiel.mcp_config>` alleen als het profiel er een heeft.
- Elke subprocess-aanroep loopt via een injecteerbare `workplace.Runner`; geen test raakt systemd, podman of een git-remote op het net, behalve de expliciet gemarkeerde echte-systemd-test (skip zonder user-systemd).
- Onmeetbaar is nooit "weg" of "schoon": een onbekende exitcode of time-out van `pgrep`, `systemctl is-active` of `git` is een fout, geen negatief signaal.
- Een lopende run wordt nooit afgebroken voor het opruimen; een vreemde sessie stopt lighthouse nooit.
- Geen nieuwe tabel (spec §1). Migratie `0003` bouwt alleen `tick_state` opnieuw met de extra soort.
- Commits: Conventional Commits, Nederlands, één logische wijziging per commit. Elke PR een fragment in `changelog.d/`.
- Verificatie zoals CI: `go test -race -shuffle=on ./...`, `golangci-lint run`, en voor web `cd web && npm run lint && npm run typecheck && npm test`.

**Afwijking van de AC-tekst.** De AC citeren de reden "start onzeker: geen sessie gevonden". Onder `internal/**` is tekst Engels (zoals #111 met "previous tick still running"). De reden wordt `start uncertain: no session found`; de time-out `timeout (max_runtime)`. De PR noemt dat bij het bewijs; de vertaling naar de projecttaal op het issue hoort bij het posten in #114.

**Besluit: 5xx op de spawn.** Spec §6 en AC 1 zeggen: een 5xx is "niet aangenomen" en sluit als `start_failed`. `forge_ops.py` wijst erop dat een 5xx ná het starten van de agent kan komen. Daarom: na een 5xx leest `Spawn` de sessies één keer opnieuw. Staat er een **nieuwe** sessie (een key die vóór de POST niet bestond) met de `session_id` van de run of nog zonder `session_id`, dan is de spawn aangenomen (paia, `forge_ops.py`, neemt elke nieuwe key aan); anders `ErrNotAccepted` en wist de adapter de drager, zodat een laat startend target hem niet meer verbruikt. Verschijnt de agent van poging 1 pas ná de herlezing, dan is zijn rij al `start_failed`. `runs.SessionIDs` laat `start_failed`-rijen weg, dus `Busy` ziet die verdwaalde sessie als **vreemd**: bezet zolang ze `working` is of binnen `max_wait` actief, en `ForeignLive` houdt de workspace staan. De aanroeper (decide, #114) toetst `Busy` vóór elke start, ook vóór een tweede poging; `Launch` toetst zelf niet. Een time-out of verbroken verbinding is altijd `ErrUncertain`: nooit een tweede spawn.

## Review Focus

1. **Een forge die na een 5xx toch een agent start, maar de sessie pas na de herlezing toont** (koude `devcontainer up`). Verwacht: geen tweede agent naast de eerste. → Taak 4 wist de drager (`TestSpawn5xxRemovesCarrier`); Taak 1 laat de `session_id` van een `start_failed`-rij uit `own` weg (`TestSessionIDsExcludesStartFailed`), en Taak 6 laat een levende vreemde sessie elke start blokkeren (`TestBusyForeignWorking`).
2. **Een `updated_at` die ontbreekt of niet te lezen is** op een vreemde `idle`-sessie. Verwacht: telt als bezet (niet meetbaar is nooit stil), en de workspace wordt niet verwijderd. → Taak 6, `TestBusyForeignUnparsedUpdatedAt`.
3. **Een workspace waarvan de branch nooit gepusht is, of geen upstream heeft.** Verwacht: telt als niet-gepushte commits, de workspace blijft staan. → Taak 6, `TestUnpushedWithoutRemoteBranch`.
4. **Een token-bestand dat ontbreekt of leeg is.** Verwacht: `ErrNotAccepted` zonder verzoek naar forge (er is niets verstuurd), en nooit een `Authorization: Bearer ` zonder token. → Taak 3, `TestClientEmptyTokenSendsNothing`.
5. **Een forge-redirect naar een ander adres.** Verwacht: niet gevolgd, de bearer-header verlaat loopback niet. → Taak 3, `TestClientDoesNotFollowRedirect` (de tweede server ziet nul verzoeken).

---

## Bestanden

| Bestand | Verantwoordelijkheid |
|---|---|
| `internal/runs/runs.go` (+ `_test`) | `SetWorkplace`, `SetSession`, `Open`, `SessionIDs` |
| `internal/workplace/workplace.go` | Interface, typen, foutsoorten, `Stand`, `Runner` |
| `internal/workplace/carrier.go` (+ `_test`) | De drager: argumenten, validatie, atomair schrijven, wissen |
| `internal/workplace/rules.go` (+ `_test`) | `Busy` en `CleanupBlocker` (puur) |
| `internal/workplace/git.go` (+ `_test`) | `Dirty`, `Unpushed` via de `Runner` |
| `internal/workplace/lifecycle.go` (+ `_test`) | `Launch`, `CheckOpen`, `Cleanup` |
| `internal/workplace/leftovers.go` (+ `_test`) | Achtergebleven workspaces in `tick_state` |
| `internal/workplace/kennforge/client.go` (+ `_test`) | HTTP, foutsoorten |
| `internal/workplace/kennforge/adapter.go` (+ `_test`) | `Prepare`, `Spawn`, `Sessions`, `Remove` |
| `internal/workplace/kennforge/stop.go` (+ `_test`, `stop_systemd_test.go`) | `Stop`, de unitnaam |
| `internal/store/migrations/0003_cleanup.sql` | `tick_state` met soort `cleanup` |
| `internal/server/server.go`, `forge.go` (+ tests), `internal/cli/serve.go` | `GET /api/cleanup` |
| `web/openapi.json`, `web/src/api/*`, `web/src/lib/{api,queries}.ts`, `web/src/components/CleanupList.tsx`, `web/src/App.tsx`, `web/src/components/lists.test.tsx` | De UI-lijst |
| `docs/workplace.md`, `docs/tick.md`, `changelog.d/issue-113-forge-werkplek.md` | Contract van de drager en de regels |

---

### Taak 1: Werkplekkolommen in `internal/runs`

**Files:**
- Modify: `internal/runs/runs.go`
- Test: `internal/runs/runs_test.go`

**Interfaces:**
- Produces:
  - `func SetWorkplace(ctx context.Context, db *sql.DB, id int64, workspaceID, branch string) error` — alleen op een open rij, anders `ErrNotOpen`.
  - `func SetSession(ctx context.Context, db *sql.DB, id int64, key string) error` — zet `workplace_session`; alleen op een open rij, anders `ErrNotOpen`.
  - `type OpenRun struct { ID int64; Issue int; SessionID, Profile, Branch, WorkspaceID, WorkplaceSession string; StartedAt time.Time }`
  - `func Open(ctx context.Context, db *sql.DB, repo string) ([]OpenRun, error)` — open rijen van `repo`, op `id` gesorteerd.
  - `func SessionIDs(ctx context.Context, db *sql.DB, repo string, issue int) (map[string]bool, error)` — de `session_id`'s van het issue, open en dicht, **behalve** rijen met `outcome = start_failed`. Reden (commentaar + test): een agent die na een 5xx toch start, hoort bij geen lopende run en moet voor `Busy` en `ForeignLive` als vreemd tellen (Besluit bovenaan).

- [ ] **Step 1: Failing tests** — `TestSetWorkplaceAndSession` (na `Take`: `SetWorkplace(…, "ws-1", "issue-7-x")` en `SetSession(…, "key-1")`; `Open` geeft één rij met die waarden en `StartedAt == now` op de seconde), `TestSetOnClosedRun` (na `Release`: beide setters `errors.Is(err, ErrNotOpen)`), `TestOpenOnlyRepo` (rij van een andere repo en een dichte rij ontbreken), `TestSessionIDsIncludesClosed` (dichte en open rij van issue 7: beide ids; issue 8 niet), `TestSessionIDsExcludesStartFailed` (rij van issue 7 dicht als `StartFailed`: zijn id ontbreekt; een rij dicht als `Returned` staat er wel in).
- [ ] **Step 2:** `go test ./internal/runs -run 'SetWorkplace|SetOnClosed|OpenOnly|SessionIDs'` → FAIL (undefined).
- [ ] **Step 3:** Implementeer in `runs.go`; `StartedAt` parsen met `time.RFC3339` (zoals `stamp`).
- [ ] **Step 4:** `go test -race ./internal/runs` → PASS.
- [ ] **Step 5:** Commit `feat(runs): werkplekkolommen zetten en open runs lezen`.

### Taak 2: Interface, typen en de drager

**Files:**
- Create: `internal/workplace/workplace.go`, `internal/workplace/carrier.go`
- Test: `internal/workplace/carrier_test.go`, `internal/workplace/workplace_test.go`

**Interfaces:**
- Produces (`package workplace`):
  ```go
  type Workspace struct{ ID, Path, Branch string }
  type Session struct {
      Key, WorkspaceID, State, SessionID string
      UpdatedAt time.Time // zero when forge sent none or it did not parse
  }
  type Stand int // Working, Waiting, Done
  func (s Session) Stand() Stand // "working" → Working, "done" → Done, anything else (idle, null) → Waiting
  type LaunchSpec struct {
      Repo string; Issue int; SessionID, Mode, Profile, Prompt string
      Native bool               // lane: native → --model sonnet
      InstructionsFile string   // absolute; --append-system-prompt-file
      MCPConfig string          // absolute or ""; from the profile
  }
  type Workplace interface {
      Prepare(ctx context.Context, branch string) (Workspace, error)
      Spawn(ctx context.Context, ws Workspace, l LaunchSpec) (Session, error)
      Sessions(ctx context.Context, workspaceID string) ([]Session, error)
      Stop(ctx context.Context, workspaceID string, sessionIDs []string) error
      Remove(ctx context.Context, workspaceID string) error
  }
  var ErrNotAccepted, ErrUncertain, ErrDirty error // sentinels; adapters wrap them with %w
  type Runner func(ctx context.Context, timeout time.Duration, name string, args ...string) (stdout string, code int, err error)
  func ExecRunner() Runner // os/exec; err only when the command could not run or timed out, code is the exit code otherwise
  const CarrierVersion = 1
  type Carrier struct {
      Version int `json:"version"`; Repo string `json:"repo"`; Issue int `json:"issue"`
      SessionID string `json:"session_id"`; Mode string `json:"mode"`; Profile string `json:"profile"`
      Args []string `json:"args"`; Env map[string]string `json:"env"`; CreatedAt string `json:"created_at"`
  }
  func NewCarrier(l LaunchSpec, stateDir string, now time.Time) (Carrier, error)
  func CarrierPath(stateDir, workspaceID string) string // <stateDir>/carriers/<workspaceID>.json
  func WriteCarrier(stateDir, workspaceID string, c Carrier) (string, error)
  func RemoveCarrier(stateDir, workspaceID string) error // missing file is no error
  ```
- `Args` in deze volgorde: `--permission-mode auto`, `--strict-mcp-config`, [`--mcp-config <MCPConfig>`], `--append-system-prompt-file <InstructionsFile>`, `--session-id <id>` bij `start` of `--resume <id>` bij `resume`, [`--model sonnet`], dan de prompt als laatste argument.
- `Env`: precies `LIGHTHOUSE_CONTROLLER_RUN=<SessionID>` en `LIGHTHOUSE_STATE_DIR=<stateDir>`.

- [ ] **Step 1: Failing tests** — `TestStand` (tabel: `working`, `done`, `idle`, `""` → Working, Done, Waiting, Waiting); `TestNewCarrierArgsStart` (exacte `Args`-slice voor start met `MCPConfig` en `Native`), `TestNewCarrierArgsResumeNoMCP` (bevat `--resume <id>` en `--strict-mcp-config`, geen `--mcp-config`, geen `--model`); `TestNewCarrierRejects` (tabel: sessie geen UUID, mode `x`, `InstructionsFile` relatief, `MCPConfig` relatief, NUL in prompt → fout); `TestWriteCarrierAtomic0600` (bestand mode `0600`, JSON terug te lezen met `version == 1`, geen `.tmp` achter); `TestWriteCarrierRejectsWorkspaceID` (`../x`, `.x`, `a/b` → fout, niets geschreven); `TestCarrierHasNoToken` (zet `GH_TOKEN=ghp_secret`, `GITHUB_TOKEN=gho_secret` in de omgeving; de geschreven bytes bevatten geen van beide waarden en geen sleutel die `TOKEN` bevat).
- [ ] **Step 2:** `go test ./internal/workplace` → FAIL.
- [ ] **Step 3:** Implementeer. Workspace-id-regex `^[A-Za-z0-9][A-Za-z0-9._-]*$` (wordt een bestandsnaam); UUID-regex als `runs.NewSessionID` hem maakt. Schrijven: `os.CreateTemp` in de map (`0700`), `Chmod 0600`, `Rename`.
- [ ] **Step 4:** `go test -race ./internal/workplace` → PASS.
- [ ] **Step 5:** Commit `feat(workplace): werkplek-interface en de drager met versieveld`.

### Taak 3: kenn-forge HTTP-client

**Files:**
- Create: `internal/workplace/kennforge/client.go`
- Test: `internal/workplace/kennforge/client_test.go`

**Interfaces:**
- Consumes: `workplace.ErrNotAccepted`, `workplace.ErrUncertain`.
- Produces:
  ```go
  type Client struct {
      URL string; TokenFile string
      HTTP *http.Client // nil: New's client
  }
  func New(url, tokenFile string) *Client // Transport.Proxy nil, CheckRedirect → http.ErrUseLastResponse, Client.Timeout 0 (deadline per request through ctx)
  type StatusError struct{ Status int; Detail string } // non-2xx below 500, 3xx included
  func (c *Client) do(ctx context.Context, method, path string, body any, timeout time.Duration, out any) error // timeout 0: 15s; via context.WithTimeout
  ```
- Foutsoorten van `do`:
  - token onleesbaar of leeg → `ErrNotAccepted` (er is niets verstuurd);
  - dial-fout (`*net.OpError` met `Op == "dial"`) → `ErrNotAccepted`;
  - 5xx → `fmt.Errorf("…: %w", &statusErr{…})` die zowel `errors.Is(err, workplace.ErrNotAccepted)` geeft als `errors.As(err, *StatusError)` met de status (voor de herlezing in Taak 4);
  - 3xx/4xx → `*StatusError`;
  - time-out, reset, EOF na verzenden, body geen JSON → `ErrUncertain`;
  - leeg antwoord (204) → `out` onaangeroerd, `nil`.

- [ ] **Step 1: Failing tests** (alle met `httptest.NewServer`) — `TestClientSendsBearer` (header `Authorization: Bearer tok1`), `TestClientRereadsToken` (bestand tussen twee verzoeken herschreven naar `tok2`; tweede verzoek draagt `tok2`), `TestClientEmptyTokenSendsNothing` (leeg bestand: `ErrNotAccepted`, server-teller 0), `TestClient5xx` (503: `errors.Is(ErrNotAccepted)` en `StatusError.Status == 503`), `TestClient4xx` (404: `*StatusError` 404, niet `ErrNotAccepted`, niet `ErrUncertain`), `TestClientConnRefused` (server gesloten vóór het verzoek: `ErrNotAccepted`), `TestClientTimeoutUncertain` (handler slaapt langer dan `timeout = 50ms`: `ErrUncertain`), `TestClientNonJSONUncertain`, `TestClientDoesNotFollowRedirect` (302 naar tweede server: `*StatusError` 302, tweede server-teller 0), `TestClientNoProxyNoClientTimeout` (op de client van `New`: `Transport.(*http.Transport).Proxy == nil` en `Timeout == 0`; een omgevingstest met `HTTP_PROXY` bewijst niets, want Go proxiet loopback nooit), `TestClientLongTimeoutHonoured` (handler slaapt 100 ms, `timeout = 1s`: geslaagd).
- [ ] **Step 2:** `go test ./internal/workplace/kennforge` → FAIL.
- [ ] **Step 3:** Implementeer `client.go`.
- [ ] **Step 4:** `go test -race ./internal/workplace/kennforge` → PASS.
- [ ] **Step 5:** Commit `feat(kennforge): HTTP-client zonder proxy en redirects, met drie foutsoorten`.

### Taak 4: kenn-forge-adapter: Prepare, Spawn, Sessions, Remove

**Files:**
- Create: `internal/workplace/kennforge/adapter.go`
- Test: `internal/workplace/kennforge/adapter_test.go`

**Interfaces:**
- Consumes: Taak 2 (`Workspace`, `Session`, `LaunchSpec`, `NewCarrier`, `WriteCarrier`, `RemoveCarrier`, `Runner`), Taak 3 (`Client`).
- Produces:
  ```go
  type Adapter struct {
      Client *Client
      Kind, Repo string                 // [forge] kind and repo of the repository
      MainCheckout string               // the repo's main checkout, for the devcontainer label
      StateDir string                   // where carriers go
      Targets map[string]string         // profile → forge target_key
      Run workplace.Runner
      Sleep func(time.Duration)         // nil: time.Sleep
      ReadyWait time.Duration           // 0: 60s
  }
  ```
  `Adapter` krijgt `Stop` pas in Taak 5; de compile-toets `var _ workplace.Workplace = (*Adapter)(nil)` komt daarom in Taak 5, niet hier.
- **Prepare(branch)**: `GET /workspaces`, pak de workspace met `git_head_ref` gelijk aan `branch` of `refs/heads/<branch>`. Geen: `POST /repo/<kind>/<repo>/workspaces`. In beide gevallen daarna `GET /workspaces/{id}` tot `status == "ready"` (poll elke seconde, hoogstens `ReadyWait`; spec §6: "wacht tot hij klaar is", ook een gevonden workspace kan nog `creating` zijn), en daarna `followRemote`. Staat `refs/heads/<branch>` niet op origin (`git -C <path> ls-remote origin refs/heads/<branch>` leeg), dan niets. Anders `git fetch origin <branch>`, en:
  - **nieuwe** workspace: `git checkout -B <branch> --track origin/<branch>`, en toets `HEAD == origin/<branch>`. Reden (commentaar + test): forge zet een nieuwe workspace op een bestaande remote branch op `origin/HEAD` (gemeten in paia, `controller-forge-verification.md` rij (i)).
  - **gevonden** workspace (bijvoorbeeld na een crash tussen aanmaken en `followRemote`): alleen als `HEAD` een echte voorganger van `origin/<branch>` is (`git merge-base --is-ancestor HEAD origin/<branch>` rc 0 en `HEAD != origin/<branch>`) → `git merge --ff-only origin/<branch>`. Rc 1 (eigen commits of al gelijk) → ongemoeid; ander rc → fout. Nooit `checkout -B` op een gevonden workspace: die gooit eigen commits van een vorige run weg.
- **Spawn(ws, l)**: elke fout vóór het verzoek (`Targets[l.Profile]` bestaat niet, `NewCarrier` of `WriteCarrier` faalt) → `fmt.Errorf("…: %w", workplace.ErrNotAccepted)` zonder verzoek: er kan geen agent draaien, dus `Launch` sluit de rij meteen als `start_failed` met de echte oorzaak. Volgorde: drager schrijven → sessies vóór lezen (fout: drager wissen, `ErrNotAccepted`) → `POST …/runtime/agent-handoffs` met `{"target_key", "message": l.Prompt}` en time-out 30 s. Uitkomst:
  - 2xx met `session.key` → `Session{Key, WorkspaceID: ws.ID, SessionID: l.SessionID}`;
  - 5xx → één herlezing (Besluit bovenaan): nieuwe key met `SessionID == l.SessionID` of `SessionID == ""` → aangenomen; anders drager wissen, `ErrNotAccepted`;
  - dial-fout, token-fout of 3xx/4xx → drager wissen, `ErrNotAccepted`;
  - `ErrUncertain` (time-out, reset, geen `session.key`) → herlezing: nieuwe sessie (voorkeur `SessionID == l.SessionID`) → aangenomen; anders `ErrUncertain`, drager blijft.
- **Sessions(id)**: `GET …/{id}/agent-sessions`, veld `sessions[]`: `runtime_session_key` → `Key` (zonder key overslaan), `state` (null → `""`), `session_id`, `updated_at` (RFC 3339; onleesbaar → nulwaarde). Antwoord zonder lijst `sessions` → `ErrUncertain`.
- **Remove(id)**: `DELETE …/{id}`; 409 → `fmt.Errorf("…: %w", workplace.ErrDirty)`; 404 → `nil` (al weg); andere fout doorgeven.

- [ ] **Step 1: Failing tests** (httptest-server die per route telt en antwoorden scripten) — `TestPrepareFindsExisting` (geen POST), `TestPrepareCreatesAndWaitsReady` (status `creating`, `creating`, `ready`; `Sleep` fake telt 2), `TestPrepareReadyTimeout` (nooit ready: fout met "not ready"), `TestPrepareFollowsRemoteBranch` (echte git: bare origin met `issue-1-x` op commit B, worktree op origin/HEAD = A via `ExecRunner`; na Prepare `HEAD == B`), `TestPrepareFoundWaitsReady` (gevonden workspace `creating`, `ready`: geen POST, `Sleep` 1×), `TestPrepareFoundBehindFastForwards` (echte git: gevonden workspace op A, origin/`issue-1-x` op B met A als voorganger: na Prepare `HEAD == B`), `TestPrepareFoundOwnCommitUntouched` (echte git: gevonden workspace met een eigen commit C op A, origin op B: na Prepare `HEAD == C`), `TestSpawnAccepted` (één POST, body `target_key == "paia-software"`, `message == prompt`; drager bestaat), `TestSpawn5xxNoSessionStartFailed` (503, herlezing leeg: `ErrNotAccepted`, POST-teller == 1, drager weg), `TestSpawn5xxRemovesCarrier` (zelfde, expliciet `os.Stat` → `ErrNotExist`), `TestSpawn5xxSessionAppeared` (503, herlezing toont nieuwe key met `session_id` van de run: aangenomen, POST-teller 1), `TestSpawn5xxNewKeyWithoutSessionID` (503, herlezing toont nieuwe key zonder `session_id`: aangenomen, drager blijft), `TestSpawn5xxForeignNewKey` (503, nieuwe key met een andere `session_id`: `ErrNotAccepted`), `TestSpawnTimeoutNeverRespawns` (handler blokkeert langer dan de spawn-time-out, herlezing leeg: `ErrUncertain`, POST-teller == 1, drager bestaat nog), `TestSpawnNoKeyUncertain`, `TestSpawnUnknownProfile` (nul verzoeken, `errors.Is(err, workplace.ErrNotAccepted)`), `TestSpawnInvalidSpecNotAccepted` (`InstructionsFile` relatief: nul verzoeken, `errors.Is(err, workplace.ErrNotAccepted)`), `TestSessionsParses` (idle met `updated_at`, null-state, entry zonder key overgeslagen), `TestRemove409Dirty` (`errors.Is(err, workplace.ErrDirty)`), `TestRemove404Gone`.
  Gebruik in de time-outtest een kortere spawn-time-out via een ongeëxporteerd veld `spawnTimeout` (0: 30 s).
- [ ] **Step 2:** `go test ./internal/workplace/kennforge -run 'Prepare|Spawn|Sessions|Remove'` → FAIL.
- [ ] **Step 3:** Implementeer `adapter.go`.
- [ ] **Step 4:** `go test -race ./internal/workplace/kennforge` → PASS.
- [ ] **Step 5:** Commit `feat(kennforge): Prepare, Spawn met drie uitkomsten, Sessions en Remove`.

### Taak 5: Stop via systemd en podman

**Files:**
- Create: `internal/workplace/kennforge/stop.go`
- Test: `internal/workplace/kennforge/stop_test.go`, `internal/workplace/kennforge/stop_systemd_test.go`

**Interfaces:**
- Produces:
  - `func UnitName(profile, worktreePath string) string` — `paia-forge-<profile>-<basename>`, waarbij elke byte buiten `[A-Za-z0-9_-]` één `-` wordt (gelijk aan `unitnaam()` in paia's `forge_agent.sh`; een meerbytig teken geeft meerdere streepjes).
  - `func (a *Adapter) Stop(ctx context.Context, workspaceID string, sessionIDs []string) error`.
  - `Adapter` krijgt veld `Profile string` (het profiel van de repo, `[controller] profile`).
  - `var _ workplace.Workplace = (*Adapter)(nil)` in `stop.go` (of `adapter.go`): nu heeft `Adapter` alle vijf methoden.
- Gedrag: pad via `GET /workspaces/{id}` (`worktree_path`). `systemctl --user stop <unit>` (exitcode genegeerd: een unit die er niet is faalt), dan `systemctl --user is-active <unit>`: alleen stdout `inactive` of `failed` telt als weg, anders fout. Bij `Profile == "software"` en ≥1 sessie-id: containers via `podman ps --filter label=devcontainer.local_folder=<MainCheckout> --format {{.ID}}` (rc ≠ 0 → fout); per container en sessie `podman exec <c> pgrep -f <sessie>`: rc 0 leeft, rc 1 weg, anders fout; leeft → `pkill -f <sessie>` en tot 5 keer opnieuw `pgrep` met 1 s `Sleep`; leeft dan nog → fout. Reden in commentaar: bij Software draait claude in de gedeelde devcontainer, buiten de cgroup van de unit, en forge heeft geen stop-endpoint (spec §6).

- [ ] **Step 1: Failing tests** — `TestUnitName` (tabel: `/w/issue-7-x` → `paia-forge-software-issue-7-x`; `/w/a.b` → `…-a-b`; `/w/é` → twee streepjes); met een fake `Runner` die aanroepen opneemt en antwoorden scriptet: `TestStopUnitInactive` (infra: precies `stop` en `is-active`, geen podman), `TestStopUnitStillActive` (`active` → fout met unitnaam), `TestStopIsActiveUnmeasurable` (rc 4, lege stdout → fout), `TestStopSoftwarePkill` (pgrep rc 0, pkill, pgrep rc 1 → nil; volgorde van aanroepen exact), `TestStopSoftwareSurvivesPkill` (pgrep blijft 0 → fout na 5 herhalingen, `Sleep` 5×), `TestStopPgrepUnmeasurable` (rc 2 → fout, geen pkill).
- [ ] **Step 2: Echte-systemd-test** in `stop_systemd_test.go`: `TestStopKillsRealUnit` — skip alleen als `systemctl --user show-environment` faalt (geen user-bus) of `systemd-run` ontbreekt. Niet op `is-system-running`: dat geeft rc 1 bij `degraded`, en deze host is `degraded` (gemeten), dus de test zou altijd overslaan. Start `systemd-run --user --unit=<UnitName("lhtest", dir)> sleep 600` met `dir` een map met een unieke naam (`filepath.Join(t.TempDir(), fmt.Sprintf("ws-%d-%d", os.Getpid(), time.Now().UnixNano()))`, aangemaakt; de basename van `t.TempDir()` zelf is `001` en botst tussen runs), en een `t.Cleanup` die de unit stopt; `Stop` met een httptest-forge die `worktree_path = dir` geeft en `ExecRunner()`; lees vóór Stop de `MainPID` (`systemctl --user show -p MainPID --value <unit>`); daarna is `is-active` niet `active` **en** bestaat `/proc/<MainPID>` niet meer. Dit is het bewijs "geen proces meer" voor het unitdeel van AC 3.
- [ ] **Step 3:** `go test ./internal/workplace/kennforge -run 'Stop|UnitName' -v` → FAIL.
- [ ] **Step 4:** Implementeer `stop.go`.
- [ ] **Step 5:** `go test -race ./internal/workplace/kennforge -v` → PASS, en `TestStopKillsRealUnit` PASS (niet SKIP) op deze host.
- [ ] **Step 6:** Commit `feat(kennforge): Stop via systemctl en pkill in de devcontainer`.

### Taak 6: Bezet, opruimregels en git-checks

**Files:**
- Create: `internal/workplace/rules.go`, `internal/workplace/git.go`
- Test: `internal/workplace/rules_test.go`, `internal/workplace/git_test.go`

**Interfaces:**
- Produces:
  ```go
  // own: the session ids of runs of this issue (runs.SessionIDs, start_failed excluded); open: of open runs only.
  // activity: last transcript event per session key, measured by the caller (#115); nil or a missing key falls back to UpdatedAt.
  func Busy(sessions []Session, own, open map[string]bool, activity map[string]time.Time, now time.Time, maxWait time.Duration) (Session, bool)
  func BusyReason(s Session) string // "workplace busy: session <key>, <state> since 15:04" (local time of UpdatedAt; "unknown" without)
  func ForeignLive(sessions []Session, own map[string]bool) (Session, bool)
  func Dirty(ctx context.Context, run Runner, path string) (bool, error)
  // mergedHead: head SHA of the issue's merged PR, or "" when unknown.
  func Unpushed(ctx context.Context, run Runner, path, mergedHead string) (bool, error)
  ```
- `Busy`: een sessie van een open run (`open[s.SessionID]`) is bezet tenzij `Done`. Een eigen sessie van een dichte run telt niet. Een vreemde sessie (niet in `own`): `Working` → bezet; `Waiting` → bezet als de laatste activiteit nul is of `now - activiteit < maxWait`; `Done` → niet. Laatste activiteit: `activity[s.Key]` als die er is, anders `UpdatedAt`. Reden: spec §8 meet "stil" aan transcript-events (§7 *Activiteit*, ook `subagents/*.jsonl`); die meting hoort bij #115, dat `activity` vult. Zonder meting valt `Busy` terug op de forge-stand (spec §7: "niet meetbaar valt terug op de forge-stand"). Eerste bezette sessie in volgorde van de lijst wordt teruggegeven.
- `ForeignLive`: een vreemde sessie die niet `Done` is, ook een stille (spec §8: de workspace verdwijnt niet onder haar).
- `Dirty`: `git --no-optional-locks -C <path> status --porcelain` (`--no-optional-locks`: anders ververst status de index van een worktree waarin de gebruiker misschien commit); rc ≠ 0 → fout; niet-lege stdout → vuil.
- `Unpushed`: is `mergedHead` gezet en geeft `git -C <path> rev-parse HEAD` precies die SHA, dan `false`. Anders `git -C <path> rev-list --count HEAD --not --remotes=origin`; rc ≠ 0 → fout; > 0 → niet gepusht. Telt zo ook een branch die nooit gepusht is. Reden voor `mergedHead` (commentaar + test): elke PR wordt squash-gemerged en GitHub wist de head-branch (`delete_branch_on_merge`); na een `fetch --prune` staat geen commit van de branch meer op een remote ref, en zonder deze uitzondering bleef elke afgeronde workspace staan.

- [ ] **Step 1: Failing tests** — `TestBusyForeignWorking`, `TestBusyForeignIdleRecent` (idle, 10 min oud, `maxWait` 1 h → bezet), `TestBusyForeignIdleStale` (#31: idle, 2 h oud → niet bezet), `TestBusyForeignUnparsedUpdatedAt` (idle, nul-tijd → bezet), `TestBusyOpenRunWaiting` (eigen open run, idle 2 h oud → bezet), `TestBusyClosedOwnIgnored`, `TestBusyActivityOverridesUpdatedAt` (vreemde idle, `UpdatedAt` 2 h oud, `activity[key]` 5 min oud → bezet), `TestBusyActivityStale` (vreemde idle, `UpdatedAt` 5 min oud, `activity[key]` 2 h oud → niet bezet), `TestBusyReason` (exacte string met `time.Local = time.UTC` in de test); `TestForeignLiveStaleIdle` (stille vreemde idle → wel "live" voor opruimen); git, met `ExecRunner` op een echte tempdir-repo met bare origin: `TestDirtyClean`, `TestDirtyUntracked`, `TestUnpushedAfterPush` (false), `TestUnpushedLocalCommit` (true), `TestUnpushedWithoutRemoteBranch` (nieuwe branch, nooit gepusht, met een eigen commit → true), `TestUnpushedSquashMergedPruned` (branch gepusht, remote branch gewist en `fetch --prune`: met `mergedHead == HEAD` → false, met `mergedHead == ""` → true), `TestGitUnmeasurable` (pad bestaat niet → fout, niet `false`).
- [ ] **Step 2:** `go test ./internal/workplace -run 'Busy|ForeignLive|Dirty|Unpushed|GitUnmeasurable'` → FAIL.
- [ ] **Step 3:** Implementeer.
- [ ] **Step 4:** `go test -race ./internal/workplace` → PASS.
- [ ] **Step 5:** Commit `feat(workplace): bezet-toets (#31) en de git-checks voor opruimen`.

### Taak 7: Achtergebleven workspaces in `tick_state`

**Files:**
- Create: `internal/store/migrations/0003_cleanup.sql`, `internal/workplace/leftovers.go`
- Modify: `internal/store/migrate_test.go` (verwachte `user_version` 3, als die test hem pint), `internal/cli/tick_test.go` (pint `user_version` 2 op regel 144: wordt 3)
- Test: `internal/workplace/leftovers_test.go`

**Interfaces:**
- Produces:
  ```go
  type Leftover struct {
      Repo string `json:"repo"`; Issue int `json:"issue"`
      WorkspaceID string `json:"workspace_id"`; Path string `json:"path"`; Reason string `json:"reason"`
  }
  func RecordLeftover(ctx context.Context, db *sql.DB, l Leftover, now time.Time) error // upsert tick_state(repo, 'cleanup', workspace_id)
  func ClearLeftover(ctx context.Context, db *sql.DB, repo, workspaceID string) error
  func Leftovers(ctx context.Context, db *sql.DB) ([]Leftover, error) // all repos, sorted by repo, issue, workspace id; never nil
  ```
- `0003_cleanup.sql`: `tick_state` opnieuw aanmaken met `CHECK (kind IN ('fingerprint', 'deferred', 'run', 'cleanup'))`, rijen kopiëren, oude tabel weg, nieuwe hernoemen. `value` is JSON `{"issue", "path", "reason"}`.

- [ ] **Step 1: Failing tests** — `TestLeftoversRoundTrip` (twee repo's, record, lees in vaste volgorde), `TestRecordLeftoverUpserts` (tweede record met andere reden: één rij, nieuwe reden), `TestClearLeftover`, `TestLeftoversEmptyNotNil`, en in `internal/store`: `TestMigrate0003KeepsTickState` (database op versie 2 met een `fingerprint`-rij migreren: rij bestaat nog, `cleanup` wordt geaccepteerd).
- [ ] **Step 2:** `go test ./internal/store ./internal/workplace -run 'Leftover|0003'` → FAIL.
- [ ] **Step 3:** Implementeer migratie en `leftovers.go`.
- [ ] **Step 4:** `go test -race ./...` → PASS (de hele boom: de test-gate selecteert geen importeurs, en `internal/cli` pint de versie ook).
- [ ] **Step 5:** Commit `feat(workplace): achtergebleven workspaces in tick_state (#85)`.

### Taak 8: Levenscyclus: Launch, CheckOpen en Cleanup

**Files:**
- Create: `internal/workplace/lifecycle.go`
- Test: `internal/workplace/lifecycle_test.go`

**Interfaces:**
- Consumes: Taak 1 (`runs.*`), Taak 2 (interface, fouten), Taak 6 (`Busy`, `ForeignLive`, `Dirty`, `Unpushed`), Taak 7 (`RecordLeftover`, `ClearLeftover`).
- Produces:
  ```go
  const (
      ReasonUncertain = "start uncertain: no session found"
      ReasonTimeout   = "timeout (max_runtime)"
  )
  type Limits struct{ MaxWait, MaxRuntime time.Duration }
  type Outcome int // Accepted, Uncertain, NotAccepted
  func Launch(ctx context.Context, db *sql.DB, w Workplace, runID int64, branch string, l LaunchSpec, now time.Time) (Outcome, error)
  type Closed struct{ RunID int64; Issue int; Reason string }
  func CheckOpen(ctx context.Context, db *sql.DB, w Workplace, repo string, lim Limits, now time.Time) ([]Closed, error)
  type Candidate struct{ Repo string; Issue int; InController bool; MergedHead string; Workspace Workspace } // InController and MergedHead come from the board and the PR (#112, #114)
  func Cleanup(ctx context.Context, db *sql.DB, w Workplace, run Runner, c Candidate, now time.Time) (removed bool, err error)
  ```
- **Launch** (na `runs.Take`): `Prepare(branch)` faalt → `Release(StartFailed, "prepare: <err>")`, `NotAccepted`. Anders `SetWorkplace`, dan `Spawn`: `ErrNotAccepted` → `Release(StartFailed, <err>)`, `NotAccepted`; `ErrUncertain` → rij blijft open, `workplace_session` leeg, `Uncertain`; aangenomen → `SetSession(key)`, `Accepted`. Andere fout van Spawn → behandel als `ErrUncertain` (er kan een agent draaien).
- **CheckOpen**, per open rij van `repo` met `WorkspaceID != ""`:
  1. `WorkplaceSession == ""` (onzeker): `Sessions`; een sessie met `SessionID == run.SessionID` → `SetSession`. Geen, en `now - StartedAt ≥ MaxWait` → `Release(Returned, ReasonUncertain)` en **door naar de volgende rij** (stap 2 zou een tweede `Release` doen en `ErrNotOpen` geven). `Sessions` faalt → rij ongemoeid (niet meetbaar), fout verzameld.
  2. `now - StartedAt ≥ MaxRuntime` → `Stop(WorkspaceID, [SessionID])`; alleen als Stop slaagt `Release(Returned, ReasonTimeout)`. Stop faalt → rij blijft open, fout verzameld (nooit "gestopt" melden zonder bewijs).
  Geeft de gesloten rijen terug, en `errors.Join` van de verzamelde fouten.
- **Cleanup**: `InController` → niets. Open run voor het issue (`runs.Open`) → niets (een lopende run wordt nooit afgebroken). Anders `Sessions`; `own := runs.SessionIDs`. `ForeignLive` → leftover "foreign session <key>". `Dirty` → "uncommitted changes". `Unpushed` → "unpushed commits". `Unpushed` krijgt `c.MergedHead`. Een fout van een check → leftover "not measurable: <err>" (nooit verwijderen). Daarna `Stop` met de sessie-ids uit `Sessions` die in `own` staan; faalt Stop → leftover "not measurable: stop: <err>", geen `Remove`. Dan `Remove`: `ErrDirty` → leftover "forge refused: dirty worktree (409)"; gelukt → `ClearLeftover`, `removed = true`.

- [ ] **Step 1: Failing tests** — met een fake `Workplace` (scriptbare antwoorden, opgenomen aanroepen) en een echte database via `store.OpenShared` op `t.TempDir()`: `TestLaunchNotAcceptedReleasesStartFailed` (AC 1: Spawn geeft `ErrNotAccepted` → rij dicht met `outcome = start_failed`; Spawn 1× aangeroepen), `TestLaunchUncertainKeepsRowOpen` (rij open, `workplace_session` leeg, Spawn 1×), `TestLaunchAcceptedSetsSession`, `TestLaunchPrepareFails`; `TestCheckOpenUncertainFoundLater` (sessie verschijnt: `SetSession`, rij open), `TestCheckOpenUncertainPastMaxWait` (AC 2: `Returned` met reden `ReasonUncertain`), `TestCheckOpenUncertainWithinMaxWait` (rij open), `TestCheckOpenUncertainPastBothLimits` (onzeker, ouder dan `MaxWait` én `MaxRuntime`: één keer `Returned` met `ReasonUncertain`, geen Stop, geen fout), `TestCheckOpenOverdueStops` (AC 3: Stop met de `session_id`, dan `Returned` met `ReasonTimeout`), `TestCheckOpenStopFailsKeepsOpen`; `TestCleanupRemovesClean` (Stop en Remove aangeroepen, leftover gewist), `TestCleanupInControllerUntouched`, `TestCleanupOpenRunUntouched` (geen Stop, geen Remove), `TestCleanupStaleForeignSessionStays` (AC 4: stille vreemde idle → geen Remove, leftover), `TestCleanupDirtyStays`, `TestCleanupUnpushedStays`, `TestCleanupMergedHeadRemoves` (`MergedHead` gelijk aan HEAD van de fake `Runner`: Remove aangeroepen), `TestCleanupStopFailsStays` (Stop faalt: geen Remove, leftover "not measurable: stop: …"), `TestCleanup409Stays` (AC 5: drie gevallen samen geven elk een leftover onder de juiste workspace-id). De git-checks lopen hier via een fake `Runner`.
- [ ] **Step 2:** `go test ./internal/workplace -run 'Launch|CheckOpen|Cleanup'` → FAIL.
- [ ] **Step 3:** Implementeer `lifecycle.go`.
- [ ] **Step 4:** `go test -race ./internal/workplace/...` → PASS.
- [ ] **Step 5:** Commit `feat(workplace): levenscyclus van launch, bovengrens en opruimen`.

### Taak 9: `GET /api/cleanup` en de UI-lijst

**Files:**
- Modify: `internal/server/server.go`, `internal/server/forge.go`, `internal/cli/serve.go`, en **elke** aanroep van `New` in de tests: `internal/server/server_test.go`, `forge_test.go`, `host_test.go`, `openapi_snapshot_test.go` (`grep -n 'New(' internal/server/*_test.go`); de nieuwe tests in `forge_test.go`
- Modify: `web/openapi.json`, `web/src/api/*` (gegenereerd), `web/src/lib/api.ts`, `web/src/lib/queries.ts`, `web/src/App.tsx`, `web/src/components/lists.test.tsx`
- Create: `web/src/components/CleanupList.tsx`

**Interfaces:**
- Consumes: Taak 7 (`workplace.Leftover`, `workplace.Leftovers`).
- Produces: `server.New(site fs.FS, report func(context.Context) usage.Report, src overview.Source, leftovers func(context.Context) ([]workplace.Leftover, error)) http.Handler`; route `GET /api/cleanup`, operation-id `get-cleanup`, body `{"cleanup": [Leftover…]}`; fout → 500. `serve` geeft `func(ctx) { return workplace.Leftovers(ctx, st.DB) }`.
- UI: `CleanupList` volgt `ReturnedList`: paneel "Workspaces to clean up", lege tekst "No workspaces to clean up", per item `repo#issue`, workspace-id, pad (als code) en reden. Query-interval `FORGE_INTERVAL_MS`.

- [ ] **Step 1: Failing tests** — Go: `TestCleanupEndpoint` (fake `leftovers` met één item: 200, JSON gelijk), `TestCleanupEndpointError` (500); web (`lists.test.tsx`): `CleanupList` "shows repo, issue, workspace id, path and reason" en "shows the empty text".
- [ ] **Step 2:** `go test ./internal/server` en `cd web && npm test` → FAIL.
- [ ] **Step 3:** Implementeer de route en `serve`-bedrading.
- [ ] **Step 4:** `go test ./internal/server -run TestOpenAPISnapshot -update`, dan `cd web && npm run gen`.
- [ ] **Step 5:** Implementeer `fetchCleanup`, `useCleanup`, `CleanupList`, en zet hem in `App.tsx` onder `ReturnedList`.
- [ ] **Step 6:** `go test -race ./internal/server ./internal/cli` en `cd web && npm run lint && npm run typecheck && npm test` → PASS.
- [ ] **Step 7:** Commit `feat(server): werkplekken om op te ruimen in API en UI (#85)`.

### Taak 10: Documentatie, changelog en eindverificatie

**Files:**
- Create: `docs/workplace.md`, `changelog.d/issue-113-forge-werkplek.md`
- Modify: `docs/tick.md` (link naar `workplace.md`)

- [ ] **Step 1:** `docs/workplace.md` (Nederlands): de interface in één alinea; **het dragercontract** voor paia#1661 (pad `<statemap>/carriers/<workspace-id>.json`, mode `0600`, velden met `version: 1`, volgorde van `args`, `env`, "nooit een token", het target verbruikt hem en voert precies `args` uit); de drie spawn-uitkomsten met het 5xx-besluit; Stop (unitnaam, `is-active`, `pgrep`/`pkill`); bezet (#31) en opruimen (#85) met de redenen; dat decide/apply ze in #114/#115 aanroepen, dat de aanroeper `Busy` vóór elke start toetst (ook een tweede poging), en dat #115 `activity` en #112/#114 `InController` en `MergedHead` leveren. Relatieve links.
- [ ] **Step 2:** Changelog-fragment `type: added`, één zin, model `changelog.d/issue-111-runstaat-claim-tick.md`.
- [ ] **Step 3:** Verificatie: `go test -race -shuffle=on ./...`, `golangci-lint run`, `cd web && npm run lint && npm run typecheck && npm test`, en `python3 ci/domains.py gate`. Alles groen; `TestStopKillsRealUnit` PASS, niet SKIP.
- [ ] **Step 4:** Commit `docs(workplace): dragercontract en regels voor bezet en opruimen`.

**Buiten dit plan, wel nodig voor het volle bewijs van AC 3:** de podman-kant van Stop tegen een echte devcontainer vergt paia#1661 (CorneeldH/personal_ai_agent#1661). De PR noemt dat als open bewijs en verwijst er met `Refs` naar, tenzij #1661 dan al gemerged is.
