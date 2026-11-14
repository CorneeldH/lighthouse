# bash-write-check in Go, met turn-roots — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** `bash-write-check` draait als `lighthouse hook bash-write-check pre|post` achter `run.sh`, en `lighthouse turn-roots` vervangt `turn_roots.py` in `go-lint-gate.sh` en `ty-gate.sh`. Beide geven hetzelfde oordeel en dezelfde tekst als nu. `bash-write-check.sh` en `turn_roots.py` verdwijnen.

**Architectuur:** De vijf Python-modules krijgen een Go-tweeling: `internal/repoidentity`, `internal/deadline`, een blokkerende `Lock` in `internal/flock`, `internal/bashwrites` (wortels, git-lezers, state, `pre`, `post`) en `internal/turnroots`. De hook komt in `internal/hooks`, het subcommando in `internal/cli`. `bash_writes.py`, `deadline.py`, `repo_identity.py` en `filelock.py` blijven bevroren staan voor `commit_base.py`, `commit-test-gate.sh`, `pr_body_gate.py` en `gate_slot.py`. Gedeelde fixtures onder `tests/pack/fixtures/bash_writes/` en `tests/pack/fixtures/repo_identity/` bewijzen dat Python en Go hetzelfde zeggen. De black-box-tests (`test_bash_write_check.py`, `test_turn_roots.py`) lopen voortaan tegen de gebouwde binary.

**Tech stack:** Go (stdlib, `internal/hook`, `internal/config`, `internal/gitinvocations`, `internal/manifest`, `internal/flock`), POSIX `sh` (`run.sh`, ongewijzigd), bash (`go-lint-gate.sh`, `ty-gate.sh`), pytest.

**Spec:** [`2026-11-07-zonder-python-design.md`](../specs/2026-11-07-zonder-python-design.md), stap 5 van *Opdeling*, met §1, §2, §5, §9, §10 en *Anti-doelen*. Issue: #278. Vorige stap als patroon: [`2026-11-08-main-commit-gate-worktree-check-go.md`](2026-11-08-main-commit-gate-worktree-check-go.md).

## Globale eisen

- Elke hook en elk subcommando geeft exact hetzelfde oordeel en dezelfde tekst als zijn Python-voorganger, op dezelfde payloads (spec §2). Kopieer elke berichttekst letterlijk uit `pack/core/lib/bash_writes.py`, `turn_roots.py` en `pack/core/hooks/bash-write-check.sh`.
- `pre` eindigt altijd met exit 0, zonder uitvoer (`bash_writes.py:844`, `bash-write-check.sh:50-55`).
- `post` geeft exit 0 of 2. Bij exit 2 gaat de bundel naar stderr én als `additionalContext` naar stdout. Een melding met `notice` gaat ook als `systemMessage` mee (`_Outcome.render`, `bash_writes.py:1003`).
- Paden, namen en vorm van de state blijven gelijk: `${XDG_RUNTIME_DIR:-/tmp}/lighthouse-bash-gates/<session_id>/`, `ledger.json`, `ledger.lock`, `ledger.reset`, `<key>.json`, mode `0700`. JSON hoeft niet byte-gelijk te zijn, wel gelijk van inhoud.
- De testnaden blijven werken onder dezelfde namen: `LIGHTHOUSE_BASH_GATES_DEADLINE`, `LIGHTHOUSE_BASH_GATES_START_NS`, en `LIGHTHOUSE_UNDER_BASH_WRITE_CHECK=1` voor elke gate.
- Bevroren, byte voor byte gelijk aan `origin/main`: `pack/core/lib/{bash_writes,deadline,repo_identity,filelock}.py` en hun geïnstalleerde kopieën onder `.lighthouse/lib/lighthouse_lib/`.
- Go volgt `/lighthouse-go-standard`, tests volgen `/lighthouse-test-standard`. Alles compileert ook met `GOOS=windows` en `GOOS=darwin`.
- Commits volgen Conventional Commits, met de berichten in het Nederlands.
- Een wijziging onder `pack/` → `go run ./cmd/lighthouse init --upgrade`, en het resultaat gaat in dezelfde commit mee.

## Besluiten die dit plan neemt

1. **Welke gevallen naar fixtures gaan (AC 2).** Net als in stap 4 (Besluit 1): de inline gevallen van de *modules* die naar Go gaan, voor zover ze data zijn of een repo die de fixture-opbouw kan maken. Dat zijn in `tests/pack/test_bash_writes.py`: `parse_porcelain` (2), `parse_diff_tree` (1), `parse_reflog` en `commits_to_check` (10, regels 125–234), `derive_roots` (6, regels 314–405), `project_filter` (3), `marker_key` (2) en `classify` (de parametrize op regel 863 plus `test_classify_first_line_only`). In `tests/pack/test_repo_identity.py`: `remote_key` (3 tests) en `same_origin` (5). Wat over processen, tijd, locks of nep-gates gaat (`deadline`, `filelock`, ledger, `pre`, dispatch), is geen data. Daarvoor krijgt Go een tweelingtest per Python-test, met dezelfde asserts. De Python-tests blijven staan, want hun module is bevroren.
2. **`test_turn_roots.py` is een black-box-test.** Hij draait het script als subproces, zoals de hooks dat doen. Hij blijft dus pytest en roept voortaan `lighthouse turn-roots` aan, met dezelfde asserts (spec §9, derde bullet). Er komen geen fixtures voor: `turn_roots.py` verdwijnt, dus er is geen tweede implementatie om mee te vergelijken. `test_only_a_json_error_is_reported_as_invalid_json` patcht Python-interne functies. Zijn onderwerp verdwijnt; er komt een Go-test met dezelfde bedoeling voor in de plaats (taak 7).
3. **Tests waarvan het onderwerp verdwijnt**, zoals in stap 4 (Besluit 2). Er wordt geen verwachting aangepast; deze tests gaan weg:
   - in `test_bash_write_check.py`: `test_wrapper_without_python3_is_loud`, `test_wrapper_dispatcher_crash_is_loud`, `test_wrapper_without_the_lib_is_loud` en `test_pre_without_python_is_silent_post_is_loud`;
   - in `test_turn_roots.py`: `test_only_a_json_error_is_reported_as_invalid_json`;
   - in `tests/pack/go/test_go_lint_gate.py`: de parameter `python3` van `test_missing_tool_could_not_check` wordt `lighthouse`. De gate heeft `python3` niet meer nodig, wel de binary.
