# Een mislukte post laat de keten niet stilstaan (#395) — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `post-artefact` gebruikt geen GraphQL meer, en een mislukte handoff- of vraagpost van een controller-run laat een notitie achter die de controller zelf post of als reden teruggeeft.

**Architecture:** `lighthouse forge issue comment` en `lighthouse ask` schrijven bij een mislukte post `<state>/runs/<run>/post-failed.json`. De tick leest die notitie zoals `permission.json` (lokaal of via een node-route) in `RunState.PostFailed`. Een nieuwe decide-regel 14a, vóór regel 14, geeft een `KindRepost` (de controller post de body, de run blijft open) of een Teruggegeven met de fout.

**Tech Stack:** Go (cobra, huma), `gh api` in `internal/postartefact`.

**Spec:** [2026-11-20-mislukte-post-design.md](../specs/2026-11-20-mislukte-post-design.md)

## Global Constraints

- Code, namen, commentaar en foutmeldingen Engels; plan, commits, PR en comments Nederlands.
- Elke commit groen (`[test_gate] command`), nooit `--no-verify`. `/lighthouse-go-standard` en `/lighthouse-test-standard` gelden.
- Een wijziging onder `pack/` loopt ook `go run ./cmd/lighthouse init --upgrade`; de geïnstalleerde kopie gaat mee in dezelfde commit.
- Notitie: `<LIGHTHOUSE_STATE_DIR>/runs/<run>/post-failed.json`, velden in de volgorde `version`, `at`, `issue`, `command`, `error`, `body`; `version` 1; `at` in milliseconden UTC; `command` is `forge issue comment` of `ask`; `error` op één regel.
- Alleen in een controller-run: `LIGHTHOUSE_CONTROLLER_RUN` in uuid-vorm en `LIGHTHOUSE_STATE_DIR` niet leeg.
- Het schrijven van de notitie verandert nooit de exitcode of foutmelding van de mislukte post. Lukt het schrijven niet, dan één regel op stderr met `COULD NOT CHECK`.
- `nodeapi.Version` blijft 1. Een `404` zonder code op de nieuwe route is "geen notitie".
- Een mislukte `KindRepost` is geen apply-fout: één logregel, de tickstaat wordt bewaard, de run blijft open.
- Redenen: `reason_post_failed` (nl `post mislukt`, en `post failed`) met de fout als argument; `reason_post_failed_unreadable` (nl `post mislukt: notitie onleesbaar`, en `post failed: note unreadable`) zonder argument.
- De PR schrijft `Closes #395`. Eén fragment in `changelog.d/`.

## Review Focus

- **Een geslaagde herpost wordt niet herhaald.** De volgende tick ziet de Handoff en sluit met regel 2; regel 14a vuurt niet opnieuw. Test in Taak 5 (`postfailed-reposted-handoff-closes.json`).
- **Een notitie van een eerdere run in dezelfde sessie** (resume) telt niet: `at` vóór `started_at`. Test in Taak 5 (`postfailed-old-note.json`).
- **Een oude node zonder de route** maakt de run niet onmeetbaar. Test in Taak 4 (`TestNodeClientPostFailedOldNode`).
- **Een vraag van een andere sessie of een handoff voor een ander issue** wordt niet gepost maar teruggegeven. Test in Taak 5 (`postfailed-other-issue.json`).
- **De mislukte post blijft rood** ook als de notitie geschreven is. Test in Taak 3 (`TestForgeIssueCommentFailureWritesNote` checkt exitcode én notitie).

---

### Task 1: `openPR` via REST

**Files:**
- Modify: `internal/postartefact/postartefact.go:138-142`
- Test: `internal/postartefact/postartefact_test.go`

**Interfaces:**
- Consumes: niets.
- Produces: niets nieuws; `openPR(branch string) (bool, *runError)` houdt zijn signatuur.

- [ ] **Step 1: Pas de replay en voeg een test toe**

In de replay (`postartefact_test.go:98`) wordt de case `strings.HasPrefix(joined, "pr list")` vervangen door een case op `api repos/o/r/pulls?head=o:` die `0\n` geeft. Nieuwe test `TestOpenPRUsesREST`: een replay die elke `gh`-aanroep vastlegt; `openPR("issue-7-x")` geeft `true` bij stdout `1\n`, en de vastgelegde args zijn precies:

```go
[]string{"api", "repos/o/r/pulls?head=o:issue-7-x&state=open&per_page=1", "--jq", "length"}
```

