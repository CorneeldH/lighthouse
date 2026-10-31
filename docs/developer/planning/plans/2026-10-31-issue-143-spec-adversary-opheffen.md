# `lighthouse-spec-adversary` opheffen — implementatieplan (#143)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** De agent `lighthouse-spec-adversary` verdwijnt; beide reviewskills doen de retry van een
dubbel gefaalde ronde 1 zelf, met Codex inline.

**Architecture:** De retry wordt dezelfde achtergrond-Bash-run van `review.sh` die ronde 1 al
gebruikt, gevolgd door de jq-tabel uit *Opus dispatch* van de eigen skill. Daarna verdwijnt het
agentbestand met al zijn verwijzingen (pakketmetadata, kaartdata, tests), en `init --upgrade`
ruimt de geïnstalleerde kopie op.

**Tech Stack:** Markdown-skills onder `pack/core/`, pytest (`tests/pack/`, `tests/test_site_*.py`),
Go-test `internal/packs/core_test.go`, `go run ./cmd/lighthouse init --upgrade`.

**Spec:** [2026-10-31-issue-143-spec-adversary-opheffen.md](../specs/2026-10-31-issue-143-spec-adversary-opheffen.md)

## Global Constraints

- Skilltekst, agenttekst en testcode in het Engels; commitberichten, issuecomment en
  changelog-fragment volgen de bestaande praktijk (commits Nederlands, fragmenten Engels).
- Geen wijziging aan `review.sh`, `verify.sh`, schema's, prompts of `plan_review_record.py`.
- Retrybeleid ongewijzigd: na een dubbel gefaald paar draait Codex één keer opnieuw; faalt dat
  ook, dan `--outcome aborted` met beide fouten.
- `REVIEW_SH`-paden blijven repo-relatief (`.claude/skills/lighthouse-{plan,spec}-adversarial-review/review.sh`):
  de allowlist in `.claude/settings.json:144,146` matcht op dat prefix.
- Historische documenten onder `docs/developer/planning/` blijven ongewijzigd.
- Na de laatste taak geeft `go run ./cmd/lighthouse init --upgrade` `No changes.`

## Review Focus

1. **Codex exit 6 in het paar en Opus faalt ook.** Verwacht: de retry draait wel, zoals nu (de
   regel "Both fail" retryt altijd); `review.sh` checkt het budget opnieuw. Geeft de retry zelf
   exit 6, dan geen tweede retry maar `--outcome aborted`. Getest in taak 1
   (`test_retry_skips_exit_6`).
2. **Retry schrijft naar `round-1.json`, niet naar `round-1.codex.json`.** Anders leest de
   jq-tabel een oud of ontbrekend bestand. Getest in taak 1 (`test_retry_is_inline_codex`).
3. **De retry leest de log niet.** Alleen de exitcode, zoals in ronde 1; de ruwe review blijft
   buiten de context. Getest in taak 1 (`test_retry_is_inline_codex`).
4. **Een skill noemt de agent nog.** Een verwijzing die blijft hangen stuurt de operator naar een
   agent die niet bestaat. Getest in taak 1 (`test_no_skill_names_the_adversary`).
5. **Kaart en pakket lopen uit de pas.** Een node met een `source` naar een verwijderd bestand.
   Gedekt door de bestaande `tests/test_site_chain.py::test_every_source_exists` en
   `tests/test_site_packs.py::test_skills_agents_workflows_match` (taak 2).

---

### Task 1: Retry inline in beide reviewskills

