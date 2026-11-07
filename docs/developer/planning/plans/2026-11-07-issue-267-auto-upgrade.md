# Automatische upgrade van de binary (#267) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `lighthouse self-update` vervangt de eigen binary door de nieuwste release als die nieuwer
is en de checksum klopt; een dagelijkse user-timer roept het aan en herstart `lighthouse.service`.

**Architecture:** Een nieuw package `internal/selfupdate` doet het werk: tag ophalen, vergelijken,
asset en `SHA256SUMS` downloaden, controleren, atomair vervangen, optioneel herstarten. Alle
buitenwereld (HTTP-basis-URL's, doelpad, GOOS/GOARCH, huidige versie, restart) komt binnen via
`Options`, zodat de tests een `httptest`-server en een tijdelijke map gebruiken.
`internal/cli/selfupdate.go` vult `Options` met de echte waarden. Twee units in `deploy/systemd/`
roepen het commando dagelijks aan.

**Tech Stack:** Go (`net/http`, `crypto/sha256`, `os`, `os/exec`, cobra), systemd user units, Markdown.

**Spec:** [2026-11-07-issue-267-auto-upgrade-design.md](../specs/2026-11-07-issue-267-auto-upgrade-design.md)

## Global Constraints

- Repo van de releases: `CorneeldH/lighthouse`. Tag-API: `GET <apiBase>/repos/CorneeldH/lighthouse/releases/latest`, veld `tag_name`. Echte `apiBase` = `https://api.github.com`.
- Downloads: `<downloadBase>/CorneeldH/lighthouse/releases/download/<tag>/<asset>`. Echte `downloadBase` = `https://github.com`.
- Asset: `lighthouse_<GOOS>_<GOARCH>`; checksumbestand `SHA256SUMS` (`sha256sum`-formaat: `<hex>  <naam>`, ook `<hex> *<naam>` accepteren).
- Huidige versie `dev` (zie `internal/version`): niets aanraken, exit 0.
- Alleen een strikt nieuwere tag vervangt. Tags zijn `vX.Y.Z` (cijfers, numeriek vergeleken: `v0.11.10` > `v0.11.9`); elke andere vorm (ook `-rc.N`) is een fout.
- GOOS `windows`: weigeren met een fout.
- Vervangen: tijdelijk bestand in de map van het doel, `chmod 0755`, `os.Rename`. Bij elke fout vóór de rename blijft het doel ongewijzigd en is het tijdelijke bestand weg.
- Doel = `os.Executable()` na `filepath.EvalSymlinks`: een symlink wordt gevolgd, het bestand erachter vervangen.
- Restart alleen na een vervanging: `systemctl --user try-restart <unit>`. Mislukt die: fout, nieuwe binary blijft.
- Elke uitkomst één regel, prefix `self-update: `. Teksten exact: `self-update: skipped — dev build`, `self-update: up to date (<tag>)`, `self-update: replaced <oud> with <nieuw>`, `self-update: restarted <unit>`.
- Geen GitHub-token nodig; is `GH_TOKEN` gezet, dan gaat hij mee als `Authorization: Bearer` op de API-call.
- Geen nieuwe dependency in `go.mod`.
- Go volgt `/lighthouse-go-standard`, tests `/lighthouse-test-standard`. Code, commentaar en unit-commentaar Engels; docs, commitberichten en dit plan Nederlands; changelog-fragment Engels.

## Review Focus

1. **`SHA256SUMS` met `*`-prefix of extra regels voor andere assets** — de juiste regel op naam kiezen; `*naam` telt ook. Test in Task 1 (`TestRunAcceptsBinaryModeChecksumLine`).
2. **Symlink als doel** — het bestand achter de link wordt vervangen, de link blijft een link. Test in Task 1 (`TestRunReplacesSymlinkTarget`).
3. **Download 404/500 halverwege** — fout, oude binary byte-gelijk, geen tijdelijk bestand achter in de map. Test in Task 1 (`TestRunKeepsBinaryOnDownloadError`).
4. **Prerelease- of rare tag uit de API** (`v0.12.0-rc.1`, `0.12.0`, leeg) — fout, niets vervangen. Test in Task 1 (`TestRunRejectsMalformedTag`).
5. **Restart mislukt na vervanging** — fout terug, nieuwe binary staat er. Test in Task 1 (`TestRunRestartFailureKeepsNewBinary`).

---

### Task 1: package `internal/selfupdate`

**Files:**
- Create: `internal/selfupdate/selfupdate.go`
- Test: `internal/selfupdate/selfupdate_test.go`

**Interfaces:**
- Produces:
  ```go
  type Outcome int
  const (SkippedDev Outcome = iota; UpToDate; Replaced)
  type Options struct {
      Current      string            // version.String()
      Target       string            // resolved path of the binary to replace
      GOOS, GOARCH string
      Repo         string            // "CorneeldH/lighthouse"
      APIBase      string            // "https://api.github.com"
      DownloadBase string            // "https://github.com"
      Token        string            // optional, GH_TOKEN
      Client       *http.Client      // nil = http.DefaultClient
      Restart      func() error      // nil = no restart
  }
  type Result struct { Outcome Outcome; From, To string }
  func Run(ctx context.Context, o Options) (Result, error)
  ```
  `Run` roept `Restart` alleen aan bij `Replaced`. Het commando (Task 2) maakt de regels; `Run` print niets.

- [ ] **Step 1: Schrijf de falende tests**

  Hulp in de test: een `httptest.Server` die `/repos/CorneeldH/lighthouse/releases/latest` beantwoordt met `{"tag_name": tag}` en `/CorneeldH/lighthouse/releases/download/<tag>/lighthouse_linux_amd64` en `.../SHA256SUMS` uit een map; doel = `t.TempDir()/lighthouse` met inhoud `old`. Nieuwe inhoud `new`.

  - `TestRunReplacesOnNewerTagAndGoodChecksum`: Current `v0.11.2`, tag `v0.11.3` → `Result{Replaced, "v0.11.2", "v0.11.3"}`, doel bevat `new`, mode `0755`, map bevat alleen `lighthouse`.
  - `TestRunKeepsBinaryOnBadChecksum`: hash in `SHA256SUMS` klopt niet → fout met `checksum`, doel bevat `old`, geen extra bestanden.
  - `TestRunKeepsBinaryWhenAssetMissingFromSums`: `SHA256SUMS` zonder regel voor het asset → fout, doel `old`.
  - `TestRunUpToDate`: tabel (`v0.11.3`/`v0.11.3`, `v0.11.10`/`v0.11.9`, `v1.0.0`/`v0.99.99`) → `UpToDate`, doel `old`, downloadpad nooit opgevraagd (teller in de server = 0).
  - `TestRunNewerComparesNumerically`: Current `v0.11.9`, tag `v0.11.10` → `Replaced`.
  - `TestRunSkipsDevBuild`: Current `dev` → `SkippedDev`, server nooit aangeroepen.
  - `TestRunRejectsWindows`: GOOS `windows` → fout, doel `old`.
  - `TestRunRejectsMalformedTag`: tabel `v0.12.0-rc.1`, `0.12.0`, `""` → fout, doel `old`.
  - `TestRunKeepsBinaryOnDownloadError`: asset-pad geeft 500 → fout, doel `old`, geen extra bestanden.
  - `TestRunAcceptsBinaryModeChecksumLine`: `SHA256SUMS` met een regel voor een ander asset en `<hex> *lighthouse_linux_amd64` → `Replaced`.
  - `TestRunReplacesSymlinkTarget`: `Target` is het resultaat van `filepath.EvalSymlinks` op een link `bin/lighthouse → real/lighthouse`; na `Run` is `bin/lighthouse` nog een link en bevat `real/lighthouse` `new`. (Dit test dat het package het opgeloste pad vervangt; het oplossen zelf zit in Task 2.)
  - `TestRunRestartOnlyAfterReplace`: `Restart` telt aanroepen: 1 na `Replaced`, 0 na `UpToDate` en 0 na `SkippedDev`.
  - `TestRunRestartFailureKeepsNewBinary`: `Restart` geeft een fout → `Run` geeft een fout die hem omhult, doel bevat `new`.
  - `TestRunSendsTokenOnlyWhenSet`: `Token` `abc` → API-verzoek heeft `Authorization: Bearer abc`; leeg → geen header.

- [ ] **Step 2: Draai ze, verwacht rood**

  Run: `go test ./internal/selfupdate/`
  Expected: FAIL (package bestaat niet / `Run` ongedefinieerd).

- [ ] **Step 3: Implementeer `Run` in `internal/selfupdate/selfupdate.go`**

  Volgorde: dev-check → windows-check → tag ophalen → tag parsen (`^v(\d+)\.(\d+)\.(\d+)$`, ook Current) → vergelijken → `SHA256SUMS` ophalen en regel kiezen → asset streamen naar `os.CreateTemp(filepath.Dir(Target), ".lighthouse-update-*")` met `sha256` erbij → hash vergelijken → `Chmod(0o755)` → `os.Rename` → `Restart`. Een `defer` haalt het tijdelijke bestand weg zolang de rename niet gelukt is. Een niet-200-antwoord is een fout met de URL en de status.

- [ ] **Step 4: Draai de tests, verwacht groen**

  Run: `go test -race ./internal/selfupdate/`
  Expected: `ok`.

- [ ] **Step 5: Mutatiecheck** (`/lighthouse-test-standard`): zet tijdelijk de hash-vergelijking uit en de vergelijking op `>=`; `TestRunKeepsBinaryOnBadChecksum` en `TestRunUpToDate` moeten rood worden. Terugzetten.

- [ ] **Step 6: Commit**

  ```bash
  git add internal/selfupdate/
  git commit -m "feat(selfupdate): binary vervangen door nieuwere release met checksum (#267)"
  ```

### Task 2: commando `lighthouse self-update`

**Files:**
- Create: `internal/cli/selfupdate.go`
- Modify: `internal/cli/root.go` (`root.AddCommand(newSelfUpdateCommand())` na `newAskCommand`)
- Test: `internal/cli/selfupdate_test.go`

**Interfaces:**
- Consumes: `selfupdate.Run`, `selfupdate.Options`, `selfupdate.Result`, `version.String()`.
- Produces: `func newSelfUpdateCommand() *cobra.Command` met vlag `--restart string` (unitnaam, leeg = geen restart); `func reportSelfUpdate(out io.Writer, r selfupdate.Result, unit string)` die de regels uit *Global Constraints* schrijft.

- [ ] **Step 1: Schrijf de falende tests**

  - `TestSelfUpdateSkipsDevBuild`: `NewRoot` met buffers, args `self-update`; een testbinary rapporteert `dev` → `execute` geeft 0, stdout is exact `self-update: skipped — dev build\n`.
  - `TestReportSelfUpdate`: tabel → `UpToDate{To:"v0.11.3"}` geeft `self-update: up to date (v0.11.3)\n`; `Replaced{From:"v0.11.2",To:"v0.11.3"}` met unit `lighthouse.service` geeft `self-update: replaced v0.11.2 with v0.11.3\nself-update: restarted lighthouse.service\n`; zonder unit alleen de eerste regel.

- [ ] **Step 2: Draai ze, verwacht rood**

  Run: `go test ./internal/cli/ -run 'SelfUpdate'`
  Expected: FAIL (`unknown command "self-update"` / ongedefinieerd).

- [ ] **Step 3: Implementeer**

  `RunE`: `exe, _ := os.Executable()` → `filepath.EvalSymlinks` (fout = fout) → `Options` met `runtime.GOOS/GOARCH`, de vaste repo en bases uit *Global Constraints*, `Token: os.Getenv("GH_TOKEN")`, en bij `--restart` een `Restart` die `exec.CommandContext(ctx, "systemctl", "--user", "try-restart", unit).CombinedOutput()` draait (fout met de output erin). Context: `cmd.Context()` met een timeout van 5 minuten. Fout van `Run` gaat terug als gewone error (`execute` print `lighthouse: …`, exit 1).

- [ ] **Step 4: Draai de tests, verwacht groen**

  Run: `go test -race ./internal/cli/ -run 'SelfUpdate'` en `go run ./cmd/lighthouse self-update`
  Expected: `ok`; het commando print `self-update: skipped — dev build`.

- [ ] **Step 5: Commit**

  ```bash
  git add internal/cli/selfupdate.go internal/cli/selfupdate_test.go internal/cli/root.go
  git commit -m "feat(cli): lighthouse self-update (#267)"
  ```

### Task 3: units, docs en changelog

**Files:**
- Create: `deploy/systemd/lighthouse-upgrade.service`, `deploy/systemd/lighthouse-upgrade.timer`
- Modify: `docs/install.md` (na *Run it as a user service*), `docs/releasing.md` (`## Upgrading`)
- Create: `changelog.d/issue-267-auto-upgrade.md`

- [ ] **Step 1: Units schrijven**

  Service, in de stijl van `deploy/systemd/lighthouse.service` (kopcommentaar met *Why*-regels: waarom oneshot, waarom `ReadWritePaths=%h/go/bin`, dat een release zonder review live gaat, installatiestappen):
  `Type=oneshot`, `EnvironmentFile=-%h/.config/lighthouse/forge.env` (voor een optionele `GH_TOKEN`), `ExecStart=%h/go/bin/lighthouse self-update --restart lighthouse.service`, `ProtectSystem=strict`, `ProtectHome=read-only`, `ReadWritePaths=%h/go/bin`, `NoNewPrivileges=true`, journal-uitvoer, `SyslogIdentifier=lighthouse-upgrade`.
  Timer: `OnCalendar=daily`, `RandomizedDelaySec=1h`, `Persistent=true`, `[Install] WantedBy=timers.target`.

- [ ] **Step 2: Units controleren**

  Run: `systemd-analyze --user verify deploy/systemd/lighthouse-upgrade.service deploy/systemd/lighthouse-upgrade.timer`
  Expected: geen uitvoer (of alleen een waarschuwing dat `~/go/bin/lighthouse` het commando nog niet kent, niet over de unit-syntax).

- [ ] **Step 3: Docs**

  `docs/install.md`: een sectie *Upgrade automatically* (Engels, zoals de rest van het bestand): units kopiëren naar `~/.config/systemd/user/`, `systemctl --user daemon-reload && systemctl --user enable --now lighthouse-upgrade.timer`; uitzetten `systemctl --user disable --now lighthouse-upgrade.timer`; welke versie draait: `lighthouse version`, `curl -s 127.0.0.1:8127/api/health`, laatste run `journalctl --user -u lighthouse-upgrade.service -n 5`; een `dev`-build wordt overgeslagen; elke release gaat zonder review live.
  `docs/releasing.md` *Upgrading*: bij stap 1 en 2 één zin dat `lighthouse-upgrade.timer` die doet als hij aanstaat, met een relatieve link naar de nieuwe sectie.

- [ ] **Step 4: Changelog-fragment**

  ```markdown
  ---
  type: added
  ---
  `lighthouse self-update` replaces the binary with the newest release when it is newer and its checksum matches; `deploy/systemd/lighthouse-upgrade.timer` runs it daily and restarts `lighthouse.service`.
  ```

- [ ] **Step 5: Volledige suite**

  Run: `go test -race -shuffle=on ./... && uv run pytest -q tests/ && golangci-lint run ./...`
  Expected: alles groen, `0 issues.`

- [ ] **Step 6: Commit**

  ```bash
  git add deploy/systemd/lighthouse-upgrade.service deploy/systemd/lighthouse-upgrade.timer docs/install.md docs/releasing.md changelog.d/issue-267-auto-upgrade.md
  git commit -m "feat(deploy): dagelijkse lighthouse-upgrade.timer met docs (#267)"
  ```

### Task 4: op de Beelink aanzetten (na merge en release)

Kan pas als een release `self-update` bevat. Geen code; bewijs voor AC 5.

- [ ] **Step 1:** Binary eenmalig met de hand naar die release (commando uit `docs/install.md`), daarna `lighthouse version` → de nieuwe tag.
- [ ] **Step 2:** Units installeren en aanzetten volgens de nieuwe sectie in `docs/install.md`.
- [ ] **Step 3:** `systemctl --user start lighthouse-upgrade.service`, dan `journalctl --user -u lighthouse-upgrade.service -n 5` → `self-update: up to date (<tag>)`; `systemctl --user list-timers lighthouse-upgrade.timer` → volgende run gepland. Beide regels als bewijs op #267.
