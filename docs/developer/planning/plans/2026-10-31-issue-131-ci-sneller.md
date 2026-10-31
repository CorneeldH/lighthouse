# CI sneller (#131) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Een PR die Go en web raakt, is in minder dan 160 s door CI: npm-cache, `binary` los van `web`, en de `go`-job gesplitst op basis van een meting.

**Architecture:** Alles gebeurt in `.github/workflows/ci.yml`, met de pin-tests in `tests/test_ci_domains_pin.py`. Eerst gaan de twee wijzigingen zonder ontwerpkeuze erin (npm-cache, `binary` zonder `needs: web`). Daarna draait een tijdelijke meetstap op een draft-PR. De uitkomst gaat als comment op het issue. Pas daarna wordt `go` gesplitst, volgens de beslisregel in taak 4.

**Tech Stack:** GitHub Actions, pytest (pin-tests op de tekst van `ci.yml`), `gh` + `jq` voor de meting.

**Spec:** issue #131 (geen apart spec-document; de acceptatiecriteria van het issue zijn de spec).

## Global Constraints

- Elke `uses:` staat op een SHA van 40 hex-tekens met `# vX.Y.Z` erachter; `test_actions_pinned_by_sha` telt ze (nu `16`).
- Elke job heeft `timeout-minutes` en een checkout met `persist-credentials: false`.
- Elke job die uit `go` ontstaat, heeft `needs: changes` en `if: needs.changes.outputs.go == 'true'`.
- `tests/test_ci_go_test_pin.py` blijft ongewijzigd en groen. Er blijven dus precies twee regels `- run: go test …` in het hele bestand, elk met `-race`.
- Bij `golangci/golangci-lint-action` blijft `version:` de eerste sleutel onder `with:` (`tests/test_golangci_pin.py`).
- Commentaar in `ci.yml` is Engels. Commits, het PR en de issue-comment zijn Nederlands.
- Een commit die `ci.yml` raakt, laat de commit-gate ook de web-checks draaien. Draai eerst `(cd web && npm ci)`, anders eindigt de gate met exit 3 (`COULD NOT CHECK`).
- `concurrency` heeft `cancel-in-progress: true`. Push pas weer als de vorige run klaar is, anders wordt de meetrun afgebroken.
- `gh`-aanroepen draaien met `dangerouslyDisableSandbox: true`.

## Review Focus

1. Een PR die alleen web raakt: `binary` draait nog (`outputs.web == 'true'`) en bouwt de SPA zelf. Gepind in taak 2 (`test_binary_runs_without_web`).
2. Een PR die alleen Go raakt: `web` wordt overgeslagen en `binary` draait toch. Vroeger regelde `!cancelled()` dat; zonder `needs: web` is het niet meer nodig. Gepind in taak 2: `binary` noemt `needs.web` nergens.
3. Een gesplitste Go-job zonder de `if` draait ook voor een PR met alleen docs, en dan geldt #130 niet meer. Gepind in taak 4 (`test_go_jobs_wait_for_changes`).
4. Een stap die bij het splitsen wegvalt (cross-build, pack-match, lint). Gepind in taak 4 (`test_go_checks_stay_in_go_jobs`).
5. De meetstap blijft na de splitsing staan en kost dan elke run tijd. Taak 4 haalt hem weg; `test_no_measurement_step` pint dat.

---

### Task 1: npm-cache in `web` en `binary`

**Files:**
- Modify: `.github/workflows/ci.yml:113-115` (setup-node in `web`), `:147-149` (setup-node in `binary`)
- Test: `tests/test_ci_domains_pin.py`

**Interfaces:**
- Produces: beide setup-node-stappen hebben `cache: npm` en `cache-dependency-path: web/package-lock.json`.

- [ ] **Step 1: Schrijf de falende test** in `tests/test_ci_domains_pin.py`:

```python
def test_node_jobs_cache_npm():
    """Fails when web or binary stops caching the npm download for npm ci."""
    jobs = _jobs()
    for name in ("web", "binary"):
        steps = re.split(r"(?m)^      - ", jobs[name])[1:]
        setup = [s for s in steps if "actions/setup-node" in s]
        assert len(setup) == 1, (name, setup)
        assert re.search(r"^\s+cache: npm\s*$", setup[0], re.MULTILINE), name
        assert "cache-dependency-path: web/package-lock.json" in setup[0], name
```

- [ ] **Step 2: Draai hem, verwacht FAIL**

Run: `uv run pytest -q tests/test_ci_domains_pin.py::test_node_jobs_cache_npm`
Expected: FAIL op `web`.

