# Romp van `lighthouse serve` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `lighthouse serve` als altijd-aan-dienst: Huma v2 op `net/http`, SQLite in WAL met
genummerde migraties, een ingebedde placeholder met SPA-terugval, en een systemd-user-unit met
`tailscale serve`-script.

**Architecture:** vier packages met één verantwoordelijkheid elk: `internal/store` (pad, slot,
openen, migraties), `internal/web` (ingebedde `dist/`), `internal/server` (één `http.Handler`
met API en SPA-terugval, zonder netwerk testbaar) en `internal/cli/serve.go` (vlaggen, signalen,
listen en shutdown). Deploy-bestanden staan los in `deploy/`.

**Tech Stack:** Go 1.26.4, Cobra (bestaat), `github.com/danielgtaylor/huma/v2` met adapter
`humago`, `modernc.org/sqlite`, systemd (user), Tailscale. Geen testify: `t.Fatalf`/`t.Errorf`.

**Spec:** [2026-10-25-romp-serve-design.md](../specs/2026-10-25-romp-serve-design.md)

## Global Constraints

- Code, commentaar en namen in het Engels (`code-language`-hook); commitberichten en dit plan in
  het Nederlands, als Conventional Commits.
- `CGO_ENABLED=0 go build ./...` slaagt: geen cgo-dependency.
- Standaardadres `127.0.0.1:8127`; standaard-DB
  `${XDG_STATE_HOME:-$HOME/.local/state}/lighthouse/lighthouse.db`, `XDG_STATE_HOME` alleen als
  absoluut pad; map aanmaken met `0700`.
- Pragma's op elke verbinding: `journal_mode(WAL)`, `busy_timeout(5000)`, `foreign_keys(ON)`,
  `synchronous(NORMAL)`.
- Slot: exclusieve, niet-blokkerende `flock` op `<db>.lock`; melding
  `another lighthouse serve holds <db>.lock`; exit 1.
- Shutdown-deadline 10 seconden; daarna `Close`, altijd exit 0 bij SIGINT/SIGTERM.
- Startregel op stderr: `lighthouse: listening on <host:port>` (de echte poort, ook bij `:0`).
- Geen tabel `runs`. Geen authenticatie. Geen paden, hostnamen of IP's van de auteur in code of
  `deploy/`.
- Elke commit gaat door de test-gate (`[test_gate] command`); na Task 1 is dat
  `uv run pytest -q && go test -race -shuffle=on ./...`.
- Klaar melden per KPI-rij van `lighthouse-go-writing`, met omgeving (linux, tags, `-race`,
  `GOWORK`).

## Review Focus

1. **Een tweede `serve` op dezelfde database** (ook met een ander `--addr`): exit 1 met de
   slotmelding, en de eerste server blijft draaien. → e2e-test in Task 5.
2. **Een databasepad met een spatie** (`~/My State/…` via `--db`): de DSN mag niet breken. →
   test in Task 2.
3. **Een database van een nieuwere binary** (`user_version` hoger dan de hoogste migratie):
   `serve` weigert en laat de database ongemoeid. → test in Task 3.
4. **Een pad met `..` of een map onder `/`** (`/../../etc/passwd`, `/assets/`): de SPA-handler
   serveert nooit iets buiten de ingebedde map en geeft voor een map `index.html`, geen listing. →
   test in Task 4.
5. **Een poort die al bezet is**: `serve` stopt met exit 1 en de fout van `net.Listen`, en laat
   geen `.lock` achter dat de volgende start blokkeert (de `flock` valt weg met het proces). →
   e2e-test in Task 5.

---

### Task 1: `-race` in gate en CI, plus een build zonder cgo

**Files:**
- Modify: `lighthouse.toml` (`[test_gate] command`)
- Modify: `.github/workflows/ci.yml` (beide `go test`-stappen, nieuwe build-stap)
- Modify: `tests/test_dogfood.py:154-164`
- Modify: `tests/test_ci_go_test_pin.py`

**Interfaces:**
- Produces: de gate-opdracht `uv run pytest -q && go test -race -shuffle=on ./...`.

