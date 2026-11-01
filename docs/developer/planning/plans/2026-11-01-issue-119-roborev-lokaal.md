# Roborev lokaal in write-pr, en fix-pr vanuit de dispatcher — implementatieplan

> **Voor agents die dit uitvoeren:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. De stappen gebruiken checkboxes (`- [ ]`).

**Doel:** `/lighthouse-write-pr` draait boven de drempel en met Codex-budget één lokale
roborev-review vóór `gh pr create` en legt elke bevinding parseerbaar vast op de PR; de
dispatcher start bij een rode PR `/lighthouse-fix-pr`, hooguit drie keer per PR.

**Architectuur:** twee kleine Python-scripts in `pack/core/scripts/` dragen alles wat meetbaar
is: `roborev_gate.py` (drempel, budget, reviewuitvoer en herstart, de bodyregel) en
`roborev_comment.py` (de comment met log-blok, tabel en ingekorte ruwe output). De skill houdt
alleen de zichtbare aanroep van `roborev ci review` en het beoordelen per bevinding. In de
dispatcher sluit `settle` een klare run op een rode PR als `handoff` en geeft de fix-pr-start door
aan `decideIssue`, dat hem vóór `c.next()` plant, achter de bestaande poorten. Een post-pass in
`Decide`, na de rem, maakt van elke fix-pr-close zonder start alsnog `returned` "PR rood".

**Tech stack:** Python 3 (stdlib, `lighthouse_lib.config`), pytest; Go (`internal/dispatch`),
JSON-fixtures in `internal/dispatch/testdata/decide/`; markdown-skills in `pack/core/skills/`.

**Spec:** [2026-11-01-issue-119-roborev-lokaal-design.md](../specs/2026-11-01-issue-119-roborev-lokaal-design.md)

## Vaste eisen

- Python-scripts volgen het patroon van `pack/core/scripts/plan_review_record.py`; Go volgt
  `/lighthouse-go-standard`; tests volgen `/lighthouse-test-standard`.
- Code, skilltekst, CLI-sleutels van de scripts en testnamen Engels. Artefacten (PR-body, comment,
  commitberichten, deze docs) Nederlands. De letterlijke teksten hieronder zijn artefact en blijven
  Nederlands.
- Elke wijziging onder `pack/`: `go run ./cmd/lighthouse init --upgrade` en alles wat dat wijzigt
  (`git status`: de geïnstalleerde kopie én `.lighthouse/manifest.toml`) in dezelfde commit; CI
  vereist `No changes.`. Een nieuw script komt ook in de lijst van `internal/packs/core_test.go`
  (alfabetisch) en krijgt een entry met `new = true` in `pack/SOURCES.toml`
  (`tests/test_sources.py`).
- Python-namen onder `pack/**` en `tests/**` zijn Engels (`code-language-check` blokkeert anders):
  `source`, `outcome`, `reason`. De Nederlandse JSON-sleutels en CLI-waarden (`bron`, `uitkomst`,
  `reden`, `--bron lokaal`) zijn stringliterals en blijven.
- Een alinea in een core-skill die `controller` noemt, zegt `(controller run, if present)`
  (`tests/pack/test_skill_contracts.py:102-103`).
