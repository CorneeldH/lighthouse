# ci/changelog naar Go, met workflow_dispatch en de nulmeting — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** `ci/changelog.py` wordt `go run ./ci/changelog`, met dezelfde vlaggen, uitvoer en
exitcodes; `ci.yml` krijgt `workflow_dispatch` met alle domeinen, en de nulmeting staat op #360.

**Architectuur:** Een `main`-pakket `ci/changelog` in de bestaande module, alleen stdlib. De
functies volgen die van het Python-script één op één; `run(args, stdout, stderr) int` is de
CLI, zodat tests de exitcodes in-process zien. Een fixture uit de laatste drie releases bewijst
byte-gelijkheid met wat het Python-script echt in `CHANGELOG.md` schreef.

**Tech stack:** Go 1.26.4 (stdlib: `flag`, `os/exec`, `regexp`, `strings`), GitHub Actions,
`gh` voor de nulmeting.

**Spec:** [`2026-11-16-repo-zonder-python-design.md`](../specs/2026-11-16-repo-zonder-python-design.md)
(§1 triage, §4 scripts, §7 meten). Issue: #360.

## Globale randvoorwaarden

- Code, comments en testnamen Engels; commits, PR-body, issuecomment en fragment Nederlands.
- Go volgt `/lighthouse-go-standard`; tests volgen `/lighthouse-test-standard` (kernvraag: welke
  productiewijziging laat deze test falen?).
- Geen ander gedrag (No-Go van de milestone): subcommando's `check`, `release`, `notes`; vlaggen
  `--base`, `--head`, `--root` (default `.`), `--event` ∈ {`push`,`schedule`,`workflow_dispatch`},
  `--bump` ∈ {`auto`,`patch`,`minor`,`major`} (default `auto`), `--date` (default vandaag, lokaal,
  `YYYY-MM-DD`); positioneel `<tag>` bij `notes`.
- Exitcodes van het programma: 0 ok, 1 verdict, 2 `COULD NOT CHECK` of een gebruiksfout (zoals
  argparse). `go run` maakt van elke niet-nul exit een 1 en zet `exit status N` op stderr
  (gemeten, go1.26.4); geen workflow onderscheidt 1 van 2. De package-doc zegt dat.
