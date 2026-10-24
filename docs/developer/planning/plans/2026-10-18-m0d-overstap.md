# M0 (d): personal_ai_agent stapt over op `lighthouse init` — implementatieplan (#9)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** paia haalt `pack/core`, `pack/python`, `pack/shell` en `pack/expo` via `lighthouse init`. De oude kopieën zijn weg, en de controller start een lopend issue met een oude `/paia-*`-naam onder de nieuwe naam.

**Architecture:** Vijf PR's in paia, elk met een eigen paia-issue (tabel *PR-indeling*): één voorbereiding en één per pakket. PR d0 kan nu al. Hij maakt de controller, check 6 en 13, de bumplane en twee paia-tests bestand tegen `lighthouse-*`-skills, en is een no-op zolang die er niet zijn. De pakket-PR's volgen één recept (*Overstaprecept*): de drift controleren, `lighthouse.toml` aanvullen, de oude kopieën weghalen en `lighthouse init` draaien in één commit, dan alles bijwerken wat tegelijk mee moet.

**Tech Stack:** paia: Python ≥ 3.11, pytest via `uv`, bash. Lighthouse: Go 1.26, de binary via `go install`.

**Spec:** [`2026-10-17-m0-pakket-design.md`](../specs/2026-10-17-m0-pakket-design.md), secties *Opdeling (d)*, *Versiemarker en manifest*, *Risico's*; besluiten 3, 6, 9, 12, 14, 15, 17, 20, 21, 23, 25–29. Plan (b): [`2026-10-18-m0b-pack-core.md`](2026-10-18-m0b-pack-core.md). Plan (c): [`2026-10-18-m0c-taalpakketten.md`](2026-10-18-m0c-taalpakketten.md). Wat daar nog verandert (c2, c3, PR 4, PR 5), staat in die plannen en hun PR-beschrijvingen. Dit plan verwijst ernaar en kopieert het niet. Invoer: *Inventaris (d)* hieronder, de inventarissen op #7 en #8, en de secties *Voor (d)* in de beschrijvingen van #14, #16 en #21.

**Paia:** `/home/corneel/repositories/personal_ai_agent`, gemeten op `origin/main` = `c2fd5bb54` (2026-10-24). Regelnummers horen bij die commit.

## Ontwerpkeuzes

Elk punt is een voorstel, tenzij er een akkoord bij staat.

