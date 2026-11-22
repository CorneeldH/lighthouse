# Nacontrole met vervolgstap, en spec ja plan nee bij een blokkade — Implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Een nacontrole (B7) met alleen wachtende criteria eindigt in een handoff terug naar zichzelf, een nacontrole zonder vervolgstap krijgt een eerlijke reden, en een geblokkeerd issue mag een spec maar geen plan.

**Architecture:** Twee wijzigingen in `internal/dispatch` (een reden in regel 14, een `SpecPhase`-predicaat naast `Executing` voor de parkeercheck van het handoff-pad), proza in zes skillbestanden (elk in de varianten `claude` en `agents`), en fixtures en proza-contracten die het bewijzen. Geen nieuwe controllerfunctie: het wachten gebruikt de bestaande parkeer- en `unpark`-regels.

**Tech Stack:** Go (`internal/dispatch`, `internal/packcontract`), JSON-fixtures, Markdown-skills in `pack/core/`.

**Spec:** [`../specs/2026-11-22-issue-394-nacontrole-vervolgstap-design.md`](../specs/2026-11-22-issue-394-nacontrole-vervolgstap-design.md)

## Global Constraints

- Artefacten (spec, plan, commitberichten, proza in `docs/`) zijn Nederlands; skills, hookteksten en code-commentaar Engels (`AGENTS.md`).
- Eén PR, `Refs #394`. Atomaire commits als Conventional Commits; B7 en reden los van spec-ja-plan-nee.
- Dogfood: elke wijziging onder `pack/` eindigt met `go run ./cmd/lighthouse init --upgrade` en een commit van het geïnstalleerde exemplaar; CI eist `No changes.`
- Voor elke commit: `[test_gate] command` uit `lighthouse.toml` en `lighthouse imports check`.
- `Executing` blijft bestaan en ongewijzigd (`repeatsHandoff` gebruikt het); `SpecPhase` komt ernaast.
- Parkeren gaat naar `To do` met `blocked_issue` (niet `On hold`).
- `labels.blocked_external` verandert niet in de controller; de zin `` `labels.blocked_external` always stops`` blijft in poort 0.
- Reden-sleutel exact `reason_closecheck_open`, nl `nacontrole: criteria open`, en `after-merge check: criteria open`, label `needs_hands`.
- Een wachtend criterium telt alleen bij een voorganger die `OPEN` is, zonder gemergde PR, in deze repo.
- Alle proza-wijzigingen staan in beide varianten (`claude` en `agents`); tests lopen over `variants(t, …)`.
- Bestaande prozatests die letterlijke B7- of poort-0-zinnen pinnen: aanpassen, niet verwijderen.

## Review Focus

1. Voorganger `OPEN` mét gemergde PR: B7 parkeert niet (de handoff zou meteen weer een nacontrole starten) en valt onder de vraag. Test: contract in Task 4 + fixture `handoff-blocker-merged-pr-starts` blijft groen.
2. `lighthouse forge issue block` faalt (exit ≠ 0): B7 valt terug op de vraag, post geen handoff. Test: contract in Task 4.
3. Voorganger in een andere repo: geen relatie mogelijk, het criterium valt onder de vraag. Test: contract in Task 4.
4. Herstart van de nacontrole na `unpark` (trigger `handoff`, niet `closecheck`) zonder vervolgstap: ook `reason_closecheck_open`. Test: fixture in Task 2.
5. Twee nacontroles met identieke handoff-tekst: tweede geeft `reason_repeat_handoff`, geen lus. Test: fixture in Task 3 (andere tekst → start; identieke tekst → `handoff-repeat-returns` dekt het bestaande gedrag).

---

### Task 1: Controller parkeert writing-plans en review-plan, niet de specfase

**Files:**
- Modify: `internal/dispatch/judge.go` (naast `Executing`, regel ~206)
- Modify: `internal/dispatch/decide.go:1765` (parkeercheck in `handoff()`)
- Test: `internal/dispatch/judge_test.go`, `internal/dispatch/testdata/decide/`

**Interfaces:**
- Produces: `func SpecPhase(command string) bool` — `true` alleen voor `/lighthouse-review-spec` (na `rewrite`, dus ook `/paia-review-spec` en `/lighthouse-spec-adversarial-review`); lege command → `false`.

