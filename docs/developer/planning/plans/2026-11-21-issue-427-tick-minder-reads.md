# Tick met minder reads: implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Een tick op de REST-forge doet minder sequentiële GET's: geen `blocked_by` voor gesloten issues en elke (repo, PR) hoogstens één keer verrijkt per tick.

**Architecture:** `GitHub.BoardItems` slaat de `blocked_by`-lezing over voor een item met een gesloten issue. `GitHub.enrich` onthoudt het resultaat per (repo, PR) in een memo op de `GitHub`-client, zodat `complete` (sluitende PR's) en `BranchPRs` samen één verrijking doen. De memo staat uit tenzij de tick-opener hem aanzet: een client die langer leeft dan een tick (pusher, serve) mag geen verouderde mergebaarheid terugkrijgen.

**Tech Stack:** Go, `internal/forge`, `forgetest` nep-server (`srv.Count`), `docs/tick.md`.

**Spec:** geen spec; de acceptatiecriteria van issue #427 zijn de spec.

## Global Constraints

- Artefacten (docs, commits, changelog) Nederlands; code en commentaar Engels.
- Elke PR krijgt een fragment in `changelog.d/` (`type: changed`).
- Tests dragen een `// Classification: io. Fails when …`-kop (`/lighthouse-test-standard`); mutatiecheck voor "klaar".
- Verificatie: `lighthouse gate go -- go test -race ./internal/forge/... ./internal/dispatch/... ./internal/cli/...`, plus `lighthouse imports check`.
- GitLab (`internal/forge/gitlab*.go`) en `dispatch.Prepare` blijven ongewijzigd.
- Geen wijziging onder `pack/`: dogfood-upgrade niet nodig.

## Review Focus

- Gesloten issue met `total_blocked_by > 0`, gelezen via `BoardItems`: `BlockedBy` is leeg, geen request. Test in Task 1.
- Open issue met `total_blocked_by > 0` leest `blocked_by` nog steeds (geen regressie). Task 1.
- Dezelfde PR als sluitende PR én via `BranchPRs` op één client: `pulls/<n>` en check-requests precies één keer. Task 2.
- Een client zonder memo (pusher, serve, overview) verrijkt bij elke aanroep opnieuw, dus ziet een gewijzigde mergebaarheid. Task 2.
- Een PR van een andere repo met een verborgen 403/404 wordt niet in de memo bewaard als succes. Task 2.

---

### Task 1: Geen `blocked_by` voor gesloten issues

**Files:**
- Modify: `internal/forge/board.go:258-291` (`BoardItems`)
- Test: `internal/forge/board_test.go` (naast `TestBoardItemsBlockers`, `:263-306`)

**Interfaces:**
- Consumes: `restItem.issue()`, `restItem.Content.Summary.TotalBlockedBy`, `BoardItem.IssueState` (hoofdletters, `OPEN`/`CLOSED`, via `r.boardItem`).
- Produces: `BoardItems` zet `total = 0` als `strings.EqualFold(it.IssueState, "CLOSED")`; `complete` blijft ongewijzigd.

- [ ] **Step 1: Schrijf de falende test** `TestBoardItemsClosedIssueSkipsBlockedBy`. Bouw het board met `projectServer`; een item via `restIssue` met `node["content"]["state"] = "CLOSED"` en een blocker zodat `total_blocked_by > 0` (kopieer de opzet van `TestBoardItemsBlockers`), plus een open item met een blocker. Asserts: `srv.Count(".../issues/<gesloten>/dependencies/blocked_by") == 0`; `srv.Count(".../issues/<open>/dependencies/blocked_by") == 1`; het gesloten item heeft `BlockedBy == nil`.
- [ ] **Step 2: Draai** `go test -race ./internal/forge -run TestBoardItemsClosedIssueSkipsBlockedBy` — verwacht FAIL (count 1 voor het gesloten issue).
- [ ] **Step 3: Implementeer** in `BoardItems`: na `it, pulls := r.boardItem(p, b)` alleen `total` vullen als het issue niet gesloten is. Eén regel commentaar met de reden (een gesloten issue start of hervat geen run).
- [ ] **Step 4: Controleer de consumenten** van `BoardItem.BlockedBy` van een gesloten item: `internal/cli/triage_gather.go:117-120` (triage leest `it.BlockedBy` voor een bordissue; bepaal of triage een gesloten issue ooit met blockers nodig heeft, zo ja lees dan via `g.c.BlockedBy` voor een gesloten item), `internal/dispatch/fingerprint.go:92` (alleen `On hold`-items) en `internal/dispatch/gather.go:137`. Leg de uitkomst in de commitbody vast; pas een consument aan met een test als hij een gesloten item met blockers nodig heeft.
- [ ] **Step 5: Draai** `go test -race ./internal/forge/... ./internal/dispatch/... ./internal/cli/...` — verwacht PASS (incl. `forgesuite`).
- [ ] **Step 6: Commit**: `feat(forge): BoardItems leest geen blocked_by voor gesloten issues (#427)`.

---

### Task 2: Verrijking per (repo, PR) onthouden binnen een tick-client

**Files:**
- Modify: `internal/forge/github.go:45-61` (struct `GitHub`: veld `EnrichMemo bool` en `enriched map[prKey]enrichedPR`, beschermd door `mu`), `internal/forge/pr.go:63-79` (`enrich`)
- Modify: `internal/cli/forges.go:55-88` en/of `internal/cli/tick.go:298-318` (`openForges`): zet de memo aan voor de tick-clients
- Test: `internal/forge/board_test.go` (naast `:594-598`, `BoardItems` + `BranchPRs`), `internal/forge/pr_test.go`

**Interfaces:**
- Consumes: `GitHub.enrich(ctx, repo string, pr *PR) error`, `c.checks`.
- Produces: `type prKey struct{ repo string; number int; headSHA string }`, `type enrichedPR struct{ mergeable, checks string }`. `enrich` kijkt eerst in de memo (alleen als `c.EnrichMemo`), bewaart alleen bij succes, en geeft de bewaarde `Mergeable` en `Checks` terug zonder request. Sleutel: de repo-string (en `HeadSHA` van de PR, want checks horen bij een head) zoals `complete` en `BranchPRs` hem al doorgeven (`cmp.Or(pl.repo(), …)`).

- [ ] **Step 1: Schrijf de falende test** `TestBoardItemsAndBranchPRsEnrichOnce`: één open PR met run-branch is sluitende PR van een bordissue én staat in `pulls?head=`. Eén client met `EnrichMemo = true`; roep `BoardItems` en daarna `BranchPRs(branch)`. Asserts: `srv.Count("/repos/<repo>/pulls/<n>") == 1`, het check-run-pad precies 1, en beide PR's dragen dezelfde `Mergeable` en `Checks`.
- [ ] **Step 2: Schrijf de falende test** `TestEnrichWithoutMemoRepeats`: zelfde opzet met `EnrichMemo = false`; `pulls/<n>` wordt 2 keer gevraagd (zo blijft de pusher actueel). Draai beide — verwacht FAIL op de eerste, PASS op de tweede.
- [ ] **Step 3: Schrijf** `TestEnrichHiddenErrorNotMemoized`: een PR van een andere repo met 404 op `pulls/<n>` wordt bij een tweede aanroep opnieuw gevraagd (fout niet bewaard).
- [ ] **Step 4: Implementeer** de memo in `enrich` en `GitHub`. Alleen een `OPEN` PR gaat door de memo; de vroege `return nil` voor andere PR's blijft.
- [ ] **Step 5: Zet `EnrichMemo` aan** alleen via `openForges` (`tick.go:299`): een veld op `forgeOpener` dat alleen daar aan staat en in `forRepo` (`forges.go:55-88`, gedeeld met `serve`, pusher en `run`) op de `*forge.GitHub` wordt toegepast; nooit onvoorwaardelijk in `forRepo`. Niet voor `serve`, `overview`, pusher of `postartefact`. Test in `internal/cli` dat een via de tick-opener geopende GitHub-client `EnrichMemo` heeft en een andere opener niet (volg het bestaande opener-testpatroon in `internal/cli`).
- [ ] **Step 6: Draai** `go test -race ./internal/forge/... ./internal/dispatch/... ./internal/cli/...` — verwacht PASS; `TestBoardItemsSecondClientSameStore` (`board_test.go:415-434`) blijft groen.
- [ ] **Step 7: Commit**: `feat(forge): één verrijking per (repo, PR) binnen een tick-client (#427)`.

---

### Task 3: Documentatie en changelog

**Files:**
- Modify: `docs/tick.md` (stap 7 *Gather*, regels 85-109): voeg een lijst toe van de reads per item en per PR
- Create: `changelog.d/issue-427-tick-minder-reads.md`

- [ ] **Step 1: Schrijf in `docs/tick.md`** een korte lijst (Nederlands) onder *Gather*: per board-item `projectsV2`-pagina's (eenmaal per tick, gedeeld), bij GitHub `dependencies/blocked_by` alleen bij een open issue met `total_blocked_by > 0`; per PR `pulls/<n>` plus checks, hoogstens één keer per (repo, PR) per tick; per run-branch één `pulls?head=`; comments, reacties, `user` en collaborator-permissie zoals bij stap 7 al beschreven. Noem dat een 304 nog steeds een round trip kost, dus dat minder requests de tick verkort, niet de cache.
- [ ] **Step 2: Schrijf het fragment** `changelog.d/issue-427-tick-minder-reads.md` met `type: changed` en één Nederlandse zin.
- [ ] **Step 3: Draai** `.claude/hooks/lighthouse/run.sh md-link-check` is al een hook; controleer dat er geen absolute links zijn. Draai `lighthouse imports check`.
- [ ] **Step 4: Commit**: `docs(tick): reads per item en per PR, changelog (#427)`.

---

### Task 4: Meting op het echte board (hand nodig)

**Buiten de onbewaakte uitvoering (SDD/executing-plans eindigt na Task 3).** De PR-body wordt `Refs #427 — meting AC3 volgt met de hand` (geen `Closes`). Dit is acceptatiecriterium 3 en kan niet in een test. Een controller-run heeft geen `/rate_limit`-meting op het echte board bij de hand; deze taak is voor een sessie met echte forge-toegang.

- [ ] **Step 1:** Bouw de branch (`go build ./cmd/lighthouse`), meet zoals op #395: `/rate_limit` (core en graphql) voor en na één rustige tick (`lighthouse tick` met de echte `forge-cache.db`, direct na een volle tick; let op: dit is een echte tick met neveneffecten en `tick.lock`, draai hem alleen als normale timer-tick met de rem aan of in overleg), en de duur.
- [ ] **Step 2:** Post de meting als comment op #427 met dezelfde tabel als in de issuetekst. Verwacht: duur < 78 s en minder `blocked_by`-GET's dan 127.
- [ ] **Step 3:** Faalt de meting (niet korter), meld dat in het comment en in de PR-body; de andere drie criteria staan dan nog.