- Het V4-deel van `pack/core/skills/write-pr/SKILL.md` krijgt geen regel die met `## ` begint.
- Drempel: **100** coderegels; telregel van paia's `Size`-stap: `git diff --numstat --no-renames
  <base> HEAD`, sla over bij numstat `-` (binair) of pad onder `docs/`, en bij extensie
  `.md .rst .txt .toml .yaml .yml .json .lock .csv .ini .cfg .cff` behalve onder `.github/workflows/`;
  tel toegevoegd + verwijderd.
- Reviewaanroep: `timeout 540 roborev ci review --agent codex --ref <range>`, zonder `--comment`,
  Bash-timeout `600000`, voorgrond, `dangerouslyDisableSandbox: true`.
- Skipredenen, letterlijk: `geen .roborev.toml`, `geen roborev of codex op PATH`,
  `<n> coderegels (drempel 100)`, `Codex-budget: <besluit> - <reden>`, `review faalde: <kop>`,
  `review faalde: <eerste foutregel>`, `review faalde: timeout na 9 min`.
- PR-body: `<!-- roborev-lokaal: <sha> -->` of `Roborev overgeslagen: <reden>`; bij `off` niets.
- Comment: eerste regel `<!-- roborev-verwerkt: <sha> -->`, tweede regel
  `<!-- roborev-log: {"v":1,"sha":…,"bron":"lokaal"|"ci","pr":<nr>,"bevindingen":[…]} -->`;
  bevindingvelden `path`, `line`, `severity`, `uitkomst` (`fixed`|`refuted`|`left`), `commit`,
  `reden`. In de JSON: `<` `>` `-` als `<` `>` `-`. Comment ≤ **65536** tekens.
- Fix-commitbody: een regel `roborev: <path>:<line> (<severity>)`.
- Dispatcher: trigger `fixpr`, trigger key `fixpr:<pr>:<head-sha>`, plafond **3**, nieuwe reden
  `reason_pr_red_ceiling` = nl `PR rood na 3 fix-pr-runs`, en `PR red after 3 fix-pr runs`.

## r3-f1 en r3-f2 opnieuw gewogen

**r3-f1 (§5).** De spec laat het fix-pr-signaal één tick leven: geen start ⇒ de rij sluit als
`returned` "PR rood", ook bij uitstellen. Eén geval ziet `decideIssue` niet: de rem
(`Brake.apply`, `internal/dispatch/brake.go:33`) draait ná `decideIssue` over alle repo's en kan
de start nog in een `defer` veranderen of (bij `Paused`) laten vallen. Dit plan volgt de spec en
dicht dat gat zonder nieuwe regel in `next()`:

- `settle` geeft bij een rode PR onder het plafond op een verse SHA een close `handoff` met
  `ReasonArg = fixpr:<pr>:<sha>` en `SessionID` van de rij, en geeft de start door aan
  `decideIssue`.
- `decideIssue` plant de start als eerste, vóór `c.next()`, alleen als `free`, `OPEN`,
  `Controller` en geen actieve blokkade (een fix-pr is `Executing`). Anders plant het geen start.
- Een post-pass `returnUnstartedFixPR` in `Decide`, ná `Brake.apply`, zet elke close met
  `Outcome handoff` en een `ReasonArg` met prefix `fixpr:` zonder `KindStart` met trigger `fixpr`
  voor hetzelfde repo/issue om in `KindReturn` `reason_pr_red` met dezelfde `RunID` en
  `SessionID`.
- `apply.start` dekt de rest: een fix-pr-start die vóór `runs.Take` stopt (werkplek bezet, een fout
  in `Prepare` of in de busy-check, `apply.go:210-224`) post een 🤖 Teruggegeven "PR rood" zonder
  `RunID`. Een fout ná de claim sluit de run als `start_failed`, en dan neemt `retry` het over.
  Alleen de eerste poging (`Attempt == 1`) post: een tweede poging komt uit `retry`, die elke tick
  opnieuw plant en zelf na twee mislukte starts teruggeeft.
  Zo eindigt elke uitkomst anders dan een start als `returned`, zoals §5 vraagt.

**r3-f2 (§2).** De spec routeert `<branch>.md` zonder `<branch>.json` al naar `classify`. Het plan
pint dat in Task 3, plus het terughalen van al genomen besluiten (`fixed`), en laat `gate` een
skip zelf vastleggen, zodat een herstart nooit een tweede review start.

## Review Focus

1. **Herstart na een afbreek tijdens het verwerken** (`.md` zonder `.json`, al twee fix-commits):
   `gate` geeft `classify`, `fixed` geeft beide commits terug, en geen tweede `roborev`-aanroep.
   Test in Task 3.
2. **Een reden met `-->`, `<script>` of een regeleinde** in het log-blok: het blok blijft één
   regel, sluit niet voortijdig af en `json.loads` geeft de reden ongewijzigd terug. Test in Task 4.
3. **Ruwe output boven 65536 tekens**: alleen de `<details>` wordt ingekort; log-blok en tabel
   volledig. Test in Task 4.
4. **De rem of een bezette werkplek houdt de fix-pr-start tegen** (slots vol, `lighthouse pause`,
   een eigen sessie in de werkplek): het issue krijgt een Teruggegeven "PR rood", en blijft nooit
   stil op een rode PR achter een `handoff` zonder start. Fixtures en apply-tests in Task 6.
5. **Een branchnaam met `/`** (`fix/x`): de uitvoerbestanden heten `fix-x.md`/`fix-x.json` en
   botsen niet met een map. Test in Task 2.

---

### Task 1: Setting `[review] roborev`

**Files:**
- Modify: `pack/core/lib/config.py` (SCHEMA, DEFAULTS, `load`)
- Test: `tests/pack/test_config.py`

**Interfaces:**
- Produces: `config.py review.roborev` print `local` of `off` (default `off`).

- [ ] **Step 1: Schrijf de falende tests**

```python
def test_review_roborev_defaults_to_off(tmp_path): ...      # zonder [review]: lookup == "off"
def test_review_roborev_accepts_local(tmp_path): ...        # roborev = "local" -> "local"
@pytest.mark.parametrize("value", ["pr", "github", "LOCAL"])
def test_review_roborev_rejects_other_values(tmp_path, value):
    # ConfigError met "key review.roborev must be 'local' or 'off'"
