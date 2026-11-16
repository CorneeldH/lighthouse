# Web-UI in de module-source van een tag — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `go install …@<tag>` levert dezelfde binary als de release, met de gebouwde SPA erin (#349).

**Architecture:** Een nieuw script `ci/tag-with-ui.sh` zet een gebouwde `dist` in een tweede commit (C2) boven de changelog-commit (C1) en tagt C2. `release.yml` en een nieuwe `prerelease.yml` bouwen `dist` in een aparte leesrechten-job `ui`, geven het als artifact door, en de schrijvende job draait het script en pusht. `build.yml` bouwt de UI niet meer apart voor een artifact maar controleert dat de `dist` in de tag past bij `web/`, en bewijst met een nieuwe job `go-install` dat `go install …@<tag>` de SPA serveert.

**Tech Stack:** bash, GitHub Actions-YAML, pytest-pintests op de workflowtekst, een pytest-test die het script in een tijdelijke git-repo draait.

**Spec:** [2026-11-16-issue-349-go-install-ui-design.md](../specs/2026-11-16-issue-349-go-install-ui-design.md)

## Global Constraints

- Actions blijven gepind op een 40-hex SHA met `# vX.Y.Z`; gebruik exact de SHA's die `build.yml` nu heeft (`checkout`, `setup-node`, `setup-go`, `upload-artifact`, `download-artifact`).
- Elke `actions/checkout` heeft `persist-credentials: false`, behalve de checkout in de jobs die pushen (`release.yml` job `release`, `prerelease.yml` job `tag`): daar `true`.
- `npm ci` en `npm run build` draaien nooit in een job met schrijfrechten of een schrijftoken; de schrijvende jobs hebben geen `npm` en geen `setup-node`.
- Geen `${{ … }}` in een `run:`-blok; waarden gaan via `env:` (`test_no_inputs_in_run_scripts`).
- `ci/tag-with-ui.sh <tag> <dist-bron>`: bouwt niet, pusht niet, `<dist-bron>` is een map buiten de worktree; faalt (exit ≠ 0) zonder `<dist-bron>/index.html`; commitbericht `chore(release): <tag> web-UI`; geannoteerde tag `<tag>` op die commit.
- Artifact van de UI-build: upload met `include-hidden-files: true` (het getrackte `dist/.gitkeep` hoort bij de build-output).
- Een prerelease-tag heeft de vorm `vX.Y.Z-<suffix>`; `prerelease.yml` weigert alles anders en pusht alleen `refs/tags/$tag`.
- `git push --atomic origin "$main_sha:refs/heads/main" "refs/tags/$tag"` in `release.yml`.
- `go-install` gebruikt `GOPRIVATE=github.com/CorneeldH/lighthouse`, geen globale `GOPROXY=direct`.
- Commentaar in workflows/scripts en het changelog-fragment in het Engels (`code_language = en`); commits, docs en dit plan in het Nederlands, Conventional Commits. Elke commit groen op `[test_gate] command`; nooit `--no-verify`.

## Review Focus

1. **Retry-lus van `release`**: na een mislukte push is HEAD C2 en `git reset --hard origin/main` verwijdert het gevolgde `dist`; het artifact staat buiten de worktree en het script kopieert opnieuw. Spec-punt `r3-f1` ("accepted open") is hiermee gedekt. Test in taak 1 (`test_second_run_after_reset_works`).
2. **Dotfile `.gitkeep` valt uit het artifact** (`upload-artifact` slaat verborgen bestanden standaard over): de herbouwde tree wijkt dan af en `web` faalt op elke tag. Pin in taak 3 (`test_ui_job_uploads_hidden_files`).
3. **Schrijvende job krijgt npm terug** (iemand verplaatst de build): pin in taak 3 en 4 (`test_writing_jobs_run_no_npm`).
4. **`prerelease.yml` zet een gewone release-tag** (`v1.2.3`) of een tag die al bestaat: de weigering en de bestaande-tag-controle in taak 4.
5. **Script draait in een worktree met een gewijzigde of vuile index**: `git add -f` raakt alleen `internal/web/dist`; test in taak 1 met een al aanwezige `dist` van een eerdere run.

