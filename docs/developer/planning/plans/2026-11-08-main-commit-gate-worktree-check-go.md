# main-commit-gate en worktree-check in Go — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** `main-commit-gate` en `worktree-check` draaien als `lighthouse hook <naam>` achter `run.sh`, op een Go-port van `git_invocations`. Ze geven hetzelfde oordeel en dezelfde tekst als de bash-hooks, en die bash-hooks verdwijnen.

**Architectuur:** De parser `pack/core/lib/git_invocations.py` krijgt een Go-tweeling in `internal/gitinvocations`. De Python-versie blijft bevroren staan, want `bash_writes`, `turn_roots`, `pr_body_gate`, `no_verify` en `commit-test-gate.sh` gebruiken hem nog. Gedeelde fixtures onder `tests/pack/fixtures/git_invocations/` bewijzen dat beide hetzelfde zeggen. De twee hooks komen in een nieuw pakket `internal/hooks`, dat zich bij `internal/hook` registreert. De pytest-tests van de hooks roepen voortaan `run.sh` aan, met de binary die de testsuite één keer bouwt.

**Tech stack:** Go (stdlib, `internal/hook`, `internal/config`), POSIX `sh` (`run.sh`, ongewijzigd), pytest.

**Spec:** [`2026-11-07-zonder-python-design.md`](../specs/2026-11-07-zonder-python-design.md), stap 4 van *Opdeling*, met §1, §2, §9, §10 en *Anti-doelen*. Issue: #277.

## Globale eisen

- Elke hook geeft exact hetzelfde oordeel en dezelfde tekst als zijn bash-voorganger, op dezelfde payloads (spec §2). Kopieer de berichtteksten letterlijk uit `pack/core/hooks/main-commit-gate.sh` en `pack/core/hooks/worktree-check.sh`, regel voor regel, met dezelfde `\n` aan het eind.
- `main-commit-gate` blokkeert met exit 2 en stderr. Hij schrijft nooit naar stdout, behalve de versiemelding die `hook.Run` bij exit 0 toevoegt (spec §3).
- `worktree-check` blokkeert nooit: exit 0, en een eventuele melding als `{"systemMessage": …}` op stdout.
- `pack/core/lib/git_invocations.py` en de geïnstalleerde kopie blijven byte-voor-byte gelijk (bevroren, spec *Anti-doelen*).
- Go volgt `/lighthouse-go-standard`, tests volgen `/lighthouse-test-standard`.
- Commits volgen Conventional Commits, met de berichten in het Nederlands.
- Een wijziging onder `pack/` → `go run ./cmd/lighthouse init --upgrade`, en het resultaat gaat in dezelfde commit mee.

## Besluiten die dit plan neemt

1. **Welke gevallen naar fixtures gaan (AC 2).** Dat zijn de gevallen van de *module* die naar Go gaat: de inline gevallen van `tests/pack/test_git_invocations.py` (spec §9, eerste bullet). De black-box-tests van de hooks blijven pytest en lopen via `run.sh` (§9, derde bullet). Die bewijzen dat het gedrag gelijk is. Niet naar fixtures gaan de Python-eigen tests: `test_cli_contract`, `test_cli_takes_the_default_branch`, `test_hook_lib_stays_importable_on_python_39`, `test_lib_names_do_not_shadow_the_stdlib` en `test_import_reads_no_stdin`. Die horen bij de bevroren Python-CLI. Ze blijven inline, en verdwijnen pas in stap 8 met die CLI.
2. **Tests waarvan het onderwerp verdwijnt.** Zes tests van `main-commit-gate` en twee van `worktree-check` testen tooling die de Go-hook niet meer heeft: `jq`, `python3`, `cat`, `realpath`, de Python-lib, `tomllib` en een crashende Python-parser. Die tests gaan weg, als deel van "wat deze stap weghaalt". Er wordt geen verwachting aangepast. Elke andere test blijft staan met dezelfde asserts. Alleen de helpers veranderen: hoe de hook gestart wordt, en welke tools op een afgeschermd PATH staan. De lijst staat in taak 4 en taak 5.
3. **Registratie:** `${CLAUDE_PLUGIN_ROOT}/hooks/run.sh main-commit-gate` en `${CLAUDE_PLUGIN_ROOT}/hooks/run.sh worktree-check` in `pack/core/hooks/hooks.json`. `blocking` blijft zoals het is: `main-commit-gate` staat erin, `worktree-check` niet.
4. **Geen Go-CLI voor de parser.** `commit-test-gate.sh` roept tot stap 7 de bevroren `git_invocations.py` aan.
5. **De tekst van `worktree-check` blijft letterlijk gelijk**, ook "(main-commit-gate.sh)" in de waarschuwing (spec §2). Eén uitzondering: de melding "cannot resolve '…' (no 'realpath -m' and no python3)." wordt `cannot resolve '<abs>': <fout>.`, want die oorzaak bestaat niet meer.
6. **`requires.toml`:** `main-commit-gate` en `worktree-check` gaan van de regels `jq`, `python3` en `tomllib` af. Onder `git` blijven ze staan.