1. **Herschrijven in de handoff-route, per worktree.** Een oude naam komt alleen binnen via een handoff-comment. De commando's die de controller zelf bouwt (`decide.py:1094`, `1893`, `1977`; `triage.py:777`, `811`), worden in de core-PR letterlijk `/lighthouse-*`. Daarom herschrijft `review_sprong` (`decide.py:113-127`) het commando als eerste stap. `_handoff_oordeel` (`decide.py:2003-2004`) geeft het resultaat al door aan `beoordeel_commando` en aan de launch (`_start`, `commando=` op 2113). Herschreven wordt alleen als `<handoff.worktree>/.claude/skills/lighthouse-<n>/SKILL.md` bestaat: daar start de sessie, en daar zoekt Claude Code de skill. `decide` is puur. Daarom meet de gather per worktree welke `lighthouse-*`-skills er staan, en die meting komt via de bundel in `Invoer`.
2. **Het beleid kent beide namen, via `chain_name`.** `beoordeel_commando` (`comments.py:192-223`), `_REVIEW_SPRONG`, `VOOR_UITVOERING` en de fasevergelijking met `EIND` in `overzicht.py` vergelijken voortaan de ketennaam zonder prefix (`start-issue`, `fix-pr`, …). In d0 blijven de literals `ALLOWLIST` en `EIND` `/paia-*`. De core-PR zet ze om, in dezelfde PR als de skills (ontwerpkeuze 5). Een letterlijke `/lighthouse-<n>` in een handoff, terwijl de skill niet in de worktree staat, gaat terug met de reden `skill not installed — rebase the worktree onto main`. Dat sluit het risico uit de spec: "laat een onbemande run stil de verkeerde of geen skill starten".
3. **Nieuwe code in het Engels, bestaande namen blijven.** De controller blijft tot M2 in paia (besluit 9) en gaat dan naar lighthouse. Wat d0 toevoegt, is brugcode voor die stap, en is Engels: de module `skill_names.py`, `chain_name`, `rewrite_legacy`, het veld `lighthouse_skills`, en de nieuwe testnamen. Bestaande paia-namen (`beoordeel_commando`, `review_sprong`, `Invoer`) hernoemen we niet; dat is herstructurering (besluit 14) en gebeurt in M2. De herschrijving kent alleen de prefixen `/paia-` en `/lighthouse-`, geen skillnamen. Na de core-PR staat er dus geen `paia-start-issue` meer in code. De aliassen gaan er in M2 uit (spec, *Risico's*).
4. **d0 slaat gemarkeerde skills over op elke plek die in paia blijft.** Dat zijn check 6 en 13, de bumplane, de prefixtest in `test_skill_mirror.py` en de domein- en metadatatests in `test_instructielagen.py`. Ze blijven na M0 in paia, en zonder `lighthouse-*`-skills verandert er niets aan hun uitkomst. Zo laat geen van hen de core-PR rood worden.
5. **Wat tegelijk moet, zit in één PR.** Check 8 en 11 verhuizen met de andere generieke checks (1–5, 7, 10) naar `.lighthouse/scripts/check-instructions.sh`, dat de marker al overslaat en submappen ziet (3C). De core-PR haalt ze in dezelfde PR uit paia's script, zodat d0 ze niet hoeft aan te passen. Paia houdt check 6, 9, 12 en 13 (beschrijving #16). Zo ook: `ALLOWLIST` en `EIND` naar `/lighthouse-*`, de skills erin, de oude kopieën eruit, en de aanroepers van hernoemde scripts en subcommando's.
6. **"Buiten de historie" voor de grep.** Uitgezonderd zijn bestanden die vastleggen wat was: `docs/developer/planning/**`, `CHANGELOG.md`, `docs/developer/reflect/`, `docs/developer/research/`, de gegenereerde codebase-kaart (`docs/developer/codebase/`, `.planning/`) en `experiments/`. `vendor/` telt wel mee: de treffer daar (`vendor/addyosmani-agent-skills/PROVENANCE.md:79`) is paia's eigen tekst, geen upstream-kopie, en gaat mee in de python-PR. Op `c2fd5bb54` raakt de grep 357 bestanden, waarvan 176 in `docs/developer/planning`. Het commando staat in *Global Constraints*; de eerste run in de core-PR toetst de grens.
7. **Drift lossen we op wanneer hij er is.** Elke pakket-PR draait eerst het drift-commando (R1). Geeft het uitvoer, dan gaat die wijziging in een kleine lighthouse-PR naar `pack/`, met `source_sha` op de overgezette paia-commit. Op 2026-10-24 waren het er twee: `d11f5b5e9` (actie-pin in `unblock-dependents.yml`) en `db605b7a7` (`upgrading-expo` → `expo-upgrade` in de expo-skill).
8. **Tag `v0.1.0`** (*akkoord Corneel, 2026-10-24*), gezet door Corneel na (b) PR 5. Zijn c2 en c3 dan ook gemerged, dan bedient één tag alle vier de pakket-PR's. Paia installeert met `GOPRIVATE=github.com/CorneeldH/* go install github.com/CorneeldH/lighthouse/cmd/lighthouse@v0.1.0` (besluit 3).
9. **Reviewrecords naar de nieuwe datamap** (*akkoord Corneel, 2026-10-24*). Nu staan ze in `~/.local/share/lekmeting-keten/{plan-review,spec-review}/` (`lk_common.data_dir()`, met de override `LEKMETING_DATA`). Na de core-PR schrijft `.lighthouse/scripts/plan_review_record.py` naar `~/.local/share/lighthouse/review-records/` (besluit 27). `review_pair.py tally` blijft in paia (#14) en leest nu de oude map. Zonder ingreep loopt hij stil achter. Daarom geen `LIGHTHOUSE_DATA`-override, want die moet elk harnas en elke controller-unit dragen, en wie hem mist, splitst de records stil. In plaats daarvan leest `tally` via `lighthouse_lib.datadir.data_dir()`, en verhuizen `plan-review/` en `spec-review/` één keer met `mv`. De andere lekmeting-data blijft staan, voor `experiments/lekmeting-keten/`.
10. **Eén paia-issue per PR** (*Corneel, 2026-10-24*). Een issue krijgt twee PR's als een deel pas kan als de code op `main` staat. Bij de core-PR is dat het geval: de verificatie na de merge (taak K5) kan pas op `main`.

## Global Constraints

- Paden, letterlijk uit de spec: skills `.claude/skills/lighthouse-<naam>/` en `.agents/skills/lighthouse-<naam>/`; agents `.claude/agents/lighthouse-<naam>.md`; hooks `.claude/hooks/lighthouse/<naam>.sh`; Python `.lighthouse/lib/lighthouse_lib/`; scripts `.lighthouse/scripts/`; workflows `.github/workflows/lighthouse-<naam>.yml`; manifest `.lighthouse/manifest.toml`.
- Marker (`internal/packs/marker.go`): in een `SKILL.md` de regel `  lighthouse: "<pakket>@<versie>"` onder `metadata:`. Paia leest hem met de bestaande `skill_meta <dir> lighthouse` (`check-instructies.sh:126-137`) en in Python met `_lees_metadata(...)` (`skills_upstream_bump.py:120`). Een skillmap is een lighthouse-map als zijn `SKILL.md` die sleutel draagt, met een niet-lege waarde.
- Ketenskills voor de grep (besluit 6): `start-issue`, `route-issue`, `handoff`, `verify-pr`, `fix-pr`, `close-issue`, `spec-adversarial-review`, `plan-adversarial-review`, `write-issue`, `worktree-audit`, `instrumenteren`; per taal-PR ook `python-schrijven`, `shell-gates` en `refresh-expo-lockfiles`. Blijven in paia en tellen niet mee: `paia-controller`, `paia-pauzeer`, `paia-backlog-stand`, `paia-board-sweep` en de rest uit de spec, *Inventaris*.
- Grep, nul treffers op de kop van elke pakket-PR (`<namen>` = de ketenskills tot en met die PR):
  ```bash
  git grep -nE 'paia-(<namen>)\b' -- . ':!docs/developer/planning' ':!CHANGELOG.md' \
    ':!docs/developer/reflect' ':!docs/developer/research' ':!docs/developer/codebase' \
    ':!.planning' ':!experiments'
  ```
- Drift (besluit 12), vanuit lighthouse; verwacht: geen uitvoer:
  ```bash
  python3 - <<'EOF'
  import subprocess, tomllib
  P = "/home/corneel/repositories/personal_ai_agent"
  for f in tomllib.load(open("pack/SOURCES.toml", "rb"))["file"]:
      if f.get("new"):
          continue
      for p in [f["source_path"]] if isinstance(f["source_path"], str) else f["source_path"]:
          log = subprocess.run(["git", "-C", P, "log", "--oneline", f'{f["source_sha"]}..origin/main', "--", p],
                               capture_output=True, text=True, check=True).stdout.strip()
          if log:
              print(f["path"], "<-", p, "\n ", log.replace("\n", "\n  "))
  EOF
  ```
- Testcommando van paia, gelijk aan `.claude/hooks/pre-commit-tests.sh:409-410`: `uv run pytest tests/unit/ -m 'not hardware_audio and not slow' --tb=short -q -n auto --maxprocesses 4`. De volle suite draait vóór elke push; de commit-gate draait hem ook.
- Paia's eigen regels gelden: `AGENTS.md`, `CLAUDE.md`, worktree vóór de eerste schrijfactie, Conventional Commits, artefacten in het Nederlands. `AGENTS.md` + `CLAUDE.md` eindigen netto op ≤ 3000 woorden (nu 2561 + 435 = 2996).
- Nieuwe code in paia: namen, meldingen en tests in het Engels (ontwerpkeuze 3). Een melding die in een comment op GitHub landt, volgt het protocol van (b) (`headings.toml`, `[project] language`).
- Nooit herschrijven: oude comments op GitHub (spec, *Risico's*).

## Review Focus

1. **Een handoff uit een worktree die vóór de core-PR is gemaakt.** Na de merge heeft `main` de `lighthouse-*`-skills, maar een oude featureworktree nog de `paia-*`-kopieën. Verwacht: `/paia-fix-pr 7` in die worktree wordt niet herschreven. Na de core-PR staat de oude naam niet meer in `ALLOWLIST`, en de handoff gaat terug met de reden om de worktree te rebasen. Na de rebase start hij onder de nieuwe naam. Uitzondering: `/paia-close-issue` uit zo'n worktree blijft een eindhandoff. De nacontrole start `EIND` zelf, in de hoofdcheckout of de forge-workspace, nooit in die worktree. Test: `test_skill_in_other_worktree_does_not_count` in taak D2, en in de core-PR `test_legacy_name_without_skill_is_returned`.
2. **Een letterlijke `/lighthouse-<n>` zonder skill in de worktree.** Verwacht: Teruggegeven met `skill not installed`, geen start. Test: `test_new_name_without_skill_is_returned` in taak D2.
3. **Een uitgezette review met de nieuwe naam.** `spec_review = false` en een handoff `/lighthouse-spec-adversarial-review <spec>`. Verwacht: dezelfde sprong naar `/superpowers:writing-plans` als met de oude naam. Test: `test_new_review_name_also_skips` in taak D2.
4. **Een marker op de verkeerde plek.** `lighthouse:` top-level in de frontmatter in plaats van onder `metadata:`. Verwacht: dat telt niet als marker, en check 6 en 13 toetsen gewoon. Test: `test_marker_outside_metadata_does_not_count` in taak D3.
5. **Een forge-workspace als worktree.** `controller_gather.py:779-781` voegt forge-paden toe aan `worktrees`. Verwacht: de skillmeting kijkt daar ook, en een pad dat lokaal niet bestaat, geeft een lege set zonder fout. Test: `test_lighthouse_skills_missing_path` in taak D1.

---

## Inventaris (d)

Gemeten op paia `c2fd5bb54`. De bouwer toetst de regelnummers bij de uitvoering.

### Controller

| Plek | Wat | Wanneer |
|---|---|---|
| `src/controller/comments.py:32-40` | `ALLOWLIST` (`/paia-start-issue`, `-spec-adversarial-review`, `-plan-adversarial-review`, `-fix-pr`), `EIND = "/paia-close-issue"` | d0: vergelijken op `chain_name`; core: literals naar `/lighthouse-*` |
| `comments.py:192-223` | `beoordeel_commando`, met `== "/paia-start-issue"` (211) en `== "/paia-fix-pr"` (213) | d0 |
| `src/controller/decide.py:88-92` | `VOOR_UITVOERING` | d0: `chain_name` |
| `decide.py:101-127` | `_REVIEW_SPRONG`, `review_sprong` | d0: herschrijven, sleutel zonder prefix |
| `decide.py:2000-2033` | `_handoff_oordeel`: `review_sprong` → `beoordeel_commando` → `VOOR_UITVOERING` (2029) → `_start` (2113) | d0 |
| `decide.py:979` | `_oordeel`: `beoordeel_commando` zonder `review_sprong` | d0, via ontwerpkeuze 2 |
| `decide.py:989` | `_fix_pr_sinds_klaar` vergelijkt `Gestart.commando` met `/paia-fix-pr <n>`: beleid, geen zelfgebouwd commando | d0: `_is_fix_pr` op `chain_name` (paia#1586) |
| `decide.py:1094`, `1893`, `1977`; `triage.py:777`, `811`; `overzicht.py:65` | commando's die de controller zelf bouwt | core |
| `src/controller/overzicht.py:24`, `100`, `297`, `357` | `fase == EIND` | d0: `chain_name` |
| `decide.py:267-306` | `Invoer` | d0: veld `lighthouse_skills` |
| `scripts/controller_gather.py:495-512`, `778-801` | `worktrees()`, de bundel | d0: meting `lighthouse_skills` |
| `scripts/controller_state.py:154-170` | `Invoer` uit de bundel | d0 |
| `tests/helpers/controller.py:194-246` | `invoer()`-helper | d0: parameter `lighthouse_skills` |
| `scripts/controller_launch.sh:133`, `scripts/forge_agent.sh:152-279`, `scripts/controller_vraag.py:42` | `PAIA_CONTROLLER_RUN` | core: `LIGHTHOUSE_CONTROLLER_RUN` ernaast (#7, *Na de laatste taak*) |
| `scripts/review_pair.py:615`, `scripts/plan_review_record.py:124-131` | `tally` leest `lk_common.data_dir()` | core: `lighthouse_lib.datadir` (ontwerpkeuze 9) |

### Checks, bumplane en tests

| Plek | Wat | Wanneer |
|---|---|---|
| `scripts/hooks/check-instructies.sh:523-550` | check 6: eist `paia-` of `origin: external` | d0 (D3) |
| `check-instructies.sh:943-1000` | check 13: eist `metadata.domain` ∈ {applicatie, inhoud, factory, infra}, voor beide wortels | d0 (D3) |
| `check-instructies.sh:800-888` | check 9: kijkt alleen naar `origin: external|extended`; een pakketskill draagt geen `origin` | geen wijziging |
| `check-instructies.sh:168-520`, `552-790`, `890-941` | de generieke checks 1–5, 7, 8 (regex op 678 ziet geen submap), 10 en 11 | core: weg uit paia (ontwerpkeuze 5) |
| `scripts/skills_upstream_bump.py:209-224` | `lees_eenheden`: leest `origin` uit `_lees_metadata` (219) | d0 (D4): marker eerst overslaan |
| `tests/unit/test_skill_mirror.py:86`, `113-135` | `NAME_PREFIX = "paia-"`, prefixtest | d0 (D5) |
| `tests/unit/test_instructielagen.py:803-819`, `915-946` | `DOMEINEN`, `test_skill_draagt_een_domein`, `test_expected_metadata_table_matches_reality` | d0 (D5) |
| `scripts/hooks/pre-push-instructielagen.sh:17`, `skills_upstream_bump.py:1147` | draaien `check-instructies.sh` | core: ook `.lighthouse/scripts/check-instructions.sh` |

### Wat de PR's van (b) en (c) voor (d) noemen

Uit de plannen en de beschrijvingen van #14, #16, #18 en #21. Wat c2, c3, PR 4 en PR 5 er nog aan toevoegen, staat in hun eigen beschrijving onder *Voor (d)*. De bouwer leest die bij de start van de pakket-PR.

- **Controllerbeleid** importeert `lighthouse_lib.protocol` uit `.lighthouse/lib/`: `ALLOWLIST`, `EIND`, `BRAINSTORM`, `INTERACTIEF`, `NATIVE`, `NATIVE_MODEL`, `beoordeel_commando`, `is_antwoord`, `antwoord_prompt`, de proza van `render_klaar` (`render_done(note=…)`) en de lane-validatie. `src/controller/artefacten.py` en het protocoldeel van `comments.py` gaan weg. De controller leest de Engelse vraagvelden uit `parse_question` (#14).
- **`triage.py`** stapt over op de marker uit `protocol` en schrijft de triage-marker met de nieuwe tokens (plan (b), *Protocoltokens*).
- **`review_pair.py tally`** blijft in paia (#14), en leest de records via `lighthouse_lib.datadir` (ontwerpkeuze 9).
- **`codex_usage.py`**: `gate`, `_parkeer` en `schrijf_marker` blijven in paia (besluit 26). `usage_window.py:742-757` laadt `budget`/`read` uit `.lighthouse/scripts/codex_usage.py` en leest de Engelse sleutels (#16). Andere aanroeper: `.github/workflows/roborev.yml:106`.
- **Hernoemde CLI**: `accepteer-open` → `accept-open`, `pr-open-punten` → `pr-open-points`, `--heropen` → `--reopen`, `--voor-pr` → `--before-pr` (plan (b), ontwerpkeuze 3).
- **`lighthouse.toml` van paia**: `[project] language = "nl"`; `[test_gate]` met het testcommando hierboven plus `--basetemp="$LIGHTHOUSE_GATE_TMP"`, en `unchecked_exit_codes = [3, 4, 5]`; `[labels]` met paia's namen (`3-blocked-issue` …, `location_prefix = "1-loc:"`, `area_prefix = "2-area:"`); `[paths] tenets`, `areas`, `history`, `labels` (de taxonomie uit de beschrijving van PR 4, besluit 28), `mirror_exceptions = "scripts/hooks/spiegel-uitzonderingen.txt"`; `[worktree_audit] protected_refs = ["entire/*"]`, `protected_paths = [".port-registry"]`; `[ci] runs_on` en de bot-sleutels; `[project] skill_prefix = "paia-"`. `host.toml` op de Beelink: `gate_slice = "gates.slice"` onder `[host]` (besluit 20).
- **Python** (plan (c), *Na de laatste taak*): `[shell] strict_dirs = ["scripts/hooks", "tests/integration", ".claude/hooks"]`; `[python] coverage_command` met paia's markers; `nightly_workflow = "nightly-tests.yml"`; `coverage_issue_labels = ["1-loc:container", "2-area:meta"]`; `nightly-tests.yml` roept `.lighthouse/scripts/quality/ratchet.py nightly` aan met `LIGHTHOUSE_BOARD_TOKEN` en `LIGHTHOUSE_HANDOFF_WORKTREE`; `quality-baseline-ratchet.yml` roept `tighten` aan op het nieuwe pad. Importeurs van `quality.*`: `scripts/deep_sweep.py:55-60`, `1565`, `1711`, `1720`; `scripts/opencode-lane/lane/gates.py:17`; `scripts/hooks/pre-push-quality.sh:127`. Ook `vendor/addyosmani-agent-skills/PROVENANCE.md:79`. Wat in paia blijft: plan (c), ontwerpkeuze 14.
- **Shell**: `workflow-audit.sh` blijft in paia (besluit 17), met zijn eigen shellcheck-pin. `AGENTS.md` *Buiten Claude Code* wijst naar `.lighthouse/scripts/shellcheck-repo.sh`.
- **Expo**: alleen `npm`, geen workspaces (#21).

---

## PR-indeling

| PR | Repo, branch | Taken | Heeft nodig |
|---|---|---|---|
| plan | lighthouse, `issue-9-plan` | dit plan | — |
| d0 voorbereiding | paia, `issue-<nr>-lighthouse-voorbereiding` | D1–D5 | het plan |
| core | paia, `issue-<nr>-lighthouse-core` | K1–K4 | d0; (b) PR 4 en PR 5 gemerged; tag `v0.1.0` |
| core, na de merge | paia, zelfde issue, tweede PR als er iets te repareren valt | K5 | core gemerged |
| expo | paia, `issue-<nr>-lighthouse-expo` | T1 voor `expo` | core; c4 (#21, gemerged); een tag met c4 |
| python | paia, `issue-<nr>-lighthouse-python` | T1 voor `python` | core; c2; een tag met c2 |
| shell | paia, `issue-<nr>-lighthouse-shell` | T1 voor `shell` | core; c3; een tag met c3 |

- **Elke pakket-PR** draait eerst R1. Een drift-fix in lighthouse is dan een kleine PR met een nieuwe patch-tag (ontwerpkeuze 7).
- **Volgorde van de taal-PR's.** Elk na de core-PR, onderling vrij. Expo kan als eerste, want c4 staat al op `main`.
- **Gedeelde bestanden in paia.** `lighthouse.toml`, `.lighthouse/manifest.toml`, `.claude/settings.json`, `AGENTS.md` en `scripts/hooks/spiegel-uitzonderingen.txt`. Twee pakket-PR's tegelijk betekent rebasen en `lighthouse init --upgrade` opnieuw draaien. Voorstel: één tegelijk.

---

## PR d0 — voorbereiding (paia)

Werkwijze: paia's `AGENTS.md`, een worktree, TDD. Elke taak eindigt met het testcommando uit *Global Constraints*.

### Taak D1: Welke `lighthouse-*`-skills elke worktree heeft

**Files:**
- Create: `src/controller/skill_names.py`, `tests/unit/test_controller_skill_names.py`
- Modify: `src/controller/decide.py` (`Invoer`), `scripts/controller_gather.py`, `scripts/controller_state.py`, `tests/helpers/controller.py`, `tests/unit/test_controller_gather.py`, `tests/unit/test_controller_state_cli.py`

**Interfaces:**
- Produces in `skill_names.py`:
  - `PREFIXES = ("/paia-", "/lighthouse-")`.
  - `chain_name(command: str) -> str | None`: van het eerste woord de naam na een prefix uit `PREFIXES`, anders `None`. `"/paia-fix-pr 7"` → `"fix-pr"`; `"/superpowers:writing-plans x"` → `None`; `""` → `None`; `"/paia-"` → `None`.
  - `rewrite_legacy(command: str, installed: frozenset[str]) -> str`: begint het eerste woord met `/paia-` en staat zijn ketennaam in `installed`, dan wordt dat woord `/lighthouse-<naam>`; de rest blijft byte voor byte gelijk. Anders ongewijzigd.
- `Invoer.lighthouse_skills: dict[str, frozenset[str]] = field(default_factory=dict)`: worktreepad → de namen `<n>` waarvoor `<pad>/.claude/skills/lighthouse-<n>/SKILL.md` een bestand is.
- `controller_gather.lighthouse_skills(paths: Iterable[str]) -> dict[str, list[str]]`: gesorteerde namen per pad. Een pad zonder zulke skills, of een pad dat niet bestaat, staat er niet in. De bundel krijgt de sleutel `"lighthouse_skills"` over `[hoofd, *paden]`, na het samenvoegen met de forge-paden (779-781).
- `controller_state` zet de lijsten om naar `frozenset`. Een bundel zonder de sleutel geeft `{}`.
- `tests/helpers/controller.invoer(..., lighthouse_skills: dict[str, set[str]] | None = None)`.

- [ ] **Stap 1: Schrijf de falende tests.**
  - `test_chain_name` (geparametriseerd over de voorbeelden hierboven, plus `/lighthouse-start-issue 12` → `start-issue`).
  - `test_rewrite_only_when_installed`: `rewrite_legacy("/paia-start-issue 12", frozenset({"start-issue"})) == "/lighthouse-start-issue 12"`; met `frozenset()` ongewijzigd; `/paia-fix-pr 7` met alleen `start-issue` ongewijzigd.
  - `test_rewrite_keeps_the_rest`: `"/paia-spec-adversarial-review /a/b.md  extra"` → alleen het eerste woord anders.
  - `test_rewrite_leaves_other_commands`: `/superpowers:writing-plans x` en `/lighthouse-fix-pr 7` ongewijzigd.
  - `test_lighthouse_skills_per_worktree` (`test_controller_gather.py`): `tmp_path/a` met `.claude/skills/lighthouse-start-issue/SKILL.md` en een map `lighthouse-fix-pr/` zonder `SKILL.md`, `tmp_path/b` zonder skills → `{str(a): ["start-issue"]}`.
  - `test_lighthouse_skills_missing_path`: een pad dat niet bestaat → geen sleutel, geen fout.
  - `test_bundle_lighthouse_skills_to_invoer` en `test_bundle_without_lighthouse_skills` (`test_controller_state_cli.py`).
- [ ] **Stap 2: Draai** `uv run pytest tests/unit/test_controller_skill_names.py tests/unit/test_controller_gather.py tests/unit/test_controller_state_cli.py -q` — verwacht: FAIL.
- [ ] **Stap 3: Implementeer.**
- [ ] **Stap 4: Draai** het testcommando — verwacht: PASS.
- [ ] **Stap 5: Commit** `feat(controller): meet per worktree welke lighthouse-skills er staan`

### Taak D2: Oude naam herschrijven vóór de launch, beide namen in het beleid

**Files:**
- Modify: `src/controller/decide.py` (`VOOR_UITVOERING`, `_REVIEW_SPRONG`, `review_sprong`, `_handoff_oordeel`), `src/controller/comments.py` (`beoordeel_commando`), `src/controller/overzicht.py` (`fase == EIND` op 100, 297, 357)
- Test: `tests/unit/test_controller_decide_starten.py` (nieuwe klasse `TestLegacySkillNames`, naast `TestReviewOverslaan`), de test van `beoordeel_commando` en van `overzicht`

**Interfaces:**
- Consumes: `chain_name`, `rewrite_legacy`, `Invoer.lighthouse_skills` (taak D1).
- Produces:
  - `review_sprong(volgende: str, inv: Invoer, installed: frozenset[str] = frozenset()) -> tuple[str, str]`: eerst `rewrite_legacy(volgende, installed)`, dan de sprong. `_REVIEW_SPRONG` sleutelt op `"spec-adversarial-review"` en `"plan-adversarial-review"`.
  - `_handoff_oordeel` geeft `ctx.inv.lighthouse_skills.get(handoff.worktree, frozenset())` mee. Begint het commando daarna met `/lighthouse-` en staat zijn ketennaam niet in die set, dan volgt vóór `beoordeel_commando`: `_terug(issue, "skill not installed — rebase the worktree onto main", detail=f"{command} is not in {handoff.worktree}/.claude/skills/", react=c.id)`.
  - `beoordeel_commando` herkent een commando als het in `ALLOWLIST` staat, óf als zijn `chain_name` gelijk is aan die van een element van `ALLOWLIST`. Hetzelfde geldt voor `EIND`. De argumentregels van 211 en 213 kijken naar `chain_name` (`"start-issue"`, `"fix-pr"`).
  - `_before_execution(command: str) -> bool` in `decide.py`: waar als het eerste woord in `VOOR_UITVOERING` staat, of als zijn `chain_name` die van een element ervan is. Vervangt de test op 2029.
  - `overzicht.py` vergelijkt `chain_name(fase) == chain_name(EIND)`.

- [ ] **Stap 1: Schrijf de falende tests.** Met de helpers `issue`, `comment`, `handoff`, `invoer`, `beslis_`, `van_soort`:
  - `test_legacy_name_stays_without_lighthouse_skill`: `handoff(volgende="/paia-start-issue 12")`, geen `lighthouse_skills` → `start["commando"] == "/paia-start-issue 12"`.
  - `test_legacy_name_becomes_new_with_lighthouse_skill`: `lighthouse_skills={WT: {"start-issue"}}` → `start["commando"] == "/lighthouse-start-issue 12"` en `start["prompt"].startswith("/lighthouse-start-issue 12 ")`.
  - `test_skill_in_other_worktree_does_not_count`: `lighthouse_skills={HOOFD: {"start-issue"}}`, handoff in `WT` → ongewijzigd.
  - `test_rewritten_review_skips_when_review_off`: `/paia-spec-adversarial-review {SPEC}`, `{WT: {"spec-adversarial-review"}}`, `spec_review=False` → `/superpowers:writing-plans {SPEC}`, met `sprong:` in het bericht.
  - `test_new_review_name_also_skips`: `/lighthouse-plan-adversarial-review {PLAN}`, geïnstalleerd, `plan_review=False` → `SDD`.
  - `test_new_review_name_is_before_execution`: `/lighthouse-plan-adversarial-review {PLAN}`, geïnstalleerd, een open `Blokkeerder` → er komt een start (tegenhanger van `test_plan_review_aan_start_wel_bij_open_blokkeerder`).
  - `test_new_name_without_skill_is_returned`: `/lighthouse-fix-pr 7`, geen skill → geen start, één Teruggegeven met `skill not installed`.
  - `test_judge_accepts_both_prefixes`: `("/lighthouse-start-issue 12", 12)` → `start`; `("/lighthouse-start-issue 13", 12)` → `afgewezen`; `("/lighthouse-fix-pr 7", 12)` → `start`; `("/lighthouse-close-issue 12", 12)` → `eind`; `("/lighthouse-route-issue 12", 12)` → `afgewezen`.
  - `test_close_issue_phase_both_names` (overzicht): een issue met een gemergde PR en fase `/lighthouse-close-issue` valt in dezelfde groep als met `/paia-close-issue`.
  - Bestaande tests (`TestReviewOverslaan` en de rest) blijven ongewijzigd groen.
- [ ] **Stap 2: Draai** `uv run pytest tests/unit/ -k "controller" -q` — verwacht: FAIL op de nieuwe tests.
- [ ] **Stap 3: Implementeer.**
- [ ] **Stap 4: Draai** het testcommando — verwacht: PASS.
- [ ] **Stap 5: Commit** `feat(controller): herschrijf een oude /paia-naam naar lighthouse als de skill er staat`

### Taak D3: Check 6 en 13 slaan lighthouse-skills over

**Files:**
- Modify: `scripts/hooks/check-instructies.sh`
- Test: `tests/unit/test_check_instructies.py`

**Interfaces:**
- Produces: `is_lighthouse_skill() { [ -n "$(skill_meta "$1" lighthouse)" ]; }`, met als argument een skillmap, naast `skill_meta`. Gebruik:
  - check 6 (`check_naamcontract`): een gemarkeerde map in `.claude/skills` slaat de check over, vóór de toets op `origin`;
  - check 13 (`check_domein`): een gemarkeerde map slaat de check over, per wortel;
  - de kop van het script noemt de regel in één zin: lighthouse-mappen zijn van lighthouse en worden daar getoetst.

- [ ] **Stap 1: Schrijf de falende tests**, met de fixturestijl van het bestand:
  - `test_check_6_skips_lighthouse_skill`: `.claude/skills/lighthouse-x` met marker, zonder `origin` → geen `FOUT  naamcontract`.
  - `test_check_13_skips_lighthouse_skill`: gemarkeerd, zonder `domain` → geen `FOUT  domein`.
  - `test_marker_outside_metadata_does_not_count`: `lighthouse: "core@v0.1.0"` top-level → check 6 en 13 toetsen gewoon.
- [ ] **Stap 2: Draai** `uv run pytest tests/unit/test_check_instructies.py -q` — verwacht: FAIL.
- [ ] **Stap 3: Implementeer.** Draai daarna `scripts/hooks/check-instructies.sh` op de worktree: geen nieuwe `FOUT`.
- [ ] **Stap 4: Draai** het testcommando — verwacht: PASS.
- [ ] **Stap 5: Commit** `feat(checks): check 6 en 13 slaan lighthouse-skills over`

### Taak D4: De bumplane raakt geen lighthouse-skill

**Files:**
- Modify: `scripts/skills_upstream_bump.py` (`lees_eenheden`, 209-224)
- Test: `tests/unit/test_skills_upstream_bump.py`

**Interfaces:**
- Produces: in `lees_eenheden` slaat een skill met een niet-lege `metadata["lighthouse"]` over, vóór de toets op `origin` (219). De skill komt niet in `external`, niet in `extended`, en niet in `fouten` of `overgeslagen`.

- [ ] **Stap 1: Test** `test_lighthouse_skill_is_outside_the_bump_lane`: een skill met `metadata.lighthouse` én `origin: external` (zonder `upstream`, wat anders een `LuideFout` gaf) → `eenheden.external == []` en `eenheden.fouten == []`.
- [ ] **Stap 2: Draai** `uv run pytest tests/unit/test_skills_upstream_bump.py -q` — verwacht: FAIL.
- [ ] **Stap 3: Implementeer.** De docstring van de module noemt de regel in één zin.
- [ ] **Stap 4: Draai** het testcommando — verwacht: PASS.
- [ ] **Stap 5: Commit** `feat(bumplane): lighthouse-skills vallen buiten de bumplane`

### Taak D5: Paia's skilltests slaan lighthouse-skills over

**Files:**
- Modify: `tests/unit/test_skill_mirror.py`, `tests/unit/test_instructielagen.py`

**Interfaces:**
- Produces: in elk van beide bestanden een helper `_is_lighthouse(skill_dir: Path) -> bool`: de `metadata`-tabel uit `yaml.safe_load` van de frontmatter heeft een niet-lege `lighthouse`. Gebruik: `test_every_skill_carries_the_name_prefix` geeft `return` bij een gemarkeerde skill; `test_skill_draagt_een_domein` slaat hem over (`pytest.skip` met reden `lighthouse skill`); `test_expected_metadata_table_matches_reality` laat hem buiten `actual`.

- [ ] **Stap 1: Schrijf de falende test** `test_is_lighthouse_detects_marker`, op een tijdelijke `SKILL.md`: onder `metadata:` → waar; top-level of leeg → onwaar.
- [ ] **Stap 2: Draai** — verwacht: FAIL.
- [ ] **Stap 3: Implementeer.**
- [ ] **Stap 4: Draai** het testcommando — verwacht: PASS.
- [ ] **Stap 5: Commit** `test(skills): prefix-, domein- en metadatatests slaan lighthouse-skills over`. Daarna één verse review van de hele branch (opus) en de vraag aan Corneel vóór push en PR.

---

## Overstaprecept (paia, per pakket)

Elke pakket-PR volgt deze stappen. De taak noemt alleen wat bijzonder is.

- **R1. Drift en tag.** Draai het drift-commando. Geeft het uitvoer, dan eerst een kleine lighthouse-PR die de wijziging overzet, vertaald en losgemaakt volgens het verhuisrecept (plan (b), V3–V5), met `source_sha` op de overgezette paia-commit en een nieuwe patch-tag. Daarna is het log leeg (besluit 12). De tag bevat het pakket.
- **R2. `lighthouse.toml`.** Vul met de hand aan wat het pakket nodig heeft (*Inventaris (d)*). Commit: `chore(lighthouse): config voor pack/<pakket>`.
- **R3. Weg en erin, in één commit.** Verwijder de oude kopieën in beide spiegels, hun hookregistraties in `.claude/settings.json`, en hun tests die in lighthouse meeverhuisd zijn. `init` geeft een conflict op een bestaand bestand dat niet gelijk is (`internal/reconcile/reconcile.go:87-103`). Draai daarna `lighthouse init` (core) of `lighthouse init --upgrade --pack <pakket>`, en lees de diff. Verwacht: geen conflict, niets buiten de paden uit *Global Constraints*. Commit: `feat(lighthouse): pack/<pakket> via lighthouse init; oude kopieën weg`.
- **R4. Wat tegelijk mee moet.** Elke aanroeper van een verhuisd of hernoemd bestand, en elke `paia-<skill>` uit dit pakket. Eén commit per soort.
- **R5. Instructielaag.** `spiegel-uitzonderingen.txt` opnieuw met `.lighthouse/scripts/check-instructions.sh --print-mirror-exceptions` (`pack/core/scripts/check-instructions.sh:745`), zodat hij alleen projecteigen skills draagt (DoD). `AGENTS.md`: haal uit het projectdeel wat het beheerde blok nu draagt. `AGENTS.md` + `CLAUDE.md` ≤ 3000 woorden (`wc -w`).
- **R6. Bewijs.** Het testcommando; `scripts/hooks/check-instructies.sh` en `.lighthouse/scripts/check-instructions.sh`; de grep uit *Global Constraints*, met nul treffers; `lighthouse init --upgrade`, met exact `No changes.`.

## PR core (paia)

### Taak K1: Config en installatie (R1–R3)

- [ ] **Stap 1: R1.** Ook: (b) PR 4 en PR 5 zijn gemerged. Lees hun beschrijving onder *Voor (d)* en vul *Inventaris (d)* aan in de PR-beschrijving.
- [ ] **Stap 2: R2.** `[project] language = "nl"` en `skill_prefix = "paia-"`; `[test_gate]`: `command` = het testcommando uit *Global Constraints* met ` --basetemp="$LIGHTHOUSE_GATE_TMP"` erachter, en `unchecked_exit_codes = [3, 4, 5]`; `[labels]`, `[paths]`, `[worktree_audit]`, `[ci]` uit *Inventaris (d)*. Het taxonomiebestand onder `[paths] labels` krijgt de inhoud uit de beschrijving van PR 4. Op de Beelink, buiten de repo: `gate_slice = "gates.slice"` in `~/.config/lighthouse/host.toml`.
- [ ] **Stap 3: R3.** Weg: de elf ketenskills (Global Constraints) in beide spiegels; `.claude/agents/issue-impact-scout.md`, `spec-adversary.md`; de hooks `main-commit-guard`, `worktree-guard`, `worktree-linkcheck`, `bash-write-gates`, `pr-create-gate`, `pre-commit-tests` met hun registraties en `.claude/hooks/lib/` (voor zover verhuisd); de scripts uit `pack/core/scripts/` (`codex_usage.py` alleen het `budget`/`read`-deel, ontwerpkeuze 12 van (b)); `.github/workflows/unblock-dependents.yml`; `.github/pull_request_template.md`; de paia-tests uit plan (b), *Binary*. Draai `lighthouse init`.
- [ ] **Stap 4: Bewijs dat de guards en de test-gate werken.** Een commit op de featurebranch draait de suite via `.claude/hooks/lighthouse/pre-commit-tests.sh`. Een payload `git commit` op `main` naar het commando uit `.claude/settings.json` geeft exit 2 (zoals de e2e van (a)).

### Taak K2: Controller en scripts (R4)

- [ ] **Stap 1: Tests eerst.** `test_controller_builds_lighthouse_names`: de starts van `decide.py:1094`, `1893`, `1977` en de handoff van `triage.py:811` noemen `/lighthouse-*`. `test_legacy_name_without_skill_is_returned`: `/paia-fix-pr 7` in een worktree zonder `lighthouse-fix-pr` gaat terug met de reden om te rebasen (Review Focus 1). `test_legacy_close_name_is_still_end`: `/paia-close-issue 12` uit zo'n worktree geeft geen Teruggegeven en geen start, net als `/lighthouse-close-issue 12` (`test_new_close_name_without_skill_is_end` uit d0). De controllertests importeren het protocol uit `lighthouse_lib` en lezen een oude `<!-- paia: … -->`-marker nog (Review Focus 2 van (b)). `test_tally_reads_lighthouse_data_dir`. Draai — verwacht: FAIL.
- [ ] **Stap 2: Implementeer.** `ALLOWLIST`, `EIND`, `VOOR_UITVOERING`, de commando's hierboven en `overzicht.py:65` naar `/lighthouse-*`. Een `/paia-*` van de allowlist die na `review_sprong` overblijft, gaat terug met `skill not installed — rebase the worktree onto main`. Breid daarvoor `_missing_skill` uit d0 uit; die toetst alleen namen waarvoor `on_allowlist` waar is. `EIND` valt daar buiten en blijft onder beide prefixen een eindhandoff. De vergelijking "letterlijk of dezelfde ketennaam" staat op één plek, `skill_names.matches`; de literals wijzigen, die functie niet. Het controllerbeleid importeert `lighthouse_lib.protocol` (*Inventaris (d)*). `artefacten.py` en het protocoldeel van `comments.py` gaan weg. `triage.py` op de nieuwe marker en tokens. `--setenv=LIGHTHOUSE_CONTROLLER_RUN` naast `PAIA_CONTROLLER_RUN` in `controller_launch.sh:133` en `forge_agent.sh`. `usage_window.py` laadt `.lighthouse/scripts/codex_usage.py`; ook `roborev.yml:106`. De hernoemde CLI-subcommando's. `review_pair.py tally` blijft als eigen script en leest `lighthouse_lib.datadir.data_dir()`.
- [ ] **Stap 3: Draai** het testcommando — verwacht: PASS. Commit per soort.

### Taak K3: Checks en pre-push (R4, R5)

- [ ] **Stap 1:** `check-instructies.sh` houdt alleen check 6, 9, 12 en 13, met hun nummers (ontwerpkeuze 5). De tests van de andere checks gaan weg; ze staan in lighthouse. `pre-push-instructielagen.sh` en `skills_upstream_bump.py:1147` draaien ook `.lighthouse/scripts/check-instructions.sh`. Test: in `tests/integration/test_pre_push_instructielagen.sh` een geval waarin een fout van alleen de lighthouse-check de push blokkeert.
- [ ] **Stap 2: R5.** Voeg aan `AGENTS.md` niets toe buiten het blok. Haal uit het projectdeel de regels die het blok van `pack/core` draagt: worktree vóór de eerste schrijfactie, niets direct op `main`, de verwijzingen naar ketenskills in *Vaste naden*, en het testcommando onder *Buiten Claude Code*.
- [ ] **Stap 3: Commit** per stap.

### Taak K4: De grep en het bewijs (R6)

- [ ] **Stap 1:** Draai de grep met de elf ketenskills. Elke treffer gaat over naar `/lighthouse-*`, of het pad valt onder ontwerpkeuze 6. Valt de grens anders uit dan verwacht, leg dat dan voor aan Corneel. Op `c2fd5bb54` staan de treffers buiten de historie in `tests/unit` (40), de skillspiegels (54), `deploy/beelink` (10), `.claude/hooks` (8), `src/controller` (4), `scripts/` (± 12), `.claude/rules` (2), `.claude/agents` (2) en `tests/fixtures` (1).
- [ ] **Stap 2: R6.** Daarna één verse review van de hele branch (opus) en de vraag aan Corneel vóór push en PR.

### Taak K5: Mergen met de controller stil

Het deel dat pas op `main` kan (ontwerpkeuze 10). Valt er iets te repareren, dan in een tweede PR onder hetzelfde issue.

- [ ] **Stap 1:** Op de Beelink: `systemctl --user stop controller-tick.timer`. Controleer: `systemctl --user is-active controller-tick.service` geeft `inactive` (geen tick halverwege).
- [ ] **Stap 2:** Squash-merge de core-PR. Werk de hoofdcheckout bij (`git -C <paia> pull --ff-only`). Daar moet `lighthouse init --upgrade` exact `No changes.` geven.
- [ ] **Stap 3:** Verhuis de reviewrecords (ontwerpkeuze 9): `mkdir -p ~/.local/share/lighthouse/review-records && mv ~/.local/share/lekmeting-keten/{plan-review,spec-review} ~/.local/share/lighthouse/review-records/`. Controleer: `python3 scripts/review_pair.py tally` telt evenveel records als vóór de verhuizing.
- [ ] **Stap 4:** Eén handmatige tick: `systemctl --user start controller-tick.service`. Controleer met `/paia-controller` dat geen issue `skill not installed` of `commando niet toegestaan` kreeg, tenzij het in een worktree van vóór de merge staat; rebase die (Review Focus 1).
- [ ] **Stap 5:** `systemctl --user start controller-tick.timer`.

## PR's expo, python, shell (paia)

### Taak T1: Taalpakket `<p>` (R1–R6)

Per pakket één PR, na de core-PR. `lighthouse init --upgrade --pack <p>` (plan (c), ontwerpkeuze 6).

- [ ] **expo:** weg `paia-refresh-expo-lockfiles` (beide spiegels). R4: `fix-pr` verwijst al naar `/lighthouse-refresh-expo-lockfiles` (core); `apps/CLAUDE.md` en de areas noemen de nieuwe naam. Geen config.
- [ ] **python:** weg `paia-python-schrijven`, `.claude/hooks/ruff-format.sh`, `ty-check.sh` met hun registraties, en `scripts/quality/` voor zover verhuisd. R2: de `[python]`-sleutels uit *Inventaris (d)*. Het fragment levert `[test_gate]`, en `init` laat paia's eigen `command` staan (plan (c), ontwerpkeuze 4). R4: de importeurs van `quality.*`, de workflows en `vendor/addyosmani-agent-skills/PROVENANCE.md:79` uit *Inventaris (d)*. Blijft: plan (c), ontwerpkeuze 14; elke test daarvan blijft groen of gaat weg met reden.
- [ ] **shell:** weg `paia-shell-gates`, `.claude/hooks/shellcheck.sh`, `scripts/hooks/shellcheck-repo.sh`, `scripts/hooks/lib/shellcheck-bin.sh`. R2: `[shell] strict_dirs`. R4: `AGENTS.md` *Buiten Claude Code*, `workflow-audit.sh` (blijft, eigen pin, besluit 17), `tests/integration/lib/gate-assert.sh`.
- [ ] **Elke PR:** de grep met de ketenskills tot en met dit pakket; R6; review (opus); vraag aan Corneel vóór push en PR. Elke paia-PR noemt `Refs CorneeldH/lighthouse#9`, en geen enkele sluit #9: dat gebeurt na de DoD hieronder.

---

## Na de laatste PR

- [ ] **DoD op de Mac.** Een andere eigen repo: `go install …@<tag>`, `lighthouse init --yes`, featurebranch en `main`. Een commit op `main` via Claude Code wordt geblokkeerd door `.claude/hooks/lighthouse/main-commit-guard.sh`. Zet het bewijs (commando's en uitvoer) als comment op #9.
- [ ] **De vier DoD-punten van de pitch** nalopen op #1, met per punt het bewijs. Het tweede punt noemt check 11 in `scripts/hooks/check-instructies.sh`. Na de core-PR draait check 11 vanuit `.lighthouse/scripts/check-instructions.sh` en slaat daar gemarkeerde skills over (ontwerpkeuze 5). Meld dat bij het bewijs.
- [ ] Zet in #9 onder *Verbeterpunten* wat tijdens de overstap opkwam. De aliassen voor `/paia-*` gaan eruit in M2.
- [ ] Sluit #9 met `/lighthouse-close-issue 9`, na akkoord van Corneel.
