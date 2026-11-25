# Architectuurscan Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Een architectuurscan-skill in `pack/core` en een geplande Go-job `architecture-scan` die haar onbemand draait en het resultaat als één bijgewerkt beslisissue publiceert.

**Architecture:** De agent schrijft één markdownbestand (`architecture-scan-result.md`); `Check` valideert vorm, markeringen en nummers tegen de forge; pas in `Finish` publiceert Go het via een helper in `internal/scheduled` (nieuw issue, of comment met de oude body plus herschreven body). Een mislukte run sluit `failed` zonder issue of comment. Modelleer het package naar `internal/scheduled/reflect` (params, corpus) en `internal/scheduled/mapplanned` (forge-aanroepen, tests).

**Tech Stack:** Go (`internal/scheduled/archscan/`), `forgetest` (nep-GitHub) voor tests, markdown-skill in `pack/core/skills/`.

**Spec:** [`../specs/2026-11-24-architectuurscan-design.md`](../specs/2026-11-24-architectuurscan-design.md). Uitvoering wacht op de merge van #375 alleen voor het laatste criterium (tokenverbruik in de telemetrie); geen taak hieronder hangt ervan af.

## Global Constraints

- Geen nieuwe Python buiten `pack/python/`; de job is Go in `internal/scheduled/archscan/`.
- Code, namen, commentaar, tests (`internal/**`, `pack/**`, `tests/**`) zijn Engels; het issue en de docs zijn in `[project] language` (`nl`). Vaste koppen komen uit `headings.toml`, nooit als letterlijke tekst in Go.
- De skill is Engels, geïnstalleerd als `lighthouse-architecture-scan`; herkomst `mattpocock-skills` (MIT) blijft in de front matter en in `pack/SOURCES.toml`.
- `tools` default `Read Glob Grep Write`; `Configure` weigert elke tool buiten die lijst (nooit `Bash` of `Agent`). `max_runtime` default `60m`, `window` default `3 months`, `command` default `/lighthouse-architecture-scan`.
- Resultaatpad: `architecture-scan-result.md` in de root van de werkplek. Corpus: `.lighthouse/run/architecture-scan-corpus.json`.
- Marker op regel 1 van de body: `<!-- lighthouse: {"kind":"architecture-scan","version":1,"run":"…","base":"<sha>","first":"<datum>","scans":<n>} -->`; waarschuwingsregel op regel 2. Body hoogstens 60 000 tekens.
- Scanissue = label `needs_decision` + marker op regel 1 + auteur is de login van het token; meerdere open: het oudste.
- Elke claim eindigt op precies één van `[nagemeten: \`<commando>\` → <uitkomst>]`, `[niet nagemeten]`, `[niet meetbaar vanaf hier]`.
- Gate-fout: geen run-rij, tick exit 1 (zoals `map-planned`). Fout vanaf `Stage`: run sluit `failed`, geen issue of comment, tick exit 0.
- Elke `Finish`-route verwijdert het resultaatbestand, ook vroege uitgangen.
- Voor elke commit: `lighthouse imports check` naast `[test_gate] command`. Na een wijziging onder `pack/`: `go run ./cmd/lighthouse init --upgrade` en commit van de geïnstalleerde kopie.
- Conventional Commits, elke taak eindigt met een commit; nooit `--no-verify`.

## Review Focus

Invoer die de spec impliceert maar geen taak-test vanzelf raakt, meest waarschijnlijk eerst:

1. Resultaatbestand ontbreekt of is leeg bij een herhaalde `Postcheck` nadat `Finish` al publiceerde: moet goed zijn, geen tweede post (Task 5, test `TestCleanupMissingFileAfterPublish`).
2. Nul kandidaten en geen open scanissue: niets posten, geen fout (Task 5, `TestFinishNoCandidatesNoIssue`).
3. Scanissue van een andere auteur of zonder marker met label `needs_decision`: telt niet mee, nooit overschreven (Task 3, `TestFindScanIssueIgnoresForeign`).
4. Kandidaatnaam of claim met `[`, backticks of een markdown-kop erin: parser mag niet verspringen (Task 2, `TestParseResultClaimWithBracketsInCommand`).
5. Gebruiker vinkt een kandidaat af tussen `Check` en `Finish`: vinkje blijft behouden (Task 5, `TestFinishKeepsCheckboxSetAfterCheck`).

## File Structure

