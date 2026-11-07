# Handoffs bouw→write-pr en merge→close-issue — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Een klare bouwsessie geeft het openen van de PR via een handoff aan een verse `/lighthouse-write-pr`-run, write-pr schrijft altijd `Refs #N` met een Evidence-tabel van de bewezen criteria, en na de merge toetst en sluit `/lighthouse-close-issue` (ingang B) het issue, ook met die tabel.

**Architecture:** In Go verandert alleen `Judge`: `/lighthouse-write-pr <nr>` komt op de allowlist, met het issuenummer als argument; de start loopt via de bestaande `handoff`-regel en de nacontrole via de bestaande `closeCheck`. De rest is tekst (run-instructies, de skills write-pr, close-issue en write-handoff, de PR-template, de kaart, de M2-spec) plus één regel in `pr_body_gate.py`: een Evidence-tabel onder `Refs #N` wordt op vorm gecontroleerd, niet geteld.

**Tech Stack:** Go (`internal/dispatch`), Python stdlib (`pack/core/lib`), pytest, markdown-skills in `pack/core/skills`, JSON (`docs/site/data/chain.json`).

**Spec:** [2026-11-01-handoffs-write-pr-close-issue-design.md](../specs/2026-11-01-handoffs-write-pr-close-issue-design.md)

## Besluiten van dit plan

De spec laat deze keuzes aan de bouwer; het plan legt ze vast.

- **Skilltests in Python.** De bestaande tests op skilltekst staan in `tests/pack/` (`test_write_pr_skill.py`, `test_close_issue_skill.py`, `test_skills_issue_flow.py`); de nieuwe gaan daarbij. De test op `run.md` blijft in `internal/dispatch/instructions_test.go`.
- **"Vorm en allowlist" in de gate is vorm.** De spec zegt dat `pr_body_gate.py` de rijen onder `Refs` valideert "zoals nu onder `Closes`". Onder `Closes` controleert de gate nu alleen de vorm (`evidence_rows`); de allowlist controleert `evidence.py`. Onder `Refs` doet de gate hetzelfde: vorm, geen telling, geen allowlist.
- **Een criterium is het nummer waarmee de AC-cel begint.** Write-pr begint elke AC-cel met het nummer van het criterium in het issue (`1. …`), zoals het voorbeeld in de skill al doet. Close-issue kiest per nummer de rij van de laatst gemergde PR. Een cel zonder nummervoorloop telt als eigen criterium.
- **Close-issue bouwt één body-bestand.** Uit de gemergde `Refs`-PR's met een `### Evidence #<nr>`-tabel, nieuwste eerst, neemt de skill per criterium de eerste rij, en draait dat ene bestand met `evidence.py run` op een verse `origin/<default branch>`, zoals C2. Oudere rijen voor hetzelfde criterium komen niet in het bestand en draaien dus niet. Geen nieuwe code in `evidence.py`.
- **De write-pr-run verandert geen code** (spec §1). De bouwsessie draait V2b vóór haar push. Zegt de gate in een run die met `/lighthouse-write-pr <nr>` startte `done`, `off` of `skip`, dan gaat hij door naar V4. Zegt hij `run` of `classify` (de bouw sloeg V2b over), dan reviewt hij niet en verwerkt hij niets: `roborev_gate.py record --skip "build session ran no review"`, en V4 zet die regel in de body. Alleen het changelog-fragment commit hij, zoals nu.
- **De plek van de regel.** `/lighthouse-place-instruction` heeft `disable-model-invocation: true` (`pack/core/skills/place-instruction/SKILL.md:4`): een agent kan hem niet starten. Taak 6 leest zijn lagentabel als naslag, zonder zijn npx-stappen, en zet de regel in de handoff-skill, sectie `## Where a handoff belongs` direct vóór `## Workflow`. De PR noemt de laag (skill) en waarom, voor AC 5.
- **Engelse tekst van de regel**, letterlijk vertaald uit de spec: "A handoff belongs where the next step needs a clean context: the previous step used up a lot of context, or the next step must judge independently. A handoff also belongs where the chain waits for an external event, such as red CI or a merge."

## Global Constraints

- Instructies (skills, `run.md`, hook-teksten) in het Engels; code, commentaar en testnamen in het Engels; `docs/developer/planning/*` in het Nederlands; `docs/getting-started.md` en `chain.json` in het Engels; het changelog-fragment in het Engels.
- Go volgt `/lighthouse-go-standard`; tests volgen `/lighthouse-test-standard`.
- Dogfood: een taak die iets onder `pack/` wijzigt, draait daarna `go run ./cmd/lighthouse init --upgrade` en commit de geïnstalleerde kopie in dezelfde commit (`git add -u .claude/skills .agents/skills .lighthouse .github`). Een tweede `init --upgrade` geeft `No changes.`
- Write-pr schrijft nooit `Closes`. `pr-create-gate` en ingang C van close-issue blijven `Closes` accepteren voor een PR die een mens zelf opent: een bestaande test die de gate op een `Closes`-body pint, blijft.
- `run.md` bevat het woord `roborev` niet (`TestRunInstructionsNameExistingSkills`).
- Geen wijziging in `decide.go`, `gather.go`, `evidence.py` of `internal/forge`.
- Testcommando's: `go test -race ./internal/dispatch` en `uv run pytest -q <bestand>`; de hele gate staat in `lighthouse.toml` `[test_gate]`.

## Review Focus

