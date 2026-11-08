# Skill-scripts deel 2 in Go: implementatieplan

> **Voor agents:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development
> (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen
> gebruiken checkboxes (`- [ ]`).

**Doel:** stap 9 van de milestone *Lighthouse zonder Python* (#282). De review-runner, het
meetrecord, de gepaarde ronde 1, de roborev-gate, het roborev-comment, het Codex-budget en
`check-instructions` draaien als subcommando's van de binary. Skills roepen ze aan zonder `python3`.

**Architectuur:** drie Go-pakketten, elk met een dunne cobra-wrapper in `internal/cli`:
`internal/review` (runner, record, paar), `internal/roborev` (gate, comment) en
`internal/checkinstr`. Het Codex-budget komt in het bestaande `internal/usage`, dat de
app-server en het rollout-log al leest. Gelijk gedrag bewijzen we zo. Pytest-tests die alleen het
CLI-contract als subprocess draaien, blijven en wijzen voortaan naar de binary. Tests die interne
functies importeren, gaan als Go-tests mee, geval voor geval.

**Tech stack:** Go (cobra, `internal/config`, `internal/datadir`, `internal/orderedjson`,
`internal/wordcount`), pytest met de fixture `lighthouse_bin` (`tests/conftest.py:62`).

**Spec:** [`2026-11-07-zonder-python-design.md`](../specs/2026-11-07-zonder-python-design.md),
§4, §9, §10, *Anti-doelen* en *Opdeling* stap 9.

## Opdeling in drie PR's

Het werk is ruim 3.300 regels Python en bash, plus ongeveer 5.700 regels tests. Daarom komt het in
drie PR's, elk zelfstandig groen en te mergen. *(Eigen keuze; de spec laat de opdeling aan de
bouwer.)*

| PR | Taken | Body | Haalt weg |
|---|---|---|---|
| A: review-keten | 1–5 | `Refs #282 — deel A van 3` | `review.sh`/`verify.sh` (×2), `plan_review_record.py`, `review_pair.py` |
| B: roborev | 6–8 | `Refs #282 — deel B van 3` | `roborev_gate.py`, `roborev_comment.py`, `codex_usage.py` |
| C: check-instructions | 9–10 | `Closes #282` | `check-instructions.sh` |

C hangt niet van A of B af en mag parallel lopen. B bouwt voort op taak 1 (het budget in Go). PR A
draait op deze branch (`issue-282-skill-scripts-deel-2`), samen met dit plan. B en C krijgen elk
een eigen worktree vanaf `main`. Alleen de laatste PR die merget, zet `Closes #282`. Merget
C eerder, dan krijgt C `Refs` en de laatste van A en B `Closes`.

## Globale eisen

- **Zelfde contract.** Elk subcommando houdt de argumenten, de exitcodes, de stdout en de
  stderr-regels van zijn script. Naam: het script met streepjes (spec §4):
  `lighthouse codex-usage`, `lighthouse plan-review-record`, `lighthouse review-pair`,
  `lighthouse roborev-gate`, `lighthouse roborev-comment` en `lighthouse check-instructions`. De
  runner heet `lighthouse review-run`.
- **Gebruiksfout is exit 2**, zoals bij argparse. Cobra geeft standaard exit 1. Daarom zet elk
  nieuw commando `SetFlagErrorFunc` en een `Args`-validator die `&exitError{code: 2, err: …}`
  teruggeven. Dat gaat via één helper `usageErr(err error) error` in `internal/cli/scripts.go`.
- **JSON.** Wat een skill of een ander commando leest, moet na parsen gelijk zijn: zelfde sleutels,
  zelfde volgorde (`internal/orderedjson`), `null` waar Python `None` schrijft. Bytegelijkheid is
  alleen verplicht waar een hash, een marker of een opgeslagen bestand ervan afhangt (zie
  *Review-focus* 2). `encoding/json` krijgt `SetEscapeHTML(false)`, behalve waar Python juist
  escapet.
- **Repo-root**: `LIGHTHOUSE_REPO_ROOT`, anders `git rev-parse --show-toplevel`
  (`config.RepoRoot()`). Nooit de plek van de binary.
- **Config**: `internal/config` in-process (`config.Load`, `config.Lookup`). Er is geen
  `config.py`-aanroep meer.
- **Bevroren** in deze stap, geen byte anders: `pack/core/lib/{config,headings,datadir}.py`
  (weg in stap 12). In PR A ook `pack/core/scripts/codex_usage.py`, want `roborev_gate.py` is daar
  nog zijn afnemer. B haalt het weg.
- **Stap 6, 8 en 10 blijven buiten deze stap**: `pr_body_gate.py` en `evidence.py` in `write-pr`,
  `measure_fix_pr_kpi.py` in `fix-pr` en `measure_skill_surface.py` in `place-instruction`.
- Go volgt `/lighthouse-go-standard`, tests volgen `/lighthouse-test-standard`. Code, comments en
  meldingen zijn Engels. Meldingen die nu Nederlands zijn, blijven letterlijk zo: `uitkomst`,
  `reden`, `Codex-budget: onleesbaar - exit 1`, `… ingekort tot de limiet van GitHub`. Commits, PR's
  en dit plan zijn Nederlands.
- Elke PR draait `go run ./cmd/lighthouse init --upgrade`, commit de geïnstalleerde kopie en krijgt
  een fragment `changelog.d/<branch>.md` met `type: changed` (minor, spec §10). Een `changed`-fragment
  start geen release bij een push (`ci/changelog.py:147`): de release volgt de wekelijkse cron of een
  handmatige `workflow_dispatch`. Tot dan draait wie nieuwe subcommando's nodig heeft `go install`.
- **Na elke merge** draait wie in deze repo werkt `go install ./cmd/lighthouse` op `main`. Skills
  roepen `lighthouse` van het PATH aan, en de vorige release kent de nieuwe subcommando's niet
  (spec §10).

## Review-focus

1. **Lengte in tekens, niet in bytes.** `roborev_comment.py` knipt op `LIMIT = 65536` met Python
   `len()`, dus op code points. Go's `len` telt bytes. → `TestRenderCutsOnRunesNotBytes`
   (taak 7): raw van 70.000 × `é` geeft een comment van precies 65.536 runes, en de marker, het log
   en de tabel staan er heel in.
2. **Bytegelijke JSON waar het telt.** Het `roborev-log`-blok escapet `<`, `>` en `-` als
   `<`, `>` en `-`. `roborev_comment.py:35-39` roept `json.dumps` aan met
   `separators=(",", ":")` en `ensure_ascii=False`: geen spaties na `,` en `:`, en niet-ASCII
   (`é`) blijft rauwe UTF-8. → `TestLogLineEscapesHostileReason` (taak 7) met de
   reden `a --> b <x> é\n`: één regel, gelijk aan de fixture die pytest nu met de Python-versie
   vastlegt, en terug te parsen met `encoding/json`.
3. **De prompt blijft byte voor byte gelijk.** Zonder `--context` en zonder sidecar mag de prompt
   van `review-run` niet afwijken van wat `review.sh` bouwde. Dat geldt ook bij een pad met spaties
   en bij `{{` in de documenttekst. Die tekst wordt niet gesubstitueerd, alleen de template. →
   `test_spec_review_prompt_is_byte_identical_without_new_flags` blijft (taak 2). Nieuw:
   `test_doc_path_with_space_reaches_codex_verbatim`.
4. **Een kapotte budgetcheck stopt de ronde niet.** Bij een seam die niet start, geen JSON geeft
   of exit 1 geeft, schrijft `review-run` het synthetische `not_measurable`-object en gaat Codex
   gewoon door. Hetzelfde geldt voor een panic in de in-process check. →
   `TestBudgetPanicIsNotMeasurable` (taak 2) naast de bestaande pytest-gevallen.
5. **`check-instructions` blijft hetzelfde bij mirror-uitzonderingen.** Deze repo heeft geen
   `[paths] mirror_exceptions` (`lighthouse.toml`), dus de gelijkheid bewijs je met fixtures die het
   huidige bash-script vastlegt (`--print-mirror-exceptions`), niet met een bestand van deze repo.
   Anders wordt check 11 na de port rood in elke repo met uitzonderingen. → `TestMirrorFixtures`
   (taak 9 stap 1): de Go-uitvoer is byte voor byte gelijk aan de bash-fixtures voor een gewijzigde
   regel, een ontbrekende slotnewline, een binair bestand, een bestand aan één kant en een
   gemarkeerde skill.
6. **Gedeelde fixtures vóór de port (spec §9, r1-f15).** Modules met inline pytest-gevallen
   (`plan_review_record`, `review_pair`, `roborev_gate`) krijgen eerst fixtures in
   `tests/pack/fixtures/<module>/`, groen met pytest tegen Python; de Go-tests lezen dezelfde
   fixtures. Het Python-orakel gaat pas weg ná die stap.
7. **De productie-budgetroute blijft getest zonder seam.** Zie taak 2 stap 1.

---

## PR A: review-keten

### Taak 1: Codex-budget in Go, `lighthouse codex-usage read|budget`

**Bestanden:**
- Nieuw: `internal/usage/codex_budget.go` (`BudgetOutcome`, `DecideBudget`, `ReadBudget`)
- Wijzig: `internal/usage/codex.go` (`ResetCredits` in de reading, timeout uit config)
- Nieuw: `internal/cli/codexusage.go`, `internal/cli/scripts.go` (`usageErr`)
- Wijzig: `internal/cli/root.go` (registratie)
- Test: `internal/usage/codex_budget_test.go`, `internal/cli/codexusage_test.go`
- Wijzig: `docs/usage-venster.md:110-119` (uitvoer van `usage codex`, zie stap 3)

**Interfaces:**
- Produceert:
  ```go
  type BudgetOutcome struct { // JSON-volgorde = veldvolgorde, nil = null
      Decision string; Reason string; Window, ResetAt *string; SessionPct, WeekPct *int
      Source *string; LowerBound *bool; Thresholds *BudgetThresholds }
  type BudgetThresholds struct{ Session, Week int } // json "session","week"
  func DecideBudget(r *CodexState, review5h, reviewWeek int, now time.Time) BudgetOutcome
  // ReadBudget: config lezen, reading, besluit. code 6 bij fallback, anders 0.
  func ReadBudget(ctx context.Context, read func(context.Context, time.Duration) (CodexState, error)) (BudgetOutcome, int, string /*stderr*/)
  ```
- `lighthouse codex-usage read`: de reading als JSON met inspringing 2, exit 0. Niet meetbaar:
  `not measurable: <reden>` op stderr en exit 1. `lighthouse codex-usage budget`: één regel JSON,
  exit 6 bij `fallback`, anders 0. Bij een configfout staat `codex-usage: config: <fout>` op
  stderr.

- [ ] **Stap 1: Go-tests uit `tests/pack/test_codex_usage.py`.** Eén Go-test per pytest-geval dat
  `decide_budget`, `parse_app_server`, `read_rollout`, `read_usage`, `cmd_read` of `cmd_budget`
  raakt. De naam is de pytest-naam in CamelCase. Lees `tests/pack/fixtures/codex_usage/*`. Wat pytest
  met `monkeypatch.setattr` vervangt, wordt in Go een parameter (`read`, `now`) of een
  `CodexReader`-veld. Noteer bij elke test in een comment welk pytest-geval hij overneemt.
- [ ] **Stap 2:** `go test ./internal/usage ./internal/cli -run 'Budget|CodexUsage'` → FAIL
  (undefined).
- [ ] **Stap 3: implementeer.** `CodexState` krijgt `ResetCredits json.RawMessage` met de tag
  `json:"reset_credits"`: `null` bij het rollout-log, anders het object dat `_reset_credits` bouwt.
  `SourceFile` wordt `*string` zonder `omitempty`, zodat er `null` staat zoals in Python. Daarmee
  verandert ook `lighthouse usage codex`; zijn test past mee. Dat is een bewuste gedragswijziging
  (`source_file: null`, een `reset_credits`-sleutel, timeout uit config): werk
  `docs/usage-venster.md:110-119` mee bij (de zinnen over `source_file`, `reset_credits` en de vaste
  20 s) en noem het in het changelog-fragment. Een onleesbare `rateLimitResetCredits` behandelen
  de twee kanten verschillend. De gedeelde parser geeft de credits-fout apart terug (naast een
  geldige stand met `reset_credits` `null`). `codex-usage read` en `budget` maken er
  `not measurable: reset credits unreadable: …` van (exit 1, bij budget `not_measurable`), zoals
  `codex_usage.py:205-213,235`. `usage codex` en `serve` (`internal/cli/serve.go:154`) negeren die fout
  en houden de stand geldig. Zet één test op elke kant. De timeout per home komt uit
  `codex_usage.app_server_timeout_seconds` (default 20). De drempels komen uit
  `codex_usage.review_pct_session` (90) en `review_pct_week` (95). De volgorde en de redenteksten
  van `decide_budget` zijn letterlijk (`codex_usage.py:355-412`).
- [ ] **Stap 4:** dezelfde `go test` → PASS. Daarna de mutatiecheck: draai in `DecideBudget` de
  volgorde week/5h om. Minstens één test wordt rood. Zet het terug.
- [ ] **Stap 5: commit** `feat(codex-usage): budget en reading in Go (#282)`.

### Taak 2: `lighthouse review-run`

**Bestanden:**
- Nieuw: `internal/review/runner.go` (`RunRound`), `internal/review/prompt.go` (`BuildPrompt`)
- Nieuw: `internal/cli/reviewrun.go`
- Test: `internal/review/prompt_test.go`, `internal/review/runner_test.go`,
  `tests/pack/test_adversarial_review_sh.py` (naar de binary)

**Interfaces:**
- Consumeert: `usage.ReadBudget` (taak 1).
- Produceert:
  ```
  lighthouse review-run [--kind spec|plan] [--verify] [--skill-dir DIR]
                        [--prompt F] [--schema F] [--context PATH]... <DOC> <ROUND> <OUT> [PRIOR] [LEVEL]
  ```
  `--kind` is standaard `spec`. `--skill-dir` is standaard `.claude/skills/lighthouse-review-<kind>`
  (repo-relatief). Exit 1 komt voor bij de fouten die het script nu ook met 1 meldt: een prompt
  die niet te bouwen is (`--context given…`, `unsubstituted placeholder`) en `incomplete result`
  (`review.sh:162-164,168-169,220-222`). Zonder `--verify` zijn de defaults `reviewer-prompt.md` en
  `findings.schema.json` in die map, met `--verify` `verifier-prompt.md` en
  `verification.schema.json`. `--prompt` en `--schema` gaan voor. Vlaggen mogen overal staan
  (`cmd.Flags().SetInterspersed(true)`). Exit 0, 2, 3, 4, 5 en 6 komen uit de kop `review.sh:1-36`,
  de stderr-teksten uit `review.sh:63-200`. De `ok | reviewer: …`-regel blijft letterlijk.
  ```go
  func BuildPrompt(template []byte, doc, repo, round, prior string, contexts []string) (string, error)
  ```

- [ ] **Stap 1: pytest naar de binary.** `tests/pack/test_adversarial_review_sh.py` roept nu
  `review.sh` aan. Dat wordt `[lighthouse_bin, "review-run", "--kind", kind, *("--verify",)?, …]`,
  met `LIGHTHOUSE_REPO_ROOT` op de tijdelijke repo. `test_runners_are_allowlisted` checkt voortaan
  `Bash(lighthouse review-run:*)` in `pack/core/permissions.json`. Zet een agents-geval erbij,
  `test_agents_skill_dir_reads_its_own_templates`: `--skill-dir .agents/skills/lighthouse-review-plan`
  stuurt de prompt uit die map. Zet ook
  `test_doc_path_with_space_reaches_codex_verbatim` erbij (Review-focus 3). De twee tests
  `test_production_path_calls_codex_usage_budget` en `test_repo_root_from_lighthouse_repo_root`
  (`:495-545`) eisen `codex_usage.py` en kunnen niet blijven. Vervang ze door seam-vrije
  versies: een nep-`codex` op het PATH die `app-server` beantwoordt met een
  `account/rateLimits/read`-stand boven `review_pct_session`, en `LIGHTHOUSE_REPO_ROOT` op een
  repo waarvan `lighthouse.toml` de drempel verlaagt. Eis exit 6, de regel
  `CODEX_BUDGET: fallback — …`, een `budget.json` met `decision=fallback` en de drempels uit die
  `lighthouse.toml`, en dat `codex exec` niet is aangeroepen. Mutatiecheck: laat `ReadBudget`
  altijd een fout geven; de test moet rood worden.
- [ ] **Stap 2:** `uv run pytest tests/pack/test_adversarial_review_sh.py -q` → FAIL (unknown
  command).
- [ ] **Stap 3: Go-unittests.** `TestBuildPromptByteIdentical` legt de prompt vast die `review.sh`
  nu bouwt. Genereer de fixture eenmalig met het oude script onder
  `tests/pack/fixtures/review_run/prompt-*.txt`, voor deze combinaties: wel of geen sidecar, wel of
  geen context, en een template zonder `{{CONTEXT_PATHS}}` met `--context`. Die laatste moet de
  fout `--context given, but the template has no {{CONTEXT_PATHS}}` geven. Zet ook
  `TestBudgetPanicIsNotMeasurable` erbij (Review-focus 4).
- [ ] **Stap 4: implementeer** `RunRound`, met deze volgorde uit `review.sh`. Eerst de vlaggen en
  de paden, dan `codex` op het PATH (exit 3). Daarna `CODEX_SPECREVIEW_HOME` (default
  `~/.codex-specreview`): `auth.json` kopiëren (exit 4 als `~/.codex/auth.json` ontbreekt) en
  `config.toml` maken met precies `model = "gpt-5.6-sol"\nmodel_reasoning_effort = "high"\n` als
  hij leeg is. Dan het budget. `LIGHTHOUSE_CODEX_BUDGET_CMD` blijft de test-seam (op spaties
  gesplitst), anders `usage.ReadBudget` in-process, met `recover`. Het pad-controleren van document
  en context gebeurt na de `codex`-check, zoals in `review.sh:71-82`. Het budget gaat naar
  `<OUT>.budget.json`, bij code 6 met `CODEX_BUDGET: fallback — <reason>` en exit 6. Daarna komen de
  prompt en `codex exec` met dezelfde argv: `--sandbox read-only`, het optionele
  `-c model_reasoning_effort=<LEVEL>`, `--cd`, `--output-schema`, `-o`, `--json` en de prompt.
  Stdin is leeg, stdout gaat naar `<OUT>.events`, stderr naar `<OUT>.err`. Bij een fout volgen
  `CODEX_FAILED (exit N). Last lines of …` en de laatste 15 regels, met exit 5. Tot slot de
  resultaatcheck en de `ok`-regel.
- [ ] **Stap 5:** `go test ./internal/review` en de pytest van stap 2 → PASS. Mutatiecheck: laat
  de sidecar-tak weg. `test_sidecar_changes_only_the_prior_block` wordt rood.
- [ ] **Stap 6: commit** `feat(review-run): de review-runner in Go (#282)`.

### Taak 3: `lighthouse plan-review-record`

**Bestanden:**
- Nieuw: `internal/review/record.go` (`Start`, `Reserve`, `BuildRecord`, `Write`),
  `internal/review/openpoints.go` (`AcceptOpen`, `PROpenPoints`, `RenderOpen`)
- Nieuw: `internal/cli/planreviewrecord.go`
- Test: `internal/review/record_test.go`, `internal/review/openpoints_test.go`

**Interfaces:**
- Produceert de vier subcommando's van `plan_review_record.py:1-72`, met dezelfde vlaggen, de
  regel dat `--issue`, `--issue-body` en `--issue-fetched-at` samengaan, en exit 0, 2, 3 en 4.
  `schema_version` blijft `5`. Voor `pair.go` (taak 4):
  ```go
  var DefaultSkillDirs = map[string]string{"plan": ".claude/skills/lighthouse-review-plan", "spec": ".claude/skills/lighthouse-review-spec"}
  func ReadMeta(attemptDir string) (map[string]any, error) // attempt.json; kind ontbreekt → "plan"
  type Deps struct{ HostRefusal func(kind string) *string; CodexVersion func() *string; Now func() time.Time }
  ```

- [ ] **Stap 0: gedeelde fixtures eerst (spec §9, Review-focus 6).** Verplaats de invoer en de
  verwachte uitvoer van de inline gevallen uit `tests/pack/test_plan_review_record.py` naar
  `tests/pack/fixtures/plan_review_record/` (JSON per geval), laat pytest die fixtures lezen en
  draai pytest groen tegen Python. Commit dat apart (`test(plan-review-record): …`).
- [ ] **Stap 1: Go-tests op dezelfde fixtures.** Alle 72 gevallen, één
  Go-test per geval. `monkeypatch.setattr(rec, "host_refusal"|"codex_version", …)` wordt een
  `Deps`-veld. `rec.main([...])` wordt een CLI-test in `internal/cli/planreviewrecord_test.go`
  (`NewRoot(in, out, errOut)`, `SetArgs`, `execute(root, errOut)` uit `internal/cli/root.go:37-53`),
  zodat exitcode en stderr mee getest worden; `internal/review` kan `internal/cli` niet importeren.
  Elke literale stderr-regel (zoals `roborev_gate: …`) schrijft het commando zelf, en het geeft
  `&exitError{code, errSilent}` terug, zodat `lighthouse: ` er niet voor komt. Hashes en record
  vergelijk je na parsen met de verwachte waarden uit de fixtures.
- [ ] **Stap 2:** `go test ./internal/review -run 'Record|OpenPoints|AcceptOpen|Start'` → FAIL.
- [ ] **Stap 3: implementeer.** Het data-pad komt uit `datadir.Dir`, het host-oordeel uit
  `config.LoadHost` met dezelfde stderr-regel als `host_refusal`. De headings komen uit
  `config.Heading("open_after_review", lang)` en `config.Variants`. Bewaar de atomiciteit: eerst
  een `mkdir`-reservering, dan een staging-map, `record.json` als laatste en één `os.Rename`. De
  sluitwoorden met `#N` neutraliseer je zoals `render_open` het doet.
- [ ] **Stap 4:** dezelfde `go test` → PASS. Mutatiecheck: laat `accepted_open` als sluitende
  beslissing weg. Een `AcceptOpen`-test wordt rood.
- [ ] **Stap 5: commit** `feat(plan-review-record): meetrecord en open punten in Go (#282)`.

### Taak 4: `lighthouse review-pair blind|merge`

**Bestanden:**
- Nieuw: `internal/review/pair.go` (`Blind`, `Merge`, `validateFindings`)
- Nieuw: `internal/cli/reviewpair.go`
- Test: `internal/review/pair_test.go`

**Interfaces:**
- Consumeert: `DefaultSkillDirs`, `ReadMeta` (taak 3).
- Produceert `lighthouse review-pair blind|merge <ATTEMPT_DIR>`, met exit 0, 2 en 4
  (`SHADOW-PAIR FAILED: <reden>`). Bij 4 schrijft het geen uitvoerbestand. Seams:
  `type PairDeps struct{ Coin func() bool; TreeState func(root string) (head string, clean bool, err error) }`.

- [ ] **Stap 0: gedeelde fixtures eerst.** Zoals taak 3 stap 0, voor `tests/pack/test_review_pair.py`
  naar `tests/pack/fixtures/review_pair/`; pytest groen tegen Python; apart committen.
- [ ] **Stap 1: Go-tests op die fixtures.** Alle 20 gevallen, met `_coin` en
  `_tree_state` als `PairDeps`. De schemavalidatie zonder `jsonschema` port je als
  `validateFindings` met precies de foutteksten van `review_pair.py:96-160`.
- [ ] **Stap 2:** `go test ./internal/review -run Pair` → FAIL.
- [ ] **Stap 3: implementeer.** `pair.json` wordt één keer geloot en daarna hergebruikt. De
  celescape (`|` en regeleinde) volgt `review_pair.py:85-95`.
- [ ] **Stap 4:** PASS. Mutatiecheck: loot opnieuw als `pair.json` al bestaat. De
  hergebruiktest wordt rood.
- [ ] **Stap 5: commit** `feat(review-pair): gepaarde ronde 1 in Go (#282)`.

### Taak 5: skills, toestemming en opruimen (PR A)

**Bestanden:**
- Wijzig: `pack/core/skills/review-spec/{claude,agents}/SKILL.md`,
  `pack/core/skills/review-plan/{claude,agents}/SKILL.md`, `pack/core/skills/write-pr/SKILL.md:300`
- Wijzig: `pack/core/permissions.json`, `pack/core/templates/pull_request_template.md`,
  `.github/pull_request_template.md`
- Weg: `pack/core/skills/review-{spec,plan}/{review,verify}.sh`,
  `pack/core/scripts/{plan_review_record,review_pair}.py`, `tests/pack/test_plan_review_record.py`,
  `tests/pack/test_review_pair.py`
- Wijzig: `internal/packs/core_test.go:37-48,77-88,125-134`, `pack/SOURCES.toml`,
  `pack/core/requires.toml`, `docs/site/data/{config,loop}.json`,
  `tests/pack/test_adversarial_review_skills.py`, `tests/pack/test_write_pr_skill.py`,
  `tests/pack/test_skill_contracts.py`, `tests/pack/test_pr_template.py`
- Geïnstalleerd: uitvoer van `init --upgrade`

- [ ] **Stap 1: skilltekst-tests eerst.** In `test_adversarial_review_skills.py` en
  `test_skill_contracts.py` komt erbij: geen `python3` en geen `review.sh`/`verify.sh` in beide
  review-skills. De claude-variant roept `lighthouse review-run --kind <kind>` aan, met
  `--verify` vanaf ronde 2. De agents-variant geeft ook
  `--skill-dir .agents/skills/lighthouse-review-<kind>` mee. `test_write_pr_skill.py` eist
  `lighthouse plan-review-record pr-open-points`. Draai de tests → FAIL.
- [ ] **Stap 2: tekst.** Vervang elke aanroep met dezelfde argumenten:
  `python3 .lighthouse/scripts/plan_review_record.py` wordt `lighthouse plan-review-record`,
  `review_pair.py` wordt `lighthouse review-pair`. `.claude/skills/lighthouse-review-<kind>/review.sh`
  wordt `lighthouse review-run --kind <kind>`, en `verify.sh` dezelfde aanroep met `--verify`. Zinnen
  die `review.sh` als wrapper of runner beschrijven, noemen voortaan `lighthouse review-run`. De
  allowlist-alinea (`review-spec/claude/SKILL.md:255-263`) toont de nieuwe regel. De valkuil
  "path absolute" (`review-plan/claude/SKILL.md:504`) wordt: de vaste vorm `lighthouse review-run`,
  nooit een pad naar de binary.
- [ ] **Stap 3: toestemming.** `pack/core/permissions.json` wordt `{"allow":
  ["Bash(lighthouse review-run:*)"]}`. `init --upgrade` haalt de vier oude regels uit
  `.claude/settings.json`. Controleer dat in de diff van stap 6. Blijven ze staan, dan is dat een
  defect in de opruimroute van `reconcile` en een eigen fix met test in deze taak.
- [ ] **Stap 4: opruimen.** Haal de bestanden onder *Weg* weg. Werk dan alles bij dat ze noemt: de
  lijsten in `core_test.go`, `SOURCES.toml` (de entries van de weggehaalde bestanden),
  `requires.toml` (de unitnamen `plan_review_record` en `review_pair`, en `review-spec` en
  `review-plan` zelf, uit `git`, `python3` en `tomllib`; zie `pack/core/requires.toml:7,9` en
  `tests/pack/test_skill_contracts.py:355-367`), de promptsjablonen `reviewer-prompt.md` en
  `verifier-prompt.md` van beide skills (de kop noemt `review.sh`/`verify.sh`) en de site-data (`readers`/links naar de Go-bestanden; `uv run pytest
  tests/test_site_config.py tests/test_site_reference.py` wijst aan wat nog niet klopt). Werk ook
  de PR-templates bij (`lighthouse plan-review-record pr-open-points`). `codex_usage.py` blijft
  staan (bevroren, afnemer `roborev_gate.py`).
- [ ] **Stap 5: init.** `go run ./cmd/lighthouse init --upgrade`, en daarna nog een keer: die tweede
  run zegt `No changes.`
- [ ] **Stap 6: bewijs.**
  `grep -rn 'python3' .claude/skills/lighthouse-review-{spec,plan} .agents/skills/lighthouse-review-{spec,plan}`
  → geen treffer. Daarna `go test ./... && uv run pytest -q` → groen.
- [ ] **Stap 7: de gepaarde ronde 1 zonder Python, als test.** Zet
  `test_paired_round_one_without_python3` in `tests/pack/test_adversarial_review_sh.py`. De test
  maakt een `bin`-map met symlinks naar `git`, `sh`, `cat`, `mkdir` en `lighthouse_bin`, plus de
  nep-`codex` die de andere tests al gebruiken, en zet `PATH` op alleen die map. Er staat geen
  `python3` op. Op een klein spec-document in een tijdelijke repo draait de test dan
  `lighthouse plan-review-record start --kind spec`, twee keer `lighthouse review-run`
  (`round-1.codex.json` en `round-1.fallback.json`), `lighthouse review-pair blind`, een
  blind-decisions-bestand en `lighthouse review-pair merge`. De tijdelijke repo bevat beide
  skills (met `findings.schema.json`) en is gecommit en schoon, anders weigert `blind`
  (`review_pair.py:72-77,149-152`). Elke stap moet exit 0 geven, en
  `round-1.json` moet bestaan. Mutatiecheck: zet in de promptbouw van `RunRound` (niet in de
  budgetroute, die `recover` slikt) een `exec.Command("python3", …)`.
  De test wordt rood. Haal die regel weer weg.
- [ ] **Stap 8: changelog en commit.** Maak `changelog.d/issue-282-review-keten.md` met
  `type: changed` en de tekst: "The spec and plan reviews run through `lighthouse review-run`,
  `lighthouse plan-review-record` and `lighthouse review-pair`; they no longer need `python3`." Commit
  per logische wijziging (skills, opruimen, init, changelog). Open dan PR A via
  `/lighthouse-write-pr`.

## PR B: roborev

Nieuwe worktree vanaf `main` na de merge van A, branch `issue-282-roborev`.

### Taak 6: `lighthouse roborev-gate`

**Bestanden:**
- Nieuw: `internal/roborev/{gate,classify,state,size}.go`, `internal/cli/roborev.go`
- Test: `internal/roborev/*_test.go`, met `tests/pack/fixtures/roborev/*.md`

**Interfaces:**
- Consumeert: `usage.ReadBudget` (taak 1).
- Produceert `gate | fixed | body-line | classify [<path>] | record --skip|--done … [--decisions
  --out --raw --range]`, met exit 0, 2 (`roborev_gate: <fout>`) en 3
  (`COULD NOT CHECK: <reden>`). De state blijft in `<git-dir>/lighthouse/roborev/<branch met - >.json`,
  met de Nederlandse sleutels `uitkomst`, `reden` en `bevindingen`.
  ```go
  type Budget func() (code int, stdout string)
  func Gate(root string, budget Budget) (map[string]any, error) // ordered
  ```

- [ ] **Stap 0: gedeelde fixtures eerst.** Zoals taak 3 stap 0, voor `tests/pack/test_roborev_gate.py`
  naar `tests/pack/fixtures/roborev_gate/` (let op: `parametrize`-rijen zijn aparte gevallen);
  pytest groen tegen Python; apart committen.
- [ ] **Stap 1: Go-tests op die fixtures.** Alle 57 gevallen. De `budget`-lambda
  wordt een `Budget`. De nep-binaries `roborev`/`codex` op het PATH blijven: een helper zet ze in
  `t.TempDir()`, met `t.Setenv("PATH", …)`. Neem de tabel uit `test_gate_skips_on_the_budget`
  letterlijk over, dus ook `Codex-budget: onleesbaar - exit 0` bij geen JSON.
- [ ] **Stap 2:** `go test ./internal/roborev` → FAIL.
- [ ] **Stap 3: implementeer.** De groottenregel staat in `roborev_gate.py:30-37`:
  `git diff --numstat --no-renames -z`, zonder binaries en zonder `docs/`. Prosa- en data-extensies
  tellen niet mee, behalve onder `.github/workflows/`. De default-`Budget` roept `usage.ReadBudget`
  aan en marshalt de uitkomst. Exit 6 betekent fallback.
- [ ] **Stap 4:** PASS. Mutatiecheck: tel `docs/` wel mee. Een groottetest wordt rood.
- [ ] **Stap 5: commit** `feat(roborev-gate): roborev-gate in Go (#282)`.

### Taak 7: `lighthouse roborev-comment render`

**Bestanden:**
- Nieuw: `internal/roborev/comment.go` (`Render`, `LogLine`), uitbreiding van `internal/cli/roborev.go`
- Test: `internal/roborev/comment_test.go`, `tests/pack/fixtures/roborev/comment-*.md`

**Interfaces:**
- Produceert `lighthouse roborev-comment render --pr N --bron lokaal|ci --state P --raw P`, met
  exit 0 of exit 2 (`roborev_comment: <fout>`). Exit 2 krijg je ook bij een state die geen `done`
  is.

- [ ] **Stap 1: fixtures vastleggen.** Draai met de huidige `roborev_comment.py` drie gevallen en
  sla stdout op onder `tests/pack/fixtures/roborev/comment-{basis,vijandig,lang}.md`. `vijandig`
  heeft een reden met `-->`, `<x>`, `|`, `é` en een regeleinde, `lang` een raw van 70.000 × `é`.
  Port daarna de 14 gevallen uit `test_roborev_comment.py`, met daarbij
  `TestRenderCutsOnRunesNotBytes` en `TestLogLineEscapesHostileReason` (Review-focus 1 en 2), die de
  fixtures byte voor byte vergelijken.
- [ ] **Stap 2:** FAIL. **Stap 3:** implementeer. `LIMIT` is 65536 runes. Alleen raw wordt
  ingekort, met `… ingekort tot de limiet van GitHub`. De logregel volgt `roborev_comment.py:35-39`:
  compacte scheidingstekens `,` en `:` zonder spaties, niet-ASCII rauw (geen `\uXXXX`; `SetEscapeHTML(false)`),
  en daarna de drie escapes `<`, `>` en `-`. De verwachting van `TestLogLineEscapesHostileReason`
  heeft dus een rauwe `é`.
- [ ] **Stap 4:** PASS. Mutatiecheck: `len(raw)` in bytes. `TestRenderCutsOnRunesNotBytes` wordt
  rood.
- [ ] **Stap 5: commit** `feat(roborev-comment): PR-comment van roborev in Go (#282)`.

### Taak 8: skills en opruimen (PR B)

**Bestanden:**
- Wijzig: `pack/core/skills/write-pr/SKILL.md` (roborev-regels 50–120, 307, 336),
  `pack/core/skills/fix-pr/{claude,agents}/SKILL.md:206,219,220`
- Weg: `pack/core/scripts/{roborev_gate,roborev_comment,codex_usage}.py`,
  `tests/pack/test_{roborev_gate,roborev_comment,codex_usage}.py`
- Wijzig: `core_test.go`, `SOURCES.toml`, `requires.toml` (`codex = [...]` zonder `codex_usage`),
  `docs/site/data/{config,loop}.json`, `tests/pack/test_{write_pr,fix_pr}_skill.py`,
  `tests/test_site_config.py:250-252` (leest `codex_usage.py`)
- Geïnstalleerd: uitvoer van `init --upgrade`

- [ ] **Stap 1: skilltekst-tests eerst.** `test_write_pr_skill.py` en `test_fix_pr_skill.py`
  eisen `lighthouse roborev-gate …` en `lighthouse roborev-comment render …` en verbieden
  `roborev_gate.py`/`roborev_comment.py`. Draai → FAIL.
- [ ] **Stap 2:** vervang de aanroepen met dezelfde argumenten. Laat `pr_body_gate.py` en
  `evidence.py` (stap 6) en `measure_fix_pr_kpi.py` (stap 10) staan.
- [ ] **Stap 3:** haal de bestanden onder *Weg* weg en werk de lijsten bij, zoals in taak 5 stap 4.
  Controleer met `grep -rn 'codex_usage' pack/ internal/ tests/`: alleen nog de config-sleutels
  `codex_usage.*` horen te treffen.
- [ ] **Stap 4:** `init --upgrade` twee keer (de tweede zegt `No changes.`), en daarna
  `go test ./... && uv run pytest -q` → groen.
- [ ] **Stap 5:** `grep -rn 'roborev_\|codex_usage.py' .claude .agents .lighthouse` → geen
  treffer.
- [ ] **Stap 6: changelog en commit.** Maak `changelog.d/issue-282-roborev.md` met `type: changed`:
  "`lighthouse roborev-gate`, `lighthouse roborev-comment` and `lighthouse codex-usage` replace the
  Python scripts in `write-pr` and `fix-pr`." Open PR B.

## PR C: check-instructions

Eigen worktree vanaf `main`, branch `issue-282-check-instructions`. Kan parallel met A en B.

### Taak 9: `lighthouse check-instructions`

**Bestanden:**
- Nieuw: `internal/checkinstr/{run,skills,mirror,overlap,rules,pairing,hooknames,size}.go`
- Nieuw: `internal/cli/checkinstructions.go`
- Wijzig: `internal/initcmd/budget_parity_test.go`, `internal/wordcount/wordcount.go:1` (comment)
- Test: `internal/checkinstr/*_test.go`, `tests/pack/test_check_instructions.py` (naar de binary)

**Interfaces:**
- Produceert:
  ```go
  // Run draait de checks 1, 2, 3, 4, 5, 7, 8, 10, 11 en 14 in de volgorde van het script.
  // Regels als "ok    …", "FAIL  …", "SKIPPED  …". Exit 1 bij minstens één FAIL, 2 bij
  // COULD NOT CHECK (stderr: "check-instructions: COULD NOT CHECK — <reden>").
  func Run(root, personal string, out, errOut io.Writer) int
  func InstructionWords(root string) (label string, words int, err error) // check 10
  ```
  `personal` komt uit `CHECK_INSTRUCTIONS_PERSONAL`, met als default `$HOME/.claude/skills`.

- [ ] **Stap 1: pytest naar de binary.** `SCRIPT` in `test_check_instructions.py` wordt
  `[lighthouse_bin, "check-instructions"]`, met `LIGHTHOUSE_REPO_ROOT` op de tijdelijke boom. Tests
  die een ontbrekend `python3`, `wc`, `sha256sum`/`shasum` of `diff` naspelen, gaan weg. De binary
  heeft die tools niet nodig, dus die SKIPPED/FAIL-takken bestaan niet meer. Noem ze bij naam in de
  commitbody als bewuste afwijking. `git` blijft nodig: zonder `git` geeft check 11 SKIPPED
  `mirror-content: diff could not compare a skill pair, so the content was not measured`, zoals nu.
  Leg vóór de port met het bash-script vast (`--print-mirror-exceptions`, en de check-11-uitvoer)
  fixtures onder `tests/pack/fixtures/check_instructions/` voor: een gewijzigde regel, een
  ontbrekende slotnewline, een binair bestand, een bestand aan één kant en een gemarkeerde skill.
  Zet `TestMirrorFixtures` erbij als Go-test die de Go-uitvoer byte voor byte met die fixtures
  vergelijkt (Review-focus 5). `_record_current_differences` (`:220-229`) mag niet meer zelf de
  verwachting uit de binary halen. De interface krijgt `--print-mirror-exceptions` (exit 0; exit 4
  met `SKIPPED  diff could not compare a skill pair` op stderr). De FAIL-hint van check 11 noemt
  `lighthouse check-instructions --print-mirror-exceptions > <rel>`; pas
  `tests/pack/test_check_instructions.py:1164,1174` aan en noem dit in de commitbody als bewuste
  afwijking.
- [ ] **Stap 2:** `uv run pytest tests/pack/test_check_instructions.py -q` → FAIL.
- [ ] **Stap 3: implementeer per check, in de volgorde van het script.** Een paar regels zijn
  niet-triviaal:
  - **Frontmatter.** `frontmatter_field`/`skill_meta` met folded (`>`) en literal (`|`)
    blockscalars (`check-instructions.sh:122-191`). Een lege waarde telt als ontbrekend.
  - **Sorteren** gaat op bytes (`LC_ALL=C`): `sort.Strings`.
  - **Check 2.** De directory-hash over het relatieve pad plus de inhoud, met `crypto/sha256`.
  - **Check 11.** Shell uit naar `git diff --no-index --no-color --no-ext-diff --no-textconv -U0
    --diff-algorithm=myers --no-indent-heuristic` en zet om naar `<`/`>` zoals de awk het doet.
    Alleen zo blijft de uitvoer bytegelijk aan wat het bash-script vastlegde (de fixtures). De
    FAIL-weergave (nu `diff -u … | tail -n +3`) wordt `git diff --no-index --no-color -U3
    --diff-algorithm=myers` tussen twee tijdelijke bestanden (vastgelegd, nu). Alles vóór de eerste
    `@@` valt weg, en elke regel krijgt zes spaties ervoor. Dit is alleen weergave: het oordeel
    hangt af van de vergelijking van de strings, niet van deze uitvoer.
  - **Check 8 en 14.** `settings.json` met `encoding/json`. Ongeldig of geen object geeft de
    FAIL-tekst van het script.
  - **Check 10.** `wordcount.Count` over `CLAUDE.md` plus de geïmporteerde `AGENTS.md`. Het plafond
    komt uit `project.instructions_word_ceiling` (default 3000), het pad uit
    `paths.mirror_exceptions`. Een kapotte config geeft COULD NOT CHECK met exit 2.
- [ ] **Stap 4: `budget_parity_test.go`.** Draai `checkinstr.InstructionWords` op dezelfde bytes
  in plaats van bash. Het rood-als blijft: rood als init en check 10 niet meer met dezelfde teller
  dezelfde bestanden tellen. De skips voor `bash` en `python3` vervallen.
- [ ] **Stap 5:** `go test ./internal/checkinstr ./internal/initcmd` en de pytest van stap 2 →
  PASS. Draai ook `lighthouse check-instructions` op deze checkout: hetzelfde oordeel per check als
  `.lighthouse/scripts/check-instructions.sh`, regel voor regel (`diff <(…) <(…)`, alleen tekens die
  van de tool afhangen mogen verschillen). Mutatiecheck: tel in check 10 met `strings.Fields`.
  `TestBudgetMatchesCheck10` wordt rood.
- [ ] **Stap 6: commit** `feat(check-instructions): instructiechecks in Go (#282)`.

### Taak 10: skills en opruimen (PR C)

**Bestanden:**
- Wijzig: `pack/core/skills/place-instruction/SKILL.md:20,118,134,235-249`, `docs/host-assumptions.md:56`
- Weg: `pack/core/scripts/check-instructions.sh`
- Wijzig: `core_test.go`, `SOURCES.toml:241`, `requires.toml` (`check-instructions` uit `git`,
  `python3`, `tomllib`), `docs/site/data/config.json`, `tests/pack/test_place_instruction_skill.py`,
  `tests/pack/test_skill_contracts.py`
- Geïnstalleerd: uitvoer van `init --upgrade`

- [ ] **Stap 1:** `test_place_instruction_skill.py` eist `lighthouse check-instructions` en
  verbiedt `check-instructions.sh` → FAIL.
- [ ] **Stap 2:** vervang `.lighthouse/scripts/check-instructions.sh` door `lighthouse
  check-instructions`. De link in `docs/host-assumptions.md:56` (sha256-fallback) vervalt. Zet er
  een zin voor in de plaats: de binary hasht zelf.
- [ ] **Stap 3:** haal het script weg en werk de lijsten bij. Daarna `init --upgrade` twee keer
  (`No changes.`) en `go test ./... && uv run pytest -q` → groen.
- [ ] **Stap 4: grep van het issue.** Doe een verse `lighthouse init` met `core`, `go`, `node`,
  `shell` en `expo` in een tijdelijke repo, en draai daar `grep -rn python3 .`. Geen van de 67
  treffers uit de issue-comment van #282 komt nog terug, behalve die van stap 6, 8 en 10/11
  (`pr_body_gate.py`, `evidence.py`, `measure_fix_pr_kpi.py`, `measure_skill_surface.py`). Zet de uitvoer in de PR.
- [ ] **Stap 5: changelog en commit.** Maak `changelog.d/issue-282-check-instructions.md` met
  `type: changed`: "`lighthouse check-instructions` replaces `.lighthouse/scripts/check-instructions.sh`
  and no longer needs `python3`, `wc` or `sha256sum`." Open PR C.

## Klaar

| Acceptatiecriterium | Bewijs |
|---|---|
| Gepaarde ronde 1 zonder `python3` | `test_paired_round_one_without_python3` (taak 5 stap 7) |
| Review-run vraagt geen toestemming na `init` | `Bash(lighthouse review-run:*)` in `.claude/settings.json` na `init`; `test_runners_are_allowlisted` |
| Zelfde oordeel per check, `budget_parity_test.go` groen | taak 9 stap 1 en 5 |
| Weg is weg, bevroren is ongewijzigd | `git diff --stat main -- pack/core/lib/` leeg; de bestanden onder *Weg* bestaan niet meer |
| `init --upgrade` gecommit, CI `No changes.` | taak 5, 8 en 10 |
| `minor`-fragment | drie `changelog.d/issue-282-*.md` met `type: changed` |

## Open na review

Ronde 3 haalde de rondegrens met deze punten open. Ze zijn aanvaard als `accepted_open`;
de volgende fase of de PR-review weegt ze opnieuw.

- **r3-f1** (MAJOR, Taak 1 stap 3 (credits-fout, read/budget-kant)): The split is named, but the diff pins codex-usage read/budget to exit 1 with "not measurable: reset credits unreadable: …" as in codex_usage.py. In Python that NotMeasurable is raised inside read_usage's per-home loop, so it only fails that home: the next home, then the rollout log, are tried, and the final message is "app-server: <home>: …; rollout-log: …". The diff's shared parser returns a valid state plus a separate error, and Go CodexReader.Read returns at the first nil-error state, so the loop stops before home b and the rollout log. The diff does not say where read/budget apply the error.
