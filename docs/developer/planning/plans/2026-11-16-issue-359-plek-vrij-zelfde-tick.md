# Plek die een tick vrijmaakt telt al in die tick — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** de rem geeft de plek van een run die in deze tick sluit al aan de starts van deze tick.

**Architectuur:** decide markeert elke sluiting die een plek vrijmaakt (`planned.frees`) en geeft
haar de node en het profiel van de run. `Brake.apply` toetst de starts tegen een kopie van de rem
waarin die runs zijn afgeteld (`Brake.freed`, het omgekeerde van `CountRun`). `closed` en
`nodeClosed` blijven ongewijzigd.

**Tech stack:** Go, `internal/dispatch`; docs in het Nederlands.

**Spec:** [2026-11-16-issue-359-plek-vrij-zelfde-tick-design.md](../specs/2026-11-16-issue-359-plek-vrij-zelfde-tick-design.md)

## Globale eisen

- Code, namen en commentaar Engels; `docs/` en commitberichten Nederlands (Conventional Commits).
- `/lighthouse-go-standard` en `/lighthouse-test-standard` gelden; `gofmt` en `golangci-lint`
  draaien als hook.
- De geheugenrem verandert niet: `MemAvailableGiB` en `passed` blijven zoals ze zijn.
- Worktree: `/home/corneel/repositories/lighthouse/.claude/worktrees/issue-359-plek-vrij-zelfde-tick`.
  Begin elk shellcommando met `cd` daarheen.
- Testcommando: `go test -race ./internal/dispatch/ ./internal/cli/`.

## Review-focus

1. **Tellers onder nul.** Een sluiting die `freed` aftelt van een rem die haar run niet telde
   (fixture met `in_use: 0`), mag `InUse`, `SoftwareInUse` of `n.InUse` niet negatief maken; anders
   laat de rem meer starts toe dan `slots`. Test in taak 1.
2. **Dezelfde issue sluit en start.** Een handoff-sluiting met een volgende start voor hetzelfde
   issue geeft nu bij volle plekken een start in dezelfde tick (netto nul). Bestaande fixtures die
   dat als `defer` vastlegden, veranderen; taak 2 noemt ze.
3. **Fix-pr-sluiting die na de rem een Teruggegeven wordt** (`settleUnstartedFixPR`, actieve
   blokkeerder): maakt in deze tick niets vrij, ook niet als Teruggegeven. Conservatief en
   bedoeld; geen test buiten de bestaande fix-pr-tests.
4. **De gedeelde `Nodes`-map.** `freed` mag de map van `b` niet wijzigen: `b` blijft de meting.
   Test in taak 1.
5. **Pauze en `Manual`.** Bij een pauze vallen alle starts weg, bij `Manual` slaat `closed`
   plekken over; `freed` verandert daar niets aan. Gedekt door de bestaande fixtures
   `brake-paused-settles` en `brake-manual`.

---

### Taak 1: `Brake.freed` en de toets in `apply`

**Files:**
- Modify: `internal/dispatch/brake.go` (`apply`, nieuwe methode `freed`)
- Modify: `internal/dispatch/decide.go:194-208` (`planned`: veld `frees` plus commentaar bij
  `software` en `node`)
- Test: `internal/dispatch/brake_test.go`

**Interfaces:**
- Produces: `planned.frees bool` — de sluiting maakt de plek van haar run vrij; `planned.node` en
  `planned.software` zijn bij een sluiting de node en het softwareprofiel van de sluitende run.
- Produces: `func (b *Brake) freed(ps []planned) *Brake` — een kopie van `b` (met een eigen
  `Nodes`-map) waarin elke `p` met `p.frees` is afgeteld zoals `CountRun` hem telde: `node == ""`
  één van `InUse`, een node in `Nodes` één van die node, een node buiten `Nodes` niets;
  `p.software` één van `SoftwareInUse`. Geen teller zakt onder 0.

