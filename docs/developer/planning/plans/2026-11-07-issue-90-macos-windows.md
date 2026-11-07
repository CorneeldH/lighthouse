# Issue #90 — lighthouse op macOS en Windows: implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Meten waar lighthouse buiten Linux breekt, het kleine oplossen, het grote als issue vastleggen, en de startgids laten zeggen welke route op Windows werkt.

**Architecture:** CI krijgt een `os`-matrix (`ubuntu-latest`, `macos-latest`) op de jobs `go-test`, `go-e2e` en `python`; de `run`-regels veranderen niet, alleen de `if:`-regel van `ci/literal-grep.sh` en de pin daarop in `tests/test_ci_domains_pin.py`. Een nieuwe Python-test verbiedt GNU-only en bash-4-constructies in de shell van `pack/`. De handmatige metingen op een echte Mac en op Windows volgen een vast protocol; hun uitkomst gaat als comment op #90 en bepaalt de tekst in de startgids (`docs/getting-started.md`) en de installatiedocs.

**Stoppunt:** Task 3 vraagt een mens met een echte Mac en Windows. De onbewaakte uitvoering (SDD) doet Task 1, Task 2 en Task 3 Step 0, en stopt daar met een handoff. Task 3 Step 1-4 en Task 4 draaien in een nieuwe sessie, pas als de gebruiker de uitkomsten op #90 heeft gezet.

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
4. **Hooks onder de systeem-`python3` 3.9 van macOS.** Lighthouse eist Python 3.11+ met `tomllib` (`docs/getting-started.md:17`). Verwacht: met alleen 3.9 op `PATH` melden de core-hooks `COULD NOT CHECK` en blokkeren de blokkerende; met 3.11+ (Homebrew of uv) draaien ze. CI dekt dat niet (uv pint een nieuwere Python). → Task 3, protocolstap met `python3 --version` en `/usr/bin/python3 --version` in de uitkomst.
5. **`usage-gate.sh` met een `UNTIL` als `…Z` en als `…+01:00` onder BSD `date`.** De fallback bestaat (`pack/core/hooks/usage-gate.sh:66-77`), maar `docs/install.md:9` zegt nog dat hij GNU-only is. → Task 2, test `test_usage_gate_parses_until_with_bsd_date_only`, en Task 4 haalt de docregel weg.

---

### Task 1: CI draait de Go- en Python-tests ook op `macos-latest`

**Files:**
- Modify: `.github/workflows/ci.yml` (jobs `python` regels 86-110, `go-test` regels 145-158, `go-e2e` regels 160-174)
- Test: `tests/test_ci_domains_pin.py` (nieuwe test, en `test_python_job_runs_in_three_shards` regel 81 aangepast)

**Interfaces:**
- Produces: CI-jobnamen `python (<os>, <shard>)`, `go-test (<os>)` en `go-e2e (<os>)`; Task 3 leest hun uitkomst.

- [ ] **Step 1: Schrijf de falende test** in `tests/test_ci_domains_pin.py`, met de bestaande helper `_jobs()`, en pas in `test_python_job_runs_in_three_shards` (regel 81) de `if:`-pin aan op de nieuwe regel:

```python
def test_tests_run_on_linux_and_macos():
    """Fails when the Go race tests, the e2e tests or the pytest suite stop running on macOS (#90)."""
    jobs = _jobs()
    for name in ("python", "go-test", "go-e2e"):
        job = jobs[name]
        assert "runs-on: ${{ matrix.os }}" in job, name
        assert re.search(r"^        os: \[ubuntu-latest, macos-latest\]\s*$", job, re.MULTILINE), name
```

```python
    assert re.match(
        r"if: matrix\.shard == 1 && matrix\.os == 'ubuntu-latest'\n        run: ci/literal-grep\.sh$",
        grep[0],
        re.MULTILINE,
    ), grep[0]
```

De asserts op `^      fail-fast: false` en `^        shard: \[1, 2, 3\]` blijven ongewijzigd: de matrix blijft in blok-stijl (Step 3).