- [ ] **Step 1: Pas de tests aan zodat ze falen**

In `tests/test_dogfood.py` heet `test_test_gate_shuffles` voortaan `test_test_gate_shuffles_with_race`
en eist `"go test -race -shuffle=on ./..." in result.stdout`. In `tests/test_ci_go_test_pin.py`
een nieuwe test:

```python
def test_ci_builds_without_cgo():
    """Fails when CI stops proving the binary builds without cgo."""
    ci = (ROOT / ".github" / "workflows" / "ci.yml").read_text(encoding="utf-8")
    assert re.search(r"^\s*- run: CGO_ENABLED=0 go build \./\.\.\.\s*$", ci, re.MULTILINE)
```

- [ ] **Step 2: Draai ze en zie ze falen**

Run: `uv run pytest -q tests/test_dogfood.py tests/test_ci_go_test_pin.py`
Expected: 2 failed (`test_test_gate_shuffles_with_race`, `test_ci_builds_without_cgo`).

- [ ] **Step 3: Pas gate en CI aan**

`lighthouse.toml`: `command = "uv run pytest -q && go test -race -shuffle=on ./..."`.
`ci.yml`: beide stappen krijgen `-race` (`go test -race -shuffle=on ./...` en
`go test -race -shuffle=on -tags e2e ./e2e/ -v`); voeg na de lint-stap
`- run: CGO_ENABLED=0 go build ./...` toe. Werk het commentaar boven de `go test`-stap niet om:
het klopt nog.

- [ ] **Step 4: Draai de hele gate**

Run: `uv run pytest -q && go test -race -shuffle=on ./...`
Expected: alles groen. Faalt een bestaande Go-test pas onder `-race`, stop en meld het: dat is
een eigen bug, geen onderdeel van deze taak.

- [ ] **Step 5: Commit**

```bash
git add lighthouse.toml .github/workflows/ci.yml tests/test_dogfood.py tests/test_ci_go_test_pin.py
git commit -m "ci: go test met -race en een build zonder cgo (#63)"
```

### Task 2: `internal/store` — pad, slot en openen met pragma's

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/store/lock.go`
- Test: `internal/store/store_test.go`
- Modify: `go.mod`, `go.sum` (`modernc.org/sqlite`)

**Interfaces:**
- Produces:
  - `func DefaultPath() (string, error)`
  - `type Store struct { DB *sql.DB; … }` met `func Open(ctx context.Context, path string) (*Store, error)` en `func (s *Store) Close() error`
  - `var ErrLocked = errors.New("database locked")` en `type LockedError struct{ Path string }`
    met `Error()` = `"another lighthouse serve holds " + Path` en `Is(target error) bool` dat
    `target == ErrLocked` geeft. `Open` geeft `&LockedError{Path: lockPath}`, zodat
    `cli.Execute` precies de spec-melding `lighthouse: another lighthouse serve holds <db>.lock`
    print (spec §1), zonder achtervoegsel.

- [ ] **Step 1: Schrijf de falende tests**

- `TestDefaultPath`, tabel met `t.Setenv`: `XDG_STATE_HOME=/x/state`, `HOME=/h` → `/x/state/lighthouse/lighthouse.db`;
  `XDG_STATE_HOME=rel/state`, `HOME=/h` → `/h/.local/state/lighthouse/lighthouse.db`;
  `XDG_STATE_HOME=""`, `HOME=/h` → `/h/.local/state/lighthouse/lighthouse.db`.
- `TestOpenSetsPragmas`: `Open` op `filepath.Join(t.TempDir(), "My State", "l.db")` (map bestaat
  niet, met spatie). Haal met `s.DB.Conn(ctx)` twee aparte verbindingen tegelijk op; op elk:
  `PRAGMA journal_mode` = `"wal"`, `PRAGMA busy_timeout` = `5000`, `PRAGMA foreign_keys` = `1`,
  `PRAGMA synchronous` = `1` (NORMAL). Daarna: de map heeft mode `0o700`.
- `TestOpenTwiceIsLocked`: tweede `Open` op hetzelfde pad terwijl de eerste open is →
  `errors.Is(err, ErrLocked)` en `err.Error()` = `"another lighthouse serve holds " + path +
  ".lock"` (exact). Na `Close` van de eerste slaagt een derde `Open`.

- [ ] **Step 2: Draai ze en zie ze falen**

Run: `go test -race -run 'TestDefaultPath|TestOpen' ./internal/store/`
Expected: FAIL, build error (package bestaat niet).

- [ ] **Step 3: Implementeer**

`DefaultPath` met `os.Getenv("XDG_STATE_HOME")` + `filepath.IsAbs`, anders `os.UserHomeDir()`.
`Open`: `os.MkdirAll(dir, 0o700)`, dan het slot (`lock.go`: `os.OpenFile(path+".lock",
O_CREATE|O_RDWR, 0o600)` + `syscall.Flock(fd, LOCK_EX|LOCK_NB)`; `EWOULDBLOCK` → `*LockedError`),
dan `sql.Open("sqlite", dsn)` en `PingContext`. Bouw de DSN met `file:` + het pad en de vier
`_pragma=`-parameters; zorg dat een spatie in het pad werkt (de test bewijst het). `Close`
sluit eerst de DB, dan het slotbestand. `go get modernc.org/sqlite@latest`, `go mod tidy`.

- [ ] **Step 4: Draai de tests**

Run: `go test -race -run 'TestDefaultPath|TestOpen' ./internal/store/ && CGO_ENABLED=0 go build ./...`
Expected: PASS en een build zonder fout.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/store
git commit -m "feat(store): SQLite in WAL met busy_timeout en één-server-slot (#63)"
```

