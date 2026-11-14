# commit-test-gate in Go — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** `commit-test-gate` draait als `lighthouse hook commit-test-gate pre|post` achter `run.sh`, met hetzelfde oordeel en dezelfde tekst als `commit-test-gate.sh`. De bash-hook en de zeven Python-modules die alleen hij nog gebruikt, verdwijnen.

**Architectuur:** Eén nieuw pakket `internal/commitgate` met de ports van `no_verify`, `host_memory`, `commit_base` en `gate_slot`, plus de gate zelf (config, slot, geheugen, rem, de lus per wortel, `Pre`, `Post`). Het hergebruikt `gitinvocations.Find`, `repoidentity.SameOrigin`, `deadline.RunCapture`, `flock` en `bashwrites` (`HeadOf`, `Git`, `MarkerKey`, de markerhelpers). De hook zelf is dun en staat in `internal/hooks/committestgate.go`. Data-achtige gevallen gaan eerst naar gedeelde fixtures en draaien tegen Python; daarna leest Go dezelfde fixtures. De black-box-tests (`test_commit_test_gate.py`, `test_commit_test_gate_no_verify.py`) lopen voortaan via `run.sh` tegen de gebouwde binary.

**Tech stack:** Go (stdlib, `internal/hook`, `internal/config`, `internal/gitinvocations`, `internal/repoidentity`, `internal/deadline`, `internal/flock`, `internal/bashwrites`), POSIX `sh` (`run.sh`, ongewijzigd), pytest.

**Spec:** [`2026-11-07-zonder-python-design.md`](../specs/2026-11-07-zonder-python-design.md), stap 7 van *Opdeling*, met §1, §2, §9, §10 en *Anti-doelen*. Issue: #280. Gestapeld op #278 (PR #326). Vorige stap als patroon: [`2026-11-09-issue-278-bash-write-check-go.md`](2026-11-09-issue-278-bash-write-check-go.md).

## Globale eisen

- Elk oordeel en elke berichttekst komt letterlijk uit `pack/core/hooks/commit-test-gate.sh` en de vier modules. Een blokkade is exit 2 met de tekst op stderr en niets op stdout. Een pass is exit 0, met `{"systemMessage": M, "hookSpecificOutput": {"hookEventName": E, "additionalContext": M}}` op stdout alleen als er meldingen zijn (`emit_message_and_exit`, `:258-263`).
- De bash-tekst komt uit `$(…)` en `echo`. Een vastgelegde uitvoer verliest daarom zijn afsluitende `\n`'s en krijgt er bij het afdrukken precies één bij. Go doet hetzelfde voor `TEST_OUTPUT` en voor de stderr van git en config.
- De state blijft gelijk van pad en vorm, zodat een marker van de bash-`pre` door de Go-`post` gelezen wordt (een upgrade midden in een Bash-call): `${XDG_RUNTIME_DIR:-/tmp}/lighthouse-commit-test-gate/<session_id>/<marker_key>.json` met `{"heads": {root: sha}}`, mode `0700`. De slotmap blijft `LIGHTHOUSE_GATE_SLOT_DIR`, anders `/run/user/$UID/lighthouse-gate-slots`, anders `/tmp/lighthouse-gate-slots-$UID`, met `slot-<n>`.
- De testnaden blijven werken onder dezelfde namen: `LIGHTHOUSE_TOML`, `LIGHTHOUSE_HOST_TOML`, `LIGHTHOUSE_GATE_SLOT_DIR`, `LIGHTHOUSE_GATE_CGROUP_ROOT`, `LIGHTHOUSE_GATE_MEMINFO`. Nieuw zijn `LIGHTHOUSE_GATE_START` en `LIGHTHOUSE_GATE_TRACE` (Besluit 5).
- Constanten: `MAX_DEADLINE = 280`, `MAX_SLOTS = 7`, grace 10 s, git-deadline van `commit_base` 30 s, slot- en geheugeninterval 1 s, `EMPTY_TREE = 4b825dc642cb6eb9a060e54bf8d69288fbee4904`, hook-timeout 300 s in `hooks.json`.
- Bevroren, byte voor byte gelijk aan `origin/main`: `pack/core/lib/{git_invocations,repo_identity,pr_body_gate,config,headings,datadir,protocol}.py` en hun kopieën onder `.lighthouse/lib/lighthouse_lib/`.
- Go volgt `/lighthouse-go-standard`, tests volgen `/lighthouse-test-standard`. Alles compileert ook met `GOOS=windows` en `GOOS=darwin`.
- Commits volgen Conventional Commits, met de berichten in het Nederlands.
- Een wijziging onder `pack/` → `go run ./cmd/lighthouse init --upgrade`, en het resultaat gaat in dezelfde commit mee.

## Besluiten die dit plan neemt