- [ ] **Step 2: Draai hem en zie hem falen**

Run: `uv run pytest -q tests/test_ci_domains_pin.py::test_tests_run_on_linux_and_macos`
Expected: FAIL op `runs-on: ${{ matrix.os }}` voor `python`.

- [ ] **Step 3: Pas `ci.yml` aan.** Alles in blok-stijl, zes spaties voor `fail-fast:`, acht voor `os:` en `shard:`:

```yaml
    runs-on: ${{ matrix.os }}
    timeout-minutes: 15
    strategy:
      fail-fast: false
      matrix:
        os: [ubuntu-latest, macos-latest]
        shard: [1, 2, 3]
```

  - `python`: het blok hierboven (macOS-runners zijn trager; meet `timeout-minutes` en stel bij). De stap `ci/literal-grep.sh` krijgt `if: matrix.shard == 1 && matrix.os == 'ubuntu-latest'`.
  - `go-test` en `go-e2e`: hetzelfde blok zonder de regel `shard:`, `timeout-minutes` blijft 10 tenzij de macOS-run langer duurt. De `run`-regels blijven letterlijk.
  - `go`, `web`, `binary`, `secret-scan`, `changelog`, `changes`: ongewijzigd.
  - Werk de commentaarkop van `ci.yml` (regels 1-4) bij: Python, `go-test` en `go-e2e` draaien op Linux en macOS.

- [ ] **Step 4: Draai de CI-pins**

Run: `uv run pytest -q tests/test_ci_domains_pin.py tests/test_ci_go_test_pin.py`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/ci.yml tests/test_ci_domains_pin.py
git commit -m "ci: draai go-test en pytest ook op macos-latest (#90)"
```

- [ ] **Step 6: Push en open een draft-PR** om de eerste macOS-run te krijgen (CI draait alleen op `pull_request`). Body: `Refs #90 — draft voor de macOS-meting in CI; wordt Closes na de handmatige metingen`. Voeg eerst het changelog-fragment toe (zie Global Constraints), anders is de job `changelog` rood.

- [ ] **Step 7: Lees de macOS-jobs.** Per rode test één regel: test-id, foutregel, oorzaak, en `klein` of `groot`. Kleine fixes gaan naar Task 2 (shell) of een eigen commit `fix(<scope>): … (#90)` met een test die op Linux al faalt waar dat kan. Grote: issue via `/lighthouse-write-issue` (milestone "Product: eerste versie voor anderen") en een skip met het nummer. Herhaal tot de drie macOS-jobs groen zijn. Verwacht minstens: `tests/pack/test_shell_style.py` (shellcheck-download zonder `sha256sum`), tests rond `/proc`, `/run/user`, systemd-stubs, gitleaks.

---

### Task 2: GNU-only en bash-4-constructies in `pack/`

**Files:**
- Modify: `pack/shell/scripts/lib/shellcheck-bin.sh:67`
- Modify: `pack/shell/requires.toml:7` (regel `sha256sum` weg)
- Modify: `internal/packs/core_test.go:222` (`tools` zonder `sha256sum`)
- Modify: `docs/getting-started.md:30` (rij `sha256sum` in Requirements)
- Modify: `docs/site/agents/install.md:65` (rij `curl`, `sha256sum`, `tar`, `xz` in Look first)
- Create: `tests/pack/test_portable_shell.py`
- Modify: `tests/pack/shell/test_shellcheck.py` (één test erbij)
- Modify: `tests/pack/test_usage_gate_controller.py` (één test erbij)

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

Een `command -v sha256sum`-test (zoals `pack/core/scripts/check-instructions.sh:53`) is een fallback, geen gebruik: sta `command -v <tool>` toe in de regex. Elke andere regel binnen zo'n fallback die het woord `sha256sum` bevat (een toewijzing als `SHA_CMD="sha256sum"`, een aanroep in de eerste tak, een foutmelding als `printf 'FAIL  no sha256sum and no shasum …'`) krijgt `# portable: <reden>`. Bekend vooraf: `pack/core/scripts/check-instructions.sh:54` en `:58`.

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