- [ ] **Step 1: Schrijf de falende tests.** `TestSpecPhase` in `judge_test.go`, tabel naar het model van `TestExecuting`: `/lighthouse-review-spec /a.md`, `/lighthouse-spec-adversarial-review /a.md` → `true`; `/lighthouse-review-plan /a.md`, `/superpowers:writing-plans /a.md`, `/lighthouse-start-issue 7`, `/superpowers:subagent-driven-development /a.md`, `""` → `false`. Twee fixtures, beide uit `handoff-blocker-executing-park.json` (issue 7, voorganger `o/r#5` open): `handoff-blocker-writing-plans-park.json` (`next: /superpowers:writing-plans <spec-pad>`) en `handoff-blocker-plan-review-park.json` (`next: /lighthouse-review-plan <plan-pad>`); `want`: één `park` met `blockers: ["o/r#5"]`. `handoff-blocker-spec-review-starts.json` bestaat al en moet groen blijven.
- [ ] **Step 2: Draai ze rood.** `go test ./internal/dispatch/ -run 'TestSpecPhase|TestDecideFixtures' -v` → `TestSpecPhase` compileert niet; de twee fixtures geven een start in plaats van een park.
- [ ] **Step 3: Implementeer `SpecPhase(command string) bool`** in `judge.go` (zelfde `strings.Fields` + `rewrite` als `Executing`), en wijzig in `handoff()` de parkeerconditie `Executing(j.Command) && c.active()` naar `!SpecPhase(j.Command) && c.active()`. De `repeatsHandoff`-regel erna blijft `Executing(j.Command)`. Commentaar bij `SpecPhase`: alleen de specfase loopt vrij bij een blokkade; plan en uitvoering wachten op de merge van de voorganger.
- [ ] **Step 4: Draai groen.** `go test ./internal/dispatch/` → PASS, ook `handoff-blocker-executing-park`, `handoff-blocker-merged-pr-starts` en `handoff-blocker-closed-starts`.
- [ ] **Step 5: Commit.** `git add internal/dispatch && git commit -m "feat(dispatch): handoff naar writing-plans of review-plan parkeert bij een actieve voorganger (#394)"`

### Task 2: Reden `reason_closecheck_open` bij een nacontrole zonder vervolgstap

**Files:**
- Modify: `pack/core/headings.toml` (naast `[reason_no_handoff]`, regel ~129)
- Modify: `internal/dispatch/decide.go:1008` (`noHandoff`)
- Modify: `internal/dispatch/decide_test.go` (`TestReturnLabelKeyTable`, regel ~715)
- Test: `internal/dispatch/testdata/decide/`

**Interfaces:**
- Produces: heading-sleutel `reason_closecheck_open`; `noHandoff` geeft `c.returned("reason_closecheck_open", "")` wanneer `rewrite(strings.Fields(c.row.Command)[0]) == cmdClose`, anders `reason_no_handoff`. Rule 14a (`postFailed`) blijft voor.

- [ ] **Step 1: Schrijf de falende tests.** Rij `"reason_closecheck_open": "needs_hands"` in `TestReturnLabelKeyTable`. Twee fixtures uit `done-no-handoff.json`, rij met `Command: "/lighthouse-close-issue 7"`, `Mode`/`Branch` van een closecheck (`closecheck/issue-7-pr-<n>`), geen handoff of vraag, geen PR die de run sluit: `closecheck-done-no-handoff-returns.json` (`Trigger: "closecheck"`, `TriggerKey: "closecheck:<pr>"`) en `closecheck-restart-done-no-handoff-returns.json` (`Trigger: "handoff"`, `TriggerKey: "handoff:<comment-id>"`). `want` beide: `returned` met `reason_closecheck_open`. Ook één fixture `closecheck-question-closes-as-question.json`: dezelfde rij mét een 🤖 Vraag als laatste comment → run sluit als vraag (bestaand gedrag vastleggen; copy het uitvoer-formaat van een bestaande `question`-fixture).
- [ ] **Step 2: Draai rood.** `go test ./internal/dispatch/ -run 'TestReturnLabelKeyTable|TestDecideFixtures|TestDecideKeysAreHeadings'` → tabel rood (sleutel onbekend), twee fixtures geven `reason_no_handoff`.
- [ ] **Step 3: Implementeer.** Sleutel in `pack/core/headings.toml` met `nl = "nacontrole: criteria open"`, `en = "after-merge check: criteria open"`. Pas `noHandoff` aan zoals bij Interfaces. Doe daarna `go run ./cmd/lighthouse init --upgrade` zodat `.lighthouse/headings.toml` de sleutel krijgt.
- [ ] **Step 4: Draai groen.** `go test ./internal/dispatch/ ./internal/headings/... ` → PASS. Controleer met `git diff --stat` dat alleen `pack/core/headings.toml` en `.lighthouse/headings.toml` van de pack zijn veranderd.
- [ ] **Step 5: Commit.** `git commit -m "feat(dispatch): eigen reden voor een nacontrole zonder handoff of vraag (#394)"`

