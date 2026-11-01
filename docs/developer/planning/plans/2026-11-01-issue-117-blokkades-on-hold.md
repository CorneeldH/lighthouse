# Blokkades naar On hold en terug (#117) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Een uitvoerend issue met een actieve voorganger parkeert de tick met 🤖 Geparkeerd en On hold, en zet het terug naar Controller zodra geen voorganger meer actief is; het overzicht toont alleen Controller-issues.

**Architecture:** Een nieuwe protocol-kind `parked`. Decide krijgt drie acties: `park` (comment, dan board naar On hold), `hold` (alleen board, voor een half gelukte parkering) en `unpark` (board naar Controller). Apply schrijft de board-status via een nieuwe `Board`-afhankelijkheid in `Env`. Het overzicht krijgt een filter op de board-status, dat `serve` uit `host.toml [dispatch]` haalt.

**Tech Stack:** Go (`internal/protocol`, `internal/dispatch`, `internal/overview`, `internal/server`, `internal/cli`), Python-tweeling in `pack/core/lib/protocol.py`, TOML-koppen in `pack/core/headings.toml`.

**Spec:** [2026-10-27-m2-dispatcher-design.md](../specs/2026-10-27-m2-dispatcher-design.md), secties *3. De tick* (Apply), *4. Decide*, *5. Eigen comments*, *9. Blokkades*, *12. Zicht* (Overzicht), *Teststrategie*.

## Global Constraints

- Code, comments en bestandsnamen Engels; plan, docs, commits en changelog Nederlands.
- `kind_parked`: nl `"Geparkeerd"`, en `"Parked"` (spec 9).
- Actieve voorganger = `state == "OPEN"` en geen gemergde PR (`MergedPR == false`). Gesloten zonder merge telt als vervallen (spec 9).
- "Eigen" Geparkeerd = de 🤖-kop klopt **en** de auteur is `Snapshot.TokenLogin` (spec 5).
- Een samengestelde actie post altijd eerst de comment, dan de board-wijziging (spec 3).
- Lighthouse haalt een issue alleen uit Controller om het te parkeren (spec 12).
- `lighthouse run` voert geen park, hold of unpark uit; dat blijft aan de tick (spec 3). `runKinds` in `internal/cli/run.go` blijft ongewijzigd.
- Dogfood: na een wijziging onder `pack/` draait `go run ./cmd/lighthouse init --upgrade`, en de geïnstalleerde kopie gaat mee in dezelfde commit; daarna geeft het `No changes.`
- Elke PR voegt `changelog.d/issue-117-blokkades-on-hold.md` toe.
- Verificatie per taak: `go test -race` op de geraakte pakketten; aan het eind `[test_gate] command` uit `lighthouse.toml`.

## Besluiten die dit plan neemt

- **Parked is een statusnotitie voor de beurt.** `lastTurn` in `decide.go` slaat Parked over, zoals Rebased. Zo ziet de Handoff-regel na het terugzetten de laatste Handoff weer (spec 9: "De volgende tick start de laatste Handoff").
- **Half gelukt herkennen:** de Handoff-regel komt bij een actieve voorganger en vraagt: is de laatste protocol-comment (Rebased niet meegeteld) een eigen Parked? Dan `hold`, anders `park`. Zo post de tick nooit een tweede Geparkeerd.
- **Board-status van een voorganger** komt uit hetzelfde board dat gather al leest: `forge.Blocker` krijgt een veld `Status`. Een voorganger die niet op het board staat, heeft `Status == ""` en verschijnt in de comment zonder haakjes.
- **Overzicht zonder board:** draait `serve` voor een repo die niet in `host.toml [dispatch]` staat, of zonder `[forge.board]`, dan geldt het gedrag van M1: geen filter (spec 12: "Zonder `[dispatch]` blijft het gedrag van M1").

## Review Focus

1. **Een Handoff met 🚀 na het terugzetten.** Na `unpark` start de tick de Handoff; die krijgt 🚀. Een daarna nog actieve voorganger mag geen nieuwe park geven op een verbruikte Handoff → test `TestDecideFixtures/park-spent-handoff` (taak 3).
2. **Een Geparkeerd van een vertrouwde mens** (niet de token-login) mag nooit terugzetten → fixture `on-hold-foreign-parked` (taak 3).
3. **Een voorganger met gemergde PR die nog open staat** (de nacontrole loopt) is niet actief: geen park, wel start of unpark → fixtures `handoff-blocker-merged-pr-starts`, `unpark-merged-pr` (taak 3).
4. **Een mislukte board-wijziging na een geplaatste comment** moet de volgende tick afmaken, en de vingerafdruk mag die tick niet overslaan → fixture `park-half-done` (taak 3) en `TestTickParkBoardFailureRetries` (taak 4).
5. **Een gesloten issue in On hold met een eigen Geparkeerd** zet lighthouse niet terug → fixture `unpark-closed-issue` (taak 3).

