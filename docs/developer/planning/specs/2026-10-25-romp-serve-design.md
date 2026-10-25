# Ontwerp: romp van `lighthouse serve`

Issue: #63 · Milestone: M1 — Romp en zicht · Datum: 2026-10-25

## Aanleiding en doel

M1 zet de romp van de binary neer. #63 is het fundament waar de rest van M1 op bouwt (het issue
blokkeert er drie): een HTTP-server met een OpenAPI-beschrijving, een SQLite-database en een
ingebedde webpagina. Cobra (`internal/cli/root.go`) en golangci-lint staan er al.

`lighthouse serve` is een **dienst die altijd aan staat**, geen proces dat je met de hand start
als je even wilt kijken. Hij draait als systemd-user-unit, één per machine, en is via Tailscale
bereikbaar vanaf andere apparaten. Een nieuwe gebruiker verwacht dat lighthouse zijn eigen
dienst en database neerzet, zoals immich en miniflux hun database meebrengen. Daarom horen de
unit en de installatie-uitleg in deze repo, niet alleen in de host-deploy van de auteur.

## Scope

#63 levert:

1. het subcommando `lighthouse serve` (Huma v2, graceful shutdown);
2. de database-laag: openen met pragma's, één-server-slot, genummerde migraties;
3. een ingebedde placeholderpagina met SPA-terugval;
4. een systemd-user-unit en een `tailscale serve`-script in `deploy/`, met een installatiepagina;
5. CI-aanpassingen voor `-race` en `CGO_ENABLED=0`.

**Buiten scope:**

- De tabel `runs` en alles van de dispatcher (M2).
- De SPA zelf (React, Vite, orval-client): #63 levert alleen de placeholder en de terugval.
- Endpoints die de forge lezen of het usage-venster tonen: andere M1-issues bouwen ze op deze
  romp.
- Een commando `lighthouse service install`. De unit is een bestand met uitleg; een
  installatiecommando kan later een eigen issue worden.
- macOS (launchd). Alleen Linux met systemd.

## Non-goals en anti-goals

- **Geen authenticatie**, niet in M1 en niet in M2 (besluit gebruiker). De tailnet is de
  toegangsgrens. Zie *Beveiliging*.
- **Geen tweede database** en geen Postgres (no-go van de milestone).
- **Geen cgo.** `CGO_ENABLED=0 go build ./...` moet slagen.
- **Anti-goal:** paden, hostnamen of IP's van de auteur in code of in `deploy/`. De README zegt
  het al: "nothing in the code may depend on the author's paths, host or board". De unit gebruikt
  `%h`; het serve-script neemt de poort als argument en kent geen hostnaam.
- **Anti-goal:** twee servers die tegelijk dezelfde database migreren.

## Besluiten

### 1. Hoe de server leeft

- **Eén server per machine**, die al je repos bedient vanuit één database.
- **Bind op loopback:** standaard `127.0.0.1:8127`; `--addr` overschrijft het.
  *Mijn keuze voor 8127:* op de host van de auteur is 8090–8126 bezet; een vaste standaard laat
  UI, hooks en later de dispatcher de server vinden zonder configuratie.
- **Tailnet-toegang via `tailscale serve`**, naar het model van kenn-forge in de host-deploy van
  de auteur: `tailscale serve --bg --https=8127 http://127.0.0.1:8127`. Dat geeft HTTPS met een
  Tailscale-certificaat, alleen op de tailnet. Nooit `tailscale funnel`.
- **Eén server tegelijk:** `serve` neemt een exclusieve `flock` op `<db>.lock` (naast het
  databasebestand) vóór de database opengaat en houdt hem tot het einde van het proces. Lukt dat
  niet, dan stopt `serve` met exit 1 en de melding
  `lighthouse: another lighthouse serve holds <pad>.lock`. Dat geldt ook bij een ander `--addr`:
  het slot hoort bij de database, niet bij de poort.
- **Stoppen:** bij SIGINT of SIGTERM roept `serve` `http.Server.Shutdown` aan met een deadline
  van 10 seconden, sluit daarna de database en geeft exit 0. Na de deadline sluit hij harde
  verbindingen en geeft hij nog steeds exit 0 (*mijn keuze*: systemd moet een gewone stop niet
  als fout zien).
- **Logs** naar stderr; journald vangt ze op. Bij de start één regel met het adres waarop hij
  luistert, bijvoorbeeld `lighthouse: listening on 127.0.0.1:8127`. Tests met `--addr
  127.0.0.1:0` lezen de echte poort uit die regel.

### 2. Vlaggen

| Vlag | Standaard | Doel |
|---|---|---|
| `--addr` | `127.0.0.1:8127` | luisteradres |
| `--db` | `${XDG_STATE_HOME:-$HOME/.local/state}/lighthouse/lighthouse.db` | databasepad; tests zetten een tijdelijk pad |