## Review-focus

Deze vijf inputs dekt geen bestaande test. Ze bijten het eerst bij een port van Python naar Go. Elk ervan krijgt een test in de taak die de code bezit:

1. **Niet-ASCII in een commando** (`echo 'café — ok' && git commit -m x`). Python indexeert per codepunt, Go per byte. De lexer moet op `[]rune` werken. Fixture `non-ascii-before-commit` (taak 1, taak 2).
2. **Andere regeleinden** (`git status\r\ngit commit -m x`, en `\x0b`, `\x0c` of ` ` als scheiding). Waar de Python `splitlines()` of `split("\n")` gebruikt, doet Go precies hetzelfde. Fixtures `crlf-commit` en `vertical-tab-commit` (taak 1, taak 3).
3. **Een cwd of `-C`-pad via een symlink.** Gebruikt de Python `os.path.normpath`/`abspath` (lexicaal) of `realpath`? Go kiest per plek de tegenhanger: `filepath.Clean`/`Join` of `filepath.EvalSymlinks`. Fixture `cwd-is-symlink-to-repo` (taak 1, taak 3).
4. **`~` en `$HOME`:** `HOME` gezet naar een andere map, `~` tussen quotes, en `~user`. `os.path.expanduser` leest `HOME`, en zonder `HOME` de passwd-database. In Go wordt dat `os.Getenv("HOME")`, met `user.Current()` als terugval. `~user` gaat via `user.Lookup`. Fixtures `tilde-home-override` en `tilde-other-user` (taak 1, taak 3).
5. **Een vreemde vorm van de payload.** Bij een JSON-array als payload faalt `jq` (exit 2 bij de gate, een melding bij de check). Bij `null` en bij een ontbrekende `tool_input` komt er niets uit (exit 0). Is `command` een getal, dan is de tekst `5` en is er geen git (exit 0). Nieuwe pytest-tests `test_commit_guard_payload_shapes` (taak 4) en `test_worktree_check_payload_shapes` (taak 5). Ze worden eerst tegen bash groen gedraaid en daarna tegen Go.

---

### Taak 1: Gedeelde fixtures voor `git_invocations`, gedraaid tegen Python

**Bestanden:**
- Maken: `tests/pack/fixtures/git_invocations/cases.json`
- Maken: `tests/pack/test_git_invocations_fixtures.py`
- Wijzigen: `tests/pack/test_git_invocations.py` (de inline gevallen die verhuizen, gaan eruit)

**Interfaces:**
- Levert: het fixture-formaat hieronder, dat taak 2 en 3 in Go lezen.

Formaat: een JSON-array. Elk geval is een object:

```json
{
  "name": "commit-via-dash-c",
  "setup": [
    {"repo": "x", "branch": "main"},
    {"dir": "a"},
    {"file": "wt/.git", "text": "gitdir: /nergens/.git/worktrees/wt\n"},
    {"symlink": "link", "to": "x"}
  ],
  "env": {"HOME": "{root}/home"},
  "kind": "find",
  "command": "git -C {root}/x commit -m x",
  "cwd": "x",
  "default_branch": "main",
  "report_new_dirs": false,
  "expect": [["commit", "{root}/x", "no", "{root}/x"]]
}
```

- `setup`: in volgorde uitgevoerd, relatief aan een lege tijdelijke map `{root}`. `repo` doet wat `init_repo` uit `tests/pack/_testkit.py` doet: `git init -b <branch>`, één commit van `README.md`, en tussenliggende mappen worden aangemaakt. `{"overwrite": "x/.git/HEAD", "text": "garbage\n"}` overschrijft een bestand.
- `kind`: `find`, `walk`, `git_target` of `repo_of`.
  - `find` → `expect` is een lijst van `[action, target, to_main, raw]`, of `"unparseable"`.
  - `walk` → `expect` is een lijst van `[woord op idx, here]`, of `"unparseable"`.
  - `git_target` → `expect` is `{"target": …, "action_word": …}` voor de eerste stap.
  - `repo_of` → `path` (relatief aan `{root}`), en `expect` is een pad of `null`.
