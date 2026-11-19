# close-issue, unblock-dependents en worktree-audit op de forge — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `/lighthouse-close-issue` (beide tweelingen), `lighthouse unblock-dependents` en `lighthouse worktree-audit` praten via `forge.Forge` en `lighthouse forge`, niet meer via `gh`, op GitHub en GitLab.

**Architecture:** Vijf nieuwe methodes op `forge.Forge` (`Blocking`, `IssueChanges`, `Closer`, `ListChanges`, `ReopenIssue`) plus uitbreidingen op `Milestone`, `EditMilestone` en `Change`, elk met een case in `forgesuite` en een route in beide fake-servers. Daarbovenop komen de subcommando's van `lighthouse forge`. `internal/unblock` en `internal/worktreeaudit` gaan op `forge.Forge`; de skilltekst volgt de subcommando's.

**Tech Stack:** Go (cobra, `net/http`), de fake-servers `forgetest` en `gitlabtest`, pytest voor de pack-tests.

**Spec:** [2026-11-19-issue-406-close-issue-forge-design.md](../specs/2026-11-19-issue-406-close-issue-forge-design.md)

## Global Constraints

- Geen `gh issue|pr|api|label|project` meer in `pack/core/skills/close-issue/{agents,claude}/SKILL.md`. `gh run`, `gh workflow` en `gh release view` blijven.
- Geen merge-commando in `lighthouse forge`. Geen GitLab-pipeline of webhook. `Blocked by #<n>`-regels in een GitLab-body blijven buiten het vrijgeven.
- Op GitHub met lane: dezelfde labels eraf, dezelfde comments erop, dezelfde auditrijen. De enige tekstwijziging: `the release lane removes` → `` `<label>` comes off``.
- Alleen `completed` geeft vrij; een lege `StateReason` op een forge met `Kind() == "gitlab"` telt als `completed`.
- Een afhankelijke met `Blocker.Repo` ≠ `forge.repo` (vergelijking met `strings.EqualFold`) wordt niet gelezen of bewerkt; de summary krijgt `- <repo>#<m>: in another repository, not released`.
- `state` in de CLI-JSON is kleine letters (`open`/`closed`/`merged`); `head_ref`, `merged_at`, `closed_at`, `state_reason` zijn de veldnamen.
- Milestones gaan op titel; nooit een numeriek `milestone_id` naar het GitLab-issues-endpoint.
- Code, namen, commentaar en foutmeldingen Engels; plan, commits, PR en comments Nederlands. Elke commit groen (`[test_gate] command`), nooit `--no-verify`. `/lighthouse-go-standard` en `/lighthouse-test-standard` gelden.
- Een wijziging onder `pack/` of onder `internal/` die wat `init` schrijft verandert, draait ook `go run ./cmd/lighthouse init --upgrade`; die kopie wordt in dezelfde commit gecommit (Task 8 en 10).
- De PR schrijft `Refs #406 — <reden>` (de echte GitLab-run is #358). Eén fragment in `changelog.d/` (Task 10).

## Review Focus

- **`ListChanges("merged")` op GitHub**: de pulls-lijst kent geen `merged`; een gesloten-niet-gemerged PR (`merged_at` null) mag er niet in. Test in Task 2.
- **Meer dan één pagina** bij `Blocking`, `ListChanges` en `IssueChanges`: alle pagina's volgen, een mislukte tweede pagina is een fout, geen afgekapte lijst. Test in Task 1 en 2.
- **Een afhankelijke zonder het label** (al vrijgegeven) of **met hoofdletter-afwijkende repo** krijgt niets. Test in Task 7.
- **Forge niet te openen of lees mislukt** in `worktree-audit`: "PR-status onbekend", nooit "geen PR". Test in Task 9.
- **`ReopenIssue` met een comment die mislukt**: het issue blijft dicht. Test in Task 4.

---

### Task 1: `Blocking` op beide forges

**Files:**
- Modify: `internal/forge/forge.go` (interface), `internal/forge/issues.go` (GitHub), `internal/forge/gitlab_planning.go` of het GitLab-bestand waar `BlockedBy` staat (GitLab)
- Modify: `internal/forge/forgetest/server.go`, `internal/forge/gitlabtest/server.go` (route)
- Test: `internal/forge/forgesuite/suite.go`

**Interfaces:**
- Produces: `Blocking(ctx context.Context, number int) ([]Blocker, error)` op `Forge`. `Blocker` blijft het bestaande type (`Repo`, `Issue`, `State` in hoofdletters). `Blocker` wordt niet uitgebreid met labels; `unblock` leest labels met `Issue(m)`.

- [ ] **Step 1: Test.** In `forgesuite`: case `Blocking`: issue 7 blokkeert 8 (open) en 9 (gesloten) → resultaat `[{Repo, 8, "OPEN"}, {Repo, 9, "CLOSED"}]`; issue zonder blokkades → lege lijst, geen fout; meer dan 100 blokkades (twee pagina's) → alle; een 500 op pagina twee → fout.
- [ ] **Step 2:** `go test ./internal/forge/...` → FAIL (methode ontbreekt).
- [ ] **Step 3: GitHub.** `(*GitHub).Blocking` in `issues.go`, het spiegelbeeld van `BlockedBy` op `GET repos/{repo}/issues/{nr}/dependencies/blocking?per_page=100`, zelfde paginering en `repoFromURL`.
- [ ] **Step 4: GitLab.** `(*GitLab).Blocking` op `GET projects/:id/issues/:iid/links`, houd `link_type == "blocks"`; `Repo` uit het `references.full`-pad van het gelinkte issue; `State` `OPEN`/`CLOSED` uit `opened`/`closed`.
- [ ] **Step 5: Fakes.** Route `dependencies/blocking` in `forgetest` (zelfde fixturebron als `blocked_by`, omgekeerd, met een knop om paginering en een fout op pagina twee te forceren) en `issues/:iid/links` in `gitlabtest`.
- [ ] **Step 6:** `go test ./internal/forge/...` → PASS.
- [ ] **Step 7: Commit** `feat(forge): Blocking leest welke issues een issue blokkeert (#406)`.

### Task 2: `ListChanges` en `Change.MergedAt`

**Files:**
- Modify: `internal/forge/write_issue.go` (`Change.MergedAt time.Time`), `forge.go`, GitHub- en GitLab-implementatie naast `Change`/`CreateChange`, beide fakes
- Test: `internal/forge/forgesuite/suite.go`

**Interfaces:**
- Produces: `ListChanges(ctx context.Context, state string) ([]Change, error)`; `state` is `open`, `merged`, `closed` of `all`, `""` is `open`; een andere waarde is een fout. `Change.State` blijft `open|closed|merged`; `MergedAt` is nul zolang niet gemerged. `Change.HeadRef` is gevuld.

- [ ] **Step 1: Test** in `forgesuite`: drie changes (open, gemerged, gesloten-ongemerged) → `open` geeft 1, `merged` geeft alleen de gemergede met `MergedAt` gezet, `closed` alleen de ongemergede, `all` geeft 3; `State` van de gemergede is `merged`; 101 changes → alle pagina's; `state: "bogus"` → fout.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: GitHub.** `GET pulls?state=open|closed|all&per_page=100`; `merged` = `closed` gefilterd op `merged_at != nil`; `closed` = `closed` met `merged_at == nil`. Hergebruik de bestaande PR→`Change`-conversie van `Change(ctx, n)`.
- [ ] **Step 4: GitLab.** `GET merge_requests?state=opened|merged|closed|all` (project-pad); map `opened`→`open`; `MergedAt` uit `merged_at`.
- [ ] **Step 5: Fakes.** Lijstroute op pulls/merge_requests met state-filter en paginering.
- [ ] **Step 6:** run → PASS.
- [ ] **Step 7: Commit** `feat(forge): ListChanges somt PR's en MR's op per staat (#406)`.

### Task 3: `IssueChanges` en `Closer`

**Files:**
- Modify: `internal/forge/forge.go`, `internal/forge/write_issue.go` (types), GitHub (waar `Planning`/GraphQL-helpers staan, `graphql.go`), GitLab (`gitlab_mr.go`), beide fakes
- Test: `internal/forge/forgesuite/suite.go`

**Interfaces:**
- Produces: `IssueChange{Number int; State string; HeadRef string; CrossRepo bool; Repo string; Body string; Closes bool}` (`State` `OPEN`/`CLOSED`/`MERGED`); `IssueChanges(ctx, number int) ([]IssueChange, error)`; `Closer(ctx, number int) (int, error)` (0 = geen sluitende PR/MR, ook voor een met de hand gesloten issue).

- [ ] **Step 1: Test** in `forgesuite`: (a) issue gesloten door gemergede PR 12 → `IssueChanges` bevat `{12, MERGED, "issue-5-x", false, repo, body, Closes: true}` en `Closer` is 12; (b) PR 13 noemt `Refs #5` → `Closes` false, aanwezig; (c) PR uit andere repo → `CrossRepo` true, `Repo` gevuld; (d) met de hand gesloten → `Closer` 0; (e) heropend en daarna door PR 14 gesloten → `Closer` 14 (laatste sluiting wint).
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: GitHub.** Dezelfde GraphQL-query als `IssuePRHeads` nu stuurt (`closedByPullRequestsReferences` + `CROSS_REFERENCED_EVENT`) voor `IssueChanges`; `timelineItems(itemTypes: CLOSED_EVENT, last: 1)` met `closer { ... on PullRequest { number } }` voor `Closer`. Gebruik de bestaande GraphQL-client van `GitHub`.
- [ ] **Step 4: GitLab.** `IssueChanges`: `GET issues/:iid/closed_by` (`Closes` true) plus `GET issues/:iid/related_merge_requests` (`Closes` false, dedupe op nummer); `CrossRepo` bij een MR met ander `project_id`. `Closer`: `GET issues/:iid/resource_state_events`, laatste event met `state == "closed"`; zijn `source_merge_request.iid` of 0.
- [ ] **Step 5: Fakes.** Fixturevelden voor de sluit-referenties per issue in `forgetest` (GraphQL-antwoord) en `gitlabtest` (de drie routes).
- [ ] **Step 6:** run → PASS.
- [ ] **Step 7: Commit** `feat(forge): IssueChanges en Closer geven de PR's rond een issue (#406)`.

### Task 4: `ReopenIssue`

**Files:**
- Modify: `internal/forge/forge.go`, `internal/forge/write.go` (GitHub), `internal/forge/gitlab_write.go`, beide fakes
- Test: `internal/forge/forgesuite/suite.go`

**Interfaces:**
- Produces: `ReopenIssue(ctx context.Context, number int, comment string) error`: comment eerst (als niet leeg), dan heropenen; faalt de comment, dan blijft het issue dicht en is de fout teruggegeven.

- [ ] **Step 1: Test** in `forgesuite`: gesloten issue + comment `"weer open"` → issue `open`, comment staat erop; lege comment → geen comment geplaatst; comment-route faalt → fout én issue nog `closed`.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3:** GitHub `PATCH issues/{nr} {"state":"open"}`, GitLab `PUT issues/:iid state_event=reopen`; hergebruik de comment-dan-actie-volgorde van `closeWithComment`.
- [ ] **Step 4:** fakes: de patch/put-routes accepteren heropenen.
- [ ] **Step 5:** run → PASS.
- [ ] **Step 6: Commit** `feat(forge): ReopenIssue opent een issue weer na een comment (#406)`.

### Task 5: Milestone-tellers en `EditMilestone` met state

**Files:**
- Modify: `internal/forge/issues.go` (`Milestone.OpenIssues, ClosedIssues int`), `forge.go` (`EditMilestone`-signatuur), GitHub- en GitLab-milestonecode, de aanroepers van `EditMilestone` (`internal/cli/forgecmd_read.go`/`forgecmd_write.go`, `writeForge` in `forgecmd_write_test.go`), beide fakes
- Test: `internal/forge/forgesuite/suite.go`

**Interfaces:**
- Produces: `Milestone.OpenIssues`, `Milestone.ClosedIssues`; `EditMilestone(ctx, number int, title, description, state *string) (Milestone, error)` (`state` `open`/`closed`, nil blijft). Een GitLab-groepsmilestone weigert met de bestaande fout voor `*GroupMilestone`.

- [ ] **Step 1: Test** in `forgesuite`: milestone met 2 open en 3 gesloten issues → `OpenIssues == 2`, `ClosedIssues == 3` uit `Milestones` en `MilestoneByNumber`; `EditMilestone` met `state: "closed"` en nieuwe titel → `State == "closed"`, titel gewijzigd in één aanroep; ongeldige state → fout; GitLab-groepsmilestone → de bestaande weigering.
- [ ] **Step 2:** run → FAIL (compileert niet).
- [ ] **Step 3: GitHub.** Lees `open_issues`/`closed_issues`; `EditMilestone` stuurt `state` mee.
- [ ] **Step 4: GitLab.** Tel via `issues_statistics?milestone=<titel>` (of twee `issues?milestone=<titel>&state=` tellingen, builder kiest); `EditMilestone` stuurt `state_event=close|activate`. Nooit een numeriek `milestone_id`.
- [ ] **Step 5:** pas alle aanroepers en `writeForge.EditMilestone` aan op de nieuwe signatuur (`nil` voor state).
- [ ] **Step 6:** `go build ./... && go test ./...` → PASS.
- [ ] **Step 7: Commit** `feat(forge): milestone-tellers en sluiten via EditMilestone (#406)`.

### Task 6: Subcommando's van `lighthouse forge`

**Files:**
- Modify: `internal/cli/forgecmd_read.go`, `forgecmd_write.go`, `forgecmd_render.go`, `internal/cli/forgecmd.go` (registratie)
- Test: `internal/cli/forgecmd_test.go`, `internal/cli/forgecmd_write_test.go`

**Interfaces:**
- Consumes: de methodes uit Task 1–5.
- Produces (stdout JSON, `state` kleine letters):
  - `issue close <nr> [--body-file F]` → `CloseIssue`; `issue reopen <nr> [--body-file F]` → `ReopenIssue`.
  - `issue blocking <nr>` → lijst met `renderBlocker`.
  - `issue changes <nr>` → `{"state": <issue state>, "closer": <nr of null>, "changes": [{number,state,head_ref,cross_repo,repo,body,closes}]}`.
  - `pr list [--state open|merged|closed|all]` → per element `number,title,body,state,labels,milestone,url,created_at,author,head_ref,merged_at` (`merged_at` null zolang niet gemerged).
  - `milestone edit <titel> --state closed [--title T]` en `milestone view` met `open_issues`, `closed_issues`.
  - `issue view` krijgt `closed_at` (leeg/null als open) en `state_reason`.

- [ ] **Step 1: Tests** (zoals de #260-tests, op de fake-servers en `writeForge`): per subcommando de JSON-vorm met de exacte veldnamen hierboven; `issue changes` met `closer` null en met 14; `pr list --state merged` geeft `merged_at` gezet; `issue view --field state_reason` en `--field closed_at`; ongeldige `--state` → exit 2 (usage); `issue reopen --body-file` plaatst de comment.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3:** implementeer met `env.read`/`env.write` en `renderChange` uitgebreid met `head_ref` en `merged_at`; `renderIssue` met `closed_at` en `state_reason`; `renderMilestone` met de tellers. `issue changes` roept `IssueChanges` en `Closer` na elkaar aan.
- [ ] **Step 4:** run → PASS.
- [ ] **Step 5: Commit** `feat(cli): forge issue close|reopen|blocking|changes, pr list en milestone edit --state (#406)`.

### Task 7: `internal/unblock` op `forge.Forge`

**Files:**
- Modify: `internal/unblock/unblock.go`, `internal/cli/unblockdependents.go`
- Replace: `internal/unblock/unblock_test.go` (Go-tests uit `tests/pack/fixtures/scripts/unblock-dependents/cases.json`)
- Modify: `tests/pack/test_script_cases.py` (verliest de `unblock-dependents`-map), verwijder `tests/pack/fixtures/scripts/unblock-dependents/`

**Interfaces:**
- Consumes: `Issue`, `Blocking`, `BlockedBy`, `CreateComment`, `EditIssue(m, IssueEdit{RemoveLabels: []string{label}})`.
- Produces: `unblock.Run(ctx context.Context, f forge.Forge, repo string, nr int, label string, dryRun bool) (int, []string)`; de drie fasen, volgorde, summary-regels, exitcodes en `ReleaseLine` blijven. De `gh`-types (`ghError`, `newGh`, `pyRepr`, `jsonError`, `GhTimeout`) vervallen. `EditIssue` met een te verwijderen label dat er niet is telt als gelukt (404 → geen fout) in `GitHub.EditIssue` en `GitLab.EditIssue`.

- [ ] **Step 1: Tests** (fake `forge.Forge` op de GitHub- en GitLab-server): elke case uit `cases.json` als Go-test (volgorde: reply-comment vóór de afhankelijken; comment vóór label), plus:
  - een lege `StateReason` op `Kind() == "gitlab"` telt als `completed` → vrijgegeven; op GitHub niet;
  - `not_planned` geeft niets vrij;
  - afhankelijke zonder label wordt niet gelezen, niet bewerkt, niet genoemd;
  - `Blocker.Repo` `corneeldh/Lighthouse` tegenover `forge.repo` `CorneeldH/lighthouse` telt als dezelfde repo; een andere repo geeft `- other/repo#9: in another repository, not released` en komt niet in de reply-comment;
  - label al weg (404 op bewerken) is geen fout;
  - een leesfout op de blockers van één afhankelijke: label blijft, exit 1;
  - de comment-tekst bevat `` `<label>` comes off`` en niet `the release lane removes`.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3:** herschrijf `unblock.go` op de forge-methodes uit de spec-tabel §6. De GitLab-regel voor lege `StateReason` woont hier, niet in `glIssue.issue()`.
- [ ] **Step 4:** `internal/cli/unblockdependents.go`: open de forge met de helper die `forge`-subcommando's gebruiken (`forgeOpenWith`); `GH_TOKEN`/config-fouten geven exit 2, een forge die niet opent exit 1 met `released issues not read`.
- [ ] **Step 5:** verwijder de `gh`-replay-map en de verwijzing in `test_script_cases.py`; `go test ./internal/unblock/... ./internal/cli/... && pytest tests/pack/test_script_cases.py` → PASS.
- [ ] **Step 6: Commit** `refactor(unblock): unblock-dependents op forge.Forge in plaats van gh (#406)`.

### Task 8: De lane-workflow en `token_env`

**Files:**
- Modify: `pack/core/workflows/unblock-dependents.yml`, `.github/workflows/lighthouse-unblock-dependents.yml` (dogfood), eventueel `internal/initcmd/workflows.go` alleen als de statische sjabloon dat vraagt
- Test: `tests/pack/test_workflow_unblock.py`

- [ ] **Step 1: Test.** In `test_workflow_unblock.py`: de workflow bevat een shell-stap die `lighthouse config forge.token_env` leest en het token via `$GITHUB_ENV` onder die naam exporteert **alleen** als de sleutel gezet is; test beide takken (gezet: export-regel; niet gezet: geen export, geen fout); geen `@@forge.token_env@@` in het sjabloon; `GH_REPO` blijft staan.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3:** voeg de stap toe aan het sjabloon in `pack/core/workflows/`.
- [ ] **Step 4:** `go run ./cmd/lighthouse init --upgrade`; de geïnstalleerde `.github/workflows/lighthouse-unblock-dependents.yml` wordt mee gecommit (`workflow-push-gate`: een run van de controller pusht workflowbestanden niet zelf; zie `next: push-workflows`). Verwacht daarna `No changes.` bij een tweede run.
- [ ] **Step 5:** `pytest tests/pack/test_workflow_unblock.py` → PASS.
- [ ] **Step 6: Commit** `feat(workflow): lane geeft token_env door aan unblock-dependents (#406)`.

### Task 9: `internal/worktreeaudit` op `forge.Forge`

**Files:**
- Modify: `internal/worktreeaudit/collect.go` (`pulls`, `IssuePRHeads`, `Env`), `parse.go` (`ParsePulls`, `ParseIssuePRHeads`), `internal/cli/worktreeaudit.go`
- Replace: `internal/worktreeaudit/twins_test.go` en de `gh`-argv in `fixtures_test.go`
- Modify: `tests/pack/test_script_cases.py` (verliest de `worktree-audit`-map), verwijder `tests/pack/fixtures/scripts/worktree-audit/`

**Interfaces:**
- Consumes: `ListChanges(ctx, "all")`, `IssueChanges(ctx, nr)`.
- Produces: `Env` krijgt `Forge func(ctx context.Context) (forge.Forge, error)` in plaats van de `gh`-runner; `IssuePRHeads(ctx, env, nr) ([]string, bool)` (geen `repo`-parameter meer). `ParsePulls` en `ParseIssuePRHeads` nemen typed `[]forge.Change` / `[]forge.IssueChange` in plaats van `any`/JSON. `git` blijft via `Env.Run`; de read-only-controle `assertReadonly` blijft voor `git`.

- [ ] **Step 1: Tests**: de 18 replay-cases van `cases.json` als Go-tests op de fake-servers, met het exitcode-contract (`exit=$?`) intact; plus:
  - forge niet te openen (geen token) → rij met "PR-status onbekend", nooit "geen PR";
  - `ListChanges` faalt → idem;
  - `IssuePRHeads` filtert op gemerged, niet cross-repo, en voor een kruisverwijzing alleen een PR met sluitwoord of `Refs`;
  - dezelfde cases op de GitLab-fake voor `ListChanges`.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3:** vervang `gh`-aanroepen; behoud de rangorde-logica in `prRank` op de `state`-waarden `open`/`merged`/`closed`.
- [ ] **Step 4:** `internal/cli/worktreeaudit.go`: `defaultWorktreeAuditEnv` krijgt de forge-opener; verwijder `GHTimeout` en `procrun.Exec(GHTimeout)` voor de forge-kant.
- [ ] **Step 5:** verwijder `gh`-replay-map; `go test ./internal/worktreeaudit/... ./internal/cli/... && pytest tests/pack/test_script_cases.py` → PASS.
- [ ] **Step 6: Commit** `refactor(worktree-audit): PR-status via forge.Forge in plaats van gh (#406)`.

### Task 10: Skills, forge-referentie, tests, dogfood en changelog

**Files:**
- Modify: `pack/core/skills/close-issue/agents/SKILL.md`, `pack/core/skills/close-issue/claude/SKILL.md` (beide tweelingen identiek in aanpak), `pack/core/skills/forge/github.md`, `pack/core/skills/forge/gitlab.md` (zesde sectie `## §close-issue`), `pack/core/skills/audit-worktrees/*/SKILL.md` (valkuil regel ~148), `pack/core/requires.toml`
- Test: `tests/pack/test_close_issue_skill.py`, `tests/pack/test_skill_contracts.py`
- Create: `changelog.d/issue-406-close-issue-forge.md` (naam en format volgens een bestaand fragment in `changelog.d/`)

- [ ] **Step 1: Tests.**
  - `test_skill_contracts.py`: `FORGE_SECTIONS` krijgt `"## §close-issue"` als zesde; een test dat geen enkele close-issue-tweeling `gh (issue|pr|api|label|project)` noemt; de tekst noemt `lighthouse forge issue close|reopen|blocking|changes` en `pr list`; de `§close-issue`-verwijzing bestaat in beide tweelingen.
  - `test_close_issue_skill.py`: pins op `gh`-tekst worden pins op de `lighthouse forge`-tekst (`head_ref`, `merged_at`, kleine-letters `state`); pins op `gh run`/`gh workflow` blijven; stap 6 noemt `lighthouse unblock-dependents --issue` voor "geen lane" en de commit-link per forge (`/commit/<sha>` op GitHub, `/-/commit/<sha>` op GitLab) in plaats van vast `https://github.com/`.
- [ ] **Step 2:** `pytest tests/pack -k "close_issue or contracts"` → FAIL.
- [ ] **Step 3: close-issue-tekst.** Elke `gh issue|pr|api|label|project` wordt het subcommando uit Task 6 of uit #260 (`issue view [--comments] [--field]`, `issue comment`, `issue edit --remove-label`, `issue status`, `pr view --field body`); `jq`-filters op `head_ref`/`merged_at`/kleine-letters `state`. Stap 6 splitst op lane (spec §1): lane aanwezig = lezen via `lighthouse forge`, run-check en herstart blijven `gh`; geen lane = `lighthouse unblock-dependents --issue <nr>` draaien in elke tak waarin het issue gesloten is en blijft (na sluiten in A/B, in C na `heading:evidence_holds`, `heading:evidence_unmeasurable`, C1 "gesloten zonder sluitende PR" en C3 `COULD NOT CHECK`, en in B7; niet na een heropening), samenvatting letterlijk rapporteren, exit 1 = `released issues not read` met het commando. *Close the milestone*: `milestone edit <titel> --state closed --title …` met de titel uit `issue view --field milestone.title`; een GitLab-groepsmilestone: melden dat hij met de hand dicht moet en doorgaan. De zin die `worktree-audit` buiten de sandbox zet blijft, nu om netwerktoegang.
- [ ] **Step 4: forge-referentie.** `## §close-issue` als zesde sectie in `github.md` (lane met bot, zonder bot de sessie; `gh run`/`gh workflow`; release-flow alleen als `scripts/release.py` bestaat; `closer` uit de laatste `ClosedEvent`; commit-URL) en `gitlab.md` (geen lane; elke close is `completed`; blockers alleen uit links (Premium); `release: skipped — no release flow on GitLab`; `closer` uit `resource_state_events`; commit-URL; groepsmilestone).
- [ ] **Step 5: audit-worktrees en requires.** Haal de `gh`-kant van de valkuil weg, behoud "buiten de sandbox" op netwerkbasis. `requires.toml`: `gh` houdt `close-issue` (run/release); `audit-worktrees` verlaat de `gh`-lijst als de tekst `gh` niet meer noemt.
- [ ] **Step 6: dogfood.** `go run ./cmd/lighthouse init --upgrade`; tweede run geeft `No changes.`; commit de geïnstalleerde kopieën (`.claude/skills/`, `.agents/` of waar `init` ze schrijft).
- [ ] **Step 7: fragment** in `changelog.d/` (Nederlands, één alinea: close-issue, unblock-dependents en worktree-audit op de forge; zonder bot geeft de sessie zelf vrij).
- [ ] **Step 8: volledige run.** `go build ./... && go test ./... && golangci-lint run && pytest tests/pack` → PASS; `go run ./cmd/lighthouse init --upgrade` → `No changes.`
- [ ] **Step 9: Commit** `docs(skills): close-issue op lighthouse forge, §close-issue per forge (#406)`.

## Zelfreview

- **Spec-dekking:** §1 → Task 7 (staterègel) + 10 (lane-splitsing); §2 → Task 1–5; §3 → Task 6; §4 → Task 10; §5 → Task 10; §6 → Task 7 + 8; §7 → Task 9 + 10; §8 → in elke task, `FORGE_SECTIONS` en gh-vrij-test in Task 10.
- **Typeconsistentie:** `Blocking`, `ListChanges`, `IssueChanges`/`IssueChange`, `Closer`, `ReopenIssue`, `EditMilestone(…, state *string)` heten overal zo; `unblock.Run` en `worktreeaudit.Env.Forge` worden alleen in Task 7 en 9 en hun CLI-aanroepers gebruikt.
- **Volgorde:** Task 5 verandert een bestaande signatuur en raakt `writeForge`; Task 6 gebruikt de nieuwe versie. Elke task laat `go build ./...` groen.