---

### Task 1: `ci/tag-with-ui.sh`

**Files:**
- Create: `ci/tag-with-ui.sh` (uitvoerbaar, `git update-index --chmod=+x`)
- Create: `tests/test_tag_with_ui.py`

**Interfaces:**
- Produces: `ci/tag-with-ui.sh <tag> <dist-bron>` — draait vanuit de root van de worktree (cwd); laat HEAD op C2 met tag `<tag>` erop; exit 0 bij succes. Taak 3 en 4 roepen hem zo aan.

- [ ] **Step 1: Schrijf de falende tests in `tests/test_tag_with_ui.py`**

Helper `_repo(tmp_path)`: `git init`, user.name/email ingesteld, één commit met een bestand, zonder `internal/web/dist`; helper `_src(tmp_path)`: map met `index.html` en `assets/app.js`. Draai het script met `subprocess.run([str(SCRIPT), tag, str(src)], cwd=repo)`.

- `test_tag_points_to_commit_with_dist`: na het script bevat `git ls-tree -r <tag> --name-only` `internal/web/dist/index.html` en `internal/web/dist/assets/app.js`; `git cat-file -t <tag>` is `tag` (geannoteerd); het onderwerp van `<tag>^{commit}` is `chore(release): v1.2.3 web-UI`.
- `test_parent_has_no_dist`: `git ls-tree -r <tag>^ --name-only` bevat geen pad met `internal/web/dist`.
- `test_fails_without_index_html`: bron zonder `index.html` → returncode ≠ 0, geen tag (`git tag --list` leeg), HEAD ongewijzigd.
- `test_second_run_after_reset_works`: draai het script, `git tag -d <tag>`, `git reset --hard HEAD~1` (dist weg uit de tree), draai opnieuw met dezelfde bron → returncode 0 en de tag wijst weer naar een commit met `index.html`.
- `test_ignored_dist_is_forced`: schrijf `.gitignore` met `/internal/web/dist/*` en commit dat; het script voegt `dist` toch toe (zelfde assert als de eerste test).
- `test_script_is_tracked_executable`: `git ls-files -s ci/tag-with-ui.sh` begint met `100755` (patroon van de bestaande spa-smoke-pin).

- [ ] **Step 2: Draai ze, verwacht FAIL**

Run: `uv run pytest tests/test_tag_with_ui.py -q`
Expected: FAIL (script ontbreekt).

- [ ] **Step 3: Schrijf `ci/tag-with-ui.sh`**

`set -euo pipefail`, kop-commentaar zoals `ci/spa-smoke.sh` (usage, wat het doet, pusht niet). Stappen: argumentcheck (precies 2), `[ -f "$src/index.html" ]` anders `fail`, `rm -rf internal/web/dist`, `mkdir -p`, `cp -R "$src"/. internal/web/dist/`, `git add -f internal/web/dist`, `git commit -m "chore(release): $tag web-UI"`, `git tag -a "$tag" -m "$tag"`. De bestaande `.gitkeep` hoeft niet apart behandeld: het artifact draagt hem (zie Global Constraints). Volg de shell-standaard (`pack/shell/skills/shell-standard/agents/SKILL.md`) voor quoting en foutmeldingen; `fail()` zoals in `spa-smoke.sh`.

- [ ] **Step 4: Draai de tests, verwacht PASS**

Run: `chmod +x ci/tag-with-ui.sh && git update-index --add --chmod=+x ci/tag-with-ui.sh; uv run pytest tests/test_tag_with_ui.py -q`
Expected: 6 passed. Draai ook `shellcheck ci/tag-with-ui.sh` als shellcheck beschikbaar is.

- [ ] **Step 5: Commit**

```bash
git add ci/tag-with-ui.sh tests/test_tag_with_ui.py
git commit -m "feat(ci): tag-with-ui.sh zet dist in de tag-commit (#349)"
```

---

### Task 2: `build.yml` controleert de tag en bewijst `go install`