- `internal/scheduled/decisionissue.go` (+ `_test.go`) — nieuwe helper: marker, scanissue vinden, publiceren (create of update).
- `internal/scheduled/scheduled.go` — `Verdict.RunID string`.
- `internal/scheduled/archscan/params.go` — `config`, `defaults()`, `parse(params)`.
- `internal/scheduled/archscan/result.go` — `Result`, `Candidate`, `Claim`, `parseResult`, `render` (vinkjes overnemen).
- `internal/scheduled/archscan/corpus.go` — hotspots en corpus-JSON.
- `internal/scheduled/archscan/archscan.go` — `Job` met Gate/Launch/Stage/Cleanup/Allowed/Check/Finish.
- `pack/core/headings.toml` — nieuwe sleutels.
- `pack/core/skills/architecture-scan/SKILL.md`, `pack/SOURCES.toml`, `pack/core/levels.toml`.
- `internal/repoconfig/schedule.go`, `internal/cli/tick_scheduled.go` — registratie.
- `internal/packcontract/architecturescan_test.go` — skillcontract.
- Docs: `docs/tick.md`, `docs/install.md`, `changelog.d/issue-386-architectuurscan.md`, `lighthouse.toml`.

---

### Task 1: Registratie, params en headings

**Files:**
- Create: `internal/scheduled/archscan/params.go`, `internal/scheduled/archscan/archscan.go` (alleen `Job`, `Name`, `New`, `Configure`, `init`), `internal/scheduled/archscan/params_test.go`
- Modify: `internal/repoconfig/schedule.go:33` (scheduleJobs), `internal/repoconfig/schedule_test.go`, `internal/cli/tick_scheduled.go:19-21` (blank import), `pack/core/headings.toml`

**Interfaces:**
- Produces: `const Name = "architecture-scan"`; `type config struct{ command string; window string; labels []string; tools []string; maxRuntime time.Duration }`; `func parse(params map[string]any) (config, error)`; `type Job struct{ cfg config; bound bool }` met `Configure(s repoconfig.Schedule) (scheduled.Job, error)`.
- Headings (nl/en): `scan_candidates` (Kandidaten/Candidates), `scan_still_open` (Nog open uit eerdere scans/Still open from earlier scans), `scan_method` (Methode/Method), `scan_top` (Topaanbeveling/Top recommendation), `scan_unresolved` (Onbeslist sinds/Undecided since), `scan_rejected` (Afgewezen/Rejected), `scan_of` (Scan van/Scan of).

- [ ] **Step 1: Failing tests** in `params_test.go`: `TestParseDefaults` (lege params → `command == "/lighthouse-architecture-scan"`, `window == "3 months"`, `maxRuntime == 60*time.Minute`, tools `Read Glob Grep Write`), `TestParseRejectsBash` (`tools = ["Read","Bash"]` → fout met `Bash`), `TestParseRejectsUnknownKey`, `TestParseLabels` (`labels = ["2-area:core"]` → `[]string{"2-area:core"}`). In `schedule_test.go`: `TestScheduleAcceptsArchitectureScan` naar regel 115 (`job = "architecture-scan"` laadt). In `internal/cli` een test dat `scheduled.Lookup("architecture-scan")` iets teruggeeft (naast de bestaande jobtests).
- [ ] **Step 2:** `go test ./internal/scheduled/archscan/ ./internal/repoconfig/ -run 'Parse|ArchitectureScan'` → FAIL (package ontbreekt / job onbekend).
- [ ] **Step 3: Implement** `parse` naar `reflect/params.go` (onbekende sleutel is een fout, `scheduled.ShowPath` in meldingen; `tools` toegestaan uit exact `Read, Glob, Grep, Write`), `Job.Configure`, `init(){ scheduled.Register(New()) }`; voeg `"architecture-scan"` toe aan `scheduleJobs` en de blank import aan `tick_scheduled.go`. Voeg de zeven headings-sleutels toe aan `pack/core/headings.toml` (beide talen).
- [ ] **Step 4:** zelfde commando → PASS; `go test ./internal/config/ ./internal/cli/ -run Headings` → PASS (parity).
- [ ] **Step 5: Commit** `feat(scheduled): job architecture-scan geregistreerd met params en headings (#386)`.

### Task 2: Resultaatformaat — parser en vinkjes

**Files:**
- Create: `internal/scheduled/archscan/result.go`, `internal/scheduled/archscan/result_test.go`

