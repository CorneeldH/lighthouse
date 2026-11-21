# Herstel na merge per PR meten — implementatieplan

> **Voor agentic workers:** VEREIST SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkbox-syntaxis (`- [ ]`).

**Doel:** per gemergede PR het herstel binnen 7 dagen meten (fix-commits, reverts, bug-issues), de `fix-pr`-rondes vóór de merge apart tellen, en de uitkomst bij het traject in `lighthouse.db` bewaren.

**Architectuur:** een pure meetfunctie (`telemetry.MeasureRecovery`) werkt op een kleine `RecoverySource`-interface en een geïnjecteerde klok. Een lokale-git-implementatie levert merge-commit, bestanden en commits; een forge-adapter levert bug-issues. Een idempotente schrijver legt het resultaat vast in twee nieuwe tabellen (migratie 0011); een CLI-opdracht verbindt alles.

**Tech Stack:** Go, SQLite (`internal/store/migrations`), `git` via `os/exec`, `internal/forge`.

**Spec:** [`2026-11-16-zelflerende-lighthouse-design.md`](../specs/2026-11-16-zelflerende-lighthouse-design.md) (T4), [`2026-11-19-telemetrie-gegevensmodel-design.md`](../specs/2026-11-19-telemetrie-gegevensmodel-design.md) (herkomst, idempotentie, schrijffuncties, tests, documentatie).

## Gekozen ontwerpkeuzes

Dit plan beslist wat het issue openlaat:

- **Toegang: lokale git voor commits en bestanden, forge voor issues.** De `Forge`-interface kent geen commits of bestandslijsten; uitbreiden raakt GitHub, GitLab en alle fakes. De meting draait tegen een clone (`--repo-dir`). De squash-merge-commit is de commit op de default branch met `(#<pr>)` aan het eind van het onderwerp (de repo-conventie). Een PR zonder zo'n commit krijgt geen meting maar staat in `Skipped` met reden.
- **PR → traject via de branchnaam** `issue-<nr>-…` uit `Forge.PullHeads` (`PullHead.HeadRef`). Een PR zonder issuebranch staat in `Skipped` met reden `no_issue_branch`; niets telt stil niet mee.
- **Herstelcommit** = commit op de default branch na de merge-commit, tot en met merge + 7 dagen, waarvan het onderwerp met `fix:` of `fix(` begint (herstel) of met `Revert "`, `revert:` of `revert(` (revert), en die minstens één bestand raakt dat ook de merge-commit raakte. De merge-commit zelf telt niet.
- **Bug-issue** = issue met een label `bug` (hoofdletterongevoelig) waarvan titel of body `#<pr>` bevat, aangemaakt na de merge en binnen het venster. Gevonden met `Forge.SearchIssues("#<pr>")`; de filtering gebeurt in `MeasureRecovery`, de adapter mapt alleen.
- **`fix-pr`-rondes vóór de merge** komen uit `runs` (trigger `fixpr`, `trigger_key` `fixpr:<pr>:<sha>`, zie `internal/dispatch/decide.go:894`), gestart vóór de merge. Een rij met `outcome = 'start_failed'` telt niet mee (een mislukte start plus zijn herpoging is één ronde, zoals `fixPRRuns` in `internal/dispatch/decide.go`). Alleen dispatcher-runs; interactieve rondes hebben geen PR-record en staan als beperking in de documentatie.
- **Status:** `open` zolang `now < merged_at + 7d` (ook als er al herstel is: dan staan de items er al bij), daarna `recovered` (≥ 1 item) of `none`. Het venster is `(merged_at, merged_at + 7d]`.
- **Klok:** `now time.Time` als parameter; geen `time.Now()` in de meetlogica.
- **Eenmaal gesloten blijft gesloten:** een rij met status `recovered` of `none` wordt bij herhaling niet meer aangepast; een rij met `open` wordt vervangen door de nieuwe meting. Omdat niets de opdracht automatisch draait, ververst `measure-recovery` ook elke `open`-rij van de repo die buiten `--days` valt (met `merged_at` uit de rij), zodat een `open`-rij altijd sluit zodra zijn venster voorbij is.
- **Actuele bron:** de opdracht haalt eerst de default branch op (`git fetch origin <default>`) en meet op `origin/<default>`; een mislukte fetch is een fout (exit 1) en er wordt dan niets gesloten. Zo bevriest een verouderde clone nooit een onterecht `none`.
- **Forge-limiet:** PR's met een reeds gesloten rij gaan niet langs de forge. Bij een `RateLimited`-fout stopt de run met exit 1 nadat de eerder gemeten PR's zijn vastgelegd; een herhaling gaat verder bij de rest.

