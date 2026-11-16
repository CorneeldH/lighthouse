# Verslag per dag, week of maand — implementatieplan

> **Voor agents:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkboxen (`- [ ]`).

**Doel:** `lighthouse report day|week|month [--date JJJJ-MM-DD] [--repo <pad>]` maakt een markdown-verslag (gedaan en gaande) van één repo, naar stdout of naar een bestand.

**Architectuur:** vier getypte periode-queries op de `Forge`-interface (GitHub-implementatie, fake-server en forge-neutrale contractsuite erbij). Een nieuw pakket `internal/report` rekent de periode uit, bouwt het verslag uit een smalle bron-interface en rendert het per taal. `internal/cli/report.go` leest eerst `report.enabled`, laadt dan `repoconfig` en schrijft de sink.

**Tech stack:** Go, cobra, BurntSushi/toml, `net/http/httptest` (`internal/forge/forgetest`), `internal/forge/forgesuite`; Python-mirror `pack/core/lib/config.py` en de pariteitstests onder `tests/`.

**Spec:** [2026-11-16-issue-44-verslag-design.md](../specs/2026-11-16-issue-44-verslag-design.md). Issue #44.

## Globale randvoorwaarden

- Instructies in het Engels; dit plan, commits, de PR en het verslag zelf (`[project] language` is `nl`) in het Nederlands. De melding bij een uitgezet verslag is Engels (toolmelding).
- Go volgens `/lighthouse-go-standard`, tests volgens `/lighthouse-test-standard`.
- Geen nieuw `.py`-bestand; geen `gh`-aanroep naast `Forge`; geen repo-naam, labelnaam of pad als literal in de code.
- Geen stille lege lijst bij een forge-fout: fout en exit ≠ 0.
- Periodes in `time.Local`, halfopen `[since, until)`, grenzen uit `time.Date`, nooit `+24h`.
- Config: `report.enabled` bool, default `false`; `report.sink` `"stdout"` (default) of `"file"`; `report.dir` string, verplicht bij `file`, relatief aan de repo-root, niet absoluut en na `filepath.Clean` niet buiten de repo.
- Bestandspad: `<dir>/<periode>/<label>.md`, `<periode>` letterlijk `day|week|month`; label `2026-11-16`, `2026-W47`, `2026-11`; overschrijft; commit niets.
- Een wijziging onder `pack/` of `internal/` die `init` raakt: `go run ./cmd/lighthouse init --upgrade`, de tweede run print `No changes.`, installed kopie committen.
- Elke PR een fragment in `changelog.d/`.
- Atomic commits als Conventional Commits, Nederlands, eindigend op de attributieregel van de sessie.

## Review Focus

1. Een PR die precies op `until` (00:00) mergt staat in de volgende periode, niet in beide (search-bereik is inclusief; filter opnieuw) — test in Task 3.
2. Meer dan 1000 zoekresultaten of `incomplete_results` is een fout, geen afgekapte lijst — Task 3.
3. Eén bron faalt halverwege (bijv. `OpenPRsWithStatus`): geen half verslag, exit ≠ 0, niets naar het bestand geschreven — Task 7 en 9.
4. Een foute `--date` (`2026-13-40`, `morgen`) of een onbekende periode (`year`): usage-fout, exit 2, geen forge-call — Task 9.
5. Een titel met een newline, `]`, `|` of `#12` breekt de lijstregel of linkt verkeerd: Render maakt er één regel van — Task 8.
6. Een `lighthouse.toml` ontbreekt, of `[report]` ontbreekt: "uit"-melding bij `[report]` zonder `enabled`, een duidelijke fout zonder `lighthouse.toml` — Task 9.

---

### Task 1: Config-sleutels `report.*` (Go, mirror, site, fixtures)

