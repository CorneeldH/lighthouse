# Naamgeving van skills, hooks en agents — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Elke skill en hook in `pack/` volgt de naamconventie van #142, een upgrade laat niets onder een oude naam achter, en `check-instructions.sh` bewaakt de hooknamen.

**Architecture:** Hernoemen is mechanisch: `git mv` plus vervangen van vaste tokens, daarna `init --upgrade` voor de geïnstalleerde kopie. De manifest-reconcile maakt van een hernoeming Remove + Add; de enige Go-wijziging is het opruimen van mappen die daardoor leeg raken. De conventie komt in `place-instruction`, de controle als check 14 in `check-instructions.sh`.

**Tech Stack:** Go (`internal/initcmd`), bash + inline Python (`check-instructions.sh`), pytest, markdown.

**Spec:** [2026-10-31-naamgeving-design.md](../specs/2026-10-31-naamgeving-design.md)

## Global Constraints

- Worktree: `/home/corneel/repositories/lighthouse/.claude/worktrees/issue-142-naamgeving`. Elk shellcommando begint met `cd <worktree>`.
- Instructies (skills, agents, hookmeldingen, codecommentaar) Engels; commits, spec, plan en changelog-fragment Nederlands (`[project] language = nl`), changelog-tekst zelf Engels (zoals de bestaande fragmenten).
- Nooit met de hand bewerken: `.claude/skills/lighthouse-*`, `.agents/skills/lighthouse-*`, `.claude/agents/lighthouse-*`, `.claude/hooks/lighthouse/`, de lighthouse-registraties in `.claude/settings.json`, `.lighthouse/` (behalve via `init --upgrade`), het managed block in `AGENTS.md`. Die volgen uit `go run ./cmd/lighthouse init --upgrade --yes`.
- Historische tekst blijft: `changelog.d/` (bestaande fragmenten), `CHANGELOG.md`, `docs/developer/planning/**`.
- Blijft ongewijzigd: `[test_gate]`, `[usage_guard]` in `lighthouse.toml`, de statusmap `~/.local/state/lighthouse/usage-guard` (`pack/core/lib/usage_guard.py:141`), modulenamen onder `pack/*/lib/` en `pack/*/scripts/` (o.a. `usage_guard.py`, `code_language.py`, `worktree_audit.py`, `gate_*.py`), het voorvoegsel `lighthouse-`, `spec-adversary`.
- Na elke taak: `go run ./cmd/lighthouse init --upgrade --yes`, daarna `go run ./cmd/lighthouse init --upgrade </dev/null` geeft precies `No changes.`; de installatiekopie gaat in dezelfde commit.
- Nooit `--no-verify`; rood van de test gate wordt een nieuwe commit.

## Review Focus

- **Een gewoon woord dat ook een oude naam is** (`testing`, `handoff`, `instrument`, `gofmt`, `shellcheck`, `ruff-format`): alleen de tokens uit de tabellen hieronder vervangen, nooit het losse woord. `internal/protocol/header.go:26` (`KindHandoff`) en `internal/runs/runs.go:23` blijven. Test: de restgrep in Taak 6 en `go test ./...`.
- **De statusmap van de usage-hook**: een vervanging van `usage-guard` die ook `"lighthouse" / "usage-guard"` raakt, reset elk venster op elke machine. Test: Taak 3 stap 1 pint het pad.
- **Een map die al leeg was of nog iets anders bevat**: het opruimen in `init` mag die niet raken. Test: Taak 1 stap 1, gevallen `keeps_sibling` en `keeps_preexisting_empty`.
- **Een project-hook in `settings.json`** (`.claude/hooks/own.sh`, `npx …`): check 14 mag er niet op falen. Test: Taak 5 stap 1, `test_hook_names_ignore_project_hooks`.
- **Een upgrade met één lokaal aangepaste oude kopie**: geweigerd, niets geschreven; met `--force` alles weg. Test: Taak 1 stap 1, `TestUpgradeRenamesUnits`.

---

### Taak 1: `init` ruimt geleegde mappen op; upgrade over hernoemde units

**Files:**
- Modify: `internal/initcmd/initcmd.go` (`apply`, ~regel 585)
- Test: `internal/initcmd/initcmd_test.go`