1. **Eén PR, niet twee.** `pre` en `post` delen `run_gate_roots` (`:318-720`): config, slot, geheugen, rem en de lus. Bij een splitsing per stand draait er een tussenstand met de helft in bash en de helft in Go, en dan moeten twee implementaties dezelfde code en hetzelfde markerformaat delen. De PR blijft groot. De taken zijn daarom zo gesneden dat elke taak los te reviewen is.
2. **Pakketindeling.** `internal/commitgate` krijgt de logica en `internal/hooks/committestgate.go` de dunne hook (spec, *Aan de bouwer*). `bashwrites` exporteert `PruneMarkers` en `AtomicWriteJSON` (nu `pruneMarkers` en `atomicWriteJSON`, `state.go:105,264`), want `commit_base.py` gebruikt ze ook (`:60-69`).
3. **Welke gevallen naar fixtures gaan (AC 2).** Net als stap 5 (Besluit 1): de gevallen die data zijn, of een repo die `fixturesetup` kan bouwen.
   - `tests/pack/fixtures/no_verify/cases.json`: elk geval uit `test_no_verify.py`, plus de commando's van `test_commit_test_gate_no_verify.py`, die een verwachting op exit 0 of 2 hebben (`command` → `expect: true|false`).
   - `tests/pack/fixtures/host_memory/cases.json`: `read_meminfo` (tekst → velden, of `"raises"`) en `shortfalls` (velden plus drempels → `{"found": [...], "notes": [...]}`), uit `test_host_memory.py`.
   - `tests/pack/fixtures/commit_base/cases.json`: `record`, `moved` en `changed_files`, met een `setup` in het formaat van `bash_writes/cases.json`, uit `test_commit_base.py`.

   Wat over locks, tijd of processen gaat (`gate_slot`, de wachtlus van `host_memory`), is geen data. Daarvoor krijgt Go een tweelingtest per Python-test, met dezelfde asserts. `deadline` en `filelock` kregen hun Go-tweelingen al in stap 5. Taak 3 controleert dat per test en vult aan wat ontbreekt.
4. **Weg in deze PR, want de laatste afnemer gaat om** (*Anti-doelen*): `commit_base.py`, `bash_writes.py`, `gate_slot.py`, `host_memory.py`, `no_verify.py` en `deadline.py` (spec, stap 7). Daarbij komt `filelock.py`: zijn enige afnemers zijn `bash_writes.py` en `gate_slot.py`. Met de modules gaan hun Python-unittests weg: `test_commit_base.py`, `test_no_verify.py`, `test_gate_slot.py`, `test_host_memory.py`, `test_deadline.py`, `test_filelock.py`, `test_bash_writes.py`, `test_bash_writes_fixtures.py`. Hun gevallen leven verder in de fixtures en de Go-tweelingen. Bevroren blijven `git_invocations.py` en `repo_identity.py`, voor `pr_body_gate.py` (stap 8).
5. **Testnaden voor de klok.** De bash rekent de deadline met één `date +%s` uit (`:386`), en drie tests vervangen die `date` (`test_gate_uses_one_deadline_for_two_roots`, `test_memory_wait_gets_what_the_slot_wait_left`, `test_slot_stays_held_until_deadline_py_ends`). Go roept geen `date` aan. In de plaats komen twee naden:
   - `LIGHTHOUSE_GATE_START`: epoch-seconden die bij die ene berekening de klok vervangen.
   - `LIGHTHOUSE_GATE_TRACE`: een bestand waaraan de hook één regel `deadline` toevoegt op het moment dat hij de deadline zet.

   De drie tests zetten de naden in plaats van de stub. De assert die de `date`-aanroepen telt (`test_commit_test_gate.py:698`), gaat van `["call"]` naar `["deadline"]`. Dat zijn de enige aangepaste verwachtingen. Hun onderwerp, "één deadline voor de hele lus", blijft gelijk; alleen het meetpunt heet anders. De PR noemt ze.
6. **Tests waarvan het onderwerp verdwijnt** (stap 5, Besluit 3). Deze gaan weg, met een Go-test met dezelfde bedoeling ervoor in de plaats:
   - `test_gate_loud_without_cat` → `TestMissingToolingBlocks` (taak 5): zonder `git` of `bash` op het PATH volgt `missing tooling: git` of `missing tooling: bash`.
   - `test_gate_blocks_when_gate_slot_breaks` → `TestSlotErrorBlocks` (taak 5): een lock die niet met "bezet" maar met een andere fout faalt, geeft `cannot take a gate slot: <fout>`.
   - `test_hook_has_no_bash4_fd_variables`: de bash-hook bestaat niet meer. Er komt niets voor in de plaats.

   In de PATH-tuples van `tool_bin` (`:622,640,1331`) gaan `jq`, `python3` en `cat` eruit, en `bash` plus `RUN_SH_TOOLS` erin. Het versmalde PATH krijgt `lighthouse_dir()` vooraan, zoals `test_bash_write_check.py:104`; anders stopt `run.sh` met `lighthouse not on PATH`. Dat is opbouw, geen verwachting.