### Task 3: `internal/store` — migratierunner en `0001_meta.sql`

**Files:**
- Create: `internal/store/migrate.go`
- Create: `internal/store/migrations/0001_meta.sql`
- Test: `internal/store/migrate_test.go`
- Modify: `internal/store/store.go` (`Open` migreert na het slot)

**Interfaces:**
- Consumes: `Open`, `Store.DB` uit Task 2.
- Produces: `func migrate(ctx context.Context, db *sql.DB, fsys fs.FS) error` (intern) en
  `var ErrSchemaTooNew = errors.New(…)`. `Open` roept `migrate` aan met de ingebedde set.

- [ ] **Step 1: Schrijf de falende tests**

- `TestMigrateTwice`: `Open` op een tempdir-pad, lees `SELECT type, name, coalesce(sql, '') FROM
  sqlite_master ORDER BY name` (de autoindex van `meta` heeft `sql` NULL) en `PRAGMA user_version`; `Close`; `Open` opnieuw (migreert weer), lees
  opnieuw. Gelijk; `user_version` = `1`; `meta` bestaat, `runs` niet; `SELECT value FROM meta
  WHERE key='created_at'` geeft precies één rij, ook na de tweede open.
- `TestMigrationNumbering`, tabel met `fstest.MapFS` op een kale `sql.Open` (geen `Open`):
  `{0001_a.sql, 0002_b.sql}` → ok, `user_version` 2;
  `{0001_a.sql, 0003_c.sql}` → fout met `0002` in de tekst;
  `{0001_a.sql, 0001_b.sql}` → fout met `0001` in de tekst;
  `{1_a.sql}` → fout (geen vier cijfers).
- `TestMigrationFailsAtomically`: `{0001_a.sql: "CREATE TABLE a(x); CREATE TABLE a(x);"}` →
  fout; daarna `user_version` 0 en geen tabel `a`.
- `TestSchemaTooNew`: zet op een kale DB `PRAGMA user_version = 99`, roep `migrate` met de
  ingebedde set → `errors.Is(err, ErrSchemaTooNew)`; `user_version` blijft 99.
- `TestEmbeddedMigrationsNumbered`: de ingebedde set laadt zonder fout (pint de nummering).

- [ ] **Step 2: Draai ze en zie ze falen**