**Interfaces:**
- Produces: `func removeEmptyParents(root, rel string) error` in `internal/initcmd/initcmd.go`. Loopt vanaf `filepath.Dir(rel)` omhoog; verwijdert een map alleen als `os.ReadDir` nul entries geeft; stopt bij de eerste niet-lege map, bij de repo-wortel, en bij een van de installatiewortels `.claude/skills`, `.agents/skills`, `.claude/agents`, `.claude/hooks/lighthouse` (die zelf blijven). Doet niets als `rel` niet onder een van die vier wortels ligt. `apply` roept haar aan na een geslaagde `os.Remove` in de `p.delete`-tak.

- [ ] **Stap 1: Schrijf de falende tests**

`TestUpgradeRemovesEmptiedDirs` (tabel, fake packs zoals `testPacks()`, skill als `core/skills/old/SKILL.md` met `name: lighthouse-old`, hook als `core/hooks/old/helper.py` naast `core/hooks/g.sh`):
- `skill_dir`: init met skill `old`, upgrade zonder → `.claude/skills/lighthouse-old` en `.agents/skills/lighthouse-old` bestaan niet; `.claude/skills` bestaat nog.
- `hook_subdir`: init met `core/hooks/old/helper.py`, upgrade zonder → `.claude/hooks/lighthouse/old` bestaat niet; `.claude/hooks/lighthouse/g.sh` bestaat nog.
- `keeps_sibling`: vóór de upgrade `write(t, root, ".claude/skills/lighthouse-old/notes.txt", "mine\n")` → na de upgrade bestaan de map en `notes.txt`.
- `keeps_preexisting_empty`: vóór de upgrade `os.MkdirAll(".claude/skills/empty-own")` → bestaat na de upgrade.
- `outside_roots`: `TestUpgradeRemovesDroppedUnit` (bestaand) blijft groen, en `.lighthouse/lib/lighthouse_lib` blijft bestaan als `a.py` het laatste bestand was.

`TestUpgradeRenamesUnits` (AC4): init met skill `old` en hook `old-guard.sh` (geregistreerd in een `hooks.json`), daarna packs met skill `new` en hook `new-gate.sh`:
- onaangeraakt: upgrade rc 0; geen pad onder `.claude/`, `.agents/` of in `.lighthouse/manifest.toml` bevat `old`; `.claude/settings.json` bevat `old-guard.sh` niet en `new-gate.sh` wel.
- aangepaste oude kopie (regel toegevoegd aan `.claude/hooks/lighthouse/old-guard.sh`): rc 1, uitvoer bevat `--force`, `sameSnapshot` vóór en na (niets geschreven), `new-gate.sh` bestaat niet.
- dezelfde met `--force`: rc 0, `old-guard.sh` en zijn registratie weg, `new-gate.sh` aanwezig.

- [ ] **Stap 2: Draai ze, verwacht FAIL**

Run: `cd <worktree> && go test ./internal/initcmd -run 'TestUpgradeRemovesEmptiedDirs|TestUpgradeRenamesUnits' -race`
Expected: FAIL op `skill_dir` en `hook_subdir` (map bestaat nog). `TestUpgradeRenamesUnits` kan al groen zijn op de registratie; faalt alleen nog op de lege map. Groen zonder implementatie op de andere gevallen is goed: die pinnen bestaand gedrag.

- [ ] **Stap 3: Implementeer `removeEmptyParents` en roep haar aan in `apply`**

- [ ] **Stap 4: Draai de package, verwacht PASS**

Run: `cd <worktree> && go test ./internal/... -race`
Expected: PASS.

- [ ] **Stap 5: Commit**

```bash
cd <worktree> && git add internal/initcmd && git commit -m "feat(init): geleegde mappen opruimen bij upgrade (#142)"
```

### Taak 2: Skills hernoemen

**Files:** `pack/**/skills/*`, alle verwijzingen (zie tabel), testbestanden die naar een skill heten, lighthouse's eigen `AGENTS.md` (Rules), `CLAUDE.md`, `docs/`, `docs/site/data/*.json`, `tests/fixtures/site/map.json`, Go-tests.

**Interfaces:**
- Produces: de nieuwe skillnamen uit de tabel; Taak 4 verwijst naar `place-instruction`.