- [ ] **Stap 1: schrijf de falende tests in `brake_test.go`**

`TestBrakeFreedSlotAdmitsStart` — tabel met volle rem (`openBrake()` uit
`decide_merge_test.go`, dan per geval gezet) en `ps = []planned{close, start}`, waarbij
`close := planned{Action: Action{Kind: KindClose, Repo: "o/a", Issue: 1, RunID: 9}, frees: true, …}`
en `start := planned{Action: Action{Kind: KindStart, Repo: "o/b", Issue: 2}, …}`:

| geval | rem | close | start | verwacht `got[1].Kind` |
|---|---|---|---|---|
| host | `InUse: 4` | `node: ""` | `node: ""` | `KindStart` |
| software | `InUse: 3, SoftwareInUse: 2` | `software: true` | `software: true` | `KindStart` |
| node | `Nodes: {"node-a": {Name: "a", Slots: 1, InUse: 1}}` | `node: "node-a"` | `node: "node-a"` | `KindStart` |
| geen frees | `InUse: 4` | `frees: false` | `node: ""` | `KindDefer`, `Reason == "slots: 4 of 4 in use"` |
| andere node | `Nodes: {"node-a": {…InUse: 1}, "node-b": {Name: "b", Slots: 1, InUse: 1}}` | `node: "node-b"` | `node: "node-a"` | `KindDefer`, `Reason == "slots: 1 of 1 in use on a"` |
| node buiten `Nodes` | `Nodes: {"node-a": {…InUse: 1}}` | `node: "node-x"` | `node: "node-a"` | `KindDefer` |
| geen software vrij | `InUse: 3, SoftwareInUse: 2` | `software: false` | `software: true` | `KindDefer`, `Reason == "software: 2 of 2 in use"` |
| één plek, twee starts | `InUse: 4` | `node: ""` | `start` plus tweede start `o/c#3` | `got[1]` `KindStart`, `got[2]` `KindDefer` `"slots: 4 of 4 in use"` |

`TestBrakeFreedNeverBelowZero` — `b := openBrake()` met `InUse: 0, SoftwareInUse: 0`;
`ps` = één `frees`-sluiting (`software: true`) plus 5 starts (`software: false`). Verwacht: precies
4 starts door, de vijfde `KindDefer` `"slots: 4 of 4 in use"`.

`TestBrakeFreedKeepsMeasurement` — `b` met `Nodes: {"node-a": {Slots: 1, InUse: 1}}`, `InUse: 2`;
na `b.apply([]planned{closeOnNodeA, closeOnHost})` geldt nog `b.InUse == 2` en
`b.Nodes["node-a"].InUse == 1`.

- [ ] **Stap 2: draai de tests en zie ze falen**

Run: `go test -race ./internal/dispatch/ -run 'TestBrakeFreed'`
Verwacht: compileerfout `unknown field frees`.

- [ ] **Stap 3: implementeer**

Voeg `frees bool` toe aan `planned` (commentaar: de sluiting maakt de plek van haar run in deze
tick vrij, zie `Brake.freed`). Pas het commentaar van `software` en `node` aan: bij een start of
hervatting de run die komt, bij een sluiting de run die sluit. Schrijf `freed` naast `CountRun`.
In `apply`: `eff := b.freed(ps)` vóór de lus; de lus roept `eff.closed(...)` aan, `b.Paused` blijft.
Werk het doc-commentaar van `apply` bij: de starts toetsen tegen de rem na aftrek van de plekken
die de sluitingen in `ps` vrijmaken.

- [ ] **Stap 4: draai de tests**

Run: `go test -race ./internal/dispatch/ -run 'TestBrake'`
Verwacht: PASS. (Andere dispatchtests veranderen nog niet: geen enkele `planned` heeft al `frees`.)

- [ ] **Stap 5: mutatiecheck**