Run: `go test -race -run 'TestMigrat|TestSchemaTooNew|TestEmbedded' ./internal/store/`
Expected: FAIL, `migrate` undefined.

- [ ] **Step 3: Implementeer**

`//go:embed migrations/*.sql`. Laden: bestandsnamen `^(\d{4})_[a-z0-9_]+\.sql$`, gesorteerd,
nummers 1..n zonder gat of dubbel. Draaien: lees `user_version`; hoger dan n → `ErrSchemaTooNew`;
per hoger nummer één `BeginTx`, `ExecContext` van het bestand, `PRAGMA user_version = <n>` in
dezelfde transactie, `Commit`. `0001_meta.sql`:

```sql
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO meta (key, value) VALUES ('created_at', strftime('%Y-%m-%dT%H:%M:%SZ', 'now'));
```

- [ ] **Step 4: Draai de tests**

Run: `go test -race ./internal/store/`
Expected: PASS (ook de tests uit Task 2).

- [ ] **Step 5: Commit**

```bash
git add internal/store
git commit -m "feat(store): genummerde migraties op user_version (#63)"
```

### Task 4: `internal/web` en `internal/server` — API, OpenAPI en SPA-terugval

**Files:**
- Create: `internal/web/web.go`, `internal/web/dist/index.html`
- Create: `internal/server/server.go`, `internal/server/spa.go`
- Test: `internal/server/server_test.go`
- Modify: `go.mod`, `go.sum` (`github.com/danielgtaylor/huma/v2`)

**Interfaces:**
- Produces:
  - `func web.FS() fs.FS` — `fs.Sub` van de ingebedde `dist`.
  - `func server.New(site fs.FS) http.Handler`.

- [ ] **Step 1: Schrijf de falende tests**

Alle tests via `httptest.NewRecorder` tegen `server.New(fstest.MapFS{"index.html": {Data:
[]byte("<p>placeholder</p>")}, "assets/app.js": {Data: []byte("js")}})`, tabelgedreven:

| Request | Status | Body / header |
|---|---|---|
| `GET /api/health` | 200 | JSON-object met precies één veld, `version` = `version.String()` (in tests `"dev"`) |
| `GET /api/openapi.json` | 200 | geparste JSON heeft `paths["/api/health"]` |
| `GET /api/onbekend` | 404 | body bevat niet `placeholder` |
| `GET /` | 200 | `placeholder` |
| `GET /runs/42` | 200 | `placeholder` |
| `GET /assets/app.js` | 200 | `js` |
| `GET /assets/` | 200 | `placeholder` (geen listing) |
| `GET /../../etc/passwd` (zet `req.URL.Path` direct) | 200, 301, 307 of 404 | bij 301/307: `Location` begint met `/` en bevat geen `..`; bij 200: `placeholder` |

`http.ServeMux` canoniseert een pad met `..` met een redirect vóór de handler; die rij bewijst
dus alleen dat de mux niets buiten de site serveert. Daarom ook `TestSPAEscapes`, rechtstreeks
tegen `spa(site)` (zelfde `MapFS`, test in package `server`): `req.URL.Path` =
`/../../etc/passwd`, `/assets/../../etc/passwd` en `/assets/` → 200 en body `placeholder`.

Plus `TestEmbeddedSiteHasIndex`: `fs.ReadFile(web.FS(), "index.html")` slaagt.

- [ ] **Step 2: Draai ze en zie ze falen**

Run: `go test -race ./internal/server/ ./internal/web/`
Expected: FAIL, build error.

- [ ] **Step 3: Implementeer**

- `web.go`: `//go:embed dist`; `FS()` panikeert niet maar geeft `fs.Sub(dist, "dist")` (een fout
  daar is een bouwfout; een `panic` met een duidelijke tekst mag).