**Files:**
- Modify: `pack/core/skills/plan-adversarial-review/claude/SKILL.md:166-179` (step 5, de retry),
  `:226` ("like `LEVEL` above"), `:230` (ronde 1, Codex-kant), `:264` (faaltabel, rij "Both
  fail"), `:462` (*Opus dispatch*)
- Modify: `pack/core/skills/spec-adversarial-review/claude/SKILL.md:212-215` (step 2), `:258-265`
  ("Running by hand works too, without an agent"), `:275` ("plain step 5"), `:285` ("like
  `LEVEL` in the plain dispatch")
- Modify: `pack/core/skills/plan-adversarial-review/agents/SKILL.md:135`,
  `pack/core/skills/spec-adversarial-review/agents/SKILL.md:169`
- Test: `tests/pack/test_adversarial_review_skills.py`

**Interfaces:**
- Produces: in beide `claude`-skills een retry-paragraaf met het Codex-commando dat
  `<ATTEMPT>/round-1.json` schrijft, log naar `<ATTEMPT>/round-1.retry.log`, en de verwijzing naar
  de jq-tabel uit *Opus dispatch*. Taak 2 rekent erop dat na deze taak geen skill meer
  `spec-adversary` bevat.

- [ ] **Step 1: Schrijf de falende tests** in `tests/pack/test_adversarial_review_skills.py`:

```python
@pytest.mark.parametrize("name", NAMES)
@pytest.mark.parametrize("variant", VARIANTS)
def test_no_skill_names_the_adversary(name, variant):
    assert "spec-adversary" not in skill(name, variant)


@pytest.mark.parametrize("name", NAMES)
def test_retry_is_inline_codex(name):
    text = flat(skill(name, "claude"))
    assert f".claude/skills/lighthouse-{name}/review.sh" in text
    assert "1 <ATTEMPT>/round-1.json none" in text
    assert "> <ATTEMPT>/round-1.retry.log 2>&1; echo \"exit=$?\"" in text
    assert "never `round-1.retry.log`" in text


def test_retry_skips_exit_6():
    text = flat(skill("plan-adversarial-review", "claude"))
    assert "exit 6 is never retried" in text
```

Verwijder in hetzelfde bestand `ADVERSARY` (regel 30), `test_spec_adversary_knows_exit_6_without_retry`,
`_step2_jq` en `test_spec_adversary_does_not_collapse_minors`: ze testen een bestand dat taak 2
verwijdert. Het eerste vervangt `test_retry_skips_exit_6`; de MINOR-regel van het tweede geldt
al voor de jq-fragmenten in *Opus dispatch* (geen collapse, één rij per finding).

- [ ] **Step 2: Draai de tests en zie ze falen**

Run: `uv run pytest -q tests/pack/test_adversarial_review_skills.py`
Expected: FAIL op `test_no_skill_names_the_adversary` (4×), `test_retry_is_inline_codex` (2×),
`test_retry_skips_exit_6`.

- [ ] **Step 3: Herschrijf de retry in de planskill** (`claude`-variant).

Step 5, de bullets bij "Only the retry … dispatches the `lighthouse-spec-adversary` agent":
vervang door één alinea plus commando. De retry draait Codex zelf, als Bash-call met
`run_in_background: true` en `dangerouslyDisableSandbox: true`. Het commando staat op **één
regel**, zonder `\`-vervolg: `flat()` in de test laat een backslash staan, en dan matcht
`"1 <ATTEMPT>/round-1.json none"` niet.

```bash
.claude/skills/lighthouse-plan-adversarial-review/review.sh <plan> 1 <ATTEMPT>/round-1.json none [max] --context <spec path> [--context <spec path 2>] > <ATTEMPT>/round-1.retry.log 2>&1; echo "exit=$?"
```

Daarna de ID-tabel met de jq uit *Opus dispatch* op `round-1.json`, zonder banner. Lees alleen de
exitcode, never `round-1.retry.log`. Neem de exittabel uit `pack/core/agents/spec-adversary.md`
(exit 2–6, één regel per code) hier over, met letterlijke paden in plaats van `$OUT`:
`<ATTEMPT>/round-1.json.budget.json` (exit 6) en `<ATTEMPT>/round-1.json.events` (exit 5, de
`grep -o 'try again at …'`). Met de regel "exit 6 is never retried": die gaat over de exit van de
retry zelf. Het retrybeleid blijft: na "Both fail" draait de retry altijd één keer, ook als de
Codex-kant van het paar exit 6 gaf; faalt de retry (welke exit ook), dan `--outcome aborted` met
beide fouten, nooit een tweede retry. Behoud de `REVIEW_SH`-allowlistzin (exact repo-relatief
pad).

Regel 226: "like `LEVEL` above" wordt "like in the retry above" (de `LEVEL`-bullet verdwijnt).

Regel 230: "No `lighthouse-spec-adversary`: it returns a table" wordt "No ID table for this run:
…" — de reden (blind beslissen) en de verplichte redirect blijven.

Regel 264, faaltabel: "run round 1 once more as a plain step 5 (without a pair)" wordt "run the
retry from step 5 once". Regel 462 in *Opus dispatch*: "There is then no
`lighthouse-spec-adversary` that makes the `ID` column; compute it yourself" wordt "Compute the
`ID` column yourself".

- [ ] **Step 4: Herschrijf de retry in de spec-skill** (`claude`-variant).

Step 2 (regel 212-215): dezelfde alinea als in de planskill, met
`.claude/skills/lighthouse-spec-adversarial-review/review.sh <spec> 1 <ATTEMPT>/round-1.json none [max] > <ATTEMPT>/round-1.retry.log 2>&1; echo "exit=$?"`
(zonder `--context`, zoals de Codex-kant van ronde 1 hier), dan de jq uit de eigen *Opus
dispatch*, never `round-1.retry.log`. Voor de exitcodes: verwijs naar de exittabel in de
planskill, step 5. Regel 258-265 ("Running by hand works too, without an agent — then the whole
review does come into your context", met het commando eronder): schrappen; de retry in step 2
is nu het handmatige pad. Regel 275: "that is step 2 here: the `lighthouse-spec-adversary` without a
pair" wordt "that is the retry in step 2 here". Regel 285: "like `LEVEL` in the plain dispatch"
wordt "like in the retry in step 2".

- [ ] **Step 5: `agents`-varianten.** Schrap in beide de zin "The `lighthouse-spec-adversary`
  agent exists only in Claude Code (`.claude/agents/`)."; de rest van de alinea ("Here you call
  the runner yourself …") blijft, met de eerste letter hoofdletter.

- [ ] **Step 6: Draai de tests en zie ze slagen**

Run: `uv run pytest -q tests/pack/test_adversarial_review_skills.py`
Expected: PASS

Mutatiecheck: zet in de planskill tijdelijk `round-1.retry.log` terug naar `round-1.codex.log`
in het retrycommando; `test_retry_is_inline_codex[plan-adversarial-review]` moet falen. Draai
terug.

- [ ] **Step 7: Commit**

```bash
git add pack/core/skills/ tests/pack/test_adversarial_review_skills.py
git commit -m "refactor(review): retry van ronde 1 inline in de reviewskills (#143)"
```

### Task 2: Agent verwijderen, met metadata, kaart, tests en installatie

**Files:**
- Delete: `pack/core/agents/spec-adversary.md`
- Modify: `pack/core/requires.toml:6` (`"spec-adversary"` uit de `jq`-lijst),
  `pack/SOURCES.toml:219-222` (het `[[file]]`-blok van de agent)
- Modify: `docs/site/data/chain.json:249-267` (node `spec-adversary` weg), `:798` (uit
  `examples` van route `4c`; `worktree-linkcheck` blijft), `:811` (uit `chips` van route `4c`)
- Modify: `docs/site/data/packs.json:22` (uit `agents`)
- Modify: `tests/pack/test_issue_impact_scout.py` (regel 1 docstring, `ADVERSARY` regel 21, de
  sectie `# --- spec-adversary ---` regel 179-215, `(AGENT, ADVERSARY)` in
  `test_agents_are_english_and_carry_no_source_issue_numbers` wordt `(AGENT,)`)
- Modify: `internal/packs/core_test.go:56` (regel weg)
- Installed copies via `init --upgrade`: `.claude/agents/lighthouse-spec-adversary.md` (weg),
  `.lighthouse/manifest.toml`, `.claude/skills/lighthouse-{spec,plan}-adversarial-review/SKILL.md`,
  `.agents/skills/lighthouse-{spec,plan}-adversarial-review/SKILL.md`
- Create: `changelog.d/issue-143-spec-adversary.md`

**Interfaces:**
- Consumes: taak 1 — geen skill noemt `spec-adversary` meer.

- [ ] **Step 1: Pas de tests aan zodat ze de verwijdering eisen.** In `internal/packs/core_test.go`
  de regel `".claude/agents/lighthouse-spec-adversary.md",` weg; in
  `tests/pack/test_issue_impact_scout.py` de agentsectie en `ADVERSARY` weg (zie Files). Voeg toe:

```python
def test_only_the_scout_is_an_agent() -> None:
    assert sorted(p.name for p in (CORE / "agents").iterdir()) == ["issue-impact-scout.md"]
```

- [ ] **Step 2: Draai en zie falen**

Run, als twee losse commando's (niet met `&&`, anders draait de Go-test niet na de verwachte
pytest-fout): `uv run pytest -q tests/pack/test_issue_impact_scout.py`, dan
`go test ./internal/packs/...`
Expected: FAIL op `test_only_the_scout_is_an_agent`, en de Go-test faalt op de exacte
vergelijking van de bestandsunits (`core_test.go:130`): het pakket bevat nog
`.claude/agents/lighthouse-spec-adversary.md`.

