# M0 (d): personal_ai_agent stapt over op `lighthouse init` — implementatieplan (#9)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** paia haalt `pack/core`, `pack/python`, `pack/shell` en `pack/expo` via `lighthouse init`. De oude kopieën zijn weg, en de controller start een lopend issue met een oude `/paia-*`-naam onder de nieuwe naam.

**Architecture:** Vijf PR's in paia, één voorbereiding en één per pakket (tabel *PR-indeling*), plus kleine bijwerk-PR's in lighthouse voor de drift (taak L1). Elke PR is groen op zichzelf. PR d0 kan nu al: hij maakt de controller en de checks bestand tegen `lighthouse-*`-skills, en is een no-op zolang die er niet zijn. De pakket-PR's volgen één recept (*Overstaprecept*): de drift controleren, `lighthouse.toml` aanvullen, de oude kopieën weghalen en `lighthouse init` draaien in één commit, dan de verwijzingen bijwerken.

**Tech Stack:** paia: Python ≥ 3.11, pytest via `uv`, bash. Lighthouse: Go 1.26, de binary via `go install`.

**Spec:** [`2026-10-17-m0-pakket-design.md`](../specs/2026-10-17-m0-pakket-design.md), secties *Opdeling (d)*, *Versiemarker en manifest*, *Risico's*; besluiten 6, 9, 12, 15, 17, 20, 21, 23, 25–29. Plan (b): [`2026-10-18-m0b-pack-core.md`](2026-10-18-m0b-pack-core.md). Plan (c): [`2026-10-18-m0c-taalpakketten.md`](2026-10-18-m0c-taalpakketten.md). Wat daar nog verandert (c2, c3, PR 4, PR 5), staat in die plannen en hun PR-beschrijvingen. Dit plan verwijst ernaar en kopieert het niet. Invoer: de inventaris in *Inventaris (d)* hieronder, de inventarissen op #7 en #8, en de secties *Voor (d)* in de beschrijvingen van #14, #16 en #21.

**Paia:** `/home/corneel/repositories/personal_ai_agent`, gemeten op `origin/main` = `c2fd5bb54` (2026-10-24). Regelnummers horen bij die commit.

## Ontwerpkeuzes

Elk punt is een voorstel. Wijs vóór de uitvoering aan wat anders moet.

