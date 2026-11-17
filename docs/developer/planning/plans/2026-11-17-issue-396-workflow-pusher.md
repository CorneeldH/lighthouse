# Workflow-pusher — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** een run draagt een workflow-wijziging over met een `🤖 Handoff` `next: push-workflows`; tick start
`lighthouse push-workflows`, dat na harde checks, policy en (pre-merge) een unanieme security-review pusht met
een token dat alleen dat proces leest.

**Architectuur:** `Judge` kent `VerdictPush`, `decide` plant `KindPush`, `apply` claimt een sessieloze
push-rij en start het subcommando losgekoppeld met een opgeschoonde omgeving. Het subcommando
(`internal/pusher`) draait vloer → policy → review → push in een eigen bare kloon en sluit zijn eigen rij.
Een hook (`workflow-push-gate`) en `post-artefact` houden de run van de push af.

**Tech stack:** Go (`internal/dispatch`, `internal/protocol`, `internal/hostconfig`, `internal/runs`,
`internal/hooks`, nieuw `internal/pusher`, `internal/cli`), SQLite-migratie, `bwrap`, `actionlint`, `zizmor`;
een YAML-bibliotheek (geen regex; `gopkg.in/yaml.v3` tenzij de bouwer een reden voor een andere heeft); docs in
het Nederlands.

**Spec:** [2026-11-16-issue-396-workflow-pusher-design.md](../specs/2026-11-16-issue-396-workflow-pusher-design.md)

## Globale eisen

- Code, namen en commentaar Engels; `docs/`, comments op issues en commitberichten Nederlands (Conventional Commits).
- `/lighthouse-go-standard` en `/lighthouse-test-standard` gelden; `gofmt` en `golangci-lint` draaien als hook.
- Het workflow-token staat nooit in `os.Environ()` van tick, een run of een reviewer; alleen het ene `git push`-commando krijgt het.
- Elk git-commando van de pusher: `GIT_CONFIG_GLOBAL=/dev/null`, `GIT_CONFIG_NOSYSTEM=1`, push naar de URL uit `forge.repo` (`git push <url> <sha>:refs/heads/<branch>`), nooit een remote-naam. De URL komt uit `repoconfig` (Kind, Host, Slug): alleen `github.com` wordt ondersteund, andere forges ⇒ route uit met de reden. Het token gaat nooit in argv: `GIT_CONFIG_COUNT`/`GIT_CONFIG_KEY_0`/`GIT_CONFIG_VALUE_0` (`http.extraHeader`) in de `Cmd.Env` van dat ene commando; een test controleert dat argv geen token bevat. In de worktree van de run alleen `rev-parse HEAD`, `rev-parse --abbrev-ref HEAD` en `status --porcelain` met `-c core.hooksPath=/dev/null -c core.fsmonitor=false`, plus één bestandslezing van het roborev-record.
- Geen tekst, melding of Vraag adviseert een token-scope te verruimen.
- Ontbreekt `actionlint`, `zizmor` of `bwrap` op `PATH`: rood met `COULD NOT CHECK`, nooit groen.
- Tokenbestand moet mode `0600` hebben; `[dispatch.workflow_push]` ontbreekt ⇒ route uit ⇒ Vraag.
- Defaults: `review = "shadow"`, `timeout = "30m"`. Reviewer-prompts zijn `embed`-bestanden, nooit uit de branch.
- Nieuwe headings in `.lighthouse/headings.toml`; schrijf nooit de letterlijke `heading:<key>`.
- Een wijziging onder `pack/` of `internal/` die `init` raakt: `go run ./cmd/lighthouse init --upgrade` en commit het resultaat (dogfood). Elke PR krijgt een fragment in `changelog.d/`.
- Testcommando: `go test -race ./internal/...` en `uv run pytest -q tests/` (`./tests/...` is geen Go-pakket; daar staan de loop-, handoff-tabel-, site- en changelogregels); git-checks op een echte tijdelijke repo met bare remote, reviewers als fake commando's op `PATH` (patroon `internal/cli/roborev_fixture_test.go`).

## Review-focus

