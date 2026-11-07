# Issue #90 — lighthouse op macOS en Windows: implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Meten waar lighthouse buiten Linux breekt, het kleine oplossen, het grote als issue vastleggen, en de startgids laten zeggen welke route op Windows werkt.

**Architecture:** CI krijgt een `os`-matrix (`ubuntu-latest`, `macos-latest`) op de jobs `go-test` en `python`; de bestaande pin-tests blijven heel omdat de `run`-regels niet veranderen. Een nieuwe Python-test verbiedt GNU-only en bash-4-constructies in de shell van `pack/`. De handmatige metingen op een echte Mac en op Windows volgen een vast protocol; hun uitkomst gaat als comment op #90 en bepaalt de tekst in de startgids.

**Tech Stack:** GitHub Actions, pytest (uv), Go (`go test -race -shuffle=on`), bash 3.2 (macOS `/bin/bash`), BSD userland.

**Spec:** geen aparte spec: de acceptatiecriteria van #90 leggen het HOE vast (Poort 4b). Het issue is de spec.

## Global Constraints

- CI-runner voor macOS: `macos-latest` (letterlijk uit AC 1).
- De `run`-regels `go test -race -shuffle=on ./...` en `go test -race -shuffle=on -tags e2e ./e2e/ -v` blijven byte-gelijk: `tests/test_ci_go_test_pin.py` en `tests/test_ci_domains_pin.py` pinnen ze. Precies twee `- run: go test`-regels in `ci.yml`.
- Elke action blijft op SHA gepind met `# vX.Y.Z`-commentaar; het aantal `uses:` in `ci.yml` blijft 20 (`test_actions_pinned_by_sha`). Een matrix voegt geen `uses:` toe.
- De job `changelog` blijft op `ubuntu-latest`.
- Code, commentaar, testnamen en bestandsnamen in `pack/`, `tests/`, `ci/` zijn Engels. Issue-comments, PR-body en dit plan Nederlands. `docs/install.md` en `docs/site/**` blijven Engels.
- Dogfood: na elke wijziging onder `pack/` draait `go run ./cmd/lighthouse init --upgrade` en gaat de geïnstalleerde kopie mee in dezelfde commit; CI eist `No changes.`
- Changelog-fragment: `changelog.d/issue-90-macos-windows.md`, frontmatter `type: fixed` (de shasum-fallback is een fix voor gebruikers), 1-3 Engelse zinnen.
- Een rode test op macOS die niet klein te fixen is: eigen issue via `/lighthouse-write-issue`, en de test krijgt een skip met dat nummer in de reden (`pytest.mark.skipif(sys.platform == "darwin", reason="#<nr>: …")`, in Go `t.Skip("#<nr>: …")` achter `runtime.GOOS == "darwin"`). Klein = één bestand, geen ontwerpkeuze, ≤ ~30 regels.
- Geen `lighthouse serve`, systemd, controller of SPA (no-gos van de milestone). Een rode test daarin op macOS wordt een skip met issue, geen port.
- `/lighthouse-verify-pr` uit AC 2 bestaat niet meer: die is opgegaan in `/lighthouse-write-pr` (`pack/SOURCES.toml:273-274`). Het protocol gebruikt `/lighthouse-write-pr` en meldt die vervanging in de comment.

## Review Focus

1. **Een hook draait op een Mac onder `/bin/bash` 3.2**, ook als de CI-runner een nieuwere bash op `PATH` heeft. Verwacht: geen `declare -A`, `mapfile`, `${x,,}` in `pack/**/*.sh`. → Task 2, test `test_pack_shell_has_no_bash4_constructs`.
2. **`shell-lint-check` op een Mac zonder `sha256sum`** moet de pinned shellcheck downloaden en verifiëren met `shasum -a 256`. → Task 2, test `test_download_verifies_with_shasum_when_sha256sum_is_missing`.
3. **De test-gate op een Mac zonder `/proc/meminfo`** blokkeert elke commit zodra `host.toml` geheugendrempels zet (`docs/test-gate.md:27`). Verwacht in het Mac-protocol: zonder drempels gaat de commit door. → Task 3, stap "commit in worktree" met en zonder `host.toml`.
4. **Hooks onder de systeem-`python3` 3.9 van macOS.** Verwacht: hooks starten zonder `SyntaxError`. CI dekt dat niet (uv pint een nieuwere Python). → Task 3, protocolstap met `/usr/bin/python3 --version` in de uitkomst.
5. **`usage-gate.sh` met een `UNTIL` als `…Z` en als `…+01:00` onder BSD `date`.** De fallback bestaat (`pack/core/hooks/usage-gate.sh:66-77`), maar `docs/install.md:9` zegt nog dat hij GNU-only is. → Task 2, test `test_usage_gate_parses_until_with_bsd_date_only`, en Task 4 haalt de docregel weg.