1. **Een handoff naar `/lighthouse-write-pr` met een ander nummer, zonder nummer, of in een repo zonder de skill** → afgewezen, of `reason_skill_missing`. → Taak 1 (`TestJudgeVerdicts`, `TestJudgeSkillMissing`).
2. **Een write-pr-run die geen PR opent** (rode tests, evidence-exit 3/4, gate-blok, PR bestaat al) mag geen `next: /lighthouse-close-issue` posten: anders lijkt de keten af terwijl er niets te mergen is. → Taak 2 (pin in `run.md`), Taak 4 (pin in write-pr V5).
3. **Een kapotte tabel onder `Refs #N`** blokkeert met een melding die `Refs #N` noemt; een `Refs`-body zonder tabel, of met minder rijen dan criteria, gaat door zonder het issue op te halen. → Taak 3.
4. **Een open of gemergde PR van een ander issue met een nummer dat met hetzelfde cijfer begint** (`Refs #14`, head `issue-14-…` bij issue 147, of andersom) telt niet mee in step 0 of B2a. → Taak 5 (de selectoren `startswith("issue-<nr>-")` en `Refs #<nr>([^0-9]|$)` gepind).
5. **Twee gemergde `Refs`-PR's met een rij voor hetzelfde criterium** → alleen de rij van de laatst gemergde draait; een oudere rij die niet meer houdt, maakt het criterium niet `not`. → Taak 5 (B2a-tekst), Taak 4 (AC-cel begint met het nummer).

---

### Task 1: Judge laat `/lighthouse-write-pr <issue>` starten

**Files:**
- Modify: `internal/dispatch/judge.go:36-43` (`allowlist`), `:110-122` (`argFits`)
- Test: `internal/dispatch/judge_test.go` (`allInstalled`, `TestJudgeVerdicts`, `TestJudgeSkillMissing`)
- Create: `internal/dispatch/testdata/decide/handoff-write-pr.json`

**Interfaces:**
- Produces: `Judge("/lighthouse-write-pr 7", 7, "", installed)` → `VerdictStart` met `Command == "/lighthouse-write-pr 7"`; een ander nummer of geen argument → `VerdictRejected`; `installed` zonder `lighthouse-write-pr` → `VerdictSkillMissing` met `Arg == "lighthouse-write-pr"`.

- [ ] **Step 1: Failing tests**

  - `allInstalled` krijgt `"lighthouse-write-pr": true`.
  - `TestJudgeVerdicts`, nieuwe rijen (issue 7):
    `{"write-pr own issue", "/lighthouse-write-pr 7", "", VerdictStart, "/lighthouse-write-pr 7", false, ""}`,
    `{"write-pr other issue", "/lighthouse-write-pr 8", "", VerdictRejected, "", false, "/lighthouse-write-pr 8"}`,
    `{"write-pr without number", "/lighthouse-write-pr", "", VerdictRejected, "", false, "/lighthouse-write-pr"}`.
  - `TestJudgeSkillMissing`: `Judge("/lighthouse-write-pr 7", 7, "", map[string]bool{})` → `Verdict == VerdictSkillMissing`, `Arg == "lighthouse-write-pr"`.
  - Fixture `handoff-write-pr.json`: kopie van `handoff-start.json` met drie wijzigingen. De handoff (comment 63) krijgt `branch: issue-7-bouw-de-keten` en `next: /lighthouse-write-pr 7`, ook in het blok (`Eerste stap: /lighthouse-write-pr 7`). `installed` krijgt `"lighthouse-write-pr": true`. `want` is één start: `trigger` `handoff`, `trigger_key` `handoff:63`, `command` `/lighthouse-write-pr 7`, `prompt` `/lighthouse-write-pr 7 (issue #7, controller-run)`, `branch` `issue-7-bouw-de-keten`, `comment_id` 63, `attempt` 1, `mode` `start`, `language` `nl`.

- [ ] **Step 2: Draai, verwacht rood**

  Run: `go test -race ./internal/dispatch -run 'TestJudge|TestDecideFixtures'`
  Expected: FAIL op `write-pr own issue`, `TestJudgeSkillMissing` en `TestDecideFixtures/handoff-write-pr` (verdict `reason_not_allowed` in plaats van een start).

- [ ] **Step 3: Implementeer**

  `allowlist` krijgt `"/lighthouse-write-pr": true`. In `argFits` deelt `/lighthouse-write-pr` de case van `/lighthouse-start-issue`: `args[0] == strconv.Itoa(issue)`. Geen nieuwe constante nodig; wel een regel in de doc-comment van `allowlist` als die commands opsomt.

- [ ] **Step 4: Draai, verwacht groen**

  Run: `go test -race ./internal/dispatch`
  Expected: PASS.

- [ ] **Step 5: Commit**

  ```bash
  git add internal/dispatch/judge.go internal/dispatch/judge_test.go internal/dispatch/testdata/decide/handoff-write-pr.json
  git commit -m "feat(dispatch): handoff naar /lighthouse-write-pr <issue> start een run (#147)"
  ```

### Task 2: Run-instructies — de bouw pusht en geeft over aan write-pr

**Files:**
- Modify: `internal/dispatch/instructions/run.md` (de drie bullets van "At `finishing-a-development-branch`, choose push + PR." tot en met de bullet "Never wait for CI …")
- Test: `internal/dispatch/instructions_test.go` (`TestRunInstructionsPinnedText`, `TestRunInstructionsNameExistingSkills`)

**Interfaces:**
- Consumes: Task 1 (de handoff `next: /lighthouse-write-pr <nr>` wordt een start).