- Elke stderr-tekst van het Python-script blijft letterlijk gelijk, behalve het detail van een
  OS- of decodeerfout na `cannot read: ` (dat is Go's eigen fouttekst). `{x!r}` wordt `'x'`
  (enkele quotes), niet `%q`; dat wijkt af van Python's `repr` bij een waarde met `'` (alleen stderr), en de package-doc noemt dat.
- Geen `.go`-bestand noemt `docs/`, `changelog.d` of `ci/literal-` zonder vrijstelling in
  `tests/test_domains.py` (`_without_default_paths`).
- Pins in `tests/test_ci_domains_pin.py` en `tests/test_release_workflows_pin.py` krijgen alleen
  een nieuwe needle, behalve de telling in `test_actions_pinned_by_sha` (24 → 25, Task 5);
  hun triage hoort bij #365.
- Een run onder de controller kan een branch die `.github/workflows/` raakt niet zelf pushen:
  `workflow-push-gate` blokkeert dat. Zo'n push gaat via de handoff `next: push-workflows`
  (Task 2 Step 1, Task 6 Step 3); de run stopt daar. `after:` is bij Task 2 de uitvoering van
  dit plan, bij Task 6 `/lighthouse-write-pr 360`.
- Oude plannen en specs die `changelog.py` noemen, blijven staan (No-Go).

## Review Focus

1. **`notes <tag> --root <dir>` in beide volgordes.** argparse neemt vlaggen na de positionele
   tag; Go's `flag` stopt bij de eerste niet-vlag. `notes v1 --root x` en `notes --root x v1`
   geven dezelfde uitvoer → test in Task 3.
2. **Een workflow zonder Go-toolchain.** `release` (release.yml) en `publish` (build.yml) hebben
   geen `setup-go`; `go run` daar zonder die stap draait op de toolchain van de runner-image of
   faalt. → `setup-go` toegevoegd en gepind in Task 5.
3. **`go run` downloadt modules in de release-job.** Het pakket is stdlib-only; dat mag geen
   module uit `go.mod` laten ophalen. → `GOPROXY=off GOFLAGS=-mod=mod` met een lege
   `GOMODCACHE` in Task 5.
4. **CRLF of BOM.** Een fragment met BOM en CRLF wordt geaccepteerd (bestaande test). Een
   `CHANGELOG.md` met CRLF: Python's `splitlines()` breekt op `\r\n`, `\r` en `\n` en `notes`
   geeft geen `\r` terug → `splitLines` doet die drie, test in Task 3. Overige Unicode-
   regelscheiders van `splitlines()` volgen niet; de package-doc noemt dat.
5. **`publish` op een tag van vóór de port.** Die checkout heeft geen `ci/changelog/`; `go run`
   faalt, en dan zegt publish `no CHANGELOG.md section` (bij een prerelease: de vaste regel).
   Alleen een handmatige herpublicatie van een oude tag raakt dit; geen code, wel een zin in de
   PR-body.

---

## Bestanden

| Pad | Rol |
|---|---|
| `.github/workflows/ci.yml` | `workflow_dispatch` + invoer `all-domains`; changelog-check via Go |
| `.github/workflows/release.yml`, `build.yml` | `setup-go` + `go run ./ci/changelog` |
| `ci/changelog/main.go` | package-doc, `main`, `run` (vlaggen, exitcodes) |
| `ci/changelog/changelog.go` | fragmenten, bump, tags, sectie renderen/invoegen/lezen, `release` |
| `ci/changelog/changelog_test.go`, `main_test.go` | unit- en CLI-tests |
| `ci/changelog/release_fixture_test.go` + `testdata/releases/<tag>/` | byte-gelijkheid op drie releases |
| `tests/test_domains.py` | vrijstelling voor `ci/changelog/*.go` |
| `tests/test_ci_domains_pin.py`, `tests/test_release_workflows_pin.py` | needles naar `go run ./ci/changelog` |
| `ci/changelog.py`, `tests/test_changelog.py`, `tests/_changelog.py` | weg |
| `changelog.d/issue-360-changelog-go.md` | fragment `type: internal` |

---

### Task 1: `workflow_dispatch` met alle domeinen in `ci.yml`

**Files:**
- Modify: `.github/workflows/ci.yml:8-9` (trigger), `:31-37` (`changes`), `:40-51` (`changelog`), `:76-80` (gitleaks-stap)
- Modify: `tests/test_ci_domains_pin.py:198-202`
- Create: `changelog.d/issue-360-changelog-go.md`

**Interfaces:**
- Produces: invoer `all-domains` (boolean, default `true`); bij `workflow_dispatch` zet `changes`
  `web`, `python` en `go` op `"${{ inputs.all-domains }}"` zonder `git diff`.

Ontwerpkeuzes (vast):
- Trigger: `workflow_dispatch:` met `inputs: all-domains: {type: boolean, default: true, description: …}`.
- `changes`-stap: `EVENT: ${{ github.event_name }}` en `ALL: ${{ inputs.all-domains }}` in `env`;
  bij `EVENT = workflow_dispatch` schrijft hij `web=$ALL`, `python=$ALL`, `go=$ALL` naar
  `$GITHUB_OUTPUT` en stopt; anders de bestaande regel ongewijzigd.
- `changelog`-job: job-level `if: github.event_name == 'pull_request'` — op een dispatch is er
  geen PR om te checken. Geen `needs:`.
- `secret-scan`: alleen de stap `scan the commits of this PR` krijgt
  `if: github.event_name == 'pull_request'`; de pytest-stappen draaien ook bij een dispatch, zodat
  de nulmeting hun minuten meetelt.
- `concurrency` blijft; de runs van de nulmeting gaan daarom na elkaar (Task 2).

- [ ] **Step 1: Pas de pin aan (faalt eerst).** In `test_changelog_job_runs_on_every_pr`:
  vervang `assert "needs:" not in job and "if:" not in job` door
  `assert "needs:" not in job and "needs.changes" not in job` en
  `assert re.findall(r"^\s+if: .*$", job, re.M) == ["    if: github.event_name == 'pull_request'"]`.
  De `check`-needle blijft nog `python3 ci/changelog.py check …` (Task 5 wijzigt hem). Voeg
  `test_dispatch_turns_on_every_domain` toe: `_text()` bevat `workflow_dispatch:` en
  `all-domains:` met `type: boolean` en `default: true`, en de job `changes` bevat
  `web=$ALL`, `python=$ALL` en `go=$ALL`.
- [ ] **Step 2:** `uv run pytest -q tests/test_ci_domains_pin.py` → FAIL op de twee tests.
- [ ] **Step 3:** Wijzig `ci.yml` zoals hierboven. Werk de kopcomment (r. 1-5) bij met één zin
  over de dispatch.
- [ ] **Step 4:** `uv run pytest -q tests/test_ci_domains_pin.py tests/test_domains.py` → PASS;
  `actionlint .github/workflows/ci.yml` als het lokaal staat, anders overslaan en melden.
- [ ] **Step 5:** Fragment `changelog.d/issue-360-changelog-go.md`:

  ```markdown
  ---
  type: internal
  ---
  `ci/changelog` is Go (`go run ./ci/changelog`), en `ci.yml` kan met de hand draaien met alle domeinen aan.
  ```
- [ ] **Step 6: Commit** `ci(ci): workflow_dispatch met alle domeinen voor de nulmeting (#360)`.

### Task 2: De nulmeting

Drie runs van `ci.yml` met alle domeinen, op de code van `main` vóór de port: de branch staat
op `72fafa51` (v0.18.0) plus de plancommit en Task 1; `origin/main` is intussen verder. Noteer
in de comment van Step 4 de gemeten commit.

- [ ] **Step 1:** Push `issue-360-changelog-go` (alleen Task 1 erop). Onder de controller blokkeert
  `workflow-push-gate` een gewone `git push` (Task 1 raakt `ci.yml`): draai eerst V2b van
  `/lighthouse-write-pr` (`lighthouse roborev-gate gate --push`) en post dan de handoff met
  `next: push-workflows`, `after: /superpowers:subagent-driven-development <absoluut planpad>` en
  `roborev: <uitvoer van lighthouse roborev-gate body-line --push>`; de run stopt, en de volgende
  sessie hervat bij Task 2 Step 2. Buiten de controller:
  `git push -u origin issue-360-changelog-go`.
- [ ] **Step 2:** Drie keer na elkaar, elke run pas na de vorige (zelfde concurrency-groep,
  `cancel-in-progress`):

  ```bash
  gh workflow run ci.yml --ref issue-360-changelog-go -f all-domains=true
  # wacht tot er een id is die niet de vorige run is (`prev`; leeg bij de eerste run)
  until id=$(gh run list --workflow ci.yml --branch issue-360-changelog-go --event workflow_dispatch -L1 --json databaseId -q '.[0].databaseId') && [ "$id" != "$prev" ]; do sleep 3; done
  gh run watch "$id" --exit-status; prev=$id
  ```

  Weigert GitHub de dispatch (`Workflow does not have 'workflow_dispatch' trigger`) omdat `main`
  de trigger nog niet heeft: stop en meld het. De terugvaloptie is Task 1 als eigen PR mergen
  en dan op `main` meten; dat beslist de gebruiker.
- [ ] **Step 3:** Per run de wandklok en de som van de runnerminuten:

  ```bash
  gh run view "$id" --json startedAt,updatedAt,jobs -q '
    [(.updatedAt|fromdate)-(.startedAt|fromdate)] as [$wall]
    | [.jobs[] | select(.conclusion!="skipped") | ((.completedAt|fromdate)-(.startedAt|fromdate))] | add as $sum
    | "\($wall) s wandklok, \($sum) s runnertijd"'
  ```

  Elke run moet groen zijn en alle domeinjobs bevatten (`python` ×3, `go`, `go-test`, `go-e2e`,
  `no-python`, `web`, `binary`); anders telt hij niet.
- [ ] **Step 4:** Comment op #360 (`lighthouse forge issue comment 360 --body-file …`), Nederlands:
  tabel run-id (link), commit, wandklok, som runnerminuten, plus het gemiddelde. Kop:
  `Nulmeting #360 (ci.yml, alle domeinen)`.

### Task 3: `ci/changelog` in Go

**Files:**
- Create: `ci/changelog/main.go`, `ci/changelog/changelog.go`, `ci/changelog/changelog_test.go`, `ci/changelog/main_test.go`
- Modify: `tests/test_domains.py` (`_without_default_paths`)

**Interfaces (Produces, alles in `package main`):**

```go
type fragment struct{ path, typ, text string }
type fragmentError struct{ msg string } // exit 1; msg begint met het pad
type releaseError struct{ msg string }  // exit 1
type tagError struct{ msg string }      // exit 2

func parseFragment(path string) (fragment, error)
func readFragments(dir string) ([]fragment, error)                  // ontbrekende dir: nil, nil
func addedFragments(root, base, head string) ([]string, error)      // git-fout: gewone error (exit 2)
func decideBump(frags []fragment, event, forced string) (string, error) // "" = niets te releasen
func latestTag(tags []string) (string, error)
func nextVersion(tag, bump string) (string, error)
func renderSection(version, date string, frags []fragment) string
func insertSection(changelog, section string) string
func extractSection(changelog, tag string) (string, bool)
func splitLines(s string, keepEnds bool) []string                  // breekt op "\r\n", "\r", "\n"
func release(root, event, forced, date string, tags []string) (string, error)
func run(args []string, stdout, stderr io.Writer) int
```

Elk gedrag en elke tekst komt uit `ci/changelog.py` (regelnummers per functie: 55-84, 87-108,
111-123, 131-149, 152-169, 172-205, 208-287). Twee punten die de signatuur niet vastlegt:
- Sorteren: sleutel `(stam zonder -<n>, n)`, zonder suffix n = 1, stabiel op de volgorde van
  `os.ReadDir`; `.lighthouse` wordt overgeslagen, elk ander item dat geen regulier `*.md`-bestand
  is → `fragmentError "<pad>: not a `*.md` fragment"`. Het pad in een melding is
  `filepath.Join(root, "changelog.d", naam)`, zoals Python's `Path`.
- `run`: per subcommando een `flag.FlagSet` met `ContinueOnError`; positionele tag van `notes`
  mag vóór of na `--root` (zie Review Focus 1). Gebruiksfout → usage op stderr, exit 2.
  `release` print de tag of een lege regel; `notes` print de sectie plus `\n`.

De vrijstelling in `tests/test_domains.py`: `ci/changelog/*.go` leest `changelog.d/` van de
checkout alleen tijdens `check`/`release`, in jobs die bij elke PR of release draaien; de tests
lezen alleen `testdata/` en tijdelijke repo's. Haal `changelog.d` en `docs/releasing.md` (uit de
melding van `check`) weg, met die reden als comment.

**Triage van `tests/test_changelog.py` (34 functies, 58 gevallen) → Go.** Standaard gaat elk geval mee als
tabelgeval; samenvoegen alleen waar twee gevallen op dezelfde mutatie falen:

| Python | Go | Bestemming |
|---|---|---|
| `test_parse_fragment_accepts` (2), `_refuses` (8) | `TestParseFragment` (tabel, 10) | naar Go |
| `test_read_fragments_skips_marker_and_sorts`, `_orders_numbered_suffixes_after_the_base` | `TestReadFragmentsOrder` | naar Go, samengevoegd |
| `test_read_fragments_missing_directory`, `_refuses_other_files` | `TestReadFragmentsMissingDir`, `TestReadFragmentsRefusesOtherFiles` | naar Go |
| `test_check_*` (8) | `TestCheck` (tabel, tijdelijke git-repo via `t.TempDir()` + `git`, met `GIT_AUTHOR_*`/`GIT_COMMITTER_*` in de env zoals de andere Go-tests) | naar Go |
| `test_added_fragments_non_utf8_name_cannot_check` | `TestCheck/nonUTF8Name` | naar Go: `addedFragments` weigert met `utf8.ValidString` een niet-UTF-8-pad (gewone error, exit 2); `t.Skip` bij `errors.Is(err, syscall.EILSEQ)` (APFS, macos.yml draait `go test ./...`) |
| `test_decide_bump` (tabel), `_refuses` (3) | `TestDecideBump`, `TestDecideBumpRefuses` | naar Go |
| `test_latest_tag_*`, `test_next_version` | `TestLatestTag`, `TestNextVersion` | naar Go |
| `test_render_section_*`, `test_insert_section_goes_above_latest`, `test_extract_section_roundtrip` | Task 4-fixture | weg — gedekt door `TestReleaseFixture`, alleen als de fixture een `internal`-fragment, een `removed`-fragment en een meerregelig fragment bevat; anders naar Go |
| `test_insert_section_without_release_goes_last` | `TestInsertSectionWithoutRelease` | naar Go |
| `test_extract_section_missing`, `test_cli_notes_missing_section_exits_1` | `TestRunNotesMissingSection` (exit 1, melding) | naar Go, samengevoegd |
| `test_release_without_tags_raises_tag_error`, `test_cli_release_without_tags_exits_2` | `TestRunReleaseWithoutTags` (exit 2, `COULD NOT CHECK:`) | naar Go, samengevoegd |
| `test_release_without_changelog_refuses`, `test_cli_release_error_exits_1` | `TestRunReleaseError` (exit 1) | naar Go, samengevoegd |
| `test_release_writes_changelog_and_removes_fragments`, `_nothing_leaves_tree_untouched`, `test_cli_release_prints_tag`, `test_cli_notes_prints_section` | `TestRelease…`, `TestRun…` | naar Go |

Nieuw (Review Focus): `TestRunNotesTagAfterRoot` (beide volgordes, zelfde stdout) en
`TestExtractSectionCRLF` (`"## [v1] - d\r\nbody\r\n## [v0] - d\r\n"` → `"body"`, true).

- [ ] **Step 1:** Schrijf `changelog_test.go` en `main_test.go` volgens de tabel; de verwachte
  waarden en meldingen komen letterlijk uit `tests/test_changelog.py`.
- [ ] **Step 2:** `go test ./ci/changelog/` → FAIL (build: undefined).
- [ ] **Step 3:** Implementeer `changelog.go` en `main.go` met de signaturen hierboven;
  package-doc vertelt de drie aanroepen en de exitcode-mapping van `go run`.
- [ ] **Step 4:** `go test -race -shuffle=on ./ci/changelog/` → PASS; `go vet ./ci/changelog/`
  en `golangci-lint run ./ci/changelog/...` → schoon; `uv run pytest -q tests/test_domains.py`
  → PASS.
- [ ] **Step 5: Mutatiecheck.** Draai elk één keer en zie rood: sorteer `x-10.md` vóór `x-2.md`;
  laat `decideBump` bij `push` zonder `fixed` toch `minor` geven; laat `run` bij `tagError` 1
  geven; laat `notes` de tag na `--root` negeren. Zet terug.
- [ ] **Step 6: Commit** `feat(ci): ci/changelog in Go (#360)`.

### Task 4: Byte-gelijkheid op de laatste drie releases

Vóór Task 5: het Python-script moet nog bestaan.

**Files:**
- Create: `ci/changelog/testdata/releases/{v0.16.2,v0.17.0,v0.18.0}/` en `ci/changelog/release_fixture_test.go`

Per release `<tag>` met release-commit `<c>` (`c3036aed`, `78cde225`, `72fafa51`) en vorige tag `<prev>`:
- `changelog.d/` = `git archive <c>^ changelog.d` (alle fragmenten, marker incluis);
- `CHANGELOG.before` = `git show <c>^:CHANGELOG.md`; `CHANGELOG.want` = `git show <c>:CHANGELOG.md`
  (zonder `.md`: `md-link-check` blokkeert de relatieve link `docs/releasing.md` in een testdata-map)
  — wat het Python-script werkelijk schreef;
- `notes.want` = uitvoer van `python3 ci/changelog.py notes <tag> --root <dir waarin CHANGELOG.want als CHANGELOG.md staat>`;
- `meta` = `<prev>` en de datum uit de kop `## [<tag>] - <datum>`.

- [ ] **Step 1:** Bouw de drie mappen met de commando's hierboven; controleer met de hand dat
  `notes.want` gelijk is aan het stuk van `CHANGELOG.want` onder `## [<tag>]` en dat elke
  release `## [<tag>]` boven `## [<prev>]` heeft.
- [ ] **Step 2:** `TestReleaseFixture` (tabel over de drie tags): kopieer de map naar
  `t.TempDir()` (`CHANGELOG.before` → `CHANGELOG.md`), roep
  `release(dir, "workflow_dispatch", "auto", datum, []string{prev})` aan en assert: tag == `<tag>`;
  `CHANGELOG.md` byte-gelijk aan `CHANGELOG.want`; geen `*.md` meer in `changelog.d/`. Dan
  `run([]string{"notes", tag, "--root", dir}, …)` → exit 0, stdout byte-gelijk aan `notes.want`.
- [ ] **Step 3:** `go test -run TestReleaseFixture ./ci/changelog/` → PASS. Mutatie: laat
  `renderSection` de groepen in andere volgorde zetten → rood; terug.
- [ ] **Step 4:** Bepaal met `grep -l '^type: internal' …/changelog.d/*.md` en een `removed`- en een meerregelig
  fragment of de fixture `render`/`insert`/`extract` dekt (tabel Task 3); zo niet, port die
  tests alsnog.
- [ ] **Step 5: Commit** `test(ci): changelog-fixture uit drie releases (#360)`.

### Task 5: Workflows naar Go, Python weg

**Files:**
- Modify: `.github/workflows/ci.yml` (`changelog`-job), `release.yml:70-103`, `build.yml:168-181`
- Modify: `tests/test_ci_domains_pin.py:201`, `tests/test_release_workflows_pin.py:204,413`
- Delete: `ci/changelog.py`, `tests/test_changelog.py`, `tests/_changelog.py`

- [ ] **Step 1:** Needles: `python3 ci/changelog.py check --base "$BASE" --head "$HEAD"` →
  `go run ./ci/changelog check --base "$BASE" --head "$HEAD"`; `ci/changelog.py notes` →
  `go run ./ci/changelog notes`; `ci/changelog.py release` → `go run ./ci/changelog release`.
  Voeg in beide pinbestanden een check toe dat de job (`changelog`, `release`, `publish`)
  `actions/setup-go@` met `go-version-file: go.mod` bevat vóór de `go run`. Verhoog in
  `test_actions_pinned_by_sha` (`tests/test_ci_domains_pin.py:192`) de telling 24 naar 25: de
  `changelog`-job in `ci.yml` krijgt een `uses:`-regel erbij.
- [ ] **Step 2:** `uv run pytest -q tests/test_ci_domains_pin.py tests/test_release_workflows_pin.py` → FAIL.
- [ ] **Step 3:** In de drie jobs: `actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0`
  met `go-version-file: go.mod` (in `release` en `publish` ook `cache: false`: geen build-cache in
  een release-job) na de checkout (in `release` vóór de `release`-stap, buiten de
  retry-lus), en de aanroep naar `go run ./ci/changelog …`, verder letterlijk gelijk.
- [ ] **Step 4:** `git rm ci/changelog.py tests/test_changelog.py tests/_changelog.py`.
- [ ] **Step 5:** Verificatie:
  - `uv run pytest -q tests/test_ci_domains_pin.py tests/test_release_workflows_pin.py tests/test_domains.py` → PASS
  - `grep -rn 'changelog\.py' --exclude-dir=planning --exclude-dir=.git .` → niets
  - `d=$(mktemp -d); GOMODCACHE=$d GOPROXY=off GOFLAGS=-mod=mod go run ./ci/changelog notes v0.18.0 | head -3` → de sectie, geen download
  - `go run ./ci/changelog check --base origin/main --head HEAD; echo $?` → 0 (fragment uit Task 1)
- [ ] **Step 6: Commit** `ci(release): changelog via go run ./ci/changelog, Python-script weg (#360)`.

### Task 6: PR

- [ ] **Step 1:** `uv run pytest -q` en `go test -race -shuffle=on ./...` → groen.
- [ ] **Step 2:** `/lighthouse-write-pr`: `Closes #360` alleen als de nulmeting-comment er staat;
  de PR-body bevat de triagetabel van Task 3 (bestand, aantal, bestemming, reden, bij
  **weg — gedekt** de Go-test) en de zin over Review Focus 5.
- [ ] **Step 3:** Push (raakt `.github/workflows/`: onder de controller V2b, dan de handoff
  `next: push-workflows`, `after: /lighthouse-write-pr 360`) en PR.