---

### Task 1: CI draait de Go- en Python-tests ook op `macos-latest`

**Files:**
- Modify: `.github/workflows/ci.yml` (jobs `python` regels 86-110, `go-test` regels 145-158)
- Test: `tests/test_ci_domains_pin.py`

**Interfaces:**
- Produces: CI-jobnamen `python (<os>, <shard>)` en `go-test (<os>)`; Task 3 leest hun uitkomst.

- [ ] **Step 1: Schrijf de falende test** in `tests/test_ci_domains_pin.py`, met de bestaande helper `_jobs()`:

```python
def test_tests_run_on_linux_and_macos():
    """Fails when the Go race tests or the pytest suite stop running on macOS (#90)."""
    jobs = _jobs()
    for name in ("python", "go-test"):
        job = jobs[name]
        assert "runs-on: ${{ matrix.os }}" in job, name
        assert re.search(r"os: \[ubuntu-latest, macos-latest\]", job), name
```

- [ ] **Step 2: Draai hem en zie hem falen**

Run: `uv run pytest -q tests/test_ci_domains_pin.py::test_tests_run_on_linux_and_macos`
Expected: FAIL op `runs-on: ${{ matrix.os }}` voor `python`.

- [ ] **Step 3: Pas `ci.yml` aan.**
  - `python`: `matrix: {os: [ubuntu-latest, macos-latest], shard: [1, 2, 3]}`, `runs-on: ${{ matrix.os }}`, `timeout-minutes: 15` (macOS-runners zijn trager; meet en stel bij). De stap `ci/literal-grep.sh` krijgt `if: matrix.shard == 1 && matrix.os == 'ubuntu-latest'`.
  - `go-test`: `strategy: {fail-fast: false, matrix: {os: [ubuntu-latest, macos-latest]}}`, `runs-on: ${{ matrix.os }}`. De `run`-regel blijft letterlijk.
  - `go`, `go-e2e`, `web`, `binary`, `secret-scan`, `changelog`, `changes`: ongewijzigd.
  - Werk de commentaarkop van `ci.yml` (regels 1-4) bij: Python en `go-test` draaien op Linux en macOS.

- [ ] **Step 4: Draai de CI-pins**

Run: `uv run pytest -q tests/test_ci_domains_pin.py tests/test_ci_go_test_pin.py`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/ci.yml tests/test_ci_domains_pin.py
git commit -m "ci: draai go-test en pytest ook op macos-latest (#90)"
```

- [ ] **Step 6: Push en open een draft-PR** om de eerste macOS-run te krijgen (CI draait alleen op `pull_request`). Body: `Refs #90 — draft voor de macOS-meting in CI; wordt Closes na de handmatige metingen`. Voeg eerst het changelog-fragment toe (zie Global Constraints), anders is de job `changelog` rood.

- [ ] **Step 7: Lees de macOS-jobs.** Per rode test één regel: test-id, foutregel, oorzaak, en `klein` of `groot`. Kleine fixes gaan naar Task 2 (shell) of een eigen commit `fix(<scope>): … (#90)` met een test die op Linux al faalt waar dat kan. Grote: issue via `/lighthouse-write-issue` (milestone "Product: eerste versie voor anderen") en een skip met het nummer. Herhaal tot beide macOS-jobs groen zijn. Verwacht minstens: `tests/pack/test_shell_style.py` (shellcheck-download zonder `sha256sum`), tests rond `/proc`, `/run/user`, systemd-stubs, gitleaks.

---

### Task 2: GNU-only en bash-4-constructies in `pack/`

**Files:**
- Modify: `pack/shell/scripts/lib/shellcheck-bin.sh:67`
- Modify: `pack/shell/requires.toml:7`
- Create: `tests/pack/test_portable_shell.py`
- Modify: `tests/pack/shell/test_shellcheck.py` (één test erbij)
- Modify: `tests/pack/test_usage_gate_controller.py` of een nieuw testbestand naast de bestaande usage-gate-tests (één test erbij)

**Interfaces:**
- Consumes: niets uit Task 1.
- Produces: de marker `# portable: <reden>` op een regel die een verboden construct bewust gebruikt naast een fallback.

- [ ] **Step 1: Schrijf `tests/pack/test_portable_shell.py`.** Loopt over alle `pack/**/*.sh` plus de scripts zonder extensie met een bash-shebang. Commentaarregels (na `#`) tellen niet. Een regel met `# portable:` telt niet.

