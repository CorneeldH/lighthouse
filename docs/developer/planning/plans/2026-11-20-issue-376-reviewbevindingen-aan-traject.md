# Reviewbevindingen aan het traject koppelen — implementatieplan

> **Voor agentic workers:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkbox-syntax (`- [ ]`).

**Doel:** Elk reviewonderdeel (specreview, planreview, taakreview uit de SDD-ledger) schrijft zijn bevindingen aan het traject in `lighthouse.db`, met verwijzing en inhoudshash; `plan-review-record` schrijft op elke machine.

**Architectuur:** De recordbestanden in de datamap blijven de bron. Een nieuwe indexer in `internal/review` leest ze, kiest per keten de nieuwste poging, vertaalt elke bevinding naar een `telemetry.FindingRecord` en schrijft ze met de bestaande idempotente `telemetry.RecordFinding` (tabel `findings` uit migratie 0010, geen nieuwe migratie). Het traject volgt uit een nieuw veld `trajectory` in het record (repo + issue). De SDD-ledger krijgt een eigen parser. Een nieuw subcommando `lighthouse review-index` draait de indexer; `plan-review-record write` roept hem na het schrijven aan.

**Tech Stack:** Go, SQLite via `internal/store`, `internal/telemetry`, cobra.

**Spec:** [`2026-11-16-zelflerende-lighthouse-design.md`](../specs/2026-11-16-zelflerende-lighthouse-design.md) (issue T3) en [`2026-11-19-telemetrie-gegevensmodel-design.md`](../specs/2026-11-19-telemetrie-gegevensmodel-design.md) (tabel `findings`, herkomst, idempotentie).

## Globale randvoorwaarden

- Geen tweede opslag: de database bewaart alleen pad, hash, componentnaam, sleutel, ernst en besluit; nooit de inhoud van een record of ledger (`internal/telemetry/record.go`, kop van het bestand).
- Besluiten in `findings.decision`: alleen `adopted`, `rejected`, `left_open` of leeg (`0010_telemetry.sql`).
- `source` voor reviewrecords is `plan-review-record`; voor de ledger `sdd-ledger`. `source_id` is deterministisch uit het bronrecord afgeleid.
- Code, commentaar en namen in het Engels (`code_language = "en"`); documenten, issues en commits in het Nederlands.
- Elke PR krijgt een fragment in `changelog.d/`. Een wijziging onder `pack/` draait ook `go run ./cmd/lighthouse init --upgrade` en commit de geïnstalleerde kopie.
- Testgate: `go test -race ./internal/review/... ./internal/cli/... ./internal/telemetry/... ./internal/store/...`.

## Ontwerpkeuzes (door dit plan gemaakt)

Het issue en de spec laten deze open; ze staan hier zodat de uitvoerder ze niet zelf hoeft te raden.