Laat `freed` tijdelijk `b` teruggeven: `TestBrakeFreedSlotAdmitsStart` faalt op host, software en
node. Haal de clamp weg: `TestBrakeFreedNeverBelowZero` faalt. Zet beide terug.

- [ ] **Stap 6: commit**

```bash
git add internal/dispatch/brake.go internal/dispatch/decide.go internal/dispatch/brake_test.go
git commit -m "feat(dispatch): rem telt plekken van sluitende runs af bij de starts (#359)"
```

### Taak 2: decide markeert welke sluiting een plek vrijmaakt

**Files:**
- Modify: `internal/dispatch/decide.go` (`decideIssue`, de settle-lus rond regel 252-266; nieuwe
  functie `freesSlot`)
- Test: nieuw bestand `internal/dispatch/decide_freed_test.go`
- Modify: fixtures in `internal/dispatch/testdata/decide/` alleen waar het nieuwe gedrag dat vraagt

**Interfaces:**
- Consumes: `planned.frees`, `planned.node`, `planned.software` uit taak 1.
- Produces: `func freesSlot(a Action) bool` — `KindClose`: `a.Outcome != runs.Parked`;
  `KindReturn`: `a.RunID != 0`; `KindDone`: `true`; elk ander soort (`KindMerge` inbegrepen):
  `false`.

In de settle-lus, voor elke `closes`: `p.node = row.Node`,
`p.software = row.Profile == ProfileSoftware`, `p.frees = f == nil && freesSlot(a)`. (`f != nil`
is de fix-pr-handoff van regel 13: die maakt nooit iets vrij.)

- [ ] **Stap 1: schrijf de falende tests in `decide_freed_test.go`**

Helper `withClosingRepo(t *testing.T, in *Input, row func(*runs.Row))`: laadt
`loadInput(t, "done-no-handoff")` (issue 7, open run 5, sluit als `return`/`reason_no_handoff`),
zet `Snapshot.Repo` en de `Repo` van zijn rijen op `"o/s"`, past `row(&History[0])` toe en voegt
de repo aan `in.Repos` toe.

`TestClosingRunFreesSlotSameTick` — tabel op basis van `loadInput(t, "brake-slots")` (o/r#7,
`in_use: 4`, verwacht nu `defer`):

| geval | aanpassing | verwachte kinds (`[]Kind` uit `Decide`) |
|---|---|---|
| host | `withClosingRepo` zonder wijziging | `[KindReturn, KindStart]` |
| software | basis `brake-software` (`in_use: 2`, `software_in_use: 2`, `max_software: 2`), sluitende run `Profile = ProfileSoftware` | `[KindReturn, KindStart]` |
| node | basis `brake-node-full` (`node-a`: 1 van 1), sluitende run `Node = "node-a"` | de `defer` van o/r#7 wordt een `start`; o/r#8 blijft `start` |
| geen vrije plek: node buiten `Nodes` | basis `brake-slots`, sluitende run `Node = "node-x"` | `[KindReturn, KindDefer]`, reden `"slots: 4 of 4 in use"` |
| geen vrije plek: geen software | basis `brake-software`, sluitende run `Profile = ""` | `[KindReturn, KindDefer]`, reden `"software: 2 of 2 in use"` |

`TestFreesSlot` — tabel voor `freesSlot`: `KindClose` met `runs.Parked` → false, met `runs.Done`,
`runs.Handoff`, `runs.Question`, `runs.StartFailed` → true; `KindReturn` met `RunID: 5` → true,
zonder → false; `KindDone` → true; `KindMerge` → false.

Helper `withWaitingRepo(t *testing.T, in *Input)`: laadt `loadInput(t, "brake-slots")`, zet
`Snapshot.Repo` op `"o/t"` en voegt zijn `Repos[0]` (issue 7 in `Controller`, geen run) aan
`in.Repos` toe; de rem van `brake-slots` gebruik je niet.

