# macOS-tests van de PR naar de releasepoort — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Een PR draait geen macOS-job meer; een release komt alleen van een commit waarvan de macOS-tests groen waren (#337).

**Architecture:** Een nieuwe herbruikbare workflow `macos.yml` (`workflow_call` en `workflow_dispatch`) draait `python` ×3, `go-test` en `go-e2e` op `macos-latest`. `release.yml` roept hem aan als job `macos`; job `release` heeft `needs: macos` en releaset alleen als `origin/main` nog gelijk is aan `github.sha`, de commit die `macos` testte. Daarna gaan de macOS-jobs uit `ci.yml`.

**Tech Stack:** GitHub Actions-YAML, pytest-pintests op de workflowtekst.

**Spec:** [2026-11-14-macos-releasepoort-design.md](../specs/2026-11-14-macos-releasepoort-design.md)

## Global Constraints

- Actions blijven gepind op een 40-hex SHA met `# vX.Y.Z`; gebruik exact de SHA's die `ci.yml` nu heeft.
- Elke `actions/checkout` buiten de job `release` heeft `persist-credentials: false`.
- Geen `${{ … }}` in een `run:`-blok; waarden gaan via `env:` (`test_no_inputs_in_run_scripts`).
- `go test`-stappen houden de flags van de gate: `go test -race -shuffle=on ./...` en `go test -race -shuffle=on -tags e2e ./e2e/ -v`.
- De geteste commit is `github.sha`: bij push de gepushte commit, bij cron en dispatch de kop van de default branch, en bij *Re-run failed jobs* dezelfde als in de eerste poging. `macos.yml` checkt uit zonder `ref:`.
- Commentaar in workflows en het changelog-fragment in het Engels (`code_language = en`); commits en dit plan in het Nederlands, Conventional Commits.
- Elke commit groen op `[test_gate] command`; nooit `--no-verify`.

## Review Focus

1. **`main` schuift tijdens de macOS-run** (een merge binnen ~5 min na de vorige): `release` stopt met `exit 0`, zonder tag, en de run van de nieuwere commit releaset. Gepind in taak 1 (`test_release_only_releases_the_tested_commit`).
2. **De push van `release` wordt geweigerd omdat `main` bewoog**: de herhaallus haalt opnieuw op, ziet dat `main` verschoven is en stopt met `exit 0`, in plaats van een niet-geteste commit te taggen. Valt onder dezelfde test (de controle staat binnen de lus, na de reset).
3. **De maandag-cron wordt ingehaald door een push**: de cron-run stopt; de push-run volgt de push-regel, dus fragmenten met alleen `added`/`changed`/`removed` wachten een week. `releasing.md` zegt: start dan `release` met de hand. Taak 1, stap docs.
4. **`release` met de hand gestart vanaf een andere branch dan `main`**: `github.sha` ≠ `origin/main`, dus geen release. `releasing.md` zegt: start vanaf `main`. Taak 1, stap docs.
5. **macOS sluipt terug op de PR** (iemand voegt `pull_request` toe aan `macos.yml` of `macos-latest` aan `ci.yml`): gepind in taak 1 (`test_macos_gate_triggers`) en taak 2 (`test_pr_runs_no_macos_job`).

---

### Task 1: macOS-poort vóór de release

**Files:**
- Create: `.github/workflows/macos.yml`
- Modify: `.github/workflows/release.yml` (job `macos` erbij, job `release`: `needs`, `env`, controle in de lus)
- Modify: `docs/releasing.md` (nieuwe sectie, aanvulling op "Recovering a failed release")
- Test: `tests/test_release_workflows_pin.py`, `tests/test_ci_go_test_pin.py`

**Interfaces:**
- Produces: `.github/workflows/macos.yml` met jobs `python`, `go-test`, `go-e2e`; job `macos` in `release.yml`. Taak 2 leunt erop dat macOS hier al draait voordat het uit `ci.yml` verdwijnt.

- [ ] **Stap 1: Schrijf de falende tests in `tests/test_release_workflows_pin.py`**

Voeg `MACOS = WORKFLOWS / "macos.yml"` toe naast `RELEASE` en `BUILD`, en deze tests:

```python
def test_macos_gate_triggers():
    """Fails when macos.yml runs on a PR, a push or a schedule, or can no longer be called or started by hand."""
    # Only the `on:` block: the header comment may name a push or a PR.
    on_block = _text(MACOS).split("\non:\n", 1)[1].split("\npermissions:")[0]
    assert "workflow_call" in on_block and "workflow_dispatch" in on_block
    for trigger in ("pull_request", "push", "schedule"):
        assert trigger not in on_block, trigger


def test_macos_gate_runs_the_suites_on_macos():
    """Fails when a suite drops out of the macOS gate or stops running on macos-latest."""
    jobs = _jobs(MACOS)
    assert sorted(jobs) == ["go-e2e", "go-test", "python"]
    for name, job in jobs.items():
        assert re.search(r"^    runs-on: macos-latest\s*$", job, re.MULTILINE), name
        assert "timeout-minutes:" in job, name
    python = jobs["python"]
    assert re.search(r"^        shard: \[1, 2, 3\]\s*$", python, re.MULTILINE)
    assert "LIGHTHOUSE_PYTEST_SHARD: ${{ matrix.shard }}/3" in python
    assert "uv run pytest -n auto" in python
    assert "actions/setup-go@" in python  # the lighthouse_bin fixture builds cmd/lighthouse
    assert "- run: go test -race -shuffle=on ./..." in jobs["go-test"]
    assert "- run: go test -race -shuffle=on -tags e2e ./e2e/ -v" in jobs["go-e2e"]
    assert "astral-sh/setup-uv@" in jobs["go-e2e"]
    assert "ci/literal-grep.sh" not in _text(MACOS)


def test_release_waits_for_macos():
    """Fails when the release can start without a green macOS run (#337)."""
    jobs = _jobs(RELEASE)
    assert "uses: ./.github/workflows/macos.yml" in jobs["macos"]
    assert re.search(r"^    needs: macos\s*$", jobs["release"], re.MULTILINE)


def test_release_only_releases_the_tested_commit():
    """Fails when release can tag a commit macOS did not test, or turns red when main moved on."""
    release = _jobs(RELEASE)["release"]
    assert "TESTED: ${{ github.sha }}" in release
    line = re.search(
        r'^\s*if \[ "\$\(git rev-parse HEAD\)" != "\$TESTED" \]; then echo "release: [^"]+"; exit 0; fi\s*$',
        release,
        re.MULTILINE,
    )
    assert line, "no tested-commit check that exits 0"
    check = line.start()
    assert release.index("for attempt in 1 2 3") < check
    assert release.index("git reset --hard origin/main") < check
    assert check < release.index("ci/changelog.py release")
```

Breid uit, zodat `macos.yml` dezelfde hygiëne krijgt:
- `test_actions_pinned_by_sha`: loop over `(RELEASE, BUILD, MACOS)`; sla elke `./.github/workflows/<naam>.yml` over (nu alleen `build.yml`).
- `test_only_release_job_persists_credentials` en `test_no_inputs_in_run_scripts`: loop ook over `MACOS` (exempt 0).
- `test_write_permission_only_where_needed`: loop ook over `MACOS`; `allowed` blijft gelijk.

In `tests/test_ci_go_test_pin.py`: maak `_ci_go_commands` een `_go_commands(path: Path) -> list[str]` en voeg toe:

```python
def test_every_macos_go_test_has_gate_flags():
    """Fails when a `go test` step in the macOS gate drops a flag of the gate."""
    flags = [token for token in _gate_go_command().split() if token.startswith("-")]
    runs = _go_commands(ROOT / ".github" / "workflows" / "macos.yml")
    assert len(runs) == 2, runs
    for run in runs:
        assert all(flag in run.split() for flag in flags), run
```

- [ ] **Stap 2: Draai de tests en zie ze falen**

Run: `uv run pytest -q tests/test_release_workflows_pin.py tests/test_ci_go_test_pin.py`
Verwacht: FAIL in de vier nieuwe tests en in `test_every_macos_go_test_has_gate_flags` (`macos.yml` bestaat niet, `KeyError: 'macos'`).

- [ ] **Stap 3: Schrijf `.github/workflows/macos.yml`**

