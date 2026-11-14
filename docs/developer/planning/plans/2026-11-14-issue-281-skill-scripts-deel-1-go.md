# Skill-scripts deel 1 in Go — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** `post_artefact.py`, `board_start.py`, `unblock_dependents.py` en `worktree_audit.py` worden `lighthouse post-artefact`, `lighthouse board-start`, `lighthouse unblock-dependents` en `lighthouse worktree-audit`, met dezelfde argumenten, uitvoer en exitcodes. De workflow `unblock-dependents` draait de binary zonder `uv` en Python. Met de laatste afnemers verdwijnen `artefacts.py`, `protocol.py`, `pr_body_gate.py`, `git_invocations.py` en `repo_identity.py`.

**Architectuur:** Elk script wordt een eigen pakket onder `internal/` met een dunne cobra-wrapper in `internal/cli/`. Alle aanroepen van `gh` en `git` lopen via één nieuwe naad, `internal/procrun`. Zo blijven argv, stderr en de detectie van `HTTP 404` gelijk aan de Python, en kan een test elke aanroep naspelen. Het formaat van artefact-comments komt in `internal/protocol`; dat pakket wordt de enige lezer van het protocol. Gedrag gaat eerst naar gedeelde fixtures, die tegen de Python groen draaien. Daarna lezen Go en een black-box-pytest op de gebouwde binary dezelfde fixtures, en gaat de Python weg.

**Tech stack:** Go (stdlib, cobra, `internal/config`, `internal/protocol`, `internal/prbody`, `internal/fixturesetup`), pytest, GitHub Actions (`gh release download`, `actions/setup-go`).

**Spec:** [`2026-11-07-zonder-python-design.md`](../specs/2026-11-07-zonder-python-design.md), stap 8 van *Opdeling*, met §4, §6, §7, §9, §10 en *Anti-doelen*. Issue: #281. Vorige stap als patroon: [`2026-11-13-issue-280-commit-test-gate-go.md`](2026-11-13-issue-280-commit-test-gate-go.md).

## Globale eisen

