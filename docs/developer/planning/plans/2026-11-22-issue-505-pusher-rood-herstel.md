# Pusher-rood terug naar het werk — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Een rode `policy` of `lint` van de workflow-pusher die een fout van het werk is, hervat de run die de push-handoff schreef, in plaats van het issue terug te geven.

**Architecture:** De pusher post voor zo'n rood een eigen 🤖 **Herstel** (nieuwe `protocol.Kind` `repair`) en sluit zijn row als `handoff`. Een nieuwe regel `repair` in `next()` zoekt de handoff-run in de historie en hervat zijn sessie (trigger `repair`, in de database `answer`); na `maxRepairRuns` aaneengesloten Herstel-rondes volgt een Teruggegeven.

**Tech Stack:** Go (`internal/protocol`, `internal/pusher`, `internal/dispatch`, `internal/runs`), `pack/core/headings.toml`, `docs/site/data/loop.json`.

**Spec:** [2026-11-22-issue-505-pusher-rood-herstel-design.md](../specs/2026-11-22-issue-505-pusher-rood-herstel-design.md)

## Global Constraints

- Artefacten (commits, changelog, plan) in het Nederlands; skills en hookteksten in het Engels. Code-identifiers Engels.
- Nieuwe constante `maxRepairRuns = 2` (naast `maxFixPRRuns` in `internal/dispatch/decide.go`), niet instelbaar.
- Heading-sleutels: `kind_repair` (nl `Herstel`, en `Repair`), `prompt_pusher_repair`, `reason_pusher_red_ceiling`. In `pack/core/headings.toml` én `.lighthouse/headings.toml`.
- Volgorde in `next()`: `retry, finishReturn, usageResume, answer, repair, closeCheck, newIssue, handoff`.
- Trigger-sleutel van een hervatting: `repair:<comment-id>`; trigger `repair`, bij `runs.Take` omgezet naar `answer`. Geen migratie.
- Alleen `policy` en `lint` zonder het prefix `COULD NOT CHECK:` zijn repareerbaar. Een `RuleParse`-overtreding is repareerbaar.
- Een Herstel telt alleen als de auteur `Snapshot.TokenLogin` is.
- Geen wijziging aan `internal/pusher/merge.go`, `repeatsHandoff` of `finishReturn`.
- Raakt iets `pack/`: `go run ./cmd/lighthouse init --upgrade` en de geïnstalleerde kopie meecommitten.
- Vóór elke commit: `lighthouse imports check` en de targeted test van de taak.

## Review Focus

1. **`COULD NOT CHECK` van `policy`/`lint`** (linter niet op `PATH`, uitvoer geen JSON, `workflowDiff` faalt) blijft een Teruggegeven met row `returned`, nooit een Herstel. Test in Taak 2: `TestRepairableClassifiesOnPrefix` en `TestCouldNotCheckStaysReturned`.
2. **Herstel van een andere auteur dan de token-login** sluit geen row en start geen run. Test in Taak 3 en 4.
3. **Teller reset** bij een gepushte row, een `question`-row of een Teruggegeven tussen Herstels; anders hand-back te vroeg of nooit. Test in Taak 4: `TestRepairCountResets`.
4. **Gevonden run is de handoff-run**, niet de push-row (die sluit ook als `handoff`) en niet een run van een andere host. Test in Taak 4: `TestRepairFindsHandoffRunNotPushRow`.
5. **Hervatte run schrijft geen handoff**: geen lus en gewone uitkomstregels; ronde telt niet. Test in Taak 4: `TestRepairRoundWithoutHandoffNotCounted`.

---

### Taak 1: Soort `repair` en headings

**Files:**
- Modify: `internal/protocol/header.go:25-35` (`KindRepair`, in `kinds`), `internal/protocol/status.go:98` (kijk of een `switch` op `Kind` een tak mist)
- Modify: `pack/core/headings.toml`, `.lighthouse/headings.toml`
- Test: `internal/protocol/header_test.go`