### Task 3: Fixtures voor het wachten op een voorganger (bewijs, geen nieuwe code)

**Files:**
- Test: `internal/dispatch/testdata/decide/` (vijf fixtures)

**Interfaces:**
- Consumes: bestaand gedrag van regel 13c, `handoff()`/`unpark` en `repeatsHandoff`; `Executing("/lighthouse-close-issue 7")` is `true`.

- [ ] **Step 1: Schrijf de fixtures** (modellen: `handoff-blocker-executing-park.json`, `unpark.json`, `handoff-repeat-returns.json`, `settle-handoff-end-waits.json`):
  - `closecheck-wait-handoff-closes-as-handoff.json`: Done-run van de nacontrole met laatste comment een Handoff `next: /lighthouse-close-issue 7`, voorganger actief → run sluit als `handoff` (13c), niet als `reason_closecheck_open`; in dezelfde tick de actie `park` met `blockers: ["o/r#5"]`.
  - `closecheck-wait-park-while-blocker-open.json`: de handoff staat er al, run al gesloten, voorganger `OPEN` zonder gemergde PR → `park`.
  - `closecheck-wait-unpark-blocker-closed.json`: geparkeerd issue (eigen 🤖 Geparkeerd), voorganger `CLOSED` → `unpark`; een tweede fixture `closecheck-wait-start-after-unpark.json` met de status al terug op Controller → start `handoff:<comment-id>` met command `/lighthouse-close-issue 7` en branch `closecheck/issue-7-pr-<n>`.
  - `closecheck-wait-second-handoff-other-text-starts.json`: twee Handoffs, de eerste al gebruikt, tweede met andere State/Context (andere voorganger) → start, geen `reason_repeat_handoff`.
- [ ] **Step 2: Draai.** `go test ./internal/dispatch/ -run TestDecideFixtures -v`. Verwacht groen; een rode fixture is een echt gat (bijvoorbeeld 13c sluit als `reason_closecheck_open`, of de park gebeurt niet in dezelfde tick): herstel het in `decide.go` met het kleinste gedrag dat de fixture groen maakt, en noem het in de commit.
- [ ] **Step 3: Commit.** `git add internal/dispatch/testdata && git commit -m "test(dispatch): nacontrole die op een voorganger wacht parkeert en komt terug (#394)"`

### Task 4: B7 in close-issue: drie soorten open criteria

**Files:**
- Modify: `pack/core/skills/close-issue/claude/SKILL.md`, `pack/core/skills/close-issue/agents/SKILL.md` (`### B7.`, regel ~237)
- Test: `internal/packcontract/closeissue_test.go`

**Interfaces:**
- Produces: in B7 (beide varianten) de tabel *wacht op een issue / codewerk of docs / handwerk of een gebeurtenis*; de uitkomstregel *alle open criteria wachten op een issue in deze repo*.