| Oud (dir) | Nieuw | Tokens om te vervangen | Testbestand |
|---|---|---|---|
| `core/skills/verify-pr` | `write-pr` | `lighthouse-verify-pr`, `skills/verify-pr` | `test_verify_pr_skill.py` → `test_write_pr_skill.py` |
| `core/skills/handoff` | `write-handoff` | `lighthouse-handoff`, `skills/handoff` | — |
| `core/skills/instrument` | `place-instruction` | `lighthouse-instrument`, `skills/instrument` | `test_instrument_skill.py` → `test_place_instruction_skill.py` |
| `core/skills/spec-adversarial-review` | `review-spec` | `lighthouse-spec-adversarial-review`, `skills/spec-adversarial-review` | — (`test_adversarial_review_*.py` blijven) |
| `core/skills/plan-adversarial-review` | `review-plan` | `lighthouse-plan-adversarial-review`, `skills/plan-adversarial-review` | — |
| `core/skills/worktree-audit` | `audit-worktrees` | `lighthouse-worktree-audit`, `skills/worktree-audit` | `test_worktree_audit_skill.py` → `test_audit_worktrees_skill.py` (`test_worktree_audit.py` test het script, blijft) |
| `core/skills/testing` | `test-standard` | `lighthouse-testing`, `skills/testing` | `test_testing_skill.py` → `test_test_standard_skill.py` |
| `go/skills/go-writing` | `go-standard` | `lighthouse-go-writing`, `skills/go-writing` | — |
| `python/skills/python-writing` | `python-standard` | `lighthouse-python-writing`, `skills/python-writing` | — |
| `shell/skills/shell-gates` | `shell-standard` | `lighthouse-shell-gates`, `skills/shell-gates` | — |

Prose die een skill bij de korte naam noemt (bijv. "the testing skill", kopjes als `# Writing Go in this repository`) pas je met de hand aan waar de naam bedoeld is; het losse woord blijft.

- [ ] **Stap 1: `git mv` per regel, en het `name:`-veld** in elke `SKILL.md` (gedeeld, of `claude/` en `agents/`) naar `lighthouse-<nieuw>`.
- [ ] **Stap 2: Vervang de tokens.** Zoek per regel met `cd <worktree> && git grep -nF '<token>' -- . ':!changelog.d' ':!CHANGELOG.md' ':!docs/developer/planning' ':!.claude/skills' ':!.agents' ':!.claude/agents' ':!.claude/hooks/lighthouse' ':!.claude/settings.json' ':!.lighthouse'` en vervang elke treffer. In de root-`AGENTS.md` alleen buiten het managed block (de secties vanaf `## Lighthouse: core` schrijft `init`): een handmatige wijziging daarin is een Conflict en laat de upgrade weigeren. Hernoem de testbestanden uit de tabel met `git mv`.
- [ ] **Stap 3: Dogfood.** `cd <worktree> && go run ./cmd/lighthouse init --upgrade --yes`, daarna `go run ./cmd/lighthouse init --upgrade </dev/null` → `No changes.`
- [ ] **Stap 4: Restgrep.** Zelfde `git grep`-pathspecs als stap 2 maar zonder de vier geïnstalleerde uitsluitingen, op alle oude tokens: nul treffers. `ls .claude/skills .agents/skills` toont geen oude naam.
- [ ] **Stap 5: Tests.** `cd <worktree> && go test ./... -race && uv run pytest -q && .lighthouse/scripts/check-instructions.sh` → alles groen, exit 0.
- [ ] **Stap 6: Commit** (pack, verwijzingen, installatiekopie samen): `refactor(pack): skills hernoemen naar taak of standard (#142)`.

### Taak 3: Hooks hernoemen en "write gates" wordt "write checks"

**Files:** `pack/*/hooks/*`, `hooks.json`, `requires.toml`, `pack/SOURCES.toml`, `pack/core/lib/*.py`, `pack/*/instructions/AGENTS.md`, skills die hooks noemen, `CLAUDE.md` (hooklijst), `docs/` (`docs/usage-guard.md` → `docs/usage-gate.md`), `docs/site/data/*.json`, `tests/fixtures/site/map.json`, tests.