- [ ] **Step 5: Implementeer `_sc_sha256 <file>` in `shellcheck-bin.sh`**: `sha256sum` als die er is, anders `shasum -a 256`, anders rc 1 met `shellcheck: no sha256sum and no shasum` op stderr. Regel 67 roept hem aan. Zelfde patroon als `pack/core/scripts/check-instructions.sh:51-59`. Elke regel in `_sc_sha256` met het woord `sha256sum` krijgt `# portable: fallback to shasum -a 256`.

  `requires.toml` kent geen alternatieven: `deps.Checker` doet één `LookPath` per sleutel (`internal/deps/deps.go:61`), en de sleutels zijn in beide richtingen gepind tegen de docs. Daarom:
  - `pack/shell/requires.toml`: haal de regel `sha256sum` weg. Eén van beide tools staat op elk ondersteund platform (coreutils op Linux, `shasum` op macOS); ontbreken ze allebei, dan meldt `_sc_sha256` dat bij de download.
  - `internal/packs/core_test.go:222`: `tools: "curl,git,jq,python3,tar,tomllib,xz"`.
  - `docs/getting-started.md:30`: haal de rij `sha256sum` weg en schrijf in de rij `curl` bij *Needed for*: "the first download of the pinned `shellcheck`, verified with sha256sum or shasum" (zonder backticks om die twee namen: `test_requirements_match_code_spans_exactly` eist dat elke code-span in de eerste kolom een requires-sleutel is; de andere kolommen telt hij niet, maar houd het gelijk).
  - `docs/site/agents/install.md:65`: de eerste cel wordt `` `curl`, `tar`, `xz` `` en de laatste cel noemt "sha256sum or shasum verifies it".
  - Draai `go test ./internal/packs/ ./internal/deps/` en `uv run pytest -q tests/test_getting_started.py tests/test_agent_guide.py`: PASS.

- [ ] **Step 6: Schrijf de test voor de BSD-tak van `usage-gate.sh`** in `tests/pack/test_usage_gate_controller.py`, naast `test_pause_bsd_date` (regel 214). Die bestaande test dekt de BSD-tak al met `BSD_DATE_STUB` (`tests/pack/_testkit.py:304`) en `_flag_text()`, maar alleen voor één offsetvorm en zonder het argument te controleren. De nieuwe test voegt twee dingen toe: de `Z`-vorm en de `+01:00`-vorm, en een assert op het exacte argument dat `date -j -f` krijgt.
  - Tijden altijd relatief aan nu, nooit een vaste datum: bouw `UNTIL` uit `datetime.now(timezone.utc) + timedelta(hours=1)`, één keer als `…Z` en één keer omgerekend naar `+01:00`. Een vaste datum verloopt en maakt de test rood met een correcte gate.
  - Stub: een kopie van `BSD_DATE_STUB` die eerst zijn argumenten naar een logbestand schrijft (`printf '%s\n' "$@" >> "$LOG"`), daarna hetzelfde doet. Assert dat het log de BSD-vorm bevat (`…+0000` voor `Z`, `…+0100` voor `+01:00`, zelf afgeleid van dezelfde `datetime` met `strftime('%Y-%m-%dT%H:%M:%S%z')`) en dat de gate de vlag als actief ziet (`_is_controller_reason(_reason(result))`, zoals `test_pause_bsd_date`).

```python
@pytest.mark.parametrize("form", ["Z", "+01:00"])
def test_usage_gate_parses_until_with_bsd_date_only(tmp_path, form): ...
```

Deze test moet nu al groen zijn (de fallback bestaat): bewijs met een mutatie — zet `BSD_UNTIL="$BSD_UNTIL+0000"` (`usage-gate.sh:70`) tijdelijk op `BSD_UNTIL="$BSD_UNTIL"` en zie de `Z`-case rood worden; zet daarna in regel 73 `${OFFSET%:*}${OFFSET#*:}` tijdelijk op `${OFFSET}` en zie de `+01:00`-case rood worden. Zet beide terug. Rood komt van de assert op het log: de GNU-`date` onder de stub parseert ook de gemuteerde vorm.

