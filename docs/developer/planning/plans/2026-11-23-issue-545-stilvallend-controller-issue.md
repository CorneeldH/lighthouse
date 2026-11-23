# Controller-issue dat stilvalt: herstellen, teruggeven met reden, vangnet — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Een issue op status `Controller` valt nooit meer onzichtbaar stil: de tick herstelt waar het vervolg vaststaat, geeft anders terug met een reden, en een vangnet geeft een issue zonder enig vervolg na drie ticks terug; een bezette werkplek staat in Queue en een antwoord van een niet-vertrouwde auteur staat als open vraag in `/api/questions`.

**Architecture:** `Decide` blijft puur. De regelketen `issueCtx.next()` (`internal/dispatch/decide.go`) krijgt gerichte uitbreidingen in `answer()`, `handoff()`/`awaitsPR()` en een nieuwe regel `idle` achteraan. Wat git of een teller nodig heeft, gebeurt in `apply` (branch-controle) of komt als invoer binnen (`RepoInput.IdleTicks`, uit `tick_state` kind `idle`). Een bezette werkplek wordt in `tick.go` samen met de `KindDefer`-acties naar `SaveDeferred` gebracht. De overview krijgt een vertrouwenscontrole voor antwoorden.

**Tech Stack:** Go (`internal/dispatch`, `internal/cli`, `internal/overview`, `internal/server`, `internal/protocol`), SQLite `tick_state`. Volgt `/lighthouse-go-standard` en `/lighthouse-test-standard`.

