# De tick merget een groene eigen PR, of start fix-pr (#294) — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** De tick squash-merget een groene eigen PR van een klare run in `apply`. Bij een conflict,
een onverwerkte roborev-review of een mislukte merge start hij `/lighthouse-fix-pr` met de poorten
van #119 en #296 (#294, neemt #295 mee).

**Architecture:** `gather` meet de roborev-stand van een mergeklare PR (`forge.PR.Roborev`). Decide
krijgt de afloop-regel `mergeReady` (actie `KindMerge`), een smallere `green` en `fixPR` met vier
oorzaken in plaats van `prRed`. `apply` merget via `forge.Client.MergePR` met de gemeten SHA. Een
weigering noteert `apply` op de open run-rij (`merge_sha`, `merge_error`), en de volgende tick leest
die notitie als oorzaak voor fix-pr.

**Tech Stack:** Go (`internal/dispatch`, `internal/forge`, `internal/runs`, `internal/store`), SQLite-migratie,
`pack/core` (headings, fix-pr-skill), Markdown-docs.

**Spec:** [`2026-11-08-issue-294-tick-merget-design.md`](../specs/2026-11-08-issue-294-tick-merget-design.md)

## Global Constraints

- Decide blijft puur. De merge zit in `apply`. Elke fix-pr-start komt uit decide, zodat de rem (#296) erover beslist.
- Sleutel `fixpr:<pr>:<sha>` en plafond `maxFixPRRuns` (3) blijven. De oorzaak verandert de sleutel niet.
- Alleen squash (`"merge_method": "squash"`), altijd met `"sha": <gemeten head>`. Geen commit-titel, geen branch-opruiming.
- Code, comments en skilltekst in het Engels. Docs, headings-`nl`, changelog en commitberichten in het Nederlands.
- Migratie heet `internal/store/migrations/0006_runs_merge.sql`.
- Elke commit die `pack/` raakt, draait eerst `go run ./cmd/lighthouse init --upgrade` en neemt de geïnstalleerde kopie mee. Een tweede run moet `No changes.` geven.
- Go volgt `/lighthouse-go-standard`. Tests volgen `/lighthouse-test-standard`: elke test faalt als zijn regel wegvalt.
- Roborev-waarden zijn precies `""`, `"processed"` en `"open"`.

## Beslissingen die dit plan neemt

1. **`MergePR` hoort bij `Poster`.** `apply` schrijft al via `Env.Poster(repo)`, en `*forge.Client` implementeert die interface. `Poster` krijgt `MergePR(ctx, number int, sha string) error`. Er komt geen nieuw veld in `Env`.
2. **De rate-limit-headers gaan mee in `httpStatusError`.** `statusError` krijgt de velden `Message string` (de `message` van GitHub) en `RateLimited bool`. `RateLimited` is waar bij `x-ratelimit-remaining: 0`, bij een niet-lege `retry-after`, of als "rate limit" in de message staat (hoofdletters tellen niet). `MergePR` gebruikt gewoon `do`.
3. **Roborev-constanten staan in `forge`**, naast `PR`: `RoborevProcessed = "processed"`, `RoborevOpen = "open"`.
4. **De fix-pr-reden** gaat op twee plekken mee:
   - de start-actie draagt `Reason` en `ReasonArg`;
   - de close krijgt in `planned` de velden `fixReason` en `fixReasonArg`.

   Is `fixReason` leeg (een close van buiten decideIssue, in tests), dan blijft het `reason_pr_red`. `returnFixPR` gebruikt dezelfde terugval.
5. **`activeBlockers` wordt een vrije functie** `activeBlockers(it Item) []string`. `issueCtx.activeBlockers` roept die aan, en `runCtx.mergeReady` gebruikt hem ook.
6. **Decide-tests van de nieuwe regels** zijn tabeltests op de invoer van `testdata/decide/settle-green-pr-done.json`, met per geval een mutatie. Voor elk geval een eigen fixture zou ruim twintig bijna gelijke JSON-bestanden geven. Voor de positieve merge en de groene vreemde PR komen wel fixtures, zodat `TestDecideKeysAreHeadings` hun sleutels ziet.

## Review Focus

- **Een verouderde marker.** Een upsert van paia heeft de review naar een nieuwe SHA bijgewerkt, en de enige marker noemt de oude SHA. Verwacht: `open`, dus fix-pr en geen merge. Test: `TestRoborevStaleMarkerStaysOpen` (Taak 3).
- **Kosten van gather.** Een PR die niet mergeklaar is, of een item zonder open run, kost geen comments-call. Test: `TestCompleteReadsPRCommentsOnlyWhenMergeReady` (Taak 3) telt de requests.
- **Na een push telt de oude merge-fout niet meer.** `MergeSHA` is een oude head en de PR heeft een nieuwe head. Verwacht: de tick merget opnieuw, zonder fix-pr. Test: geval `notitie op oude head` in `TestMergeReady` (Taak 4).
- **De merge lukt, de Klaar-post faalt.** Verwacht: de actie faalt, er komt geen notitie, en de rij en de claude blijven. De volgende tick sluit de run via `merged`. Test: `TestApplyMergePostFailsKeepsRowOpen` (Taak 5).
- **Twee open eigen PR's**, #12 `CONFLICTING` en #14 groen. De eerste op nummer beslist: fix-pr op #12, geen merge van #14. Test: geval `twee eigen PR's, eerste conflict` in `TestMergeReady` (Taak 4).

---

### Taak 1: Merge-notitie op de run-rij

**Files:**
- Create: `internal/store/migrations/0006_runs_merge.sql`
- Modify: `internal/runs/history.go` (`Row`, `rowColumns`, `scanRow`), `internal/runs/runs.go` (`OpenRun`, `openColumns`, `scanOpen`, nieuwe `NoteMergeFailed`), `internal/dispatch/fingerprint.go` (`fpRun`)
- Test: `internal/store/migrate_test.go`, `internal/runs/runs_test.go`, `internal/dispatch/fingerprint_test.go`

**Interfaces:**
- Produces:
  - `runs.Row.MergeSHA`, `runs.Row.MergeError string`, zonder JSON-tags, zoals de andere velden van `Row`;
  - `runs.OpenRun.MergeSHA string` met tag `json:"merge_sha,omitempty"`, en `runs.OpenRun.MergeError string` met tag `json:"merge_error,omitempty"`;
  - `func NoteMergeFailed(ctx context.Context, db *sql.DB, id int64, sha, message string) error`. Op een gesloten of onbekende rij geeft hij `ErrNotOpen`.

- [ ] **Stap 1: Schrijf de falende tests**
  - `TestMigrate0006KeepsRuns` in `migrate_test.go`, naar het model van `TestMigrate0005KeepsRuns`. Migreer met `embeddedUpTo` tot en met `0005_workspace_removed.sql`, en voeg rij 41 in. Migreer daarna naar `migrationFS`. Verwacht:
    - `user_version` is 6;
    - `merge_sha` en `merge_error` van rij 41 zijn `""`;
    - de andere kolommen zijn ongewijzigd.
  - Pas `TestMigrate0005KeepsRuns` aan: hij migreert naar `embeddedUpTo(... "0005_workspace_removed.sql")` en verwacht nog steeds 5. Pas de `want 5` op regel 81 aan naar 6.
  - `TestNoteMergeFailed` in `runs_test.go`:
    - na `Take` geeft `NoteMergeFailed(ctx, db, id, "abc1234", "Pull Request is not mergeable")` nil;
    - `Open` geeft `MergeSHA == "abc1234"` en `MergeError == "Pull Request is not mergeable"`, en `History` ook;
    - na `Release` geeft een tweede `NoteMergeFailed` `errors.Is(err, ErrNotOpen)`;
    - de waarden op de gesloten rij veranderen niet.
  - `TestFingerprintChangesOnMergeSHA` in `fingerprint_test.go`: twee `Pre`'s met dezelfde open run. Bij de ene is `MergeSHA` leeg, bij de andere `"abc1234"`. Hun fingerprints verschillen. Een lege `MergeSHA` geeft dezelfde hash als vóór deze wijziging: `fpRun.MergeSHA` krijgt `json:"merge_sha,omitempty"`.

- [ ] **Stap 2: Run de tests en zie ze falen**
  Run: `go test ./internal/store ./internal/runs ./internal/dispatch -run 'Migrate|NoteMergeFailed|FingerprintChangesOnMergeSHA'`
  Verwacht: FAIL (compileerfout op `NoteMergeFailed`/`MergeSHA`, en `user_version` 5).

- [ ] **Stap 3: Implementeer**
  - Migratie, letterlijk uit de spec:
    ```sql
    ALTER TABLE runs ADD COLUMN merge_sha TEXT NOT NULL DEFAULT '';
    ALTER TABLE runs ADD COLUMN merge_error TEXT NOT NULL DEFAULT '';
    ```
  - Zet `merge_sha, merge_error` achteraan in `rowColumns` en `openColumns`, en voeg ze toe aan beide scans.
  - `NoteMergeFailed` gebruikt `setOpen` met `UPDATE runs SET merge_sha = ?, merge_error = ? WHERE id = ? AND ended_at IS NULL`.
  - `fpRun` krijgt `MergeSHA string \`json:"merge_sha,omitempty"\``, gevuld uit `r.MergeSHA`.

- [ ] **Stap 4: Run de tests en zie ze slagen**
  Run: `go test -race ./internal/store ./internal/runs ./internal/dispatch`
  Verwacht: PASS.

- [ ] **Stap 5: Commit**
  ```bash
  git add internal/store internal/runs internal/dispatch/fingerprint.go internal/dispatch/fingerprint_test.go
  git commit -m "feat(runs): noteer een geweigerde merge op de open run (#294)"
  ```

---

### Taak 2: `MergePR` en de roborev-stand in `forge`

**Files:**
- Modify: `internal/forge/board.go` (`PR` krijgt `Roborev`, plus de constanten), `internal/forge/github.go` (packagecommentaar, `statusError`, `httpStatusError`), `internal/forge/write.go` (`MergePR`, `ErrHeadMoved`, `MergeRefused`)
- Test: `internal/forge/write_test.go`, `internal/forge/github_test.go`

**Interfaces:**
- Produces:
  - `forge.PR.Roborev string`, met tag `json:"roborev,omitempty"`;
  - `const RoborevProcessed = "processed"` en `const RoborevOpen = "open"`;
  - `var ErrHeadMoved = errors.New("forge: head moved")`;
  - `type MergeRefused struct{ Message string }`, met `Error()` = `"forge: merge refused: " + Message`;
  - `func (c *Client) MergePR(ctx context.Context, number int, sha string) error`.

- [ ] **Stap 1: Schrijf de falende tests** in `write_test.go`. Maak een helper `mergeServer(t, status int, header map[string]string, resp string) *Client`. Die controleert:
  - methode `PUT` en pad `/repos/o/r/pulls/12/merge`;
  - de JSON-body, gedecodeerd naar `map[string]string`, is `{"sha": "abc1234", "merge_method": "squash"}`;
  - de vaste headers, zoals `writeServer`.

  Daarna zet hij `header` op de response. Tabeltest `TestMergePR`, met per geval status, headers, body en het verwachte resultaat:

  | Geval | Response | Verwacht |
  |---|---|---|
  | merged | 200 `{"merged":true,"message":"Pull Request successfully merged"}` | `nil` |
  | 200 niet gemerged | 200 `{"merged":false,"message":"x"}` | `*MergeRefused{Message:"x"}` |
  | head veranderd | 409 `{"message":"Head branch was modified. Review and try the merge again."}` | `errors.Is(err, ErrHeadMoved)` |
  | base veranderd | 405 `{"message":"Base branch was modified. Review and try the merge again."}` | `ErrHeadMoved` |
  | niet mergeable | 405 `{"message":"Pull Request is not mergeable"}` | `*MergeRefused{Message:"Pull Request is not mergeable"}` |
  | 403 zonder limiet | 403 `{"message":"Resource not accessible by personal access token"}` | `*MergeRefused` met die message |
  | 403 rate limit (header) | 403, `x-ratelimit-remaining: 0` | geen `MergeRefused`, geen `ErrHeadMoved`, wel een fout |
  | 403 rate limit (retry-after) | 403, `retry-after: 60` | idem |
  | 403 rate limit (message) | 403 `{"message":"API rate limit exceeded for user"}` | idem |
  | 422 | 422 `{"message":"Validation failed"}` | idem |
  | 500 | 500 | idem |

  Een extra test `TestStatusErrorKeepsMessage` in `github_test.go`: een 404 met message `Not Found` geeft via `errors.As` een `*httpStatusError` met `Code == 404`, `Message == "Not Found"` en `RateLimited == false`. Vul `TestErrorIncludesGitHubMessage` niet aan; die controleert de tekst al.

- [ ] **Stap 2: Run de tests en zie ze falen**
  Run: `go test ./internal/forge -run 'MergePR|StatusErrorKeepsMessage'`
  Verwacht: FAIL (compileerfout).

- [ ] **Stap 3: Implementeer**
  - `httpStatusError` krijgt de velden `Message` en `RateLimited`. `statusError` vult ze uit de body en uit `resp.Header` (zie beslissing 2). `Error()` blijft gelijk.
  - `MergePR` doet `c.do(ctx, http.MethodPut, url, body, &out)`, met `out` = `{Merged bool; Message string}`. Daarna, in deze volgorde:
    1. gelukt en `!out.Merged`: `&MergeRefused{out.Message}`;
    2. een `*httpStatusError` met code 409: `ErrHeadMoved`;
    3. code 405 en een message met `Base branch was modified`: `ErrHeadMoved`;
    4. elke andere 405: `&MergeRefused{e.Message}`;
    5. code 403 en `!e.RateLimited`: `&MergeRefused{e.Message}`;
    6. anders de fout van `do`, ongewijzigd.
  - Het packagecommentaar in `github.go` wordt: `Package forge reads and writes GitHub issues, comments, reactions and the Projects v2 board, and squash-merges a pull request at a measured head.`

- [ ] **Stap 4: Run de tests en zie ze slagen**
  Run: `go test -race ./internal/forge`
  Verwacht: PASS.

- [ ] **Stap 5: Commit**
  ```bash
  git add internal/forge
  git commit -m "feat(forge): squash-merge een PR op de gemeten head (#294)"
  ```

---

### Taak 3: Roborev-stand meten in `Complete`

**Files:**
- Create: `internal/dispatch/roborev.go`
- Modify: `internal/dispatch/gather.go` (`Complete`)
- Test: `internal/dispatch/roborev_test.go` (nieuw), `internal/dispatch/gather_test.go`

**Interfaces:**
- Consumes: `forge.PR.Roborev`, `forge.RoborevOpen` en `forge.RoborevProcessed` (Taak 2), `(*Trust).Trusted`.
- Produces:
  - `func mergeCandidate(pr forge.PR) bool`: `OPEN`, geen draft, `MERGEABLE` en `SUCCESS`;
  - `func roborevState(ctx context.Context, comments []protocol.Comment, trust *Trust) (string, error)`.

- [ ] **Stap 1: Schrijf de falende tests** in `roborev_test.go`, als tabeltest `TestRoborevState`.
  - Gebruik de `client(t)` van `trust_test.go`, met `srv.Roles = {"owner": "admin", "stranger": "read"}` en `NewTrust(c, defaultAllowed)`.
  - `review(sha, verdict)` is de body: `"<!-- roborev-pr-comment -->\n## roborev: Review Complete (`" + sha + "`)\n\n" + verdict + "\n\n### 1. high\n- a.go:3: x"`.
  - De bot is `github-actions[bot]`.

  | Geval | Comments (auteur: body) | Verwacht |
  |---|---|---|
  | geen review | owner: `hallo` | `""` |
  | review zonder marker | bot: `review("abc1234def", "Changes require fixes")` | `"open"` |
  | `github-actions` telt ook | `github-actions`: idem | `"open"` |
  | review met marker | bot: review; owner: `<!-- roborev-verwerkt: abc1234def -->\n…` | `"processed"` |
  | korte marker (prefix) | bot: review; owner: marker `abc1234` | `"processed"` |
  | marker korter dan 7 | bot: review; owner: marker `abc123` | `"open"` |
  | lege marker | bot: review; owner: `<!-- roborev-verwerkt:  -->` | `"open"` |
  | marker van onvertrouwde auteur | bot: review; stranger: marker `abc1234def` | `"open"` |
  | review van andere auteur | owner: `review(...)` | `""` |
  | `Combined Review` | bot: idem met kop `Combined Review` | `"open"` |
  | `Review Passed` | bot: kop `Review Passed`, met `Changes require fixes` in de body | `""` |
  | ander oordeel | bot: `review(sha, "Looks good")` | `""` |
  | laatste review telt | bot: review `aaa1111`; owner: marker `aaa1111`; bot: review `bbb2222` | `"open"` (`TestRoborevStaleMarkerStaysOpen` als eigen test) |

  Een rolvraag die faalt (`srv.RoleStatus = 500`) bij een marker geeft een fout terug.

  In `gather_test.go`, `TestCompleteMeasuresRoborev`:
  - `Pre` met issue 7, `OpenRuns: []runs.OpenRun{{ID: 5, Issue: 7}}`;
  - PR #12 mergeklaar, PR #13 met checks `PENDING`;
  - `srv.Comments[12]` bevat een bot-review zonder marker.

  Verwacht: `snap.Items[0].PRs` heeft voor #12 `Roborev == "open"` en voor #13 `""`. De `Pre` blijft onaangeroerd: `pre.Items[0].PRs[0].Roborev == ""`.

  `TestCompleteReadsPRCommentsOnlyWhenMergeReady`: drie gevallen geven elk `srv.Count("/repos/o/r/issues/12/comments") == 0`:
  - geen open run;
  - PR `CONFLICTING`;
  - PR draft.

- [ ] **Stap 2: Run de tests en zie ze falen**
  Run: `go test ./internal/dispatch -run 'Roborev|CompleteMeasuresRoborev|CompleteReadsPRComments'`
  Verwacht: FAIL (compileerfout).

- [ ] **Stap 3: Implementeer** in `roborev.go`:
  - regex voor de kop, zoals `HEADING` in `pack/core/scripts/roborev_gate.py:204`:
    ``(?m)^## roborev: (.+?) \(`([^`]+)`\)\s*$``. De eerste match telt;
  - regex voor de marker: `<!-- roborev-verwerkt: ([^ >]*) -->`, alle matches;
  - de review is de laatste comment van `github-actions[bot]` of `github-actions` met `roborev-pr-comment` in de body. Hij telt alleen bij kind `Review Complete` of `Combined Review` en met `Changes require fixes` in de body. Anders `""`;
  - `sameSHA(a, b string) bool`: beide `^[0-9a-f]{7,}$`, en de een is een prefix van de ander;
  - `Trusted` alleen vragen voor een comment met een marker waarvan `sameSHA` geldt. Zo kost een gewone comment geen rolvraag.

  In `Complete`: bepaal `hasRun` uit `pre.OpenRuns`. Voor een item met een open run: kloon `it.PRs` (`slices.Clone`, zodat `Pre` niet verandert). Doe voor elke PR met `mergeCandidate`:
  - `c.Comments(ctx, pr.Number)`;
  - daarna `roborevState`.

  Elke fout laat `Complete` falen. Vul het doc-commentaar van `Complete` aan met één zin over deze meting.

- [ ] **Stap 4: Run de tests en zie ze slagen**
  Run: `go test -race ./internal/dispatch`
  Verwacht: PASS.

- [ ] **Stap 5: Commit**
  ```bash
  git add internal/dispatch/roborev.go internal/dispatch/roborev_test.go internal/dispatch/gather.go internal/dispatch/gather_test.go
  git commit -m "feat(dispatch): gather meet de roborev-stand van een mergeklare PR (#294)"
  ```

---

### Taak 4: De afloop-regels in decide: `fixPR`, `mergeReady` en een smallere `green`

**Files:**
- Modify: `internal/dispatch/decide.go` (`prRed` wordt `fixPR`, `fixPRStart`, `planned`, `decideIssue`, `settleUnstartedFixPR`, `KindMerge`, `Action.PR`/`HeadSHA`, `mergeReady`, `green`, `outcome`, `activeBlockers`, en de commentaren van `Decide` en `settle`), `pack/core/headings.toml`
- Modify fixtures:
  - `testdata/decide/settle-red-pr.json`, `fixpr-own-after-foreign.json` en `fixpr-ceiling-start-failed.json`: de start in `want` krijgt `"reason": "reason_pr_red"`;
  - `testdata/decide/settle-green-pr-done.json`: `want` wordt de merge-actie hieronder;
  - `testdata/decide/settle-handoff-end-green-pr.json`: idem, met de `session_id` en `run_id` van die fixture.
- Create fixtures:
  - `testdata/decide/fixpr-foreign-conflict.json`;
  - `testdata/decide/settle-green-foreign-pr-done.json`: een kopie van `settle-green-pr-done.json`, met PR #12 op `head_ref "other-branch"`. `want` is de oude `done`-actie;
  - `testdata/decide/closecheck-after-tick-merge.json` (zie Stap 2).
- Test: `internal/dispatch/decide_merge_test.go` (nieuw), `internal/dispatch/decide_test.go`

**Interfaces:**
- Consumes:
  - `runs.Row.MergeSHA` en `MergeError` (Taak 1);
  - `forge.PR.Roborev` en `forge.RoborevOpen` (Taak 2).
- Produces:
  - `type fixPRStart struct{ pr forge.PR; key, reason, arg string }`;
  - `planned.fixReason` en `planned.fixReasonArg string`;
  - `func (c runCtx) ownOpenPR() (forge.PR, bool)`: de eerste PR op nummer met `State == "OPEN"` en `c.ownPR(pr)`. `Item.PRs` is al gesorteerd;
  - `func (c runCtx) fixPR() (Action, bool, *fixPRStart)`;
  - `const KindMerge Kind = "merge"`;
  - `Action.PR int` met tag `json:"pr,omitempty"`, en `Action.HeadSHA string` met tag `json:"head_sha,omitempty"`;
  - `func (c runCtx) mergeReady() (Action, bool)`;
  - `func activeBlockers(it Item) []string`;
  - de testhelpers `loadInput(t, name string) Input` (decodeert het fixture-bestand bij elke aanroep opnieuw) en `summary(got []Action) []string`.

  De merge-actie: `{Kind: merge, Repo, Issue, RunID: row.ID, SessionID: row.SessionID, PR: pr.Number, HeadSHA: pr.HeadSHA, PRURL: pr.URL, Language, End: true}`.

  Waarom één taak: zolang `green` elke groene PR afsluit, zijn de oorzaken merge en roborev (op een groene PR) niet te testen. En de tabel van `mergeReady` verwacht fix-pr-uitkomsten. Elke splitsing laat een commit achter met rode tests of een run die nergens meer eindigt.

- [ ] **Stap 1: Headings.** Voeg aan `pack/core/headings.toml` toe, onder `reason_pr_red_ceiling`:

  | Sleutel | nl | en |
  |---|---|---|
  | `reason_pr_conflict` | PR heeft een merge-conflict | PR has a merge conflict |
  | `reason_pr_conflict_ceiling` | PR heeft een merge-conflict na 3 fix-pr-runs | PR has a merge conflict after 3 fix-pr runs |
  | `reason_pr_roborev` | PR heeft een onverwerkte roborev-review | PR has an unprocessed roborev review |
  | `reason_pr_roborev_ceiling` | onverwerkte roborev-review na 3 fix-pr-runs | unprocessed roborev review after 3 fix-pr runs |
  | `reason_merge_failed` | merge mislukt | merge failed |
  | `reason_merge_failed_ceiling` | merge mislukt na 3 fix-pr-runs | merge failed after 3 fix-pr runs |

  En onder `note_done` de sleutel `note_merged`:
  - nl: `Checks groen, door de controller gesquasht en gemerged. Staat het issue nog open, dan toetst de controller het opnieuw.`
  - en: `Checks green, squashed and merged by the controller. If the issue is still open, the controller checks it again.`

- [ ] **Stap 2: Schrijf de falende tests** in `decide_merge_test.go`.
  - `loadInput(t, "settle-green-pr-done")` is de basis: run 5 klaar (stand 2), eigen PR #12 groen op `abc123`, item in `Controller`, geen rem.
  - `summary(got []Action) []string` geeft per actie `kind + ":" + reason + ":" + reason_arg`.

  Tabeltest `TestFixPRCauses`. Elk geval muteert PR #12 of rij 5:

  | Geval | Mutatie | Verwacht `summary` |
  |---|---|---|
  | rood | `Checks = "FAILURE"` | `close::fixpr:12:abc123`, `start:reason_pr_red:` |
  | conflict | `Mergeable = "CONFLICTING"` | `close::fixpr:12:abc123`, `start:reason_pr_conflict:` |
  | merge mislukt | rij: `MergeSHA = "abc123"`, `MergeError = "Pull Request is not mergeable"` | `close::fixpr:12:abc123`, `start:reason_merge_failed:Pull Request is not mergeable` |
  | roborev | `Roborev = "open"` | `close::fixpr:12:abc123`, `start:reason_pr_roborev:` |
  | rood wint van conflict | `Checks = "FAILURE"`, `Mergeable = "CONFLICTING"` | `…`, `start:reason_pr_red:` |
  | `UNKNOWN` | `Mergeable = "UNKNOWN"` | leeg |
  | conflict bij plafond | conflict, plus 3 gesloten rijen met `Command "/lighthouse-fix-pr 12"` | `return:reason_pr_conflict_ceiling:` |
  | merge bij gebruikte SHA | merge-notitie, plus een gesloten rij met `TriggerKey "fixpr:12:abc123"` | `return:reason_merge_failed:Pull Request is not mergeable` |
  | roborev bij plafond | roborev open, plus 3 fix-pr-rijen | `return:reason_pr_roborev_ceiling:` |
  | roborev bij gebruikte SHA | roborev open, plus de gebruikte sleutel | `return:reason_pr_roborev:` |
  | conflict met blokkeerder | conflict, `BlockedBy: [{Repo o/r, Issue 3, State OPEN}]` | `return:reason_pr_conflict:` |

  In elke rij met een `start` is de `TriggerKey` `fixpr:12:abc123`, het `Command` `/lighthouse-fix-pr 12` en de `Branch` `issue-7-bouw-de-keten`. Controleer dat in een aparte assert op de start.

  "3 gesloten rijen" zijn rijen van issue 7, met:
  - ID 1–3, en `Open false`;
  - `Outcome "handoff"`;
  - `Command "/lighthouse-fix-pr 12"`;
  - `TriggerKey "fixpr:12:s1"` tot en met `"fixpr:12:s3"`;
  - `StartedAt` vóór die van rij 5.

  "De gebruikte sleutel" is één zo'n rij, met `TriggerKey "fixpr:12:abc123"`.

  Fixture `fixpr-foreign-conflict.json`:
  - een kopie van `fixpr-foreign-pr.json`;
  - de vreemde PR krijgt `checks "SUCCESS"` en `mergeable "CONFLICTING"`;
  - `want` is één `return` met `reason_pr_conflict` en de sessie en `run_id` van de rij (#295).

  In `decide_test.go`, `TestSettleUnstartedFixPRCarriesCause`: een close met `planned{fixReason: "reason_merge_failed", fixReasonArg: "x"}`, zonder start en zonder `wanted`. Verwacht: een `KindReturn` met `Reason == "reason_merge_failed"` en `ReasonArg == "x"`. De bestaande `TestSettleUnstartedFixPR` blijft `reason_pr_red` geven voor een close zonder `fixReason`.

  Tabeltest `TestMergeReady`, op dezelfde basis:

  | Geval | Mutatie | Verwacht `summary` |
  |---|---|---|
  | merget | geen | `merge::` |
  | niet klaar | stand 0 (working) | leeg |
  | draft | `Draft = true` | `return:reason_pr_draft:` |
  | conflict | `CONFLICTING` | `close::…`, `start:reason_pr_conflict:` |
  | `UNKNOWN` | `Mergeable = "UNKNOWN"` | leeg |
  | checks `PENDING` | `Checks = "PENDING"` | leeg |
  | roborev open | `Roborev = "open"` | `close::…`, `start:reason_pr_roborev:` |
  | roborev verwerkt | `Roborev = "processed"` | `merge::` |
  | notitie op deze head | rij `MergeSHA = "abc123"` | `close::…`, `start:reason_merge_failed:…` |
  | notitie op oude head | rij `MergeSHA = "old1234"` | `merge::` |
  | andere branch | `HeadRef = "other-branch"` | `done::` |
  | fork | `CrossRepo = true` | `done::` |
  | default branch | `HeadRef` en rij `Branch` = `main` | `done::` |
  | pauze | `Brake = &Brake{Paused: true}` | leeg |
  | niet in Controller | `Status = "On hold"` | leeg |
  | actieve blokkeerder | `BlockedBy = [{o/r, 3, OPEN}]` | leeg |
  | blokkeerder met gemergde PR | `BlockedBy = [{o/r, 3, OPEN, MergedPR: true}]` | `merge::` |
  | twee eigen PR's, eerste conflict | #12 `CONFLICTING`, plus #14 groen op dezelfde branch met `head_sha "def456"` | `close::fixpr:12:abc123`, `start:reason_pr_conflict:` |
  | vreemde groene naast eigen conflict | eigen #12 `CONFLICTING`, plus vreemde #13 groen op `head_ref "other"` | `close::fixpr:12:abc123`, `start:reason_pr_conflict:` (geen `done`) |
  | vreemde conflict naast eigen groene, pauze | eigen #12 groen, vreemde #13 `CONFLICTING` op `head_ref "other"`, `Brake = &Brake{Paused: true}` | `return:reason_pr_conflict:` |

  Voor `merget`: vergelijk de hele actie met `reflect.DeepEqual` tegen de merge-actie uit de Interfaces, met PR 12, `HeadSHA "abc123"`, `PRURL "https://github.com/o/r/pull/12"` en sessie `11111111-2222-4333-8444-555555555555`.

  Voor de tegengehouden gevallen (pauze, niet in Controller, blokkeerder): Decide geeft geen enkele `close` voor run 5. De run blijft open.

  Fixture `closecheck-after-tick-merge.json`, als regressie-bewaker voor spec §8. Hij slaagt al vóór Stap 4. Het is een kopie van `settle-green-pr-done.json`, met:
  - PR #12: `state "MERGED"`, `merged_at "2026-10-31T11:30:00Z"`, `mergeable "UNKNOWN"`, `closes false`;
  - rij 5: `Open false`, `Outcome "done"`;
  - `stands` is `{}`.

  `want` is één `start`, met trigger `closecheck`, sleutel `closecheck:12`, command `/lighthouse-close-issue 7`, prompt `/lighthouse-close-issue 7 (issue #7, controller-run)`, branch `issue-7-bouw-de-keten`, attempt 1 en mode `start`.

- [ ] **Stap 3: Run de tests en zie ze falen**
  Run: `go test ./internal/dispatch -run 'FixPRCauses|MergeReady|SettleUnstarted|DecideFixtures|DecideKeysAreHeadings'`
  Verwacht: FAIL (compileerfout op `KindMerge`, `Roborev`; daarna: `merget` geeft nog `done::`, conflict, merge en roborev geven nog geen start, en de drie fixtures missen `reason`).

- [ ] **Stap 4: Implementeer** in `decide.go`.
  - `fixPR` vervangt `prRed` op dezelfde plek in `outcome`. Alleen bij `c.done()`:
    1. Is er een `ownOpenPR` met een oorzaak? De oorzaken, eerste wint: rood (`FAILURE`/`ERROR`); `CONFLICTING`; `c.row.MergeSHA != "" && c.row.MergeSHA == pr.HeadSHA` (arg `c.row.MergeError`); `MERGEABLE` + `SUCCESS` + `Roborev == forge.RoborevOpen`. Pas dan de poorten van `prRed` toe, in dezelfde volgorde:
       - plafond: return met de plafondreden, zonder arg;
       - gebruikte sleutel: return met de reden en arg van de oorzaak;
       - anders: de handoff-close plus een `fixPRStart` met de reden en arg.
    2. Anders: een open rode PR geeft `returned("reason_pr_red", "")`. Anders geeft een open `CONFLICTING`-PR `returned("reason_pr_conflict", "")`.
    3. Anders niets.
  - `decideIssue`:
    - zet `fixReason` en `fixReasonArg` op de `planned` van de close als `f != nil`;
    - zet `a.Reason` en `a.ReasonArg` op de fix-pr-start.
  - `settleUnstartedFixPR`: `a.Reason = cmp.Or(p.fixReason, "reason_pr_red")`, en `a.ReasonArg = p.fixReasonArg`.
  - Werk de commentaren bij die "PR red" of `prRed` noemen (`Decide`, `settle`, `settleUnstartedFixPR`, `fixPRStart`).

  - `outcome` krijgt `noStart(c.mergeReady)` direct vóór `noStart(c.green)`.
  - `mergeReady` geeft de merge-actie alleen als alles hieronder geldt:
    - er is een `ownOpenPR`;
    - `c.done()`;
    - de PR is geen draft, is `MERGEABLE` en heeft `SUCCESS`;
    - `Roborev != forge.RoborevOpen`;
    - `c.row.MergeSHA != pr.HeadSHA`;
    - het item staat in `Controller`;
    - `c.in.Brake == nil || !c.in.Brake.Paused`;
    - `len(activeBlockers(c.it)) == 0`.
  - `green` geldt alleen als `ownOpenPR` niets vindt.
  - `issueCtx.activeBlockers` roept `activeBlockers(c.it)` aan.
  - Werk het commentaar van `outcome` bij, en dat van de nieuwe regels.
  - Run `go run ./cmd/lighthouse init --upgrade`. Daarna geeft een tweede run `No changes.`.

- [ ] **Stap 5: Run de tests en zie ze slagen**
  Run: `go test -race ./internal/dispatch ./internal/protocol`
  Verwacht: PASS.

- [ ] **Stap 6: Commit**
  ```bash
  git add internal/dispatch pack/core/headings.toml .lighthouse/headings.toml
  git commit -m "feat(dispatch): de tick plant de merge van een groene eigen PR, en fix-pr bij conflict, roborev of mislukte merge (#294, #295)"
  ```

---

### Taak 5: De merge in `apply`

**Files:**
- Modify: `internal/dispatch/apply.go` (`Poster`, `apply`, nieuwe `merge`, `returnFixPR`)
- Test: `internal/dispatch/apply_test.go` (`fakePoster` krijgt `MergePR`)

**Interfaces:**
- Consumes:
  - `forge.ErrHeadMoved` en `*forge.MergeRefused` (Taak 2);
  - `runs.NoteMergeFailed` (Taak 1);
  - `KindMerge`, `Action.PR` en `Action.HeadSHA` (Taak 4).
- Produces: `Poster.MergePR(ctx context.Context, number int, sha string) error`, plus `func (ap applier) merge(ctx context.Context, a Action) error`.

- [ ] **Stap 1: Schrijf de falende tests.** `fakePoster` krijgt:
  - `mergeErr error`;
  - `merges []string`, met `"<pr> <sha>"` per aanroep;
  - een `log.add("MergePR")`.

  `mergeAction(runID)` is `{Kind: KindMerge, Repo: applyRepo, Issue: 7, RunID: runID, SessionID: oldSession, PR: 12, HeadSHA: "abc1234", PRURL: "https://github.com/o/r/pull/12", Language: "nl", End: true}`. Elke test seedt een open run (`h.seed(t, 7, oldSession, "start:7", "")`).

  | Test | `mergeErr` | Verwacht |
  |---|---|---|
  | `TestApplyMergeSucceeds` | nil | De calls zijn `MergePR`, `CreateComment` en `Kill`, in die volgorde. `merges == ["12 abc1234"]`. De body bevat de nl-tekst van `note_merged` en de PR-URL. De rij is gesloten als `done`. Geen fout. |
  | `TestApplyMergeHeadMoved` | `forge.ErrHeadMoved` | Geen post en geen `Kill`. De rij blijft open met een lege `MergeSHA`. `h.out` bevat `o/r#7: merge of PR #12 skipped: head moved`. Geen fout. |
  | `TestApplyMergeRefusedNotes` | `&forge.MergeRefused{Message: "Pull Request is not mergeable"}` | Geen post en geen `Kill`. De rij is open, met `MergeSHA "abc1234"` en `MergeError "Pull Request is not mergeable"`. Een logregel met `refused`. Geen fout. |
  | `TestApplyMergeOtherErrorFails` | `errors.New("status 500")` | Een fout, en `res.Failed[applyRepo]` is gezet. Geen post. De rij is open, zonder notitie. |
  | `TestApplyMergePostFailsKeepsRowOpen` | nil, met `postErr` gezet | Een fout. `merges` heeft één element. Geen `Kill`. De rij is open, zonder notitie. |
  | `TestApplyMergeRefusedOnClosedRunFails` | `MergeRefused`, met de rij vooraf gesloten | Een fout (`NoteMergeFailed` geeft `ErrNotOpen`). |

  Daarnaast `TestApplyFixPRReturnCarriesCause`: een eerste fix-pr-start met `Reason "reason_pr_conflict"`, terwijl de workplace busy is (zoals `TestApplyFixPRBusyReturns`). De geposte Teruggegeven bevat `PR heeft een merge-conflict` en niet `PR rood`. Een start zonder `Reason` geeft nog steeds `PR rood`.

- [ ] **Stap 2: Run de tests en zie ze falen**
  Run: `go test ./internal/dispatch -run 'ApplyMerge|ApplyFixPRReturnCarriesCause'`
  Verwacht: FAIL (compileerfout op `MergePR` of `KindMerge`).

- [ ] **Stap 3: Implementeer**
  - `Poster` krijgt `MergePR`.
  - `apply` krijgt `case KindMerge: return ap.merge(ctx, a)`.
  - `merge` volgt spec §3 stap 1–5:
    - gelukt: `RenderDone(a.SessionID, a.PRURL, Heading("note_merged"), a.Language)`, dan `release`, dan `closeRun(Done, "")`;
    - `errors.Is(err, forge.ErrHeadMoved)`: `ap.logf("%s#%d: merge of PR #%d skipped: head moved", …)`, en nil. `prefixed` in de CLI zet `lighthouse: ` ervoor;
    - `errors.As(err, &refused)`: `runs.NoteMergeFailed(ctx, db, a.RunID, a.HeadSHA, refused.Message)`, dan `ap.logf("%s#%d: merge of PR #%d refused: %s", …, oneLine(refused.Message))`. De fout van `NoteMergeFailed` gaat terug;
    - anders: `fmt.Errorf("merge: %w", err)`.
  - `returnFixPR`: de reden is `cmp.Or(a.Reason, "reason_pr_red")` met `a.ReasonArg`, via `reasonText`.
  - `internal/cli/tick.go` hoeft niet te veranderen: `env.client` geeft een `*forge.Client`, en die implementeert `MergePR`. Controleer dat met `go build ./...`.

- [ ] **Stap 4: Run de tests en zie ze slagen**
  Run: `go build ./... && go test -race ./internal/dispatch ./internal/cli`
  Verwacht: PASS.

- [ ] **Stap 5: Commit**
  ```bash
  git add internal/dispatch/apply.go internal/dispatch/apply_test.go
  git commit -m "feat(dispatch): apply merget, noteert een weigering of slaat een verschoven head over (#294)"
  ```

---

### Taak 6: Docs, skill en changelog

**Files:**
- Modify:
  - `docs/tick.md`;
  - `docs/site/data/loop.json`;
  - `docs/developer/planning/specs/2026-10-27-m2-dispatcher-design.md` (*No-Gos*, regel 34);
  - `docs/developer/planning/specs/2026-11-01-issue-119-roborev-lokaal-design.md` (*Niet-doelen*);
  - `pack/core/skills/fix-pr/claude/SKILL.md` en `pack/core/skills/fix-pr/agents/SKILL.md`.
- Create: `changelog.d/issue-294-tick-merget.md`
- Installed: de kopieën onder `.claude/skills/lighthouse-fix-pr/` en `.agents/skills/lighthouse-fix-pr/` (via `init --upgrade`)

- [ ] **Stap 1: `docs/tick.md`**, volgens spec §7:
  - De afloop-tabel krijgt drie regels:
    - 7: groene eigen PR van een klare run, rem open → merge, 🤖 Klaar met `note_merged`, claude beëindigen;
    - 7b: groene vreemde PR, geen open eigen PR → 🤖 Klaar zoals nu;
    - 13: klaar en een eigen PR met een van de vier oorzaken → fix-pr, met per oorzaak de reden en de plafondreden uit spec §2.
  - Bij 13 komt ook de vreemde PR erbij: rood geeft *PR rood*, `CONFLICTING` geeft *PR heeft een merge-conflict*.
  - Stap 7 *Gather* noemt het lezen van de PR-comments van een mergeklare PR bij een open run.
  - *Apply* beschrijft de merge en de drie soorten fout: 409/"Base branch was modified" geeft een logregel; een weigering een notitie; een andere fout exitcode 1.
  - *De rem* zegt dat een pauze, een item buiten `Controller` en een actieve blokkeerder de merge tegenhouden, en dat de run dan open blijft.
  - Stap 2 *Token* noemt *Contents: write* voor het mergen.
  - De alinea over `End` noemt de merge.

- [ ] **Stap 2: `loop.json`.** Regel 580 en de fix-pr-node noemen:
  - dat de tick merget;
  - dat fix-pr start bij rood, conflict, een onverwerkte roborev-review en een mislukte merge.

  Engels, zoals de rest van het bestand. Controleer dat het bestand geldige JSON blijft: `python3 -m json.tool docs/site/data/loop.json >/dev/null`.

- [ ] **Stap 3: De specs.**
  - In de M2-spec vervalt "lighthouse merget nooit". Op die plek komen de vijf punten uit spec §7, met een verwijzing naar #294.
  - De #119-spec krijgt onder *Niet-doelen* één regel: #294 draait de uitsluiting van een trigger op een onverwerkte roborev-comment om. De oude tekst blijft staan.

- [ ] **Stap 4: De fix-pr-skill, in beide varianten** (`claude` en `agents`, met dezelfde tekst):
  - *Domain*: de dispatcher start deze skill na een klare run ook bij een conflict, een onverwerkte roborev-review en een merge die faalde, niet alleen bij een rode PR. De tick merget een groene eigen PR zelf. Deze skill merget nooit.
  - De slotzin "the merge itself — that is a human decision" wordt: "the merge itself — the tick does that after a finished run, never this skill".
  - §6 punt 4: "does not read the marker … only on a red PR" wordt: "the dispatcher reads the marker: a review without one keeps the PR from merging and starts this skill, at most three times per PR".
  - Een nieuwe zin bij "stop at green" in §1: "A dispatcher start after a failed merge finds the PR green: report that and stop; the failure itself is in the 🤖 Returned that follows."

  Run `go run ./cmd/lighthouse init --upgrade`. Daarna geeft een tweede run `No changes.`.

- [ ] **Stap 5: Changelog.** Maak `changelog.d/issue-294-tick-merget.md` met `type: added`. Twee tot vier zinnen in het Nederlands:
  - de tick squash-merget een groene eigen PR van een klare run;
  - fix-pr start ook bij een conflict, een onverwerkte roborev-review en een mislukte merge;
  - het token heeft *Contents: write* nodig.

- [ ] **Stap 6: Verifieer.**
  - Run: `go run ./cmd/lighthouse init --upgrade` (verwacht `No changes.`).
  - Run: `go test -race ./...` (verwacht PASS).
  - Run: `golangci-lint run ./...` (verwacht geen bevindingen).

- [ ] **Stap 7: Commit**
  ```bash
  git add docs changelog.d pack/core/skills/fix-pr .claude/skills/lighthouse-fix-pr .agents/skills/lighthouse-fix-pr
  git commit -m "docs: de tick merget, fix-pr start bij vier oorzaken (#294)"
  ```
