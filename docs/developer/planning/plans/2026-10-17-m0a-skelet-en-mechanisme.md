# M0 (a): Go-skelet, `lighthouse init` en het installatiemechanisme — implementatieplan (#4)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Een Go-binary met `lighthouse init`, `init --upgrade [--force]`, `--yes` en `lighthouse version`, die `pack/core` (alleen `main-commit-guard` en de configlezer) in een projectrepo installeert, met manifest, markers, de merge van `.claude/settings.json` en een beheerd blok in `AGENTS.md`.

**Architecture:** De pakketinhoud zit via `go:embed` in de binary. `internal/packs` vertaalt een pakketmap naar *eenheden* (bestand, beheerd blok, hookregistratie) met hun gewenste inhoud. `internal/reconcile` vergelijkt per eenheid de gewenste inhoud, de huidige inhoud en de hash in het manifest, en beslist: toevoegen, vervangen, verwijderen, ongewijzigd, adopteren of conflict. `internal/initcmd` bouwt daaruit de nieuwe bestanden, toont per bestand een unified diff, vraagt akkoord en schrijft. De guard en zijn parser verhuizen uit paia naar `pack/core`. De configlezer is nieuw Python in `pack/core/lib/config.py`.

**Tech Stack:** Go 1.26 (lokaal `go1.26.4`), `github.com/spf13/cobra` v1.10.2, `github.com/BurntSushi/toml` v1.6.0, `github.com/pmezard/go-difflib` v1.0.0. Python ≥ 3.11 voor de configlezer, `uv` en `pytest` voor de Python-lane, bash en `jq` voor de guard. CI op `ubuntu-latest`, actions op SHA gepind zoals in (a0): `actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1` (v7.0.1), `actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e` (v7.0.0), `golangci/golangci-lint-action@ba0d7d2ec06a0ea1cb5fa41b2e4a3ab91d21278a` (v9.3.0) met golangci-lint v2.15.0, `astral-sh/setup-uv@1c37ad07a6a961277cf70c0d37d6f313000f5884` (v10.3.0).

**Spec:** [`2026-10-17-m0-pakket-design.md`](../specs/2026-10-17-m0-pakket-design.md), secties *Binary*, *Versiemarker en manifest*, *init*, *Configlezer*, *Portabiliteit* en *Opdeling (a)*. Issue #4, sub-issue van #1.

**Voorwaarde:** (a0) is gemerged. PR #5 landde in `docs/m0-spec` (`f2a170b`), niet in `main`; deze branch staat daarom op `origin/docs/m0-spec`. Zodra (a0) op `main` staat: `git rebase origin/main`. (a0) leverde `.github/workflows/ci.yml` (job `python`), `pyproject.toml` met `uv.lock`, en de Engelse bootstrapkopieën van de guards in `.claude/hooks/` met hun tests in `tests/hooks/`.

## Ontwerpkeuzes

1. **Init-stappen 3 en het meten van board-ID's schuiven naar (b).** `host.toml` voorstellen en `gh project field-list` draaien hebben in (a) geen gebruiker: de guard leest geen van beide (spec, *init* punt 3). Ze komen in (b), bij de eerste skill die ze leest. Hetzelfde geldt voor het afbeelden van `skills/` (de vorm van de harnasvarianten), het invullen van workflowbestanden uit `[ci]` (*Portabiliteit*) en de Superpowers-check (*init* punt 1). `packs.Load` geeft op `skills/` een luide fout, zodat niets stil wegvalt.
2. **De guard komt uit de bootstrap van (a0).** Die is al een Engelse vertaling van paia@`231f6e84`, met geporte tests. `pack/core` neemt `main-commit-guard.sh`, `git_invocations.py` en hun tests daarvan over. Alleen besluit 14 geldt: de lib-locatie, de repo-root via git, en geen deny-lijstliteral (de kopregel `Bootstrap copy of personal_ai_agent@…` gaat eruit; de herkomst staat in `pack/SOURCES.toml`). De bootstrap in `.claude/hooks/` en `tests/hooks/` blijft staan tot (b).
3. **Hookregistraties in plugin-vorm.** `pack/core/hooks/hooks.json` volgt het formaat van een Claude Code-plugin, met `${CLAUDE_PLUGIN_ROOT}/hooks/<naam>`. `init` herschrijft dat voorvoegsel naar `"$CLAUDE_PROJECT_DIR"/.claude/hooks/lighthouse/`. Zo blijft een plugin later goedkoop (besluit 5).
4. **Een conflict weigert de hele run.** Is er één lokaal gewijzigde eenheid en geen `--force`, dan schrijft `init` niets, toont hij de diff per conflict en eindigt hij met rc 1. Een lokaal verwijderde eenheid telt ook als lokale wijziging; `init` zet hem niet stil terug.
5. **`init` op een geïnitialiseerde repo weigert** en verwijst naar `--upgrade`; `--upgrade` zonder manifest verwijst naar `init`. Beide delen één pad. `--force` werkt bij beide (bij `init`: een bestaand bestand op een lighthouse-pad).
6. **De guard vindt zijn lib** via `LIGHTHOUSE_REPO_ROOT`, anders `git -C "$(dirname "$0")" rev-parse --show-toplevel`, en draait de parser als script op pad (`python3 <root>/.lighthouse/lib/lighthouse_lib/git_invocations.py`), niet met `-m`. `-m` zet de cwd vooraan in `sys.path`, en de cwd is de repo van de gebruiker. Vindt hij de lib niet, dan faalt de parser en blokkeert de guard ("KON NIET CONTROLEREN"): fail-closed.
7. **Configlezer: rc 1 voor "niet gezet".** `python3 -m lighthouse_lib.config <sleutel>` geeft rc 0 met de waarde, rc 1 als de sleutel in een geldige config ontbreekt (een optionele tabel als `[forge.board]`), en rc 2 bij een configuratiefout. Zo kan een skill een bordstap overslaan zonder een kapotte config te verbergen (besluit 16).
8. **`lighthouse.toml` in (a):** `init` stelt `[project] language = "en"` voor, en `[forge]` met `kind = "github"` en `repo` als `origin` een GitHub-remote is. Zonder GitHub-remote laat hij `[forge]` weg en meldt dat. Aanvullen gebeurt door tekst in te voegen (spec, *init* punt 2), nooit door het bestand opnieuw te schrijven.
9. **`pack/SOURCES.toml` begint in (a)**, met de paia-SHA van de twee overgenomen bestanden. Hij noemt geen repo-naam, want de literal-grep loopt ook over `pack/`.
10. **Afhankelijkheden komen uit `pack/<p>/requires.toml`** (tool → eenheden die hem nodig hebben). Een ontbrekende tool meldt `init` met die namen; hij stopt er niet op.

## Global Constraints

- Paden in een projectrepo, letterlijk uit de spec: hooks `.claude/hooks/lighthouse/<naam>.sh`; Python `.lighthouse/lib/lighthouse_lib/`; scripts `.lighthouse/scripts/`; agents `.claude/agents/lighthouse-<naam>.md`; manifest `.lighthouse/manifest.toml`.
- Marker: frontmatter `metadata.lighthouse: "<pakket>@<versie>"`; shell, Python en YAML een regel `# lighthouse: <pakket>@<versie>`, onder de shebang als die er is. Geen marker in JSON en niet in `pull_request_template.md`.
- Blokmarkers: `<!-- lighthouse:begin <pakket> -->` en `<!-- lighthouse:end <pakket> -->`, alleen in `AGENTS.md`. `CLAUDE.md` krijgt alleen `@AGENTS.md`, en alleen als `init` hem aanmaakt.
- Versie: de tag van de binary; `(devel)`, een pseudo-versie of een `+dirty`-build schrijft `dev`.
- Een registratie waarvan het commando `.claude/hooks/lighthouse/` bevat, is van lighthouse; elke andere registratie blijft staan, op dezelfde plek.
- `init` schrijft niets zonder akkoord of `--yes`.
- Repo-root in Python en shell: `LIGHTHOUSE_REPO_ROOT`, anders `git rev-parse --show-toplevel`. Nooit afgeleid uit het eigen pad.
- Env-overrides van de configlezer: `LIGHTHOUSE_TOML`, `LIGHTHOUSE_HOST_TOML`.
- Geen literal uit `ci/literal-denylist.txt` in `pack/` (taak 4).
- Elk bestand in `pack/core/lib/` parseert met de syntaxis van Python 3.9 (macOS-host draait 3.9.6) en heeft geen naam uit de standaardbibliotheek.
- Taal: Go-meldingen, het AGENTS.md-blok en de configlezer in het Engels; dit plan, commits en PR's in het Nederlands.
- Go-tests met alleen `testing`; Go-pakketinhoud in tests via `testing/fstest.MapFS`.