- [ ] **Step 3: Verwijder** `pack/core/agents/spec-adversary.md` (`git rm`) en werk
  `requires.toml`, `SOURCES.toml`, `chain.json` en `packs.json` bij zoals onder Files.

- [ ] **Step 4: Draai de pakket- en sitetests**

Run: `uv run pytest -q tests/pack tests/test_site_chain.py tests/test_site_packs.py && go test ./internal/packs/...`
Expected: PASS

- [ ] **Step 5: Installeer de eigen kopie**

Run: `go run ./cmd/lighthouse init --upgrade --yes` (zonder `--yes` leest `init` EOF bij
`Apply these changes? [y/N]` als weigering en schrijft niets), dan
`go run ./cmd/lighthouse init --upgrade </dev/null`, zoals CI (`.github/workflows/ci.yml:98`).
Expected: de eerste run verwijdert `.claude/agents/lighthouse-spec-adversary.md` en werkt de
vier skillkopieën en `.lighthouse/manifest.toml` bij; de tweede geeft `No changes.`
Controle: `git grep -n 'spec-adversary' -- ':!docs/developer/planning' ':!changelog.d'` geeft
niets.

- [ ] **Step 6: Changelog-fragment** `changelog.d/issue-143-spec-adversary.md`:

```markdown
---
type: changed
---
The `lighthouse-spec-adversary` agent is gone: the review skills retry a failed round 1 with Codex themselves.
```