**Interfaces:**
- Produces: `protocol.KindRepair Kind = "repair"`; `ParseHeader("🤖 Herstel\nstep: policy\n…")` en `"🤖 Repair\n…"` geven `Kind == KindRepair`; headings `kind_repair`, `prompt_pusher_repair`, `reason_pusher_red_ceiling`.

- [ ] **Step 1: Failing test** `TestParseHeaderRepair` in `header_test.go`: `ParseHeader("🤖 Herstel\nworktree: /w\nbranch: b\nstep: policy")` en dezelfde met `Repair` geven `ok`, `Kind == protocol.KindRepair`, `Fields["step"] == "policy"`, `Session == ""`.
- [ ] **Step 2:** `go test ./internal/protocol -run TestParseHeaderRepair` → FAIL (`KindRepair` onbekend).
- [ ] **Step 3:** Voeg `KindRepair` toe en zet hem in `kinds`. Voeg de drie headings toe in beide toml-bestanden. Teksten: `kind_repair`; `reason_pusher_red_ceiling` nl "workflow-pusher bleef rood na twee reparatierondes", en "workflow pusher stayed red after two repair rounds"; `prompt_pusher_repair` nl "De workflow-pusher vond deze stap rood. Repareer het in de worktree, commit, en schrijf een nieuwe handoff met `next: push-workflows`." plus en-variant.
- [ ] **Step 4:** `go test ./internal/protocol` → PASS (ook de heading-contracttests; doe `grep -rn kind_returned internal/ --include=*_test.go` en pas lijsten aan die elke kind noemen).
- [ ] **Step 5:** Commit `feat(protocol): soort Herstel en headings voor de pusher-reparatie (#505)`.

### Taak 2: De pusher kiest Herstel of Teruggegeven

**Files:**
- Modify: `internal/pusher/outcome.go` (`ask`, `fitQuestion`, nieuwe `renderRepair`), `internal/pusher/floor.go` (naast `Red`)
- Test: `internal/pusher/pusher_test.go`, `internal/pusher/lint_test.go`, `internal/pusher/policy_test.go`

**Interfaces:**
- Produces: `func (r Red) repairable() bool` (true als `r.Step` `StepPolicy` of `StepLint` is en `r.Why` niet begint met `COULD NOT CHECK:`); `func renderRepair(q questionInput, language string) string`.
- Consumes: bestaande `pushFields`, `redLines`, `attachmentLines`, `fitQuestion`.

- [ ] **Step 1: Failing tests** in `pusher_test.go`:
  - `TestRepairableClassifiesOnPrefix` (tabel): `{policy, "x"}` en `{lint, "x"}` → true; `{policy, "COULD NOT CHECK: x"}`, `{lint, "COULD NOT CHECK: x"}`, `{review, "x"}`, `{token, "x"}`, `{push, "x"}` → false; een `RuleParse`-overtredingstekst van policy → true.
  - `TestPolicyRedPostsRepair` en `TestLintRedPostsRepair`: bouw op `TestPolicyRedIsReturnedNotAQuestion` (r. 482); verwacht comment met kop `🤖 Herstel`, `step: policy`/`lint`, velden `worktree`, `branch`, geen `<sub>session`, geen push-commando, diff in `<details>`, en row `Outcome == runs.Handoff`.
  - `TestCouldNotCheckStaysReturned` (drie gevallen: linter niet op `PATH`, linteruitvoer geen JSON, `workflowDiff`-fout; dat laatste geval mag een unittest op `ask` zijn, want pusher.go:225-228 draait pas na een geslaagde Floor op dezelfde clone): comment `🤖 Teruggegeven`, row `returned`.
  - `TestRepairTruncatesLikeQuestion`: diff > `maxComment` → body < `maxComment` en bijlage genoemd.
  Pas bestaande tests aan die rood `policy`/`lint` als Teruggegeven of Vraag verwachten (`TestPolicyRedIsReturnedNotAQuestion`, lint-tests, `TestRedQuestionHasNoTokenScopeAdvice/policy` en `TestBranchTextGivesNoSessionFooter/Vraag` in pusher_test.go ~r. 567 en ~r. 1091) en `TestPublicNames` in `internal/protocol/legacy_twins_test.go:51` (letterlijke `kinds`-lijst).
