# Startgids in het Engels, van installatie tot eerste PR — implementatieplan

> **Voor agents:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkboxen (`- [ ]`).

**Doel:** Een Engelse gids `docs/getting-started.md` waarmee een developer die niet Corneel is,
zonder hulp van installatie tot eerste PR komt, met tests die falen als de gids en de repo uit
elkaar lopen.

**Architectuur:** Eén markdownbestand met vaste kopjes, zodat de tests secties kunnen vinden.
Eén testbestand in `tests/` leest de gids en de bronnen (`pack/*/requires.toml`, `go.mod`,
`internal/cli/init.go`, `internal/initcmd/initcmd.go`, `pack/core/lib/config.py`) en vergelijkt.
De README linkt naar de gids. Er is geen code buiten de test.

**Tech stack:** Markdown, pytest (`tomllib`, `re`, `subprocess`).

**Spec:** [2026-10-27-kaarten-design.md](../specs/2026-10-27-kaarten-design.md), §5 (de
tekstgids) en test 1 in §7. Issue #87. De kaarten (#99–#104) vallen buiten dit plan.

## Globale randvoorwaarden

- De gids is Engels. Plan en commits zijn Nederlands. Testcode, namen en commentaar zijn Engels (`code_language_paths` bevat `tests/**`).
- De vaste kopjes van de gids, letterlijk: `## Requirements`, `## Install`, `## Run lighthouse init`, `## Configure lighthouse.toml` en `## Your first issue`. De tests zoeken ze op.
- De commando's staan als tekst om te kopiëren, in codeblokken.
- Links in `.md` zijn relatief en bestaan (`worktree-linkcheck`). Een link naar een kaart komt pas als die kaart bestaat. In dit plan komt er dus geen.
- Verzin geen gedrag. Haal elke bewering over een tool, hook of flag uit de bron die bij de taak staat.
- Er is geen `init --upgrade` nodig, want dit raakt `pack/` en `internal/` niet.
- Testgate: `uv run pytest -q && go test -race -shuffle=on ./...`.

## Review-focus

1. **Een tool die alleen in de lopende tekst staat**, niet in de tabel. De test kijkt alleen naar de eerste kolom van de tabel onder `## Requirements`. Test in taak 1: `test_requirements_table_names_every_tool`.
2. **Een toolnaam die in een andere zit**, zoals `tar` in `start` of `go` in `golangci-lint`. De test matcht codespans exact (`` `tar` ``), geen substrings. Test in taak 1: `test_requirements_match_code_spans_exactly`.
3. **Een voorbeeld-`lighthouse.toml` die geldige TOML is maar ongeldige config.** Denk aan `language = "english"` of een `[forge.board]` zonder status-opties. De test laadt het voorbeeld met `config.py`, niet met `tomllib`. Test in taak 2: `test_config_example_loads`.
4. **Installeren uit een private repo.** `go install` faalt zonder `GOPRIVATE` zolang #93 niet beslist is. De test eist het modulepad uit `go.mod` en `GOPRIVATE` in de installregel. Test in taak 2: `test_install_uses_module_path`.
5. **`init` zonder terminal (CI, een agent).** Lege stdin weigert en geeft exit 1. De gids noemt `--yes`, en elke genoemde flag bestaat. Test in taak 2: `test_init_flags_exist`.

---

### Taak 1: Vereistentabel met test

**Bestanden:**
- Create: `docs/getting-started.md` (intro plus `## Requirements`)
- Create: `tests/test_getting_started.py`

**Interfaces:**
- Produceert: `GUIDE = ROOT / "docs/getting-started.md"`, `ROOT = Path(__file__).resolve().parents[1]` (patroon van `tests/test_instruction_files.py`) en `section(title: str) -> str`. Die laatste geeft de tekst tussen `## <title>` en het volgende `## `-kopje, en faalt met een assert als het kopje ontbreekt. Taak 2 en 3 gebruiken ze.
- Produceert: `table_rows(text: str) -> list[list[str]]`: de cellen van elke markdown-tabelrij, zonder de kopregel en de scheidingsregel.

- [ ] **Stap 1: Schrijf de falende tests**