**Files:**
- Modify: `internal/config/config.go` (schema ~r.110–133, defaults ~r.157–170, validatie na `review.roborev` ~r.623)
- Modify: `pack/core/lib/config.py` (SCHEMA ~r.133, DEFAULTS ~r.172, validatie ~r.459)
- Modify: `docs/site/data/config.json` (nieuwe tabel `report`, na `review`)
- Modify: `tests/test_site_config.py` (`SET`, `options`-verwachting ~r.480)
- Modify: `tests/pack/fixtures/config-schema.json`, `tests/pack/fixtures/config/defaults/expect.json`, `.../full/{lighthouse.toml,expect.json}`
- Create: `tests/pack/fixtures/config/report-sink-bad/`, `report-enabled-type/` (naar model van `roborev-bad`, `roborev-type`)
- Test: `internal/config/config_test.go`, `tests/pack/test_config.py`

**Interfaces:**
- Produces: sleutels `report.enabled` (kBool, `false`), `report.sink` (kStr, `"stdout"`), `report.dir` (kStr, geen default); foutmelding `key report.sink must be 'stdout' or 'file', not <repr>` in Go en Python identiek.

- [ ] **Step 1: Schrijf de falende tests.** Go (`config_test.go`): `TestReportDefaults` (`Default("report.enabled")==false`, `Default("report.sink")=="stdout"`, `report.dir` heeft geen default); `TestReportSinkRejectsOtherValues` (`sink = "wiki"` → fout met `key report.sink must be 'stdout' or 'file', not 'wiki'`); `TestReportEnabledMustBeBool` (`enabled = "true"` → `key report.enabled must be of type bool`). Python (`tests/pack/test_config.py`, blok `# --- [report]`): dezelfde drie, naar model van de `review.roborev`-tests r.1129–1146. Maak de fixtures `report-sink-bad` en `report-enabled-type` met `lighthouse.toml` en `expect.json` (rc 2, de exacte stderr van Go én Python).
- [ ] **Step 2: Run rood.** `go test ./internal/config/ -run Report` en `python -m pytest tests/pack/test_config.py -k report -q`. Verwacht: FAIL (sleutels onbekend).
- [ ] **Step 3: Implementeer** de drie sleutels in `schema` en `defaults` van `config.go` en de waardecontrole van `sink` direct na die van `review.roborev`; spiegel exact in `config.py`. Voeg de sleutels toe aan `tests/pack/fixtures/config-schema.json` (lijst en defaults) en `defaults/expect.json`; zet `[report]` met alle drie in `full/lighthouse.toml` en `full/expect.json`.
- [ ] **Step 4: Site-config.** Voeg tabel `report` toe aan `docs/site/data/config.json` met drie sleutels (`level: "set"`, `required: "no"`, `default`, `about`, `example`, `readers`: `internal/repoconfig/repoconfig.go` en `internal/cli/report.go`; `options: ["stdout","file"]` bij `sink`; `report.dir` `required: "with sink = file"`). Voeg `report.sink` toe aan `SET` en de `options`-verwachting in `tests/test_site_config.py`. De `readers`-paden moeten bestaan: in deze taak alleen `internal/repoconfig/repoconfig.go` (bestaat al); Task 9 voegt `internal/cli/report.go` toe aan de `readers` zodra dat bestand er is.
- [ ] **Step 5: Run groen + pariteit.** `go test ./internal/config/... ./tests/... 2>&1 | tail`; `python -m pytest tests/pack/test_config.py tests/test_site_config.py -q`. Verwacht: PASS.
- [ ] **Step 6: Dogfood + commit.** `go run ./cmd/lighthouse init --upgrade` (twee keer; tweede print `No changes.`), commit config, mirror, fixtures, site-data en de installed kopie: `feat(config): [report]-sleutels enabled, sink en dir (#44)`.

---

### Task 2: `repoconfig.Repo.Report` en `ReportErr`

**Files:**
- Modify: `internal/repoconfig/repoconfig.go` (`Repo`, `rawFile`, `Load`)
- Test: `internal/repoconfig/repoconfig_test.go`

