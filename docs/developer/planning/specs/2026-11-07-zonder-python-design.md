# Ontwerp: Lighthouse zonder Python

Milestone: *Lighthouse zonder Python* (nieuw) · Datum: 2026-11-07 · Bouwt op M0 (besluit 1 van
[de M0-spec](2026-10-17-m0-pakket-design.md): "Herbouw in Go komt in M1/M2") en gaat vóór het
Windows-issue van [M3](2026-11-07-m3-tweede-omgeving-design.md) (#90).

## Aanleiding en doel

In M0 ging de Python van paia als tussenstap mee in `pack/core`, omdat herbouwen in Go toen te
groot was. Die herbouw is er niet gekomen. Daardoor heeft elke host nog `python3` ≥ 3.11 nodig
(`tomllib`), naast de lighthouse-binary. De M3-spec moet die eis per OS uitleggen. Ook het
Windows-werk wordt er zwaarder door: een hook is daar bash plus Python onder Git Bash.

**Doel:** lighthouse draait op een host zonder Python. Alles wat `lighthouse init` installeert
roept de binary aan, niet `python3` en niet een eigen bash-hook met logica.

**Klaar als:** een verse `lighthouse init` met de pakketten `core`, `go`, `node`, `shell` en
`expo` werkt op een host zonder `python3` op het PATH. Dat betekent: de hooks geven hetzelfde
oordeel als nu op dezelfde payloads, en elke skill-opdracht en elk script draait. Een CI-job
bewijst dat (zie *Bewijs*).

## Doelen, niet-doelen, anti-doelen

**Doelen**

- Elke hook van `core` wordt helemaal Go: `lighthouse hook <naam> [stand]`.
- Elk script en elke lib-aanroep die een skill doet, wordt een subcommando van de binary.
- De taalpakketten `go`, `node`, `shell` en `expo` hebben geen Python meer nodig.
- De binary roept zelf geen `python3` meer aan (`internal/initcmd/workflows.go`,
  `internal/deps`).
- `init --upgrade` ruimt `.lighthouse/lib/` en `.lighthouse/scripts/` op in elke repo.

**Niet-doelen**

- `pack/python` blijft Python. Dat is het taalpakket voor Python-repo's, en daar is Python er
  toch al. Alleen zijn aanroepen van de core-lib gaan over op de binary (§5).
- Python in deze repo die niet wordt geïnstalleerd: `tests/` (pytest mag blijven), `tools/`,
  `ci/` en de site-tests.
- De hooks van `pack/go`, `pack/shell` en `pack/node` die al geen Python gebruiken
  (`gofmt-check`, `go-lint-gate`). Die blijven bash. Ze gebruiken wel `config.py`; die aanroep
  gaat over (zie *Taalpakketten*).
- Nieuwe checks of ander gedrag. Een hook geeft na de ombouw hetzelfde oordeel als ervoor.

**Anti-doelen.** Dit telt als mislukt, ook als het technisch werkt:

- Een gate die stil verdwijnt. Kan een hook niet checken (geen binary, kapotte config), dan zegt
  hij `COULD NOT CHECK`, zoals nu.
- Een tijdelijke stand waarin dezelfde logica in Go én in Python staat en beide worden
  onderhouden. Elke PR zet een module over en haalt de Python-versie in dezelfde PR weg.
  `internal/protocol` en `protocol.py` zijn nu al zo'n tweeling; die verdwijnt in deze milestone.
- Een host die na een upgrade van de binary met andere gates werkt dan de repo verwacht, zonder
  dat iemand het ziet.

## Inventaris

| Wat | Waar | Omvang | Wie roept het aan |
|---|---|---|---|
| Hook-libs | `pack/core/lib/*.py` | 21 modules, ~10k regels | 8 hooks, skills |
| Skill-scripts | `pack/core/scripts/*.py` | 11 scripts, ~5.9k regels | 10 skills, 1 workflow |
| Shell-scripts met Python erin | `pack/core/skills/review-spec/review.sh` (3 inline `python3 -` + `codex_usage.py`), `pack/core/scripts/check-instructions.sh` (`python3 -c`, `config.py`) | 2 scripts | `review-spec`, `review-plan`; CI en skills |
| Hook-hulpjes | `pack/core/hooks/{permission-check,usage-gate}/*.py` | 3 bestanden | 2 hooks |
| Core-hooks (bash) | `pack/core/hooks/*.sh` | 9 hooks, ~2k regels; `commit-test-gate.sh` alleen al 922 | Claude Code |
| Config uit skills | `python3 .lighthouse/lib/lighthouse_lib/config.py <key>` | ~200 aanroepen in SKILL.md's | skills van alle pakketten |
| Gates van taalpakketten | `pack/go/scripts/gate_go.py`, `pack/node/scripts/gate_node.py` | 335 regels | `[test_gate] command` |
| Workflow | `pack/core/workflows/unblock-dependents.yml` | `uv run … python unblock_dependents.py` | GitHub-runner |
| Binary | `internal/initcmd/workflows.go:117`, `internal/deps/deps.go:55` | 2 plekken | `init` |

`pack/*/requires.toml` houdt per unit bij welke tool hij nodig heeft. Nu staat `python3` bij
bijna elke unit van `core` en bij drie van `go`.

## Aanpakken

| | Aanpak | Wint | Kost |
|---|---|---|---|
| A | De bash-hooks blijven; `python3 "$LIB/x.py"` wordt `lighthouse lib x` | Kleinste stap per PR | Bash en Go blijven naast elkaar; het Windows-werk blijft even groot |
| **B** | **Elke hook helemaal naar Go: `lighthouse hook <naam>`; de bash verdwijnt** | **Eén taal, één testsuite; lost ook Git Bash op Windows grotendeels op** | **`commit-test-gate` (922 regels bash met `systemd-run`, slots, deadline) moet mee** |
| C | Per hook kiezen: dunne hooks naar B, dikke naar A | Minder werk dan B | De dikke hooks, en dus het moeilijkste Windows-werk, blijven bash |

**Gekozen: B** (gebruiker, 2026-11-07). C laat precies het Windows-knelpunt staan, en B maakt het
Windows-issue van M3 kleiner.

## Ontwerp

### 1. De binary is verplicht (besluit gebruiker)

Wie in een lighthouse-repo werkt, heeft de binary op het PATH. Hij installeert die met de
release-download of bouwt hem met `go install github.com/CorneeldH/lighthouse/cmd/lighthouse@<versie>`.
Ontbreekt de binary, dan blokkeert een hook niet maar meldt hij:

```
COULD NOT CHECK: lighthouse not on PATH — install v<versie>: go install github.com/CorneeldH/lighthouse/cmd/lighthouse@v<versie>, or download it from the release
```

`<versie>` komt uit `.lighthouse/manifest.toml`.

### 2. Hoe Claude Code een hook aanroept

`init` registreert per hook één opdracht in `.claude/settings.json`:

```
.claude/hooks/lighthouse/run.sh <naam> [stand]
```

`run.sh` is het enige stuk shell dat overblijft: POSIX `sh`, rond de tien regels. Het zoekt
`lighthouse` op het PATH. Ontbreekt die, dan print het de melding van §1 en sluit het af met 0.
Anders doet het `exec lighthouse hook "$@"`. Zo'n vast stuk is nodig, want zonder binary geeft een
kale `lighthouse hook …` in `settings.json` alleen een hookfout met `command not found`, zonder
uitleg hoe je de binary krijgt. *(Mijn keuze; een inline `command -v … ||` in de registratie kan ook, maar is
slechter te testen.)*

`lighthouse hook <naam>` leest de payload van stdin, zoals de bash-hooks nu. Het houdt het
contract van Claude Code aan: exit 2 met de reden op stderr blokkeert, JSON op stdout
(`additionalContext`, `systemMessage`, `decision`) werkt zoals nu. Elke hook geeft exact hetzelfde
oordeel en dezelfde tekst als zijn bash-voorganger, op dezelfde payloads.

### 3. Versies van binary en repo

De logica zit in de binary. Wat de repo installeert, is alleen nog registraties, `run.sh` en
tekst (skills, AGENTS.md). Een andere binary-versie dan in de manifest is daarom geen reden om
niet te checken. De hook draait gewoon, en zet een `systemMessage` met
`lighthouse v<binary> ≠ v<manifest> in this repo — run lighthouse init --upgrade`, één keer per
sessie. *(Mijn keuze. Dit verandert wat ik eerder voorstelde, namelijk COULD NOT CHECK bij een
verschil: dan zou elke upgrade van de binary alle gates stilzetten tot `init --upgrade`, en dat
botst met het eerste anti-doel.)*

Een hook die de binary niet kent (registratie nieuwer dan de binary) geeft `COULD NOT CHECK:
unknown hook <naam> — update lighthouse to v<manifest>`.

### 4. Subcommando's voor skills

Elke aanroep van `python3 .lighthouse/…` in een skill wordt een subcommando. De naam volgt het
script, de vlaggen en uitvoer blijven gelijk, zodat de skilltekst alleen het commando verandert:

| Nu | Straks |
|---|---|
| `python3 .lighthouse/lib/lighthouse_lib/config.py <key>` | `lighthouse config <key>` |
| `python3 .lighthouse/lib/lighthouse_lib/headings.py <key>` | `lighthouse headings <key>` |
| `python3 .lighthouse/lib/lighthouse_lib/evidence.py run …` | `lighthouse evidence run …` |
| `python3 .lighthouse/scripts/<script>.py …` | `lighthouse <script met streepjes> …`, bijvoorbeeld `lighthouse worktree-audit` |
| `python3 .lighthouse/scripts/gate_go.py -- …` | `lighthouse gate go -- …` |
| `.lighthouse/scripts/check-instructions.sh` | `lighthouse check-instructions` |
| `.claude/skills/lighthouse-review-spec/review.sh` | `lighthouse review-run` (de hele runner, ook de inline Python) |

`lighthouse config` geeft dezelfde exitcodes als `config.py`: 0 met waarde, 1 als de sleutel niet
gezet is, 2 bij een fout. Skills leunen op dat onderscheid.

### 5. Taalpakketten

- `gate_go.py` en `gate_node.py` worden `lighthouse gate go` en `lighthouse gate node`. In
  `lighthouse.toml` van deze repo en in de fragmenten van de pakketten verandert
  `[test_gate] command` mee. `init --upgrade` herschrijft een `command` van een gebruiker niet;
  het meldt het oude commando, zoals nu bij sleutels die blijven staan.
- `go-lint-gate.sh`, `shell-lint-check.sh` en `shellcheck-repo.sh` lezen hun config met
  `lighthouse config`. `shell-lint-check.sh` leest de payload met `jq` of `python3`; dat wordt
  alleen `jq`, en zonder `jq` meldt hij `COULD NOT CHECK`. *(Mijn keuze: deze hooks blijven
  bash, want ze roepen vooral een linter aan.)*
- `pack/python` gebruikt de core-lib op vier plekken: `config.py` in skills en hooks,
  `turn_roots.py` in `ty-gate.sh`, `config`, `headings` en `protocol` als import in
  `quality/prcov_issues.py`, en `config` in `quality/ratchet.py`. Die roepen voortaan de binary
  aan: `lighthouse config`, `lighthouse headings`, `lighthouse protocol …` en `lighthouse
  turn-roots` (besluit gebruiker). Zo staat elke regel logica één keer in Go. De rest van
  `pack/python` (`gate_python.py`, `quality/`) blijft Python. De PR die een module weghaalt, zet
  ook de aanroep in `pack/python` om.

### 6. De workflow op GitHub

`unblock-dependents.yml` downloadt de release-binary van de versie in de manifest
(`gh release download` met `GH_TOKEN`) en draait `lighthouse unblock-dependents --issue
"$ISSUE"`. `uv` en Python verdwijnen uit de workflow. Bij een `dev`-versie (deze repo) bouwt de
workflow de binary uit de checkout met `go build`. *(Mijn keuze.)*

### 7. De binary zelf

- `internal/initcmd/workflows.go` vult de workflow-sleutels met de Go-configlezer
  (`internal/repoconfig`) in plaats van `python3 -m lighthouse_lib.config`.
- `internal/deps` controleert `python3` niet meer voor `core`; `requires.toml` van `core`, `go`,
  `node`, `shell` en `expo` noemt `python3` niet meer. `pack/python` houdt het.
- `internal/protocol` wordt de enige lezer van het protocol; `protocol.py` verdwijnt.

### 8. Opruimen in repo's

Na de laatste PR installeert `pack/core` niets meer onder `.lighthouse/lib/` en
`.lighthouse/scripts/`. `init --upgrade` haalt die bestanden weg via de manifest (de
`reconcile.Remove`-route bestaat al). Een lokaal gewijzigd bestand blijft staan en wordt gemeld,
zoals nu. `pack/python` installeert zijn eigen scripts nog wel onder `.lighthouse/scripts/`.

### 9. Tests

- Een module die naar Go gaat, neemt zijn unittests mee. De Go-tests lezen dezelfde fixtures in
  `tests/pack/fixtures/`, zodat het oordeel aantoonbaar gelijk blijft.
- De black-box-tests van de hooks (`tests/pack/test_*_gate.py`, `test_*_check.py`) blijven pytest.
  Ze roepen voortaan `run.sh` aan, met een binary die de testsuite één keer bouwt. Zo bewijzen
  dezelfde tests dat het gedrag van voor en na de ombouw gelijk is. *(Mijn keuze.)*
- `test_*_skill.py` (tests op de tekst van SKILL.md) blijven pytest en lopen mee met de nieuwe
  commando's.
- Pas Go-code aan volgens `/lighthouse-go-standard` en tests volgens `/lighthouse-test-standard`.

### 10. Dogfood

Lighthouse draait zijn eigen hooks, dus in deze repo roept `run.sh` de geïnstalleerde
`lighthouse` aan, niet de code van de branch. Een PR die een hook ombouwt, test die hook daarom
via de pytest-tests met de gebouwde binary, niet via de hooks van de eigen sessie. *(Mijn keuze.)*

Deze repo heeft `version = "dev"` in de manifest. Daarvoor gelden twee regels *(mijn keuze)*:

- Bij `dev` geeft §3 geen versiemelding, en §1 noemt geen versie maar
  `go install ./cmd/lighthouse` vanuit de checkout.
- Een PR die een hook ombouwt, zet een nieuwe registratie in de eigen `.claude/settings.json`.
  Tot de volgende release op de host staat, kent de geïnstalleerde binary die hook niet en zegt
  de eigen gate `COULD NOT CHECK: unknown hook`. Dat gat accepteren we: releases gaan vaak, en de
  always-on host werkt zichzelf bij. Wie eerder wil, draait `go install ./cmd/lighthouse` na de
  merge.

Elke PR draait `go run ./cmd/lighthouse init --upgrade` en commit het resultaat, zoals AGENTS.md
voorschrijft.

## Opdeling in issues

Volgorde is afhankelijkheid. Elke PR haalt de Python weg die hij vervangt.

1. **Fundament.** `lighthouse config`, `lighthouse headings`, de hook-runtime (`lighthouse hook`,
   `run.sh`, versiemelding), `lighthouse protocol` voor `prcov_issues.py`, `protocol.py` weg. De skill-aanroepen van `config.py` en `headings.py`
   in alle pakketten gaan in dezelfde PR over (mechanisch, ~200 regels).
2. **Kleine hooks**: `permission-check`, `usage-gate`, `md-link-check`.
3. **`code-language-check`** (met `code_language*.py` en de frequentietabel).
4. **`bash-write-check`** (met `bash_writes.py`, 1.7k regels, en `turn_roots.py`; `ty-gate.sh`
   van `pack/python` gaat mee over op `lighthouse turn-roots`).
5. **`pr-create-gate`** (met `pr_body_gate.py`, `evidence.py`, `artefacts.py`).
6. **`worktree-check` en `main-commit-gate`** (met `git_invocations.py`).
7. **`commit-test-gate`** (met `commit_base`, `deadline`, `gate_slot`, `host_memory`, `no_verify`,
   `repo_identity`). Mag in twee PR's: `pre` en `post`. `systemd-run` blijft achter
   `host.gate_slice`, alleen op Linux.
8. **Skill-scripts, deel 1**: `worktree-audit`, `board-start`, `post-artefact`,
   `unblock-dependents` (+ de workflow, §6).
9. **Skill-scripts, deel 2**: `plan-review-record`, `review-pair`, `roborev-gate`,
   `roborev-comment`, `codex-usage`, en `review.sh` als `lighthouse review-run`.
   `check-instructions.sh` wordt `lighthouse check-instructions`.
10. **KPI-scripts**: `measure-start-issue-kpi`, `measure-fix-pr-kpi`.
11. **Taalpakketten**: `lighthouse gate go|node`, de bash-hooks van §5.
12. **Afronden**: `internal/initcmd`, `internal/deps`, `requires.toml`, opruimen (§8), de CI-job
    van *Bewijs*, en de M3-spec en de installatiegids zonder Python-eis.

Stap 2 tot en met 10 hangen alleen af van stap 1 en kunnen parallel. Stap 12 komt als laatste.

## Bewijs

- Een CI-job (Linux) haalt `python3` uit het PATH, doet een verse `lighthouse init` met `core`,
  `go`, `node`, `shell` en `expo`, en voert elke hook uit met de payloads van `tests/pack`. Er
  verschijnt nergens `COULD NOT CHECK` of `command not found`.
- `grep -rn python3` op de geïnstalleerde bestanden van die init geeft niets.
- `windows-measure.yml` draait opnieuw. De uitkomst gaat als meting naar #90, niet als gate.

## Gevolgen voor M3

De M3-spec gaat uit van Python ≥ 3.11 in de installatiegids (§5) en noemt "Python op Windows" als
risico. Na deze milestone vervalt dat: de gids noemt `gh`, `jq`, `git` en de binary. Stap 12
past de M3-spec daarop aan. Het Windows-issue begint pas na deze milestone.

## Aan de bouwer

- De Go-pakketindeling onder `internal/` voor hooks en subcommando's.
- Of een hook en een subcommando die dezelfde lib gebruiken, één Go-pakket delen.
- De opdeling van stap 7 in één of twee PR's, en het samenvoegen van kleine stappen als een PR
  klein blijft.
- Hoe de testsuite de binary één keer bouwt (fixture, `conftest.py`).