1. **Traject van een record.** Het record heeft nu geen repo of issue. `BuildRecord` krijgt `RecordInput.Trajectory *Trajectory{Repo string; Issue int}`; het record schrijft het als sleutel `trajectory` (`null` of object `{repo, issue}`) en `SchemaVersion` wordt 6. De CLI vult het zo: issue = `--issue` (issue is de spec, AC 6) als gegeven, anders het nummer uit de huidige branch (`^issue-(\d+)-`), anders `null`; repo = `forge.repo` uit `lighthouse.toml`, bij ontbreken `null`. Een record met `trajectory: null` of van schema 5 of lager wordt niet geïndexeerd en verschijnt in het rapport met reden (milestone: een koppeling die ontbreekt staat er met reden).
2. **Nieuwste poging per keten.** Per keten telt de poging met de grootste attempt-naam (lexicografisch, zoals `ReadMeta` ze schrijft), staging `.<attempt>.tmp/` meegeteld. Heeft die poging `record.json`, dan telt ze en worden de bevindingen van oudere pogingen uit de database verwijderd. Heeft ze alleen `attempt.json` (nog `.tmp`, of een mislukte `write`), dan telt de keten als niet geslaagd: er staan geen bevindingen van die keten in de database.
3. **Besluit-vertaling** (record → `findings.decision`): `accept`, `minor_yes` → `adopted`; `reject`, `minor_no` → `rejected`; `accepted_open`, `defer`, `spec` → `left_open`; geen besluit (alleen bij `aborted`) → leeg.
4. **Taakreview uit de ledger.** Onderdeelnaam `task_review`; een ledgerregel met `Ruling:` is één bevinding. Zonder ernst in de ledger blijft `severity` leeg. Besluit: regel `Task <N>: parked — … — Ruling: …` → `rejected` (de code blijft staan); elke andere `Ruling:`-regel → `left_open`. De ledger heeft geen vast formaat in deze repo (hij komt uit de externe superpowers-skill `subagent-driven-development`, `references/fix-loop.md`); de parser leest alleen regels met `Ruling:` en het getal na `Task `, en dat is wat de test pint. Een ledger wordt aan een traject gekoppeld via het planpad op de eerste regel (`# SDD ledger — plan: <pad>`, met `issue-(\d+)` in de bestandsnaam) of via `--issue`.
5. **Wanneer indexeren.** `plan-review-record write` indexeert zijn eigen record na het schrijven (een fout daarin is een waarschuwing, geen exitcode). `lighthouse review-index` loopt alle ketens in de datamap af (inhaalslag, en herkenning van een gewijzigd bestand) en leest ledgers via `--ledger <pad>` (herhaalbaar). De ledger verdwijnt aan het eind van de SDD-run (`references/final-review.md`), dus `review-index --ledger` moet vóór die opruiming draaien; automatisch aanroepen vanuit de skill valt buiten dit issue.

## Review Focus

- Een record waarvan `record.json` na indexering is aangepast (zelfde pad, andere inhoud): de hash verandert en de rij wordt bijgewerkt, niet verdubbeld.
- Een keten met twee pogingen waarvan de nieuwste alleen `.tmp` is: de bevindingen van de oudere poging verdwijnen uit de database.
- Een record van schema 5 (zonder `trajectory`): wordt overgeslagen met reden, geen paniek en geen halve rij.
- Een ledger met `Ruling:`-regels van twee taken, tweemaal geïndexeerd: precies twee rijen.
- `Reserve` en `write` op een machine zonder `host.toml`: beide schrijven (geen half werkende reservering).

---

### Taak 1: Hostbeperking van `plan-review-record` weghalen

**Bestanden:**
- Wijzig: `internal/review/record.go` (`Deps`, `DefaultDeps`, `HostRefusal`, `hostRefusalWith`, `Reserve`; import `config` verdwijnt als niets anders hem gebruikt)
- Wijzig: `internal/cli/planreviewrecord.go` (`runRecordWrite`, `Long`-tekst met exit 3)
- Wijzig: `internal/review/record_test.go` (`TestRecordHostRefusal*`, regels 39-78), `internal/cli/planreviewrecord_test.go` (cases `refuse` en `unknown`, regels 114-125)
- Wijzig: `docs/host-assumptions.md` (rij met `record.go:77-99`), de `SKILL.md`-bronnen in `pack/core/skills/review-plan/` en `review-spec/` (alle plekken die "alleen op de host" of exit 3 beschrijven), `docs/site/data/config.json:27` en de sitetests die `record.go` tellen (zie `docs/developer/planning/plans/2026-11-12-issue-256-host-check-sleutel.md:52`)
- Test: `internal/review/record_test.go`, `internal/cli/planreviewrecord_test.go`

**Interfaces:**
- Produceert: `review.Deps` zonder veld `HostRefusal`: `Deps{CodexVersion func() *string; Now func() time.Time}`; `Reserve(attemptDir string, deps Deps, stderr io.Writer)` en `plan-review-record write` schrijven altijd; exit 3 bestaat niet meer.

- [ ] **Stap 1: Schrijf de falende tests**
  - `TestReserveWritesWithoutHostToml` in `internal/review/record_test.go`: `LIGHTHOUSE_DATA` naar een tempdir, `HOME` en `XDG_CONFIG_HOME` leeg, geen `host.toml`; `Reserve` op een attempt-map; verwacht dat `<data>/plan-review/<chain>/.<attempt>.tmp/attempt.json` bestaat en dat stderr leeg is.
  - `TestRecordWriteWithoutHostToml` in `internal/cli/planreviewrecord_test.go`: dezelfde omgeving, `plan-review-record write` met een geldige attempt (hergebruik de fixture van de bestaande succesvolle case); verwacht exit 0 en `record.json` in de datamap.