## Review Focus

1. **`__pycache__` in `pack/`.** De Python-lane maakt `pack/core/lib/__pycache__/`; `//go:embed all:core` neemt dat mee. Verwacht: `packs.Load` slaat `__pycache__/`, `*.pyc`, `.pytest_cache/` en `.DS_Store` over. Test in taak 7.
2. **Hookscript zonder uitvoerrecht.** `embed.FS` draagt geen modus. Zonder `0755` faalt de hook met een fout die Claude Code niet als blokkade leest: fail-open. Verwacht: elk bestand dat met `#!` begint, krijgt `0755`. Test in taak 7, en de e2e-test draait het commando zoals `settings.json` het noemt (taak 15).
3. **Een `settings.json` die `init` niet begrijpt** (JSONC-commentaar, `hooks` als array, een groep zonder `hooks`-array, dubbele sleutels). Verwacht: luide fout, niets geschreven. Test in taak 10.
4. **`AGENTS.md` met CRLF of kapotte markers** (begin zonder end, twee keer hetzelfde begin). Verwacht: CRLF werkt; kapotte markers geven een fout met het regelnummer, niets geschreven. Test in taak 9.
5. **Een guard die om de verkeerde reden blokkeert.** Ontbreekt `jq` of de lib, dan eindigt de guard ook op rc 2. Verwacht: de e2e-test eist de tekst `geblokkeerd` en niet `KON NIET CONTROLEREN`, en eist rc 0 op een featurebranch. Test in taak 15. Daarnaast: `config.py` onder Python < 3.11 geeft rc 2 met een melding, geen traceback (taak 3).

---

## Bestandsstructuur

```
go.mod, go.sum                     module github.com/CorneeldH/lighthouse
.golangci.yml                      golangci-lint v2, default: standard
pyproject.toml, uv.lock            uit (a0); ongewijzigd
cmd/lighthouse/main.go             os.Exit(cli.Execute())
internal/version/                  versie uit debug.ReadBuildInfo
internal/cli/                      Cobra: root, version, init
internal/orderedjson/              JSON met behouden sleutelvolgorde (settings.json)
internal/packs/                    pakketmap → eenheden; marker.go
internal/manifest/                 .lighthouse/manifest.toml lezen en schrijven
internal/agentsmd/                 beheerde blokken in AGENTS.md
internal/settings/                 merge van hookregistraties
internal/reconcile/                beslissing per eenheid
internal/projectconfig/            lighthouse.toml aanvullen, forge-repo uit remote
internal/deps/                     afhankelijkheden controleren
internal/initcmd/                  init en --upgrade orkestreren
pack/pack.go                       //go:embed all:core
pack/SOURCES.toml                  herkomst van overgenomen bestanden (niet ge-embed)
pack/core/hooks/main-commit-guard.sh, hooks/hooks.json
pack/core/lib/__init__.py, git_invocations.py, config.py
pack/core/instructions/AGENTS.md
pack/core/requires.toml
ci/literal-denylist.txt, ci/literal-grep.sh
tests/pack/                        conftest.py, _testkit.py en de pakkettests
e2e/e2e_test.go                    //go:build e2e
```

---

### Taak 1: Go-skelet met `lighthouse version` en de Go-lane in de CI

**Files:**
- Create: `go.mod`, `cmd/lighthouse/main.go`, `internal/version/version.go`, `internal/version/version_test.go`, `internal/cli/root.go`, `internal/cli/root_test.go`, `.golangci.yml`
- Modify: `.github/workflows/ci.yml`, `.gitignore`

**Interfaces:**
- Produces: `version.String() string`; `cli.Execute() int`; `cli.NewRoot(in io.Reader, out, errOut io.Writer) *cobra.Command`. Taak 14 hangt `init` onder deze root.

- [ ] **Stap 1: Schrijf de falende tests**

```go
func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"":                                     "dev",
		"(devel)":                              "dev",
		"v0.0.0-20261017120000-abcdef123456":   "dev",
		"v0.1.1-0.20261017120000-abcdef123456": "dev",
		"v0.1.0+dirty":                         "dev",
		"v0.1.0":                               "v0.1.0",
		"v1.2.3-rc.1":                          "v1.2.3-rc.1",
	} {
		if got := normalize(in); got != want { t.Errorf("normalize(%q) = %q, want %q", in, got, want) }
	}
}
```

`TestVersionCommand` in `internal/cli`: `NewRoot` met `SetArgs([]string{"version"})` schrijft `"lighthouse dev\n"` naar `out` (onder `go test` is de versie `(devel)` of leeg).

- [ ] **Stap 2: Draai** `go test ./...` — verwacht: FAIL, `normalize` en `NewRoot` bestaan niet.
- [ ] **Stap 3: Implementeer.** `go mod init github.com/CorneeldH/lighthouse`; `go get github.com/spf13/cobra@v1.10.2`. `normalize`: `dev` bij leeg, `(devel)`, een `+` in de string of een pseudo-versie (regex `\d{14}-[0-9a-f]{12}$`); anders de string zelf. `String()` leest `debug.ReadBuildInfo().Main.Version`. `.golangci.yml`: `version: "2"`, `linters: default: standard`. `.gitignore`: `/lighthouse` erbij.
- [ ] **Stap 4: CI.** Job `go` in `ci.yml`, naast `python`: checkout (`persist-credentials: false`), `setup-go` met `go-version-file: go.mod`, `golangci-lint-action` met `version: v2.15.0`, `go test ./...`.
- [ ] **Stap 5: Draai** `go test ./... && golangci-lint run` (lokaal: `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run`) — verwacht: PASS, geen bevindingen.
- [ ] **Stap 6: Commit** `feat(skelet): Go-module met lighthouse version en Go-lane in de CI`

### Taak 2: `pack/core` met main-commit-guard en zijn parser, en de Python-lane

**Files:**
- Create: `pack/core/hooks/main-commit-guard.sh` (uit de bootstrap `.claude/hooks/main-commit-guard.sh`), `pack/core/lib/git_invocations.py` (uit `.claude/hooks/lib/git_invocations.py`), `pack/core/lib/__init__.py`, `pack/core/hooks/hooks.json`, `pack/core/instructions/AGENTS.md`, `pack/core/requires.toml`, `pack/pack.go`, `pack/SOURCES.toml`, `tests/pack/__init__.py`, `tests/pack/conftest.py`, `tests/pack/_testkit.py`, `tests/pack/test_git_invocations.py`, `tests/pack/test_main_commit_guard.py`

**Interfaces:**
- Produces: `pack.Files embed.FS` (root `core/`); de pakketindeling hieronder, die taak 7 leest. In `tests/pack/_testkit.py`: `install_core(root: Path) -> Path` (bouwt de geïnstalleerde indeling na en geeft het pad van de guard), `run_hook(hook, payload, cwd, env=None)`, `git(repo, *args)`, `init_repo(path, branch="main")`, `bash_payload(cmd, cwd)`, `tool_bin(path, *, drop="", stubs=None)`.

