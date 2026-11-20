# Toestand, blokkadelabel en teruggeven op één plek — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eén Go-package `internal/issuestate` met de toestandsregel, de rangregel en teruggeven,
een subcommando `lighthouse state` ervoor, en gate 0 van triage erop (#433).

**Architecture:** Pure functies (`Of`, `GateZero`, `SetLabel`) zonder I/O. Daarnaast twee functies
die schrijven via `forge.Forge` (`Apply`, `Return`), zodat GitHub en GitLab hetzelfde pad volgen. Het
subcommando volgt het patroon van `board-start`: een dunne cobra-wrapper, een `Env` met config- en
forge-opener en een exit-code. `triage.FixedGate0` wordt een wrapper om `issuestate.GateZero`. De
testgevallen van de toestandsregel staan in een JSON-fixture die `go test` leest en die de web-UI
in issue 3 van de spec ook kan lezen.

**Tech Stack:** Go, cobra, `internal/forge`, `internal/repoconfig`, `internal/dispatch` (runs),
`internal/protocol` (open vraag).

**Spec:** de spec staat nog op branch `fix-statusmodel-spec` (PR #441) en wordt gelezen met
`git show origin/fix-statusmodel-spec:docs/developer/planning/specs/2026-11-20-statusmodel-en-statusschema-design.md`.
Relevant zijn *Toestanden*, *Teruggeven door de controller* en *Eén plek voor de regels*. De
rangtabel staat in [labels.md](../../labels.md).

## Global Constraints

- Code, namen, comments en testnamen zijn Engels (`code_language = "en"`). Het plan, de commits,
  de changelog en de PR zijn Nederlands.
- Geen nieuw label en geen nieuwe Status (No-Go van milestone 16). Teruggeven zet alleen `To do`
  en een van de zes bestaande blokkadelabels.
- De board-Priority blijft staan. `forge.Forge` heeft geen Priority-write, dus `Return` raakt hem
  niet aan.
- Rang, laagste wint: 1 `needs_shaping`, 2 `needs_decision`, 3 `blocked_issue`,
  4 `blocked_external`, 5 `needs_hands`, 6 `needs_testing`. `repoconfig.Labels.Blocking()` heeft
  een **andere** volgorde. Gebruik die niet voor de rang.
- Een open `blocked_by` is een `forge.Blocker` met `State == "OPEN"`. `MergedPR` telt niet mee
  (keuze van de spec).
- Een labelnaam `""` (key die de repo niet heeft) telt nooit als aanwezig.
- Go volgt `/lighthouse-go-standard` en tests volgen `/lighthouse-test-standard`.
- Elke commit is Conventional en atomair. De PR krijgt een fragment in `changelog.d/`.
- Er verandert niets onder `pack/`, dus `init --upgrade` is niet nodig. Draai het toch één keer
  aan het eind en verwacht `No changes.`

## Review Focus

1. **Twee blokkadelabels tegelijk** (`blocked_issue` + `needs_shaping`), in strijd met de
   invariant. `Of` geeft dan *mens nodig* (rij 2 wint). `GateZero` stopt zoals `FixedGate0` nu
   (label wint), want het AC eist geen gedragswijziging. Beide gevallen staan in een test
   (Task 1, Task 2), zodat het verschil bewust is en niet stil.
2. **`label`/`return` met een sleutel die verliest** (`needs_hands` op een issue met
   `needs_shaping`). Het lagere label blijft staan, er komt geen edit en de uitvoer zegt welk label
   wint (Task 3).
3. **Een sleutel die de repo niet kent of die leeg is** (`labels.needs_testing = ""`, of een
   typfout als `needs-hands`). Dat is exit 1 met een usage-regel, zonder forge-call (Task 3, Task 4).
4. **`return` op een issue dat niet op het board staat, of in een repo zonder `[forge.board]`.**
  Het label wordt wel gezet en de board-regel zegt `skipped`. Exit 0 (Task 3, Task 4).
5. **Status `On hold` of een status die `status_options` niet kent** (`unknown:<id>`). Die is
  niet actief en de toestand volgt uit labels en blockers. De status komt van de board-naam
  (`StatusName`), niet van de prefix (Task 1, Task 4).

---

### Task 1: Toestandsregel en gedeelde fixture

**Files:**
- Create: `internal/issuestate/state.go`
- Create: `internal/issuestate/state_test.go`
- Create: `tests/fixtures/issue-state/cases.json`

**Interfaces:**
- Produces:
  ```go
  type State string
  const (Done State = "done"; Active State = "active"; NeedsHuman State = "needs_human"
         Blocked State = "blocked"; Ready State = "ready")
  type Input struct {
      Closed       bool
      Labels       []string        // label names on the issue
      Status       string          // board Status name; "" without one
      Run          string          // "open", "parked" or "" (dispatch.RunView.State)
      Blockers     []forge.Blocker
      OpenQuestion bool            // protocol.Classify(...).State == protocol.StateQuestion
  }
  type Result struct { State State; NeedsHuman bool } // NeedsHuman only true when State == Active
  func Of(in Input, l repoconfig.Labels) Result
  func OpenBlockers(bs []forge.Blocker) []int // issue numbers with State "OPEN", input order; nil for none
  ```

- [ ] **Step 1: Schrijf de fixture.** Een JSON-array van gevallen:
  `{"name", "closed", "labels" (sleutels), "status", "run", "blockers" [{"issue","state"}], "open_question", "want": {"state", "needs_human"}}`.
  Labels staan als **sleutel** (`needs_hands`), want de API van issue 3 geeft de sleutel. Velden
  die er niet zijn, zijn hun nulwaarde. Minstens deze gevallen, elk met een eigen naam:
  - af: `closed` met run `open` geeft `done`
  - rij 1: run `open`, run `parked`, status `In progress`, status `Controller` geven `active`
  - rij 2: `needs_shaping`, `needs_decision` geven `needs_human`
  - rij 3: `blocked_issue`, `blocked_external`, en open blocker zonder label geven `blocked`
  - rij 4: `needs_hands`, `needs_testing` geven `needs_human`
  - rij 5: niets, alleen een `CLOSED` blocker, status `To do` en status `On hold` geven `ready`
  - AC: open blocker + `needs_hands` geeft `blocked`, open blocker + `needs_shaping` geeft `needs_human`
  - Review Focus 1: `blocked_issue` + `needs_shaping` geeft `needs_human`
  - vlag: `active` + `needs_hands` geeft `needs_human: true`, `active` + `open_question` geeft
    `needs_human: true`, `ready` + `open_question` geeft `needs_human: false`

- [ ] **Step 2: Schrijf `TestOfCases`.** De test leest `../../tests/fixtures/issue-state/cases.json`
  met `os.ReadFile` en `encoding/json`. Hij gebruikt `repoconfig.Labels` waarin elke naam gelijk is
  aan zijn sleutel (`BlockedIssue: "blocked_issue"`, …), zodat de fixture de labels letterlijk
  doorgeeft. Per geval: `t.Run(name)`, en `Of(...)` moet gelijk zijn aan
  `Result{State(want.state), want.needs_human}`. Voeg `TestOpenBlockers` toe: OPEN 5, CLOSED 6 en
  OPEN 7 geven `[]int{5, 7}`, en geen blockers geven `nil`.

- [ ] **Step 3: Draai** `go test ./internal/issuestate/` en verwacht FAIL (package of `Of` bestaat niet).

- [ ] **Step 4: Implementeer `Of` en `OpenBlockers`** in `state.go`, met de eerste passende rij uit
  de spec (af gaat vóór rij 1). Zet een package-doc bovenaan met de verwijzing naar de spec, en
  vermeld dat `tests/fixtures/issue-state/cases.json` de gedeelde testgevallen voor de web-UI
  (`issueState()`) bevat.

- [ ] **Step 5: Draai** `go test -race ./internal/issuestate/` en verwacht PASS. Mutatiecheck:
  verwissel rij 2 en rij 3 in `Of`. De gevallen "open blocker + needs_shaping" en Review Focus 1
  moeten dan rood worden. Draai daarna de wijziging terug.

- [ ] **Step 6: Commit** met `feat(issuestate): toestandsregel van een issue met gedeelde testgevallen (#433)`.

### Task 2: Gate 0 van triage op het package

**Files:**
- Modify: `internal/issuestate/state.go`, `internal/issuestate/state_test.go`
- Modify: `internal/triage/rules.go:10-39`

**Interfaces:**
- Consumes: `OpenBlockers` (Task 1).
- Produces:
  ```go
  // Stop is the route-issue gate 0 stop.
  type Stop struct { Label string; Blockers []int }
  // GateZero: a blocked_issue (before blocked_external) label stops with that label; else open
  // blockers without needs_shaping or needs_decision stop. Blockers is OpenBlockers in both cases.
  func GateZero(labels []string, blockers []forge.Blocker, l repoconfig.Labels) (Stop, bool)
  ```
  `triage.Gate0` en de signatuur van `FixedGate0` blijven ongewijzigd.

- [ ] **Step 1: Schrijf `TestGateZero`** in `state_test.go`. Neem de zeven gevallen van
  `TestFixedGate0` (`internal/triage/rules_test.go:29-56`) over op `Stop`, plus
  `blocked_issue` + `needs_shaping` + open 5, dat `Stop{Label: "blocked_issue", Blockers: []int{5}}, true`
  geeft (Review Focus 1, het gedrag van nu).
- [ ] **Step 2: Draai** `go test ./internal/issuestate/ -run TestGateZero` en verwacht FAIL.
- [ ] **Step 3: Implementeer `GateZero`.** Maak daarna van `FixedGate0` een wrapper:
  `s, ok := issuestate.GateZero(f.Labels, f.Blockers, l)`. Bij `!ok` geeft hij `Gate0{}, false`,
  anders `Gate0{Blockers: s.Blockers, Label: s.Label}, true`. Haal de `slices`-import alleen weg als
  `Invariant` hem niet meer nodig heeft.
- [ ] **Step 4: Draai** `go test -race ./internal/issuestate/ ./internal/triage/ ./internal/cli/ -run 'GateZero|FixedGate0|Gate0|Triage'`
  en verwacht PASS, met `rules_test.go`, `outcome_test.go`, `judgment_test.go` en
  `cli/triage_test.go` **ongewijzigd**. Dat bewijst "zonder gedragswijziging".
- [ ] **Step 5: Commit** met `refactor(triage): gate 0 via issuestate.GateZero (#433)`.

### Task 3: Rangregel, blokkadelabel zetten en teruggeven

**Files:**
- Create: `internal/issuestate/label.go`, `internal/issuestate/label_test.go`
- Create: `internal/issuestate/giveback.go`, `internal/issuestate/giveback_test.go`
- Modify: `internal/forge/board.go`: methode `func (it BoardItem) EffectiveStatus() string`.
  Die geeft `StatusName` als `Status` met `UnknownStatusPrefix` begint, anders `Status`.
- Modify: `internal/boardstart/boardstart.go:75-80`: `statusOf` wordt `it.EffectiveStatus()`, en
  de functie `statusOf` verdwijnt.

**Interfaces:**
- Produces:
  ```go
  var Keys = []string{"needs_shaping", "needs_decision", "blocked_issue", "blocked_external", "needs_hands", "needs_testing"} // rank order
  // Name is the label name of key in l; ok false for an unknown key or an empty name.
  func Name(key string, l repoconfig.Labels) (name string, ok bool)
  type LabelEdit struct { Winner string; Add, Remove []string } // label names
  // SetLabel: winner = lowest rank among the blocking labels in current plus want; Add = [winner]
  // when current lacks it; Remove = every other blocking label in current, in rank order.
  func SetLabel(current []string, wantKey string, l repoconfig.Labels) (LabelEdit, error) // error: unknown key or empty name
  // Apply reads issue nr, applies SetLabel and sends one EditIssue with Add and Remove, or none
  // when both are empty.
  func Apply(ctx context.Context, f forge.Forge, nr int, wantKey string, l repoconfig.Labels) (LabelEdit, error)
  const ToDo = "To do"
  type Returned struct { Label LabelEdit; From string; OnBoard bool } // From: EffectiveStatus before
  // Return: Apply, then when board is true IssueItem and SetStatus(item, ToDo) unless it is
  // already To do. Not on the board: OnBoard false and no error.
  func Return(ctx context.Context, f forge.Forge, nr int, wantKey string, l repoconfig.Labels, board bool) (Returned, error)
  ```

- [ ] **Step 1: Schrijf `TestSetLabel`** als tabeltest, met labelnamen gelijk aan de sleutel:
  - leeg + `needs_hands` geeft `{needs_hands, [needs_hands], nil}`
  - `needs_testing` + `needs_shaping` geeft `{needs_shaping, [needs_shaping], [needs_testing]}`
  - `needs_shaping` + `needs_hands` geeft `{needs_shaping, nil, nil}` (Review Focus 2)
  - `needs_hands` + `needs_hands` geeft `{needs_hands, nil, nil}`
  - `blocked_external`, `needs_testing` en `2-area:x` + `blocked_issue` geven
    `{blocked_issue, [blocked_issue], [blocked_external, needs_testing]}`; het area-label blijft
  - `needs-hands` (typfout) geeft een error, en `needs_testing` met `NeedsTesting: ""` geeft een error (Review Focus 3)
- [ ] **Step 2: Schrijf `TestApply` en `TestReturn`** met een fake die `forge.Forge` embedt en
  `Issue`, `EditIssue`, `IssueItem` en `SetStatus` overschrijft. De fake legt de calls vast, en elke
  andere methode panict via de nil-embed. Gevallen:
  - Apply zonder wijziging doet 0 `EditIssue`-calls.
  - Apply met add en remove doet precies 1 `EditIssue`, met beide velden.
  - Return op een item met `Controller` geeft `SetStatus(item, "To do")` en `From "Controller"`.
  - Return op `To do` doet geen `SetStatus`.
  - Return met `board=false` doet geen `IssueItem`.
  - Return met `IssueItem` ok=false geeft `OnBoard false` en geen error (Review Focus 4).
  - Return met status `unknown:x` en `StatusName "On hold"` geeft `From "On hold"` (Review Focus 5).
  - Een error van `EditIssue` stopt vóór `SetStatus`.
- [ ] **Step 3: Draai** `go test ./internal/issuestate/` en verwacht FAIL.
- [ ] **Step 4: Implementeer** `Keys`, `Name`, `SetLabel` en `Apply` in `label.go`, `Return` in
  `giveback.go`, en `EffectiveStatus` met de boardstart-aanpassing.
- [ ] **Step 5: Draai** `go test -race ./internal/issuestate/ ./internal/boardstart/ ./internal/cli/ ./internal/forge/`
  en verwacht PASS. Mutatiecheck: draai de volgorde van `Keys` om. `TestSetLabel` moet dan rood
  worden. Draai daarna de wijziging terug.
- [ ] **Step 6: Commit** in twee commits:
  `refactor(forge): BoardItem.EffectiveStatus vervangt statusOf van board-start (#433)` en
  `feat(issuestate): rangregel voor het blokkadelabel en teruggeven naar To do (#433)`.

### Task 4: Subcommando `lighthouse state`

**Files:**
- Create: `internal/issuestate/cmd.go` (`Env`, `Show`, `Label`, `GiveBack`, die elk de exit-code teruggeven)
- Create: `internal/cli/state.go`, `internal/cli/state_test.go`
- Modify: `internal/cli/root.go:100`: `root.AddCommand(newStateCommand())` na `newBoardStartCommand()`

**Interfaces:**
- Consumes: `Of`, `OpenBlockers`, `Apply`, `Return`, `Name`, `forge.BoardItem.EffectiveStatus`.
- Produces: het commando, met uitvoer op stdout:
  ```text
  lighthouse state show <nr>            → "state: #<nr> <state>" or "state: #<nr> active, needs human"
  lighthouse state label <nr> <key>     → "label: #<nr> <winner> (+<add>, -<remove>)" or "label: #<nr> keeps <winner>"
  lighthouse state return <nr> <key>    → the label line, then "board: #<nr> <from> → To do",
                                          "board: #<nr> stays To do", "board: #<nr> is not on the board"
                                          or "board: skipped — no [forge.board] configured"
  ```
  Exit 0 bij succes. Exit 1 bij usage (fout nummer of onbekende sleutel, zonder forge-call) of een
  forge-error (`state: ERROR — <oneline>`). Exit 2 bij een config-error, zoals in `boardstart.Run`.
  `Env` heeft `Config` en `Forge` zoals `boardstart.Env`, plus
  `Runs func(ctx, repo string) ([]dispatch.RunView, error)`.

- [ ] **Step 1: Schrijf `internal/cli/state_test.go`** naar het model van
  `TestBoardStartOpensTheForgeOfLighthouseToml` (`internal/cli/boardstart_test.go:52`):
  `forgetest.New`, `newForgeOpener` en `execute`. Gevallen:
  - `show 7` met item `In progress` geeft `state: #7 active`.
  - `show 7` met label `3-needs-hands` en een open blocker geeft `state: #7 blocked`.
  - `label 7 needs_shaping` op een issue met `3-needs-testing` doet één PATCH met beide wijzigingen.
  - `return 7 needs_hands` op `Controller` geeft twee regels en zet de status naar `opt-todo`.
  - `label 7 needs-hands` geeft exit 1 en 0 requests op de server.
  - `return 7 needs_hands` zonder `[forge.board]` zet het label en print `board: skipped — …`.

  Run-gegevens komen via een gestubde `Runs`. Een geval: run `parked` geeft `active`.
- [ ] **Step 2: Draai** `go test ./internal/cli/ -run TestState` en verwacht FAIL.
- [ ] **Step 3: Implementeer.**
  - `show` leest `Issue` (labels, state `closed`), `BlockedBy`, `IssueItem` (alleen met board) en
    `Comments`. `OpenQuestion` komt uit `protocol.Classify(comments).State == protocol.StateQuestion`.
  - De run komt uit `dispatch.LoadView(ctx, db, []string{repo})`, het eerste `RunView` met dit
    issue. De DB komt van `store.Open(ctx, store.DefaultPath())`, zoals in `internal/cli/serve.go:186-192`.
  - De labels komen van `repoconfig.LoadFile(config.Path())`. De cobra-wrapper heeft drie
    subcommando's met `cobra.ExactArgs`.
- [ ] **Step 4: Draai** `go test -race ./internal/cli/ ./internal/issuestate/` en verwacht PASS.
  Draai daarna `go run ./cmd/lighthouse state show 433` in deze worktree. Verwacht een
  `state: #433 …`-regel of een forge-error op één regel, nooit een panic.
- [ ] **Step 5: Commit** met `feat(cli): lighthouse state show|label|return (#433)`.

### Task 5: Docs, changelog en de gate

**Files:**
- Modify: [labels.md](../../labels.md): voeg onder de rangtabel een zin toe dat
  `lighthouse state label <nr> <key>` de rangregel toepast.
- Create: `changelog.d/issue-433-toestand-package.md` (Nederlands, in de vorm van de bestaande fragmenten)

- [ ] **Step 1: Schrijf de docs-zin en het fragment.**
- [ ] **Step 2: Draai** `go run ./cmd/lighthouse init --upgrade` en verwacht `No changes.`
- [ ] **Step 3: Draai de test-gate** uit `lighthouse.toml` `[test_gate] command`, of minstens
  `lighthouse gate go -- go test -race` en `golangci-lint run ./...`. Verwacht alles groen.
- [ ] **Step 4: Commit** met `docs(labels): lighthouse state past de rangregel toe (#433)`.