- [ ] **Step 1: Failing tests**

  - `TestRunInstructionsPinnedText` krijgt deze strings (op enkele spaties vergeleken):
    - `"A run that builds opens no PR itself, whatever command started it."`
    - `"run step V2b of `/lighthouse-write-pr`"`
    - `"push the branch yourself with `git push -u origin HEAD`, then post the handoff `next: /lighthouse-write-pr <nr>` and stop."`
    - `"At `finishing-a-development-branch` this replaces push + PR."`
    - `"A run started with `/lighthouse-write-pr <nr>` verifies the acceptance criteria and opens the PR, the body through `--body-file`."`
    - `"When it opens no PR (red tests, `evidence.py` exit 3 or 4, a block of `pr-create-gate`, a PR that already exists), it posts no handoff"`
    - `"after the merge it starts `/lighthouse-close-issue`"`
    - `"End every phase with `/lighthouse-write-handoff`, except a phase that ends with `lighthouse ask` (see above) or a `/lighthouse-write-pr` run that opened no PR"`
  - De bestaande pin `"End every phase with `/lighthouse-write-handoff`, except a phase that ends with `lighthouse ask`"` blijft; hij is een prefix van de nieuwe.
  - `TestRunInstructionsNameExistingSkills`: de assert op `"Before push + PR: `/lighthouse-write-pr <nr>`"` wordt omgedraaid: `run.md` bevat `"Before push + PR"` niet en `"choose push + PR"` niet. De regex-lus op bestaande skills en de roborev-check blijven.

- [ ] **Step 2: Draai, verwacht rood**

  Run: `go test -race ./internal/dispatch -run TestRunInstructions`
  Expected: FAIL, `run.md lacks "A run that builds opens no PR itself, …"`.

- [ ] **Step 3: Vervang de drie bullets in `run.md`**

  Exacte tekst (de tests pinnen hem):

  ```markdown
  - A run that builds opens no PR itself, whatever command started it. When the build is done and
    `commit-test-gate` reported no red after the last commit: run step V2b of
    `/lighthouse-write-pr` (it ends at once when the repo turned it off), push the branch yourself
    with `git push -u origin HEAD`, then post the handoff `next: /lighthouse-write-pr <nr>` and
    stop. At `finishing-a-development-branch` this replaces push + PR. A red last commit you fix
    first, in a new commit.
  - A run started with `/lighthouse-write-pr <nr>` verifies the acceptance criteria and opens the
    PR, the body through `--body-file`. Then it posts the handoff
    `next: /lighthouse-close-issue <nr>` and stops. When it opens no PR (red tests, `evidence.py`
    exit 3 or 4, a block of `pr-create-gate`, a PR that already exists), it posts no handoff: it
    asks with `lighthouse ask` when the user has to choose, else it ends without one.
  - Never wait for CI and never poll `gh pr checks`, also not in `/lighthouse-fix-pr`. After a
    push to an open PR, post the handoff `next: /lighthouse-close-issue <nr>` and stop: the
    controller judges green or red and starts `/lighthouse-fix-pr` when needed, and after the
    merge it starts `/lighthouse-close-issue`. This goes above every rule that tells you to wait
    for a green PR, also above "stop at green" in `lighthouse-fix-pr`.
  ```

  De bullet "Never merge. Verifying (`/lighthouse-write-pr`) is allowed." blijft.

  In de bullet "End every phase with `/lighthouse-write-handoff`, …" wordt de eerste zin:
  "End every phase with `/lighthouse-write-handoff`, except a phase that ends with `lighthouse ask`
  (see above) or a `/lighthouse-write-pr` run that opened no PR: it posts the block as `🤖 Handoff`
  on the issue." De rest van die bullet blijft.

- [ ] **Step 4: Draai, verwacht groen**

  Run: `go test -race ./internal/dispatch`
  Expected: PASS.

- [ ] **Step 5: Commit**

  ```bash
  git add internal/dispatch/instructions/run.md internal/dispatch/instructions_test.go
  git commit -m "feat(dispatch): bouwrun pusht en geeft over aan /lighthouse-write-pr (#147)"
  ```

### Task 3: De gate controleert een Evidence-tabel onder `Refs` op vorm

**Files:**
- Modify: `pack/core/lib/pr_body_gate.py` (module-docstring r8-13, `check_body` r288-320)
- Test: `tests/pack/test_pr_body_gate.py`, `tests/pack/test_evidence.py`
- Geïnstalleerd: `.lighthouse/lib/lighthouse_lib/pr_body_gate.py` (via `init --upgrade`)

**Interfaces:**
- Produces: `def has_evidence_block(body: str, nr: int) -> bool` in `pr_body_gate.py`: of `strip_comments(body)` een regel heeft die op `^###[ \t]+Evidence[ \t]+#<nr>[ \t]*$` past (dezelfde regex als `evidence_rows`; haal hem naar één helper `_evidence_heading_re(nr)`).
- Produces: `check_body` geeft voor elke `nr` in `refs_issues(body)` die niet in `closes_targets(body)` staat en `has_evidence_block(body, nr)` is, een blocker `f"Refs #{nr}: {rows}"` als `evidence_rows` een string geeft. Geen `ac_count`-aanroep voor `Refs`.