7. **Gereedschap.** Go heeft `git` en `bash` nodig. De melding blijft `commit-test-gate: COULD NOT CHECK — missing tooling: <namen>` met dezelfde twee regels eronder (`:131-139`). De testcommand draait als `<bash> -c <command>`. `<bash>` is `exec.LookPath("bash")`, opgezocht bij de start van de hook. Dat vervangt `"$BASH"` (`:578`). Het lib-loze pad (`:233-245`) vervalt: er is geen lib meer.
8. **Een gedode hook.** In bash hield `deadline.py` het slot vast als de hook-bash stierf. Het bewaakte ook de deadline (`:62-64`). Die rol neemt de binary over: `run.sh` start hem als kind. Wordt `run.sh` gedood (de test doodt `hook.pid`), dan leeft de binary door, stopt hij de groep op de deadline en geeft hij daarna het slot vrij. `test_slot_stays_held_until_deadline_py_ends` bewijst dat ongewijzigd. Het slot is een `*os.File` die de hook open houdt tot `RunRoots` klaar is. Het testcommando erft hem niet.
9. **Een signaal tijdens de testrun.** `RunCapture` geeft `*deadline.Interrupted` terug nadat hij de groep gestopt heeft. In bash stierf de hook hier aan het signaal, zonder uitvoer. Go doet hetzelfde oordeel na (spec, *Niet-doelen*: "Een hook geeft na de ombouw hetzelfde oordeel als ervoor"): `Result{Code: 128 + n}`, zonder stdout en zonder stderr, en het slot gaat vrij. Het is niet de signaaltak van `:649-674`: die gaat over een testcommando dat aan een signaal stierf.
10. **Slots op Windows.** `gate_slot.py` weigerde op Windows, omdat de lock daar sterft met het Python-proces. In Go houdt de hook de lock zelf vast, dus die reden vervalt. Slots draaien via `flock.TryFile` op elk platform dat `flock` kent. Dat is de enige uitzondering op "hetzelfde oordeel"; de spec noemt hem (*Niet-doelen*). Op Windows moet het slotbestand open staan met lees- of schrijfrecht, anders faalt `LockFileEx`. Go's `O_APPEND` haalt `GENERIC_WRITE` weg (`syscall_windows.go`), dus `OpenSlots` opent met `O_RDWR|O_CREATE`. Op een platform zonder `flock` (`flock_other.go`) komt er een fout, en dat blokkeert met `cannot take a gate slot: …`. Rij 11 van `ci/windows-measure.sh` (rc 0, `git version`) meet het op Windows. Het changelog-fragment meldt het verschil.
11. **Config lezen** gaat via `config.Run([]string{key}, …)`, zoals `defaultBranch()` (`internal/hooks/hooks.go:76`). De wortel is dezelfde: `LIGHTHOUSE_REPO_ROOT`, anders de `root` die `hook.Run` meegeeft. rc 0 betekent gezet, rc 1 niet gezet. Elke andere rc blokkeert met `the configuration cannot be read (<key>): <stderr>`. Lijsten komen één waarde per regel. De bestandsnaam in `COMMAND_HINT` is `${LIGHTHOUSE_TOML:-<root>/lighthouse.toml}`.
12. **`bash_writes.classify` in de tests van de taalpakketten.** `tests/pack/go/test_gofmt.py`, `tests/pack/python/test_ruff_format.py` en `tests/pack/shell/test_shellcheck.py` importeren nog `bash_writes`. Ze krijgen `classify(rc: int, output: bytes) -> str` uit `tests/pack/_testkit.py`. Een nieuwe test, `test_classify_matches_the_shared_fixtures`, draait die helper over de `classify`-gevallen van `tests/pack/fixtures/bash_writes/cases.json`. Dat is dezelfde fixture die `internal/bashwrites/fixtures_test.go` leest. Zo blijft er één waarheid.
13. **Registratie**: `${CLAUDE_PLUGIN_ROOT}/hooks/run.sh commit-test-gate pre` en `… post`, met timeout 300 op alle drie de plekken. `blocking` noemt `commit-test-gate` al.
14. **`requires.toml` van `pack/core`:** `commit-test-gate` gaat van de `jq`-, de `python3`- en de `tomllib`-regel. Van de `python3`-regel gaan ook `lighthouse_lib.deadline`, `gate_slot`, `filelock`, `no_verify`, `bash_writes` en `commit_base` af, en van de `git`-regel `lighthouse_lib.bash_writes` en `lighthouse_lib.commit_base`.

## Review-focus

Geen bestaande test dekt deze vijf inputs. Ze bijten het eerst bij een port van bash naar Go. Elk krijgt een test in de taak die de code bezit:

1. **Testuitvoer die geen UTF-8 is** (`\xff`), en een samenvattingsregel met zo'n byte. Op stderr moet de uitvoer byte voor byte gelijk blijven. In de JSON wordt hij U+FFFD, zoals `jq --arg` doet. Test `TestOutputBytesPassThrough` (taak 4).
2. **Uitvoer met afsluitende lege regels en regels van alleen spaties** (`"3 passed\n  \n\n"`). De samenvatting is `3 passed`, en het blok op stderr eindigt met precies één `\n` (`:705-715`). Test `TestSummaryLine` (taak 4).
3. **Een `[test_gate] command` met een `'` en een newline erin.** De plakregel van `post_untested` voert dat commando ongewijzigd uit. Test `TestShellQuote` (taak 4) en `test_rerun_line_survives_a_quote_in_the_command` (taak 5). Die laatste draait de regel met `_run_rerun_line` en kijkt of de nep-`uv` liep.
4. **Een marker die de bash-`pre` schreef, met een niet-ASCII-wortel** (`café`, `json.dumps` met `ensure_ascii`). Na een upgrade midden in een call leest de Go-`post` hem. Test `TestMovedReadsAPythonWrittenMarker` (taak 3), op fixture `tests/pack/fixtures/commit_base/marker_python.json`.
5. **Een signaal tijdens de testrun** (Besluit 9). Test `TestInterruptedRunEndsLikeBash` (taak 4): een `runFunc` die `*deadline.Interrupted{Signal: syscall.SIGTERM}` geeft, levert exit 143, geen `Output`, een lege `Stderr`, en het slot is daarna weer te nemen.

---

### Taak 1: Gedeelde fixtures, gedraaid tegen Python

**Bestanden:**
- Maken: `tests/pack/fixtures/no_verify/cases.json`, `tests/pack/fixtures/host_memory/cases.json`, `tests/pack/fixtures/commit_base/cases.json`, `tests/pack/fixtures/commit_base/marker_python.json`
- Maken: `tests/pack/test_commit_gate_fixtures.py` (één runner voor de drie)
- Wijzigen: `tests/pack/test_no_verify.py`, `test_host_memory.py`, `test_commit_base.py` (de gevallen die verhuizen, gaan eruit)
- Wijzigen: `tests/pack/_testkit.py`: `classify(rc, output)` en `test_classify_matches_the_shared_fixtures` in `tests/pack/test_bash_writes_fixtures.py` (Besluit 12; die test verhuist in taak 6 naar `tests/pack/test_classify.py`)
- Wijzigen: `tests/pack/_testkit.py`: `build_fixture_setup` en `fill_fixture` krijgen de stappen en placeholders hieronder. Taak 3 geeft `internal/fixturesetup` dezelfde.