`XDG_STATE_HOME` telt alleen als het een absoluut pad is (XDG-basisregel); anders geldt
`$HOME/.local/state`. De map wordt aangemaakt met mode `0700` als hij ontbreekt.

### 3. Database

- **Driver:** `modernc.org/sqlite` (pure Go), als `database/sql`-driver `sqlite`.
- **Pragma's** op elke verbinding, via de DSN (`_pragma=…`), zodat elke verbinding in de pool
  ze heeft: `journal_mode(WAL)`, `busy_timeout(5000)`, `foreign_keys(ON)`, `synchronous(NORMAL)`.
  M2 claimt runs met één `INSERT` in `BEGIN IMMEDIATE`; WAL en `busy_timeout` zijn daar de
  voorwaarde voor.
- **Migraties:** een eigen kleine runner, zonder extra dependency.
  - Bestanden `internal/store/migrations/NNNN_<naam>.sql`, vier cijfers, ingebed met `go:embed`.
  - De schemaversie staat in `PRAGMA user_version`. De runner leest die, draait elk bestand met
    een hoger nummer op volgorde, elk in één transactie, en zet `user_version` in diezelfde
    transactie.
  - Nummering zonder gaten en zonder dubbele nummers; de runner weigert anders te starten (een
    test pint dat op de ingebedde set).
  - Een database met een `user_version` hoger dan de hoogste bekende migratie: `serve` weigert
    met een melding (oude binary op een nieuwe database). *Mijn keuze.*
  - Ze draaien bij elke start van `serve`, na het slot uit besluit 1.
  - `0001_meta.sql` maakt alleen `meta(key TEXT PRIMARY KEY, value TEXT NOT NULL)` en schrijft
    `created_at`. Geen `runs`.

### 4. HTTP

- **Huma v2** op de standaard `net/http`-`ServeMux`, via de adapter `humago`. Geen chi.
- **Alles onder `/api/` is API.**
  - `GET /api/health` → 200, body `{"version": "<version.String()>"}`.
  - OpenAPI op `/api/openapi.json` en `/api/openapi.yaml`, de docs op `/api/docs`, de schema's
    onder `/api/schemas`. Dat is de bron voor de orval-client later.
  - Een onbekend pad onder `/api/` geeft 404 (de 404 van Huma of van de mux, niet `index.html`).
- **De rest is de webpagina** (`internal/web`, ingebed met `go:embed`):
  - `/` serveert `index.html`, een placeholder.
  - Een bestaand bestand in de ingebedde map wordt als bestand geserveerd.
  - Elk ander pad buiten `/api/` krijgt `index.html` met 200 (SPA-routing).
- De ingebedde map heet `internal/web/dist/`; de latere Vite-build schrijft daar zijn uitvoer.
  Nu staat er alleen een met de hand geschreven `index.html`.

### 5. Packages

| Package | Inhoud |
|---|---|
| `internal/cli` | `serve.go`: de Cobra-command, vlaggen, signalen, start en stop |
| `internal/server` | Huma-API, routes, SPA-terugval; een `http.Handler` die zonder netwerk testbaar is |
| `internal/store` | standaardpad, slot, openen met pragma's, migratierunner, `migrations/` |
| `internal/web` | `dist/` met `go:embed` en een `fs.FS` voor de server |

De opdeling is een richting; de bouwer mag een grens verleggen als een test of de lint daar om
vraagt.

### 6. Deploy

Nieuw, in deze repo:

- `deploy/systemd/lighthouse.service`, naar het model van `kenn-forge.service` in de host-deploy
  van de auteur, maar generiek:
  - `ExecStart=%h/go/bin/lighthouse serve --db %S/lighthouse/lighthouse.db` (`%h/go/bin` is
    waar `go install` hem neerzet); de docs zeggen dat je het pad aanpast als de binary elders
    staat. `%S` is in een user-unit de state-map van de user-manager (`$XDG_STATE_HOME`, anders
    `~/.local/state`), dus de unit en de map uit `StateDirectory=` wijzen altijd naar hetzelfde
    pad.
  - `StateDirectory=lighthouse` met `StateDirectoryMode=0700`. systemd maakt de map vóór de start
    aan en maakt hem schrijfbaar binnen de hardening. Zonder dat faalt de eerste start op een
    nieuwe machine: een `ReadWritePaths` naar een map die niet bestaat geeft `226/NAMESPACE`, en
    onder `ProtectHome=read-only` kan `serve` de map niet zelf aanmaken. *Mijn keuze.* Buiten
    systemd maakt `serve` de map aan, zoals besluit 2 zegt.
  - `Type=exec`, `Restart=on-failure`, `RestartSec=5`, `StandardOutput=journal`,
    `StandardError=journal`, `SyslogIdentifier=lighthouse`.
  - Hardening: `ProtectSystem=strict`, `ProtectHome=read-only`, `NoNewPrivileges=true`. Latere
    M1-issues die meer schrijven (zoals de usage-meter naar `~/.cache/lighthouse`) voegen
    `CacheDirectory=lighthouse` of een `ReadWritePaths` toe.
  - `[Install] WantedBy=default.target`.
  - Geen `Slice=` en geen `EnvironmentFile=`: die zijn host-eigen.