```

Volg de opbouw van de bestaande tests in `tests/pack/test_config.py` voor een tijdelijke
`lighthouse.toml`.

- [ ] **Step 2: Draai, verwacht FAIL** — `uv run pytest tests/pack/test_config.py -q -k review_roborev`

- [ ] **Step 3: Implementeer**

`"review.roborev": _STR` in SCHEMA, `"review.roborev": "off"` in DEFAULTS, en in `load` een
enum-check naast die van `forge.kind` (`config.py:369-372`) met de boodschap uit de test.

- [ ] **Step 4: Draai** `uv run pytest tests/pack/test_config.py -q` — PASS. De rij op de
  configuratiekaart volgt in Task 5, als haar readers bestaan.

- [ ] **Step 5: Dogfood en commit**

```bash
go run ./cmd/lighthouse init --upgrade
git status --short   # alles van init --upgrade mee, ook .lighthouse/manifest.toml
git add pack/core/lib/config.py tests/pack/test_config.py .lighthouse/
git commit -m "feat(config): setting [review] roborev, local of off (#119)"
```

---

### Task 2: `roborev_gate.py gate` — drempel, budget, uitvoer per branch

**Files:**
- Create: `pack/core/scripts/roborev_gate.py`
- Modify: `internal/packs/core_test.go` (scriptlijst), `pack/SOURCES.toml` (entry, `new = true`)
- Test: `tests/pack/test_roborev_gate.py`

**Interfaces:**
- Produces (module, importeerbaar zoals `plan_review_record` in zijn test):
  - `state_dir(root: Path) -> Path` — `<git rev-parse --git-dir>/lighthouse/roborev`, absoluut.
  - `branch_stem(branch: str) -> str` — `/` wordt `-`.
  - `count_code_lines(numstat: str) -> int` — de telregel uit *Vaste eisen*.
  - `run_budget() -> tuple[int, str]` — exitcode en stdout van
    `python3 <scripts>/codex_usage.py budget`.
  - `gate(root: Path, budget: Callable[[], tuple[int, str]] = run_budget) -> dict`
  - CLI `roborev_gate.py gate` print `json.dumps(gate(Path.cwd()))`, exit 0; exit 3 met
    `COULD NOT CHECK: <reden>` op stderr bij geen git-repo of een configfout.
- Elk verdict behalve `off` draagt ook `"state": "<state_dir>/<stem>.json"` en
  `"raw": "<state_dir>/<stem>.md"` (absoluut), zodat V4 en een herstart nooit een pad zelf bouwen.
- `gate` geeft, eerste treffer wint:
  1. `{"verdict": "off"}` bij `review.roborev != "local"`;
  2. `{"verdict": "done", "sha": s}` of `{"verdict": "skip", "reason": r}` uit een bestaand
     `<stem>.json` (vorm in Task 3);
  3. `{"verdict": "classify", "raw": "<pad>.md"}` bij `<stem>.md` zonder `<stem>.json`;
  4. skip `geen .roborev.toml`; 5. skip `geen roborev of codex op PATH` (`shutil.which`);
  6. skip `<n> coderegels (drempel 100)` bij n < 100, base = `git merge-base
     origin/<forge.default_branch> HEAD`;
  7. skip `Codex-budget: <decision> - <reason>`, en bij exit ∉ {0, 6} of onleesbare JSON
     `Codex-budget: onleesbaar - exit <code>`;
  8. `{"verdict": "run", "range": "<base>..<HEAD-sha>", "raw": "<pad>.md", "lines": n}`.
  Bij 4–7 schrijft `gate` de skip zelf naar `<stem>.json` (vorm van Task 3, `range` erbij), zodat
  een herstart dezelfde reden geeft.

- [ ] **Step 1: Schrijf de falende tests** (tijdelijke git-repo met `refs/remotes/origin/main` via
  `git update-ref`, `lighthouse.toml` met `[review] roborev = "local"`, `.roborev.toml`, `PATH`
  met nep-`roborev`/`codex` via `monkeypatch.setenv`, budget als lambda)

```python
def test_count_code_lines_follows_the_paia_size_rule():
    numstat = "10\t2\tinternal/x.go\n5\t5\tdocs/a.go\n-\t-\tlogo.png\n7\t0\tREADME.md\n" \
              "3\t1\t.github/workflows/ci.yml\n4\t0\tpack/x.toml\n"
    assert count_code_lines(numstat) == 16
