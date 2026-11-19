# Forge-neutrale skills (#407, deel 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** De skills `fix-pr`, `write-handoff`, `review-plan`, `upgrade` en `audit-worktrees` roepen
geen `gh` meer aan voor een forge-bewerking, `requires.toml` kent een eis per forge, en een
contracttest bewaakt dat.

**Architecture:** Wat de skills nu met `gh` lezen, komt achter `lighthouse forge` (Go, GitHub en
GitLab naast elkaar, zoals #260 de keten van `start-issue` tot `write-pr` deed). Wat echt per forge
verschilt (CI-rerun, roborev), krijgt een sectie `§ci` in `pack/core/skills/forge/<kind>.md`. Dat
is de uitzondering die het eerste AC toestaat. `requires.toml` krijgt een tabel `[forge.<kind>]`,
en `init` meldt alleen de eisen van de forge van de repo.

**Tech Stack:** Go (cobra, `internal/forge`, `internal/cli`, `internal/packs`, `internal/initcmd`,
`internal/kpi`), Python-pytest (`tests/pack`), Markdown-skills onder `pack/core/skills/`.

**Spec:** issue #407 (geen aparte spec). Scopekeuze van de gebruiker op 2026-11-19: twee plannen en
twee PR's. Dit is plan 1, en de PR krijgt `Refs #407`. Plan 2 zet de Go-pakketten
`internal/postartefact`, `internal/worktreeaudit` en `internal/labels` om. Die PR sluit #407.

## Global Constraints

- Instructies, skilltekst en commentaar zijn Engels; dit plan, commits, issue en PR zijn Nederlands (`[project] language = nl`).
- Commits zijn Conventional Commits, één logische wijziging per commit, en worden gemaakt in de worktree `/home/corneel/repositories/lighthouse/.claude/worktrees/issue-407-overige-gh-units`.
- Een wijziging onder `pack/` draait `go run ./cmd/lighthouse init --upgrade` en commit de geïnstalleerde kopie; CI eist daarna `No changes.`
- Beide varianten (`claude/` en `agents/`) van een skill veranderen samen; een skill met één `SKILL.md` heeft er één.
- `gh auth token` blijft toegestaan (`internal/forge/token.go`, `internal/cli/ask.go`).
- Elke forge-aanroep in een skill draait met `dangerouslyDisableSandbox: true` (Go-TLS).
- Go volgt `/lighthouse-go-standard`; tests volgen `/lighthouse-test-standard`.
- Een nieuwe methode op `forge.Forge` krijgt een implementatie op `*GitHub` én `*GitLab`, en op elke fake die het interface vervult (`go build ./...` en `go vet ./...` vinden ze).
- Geen derde forge (No-Go van milestone 9).

## Review Focus

1. **Een repo zonder `[forge] kind` en zonder herkenbare remote.** `init` kent de forge dan niet. Verwachting: alle eisen worden gemeld, ook `gh`, want twijfel meldt liever te veel dan te weinig. Test in Task 1.
2. **Een PR die GitHub nog aan het doorrekenen is** (`mergeable: null`). Dat mag nooit als `conflicting` lezen. Verwachting: `unknown`. Test in Task 2.
3. **Een PR zonder CI-run of zonder pipeline.** `pr checks` geeft dan `[]` en exit 0, geen fout. De skill meldt dan "no fail check". Test in Task 3.
4. **Een check-run die niet van Actions komt** (een externe app). Die heeft geen joblog: `--failed-log` meldt `no log for job <id>` en gaat door met de volgende job. Test in Task 3.
5. **`issue status <nr>` in een repo zonder `[forge.board]`.** Verwachting: `{"board": false, "on_board": false, "status": null}` en exit 0, zodat `write-handoff` dan `board: skipped` meldt. Test in Task 4.

---

### Task 1: `requires.toml` per forge

**Files:**
- Modify: `internal/packs/packs.go:86-90` (`Requirement`), `:580-591` (`parseRequires`)
- Modify: `internal/initcmd/initcmd.go:172`, `:187-188`, `:531-558` (`configTables`)
- Modify: `internal/initcmd/select.go:213` (naast `mergeRequires`)
- Modify: `pack/core/requires.toml`, `pack/python/requires.toml`
- Test: `internal/packs/packs_test.go`, `internal/initcmd/initcmd_test.go`

**Interfaces:**
- Produces: `packs.Requirement{Tool string; NeededBy []string; Forge string}`. Een lege `Forge` geldt voor elke forge.
- Produces: `func forgeRequires(reqs []packs.Requirement, kind string) []packs.Requirement` in `internal/initcmd/select.go`. Die houdt een eis als `r.Forge == ""`, `kind == ""` of `r.Forge == kind`.
- Produces: `configTables` geeft `(tables []projectconfig.Table, haveRepo bool, kind string, err error)`. `kind` is de bestaande `[forge] kind`, anders de kind van de remote, anders `""`.

Formaat van `requires.toml` (de sleutels op het hoogste niveau blijven zoals ze zijn):

```toml
git = ["…"]

[forge.github]
gh = ["close-issue", "fix-pr", "forge", "write-handoff", "audit-worktrees", "write-pr"]
```

- [ ] **Step 1: Write the failing tests**

`internal/packs/packs_test.go`:
- `TestParseRequiresReadsForgeTables`: de invoer `git = ["a"]` plus `[forge.github]` met `gh = ["fix-pr"]` geeft precies `{Tool:"gh", NeededBy:["fix-pr"], Forge:"github"}` en `{Tool:"git", NeededBy:["a"], Forge:""}`, gesorteerd op `Tool`.
- `TestParseRequiresRefusesUnknownForge`: `[forge.gitea]` met `gh = ["x"]` geeft een fout die `gitea` bevat.

`internal/initcmd/initcmd_test.go`, met de Deps-fake van de bestaande requires-test (`:32`) die elke tool als ontbrekend meldt:
- `TestInitReportsGhOnlyOnGitHub`: een tabeltest over `kind`. Bij `github` bevat stderr `missing dependency: gh`. Bij `gitlab` bevat stderr die regel niet, maar wel `missing dependency: git`. Bij een repo zonder `[forge] kind` en zonder remote bevat stderr `missing dependency: gh`.

- [ ] **Step 2: Run, verify FAIL**

Run: `go test ./internal/packs/ ./internal/initcmd/ -run 'TestParseRequires|TestInitReportsGhOnlyOnGitHub'`
Verwacht: FAIL, de TOML-decode van `[forge.github]` in een `map[string][]string` faalt.

- [ ] **Step 3: Implement**

`parseRequires` decodeert naar `map[string]any`. De sleutel `forge` is een tabel `kind → tool → []string`. Alleen `github` en `gitlab` zijn toegestaan; elke andere kind geeft `fmt.Errorf("requires.toml: unknown forge %q", kind)`. Elke andere sleutel is een `[]string`. In `initcmd.go` gaat `forgeRequires(reqs, kind)` vóór `mergeRequires`.

- [ ] **Step 4: Pas de TOML-bestanden aan**

In `pack/core/requires.toml` verdwijnt de regel `gh` van het hoogste niveau. Hij komt onder `[forge.github]` terug met precies `close-issue`, `fix-pr`, `forge`, `write-handoff`, `audit-worktrees` en `write-pr`:
- `write-pr` blijft erin, want Evidence-rijen mogen `gh` draaien (zie de tabel bij `SKILL.md:243`).
- `write-handoff` en `audit-worktrees` blijven erin tot plan 2, door `post-artefact` en `worktree-audit`.

In `pack/python/requires.toml` gaat `gh = ["quality/ratchet.py nightly"]` naar `[forge.github]`.

- [ ] **Step 5: Run, verify PASS**

Run: `go test -race ./internal/packs/... ./internal/initcmd/...`
Verwacht: PASS, ook `core_test.go:95,216` die over `p.Requires` lopen.

- [ ] **Step 6: Commit**

`feat(init): requires.toml kent een eis per forge (#407)`

---

### Task 2: `pr view` met draft, mergeable, head/base en comments

**Files:**
- Modify: `internal/forge/write_issue.go:46-54` (`Change`), `:269-310` (`ghPull`, `change()`)
- Modify: `internal/forge/gitlab_mr.go:13-30`, `internal/forge/gitlab_write.go:38-53`
- Modify: `internal/forge/forge.go` (methode `ChangeComments`)
- Modify: `internal/cli/forgecmd_render.go:48-54`, `internal/cli/forgecmd_read.go:169` (`pr view`)
- Test: `internal/forge/write_issue_test.go`, `internal/forge/gitlab_mr_test.go`, `internal/cli/forgecmd_test.go`

**Interfaces:**
- Produces: `forge.Change.Draft bool` en `forge.Change.Mergeable string`. `Mergeable` is altijd `mergeable`, `conflicting` of `unknown`.
- Produces: `ChangeComments(ctx context.Context, number int) ([]protocol.Comment, error)` op `forge.Forge`. Op GitHub zijn dat de issue-comments van de PR (`Comments`). Op GitLab zijn het de notes van de MR, zonder system notes, over alle pagina's.
- Produces: de JSON van `lighthouse forge pr view <n> [--comments]` krijgt de sleutels `draft`, `mergeable`, `head_ref`, `base_ref` en, met `--comments`, `comments` in de vorm van `renderComments`.

- [ ] **Step 1: Write the failing tests**

- GitHub, tabeltest `TestGitHubChangeMergeable`. Pull-JSON `"mergeable": true` geeft `mergeable`, `false` geeft `conflicting` en `null` geeft `unknown`. `"draft": true` geeft `Draft == true`.
- GitLab, tabeltest `TestGitLabChangeMergeable`. `has_conflicts: true` geeft `conflicting`. `detailed_merge_status` geeft `unknown` voor elk van `unchecked`, `checking`, `preparing` en `approvals_syncing`. `conflict` geeft `conflicting`. `mergeable` en `not_approved` geven `mergeable`.
- `TestGitLabChangeCommentsSkipsSystemNotes`: twee notes, één met `"system": true`; alleen de andere komt terug.
- CLI, `TestForgePRViewRendersMergeFields`: met de fake forge van `forgecmd_test.go` heeft de JSON `draft`, `mergeable`, `head_ref` en `base_ref`, en met `--comments` ook een array `comments`.

- [ ] **Step 2: Run, verify FAIL**

Run: `go test ./internal/forge/ ./internal/cli/ -run 'Mergeable|ChangeComments|PRViewRendersMergeFields'`

- [ ] **Step 3: Implement**

- GitHub: `ghPull` krijgt `Draft bool \`json:"draft"\`` en `Mergeable *bool \`json:"mergeable"\``.
- GitLab: `glMR` heeft de velden al.
- `pr view` krijgt de vlag `--comments`, net als `issue view`.

- [ ] **Step 4: Run, verify PASS**

Run: `go build ./... && go test -race ./internal/forge/... ./internal/cli/...`

- [ ] **Step 5: Commit**

`feat(forge): pr view toont draft, mergeable, branches en comments (#407)`

---

### Task 3: `lighthouse forge pr checks`

**Files:**
- Create: `internal/forge/checks.go` (GitHub), `internal/forge/gitlab_checks.go` (GitLab)
- Modify: `internal/forge/forge.go` (twee methodes), `internal/cli/forgecmd_read.go` (subcommando onder `pr`)
- Test: `internal/forge/checks_test.go`, `internal/forge/gitlab_checks_test.go`, `internal/cli/forgecmd_test.go`

**Interfaces:**
- Produces: `type Check struct { Name, Bucket, Link string; Job int64 }`. `Bucket` is een van `pass`, `fail`, `pending`, `skipping` of `cancel`. `Job` is 0 als de check geen joblog heeft.
- Produces: `Checks(ctx context.Context, number int) ([]Check, error)` en `JobLog(ctx context.Context, job int64) (string, error)` op `forge.Forge`.
- Produces: `lighthouse forge pr checks <n> [--field PATH] [--failed-log]`. Zonder `--failed-log` print het een JSON-array met de sleutels `name`, `bucket`, `link` en `job`. Met `--failed-log` print het per check met `bucket == "fail"` een kopregel `--- job <job> <name> ---` en daaronder de ruwe log. Is er geen log (`Job == 0`, of een 404), dan print het `no log for job <job>` en gaat het door. De exit is 0 welke buckets er ook zijn; alleen een fout van de forge geeft exit 1.

Bucket-tabel:

| Bron | pass | fail | pending | skipping | cancel |
|---|---|---|---|---|---|
| GitHub check-run (`commits/{head sha}/check-runs`) | conclusion `success`, `neutral` | `failure`, `timed_out`, `action_required`, `startup_failure` | status niet `completed` | `skipped` | `cancelled` |
| GitHub commit-status (`commits/{sha}/status`) | `success` | `failure`, `error` | `pending` | — | — |
| GitLab job van de nieuwste MR-pipeline (`merge_requests/:iid/pipelines`, daarna `pipelines/:id/jobs`) | `success`, of `failed` met `allow_failure` | `failed` | `created`, `waiting_for_resource`, `preparing`, `pending`, `running`, `scheduled` | `skipped`, `manual` | `canceled` |

Joblog: op GitHub is dat `GET actions/jobs/{job}/logs` (volgt de redirect); de id van de check-run is de job-id. Op GitLab is dat `GET jobs/:id/trace`. Een commit-status heeft `Job == 0`.

- [ ] **Step 1: Write the failing tests**

- `TestGitHubChecksBuckets`: een fake server met één check-run per rij van de tabel en één commit-status. De buckets komen exact overeen met de tabel, en `Link` is `html_url` of `target_url`.
- `TestGitLabChecksBuckets`: hetzelfde voor de GitLab-jobs, met `allow_failure` als aparte rij.
- `TestChecksEmptyWithoutRuns`: zonder check-runs, statussen of pipelines komt `[]` terug, zonder fout.
- `TestForgePRChecksFailedLog`: de fake forge heeft twee fail-checks, één met `Job == 0`. De output bevat `--- job 7 build ---` met de logtekst en `no log for job 0`, en de exit is 0.

- [ ] **Step 2: Run, verify FAIL**

Run: `go test ./internal/forge/ ./internal/cli/ -run 'Checks|PRChecks'`

- [ ] **Step 3: Implement** (methodes en subcommando zoals hierboven; paginatie volgt het bestaande `Link`- en `X-Next-Page`-patroon van de client)

- [ ] **Step 4: Run, verify PASS**

Run: `go build ./... && go test -race ./internal/forge/... ./internal/cli/...`

- [ ] **Step 5: Commit**

`feat(forge): pr checks leest CI-status en joblogs op beide forges (#407)`

---

### Task 4: `lighthouse forge issue status <nr>` leest

**Files:**
- Modify: `internal/cli/forgecmd_write.go:233-260` (`newForgeIssueStatusCommand`)
- Test: `internal/cli/forgecmd_write_test.go`

**Interfaces:**
- Produces: `lighthouse forge issue status <nr>` met één argument. Het leest via `IssueItem` en print `{"board": true, "on_board": <ok>, "status": <item.Status, of null als die leeg is>}`. Zonder `status_options` (`env.statusNames() == nil`) print het `{"board": false, "on_board": false, "status": null}` zonder forge-aanroep. Met twee argumenten werkt het zoals nu. De `Use` wordt `status <number> [<status>]` en `Args` wordt 1 tot 2.

- [ ] **Step 1: Write the failing tests**

`TestForgeIssueStatusReads` (tabel):
- Op het board met `In progress` geeft `{"board":true,"on_board":true,"status":"In progress"}`.
- Niet op het board geeft `on_board:false` en `status:null`.
- Zonder `[forge.board]` geeft `board:false`, en de fake meldt nul aanroepen.

- [ ] **Step 2: Run, verify FAIL**

Run: `go test ./internal/cli/ -run TestForgeIssueStatusReads`

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run, verify PASS**

Run: `go test -race ./internal/cli/...`

- [ ] **Step 5: Commit**

`feat(forge): issue status zonder status leest de boardstatus (#407)`

---

### Task 5: `lighthouse releases`

**Files:**
- Create: `internal/cli/releases.go`, `internal/cli/releases_test.go`
- Modify: de plek waar `newSelfUpdateCommand` aan de root hangt (zelfde registratiebestand)

**Interfaces:**
- Consumes: `forge.ReleaseReader` (`internal/forge/releases.go:18`) en `forge.New("", token)` met `token, _ := forge.Token("github", "", os.Getenv)`, zoals in `internal/cli/pins.go:18-19`.
- Produces: `lighthouse releases [--repo owner/name] [--field PATH]`. Standaard is de repo `CorneeldH/lighthouse`, dezelfde waarde als `selfupdate.Options.Repo`. Het print een JSON-array met de sleutels `tag`, `name`, `body`, `url`, `draft` en `prerelease`, in de volgorde van de forge (nieuwste eerst). `--field` werkt via `render` zoals bij `lighthouse forge`. Een fout geeft exit 1 en een regel op stderr.
- Produces: `newReleasesCommandWith(rr forge.ReleaseReader) *cobra.Command` voor tests.

- [ ] **Step 1: Write the failing tests**

- `TestReleasesPrintsJSON`: een fake `ReleaseReader` met twee releases, de nieuwste een prerelease. De output is exact de verwachte array, in die volgorde.
- `TestReleasesField`: `--field tag` print twee regels met de tags.
- `TestReleasesError`: de fake geeft een fout, dus exit 1.

- [ ] **Step 2: Run, verify FAIL**

Run: `go test ./internal/cli/ -run TestReleases`

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run, verify PASS**

Run: `go test -race ./internal/cli/...`

- [ ] **Step 5: Commit**

`feat(cli): lighthouse releases leest de releases van lighthouse (#407)`

---

### Task 6: KPI `diagnose` herkent `lighthouse forge pr`

**Files:**
- Modify: `internal/kpi/classify.go:56-60` (`diagnosticRe`)
- Test: `internal/kpi/fixpr_test.go` of `internal/kpi/classify_test.go` (waar de bestaande `isDiagnostic`-gevallen staan)

**Interfaces:**
- Produces: `diagnosticRe` matcht ook `lighthouse⟨ws⟩+forge⟨ws⟩+pr⟨ws⟩+(view|checks)\b`.

- [ ] **Step 1: Failing test**

`TestIsDiagnosticForgePR`:
- `lighthouse forge pr view 12` geeft true.
- `lighthouse forge pr checks 12 --failed-log` geeft true.
- `lighthouse forge pr create --title x` geeft false.

- [ ] **Step 2: Run FAIL**

Run: `go test ./internal/kpi/ -run TestIsDiagnosticForgePR`

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run PASS**

Run: `go test -race ./internal/kpi/...`

- [ ] **Step 5: Commit**

`fix(kpi): diagnose telt lighthouse forge pr view|checks (#407)`

---

### Task 7: Contracttest, `review-plan`, `upgrade` en `audit-worktrees`

**Files:**
- Create: `tests/pack/test_forge_neutral_units.py`
- Modify: `pack/core/skills/review-plan/{claude,agents}/SKILL.md` (`:160`, `:164`, `:207`)
- Modify: `pack/core/skills/upgrade/{claude,agents}/SKILL.md` (`:26-67`)
- Modify: `pack/core/skills/audit-worktrees/SKILL.md:148-149`
- Modify (only if pinned): `tests/pack/test_skill_contracts.py`

**Interfaces:**
- Produces: `UNITS` in de test. Task 8 voegt `write-handoff` toe, Task 9 voegt `fix-pr` toe.

- [ ] **Step 1: Write the failing test**

```python
FORGE_OP = re.compile(r"\bgh\s+(issue|pr|api|label|project|release|run)\b")
UNITS = ["review-plan", "upgrade", "audit-worktrees"]

@pytest.mark.parametrize("unit", UNITS)
def test_unit_calls_no_gh_for_a_forge_operation(unit: str) -> None:
    hits = [
        f"{p.relative_to(CORE)}:{i}: {line.strip()}"
        for p in sorted((CORE / "skills" / unit).rglob("*.md"))
        for i, line in enumerate(p.read_text(encoding="utf-8").splitlines(), 1)
        if FORGE_OP.search(line)
    ]
    assert hits == []

def test_gh_is_required_only_on_github() -> None:
    for path in sorted(PACKS.glob("*/requires.toml")):
        data = tomllib.loads(path.read_text(encoding="utf-8"))
        assert "gh" not in data, path
        assert set(data.get("forge", {})) <= {"github", "gitlab"}, path
```

`CORE` en `PACKS` komen uit `tests.pack._testkit`. Docstring: de controle is per regel. `gh run` en `gh release` vallen er ook onder: dat zijn geen forge-bewerkingen in de zin van het AC, maar de skill heeft ze niet meer nodig.

- [ ] **Step 2: Run, verify FAIL**

Run: `uv run pytest -q tests/pack/test_forge_neutral_units.py`
Verwacht: FAIL op `review-plan`, `upgrade` en `audit-worktrees`. `test_gh_is_required_only_on_github` slaagt al door Task 1.

- [ ] **Step 3: `review-plan` (beide varianten)**

Vervang regel 160 door `lighthouse forge issue view <N> --field body > <ATTEMPT>/issue-<N>.md`. Regel 164 noemt nu `lighthouse forge`, met `dangerouslyDisableSandbox: true`. Regel 207 verwijst naar "the same `lighthouse forge issue view` line as step 4".

- [ ] **Step 4: `upgrade` (beide varianten)**

- `N=$(lighthouse releases | jq -r '[.[] | select((.prerelease or .draft) | not)][0].tag // empty')`. Is `N` leeg of faalt de aanroep, dan geldt het bestaande pad "self-update not offered".
- Lijst van tags: `lighthouse releases | jq -r '.[] | select((.prerelease or .draft) | not) | .tag'`. Sorteer met `sort -V`, zoals nu.
- Body: `lighthouse releases | jq -r --arg t "<tag>" '.[] | select(.tag == $t) | .body'`.
- De netwerkzinnen op regels 26, 33 en 58 noemen `lighthouse releases` in plaats van `gh`.

- [ ] **Step 5: `audit-worktrees`**

Vervang de valkuil op regels 148-149 door: "`lighthouse worktree-audit` reads the PR state itself; call no forge CLI next to it." Laat de rest van de skill staan.

- [ ] **Step 6: Run, verify PASS**

Run: `uv run pytest -q tests/pack/`
Verwacht: PASS. Een bestaande test die de oude `gh`-regels vastpint, pas je aan naar de nieuwe regel en niet weg.

- [ ] **Step 7: Mutatiecheck**

Zet `gh api "repos/$R/issues/<N>"` tijdelijk terug in `review-plan/claude/SKILL.md`. De test faalt dan met die regel in de melding. Draai het terug.

- [ ] **Step 8: Commit**

`feat(skills): review-plan, upgrade en audit-worktrees zonder gh (#407)`

---

### Task 8: `write-handoff` op `lighthouse forge`

**Files:**
- Modify: `pack/core/skills/write-handoff/SKILL.md` (`:80`, `:150-170`, `:286`, `:295-303`)
- Modify: `tests/pack/test_forge_neutral_units.py` (voeg `"write-handoff"` toe aan `UNITS`)
- Modify (only if pinned): `tests/pack/test_skills_issue_flow.py` (handoff-rijen)

**Interfaces:**
- Consumes: `lighthouse forge issue status <nr>` (Task 4), `lighthouse forge issue comment <nr> --body-file F`, `lighthouse forge issue status <nr> Controller`.

- [ ] **Step 1: Test eerst**

Voeg `write-handoff` toe aan `UNITS`. Run `uv run pytest -q tests/pack/test_forge_neutral_units.py`. Verwacht: FAIL op regels 162, 286, 297 en 302.

- [ ] **Step 2: Stap 2 (lezen)**

Vervang het hele blok `gh api graphql …` door `lighthouse forge issue status <nr>`. `board: false` betekent `board: skipped — no [forge.board] configured`. Exit 2 betekent stop. De regel over `forge.board.project_number` met rc 1 en de zin over `gh project item-list` vervallen. Er is geen item-ID meer nodig.

- [ ] **Step 3: Stap 5 (posten)**

Schrijf het commentaar eerst naar `<scratch>/handoff.md`, met de bestaande heredoc maar met `> <scratch>/handoff.md` in plaats van de pipe. Post daarna met `lighthouse forge issue comment <nr> --body-file <scratch>/handoff.md`.

- [ ] **Step 4: Stap 6 (status)**

Het blok `item-add`/`item-edit` wordt `lighthouse forge issue status <nr> Controller`. Die zet het issue zo nodig eerst op het board.

- [ ] **Step 5: Run PASS**

Run: `uv run pytest -q tests/pack/`

- [ ] **Step 6: Commit**

`feat(write-handoff): board en comment via lighthouse forge (#407)`

---

### Task 9: `fix-pr` op `lighthouse forge`, plus `§ci` per forge

**Files:**
- Modify: `pack/core/skills/fix-pr/{claude,agents}/SKILL.md` (`:30-100`, `:158`, `:173`, `:182`, `:205-226`, `:243`, `:255`, `:266-272`)
- Modify: `pack/core/skills/forge/github.md` en `pack/core/skills/forge/gitlab.md` (nieuwe sectie `## §ci` na `§proxy`)
- Modify: `tests/pack/test_forge_neutral_units.py` (voeg `"fix-pr"` toe aan `UNITS`, en een test voor `§ci`)

**Interfaces:**
- Consumes: `pr view --comments` met `draft`, `mergeable` en `head_ref` (Task 2), en `pr checks [--failed-log]` (Task 3).

- [ ] **Step 1: Tests eerst**

Voeg `fix-pr` toe aan `UNITS`, plus:

```python
@pytest.mark.parametrize("kind", ["github", "gitlab"])
def test_forge_reference_has_ci_section(kind: str) -> None:
    assert "\n## §ci\n" in (CORE / "skills" / "forge" / f"{kind}.md").read_text(encoding="utf-8")
```

Run: verwacht FAIL.

- [ ] **Step 2: Het diagnoseblok (sectie 1)**

Het blok blijft één Bash-aanroep:
- `PR=$(lighthouse forge pr view "$N" --comments 2>&1)`, met de guard `no PR #$N — is it an issue? forge said: $PR`.
- De jq-regel: `"PR #\(.number) [\(.state)] draft=\(.draft) merge=\(.mergeable)\nbranch \(.head_ref)\n\(.url)\n\(.title)"`. `B` komt uit `.head_ref`.
- Checks: `lighthouse forge pr checks "$N" | jq -r '.[] | "\(.bucket)\t\(.name)\t\(.link)"'`. Een lege uitvoer is "no checks".
- Failure log: `lighthouse forge pr checks "$N" --failed-log | grep -aE '<huidige markers>|^--- job ' | tail -15 || echo …`.
- Roborev: dezelfde jq-filter op `printf '%s' "$PR" | jq -r '.comments[] | select((.author == "github-actions[bot]" or .author == "github-actions") and (.body | contains("roborev-pr-comment"))) | .body | @json'`, plus het `roborev-verwerkt`-filter op `.comments[].body`.
- De vier "measured, not taste"-punten noemen de nieuwe commando's. Het punt over `|| true` vervalt, want `pr checks` geeft exit 0. Zeg dat in één zin.

- [ ] **Step 3: Secties 4 tot 6**

- Sectie 4: de jobs-lookup en `gh run rerun` gaan naar `../lighthouse-forge/<kind>.md §ci`, met `<kind>` uit `lighthouse config forge.kind`. De skill verwijst ernaar.
- Sectie 5: "green" is dat `lighthouse forge pr checks <nr>` geen `fail` of `pending` heeft en dat `mergeable` `mergeable` is.
- Sectie 6: het roborev-commentaar opslaan gaat via `lighthouse forge pr view <nr> --comments` met dezelfde jq-filter. Posten gaat via `lighthouse forge pr comment <nr> --body-file <scratch>/comment.md`.
- Non-negotiable `:243` en KPI-tabel `:255`: `lighthouse forge` in plaats van `gh`.
- Valkuilen `:266-272`: `gh pr checks` exit 8 vervalt. "issue and PR do not share the number space" blijft, met `lighthouse forge pr view` als test. De rtk-valkuil verwijst naar `§proxy`. Bij `DIRTY` staat nu `mergeable: conflicting`.

- [ ] **Step 4: `§ci` in beide forge-bestanden**

- `github.md §ci`:
  - joblijst: `R=$(lighthouse config forge.repo) && gh api "repos/$R/actions/runs/<id>/jobs"`
  - rerun: `gh run rerun <id> --failed --repo "$R"`
  - roborev: een comment van `github-actions[bot]`
- `gitlab.md §ci`:
  - jobs van een pipeline: `lighthouse forge api "projects/<project>/pipelines/<id>/jobs"`
  - rerun van één job: `lighthouse forge api -X POST "projects/<project>/jobs/<job>/retry"`
  - `<project>` is de URL-gecodeerde `forge.repo`.
  - Roborev bestaat hier nog niet; sectie 6 vervalt (echte run: #358).

- [ ] **Step 5: Run PASS**

Run: `uv run pytest -q tests/pack/`. Pas pins in `test_skill_contracts.py` aan naar de nieuwe regels.

- [ ] **Step 6: Mutatiecheck**

Zet `gh pr checks "$N" --repo "$R" || true` tijdelijk terug in `fix-pr/agents/SKILL.md`. De test faalt dan. Draai het terug.

- [ ] **Step 7: Commit**

`feat(fix-pr): diagnose en roborev via lighthouse forge, CI-acties in §ci (#407)`

---

### Task 10: Dogfood, gate en issue-commentaar

**Files:**
- Modify: alles wat `go run ./cmd/lighthouse init --upgrade` schrijft (`.claude/skills/**`, `.agents/**`, `.lighthouse/manifest.toml`)
- Check: `docs/site/data/*.json`, `ci/literal-allowlist.txt` (alleen als een drift-check in de gate faalt)

- [ ] **Step 1: Dogfood**

Run: `go run ./cmd/lighthouse init --upgrade`. Commit de geïnstalleerde kopie als `chore(pack): installeer de forge-neutrale skills (#407)`. Run het nog een keer. Verwacht: `No changes.`

- [ ] **Step 2: Volledige gate**

Run de `[test_gate] command` uit `lighthouse.toml`: `lighthouse gate go -- go test -race`, de Python-gate en de node-gate.
Verwacht: alles groen. Bij rood volgt een fix in een nieuwe commit.

- [ ] **Step 3: Restscan**

Run: `git grep -nE '\bgh\s+(issue|pr|api|label|project)\b' -- pack/core/skills/{fix-pr,write-handoff,review-plan,upgrade,audit-worktrees}`
Verwacht: geen hits.

- [ ] **Step 4: Issue-commentaar (Nederlands) op #407**

Via `lighthouse forge issue comment 407 --body-file <scratch>/407.md`, met:
- `create_labels.py` bestaat niet. Labels maken zit in `internal/labels`, en dat gaat naar plan 2, samen met `internal/postartefact` en `internal/worktreeaudit`.
- Uitzondering met reden: `unblock-dependents` en de workflows in `.github/workflows` en `pack/core/workflows`. Die draaien alleen in GitHub Actions, op het event `issues: closed`, en `gh` staat daar op de runner. GitLab heeft geen equivalent in deze milestone.
- Uitzondering met reden: CI-rerun en roborev in `fix-pr` staan per forge in `lighthouse-forge §ci`.
- `close-issue` staat niet in de lijst van het issue en houdt voorlopig `gh`.
- De echte run op GitLab hoort bij #358.

- [ ] **Step 5: PR**

Via `/lighthouse-write-pr` (schrijft ook het fragment in `changelog.d/`). De body krijgt `Refs #407 — deel 1 van 2; plan 2 zet postartefact, worktreeaudit en labels om`.