1. **Token in een omgeving.** Fake reviewer en fake `git` dumpen `os.Environ()`; beide zien nooit het workflow- of forge-token (taak 8, taak 9).
2. **Worktree-config stuurt de push om.** `pre-push`-hook, `core.fsmonitor`, `url.*.insteadOf` in de worktree of `~/.gitconfig` raken het token niet en leiden de push niet om (taak 6).
3. **Bestand dat tijdens de review verandert / SHA-wissel.** Pusher leest één SHA, pusht precies die; een tweede commit na de handoff wordt niet gepusht (taak 6, 9).
4. **Backticks in de diff.** Een diff met ```` ``` ```` breekt het codeblok van de Vraag niet; >65536 tekens wordt afgekapt met bijlage (taak 9).
5. **Een tweede pusher voor dezelfde repo / een herstart na crash.** `flock` op de kloon; een push-rij die `start_failed` sluit start één keer opnieuw, dan Vraag (taak 5, 6).

---

### Taak 1: Verifieer of de API-merge van regel 7 een workflow-PR toelaat

**Files:**
- Modify: dit plan (het resultaat onder *Uitkomst taak 1*)

**Interfaces:**
- Produces: de beslissing "regel 7 ongewijzigd" (alleen bij een aangetoond slagen) of "taak 12 is nodig" (ook bij niet-geverifieerd). Taak 12 wacht op dit resultaat.

- [ ] **Step 1:** Nooit in `CorneeldH/lighthouse` zelf (een merge naar `main` start `release.yml`). Alleen als de uitvoerder een expliciet benoemd credential met `workflow`-scope heeft: maak een wegwerp-repository, push daar een wijziging onder `.github/workflows/` met dat credential, open een PR en merge hem met een token zonder `workflow`-scope; verwijder de repository daarna. Heeft de uitvoerder zo'n credential niet, sla de verificatie over en schrijf onder *Uitkomst taak 1*: "niet geverifieerd; taak 12 wordt gebouwd" (de spec §7 gaat dan uit van weigering).
- [ ] **Step 2:** Noteer de exacte GitHub-melding of het slagen onder *Uitkomst taak 1* in dit plan.
- [ ] **Step 3: Commit** `docs(plan): leg uitkomst merge-verificatie vast (#396)`.

#### Uitkomst taak 1

*(in te vullen door de uitvoerder)*

---

### Taak 2: `[dispatch.workflow_push]` in `host.toml`

**Files:**
- Modify: `internal/hostconfig/hostconfig.go` (`Dispatch`, `LoadDispatch`)
- Test: `internal/hostconfig/hostconfig_test.go`

**Interfaces:**
- Produces: `type WorkflowPush struct { TokenFile string; Review string; Timeout time.Duration; Reviewers []Reviewer }`, `type Reviewer struct { Topic string; Command []string; Binds []string; Env []string }`, en `Dispatch.WorkflowPush *WorkflowPush` (nil = route uit). `TokenFile` en `Binds` krijgen `~` expliciet uitgebreid (zoals `datadir.expandUser`; `DispatchForge.TokenFile` doet dat niet), met een test.

- [ ] **Step 1: Tests** `TestLoadDispatchWorkflowPush` (volledig voorbeeld uit spec §6 → verwachte struct; `Review` default `shadow`, `Timeout` default 30m), `TestLoadDispatchWorkflowPushAbsent` (`WorkflowPush == nil`), `TestLoadDispatchWorkflowPushInvalid` (tabel: `review = "x"`, `topic = "x"`, lege `command`, een `env`-naam gelijk aan `GH_TOKEN`/`GITHUB_TOKEN`, een `binds`-pad onder `~/.config/lighthouse`, ontbrekende `token_file` ⇒ fout).
- [ ] **Step 2:** Run `go test ./internal/hostconfig/ -run WorkflowPush` — verwacht FAIL.
- [ ] **Step 3:** Implementeer. `command` bevat de plaatshouder `{out}` (pad van het uitvoerbestand) en `-` voor stdin. Onbekende sleutels blijven een fout (bestaand gedrag).
- [ ] **Step 4:** Test PASS. **Step 5: Commit** `feat(hostconfig): [dispatch.workflow_push] (#396)`.

---

### Taak 3: Protocol — `VerdictPush`, handoff-velden, headings

**Files:**
- Modify: `internal/protocol/handoff.go` (velden `After`, `Roborev`), `internal/dispatch/judge.go`, `.lighthouse/headings.toml` (+ pack-bron zoals de bestaande keys), `internal/protocol/render.go`
- Test: `internal/protocol/handoff_test.go`, `internal/dispatch/judge_test.go`

**Interfaces:**
- Produces: `Judge`/`judgeHandoff` krijgen de handoff-velden `after` mee (signatuurwijziging in `decide.go:898`, alle aanroepers bijwerken); `Handoff.After, Handoff.Roborev string`; `const VerdictPush`; `const cmdPush = "push-workflows"`; `Judge("push-workflows", …)` ⇒ `Judgement{Verdict: VerdictPush, Command: "push-workflows", Arg: <after>}` alleen als `after` leeg is of door `Judge` als start komt (allowlist; anders `VerdictRejected`).
- Produces: `func RenderPushed(issue int, worktree, branch, repoRange, workflows, next, lang string) string` (handoff, bouwroute: velden `issue`, `worktree`, `branch`, `next`, `when: now` die `ParseHandoff` eist, plus `range`, `workflows`) en `func RenderPushNote(rangeStr, workflows, lang string) string` (🤖-comment, fix-pr-route). Headings: `reason_pusher_off`, `reason_pusher_silent`, `question_push_*` (rode stap, diff, oordeel, commando), `note_pushed`.

- [ ] **Step 1: Tests** `TestParseHandoffAfterAndRoborev`; `TestJudgePush` (zonder `after`; met `after=/lighthouse-write-pr 396` voor issue 396 ⇒ push; `after=/bin/sh -c x` ⇒ Rejected; `push-workflows extra` ⇒ Rejected); `TestPushedHandoffParses` (`RenderPushed` → `ParseHandoff` round trip, alle vijf verplichte velden).
- [ ] **Step 2:** Run ⇒ FAIL. **Step 3:** Implementeer; `push-workflows` staat niet in `allowlist`, maar wordt vóór de allowlist-controle in `Judge` herkend.
- [ ] **Step 4:** PASS. **Step 5: Commit** `feat(protocol): handoff next push-workflows en velden after/roborev (#396)`.

---

### Taak 4: `decide` — `KindPush` en het antwoord op de pusher-Vraag

**Files:**
- Modify: `internal/dispatch/decide.go` (`Kind`, `RepoInput`, `issueCtx.handoff`, `issueCtx.answer`), `internal/cli/tick.go` (`repoInput`/`decideInput`: hier wordt `RepoInput` gebouwd; zet `WorkflowPush` en de pusher-timeout uit `hostconfig`), `internal/dispatch/gather.go`
- Test: `internal/dispatch/decide_test.go`, `internal/dispatch/gather_test.go`

**Interfaces:**
- Consumes: taak 2 (`WorkflowPush != nil`), taak 3 (`VerdictPush`).
- Produces: `KindPush Kind = "push"`; `RepoInput.WorkflowPush bool` en `RepoInput.PushTimeout time.Duration` (beide gezet in `internal/cli/tick.go` `repoInput`, niet in `gather.go`; test `TestTickPushHandoffYieldsKindPush` in `internal/cli/tick_test.go`: geconfigureerde `host.toml` ⇒ `KindPush`); `Action` krijgt `Worktree string` en `PermitSHA string` (JSON-tags `worktree`, `permit_sha`); trigger-sleutels `push:<handoff-comment-id>` en `pushanswer:<answer-id>`.

- [ ] **Step 1: Tests** (tabel in `decide_test.go`): handoff `next: push-workflows` met config, rem open, geen blokkade, run op deze host ⇒ één `KindPush` met `Worktree`/`Branch`/`TriggerKey`; zonder config ⇒ `KindReturn` reden `reason_pusher_off` (ruling: de Teruggegeven-comment staat hier voor de Vraag van spec §2/§6 en bevat het commando `cd <worktree> && git push -u origin HEAD`); handoff van een run met `node` ≠ self ⇒ Vraag-pad (`KindReturn` met die reden, geen stille skip); rem dicht of actieve blokkade ⇒ zelfde gedrag als een gewone handoff; trigger al gebruikt ⇒ niets. Antwoord `push` op een pusher-🤖 Vraag ⇒ `KindPush` met `PermitSHA` uit de Vraag; ander antwoord ⇒ geen actie (er is geen sessie).
- [ ] **Step 2:** FAIL. **Step 3:** Implementeer: in `issueCtx.handoff` een tak voor `VerdictPush` vóór `Executing/active`; `answer()` herkent een Vraag zonder sessie met het veld `sha` als pusher-Vraag. `Decide` blijft puur.
- [ ] **Step 4:** `go test ./internal/dispatch/` PASS. **Step 5: Commit** `feat(dispatch): decide KindPush voor push-workflows (#396)`.

---

### Taak 5: De push-rij in `runs`, de brake en de uitkomstregels

**Files:**
- Create: `internal/store/migrations/0008_runs_pushed.sql` (kolom `pushed_sha TEXT NOT NULL DEFAULT ''`)
- Modify: `internal/runs/runs.go` (`SetPushed`), `internal/runs/history.go` (`Row.PushedSHA`), `internal/dispatch/decide.go` (`settle`/`outcome`, `secondAttempt`), `internal/dispatch/brake.go` (`CountRun`), `internal/cli/brake.go`, `internal/dispatch/precheck.go`, `internal/dispatch/fingerprint.go`, `internal/dispatch/gather.go`
- Test: `internal/runs/runs_test.go`, `internal/dispatch/decide_test.go`, `internal/dispatch/brake_test.go`

**Interfaces:**
- Produces: `func SetPushed(ctx, db *sql.DB, id int64, sha string) error`; `Row.PushedSHA`; `func isPush(row runs.Row) bool` (`Command == "push-workflows"`).
- Regels voor een push-rij (vroege splitsing in `newRunCtx`/`outcome` op `isPush`, eigen tabel `pushOutcome`): open zonder `PushedSHA` na `timeout` ⇒ `Teruggegeven` reden `reason_pusher_silent`; met `PushedSHA` ⇒ alleen regels 6, 7, 13 (`merged`, `mergeReady`, `green`→`done`; rood ⇒ fix-pr-start); `max_runtime` en `timeout` gelden dan niet. Geen transcript- of werkplekregels.
- `Brake.CountRun` (aanroep in `internal/cli/brake.go`, over `runs.OpenAll`) telt een push-rij niet voor `slots`, `max_software` of geheugenreserve; `freesSlot` geeft `false` voor het sluiten van een push-rij (anders trekt `freed()` een slot af dat niemand hield). Brake-regel voor `KindPush`: valt bij pauze, wordt nooit uitgesteld op slots, geheugen of usage (spec §2). `secondAttempt` bouwt voor een push-rij een `KindPush` (geen `KindStart`): `triggerComment` leest ook de sleutels `push:` en `pushanswer:`, de worktree en branch komen uit de handoff-comment; voor een `pushanswer:`-rij komen worktree en `PermitSHA` uit de pusher-Vraag waarop het antwoord reageert (haar `sha`-veld en `cd <worktree>`), want de antwoord-comment draagt ze niet; na de tweede mislukking volgt de Teruggegeven met het push-commando. Tick leest de timeout uit `RepoInput.PushTimeout`; `Boundaries` (`precheck.go`) krijgt `started_at + timeout` voor push-rijen en `fpRun` krijgt `PushedSHA`, zodat een stille pusher op tijd wordt gezien.

- [ ] **Step 1: Tests** `TestPushRowNotCounted` (brake: `in_use` blijft 0), `TestPushRowCloseFreesNoSlot`, `TestPushAnswerRowRetryKeepsWorktreeAndPermit`, `TestPushBrakeGroupIgnoresSlotsButHonoursPause`, `TestPushRowBoundaryAtTimeout`, `TestPushRowTimesOutWithoutMarker`, `TestPushRowAfterPushOwnsPR` (groen PR ⇒ merge; rood ⇒ fix-pr-start; `max_runtime` genegeerd), `TestPushStartFailedGetsOneRetryThenQuestion`, `TestSetPushedRoundTrip`, migratietest bestaat al (`migrate_test.go`): voeg 0008 toe aan de verwachte lijst.
- [ ] **Step 2:** FAIL. **Step 3:** Implementeer. **Step 4:** `go test -race ./internal/runs/ ./internal/dispatch/ ./internal/store/` PASS. **Step 5: Commit** `feat(dispatch): push-rij zonder sessie met eigen uitkomstregels (#396)`.

---

### Taak 6: Pusher-kern — eigen kloon en harde checks 1–6

**Files:**
- Create: `internal/pusher/git.go` (veilige git-aanroep), `internal/pusher/floor.go`
- Test: `internal/pusher/floor_test.go`

**Interfaces:**
- Produces: `type Request struct { Repo, URL, Branch, Default, Worktree string; Issue int; RunID int64; StateDir, CloneDir string; ForgeToken string; Permit string }` (`ForgeToken` alleen voor het fetch-commando).
- Produces: `func ResolveWorktree(ctx, db *sql.DB, w workplace.Workplace, req Request, handoffTime time.Time) (string, error)` (pad uit de werkplek van de laatste `handoff`-rij vóór het comment; wijkt het handoff-pad af ⇒ fout).
- Produces: `func Floor(ctx, req Request) (Checked, *Red)` met `Checked{SHA, MergeBase string; Files []string}` en `Red{Step, Why string}`; stappen in volgorde van spec §3 (1 branch, 2 schoon, 3 fast-forward, 4 alleen eigen commits, 5 roborev-dekking, 6 rem niet op pauze).
- Produces: `func covers(record roborevRecord, sha string, commitsAfter []string) bool` — het record is de nieuwste `push-*.json` (fix-pr-route) of het branch-record waarvan `sha` voorouder van HEAD is; dan `record.SHA == sha` of elke commit erna staat als `commit` van een `fixed`-beslissing; `uitkomst` ≠ `done` ⇒ false.

- [ ] **Step 1: Tests**, elk een eigen functie op een echte tijdelijke repo met bare remote: `TestFloorRefusesDefaultBranch`, `TestFloorRefusesNonFastForward`, `TestFloorRefusesDirtyWorktree` (ook untracked), `TestFloorRefusesForeignMerge`, `TestFloorAcceptsUpdateMerge`, `TestFloorRefusesMissingRoborev`, `TestFloorRefusesSkipRecord`, `TestFloorRefusesStaleRoborev` (commit na `sha`), `TestFloorAcceptsFixCommitInRecord`, `TestFloorAcceptsFixCommitInPushRecord` (record op `push-<oude head>.json`, fix-commit erna), `TestFloorRefusesWhenPaused`, `TestFloorRefusesHandoffPathMismatch`, `TestFloorIgnoresWorktreeGitConfig` (`pre-push`-hook, `core.fsmonitor`, `url.*.insteadOf` en `~/.gitconfig` met `HOME` naar een tempdir: de marker die ze zouden schrijven bestaat na afloop niet), `TestCloneFetchFailureIsRed`, `TestSecondPusherWaitsOnFlock`.
- [ ] **Step 2:** FAIL. **Step 3:** Implementeer met `internal/flock` op `<clone>.lock`; `git fetch <worktree> <sha>` naar de bare kloon; fetch van `<default>` en `<branch>` met `ForgeToken` via `http.extraHeader` voor dat ene commando.
- [ ] **Step 4:** `go test -race ./internal/pusher/` PASS. **Step 5: Commit** `feat(pusher): eigen kloon en harde checks (#396)`.

---

### Taak 7: Policy op de workflow-diff en de triggerindeling

**Files:**
- Create: `internal/pusher/policy.go`, `internal/pusher/lint.go`
- Modify: `go.mod` (YAML-bibliotheek)
- Test: `internal/pusher/policy_test.go`

**Interfaces:**
- Produces: `func Policy(ctx, req Request, c Checked) (Class, []Violation, *Red)` met `type Class int` (`PostMerge`, `PreMerge`); parseert elke toegevoegde of gewijzigde workflow in de kloon (verwijderd telt niet).
- Regels: elke externe `uses:` op 40 hex (`./…` en `docker://…@sha256:…` mogen); expliciet `permissions` op workflow-niveau, niet `write-all`; geen `pull_request_target`/`workflow_run`. `PostMerge` alleen als elke gewijzigde workflow uitsluitend `schedule` of `push` met `branches == [default]` en zonder `tags` heeft; al het andere (ook `workflow_dispatch`, `workflow_call`) is `PreMerge`.
- Produces: `func Lint(ctx, req Request, c Checked, files []string) *Red` — `actionlint` en `zizmor --offline` op SHA en op merge-base; rood alleen voor een finding die op de merge-base niet bestond; ontbrekende binary ⇒ `Red{Why: "COULD NOT CHECK …"}`.

- [ ] **Step 1: Tests** `TestPolicyUnpinnedAction`, `TestPolicyPullRequestTarget`, `TestPolicyNoPermissions`, `TestPolicyWriteAll`, `TestPolicyLocalAndDockerDigestAllowed`, `TestClassPostMergeSchedule`, `TestClassPostMergePushDefaultOnly`, `TestClassPreMergeWorkflowDispatch`, `TestClassPreMergeCall`, `TestClassPreMergePushNoFilter`, `TestClassPostMergePushWithTagsIsPre`, `TestLintExistingFindingNotRed` (fake `zizmor` geeft dezelfde finding op beide kanten), `TestLintNewFindingRed`, `TestLintMissingBinaryIsRed`.
- [ ] **Step 2:** FAIL. **Step 3:** Implementeer; finding-sleutel = `(rule, file, text)` zonder regelnummer.
- [ ] **Step 4:** PASS. **Step 5: Commit** `feat(pusher): policy, triggerindeling en lint (#396)`.

---

### Taak 8: De security-review — insluiting, uitvoer, besluit

**Files:**
- Create: `internal/pusher/review.go`, `internal/pusher/sandbox.go`, `internal/pusher/prompts/{supply-chain,secrets,injection}.md` (embed)
- Test: `internal/pusher/review_test.go`

**Interfaces:**
- Consumes: `hostconfig.WorkflowPush` (taak 2), `Checked` (taak 6).
- Produces: `func Review(ctx, cfg hostconfig.WorkflowPush, diff string, scrub []string) Verdict` met `Verdict{Approve bool; Results []ReviewerResult}`, `ReviewerResult{Topic string; Approve bool; Findings []Finding; Why string}`.
- Elke reviewer draait onder `bwrap`: lege tempmap als enige schrijfbare bind, `binds` uit config alleen-lezen, geen `$HOME`, omgeving alleen `PATH` plus de `env`-namen uit config; prompt en diff op stdin, diff afgebakend als data; `{out}` in `command` wordt het uitvoerbestand in de tempmap; oordeel uit dat bestand. `reject` zonder finding met bewijs, ongeldige JSON, timeout, exit ≠ 0 ⇒ `reject`. De waarden van forge- en workflow-token worden uit alle reviewer-tekst geschrobd. Unaniem `approve` is de enige `Approve`. Per-reviewer timeout en flock-wacht worden afgeleid uit `WorkflowPush.Timeout` met marge (samen < timeout), met een test.

- [ ] **Step 1: Tests** (fake reviewers als shell-scripts op `PATH`): `TestReviewUnanimousApprove`, `TestReviewOneRejectBlocks`, `TestReviewRejectWithoutEvidenceIsReject` (en `approve` is dan ook niet af te leiden), `TestReviewInvalidJSONIsReject`, `TestReviewTimeoutIsReject`, `TestReviewerEnvHasNoToken` (fake dumpt `os.Environ` naar `{out}`; assert geen `GH_TOKEN`/forge/workflow-waarde), `TestReviewerCannotReadTokenFile` (fake leest het tokenbestand onder een tempdir-`HOME`; leesfout verwacht), `TestReviewMissingBwrapIsRed`, `TestReviewScrubsTokens`, `TestPromptsEmbedded` (prompts komen uit `embed`, niet uit een pad in de branch).
- [ ] **Step 2:** FAIL (bwrap-afhankelijke tests `t.Skip` met reden als `bwrap` ontbreekt of een probe `bwrap --ro-bind / / true` faalt (AppArmor op ubuntu-latest) in CI; de tests zonder bwrap draaien altijd). **Step 3:** Implementeer; schrijf de drie prompts binnen de onderwerpen van spec §4; de exacte tool-loos-vlaggen van `claude -p` en `codex exec` volgen uit hun `--help` en komen in `docs/tick.md`.
- [ ] **Step 4:** PASS. **Step 5: Commit** `feat(pusher): security-review met bwrap-insluiting (#396)`.

---

### Taak 9: Push, uitkomsten en het subcommando

**Files:**
- Create: `internal/pusher/pusher.go`, `internal/pusher/outcome.go`, `internal/cli/pushworkflows.go`
- Modify: `internal/cli/root.go` (`newPushWorkflowsCommand()`)
- Test: `internal/pusher/pusher_test.go`, `internal/cli/pushworkflows_test.go`

**Interfaces:**
- Consumes: taken 2, 3, 5, 6, 7, 8.
- Produces: `func Run(ctx, deps Deps, req Request) error` met `Deps{DB *sql.DB; Forge forge.Forge; Workplace workplace.Workplace; Now func() time.Time; ReadToken func(path string) (string, error)}`. Volgorde: vloer → policy → (PreMerge) review → token lezen (mode `0600`, anders rood) → push → uitkomst.
- Vlaggen: `--repo --issue --worktree --run-id --db --pause-file --clone-dir --state-dir --host-toml [--permit <sha>]`: tick geeft de opgeloste DB- en pauzepaden door (`XDG_STATE_HOME` en `LIGHTHOUSE_CONTROLLER_STATE` staan niet in de opgeschoonde omgeving), `TestPusherResolvesSameDBAndPauseFile` bewijst dat beide uitkomen waar tick uitkomt; leest alleen deze vlaggen en `PATH`, `HOME`, `XDG_RUNTIME_DIR`, forge-token (onder zijn eigen naam, `GH_TOKEN` voor GitHub).
- Uitkomst groen, bouwroute: `RenderPushed` met `next: <after>`, `range`, `workflows`; rij sluit `handoff`. Fix-pr-route (PR bestaat): `RenderPushNote`; `SetPushed(sha)`; rij blijft open.
- Uitkomst rood: `🤖 Vraag` met stap en reden, diff in codeblok met hek langer dan elke backtick-reeks, afgekapt onder 65536 tekens met de rest als bijlage in de worktree, reviewer-oordeel, en `cd <worktree> && git push -u origin HEAD`; rij sluit `question`. Shadow of `reject`: dezelfde Vraag, met het antwoord `push` als toestemming. Met `--permit`: vloer opnieuw, SHA onveranderd, review overgeslagen; rode vloer ⇒ nieuwe Vraag met de rode stap.

- [ ] **Step 1: Tests**: `TestPostMergeWorkflowPushesWithoutReview`, `TestPreMergeApprovePushesInEnforce`, `TestShadowAsksEvenOnUnanimousApprove`, `TestRejectAsks`, `TestPermitPushesSameSHAOnly`, `TestPermitDoesNotLiftRedFloor`, `TestRedQuestionHasNoTokenScopeAdvice` (assert geen `scope`/`workflow-scope` in de tekst), `TestQuestionFenceLongerThanBackticks`, `TestQuestionTruncatedUnderLimit`, `TestTokenFileModeMustBe0600`, `TestPushTokenOnlyInPushCommandEnv` (fake `git` op `PATH` dumpt omgeving per subcommando; alleen `push` ziet het token), `TestBuildRouteClosesHandoffWithAfter`, `TestFixPRRouteKeepsRowOpenAndSetsPushed`, en in `pushworkflows_test.go` `TestPushWorkflowsRefusesMissingConfig`.
- [ ] **Step 2:** FAIL. **Step 3:** Implementeer (token via `GIT_CONFIG_*` in de `Cmd.Env` van `git push`, nooit in argv). **Step 4:** `go test -race ./internal/pusher/ ./internal/cli/` PASS. **Step 5: Commit** `feat(cli): lighthouse push-workflows (#396)`.

---

### Taak 10: `apply` start de pusher

**Files:**
- Modify: `internal/dispatch/apply.go` (`Env.StartPusher`, `apply` voor `KindPush`), `internal/cli/tick.go` (bedrading), `internal/dispatch/apply_test.go`
- Test: `internal/dispatch/apply_test.go`, `internal/cli/tick_test.go`

**Interfaces:**
- Produces: `Env.StartPusher func(ctx context.Context, spec PusherSpec) error` met `PusherSpec{Repo string; Issue int; Worktree string; RunID int64; Permit string; Env []string}`; de standaardimplementatie in `tick.go` gebruikt `setsid` (patroon `internal/hooks/detach_unix.go`) en `Env` = alleen `PATH`, `HOME`, `XDG_RUNTIME_DIR`, forge-token.
- `apply` voor `KindPush`: `runs.Take` (command `push-workflows`, `SessionID ""`, `Mode "start"`, `Trigger "handoff"` of `"answer"`: de CHECK-constraints van `runs` (`0004_runs_fixpr.sql:5-6`) laten alleen die waarden toe en migratie 0008 herbouwt de tabel niet; een push-rij herken je aan `Command`; trigger-sleutel van de actie), 🚀 op de handoff, dan `StartPusher`; een fout daarbij sluit de rij `start_failed`.

- [ ] **Step 1: Tests** `TestApplyPushClaimsRowAndStartsPusher` (fake `StartPusher` legt de spec vast; assert `Env` bevat geen `WORKFLOW_PUSH_TOKEN` en geen andere niet-genoemde variabele uit een tick-omgeving, en bevat het forge-token precies onder zijn eigen naam), `TestApplyPushStartFailureClosesStartFailed`, `TestApplyPushSpentTriggerStartsNothing`.
- [ ] **Step 2:** FAIL. **Step 3:** Implementeer. **Step 4:** PASS. **Step 5: Commit** `feat(dispatch): apply start de pusher met opgeschoonde omgeving (#396)`.

---

### Taak 11: De run pusht geen workflow-commits — hook, `post-artefact`, roborev-record, skills

**Files:**
- Create: `internal/hooks/workflowpushgate.go`, `pack/core/hooks` registratie (`hooks.json`, `blocking`)
- Modify: `internal/postartefact/postartefact.go` (`ensureCommitOnGitHub`), `internal/roborev/gate.go`, `internal/cli/roborev.go` (vlag `--push` zonder drempel, record `push-<head>.json`), `pack/core/skills/{write-handoff,write-pr,fix-pr}/SKILL.md`, `.claude/` kopieën via init
- Test: `internal/hooks/workflowpushgate_test.go`, `internal/postartefact/postartefact_test.go`, `internal/roborev/gate_test.go`

**Interfaces:**
- Produces: hook `workflow-push-gate` (PreToolUse `Bash`): alleen met `LIGHTHOUSE_CONTROLLER_RUN` gezet blokkeert hij een `git push` waarvan `origin/<branch>..HEAD` (of `merge-base(origin/<default>, HEAD)..HEAD` als de branch niet op origin staat) een pad onder `.github/workflows/` raakt; melding noemt V2b en `next: push-workflows`, geen scope; niet-meetbaar ⇒ `COULD NOT CHECK` en blokkeren; buiten een controller-run niets.
- `roborev gate --push` (vlag in `internal/cli/roborev.go`): slaat de coderegel-drempel over (skip blijft rood), leest en schrijft alleen `<git-dir>/lighthouse/roborev/push-<head>.json` met veld `sha` (een bestaand branch-record, `gate.go:117`, wordt niet teruggegeven). Bouwroute (V2b in `write-handoff`/`write-pr`) en fix-pr-route draaien V2b allebei met `--push`, anders schrijft de drempel (100 coderegels) een skip dat check 5 rood telt.
- `post-artefact` weigert met dezelfde melding als de range `.github/workflows/` raakt.

- [ ] **Step 1: Tests** `TestWorkflowPushGateBlocksInControllerRun`, `…AllowsOutsideControllerRun`, `…AllowsNonWorkflowRange`, `…UsesMergeBaseWhenBranchNotOnOrigin`, `…CouldNotCheckBlocks`, `…MessageHasNoScopeAdvice`; `TestPostArtefactRefusesWorkflowRange`; `TestRoborevGatePushSkipsThreshold` (3 coderegels ⇒ `verdict: run`), `TestRoborevGatePushIgnoresBranchRecord`.
- [ ] **Step 2:** FAIL. **Step 3:** Implementeer; skills krijgen één regel (spec §1) de bouwroute en de fix-pr-route reviewen `origin/<branch>..HEAD` met `--push`; run `go run ./cmd/lighthouse init --upgrade`.
- [ ] **Step 4:** PASS, `go run ./cmd/lighthouse init --upgrade` meldt na commit `No changes.` **Step 5: Commit** `feat(hooks): workflow-push-gate en push-record (#396)`.

---

### Taak 12: Merge door de pusher (alleen als taak 1 weigering toonde)

**Files:**
- Modify: `internal/cli/pushworkflows.go` (`--merge --pr <n> --sha <head>`), `internal/dispatch/gather.go` (nieuw invoerveld `IssueInput.PushedSHA`: de SHA uit de nieuwste pusher-comment, `RenderPushed`/`RenderPushNote`, op het issue), `internal/dispatch/decide.go` (`mergeReady`: heeft het issue een `PushedSHA`, dan start een groene PR `push-workflows --merge` in plaats van `KindMerge`, ongeacht welke rij de PR bezit, ook de write-pr-run op de bouwroute; `decide` kijkt niet naar bestanden of voorouderschap, dat doet `Merge` zelf en weigert anders), `internal/dispatch/apply.go`
- Test: `internal/pusher/pusher_test.go`, `internal/dispatch/decide_merge_test.go`

**Interfaces:**
- `func Merge(ctx, deps Deps, req Request, pr int, head string) error`: merget met het workflow-token alleen als de gepushte SHA (uit de push-comment) voorouder is van `head` en `gepusht..head` geen pad onder `.github/workflows/` raakt. Tick leest het token nooit.

- [ ] **Step 1:** Is de uitkomst van taak 1 "GitHub merget zonder bezwaar", sla deze taak over en zeg dat in de handoff; anders **Tests** `TestMergeRefusesNonAncestor`, `TestMergeRefusesWorkflowChangeAfterPush`, `TestMergeAllowsChangelogFragmentAfterPush`, `TestDecideMergeStartsPusherForPushRow`, `TestDecideMergeStartsPusherForBuildRoutePR` (invoer: `PushedSHA` gezet, groene PR van een write-pr-rij), `TestDecideMergeUsesRunTokenWithoutPushedSHA`, `TestGatherPushedSHAFromPusherComment`.
- [ ] **Step 2:** FAIL. **Step 3:** Implementeer. **Step 4:** PASS. **Step 5: Commit** `feat(pusher): merge met workflow-token (#396)`.

---

### Taak 13: Docs, `loop.json`, changelog

**Files:**
- Modify: `docs/tick.md` (*Wat tick doet*, *Afloop*, *`[dispatch]` in `host.toml`*), `docs/site/data/loop.json` (node `push-workflows`), `docs/install.md` (tokenbestand, fine-grained, *Contents* en *Workflows*), `docs/releasing.md` niet
- Create: `changelog.d/feat-workflow-pusher.md`
- Test: `uv run pytest -q tests/` (o.a. `test_site_loop.py`, `test_loop_roadmap.py`, `test_changelog.py`, `test_instruction_files.py`); `loop.json` en de write-handoff-tabel in `pack/core/skills/write-handoff/SKILL.md` krijgen `push-workflows` samen

- [ ] **Step 1:** Voeg de teksten toe volgens spec §8; `loop.json` krijgt handoffs vanuit de bouw- en fix-pr-route en naar `write-pr` en `fix-pr`.
- [ ] **Step 2:** Run `go test -race ./internal/...`, `uv run pytest -q tests/` en `lighthouse check-instructions`-equivalent van de repo (`go run ./cmd/lighthouse check-instructions`); verwacht PASS.
- [ ] **Step 3: Commit** `docs(tick): workflow-pusher beschreven (#396)`.

---

## Zelfreview

- **Dekking spec:** §1 → taak 11; §2 → taken 3, 4, 5, 10; §3 → 6, 7; §4 → 8; §5 → 9 (en 4 voor het antwoord); §6 → 2; §7 → 1, 12; §8 → 13; Tests-sectie verdeeld over de taken (elke weigering een eigen test in taak 6/7).
- **Typeconsistentie:** `Checked`, `Red`, `Request`, `Verdict` (pusher) en `hostconfig.WorkflowPush` worden in taken 6–9 met dezelfde namen gebruikt; `Action.PermitSHA` (taak 4) ⇒ `PusherSpec.Permit` (taak 10) ⇒ `--permit` (taak 9).
- **Open voor de bouwer (spec):** hoe de hook een controller-run herkent (`LIGHTHOUSE_CONTROLLER_RUN` is de marker die `permission-check` al gebruikt), exacte reviewer-vlaggen, reviewer-prompts.
- **Open na review r3-f1:** `binds`/`env` per reviewer zijn in taak 2 en 8 opgenomen; de PR-review weegt of de voorbeeldreviewers (`claude -p`) daarmee werken.