- `index.html`: één regel tekst met "lighthouse", geen externe bronnen.
- `server.New`: `mux := http.NewServeMux()`; `cfg := huma.DefaultConfig("lighthouse",
  version.String())` met `OpenAPIPath = "/api/openapi"`, `DocsPath = "/api/docs"`,
  `SchemasPath = "/api/schemas"` en `CreateHooks = nil` (anders zet Huma een veld `$schema` in
  elke body, en spec §4 schrijft `{"version": …}` voor); `api := humago.New(mux, cfg)`; `huma.Get(api, "/api/health",
  …)` met `OperationID: "get-health"` en output `struct{ Body struct{ Version string
  \`json:"version"\` } }`. Daarna `mux.Handle("/api/", http.NotFoundHandler())` (de specifieke
  Huma-patronen winnen) en `mux.Handle("/", spa(site))`.
- `spa.go`: `spa(site fs.FS) http.Handler` — `path.Clean` van het pad zonder leidende `/`;
  bestaat dat als **bestand** in `site`, serveer het met `http.ServeFileFS`; anders
  `index.html`. Een map of een fout telt als "bestaat niet".

- [ ] **Step 4: Draai de tests**

Run: `go test -race ./internal/server/ ./internal/web/ && CGO_ENABLED=0 go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/web internal/server
git commit -m "feat(server): Huma-API met health, OpenAPI en SPA-terugval (#63)"
```

### Task 5: `lighthouse serve` — vlaggen, signalen en shutdown

**Files:**
- Create: `internal/cli/serve.go`
- Modify: `internal/cli/root.go` (`root.AddCommand(newServeCommand())`; de `Short` van root
  wordt `"Install the lighthouse packages and run the lighthouse server"`)
- Test: `internal/cli/serve_test.go`, `e2e/serve_test.go` (`//go:build e2e`)

**Interfaces:**
- Consumes: `store.DefaultPath`, `store.Open`, `Store.Close` (Task 2–3); `server.New`, `web.FS`
  (Task 4).
- Produces: `const defaultAddr = "127.0.0.1:8127"`; `func newServeCommand() *cobra.Command`;
  `func serve(ctx context.Context, addr, dbPath string, errOut io.Writer) error`.

- [ ] **Step 1: Schrijf de falende tests**

`internal/cli/serve_test.go`:
- `TestServeDefaults`: `serve --help` bevat `127.0.0.1:8127` en `--db`.
- `TestServeStopsOnCancel`: `serve(ctx, "127.0.0.1:0", <tmp>/l.db, &syncBuf)` in een goroutine;
  wacht tot de buffer `lighthouse: listening on ` bevat (poll met deadline 5 s), lees het adres,
  `GET http://<addr>/api/health` = 200; `cancel()`; `serve` geeft `nil` binnen 11 s. Gebruik
  een buffer met mutex: de goroutine schrijft terwijl de test leest (`-race`).

`e2e/serve_test.go`, hergebruik `buildLighthouse`:
- `TestServeSignals`, tabel `{SIGTERM, SIGINT}`: start de binary met `serve --addr 127.0.0.1:0
  --db <tmp>/l.db`, lees stderr regel voor regel tot `listening on `, `GET /api/health` = 200,
  stuur het signaal, `Wait` → exitcode 0.
- `TestServeSecondInstanceRefused`: eerste server draait (zoals hierboven); tweede met een ander
  `--addr` (`127.0.0.1:0`) en dezelfde `--db` → exitcode 1, stderr bevat
  `another lighthouse serve holds` en `l.db.lock`; de eerste geeft daarna nog 200 op
  `/api/health`.
- `TestServePortInUse`: houd zelf een `net.Listen("tcp", "127.0.0.1:0")` open; `serve --addr
  <dat adres>` → exitcode 1; daarna start `serve --addr 127.0.0.1:0` op dezelfde `--db` wel
  (geen achtergebleven slot).

- [ ] **Step 2: Draai ze en zie ze falen**

Run: `go test -race -run TestServe ./internal/cli/ && go test -race -tags e2e -run TestServe ./e2e/`
Expected: FAIL, `serve` undefined / unknown command "serve".

- [ ] **Step 3: Implementeer**

