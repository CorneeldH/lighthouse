# Forge-neutrale Go-units (#407, deel 2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `post-artefact`, `labels`, `worktree-audit` en de workflows roepen geen `gh` meer aan voor een forge-bewerking, of de uitzondering staat met reden vast; daarmee zijn AC 1 en 2 van #407 af.

**Architecture:** De drie Go-pakketten (`internal/postartefact`, `internal/labels`, `internal/worktreeaudit`) krijgen een `forge.Forge` in hun `Env`, geopend via `forgeOpener.forRepo` (`internal/cli/forges.go:56`: `forge.Token` met de `gh auth token`-terugval, dan `forge.Open`), met de repo uit `repoconfig.LoadFile(config.Path())`. Wat het interface nog niet kan (commit bekend?, label maken, alle PR-koppen, PR's die een issue sluiten) komt als vier nieuwe methoden op `*GitHub` en `*GitLab`. De workflows blijven op `gh` (alleen GitHub Actions) met een uitzondering met reden in `lighthouse-forge`.

**Tech Stack:** Go (`internal/forge`, `forgetest`, `gitlabtest` en `forgesuite` voor de fakes en de suite), pytest voor de contracttest in `tests/pack`.

**Spec:** issue #407 (geen aparte spec); deel 1 is [het plan van 2026-11-19](2026-11-19-issue-407-forge-neutrale-skills.md), de nacontrole van #430 staat als comment op het issue. Dit plan sluit #407.

## Global Constraints

- Instructies, skilltekst en commentaar zijn Engels; dit plan, commits, issue en PR zijn Nederlands (`[project] language = nl`).
- Commits zijn Conventional Commits, één logische wijziging per commit, in de worktree van branch `issue-407-gitlab-forge-overige-units-met-gh-op`.
- Een wijziging onder `pack/` of onder `internal/` die verandert wat `init` schrijft draait `go run ./cmd/lighthouse init --upgrade`; de geïnstalleerde kopie wordt gecommit en CI eist daarna `No changes.`
- Beide varianten (`claude/` en `agents/`) van een skill veranderen samen.
- `gh auth token` blijft toegestaan (`internal/cli/forges.go`, `internal/cli/ask.go`); `internal/forge/token.go` kent de terugval zelf niet.
- De drie units openen de forge nooit met `laneEnvFrom` (geen `gh auth token`-terugval, leest alleen `<root>/lighthouse.toml`) maar met `forgeOpener.forRepo`, de repo uit `repoconfig.LoadFile(config.Path())` (dat eert `LIGHTHOUSE_TOML`, net als `Env.Config`). Een gebruiker die alleen via `gh auth login` is ingelogd houdt zo wat nu werkt. Een test dekt die terugval per unit.
- Een GitHub-forge in deze units neemt `prbody.GitHubAPIURLEnv` (`LIGHTHOUSE_GITHUB_API_URL`) als `BaseURL`, alleen via dezelfde loopback-keuze als `prbody/gate.go:86` (`loopbackAPIURL`, :100-110; exporteer of deel die helper): het token gaat met elk verzoek mee, een niet-loopback waarde wordt genegeerd en een test dekt dat. Zo draait het binary in `tests/pack` tegen een `httptest`-server.
- Een nieuwe methode op `forge.Forge` krijgt een implementatie op `*GitHub` én `*GitLab`, op `forgetest`, `gitlabtest` en elke andere fake die het interface vervult, plus een case in `internal/forge/forgesuite/suite.go` (`go build ./...` en `go vet ./...` vinden de fakes).
- Een foutpad "404 of 422 = bestaat niet" leest een typed fout van de forge, nooit de stderr-tekst van een proces.
- Geen derde forge (No-Go van milestone 9). Geen echte GitLab-run: die is #358; tests draaien op fakes en `httptest`.
- Go volgt `/lighthouse-go-standard`; tests volgen `/lighthouse-test-standard`.
- Elke PR krijgt een fragment in `changelog.d/`; de PR-body begint met `Closes #407`.

## Review Focus

- `post-artefact` op GitLab met een commit die de forge niet kent: de forge antwoordt 404, dat is `false`, geen fout; elk ander antwoord (401, 500, timeout) is een fout `artefact: forge failed: …`.
- `post-artefact` met een branch die alleen een gesloten of gemergde PR heeft: telt niet als open PR, want `BranchPRs` geeft alle statussen terug.
- `labels` zonder token of zonder `forge.repo`: valt terug op het printen van de ontbrekende labels, nooit een kale fout, en `init` blijft afronden met `warning: labels: not synchronised`.
- `labels` op GitLab: kleur is `#RRGGBB` met hekje, op GitHub zonder; een naam met een dubbele punt (`1-location:local`) en hoofdletters wordt niet gewijzigd.
- `worktree-audit` met een repo die de forge niet kan lezen: elke PR-status blijft `onbekend`, nooit "geen PR" (bestaand contract, `pulls` regel ~123).
- Een fork-PR (`CrossRepo`) telt niet als merged head van het issue, op beide forges.

---

### Task 1: `CommitKnown` en `CreateLabel` op de forge

**Files:**
- Modify: `internal/forge/forge.go` (interface), `internal/forge/github.go` of `write_issue.go` (naast `Labels`, regel ~443), `internal/forge/gitlab_write.go` (naast `Labels`, regel ~363)
- Modify: `internal/forge/forgetest/server_write.go`, `internal/forge/gitlabtest/server_write.go` (fakes)
- Test: `internal/forge/forgesuite/suite.go` (met nieuwe `Fixture`-methoden voor een commit en een bestaand label), `internal/forge/github_contract_test.go`, `internal/forge/gitlab_contract_test.go` (implementeren die methoden)

**Interfaces:**
- Produces: `CommitKnown(ctx context.Context, sha string) (bool, error)` — `true` als de forge de commit kent; een 404 of 422 is `false, nil`, elke andere fout wordt teruggegeven. GitHub: `GET repos/<repo>/commits/<sha>`; GitLab: `GET projects/<id>/repository/commits/<sha>`.
- Produces: `CreateLabel(ctx context.Context, name, color string) error` — `color` is zes hexcijfers zonder hekje; GitLab zet er zelf een `#` voor. Een label dat al bestaat is geen fout (409 GitHub 422 `already_exists`, GitLab 409).

- [ ] **Step 1: Schrijf de suite-cases die falen**

In `forgesuite/suite.go` zijn cases entries in `Cases`, subtests van `TestGitHubConformance` en `TestGitLabConformance` (geen losse `Test…`-functies). Naast de `Labels`-case: `CommitKnown` (bekende sha → `true`; onbekende sha → `false, nil`; een 500 → fout) en `CreateLabel` (nieuw label verschijnt in `Labels`; tweede keer dezelfde naam → `nil`; kleur komt aan zoals de forge hem verwacht).

- [ ] **Step 2: Run, verwacht rood**

Run: `go test ./internal/forge/... -run 'Conformance/(CommitKnown|CreateLabel)'`
Verwacht: build-fout, de methoden bestaan niet.

- [ ] **Step 3: Implementeer op `*GitHub`, `*GitLab` en beide fakes**

Gebruik de bestaande `c.do`-helper en de statusfout waar `checks.go:123` al `errors.As(err, &se)` op doet. `httpStatusError` (`github.go:235-249`) draagt alleen `Code` en `Message`, niet `errors[].code`: een 422 van `CreateLabel` telt pas als "bestaat al" na een nieuwe `Labels`-aanroep die de naam bevat (hoofdletterongevoelig); anders is het een fout, zodat een geweigerd label nooit als `labels: created` telt. Voeg de twee routes toe aan de fakes.

- [ ] **Step 4: Run, verwacht groen**

Run: `go build ./... && go vet ./... && go test -race ./internal/forge/...`
Verwacht: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/forge
git commit -m "feat(forge): CommitKnown en CreateLabel op GitHub en GitLab (#407)"
```

### Task 2: `PullHeads` en `IssueClosingHeads` op de forge

**Files:**
- Modify: `internal/forge/forge.go`, `internal/forge/board.go` (naast `BranchPRs`), `internal/forge/gitlab_mr.go`, fakes in `forgetest` en `gitlabtest`; `PullHeads`/`IssueClosingHeads`-fixturemethoden in `forgesuite` en beide `*_contract_test.go`
- Test: `internal/forge/forgesuite/suite.go`

**Interfaces:**
- Produces: `type PullHead struct{ Number int; State string; MergedAt string; HeadRef string }` en `PullHeads(ctx context.Context) ([]PullHead, error)` — elke PR of MR in elke staat, alle pagina's; `State` is `OPEN`, `CLOSED` of `MERGED` zoals `PR.State`.
- Produces: `IssueClosingHeads(ctx context.Context, number int) ([]ClosingHead, error)` met `type ClosingHead struct{ HeadRef, Repo, Body string; Merged, Closes, CrossRepo bool }` (`Closes`: sluit het issue, anders alleen een verwijzing) — de PR's die issue `number` sluiten of ernaar verwijzen (GitHub: dezelfde `closedByPullRequestsReferences` en `CROSS_REFERENCED_EVENT` als `issuePRsQuery`, via de bestaande GraphQL-helper in `graphql.go`; GitLab: `GET projects/<id>/issues/<iid>/closed_by` samen met `related_merge_requests`).

- [ ] **Step 1: Schrijf de suite-cases**

`TestPullHeads`: open, gesloten en gemergde PR komen alle drie terug met de juiste `State`; meer dan één pagina. `IssueClosingHeads`: een gemergde PR, een fork-PR (`CrossRepo` true), een PR die het issue alleen noemt (met en zonder `Refs #nr` in de body) en een PR uit een andere repo.

- [ ] **Step 2: Run, verwacht rood**

Run: `go test ./internal/forge/... -run 'Conformance/(PullHeads|IssueClosingHeads)'`
Verwacht: build-fout.

- [ ] **Step 3: Implementeer op beide forges en beide fakes**

Eén regel aanpak: GitHub hergebruikt de query-tekst uit `worktreeaudit/collect.go:303` (kopieer hem naar `internal/forge`; `worktreeaudit` houdt zijn kopie tot Task 5 hem weghaalt, anders breekt `go build` in Task 2); GitLab mapt `state: opened|closed|merged` naar `OPEN|CLOSED|MERGED`.

- [ ] **Step 4: Run, verwacht groen**

Run: `go build ./... && go vet ./... && go test -race ./internal/forge/...`
Verwacht: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/forge
git commit -m "feat(forge): PullHeads en IssueClosingHeads op GitHub en GitLab (#407)"
```

### Task 3: `post-artefact` op de forge

**Files:**
- Modify: `internal/postartefact/postartefact.go` (regels 41-209, 319, 436), `internal/cli/postartefact.go` (regels 29-53, Long-tekst), `internal/protocol/artefact.go` (regel ~280, de bronlink)
- Test: `internal/postartefact/postartefact_test.go` (regels 85-101 herschrijven), `internal/cli/postartefact_test.go` (`TestPostArtefactCases`)
- Modify (fixtures): `tests/pack/fixtures/scripts/post-artefact/cases.json` (29 van 38 cases scripten `gh`), `tests/pack/test_script_cases.py` (regels 24-29, 73-76), `tests/pack/_testkit.py`, `internal/scriptcase`

**Interfaces:**
- Consumes: `Forge.CommitKnown`, `Forge.BranchPRs`, `Forge.Comments`, `Forge.CreateComment` (Task 1 en het bestaande interface).
- Produces: `postartefact.Env` met veld `Forge func(root string) (forge.Forge, error)` naast `Run` (alleen nog voor `git`) en `Config`; `p.gh` en `p.repo` verdwijnen. `commentOf` verdwijnt: `protocol.Comment` komt kant-en-klaar uit `Forge.Comments`. De foutregel wordt `artefact: forge failed: <reden>`.

- [ ] **Step 1: Herschrijf de test-replay naar een fake forge**

`forgetest` levert de fake. Cases (bestaande namen behouden): commit onbekend → push, daarna post; commit bekend → geen push; branch met open PR → geen push; branch met alleen een gemergde PR → wel push; `CommitKnown`-fout 500 → exit 1 met `forge failed`; hetzelfde scenario op `gitlabtest`; header-bronlink wijst op GitLab naar de GitLab-host; `LIGHTHOUSE_TOML` buiten de root wordt gevolgd; zonder `GH_TOKEN` maar met `gh auth token` opent de forge.

- [ ] **Step 2: Run, verwacht rood**

Run: `go test ./internal/postartefact/... ./internal/cli/... -run PostArtefact`
Verwacht: FAIL.

- [ ] **Step 3: Zet `commitKnown`, `openPR`, `readComments` en `postComment` om**

`openPR` filtert `BranchPRs` op `State == "OPEN"`. `postComment` geeft `Comment.URL`. De forge opent `defaultPostArtefactEnv` via `forgeOpener.forRepo` met `repoconfig.LoadFile(config.Path())` (zie Global Constraints); een ontbrekend token (ook na `gh auth token`) geeft `artefact: no token for forge …`. `protocol.RenderArtefact` zet de bronlink hard op `https://github.com/<repo>/blob/<sha>/<pad>` (`artefact.go:280`): maak hem forge-bewust (GitLab: `<web>/<repo>/-/blob/<sha>/<pad>`, host uit `repoconfig`), met een test voor beide forges. Pas de `Long`-tekst aan: "not on the forge" in plaats van "not on GitHub".

- [ ] **Step 3b: Zet de replay-fixtures van post-artefact om**

Voeg in `internal/scriptcase` en `tests/pack/_testkit.py` (naast `fake_gh_bin`) een `httptest`-replay toe: de `gh`-calls van een case worden `forge_calls` (method, pad, status, body, in volgorde); het binary bereikt de server via `LIGHTHOUSE_GITHUB_API_URL` plus een dummy-token in `env`; `git` blijft echt; de assert "alle gescripte calls zijn gedaan" blijft, nu op de server. Schrijf `post-artefact/cases.json` om met behoud van `name` en `expect`; een case die vervalt gebeurt alleen met reden in het commitbericht. `unblock-dependents` en `_self` houden hun `gh`-replay.

- [ ] **Step 4: Run, verwacht groen**

Run: `go test -race ./internal/postartefact/... ./internal/cli/...` en `uv run pytest -q tests/pack/test_script_cases.py -k post-artefact` (38 cases verzameld, geen gedeselecteerd; minus wat met reden vervalt)
Verwacht: PASS. Controle: `git grep -n '"gh"' internal/postartefact internal/cli/postartefact.go` geeft geen hit.

- [ ] **Step 5: Commit**

```bash
git add internal/postartefact internal/protocol internal/scriptcase internal/cli/postartefact.go internal/cli/postartefact_test.go tests/pack
git commit -m "feat(post-artefact): leest en schrijft via lighthouse forge, ook op GitLab (#407)"
```

### Task 4: `labels` op de forge, plus `lighthouse forge label create`

**Files:**
- Modify: `internal/labels/labels.go` (regels 43-63, 162-260), `internal/initcmd/initcmd.go` (regel 251), `internal/cli/forge*.go` (het `forge`-subcommando, naast `issue` en `pr`), `docs/site/agents/install.md` (regels ~87 en ~190: "needs gh" en de `gh label create`-regels)
- Test: `internal/labels/labels_test.go`, `internal/initcmd/labels_test.go`, `internal/cli/forge_label_test.go` (nieuw)

**Interfaces:**
- Consumes: `Forge.Labels` en `Forge.CreateLabel` (Task 1).
- Produces: `labels.Sync(root string, create bool, f forge.Forge, out, errw io.Writer) error`; `f == nil` betekent "de forge is niet te openen" en valt terug op het print-pad. `runGH`, `SetRunGH`, `readRemote` en `createArgs` verdwijnen.
- Produces: `lighthouse forge label create <name> --color <hex>` en het fallbackregel-formaat `lighthouse forge label create '<name>' --color <hex>`, dat op beide forges werkt.

- [ ] **Step 1: Schrijf de falende tests**

`TestSyncCreatesMissingOnForge` (fake forge, GitHub en GitLab: ontbrekend label wordt gemaakt, bestaande niet, hoofdletterongevoelig vergeleken); `TestSyncFallbackPrintsForgeCommand` (forge `nil` → regels `lighthouse forge label create …`, geen `gh`); `TestSyncCreateErrorFallsBack` (`CreateLabel` faalt → die regel wordt geprint, de rest gemaakt); `TestForgeLabelCreate` voor de CLI. De bestaande `fakeGH`-tests worden omgezet, niet verwijderd.

- [ ] **Step 2: Run, verwacht rood**

Run: `go test ./internal/labels/... ./internal/initcmd/... ./internal/cli/... -run 'Label|Sync'`
Verwacht: build-fout.

- [ ] **Step 3: Implementeer**

`initcmd` opent de forge voor `Sync` via `forgeOpener.forRepo` met `repoconfig.LoadFile(config.Path())` (geen token, ook na `gh auth token` → `nil`, nooit een fout); een test dekt de `gh auth token`-terugval. Het `forge label create`-subcommando valt onder dezelfde `dangerouslyDisableSandbox`-regel als de andere forge-commando's.

- [ ] **Step 4: Run, verwacht groen**

Run: `go test -race ./internal/labels/... ./internal/initcmd/... ./internal/cli/...`
Verwacht: PASS; `git grep -n '"gh"' internal/labels` geeft geen hit.

- [ ] **Step 5: Commit**

```bash
git add internal/labels internal/initcmd internal/cli
git commit -m "feat(labels): maakt labels via lighthouse forge op beide forges (#407)"
```

### Task 5: `worktree-audit` op de forge

**Files:**
- Modify: `internal/worktreeaudit/collect.go` (regels 125, 283-329), `internal/cli/worktreeaudit.go` (regels 16, 82, 111)
- Test: `internal/worktreeaudit/twins_test.go` (regels 283-298, 533 omzetten), `internal/worktreeaudit/collect_test.go`, `internal/cli/worktreeaudit_test.go`
- Modify (fixtures): `tests/pack/fixtures/scripts/worktree-audit/cases.json` (18 van 24 cases scripten `gh`), met de `httptest`-replay uit Task 3 Step 3b

**Interfaces:**
- Consumes: `Forge.PullHeads`, `Forge.IssueClosingHeads` (Task 2).
- Produces: `worktreeaudit.Env` met `Forge forge.Forge` naast `Run` (alleen nog `git`); `collector.gh` verdwijnt. `IssuePRHeads(ctx, env, repo, nr)` houdt zijn signatuur en `false` bij elke fout; `ParseIssuePRHeads` krijgt `[]forge.ClosingHead` in plaats van het ruwe GraphQL-document en behoudt de filters van `parse.go:337-381`: alleen `Merged`, `!CrossRepo`, `Repo == slug`, en een alleen-verwijzende PR (`!Closes`) telt alleen met `Refs #nr` in `Body` (`prbody.RefsTargets`).

- [ ] **Step 1: Zet de twin-tests om**

De scriptcases met `gh api …` worden fake-forgeantwoorden; verwachte uitvoer blijft byte voor byte gelijk. Voeg toe: forge-fout → PR-status `onbekend` (Review Focus), fork-PR telt niet, een gemergde PR die het issue alleen noemt telt niet, een PR uit een andere repo telt niet.

- [ ] **Step 2: Run, verwacht rood**

Run: `go test ./internal/worktreeaudit/... ./internal/cli/...`
Verwacht: FAIL.

- [ ] **Step 3: Implementeer**

`pulls` bouwt zijn `[]any`-payload uit `PullHeads` (veldnamen `number`, `state`, `merged_at`, `head.ref` blijven, zodat de parser ongemoeid blijft; `state` wordt `open`/`closed` in kleine letters zoals GitHub REST).

- [ ] **Step 4: Run, verwacht groen**

Run: `go test -race ./internal/worktreeaudit/... ./internal/cli/...` en `uv run pytest -q tests/pack/test_script_cases.py -k worktree-audit` (24 cases verzameld; exit 5 "no tests collected" is rood)
Verwacht: PASS; `git grep -n '"gh"' internal/worktreeaudit` geeft geen hit.

- [ ] **Step 5: Commit**

```bash
git add internal/worktreeaudit internal/cli/worktreeaudit.go internal/cli/worktreeaudit_test.go tests/pack
git commit -m "feat(worktree-audit): leest PR's via lighthouse forge, ook op GitLab (#407)"
```

### Task 6: Workflows als uitzondering met reden

**Files:**
- Modify: `pack/core/skills/forge/github.md` en `gitlab.md` (nieuwe sectie `## §workflows`, laatste in beide), `pack/core/requires.toml` (haal `write-handoff` en `audit-worktrees` uit `[forge.github] gh`: ze roepen `gh` niet meer aan; zie plan 1, regel 90), `internal/initcmd` (workflow-units niet schrijven bij `forge.kind = "gitlab"`; `fillWorkflows` in `workflows.go:20-49` kent geen kindvoorwaarde)
- Test: `tests/pack/test_forge_neutral_units.py`, `tests/pack/test_skill_contracts.py` (`FORGE_SECTIONS` krijgt `"## §workflows"` aan het eind), een nieuwe test in `internal/initcmd`

**Interfaces:**
- Produces: `§workflows` in beide forgebestanden. GitHub: `lighthouse unblock-dependents` (`internal/unblock/unblock.go:72-84`) roept zelf `gh` aan en draait alleen in GitHub Actions op `issues: closed`; `gh release download` haalt daar het lighthouse-binary op de runner, en `.github/workflows/build.yml` publiceert de release van lighthouse zelf met `gh release`. GitLab: geen equivalent in deze milestone; een GitLab-repo draait de workflows niet en `lighthouse init` schrijft ze er niet.

- [ ] **Step 1: Test die het ontbreken van de sectie vindt**

In `test_forge_neutral_units.py`: `test_forge_files_have_workflows_section` (beide bestanden bevatten `\n## §workflows\n`) en `test_requires_toml_drops_gh_for_converted_units` (`write-handoff` en `audit-worktrees` staan niet meer onder `[forge.github] gh`, wel nog onder `git`); in `internal/initcmd` een test: bij `forge.kind = "gitlab"` schrijft `init` geen `lighthouse-*.yml`, bij GitHub wel.

- [ ] **Step 2: Run, verwacht rood**

Run: `uv run pytest -q tests/pack/test_forge_neutral_units.py tests/pack/test_skill_contracts.py` en `go test ./internal/initcmd/...`
Verwacht: FAIL.

- [ ] **Step 3: Schrijf de secties en haal de notities weg**

Voeg de GitLab-voorwaarde in `internal/initcmd/workflows.go` toe (hij bestaat nog niet) en haal `write-handoff` en `audit-worktrees` uit `requires.toml`. De `initcmd`-wijziging krijgt een eigen eerdere commit `feat(init): schrijft geen GitHub-workflows op GitLab (#407)` met `git add internal/initcmd`.

- [ ] **Step 4: Run, verwacht groen**

Run: `uv run pytest -q tests/pack/test_forge_neutral_units.py tests/pack/test_skill_contracts.py` en `go test ./internal/initcmd/...`
Verwacht: PASS.

- [ ] **Step 5: Commit**

```bash
git add pack tests/pack
git commit -m "docs(forge): §workflows legt de gh-uitzondering van de workflows vast (#407)"
```

### Task 7: Contracttest voor de Go-bronnen

**Files:**
- Modify: `tests/pack/test_forge_neutral_units.py`

**Interfaces:**
- Produces: `test_go_units_do_not_call_gh`: scant `internal/postartefact`, `internal/labels`, `internal/worktreeaudit` en `internal/cli/{postartefact,worktreeaudit}.go` (zonder `_test.go`) op `"gh"` als programmanaam en op `gh label|api|pr|issue`; toegestaan blijft alleen wat in een allowlist staat (`internal/forge/token.go`, `internal/cli/ask.go`, buiten de gescande pakketten).

- [ ] **Step 1: Schrijf de test en bewijs dat hij rood kan worden**

Voeg tijdelijk `exec.Command("gh")` toe in `internal/labels/labels.go`, run, verwacht FAIL, haal het weer weg.

- [ ] **Step 2: Run, verwacht groen**

Run: `uv run pytest -q tests/pack/test_forge_neutral_units.py`
Verwacht: PASS.

- [ ] **Step 3: Commit**

```bash
git add tests/pack/test_forge_neutral_units.py
git commit -m "test(pack): contracttest weigert gh in de Go-units (#407)"
```

### Task 8: Dogfood, gate, commentaar en PR

**Files:**
- Modify: alles wat `go run ./cmd/lighthouse init --upgrade` schrijft; `changelog.d/`

- [ ] **Step 1: Dogfood**

Run: `go run ./cmd/lighthouse init --upgrade`, commit als `chore(pack): installeer de forge-neutrale Go-units (#407)`, run opnieuw. Verwacht: `No changes.`

- [ ] **Step 2: Volledige gate**

Run de `[test_gate] command` uit `lighthouse.toml`. Verwacht: groen; rood wordt een nieuwe fix-commit.

- [ ] **Step 3: Restscan**

Run: `git grep -nE '\bgh\s+(issue|pr|api|label|project)\b|exec\.Command\("gh"' -- internal/postartefact internal/labels internal/worktreeaudit internal/cli/postartefact.go internal/cli/worktreeaudit.go pack/core/skills/write-handoff pack/core/skills/audit-worktrees pack/core/skills/start-issue pack/core/skills/write-issue pack/core/skills/route-issue pack/core/workflows ':!*_test.go'`
Verwacht: geen hit buiten `§workflows` en `internal/unblock`. De rest van de repo (`close-issue`, `write-pr`, `internal/evidence`, `internal/prbody`, `internal/hooks`, `forge/github.md §ci` en `§proxy`) valt buiten #407 en houdt zijn `gh`; zeg dat in het commentaar. De vijf skills van deel 1 zijn dezelfde als in [plan 1](2026-11-19-issue-407-forge-neutrale-skills.md); pas de padlijst daarop aan als zij afwijken.

- [ ] **Step 4: Issue-commentaar (Nederlands) op #407**

Via `lighthouse forge issue comment 407 --body-file <scratch>/407.md`: AC 1 en 2 afgevinkt met bewijs (Task 3, 4, 5, 6 en de restscan); uitzondering met reden voor de workflows (`§workflows`); `close-issue` houdt `gh` voor nu en staat niet in de lijst; de echte run op GitLab is #358.

- [ ] **Step 5: PR**

Via `/lighthouse-write-pr` (schrijft ook het fragment in `changelog.d/`), body `Closes #407` met bewijstabel.