**Spec:** geen aparte spec; het ontwerp ("B uit het gesprek van 2026-11-23") en de zes acceptatiecriteria staan in [issue #545](https://github.com/CorneeldH/lighthouse/issues/545) en gelden als spec.

## Global Constraints

- Code, codecommentaar, testnamen en foutmeldingen in het Engels (`internal/**`); plan, `docs/tick.md` en PR in het Nederlands; het changelog-fragment in het Engels (`docs/releasing.md`).
- Reden van een teruggave is een headings-sleutel `reason_*` in `pack/core/headings.toml` (nl én en) en het geïnstalleerde `.lighthouse/headings.toml`; geen losse tekst in `decide.go`.
- `Decide` blijft pure functie van `Input`: geen git-, forge- of db-aanroep in `decide*.go`.
- Een bestaand `Decide`-resultaat van de fixtures in `decideFixtures` (`internal/dispatch/decide_test.go:28`) verandert niet, behalve waar een taak het uitdrukkelijk zegt.
- Geen automatisch oppakken van een teruggegeven issue, geen nieuwe controllerfuncties (milestone No-Gos).
- Vangnet-drempel: `idleTickLimit = 3` ticks.
- Vóór elke commit: `[test_gate] command` uit `lighthouse.toml` en `lighthouse imports check`. Atomic Conventional Commits, nooit `--no-verify`. Een wijziging onder `pack/` volgt met `go run ./cmd/lighthouse init --upgrade` en commit van de kopie (CI eist `No changes.`). Eén fragment in `changelog.d/`.

## Review Focus

1. Een tweede antwoord `push` ná een eerste antwoord `gepushed`: de tick mag de Vraag niet nog eens teruggeven of twee keer starten (Task 1).
2. De worktree of branch van de Vraag bestaat niet meer, of `git` faalt: teruggave met reden `COULD NOT CHECK`, geen start en geen paniek (Task 1).
3. Een teruggegeven issue dat zelf weer in `Controller` komt zonder nieuw protocol-comment: geen teruggave-lus (Task 5).
4. De teller van het vangnet loopt niet door voor een issue met een pauze, een open run, een bezette werkplek of een open PR, en wordt gewist zodra het issue weer een vervolg heeft (Task 5).
5. Een antwoord van een niet-vertrouwde auteur mag nooit een start veroorzaken; alleen de overview toont het (Task 4).

---

## File Structure

- `internal/dispatch/decide.go` — `answer`, `answerTo`, `pushAnswer`, `awaitsPR`, `next`; nieuwe regels.
- `internal/dispatch/decide_idle.go` (nieuw) — `idle()`, `idleCandidate()`, `idleTickLimit`.
- `internal/dispatch/apply.go` — `KindPushRecover` (branch-controle in git) en `busy`-afhandeling.
- `internal/dispatch/precheck.go`, `view.go` — `SaveIdle`/`LoadIdle`; `SaveDeferred` met apply-uitstel.
- `internal/cli/tick.go` — uitstel en teller bewaren.
- `internal/overview/overview.go`, `internal/cli/serve.go` — vertrouwde antwoorden.
- `pack/core/headings.toml`, `.lighthouse/headings.toml` — nieuwe `reason_*`-sleutels.
- `docs/tick.md`, `changelog.d/issue-545-controller-issue-dat-stilvalt-tick.md` (met front matter `---`, `type: fixed`, `---`, zie `docs/releasing.md`).

---

### Task 1: Pusher-Vraag met een ander antwoord dan `push`

**Files:**
- Modify: `internal/dispatch/decide.go` (`answerTo`, `pushAnswer`, `permitAnswer`), `internal/dispatch/apply.go` (nieuwe `KindPushRecover`, naast `KindPush` op `apply.go:174`; `Env.LsRemote`), `pack/core/headings.toml`, `.lighthouse/headings.toml`
- Test: `internal/dispatch/decide_test.go` (`TestDecidePushAnswer`, :364), `internal/dispatch/apply_test.go`

**Interfaces:**
- Produces: `func (c issueCtx) answersTo(id int64) []Comment` (alle antwoorden na de Vraag, oudste eerst; `answerTo` blijft het eerste teruggeven voor sessie-Vragen en ongewijzigd gedrag); `KindPushRecover Kind` met `Action{Worktree, Branch, PermitSHA, CommentID}` plus twee nieuwe velden `AnswerText string` en `After string` (het `after:`-commando van de Vraag; `Decide` leest het uit de Vraag-header); headings-sleutels `reason_pusher_answer_unknown` (arg: het antwoord), `reason_pusher_answer_moved` (arg: het antwoord), `reason_pusher_no_after` (geen `after:` in de Vraag) en `reason_pusher_git_failed` (arg: de gitfout; de tekst begint met `COULD NOT CHECK`). `Env` krijgt een naad `LsRemote func(ctx context.Context, worktree, branch string, env []string) (sha string, err error)`; productie roept `git ls-remote origin refs/heads/<branch>` aan, tests zetten de naad.
- Beslissing (vastgelegd, niet opnieuw te kiezen): `pushAnswer` kijkt naar het **laatste** antwoord op de Vraag. Is dat `push` (`permitAnswer`) → ongewijzigd `KindPush`. Is het iets anders → `KindPushRecover`. Voor een Vraag mét sessie blijft `answerTo` het eerste antwoord lezen.
- Beslissing: "de branch staat op de `sha` van de Vraag" betekent dat `git ls-remote origin refs/heads/<branch>` in de worktree van de Vraag precies `sha` geeft (de push is dan al gebeurd, bijvoorbeeld met de hand). `apply` doet die controle; `Decide` blijft puur.

- [ ] **Step 1: Schrijf de falende tests** in `TestDecidePushAnswer` (tabel met `pushQuestion(t, answer)`): case "other answer" (:373) herschrijven naar de nieuwe verwachting `KindPushRecover` met `PermitSHA` = sha van de Vraag en `AnswerText: "gepushed"`; nieuwe cases "push after other answer" (twee antwoorden `gepushed`, dan `push` → `KindPush`), "other answer, workflow push off" (→ `KindReturn` met `reason_pusher_off`), "spent key" (→ geen actie). In `apply_test.go`: `TestApplyPushRecoverBranchOnSHA` (ls-remote geeft sha → een `🤖 Handoff`-comment met `next: <after>` gepost en geen pusher gestart; de claim-rij is gesloten met uitkomst handoff), `TestApplyPushRecoverBranchElsewhere` (andere sha → `KindReturn`-gedrag: Teruggegeven met reden `reason_pusher_answer_moved`, argument bevat `gepushed`), `TestApplyPushRecoverGitFails` (ls-remote faalt → Teruggegeven `COULD NOT CHECK`, geen start).
- [ ] **Step 2: Run** `go test ./internal/dispatch/ -run 'TestDecidePushAnswer|TestApplyPushRecover' -v`. Verwacht: FAIL (onbekend `KindPushRecover`, oud gedrag `[]`).
- [ ] **Step 3: Implementeer** in `decide.go`: `answersTo`; `pushAnswer` neemt het laatste antwoord, `permitAnswer` beslist tussen `KindPush` en `KindPushRecover`; sleutel `triggerPushAnswer:<id>` blijft de uitgave-sleutel. In `apply.go`: `ap.pushRecover(ctx, a, res)` doet de `git ls-remote` met de pusher-omgeving van `pusherEnv` zonder token, gelijk → neem **eerst** de uitgave-sleutel `triggerPushAnswer:<id>` met `runs.Take` zoals `ap.push` (stop bij `ErrClaimed`/`ErrTriggerSpent`), post **dan** de handoff die de pusher na een geslaagde push zelf post: `protocol.RenderPushed(issue, worktree, branch, range, workflows, next=a.After, lane, language)` met `range`/`workflows` leeg waar er niets te melden valt (zie `internal/pusher/outcome.go:124`), en sluit de rij **meteen** met `runs.Release(ctx, db, run.ID, runs.Handoff, "", now)` zoals `outcome.go:139` (zonder `StartPusher`; een open push-rij zou `handoff()` blokkeren en na de push-timeout `reason_pusher_silent` geven, en `settle` slaat comments van vóór `StartedAt` over); de bestaande `handoff()`-regel start het `after:`-commando dan in dezelfde of de volgende tick, mét brake, plaatsing, nachtvenster en `judgeHandoff`. Zonder `after` → `KindReturn`-gedrag met `reason_pusher_no_after`. Ongelijk of fout → eerst `protocol.RenderReturned` met de reden posten (zoals het `KindReturn`-pad `apply.go:138-146`), daarna `ap.giveBack(ReturnLabelKey(...))`: `giveBack` alleen zet label en status en post niets. Voeg de vier sleutels in nl en en toe aan beide headings-bestanden.
- [ ] **Step 4: Run** dezelfde tests plus `go test ./internal/dispatch/...`. Verwacht: PASS, geen fixture-verschil.
- [ ] **Step 5: Commit** `fix(dispatch): pusher-Vraag met een ander antwoord dan push herstelt of geeft terug (#545)`.

### Task 2: Beantwoorde Vraag zonder sessie of run, en gesloten PR zonder merge

**Files:**
- Modify: `internal/dispatch/decide.go` (`answer`, `awaitsPR`, `handoff`)
- Test: `internal/dispatch/decide_test.go`, `internal/dispatch/decide_merge_test.go`

**Interfaces:**
- Produces: headings-sleutels `reason_answer_no_session`, `reason_answer_no_run`, `reason_pr_closed_unmerged` (arg: PR-nummer).
- Beslissing: in `answer()` geeft een Vraag met sessie waarvoor `lastRunOf` niets vindt nu `c.returned("reason_answer_no_run", q.Header.Session)` (eenmaal: de geposte Teruggegeven is daarna de laatste beurt, `KindReturn` claimt geen run, dus `c.spent` helpt hier niet); een Vraag zonder sessie en zonder `sha` geeft `reason_answer_no_session`. `awaitsPR` telt een PR die zonder merge gesloten is (`pr.State == "CLOSED"` met lege `MergedAt`) niet meer als wachten; `handoff()` geeft dan `c.returned("reason_pr_closed_unmerged", strconv.Itoa(pr.Number))`.

- [ ] **Step 1: Falende tests**: `TestDecideAnswerNoRun`, `TestDecideAnswerNoSession` (beide: verwacht één `KindReturn` met de reden; de tweede tick met een Teruggegeven-comment als laatste beurt geeft niets), `TestDecideEndHandoffClosedPRWithoutMerge` (eind-handoff `/lighthouse-close-issue`, PR gesloten zonder merge → `KindReturn` `reason_pr_closed_unmerged`; PR `OPEN` blijft wachten; PR gemerged blijft naar de close-check gaan).
- [ ] **Step 2: Run** `go test ./internal/dispatch/ -run 'TestDecideAnswerNo|TestDecideEndHandoffClosedPR' -v`. Verwacht: FAIL.
- [ ] **Step 3: Implementeer** de drie wijzigingen en voeg de sleutels in nl en en toe (beide headings-bestanden).
- [ ] **Step 4: Run** `go test ./internal/dispatch/...`. Verwacht: PASS.
- [ ] **Step 5: Commit** `fix(dispatch): beantwoorde Vraag zonder run en gesloten PR zonder merge geven terug (#545)`.

### Task 3: Gestarte of hervatte run met gesloten parkeerrij en verdwenen marker

**Files:**
- Modify: `internal/dispatch/decide.go` (`usageResume` en de regel die een rij met marker leest; `grep -n "Markers" internal/dispatch/decide*.go` vindt hem; nieuw veld `RepoInput.MarkersIncomplete bool`), `internal/cli/brake.go` en de gather in `internal/cli/tick.go` (zetten het veld waar een node-marker niet te lezen was, `brake.go:111-117`)
- Test: `internal/dispatch/decide_test.go`, `internal/dispatch/brake_test.go` waar nodig

**Interfaces:**
- Produces: headings-sleutel `reason_park_marker_gone` (arg: sessie-id).
- Beslissing: een Controller-issue waarvan de laatste run een **gesloten** parkeerrij is (uitkomst parkeren door usage) en waarvoor geen marker in `RepoInput.Markers` of `Unreadable` staat en `MarkersIncomplete` onwaar is (een node waarvan de markers deze tick niet te lezen waren, is geen verdwenen marker), geeft `c.returned("reason_park_marker_gone", sessie)`. Met marker of `Unreadable` blijft het gedrag zoals nu (wachten).

- [ ] **Step 1: Zoek** de plek: lees `usageResume` (`decide.go:1615`) en het bestaande gedrag bij `Unreadable`; noteer in de test waarom de huidige code niets doet voor "rij gesloten, marker weg".
- [ ] **Step 2: Falende test** `TestDecideUsageParkMarkerGone` (rij gesloten, geen marker → `KindReturn` met de reden; marker aanwezig → `usageResume`-start of wachten zoals voorheen; `Unreadable` of `MarkersIncomplete` → wachten). Run `go test ./internal/dispatch/ -run TestDecideUsageParkMarkerGone -v`. Verwacht: FAIL.
- [ ] **Step 3: Implementeer** de regel binnen `usageResume` of als eigen regel direct erna in `next()`; voeg de sleutel in nl en en toe.
- [ ] **Step 4: Run** `go test ./internal/dispatch/...`. Verwacht: PASS.
- [ ] **Step 5: Commit** `fix(dispatch): parkeerrij zonder marker geeft het issue terug (#545)`.

### Task 4: Bezette werkplek in Queue en vertrouwd antwoord in de overview

**Files:**
- Modify: `internal/dispatch/apply.go` (busy-pad `apply.go:400-408`), `internal/cli/tick.go:485-498`, `internal/overview/overview.go`, `internal/cli/serve.go:113-143`
- Test: `internal/cli/tick_test.go` (naast `TestTickDeferredTitle`, :1582), `internal/cli/serve_test.go`, `internal/dispatch/apply_test.go`, `internal/dispatch/view_test.go`, `internal/overview/overview_test.go`, `internal/server/forge_test.go`

**Interfaces:**
- Produces: `Result.DeferredActions []Action` (elke apply-time uitstel als `Action{Kind: KindDefer, Issue, Reason}`; `Result.Deferred []string` blijft ongewijzigd voor bestaande tests); `overview.RepoSource.NewTrusted func() func(ctx context.Context, login string) (bool, error)` (een fabriek: elke `Build` krijgt een verse `Trust`, want `dispatch.Trust` is niet concurrentieveilig en cachet rollen voor altijd, `trust.go:11`; nil = alles vertrouwd, zodat bestaande tests blijven slagen).
- Beslissing: `tick.go` geeft `SaveDeferred` de `repoDefers(actions, slug)` **plus** de `DeferredActions` van dat repo. De fingerprint-uitzondering (`tick.go:507-511`) blijft. `overview.Build` kijkt per issue met `protocol.Classify` naar de laatste Vraag; is het antwoord er één van een niet-vertrouwde auteur (Trusted geeft false), dan blijft de Vraag open in `Questions`. `fetchStatuses` geeft per issue ook de auteur van het antwoord terug; de vertrouwenscontrole loopt **na** `wg.Wait` sequentieel, niet in de goroutines van `overview.go:198-231`. `serve.go` vult `NewTrusted` voor elke productiebron met `func() ... { return dispatch.NewTrust(f, cfg.AnswerPermissions).Trusted }`.

- [ ] **Step 1: Falende tests**: `TestTickBusyWorkplaceSavedAsDeferred` (apply geeft busy → `LoadView` toont een deferred-rij met reden en titel), `TestBuildUntrustedAnswerStaysQuestion` en `TestBuildTrustedAnswerIsAnswered` in `overview_test.go` (helper `question` :46), een `forge_test.go`-case die `/api/runs` de rij laat tonen en `/api/questions` de open vraag, een `serve_test.go`-test die via `forgeReposWith` bewijst dat elke productiebron een niet-nil `NewTrusted` met `cfg.AnswerPermissions` krijgt, en in `internal/dispatch/apply_test.go` een test dat een bezette werkplek in `Result.DeferredActions` komt (AC1 eist de test in `internal/dispatch`).
- [ ] **Step 2: Run** `go test ./internal/cli/ ./internal/overview/ ./internal/server/ -run 'Busy|Untrusted|Trusted' -v`. Verwacht: FAIL.
- [ ] **Step 3: Implementeer** `DeferredActions` in `ap.busy`-pad en `KindDefer`-pad van `apply`; `tick.go` voegt ze toe voor `SaveDeferred`; overview krijgt `Trusted` en past `Build` aan; `serve.go` vult het veld. Werk `web/openapi.json` alleen bij als de response-vorm wijzigt (dat doet hij niet).
- [ ] **Step 4: Run** `go test ./internal/dispatch/... ./internal/cli/... ./internal/overview/... ./internal/server/...`. Verwacht: PASS, ook de exacte-string tests op `Deferred` (`apply_test.go:1942`).
- [ ] **Step 5: Commit** `fix(tick): bezette werkplek staat in Queue en onvertrouwd antwoord is een open vraag (#545)`.

### Task 5: Vangnet voor een Controller-issue zonder enig vervolg

**Files:**
- Create: `internal/dispatch/decide_idle.go`
- Modify: `internal/dispatch/decide.go` (`RepoInput.IdleTicks map[int]int`, `next()`), `internal/dispatch/precheck.go` (`SaveIdle`, `LoadIdle`), `internal/cli/tick.go` (telling, en `KindIdle` uit de actielijst halen vóór `applyActions`: `apply.go:206` kent hem niet), headings-bestanden
- Test: `internal/dispatch/decide_idle_test.go`, `internal/dispatch/precheck_test.go`, `internal/cli/tick_test.go`

**Interfaces:**
- Produces: `const idleTickLimit = 3`; `func (c issueCtx) idle() (planned, bool)` (laatste regel in `next()`); `KindIdle Kind` (geen bijwerking, alleen een markering dat het issue deze tick geen vervolg heeft); `func SaveIdle(ctx context.Context, db *sql.DB, repo string, counts map[int]int, now time.Time) error` en `func LoadIdle(ctx context.Context, db *sql.DB, repo string) (map[int]int, error)` (opslag: één JSON-map issuenummer → teller onder `tick_state` kind `fingerprint`, sleutel `idle`, zoals `SaveToDo` met `toDoKey`; de `CHECK` van `internal/store/migrations/0003_cleanup.sql:3` staat geen kind `idle` toe en er komt geen migratie); headings-sleutel `reason_no_rule` met velden voor de laatste protocol-comment (arg: de eerste regel van die comment, of "geen").
- Beslissing: `idle()` vuurt alleen als `next()` geen andere regel gaf **en** het issue vrij is (geen open run), niet `needsHuman()`, niet `c.active()`, geen pauze, geen deferred, geen open PR (`awaitsPR` of een PR in `c.it.PRs` met `State == "OPEN"`), niet al teruggegeven (`finishReturn`-toestand), en de laatste beurt (`lastTurn`) is geen Vraag zonder antwoord (die wacht op een mens, ook een pusher-Vraag). Is `c.repo.IdleTicks[issue]+1 >= idleTickLimit` (de huidige evaluatie telt mee: drie opeenvolgende evaluaties geven terug) dan komt `c.returned("reason_no_rule", laatsteProtocolComment)`; anders `KindIdle`. `Decide` draait alleen bij een volledige gather (`precheck.Due`, `tick.go:604-616`, bij een ongewijzigd bord hoogstens elke `FullGatherAfter` = 30 minuten), dus "tick" betekent hier: een volledige gather; `docs/tick.md` zegt dat. "Geen pauze" is: geen geparkeerde run (marker) en `MarkersIncomplete` onwaar. `tick.go` telt na `Decide` en haalt `KindIdle` daarna uit de actielijst, zodat `Apply` hem nooit ziet: elk issue met `KindIdle` +1, elk ander issue van het repo wordt uit de teller gewist; `SaveIdle` bewaart het resultaat; `LoadIdle` voedt `RepoInput.IdleTicks` bij de volgende tick.

- [ ] **Step 1: Falende tests** (`decide_idle_test.go`): `TestIdleBelowLimit` (teller 1 → `KindIdle`), `TestIdleAtLimitReturns` (teller 2 → `KindReturn` `reason_no_rule`, arg noemt de laatste protocol-comment), `TestIdleLeavesPauseAlone`, `TestIdleLeavesBusyWorkplaceAlone` (uitstel uit placement), `TestIdleLeavesOpenPRAlone`, `TestIdleLeavesOpenRunAlone`, `TestIdleLeavesOpenQuestionAlone` (sessie-Vraag en pusher-Vraag zonder antwoord), `TestIdleThreeEvaluationsReturn` (vanaf teller 0 geeft de derde evaluatie terug), `TestIdleAfterReturnNoLoop` (laatste protocol-comment is al een Teruggegeven van de tick → geen nieuwe teruggave). In `precheck_test.go`: `TestSaveLoadIdleRoundTrip` en wissen. In `tick_test.go`: `TestTickIdleCounterAdvancesAndResets` en `TestTickIdleActionNeverReachesApply`.
- [ ] **Step 2: Run** `go test ./internal/dispatch/ ./internal/cli/ -run 'Idle' -v`. Verwacht: FAIL.
- [ ] **Step 3: Implementeer** `decide_idle.go`, `next()`-aanpassing, `SaveIdle`/`LoadIdle`, `tick.go` en de sleutel in nl en en (beide headings-bestanden).
- [ ] **Step 4: Run** `go test ./internal/dispatch/... ./internal/cli/...`. Verwacht: PASS; de `decideFixtures` geven geen `KindIdle` waar ze dat niet horen te geven (pas een fixture alleen aan met een korte reden in de test).
- [ ] **Step 5: Commit** `feat(dispatch): vangnet geeft een Controller-issue zonder vervolg terug (#545)`.

### Task 6: Documentatie, dogfood en changelog

**Files:**
- Modify: `docs/tick.md` (secties `## Teruggegeven en het statusmodel` :722, `## Zicht` :1101, `### Afloop van een push-rij` :421), `docs/install.md:184-185,359` waar `/api/runs` en `/api/questions` staan
- Create: `changelog.d/issue-545-controller-issue-dat-stilvalt-tick.md` (met front matter `---`, `type: fixed`, `---`, zie `docs/releasing.md`)

- [ ] **Step 1: Schrijf** in `docs/tick.md` de nieuwe regel voor een pusher-Vraag met een ander antwoord (herstel op `sha`, anders teruggave met het antwoord), de teruggaven uit Task 2 en 3, het vangnet (drempel drie ticks, wat het met rust laat, de teller in `tick_state` kind `fingerprint`, sleutel `idle`, geteld per volledige gather) en onder `## Zicht` de bezette werkplek in Queue en de open vraag bij een onvertrouwd antwoord. Alleen relatieve links naar bestaande doelen.
- [ ] **Step 2: Fragment** in het Engels, één tot drie zinnen: a Controller issue that has nowhere to go is now recovered or returned with a reason, and a busy workplace and an untrusted answer show up in the UI.
- [ ] **Step 3: Dogfood**: run `go run ./cmd/lighthouse init --upgrade` en commit de gewijzigde kopie (`.lighthouse/headings.toml`); run daarna opnieuw en verwacht `No changes.`.
- [ ] **Step 4: Volledige verificatie**: `[test_gate] command` uit `lighthouse.toml`, `go test ./...`, `lighthouse imports check`. Verwacht: groen.
- [ ] **Step 5: Commit** `docs(tick): stilvallend Controller-issue, vangnet en zicht (#545)`.

## Zelfreview

- Dekking: AC1 (test per toestand) → Task 1–5; AC2 → Task 1; AC3 → Task 5; AC4 → Task 4; AC5 → Task 4; AC6 → Task 6.
- Gesloten parkeerrij zonder marker (Task 3) en het precieze punt waar de regel landt zijn bewust aan de uitvoerder gelaten na `Step 1` (lezen van `usageResume`); de reden, het gedrag en de test staan vast.