`newServeCommand`: vlaggen `--addr` (standaard `defaultAddr`) en `--db` (standaard leeg →
`store.DefaultPath()`); `RunE` maakt `signal.NotifyContext(cmd.Context(), os.Interrupt,
syscall.SIGTERM)` en roept `serve`. `serve`: `store.Open` → `net.Listen` (fout: sluit de store,
geef de fout) → print `lighthouse: listening on <ln.Addr()>` naar `errOut` →
`(&http.Server{Handler: server.New(web.FS()), ReadHeaderTimeout: 10 * time.Second}).Serve(ln)`
in een goroutine → wacht op `ctx.Done()` of een `Serve`-fout → `Shutdown` met een context van
10 s; bij `context.DeadlineExceeded` `Close` en toch `nil` → `store.Close`. Een `Serve`-fout
anders dan `http.ErrServerClosed` geef je terug.

- [ ] **Step 4: Draai de tests**

Run: `go test -race -shuffle=on ./... && go test -race -shuffle=on -tags e2e ./e2e/ -v`
Expected: PASS. De e2e-run vraagt `uv` op het `PATH` voor de bestaande pack/python-tests.

- [ ] **Step 5: Commit**

```bash
git add internal/cli e2e/serve_test.go
git commit -m "feat(cli): lighthouse serve met nette stop op SIGINT en SIGTERM (#63)"
```

### Task 6: Deploy en documentatie

**Files:**
- Create: `deploy/systemd/lighthouse.service`
- Create: `deploy/tailscale-serve.sh` (uitvoerbaar)
- Create: `docs/install.md`
- Modify: `README.md` (sectie *Status*, nieuwe sectie *Running the server*)

**Interfaces:**
- Consumes: de vlaggen en de standaardpoort uit Task 5.

- [ ] **Step 1: Schrijf de unit**

Precies de regels uit spec §6: `Description=lighthouse — development chain server`,
`Type=exec`, `ExecStart=%h/go/bin/lighthouse serve --db %S/lighthouse/lighthouse.db`,
`StateDirectory=lighthouse`, `StateDirectoryMode=0700`, `Restart=on-failure`, `RestartSec=5`,
`ProtectSystem=strict`, `ProtectHome=read-only`, `NoNewPrivileges=true`,
`StandardOutput=journal`, `StandardError=journal`, `SyslogIdentifier=lighthouse`,
`[Install] WantedBy=default.target`. Een kop met commentaar: wat de unit doet, waarom loopback,
waarom `StateDirectory` (de `226/NAMESPACE`-val), en de installatieregels. Geen `Slice=`, geen
`EnvironmentFile=`.

- [ ] **Step 2: Verifieer de unit**

Run: `XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}" systemd-analyze --user verify deploy/systemd/lighthouse.service`
Expected: geen uitvoer, rc 0. Klaagt hij alleen dat `%h/go/bin/lighthouse` ontbreekt, draai dan
eerst `go install ./cmd/lighthouse`.

- [ ] **Step 3: Schrijf `deploy/tailscale-serve.sh`**

`#!/usr/bin/env bash`, `set -euo pipefail`; `port="${1:-8127}"`; controleer dat `port` alleen
cijfers is (anders exit 2 met een melding); `tailscale serve --bg --https="$port"
"http://127.0.0.1:$port"`; `tailscale serve status`. Commentaar: idempotent, `--bg` overleeft een
reboot, nooit `tailscale funnel`, geen authenticatie dus de tailnet is de grens (spec,
*Beveiliging*), de eenmalige voorwaarden, en hoe je het uitzet (`tailscale serve
--https=<port> off`).

`shellcheck` staat niet op het `PATH`; gebruik de gepinde binary via de resolver van de repo.
Maak het script uitvoerbaar en controleer het apart:

```bash
chmod +x deploy/tailscale-serve.sh
bash -n deploy/tailscale-serve.sh
sc="$(source pack/shell/scripts/lib/shellcheck-bin.sh && resolve_shellcheck)" && "$sc" deploy/tailscale-serve.sh; echo "shellcheck rc=$?"
deploy/tailscale-serve.sh abc; echo "rc=$?"
```

Expected: `shellcheck rc=0`; de laatste regel `rc=2`. Geeft de resolver rc 4 (kon niet
controleren), stop en meld het: sla de lint nooit stil over.