- [ ] **Step 7: Markeer `usage-gate.sh:75`** met `# portable: BSD fallback on the next line`. Draai `uv run pytest -q tests/pack/test_portable_shell.py tests/pack/shell/test_shellcheck.py tests/pack/test_usage_gate_controller.py tests/pack/test_shell_style.py tests/test_getting_started.py tests/test_agent_guide.py` en `go test ./internal/packs/` — PASS.

- [ ] **Step 8: Dogfood**

Run: eerst `go run ./cmd/lighthouse init --upgrade --yes` (schrijft de gewijzigde pack; zonder `--yes` vraagt `init` om akkoord en weigert bij lege stdin, `internal/initcmd/initcmd.go:210`), daarna `go run ./cmd/lighthouse init --upgrade </dev/null`
Expected: de tweede run print `No changes.`

- [ ] **Step 9: Commit**, in deze volgorde, elk met de dogfood-kopie van wat hij onder `pack/` wijzigt. De verbod-test komt als laatste: committed vóór de fix maakt de test-gate rood op `shellcheck-bin.sh:67`.

```bash
git commit -m "fix(shell): verifieer de shellcheck-download ook met shasum (#90)"   # shellcheck-bin.sh, requires.toml, core_test.go, beide docs, test_shellcheck.py
git commit -m "test(pack): BSD date-tak van usage-gate met Z en offset (#90)"      # test_usage_gate_controller.py
git commit -m "test(pack): verbied GNU-only en bash-4-shell in pack/ (#90)"         # test_portable_shell.py en de markers
```

---

### Task 3: Handmatige meting op een echte Mac en op Windows (handwerk)

Dit is een hand-taak: een mens met een echte Mac en een Windows-machine (met WSL en Git Bash) voert Step 1 en 2 uit. Geen agent kan dat. De onbewaakte uitvoering doet alleen Step 0 en stopt dan.

**Files:**
- Geen bestanden; protocol en uitkomst als comments op #90.

**Interfaces:**
- Consumes: de gepushte branch na Task 1 en 2. De binary komt altijd uit `go build ./cmd/lighthouse` op die branch: de pack zit bij de build in de binary (`pack/pack.go:12`, `//go:embed`), dus een release-binary zoals v0.9.0 test de hooks van deze branch niet.
- Produces: de keuze `native (Git Bash)`, `alleen WSL` of `nog niet` voor Task 4.

- [ ] **Step 0: Protocol klaarzetten en stoppen (onbewaakt).** Post Step 1 en 2 hieronder als één comment op #90 (Nederlands), met bovenaan de branch, de commit-SHA van `HEAD` en het build-commando `git checkout issue-90-macos-windows && go build -o lighthouse ./cmd/lighthouse` (Windows native: `GOOS=windows GOARCH=amd64 go build -o lighthouse.exe ./cmd/lighthouse`, of de build op de Windows-machine zelf). Vraag de gebruiker de uitkomst als comment op #90 te zetten. Stop dan de uitvoering: `/lighthouse-write-handoff` met als volgende stap Task 3 Step 3 van dit plan, gegated op die comment. Task 3 Step 1-4 en Task 4 komen niet in deze run; verzin geen uitkomst.

- [ ] **Step 1: Mac-protocol (mens).** Per stap: commando, verwacht, gezien, ✅/❌. Noteer de commit-SHA van de gebouwde binary (`./lighthouse version`).
  1. `sw_vers`, `/bin/bash --version`, `python3 --version`, `/usr/bin/python3 --version`, `which python3 uv go gh`. Verwacht: met alleen Python 3.9 op `PATH` melden de core-hooks `COULD NOT CHECK` en blokkeren de blokkerende (Review Focus 4); installeer dan Python 3.11+ en ga door.
  2. Schone repo met GitHub-remote (privé testrepo van de gebruiker), `lighthouse init`, akkoord geven.
  3. Worktree, één commit met een triviale wijziging: de test-gate draait (melding in de sessie).
  4. Herhaal 3 met een `host.toml` met `gate_min_mem_available_gb = 1`: verwacht `host memory not measurable` (bekend, `docs/test-gate.md:27`).
  5. Commit op `main`: `main-commit-gate` blokkeert.
  6. `/lighthouse-write-pr` (opvolger van `/lighthouse-verify-pr`) en `gh pr create`: `pr-create-gate` laat de body door.
  7. Elke andere hook-melding of `COULD NOT CHECK` letterlijk noteren.
