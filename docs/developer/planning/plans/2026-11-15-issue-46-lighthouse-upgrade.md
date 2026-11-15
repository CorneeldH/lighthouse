# Skill `lighthouse-upgrade` — implementatieplan (#46)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** een skill `/lighthouse-upgrade` in `pack/core` die een gebruiker-repo naar de versie van de geïnstalleerde lighthouse-binary brengt, met bewijs en zonder te forceren.

**Architectuur:** alleen skilltekst (Markdown, twee varianten) plus registratie: `requires.toml`,
`SOURCES.toml`, de skillijst in `pack/core/instructions/AGENTS.md`, de geïnstalleerde-bestandenlijst
in `internal/packs/core_test.go` en twee regels in `ci/literal-allowlist.txt`. Geen Go-code. Een
eigen pytest-bestand pint het gedrag dat de spec vastlegt.

**Tech stack:** Markdown-skills, pytest (`uv run pytest`), Go-test van de pakketinstallatie,
`ci/literal-grep.sh`.

**Spec:** [2026-11-15-issue-46-lighthouse-upgrade-design.md](../specs/2026-11-15-issue-46-lighthouse-upgrade-design.md)

## Globale eisen

- Skilltekst in het Engels; spec, plan, commit en PR in het Nederlands.
- Frontmatter: `name: lighthouse-upgrade`, daarna `description`, daarna `metadata:` met
  `binding: default` en `origin: own` (zoals `pack/core/skills/fix-pr/claude/SKILL.md:1-7`).
- De skill noemt nergens `go install` en nergens het modulepad.
- De lighthouse-repo staat in elke variant precies op één regel, exact `L=CorneeldH/lighthouse`,
  en alle `gh release`-aanroepen gebruiken `--repo "$L"`.
- `--force` komt alleen voor in een verbod ("never `--force`").
- `requires.toml`: `upgrade` bij `git` en `gh`; niet bij `python3`, `tomllib`, `jq`, `codex` of
  `superpowers`, tenzij de tekst die echt gebruikt (de contracttests controleren beide kanten).
- Commitonderwerp in de gebruiker-repo: `chore(lighthouse): <upgrade to <tag>, in [project] language>`.
  De spec noemt `upgrade naar <B>`; dat is de `nl`-vorm. De skill schrijft hem in de taal van de
  repo, omdat commitberichten artefacten zijn (core-regel).

## Review-focus

- **`lighthouse version` zonder `v`-prefix tegenover het manifest met `v`** (of andersom): de
  vergelijking moet `v0.17.0` en `0.17.0` gelijk zien. Pin: de skill normaliseert met
  `${x#v}` vóór `sort -V`; test `test_versions_are_compared_without_the_v_prefix`.
- **`dev` aan één kant**: stoppen, niet vergelijken. `lighthouse version` print `lighthouse dev`,
  dus de skill neemt het laatste veld. Test `test_dev_build_stops`.
- **Binary ouder dan het manifest**: stoppen met de melding dat de binary ouder is, nooit
  `init --upgrade` (dat zou downgraden). Test `test_older_binary_stops`.
- **Afgewezen `self-update` terwijl `B` = `M`**: "al actueel"-stop; afgewezen terwijl `B` > `M`:
  doorgaan naar `B`. Test `test_declined_self_update_continues_only_when_binary_is_ahead`.
- **Werkboom vuil na het bewijs** (test-gate schrijft bestanden): commit alleen wat
  `init --upgrade` en het spiegelbestand schreven; iets anders in `git status` is een stop. Test
  `test_commit_holds_only_what_init_wrote`.

---

### Taak 1: de skill, met registratie en pin-tests

**Bestanden:**
- Maken: `pack/core/skills/upgrade/claude/SKILL.md`, `pack/core/skills/upgrade/agents/SKILL.md`
- Maken: `tests/pack/test_upgrade_skill.py`
- Wijzigen: `pack/core/requires.toml` (regels `gh` en `git`)
- Wijzigen: `pack/SOURCES.toml` (twee `[[file]]`-blokken met `new = true`, na het blok van
  `pack/core/skills/test-standard/SKILL.md`)
- Wijzigen: `pack/core/instructions/AGENTS.md:15-28` (skillijst)
- Wijzigen: `internal/packs/core_test.go:31-73` (`.agents/skills/lighthouse-upgrade/SKILL.md` en
  `.claude/skills/lighthouse-upgrade/SKILL.md`, alfabetisch tussen `test-standard` en `write-handoff`)
- Wijzigen: `ci/literal-allowlist.txt` (twee regels, met een commentaarregel als reden)

**Interfaces:**
- Produceert: de skillnaam `lighthouse-upgrade` en de map `pack/core/skills/upgrade/`, die taak 2
  installeert.

- [ ] **Stap 1: schrijf de falende tests** in `tests/pack/test_upgrade_skill.py`, naar het patroon
  van `tests/pack/test_test_standard_skill.py`, geparametriseerd over beide varianten:

```python
VARIANTS = {v: (CORE / "skills" / "upgrade" / v / "SKILL.md").read_text(encoding="utf-8")
            for v in ("claude", "agents")}

@pytest.mark.parametrize("v", VARIANTS)
def test_frontmatter(v):
    front = VARIANTS[v].split("---\n", 2)[1]
    assert "name: lighthouse-upgrade\n" in front
    assert "origin: own" in front

@pytest.mark.parametrize("v", VARIANTS)
def test_reads_both_versions(v):
    assert ".lighthouse/manifest.toml" in VARIANTS[v]
    assert "lighthouse version" in VARIANTS[v]

@pytest.mark.parametrize("v", VARIANTS)
def test_versions_are_compared_without_the_v_prefix(v):
    assert "#v}" in VARIANTS[v] and "sort -V" in VARIANTS[v]

@pytest.mark.parametrize("v", VARIANTS)
def test_dev_build_stops(v):
    assert re.search(r"`dev`.*stop", VARIANTS[v], re.I)

@pytest.mark.parametrize("v", VARIANTS)
def test_older_binary_stops(v):
    assert re.search(r"older than the (repo|manifest).*stop", VARIANTS[v], re.I | re.S)

@pytest.mark.parametrize("v", VARIANTS)
def test_binary_only_through_self_update_after_consent(v):
    t = VARIANTS[v]
    assert "lighthouse self-update" in t and "go install" not in t
    assert "whole host" in t

@pytest.mark.parametrize("v", VARIANTS)
def test_declined_self_update_continues_only_when_binary_is_ahead(v):
    assert "already current" in VARIANTS[v]

@pytest.mark.parametrize("v", VARIANTS)
def test_release_notes_come_from_the_lighthouse_repo(v):
    """Pins ci/literal-allowlist.txt: the one line that names the lighthouse repo."""
    lines = VARIANTS[v].splitlines()
    assert lines.count("L=CorneeldH/lighthouse") == 1
    assert sum("CorneeldH" in line for line in lines) == 1
    assert '--repo "$L"' in VARIANTS[v]

@pytest.mark.parametrize("v", VARIANTS)
def test_never_forces_a_conflict(v):
    t = VARIANTS[v]
    assert "refused:" in t
    assert all("never" in line.lower() for line in t.splitlines() if "--force" in line)

@pytest.mark.parametrize("v", VARIANTS)
def test_evidence_in_order(v):
    t = VARIANTS[v]
    keys = ["test_gate.command", "check-instructions.sh", "--print-mirror-exceptions", "No changes."]
    assert [t.index(k) for k in keys] == sorted(t.index(k) for k in keys)

@pytest.mark.parametrize("v", VARIANTS)
def test_commit_holds_only_what_init_wrote(v):
    assert "git status" in VARIANTS[v] and "chore(lighthouse):" in VARIANTS[v]

@pytest.mark.parametrize("v", VARIANTS)
def test_push_and_pr_after_consent_through_write_pr(v):
    assert "/lighthouse-write-pr" in VARIANTS[v]

def test_agents_md_lists_the_skill():
    assert "- `/lighthouse-upgrade`:" in (CORE / "instructions" / "AGENTS.md").read_text(encoding="utf-8")
```

- [ ] **Stap 2: draai ze, verwacht rood**

Run: `uv run pytest tests/pack/test_upgrade_skill.py -q`
Verwacht: FAIL bij het inlezen (`FileNotFoundError` op `upgrade/claude/SKILL.md`).

- [ ] **Stap 3: schrijf `pack/core/skills/upgrade/claude/SKILL.md`** met de zeven stappen uit de
  spec (§ Werking), in die volgorde en met de koppen van een bestaande skill (`## Domain`,
  `## Workflow`, `## Non-negotiable`). Vast:
  - de description triggert op "upgrade lighthouse", "bring this repo to the new lighthouse",
    `/lighthouse-upgrade`, en heeft een `NOTE —`-zin: niet voor de binary zelf
    (`lighthouse self-update`) en niet voor lighthouse's eigen checkout (`dev`);
  - stap 1 staat in één Bash-blok: `L=CorneeldH/lighthouse`, `M` uit het manifest met
    `sed -n 's/^version = "\(.*\)"$/\1/p' .lighthouse/manifest.toml | head -n 1`,
    `B=$(lighthouse version | awk '{print $NF}')` (de uitvoer is `lighthouse <versie>`, bijvoorbeeld
    `lighthouse dev`), `N=$(gh release view --repo "$L" --json tagName --jq .tagName)`,
    elk genormaliseerd met `${x#v}`;
  - stap 2: akkoord via `AskUserQuestion` (claude-variant);
  - stap 3: `gh release view <tag> --repo "$L" --json body --jq .body` per tag uit
    `gh release list --repo "$L" --exclude-pre-releases --json tagName --jq '.[].tagName'`
    tussen `M` (exclusief) en `B` (inclusief);
  - stap 4: default branch uit `lighthouse config forge.default_branch`, worktree onder
    `.claude/worktrees/`;
  - stap 6: `[paths] mirror_exceptions` is optioneel ("if set"); `test_gate.command` lezen met
    `lighthouse config test_gate.command`;
  - stap 7: push en `/lighthouse-write-pr` alleen na akkoord.