**Files:**
- Modify: `.github/workflows/build.yml` (kop-commentaar; job `web`; job `build`; nieuwe job `go-install`; job `publish`)
- Modify: `tests/test_release_workflows_pin.py`

**Interfaces:**
- Consumes: `ci/spa-smoke.sh <binary>` (bestaat).
- Produces: `build.yml` verwacht een tag waarin `internal/web/dist/index.html` getrackt is. Taak 3 en 4 maken zulke tags.

- [ ] **Step 1: Pas de pin-tests aan (falend)**

In `tests/test_release_workflows_pin.py`:
- Verwijder `test_build_embeds_real_spa`; vervang door `test_web_job_checks_dist_of_the_tag`: job `web` bevat in deze volgorde `git ls-files --error-unmatch internal/web/dist/index.html`, `npm ci`, `npm run build` (elk als eigen `run:`-stap, de npm-stappen met `working-directory: web`), en `test -z "$(git status --porcelain --ignored -- internal/web/dist)"`; `contents: read`, geen `contents: write`, geen `upload-artifact`.
- `test_build_has_no_spa_artifact`: job `build` bevat geen `download-artifact` en niet `spa-dist`; `needs: [resolve, web]` blijft; de stap `git status --porcelain` staat vóór `go build`.
- `test_go_install_job_proves_spa`: `jobs["go-install"]` heeft `needs: [resolve, web]`, `contents: read`, checkout met `ref: refs/tags/${{ needs.resolve.outputs.tag }}` en `persist-credentials: false`, `actions/setup-go@`, `go-version-file: go.mod`, en een `run:` die `GOPRIVATE=github.com/CorneeldH/lighthouse`, `GOBIN="$RUNNER_TEMP/bin"` en `go install github.com/CorneeldH/lighthouse/cmd/lighthouse@$TAG` bevat, daarna `ci/spa-smoke.sh "$RUNNER_TEMP/bin/lighthouse"`; `GOPROXY` komt in geen enkel workflowbestand voor.
- `test_publish_needs_go_install`: `publish` heeft `needs: [resolve, build, go-install]`.
- `test_build_checkouts_pin_the_tag`: `found == 4`.
- `test_build_runs_spa_smoke` blijft.

- [ ] **Step 2: Draai, verwacht FAIL**

Run: `uv run pytest tests/test_release_workflows_pin.py -q`
Expected: FAIL op de nieuwe en gewijzigde tests.

- [ ] **Step 3: Pas `build.yml` aan**

Job `web`: vervang de upload-stap door de drie controles uit de spec (`Verify dist is tracked`, `npm ci`, `npm run build`, `Verify dist matches the tag`); behoud checkout met `persist-credentials: false` en `setup-node`. Job `build`: verwijder de `download-artifact`-stap en het bijbehorende commentaar. Voeg job `go-install` toe (tussen `build` en `publish`): `runs-on: ubuntu-latest`, `timeout-minutes: 10`, `env: TAG: ${{ needs.resolve.outputs.tag }}`, stappen checkout, `setup-go`, een `run`-stap die `GOPRIVATE=… GOBIN="$RUNNER_TEMP/bin" go install …@"$TAG"` draait en `ci/spa-smoke.sh "$RUNNER_TEMP/bin/lighthouse"`. `publish`: `needs: [resolve, build, go-install]`. Werk het kop-commentaar bij (de binary embedt de `dist` uit de tag, `web` controleert hem).

- [ ] **Step 4: Draai de tests, verwacht PASS**

