# M0 (a0) Bootstrap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Lighthouse krijgt met de hand de bootstrap waarmee je veilig aan de repo zelf werkt:
`main-commit-guard` en `worktree-guard` met registratie, `AGENTS.md`/`CLAUDE.md` en een CI-skelet
op een GitHub-hosted runner (issue #3).

**Architecture:** De twee guards en `lib/git_invocations.py` komen als kopie uit paia naar
`.claude/hooks/`, met hun tests naar `tests/hooks/`. Hookmeldingen gaan naar het Engels;
`worktree-guard` meldt zich voortaan als hij niet kan kijken. Een Python-lane (`uv` + `pytest`) in
GitHub Actions draait die tests op elke PR. Na (b) ruimt `lighthouse init` deze bootstrap op
(besluit 13).

**Tech Stack:** bash (3.2-compatibel), Python ≥ 3.11 voor de tests (de hooklib zelf blijft
3.9-compatibel), `uv`, `pytest`, GitHub Actions.

**Spec:** [2026-10-17-m0-pakket-design.md](../specs/2026-10-17-m0-pakket-design.md), sectie
*Opdeling* (a0); besluiten 7, 13 en 14; risico *Fail-open guards*.

## Global Constraints

- Bron: `CorneeldH/personal_ai_agent` op commit `231f6e84fa709dd37c4fc8578453907a8a6efdf6`, map
  `.claude/hooks/` (`main-commit-guard.sh`, `worktree-guard.sh`, `lib/git_invocations.py`) en
  `tests/unit/` (`_hook_testkit.py`, `test_git_invocations.py`, de `test_commit_guard_*`- en
  `test_worktree_guard_*`-tests uit `test_instructie_hooks.py`).
- Elk gekopieerd bestand krijgt bovenaan (onder de shebang) één commentaarregel:
  `# Bootstrap copy of personal_ai_agent@231f6e84 <bronpad>; lighthouse init replaces it in M0 (b).`
- Besluit 7: hookmeldingen, `AGENTS.md`, `CLAUDE.md` en het commentaar in de twee shellscripts in
  het Engels. Artefacten (issues, PR's, specs, plannen, commitberichten) in het Nederlands.
- Besluit 14: gedrag blijft gelijk. Toegestaan: vertalen, het fail-open-gat van `worktree-guard`
  dichten. `lib/git_invocations.py` blijft inhoudelijk een letterlijke kopie (alleen de
  bronregel erbij); hij geeft geen meldingen aan mensen.
- Vaste Engelse tokens in de meldingen (tests leunen erop): `COULD NOT CHECK`,
  `missing tooling`, `Main-commit guard: blocked`, `Worktree guard:`.
- Geen keten-skills, geen `/paia-*`-verwijzingen als iets dat hier bestaat.
- Shell: geen bash-4-constructies (`mapfile`, `${x,,}`, associatieve arrays); macOS draait 3.2.
- Niets op `main` committen; alles op branch `issue-3-bootstrap` in
  `.claude/worktrees/issue-3-bootstrap`.

## Review Focus

1. **Echte sessie in de hoofdcheckout.** Een verse Claude Code-sessie op `main` doet `git commit`:
   de guard moet via `$CLAUDE_PROJECT_DIR` vuren. Tests simuleren dat (Task 3); de echte toets is
   stap 6 van Task 5, na de merge.
2. **Hook zonder execute-bit** (`core.fileMode=false`, kopie via een editor): de registratie roept
   het pad direct aan. Task 3 draait het geregistreerde commando, dus een ontbrekend `+x` faalt daar.
3. **`worktree-guard` zonder `jq`** moet nog steeds geldige JSON op stdout zetten, zonder `jq` om
   te escapen. Task 2 parst die uitvoer met `json.loads`.
4. **`.claude/allow-main` blijft liggen of wordt gecommit.** Hij mag niet in `.gitignore` (dan
   ziet `git status` hem niet) en niet in git. Task 3 toetst beide.
5. **Bash 3.2.** CI draait bash 5; het nieuwe preflight-blok in `worktree-guard.sh` mag alleen
   POSIX-achtige constructies gebruiken (zie Global Constraints). Reviewer leest dit na.

---

### Task 1: main-commit-guard met parser, tests en Python-lane

**Files:**
- Create: `pyproject.toml`, `uv.lock`, `.gitignore`
- Create: `tests/__init__.py`, `tests/hooks/__init__.py` (leeg)
- Create: `tests/hooks/_hook_testkit.py` (kopie van paia `tests/unit/_hook_testkit.py`)
- Create: `tests/hooks/test_git_invocations.py` (kopie)
- Create: `tests/hooks/test_main_commit_guard.py` (de `test_commit_guard_*`-tests, regels 671–1806
  van paia `test_instructie_hooks.py`, met de helpers `_run`, `_GIT_ENV`, `_git`, `_init_repo`,
  `_bash`, `_REAL_TOOLS`, `_tool_bin`)
- Create: `.claude/hooks/main-commit-guard.sh` (mode 755), `.claude/hooks/lib/git_invocations.py`

**Interfaces:**
- Produces: `tests/hooks/_hook_testkit.py` met `HOOKS` (= `<repo>/.claude/hooks`), `init_repo`,
  `load_hook_lib`, `tool_bin`, `bash_payload`, `run_hook`; de helpers in
  `test_main_commit_guard.py` die Task 2 kopieert.
- Produces: `.claude/hooks/main-commit-guard.sh` — stdin PreToolUse-JSON; exit 0 = door, exit 2 +
  stderr = blokkeren; nooit stdout.

- [ ] **Step 1: Python-lane neerzetten.** `pyproject.toml`: `name = "lighthouse"`,
  `requires-python = ">=3.11"`, `[dependency-groups] dev = ["pytest>=8"]`,
  `[tool.uv] package = false`, `[tool.pytest.ini_options] testpaths = ["tests"]`,
  `pythonpath = ["."]`. Dan `uv lock`. `.gitignore`: `.claude/worktrees/`,
  `.claude/settings.local.json`, `__pycache__/`, `.venv/`, `.pytest_cache/`. Niet
  `.claude/allow-main`.
- [ ] **Step 2: Tests kopiëren.** Imports `tests.unit._hook_testkit` → `tests.hooks._hook_testkit`.
  Vertaal de meldingtokens in de asserts: `"KON NIET CONTROLEREN"` → `"COULD NOT CHECK"`,
  `"ontbrekende tooling"` → `"missing tooling"`. De asserts op `"main"`, `"jq"`, `"python3"`,
  `"git"`, `"cat"` en `"parser"` blijven. Docstrings mogen Nederlands blijven.
- [ ] **Step 3: Draai** `uv run pytest tests/hooks -q`. Verwacht: FAIL, de hookbestanden ontbreken.
- [ ] **Step 4: Hooks kopiëren.** `git_invocations.py` letterlijk plus de bronregel.
  `main-commit-guard.sh`: bronregel, commentaar en alle `echo`-meldingen naar het Engels, met de
  tokens uit Global Constraints (de blokkeermelding begint met `Main-commit guard: blocked — `,
  de andere met `Main-commit guard: COULD NOT CHECK — `). De logica en de `<onbekend>`-marker
  blijven ongewijzigd. `chmod 755`.
- [ ] **Step 5: Draai** `uv run pytest tests/hooks -q`. Verwacht: alles PASS.
- [ ] **Step 6: Commit**

```bash
git add pyproject.toml uv.lock .gitignore tests .claude/hooks
git commit -m "feat(hooks): main-commit-guard als bootstrap uit paia (#3)"
```

### Task 2: worktree-guard, fail-open dicht

**Files:**
- Create: `.claude/hooks/worktree-guard.sh` (mode 755)
- Create: `tests/hooks/test_worktree_guard.py` (de zes `test_worktree_guard_*`-tests, regels
  216–367 van paia `test_instructie_hooks.py`, plus de nieuwe hieronder)

**Interfaces:**
- Consumes: `_run`, `_git`, `_init_repo`, `_tool_bin` uit Task 1 (gekopieerd in dit testbestand, of
  via `tests.hooks._hook_testkit`).
- Produces: `.claude/hooks/worktree-guard.sh` — stdin PreToolUse-JSON; altijd exit 0; stdout leeg
  (niets te melden) of `{"systemMessage": "..."}`.

- [ ] **Step 1: Schrijf de nieuwe tests** (naast de zes gekopieerde):

```python
def test_worktree_guard_warns_without_jq(tmp_path):
    # PATH = _tool_bin(..., drop="jq"); Write naar repo/x.py op main
    assert result.returncode == 0
    msg = json.loads(result.stdout)["systemMessage"]
    assert "COULD NOT CHECK" in msg and "jq" in msg

def test_worktree_guard_warns_without_git(tmp_path):
    # PATH = _tool_bin(..., drop="git")
    assert result.returncode == 0
    msg = json.loads(result.stdout)["systemMessage"]
    assert "COULD NOT CHECK" in msg and "git" in msg

def test_worktree_guard_warns_on_invalid_json_payload(tmp_path):
    # stdin = "not json" (rauw, niet via json.dumps)
    assert result.returncode == 0
    assert "COULD NOT CHECK" in json.loads(result.stdout)["systemMessage"]

def test_worktree_guard_warns_when_the_path_cannot_be_resolved(tmp_path):
    # PATH = _tool_bin(..., drop="python3"): geen realpath, geen python3 → canon_path faalt
    assert result.returncode == 0
    assert "COULD NOT CHECK" in json.loads(result.stdout)["systemMessage"]
```

De zes gekopieerde tests: de assert `"worktree" in ...lower()` blijft.
- [ ] **Step 2: Draai** `uv run pytest tests/hooks/test_worktree_guard.py -q`. Verwacht: FAIL.
- [ ] **Step 3: Kopieer en repareer `worktree-guard.sh`.** Bronregel, commentaar en melding naar
  het Engels; de waarschuwing begint met `Worktree guard: `. Drie wijzigingen, verder gelijk:
  1. Preflight vóór het lezen van stdin: ontbreekt `jq`, `git` of `cat`, dan
     `printf '{"systemMessage":"Worktree guard: COULD NOT CHECK — missing tooling:%s. ..."}\n'`
     met alleen de toolnamen als variabel deel, en exit 0. Geen `jq` nodig om te escapen.
  2. `jq` faalt op de payload: melding `Worktree guard: COULD NOT CHECK — the payload is not valid JSON.`
  3. `canon_path` faalt of geeft leeg: melding `Worktree guard: COULD NOT CHECK — cannot resolve '<pad>'.`
  Meldingen 2 en 3 via `jq -cn --arg m "$MSG" '{systemMessage: $m}'`. Blijven stil, want dat
  zijn echte uitkomsten: geen `file_path`, doel buiten een git-repo, linked worktree, branch ≠
  `main`. Het commentaarblok bovenaan zegt waarom deze hook nu luid faalt (anti-goal M0-spec).
- [ ] **Step 4: Draai** `uv run pytest tests/hooks -q`. Verwacht: alles PASS.
- [ ] **Step 5: Commit**

```bash
git add .claude/hooks/worktree-guard.sh tests/hooks/test_worktree_guard.py
git commit -m "feat(hooks): worktree-guard als bootstrap, meldt zich zonder jq (#3)"
```

### Task 3: registratie in `.claude/settings.json`

**Files:**
- Create: `.claude/settings.json`
- Create: `tests/hooks/test_registration.py`

**Interfaces:**
- Consumes: de twee hooks uit Task 1 en 2; `_init_repo`/`_git` (kopie of testkit).
- Produces: `settings.json` met precies:
  `PreToolUse` → `{"matcher": "Bash", hooks: [main-commit-guard, timeout 15]}` en
  `{"matcher": "Edit|Write", hooks: [worktree-guard, timeout 15]}`; command-vorm
  `"\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/<naam>.sh"`, `type: "command"`. Geen permissions,
  plugins of model.

- [ ] **Step 1: Schrijf de tests.** Helper `_registered(matcher: str) -> list[str]` leest de
  commands uit `PreToolUse` van `<repo>/.claude/settings.json`. Elk commando draait via
  `subprocess.run(["sh", "-c", cmd], input=json.dumps(payload), env={..., "CLAUDE_PROJECT_DIR": str(ROOT)})`,
  zoals Claude Code het doet.

```python
def test_registered_bash_hook_blocks_a_commit_on_main(tmp_path):
    # tmp repo op main, payload git commit -m x, commando met "main-commit-guard.sh"
    assert result.returncode == 2 and "Main-commit guard: blocked" in result.stderr

def test_registered_bash_hook_allows_a_commit_on_a_branch(tmp_path):
    assert result.returncode == 0

def test_registered_edit_hook_warns_in_main_checkout(tmp_path):
    assert "Worktree guard:" in json.loads(result.stdout)["systemMessage"]

def test_bash_pretooluse_hooks_live_in_one_block():
    assert len([b for b in pre if b["matcher"] == "Bash"]) == 1

def test_allow_main_sentinel_is_not_tracked():
    # git ls-files -- .claude/allow-main is leeg

def test_allow_main_sentinel_is_not_ignored():
    # git check-ignore -q .claude/allow-main geeft returncode 1
```

- [ ] **Step 2: Draai** `uv run pytest tests/hooks/test_registration.py -q`. Verwacht: FAIL.
- [ ] **Step 3: Schrijf `.claude/settings.json`** volgens de Interfaces hierboven.
- [ ] **Step 4: Draai** `uv run pytest tests/hooks -q`. Verwacht: alles PASS.
- [ ] **Step 5: Commit**

```bash
git add .claude/settings.json tests/hooks/test_registration.py
git commit -m "feat(hooks): registreer de guards in .claude/settings.json (#3)"
```

### Task 4: `AGENTS.md` en `CLAUDE.md`

**Files:**
- Create: `AGENTS.md`, `CLAUDE.md`
- Create: `tests/test_instruction_files.py`

**Interfaces:**
- Produces: `AGENTS.md` als enige bron van de werkafspraken; `CLAUDE.md` begint met `@AGENTS.md`.

- [ ] **Step 1: Schrijf de tests.**

```python
def test_claude_md_imports_agents_md():
    assert CLAUDE.read_text().splitlines()[0] == "@AGENTS.md"

def test_agents_md_names_the_guards_and_the_escape_hatch():
    text = AGENTS.read_text()
    for token in ("main-commit-guard", "worktree-guard", ".claude/allow-main", ".claude/worktrees/"):
        assert token in text
```

- [ ] **Step 2: Draai** `uv run pytest tests/test_instruction_files.py -q`. Verwacht: FAIL.
- [ ] **Step 3: Schrijf `AGENTS.md`** (Engels, ≤ 600 woorden), met deze regels, elk één keer:
  - one file for every harness; `CLAUDE.md` imports it and adds only Claude-specific content;
  - language: instructions in English, artefacts (issues, PRs, specs, plans, commit messages) in Dutch;
  - worktree before the first write: `git worktree add .claude/worktrees/<name> -b <branch>`,
    branches `issue-<nr>-<slug>` or `fix-<slug>`; prefix shell commands with `cd <worktree>` or
    `git -C <worktree>`;
  - nothing lands on `main` directly: PR, squash merge; `main-commit-guard` blocks commit and push
    on `main`; escape hatch `touch .claude/allow-main`, once, remove it afterwards, never commit it;
  - atomic commits, Conventional Commits (`feat(scope): …`);
  - links: relative in `.md` files, absolute paths in chat;
  - specs in `docs/developer/planning/specs/`, plans in `docs/developer/planning/plans/`;
  - before a push run `uv run pytest`; harnesses without Claude Code hooks do not run the guards;
  - bootstrap: the guards are a manual copy from `personal_ai_agent@231f6e84`; M0 (b) replaces
    them via `lighthouse init`. Chain skills (`/paia-*`, `/lighthouse-*`) do not exist here yet.
- [ ] **Step 4: Schrijf `CLAUDE.md`**: regel 1 `@AGENTS.md`, daarna een korte sectie *Claude Code
  only*: `main-commit-guard` (PreToolUse `Bash`, blocks with exit 2) en `worktree-guard`
  (PreToolUse `Edit|Write`, warns), geregistreerd in `.claude/settings.json`. Niets uit
  `AGENTS.md` herhalen.
- [ ] **Step 5: Draai** `uv run pytest -q`. Verwacht: alles PASS.
- [ ] **Step 6: Commit**

```bash
git add AGENTS.md CLAUDE.md tests/test_instruction_files.py
git commit -m "docs: AGENTS.md en CLAUDE.md met de werkafspraken (#3)"
```

### Task 5: CI-skelet en PR

**Files:**
- Create: `.github/workflows/ci.yml`

- [ ] **Step 1: Schrijf de workflow.** `name: ci`; `on: pull_request`;
  `permissions: contents: read`; `concurrency: {group: ci-${{ github.ref }}, cancel-in-progress: true}`;
  één job `python` op `runs-on: ubuntu-latest` met `actions/checkout`, `astral-sh/setup-uv`,
  `uv sync --locked`, `uv run pytest -q`. Pin beide actions op een volledige commit-SHA met de
  versie als commentaar (`@<sha> # v4.2.2`); zoek de SHA van de laatste release op met
  `gh api repos/<owner>/<repo>/releases/latest` en `gh api repos/<owner>/<repo>/git/ref/tags/<tag>`.
  `persist-credentials: false` bij checkout.
- [ ] **Step 2: Valideer lokaal:** `uv run pytest -q` groen, en
  `python3 -c 'import yaml,sys; yaml.safe_load(open(".github/workflows/ci.yml"))'` (of
  `actionlint` als hij er is) zonder fout.
- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: Python-lane op een GitHub-hosted runner (#3)"
```

- [ ] **Step 4: Basis bijwerken.** Is #2 gemerged: `git rebase --onto origin/main 58dd7b8`. Anders
  blijft de basis `docs/m0-spec`.
- [ ] **Step 5: Push en PR.** `git push -u origin issue-3-bootstrap`; PR met base `main` (of
  `docs/m0-spec` als #2 nog open is), titel `M0 (a0): bootstrap met guards, werkafspraken en
  CI-skelet`, body met `Closes #3` en de drie acceptatiecriteria. Verwacht: check `ci / python`
  draait op `ubuntu-latest` en wordt groen (`gh pr checks <nr> --watch`).
- [ ] **Step 6: Na de merge (handmatig, door Corneel):** in een nieuwe Claude Code-sessie in de
  hoofdcheckout op `main`: `git commit --allow-empty -m test`. Verwacht: geblokkeerd met
  `Main-commit guard: blocked`. Dit is AC 1 in de echte harness.