Geen enkele vastgelegde aanroep begint met `pr` of `api graphql`.

- [ ] **Step 2: Draai de test, verwacht FAIL**

Run: `go test ./internal/postartefact/ -run 'TestOpenPRUsesREST|TestPostArtefact' -v`
Expected: FAIL op de args van `TestOpenPRUsesREST`.

- [ ] **Step 3: Implementeer**

`openPR` roept `p.gh(nil, "api", fmt.Sprintf("repos/%s/pulls?head=%s:%s&state=open&per_page=1", p.repo, owner, branch), "--jq", "length")`, met `owner` het deel van `p.repo` vóór de `/`. Escape `branch` met `url.QueryEscape`; `owner` en de `:` blijven letterlijk (de test gebruikt een branch zonder bijzondere tekens).

- [ ] **Step 4: Draai de tests, verwacht PASS**

Run: `go test -race ./internal/postartefact/ ./internal/cli/ -run 'PostArtefact|OpenPR' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/postartefact/
git commit -m "fix(postartefact): open PR via REST in plaats van gh pr list (#395)"
```

---

### Task 2: De notitie: schrijven, lezen, parsen

**Files:**
- Create: `internal/dispatch/postfailed.go`
- Test: `internal/dispatch/postfailed_test.go`

**Interfaces:**
- Produces:
  ```go
  type PostFailed struct {
      At         time.Time `json:"at"`
      Issue      int       `json:"issue"`
      Command    string    `json:"command"`
      Error      string    `json:"error"`
      Body       string    `json:"body"`
      Unreadable bool      `json:"unreadable,omitempty"` // the note did not parse; the other fields are zero
  }
  func PostFailedPath(stateDir, sessionID string) string          // <stateDir>/runs/<sessionID>/post-failed.json
  func WritePostFailed(stateDir, sessionID string, n PostFailed) error
  func ReadPostFailed(stateDir, sessionID string) (*PostFailed, error)
  func ParsePostFailed(data []byte) *PostFailed                   // never nil
  ```

- [ ] **Step 1: Schrijf de falende tests**

- `TestWritePostFailedFieldOrder`: `WritePostFailed(dir, "11111111-2222-4333-8444-555555555555", PostFailed{At: time.Date(2026, 11, 20, 21, 4, 5, 123e6, time.UTC), Issue: 121, Command: "forge issue comment", Error: "a\nb", Body: "🤖 Handoff\nissue: #121\n"})`; het bestand begint met `{"version":1,"at":"2026-11-20T21:04:05.123Z","issue":121,"command":"forge issue comment","error":"a; b","body":`, en heeft modus `0o600`.
- `TestWritePostFailedOverwrites`: twee keer schrijven; `ReadPostFailed` geeft de tweede.
- `TestReadPostFailedMissing`: geen bestand geeft `nil, nil`.
- `TestReadPostFailedUnreadable`: inhoud `{`, `{"version":2,"at":"2026-11-20T21:04:05.123Z"}` en `{"version":1}` geven elk `&PostFailed{Unreadable: true}, nil`.
- `TestPostFailedPathRefusesSession`: `ReadPostFailed(dir, "../x")` en `WritePostFailed(dir, "..", …)` geven een fout (`checkFileName`).

- [ ] **Step 2: Draai, verwacht FAIL** — `go test ./internal/dispatch/ -run PostFailed -v`, faalt op ontbrekende symbolen.

- [ ] **Step 3: Implementeer** in `internal/dispatch/postfailed.go`. `WritePostFailed` maakt `runs/<session>` aan met `0o700`, schrijft via een tijdelijk bestand plus `os.Rename`, en zet `error` op één regel met dezelfde vervanging als `oneLine` (`\n` wordt `; `). Een vaste struct met `json`-tags in contractvolgorde (`version` eerst) geeft de veldvolgorde. `ParsePostFailed` geeft `Unreadable` bij een JSON-fout, een `version` anders dan 1, of een nul-`at`.

- [ ] **Step 4: Draai, verwacht PASS** — `go test -race ./internal/dispatch/ -run PostFailed -v`.

- [ ] **Step 5: Commit**

```bash
git add internal/dispatch/postfailed.go internal/dispatch/postfailed_test.go
git commit -m "feat(dispatch): notitie post-failed.json voor een mislukte post (#395)"
```

---

### Task 3: `forge issue comment` en `ask` schrijven de notitie

