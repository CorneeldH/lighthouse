# De keten in decide en apply — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** De tick beslist en handelt: een Handoff of een nieuw Controller-issue start een run, een Vraag sluit hem, een antwoord hervat hem, een groene PR geeft 🤖 Klaar, en een gemergde `Refs`-PR start de nacontrole. Daarnaast komen `lighthouse ask` en `lighthouse run <repo>#<issue>`.

**Architecture:** `dispatch.Decide` is een pure functie van `Input` (de snapshots, de runhistorie, de sessiestand en de config per repo) naar een geordende lijst `Action`. `dispatch.Apply` voert die acties uit tegen GitHub (`forge.Client`), de runstaat (`runs`) en de werkplek (`workplace`). Een start doet eerst de claim, dan de 🤖-comment, dan de 🚀 en pas daarna de launch. De Go-twin van `protocol.py` krijgt de sessie uit de voet, `ParseHandoff`, de renderers en `ParseQuestion`. `ask` en de tick lezen een Vraag dus met dezelfde parser. Een start-, hervat- of nacontroleactie is altijd één gerichte uitvoering. Wat in #115 (afloop), issue 6 (de rem) of issue 7 (blokkades) hoort, bouwt dit plan niet. Een uitvoerende fase met een open blokkeerder wordt hier daarom uitgesteld.

**Tech Stack:** Go, cobra/pflag, `modernc.org/sqlite`, `embed`, `net/http/httptest` (`internal/forge/forgetest`); Python alleen voor de gedeelde protocol-fixture.

**Spec:** [`docs/developer/planning/specs/2026-10-27-m2-dispatcher-design.md`](../specs/2026-10-27-m2-dispatcher-design.md), §3 *De tick* (stap 4–5, `run`), §4 *Decide: de regels* (regels 2, 4, 5, 6 en *Dedup*), §5 *Antwoorden*, §6 (*Launch-opdracht*, run-instructies, `lighthouse ask`), §7 (alleen de rijen Handoff, Vraag en PR groen) en *Teststrategie*. Issue #114. Naslag voor gedrag, niet als blauwdruk: `personal_ai_agent/src/controller/comments.py` (`beoordeel_commando`, `ALLOWLIST`, `antwoord_prompt`, `render_klaar`), `src/controller/decide.py` (`review_sprong`, `_start`), `scripts/controller_vraag.py`, `scripts/controller_forge_instructies.md` en `scripts/controller_forge_rondegrens_vraag.md`.

## Global Constraints

- Go volgt `/lighthouse-go-writing`. Tests volgen `/lighthouse-testing`: de classificatiezin vóór de eerste edit, de mutatiecheck vóór "klaar".
- Code-taal Engels onder `internal/**`, `cmd/**`, `pack/**` en `tests/**`. Dat geldt ook voor de run-instructies en voor de prompt aan de run: dat zijn instructies. Plan, docs, commits, PR en **alles wat op een issue komt** volgen `[project] language` van de bediende repo. Die tekst komt uit `pack/core/headings.toml` (`protocol.Heading`), nooit als letterlijke string in Go.
- De allowlist, letterlijk uit paia: `/lighthouse-start-issue`, `/lighthouse-spec-adversarial-review`, `/lighthouse-plan-adversarial-review`, `/superpowers:writing-plans`, `/superpowers:subagent-driven-development` en `/lighthouse-fix-pr`. Eindcommando `/lighthouse-close-issue`, brainstorm `/superpowers:brainstorming`, interactief `/superpowers:executing-plans`, lane `native`, model `sonnet`. Een `/paia-<naam>` telt als `/lighthouse-<naam>`.
- De vorm van het argument: `start-issue` precies `<issue>`; `fix-pr` één getal; elk ander commando één absoluut pad dat past op `^/[A-Za-z0-9._/\-]+\.md$`.
- De prompt van elke start en nacontrole is `<commando> (issue #<N>, controller-run)`. Dat is het signaal waarop `lighthouse-start-issue` een controller-run herkent.
- De trigger-sleutels (spec §4): `start:<issue>`, `handoff:<comment-id>`, `answer:<comment-id>` en `closecheck:<pr>`. `Trigger` is `handoff`, `answer` of `closecheck`.
- 🚀 is weergave, geen dedup. Dedup loopt alleen via `runs` (`runs.Take` en `runs.History`).
- Een groene PR: `State == "OPEN"`, niet `Draft`, `Mergeable == "MERGEABLE"` en `Checks == "SUCCESS"`.
- Decide zet geen boardstatus. Een Teruggegeven laat het issue in Controller (spec r1-f15).
- `lighthouse ask` opent geen database. Het token komt uit `GH_TOKEN`, dan `GITHUB_TOKEN`, dan `gh auth token`.
- Commits volgen Conventional Commits, in het Nederlands, met één logische wijziging per commit. Een wijziging onder `pack/` draait ook `go run ./cmd/lighthouse init --upgrade` en commit de geïnstalleerde kopie (CI eist `No changes.`). Elke PR krijgt een fragment in `changelog.d/`.
- Verificatie zoals CI: `go test -race -shuffle=on ./...`, `golangci-lint run` en `uv run pytest -q tests/pack/test_protocol.py`.

**Besluit: de volgorde van een start.** Eerst `Prepare` en de bezet-toets, dan `runs.Take`, dan de 🤖 Gestart of Hervat, dan de 🚀 en als laatste `workplace.Launch`. De comment staat zo vóór de launch (spec §3 stap 5, "altijd eerst de comment"), en de rij staat vóór de comment, zodat de wezenregel hem nooit voor een wees aanziet. Mislukt de post, dan sluit apply de rij als `start_failed` met reden `post: <fout>`; er is dan niets gestart. Mislukt de 🚀, dan gaat de launch toch door; de herstelregel van decide zet de 🚀 de volgende tick. Een `NotAccepted` launch laat Gestart staan. *Dedup* start de trigger dan als `attempt = 2`, ongeacht 🚀 of Gestart. Dat is precies het geval dat de spec beschrijft.

**Besluit: wat dit plan uit *Afloop* (§7) neemt.** Alleen wat de keten nodig heeft om rond te komen:

- Een 🤖 Handoff van de run-sessie na de start sluit de rij als `handoff`.
- Een 🤖 Vraag van de run-sessie sluit de rij als `question`.
- Een groene PR met een sessie die niet `working` is, geeft eerst 🤖 Klaar en sluit dan de rij als `done`.
- Een 🤖 Klaar van de run-sessie die al staat, sluit alleen de rij. Dat is de "volgende tick maakt de overgang af".

Al het andere uit §7, ook `CheckOpen`, hoort in #115.