**Interfaces:**
- Consumes: headings via `lighthouse headings`-equivalent in Go (`internal/config/headings.go`; gebruik de bestaande lezer die alle taalvarianten accepteert).
- Produces:
  - `type Claim struct{ Text, Mark, Command, Outcome string }` (`Mark` ∈ `nagemeten|niet-nagemeten|niet-meetbaar`)
  - `type Candidate struct{ Key, Name string; Rejected bool; Claims []Claim; Raw string }` (`Raw` = het hele `###`-blok)
  - `type Result struct{ Candidates []Candidate; StillOpen []int; Body string }`
  - `func parseResult(b []byte, h headings) (Result, error)` — fout bij ontbrekende verplichte kop (kandidaten, nog open, methode, topaanbeveling), claim zonder of met meer dan één markering, `nagemeten` zonder commando of uitkomst, leeg bestand, dubbele sleutel.
  - `func (r Result) WithCheckboxes(live map[string]bool) string` — render de body met `- [x] Afgewezen` voor elke sleutel in `live`.
  - `func RejectedKeys(body string) map[string]bool` — sleutels met `- [x]` uit een bestaande body.

- [ ] **Step 1: Failing tests**: `TestParseResultValid` (nl-voorbeeld met twee kandidaten, één nagemeten claim met commando en uitkomst → sleutels, markeringen), `TestParseResultZeroCandidates` (regel die dat zegt is geldig), `TestParseResultEmptyFile` (fout), `TestParseResultMissingHeading` (per verplichte kop), `TestParseResultClaimTwoMarks`, `TestParseResultClaimNoMark`, `TestParseResultNagemetenWithoutOutcome`, `TestParseResultClaimWithBracketsInCommand` (commando `rg "\[x\]" -c` mag de markering niet breken), `TestParseResultAcceptsEnglishHeadings`, `TestRejectedKeys`, `TestWithCheckboxesKeepsLiveTick`.
- [ ] **Step 2:** `go test ./internal/scheduled/archscan/ -run 'ParseResult|Rejected|WithCheckboxes'` → FAIL.
- [ ] **Step 3: Implement** regel-voor-regel parser (geen markdownbibliotheek); een claim is een bullet onder het kopje claims van een kandidaat; markering = laatste `[...]`-groep op de regel, geparsed vanaf het einde zodat haakjes in het commando niet storen.
- [ ] **Step 4:** zelfde commando → PASS.
- [ ] **Step 5: Commit** `feat(scheduled): resultaatformaat van de architectuurscan parsen (#386)`.

### Task 3: Helper voor beslisissue in `internal/scheduled`

**Files:**
- Create: `internal/scheduled/decisionissue.go`, `internal/scheduled/decisionissue_test.go`
- Modify: `internal/scheduled/scheduled.go` (`Verdict.RunID string`)

**Interfaces:**
- Produces:
  - `type Marker struct{ Kind string; Version int; Run, Base, First string; Scans int }` met `func (m Marker) String() string` (de HTML-commentaarregel) en `func ParseMarker(body string) (Marker, bool)` (alleen regel 1).
  - `func FindDecisionIssue(ctx context.Context, f forge.Forge, kind, label string, state string) ([]forge.Issue, error)` — issues met `label`, marker `kind` op regel 1, auteur == `f.Login`; oudste eerst. `state` is `open` of `closed`.
  - `type Publish struct{ Kind, Title, Label string; ExtraLabels []string; Warning func(first string, scans int) string; Run, Base string; Now time.Time; Body string; MaxBody int; Merge func(live string) string; CommentTitle string }`
  - `func PublishDecisionIssue(ctx context.Context, f forge.Forge, p Publish) (forge.Issue, error)` — geen open scanissue: `CreateIssue` met marker (`scans` 1); open issue met `Run == p.Run`: niets doen; anders: lees live body (`f.Issue`), pas `p.Merge(live)` toe, plaats `live` als comment (`p.CommentTitle`), dan `UpdateIssueBody` met `scans+1`, `first` ongewijzigd, waarschuwing op regel 2; fout als de body > `MaxBody` is.
- `Verdict.RunID`: gezet door de gate van een job; `Env.Now` bepaalt het id in `archscan`.