- `{home:<naam>}` in `expect` wordt de homedirectory van gebruiker `<naam>`, door de runner opgezocht (niet vast in de fixture).
- `cwd`: relatief aan `{root}`. `""` is `{root}` zelf, en een absoluut pad blijft staan.
- `{root}` in strings wordt de tijdelijke map, zoals de runner hem aanmaakt, **zonder** `realpath`. Zo komt het verschil van Review-focus 3 boven.
- `env`: wordt gezet zolang het geval loopt.

- [ ] **Stap 1: Zet elk inline geval van `test_git_invocations.py` om naar `cases.json`.** Het gaat om `test_finds_commit_with_target` (9), `test_finds_no_commit` (3), de twee `new_dir`-tests (elk twee gevallen: `report_new_dirs` false en true), `test_repo_of_*` (4), `test_open_quote_raises`, `test_walk_*` (3), `test_git_target_follows_dash_c`, `test_heredoc_body_with_commit_text_is_no_invocation` en `test_push_destination_follows_default_branch` (4). Vul `raw` in `expect` met wat de Python teruggeeft (zie stap 3). Voeg ook de gevallen uit Review-focus 1–4 toe: `non-ascii-before-commit`, `crlf-commit`, `vertical-tab-commit`, `cwd-is-symlink-to-repo`, `tilde-home-override` en `tilde-other-user`. `tilde-other-user` gebruikt `~root`. De verwachting is `{home:root}`: een placeholder die de Python-runner vult met `pwd.getpwnam("root").pw_dir` en de Go-runner met `user.Lookup("root").HomeDir`. Zo blijft het geval groen op macOS (`/var/root`) en Linux (`/root`). Voeg ook `unset-variable-path` toe: `git -C $LH_UNSET_VAR commit -m x` met die variabele niet gezet, waarvan de Python-uitkomst `<unknown>` is (de `$` blijft staan, `os.path.expandvars` laat een onbekende naam ongemoeid).
- [ ] **Stap 2: Schrijf `test_git_invocations_fixtures.py`.** Eén test, geparametriseerd per `name`: `test_fixture_case(case, tmp_path, monkeypatch)`. Die bouwt `setup`, zet `env` met `monkeypatch.setenv` en roept `gi.find_invocations`, `gi.walk`, `gi.git_target` of `gi.repo_of` aan. Daarna vergelijkt hij met `expect`, waarin `{root}` vervangen is. `test_markers_are_english_tokens` blijft inline.
- [ ] **Stap 3: Draai de test.**
  Run: `uv run pytest -q tests/pack/test_git_invocations_fixtures.py tests/pack/test_git_invocations.py`
  Verwacht: PASS. Wijkt een `expect` af van wat de Python teruggeeft, dan schrijf je de Python-uitkomst in de fixture, want de fixture legt de Python vast. Gebeurt dat bij een Review-focus-geval, noteer het dan in de commitboodschap.
- [ ] **Stap 4: Mutatiecheck** (`/lighthouse-test-standard`). Verander tijdelijk in de bevroren lib `refspec_target` (laat `removeprefix("refs/heads/")` weg) of de vergelijking `== default_branch` in `push_destination`. (`"main"` als default-parameter van `push_destination` telt niet: `find_invocations` geeft de default branch altijd zelf mee.) Daarna moet ten minste één fixture rood worden. Zet de lib terug met `git checkout pack/core/lib/git_invocations.py`.
- [ ] **Stap 5: Commit.** `test(git-invocations): gevallen naar gedeelde fixtures`

### Taak 2: Go-port deel 1 — lexer, segmenten en `Walk`

**Bestanden:**
- Maken: `internal/gitinvocations/lex.go` (port van `read_delimiter` t/m `split_segments`, regels 92–470)
- Maken: `internal/gitinvocations/walk.go` (port van `resolve` t/m `WalkState`/`walk`, regels 472–880, zonder `push_destination`)
- Maken: `internal/gitinvocations/fixtures_test.go`

**Interfaces:**
- Levert, in package `gitinvocations`:
  - `const Unresolved = "<unknown>"`, `const NewDir = "<new>"`
  - `var ErrUnparseable = errors.New("unparseable: open quote")`
  - `type Step struct { Words []string; Literal, Tildes []bool; Idx int; Here, Raw string; NewDir bool }`. `Here`, `Raw` en `NewDir` zijn een kopie van `dirs["here"]`, `dirs["raw"]` en `dirs["new_dir"]` op het moment van de stap. Zo vervalt de "live dict" van de Python.
  - `func Walk(command, cwd string) ([]Step, error)`
  - `func RepoOf(path string) (string, bool)`, `func ExistingAncestor(path string) (string, bool)`