**Interfaces:**
- Nieuwe `setup`-stappen, naast de bestaande (alle paden relatief aan `{root}`):
  - `{"unborn": "x"}`: `git init -q -b main` in `x`, zonder commit.
  - `{"commit": "x", "message": "m"}`: `git -C x add -A` en `git -C x commit -qm m`. Bestanden komen er vooraf in met de bestaande `file`-stap.
  - `{"mv": "x", "from": "a.txt", "to": "b.txt"}`: `git -C x mv a.txt b.txt`.
- Nieuwe placeholders:
  - `{rev:<pad>:<rev>}`: `git -C {root}/<pad> rev-parse <rev>`, bijvoorbeeld `{rev:repo:HEAD~1}`.
  - `{empty_tree}`: `4b825dc642cb6eb9a060e54bf8d69288fbee4904`.
  De runner vult ze pas in na de laatste stap van het geval, en dus na `commits` bij `moved`. De bestaande stappen en placeholders blijven ongewijzigd; de vijf andere Go-pakketten en Python-runners die ze gebruiken, merken niets.
- Levert het fixtureformaat van taak 2 en 3. Het is een JSON-array van `{name, kind, ..., expect}`:
  - `no_verify`: `command` → `expect: bool`. Neem ook `git commit -t x -n` en `git commit -tn` op (beide `false`, want `t` neemt een waarde), zodat de mutatie van stap 3 een fixture rood maakt.
  - `meminfo`: `text` → `expect: {veld: kB}` of `"raises"`.
  - `shortfalls`: `info`, `min_available_gib` (getal of `null`), `min_swap_free_gib` → `expect: {"found": [...], "notes": [...]}`. Neem ten minste `1234567` als drempel op: dat legt de `{:g}`-opmaak van Python vast.
  - `record`: `setup` en `roots` → `expect: {"heads": {...}}`, met `{rev:<pad>:HEAD}` als waarde en `""` voor een unborn repo.
  - `moved`: `setup`, `roots` en `commits` (`setup`-stappen die na `record` lopen) → `expect`: lijst van `[root, base, head]` in de volgorde van `roots`, bijvoorbeeld `["{root}/repo", "{rev:repo:HEAD~1}", "{rev:repo:HEAD}"]`, en `{empty_tree}` als base voor een unborn repo. De runner roept `record`, dan de `commits`, dan `moved` aan, met dezelfde payload. Een geval met twee keer `moved` (de marker is weg) heeft `expect_second: []`.
  - `changed_files`: `setup` (met `commit`- en `mv`-stappen), `repo`, `base` en `head` (placeholders) → `expect`: gesorteerde paden, ook beide kanten van een rename.
  - Wat geen data is, blijft een test per taal: `test_state_dir_refuses_path_escape`, `test_old_markers_are_pruned`, `test_moved_does_not_create_the_state_dir` en `test_moved_with_read_only_runtime_dir_is_empty` krijgen in taak 3 een Go-tweeling.
- `marker_python.json` is een marker zoals `json.dumps` hem schrijft, met één wortel `{root}/café` (Review-focus 4).

- [ ] **Stap 1: Zet de gevallen van Besluit 3 om naar de drie `cases.json`.** Vul elke `expect` met wat de Python teruggeeft.
- [ ] **Stap 2: Schrijf de runner en `classify`.** `test_fixture_case(case, tmp_path, monkeypatch)`, geparametriseerd per `name`, met `build_fixture_setup` uit `_testkit.py`.
  Run: `uv run pytest -q tests/pack/test_commit_gate_fixtures.py tests/pack/test_no_verify.py tests/pack/test_host_memory.py tests/pack/test_commit_base.py tests/pack/test_bash_writes_fixtures.py` · Verwacht: PASS.
- [ ] **Stap 3: Mutatiecheck.** Haal in `no_verify.py` tijdelijk `"t"` uit `VALUE_SHORT_LETTERS` en laat in `host_memory.py` de `SwapTotal == 0`-tak weg. Daarna moet elk ten minste één fixture rood worden. Zet het terug met `git checkout pack/core/lib/`.
- [ ] **Stap 4: Commit.** `test(commit-test-gate): gevallen naar gedeelde fixtures`

### Taak 2: `commitgate` deel 1 — `no_verify` en `host_memory`

**Bestanden:**
- Maken: `internal/commitgate/noverify.go`, `internal/commitgate/memory.go`, `internal/commitgate/fixtures_test.go`, `internal/commitgate/memory_test.go`

**Interfaces:**
- Gebruikt: `gitinvocations.Walk`, `GitTarget`, `Basename`, `ErrUnparseable`.
- Levert:
  - `func CommandHasNoVerify(command, cwd string) bool`. Een open quote geeft `false` (`no_verify.py:117`).
  - `type Thresholds struct{ MinAvailableGiB, MinSwapFreeGiB *float64 }`
  - `func ReadMeminfo(path string) (map[string]int64, error)`. De fout zonder `MemAvailable:` is `<path>: no MemAvailable: line`.
  - `func Shortfalls(info map[string]int64, t Thresholds) (found, notes []string)`. De tekst is `<naam> %.1f GiB < <drempel> GiB`. De drempel volgt Python `{:g}`: zes significante cijfers, zonder nullen achteraan, en een exponent vanaf 1e6. Maak daarvoor `pyG(float64) string` in dit pakket. `%g` van Go is fout, want die geeft `1.234567e+06` waar Python `1.23457e+06` geeft.
  - `func WaitMemory(path string, t Thresholds, wait time.Duration, sleep func(time.Duration), now func() time.Time) (notes []string, below string, err error)`. Hij meet minstens één keer, ook bij `wait == 0`. Daarna meet hij elke seconde, maar slaapt nooit voorbij start + `wait`. `below != ""` betekent: na `wait` nog te kort, en `below` is de lijst met `; ` ertussen. `err` betekent: niet meetbaar, met de tekst `<path>: <fout>`.