```python
def required_tools() -> set[str]:
    tools = set()
    for path in sorted((ROOT / "pack").glob("*/requires.toml")):
        tools |= set(tomllib.loads(path.read_text(encoding="utf-8")))
    return tools

def first_cells() -> list[str]:
    return [row[0] for row in table_rows(section("Requirements"))]

def test_requirements_table_names_every_tool():
    named = {span for cell in first_cells() for span in re.findall(r"`([^`]+)`", cell)}
    missing = sorted(required_tools() - named)
    assert not missing, f"docs/getting-started.md Requirements lacks {missing}"

def test_requirements_match_code_spans_exactly():
    named = {span for cell in first_cells() for span in re.findall(r"`([^`]+)`", cell)}
    unknown = sorted(named - required_tools())
    assert not unknown, f"Requirements names tools no requires.toml has: {unknown}"

def test_requirements_name_claude_code():
    assert any("Claude Code" in cell for cell in first_cells())

def test_requirements_say_what_falls_away():
    rows = table_rows(section("Requirements"))
    empty = [row[0] for row in rows if not row[-1].strip()]
    assert not empty, f"no 'Without it' text for {empty}"
```

- [ ] **Stap 2: Draai ze en zie ze falen**

Run: `uv run pytest tests/test_getting_started.py -q`
Verwacht: FAIL, want `docs/getting-started.md` bestaat niet.

- [ ] **Stap 3: Schrijf de intro en `## Requirements`**

De intro bestaat uit drie zinnen:
- wat lighthouse is;
- voor wie het is: een developer met een GitHub-repo en Claude Code;
- wat er zonder controller al werkt: worktrees, een test gate voor elke commit, PR-bodies met bewijs en adversarial reviews.

De tabel heeft de kolommen `Requirement | Package | Needed for | Without it`.

Zo ziet de eerste kolom eruit:
- Claude Code (zonder codespan);
- Python 3.11+ (`` `python3` ``, `` `tomllib` ``);
- `` `superpowers` `` (Claude Code plugin);
- elke andere sleutel als codespan.

Er komt één rij per tool uit alle vijf `requires.toml`. De pakketkolom noemt de pakketten die de tool vragen. `codex` staat erin als optional, voor de adversarial reviews en `lighthouse usage codex`.

De laatste kolom leidt wat wegvalt af uit de units die `requires.toml` per tool noemt. Ga na in de hook of skill wat er dan gebeurt. Zegt een hook `COULD NOT CHECK`, schrijf dat dan. Kopieer geen unitlijsten.

Zet onder de tabel één zin: `init` meldt een ontbrekende tool als `missing dependency: <tool> (needed by: …)` en gaat door (`internal/initcmd/initcmd.go:166`).

- [ ] **Stap 4: Draai de tests en zie ze slagen**

Run: `uv run pytest tests/test_getting_started.py -q`
Verwacht: 4 passed.

- [ ] **Stap 5: Mutatiecheck**

Voeg tijdelijk `zstd = ["x"]` toe aan `pack/shell/requires.toml`. Draai dan
`uv run pytest tests/test_getting_started.py -q`. Verwacht: `test_requirements_table_names_every_tool`
faalt met `['zstd']`. Draai de wijziging terug (`git checkout pack/shell/requires.toml`).

- [ ] **Stap 6: Commit**

```bash
git add docs/getting-started.md tests/test_getting_started.py
git commit -m "docs(guide): Engelse startgids met vereistentabel en test (#87)"
```

### Taak 2: Installeren, `init` en `lighthouse.toml`

**Bestanden:**
- Modify: `docs/getting-started.md` (kopjes `## Install`, `## Run lighthouse init`, `## Configure lighthouse.toml`)
- Modify: `tests/test_getting_started.py`

**Interfaces:**
- Gebruikt: `ROOT`, `GUIDE` en `section()` uit taak 1.

- [ ] **Stap 1: Schrijf de falende tests**

```python
def test_install_uses_module_path():
    module = (ROOT / "go.mod").read_text(encoding="utf-8").splitlines()[0].split()[1]
    text = section("Install")
    assert f"go install {module}/cmd/lighthouse@" in text
    assert "GOPRIVATE=" in text

def test_init_prompt_matches_the_binary():
    src = (ROOT / "internal/initcmd/initcmd.go").read_text(encoding="utf-8")
    prompt = re.search(r'ask\(o\.Out, in, "([^"]+)"\)', src).group(1).strip()
    assert prompt in section("Run lighthouse init")

def test_init_flags_exist():
    src = (ROOT / "internal/cli/init.go").read_text(encoding="utf-8")
    known = set(re.findall(r'Var\(&o\.\w+, "([\w-]+)"', src))
    used = set(re.findall(r"lighthouse init[^\n`]*?--([\w-]+)", GUIDE.read_text(encoding="utf-8")))
    assert {"yes", "upgrade"} <= used
    assert used <= known, f"guide names unknown init flags {sorted(used - known)}"

def test_config_example_loads(tmp_path):
    block = re.search(r"```toml\n(.*?)```", section("Configure lighthouse.toml"), re.S).group(1)
    (tmp_path / "lighthouse.toml").write_text(block, encoding="utf-8")
    env = {**os.environ, "LIGHTHOUSE_REPO_ROOT": str(tmp_path)}
    for key, want in [("project.language", "en"), ("forge.kind", "github")]:
        out = subprocess.run(["python3", str(ROOT / "pack/core/lib/config.py"), key],
                             env=env, capture_output=True, text=True, check=False)
        assert out.returncode == 0, out.stderr
        assert out.stdout.strip() == want
```

Controleer vóór stap 3 dat de regex in `test_init_flags_exist` alle flags uit
`internal/cli/init.go` vindt: `upgrade`, `force`, `yes`, `pack` en `no-pack`. Pas anders de regex
aan, niet de verwachting.

- [ ] **Stap 2: Draai ze en zie ze falen**

Run: `uv run pytest tests/test_getting_started.py -q`
Verwacht: de vier nieuwe tests falen, want de kopjes ontbreken (assert uit `section()`).

- [ ] **Stap 3: Schrijf de drie secties**

- **`## Install`**:
  - Eerst de download: release-binaries komen met de website (#89). Noem nog geen URL.
  - Dan het commando `GOPRIVATE=github.com/CorneeldH go install github.com/CorneeldH/lighthouse/cmd/lighthouse@<tag>`, zolang de repo private is (#93). Neem de uitleg over git-auth over uit `docs/install.md`.
  - Installeer een tag. Een build van `main` meldt `dev` en schrijft dat in `.lighthouse/manifest.toml`.
- **`## Run lighthouse init`**:
  - `cd` naar je eigen repo en draai `lighthouse init`.
  - Het toont per bestand een diff en vraagt `Apply these changes? [y/N]`.
  - `--yes` is er voor CI of een agent. Lege stdin weigert.
  - `--upgrade` werkt een bestaande installatie bij, `--pack` en `--no-pack` kiezen pakketten.
  - Een tweede `init` zonder `--upgrade` weigert.
  - Daarna een korte lijst van wat `init` schrijft en waar. De bron is de layout-comment in `internal/packs/packs.go:4-27`, plus `.lighthouse/manifest.toml`. Het managed block in `AGENTS.md` komt er alleen met jouw akkoord, op de diff.
- **`## Configure lighthouse.toml`**:
  - Eén `toml`-codeblok met `[project] language = "en"`, `[forge] kind = "github"`, `repo = "your-name/your-repo"` en `[test_gate] command = "<your test command>"`. Gebruik geen `<owner>/<repo>`, want `forge.repo` moet de vorm owner/name hebben (`_REPO` in `config.py`), en de test laadt het blok.
  - Een tabel met deze sleutels en wat ze doen. De bron is `SCHEMA` en `DEFAULTS` in `pack/core/lib/config.py`.
  - Een lege `command` zet de gate uit.
  - `language` is `nl` of `en`.
  - `[forge.board]` staat erbij als optioneel, met de verplichte velden (`project_number`, `project_id`, `status_field_id`, en `status_options` met `To do`, `In progress`, `Done` en `On hold`). Hij staat niet in het voorbeeldblok.
  - Het pakket python zet zelf `uv run pytest -q` als test gate.

- [ ] **Stap 4: Draai de tests en zie ze slagen**

Run: `uv run pytest tests/test_getting_started.py -q`
Verwacht: 8 passed.

- [ ] **Stap 5: Mutatiecheck**

Zet tijdelijk `language = "english"` in het codeblok van de gids. Verwacht:
`test_config_example_loads` faalt met de `ConfigError` op stderr. Draai het terug.

- [ ] **Stap 6: Commit**

```bash
git add docs/getting-started.md tests/test_getting_started.py
git commit -m "docs(guide): installeren, init en lighthouse.toml in de startgids (#87)"
```

### Taak 3: Het eerste issue en de README

**Bestanden:**
- Modify: `docs/getting-started.md` (kopje `## Your first issue`)
- Modify: `README.md` (status-alinea, link naar de gids)
- Modify: `tests/test_getting_started.py`

**Interfaces:**
- Gebruikt: `ROOT` en `section()` uit taak 1.

- [ ] **Stap 1: Schrijf de falende tests**

```python
CHAIN = ["/lighthouse-write-issue", "/lighthouse-start-issue", "/lighthouse-verify-pr"]

def test_first_issue_walks_the_chain_in_order():
    text = section("Your first issue")
    positions = [text.find(cmd) for cmd in CHAIN]
    assert -1 not in positions and positions == sorted(positions)

def test_first_issue_names_only_existing_skills():
    named = set(re.findall(r"/lighthouse-([\w-]+)", section("Your first issue")))
    skills = {p.name for p in (ROOT / "pack").glob("*/skills/*")}
    assert named <= skills, f"unknown skills {sorted(named - skills)}"

def test_readme_links_the_guide():
    assert "](docs/getting-started.md)" in (ROOT / "README.md").read_text(encoding="utf-8")
```

- [ ] **Stap 2: Draai ze en zie ze falen**

Run: `uv run pytest tests/test_getting_started.py -q`
Verwacht: de drie nieuwe tests falen.

- [ ] **Stap 3: Schrijf `## Your first issue` en pas de README aan**

**`## Your first issue`**: genummerde stappen. Bij elke stap staat wat je typt, wat je ziet, en welke hook kan ingrijpen:
1. `/lighthouse-write-issue`, het issue met acceptatiecriteria.
2. `/lighthouse-start-issue <nr>`:
   - de worktree;
   - de routeregel `#<nr> Gate <n><a|b|c>: …`;
   - bij 4b of 4c eerst een spec of plan, en een handoff.
3. Bouwen in de worktree:
   - `pre-commit-tests` draait je `[test_gate] command` voor elke commit;
   - `main-commit-guard` blokkeert een commit op `main`.
4. `/lighthouse-verify-pr`: de PR-body met bewijs. `pr-create-gate` blokkeert `gh pr create` zonder `Closes #N` met bewijs, `Refs #N — …` of `No issue: …`.
5. Na de merge: `/lighthouse-close-issue <nr>`.

Haal de hookbeschrijvingen uit `CLAUDE.md` *Hooks*, en het gate-woord uit `.lighthouse/headings.toml` (sleutel `gate_line`, variant `en`). Sluit af met één zin: de hooks draaien alleen in Claude Code. In Codex en OpenCode draai je de test gate zelf (`AGENTS.md`, *Outside Claude Code*).

**README**, de status-alinea:
- zeg wat zonder controller al bruikbaar is: de pakketten via `lighthouse init`, met worktrees, de test gate, PR-bodies met bewijs en adversarial reviews;
- voeg de link `[Getting started](docs/getting-started.md)` toe;
- laat `lighthouse serve` staan als vroege ontwikkeling;
- laat de rest van de README ongemoeid.

- [ ] **Stap 4: Draai de tests en zie ze slagen**

Run: `uv run pytest tests/test_getting_started.py -q`
Verwacht: 11 passed.

- [ ] **Stap 5: Mutatiecheck**

Verander tijdelijk `/lighthouse-verify-pr` in de gids in `/lighthouse-verify-prs`. Verwacht: beide
ketentests falen. Draai het terug.

- [ ] **Stap 6: Volledige testgate en commit**

Run: `uv run pytest -q && go test -race -shuffle=on ./...`
Verwacht: alles groen.

```bash
git add docs/getting-started.md README.md tests/test_getting_started.py
git commit -m "docs(guide): eerste issue in de startgids en link vanuit de README (#87)"
```