- Gebruikt: niets uit eerdere taken, behalve `cases.json`.

- [ ] **Stap 1: Schrijf `fixtures_test.go`.** `TestFixtures` leest `../../tests/pack/fixtures/git_invocations/cases.json`, zoals `internal/config/parity_test.go` zijn fixtures leest. Per geval komt er een `t.Run(name)`. De test bouwt `setup` met `os`-calls en `exec.Command("git", …)`, met dezelfde `GIT_AUTHOR_*`- en `GIT_COMMITTER_*`-waarden als `_GIT_ENV` in de tests. `env` gaat via `t.Setenv` (geen `t.Parallel`). Een geval van een soort die nog niet bestaat (`find`, `git_target`) slaat hij over met `t.Skip("taak 3")`.
- [ ] **Stap 2: Draai de test.** Run: `go test ./internal/gitinvocations/` · Verwacht: FAIL (compileert niet).
- [ ] **Stap 3: Port de lexer en `Walk`, functie voor functie.** Houd de Python-namen aan in camelCase. Werk op `[]rune` (Review-focus 1). Volg de Python ook in hoe regels gescheiden worden (Review-focus 2), en in welke padfunctie per plek gebruikt wordt (Review-focus 3). `os.path.expandvars` laat een onbekende of ongeldige `$NAAM` letterlijk staan (dan wordt het pad `Unresolved`): gebruik dus geen `os.ExpandEnv`, maar een eigen `expandVars` met die semantiek. `os.path.expanduser` wordt `expandUser(s string) string`: `HOME` uit de omgeving, daarna `user.Current()`, en voor `~naam` `user.Lookup` (Review-focus 4).
- [ ] **Stap 4: Draai de test.** Run: `go test -race ./internal/gitinvocations/` · Verwacht: PASS voor elk geval van de soorten `walk` en `repo_of`, inclusief een `walk`-geval met `"unparseable"`. Gevallen van de soorten `find` en `git_target` geven SKIP, ook als hun `expect` `"unparseable"` is (zoals `test_open_quote_raises`).
- [ ] **Stap 5: Commit.** `feat(gitinvocations): lexer en Walk in Go`

### Taak 3: Go-port deel 2 — `GitTarget` en `Find`

**Bestanden:**
- Maken: `internal/gitinvocations/find.go` (port van `refspec_target`, `push_destination`, `git_target`, `settle_target` en `find_invocations`)
- Wijzigen: `internal/gitinvocations/fixtures_test.go` (de `t.Skip` gaat weg)

**Interfaces:**
- Gebruikt: `Step`, `Walk`, `RepoOf` en `ExistingAncestor` uit taak 2.
- Levert:
  - `type Options struct { DefaultBranch string; ReportNewDirs bool }`. Een lege `DefaultBranch` betekent `"main"`.
  - `type Invocation struct { Action, Target, ToMain, Raw string }`. `ToMain` is `"yes"`, `"no"`, `"all"` of `"unresolved"`.
  - `func Find(command, cwd string, opts Options) ([]Invocation, error)`. Bij een open quote komt `ErrUnparseable` terug, ingepakt of kaal (`errors.Is`).
  - `func GitTarget(s Step) (target string, known bool, raw string, fromNewDir bool, actionIdx int)`. `known == false` staat voor Python `None`.

- [ ] **Stap 1: Haal de `t.Skip` weg.** Run: `go test ./internal/gitinvocations/` · Verwacht: FAIL op de gevallen van `find` en `git_target`.
- [ ] **Stap 2: Port de vijf functies.** Een Python-`target` die `None` is, wordt `""` met `known=false`. `Find` geeft een `None`-target door als `Unresolved`, zoals `main()` doet bij het afdrukken. Lees `main()` (regel 1022) na en kopieer die omzetting.
- [ ] **Stap 3: Draai de test.** Run: `go test -race ./internal/gitinvocations/` · Verwacht: PASS, geen SKIP.
- [ ] **Stap 4: Mutatiecheck.** Draai `"no"` en `"yes"` in `pushDestination` tijdelijk om. Daarna moet `push-destination-*` rood worden. Zet het terug.
- [ ] **Stap 5: Commit.** `feat(gitinvocations): GitTarget en Find in Go`

### Taak 4: `main-commit-gate` als Go-hook; zijn pytest-tests via `run.sh`