- [ ] **Stap 1: Schrijf de tests.** `TestFixtures` leest `no_verify`, `meminfo` en `shortfalls` uit `../../tests/pack/fixtures/`. Schrijf een tweeling van elke test in `test_host_memory.py` die over `main` en de wachtlus gaat, met een nepklok via `sleep` en `now`.
  Run: `go test ./internal/commitgate/` · Verwacht: FAIL (compileert niet).
- [ ] **Stap 2: Port de twee bestanden.**
- [ ] **Stap 3: Draai de tests.** Run: `go test -race ./internal/commitgate/` · Verwacht: PASS.
- [ ] **Stap 4: Mutatiecheck.** Laat `hasNoVerify` tijdelijk de `--` niet als einde zien. Daarna moet `test_precommit_hook_allows_no_verify_after_bare_separator` als fixture rood worden. Zet het terug.
- [ ] **Stap 5: Commit.** `feat(commitgate): no_verify en host_memory in Go`

### Taak 3: `commitgate` deel 2 — `commit_base` en slots

**Bestanden:**
- Maken: `internal/commitgate/base.go`, `internal/commitgate/slot.go`, `internal/commitgate/base_test.go`, `internal/commitgate/slot_test.go`
- Wijzigen: `internal/bashwrites/state.go` (`PruneMarkers`, `AtomicWriteJSON` exporteren, met hun aanroepers)
- Wijzigen: `internal/flock/flock_unix.go`, `flock_windows.go`, `flock_other.go`, `flock_test.go` (`TryFile`)
- Wijzigen: `internal/deadline/deadline_test.go` en `internal/flock/flock_test.go`: alleen de tweelingen die nog ontbreken (Besluit 3)
- Wijzigen: `internal/fixturesetup/fixturesetup.go`: de stappen `unborn`, `commit`, `mv` en de placeholders `{rev:<pad>:<rev>}` en `{empty_tree}` uit taak 1, met dezelfde betekenis als in `_testkit.py`

**Interfaces:**
- Gebruikt: `bashwrites.HeadOf`, `Git`, `MarkerKey`, `PruneMarkers`, `AtomicWriteJSON`.
- Levert:
  - `const EmptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"`
  - `type Moved struct{ Root, Base, Head string }`
  - `func StateDir(sessionID string, create bool) (string, error)`. Een `/` in de id, of de id `..`, geeft `invalid session_id: '<id>'`.
  - `func Record(raw []byte, roots []string) error`, `func MovedRoots(raw []byte) ([]Moved, error)`. `MovedRoots` leest de marker en verwijdert hem. Een ontbrekende marker geeft `nil, nil`, en een unborn base wordt `EmptyTree`. Elke fout geeft `commit_base: <fout>`. De rijen komen in de volgorde van de sleutels in de marker, zoals Python `dict` die leest: decodeer `heads` met `json.Decoder.Token` en niet in een `map`.
  - `func ChangedFiles(root, base, head string) ([]string, error)`: `git diff --name-only --no-renames -z`, gesorteerd, zonder lege paden.
  - `func flock.TryFile(f *os.File) (held bool, err error)`: `LOCK_EX|LOCK_NB` op een open bestand. Bezet is `false, nil`. Op Windows gebruikt hij `LockFileEx` met `LOCKFILE_FAIL_IMMEDIATELY`. Elders geeft hij een fout.
  - `const MaxSlots = 7`, `var ErrSlotsBusy = errors.New("all gate slots busy")`
  - `func SlotDir() string`: de drie regels van `:391-397`.
  - `func OpenSlots(dir string, n int) ([]*os.File, error)`. Hij doet eerst `MkdirAll` met `0700` en opent dan `slot-1` tot `slot-n` met `O_RDWR|O_CREATE` en `0666` (Besluit 10: niet `O_APPEND`). Er zijn twee fouttypen, met de teksten van `:400-411`: `*SlotDirError` (`lock dir <dir> cannot be created (<fout>)`) en `*SlotOpenError` (`lock files in <dir> cannot be opened`).
  - `func Acquire(files []*os.File, wait, interval time.Duration) (slot int, err error)`. Het slot telt vanaf 1. De eerste ronde loopt meteen. Na `wait` volgt `ErrSlotsBusy`. Elke andere fout van `TryFile` gaat ongewijzigd terug. Bij een gelukte poging sluit hij de bestanden van de andere slots.

- [ ] **Stap 1: Schrijf de tests.** `TestFixtures` krijgt de soorten `record`, `moved` en `changed_files`, en wordt opgebouwd met `fixturesetup.Build`. Daarbij komen `TestMovedReadsAPythonWrittenMarker` (Review-focus 4) en een tweeling van elke test in `test_gate_slot.py` met dezelfde asserts. Vergelijk daarna `test_deadline.py` en `test_filelock.py` per test met de Go-tests uit stap 5. Schrijf de tweelingen die ontbreken.
  Run: `go test ./internal/commitgate/ ./internal/flock/ ./internal/deadline/` · Verwacht: FAIL.
- [ ] **Stap 2: Port de twee bestanden en `TryFile`.**
- [ ] **Stap 3: Draai de tests.** Run: `go test -race ./internal/commitgate/ ./internal/flock/ ./internal/deadline/ ./internal/bashwrites/ && GOOS=windows go vet ./internal/flock/ ./internal/commitgate/` · Verwacht: PASS.
- [ ] **Stap 4: Mutatiecheck.** Laat `MovedRoots` tijdelijk de marker niet weghalen. Daarna moet de tweeling van de test die een tweede `moved` leeg verwacht, rood worden. Zet het terug.
- [ ] **Stap 5: Commit.** `feat(commitgate): commit_base en gate-slots in Go`

