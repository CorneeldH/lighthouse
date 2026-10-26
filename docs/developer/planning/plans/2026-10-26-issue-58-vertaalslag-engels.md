# Vertaalslag naar het Engels en de codetaal-hook aan — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** alle namen, commentaar en docstrings in de code van lighthouse Engels maken, en daarna in
dezelfde PR `code_language = "en"` aanzetten met een test die het zo houdt (issue #58).

**Architectuur:** een vertaling zonder gedragswijziging, per module uitgevoerd: bron en bijbehorende
test samen in één taak, omdat hernoemde namen ook in de test terugkomen. De detector van #37
(`pack/core/lib/code_language.py`) is de meetlat per taak. Pas de laatste taak zet de config aan,
zodat de hook tijdens het vertalen niet op elk half vertaald bestand blokkeert.

**Tech stack:** Python 3 (stdlib, pytest via `uv`), Bash, Go.

**Spec:** geen eigen spec. Het issue #58 is de opdracht; de context staat in
[de spec van de detector](../specs/2026-10-25-codetaal-detector-design.md), sectie *Vervolg*.

## Nulmeting (2026-10-26, `origin/main` 786176a)

De detector met `code_language = "en"`, paden `pack/**`, `internal/**`, `cmd/**`, `tests/**`,
`tools/**` en uitzondering `tests/pack/fixtures/code_language/**`:

- **2137 vondsten in 65 bestanden**: 480 commentaar, 711 docstring, 946 namen. 137 twijfels
  (geen vondst; laag 2 beslist, buiten deze PR).
- Per map: `tests/pack` 1052, `pack/core` 922, `pack/python` 139, `pack/shell` 22,
  `internal/initcmd` 2. `cmd/`, `tools/` en `.lighthouse/` (beheerde kopieën) 0.
- Daarnaast staat `besluit N` en `ontwerpkeuze N` in commentaar dat de detector alleen als twijfel
  ziet (Go in `internal/` en `pack/pack.go`, hookkoppen, `pack/core/lib/*.py`). Het issue vraagt
  ook die weg.
- Geen Nederlandse bestandsnamen; er hoeft geen bestand te verhuizen.

## Global Constraints

- **Geen gedragswijziging.** Stringliterals blijven byte-gelijk: uitvoer, foutmeldingen, JSON- en
  TOML-sleutels, CLI-vlaggen, namen van omgevingsvariabelen, de Nederlandse kopvarianten
  (`headings.toml`), tokens als `COULD NOT CHECK`. Alleen namen, commentaar en docstrings
  veranderen.
- **Alleen de bron onder `pack/` bewerken**, nooit de geïnstalleerde kopie
  (`.lighthouse/lib/`, `.lighthouse/scripts/`, `.claude/hooks/lighthouse/`, `.claude/skills/`,
  `.agents/skills/`). Elke taak die `pack/` raakt draait `go run ./cmd/lighthouse init --upgrade`
  en commit de geïnstalleerde kopie in dezelfde commit.
- **Een naam hernoemen = alle verwijzingen mee.** Na elke hernoeming:
  `git grep -nw '<oude naam>' -- pack internal cmd tests tools .github` geeft nul treffers buiten
  `docs/` (docs blijven Nederlands en historisch; niet aanpassen).
- **`besluit N` wordt `decision N`, `ontwerpkeuze N` wordt `design choice N`**; de verwijzing naar
  spec of plan blijft staan.
- **Een Engelse naam die de detector toch als Nederlands ziet** (bijv. `_AUTOLINK`, `_killpg`):
  eerst een duidelijker Engelse naam als die er is; anders op die regel
  `# lighthouse: allow-language — <reden>` (in Go `//`). De markering kan geen module- of mapnaam
  vrijgeven.
- **Gelaten:** `docs/`, `tests/pack/fixtures/code_language/**` (vendored stdlib en de bewust
  Nederlandse `bash_writes_nl.py`), Nederlandse stringliterals, de detectorlogica en
  `code_language_freq.tsv.gz`.
- **Engels in de code, Nederlands in de artefacten:** commitberichten en de PR-tekst zijn
  Nederlands.
- Go-wijzigingen volgen `/lighthouse-go-writing`; `gofmt` en `go-check` lopen als gate.

## Gereedschap voor elke taak

Twee hulpscripts, niet gecommit. Zet ze in de scratchpad van de sessie (hieronder `$S`).

**`$S/measure.py`** — vondsten van de detector, zonder dat de config al aan staat:

```python
"""Detector findings for lighthouse's code, with code_language patched on."""
import subprocess
import sys
from pathlib import Path

root = Path(sys.argv[1]).resolve()
prefixes = tuple(sys.argv[2:]) or ("pack/", "internal/", "cmd/", "tests/", "tools/")
sys.path.insert(0, str(root / ".lighthouse/lib"))
from lighthouse_lib import code_language as cl, config  # noqa: E402

real = config.lookup
CFG = {
    "project.code_language": "en",
    "project.code_language_paths": ["pack/**", "internal/**", "cmd/**", "tests/**", "tools/**"],
    "project.code_language_exempt": ["tests/pack/fixtures/code_language/**"],
}
config.lookup = lambda cfg, key: CFG[key] if key in CFG else real(cfg, key)
files = subprocess.run(["git", "-C", root, "ls-files"], capture_output=True, text=True).stdout.split()
total = 0
for rel in files:
    if rel.startswith(prefixes) and (root / rel).is_file():
        for f in cl.check_file(root / rel, root).findings:
            total += 1
            print(f"{rel}:{f.line}: {f.kind}: {f.text[:100]}")
print("total", total)
```

Draai met `python3 $S/measure.py <worktree> <pad> [<pad> ...]`. Let op: het laadt de detector uit
`.lighthouse/lib`, de geïnstalleerde kopie; dat is de stand van `origin/main` tot Taak 11.

**`$S/literals.py`** — bewijst dat geen stringliteral in een Python-bestand veranderde:

```python
"""Exit 1 when the non-docstring string constants of a .py file differ from origin/main."""
import ast
import collections
import subprocess
import sys


def strings(source: str) -> collections.Counter:
    tree = ast.parse(source)
    docs = set()
    for node in ast.walk(tree):
        if isinstance(node, (ast.Module, ast.ClassDef, ast.FunctionDef, ast.AsyncFunctionDef)):
            body = node.body
            if body and isinstance(body[0], ast.Expr) and isinstance(body[0].value, ast.Constant):
                docs.add(id(body[0].value))
    return collections.Counter(
        n.value for n in ast.walk(tree)
        if isinstance(n, ast.Constant) and isinstance(n.value, str) and id(n) not in docs
    )


bad = 0
for path in sys.argv[1:]:
    old = subprocess.run(["git", "show", f"origin/main:{path}"], capture_output=True, text=True).stdout
    with open(path, encoding="utf-8") as f:
        new = f.read()
    if strings(old) != strings(new):
        bad = 1
        print(path, "removed:", list((strings(old) - strings(new)).elements())[:5],
              "added:", list((strings(new) - strings(old)).elements())[:5])
sys.exit(bad)
```

Draai vanuit de worktree: `python3 $S/literals.py <bestand.py> ...`. Een hernoemde parameter die
als `monkeypatch.setattr(mod, "<naam>", …)` of `getattr(x, "<naam>")` in een test staat, verschijnt
hier als verschil: dat is dan een bewuste, verklaarbare wijziging, die je in de commit noemt.

## Werkwijze per vertaaltaak (Taak 1 t/m 10)

Elke vertaaltaak heeft dezelfde vijf stappen; per taak staan alleen de bestanden en de bijzonderheden.

1. **Meet rood:** `python3 $S/measure.py <worktree> <bestanden van de taak>` — verwacht het getal
   uit de taak.
2. **Vertaal** commentaar, docstrings en namen (functies, klassen, parameters, lokale variabelen,
   attributen, shellvariabelen) naar idiomatisch Engels. Geen stringliterals. Houd de toon en
   dichtheid van het commentaar gelijk; niet inkorten, niet uitbreiden.
3. **Meet groen:** dezelfde `measure.py`-aanroep geeft `total 0`. `literals.py` over de gewijzigde
   `.py`-bestanden geeft exit 0, of alleen de verklaarde verschillen uit stap 2.
4. **Test gate en dogfood:** `uv run pytest -q && go test -race -shuffle=on ./...` groen;
   `go run ./cmd/lighthouse init --upgrade` en de geïnstalleerde kopie meestagen.
5. **Commit**, één per taak, bijv. `refactor(core): Engelse namen en commentaar in evidence (#58)`.

---

### Taak 1: `evidence`

**Bestanden:** `pack/core/lib/evidence.py` (89), `tests/pack/test_evidence.py` (38). Verwacht 127.

- Veel namen (`GIT_LOG_ALLEEN_GELIJK`, `MAX_UITVOER`, `_draai`, `_gh_bereikbaar`, `uitkomst`).
  Check per hernoemde module-naam ook `pack/core/lib/pr_body_gate.py` en `pack/core/scripts/` op
  aanroepers.
- [ ] Stap 1–5 van de werkwijze.

### Taak 2: `pr_body_gate`

**Bestanden:** `pack/core/lib/pr_body_gate.py` (141), `tests/pack/test_pr_body_gate.py` (110),
`tests/pack/test_pr_create_gate.py` (29). Verwacht 280.

- `besluit, reden = in_scope(...)` met waarden `"overslaan"`/`"blokkeren"` (r. 682–685): de namen
  worden `decision, reason`; de stringwaarden blijven, want `in_scope` levert ze en tests vergelijken
  ze.
- `pack/core/hooks/pr-create-gate.sh` roept de module aan; controleer daar functienamen.
- [ ] Stap 1–5 van de werkwijze.

### Taak 3: `check-instructions`

**Bestanden:** `pack/core/scripts/check-instructions.sh` (106),
`tests/pack/test_check_instructions.py` (157). Verwacht 263.

- Shellvariabelen (`gevonden`, `regels`, `settings_kapot`, `STOPWOORDEN`, `PLAFOND`) en functies
  (`check_spiegel_inhoud`, `check_schaduw`, `check_dubbele_naam`, `check_byte_identiek`).
  Controleer of een functie- of variabelenaam in uitvoer, in een skill of in een test als string
  voorkomt (`git grep -nw`); zo ja, staat hij in een literal en blijft die literal staan.
- [ ] Stap 1–5 van de werkwijze.

### Taak 4: schrijfpoorten en git-aanroepen

**Bestanden:** `pack/core/lib/bash_writes.py` (81), `pack/core/lib/git_invocations.py` (60),
`pack/core/lib/no_verify.py` (7), `tests/pack/test_bash_writes.py` (34),
`tests/pack/test_bash_write_gates.py` (38), `tests/pack/test_git_invocations.py` (8),
`tests/pack/test_pre_commit_no_verify.py` (12), `tests/pack/test_main_commit_guard.py` (95),
`tests/pack/test_pre_commit_tests.py` (35). Verwacht 370.

- `bash_writes.py` heeft Nederlandse docstrings en twee `besluit`-verwijzingen (r. 11, 832, 856,
  921); `classify` leest het token `COULD NOT CHECK` — literal, blijft.
- [ ] Stap 1–5 van de werkwijze.

### Taak 5: kleine bibliotheekmodules en de testkit

**Bestanden:** `pack/core/lib/deadline.py` (13), `pack/core/lib/md_links.py` (10),
`pack/core/lib/gate_slot.py` (3), `pack/core/lib/protocol.py` (1),
`pack/core/lib/code_language.py` (1), `tests/pack/_testkit.py` (7), `tests/pack/test_deadline.py` (9),
`tests/pack/test_md_links.py` (2), `tests/pack/test_gate_slot.py` (4),
`tests/pack/test_worktree_linkcheck.py` (12), `tests/pack/test_worktree_guard.py` (2),
`tests/pack/test_hooks_minimal_config.py` (8). Verwacht 72.

- `code_language.py:397` (`_AUTOLINK`) en `deadline.py:88` (`_killpg`) zijn Engelse namen die de
  detector als Nederlands ziet: kies een andere Engelse naam of zet de markering met reden.
- `_testkit.py` wordt door bijna elke test geïmporteerd: hernoem daar alleen wat de detector meldt,
  en draai de volledige `pytest`.
- [ ] Stap 1–5 van de werkwijze.

### Taak 6: `worktree_audit` en `codex_usage`

**Bestanden:** `pack/core/scripts/worktree_audit.py` (71), `tests/pack/test_worktree_audit.py` (78),
`pack/core/scripts/codex_usage.py` (64), `tests/pack/test_codex_usage.py` (72). Verwacht 285.

- `codex_usage.py:370` `def uitkomst(besluit, reden, **extra) -> BudgetUitkomst`: de velden van
  `BudgetUitkomst` kunnen als JSON-sleutels naar buiten gaan. Hernoem een veld alleen als de
  uitvoer de sleutel als literal houdt; anders blijft het veld met de markering en de reden
  "JSON-sleutel van de uitvoer".
- [ ] Stap 1–5 van de werkwijze.

### Taak 7: artefacten en review-records

**Bestanden:** `pack/core/scripts/post_artefact.py` (60), `tests/pack/test_post_artefact.py` (69),
`pack/core/scripts/plan_review_record.py` (45), `tests/pack/test_plan_review_record.py` (42),
`pack/core/scripts/review_pair.py` (13), `tests/pack/test_review_pair.py` (4). Verwacht 233.

- `plan_review_record.py:63-64` noemt JSON-sleutels (`bestand`, `besluit`, `reden`, `drempels`) in
  een docstring: dat zijn sleutels van bestaande records op schijf; ze blijven in de literals staan
  en de vertaalde docstring noemt ze letterlijk tussen backticks.
- [ ] Stap 1–5 van de werkwijze.

### Taak 8: board, KPI-metingen en blokkades

**Bestanden:** `pack/core/scripts/measure_start_issue_kpi.py` (67),
`tests/pack/test_start_issue_kpi.py` (28), `pack/core/scripts/measure_fix_pr_kpi.py` (19),
`tests/pack/test_fix_pr_kpi.py` (16), `pack/core/scripts/board_start.py` (22),
`tests/pack/test_board_start.py` (27), `pack/core/scripts/unblock_dependents.py` (49),
`tests/pack/test_unblock_dependents.py` (34), `tests/pack/test_issue_impact_scout.py` (14),
`tests/pack/test_repo_identity.py` (10), `tests/pack/test_testing_skill.py` (1). Verwacht 287.

- De KPI-scripts staan als commando in skills (`measure_start_issue_kpi.py leadin`); subcommando's
  zijn literals en blijven.
- [ ] Stap 1–5 van de werkwijze.

### Taak 9: pakket python

**Bestanden:** `pack/python/scripts/quality/ratchet.py` (51), `prcov_issues.py` (45),
`baseline.py` (14), `diffcov.py` (14), `prcov.py` (11), `gitenv.py` (3),
`pack/python/hooks/ty-check.sh` (1), `tests/pack/python/test_ratchet.py` (40),
`tests/pack/python/test_ratchet_nightly.py` (12), `tests/pack/python/test_prcov.py` (4).
Verwacht 195.

- `prcov_issues.py:309` noemt sleutels die de controller leest: literals, blijven.
- [ ] Stap 1–5 van de werkwijze.

### Taak 10: pakket shell

**Bestanden:** `pack/shell/hooks/shellcheck.sh` (9), `pack/shell/scripts/lib/shellcheck-bin.sh` (9),
`pack/shell/scripts/shellcheck-repo.sh` (4), `tests/pack/shell/test_shellcheck.py` (1). Verwacht 23.

- `lib/shellcheck-bin.sh` wordt gesourced: een variabele of functie die de aanroeper leest, hernoem
  je in beide bestanden tegelijk. Een omgevingsvariabele die van buiten komt, blijft.
- [ ] Stap 1–5 van de werkwijze.

### Taak 11: `besluit` en `ontwerpkeuze`, en de oude lijst weg

Deze woorden geven bijna nooit een vondst (alleen twijfel), dus `measure.py` is hier niet de
meetlat; `git grep` is dat wel.

**Bestanden:** de treffers van
`git grep -nwiE 'ontwerpkeuzes?|besluit' -- pack internal cmd tests '*.go' '*.py' '*.sh'`
(nu o.a. `internal/initcmd/{initcmd,select,workflows,workspace,initcmd_test}.go`,
`internal/packs/{packs,packs_test}.go`, `internal/projectconfig/projectconfig.go`,
`internal/version/version.go`, `internal/reconcile/reconcile.go`, `pack/pack.go`, de hookkoppen
in `pack/core/hooks/` en `pack/python/hooks/ruff-format.sh`, `pack/core/lib/{config,artefacts,protocol,headings,datadir,code_language}.py`,
en wat Taak 1–10 nog liet staan), plus `tests/pack/test_code_language_corpus.py`.

- [ ] **Stap 1: Meet rood** — de `git grep` hierboven geeft treffers.
- [ ] **Stap 2: Vertaal** `besluit N` → `decision N`, `ontwerpkeuze N` → `design choice N`
  (`Besluit` aan het begin van een zin → `Decision`). Stringliterals met die woorden
  (bijv. `"besluit"` als JSON-sleutel in `plan_review_record.py`) blijven staan; de `git grep`
  telt die ene categorie niet mee.
- [ ] **Stap 3: Haal de lijst weg** uit `tests/pack/test_code_language_corpus.py`: `KNOWN_DUTCH`,
  `_dutch_words`, `_unknown` en `test_known_dutch_still_occurs` gaan eruit;
  `test_go_english`, `test_config_py_english` en `test_hooks_english` asserten dan
  `[f for f in findings if not f.doubt] == []`.
- [ ] **Stap 4: Meet groen** — de `git grep` geeft alleen nog stringliterals; test gate groen;
  `init --upgrade` en de kopie meestagen.
- [ ] **Stap 5: Commit** — `refactor: decision en design choice in plaats van besluit en ontwerpkeuze (#58)`.

### Taak 12: de hook aan, en een test die het zo houdt

**Bestanden:**
- Wijzig: `lighthouse.toml` (`[project]`)
- Wijzig: `tests/pack/test_code_language_corpus.py`

- [ ] **Stap 1: Schrijf de test** in `tests/pack/test_code_language_corpus.py` (voeg
  `import subprocess` toe):

```python
def test_lighthouse_code_english():
    """The whole repo, as the hook sees it: lighthouse.toml decides scope and exemptions."""
    cfg = cl.config.load(REPO / "lighthouse.toml")
    assert cl.config.lookup(cfg, "project.code_language") == "en"
    assert {"pack/**", "internal/**", "cmd/**"} <= set(cl.config.lookup(cfg, "project.code_language_paths"))
    files = subprocess.run(
        ["git", "-C", REPO, "ls-files"], capture_output=True, text=True, check=True
    ).stdout.split()
    found = [
        f"{rel}:{f.line}: {f.kind}: {f.text}"
        for rel in files
        if (REPO / rel).is_file()
        for f in cl.check_file(REPO / rel, REPO).findings
    ]
    assert not found, "\n".join(found)
```

  `check_file` slaat zelf over wat buiten de scope, onder een uitzondering of in het manifest
  staat. Twijfels tellen niet (`.findings`, niet `.doubts`); er is geen `session_toml`, dus laag 2
  draait niet.

- [ ] **Stap 2: Draai de test** — `uv run pytest tests/pack/test_code_language_corpus.py -q`.
  Verwacht: FAIL op `KeyError` of de `code_language`-assert, want de config staat nog uit.
- [ ] **Stap 3: Zet de config aan** in `lighthouse.toml`, onder `[project]`:

```toml
code_language = "en"
code_language_paths = ["pack/**", "internal/**", "cmd/**", "tests/**", "tools/**"]
code_language_exempt = ["tests/pack/fixtures/code_language/**"]
```

- [ ] **Stap 4: Draai de test** — PASS. Noemt hij nog een vondst (bijv. een bestand dat na de
  nulmeting op `main` kwam), vertaal die dan in de bron onder `pack/` en draai opnieuw.
- [ ] **Stap 5: Mutatiecheck** — zet tijdelijk `# lees het bestand en tel de regels` boven een
  functie in `pack/core/lib/md_links.py`; de test moet falen met die regel in de melding. Draai
  ook de hook: `printf '{"tool_input":{"file_path":"%s"}}' "$PWD/pack/core/lib/md_links.py" | bash .claude/hooks/lighthouse/code-language.sh; echo rc=$?`
  geeft `rc=2`. Zet de regel terug.
- [ ] **Stap 6: Volledige controle** — `uv run pytest -q && go test -race -shuffle=on ./...` groen;
  `go run ./cmd/lighthouse init --upgrade` geeft `No changes.`; `python3 $S/measure.py <worktree>`
  geeft `total 0`.
- [ ] **Stap 7: Commit** — `feat(config): codetaal Engels aan voor lighthouse zelf (#58)`.

## Review Focus

- **Een hernoemde parameter die een aanroeper als keyword meegeeft** buiten de tests (een hook,
  een ander script): `TypeError` pas op het moment dat de hook draait. Taak-stap 2 met de
  `git grep -nw` op elke hernoemde parameter vangt het; de reviewer controleert dat per taak.
- **Veldnamen die naar buiten lekken** (`dataclasses.asdict`, `vars()`, `_asdict()`, `**kwargs`
  uit een dict): een hernoemd veld verandert dan JSON-uitvoer of bestaande records. `literals.py`
  ziet dit niet; de reviewer zoekt per taak naar `asdict`/`vars(`/`_asdict` in de gewijzigde
  bestanden.
- **Gesourcede shell**: een variabele uit `lib/*.sh` die de aanroeper leest, of die een gebruiker
  als omgevingsvariabele zet. Taak 10 en Taak 3 noemen het; de reviewer leest per hernoemde
  shellvariabele waar hij gelezen wordt.
- **Per ongeluk vertaalde literal** (een melding, een kopvariant, een token): `literals.py` vangt
  het voor Python; voor shell bekijkt de reviewer de diff op regels met `echo`/`printf`.
- **Hook na het aanzetten**: elke latere edit van een `.py`-, `.sh`- of `.go`-bestand in lighthouse
  wordt gecontroleerd. Taak 12 stap 5 bewijst dat de hook blokkeert op Nederlands, en stap 4 dat
  de schone tree geen vals alarm geeft.
