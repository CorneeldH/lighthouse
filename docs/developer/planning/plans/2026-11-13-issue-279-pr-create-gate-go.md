# pr-create-gate in Go, met pr-body-gate en evidence — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** `pr-create-gate` draait als `lighthouse hook pr-create-gate` achter `run.sh`. `lighthouse pr-body-gate <cwd>` en `lighthouse evidence check|run` vervangen de Python-aanroepen in `write-pr` en `close-issue`. Ze geven hetzelfde oordeel en dezelfde tekst als nu. `pr-create-gate.sh` en `evidence.py` verdwijnen. `pr_body_gate.py` blijft bevroren staan voor `worktree_audit.py`.

**Architectuur:** Twee nieuwe pakketten. `internal/prbody` is de tweeling van `pr_body_gate.py`: vormen, Evidence-tabel, AC-telling, `gh pr create` vinden, scope en `Main`. `internal/evidence` is de tweeling van `evidence.py`: shlex-splitsing, allowlist, runner met output-cap en `run`. De hook komt in `internal/hooks/prcreategate.go` en roept `prbody` in-process aan. De subcommando's komen in `internal/cli`. Eerst gaan de inline pytest-gevallen naar gedeelde fixtures, en die draaien eerst tegen Python. Daarna lezen de Go-tests dezelfde fixtures.

**Tech stack:** Go (stdlib, `internal/gitinvocations`, `internal/repoidentity`, `internal/config`, `internal/hook`, cobra), POSIX `sh` (`run.sh`, ongewijzigd), pytest.

**Spec:** [`2026-11-07-zonder-python-design.md`](../specs/2026-11-07-zonder-python-design.md), stap 6 van *Opdeling*, met §1, §2, §4, §9, §10 en *Anti-doelen*. Issue: #279. Gestapeld op #278 (PR #326); dat plan is het patroon: [`2026-11-09-issue-278-bash-write-check-go.md`](2026-11-09-issue-278-bash-write-check-go.md).

## Globale eisen

- Elke hook en elk subcommando geeft exact hetzelfde oordeel, dezelfde exitcode en dezelfde tekst als zijn Python-voorganger, op dezelfde invoer (spec §2). Kopieer elke berichttekst letterlijk uit `pack/core/lib/pr_body_gate.py`, `pack/core/lib/evidence.py` en `pack/core/hooks/pr-create-gate.sh`.
- Exitcodes van `lighthouse pr-body-gate <cwd>` (commando op stdin): 0 (stdout: regels), 2 (stderr: `BLOCKED_MESSAGE`), 3 (open quote), 1 (configfout, één regel `pr-create-gate: <fout>` op stderr), 64 (verkeerd aantal argumenten, `usage: pr_body_gate.py <cwd>  (command on stdin)` wordt `usage: lighthouse pr-body-gate <cwd>  (command on stdin)`).
- Exitcodes van `lighthouse evidence`: `check` 0 of 1 (reden op stdout); `run` 0, 1 (FAILS/INVALID), 2 (geen blok), 3 (`COULD NOT CHECK`), 4 (UNMEASURABLE). Vlaggen gelijk: `run --issue N --body-file PATH --cwd DIR [--before-pr]`.
- De totale deadline van de gate is 12 s (`DEADLINE_S`) en blijft onder de hook-timeout van 15 s in `pack/core/hooks/hooks.json`. `gh` krijgt `min(5 s, rest)`, `git` `min(10 s, rest)` (AC 3).
- Bevroren, byte voor byte gelijk aan `origin/main`: `pack/core/lib/pr_body_gate.py` en zijn geïnstalleerde kopie, plus alles wat #278 al bevroor (`git_invocations.py`, `repo_identity.py`, `bash_writes.py`, `deadline.py`, `filelock.py`).
- Go volgt `/lighthouse-go-standard`, tests volgen `/lighthouse-test-standard`. Alles compileert ook met `GOOS=windows` en `GOOS=darwin`.
- Commits volgen Conventional Commits, met de berichten in het Nederlands.
- Een wijziging onder `pack/` → `go run ./cmd/lighthouse init --upgrade`, en het resultaat gaat in dezelfde commit mee.

## Besluiten die dit plan neemt

1. **Welke gevallen naar fixtures gaan (AC 2).** De gevallen die data zijn, zoals in stap 5 (Besluit 1).
   - Uit `tests/pack/test_pr_body_gate.py`: `closes_targets` (regels 67–157), `refs_issues`, `refs_targets` en `no_issue` (164–203), `evidence_rows` (206–266), `count_acs` (269–292), `check_body` met een vaste AC-telling (294–383), `find_pr_creates` met de bronnen van de body (448–557), en `_normalize_repo` met `_scope_repo` (583–640).
   - Uit `tests/pack/test_evidence.py`: `ALLOWED`, `REFUSED`, de shell-operatoren, de pipe-escapes (21–163), `expected` (166–182) en `outcome_of` (516–533).
   - Wat over processen, `gh`, git-repo's of tijd gaat (`fetch_ac_count`, `in_scope` met echte repo's, `run_row`, de CLI), is geen data. Daarvoor krijgt Go een tweelingtest per Python-test, met dezelfde asserts.