### Taak 4: `commitgate` deel 3 — de gate op een rij wortels

**Bestanden:**
- Maken: `internal/commitgate/gate.go` (`run_gate_roots`, `:318-720`), `internal/commitgate/brake.go` (`:504-550`, `event_count`, `delta`), `internal/commitgate/message.go` (`shell_quote`, `post_untested`, `block`, `slot_failed`, `add_message`, `emit`)
- Maken: `internal/commitgate/gate_test.go`, `internal/commitgate/brake_test.go`, `internal/commitgate/message_test.go`

**Interfaces:**
- Gebruikt: `deadline.RunCapture`, `deadline.Interrupted`; `OpenSlots`, `Acquire`, `SlotDir`, `WaitMemory`, `ChangedFiles` (taak 2 en 3); `config.Run`.
- Levert:
  - `type Result struct{ Code int; Output map[string]any; Stderr string }`. Hij past op `hook.Result`.
  - `type Row struct{ Root, Base, Head string }`. Een lege `Base` betekent een rij van `pre`, zonder `LIGHTHOUSE_GATE_*`.
  - `type Gate struct { Mode string; Root string; Env []string; Bash string; Now func() time.Time; LookPath func(string) (string, error); Run func(deadline time.Time, argv []string, o deadline.Options) (int, []byte, error); Trace string; Start *int64; msgs []string; rows []Row; done int }`. `Mode` is `"pre"` of `"post"`. `Env` is de omgeving voor kinderen, zonder `LIGHTHOUSE_REPO_ROOT`: `run.sh` exporteert die, en de bash-hook gaf hem niet aan het testcommando. `Run` is standaard `deadline.RunCapture`. `Trace` en `Start` komen uit Besluit 5.
  - `func (g *Gate) Note(line string)`: `add_message`.
  - `func (g *Gate) Block(reason string) Result`: `block`, met de meldingen tot dan ervoor (`pending_messages`).
  - `func (g *Gate) Emit(event string) Result`: `emit_message_and_exit`.
  - `func (g *Gate) RunRoots(rows []Row, event string) Result`. Volg de volgorde van `:318-720` regel voor regel. `XDG_RUNTIME_DIR` wordt alleen in `g.Env` gezet (`:507-510`), nooit met `os.Setenv`. Een `*deadline.Interrupted` van `Run` eindigt meteen volgens Besluit 9. Daarna komen de verdicttakken in dezelfde volgorde: OOM, systemd-run, 124, 127, signaal (128 + n), unchecked, rood, groen.
  - `type Brake struct{ Reason string; Wrap []string; Events string }`, `func DetectBrake(slice string, env []string, lookPath func(string) (string, error), run func(argv []string, env []string) (stdout, stderr string, err error)) Brake`
  - `func shellQuote(s string) string`

- [ ] **Stap 1: Schrijf de tests.** Gebruik een nep-`Run` en een nep-`LookPath`, een tijdelijke slotmap en `LIGHTHOUSE_GATE_MEMINFO`. Elke tak van `RunRoots` krijgt een test op de exacte stderr van de bash (kopieer de regels uit `:417-701`). Review-focus 1, 2, 3 en 5 komen erbij. Daarnaast `TestOneDeadlineForAllRoots`: `Now` telt zijn aanroepen. Na twee rijen is hij precies één keer aangeroepen, en de tweede `Run` krijgt dezelfde deadline als de eerste.
  Run: `go test ./internal/commitgate/` · Verwacht: FAIL.
- [ ] **Stap 2: Port de drie bestanden.**
- [ ] **Stap 3: Draai de tests.** Run: `go test -race ./internal/commitgate/ && GOOS=windows go vet ./internal/commitgate/ && GOOS=darwin go vet ./internal/commitgate/` · Verwacht: PASS.
- [ ] **Stap 4: Mutatiecheck.** Zet tijdelijk de 124-tak vóór de OOM-tak. Daarna moet de test op OOM met exit 124 rood worden. Zet het terug.
- [ ] **Stap 5: Commit.** `feat(commitgate): de gate op een rij wortels in Go`

### Taak 5: `Pre`, `Post` en de hook; de black-box-tests via `run.sh`

**Bestanden:**
- Maken: `internal/commitgate/pre.go` (`:751-922`), `internal/commitgate/post.go` (`:725-749`), `internal/commitgate/prepost_test.go`
- Maken: `internal/hooks/committestgate.go`, `internal/hooks/committestgate_test.go`; wijzigen: `internal/hooks/hooks.go` (registratie; `jqFields`/`jqText` eruit), `internal/hook/hook.go` (`JQFields`, `JQText`), de andere aanroepers in `internal/hooks/`
- Wijzigen: `tests/pack/test_commit_test_gate.py`, `tests/pack/test_commit_test_gate_no_verify.py`

**Interfaces:**
- Gebruikt: alles uit taak 2 tot en met 4; `gitinvocations.Find` met `Options{ReportNewDirs: true}`; `repoidentity.SameOrigin`; `jqFields` en `jqText` uit `internal/hooks/hooks.go`. Verplaats die twee naar `internal/hook` als `hook.JQFields` en `hook.JQText`, want `commitgate` mag niet van `hooks` afhangen.
- Levert:
  - `func Pre(ctx context.Context, g *Gate, raw []byte, cwd string) Result` en `func Post(ctx context.Context, g *Gate, raw []byte) Result`. Volg de volgorde van de bash. Een lege `command` geeft exit 0. `cwd` komt uit de payload, of anders uit de procesmap. De tweestapsregel op `git` en `commit` geldt alleen op de twee foutpaden, zoals in `:727-737` en `:755-766`.
  - `func CommitTestGate(ctx context.Context, root, stand string, p hook.Payload) hook.Result`. Hij controleert eerst `git` en `bash` (Besluit 7) en bouwt dan een `Gate` uit `os.Environ()`, `LIGHTHOUSE_GATE_START` en `LIGHTHOUSE_GATE_TRACE`. Een andere stand dan `pre` of `post` geeft de drie regels van `:107-111` op stderr, met exit 2 en zonder stdout.

