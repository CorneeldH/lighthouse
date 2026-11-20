# Forge-neutrale Go-units (#407, deel 2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `post-artefact`, `labels`, `worktree-audit` en de workflows roepen geen `gh` meer aan voor een forge-bewerking, of de uitzondering staat met reden vast; daarmee zijn AC 1 en 2 van #407 af.

**Architecture:** De drie Go-pakketten (`internal/postartefact`, `internal/labels`, `internal/worktreeaudit`) krijgen een `forge.Forge` in hun `Env`, geopend zoals `laneEnvFrom` in `internal/cli/pinslane.go` dat doet (`repoconfig.Load` → `forge.Token` → `forge.Open`). Wat het interface nog niet kan (commit bekend?, label maken, alle PR-koppen, PR's die een issue sluiten) komt als vier nieuwe methoden op `*GitHub` en `*GitLab`. De workflows blijven op `gh` (alleen GitHub Actions) met een uitzondering met reden in `lighthouse-forge`.

**Tech Stack:** Go (`internal/forge`, `forgetest`, `gitlabtest` en `forgesuite` voor de fakes en de suite), pytest voor de contracttest in `tests/pack`.

**Spec:** issue #407 (geen aparte spec); deel 1 is [het plan van 2026-11-19](2026-11-19-issue-407-forge-neutrale-skills.md), de nacontrole van #430 staat als comment op het issue. Dit plan sluit #407.

## Global Constraints

- Instructies, skilltekst en commentaar zijn Engels; dit plan, commits, issue en PR zijn Nederlands (`[project] language = nl`).
- Commits zijn Conventional Commits, één logische wijziging per commit, in de worktree van branch `issue-407-gitlab-forge-overige-units-met-gh-op`.
- Een wijziging onder `pack/` of onder `internal/` die verandert wat `init` schrijft draait `go run ./cmd/lighthouse init --upgrade`; de geïnstalleerde kopie wordt gecommit en CI eist daarna `No changes.`
- Beide varianten (`claude/` en `agents/`) van een skill veranderen samen.
- `gh auth token` blijft toegestaan (`internal/forge/token.go`, `internal/cli/ask.go`).
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
- Test: `internal/forge/forgesuite/suite.go`, `internal/forge/github_contract_test.go`, `internal/forge/gitlab_contract_test.go`

**Interfaces:**
- Produces: `CommitKnown(ctx context.Context, sha string) (bool, error)` — `true` als de forge de commit kent; een 404 of 422 is `false, nil`, elke andere fout wordt teruggegeven. GitHub: `GET repos/<repo>/commits/<sha>`; GitLab: `GET projects/<id>/repository/commits/<sha>`.
- Produces: `CreateLabel(ctx context.Context, name, color string) error` — `color` is zes hexcijfers zonder hekje; GitLab zet er zelf een `#` voor. Een label dat al bestaat is geen fout (409 GitHub 422 `already_exists`, GitLab 409).

- [ ] **Step 1: Schrijf de suite-cases die falen**

In `forgesuite/suite.go` naast de `Labels`-test: `TestCommitKnown` (bekende sha → `true`; onbekende sha → `false, nil`; een 500 → fout) en `TestCreateLabel` (nieuw label verschijnt in `Labels`; tweede keer dezelfde naam → `nil`; kleur komt aan zoals de forge hem verwacht).

- [ ] **Step 2: Run, verwacht rood**

Run: `go test ./internal/forge/... -run 'CommitKnown|CreateLabel'`
Verwacht: build-fout, de methoden bestaan niet.

- [ ] **Step 3: Implementeer op `*GitHub`, `*GitLab` en beide fakes**

Gebruik de bestaande `c.do`-helper en de statusfout waar `checks.go:123` al `errors.As(err, &se)` op doet; geen nieuwe fouttypes. Voeg de twee routes toe aan de fakes.

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
- Modify: `internal/forge/forge.go`, `internal/forge/board.go` (naast `BranchPRs`), `internal/forge/gitlab_mr.go`, fakes in `forgetest` en `gitlabtest`
- Test: `internal/forge/forgesuite/suite.go`

**Interfaces:**
- Produces: `type PullHead struct{ Number int; State string; MergedAt string; HeadRef string }` en `PullHeads(ctx context.Context) ([]PullHead, error)` — elke PR of MR in elke staat, alle pagina's; `State` is `OPEN`, `CLOSED` of `MERGED` zoals `PR.State`.
- Produces: `IssueClosingHeads(ctx context.Context, number int) ([]ClosingHead, error)` met `type ClosingHead struct{ HeadRef string; Merged, CrossRepo bool }` — de PR's die issue `number` sluiten of ernaar verwijzen (GitHub: dezelfde `closedByPullRequestsReferences` en `CROSS_REFERENCED_EVENT` als `issuePRsQuery`, via de bestaande GraphQL-helper in `graphql.go`; GitLab: `GET projects/<id>/issues/<iid>/closed_by` samen met `related_merge_requests`).

- [ ] **Step 1: Schrijf de suite-cases**

`TestPullHeads`: open, gesloten en gemergde PR komen alle drie terug met de juiste `State`; meer dan één pagina. `TestIssueClosingHeads`: een gemergde PR, een fork-PR (`CrossRepo` true) en een PR die het issue alleen noemt.

- [ ] **Step 2: Run, verwacht rood**

Run: `go test ./internal/forge/... -run 'PullHeads|IssueClosingHeads'`
Verwacht: build-fout.

- [ ] **Step 3: Implementeer op beide forges en beide fakes**

Eén regel aanpak: GitHub hergebruikt de query-tekst uit `worktreeaudit/collect.go:303` (verplaats hem naar `internal/forge`); GitLab mapt `state: opened|closed|merged` naar `OPEN|CLOSED|MERGED`.

- [ ] **Step 4: Run, verwacht groen**

Run: `go build ./... && go vet ./... && go test -race ./internal/forge/...`
Verwacht: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/forge internal/worktreeaudit/collect.go
git commit -m "feat(forge): PullHeads en IssueClosingHeads op GitHub en GitLab (#407)"
```

### Task 3: `post-artefact` op de forge

**Files:**
- Modify: `internal/postartefact/postartefact.go` (regels 41-209, 319, 436), `internal/cli/postartefact.go` (regels 29-53, Long-tekst)
- Test: `internal/postartefact/postartefact_test.go` (regels 85-101 herschrijven), `internal/cli/postartefact_test.go`

**Interfaces:**
- Consumes: `Forge.CommitKnown`, `Forge.BranchPRs`, `Forge.Comments`, `Forge.CreateComment` (Task 1 en het bestaande interface).
- Produces: `postartefact.Env` met veld `Forge func(root string) (forge.Forge, error)` naast `Run` (alleen nog voor `git`) en `Config`; `p.gh` en `p.repo` verdwijnen. `commentOf` verdwijnt: `protocol.Comment` komt kant-en-klaar uit `Forge.Comments`. De foutregel wordt `artefact: forge failed: <reden>`.

- [ ] **Step 1: Herschrijf de test-replay naar een fake forge**

`forgetest` levert de fake. Cases (bestaande namen behouden): commit onbekend → push, daarna post; commit bekend → geen push; branch met open PR → geen push; branch met alleen een gemergde PR → wel push; `CommitKnown`-fout 500 → exit 1 met `forge failed`; hetzelfde scenario op `gitlabtest`.

- [ ] **Step 2: Run, verwacht rood**

Run: `go test ./internal/postartefact/... ./internal/cli/... -run PostArtefact`
Verwacht: FAIL.

- [ ] **Step 3: Zet `commitKnown`, `openPR`, `readComments` en `postComment` om**

`openPR` filtert `BranchPRs` op `State == "OPEN"`. `postComment` geeft `Comment.URL`. De forge opent `defaultPostArtefactEnv` zoals `laneEnvFrom`; een ontbrekend token geeft `artefact: no token for forge …`. Pas de `Long`-tekst aan: "not on the forge" in plaats van "not on GitHub".

- [ ] **Step 4: Run, verwacht groen**

Run: `go test -race ./internal/postartefact/... ./internal/cli/...`
Verwacht: PASS. Controle: `git grep -n '"gh"' internal/postartefact internal/cli/postartefact.go` geeft geen hit.

- [ ] **Step 5: Commit**

```bash
git add internal/postartefact internal/cli/postartefact.go internal/cli/postartefact_test.go
git commit -m "feat(post-artefact): leest en schrijft via lighthouse forge, ook op GitLab (#407)"
```

### Task 4: `labels` op de forge, plus `lighthouse forge label create`

**Files:**
- Modify: `internal/labels/labels.go` (regels 43-63, 162-260), `internal/initcmd/initcmd.go` (regel 251), `internal/cli/forge*.go` (het `forge`-subcommando, naast `issue` en `pr`)
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

`initcmd` opent de forge voor `Sync` zoals `laneEnvFrom` (geen token → `nil`, nooit een fout). Het `forge label create`-subcommando valt onder dezelfde `dangerouslyDisableSandbox`-regel als de andere forge-commando's.

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
- Test: `internal/worktreeaudit/twins_test.go` (regels 283-298, 533 omzetten), `internal/worktreeaudit/collect_test.go`

**Interfaces:**
- Consumes: `Forge.PullHeads`, `Forge.IssueClosingHeads` (Task 2).
- Produces: `worktreeaudit.Env` met `Forge forge.Forge` naast `Run` (alleen nog `git`); `collector.gh` verdwijnt. `IssuePRHeads(ctx, env, repo, nr)` houdt zijn signatuur en `false` bij elke fout; `ParseIssuePRHeads` krijgt `[]forge.ClosingHead` in plaats van het ruwe GraphQL-document en filtert op `Merged && !CrossRepo`.

- [ ] **Step 1: Zet de twin-tests om**

De scriptcases met `gh api …` worden fake-forgeantwoorden; verwachte uitvoer blijft byte voor byte gelijk. Voeg toe: forge-fout → PR-status `onbekend` (Review Focus), fork-PR telt niet.

- [ ] **Step 2: Run, verwacht rood**

Run: `go test ./internal/worktreeaudit/... ./internal/cli/...`
Verwacht: FAIL.

- [ ] **Step 3: Implementeer**

`pulls` bouwt zijn `[]any`-payload uit `PullHeads` (veldnamen `number`, `state`, `merged_at`, `head.ref` blijven, zodat de parser ongemoeid blijft; `state` wordt `open`/`closed` in kleine letters zoals GitHub REST).

- [ ] **Step 4: Run, verwacht groen**

Run: `go test -race ./internal/worktreeaudit/... ./internal/cli/...`
Verwacht: PASS; `git grep -n '"gh"' internal/worktreeaudit` geeft geen hit.

- [ ] **Step 5: Commit**

```bash
git add internal/worktreeaudit internal/cli/worktreeaudit.go
git commit -m "feat(worktree-audit): leest PR's via lighthouse forge, ook op GitLab (#407)"
```

### Task 6: Workflows als uitzondering met reden

**Files:**
- Modify: `pack/core/skills/lighthouse-forge/github.md` en `gitlab.md` (nieuwe sectie `## workflows`, zelfde plek in beide), `pack/core/skills/write-handoff/**` en `audit-worktrees/**` (de "blijft tot plan 2"-notities weg; zie plan 1, regel 90)
- Test: `tests/pack/test_forge_neutral_units.py`

**Interfaces:**
- Produces: `§workflows` in beide forgebestanden. GitHub: `unblock-dependents.yml` draait alleen in GitHub Actions op `issues: closed`; `gh release download` haalt daar het lighthouse-binary op de runner, en `.github/workflows/build.yml` publiceert de release van lighthouse zelf met `gh release`. GitLab: geen equivalent in deze milestone; een GitLab-repo draait de workflows niet en `lighthouse init` schrijft ze er niet.

- [ ] **Step 1: Test die het ontbreken van de sectie vindt**

In `test_forge_neutral_units.py`: `test_forge_files_have_workflows_section` (beide bestanden bevatten `## workflows`) en `test_no_stays_until_plan_2_notes` (geen "plan 2" meer in de vijf units).

- [ ] **Step 2: Run, verwacht rood**

Run: `uv run pytest -q tests/pack/test_forge_neutral_units.py`
Verwacht: FAIL.

- [ ] **Step 3: Schrijf de secties en haal de notities weg**

Controleer dat `lighthouse init` voor een GitLab-repo de workflow niet schrijft (lees `pack/core/pack.toml`); is dat niet zo, voeg die voorwaarde toe en een test in `internal/initcmd`.

- [ ] **Step 4: Run, verwacht groen**

Run: `uv run pytest -q tests/pack/test_forge_neutral_units.py`
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

Run: `git grep -nE '\bgh\s+(issue|pr|api|label|project)\b|exec.Command\("gh"' -- pack/core/skills internal ':!internal/**/*_test.go'`
Verwacht: alleen `gh auth token` en de plekken uit `§workflows`.

- [ ] **Step 4: Issue-commentaar (Nederlands) op #407**

Via `lighthouse forge issue comment 407 --body-file <scratch>/407.md`: AC 1 en 2 afgevinkt met bewijs (Task 3, 4, 5, 6 en de restscan); uitzondering met reden voor de workflows (`§workflows`); `close-issue` houdt `gh` voor nu en staat niet in de lijst; de echte run op GitLab is #358.

- [ ] **Step 5: PR**

Via `/lighthouse-write-pr` (schrijft ook het fragment in `changelog.d/`), body `Closes #407` met bewijstabel.