4. **Zonder binary blokkeert ook `pre`.** `run.sh` kijkt alleen naar de hooknaam in `blocking`, niet naar de stand, en `bash-write-check` staat erin (spec §1). Zonder binary geeft `bash-write-check pre` dus exit 2, waar de bash-hook stil bleef. Dat verandert niets in de praktijk: zonder binary blokkeert `main-commit-gate` toch al elke Bash-call (spec §1: "Dat is bedoeld"). Hetzelfde geldt voor een oudere binary die `main-commit-gate` kent maar `bash-write-check` niet (exit 3 wordt via `blocking` exit 2); dat is wat spec §1/§3 zegt, en het changelog-fragment en de PR-tekst melden het. `run.sh` blijft ongewijzigd. Een nieuwe test legt het vast (taak 6).
5. **Een paniek in `post`** wordt binnen de hook opgevangen en wordt `bash-write-check: COULD NOT CHECK: internal error in the dispatcher (panic: <waarde>)`, met exit 2 en dezelfde uitvoer als elke andere `cannot`. Zo komt de melding ook als `additionalContext` mee, zoals `run_post` (`bash_writes.py:1658`). Zonder deze opvang maakt `hook.invoke` er exit 3 van, en dan geeft `run.sh` een andere tekst.
6. **De manifest** wordt soepel gelezen, zoals `managed_files` (`bash_writes.py:269-293`), en dus niet met de strikte `manifest.Read`: die weigert een `sha256` die geen 64 hex-tekens is, een unit zonder `package` of `version`, onbekende sleutels en dubbele units, en dat zijn precies de manifesten die Python accepteert (`test_managed_block_leaves_the_file_to_the_project` schrijft `sha256 = "0"` en verwacht een gewone dispatch; spec §3 staat sleutels van een nieuwere lighthouse toe). Een ontbrekende manifest-file is een lege map. De TOML wordt in een generieke map gedecodeerd; alleen `unit`-regels met `kind == "file"` tellen, en bij zo'n regel geeft een ontbrekend of verkeerd getypeerd `path` of `sha256` een `ManifestError`. Alles anders wordt genegeerd. Ongeldige TOML of een leesfout wordt `ManifestError` met de tekst `cannot read <root>/.lighthouse/manifest.toml: <fout>`.
7. **Gates draaien als subproces**, ook `md-link-check` (`run.sh md-link-check`), net als in de Python (`GATE_ARGV`). De Go-hook roept geen andere hook in-process aan.
8. **De registratie** wordt `${CLAUDE_PLUGIN_ROOT}/hooks/run.sh bash-write-check pre` en `… post`, met dezelfde timeouts (5, 120, 120). `blocking` blijft zoals het is.
9. **`requires.toml`:** `bash-write-check` gaat van de `python3`-regel van `pack/core` af. In `pack/go` gaat `go-lint-gate` van de `python3`-regel af. `ty-gate` in `pack/python` houdt `python3`, want hij leest `config.py` nog.
10. **Preflight van de gates.** `go-lint-gate.sh` controleert `jq git lighthouse` in plaats van `jq git python3`. `ty-gate.sh` controleert `jq git python3 lighthouse`.

## Review-focus

Deze vijf inputs dekt geen bestaande test (op parallelle schrijvers na: `test_ledger_survives_parallel_writers`, `test_bash_writes.py:646`, is de Python-tweeling). Ze bijten het eerst bij een port van Python naar Go. Elk ervan krijgt een test in de taak die de code bezit:

1. **Een gate die een kind op de achtergrond achterlaat** (`sleep 60 & exit 0`). `exec.Cmd.Wait` met een pipe of `bytes.Buffer` wacht tot elk kind de pijp sluit. Dan hangt `post` tot zijn deadline. De Python schrijft naar een tijdelijk bestand en wacht alleen op het proces. Go doet hetzelfde: stdout en stderr gaan naar één `*os.File` uit `os.CreateTemp`. Test `TestRunCaptureDoesNotWaitForAGrandchild` (taak 2) en `test_gate_leaving_a_background_child_does_not_stall_post` (taak 6).
2. **Een bestandsnaam die geen UTF-8 is** (`b"bad\xff.sh"`), en een met niet-ASCII (`café.sh`). Python leest paden via `os.fsdecode` en drukt ze af met `backslashreplace` (`_printable`, `bash_writes.py:964`). Go werkt op bytes in strings. `printable(s string) string` zet elke ongeldige byte om naar `\xNN` en laat geldige UTF-8 staan. Tests `TestPrintable` (taak 5) en `test_non_utf8_and_non_ascii_names_in_the_bundle` (taak 6).
3. **Een signaal tijdens een gate** (de hook-timeout van Claude Code, een Ctrl-C). De Python stuurt het door naar de procesgroep van de gate, meldt `signal received` en geeft de claims vrij. Go ontvangt het via `signal.Notify` in `RunCapture`, of via `Options.Stop` in een test. Tests `TestRunCaptureStopsTheGroupOnASignal` (taak 2) en `TestDispatchStopsLoudlyOnASignal` (taak 5).
4. **Een ledger die Python schreef** (`json.dumps` met `ensure_ascii`, dus `é` in plaats van `é`), en parallelle schrijvers. Tijdens de overgang kan een sessie een ledger van de bevroren Python-hook hebben. Tests `TestLedgerReadsAPythonWrittenLedger` (fixture `tests/pack/fixtures/bash_writes/ledger_python.json`) en `TestLedgerSurvivesParallelWriters` (taak 4).
5. **Een payload van een vreemde vorm voor `post`:** een JSON-array, `null`, een ontbrekende `tool_input`, een `command` dat een getal is, en een onbekende stand (`bash-write-check foo`). De Python geeft bij de eerste twee `the hook payload on stdin is not a JSON object` met exit 2. Een ontbrekende `tool_input` loopt door met `""` als command. Een `command` dat een getal is, loopt ook door als er een `tool_use_id` is; zonder `tool_use_id` geeft hij exit 2 met `COULD NOT CHECK` (zie `MarkerKey`, taak 3). De test controleert daar alleen exit 2 en `COULD NOT CHECK`, niet de tekst tussen haakjes. De wrapper geeft `unknown mode, expected pre or post`. Nieuwe test `test_post_payload_shapes`: eerst tegen de bash-hook groen (taak 6, stap 1), daarna tegen Go.

---

### Taak 1: Gedeelde fixtures voor `bash_writes` en `repo_identity`, gedraaid tegen Python