- [ ] **Step 1: Failing tests**

  In `test_pr_body_gate.py`, met een helper `refs_body(nr, rows)` naast `closes_body` die `f"Refs #{nr} — close-issue toetst na de merge\n\n### Evidence #{nr}\n\n| AC | Command | Result |\n|---|---|---|\n{table}\n"` geeft:

  - `test_check_body_refs_with_valid_evidence_passes`: `check_body(refs_body(5, ROWS1), ac_count=_never)` → `blockers == []`, `notices == []`; `_never` faalt de test als hij wordt aangeroepen (`pytest.fail`).
  - `test_check_body_refs_evidence_is_not_counted`: `check_body(refs_body(5, ROWS1), lambda n: (3, ""))` → `blockers == []`.
  - `test_check_body_refs_with_broken_evidence_blocks`: `check_body(refs_body(5, [("1", "ls", "x")]))` → één blocker, die begint met `"Refs #5: "` en `"backticks"` bevat.
  - `test_check_body_refs_heading_without_table_blocks`: `"Refs #5 — close-issue toetst na de merge\n\n### Evidence #5\n\ngeen tabel"` → blocker met `"Refs #5: no table"`.
  - `test_has_evidence_block`: `True` voor `refs_body(5, ROWS1)` en nummer 5; `False` voor nummer 6, voor een body zonder heading, en voor een heading binnen `<!-- … -->`.

  In `test_evidence.py`: `test_run_accepts_a_refs_body` — schrijf een body `Refs #5 — …` met één `grep`-rij die in `tmp_path` houdt (zelfde opzet als de bestaande `Closes`-runtest rond r325) en verwacht exit 0 met `heading:evidence_holds` in de uitvoer. Deze test is meteen groen: hij pint dat `evidence.py` een `Refs`-body leest, waar write-pr en close-issue op leunen.

- [ ] **Step 2: Draai, verwacht rood**

  Run: `uv run pytest -q tests/pack/test_pr_body_gate.py tests/pack/test_evidence.py`
  Expected: FAIL op `has_evidence_block` (bestaat niet) en de twee `blocks`-tests; `test_run_accepts_a_refs_body` PASS.

- [ ] **Step 3: Implementeer** `has_evidence_block` en de `Refs`-lus in `check_body` zoals in *Interfaces*. De docstring zegt bij `Refs #N`: een `### Evidence #N`-tabel eronder is optioneel, wordt op vorm gecontroleerd en niet geteld.

- [ ] **Step 4: Draai, verwacht groen**

  Run: `uv run pytest -q tests/pack/test_pr_body_gate.py tests/pack/test_evidence.py tests/pack/test_pr_create_gate.py`
  Expected: PASS.

- [ ] **Step 5: Dogfood en commit**

  ```bash
  go run ./cmd/lighthouse init --upgrade && go run ./cmd/lighthouse init --upgrade   # tweede: No changes.
  git add pack/core/lib/pr_body_gate.py tests/pack/test_pr_body_gate.py tests/pack/test_evidence.py
  git add -u .claude/skills .agents/skills .lighthouse .github
  git commit -m "feat(pr-gate): Evidence-tabel onder Refs op vorm controleren (#147)"
  ```

### Task 4: Write-pr schrijft altijd `Refs`, met de bewezen rijen, en geeft over aan close-issue

**Files:**
- Modify: `pack/core/skills/write-pr/SKILL.md` (frontmatter `description`, intro r11-14, V1 r33-35, V2b r45-46, Verdicts r132-139, V3 r141-153, Evidence rules r188-189, `evidence.py run`-uitleg en exit-tabel r231-249, DoD r253, Shape r312-326, Counterpart r330-334; nieuwe sectie `## V5. After the PR` na V4)
- Modify: `pack/core/templates/pull_request_template.md` (het commentaar onder `## Related issues`)
- Test: `tests/pack/test_write_pr_skill.py`, `tests/pack/test_pr_template.py`
- Geïnstalleerd: `.claude/skills/lighthouse-write-pr/SKILL.md`, `.agents/skills/lighthouse-write-pr/SKILL.md`, `.github/pull_request_template.md`

**Interfaces:**
- Consumes: Task 3 (de gate laat een `Refs`-tabel met minder rijen dan criteria door).
- Produces (voor Task 5): elke Evidence-rij van write-pr staat onder `### Evidence #<nr>` in een body met `Refs #<nr> — <reason>`, en de AC-cel begint met het nummer van het criterium (`1. …`).

- [ ] **Step 1: Failing tests in `test_write_pr_skill.py`**

  - `test_names_both_new_verdicts` wordt `test_after_pr_is_gone`: `"`after merge`" in TEXT`; `"`after PR`"`, `"evidence_after_pr"` en `"closedByPullRequestsReferences"` niet in `TEXT`.
  - `test_gh_view_demands_a_number_and_after_pr_measures_through_the_issue` wordt `test_gh_view_demands_a_number`: alleen de eerste assert blijft.
  - `test_outcomes_are_headings`: de sleutel `evidence_after_pr_unmeasured` gaat uit de lus.
  - Nieuw `test_v3_always_writes_refs`: `v3 = _section(TEXT, "## V3.")`, platgeslagen op spaties, bevat `"always `Refs #N — <reason>`, never `Closes`"`, `"/lighthouse-close-issue"`, `"one row per AC that is `demonstrably done`"`, `"starts with the number of the criterion"` en `"no table"`; `"Mixing is allowed"` niet in `TEXT`.
  - Nieuw `test_description_names_refs_not_closes`: de `description` bevat `"`Refs #N — <reason>`"` en niet `"`Closes #N`"`.
  - `test_example_row_holds_against_the_template` blijft; nieuw `test_example_block_is_a_valid_refs_body`: het voorbeeldblok bevat `"Refs #1091 — "` en niet `"Closes #1091"`, en `gate.check_body(block, ac_count=<faalt bij aanroep>).blockers == []`.
  - Nieuw `test_v2b_runs_in_the_build_session`: `_section(TEXT, "## V2b.")` platgeslagen bevat `"build session"`, `"`run` or `classify`"` en `'record --skip "build session ran no review"'`.
  - Nieuw `test_v5_hands_off_to_close_issue`: `v5 = _section(TEXT, "## V5.")` bevat `"/lighthouse-write-handoff"`, `"`/lighthouse-close-issue <nr>`"`, `"after the merge"`, `"(issue #<nr>, controller-run)"`, `"LIGHTHOUSE_CONTROLLER_RUN"` en `"no handoff"`; `TEXT.index("## V4.") < TEXT.index("## V5.")`.

  In `test_pr_template.py`:
  - `test_template_names_lighthouse_write_pr`: de verwachte regelnummers worden die van de nieuwe tekst.
  - `test_template_is_hostless`: `"a test that cannot run in CI" in _text()` in plaats van op regel 15.
  - Nieuw `test_template_puts_refs_first`: in het `## Related issues`-commentaar staat `` `Refs #123 — <reason>` `` vóór `` `Closes #123` ``, en de platgeslagen tekst bevat `"only when a person opens the PR without /lighthouse-write-pr"`.
  - `test_template_keeps_its_tokens` blijft ongewijzigd.