**Besluit: teksten op het issue.** De redenen en de Klaar-tekst krijgen sleutels in `pack/core/headings.toml` (sectie `# Texts the dispatcher posts`), met `nl` en `en`. Zo vertaalt het posten de Engelse redenen uit #113 naar de projecttaal (`docs/workplace.md` belooft dat). De sleutels en de teksten:

| Sleutel | nl | en |
|---|---|---|
| `reason_needs_brainstorm` | heeft brainstorm nodig | needs a brainstorm |
| `reason_interactive` | vraagt interactieve uitvoering | asks for interactive execution |
| `reason_skill_missing` | skill niet geïnstalleerd in de worktree | skill not installed in the worktree |
| `reason_not_allowed` | commando niet toegestaan | command not allowed |
| `reason_bad_handoff` | handoff onleesbaar | handoff unreadable |
| `reason_state_lost` | runstaat kwijt | run state lost |
| `reason_start_failed` | start mislukt | start failed |
| `skip_spec_review` | spec-review overgeslagen (config) | spec review skipped (config) |
| `skip_plan_review` | plan-review overgeslagen (config) | plan review skipped (config) |
| `note_done` | Checks groen, klaar voor review. Mergen blijft bij jou; na de merge toetst de controller het issue opnieuw als het nog open staat. | Checks green, ready for review. Merging stays with you; after the merge the controller checks the issue again if it is still open. |

Een reden met een argument wordt `<tekst>: <argument>`. Het argument is het commando, de skill of de foutmelding, letterlijk.

## Review Focus

1. **Twee processen, één trigger.** De tick en `lighthouse run` beslissen tegelijk over hetzelfde antwoord. Verwacht: precies één hervatting en één 🤖 Hervat. De tweede `Take` geeft `ErrClaimed` of `ErrTriggerSpent`, en apply post dan niets. → Taak 7, `TestApplyClaimLostPostsNothing`.
2. **Een Vraag of Handoff zonder sessie in de voet**, of met de sessie van een andere run. Verwacht: die sluit geen open rij, en een antwoord erop hervat niets. → Taak 5, fixture `settle-question-other-session`, en Taak 6, fixture `answer-question-without-session`.
3. **Een Handoff met `issue: #12` op issue 13**, of een `next` met spaties, aanhalingstekens of `;`. Verwacht: Teruggegeven `commando niet toegestaan: …`, nooit een start. → Taak 4, `TestJudgeArgumentShapes`, en Taak 6, fixture `handoff-wrong-issue`.
4. **Een nachtvenster over middernacht** (`21:00-08:00`) om 07:59, 08:00 en 20:59. Verwacht: binnen, buiten, buiten. → Taak 4, `TestInNight`.
5. **`lighthouse ask` met een `--option` vóór de eerste `--question`**, met twee `--recommended` bij één vraag, of met een `--recommended` die wijst naar een optie die er niet is. Verwacht: een melding, niets gepost, exit 1. → Taak 10, `TestAskRejectsShape`.

---

## Bestanden

| Bestand | Verantwoordelijkheid |
|---|---|
| `pack/core/headings.toml`, `.lighthouse/headings.toml` (via `init --upgrade`) | Teksten op het issue |
| `internal/protocol/header.go`, `heading.go` (nieuw), `handoff.go` (nieuw), `render.go` (nieuw), `question.go` (nieuw) (+ tests) | Sessie uit de voet, `Heading`, `ParseHandoff`, renderers, `ParseQuestion`, `AnswerPrompt` |
| `tests/pack/fixtures/protocol/questions.json` (nieuw), `tests/pack/test_protocol.py` | Gedeelde fixture voor render en parse van een Vraag |
| `internal/repoconfig/repoconfig.go` (+ test) | `Kind`, `Language` |
| `internal/forge/board.go` (+ test) | `PR.URL`, `PR.MergedAt` |
| `internal/runs/history.go` (nieuw, + test) | `History` |
| `internal/dispatch/judge.go` (nieuw, + test) | Allowlist, oordeel, review-sprong, nachtvenster, slug |
| `internal/dispatch/decide.go` (nieuw), `decide_test.go`, `testdata/decide/*.json` | De regels |
| `internal/dispatch/apply.go` (nieuw, + test) | Acties uitvoeren |
| `internal/dispatch/instructions.go`, `instructions/run.md`, `instructions/round_limit_question.md` (nieuw, + test) | Ingebedde run-instructies |
| `internal/dispatch/gather.go`, `internal/cli/tick.go`, `internal/cli/gather.go` (+ tests) | Decide en apply in de tick |
| `internal/cli/run.go`, `internal/cli/ask.go` (nieuw, + tests), `internal/cli/root.go` | De twee commando's |
| `docs/tick.md`, `docs/workplace.md`, `docs/ask.md` (nieuw), `changelog.d/issue-114-decide-apply-keten.md` | Documentatie |

---

### Taak 1: Protocol-twin: sessie, kopteksten en Handoff

**Files:**
- Modify: `internal/protocol/header.go`, `pack/core/headings.toml`, `tests/pack/fixtures/protocol/headers.json`
- Create: `internal/protocol/heading.go`, `internal/protocol/handoff.go`
- Test: `internal/protocol/header_test.go`, `internal/protocol/heading_test.go`, `internal/protocol/handoff_test.go`

**Interfaces:**
- Produces:
  ```go
  type Header struct { Kind Kind; Fields map[string]string; Session string } // Session: last `<sub>session|sessie <uuid>` in the body, "" when none
  func Heading(key, language string) string // the variant of language; unknown language falls back to "en"; unknown key panics (embedded table)
  func Variants(key string) []string         // every variant, for reading
  type Handoff struct { Issue int; Worktree, Branch, Anchor, Next, When, Lane string }
  var ErrHandoff = errors.New("protocol: handoff")
  func ParseHandoff(h Header) (Handoff, error) // errors wrap ErrHandoff; text as in parse_handoff of protocol.py
  ```
- `ParseHandoff` volgt `parse_handoff` uit `.lighthouse/lib/lighthouse_lib/protocol.py`. Verplicht zijn `issue`, `worktree`, `branch`, `next` en `when`. `issue` past op `#?[0-9]+`, `when` is `now` of `night` (na de legacy-vertaling), en `Worktree` verliest zijn slash aan het eind.