**Bestanden:**
- Maken: `internal/hooks/hooks.go` (`init()` registreert de hooks; de gedeelde helpers `runGit` en `defaultBranch`)
- Maken: `internal/hooks/maincommitgate.go`
- Wijzigen: `internal/cli/hook.go` (blank import `_ "github.com/CorneeldH/lighthouse/internal/hooks"`)
- Wijzigen: `tests/pack/_testkit.py` (`hook_shim`, `RUN_SH_TOOLS`; `install_core` geeft de shim terug)
- Wijzigen: `tests/pack/test_main_commit_gate.py`

**Interfaces:**
- Gebruikt: `gitinvocations.Find`, `Options`, `Invocation`, `Unresolved` en `ErrUnparseable` (taak 3); `hook.Register`, `hook.Payload` en `hook.Result` (`internal/hook/hook.go`); `config.Run` (`internal/config/config.go:758`).
- Levert:
  - `func MainCommitGate(ctx context.Context, root, stand string, p hook.Payload) hook.Result`
  - `func defaultBranch() (branch string, errText string, ok bool)`. Roept `config.Run([]string{"forge.default_branch"}, &out, &errOut)` aan. Bij rc 0 is de uitvoer de waarde, zonder de afsluitende `\n`; anders is `errOut` de tekst. Zo is de foutmelding dezelfde als die van `config.py`.
  - `func runGit(ctx context.Context, dir string, cLocale bool, args ...string) (stdout, stderr string, err error)`. Draait `git -C dir …`, met `LC_ALL=C` in de omgeving als `cLocale` waar is. De afsluitende `\n` gaat van beide strings af.
  - testkit: `RUN_SH_TOOLS: tuple[str, ...]`, alles wat `run.sh` aanroept (`dirname`, `grep`, `sed`, `awk`, `cat`, `mktemp`, `rm`, `head`, `mkdir`, `find`, `touch`, `date`), en `hook_shim(root: Path, name: str) -> Path`. Die schrijft `root/.lighthouse/test-shims/<name>`, met `#!/bin/sh\nexec '<root>/.claude/hooks/lighthouse/run.sh' <name> "$@"\n`, en maakt het uitvoerbaar.

- [ ] **Stap 1: Schrijf `test_commit_guard_payload_shapes` (Review-focus 5) en draai hem tegen de bash-hook.** Vier payloads: `[]` → rc 2 met `COULD NOT CHECK`; `null` → rc 0; `{"cwd": …}` zonder `tool_input` → rc 0; `{"tool_input": {"command": 5}}` → rc 0.
  Run: `uv run pytest -q tests/pack/test_main_commit_gate.py -k payload_shapes` · Verwacht: PASS (bash).
- [ ] **Stap 2: Zet de tests over op `run.sh`.** In `_testkit.py` geeft `install_core` `hook_shim(root, "main-commit-gate")` terug. Daarmee wordt `GUARD` de shim. In `test_main_commit_gate.py` komt `pytestmark = pytest.mark.usefixtures("lighthouse_bin")`, en `_REAL_TOOLS` wordt `(*RUN_SH_TOOLS, "git", "lighthouse", "sh")`. Haal de zes tests weg waarvan het onderwerp verdwijnt (Besluit 2): `test_commit_guard_fails_loud_without_jq`, `…_without_python3`, `…_without_cat`, `test_commit_guard_blocks_when_the_parser_crashes`, `test_guard_blocks_when_its_lib_is_missing` en `test_guard_names_the_python_cause_without_tomllib`. Verander geen enkele assert.
  Run: `uv run pytest -q tests/pack/test_main_commit_gate.py` · Verwacht: FAIL, want de binary kent `main-commit-gate` nog niet (exit 3, en `run.sh` blokkeert).
- [ ] **Stap 3: Bouw `MainCommitGate` als regel-voor-regel-vertaling van `main-commit-gate.sh` vanaf regel 45.** De payload gaat met `json.Unmarshal` in `struct{ ToolInput *struct{ Command json.RawMessage } ; Cwd string }`. Een decodeerfout geeft "the payload is not valid JSON". Een command dat geen string is, wordt zijn JSON-tekst. De preflight vraagt alleen nog `exec.LookPath("git")`, met de tekst `Main-commit gate: COULD NOT CHECK — missing tooling: git` en de twee regels die daarop volgen. Het blok "lib missing" (regels 69–95) vervalt. Lees `forge.default_branch` alleen als de tekst `git` en `commit`/`push` bevat, zoals in regels 101–123. `ErrUnparseable` volgt de tweestappenregel (regels 129–154). Elke andere fout geeft "the command parser failed." Een lege cwd wordt `os.Getwd()`. Een `rev-parse` gaat met `LC_ALL=C`. `CWD_ROOT` en de lus per finding volgen regels 169–268. Zet `hook.Register("main-commit-gate", MainCommitGate)` in `init()` van `hooks.go`.
- [ ] **Stap 4: Draai de tests.**
  Run: `go build ./... && uv run pytest -q tests/pack/test_main_commit_gate.py tests/pack/test_run_sh.py`
  Verwacht: PASS, de vier `payload_shapes`-gevallen inbegrepen. `test_commit_guard_fails_loud_without_tooling` (een leeg PATH) blijft groen via de melding "lighthouse not on PATH" van `run.sh`.