def test_gate_is_off_without_the_setting(repo): assert gate(repo)["verdict"] == "off"
def test_gate_skips_without_roborev_toml(repo): ...         # reason == "geen .roborev.toml"
def test_gate_skips_without_roborev_on_path(repo): ...      # "geen roborev of codex op PATH"
def test_gate_skips_below_the_threshold(repo): ...          # 99 regels -> "99 coderegels (drempel 100)"
def test_gate_runs_at_the_threshold(repo): ...              # 100 regels, budget (0, codex-json) -> run, range "<base>..<head>"
@pytest.mark.parametrize("budget,reason", [
    ((6, '{"decision":"fallback","reason":"5h 93%"}'), "Codex-budget: fallback - 5h 93%"),
    ((0, '{"decision":"not_measurable","reason":"not measurable"}'), "Codex-budget: not_measurable - not measurable"),
    ((1, "Traceback"), "Codex-budget: onleesbaar - exit 1"),
])
def test_gate_skips_on_the_budget(repo, budget, reason): ...
def test_gate_records_its_skip_and_repeats_it(repo): ...    # tweede gate() met budget dat zou runnen: zelfde skip
def test_state_files_of_a_slashed_branch(repo): ...         # branch fix/x -> fix-x.json in state_dir   (Review Focus 5)
def test_every_verdict_names_state_and_raw(repo): ...       # skip, run, done, classify: beide paden, absoluut, onder state_dir
def test_state_dir_is_per_worktree(repo, tmp_path): ...     # git worktree add: andere state_dir
```

- [ ] **Step 2: Draai, verwacht FAIL** — `uv run pytest tests/pack/test_roborev_gate.py -q`
- [ ] **Step 3: Implementeer** `roborev_gate.py` met de interfaces hierboven en een docstring
  WHY in de stijl van `codex_usage.py`.
- [ ] **Step 4: Draai** — PASS; `go test ./internal/packs/` — PASS na de lijst.
- [ ] **Step 5: Dogfood en commit** — `init --upgrade`;
  `git commit -m "feat(roborev): gate met drempel en Codex-budget voor de lokale review (#119)"`

---

### Task 3: `roborev_gate.py classify`, `fixed`, `record`, `body-line`

**Files:**
- Modify: `pack/core/scripts/roborev_gate.py`
- Create: `tests/pack/fixtures/roborev/review-complete.md` (de paia-comment uit PR #1665,
  `issuecomment-6555476169`, zonder de eerste regel `<!-- roborev-pr-comment -->`),
  `review-passed.md`, `review-failed.md` (hand gemaakt naar dezelfde kopvorm),
  `review-local.md` (gemeten in Step 0)
- Test: `tests/pack/test_roborev_gate.py`

**Interfaces:**
- Consumes: `state_dir`, `branch_stem`, `gate` uit Task 2.
- Produces:
  - `classify(raw: str) -> dict` — kop `` ## roborev: <soort> (`<sha>`) ``:
    `Review Complete`/`Combined Review` → `{"verdict": "done", "sha", "kind", "findings": [...]}`;
    `Review Passed` → idem met `findings: []`; `Review Failed`/`Review Skipped` →
    `{"verdict": "skip", "reason": "review faalde: <soort>"}`; geen kop → `review faalde: <eerste
    niet-lege regel, ≤200 tekens>`, lege input → `review faalde: geen output`. Een bevinding is een
    regel `- <path>:<line>: <tekst>` onder `### <Severity>`; `severity` in kleine letters; een
    bullet zonder `path:line:` geeft `path`/`line` `None`. De exitcode van roborev speelt geen rol.
  - `fixed_findings(root: Path) -> list[dict]` — per regel `roborev: <path>:<line> (<severity>)`
    in een commitbody van `<base>..HEAD`: `{"path", "line", "severity", "commit"}`.
  - `record(root, *, skip=None, sha=None, decisions=None, out=None) -> Path` — schrijft
    `<stem>.json` (of `out`): `{"range", "uitkomst": "skip", "reden"}` of
    `{"range", "uitkomst": "done", "sha", "bevindingen": [...]}`. Valideert: `uitkomst` ∈
    {fixed, refuted, left}; `fixed` ⇒ `commit` niet leeg en `reden` `None`; anders `reden` niet
    leeg en `commit` `None`; staat `<stem>.md` er, dan heeft elke bevinding van `classify` precies
    één besluit (gelijk `path`, `line`, `severity`). Fout ⇒ `ValueError`, CLI exit 2.
  - `body_line(root: Path) -> str` — `""` bij `off`; `<!-- roborev-lokaal: <sha> -->` bij done;
    `Roborev overgeslagen: <reden>` bij skip; `local` zonder `.json`:
    `Roborev overgeslagen: geen reviewuitkomst vastgelegd` (doel 3: nooit stil).
  - CLI: `classify [<pad>]` (default de `raw` van `gate`; schrijft bij skip ook `record`),
    `fixed`, `record --skip <reden>` | `record --done <sha> --decisions <file> [--out <pad>]`,
    `body-line`.

- [ ] **Step 0: Meet de echte lokale uitvoer, één keer** (kost één Codex-review; eerst
  `python3 .lighthouse/scripts/codex_usage.py budget`, alleen bij `codex`):

```bash
timeout 540 roborev ci review --agent codex --ref "$(git rev-parse HEAD~1)..HEAD" > <scratch>/local.out 2> <scratch>/local.err; echo "exit=$?"
```

  Buiten de sandbox, voorgrond, Bash-timeout 600000. Bewaar stdout als
  `tests/pack/fixtures/roborev/review-local.md` en noteer exitcode en stderr in de docstring van
  `classify`. Wijkt de vorm af van de CI-comment (geen kop op stdout, kop op stderr, eis van
  `--gh-repo`), pas dan de aanroep in Task 5 en de classificatie hier aan en meld het in de
  PR-body. Geen budget: sla de stap over en zet dat als open punt in de PR-body.

- [ ] **Step 1: Schrijf de falende tests**

```python
def test_classify_reads_the_measured_local_output():         # review-local.md -> verdict done of skip met de gemeten kop, nooit "review faalde: <eerste regel>"
    ...
def test_classify_reads_sha_and_findings_of_a_complete_review():
    got = classify(FIX("review-complete.md"))
    assert got["verdict"] == "done" and got["sha"] == "bf1eb24"
    assert [(f["path"], f["line"], f["severity"]) for f in got["findings"]] == [
        ("scripts/forge_drager.py", 93, "medium"), ("scripts/forge_agent.sh", 612, "medium")]
def test_classify_passed_is_done_without_findings(): ...
@pytest.mark.parametrize("kind", ["Review Failed", "Review Skipped"])
def test_classify_failed_heads_skip(kind): ...               # reason == f"review faalde: {kind}"
def test_classify_without_heading_names_the_first_line(): ... # "Error: codex: 401\n..." -> "review faalde: Error: codex: 401"
def test_classify_empty_output(): ...                          # "review faalde: geen output"
def test_resume_after_abort_routes_to_classify_and_recovers_fixes(repo):   # Review Focus 1
    # .md aanwezig, geen .json, twee commits met "roborev: scripts/forge_drager.py:93 (medium)" e.d.
    assert gate(repo, budget=_never)["verdict"] == "classify"   # _never faalt de test als hij draait
    assert [f["commit"] for f in fixed_findings(repo)] == [c1, c2]
def test_record_refuses_a_missing_decision(repo): ...          # 2 bevindingen, 1 besluit -> ValueError
def test_record_refuses_fixed_without_commit(repo): ...
def test_record_done_then_gate_says_done(repo): ...            # gate -> {"verdict":"done","sha":"bf1eb24"}
@pytest.mark.parametrize("state,line", [
    (None, ""),  # setting off
    ({"uitkomst": "done", "sha": "bf1eb24"}, "<!-- roborev-lokaal: bf1eb24 -->"),
    ({"uitkomst": "skip", "reden": "geen .roborev.toml"}, "Roborev overgeslagen: geen .roborev.toml"),
])
def test_body_line(repo, state, line): ...
```

