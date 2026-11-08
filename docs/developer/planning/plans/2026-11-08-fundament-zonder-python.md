# Fundament zonder Python: implementatieplan

> **Voor agents:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development
> (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen
> gebruiken checkboxes (`- [ ]`).

**Doel:** stap 1 van de milestone *Lighthouse zonder Python* (#274). De binary leest
`lighthouse.toml`, `host.toml` en `headings.toml` zelf. De hook-runtime en `run.sh` staan klaar
voor stap 2. Skills roepen `lighthouse config` en `lighthouse headings` aan in plaats van
`config.py` en `headings.py`.

**Architectuur:** `internal/config` wordt een getrouwe Go-port van `pack/core/lib/config.py`. De
gelijkheid bewijzen we met gedeelde fixtures, die zowel pytest (tegen `config.py`) als `go test`
draaien. Cobra-subcommando's `config`, `headings`, `protocol` en `hook` komen in `internal/cli`.
`run.sh` is POSIX `sh`: hij zoekt de binary en bouwt hem bij `version = "dev"` op de achtergrond
(spec §10). In deze stap registreert nog geen enkele hook zich op `run.sh`. Dat gebeurt vanaf
stap 2.

**Tech stack:** Go (cobra, `github.com/BurntSushi/toml`), POSIX `sh`, pytest voor fixtures en
black-box-tests.

**Spec:** [`2026-11-07-zonder-python-design.md`](../specs/2026-11-07-zonder-python-design.md):
§1–§4, §7, §9, §10 en *Opdeling* stap 1.

## Globale eisen

- Exitcodes van `lighthouse config`: 0 met waarde, 1 met `config: <key> is not set` op stderr,
  2 met `config: <fout>` op stderr. `host-check`: 0, 1 of 2. `lighthouse headings`: 0, of 2 met
  `headings: unknown key <key>` of `headings: <fout>`.
- Uitvoer zoals in `config.py`: een lijst geeft één element per regel, een bool `true` of
  `false`, een table geeft rc 2 met `<key> is a table, not a value`.
- Omgevingsvariabelen zoals in Python: `LIGHTHOUSE_REPO_ROOT`, `LIGHTHOUSE_TOML`,
  `LIGHTHOUSE_HOST_TOML`, `LIGHTHOUSE_HEADINGS`, `LIGHTHOUSE_DATA`, `XDG_DATA_HOME`.
- Bevroren, geen byte anders: `pack/core/lib/{config,headings,datadir,protocol}.py` en hun kopie
  onder `.lighthouse/lib/lighthouse_lib/`.
- Weg in deze stap: niets. Alle bash-hooks blijven geregistreerd zoals ze zijn.
- Go volgt `/lighthouse-go-standard`, tests volgen `/lighthouse-test-standard`. Code, comments en
  meldingen zijn Engels. Commits, PR en issue-comments zijn Nederlands.
- `run.sh`: POSIX `sh`, schoon voor `shellcheck`. Draait op Linux en macOS (geen `setsid` op
  macOS).
- Elke wijziging onder `pack/` draait `go run ./cmd/lighthouse init --upgrade` en commit de
  geïnstalleerde kopie. CI eist `No changes.`

## Review-focus

Vijf invoerklassen die de spec impliceert maar die geen gewone test raakt. Elke regel heeft een
test in de taak die de code bezit.

1. **Floats in een vrije table** (`[x] f = 123456789.0`, `1e16`, `1e-5`). Python print
   `123456789.0`, `1e+16` en `1e-05`, en Go's `'g'`-formaat wijkt daarvan af. → fixture
   `floats` (taak 1).
2. **Optienamen met spaties en punten** (`forge.board.status_options.In progress`, een optie
   `"a.b"`). `_walk` laat een exacte sleutel winnen. → fixture `options` (taak 1).
3. **Een TOML-datetime of een table als waarde** geeft rc 2 met `is a table, not a value`. →
   fixture `scalars` (taak 1).
4. **`run.sh` aangeroepen vanuit een submap of via een pad met spaties.** De root komt uit de
   plek van het script, niet uit de cwd. → `test_run_sh_from_subdir_with_space` (taak 7).
5. **`run.sh` op een host zonder `setsid` (macOS).** De build moet daar ook loskomen. →
   `test_build_detaches_without_setsid` (taak 7).

---

### Taak 1: `internal/config`, een getrouwe port van `config.py`

**Bestanden:**
- Nieuw: `internal/config/config.go` (schema, defaults, `Load`, `Parse`, `Lookup`, `Lines`)
- Nieuw: `internal/config/host.go` (`LoadHost`, `HostValue`, `HostCheck`, `isLocal`)
- Nieuw: `internal/config/pyfloat.go` (`formatFloat`, Python-`str()` voor float64)
- Nieuw: `internal/config/parity_test.go`, `internal/config/config_test.go`
- Nieuw: `tests/pack/fixtures/config/<geval>/{lighthouse.toml,host.toml?,expect.json}`
- Nieuw: `tests/pack/test_config_fixtures.py`

**Interfaces:**
- Produceert:
  ```go
  type Error struct{ Msg string }            // ConfigError; rc 2; Error() = Msg
  var ErrNotSet = errors.New("not set")      // rc 1
  type Board struct { ProjectNumber int; ProjectID, StatusFieldID, PriorityFieldID string
      StatusOptions, PriorityOptions map[string]string }
  type Config struct { Kind, Repo, Language string; Board *Board; Raw map[string]any; Path string }
  func RepoRoot() (string, error)
  func Path() (string, error)                 // LIGHTHOUSE_TOML, else RepoRoot()/lighthouse.toml
  func Load(path string) (*Config, error)
  func Parse(data []byte, path string) (*Config, error)
  func Lookup(c *Config, key string) (any, error)   // wraps ErrNotSet
  func Lines(key string, v any) ([]string, error)  // one line per list element
  func HostValue(key string) (string, error)
  func HostCheck(out, errOut io.Writer) int
  func Run(args []string, out, errOut io.Writer) int // port of main(): rc 0/1/2, shared by CLI and parity test
  func IsKnownKey(key string) bool
  const ControllerStatusOption = "Controller"
  ```
- Hostpad: `hostconfig.Path()` (bestaat al, dezelfde regel als `host_path()`).

**Fixtureformaat.** Elk geval is een map. `expect.json` is een lijst van
`{"args": [...], "rc": n, "stdout": "...", "stderr": "..."}`. Een geval met
`"stderr_prefix"` in plaats van `"stderr"` vergelijkt alleen het begin. Dat is nodig bij een
TOML-parsefout, omdat tomllib en BurntSushi die anders verwoorden. Ontbreekt `host.toml`, dan
wijst `LIGHTHOUSE_HOST_TOML` naar een pad dat niet bestaat. Foutmeldingen bevatten het pad van
het bestand. Daarom draaien beide kanten (pytest en `go test`) met cwd = de fixturemap en
relatieve `LIGHTHOUSE_TOML`, `LIGHTHOUSE_HOST_TOML` en `LIGHTHOUSE_HEADINGS` (Go: `t.Chdir`).
Zo staan in `expect.json` alleen relatieve paden.

- [ ] **Stap 1: zet de fixtures neer.** Minimaal deze gevallen:
  - `minimal`: geen `[forge]`.
  - `full`: elke sleutel uit `SCHEMA`, `_BASE_KEYS` en `HOST_KEYS`.
  - `defaults`: elke sleutel uit `DEFAULTS`, afwezig in de toml.
  - `options`: spaties, punten, een exacte sleutel die wint.
  - `floats`: Review-focus 1, plus host-floats `2.0` → `2` en `1.5`.
  - `scalars`: datetime, table, bool, lege `test_gate.command`, `inf`, `-inf`, `nan` en een
    array of tables (`[[x]]`).
  - Één geval per `ConfigError`-melding in `load` en `load_host`: forge.kind, repo-regex,
    language, code_language, roborev, ontbrekende statusoptie, Controller met en zonder
    `[controller]`, verkeerd type, lege string, priority_options geen table, de host-validaties.
  - `bad-toml`, `missing-file`.
  - `host-check` alleen met rc 2 (het rc 0/1-pad hangt van de machine af).
- [ ] **Stap 2: schrijf `tests/pack/test_config_fixtures.py`.**
  - `test_python_matches_fixtures[<geval>]` draait `python3 -m lighthouse_lib.config <args>`
    met `PYTHONPATH=str(IMPORT_ROOT)` (absoluut pad, `IMPORT_ROOT` uit `tests/pack/_testkit.py`: een
    tempdir met de symlink `lighthouse_lib -> pack/core/lib`, zoals `tests/pack/test_config.py:519`;
    `config.py` direct aanroepen faalt met `ModuleNotFoundError: lighthouse_lib`) per verwachting en assert rc, stdout en stderr (of `stderr_prefix`).
  - `test_fixtures_cover_every_key` assert dat de vereniging van de `args` uit alle
    `expect.json` een superset is van `SCHEMA | DEFAULTS | _BASE_KEYS | HOST_KEYS`,
    geïmporteerd uit `config.py`, plus minstens één sleutel per `_OPTION_PREFIXES`.
- [ ] **Stap 3:** `uv run pytest -q tests/pack/test_config_fixtures.py`. Verwacht: PASS. De
  fixtures beschrijven dan aantoonbaar `config.py`. Klopt een verwachting niet, corrigeer de
  fixture, nooit `config.py`.
- [ ] **Stap 4: schrijf `internal/config/parity_test.go`.**
  `TestParityWithConfigPy` loopt over dezelfde mappen (pad `../../tests/pack/fixtures/config`),
  zet de env met `t.Setenv`, roept `config.Run(args, out, errOut) int` aan (geëxporteerd, de port van `main()` in Python:
  `Lookup`, `Lines`, `HostCheck`, ook de omleiding van `host.*`-sleutels zonder `lighthouse.toml`
  te laden) en assert hetzelfde. `lighthouse config` in taak 2 is een dunne schil om dezelfde
  `config.Run`, zodat de parity-test de CLI-logica echt dekt. Ook `TestFormatFloat` met de tabel `123456789.0→"123456789.0"`, `1e16→"1e+16"`,
  `1e-5→"1e-05"`, `+Inf→"inf"`, `-Inf→"-inf"`, `NaN→"nan"`, `0.0001→"0.0001"`, `1.5→"1.5"`, `-0.0→"-0.0"`.
- [ ] **Stap 5:** `go test ./internal/config/`. Verwacht: FAIL, het pakket bestaat nog niet.
- [ ] **Stap 6: implementeer het pakket.** Schema en defaults als Go-maps, letterlijk overgenomen
  uit `config.py:66-161`. TOML decoderen in `map[string]any`. `_walk` bewaart de volgorde niet,
  maar de semantiek "exacte sleutel wint, anders het prefix" wel. Bij meer dan één passend
  prefix: kies dezelfde als Python, dus de eerste in documentvolgorde (`toml.MetaData.Keys()`).
  `formatFloat` volgt Python `repr`: exponent als de exponent < -4 of ≥ 16, anders decimaal met
  minstens één cijfer achter de punt. `isLocal` doet een UDP-`bind` en geeft
  `EADDRNOTAVAIL` → false.
- [ ] **Stap 7:** `go test -race ./internal/config/` en
  `uv run pytest -q tests/pack/test_config_fixtures.py`. Verwacht: PASS.
- [ ] **Stap 8: commit** `feat(config): Go-lezer voor lighthouse.toml en host.toml (#274)`.

### Taak 2: `lighthouse config`

**Bestanden:**
- Nieuw: `internal/cli/config.go`, `internal/cli/config_test.go`
- Wijzig: `internal/cli/root.go` (`root.AddCommand(newConfigCommand())`)

**Interfaces:**
- Gebruikt: alles uit taak 1.
- Produceert: `lighthouse config <key>`, `lighthouse config host-check`,
  `lighthouse config --json <key>`.

- [ ] **Stap 1: tests.**
  - `TestConfigCommandExitCodes`: rc 0, 1 en 2 via `execute(NewRoot(...))` op een tijdelijke
    `lighthouse.toml`, met de stderr-teksten uit Globale eisen.
  - `TestConfigJSON`: `--json forge.board` geeft een object met `project_number` (getal),
    `project_id`, `status_field_id` en `status_options` (object). `--json paths.search_dirs` op
    zijn default geeft `["."]`. Een niet-gezette sleutel met `--json` geeft rc 1, zonder stdout.
- [ ] **Stap 2:** `go test ./internal/cli/ -run Config`. Verwacht: FAIL.
- [ ] **Stap 3: implementeer.** `lighthouse config` roept `config.Run` aan; alleen `--json` is
  nieuw in de CLI-laag. `--json` schrijft de waarde van `Lookup` als JSON met
  `encoding/json`. Een table mag hier wel (in tegenstelling tot zonder `--json`). Datetime wordt
  een RFC 3339-string. Fouten gaan via `exitError{code, err}`, met de tekst letterlijk op stderr.
  Zorg dat `execute` er geen `lighthouse: ` voor zet; gebruik daarvoor `errSilent` plus een eigen
  `Fprintln`.
- [ ] **Stap 4:** `go test -race ./internal/cli/`. Verwacht: PASS.
- [ ] **Stap 5: commit** `feat(cli): lighthouse config met --json en host-check (#274)`.

### Taak 3: `lighthouse headings` en `internal/datadir`

**Bestanden:**
- Nieuw: `internal/config/headings.go` (`HeadingsPath`, `Heading`, `Variants`)
- Nieuw: `internal/cli/headings.go`
- Nieuw: `internal/datadir/datadir.go`, `internal/datadir/datadir_test.go`
- Nieuw: `tests/pack/fixtures/headings/<geval>/{headings.toml,lighthouse.toml,expect.json}`
- Wijzig: `tests/pack/test_config_fixtures.py` (ook headings-fixtures tegen `headings.py`)

**Interfaces:**
- Produceert:
  ```go
  func HeadingsPath() (string, error)              // LIGHTHOUSE_HEADINGS, else <root>/.lighthouse/headings.toml
  func Heading(key, language string) (string, error) // language "" = [project] language
  func Variants(key string) ([]string, error)      // nl first, no duplicates
  package datadir
  var ErrInRepo = errors.New("data directory is inside a git work tree")
  func Dir(getenv func(string) string) (string, error) // creates it
  ```
- `internal/protocol` houdt zijn eigen ingebedde tabel. Dit leest het bestand van de repo,
  zoals `headings.py`.

- [ ] **Stap 1: fixtures en tests.**
  - Fixtures: bekende sleutel in `nl` en `en`, onbekende sleutel (rc 2), ontbrekende
    `headings.toml`, een variant die geen string is, ontbrekende `lighthouse.toml` (rc 2).
  - `test_python_matches_headings_fixtures` draait `headings.py`.
  - `TestHeadingsParity` (Go) draait dezelfde fixtures.
  - `TestDataDir` port de 6 gevallen uit `tests/pack/test_datadir.py` als tabel: env wint,
    relatieve `XDG_DATA_HOME` genegeerd, binnen een git-tree → `ErrInRepo`, map wordt gemaakt.
- [ ] **Stap 2:** run beide. Verwacht: Python PASS, Go FAIL.
- [ ] **Stap 3: implementeer.** Daarna PASS in `go test -race ./internal/config/
  ./internal/datadir/ ./internal/cli/` en in pytest.
- [ ] **Stap 4: commit** `feat(cli): lighthouse headings en datadir in Go (#274)`.

### Taak 4: `lighthouse protocol parse-header|robot`

**Bestanden:**
- Nieuw: `internal/cli/protocol.go`, `internal/cli/protocol_test.go`

**Interfaces:**
- Gebruikt: `protocol.ParseHeader(body string) (Header, bool)`, `protocol.Robot`.
- Produceert: `lighthouse protocol robot` (print `🤖`, rc 0) en
  `lighthouse protocol parse-header` (body op stdin, op stdout
  `{"kind": "...", "fields": {...}, "session": "..." | null}` of `null`, rc 0).

- [ ] **Stap 1: test** `TestProtocolParseHeaderFixtures` loopt over
  `tests/pack/fixtures/protocol/headers.json` en assert dat `kind`, `fields` (ook met legacy-sleutels) en `session` (ook met sessie-footer) gelijk zijn aan de fixture, die uit `protocol.py` komt. Een
  body zonder header geeft `null`. `session` is `null` als er geen footer is.
- [ ] **Stap 2:** FAIL, dan implementeren, dan `go test -race ./internal/cli/`. PASS.
- [ ] **Stap 3: commit** `feat(cli): lighthouse protocol parse-header en robot (#274)`.

### Taak 5: `repoconfig` en `workflows.go` op de Go-lezer, de toestemmingsregel

**Bestanden:**
- Wijzig: `internal/repoconfig/repoconfig.go:116-162`
- Wijzig: `internal/initcmd/workflows.go` (`errNoPython`, `pythonLookup` gaan weg) en de
  aanroep in `internal/initcmd/initcmd.go:155`
- Wijzig: `internal/packs/packs.go:144-149` (`IsLighthouseRule`) en de melding op `:511`
- Test: `internal/repoconfig/repoconfig_test.go`, `internal/initcmd/initcmd_test.go`,
  `internal/packs/packs_test.go:384`

**Interfaces:**
- Gebruikt: `config.Default` (voor `repoconfig`); `config.Parse`, `config.Lookup` (alleen voor
  de init-lookup).
- `repoconfig.Repo` en `repoconfig.Load(dir)` houden hun signatuur. De dispatch- en
  triage-afnemers veranderen niet. `Load` blijft falen op een ontbrekende `[forge] repo`.
- `Load` houdt zijn eigen soepele decodering en zijn eigen meldingen (`config.Parse` valideert
  streng en wijst repo's af die `Load` nu laadt; tests als `triage_test.go`, `tick_test.go` en
  `serve_test.go` hangen daaraan). `Load` haalt alleen de **defaults** uit `internal/config`
  (een geëxporteerde `config.Default(key string) (any, bool)`, gevuld uit dezelfde tabel als
  `Lookup`). De foutmeldingen van `Load` veranderen niet.

- [ ] **Stap 1: tests.**
  - `TestLoadDefaultsFromConfig`: zonder `[labels]` en `default_branch` geeft `Load` dezelfde
    waarden als `config.Default` (één bron). De bestaande tests van `Load`, tick, triage en serve
    blijven ongewijzigd groen.
  - `TestFillWorkflowsWithoutPython`: `PATH` zonder `python3` (`t.Setenv("PATH", tmp)`), een
    `lighthouse.toml` zonder `[ci]`. De workflow krijgt `ubuntu-latest` voor `@@ci.runs_on@@`.
    Dit is AC 5.
  - `TestIsLighthouseRule` krijgt rijen `Bash(lighthouse config:*)` → true,
    `Bash(lighthouse:*)` → false en `Bash(lighthousex foo:*)` → false.
- [ ] **Stap 2:** `go test ./internal/repoconfig/ ./internal/initcmd/ ./internal/packs/`.
  Verwacht: FAIL op de nieuwe tests.
- [ ] **Stap 3: implementeer.** Labels, `default_branch` en `language` in `repoconfig` krijgen hun
  default uit `config.Default`, en de letterlijke `cmp.Or`-defaults gaan weg. Geen validatie erbij. `forge.kind` houdt `"github"` als default,
  want `config.py` heeft er geen. De nieuwe lookup voor init is
  `func goLookup(toml []byte) packs.Lookup`, gebouwd op `config.Parse` van de voorgestelde config
  (`proposeConfig`). Bij een `config.Error` wordt de workflow overgeslagen met de melding, zoals
  nu bij `errNoPython`. `IsLighthouseRule` accepteert ook `^Bash\(lighthouse [a-z-]+(:\*)?\)$`.
- [ ] **Stap 4:** `go test -race ./...`. PASS. Is `testdata/places.golden` veranderd, draai
  dan `go test ./internal/initcmd/ -run TestPlacesGolden -update` en houd
  `tests/test_site_repo.py` gelijk.
- [ ] **Stap 5: commit** `refactor(config): repoconfig en init lezen config in Go (#274)`.

### Taak 6: hook-runtime `lighthouse hook <naam> [stand]`

**Bestanden:**
- Nieuw: `internal/hook/hook.go`, `internal/hook/hook_test.go`
- Nieuw: `internal/cli/hook.go`

**Interfaces:**
- Produceert:
  ```go
  type Payload struct{ SessionID string `json:"session_id"`; Raw []byte }
  type Result struct { Code int; Output map[string]any; Stderr string } // Output → JSON on stdout
  type Func func(ctx context.Context, root string, stand string, p Payload) Result
  func Register(name string, f Func)          // stap 2 en verder; panic bij dubbel
  func Run(ctx context.Context, name, stand string, in io.Reader, out, errOut io.Writer, env Env) int
  type Env struct { Root, ManifestVersion, BinaryVersion, StateDir string }
  const ExitUnknown = 3
  ```
- `Env` vult de CLI: `Root` uit `config.RepoRoot()`, `ManifestVersion` uit `manifest.Read`,
  `BinaryVersion` uit `version.String()`, `StateDir` =
  `${XDG_STATE_HOME:-$HOME/.local/state}/lighthouse/hook-notices`.

- [ ] **Stap 1: tests** (met een test-hook die `Register` in de test zet):
  - `TestUnknownHookExits3`: rc 3. Stderr is
    `COULD NOT CHECK: unknown hook <naam> — update lighthouse to v<manifest>`. Bij manifest
    `dev`: `… — go build -o .lighthouse/bin/lighthouse ./cmd/lighthouse`.
  - `TestVersionNoticeOncePerSession`: binary `v0.13.0`, manifest `v0.12.1`. De eerste call
    met `session_id` `s1` krijgt `systemMessage`
    `lighthouse v0.13.0 ≠ v0.12.1 in this repo — run lighthouse init --upgrade`, samengevoegd
    met de `Output` van de hook. De tweede call met `s1` krijgt hem niet, een call met `s2` wel.
  - `TestVersionNoticeNotOnBlock`: een hook met `Code 2` zet geen marker. De volgende call
    met code 0 in dezelfde sessie krijgt de melding alsnog.
  - `TestNoVersionNoticeForDev`: manifest `dev` geeft nooit een versiemelding.
  - `TestBuildFailedNotice`: `<root>/.lighthouse/bin/build.failed` bestaat bij manifest `dev`.
    Eén keer per sessie komt er een `systemMessage` met het pad van
    `.lighthouse/bin/build.log` en `go build -o .lighthouse/bin/lighthouse ./cmd/lighthouse`.
  - `TestTwoNoticesJoin`: versie- en buildmelding samen met een `systemMessage` van de hook
    worden één string, gescheiden door `\n`.
- [ ] **Stap 2:** FAIL, implementeer, `go test -race ./internal/hook/ ./internal/cli/`. PASS.
- [ ] **Stap 3: commit** `feat(hook): runtime voor lighthouse hook (#274)`.

### Taak 7: `run.sh`, de lijst van blokkerende hooks, `.gitignore`

**Bestanden:**
- Nieuw: `pack/core/hooks/run.sh`, `pack/core/hooks/blocking` (één naam per regel:
  `main-commit-gate`, `commit-test-gate`, `pr-create-gate`, `bash-write-check`,
  `md-link-check`, `code-language-check`)
- Wijzig: `.gitignore` (`.lighthouse/bin/`), `pack/core/requires.toml` als een unit `run.sh`
  daar een regel nodig heeft (`sh`, `git`)
- Nieuw: `tests/pack/test_run_sh.py`
- Wijzig: `pack/SOURCES.toml` (entries `new = true` voor `run.sh` en `blocking`:
  `tests/test_sources.py` eist er een per bestand onder `pack/`). `run.sh` is geen geregistreerde
  hook en `tests/test_site_packs.py` eist precies 9 core-hooks: sluit `run.sh` en `blocking` uit
  van de hooklijst van de site (`docs/site/data/packs.json`) en pas `tests/test_site_reference.py`
  zo aan dat het die twee overslaat. Draai `uv run pytest -q tests/test_sources.py
  tests/test_site_packs.py tests/test_site_reference.py` in stap 4.

**Gedrag** (spec §1, §2, §10). Dit algoritme ligt niet vast in de tests, daarom staat het hier:

```
root   = dirname "$0"/../../..   (absoluut, met cd -P)
dev    = grep -q '^version = "dev"' "$root/.lighthouse/manifest.toml"
bin    = "$root/.lighthouse/bin"
als dev, geen "$bin/use-path" en `go` op het PATH:
    head = git -C "$root" rev-parse HEAD
    als "$bin/lighthouse" ontbreekt of "$bin/stamp" ≠ head: start_build
    als "$bin/lighthouse" uitvoerbaar is: LH = "$bin/lighthouse"
LH = LH, anders `command -v lighthouse`
geen LH:  blokkerend → stderr §1-melding, exit 2;  anders → {"systemMessage": melding}, exit 0
          bij dev noemt de melding `go install ./cmd/lighthouse` en `.lighthouse/bin/use-path`
"$LH" hook "$@";  rc
rc = 3, of rc 1 met `unknown command "hook"` op stderr (oude PATH-binary): dev of niet-blokkerend → {"systemMessage": stderr-tekst}, exit 0;  anders exit 2
anders:   exit rc   (stdout én stderr van LH ongemoeid; run.sh schrijft dan zelf niets)
          stderr van LH gaat naar een tijdelijk bestand en wordt daarna met `cat >&2` doorgegeven;
          bij rc 3 wordt de tekst als JSON-string geëscaped (backslash, aanhalingsteken, nieuwe
          regel) met `sed`/`awk`, zonder jq of Python

start_build:
    "$bin/build.failed" bevat head en is jonger dan 60 s → niets
        (meten met `find "$bin/build.failed" -newer "$bin/pause.ref"`; `pause.ref` wordt bij elke
        mislukking met `touch -t` op nu min 60 s gezet. Geen `stat -c` of `date -d`:
        `test_portable_shell.py` verbiedt ze)
    mkdir "$bin/build.lock" mislukt:
        lock ouder dan 120 s (find -prune -mmin +1; `+2` rondt op naar >180 s) → rm -rf, één nieuwe poging; anders niets
    los proces (setsid als het bestaat, anders nohup), stdin, stdout en stderr naar /dev/null:
        cd "$root" && go build -o "$bin/lighthouse.tmp.$$" ./cmd/lighthouse >"$bin/build.log" 2>&1
        gelukt: mv tmp → lighthouse; echo head > stamp.tmp; mv → stamp; rm -f build.failed
        mislukt: echo head > build.failed; rm -f tmp
        rmdir "$bin/build.lock"
```

- [ ] **Stap 1: tests** in `tests/pack/test_run_sh.py`. Elke test bouwt een tijdelijke repo
  (`git init`, één commit, `.claude/hooks/lighthouse/{run.sh,blocking}` gekopieerd uit
  `pack/core/hooks/`, een manifest). Er is een nep-`go` op het PATH: die slaapt `FAKE_GO_SLEEP`
  seconden, telt zijn aanroepen in een bestand en schrijft een nep-`lighthouse`-script naar het
  `-o`-pad. Er is ook een nep-`lighthouse` op het PATH waarvan rc, stdout en stderr in te
  stellen zijn.
  - `test_missing_binary_blocks_blocking_hook`: geen `lighthouse`, niet-dev,
    `main-commit-gate`. rc 2, stderr begint met `COULD NOT CHECK: lighthouse not on PATH`.
  - `test_missing_binary_warns_for_other_hook`: `worktree-check`. rc 0, stdout is JSON met
    `systemMessage` en dezelfde tekst.
  - `test_unknown_hook_maps_per_list`: nep-binary rc 3. Blokkerend rc 2, anders rc 0 met
    melding. Bij dev altijd rc 0 met melding.
  - `test_binary_output_passes_untouched`: nep-binary print `{"decision":"block"}` en rc 2.
    run.sh geeft byte voor byte dezelfde stdout en stderr en rc 2. Een tweede test: rc 3 met
    aanhalingstekens, backslash en nieuwe regel in de tekst geeft geldige JSON.
  - `test_parallel_calls_build_once_and_never_wait`: dev, `FAKE_GO_SLEEP=3`, 8 parallelle
    aanroepen. Elke aanroep is binnen 2 s klaar (ruim onder de 3 s van de nep-build; spec: ruim binnen 5 s). Na ≤ 5 s is de teller 1,
    `.lighthouse/bin/lighthouse` uitvoerbaar, `stamp` = `HEAD` en de lock weg.
  - `test_runs_previous_binary_during_build`: oude checkout-binary aanwezig, stempel
    verouderd. De aanroep draait de oude binary (te zien aan de uitvoer) terwijl de build loopt.
  - `test_stale_lock_is_removed`: `build.lock` met mtime 150 s geleden. Er start een build.
    `test_fresh_lock_starts_nothing`: lock van nu, teller blijft 0.
  - `test_failed_build_pauses_60s`: nep-`go` faalt. Een tweede aanroep start niets
    (teller 1), behalve met `build.failed` 61 s oud (teller 2) of na een nieuwe commit.
    `build.log` bevat de uitvoer van `go`.
  - `test_use_path_skips_build`: `.lighthouse/bin/use-path` bestaat. Teller 0, de
    PATH-binary draait.
  - `test_no_go_uses_path`: PATH zonder `go`. Teller 0, de PATH-binary draait.
  - `test_not_dev_never_builds`: manifest `v0.12.1`. Teller 0.
  - `test_run_sh_from_subdir_with_space`: repo in `tmp/a b/`, cwd een submap. De build start
    en de binary draait (Review-focus 4).
  - `test_build_detaches_without_setsid`: een PATH met alleen de nodige tools, zonder
    `setsid`. Gedraagt zich als `test_parallel_calls_build_once_and_never_wait`
    (Review-focus 5).
  - `test_shellcheck_clean`: `shellcheck -s sh pack/core/hooks/run.sh` (skip als
    `shellcheck` ontbreekt).
- [ ] **Stap 2:** `uv run pytest -q tests/pack/test_run_sh.py`. Verwacht: FAIL.
- [ ] **Stap 3:** implementeer `run.sh` en `blocking`, voeg `.lighthouse/bin/` toe aan
  `.gitignore`, en draai `go run ./cmd/lighthouse init --upgrade`.
- [ ] **Stap 4:** run de tests opnieuw. Verwacht: PASS. Draai ook
  `go test -race ./internal/initcmd/ ./internal/packs/` (golden en core-units) en de drie
  site-/sources-tests uit *Bestanden*. Is
  `places.golden` veranderd, gebruik dan `-update`.
- [ ] **Stap 5: commit** `feat(hooks): run.sh met bouw uit de checkout bij dev (#274)`, inclusief
  de geïnstalleerde kopie.

### Taak 8: skills en `pack/python` op de binary

**Bestanden:**
- Wijzig: elke `SKILL.md` en `pack/core/instructions/AGENTS.md` onder `pack/` met
  `python3 .lighthouse/lib/lighthouse_lib/config.py` (225 keer) of `…/headings.py` (6 keer).
  Volledige lijst: `grep -rlE 'lighthouse_lib/(config|headings)\.py' pack --include=*.md`.
- Wijzig: `pack/python/scripts/quality/prcov_issues.py`, `pack/python/scripts/quality/ratchet.py`
- Nieuw: een sessie-fixture in `tests/conftest.py` (of `tests/pack/conftest.py`) die
  `go build -o <tmp>/lighthouse ./cmd/lighthouse` één keer draait en `<tmp>` vooraan op het PATH
  zet voor tests die hem vragen (`lighthouse_bin`)
- Wijzig: `tests/pack/test_skill_contracts.py` (`_CONFIG_RE` en `test_config_keys_known`, en de
  `tomllib`-eis rond regel 326), `tests/pack/go/test_go_writing.py` (:142-147) en
  `tests/pack/python/test_python_writing.py` (:90-95): zet `config\.py` om naar
  `lighthouse config`. Elke test asserteert ook dat er minstens één treffer is, anders slaagt hij
  leeg.
- Wijzig: de tests uit
  `grep -rlE '(lighthouse_lib/)?(config|headings)\.py' tests internal` die tekst van skills
  asserteren (`test_*_skill.py`, `test_getting_started.py`, `test_instruction_files.py`,
  `internal/packs/core_test.go`, `internal/initcmd/{initcmd,places}_test.go`), en de tests van
  `prcov_issues` en `ratchet`
- Niet: de bash-hooks, `check-instructions.sh`, `shell-lint-check.sh`, `shellcheck-repo.sh`,
  `ty-gate.sh` (die horen bij stap 2–11). `pack/SOURCES.toml` alleen voor nieuwe bestanden (taak 7).

**Interfaces:**
- Gebruikt: `lighthouse config [--json] <key>`, `lighthouse headings <key>`,
  `lighthouse protocol robot|parse-header` (taken 2–4).

- [ ] **Stap 1: schrijf de guard-test.** `test_no_installed_skill_calls_config_or_headings_py`
  in `tests/pack/test_skills_issue_flow.py` (of een nieuw `tests/pack/test_no_python_lib.py`)
  doet een verse init in een tijdelijke map met `core`, `go`, `node`, `shell`, `expo` en
  `python`. Hij assert dat geen bestand onder `.claude/skills/`, `.agents/skills/` of
  `AGENTS.md` de reeksen `config.py` of `headings.py` nog bevat. Dit is AC 2.
- [ ] **Stap 2:** run hem. Verwacht: FAIL met de lijst van bestanden.
- [ ] **Stap 3: vervang mechanisch.**
  `python3 .lighthouse/lib/lighthouse_lib/config.py` → `lighthouse config`, en
  `python3 .lighthouse/lib/lighthouse_lib/headings.py` → `lighthouse headings`. Lees daarna de
  diff op zinnen die het oude mechanisme uitleggen (bijvoorbeeld "rc 1 van `config.py`") en pas
  die tekst aan. Tekst die `dangerouslyDisableSandbox` alleen voor `config.py` noemt, blijft
  staan als de binary dezelfde sandbox nodig heeft.
- [ ] **Stap 4: `pack/python`.** Vervang `from lighthouse_lib import config, headings, protocol`
  door een helper `_lh(*args, stdin=None) -> subprocess.CompletedProcess` die `lighthouse`
  aanroept.
  - `board()` gebruikt `lighthouse config --json forge.board`, met rc 1 → `None`.
  - `CONTROLLER_STATUS_OPTION` wordt de constante `"Controller"`.
  - `repo_root()`: eerst `LIGHTHOUSE_REPO_ROOT`, anders `git rev-parse --show-toplevel` (zoals
    `config.repo_root`; `test_worktree_from_env_or_main_checkout` bewaakt dit).
  - `protocol.ROBOT` gebruikt `lighthouse protocol robot`.
  - `protocol.parse_header` gebruikt `lighthouse protocol parse-header`.
  - `headings.heading('kind_handoff', lang)` gebruikt `lighthouse headings kind_handoff`.
  - In `ratchet.py` wordt `config.lookup` `lighthouse config --json` (lijstsleutels als
    `python.source_dirs` blijven zo een lijst; `_dirs` asserteert dat).
  - Rc 2 wordt een eigen `ConfigError` met de stderr-tekst. Ontbreekt `lighthouse` op het PATH
    (`FileNotFoundError`), dan ook een `ConfigError`, met uitleg dat de nightly de binary nodig heeft.
  - De `sys.path`-truc voor `lighthouse_lib` (`prcov_issues.py:32-40`) gaat weg.
- [ ] **Stap 4b:** controleer `.github/workflows/ci.yml`: heeft de Python-job `actions/setup-go`
  (go.mod eist `go 1.26.4`)? Zo niet, voeg het toe, en laat Go-only wijzigingen ook de
  Python-job draaien als tests de binary gebruiken (`ci/domains.py`).
- [ ] **Stap 5:** werk de tekst-asserties in de tests bij en geef de prcov- en ratchet-tests de
  fixture `lighthouse_bin`. Draai `go run ./cmd/lighthouse init --upgrade`.
- [ ] **Stap 5b:** de geïnstalleerde skills roepen nu `lighthouse config` aan, en de binary op het
  PATH kent dat subcommando nog niet (`unknown command`, rc 1, door skills gelezen als "niet
  gezet"). Draai `go install ./cmd/lighthouse` en controleer `lighthouse config forge.repo`
  (rc 0) vóór je een skill start. Zet deze stap ook in de PR-tekst voor gebruikers op `main`.
- [ ] **Stap 6:** `go test -race ./...` en `uv run pytest -q`. Verwacht: PASS, de guard-test
  ook.
- [ ] **Stap 7: commits**, één per logische wijziging:
  `refactor(skills): lighthouse config en headings in plaats van Python (#274)` en
  `refactor(python): prcov_issues en ratchet via de binary (#274)`, elk met de geïnstalleerde
  kopie.

### Taak 9: dogfood, changelog, de `python3`-grep op de issues

**Bestanden:**
- Nieuw: `changelog.d/issue-274-fundament-zonder-python.md`
- Geen code. Alleen comments op #275–#285.

- [ ] **Stap 1: changelog-fragment** `type: added`, met de tekst: *"`lighthouse config`,
  `lighthouse headings` and `lighthouse protocol` replace the Python config readers in skills;
  hooks can run through `.claude/hooks/lighthouse/run.sh`."* Let op: volgens
  `docs/releasing.md` wacht een merge met alleen `added` op maandag. Spec §10 rekent op een
  release direct na de merge, maar de bouw uit de checkout dekt `main` van deze repo nu ook. Een
  handmatige `workflow_dispatch` is nodig als een andere repo de binary eerder nodig heeft.
  Vermeld dat in de PR.
- [ ] **Stap 2:** `go run ./cmd/lighthouse init --upgrade` en daarna nog een keer. Verwacht de
  tweede keer: `No changes.`
- [ ] **Stap 3: de grep.** Doe een verse `go run ./cmd/lighthouse init` met `core`, `go`, `node`,
  `shell` en `expo` in een tijdelijke map (zonder `python`, zoals de milestone-DoD), en daarna
  `grep -rn python3 .` daar. Deel elke treffer toe aan een stap volgens *Opdeling* in de spec.
- [ ] **Stap 4: post per issue één comment.** Controleer eerst met
  `gh api repos/<repo>/issues/<nr> --jq .title` dat het nummer bij de stap hoort (stap n =
  #273+n). Het comment is in het Nederlands: kop "`python3`-treffers uit de grep van #274",
  daaronder de treffers als `pad:regel`, met het grep-commando erbij. Is een stap zonder
  treffers, dan zegt het comment dat. Dit is AC 6.
- [ ] **Stap 5: controleer de bevriezing** met
  `git diff origin/main -- pack/core/lib/config.py pack/core/lib/headings.py pack/core/lib/datadir.py pack/core/lib/protocol.py .lighthouse/lib/`.
  Verwacht: leeg. Dit is AC 7.
- [ ] **Stap 6: commit** `docs(changelog): fragment voor #274`.

---

## Klaar

- AC 1: taken 1 en 2. AC 2: taak 8. AC 3 en AC 4: taak 7. AC 5: taak 5. AC 6, 7, 9 en 10:
  taak 9. AC 8 ("weg is weg, bevroren is ongewijzigd"): taak 9, stap 5. Deze stap haalt niets
  weg.
- Daarna: `/lighthouse-write-pr`, met `Closes #274`.