**Interfaces:**
- Produces:
  ```go
  type Report struct{ Enabled bool; Sink, Dir string }
  // in Repo: Report Report; ReportErr error
  ```
  `Sink` is `"stdout"` als niet gezet. `Load` faalt nooit op `[report]`.

- [ ] **Step 1: Falende tests** (tabeltest `TestLoadReport`): geldig (`enabled=true, sink="file", dir="docs/verslagen"`) vult `Report`, `ReportErr == nil`; default zonder tabel: `Enabled=false, Sink="stdout"`; elk van deze geeft `Load` zonder fout én `ReportErr != nil` waarvan de tekst de sleutelnaam bevat: `enabled = "true"` (`report.enabled`), `sink = "wiki"` (`report.sink`), `sink = "file"` zonder `dir` (`report.dir`), `dir = "/abs"`, `dir = "../buiten"`, `dir = "a/../../b"` (`report.dir`), `dir = 5` (`report.dir`). Een tweede test: een repo met `enabled = "true"` laadt nog steeds met `Slug` gevuld (`Triageable`/`Dispatchable` blijven werken).
- [ ] **Step 2: Run rood.** `go test ./internal/repoconfig/ -run Report`. Verwacht: FAIL (veld ontbreekt).
- [ ] **Step 3: Implementeer.** `rawFile.Report toml.Primitive` met `toml:"report"` (en een `bool` via `md.IsDefined("report")` om "afwezig" te onderscheiden); `func reportFrom(prim toml.Primitive, md toml.MetaData) (Report, error)` die met `md.PrimitiveDecode` in een struct met `*bool`/`*string`/`any`-velden decodeert zodat een typefout `ReportErr` wordt, en daarna `sink` en `dir` controleert (`filepath.IsAbs`, `filepath.Clean` begint met `..`). `Load` zet `r.Report, r.ReportErr`.
- [ ] **Step 4: Run groen.** `go test ./internal/repoconfig/...`. Verwacht: PASS.
- [ ] **Step 5: Commit** `feat(repoconfig): [report] met eigen fout die Load niet breekt (#44)`.

---

### Task 3: Forge — `MergedPRs` en `ClosedIssues` (search)

**Files:**
- Create: `internal/forge/report.go` (types + GitHub-implementatie van `MergedPRs`, `ClosedIssues`, gedeelde `searchPeriod`)
- Modify: `internal/forge/forge.go` (types in `report.go`; twee methodes in `Forge`)
- Modify: `internal/forge/forgetest/server.go` (route `GET /search/issues`), `internal/forge/forgesuite/suite.go` (Fixture + cases), de Fixture-implementatie in `internal/forge/github_contract_test.go`
- Test: `internal/forge/report_test.go`

**Interfaces:**
- Produces:
  ```go
  type PRSummary struct { Number int; Title, URL string; Draft bool; Mergeable, Checks string; MergedAt time.Time }
  MergedPRs(ctx context.Context, since, until time.Time) ([]PRSummary, error)   // oplopend op MergedAt
  ClosedIssues(ctx context.Context, since, until time.Time) ([]Issue, error)    // oplopend op ClosedAt
  ```
  Fixture-methodes: `AddMergedPR(t, pr forge.PRSummary)`; `AddIssue` bestaat al en krijgt voor `ClosedIssues` een `StateReason`-veld op `forge.Issue` (`"completed"`, `"not_planned"`, `""`).