- [ ] **Stap 5: Mutatiecheck.** Draai in `MainCommitGate` tijdelijk de vergelijking `branch != defaultBranch` om. Daarna moeten `test_commit_guard_blocks_commit_on_main` en `…_allows_commit_on_feature_branch` rood worden. Zet het terug.
- [ ] **Stap 6: Commit.** `feat(hooks): main-commit-gate in Go`

### Taak 5: `worktree-check` als Go-hook; zijn pytest-tests via `run.sh`

**Bestanden:**
- Maken: `internal/hooks/worktreecheck.go`
- Wijzigen: `internal/hooks/hooks.go` (registratie)
- Wijzigen: `tests/pack/test_worktree_check.py`

**Interfaces:**
- Gebruikt: `runGit` en `defaultBranch` (taak 4), `hook_shim` en `RUN_SH_TOOLS` (taak 4).
- Levert: `func WorktreeCheck(ctx context.Context, root, stand string, p hook.Payload) hook.Result` en `func canonMissing(path string) (string, error)`. `canonMissing` loopt de onderdelen van links naar rechts af, zoals `realpath -m`. Een onderdeel dat bestaat, wordt via `filepath.EvalSymlinks` opgelost. `..` neemt de parent van wat al opgelost is. Een onderdeel dat niet bestaat, wordt lexicaal aangeplakt.

- [ ] **Stap 1: Schrijf `test_worktree_check_payload_shapes` (Review-focus 5) en draai hem tegen bash.** `[]` → rc 0 en een `systemMessage` met `COULD NOT CHECK`; `null` → rc 0 zonder stdout; `{"tool_input": {}}` → rc 0 zonder stdout.
  Run: `uv run pytest -q tests/pack/test_worktree_check.py -k payload_shapes` · Verwacht: PASS (bash).
- [ ] **Stap 2: Zet de tests over op `run.sh`.** `WORKTREE_GUARD = hook_shim(INSTALL_ROOT, "worktree-check")`, en `pytestmark = pytest.mark.usefixtures("lighthouse_bin")`. `_TOOLS` wordt `(*RUN_SH_TOOLS, "git", "lighthouse", "sh")`. Haal de twee tests weg waarvan het onderwerp verdwijnt: `test_worktree_check_warns_without_jq` en `test_worktree_check_warns_when_the_path_cannot_be_resolved`. Verander geen assert.
  Run: `uv run pytest -q tests/pack/test_worktree_check.py` · Verwacht: FAIL. De binary kent `worktree-check` nog niet: exit 3, en `run.sh` geeft een melding (geen blokkade). De asserts op stilte falen.
- [ ] **Stap 3: Bouw `WorktreeCheck` als vertaling van `worktree-check.sh` vanaf regel 48.** De preflight vraagt alleen `git`, met de tekst `Worktree check: COULD NOT CHECK — missing tooling: git. Nothing was checked, so this is no sign that the write is in a worktree.` (regel 36). `warn_uncheckable` wordt `Result{Output: {"systemMessage": msg}}`, met de tekst van regels 21–25. `rev-parse --git-dir` en `--git-common-dir` lopen met `cmd.Dir = DIR` en `LC_ALL=C`. Van de uitvoer wordt stdout en stderr samen gelezen (`2>&1`). `rev-parse` geeft in een gelinkte worktree en in een submap een *absoluut* pad, en `filepath.Join` laat een absoluut tweede deel niet winnen. Doe dus: afsluitende `\n` eraf, `if !filepath.IsAbs(raw) { raw = filepath.Join(DIR, raw) }`, en dan `EvalSymlinks`. Dat geldt voor `GIT_DIR` en `GIT_COMMON`. Voeg een test toe met een schrijfactie in een submap van een worktree (`test_worktree_check_silent_in_worktree_subdir`). De waarschuwing komt letterlijk uit regels 111–120.
- [ ] **Stap 4: Draai de tests.**
  Run: `go build ./... && uv run pytest -q tests/pack/test_worktree_check.py tests/pack/test_main_commit_gate.py`
  Verwacht: PASS.