Run: `uv run pytest tests/test_release_workflows_pin.py tests/test_ci_domains_pin.py -q`
Expected: PASS. Controleer ook `actionlint` als beschikbaar.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/build.yml tests/test_release_workflows_pin.py
git commit -m "feat(ci): build controleert dist van de tag en bewijst go install (#349)"
```

---

### Task 3: `release.yml` maakt C2 en pusht atomair

**Files:**
- Modify: `.github/workflows/release.yml` (kop-commentaar; nieuwe job `ui`; job `release`)
- Modify: `tests/test_release_workflows_pin.py`

**Interfaces:**
- Consumes: `ci/tag-with-ui.sh <tag> <dist-bron>` uit taak 1; `build.yml` uit taak 2.
- Produces: artifact `ui-dist` (inclusief verborgen bestanden), gedownload naar `$RUNNER_TEMP/ui-dist`; hetzelfde patroon gebruikt taak 4.

- [ ] **Step 1: Pin-tests (falend)**

- `test_release_pushes_atomically` wordt: `release` bevat `ci/tag-with-ui.sh "$tag" "$RUNNER_TEMP/ui-dist"`, `main_sha=$(git rev-parse HEAD)` vóór dat script, en `git push --atomic origin "$main_sha:refs/heads/main" "refs/tags/$tag"`; `for attempt in 1 2 3` blijft.
- `test_ui_job_builds_in_a_read_only_job`: job `ui` heeft `contents: read`, checkout met `ref: ${{ github.sha }}` en `persist-credentials: false`, `node-version-file: web/.nvmrc`, `npm ci` en `npm run build` met `working-directory: web`, en een `upload-artifact` met `name: ui-dist`, `path: internal/web/dist`, `if-no-files-found: error`.
- `test_ui_job_uploads_hidden_files`: de upload-stap van `ui` bevat `include-hidden-files: true`.
- `test_writing_jobs_run_no_npm`: voor `release.yml` job `release` en `prerelease.yml` job `tag` (laatste nog niet aanwezig: sla over als het bestand ontbreekt, taak 4 schakelt het in) geldt: geen `npm`, geen `setup-node`; `release` heeft `needs: [macos, ui]` en een `download-artifact` van `ui-dist` naar `${{ runner.temp }}/ui-dist`.
- `test_release_retry_does_not_rebuild`: `npm` komt niet binnen de `for attempt`-lus voor (volgt uit de vorige, maar pint dat de lus het artifact gebruikt: het script wordt binnen de lus aangeroepen, na `git reset --hard origin/main`).
- Bestaande `test_write_permission_only_where_needed`, `test_only_release_job_persists_credentials` en `test_every_step_job_has_a_runner` blijven gelden; `ui` heeft `runs-on` en `timeout-minutes`, en de telling van `persist-credentials: false` klopt met één extra checkout.

- [ ] **Step 2: Draai, verwacht FAIL**

Run: `uv run pytest tests/test_release_workflows_pin.py -q`

- [ ] **Step 3: Pas `release.yml` aan**

Nieuwe job `ui` (`needs: macos`, `runs-on: ubuntu-latest`, `timeout-minutes: 10`, `permissions: contents: read`) met checkout `ref: ${{ github.sha }}`, `setup-node` (SHA van `build.yml`), `npm ci`, `npm run build`, upload zoals in de pin. Job `release`: `needs: [macos, ui]`; een `download-artifact`-stap (SHA van `build.yml`) naar `${{ runner.temp }}/ui-dist`; in de stap `release` vóór het script `UI_DIST: ${{ runner.temp }}/ui-dist` in `env:` (geen `${{ }}` in `run:`, dus de pin-roep gebruikt `"$RUNNER_TEMP/ui-dist"`, wat op de runner hetzelfde pad is). Binnen de lus, na `git commit -m "chore(release): $tag"`: `main_sha=$(git rev-parse HEAD)`, `ci/tag-with-ui.sh "$tag" "$RUNNER_TEMP/ui-dist"` (vervangt `git tag -a`), push `--atomic origin "$main_sha:refs/heads/main" "refs/tags/$tag"`. De controle op onze tag op de remote en `git tag -d` blijven. Werk het kop-commentaar bij (twee commits, tag naast `main`).

- [ ] **Step 4: Draai de tests, verwacht PASS**

Run: `uv run pytest tests/test_release_workflows_pin.py tests/test_tag_with_ui.py -q`

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/release.yml tests/test_release_workflows_pin.py
git commit -m "feat(ci): release tagt een commit met dist, UI gebouwd in een leesrechten-job (#349)"
```

---

