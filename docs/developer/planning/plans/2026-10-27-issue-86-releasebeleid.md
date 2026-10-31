# Releasebeleid, releasebinaries en changelog (#86) — implementatieplan

> **Voor agents:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkboxen (`- [ ]`).

**Doel:** elke merge met een fix en elke week met iets anders levert automatisch een GitHub-release met vijf binaries, `SHA256SUMS` en Engelse notes uit `changelog.d/`; de merge van deze PR levert `v0.2.0`.

**Architectuur:** één stdlib-Python-script, `ci/changelog.py`, is de enige parser van fragmenten. CI roept `check` aan op elke PR; `release.yml` roept `release` aan (bump, versie, sectie in `CHANGELOG.md`, fragmenten weg), commit en tagt en pusht atomair, en roept `build.yml` aan. `build.yml` bouwt de matrix buiten de checkout, controleert `lighthouse version` en publiceert met `notes` uit hetzelfde script. `/lighthouse-verify-pr` schrijft het fragment, alleen als `changelog.d/.lighthouse` er is.

**Tech stack:** Python 3.11 stdlib (zoals `ci/domains.py`), pytest, GitHub Actions, `gh`, Go 1.26 build tags.

**Spec:** [2026-10-27-releasebeleid-design.md](../specs/2026-10-27-releasebeleid-design.md)

## Globale randvoorwaarden