- [ ] **Stap 2: Draai ze en zie ze falen**
  Run: `go test ./internal/review/ ./internal/cli/ -run 'TestReserveWritesWithoutHostToml|TestRecordWriteWithoutHostToml' -v`
  Verwacht: FAIL (refusal-bericht `…-REVIEW NOT RECORDED`, exit 3).
- [ ] **Stap 3: Haal de beperking weg**
  Verwijder `HostRefusal`, `hostRefusalWith`, het veld in `Deps` en beide aanroepen; werk `Long` van het commando bij (exit-codes 0, 2, 4). Verwijder de tests `TestRecordHostRefusal*` en de cases `refuse`/`unknown`.
- [ ] **Stap 4: Werk docs en skilltekst bij, draai dogfood**
  Pas `docs/host-assumptions.md` aan (rij weg of herschreven: `plan-review-record` heeft geen hostaanname meer, `host.controller` blijft voor `config host-check`). Pas de skillbronnen aan, draai `go run ./cmd/lighthouse init --upgrade` en daarna `uv run pytest -q tests/pack -k "review or skill"`; pas de Python-tests aan waar ze de oude tekst pinnen (`tests/pack/test_adversarial_review_sh.py`, `test_adversarial_review_skills.py`, `test_skill_contracts.py`).
- [ ] **Stap 5: Draai de gate**
  Run: `go test -race ./internal/review/... ./internal/cli/... && go run ./cmd/lighthouse init --upgrade`
  Verwacht: PASS en `No changes.` na de eerste upgrade-commit.
- [ ] **Stap 6: Commit**
  `git commit -m "feat(review): plan-review-record schrijft op elke machine (#376)"`

---

### Taak 2: Traject in het record (schema 6)

**Bestanden:**
- Wijzig: `internal/review/record.go` (`SchemaVersion = 6`, `RecordInput.Trajectory`, `BuildRecord`)
- Wijzig: `internal/cli/planreviewrecord.go` (`writeRecord`)
- Test: `internal/review/record_test.go`, `internal/cli/planreviewrecord_test.go`

**Interfaces:**
- Produceert: `type Trajectory struct { Repo string; Issue int }`; `RecordInput.Trajectory *Trajectory`; het record bevat `"trajectory": null | {"repo": string, "issue": number}` na `issue_spec`.
- Produceert: `func BranchIssue(branch string) (int, bool)` in `internal/review/record.go`: nummer uit `^issue-(\d+)-`, anders `false`.

- [ ] **Stap 1: Schrijf de falende tests**
  - `TestBranchIssue`: `issue-376-reviewbevindingen` → `376,true`; `fix-iets` → `0,false`; `issue-12` (geen slug) → `0,false`; `work-issue-5-x` → `0,false`.
  - `TestBuildRecordTrajectory`: met `Trajectory{"CorneeldH/lighthouse", 376}` bevat het record `trajectory.repo == "CorneeldH/lighthouse"` en `trajectory.issue == 376`; zonder `Trajectory` is de sleutel aanwezig en `nil`; `schema_version == 6`.
  - `TestRecordWriteTrajectoryFromIssueFlag` (cli): een planreview met `--issue 42` geeft `trajectory.issue == 42` ook op een branch zonder issuenummer (AC 6: het issue is de spec).
- [ ] **Stap 2: Draai en zie falen**
  Run: `go test ./internal/review/ ./internal/cli/ -run 'TestBranchIssue|TestBuildRecordTrajectory|TestRecordWriteTrajectoryFromIssueFlag' -v`
  Verwacht: FAIL (`BranchIssue` en `Trajectory` bestaan niet).