- `deploy/tailscale-serve.sh [poort]`: standaard 8127. Draait
  `tailscale serve --bg --https=<poort> http://127.0.0.1:<poort>` en daarna
  `tailscale serve status`. Idempotent, `set -euo pipefail`, en commentaar over waarom nooit
  `funnel`.
- Een installatiepagina `docs/install.md` (Engels, zoals de README; *mijn keuze*):
  `go install`, de unit kopiëren, `systemctl --user daemon-reload`,
  `systemctl --user enable --now lighthouse.service`, `loginctl enable-linger $USER`, het
  serve-script met zijn eenmalige voorwaarden (`tailscale set --operator=$USER`, HTTPS-certificaten
  aan in de tailnet), en controleren met `curl http://127.0.0.1:8127/api/health`.
- De README: de status "Pre-M0: no code yet" klopt niet meer; een korte sectie *Running the
  server* met een link naar `docs/install.md`.

Op de host van de auteur (de Beelink) installeert de auteur de unit met de hand volgens die
pagina. `deploy/beelink/` in de host-deploy raakt #63 niet.

### 7. CI en test-gate

- `-race`: de AC eist dat `go test -race ./...` groen is. `[test_gate] command` in
  `lighthouse.toml` krijgt `-race` (`go test -race -shuffle=on ./...`), en beide `go test`-stappen
  in `.github/workflows/ci.yml` ook: `tests/test_ci_go_test_pin.py` eist dezelfde vlaggen in de
  gate en in elke CI-stap, de e2e-stap inbegrepen. `-race` vraagt cgo; dat is in CI en lokaal
  aanwezig en staat los van de `CGO_ENABLED=0`-build.
- Een nieuwe CI-stap `CGO_ENABLED=0 go build ./...`.
- golangci-lint blijft zoals hij is (`default: standard`).

## Beveiliging

Geen authenticatie, bewust. Wie op de tailnet zit, kan via `tailscale serve` alles wat de API
kan. In M1 is de API alleen-lezen. In M2 gaat lighthouse runs starten; ook dan komt er volgens
het besluit geen authenticatie, dus elk apparaat op de tailnet kan dan een run starten. Dat is
het verschil met kenn-forge, dat om die reden `require_auth = true` draagt. De bind blijft
loopback, zodat niets buiten de host en de tailnet de server ziet. Wie het besluit later
heroverweegt: de plek is vóór de eerste schrijvende endpoint.

## Teststrategie

Per AC de test die faalt als het gedrag breekt:

| AC | Test |
|---|---|
| `serve` start, `--addr`, stopt op SIGINT/SIGTERM met exit 0 | e2e: bouw de binary, start `serve --addr 127.0.0.1:0 --db <tmp>`, lees de poort uit de log, `GET /api/health`, stuur `SIGTERM` (en apart `SIGINT`), verwacht exit 0 |
| standaard op `127.0.0.1` | unit: de standaardwaarde van `--addr` is `127.0.0.1:8127` |
| `/api/health` 200 met versie | `httptest` tegen de handler: status 200, `version` gelijk aan `version.String()` |
| OpenAPI van alle endpoints | `/api/openapi.json` geeft 200, en bevat het pad `/api/health` |
| standaardpad DB | unit op de padfunctie: met absoluut `XDG_STATE_HOME`, met relatief, en zonder |
| WAL en `busy_timeout` | open een tijdelijke database; `PRAGMA journal_mode` = `wal`, `PRAGMA busy_timeout` = `5000`, op meer dan één verbinding uit de pool |
| migraties twee keer | migreer, leg `sqlite_master` en `user_version` vast, migreer opnieuw, vergelijk; geen fout, gelijk schema, geen tabel `runs` |
| placeholder en SPA-terugval | `/` → 200 met de placeholder; `/iets/onbekends` → 200 met `index.html`; `/api/onbekend` → 404 en niet `index.html` |
| `CGO_ENABLED=0` | de CI-stap uit besluit 7 |
| lint en `-race` | de CI-stappen uit besluit 7 |

Daarnaast: een tweede slot op dezelfde database faalt (unit op `store`), en een nummergat in de
migratieset faalt (unit op de runner met een test-`fs.FS`).

De e2e-test draait onder de bestaande build-tag `e2e` in `e2e/`, of in een eigen testbestand
met dezelfde tag; aan de bouwer.

## Aan de bouwer

- De exacte Huma-versie (laatste v2) en de versie van `modernc.org/sqlite`.
- De vorm van de Huma-config (`huma.DefaultConfig`, paden onder `/api`).
- De inhoud en opmaak van de placeholderpagina: één regel tekst met de versie is genoeg.
- Of `serve` de versie in de startregel logt.
- De namen van functies en types binnen de packages.
