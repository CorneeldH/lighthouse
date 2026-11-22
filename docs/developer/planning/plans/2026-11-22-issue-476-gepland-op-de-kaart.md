# Gepland item op de kaart — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Een dagelijkse geplande job `map-planned` zet open issues met een vrij label met document als gepland item in dat document; `write-issue` legt plek en wens vast bij het aanmaken.

**Architecture:** Een nieuwe `scheduled.Job` in `internal/scheduled/mapplanned/`. Een Go-poort leest de tabel uit `labels.md` en de issues van de forge, en start alleen een agent (skill `lighthouse-map-planned`) als er iets ontbreekt. De agent schrijft de documenten en een resultaatbestand; `Cleanup` post daaruit vragen en beslissingen als comments met een eigen, niet-protocol kop. `scheduled.Env` krijgt daarvoor een forge en een trust-functie.

**Tech Stack:** Go 1.26, `gopkg.in/yaml.v3`, `encoding/json`, `internal/forge`, `internal/issuestate`; Python 3 + pytest voor `ci/loop_roadmap.py`; skills in Markdown onder `pack/core`.

**Spec:** `docs/developer/planning/specs/2026-11-22-issue-476-gepland-op-de-kaart-design.md`

## Global Constraints

- Artefacten (docs, comments van de job in `nl`, commitberichten) in het Nederlands; code, namen, skills en Go-comments in het Engels (`[project] code_language = en`).
- Geen labelnaam (`loop`, `kaart`) en geen pad (`loop.json`, `kaart.yaml`) in `pack/core` of `internal/`.
- Zonder `paths.labels`, zonder bestand, of zonder rij met `Document` en `Planned`: de poort geeft `skip`, `write-issue` gedraagt zich als nu.
- `Planned` heeft precies twee waarden: `always`, `milestone`; leeg = geen aanmaakkant.
- Commentkop: `🗺 Kaart · map-planned` (`nl`), `🗺 Map · map-planned` (anders); machineregel `<!-- lighthouse:map-planned kind=<question|decision> topic=<place|wish> off=<letter> -->`.
- Jobnaam en schedulenaam: `map-planned`; cron `0 5 * * *`.
- Dogfood: na een wijziging onder `pack/` draait `go run ./cmd/lighthouse init --upgrade` en gaat de geïnstalleerde kopie mee in de commit; CI eist `No changes.`
- Go volgt `/lighthouse-go-standard`; tests volgen `/lighthouse-test-standard` (classificatiezin boven elke test: welke productiewijziging hem rood maakt).
- Commits als Conventional Commits, atomair, eindigend op `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **Een document dat een getal `N` elders bevat** (bijvoorbeeld `"min_lines": 168`) mag niet tellen als "issue N staat erop": alleen een veld met sleutel `issue`. Test in Taak 4.
2. **Een antwoord van een niet-vertrouwde gebruiker** (geen rol uit `answer_permissions`) heropent niets en sluit niets af. Test in Taak 5.
3. **Een herhaalde `Cleanup`** (postcheck na een tijdelijke fout) post geen tweede vraag voor hetzelfde issue. Test in Taak 7.
4. **Een comment van de job breekt de keten niet**: `protocol.ParseHeader` herkent hem niet. Test in Taak 5.
5. **Een issue zonder milestone onder `Planned = milestone`** blijft buiten de poort, ook als het label klopt. Test in Taak 6.

---

### Taak 1: `[paths]` in `repoconfig.Repo`

**Files:**
- Modify: `internal/repoconfig/repoconfig.go` (struct `Repo`, de toml-struct rond regel 137, `Load`)
- Test: `internal/repoconfig/repoconfig_test.go`

**Interfaces:**
- Produces: `type Paths struct{ Labels, Tenets, Areas string }` en veld `Repo.Paths Paths` (paden relatief aan de repo, `""` als niet gezet).

- [ ] **Stap 1: Schrijf de falende test** `TestLoadReadsPaths`: een `lighthouse.toml` met `[paths] labels = "docs/developer/labels.md"`, `tenets = "docs/t.md"`, `areas = "docs/areas"` geeft `Repo.Paths == Paths{Labels: "docs/developer/labels.md", Tenets: "docs/t.md", Areas: "docs/areas"}`; zonder `[paths]` is `Repo.Paths` de nulwaarde.
- [ ] **Stap 2: Draai** `go test ./internal/repoconfig/ -run TestLoadReadsPaths` — verwacht FAIL (veld bestaat niet).
- [ ] **Stap 3: Implementeer** het veld en de toml-sleutels `labels`, `tenets`, `areas` onder `[paths]`. Andere sleutels onder `[paths]` (`history`, `mirror_exceptions`) blijven toegestaan: Load mag er niet op falen.
- [ ] **Stap 4: Draai** `go test ./internal/repoconfig/` — verwacht PASS.
- [ ] **Stap 5: Commit** `feat(repoconfig): [paths] labels, tenets en areas in Repo (#476)`.

### Taak 2: forge en trust in `scheduled.Env`

**Files:**
- Modify: `internal/scheduled/scheduled.go` (struct `Env`)
- Modify: `internal/cli/tick.go:267` (`planSchedules` krijgt `ticked` en `cfg.AnswerPermissions`), `internal/cli/tick.go:539` (`scheduledJob` krijgt `forges` en `cfg`)
- Modify: `internal/cli/tick_scheduled.go` (`planSchedules`, `planRepo`, `scheduledJob`)
- Test: `internal/cli/tick_scheduled_test.go`

**Interfaces:**
- Produces: in `scheduled.Env`:
  ```go
  // Forge is the forge of the repo; nil where the tick has none (a skipped repo).
  Forge forge.Forge
  // Trusted reports whether login may answer a question of a run ([dispatch] answer_permissions).
  Trusted func(ctx context.Context, login string) (bool, error)
  ```
  `Trusted` komt van `dispatch.NewTrust(f, cfg.AnswerPermissions).Trusted`.

- [ ] **Stap 1: Schrijf de falende tests** `TestPlanRepoGivesTheJobForgeAndTrust` en `TestScheduledJobGivesForgeAndTrust`: een testjob (via `tickEnv.Jobs`, met een `Gate` resp. de teruggegeven `Env` die vastlegt wat hij ziet) krijgt `Env.Forge` gelijk aan de forge van de repo en een niet-nil `Trusted`.
- [ ] **Stap 2: Draai** `go test ./internal/cli/ -run 'TestPlanRepoGivesTheJob|TestScheduledJobGives'` — verwacht FAIL.
- [ ] **Stap 3: Implementeer** de velden en de doorgifte. `internal/scheduled` mag `internal/forge` importeren; niet `internal/dispatch` (daarom een functie, geen `*dispatch.Trust`).
- [ ] **Stap 4: Draai** `go test ./internal/cli/ ./internal/scheduled/...` — verwacht PASS, bestaande tests blijven groen.
- [ ] **Stap 5: Commit** `feat(scheduled): forge en trust in de Env van een geplande job (#476)`.

### Taak 3: de tabel uit het taxonomiebestand

**Files:**
- Create: `internal/scheduled/mapplanned/taxonomy.go`
- Test: `internal/scheduled/mapplanned/taxonomy_test.go`

**Interfaces:**
- Produces:
  ```go
  type Planned string
  const (PlannedAlways Planned = "always"; PlannedMilestone Planned = "milestone")
  type Row struct{ Label, Document string; Planned Planned }
  // Rows reads every row with a Document and a Planned value from the Markdown tables of md
  // whose header has the columns Label, Document and Planned. Backticks around a cell go.
  func Rows(md []byte) ([]Row, error)
  ```

- [ ] **Stap 1: Schrijf de falende tests:**
  - `TestRowsReadsTheFreeLabelTable`: de tabel `| Label | Kleur | When | Document | Planned |` met rij `` | `loop` | `F4D74B` | Voegt … | `docs/site/data/loop.json` | `milestone` | `` geeft `[]Row{{"loop", "docs/site/data/loop.json", PlannedMilestone}}`.
  - `TestRowsSkipsTablesWithoutTheColumns`: de area-tabel (`| Area | Kleur | Vuistregel |`) en een tabel met `Document` maar zonder `Planned` geven `nil, nil`.
  - `TestRowsSkipsAnEmptyPlannedCell`: een rij met lege `Planned` telt niet mee.
  - `TestRowsRefusesAnUnknownPlannedValue`: `Planned` = `soms` geeft een fout die `soms` en het label noemt.
- [ ] **Stap 2: Draai** `go test ./internal/scheduled/mapplanned/ -run TestRows` — verwacht FAIL.
- [ ] **Stap 3: Implementeer `Rows`**: kolommen op naam uit de kopregel, de scheidingsregel overslaan, cellen trimmen en omringende backticks weghalen.
- [ ] **Stap 4: Draai** de tests — verwacht PASS.
- [ ] **Stap 5: Commit** `feat(mapplanned): de tabel Label/Document/Planned lezen (#476)`.

### Taak 4: staat issue N in het document?

**Files:**
- Create: `internal/scheduled/mapplanned/document.go`
- Test: `internal/scheduled/mapplanned/document_test.go`

**Interfaces:**
- Produces:
  ```go
  // Issues returns every integer value of a key "issue" anywhere in the JSON or YAML document,
  // chosen by the extension of path (.json; .yaml or .yml). Another extension or a parse error
  // is an error that names path.
  func Issues(path string, doc []byte) (map[int]bool, error)
  ```

- [ ] **Stap 1: Schrijf de falende tests:**
  - `TestIssuesFindsTheIssueKeyInJSON`: een `loop.json`-achtig document met `{"chips":[{"id":"auto-answer","status":"planned","issue":168}]}` geeft `{168: true}`.
  - `TestIssuesFindsTheIssueKeyInYAML`: `onderdelen:\n  - id: live\n    stand: deels\n    issue: 987\n` geeft `{987: true}`.
  - `TestIssuesIgnoresOtherNumbers` (Review Focus 1): `{"min_lines": 168, "chips":[{"issue": 5}]}` geeft `{5: true}`, niet 168.
  - `TestIssuesRefusesOtherFormatsAndBrokenDocuments`: `kaart.md` en een kapotte `.json` geven een fout met het pad erin.
- [ ] **Stap 2: Draai** `go test ./internal/scheduled/mapplanned/ -run TestIssues` — verwacht FAIL.
- [ ] **Stap 3: Implementeer `Issues`**: unmarshal naar `any` (`encoding/json` met `UseNumber`, `yaml.v3`), recursief lopen over maps en slices; alleen een geheel getal onder sleutel `issue` telt.
- [ ] **Stap 4: Draai** de tests — verwacht PASS.
- [ ] **Stap 5: Commit** `feat(mapplanned): issuenummers in een JSON- of YAML-document (#476)`.

### Taak 5: de comments van de job

**Files:**
- Create: `internal/scheduled/mapplanned/comments.go`
- Test: `internal/scheduled/mapplanned/comments_test.go`

**Interfaces:**
- Produces:
  ```go
  type Topic string // "place" | "wish"
  type Question struct {
      Issue       int
      Topic       Topic
      Text        string
      Options     []string // 2..6, letters a..f in order
      Recommended string   // a letter
      Off         string   // the letter of "not on the map"; "" for a wish question
  }
  // RenderQuestion and RenderDecision write the comment body: the heading, the machine line, the
  // text in language ("nl" or other).
  func RenderQuestion(q Question, language string) string
  func RenderDecision(issue int, language string) string // value: no item
  type Status int
  const (StatusNone Status = iota; StatusOpen; StatusAnswered; StatusSettled)
  type Answer struct{ Topic Topic; Letter, Text string }
  // StatusOf reads the job's comments of one issue, oldest first: the latest question or
  // decision of the job, and the first comment after a question by a trusted login that is not
  // a job comment. A decision, or an answer whose letter is Off, is settled.
  func StatusOf(ctx context.Context, cs []protocol.Comment, trusted func(context.Context, string) (bool, error)) (Status, Answer, error)
  ```

- [ ] **Stap 1: Schrijf de falende tests:**
  - `TestJobCommentsAreNoProtocolComments` (Review Focus 4): `protocol.ParseHeader(RenderQuestion(q, "nl"))` en `ParseHeader(RenderDecision(7, "nl"))` geven `ok == false`.
  - `TestRenderQuestionCarriesTheMachineLine`: de body bevat `<!-- lighthouse:map-planned kind=question topic=place off=c -->` en de opties als `a)`, `b)`, `c)`; `nl` begint met `🗺 Kaart · map-planned`.
  - `TestStatusOf`: tabel met gevallen — geen job-comment → `StatusNone`; alleen een vraag → `StatusOpen`; vraag + antwoord `c` van een vertrouwde login met `off=c` → `StatusSettled`; vraag + `b, onder intake` vertrouwd → `StatusAnswered` met `Letter "b"`; beslissing → `StatusSettled`.
  - `TestStatusOfIgnoresUntrustedAnswers` (Review Focus 2): vraag + antwoord `c` van een niet-vertrouwde login → `StatusOpen`.
- [ ] **Stap 2: Draai** `go test ./internal/scheduled/mapplanned/ -run 'TestJobComments|TestRender|TestStatusOf'` — verwacht FAIL.
- [ ] **Stap 3: Implementeer** render en parse; de letter van een antwoord is het eerste teken (kleine letter `a`–`f`) gevolgd door einde, spatie of leesteken.
- [ ] **Stap 4: Draai** de tests — verwacht PASS.
- [ ] **Stap 5: Commit** `feat(mapplanned): vragen en beslissingen als comments buiten het protocol (#476)`.

### Taak 6: de job — poort en Launch

**Files:**
- Create: `internal/scheduled/mapplanned/mapplanned.go`
- Modify: `internal/repoconfig/schedule.go:39` (`scheduleJobs` krijgt `"map-planned"`)
- Modify: `internal/cli/tick_scheduled.go` (blank import `_ ".../internal/scheduled/mapplanned" // registers job map-planned`)
- Test: `internal/scheduled/mapplanned/job_test.go`

**Interfaces:**
- Consumes: `Rows` (T3), `Issues` (T4), `StatusOf` (T5), `Env.Forge`, `Env.Trusted` (T2), `Repo.Paths` (T1).
- Produces:
  ```go
  const Name = "map-planned"
  func New() scheduled.Job          // registered in init()
  // Configure refuses every param: the job has none.
  func (j *Job) Configure(s repoconfig.Schedule) (scheduled.Job, error)
  // due are the open issues the gate lets through, per row; Gate and Stage both use it.
  func (j *Job) due(ctx context.Context, env scheduled.Env, dir, ref string) ([]dueIssue, error)
  type dueIssue struct{ Row Row; Issue forge.Issue; Answer *Answer }
  ```
  Launch: `Command: "/lighthouse-map-planned --corpus .lighthouse/run/map-planned-corpus.json --result .lighthouse/run/map-planned-result.json"`, `Tools: Read Write Edit Glob Grep` (geen Bash), `MaxRuntime: 30m`, `Instructions`: onbemand, geen vragen in de sessie, alleen de documenten uit het corpus en het resultaatbestand schrijven, niet committen of pushen.

- [ ] **Stap 1: Schrijf de falende tests** (git-repo met een `origin` in `t.TempDir()`, forge via `forgetest.New` zoals `internal/forge/forgetest/server_test.go:18`):
  - `TestGateSkipsWithoutTaxonomy`: `Paths.Labels == ""` → `Skip == "no planned documents"`; een bestand zonder rij met `Planned` → dezelfde skip.
  - `TestGateLetsThroughAnIssueMissingFromItsDocument`: rij `loop`/`docs/site/data/loop.json`/`always`, open issue 12 met label `loop`, document zonder `issue: 12` → `Skip == ""`, `Score == 1`.
  - `TestGateSkipsWhenEveryIssueIsOnItsMap`: document met `"issue": 12` → `Skip == "every planned issue is on its map"`.
  - `TestGateMilestoneRowNeedsAMilestone` (Review Focus 5): rij `milestone`, issue 12 zonder milestone → skip; met milestone → door.
  - `TestGateLeavesOutOpenAndSettledIssues`: issue met een open job-vraag, en een met een beslissing → skip.
  - `TestGateReadsOrigin`: een rij die alleen in de werkmap staat en niet op `origin/main` telt niet.
  - `TestGateFailsOnABrokenDocument`: kapotte JSON → fout met de naam van het document.
  - `TestConfigureRefusesParams`: `Params{"x": 1}` → fout.
- [ ] **Stap 2: Draai** `go test ./internal/scheduled/mapplanned/ -run 'TestGate|TestConfigure'` — verwacht FAIL.
- [ ] **Stap 3: Implementeer** `Gate` (eerst `git fetch --quiet origin <branch>` zoals `maprefresh/drift.go:78`, dan `git show origin/<branch>:<path>` voor `labels.md` en elk document; `Forge.ListIssues(forge.IssueFilter{State: "open", Label: row.Label})`; per kandidaat `Forge.Comments` en `StatusOf`; `StatusNone` en `StatusAnswered` gaan door), `Launch`, registratie en `scheduleJobs`.
- [ ] **Stap 4: Draai** `go test ./internal/scheduled/mapplanned/ ./internal/repoconfig/ ./internal/cli/` — verwacht PASS.
- [ ] **Stap 5: Commit** `feat(mapplanned): de poort van de job map-planned (#476)`.

### Taak 7: de job — Stage, Cleanup, Allowed, Check, Finish

**Files:**
- Modify: `internal/scheduled/mapplanned/mapplanned.go`
- Create: `internal/scheduled/mapplanned/corpus.go` (corpus en resultaat)
- Test: `internal/scheduled/mapplanned/job_test.go`, `internal/scheduled/mapplanned/e2e_test.go` (naar `internal/scheduled/maprefresh/e2e_test.go`)

**Interfaces:**
- Produces: het corpus (`.lighthouse/run/map-planned-corpus.json`):
  ```json
  {"language":"nl","taxonomy":"docs/developer/labels.md","tenets":"","areas":"",
   "documents":[{"label":"loop","document":"docs/site/data/loop.json","planned":"milestone"}],
   "issues":[{"number":12,"title":"…","body":"…","labels":["loop"],"milestone":"M2 — …",
              "label":"loop","document":"docs/site/data/loop.json",
              "answer":{"topic":"place","letter":"b","text":"b, onder intake"}}]}
  ```
  en het resultaat (`.lighthouse/run/map-planned-result.json`):
  ```json
  {"questions":[{"issue":12,"topic":"place","text":"…","options":["…","nieuw onderdeel op de kaart","niet op de kaart"],"recommended":"a","off":"c"}],
   "decisions":[{"issue":13,"value":"no-item"}]}
  ```

- [ ] **Stap 1: Schrijf de falende tests:**
  - `TestStageWritesTheCorpus`: na `Stage` staat het corpus met issue 12, zijn document en `answer: null`; met een beantwoorde vraag staat `answer.letter`.
  - `TestCleanupPostsQuestionsAndDecisions`: een resultaat met één plaatsvraag (issue 12) en één beslissing (issue 13) → de fake forge heeft twee POSTs met de job-kop; geen label op 12.
  - `TestCleanupLabelsAWishQuestion`: een `wish`-vraag → POST plus `labels.needs_decision` op het issue (via `issuestate.Apply`).
  - `TestCleanupPostsOncePerIssue` (Review Focus 3): `Cleanup` twee keer op hetzelfde resultaat → één POST per issue.
  - `TestCleanupRefusesAPlaceQuestionWithoutOff`: `off` leeg of buiten de opties → fout, niets gepost.
  - `TestAllowedIsTheDocumentsOnly`: `docs/site/data/loop.json` ja; `labels.md`, `.lighthouse/run/x` (na Cleanup weg) en andere paden nee.
  - `TestCheckRefusesAnItemForAnIssueOutsideTheCorpus`: een nieuw `"issue": 99` dat niet in het corpus staat → fout met `#99`; een kapot document → fout.
  - `TestARunWithOnlyQuestionsEndsWithoutACommit` (e2e): agent schrijft alleen het resultaat → vraag gepost, geen commit, `done` zonder PR.
  - `TestARunReachesTheBranchWithTheDocument` (e2e): agent zet `"issue": 12` in `loop.json` → commit met alleen dat bestand, zonder corpus of resultaat.
- [ ] **Stap 2: Draai** `go test ./internal/scheduled/mapplanned/` — verwacht FAIL.
- [ ] **Stap 3: Implementeer** `Stage` (staging zoals `reflect.Stage`, via `safefs`), `Cleanup` (resultaat lezen; per issue eerst `StatusOf` — alleen bij `StatusNone` of `StatusAnswered` posten; vraag of beslissing posten met `Forge.CreateComment`; bij `wish` `issuestate.Apply(ctx, f, nr, "needs_decision", env.Repo.Labels)`; dan symlinks ontkoppelen en de stagingbestanden weghalen). `Cleanup` leest ook het corpus vóór het weghalen en bewaart op de job-instantie de `Document`-paden en de issuenummers: `Postcheck` roept `Cleanup`, `Allowed` en `Check` op dezelfde instantie aan, en daarna is het corpus weg. `Allowed`: precies die paden (vóór `Cleanup`: niets). `Check`: elk gewijzigd document parseert met `Issues`, en de nummers die erbij kwamen ten opzichte van `HEAD` zitten in de bewaarde set. Een hervatte run (`resume` → `checkCommit` in `postcheck.go`) roept `Check` aan zonder `Cleanup` ervoor: zonder bewaarde set toetst `Check` alleen het parsen (test `TestAResumedRunPassesCheckOnItsPushedCommit`, naar `reflect/job_test.go:387`). `Finish`: niets.
- [ ] **Stap 4: Draai** `go test -race ./internal/scheduled/...` — verwacht PASS.
- [ ] **Stap 5: Commit** `feat(mapplanned): corpus, comments en controle van de job (#476)`.

### Taak 8: de skill `lighthouse-map-planned`

**Files:**
- Create: `pack/core/skills/map-planned/SKILL.md`
- Modify: `internal/packs/core_test.go` (lijst: `.agents/skills/lighthouse-map-planned/SKILL.md` en de `.claude`-tegenhanger, zoals de andere skills)
- Test: `internal/packcontract/mapplanned_test.go`
- Dogfood: `go run ./cmd/lighthouse init --upgrade`; de nieuwe `.claude/skills/` en `.agents/skills/` mee committen

**Interfaces:**
- Consumes: corpus en resultaat van Taak 7 (velden exact zoals daar).

De skill (Engels, frontmatter `name: lighthouse-map-planned`, `description` met "Use when the scheduled job map-planned starts it, or by hand to put planned issues on their map"), in deze volgorde:
1. Lees `--corpus` en `--result` uit de argumenten; lees het taxonomiebestand en de uitleg onder de tabel *per document*: die zegt wat "als gepland toevoegen" betekent en of een wijziging aan een bestaand onderdeel een item geeft.
2. Per issue: plek uit `heading:map_placement` in de body (`lighthouse headings map_placement` geeft de varianten), uit `answer`, of uit het document zelf.
3. Plek eenduidig: item erin volgens de uitleg. *Geen item*: een beslissing `no-item`. Onduidelijk: een vraag `place` met de kandidaten, altijd ook *nieuw onderdeel op de kaart* en *niet op de kaart*, `off` = de letter van de laatste.
4. Geen `heading:map_placement` en `tenets`/`areas` in het corpus niet leeg: toets de wens; twijfel → een vraag `wish`, en dat issue komt er niet in.
5. Onbemand (de opdracht kwam van de job): geen vraag in de sessie, niets committen. Met de hand: vraag in het gesprek, en schrijf geen resultaatbestand.
6. Noem nooit een labelnaam of pad die niet uit het corpus of het taxonomiebestand komt.

- [ ] **Stap 1: Schrijf de falende test** `TestMapPlannedSkillContract` (stijl van `internal/packcontract/writeissue_test.go`): de skill noemt `--corpus` en `--result`, `heading:map_placement`, `no-item`, `off`, beide verplichte opties, en bevat geen `loop`, `kaart`, `loop.json` of `kaart.yaml`.
- [ ] **Stap 2: Draai** `go test ./internal/packcontract/ ./internal/packs/` — verwacht FAIL.
- [ ] **Stap 3: Schrijf de skill**; draai `go run ./cmd/lighthouse init --upgrade`.
- [ ] **Stap 4: Draai** `go test ./internal/packcontract/ ./internal/packs/` en `go run ./cmd/lighthouse init --upgrade` nogmaals — verwacht PASS en `No changes.`
- [ ] **Stap 5: Commit** `feat(core): skill lighthouse-map-planned (#476)`.

### Taak 9: `write-issue` — plek en wens bij het labelen

**Files:**
- Modify: `pack/core/skills/write-issue/claude/SKILL.md` en `pack/core/skills/write-issue/agents/SKILL.md` (stap 3f, de tabel *An existing issue*)
- Modify: `pack/core/headings.toml` (sleutel `map_placement`: `nl = "Op de kaart"`, `en = "On the map"`)
- Test: `internal/packcontract/writeissue_test.go` (`TestWriteIssueFreeLabelsWithADocument` uitbreiden)
- Dogfood: `init --upgrade`, `.lighthouse/headings.toml` en de skillkopieën mee committen

Nieuwe tekst van 3f, na de bestaande alinea (letterlijk in beide varianten):

> **Planned on the map.** When the label's row also has a `Planned` value (`always`, or `milestone` and the issue gets the milestone from 3d), ask two things in the conversation before step 4. *Where does it belong?* The document and the explanation under the table give the candidates; a new part of the map, and `no item` when the explanation says a change to an existing part gives none, are always candidates. *Do we want it?* Always ask; when `lighthouse config paths.tenets` or `paths.areas` gives a file or folder, first test the issue against the tenets and the leading policy and the deliberate no's of its area, and say what came out. Write the place as one line under `### heading:map_placement` in the body. An answer other than yes to the second question: no free label, or `labels.needs_decision` with the reason in the body. Headless (no conversation): write the place only when it is unambiguous, ask nothing, and leave the rest to the scheduled job. This step never writes the document.

- [ ] **Stap 1: Breid de test uit**: `wantAll` op de zinnen *"When the label's row also has a `Planned` value"*, *"Always ask"*, *"### heading:map_placement"*, *"This step never writes the document."*; en `lighthouse headings map_placement` geeft `Op de kaart` (test naast de bestaande headingstests).
- [ ] **Stap 2: Draai** `go test ./internal/packcontract/ -run TestWriteIssueFreeLabels` — verwacht FAIL.
- [ ] **Stap 3: Pas** beide varianten en `headings.toml` aan; draai `init --upgrade`.
- [ ] **Stap 4: Draai** `go test ./internal/packcontract/ ./internal/config/` en `init --upgrade` — verwacht PASS en `No changes.`
- [ ] **Stap 5: Commit** `feat(core): write-issue legt plek en wens vast voor een gepland item (#476)`.

### Taak 10: `labels.md` en de check van de loop-kaart

**Files:**
- Modify: `docs/developer/labels.md` (kolom `Planned`, uitleg)
- Modify: `ci/loop_roadmap.py` (`problems`, docstring), `.github/workflows/loop-roadmap.yml:10`
- Test: `tests/test_loop_roadmap.py`

- [ ] **Stap 1: Pas de tests aan:** `test_open_issue_on_a_milestone_missing_from_the_map_is_flagged` en `test_main_mode_flags_a_milestoned_issue_missing_from_the_map` worden `…_is_not_flagged` / `test_main_mode_leaves_a_milestoned_issue_missing_from_the_map_to_the_job` (verwacht: geen probleem, exit 0). `test_planned_item_on_a_closed_or_unlabelled_issue_is_flagged` blijft.
- [ ] **Stap 2: Draai** `uv run pytest -q tests/test_loop_roadmap.py` — verwacht FAIL op de twee aangepaste tests.
- [ ] **Stap 3: Implementeer:** `problems` laat de derde lijst vallen (`open_issues` blijft een `dict` voor de eerste twee); de docstring zegt dat de job `map-planned` de richting issue → kaart draagt; de workflow laat `milestoned, demilestoned` uit `types` vallen. `labels.md`: kolom `Planned` met `milestone` voor `loop`; de uitleg zegt wat "als gepland toevoegen" in `loop.json` is (alleen een nieuwe stap, handoff of route als `planned` met `issue`; een wijziging aan een bestaand onderdeel geeft geen item), dat de job `map-planned` dat dagelijks doet, en de hoofdmodus zonder de vervallen richting.
- [ ] **Stap 4: Draai** `uv run pytest -q tests/test_loop_roadmap.py tests/test_site_loop.py` en `go test ./internal/mdlinks/...` — verwacht PASS.
- [ ] **Stap 5: Commit** `feat(loop): de job map-planned draagt geplande items, de check alleen de andere richting (#476)`.

### Taak 11: aanzetten, docs, changelog

**Files:**
- Modify: `lighthouse.toml` (blok `[[schedule]] name = "map-planned"`, `job = "map-planned"`, `cron = "0 5 * * *"`)
- Modify: `docs/tick.md` (sectie *Job `map-planned`* na *Job `reflect`*: poort, corpus, comments, `Allowed`, geen params), `docs/install.md` (*Scheduled runs*: job `map-planned` in de lijst en het commentaar `# required; map-refresh, reflect or map-planned`)
- Create: `changelog.d/issue-476-kaart-bij-aanmaken.md` (`type: added`; één tot drie zinnen: de job en de `write-issue`-stap)

- [ ] **Stap 1: Draai** `go run ./cmd/lighthouse config` op de nieuwe `lighthouse.toml` (of `go test ./internal/repoconfig/`) — verwacht geen configfout.
- [ ] **Stap 2: Schrijf** de docs en het fragment.
- [ ] **Stap 3: Draai** `go test ./...`, `uv run pytest -q`, `go run ./cmd/lighthouse init --upgrade` — verwacht PASS en `No changes.`
- [ ] **Stap 4: Commit** `docs(tick): job map-planned, aangezet in deze repo (#476)`.