- [ ] **Step 1: Schrijf de falende test** `TestCloseIssueB7ClassifiesOpenCriteria` (met `Classification:`-regel erboven, zoals de buren), over `closeIssueSection(t, body, "### B7.")` en `flat(...)`: `wantAll` op `waits on an issue`, `code or docs`, `manual work or an event`, `lighthouse forge issue block <nr> --by <predecessor>`, `lighthouse forge pr list`; `wantOrder` toont dat `issue block` vóór `/lighthouse-write-handoff` staat; `wantAll` op `next: /lighthouse-close-issue <nr>`, `falls back to the question`, `only for a predecessor that is `OPEN` and has no merged PR`, `in another repository`; en `git fetch origin`, `git worktree add --detach`, `origin/<default branch>` (de meting loopt op een wegwerp-worktree, niet in de cwd).
- [ ] **Step 2: Draai rood.** `go test ./internal/packcontract/ -run TestCloseIssueB7 -v` → FAIL op de eerste ontbrekende zin, in beide varianten.
- [ ] **Step 3: Schrijf het proza** in beide varianten, Engels, in B7 naast de bestaande rij *otherwise* en *Open criteria: one question*: de drie-soortentabel uit spec §1; de uitkomst (alle wachtend → per voorganger `issue block`, dan handoff `next: /lighthouse-close-issue <nr>` zonder vraag; de handoff noemt zijn voorgangers in State of Context; het after-merge-comment noemt per criterium de voorganger; anders de vraag met het wachtende criterium in `--what`); de terugval bij een mislukte `issue block`; de maat voor *wacht op een issue* (`OPEN`, geen gemergde PR via `lighthouse forge pr list`, in deze repo); en de meting: eerst `git fetch origin`, dan B2 (zetten 2 en 3) en elk criteriumcommando in `git worktree add --detach <scratch>/… origin/<default branch>`, zoals B2a en C2. Pas de zin *Step 0, B1–B4 and B2a hold unchanged* aan zodat die de nieuwe meetmaat niet tegenspreekt (de test `TestCloseIssueB7IncludesB2a` blijft die zin of zijn opvolger pinnen: werk die test bij, verwijder hem niet).
- [ ] **Step 4: Draai groen.** `go test ./internal/packcontract/` → PASS.
- [ ] **Step 5: Commit** (na `go run ./cmd/lighthouse init --upgrade` en toevoegen van het geïnstalleerde exemplaar): `git commit -m "feat(close-issue): B7 wacht op een voorganger via een handoff naar de nacontrole (#394)"`

### Task 5: Spec ja, plan nee in route-issue, start-issue, review-spec en write-issue

**Files:**
- Modify: `pack/core/skills/route-issue/SKILL.md` (Gate 0, regels ~93–132, en de tabelrijen ~206–221)
- Modify: `pack/core/skills/start-issue/{claude,agents}/SKILL.md` (stap 3 *board-start*, stap 4 *Blocked in a controller run?*, scout-sectie)
- Modify: `pack/core/skills/review-spec/{claude,agents}/SKILL.md` (afsluiting regel ~450)
- Modify: `pack/core/skills/write-issue/{claude,agents}/SKILL.md` (stapelvolgorde, regel ~208)
- Test: `internal/packcontract/issueflow_test.go` (en een test voor review-spec in `internal/packcontract/review_test.go`)

**Interfaces:**
- Produces: poort 0 stopt niet meer op een blokkade (behalve *unknown* en `labels.blocked_external`); start-issue voert bij een blokkade alleen 4c uit.

- [ ] **Step 1: Schrijf de falende tests:**
  - `TestRouteIssueGate0RoutesOnWithABlocker` (vervangt in rol `TestRouteIssueGate0StopsWithoutBlockedLabel`, werk die bij in plaats van te verwijderen): gate 0 bevat `— blocker #<x> open`, `— blocked: <label>`, `**unknown**` met *stays a stop*, `wait for: <label>` (zonder issuenummer), en nog steeds `` `labels.blocked_external` always stops``. `TestRouteIssueGate0RoutesOnInAControllerRun` en `TestRouteIssueInvariantsKnowTheControllerRun` aanpassen op de nieuwe tekst (de regel geldt nu voor beide modi; de rij van `labels.blocked_external` noemt *controller run* nog steeds niet).
  - `TestStartIssueBlockedOnlyRunsGate4c`: stap 4 bevat de tabel *4c | execute: brainstorming, spec* en *4b, 4a, 1, 2 | do not execute*, `wait for #<x>`, `next: /lighthouse-start-issue <nr>`, de uitzondering `` `stack:` or `blocker:` ``, en de instructie aan brainstorming om na de goedgekeurde spec te eindigen met `wait for #<x>` zonder `writing-plans`; de scout-sectie bevat `heading:scout_skipped — blocker #<x> open, the plan waits for the merge.`; stap 3 slaat `board-start` over bij een blokkadelabel én bij *label missing*. Pas `TestStartIssueHoldsABlockedExecution` aan op de nieuwe regel *Gate `1`, `2` or `4a`* (nu ook `4b`).
  - `TestReviewSpecInteractiveEndWaitsOnAPredecessor` (in `review_test.go`): de interactieve afsluiting bevat `wait for #<x>` en nog steeds `next: /superpowers:writing-plans`; de sectie *Controller run* wijzigt niet (`wantAll` op `next: /superpowers:writing-plans <absolute spec path>`).
  - `TestWriteIssueStackingOrderSpecYesPlanNo` (in `writeissue_test.go`): bevat `spec yes, plan and execution wait for a merged PR of the predecessor`.