- [ ] **Stap 1: Zet de tests om.** `HOOK` wordt `hook_shim(<root>, "commit-test-gate")` op een `install_core`-wortel, en de module krijgt `pytestmark = pytest.mark.usefixtures("lighthouse_bin")`. Doe hetzelfde in `test_commit_test_gate_no_verify.py`. Verwerk Besluit 5 en 6: de naden, de twee `deadline`-asserts, de drie verdwenen tests en de PATH-tuples. Voeg `test_rerun_line_survives_a_quote_in_the_command` toe (Review-focus 3). `test_hook_timeout_exceeds_deadline` en `test_hooks_json_registers_pre_and_post` blijven hier ongewijzigd; ze gaan in taak 6 mee. Verander geen andere assert.
  Run: `uv run pytest -q tests/pack/test_commit_test_gate.py tests/pack/test_commit_test_gate_no_verify.py` · Verwacht: FAIL (onbekende hook; `run.sh` blokkeert).
- [ ] **Stap 2: Schrijf `prepost_test.go` en `committestgate_test.go`.** Test `TestMissingToolingBlocks`, `TestSlotErrorBlocks` (Besluit 6), de onbekende stand en een `post` zonder marker. Die laatste is stil en leest geen config (tweeling van `test_post_without_record_reads_no_config`).
- [ ] **Stap 3: Bouw `pre.go`, `post.go` en de hook, en registreer hem.**
- [ ] **Stap 4: Draai de tests.**
  Run: `go build ./... && go test -race ./internal/commitgate/ ./internal/hooks/ ./internal/hook/ && uv run pytest -q tests/pack/test_commit_test_gate.py tests/pack/test_commit_test_gate_no_verify.py tests/pack/test_run_sh.py` · Verwacht: PASS.
- [ ] **Stap 5: Mutatiecheck.** Laat `Pre` tijdelijk de `--no-verify`-check overslaan. Daarna moet `test_precommit_hook_blocks_no_verify` rood worden. Zet het terug.
- [ ] **Stap 6: Commit.** `feat(hooks): commit-test-gate in Go`

### Taak 6: Registratie via `run.sh`; de bash-hook en de Python weg

**Bestanden:**
- Wijzigen: `pack/core/hooks/hooks.json:14,66,92` (Besluit 13)
- Verwijderen: `pack/core/hooks/commit-test-gate.sh`, `pack/core/lib/{commit_base,bash_writes,gate_slot,host_memory,no_verify,deadline,filelock}.py`, en de unittests van Besluit 4
- Wijzigen: `pack/core/requires.toml` (Besluit 14), `pack/SOURCES.toml` (de acht entries), `internal/packs/core_test.go:90-107,128` (de vaste lijst; de telling van `KindHook` als die verandert)
- Wijzigen: `tests/pack/go/test_gofmt.py`, `tests/pack/python/test_ruff_format.py`, `tests/pack/shell/test_shellcheck.py` (Besluit 12). De classify-test verhuist naar `tests/pack/test_classify.py`.
- Wijzigen: `tests/pack/test_hooks_minimal_config.py:97,163,180` (de sleutels worden `run.sh commit-test-gate pre|post`), `tests/pack/test_commit_test_gate.py` (`test_hook_timeout_exceeds_deadline`: zoekt `run.sh commit-test-gate pre`, en `os.access(INSTALLED_HOOKS / "run.sh", os.X_OK)`; `test_hooks_json_registers_pre_and_post`: de nieuwe opdrachten)
- Wijzigen: `e2e/e2e_test.go:8,246,254,260,320,340,364,370,381` (`run.sh commit-test-gate pre|post`; de lijst geïnstalleerde bestanden), `internal/initcmd/select_test.go:361`, `ci/windows-measure.sh:107-140`
- Wijzigen: `docs/site/data/config.json` (en `session.json`, `loop.json` waar ze de hook of een weggehaalde module noemen): de readers worden `internal/commitgate/*.go`. `tests/test_site_config.py:52-53` (`NOT_READERS` op `host_memory.py`) en `:315` (`commit-test-gate.sh` als reader) gaan mee.
- Wijzigen: `CLAUDE.md` (hooklijst: `commit-test-gate` (`run.sh commit-test-gate pre|post`)), de commentaren die `bash_writes.py` noemen (`pack/python/hooks/ruff-format-check.sh:12`, `pack/go/hooks/gofmt-check.sh:10`, `pack/core/hooks/code-language-check.sh:14,32`, `pack/core/lib/repo_identity.py:11` blijft, want bevroren), en elke andere treffer van `git grep -n -e 'commit-test-gate\.sh' -e 'commit_base\.py' -e 'lighthouse_lib\.commit_base' -e 'gate_slot\.py' -e 'host_memory\.py' -e 'no_verify\.py' -e 'deadline\.py' -e 'filelock\.py' -e 'bash_writes\.py' -e 'lighthouse_lib\.bash_writes' -- ':!tests/pack/fixtures' ':!docs/developer/planning' ':!CHANGELOG.md' ':!pack/core/lib' ':!.lighthouse/lib' ':!changelog.d'`. Blijven staan: de configsleutel `host.gate_slots`, het fixturepad `tests/pack/fixtures/bash_writes/`, en synthetische paden in `tests/pack/python/test_gate_python.py`.
- Geïnstalleerd (door `init --upgrade`): `.claude/settings.json`, `.claude/hooks/lighthouse/`, `.lighthouse/lib/lighthouse_lib/` (zeven modules weg), `.claude/skills/`, `.agents/skills/`, `.lighthouse/manifest.toml`