- [ ] **Step 2: Draai, verwacht rood**

  Run: `uv run pytest -q tests/pack/test_write_pr_skill.py tests/pack/test_pr_template.py`
  Expected: FAIL op de nieuwe en omgedraaide tests.

- [ ] **Step 3: Herschrijf de skill**

  Per sectie, binnen de spec (§2 *Write-pr*):
  - **description**: "… writes the PR body itself — `Refs #N — <reason>` with an Evidence table of the proven criteria, or `No issue: <reason>` — …"; blijft tussen enkele quotes en eindigt op `is lighthouse-fix-pr.'`.
  - **Intro**: de PR noemt zijn issue altijd met `Refs #N`; `/lighthouse-close-issue` toetst na de merge en sluit. De skill sluit nooit zelf.
  - **V1**: niet meer "the hook counts them and demands as many Evidence rows"; elk criterium krijgt een verdict.
  - **V2b**: in een controllerrun draait V2b in de **build session**, vóór haar push en haar handoff; de run die met `/lighthouse-write-pr <nr>` start, vindt dan `done` bij stap 0 en verandert geen code. Zegt de gate daar `run` or `classify`, dan sloeg de bouw V2b over: geen review, geen verwerking, maar `python3 .lighthouse/scripts/roborev_gate.py record --skip "build session ran no review"` en door naar V4.
  - **Verdicts**: de rij `after PR` gaat weg. `after merge` dekt ook een criterium dat alleen na de PR te meten is.
  - **V3**: always `Refs #N — <reason>`, never `Closes`; de reden zegt dat `/lighthouse-close-issue` de rijen na de merge draait en het issue sluit. Daaronder `### Evidence #N` met one row per AC that is `demonstrably done`; de AC-cel starts with the number of the criterion in the issue (`1. …`). Daaronder per open criterium (`partly`, `not`, `after merge`) het verdict en wat nog nodig is. Alle criteria open: no table. Eén zin: `Closes` blijft geldig voor een PR die een mens zelf opent; write-pr schrijft het niet, want de merge zou het issue sluiten vóór de nacontrole.
  - **Evidence rules**: "Entry C runs every row again" wordt "`/lighthouse-close-issue` runs every row again after the merge".
  - **`evidence.py run`**: alleen als de body een tabel heeft. Exit 0 = alle rijen `heading:evidence_holds`; `heading:evidence_after_pr_unmeasured` verdwijnt uit tekst en exit-tabel. Een rij die niet houdt, gaat uit de tabel en het criterium wordt open.
  - **DoD**: "does not decide between `Closes`/`Refs`" wordt "does not decide which criteria get a row".
  - **V5. After the PR** (nieuw, na V4): alleen als `gh pr create` een PR opende. Interactief: `/lighthouse-write-handoff`, first step `/lighthouse-close-issue <nr>`, uit te voeren na de merge (rij "work done, issue can be closed"). Controllerrun (prompt eindigt op `(issue #<nr>, controller-run)` of `LIGHTHOUSE_CONTROLLER_RUN` staat in de omgeving): `/lighthouse-write-handoff` post `next: /lighthouse-close-issue <nr>`; stop. Geen PR geopend: no handoff naar close-issue; in een controllerrun `lighthouse ask` als de mens moet kiezen, anders einde.
  - **Shape**: `Refs #1091 — /lighthouse-close-issue runs these rows after the merge and closes the issue`; rij 6 (`after PR`) gaat weg; onder de tabel één regel voor een open criterium, bv. `- 6. … — `after merge`: <wat nog nodig is>`.
  - **Counterpart**: onder `Closes #N` telt de hook de rijen tegen de criteria; onder `Refs #N` controleert hij een tabel op vorm en telt niet.

