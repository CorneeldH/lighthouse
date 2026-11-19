# Ontwerp: per taalpack een poort op ongedeclareerde directe imports

Datum: 2026-11-19 · Issue: [#383](https://github.com/CorneeldH/lighthouse/issues/383) · Milestone:
[Kwaliteitspoorten per taalpack](https://github.com/CorneeldH/lighthouse/milestone/13) (K3 in
[de milestone-spec](2026-11-16-zelflerende-lighthouse-design.md))

## Aanleiding en doel

Een pakket dat de code direct importeert, maar dat alleen via een andere dependency in de omgeving
staat, werkt tot die andere dependency verandert. Dan valt het om, terwijl aan de eigen constraints
niets veranderde. In paia waren dat er vijf, onder andere `numpy` (meeliftend op `scipy`) en
`botocore` (op `boto3`) ([paia #590](https://github.com/CorneeldH/personal_ai_agent/issues/590)).

Doel: elk taalpack (Python, Go, Node, Expo) krijgt een poort die faalt op zo'n import en daarbij de
import en de plek noemt. Een onbemande run kan dan vertrouwen op wat er gedeclareerd staat.

## Doelen, niet-doelen, anti-doelen

**Doelen**

- Eén subcommand, `lighthouse imports check`, dat per taal de check draait en bevindingen meldt
  met plek, regel-ID en import (§1).
- Twee aanroepers: een Stop-hook per pack, en de commit-testpoort, die de check zelf draait, los
  van `[test_gate] command` (besluit gebruiker na de spec-review, r1-f1).
- Per pack een gepind gereedschap met getoetste licentie, of de standaard-toolchain.
- Per regel-ID een canary in de vorm van [#381](https://github.com/CorneeldH/lighthouse/issues/381) (spec op branch `issue-381-canary-per-regel`), en per
  pack een nep-repo met een meeliftende import die faalt en met de import gedeclareerd slaagt.

**Niet-doelen**

- De vijf dependencies in paia toevoegen: dat blijft in paia.
- Ongebruikte dependencies (de omgekeerde richting: gedeclareerd maar niet geïmporteerd), ook in
  Go (§3). knip en deptry kunnen het, maar het is een andere as en geen deel van dit AC.
- Het mechanisme "linter aangesloten" (K2, #382). Dit ontwerp legt de vorm neer die K2 kan
  hergebruiken, maar bouwt K2 niet.
- Bevindingen in een gebruikersrepo wegwerken (no-go van de milestone). Daarom blokkeert de poort
  alleen op wat de beurt of de commit raakte (§5).

**Anti-doelen**

- Een poort die stil stopt met kijken: geen gereedschap, geen venv, geen `node_modules`, een te
  oude Go of een te oude lighthouse. Dat geeft `COULD NOT CHECK`, nooit groen.
- Een poort die vals alarm geeft op aliassen, workspaces of type-only imports. Daarom per taal
  het gereedschap dat die kent, geen eigen parser.
- Configuratie versoepelen om lighthouse zelf groen te krijgen (no-go van de milestone).

## Randvoorwaarden

- Geen nieuwe Python-code buiten `pack/python/` (no-go). deptry is een extern gereedschap, geen
  Python in de repo.
- Elk gereedschap gepind, de licentie getoetst en in de PR genoemd (AC 2, no-go).
- Hook-meldingen en instructies in het Engels; deze spec, de PR en de changelog in het Nederlands.
- Dogfood: een wijziging onder `pack/` draait `go run ./cmd/lighthouse init --upgrade` en commit de
  geïnstalleerde kopie.
- Elk nieuw bestand onder `pack/` krijgt een regel in `pack/SOURCES.toml` (`new = true`);
  `tests/test_sources.py` eist dat (r1-f13).
- Afhankelijkheid met [#381](https://github.com/CorneeldH/lighthouse/issues/381) (spec op branch `issue-381-canary-per-regel`) (canary per regel): die spec
  sluit Node en Expo uit omdat ze geen blokkerende hook hebben; deze spec maakt de eerste. Welke
  van de twee als tweede landt, neemt de hooks van de andere mee in het canary-patroon.

## Aanpakken

**Waar de poort draait** (besluit gebruiker: c).

- a) Een Stop-hook per pack met de check zelf erin, zoals `go-lint-gate`. Snel en warm voor de
  agent, maar vier losse scripts en geen dekking buiten de beurt.
- b) Alleen in de testpoort. Eén plek, maar de melding komt pas bij de commit.
- **c) Een subcommand, aangeroepen door een Stop-hook én door de testpoort.** De packs leveren
  alleen detectie en hook; de binary doet de check. K2 kan dezelfde vorm hergebruiken.

**Hoe de testpoort de check krijgt** (besluit gebruiker na de spec-review, r1-f1). `init --upgrade`
overschrijft een bestaand `[test_gate] command` nooit (`AppendMissing`), en Go en Node hebben geen
fragment. Een fragment of een changelog-regel bereikt bestaande repo's als paia dus niet.

- **De commit-gate draait de check zelf**, voor de geïnstalleerde packs, los van
  `[test_gate] command`. Elke repo krijgt hem bij de upgrade.
- Afgevallen: alleen fragment plus changelog (paia blijft onbeschermd); upgrade herschrijft het
  command (raakt een command van de gebruiker); geen testpoort (een mens of Codex wordt niet
  gevangen).

**Gereedschap per pack.**

| Pack | Gekozen | Afgevallen |
|---|---|---|
| Python | `deptry`, gepind, MIT (besluit gebruiker) | eigen Go-check die importnamen aan distributies koppelt: die mapping is precies wat deptry al kent |
| Go | `go mod tidy -diff` (standaard sinds Go 1.23), plek uit de binary (besluit gebruiker) | eigen check op `go list` als detector; golangci-lint (`depguard`, `gomodguard` blokkeren alleen modules op een lijst) |
| Node, Expo | `knip --include unlisted`, gepind (besluit gebruiker) | `depcheck` (inactief, geen release in twee jaar); eigen Go-parser (mist aliassen, workspaces, dynamische imports); `eslint-plugin-import` (vraagt een ESLint-opzet in elke repo) |

## Ontwerp

### 1. `lighthouse imports check`

```
lighthouse imports check [--lang python|go|node]... [--files <pad>]...
```

- Naam: `imports`, niet `deps`. `internal/deps` bestaat al en controleert de gereedschappen van
  `init`. De logica komt in een eigen pakket, `internal/importcheck` *(eigen keuze na
  spec-review, r1-f14)*. Het subcommand volgt het model van `newGateCommand`
  (`internal/cli/gate.go`).
- **Talen.** Zonder `--lang` de talen van de packs die `init` installeerde (`.lighthouse/manifest.toml`
  `packages`; Expo valt onder `node`, §4).
- **Projecten.** Alleen bestanden die git volgt, zoals `detect.toml` *(eigen keuze na spec-review,
  r1-f11)*: elke gevolgde `go.mod` is een Go-module, elke gevolgde `pyproject.toml` een
  Python-project, en Node volgt §4. In lighthouse zelf zijn dat `go.mod` en `web/go.mod`.
- **Scope.** Met `--files` (de bestanden van de beurt of de commit) draait de check alleen de
  projecten waarin een van die bestanden ligt, en meldt hij alleen bevindingen in die bestanden.
  Raakt `--files` een manifest of lockfile van een project (`pyproject.toml`, `uv.lock`, `go.mod`,
  `go.sum`, `package.json`, een lockfile), dan alle bevindingen van dat project. Zonder `--files`:
  alles *(eigen keuze na spec-review, r1-f6: zo blokkeert een commit niet op een project dat hij
  niet raakte, en niet op schuld in bestanden die hij niet aanraakte)*.
- **Uitvoer.** Eén regel per bevinding op stdout:
  `<bestand>:<regel>: <ID> <import> — <reden>`. De ID is de regel-ID van §5 (`DEP001`, `DEP003`,
  `indirect-import`, `missing-require`, `unlisted`). Een `COULD NOT CHECK — <reden>`-regel per
  project dat niet kon controleren gaat naar stderr.
- **Exitcodes.** 0 schoon, 1 bevindingen (minstens één bevindingsregel op stdout), 3 kon niet
  controleren. 3 is `gate.Unchecked` (`internal/gate/gate.go`). Een fout in de aanroep (onbekende
  vlag) geeft ook 3, niet cobra's 1 *(eigen keuze na spec-review, r1-f8)*.
- **Combinatie** binnen één run: bevindingen winnen. Is er ergens een bevinding, dan exit 1, met
  de bevindingen én de `COULD NOT CHECK`-regels in de uitvoer. Anders wint 3 van 0.
- **Aanroepers lezen exit 1 streng.** Exit 1 zonder bevindingsregel op stdout, of met
  `unknown command` op stderr (een te oude binary, zie AGENTS.md), is `COULD NOT CHECK`, geen
  bevinding (r1-f8).

### 2. Python: deptry

- deptry moet in de omgeving van het project draaien. Het koppelt importnamen aan distributies, en
  ziet een transitieve dependency (`DEP003`), via de geïnstalleerde pakketten; `uvx` draait in een
  eigen omgeving en ziet die niet. Aanroep in de venv van het project zonder te syncen,
  bijvoorbeeld `uv run --frozen --no-sync --with deptry==<versie> deptry <bron>`; de bouwer
  bevestigt de exacte vorm.
- **Welke venv.** `<project>/.venv`, anders die van de main checkout (`$CLAUDE_PROJECT_DIR/.venv`),
  zoals `ty-gate.sh:140-149`: een verse worktree heeft nog geen `.venv`. Geen van beide:
  `COULD NOT CHECK — no .venv in <project> or the main checkout; run uv sync first.`
  *(eigen keuze na spec-review, r1-f5)*. Buiten Claude Code (geen `CLAUDE_PROJECT_DIR`) alleen
  de eigen `.venv`.
- **Bronmappen** uit `python.source_dirs`, met de regels van `ty-gate.sh` *(r1-f10)*:
  `[]` zet de check voor Python uit, zonder melding; een map die niet bestaat wordt overgeslagen;
  bestaat er geen enkele, dan `not checked` voor dat project, geen bevinding.
- `DEP001` (ontbrekend) en `DEP003` (alleen transitief) blokkeren; `DEP002` (ongebruikt) en
  `DEP004` (dev-dependency in productiecode) niet.
- **Extras.** deptry leest `[project.optional-dependencies]` als gedeclareerd. Een import achter
  een `ImportError`-guard die in een extra staat telt dan als gedeclareerd: dat dekt AC 3. De docs
  bevestigen dit niet expliciet (alleen [openapi-core #574](https://github.com/python-openapi/openapi-core/issues/574)
  wijst die kant op). De test bewijst het; faalt hij, dan is dit een open punt voor de gebruiker,
  geen stille workaround.
- deptry geeft `bestand:regel:kolom`; de binary herschrijft naar het formaat van §1.
- Geen `uv`, deptry niet op te halen, of uitvoer die niet te lezen is: `COULD NOT CHECK`.

### 3. Go: `go mod tidy -diff` plus plek

Per gevolgde `go.mod`, in de map van die module:

- **Exitcode eerst** *(r1-f9)*. Exit 0 met lege stdout: schoon. Exit 1 met een diff op stdout:
  classificeren (hieronder). Elke andere uitkomst, ook exit 1 zonder diff (bijvoorbeeld een module
  die offline niet in de cache staat): `COULD NOT CHECK` met stderr als reden. Go ouder dan 1.23
  (geen `-diff`): `COULD NOT CHECK`.
- **Classificeren** *(r1-f4)*. Alleen twee soorten hunks in `go.mod` blokkeren:
  - een module die van `// indirect` naar direct verschuift: `indirect-import`. De binary zoekt
    met `go list -json ./...` de pakketten die een pakket uit die module importeren, en met
    `go/parser` bestand en regel. Vindt hij de import niet (bijvoorbeeld alleen in een bestand
    voor een ander platform), dan is de plek de regel in `go.mod`.
  - een `require` die ontbreekt: `missing-require`, met de plek van de import.
- **Overige drift blokkeert niet**: een overbodige `require` (de omgekeerde richting, een
  niet-doel), een `go.sum`-hunk, een versie die anders uitkomt. Die laat de check stil liggen.
- In Go is het paia-probleem kleiner: een import uit een module buiten `go.mod` compileert niet, en
  een `// indirect`-module heeft toch een eigen `require`. Deze poort houdt vooral `go.mod` eerlijk.
  Dat is bewust: de milestone eist de check per pack.

### 4. Node en Expo: knip

- **Waar knip draait** *(r1-f3)*. Per **workspace-root**: een gevolgde `package.json` die niet zelf
  een workspace is van een `package.json` erboven (via `workspaces` in `package.json`, of
  `pnpm-workspace.yaml`). knip draait één keer per root en ziet de workspaces als één graph.
  `node_modules` moet in die root staan, ook bij gehoiste installaties.
- Aanroep: `npx --yes knip@<versie> --include unlisted --reporter json`. knip vraagt een
  geïnstalleerde `node_modules`. Ontbreekt die: `COULD NOT CHECK — node_modules missing in <root>`,
  niet installeren *(installeren is traag en heeft netwerk nodig, en de poort hoort niets te
  veranderen)*.
- `npx` dat niet start, knip niet op te halen, of JSON die niet te lezen is: `COULD NOT CHECK`
  *(r1-f12)*. `npx` en knip geven bij bevindingen en bij een fout allebei exit 1; de binary leest
  daarom de JSON, niet de exitcode.
- **Expo.** Een Expo-repo krijgt ook `pack/node` (die detecteert elke `package.json`), en knip zet
  zijn Expo-plugin automatisch aan als `expo` in de dependencies staat. `pack/expo` krijgt daarom
  geen eigen hook; de test bewijst dat een Expo-nep-repo via `pack/node` gevangen wordt.
- `pack/node/requires.toml` krijgt `npx` erbij.

### 5. Aanroepers: Stop-hook, commit-gate, Codex

**Regel-ID's.** Elke reden waarop de poort blokkeert heeft een vaste ID, in de vorm van #381:

| Pack | `BLOCKING_RULES` |
|---|---|
| python | `DEP001,DEP003` |
| go | `indirect-import,missing-require` |
| node | `unlisted` |

**Stop-hook.** Eén script per pack met een **eigen naam**, `imports-gate-python.sh`,
`imports-gate-go.sh` en `imports-gate-node.sh`. Alle hooks landen in `.claude/hooks/lighthouse/`
(`internal/packs/packs.go:121`), dus één gedeelde naam zou botsen *(r1-f2)*. `pack/node` krijgt
daarvoor een `hooks/`-map met `hooks.json`.

- Elk script declareert `BLOCKING_RULES="…"` uit de tabel, zoals #381 eist, en roept
  `lighthouse imports check --lang <taal> --files <bestanden van de beurt>` aan.
- Het volgt het patroon van `ty-gate.sh`: het kijkt alleen in checkouts waar de beurt een relevant
  pad raakte (`lighthouse turn-roots --match`), zegt `not checked` tijdens een usage-alarm, en zegt
  `COULD NOT CHECK` zonder te blokkeren op exit 3 en op exit 1 zonder bevinding (§1).
- Exit 1 met bevindingen blokkeert (exit 2 naar Claude Code) met de bevindingen op stderr; elke
  regel bevat de ID.
- Relevante paden: Python `*.py`, `pyproject.toml`, `uv.lock`; Go `*.go`, `go.mod`, `go.sum`; Node
  `*.js|jsx|ts|tsx|mjs|cjs`, `package.json`, de lockfile.

**Commit-gate** (besluit gebruiker na de spec-review, r1-f1). `commit-test-gate`
(`internal/commitgate`) draait na het testcommando ook `lighthouse imports check --files
<bestanden van de commit>`, voor de geïnstalleerde packs. Dat staat los van `[test_gate] command`:
geen fragment, geen migratie, en elke repo krijgt het bij de upgrade.

- Bevindingen: rood, zoals een falende test, met de bevindingen in de melding.
- Exit 3, of exit 1 zonder bevinding: `COULD NOT CHECK`, blokkerend zoals een code uit
  `unchecked_exit_codes` (`internal/commitgate/gate.go:566`). Door de scope van §1 gebeurt dat
  alleen als de commit een project raakte dat niet te controleren was, zoals een commit onder
  `web/` zonder `web/node_modules`. Dat is hetzelfde gedrag als `lighthouse gate node` vandaag
  *(r1-f6)*.
- Het moment volgt de commit-gate: op een branch na de commit, op de default branch ervoor.
- Hoe de stap in `commitgate` hangt (eigen stap, volgorde ten opzichte van het testcommando,
  hoe de melding de twee uitslagen scheidt) is aan de bouwer.

**Codex en OpenCode** draaien geen hooks. Elk pack (`pack/python`, `pack/go`, `pack/node`) krijgt
in zijn instructies één regel: draai `lighthouse imports check` voor een commit, naast
`[test_gate] command`. `pack/node` krijgt daarvoor een `instructions/`-map.

**Pin.** De pin van deptry en knip staat als Go-constante naast de code die het gereedschap
aanroept, want de binary roept `uv` en `npx` aan, niet het hook-script. De licentie staat in het
commentaar bij de constante (zoals de kop van `shellcheck-bin.sh`). Er is één plek voor de pin; de
documentatie noemt het gereedschap zonder versie.

### 6. Dogfood

Lighthouse installeert zelf `core`, `go` en `node` (`.lighthouse/manifest.toml`), niet `python`.
De check draait hier dus op Go (`go.mod` en `web/go.mod`) en Node (`web/`). De Python van de
pytest-suite valt erbuiten: geen pack, en de milestone *De repo zonder Python* haalt hem weg.
`[test_gate] command` blijft gelijk; de commit-gate draait de check zelf. Bevindingen in
lighthouse zelf worden in deze PR opgelost door de dependency te declareren, niet door een regel
uit te zetten.

**Overgang.** Tot de nieuwe binary op `PATH` staat, kent `lighthouse` `imports` niet. De hook en de
commit-gate lezen dat als `COULD NOT CHECK` (§1), niet als een bevinding.

## Tests

**Check-logica** (Go, `internal/importcheck`): per pack een nep-repo in `t.TempDir()`.

| Pack | Faalt | Slaagt |
|---|---|---|
| Python | `import numpy` met alleen `scipy` gedeclareerd (`DEP003`) | `numpy` in `[project.dependencies]` |
| Python, extra | `try: import httpx except ImportError` zonder declaratie | `httpx` in een extra |
| Go | import uit een module die `// indirect` staat | dezelfde module direct |
| Node | `import x from "lodash"` met alleen een pakket dat lodash meebrengt | `lodash` in `dependencies` |
| Node, workspace | import in een child-workspace, `node_modules` gehoist in de root | idem gedeclareerd in de child |
| Expo | idem in een repo met `expo` in `package.json` | idem |

De Python-nep-repo installeert geen echte `scipy`/`numpy` (netwerk, traag). De test maakt een
venv met `uv venv` en schrijft er twee nep-distributies in: `.dist-info`-mappen met `METADATA`
(`b` als `Requires-Dist` van `a`) en `top_level.txt`. deptry leest die via `importlib.metadata`,
dus de test draait offline. De bouwer bevestigt dat deptry `DEP003` daarop meldt; lukt dat niet,
dan is dat een open punt voor de gebruiker.

Plus:

- `COULD NOT CHECK` per pack (geen `uv`, geen `.venv`, geen `node_modules`, Go < 1.23 of
  nagebootst, `tidy -diff` exit 1 zonder diff), met exit 3.
- Go-drift die niet blokkeert: een overbodige `require` en een `go.sum`-hunk geven exit 0.
- De uitvoer noemt bestand, regel, ID en import (AC 1).
- Scope: een bevinding buiten `--files` blokkeert niet, behalve als `--files` het manifest raakt.
- Exitcode 3 wint van 0, en 1 wint van 3 met beide meldingen in de uitvoer.
- Een onbekende vlag geeft 3.

**Canaries per regel-ID** (de vorm van #381): per ID een overtreding onder
`testdata/<hook>/<id>.<ext>`, en een test die de lijst uit `BLOCKING_RULES` leest en per ID exit 2
plus de ID in stderr eist; een ID zonder bestand en een bestand zonder ID falen allebei. De
canary van `imports-gate-python.sh` is pytest in `tests/pack/python/`, zoals #381 de Python-hooks
laat; die van Go en Node zijn Go-tests.

**Hook en commit-gate**: de hook zegt `not checked` tijdens een usage-alarm, blokkeert op exit 1
met bevindingen, en zegt `COULD NOT CHECK` op exit 3 en op `unknown command`. De commit-gate wordt
rood op een bevinding in de commit en `COULD NOT CHECK` op exit 3.

**CI** *(r1-f7)*. Een test die `uv` of `npx` mist, slaat lokaal over met een duidelijke reden,
maar **faalt** als `CI` gezet is: een overgeslagen canary in CI is de stille poort die #381
verbiedt. De job in `.github/workflows/ci.yml` die deze Go-tests draait (nu `go-test`, alleen
`setup-go`) krijgt `setup-uv` en `setup-node`.

## Wat aan de bouwer is

- De exacte deptry- en knip-flags en het JSON-formaat dat de binary parset.
- De indeling binnen `internal/importcheck`.
- Hoe de check in `internal/commitgate` hangt (§5).
- De versies van deptry en knip (de laatste stabiele, minstens twee weken oud) en de licentietoets
  van knip (verwacht ISC, te bevestigen in de PR).

## Bronnen

- [deptry: rules and violations](https://fpgmaas.github.io/deptry/rules-violations/)
- [deptry: usage](https://fpgmaas.github.io/deptry/usage/)
- [knip vs depcheck](https://www.pkgpulse.com/guides/knip-vs-depcheck-2026)
- [Snyk: depcheck](https://snyk.io/advisor/npm-package/depcheck)