- [ ] **Step 1: Failing tests.**
  - `TestParseHeaderSharedFixture` vergelijkt ook `session`. Voeg aan `headers.json` twee gevallen toe: `session-footer`, een voet `<sub>session 1b2c…</sub>` die `session` geeft, en `sessie-legacy-footer`, `<sub>sessie …</sub>` met twee voeten, waarvan de laatste wint. Breid de Python-test uit met dezelfde vergelijking (`header.session`).
  - `TestHeadingLanguages`: `Heading("kind_done","nl") == "Klaar"`, `Heading("kind_done","de") == "Done"`, `Heading("reason_state_lost","nl") == "runstaat kwijt"`.
  - `TestHeadingUnknownKeyPanics`.
  - `TestParseHandoff` is een tabel: alle velden geldig; `wanneer: nacht` wordt `When == "night"`; elk ontbrekend verplicht veld geeft een fout met `"fields missing: <key>"`; `issue: abc`; `when: later`; `lane: native` wordt bewaard.
- [ ] **Step 2:** `go test ./internal/protocol` en `uv run pytest -q tests/pack/test_protocol.py` → FAIL.
- [ ] **Step 3:** Implementeer de code. `Heading` en `Variants` lezen dezelfde `sync.OnceValue`-tabel als `kindVariants`. Zet de sleutels uit de teksttabel bovenaan in `pack/core/headings.toml`, en draai `go run ./cmd/lighthouse init --upgrade`.
- [ ] **Step 4:** `go test -race ./internal/protocol ./internal/packs` en `uv run pytest -q tests/pack` → PASS; `go run ./cmd/lighthouse init --upgrade` → `No changes.`
- [ ] **Step 5:** Commit `feat(protocol): sessie uit de voet, Handoff en de teksten van de dispatcher`.

### Taak 2: Protocol-twin: renderers, Vraag en antwoordprompt

**Files:**
- Create: `internal/protocol/render.go`, `internal/protocol/question.go`, `tests/pack/fixtures/protocol/questions.json`
- Modify: `tests/pack/test_protocol.py`
- Test: `internal/protocol/render_test.go`, `internal/protocol/question_test.go`

**Interfaces:**
- Consumes: Taak 1 (`Heading`, `Variants`).
- Produces (een woord-voor-woord twin van de Python-functies met dezelfde naam):
  ```go
  func Footer(session string, extra ...string) string
  type Started struct { Session, Worktree, Command, Start, Branch, Skip, Runner, Workspace string }
  func RenderStarted(s Started, language string) string          // render_started; only these fields
  func RenderResumed(session string, answerID int64, language string) string
  func RenderDone(session, prURL, note, language string) string
  func RenderReturned(reason, session, detail, language string) string
  type Option struct { Text string `json:"text"`; Recommended bool `json:"recommended"` }
  type Question struct { Question string `json:"question"`; Options []Option `json:"options"` }
  type QuestionFields struct {
      Phase string `json:"phase"`; Task string `json:"task"`; Where string `json:"where"`; What string `json:"what"`
      Commands []string `json:"commands"`; Questions []Question `json:"questions"`
  }
  const MaxQuestions = 4
  const Letters = "abcdef"
  type QuestionMeta struct { Issue int; Title, IssueURL, Session, Worktree, HandoffURL string }
  func RenderQuestion(m QuestionMeta, f QuestionFields, language string) string
  func ParseQuestion(body string) (QuestionFields, bool) // false: no 🤖 Question
  func AnswerPrompt(question, answer string) string
  ```
- `AnswerPrompt` is de Engelse versie van `antwoord_prompt`. Het laat de regels die met `<sub>` beginnen weg, en ook de regels die gelijk zijn aan een variant van `question_close` of `question_bundle_hint`. Daarna komt letterlijk:
  `"Your question stands on the issue like this; the controller added the option letters:\n\n" + posted + "\n\nThe user's answer:\n\n" + strings.TrimSpace(answer)`.
- `questions.json` is een lijst van `{"name", "meta", "fields", "language", "body"}`. Beide talen lezen hem: `render(meta, fields, language) == body` en `parse(body) == fields`. Cases: één vraag met `commands` (`nl`), een bundel van twee vragen (`en`), een `task`, en een optie met backticks en `<sub>` (via `_clean`). Python roept `render_question` en `parse_question` aan.