- [ ] **Step 4: Herschrijf het template-commentaar**

  ```markdown
  <!-- Written by /lighthouse-write-pr. One of three forms:
       `Refs #123 — <reason>`, with a `### Evidence #123` table (| AC | Command | Result |) of one
         row per criterion proven on this branch, and below it each open criterion with what is
         still needed (a deploy, a measurement over days, a test that cannot run in CI);
         /lighthouse-close-issue runs the rows after the merge and closes the issue;
       `Closes #123` + a `### Evidence #123` table with one row per acceptance criterion, only
         when a person opens the PR without /lighthouse-write-pr: the merge closes the issue;
       `No issue: <reason>`.
       Give the PR the milestone of that issue (`gh pr create --milestone "<title>"`).
       No issue, no milestone. -->
  ```

- [ ] **Step 5: Draai, verwacht groen**

  Run: `uv run pytest -q tests/pack/test_write_pr_skill.py tests/pack/test_pr_template.py tests/pack/test_skill_contracts.py`
  Expected: PASS.

- [ ] **Step 6: Dogfood en commit**

  ```bash
  go run ./cmd/lighthouse init --upgrade && go run ./cmd/lighthouse init --upgrade   # tweede: No changes.
  git add pack/core/skills/write-pr/SKILL.md pack/core/templates/pull_request_template.md tests/pack/test_write_pr_skill.py tests/pack/test_pr_template.py
  git add -u .claude/skills .agents/skills .lighthouse .github
  git commit -m "feat(write-pr): altijd Refs met de bewezen rijen, handoff naar close-issue (#147)"
  ```

### Task 5: Close-issue wacht op een open `Refs`-PR en draait de tabel van gemergde `Refs`-PR's

**Files:**
- Modify: `pack/core/skills/close-issue/claude/SKILL.md` en `pack/core/skills/close-issue/agents/SKILL.md`, identiek (frontmatter `description`, *When to Use* r24-27, *Step 0* r29-46, *Three entries* r50-54, nieuwe `### B2a.` tussen B2 en B3, B3-verdicts, B7 r175-179)
- Test: `tests/pack/test_close_issue_skill.py`
- Geïnstalleerd: `.claude/skills/lighthouse-close-issue/SKILL.md`, `.agents/skills/lighthouse-close-issue/SKILL.md`

**Interfaces:**
- Consumes: Task 4 (AC-cel begint met het criteriumnummer; tabel onder `Refs #<nr>`).

- [ ] **Step 1: Failing tests**

  - `test_step_zero_stops_on_an_open_refs_pr`: `s0 = _section(body, "## Step 0")` bevat `"gh pr list"`, `"--state open"`, `'startswith("issue-<nr>-")'`, `'Refs #<nr>([^0-9]|$)'` en `"dangerouslyDisableSandbox: true"`; de bestaande `waits for merge of #<pr>`-melding staat in `s0`.
  - `test_entry_b_runs_the_evidence_of_merged_refs_prs`: `b2a = _section(body, "### B2a.")` (knip tot de volgende `\n### `) bevat `"R=$(python3 .lighthouse/lib/lighthouse_lib/config.py forge.repo)"` vóór de eerste `--repo "$R"`, precies één `mktemp -d`, `"--state merged"`, `"mergedAt"`, `'startswith("issue-<nr>-")'`, `'Refs #<nr>([^0-9]|$)'`, `"### Evidence #<nr>"`, `"python3 .lighthouse/lib/lighthouse_lib/evidence.py run"`, `"git worktree add --detach"`, `"config.py forge.default_branch"`, `"only the row of the latest merged PR"`, `"the number the AC cell starts with"`, `"`demonstrably done`"`, `"`not`"` en `"not measurable from here"`.
  - `test_b2a_sits_between_b2_and_b3`: `body.index("### B2.") < body.index("### B2a.") < body.index("### B3.")`.
  - `test_b2a_never_pastes_body_text_into_a_command`: `b2a` bevat `"--body-file"` en `"never paste"`.
  - `test_b7_includes_b2a`: de B7-sectie bevat `"B2a"`.
  - De bestaande tests (entry C, step 0 `waits for merge`) blijven.

- [ ] **Step 2: Draai, verwacht rood**

  Run: `uv run pytest -q tests/pack/test_close_issue_skill.py`
  Expected: FAIL op de vijf nieuwe tests.

- [ ] **Step 3: Step 0**

  Na de bestaande GraphQL-query een tweede commando, ook buiten de sandbox:

  ```bash
  R=$(python3 .lighthouse/lib/lighthouse_lib/config.py forge.repo) || exit 2
  gh pr list --repo "$R" --state open --limit 100 --json number,headRefName,body \
    --jq '.[] | select((.headRefName | startswith("issue-<nr>-")) or (.body | test("Refs #<nr>([^0-9]|$)"))) | .number'
  ```

  De eerste tabelrij wordt: issue `OPEN` en een `Closes`-node met `state` `OPEN`, **of** een nummer uit deze lijst → `waits for merge of #<pr>; …` en stop. Eén zin waarom: een `Refs`-PR staat niet in `closedByPullRequestsReferences`, en een geplakte handoff zou het issue anders vóór de merge kunnen sluiten.