## Globale randvoorwaarden

- Geen tweede opslag: alles in `lighthouse.db` (milestone-No-Go).
- Geen Codex-, roborev- of kenn-forge-afhankelijkheid.
- Schrijvers zijn idempotent met `Origin` (bron `recovery`, `SourceID` `<repo>#<pr>`), in één transactie, zoals `internal/telemetry/record.go`.
- Code, commentaar en bestandsnamen Engels; documentatie en changelog volgen `[project] language`.
- Test-gate: `lighthouse gate go -- go test -race`; nooit `--no-verify`.

## Review Focus

- PR die dezelfde bestandsnaam raakt als een fix, maar een andere map: geen herstel (vergelijk volledige paden).
- Fix-commit precies op `merged_at + 7d`: telt mee; één seconde later niet.
- Revert van een commit die niet de PR is: telt alleen bij bestandsoverlap.
- Bug-issue dat vóór de merge naar de PR verwijst (bijv. de review-discussie): telt niet.
- Dezelfde PR twee keer gemeten, eerst `open`, dan na het venster: één rij, status vervangen; daarna `none` blijft `none`.

---

### Taak 1: Migratie 0011

**Bestanden:**
- Maak: `internal/store/migrations/0011_recovery.sql`
- Test: `internal/store/migrate_test.go` en `internal/cli/tick_test.go` (zoek eerst repo-breed op hardgecodeerde versienummers, `want 10` en `user_version`, zoals `tick_test.go:239-241`; pas alle aan)

**Interfaces:**
- Produceert tabellen:
  - `recoveries(id, source, source_id, recorded_at, trajectory_id NOT NULL REFERENCES trajectories(id), pr INTEGER NOT NULL, merged_at TEXT NOT NULL, window_end TEXT NOT NULL, status TEXT NOT NULL CHECK (status IN ('open','recovered','none')), fixpr_rounds INTEGER NOT NULL DEFAULT 0, UNIQUE (trajectory_id, pr), UNIQUE (source, source_id))`
  - `recovery_items(id, recovery_id NOT NULL REFERENCES recoveries(id) ON DELETE CASCADE, kind TEXT NOT NULL CHECK (kind IN ('fix','revert','bug_issue')), ref TEXT NOT NULL, at TEXT NOT NULL, UNIQUE (recovery_id, kind, ref))` — `ref` is een SHA of een issuenummer.
  - Index `recovery_items_recovery` op `recovery_items(recovery_id)`.

- [ ] **Stap 1: schrijf de falende test** `TestMigration0011Recovery` in `internal/store/migrate_test.go`: na `Open` bestaan beide tabellen; een `status`-waarde `maybe` wordt geweigerd; een tweede rij met dezelfde `(trajectory_id, pr)` wordt geweigerd; verwijderen van een `recoveries`-rij verwijdert zijn items (foreign keys aan).
- [ ] **Stap 2:** `go test -race ./internal/store/...` — verwacht FAIL (tabel ontbreekt).
- [ ] **Stap 3:** schrijf `0011_recovery.sql` met de schema's hierboven.
- [ ] **Stap 4:** `go test -race ./internal/store/... ./internal/cli/...` — verwacht PASS, ook de bestaande migratietests.
- [ ] **Stap 5: commit** `feat(store): migratie 0011 voor herstel na merge (#377)`.

### Taak 2: Pure meting

**Bestanden:**
- Maak: `internal/telemetry/recovery.go`
- Test: `internal/telemetry/recovery_test.go`

**Interfaces:**
- Produceert:

```go
// RecoveryWindow is the span after the merge in which repairs count.
const RecoveryWindow = 7 * 24 * time.Hour

type RecoveryStatus string // "open", "recovered", "none"

type Commit struct {
	SHA     string
	Subject string
	At      time.Time
	Files   []string
}

type BugIssue struct {
	Number    int
	Title     string
	Labels    []string
	CreatedAt time.Time
	Body      string
}

// RecoverySource is what MeasureRecovery needs from a repo and its forge.
type RecoverySource interface {
	// MergeCommit finds the squash commit of pr on the default branch.
	MergeCommit(ctx context.Context, pr int) (Commit, error)
	// CommitsAfter lists default-branch commits with At in (after, until], oldest first.
	CommitsAfter(ctx context.Context, after, until time.Time) ([]Commit, error)
	// IssuesMentioning lists issues whose title or body may mention #pr.
	IssuesMentioning(ctx context.Context, pr int) ([]BugIssue, error)
}

type RecoveryItem struct {
	Kind string // "fix", "revert" or "bug_issue"
	Ref  string
	At   time.Time
}

type Recovery struct {
	PR        int
	MergedAt  time.Time
	WindowEnd time.Time
	Status    RecoveryStatus
	Items     []RecoveryItem
}

var ErrNoMergeCommit = errors.New("telemetry: no merge commit found for the PR")

func MeasureRecovery(ctx context.Context, src RecoverySource, pr int, mergedAt, now time.Time) (Recovery, error)
```

- [ ] **Stap 1: schrijf de falende tests** met een `fakeSource` in `recovery_test.go`, tabelgedreven, elk met `mergedAt` vast en `now` per geval:
  - `fix(x): …` op een gedeeld pad, dag 2, `now` dag 10 → `recovered`, één item `fix` met die SHA.
  - zelfde commit, `now` dag 3 → `open` met het item erbij (niet `none`).
  - geen herstel, `now` dag 3 → `open`, nul items; `now` dag 8 → `none`.
  - fix op dezelfde bestandsnaam in een andere map → geen item.
  - `feat:`-commit op een gedeeld pad → geen item.
  - `Revert "feat: x"` op een gedeeld pad → item `revert`.
  - fix op precies `mergedAt + 7d` → item; op `+7d+1s` → geen item (de fake respecteert zijn argument niet; `MeasureRecovery` filtert zelf op het venster).
  - merge-commit zelf in `CommitsAfter` → geen item.
  - bug-issue met label `Bug`, body `#12`, na de merge → item `bug_issue` met ref `"<nummer>"`; zonder label `bug`, of vóór de merge aangemaakt, of met `#120` in de body (alleen `#12` als heel getal) → geen item.
  - `MergeCommit` geeft `ErrNoMergeCommit` → `MeasureRecovery` geeft die fout door (`errors.Is`).
- [ ] **Stap 2:** `go test -race ./internal/telemetry/ -run Recovery` — verwacht FAIL (niet gedefinieerd).
- [ ] **Stap 3:** implementeer de signaturen hierboven. Bestandsoverlap op volledige paden (set-doorsnede). Onderwerpclassificatie met een kleine hulpfunctie `classifySubject(s string) string` (`"fix"`, `"revert"` of `""`). Items gesorteerd op `At`, dan `Ref`.
- [ ] **Stap 4:** dezelfde test — verwacht PASS.
- [ ] **Stap 5: commit** `feat(telemetry): meting van herstel binnen 7 dagen (#377)`.

### Taak 3: Lokale git-bron

**Bestanden:**
- Maak: `internal/telemetry/gitsource.go`
- Test: `internal/telemetry/gitsource_test.go`

**Interfaces:**
- Verbruikt: `Commit`, `RecoverySource`, `ErrNoMergeCommit` uit taak 2.
- Produceert: `type GitSource struct { Dir, Branch string; Issues func(ctx context.Context, pr int) ([]BugIssue, error) }` met alle drie `RecoverySource`-methoden. `MergeCommit` en `CommitsAfter` draaien `git -C Dir log` op `Branch`; `IssuesMentioning` roept `Issues` aan (taak 5 vult die met de forge).