- Elke uitvoerregel, elke foutregel en elke exitcode komt letterlijk uit het Python-script dat de taak vervangt. Uitzonderingen staan alleen in *Besluiten*.
- `gh` en `git` krijgen dezelfde argv als in de Python, ook de GraphQL-query en `--jq`-filters. Timeouts: `board_start` 60 s, `unblock_dependents` 60 s, `post_artefact` 300 s.
- Bevroren tot de taak die ze weghaalt, byte voor byte gelijk aan `origin/main`: `pack/core/lib/{config,headings,datadir,protocol,pr_body_gate,git_invocations,repo_identity}.py` en hun kopieën onder `.lighthouse/lib/lighthouse_lib/`. `config.py`, `headings.py` en `datadir.py` blijven ook na deze PR staan (stap 12).
- Go volgt `/lighthouse-go-standard` en tests volgen `/lighthouse-test-standard`. Alles compileert ook met `GOOS=windows` en `GOOS=darwin`.
- Commits volgen Conventional Commits, met de berichten in het Nederlands. Instructietekst (SKILL.md, uitvoer van de commando's) blijft Engels.
- Verandert een commit iets onder `pack/`, dan draait eerst `go run ./cmd/lighthouse init --upgrade`, en het resultaat gaat mee in dezelfde commit.
- Testgate: `[test_gate] command` uit `lighthouse.toml` (`go test -race` plus pytest). Een rode commit los je op met een nieuwe commit, nooit met `--no-verify`.

## Besluiten die dit plan neemt

1. **Eén PR, vier taken per script.** De volgorde komt uit de importgraaf. `board-start` en `unblock-dependents` hebben geen lib-afnemers. `post-artefact` is de laatste afnemer van `artefacts.py`, en daarmee van `protocol.py`. `worktree-audit` is de laatste afnemer van `pr_body_gate.py`, en daarmee van `git_invocations.py` en `repo_identity.py`. Elke taak haalt weg wat zijn laatste afnemer verliest (*Anti-doelen*) en werkt in dezelfde commit de lijsten bij die dat bestand noemen (`internal/packs/core_test.go:78-92`, `pack/SOURCES.toml` met `tests/test_sources.py`, `docs/site/data/*.json` met `tests/test_site_config.py`, `pack/core/requires.toml`), dus de testgate is na elke taak groen. Taak 6 houdt alleen de eindmeting over.
2. **`gh` en `git` via exec, niet via `forge.Client`.** `forge.Client` praat HTTP met een token, en zijn fouten zien er anders uit. De scripts lezen `HTTP 404`/`HTTP 422` uit de stderr van `gh` (`post_artefact.py:118`, `unblock_dependents.py:127`), en de workflow levert alleen `GH_TOKEN` aan `gh`. Ook `internal/prbody/gate.go:32` roept `gh` aan. Nieuw pakket `internal/procrun`:
   - `type Result struct{ Stdout, Stderr string; Code int }`
   - `type Func func(ctx context.Context, dir string, stdin *string, name string, args ...string) (Result, error)` (`dir` is de werkmap, leeg = huidige; `worktree_audit.run_git` draait met `cwd=`, `worktree_audit.py:460-475`, en de argv blijft zo gelijk aan de Python)
   - `func Exec(timeout time.Duration) Func`: echte uitvoering. Een timeout geeft `*Timeout{After time.Duration}`, een ontbrekend programma `*NotFound{Name string}`, en een exitcode ≠ 0 geen error maar `Result.Code`.
3. **Foutnamen blijven de Python-namen.** `board_start` drukt de klassenaam van de exception af (`board: skipped (CalledProcessError)`, `board: ERROR — ConfigError: <msg>`). De skilltekst noemt die namen niet, maar de fixtures zijn het bewijs dat de uitvoer gelijk blijft, en die moeten tegen beide implementaties draaien. Daarom gebruikt Go een vaste vertaling: exit ≠ 0 → `CalledProcessError`, `*procrun.Timeout` → `TimeoutExpired`, `*procrun.NotFound` → `FileNotFoundError`, ongeldige JSON → `JSONDecodeError`, ontbrekend veld → `KeyError`, verkeerd type → `TypeError`, configfout → `ConfigError: <bericht op één regel>`. De tabel staat één keer in `internal/procrun/pyname.go` als `func PyName(err error) string`.
4. **Gebruiksfouten.** `post-artefact`, `unblock-dependents` en `worktree-audit` gebruiken `scriptCommand` (`internal/cli/scripts.go:14`): een verkeerde vlag geeft exit 2, zoals argparse. `scriptCommand` weigert elk positioneel argument, dus `post-artefact <nr> <file>` zet daarna een eigen `Args` die `usageErr` teruggeeft bij een ander aantal dan twee (precedent: `withArgs` in `internal/cli/planreviewrecord.go:51-62`), en `unblock-dependents` controleert `--issue` zelf met `usageErr` (een cobra-`MarkFlagRequired` geeft exit 1). `worktree-audit` houdt de weergavevlaggen onderling exclusief (exit 2) en kent `--repo <pad>` (`worktree_audit.py:1515-1549`). De usagetekst zelf is die van cobra; die verschilt, en geen skill leest hem. `board-start` neemt zijn argumenten rauw (`DisableFlagParsing: true`), want `<nr>` en `--reopen <nr>` zijn daar positioneel. De usageregel verandert van naam: `board: usage: lighthouse board-start <nr>` met exit 0, en `board: usage: lighthouse board-start --reopen <nr>` met exit 1. Dat is de enige tekst die verandert.
5. **Fixtures voor scripts: nagespeelde aanroepen.** Per commando komt er `tests/pack/fixtures/scripts/<commando>/cases.json`, een lijst van:
   ```json
   {"name": "test_<naam van de Python-test die het vervangt>",
    "argv": ["123"], "config": "<inhoud lighthouse.toml>", "env": {},
    "setup": [/* stappen in het formaat van fixturesetup, optioneel */],
    "calls": [{"argv": ["gh", "api", "..."], "stdin": null, "stdout": "...", "stderr": "", "code": 0}],
    "expect": {"stdout": "...", "stderr": "...", "code": 0, "files": {"<pad>": "<inhoud>"}}}
   ```
   `expect` kent naast `stdout` en `stderr` ook `stdout_contains` en `stderr_contains` (lijsten), voor help- en usage-gevallen waar de tekst van argparse en cobra verschilt (besluit 4): zo'n geval gebruikt alleen die velden en draait tegen beide implementaties. `calls` is een geordend script. Een aanroep die niet de volgende in de lijst is, of een lijst die niet op raakt, laat de test falen. Placeholders (`{root}`, `{rev:…}`) vult `fixturesetup.Fill`. `git` draait echt tegen de `setup`-repo, alleen `gh` wordt nagespeeld. Een `calls`-regel is alleen voor `gh`: de Python-runner heeft alleen een nep-`gh`, dus een `git`-regel kan daar nooit opraken. Wil een geval een `git`-fout, dan bouwt `setup` een kapotte repo (geen `git`-regels in `calls`). Een `gh`-regel met `"timeout": true` laat de Go-`Replay` een `*procrun.Timeout` geven; de Python-nep-`gh` slaapt dan voorbij de timeout van het commando, en de runner zet die timeout via `LIGHTHOUSE_TEST_TIMEOUT_SCALE` omlaag. Faalt dat in Python, dan is het geval alleen-Go (`"only": "go"`) en staat de reden in `name`.
   - Python: `tests/pack/_fakegh.py` is een nep-`gh` op het PATH. Hij leest `FAKE_CALLS` (JSON-pad) en een tellerbestand, en speelt de aanroepen na. Bij een afwijking schrijft hij naar `FAKE_LOG` en eindigt met exit 97. De runner `tests/pack/test_script_cases.py` is geparametriseerd over alle `cases.json` en zet `TZ=UTC`, `LIGHTHOUSE_TOML` en `HOME` in een tmp-map.
   - Go: `internal/scriptcase` (alleen voor tests, zoals `fixturesetup`), met `Load(t, path) []Case` en `Replay(t, calls) procrun.Func`. `git` gaat altijd naar `procrun.Exec`.
   - De runner kiest per commando wat hij start, met `COMMANDS = {"board-start": [...]}`. In de taak die het Python-script vervangt, gaat die regel van `python3 <script>` naar `[lighthouse_dir()/"lighthouse", "<commando>"]`. Zo is dezelfde pytest eerst het bewijs dat de fixture de Python beschrijft, en daarna de black-box-test van de binary (spec §9).
6. **Wat geen script-fixture is.** Pure functies (`judge`, `build_rows`, `parse_*`, `render_*` van `worktree_audit`; `parse_meta`, `render`, `split`, `current`, `next_version` van `artefacts`) krijgen data-fixtures: `tests/pack/fixtures/worktree_audit/{judge,rows,parse,render}.json` en `tests/pack/fixtures/artefacts/cases.json`. Elk geval heeft `name` gelijk aan de Python-test. Een Python-test over processen, tijd of het bestandssysteem (`pid_alive`, `find_orphans`, `_load_sessions`, `latest_transcript`) krijgt een Go-tweeling met dezelfde asserts en een doc-comment `// twin of <test_naam>`.
7. **De dekkingsregel, meetbaar.** Elke `test_*` in een pytest-bestand dat deze PR weghaalt, komt terug als `name` in een fixture of als `twin of <naam>` in een Go-test. Taak 6 meet dat met één commando. Voor `test_pr_body_gate*.py`, `test_repo_identity*.py` en `test_git_invocations*.py` lezen de Go-tests de fixtures al (`internal/prbody/fixtures_test.go:214`, `internal/repoidentity/fixtures_test.go:27`, `internal/gitinvocations/fixtures_test.go`). Alleen hun inline-tests vallen onder de regel; de drie `test_*_fixtures.py`-runners (één test elk) zijn een Python-runner van de fixtures en vallen er niet onder, taak 6 slaat ze over. De inline-tests van `test_pr_body_gate.py` (43), `test_git_invocations.py` (6) en `test_repo_identity.py` (1) hebben nog geen Go-tegenhanger: taak 5 stap 1b zet ze om. Voor `test_protocol.py` geldt hetzelfde, tegen `internal/protocol/*_test.go`.
8. **`internal/protocol` krijgt de artefact-comments** (AC 3). Het artefactformaat is een `<!-- lighthouse: {...} -->`-marker, dus het hoort bij het protocol. Nieuw bestand `internal/protocol/artefact.go`:
   - `type ArtefactMeta struct{ Kind string; Version, Part, Parts int; File, Commit, Hash string; Replaces *string; Extra *orderedjson.Object }`. `Extra` (nil = geen) houdt de onbekende sleutels vast in volgorde (`internal/orderedjson/orderedjson.go:19`, `Keys()`/`Get`/`Set`; zoals `meta.extra` in `artefacts.py:63-64`); `Parse` vult ze, `Render` schrijft ze na de acht vaste sleutels terug (`test_unknown_fields_survive`).
   - `func ParseArtefactMeta(body string) (ArtefactMeta, bool)`
   - `func RenderArtefact(m ArtefactMeta, content, language, repo string) (string, error)`. `repo` is `forge.repo`; de bronlink is `https://github.com/<repo>/blob/<commit>/<pad>` (`artefacts.py:123-136`) en `internal/protocol` leest zelf geen config. Een lege `repo` geeft de fout `forge.repo is not set; an artefact links to its source`; de aanroeper (`postartefact`) zet er het configpad voor, zoals Python (`test_render_without_repo_is_a_config_error`). Een artefact-fixturegeval krijgt een optioneel veld `"repo"` en `"language"` onder `input`; de Python-runner schrijft ze als `lighthouse.toml` naar `LIGHTHOUSE_TOML`. Byte-gelijkheid met Python: `html.escape` geeft `&#x27;` en `&quot;` (Go: `&#39;`, `&#34;`), `json.dumps(ensure_ascii=False)` laat `<>&` staan (Go `json.Marshal` escapet ze, zet `SetEscapeHTML(false)`), en `str.splitlines` knipt ook op `\x0b \x0c \x1c-\x1e \x85 \u2028 \u2029`. Elk krijgt een fixturegeval in taak 4 (`test_render_escapes_quote_and_ampersand`, `test_render_keeps_angle_brackets`, `test_split_on_unicode_line_breaks`).
   - `func ArtefactHash(data []byte) string`
   - `func SplitArtefact(text string, limit int) []string` (standaard `limit = 60000`)
   - `func CurrentArtefact(cs []Comment, kind, file string) []Comment`
   - `func NextArtefactVersion(cs []Comment, kind, file string) int`

   De constanten blijven gelijk: `GITHUB_LIMIT = 65536`, `KINDS = spec, plan`, `_LEGACY_FIELDS`. Hun waarden komen uit `artefacts.py`.
9. **De workflow (spec §6).** De stap `Install uv` gaat weg, en vóór het App-token komt de stap `Get lighthouse`:
   - Een regel `version = "dev"` in `.lighthouse/manifest.toml` (de eerste `^version =`, zoals `run.sh:71`) plus `cmd/lighthouse/main.go` in de checkout: dan `actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0` met `go-version-file: go.mod` (een aparte stap met `if:` op een output van de versiestap), en `go build -o "$RUNNER_TEMP/bin/lighthouse" ./cmd/lighthouse`.
   - `dev` zonder `cmd/lighthouse`: `::error::` plus exit 1.
   - Anders: `gh release download "v$version" --repo CorneeldH/lighthouse --pattern "lighthouse_${os}_${arch}" --pattern SHA256SUMS --dir "$RUNNER_TEMP/bin"`, en daarna `sha256sum --check --ignore-missing SHA256SUMS`. `os` komt uit `RUNNER_OS` (`Linux` → `linux`, `macOS` → `darwin`), `arch` uit `RUNNER_ARCH` (`X64` → `amd64`, `ARM64` → `arm64`). Een andere waarde geeft `::error::` plus exit 1. Daarna `chmod +x`, de binary wordt `lighthouse`, en `$RUNNER_TEMP/bin` gaat in `$GITHUB_PATH`. `CorneeldH` staat in `ci/literal-denylist.txt:10`: de `--repo`-regel krijgt daarom in taak 3 een exacte regel in `ci/literal-allowlist.txt` (`pack/core/workflows/unblock-dependents.yml:<regeltekst>`, met een pinnende test zoals de `run.sh`-regel in `tests/pack/test_run_sh.py`), anders worden `test_pack_is_clean` en de literal-grep-stap van CI rood.
   - De download gebruikt `GH_TOKEN: ${{ github.token }}`: de release is publiek, en het App-token is alleen voor schrijven.
   - De releasestap wordt `run: lighthouse unblock-dependents --issue "$ISSUE"`.
10. **Python-tests van gebleven skills.** `test_pr_create_gate.py` haalt een helper uit `test_pr_body_gate.py`; die helper verhuist naar `tests/pack/_testkit.py`. `test_write_pr_skill.py:128,178` en `test_close_issue_skill.py:349` gebruiken `gate.evidence_rows`, `gate.check_body` en `gate.refs_targets` als orakel; `lighthouse pr-body-gate` (`internal/cli/prbodygate.go:15-40`) leest een `gh pr create`-commando en geeft die waarden niet terug. De `jq`-filter-pariteit van `close-issue` (de acht bodies, `147 in refs_targets`) verhuist daarom naar een Go-test `TestRefsTargetsMatchesCloseIssueJqFilter` in `internal/prbody` over dezelfde bodies, die het `jq`-filter uit de SKILL.md haalt en draait (overslaan als `jq` ontbreekt, zoals nu). `evidence_rows` en `check_body` zijn al gedekt door `internal/prbody/fixtures_test.go`; de twee write-pr-tests worden daar gevallen of vallen weg met een regel in de commit. `tests/pack/python/test_prcov_issues.py:19,378-405` gebruikt `protocol.parse_handoff(protocol.parse_header(body))` en geen robot: die tests gaan via `subprocess` naar `lighthouse protocol parse-header` (`internal/cli/protocol.go:26-55`, JSON op stdout) en asserten op de velden (`issue`, `worktree`, `branch`, `anchor`, `next`, `when`, `kind`). Zijn de typen daar anders dan `parse_handoff` gaf, dan wordt de assert de veldwaarde als string; een test die méér dan `parse-header` kan, krijgt een Go-tweeling in `internal/protocol`. `tests/pack/fixtures/code_language/bash_writes_nl.py` is corpusdata die niet draait en blijft staan.

## Review-focus

Geen bestaande test dekt deze zes inputs. Elk krijgt een test in de taak die de code bezit:

1. **Een labelnaam met spatie, `:`, `/` of `+`** in `unblock-dependents`. `quote(label, safe='')` codeert alles behalve `A-Za-z0-9_.-~`. `url.PathEscape` laat `:@&=+$,;` staan, en dan wijst de DELETE naar een ander pad. Fixturegeval `test_label_with_reserved_characters_is_fully_encoded` met label `3: blocked/issue +x` (taak 3).
2. **Een planbestand boven 60.000 tekens met fences die over de knip heen lopen**, plus multibyte-tekens precies op de grens. Python telt codepunten, Go standaard bytes. Fixturegeval `test_split_counts_code_points_at_the_fence` (taak 4).
3. **`gh`-uitvoer met een afsluitende newline of witruimte.** `post_artefact._run` doet `.strip()` (`"0\n"` → `"0"` bij `open_pr`), `board_start` niet. Fixturegeval `test_open_pr_count_with_trailing_newline` (taak 4) en `test_graphql_reply_with_trailing_newline` (taak 2).
4. **De workflow op een runner die niet `X64` is, of op een `dev`-repo zonder `cmd/lighthouse`.** Wie dat stil laat passeren, draait niets en laat labels staan. Test `test_get_lighthouse_maps_runner_and_refuses_unknown` in `test_workflow_unblock.py` (taak 3). Die extraheert het `run:`-blok van `Get lighthouse` en draait het met `bash` in een tmp-map, met een nep-`gh`, nep-`sha256sum` en nep-`go` op het PATH, voor `X64`, `ARM64`, `X86` (fout) en `dev` zonder `cmd/` (fout).
5. **`worktree-audit` zonder `~/.claude/sessions`, met een kapotte sessie-JSON, of met een pid die niet te meten is.** Een rij die niet te meten is, wordt nooit `cleanup: yes`. Go-tweelingen `TestMissingSessionsDirIsNoSessions`, `TestBrokenSessionJSONIsSkipped` en `TestUnmeasurablePidBlocksCleanup` (taak 5).
6. **Python-tekstsemantiek in `render`/`split`/`post_artefact`.** `html.escape`, `json.dumps` en `str.splitlines` verschillen van hun Go-evenknie (besluit 8); de drie gevallen daar zijn het bewijs (taak 4).

---

### Taak 1: `procrun`, `scriptcase` en de fixture-runner

**Files:**
- Create: `internal/procrun/procrun.go`, `internal/procrun/pyname.go`, `internal/procrun/procrun_test.go`
- Create: `internal/scriptcase/scriptcase.go`
- Create: `tests/pack/_fakegh.py`, `tests/pack/test_script_cases.py`
- Modify: `tests/pack/_testkit.py` (helper `fake_gh_bin(tmp_path, calls) -> tuple[Path, Path]`), `internal/fixturesetup/fixturesetup.go` en `build_fixture_setup` in `tests/pack/_testkit.py` (nieuwe stap `bare`: `git init --bare <pad>`, voor de kale `origin` van taak 4; `fixturesetup` kent nu alleen `origin` als `git remote add`, `fixturesetup.go:82-140`)

**Interfaces:**
- Produces: `procrun.Result`, `procrun.Func`, `procrun.Exec(timeout) Func`, `*procrun.Timeout`, `*procrun.NotFound`, `procrun.PyName(err error) string` (besluit 2, 3); `scriptcase.Case`, `scriptcase.Call`, `scriptcase.Load(t, path) []Case`, `scriptcase.Replay(t, calls []Call) procrun.Func` (de `dir` telt niet mee bij het vergelijken van een aanroep); in pytest `COMMANDS: dict[str, list[str]]` en de parametrisatie over `tests/pack/fixtures/scripts/*/cases.json`.

- [ ] **Step 1: Schrijf `TestExecReportsCodeTimeoutAndMissing`.** `Exec(1*time.Second)` op `sh -c 'echo out; echo err >&2; exit 3'` geeft `Result{"out\n","err\n",3}` en `err == nil`. Op `sleep 5` geeft het `*Timeout`. Op `no-such-binary-xyz` geeft het `*NotFound`. Met stdin `"abc"` geeft `cat` als uitvoer `"abc"`.
- [ ] **Step 2: Schrijf `TestPyNameTable`**, met één geval per rij van besluit 3.
- [ ] **Step 3: Draai `go test ./internal/procrun/`.** Verwacht: FAIL (pakket bestaat niet).
- [ ] **Step 4: Implementeer `procrun`.** Maak de procesgroep dood bij een timeout, zoals `internal/deadline` doet.
- [ ] **Step 5: Implementeer `scriptcase` en `_fakegh.py`.** Allebei melden ze een onverwachte aanroep met de verwachte en de echte argv.
- [ ] **Step 6: Draai de rooktest.** Met één geval `test_fake_gh_replays_in_order` in `tests/pack/fixtures/scripts/_self/cases.json`: `argv ["sh","-c","gh a; gh b"]` met twee calls, en een omgekeerde volgorde die faalt. Run: `go test -race ./internal/procrun/ ./internal/scriptcase/ && uv run pytest -q tests/pack/test_script_cases.py`. Verwacht: PASS.
- [ ] **Step 7: Commit** `test(scripts): nagespeelde gh-aanroepen als gedeelde fixture`.

### Taak 2: `lighthouse board-start`

**Files:**
- Create: `internal/boardstart/boardstart.go`, `internal/boardstart/fixtures_test.go`, `internal/cli/boardstart.go`
- Create: `tests/pack/fixtures/scripts/board-start/cases.json`
- Modify: `internal/cli/root.go:70-94` (AddCommand)
- Modify: `pack/core/skills/start-issue/{claude,agents}/SKILL.md:119`, `pack/core/skills/close-issue/{claude,agents}/SKILL.md:341`
- Modify: `tests/test_site_config.py:54-64,329-331` en de lezerskaart in `docs/site/data/config.json` (lezer wordt `internal/boardstart/boardstart.go`), `tests/pack/test_skills_issue_flow.py:229` (de `board_start.py`-tekst in step 3), `tests/pack/test_close_issue_skill.py:167`, en de losse vermeldingen van `board_start.py` in `pack/core/skills/start-issue/{claude,agents}/SKILL.md:267,274` en `close-issue/*/SKILL.md:350`
- Delete: `pack/core/scripts/board_start.py`, `tests/pack/test_board_start.py` (+ geïnstalleerde kopie via `init --upgrade`)

**Interfaces:**
- Consumes: `procrun.Func`, `procrun.PyName`, `scriptcase`
- Produces: `boardstart.Run(ctx context.Context, env Env, args []string, out io.Writer) int` met `type Env struct{ Run procrun.Func; Config func() (*config.Config, error) }`; CLI `lighthouse board-start <nr>` en `lighthouse board-start --reopen <nr>`.

- [ ] **Step 1: Zet elke test uit `test_board_start.py` om naar een geval in `cases.json`** (besluit 5), plus `test_graphql_reply_with_trailing_newline` (Review-focus 3). `COMMANDS["board-start"] = ["python3", str(INSTALL_ROOT / ".lighthouse/scripts/board_start.py")]` (`tests/pack/_testkit.py:576`; een relatief pad bestaat niet in de tmp-map van het geval).
- [ ] **Step 2: Draai `uv run pytest -q tests/pack/test_script_cases.py -k board-start`.** Verwacht: PASS tegen de Python. Faalt een geval, dan klopt de fixture niet, niet de Python.
- [ ] **Step 3: Schrijf `TestBoardStartCases`.** Die draait elk geval via `scriptcase.Replay` door `boardstart.Run`. Run: `go test ./internal/boardstart/`. Verwacht: FAIL.
- [ ] **Step 4: Implementeer `boardstart.Run` en de CLI** (besluit 4: rauwe argumenten). De query-tekst is byte voor byte `QUERY` uit `board_start.py:40-56`.
- [ ] **Step 5: Draai Go en pytest.** Zet `COMMANDS["board-start"]` op de binary. Run: `go test -race ./internal/boardstart/ ./internal/cli/ && uv run pytest -q tests/pack/test_script_cases.py -k board-start tests/test_site_config.py`. Verwacht: PASS.
- [ ] **Step 6: Zet de skills om naar `lighthouse board-start <nr>` / `lighthouse board-start --reopen <nr>`.** Haal daarna de Python weg, draai `init --upgrade`, en draai `uv run pytest -q tests/pack/test_skills_issue_flow.py tests/pack/test_close_issue_skill.py`. Verwacht: PASS.
- [ ] **Step 7: Commit** `feat(core): board-start in Go`.

### Taak 3: `lighthouse unblock-dependents` en de workflow

**Files:**
- Create: `internal/unblock/unblock.go`, `internal/unblock/fixtures_test.go`, `internal/cli/unblockdependents.go`
- Create: `tests/pack/fixtures/scripts/unblock-dependents/cases.json`
- Modify: `ci/literal-allowlist.txt`, `pack/core/workflows/unblock-dependents.yml` (kop `:10`, stappen `:51-55`, `:68-73`), `tests/pack/test_workflow_unblock.py:48-52`
- Delete: `pack/core/scripts/unblock_dependents.py`, `tests/pack/test_unblock_dependents.py`

**Interfaces:**
- Consumes: `procrun`, `scriptcase`
- Produces: `unblock.Run(ctx context.Context, gh procrun.Func, nr int, label string, dryRun bool) (code int, lines []string)`; CLI `lighthouse unblock-dependents --issue <nr> [--dry-run]`, die schrijft naar stdout en voegt toe aan `$GITHUB_STEP_SUMMARY`; configfout → `unblock-dependents: <bericht>` op stderr, exit 2.

- [ ] **Step 1: Zet elke test uit `test_unblock_dependents.py` om naar een geval,** plus `test_label_with_reserved_characters_is_fully_encoded` (Review-focus 1) en `test_step_summary_is_appended` (`env.GITHUB_STEP_SUMMARY` naar een bestand dat al een regel heeft; `expect.files`).
- [ ] **Step 2: Draai de gevallen tegen de Python.** Run: `uv run pytest -q tests/pack/test_script_cases.py -k unblock-dependents`. Verwacht: PASS.
- [ ] **Step 3: Schrijf `TestUnblockCases`.** Run: `go test ./internal/unblock/`. Verwacht: FAIL.
- [ ] **Step 4: Implementeer.** `RELEASE_LINE = "Released lane after this close:"` blijft letterlijk; `close-issue` leest hem. De labelcodering is `quote(…, safe='')`: alles behalve `A-Za-z0-9_.-~` wordt `%XX`, als `func quoteAll(s string) string`.
- [ ] **Step 5: Draai Go en pytest.** Zet `COMMANDS` op de binary. Run: `go test -race ./internal/unblock/ ./internal/cli/ && uv run pytest -q tests/pack/test_script_cases.py -k unblock-dependents`. Verwacht: PASS.
- [ ] **Step 6: Workflow volgens besluit 9.** `test_calls_the_installed_script` wordt `test_calls_the_binary`: `lighthouse unblock-dependents --issue "$ISSUE"` staat erin, `uv` en `python` niet. Voeg `test_get_lighthouse_maps_runner_and_refuses_unknown` toe (Review-focus 4). Run: `uv run pytest -q tests/pack/test_workflow_unblock.py`. Verwacht: PASS. Draai `actionlint` op de geïnstalleerde `.github/workflows/lighthouse-unblock-dependents.yml` als het op het PATH staat.
- [ ] **Step 7: Haal de Python weg, draai `init --upgrade`, en commit** `feat(core): unblock-dependents in Go, workflow zonder uv`.

### Taak 4: `lighthouse post-artefact`; `artefacts.py` en `protocol.py` weg

**Files:**
- Create: `internal/protocol/artefact.go`, `internal/protocol/artefact_test.go`, `tests/pack/fixtures/artefacts/cases.json`
- Create: `internal/postartefact/postartefact.go`, `internal/postartefact/fixtures_test.go`, `internal/cli/postartefact.go`, `tests/pack/fixtures/scripts/post-artefact/cases.json`
- Modify: `pack/core/skills/write-handoff/SKILL.md:40,73,272,301`, `tests/pack/python/test_prcov_issues.py:19,378-405`, `tests/pack/test_skills_issue_flow.py:5,446-470,499,526`, `ci/literal-allowlist.txt:5`, `internal/protocol/header_test.go:19` en `internal/protocol/question_test.go:20` (noemen `tests/pack/test_protocol.py`)
- Delete: `pack/core/scripts/post_artefact.py`, `pack/core/lib/artefacts.py`, `pack/core/lib/protocol.py`, `tests/pack/test_post_artefact.py`, `tests/pack/test_artefacts.py`, `tests/pack/test_protocol.py`, `tests/pack/test_artefact_cases.py` (de Python-runner uit stap 1; daarna dragen de Go-tests de fixtures)

**Interfaces:**
- Consumes: `procrun`, `scriptcase`, `fixturesetup`, `protocol.Comment`, `config.Heading`
- Produces: de functies van besluit 8; `postartefact.Run(ctx context.Context, env Env, nr int, path string) (line string, err error)` met `type Env struct{ Run procrun.Func; Config func() (*config.Config, error) }`; een `*postartefact.Error` waarvan `Error()` de volledige `artefact:`-regel is; CLI `lighthouse post-artefact <nr> <file>`: exit 0 met de regel op stdout, 1 met de regel op stderr, 2 bij gebruik.

- [ ] **Step 1: Artefact-fixtures.** Zet elke test uit `test_artefacts.py` om naar `artefacts/cases.json` (`{"name","fn","input","expect"}`, met `fn` in `parse_meta|render|hash_of|split|current|next_version`), plus `test_split_counts_code_points_at_the_fence` (Review-focus 2). Draai ze tegen `artefacts.py` met een kleine pytest-runner `tests/pack/test_artefact_cases.py`. Run: `uv run pytest -q tests/pack/test_artefact_cases.py`. Verwacht: PASS.
- [ ] **Step 2: Schrijf `TestArtefactCases`** in `internal/protocol`. Run: `go test ./internal/protocol/`. Verwacht: FAIL. Implementeer `artefact.go`; `split` telt in runes. Run opnieuw. Verwacht: PASS.
- [ ] **Step 3: Script-fixtures.** Zet elke test uit `test_post_artefact.py` om naar een geval, met een `setup` die een repo, een gecommit planbestand en een kale `origin` bouwt, plus `test_open_pr_count_with_trailing_newline` (Review-focus 3). Draai tegen de Python. Run: `uv run pytest -q tests/pack/test_script_cases.py -k post-artefact`. Verwacht: PASS.
- [ ] **Step 4: Schrijf `TestPostArtefactCases`, implementeer, en zet `COMMANDS` op de binary.** Run: `go test -race ./internal/postartefact/ ./internal/protocol/ ./internal/cli/ && uv run pytest -q tests/pack/test_script_cases.py -k post-artefact`. Verwacht: PASS.
- [ ] **Step 5: Dekking van `test_protocol.py` (besluit 7).** Elke test die nog geen Go-tegenhanger heeft, gaat naar `tests/pack/fixtures/protocol/` of krijgt een `// twin of <naam>`-test in `internal/protocol`. Run: `go test ./internal/protocol/`. Verwacht: PASS.
- [ ] **Step 6: Zet de afnemers om.** De skill gebruikt `lighthouse post-artefact <nr> <absolute path>`, `test_prcov_issues.py` gaat naar `lighthouse protocol robot`, en de allowlistregel gaat weg. Haal de zeven bestanden weg en draai `init --upgrade`. Run: `uv run pytest -q tests/pack tests/pack/python`. Verwacht: PASS. Daarna geeft `grep -rn "lighthouse_lib.protocol\|lighthouse_lib.artefacts\|lighthouse_lib import protocol" pack tests tools ci` niets.
- [ ] **Step 7: Commit** `feat(core): post-artefact in Go; protocol alleen nog in internal/protocol`.

### Taak 5: `lighthouse worktree-audit`; `pr_body_gate.py`, `git_invocations.py` en `repo_identity.py` weg

**Files:**
- Create: `internal/worktreeaudit/{rows.go,judge.go,collect.go,render.go,pid_unix.go,pid_windows.go}`, plus de tests; `internal/cli/worktreeaudit.go`
- Create: `tests/pack/fixtures/worktree_audit/{judge,rows,parse,render}.json`, `tests/pack/fixtures/scripts/worktree-audit/cases.json` (`--issue`: de GraphQL-call en de PR-lookup)
- Modify: `pack/core/skills/audit-worktrees/SKILL.md:12-17,159`, `pack/core/skills/close-issue/{claude,agents}/SKILL.md:502,518`, `docs/host-assumptions.md:28`, de doc-comments in `internal/prbody/form.go:2`, `internal/repoidentity/repoidentity.go:4` en `internal/gitinvocations` (geen "frozen"-zin meer), `tests/pack/test_pr_create_gate.py:26`, `tests/pack/test_write_pr_skill.py:128,178`, `tests/pack/test_close_issue_skill.py:254,349`, `tests/pack/test_audit_worktrees_skill.py:20,34-36` (laadt `worktree_audit.py` op moduleniveau: laat de skilltekst controleren zonder de module), `tests/pack/test_skill_contracts.py:337,370-374` (`test_tool_patterns_see_python_and_tomllib`: `close-issue` heeft na deze taak geen `python3`/`tomllib` meer en geen kernskill noemt `tomllib` dan nog. De `python3`-positieve check gaat naar `place-instruction` (`SKILL.md:247`), de `tomllib`-positieve check draait op een synthetische tekst `python3 .lighthouse/lib/lighthouse_lib/config.py` tegen `_TOOL_USES["tomllib"]`, en de negatieve check op `route-issue` blijft. `pack/core/requires.toml` verliest `close-issue` onder `python3` en `tomllib` in deze taak), `tests/pack/_testkit.py`
- Delete: `pack/core/scripts/worktree_audit.py`, `pack/core/lib/{pr_body_gate,git_invocations,repo_identity}.py`, `tests/pack/test_worktree_audit.py`, `tests/pack/test_worktree_audit_cases.py` (de Python-runner uit stap 1; daarna dragen de Go-tests de fixtures), `tests/pack/test_pr_body_gate.py`, `tests/pack/test_pr_body_gate_fixtures.py`, `tests/pack/test_repo_identity.py`, `tests/pack/test_repo_identity_fixtures.py`, `tests/pack/test_git_invocations.py`, `tests/pack/test_git_invocations_fixtures.py`

**Interfaces:**
- Consumes: `procrun`, `scriptcase`, `fixturesetup`, `prbody.RefsTargets(body string) []int`, `config`
- Produces: `worktreeaudit.Row`, `worktreeaudit.Verdict`, `func Judge(r Row, alive func(pid int) *bool) Verdict`, `func BuildRows(...) []Row` (de argumenten van `build_rows`, `worktree_audit.py:690`; git loopt via `procrun.Func` met `dir` = de repo, dus `_assert_readonly` ziet de argv zonder `-C`), `func Collect(ctx context.Context, env Env, repoRoot string) ([]Row, error)`, `func PidAlive(pid int) *bool` (nil = niet meetbaar; op Windows altijd nil); CLI `lighthouse worktree-audit [--cleanup|--sessions|--origin|--kpi|--only <branch>|--issue <nr>]` met de exitcodes uit de docstring (`--only`: 0 = opruimbaar; `--issue`: 2 = PR-lookup mislukt).

- [ ] **Step 1: Data-fixtures.** Zet de tests over `judge`, `build_rows`, `parse_*`, `render_*`, `issue_branches` en `_assert_readonly` uit `test_worktree_audit.py` om naar de vier JSON-bestanden (besluit 6). Rijen staan als JSON-objecten met de veldnamen van `Row`. `render`-gevallen draaien met `TZ=UTC`. Draai ze tegen de Python met `tests/pack/test_worktree_audit_cases.py`. Run: `uv run pytest -q tests/pack/test_worktree_audit_cases.py`. Verwacht: PASS.
- [ ] **Step 1b: Dekking van de inline-tests (besluit 7).** Elke `test_*` uit `test_pr_body_gate.py`, `test_git_invocations.py` en `test_repo_identity.py` die niet al als `name` in `tests/pack/fixtures/{pr_body_gate,git_invocations,repo_identity}` staat, wordt een fixturegeval of een `// twin of <naam>`-test in `internal/prbody`, `internal/gitinvocations` of `internal/repoidentity`. Meet het met het commando van taak 6 stap 2, op `origin/main`. Run: `go test ./internal/prbody/ ./internal/gitinvocations/ ./internal/repoidentity/`. Verwacht: PASS.
- [ ] **Step 2: Schrijf de Go-tests die de fixtures lezen, plus de tweelingen** voor de IO-tests en de drie van Review-focus 5. Run: `go test ./internal/worktreeaudit/`. Verwacht: FAIL.
- [ ] **Step 3: Implementeer.** `run_git` houdt zijn allowlist van alleen-lezende verbs (`_assert_readonly`, `:439`). Een git-verb buiten die lijst is een panic in de test en een fout in productie, nooit een uitvoering. `--issue` gebruikt `prbody.RefsTargets` in plaats van `refs_targets`.
- [ ] **Step 4: Draai Go en de script-fixtures.** Zet `COMMANDS["worktree-audit"]` op de binary. Run: `go test -race ./internal/worktreeaudit/ ./internal/cli/ && uv run pytest -q tests/pack/test_script_cases.py -k worktree-audit`. Verwacht: PASS.
- [ ] **Step 5: Zet de afnemers om** (besluit 10): de skills naar `lighthouse worktree-audit …` en de pytest-imports naar `_testkit` of de binary. Werk de rij in `host-assumptions.md` bij naar `internal/worktreeaudit/pid_unix.go` (met regelnummer). Haal de twaalf bestanden weg en draai `init --upgrade`. Run: `uv run pytest -q tests/pack`. Verwacht: PASS. Daarna geeft `grep -rn "pr_body_gate\|repo_identity\|git_invocations" pack tests --include=*.py` alleen `tests/pack/fixtures/code_language/bash_writes_nl.py`.
- [ ] **Step 6: Commit** `feat(core): worktree-audit in Go; pr_body_gate, repo_identity en git_invocations weg`.

### Taak 6: Opruimen, changelog en eindcontrole

**Files:**
- Modify: `pack/core/requires.toml:4,5,7,9` (de vier scripts en `lighthouse_lib.{protocol,artefacts,pr_body_gate}` eruit; `python3` en `tomllib` blijven voor `config`, `headings`, `datadir`, `check-instructions` en de skills die nog `python3` noemen), `pack/SOURCES.toml` (de items van de verwijderde bestanden), `internal/packs/core_test.go:78-103`, `docs/site/data/{loop,packs,config}.json`, `docs/site/reference.html`, `internal/initcmd/places_test.go`
- Create: `changelog.d/issue-281-skill-scripts-go.md` met `type: changed` (types: `ci/changelog.py:30`; formaat: `docs/releasing.md:57-77`)

- [ ] **Step 1: Werk de lijsten bij.** Run: `go test ./internal/packs/ ./internal/initcmd/ && uv run pytest -q tests/test_site_config.py`. Verwacht: PASS.
- [ ] **Step 2: Dekkingsregel (besluit 7).** Voor elk weggehaald pytest-bestand `F`, behalve de `test_*_fixtures.py`-runners en de Python-runners van stap 1 (`test_artefact_cases.py`, `test_worktree_audit_cases.py`):
  ```bash
  git show origin/main:"$F" | grep -oP '^\s*def \Ktest_\w+' | sort -u | while read -r t; do grep -rqwF "$t" tests/pack/fixtures internal || echo "MISSING $F $t"; done
  ```
  Verwacht: geen `MISSING`-regel.
- [ ] **Step 3: Weg is weg, bevroren is gelijk.** `git diff --stat origin/main -- pack/core/lib/config.py pack/core/lib/headings.py pack/core/lib/datadir.py` geeft niets. `ls pack/core/scripts pack/core/lib` noemt geen van de negen verwijderde bestanden. `grep -rn "python3" .claude/skills .agents/skills .lighthouse/scripts | grep -E "board_start|post_artefact|unblock_dependents|worktree_audit"` geeft niets, en `grep -rnE "board_start|post_artefact|unblock_dependents|worktree_audit|lighthouse_lib\.(protocol|artefacts)" pack internal .claude/skills .agents/skills` noemt geen verwijderd script of module meer (alleen `bash_writes_nl.py` mag blijven).
- [ ] **Step 4: Dogfood.** Run: `go run ./cmd/lighthouse init --upgrade`. Verwacht: `No changes.`
- [ ] **Step 5: Volledige gate.** Run het `[test_gate] command` uit `lighthouse.toml` en `GOOS=windows go vet ./... && GOOS=darwin go vet ./...`. Verwacht: groen.
- [ ] **Step 6: Commit** `docs(core): changelog en opruiming van skill-scripts deel 1`.