- [ ] **Step 7: Volledige testgate**

Run: het `[test_gate] command` uit `lighthouse.toml`.
Expected: groen.

- [ ] **Step 8: Commit**

```bash
git add -A pack/ docs/site/data/ tests/ internal/packs/core_test.go .claude/ .agents/ .lighthouse/manifest.toml changelog.d/
git commit -m "refactor(core): spec-adversary-agent opgeheven (#143)"
```

### Task 3: Meting als comment op het issue

**Files:** geen.

- [ ] **Step 1: Post de meting (AC 1) als comment op #143**, in het Nederlands, met
  `dangerouslyDisableSandbox: true`. Inhoud: de periode (2026-09-28 t/m 2026-10-31, bewaartermijn
  30 dagen), de bron (`~/.claude/projects/*/…jsonl`, unieke `tool_use` van `Agent`/`Task` met
  `subagent_type` die `adversary` bevat), de uitkomst (56 keer `spec-adversary` in paia, laatste op
  2026-10-16; 0 keer `lighthouse-spec-adversary` sinds `52c8723` van 2026-10-18), waarom
  `plan_review_record.py` het niet kan meten, en het besluit met een link naar de spec.

Expected: de comment-URL; zet die als bewijs in de PR-body (`/lighthouse-verify-pr`).