- [ ] **Stap 5: Mutatiecheck.** Laat `GIT_DIR != GIT_COMMON` tijdelijk niet meer stil worden. Daarna moet `test_worktree_check_silent_inside_worktree` rood worden. Zet het terug.
- [ ] **Stap 6: Commit.** `feat(hooks): worktree-check in Go`

### Taak 6: Registratie via `run.sh`, de bash-hooks weg

**Bestanden:**
- Wijzigen: `pack/core/hooks/hooks.json:9,34`
- Verwijderen: `pack/core/hooks/main-commit-gate.sh`, `pack/core/hooks/worktree-check.sh`
- Wijzigen: `pack/core/requires.toml` (Besluit 6)
- Wijzigen: `pack/core/scripts/check-instructions.sh` (check 14, hooknamen): bij een command `…/lighthouse/run.sh <naam> [stand]` is de hooknaam het eerste argument na `run.sh`, niet de scriptnaam (`run`, dat op geen van `-gate`/`-check` eindigt). Plus een geval in `tests/pack/test_check_instructions.py` voor een `run.sh`-registratie. De geïnstalleerde kopie volgt via `init --upgrade`.
- Wijzigen: `e2e/e2e_test.go`: `runHook` en `runGate` zetten de map van de gebouwde binary vooraan op `PATH` (geef `bin` mee), want `run.sh` zoekt `lighthouse` op `PATH`. Werk ook de pakketcommentaar bij.
- Wijzigen: `docs/site/map.js` (`REFERENCE_PARTS.hooks` gebruikt voor een hook zonder `.sh` het veld `source` uit `docs/site/data/packs.json` letterlijk als repopad: `internal/hooks/maincommitgate.go` en `internal/hooks/worktreecheck.go`), `tests/test_site_packs.py` (`registered()` leest bij `run.sh <naam>` de naam uit het argument), `docs/site/data/packs.json`, `docs/site/data/session.json` (command `run.sh main-commit-gate`; `test_site_session` eist dat de bron op `core/hooks/run.sh` eindigt, dus de chipbron is `pack/core/hooks/run.sh`) en `docs/site/data/config.json` (readers: de twee `.sh` eruit, de nieuwe `internal/hooks`-bestanden die `forge.default_branch` lezen erin)
- Wijzigen: elke verwijzing die `git grep -n -e 'main-commit-gate\.sh' -e 'worktree-check\.sh' -- ':!docs/developer/planning' ':!CHANGELOG.md'` vindt, behalve `pack/core/lib/git_invocations.py` en zijn geïnstalleerde kopie (bevroren) en `internal/hooks/` (de letterlijke waarschuwingstekst van Besluit 5 noemt `(main-commit-gate.sh)` en blijft zo). Gebruik `-- ':!internal/hooks'`. Volgens de scout zijn dat: `tests/test_dogfood.py`, `tests/pack/test_hooks_minimal_config.py`, `tests/pack/test_check_instructions.py`, `tests/test_site_session.py`, `tests/test_site_config.py`, `tests/fixtures/site/map.json`, `docs/site/data/{session,config,loop}.json`, `docs/site/session.html`, `internal/packs/core_test.go`, `internal/initcmd/select_test.go`, `ci/windows-measure.sh`, `pack/SOURCES.toml`, `pack/core/hooks/commit-test-gate.sh` (alleen het commentaar), `pack/core/skills/fix-pr/{claude,agents}/SKILL.md` en de hooklijst in `CLAUDE.md`
- Geïnstalleerd (door `init --upgrade`): `.claude/settings.json`, `.claude/hooks/lighthouse/`, `.claude/skills/`, `.agents/skills/`, `.lighthouse/manifest.toml`

**Interfaces:**
- Gebruikt: de geregistreerde hooks uit taak 4 en 5, en `hook_shim` en `RUN_SH_TOOLS` uit taak 4.