- [ ] **Step 2:** `go test ./internal/pusher -run 'Repair|CouldNotCheck'` → FAIL.
- [ ] **Step 3:** Implementeer `Red.repairable()` (één functie op het prefix) en `renderRepair`: kop `🤖 <kind_repair>`, `pushFields(q)`, `redLines`, diff opgevouwen als in `renderReturned`, `attachmentLines`; géén push-commando en géén hint. In `ask`: bij `q.hint == hintManual && q.red.repairable()` rendert `fitQuestion` via `renderRepair` en sluit `runs.Release(..., runs.Handoff, "push: "+step, …)`; elk ander `hintManual` blijft ongewijzigd. Werk de doc-comment van `ask` bij.
- [ ] **Step 4:** `go test ./internal/pusher` → PASS.
- [ ] **Step 5:** Commit `feat(pusher): rode policy of lint wordt een Herstel (#505)`.

### Taak 3: `settle` en `lastTurn` kennen de Herstel

**Files:**
- Modify: `internal/dispatch/decide.go` (`settle` ~r. 548, `triggerComment` r. 1267, nieuwe constanten bij r. 1286)
- Test: `internal/dispatch/decide_test.go`

**Interfaces:**
- Produces: constanten `triggerRepair = "repair"`, `maxRepairRuns = 2`; `func pusherRepair(h protocol.Header) bool` (Kind `repair`, `Session == ""`); `triggerComment("repair:<id>")` geeft `(id, true)`.

- [ ] **Step 1: Failing tests**: `TestSettleClosesPushRowOnRepair` (open push-row, Herstel van `lh-bot` na `StartedAt` → één actie `close` met `Outcome == runs.Handoff`, naar model van `TestPushRowClosesOnThePushersReturned`); `TestSettleIgnoresRepairOfOtherAuthor` (andere auteur → geen close); `TestTriggerCommentRepairPrefix`; `TestPushRowClosesOnThePushersReturned` (decide_test.go:525) dekt in zijn tweede helft al `finish_return:reason_pusher_red`; laat hem groen. Voeg `TestRepairWaitsWhilePushRowOpen` toe in Taak 4 (zie daar).
- [ ] **Step 2:** `go test ./internal/dispatch -run 'Settle.*Repair|TriggerCommentRepair'` → FAIL.
- [ ] **Step 3:** In `settle` een tak `protocol.KindRepair`: `repair = repair || isPush(row) && pusherRepair(p.Header) && p.Comment.Author == in…TokenLogin` (zelfde bron als `finishReturn`, `repo.Snapshot.TokenLogin`); `case repair:` sluit `runs.Handoff`, plaatsing naast `pushReturned`. Voeg `triggerRepair+":"` toe aan `triggerComment`. `lastTurn` hoeft niet te veranderen (een Herstel is al een beurt); de test in Taak 4 bewijst dat.
- [ ] **Step 4:** `go test ./internal/dispatch` → PASS.
- [ ] **Step 5:** Commit `feat(dispatch): settle sluit de push-row op een Herstel (#505)`.

### Taak 4: Regel `repair`

**Files:**
- Modify: `internal/dispatch/decide.go` (`next()` r. 1313, nieuwe `repair`, `repairRounds`, `handoffRunBefore`)
- Test: `internal/dispatch/decide_test.go`, `internal/dispatch/testdata/decide/repair-*.json` (golden, zoals `answer-resume.json`)

