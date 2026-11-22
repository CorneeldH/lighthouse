# check_cla en checkpoint_scan naar Go — implementatieplan

> **Voor agentic workers:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkboxes (`- [ ]`).

**Doel:** `tools/check_cla.py` wordt `go run ./tools/check_cla` en `tools/hooks/checkpoint_scan.py` wordt het verborgen subcommando `lighthouse checkpoint-scan`, zonder gedragswijziging.

**Architectuur:** `tools/check_cla` is een `main`-pakket met `run(args, stdout, stderr) int` naar het voorbeeld van `ci/changelog` (#360). De scanner krijgt een eigen pakket `internal/checkpointscan` met de logica; `internal/cli/checkpoint_scan.go` is een dunne, verborgen cobra-command (`Hidden: true`) die het aanroept. De limieten uit `host.toml` leest het in-process via `config.HostValue` (`internal/config/host.go:229`), niet door `lighthouse config` opnieuw te starten.

**Tech stack:** Go (module-versie uit `go.mod`), `github.com/BurntSushi/toml` v1.6.0 (al afhankelijkheid), cobra, `os/exec` voor git en gitleaks.

**Spec:** [`2026-11-16-repo-zonder-python-design.md`](../specs/2026-11-16-repo-zonder-python-design.md) §1 (triage) en §4 (scripts in `tools/`, uitzondering `checkpoint_scan`).

## Global Constraints

- Geen gedragswijziging: zelfde oordeel, zelfde exitcode, zelfde stderr-contract waar een hook of workflow ervan afhangt.
- Exitcodes `check_cla`: 0 gedekt, 1 ondertekening ontbreekt, 2 niet te meten (register, git, lege range). `checkpoint-scan`: 0 schoon, 1 bevindingen of vastgehouden refs, 2 `COULD NOT CHECK` (elke fout, ook een onverwachte). Prefix `secret-scan:`.
- `go run` maakt van elke exitcode ≠ 0 een 1 (`ci/changelog/main.go:19-22`): `cla.yml` onderscheidt 1 en 2 niet (stap `Check the CLA` is één `run`), dus dat is veilig; de rapporttekst in de job-summary blijft het verschil dragen.
- `checkpoint-scan` staat niet in `--help` (`Hidden: true`); `lighthouse checkpoint-scan --help` werkt wel.
- Constanten letterlijk uit het origineel: `BATCH_BYTES = 64 MiB`, `HOOK_VOLUME_LIMIT = 256 MiB`, `FINDINGS_CAP = 200`, `PUSH_CHUNK = 500`, `CEILING_RCS = (137, -9)`, `DEFAULT_MEMORY_MAX = "2G"`, `DEFAULT_CPU_QUOTA = "200%"`, env `LIGHTHOUSE_SCAN_BATCH_BYTES` en `LIGHTHOUSE_SCAN_VOLUME_LIMIT`, `REQUIRED_AGREEMENT = "I have read the CLA document and I hereby sign the CLA."`, `IGNORED_COMMITTER_EMAILS = {"noreply@github.com"}`.
- Code, commentaar en identifiers Engels; docs, changelog en PR-body Nederlands. Go volgt `/lighthouse-go-standard`; tests volgen `/lighthouse-test-standard` (kernvraag: welke productiewijziging laat deze test falen?).
- Dogfood: een wijziging onder `internal/` die `init` raakt, vraagt `go run ./cmd/lighthouse init --upgrade` en een commit; verwacht `No changes.`.

## Review Focus

- `check_cla` met lege `--base` of `--head`: exit 2, nooit een groene run zonder meting (test in Taak 1).
- Register met datum die geen `YYYY-MM-DD` is, of een e-mailadres in meerdere lijsten: Python-`re`/`json` versus Go-`regexp`/`encoding/json` (hoofdletters, Unicode, dubbele sleutels) — zelfde oordeel (Taak 1).
- Commit met `Co-authored-by`-trailer in andere hoofdletters, of een body met NUL: de `%x00`-splitsing mag niet verschuiven (Taak 1).
- `checkpoint-scan` zonder `lighthouse` op PATH of zonder `host.toml`: defaults en geen self-exec; ongeldige `host.toml` is exit 2 (Taak 3).
- Een ref met 64-hex-sha (sha256-repo) en een remote als URL in plaats van naam (Taak 2 en 4).
- Een geheim in een blob die al in de ledger staat, met `--full`: opnieuw scannen (Taak 3).
- Hook zonder binary op PATH weigert te pushen met een duidelijke melding, nooit ongescand (Taak 5).

---

### Taak 1: `tools/check_cla` in Go

**Files:**
- Create: `tools/check_cla/main.go`, `tools/check_cla/cla.go`, `tools/check_cla/main_test.go`, `tools/check_cla/cla_test.go`
- Delete: `tools/check_cla.py`
- Modify: `.github/workflows/cla.yml:51,64`
- Test: `tests/test_check_cla.py` (triage; gaat weg met de port)

**Interfaces:**
- Produces: `func run(args []string, stdout, stderr io.Writer) int`; `main` roept `os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))`. Flags `--repo` (default `.`), `--base` (default `origin/main`), `--head` (default `HEAD`), `--register` (default `<repo>/.github/cla-signatures.json`).
- Produces (in `cla.go`): `type Identity struct{...}` met dezelfde velden als de Python-dataclass (`tools/check_cla.py:84`); `collectIdentities(repo, base, head string) ([]Identity, error)`, `loadRegister(path string) (Register, error)`, `validateRegister(Register) []string`, `unsigned([]Identity, Register) []Identity`, `renderReport(missing []Identity, repoURL string) string` (repo-URL uit `<--repo>/lighthouse.toml` `[forge] repo`, `check_cla.py:93-96`), `emit(...)` met append aan `GITHUB_STEP_SUMMARY` (`check_cla.py:330-336`; `cla.yml` leunt hierop voor het verschil 1 vs 2; test `TestReportLandsInStepSummary`), `checkAppendOnly(repo, base, registerPath string, Register) ([]string, error)`. Git via `git log --format=%h%x00%an%x00%ae%x00%cn%x00%ce%x00%B` (zes velden per commit).

- [ ] **Stap 1: Triage.** Loop de 62 tests van `tests/test_check_cla.py` langs en leg per groep een bestemming vast (spec §1). Verwachting: register-validatie, identiteiten-verzameling, `unsigned`, `check_append_only` en het rapport gaan naar Go; de workflow-tekstpins op `cla.yml` (`:948-1005`) pinnen `python3 tools/check_cla.py` en gaan weg als *onderwerp weg* (de regel verdwijnt), tenzij een invariant blijft (bijv. `ubuntu-latest`, nooit self-hosted): die pin herschrijf je naar Go. Houd de tabel bij voor de PR-body.
- [ ] **Stap 2: Schrijf de falende tests** in `cla_test.go`/`main_test.go` per getrieerde groep, tegen een tijdelijke git-repo (`git init` + commits in `t.TempDir()`), met assertions op exitcode en de stdout/stderr-tekst. Minimaal: `TestRunEmptyBaseOrHeadExits2`, `TestRunUnsignedIdentityExits1WithReport`, `TestRunAllCoveredExits0`, `TestRunInvalidRegisterExits2`, `TestRunRegisterNotAppendOnlyExits2`, `TestIgnoresNoreplyGithubCommitter`, `TestCoAuthorTrailerCounts`.
- [ ] **Stap 3: Draai ze:** `go test ./tools/check_cla/...` — verwacht FAIL (niet gedefinieerd).
- [ ] **Stap 4: Implementeer** `cla.go` en `main.go` met de signaturen hierboven; houd de tekst van elk rapport en elke foutmelding gelijk aan het origineel (Nederlands/Engels zoals die staat). Datum-regex `^\d{4}-\d{2}-\d{2}$` blijft; register als JSON via `encoding/json` met `UseNumber` in `map[string]any` en dezelfde isinstance-controles (`check_cla.py:161-165,339-346`; geen getypeerde struct die inzendingen afkeurt die Python negeert, geen float64 die de handtekening-sleutel verzwakt); `fullmatch` wordt een expliciet geanchord `^(?:…)$` (`check_cla.py:295`, ook in Taak 2 voor de remote-regels: `notgithub.com` mag niet matchen, port de other-host-gevallen) met `DisallowUnknownFields` alleen als het origineel onbekende velden afkeurt (controleer `validate_register`).
- [ ] **Stap 5: Draai:** `go test -race -shuffle=on ./tools/check_cla/...` — verwacht PASS. Mutatiecheck: breek per groep één regel productiecode en zie de bijbehorende test falen.
- [ ] **Stap 6: Wijs de workflow om.** In `cla.yml`: voeg vóór de stap `Check the CLA` een `actions/setup-go` toe met dezelfde gepinde SHA als `ci.yml:63`, `go-version-file: go.mod` en `cache: false` (`ci.yml:63-66`); verander de run in `go run ./tools/check_cla --base "$BASE_SHA" --head "$HEAD_SHA"`; werk de commentaarregels (`bare python3`, `stdlib-only`, `tools/check_cla.py` hollowed out) bij naar Go zonder de betekenis van de NOTE over niet-sluitende branch-protection te verliezen. Check: `permissions: contents: read` en `ubuntu-latest` blijven.
- [ ] **Stap 7: Verwijder** `tools/check_cla.py` en `tests/test_check_cla.py`. Draai `grep -rn check_cla.py . --include='*' -I | grep -v -e changelog.d -e CHANGELOG -e planning -e testdata` — verwacht leeg; de `$comment` in `.github/cla-signatures.json:2` noemt het script en wordt aangepast.
- [ ] **Stap 8: Commit** `feat(tools): check_cla naar Go (#362)`.

### Taak 2: `internal/checkpointscan` — kern (limieten, remote-regels, ledger, batches)

**Files:**
- Create: `internal/checkpointscan/limits.go`, `entire.go`, `ledger.go`, `batches.go` en bijbehorende `_test.go`
- Test: `tests/test_checkpoint_scan.py` (triage, gedeeltelijk)

**Interfaces:**
- Produces: `type Limits struct{ MemoryMax, CPUQuota string; Slice *string }`; `func LoadLimits() (Limits, error)` (via `config.HostValue("host.scan_memory_max" | "host.scan_cpu_quota" | "host.gate_slice")`; ontbrekend → defaults; ongeldige `host.toml` → fout, `CouldNotCheck`).
- Produces: `type CouldNotCheck struct{ Msg string }` (implementeert `error`); `type EntireSettings struct{...}`; `func ReadEntireSettings(toplevel string) (EntireSettings, error)` (`.entire/settings.json`); `func IsCheckpointRemote(s EntireSettings, remote, url string) bool` (URL- en SCP-regexes van `checkpoint_scan.py:761-790`, forge-hosts `github`→`github.com`, `gitlab`→`gitlab.com`); `func RemoteExclusion(repo, remote string) ([]string, error)`; `func LedgerPath(repo, config string) (string, error)`, `LoadLedger(path string) (map[string]struct{}, error)`, `AddToLedger(path string, blobs []string) error`; `type Blob struct{...}`; `func Batches(blobs map[string]Blob, limit int) [][]string`.

- [ ] **Stap 1: Triage** de tests van `tests/test_checkpoint_scan.py` (69) die dit deel raken; leg de bestemming per groep vast. `weg — gedekt` alleen met de naam van een bestaande Go-test.
- [ ] **Stap 2: Schrijf de falende tests**, bijvoorbeeld `TestLoadLimitsDefaultsWhenUnset`, `TestLoadLimitsInvalidHostTomlIsCouldNotCheck`, `TestIsCheckpointRemoteByNameAndURLAndSCP`, `TestBatchesSplitAtLimit` (64 MiB-grens, één blob groter dan de limiet staat alleen), `TestLedgerRoundTripAndFullIgnoresLedger`.
- [ ] **Stap 3: Draai:** `go test ./internal/checkpointscan/...` — verwacht FAIL.
- [ ] **Stap 4: Implementeer** de signaturen hierboven; port de logica regel voor regel uit `checkpoint_scan.py:116-165,303-460,723-806`. Regexes via `regexp` (RE2): controleer dat de Python-patronen er zonder lookaround in passen; `(?P<name>…)` is geldig in Go.
- [ ] **Stap 5: Draai:** `go test -race -shuffle=on ./internal/checkpointscan/...` — PASS; ook `GOOS=windows go build ./...` en `GOOS=darwin go build ./...` (CI bouwt beide, `ci.yml:174-176`). Testfixture-geheimen (PAT) bouw je tijdens de run uit gesplitste literals, zoals `tests/test_pre_push_secret_scan.py:37-40` (anders blokkeert gitleaks over de PR-range de eigen CI).
- [ ] **Stap 6: Commit** `feat(checkpointscan): limieten, remote-regels, ledger en batches (#362)`.

### Taak 3: `internal/checkpointscan` — scan en run-bounded

**Files:**
- Create: `internal/checkpointscan/bounded.go`, `scan.go` en `_test.go`

**Interfaces:**
- Consumes: Taak 2 (`Limits`, `Blob`, `Batches`, ledger, `CouldNotCheck`).
- Produces: `type Run struct{ ReturnCode int; PeakRSSKB *int; ... }` en `func RunBounded(argv []string, limits Limits, cwd string) (Run, error)` (systemd-run-wrapper uit `wrap`/`_probe_ceiling`, `_repair_runtime_dir`, `CEILING_RCS`, `ru_maxrss` via `syscall.Rusage`); `type Finding struct{...}`, `type ScanResult struct{...}`; `func MissingBlobs(repo string, refs map[string]string, remote string) (map[string]Blob, error)`; `func ScanBlobs(repo string, blobs map[string]Blob, opts ScanOptions) (ScanResult, error)` met `ScanOptions{Ledger string; StopOnCap, ReportRSS bool; Config string; Limits Limits; Stderr io.Writer}` (spiegelt `scan_blobs` `checkpoint_scan.py:608-616`: `stop_on_cap` en `report_rss` onafhankelijk; ledger-filtering `--full` blijft bij de aanroepers; `push` zet `StopOnCap=false` ook zonder `--report-rss`). `RSS`/runtime-dir/`wait4` staan in `bounded_unix.go` (`//go:build unix`, darwin geeft bytes → KB) en `bounded_other.go` (RSS `nil`, geen plafond-herkenning), naar het voorbeeld van `internal/deadline/group_unix.go`; geen `syscall.Rusage` in een bestand zonder build-tag. Het plafond-returncode volgt uit `syscall.WaitStatus`: signaled → `-signal` (Go's `ExitCode()` geeft -1 bij een signaal), zodat `CEILING_RCS = (137, -9)` blijft werken. De probe-uitkomst en reden zijn injecteerbaar/resetbaar (geen package-global `sync.Once` die `-shuffle=on` order-afhankelijk maakt). Gitleaks-aanroep, blobdir-methode (`BLOB_PREFIX = "\n"`), `SCAN_METHOD`-tekst en `FINDINGS_CAP` letterlijk als origineel.

- [ ] **Stap 1: Triage** het resterende deel van `tests/test_checkpoint_scan.py`; de gitleaks-afhankelijke tests blijven onder `LIGHTHOUSE_REQUIRE_GITLEAKS=1` (skip zonder gitleaks, hard falen in CI, zoals nu).
- [ ] **Stap 2: Schrijf de falende tests:** `TestScanBlobsFindsSecret` (fixture-blob met een geheim dat `.gitleaks.toml` blokkeert → `ScanResult` met bevinding), `TestScanBlobsCleanPasses`, `TestScanStopsAtFindingsCapUnlessKeepGoing`, `TestRunBoundedReportsCeilingKill` (child eindigt met 137 → plafond) en `TestRunBoundedReportsSignalKillAsCeiling` (child stuurt zichzelf SIGKILL → returncode -9 → plafond, niet `exited -1`), `TestPushStopsNotAtCapWithoutReportRSS`, `TestWrapWithoutSystemdRunReportsNoCeiling`.
- [ ] **Stap 3: Draai** — verwacht FAIL.
- [ ] **Stap 4: Implementeer** vanuit `checkpoint_scan.py:168-300,542-676`. `GOMAXPROCS` uit `cpu_quota` zoals `_gomaxprocs`.
- [ ] **Stap 5: Draai:** `LIGHTHOUSE_REQUIRE_GITLEAKS=1 go test -race -shuffle=on ./internal/checkpointscan/...` — PASS.
- [ ] **Stap 6: Commit** `feat(checkpointscan): scan en run-bounded (#362)`.

### Taak 4: het verborgen subcommando `lighthouse checkpoint-scan`

**Files:**
- Create: `internal/cli/checkpoint_scan.go`, `internal/cli/checkpoint_scan_test.go`, `internal/checkpointscan/push.go` (+ test)
- Modify: `internal/cli/root.go` (één `root.AddCommand(newCheckpointScanCommand())`, `root.go:70-106`)

**Interfaces:**
- Consumes: Taak 2 en 3.
- Produces: `func newCheckpointScanCommand() *cobra.Command` met `Hidden: true` en subcommando's `scan`, `push`, `entire-remote`, `remote-exclusion`, `run-bounded` met dezelfde flags en argumenten als `checkpoint_scan.py:_main` (`--remote`, `--full`, `--keep-going`, `--config` default `<repo-root>/.gitleaks.toml`, `<ref>=<sha>…`; `push <remote> --full --report-rss`; `entire-remote --toplevel <dir> <remote> <url>`; `remote-exclusion <remote>`; `run-bounded -- <argv…>`). Alles na de eerste `--` is het commando, ongemoeid. `func Push(repo, remote string, opts PushOptions, out, errOut io.Writer) int` in `internal/checkpointscan/push.go` (`PUSH_CHUNK = 500`, held-bestand, `--report-rss`).
- Contract: een gebruiksfout van cobra (onbekende flag, ontbrekende vlag, verkeerd aantal argumenten) is ook exit 2 met een `COULD NOT CHECK`-regel, niet cobra's standaard 1 (`internal/cli/root.go` `execute()`; argparse gaf 2): `SetFlagErrorFunc` en `Args`-validators geven `exitError{2}`, met test. Elke fout, ook een onverwachte (panic inbegrepen), wordt `secret-scan: COULD NOT CHECK — <msg>` op stderr met exit 2 (de hook leest 1 als bevindingen).

- [ ] **Stap 1: Schrijf de falende tests** in `checkpoint_scan_test.go`: `TestCheckpointScanIsHiddenFromHelp` (root `--help` bevat `checkpoint-scan` niet; `checkpoint-scan --help` wel), `TestEntireRemoteOutputMatchesPython` (zelfde stdout als het script op dezelfde invoer — leg de verwachte regels vast uit `checkpoint_scan.py:798-805`), `TestRemoteExclusionOutput`, `TestScanSecretFixtureExits1` (git-repo met een geheim in een `refs/entire/*`-commit en een lokale bare remote → exit 1), `TestScanCleanRefExits0`, `TestUnknownErrorIsCouldNotCheckExit2`, `TestRunBoundedNeedsCommandAfterDashes`.
- [ ] **Stap 2: Draai:** `go test ./internal/cli/ -run CheckpointScan` — FAIL.
- [ ] **Stap 3: Implementeer** de command en `Push`; registreer in `root.go`.
- [ ] **Stap 4: Draai:** `go test -race -shuffle=on ./internal/...` — PASS. Draai `go run ./cmd/lighthouse init --upgrade`; verwacht `No changes.` (zo niet: commit de installed copy mee).
- [ ] **Stap 5: Pariteitscheck** (handmatig, in de PR-evidence, niet als blijvende test): draai het Python-script en de binary op dezelfde fixture (geheim-ref en schone ref) en vergelijk stdout en exitcode.
- [ ] **Stap 6: Commit** `feat(cli): verborgen lighthouse checkpoint-scan (#362)`.

### Taak 5: hook, CI, docs en installer omzetten, Python weg

**Files:**
- Modify: `tools/hooks/pre-push-secret-scan.sh:32-48,67,91-97,216,226,231`, `.github/workflows/ci.yml:101,110-112,134`, `docs/secret-scan.md:5,17,24,29,88,119,186,251,294,300`, `tests/test_pre_push_secret_scan.py:33,378,399,412,421,490,523`, `.github/cla-signatures.json` (`$comment`, `:2`)
- Delete: `tools/hooks/checkpoint_scan.py`, `tests/test_checkpoint_scan.py`
- Create: `changelog.d/issue-362-check-cla-checkpoint-scan-go.md`

- [ ] **Stap 1: Hook.** Vervang `CHECKPOINT_SCAN="$SCRIPT_DIR/checkpoint_scan.py"` door een aanroep van `lighthouse checkpoint-scan` (`command -v lighthouse` als voorcontrole met `die "COULD NOT CHECK — lighthouse is not on PATH; …"`, plus een probe `lighthouse checkpoint-scan --help >/dev/null 2>&1` met `die "COULD NOT CHECK — this lighthouse has no checkpoint-scan; install a build from main or the next release"`: de geïnstalleerde v0.19.0 kent het commando niet en de oude foutmelding verwees naar `.entire/settings*.json`); `python3 -I "$CHECKPOINT_SCAN" <sub> …` wordt `lighthouse checkpoint-scan <sub> …` op de vier plekken; de `python3`-voorcontrole (regel 91) vervalt. De beslislogica (refclassificatie, `block`) blijft ongemoeid.
- [ ] **Stap 2: Installer.** In het blok van `install.sh`: `tools/hooks/install.sh` bevat geen `python3`-pad (`:28-49` zoekt alleen `pre-push-secret-scan.sh`) en blijft ongewijzigd; controleer dat en voeg niets toe. Weigermelding blijft "refusing to push unscanned".
- [ ] **Stap 3: Test aanpassen.** `tests/test_pre_push_secret_scan.py` blijft bestaan als black-box van de hook (registratie en samenwerking met git, spec §3): `MODULE` weg (ook de kopie op `:523`), de drie tool-bin-PATHs op `:399`, `:412`, `:421` krijgen een link naar de gebouwde `lighthouse` in plaats van `python3` (assertions ongewijzigd), de stderr-assertie op `"checkpoint_scan.py push ckpt"` wordt `"checkpoint-scan push ckpt"`, de test voor een checkout zonder `tools/hooks/` blijft. Triage rest in de PR-tabel; dit bestand is het onderwerp van een latere triage-issue (hooks als black box) en wordt hier alleen minimaal omgezet.
- [ ] **Stap 4: CI.** `ci.yml` secret-scan-job: commentaren op regel 101 en 134 noemen `lighthouse checkpoint-scan`; de pytest-regel verliest `tests/test_checkpoint_scan.py`; de build-stap van de binary blijft (hook-test heeft hem op PATH nodig). De job `go-test` heeft geen gitleaks en geen `LIGHTHOUSE_REQUIRE_GITLEAKS`, dus daar skippen de gitleaks-tests: voeg in de secret-scan-job, na gitleaks en `setup-go`, `LIGHTHOUSE_REQUIRE_GITLEAKS=1 go test -race ./internal/checkpointscan/... ./internal/cli/` zonder `-run`-filter (elke test die zonder gitleaks skipt, ook `TestScanSecretFixtureExits1` en `TestScanCleanRefExits0`, draait dan hard) toe (zoals de pytest nu met REQUIRE=1 draait).
- [ ] **Stap 5: Docs.** `docs/secret-scan.md` (Nederlands): alle verwijzingen naar `checkpoint_scan.py`/`python3`; de systemd-`ExecStart` op regel 186 wordt `ExecStart=%h/go/bin/lighthouse checkpoint-scan push --report-rss …` (pad zoals `docs/install.md:37`; de unit-PATH bevat `~/go/bin` niet); noem dat de host een `lighthouse`-binary mét `checkpoint-scan` nodig heeft (eerst releasen/upgraden, dan de hook en de unit op de host omzetten), geen Go-toolchain en geen python3.
- [ ] **Stap 6: Verwijderen.** `git rm tools/hooks/checkpoint_scan.py tests/test_checkpoint_scan.py`. Verifieer: `grep -rn 'checkpoint_scan' . -I --exclude-dir=.git | grep -v -e changelog.d -e CHANGELOG -e planning -e testdata` — leeg, behalve historische tekst die je bewust laat staan.
- [ ] **Stap 7: Changelog.** Fragment met frontmatter `type: internal` en één Nederlandse zin: `tools/check_cla` is Go (`go run ./tools/check_cla`) en `checkpoint_scan` is het verborgen subcommando `lighthouse checkpoint-scan`.
- [ ] **Stap 8: Verifieer.** `go vet ./...`, `golangci-lint run`, `go test -race -shuffle=on ./...`, `LIGHTHOUSE_REQUIRE_GITLEAKS=1 uv run pytest -q tests/test_pre_push_secret_scan.py` (binary op PATH), `go run ./cmd/lighthouse init --upgrade` → `No changes.`. Bewijst: zelfde oordeel en exitcode op de fixtures en de hook-samenwerking. Bewijst niet: de systemd-unit van de host, `go run` in `cla.yml` op een echte PR en een echte push via de geïnstalleerde `.git/hooks`; noem dat in de PR-body.
- [ ] **Stap 9: Commit** `feat(tools): hook, CI en docs op lighthouse checkpoint-scan (#362)`; push en PR via `/lighthouse-write-pr` met de triagetabel van spec §1 (bestand, aantal tests, bestemming, reden, bij *gedekt* de Go-test) over `test_check_cla.py` (62), `test_checkpoint_scan.py` (69) en de omzetting van `test_pre_push_secret_scan.py`.

## Zelfreview

- Spec §4 gedekt: `check_cla` als `main`-pakket (Taak 1), uitzondering `checkpoint_scan` als verborgen subcommando (Taak 4), tests mee na triage (Taak 1–3, 5).
- AC-dekking: `cla.yml` + weg (T1), verborgen subcommando met pariteit (T3/T4), hook/ci/docs/systemd (T5), triagetabel (PR-body), changelog-fragment `internal` (T5).
- Open punt voor de uitvoerder: of `host.toml`-ontbreken in-process dezelfde defaults geeft als `lighthouse config` met rc 1 (zie `config.HostValue`); test `TestLoadLimitsDefaultsWhenUnset` pint dat.
- Buiten scope: `tools/gen_code_language_freq.py`, `ci/domains.py`, `pack/python`, `.gitleaks.toml`, de beslislogica van de hook.