- [ ] **Step 2: Draai rood.** `go test ./internal/packcontract/ -run 'RouteIssue|StartIssue|ReviewSpec|WriteIssue' -v` → de nieuwe tests FAIL, de bijgewerkte oude ook.
- [ ] **Step 3: Schrijf het proza** volgens spec §4, Engels, in alle varianten. Route-issue: de regel *Controller run (only if present)* in Gate 0 wordt de regel voor beide modi (zonder `(only if present)`-kader), nog steeds zonder `/lighthouse-write-handoff` in poort 0; *lapsed*, *unlinked* en *label missing* routeren door met melding en herstelcommando achter de regel en tellen als blokkade; zonder issuenummer zegt de melding `wait for: <label>`. Start-issue: stap 3 slaat `board-start` over bij een blokkadelabel of een open `blocked_by` zonder label; stap 4 krijgt de tabel en de brainstorming-opdracht; `Report the choice in one line` blijft. Review-spec: de interactieve afsluiting schrijft bij een actieve voorganger `wait for #<x>` in plaats van de writing-plans-handoff. Write-issue: de stapelvolgorde-zin.
- [ ] **Step 4: Draai groen.** `go test ./internal/packcontract/` → PASS. Controleer ook `lighthouse imports check`.
- [ ] **Step 5: Commit** (na `init --upgrade` en het geïnstalleerde exemplaar): `git commit -m "feat(pack): een geblokkeerd issue mag een spec, geen plan (#394)"`

### Task 6: Documentatie, changelog en eindcontrole

**Files:**
- Modify: `docs/tick.md` (regel 14 ~317, sectie *Blokkades*)
- Create: `changelog.d/issue-394-close-issue-b7-eindigt-bij-open-criteria.md`

- [ ] **Step 1: Pas `docs/tick.md` aan** (Nederlands): regel 14 krijgt de reden *nacontrole: criteria open* voor een run met command `/lighthouse-close-issue` (trigger `closecheck` of `handoff`), en een zin dat een nacontrole die op een issue wacht eindigt in een handoff naar zichzelf, geparkeerd wordt en na het sluiten van de voorganger opnieuw start; *Blokkades*: "een issue in de spec-fase blijft staan" in plaats van "spec-, plan- of reviewfase", met de plan-handoff als parkeerreden.
- [ ] **Step 2: Schrijf het fragment** (Engels, `type: changed`, twee zinnen): de nacontrole wacht op een voorganger via een handoff of vraagt, krijgt een eigen reden; tweede wijziging: een geblokkeerd issue mag een spec maar geen plan, interactief en in de controller.
- [ ] **Step 3: Draai alles.** `go run ./cmd/lighthouse init --upgrade` → `No changes.` na de commit; `go test ./...`; `lighthouse imports check`. Alle drie groen.
- [ ] **Step 4: Commit.** `git commit -m "docs(tick): nacontrole met vervolgstap en spec ja plan nee (#394)"`

---

## Self-review

- **Spec coverage:** §1 → Task 4; §2 → Task 3 (+ de meetmaat in Task 4); §3 → Task 2; §4 route/start/review-spec/write-issue → Task 5, controller → Task 1; §5 → Task 6; Tests → Tasks 1–5. Geen gat.
- **Types:** `SpecPhase(command string) bool` is in Task 1 gedefinieerd en alleen daar gebruikt; `reason_closecheck_open` is in Task 2 gedefinieerd, Task 6 noemt de tekst.
- **Proportie:** het plan is korter dan de spec-code die het beschrijft; bodies staan er niet, alleen signaturen, namen en de exacte zinnen die de tests pinnen.