---

### Task 1: Protocol-kind `parked`

**Files:**
- Modify: `pack/core/headings.toml` (na `[kind_rebased]`)
- Modify: `internal/protocol/header.go:24-34`
- Modify: `internal/protocol/render.go` (na `RenderReturned`)
- Modify: `pack/core/lib/protocol.py:40`
- Modify: `tests/pack/fixtures/protocol/headers.json` (na `en-rebased`)
- Modify: `tests/pack/test_headings.py:22-30`, `tests/pack/test_protocol.py:73-81` (de `KINDS`-tuple in `test_public_names`)
- Test: `internal/protocol/header_test.go:60-66`, `internal/protocol/render_test.go` (nieuwe `TestRenderParked`)
- Installed copy: `.lighthouse/headings.toml`, `.lighthouse/lib/lighthouse_lib/protocol.py`

**Interfaces:**
- Produces: `protocol.KindParked Kind = "parked"`; `func RenderParked(blockers []string, language string) string`.

- [ ] **Step 1: Write the failing tests**

In `headers.json` twee gevallen: `{"name": "nl-parked", "body": "🤖 Geparkeerd", "kind": "parked"}` en `{"name": "en-parked", "body": "🤖 Parked", "kind": "parked"}`. In `test_headings.py` `NL`: `"kind_parked": "Geparkeerd"`. In `test_protocol.py` `test_public_names`: `"parked"` achteraan de verwachte `KINDS`-tuple. In `header_test.go` de rij `{KindParked, ...}` in de kinds-tabel. In `render_test.go` **geen** rij in `TestParseHeaderOfRendered`: die lus eist `header.Session == testSession` (`render_test.go:66`) en een Geparkeerd heeft geen sessie. De roundtrip zit in `TestRenderParked`, ook voor `en`:

```go
func TestRenderParked(t *testing.T) {
	got := RenderParked([]string{"o/r#5 (In progress)", "x/y#9"}, "nl")
	want := "🤖 Geparkeerd\nblocked_by: o/r#5 (In progress), x/y#9"
	if got != want {
		t.Errorf("RenderParked = %q, want %q", got, want)
	}
	h, ok := ParseHeader(got)
	if !ok || h.Kind != KindParked || h.Fields["blocked_by"] != "o/r#5 (In progress), x/y#9" {
		t.Errorf("ParseHeader = %+v, %v", h, ok)
	}
	if h, ok := ParseHeader(RenderParked([]string{"o/r#5"}, "en")); !ok || h.Kind != KindParked || h.Session != "" {
		t.Errorf("en: ParseHeader = %+v, %v", h, ok)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/protocol/ && uv run pytest -q tests/pack/test_headings.py tests/pack/test_protocol.py`
Expected: FAIL (`undefined: KindParked`, de Python-fixture kent `parked` niet).

- [ ] **Step 3: Implement**

`[kind_parked]` met nl/en in `headings.toml`; `KindParked` in de consts en de `kinds`-slice; `"parked"` in `KINDS`. `RenderParked` schrijft kop en één regel `blocked_by: ` met de regels gescheiden door `", "`, zonder footer (er is geen sessie). Doc-comment: geen Python-tweeling, want alleen de tick post een Geparkeerd.

- [ ] **Step 4: Dogfood en groen**

Run: `go run ./cmd/lighthouse init --upgrade && go run ./cmd/lighthouse init --upgrade && go test -race ./internal/protocol/ && uv run pytest -q tests/pack/`
Expected: de tweede `init --upgrade` meldt `No changes.`; alle tests PASS.

- [ ] **Step 5: Commit**

```bash
git add pack/core/headings.toml pack/core/lib/protocol.py internal/protocol tests/pack .lighthouse
git commit -m "feat(protocol): kind Geparkeerd (#117)"
```

### Task 2: Board-status van voorgangers in de snapshot

**Files:**
- Modify: `internal/forge/board.go:26-31` (`Blocker`)
- Modify: `internal/dispatch/gather.go` (`Prepare`)
- Test: `internal/dispatch/gather_test.go`