- [ ] **Stap 3: Implementeer**
  `BranchIssue` met een gecompileerde regexp. `BuildRecord` zet `trajectory` direct na `issue_spec`. In `writeRecord` bepaalt de CLI het traject volgens ontwerpkeuze 1: `f.issue` als `hasIssue`, anders `BranchIssue(git branch --show-current)` via `review.Git(repo, "branch", "--show-current")`; repo uit `config` (`forge.repo`, lees hoe `internal/cli/pushworkflows.go:198` of `boardstart.go:58` hem laadt); ontbreekt een van beide, dan `Trajectory == nil`.
- [ ] **Stap 4: Draai de tests**
  Run: `go test -race ./internal/review/... ./internal/cli/...`
  Verwacht: PASS (pas bestaande tests aan die `schema_version == 5` of de vaste sleutelvolgorde pinnen).
- [ ] **Stap 5: Commit**
  `git commit -m "feat(review): record draagt het traject, schema 6 (#376)"`

---

### Taak 3: Besluit-vertaling en indexer voor plan- en specrecords

**Bestanden:**
- Maak: `internal/review/index.go`
- Maak: `internal/review/index_test.go`
- Wijzig: `internal/telemetry/record.go` (twee hulpfuncties, zie Interfaces) en `internal/telemetry/record_test.go`

**Interfaces:**
- Consumeert: `telemetry.RecordFinding(ctx, db *sql.DB, f FindingRecord) (id int64, isNew bool, err error)` en `telemetry.Origin{Source, SourceID, RecordedAt}`; `review.Sum(data []byte) string`; `datadir.Dir`.
- Produceert in `internal/telemetry/record.go`:
  - `func FindingSum(ctx context.Context, db *sql.DB, source, sourceID string) (sum string, found bool, err error)`: de `record_sum` van die bevinding.
  - `func DeleteFindingsUnder(ctx context.Context, db *sql.DB, source, prefix, keep string) (int64, error)`: verwijdert rijen van `source` waarvan `source_id` met `prefix` begint en niet met `keep` (is `keep` leeg, dan alles onder `prefix`); geeft het aantal.
- Produceert in `internal/review/index.go`:
  - `func FindingDecision(recordDecision string) string` (ontwerpkeuze 3).
  - `type IndexResult struct { New, Updated, Unchanged, Removed int; Skipped []Skip }` en `type Skip struct { Path, Reason string }`.
  - `func IndexRecords(ctx context.Context, db *sql.DB, dataRoot string, now func() time.Time) (IndexResult, error)`: loopt `<dataRoot>/{plan,spec}-review/<chain>/` af volgens ontwerpkeuze 2 en schrijft per bevinding één `FindingRecord`: `Source "plan-review-record"`, `SourceID "<kind>/<chain>/<attempt>/<finding_id>"`, `Component kind+"_review"` (`plan_review`, `spec_review`), `FindingKey` = `finding_id`, `Severity`, `Decision` via `FindingDecision`, `RecordPath` = pad van `record.json`, `RecordSum` = `Sum` van de bytes van `record.json`. `Updated` telt rijen waarvan `FindingSum` verschilt van de nieuwe hash.

- [ ] **Stap 1: Schrijf de falende tests**
  Fixture-helper `writeChain(t, root, kind, chain, attempt string, record map[string]any)` die een map met `record.json` aanmaakt (of alleen `attempt.json` in `.<attempt>.tmp` als `record == nil`); een in-memory/tempdir-database via `store.Open` + migraties zoals `internal/telemetry/record_test.go` dat doet.
  - `TestFindingDecision`: tabel van alle zes plan-/specbesluiten plus `""` naar de vertaling uit ontwerpkeuze 3.
  - `TestIndexRecordsLinksFindings`: een planrecord met `trajectory {CorneeldH/lighthouse, 376}` en twee bevindingen (BLOCKER `accept`, MINOR `minor_no`) geeft 2 rijen `component = plan_review`, besluiten `adopted`/`rejected`, `record_sum` = sha256 van het bestand; `IndexResult.New == 2`.
  - `TestIndexRecordsDetectsChangedFile`: na de eerste run `record.json` herschrijven met een ander besluit; tweede run geeft `Updated == 2`, `New == 0`, nog steeds 2 rijen, `record_sum` veranderd (AC 2: een gewijzigd bestand wordt herkend); derde run zonder wijziging: `Unchanged == 2`.
  - `TestIndexRecordsNewestAttemptWins`: keten met poging `A1` (record, 2 bevindingen, al geïndexeerd) en nieuwere `A2` met alleen `.A2.tmp/attempt.json`; na `IndexRecords` staan er 0 rijen voor de keten en `Removed == 2` (AC 4). Variant: `A2` met record (1 bevinding) → 1 rij, `Removed == 2`.
  - `TestIndexRecordsSkipsWithoutTrajectory`: record met `trajectory: null` en een schema-5-record zonder de sleutel: geen rijen, twee `Skipped` met een reden die `trajectory` noemt.
  - `TestIndexRecordsSpecIsIssue`: planrecord met `issue_spec.issue == 42` en `trajectory.issue == 42` en zonder specbestanden wordt gekoppeld (AC 6).
  - Telemetry: `TestFindingSumAndDelete` (rij aanmaken, `FindingSum` geeft de hash en `found`; `DeleteFindingsUnder` met `keep` laat de behouden rij staan).