`TestFixPRCloseFreesNothing` — `settle-green-pr-done` met `red(&in)`, `in.Brake = openBrake()`,
`in.Brake.InUse = 4`, plus `withWaitingRepo`. Verwacht `summary(Decide(in))`:
`[fixClose12, "defer:slots: 4 of 4 in use:", "start:reason_pr_red:"]` (volgorde van
`comparePlanned`: groep 0, dan 1, dan 2). Criterium 3: de fix-pr-start erft de plek, o/t#7 krijgt
geen tweede.

- [ ] **Stap 2: draai de tests en zie ze falen**

Run: `go test -race ./internal/dispatch/ -run 'TestClosingRunFreesSlotSameTick|TestFreesSlot|TestFixPRCloseFreesNothing'`
Verwacht: compileerfout `undefined: freesSlot`.

- [ ] **Stap 3: implementeer `freesSlot` en de drie toewijzingen in de settle-lus**

- [ ] **Stap 4: draai de hele dispatch- en cli-suite**

Run: `go test -race ./internal/dispatch/ ./internal/cli/`
Verwacht: de nieuwe tests PASS. Een fixture in `TestDecideFixtures` die nu faalt, bekijk je per
geval: is de nieuwe uitkomst een start die door een vrijgekomen plek mag (spec, tabel "Welke
sluiting"), pas dan `want` aan en noteer de fixturenaam voor de PR. Elke andere afwijking is een
bug in taak 1 of 2.

- [ ] **Stap 5: mutatiecheck**

Zet `p.frees = freesSlot(a)` (zonder `f == nil`): `TestFixPRCloseFreesNothing` faalt. Laat
`freesSlot` `true` geven voor `runs.Parked`: `TestFreesSlot` faalt. Laat `p.node` weg:
het node-geval faalt. Zet alles terug.

- [ ] **Stap 6: commit**

```bash
git add internal/dispatch/decide.go internal/dispatch/decide_freed_test.go internal/dispatch/testdata/decide/
git commit -m "feat(dispatch): sluiting geeft haar plek al in dezelfde tick vrij (#359)"
```

### Taak 3: `docs/tick.md` en changelog

**Files:**
- Modify: `docs/tick.md:367-378` (de rem)
- Create: `changelog.d/issue-359-plek-vrij-zelfde-tick.md`

- [ ] **Stap 1: herschrijf de zin in `docs/tick.md`**

Vervang "Een run die in deze tick sluit, telt nog mee; de volgende tick ziet de plek vrij." door
tekst die zegt: een run die in deze tick sluit, geeft zijn plek (en bij profiel `software` zijn
software-plek, op een node de plek van die node) al aan de starts van deze tick. Drie sluitingen
geven niets vrij: een geparkeerde run (hij blijft in `in_use` tot zijn usage-hervatting), de
fix-pr-handoff van regel 13 (de fix-pr-start erft de plek) en een merge (een geweigerde merge laat
de run open). Mislukt de stop van een sessie, dan draait er één tick een run boven `slots` of
`max_software`; de volgende tick telt beide, laat geen start door en stopt de sessie via regel 4.
Controleer dat de alinea over de fix-pr-start (regel 373-378) er nog bij past.

- [ ] **Stap 2: changelog-fragment**

```markdown
---
type: changed
---
A slot that a closing run frees in a tick now goes to the starts of that same tick, for `slots`, `max_software` and node slots; a parked run, a fix-pr handoff and a merge free nothing until the next tick.
```

- [ ] **Stap 3: controleer links en tests**

Run: `go test -race ./internal/dispatch/ ./internal/cli/`
Verwacht: PASS.

- [ ] **Stap 4: commit**

```bash
git add docs/tick.md changelog.d/issue-359-plek-vrij-zelfde-tick.md
git commit -m "docs(tick): plek van een sluitende run telt al in dezelfde tick (#359)"
```