- Code, commentaar en workflowcommentaar in het Engels; `docs/releasing.md`, `CHANGELOG.md` en fragmenten in het Engels; dit plan en commits in het Nederlands (Conventional prefix).
- Fragmenttypen, exact: `added`, `changed`, `fixed`, `removed`, `internal`. Releasebaar = alles behalve `internal`.
- Markeerbestand `changelog.d/.lighthouse`; nooit verwijderd, nooit geparsed.
- Sectiekop exact `## [vX.Y.Z] - YYYY-MM-DD`; groepen in deze volgorde: `### Added`, `### Changed`, `### Fixed`, `### Removed`.
- Doelen exact: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`; bestandsnamen `lighthouse_<os>_<arch>`, `.exe` voor Windows; plus `SHA256SUMS`. `CGO_ENABLED=0`, `-trimpath`.
- Versiecontrole: `lighthouse_linux_amd64 version` moet exact `lighthouse <tag>` geven.
- Cron: maandag 06:00 UTC (`0 6 * * 1`).
- Elke stappenjob (alles behalve een `uses:`-job) heeft `runs-on: ubuntu-latest`.
- Acties op SHA met `# vX.Y.Z`-commentaar; hergebruik de SHA's uit `ci.yml` (checkout v7.0.1, setup-go v7.0.0, upload-artifact v7.0.2, download-artifact v8.0.2). `persist-credentials: false`, behalve in de ene job die pusht. `timeout-minutes` op elke job.
- Nooit `${{ }}` met invoer of fragmenttekst in een `run:`-script; altijd via `env:`.
- `tests/test_ci_go_test_pin.py` en `tests/test_golangci_pin.py` pinnen regels in `ci.yml`: precies twee `- run: go test …`-regels, de `CGO_ENABLED=0 go build ./...`-regel, de golangci-versie. Die blijven.
- Testgate: `python3 ci/domains.py gate`.
- De releasebinary draagt de placeholder (`internal/web/placeholder`): `build.yml` draait geen `npm`-build (non-goal, #78).

## Afwijkingen van de spec, door de bouwer gekozen

- Main liep na de spec door (#68, #92): CI is per domein (`ci/domains.py`), en `ci.yml` heeft een `changes`-job. De fragmentcheck wordt een eigen job **zonder** `needs: changes`: hij geldt voor elke PR. `tests/test_ci_domains_pin.py` telt de `uses:`-regels (14); de nieuwe job maakt dat 15.
- De spec noemt als guard-frase "only if `changelog.d/.lighthouse` is present". `_GUARD_RE` in `tests/pack/test_skill_contracts.py` zoekt de aaneengesloten tekst `if present`; die frase faalt dus. De alinea gebruikt `if present` letterlijk (taak 5).
- Bump is een functie van de **stand** (alle openstaande fragmenten), niet van de laatste merge. Na een normale run is dat hetzelfde als besluit 1; na een mislukte run pakt een volgende run het verschil vanzelf op, en een door `concurrency` vervangen wachtende run verliest niets.
- Een geforceerde bump via dispatch die lager is dan wat `auto` zou kiezen (een `patch` terwijl er een `added` openstaat), of een bump zonder releasebaar fragment, faalt luid in plaats van te releasen.
- Fragmentnummering: eerste `<branch>.md`, daarna `<branch>-2.md`, `<branch>-3.md`, …
- `CONTRIBUTING.md` krijgt één regel over het fragment: een PR van buiten heeft geen verify-pr maar wel de CI-check.

## Review-focus

1. **Een prerelease-tag als "laatste tag".** `v0.2.0-rc.1` bestaat even op de remote; `latest_tag` moet hem negeren, anders wordt de volgende versie `v0.2.0-rc.2`-achtig onzin of `v0.3.0`. Test in taak 3.
2. **Een fragment met Windows-regeleinden, BOM of geen afsluitende `---`.** Een bijdrager van buiten schrijft het met de hand. CRLF en BOM: geaccepteerd; ontbrekende sluiting, lege tekst, onbekende sleutel, twee `type`-regels: luid geweigerd met het pad in de melding. Tests in taak 2.
3. **Een PR die alleen bestaande fragmenten wijzigt of hernoemt.** Telt niet als nieuw fragment (`--diff-filter=A --no-renames`); een hernoemd fragment telt wel als nieuw onder de nieuwe naam — `--no-renames` maakt er delete+add van. Test in taak 2.
4. **De releaseworkflow vindt geen `v*`-tag** (checkout zonder tags). `release` faalt met exit 2 en een melding, nooit stil `v0.0.1`. Test in taak 3.
5. **Notes voor een tag zonder sectie.** `notes v0.2.0` zonder sectie in `CHANGELOG.md`: exit 1, en `build.yml` faalt voor een gewone tag; alleen een prerelease-tag (met `-`) krijgt de vaste regel. Test in taak 3 (script), pin in taak 4 (workflow).

---

### Taak 0: voorbereiding

- [ ] **Stap 1: main erin** — `git fetch origin && git merge --no-edit origin/main` (geen rebase: de branch is gepusht). Sinds het plan kwam o.a. 193f6a7 (#87, `docs/getting-started.md`); taak 6 leunt daarop.
- [ ] **Stap 2: web-afhankelijkheden** — `(cd web && npm ci)`. Taak 1 en 2 wijzigen `.github/workflows/ci.yml`, dat in `_WEB_TOO` van `ci/domains.py` staat: zonder `web/node_modules` geeft de testgate exit 3 en blokkeert `pre-commit-tests` de commit met `COULD NOT CHECK`.

### Taak 1: Windows bouwt, en CI bouwt Windows en macOS

**Bestanden:**
- Wijzig: `internal/store/lock.go` (houdt `ErrLocked`, `LockedError`; `acquireLock` gaat eruit)
- Nieuw: `internal/store/lock_flock.go`, `internal/store/lock_other.go`
- Wijzig: `.github/workflows/ci.yml` (job `go`)
- Test: `tests/test_ci_go_test_pin.py`

**Interfaces:**
- Produceert: `acquireLock(path string) (*os.File, error)` ongewijzigd op flock-platforms; op andere platforms een fout `errors.New("database lock not supported on this platform")`.

- [ ] **Stap 1: falende test schrijven**

In `tests/test_ci_go_test_pin.py`:

```python
def test_ci_cross_builds_release_targets():
    """Fails when CI stops building the non-linux release targets, so a platform-only break
    would first show up at the release."""
    ci = (ROOT / ".github" / "workflows" / "ci.yml").read_text(encoding="utf-8")
    for target in ("GOOS=windows GOARCH=amd64", "GOOS=darwin GOARCH=arm64"):
        assert re.search(rf"^\s*- run: {target} CGO_ENABLED=0 go build \./\.\.\.\s*$", ci, re.MULTILINE), target
```

- [ ] **Stap 2: rood zien**

Run: `uv run pytest tests/test_ci_go_test_pin.py -q` → FAIL op `test_ci_cross_builds_release_targets`.
Run: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...` → `internal/store/lock.go:28:20: undefined: syscall.Flock`.

- [ ] **Stap 3: splitsen**

`lock_flock.go` met `//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd` en de huidige `acquireLock`; `lock_other.go` met de negatie en de fout hierboven (sluit het bestand niet open: open het niet). Kopieer de vorm van `internal/usage/lock_flock.go` / `lock_other.go`. In `ci.yml`, job `go`, direct na `- run: CGO_ENABLED=0 go build ./...`:

```yaml
      # The release also ships windows and darwin: a platform-only break shows up here.
      - run: GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...
      - run: GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build ./...
```

- [ ] **Stap 4: groen zien**

Run: `for t in windows/amd64 darwin/arm64 darwin/amd64 linux/arm64; do GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go build ./... || echo FAIL $t; done` → geen uitvoer.
Run: `go test -race ./internal/store/ && uv run pytest tests/test_ci_go_test_pin.py tests/test_ci_domains_pin.py -q` → PASS.

- [ ] **Stap 5: commit** — `fix(store): bouw op Windows met een lock per platform, CI bouwt windows en darwin (#86)`

### Taak 2: de fragmentparser, de CI-check en het markeerbestand

**Bestanden:**
- Nieuw: `ci/changelog.py`, `tests/test_changelog.py`, `changelog.d/.lighthouse` (de tekst noemt `docs/releasing.md` als pad, niet als markdown-link: het is geen `.md`)
- Wijzig: `.github/workflows/ci.yml` (nieuwe job `changelog`), `tests/test_ci_domains_pin.py`

**Interfaces:**
- Produceert in `ci/changelog.py` (module-docstring zoals `ci/domains.py`: wat, wie roept aan, "Stdlib only, Python 3.11"):
  - `TYPES = ("added", "changed", "fixed", "removed", "internal")`, `RELEASABLE = TYPES[:4]`, `MARKER = ".lighthouse"`, `FRAGMENT_DIR = "changelog.d"`
  - `class FragmentError(Exception)` — bericht begint met het pad
  - `@dataclass(frozen=True) class Fragment: path: Path; type: str; text: str`
  - `parse_fragment(path: Path) -> Fragment`
  - `read_fragments(directory: Path) -> list[Fragment]` — gesorteerd op bestandsnaam; slaat `MARKER` over; elk ander item dat geen `*.md`-bestand is: `FragmentError`; ontbrekende map: `[]`
  - `added_fragments(root: Path, base: str, head: str) -> list[str]` — `git diff --name-only --diff-filter=A --no-renames -z base...head -- changelog.d/`, zonder `MARKER`; git-fout: `RuntimeError`
  - CLI `python3 ci/changelog.py check --base <sha> --head <sha> [--root .]`: exit 0 ok; 1 geen nieuw fragment of een `FragmentError` (melding op stderr); 2 git faalt (`COULD NOT CHECK` op stderr)
  - Testloader: `tests/test_changelog.py` laadt het script met `importlib` zoals `tests/_domains.py`.

Parsercontract: utf-8 (BOM toegestaan), `\r\n` als `\n`; regel 1 exact `---`; front matter tot de volgende regel `---`; daarin precies één niet-lege regel, `type: <t>` met `t` in `TYPES`; tekst na de sluiting, `strip()`, niet leeg.

- [ ] **Stap 1: falende tests schrijven**

```python
@pytest.mark.parametrize("body,typ,text", [
    ("---\ntype: fixed\n---\nNo more rewrites.\n", "fixed", "No more rewrites."),
    ("﻿---\r\ntype: added\r\n---\r\nNew skill.\r\n", "added", "New skill."),
])
def test_parse_fragment_accepts(tmp_path, body, typ, text): ...  # Fragment(type, text) zoals gegeven

@pytest.mark.parametrize("body", [
    "type: fixed\n---\nx\n",                 # no opening ---
    "---\ntype: fixed\nx\n",                 # no closing ---
    "---\ntype: fixed\n---\n   \n",          # empty text
    "---\ntype: bugfix\n---\nx\n",           # unknown type
    "---\ntype: fixed\ntype: added\n---\nx\n",
    "---\ntype: fixed\nscope: cli\n---\nx\n",
    "---\n---\nx\n",
])
def test_parse_fragment_refuses(tmp_path, body): ...  # FragmentError, str(err) bevat de bestandsnaam

def test_read_fragments_skips_marker_and_sorts(tmp_path): ...  # .lighthouse + b.md + a.md -> [a, b]
def test_read_fragments_refuses_other_files(tmp_path): ...     # notes.txt -> FragmentError
def test_check_requires_an_added_fragment(git_repo): ...       # alleen code gewijzigd -> exit 1, "no new fragment"
def test_check_ignores_modified_fragment(git_repo): ...        # bestaand fragment gewijzigd -> exit 1
def test_check_counts_renamed_fragment(git_repo): ...          # git mv a.md b.md -> exit 0
def test_check_ignores_added_marker(tmp_path): ...             # eigen repo: commit 1 zonder changelog.d/, commit 2 voegt alleen .lighthouse toe -> exit 1
def test_check_refuses_bad_fragment_anywhere(git_repo): ...    # nieuw goed fragment + oud kapot fragment -> exit 1
def test_check_passes(git_repo): ...                           # exit 0
def test_check_bad_base_cannot_check(git_repo): ...            # --base deadbeef -> exit 2, "COULD NOT CHECK"
```

`git_repo`: fixture met `tests.pack._testkit.git`, commit 1 met `changelog.d/.lighthouse` en `changelog.d/old.md`; de test maakt commit 2 en roept `main(["check", "--base", c1, "--head", c2, "--root", str(root)])` aan.

- [ ] **Stap 2: rood zien** — `uv run pytest tests/test_changelog.py -q` → FAIL (script ontbreekt).

- [ ] **Stap 3: implementeren** — de interfaces hierboven in `ci/changelog.py`, `main(argv: list[str] | None = None) -> int` met `argparse`-subcommando's (taak 3 voegt er twee toe). `chmod +x` niet nodig: CI roept `python3 ci/changelog.py` aan.

- [ ] **Stap 4: markeerbestand en CI-job**

`changelog.d/.lighthouse`, exact:

```
This directory holds one changelog fragment per change; the release workflow merges them into CHANGELOG.md. See docs/releasing.md.
```

In `ci.yml` een job `changelog` (geen `needs`, geen `if`), `runs-on: ubuntu-latest`, `timeout-minutes: 5`, checkout met `fetch-depth: 0` en `persist-credentials: false`, en één stap met `env: BASE/HEAD` uit `github.event.pull_request.{base,head}.sha` (zoals de `changes`-job) en `run: python3 ci/changelog.py check --base "$BASE" --head "$HEAD"`. Commentaar boven de job: elke PR draagt een fragment, ook buiten de domeinen, zie `docs/releasing.md`.

In `tests/test_ci_domains_pin.py`: `len(uses) == 14` wordt `15`, en:

```python
def test_changelog_job_runs_on_every_pr():
    """Fails when the fragment check starts depending on the domains of the change."""
    job = _jobs()["changelog"]
    assert "needs:" not in job and "if:" not in job
    for needle in ("runs-on: ubuntu-latest", "fetch-depth: 0", "python3 ci/changelog.py check --base \"$BASE\" --head \"$HEAD\""):
        assert needle in job, needle
```

- [ ] **Stap 5: groen zien** — `uv run pytest tests/test_changelog.py tests/test_ci_domains_pin.py -q` → PASS.

- [ ] **Stap 6: commit** — `feat(ci): fragmentparser en CI-check op changelog.d (#86)`

### Taak 3: bump, versie, `CHANGELOG.md` en notes

**Bestanden:**
- Wijzig: `ci/changelog.py`, `tests/test_changelog.py`

**Interfaces:**
- Consumeert: `Fragment`, `read_fragments`, `RELEASABLE`, `MARKER` uit taak 2.
- Produceert:
  - `class ReleaseError(Exception)` (exit 1) en `class TagError(Exception)` (exit 2)
  - `decide_bump(fragments: list[Fragment], event: str, forced: str = "auto") -> str | None` — `event` in `push`, `schedule`, `workflow_dispatch`; `forced` in `auto`, `patch`, `minor`, `major`
  - `latest_tag(tags: list[str]) -> str` — hoogste naar semver onder `^v\d+\.\d+\.\d+$`; geen: `TagError`
  - `next_version(tag: str, bump: str) -> str`
  - `render_section(version: str, date: str, fragments: list[Fragment]) -> str`
  - `insert_section(changelog: str, section: str) -> str` — vóór de eerste regel die met `## ` begint; geen zo'n regel: achteraan
  - `extract_section(changelog: str, tag: str) -> str | None` — de body onder `## [<tag>] - …` tot de volgende `## `-regel, `strip()`
  - `release(root: Path, event: str, forced: str, date: str, tags: list[str]) -> str | None` — schrijft `CHANGELOG.md`, verwijdert alle gelezen fragmenten (ook `internal`, nooit `MARKER`), geeft de nieuwe tag; `None` = niets gedaan, tree ongemoeid; ontbrekende `CHANGELOG.md`: `ReleaseError` (taak 6 schrijft hem). Volgorde, vast: eerst `latest_tag(tags)`, dan fragmenten lezen en `decide_bump`, dan `CHANGELOG.md` lezen; zo geeft een repo zonder tags altijd exit 2
  - CLI `release --event <e> [--bump auto] [--date YYYY-MM-DD] [--root .]`: tags uit `git tag --list 'v*'`; schrijft de tag op stdout (leeg bij `None`); exit 0, 1 `ReleaseError`/`FragmentError`, 2 `TagError`/git-fout
  - CLI `notes <tag> [--root .]`: sectie op stdout, exit 0; geen sectie: exit 1

Regels van `decide_bump` (R = releasebare fragmenten):
- R leeg: `None` bij `auto`; `ReleaseError("nothing to release")` bij een geforceerde bump.
- `push`: geen `fixed` in R → `None`; alleen `fixed` → `patch`; `fixed` plus een ander type → `minor`.
- `schedule` en `workflow_dispatch`: alleen `fixed` → `patch`; anders `minor`.
- `forced` ≠ `auto`: de geforceerde bump; lager dan wat de `schedule`-regel kiest → `ReleaseError`.

`render_section`: kop `## [<version>] - <date>`, dan per releasebaar type met fragmenten een `### <Type>` en per fragment `- <text>`, vervolgregels van de tekst met twee spaties ingesprongen; lege regel tussen blokken; `internal` nooit.

- [ ] **Stap 1: falende tests schrijven**

```python
@pytest.mark.parametrize("types,event,forced,want", [
    ([], "push", "auto", None),
    (["internal"], "push", "auto", None),
    (["fixed"], "push", "auto", "patch"),
    (["fixed", "internal"], "push", "auto", "patch"),
    (["added"], "push", "auto", None),
    (["fixed", "added"], "push", "auto", "minor"),
    (["added"], "schedule", "auto", "minor"),
    (["fixed"], "schedule", "auto", "patch"),
    (["internal"], "schedule", "auto", None),
    (["changed"], "workflow_dispatch", "auto", "minor"),
    (["fixed"], "workflow_dispatch", "minor", "minor"),
    (["added"], "workflow_dispatch", "major", "major"),
])
def test_decide_bump(types, event, forced, want): ...

@pytest.mark.parametrize("types,forced", [(["added"], "patch"), ([], "major"), (["internal"], "minor")])
def test_decide_bump_refuses(types, forced): ...  # ReleaseError

def test_latest_tag_ignores_prerelease_and_sorts_numerically():
    assert latest_tag(["v0.1.6", "v0.1.10", "v0.1.9", "v0.2.0-rc.1", "vfoo"]) == "v0.1.10"
def test_latest_tag_without_tags(): ...  # [] en ["v0.2.0-rc.1"] -> TagError

@pytest.mark.parametrize("bump,want", [("patch", "v0.1.7"), ("minor", "v0.2.0"), ("major", "v1.0.0")])
def test_next_version(bump, want): assert next_version("v0.1.6", bump) == want

def test_render_section_groups_in_order_and_drops_internal(): ...
    # removed, internal, added, fixed -> "## [v0.2.0] - 2026-10-27\n\n### Added\n\n- …\n\n### Fixed\n\n- …\n\n### Removed\n\n- …\n"
def test_render_section_indents_continuation_lines(): ...  # "a\nb" -> "- a\n  b"
def test_insert_section_goes_above_latest(): ...   # boven "## [v0.2.0]", onder de preamble
def test_extract_section_roundtrip(): ...          # insert dan extract geeft de body zonder kop
def test_extract_section_missing(): ...            # None
def test_release_writes_changelog_and_removes_fragments(tmp_path): ...
    # fixed + added + internal + .lighthouse, event push, tags ["v0.1.6"] -> "v0.2.0";
    # CHANGELOG.md bevat "## [v0.2.0] - 2026-10-27"; changelog.d/ bevat alleen .lighthouse
def test_release_nothing_leaves_tree_untouched(tmp_path): ...  # alleen added op push -> None, bytes ongewijzigd
def test_release_without_changelog_refuses(tmp_path): ...  # fixed, geen CHANGELOG.md -> ReleaseError, fragment blijft
def test_cli_release_without_tags_exits_2(tmp_path, capsys): ...  # eigen repo zonder tags, met CHANGELOG.md en een fixed-fragment -> exit 2
def test_cli_notes_missing_section_exits_1(tmp_path): ...
```

- [ ] **Stap 2: rood zien** — `uv run pytest tests/test_changelog.py -q` → FAIL op de nieuwe tests.

- [ ] **Stap 3: implementeren** in `ci/changelog.py` volgens de interfaces en regels.

- [ ] **Stap 4: groen zien** — `uv run pytest tests/test_changelog.py -q` → PASS.

- [ ] **Stap 5: commit** — `feat(ci): bump, versie en CHANGELOG.md uit de fragmenten (#86)`

### Taak 4: `release.yml` en `build.yml`

**Bestanden:**
- Nieuw: `.github/workflows/release.yml`, `.github/workflows/build.yml`, `tests/test_release_workflows_pin.py`

**Interfaces:**
- Consumeert: `python3 ci/changelog.py release --event "$EVENT" --bump "$BUMP"` en `python3 ci/changelog.py notes "$TAG"` uit taak 3.
- Produceert: `build.yml` met `workflow_call`-invoer `tag` (string, required) en `workflow_dispatch`-invoer `tag`; `release.yml`-job `release` met output `tag`.

`release.yml`:
- `on`: `push: branches: [main]`; `schedule: - cron: "0 6 * * 1"`; `workflow_dispatch` met `bump` (`type: choice`, `auto|patch|minor|major`, default `auto`).
- `permissions: contents: read` bovenaan; `concurrency: group: release-${{ github.event_name }}`, `cancel-in-progress: false`.
- Job `release` (`runs-on: ubuntu-latest`, `contents: write`, `timeout-minutes: 10`, output `tag`): checkout `ref: main`, `fetch-depth: 0`, `persist-credentials: true` met commentaar waarom (de enige job die pusht). `env: EVENT: ${{ github.event_name }}`, `BUMP: ${{ inputs.bump || 'auto' }}`. Gitidentiteit `github-actions[bot]` / `41898283+github-actions[bot]@users.noreply.github.com`. Script met `set -euo pipefail`, deze volgorde:

```bash
for attempt in 1 2 3; do
  git fetch --force --tags origin main
  git reset --hard origin/main
  tag=$(python3 ci/changelog.py release --event "$EVENT" --bump "$BUMP")
  if [ -z "$tag" ]; then echo "nothing to release"; exit 0; fi
  git add -A CHANGELOG.md changelog.d
  git commit -m "chore(release): $tag"
  git tag -a "$tag" -m "$tag"
  if git push --atomic origin HEAD:main "refs/tags/$tag"; then
    echo "tag=$tag" >> "$GITHUB_OUTPUT"; exit 0
  fi
  git tag -d "$tag"
done
echo "release: push refused three times; nothing landed" >&2; exit 1
```

- Job `build`: `needs: release`, `if: needs.release.outputs.tag != ''`, `permissions: contents: write`, `uses: ./.github/workflows/build.yml`, `with: tag: ${{ needs.release.outputs.tag }}`.

`build.yml`:
- `on`: `push: tags: ["v*"]`; `workflow_call` en `workflow_dispatch`, elk met invoer `tag`. `permissions: contents: read`.
- Job `resolve` (5 min): `env: TAG: ${{ inputs.tag || github.ref_name }}`; faalt tenzij `TAG` matcht `^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`; outputs `tag` en `prerelease` (`true` als `TAG` een `-` bevat).
- `build` en `publish` krijgen `env: TAG: ${{ needs.resolve.outputs.tag }}` (en `PRERELEASE` in `publish`) op jobniveau.
- Job `build` (`needs: resolve`, 10 min, read-only), matrix `include` met de vijf `goos`/`goarch`: checkout `ref: refs/tags/${{ needs.resolve.outputs.tag }}`, `fetch-depth: 0`, `persist-credentials: false`; setup-go `go-version-file: go.mod`; stap `test -z "$(git status --porcelain)"` (commentaar: een vuile tree meldt `dev`); build `CGO_ENABLED=0 GOOS=… GOARCH=… go build -trimpath -o "$RUNNER_TEMP/dist/$NAME" ./cmd/lighthouse` met `NAME` = `lighthouse_<goos>_<goarch>` plus `.exe` voor windows; alleen op `linux/amd64`: `out=$("$RUNNER_TEMP/dist/lighthouse_linux_amd64" version)` en `test "$out" = "lighthouse $TAG"`; upload-artifact `name: bin-<goos>-<goarch>`, `path: ${{ runner.temp }}/dist/`.
- Job `publish` (`needs: [resolve, build]`, `contents: write`, 10 min): checkout van de tag (`persist-credentials: false`); download-artifact `pattern: bin-*`, `merge-multiple: true`, `path: ${{ runner.temp }}/dist`; `(cd "$RUNNER_TEMP/dist" && sha256sum lighthouse_* > SHA256SUMS)`; notes: `python3 ci/changelog.py notes "$TAG" > "$RUNNER_TEMP/notes.md"`, en bij exit ≠ 0 alleen als `PRERELEASE = true` de regel `Pre-release build of $TAG for testing; not a release.`, anders faalt de job; dan `gh release view "$TAG"` → bestaat: `gh release upload "$TAG" --clobber "$RUNNER_TEMP"/dist/*`; anders `gh release create "$TAG" --title "$TAG" --notes-file "$RUNNER_TEMP/notes.md" [--prerelease] "$RUNNER_TEMP"/dist/*`. `GH_TOKEN: ${{ github.token }}`, `GH_REPO: ${{ github.repository }}`.

- [ ] **Stap 1: falende pintests schrijven** in `tests/test_release_workflows_pin.py` (zelfde `_jobs`-snijwijze als `test_ci_domains_pin.py`, per bestand):

```python
def test_actions_pinned_by_sha(): ...            # beide bestanden: elke uses: @<40 hex> plus "# vX.Y.Z", behalve "./.github/workflows/build.yml"
def test_release_triggers_and_concurrency(): ... # "branches: [main]", "0 6 * * 1", "workflow_dispatch", "group: release-${{ github.event_name }}", "cancel-in-progress: false"
def test_release_pushes_atomically(): ...        # "git push --atomic origin HEAD:main", "for attempt in 1 2 3"
def test_only_release_job_persists_credentials(): ...  # "persist-credentials: true" precies 1x, in job release
def test_write_permission_only_where_needed(): ...     # contents: write in release.release, release.build, build.publish; nergens anders
def test_build_targets_and_flags(): ...          # de vijf goos/goarch-paren, "-trimpath", "CGO_ENABLED=0", '"$RUNNER_TEMP/dist/'
def test_build_checks_version_and_clean_tree(): ...    # 'test "$out" = "lighthouse $TAG"', "git status --porcelain"
def test_publish_uses_notes_file_and_clobber(): ...    # "ci/changelog.py notes", "--notes-file", "--clobber", "SHA256SUMS"
def test_no_inputs_in_run_scripts(): ...          # geen "${{ inputs." en geen "${{ github.ref_name" binnen een run:-blok
def test_every_step_job_has_a_runner(): ...       # beide bestanden: elke job met steps: heeft "runs-on: ubuntu-latest"; release.build (uses:) niet
```

- [ ] **Stap 2: rood zien** — `uv run pytest tests/test_release_workflows_pin.py -q` → FAIL (bestanden ontbreken).

- [ ] **Stap 3: beide workflows schrijven** volgens de opsomming hierboven.

- [ ] **Stap 4: groen zien en lokaal proeven**

Run: `uv run pytest tests/test_release_workflows_pin.py -q` → PASS.
Run: `uvx --from actionlint-py actionlint .github/workflows/*.yml` → geen uitvoer (actionlint staat niet op deze machine; lukt `uvx` niet, noem dat in de PR).
Run (proef van de versiecontrole, buiten de checkout): `d=$(mktemp -d) && git worktree add -q --detach "$d/wt" HEAD && git -C "$d/wt" tag v9.9.9-proef && (cd "$d/wt" && CGO_ENABLED=0 go build -trimpath -o "$d/lh" ./cmd/lighthouse) && "$d/lh" version; git tag -d v9.9.9-proef; git worktree remove "$d/wt"` → `lighthouse v9.9.9-proef`.

- [ ] **Stap 5: commit** — `feat(ci): releaseworkflow en buildworkflow met vijf binaries (#86)`

### Taak 5: verify-pr schrijft het fragment

**Bestanden:**
- Wijzig: `pack/core/skills/verify-pr/SKILL.md` (nieuwe sectie tussen `## V3. Choose the form` en `## Evidence rules`; geen nieuwe `## V4.`)
- Test: `tests/pack/test_verify_pr_skill.py`
- Daarna: de geïnstalleerde kopie via `go run ./cmd/lighthouse init --upgrade`

**Interfaces:**
- Consumeert: het fragmentcontract van taak 2 (alleen als tekst; de skill roept `ci/changelog.py` niet aan, een consumer heeft dat niet).

Inhoud van de sectie `## Changelog fragment (if present)`, in het Engels, één tot drie korte alinea's; elke alinea met het woord `release` bevat letterlijk `if present`:
- Only if present: `changelog.d/.lighthouse` in the repository root. Otherwise skip this section.
- First look at what the branch already adds: `git diff --name-only --diff-filter=A origin/<default branch>...HEAD -- changelog.d/`. Every change it carries that already has a fragment gets no new one; a re-run of the skill (after a `pr-create-gate` block, say) writes nothing twice.
- For each change without one, write a fragment before `gh pr create`, commit it on the branch and push: `changelog.d/<branch>.md`, a `/` in the branch name becomes `-`; the branch's next fragment, or a name held by an open fragment of another branch, gets the first free `-<n>` from 2.
- The format block (front matter `type:` en één tot drie zinnen), de vijf typen met één regel elk; `internal` voor wat een gebruiker niet merkt (docs voor ontwikkelaars, tests, CI) en niet in de notes komt.
- Pick the type from the diff and the AC; write the text in English for a user, whatever `[project] language` is.

- [ ] **Stap 1: falende test schrijven**

```python
def test_changelog_fragment_is_opt_in_and_english():
    section = _section(TEXT, "## Changelog fragment")
    for needle in ("if present", "changelog.d/.lighthouse", "changelog.d/<branch>.md", "-<n>",
                   "--diff-filter=A", "writes nothing twice",
                   "added", "changed", "fixed", "removed", "internal", "English", "before `gh pr create`"):
        assert needle in section, needle
```

- [ ] **Stap 2: rood zien** — `uv run pytest tests/pack/test_verify_pr_skill.py -q` → FAIL.

- [ ] **Stap 3: sectie schrijven.**

- [ ] **Stap 4: dogfood** — `go run ./cmd/lighthouse init --upgrade` (bevestig met `y`), dan nog een keer zonder invoer: `go run ./cmd/lighthouse init --upgrade </dev/null` → `No changes.`

- [ ] **Stap 5: groen zien** — `uv run pytest tests/pack/test_verify_pr_skill.py tests/pack/test_skill_contracts.py tests/test_dogfood.py -q && ci/literal-grep.sh` → PASS, geen hits.

- [ ] **Stap 6: commit** — `feat(verify-pr): schrijf een changelog-fragment als changelog.d/.lighthouse er is (#86)` (skill en geïnstalleerde kopie samen).

### Taak 6: het beleid in de docs

**Bestanden:**
- Nieuw: `docs/releasing.md`, `CHANGELOG.md` (hier en niet in taak 3: zijn link naar `docs/releasing.md` moet bestaan, anders blokkeert `worktree-linkcheck`)
- Wijzig: `AGENTS.md` (regel *Releases*), `docs/install.md`, `docs/getting-started.md` (sectie *Install*, van #87, via taak 0), `CONTRIBUTING.md`
- Test: `tests/test_instruction_files.py`, `tests/test_getting_started.py`

`docs/releasing.md`, Engels, secties in deze volgorde: *Versions* (de tabel uit besluit 1, met wat "user-visible" is: de binary en alles wat `init` schrijft; een breaking change onder 0.x is een minor), *When a release happens* (merge met alleen `fixed` → patch; maandag 06:00 UTC → minor; dispatch, ook major; nooit een tag met de hand), *Changelog fragments* (vorm, typen, naam, de CI-check, `internal`), *The release commit* (de bot-uitzondering op "nothing lands on `main` directly"; bij een ruleset krijgt de bot een bypass), *Recovering a failed release* (atomaire push: dispatch `bump: auto` opnieuw; mislukte build: `workflow_dispatch` van `build.yml` met de tag, die `--clobber` uploadt of de release aanmaakt), *Upgrading* (binary downloaden, `lighthouse init --upgrade` in elke repo, de geïnstalleerde kopie via een PR).

- [ ] **Stap 1: falende test schrijven** in `tests/test_instruction_files.py`:

```python
def test_project_part_points_at_the_release_policy():
    """The old hand-tagging rule is gone; the policy lives in docs/releasing.md."""
    part = _project_part()
    assert "tags are set by hand" not in part
    assert "docs/releasing.md" in part and "changelog.d/" in part
```

In `tests/test_getting_started.py` wordt `test_install_uses_module_path` (pint nu `"GOPRIVATE=" in text`):

```python
def test_install_offers_binary_then_go():
    """The binary route comes first; the Go route stays, without the private-repo detour."""
    module = (ROOT / "go.mod").read_text(encoding="utf-8").splitlines()[0].split()[1]
    text = section("Install")
    assert "releases/latest/download/lighthouse_linux_amd64" in text and "SHA256SUMS" in text
    assert f"go install {module}/cmd/lighthouse@" in text
    assert "GOPRIVATE" not in text and "not published yet" not in text
```

- [ ] **Stap 2: rood zien** — `uv run pytest tests/test_instruction_files.py tests/test_getting_started.py -q` → FAIL.

- [ ] **Stap 3: schrijven**, in deze volgorde (een link moet bij het schrijven al een doel hebben, anders blokkeert `worktree-linkcheck`): eerst `docs/releasing.md`, dan de rest.

- `AGENTS.md`: de regel *Releases* wordt precies één bullet: "**Releases.** The release workflow tags and publishes, never a hand-set tag: see `docs/releasing.md`. Every PR adds a fragment to `changelog.d/`." — met `docs/releasing.md` als relatieve markdown-link.
- `CHANGELOG.md`, exact (de twee verwijzingen als markdown-links: Keep a Changelog naar `https://keepachangelog.com/en/1.1.0/`, de tweede relatief naar `docs/releasing.md`):

  ```markdown
  # Changelog

  All notable changes to lighthouse. The format follows Keep a Changelog; how versions are
  chosen is in docs/releasing.md. The release workflow writes the sections below from the
  fragments in `changelog.d/`; never edit a released section by hand.

  ## Earlier versions

  v0.1.0–v0.1.6: internal pre-releases.
  ```
- `docs/install.md` (lees eerst de versie van main, #68 veranderde hem): nieuwe sectie `## Download a release binary` vóór de Go-route, met `curl -fsSLO https://github.com/CorneeldH/lighthouse/releases/latest/download/lighthouse_linux_amd64` en `…/SHA256SUMS`, `sha256sum --check --ignore-missing SHA256SUMS`, `chmod +x`, en waar je hem neerzet; de vijf bestandsnamen. *Requirements*: macOS niet ondersteund geldt alleen voor `lighthouse serve` (systemd); de CLI draait op macOS, de hooks zijn daar niet getest (`date -d` in `usage-guard.sh` is GNU); Windows: binary bestaat, hooks vragen WSL. "Go, to install the binary" wordt optioneel. De `GOPRIVATE`-tekst en "The repository is private" vervallen.
- `docs/getting-started.md`, sectie *Install*: de binary-route eerst (zelfde `curl`/`SHA256SUMS`/`chmod +x` als `docs/install.md`, of een korte versie met een link daarheen), dan `go install …@latest` zonder `GOPRIVATE`; "Prebuilt release binaries are not published yet" en de private-repo-uitleg vervallen. De alinea over een tag in plaats van `main` blijft.
- `CONTRIBUTING.md`, onder *Sending a pull request*: één bullet dat elke PR een fragment in `changelog.d/` toevoegt, met een relatieve link naar de sectie *Changelog fragments* van `docs/releasing.md`.

- [ ] **Stap 4: groen zien** — `uv run pytest tests/test_instruction_files.py tests/test_getting_started.py tests/pack/test_md_links.py -q` → PASS.

- [ ] **Stap 5: commit** — `docs(release): releasebeleid, installatie van een binary, regel in AGENTS.md (#86)`

### Taak 7: de fragmenten van deze PR

**Bestanden:**
- Nieuw: `changelog.d/issue-86-releasebeleid.md` t/m `-4.md`

Exact (vier fragmenten, de check en `release` slikken ze):

| Bestand | type | Tekst |
|---|---|---|
| `issue-86-releasebeleid.md` | `added` | Every GitHub release now carries prebuilt binaries for Linux, macOS and Windows (amd64 and arm64 where available) with a `SHA256SUMS` file, so you can install lighthouse without Go. |
| `issue-86-releasebeleid-2.md` | `added` | Releases are automatic: a merged fix ships as a patch release, everything else in the weekly minor release; the policy is in `docs/releasing.md`. `/lighthouse-verify-pr` writes a changelog fragment in repositories that opt in with `changelog.d/.lighthouse`. |
| `issue-86-releasebeleid-3.md` | `fixed` | lighthouse builds on Windows again; `lighthouse serve` reports that its database lock is not supported there. |
| `issue-86-releasebeleid-4.md` | `changed` | Since v0.1.6: lighthouse's own code is in English and a code-language hook checks it, blocking Stop gates stay quiet while a valid usage alarm is set, the repository has a getting-started guide, contribution guidelines and a CLA check, and work started on a web UI (release binaries still serve a placeholder page). |

Controleer vóór het schrijven met `git log --oneline v0.1.6..origin/main` of de laatste rij nog volledig is; voeg een merge van na vandaag toe. Geen belofte over de SPA in de binary: `build.yml` draait geen `npm`-build (#78).

- [ ] **Stap 1: schrijven en committen** — `docs(changelog): fragmenten voor #86`. Eerst committen: `check` en de droogloop lezen alleen gecommitte staat.

- [ ] **Stap 2: proeven**

Run: `python3 ci/changelog.py check --base origin/main --head HEAD` → exit 0.
Run (in een wegwerpkopie): `d=$(mktemp -d) && git worktree add -q --detach "$d/wt" HEAD && python3 ci/changelog.py release --event push --root "$d/wt"; git -C "$d/wt" diff --stat; git worktree remove --force "$d/wt"` → `v0.2.0`, `CHANGELOG.md` gewijzigd, vier fragmenten weg, `.lighthouse` blijft. (`release` leest de tags uit `--root`; de worktree deelt ze.)

### Taak 8: bewijs vóór de merge, en de PR

Corneel gaf op 2026-10-27 akkoord voor de uitgaande stappen van deze taak: branch pushen, `v0.2.0-rc.1` pushen en met prerelease weer verwijderen, de PR maken. Mergen doet hij zelf.

- [ ] **Stap 0: preflight op de bot-push (read-only)** — `gh api repos/CorneeldH/lighthouse/rulesets --jq length` → `0`; `gh api repos/CorneeldH/lighthouse/branches/main/protection` → HTTP 404 `Branch not protected`; `gh api repos/CorneeldH/lighthouse/actions/permissions/workflow --jq .default_workflow_permissions` → `read` (een job mag dan zelf `contents: write` vragen). Gemeten op 2026-10-27 met precies deze uitkomsten. Wijkt een uitkomst af (ruleset of protection op `main`): stop vóór de PR en meld het; de bot heeft dan een bypass nodig (spec, besluit 6), en dat is Corneels instelling.
- [ ] **Stap 1: push** — `git push -u origin issue-86-releasebeleid`; CI groen, ook de job `changelog`.
- [ ] **Stap 2: prerelease als bewijs voor AC 2 en 3** — `git tag v0.2.0-rc.1 && git push origin v0.2.0-rc.1`. Wacht op `build.yml`. Verwacht: prerelease `v0.2.0-rc.1` met vijf binaries en `SHA256SUMS`, de stap met `lighthouse $TAG` groen. Controleer zelf: `gh release view v0.2.0-rc.1 --json assets --jq '.assets[].name'` (zes namen) en download `lighthouse_linux_amd64`, `sha256sum --check --ignore-missing SHA256SUMS`, `./lighthouse_linux_amd64 version` → `lighthouse v0.2.0-rc.1`. Noteer de run-URL voor de PR.
- [ ] **Stap 3: opruimen** — `gh release delete v0.2.0-rc.1 --cleanup-tag --yes && git tag -d v0.2.0-rc.1`.
- [ ] **Stap 4: PR** — `/lighthouse-verify-pr 86`. AC 2 en 3: de run-URL uit stap 2 is geen allowlist-commando; schrijf ze als `after merge` als er geen herhaalbaar commando is (het bewijs dan in `## Verification`), en AC 5 is `after merge`: het wordt `Refs #86 — <reden>`, of `Closes` als elk AC een geldige rij heeft. Verify-pr bepaalt dat, niet dit plan. De vier fragmenten uit taak 7 dekken alle wijzigingen; verify-pr schrijft er geen bij (taak 5).
- [ ] **Stap 5: overdracht** — de uitvoering stopt hier. `/lighthouse-handoff` met taak 9 als eerste stap; die start pas na de merge door Corneel.

### Taak 9: na de merge (AC 5)

- [ ] **Stap 1: is hij gemerged?** — `gh pr view <nr> --json state --jq .state` → `MERGED`. Anders: stop, niets te doen.
- [ ] **Stap 2: release controleren** — release-run groen; `gh release view v0.2.0 --json assets --jq '.assets[].name'` → vijf binaries en `SHA256SUMS`; `curl -fsSLO https://github.com/CorneeldH/lighthouse/releases/download/v0.2.0/lighthouse_linux_amd64` zonder login → exit 0; `chmod +x` en `version` → `lighthouse v0.2.0`. Mislukt de build: herstel volgens `docs/releasing.md` (dispatch `build.yml` met `tag: v0.2.0`). Daarna `/lighthouse-close-issue 86`.
