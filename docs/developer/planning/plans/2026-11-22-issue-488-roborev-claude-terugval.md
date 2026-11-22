# Roborev-terugval op Claude bij een vol Codex-budget — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bij Codex-budget `fallback` reviewt `roborev` met `claude-code` in plaats van over te slaan, zodat de workflow-pusher een `done`-record krijgt.

**Architecture:** De gate kiest de agent: `codex` bij Codex-budget `codex`, `claude-code` bij `fallback` als `claude` op PATH staat en het Claude-alarm van `lighthouse usage` uit is; anders een skip. De gate-uitvoer `run` draagt `agent`, en V2b van `write-pr` geeft die door aan `roborev ci review --agent <agent>`. `classify` leest de agent uit de logregel die roborev in `<raw>` schrijft. `record --done` zet hem in het record, en `body-line` noemt hem in de marker.

**Tech Stack:** Go (`internal/roborev`, `internal/cli`, `internal/pusher`, `internal/packcontract`), skilltekst in `pack/core/skills/`.

**Spec:** [issue #488](https://github.com/CorneeldH/lighthouse/issues/488). Er is geen apart specbestand: de acceptatiecriteria van het issue zijn de spec.

## Global Constraints

- Gemeten met roborev v0.69.0 en nep-agents op PATH (scratchpad-probe, 2026-11-22):
  - `roborev ci review --agent claude-code` laat het repo-`model` (`gpt-5.6-sol`) vallen. `claude` krijgt geen `--model` en draait met zijn eigen standaardmodel.
  - `review_model`, `review_model_<level>` en `review_agent_<level>` hebben geen invloed op `ci review`.
  - AC3 ("de Claude-review draait niet met het Codex-model") geldt dus met de aanroep `--agent claude-code`, zonder wijziging in `.roborev.toml`.
  - Een eigen Claude-model vastzetten kan in v0.69.0 niet: `ci review` heeft geen `--model` en geen per-agent model. Zet dat niet in de scope.
- roborev schrijft op stderr `… ci review: ref=<range> agents=[<agent>] types=[…] …`. V2b leidt stderr naar `<raw>` om (`2>&1`), dus die regel staat in `<raw>`.
- Skipredenen, letterlijk:
  - fallback zonder `claude` op PATH: `Codex-budget: fallback - <reden>; geen claude op PATH`
  - fallback met Claude-alarm: `Codex-budget: fallback - <reden>; Claude-alarm: <kind> <pct>%` (`<kind>` is `venster` of `week`)
- Marker in body-line met agent: `<!-- roborev-lokaal: <sha> agent: <agent> -->`. Zonder agent in het record blijft hij `<!-- roborev-lokaal: <sha> -->`.
- Skills in het Engels, artefacten (commits, changelog, plan) in het Nederlands. Bewerk alleen `pack/core/skills/`. De kopieën in `.claude/` en `.agents/` komen uit `go run ./cmd/lighthouse init --upgrade`.
- Vóór elke commit: `lighthouse imports check` en de targeted test van de taak. `commit-test-gate` draait daarna `[test_gate] command`.

## Review Focus

1. **Een onleesbaar of niet-meetbaar Codex-budget** (`not_measurable` met code 0, exit 1, JSON zonder `decision`, of code 6 met decision `codex`) blijft een skip met de bestaande reden. Nooit een stille terugval op Claude. Test in Taak 1: `TestReviewAgentKeepsUnreadableAsSkip`.
2. **Herstart na een Claude-review.** Gate geeft `classify` en dus geen `agent`. Het record moet de agent toch dragen, uit `<raw>`. Test in Taak 2: `TestRecordTakesAgentFromRaw`.
3. **Raw zonder logregel** (een CI-commentaar via fix-pr §6, of een oude raw): geen `agent`-sleutel en een marker in het oude formaat, byte voor byte. Test in Taak 2: `TestBodyLineWithoutAgentKeepsOldMarker`.
4. **`codex` ontbreekt op PATH bij fallback.** Dat blijft `geen roborev of codex op PATH`: de terugval vervangt geen ontbrekende Codex-installatie. Test in Taak 1: `TestGateFallbackWithoutCodexStaysSkip`.
5. **Een skip-record van vóór deze wijziging** (`push-<head>.json` met `Codex-budget: fallback - …`) blijft een skip: de gate beslist niet opnieuw. Dat is het bestaande herstartgedrag. Bestaande fixture 11 dekt het; niets nieuws.

---

### Taak 1: Gate kiest de review-agent

**Files:**
- Modify: `internal/roborev/gate.go`. `budgetSkip` (r. 45-62) wordt `reviewAgent`. Daarbij komen `ClaudeAlarm` en `DefaultClaudeAlarm`, de `switch` in `gate` (r. 171-199) en de signaturen van `Gate`/`GatePush`.
- Modify: `internal/cli/roborev.go` (`newRoborevGateCommand`, `newRoborevGate`) en `internal/cli/root.go:80`.
- Modify, alleen de aanroepen: `internal/roborev/gate_test.go`, `internal/cli/roborev_fixture_test.go` (r. 355, 510, 594, 612, 634, 660, 687) en `internal/pusher/roundtrip_test.go` (r. 54, 83).
- Modify: `tests/pack/fixtures/roborev_gate/07-gate-skips-on-the-budget-1.json` en `11-gate-records-its-skip-and-repeats-it.json`. De fixture-PATH heeft geen `claude`, dus de verwachte reden wordt `Codex-budget: fallback - 5h 93%; geen claude op PATH`. Fixture 15 verwacht alleen `verdict: skip` en blijft zoals hij is.
- Test: `internal/roborev/agent_test.go` (nieuw)

**Interfaces:**
- Produces:
  - `type ClaudeAlarm func() (usage.Alarm, bool)`
  - `func DefaultClaudeAlarm() ClaudeAlarm`. Leest `usage.DefaultPaths()` en daarna `paths.CurrentAlarm(time.Now(), park.ControllerRun(os.Getenv))`. Faalt `DefaultPaths`, dan geldt dat als geen alarm, net als in `internal/hooks/usagegate.go:42-43`.
  - `func reviewAgent(budget Budget, alarm ClaudeAlarm) (agent, reason string)`. Precies één van de twee is niet leeg.
  - `func Gate(root string, budget Budget, alarm ClaudeAlarm) (*orderedjson.Object, error)` en `GatePush` met dezelfde parameters.
  - Gate-uitvoer bij `run`: de sleutels `verdict`, `range`, `lines`, `agent`, `state`, `raw`, in die volgorde.
  - `newRoborevGateCommand(budget roborev.Budget, alarm roborev.ClaudeAlarm)`.

- [ ] **Stap 1: Schrijf de falende tests** in `internal/roborev/agent_test.go`. Gebruik `newPushGateRepo` en `codexBudget` uit `gate_test.go`. Een helper `fallbackBudget()` geeft `6, {"decision":"fallback","reason":"5h 93%"}`. `noAlarm` geeft `usage.Alarm{}, false`. `withClaude(r)` zet een nep-`claude` in de bin-map van de repo. `strictPath(t, bin)` zet PATH op alleen `bin` plus de map van `git`. Staat `claude` in die map, dan `t.Skip`.
  - `TestReviewAgentCodex`: `reviewAgent(codexBudget, noAlarm)` geeft `("codex", "")`.
  - `TestReviewAgentFallbackIsClaude`: met `withClaude` geeft `reviewAgent(fallbackBudget, noAlarm)` `("claude-code", "")`.
  - `TestReviewAgentFallbackWithoutClaude`: met `strictPath` geeft het `("", "Codex-budget: fallback - 5h 93%; geen claude op PATH")`.
  - `TestReviewAgentFallbackWithClaudeAlarm`: met `withClaude` en een alarm `usage.Alarm{Kind: "venster", Pct: 92}` geeft het `("", "Codex-budget: fallback - 5h 93%; Claude-alarm: venster 92%")`.
  - `TestReviewAgentKeepsUnreadableAsSkip`, tabelgedreven. Elke rij geeft agent `""` en precies de reden van nu:
    - `0, {"decision":"not_measurable","reason":"x"}` geeft `Codex-budget: not_measurable - x`
    - `1, ""` geeft `Codex-budget: onleesbaar - exit 1`
    - `6, {"decision":"codex","reason":"x"}` geeft `Codex-budget: codex - x`
    - `6, {"reason":"x"}` geeft `Codex-budget: onleesbaar - exit 6`
    - Een alarm dat raakt, moet `t.Fatal` geven: bij deze rijen leest de gate het alarm niet.
  - `TestGatePushFallbackRunsClaude`: `GatePush(r.root, fallbackBudget, noAlarm)` met `withClaude` geeft `verdict` `run` en `agent` `claude-code`, en schrijft geen state-bestand.
  - `TestGatePushCodexRunCarriesAgent`: `GatePush(r.root, codexBudget, noAlarm)` geeft `agent` `codex`.
  - `TestGateFallbackWithoutCodexStaysSkip`: `strictPath` met alleen `roborev` en `claude` in bin geeft `verdict` `skip` en reden `geen roborev of codex op PATH`.

- [ ] **Stap 2: Draai de tests en zie ze falen**
  Run: `go test ./internal/roborev/ -run 'ReviewAgent|GatePushFallback|GatePushCodexRun|GateFallbackWithoutCodex'`
  Verwacht: build failure op `reviewAgent`, `ClaudeAlarm` en het aantal argumenten van `GatePush`.

- [ ] **Stap 3: Implementeer.**
  - `reviewAgent` houdt de parse- en onleesbaar-logica van `budgetSkip` letterlijk.
  - Decision `codex` met code 0 geeft `codex`.
  - Decision `fallback` met code 6 gaat zo, in deze volgorde:
    1. `exec.LookPath("claude")` faalt: reden `…; geen claude op PATH`.
    2. Anders `alarm()`. Raakt die, dan reden `…; Claude-alarm: <Kind> <Pct>%`.
    3. Anders `claude-code`.
  - Elke andere combinatie geeft de reden `Codex-budget: <decision> - <reason>` van nu.
  - In `gate` roept de `default`-tak `reviewAgent` aan. Een niet-lege agent gaat als `out.Set("agent", agent)` na `lines`.
  - Pas alle aanroepers en de twee fixtures aan.

- [ ] **Stap 4: Draai de tests en zie ze slagen**
  Run: `go test -race ./internal/roborev/... ./internal/cli/... ./internal/pusher/...`
  Verwacht: PASS. Fixtures 07 en 11 slagen met de nieuwe reden.

- [ ] **Stap 5: Commit**
  ```bash
  lighthouse imports check
  git add internal/roborev internal/cli internal/pusher/roundtrip_test.go tests/pack/fixtures/roborev_gate
  git commit -m "feat(roborev): gate valt bij Codex-budget fallback terug op claude-code (#488)"
  ```

### Taak 2: Record en body-line noemen de agent

**Files:**
- Modify: `internal/roborev/classify.go`: het veld `Agent` op `Classification`, gevuld in `Classify`.
- Modify: `internal/roborev/record.go`:
  - `Record` zet `agent`.
  - `BodyLine` en `BodyLinePush` zetten de agent in de marker.
  - `pushRecord` krijgt `Agent string \`json:"agent"\``.
  - `coveringPushRecord` geeft `(sha, agent string)`.
- Modify: `internal/roborev/state.go`. `readState` neemt bij `done` de `agent` over als die in het record staat. De sleutelvolgorde wordt `verdict`, `sha`, `agent`.
- Test: `internal/roborev/agent_test.go` (uitbreiden) en `internal/roborev/classify_test.go`

**Interfaces:**
- Consumes: de gate-uitvoer `agent` uit Taak 1. Niet direct: de agent komt uit `<raw>`.
- Produces:
  - `Classification.Agent string`. Gevuld uit de eerste regel die matcht op `` `ci review: ref=\S+ agents=\[([^\]]+)\]` ``, met de groep letterlijk. Matcht geen regel, dan is het veld leeg.
  - In `classify`-JSON komt de sleutel niet: classify-fixtures blijven ongewijzigd.
  - `record --done` schrijft `agent` na `sha`, alleen als `Classification.Agent` niet leeg is.

- [ ] **Stap 1: Schrijf de falende tests**
  - `TestClassifyReadsAgentFromLogLine`:
    - De raw `2026/11/22 14:31:33 ci review: ref=a..b agents=[claude-code] types=[default] reasoning=thorough\n## roborev: Review Passed (\`<sha>\`)\n\nNo issues found.\n` geeft `Agent == "claude-code"`.
    - Dezelfde raw zonder de logregel geeft `Agent == ""`.
  - `TestRecordTakesAgentFromRaw`: in een `newPushGateRepo`-repo schrijft `Record(r.root, RecordOptions{Sha: &head, Decisions: []any{}, Out: state, Raw: raw, Range: &rng})` een record met `"agent": "claude-code"`. `raw` is de raw hierboven met `<sha>` = HEAD. Dit is het herstartpad: geen gate-`run` in deze test.
  - `TestBodyLinePushNamesAgent`: na dat record geeft `BodyLinePush(r.root)` `<!-- roborev-lokaal: <head> agent: claude-code -->`.
  - `TestBodyLineWithoutAgentKeepsOldMarker`: een record zonder `agent` geeft precies `<!-- roborev-lokaal: <head> -->`. Doe dit voor `BodyLine` en voor `BodyLinePush` via `coveringPushRecord` (een oud record plus een fix-commit, zoals `gate_test.go:262-274`).
  - Plus `TestBodyLinePushCoveringNamesAgent`: dezelfde opzet met een record met `agent` geeft de marker met `agent: claude-code`.

- [ ] **Stap 2: Draai de tests en zie ze falen**
  Run: `go test ./internal/roborev/ -run 'ClassifyReadsAgent|RecordTakesAgent|BodyLine'`
  Verwacht: FAIL (`Agent` bestaat niet, de marker heeft geen agent).

- [ ] **Stap 3: Implementeer** volgens de Interfaces hierboven. De markerformat-string staat op één plek: een helper `marker(sha, agent string) string` in `record.go`, die de drie huidige `fmt.Sprintf`-aanroepen vervangt.

- [ ] **Stap 4: Draai de tests en zie ze slagen**
  Run: `go test -race ./internal/roborev/... ./internal/cli/... ./internal/pusher/...`
  Verwacht: PASS. De bestaande marker-asserties (`roborev_fixture_test.go:681`, `roundtrip_test.go:118`, fixtures 68 en 73) blijven groen, omdat hun records geen agent dragen.

- [ ] **Stap 5: Commit**
  ```bash
  lighthouse imports check
  git add internal/roborev
  git commit -m "feat(roborev): record en body-line noemen de reviewende agent (#488)"
  ```

### Taak 3: Pusher accepteert een Claude-record (check 5)

**Files:**
- Test: `internal/pusher/roundtrip_test.go`

**Interfaces:**
- Consumes:
  - `roborev.GatePush(root, budget, alarm)` uit Taak 1.
  - `roborev.Record` met `Raw` en de marker met agent uit Taak 2.
  - `recordCovers` in `internal/pusher/floor.go`. Die blijft ongewijzigd: onbekende JSON-sleutels worden genegeerd.

- [ ] **Stap 1: Schrijf de test** `TestRoborevPushClaudeRecordPassesCheckFive`. Volg `TestRoborevPushRecordRoundTrip` (r. 50-74), met deze verschillen:
  - `pushReviewFixture` met een nep-`claude` erbij.
  - `GatePush(f.wt, fallbackOK, noAlarm)`, met `fallbackOK` als `6, {"decision":"fallback","reason":"5h 93%"}`. Assert `agent` is `claude-code`.
  - Schrijf de raw met de `ci review: … agents=[claude-code] …`-logregel en `## roborev: Review Passed (\`<head>\`)` naar het `raw`-pad van de gate.
  - Roep `Record` aan met `Out: state`, `Raw: raw` en `Range: &rng`.
  - Assert dat het record `"agent": "claude-code"` en `"uitkomst": "done"` bevat.
  - Assert dat `recordCovers(ctx, f.wt, state, head)` `true` geeft.

- [ ] **Stap 2: Draai de test**
  Run: `go test -race ./internal/pusher/ -run 'RoborevPush'`
  Verwacht: PASS. Taken 1 en 2 dragen het gedrag; deze test legt het contract met de pusher vast.
  Mutatiecheck: zet in `Record` tijdelijk `uitkomst` op `skip` als de agent `claude-code` is. De test moet dan falen. Draai de mutatie terug.

- [ ] **Stap 3: Commit**
  ```bash
  git add internal/pusher/roundtrip_test.go
  git commit -m "test(pusher): check 5 laat een done-record van een Claude-review door (#488)"
  ```

### Taak 4: Skills gebruiken de agent uit de gate, plus changelog

**Files:**
- Modify: `pack/core/skills/write-pr/SKILL.md`:
  - V2b stap 1 (r. 80-84): ``with `<agent>`, `<range>` and `<raw>` from the gate output`` en `timeout 540 roborev ci review --agent <agent> --ref <range> > <raw> 2>&1`.
  - Eén zin erbij: `<agent>` is `codex`, or `claude-code` when the Codex budget is in `fallback`; never pass a fixed agent.
  - V4 (r. 313-316): de marker `<!-- roborev-lokaal: <sha> agent: <agent> -->` after a review.
- Modify: `pack/core/skills/fix-pr/agents/SKILL.md` en `pack/core/skills/fix-pr/claude/SKILL.md`. Alleen als er een vaste `--agent codex` of "Codex review" staat. Volgens de scout delegeert fix-pr naar V2b (r. 179) en draait het geen roborev zelf. Is er niets, dan wijzigt de skilltekst niet en dekt de test hieronder het.
- Modify: `internal/packcontract/writepr_test.go:524`. De vastgezette regel wordt `timeout 540 roborev ci review --agent <agent> --ref <range> > <raw> 2>&1`. Voeg `wantNone(t, raw, "--agent codex")` toe.
- Modify: `internal/packcontract/fixpr_test.go`. De nieuwe test `TestFixPRRunsNoFixedRoborevAgent` doet `wantNone(t, body, "--agent codex")` over beide fix-pr-varianten.
- Create: `changelog.d/issue-488-roborev-claude-terugval.md`, met `type: changed`.
- Regenerate: `.claude/skills/` en `.agents/skills/` via `go run ./cmd/lighthouse init --upgrade`.

- [ ] **Stap 1: Pas de contracttests aan** zoals hierboven.
  Run: `go test ./internal/packcontract/ -run 'WritePRRoborev|FixPRRunsNoFixed'`
  Verwacht: FAIL op de ontbrekende `--agent <agent>`-regel.
- [ ] **Stap 2: Pas de skilltekst aan** in `pack/core/skills/write-pr/SKILL.md`.
- [ ] **Stap 3: Schrijf het changelog-fragment.** Nederlands, één alinea:
  - Bij een vol Codex-budget reviewt roborev met Claude, zolang `claude` op PATH staat en het Claude-alarm uit is.
  - Daardoor eindigt een workflow-push in een vol Codex-venster niet meer in een Vraag.
  - De marker en het record noemen de agent.
- [ ] **Stap 4: Installeer en verifieer**
  Run: `go run ./cmd/lighthouse init --upgrade`, daarna nogmaals `go run ./cmd/lighthouse init --upgrade`.
  Verwacht: de tweede run print `No changes.`
  Run: `go test -race ./internal/packcontract/... ./internal/roborev/... ./internal/pusher/... ./internal/cli/...`
  Verwacht: PASS.
- [ ] **Stap 5: Commit**
  ```bash
  lighthouse imports check
  git add pack/core/skills internal/packcontract changelog.d/issue-488-roborev-claude-terugval.md .claude/skills .agents/skills
  git commit -m "feat(write-pr): V2b reviewt met de agent uit de gate (#488)"
  ```

## Wat dit plan niet bewijst

- Een echte `roborev ci review --agent claude-code`-run met een echt model. De probe gebruikte nep-agents en liet alleen de argumenten zien die roborev doorgeeft.
- Een live exit 6 van het Codex-budget op de host.

Het PR-bewijs noemt beide als handwerk na de merge.