| Oud | Nieuw | Tokens om te vervangen | Testbestand |
|---|---|---|---|
| `core/hooks/main-commit-guard.sh` | `main-commit-gate.sh` | `main-commit-guard` | `test_main_commit_guard.py` → `test_main_commit_gate.py` |
| `core/hooks/commit-tests.sh` | `commit-test-gate.sh` | `commit-tests` | `test_commit_tests.py` → `test_commit_test_gate.py`, `test_commit_tests_no_verify.py` → `test_commit_test_gate_no_verify.py` |
| `core/hooks/usage-guard.sh` + `core/hooks/usage-guard/` | `usage-gate.sh` + `usage-gate/` | `usage-guard.sh`, `hooks/lighthouse/usage-guard/`, `hooks_dir / "usage-guard"`, meldingsprefix `usage-guard:`, prose "usage-guard" | `test_usage_guard*.py` → `test_usage_gate*.py` |
| `go/hooks/go-check.sh` | `go-lint-gate.sh` | `go-check` | `go/test_go_check.py` → `go/test_go_lint_gate.py` |
| `python/hooks/ty-check.sh` | `ty-gate.sh` | `ty-check` | `python/test_ty_check.py` → `python/test_ty_gate.py` |
| `core/hooks/worktree-guard.sh` | `worktree-check.sh` | `worktree-guard` | `test_worktree_guard.py` → `test_worktree_check.py` |
| `core/hooks/worktree-linkcheck.sh` | `md-link-check.sh` | `worktree-linkcheck` | `test_worktree_linkcheck.py` → `test_md_link_check.py` |
| `core/hooks/code-language.sh` | `code-language-check.sh` | `code-language.sh`, meldingsprefix `code-language:` | `test_code_language_hook.py` blijft |
| `core/hooks/bash-write-gates.sh` | `bash-write-check.sh` | `bash-write-gates`, `LIGHTHOUSE_UNDER_BASH_GATES` → `LIGHTHOUSE_UNDER_BASH_WRITE_CHECK` | `test_bash_write_gates.py` → `test_bash_write_check.py` |
| `go/hooks/gofmt.sh` | `gofmt-check.sh` | `gofmt.sh`, prose "the gofmt hook/gate" | — |
| `python/hooks/ruff-format.sh` | `ruff-format-check.sh` | `ruff-format.sh`, prose "the ruff-format hook/gate" | — |
| `shell/hooks/shellcheck.sh` | `shell-lint-check.sh` | `shellcheck.sh`, prose "the shellcheck hook/gate" | — |

Ook: "write gates" → "write checks", "Edit\|Write gates" → "Edit\|Write checks" in prose en meldingen. Nooit vervangen: het commando `gofmt`, `shellcheck`, `ruff format`, `[usage_guard]`, `usage_guard.py`, `code_language`, het pad `"lighthouse" / "usage-guard"` in `pack/core/lib/usage_guard.py:141`, `[test_gate]` en "test gate".