- [ ] **Stap 1: Neem de tests over** uit de bootstrap: `tests/hooks/test_git_invocations.py` en `tests/hooks/test_main_commit_guard.py` in hun geheel, en de helpers uit `tests/hooks/_hook_testkit.py` naar `tests/pack/_testkit.py` (zonder de bootstrap-kopregel). Pas alleen aan: `COMMIT_GUARD` wordt de guard uit `install_core(tmp)` (één keer per sessie, via een fixture of op moduleniveau met `tempfile.mkdtemp()`); `run_hook` zet standaard `LIGHTHOUSE_REPO_ROOT` op die installatie; `load_hook_lib` en de aanroepen `HOOKS / "lib" / "git_invocations.py"` wijzen naar `LIB = pack/core/lib/`; de parametrize van `test_hook_lib_stays_importable_on_python_39` loopt over `LIB.glob("*.py")`. `conftest.py` zet `PYTHONDONTWRITEBYTECODE=1`. Voeg toe:

```python
def test_guard_finds_its_lib_via_git_without_env(tmp_path):
    home = tmp_path / "project"; init_repo(home)
    guard = install_core(home)              # installatie ín een git-repo
    env = {k: v for k, v in os.environ.items() if k != "LIGHTHOUSE_REPO_ROOT"}
    feature = tmp_path / "feat"; init_repo(feature, branch="issue-1-x")
    r = subprocess.run([str(guard)], input=json.dumps(bash_payload("git commit -m x", feature)),
                       capture_output=True, text=True, cwd=feature, env=env)
    assert r.returncode == 0, r.stderr

def test_guard_blocks_when_its_lib_is_missing(tmp_path):
    repo = tmp_path / "repo"; init_repo(repo)
    r = run_hook(COMMIT_GUARD, bash_payload("git commit -m x", repo), repo,
                 env={"LIGHTHOUSE_REPO_ROOT": str(tmp_path / "nergens")})
    assert r.returncode == 2 and "KON NIET CONTROLEREN" in r.stderr

def test_lib_names_do_not_shadow_the_stdlib():
    names = {p.stem for p in LIB.glob("*.py")} - {"__init__"}
    assert not names & set(sys.stdlib_module_names)
```

- [ ] **Stap 2: Draai** `uv run pytest tests/pack -q` — verwacht: FAIL, `pack/core` bestaat niet.
- [ ] **Stap 3: Kopieer de twee bronbestanden** uit de bootstrap, zonder hun kopregel `Bootstrap copy of …`, en wijzig in de guard alleen de parseraanroep:
    ```bash
    LIB_ROOT="${LIGHTHOUSE_REPO_ROOT:-$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null)}"
    FINDINGS=$(printf '%s' "$CMD" | python3 "$LIB_ROOT/.lighthouse/lib/lighthouse_lib/git_invocations.py" "$CWD" 2>"$PARSER_ERR")
    ```
  `git_invocations.py` blijft verder gelijk.
- [ ] **Stap 4: Schrijf de pakketbestanden.**
  - `hooks.json`: `{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/hooks/main-commit-guard.sh", "timeout": 15}]}]}}`.
  - `instructions/AGENTS.md`, letterlijk:
    ```markdown
    ## Lighthouse: core

    - Nothing lands directly on `main`. Work on a branch in a worktree; `main-commit-guard` blocks `git commit` and `git push` to `main`.
    - Escape hatch, deliberately and once: `touch .claude/allow-main` in the repo root, and remove it afterwards.
    ```
  - `requires.toml`: `git = ["main-commit-guard"]`, `jq = ["main-commit-guard"]`, `python3 = ["main-commit-guard", "lighthouse_lib.config"]`, `tomllib = ["lighthouse_lib.config"]`.
  - `lib/__init__.py`: alleen een docstring; hij importeert niets, zodat `git_invocations` op 3.9 los blijft van `config`.
  - `pack/pack.go`: `//go:embed all:core` → `var Files embed.FS` (`all:` omdat `__init__.py` met `_` begint).
  - `pack/SOURCES.toml`: per bestand `[[file]]` met `path`, `source_path` (het pad in de bronrepo) en `source_sha = "231f6e84fa709dd37c4fc8578453907a8a6efdf6"`, met een commentaarregel dat de tekst via de Engelse bootstrap van (a0) kwam.
- [ ] **Stap 5: Draai** `uv run pytest -q` — verwacht: PASS (bootstrap- en pakkettests). `go build ./...` — verwacht: geen fout. De bestaande job `python` draait de nieuwe tests al mee.
- [ ] **Stap 6: Commit** `feat(core): main-commit-guard en git_invocations naar pack/core, met Python-lane`

### Taak 3: Configlezer `lighthouse_lib.config`

**Files:**
- Create: `pack/core/lib/config.py`, `tests/pack/test_config.py`
- Modify: `tests/pack/conftest.py`: maak `lighthouse_lib` importeerbaar via een symlink `<tmp>/lighthouse_lib → pack/core/lib`, met `<tmp>` vooraan in `sys.path`. De subprocess-tests zetten `PYTHONPATH=<tmp>`.

**Interfaces:**
- Consumes: de lib-indeling uit taak 2. Fixtures in `test_config.py`: `cfg_env` (zet `LIGHTHOUSE_TOML` op `_write(tmp_path, VALID)`) en `cfg_env_without_board` (idem met `_without(("forge", "board"))`).
- Produces (Python): `ConfigError(Exception)`; `REQUIRED_STATUS_OPTIONS = ("To do", "In progress", "Done", "On hold")`; dataclasses `Board(project_number: int, project_id: str, status_field_id: str, status_options: Mapping[str, str], priority_field_id: str | None, priority_options: Mapping[str, str])`, `Config(kind: str, repo: str, language: str, board: Board | None, raw: Mapping[str, Any])` met properties `owner` en `name`, `Host(name: str, tailscale_ip: str)`; `repo_root() -> Path`; `config_path() -> Path`; `host_path() -> Path`; `load(path: Path) -> Config`; `load_host(path: Path) -> Host`; `lookup(cfg: Config, key: str) -> object` (`KeyError` als niet gezet); `is_local(ip: str) -> bool`; `main(argv: list[str] | None = None) -> int`.
- CLI: `python3 -m lighthouse_lib.config <sleutel>` en `python3 -m lighthouse_lib.config host-check`.