- [ ] **Step 3: Voeg `cache: npm` en `cache-dependency-path: web/package-lock.json` toe** onder `with:` van beide setup-node-stappen, na `node-version-file`.

- [ ] **Step 4: Draai de pin-tests, verwacht PASS**

Run: `uv run pytest -q tests/test_ci_domains_pin.py tests/test_ci_go_test_pin.py tests/test_golangci_pin.py`
Expected: alles groen (`uses:`-telling blijft `16`).

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/ci.yml tests/test_ci_domains_pin.py
git commit -m "ci(web): npm-cache in web en binary (#131)"
```

### Task 2: `binary` start tegelijk met `web`

**Files:**
- Modify: `.github/workflows/ci.yml:128-154` (`binary`), `:122-126` (upload van `web-dist` in `web`), `:1-3` (kopcommentaar)
- Test: `tests/test_ci_domains_pin.py:69-81` (`test_binary_builds_after_web` wordt vervangen)

**Interfaces:**
- Consumes: taak 1 (setup-node met cache in `binary`).
- Produces: `binary` met alleen `needs: changes`. Na deze taak staan er `14` `uses:`-regels.

- [ ] **Step 1: Vervang `test_binary_builds_after_web`** door:

```python
def test_binary_runs_without_web():
    """Fails when binary waits for web again, or stops building the SPA, the binary or the smoke."""
    binary = _jobs()["binary"]
    assert re.search(r"^    needs: changes\s*$", binary, re.MULTILINE)
    assert "needs.web" not in binary
    for needle in (
        "outputs.web == 'true'",
        "outputs.go == 'true'",
        "CGO_ENABLED=0 go build ./cmd/lighthouse",
        "ci/spa-smoke.sh",
    ):
        assert needle in binary, needle
    steps = re.split(r"(?m)^      - ", binary)[1:]
    build = [s for s in steps if re.search(r"run: npm run build\s*$", s, re.MULTILINE)]
    assert len(build) == 1 and not re.search(r"^\s*if:", build[0], re.MULTILINE), build
    assert binary.index("run: npm ci") < binary.index("run: npm run build") < binary.index("go build ./cmd/lighthouse")
    assert "web-dist" not in _text()
```

Zet in `test_actions_pinned_by_sha` de telling op `14`.

- [ ] **Step 2: Draai hem, verwacht FAIL**

Run: `uv run pytest -q tests/test_ci_domains_pin.py`
Expected: FAIL in `test_binary_runs_without_web` (needs) en `test_actions_pinned_by_sha` (16 ≠ 14).

- [ ] **Step 3: Pas `ci.yml` aan**
  - `binary`: `needs: changes`, `if: needs.changes.outputs.web == 'true' || needs.changes.outputs.go == 'true'`. Weg: de stap `download-artifact` en de `if:` op `npm run build`.
  - `web`: de stap `upload-artifact` `web-dist` weg; niemand leest hem meer. `npm run build` blijft staan als check.
  - Het commentaar boven `binary` en het kopcommentaar (`:1-3`) zeggen dat `binary` de SPA zelf bouwt en niet op `web` wacht.

- [ ] **Step 4: Draai de pin-tests, verwacht PASS**

Run: `uv run pytest -q tests/test_ci_domains_pin.py tests/test_ci_go_test_pin.py tests/test_golangci_pin.py`
Expected: alles groen.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/ci.yml tests/test_ci_domains_pin.py
git commit -m "ci(binary): bouwt de SPA zelf en wacht niet meer op web (#131)"
```

### Task 3: Meting van de `go`-job (vóór de splitsing)

**Files:**
- Modify: `.github/workflows/ci.yml` (job `go`: tijdelijke meetstap)
- Create: `changelog.d/issue-131-ci-sneller.md` (nodig om de `changelog`-job groen te krijgen)

**Interfaces:**
- Produces: issue-comment met de meetwaarden die taak 4 gebruikt: `M_lint`, `M_build` (linux + windows + darwin), `M_race` (compile), `M_e2e_compile`, `M_test` (de `go test`-stap na de warme compile), `M_e2e` (de e2e-stap), `M_pack`, `M_setup` (checkout + setup-go), en per cache-stap: key, ref, grootte, hit/miss.

- [ ] **Step 1: Voeg een tijdelijke meetstap toe** in `go`, direct na golangci-lint. Gebruik de `run: |`-vorm, zodat de regex van `test_ci_go_test_pin.py` hem niet telt:

```yaml
      # Temporary measurement for #131: compile time with -race apart from test time.
      - name: measure race compile (temporary, #131)
        run: |
          time go test -race -run '^$' ./...
          time go test -race -tags e2e -run '^$' ./e2e/
```