- [ ] **Stap 2: Draai en zie falen**
  Run: `go test ./internal/review/ ./internal/telemetry/ -run 'FindingDecision|IndexRecords|FindingSumAndDelete' -v`
  Verwacht: FAIL (functies bestaan niet).
- [ ] **Stap 3: Implementeer `FindingSum` en `DeleteFindingsUnder`** in `internal/telemetry/record.go`, in dezelfde stijl als de bestaande schrijvers (één query; geen transactie nodig voor de lees-functie, `inTx` voor het verwijderen).
- [ ] **Stap 4: Implementeer `FindingDecision` en `IndexRecords`**
  Kies de nieuwste poging per keten door de namen in de ketenmap te sorteren en de voorloop-`.` en `.tmp`-staart te strippen. Lees `record.json` met `orderedjson` (zoals `BuildRecord` het schrijft) of `encoding/json`; de bevindingen staan onder `findings[]` met `finding_id`, `severity`, `decision` (string of `null`). Roep per keten eerst `DeleteFindingsUnder(…, "<kind>/<chain>/", keep)` aan (`keep` = `"<kind>/<chain>/<attempt>/"` van de geslaagde poging, anders `""`), dan `RecordFinding` per bevinding.
- [ ] **Stap 5: Draai de tests met `-race`**
  Run: `go test -race ./internal/review/... ./internal/telemetry/...`
  Verwacht: PASS.
- [ ] **Stap 6: Mutatiecheck (lighthouse-test-standard)**
  Zet de vertaling van `reject` tijdelijk op `adopted`: `TestFindingDecision` en `TestIndexRecordsLinksFindings` moeten rood worden. Haal de `Removed`-logica tijdelijk weg: `TestIndexRecordsNewestAttemptWins` moet rood worden. Herstel.
- [ ] **Stap 7: Commit**
  `git commit -m "feat(review): indexeer plan- en specrecords als bevindingen van het traject (#376)"`

---

### Taak 4: Taakreview uit de SDD-ledger

**Bestanden:**
- Maak: `internal/review/ledger.go`
- Maak: `internal/review/ledger_test.go`
- Maak: `internal/review/testdata/sdd-ledger-ruling.md`

**Interfaces:**
- Consumeert: `telemetry.RecordFinding`, `telemetry.DeleteFindingsUnder`, `BranchIssue`, `Sum` (Taak 3 en 2).
- Produceert:
  - `type LedgerFinding struct { Task int; Text string; Decision string }`.
  - `func ParseLedger(data []byte) (planPath string, findings []LedgerFinding)`: `planPath` uit de eerste regel `# SDD ledger — plan: <pad>`; per regel met `Ruling:` één bevinding (ontwerpkeuze 4); `Text` is wat na `Ruling:` staat, getrimd.
  - `func IndexLedger(ctx context.Context, db *sql.DB, path string, tr *Trajectory, now func() time.Time) (IndexResult, error)`: `tr` is het door de aanroeper bepaalde traject (repo + issue); bij `nil` wordt het bestand overgeslagen met reden. Schrijft `Source "sdd-ledger"`, `SourceID "<planPath>#<Task>#<k>"` (`k` = volgnummer van de `Ruling:`-regel binnen die taak, vanaf 1), `Component "task_review"`, `FindingKey "task-<N>-ruling-<k>"`, `RecordPath` = `path`, `RecordSum` = `Sum` van het ledger.
  - `func LedgerIssue(planPath string) (int, bool)`: `issue-(\d+)` uit de bestandsnaam van het planpad.

