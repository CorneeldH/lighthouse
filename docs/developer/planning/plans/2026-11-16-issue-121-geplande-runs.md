# Geplande runs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** De tick plant en draait per repo `[[schedule]]`-jobs (`map-refresh`, `reflect`) onder dezelfde rem als een issue-run, met de uitkomst via een auto-gemergde PR en zichtbaar in `GET /api/runs`.

**Architecture:** Een pure planner (`internal/scheduled`) bepaalt per schema het slot dat aan de beurt is; de tick draait de poort van de job, claimt een `runs`-rij (`issue` NULL, `schedule` gezet) en start de agent via `Workplace.Spawn`. Latere ticks meten de run in `Decide` (eigen tak), doen de nacontrole, openen de PR en mergen hem. De toestand is afgeleid uit `runs`; alleen `skipped` staat in `schedule_slots`.

**Tech Stack:** Go (modernc sqlite, cobra, BurntSushi/toml), git in de werkplek, `web/openapi.json` + orval-client.

**Spec:** [2026-11-16-issue-121-geplande-runs-design.md](../specs/2026-11-16-issue-121-geplande-runs-design.md)

Indeling in PR's volgt de spec: Task 1–10 = PR 1 (`Refs #121`), Task 11 = PR 2, Task 12 = PR 3, Task 13 = PR 4 (andere repo, sluit #121). Elke PR krijgt een fragment in `changelog.d/` en, bij een wijziging onder `pack/` of in wat `init` schrijft, `go run ./cmd/lighthouse init --upgrade`.

## Global Constraints