- [ ] **Stap 1: schrijf de falende test** met een nep-repo in `t.TempDir()`, gebouwd met echte `git` (`git init`, vaste `GIT_AUTHOR_DATE`/`GIT_COMMITTER_DATE` per commit, geen globale config nodig: zet `user.name`/`user.email` per commando). Geschiedenis: commit A `feat(a): maak a (#10)` raakt `a.txt`; commit B `fix(a): herstel a` raakt `a.txt` twee dagen later; commit C `feat(b): maak b (#11)` raakt `b.txt`, met datum A+8 dagen (zodat `CommitsAfter(A.At, A.At+7d)` C uitsluit). Asserts: `MergeCommit(10)` → SHA van A met `Files == ["a.txt"]`; `MergeCommit(99)` → `ErrNoMergeCommit`; `CommitsAfter(A.At, A.At+7d)` → `[B]` met `Files == ["a.txt"]`. Gebruik de bestaande git-helper in `internal/fixturesetup/fixturesetup.go` alleen als die zonder omweg herbruikbaar is; anders een eigen `runGit(t, dir, env, args...)` in de testfile.
- [ ] **Stap 2:** `go test -race ./internal/telemetry/ -run GitSource` — FAIL.
- [ ] **Stap 3:** implementeer. Gebruik `git log --format=%H%x00%s%x00%cI --name-only -z`-achtige uitvoer met een eigen veilige parser; de SHA-lijst blijft stabiel gesorteerd (oud → nieuw). `(#<pr>)` matcht alleen aan het eind van het onderwerp (`regexp` met `\(#<pr>\)$`).
- [ ] **Stap 4:** PASS.
- [ ] **Stap 5: end-to-end op de nep-repo** (zelfde testfile): `MeasureRecovery(ctx, gitSrc, 10, A.At, A.At+10d)` → `recovered` met precies de SHA van B; `MeasureRecovery(…, 11, C.At, C.At+10d)` → `none`. Dit is acceptatiecriterium 5 (een PR mét en een PR zonder fix op hetzelfde bestand).
- [ ] **Stap 6: commit** `feat(telemetry): git-bron voor herstelmeting met nep-repo-test (#377)`.

### Taak 4: Schrijver

**Bestanden:**
- Wijzig: `internal/telemetry/record.go` of maak `internal/telemetry/record_recovery.go`
- Test: `internal/telemetry/record_recovery_test.go` (stijl van `record_test.go`)

**Interfaces:**
- Verbruikt: `Recovery` (taak 2), `Origin`, `inTx`, `resolveRepo`, `ensureTrajectory` uit `record.go`.
- Produceert:

```go
type RecoveryRecord struct {
	Origin                 // Source "recovery", SourceID "<repo>#<pr>"
	Repo       string
	Issue      int
	Recovery
	FixPRRounds int
}

// RecordRecovery stores or refreshes the recovery of one PR. isNew is true for a first record.
// A row whose status is already recovered or none is left as it is.
func RecordRecovery(ctx context.Context, db *sql.DB, r RecoveryRecord) (id int64, isNew bool, err error)

// CountFixPRRounds counts dispatcher runs of trigger fixpr for pr that started before mergedAt.
func CountFixPRRounds(ctx context.Context, db *sql.DB, repo string, pr int, mergedAt time.Time) (int, error)
```

- [ ] **Stap 1: schrijf de falende tests**: (a) eerste aanroep maakt traject + rij + items en geeft `isNew == true`; (b) tweede aanroep met `open` en een extra item vervangt de items, `isNew == false`, één rij; (c) na `recovered`/`none` verandert een nieuwe aanroep niets (items en status gelijk); (d) `FixPRRounds` staat in de rij en telt niet mee in `recovery_items`; (e) `CountFixPRRounds` telt alleen runs met `trigger_key` `fixpr:<pr>:%` van dezelfde repo, gestart vóór `mergedAt` en niet `start_failed` (een run erna, voor een andere PR, of een `start_failed`-rij naast zijn herpoging telt niet dubbel); (f) ongeldige `Origin` of `Issue < 1` geeft een fout en laat geen traject achter (rollback).
- [ ] **Stap 2:** FAIL.
- [ ] **Stap 3:** implementeer met `inTx` en `insertOrFind` volgens het patroon van `RecordFinding` (`record.go:423-462`). `window_end` = `MergedAt + RecoveryWindow`.
- [ ] **Stap 4:** `go test -race ./internal/telemetry/... ./internal/store/...` — PASS.
- [ ] **Stap 5: commit** `feat(telemetry): idempotente schrijver voor herstel na merge (#377)`.

### Taak 5: CLI, forge-adapter, documentatie