- [ ] **Step 1: Failing tests.**
  - `TestQuestionSharedFixture` (Go) en `test_question_shared_fixture` (Python) lezen `questions.json`.
  - `TestRenderStartedFields`: de lege velden ontbreken en de volgorde is `session, worktree, command, start, branch, skip, runner, workspace`, met de voet aan het eind.
  - `TestRenderReturnedDetail`: detail met ```` ``` ```` wordt `'''`.
  - `TestAnswerPromptDropsFooterAndClose`: geen `<sub>` en geen slotregel; het antwoord staat er letterlijk.
  - `TestParseHeaderOfRendered`: elke renderer geeft een body die `ParseHeader` terugleest met dezelfde `Kind` en `Session`.
- [ ] **Step 2:** `go test ./internal/protocol` en `uv run pytest -q tests/pack/test_protocol.py` → FAIL.
- [ ] **Step 3:** Implementeer de code, regel voor regel naar `protocol.py` (`_clean`, `_code_block`, `_question_pieces`, `_add_option`).
- [ ] **Step 4:** Draai beide testsuites → PASS.
- [ ] **Step 5:** Commit `feat(protocol): renderers en de Vraag-parser als Go-twin`.

### Taak 3: Config, PR-velden en runhistorie

**Files:**
- Modify: `internal/repoconfig/repoconfig.go`, `internal/forge/board.go`, `internal/dispatch/testdata/snapshot.json`
- Create: `internal/runs/history.go`
- Test: `internal/repoconfig/repoconfig_test.go`, `internal/forge/board_test.go`, `internal/runs/history_test.go`

**Interfaces:**
- Produces:
  - `repoconfig.Repo` krijgt `Kind string` (`[forge] kind`, standaard `"github"`) en `Language string` (`[project] language`, standaard `"en"`).
  - `forge.PR` krijgt `URL string \`json:"url"\`` en `MergedAt time.Time \`json:"merged_at"\``. `prFields` wordt `number state isDraft mergeable headRefOid headRefName mergedAt url statusCheckRollup { state }`. `SnapshotVersion` blijft `1`, want de velden zijn toegevoegd.
  - ```go
    type Row struct {
        ID int64; Issue int; SessionID, Mode, Trigger, TriggerKey string; Attempt int
        Command, Prompt, Branch, Profile string
        StartedAt time.Time; Open bool; Outcome Outcome; Reason string
    }
    func History(ctx context.Context, db *sql.DB, repo string) ([]Row, error) // every row of repo, open and closed, by id
    ```

- [ ] **Step 1: Failing tests.**
  - `TestLoadKindAndLanguage` leest de waarden en de standaarden.
  - `TestBoardItemsPRURLAndMergedAt`: de forgetest-server geeft `mergedAt` en `url` mee, en de items dragen ze.
  - `TestHistoryOpenAndClosed`: na `Take` en `Release(StartFailed, "x")` en een tweede `Take` geeft `History` twee rijen in id-volgorde. De eerste is `Open == false`, `Outcome == StartFailed`, `Reason == "x"`; de tweede is `Open == true` met `Attempt == 2`. Een rij van een andere repo ontbreekt.
- [ ] **Step 2:** `go test ./internal/repoconfig ./internal/forge ./internal/runs` → FAIL.
- [ ] **Step 3:** Implementeer de code. Werk `testdata/snapshot.json` en de GraphQL-antwoorden in `internal/forge/forgetest` bij.
- [ ] **Step 4:** `go test -race ./internal/repoconfig ./internal/forge/... ./internal/runs ./internal/dispatch` → PASS.
- [ ] **Step 5:** Commit `feat(dispatch): taal, PR-merge en runhistorie voor decide`.

### Taak 4: Het oordeel over een commando

**Files:**
- Create: `internal/dispatch/judge.go`
- Test: `internal/dispatch/judge_test.go`

**Interfaces:**
- Produces:
  ```go
  type Verdict int // VerdictStart, VerdictEnd, VerdictBrainstorm, VerdictInteractive, VerdictRejected, VerdictSkillMissing
  type Judgement struct { Verdict Verdict; Command string; Native bool; Arg string } // Command: rewritten (/paia- → /lighthouse-); Arg: the reason argument
  func Judge(next string, issue int, lane string, installed map[string]bool) Judgement
  func ReviewSkip(next string, c repoconfig.Controller) (command, skipKey string) // skipKey "" or skip_spec_review / skip_plan_review
  func InNight(night string, now time.Time) bool // "HH:MM-HH:MM" in now's zone; a window across midnight wraps; unreadable is false
  func Executing(command string) bool            // not spec-adversarial-review, plan-adversarial-review or writing-plans (VOOR_UITVOERING)
  func Slug(title string) string                 // lower case, every run outside [a-z0-9] becomes one "-", trimmed, at most 40 bytes cut back to a "-"
  func Installed(dir string) (map[string]bool, error) // names of <dir>/.claude/skills/*/SKILL.md, e.g. "lighthouse-start-issue"
  ```
- De volgorde van `Judge` is die van `beoordeel_commando`:
  1. leeg → Rejected met Arg `(empty)`;
  2. eindcommando → End;
  3. brainstorm → Brainstorm;
  4. interactief zonder `lane: native` → Interactive;
  5. `lane` niet leeg en niet (`native` met interactief) → Rejected met Arg `next`;
  6. niet op de allowlist en niet native → Rejected met Arg `next`;
  7. een `/lighthouse-<naam>` waarvan `installed["lighthouse-<naam>"]` false is → SkillMissing met Arg `lighthouse-<naam>`;
  8. het argument heeft de verkeerde vorm → Rejected met Arg `next`;
  9. anders Start, en `Native` is true bij interactief met native.

  `ReviewSkip` springt zoals `_REVIEW_SPRONG`: van spec-review naar `/superpowers:writing-plans <arg>`, van plan-review naar `/superpowers:subagent-driven-development <arg>`.

- [ ] **Step 1: Failing tests.**
  - `TestJudgeVerdicts` is een tabel met één rij per stap, ook `/paia-start-issue 7`, dat Start wordt met Command `/lighthouse-start-issue 7`.
  - `TestJudgeArgumentShapes`:
    - `start-issue 8` op issue 7 → Rejected;
    - `fix-pr 12a` → Rejected;
    - `writing-plans relative.md` → Rejected;
    - `writing-plans /a/b.md; rm` → Rejected;
    - `writing-plans /a/b.md` → Start.
  - `TestReviewSkip`: aan → ongewijzigd; `SpecReview=false` → writing-plans met hetzelfde pad en `skip_spec_review`.
  - `TestInNight`: venster `21:00-08:00` om 20:59 → false, 21:00 → true, 07:59 → true, 08:00 → false. Venster `01:00-03:00` om 02:00 → true. `"x"` → false.
  - `TestSlug`: `"De keten in decide en apply: handoff, start"` → `"de-keten-in-decide-en-apply-handoff"`; `"Één"` → `"n"`; `""` → `""`.
  - `TestInstalled` werkt op `t.TempDir()`.
- [ ] **Step 2:** `go test ./internal/dispatch -run 'Judge|ReviewSkip|InNight|Slug|Installed'` → FAIL.
- [ ] **Step 3:** Implementeer de code.
- [ ] **Step 4:** `go test -race ./internal/dispatch` → PASS.
- [ ] **Step 5:** Commit `feat(dispatch): allowlist, oordeel en nachtvenster`.

### Taak 5: Decide: typen, wezen, afloop van de keten en herstel

**Files:**
- Create: `internal/dispatch/decide.go`, `internal/dispatch/testdata/decide/*.json`
- Test: `internal/dispatch/decide_test.go`

**Interfaces:**
- Consumes: Taak 1–4.
- Produces:
  ```go
  type Input struct { Now time.Time; Night string; Repos []RepoInput }
  type RepoInput struct {
      Snapshot   Snapshot
      Controller repoconfig.Controller
      Language   string
      Installed  map[string]bool
      History    []runs.Row
      Stands     map[int64]workplace.Stand // per open run id; missing = not measurable
  }
  type Kind string // "start", "resume", "return", "done", "close", "rocket", "defer"
  type Action struct {
      Kind Kind `json:"kind"`; Repo string `json:"repo"`; Issue int `json:"issue"`
      RunID int64 `json:"run_id,omitempty"`; Outcome runs.Outcome `json:"outcome,omitempty"`
      Trigger string `json:"trigger,omitempty"`; TriggerKey string `json:"trigger_key,omitempty"`; Attempt int `json:"attempt,omitempty"`
      Mode string `json:"mode,omitempty"`; SessionID string `json:"session_id,omitempty"` // resume: the session; start: "" (apply makes one)
      Command string `json:"command,omitempty"`; Prompt string `json:"prompt,omitempty"`; Branch string `json:"branch,omitempty"`
      Native bool `json:"native,omitempty"`; Skip string `json:"skip,omitempty"` // a headings key
      CommentID int64 `json:"comment_id,omitempty"` // 🚀 target; for resume also the answer id of 🤖 Resumed
      Reason string `json:"reason,omitempty"`; ReasonArg string `json:"reason_arg,omitempty"` // a headings key and its argument
      PRURL string `json:"pr_url,omitempty"`; Language string `json:"language,omitempty"`
  }
  func Decide(in Input) []Action
  ```
- De fixture `testdata/decide/<naam>.json` is `{"input": Input, "want": []Action}`. `TestDecideFixtures` leest elke file, vergelijkt `Decide(input)` met `want` via `reflect.DeepEqual` en print het verschil als JSON. Een lege map faalt.
- Een 🤖-comment hoort **bij run R** als `Header.Session == R.SessionID` en `CreatedAt` niet vóór `R.StartedAt` ligt.
- Regels van deze taak, per issue in deze volgorde:
  1. **Wees**: de laatste 🤖-comment (Rebased telt niet) is Started of Resumed, met een `Session` die in geen enkele `History`-rij staat → `return` met `reason_state_lost` en `SessionID` (voor de voet).
  2. **Afloop, per open rij R**:
     - een 🤖 Done van R → `close` met `done`;
     - anders een 🤖 Handoff van R → `close` met `handoff`;
     - anders een 🤖 Question van R → `close` met `question`;
     - anders een groene PR in `Item.PRs` en `Stands[R.ID]` gemeten en niet `Working` → `done` (Klaar met `PRURL`, daarna sluiten als `done`; `RunID`, `SessionID`).
  3. **Herstel**: per `History`-rij met trigger-sleutel `handoff:<id>` of `answer:<id>` waarvan die comment in de snapshot staat zonder `Rocket` → één `rocket` met `CommentID`.

  Een issue met een open rij die in deze tick niet sluit, krijgt geen startactie (Taak 6). Elke actie draagt `Language` van de repo.

- [ ] **Step 1: Failing tests.** Fixtures (elk met één issue in Controller, tenzij anders):
  - `orphan-started`: laatste 🤖 Gestart met sessie S, `History` leeg → `return` `reason_state_lost` met S.
  - `orphan-known-session`: S staat in een dichte rij → `[]`.
  - `orphan-after-returned`: Gestart en daarna Teruggegeven → `[]`.
  - `settle-handoff`, `settle-question` en `settle-done-posted` → één `close` met de juiste uitkomst.
  - `settle-question-other-session`: Vraag met een andere sessie → `[]`.
  - `settle-question-before-start`: Vraag van S, maar vóór `StartedAt` (een oudere run met dezelfde sessie) → `[]`.
  - `settle-green-pr-done`: Stand `Waiting` → `done`.
  - `settle-green-pr-working`: Stand `Working` → `[]`.
  - `settle-green-pr-unmeasured`: geen Stand → `[]`.
  - `settle-red-pr`: `Checks: FAILURE` → `[]`.
  - `repair-rocket`: rij `handoff:55` dicht als `done`, comment 55 zonder 🚀 → `rocket` 55.
- [ ] **Step 2:** `go test ./internal/dispatch -run TestDecideFixtures` → FAIL.
- [ ] **Step 3:** Implementeer `Decide` met de regels van deze taak. Hulpfunctie `func headers(it Item) []parsed` met `{Comment, Header, Time}`, oudste eerst. Een `CreatedAt` die niet te lezen is, telt als `time.Time{}`.
- [ ] **Step 4:** `go test -race ./internal/dispatch` → PASS.
- [ ] **Step 5:** Commit `feat(dispatch): decide voor wezen, afloop van de keten en herstel`.

### Taak 6: Decide: antwoorden, nacontrole, handoffs, dedup en volgorde

**Files:**
- Modify: `internal/dispatch/decide.go`
- Create: meer fixtures in `internal/dispatch/testdata/decide/`
- Test: `internal/dispatch/decide_test.go`

**Interfaces:**
- Consumes: Taak 5.
- Regels, na die van Taak 5. Alleen voor items met `Status == "Controller"` en `State == "OPEN"`, zonder open rij (of met een open rij die in deze tick sluit). Hoogstens één `start` of `resume` per issue, en de eerste regel die raakt, wint:
  1. **Tweede poging** (*Dedup*): een trigger-sleutel van het issue waarvan alle `History`-rijen `start_failed` zijn:
     - één rij → dezelfde `Mode`, `Trigger`, `TriggerKey`, `Command`, `Prompt` en `Branch`, met `Attempt: 2` (`resume` behoudt `SessionID`, `start` laat hem leeg);
     - twee rijen, en nog geen 🤖 Returned van de sessie van de tweede rij → `return` met `reason_start_failed`, `ReasonArg` = de reden van de tweede rij en `SessionID` van die rij.
     Dit geldt ongeacht 🚀 of 🤖 Gestart.
  2. **Antwoord** (§5): de laatste 🤖 is een Question met een `Session`, en het antwoord is de eerste comment erna die geen 🤖-kop en geen `Rocket` heeft en waarvoor `protocol.IsAnswer` geldt. Heeft `answer:<id>` geen rij → `resume` met `Trigger: "answer"`, `SessionID` = de sessie van de Vraag, `CommentID` = het antwoord, `Prompt` = `protocol.AnswerPrompt(vraag.Body, antwoord.Body)`, `Branch` en `Command` van de laatste `History`-rij met die sessie, en `Attempt: 1`. Een Vraag zonder sessie → niets.
  3. **Nacontrole** (§4 regel 5): een PR met `Closes == false`, `State == "MERGED"` en een 🤖 Started waarvan `CreatedAt` vóór `MergedAt` ligt. Heeft `closecheck:<pr>` geen rij → `start` met `Trigger: "closecheck"`, `Command: "/lighthouse-close-issue <N>"`, de prompt met het achtervoegsel, `Branch` = `HeadRef` van de PR, en `Attempt: 1`. Een gemergde PR met `Closes == true` → niets.
  4. **Nieuw issue**: geen enkele 🤖-comment, en `start:<N>` heeft geen rij → `start` met `Command: "/lighthouse-start-issue <N>"`, `Branch: "issue-<N>-" + Slug(title)` (zonder slug: `issue-<N>`), `Trigger: "handoff"` en `Attempt: 1`.
  5. **Handoff**: de laatste 🤖 is een Handoff zonder `Rocket`, en `handoff:<id>` heeft geen rij. Dan:
     - `ParseHandoff` faalt → `return` met `reason_bad_handoff` en de fout;
     - `Handoff.Issue != N` → `return` met `reason_not_allowed` en `next`;
     - anders `ReviewSkip`, dan `Judge`. End → niets; Brainstorm → `return` met `reason_needs_brainstorm`; Interactive → `return` met `reason_interactive`; Rejected → `return` met `reason_not_allowed`; SkillMissing → `return` met `reason_skill_missing`;
     - bij Start, en `When == "night"` terwijl `!InNight` → `defer` met `Reason: "night"`;
     - bij Start, en `Executing(command)` met een `BlockedBy` die `OPEN` is → `defer` met `Reason: "blocker"` (de blokkaderegel volgt in issue 7);
     - anders `start` met `Trigger: "handoff"`, `CommentID` = de Handoff, `Branch` = `Handoff.Branch`, `Native`, `Skip` = de skip-sleutel en `Attempt: 1`.
- **Volgorde** van de uitkomst: eerst alle acties zonder start of hervatting (op repo en issue), dan de `defer`'s, dan `start` en `resume`. Die laatste gaan op Priority (`P0 < P1 < P2 < ""`), dan op de tijd van de trigger (de `CreatedAt` van de Handoff of het antwoord, bij `start:`/`closecheck:` `ItemUpdatedAt`), dan op repo en issue.

- [ ] **Step 1: Failing tests.** Fixtures:
  - `answer-resume`;
  - `answer-already-rocket` → `[]`;
  - `answer-question-without-session` → `[]`;
  - `answer-row-exists` (de rij van `answer:<id>` bestaat al, dicht als `question`; het antwoord draagt een 🚀) → `[]`;
  - `closecheck-refs-merged` → `start` close-issue;
  - `closecheck-closes-merged` → `[]` (**AC 3**);
  - `closecheck-started-after-merge` → `[]`;
  - `new-issue-start`;
  - `new-issue-on-hold` (Status On hold) → `[]`;
  - `handoff-start`;
  - `handoff-native` (`executing-plans` met `lane: native` → `Native: true`);
  - `handoff-interactive` → precies `[return reason_interactive]` (**AC 4**: geen boardactie, het issue blijft in Controller);
  - `handoff-brainstorm`, `handoff-end` → `[]`;
  - `handoff-not-allowed`, `handoff-wrong-issue`, `handoff-skill-missing`, `handoff-unreadable`;
  - `handoff-night-defer`, `handoff-night-in-window` → `start`;
  - `handoff-spec-review-skip` (`SpecReview: false` → Command writing-plans, `Skip: "skip_spec_review"`);
  - `handoff-blocker-executing-defer`;
  - `handoff-blocker-spec-review-starts`;
  - `dedup-row-not-failed`: de rij `handoff:<id>` is dicht als `done` en de Handoff draagt een 🚀 → `[]`. `dedup-row-not-failed-no-rocket`: dezelfde rij, de Handoff zonder 🚀 → alleen `rocket`, geen `start` (**AC 2a**);
  - `dedup-one-start-failed`: één rij `start_failed`; de Handoff draagt 🚀 en daarna staat een 🤖 Gestart van die rij → `start` met `Attempt: 2` (**AC 2b**);
  - `dedup-one-start-failed-resume` → `resume` met dezelfde `SessionID` en `Attempt: 2`;
  - `dedup-two-start-failed` → precies één `return` met `reason_start_failed` (**AC 2c**);
  - `dedup-two-start-failed-returned` (die Teruggegeven staat er al) → `[]`;
  - `open-run-blocks-start` (open rij die niet sluit, plus een nieuwe Handoff) → `[]`;
  - `order-priority` (twee repo's: P1 oud, P0 nieuw, en een `return` → `return`, dan P0, dan P1).
- [ ] **Step 2:** `go test ./internal/dispatch -run TestDecideFixtures` → FAIL.
- [ ] **Step 3:** Implementeer de code.
- [ ] **Step 4:** `go test -race ./internal/dispatch` → PASS. Mutatiecheck: maak in regel 1 van `start_failed` een `!= ""`. `dedup-row-not-failed` moet dan falen. Zet de mutatie terug.
- [ ] **Step 5:** Commit `feat(dispatch): decide voor antwoord, nacontrole, handoff en dedup`.

### Taak 7: Run-instructies en apply

**Files:**
- Create: `internal/dispatch/instructions.go`, `internal/dispatch/instructions/run.md`, `internal/dispatch/instructions/round_limit_question.md`, `internal/dispatch/apply.go`
- Test: `internal/dispatch/instructions_test.go`, `internal/dispatch/apply_test.go`

**Interfaces:**
- Consumes: Taak 2 (renderers), Taak 5 (`Action`), `runs.Take/Release/SessionKeys/Open`, `workplace.Launch/Busy/BusyReason`.
- Produces:
  ```go
  func WriteInstructions(stateDir, sessionID, atRoundLimit string) (string, error) // <stateDir>/runs/<sessionID>/instructions.md, 0600, dir 0700; appends round_limit_question.md when atRoundLimit == "vraag"
  type Poster interface {
      CreateComment(ctx context.Context, number int, body string) (protocol.Comment, error)
      AddRocket(ctx context.Context, commentID int64) error
  }
  type Env struct {
      DB         *sql.DB
      Poster     func(repo string) Poster
      Workplace  func(repo string) workplace.Workplace
      Profile    func(repo string) (name, mcpConfig string)
      RoundLimit func(repo string) string
      StateDir   string
      MaxWait    time.Duration
      Now        func() time.Time
      NewSession func() string // runs.NewSessionID
      Log        io.Writer
  }
  type Result struct { Deferred []string; Started int } // Deferred: "<repo>#<n>: <reason>"
  func Apply(ctx context.Context, env Env, actions []Action) (Result, error) // runs every action; errors are joined, one failing action never stops the rest
  ```
- Per soort:
  - `return`: post `RenderReturned(text, SessionID, "", Language)` met `text = Heading(Reason) [+ ": " + ReasonArg]`.
  - `done`: post `RenderDone(SessionID, PRURL, Heading("note_done"), Language)`, dan `runs.Release(RunID, Done, "")`.
  - `close`: `runs.Release(RunID, Outcome, "")`. Een `ErrNotOpen` is geen fout.
  - `rocket`: `AddRocket(CommentID)`.
  - `defer`: zet de reden in `Result.Deferred`.
  - `start` en `resume`:
    1. `ws := Prepare(Branch)`;
    2. `Sessions(ws.ID)`, met `own` uit `runs.SessionKeys` en de open sessie-ids van het issue. Is `Busy` waar, dan komt er een deferral met `"workplace busy: " + BusyReason`;
    3. `runs.Take`. `ErrClaimed` of `ErrTriggerSpent` → een logregel en verder niets;
    4. `WriteInstructions`;
    5. de comment: Started (`Start = now` in RFC3339, `Worktree = ws.Path`, `Branch`, `Skip` als `Heading(Skip)`, `Runner: "forge"`, `Workspace: ws.ID`), of `RenderResumed(SessionID, CommentID)`. Mislukt de post, dan `runs.Release(StartFailed, "post: <fout>")` en stop;
    6. `AddRocket(CommentID)` als `CommentID > 0`. Een fout komt alleen in het log;
    7. `workplace.Launch` met een `LaunchSpec` (`Prompt`, `Native`, `InstructionsFile`, `MCPConfig`, `Profile`, `Mode` en de sessie). `Accepted` of `Uncertain` telt in `Started`. `NotAccepted` laat alleen een logregel achter.
- `run.md` is een Engelse port van `controller_forge_instructies.md`, met deze wijzigingen:
  - `scripts/controller_vraag.py` wordt `lighthouse ask` met de Engelse vlaggen uit Taak 10, en het voorbeeld wordt herschreven;
  - `PAIA_CONTROLLER_RUN` wordt `LIGHTHOUSE_CONTROLLER_RUN`;
  - bij een post-fout geeft het commando exit 1, en bij een vorm-fout zegt het "nothing posted".
  
  Wat paia-specifiek is, valt weg: tenets- en areapaden die lighthouse niet heeft, en `controller_auto`. `round_limit_question.md` is de port van `controller_forge_rondegrens_vraag.md`.

- [ ] **Step 1: Failing tests.** Elke test gebruikt een echte database (`store.OpenShared` op `t.TempDir()`), een fake `Poster` en een fake `Workplace` die zijn aanroepen opneemt.
  - `TestApplyStartOrder`: de volgorde van de aanroepen is Prepare, Sessions, CreateComment, AddRocket, Prepare, Spawn. De rij is open, en de Started-body draagt de sessie van de rij.
  - `TestApplyPostFailsReleases`: CreateComment faalt → de rij is dicht als `start_failed` met de reden `post: …`, en er is geen Spawn.
  - `TestApplyRocketFailsStillLaunches`.
  - `TestApplyBusyDefers`: een vreemde sessie die `working` is → er is geen rij, geen post en geen Spawn, en `Deferred` heeft één regel.
  - `TestApplyClaimLostPostsNothing`: er is al een open rij voor het issue → geen post en geen Spawn.
  - `TestApplyResumeKeepsSession`: de rij heeft `mode = resume` en dezelfde `session_id`, en de Resumed-body draagt `answer: <id>`.
  - `TestApplyReturnTranslates`: `Language: "nl"` en `reason_not_allowed` met arg `/x` → de body bevat `reason: commando niet toegestaan: /x`.
  - `TestApplyDonePostsThenCloses`: na de post sluit de rij als `done`. Faalt de post, dan blijft de rij open.
  - `TestApplyOneFailureDoesNotStopOthers`.
  - `TestWriteInstructions`: de modus is 0600, de inhoud is gelijk aan `run.md`, en met `"vraag"` wordt de appendix toegevoegd.
- [ ] **Step 2:** `go test ./internal/dispatch -run 'Apply|WriteInstructions'` → FAIL.
- [ ] **Step 3:** Implementeer de code. Gebruik `//go:embed instructions/*.md`.
- [ ] **Step 4:** `go test -race ./internal/dispatch` → PASS.
- [ ] **Step 5:** Commit `feat(dispatch): apply en de ingebedde run-instructies`.

### Taak 8: Decide en apply in de tick

**Files:**
- Modify: `internal/dispatch/gather.go`, `internal/cli/tick.go`, `internal/cli/gather.go`, `docs/tick.md`
- Test: `internal/cli/tick_test.go`, `internal/dispatch/gather_test.go`

**Interfaces:**
- Consumes: Taak 3 (`History`, `Kind`, `Language`), Taak 4 (`Installed`), Taak 5–7.
- Produces:
  - `dispatch.Prepare(ctx, c, repo, open []runs.OpenRun, branches map[int]string)`. `branches` is per issue de branch van de laatste `History`-rij. De PR's op die branch komen erbij, ook na een dichte run, zodat een gemergde `Refs`-PR zichtbaar blijft. Pas elke aanroep aan (`grep -rn 'dispatch.Prepare(' internal`).
  - `tickEnv` krijgt `Workplace func(repo repoconfig.Repo) workplace.Workplace` (nil → een `kennforge.Adapter` uit `cfg.Forge`, `cfg.Profiles`, `repo.Kind`, `repo.Slug`, `repo.Dir` en `repo.Controller.Profile`) en `StateDir string` ("" → `${XDG_STATE_HOME:-$HOME/.local/state}/lighthouse/dispatch`).
  - `gatherRepo` geeft `(in *dispatch.RepoInput, fp string, err error)` terug, met nil als de repo niet aan de beurt is. `Stands` komt uit `Sessions` per open rij met `WorkspaceID` en `WorkplaceSession`; de sessie met die key geeft de stand, en een fout laat de rij weg (niet meetbaar).
  - `tick` verzamelt de inputs, roept één keer `Decide` aan over alle repo's, en dan `Apply`. Daarna bewaart hij de vingerafdruk alleen voor een repo zonder `Deferred` en zonder fout in `Apply` voor die repo. Dat is spec §3 stap 2.

- [ ] **Step 1: Failing tests** in `tick_test.go`, met forgetest en een fake `Workplace`:
  - `TestTickStartsNewIssue`: een item in Controller zonder comments → één Started-comment op de server, één open rij, en de vingerafdruk is bewaard.
  - `TestTickDeferredKeepsFingerprint`: een Handoff met `when: night` buiten het venster → geen rij, en `LoadTickState` is leeg.
  - `TestTickSecondTickDoesNotRestart`: twee ticks na elkaar → één rij en één Started.
  - Pas de bestaande tests aan op de nieuwe uitvoer: de regel `gathered` blijft.
- [ ] **Step 2:** `go test ./internal/cli -run Tick` → FAIL.
- [ ] **Step 3:** Implementeer de code. Werk in `docs/tick.md` de alinea "Tick beslist nog niet…" bij tot stap 8 *Decide* en stap 9 *Apply*, met de volgorde van een start en het besluit over de vingerafdruk.
- [ ] **Step 4:** `go test -race ./internal/cli ./internal/dispatch ./e2e/...` → PASS.
- [ ] **Step 5:** Commit `feat(tick): decide en apply na de gather`.

### Taak 9: `lighthouse run <repo>#<issue>`

**Files:**
- Create: `internal/cli/run.go`
- Modify: `internal/cli/root.go`, `docs/tick.md`
- Test: `internal/cli/run_test.go`

**Interfaces:**
- Consumes: Taak 8 (`gatherRepo` zonder precheck: een functie `inputFor(ctx, env, db, cfg, repo, token, login) (dispatch.RepoInput, error)` die `gatherRepo` en `run` delen).
- Produces: `lighthouse run <owner/name>#<n> [--db <pad>]`.
  - Het commando zoekt de repo in `[dispatch] repos` op `Slug`; niet gevonden → exit 1.
  - Het neemt geen `tick.lock` en doet geen precheck. Het voert een volledige gather uit, dan `Decide`, en houdt alleen de acties met `Kind` `start` of `resume` en `Issue == n` over. Daarna volgt `Apply`.
  - Uitvoer: `lighthouse: started <repo>#<n>`, `lighthouse: deferred <repo>#<n>: <reden>` of `lighthouse: nothing to start for <repo>#<n>`, alle drie met exit 0.

- [ ] **Step 1: Failing tests:**
  - `TestRunStartsOnlyThatIssue`: twee nieuwe issues → één rij.
  - `TestRunLeavesReturnToTick`: een Handoff die Teruggegeven wordt → geen comment, en `nothing to start`.
  - `TestRunUnknownRepo` → exit 1.
  - `TestRunIgnoresTickLock`: met een bezet `tick.lock` start `run` toch.
- [ ] **Step 2:** `go test ./internal/cli -run TestRun` → FAIL.
- [ ] **Step 3:** Implementeer de code. In `docs/tick.md` komt een sectie `lighthouse run`.
- [ ] **Step 4:** `go test -race ./internal/cli` → PASS.
- [ ] **Step 5:** Commit `feat(cli): lighthouse run voor één issue`.

### Taak 10: `lighthouse ask`

**Files:**
- Create: `internal/cli/ask.go`, `docs/ask.md`
- Modify: `internal/cli/root.go`, `internal/forge/github.go` (`Issue`)
- Test: `internal/cli/ask_test.go`, `internal/forge/github_test.go`

**Interfaces:**
- Consumes: Taak 2 (`RenderQuestion`, `ParseQuestion`, `MaxQuestions`, `Letters`), `workplace.Carrier`.
- Produces:
  - `func (c *Client) Issue(ctx context.Context, number int) (Issue, error)` voor `GET /repos/{repo}/issues/{n}`, met titel en `html_url`.
  - `lighthouse ask --phase T [--task T] --where T --what T (--question T (--option T){2,6} --recommended <a-f>){1,4} [--command T]... [--dry-run]`.
  - `--question`, `--option` en `--recommended` delen één `pflag.Value` die de paren `(vlag, waarde)` in command-line-volgorde opslaat.
  - De regels en meldingen van `controller_vraag.py` (`_groepeer`, `_bouw_vraag`, `_bouw_velden`) gelden, in het Engels: `"--option before the first --question"`, `"<n> questions, want 1 to 4"`, `"question <n>: <k> options, want 2 to 6"`, `"question <n>: an option is empty"`, `"question <n>: <k> times --recommended, want exactly one"`, `"question <n>: --recommended <x> points to no option"` en `"<field> is empty"`. Elke vormfout eindigt op `"; nothing posted"`.
  - De sessie komt uit `LIGHTHOUSE_CONTROLLER_RUN` (een UUID). Repo en issue komen uit de drager in `$LIGHTHOUSE_STATE_DIR/carriers/*.json` met die `session_id`: geen of meer dan één → fout. Worktree = de cwd.
  - De taal van het issue is `[project] language` uit `lighthouse.toml` in de cwd; zonder dat bestand is het `en`.
  - De body is `RenderQuestion`. Vóór de post leest `ParseQuestion(body)` hem terug, en die uitkomst moet gelijk zijn aan de velden; anders volgt een fout en geen post. `--dry-run` print de body.
  - Na de post staat de comment-URL op stdout. Een fout van `Issue` of `CreateComment` geeft exit 1 met `lighthouse ask: post failed: <fout>`.
  - Het commando opent nooit de database.

- [ ] **Step 1: Failing tests** in `ask_test.go`, met forgetest, een drager in `t.TempDir()` en de omgeving via een `Getenv`-fake:
  - `TestAskPostsBundle`: twee vragen → één comment, en `ParseQuestion` van de geposte body geeft beide vragen.
  - `TestAskRejectsShape` is een tabel: vijf vragen; één optie; zeven opties; `--option` vóór `--question`; twee `--recommended`; `--recommended f` bij drie opties; een lege `--where`. Elke rij geeft een fout met `nothing posted`, en de server ziet nul POST's (**AC 5a**).
  - `TestAskPostFailsExit1`: de server geeft 500 → `Execute` geeft 1 (**AC 5b**).
  - `TestAskNoCarrier`.
  - `TestAskDryRunPostsNothing`.
  - `TestAskOpensNoDatabase`: zet `XDG_STATE_HOME` op een lege `t.TempDir()`; na het commando bestaat `lighthouse/lighthouse.db` daar niet.
- [ ] **Step 2:** `go test ./internal/cli -run TestAsk ./internal/forge -run TestIssue` → FAIL.
- [ ] **Step 3:** Implementeer de code. `docs/ask.md` beschrijft de vlaggen, het voorbeeld uit `run.md` en de exitcodes, in het Nederlands. Zet in `run.md` een verwijzing naar dezelfde vlaggen.
- [ ] **Step 4:** `go test -race ./internal/cli ./internal/forge/...` → PASS.
- [ ] **Step 5:** Commit `feat(cli): lighthouse ask post een Vraag vanuit de werkplek`.

### Taak 11: Documentatie, changelog en eindverificatie

**Files:**
- Modify: `docs/workplace.md` (de paragraaf *Wie roept dit aan*: `Busy` en `Launch` worden nu aangeroepen door apply; de statemap en `runs/<sessie>/instructions.md`), `docs/tick.md` (link naar `ask.md`)
- Create: `changelog.d/issue-114-decide-apply-keten.md`

- [ ] **Step 1:** Werk de docs bij. Gebruik relatieve links.
- [ ] **Step 2:** Schrijf het changelog-fragment: `type: added`, één zin, naar het model van het fragment van #113.
- [ ] **Step 3:** Draai de verificatie: `go test -race -shuffle=on ./...`, `golangci-lint run`, `uv run pytest -q tests/pack` en `go run ./cmd/lighthouse init --upgrade` (→ `No changes.`). Alles moet groen zijn.
- [ ] **Step 4:** Commit `docs(dispatch): decide, apply, run en ask`.

**Niet in dit plan:**

- de rest van *Afloop*, met `CheckOpen` in de tick, #34 en #35 (#115);
- de usage-hervatting, de rem en `lighthouse pause` (issue 6), wat ook betekent dat `run` de pauzevlag en de usage-rem pas respecteert als issue 6 ze bouwt;
- de blokkaderegel en On hold (issue 7);
- opruimen in de tick (issue 7);
- de paia-kant van de drager en de mounts (stap 0 van de *Overstap*).

De PR noemt die punten met `Refs`, zodat niemand ze voor gedaan houdt.