```python
GNU_ONLY = {
    r"\bsed -i(\s|$)": "sed -i without a suffix (BSD needs -i '')",
    r"\bdate -d\b": "GNU date -d",
    r"\bstat -c\b": "GNU stat -c",
    r"\breadlink -f\b": "readlink -f",
    r"\bgrep -[a-zA-Z]*P": "grep -P",
    r"\bxargs -r\b": "xargs -r",
    r"(?<![\w-])sha256sum\b": "sha256sum (macOS has shasum -a 256)",
    r"-printf\b": "find -printf",
}
BASH4 = {
    r"\bdeclare -A\b": "associative array",
    r"\b(mapfile|readarray)\b": "mapfile/readarray",
    r"\$\{\w+(,,|\^\^)\}": "case conversion",
    r"&>>": "&>>",
    r"\|&": "|&",
    r"\blocal -n\b": "nameref",
}

def test_pack_shell_has_no_gnu_only_constructs(): ...  # assert no hits; message lists file:line: reason
def test_pack_shell_has_no_bash4_constructs(): ...
def test_portable_marker_needs_a_reason(): ...  # `# portable:` followed by at least one word
```

Een `command -v sha256sum`-test (zoals `pack/core/scripts/check-instructions.sh:53`) is een fallback, geen gebruik: sta `command -v <tool>` toe in de regex of zet de marker.

- [ ] **Step 2: Draai en zie welke regels falen**

Run: `uv run pytest -q tests/pack/test_portable_shell.py`
Expected: FAIL met minstens `pack/shell/scripts/lib/shellcheck-bin.sh:67: sha256sum` en `pack/core/hooks/usage-gate.sh:75: GNU date -d`. Elke andere hit: fix of marker met reden. Een hit die niet klein is: issue + noteer het nummer in de marker.

- [ ] **Step 3: Schrijf de falende test voor de shasum-fallback** in `tests/pack/shell/test_shellcheck.py`: een stub-`PATH` met `uname` (meldt `Darwin`/`arm64`), een `curl` die een vast bestand neerzet, een `shasum` die `-a 256` vereist en de pin-hash van `darwin/arm64` teruggeeft, en géén `sha256sum`.

```python
def test_download_verifies_with_shasum_when_sha256sum_is_missing(tmp_path):
    """Fails when the pinned download needs GNU sha256sum, which macOS lacks (#90)."""
    ...
    assert "sha256 mismatch" not in result.stderr
    assert "sha256sum: command not found" not in result.stderr
    assert shasum_log.read_text().split()[:2] == ["-a", "256"]
```

Gebruik `tar` echt (de stub-`curl` schrijft een `.tar.xz` met een fake `shellcheck-v<pin>/shellcheck`); bereken de verwachte hash niet, laat de stub-`shasum` de pin-hash teruggeven.

- [ ] **Step 4: Draai en zie hem falen**

Run: `uv run pytest -q tests/pack/shell/test_shellcheck.py::test_download_verifies_with_shasum_when_sha256sum_is_missing`
Expected: FAIL (`sha256sum: command not found` of `sha256 mismatch`).

- [ ] **Step 5: Implementeer `_sc_sha256 <file>` in `shellcheck-bin.sh`**: `sha256sum` als die er is, anders `shasum -a 256`, anders rc 1 met `shellcheck: no sha256sum and no shasum` op stderr. Regel 67 roept hem aan. Zelfde patroon als `pack/core/scripts/check-instructions.sh:51-59`. In `pack/shell/requires.toml` wordt de regel `sha256sum` een alternatief (lees eerst hoe `requires.toml` alternatieven noteert; is er geen vorm voor, noteer `shasum` als tweede regel met dezelfde reden).

- [ ] **Step 6: Schrijf de test voor de BSD-tak van `usage-gate.sh`**: stub-`date` op `PATH` die rc 1 geeft op `-d` en voor `-j -f '%Y-%m-%dT%H:%M:%S%z' <ts> +%s` een vaste epoch print, en anders doorgeeft aan de echte `date`. Twee cases: `UNTIL=2026-11-07T12:00:00Z` (stub moet `…+0000` krijgen) en `UNTIL=2026-11-07T13:00:00+01:00` (stub moet `…+0100` krijgen).

```python
@pytest.mark.parametrize("until,bsd_arg", [
    ("2026-11-07T12:00:00Z", "2026-11-07T12:00:00+0000"),
    ("2026-11-07T13:00:00+01:00", "2026-11-07T13:00:00+0100"),
])
def test_usage_gate_parses_until_with_bsd_date_only(tmp_path, until, bsd_arg): ...
    # assert the stub saw bsd_arg, and the gate treats the flag as active (not expired)
```

Volg de opzet van de bestaande tests in `tests/pack/test_usage_gate_controller.py` voor de vlag en de aanroep. Deze test moet nu al groen zijn (de fallback bestaat): bewijs met een mutatie — zet regel 76 tijdelijk op `UNTIL_EPOCH=""` en zie hem rood worden.

- [ ] **Step 7: Markeer `usage-gate.sh:75`** met `# portable: BSD fallback on the next line`. Draai `uv run pytest -q tests/pack/test_portable_shell.py tests/pack/shell/test_shellcheck.py tests/pack/test_usage_gate_controller.py tests/pack/test_shell_style.py` — PASS.