- [ ] **Step 2: Windows-protocol (mens).**
  1. Native, Git Bash: de branch-build `lighthouse.exe init` in een schone repo, één commit. Verwacht: Python-hooks falen op `import fcntl` (`pack/core/lib/gate_slot.py:24`, `pack/core/lib/bash_writes.py:22`) en `os.killpg` (`pack/core/lib/deadline.py`). Noteer de eerste fout letterlijk.
  2. WSL (Ubuntu): Linux-build van de branch, dezelfde stappen 2-6 als op de Mac.
- [ ] **Step 3: Uitkomst vastleggen (nieuwe sessie).** Begin pas als de gebruiker de uitkomst op #90 heeft gezet; staat die er niet, stop dan. Zet die als één comment op #90 (Nederlands) als dat nog niet zo is: tabel per stap per platform, de gekozen Windows-route met reden, en de nummers van nieuwe issues.
- [ ] **Step 4: Elke ❌** die klein is wordt een commit `fix(<scope>): … (#90)` met test; elke grote een issue. Native Windows wordt naar verwachting een issue "native Windows (fcntl, killpg, /proc)" in de milestone of een volgende.

---

### Task 4: Startgids en installatiedoc zeggen wat geldt

Draait in de nieuwe sessie, na Task 3 Step 3.

**Files:**
- Modify: `docs/getting-started.md` (de startgids uit AC 3: sectie Requirements of de alinea "The other platforms…" bij regel 60-62)
- Modify: `docs/install.md:8-10`
- Modify: `docs/site/agents/install.md:153-155`
- Modify: `docs/site/install.html:94-96` (blok `windows-amd64`)
- Create: `changelog.d/issue-90-macos-windows.md` (al in Task 1 stap 6; hier bijwerken)

**Interfaces:**
- Consumes: de Windows-route en de Mac-uitkomst uit Task 3.

- [ ] **Step 1: `docs/install.md:9`**: weg met "the hooks are untested there (`usage-gate.sh` uses GNU `date -d`)". Wordt: de hooks zijn op macOS getest (CI `macos-latest` plus een handmatige run, #90); geheugendrempels in `host.toml` werken alleen op Linux. Windows-zin volgens de route uit Task 3.
- [ ] **Step 2: `docs/site/agents/install.md` en `docs/site/install.html`**: de Windows-alinea noemt de route uit Task 3 letterlijk en het issue voor native Windows. Geen andere wijziging aan die pagina's in deze task (de rij in Look first is al in Task 2 gewijzigd). Houd het woord `WSL` in *Install the binary* (`tests/test_agent_guide.py:137`).
- [ ] **Step 2b: `docs/getting-started.md`** (de startgids, AC 3): één zin bij de platformen (regel 60-62) die zegt dat macOS getest is en welke route op Windows werkt, met het issue voor native Windows. Raak de Requirements-tabel niet aan buiten wat Task 2 deed.
- [ ] **Step 3: Controleer links en de doc-tests**

Run: `uv run pytest -q tests/test_getting_started.py tests/test_agent_guide.py tests/test_site_*.py && uv run pytest -q tests -k "install or md_link"`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git commit -m "docs(install): platformroute na meting op macOS en Windows (#90)"
```

- [ ] **Step 5: PR af**: `/lighthouse-write-pr` met `Closes #90` en een bewijsrij per AC (CI-runs, de comment uit Task 3, de issue-nummers, de docregels). Haal de draft-status weg.