- [ ] **Step 4: B2a**

  Nieuwe subsectie `### B2a. The Evidence tables of merged Refs PRs`, met:
  - De lijst, nieuwste eerst, in een zelfstandig blok (variabelen leven niet door naar een volgende tool-aanroep):

    ```bash
    R=$(python3 .lighthouse/lib/lighthouse_lib/config.py forge.repo) || exit 2
    gh pr list --repo "$R" --state merged --limit 100 --json number,headRefName,body,mergedAt \
      --jq '[.[] | select((.headRefName | startswith("issue-<nr>-")) or (.body | test("Refs #<nr>([^0-9]|$)"))) | select(.body | test("(?m)^### Evidence #<nr>[ \t\r]*$"))] | sort_by(.mergedAt) | reverse | .[] | "\(.number)\t\(.mergedAt)"'
    ```

  - Geen PR: B2a valt weg, alle criteria gaan door B2–B4.
  - Anders: maak één keer `scratch=$(mktemp -d) && echo "scratch=$scratch"` en gebruik dat geprinte pad letterlijk in elke volgende aanroep (Bash én Write tool), zonder tweede `mktemp -d`; noem dat in de skilltekst `<scratch>`, zoals C2. Haal per PR de body op met `gh pr view <pr> --json body --jq .body > <scratch>/body-<pr>.md`. Schrijf met de Write tool één bestand `<scratch>/evidence.md`: `### Evidence #<nr>`, de kop `| AC | Command | Result |`, `|---|---|---|`, en per criterium only the row of the latest merged PR; het criterium is the number the AC cell starts with (een cel zonder nummer is een eigen criterium). Rijen ongewijzigd overnemen; never paste body text into a command line, alleen via `--body-file`.
  - Draai dat bestand zoals C2, in één shell-aanroep (`B=$(… config.py forge.default_branch)`, verse `git fetch`, `git worktree add --detach <scratch>/after-merge-<nr> origin/$B`, `evidence.py run --issue <nr> --body-file <scratch>/evidence.md --cwd <scratch>/after-merge-<nr>`, `rc` vóór de `worktree remove`), zonder `--before-pr`.
  - Uitkomst per rij naar B3: `heading:evidence_holds` → `demonstrably done`, met als bewijsregel het commando en `heading:evidence_holds`; `heading:evidence_fails` of `heading:evidence_invalid` → `not`; `heading:evidence_unmeasurable` → not measurable from here. Geen `rc=`-regel of exit 3: geen verdict uit B2a; die criteria gaan door B2–B4.
  - Criteria zonder rij: B2–B4 zoals nu.

  Verder: *When to Use* krijgt "after the merge of a `Refs` PR from `/lighthouse-write-pr`: entry B, which runs that PR's Evidence table first (B2a)"; *Three entries* noemt bij B de tabellen van gemergde `Refs`-PR's; de `description` noemt naast ingang C de nacontrole na een `Refs`-PR (die waarde staat zonder quotes: schrijf er geen ` #` in, YAML leest de rest dan als commentaar); B7 zegt "Step 0, B1–B4 and B2a hold unchanged".

- [ ] **Step 5: Draai, verwacht groen**

  Run: `uv run pytest -q tests/pack/test_close_issue_skill.py tests/pack/test_skill_contracts.py`
  Expected: PASS. Controleer ook `diff pack/core/skills/close-issue/claude/SKILL.md pack/core/skills/close-issue/agents/SKILL.md`: alleen de bestaande regel 338 (`CLAUDE.md`/`AGENTS.md`) verschilt.

- [ ] **Step 6: Dogfood en commit**

  ```bash
  go run ./cmd/lighthouse init --upgrade && go run ./cmd/lighthouse init --upgrade   # tweede: No changes.
  git add pack/core/skills/close-issue tests/pack/test_close_issue_skill.py
  git add -u .claude/skills .agents/skills .lighthouse .github
  git commit -m "feat(close-issue): wachten op open Refs-PR, Evidence van gemergde Refs-PR's in ingang B (#147)"
  ```

### Task 6: De regel en de routes in de handoff-skill

**Files:**
- Modify: `pack/core/skills/write-handoff/SKILL.md` (nieuwe sectie op de plek die `/lighthouse-place-instruction` kiest; Workflow stap 4-tabel r79-91; Controller-modus stap 3-tabel r214-225)
- Test: `tests/pack/test_skills_issue_flow.py`
- Geïnstalleerd: `.claude/skills/lighthouse-write-handoff/SKILL.md`, `.agents/skills/lighthouse-write-handoff/SKILL.md`

- [ ] **Step 1: Failing tests**

  - `test_handoff_carries_the_rule`: de platgeslagen tekst bevat de twee zinnen uit *Besluiten van dit plan* letterlijk, vóór `"4. **Decide the first step"`.
  - `test_handoff_routes_a_finished_build_to_write_pr`: het deel van `"4. **Decide the first step"` tot `"5. **Name the pitfalls"` heeft een tabelrij die begint met `"| build done, last commit green, no PR yet | `/lighthouse-write-pr <nr>` |"` en eindigt op `"| `offer` |"`; het deel van `"3. **`next:` is exactly one command"` tot `"4. **`when:`**"` heeft een rij `"| build done, last commit green | `/lighthouse-write-pr <nr>` |"`.
  - `test_handoff_pr_open_row_names_the_close_check`: de `PR open`-rij van stap 3 bevat `"the controller does not start it from the handoff"` en `"after the merge of the `Refs` PR"`; `"it waits for green"` staat niet in de skill.

- [ ] **Step 2: Draai, verwacht rood**

  Run: `uv run pytest -q tests/pack/test_skills_issue_flow.py -k handoff`
  Expected: FAIL op de drie nieuwe tests.

- [ ] **Step 3: Toets de laag**

  Start `/lighthouse-place-instruction` niet (alleen de gebruiker kan dat: `disable-model-invocation: true`) en volg zijn npx-stappen niet. Lees zijn lagentabel in `pack/core/skills/place-instruction/SKILL.md` als naslag en noteer in één zin waarom de regel op de laag *skill* hoort (hij geldt alleen bij het schrijven van een handoff). Die zin gaat later in de PR, bij AC 5. De plek staat vast: sectie `## Where a handoff belongs` direct vóór `## Workflow`.

- [ ] **Step 4: Schrijf de regel en de rijen**

  - De sectie: de twee zinnen, plus één zin dat Workflow stap 4 de regel toepast.
  - Stap 4, nieuwe rij vóór "work done, issue can be closed": `| build done, last commit green, no PR yet | `/lighthouse-write-pr <nr>` | `offer` |`.
  - Stap 3, nieuwe rij vóór `PR open`: `| build done, last commit green | `/lighthouse-write-pr <nr>` |`.
  - Stap 3, rij `PR open`: `` `/lighthouse-close-issue <nr>` — final handoff: the controller does not start it from the handoff, but after the merge of the `Refs` PR, as the close check ``.