- Artefacten (docs, commitberichten, PR's) in het Nederlands; code, commentaar en namen onder `internal/**`, `cmd/**`, `tests/**` in het Engels (`code_language = "en"`).
- Go volgt `/lighthouse-go-standard`; tests volgen `/lighthouse-test-standard` (elke test faalt bij de productiewijziging die hij bewaakt).
- `name` van een schema: `[a-z0-9-]+`, uniek per repo. `job` ∈ {`map-refresh`, `reflect`}. `cron`: vijf velden, lokale tijd van de host. `weeks` en `anchor` samen of geen van beide; de week loopt maandag t/m zondag.
- Maximaal 2 pogingen per slot (`attempt` 1 en 2). `trigger_key` = `scheduled:<name>:<slot>`, slot als `YYYYMMDDTHHMM`. Branch `scheduled/<name>-<slot>-<poging>`.
- Een geplande run krijgt node `""`, geen `MCPConfig`, post nooit een 🤖 comment, landt nooit direct op `main`.
- `max_runtime` geldt tot en met de nacontrole; `pr_timeout` standaard 60 minuten daarna. Een rode of geweigerde PR → PR sluiten, branch verwijderen, `failed`, geen tweede poging.
- PR-body: `No issue: geplande run <name> (<slot>)`.
- Geen nieuwe `.py`. Geen ntfy-melder. Geen shell-commando als job.

## Review Focus

Wat de spec impliceert maar geen taak-test dekt zonder deze regels; elke regel heeft een test in de genoemde taak.

1. Symlink onder een toegestaan pad (reflect-agent schrijft `output_dir/x.md` → `~/.ssh/...`): `git status` toont alleen het pad, dus de nacontrole moet een symlink buiten de werkboom als overtreding weigeren (Task 7).
2. Cron-slot dat in een zomertijd-sprong valt (`30 2 * * *` op de nacht dat 02:30 niet bestaat) en een herfst-dubbel uur: het slot telt één keer, nooit twee runs (Task 1).
3. Een poort die faalt (git fetch mislukt) markeert het slot niet als `skipped`; het slot blijft open en de fout staat in de log (Task 8).
4. De agent verwijdert een verplicht document of laat `map_dir` leeg: de nacontrole ziet de verwijdering (porcelain `D`) en faalt op volledigheid, geen PR met een gat (Task 7, 11).
5. Een transcript met een corrupte regel of een enorme regel in het reflect-corpus: de extractie slaat de regel over en loopt door, het corpus blijft binnen `max_corpus_bytes` (Task 12).

---

## Task 1: Cron-parser en slotfilter

**Files:**
- Create: `internal/cron/cron.go`, `internal/cron/cron_test.go`

**Interfaces:**
- Produces: `cron.Parse(expr string) (Expr, error)`; `Expr.Last(now time.Time, f Filter) (time.Time, bool)`: het laatste slot ≤ `now` dat het filter doorstaat, binnen 400 dagen terug; `Filter{Weeks int; Anchor time.Time}` (nulwaarde = geen filter); `Slot(t time.Time) string` → `YYYYMMDDTHHMM` in de locatie van `t`.

- [ ] **Step 1: Tests schrijven** (tabeltest, vaste `time.Location` via `time.LoadLocation("Europe/Amsterdam")`): `"0 4 1 * *"` op 2026-11-16 → 2026-11-01 04:00; `"0 1 * * MON"` met `Filter{Weeks: 2, Anchor: 2026-10-12}` op 2026-11-16 → 2026-11-09 niet (week 4 van anchor, oneven), dus 2026-10-26 01:00; `Parse("0 4 1 *")` en `Parse("61 * * * *")` en `Parse("0 0 * * FUNDAG")` geven een fout; `*/15`, lijsten (`1,15`), reeksen (`1-5`) en `MON`–`SUN`/`JAN`–`DEC` werken. Review-focus 2: `"30 2 * * *"` op 2026-03-29 12:00 (Amsterdam, 02:30 bestaat niet) geeft het slot van 2026-03-28 02:30; `Last` op 2026-03-29 12:00 en op 2026-03-29 23:00 geeft hetzelfde slot, en op 2026-03-30 12:00 het slot van 2026-03-30 02:30. Herfst: `"30 2 * * *"` op 2026-10-25 12:00 geeft één slot (het eerste 02:30).
- [ ] **Step 2:** `go test ./internal/cron/` faalt (pakket bestaat niet).
- [ ] **Step 3: Implementeer** `Parse`, `Expr.Last`, `Filter`, `Slot`. Eigen parser (geen afhankelijkheid: vijf velden, `*`, `*/n`, lijst, reeks, namen). `Last` loopt minuutgewijs terug in wandklok-tijd en slaat een niet-bestaande wandklokminuut over; een dubbel uur telt één keer (eerste voorkomen). Dag-van-maand en dag-van-week combineren als in Vixie-cron (OR als beide beperkt zijn).
- [ ] **Step 4:** `go test -race ./internal/cron/` slaagt.
- [ ] **Step 5: Commit** `feat(cron): cron-parser met weeks/anchor-filter (#121)`.

## Task 2: `[[schedule]]` in repoconfig

**Files:**
- Modify: `internal/repoconfig/repoconfig.go` (veld `Schedules []Schedule` op `Repo`, `rawFile.Schedule`)
- Test: `internal/repoconfig/schedule_test.go`

**Interfaces:**
- Consumes: `cron.Parse`.
- Produces: `repoconfig.Schedule{Name, Job, Cron string; Weeks int; Anchor time.Time; Profile string; Params map[string]any}`; `Repo.Schedules []Schedule`. `Profile` is al ingevuld met `[controller] profile` als het schema geen heeft.

- [ ] **Step 1: Tests**: de twee voorbeeldblokken uit spec §1 laden en komen terug met `Weeks 2`, `Anchor 2026-10-12`, `Params["documents"]` als `[]any` van 7; elk van deze geeft een fout van `Load` (repo-config ongeldig): ontbrekende `name`, `name = "Map_Refresh"`, dubbele `name`, `job = "shell"`, `cron` met vier velden, `weeks` zonder `anchor`, `anchor` zonder `weeks`, `weeks = 0`, `anchor = "morgen"`, onbekende sleutel in het blok.
- [ ] **Step 2:** `go test ./internal/repoconfig/ -run Schedule` faalt.
- [ ] **Step 3: Implementeer** de parsing en validatie in `Load`. `anchor` is een datum `YYYY-MM-DD` in lokale tijd. De params-validatie per job komt in Task 11/12; hier blijft `Params` ongetypeerd.
- [ ] **Step 4:** `go test -race ./internal/repoconfig/` slaagt.
- [ ] **Step 5: Commit** `feat(repoconfig): lees en valideer [[schedule]] (#121)`.

## Task 3: Migratie en run-model

**Files:**
- Create: `internal/store/migrations/0008_runs_schedule.sql`
- Modify: `internal/runs/runs.go` (`Claim`, `Take`, `Release`, outcomes), `internal/runs/history.go` (`Row`, `rowColumns`, `scanRow`), `internal/runs/load.go`, `internal/runs/brief.go`, `internal/runs/workspaces.go`
- Test: `internal/runs/runs_test.go`, `internal/store/migrate_test.go`

**Interfaces:**
- Produces: `runs.Failed`, `runs.Retry` (type `Outcome`); `Claim.Schedule string` (leeg voor issue-run; precies één van `Issue`/`Schedule`), `Claim.StartSHA string`; `Row.Schedule`, `Row.StartSHA`; `runs.ErrClaimed` ook bij tweede open run van een schema; `runs.SetStartSHA(ctx, db, id, sha) error`; `runs.SlotSkipped(ctx, db, repo, schedule, slot) (bool, error)` en `runs.SkipSlot(ctx, db, repo, schedule, slot, reason string, now time.Time) error` (tabel `schedule_slots`); `runs.SlotSettled(history []Row, schedule, slot string) bool` (pure: `done` of `failed` aanwezig, of twee rijen voor `scheduled:<schedule>:<slot>`).

- [ ] **Step 1: Tests**: (a) migratie op een DB met een open issue-run en een gesloten run: rijen blijven, `schedule` leeg; (b) `Take` voor `Trigger: "scheduled"` met `Issue: 0, Schedule: "reflect"` slaagt, een tweede open run van hetzelfde schema geeft `ErrClaimed`, twee open runs van twee schema's in één repo slagen; (c) `Take` telt voor `scheduled` elke eerdere rij als poging: na één rij `retry` geeft `Take` `Attempt 2`, na een rij `done`, `failed` of twee rijen `ErrTriggerSpent`; voor een issue-run blijft het oude gedrag (alleen `start_failed`); (d) `Release(..., Retry, ...)` en `Failed` zijn geldig, een issue-run met `Failed` niet nodig; (e) `SlotSettled` tabel: geen rijen false; één `retry` false; `retry`+`retry` true; `failed` true; `done` true; `schedule_slots`-rij `skipped` via `SlotSkipped`; (f) `Open`, `OpenAll`, `History` en `Workspaces` scannen een geplande run zonder fout (issue = 0 in de Go-struct).
- [ ] **Step 2:** `go test ./internal/runs/ ./internal/store/` faalt.
- [ ] **Step 3: Migratie** `0008_runs_schedule.sql` in de stijl van `0004` (tabel herbouwen): `issue INTEGER` nullable, `schedule TEXT`, `start_sha TEXT NOT NULL DEFAULT ''`, `trigger` met `'scheduled'`, `outcome` met `'failed','retry'`, `CHECK ((issue IS NULL) <> (schedule IS NULL))`; behoud de kolommen van `0005`–`0007` (`workspace_removed`, `merge_sha`, `merge_error`, `node`: lees de huidige schema's, niet deze lijst); index `runs_open_issue` blijft, `runs_open_schedule ON runs(repo, schedule) WHERE ended_at IS NULL AND schedule IS NOT NULL`, `runs_trigger` opnieuw; tabel `schedule_slots(repo, schedule, slot, outcome CHECK (outcome = 'skipped'), reason, at, PRIMARY KEY (repo, schedule, slot))`. Scans gebruiken `coalesce(issue, 0)` en `coalesce(schedule, '')`.
- [ ] **Step 4:** `go test -race ./internal/runs/ ./internal/store/` slaagt.
- [ ] **Step 5: Commit** `feat(runs): migratie en run-model voor geplande runs (#121)`.

## Task 4: Forge: `CreatePR`, `ClosePR`, `DeleteBranch`

**Files:**
- Modify: `internal/forge/forge.go` (interface), `internal/forge/write.go` (of `github.go`, volg waar `MergePR` staat)
- Test: naast de bestaande forge-tests met de `httptest`-server die ze gebruiken; fake in `internal/dispatch` testhulp bijwerken zodat het `Forge` blijft implementeren

**Interfaces:**
- Produces: `CreatePR(ctx, branch, title, body string) (PR, error)` (base = default branch van de repo), `ClosePR(ctx, number int) error`, `DeleteBranch(ctx, branch string) error` (een al verdwenen branch, 404/422, is geen fout).

- [ ] **Step 1: Tests**: `CreatePR` POST `/repos/<repo>/pulls` met `head`, `base`, `title`, `body` en geeft `Number` en `URL`; `ClosePR` PATCH met `state: closed`; `DeleteBranch` DELETE `/git/refs/heads/<branch>` en een 422 "Reference does not exist" geeft nil; een 403 geeft een fout met de meldingtekst.
- [ ] **Step 2:** `go test ./internal/forge/` faalt (compileert niet).
- [ ] **Step 3: Implementeer** de drie methoden op `*GitHub` en voeg ze aan `Forge` toe; werk elke fake bij tot `go build ./... && go vet ./...` slaagt.
- [ ] **Step 4:** `go test -race ./internal/forge/ ./internal/dispatch/` slaagt.
- [ ] **Step 5: Commit** `feat(forge): CreatePR, ClosePR en DeleteBranch (#121)`.

## Task 5: `LaunchSpec.Tools` en geen MCP

**Files:**
- Modify: `internal/workplace/workplace.go` (`LaunchSpec.Tools []string`), `internal/workplace/carrier.go` (vlag in `NewCarrier`)
- Test: `internal/workplace/carrier_test.go`

**Interfaces:**
- Produces: `LaunchSpec.Tools`; lege lijst = huidig gedrag. Bij gevulde lijst zet `NewCarrier` de vlag die de beschikbare tools beperkt.

- [ ] **Step 1: Tests**: (a) een carrier met `Tools: ["Read","Glob","Grep","Write"]` bevat de vlag met precies die vier en geen `Bash`; (b) zonder `Tools` verandert de carrier niet (bestaande golden/asserts blijven groen); (c) zonder `MCPConfig` bevat de carrier `--strict-mcp-config` en geen `--mcp-config`.
- [ ] **Step 2:** Test faalt.
- [ ] **Step 3: Kies en bewijs de vlag.** Draai lokaal `claude -p --permission-mode auto --tools "Read" ...` en `--disallowedTools Bash` en kies de vlag waarvan Bash aantoonbaar niet beschikbaar is (spec §4: `--allowedTools` alleen vooraf goedkeuren is onvoldoende). Leg het bewijs (de commando's en de uitvoer in één regel) vast als commentaar boven de code.
- [ ] **Step 4:** `go test -race ./internal/workplace/` slaagt.
- [ ] **Step 5: Commit** `feat(workplace): LaunchSpec.Tools beperkt de beschikbare tools (#121)`.

## Task 6: Planner en jobinterface

**Files:**
- Create: `internal/scheduled/scheduled.go` (typen, `Job`-interface, register), `internal/scheduled/plan.go`, `internal/scheduled/plan_test.go`, `internal/scheduled/testjob_test.go`

**Interfaces:**
- Consumes: `cron.Expr.Last`, `repoconfig.Schedule`, `runs.SlotSettled`, `runs.Row`.
- Produces:
  - `scheduled.Due{Schedule repoconfig.Schedule; Slot time.Time}`.
  - `scheduled.Plan(now time.Time, schedules []repoconfig.Schedule, history []runs.Row, skipped func(schedule, slot string) bool) []Due`: per schema hooguit één `Due`, voor het laatste slot door het filter; niets als dat slot afgehandeld is (`SlotSettled` of `skipped`) of als het schema een open run in `history` heeft.
  - `scheduled.Job` interface: `Name() string`; `Gate(ctx, Env) (Verdict, error)`; `Launch(Verdict, Env) (LaunchPlan, error)`; `Allowed(path string) bool`; `Check(ctx, Env, dir string) error` (volledigheid); `Finish(ctx, Env, dir string, v Verdict) error` (spiegelen, `index.md`). `Verdict{Skip string; Mode, Args string; Score int}`. `LaunchPlan{Command string; Instructions string; Tools []string; MaxRuntime time.Duration}`. `Env{Repo repoconfig.Repo; Schedule repoconfig.Schedule; Dir string; Now time.Time; Run func(...)}` (git-aanroepen via `workplace.Runner`).
  - `scheduled.Lookup(name string) (Job, bool)` en `scheduled.Register(Job)`.

- [ ] **Step 1: Tests** (nep-klok, tabel): slot aan de beurt; slot al `done` → niets; `skipped` → niets; drie gemiste slots → één `Due`, het laatste; `weeks/anchor` slaat een oneven week over; open run → niets; twee schema's, één open → alleen het andere; nooit afgehandeld → het laatste slot; één `retry` → nog steeds `Due` (tweede poging).
- [ ] **Step 2:** `go test ./internal/scheduled/` faalt.
- [ ] **Step 3: Implementeer** `Plan` (pure functie) en de interface, plus een `testJob` in `testjob_test.go` die Task 8–9 gebruiken (instelbare `Verdict`, `Check`-fout, `Allowed` op prefix).
- [ ] **Step 4:** `go test -race ./internal/scheduled/` slaagt.
- [ ] **Step 5: Commit** `feat(scheduled): planner en jobinterface (#121)`.

## Task 7: Nacontrole en afronden in de werkplek

**Files:**
- Create: `internal/scheduled/postcheck.go`, `internal/scheduled/postcheck_test.go`, `internal/scheduled/secrets.go`, `internal/scheduled/secrets_test.go`

**Interfaces:**
- Consumes: `Job`, `workplace.Runner`.
- Produces: `scheduled.Postcheck(ctx, run workplace.Runner, job Job, env Env, startSHA, branch, title, body string, push func(ctx) error) (Outcome PostcheckResult, err error)`; `PostcheckResult{Changed bool; Failure string}`: `Failure != ""` is een echte `failed` met reden, `Changed == false` een `done` zonder PR. Volgorde: HEAD-guard (`git reset --mixed <startSHA>`), containment, `job.Check`, `job.Finish`, secret-scan, commit met pathspec, push. De PR zelf opent apply (Task 9); `Postcheck` stopt na de push en geeft de branch terug via de bestaande `Env`. `scheduled.ScanSecrets(dir string, files []string) []string` met de patroonset van de paia-scripts (lees `scripts/codebase_map_refresh.py` in personal_ai_agent voor de lijst en neem hem over, met herkomst als commentaar).

- [ ] **Step 1: Tests** in een echte git-repo in `t.TempDir()` met `testJob`: (a) alleen toegestane paden → commit bevat precies die; (b) de agent committe zelf → HEAD-guard zet terug, bestanden blijven, één eigen commit na afloop; (c) bestand buiten `Allowed` → `Failure` met het pad, geen commit, geen push; (d) `Check` faalt → `Failure`; (e) secret in een toegestaan bestand → `Failure`, niets gepusht; (f) niets gewijzigd → `Changed false`; (g) Review-focus 1: een symlink onder het toegestane pad naar `/etc/hostname` → `Failure` (containment weigert elke symlink waarvan het doel buiten de werkboom ligt of niet te bepalen is); (h) Review-focus 4: een verwijderd toegestaan bestand verschijnt in `git status` en bereikt `job.Check`; (i) hervatbaar: bestaat de branch al op de (lokale) origin, dan wordt niet opnieuw gecommit en `push` niet opnieuw nodig bij gelijke SHA.
- [ ] **Step 2:** Tests falen.
- [ ] **Step 3: Implementeer.** `git status --porcelain=v1 -z --untracked-files=all` parsen, `lstat` per pad voor de symlink-regel; commit met `git add -- <paden>` en `git commit -m`, push met de meegegeven `push`-functie (de bron van schrijfrechten kiest Task 9).
- [ ] **Step 4:** `go test -race ./internal/scheduled/` slaagt.
- [ ] **Step 5: Commit** `feat(scheduled): nacontrole, secret-scan en commit in de werkplek (#121)`.

## Task 8: Start en planner in de tick

**Files:**
- Modify: `internal/dispatch/decide.go` (nieuwe `Kind`s, `Action.Schedule`, `Action.Slot`, `RepoInput.Schedules`), `internal/dispatch/brake.go`, `internal/dispatch/apply.go` (`startScheduled`), `internal/cli/tick.go` (planner na gather, vóór issue-starts)
- Test: `internal/dispatch/decide_scheduled_test.go`, `internal/dispatch/apply_scheduled_test.go`, `internal/cli/tick_scheduled_test.go`

**Interfaces:**
- Consumes: `scheduled.Plan`, `Job.Gate`/`Launch`, `runs.Take` met `Claim.Schedule`, `workplace.LaunchSpec.Tools/InstructionsFile`.
- Produces: `dispatch.KindScheduledStart` (Action met `Schedule`, `Slot` `YYYYMMDDTHHMM`, `Profile`, `Command`, `Branch`); brake telt hem als plek en als `software` bij dat profiel; `apply` claimt, bereidt de werkplek op node `""`, schrijft `InstructionsFile` onder `<stateDir>/runs/<session>/`, slaat `start_sha` op (`runs.SetStartSHA`) en start met `LaunchSpec{Tools, MCPConfig: ""}`; start mislukt/onzeker sluit de run als `retry` (Task 3) met reden.

- [ ] **Step 1: Tests**: (a) tick met een schema aan de beurt en open brake → één `ScheduledStart` vóór de issue-starts, run-rij met `schedule`; (b) rem dicht (pauze, usage, geheugen, plekken) → geen claim, slot blijft open, volgende tick probeert opnieuw; (c) poort `Skip` → `schedule_slots`-rij `skipped`, één logregel met reden, geen run; (d) Review-focus 3: `Gate` geeft een fout → geen `skipped`, slot open, logregel met de fout; (e) tweede tick terwijl de run open is → geen tweede claim (`runs_open_schedule`); (f) `Spawn` wordt aangeroepen met lege `MCPConfig` ook als het profiel er een heeft, en met de `Tools` van de job; (g) start mislukt → run `retry`, volgende tick start poging 2 met branch `scheduled/<name>-<slot>-2`, daarna `failed` en slot afgehandeld; (h) een tick die na de claim sterft (claim zonder `SetSession`) laat een open rij die de volgende tick niet dubbel start.
- [ ] **Step 2:** Tests falen.
- [ ] **Step 3: Implementeer.** In `tick.go`: na `gatherRepo` per repo `scheduled.Plan(...)`, per `Due` de poort, dan `KindScheduledStart`-acties naar `Decide`/`Brake`; plan en poort lopen vóór `decideIssue`-starts in dezelfde volgorde-sleutel (groep vóór issue-starts, spec §2). Voeg het eerstvolgende slot toe als tijdgrens in `precheck`/`fingerprint` zodat de gather-skip doorbroken wordt.
- [ ] **Step 4:** `go test -race ./internal/dispatch/ ./internal/cli/` slaagt.
- [ ] **Step 5: Commit** `feat(dispatch): start geplande runs in de tick onder de rem (#121)`.

## Task 9: Afloop: decide-tak, PR, merge en sluiten

**Files:**
- Modify: `internal/dispatch/gather.go`, `internal/dispatch/fingerprint.go` (PR's van de branch van elke open geplande run), `internal/dispatch/decide.go` (tak `decideScheduled`), `internal/dispatch/apply.go` (`KindScheduledFinish`, `KindScheduledMerge`, `KindScheduledClosePR`), `internal/cli/tick.go` (token als HTTPS-credential voor de push)
- Test: `internal/dispatch/decide_scheduled_test.go`, `internal/dispatch/apply_scheduled_test.go`

**Interfaces:**
- Consumes: `scheduled.Postcheck`, `Forge.CreatePR/ClosePR/DeleteBranch/MergePR`, `runs.Failed/Retry/Done`.
- Produces: per open geplande run precies één van de rijen uit spec §4: sessie klaar zonder PR → `Finish` (Postcheck → `CreatePR` met body `No issue: geplande run <name> (<slot>)`; `Changed false` → `done`; `Failure` → `failed`); eigen PR groen en rem open → `MergePR(sha)` dan `DeleteBranch` en `done`; `*MergeRefused` → sluit PR, verwijder branch, `failed`; `ErrHeadMoved` → niets; rood of conflict → sluit, verwijder, `failed`, geen fix-pr; gesloten zonder merge → verwijder branch, `failed`; regels 9, 10, 15–19 en usage-park → `retry` (PR gesloten, branch verwijderd); `pr_timeout` (60 min vanaf PR) → sluit, `failed`.

- [ ] **Step 1: Tests** (decide-tabel met de fakes van `tickEnv`, één rij per regel van de spec-tabel) en apply-tests voor de acties; extra: (a) PR-fase telt een pauze of dichte rem mee in `pr_timeout`; (b) `max_runtime` in de agentfase geeft `retry`, niet `failed`; (c) een geplande run produceert nooit `CreateComment`; (d) de push gebruikt het forge-token als HTTPS-credential en logt het niet (assert op de logoutput).
- [ ] **Step 2:** Tests falen.
- [ ] **Step 3: Implementeer.** `decideScheduled` hergebruikt de `runCtx`-metingen waar die niet van een issue afhangen (`silent`, `timeout`, `uncertain`, `permission`) en slaat issue-regels over; de push-bron: `git -c credential.helper=` met `GIT_ASKPASS`-script in `stateDir` dat het token uit de omgeving van de tick leest, getest met een lokale bare remote.
- [ ] **Step 4:** `go test -race ./internal/dispatch/ ./internal/cli/` slaagt.
- [ ] **Step 5: Commit** `feat(dispatch): afloop van geplande runs via een auto-gemergde PR (#121)`.

## Task 10: Zicht, sweep, API en documentatie

**Files:**
- Modify: `internal/dispatch/view.go` (`RunView.Schedule`, `SaveRunView` onder `scheduled:<name>`, `RunViews` ontdubbelt alleen issue-runs), `internal/workplace/sweep.go` (+ `leftovers.go`: geforceerd weggooien van een geplande werkplek zonder open rij), `internal/server/*` (API-type), `web/openapi.json`, `web/src/api/**` (gegenereerd), `web/src/**` (toont schemanaam), `docs/tick.md`, `docs/install.md`, `changelog.d/issue-121-geplande-runs.md`
- Test: `internal/dispatch/view_test.go`, `internal/workplace/sweep_test.go`, `internal/server/*_test.go`, `web/src/**/*.test.tsx`

**Interfaces:**
- Produces: `RunView.Schedule string` (JSON `schedule`, leeg voor een issue-run); `GET /api/runs` geeft geplande runs in dezelfde lijst. Sweep-contract: een werkplek waarvan de run een geplande is en geen open rij heeft, wordt geforceerd verwijderd (`Remove` ook bij vuil of niet gepusht); een issue-werkplek blijft dan staan.

- [ ] **Step 1: Tests**: (a) AC 4: twee open geplande runs in één repo staan beide in `LoadView` en `GET /api/runs`; (b) sweep: vuile issue-werkplek blijft staan, vuile geplande werkplek zonder open rij gaat weg; (c) web: een rij met `schedule` toont de schemanaam en geen issuenummer; (d) de openapi-drift-check (`lighthouse gate node --drift gen=src/api`) blijft groen.
- [ ] **Step 2:** Tests falen.
- [ ] **Step 3: Implementeer**; regenereer `web/openapi.json` en de client met het bestaande commando uit `web/package.json`. Schrijf in `docs/tick.md` de sectie *Geplande runs* (config, planner, afloop, het volledige PR-pad) en de rijen in *Zicht*, in `docs/install.md` het `[[schedule]]`-blok met de eis dat de doelrepo de PR groen kan krijgen. Fragment in `changelog.d/`.
- [ ] **Step 4:** `go test -race ./...`, daarna in `web/`: `npm run lint && npm run typecheck && npm test`, en `go run ./cmd/lighthouse init --upgrade` (CI eist `No changes.`) slagen.
- [ ] **Step 5: Commit** (twee commits: `feat(web): toon geplande runs (#121)`, `docs(tick): geplande runs (#121)`). Daarna `/lighthouse-write-pr` voor PR 1 met `Refs #121 — PR 1 van 4`.

## Task 11: Job `map-refresh` (PR 2)

**Files:**
- Create: `internal/scheduled/maprefresh/maprefresh.go`, `drift.go`, `maprefresh_test.go`; registratie via `init()` of expliciete aanroep in `internal/cli`
- Modify: `internal/repoconfig/schedule.go` (param-validatie), `docs/tick.md`, `changelog.d/issue-121-map-refresh.md`

**Interfaces:**
- Consumes: `scheduled.Job`.
- Produces: `maprefresh.New() scheduled.Job`; params en standaarden uit spec §5 (`command`, `fast_args`, `map_dir`, `publish_dir`, `documents`, `min_lines`, `drift_paths`, `drift_low`, `drift_high`, `new_dir_weight`, `tools`, `max_runtime_fast` 45m, `max_runtime_full` 120m). `Gate` meet op `origin/<default>`: score = gewijzigde bestanden onder `drift_paths` sinds de laatste commit die `map_dir` raakte + `new_dir_weight` × nieuwe mappen; `< drift_low` → `Skip`; `≤ drift_high` → `Mode "fast"`; daarboven of nooit gemapt → `"full"`.

- [ ] **Step 1: Tests**: drift-tabel in een echte git-repo (onder low → skip; tussen → fast met `fast_args` in `Command`; boven high → full; nooit gemapt → full; nieuwe map weegt `new_dir_weight`); `Allowed` accepteert alleen `map_dir/` en `publish_dir/`; `Check`: een ontbrekend of te kort document faalt, ook een verwijderd document (Review-focus 4), en lege `documents` is een configfout; `Finish` kopieert (geen symlink) en schrijft `index.md` met modus, score, basis en datum; `map_dir` byte-gelijk aan HEAD vóór het spiegelen → `Postcheck` geeft `Changed false`.
- [ ] **Step 2:** Tests falen.
- [ ] **Step 3: Implementeer** met het gedrag van `scripts/codebase_map_refresh.py` in personal_ai_agent als referentie; poort en afronden zijn zuivere git- en bestandsoperaties via `workplace.Runner`.
- [ ] **Step 4:** `go test -race ./internal/scheduled/... ./internal/repoconfig/` slaagt; een end-to-end tick-test (fake agent die de zeven documenten schrijft) levert een PR en `done`.
- [ ] **Step 5: Commit** `feat(scheduled): job map-refresh (#121)`; daarna `/lighthouse-write-pr` met `Refs #121 — PR 2 van 4`.

## Task 12: Job `reflect` (PR 3)

**Files:**
- Create: `internal/scheduled/reflect/reflect.go`, `sessions.go`, `corpus.go`, `reflect_test.go`, `testdata/*.jsonl`
- Modify: `internal/repoconfig/schedule.go`, `docs/tick.md`, `changelog.d/issue-121-reflect.md`

**Interfaces:**
- Consumes: `scheduled.Job`, `runs.History` (sessie-id's per trigger), de werkplek-adapter voor de kenn-forge-mapnaam.
- Produces: `reflect.New() scheduled.Job`; params uit spec §6; `Gate` telt sessiebestanden van beide soorten nieuwer dan het jongste document in `output_dir`, binnen `window_min_days`–`window_max_days`, onder `min_sessions` → `Skip` met beide aantallen in de reden; `sessions.Kind(sessionID string, controller map[string]bool) string` ("interactive"/"controller"; DB is de bron, niet het pad); `corpus.Build(files []SessionFile, maxBytes int) []byte` met secties *Interactief* en *Controller*, identieke controller-prompts samengevoegd met telling, budget half om half met doorschuiven; `Launch` geeft `Command` met `--corpus .lighthouse/run/reflect-corpus.md --out <output_dir>/<datum>-skill-kandidaten.md`; `Allowed` alleen dat ene bestand en `output_dir/index.md`; `Finish` schrijft `index.md` en verwijdert het corpus vóór de nacontrole.

- [ ] **Step 1: Tests**: soort uit de DB (een sessie in een kenn-forge-map zonder `runs`-rij blijft interactief; een sessie met `trigger = scheduled` telt niet mee, ook eerdere reflect-rondes); vindplaats omvat de repo-map, `--claude-worktrees-` en de kenn-forge-mappen afgeleid uit de adapter; poort-tabel rond `min_sessions` en het venster; corpus op fixtures: `SKIP_PATTERNS` filteren, identieke controller-prompts krijgen `×N`, budgetverdeling (leeg interactief geeft alles aan controller, en omgekeerd), Review-focus 5: een corrupte JSON-regel en een regel groter dan het budget worden overgeslagen en het corpus blijft ≤ `max_corpus_bytes`; `Allowed` weigert een tweede bestand onder `output_dir`; `Check` weigert < `min_lines`; `Tools` bevat geen `Bash`.
- [ ] **Step 2:** Tests falen.
- [ ] **Step 3: Implementeer** met `scripts/reflect_skills_run.py` als referentie; neem extractie en `SKIP_PATTERNS` over met de herkomst (claude-reflect, MIT) als commentaar.
- [ ] **Step 4:** `go test -race ./internal/scheduled/... ./internal/repoconfig/` slaagt; end-to-end tick-test met een fake agent die het ene document schrijft.
- [ ] **Step 5: Commit** `feat(scheduled): job reflect (#121)`; daarna `/lighthouse-write-pr` met `Refs #121 — PR 3 van 4`.

## Task 13: Overstap in paia (PR 4, andere repo; sluit AC 3)

Eigen sessie in personal_ai_agent, na merge van PR 1–3 en een binary-update op de Beelink. Geen Go.

- [ ] **Step 1:** De twee `[[schedule]]`-blokken uit spec §1 in paia's `lighthouse.toml`; `/paia-reflect-skills` leert *Interactief* en *Controller* apart te wegen; paia's CI moet de PR van een geplande run groen kunnen krijgen met alleen de toegestane paden.
- [ ] **Step 2:** Op de Beelink: wacht op of forceer een eerste run van elk schema; controleer dat beide in `GET /api/runs` staan en dat de PR landt. Dit is het bewijs voor AC 3, geen test.
- [ ] **Step 3:** Verwijder `deploy/beelink/codebase-map-refresh.{service,timer}`, `deploy/beelink/reflect-skills.{service,timer}`, `scripts/codebase_map_refresh.py`, `scripts/reflect_skills_run.py`, hun tests en de vermelding in `deploy/beelink/manifest.toml`; draai `systemctl --user disable --now codebase-map-refresh.timer reflect-skills.timer` (dit commando loopt via `lighthouse ask` naar de gebruiker).
- [ ] **Step 4:** PR met `Closes #121` en een Evidence-tabel van de vier AC.

## Self-review

- **Spec-dekking:** §1 → Task 1–2; §2 → Task 6, 8 (inhalen, open run, poort, rem, start, node `""`, tijdelijk mislukt); §3 → Task 3, 10; §4 → Task 5, 7, 9; §5 → Task 11; §6 → Task 12; §7 → Task 13; §8 → Task 10–12. AC 1: Task 2; AC 2: Task 3 (index), 6, 8; AC 3: Task 13; AC 4: Task 10.
- **Bewust aan de bouwer:** vorm van `schedule_slots` en de migratie-details (Task 3), de vlag voor `Tools` (Task 5), de bron van de push-credential (Task 9), de exacte hulpfuncties in `internal/scheduled`.
- **Risico:** Task 8–9 raken `decide.go` (1367 regels) en `apply.go`; houd de geplande tak in eigen bestanden (`decide_scheduled.go`, `apply_scheduled.go`) zodat de issue-tak ongewijzigd blijft.