- [ ] **Stap 1: Schrijf de falende tests**
  Fixture `testdata/sdd-ledger-ruling.md`:
  ```
  # SDD ledger — plan: docs/developer/planning/plans/2026-11-20-issue-376-x.md
  Task 1: done — abc1234
  Task 2: parked — naamgeving — Ruling: de code blijft staan, het patroon is elders gelijk
  Task 3: Ruling: finding over cache genegeerd — kost niets als het fout is
  Task 3: done — def5678
  ```
  - `TestParseLedger`: `planPath` komt uit de kopregel; twee bevindingen: `{Task 2, rejected}` en `{Task 3, left_open}`; de regels `done` leveren niets.
  - `TestLedgerIssue`: `…/2026-11-20-issue-376-x.md` → `376,true`; een pad zonder `issue-` → `0,false`.
  - `TestIndexLedgerRulings` (AC 5): met `Trajectory{"CorneeldH/lighthouse", 376}` komen er twee rijen `component = task_review`; tweede run: `Unchanged == 2`, nog twee rijen; ledger met een extra `Ruling:`-regel: `New == 1`, `Updated == 2` (de ledgerhash is voor alle rijen veranderd).
  - `TestIndexLedgerWithoutTrajectorySkips`: `tr == nil` → geen rijen, één `Skip` met reden.
- [ ] **Stap 2: Draai en zie falen**
  Run: `go test ./internal/review/ -run 'ParseLedger|LedgerIssue|IndexLedger' -v`
  Verwacht: FAIL.
- [ ] **Stap 3: Implementeer** de vier functies in `internal/review/ledger.go`; regexps `^Task (\d+):` voor het taaknummer, kopregel `^# SDD ledger — plan: (.+)$`.
- [ ] **Stap 4: Draai met `-race` en doe de mutatiecheck**
  Run: `go test -race ./internal/review/...`
  Verwacht: PASS. Mutatie: `parked` → `left_open` maakt `TestParseLedger` rood. Herstel.
- [ ] **Stap 5: Commit**
  `git commit -m "feat(review): taakreview uit de SDD-ledger als onderdeel met eigen bevindingen (#376)"`

---

### Taak 5: Subcommando `review-index` en aanroep na `write`

**Bestanden:**
- Maak: `internal/cli/reviewindex.go`, `internal/cli/reviewindex_test.go`
- Wijzig: `internal/cli/root.go` (registratie), `internal/cli/planreviewrecord.go` (`runRecordWrite`)
- Wijzig: `docs/` (de pagina die `plan-review-record` beschrijft: zoek met `grep -rln plan-review-record docs`) en `docs/site/data/config.json` indien de sitetests nieuwe commando's tellen
- Maak: `changelog.d/issue-376-reviewbevindingen-aan-traject.md`

**Interfaces:**
- Consumeert: `review.IndexRecords`, `review.IndexLedger`, `review.ParseLedger`, `review.LedgerIssue`, `review.BranchIssue`, `datadir.Dir`, `store.OpenShared` (zelfde pad naar `lighthouse.db` als `internal/cli/gather.go:60`).
- Produceert: `lighthouse review-index [--ledger <pad>]... [--issue <nr>]`. Stdout: één regel `indexed: new=<n> updated=<n> unchanged=<n> removed=<n>` en per overgeslagen item `skipped: <pad>: <reden>`. Exit 0 ook bij overgeslagen items; exit 2 bij verkeerd gebruik, exit 1 bij een database- of leesfout. Het traject van een ledger: `--issue` als gegeven, anders `LedgerIssue(planPath)`, repo uit `forge.repo`.