**Bestanden:**
- Maken: `tests/pack/fixtures/bash_writes/cases.json`, `tests/pack/fixtures/bash_writes/ledger_python.json`
- Maken: `tests/pack/fixtures/repo_identity/cases.json`
- Maken: `tests/pack/test_bash_writes_fixtures.py`, `tests/pack/test_repo_identity_fixtures.py`
- Wijzigen: `tests/pack/test_bash_writes.py`, `tests/pack/test_repo_identity.py` (de gevallen die verhuizen, gaan eruit)

**Interfaces:**
- Levert het fixtureformaat dat taak 2 en 3 in Go lezen. Een JSON-array van objecten met `name`, `kind`, `expect`, en per soort de invoer:
  - `porcelain` en `diff_tree`: `input` (tekst, `\u0000` als scheiding) → `expect: {"changed": [...], "deleted": [...]}`, gesorteerd.
  - `reflog`: `input` → `expect`: een lijst `[sha, epoch, subject]`.
  - `commits`: `entries` (lijst `[sha, epoch, subject]`), `stop` (tekst of `null`), `since` (getal of `null`) → `expect: {"commits": [[sha, is_merge]], "notes": [...]}`.
  - `classify`: `rc`, `output` → `expect`: `"ok"`, `"finding"` of `"unchecked"`.
  - `marker_key`: `payload` → `expect`: de sleutel.
  - `derive_roots`: `setup`, `command`, `cwd` → `expect`: gesorteerde lijst wortels.
  - `project_filter`: `setup`, `roots`, `project` → `expect: {"kept": [...], "error_prefixes": [...]}`, of `"raises"`. De fout bevat de stderr van git, die per omgeving verschilt: vergelijk op prefix (`COULD NOT CHECK: {real:broken} (`), zoals `test_bash_writes.py:430-433`.
  - In `repo_identity/cases.json`: `remote_key` (`url` → tekst of `null`) en `same_origin` (`setup`, `project`, `root` → `[bool, reden]`, of `"raises"`).
- `setup` gebruikt het formaat van `tests/pack/fixtures/git_invocations/cases.json` (`repo`, `dir`, `file`, `symlink`, `overwrite`), plus twee nieuwe stappen: `{"worktree": "x/.claude/worktrees/wt", "of": "x", "branch": "wt"}` en `{"origin": "x", "url": "git@github.com:o/r.git"}`. `{root}` wordt de tijdelijke map, zonder `realpath`. In `expect` wordt `{real:<pad>}` het `realpath` van `{root}/<pad>`. De wortels van `derive_roots` gaan immers door `os.path.realpath`.
- `ledger_python.json`: een ledger zoals `json.dump` hem schrijft, met één wortel waarvan een pad niet-ASCII is (`café.sh`) (Review-focus 4).

- [ ] **Stap 1: Zet de gevallen van Besluit 1 om naar de twee `cases.json`.** Vul elke `expect` met wat de Python teruggeeft.
- [ ] **Stap 2: Schrijf de twee runners.** `test_fixture_case(case, tmp_path, monkeypatch)`, geparametriseerd per `name`. De opbouw van `setup` komt uit `tests/pack/test_git_invocations_fixtures.py`. Breid die uit met `worktree` en `origin`, en verplaats de gedeelde opbouw naar `tests/pack/_testkit.py` (`build_fixture_setup(root: Path, steps: list[dict]) -> None`), zodat beide runners hem gebruiken.
- [ ] **Stap 3: Draai de tests.**
  Run: `uv run pytest -q tests/pack/test_bash_writes_fixtures.py tests/pack/test_repo_identity_fixtures.py tests/pack/test_bash_writes.py tests/pack/test_repo_identity.py tests/pack/test_git_invocations_fixtures.py`
  Verwacht: PASS.
- [ ] **Stap 4: Mutatiecheck.** Verander tijdelijk in `bash_writes.py` `_is_merge_commit` naar `startswith("commit")`, en laat in `repo_identity.py` de `.casefold()` in `remote_key` weg. Daarna moet elk ten minste één fixture rood worden. Zet het terug met `git checkout pack/core/lib/`.
- [ ] **Stap 5: Commit.** `test(bash-writes): gevallen naar gedeelde fixtures`

### Taak 2: De bladeren in Go — `repoidentity`, `deadline`, `flock.Lock`

**Bestanden:**
- Maken: `internal/repoidentity/repoidentity.go`, `internal/repoidentity/fixtures_test.go`
- Maken: `internal/deadline/deadline.go`, `internal/deadline/group_unix.go`, `internal/deadline/group_other.go`, `internal/deadline/deadline_test.go`
- Wijzigen: `internal/flock/flock_unix.go`, `flock_windows.go`, `flock_other.go`, `flock_test.go`
- Maken: `internal/fixturesetup/fixturesetup.go`. Wijzigen: `internal/gitinvocations/fixtures_test.go` (gebruikt `fixturesetup`), `internal/gitinvocations/walk.go` (`resolve` → `Resolve`, `basename` → `Basename`, voor taak 3)
- Regel voor een gate die bestaat maar niet uitvoerbaar is (EACCES): `RunCapture` geeft een startfout anders dan ENOENT als fout terug, en `Post` maakt er `internal error in the dispatcher (<fout>)` van, zoals Python (`deadline.py:131-139`). Alleen ENOENT wordt 127.

**Interfaces:**
- Levert:
  - `repoidentity.RemoteKey(url string) (key string, ok bool)`
  - `repoidentity.Origin(ctx context.Context, root string) (url string, set bool, err error)`. Draait `git -C root config --get remote.origin.url` met `LC_ALL=C`. rc 1 met lege stderr is "niet gezet". Elke andere fout wordt een `*OriginError` met de tekst van `repo_identity.py:72-79`.
  - `repoidentity.SameOrigin(ctx context.Context, projectDir, root string) (same bool, reason string, err error)`
  - `deadline.TimeoutExit = 124`, `deadline.NotFoundExit = 127`
  - `type deadline.Options struct { Grace time.Duration; Dir string; Stdin []byte; Env []string; Stop <-chan os.Signal }`. Bij `Stop == nil` zet `RunCapture` zelf `signal.Notify` op SIGTERM, SIGINT en SIGHUP (wat het platform heeft), en haalt dat weer weg.
  - `deadline.RunCapture(deadline time.Time, argv []string, o Options) (code int, output []byte, err error)`. Gedrag van `run_until` met `capture=True` en `raise_on_signal=True` (`deadline.py:150`). Is de deadline al voorbij, dan komt `(124, nil, nil)` terug zonder start. Een ontbrekend commando geeft `(127, nil, nil)`. Na de deadline: SIGTERM naar de groep, `Grace` wachten, dan SIGKILL, en code 124 met wat er geschreven was. Bij een signaal komt `err` als `*Interrupted{Signal os.Signal; Output []byte}` terug, na het stoppen van de groep. Stdout en stderr gaan samen naar één bestand uit `os.CreateTemp` (Review-focus 1).
  - `flock.Lock(path string) (*os.File, error)`: als `TryLock`, maar wacht (`LOCK_EX` zonder `LOCK_NB`; op Windows `LockFileEx` zonder `LOCKFILE_FAIL_IMMEDIATELY`; elders een fout).