- [ ] **Step 1: Falende `httptest`-tests** (`report_test.go`, eigen server met opgenomen search-antwoorden in GitHubs vorm): `TestMergedPRsQuery` (gedecodeerde `q` is `repo:o/r is:pr is:merged merged:2026-11-16T00:00:00+01:00..2026-11-17T00:00:00+01:00`, geen letterlijke `+` in de ruwe query); `TestMergedPRsDropsResultAtUntil` (item met `closed_at`/`pull_request.merged_at` precies op `until` valt weg, item op `since` blijft); `TestMergedPRsPaginates` (twee pagina's via `Link`); `TestMergedPRsErrorsAbove1000` (`total_count: 1001` → fout met "1000"); `TestMergedPRsErrorsOnIncomplete` (`incomplete_results: true` → fout); `TestClosedIssuesSkipsNotPlanned` (`state_reason: "not_planned"` valt weg, `"completed"` en ontbrekend blijven); `TestClosedIssuesDropsResultAtUntil`.
- [ ] **Step 2: Falende contractcases** in `forgesuite`: `{"MergedPRs", mergedPRs}` (twee gemergde PR's in en één buiten de periode → twee, op `MergedAt` oplopend, `Title`/`URL`/`Number` gevuld, `Mergeable == ""`) en `{"ClosedIssues", closedIssues}` (een `completed`, een `not_planned`, een buiten de periode → alleen de eerste). Bouw `AddMergedPR` en de search-route in `forgetest` (leest `q`, filtert op `[since,until]` inclusief zoals GitHub, pagineert met `page()`).
- [ ] **Step 3: Run rood.** `go test ./internal/forge/... -run 'MergedPRs|ClosedIssues'`. Verwacht: FAIL (methodes ontbreken).
- [ ] **Step 4: Implementeer** `func (c *GitHub) searchPeriod(ctx, kind, qualifier string, since, until time.Time) ([]searchItem, error)`: bouwt `q` met `url.Values` (zo wordt `+` `%2B`), volgt `Link` via `nextPage`, faalt op `total_count > 1000` of `incomplete_results`; `MergedPRs` en `ClosedIssues` filteren daarna zelf op `[since, until)` met `MergedAt`/`ClosedAt` en sorteren. `state_reason` komt in `issueItem`/`Issue` (`StateReason string`).
- [ ] **Step 5: Run groen.** `go test ./internal/forge/...`. Verwacht: PASS, ook de bestaande suites.
- [ ] **Step 6: Commit** `feat(forge): MergedPRs en ClosedIssues per periode (#44)`.

---

### Task 4: Forge — `MilestonedIssues`

**Files:**
- Modify: `internal/forge/report.go` (type `MilestonedIssue`, methode), `internal/forge/forge.go`
- Modify: `internal/forge/forgetest/server.go` (query `milestone=*`, `since` op `GET .../issues`; route `GET .../issues/{n}/events`), `internal/forge/forgesuite/suite.go`, `github_contract_test.go`
- Test: `internal/forge/report_test.go`

**Interfaces:**
- Produces:
  ```go
  type MilestonedIssue struct { Issue Issue; Milestone Milestone; At time.Time }
  MilestonedIssues(ctx context.Context, since, until time.Time) ([]MilestonedIssue, error)
  ```
  Fixture: `AddMilestonedEvent(t, number int, milestoneTitle string, at time.Time)`. Resultaat gesorteerd op milestone-titel, dan nummer.

- [ ] **Step 1: Falende `httptest`-tests** met events in GitHubs echte vorm (`{"event":"milestoned","created_at":…,"milestone":{"title":"M3"}}`, zonder nummer): `TestMilestonedIssuesInPeriod` (event binnen de periode, titel gelijk aan de huidige milestone → telt, `Milestone.Number` komt uit het issue, `At` = event); `TestMilestonedIssuesEventOutsidePeriod` (event vóór `since` of op `until` → niet); `TestMilestonedIssuesNowInOtherMilestone` (event voor "M2", issue hangt nu in "M3" → niet); `TestMilestonedIssuesLastEventWins` (twee events in de periode → `At` is de laatste); `TestMilestonedIssuesSkipsPullRequests` (item met `pull_request`-sleutel in de lijst valt weg, geen events-call erover); `TestMilestonedIssuesPaginatesEvents`; `TestMilestonedIssuesCandidatesRequest` (query bevat `state=open`, `milestone=*`, `since=<since in RFC 3339>`).
- [ ] **Step 2: Contractcase** `{"MilestonedIssues", milestonedIssues}`: issue 1 in M3 met event in de periode, issue 2 in M3 met event erbuiten, issue 3 dicht met event in de periode → alleen issue 1.
- [ ] **Step 3: Run rood.** `go test ./internal/forge/... -run Milestoned`. Verwacht: FAIL.
- [ ] **Step 4: Implementeer** volgens spec §4 stappen 1–4 (kandidaten via issue-lijst, per kandidaat events; hergebruik de bestaande paginering). Een fout in één events-call is een fout van de methode.
- [ ] **Step 5: Run groen.** `go test ./internal/forge/...`. Verwacht: PASS.
- [ ] **Step 6: Commit** `feat(forge): MilestonedIssues via de events van elk kandidaat-issue (#44)`.

---

### Task 5: Forge — `OpenPRsWithStatus`

**Files:**
- Modify: `internal/forge/report.go`, `internal/forge/forge.go`
- Modify: `internal/forge/forgetest/server.go` (GraphQL-operatie `pullRequests`), `forgesuite/suite.go`, `github_contract_test.go`
- Test: `internal/forge/report_test.go`

**Interfaces:**
- Produces: `OpenPRsWithStatus(ctx context.Context) ([]PRSummary, error)` gesorteerd op nummer; `MergedAt` nul. Fixture: `AddOpenPRWithStatus(t, pr forge.PRSummary)` (naast het bestaande `AddOpenPR`).

- [ ] **Step 1: Falende tests.** `TestOpenPRsWithStatusFields` (GraphQL-antwoord met `isDraft`, `mergeable: "CONFLICTING"`, `statusCheckRollup.state: "FAILURE"` → velden in `PRSummary`; `statusCheckRollup: null` → `Checks == ""`); `TestOpenPRsWithStatusPaginates` (`pageInfo.hasNextPage` volgen, 101 PR's); `TestOpenPRsWithStatusGraphQLError` (200 met `errors` → fout, geen lege lijst). Contractcase `{"OpenPRsWithStatus", openPRsWithStatus}` met één draft, één conflicterende en één met rode checks.
- [ ] **Step 2: Run rood.** `go test ./internal/forge/... -run OpenPRsWithStatus`. Verwacht: FAIL.
- [ ] **Step 3: Implementeer** met `c.graphql` en een query naar het model van `prFields` in `board.go`; sorteer op `Number`.
- [ ] **Step 4: Run groen.** `go test ./internal/forge/... && go vet ./...`. Verwacht: PASS (alle `Forge`-implementaties compileren; fakes die `forge.Forge` embedden hoeven niets).
- [ ] **Step 5: Commit** `feat(forge): OpenPRsWithStatus met draft, mergeable en checks (#44)`.

---

### Task 6: `internal/report` — periodes

**Files:**
- Create: `internal/report/period.go`, `internal/report/period_test.go`

**Interfaces:**
- Produces:
  ```go
  type Kind string // "day", "week", "month"
  type Period struct { Kind Kind; Since, Until time.Time; Label string }
  func ParseKind(s string) (Kind, error)
  func NewPeriod(kind Kind, at time.Time) Period   // at in time.Local
  ```

- [ ] **Step 1: Falende tests** (zet `time.Local` via `t.Setenv("TZ", …)` of geef `at` een `Europe/Amsterdam`-locatie mee en bouw grenzen in `at.Location()`): `TestWeekAcrossISOYear` (30 dec 2024 → label `2025-W01`, `Since` maandag 30 dec 00:00); `TestDayLabelAndBounds` (`2026-11-16`); `TestMonthLabelLeapFebruary` (2028-02 → `Until` is 1 maart; 29 dagen); `TestDSTDayLengths` (30 maart 2025 en 26 oktober 2025 in `Europe/Amsterdam`: `Until.Sub(Since)` is 23u en 25u); `TestWeekAcrossDST`; `TestParseKindRejectsOther` (`year` → fout die `day|week|month` noemt).
- [ ] **Step 2: Run rood.** `go test ./internal/report/`. Verwacht: FAIL (pakket ontbreekt).
- [ ] **Step 3: Implementeer** met `time.Date(..., at.Location())` en `time.ISOWeek`; nooit `Add(24*time.Hour)`.
- [ ] **Step 4: Run groen.** `go test ./internal/report/`. Verwacht: PASS.
- [ ] **Step 5: Commit** `feat(report): periode day, week en month in lokale tijd (#44)`.

---

### Task 7: `internal/report` — `Build`

**Files:**
- Create: `internal/report/build.go`, `internal/report/build_test.go`

**Interfaces:**
- Consumes: Task 6 (`Period`), Task 3–5 (`forge.PRSummary`, `forge.Issue`, `forge.MilestonedIssue`).
- Produces:
  ```go
  type Source interface { // de vier methodes van forge.Forge, niets anders
      MergedPRs(ctx, since, until time.Time) ([]forge.PRSummary, error)
      ClosedIssues(ctx, since, until time.Time) ([]forge.Issue, error)
      MilestonedIssues(ctx, since, until time.Time) ([]forge.MilestonedIssue, error)
      OpenPRsWithStatus(ctx) ([]forge.PRSummary, error)
  }
  type Report struct { Period Period; Repo string; At time.Time; Merged []forge.PRSummary; Closed []forge.Issue; OpenPRs []forge.PRSummary; Roadmap []RoadmapGroup }
  type RoadmapGroup struct { Milestone forge.Milestone; Issues []forge.Issue }
  func Build(ctx context.Context, src Source, repo string, period Period, now time.Time) (Report, error)
  func PRStatus(pr forge.PRSummary) Status // StatusDraft, StatusConflict, StatusRed, StatusBusy, StatusGreen, StatusNoChecks
  ```

- [ ] **Step 1: Falende tests** met een kleine fake `Source`: `TestBuildFillsEverySection`; `TestBuildEmptySections` (alles leeg → lege slices, geen fout); `TestBuildErrorFromAnySourceFailsBuild` (tabel over de vier bronnen: één faalt → `Build` geeft die fout en een lege `Report`); `TestBuildOrder` (gemerged op `MergedAt`, gesloten op `ClosedAt`, open PR's op nummer, mijlpalen op titel, issues op nummer; bron geeft door elkaar); `TestPRStatusFirstHitWins` (tabel: draft+conflict → draft; conflict+rood → conflict; `FAILURE`/`ERROR` → rood; `PENDING`/`EXPECTED` → bezig; `SUCCESS` → groen; `""` → geen checks); `TestBuildGroupsRoadmapByMilestone`.
- [ ] **Step 2: Run rood.** `go test ./internal/report/ -run Build`. Verwacht: FAIL.
- [ ] **Step 3: Implementeer** `Build` (de vier calls mogen sequentieel; eerste fout wint, `fmt.Errorf("report: <bron>: %w")`) en `PRStatus`. `At` is `now`.
- [ ] **Step 4: Run groen.** `go test ./internal/report/`. Verwacht: PASS.
- [ ] **Step 5: Commit** `feat(report): Build uit een smalle bron-interface (#44)`.

---

### Task 8: `internal/report` — `Render`

**Files:**
- Create: `internal/report/render.go`, `internal/report/render_test.go`, `internal/report/testdata/{nl,en}.golden.md`

**Interfaces:**
- Consumes: `Report`, `PRStatus` uit Task 7.
- Produces: `func Render(r Report, language string) string`. Teksten per taal in een `map[string]text` naar het patroon van `openText` in `internal/review/openpoints.go`; onbekende taal valt terug op `en`. Statuswoorden `nl`: `concept`, `conflict`, `rood`, `bezig`, `groen`, `geen checks`.

- [ ] **Step 1: Falende tests.** `TestRenderGoldenNL` en `TestRenderGoldenEN` (vul een `Report` zoals de schets in spec §5; vergelijk met `testdata/*.golden.md`; schrijf de golden met de hand uit de schets, niet door de output te kopiëren); `TestRenderUnknownLanguageFallsBackToEN` (`"fr"` geeft gelijk aan `en`); `TestRenderEmptySectionSaysGeen` (elke lege sectie heeft `Geen.`, ook de roadmap); `TestRenderTitleOnOneLine` (titel `"fix: a\nb] [c |d #12"` geeft één lijstregel waarin de link `[#N](url)` heel blijft); `TestRenderOpenPRsStampHeader` (de kop noemt het tijdstip `At` bij een periode in het verleden); `TestRenderWeekRangeInHeader` (`16–22 november 2026`).
- [ ] **Step 2: Run rood.** `go test ./internal/report/ -run Render`. Verwacht: FAIL.
- [ ] **Step 3: Implementeer** `Render`; collapse whitespace in titels tot één spatie en laat `[`/`]` in de titel staan buiten de linktekst (alleen het `#N` zit in de link).
- [ ] **Step 4: Run groen.** `go test ./internal/report/ && golangci-lint run ./internal/report/...`. Verwacht: PASS.
- [ ] **Step 5: Commit** `feat(report): Render in nl en en met "Geen." bij een lege sectie (#44)`.

---

### Task 9: `lighthouse report` — het commando

**Files:**
- Create: `internal/cli/report.go`, `internal/cli/report_test.go`
- Modify: `internal/cli/root.go` (`root.AddCommand(newReportCommand(defaultReportEnv()))` naast `worktree-audit`)

**Interfaces:**
- Consumes: `report.NewPeriod/ParseKind/Build/Render`, `repoconfig.Load`, `forgeOpener.forRepo`, `config.RepoRoot`, `config.Load`.
- Produces:
  ```go
  type reportEnv struct {
      Getenv  func(string) string
      Now     func() time.Time
      RepoRoot func() (string, error)
      GhToken func() string
      ForgeURL string
      Open    func(forge.Config) (forge.Forge, error) // nil: forge.Open
  }
  func defaultReportEnv() reportEnv
  func newReportCommand(env reportEnv) *cobra.Command // report day|week|month [--date JJJJ-MM-DD] [--repo <pad>]
  ```

- [ ] **Step 1: Falende tests** (injecteer klok, repo-root in `t.TempDir()`, fake forge die elke call telt; stdout/stderr/exitcode asserten): `TestReportDisabledPrintsOneLineAndExits0` (tabel: geen `[report]`, `enabled=false`, en een `lighthouse.toml` zonder `[forge]`: stderr is één Engelse regel die `[report] enabled = true` noemt, exit 0, 0 forge-calls, stdout leeg); `TestReportErrKinds` (typefout, `sink="wiki"`, `file` zonder `dir`, `dir` buiten de repo: exit ≠ 0, stderr noemt de sleutel, 0 forge-calls); `TestReportStdout` (aan, sink stdout: markdown op stdout, taal uit `[project] language`); `TestReportFileSink` (pad `<repo>/<dir>/week/2026-W47.md`, map aangemaakt, tweede run overschrijft; `day` → `.../day/2026-11-16.md`, `month` → `.../month/2026-11.md`); `TestReportDateFlag` (`--date 2025-12-30 week` → label `2025-W01`); `TestReportBadDateAndKind` (`--date 2026-13-40`, `--date morgen`, `year`, geen argument: exit 2, 0 forge-calls); `TestReportForgeErrorWritesNothing` (bron faalt: exit ≠ 0, stdout leeg, bestand niet aangemaakt); `TestReportRepoFlagAndRootOverride` (`--repo` wint van `RepoRoot`); `TestReportMissingToml` (geen `lighthouse.toml`: exit ≠ 0, duidelijke melding); `TestReportTokenFallsBackToGh` (geen `GH_TOKEN`, `GhToken` geeft een token → opent).
- [ ] **Step 2: Run rood.** `go test ./internal/cli/ -run Report`. Verwacht: FAIL.
- [ ] **Step 3: Implementeer.** Volgorde: periodeargument en `--date` (usage-fout via `usageErr`) → repo-root → `config.Load(<root>/lighthouse.toml)` en lees `report.enabled`; niet `true` → stderr-regel, `return nil` → `repoconfig.Load` → `Repo.ReportErr` → `forgeOpener.forRepo` (naar model `askOpener`) → `Build` → `Render` → sink. Bestand: schrijf pas na een geslaagde `Build`, via tijdelijk bestand en `os.Rename`. Een fout is `&exitError{code: 1, err: …}`.
- [ ] **Step 4: Run groen.** `go test ./internal/cli/ && go vet ./... && golangci-lint run ./...`. Verwacht: PASS. Controleer ook dat `docs/site/data/config.json` `readers` `internal/cli/report.go` kent (`python -m pytest tests/test_site_config.py -q`).
- [ ] **Step 5: Commit** `feat(cli): lighthouse report day|week|month (#44)`.

---

### Task 10: Documentatie, changelog en vervolgissues

**Files:**
- Create: `docs/report.md`, `changelog.d/issue-44-verslag.md`
- Modify: `docs/getting-started.md` (of de plek die de subcommando's opsomt; zoek met `grep -n "worktree-audit\|lighthouse ask" docs/*.md`)

**Interfaces:** geen.

- [ ] **Step 1: Schrijf `docs/report.md`** (Nederlands): doel, de drie `[report]`-sleutels met de defaults, de commando-aanroep, de periodes en labels, het bestandspad, wat "Gedaan" en "Gaande" tellen (`not_planned` niet; open PR's als momentopname; "Nieuw op de roadmap" alleen nog open issues), en de bekende rand (een hernoemde milestone matcht niet). Relatieve links alleen naar bestaande doelen; link vanuit de plek die andere subcommando's beschrijft.
- [ ] **Step 2: Fragment** `changelog.d/issue-44-verslag.md` met frontmatter `type: added` en één Engelse zin: `lighthouse report day|week|month makes a report of merged PRs, closed issues, open PRs and new roadmap issues, enabled per repo with [report] enabled = true.` (formaat volgens de bestaande fragmenten).
- [ ] **Step 3: Controleer** `go run ./cmd/lighthouse init --upgrade` (print `No changes.`), `go test ./...`, `python -m pytest tests -q`, en de `md-link-check` (geen fout bij het schrijven).
- [ ] **Step 4: Commit** `docs(report): lighthouse report en de [report]-sleutels (#44)`.
- [ ] **Step 5: Vervolgissues en #44-body.** Maak geen issue namens de gebruiker aan zonder instructie van het plan; noteer in de handoff dat §8 van de spec nog vraagt om: de body van #44 bijwerken (AC 2 naar "nieuw op de roadmap", week en maand erbij) en vervolgissues voor sinks `wiki`/`discussion`, over repo's heen, een timer en de UI, via `/lighthouse-write-issue`.

---

## Zelfreview

- **Spec-dekking:** §1 → Task 1–2; §2 → Task 9; §3 → Task 6; §4 → Task 3–5; §5 → Task 7–8; §6 → Task 6–9; §7 → tests in elke taak; §8 → Task 10. De keuze "aan de bouwer" (pakketnaam `internal/report`, `PRSummary` in `internal/forge/report.go`, GraphQL voor open PR's, `docs/report.md`) is vastgelegd.
- **Types:** `PRSummary`, `MilestonedIssue`, `Period`, `Report`, `Source` en `PRStatus` heten in elke taak hetzelfde. `Source` is een deel van `forge.Forge`, dus een `forge.Forge` voldoet er direct aan.
- **Evenredigheid:** één taak per laag; geen functielichamen, alleen handtekeningen, testnamen en de exacte waarden uit de spec.