- [ ] **Step 2: Schrijf het changelog-fragment** `changelog.d/issue-131-ci-sneller.md`, met `type: internal` (formaat: [`docs/releasing.md`](../../../releasing.md)):

```markdown
---
type: internal
---
CI caches the npm download, builds the binary job without waiting for the web job, and splits the Go job into parallel jobs.
```

- [ ] **Step 3: Draai de pin-tests en commit**

Run: `uv run pytest -q tests/test_ci_domains_pin.py tests/test_ci_go_test_pin.py tests/test_golangci_pin.py` → groen.

```bash
git add .github/workflows/ci.yml changelog.d/issue-131-ci-sneller.md
git commit -m "ci(go): tijdelijke meetstap voor de race-compileertijd (#131)"
```

- [ ] **Step 4: Push en open een draft-PR**

```bash
git push -u origin issue-131-ci-sneller
gh pr create --draft --title "CI sneller: npm-cache, binary los van web, go gesplitst" \
  --body "Refs #131 — meetrun voor de splitsing van go; de body volgt via /lighthouse-verify-pr vóór ready-for-review."
```

Een push op `ci.yml` zet `web`, `go` en `python` alle drie op `true` (`ci/domains.py` `_WEB_TOO`). Deze run raakt dus Go en web.

- [ ] **Step 5: Haal de meetwaarden op** zodra de run klaar is (`gh run watch`):

```bash
R=$(python3 .lighthouse/lib/lighthouse_lib/config.py forge.repo)
RUN=$(gh run list --branch issue-131-ci-sneller --workflow ci --limit 1 --json databaseId --jq '.[0].databaseId')
# Stapduur per job:
gh api "repos/$R/actions/runs/$RUN/jobs" --jq '.jobs[] | select(.conclusion != "skipped") | .name as $j | .steps[] | "\($j)\t\(.name)\t\((.completed_at|fromdate)-(.started_at|fromdate))"'
# De twee `time`-regels van de meetstap, en wat elke cache-stap herstelt:
gh run view "$RUN" --log --job <go-job-id> | grep -E '^real|[Cc]ache (restored|not found|hit|Size)|[Cc]ache key|Received'
# Key, ref en grootte van de caches:
gh api "repos/$R/actions/caches" --jq '.actions_caches[] | "\(.key)\t\(.ref)\t\(.size_in_bytes)"'
```

- [ ] **Step 6: Post de meting als comment op #131**, in het Nederlands: een tabel met elke `M_*`-waarde, de caches (key, ref, grootte, hit/miss per stap) en het run-id. Sluit af met de indeling die de beslisregel van taak 4 eruit haalt.

```bash
gh issue comment 131 --body-file <scratchpad>/meting-131.md
```

### Task 4: `go` splitsen volgens de meting

**Files:**
- Modify: `.github/workflows/ci.yml:68-99` (job `go` en de nieuwe jobs; meetstap weg)
- Test: `tests/test_ci_domains_pin.py`

**Interfaces:**
- Consumes: de `M_*`-waarden uit de comment van taak 3.
- Produces: jobs met de naam `go` of `go-<deel>`. De job `go` blijft bestaan (`test_domain_jobs_wait_for_changes` leest hem).

**Beslisregel.** Elke Go-job duurt hoogstens 120 s (`M_setup` meegerekend). Dan blijft er ruimte voor `changes` (≈15 s) en de wachtrij binnen 160 s. Uitgangspunt:

| Job | Stappen |
|---|---|
| `go` | golangci-lint, `CGO_ENABLED=0 go build ./...`, windows- en darwin-build, pack-match |
| `go-test` | `go test -race -shuffle=on ./...`, setup-uv, e2e |

- Is `M_setup + M_race + M_test + M_e2e > 120`? Zet de e2e (met setup-uv) dan in een eigen job `go-e2e`. Anders blijft hij in `go-test`: daar hergebruikt hij de race-compile van de dependencies, en een eigen job zou die opnieuw doen.
- Is `M_setup + M_lint + M_build + M_pack > 120`? Zet de windows- en darwin-build dan in een eigen job `go-cross`.
- Is `M_setup + M_race + M_test` alleen al groter dan 120? Dan haalt splitsen de 160 s niet. Stop, zet dat in de issue-comment en vraag de gebruiker om een keuze (bijvoorbeeld een cache-opwarming op `main` als vervolg-issue). Ga niet zelf verder.