**Files:**
- Create: `internal/cli/postfailed.go`
- Modify: `internal/cli/forgecmd.go:25-32` (`forgeEnv`), `internal/cli/forgecmd_write.go:161-191`, `internal/cli/ask.go:23-28, 62-182`
- Test: `internal/cli/postfailed_test.go`, bestaande `internal/cli/forgecmd_test.go` en `ask_test.go`

**Interfaces:**
- Consumes: `dispatch.WritePostFailed`, `dispatch.PostFailed` (Taak 2).
- Produces:
  ```go
  // notePostFailed writes the post-failed note of a controller run; outside one it does nothing.
  func notePostFailed(getenv func(string) string, now time.Time, stderr io.Writer, issue int, command, body string, postErr error)
  ```
  `forgeEnv` krijgt `getenv func(string) string` en `now func() time.Time` (nil is `os.Getenv` en `time.Now`). `askEnv` krijgt `Now func() time.Time` (nil is `time.Now`). `ask` krijgt een parameter `errOut io.Writer` na `out`.

- [ ] **Step 1: Schrijf de falende tests**

- `TestNotePostFailedOutsideControllerRun`: `getenv` zonder `LIGHTHOUSE_CONTROLLER_RUN`, of met waarde `abc`: er ontstaat geen bestand onder de state-dir, stderr leeg.
- `TestNotePostFailedCouldNotCheck`: `LIGHTHOUSE_STATE_DIR` wijst naar een bestand (geen map): stderr bevat `COULD NOT CHECK`.
- `TestForgeIssueCommentFailureWritesNote`: fake forge waarvan `CreateComment` faalt met `rate limit`; `lighthouse forge issue comment 121 --body-file f` in een controller-run geeft dezelfde exitcode en stderr als zonder notitie (exit 1), en `ReadPostFailed` geeft `Issue 121`, `Command "forge issue comment"`, `Body` gelijk aan de inhoud van `f`, `Error` met `rate limit`.
- `TestForgeIssueCommentOpenFailureWritesNote`: `env.open` faalt; notitie met de body van `f`.
- `TestForgeIssueCommentUsageErrorWritesNothing`: ontbrekend body-bestand of nummer `x`: geen notitie.
- `TestForgePRCommentFailureWritesNothing`: `forge pr comment` die faalt schrijft niets.
- `TestAskIssueFailureWritesEmptyBody`: `c.Issue` faalt; notitie met `Command "ask"` en `Body ""`.
- `TestAskPostFailureWritesBody`: `CreateComment` faalt; `Body` is de gerenderde vraag, en `protocol.ParseQuestion(Body)` lukt.
- `TestAskInvalidInputWritesNothing`: ongeldige velden of `--issue 0`: geen notitie. `--dry-run`: geen notitie.

- [ ] **Step 2: Draai, verwacht FAIL** — `go test ./internal/cli/ -run 'PostFailed|ForgeIssueComment|ForgePRComment|Ask' -v`.

- [ ] **Step 3: Implementeer**

`notePostFailed` controleert `LIGHTHOUSE_CONTROLLER_RUN` met `askSessionUUID` en `LIGHTHOUSE_STATE_DIR` niet leeg, en roept `dispatch.WritePostFailed`. Een schrijffout geeft één regel `lighthouse: post-failed note: COULD NOT CHECK — <fout>` op stderr. `newForgeCommentCommand` krijgt een laatste parameter `onFail func(cmd *cobra.Command, n int, body string, err error)`: na een fout van `env.write` roept RunE `onFail` aan (als die niet nil is) en geeft daarna dezelfde fout terug. De issue-variant geeft een `onFail` die `notePostFailed(…, "forge issue comment", body, err)` aanroept; de PR-variant geeft `nil`. In `ask` schrijft elke fout ná de checks op velden, sessie en `--issue` een notitie: config, opener, `c.Issue` (body `""`) en `CreateComment` (de gerenderde body). De read-back-check schrijft niets, en `--dry-run` ook niet. `RunE` geeft `cmd.ErrOrStderr()` mee als `errOut`.