2. **`test_pr_body_gate.py` blijft staan**, want zijn module blijft bevroren. Hij leest de verhuisde gevallen voortaan uit de fixtures.
3. **`test_evidence.py` wordt een black-box-test van `lighthouse evidence`.** `evidence.py` verdwijnt, dus `load_hook_lib("evidence")` kan niet meer.
   - De gevallen uit Besluit 1 lopen via `lighthouse evidence check '<cel>'`. Toegestaan: exit 0, zonder uitvoer. Geweigerd: exit 1, en de reden op stdout is gelijk aan `expect`.
   - `expected` en `outcome_of` hebben geen CLI. Hun fixtures lezen alleen de Go-tests. In taak 1 draaien ze nog één keer tegen Python, als bewijs dat de fixture klopt.
   - De `run_row`-tests (184–289) worden Go-tweelingen in `internal/evidence`. `test_cli_*`, `test_pytest_path_*`, `test_run_accepts_a_refs_body` en `test_outcomes_follow_language` blijven pytest en roepen `lighthouse evidence` aan. Hun asserts blijven gelijk.
4. **De allowlist-constanten** die `test_allowlist_table_covers_the_code` uit `evidence.py` leest, komen in `tests/pack/fixtures/evidence/allowlist.json` (sleutel per constante, waarde een gesorteerde lijst). `TestAllowlistMatchesFixture` (Go) eist dat de Go-sets gelijk zijn aan dat bestand. De skilltest leest het bestand in plaats van de module. Zo blijft de keten skilltabel ↔ code gesloten.
5. **Tests waarvan het onderwerp verdwijnt.** Er wordt geen verwachting aangepast; deze tests gaan weg:
   - in `test_pr_create_gate.py`: `test_without_tooling_blocks` (beide parameters). De Go-hook heeft geen `jq` en geen `python3` nodig. "Zonder binary blokkeert hij" dekt `test_run_sh.py` al.
   - in `test_pr_create_gate.py`: `test_parser_with_unexpected_rc_blocks` (een nep-`python3`). Een Go-tweeling met dezelfde bedoeling komt ervoor in de plaats (Besluit 6).
   - Uit `BASE_TOOLS` van die test gaan `jq`, `python3` en `dirname` weg; `RUN_SH_TOOLS` komt erbij, zoals in `test_main_commit_gate.py`.