Het commentaar boven de Go-jobs in `ci.yml` noemt de indeling, de gemeten waarden en de link naar de comment. Voorbeeld: `# Split per the measurement in #131 (issuecomment-<id>): race compile 55 s, tests 24 s, ...`.

- [ ] **Step 1: Schrijf de falende tests** in `tests/test_ci_domains_pin.py`:

```python
def _go_jobs() -> dict[str, str]:
    return {name: body for name, body in _jobs().items() if name == "go" or name.startswith("go-")}


def test_go_jobs_wait_for_changes():
    """Fails when a job split off from go runs without the go output of the changes job."""
    jobs = _go_jobs()
    assert len(jobs) >= 2, sorted(jobs)
    for name, body in jobs.items():
        assert re.search(r"^    needs: changes\s*$", body, re.MULTILINE), name
        assert "if: needs.changes.outputs.go == 'true'" in body, name


def test_go_checks_stay_in_go_jobs():
    """Fails when a Go check drops out of every go job while the go jobs are split."""
    go = "\n".join(_go_jobs().values())
    for needle in (
        "golangci/golangci-lint-action@",
        "- run: CGO_ENABLED=0 go build ./...",
        "- run: GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...",
        "- run: GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build ./...",
        "- run: go test -race -shuffle=on ./...",
        "- run: go test -race -shuffle=on -tags e2e ./e2e/ -v",
        "name: installed pack/core matches pack/",
    ):
        assert needle in go, needle


def test_no_measurement_step():
    """Fails when the temporary #131 measurement step stays in CI."""
    assert "-run '^$'" not in _text()
```

Zet de telling in `test_actions_pinned_by_sha` op `14` plus de `uses:`-regels van de nieuwe jobs. Elke nieuwe job krijgt checkout en setup-go (+2); `go-e2e` krijgt ook setup-uv (+3 in totaal). Setup-uv verhuist mee met de e2e en telt daarom niet dubbel.

- [ ] **Step 2: Draai ze, verwacht FAIL**

Run: `uv run pytest -q tests/test_ci_domains_pin.py`
Expected: FAIL in `test_go_jobs_wait_for_changes` (één Go-job) en `test_no_measurement_step`.

- [ ] **Step 3: Splits `go`** volgens de beslisregel en haal de meetstap weg. Elke nieuwe job kopieert de kop van `go` (`needs`, `if`, `runs-on`, `timeout-minutes: 10`, checkout, setup-go). Het commentaar `# Same flags as the Go part of [test_gate] command` verhuist mee met de `go test`-stap, en het e2e-commentaar met setup-uv. Pas ook het kopcommentaar van `ci.yml` aan.

- [ ] **Step 4: Draai alle pin-tests, verwacht PASS**

Run: `uv run pytest -q tests/test_ci_domains_pin.py tests/test_ci_go_test_pin.py tests/test_golangci_pin.py tests/test_domains.py`
Expected: alles groen. `git diff main -- tests/test_ci_go_test_pin.py` is leeg.

- [ ] **Step 5: Commit en push**

```bash
git add .github/workflows/ci.yml tests/test_ci_domains_pin.py
git commit -m "ci(go): go gesplitst in parallelle jobs volgens de meting (#131)"
git push
```

### Task 5: Bewijs op twee runs en PR-body

**Files:** geen wijziging, tenzij de 160 s niet gehaald wordt (dan terug naar de beslisregel van taak 4).

- [ ] **Step 1: Run 1 is de run van de push uit taak 4.** Meet de totale duur, van de start van `changes` tot het einde van de laatste job:

```bash
gh api "repos/$R/actions/runs/$RUN/jobs" --jq '(.jobs[] | select(.name == "changes") | .started_at | fromdate) as $s | ([.jobs[] | select(.conclusion != "skipped") | .completed_at | fromdate] | max) - $s'
```

- [ ] **Step 2: Run 2 is een herhaling van dezelfde run** (`gh run rerun "$RUN"`; wacht met `gh run watch`). Meet opnieuw de totale duur, en de stapduur van `npm ci` in `web` en `binary` (de jq van taak 3, step 5). Verwacht: `npm ci` < 36 s in beide jobs, en beide runs < 160 s.

- [ ] **Step 3: Haalt een run de 160 s niet?** Stop en meld de getallen. Taak 4 heeft dan een verkeerde indeling gekozen of de schatting klopt niet; de gebruiker beslist.

- [ ] **Step 4: Schrijf de PR-body met `/lighthouse-verify-pr`** (`Closes #131` met een evidence-tabel: één rij per AC, met run-id's en duur). Zet het PR daarna op ready (`gh pr ready`).