- [ ] **Stap 1: Schrijf de tests.** `repoidentity`: `TestFixtures` leest `../../tests/pack/fixtures/repo_identity/cases.json`. De opbouw van `setup` deel je met `internal/gitinvocations/fixtures_test.go` (die Wijzigen-regel staat in de bestandenlijst): verplaats die naar een nieuw pakket `internal/fixturesetup` (Maken: `internal/fixturesetup/fixturesetup.go`) (`func Build(t *testing.T, root string, steps []map[string]any)`), met de stappen `worktree` en `origin` erbij. `deadline`: een tweeling van elke test in `tests/pack/test_deadline.py` op `run_until` met `capture=True` (regels 45–73, 115–153, 177–267), plus `TestRunCaptureDoesNotWaitForAGrandchild` (`sh -c 'sleep 60 & echo ok'` is binnen 2 s klaar met `ok\n`) en `TestRunCaptureStopsTheGroupOnASignal` (via `Options.Stop`). `flock`: `TestLockWaitsForTheHolder`.
- [ ] **Stap 2: Draai de tests.** Run: `go test ./internal/repoidentity/ ./internal/deadline/ ./internal/flock/ ./internal/gitinvocations/` · Verwacht: FAIL (compileert niet).
- [ ] **Stap 3: Bouw de drie onderdelen.** `group_unix.go` start met `SysProcAttr{Setpgid: true}` en stopt met `syscall.Kill(-pgid, sig)`. `group_other.go` gebruikt `cmd.Process.Kill()`; dat stopt alleen het proces zelf, en `taskkill /F /T` van `deadline.py:95-104` (de hele boom) is geen eis van deze stap: noteer het verschil in de code. Exporteer in deze taak ook `gitinvocations.Resolve` en `Basename` (`walk.go:174,188`) voor taak 3.
- [ ] **Stap 4: Draai de tests.** Run: `go test -race ./internal/repoidentity/ ./internal/deadline/ ./internal/flock/ ./internal/gitinvocations/ && GOOS=windows go vet ./internal/deadline/ ./internal/flock/` · Verwacht: PASS.
- [ ] **Stap 5: Mutatiecheck.** Laat in `RunCapture` tijdelijk de SIGKILL na `Grace` weg. Daarna moet de tweeling van `test_sigterm_ignorer_is_killed_after_grace` rood worden (timeout). Zet het terug.
- [ ] **Stap 6: Commit.** `feat(core): repoidentity, deadline en flock.Lock in Go`

### Taak 3: `bashwrites` deel 1 — git, wortels en lezers

**Bestanden:**
- Maken: `internal/bashwrites/git.go` (port van `GitError`, `git`, `head_of`, `read_reflog`, `diff_commit`; `bash_writes.py:110-145, 311-330, 529-567`)
- Maken: `internal/bashwrites/roots.go` (`_root_of`, `_resolved_path`, `derive_roots`, `project_filter`; regels 147–262)
- Maken: `internal/bashwrites/parse.go` (`parse_porcelain`, `parse_diff_tree`, `parse_reflog`, `commits_to_check` met zijn drie takken; regels 331–527)
- Maken: `internal/bashwrites/gates.go` (`GATES`, `GATE_PACKS`, `GATE_ARGV`, `gate_path`, `gate_label`, `classify`, `marker_key`, `managed_files`, `_as_installed`)
- Maken: `internal/bashwrites/fixtures_test.go`

