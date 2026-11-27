# Vergelijking 1: wat voegt de tweede reviewer toe? Implementatieplan

> **Voor agentic workers:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkbox-syntax (`- [ ]`).

**Doel:** de eerste explore/exploit-vergelijking uitvoeren: fase 1 leert uit bestaande gepaarde plan- en specreviewrecords, fase 2 zet de variant `plan_review=codex` in, en het issue sluit met een menselijk besluit.

**Architectuur:** één nieuw leespad `lighthouse compare-reviewers` (pakket `internal/review/compare`) selecteert de records van fase 1, telt per bron en kenmerkklasse en koppelt het herstel na merge uit de telemetrie. Fase 2 is configuratie (`[[experiment]]` en `[[schedule]]` in `lighthouse.toml`), geen nieuwe code. Alleen lezen: record-schema's, `review-index` en `measure-recovery` blijven zoals #572 ze opleverde.

**Tech Stack:** Go, cobra, SQLite (`lighthouse.db`), `lighthouse.toml`.

**Spec:** [`2026-11-24-issue-388-explore-exploit-design.md`](../specs/2026-11-24-issue-388-explore-exploit-design.md), secties *Protocol per vergelijking* en *Eerste vergelijking*. Voorganger: [plan #572](2026-11-26-issue-572-explore-exploit-indeling.md).

## Globale randvoorwaarden

- Fase 1 telt alleen gepaarde records met `review_mode: codex` en een gevulde `reviewer.codex_version`; per keten de nieuwste poging (zoals `newestAttempt` in `internal/review/index.go`); het aantal afgevallen records wordt genoemd.
- Ernst per bron uit `severity_by_source` (`codex`, `fallback`), nooit de samengevoegde ernst. Bronnen: `codex`, `fallback` (= Opus), `both`.
- Contrafeitelijk: alleen Codex = `codex` + `both` met Codex-ernst; alleen Opus = `fallback` + `both` met Opus-ernst.
- Hypothese fase 1: alleen Codex vangt minstens 90% van de overgenomen (`adopted`) BLOCKER- en MAJOR-bevindingen.
- Een kenmerkklasse met minder dan 10 trajecten per groep krijgt geen conclusie. Elke uitkomst noemt aantallen en onzekerheid.
- Budget fase 2: `share` ≤ 0.20; 20 variant-trajecten of 8 weken, tenzij de instroomschatting een langere duur vraagt (dan binnen 20%).
- Vroeg stoppen alleen als het herstel na merge in de variantgroep na minstens 10 trajecten twee keer zo hoog is.
- Fallbacks en niet-toegepaste indelingen (`assignment_applied: false`) worden apart genoemd.
- Geen automatische wijziging aan model, skill of harness; zo'n wijziging wordt een eigen issue.
- Artefacten Nederlands; instructiebestanden Engels. Elke PR krijgt een fragment in `changelog.d/`. Voor elke commit: `[test_gate] command` en `lighthouse imports check`.

## Review Focus

- Record zonder `reviewer.codex_version` (null) maar met `review_mode: codex`: valt af en telt in de afvalteller, crasht niet.
- Keten met meerdere pogingen waarvan de nieuwste alleen gestaged is (geen `record.json`): de keten telt niet, ook niet met een oudere poging.
- Record zonder herleidbaar traject of met meerdere mogelijke repo's: telt mee voor bevindingen, niet voor herstel.
- Groep met 0 bevindingen BLOCKER/MAJOR: de 90%-toets is niet gedefinieerd, geen deling door nul.
- Lege klasse of klasse met < 10 trajecten per groep: expliciet "geen conclusie", geen percentage.

---

## Bestanden

- Maak: `internal/review/compare/select.go`, `tally.go`, `join.go` (+ `_test.go` per bestand) — selectie, telling, koppeling met herstel.
- Maak: `internal/cli/comparereviewers.go` (+ test) — het subcommando; registreren waar `review-index` wordt geregistreerd.
- Wijzig: `lighthouse.toml` — `[[experiment]]` en `[[schedule]]` voor fase 2.
- Maak: `docs/developer/planning/results/2026-11-27-issue-573-fase-1.md` — vastgelegde uitkomst fase 1 (aantallen, onzekerheid, afgevallen records).
- Maak: `changelog.d/<fragment>` volgens `docs/releasing.md`.

## Taak 1: Selectie van records

**Bestanden:**
- Maak: `internal/review/compare/select.go`, `internal/review/compare/select_test.go`

**Interfaces:**
- Consumeert: `newestAttempt`-logica uit `internal/review/index.go` (exporteer een dunne helper in `internal/review` of dupliceer niet: maak `review.NewestRecordPath(chainDir string) (path string, ok bool, err error)` en laat `indexChain` die gebruiken).
- Produceert: `type Record struct { Kind, Chain, Path string; Findings []Finding; Paired bool; Assignment string; AssignmentApplied *bool; Repo string; Issue int; Linked bool }`, `type Finding struct { ID, Source, Severity, SevCodex, SevFallback, Decision string }`, `func Select(dataRoot string, repos []review.ServedRepo) (kept []Record, dropped Dropped, err error)`, `type Dropped struct { Fallback, NoCodexVersion, NotPaired, NotFinished, Unreadable int }`.

- [ ] **Stap 1: faalende test** `TestSelect_KeepsOnlyPairedCodexWithVersion`: fixture-datadir (in `t.TempDir()`) met vijf ketens — gepaard codex met versie, `review_mode: fallback`, codex zonder `codex_version`, `paired: false`, en één keten waarvan de nieuwste poging alleen `.<naam>.tmp` is. Verwacht: 1 record in `kept`; `Dropped{Fallback:1, NoCodexVersion:1, NotPaired:1, NotFinished:1}`.
- [ ] **Stap 2:** `go test ./internal/review/compare/ -run TestSelect -v` → FAIL (pakket ontbreekt).
- [ ] **Stap 3:** implementeer `Select` en de helper `review.NewestRecordPath`; herleiding traject via `review.Trace` met `ServedRepo`; zet `Linked=false` als niet eenduidig.
- [ ] **Stap 4:** test `TestSelect_NewestAttemptPerChain` (twee pogingen, alleen de nieuwste telt) en `TestSelect_AmbiguousRepoNotLinked`. Draai `go test ./internal/review/...` → PASS.
- [ ] **Stap 5: commit** `feat(review): selecteer gepaarde codex-records voor de eerste vergelijking (#573)`.

## Taak 2: Telling per bron, ernst en besluit

**Bestanden:**
- Maak: `internal/review/compare/tally.go`, `tally_test.go`

**Interfaces:**
- Consumeert: `[]Record` uit taak 1.
- Produceert: `type Counts struct { N int; BySource map[string]int; Adopted, Rejected, LeftOpen int }`, `func Tally(rs []Record) Result`, `type Result struct { Sources map[string]Counts; CodexOnlyBM, OpusOnlyBM Capture }`, `type Capture struct { Adopted, Caught int; Low, High float64 }` (Wilson 95%-interval), `func Wilson(k, n int) (low, high float64)`.

- [ ] **Stap 1: faalende tests**: `TestTally_CounterfactualUsesOwnSeverity` (een `both`-bevinding met Codex-ernst MINOR en Opus-ernst MAJOR telt als MINOR bij Codex-alleen en als MAJOR bij Opus-alleen); `TestTally_NoAdoptedBlockerMajor_NoDivision` (n=0 → `Capture{}` met `Defined=false`, geen NaN); `TestWilson_KnownValues` (k=9, n=10 → ongeveer 0.60–0.98).
- [ ] **Stap 2:** draai → FAIL.
- [ ] **Stap 3:** implementeer `Tally`, `Capture.Defined bool`, `Wilson`. Besluit via `review.FindingDecision` op het recordbesluit; ernst uit `SevCodex` respectievelijk `SevFallback`.
- [ ] **Stap 4:** draai → PASS.
- [ ] **Stap 5: commit** `feat(review): tel bevindingen per bron met eigen ernst en onzekerheid (#573)`.

## Taak 3: Kenmerkklassen en herstel na merge

**Bestanden:**
- Maak: `internal/review/compare/join.go`, `join_test.go`

**Interfaces:**
- Consumeert: `Record` (taak 1), `Tally` (taak 2), de trajecten met `Recoveries`, route en plangrootte uit `internal/telemetry/report` (gebruik de bestaande loader die `components.go` voedt; lees `Trajectory.Closed`, `Recoveries`, `PlanTasks`, route en gebied).
- Produceert: `type Class struct { Key, Value string; Trajectories int; Result Result; Recovered, Open int; Conclusion bool }`, `func ByClass(rs []Record, ts []*report.Trajectory) []Class` met klassen `route`, `plangrootte`, `gebied`, `repo`.

- [ ] **Stap 1: faalende tests**: `TestByClass_UnderTenIsNoConclusion` (9 trajecten → `Conclusion=false`); `TestByClass_UnlinkedRecordCountsFindingsNotRecovery`; `TestByClass_RecoveryOnlyFromClosedMeasurements` (open meting telt als `Open`, niet als herstel).
- [ ] **Stap 2:** draai → FAIL.
- [ ] **Stap 3:** implementeer `ByClass`; "per groep" bij fase 1 betekent per bron-bepaald traject (Codex-only-teller vs Opus-only-teller), `Conclusion` pas vanaf 10 trajecten in elke vergeleken groep.
- [ ] **Stap 4:** draai `go test ./internal/review/compare/` → PASS.
- [ ] **Stap 5: commit** `feat(review): koppel herstel na merge aan kenmerkklassen (#573)`.

## Taak 4: Subcommando `compare-reviewers`

**Bestanden:**
- Maak: `internal/cli/comparereviewers.go`, `internal/cli/comparereviewers_test.go`
- Wijzig: de registratie van `newReviewIndexCommand` in `internal/cli` (zelfde plek, naast `review-index`); `docs/` pagina van `review-index` (`docs/review-index.md`) met een verwijzing.

**Interfaces:**
- Consumeert: `compare.Select`, `compare.Tally`, `compare.ByClass`, `indexDataRoot`-patroon uit `internal/cli/reviewindex.go` (`datadir.Dir`, `servedRepos`, `store.OpenShared`).
- Produceert: `lighthouse compare-reviewers [--db <path>]`; uitvoer als markdown: kop met aantal geselecteerde en afgevallen records per reden (`Dropped`), tabel per bron (aantallen, besluit), 90%-toets met interval, tabel per kenmerkklasse met aantallen en "geen conclusie" waar van toepassing, apart blok "fallbacks en niet-toegepaste indelingen". Exit 0 gemeten | 1 lees- of databasefout | 2 verkeerd gebruik.

- [ ] **Stap 1: faalende test** `TestCompareReviewers_PrintsDroppedAndNoConclusion` met fixture-datadir en tijdelijke database: uitvoer bevat `afgevallen` met de reden-aantallen en `geen conclusie` voor een klasse onder 10.
- [ ] **Stap 2:** draai `go test ./internal/cli/ -run TestCompareReviewers` → FAIL.
- [ ] **Stap 3:** implementeer het commando; geen schrijfacties naar de database.
- [ ] **Stap 4:** draai `go test ./internal/cli/` en `lighthouse gate go -- go test -race` → PASS.
- [ ] **Stap 5:** `go run ./cmd/lighthouse init --upgrade` alleen als `pack/` of een `init`-uitvoer veranderde; fragment in `changelog.d/`. **Commit** `feat(cli): compare-reviewers voor de eerste vergelijking (#573)`.

## Taak 5: Fase 1 uitvoeren en vastleggen

**Bestanden:**
- Maak: `docs/developer/planning/results/2026-11-27-issue-573-fase-1.md`

Dit is meetwerk op de echte datadir; de stappen zijn commando's, geen code.

- [ ] **Stap 1:** bepaal het recordvenster: oudste `written_at` onder `<datadir>/{plan,spec}-review/*/*/record.json`; `DAYS` = dagen sinds die datum + 7.
- [ ] **Stap 2:** voor elke bediende repo (`lighthouse config` van de hostconfig; elke clone): `lighthouse measure-recovery --repo-dir <clone> --days $DAYS`. Leg per repo de uitvoerregel vast.
- [ ] **Stap 3:** `lighthouse review-index` en daarna `lighthouse compare-reviewers > uitvoer.md`.
- [ ] **Stap 4:** schrijf het resultaatdocument: aantallen per bron, 90%-toets met interval, klassen met aantallen, afgevallen records per reden, fallbacks en niet-toegepaste indelingen apart, en wat er niet uit te concluderen valt. Alleen aantallen en patronen, geen persoonsgegevens.
- [ ] **Stap 5: commit** `docs(planning): uitkomst fase 1 van vergelijking 1 (#573)`.

## Taak 6: Instroomschatting en fase 2 in `lighthouse.toml`

**Bestanden:**
- Wijzig: `lighthouse.toml` (naast de twee `[[schedule]]`-blokken op regel ~119)
- Wijzig: resultaatdocument uit taak 5 met de sectie *Instroom en duur*

**Interfaces:**
- Consumeert: velden van `repoconfig.Experiment` (`id`, `component`, `choice`, `share`, `sample`, `start`) en `[[schedule]]` met `job = "measure-recovery"` en `[schedule.params] days` (zie `docs/tick.md`, *Job `measure-recovery`*).

- [ ] **Stap 1:** schat de instroom: aantal trajecten met een planreview per week uit `runs` en de reviewrecords van de laatste 8 weken; trajecten per week × `share` = variant-trajecten per week. Leg `n_per_week`, de verwachte duur voor `sample = 20` en de keuze (8 weken of langer, binnen `share ≤ 0.20`) vast in het resultaatdocument.
- [ ] **Stap 2: faalende test** in `internal/repoconfig/experiment_test.go`: `TestLighthouseToml_ExperimentLoads` laadt de `lighthouse.toml` van de repo en verwacht één `Experiment{Component:"plan_review", Choice:"codex"}`.
- [ ] **Stap 3:** voeg toe, met `start` = de datum van de merge van de PR en `share`/`sample` uit stap 1 (`sample = 20`):

  ```toml
  [[experiment]]
  id = "plan-review-codex"
  component = "plan_review"
  choice = "codex"
  share = 0.20
  sample = 20
  start = "<YYYY-MM-DD>"

  [[schedule]]
  name = "measure-recovery"
  job = "measure-recovery"
  cron = "0 4 * * *"

  [schedule.params]
  days = 90
  ```

  `share` mag lager dan 0.20 als de schatting dat toelaat. Documenteer in een commentaar boven het blok dat het `[[schedule]]`-blok verdwijnt zodra fase 2 stopt, en dat er geen duurveld bestaat: de 8 weken (of de langere duur) staan in het resultaatdocument en gelden als stopmoment voor het besluit.
- [ ] **Stap 4:** draai `go test ./internal/repoconfig/... ./internal/dispatch/... ./internal/experiment/...` → PASS.
- [ ] **Stap 5: commit** `feat(experiment): zet variant plan_review=codex in met schema voor herstelmeting (#573)`.

## Taak 7: Fase 2 meten en besluit

Dit vindt plaats na afloop van de steekproef of de duur; het is menselijk werk.

- [ ] **Stap 1:** draai `lighthouse compare-reviewers` en het telemetrierapport (`internal/telemetry/report`, componentblok `plan_review`) over de variantgroep en de standaardgroep. Noem fallbacks en niet-toegepaste indelingen apart.
- [ ] **Stap 2:** controleer de stopregel: herstel na merge in de variantgroep ≥ 2× standaard na ≥ 10 trajecten → direct stoppen: verwijder het `[[experiment]]`-blok (commit `chore(experiment): stop plan_review=codex wegens herstel (#573)`).
- [ ] **Stap 3:** na afloop: verwijder het `[[schedule]]`-blok `measure-recovery` van deze fase en het `[[experiment]]`-blok in een commit.
- [ ] **Stap 4:** plaats de besluitcomment op #573: één van *behouden, aanpassen, breder inzetten, verder onderzoeken, stoppen*, met onderbouwing, aantallen en onzekerheid. Een voorgestelde wijziging aan model, skill of harness wordt een eigen issue (`/lighthouse-write-issue`). De mens neemt het besluit; sluit het issue via `/lighthouse-close-issue`.

## Zelfreview

- Spec-dekking: AC 1 → taak 1; AC 2 → taak 5 stap 2; AC 3 → taken 2–4; AC 4 → taak 6 stap 1; AC 5 → taak 6 stap 3; AC 6 → taak 4 en 7 stap 1; AC 7 → taak 7 stap 4. DoD: besluit (taak 7), herleidbaar (records en telemetrie, taak 5 en 7), geen automatische wijziging (taak 7 stap 4).
- Open punten voor de uitvoerder: de exacte loader in `internal/telemetry/report` voor taak 3 en de plek waar `review-index` wordt geregistreerd (taak 4) zijn niet in dit plan gelezen; lees ze vóór de eerste test.
- Bekende onzekerheid: de aantallen in de spec (150 gepaarde records, 9 Opus-tegen-Opus-paren) zijn nog niet tegen de records gecontroleerd; taak 5 doet dat.