- [ ] **Step 2: Draai, verwacht FAIL**
- [ ] **Step 3: Implementeer** de vier functies en CLI-subcommando's (Engelse namen:
  `outcome`, `reason`; de JSON-sleutels blijven Nederlands).
- [ ] **Step 4: Draai** `uv run pytest tests/pack/test_roborev_gate.py -q` — PASS.
- [ ] **Step 5: Dogfood en commit** —
  `feat(roborev): classificatie, besluiten en herstart van de lokale review (#119)`

---

### Task 4: `roborev_comment.py render`

**Files:**
- Create: `pack/core/scripts/roborev_comment.py`
- Modify: `internal/packs/core_test.go`, `pack/SOURCES.toml` (entry, `new = true`)
- Test: `tests/pack/test_roborev_comment.py`

**Interfaces:**
- Consumes: de `done`-vorm van `<stem>.json` uit Task 3.
- Produces:
  - `log_block(sha: str, source: str, pr: int, findings: list[dict]) -> str` — één regel
    `<!-- roborev-log: <json> -->`, sleutelvolgorde `v, sha, bron, pr, bevindingen`, `v` = 1,
    `separators=(",", ":")`, `ensure_ascii=False`, daarna de drie escapes.
  - `table(findings: list[dict]) -> str` — kolommen `Bevinding | Severity | Uitkomst`; cel
    `` `path:line` `` (of `—` zonder pad); uitkomst `fixed in <commit>`, `refuted: <reden>` of
    `left: <reden>`; `|` in een reden als `\|`. Zonder bevindingen één rij
    `| geen bevindingen | — | — |`.
  - `render(state: dict, raw: str, pr: int, source: str, limit: int = 65536) -> str` — marker,
    log-blok, lege regel, tabel, lege regel, `<details><summary>Ruwe roborev-output</summary>`,
    ruwe output, `</details>`. Te lang ⇒ alleen de ruwe output inkorten, met als laatste regel
    `… ingekort tot de limiet van GitHub`.
  - CLI `roborev_comment.py render --pr <nr> --bron lokaal|ci --state <pad> --raw <pad>` → stdout;
    exit 2 bij een state zonder `uitkomst: done`.

- [ ] **Step 1: Schrijf de falende tests**

```python
def test_log_block_round_trips_a_hostile_reason():                    # Review Focus 2
    reden = "sluit af --> <script>x</script>\nnieuwe regel"
    line = log_block("abc1234", "lokaal", 7, [_finding(uitkomst="refuted", reden=reden)])
    assert "\n" not in line and line.count("-->") == 1 and line.endswith(" -->")
    payload = re.fullmatch(r"<!-- roborev-log: (.*) -->", line).group(1)
    assert json.loads(payload)["bevindingen"][0]["reden"] == reden
def test_log_block_keeps_the_key_order_and_version(): ...  # json.loads(...) keys == ["v","sha","bron","pr","bevindingen"], v == 1
def test_render_starts_with_the_marker_and_the_log(): ...  # regel 1 "<!-- roborev-verwerkt: abc1234 -->", regel 2 begint "<!-- roborev-log: "
def test_render_without_findings_says_so(): ...            # "| geen bevindingen | — | — |" en "\"bevindingen\":[]"
def test_render_truncates_only_the_raw_output():           # Review Focus 3
    out = render(_state(30 findings), raw="x" * 200_000, pr=7, source="lokaal")
    assert len(out) <= 65536 and log_block(...) in out and table(...) in out
    assert "… ingekort tot de limiet van GitHub" in out
def test_table_escapes_a_pipe_in_a_reason(): ...
```

- [ ] **Step 2: Draai, verwacht FAIL**
- [ ] **Step 3: Implementeer**
- [ ] **Step 4: Draai** `uv run pytest tests/pack/test_roborev_comment.py -q`, `go test ./internal/packs/` — PASS.
- [ ] **Step 5: Dogfood en commit** — `feat(roborev): comment met log-blok en tabel (#119)`

---

### Task 5: Skills — write-pr stap V2b en V4, fix-pr sectie 6, run.md

**Files:**
- Modify: `pack/core/skills/write-pr/SKILL.md`
- Modify: `pack/core/skills/fix-pr/claude/SKILL.md`, `pack/core/skills/fix-pr/agents/SKILL.md`
- Modify: `internal/dispatch/instructions/run.md:48,53-55,65`
- Modify: `docs/site/data/config.json` — tabel `review`, sleutel `review.roborev`, `level` `set`,
  `required` `no`, default `off`, voorbeeld `local`, `about` (Engels) "Runs a local roborev review
  in lighthouse-write-pr before gh pr create: local or off.", readers `pack/core/skills/write-pr`
  en `pack/core/scripts/roborev_gate.py`