- [ ] **Step 1: Failing tests** met `forgetest`: `TestMarkerRoundTrip`, `TestParseMarkerOnlyLineOne`, `TestFindScanIssueIgnoresForeign` (andere auteur, geen marker, geen label → niet gevonden), `TestFindDecisionIssueOldestFirst`, `TestPublishCreatesIssueWithLabels`, `TestPublishSameRunPostsNothing`, `TestPublishUpdateCommentBeforeBody` (comment bevat de oude body, daarna body nieuw, `scans` 2, `first` gelijk, regel 2 = waarschuwing), `TestPublishMergeAppliedToLiveBody`, `TestPublishBodyTooLong` (> MaxBody → fout, niets gewijzigd), `TestPublishCommentFailsLeavesBody` (fout bij comment → body ongewijzigd).
- [ ] **Step 2:** `go test ./internal/scheduled/ -run 'Marker|DecisionIssue|Publish'` → FAIL.
- [ ] **Step 3: Implement** zoals boven; auteurscheck als `mapplanned` bij antwoorden; marker-JSON met `encoding/json`.
- [ ] **Step 4:** PASS.
- [ ] **Step 5: Commit** `feat(scheduled): helper om een beslisissue te vinden en bij te werken (#386)`.

### Task 4: Gate, hotspots en corpus

**Files:**
- Create: `internal/scheduled/archscan/corpus.go`, `internal/scheduled/archscan/corpus_test.go`
- Modify: `internal/scheduled/archscan/archscan.go` (Gate, Launch, Stage)

**Interfaces:**
- Consumes: `scheduled.FindDecisionIssue`, `scheduled.ParseMarker`, `Verdict.RunID`.
- Produces:
  - `func hotspots(ctx context.Context, env scheduled.Env, dir string, window string, paths []string) ([]Hotspot, error)` met `type Hotspot struct{ Path string; Changes int }` — gewijzigde bestanden per pad binnen `window` via `git log --since=… --name-only`, aflopend op `Changes`.
  - `type corpus struct{ Language string; SearchDirs, Paths []string; Hotspots []Hotspot; Base string; Issues []corpusIssue; Open *corpusScan; LastClosed *corpusScan }` met `corpusScan{ Number int; Body string; Comments []string }`.
  - `Gate`: fetch `origin/<default>`; geen forge → fout; open scanissue met `base` == HEAD van `origin/<default>` → `Verdict{Skip: "no commits since last scan"}`; anders `Verdict{RunID: "scan-" + env.Now.UTC().Format("20060102T1504")}`.
  - `Launch`: `Command = cfg.command + " --corpus " + corpusPath + " --out architecture-scan-result.md"`, `Instructions` in het patroon van `reflect.instructions` (geen vragen, alleen corpus lezen, precies één bestand op `--out`, geen issue/commit/push), `Tools = cfg.tools`, `MaxRuntime = cfg.maxRuntime`.
  - `Stage`: schrijft het corpus met `safefs.WriteFresh`; issuebodies afgekapt op `maxIssueBody = 2000` tekens, corpus in totaal op `maxCorpus = 400000` bytes (naar `max_corpus_bytes` van `reflect`).

- [ ] **Step 1: Failing tests** in een git-rig naar `mapplanned/job_test.go` (bare origin + forgetest): `TestGateSkipsWhenNoNewCommits`, `TestGateRunsWithNewCommits` (RunID niet leeg), `TestGateNoForgeIsError`, `TestGateForgeErrorIsError`, `TestHotspotsCountsPerPathWithinWindow` (commit buiten het venster telt niet), `TestStageCorpusHasOpenAndLastClosedScan`, `TestStageTruncatesIssueBodies`, `TestStageCorpusExcludesScanIssueFromIssueList`.
- [ ] **Step 2:** `go test ./internal/scheduled/archscan/ -run 'Gate|Hotspots|Stage'` → FAIL.
- [ ] **Step 3: Implement.** Gate-fouten geven `error` terug (geen `Skip`), zodat de tick de slot openhoudt.
- [ ] **Step 4:** PASS.
- [ ] **Step 5: Commit** `feat(scheduled): gate, hotspots en corpus van de architectuurscan (#386)`.

### Task 5: Cleanup, Allowed, Check en Finish

**Files:**
- Modify: `internal/scheduled/archscan/archscan.go`, `internal/scheduled/archscan/result.go`
- Create: `internal/scheduled/archscan/check_test.go`, `internal/scheduled/archscan/finish_test.go`

