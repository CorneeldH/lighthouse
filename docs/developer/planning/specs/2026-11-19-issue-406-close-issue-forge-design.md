# close-issue, unblock-dependents en worktree-audit op de forge — ontwerp (#406)

## Waarom

Deel van de milestone GitLab-forge. #260 bracht de keten `start-issue` → `write-pr` op
`lighthouse forge`; `/lighthouse-close-issue` bleef bewust buiten, omdat het op GitHub-mechaniek
leunt ([spec #260, Niet-doelen](2026-11-16-issue-260-forge-wrapper-design.md)). Zolang
close-issue `gh` aanroept, eindigt de keten op GitLab bij de MR: het issue sluit niet met bewijs,
afhankelijke issues komen niet vrij en de worktree wordt niet opgeruimd.

Dit ontwerp brengt close-issue en de twee Go-commando's die het aanroept —
`lighthouse unblock-dependents` en `lighthouse worktree-audit` — op de forge-interface. Een echte
run op GitLab hoort bij #358; dit issue levert code en tests op de fake-servers.

## Doelen

- close-issue (beide tweelingen) roept geen `gh issue|pr|api|label|project` meer aan.
- `lighthouse unblock-dependents` en `lighthouse worktree-audit` praten via `forge.Forge`, niet
  via `gh`, op GitHub en GitLab.
- Op GitHub met lane blijft de uitkomst gelijk: dezelfde labels gaan eraf, dezelfde comments komen
  erop, dezelfde rijen en oordelen in de audit. Op GitHub zonder bot geeft de sessie voortaan zelf
  vrij, in plaats van de gebruiker met de hand (§1).
- Wat per forge verschilt voor close-issue staat in `lighthouse-forge` (`github.md` en
  `gitlab.md`).

## Niet-doelen

- GitLab Free: issue-links (blockers) bestaan alleen op Premium en Ultimate. Dit ontwerp leest
  blokkades op GitLab alleen uit links; de `Blocked by #<n>`-regels in de body
  ([`gitlab.md` §blockers](../../../../pack/core/skills/forge/gitlab.md)) blijven buiten het
  vrijgeven (keuze van de gebruiker, voor nu).
- Een GitLab-pipeline of webhook die bij een close het vrijgeven start. Dat hoort bij de open
  milestone-vraag "dispatcher op GitLab".
- De release-flow (`scripts/release.py`, `chore-merge.yml`, `gh release view`) en het controleren
  van workflow-runs (`gh run list`, `gh workflow run`). Die zijn GitHub-only en "only if present";
  ze blijven `gh` en staan als zodanig in `github.md`. Het AC noemt `gh run|workflow|release` niet.
- Een merge-commando in `lighthouse forge` (no-go van de milestone, spec #260).
- Overige `gh`-gebruikers (`fix-pr`, `write-handoff`, `evidence`, `prbody`, `labels`, …).

## Aanpakken

1. **Algemene subcommando's op `lighthouse forge`** (gekozen). Nieuwe methodes op `forge.Forge`
   en subcommando's in de vorm van #260; `unblock` en `worktreeaudit` gebruiken `forge.Forge`
   direct. Voordeel: één JSON-vorm, bouwstenen ook voor andere skills, het patroon van #260
   (`board-start`). Nadeel: meer methodes op de interface; filters blijven in de skilltekst.
2. **Eén Go-commando per skillstap** (bijv. `lighthouse close-issue precheck <nr>`). Korter in de
   skill, maar wijkt af van de wrapper en is alleen voor deze skill bruikbaar.
3. **Ruwe `lighthouse forge api` met paden per forge.** Weinig Go, maar elke stap dubbel in de
   skill en niet te testen. Afgevallen.

Gekozen is 1 (gebruiker).

## Ontwerp

### 1. Vrijgeven zonder lane: de sessie zelf

Stap 6 splitst op de vraag of de lane er is, niet op het soort forge (keuze van de gebruiker):

- **Lane aanwezig** (GitHub met de `ci.bot_*`-keys, `.github/workflows/lighthouse-unblock-dependents.yml`
  bestaat): de lane geeft vrij, als de bot uit `[ci]`, bij elke close als `completed`.
  close-issue controleert achteraf, zoals nu.
- **Geen lane** (GitLab, en GitHub zonder bot): close-issue draait zelf
  `lighthouse unblock-dependents --issue <nr>`, met het token van de gebruiker. Dat vervangt het
  weghalen van het label met de hand (`gh issue edit --remove-label`), dat nu de fallback is.

Zonder lane draait de sessie het commando in elke tak waarin het issue gesloten is en gesloten
blijft: na het sluiten (entry A/B), in entry C na `heading:evidence_holds`,
`heading:evidence_unmeasurable`, C1 "gesloten zonder sluitende PR" en C3 `COULD NOT CHECK`, en in
B7. Niet na een heropening. Een issue dat sluit door een MR-merge of met de hand komt zo vrij
zodra close-issue erop draait. Een afhankelijke die het label niet
meer draagt, valt onder "al" en krijgt niets; een die nog wacht, wordt bij elke run opnieuw in de
reply-comment genoemd (zoals nu), dus een tweede run kan een tweede reply plaatsen
*(eigen keuze na spec-review, r1-f9)*.

Sluitreden: GitLab kent geen `state_reason`. Op GitLab telt elke close als `completed`; de sessie
draait het commando immers pas na een geverifieerde close. Op GitHub blijft "alleen `completed`
geeft vrij". De regel woont in `internal/unblock`: een lege `StateReason` op een GitLab-forge
(`Kind()` is `gitlab`) telt als `completed`; `glIssue.issue()` zet hem niet. Een test op de
GitLab-fake-server dekt het *(eigen keuze na spec-review, r1-f8)*.

### 2. Nieuwe methodes op `forge.Forge`

| Methode | Wat | GitHub | GitLab |
|---|---|---|---|
| `Blocking(nr) ([]Blocker, error)` | issues die `nr` blokkeert (omgekeerde van `BlockedBy`) | `GET issues/{nr}/dependencies/blocking` | `GET issues/:iid/links`, `link_type == "blocks"` |
| `IssueChanges(nr) ([]IssueChange, error)` | PR's/MR's die het issue sluiten of noemen | GraphQL `closedByPullRequestsReferences` + `CROSS_REFERENCED_EVENT` (de query van worktree-audit) | `GET issues/:iid/closed_by` + `GET issues/:iid/related_merge_requests` |
| `Closer(nr) (int, error)` | de PR/MR die het issue de laatste keer sloot; 0 = geen | GraphQL `timelineItems(CLOSED_EVENT, last:1)` | `GET issues/:iid/resource_state_events`, laatste `closed` met `source_merge_request` |
| `ListChanges(state) ([]Change, error)` | PR's/MR's in `open`, `merged`, `closed` of `all` | `GET pulls?state=…` (merged: gefilterd op `merged_at`) | `GET merge_requests?state=…` (`opened`/`merged`/`closed`/`all`) |
| `ReopenIssue(nr, comment)` | comment (als niet leeg), dan heropenen | `PATCH issues/{nr} state=open` | `PUT issues/:iid state_event=reopen` |

`IssueChange` draagt `Number`, `State` (`OPEN`/`CLOSED`/`MERGED`), `HeadRef`, `CrossRepo`, `Repo`,
`Body` en `Closes` (true als hij het issue sluit). `Change` krijgt `MergedAt` (nul zolang niet
gemerged).

Uitbreidingen op bestaande typen:

- `Milestone` krijgt `OpenIssues` en `ClosedIssues` (GitHub: `open_issues`/`closed_issues`;
  GitLab: tellen via `issues?milestone=<titel>&state=…`, zoals `ClosedInMilestone`, of via
`issues_statistics?milestone=<titel>`; nooit via een numeriek `milestone_id`, dat het
issues-endpoint niet accepteert *(eigen keuze na spec-review, r1-f10)*).
- `EditMilestone` krijgt een optionele state (`open`/`closed`): close-issue sluit en hernoemt de
  milestone in één keer. Een GitLab-groepsmilestone weigert `EditMilestone` (`*GroupMilestone`);
  *Close the milestone* meldt dan dat de milestone met de hand dicht moet en gaat door, zonder
  sluiten en zonder hernoemen (`§close-issue` in `gitlab.md`) *(eigen keuze na spec-review,
  r1-f11)*.
- `CloseIssue` blijft zoals hij is (comment, dan sluiten als `completed`).

Elke nieuwe methode krijgt een case in `internal/forge/forgesuite` en een route in beide
fake-servers (`forgetest`, `gitlabtest`).

### 3. Nieuwe subcommando's

| Subcommando | Methode | close-issue gebruikt het in |
|---|---|---|
| `issue close <nr> [--body-file]` | `CloseIssue` | entry A stap 5, B5, het voorbeeld |
| `issue reopen <nr> [--body-file]` | `ReopenIssue` | C3 |
| `issue blocking <nr>` | `Blocking` | stap 6 |
| `issue changes <nr>` | `IssueChanges` + `Closer` | step 0, C1 |
| `pr list [--state open\|merged\|closed\|all]` | `ListChanges` | step 0 (tweede query), B2, B2a |
| `milestone edit <titel> --state closed [--title T]` | `EditMilestone` | *Close the milestone* stap 3 |

`issue changes` geeft één object: `{"state": …, "closer": <nr of null>, "changes": [ … ]}`. Zo
beantwoordt één aanroep step 0 en C1, zoals de huidige GraphQL-query. `milestone view <titel>` toont
voortaan `open_issues` en `closed_issues`. Milestones gaan op titel, zoals de bestaande
`milestone edit`/`view` (`milestoneByTitle`); close-issue neemt de titel uit
`issue view --field milestone.title`, want `Milestone.Number` is op GitLab de globale id.

`issue view` krijgt twee velden erbij: `closed_at` en `state_reason` (leeg als de forge er geen
kent), met een CLI-test; stap 6 leest `closed_at` daaruit. `pr list` geeft per element
`number,title,body,state,labels,milestone,url,created_at,author` zoals `renderChange`, plus
`head_ref` en `merged_at` (null zolang niet gemerged); `state` is kleine letters (`open`,
`merged`, `closed`). `issue changes` geeft per element van `changes` `number,state,head_ref,
cross_repo,repo,body,closes`, met `state` in kleine letters zoals de rest van de CLI-JSON; de
skill-tekst vergelijkt dus op `open`/`closed`/`merged`, niet op `OPEN`/`CLOSED`/`MERGED`. De
`jq`-filters in de skill werken op deze veldnamen (`head_ref` waar de `gh`-versie
`headRefName` had, `merged_at` voor `mergedAt`) *(eigen keuze na spec-review, r1-f1, r1-f3)*.

De rest bestaat al sinds #260: `issue view [--comments] [--field]`, `issue comment`,
`issue edit --remove-label`, `issue status` (vervangt `gh project item-edit`), `pr view --field
body`.

De filters (head-branch `issue-<nr>-`, `Refs #<nr>` in de body, `created_at`-grens) blijven in de
skill als `jq` op de JSON van `pr list`, zoals de skills van #260 doen.

### 4. close-issue

Elke `gh issue|pr|api|label|project` in beide tweelingen wordt het subcommando uit §3 of uit #260.
Wat GitHub-only blijft (`gh run list`, `gh workflow run`, `gh release view`, de release-flow) staat
ongewijzigd, met een verwijzing naar `../lighthouse-forge/<kind>.md §close-issue`.

Stap 6 *Released issues* splitst op de lane (§1):

- lane aanwezig: zoals nu, maar de leesaanroepen via `lighthouse forge issue blocking`,
  `issue view --field closed_at`, `issue view --comments` en `issue view <m> --field labels`. De
  run-check (`gh run list`) en de herstart (`gh workflow run`) blijven `gh`.
- geen lane: `lighthouse unblock-dependents --issue <nr>` draaien en de samenvatting letterlijk
  rapporteren. Geen run-check. Exit 1: `released issues not read` met het commando, nooit "none".

Het sluitcommentaar (entry A stap 5, het voorbeeld) zet de commit-link niet meer vast op
`https://github.com/`. Het patroon staat per forge in `§close-issue` (§5) (mijn keuze, alleen
skilltekst):

- GitHub: `https://github.com/<forge.repo>/commit/<sha>`;
- GitLab: `https://<forge.host, anders gitlab.com>/<forge.repo>/-/commit/<sha>`.

### 5. `lighthouse-forge`: nieuwe sectie `§close-issue`

In `github.md` en `gitlab.md`, als zesde sectie. `FORGE_SECTIONS` in
[`test_skill_contracts.py`](../../../../tests/pack/test_skill_contracts.py) groeit mee.

- `github.md`: met bot in `[ci]` geeft de lane vrij bij een close als `completed`, zonder bot de
  sessie; de run-check en herstart via `gh run`/`gh workflow`; de release-flow
  (`gh release view`) alleen als `scripts/release.py` bestaat; `closer` komt uit de laatste
  `ClosedEvent`; het commit-URL-patroon.
- `gitlab.md`: geen lane, de sessie draait `unblock-dependents` zelf; elke close telt als
  `completed`; blockers alleen uit links (Premium); geen release-flow
  (`release: skipped — no release flow on GitLab`); `closer` uit `resource_state_events`; het
  commit-URL-patroon.

### 6. `lighthouse unblock-dependents`

`internal/unblock` gaat van `gh api` naar `forge.Forge`:

| Nu (`gh api`) | Straks |
|---|---|
| `issues/{nr}` (`state`, `state_reason`) | `Issue(nr)` |
| `issues/{nr}/dependencies/blocking` | `Blocking(nr)` (+ `Issue(m)` voor de labels van een afhankelijke, als `Blocker` ze niet draagt) |
| `issues/{m}/dependencies/blocked_by` | `BlockedBy(m)` |
| `POST issues/{n}/comments` | `CreateComment` |
| `DELETE issues/{m}/labels/{label}` | `EditIssue(m, remove label)` |

De drie fasen, de volgorde (lezen, reply-comment, dan de afhankelijken), de summary-regels en de
comment-teksten blijven. Een label dat er al niet meer is
(404 op het weghalen) is geen fout: `EditIssue` met een te verwijderen label behandelt een
afwezig label als gelukt, zodat de 404-case uit `cases.json` blijft gelden. Foutregels mogen anders luiden: ze kwamen uit `gh`-stderr en de
Python-nabootsing (`pyRepr`, `KeyError`); voortaan uit de forge-fout. Op GitHub blijft de
summary verder byte-gelijk.

De comment-tekst `the release lane removes` klopt niet als de sessie vrijgeeft. Die tekst wordt
neutraal: `` `<label>` comes off``. Dat is de enige wijziging in wat GitHub met lane ziet.

Een afhankelijke in een andere repo of ander project (`Blocker.Repo` verschilt van `forge.repo`; vergelijk
hoofdletterongevoelig met `strings.EqualFold`, want de forge geeft de canonieke schrijfwijze en
`lighthouse.toml` wat de gebruiker typte; een test met afwijkende hoofdletters dekt het) wordt
niet gelezen of bewerkt: de summary krijgt `- <repo>#<m>: in another repository, not released`,
en de reply-comment noemt hem niet (mijn keuze). Nu zou de lane het nummer in de eigen repo
gebruiken; dat is een fout, geen gedrag om te bewaren.

De lane-workflow: de repo komt uit `lighthouse.toml` in de checkout, en `GH_TOKEN` is het token
van `forge.Open` ([`token.go`](../../../../internal/forge/token.go)), maar alleen zolang
`[forge] token_env` niet gezet is. Met `token_env` geeft de workflow het App-token ook onder die
naam door, zodat de lane niet op de fallback `gh auth token` leunt (mijn keuze). De workflow is
statisch: geen `@@forge.token_env@@`, want een niet-gezette sleutel laat `init` de workflow
weigeren (`internal/initcmd/workflows.go`). Een shell-stap leest
`lighthouse config forge.token_env` en exporteert het token onder die naam via `$GITHUB_ENV` als
de sleutel gezet is, anders niets *(eigen keuze na spec-review, r1-f5)*; een test in
[`test_workflow_unblock.py`](../../../../tests/pack/test_workflow_unblock.py) dekt beide. `GH_REPO`
mag blijven staan. `$GITHUB_STEP_SUMMARY` blijft: buiten Actions is hij leeg en wordt er niets
geschreven.

### 7. `lighthouse worktree-audit`

`internal/worktreeaudit` gebruikt `gh` op twee plekken; beide gaan naar `forge.Forge`:

- `pulls(repo)` (`gh api --paginate repos/…/pulls?state=all`) → `ListChanges("all")`.
- `IssuePRHeads` (GraphQL) → `IssueChanges(nr)`; `ParseIssuePRHeads` filtert zoals nu op
  gemerged, niet cross-repo, en voor een kruisverwijzing alleen een PR die het issue met een
  sluitwoord of `Refs` noemt.

`git` blijft via `Env.Run`. Een forge die niet te openen is (geen token, geen config), of een
mislukte lees, geeft zoals nu "PR-status onbekend", nooit "geen PR".

De skill `audit-worktrees` noemt `gh` alleen in een valkuil (regel 148, "`gh api`, never
`gh pr list --json`"); de `gh`-kant van die regel vervalt, maar de
aanwijzing om buiten de sandbox te draaien blijft, nu op netwerktoegang gebaseerd, en dat geldt
ook voor de zin in close-issue die `worktree-audit` buiten de sandbox zet omdat hij `gh` aanroept
*(eigen keuze na spec-review, r1-f12)*. `requires.toml`: `audit-worktrees` en `close-issue`
houden `gh` alleen als de tekst het nog noemt (close-issue: ja, voor run en release).

### 8. Tests

- `internal/forge/forgesuite`: elke nieuwe methode, op beide fake-servers.
- `internal/cli`: de nieuwe subcommando's, op de fake-servers, zoals de #260-tests.
- `internal/unblock`: de cases uit
  [`cases.json`](../../../../tests/pack/fixtures/scripts/unblock-dependents/cases.json) worden
  Go-tests op een fake `forge.Forge` (GitHub- en GitLab-server), zoals `board-start` in #260. De
  `gh`-replay-cases verdwijnen; `test_script_cases.py` verliest die map.
- `internal/worktreeaudit`: `twins_test.go` vervangt de `gh`-argv door fake-forge-antwoorden. De
  18 `gh`-replay-cases van
  [`cases.json`](../../../../tests/pack/fixtures/scripts/worktree-audit/cases.json) worden
  Go-tests op de fake-servers, met het exitcode-contract (`exit=$?` in close-issue) intact;
  `test_script_cases.py` verliest ook die map.
- `tests/pack/test_close_issue_skill.py`: de pins op `gh`-tekst worden pins op de
  `lighthouse forge`-tekst; die op `gh run`/`gh workflow` blijven.
- `tests/pack/test_skill_contracts.py`: `FORGE_SECTIONS` met `§close-issue`; een test dat
  close-issue geen `gh issue|pr|api|label|project` meer noemt.
- Dogfood: `go run ./cmd/lighthouse init --upgrade`, geïnstalleerde kopieën gecommit.

De echte GitLab-run staat in #358.

## Overgelaten aan de bouwer

- Hoe GitLab `OpenIssues`/`ClosedIssues` van een milestone telt (`issues_statistics` of twee
  tellingen); zolang de uitkomst gelijk is.
- Of `IssueChanges` en `Closer` één methode worden; het subcommando `issue changes` geeft ze
  samen.
- De volgorde van de commits, mits elke commit groen is.