- Modify: `tests/test_site_config.py:25` — `review.roborev` in `SET`
- Modify: `pack/core/requires.toml` — `write-pr` in de lijst `superpowers`; `roborev_gate` in
  `git`, `python3` en `tomllib`; `roborev_comment` in `python3`
- Test: `tests/pack/test_write_pr_skill.py`, `tests/pack/test_fix_pr_skill.py`,
  `tests/test_site_config.py`, `tests/pack/test_skill_contracts.py`

**Interfaces:**
- Consumes: CLI's van Task 2–4, letterlijk.

- [ ] **Step 1: Schrijf de falende tests**

```python
# test_write_pr_skill.py
def test_roborev_step_sits_between_v2_and_the_verdicts():
    assert TEXT.index("## V2.") < TEXT.index("## V2b. Roborev review") < TEXT.index("## Verdicts")
def test_roborev_step_carries_the_contract():
    s = _section(TEXT, "## V2b.")
    for needle in ("roborev_gate.py gate", "git fetch origin", "timeout 540 roborev ci review --agent codex --ref",
                   "600000", "dangerouslyDisableSandbox: true", "superpowers:receiving-code-review",
                   "roborev: <path>:<line> (<severity>)", "roborev_gate.py fixed", "roborev_gate.py record",
                   "classify", "No issue"):
        assert needle in s
    assert "--comment" not in s and "run_in_background" not in s
def test_v4_writes_the_marker_and_posts_the_comment():
    v4 = _section(TEXT, "## V4.")
    for needle in ("roborev_gate.py body-line", "roborev_comment.py render", "--bron lokaal", "gh pr comment"):
        assert needle in v4
# test_fix_pr_skill.py
def test_roborev_section_renders_the_shared_log():
    for text in _variants():
        s6 = text[text.index("## 6."):text.index("## Non-negotiable")]
        assert "roborev_comment.py render" in s6 and "--bron ci" in s6 and "roborev_gate.py record" in s6
```

Plus in een Go-test van `internal/dispatch/instructions_test.go` (of de bestaande): `run.md` bevat
`/lighthouse-write-pr` en niet `/lighthouse-verify-pr`, en niet `roborev-verwerkt`.

- [ ] **Step 2: Draai, verwacht FAIL**

- [ ] **Step 3: Write-pr, sectie `## V2b. Roborev review (only with [review] roborev = "local")`**

Eén commando per stap, in deze volgorde; Engels; ook bij `No issue:` (zeg dat in de inleiding).

0. `git fetch origin <default>` en `python3 .lighthouse/scripts/roborev_gate.py gate`, buiten de
   sandbox. Per `verdict`: `off` → sectie overslaan; `skip`/`done` → door naar V4;
   `classify` → stap 2 zonder review; `run` → stap 1.
1. `timeout 540 roborev ci review --agent codex --ref <range> > <raw> 2>&1`, voorgrond,
   Bash-timeout 600000, buiten de sandbox. Exit 124 → `roborev_gate.py record --skip "review
   faalde: timeout na 9 min"`, door naar V4. Elke andere exitcode → stap 2 (de exitcode alleen is
   geen faalsignaal).
2. `roborev_gate.py classify`. `skip` → door naar V4 (het script legde de skip vast).
3. `done`: eerst `roborev_gate.py fixed` (besluiten van vóór een afbreek). Dan per overige
   bevinding `superpowers:receiving-code-review`, severity-regels van fix-pr sectie 6. Elke fix
   een eigen commit met `roborev: <path>:<line> (<severity>)` in de body. Schrijf de besluiten naar
   een JSON-bestand en `roborev_gate.py record --done <sha> --decisions <file>`. Geen tweede review.
   Staat de branch al op origin (`git ls-remote --exit-code origin <branch>`), push de
   fix-commits vóór V4, zodat CI één keer draait, op de verwerkte code.

De reden voor de voorgrond schrijf je als: start no background work (controller run, if present).

In V4 (geen regel met `## `): voeg de uitvoer van `roborev_gate.py body-line` als eigen regel toe
aan de body, buiten het Evidence-blok; na `gh pr create`:
`python3 .lighthouse/scripts/roborev_comment.py render --pr <nr> --bron lokaal --state <state> --raw <raw> > <file>`,
met `<state>` en `<raw>` letterlijk uit de uitvoer van `roborev_gate.py gate` (draai `gate` opnieuw
na een herstart; bouw nooit zelf een pad),
en `gh pr comment <nr> --repo "$R" --body-file <file>`, alleen bij een `done`-uitkomst. Faalt
`gh pr comment`, meld het; de bewaarde uitvoer maakt een tweede poging mogelijk.

- [ ] **Step 4: Fix-pr sectie 6, beide varianten gelijk**

Stap 4 bouwt de comment voortaan met `roborev_gate.py record --done <sha> --decisions <file> --out
<scratch>/roborev.json` en `roborev_comment.py render --pr <nr> --bron ci --state <scratch>/roborev.json
--raw <file met de CI-comment>`. De bestaande zin over marker en tabel wijst naar de renderer.
`test_variants_differ_only_in_the_sandbox_wording` blijft groen.

- [ ] **Step 5: run.md** — regels 48 en 65: `/lighthouse-verify-pr` → `/lighthouse-write-pr`;
  regel 53-55 (fix-pr op een groene PR met roborev-review) weg.