1. **Herschrijven in de handoff-route, per worktree.** Een oude naam komt alleen binnen via een handoff-comment. De commando's die de controller zelf bouwt (`decide.py:989`, `1094`, `1893`, `1977`; `triage.py:777`, `811`), worden in de core-PR letterlijk `/lighthouse-*`. Daarom herschrijft `review_sprong` (`decide.py:113-127`) het commando als eerste stap. `_handoff_oordeel` (`decide.py:2003-2004`) geeft het resultaat al door aan `beoordeel_commando` en aan de launch (`_start`, `commando=` op 2113). Zo is er één plek. Herschreven wordt alleen als `<handoff.worktree>/.claude/skills/lighthouse-<n>/SKILL.md` bestaat. Dat is de worktree waarin de sessie start, en dus waar Claude Code de skill zoekt. `decide` is puur. Daarom meet de gather welke `lighthouse-*`-skills elke worktree heeft, en die meting komt via de bundel in `Invoer`.
2. **Beide prefixen in het controllerbeleid, met `ketennaam`.** `beoordeel_commando` (`comments.py:192-223`), `_REVIEW_SPRONG`, `VOOR_UITVOERING` en de fasevergelijking met `EIND` in `overzicht.py` vergelijken de kale ketennaam (`start-issue`, `fix-pr`, …). De literals `ALLOWLIST` en `EIND` blijven in d0 `/paia-*`, en de core-PR zet ze om. Een letterlijke `/lighthouse-<n>` in een handoff, terwijl de skill niet in de worktree staat, gaat terug met `skill niet geïnstalleerd`. Dat sluit het risico uit de spec: "laat een onbemande run stil de verkeerde of geen skill starten".
3. **Prefix-generiek, zodat de grep schoon blijft.** De herschrijving kent alleen de prefixen `/paia-` en `/lighthouse-`, geen skillnamen. Na de core-PR staat er dus geen `paia-start-issue` meer in code. De aliassen gaan er in M2 uit (spec, *Risico's*).
4. **d0 raakt ook check 6 en twee paia-tests.** De opdracht noemt check 8, 11, 13 en de bumplane. Check 6 (`check_naamcontract`), `test_every_skill_carries_the_name_prefix` (`test_skill_mirror.py:113-135`) en de domein- en metadatatests in `test_instructielagen.py` (803-819, 915-946) laten een `lighthouse-*`-skill ook falen, en blijven in paia. Met hetzelfde patroon horen ze in d0 (taak D5). Zonder D5 doet de core-PR dit.
5. **Check 8 en 11 leven in paia maar kort.** De core-PR haalt de generieke checks 1–5, 7, 8, 10 en 11 uit paia's script. Paia houdt 6, 9, 12 en 13 (beschrijving #16, *Voor (d)*). Daarna draaien 8 en 11 vanuit `.lighthouse/scripts/check-instructions.sh`, die de marker al overslaat en submappen ziet (3C). De d0-wijziging aan 8 en 11 is dus tijdelijk. Ze blijft toch in d0. De DoD noemt check 11 in `scripts/hooks/check-instructies.sh` letterlijk, en de pre-push (`pre-push-instructielagen.sh`) en de bumplane (`skills_upstream_bump.py:1147`) draaien paia's script op elke tussenstand van de core-PR.
6. **"Buiten de historie" voor de grep.** Uitgezonderd zijn bestanden die vastleggen wat was: `docs/developer/planning/**`, `CHANGELOG.md`, `docs/developer/reflect/`, `docs/developer/research/`, de gegenereerde codebase-kaart (`docs/developer/codebase/`, `.planning/`), `experiments/` en `vendor/`. Op `c2fd5bb54` raakt de grep 357 bestanden, waarvan 176 in `docs/developer/planning`. Het commando staat in *Global Constraints*.
7. **Drift eerst overzetten, dan pas de pakket-PR.** Is `git log <sha>..origin/main -- <pad>` niet leeg, dan gaat de wijziging naar lighthouse in een eigen PR (taak L1), met `source_sha` op de paia-commit die is overgezet. Daarna is het log leeg, en geldt het bestand opnieuw als bevroren (besluit 12). Op `c2fd5bb54` gaat het om twee commits: `d11f5b5e9` (de actie-pin in `unblock-dependents.yml`) en `db605b7a7` (`upgrading-expo` → `expo-upgrade` in `paia-refresh-expo-lockfiles`).
8. **Eén lighthouse-tag per golf.** De core-PR heeft een tag nodig met heel (b). Zijn c2 en c3 dan ook gemerged, dan bedient één tag alle vier de PR's. Voorstel: `v0.1.0`, gezet door Corneel nadat L1 en (b) PR 5 gemerged zijn. Paia installeert met `GOPRIVATE=github.com/CorneeldH/* go install github.com/CorneeldH/lighthouse/cmd/lighthouse@<tag>` (besluit 3).
9. **De reviewrecords.** De oude records staan in `~/.local/share/lekmeting-keten/`, samen met andere experimentdata. Voorstel: geen `LIGHTHOUSE_DATA`-override. Nieuwe records komen op de default van besluit 27, en de oude blijven waar ze staan, voor `experiments/lekmeting-keten/`.
10. **Paia-issues.** Eén paia-issue voor d0. Voor de vier pakket-PR's één paia-issue per PR, of één verzamelissue. Corneel kiest bij de start van d0.

## Global Constraints

- Paden, letterlijk uit de spec: skills `.claude/skills/lighthouse-<naam>/` en `.agents/skills/lighthouse-<naam>/`; agents `.claude/agents/lighthouse-<naam>.md`; hooks `.claude/hooks/lighthouse/<naam>.sh`; Python `.lighthouse/lib/lighthouse_lib/`; scripts `.lighthouse/scripts/`; workflows `.github/workflows/lighthouse-<naam>.yml`; manifest `.lighthouse/manifest.toml`.
- Marker (`internal/packs/marker.go`): in een `SKILL.md` de regel `  lighthouse: "<pakket>@<versie>"` onder `metadata:`. Paia leest hem met de bestaande `skill_meta <dir> lighthouse` (`check-instructies.sh:126-137`) en in Python met `_lees_metadata(...)` (`skills_upstream_bump.py:120`). Een skillmap telt als lighthouse-map als zijn `SKILL.md` die sleutel draagt, met een niet-lege waarde.
- Ketenskills voor de grep (besluit 6): `start-issue`, `route-issue`, `handoff`, `verify-pr`, `fix-pr`, `close-issue`, `spec-adversarial-review`, `plan-adversarial-review`, `write-issue`, `worktree-audit`, `instrumenteren`; per taal-PR ook `python-schrijven`, `shell-gates` en `refresh-expo-lockfiles`. Blijven in paia en tellen niet mee: `paia-controller`, `paia-pauzeer`, `paia-backlog-stand`, `paia-board-sweep` en de rest uit de spec, *Inventaris*.
- Grep, nul treffers op de kop van elke pakket-PR (`<namen>` = de ketenskills tot en met die PR):
  ```bash
  git grep -nE 'paia-(<namen>)\b' -- . ':!docs/developer/planning' ':!CHANGELOG.md' \
    ':!docs/developer/reflect' ':!docs/developer/research' ':!docs/developer/codebase' \
    ':!.planning' ':!experiments' ':!vendor'
  ```
- Drift (besluit 12), vanuit lighthouse, verwacht: geen uitvoer:
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
- Nooit herschrijven: oude comments op GitHub (spec, *Risico's*).

## Review Focus

1. **Een handoff uit een worktree die vóór de core-PR is gemaakt.** Na de merge heeft `main` de `lighthouse-*`-skills, maar een oude featureworktree nog de `paia-*`-kopieën. Verwacht: `/paia-fix-pr 7` in die worktree blijft `/paia-fix-pr 7` en start de oude kopie daar. Test: `test_skill_in_andere_worktree_telt_niet` in taak D2.
2. **Een letterlijke `/lighthouse-<n>` zonder skill in de worktree.** Verwacht: Teruggegeven met `skill niet geïnstalleerd`, geen start. Test: `test_nieuwe_naam_zonder_skill_gaat_terug` in taak D2.
3. **Een uitgezette review met de nieuwe naam.** `spec_review = false` en een handoff `/lighthouse-spec-adversarial-review <spec>`. Verwacht: dezelfde sprong naar `/superpowers:writing-plans` als met de oude naam. Test: `test_nieuwe_reviewnaam_springt_ook` in taak D2.
4. **Een marker op de verkeerde plek.** `lighthouse:` top-level in de frontmatter, of alleen in de `.agents`-spiegel. Verwacht: top-level telt niet als marker (de check toetst gewoon), en één gemarkeerde kant is genoeg om check 11 het paar te laten overslaan. Test: `test_marker_buiten_metadata_telt_niet` en `test_check_11_een_kant_gemarkeerd` in taak D3.
5. **Een forge-workspace als worktree.** `controller_gather.py:779-781` voegt forge-paden toe aan `worktrees`. Verwacht: de skillmeting kijkt daar ook, en een pad dat lokaal niet bestaat, geeft een lege set zonder fout. Test: `test_lighthouse_skills_onbestaand_pad` in taak D1.

---

## Inventaris (d)

Gemeten op paia `c2fd5bb54`. De bouwer toetst de regelnummers bij de uitvoering.

### Controller

| Plek | Wat | Wanneer |
|---|---|---|
| `src/controller/comments.py:32-40` | `ALLOWLIST` (`/paia-start-issue`, `-spec-adversarial-review`, `-plan-adversarial-review`, `-fix-pr`), `EIND = "/paia-close-issue"` | d0: vergelijken op ketennaam; core: literals naar `/lighthouse-*` |
| `comments.py:192-223` | `beoordeel_commando`, met `== "/paia-start-issue"` (211) en `== "/paia-fix-pr"` (213) | d0 |
| `src/controller/decide.py:88-92` | `VOOR_UITVOERING` | d0: ketennaam |
| `decide.py:101-127` | `_REVIEW_SPRONG`, `review_sprong` | d0: herschrijven, kale sleutel |
| `decide.py:2000-2033` | `_handoff_oordeel`: `review_sprong` → `beoordeel_commando` → `VOOR_UITVOERING` (2029) → `_start` (2113) | d0 |
| `decide.py:979` | `_oordeel`: `beoordeel_commando` zonder `review_sprong` | d0 via ontwerpkeuze 2 |
| `decide.py:989`, `1094`, `1893`, `1977`; `triage.py:777`, `811`; `overzicht.py:65` | commando's die de controller zelf bouwt | core |
| `src/controller/overzicht.py:24`, `100`, `297`, `357` | `fase == EIND` | d0: ketennaam |
| `decide.py:267-306` | `Invoer` | d0: veld `lighthouse_skills` |
| `scripts/controller_gather.py:495-512`, `778-801` | `worktrees()`, de bundel | d0: meting `lighthouse_skills` |
| `scripts/controller_state.py:154-170` | `Invoer` uit de bundel | d0 |
| `tests/helpers/controller.py:194-246` | `invoer()`-helper | d0: parameter `lighthouse_skills` |
| `scripts/controller_launch.sh:133`, `scripts/forge_agent.sh:152-279`, `scripts/controller_vraag.py:42` | `PAIA_CONTROLLER_RUN` | core: `LIGHTHOUSE_CONTROLLER_RUN` ernaast (#7, *Na de laatste taak*) |

### Checks, bumplane en tests

| Plek | Wat | Wanneer |
|---|---|---|
| `scripts/hooks/check-instructies.sh:523-550` | check 6: eist `paia-` of `origin: external` | d0 (D3) |
| `check-instructies.sh:678` | check 8: `grep -oE '\.claude/hooks/[A-Za-z0-9_-]+\.sh'` ziet geen submap; `hook_geregistreerd` (617-658) escapet het hele pad en kan het wel | d0 (D3) |
| `check-instructies.sh:234-305` | check 11: `spiegel_verschillen` vergelijkt elk paar uit `comm -12` van beide skillwortels, en `--print-spiegel-uitzonderingen` gebruikt dezelfde functie | d0 (D3) |
| `check-instructies.sh:943-1000` | check 13: eist `metadata.domain` ∈ {applicatie, inhoud, factory, infra}, voor beide wortels | d0 (D3) |
| `check-instructies.sh:800-888` | check 9: kijkt alleen naar `origin: external|extended`; een pakketskill draagt geen `origin` | geen wijziging |
| `scripts/skills_upstream_bump.py:209-224` | `lees_eenheden`: leest `origin` uit `_lees_metadata` (219) | d0 (D4): marker eerst overslaan |
| `tests/unit/test_skill_mirror.py:86`, `113-135` | `NAME_PREFIX = "paia-"`, prefixtest | d0 (D5) |
| `tests/unit/test_instructielagen.py:803-819`, `915-946` | `DOMEINEN`, `test_skill_draagt_een_domein`, `test_expected_metadata_table_matches_reality` | d0 (D5) |
| `scripts/hooks/pre-push-instructielagen.sh:17` | draait `check-instructies.sh` | core: ook `.lighthouse/scripts/check-instructions.sh` |

### Wat de PR's van (b) en (c) voor (d) noemen

Uit de plannen en de beschrijvingen van #14, #16, #18 en #21. Wat c2, c3, PR 4 en PR 5 er nog aan toevoegen, staat in hun eigen beschrijving onder *Voor (d)*. De bouwer leest die bij de start van de pakket-PR.

- **Controllerbeleid** importeert `lighthouse_lib.protocol` uit `.lighthouse/lib/`: `ALLOWLIST`, `EIND`, `BRAINSTORM`, `INTERACTIEF`, `NATIVE`, `NATIVE_MODEL`, `beoordeel_commando`, `is_antwoord`, `antwoord_prompt`, de proza van `render_klaar` (`render_done(note=…)`) en de lane-validatie. `src/controller/artefacten.py` en het protocoldeel van `comments.py` gaan weg. De controller leest de Engelse vraagvelden uit `parse_question` (#14).
- **`triage.py`** stapt over op de marker uit `protocol` en schrijft de triage-marker met de nieuwe tokens (plan (b), *Protocoltokens*).
- **`review_pair.py tally`** blijft in paia (#14).
- **`codex_usage.py`**: `gate`, `_parkeer` en `schrijf_marker` blijven in paia (besluit 26). `usage_window.py:742-757` laadt `budget`/`read` uit `.lighthouse/scripts/codex_usage.py` en leest de Engelse sleutels (#16). Andere aanroepers: `.github/workflows/roborev.yml:106`.
- **Hernoemde CLI**: `accepteer-open` → `accept-open`, `pr-open-punten` → `pr-open-points`, `--heropen` → `--reopen`, `--voor-pr` → `--before-pr` (plan (b), ontwerpkeuze 3).
- **`lighthouse.toml` van paia**: `[project] language = "nl"`; `[test_gate]` met het testcommando hierboven plus `--basetemp="$LIGHTHOUSE_GATE_TMP"`, en `unchecked_exit_codes = [3, 4, 5]`; `[labels]` met paia's namen (`3-blocked-issue` …, `location_prefix = "1-loc:"`, `area_prefix = "2-area:"`); `[paths] tenets`, `areas`, `history`, `labels` (de taxonomie uit de beschrijving van PR 4, besluit 28), `mirror_exceptions = "scripts/hooks/spiegel-uitzonderingen.txt"`; `[worktree_audit] protected_refs = ["entire/*"]`, `protected_paths = [".port-registry"]`; `[ci] runs_on` en de bot-sleutels; `[project] skill_prefix = "paia-"`. `host.toml` op de Beelink: `gate_slice = "gates.slice"` onder `[host]` (besluit 20).
- **Python** (plan (c), *Na de laatste taak*): `[shell] strict_dirs = ["scripts/hooks", "tests/integration", ".claude/hooks"]`; `[python] coverage_command` met paia's markers; `nightly_workflow = "nightly-tests.yml"`; `coverage_issue_labels = ["1-loc:container", "2-area:meta"]`; `nightly-tests.yml` roept `.lighthouse/scripts/quality/ratchet.py nightly` aan met `LIGHTHOUSE_BOARD_TOKEN` en `LIGHTHOUSE_HANDOFF_WORKTREE`; `quality-baseline-ratchet.yml` roept `tighten` aan op het nieuwe pad. Importeurs van `quality.*`: `scripts/deep_sweep.py:55-60`, `1565`, `1711`, `1720`; `scripts/opencode-lane/lane/gates.py:17`; `scripts/hooks/pre-push-quality.sh:127`. Wat in paia blijft: plan (c), ontwerpkeuze 14.
- **Shell**: `workflow-audit.sh` blijft in paia (besluit 17), met zijn eigen shellcheck-pin. `AGENTS.md` *Buiten Claude Code* wijst naar `.lighthouse/scripts/shellcheck-repo.sh`.
- **Expo**: alleen `npm`, geen workspaces (#21).

---

## PR-indeling

| PR | Repo, branch | Taken | Heeft nodig |
|---|---|---|---|
| plan | lighthouse, `issue-9-plan` | dit plan | — |
| d0 voorbereiding | paia, `issue-<nr>-lighthouse-voorbereiding` | D1–D5 | het plan |
| L1 drift | lighthouse, `fix-drift-paia-<datum>` | L1 | — (opnieuw vóór elke pakket-PR) |
| core | paia, `issue-<nr>-lighthouse-core` | K1–K5 | d0; (b) PR 4 en PR 5 gemerged; L1; tag (ontwerpkeuze 8) |
| expo | paia, `issue-<nr>-lighthouse-expo` | T1 voor `expo` | core; c4 (#21, gemerged); L1 met `db605b7a7`; tag |
| python | paia, `issue-<nr>-lighthouse-python` | T1 voor `python` | core; c2; L1; tag |
| shell | paia, `issue-<nr>-lighthouse-shell` | T1 voor `shell` | core; c3; L1; tag |

- **Volgorde van de taal-PR's.** Elk na de core-PR, onderling vrij. Expo kan als eerste, want c4 staat al op `main`.
- **Gedeelde bestanden in paia.** `lighthouse.toml`, `.lighthouse/manifest.toml`, `.claude/settings.json`, `AGENTS.md` en `scripts/hooks/spiegel-uitzonderingen.txt`. Twee pakket-PR's tegelijk betekent rebasen en `lighthouse init --upgrade` opnieuw draaien. Voorstel: één tegelijk.

---

## PR d0 — voorbereiding (paia)

Werkwijze: paia's `AGENTS.md`, een worktree, TDD. Elke taak eindigt met het testcommando uit *Global Constraints*.

### Taak D1: Welke `lighthouse-*`-skills elke worktree heeft

**Files:**
- Create: `src/controller/skillnamen.py`, `tests/unit/test_controller_skillnamen.py`
- Modify: `src/controller/decide.py` (`Invoer`), `scripts/controller_gather.py`, `scripts/controller_state.py`, `tests/helpers/controller.py`, `tests/unit/test_controller_gather.py`, `tests/unit/test_controller_state_cli.py`

**Interfaces:**
- Produces in `skillnamen.py`:
  - `PREFIXEN = ("/paia-", "/lighthouse-")`.
  - `ketennaam(commando: str) -> str | None`: van het eerste woord de naam na een prefix uit `PREFIXEN`, anders `None`. `"/paia-fix-pr 7"` → `"fix-pr"`; `"/superpowers:writing-plans x"` → `None`; `""` → `None`.
  - `herschrijf(volgende: str, geinstalleerd: frozenset[str]) -> str`: begint het eerste woord met `/paia-` en staat zijn ketennaam in `geinstalleerd`, dan wordt dat woord `/lighthouse-<naam>`; de rest blijft byte voor byte gelijk. Anders ongewijzigd.
- `Invoer.lighthouse_skills: dict[str, frozenset[str]] = field(default_factory=dict)`: worktreepad → de namen `<n>` waarvoor `<pad>/.claude/skills/lighthouse-<n>/SKILL.md` een bestand is.
- `controller_gather.lighthouse_skills(paden: Iterable[str]) -> dict[str, list[str]]`: gesorteerde namen per pad. Een pad zonder zulke skills, of een pad dat niet bestaat, staat er niet in. De bundel krijgt de sleutel `"lighthouse_skills"` over `[hoofd, *paden]`, na het samenvoegen met de forge-paden (779-781).
- `controller_state` zet de lijsten om naar `frozenset`. Een bundel zonder de sleutel geeft `{}`.
- `tests/helpers/controller.invoer(..., lighthouse_skills: dict[str, set[str]] | None = None)`.

- [ ] **Stap 1: Schrijf de falende tests.**
  - `test_ketennaam` (geparametriseerd over de voorbeelden hierboven, plus `/lighthouse-start-issue 12` → `start-issue` en `/paia-` → `None`).
  - `test_herschrijf_alleen_geinstalleerd`: `herschrijf("/paia-start-issue 12", frozenset({"start-issue"})) == "/lighthouse-start-issue 12"`; met `frozenset()` ongewijzigd; `/paia-fix-pr 7` met alleen `start-issue` ongewijzigd.
  - `test_herschrijf_laat_rest_staan`: `"/paia-spec-adversarial-review /a/b.md  extra"` → alleen het eerste woord anders.
  - `test_herschrijf_raakt_geen_andere_commando_s`: `/superpowers:writing-plans x` en `/lighthouse-fix-pr 7` ongewijzigd.
  - `test_lighthouse_skills_per_worktree` (`test_controller_gather.py`): `tmp_path/a` met `.claude/skills/lighthouse-start-issue/SKILL.md` en een map `lighthouse-fix-pr/` zonder `SKILL.md`, `tmp_path/b` zonder skills → `{str(a): ["start-issue"]}`.
  - `test_lighthouse_skills_onbestaand_pad`: een pad dat niet bestaat → geen sleutel, geen fout.
  - `test_bundel_lighthouse_skills_naar_invoer` en `test_bundel_zonder_lighthouse_skills` (`test_controller_state_cli.py`).
- [ ] **Stap 2: Draai** `uv run pytest tests/unit/test_controller_skillnamen.py tests/unit/test_controller_gather.py tests/unit/test_controller_state_cli.py -q` — verwacht: FAIL.
- [ ] **Stap 3: Implementeer.**
- [ ] **Stap 4: Draai** het testcommando — verwacht: PASS.
- [ ] **Stap 5: Commit** `feat(controller): meet per worktree welke lighthouse-skills er staan`

### Taak D2: Oude naam herschrijven vóór de launch, beide namen in het beleid

**Files:**
- Modify: `src/controller/decide.py` (`VOOR_UITVOERING`, `_REVIEW_SPRONG`, `review_sprong`, `_handoff_oordeel`), `src/controller/comments.py` (`beoordeel_commando`), `src/controller/overzicht.py` (`fase == EIND` op 100, 297, 357)
- Test: `tests/unit/test_controller_decide_starten.py` (nieuwe klasse `TestSkillnaamHerschrijven`, naast `TestReviewOverslaan`), de test van `beoordeel_commando` en van `overzicht`

**Interfaces:**
- Consumes: `ketennaam`, `herschrijf`, `Invoer.lighthouse_skills` (taak D1).
- Produces:
  - `review_sprong(volgende: str, inv: Invoer, geinstalleerd: frozenset[str] = frozenset()) -> tuple[str, str]`: eerst `herschrijf(volgende, geinstalleerd)`, dan de sprong. `_REVIEW_SPRONG` sleutelt op `"spec-adversarial-review"` en `"plan-adversarial-review"`.
  - `_handoff_oordeel` geeft `ctx.inv.lighthouse_skills.get(handoff.worktree, frozenset())` mee. Begint het commando daarna met `/lighthouse-` en staat zijn ketennaam niet in die set, dan: `_terug(issue, "skill niet geïnstalleerd", detail=f"{commando} staat niet in {handoff.worktree}/.claude/skills/", react=c.id)`, vóór `beoordeel_commando`.
  - `beoordeel_commando` herkent een commando als het in `ALLOWLIST` staat, óf als zijn ketennaam gelijk is aan die van een element van `ALLOWLIST`. Hetzelfde geldt voor `EIND`. De argumentregels van 211 en 213 kijken naar de ketennaam (`"start-issue"`, `"fix-pr"`).
  - `_voor_uitvoering(commando: str) -> bool` in `decide.py`: waar als het eerste woord in `VOOR_UITVOERING` staat, of als zijn ketennaam die van een element ervan is. Vervangt de test op 2029.
  - `overzicht.py` vergelijkt `ketennaam(fase) == ketennaam(EIND)`.

- [ ] **Stap 1: Schrijf de falende tests.** Met de helpers `issue`, `comment`, `handoff`, `invoer`, `beslis_`, `van_soort`:
  - `test_oude_naam_blijft_zonder_lighthouse_skill`: `handoff(volgende="/paia-start-issue 12")`, geen `lighthouse_skills` → `start["commando"] == "/paia-start-issue 12"`.
  - `test_oude_naam_wordt_nieuw_met_lighthouse_skill`: `lighthouse_skills={WT: {"start-issue"}}` → `start["commando"] == "/lighthouse-start-issue 12"` en `start["prompt"].startswith("/lighthouse-start-issue 12 ")`.
  - `test_skill_in_andere_worktree_telt_niet`: `lighthouse_skills={HOOFD: {"start-issue"}}`, handoff in `WT` → ongewijzigd.
  - `test_herschreven_review_springt_bij_review_uit`: `/paia-spec-adversarial-review {SPEC}`, `{WT: {"spec-adversarial-review"}}`, `spec_review=False` → `/superpowers:writing-plans {SPEC}`, met `sprong:` in het bericht.
  - `test_nieuwe_reviewnaam_springt_ook`: `/lighthouse-plan-adversarial-review {PLAN}`, geïnstalleerd, `plan_review=False` → `SDD`.
  - `test_nieuwe_reviewnaam_is_voor_uitvoering`: `/lighthouse-plan-adversarial-review {PLAN}`, geïnstalleerd, een open `Blokkeerder` → er komt een start (tegenhanger van `test_plan_review_aan_start_wel_bij_open_blokkeerder`).
  - `test_nieuwe_naam_zonder_skill_gaat_terug`: `/lighthouse-fix-pr 7`, geen skill → geen start, één Teruggegeven met `skill niet geïnstalleerd`.
  - `test_beoordeel_beide_prefixen`: `("/lighthouse-start-issue 12", 12)` → `start`; `("/lighthouse-start-issue 13", 12)` → `afgewezen`; `("/lighthouse-fix-pr 7", 12)` → `start`; `("/lighthouse-close-issue 12", 12)` → `eind`; `("/lighthouse-route-issue 12", 12)` → `afgewezen`.
  - `test_fase_close_issue_beide_namen` (overzicht): een issue met een gemergde PR en fase `/lighthouse-close-issue` valt in dezelfde groep als met `/paia-close-issue`.
  - Bestaande tests (`TestReviewOverslaan` en de rest) blijven ongewijzigd groen.
- [ ] **Stap 2: Draai** `uv run pytest tests/unit/ -k "controller" -q` — verwacht: FAIL op de nieuwe tests.
- [ ] **Stap 3: Implementeer.**
- [ ] **Stap 4: Draai** het testcommando — verwacht: PASS.
- [ ] **Stap 5: Commit** `feat(controller): herschrijf een oude /paia-naam naar lighthouse als de skill er staat`

### Taak D3: Check 6, 8, 11 en 13 en de lighthouse-marker

**Files:**
- Modify: `scripts/hooks/check-instructies.sh`
- Test: `tests/unit/test_check_instructies.py`

**Interfaces:**
- Produces: `is_lighthouse_skill() { [ -n "$(skill_meta "$1" lighthouse)" ]; }`, met als argument een skillmap, naast `skill_meta`. Gebruik:
  - check 6 (`check_naamcontract`): een gemarkeerde map in `.claude/skills` slaat de check over, vóór de toets op `origin`;
  - check 8: de regex op 678 wordt `'\.claude/hooks/([A-Za-z0-9_-]+/)*[A-Za-z0-9_-]+\.sh'`;
  - check 11 (`spiegel_verschillen`): een paar waarvan één van beide `SKILL.md`'s gemarkeerd is, valt weg, ook uit `--print-spiegel-uitzonderingen`;
  - check 13 (`check_domein`): een gemarkeerde map slaat de check over, per wortel;
  - de kop van het script noemt de regel in één zin: lighthouse-mappen zijn van lighthouse en worden daar getoetst.

- [ ] **Stap 1: Schrijf de falende tests**, met de fixturestijl van het bestand:
  - `test_check_6_slaat_lighthouse_skill_over`: `.claude/skills/lighthouse-x` met marker, zonder `origin` → geen `FOUT  naamcontract`.
  - `test_check_8_ziet_hook_in_submap`: een skill met `binding: hard` die `.claude/hooks/lighthouse/x.sh` noemt. Bestaat het script en staat het in `settings.json` → `ok`; staat het er niet in → `FOUT  pairing`.
  - `test_check_11_slaat_lighthouse_skill_over`: een gemarkeerd paar met verschillende inhoud → `ok    spiegel-inhoud`, en `--print-spiegel-uitzonderingen` noemt het paar niet.
  - `test_check_11_een_kant_gemarkeerd`: alleen de `.agents`-kant draagt de marker → het paar valt ook weg.
  - `test_check_11_ongemarkeerd_verschil_blijft_fout`: tegenhanger, zonder marker → `FOUT`.
  - `test_check_13_slaat_lighthouse_skill_over`: gemarkeerd, zonder `domain` → geen `FOUT  domein`.
  - `test_marker_buiten_metadata_telt_niet`: `lighthouse: "core@v0.1.0"` top-level → check 6 en 13 toetsen gewoon.
- [ ] **Stap 2: Draai** `uv run pytest tests/unit/test_check_instructies.py -q` — verwacht: FAIL.
- [ ] **Stap 3: Implementeer.** Draai daarna `scripts/hooks/check-instructies.sh` op de worktree: geen nieuwe `FOUT`, en `--print-spiegel-uitzonderingen` geeft byte voor byte het huidige `spiegel-uitzonderingen.txt`.
- [ ] **Stap 4: Draai** het testcommando — verwacht: PASS.
- [ ] **Stap 5: Commit** `feat(checks): check 6, 11 en 13 slaan lighthouse-skills over, check 8 ziet hooksubmappen`

### Taak D4: De bumplane raakt geen lighthouse-skill

**Files:**
- Modify: `scripts/skills_upstream_bump.py` (`lees_eenheden`, 209-224)
- Test: `tests/unit/test_skills_upstream_bump.py`

**Interfaces:**
- Produces: in `lees_eenheden` slaat een skill met een niet-lege `metadata["lighthouse"]` over, vóór de toets op `origin` (219). De skill komt niet in `external`, niet in `extended`, en niet in `fouten` of `overgeslagen`.

- [ ] **Stap 1: Test** `test_lighthouse_skill_valt_buiten_de_bumplane`: een skill met `metadata.lighthouse` én `origin: external` (zonder `upstream`, wat anders een `LuideFout` gaf) → `eenheden.external == []` en `eenheden.fouten == []`.
- [ ] **Stap 2: Draai** `uv run pytest tests/unit/test_skills_upstream_bump.py -q` — verwacht: FAIL.
- [ ] **Stap 3: Implementeer.** De docstring van de module noemt de regel in één zin.
- [ ] **Stap 4: Draai** het testcommando — verwacht: PASS.
- [ ] **Stap 5: Commit** `feat(bumplane): lighthouse-skills vallen buiten de bumplane`

### Taak D5: Paia's skilltests slaan lighthouse-skills over (ontwerpkeuze 4)

**Files:**
- Modify: `tests/unit/test_skill_mirror.py`, `tests/unit/test_instructielagen.py`

**Interfaces:**
- Produces: in elk van beide bestanden één helper `_is_lighthouse(skill_dir: Path) -> bool` (de `metadata`-tabel uit `yaml.safe_load` van de frontmatter heeft een niet-lege `lighthouse`). Gebruik: `test_every_skill_carries_the_name_prefix` geeft `return` bij een gemarkeerde skill; `test_skill_draagt_een_domein` slaat hem over (`pytest.skip` met reden `lighthouse skill`); `test_expected_metadata_table_matches_reality` laat hem buiten `actual`.

- [ ] **Stap 1: Schrijf de falende tests**, met een fixture die een tijdelijke skillboom opbouwt waarin de helper werkt: `test_is_lighthouse_herkent_marker` (onder `metadata:` → waar; top-level of leeg → onwaar).
- [ ] **Stap 2: Draai** — verwacht: FAIL.
- [ ] **Stap 3: Implementeer.**
- [ ] **Stap 4: Draai** het testcommando — verwacht: PASS.
- [ ] **Stap 5: Commit** `test(skills): prefix-, domein- en metadatatests slaan lighthouse-skills over`. Daarna één verse review van de hele branch (opus) en de vraag aan Corneel vóór push en PR.

---

## PR L1 — drift overzetten (lighthouse)

### Taak L1: Paia-wijzigingen na `source_sha` naar lighthouse

Draai vóór elke pakket-PR opnieuw. Alleen als het drift-commando uit *Global Constraints* uitvoer geeft.

**Files:**
- Modify: per regel uit de uitvoer het bestand in `pack/`, zijn test, en `source_sha` in `pack/SOURCES.toml`

- [ ] **Stap 1: Draai het drift-commando.** Op `c2fd5bb54` geeft het:
  - `pack/core/workflows/unblock-dependents.yml` ← `d11f5b5e9` (actie-pin `actions/create-github-app-token`);
  - `pack/expo/skills/refresh-expo-lockfiles/SKILL.md` ← `db605b7a7` (`upgrading-expo` → `expo-upgrade`).
- [ ] **Stap 2: Test eerst.** Per wijziging een test die de nieuwe inhoud pint. Voor de workflow: de pin van de actie. Voor expo: `test_optional_refs_guarded` noemt `expo-upgrade`. Draai — verwacht: FAIL.
- [ ] **Stap 3: Zet de wijziging over**, vertaald en losgemaakt volgens het verhuisrecept (plan (b), V3–V5). Zet `source_sha` op de overgezette paia-commit.
- [ ] **Stap 4: Draai** `uv run pytest -q && go test ./... && ci/literal-grep.sh`, en het drift-commando — verwacht: groen en geen uitvoer.
- [ ] **Stap 5: Commit** `fix(<pakket>): paia <sha> overgezet`, één per pakket, `Refs #9`. Open de PR.

---

## Overstaprecept (paia, per pakket)

Elke pakket-PR volgt deze stappen. De taak noemt alleen wat bijzonder is.

- **R1. Drift.** Het drift-commando geeft geen uitvoer (anders eerst L1), en de tag bevat het pakket.
- **R2. `lighthouse.toml`.** Vul met de hand aan wat het pakket nodig heeft (*Inventaris (d)*). Commit: `chore(lighthouse): config voor pack/<pakket>`.
- **R3. Weg en erin, in één commit.** Verwijder de oude kopieën, beide spiegels, hun hookregistraties in `.claude/settings.json`, en hun tests die in lighthouse meeverhuisd zijn. `init` geeft een conflict op een bestaand bestand dat niet gelijk is (`internal/reconcile/reconcile.go:87-103`). Draai daarna `lighthouse init` (core) of `lighthouse init --upgrade --pack <pakket>`, en lees de diff. Verwacht: geen conflict, niets buiten de paden uit *Global Constraints*. Commit: `feat(lighthouse): pack/<pakket> via lighthouse init; oude kopieën weg`.
- **R4. Verwijzingen.** Elke aanroeper van een verhuisd of hernoemd bestand en elke `paia-<skill>` uit dit pakket. Eén commit per soort.
- **R5. Instructielaag.** `spiegel-uitzonderingen.txt` opnieuw met `--print-spiegel-uitzonderingen`, zodat hij alleen projecteigen skills draagt (DoD). `AGENTS.md`: haal uit het projectdeel wat het beheerde blok nu draagt. `AGENTS.md` + `CLAUDE.md` ≤ 3000 woorden (`wc -w`).
- **R6. Bewijs.** Het testcommando; `scripts/hooks/check-instructies.sh`; de grep uit *Global Constraints*, met nul treffers; `lighthouse init --upgrade`, met exact `No changes.`.

## PR core (paia)

### Taak K1: Config en installatie (R1–R3)

- [ ] **Stap 1: R1.** Ook: (b) PR 4 en PR 5 zijn gemerged; lees hun beschrijving onder *Voor (d)* en vul *Inventaris (d)* aan in de PR-beschrijving.
- [ ] **Stap 2: R2.** `[project] language = "nl"` en `skill_prefix = "paia-"`; `[test_gate]`: `command` = het testcommando uit *Global Constraints* met ` --basetemp="$LIGHTHOUSE_GATE_TMP"` erachter, en `unchecked_exit_codes = [3, 4, 5]`; `[labels]`, `[paths]`, `[worktree_audit]`, `[ci]` uit *Inventaris (d)*. Het taxonomiebestand onder `[paths] labels` krijgt de inhoud uit de beschrijving van PR 4. Op de Beelink, buiten de repo: `gate_slice = "gates.slice"` in `~/.config/lighthouse/host.toml`.
- [ ] **Stap 3: R3.** Weg: de elf ketenskills (Global Constraints) in beide spiegels; `.claude/agents/issue-impact-scout.md`, `spec-adversary.md`; de hooks `main-commit-guard`, `worktree-guard`, `worktree-linkcheck`, `bash-write-gates`, `pr-create-gate`, `pre-commit-tests` met hun registraties en `.claude/hooks/lib/` (voor zover verhuisd); de scripts uit `pack/core/scripts/` (`codex_usage.py` alleen het `budget`/`read`-deel, ontwerpkeuze 12 van (b)); `.github/workflows/unblock-dependents.yml`; `.github/pull_request_template.md`; de paia-tests uit plan (b), *Binary*. Draai `lighthouse init`.
- [ ] **Stap 4: Bewijs dat de test-gate werkt.** Een commit op de featurebranch draait de suite via `.claude/hooks/lighthouse/pre-commit-tests.sh`. Een payload `git commit` op `main` naar het commando uit `.claude/settings.json` geeft exit 2 (zoals de e2e van (a)).

### Taak K2: Controller en scripts (R4)

- [ ] **Stap 1: Tests eerst.** `test_controller_bouwt_lighthouse_namen`: de starts van `decide.py:989`, `1094`, `1893`, `1977` en de handoff van `triage.py:811` noemen `/lighthouse-*`; `test_allowlist_is_lighthouse`; de controllertests importeren het protocol uit `lighthouse_lib` en lezen een oude `<!-- paia: … -->`-marker nog (Review Focus 2 van (b)). Draai — verwacht: FAIL.
- [ ] **Stap 2: Implementeer.** `ALLOWLIST`, `EIND`, `VOOR_UITVOERING`, de commando's hierboven en `overzicht.py:65` naar `/lighthouse-*`; het controllerbeleid importeert `lighthouse_lib.protocol` (*Inventaris (d)*); `artefacten.py` en het protocoldeel van `comments.py` weg; `triage.py` op de nieuwe marker en tokens; `--setenv=LIGHTHOUSE_CONTROLLER_RUN` naast `PAIA_CONTROLLER_RUN` in `controller_launch.sh:133` en `forge_agent.sh`; `usage_window.py` laadt `.lighthouse/scripts/codex_usage.py`; `roborev.yml:106`; de hernoemde CLI-subcommando's; `review_pair.py tally` blijft als eigen script.
- [ ] **Stap 3: Draai** het testcommando — verwacht: PASS. Commit per soort.

### Taak K3: Checks en pre-push (R4, R5)

- [ ] **Stap 1:** `check-instructies.sh` houdt alleen check 6, 9, 12 en 13, met hun nummers. De tests van de andere checks gaan weg (ze staan in lighthouse). `pre-push-instructielagen.sh` en `skills_upstream_bump.py:1147` draaien ook `.lighthouse/scripts/check-instructions.sh`. Test: `test_pre_push_draait_beide_checks` (`tests/integration/test_pre_push_instructielagen.sh`).
- [ ] **Stap 2: R5.** Voeg aan `AGENTS.md` niets toe buiten het blok. Haal uit het projectdeel de regels die het blok van `pack/core` draagt: worktree vóór de eerste schrijfactie, niets direct op `main`, de ketentabel *Vaste naden* voor zover hij `/lighthouse-*` noemt, en het testcommando onder *Buiten Claude Code*.
- [ ] **Stap 3: Commit** per stap.

### Taak K4: De grep en het bewijs (R6)

- [ ] **Stap 1:** Draai de grep met de elf ketenskills. Elke treffer krijgt een commit: hernoemen naar `/lighthouse-*`, of het pad valt onder ontwerpkeuze 6. Op `c2fd5bb54` staan de treffers buiten de historie in `tests/unit` (40), de skillspiegels (54), `deploy/beelink` (10), `.claude/hooks` (8), `src/controller` (4), `scripts/` (± 12), `.claude/rules` (2), `.claude/agents` (2) en `tests/fixtures` (1).
- [ ] **Stap 2: R6.** Daarna één verse review van de hele branch (opus) en de vraag aan Corneel vóór push en PR.

### Taak K5: Mergen met de controller stil

- [ ] **Stap 1:** Op de Beelink: `systemctl --user stop controller-tick.timer`. Controleer: `systemctl --user is-active controller-tick.service` geeft `inactive` (geen tick halverwege).
- [ ] **Stap 2:** Squash-merge de core-PR. Werk de hoofdcheckout bij (`git -C <paia> pull --ff-only`). Daar moet `lighthouse init --upgrade` exact `No changes.` geven.
- [ ] **Stap 3:** Eén handmatige tick: `systemctl --user start controller-tick.service`. Controleer met `/paia-controller` dat geen issue `skill niet geïnstalleerd` of `commando niet toegestaan` kreeg, tenzij het in een worktree van vóór de merge staat (Review Focus 1).
- [ ] **Stap 4:** `systemctl --user start controller-tick.timer`.

## PR's expo, python, shell (paia)

### Taak T1: Taalpakket `<p>` (R1–R6)

Per pakket één PR, na de core-PR. `lighthouse init --upgrade --pack <p>` (plan (c), ontwerpkeuze 6).

- [ ] **expo:** weg `paia-refresh-expo-lockfiles` (beide spiegels). R4: `fix-pr` verwijst al naar `/lighthouse-refresh-expo-lockfiles` (core); `apps/CLAUDE.md` en de areas noemen de nieuwe naam. Geen config.
- [ ] **python:** weg `paia-python-schrijven`, `.claude/hooks/ruff-format.sh`, `ty-check.sh` met hun registraties, `scripts/quality/` voor zover verhuisd. R2: de `[python]`-sleutels uit *Inventaris (d)*. Het fragment levert `[test_gate]`, en `init` laat paia's eigen `command` staan (plan (c), ontwerpkeuze 4). R4: de importeurs van `quality.*` en de workflows uit *Inventaris (d)*. Blijft: plan (c), ontwerpkeuze 14; elke test daarvan blijft groen of gaat weg met reden.
- [ ] **shell:** weg `paia-shell-gates`, `.claude/hooks/shellcheck.sh`, `scripts/hooks/shellcheck-repo.sh`, `scripts/hooks/lib/shellcheck-bin.sh`. R2: `[shell] strict_dirs`. R4: `AGENTS.md` *Buiten Claude Code*, `workflow-audit.sh` (blijft, eigen pin, besluit 17), `tests/integration/lib/gate-assert.sh`.
- [ ] **Elke PR:** de grep met de ketenskills tot en met dit pakket; R6; review (opus); vraag aan Corneel vóór push en PR. Elke paia-PR noemt `Refs CorneeldH/lighthouse#9`, en geen enkele sluit #9: dat gebeurt na de DoD hieronder.

---

## Na de laatste PR

- [ ] **DoD op de Mac.** Een andere eigen repo: `go install …@<tag>`, `lighthouse init --yes`, featurebranch en `main`. Een commit op `main` via Claude Code wordt geblokkeerd door `.claude/hooks/lighthouse/main-commit-guard.sh`. Zet het bewijs (commando's en uitvoer) als comment op #9.
- [ ] **De vier DoD-punten van de pitch** nalopen op #1, met per punt het bewijs.
- [ ] Zet in #9 onder *Verbeterpunten* wat tijdens de overstap opkwam. De aliassen voor `/paia-*` gaan eruit in M2.
- [ ] Sluit #9 met `/lighthouse-close-issue 9`, na akkoord van Corneel.