6. **Een paniek in de hook** wordt binnen de hook opgevangen. Hij wordt `pr-create-gate: COULD NOT CHECK — the body parser failed (panic: <waarde>)`, met de rest van de `block`-tekst en exit 2. Zonder die opvang maakt `hook.invoke` er exit 3 van, en geeft `run.sh` een andere tekst (zoals #278, Besluit 5).
7. **De hook roept `prbody` in-process aan**, niet als subproces. De tak "lib ontbreekt" (`pr-create-gate.sh:67-79`) verdwijnt: de lib zit in de binary. De andere uitkomsten van de bash-wrapper komen één op één in de Go-hook:
   - een payload die geen JSON is: `COULD NOT CHECK — the payload is not valid JSON.`;
   - een leeg commando: exit 0, zonder uitvoer;
   - exit 0 met regels: JSON met `systemMessage` en `hookSpecificOutput.{hookEventName: "PreToolUse", additionalContext}`;
   - exit 2: de stderr van prbody ongewijzigd;
   - exit 3: de tweetrapsregel (`gh` en `pr` in de ruwe tekst);
   - elke andere code: `the body parser failed (rc N)`. Bij een configfout staat de regel `pr-create-gate: <fout>` (rc 1) eerst op stderr, net als nu.
8. **De registratie** wordt `${CLAUDE_PLUGIN_ROOT}/hooks/run.sh pr-create-gate`, met `"timeout": 15`. `blocking` blijft zoals het is.
9. **`requires.toml`.** `pr-create-gate`, `lighthouse_lib.evidence` en hun vermeldingen gaan van de regels `python3`, `jq` en `tomllib`. `lighthouse_lib.evidence` gaat ook van `gh` en `git` af. `lighthouse_lib.pr_body_gate` blijft overal staan, want hij is bevroren. `write-pr` en `close-issue` houden `python3`: ze roepen nog `roborev_gate.py`, `board_start.py` en `worktree_audit.py` aan (stap 8 en 9).
10. **De runner van `evidence`** krijgt een eigen `runCapped` met één pipe, een cap van 4 MiB en een deadline. `deadline.RunCapture` past niet: die schrijft naar een tijdelijk bestand zonder cap, dus `cat /dev/zero` vult de schijf tot de timeout. Voor de procesgroep exporteert `internal/deadline` zijn helpers als `deadline.InOwnGroup(*exec.Cmd)` en `deadline.KillGroup(*exec.Cmd)` (nu `inOwnGroup` en `hardKill`). Na EOF of een timeout gaat SIGKILL naar de groep, zoals `_stop` (`evidence.py:422-431`). De omgeving krijgt `_SUB_ENV` erbij, stdin is `/dev/null`.
11. **`shlex` krijgt een kleine port**, `evidence.splitPosix(s string) ([]string, error)`. Hij doet wat `shlex.shlex(s, posix=True, punctuation_chars=True)` doet, met `whitespace_split=True` en `commenters=""`. Dat betekent: `'…'` letterlijk; `"…"` met `\` alleen vóór `"` en `\`; `\` buiten quotes escapet het volgende teken; een reeks tekens uit `();<>|&` is een eigen token. De twee fouten krijgen de tekst van Python: `No closing quotation` en `No escaped character`. Het oordeel van `split_command` hangt ervan af, dus de fixtures dekken elk van deze regels (Review-focus 1).

## Review-focus

Deze vijf inputs dekt geen bestaande test, en ze bijten het eerst bij deze port. Elk krijgt een test in de taak die de code bezit:

1. **De shlex-randen.** Denk aan `grep -e 'a b' "c\"d" e\ f`, een backslash aan het eind, `a>b` zonder spaties, `||` en een lege string `''`. Python geeft `['grep', '-e', 'a b', 'c"d', 'e f']`, fout `No escaped character`, `['a', '>', 'b']`, `['||']` en `['']`. Ze worden fixtures `split_*` in `tests/pack/fixtures/evidence/cases.json` (taak 1), en Go leest ze (taak 3).
2. **Een `gh` die blijft hangen** bij de AC-telling. De hook moet binnen de deadline met de notitie `timeout after 5 s` doorlaten, niet pas bij de hook-timeout. Test `test_slow_gh_passes_loudly_within_the_deadline` (taak 4): een stub die 30 s slaapt, en de hook is binnen 10 s klaar.
3. **Een body met CRLF en een niet-UTF-8-byte** in `--body-file`. Python leest met `errors="replace"` en zet `\r\n` om. Go moet hetzelfde oordeel geven: ongeldige bytes worden U+FFFD. Fixture `body-file-crlf-and-invalid-utf8` (taak 1, `check_body`), plus `TestCheckCreateReadsInvalidUTF8` (taak 2).
4. **Een Evidence-rij met een kind dat op de achtergrond blijft draaien en de pipe openhoudt** (`sh` staat niet op de allowlist, dus via een stub `grep` die `sleep 60 &` start). Python leest tot EOF of de deadline en doodt dan de groep. Go moet bij de timeout `timeout after N s` geven en de groep doden, zonder te blijven hangen. Test `TestRunRowKillsTheGroupOnTimeout` (taak 3).
5. **Een hook-payload van een vreemde vorm:** een JSON-array, `null`, `tool_input` dat een string is, en een `command` dat een getal is. De bash-wrapper (`jq -r '.tool_input.command // empty'`) geeft bij een array en `null` exit 0 (leeg). Bij een string-`tool_input` geeft jq een fout, dus `COULD NOT CHECK — the payload is not valid JSON.`. Een getal wordt een tekst (`5`), en zonder `gh` is dat exit 0. Test `test_payload_shapes`: eerst groen tegen de bash-hook (taak 4, stap 1), daarna tegen Go. De oordelen komen uit die eerste run, niet uit deze lijst.

---

### Taak 1: Gedeelde fixtures voor `pr_body_gate` en `evidence`, gedraaid tegen Python

**Bestanden:**
- Maken: `tests/pack/fixtures/pr_body_gate/cases.json`, `tests/pack/fixtures/evidence/cases.json`, `tests/pack/fixtures/evidence/allowlist.json`
- Maken: `tests/pack/test_pr_body_gate_fixtures.py`, `tests/pack/test_evidence_fixtures.py`
- Wijzigen: `tests/pack/test_pr_body_gate.py`, `tests/pack/test_evidence.py` (de gevallen die verhuizen, gaan eruit)

**Interfaces:**
- Levert het fixtureformaat dat taak 2 en 3 in Go lezen. Een JSON-array van objecten met `name`, `kind`, `expect`, en per soort de invoer:
  - `closes`, `refs`, `refs_targets`, `no_issue`: `body`, en optioneel `repo` (tekst of `null`, wordt `forge.repo`) → `expect`: lijst getallen, of een bool.
  - `evidence_rows`: `body`, `nr` → `expect`: lijst `[ac, command, result]`, of `{"reason": "<tekst>"}`.
  - `count_acs`: `body` → getal of `null`.
  - `check_body`: `body`, `counts` (map nr → `[count|null, cause]`), `repo` → `{"blockers": [...], "notices": [...]}`.
  - `find`: `command`, `cwd` (met `{root}`) → lijst `{cwd, repo, body, body_file, problem, repo_problem}`, of `"unparseable"`.
  - `normalize_repo`: `value` → tekst of `null`. `scope_repo`: `value`, `repo` → `[beslissing, reden]`.
  - In `evidence/cases.json`: `split` (`cell` → lijst, of `{"reason": …}`), `check` (`cell` → `null` of reden; dit is `split_command` plus `check`, zoals `_cmd_check`), `expected` (`cell` → `[fragment, after_pr]`), `outcome_of` (`cell` → sleutel of `null`). `check` heeft ook een optionele `evidence_test_dir` voor de pytest-gevallen.
- `allowlist.json`: `{"GREP_FLAGS": [...], "GREP_VAL": [...], …, "GIT_SUBS": [...]}`, met elke constante uit `evidence.py:49-80` die `test_allowlist_table_covers_the_code` gebruikt.

- [ ] **Stap 1: Zet de gevallen van Besluit 1 om naar de twee `cases.json`.** Vul elke `expect` met wat de Python teruggeeft. Voeg de vijf `split_*`-gevallen van Review-focus 1 toe en `body-file-crlf-and-invalid-utf8` van Review-focus 3; ook die vul je met de uitvoer van Python.
- [ ] **Stap 2: Schrijf `allowlist.json`** met een eenmalig script over `load_hook_lib("evidence")`. Het script zelf commit je niet.
- [ ] **Stap 3: Schrijf de twee runners.** `test_fixture_case(case, tmp_path, monkeypatch)`, geparametriseerd per `name`, tegen `load_hook_lib`. `repo` gaat via een `lighthouse.toml` in `tmp_path` met `LIGHTHOUSE_REPO_ROOT`, zoals `test_without_forge_repo` doet. `test_evidence_fixtures.py` draait in deze taak nog tegen `evidence.py`. Taak 5 zet het `check`-deel om naar `lighthouse evidence check`.
- [ ] **Stap 4: Draai de tests.**
  Run: `uv run pytest -q tests/pack/test_pr_body_gate_fixtures.py tests/pack/test_evidence_fixtures.py tests/pack/test_pr_body_gate.py tests/pack/test_evidence.py tests/pack/test_pr_create_gate.py`
  Verwacht: PASS.
- [ ] **Stap 5: Mutatiecheck.** Zet in `pr_body_gate.py` `MIN_REASON` tijdelijk op 9. Laat in `evidence.py` de `%G`-check in `_check_git` weg. Daarna moet bij elk ten minste één fixture rood worden (`refs-reason-too-short`, `git log --format=%G?`). Zet het terug met `git checkout pack/core/lib/`.
- [ ] **Stap 6: Commit.** `test(pr-body-gate): gevallen naar gedeelde fixtures`

### Taak 2: `internal/prbody` — de tweeling van `pr_body_gate.py`

**Bestanden:**
- Maken: `internal/prbody/form.go` (`strip_comments` … `count_acs`, `check_body`; `pr_body_gate.py:118-339`)
- Maken: `internal/prbody/find.go` (`_option` … `find_pr_creates`; regels 341–515)
- Maken: `internal/prbody/scope.go` (`_common_dir`, `_normalize_repo`, `_scope_repo`, `_without_repo`, `in_scope`; regels 518–638)
- Maken: `internal/prbody/gate.go` (`fetch_ac_count`, `_forms`, `_check_create`, `Main`; regels 192–220, 641–737)
- Maken: `internal/prbody/fixtures_test.go`, `internal/prbody/gate_test.go`
- Wijzigen: `internal/deadline/group_unix.go`, `group_other.go` (de export van Besluit 10)

**Interfaces:**
- Gebruikt: `gitinvocations.Walk`, `Step` (`Words`, `Literal`, `Tildes`, `Idx`, `Here`), `Resolve`, `Basename`, `ExistingAncestor`, `ErrUnparseable`; `repoidentity.SameOrigin(ctx, projectDir, root)`; `config.Load`, `config.Path`, `config.RepoRoot`, `config.Variants("acceptance_criteria")`.
- Levert, in package `prbody`:
  - `const DeadlineS = 12`, `ACTimeoutS = 5`, `GitTimeoutS = 10`, `MinReason = 10`
  - `type Row struct{ AC, Command, Result string }`
  - `func StripComments(body string) string`, `func ClosesTargets(body, repo string) []int` (`repo == ""`: geen `forge.repo`), `func RefsIssues(body string) []int`, `func RefsTargets(body string) []int`, `func NoIssue(body string) bool`, `func HasEvidenceBlock(body string, nr int) bool`
  - `func EvidenceRows(body string, nr int) ([]Row, string)`. De tweede waarde is de reden; `""` betekent geldig.
  - `func CountACs(issueBody string) (int, bool)`
  - `type Verdict struct{ Blockers, Notices []string }`, `func CheckBody(body, repo string, acCount func(nr int) (int, bool, string)) Verdict`
  - `type PrCreate struct{ Cwd, Repo, Body, BodyFile *string; Problem, RepoProblem string }`, `func FindPrCreates(command, cwd string) ([]PrCreate, error)`. Een open quote geeft `gitinvocations.ErrUnparseable` terug.
  - `func NormalizeRepo(value string) (string, bool)`, `func InScope(ctx context.Context, pc PrCreate, projectDir, repo string) (decision, reason string)`, met `decision` ∈ `"check"`, `"skip"`, `"block"`. Dit zijn de Engelse namen voor `toetsen`, `overslaan` en `blokkeren`; ze zijn intern.
  - `type Env struct{ GH func(ctx context.Context, argv ...string) (stdout string, code int, err error); ProcessGHRepo string }`. In productie start `GH` het commando `gh`; een test zet er een nep in.
  - `func Main(ctx context.Context, cwd string, stdin io.Reader, stdout, stderr io.Writer, env Env) int`. Zet zelf de deadline van 12 s op `ctx`. Leest `CLAUDE_PROJECT_DIR`, anders `config.RepoRoot()`.
- Regexen: Go's RE2 kent geen lookbehind en geen lookahead. `(?<![\w/])` en `(?![\w/])` worden een controle op het teken vóór en na de match in code. De volgorde van matches, en dus de uitkomst, blijft gelijk aan Python. Een match die door die controle valt, mag een latere overlappende match niet blokkeren. Zoek daarom verder vanaf de positie na het begin van de afgewezen match, niet vanaf het einde ervan.

- [ ] **Stap 1: Schrijf de tests.**
  - `TestFixtures` leest `../../tests/pack/fixtures/pr_body_gate/cases.json`.
  - Tweelingen van `test_fetch_ac_count_*` (397–445) gebruiken een nep-`Env.GH`.
  - Tweelingen van `test_scope_own_repo` … `test_scope_other_git_error_blocks` (573–700) bouwen echte repo's in `t.TempDir()` met `internal/fixturesetup`. Daarbij horen ook de forge-worktree met dezelfde `origin` en de case `common dirs and remotes`.
  - `TestCheckCreateReadsInvalidUTF8` (Review-focus 3).
  - `TestDeadlineUnderHookTimeout` leest `../../pack/core/hooks/hooks.json` en eist voor de `run.sh pr-create-gate`-registratie `DeadlineS < timeout` (AC 3).
  Run: `go test ./internal/prbody/` · Verwacht: FAIL (compileert niet).
- [ ] **Stap 2: Port de vier bestanden, functie voor functie.** De berichtteksten zijn letterlijke kopieën. `os.path.normpath` wordt `filepath.Clean`, `os.path.realpath` wordt `filepath.EvalSymlinks`. `git` draait met `LC_ALL=C` en `--path-format=absolute --git-common-dir`. Een body uit een bestand gaat door `strings.ToValidUTF8(s, "�")`.
- [ ] **Stap 3: Draai de tests.** Run: `go test -race ./internal/prbody/ ./internal/deadline/ && GOOS=windows go vet ./internal/prbody/ ./internal/deadline/` · Verwacht: PASS.
- [ ] **Stap 4: Mutatiecheck.** Laat tijdelijk de controle "teken na de match is geen `\w` of `/`" in `ClosesTargets` weg. Daarna moet `closes-number-is-not-truncated` (of een ander `closes`-geval) rood worden. Zet het terug.
- [ ] **Stap 5: Commit.** `feat(prbody): pr_body_gate in Go`

### Taak 3: `internal/evidence` — allowlist, splitsing en runner

**Bestanden:**
- Maken: `internal/evidence/split.go` (`splitPosix`, `SplitCommand`), `internal/evidence/allowlist.go` (`_parse` … `check`; `evidence.py:46-360`), `internal/evidence/run.go` (`expected`, `outcome_of`, `runCapped`, `RunRow`, `Run`, `Check`; regels 363–600)
- Maken: `internal/evidence/fixtures_test.go`, `internal/evidence/run_test.go`

**Interfaces:**
- Gebruikt: `prbody.EvidenceRows`, `prbody.Row` (taak 2); `deadline.InOwnGroup`, `deadline.KillGroup` (taak 2); `config.Load`, `config.Lookup(c, "test_gate.evidence_test_dir")`, `config.Heading`, `config.Variants`.
- Levert, in package `evidence`:
  - `const Holds = "evidence_holds"`, `Fails`, `Invalid`, `Unmeasurable`, `AfterPRUnmeasured` (de sleutels van `headings.toml`), `MaxOutput = 4 << 20`, `GHProbeTimeoutS = 10`
  - `var Allowlist = map[string][]string{…}`, met de namen uit `allowlist.json` (Besluit 4)
  - `func SplitCommand(cell string) ([]string, string)`. De tweede waarde is de reden.
  - `func Check(argv []string, testDir string) string`. `""` betekent toegestaan.
  - `func Expected(result string) (fragment string, afterPR bool)`, `func OutcomeOf(cell string) string`
  - `func RunRow(row prbody.Row, cwd string, ghOK bool, timeout time.Duration) (outcome, detail string)`
  - `func Run(issue int, bodyFile, cwd string, beforePR bool, stdout io.Writer) int`, `func CheckCell(cell string, stdout io.Writer) int`. Die laatste twee zijn de CLI-tweelingen van `_cmd_run` en `_cmd_check`. Een configfout geeft `COULD NOT CHECK: <fout>` op stdout en exit 3.
- `detail` bij een ontbrekende match: `missing from output: <repr>`. Python gebruikt `repr()` van een str. Port dat als `pyRepr(s string) string`: kies `'` als quote, tenzij `s` een `'` en geen `"` bevat; escape `\\`, de gekozen quote, `\n`, `\r` en `\t`; zet andere niet-printbare tekens om naar `\xNN`, `\uNNNN` of `\UNNNNNNNN`, zoals Python.

- [ ] **Stap 1: Schrijf de tests.**
  - `TestFixtures` leest `../../tests/pack/fixtures/evidence/cases.json`.
  - `TestAllowlistMatchesFixture` (Besluit 4).
  - Een tweeling per `test_run_row_*` (`test_evidence.py:184-289`). Daarbij hoort ook de output-cap, met een kleinere cap via een package-variabele `maxOutput` in de test.
  - `TestRunRowKillsTheGroupOnTimeout` (Review-focus 4).
  - `TestPyRepr`, met `'a'`, `"it's"`, `'a"b\'c'`, `\n` en `\x00`. De verwachte waarden haal je eenmalig uit `python3 -c 'print(repr(…))'`.
  Run: `go test ./internal/evidence/` · Verwacht: FAIL (compileert niet).
- [ ] **Stap 2: Port de drie bestanden.** `split.go` volgt Besluit 11. `runCapped` volgt Besluit 10: lees met een goroutine in brokken van 64 KiB, en stop bij meer dan `maxOutput` met de reden `output larger than 4194304 bytes`. Een fout bij het starten wordt `not found: <naam>` (ENOENT) of `not executable: <fout>`.
- [ ] **Stap 3: Draai de tests.** Run: `go test -race ./internal/evidence/ && GOOS=windows go vet ./internal/evidence/` · Verwacht: PASS.
- [ ] **Stap 4: Mutatiecheck.** Laat tijdelijk de jq-check (`env`, `$ENV`, `input`) weg. Daarna moet het `check`-geval `gh api repos/x/y --jq env` rood worden. Laat ook de groep-kill bij een timeout weg: dan moet `TestRunRowKillsTheGroupOnTimeout` rood worden. Zet beide terug.
- [ ] **Stap 5: Commit.** `feat(evidence): allowlist en runner in Go`

### Taak 4: De hook `pr-create-gate` in Go

**Bestanden:**
- Maken: `internal/hooks/prcreategate.go`, `internal/hooks/prcreategate_test.go`
- Wijzigen: `internal/hooks/hooks.go` (`hook.Register("pr-create-gate", PrCreateGate)`)
- Wijzigen: `pack/core/hooks/hooks.json:22-26` (Besluit 8). Weg: `pack/core/hooks/pr-create-gate.sh`, de entry in `pack/SOURCES.toml:100-101`
- Wijzigen: `tests/pack/test_pr_create_gate.py`, `tests/pack/test_hooks_minimal_config.py:99,165,181`, `internal/packs/core_test.go:59`
- Wijzigen: `pack/core/requires.toml` (het hook-deel van Besluit 9)

**Interfaces:**
- Gebruikt: `prbody.Main` (taak 2); `jqText`/`jqFields` uit `internal/hooks/hooks.go` voor `.tool_input.command // empty` en `.cwd // empty`, met dezelfde semantiek als jq.
- Levert: `func PrCreateGate(ctx context.Context, root, stand string, p hook.Payload) hook.Result`. `root` gaat als `LIGHTHOUSE_REPO_ROOT` naar de config, zoals `LIB_ROOT` nu doet.

- [ ] **Stap 1: Schrijf `test_payload_shapes` (Review-focus 5) en `test_slow_gh_passes_loudly_within_the_deadline` (Review-focus 2) in `test_pr_create_gate.py`, tegen de bash-hook.** Leg de oordelen vast die hij nu geeft.
  Run: `uv run pytest -q tests/pack/test_pr_create_gate.py` · Verwacht: PASS.
- [ ] **Stap 2: Zet de test om naar de binary.** Voeg `pytestmark = pytest.mark.usefixtures("lighthouse_bin")` toe. `HOOK` wordt `INSTALLED_HOOKS / "run.sh"` met het argument `pr-create-gate`. Pas `BASE_TOOLS` aan en haal de twee tests van Besluit 5 weg. Werk de `pr-create-gate.sh`-regels in `test_hooks_minimal_config.py` bij naar `run.sh pr-create-gate`, zoals #278 dat voor `bash-write-check` deed.
  Run: `uv run pytest -q tests/pack/test_pr_create_gate.py` · Verwacht: FAIL (onbekende hook, exit 2 via `blocking`).
- [ ] **Stap 3: Schrijf de Go-hook en `prcreategate_test.go`.** Volg Besluit 6 en 7. De Go-test bevat `TestPanicBecomesCouldNotCheck`: een `Main` die paniekt (package-variabele `runGate` in de test) geeft exit 2 met `the body parser failed (panic:`. Er is ook een tabeltest op de exit-code-map 0/2/3/1/anders.
- [ ] **Stap 4: Registreer, verwijder en installeer.** `hooks.json`, de `.sh` weg, `SOURCES.toml`, `requires.toml`, `core_test.go`, en daarna `go run ./cmd/lighthouse init --upgrade`.
- [ ] **Stap 5: Draai de tests.** Run: `go test -race ./internal/hooks/ ./internal/packs/ && uv run pytest -q tests/pack/test_pr_create_gate.py tests/pack/test_hooks_minimal_config.py tests/pack/test_run_sh.py` · Verwacht: PASS, zonder dat er een assert in de overgebleven tests is veranderd (AC 1). Controleer dat met `git diff origin/issue-278-bash-write-check-go -- tests/pack/test_pr_create_gate.py`: alleen de importregels, `HOOK`, `BASE_TOOLS`, `pytestmark` en de twee verwijderde tests mogen erin staan.
- [ ] **Stap 6: Mutatiecheck.** Laat in de Go-hook tijdelijk de tweetrapsregel bij exit 3 weg. Daarna moet `test_unparseable_without_gh_pr_passes` rood worden. Zet het terug.
- [ ] **Stap 7: Commit.** `feat(core): pr-create-gate in Go achter run.sh`

### Taak 5: Subcommando's, skills over, `evidence.py` weg

**Bestanden:**
- Maken: `internal/cli/prbodygate.go`, `internal/cli/evidence.go`, `internal/cli/prbodygate_test.go`, `internal/cli/evidence_test.go`; wijzigen: `internal/cli/root.go:81` (twee `AddCommand`)
- Wijzigen: `pack/core/skills/write-pr/SKILL.md:155,230,247,252,261,389`, `pack/core/skills/close-issue/claude/SKILL.md:154,290`, `pack/core/skills/close-issue/agents/SKILL.md:154,290`
- Wijzigen: `internal/dispatch/instructions/run.md:61` en `internal/dispatch/instructions_test.go:132` (`evidence.py` → `lighthouse evidence`)
- Weg: `pack/core/lib/evidence.py`, de entry in `pack/SOURCES.toml:81-82`; wijzigen: `pack/core/requires.toml` (het evidence-deel van Besluit 9), `internal/packs/core_test.go:101`
- Wijzigen: `tests/pack/test_evidence.py`, `tests/pack/test_evidence_fixtures.py` (Besluit 3), `tests/pack/test_write_pr_skill.py:61-99` (Besluit 4 en de nieuwe commando's), `tests/pack/test_close_issue_skill.py:119,159,240`
- Maken: `changelog.d/issue-279-pr-create-gate-go.md`

**Interfaces:**
- Gebruikt: `prbody.Main`, `evidence.Run`, `evidence.CheckCell`.
- Levert de commando's `lighthouse pr-body-gate <cwd>` en `lighthouse evidence check <cel>` / `lighthouse evidence run --issue N --body-file PATH --cwd DIR [--before-pr]`, met de exitcodes uit *Globale eisen*. `SilenceUsage` en `SilenceErrors` staan aan. Een cobra-fout bij het parsen van de argumenten geeft exit 2, zoals argparse (`usage:`-regel op stderr). Alleen `pr-body-gate` met een verkeerd aantal argumenten geeft 64.
- Skilltekst: `python3 .lighthouse/lib/lighthouse_lib/evidence.py check|run` → `lighthouse evidence check|run`. Het pipe-recept wordt `… | lighthouse pr-body-gate "$PWD"` (spec §4). In proza wordt `evidence.py` `lighthouse evidence`. Verder verandert er geen woord.
- Het fragment heeft `type: changed` (minor, spec §10). De tekst is één alinea, in de stijl van `changelog.d/issue-278-bash-write-check-go.md`: de hook en de twee commando's draaien in de binary, en eerst moet de binary op het PATH bijgewerkt worden.

- [ ] **Stap 1: Werk de skilltests bij naar de nieuwe commando's** (spec §9: "lopen mee met de nieuwe commando's"). Zet `test_evidence.py` en het `check`-deel van `test_evidence_fixtures.py` om naar `lighthouse evidence`, met `pytestmark = pytest.mark.usefixtures("lighthouse_bin")`.
  Run: `uv run pytest -q tests/pack/test_evidence.py tests/pack/test_evidence_fixtures.py tests/pack/test_write_pr_skill.py tests/pack/test_close_issue_skill.py` · Verwacht: FAIL.
- [ ] **Stap 2: Schrijf de twee commando's en hun Go-tests.** De tests controleren de exitcodes en dat stdout/stderr gelijk zijn aan de Python-CLI op dezelfde invoer.
- [ ] **Stap 3: Zet de skills, `run.md` en `instructions_test.go` om. Verwijder `evidence.py` en werk `SOURCES.toml`, `requires.toml` en `core_test.go` bij.** Draai daarna `go run ./cmd/lighthouse init --upgrade`. Schrijf ook het fragment.
- [ ] **Stap 4: Zoek naar resten.** Run: `git grep -n -e 'evidence\.py' -e 'pr-create-gate\.sh' -- . ':!changelog.d' ':!docs/developer/planning' ':!CHANGELOG.md'`. Verwacht: alleen treffers die over het verleden gaan. Een treffer in levende tekst wordt omgezet: `docs/site/`, `docs/getting-started.md`, `ci/windows-measure.sh`, `AGENTS.md` en `CLAUDE.md` (de hooklijst noemt `pr-create-gate.sh`).
- [ ] **Stap 5: Draai de tests.** Run: `go test -race ./internal/cli/ ./internal/dispatch/ ./internal/packs/ && uv run pytest -q tests/pack/test_evidence.py tests/pack/test_evidence_fixtures.py tests/pack/test_write_pr_skill.py tests/pack/test_close_issue_skill.py tests/pack/test_pr_body_gate.py tests/pack/test_pr_body_gate_fixtures.py` · Verwacht: PASS.
- [ ] **Stap 6: Commit.** `feat(core): lighthouse evidence en pr-body-gate; evidence.py weg`

### Taak 6: Eindcontrole

- [ ] **Stap 1: De hele Go-suite en de pack-suite.** Run: `go build ./... && go test -race ./... && GOOS=windows go vet ./... && GOOS=darwin go vet ./... && uv run pytest -q tests/pack` · Verwacht: PASS.
- [ ] **Stap 2: Dogfood.** Run: `go run ./cmd/lighthouse init --upgrade` · Verwacht: `No changes.`
- [ ] **Stap 3: Bevroren is bevroren (AC 4).** Run: `git diff --stat origin/main -- pack/core/lib/pr_body_gate.py pack/core/lib/git_invocations.py pack/core/lib/repo_identity.py .lighthouse/lib/lighthouse_lib/pr_body_gate.py` · Verwacht: geen uitvoer. Run ook: `test ! -e pack/core/lib/evidence.py && test ! -e pack/core/hooks/pr-create-gate.sh && test ! -e .lighthouse/lib/lighthouse_lib/evidence.py && test ! -e .claude/hooks/lighthouse/pr-create-gate.sh && echo weg` · Verwacht: `weg`.
- [ ] **Stap 4: De python3-treffers uit de comment op #279.** Run: `git grep -n python3 -- .claude/skills/lighthouse-write-pr .claude/skills/lighthouse-close-issue .agents/skills/lighthouse-write-pr .agents/skills/lighthouse-close-issue .claude/hooks/lighthouse | grep -v -e roborev_ -e board_start -e worktree_audit -e plan_review_record` · Verwacht: geen uitvoer. `pr_body_gate.py:18` blijft staan: die module is bevroren tot stap 8.
- [ ] **Stap 5: Lint.** Run: `golangci-lint run ./internal/prbody/... ./internal/evidence/... ./internal/hooks/... ./internal/cli/...` · Verwacht: `0 issues.`