**Interfaces:**
- Consumes: `parseResult`, `Result.WithCheckboxes`, `RejectedKeys`, `scheduled.PublishDecisionIssue`, `scheduled.FindDecisionIssue`, `scheduled.ScanSecrets(dir string, files []string) []string`.
- Produces: `Cleanup`, `Allowed(path string) bool` (alleen `architecture-scan-result.md`), `Check`, `Finish` zoals in de spec.

- [ ] **Step 1: Failing tests (Cleanup/Allowed):** `TestCleanupMissingResultFails`, `TestCleanupEmptyResultFails`, `TestCleanupMissingFileAfterPublish` (open scanissue draagt het run-id → geen fout), `TestCleanupRemovesCorpus`, `TestAllowedOnlyResultFile`.
- [ ] **Step 2: Failing tests (Check)**, elk met "geen issue en geen comment aangemaakt" als tweede assert: `TestCheckMoreThanOneFile`, `TestCheckMissingHeading`, `TestCheckClaimMarks` (per fout), `TestCheckStillOpenNumberMissing`, `TestCheckStillOpenNumberClosed`, `TestCheckStillOpenIsTheScanIssue`, `TestCheckDroppedTickAgainstLiveBody` (corpus zegt `[ ]`, live forge zegt `[x]`, resultaat zonder vinkje → fout), `TestCheckDroppedTickWithoutOpenUsesLastClosed`, `TestCheckBodyOver60000`, `TestCheckSecretInResult` (ScanSecrets-treffer faalt).
- [ ] **Step 3: Failing tests (Finish):** `TestFinishCreatesIssue` (label `needs_decision`, `params.labels`, marker `scans` 1), `TestFinishUpdatesWithComment` (comment met oude body, `scans` 2, waarschuwing *Onbeslist sinds <datum>, 2 scans* op regel 2), `TestFinishRepeatedPostsOnce` (tweede aanroep: één issue, één comment), `TestFinishKeepsCheckboxSetAfterCheck`, `TestFinishNoCandidatesNoIssue`, `TestFinishOnlyRejectedCarriedOverPostsNothing`, `TestFinishAlwaysRemovesResultFile` (alle routes), `TestFinishForgeErrorFailsRunWithoutPartialPost`.
- [ ] **Step 4:** `go test ./internal/scheduled/archscan/` → FAIL.
- [ ] **Step 5: Implement.** `Check` leest nummers en live bodies van de forge (nooit uit het corpus); `ScanSecrets` expliciet op het resultaatbestand; `Finish` verwijdert het bestand in een `defer`. Comment eerst, body daarna (via de helper).
- [ ] **Step 6:** `go test -race ./internal/scheduled/...` → PASS.
- [ ] **Step 7: Commit** `feat(scheduled): check en publiceren van de architectuurscan (#386)`.

### Task 6: Levensloop-test (end-to-end)

**Files:**
- Create: `internal/scheduled/archscan/e2e_test.go`

- [ ] **Step 1:** Test `TestRunLifecycleViaPostcheck` naar `mapplanned/e2e_test.go`: nep-runner schrijft het resultaatbestand; `scheduled.Postcheck` met de echte job; assert `PostcheckResult` zonder commit of PR, nieuw issue aanwezig, resultaatbestand weg. Tweede test `TestPostcheckFailedCheckClosesFailedNoIssue`: ongeldig resultaat → `Failure` met reden, geen issue, geen comment.
- [ ] **Step 2:** `go test ./internal/scheduled/archscan/ -run Lifecycle` → verwacht PASS als Task 5 klopt; zo niet, fix in `archscan.go` (niet in de test) en herhaal.
- [ ] **Step 3: Commit** `test(scheduled): levensloop van de architectuurscan via Postcheck (#386)`.

### Task 7: De skill, packcontract en dogfood

**Files:**
- Create: `pack/core/skills/architecture-scan/SKILL.md`, `internal/packcontract/architecturescan_test.go`
- Modify: `pack/SOURCES.toml` (`[[file]] path = "pack/core/skills/architecture-scan/SKILL.md"`, `new = true`, herkomst `mattpocock-skills` MIT naar het patroon van andere ontleende bestanden), `pack/core/levels.toml` (`"skill:architecture-scan" = "off"` in `[light.components]`, `internal/cli/level_test.go:225` lijst), geïnstalleerde kopieën onder `.claude/` en `.agents/`

**Interfaces:**
- Consumes: de veldnamen en markeringen uit Task 2 en het corpus uit Task 4.