- [ ] **Stap 1: Schrijf de falende tests.** Neem de gevallen over uit paia `tests/unit/test_controller_identity.py` en `tests/unit/test_controller_host.py`, met `VALID` zonder `[controller]` en zonder de optie `Controller`, en met `"Acme/widgets"` als repo. Behoud: de afwijzingen van `forge.kind`, de vijf ongeldige `repo`-vormen, ontbrekende sleutels onder `[forge]` en (als hij er is) `[forge.board]`, het verkeerde type van `project_number`, ongeldige TOML en UTF-8, `priority_*` optioneel, extra statusopties toegestaan, en alle host-gevallen (ontbrekende sleutels, de zeven ongeldige IP's, lege naam, `is_local` op `127.0.0.1` en `192.0.2.1`, `HostError`-paden worden `ConfigError` met "not measurable"). Elke foutmelding bevat het pad. Nieuw:

```python
def test_board_is_optional(tmp_path):
    assert load(_write(tmp_path, _without(("forge", "board")))).board is None

def test_controller_option_only_required_with_controller_table(tmp_path):
    load(_write(tmp_path, VALID))  # geen [controller], geen "Controller": geldig
    data = _with(("controller",), {"state_dir": "x"})
    with pytest.raises(ConfigError, match="Controller"):
        load(_write(tmp_path, data))

def test_controller_table_is_not_validated(tmp_path):
    data = _with(("forge", "board", "status_options", "Controller"), "e")
    data["controller"] = {"plekken": "acht"}   # paia's tabel: alleen aanwezigheid telt
    load(_write(tmp_path, data))

@pytest.mark.parametrize("value", ['"de"', '""', "1"])
def test_language_must_be_nl_or_en(tmp_path, value):
    path = tmp_path / "lighthouse.toml"
    path.write_text(_render(VALID) + f"\n[project]\nlanguage = {value}\n")
    with pytest.raises(ConfigError, match="project.language"):
        load(path)

def test_language_defaults_to_en(tmp_path):
    assert load(_write(tmp_path, VALID)).language == "en"

def test_cli_prints_value(cfg_env, capsys):
    assert main(["forge.repo"]) == 0 and capsys.readouterr().out == "Acme/widgets\n"
    assert main(["forge.owner"]) == 0
    assert main(["forge.board.status_options.In progress"]) == 0  # sleutel met spatie, één argv

def test_cli_absent_key_is_rc_1(cfg_env_without_board, capsys):
    assert main(["forge.board.project_id"]) == 1
    assert "not set" in capsys.readouterr().err

def test_cli_broken_config_is_rc_2_without_traceback(tmp_path):
    # subprocess: python3 -m lighthouse_lib.config forge.repo met LIGHTHOUSE_TOML op ongeldige TOML
    assert r.returncode == 2 and "Traceback" not in r.stderr

def test_cli_table_is_rc_2(cfg_env): assert main(["forge.board"]) == 2

def test_missing_tomllib_is_rc_2(monkeypatch, cfg_env, capsys):
    monkeypatch.setitem(sys.modules, "tomllib", None)
    assert main(["forge.repo"]) == 2 and "3.11" in capsys.readouterr().err

def test_repo_root_from_env_then_git(monkeypatch, tmp_path): ...  # env wint; anders rev-parse vanuit cwd
def test_host_check_rc(...): ...  # 0 herkend (127.0.0.1), 1 niet lokaal (192.0.2.1), 2 ontbreekt
```

- [ ] **Stap 2: Draai** `uv run pytest tests/pack/test_config.py -q` — verwacht: FAIL, `ModuleNotFoundError: lighthouse_lib.config`.
- [ ] **Stap 3: Implementeer `config.py`** met 3.9-syntaxis en `from __future__ import annotations`; `tomllib` pas importeren in `load`/`load_host`, en een `ImportError` wordt `ConfigError("python3 >= 3.11 is required (tomllib)")`. `config_path()`: `LIGHTHOUSE_TOML`, anders `repo_root() / "lighthouse.toml"`. `host_path()`: `LIGHTHOUSE_HOST_TOML`, anders `~/.config/lighthouse/host.toml`. Validatie zoals `identity.py` (`_get`, `_options`, de repo-regex `[A-Za-z0-9._-]+/[A-Za-z0-9._-]+`), met de verschillen uit de tests. `lookup`: `forge.owner` en `forge.name` zijn afgeleid; `host.*` leest `load_host(host_path())`; verder loopt hij door `raw`. Per niveau wint een exacte sleutel, anders de sleutel `k` waarvoor de rest met `k + "."` begint. Bij `__main__`: delegeer naar `importlib.import_module("lighthouse_lib.config").main()`, zoals in `identity.py`. Meldingen: `config: <tekst>` op stderr.
- [ ] **Stap 4: Draai** `uv run pytest -q` — verwacht: PASS (ook de 3.9-test en de stdlib-naamtest over `config.py`).
- [ ] **Stap 5: Commit** `feat(core): configlezer lighthouse_lib.config zonder verplicht board of controller`

### Taak 4: Literal-grep op `pack/`

**Files:**
- Create: `ci/literal-denylist.txt`, `ci/literal-grep.sh`, `tests/test_literal_grep.py`
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Produces: `ci/literal-grep.sh [map]` (standaard `pack`): rc 0 schoon, rc 1 met de treffers (`pad:regel:tekst`) op stdout, rc 2 bij een ontbrekende of lege deny-lijst.

- [ ] **Stap 1: Schrijf de falende tests**

```python
def test_clean_dir_passes(tmp_path):
    (tmp_path / "a.sh").write_text("echo hello\n")
    assert _grep(tmp_path).returncode == 0

@pytest.mark.parametrize("hit", ["PAIA_ROOT", "Beelink", "/home/x", "4-lane:stalled", "PVT_kwHOAGmU3s4BfI-i"])
def test_hit_fails_case_insensitively(tmp_path, hit):
    (tmp_path / "a.md").write_text(f"x {hit} y\n")
    r = _grep(tmp_path); assert r.returncode == 1 and "a.md:1:" in r.stdout

def test_comment_and_blank_lines_in_denylist_match_nothing(tmp_path):
    (tmp_path / "a.txt").write_text("gewone tekst\n")
    assert _grep(tmp_path).returncode == 0   # een lege regel als patroon zou alles raken

def test_pack_is_clean():
    assert _grep(REPO / "pack").returncode == 0
```

- [ ] **Stap 2: Draai** `uv run pytest tests/test_literal_grep.py -q` — verwacht: FAIL, het script bestaat niet.
- [ ] **Stap 3: Schrijf de deny-lijst**, één fixed string per regel, `#` voor commentaar: `CorneeldH`, `corneel`, `/home/`, `personal_ai_agent`, `personal-ai-agent`, `paia`, `100.76.126.126`, `beelink`, `gates.slice`; de board- en optie-ID's uit paia's `lighthouse.toml`: `PVT_kwHOAGmU3s4BfI-i`, `PVTSSF_lAHOAGmU3s4BfI-izhZeAEs`, `PVTSSF_lAHOAGmU3s4BfI-izhlFDTc`, `5995ef76`, `5c4502d5`, `4a797cd3`, `6a2f466b`, `7c834c1d`, `e78d8910`, `5866e321`, `321d79fc`; de paia-eigen labels: `3-needs-shaping`, `3-blocked-external`, `3-needs-hands`, `3-needs-decision`, `3-blocked-issue`, `3-needs-testing`, `1-loc:mac`, `1-loc:beelink`, `1-loc:container`, `2-area:intake`, `2-area:nexus`, `2-area:flow`, `2-area:models`, `2-area:clients`, `2-area:infra`, `2-area:product`, `2-area:meta`, `2-area:lighthouse`, `4-lane:review-passed`, `4-lane:stalled`, `wbso`, `open-claw`, `os-layout`, `python:uv`. Een commentaarregel legt uit waarom de GitHub-standaardlabels en de gewone woorden (`bug`, `test`, `notes`, `lane`, `tech`, `ceda` en dergelijke) er niet in staan: als substring raken ze gewone tekst.
- [ ] **Stap 4: Schrijf het script**: filter commentaar en lege regels naar een tijdelijk bestand, faal met rc 2 als dat leeg is, en draai `grep -rniF -f <tmp> -- "$dir"`.
- [ ] **Stap 5: Draai** `uv run pytest -q && ci/literal-grep.sh` — verwacht: PASS, rc 0.
- [ ] **Stap 6: CI.** Stap `ci/literal-grep.sh` in de job `python`.
- [ ] **Stap 7: Commit** `ci: literal-grep op pack/ met deny-lijst buiten pack/`

### Taak 5: `internal/orderedjson`

**Files:**
- Create: `internal/orderedjson/orderedjson.go`, `internal/orderedjson/orderedjson_test.go`

**Interfaces:**
- Produces: `type Object struct` (onexporteerde `keys []string`, `vals map[string]any`); `NewObject() *Object`; `(*Object) Get(key string) (any, bool)`; `(*Object) Set(key string, v any)` (een nieuwe sleutel komt achteraan); `(*Object) Delete(key string)`; `(*Object) Keys() []string`; `Parse(data []byte) (any, error)` (object → `*Object`, array → `[]any`, getal → `json.Number`); `Marshal(v any) ([]byte, error)` (twee spaties inspringen, geen HTML-escape, afsluitende `\n`); `Canonical(v any) ([]byte, error)` (compact, sleutels gesorteerd, voor hashes).

- [ ] **Stap 1: Schrijf de falende tests**: `TestRoundTripKeepsKeyOrder` (`{"z":1,"a":{"y":2,"b":3}}` geeft na `Parse`+`Marshal` dezelfde volgorde terug); `TestNumbersSurvive` (`15` en `1.50` blijven letterlijk); `TestNoHTMLEscape` (`"<&>"` blijft staan); `TestEmptyContainers` (`{}` en `[]` blijven `{}` en `[]`); `TestRejects` met `{"a":1,"a":2}` (dubbele sleutel), `{"a":1} x` (rommel erna), `// c\n{}` (commentaar), `{"a":1,}`; `TestCanonicalSortsKeys` (`{"b":1,"a":2}` → `{"a":2,"b":1}`).
- [ ] **Stap 2: Draai** `go test ./internal/orderedjson/` — verwacht: FAIL.
- [ ] **Stap 3: Implementeer** met `json.Decoder.Token()` en `UseNumber()`; de encoder schrijft zelf (sleutels via `json.Marshal` van de string met `SetEscapeHTML(false)`).
- [ ] **Stap 4: Draai** de test — verwacht: PASS.
- [ ] **Stap 5: Commit** `feat(init): JSON met behouden sleutelvolgorde voor settings.json`

### Taak 6: Versiemarker

**Files:**
- Create: `internal/packs/marker.go`, `internal/packs/marker_test.go`

**Interfaces:**
- Produces: `Mark(path string, content []byte, pkg, version string) ([]byte, error)`. `path` is het doelpad in de projectrepo; het bepaalt het formaat.

- [ ] **Stap 1: Schrijf de falende tests**, tabelgestuurd, met `pkg="core"`, `version="dev"`:

| geval | pad | in | uit |
|---|---|---|---|
| shebang | `x.sh` | `#!/bin/bash\necho\n` | `#!/bin/bash\n# lighthouse: core@dev\necho\n` |
| geen shebang | `x.py` | `import os\n` | `# lighthouse: core@dev\nimport os\n` |
| yaml | `x.yml` | `on: push\n` | `# lighthouse: core@dev\non: push\n` |
| bestaande marker | `x.sh` | `#!/bin/sh\n# lighthouse: core@v0.1.0\n` | `#!/bin/sh\n# lighthouse: core@dev\n` |
| leeg bestand | `__init__.py` | `` | `# lighthouse: core@dev\n` |
| frontmatter zonder metadata | `a.md` | `---\nname: x\n---\nbody\n` | `---\nname: x\nmetadata:\n  lighthouse: "core@dev"\n---\nbody\n` |
| frontmatter met metadata | `a.md` | `---\nmetadata:\n  domain: y\n---\n` | `---\nmetadata:\n  lighthouse: "core@dev"\n  domain: y\n---\n` |
| json | `s.json` | `{}` | ongewijzigd |
| PR-template | `.github/pull_request_template.md` | `---\na: b\n---\n` | ongewijzigd |
| md zonder frontmatter | `README.md` | `# t\n` | ongewijzigd |

Plus `TestMarkRejectsInlineMetadata`: `---\nmetadata: {a: b}\n---\n` geeft een fout.

- [ ] **Stap 2: Draai** `go test ./internal/packs/` — verwacht: FAIL.
- [ ] **Stap 3: Implementeer** `Mark`. Formaat per extensie: `.sh`, `.bash`, `.py`, `.yml`, `.yaml` → commentaarregel; `.md` met frontmatter → `metadata.lighthouse`, behalve `pull_request_template.md`; de rest ongewijzigd.
- [ ] **Stap 4: Draai** de test — verwacht: PASS.
- [ ] **Stap 5: Commit** `feat(init): versiemarker per bestandsformaat`

### Taak 7: Pakket → eenheden

**Files:**
- Create: `internal/packs/packs.go`, `internal/packs/packs_test.go`, `internal/packs/core_test.go`

**Interfaces:**
- Consumes: `Mark` (taak 6), `orderedjson` (taak 5), `pack.Files` (taak 2).
- Produces:
  ```go
  type Kind string // KindFile = "file", KindBlock = "block", KindHook = "hook"
  type Key struct{ Kind Kind; Path, ID string }
  type Registration struct{ Event, Matcher string; Entry *orderedjson.Object } // Matcher "" = geen matcher-veld
  type Unit struct {
  	Kind    Kind
  	Package string
  	Path    string      // doelpad, met slashes, relatief aan de repo-root
  	ID      string      // block: pakketnaam; hook: HookID(...); file: ""
  	Content []byte      // file: inhoud met marker; block: blokinhoud; hook: orderedjson.Canonical(Entry)
  	Mode    fs.FileMode // file: 0o755 als Content met "#!" begint, anders 0o644
  	Hook    *Registration
  }
  func (u Unit) Key() Key
  type Requirement struct{ Tool string; NeededBy []string } // gesorteerd op Tool
  type Pack struct{ Name string; Units []Unit; Requires []Requirement }
  const HookDir = ".claude/hooks/lighthouse/"
  func HookID(event, matcher, command string) string // event + "|" + matcher + "|" + command
  func IsLighthouseCommand(command string) bool      // strings.Contains(command, HookDir)
  func Load(fsys fs.FS, name, version string) (Pack, error) // fsys geroot op de pakketmap
  ```

- [ ] **Stap 1: Schrijf de falende tests** met `fstest.MapFS`:
  - `TestLoadMapsPaths`: `hooks/g.sh` → `.claude/hooks/lighthouse/g.sh`; `lib/a.py` → `.lighthouse/lib/lighthouse_lib/a.py`; `scripts/s.py` → `.lighthouse/scripts/s.py`; `agents/scout.md` → `.claude/agents/lighthouse-scout.md`; `instructions/AGENTS.md` → één `KindBlock` met `Path "AGENTS.md"`, `ID "core"`, `Content` = de bestandsinhoud (afsluitende `\n` gegarandeerd).
  - `TestLoadHooksJSON`: `hooks.json` geeft één `KindHook` met `Path ".claude/settings.json"`, `Hook.Event "PreToolUse"`, `Hook.Matcher "Bash"` en als command `"$CLAUDE_PROJECT_DIR"/.claude/hooks/lighthouse/g.sh`; `hooks.json` zelf wordt geen `KindFile`.
  - `TestLoadRejects`: een command zonder voorvoegsel `${CLAUDE_PLUGIN_ROOT}/hooks/`; een command naar een script dat niet in `hooks/` staat; een map `skills/` (fout bevat `skills/ is not supported yet`); een onbekende map of los bestand bovenin; ongeldige `requires.toml`.
  - `TestLoadSkipsJunk`: `lib/__pycache__/a.cpython-314.pyc`, `lib/x.pyc`, `.DS_Store`, `.pytest_cache/x` leveren geen eenheid.
  - `TestLoadModes`: `hooks/g.sh` met `#!/bin/bash` → `0o755`; `lib/a.py` zonder shebang → `0o644`.
  - `TestLoadMarks`: de inhoud van `hooks/g.sh` bevat `# lighthouse: core@v9.9.9` bij `version "v9.9.9"`.
  - `TestCorePack` (`core_test.go`, tegen het echte `pack.Files`): `Load(sub(core), "core", "dev")` geeft precies de bestanden `.claude/hooks/lighthouse/main-commit-guard.sh`, `.lighthouse/lib/lighthouse_lib/__init__.py`, `git_invocations.py`, `config.py`, één blok en één hook; `Requires` noemt `git`, `jq`, `python3`, `tomllib`.
- [ ] **Stap 2: Draai** `go test ./internal/packs/` — verwacht: FAIL.
- [ ] **Stap 3: Implementeer `Load`** met `fs.WalkDir`; `requires.toml` via `toml.Decode` naar `map[string][]string`; `go get github.com/BurntSushi/toml@v1.6.0`.
- [ ] **Stap 4: Draai** `go test ./...` — verwacht: PASS.
- [ ] **Stap 5: Commit** `feat(init): pakketmap naar eenheden, met hookregistraties uit hooks.json`

### Taak 8: Manifest

**Files:**
- Create: `internal/manifest/manifest.go`, `internal/manifest/manifest_test.go`

**Interfaces:**
- Consumes: `packs.Kind`, `packs.Key`.
- Produces:
  ```go
  const Path = ".lighthouse/manifest.toml"
  type Entry struct {
  	Kind    packs.Kind `toml:"kind"`
  	Path    string     `toml:"path"`
  	ID      string     `toml:"id,omitempty"`
  	Package string     `toml:"package"`
  	Version string     `toml:"version"`
  	SHA256  string     `toml:"sha256"`
  }
  type Manifest struct {
  	Version  string   `toml:"version"`
  	Packages []string `toml:"packages"`
  	Units    []Entry  `toml:"unit"`
  }
  func Read(root string) (Manifest, error)  // ontbreekt: errors.Is(err, fs.ErrNotExist)
  func Encode(m Manifest) []byte            // kopregel "# Managed by lighthouse init. Do not edit by hand.", units gesorteerd op (Path, ID)
  func (m Manifest) Lookup(k packs.Key) (Entry, bool)
  func Sum(b []byte) string                 // hex sha256
  ```

- [ ] **Stap 1: Schrijf de falende tests**: `TestRoundTrip` (`Read` na `Encode` geeft dezelfde `Manifest`); `TestEncodeIsDeterministic` (units in willekeurige volgorde geven dezelfde bytes); `TestReadMissing` (`fs.ErrNotExist`); `TestReadRejects`: onbekende `kind`, een `sha256` die geen 64 hex-tekens is, twee entries met dezelfde `Key`, ongeldige TOML. Elke fout noemt `.lighthouse/manifest.toml`.
- [ ] **Stap 2: Draai** `go test ./internal/manifest/` — verwacht: FAIL.
- [ ] **Stap 3: Implementeer** met `toml.NewEncoder` op de gesorteerde struct.
- [ ] **Stap 4: Draai** de test — verwacht: PASS.
- [ ] **Stap 5: Commit** `feat(init): manifest met sha256 per eenheid`

### Taak 9: Beheerde blokken in `AGENTS.md`

**Files:**
- Create: `internal/agentsmd/agentsmd.go`, `internal/agentsmd/agentsmd_test.go`

**Interfaces:**
- Produces: `Begin(pkg string) string` (`<!-- lighthouse:begin <pkg> -->`); `End(pkg string) string`; `Find(doc []byte, pkg string) (body []byte, found bool, err error)`; `Set(doc []byte, pkg string, body []byte) ([]byte, error)`; `Remove(doc []byte, pkg string) ([]byte, error)`. `body` is alles tussen de regel na de begin-marker en het begin van de end-regel.

- [ ] **Stap 1: Schrijf de falende tests**:
  - `TestSetAppendsToExistingDoc`: `"# Project\n\nTekst.\n"` + `Set(core, "x\n")` → `"# Project\n\nTekst.\n\n<!-- lighthouse:begin core -->\nx\n<!-- lighthouse:end core -->\n"`.
  - `TestSetOnEmptyDoc`: geen lege regel vooraan.
  - `TestSetReplacesOnlyInside`: tekst vóór en na het blok blijft byte voor byte gelijk.
  - `TestFindToleratesCRLF`: een doc met `\r\n` vindt het blok; `Set` behoudt `\r\n` buiten het blok.
  - `TestFindIgnoresOtherPackages`: blok `python` raakt `Find(core)` niet.
  - `TestMalformed`: begin zonder end, end zonder begin, twee keer `begin core`, `begin python` binnen `core` → fout met `AGENTS.md:<regel>`.
  - `TestRemove`: blok en de lege regel ervoor weg; de rest gelijk.
- [ ] **Stap 2: Draai** `go test ./internal/agentsmd/` — verwacht: FAIL.
- [ ] **Stap 3: Implementeer** regelgewijs; een markerregel matcht na `strings.TrimSpace` en het weghalen van `\r`.
- [ ] **Stap 4: Draai** de test — verwacht: PASS.
- [ ] **Stap 5: Commit** `feat(init): beheerde blokken per pakket in AGENTS.md`

### Taak 10: Merge van `.claude/settings.json`

**Files:**
- Create: `internal/settings/settings.go`, `internal/settings/settings_test.go`

**Interfaces:**
- Consumes: `orderedjson`, `packs.Registration`, `packs.IsLighthouseCommand`, `packs.HookID`.
- Produces: `Registrations(doc *orderedjson.Object) ([]packs.Registration, error)` (alle registraties, in bestandsvolgorde); `Apply(doc *orderedjson.Object, want []packs.Registration) error` (maakt de lighthouse-registraties precies gelijk aan `want`, laat de rest staan); `Commands(doc *orderedjson.Object) ([]string, error)` (alle commando's, voor de check op ontbrekende scripts).

- [ ] **Stap 1: Schrijf de falende tests**, elk met een JSON-fixture in de test:
  - `TestApplyAppendsToExistingGroup`: groep `PreToolUse`/`Bash` met projecthook `X` → na `Apply` `[X, lighthouse]`; andere sleutels bovenin (`permissions`, `env`) blijven in volgorde.
  - `TestApplyNewGroupAtEnd`: groep `PreToolUse`/`Edit` bestaat, `Bash` niet → nieuwe groep achteraan in `PreToolUse`.
  - `TestApplyNewEventAndHooks`: geen `hooks` → `hooks` komt achteraan in het object; geen `PreToolUse` → achteraan in `hooks`.
  - `TestApplyReplacesInPlace`: een lighthouse-registratie tussen twee projecthooks met `timeout 5` → na `Apply` met `timeout 15` op dezelfde plek.
  - `TestApplyRemovesDropped`: een lighthouse-registratie die niet in `want` staat, verdwijnt; een groep die daardoor leeg wordt, verdwijnt ook, en een event met een lege array ook. Een groep die al leeg was, blijft.
  - `TestApplyIsIdempotent`: twee keer `Apply` geeft dezelfde bytes na `orderedjson.Marshal`.
  - `TestApplyNoMatcher`: `Registration{Matcher: ""}` matcht een groep zonder `matcher`-veld en schrijft er geen.
  - `TestRejectsShape`: `hooks` als array, een event als object, een groep zonder `hooks`-array, een hook zonder `command`-tekst → fout die het JSON-pad noemt.
- [ ] **Stap 2: Draai** `go test ./internal/settings/` — verwacht: FAIL.
- [ ] **Stap 3: Implementeer** `Apply`: houd per bestaande lighthouse-registratie de positie vast via `HookID`; vervang op die plek, voeg ontbrekende achteraan in de groep toe, verwijder wat niet in `want` staat.
- [ ] **Stap 4: Draai** de test — verwacht: PASS.
- [ ] **Stap 5: Commit** `feat(init): merge van hookregistraties in .claude/settings.json`

### Taak 11: Beslissing per eenheid

**Files:**
- Create: `internal/reconcile/reconcile.go`, `internal/reconcile/reconcile_test.go`

**Interfaces:**
- Consumes: `packs.Unit`, `packs.Key`, `manifest.Manifest`, `manifest.Sum`.
- Produces:
  ```go
  type Action string // Add, Replace, Remove, Unchanged, Adopt, Conflict
  type Current struct{ Content []byte; Present bool }
  type Change struct {
  	Key     packs.Key
  	Action  Action
  	Desired *packs.Unit     // nil bij Remove
  	Entry   *manifest.Entry // nil als de eenheid niet in het manifest staat
  	Current Current
  }
  func Plan(desired []packs.Unit, m manifest.Manifest, current map[packs.Key]Current) []Change // gesorteerd op (Path, ID)
  ```

- [ ] **Stap 1: Schrijf de falende test** `TestPlanTable` met deze tabel (D = gewenst, C = huidig, M = hash in het manifest):

| D | C | M | Action |
|---|---|---|---|
| a | afwezig | — | Add |
| a | a | — | Adopt |
| a | b | — | Conflict |
| a | a | a | Unchanged |
| a2 | a | a | Replace |
| a | b | a | Conflict |
| a | b | b | Replace |
| a | afwezig | a | Conflict |
| a | a | b | Unchanged |
| — | a | a | Remove |
| — | afwezig | a | Remove (alleen uit het manifest) |
| — | b | a | Conflict |

- [ ] **Stap 2: Draai** `go test ./internal/reconcile/` — verwacht: FAIL.
- [ ] **Stap 3: Implementeer `Plan`** als die tabel: "lokaal ongewijzigd" is `Present && Sum(C) == M.SHA256`.
- [ ] **Stap 4: Draai** de test — verwacht: PASS.
- [ ] **Stap 5: Commit** `feat(init): beslissing per eenheid tegen het manifest`

### Taak 12: `lighthouse.toml` aanvullen

**Files:**
- Create: `internal/projectconfig/projectconfig.go`, `internal/projectconfig/projectconfig_test.go`

**Interfaces:**
- Produces:
  ```go
  type KV struct{ Key, Value string } // Value is een TOML-literal, bijvoorbeeld `"en"`
  type Table struct{ Name string; Keys []KV }
  func AppendMissing(existing []byte, want []Table) (out []byte, added []string, manual []string, err error)
  func ForgeRepo(remoteURL string) (string, bool)
  func CoreTables(repo string, haveRepo bool) []Table
  ```
  `added` en `manual` zijn sleutels als `forge.repo`; `manual` = ontbreekt, maar niet veilig in te voegen.

- [ ] **Stap 1: Schrijf de falende tests**:
  - `TestAppendToEmpty`: `nil` + core-tabellen → `"[project]\nlanguage = \"en\"\n\n[forge]\nkind = \"github\"\nrepo = \"acme/w\"\n"`.
  - `TestAppendKeepsEverything`: paia-vormige invoer met commentaar, `[forge]` zonder `kind`, en een `[controller]`-tabel erna → de invoer staat er byte voor byte nog, `kind = "github"` staat direct onder de laatste sleutelregel van `[forge]` (vóór `[forge.board]`), `[project]` staat achteraan.
  - `TestAppendNothingMissing`: `out` is gelijk aan de invoer, `added` is leeg.
  - `TestManual`: `forge = { repo = "a/b" }` en `forge.repo = "a/b"` bovenin → `kind` komt in `manual`, de invoer verandert niet.
  - `TestInvalidTOML`: fout, geen uitvoer.
  - `TestOutputIsValid`: elke `out` decodeert met `toml.Decode` en heeft alle `added`-sleutels.
  - `TestForgeRepo`: `https://github.com/acme/w.git`, `https://github.com/acme/w`, `git@github.com:acme/w.git`, `ssh://git@github.com/acme/w` → `acme/w`; `https://gitlab.com/a/b`, `""`, `https://github.com/a` → `false`.
  - `TestCoreTablesWithoutRepo`: alleen `[project]`.
- [ ] **Stap 2: Draai** `go test ./internal/projectconfig/` — verwacht: FAIL.
- [ ] **Stap 3: Implementeer.** Welke sleutels er zijn, komt uit `toml.Decode` (`MetaData.IsDefined`). Een tabel geldt als expliciet als er een regel `[naam]` staat; zijn sectie loopt tot de volgende regel die met `[` begint. Een ontbrekende tabel komt achteraan. Een ontbrekende sleutel komt na de laatste niet-lege, niet-commentaarregel van de sectie. Een sleutel in een impliciete of inline tabel gaat naar `manual`. Controleer `out` met `toml.Decode`.
- [ ] **Stap 4: Draai** de test — verwacht: PASS.
- [ ] **Stap 5: Commit** `feat(init): lighthouse.toml aanvullen door tekst in te voegen`

### Taak 13: Afhankelijkheden

**Files:**
- Create: `internal/deps/deps.go`, `internal/deps/deps_test.go`

**Interfaces:**
- Consumes: `packs.Requirement`.
- Produces: `type Missing struct{ Tool string; NeededBy []string }`; `type Checker struct{ LookPath func(string) (string, error); Run func(name string, args ...string) error }`; `func Default() Checker` (`exec.LookPath`, `exec.Command(...).Run`); `func (c Checker) Check(reqs []packs.Requirement) []Missing` (volgorde van `reqs`).

- [ ] **Stap 1: Schrijf de falende tests**: `TestMissingTool` (een stub-`LookPath` zonder `jq` → `[{jq [main-commit-guard]}]`); `TestTomllibRunsPython` (`tomllib` roept `Run("python3", "-c", "import tomllib")` aan; een fout → `Missing`); `TestTomllibWithoutPython` (geen `python3` → `tomllib` ontbreekt ook, zonder `Run`); `TestAllPresent` → leeg.
- [ ] **Stap 2: Draai** `go test ./internal/deps/` — verwacht: FAIL.
- [ ] **Stap 3: Implementeer**: `tomllib` is de enige bijzondere naam; elke andere tool via `LookPath`.
- [ ] **Stap 4: Draai** de test — verwacht: PASS.
- [ ] **Stap 5: Commit** `feat(init): afhankelijkheden per pakket melden`

### Taak 14: `lighthouse init` en `--upgrade`

**Files:**
- Create: `internal/initcmd/initcmd.go`, `internal/initcmd/workspace.go`, `internal/initcmd/initcmd_test.go`, `internal/cli/init.go`
- Modify: `internal/cli/root.go`

**Interfaces:**
- Consumes: alles uit taak 5 t/m 13; `pack.Files`; `version.String()`.
- Produces:
  ```go
  type Options struct {
  	Dir                  string // cwd; de root komt uit git rev-parse --show-toplevel
  	Upgrade, Force, Yes  bool
  	Version              string
  	Packs                fs.FS  // bevat <naam>/...; standaard pack.Files
  	Deps                 deps.Checker
  	In                   io.Reader
  	Out, Err             io.Writer
  }
  func Run(o Options) int // 0 = klaar of geen wijzigingen; 1 = geweigerd, afgewezen of fout
  ```
  Een `Deps` met nil-velden valt terug op `deps.Default()`; `Packs` nil valt terug op `pack.Files`.
  CLI: `lighthouse init [--upgrade] [--force] [--yes]`.

- [ ] **Stap 1: Schrijf de falende tests**, elk in een verse `git init -b main` in `t.TempDir()`, met `Packs` = een `fstest.MapFS` met een kleine `core` (een `#!`-script, een lib-bestand, `hooks.json`, `instructions/AGENTS.md`, `requires.toml`), en één test tegen het echte `pack.Files`:
  - `TestInitFresh` (`Yes`): het script staat er met marker en modus `0755`; `settings.json` heeft de registratie; `AGENTS.md` heeft het blok; `CLAUDE.md` is `"@AGENTS.md\n"`; `lighthouse.toml` heeft `[project]`; het manifest noemt elke eenheid met de juiste `sha256`; rc 0.
  - `TestInitAsksFirst`: `In = "n\n"` → rc 1 en geen enkel nieuw bestand; `In = ""` (EOF) → idem; `In = "y\n"` → geschreven. De uitvoer bevat `--- /dev/null` en `+++ b/.claude/hooks/lighthouse/` vóór de vraag `Apply these changes? [y/N] `.
  - `TestInitTwiceRefuses`: tweede `init` → rc 1, de melding noemt `--upgrade`.
  - `TestUpgradeWithoutManifest`: rc 1, de melding noemt `lighthouse init`.
  - `TestUpgradeRightAfterInitIsEmpty`: `--upgrade` → `"No changes.\n"` op `Out`, niets van `In` gelezen, elk bestand byte voor byte gelijk, rc 0.
  - `TestUpgradeRefusesLocalEdit`: een regel toegevoegd aan het script → rc 1, de diff toont de regel, het script is ongewijzigd; met `Force` → het script is terug zoals het pakket hem levert. Idem voor een gewijzigde regel binnen het blok, een gewijzigde `timeout` in de registratie en een verwijderd script.
  - `TestUpgradeFromDevToTag`: `init` met `Version "dev"`, `--upgrade` met `"v0.1.0"` → geen conflict, marker `core@v0.1.0`, manifest `version = "v0.1.0"`.
  - `TestUpgradeRemovesDroppedUnit`: een pakket zonder het lib-bestand → het bestand en zijn entry verdwijnen.
  - `TestKeepsProjectContent`: een bestaande `settings.json` met projecthook in dezelfde groep, `AGENTS.md` met eigen tekst en een `lighthouse.toml` met commentaar en `[controller]` → alles buiten de lighthouse-delen byte voor byte gelijk; `CLAUDE.md` wordt niet aangemaakt als hij al bestaat, en zonder `@AGENTS.md` meldt `init` dat op `Err`.
  - `TestFromSubdirectory`: `Dir` = `<repo>/a/b` → installeert in `<repo>`.
  - `TestNotARepo`: rc 1, melding `not a git repository`.
  - `TestCorruptManifest` en `TestUnparseableSettings`: rc 1, niets geschreven.
  - `TestWarnsMissingScript`: een projectregistratie met `"$CLAUDE_PROJECT_DIR"/.claude/hooks/weg.sh` → `Err` noemt dat pad; rc 0.
  - `TestReportsMissingDeps`: een `Deps` zonder `jq` → `Err` bevat `missing dependency: jq (needed by: main-commit-guard)`; de installatie gaat door.
  - `TestNoForgeWithoutGitHubRemote`: geen `origin` → `lighthouse.toml` zonder `[forge]`, en een melding op `Err`.
  - `TestInitInstallsRealCore` (`pack.Files`): rc 0, en `.lighthouse/lib/lighthouse_lib/config.py` bestaat.
- [ ] **Stap 2: Draai** `go test ./internal/initcmd/` — verwacht: FAIL.
- [ ] **Stap 3: Implementeer `workspace.go`**: `current(root string, desired []packs.Unit, m manifest.Manifest) (map[packs.Key]reconcile.Current, error)` voor alle sleutels uit `desired` en `m`. Een bestand: de bytes op schijf. Een blok: `agentsmd.Find` op `AGENTS.md`. Een hook: zoek in `settings.Registrations` de lighthouse-registratie met dezelfde `HookID` en neem `orderedjson.Canonical(Entry)`.
- [ ] **Stap 4: Implementeer `Run`**, in deze volgorde:
  1. root bepalen; manifest lezen; de modusfouten van ontwerpkeuze 5;
  2. pakketten: `["core"]` bij `init`, anders `m.Packages`; `packs.Load` per pakket; `deps.Check` en de meldingen;
  3. `reconcile.Plan`; zijn er `Conflict`s en geen `Force`, druk dan per conflict een diff af (huidig → pakket), gevolgd door `refused: <n> locally modified unit(s); rerun with --force to overwrite`, en eindig met rc 1. Met `Force` geldt een `Conflict` als `Replace`, of als `Remove` als `Desired` nil is;
  4. nieuwe inhoud per pad: bestanden uit `Desired` (`Remove` → verwijderen); `AGENTS.md` via `agentsmd.Set`/`Remove`; `settings.json` via `settings.Apply` met alle gewenste registraties, dan `orderedjson.Marshal`; `CLAUDE.md` alleen als hij en `AGENTS.md` ontbreken; `lighthouse.toml` via `projectconfig.AppendMissing` (`manual` → melding); het manifest via `manifest.Encode`, met per eenheid de hash van `Desired.Content` (bij `Unchanged` en `Adopt` gelijk aan de huidige);
  5. diff per pad (`go-difflib`, `--- a/<pad>` / `+++ b/<pad>`, `/dev/null` voor nieuw of weg); geen verschil → `No changes.` en rc 0;
  6. de vraag, tenzij `Yes`; alleen `y` of `yes` (hoofdletterongevoelig) is akkoord;
  7. schrijven: mappen aanmaken, bestanden met hun modus, verwijderen; daarna de check op registraties die naar een ontbrekend script wijzen (eerste woord van een commando dat met `"$CLAUDE_PROJECT_DIR"/` of `$CLAUDE_PROJECT_DIR/` begint).
  `go get github.com/pmezard/go-difflib@v1.0.0`. Hang `init` met de drie flags onder de root.
- [ ] **Stap 5: Draai** `go test ./... && golangci-lint run` — verwacht: PASS.
- [ ] **Stap 6: Commit** `feat(init): lighthouse init en --upgrade met diff, akkoord en manifest`

### Taak 15: e2e-test in de CI

**Files:**
- Create: `e2e/e2e_test.go`
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: de binary uit `cmd/lighthouse`; de geïnstalleerde guard.

- [ ] **Stap 1: Schrijf de test** (`//go:build e2e`):

```go
func TestFreshRepoBlocksCommitOnMain(t *testing.T) {
	bin := buildLighthouse(t)              // go build -o <tmp>/lighthouse ./cmd/lighthouse
	repo := freshRepo(t)                   // git init -b main, één commit, geen remote
	run(t, repo, bin, "init", "--yes")

	cmd := guardCommand(t, repo)           // uit .claude/settings.json: PreToolUse → matcher Bash → command met "main-commit-guard"
	code, stderr := runHook(t, repo, cmd, "git commit -m x") // bash -c <cmd>, CLAUDE_PROJECT_DIR=repo, cwd=repo, payload op stdin, zonder LIGHTHOUSE_REPO_ROOT
	if code != 2 || !strings.Contains(stderr, "geblokkeerd") || strings.Contains(stderr, "KON NIET CONTROLEREN") {
		t.Fatalf("main: rc %d\n%s", code, stderr)
	}

	git(t, repo, "checkout", "-q", "-b", "issue-1-x")
	if code, stderr := runHook(t, repo, cmd, "git commit -m x"); code != 0 {
		t.Fatalf("featurebranch: rc %d\n%s", code, stderr)
	}
	git(t, repo, "checkout", "-q", "main")

	before := snapshot(t, repo)            // pad → sha256 van elk bestand buiten .git
	out := run(t, repo, bin, "init", "--upgrade")
	if out != "No changes.\n" || !maps.Equal(before, snapshot(t, repo)) {
		t.Fatalf("upgrade niet leeg:\n%s", out)
	}
}
```

- [ ] **Stap 2: Draai** `go test -tags e2e ./e2e/ -v` — verwacht: PASS. Controleer dat de test faalt om de goede reden: zet tijdelijk de modus van het script in `packs.Load` op `0o644`, draai opnieuw (verwacht: FAIL op de main-assertie), en zet hem terug.
- [ ] **Stap 3: CI.** Stap `go test -tags e2e ./e2e/ -v` in de job `go`, na `go test ./...`.
- [ ] **Stap 4: Draai de hele set**: `go test ./... && go test -tags e2e ./e2e/ && uv run pytest -q && ci/literal-grep.sh` — verwacht: alles groen.
- [ ] **Stap 5: Commit** `test(e2e): verse repo, init --yes, guard blokkeert main, upgrade leeg`

---

## Na de laatste taak

- Zet in issue #4, onder de kop *Verbeterpunten*, de punten uit ontwerpkeuze 1 als bewuste verschuiving naar (b): `host.toml` voorstellen, board-ID's meten, `skills/` afbeelden, workflows invullen uit `[ci]`, de Superpowers-check.
- Dezelfde configtests tegen `identity.py` en de configlezer draaien (spec, *Configlezer*) kan pas als beide in paia staan: dat hoort bij (d). Taak 3 neemt de gevallen over, zodat (d) ze kan hergebruiken.
- PR naar `main` met `Closes #4`; de CI moet op alle drie de jobs groen zijn.