- [ ] **Stap 1: Schrijf de falende tests**
  - `TestReviewIndexIndexesDataRoot`: `LIGHTHOUSE_DATA` en de databasepad naar tempdirs; een geschreven planrecord met traject; `review-index` geeft `indexed: new=1 …`, exit 0; tweede aanroep `unchanged=1`.
  - `TestReviewIndexLedgerFlag`: `--ledger` naar de fixture uit Taak 4 plus `--issue 376`: stdout meldt `new=2`; zonder `--issue` en met een planpad zonder `issue-` komt `skipped: … reden` en exit 0.
  - `TestRecordWriteIndexes`: `plan-review-record write` (geldige attempt, traject bekend) laat daarna een rij in `findings` staan; faalt het indexeren (databasepad onschrijfbaar), dan blijft exit 0 en staat er een waarschuwing op stderr (het record zelf is het product).
- [ ] **Stap 2: Draai en zie falen**
  Run: `go test ./internal/cli/ -run 'TestReviewIndex|TestRecordWriteIndexes' -v`
  Verwacht: FAIL (commando bestaat niet).
- [ ] **Stap 3: Implementeer** `newReviewIndexCommand()` volgens het patroon van `internal/cli/gather.go` en registreer hem in `root.go`; roep in `runRecordWrite` na een geslaagde `writeRecord` `review.IndexRecords` aan met dezelfde databasepad-logica en print bij een fout één regel `REVIEW-INDEX FAILED: <fout>` naar stderr.
- [ ] **Stap 4: Documentatie en changelog**
  Beschrijf `review-index`, het veld `trajectory`, het gedrag bij `.tmp` en de ledger-beperking (draai vóór de SDD-opruiming) in de docs; schrijf het fragment (`type: added`, Engelse tekst zoals `changelog.d/issue-373-telemetrie-gegevensmodel.md`).
- [ ] **Stap 5: Volledige gate en dogfood**
  Run: `go test -race ./internal/review/... ./internal/cli/... ./internal/telemetry/... ./internal/store/... && go run ./cmd/lighthouse init --upgrade && golangci-lint run ./...`
  Verwacht: PASS en `No changes.`
- [ ] **Stap 6: Commit**
  `git commit -m "feat(cli): review-index koppelt reviewrecords en ledger aan het traject (#376)"`

---

## Dekking van de acceptatiecriteria

| AC | Taak | Test |
|---|---|---|
| `plan-review-record` schrijft op elke machine | 1 | `TestRecordWriteWithoutHostToml`, `TestReserveWritesWithoutHostToml` |
| Record met verwijzing en hash gekoppeld; gewijzigd bestand herkend | 2, 3 | `TestIndexRecordsLinksFindings`, `TestIndexRecordsDetectsChangedFile` |
| Bevindingen per onderdeel met ernst en besluit | 3, 4 | `TestFindingDecision`, `TestIndexLedgerRulings` |
| Nieuwste poging per keten, ook `.tmp` | 3 | `TestIndexRecordsNewestAttemptWins` |
| Taakreview uit ledger met `Ruling:` | 4 | `TestParseLedger`, `TestIndexLedgerRulings` |
| Issue is de spec, traject gekoppeld | 2, 3 | `TestRecordWriteTrajectoryFromIssueFlag`, `TestIndexRecordsSpecIsIssue` |

## Zelfreview

- Spec-dekking: de zes AC staan in de tabel hierboven; de DoD-punten (docs, tests) zitten in taak 1 en 5.
- Typeconsistentie: `Trajectory`, `IndexResult`, `Skip`, `FindingDecision`, `FindingSum`, `DeleteFindingsUnder` hebben één definitie (taak 2 en 3) en worden in taak 4 en 5 met dezelfde namen gebruikt.
- Open punten die het plan bewust niet oplost: het echte SDD-ledgerformaat is extern en niet in deze repo vastgelegd (alleen `Ruling:` en het getal na `Task ` worden gelezen); automatisch indexeren vóór de SDD-opruiming; bewaartermijn en gevoelige inhoud (milestone, open vraag). Een uitvoerder die een afwijkend ledgerformaat tegenkomt, past alleen de fixture en de regexps in `ledger.go` aan.