- [ ] **Step 1: Failing test** `TestArchitectureScanSkillContract` naar `packcontract/mapplanned_test.go`: front matter `name: lighthouse-architecture-scan`; tekst noemt `` `--corpus ``, `` `--out ``, `heading:scan_candidates`, `heading:scan_still_open`, `heading:scan_method`, `heading:scan_top`, `[nagemeten:`, `[niet nagemeten]`, `[niet meetbaar vanaf hier]`, `- [ ] Afgewezen`, `paths.search_dirs`, `/lighthouse-write-issue`, `Strong`, `Worth exploring`, `Speculative`; `TestArchitectureScanSkillNamesNoRepoPath` (geen `src/`, geen `paia`-pad). Beide varianten via `variants(t, …)`.
- [ ] **Step 2:** `go test ./internal/packcontract/ -run ArchitectureScan` → FAIL.
- [ ] **Step 3: Schrijf de skill** (Engels): vocabulaire, afbakening via recency, de vijf vragen en rangschikking uit paia's `paia-improve-codebase-architecture`; twee modi (interactief met `git log`, headless uit het corpus zonder `Bash`, precies één bestand op `--out`); eerdere-beslissingenregels uit de spec (onbeslist komt terug, vinkje alleen terug met *eerder afgewezen, opnieuw omdat …*, kandidaat onder open issue gaat naar *nog open*); het resultaatformaat letterlijk; kaartdocumenten uit `[paths]` alleen als ze bestaan.
- [ ] **Step 4:** `go test ./internal/packcontract/ ./internal/cli/ ./internal/packs/` → PASS.
- [ ] **Step 5:** `go run ./cmd/lighthouse init --upgrade`, daarna nogmaals → `No changes.`; `lighthouse imports check`.
- [ ] **Step 6: Commit** `feat(pack): skill architecture-scan uit paia naar pack/core (#386)` met de geïnstalleerde kopie.

### Task 8: Docs, schema en changelog

**Files:**
- Modify: `docs/tick.md` (sectie *Job `architecture-scan`* na `map-planned`, regel ~476 en ~650: ook de opsomming), `docs/install.md:534,550` (job-lijst en voorbeeldblok `cron = "0 6 1 1,4,7,10 *"`), `lighthouse.toml` (`[[schedule]] name = "architecture-scan"`, `job = "architecture-scan"`, `cron = "0 6 1 1,4,7,10 *"`, `params.labels = ["2-area:core"]`)
- Create: `changelog.d/issue-386-architectuurscan.md`

- [ ] **Step 1:** Schrijf de docs-sectie in het Nederlands: doel, params-tabel, corpus, resultaatformaat, scanissue en marker, gate, mislukken (twee soorten), telemetrie (#375). Relatieve links naar bestaande doelen.
- [ ] **Step 2:** Fragment met `type: feature` naar het formaat van de andere fragmenten in `changelog.d/`.
- [ ] **Step 3:** Zet het schemablok aan in `lighthouse.toml`; `go test ./internal/repoconfig/ ./internal/cli/` → PASS (blok laadt).
- [ ] **Step 4:** Volledige gate: `[test_gate] command` en `lighthouse imports check` → groen; `go run ./cmd/lighthouse init --upgrade` → `No changes.`
- [ ] **Step 5: Commit** `docs(scheduled): job architecture-scan beschreven en aangezet (#386)`.

## Self-Review

- **Spec-dekking:** skill (T7), schema en params (T1, T8), één open scanissue (T3, T5), markeringen (T2, T5), *nog open* (T2, T5, skill T7), afgewezen kandidaat blijft (T2, T5), mislukte run zonder issue (T5, T6), gate en run-id (T3, T4), Cleanup-uitzondering (T5), secretscan (T5), 60 000 tekens (T5), twee faalsoorten (T4, T6), docs en schema (T8). Telemetrie: bewust geen taak (wacht op #375).
- **Typeconsistentie:** `Result`/`Candidate`/`Claim` (T2) worden in T5 gebruikt; `Publish`, `Marker`, `FindDecisionIssue` (T3) in T4 en T5; `Verdict.RunID` (T3) in T4 en T5.
- **Open keuze voor de bouwer (spec, *Aan de bouwer*):** de vorm van de headings-sleutels is hier vastgelegd (T1); of `skill:architecture-scan` `off` krijgt in `light` volgt `map-planned` (T7).