- [ ] **Step 6: Draai** `uv run pytest tests/pack/test_write_pr_skill.py tests/pack/test_fix_pr_skill.py tests/test_site_config.py tests/pack/test_skill_contracts.py tests/test_sources.py -q`
  en `go test ./internal/dispatch/ -run Instructions` — PASS.

- [ ] **Step 7: Dogfood en commit** — twee commits:
  `feat(write-pr): lokale roborev-review vóór gh pr create (#119)` (write-pr, site-rij) en
  `feat(fix-pr): roborev-comment via de gedeelde renderer (#119)` (fix-pr, run.md).

---

### Task 6: Dispatcher — fix-pr bij een rode PR

**Files:**
- Create: `internal/store/migrations/0004_runs_fixpr.sql` — herbouw `runs` naar het patroon van
  `0003_cleanup.sql`: zelfde kolommen, checks en indexen als `0002_runs.sql`, alleen de
  trigger-CHECK met `'fixpr'` erbij; `INSERT … SELECT` van alle rijen; daarna
  `runs_open_issue` en `runs_trigger` opnieuw
- Modify: `internal/runs/runs.go:45` (comment bij `Trigger`)
- Modify: `internal/dispatch/decide.go` (`Decide`, `decideIssue`, `settle`, `runCtx`, `prRed`,
  consts)
- Modify: `internal/dispatch/apply.go` (`start`)
- Modify: `pack/core/headings.toml` (`reason_pr_red_ceiling`)
- Modify: `internal/dispatch/testdata/decide/settle-red-pr.json`
- Create: `internal/dispatch/testdata/decide/fixpr-*.json` (zie Step 1)
- Test: `internal/dispatch/decide_test.go` (`TestDecideFixtures`, `TestFixPRRuns`),
  `internal/runs/runs_test.go` (`Take` met `Trigger: "fixpr"`), `internal/store/migrate_test.go`
  (rijen overleven 0004), `internal/dispatch/apply_test.go` (fix-pr-start niet geclaimd)

**Interfaces:**
- Produces:
  - consts `triggerFixPR = "fixpr"`, `cmdFixPR = "/lighthouse-fix-pr"`, `maxFixPRRuns = 3`.
  - `fixPRKey(pr int, sha string) string` → `fixpr:<pr>:<sha>`.
  - `fixPRRuns(history []runs.Row, issue, pr int) int` — rijen van `issue` met
    `Command == cmdFixPR+" "+pr` en `Outcome != runs.StartFailed`. Een open rij (Outcome `""`)
    telt mee: bij het afsluiten van de derde fix-pr-run is de teller al 3.
  - `runCtx.history []runs.Row` (gevuld in `newRunCtx`).
  - `fixPRStart struct { pr forge.PR; key string }` — wat `settle` doorgeeft.
  - `settle(...) (Action, bool, *fixPRStart)` (derde resultaat alleen uit `prRed`); `outcome()` en
    `prRed()` geven hem door. `prRed()`: `done && open rode PR` (eerste in `it.PRs`). Dan, in deze
    volgorde: `fixPRRuns ≥ maxFixPRRuns` → `returned("reason_pr_red_ceiling", "")`;
    `spentKey(history, key)` → `returned("reason_pr_red", "")`; anders `closed(runs.Handoff)` met
    `ReasonArg = key` en `SessionID = row.SessionID`, plus `&fixPRStart{pr, key}`.
  - `decideIssue`: na de bestaande poorten (`free`, `OPEN`) en alleen bij `statusController`: is er
    een `fixPRStart` en geen actieve blokkade (`c.active()`), dan
    `c.planned(c.fresh(triggerFixPR, key, cmdFixPR+" "+strconv.Itoa(pr.Number), pr.HeadRef), c.now)`
    en **geen** `c.next()` in deze tick. Anders gewoon `c.next()`.
  - `returnUnstartedFixPR(ps []planned) []planned` — in `Decide` na `Brake.apply` (en altijd, ook
    zonder rem): elke `KindClose` met `Outcome == runs.Handoff` en `ReasonArg` met prefix
    `fixpr:`, zonder `KindStart` met `Trigger == triggerFixPR` voor dezelfde `Repo`/`Issue`, wordt
    `KindReturn` met `Reason "reason_pr_red"`, `RunID` en `SessionID` behouden, `Outcome` en
    `ReasonArg` leeg. Daarna opnieuw sorteren.
  - `applier.start`: bij `a.Trigger == triggerFixPR && a.Attempt == 1` post elk pad dat vóór een geslaagde
    `runs.Take` eindigt (busy-defer, fout uit `Prepare` of `busy`, `ErrClaimed`) eerst
    `protocol.RenderReturned` met de tekst van `reason_pr_red`, zonder `RunID`; de defer of de fout
    blijft daarna gewoon in `Result`. Een post die faalt, wordt aan de fout toegevoegd.

- [ ] **Step 1: Schrijf de fixtures en tests (falend)** — kopieer de opbouw van
  `settle-red-pr.json` (issue 7, PR 12 op `abc123`, branch `issue-7-bouw-de-keten`, sessie
  `1111…`).