- [ ] **Stap 1: Pin de statusmap.** Voeg `test_state_dir_keeps_old_name` toe aan het hernoemde `tests/pack/test_usage_gate_lib.py`: met `XDG_STATE_HOME=<tmp>` eindigt de statusmap van `usage_guard` op `lighthouse/usage-guard`. Draai: PASS (pint bestaand gedrag; faalt als stap 3 het pad raakt).
- [ ] **Stap 2: `git mv`** per regel, scripts en testbestanden.
- [ ] **Stap 3: Vervang de tokens** met dezelfde `git grep`-pathspecs als Taak 2 stap 2. Een meldingsprefix volgt de nieuwe scriptnaam; tests die erop asserten gaan mee.
- [ ] **Stap 4: Dogfood**, zoals Taak 2 stap 3. `.claude/settings.json` noemt daarna alleen nieuwe scriptnamen; de project-entries erin zijn onveranderd (`git diff .claude/settings.json` toont alleen lighthouse-commando's).
- [ ] **Stap 5: Restgrep** op alle oude tokens (inclusief "write gates"), zoals Taak 2 stap 4: alleen de toegestane resten uit de Global Constraints.
- [ ] **Stap 6: Tests**, zoals Taak 2 stap 5.
- [ ] **Stap 7: Commit:** `refactor(pack): hooks hernoemen naar gate of check (#142)`.

### Taak 4: De conventie in `place-instruction`

**Files:**
- Modify: `pack/core/skills/place-instruction/SKILL.md` (stap 5 en stap 6)
- Test: `tests/pack/test_place_instruction_skill.py`

**Interfaces:**
- Consumes: de namen uit Taak 2 en 3.

- [ ] **Stap 1: Falende test** `test_naming_convention_per_form`: de body van stap 6 bevat de vijf regels uit de spec-tabel, herkenbaar aan `verb + object`, `-standard`, `-gate`, `-check` en `occupation`; en stap 5 bevat een zin dat `type` wegblijft omdat de naam de soort draagt (assert op `the name carries`). Draai: FAIL.
- [ ] **Stap 2: Schrijf de tekst** (Engels): een tabel vorm → naam → één zin reden, de regel "a script with several modes is named after its strongest", en dat de controle (check 14) alleen lighthouse-hooks leest; voor project-hooks is het advies.
- [ ] **Stap 3: Dogfood + tests**, zoals Taak 2 stap 3 en 5.
- [ ] **Stap 4: Commit:** `docs(skills): naamconventie in place-instruction (#142)`.

### Taak 5: Check 14 — hooknamen

**Files:**
- Modify: `pack/core/scripts/check-instructions.sh` (nieuwe `check_hook_names`, aanroep na `check_pairing`; kopcommentaar regel 2: `1, 2, 3, 4, 5, 7, 8, 10, 11 and 14`)
- Test: `tests/pack/test_check_instructions.py`

**Interfaces:**
- Produces: `check_hook_names()` in bash, met inline Python zoals `hook_registered`. Leest `$ROOT/.claude/settings.json`; neemt alleen `command`-strings die `.claude/hooks/lighthouse/` bevatten; scriptnaam = de stam van het eerste `[A-Za-z0-9_-]+\.sh` na dat pad; verzamelt de events per script.

Uitkomsten (exacte teksten):
- `FAIL  hook-names: '<stam>' ends on neither -gate nor -check`
- `FAIL  hook-names: '<stam>' is a gate but no registration is on PreToolUse, Stop or SubagentStop`
- `FAIL  hook-names: '<stam>' is a check but is registered on Stop or SubagentStop`
- `FAIL  hook-names: '<settings>' is not valid JSON (or not an object)`
- `SKIPPED hook-names: '<settings>' does not exist` / `… python3 is missing on this machine`
- `ok    every lighthouse hook is named after what it does`

- [ ] **Stap 1: Falende tests**, elk met een eigen `settings.json` onder `tmp_path/"repo"/".claude"`:
  - `test_hook_names_ok`: `main-commit-gate.sh` op PreToolUse, `md-link-check.sh` op PostToolUse, `go-lint-gate.sh` op Stop → exit 0, `every lighthouse hook is named` in stdout.
  - `test_hook_name_without_suffix_fails`: `worktree-guard.sh` → exit 1, `ends on neither`.
  - `test_gate_only_after_the_fact_fails`: `foo-gate.sh` alleen op PostToolUse → `is a gate but`.
  - `test_check_on_stop_fails`: `foo-check.sh` op Stop → `is a check but`.
  - `test_hook_names_ignore_project_hooks`: `"$CLAUDE_PROJECT_DIR"/.claude/hooks/own.sh` en `npx prettier --write` naast een geldige lighthouse-hook → exit 0.
  - `test_hook_names_without_settings_skips`: geen `settings.json` → `SKIPPED  hook-names`, exit 0.
  Draai: `cd <worktree> && uv run pytest tests/pack/test_check_instructions.py -q -k hook_name` → FAIL.
- [ ] **Stap 2: Implementeer `check_hook_names`.**
- [ ] **Stap 3: Draai** dezelfde tests → PASS; `test_this_script_is_green_on_this_repo` blijft groen.
- [ ] **Stap 4: Dogfood + tests**, zoals Taak 2 stap 3 en 5.
- [ ] **Stap 5: Commit:** `feat(check-instructions): check 14 bewaakt hooknamen (#142)`.

### Taak 6: Changelog-fragment en eindcontrole

**Files:**
- Create: `changelog.d/issue-142-naamgeving.md`

- [ ] **Stap 1: Fragment** met frontmatter `type: changed` (vorm van `changelog.d/issue-80-stop-checks.md`), Engels. Noemt: de hernoemingen (oud → nieuw, beide tabellen), dat `init --upgrade` de oude namen verwijdert, dat een lokaal aangepaste kopie onder een oude naam de upgrade weigert met de twee uitwegen (de wijziging terugdraaien of `--force`), en dat `[usage_guard]` en `[test_gate]` blijven.
- [ ] **Stap 2: Eindcontrole.** `cd <worktree> && go test ./... -race && uv run pytest -q && .lighthouse/scripts/check-instructions.sh && go run ./cmd/lighthouse init --upgrade </dev/null` → groen, laatste regel `No changes.`. Restgrep op alle oude skill- en hooktokens over de hele repo (met alleen de historische uitsluitingen): alleen de toegestane resten.
- [ ] **Stap 3: Commit:** `docs(changelog): fragment voor de hernoemingen (#142)`.
