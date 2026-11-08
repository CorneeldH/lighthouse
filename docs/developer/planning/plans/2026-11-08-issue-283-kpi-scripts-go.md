# KPI-scripts in Go: implementatieplan

> **Voor agents:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development
> (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen
> gebruiken checkboxes (`- [ ]`).

**Doel:** stap 10 van de milestone *Lighthouse zonder Python* (#283).
`measure_start_issue_kpi.py` en `measure_fix_pr_kpi.py` worden de subcommando's
`lighthouse measure-start-issue-kpi` en `lighthouse measure-fix-pr-kpi`, met dezelfde metrics,
vlaggen en JSON-uitvoer. De skills `start-issue`, `fix-pr` en `route-issue` noemen de
subcommando's, en beide `.py` gaan weg.

**Architectuur:** eerst gaan de inline pytest-gevallen naar gedeelde fixtures onder
`tests/pack/fixtures/kpi/`, en een pytest-runner bewijst ze tegen de Python (spec §9). Daarna
komt een nieuw package `internal/kpi` met de transcriptlaag, Python-gelijke uitvoer, de
classificatie en de twee tabellen. Een Go-parity-test draait dezelfde fixtures. Twee dunne
cobra-bestanden in `internal/cli` roepen `kpi.RunStartIssue` en `kpi.RunFixPR` aan. Pas als Go
groen is, gaan de skilltekst om en de Python weg.

**Tech stack:** Go (cobra, `regexp`, `encoding/json` alleen om te lezen), pytest voor de
fixtures en de skilltekst-tests.

**Spec:** [`2026-11-07-zonder-python-design.md`](../specs/2026-11-07-zonder-python-design.md):
*Anti-doelen*, §8, §9, §10 en *Opdeling* stap 10.

## Globale eisen

- Commando's: `lighthouse measure-start-issue-kpi [leadin|worktree-first|mistrigger|paths|gate|scout] [--dir <pad>] [--since YYYY-MM-DD]`
  en `lighthouse measure-fix-pr-kpi [leadin|diagnose|mistrigger|scope] [--dir <pad>] [--since YYYY-MM-DD]`.
  Zonder metric de hele tabel, in deze volgorde.
- Uitvoer byte-gelijk aan `json.dumps(table, indent=2, ensure_ascii=False)` plus `\n`. Sleutels
  in de volgorde van de Python. Een float print zoals Python's `repr` (`1.0`, `0.667`, `2.5`).
- Afronding zoals Python's `round`: `round(x, n)` is `strconv.FormatFloat(x, 'f', n, 64)` terug
  geparsed (half-even op de exacte binaire waarde; gecontroleerd: `2.25→2.2`, `0.125→0.12`,
  `2.675→2.67`, `0.35→0.3`). `round(x)` zonder `n` is `math.RoundToEven` en geeft een int.
- Exitcodes: 0 met tabel. 2 met `transcript directory does not exist: <dir>`,
  `measure: <config-fout>` of `measure: git log failed: <stderr>` op stderr. Een onbekende metric
  of een extra argument geeft rc 2 met `measure: unknown metric "<x>" (choose from <lijst>)`.
  Die tekst wijkt bewust af van argparse; de exitcode niet.
- `<dir>` in de foutmelding print zoals Python's `Path`: dubbele `/` samen, `.`-delen en een
  slash aan het eind weg, `..` blijft (`./gone/` → `gone`, `a/../b` → `a/../b`).
- Standaardmap zonder `--dir`: `$HOME/.claude/projects/<root>`. `<root>` is `config.RepoRoot()`,
  absoluut gemaakt en door `filepath.EvalSymlinks` (bij een fout: alleen absoluut), met elk teken
  buiten `[A-Za-z0-9]` vervangen door `-`.
- `measure-start-issue-kpi` leest ook de zustermappen `<dir>--claude-worktrees-*` (prefix-match op
  de naam in `filepath.Dir(dir)`). `measure-fix-pr-kpi` leest alleen `<dir>/*.jsonl`.
- Volgorde van sessiebestanden: zoals Python's `Path`-sortering, dus per padonderdeel
  (`strings.Split(pad, "/")` lexicografisch vergeleken). Daardoor komt de hoofdmap vóór
  `<dir>--claude-worktrees-*`, ook al sorteert `-` als string vóór `/`.
- Python-semantiek die de port overneemt: `isSidechain`/`isMeta` tellen bij elke truthy waarde
  (`true`, niet-nul getal, niet-lege string, lijst of object); `userType` ontbreekt, is `null` of
  `"external"`. Prompttekst en `examples` knippen op 120 **runes**. `--since` vergelijkt de eerste
  10 runes van `timestamp` als string. Een kapotte JSON-regel valt weg; ongeldige UTF-8 wordt
  `U+FFFD`.
- Regex-tekenklassen zoals Python 3 op `str`: `\s` wordt
  `[\t\n\v\f\r\x{1c}-\x{1f}\x{85}\p{Z}]`, `\S` de negatie daarvan, `\d` wordt `\p{Nd}`, en `\w`
  in de lookaround-functies is `unicode.IsLetter || unicode.IsNumber || '_'`. Go's `\b` blijft
  ASCII. Dat wijkt alleen af als een niet-ASCII letter of cijfer direct tegen de match aan
  staat; dat staat in het package-commentaar, niet in een test.
- Een niet-string `command` in een Bash-input telt als `""`. Een timestamp die geen RFC 3339 en
  geen naïeve `2006-01-02T15:04:05[.ffffff]` is, geeft geen looptijd (Python's `fromisoformat`
  accepteert meer vormen, die niet in transcripten voorkomen).
- Bewust overgenomen, niet gerepareerd: `MUTATING_BASH_RE` matcht `cat <<< 'HELLO'` wél (de
  `(?!<)` in de Python is een no-op, gecontroleerd), ook al zegt het Python-commentaar anders. Go
  laat de lookahead weg en een fixture-rij pint de huidige uitkomst. Een fix is een eigen issue
  (milestone-no-go: geen ander gedrag).
- Weg in deze stap: `pack/core/scripts/measure_start_issue_kpi.py`, `measure_fix_pr_kpi.py`,
  hun geïnstalleerde kopie en hun pytest-bestanden. Bevroren, geen byte anders:
  `pack/core/lib/*.py` en `.lighthouse/lib/lighthouse_lib/*.py`.
- Go volgt `/lighthouse-go-standard`, tests volgen `/lighthouse-test-standard`. Code, comments en
  meldingen in het Engels; commits en PR in het Nederlands.
- Elke wijziging onder `pack/` draait `go run ./cmd/lighthouse init --upgrade` en commit de
  geïnstalleerde kopie. CI eist `No changes.`

## Review-focus

Vijf invoerklassen die de spec impliceert maar die geen bestaande test raakt. Elke regel heeft
een fixture of test in de taak die de code bezit.

1. **Sessies in hoofdmap én worktree-zustermap, met `mistrigger`-voorbeelden uit beide.** De
   volgorde van `examples` hangt aan de sortering; een string-sort zet de zustermap eerst. →
   fixture-case `start-issue/worktree-order` (taak 1, gedraaid in taak 4).
2. **Een even aantal waarden in `_summary`** (`median` wordt een float, `2.0`) en een `mean` op
   een half-even grens (`[1, 2, 2, 4]` → `2.2`). → fixture-case `start-issue/leadin-even` (taak 1)
   en `TestSummary` (taak 2).
3. **Een prompt met niet-ASCII-tekst, een U+2028, een tab en meer dan 120 runes** in
   `examples`. Go's `encoding/json` escapet U+2028, Python niet. → fixture-case
   `fix-pr/mistrigger-unicode` (taak 1) en `TestEncode` (taak 2).
4. **Lookaround-randen**: `/ab-_` (backtrack tot `/ab`), `a#12`, `12345`, `v1.23`, `١٢ x`
   (Arabisch-Indische cijfers), `é/foo`. → rijen in `classify.json` (taak 1, gedraaid in taak 3).
5. **Een NBSP tussen werkwoord en nummer** (`pak 610 op`) en een here-string
   `cat <<< 'HELLO'`. → rijen in `classify.json` (taak 1, gedraaid in taak 3).

---

### Taak 1: gedeelde fixtures, bewezen tegen de Python

**Files:**
- Create: `tests/pack/fixtures/kpi/lighthouse.toml`, `tests/pack/fixtures/kpi/headings.toml`
- Create: `tests/pack/fixtures/kpi/classify.json`
- Create: `tests/pack/fixtures/kpi/start-issue/<case>/{expect.json,transcripts/…}`
- Create: `tests/pack/fixtures/kpi/fix-pr/<case>/{expect.json,transcripts/…}`
- Create: `tests/pack/test_kpi_fixtures.py`

**Interfaces:**
- Produces: het fixture-formaat dat taak 3, 4 en 5 lezen.
  - `classify.json`: lijst van `{"fn": <naam>, "input": <string>, "want": <bool|int|string|null>, "from": <oude testnaam>}`.
    `fn` is één van: `pickup` (bool), `names_skill` (bool), `has_issue_number` (bool), `fix_pr`
    (bool), `mutating` (bool; input is het Bash-commando), `diagnostic` (bool), `gate` (int of
    null), `subgate` (string of null), `skip_reason` (bool: reden ≥ 20 runes na `strip(" .")`).
  - `<case>/expect.json`: lijst van `{"args": [...], "rc": int, "stdout": string, "stderr": string, "from": [oude testnamen]}`.
    `args` zijn relatief aan de case-map (`["leadin", "--dir", "transcripts"]`).
  - `headings.toml` bevat `gate_line` (`nl = "Poort"`, `en = "Gate"`) en `scout_skipped`
    (`nl = "Scout: overgeslagen"`, `en = "Scout: skipped"`) — neem de waarden letterlijk over uit
    `pack/core/headings.toml`. `lighthouse.toml` zet `[project] language = "nl"`.
  - Omgeving voor beide runners: cwd = case-map, `LIGHTHOUSE_REPO_ROOT=""`,
    `LIGHTHOUSE_TOML` en `LIGHTHOUSE_HEADINGS` naar de twee bestanden in `tests/pack/fixtures/kpi/`.

- [ ] **Stap 1: zet elk geval uit `tests/pack/test_start_issue_kpi.py` en `tests/pack/test_fix_pr_kpi.py` om**

  Regel per oude test:
  - Een assert op een classifier (`pickup_issue_number`, `names_a_skill`, `has_issue_number`,
    `fix_pr_number`, `is_mutating`, `is_diagnostic`, `gate_re`, `gate_suffix_re`,
    `scout_skip_re`) wordt een rij in `classify.json`.
  - Een assert op een `Turn`-functie (`count_leadin`, `first_write_path`, `announced_gate`,
    `announced_subgate`, `count_diagnostics`, `overruns_scope`) of op een `measure_*`-functie
    wordt een case-map met de records als `.jsonl` en de verwachte rij als `stdout` van
    `main([<metric>, "--dir", "transcripts"])`.
  - Geen fixture, maar een Go-test in taak 4 of 5: `test_transcript_dir_from_git_root`,
    `test_since_option`, `test_paths_dirs_from_config`, `test_git_failure_is_rc_2_without_traceback`.
  - Vervalt met de Python, zonder vervanger: `test_help_is_english` (beide),
    `test_skill_names_are_lighthouse` (beide; de namen staan in elke fixture),
    `test_loads_start_issue_kpi_as_its_neighbour`, `test_runs_as_installed_file` (vervangen door
    `TestMeasureCommandsRun` in taak 5).

  Voeg daarnaast de cases en rijen van de *Review-focus* toe: `start-issue/worktree-order`
  (hoofdmap, zustermap en een vreemde map `<dir>-ander` met elk een sessie zonder issuenummer,
  arg `mistrigger`), `start-issue/leadin-even` (vier pickup-turns met lead-in 1, 2, 2 en 4, arg
  `leadin`), `fix-pr/mistrigger-unicode` (prompt `"héél lang\t…"` van 130 runes zonder
  fix-verzoek, arg `mistrigger`), plus de lookaround- en NBSP-rijen uit *Review-focus* 4 en 5.
  Ook `start-issue/missing-dir` (`["leadin", "--dir", "./gone/"]`, rc 2, stderr
  `transcript directory does not exist: gone\n`) en een `fix-pr/full-table` zonder metric.

  `stdout` komt uit één run van de Python (een wegwerpscript in de scratchpad, niet
  gecommit). Leg daarna elke case naast de assert van de oude test: de `stdout` moet die assert
  waarmaken. Een case die dat niet doet, is fout gebouwd, niet de assert.

- [ ] **Stap 2: schrijf de runner `tests/pack/test_kpi_fixtures.py`**

  Laadt beide scripts uit `INSTALL_ROOT / ".lighthouse" / "scripts"` (zoals de oude tests).
  Tests: `test_classify_rows_match_python` (per rij de Python-functie, zie de lijst bij
  *Interfaces*; roep `gate_re.cache_clear()`, `gate_suffix_re.cache_clear()` en
  `scout_skip_re.cache_clear()` aan na het zetten van de omgeving),
  `test_start_issue_cases_match_python` en `test_fix_pr_cases_match_python` (per case `main(args)`
  met `capsys`; vergelijk `rc`, `stdout`, `stderr` exact), en
  `test_every_old_test_has_a_home`: elke `def test_` uit de twee oude bestanden staat in een
  `from` of in de vier lijsten van stap 1.

- [ ] **Stap 3: draai de runner**

  Run: `uv run pytest -q tests/pack/test_kpi_fixtures.py`
  Verwacht: alles groen. Draai ook `uv run pytest -q tests/pack/test_start_issue_kpi.py tests/pack/test_fix_pr_kpi.py`: ongewijzigd groen.

- [ ] **Stap 4: commit**

  ```bash
  git add tests/pack/fixtures/kpi tests/pack/test_kpi_fixtures.py
  git commit -m "test(kpi): gevallen van de meetscripts naar gedeelde fixtures (#283)"
  ```

### Taak 2: transcriptlaag en Python-gelijke uitvoer

**Files:**
- Modify: `internal/config/pyfloat.go` (`formatFloat` → `FormatFloat`), `internal/config/config.go:723`, `internal/config/host.go:242`
- Create: `internal/kpi/doc.go`, `internal/kpi/transcript.go`, `internal/kpi/pyout.go`
- Test: `internal/kpi/transcript_test.go`, `internal/kpi/pyout_test.go`

**Interfaces:**
- Produces:
  - `type Turn struct { Prompt string; Records []map[string]any }`
  - `type toolUse struct { Name string; Input map[string]any; ID string }`
  - `func readSession(path string) ([]map[string]any, error)`
  - `func sessionFiles(dir string, withSiblings bool) ([]string, error)`
  - `func after(rec map[string]any, since *string) bool`
  - `func iterTurns(records []map[string]any) []Turn`
  - `func toolUses(rec map[string]any) []toolUse` (alleen `type: assistant`, niet sidechain)
  - `func contentBlocks(rec map[string]any) []map[string]any`
  - `func loadedSkills(t Turn) []string`
  - `func truthy(v any) bool`
  - `type object []field; type field struct { Key string; Value any }`; `type pyFloat float64`
  - `func encode(v any) string` (Python `json.dumps(indent=2, ensure_ascii=False)`, zonder slot-`\n`)
  - `func pyRound(x float64, n int) float64`
  - `func summary(values []int) object` (`_summary`), `func summaryInt(values []int) object`,
    `func summaryFloat(values []float64) object`
  - `func pyPath(p string) string`
  - `config.FormatFloat(f float64) string` (geëxporteerd)

- [ ] **Stap 1: schrijf de falende tests**

  - `TestEncode`: `object{{"a", 1}, {"b", object{}}, {"c", []string{}}, {"d", nil}, {"e", pyFloat(1)}, {"f", "x \t\"\\\x01é"}}`
    geeft precies
    `"{\n  \"a\": 1,\n  \"b\": {},\n  \"c\": [],\n  \"d\": null,\n  \"e\": 1.0,\n  \"f\": \"x \\t\\\"\\\\\\u0001é\"\n}"`.
    `\b` en `\f` worden `\b` en `\f`; andere tekens onder `0x20` `\u00xx` met kleine hex.
  - `TestPyRound`: `{2.25,1}→2.2`, `{0.125,2}→0.12`, `{2.675,2}→2.67`, `{0.35,1}→0.3`, `{2.0/3,3}→0.667`.
  - `TestSummary`: `summary(nil)` → `{"n": 0}`; `summary([]int{4,1,3})` → `n 3, min 1, median 3, max 4, mean 2.7`;
    `summary([]int{1,2,2,4})` → `median 2.0` (als `pyFloat`), `mean 2.2`.
    `summaryInt([]int{1,2,3,4})` → `median 2` (half-even van 2.5); `summaryInt(nil)` →
    `{"n": 0, "median": null, "max": null}`. `summaryFloat([]float64{1.25, 3.75})` → `median 2.5, max 3.8`.
  - `TestSessionFilesOrder`: map `P` met `b.jsonl`, `P--claude-worktrees-x` met `a.jsonl`,
    `P-ander` met `c.jsonl`; met siblings → `[P/b.jsonl, P--claude-worktrees-x/a.jsonl]`; zonder → `[P/b.jsonl]`.
  - `TestIterTurns`: een `tool_result`-record (`type: user`, alleen een `tool_result`-blok) start
    geen turn; `isSidechain: 1`, `isMeta: "x"` en `userType: "internal"` ook niet; records vóór de
    eerste prompt vallen weg.
  - `TestReadSessionSkipsBrokenLines`: een lege regel, een kapotte regel en ongeldige UTF-8 in
    een string; terug komen de twee geldige records, de string met `U+FFFD`.
  - `TestAfter`: `since nil` → true; record zonder `timestamp` → true; `"2026-08-23T23:59Z"` met
    since `"2026-08-24"` → false.
  - `TestPyPath`: `./gone/` → `gone`, `a//b` → `a/b`, `a/../b` → `a/../b`, `/` → `/`.

- [ ] **Stap 2: draai ze**

  Run: `go test ./internal/kpi/`
  Verwacht: FAIL (package bestaat niet).

- [ ] **Stap 3: implementeer**

  `encode` is een eigen recursieve schrijver, geen `json.MarshalIndent`: die escapet U+2028 en
  `<>&`. Een `pyFloat` print via `config.FormatFloat`. `readSession` splitst op `\n`, trimt,
  decodeert elke regel met `json.Unmarshal` naar `map[string]any` (een regel die geen object is,
  valt weg) en zet eerst ongeldige UTF-8 om. `doc.go` noemt de afwijkingen uit *Globale eisen*
  (ASCII `\b`, timestampvormen, niet-string `command`).

- [ ] **Stap 4: draai de tests**

  Run: `go test -race ./internal/kpi/ ./internal/config/`
  Verwacht: PASS.

- [ ] **Stap 5: commit**

  ```bash
  git add internal/kpi internal/config
  git commit -m "feat(kpi): transcriptlaag en Python-gelijke JSON-uitvoer (#283)"
  ```

### Taak 3: classificatie

**Files:**
- Create: `internal/kpi/classify.go`
- Test: `internal/kpi/classify_test.go`

**Interfaces:**
- Consumes: `config.Variants(key string) ([]string, error)`.
- Produces:
  - `func isPickup(text string) bool`, `func isFixRequest(text string) bool`
  - `func namesASkill(text string) bool`, `func hasIssueNumber(text string) bool`
  - `func isMutating(name string, input map[string]any) bool`, `func isDiagnostic(name string, input map[string]any) bool`
  - `type headingRes struct { gate, subgate, scoutSkip *regexp.Regexp }`; `func loadHeadingRes() (headingRes, error)`
  - `func (h headingRes) gate(text string) (int, bool)`, `func (h headingRes) subgate(text string) (string, bool)`,
    `func (h headingRes) skipReasonOK(text string) bool`

- [ ] **Stap 1: schrijf de falende test `TestClassifyParity`**

  Leest `../../tests/pack/fixtures/kpi/classify.json`, zet de omgeving zoals bij taak 1, en roept
  per `fn` de Go-functie aan. Vergelijkt met `want`; meldt `from` bij een verschil. Faalt als het
  bestand geen rijen heeft, of een `fn` onbekend is.

- [ ] **Stap 2: draai hem**

  Run: `go test -run TestClassifyParity ./internal/kpi/`
  Verwacht: FAIL (functies ontbreken).

- [ ] **Stap 3: implementeer**

  De patronen, met `⟨ws⟩`, `⟨nws⟩` en `⟨d⟩` uit *Globale eisen*:
  - pickup `(?i)`: de zeven takken van `PICKUP_RE` zonder `re.VERBOSE`-witruimte, elke `\s` →
    `⟨ws⟩`, `\d` → `⟨d⟩`, `\#` → `#`.
  - fix-verzoek `(?i)`: de drie takken van `FIXPR_RE`, idem.
  - mutating `(?i)`:
    `git⟨ws⟩+(rebase|merge|cherry-pick|add|commit|push|checkout|restore|worktree⟨ws⟩+add|reset|switch)\b|git⟨ws⟩+branch⟨ws⟩+(?:⟨nws⟩+⟨ws⟩+)*(?:-[dDmMcC]|--delete|--move|--copy)\b|git⟨ws⟩+stash⟨ws⟩+(?:pop|apply)\b|sed⟨ws⟩+-i|<<-?⟨ws⟩*(?-i:['"]?[A-Z_][A-Z0-9_]*['"]?)|gh⟨ws⟩+(run⟨ws⟩+rerun|pr⟨ws⟩+merge|pr⟨ws⟩+edit)\b`.
    De weggelaten `(?!<)` verandert niets: na `<<` moet `-`, witruimte, een quote, een hoofdletter
    of `_` volgen, nooit `<`.
  - diagnostic `(?i)`: de vier takken van `DIAGNOSTIC_BASH_RE`, idem.
  - `isMutating`: `Write`, `Edit` en `NotebookEdit` zijn altijd waar; anders alleen `Bash` met de regex.
    `isDiagnostic`: alleen `Bash`, niet mutating, wel diagnostic.
  - `namesASkill` en `hasIssueNumber` zijn hand-geschreven (RE2 heeft geen lookaround) en volgen
    de backtracking van Python. `namesASkill`: er is een `/` zonder `\w` of `/` ervoor, gevolgd
    door een naam `[a-z][a-z0-9-]*(:[a-z0-9-]+)?`, en **een** eindpunt van die naam (elk prefix
    telt, ook een korter dan de langste) waar geen `\w` of `/` volgt. `hasIssueNumber`: een start
    zonder `\w` of `.` ervoor, optioneel `#`, dan 2–4 `⟨d⟩`, en daarna geen `\w` of `.`.
  - Koppen: elke variant uit `config.Variants` op spaties gesplitst, elk deel door
    `regexp.QuoteMeta`, samen met `⟨ws⟩*`; de varianten met `|` in `(?:…)`. Dan
    `\b(?:…)⟨ws⟩*(?P<gate>[0-4])[abc]?\b`,
    `\b(?:…)⟨ws⟩*(?P<gate>[0-4])⟨ws⟩*(?P<sub>[abc])?\b` en
    `(?m)^(?:…)⟨ws⟩*—⟨ws⟩*(?P<reden>.+?)⟨ws⟩*$`. `skipReasonOK`: de reden, `strings.Trim(r, " .")`,
    heeft ≥ 20 runes.

- [ ] **Stap 4: draai de test**

  Run: `go test -race ./internal/kpi/`
  Verwacht: PASS.

- [ ] **Stap 5: commit**

  ```bash
  git add internal/kpi
  git commit -m "feat(kpi): prompt-, Bash- en kopherkenning gelijk aan de meetscripts (#283)"
  ```

### Taak 4: `lighthouse measure-start-issue-kpi`

**Files:**
- Create: `internal/kpi/startissue.go`, `internal/kpi/run.go`, `internal/cli/measure.go`
- Modify: `internal/cli/root.go:67-80` (registratie)
- Test: `internal/kpi/startissue_test.go`, `internal/kpi/parity_test.go`, `internal/cli/measure_test.go`

**Interfaces:**
- Consumes: alles uit taak 2 en 3; `config.RepoRoot()`, `config.Path()`, `config.Load`, `config.Lookup`.
- Produces:
  - `type Options struct { Metric string; Dir string; Since *string }` (`Dir == ""`: standaardmap)
  - `var StartIssueMetrics = []string{"leadin", "worktree-first", "mistrigger", "paths", "gate", "scout"}`
  - `func RunStartIssue(opts Options, out, errOut io.Writer) int`
  - `const ScoutToolCallBudget = 35`
  - `func defaultTranscriptDir() (string, error)`
  - `func resolveDir(opts Options, errOut io.Writer) (string, int)` (gedeeld met taak 5: rc 0 of 2, met melding)
  - `func newMeasureStartIssueKPICommand() *cobra.Command`

- [ ] **Stap 1: schrijf de falende tests**

  - `TestStartIssueParity` in `parity_test.go`: per map onder
    `../../tests/pack/fixtures/kpi/start-issue/` de `args` naar `Options` (eerste niet-vlag is de
    metric, `--dir`, `--since`), `RunStartIssue`, en `rc`/`stdout`/`stderr` exact vergelijken.
    Faalt als de map geen cases heeft.
  - `TestTranscriptDirFromRepoRoot`: `HOME=<tmp>`, `LIGHTHOUSE_REPO_ROOT=/srv/my_repo.d` →
    `<tmp>/.claude/projects/-srv-my-repo-d`; met één sessie erin geeft
    `RunStartIssue(Options{Metric: "mistrigger"})` rc 0.
  - `TestPathsSince`: een tijdelijke repo met `docs/superpowers/plans/old.md` (2026-01-01) en
    `docs/developer/planning/specs/new.md` (2026-09-01), `LIGHTHOUSE_TOML` met
    `[project] language = "en"`. Zonder `--since`: `"since": null`, `right_dir 1`,
    `superpowers_default_dir 1`, `share 0.5`. Met `2026-08-01`: `"since": "2026-08-01"`, `1`, `0`, `1.0`.
  - `TestPathsDirsFromConfig`: `[paths] specs = "s"`, `plans = "p/"`; commits `s/a.md` en
    `docs/developer/planning/specs/b.md` → `right_dir 1`.
  - `TestPathsGitFailure`: repo zonder commits → rc 2, stderr begint met `measure: git log failed: `.
  - `TestFullTableOrder`: geen metric, met tijdelijke repo → de zes sleutels in de volgorde van
    `StartIssueMetrics`.
  - `TestMeasureStartIssueCommand` in `internal/cli/measure_test.go`: via de root,
    `measure-start-issue-kpi bogus` → rc 2 en stderr
    `measure: unknown metric "bogus" (choose from leadin, worktree-first, mistrigger, paths, gate, scout)\n`;
    `measure-start-issue-kpi leadin --dir <lege tmp>` → rc 0.

- [ ] **Stap 2: draai ze**

  Run: `go test ./internal/kpi/ ./internal/cli/`
  Verwacht: FAIL.

- [ ] **Stap 3: implementeer**

  Port van `measure_leadin`, `measure_worktree_first`, `measure_mistrigger`, `measure_gate`,
  `measure_scout`, `measure_paths` en de hulpfuncties (`count_leadin`, `first_write_path`,
  `text_before_route`, `_scout_calls`, `_finished`, `_run_stats`, `_seconds_between`,
  `_tool_use_count`), regel voor regel naar `pack/core/scripts/measure_start_issue_kpi.py`. De
  sleutels en hun volgorde komen uit die Python. Constanten: de skillnamen, `ROUTE_TOOLS`,
  `WRITE_TOOLS` (`Write`, `Edit`), `WORKTREE_MARKER`, `SCOUT_MIN_REASON = 20`,
  `ScoutToolCallBudget = 35`, `SCOUT_GATES`. `_seconds_between` rekent in microseconden en deelt
  door `1e6`. `totalToolUseCount` telt alleen als het JSON-getal een geheel getal is. `paths`
  draait `git -C <root> log --diff-filter=A --name-only --pretty=format: [--since <since>]`
  (`--since` alleen bij een niet-lege `since`) en leest `paths.specs` en `paths.plans` via
  `config.Lookup`, met `/` erachter na `strings.TrimRight(v, "/")`. De twee sleutels staan
  letterlijk als string in `startissue.go`: `tests/test_site_config.py` vindt zo de lezer (taak 6). Rijen rekenen eerst allemaal;
  pas dan printen. Een fout in een rij geeft rc 2 en geen stdout. `RunStartIssue` controleert
  eerst de metric, dan de map, dan rekent hij. Het cobra-commando (`Args: cobra.ArbitraryArgs`,
  vlaggen `--dir` en `--since`, `Short` in het Engels) zet `Since` alleen als de vlag gezet is,
  en geeft de rc terug via `exitError{code, errSilent}` zoals `internal/cli/headings.go`.

- [ ] **Stap 4: draai de tests**

  Run: `go test -race ./internal/kpi/ ./internal/cli/`
  Verwacht: PASS.

- [ ] **Stap 5: commit**

  ```bash
  git add internal/kpi internal/cli
  git commit -m "feat(cli): lighthouse measure-start-issue-kpi (#283)"
  ```

### Taak 5: `lighthouse measure-fix-pr-kpi` en de koppelingen met persona en skill

**Files:**
- Create: `internal/kpi/fixpr.go`
- Modify: `internal/cli/measure.go`, `internal/cli/root.go`
- Test: `internal/kpi/fixpr_test.go`, `internal/kpi/parity_test.go`, `internal/cli/measure_test.go`

**Interfaces:**
- Consumes: taak 2–4 (`Options`, `resolveDir`, `summary`, `toolUses`, `isMutating`, `isDiagnostic`, `isFixRequest`).
- Produces:
  - `var FixPRMetrics = []string{"leadin", "diagnose", "mistrigger", "scope"}`
  - `func RunFixPR(opts Options, out, errOut io.Writer) int`
  - `func newMeasureFixPRKPICommand() *cobra.Command`

- [ ] **Stap 1: schrijf de falende tests**

  - `TestFixPRParity`: zoals `TestStartIssueParity`, over `fixtures/kpi/fix-pr/`. Faalt als de
    map geen cases heeft.
  - `TestFixPRReadsNoWorktreeSiblings`: een fix-turn alleen in `<dir>--claude-worktrees-x` →
    `leadin` geeft `{"n": 0, "without_mutation": 0}`.
  - `TestScoutBudgetMatchesPersona` (vervangt `test_prompt_caps_the_number_of_searches`): de
    kleine letters van `../../pack/core/agents/issue-impact-scout.md` bevatten
    `fmt.Sprintf("at most %d searches", ScoutToolCallBudget)`.
  - `TestFixPRDiagnosisBlockIsReadOnly` (vervangt `test_the_diagnosis_block_is_read_only`): voor
    `../../pack/core/skills/fix-pr/{claude,agents}/SKILL.md` het enige ```` ```bash ````-blok onder
    `## 1. Diagnosis in one block` (tot de volgende `\n## `); precies één blok, en
    `mutatingRe` vindt er niets in.
  - `TestMeasureCommandsRun` in `internal/cli/measure_test.go`: `measure-fix-pr-kpi scope --dir <lege tmp>`
    → rc 0 en stdout bevat `"past_the_boundary": 0`.

- [ ] **Stap 2: draai ze**

  Run: `go test ./internal/kpi/ ./internal/cli/`
  Verwacht: FAIL.

- [ ] **Stap 3: implementeer**

  Port van `measure_fix_pr_kpi.py`: `count_leadin` (slaat alleen `Skill{skill: lighthouse-fix-pr}`
  over), `count_diagnostics`, `overruns_scope` (`SCOPE_SKILLS`: `lighthouse-close-issue`,
  `caveman:caveman-review`), de vier rijen. `leadin` is `summary(counts)` met
  `without_mutation` erachter. `WRITE_TOOLS` hier is `Write`, `Edit`, `NotebookEdit`.
  `sessionFiles(dir, false)`. Registreer het commando naast dat van taak 4.

- [ ] **Stap 4: draai de tests**

  Run: `go test -race ./internal/kpi/ ./internal/cli/`
  Verwacht: PASS.

- [ ] **Stap 5: commit**

  ```bash
  git add internal/kpi internal/cli
  git commit -m "feat(cli): lighthouse measure-fix-pr-kpi (#283)"
  ```

### Taak 6: skills naar de subcommando's, Python weg

**Files:**
- Modify: `pack/core/skills/start-issue/claude/SKILL.md:273-278`, `pack/core/skills/start-issue/agents/SKILL.md:280-285`
- Modify: `pack/core/skills/fix-pr/claude/SKILL.md:250-253`, `pack/core/skills/fix-pr/agents/SKILL.md:250-253`
- Modify: `pack/core/skills/route-issue/SKILL.md:70`
- Modify: `tests/pack/test_skills_issue_flow.py:248-252,260`, `tests/pack/test_fix_pr_skill.py:27-33,72-75,136-140`, `tests/pack/test_issue_impact_scout.py:159-175`
- Delete: `pack/core/scripts/measure_start_issue_kpi.py`, `pack/core/scripts/measure_fix_pr_kpi.py`,
  `tests/pack/test_start_issue_kpi.py`, `tests/pack/test_fix_pr_kpi.py`, `tests/pack/test_kpi_fixtures.py`
- Modify: `pack/SOURCES.toml:215-223`, `pack/core/requires.toml` (git, python3, tomllib),
  `internal/packs/core_test.go:128-129`, `docs/site/data/config.json:462,490`
- Create: `changelog.d/issue-283-kpi-scripts-go.md`
- Install: `go run ./cmd/lighthouse init --upgrade` (raakt `.claude/skills`, `.agents/skills`, `.lighthouse/scripts`, `.lighthouse/manifest.toml`)

**Interfaces:**
- Consumes: de twee subcommando's uit taak 4 en 5.

- [ ] **Stap 1: pas de tekst-tests eerst aan (falen)**

  - `test_start_issue_keeps_the_measurements_not_the_numbers`: verwacht
    `f"lighthouse measure-start-issue-kpi {sub}"` en verder `"measure_start_issue_kpi" not in body`.
    Het commentaar op regel 260 noemt `lighthouse measure-start-issue-kpi`.
  - `test_the_kpi_table_names_the_measuring_script`: verwacht `f"lighthouse measure-fix-pr-kpi {metric}"`
    en `"measure_fix_pr_kpi" not in text`.
  - Nieuw in `test_skills_issue_flow.py`: `test_route_issue_names_the_subcommand` — de
    route-issue-tekst bevat `lighthouse measure-start-issue-kpi` en niet `measure_start_issue_kpi.py`.
  - Haal weg: de `_SPEC`/`kpi`-import en `test_the_diagnosis_block_is_read_only` uit
    `test_fix_pr_skill.py`; `_kpi()` en `test_prompt_caps_the_number_of_searches` uit
    `test_issue_impact_scout.py` (taak 5 heeft de vervangers).

  Run: `uv run pytest -q tests/pack/test_skills_issue_flow.py tests/pack/test_fix_pr_skill.py tests/pack/test_issue_impact_scout.py`
  Verwacht: FAIL op de drie tekst-tests.

- [ ] **Stap 2: pas de skilltekst aan**

  In elke KPI-tabel wordt `python3 .lighthouse/scripts/measure_start_issue_kpi.py <metric>`
  `lighthouse measure-start-issue-kpi <metric>`, en idem voor `measure-fix-pr-kpi`. In
  `route-issue` wordt de zin: ``The form is a contract. `lighthouse measure-start-issue-kpi` counts the gate line and``.

  Run: dezelfde pytest-regel als in stap 1. Verwacht: PASS.
  Run: `go run ./cmd/lighthouse init --upgrade`. Verwacht: de zes geïnstalleerde SKILL.md's
  gewijzigd.

  ```bash
  git add pack/core/skills tests/pack .claude/skills .agents/skills .lighthouse/manifest.toml
  git commit -m "feat(skills): start-issue, fix-pr en route-issue roepen de meetsubcommando's aan (#283)"
  ```

- [ ] **Stap 3: haal de Python en zijn sporen weg**

  Verwijder de vijf bestanden. Haal de twee `[[file]]`-blokken uit `pack/SOURCES.toml`, en
  `measure_start_issue_kpi` en `measure_fix_pr_kpi` uit de lijsten `git`, `python3` en `tomllib`
  in `pack/core/requires.toml`. Haal de twee regels uit `internal/packs/core_test.go`. In
  `docs/site/data/config.json` gaat `pack/core/scripts/measure_start_issue_kpi.py` uit de
  `readers` van `paths.plans` en `paths.specs`; `internal/kpi/startissue.go` komt erbij,
  alfabetisch (`tests/test_site_config.py` eist het; het bestand noemt `paths.specs` en
  `paths.plans` letterlijk).

- [ ] **Stap 4: fragment en installatie**

  `changelog.d/issue-283-kpi-scripts-go.md`:

  ```markdown
  ---
  type: added
  ---
  `lighthouse measure-start-issue-kpi` and `lighthouse measure-fix-pr-kpi` replace `.lighthouse/scripts/measure_start_issue_kpi.py` and `measure_fix_pr_kpi.py`, with the same metrics, flags and JSON output. `lighthouse init --upgrade` removes the two scripts; update the `lighthouse` binary on your PATH before you run the measurements in the `start-issue` and `fix-pr` skills.
  ```

  Run: `go run ./cmd/lighthouse init --upgrade && go run ./cmd/lighthouse init --upgrade`
  Verwacht: de eerste run meldt de twee verwijderde scripts; de tweede `No changes.`
  Daarna: `test ! -e .lighthouse/scripts/measure_start_issue_kpi.py && test ! -e .lighthouse/scripts/measure_fix_pr_kpi.py && ! grep -rn 'measure_\(start_issue\|fix_pr\)_kpi' .claude .agents .lighthouse pack internal tests docs/site`
  Verwacht: rc 0, geen uitvoer.

- [ ] **Stap 5: volledige verificatie**

  Run: `go test -race ./... && uv run pytest -q && git diff --stat -- pack/core/lib .lighthouse/lib`
  Verwacht: alles groen; de laatste opdracht print niets (bevroren lib ongewijzigd).

- [ ] **Stap 6: commit**

  ```bash
  git add -A pack tests internal/packs docs/site changelog.d .lighthouse .claude/skills .agents/skills
  git commit -m "refactor(pack): meetscripts in Python weg (#283)"
  ```