- [ ] **Step 5: Draai, verwacht groen**

  Run: `uv run pytest -q tests/pack/test_skills_issue_flow.py tests/test_site_chain.py`
  Expected: PASS (`test_works_handoffs_follow_the_handoff_table` ziet nog de oude set; Task 7 breidt hem uit).

- [ ] **Step 6: Dogfood en commit**

  ```bash
  go run ./cmd/lighthouse init --upgrade && go run ./cmd/lighthouse init --upgrade   # tweede: No changes.
  git add pack/core/skills/write-handoff/SKILL.md tests/pack/test_skills_issue_flow.py
  git add -u .claude/skills .agents/skills .lighthouse .github
  git commit -m "docs(write-handoff): regel voor een handoff, route bouw→write-pr (#147)"
  ```

### Task 7: Kaart, getting-started, M2-spec en changelog

**Files:**
- Modify: `docs/site/data/chain.json` (handoffs r696-713; chips `write-pr` r517-521, `close-issue` r631-638)
- Modify: `tests/test_site_chain.py:272-277`
- Modify: `docs/getting-started.md:185-190`
- Modify: `docs/developer/planning/specs/2026-10-27-m2-dispatcher-design.md:240-243`
- Create: `changelog.d/issue-147-handoffs-verify-close.md`

- [ ] **Step 1: Failing test**

  `test_works_handoffs_follow_the_handoff_table`: de verwachte set krijgt `("commit-test-gate", "write-pr")` en `("pr-and-ci", "close-issue")`. Voeg toe: geen `works`-handoff heeft een sleutel `issue`.

- [ ] **Step 2: Draai, verwacht rood**

  Run: `uv run pytest -q tests/test_site_chain.py`
  Expected: FAIL, de twee paren staan nog op `planned`.

- [ ] **Step 3: `chain.json`**

  - De twee handoffs: `"status": "works"`, de sleutel `issue` weg. `pr-and-ci`→`fix-pr` (#119) blijft `planned`.
  - Chip `write-pr`: `what` "… writes the PR body: Refs #N with an Evidence table of the proven criteria, or No issue."; `how[2]` "Every criterion proven or not, the body says Refs #N: close-issue runs the rows after the merge and closes the issue."
  - Chip `close-issue`: `what` noemt de nacontrole na een `Refs`-PR (rijen opnieuw draaien, sluiten met bewijs); `how[0]` noemt ook het wachten op een open `Refs`-PR; `how[2]` noemt dat ingang B de tabel van gemergde `Refs`-PR's draait en ingang C die van een `Closes`-PR die een mens opende; `when` "After the merge of a PR for the issue, or when you ask to close an issue."

- [ ] **Step 4: `docs/getting-started.md` stap 4 en 5**

  Stap 4: write-pr schrijft `Refs #N` met een Evidence-tabel van de bewezen criteria; de gate-zin blijft. Stap 5: "Type `/lighthouse-close-issue <nr>`. The issue is still open: the skill runs the Evidence table of the merged PR on the default branch and closes the issue with that evidence, or leaves it open with what is missing." Draai `uv run pytest -q tests/test_getting_started.py`.

- [ ] **Step 5: M2-spec regel 5**

  Vervang de zin over `Closes` door:
  "Een controllerrun opent altijd een `Refs`-PR (`/lighthouse-write-pr`, #147), dus deze regel dekt elke PR van de controller. Een `Closes`-PR sluit het issue bij de merge al; daar doet de tick niets, want alleen een mens opent nog een `Closes`-PR (ingang C van close-issue is aan de mens) *(eigen keuze na spec-review, r1-f10; herzien in #147: eerst sloot ook een controllerrun met `Closes`, en bleef de nacontrole daarvan aan de mens)*."

- [ ] **Step 6: Changelog-fragment**

  ```markdown
  ---
  type: changed
  ---
  `/lighthouse-write-pr` now always links the issue with `Refs #N`, with an Evidence table of the proven criteria, and `/lighthouse-close-issue` runs that table after the merge and closes the issue. A controller run that finishes a build pushes and hands the PR to a fresh `/lighthouse-write-pr` session, and the controller starts the close check after every merged PR it opened.
  ```

- [ ] **Step 7: Draai, verwacht groen**

  Run: `uv run pytest -q tests/test_site_chain.py tests/test_getting_started.py tests/test_changelog.py`
  Expected: PASS.

- [ ] **Step 8: Commit**

  ```bash
  git add docs/site/data/chain.json tests/test_site_chain.py docs/getting-started.md docs/developer/planning/specs/2026-10-27-m2-dispatcher-design.md changelog.d/issue-147-handoffs-verify-close.md
  git commit -m "docs(chain): handoffs bouw→write-pr en merge→close-issue werken (#147)"
  ```

## Na de taken, vóór `/lighthouse-write-pr`

1. **AC 2 en 3 van #147 controleren.** Het issue heeft ze al in de vorm van de spec (*Bewijs*; gecontroleerd 2026-11-06): AC 2 "Write-pr schrijft altijd `Refs #N` …", AC 3 "Een gemergde `Closes`-PR start niets (fixture)". Lees ze met `gh issue view 147 --json body`. Staan ze er zo, dan gebeurt er niets; noemt AC 2 toch weer `Closes` als uitkomst van write-pr, dan is dat een vraag aan de eigenaar (interactief een vraag, in een controllerrun `lighthouse ask`), geen eigen edit.
2. **Label `chain` van #147 halen** bij het openen van de PR (`gh issue edit 147 --remove-label chain`): `chain.json` noemt #147 niet meer, en `ci/chain_roadmap.py` faalt anders op de PR zolang #147 open is.
3. **Dogfood-check** op de hele branch: `go run ./cmd/lighthouse init --upgrade` geeft `No changes.` (AC 7).