- [ ] **Step 4: Schrijf `docs/install.md` en de README**

`docs/install.md` (Engels), in deze volgorde: vereisten (Linux met systemd, Go, optioneel
Tailscale); `go install github.com/CorneeldH/lighthouse/cmd/lighthouse@main` (niet `@latest`:
dat is de nieuwste tag, en v0.1.5 heeft nog geen `serve`; zeg dat `@latest` pas werkt na een
release met `serve`) of `go install ./cmd/lighthouse` vanuit een checkout; de unit kopiëren
naar `~/.config/systemd/user/` en het `ExecStart`-pad aanpassen als de binary elders staat;
`systemctl --user daemon-reload`; `systemctl --user enable --now lighthouse.service`;
`loginctl enable-linger "$USER"`; controleren met `curl -s http://127.0.0.1:8127/api/health`;
logs met `journalctl --user -u lighthouse`; tailnet-toegang met `deploy/tailscale-serve.sh` en
de voorwaarden (`sudo tailscale set --operator="$USER"`, HTTPS-certificaten aan in de
tailnet-admin); de beveiligingsnoot (geen authenticatie, de tailnet is de grens). Links relatief
(`../deploy/systemd/lighthouse.service`).
README: vervang "Pre-M0: no code yet." door één zin over de stand (de pakketten via `init`, de
server via `serve`), en een sectie *Running the server* met een relatieve link naar
`docs/install.md`.

- [ ] **Step 5: Verifieer de documenten**

Run: `uv run pytest -q` (o.a. `test_instruction_files.py`) en controleer dat elke relatieve
link in `docs/install.md` en `README.md` naar een bestaand bestand wijst (de
`worktree-linkcheck`-hook blokkeert anders al bij de write).
Run ook: `git add deploy/tailscale-serve.sh && git ls-files -s deploy/tailscale-serve.sh`
Expected: groen, en de modus is `100755`.

- [ ] **Step 6: Commit**

```bash
git add deploy docs/install.md README.md
git commit -m "docs(deploy): systemd-user-unit, tailscale-serve-script en installatiepagina (#63)"
```

### Task 7: Eindcontrole

- [ ] **Step 1: Draai elke KPI-rij van `lighthouse-go-writing`**

```bash
git ls-files -z '*.go' | xargs -0r gofmt -l
go mod tidy -diff
go vet ./...
golangci-lint run ./...
go test -race -shuffle=on ./...
go test -race -shuffle=on -tags e2e ./e2e/ -v
CGO_ENABLED=0 go build ./...
bash -c "$(python3 .lighthouse/lib/lighthouse_lib/config.py test_gate.command)"
go run ./cmd/lighthouse init --upgrade </dev/null
```

Expected: de eerste twee leeg; de rest rc 0; de laatste print `No changes.`. Meld per rij de
uitkomst en de omgeving (linux/amd64, tags, `-race`, `GOWORK`).

- [ ] **Step 2: Rooktest met de echte binary**

Niet met `go run`: dat stuurt SIGTERM niet door naar de server, die dan als wees blijft
draaien. Bouw eerst een binary en signaleer die rechtstreeks:

```bash
tmp="$(mktemp -d)"
go build -o "$tmp/lighthouse" ./cmd/lighthouse
"$tmp/lighthouse" serve --addr 127.0.0.1:0 --db "$tmp/l.db" 2>"$tmp/err" &
pid=$!
```

Lees het adres uit `$tmp/err` (regel `lighthouse: listening on <adres>`); dan
`curl -s http://<adres>/api/health`, `curl -s http://<adres>/api/openapi.json | head -c 200`,
`curl -s http://<adres>/iets`; daarna `kill -TERM "$pid"; wait "$pid"; echo "exit=$?"`.
Expected: `{"version":"dev"}`, een OpenAPI-document, de placeholder, `exit=0`.

Installeren als dienst op de host is geen stap van dit plan: de gebruiker doet dat met de hand
volgens `docs/install.md`.