**Interfaces:**
- Produces: `forge.Blocker.Status string` met JSON-tag `status,omitempty`: de naam van de Status-optie van de voorganger op hetzelfde board, `""` als hij er niet op staat.

- [ ] **Step 1: Write the failing test**

`TestPrepareBlockerStatus`, met een eigen kopie van `testRepo` waarvan `StatusOptions` ook `"In progress": "opt-prog"` heeft (de gedeelde `testRepo` in `gather_test.go:21-27` kent die optie niet): een board met item 7 (Controller, `blocked_by` o/r#5 en x/y#9), item 5 van o/r met Status "In progress", geen item voor x/y#9. Verwacht: `Items[0].BlockedBy` = `[{o/r 5 OPEN false "In progress"} {x/y 9 OPEN false ""}]`. Item 5 zelf staat niet in `Items` (geen dispatch-status).

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/dispatch/ -run TestPrepareBlockerStatus`
Expected: FAIL (`unknown field Status`).

- [ ] **Step 3: Implement**

In `Prepare` eerst over alle board-items een map `(Repo, Issue) → statusnaam` bouwen, vóór het filter op `dispatchStatuses`; daarna per gekozen item een kopie van `BlockedBy` met `Status` ingevuld (de slice van `bi` niet muteren). De vingerafdruk blijft ongewijzigd: de status is alleen tekst voor de comment.

- [ ] **Step 4: Run tests**

Run: `go test -race ./internal/dispatch/ ./internal/forge/ ./internal/cli/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/forge/board.go internal/dispatch/gather.go internal/dispatch/gather_test.go
git commit -m "feat(dispatch): board-status van voorgangers in de snapshot (#117)"
```

### Task 3: Decide parkeert, maakt af en zet terug

**Files:**
- Modify: `internal/dispatch/decide.go`
- Rename: `internal/dispatch/testdata/decide/handoff-blocker-executing-defer.json` → `handoff-blocker-executing-park.json`
- Create: fixtures in `internal/dispatch/testdata/decide/` (lijst hieronder)

**Interfaces:**
- Consumes: `protocol.KindParked` (taak 1), `forge.Blocker.Status` (taak 2).
- Produces:
  - `KindPark Kind = "park"`, `KindHold Kind = "hold"`, `KindUnpark Kind = "unpark"`.
  - `Action.ItemID string` (`item_id,omitempty`) op elke park, hold en unpark.
  - `Action.Blockers []string` (`blockers,omitempty`) op park: per actieve voorganger `"<repo>#<n> (<status>)"`, of `"<repo>#<n>"` bij lege `Status`, in de volgorde van `BlockedBy`.
  - Constante `statusOnHold = "On hold"`.

Regels (spec 9):

1. `blocked()` wordt `active()`: een voorganger met `State == "OPEN" && !MergedPR`.
2. `lastTurn` slaat ook `KindParked` over.
3. Nieuwe helper `ownParked(c issueCtx) bool`: de laatste protocol-comment zonder Rebased is een Parked met `Author == c.repo.Snapshot.TokenLogin`.
4. In `handoff()` vervangt `Executing(j.Command) && c.active()` de defer `"blocker"`: `hold` als `ownParked`, anders `park`. De doc-comment van `Action` noemt bij `KindDefer` alleen nog `night`.
5. In `decideIssue` een tak voor `it.Status == statusOnHold && it.State == "OPEN" && free`: `unpark` als `ownParked` en geen voorganger actief; anders niets.

- [ ] **Step 1: Write the failing fixtures**

Elke fixture volgt de vorm van `handoff-blocker-executing-defer.json` (`input`, `want`), `token_login: "lh-bot"`, taal `nl`. De echte paia-toestand komt van 2026-11-01 (#464: blocker #1600 "In progress"; #757: blocker #1549 "To do"; beide On hold, laatste 🤖 een Handoff naar `/superpowers:subagent-driven-development`, #464 met legacy-sleutels `volgende`/`anker`).

| Fixture | Toestand | `want` |
|---|---|---|
| `handoff-blocker-executing-park` | hernoemd; Controller, uitvoerende Handoff, o/r#5 open | `park`, `item_id "I7"`, `blockers ["o/r#5"]` |
| `blocked-paia-on-hold` | #464 en #757 met hun echte comments, On hold, blokkeerder open | `[]` |
| `blocked-paia-controller` | dezelfde twee items in Controller | twee `park`s met `blockers ["CorneeldH/personal_ai_agent#1600 (In progress)"]` en `["CorneeldH/personal_ai_agent#1549 (To do)"]` |
| `park-half-done` | Controller, Handoff, daarna eigen Geparkeerd, voorganger open | `hold`, `item_id "I7"` |
| `park-half-done-blocker-gone` | als boven, voorganger CLOSED | `start` van het Handoff-commando met `comment_id` van de Handoff |
| `park-spent-handoff` | Controller, Handoff met 🚀 en een runrij, voorganger open | `[]` |
| `handoff-blocker-merged-pr-starts` | Controller, uitvoerende Handoff, voorganger OPEN met `merged_pr: true` | `start` |
| `unpark` | On hold, eigen Geparkeerd laatst, voorganger CLOSED | `unpark`, `item_id "I7"` |
| `unpark-merged-pr` | als boven, voorganger OPEN met `merged_pr: true` | `unpark` |
| `on-hold-parked-blocker-active` | On hold, eigen Geparkeerd, voorganger open | `[]` |
| `on-hold-human-blocker-gone` | On hold, laatste 🤖 een Handoff, voorganger CLOSED | `[]` |
| `on-hold-foreign-parked` | On hold, Geparkeerd van `owner` (vertrouwd, niet de token-login), voorganger CLOSED | `[]` |
| `unpark-closed-issue` | On hold, issue CLOSED, eigen Geparkeerd, voorganger CLOSED | `[]` |

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/dispatch/ -run TestDecideFixtures`
Expected: FAIL op de nieuwe fixtures en op de hernoemde (krijgt nu `defer`).

- [ ] **Step 3: Implement the five rules above in `decide.go`**

`park` en `hold` zijn geen start: `group()` geeft ze 0, zoals een return.

- [ ] **Step 4: Run tests**

Run: `go test -race ./internal/dispatch/`
Expected: PASS, ook `TestDecideKeysAreHeadings` (park, hold en unpark dragen geen `Reason`).

- [ ] **Step 5: Commit**

```bash
git add internal/dispatch/decide.go internal/dispatch/testdata/decide
git commit -m "feat(dispatch): parkeren, afmaken en terugzetten in decide (#117)"
```

### Task 4: Apply schrijft het board

**Files:**
- Modify: `internal/dispatch/apply.go`
- Create: `internal/dispatch/board.go`
- Modify: `internal/cli/tick.go:277` (`applyActions`)
- Modify: `internal/forge/forgetest/server.go:360` (`SetStatus` vastleggen), `:62-63` en `:337-344` (fout per operatie)
- Test: `internal/dispatch/apply_test.go`, `internal/dispatch/board_test.go`, `internal/cli/tick_test.go`

**Interfaces:**
- Consumes: `KindPark`, `KindHold`, `KindUnpark`, `Action.ItemID`, `Action.Blockers` (taak 3); `protocol.RenderParked` (taak 1).
- Produces:
  - `type Board interface { SetStatus(ctx context.Context, itemID, status string) error }` in `apply.go`; `Env.Board func(repo string) Board`.
  - `func NewBoard(c StatusSetter, b repoconfig.Board) Board` in `board.go`, met `type StatusSetter interface { SetStatus(ctx context.Context, projectID, itemID, fieldID, optionID string) error }` (`*forge.Client` voldoet). Een status die niet in `b.StatusOptions` staat geeft een fout die de status noemt.
  - `forgetest.Server.StatusSets []map[string]any`: de variabelen van elke `SetStatus`.
  - `forgetest.Server.FailOps map[string]string`: operatienaam → GraphQL-foutmelding; alleen die operatie antwoordt 200 met die fout en `data: null`, de rest werkt gewoon. `GraphQLErrors` (alle operaties) en `Fail` (op `METHOD path`, dus ook de Board-query) zijn daarvoor onbruikbaar: dan faalt de tick al in `Prepare`.

- [ ] **Step 1: Write the failing tests**

In `apply_test.go`, met een `fakeBoard` die in dezelfde `callLog` schrijft als `fakePoster`:
- `TestApplyParkPostsThenHolds`: log `["CreateComment", "SetStatus"]`; body begint met `"🤖 Geparkeerd\nblocked_by: o/r#5 (In progress)"`; `SetStatus("I7", "On hold")`.
- `TestApplyParkPostFailsNoBoard`: `postErr` → fout, geen `SetStatus`.
- `TestApplyParkBoardFails`: board-fout → `Result.Failed["o/r"]` niet nil; de comment staat.
- `TestApplyHold`: alleen `SetStatus("I7", "On hold")`.
- `TestApplyUnpark`: alleen `SetStatus("I7", "Controller")`.

In `board_test.go`: `TestNewBoardMapsStatus` (`"On hold"` → `projectID`, item, `StatusFieldID`, optie-id `"h1"`) en `TestNewBoardUnknownStatus` (fout met `"Gone"` erin, geen call).

In `tick_test.go`, `TestTickParksBlockedHandoff`: board-item 7 in Controller met een open voorganger en een uitvoerende `handoffComment`; na de tick staat op #7 één nieuwe comment die begint met `"🤖 Geparkeerd"`, en `srv.StatusSets` heeft één entry met `item "I7"` en `option "h1"`. `TestTickParkBoardFailureRetries`: de eerste tick draait met `srv.FailOps = {"SetStatus": "boom"}`, post de Geparkeerd en geeft exit 1; daarna `FailOps = nil`, en de tweede tick, met de Geparkeerd nu in de comments, post niets nieuws en zet `SetStatus` op `"h1"`.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/dispatch/ ./internal/cli/ -run 'Park|Hold|Unpark|NewBoard'`
Expected: FAIL (`undefined: Board`, `unknown action kind "park"`).

- [ ] **Step 3: Implement**

Drie cases in `applier.apply`: park = `CreateComment(RenderParked(a.Blockers, a.Language))`, bij succes `Board.SetStatus(a.ItemID, statusOnHold)`; hold idem zonder comment; unpark `SetStatus(a.ItemID, statusController)`. In `applyActions`: `Board: func(slug string) dispatch.Board { return dispatch.NewBoard(env.client(slug, token), *repos[slug].Board) }` (`Dispatchable` garandeert een board). Als `SetStatus` faalt in een tick, slaat tick de vingerafdruk van die repo al niet op (`res.Failed`); test dat, voeg niets toe.

- [ ] **Step 4: Run tests**

Run: `go test -race ./internal/dispatch/ ./internal/cli/ ./internal/forge/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/dispatch internal/cli/tick.go internal/cli/tick_test.go internal/forge/forgetest/server.go
git commit -m "feat(dispatch): apply parkeert en zet terug op het board (#117)"
```

### Task 5: Het overzicht alleen voor Controller-issues (#85 deel 2)

**Files:**
- Modify: `internal/overview/overview.go`
- Modify: `internal/server/server.go:27` (`New`), `internal/server/forge.go:48` (`registerForge`)
- Modify: `internal/cli/serve.go`
- Test: `internal/overview/overview_test.go`, `internal/server/forge_test.go`, `internal/server/server_test.go`, `internal/cli/serve_test.go`

**Interfaces:**
- Produces:
  - `type Filter func(ctx context.Context) (map[int]bool, error)` in `overview`; `func Build(ctx context.Context, src Source, inController Filter) (Overview, error)`. `nil` = alle open issues (M1).
  - `type BoardReader interface { BoardItems(ctx context.Context, projectID string) ([]forge.BoardItem, error) }`; `func ControllerIssues(r BoardReader, slug string, b repoconfig.Board) Filter`: de issues van `slug` waarvan de Status-optie `b.StatusOptions["Controller"]` is.
  - `server.New(site, report, src, inController overview.Filter, leftovers)`.
  - `func controllerFilter(hostToml, repo string, c *forge.Client, errOut io.Writer) (overview.Filter, error)` in `serve.go`.
  - `serve(ctx, addr, dbPath string, src overview.Source, inController overview.Filter, errOut io.Writer)`: `newServeCommand` bouwt de filter in `RunE` en geeft hem door.

- [ ] **Step 1: Write the failing tests**

- `TestBuildOnlyController` (overview): issues 945 (open Vraag zonder antwoord, de toestand van paia#945 op 2026-10-26: een uitvoerende run met een open Vraag, buiten Controller) en 12 (Teruggegeven) en 13 (open Vraag); filter `{12: true, 13: true}`. Verwacht: `Questions` alleen 13, `Returned` alleen 12, en `Comments` is nooit voor 945 gevraagd.
- `TestBuildFilterFails`: filter-fout → `Build` faalt met die fout.
- `TestControllerIssues`: board met o/r#12 in Controller (`"c1"`), o/r#945 in `"In progress"`, x/y#12 in Controller → `{12: true}`.
- `TestQuestionsEndpointFilter` en `TestReturnedEndpointFilter` (server, `forge_test.go`): `server.New` met een fake-bron (zoals `TestQuestionsEndpoint`) en een filter zonder het issue met de open Vraag of de Teruggegeven → het endpoint geeft een lege lijst; met `nil` staat het issue er wel. Dit bewijst de wiring `New` → `registerForge` → `Build`.
- `TestServeFilter` (cli, naast `serve_test.go:99`): `serve` met een filter die het issue weglaat → `/api/questions` op het echte adres geeft het niet. Dit bewijst dat `serve` de filter aan `server.New` doorgeeft.
- `TestControllerFilterFromHostToml` (cli): host.toml met `[dispatch] repos = [<tmp repo met board>]`, `--repo` gelijk aan de slug → filter niet nil; een andere slug → nil; geen host.toml → nil. Een onleesbare host.toml → fout.
- Bestaande `Build`-aanroepen en `server.New` in tests krijgen `nil`.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/overview/ ./internal/server/ ./internal/cli/ -run 'Controller|Filter|Build'`
Expected: FAIL (te veel argumenten, `undefined: ControllerIssues`).

- [ ] **Step 3: Implement**

`Build` haalt de filter-map vóór `fetchStatuses` op en laat issues buiten de map vallen voordat er comments worden gelezen. `controllerFilter` leest `hostconfig.LoadDispatch(hostToml)` (fout → `serve` start niet), dan per pad `repoconfig.Load`; een repo die niet laadt krijgt één regel op `errOut` en wordt overgeslagen. De eerste repo met `Slug == repo` en een `Board` geeft `ControllerIssues(c, repo, *Board)`; heeft dat board geen `StatusOptions["Controller"]`, dan één regel op `errOut` en een `nil`-filter (een lege optie-id zou gelijk zijn aan elk item zonder Status). `serve` geeft de filter door aan `server.New`; `newServeCommand` gebruikt `hostconfig.Path()`.

- [ ] **Step 4: Run tests**

Run: `go test -race ./internal/overview/ ./internal/server/ ./internal/cli/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/overview internal/server internal/cli/serve.go internal/cli/serve_test.go
git commit -m "feat(overview): alleen issues in Controller (#117)"
```

### Task 6: Docs en changelog

**Files:**
- Modify: `docs/tick.md` (sectie *Wat tick doet*, regels 35-46; "een open voorganger" als uitstelreden op regels 66 en 132 eruit; de sectie over `lighthouse run`: een geblokkeerd issue geeft daar `nothing to start`, want `runKinds` filtert park weg; plus een nieuwe sectie `## Blokkades`)
- Modify: `docs/install.md` (de passage over `/api/questions`)
- Create: `changelog.d/issue-117-blokkades-on-hold.md`

- [ ] **Step 1: Schrijf**

`docs/tick.md` *Blokkades*: wanneer de tick parkeert (uitvoerende Handoff, actieve voorganger, definitie van actief), de Geparkeerd-comment met `blocked_by`, het afmaken van een half gelukte parkering, het terugzetten, en dat een menselijk On hold blijft staan. `docs/install.md`: het overzicht toont alleen issues in Controller als de repo in `host.toml [dispatch]` staat met een `[forge.board]`; anders alle open issues; `serve` leest dan het board, dus het token heeft scope `project` nodig. Changelog: één alinea in de stijl van `changelog.d/issue-114-decide-apply-keten.md`.

- [ ] **Step 2: Verifieer**

Run de Go-KPI's uit `.agents/skills/lighthouse-go-standard/SKILL.md`, de test gate en de dogfood-check:

```bash
git ls-files -z '*.go' | xargs -0r gofmt -l
go mod tidy -diff
go vet ./...
golangci-lint run ./...
go test -race -shuffle=on ./...
bash -c "$(python3 .lighthouse/lib/lighthouse_lib/config.py test_gate.command)"
go run ./cmd/lighthouse init --upgrade
```

Expected: `gofmt` en `go mod tidy -diff` zonder uitvoer; de overige rc 0; `No changes.` Rapporteer elke uitkomst.

- [ ] **Step 3: Commit**

```bash
git add docs/tick.md docs/install.md changelog.d/issue-117-blokkades-on-hold.md
git commit -m "docs(tick): blokkades en het overzicht voor Controller-issues (#117)"
```