**Interfaces:**
- Gebruikt: `gitinvocations.Walk`, `GitTarget`, `ExistingAncestor`, `RepoOf`, `ErrUnparseable`; `repoidentity.SameOrigin` (taak 2); `github.com/BurntSushi/toml` (generieke decode, zie Besluit 6), niet `manifest.Read`; `gitinvocations.Resolve` en `Basename` (taak 2 exporteert ze uit `internal/gitinvocations/walk.go:174,188`).
- Levert, in package `bashwrites`:
  - `type GitError struct { Root string; Code int; Timeout bool; Msg string }` met `Error() string` = `Msg`. `Code` is -1 bij timeout of startfout (Python `None`).
  - `func Git(root string, deadline time.Time, args ...string) ([]byte, error)`: `git --no-optional-locks -C root …`, met een timeout van minstens 100 ms. De timeouttekst is `git read past the deadline`.
  - `func HeadOf(root string, deadline time.Time) (string, error)`. De waarde is `"unborn"` in een repo zonder commit.
  - `func DeriveRoots(command, cwd string) map[string]bool`. Een open quote valt terug op `tokenRE = regexp.MustCompile(`[^\s'"=;&|<>()]+`)`.
  - `func ProjectFilter(roots map[string]bool, projectDir string, deadline time.Time) (kept map[string]bool, errs []string, err error)`
  - `func ParsePorcelain(data []byte) (changed, deleted map[string]bool)`, `func ParseDiffTree(data []byte) (changed, deleted map[string]bool)`
  - `type ReflogEntry struct { SHA string; Epoch int64; Subject string }`, `func ParseReflog(text string) ([]ReflogEntry, error)`. Een regel zonder twee `\x1f` is een fout, geen paniek.
  - `type Commit struct { SHA string; Merge bool }`, `func CommitsToCheck(entries []ReflogEntry, stop *string, since *int64) ([]Commit, []string)`
  - `func ReadReflog(root string, deadline time.Time) ([]ReflogEntry, error)`, `func DiffCommit(root, sha string, merge bool, deadline time.Time) (changed, deleted map[string]bool, err error)`
  - `var Gates = map[string][]string{…}` (letterlijk `GATES`), `func GatePath(repoRoot, gate string) string`, `func GateLabel(gate string) string`, `func Classify(rc int, output []byte) string`
  - `func MarkerKey(raw map[string]json.RawMessage) (string, error)`. Zonder `tool_use_id` en met een `command` dat geen string is, crasht de Python in `.encode`. `pre` slikt dat, en `post` maakt er `internal error in the dispatcher (…)` van, met exit 2. Go geeft dan een fout `command is not a string`, en `Post` maakt er `internal error in the dispatcher (command is not a string)` van. Het oordeel is hetzelfde, alleen de tekst tussen haakjes niet.
  - `type ManifestError struct{ Msg string }`, `func ManagedFiles(root string) (map[string]string, error)`, `func asInstalled(root, rel string, managed map[string]string) bool`

- [ ] **Stap 1: Schrijf `fixtures_test.go`.** `TestFixtures` leest `../../tests/pack/fixtures/bash_writes/cases.json` en bouwt `setup` met `fixturesetup.Build`. Voeg tweelingen toe van `test_git_raises_giterror_*`, `test_head_of_*` en `test_diff_commit_*` (`test_bash_writes.py:73-95, 236-312`).
  Run: `go test ./internal/bashwrites/` · Verwacht: FAIL (compileert niet).
- [ ] **Stap 2: Port de vier bestanden, functie voor functie.** Python-namen in camelCase. Paden blijven bytes in een `string`. Splits nooit op iets anders dan wat de Python gebruikt (`\0`, `\n`). `os.path.realpath` wordt `filepath.EvalSymlinks` op het bestaande deel. `os.path.normpath` wordt `filepath.Clean`. De Python verwerkt de stappen van `walk` binnen de lus, omdat zijn `dirs` live is. De Go-`Walk` geeft per `Step` al een kopie (`Here`), dus dat probleem bestaat in Go niet.
- [ ] **Stap 3: Draai de tests.** Run: `go test -race ./internal/bashwrites/` · Verwacht: PASS.
- [ ] **Stap 4: Mutatiecheck.** Laat in `CommitsToCheck` tijdelijk de dedup op sha weg. Daarna moet `reflog-start-time-dedups-and-survives-a-gap` rood worden. Zet het terug.
- [ ] **Stap 5: Commit.** `feat(bashwrites): wortels, git-lezers en parsers in Go`

### Taak 4: `bashwrites` deel 2 — state, ledger en `Pre`

**Bestanden:**
- Maken: `internal/bashwrites/state.go` (port van `signature` t/m `prune_markers`; regels 570–785)
- Maken: `internal/bashwrites/pre.go` (`_run_pre`, `run_pre`; regels 787–865)
- Maken: `internal/bashwrites/state_test.go`, `internal/bashwrites/pre_test.go`

**Interfaces:**
- Gebruikt: `Git`, `HeadOf`, `ParsePorcelain`, `DeriveRoots`, `ProjectFilter`, `Gates`, `MarkerKey` (taak 3); `flock.Lock` (taak 2).
- Levert:
  - `type Signature [4]int64` (mtime_ns, ctime_ns, ino, size), `func signatureOf(path string) (Signature, bool)`. `ctime` en `ino` komen uit `syscall.Stat_t`, in aparte bestanden per OS (`signature_linux.go` met `Ctim`, `signature_darwin.go` met `Ctimespec`, en een `_other`-bestand met `0`). Draai `GOOS=darwin go vet ./internal/bashwrites/` al in stap 3 van deze taak.
  - `func StateDir(sessionID string) (string, error)`. Een `/` of `..` geeft `invalid session_id: '<id>'`.
  - `type Ledger struct { Roots map[string]json.RawMessage; Unreadable bool }`. De regels blijven ruwe JSON, zodat een veld dat de Go-versie niet kent, en een `ns`-handtekening boven 2^53 (die een `float64` zou verminken), bij het terugschrijven ongewijzigd blijven. Elke regel wordt bij gebruik gevalideerd zoals `_valid_record` (`bash_writes.py:1047-1055`); een regel die geen object is, laat alleen die ene wortel vallen, niet de hele ledger (`_read_ledger`, `:640-651`). Voeg een tweeling toe met een echte ns-handtekening die een round-trip overleeft.
  - `func withLedger(state string, body func(*Ledger) error) error`: lock, lezen, `body`, en alleen terugschrijven als `body` `nil` geeft en `Unreadable` onwaar is. Het schrijven is atomisch (tijdelijk bestand `.tmp-*.json` in `state`, dan `os.Rename`).
  - `func seedRoot(root string, startNS int64, deadline time.Time) (map[string]any, error)`, `writeMarker`, `readMarker`, `pruneMarkers(state string, now time.Time)`
  - `func Pre(raw []byte, now func() time.Time) int`: altijd 0, slikt elke fout en elke paniek.

- [ ] **Stap 1: Schrijf de tweelingtests.** Eén Go-test per Python-test in `test_bash_writes.py:460-860` en `1277-1315`, met dezelfde asserts. Plus `TestLedgerReadsAPythonWrittenLedger` (leest `ledger_python.json` en schrijft hem na een wijziging terug met `café.sh` intact) en `TestLedgerSurvivesParallelWriters`: 8 goroutines die elk 50 keer onder `withLedger` een teller ophogen. Daarna staat er 400. Op Linux sluiten twee `flock`s via aparte `open`s elkaar ook binnen één proces uit (Review-focus 4).
  Run: `go test ./internal/bashwrites/` · Verwacht: FAIL.
- [ ] **Stap 2: Port `state.go` en `pre.go`.** `LIGHTHOUSE_BASH_GATES_START_NS` overschrijft de starttijd. De deadline van `pre` is 4 s. Een sessie zonder `CLAUDE_PROJECT_DIR` doet niets.
- [ ] **Stap 3: Draai de tests.** Run: `go test -race ./internal/bashwrites/` · Verwacht: PASS.
- [ ] **Stap 4: Mutatiecheck.** Laat in `withLedger` tijdelijk de controle op `Unreadable` weg. Daarna moet de tweeling van `test_locked_ledger_does_not_write_back_an_unreadable_ledger` rood worden. Zet het terug.
- [ ] **Stap 5: Commit.** `feat(bashwrites): state, ledger en pre in Go`

### Taak 5: `bashwrites` deel 3 — pool, claims, dispatch en `Post`

**Bestanden:**
- Maken: `internal/bashwrites/pool.go` (port van `_RootView` t/m `_release_claims`; regels 1022–1405)
- Maken: `internal/bashwrites/dispatch.go` (`run_gate`, `_printable`, `_Outcome`, `_report_verdict`, `_cannot_left`, `_vanished`, `_dispatch`; regels 867–1020 en 1407–1520)
- Maken: `internal/bashwrites/post.go` (`_marker_roots` t/m `run_post`; regels 1522–1674)
- Maken: `internal/bashwrites/post_test.go`

**Interfaces:**
- Gebruikt: alles uit taak 3 en 4; `deadline.RunCapture` en `deadline.Interrupted` (taak 2).
- Levert:
  - `type Outcome struct{…}` met `block`, `note(text string, notice bool)`, `cannot` en `Render(event string) (code int, output map[string]any, stderr string)`. `output` is `nil` als er niets te melden is. Bij een blokkade is `stderr` de tekst plus `\n`.
  - `func printable(s string) string` (Review-focus 2).
  - `func runGate(repoRoot, root, rel, gate string, deadline time.Time) (verdict string, output []byte)`. Hier worden de verdicts `"stopped"` en `"missing"`. De grace is 10 s.
  - `var now = time.Now`, de klok van `Post`, die een test vervangt (Python `_now`).
  - `func Post(raw []byte, repoRoot string) (code int, output map[string]any, stderr string)`. Een payload die geen JSON-object is, geeft `the hook payload on stdin is not a JSON object` met het event `PostToolUse`. Het event komt uit `hook_event_name`, met `PostToolUse` als standaard. Een paniek wordt de melding van Besluit 5. De claims en de marker gaan altijd weg (`defer`).

- [ ] **Stap 1: Schrijf de tweelingtests.** Eén Go-test per Python-test in `test_bash_writes.py:889-1556`, met dezelfde asserts. Waar de Python `run_gate` of `_now` vervangt, vervangt Go een package-variabele (`var runGateFunc = runGate`). Plus `TestPrintable`: `"bad\xff.sh"` → `bad\xff.sh` letterlijk met backslash, `"café.sh"` blijft gelijk. En `TestDispatchStopsLoudlyOnASignal`: een `runGateFunc` die `"stopped"` geeft. Daarna noemt de uitvoer `signal received`, en staan de overige paden onder `not checked`.
  Run: `go test ./internal/bashwrites/` · Verwacht: FAIL.
- [ ] **Stap 2: Port de drie bestanden.** `LIGHTHOUSE_BASH_GATES_DEADLINE` overschrijft de 90 s. De payload aan een gate is `{"tool_name":"Bash","cwd":root,"tool_input":{"file_path":<abs>}}`, met `cmd.Dir = root` en de omgeving plus `LIGHTHOUSE_UNDER_BASH_WRITE_CHECK=1`. De volgorde van wortels en paden is gesorteerd, zoals in de Python.
- [ ] **Stap 3: Draai de tests.** Run: `go test -race ./internal/bashwrites/` · Verwacht: PASS.
- [ ] **Stap 4: Mutatiecheck.** Laat in de dispatch tijdelijk de controle `now() >= deadline` weg. Daarna moet de tweeling van `test_deadline_makes_progress_over_many_files` rood worden. Zet het terug.
- [ ] **Stap 5: Commit.** `feat(bashwrites): pool, dispatch en post in Go`

### Taak 6: `bash-write-check` als Go-hook; zijn pytest-tests via `run.sh`

**Bestanden:**
- Maken: `internal/hooks/bashwritecheck.go`, `internal/hooks/bashwritecheck_test.go`
- Wijzigen: `internal/hooks/hooks.go` (registratie)
- Wijzigen: `tests/pack/test_bash_write_check.py`

**Interfaces:**
- Gebruikt: `bashwrites.Pre` en `bashwrites.Post` (taak 4 en 5); `hook_shim`, `RUN_SH_TOOLS` en `lighthouse_bin` (stap 4).
- Levert: `func BashWriteCheck(ctx context.Context, root, stand string, p hook.Payload) hook.Result`. `pre` → `bashwrites.Pre(p.Raw, time.Now)` (schrijft de marker en het seed-ledger; geeft daarna `Result{}`). `post` → `Post(p.Raw, root)`. Een andere stand → `bash-write-check: COULD NOT CHECK — unknown mode, expected pre or post`, met exit 2, die tekst plus `\n` op stderr, en `{"hookSpecificOutput":{"hookEventName":<event>,"additionalContext":<tekst>}}` op stdout (`loud`, `bash-write-check.sh:37-47`).

- [ ] **Stap 1: Schrijf de nieuwe tests en draai ze tegen de bash-hook.** `test_post_payload_shapes` (Review-focus 5), `test_gate_leaving_a_background_child_does_not_stall_post` (een stub-gate `sleep 60 & exit 0`; `post` is binnen 20 s klaar, met exit 0) en `test_non_utf8_and_non_ascii_names_in_the_bundle`. Die laatste schrijft `café.sh` en `bad\xff.sh` met de kapotte shell-inhoud. `café.sh` staat dan letterlijk in de kop, en `bad\xff.sh` als `bad\\xff.sh`.
  Run: `uv run pytest -q tests/pack/test_bash_write_check.py -k 'payload_shapes or background_child or non_utf8'` · Verwacht: PASS (bash).
- [ ] **Stap 2: Zet de tests over op `run.sh`.** `_Session.wrapper` wordt `hook_shim(self.root, "bash-write-check")`, en de module krijgt `pytestmark = pytest.mark.usefixtures("lighthouse_bin")`. Haal de vier tests van Besluit 3 weg. Voeg `test_pre_without_lighthouse_blocks_like_every_gate` toe (Besluit 4): een PATH met alleen `RUN_SH_TOOLS` en `sh` geeft bij `pre` exit 2 met `lighthouse not on PATH`. `test_hooks_registered_pre_post_and_failure` blijft in deze taak ongewijzigd (hooks.json verandert pas in taak 9, daar gaat hij mee). Verander geen andere assert.
  Run: `uv run pytest -q tests/pack/test_bash_write_check.py` · Verwacht: FAIL (onbekende hook, exit 3 → `run.sh` blokkeert).
- [ ] **Stap 3: Bouw `BashWriteCheck` en registreer hem.** `bashwritecheck_test.go` test de drie standen en het event van de onbekende stand.
- [ ] **Stap 4: Draai de tests.**
  Run: `go build ./... && go test -race ./internal/hooks/ && uv run pytest -q tests/pack/test_bash_write_check.py tests/pack/shell/test_shellcheck.py tests/pack/python/test_ruff_format.py tests/pack/test_run_sh.py`
  Verwacht: PASS.
- [ ] **Stap 5: Mutatiecheck.** Laat `Post` tijdelijk altijd `0, nil, ""` teruggeven. Daarna moet `test_bash_write_check.py` op de findings rood worden. Zet het terug.
- [ ] **Stap 6: Commit.** `feat(hooks): bash-write-check in Go`

### Taak 7: `lighthouse turn-roots`

**Bestanden:**
- Maken: `internal/turnroots/transcript.go` (`_entries`, `_is_prompt`, `_moment`, `_tool_uses`, `turn_entries`), `internal/turnroots/turnroots.go` (`touched_roots`, `tool_paths`, `changed_since`, `_turn`, `resolve`, `classify`)
- Maken: `internal/turnroots/turnroots_test.go`
- Maken: `internal/cli/turnroots.go`; wijzigen: `internal/cli/root.go` (registratie)
- Wijzigen: `tests/pack/test_turn_roots.py`

**Interfaces:**
- Gebruikt: `bashwrites.DeriveRoots`, `ProjectFilter`, `Git`, `ParsePorcelain`, `HeadOf`, `ReadReflog`, `GitError` (taak 3); `gitinvocations.ExistingAncestor`, `RepoOf`.
- Levert:
  - `type Undetermined struct{ Msg string }` (error)
  - `func Resolve(raw []byte, projectDir string) ([]string, error)`
  - `type Verdict struct { Root string; Touched bool }`, `func Classify(raw []byte, projectDir string, patterns []string) ([]Verdict, error)`
  - `lighthouse turn-roots [--match PATTERN]...`: de payload op stdin. Exit 0 met de regels van `main` (`turn_roots.py:307`). Exit 1 met de reden op stdout. Ongeldige JSON geeft `the payload is not valid JSON.`, en een payload die geen object is `the payload is no JSON object.`. `fnmatch.fnmatchcase` wordt een eigen `fnmatchCase(name, pattern string) bool`, waarin `*` ook `/` matcht en `[...]` en `?` werken. `path.Match` voldoet niet: daar matcht `*` geen `/`.

- [ ] **Stap 1: Zet `test_turn_roots.py` om.** `_run` roept `[str(lighthouse_bin), "turn-roots", *args]` aan in plaats van `sys.executable SCRIPT`. `lighthouse_bin` komt als fixture-argument mee. Haal `test_only_a_json_error_is_reported_as_invalid_json` weg (Besluit 2). Schrijf `turnroots_test.go`: `TestFnmatchCase` (tabel met `*.go` tegen `a/b.go`, `go.mod`, `[ab].go`, `?.go`), `TestIsPrompt` (de gevallen van `_is_prompt`, ook `isCompactSummary`), en `TestAGitParseErrorIsNotInvalidJSON`: een reflog-regel zonder scheidingen geeft een `Undetermined` of een touched-verdict, nooit `not valid JSON`.
  Run: `uv run pytest -q tests/pack/test_turn_roots.py` · Verwacht: FAIL (onbekend commando).
- [ ] **Stap 2: Port de twee bestanden en het subcommando.** Gebruik `CLAUDE_PROJECT_DIR` uit de omgeving, zoals `turn_roots.py:255`. De deadline is 20 s. Tijdstempels lees je met `time.Parse(time.RFC3339Nano, …)`; een `Z` en een offset werken allebei.
- [ ] **Stap 3: Draai de tests.** Run: `go build ./... && go test -race ./internal/turnroots/ ./internal/cli/ && uv run pytest -q tests/pack/test_turn_roots.py` · Verwacht: PASS.
- [ ] **Stap 4: Mutatiecheck.** Laat `fnmatchCase` tijdelijk `path.Match` gebruiken. Daarna moet `test_match_a_pattern_with_a_directory` of `TestFnmatchCase` rood worden. Zet het terug.
- [ ] **Stap 5: Commit.** `feat(cli): lighthouse turn-roots`

### Taak 8: `go-lint-gate` en `ty-gate` op `lighthouse turn-roots`

**Bestanden:**
- Wijzigen: `pack/go/hooks/go-lint-gate.sh:11,15,26,56,99`, `pack/python/hooks/ty-gate.sh:12,17,51,97`
- Wijzigen: `pack/go/requires.toml`, `pack/python/requires.toml` (Besluit 9 en 10)
- Wijzigen: `tests/pack/go/test_go_lint_gate.py` (`TOOLS`: `python3` eruit, `lighthouse` erin; de parameter van Besluit 3), `tests/pack/python/test_ty_gate.py` (`lighthouse` op het PATH van de tests; `test_gate_python.py` toetst `gate_python` en blijft ongemoeid)
- Geïnstalleerd (door `init --upgrade`): `.claude/hooks/lighthouse/go-lint-gate.sh`

**Interfaces:**
- Gebruikt: `lighthouse turn-roots` (taak 7).

- [ ] **Stap 1: Zet de tests om.** In beide modules staat `lighthouse` op het PATH (`usefixtures("lighthouse_bin")`, `tool_bin(…, (*TOOLS, "lighthouse"))`; ook de tuples in `test_ty_gate.py:258,303-312`). Besluit per bestaande test die stukgaat (de nepstubs die `lighthouse` vervangen laten voor `turn-roots` de gebouwde binary uitvoeren en antwoorden alleen op `usage alarm`): `test_without_lighthouse_a_valid_alarm_still_blocks` (`test_go_lint_gate.py:368`) en `test_ty_gate.py:467` verliezen hun onderwerp (een alarm zonder binary bestaat niet meer) en worden `COULD NOT CHECK` met `lighthouse is missing`; `test_help_text_from_an_old_lighthouse_is_no_alarm` (`test_go_lint_gate.py:395`, `test_ty_gate.py:494`) en `test_alarm_is_read_with_lighthouse_usage_alarm` (`test_ty_gate.py:475`) krijgen een stub die alleen op de alarm-aanroep afwijkt. Elke andere assert blijft.
  Run: `uv run pytest -q tests/pack/go/test_go_lint_gate.py tests/pack/python/test_ty_gate.py` · Verwacht: FAIL bij `test_missing_tool_could_not_check[lighthouse]` (de gate controleert `lighthouse` nog niet).
- [ ] **Stap 2: Pas de twee hooks aan.** Regel 99 van `go-lint-gate.sh` wordt `ROOTS=$(printf '%s' "$INPUT" | LIGHTHOUSE_REPO_ROOT="$LIB_ROOT" lighthouse turn-roots "${MATCH_ARGS[@]}" 2>&1)`, en `ty-gate.sh:97` net zo. De commentaarregels noemen `lighthouse turn-roots`. Draai `go run ./cmd/lighthouse init --upgrade`.
- [ ] **Stap 3: Draai de tests.** Run: `go build ./... && uv run pytest -q tests/pack/go/ tests/pack/python/` · Verwacht: PASS. Let op: de `go-lint-gate` van deze sessie roept `lighthouse turn-roots` op het PATH aan (`go-lint-gate.sh:99`), niet de dev-binary; draai `go install ./cmd/lighthouse` na taak 7, anders geeft elke Stop `COULD NOT CHECK` tot de release. Elke test die al bestond, ziet dezelfde wortels als met `turn_roots.py` (AC 3).
- [ ] **Stap 4: Commit.** `feat(go,python): go-lint-gate en ty-gate op lighthouse turn-roots`

### Taak 9: Registratie via `run.sh`, de bash-hook en `turn_roots.py` weg

**Bestanden:**
- Wijzigen: `pack/core/hooks/hooks.json:19,61,87` (Besluit 8)
- Verwijderen: `pack/core/hooks/bash-write-check.sh`, `pack/core/lib/turn_roots.py`
- Wijzigen: `pack/core/requires.toml` (Besluit 9), `pack/SOURCES.toml:37,109` (de twee entries eruit)
- Wijzigen: `internal/packs/core_test.go:56,112` (de twee bestanden uit de vaste lijst)
- Wijzigen: `tests/pack/test_hooks_minimal_config.py:101,141,164,176,239`. De sleutels worden `run.sh bash-write-check pre|post`. De uitzondering voor `pre` op regel 239 blijft staan: met een binary blijft `pre` stil.
- Wijzigen: `tests/pack/_testkit.py` (de verwijzing naar `turn_roots.py`; `INSTALLED_LIB` blijft voor de bevroren modules)
- Wijzigen: `tests/pack/test_bash_write_check.py` (`test_hooks_registered_pre_post_and_failure`: zoekt `run.sh bash-write-check pre` (timeout 5) en `run.sh bash-write-check post` (120, voor `PostToolUse` en `PostToolUseFailure`), en controleert dat `INSTALLED_HOOKS / "run.sh"` uitvoerbaar is; de `.sh`-check vervalt)
- Wijzigen: `ci/windows-measure.sh:122-126` (`run.sh bash-write-check pre|post`)
- Wijzigen: `CLAUDE.md` (hooklijst: `bash-write-check` (`run.sh bash-write-check`)), `pack/core/skills/fix-pr/{claude,agents}/SKILL.md:143`, `docs/site/data/session.json`, `docs/site/data/loop.json`, en elke andere treffer van `git grep -n -e 'bash-write-check\.sh' -e 'turn_roots' -- ':!tests/pack/fixtures' ':!docs/developer/planning' ':!CHANGELOG.md' ':!pack/core/lib' ':!.lighthouse/lib'`
- Geïnstalleerd (door `init --upgrade`): `.claude/settings.json`, `.claude/hooks/lighthouse/`, `.lighthouse/lib/lighthouse_lib/turn_roots.py` (weg), `.claude/skills/`, `.agents/skills/`, `.lighthouse/manifest.toml`

- [ ] **Stap 1: Zet de tests op de nieuwe registratie.** Pas eerst `test_hooks_registered_pre_post_and_failure` aan zoals in Bestanden staat. Run: `go test ./internal/packs/ && uv run pytest -q tests/test_dogfood.py tests/pack/test_hooks_minimal_config.py tests/pack/test_bash_write_check.py::test_hooks_registered_pre_post_and_failure` · Verwacht: FAIL (`hooks.json` noemt nog de `.sh`).
- [ ] **Stap 2: Pas `hooks.json`, `requires.toml` en `SOURCES.toml` aan, haal de twee bestanden weg, en werk de overige verwijzingen bij.** Draai `go run ./cmd/lighthouse init --upgrade`. Controleer dat `.claude/hooks/lighthouse/bash-write-check.sh` en `.lighthouse/lib/lighthouse_lib/turn_roots.py` weg zijn, en dat `.lighthouse/lib/lighthouse_lib/bash_writes.py` er nog staat.
- [ ] **Stap 3: Draai de tests.**
  Run: `go build ./... && go test -race ./internal/... && go test -race -tags e2e ./e2e/... && uv run pytest -q tests/test_dogfood.py tests/pack/ tests/test_site_session.py tests/test_site_config.py tests/test_site_loop.py tests/test_site_reference.py tests/test_site_packs.py`
  Verwacht: PASS.
- [ ] **Stap 4: Controleer de dogfood.** Draai `go run ./cmd/lighthouse init --upgrade` nog een keer. Verwacht: `No changes.`
- [ ] **Stap 5: Commit.** `feat(core): bash-write-check via run.sh, bash-hook en turn_roots.py weg`. Na deze commit registreert de eigen sessie `run.sh bash-write-check`. Tot de achtergrondbuild klaar is, kent de PATH-binary de hook niet; bij `dev` geeft dat een melding en geen blokkade (spec §10).

### Taak 10: Changelog en eindcontrole

**Bestanden:**
- Maken: `changelog.d/issue-278-bash-write-check-go.md`

- [ ] **Stap 1: Schrijf het fragment.** Front matter `type: changed` (minor, spec §10). Tekst in het Engels: `bash-write-check` runs in the `lighthouse` binary through `.claude/hooks/lighthouse/run.sh`, and `go-lint-gate` and `ty-gate` find the checkouts of a turn with `lighthouse turn-roots`. Update the binary on your PATH before `lighthouse init --upgrade`: an older binary does not know these commands. Noem in de PR dat de release na de merge met `workflow_dispatch` gestart moet worden.
- [ ] **Stap 2: Draai de volledige controle.**
  Run: `python3 ci/changelog.py check --base origin/main --head HEAD && go vet ./... && GOOS=windows go vet ./... && GOOS=darwin go vet ./... && golangci-lint run ./... && go test -race ./... && uv run pytest -n auto -q`
  Verwacht: alles groen.
- [ ] **Stap 3: Controleer of de bevroren modules bevroren bleven (AC 4).** Run: `git diff --stat origin/main -- pack/core/lib/bash_writes.py pack/core/lib/deadline.py pack/core/lib/repo_identity.py pack/core/lib/filelock.py .lighthouse/lib/lighthouse_lib/bash_writes.py .lighthouse/lib/lighthouse_lib/deadline.py .lighthouse/lib/lighthouse_lib/repo_identity.py .lighthouse/lib/lighthouse_lib/filelock.py` · Verwacht: geen uitvoer. En `git ls-files pack/core/hooks/bash-write-check.sh pack/core/lib/turn_roots.py .lighthouse/lib/lighthouse_lib/turn_roots.py .claude/hooks/lighthouse/bash-write-check.sh` · Verwacht: geen uitvoer.
- [ ] **Stap 4: Commit.** `docs(changelog): fragment voor #278`