- [ ] **Step 4: Draai, verwacht PASS** — `go test -race ./internal/cli/ -v -run 'PostFailed|ForgeIssueComment|ForgePRComment|Ask'`, daarna `go test -race ./internal/cli/`.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/
git commit -m "feat(cli): forge issue comment en ask noteren een mislukte post (#395)"
```

---

### Task 4: De tick leest de notitie, ook op een node

**Files:**
- Modify: `internal/nodeapi/nodeapi.go:28-29, 66-67`, `internal/nodeserve/statemap.go:48, 96-110`, `internal/workplace/kennforge/nodeclient.go:408-427`, `internal/workplace/kennforge/node.go:188` (naast `Permission`), `internal/workplace/workplace.go:128-131`, `internal/dispatch/runstate.go:17-22`, `internal/cli/tick.go:734-755`
- Test: `internal/nodeserve/statemap_test.go`, `internal/nodeserve/contract_test.go`, `internal/workplace/kennforge/nodeclient_test.go`, `internal/cli/tick_test.go` (of het bestand waar `runProbe.permission` getest wordt)

**Interfaces:**
- Consumes: `dispatch.PostFailedPath`, `dispatch.ReadPostFailed`, `dispatch.ParsePostFailed` (Taak 2).
- Produces:
  ```go
  // nodeapi
  PathPostFailed   = Prefix + "/runs/{session}/post-failed" // GET: the raw post-failed.json, 404 no_post_failed without one
  CodeNoPostFailed = "no_post_failed"
  // workplace
  PostFailedReader interface { PostFailed(ctx context.Context, sessionID string) ([]byte, error) }
  // kennforge
  func (c *NodeClient) PostFailed(ctx context.Context, session string) ([]byte, error)
  func (a *Adapter) PostFailed(ctx context.Context, sessionID string) ([]byte, error)
  // dispatch.RunState
  PostFailed *PostFailed `json:"post_failed,omitempty"`
  // cli
  func (m *runProbe) postFailed(ctx context.Context, r runs.OpenRun) (*dispatch.PostFailed, error)
  ```

- [ ] **Step 1: Schrijf de falende tests**

- nodeserve `TestPostFailedRoute`: met een notitie geeft de route de rauwe bytes; zonder notitie `404` met code `no_post_failed`; een onveilige sessie-id `400`. Neem de route op in de contracttest, zoals `get-permission`.
- nodeclient `TestNodeClientPostFailed`: `200` geeft de bytes, `404 no_post_failed` geeft `nil, nil`, `500` geeft een fout.
- nodeclient `TestNodeClientPostFailedOldNode`: `404` zonder code (een oude node) geeft `nil, nil`.
- tick: een lokale run met een notitie in de state-dir geeft `RunState.PostFailed` gevuld; een onleesbare notitie geeft `Unreadable: true` en geen fout; een node-run gaat via `PostFailedReader`.

- [ ] **Step 2: Draai, verwacht FAIL** — `go test ./internal/nodeserve/ ./internal/workplace/kennforge/ ./internal/cli/ -run 'PostFailed' -v`.

- [ ] **Step 3: Implementeer** volgens het patroon van `permission`, met twee verschillen: de client accepteert een `404` zonder code als `nil, nil`, en `runProbe.postFailed` parset met `ParsePostFailed` (nooit een fout op onleesbare inhoud). `measureRun` vult `s.PostFailed` direct na `s.Permission`. Een fout van de node (geen antwoord) blijft een fout, zoals bij permission.

- [ ] **Step 4: Draai, verwacht PASS** — `go test -race ./internal/nodeapi/... ./internal/nodeserve/... ./internal/workplace/... ./internal/cli/ ./internal/dispatch/`.

- [ ] **Step 5: Commit**

```bash
git add internal/nodeapi internal/nodeserve internal/workplace internal/dispatch/runstate.go internal/cli/tick.go internal/cli/*_test.go internal/nodeserve/*_test.go
git commit -m "feat(tick): lees post-failed.json lokaal en via een node-route (#395)"
```

---

### Task 5: Regel 14a in decide

**Files:**
- Modify: `internal/dispatch/decide.go` (`Kind`-constanten rond :63, `Action` rond :96, `outcome` :541-546, de lus in `decideIssue` :338-355), `pack/core/headings.toml` (na `[reason_no_handoff]`)
- Create: `internal/dispatch/testdata/decide/postfailed-*.json` (zes fixtures, zie Step 1)
- Test: `internal/dispatch/decide_test.go` (bestaande fixture-runner)

**Interfaces:**
- Consumes: `RunState.PostFailed` (Taak 4).
- Produces:
  ```go
  KindRepost Kind = "repost" // posts Body on Issue for the open run RunID, which stays open
  // Action
  Body string `json:"body,omitempty"` // repost: the comment, literal
  func (c runCtx) postFailed() (Action, bool)
  func repostable(body string, issue int, session string) bool
  ```

- [ ] **Step 1: Schrijf de falende fixtures**

Elk een kopie van `done-no-handoff.json` (run 5, issue 7, sessie `11111111-2222-4333-8444-555555555555`, `started_at` `2026-10-31T10:00:00Z`, `max_runtime` `8h`), met in `runs."5"` een `post_failed`:

| Fixture | `post_failed` / wijziging | `want` |
|---|---|---|
| `postfailed-repost-handoff.json` | `at` 11:50, body een Handoff met `issue: #7` | `{"kind":"repost","repo":"o/r","issue":7,"run_id":5,"body":<body>,"language":"nl"}` |
| `postfailed-repost-question.json` | body een Vraag `· #7 ` met footer van sessie `1111…5555` | `repost` met die body |
| `postfailed-other-issue.json` | Handoff met `issue: #8` | `return`, `reason":"reason_post_failed"`, `reason_arg` de `error` |
| `postfailed-unreadable.json` | `{"unreadable":true}` | `return`, `reason":"reason_post_failed_unreadable"` |
| `postfailed-old-note.json` | `at` 09:00 (vóór `started_at`) | `return`, `reason":"reason_no_handoff"` (gedrag van vandaag) |
| `postfailed-runtime-reached.json` | geldige Handoff, `now` 8h na `started_at` | `return`, `reason":"reason_post_failed"`, `reason_arg` de `error` |

Plus `postfailed-reposted-handoff-closes.json`: zelfde notitie, maar de geposte Handoff staat in de comments (na `started_at`); `want` is de `close` met `outcome":"handoff"` en `end":true`, zoals een gewone run met een Handoff. En `postfailed-empty-body.json`: `body` `""`: `return` met `reason_post_failed`.

- [ ] **Step 2: Draai, verwacht FAIL** — `go test ./internal/dispatch/ -run 'TestDecide' -v` faalt op de nieuwe fixtures (onbekend veld `post_failed` of andere `want`).

- [ ] **Step 3: Implementeer**

- `postFailed` geldt als `c.done() && !c.handoff && !anyPR` en `n := c.state.PostFailed` niet nil is en (`n.Unreadable` of `!n.At.Before(c.row.StartedAt)`). Uitkomsten, in deze volgorde: `Unreadable` geeft `c.returned("reason_post_failed_unreadable", "")`. Is `repostable(n.Body, c.it.Issue, c.row.SessionID)` en (`MaxRuntime` is 0 of `c.age() < MaxRuntime`), dan `KindRepost` op `c.base` met `RunID` en `Body`. Anders `c.returned("reason_post_failed", n.Error)`.
- `repostable`: `protocol.ParseHeader(body)`. Een Handoff: `protocol.ParseHandoff(h)` lukt en `Issue == issue`. Een Vraag: `protocol.ParseQuestion(body)` lukt, `h.Session == session`, en de eerste regel bevat `fmt.Sprintf(" · #%d ", issue)`. Al het andere: `false`.
- In `outcome` komt `noStart(c.postFailed)` direct vóór `noStart(c.noHandoff)`.
- In de lus van `decideIssue`: een actie met `Kind == KindRepost` gaat als `planned{Action: a}` mee en zet `free = false`. Ze telt niet als close (geen `frees`, geen `fixReason`).
- `headings.toml`: `[reason_post_failed]` nl `post mislukt`, en `post failed`; `[reason_post_failed_unreadable]` nl `post mislukt: notitie onleesbaar`, en `post failed: note unreadable`. Daarna `go run ./cmd/lighthouse init --upgrade`, zodat `.lighthouse/headings.toml` meekomt.

- [ ] **Step 4: Draai, verwacht PASS** — `go test -race ./internal/dispatch/ ./internal/protocol/ ./internal/packs/`, en `go run ./cmd/lighthouse init --upgrade` gevolgd door `go run ./cmd/lighthouse init --upgrade` geeft `No changes.`

- [ ] **Step 5: Commit**

```bash
git add internal/dispatch/ pack/core/headings.toml .lighthouse/
git commit -m "feat(dispatch): regel 14a post een gestrande handoff of vraag zelf (#395)"
```

---

### Task 6: Apply voert `KindRepost` uit

**Files:**
- Modify: `internal/dispatch/apply.go` (de `switch` rond :134)
- Test: `internal/dispatch/apply_test.go`

**Interfaces:**
- Consumes: `KindRepost`, `Action.Body` (Taak 5).
- Produces: niets nieuws.

- [ ] **Step 1: Schrijf de falende tests**

- `TestApplyRepostPostsBody`: een `KindRepost`-actie post `a.Body` letterlijk op `a.Issue` van `a.Repo`, en sluit geen run (de runs-DB heeft run 5 nog open, zonder uitkomst).
- `TestApplyRepostFailureIsNoError`: `CreateComment` faalt; `Apply` geeft `nil` terug, `res.Failed` heeft geen sleutel voor de repo, en de log bevat één regel met `repost`, `o/r#7`, `run 5` en de fout.

- [ ] **Step 2: Draai, verwacht FAIL** — `go test ./internal/dispatch/ -run TestApplyRepost -v`.

- [ ] **Step 3: Implementeer** een `case KindRepost` die `ap.env.Forge(a.Repo).CreateComment(ctx, a.Issue, a.Body)` aanroept. Bij een fout volgt `ap.logf("lighthouse: repost %s run %d: %s", subject(a), a.RunID, oneLine(err.Error()))` en dan `return nil`.

- [ ] **Step 4: Draai, verwacht PASS** — `go test -race ./internal/dispatch/ ./internal/cli/`.

- [ ] **Step 5: Commit**

```bash
git add internal/dispatch/apply.go internal/dispatch/apply_test.go
git commit -m "feat(dispatch): apply post de body van een repost, een fout blijft een logregel (#395)"
```

---

### Task 7: Docs, skill en changelog

**Files:**
- Modify: `docs/tick.md` (de regeltabel :287-309, de alinea's erna, en waar `permission.json` beschreven staat), `pack/core/skills/write-handoff/SKILL.md:276-277`
- Create: `changelog.d/issue-395-mislukte-post.md`
- Generated: de geïnstalleerde kopie van write-handoff onder `.claude/skills/` via `init --upgrade`

**Interfaces:** geen.

- [ ] **Step 1: `docs/tick.md`**

- Regel 14a in de tabel, tussen 13c en 14: *Klaar, geen PR, geen Handoff of Vraag, wel een `post-failed.json` van op of na `started_at` (of een onleesbare)*, met de vier uitkomsten uit de tabel van de spec en de redenen *post mislukt: <fout>* en *post mislukt: notitie onleesbaar*.
- Een alinea over `post-failed.json`: wie schrijft (`forge issue comment` en `ask`, alleen in een controller-run), wanneer (elke fout nadat de bedoeling vaststaat), waar, dat alleen de nieuwste telt, de node-route en dat een oude node "geen notitie" geeft.
- Een alinea over de keuze: A+ boven A (alleen een reden) en B (transcript), met de reden uit *Aanpakken* van de spec. Daarbij: een mislukte repost is een logregel, geen tickfout, en `max_runtime` is de grens.
- In de alinea van Apply: `repost` post de body en sluit niets.

- [ ] **Step 2: Skill**

In stap 5 van `pack/core/skills/write-handoff/SKILL.md`, na "The post runs only when the write succeeded; stop if it fails.", komt: "In a controller run, `lighthouse forge issue comment` notes a failed post for the controller itself (`post-failed.json`), which posts it later; stop all the same." Daarna `go run ./cmd/lighthouse init --upgrade`.

- [ ] **Step 3: Changelog**

```markdown
---
type: fixed
---
A controller run whose handoff or question post fails no longer stalls the chain: the run leaves a `post-failed.json` note, and the controller posts the comment itself or hands the issue back with the error. `lighthouse post-artefact` no longer uses GraphQL.
```

- [ ] **Step 4: Verifieer**

Run: `go run ./cmd/lighthouse init --upgrade && go run ./cmd/lighthouse init --upgrade` → de tweede geeft `No changes.`; `go test -race ./internal/dispatch/ ./internal/packs/` → PASS (de tests die skilltekst vastpinnen, zoals `internal/dispatch/instructions_test.go`, blijven groen of gaan mee).

- [ ] **Step 5: Commit**

```bash
git add docs/tick.md pack/core/skills/write-handoff/SKILL.md .claude/ changelog.d/issue-395-mislukte-post.md
git commit -m "docs(tick): regel 14a en de notitie post-failed.json (#395)"
```