**Interfaces:**
- Consumes: `lastTurn`, `pusherRepair`, `c.spent`, `c.planned`, `c.returned`, `pushCommand`, `isPush`, `lastRunOf`, `protocol.Heading`.
- Produces: `func (c issueCtx) repair() (planned, bool)`; `func (c issueCtx) repairRounds() int` (punt 7 van de spec: push-rows van nieuw naar oud, sla `start_failed` en open row over, tel `handoff` zonder `PushedSHA` (de gesloten row van de huidige Herstel telt dus mee: de pusher sloot hem als `handoff`), stop bij andere uitkomst of bij een row van vóór de laatste Teruggegeven van de token-login); `func (c issueCtx) handoffRunBefore(t time.Time) (runs.Row, bool)` (laatste push-row vóór `t` → `push:<id>` → Handoff-comment → laatste run van dit issue met `Node == ""`, `Outcome == runs.Handoff`, `!isPush`, gestart vóór die comment).

- [ ] **Step 1: Failing tests** (decide_test, helpers als `pushRow`, `pusherReturned`):
  - `TestRepairResumesHandoffSession`: Herstel als laatste beurt, sessie gevonden → één actie `KindResume`, `Trigger == "repair"`, `TriggerKey == "repair:<id>"`, `SessionID`, `Command`, `Branch` van de handoff-run, `Attempt == 1`, prompt bevat `step`, de reden en de comment-URL, en niet de diff.
  - `TestRepairKeySpentDoesNothing`.
  - `TestRepairWaitsWhilePushRowOpen`: in de tick waarin `settle` de push-row op de Herstel sluit staat die row nog open in `History`; `repair` doet dan niets (alleen de `close`, geen resume en geen return). De hervatting volgt in de volgende tick, als de row gesloten is.
  - `TestRepairCeiling`: fixtures met 1 en 2 gesloten Herstel-push-rows (de huidige meegeteld) → hervatting; 3 → `return:reason_pusher_red_ceiling:` met `pushCommand(worktree)` als ReasonArg (`c.returned` geeft `KindReturn`, niet `KindFinishReturn`).
  - `TestRepairCountResets` (tabel): gepushte push-row, `question`-row, Teruggegeven van de token-login tussen Herstels → telling opnieuw, dus hervatting.
  - `TestRepairIgnoresOtherAuthor`.
  - `TestRepairFindsHandoffRunNotPushRow`: naast de push-row (ook `handoff`) en een run met `Node != ""` wint de handoff-run van deze host.
  - `TestRepairWithoutRunOrSessionReturns`: geen handoff-run, of zonder `SessionID` → `return:reason_pusher_red:` met `pushCommand(worktree)` als ReasonArg.
  - `TestRepairRoundWithoutHandoffNotCounted`: hervatte run (`repair:`-row, `answer`-trigger) sloot zonder nieuwe push-row → niet geteld door `repairRounds`.
  - Golden `repair-resume.json` en `repair-ceiling.json` (kopieer de vorm van `answer-resume.json`; het harnas heeft geen update-commando, schrijf `want` met de hand, `DisallowUnknownFields` geldt).
- [ ] **Step 2:** `go test ./internal/dispatch -run Repair` → FAIL.
- [ ] **Step 3:** Implementeer de regel in de volgorde van de spec (punten 1–4): eerst niets doen zolang `hasOpenRun(c.repo.History, c.it.Issue)` waar is (zoals `finishReturn`, decide.go:1371; `settle` sluit de push-row in dezelfde tick en `next()` ziet hem dan nog open); dan `repairRounds()` (telt de gesloten row van de huidige Herstel al mee, dus geen `+1`) tegen `maxRepairRuns` (`repairRounds() > maxRepairRuns` → `c.returned("reason_pusher_red_ceiling", pushCommand(worktree))`), run zoeken, geen run/sessie → `c.returned("reason_pusher_red", pushCommand(worktree))`, anders `KindResume`/`modeResume` via `c.planned(a, parseTime(com.CreatedAt))`. `worktree` komt uit `Fields["worktree"]` van de Herstel. Prompt: `protocol.Heading("prompt_pusher_repair", lang)` + stap, reden (uit de body van de Herstel, zoals `redLines` hem schrijft: lees `Fields["step"]` en de fenced reden) + `com.URL`. Zet `c.repair` tussen `c.answer` en `c.closeCheck`. Werk de doc-comment van `next` bij.
- [ ] **Step 4:** `go test ./internal/dispatch` → PASS (alle golden fixtures groen).
- [ ] **Step 5:** Commit `feat(dispatch): regel repair hervat de run bij een Herstel (#505)`.