Kopcommentaar (Engels): wat het is, dat het de poort vóór de release is (#337), en dat het met de hand op een branch kan starten. `on: workflow_call:` en `workflow_dispatch:`; `permissions: contents: read`. De jobs `python`, `go-test` en `go-e2e` zijn de huidige jobs uit `ci.yml` met `runs-on: macos-latest`, zonder `needs`/`if` op `changes`, zonder de `ci/literal-grep.sh`-stap en zonder `os` in de matrix (`python` houdt `fail-fast: false` en `shard: [1, 2, 3]`). Timeouts: `python` 15, `go-test` en `go-e2e` 10.

- [ ] **Stap 4: Pas `.github/workflows/release.yml` aan**

- Nieuwe eerste job:
  ```yaml
  # macOS runs only here, not on a PR (#337): a release needs a green macOS run of its commit.
  macos:
    uses: ./.github/workflows/macos.yml
  ```
- Job `release`: `needs: macos`, en bij de stap `id: release` in `env:` `TESTED: ${{ github.sha }}`.
- In de lus, direct na `git reset --hard origin/main`, exact deze regel:
  ```bash
  if [ "$(git rev-parse HEAD)" != "$TESTED" ]; then echo "release: main moved past $TESTED, the commit macOS tested; the run of the newer commit releases it"; exit 0; fi
  ```
- Kopcommentaar van `release.yml`: één zin dat de release op een groene macOS-run van zijn commit wacht.

- [ ] **Stap 5: Draai de tests en zie ze slagen**

Run: `uv run pytest -q tests/test_release_workflows_pin.py tests/test_ci_go_test_pin.py`
Verwacht: alles PASS.

- [ ] **Stap 6: Mutatiecheck**

Haal tijdelijk `needs: macos` weg en verander daarna `exit 0` in `exit 1` in de controleregel; draai na elke wijziging stap 5. Verwacht: FAIL in `test_release_waits_for_macos` resp. `test_release_only_releases_the_tested_commit`. Zet beide terug.

- [ ] **Stap 7: Docs in `docs/releasing.md`**

Nieuwe sectie `## The macOS gate`, na "When a release happens", met:
- Een release wacht op een groene run van `macos.yml` voor de commit die de run startte (`github.sha`). Een PR draait geen macOS.
- Rood: geen tag, geen release-commit; de fragmenten blijven open. Opnieuw: *Re-run failed jobs* op de run; die test dezelfde commit opnieuw.
- `main` verschoven tijdens de macOS-run: de run stopt groen zonder release; de run van de nieuwere commit releaset.
- Een maandag-cron die zo stopt, laat fragmenten met alleen `added`/`changed`/`removed` een week wachten: start dan `release` met de hand (`bump: auto`) vanaf `main`.
- Een handmatige `release` vanaf een andere branch dan `main` releaset niets.
- `macos.yml` kan ook met de hand (`workflow_dispatch`) op een branch, voor een PR die bewust aan portabiliteit raakt.

In "Recovering a failed release": een zin dat *Re-run failed jobs* ook de macOS-jobs opnieuw draait.

- [ ] **Stap 8: Commit**

```bash
git add .github/workflows/macos.yml .github/workflows/release.yml docs/releasing.md tests/test_release_workflows_pin.py tests/test_ci_go_test_pin.py
git commit -m "feat(ci): macOS-tests als poort vóór de release (#337)"
```

---

### Task 2: macOS van de PR af

**Files:**
- Modify: `.github/workflows/ci.yml` (kopcommentaar, jobs `python`, `go-test`, `go-e2e`)
- Test: `tests/test_ci_domains_pin.py`
- Create: `changelog.d/fix-macos-releasepoort.md`

**Interfaces:**
- Consumes: `macos.yml` en de job `macos` in `release.yml` uit taak 1.

- [ ] **Stap 1: Pas de tests in `tests/test_ci_domains_pin.py` aan**

Vervang `test_tests_run_on_linux_and_macos` door:

```python
def test_pr_runs_no_macos_job():
    """Fails when a PR job runs on macOS again: macOS is the gate before the release (#337)."""
    assert "macos-latest" not in _text()
    jobs = _jobs()
    for name in ("python", "go-test", "go-e2e"):
        assert re.search(r"^    runs-on: ubuntu-latest\s*$", jobs[name], re.MULTILINE), name
        assert "matrix.os" not in jobs[name], name
```

In `test_python_job_runs_in_three_shards` wordt de eerste regel van de verwachte grep-stap `r"if: matrix\.shard == 1\n"`.

- [ ] **Stap 2: Draai de tests en zie ze falen**

Run: `uv run pytest -q tests/test_ci_domains_pin.py`
Verwacht: FAIL in `test_pr_runs_no_macos_job` en `test_python_job_runs_in_three_shards`.

- [ ] **Stap 3: Pas `.github/workflows/ci.yml` aan**

- `python`: `runs-on: ubuntu-latest`, matrix alleen `shard: [1, 2, 3]`, `timeout-minutes: 10` (ubuntu-jobs duren p90 2,7 min), het commentaar over trage macOS-runners weg, de literal-grep-stap `if: matrix.shard == 1`.
- `go-test` en `go-e2e`: `runs-on: ubuntu-latest`, `strategy`-blok weg.
- Kopcommentaar: "Python, `go-test` and `go-e2e` run on Linux and macOS (#90)" wordt dat ze op Linux draaien, en dat macOS in `macos.yml` draait als poort vóór de release (#337).

- [ ] **Stap 4: Draai alle workflow-pintests**

Run: `uv run pytest -q tests/test_ci_domains_pin.py tests/test_ci_go_test_pin.py tests/test_release_workflows_pin.py tests/test_golangci_pin.py tests/test_domains.py`
Verwacht: alles PASS.

- [ ] **Stap 5: Changelog-fragment**

`changelog.d/fix-macos-releasepoort.md`:

```markdown
---
type: internal
---
Pull requests no longer run the test suites on macOS; a release waits for a green macOS run of its commit instead.
```

- [ ] **Stap 6: Commit**

```bash
git add .github/workflows/ci.yml tests/test_ci_domains_pin.py changelog.d/fix-macos-releasepoort.md
git commit -m "ci: macOS-jobs van de PR af, alleen nog in de releasepoort (#337)"
```

---

## Na de merge

Pas na de merge bestaat `macos.yml` op `main`. Controleer dan: de PR-run van deze branch had geen macOS-job, en de `release`-run van de merge-commit toont de job `macos` vóór `release` (`gh run list --workflow release.yml --limit 1`, daarna `gh run view <id>`). Zet de uitkomst als bewijs in #337.