### Task 4: `prerelease.yml`

**Files:**
- Create: `.github/workflows/prerelease.yml`
- Modify: `tests/test_release_workflows_pin.py`

**Interfaces:**
- Consumes: `ci/tag-with-ui.sh`, `build.yml` (`workflow_call` met input `tag`), het `ui-dist`-patroon uit taak 3.
- Produces: `prerelease.yml` met jobs `ui`, `tag`, `build`.

- [ ] **Step 1: Pin-tests (falend)**

Voeg `PRERELEASE = WORKFLOWS / "prerelease.yml"` toe en neem het op in de lussen van `test_actions_pinned_by_sha`, `test_only_release_job_persists_credentials` (nu `total == 2`: `release.release` en `prerelease.tag`; `persist-credentials: false`-telling per bestand: checkouts min het aantal uitzonderingen, 1 voor `RELEASE` en 1 voor `PRERELEASE`), `test_write_permission_only_where_needed` (`allowed` krijgt `("prerelease.yml","tag")` en `("prerelease.yml","build")`), `test_no_inputs_in_run_scripts` en `test_every_step_job_has_a_runner`. Nieuwe tests:

- `test_prerelease_trigger_and_input`: alleen `workflow_dispatch` met input `tag`; geen `push`/`pull_request`/`schedule`.
- `test_prerelease_rejects_non_prerelease_tags`: een `run:`-blok bevat de regex `^v[0-9]+\.[0-9]+\.[0-9]+-[0-9A-Za-z.-]+$` (via `grep -Eq`) met `exit 1` bij geen match; deze controle staat in job `ui` én in job `tag` (de tweede is de werkelijke poort voor het schrijftoken), vóór `ci/tag-with-ui.sh`.
- `test_prerelease_tag_job_pushes_only_the_tag`: job `tag` heeft `needs: ui`, `contents: write`, `persist-credentials: true`, `git push origin "refs/tags/$tag"` en niet `--atomic`, niet `HEAD:`, niet `main`; hij weigert een tag die al op de remote bestaat (`git ls-remote --exit-code --tags origin "refs/tags/$tag"` → `exit 1`), draait `ci/tag-with-ui.sh "$tag" "$RUNNER_TEMP/ui-dist"`.
- `test_prerelease_build_calls_build_workflow`: job `build` heeft `needs: tag`, `contents: write`, `uses: ./.github/workflows/build.yml`, `with: tag: ${{ needs.tag.outputs.tag }}`; `tag` heeft een output `tag`.
- `test_writing_jobs_run_no_npm` uit taak 3 dekt nu ook `prerelease.yml` job `tag` (haal de skip weg).
- `test_ui_job_uploads_hidden_files` dekt ook `prerelease.yml`.

- [ ] **Step 2: Draai, verwacht FAIL**

Run: `uv run pytest tests/test_release_workflows_pin.py -q`

- [ ] **Step 3: Schrijf `prerelease.yml`**

Kop-commentaar (waarom: een tag zonder `dist` laat `build.yml` falen, dus een prerelease-tag ontstaat ook op een runner). `on: workflow_dispatch` met input `tag` (string, required); `permissions: contents: read`; `concurrency: group: prerelease-${{ inputs.tag }}`, `cancel-in-progress: false`. Job `ui`: validatie van de tagvorm, checkout met `ref: ${{ github.sha }}` (de ref waarvan de dispatch startte), `setup-node`, npm, upload met `include-hidden-files: true`. Job `tag` (`needs: ui`, `contents: write`, output `tag`): checkout `ref: ${{ github.sha }}`, `fetch-depth: 0`, `persist-credentials: true`; download naar `$RUNNER_TEMP/ui-dist`; validatie, controle dat de tag niet op de remote staat, `git config user.name/email` zoals `release.yml`, script, push van alleen de tag, `echo "tag=$tag" >> "$GITHUB_OUTPUT"`. Job `build` zoals in de pin. De tag-invoer gaat via `env: TAG: ${{ inputs.tag }}`.

- [ ] **Step 4: Draai de tests, verwacht PASS**