- [ ] **Stap 1: Zet de registraties en hun tests om.**
  - In `tests/test_dogfood.py` zoekt `_registered("Bash", …)` naar `"run.sh main-commit-gate"` en `"run.sh worktree-check"`. `_run_registered` krijgt een PATH uit `tool_bin(tmp_path / "bin", (*RUN_SH_TOOLS, "git", "lighthouse", "sh"))`, zonder `go`. Dan bouwt `run.sh` in deze `dev`-checkout niets en neemt hij de testbinary. De module krijgt `usefixtures("lighthouse_bin")`.
  - In `test_hooks_minimal_config.py` worden de sleutels in `_payload`, `test_every_registration_has_a_payload` en `GUARDED` `run.sh main-commit-gate` en `run.sh worktree-check`. De runner start `run.sh` met de hooknaam als argument. `guard_env` haalt elke `lighthouse` van `PATH`, ook die van `lighthouse_bin`; geef dus `PATH=f"{lighthouse_bin.parent}{os.pathsep}{env['PATH']}"` als extra mee aan `guard_env` voor de Go-hooks. `test_without_lib_the_guarded_command_blocks` en `test_guarded_covers_every_blocking_bash_hook` gaan alleen over hooks die nog `.lighthouse/lib` lezen: sluit de `run.sh`-registraties daar uit (een Go-hook heeft geen lib), en voeg een aparte test toe dat `run.sh main-commit-gate` zonder `.lighthouse/lib` een commit op `main` nog steeds blokkeert. Dit is een test waarvan het onderwerp verdwijnt (Besluit 2).
  - In `internal/packs/core_test.go` gaan de twee `.sh`-bestanden uit de vaste lijst. In `select_test.go:361` komt een bestand dat nog bestaat.
  Run: `go test ./internal/packs/ ./internal/initcmd/ && uv run pytest -q tests/test_dogfood.py tests/pack/test_hooks_minimal_config.py` · Verwacht: FAIL (`hooks.json` noemt nog de `.sh`).
- [ ] **Stap 2: Pas `hooks.json` en `requires.toml` aan, haal de twee `.sh` weg, en werk de overige verwijzingen uit de lijst hierboven bij.** In docs en site-data wordt de naam `main-commit-gate` (zonder `.sh`), waar de tekst een hook noemt en niet een bestand. Draai `go run ./cmd/lighthouse init --upgrade`, en controleer dat `.claude/hooks/lighthouse/main-commit-gate.sh` en `worktree-check.sh` weg zijn.
- [ ] **Stap 3: Draai de tests.**
  Run: `go build ./... && go test -race ./internal/... && go test -race -tags e2e ./e2e/... && uv run pytest -q tests/test_dogfood.py tests/pack/ tests/test_site_session.py tests/test_site_config.py tests/test_site_loop.py tests/test_site_reference.py tests/test_site_packs.py`
  Verwacht: PASS.
- [ ] **Stap 4: Controleer de dogfood.** Draai `go run ./cmd/lighthouse init --upgrade` nog een keer. Verwacht: `No changes.`
- [ ] **Stap 5: Commit.** `feat(core): main-commit-gate en worktree-check via run.sh, bash-hooks weg`. Let op: na deze commit registreert de eigen sessie `run.sh main-commit-gate`. `run.sh` bouwt `.lighthouse/bin/lighthouse` op de achtergrond. Tot die build klaar is, kent de PATH-binary de hook niet. Bij `dev` geeft dat een melding en geen blokkade (spec §10).

### Taak 7: Changelog en eindcontrole

**Bestanden:**
- Maken: `changelog.d/issue-277-main-commit-gate-worktree-check-go.md`

- [ ] **Stap 1: Schrijf het fragment.** Front matter `type: changed` (dat geeft een minor, spec §10). Een push naar `main` geeft alleen een release bij een `fixed`-fragment (`ci/changelog.py`); een `changed`-fragment wacht dus op de wekelijkse cron. Noem in de PR dat de release na de merge met `workflow_dispatch` gestart moet worden (spec §10). Tekst in het Engels, zoals `changelog.d/issue-274-fundament-zonder-python.md`: `main-commit-gate` and `worktree-check` run in the `lighthouse` binary through `.claude/hooks/lighthouse/run.sh`. They no longer need `python3` or `jq`. Update the binary on your PATH before `lighthouse init --upgrade`: an older binary does not know these hooks, and `main-commit-gate` then blocks every Bash call.
- [ ] **Stap 2: Draai de volledige controle.**
  Run: `python3 ci/changelog.py check --base origin/main --head HEAD && go vet ./... && golangci-lint run ./... && go test -race ./... && uv run pytest -n auto -q`
  Verwacht: alles groen.
- [ ] **Stap 3: Controleer of de bevroren module bevroren bleef.** Run: `git diff --stat origin/main -- pack/core/lib/git_invocations.py .lighthouse/lib/lighthouse_lib/git_invocations.py` · Verwacht: geen uitvoer.
- [ ] **Stap 4: Commit.** `docs(changelog): fragment voor #277`