- [ ] **Stap 1: Zet de tests op de nieuwe registratie.** Run: `go test ./internal/packs/ && uv run pytest -q tests/test_dogfood.py tests/pack/test_hooks_minimal_config.py tests/pack/test_commit_test_gate.py::test_hook_timeout_exceeds_deadline tests/pack/test_commit_test_gate.py::test_hooks_json_registers_pre_and_post` · Verwacht: FAIL (`hooks.json` noemt nog de `.sh`).
- [ ] **Stap 2: Pas de bestanden aan, haal weg wat weg moet, en draai `go run ./cmd/lighthouse init --upgrade`.** Controleer daarna twee dingen. `.lighthouse/lib/lighthouse_lib/` mag geen van de zeven modules meer bevatten. `git_invocations.py` en `repo_identity.py` moeten er nog staan.
- [ ] **Stap 3: Draai de tests.**
  Run: `go build ./... && go test -race ./internal/... && go test -race -tags e2e ./e2e/... && uv run pytest -q tests/test_dogfood.py tests/test_instruction_files.py tests/pack/ tests/test_site_session.py tests/test_site_config.py tests/test_site_loop.py tests/test_site_reference.py tests/test_site_packs.py`
  Verwacht: PASS.
- [ ] **Stap 4: Controleer de dogfood.** Draai `go run ./cmd/lighthouse init --upgrade` nog een keer. Verwacht: `No changes.`
- [ ] **Stap 5: Commit.** `feat(core): commit-test-gate via run.sh, bash-hook en zijn Python weg`. Draai geen `go install` vanaf deze branch: dat vervangt de host-binary door ongereviewde code (spec §10 zegt: op `main`, na de merge). De hooks staan als `"$CLAUDE_PROJECT_DIR"/.claude/hooks/lighthouse/run.sh` in `settings.json`, en `run.sh` neemt zijn wortel en `.lighthouse/bin/` van zijn eigen plek (`run.sh:23-27`). Er zijn twee gevallen:
  - De sessie draait vanuit de main checkout (de gewone opzet). Dan registreert ze tot de merge nog de bash-hook van `main`, en die draait ook voor commits in de worktree. Er valt niets te wachten.
  - De sessie draait vanuit de worktree, te zien aan een bestaande `.lighthouse/bin/` daar. Dan start `run.sh` bij `dev` een achtergrondbuild zodra `.lighthouse/bin/stamp` niet gelijk is aan `HEAD` (`run.sh:121-133`). Tot die klaar is, kent de binary de hook niet; dat geeft een melding en geen blokkade (spec §10). Wacht in dit geval vóór de volgende commit tot `cat .lighthouse/bin/stamp` gelijk is aan `git rev-parse HEAD`, ten hoogste 5 minuten. Is hij er dan niet, ga dan door en meld het in het verslag van de taak.

### Taak 7: Changelog en eindcontrole

**Bestanden:**
- Maken: `changelog.d/issue-280-commit-test-gate-go.md`

- [ ] **Stap 1: Schrijf het fragment.** Front matter `type: changed` (minor, spec §10). Tekst in het Engels: `commit-test-gate` runs in the `lighthouse` binary through `.claude/hooks/lighthouse/run.sh`, and needs no `python3` or `jq` any more. Gate slots now also work on Windows (Besluit 10). Update the binary on your PATH before `lighthouse init --upgrade`: an older binary does not know this hook. Noem in de PR de aangepaste assert van Besluit 5, en dat de release na de merge met `workflow_dispatch` gestart moet worden.
- [ ] **Stap 2: Draai de volledige controle.**
  Run: `python3 ci/changelog.py check --base origin/main --head HEAD && go vet ./... && GOOS=windows go vet ./... && GOOS=darwin go vet ./... && golangci-lint run ./... && go test -race ./... && uv run pytest -n auto -q`
  Verwacht: alles groen.
- [ ] **Stap 3: Controleer weg en bevroren (AC 5).** Run: `git diff --stat origin/main -- pack/core/lib/git_invocations.py pack/core/lib/repo_identity.py pack/core/lib/pr_body_gate.py pack/core/lib/config.py pack/core/lib/headings.py pack/core/lib/datadir.py pack/core/lib/protocol.py .lighthouse/lib/lighthouse_lib/git_invocations.py .lighthouse/lib/lighthouse_lib/repo_identity.py .lighthouse/lib/lighthouse_lib/pr_body_gate.py` · Verwacht: geen uitvoer. En `git ls-files pack/core/hooks/commit-test-gate.sh .claude/hooks/lighthouse/commit-test-gate.sh 'pack/core/lib/commit_base.py' 'pack/core/lib/bash_writes.py' 'pack/core/lib/gate_slot.py' 'pack/core/lib/host_memory.py' 'pack/core/lib/no_verify.py' 'pack/core/lib/deadline.py' 'pack/core/lib/filelock.py' '.lighthouse/lib/lighthouse_lib/*'` · Verwacht: alleen modules die nog bevroren zijn of nog afnemers hebben, en geen van de zeven.
- [ ] **Stap 4: Controleer dat de hook geen `python3` meer aanroept.** Run: `grep -rn python3 .claude/hooks/lighthouse/run.sh internal/commitgate internal/hooks/committestgate.go` · Verwacht: geen uitvoer.
- [ ] **Stap 5: Commit.** `docs(changelog): fragment voor #280`