- [ ] **Stap 4: schrijf `pack/core/skills/upgrade/agents/SKILL.md`**: dezelfde tekst, met akkoord
  als vraag in de chat in plaats van `AskUserQuestion`.

- [ ] **Stap 5: registreer**
  - `requires.toml`: `"upgrade"` achteraan bij `gh` en bij `git`;
  - `SOURCES.toml`: `[[file]] path = "pack/core/skills/upgrade/claude/SKILL.md" new = true`, idem
    voor `agents`;
  - `pack/core/instructions/AGENTS.md`: `` - `/lighthouse-upgrade`: bring this repo to the installed lighthouse version. ``
    na de regel van `lighthouse-test-standard`;
  - `internal/packs/core_test.go`: de twee installatiepaden;
  - `ci/literal-allowlist.txt`, met de reden erboven:

```text
# The lighthouse repo, for the release notes the upgrade skill shows (spec #46 § Vastgelegd).
# Pinned by tests/pack/test_upgrade_skill.py::test_release_notes_come_from_the_lighthouse_repo.
pack/core/skills/upgrade/claude/SKILL.md:L=CorneeldH/lighthouse
pack/core/skills/upgrade/agents/SKILL.md:L=CorneeldH/lighthouse
```

- [ ] **Stap 6: draai alles van deze taak, verwacht groen**

Run: `uv run pytest tests/pack/test_upgrade_skill.py tests/pack/test_skill_contracts.py tests/test_literal_grep.py -q && go test ./internal/packs/... && ci/literal-grep.sh`
Verwacht: alle tests geslaagd; `literal-grep.sh` exit 0.

- [ ] **Stap 7: mutatiecheck** (`/lighthouse-test-standard`): zet in de claude-variant tijdelijk
  `--force` zonder "never", en daarna `go install` ergens in de tekst. Elk moet een test rood maken
  (`test_never_forces_a_conflict`, `test_binary_only_through_self_update_after_consent`). Draai
  terug.

- [ ] **Stap 8: commit**

```bash
git add pack/core/skills/upgrade tests/pack/test_upgrade_skill.py pack/core/requires.toml pack/SOURCES.toml pack/core/instructions/AGENTS.md internal/packs/core_test.go ci/literal-allowlist.txt
git commit -m "feat(core): skill lighthouse-upgrade brengt een repo naar de geïnstalleerde versie (#46)"
```

### Taak 2: dogfood, docs en changelog

**Bestanden:**
- Gegenereerd: `.claude/skills/lighthouse-upgrade/SKILL.md`, `.agents/skills/lighthouse-upgrade/SKILL.md`,
  `.lighthouse/manifest.toml`, `AGENTS.md` (beheerd blok)
- Wijzigen: `docs/releasing.md` (§ Upgrading, stap 3-4)
- Maken: `changelog.d/issue-46-lighthouse-upgrade.md`

**Interfaces:**
- Consumeert: `pack/core/skills/upgrade/` uit taak 1.

- [ ] **Stap 1: installeer de eigen kopie**

Run: `go run ./cmd/lighthouse init --upgrade --yes`
Verwacht: de twee skillbestanden, het manifest en het beheerde blok van `AGENTS.md` veranderen;
geen `refused:`.

- [ ] **Stap 2: controleer dogfood**

Run: `go run ./cmd/lighthouse init --upgrade </dev/null`
Verwacht: exact `No changes.`

- [ ] **Stap 3: docs.** In `docs/releasing.md` § Upgrading worden stap 3 en 4 één stap: "In every
  repository that uses lighthouse, run `/lighthouse-upgrade`: it runs `lighthouse init --upgrade`
  in a worktree, checks it, and commits only what it changed. By hand: `lighthouse init --upgrade`,
  then commit the installed copy and send it as a PR." De stapnummering daaronder schuift op.

- [ ] **Stap 4: changelog-fragment** `changelog.d/issue-46-lighthouse-upgrade.md`:

```markdown
---
type: added
---
`/lighthouse-upgrade` brings a repository to the version of the installed `lighthouse` binary: it offers `lighthouse self-update` when a newer release exists, shows the release notes in between, runs `lighthouse init --upgrade` in a worktree, checks the test gate, `check-instructions.sh` and `No changes.`, and commits only what the upgrade wrote. Push and PR follow only after your approval.
```

- [ ] **Stap 5: volledige testpoort**

Run: `bash -c "$(lighthouse config test_gate.command)"` (met `LIGHTHOUSE_GATE_TMP` op een lege map)
Verwacht: Go, Python en node groen.

- [ ] **Stap 6: commit**

```bash
git add .claude/skills/lighthouse-upgrade .agents/skills/lighthouse-upgrade .lighthouse/manifest.toml AGENTS.md docs/releasing.md changelog.d/issue-46-lighthouse-upgrade.md
git commit -m "chore(core): dogfood van lighthouse-upgrade, docs en changelog (#46)"
```