Run: `uv run pytest tests/test_release_workflows_pin.py -q`; `actionlint` als beschikbaar.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/prerelease.yml tests/test_release_workflows_pin.py
git commit -m "feat(ci): prerelease.yml zet een prerelease-tag met dist (#349)"
```

---

### Task 5: Documentatie en changelog-fragment

**Files:**
- Modify: `docs/install.md` (kop "Install with Go" ~r.45-53; alinea "the last one wins" ~r.71-73; `content="placeholder"` ~r.139)
- Modify: `docs/site/install.html` (~r.113-120)
- Modify: `docs/releasing.md` (*The release commit*, *Recovering a failed release*, *Testing the build*)
- Create: `changelog.d/issue-349-go-install-ui.md`

**Interfaces:**
- Consumes: gedrag uit taak 1-4.

- [ ] **Step 1: Werk de docs bij volgens de spec, sectie *Documentatie***

`install.md`: `go install …@<tag>` en `@latest` leveren de UI mee; `@main` serveert de placeholder; de "last one wins"-alinea noemt alleen een build van `@main` of een clone zonder `npm run build` als route naar de placeholder; de regel bij `content="placeholder"` idem. `install.html`: de zin over `go install` volgt `install.md`. `releasing.md`: *The release commit* beschrijft twee commits (changelog op `main`, `dist`-commit met de tag, tag geen voorouder van `main`); *Testing the build*: start `prerelease.yml` met `vX.Y.Z-rc.N` vanaf de te testen branch, een met de hand gepushte tag zonder `dist` laat `build.yml` falen; *Recovering a failed release*: faalt `web` op een gepushte tag, dan een nieuwe patch-release, nooit een verplaatste tag, ook niet bij een latere afwijkende `build`-run. Alle `.md`-links relatief naar bestaande doelen (`md-link-check`).

- [ ] **Step 2: Schrijf het fragment `changelog.d/issue-349-go-install-ui.md`**

Frontmatter `type: fixed`, één Engelse zin: `go install` of a tag now serves the web UI instead of the placeholder.

- [ ] **Step 3: Verifieer**

Run: `uv run pytest tests/test_changelog.py $(grep -l 'install.html' tests/*.py) -q`, en `grep -n "alleen een placeholder\|only a placeholder\|serves only a placeholder" docs/install.md docs/site/install.html` — verwacht geen treffers.
Expected: PASS, geen treffers.

- [ ] **Step 4: Commit**

```bash
git add docs/install.md docs/site/install.html docs/releasing.md changelog.d/issue-349-go-install-ui.md
git commit -m "docs(release): go install van een tag levert de UI (#349)"
```

---

## Self-review

- **Spec-dekking:** script + tests (taak 1); `build.yml` web/build/go-install/publish en `test_build_checkouts_pin_the_tag` = 4 (taak 2); `release.yml` ui, atomische push, retry (taak 3); `prerelease.yml` en pin-lussen (taak 4); docs en fragment (taak 5); geen npm in schrijvende jobs en `.gitkeep` gepind (taak 3/4); `ci.yml`, `.gitignore`, Vite ongewijzigd. Criterium 3 (placeholder zonder `dist`) staat al in `internal/web/web_test.go`; niets te doen.
- **Aan de bouwer (spec):** "Of `go-install` een prerelease-tag ook controleert": ja, `needs.resolve.outputs.tag` geldt voor elke tag; `GOPRIVATE` houdt hem buiten de checksum-database. Valt het in de praktijk om, noteer dat in de PR.
- **Typeconsistentie:** artifact `ui-dist`, pad `$RUNNER_TEMP/ui-dist`, script-aanroep `ci/tag-with-ui.sh "$tag" "$RUNNER_TEMP/ui-dist"` in taak 1, 3 en 4.
- **Niet geverifieerd door een run:** de workflows zelf draaien pas bij de eerste release of prerelease. Na merge: start `prerelease.yml` met `vX.Y.Z-rc.1` en controleer dat `go-install` groen is (handmatige stap voor de gebruiker, buiten dit plan).