- [ ] **Step 8: Dogfood**

Run: `go run ./cmd/lighthouse init --upgrade` en daarna `go run ./cmd/lighthouse init --upgrade </dev/null`
Expected: de tweede run print `No changes.`

- [ ] **Step 9: Commit** (twee commits: de verbod-test met markers, en de shasum-fix met zijn test)

```bash
git commit -m "test(pack): verbied GNU-only en bash-4-shell in pack/ (#90)"
git commit -m "fix(shell): verifieer de shellcheck-download ook met shasum (#90)"
```

---

### Task 3: Handmatige meting op een echte Mac en op Windows (handwerk)

Dit is een hand-taak: de uitvoerende agent bereidt het protocol voor en wacht op de uitkomst van de gebruiker. Hij voert geen stap zelf uit.

**Files:**
- Geen bestanden; uitkomst als comment op #90.

**Interfaces:**
- Consumes: de branch na Task 1 en 2 (binary via `go build` op de Mac, of release v0.9.0 + `lighthouse init` vanaf de branch-checkout).
- Produces: de keuze `native (Git Bash)`, `alleen WSL` of `nog niet` voor Task 4.

- [ ] **Step 1: Mac-protocol.** Per stap: commando, verwacht, gezien, ✅/❌.
  1. `sw_vers`, `/bin/bash --version`, `/usr/bin/python3 --version`, `which python3 uv go gh`.
  2. Schone repo met GitHub-remote (privé testrepo van de gebruiker), `lighthouse init`, akkoord geven.
  3. Worktree, één commit met een triviale wijziging: de test-gate draait (melding in de sessie).
  4. Herhaal 3 met een `host.toml` met `gate_min_mem_available_gb = 1`: verwacht `host memory not measurable` (bekend, `docs/test-gate.md:27`).
  5. Commit op `main`: `main-commit-gate` blokkeert.
  6. `/lighthouse-write-pr` (opvolger van `/lighthouse-verify-pr`) en `gh pr create`: `pr-create-gate` laat de body door.
  7. Elke andere hook-melding of `COULD NOT CHECK` letterlijk noteren.
- [ ] **Step 2: Windows-protocol.**
  1. Native, Git Bash: `lighthouse_windows_amd64.exe init` in een schone repo, één commit. Verwacht: Python-hooks falen op `import fcntl` (`pack/core/lib/gate_slot.py:24`, `pack/core/lib/bash_writes.py:22`) en `os.killpg` (`pack/core/lib/deadline.py`). Noteer de eerste fout letterlijk.
  2. WSL (Ubuntu): Linux-binary, dezelfde stappen 2-6 als op de Mac.
- [ ] **Step 3: Post de uitkomst** als één comment op #90 (Nederlands): tabel per stap per platform, de gekozen Windows-route met reden, en de nummers van nieuwe issues.
- [ ] **Step 4: Elke ❌** die klein is wordt een commit `fix(<scope>): … (#90)` met test; elke grote een issue. Native Windows wordt naar verwachting een issue "native Windows (fcntl, killpg, /proc)" in de milestone of een volgende.

---

### Task 4: Startgids en installatiedoc zeggen wat geldt

**Files:**
- Modify: `docs/install.md:8-10`
- Modify: `docs/site/agents/install.md:153-155`
- Modify: `docs/site/install.html:94-96` (blok `windows-amd64`)
- Create: `changelog.d/issue-90-macos-windows.md` (al in Task 1 stap 6; hier bijwerken)

**Interfaces:**
- Consumes: de Windows-route en de Mac-uitkomst uit Task 3.

- [ ] **Step 1: `docs/install.md:9`**: weg met "the hooks are untested there (`usage-gate.sh` uses GNU `date -d`)". Wordt: de hooks zijn op macOS getest (CI `macos-latest` plus een handmatige run, #90); geheugendrempels in `host.toml` werken alleen op Linux. Windows-zin volgens de route uit Task 3.
- [ ] **Step 2: `docs/site/agents/install.md` en `docs/site/install.html`**: de Windows-alinea noemt de gekozen route letterlijk (`WSL only` verwacht) en het issue voor native Windows. Geen andere wijziging aan die pagina's.
- [ ] **Step 3: Controleer links en de site-tests**

Run: `uv run pytest -q tests -k "site or install or md_link"`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git commit -m "docs(install): platformroute na meting op macOS en Windows (#90)"
```

- [ ] **Step 5: PR af**: `/lighthouse-write-pr` met `Closes #90` en een bewijsrij per AC (CI-runs, de comment uit Task 3, de issue-nummers, de docregels). Haal de draft-status weg.