**Bestanden:**
- Maak: `internal/telemetry/forgeissues.go` (adapter `func ForgeIssues(f forge.Forge) func(ctx context.Context, pr int) ([]BugIssue, error)`: `f.SearchIssues(ctx, "#<pr>")`, mapt naar `BugIssue`)
- Maak: een opdracht `lighthouse measure-recovery` in `internal/cli/` volgens het patroon van `internal/cli/report.go` (lees dat bestand eerst; registreer zoals de andere opdrachten)
- Wijzig: `docs/install.md` (sectie `telemetry-data-model`: tabellen `recoveries` en `recovery_items`, de definitie van herstel, het venster, de status `open`, de beperking dat alleen dispatcher-`fix-pr`-rondes tellen), `docs/tick.md` alleen als de opdracht daar thuishoort
- Maak: `changelog.d/issue-377-herstel-na-merge.md` met front matter `type: added`
- Test: `internal/cli/measure_recovery_test.go`, `internal/telemetry/forgeissues_test.go`

**Interfaces:**
- Verbruikt: `GitSource`, `MeasureRecovery`, `RecordRecovery`, `CountFixPRRounds`, `Forge.MergedPRs(since, until)`, `Forge.PullHeads`.
- Produceert: `lighthouse measure-recovery --repo-dir <pad> [--days N] [--db <pad>]` (standaard `--days 14`; `--db` standaard `store.DefaultPath()`, zoals `internal/cli/tick.go:218`; repo-slug en default branch uit `repoconfig.Load(--repo-dir)`; een testdatabase komt via `--db`): eerst `git fetch`, dan voor elke PR die in de laatste `N` dagen mergde (`MergedPRs`) en voor elke `open`-rij buiten dat venster, behalve PR's met een gesloten rij, `PullHead` zoeken op nummer, issuenummer uit `HeadRef` (`issue-<nr>-…`), meten, `CountFixPRRounds`, `RecordRecovery`. Uitvoer: een regel per PR (`#<pr> <status> <n> items, <m> fix-pr-rondes`) en een regel per overgeslagen PR met reden. Exitcode 0 bij overgeslagen PR's; 1 bij een config- of forge-fout; 2 bij verkeerd gebruik (zoals `internal/cli/report.go:48`).

- [ ] **Stap 1: schrijf de falende tests**: `ForgeIssues` filtert niet zelf (dat doet `MeasureRecovery`) maar mapt alle velden, labels inbegrepen; de CLI-test gebruikt een fake `Forge`-bron (zoals `fakeSource` in `internal/report/build_test.go:33`, bij voorkeur via een smalle interface in plaats van de hele `Forge`) en de nep-repo uit taak 3, en controleert: PR met issuebranch en fix → regel `recovered 1 items`; PR zonder fix binnen het venster → `open`; PR zonder issuebranch → overgeslagen met reden `no_issue_branch`; tweede run schrijft geen dubbele rijen; een `open`-rij buiten `--days` wordt na het venster alsnog gesloten; een gesloten rij roept de forge niet aan.
- [ ] **Stap 2:** FAIL.
- [ ] **Stap 3:** implementeer adapter en opdracht; klok `time.Now` alleen in de `main`/cli-laag, doorgegeven als parameter.
- [ ] **Stap 4:** schrijf documentatie en changelogfragment.
- [ ] **Stap 5: dogfood.** Raakt de wijziging `pack/` of wat `init` schrijft? Zo niet, noteer dat; zo ja: `go run ./cmd/lighthouse init --upgrade`, controleer `No changes.` na commit van de installed copy.
- [ ] **Stap 6:** volledige gate: `lighthouse gate go -- go test -race` en `golangci-lint run`; beide groen.
- [ ] **Stap 7: commit** `feat(cli): lighthouse measure-recovery, documentatie en changelog (#377)`.

---

## Zelfreview

- **Dekking van de AC:** 1 (SHA's, taak 2+3), 2 (bug-issues, taak 2+5), 3 (`fix-pr`-rondes apart, taak 4: aparte kolom), 4 (status `open`, taak 2), 5 (nep-repo, taak 3 stap 5).
- **DoD:** tests (elke taak), documentatie (taak 5), review (via de eindreview van de uitvoering).
- **Open punt, bewust niet beslist:** GitLab. `MergedPRs` is daar niet ondersteund (`internal/forge/gitlab_report.go:13-14`); de opdracht meldt dat als fout en laat GitLab buiten dit issue; de beperking komt in `docs/install.md` en de PR-body (de issuetekst "elke repo" is hierin ingeperkt).