### Taak 5: Apply claimt trigger `repair` als `answer`

**Files:**
- Modify: `internal/dispatch/apply.go:424` (de `runs.Take` van een start/resume)
- Test: `internal/dispatch/apply_test.go`

**Interfaces:**
- Consumes: `Action.Trigger == triggerRepair`; produces een `runs`-row met `Trigger == "answer"` en `TriggerKey == "repair:<id>"`.

- [ ] **Step 1: Failing test** `TestApplyClaimsRepairResumeAsAnswer`: echte database (zoals de bestaande apply-tests met `runs.Take`), `Action{Kind: KindResume, Trigger: "repair", TriggerKey: "repair:77", …}` → `Take` slaagt (geen CHECK-fout) en `History` toont `Trigger == "answer"`, sleutel `repair:77`; tweede apply met dezelfde sleutel → `Apply` geeft nil (de applier slokt `ErrTriggerSpent` in, apply.go:428-433), er komt geen tweede run en er staat een regel "not claimed" in de log.
- [ ] **Step 2:** `go test ./internal/dispatch -run ClaimsRepair` → FAIL (CHECK constraint).
- [ ] **Step 3:** Voor `runs.Take` in `apply.go`: `trigger := a.Trigger; if trigger == triggerRepair { trigger = triggerAnswer }`, zoals `push` (r. 530) al doet; gebruik `trigger` in de `Claim`. Eerste regel boven de code noemt dat `runs` geen trigger `repair` kent (`0008_runs_schedule.sql`).
- [ ] **Step 4:** `go test ./internal/dispatch ./internal/runs` → PASS.
- [ ] **Step 5:** Commit `fix(dispatch): trigger repair wordt als answer geclaimd (#505)`.

### Taak 6: Loop-kaart, docs, changelog, dogfood

**Files:**
- Modify: `docs/site/data/loop.json` (chip `push-workflows` r. 626–640, route `workflow-push` r. 934), `docs/tick.md`
- Create: `changelog.d/issue-505-pusher-herstel.md`
- Test: `docs/site/test/loop.test.mjs`

- [ ] **Step 1:** Voeg aan chip `push-workflows` een `how`-regel toe: een rode policy of lint wordt een Herstel, de tick hervat de run, ten hoogste twee rondes, daarna naar de gebruiker. Zet in de `note` van route `workflow-push` dat een rode policy of lint eerst naar het werk gaat. Geen nieuwe handoff-lijn tenzij de test erom vraagt (dan in `PUSHER` of `DISPATCHED`).
- [ ] **Step 2:** `docs/tick.md`: de uitkomst Herstel naast Vraag en Teruggegeven, en `repair` in de volgorde van `next`. Fragment `changelog.d/issue-505-pusher-herstel.md` met `type: changed` (vorm van de bestaande fragmenten).
- [ ] **Step 3:** `node --test docs/site/test/` → PASS.
- [ ] **Step 4:** `go run ./cmd/lighthouse init --upgrade` en daarna nogmaals → `No changes.`; commit de geïnstalleerde kopie mee.
- [ ] **Step 5:** `lighthouse imports check` en `[test_gate] command` uit `lighthouse.toml` groen. Commit `docs: reparatieronde van de pusher op de loop-kaart (#505)`.