| Fixture | Input | `want` |
|---|---|---|
| `settle-red-pr.json` (aangepast) | open start-issue-run, klaar, PR rood | `close` run 5 `outcome: handoff`, `reason_arg: fixpr:12:abc123`, `session_id`; dan `start` trigger `fixpr`, key `fixpr:12:abc123`, command `/lighthouse-fix-pr 12`, branch `issue-7-bouw-de-keten` |
| `fixpr-spent-sha.json` | open fix-pr-run met key `fixpr:12:abc123`, klaar, PR nog rood op `abc123` | `return` `reason_pr_red` |
| `fixpr-ceiling.json` | twee gesloten fix-pr-rijen + open derde, klaar, PR rood op `def456` | `return` `reason_pr_red_ceiling` |
| `fixpr-ceiling-and-spent.json` | als hierboven, key `fixpr:12:def456` al besteed | `return` `reason_pr_red_ceiling` (plafond wint) |
| `fixpr-ceiling-start-failed.json` | als `fixpr-ceiling`, maar één gesloten rij `start_failed` | `close` handoff + `start` fixpr |
| `fixpr-on-hold.json` | als `settle-red-pr`, status `On hold` | `return` `reason_pr_red`, geen start |
| `fixpr-blocker.json` | als `settle-red-pr`, actieve blokkade | `return` `reason_pr_red`, geen start, geen park |
| `fixpr-brake-defers.json` | als `settle-red-pr`, rem met slots vol | `return` `reason_pr_red` + `defer` met de slots-reden |
| `fixpr-brake-paused.json` | als `settle-red-pr`, rem `Paused` | `return` `reason_pr_red` |

`TestFixPRRuns` (tabel: open rij telt, `start_failed` telt niet, andere PR telt niet).
In `apply_test.go`, met de fakes daar: `TestApplyFixPRBusyReturns` (werkplek bezet: één comment
met de kop van Teruggegeven en "PR rood", geen runs-rij, de defer in `Result.Deferred`) en
`TestApplyFixPRPrepareFailsReturns` (fout uit `Prepare`: dezelfde comment, en de fout in
`Result.Failed`). Een gewone handoff-start die bezet is, post niets (bestaand gedrag blijft), en
`TestApplyFixPRSecondAttemptBusyPostsNothing` (`Attempt: 2`, bezet: geen comment, alleen de defer;
`retry` plant die poging elke tick opnieuw).
`TestTakeFixPR` in `internal/runs/runs_test.go`: `Take` met `Trigger: "fixpr"` op een gemigreerde
database slaagt. In `migrate_test.go`: een rij uit vóór 0004 staat er na de migratie nog, met
dezelfde id en trigger.

- [ ] **Step 2: Draai, verwacht FAIL** —
  `go test ./internal/dispatch/ ./internal/runs/ ./internal/store/`
- [ ] **Step 3: Migratie** `0004_runs_fixpr.sql`; `go test ./internal/runs/ ./internal/store/` — PASS.
- [ ] **Step 4: Heading** — `[reason_pr_red_ceiling]` met nl/en uit *Vaste eisen* in
  `pack/core/headings.toml`, na `reason_pr_red`.
- [ ] **Step 5: Implementeer** de interfaces hierboven; werk de doc-comments van `Decide`,
  `decideIssue`, `settle` en `prRed` bij.
- [ ] **Step 6: Draai** `go test ./internal/...` en `golangci-lint run ./internal/...` — PASS,
  geen bevinding.
- [ ] **Step 7: Dogfood en commit** — twee commits:
  `feat(store): trigger fixpr in de runs-tabel (#119)` (migratie, runs) en
  `feat(dispatch): start fix-pr bij een rode PR, hooguit drie keer (#119)`

---

### Task 7: Docs, activering in lighthouse, changelog

**Files:**
- Modify: `docs/developer/planning/specs/2026-10-27-m2-dispatcher-design.md:59,434`
- Modify: `docs/developer/planning/specs/2026-11-01-issue-119-roborev-lokaal-design.md`
  (*Open na review*: r3-f1 en r3-f2 met verwijzing naar dit plan)
- Modify: `lighthouse.toml` (`[review] roborev = "local"`)
- Create: `.roborev.toml` (kopie van paia's: `agent = "codex"`, `model = "gpt-5.6-sol"`,
  `[ci] review_types = ["default"]`, kopcommentaar verwijst naar #119)
- Create: `changelog.d/issue-119-roborev-voor-pr.md` (`type: added`, Engels)

- [ ] **Step 1:** M2-spec: rij 59 verwijst naar #119; rij 434 wordt `PR rood en de sessie is klaar`
  → `handoff` en start `/lighthouse-fix-pr`; na 3 runs `returned` "PR rood na 3 fix-pr-runs".
- [ ] **Step 2:** `python3 .lighthouse/lib/lighthouse_lib/config.py review.roborev` print `local`.
- [ ] **Step 3:** `uv run pytest tests/pack/test_md_links.py -q` — PASS.
- [ ] **Step 4: Commit** — `docs(spec): fix-pr en roborev in de M2-spec, lighthouse op local (#119)`

## Na de taken

- Write-pr van deze PR is het bewijs van AC 6: de diff ligt ruim boven 100 coderegels, dus de
  nieuwe stap draait echt (Codex-budget nodig). Bewijs in de body: de comment met log-blok, en
  `gh run list --commit <sha> --workflow ci.yml` met precies één run.
- AC 4 is `Refs` naar het paia-follow-up-issue (§6); dat issue wordt pas na de merge aangemaakt.
- `measure_fix_pr_kpi.py` leest transcripts, geen triggers: geen wijziging nodig.
