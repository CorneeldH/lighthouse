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

- Eén subcommand, `lighthouse deps check`, dat per gedetecteerde taal de check draait en
  bevindingen meldt als `<bestand>:<regel>: <import> — <reden>`.
- Een Stop-hook per pack die dat subcommand aanroept, en dezelfde aanroep in de commit-testpoort.
- Per pack een gepind gereedschap met getoetste licentie, of de standaard-toolchain.
- Per pack een test: een nep-repo met een meeliftende import faalt, dezelfde repo met de import
  gedeclareerd slaagt.

**Niet-doelen**

- De vijf dependencies in paia toevoegen: dat blijft in paia.
- Ongebruikte dependencies (de omgekeerde richting: gedeclareerd maar niet geïmporteerd). knip en
  deptry kunnen het, maar het is een andere as en geen deel van dit AC.
- Het mechanisme "linter aangesloten" (K2). Dit ontwerp legt de vorm neer die K2 kan hergebruiken,
  maar bouwt K2 niet.
- Bevindingen in een gebruikersrepo wegwerken (no-go van de milestone).

**Anti-doelen**

- Een poort die stil stopt met kijken: geen gereedschap, geen `node_modules`, een te oude Go. Dat
  geeft `COULD NOT CHECK`, nooit groen.
- Een poort die vals alarm geeft op aliassen, workspaces of type-only imports. Daarom per taal
  het gereedschap dat die kent, geen eigen parser.
- Configuratie versoepelen om lighthouse zelf groen te krijgen (no-go van de milestone).

## Randvoorwaarden

- Geen nieuwe Python-code buiten `pack/python/` (no-go). deptry is een extern gereedschap via
  `uvx`, geen Python in de repo.
- Elk gereedschap gepind, de licentie getoetst en in de PR genoemd (AC 2, no-go).
- Hook-meldingen en instructies in het Engels; deze spec, de PR en de changelog in het Nederlands.
- Dogfood: een wijziging onder `pack/` draait `go run ./cmd/lighthouse init --upgrade` en commit de
  geïnstalleerde kopie.
- Nieuwe tests in Go, niet in pytest: de milestone *De repo zonder Python* haalt pytest weg
  ([spec](2026-11-16-repo-zonder-python-design.md)).

## Aanpakken

**Waar de poort draait** (besluit gebruiker: c).

- a) Een Stop-hook per pack met de check zelf erin, zoals `go-lint-gate`. Snel en warm voor de
  agent, maar vier losse scripts en geen dekking voor een mens of Codex zonder hooks.
- b) Alleen in `[test_gate] command`. Eén plek, maar de melding komt pas bij de commit.
- **c) Een subcommand `lighthouse deps check`, aangeroepen door een Stop-hook én door de
  testpoort.** De packs leveren alleen gereedschap, pin en detectie. Kost het meest, maar K2 kan
  dezelfde vorm hergebruiken.

**Gereedschap per pack.**

| Pack | Gekozen | Afgevallen |
|---|---|---|
| Python | `deptry`, gepind via `uvx deptry==<versie>`, MIT (besluit gebruiker) | eigen Go-check die importnamen aan distributies koppelt: die mapping is precies wat deptry al kent |
| Go | `go mod tidy -diff` (standaard sinds Go 1.23), plek uit de binary (besluit gebruiker) | eigen check op `go list` als detector; golangci-lint (`depguard`, `gomodguard` blokkeren alleen modules op een lijst) |
| Node, Expo | `knip --include unlisted`, gepind via `npx knip@<versie>` (besluit gebruiker) | `depcheck` (inactief, geen release in twee jaar); eigen Go-parser (mist aliassen, workspaces, dynamische imports); `eslint-plugin-import` (vraagt een ESLint-opzet in elke repo) |

## Ontwerp

### 1. `lighthouse deps check`

```
lighthouse deps check [--lang python|go|node] [--dir <pad>]...
```

- Zonder `--lang` draait het de talen van de packs die `init` in deze repo installeerde. Met
  `--lang` alleen die taal. `--dir` beperkt een taal tot een map: een Node-project staat vaak niet
  in de root (`pack/node/detect.toml` zoekt `**/package.json`).
- Uitvoer: één regel per bevinding, `<bestand>:<regel>: <import> — <reden>`, met `<reden>`
  `not declared` of `only transitive via <pakket>` voor zover het gereedschap dat levert.
- Exitcodes: 0 schoon, 1 bevindingen, 3 kon niet controleren (met
  `deps check: COULD NOT CHECK — <reden>` op stderr). 3 is `gate.Unchecked`
  (`internal/gate/gate.go`): de testpoort leest hem via `unchecked_exit_codes`, en 2 kan daar niet
  staan omdat pytest hem gebruikt *(eigen keuze, volgt de repo-conventie)*.
- Combinatie binnen één run (meerdere talen, modules of `package.json`-mappen): bevindingen winnen.
  Is er ergens een bevinding, dan exit 1, met de bevindingen én de `COULD NOT CHECK`-regels in de
  uitvoer. Anders wint 3 van 0: groen betekent "alles gecontroleerd" *(eigen keuze: een bevinding
  mag nooit wegvallen achter een onderdeel dat niet kon controleren)*.
- Het subcommand komt in `internal/cli`, naar het model van `newGateCommand`
  (`internal/cli/gate.go`), met de logica per taal in een eigen pakket *(eigen keuze)*.

### 2. Python: deptry

- deptry moet in de omgeving van het project draaien: het koppelt importnamen aan distributies, en
  ziet een transitieve dependency (`DEP003`), via de geïnstalleerde pakketten. `uvx` draait in een
  eigen omgeving en ziet die niet. Aanroep daarom in de venv van het project, zonder te syncen,
  bijvoorbeeld `uv run --frozen --no-sync --with deptry==<versie> deptry <bron>`, met de bronmappen
  uit `python.source_dirs`; de bouwer bevestigt de exacte vorm.
- Geen `.venv` in de checkout: `COULD NOT CHECK — no .venv`, zoals `ty-gate.sh` *(eigen keuze:
  syncen kost netwerk en verandert de checkout)*.
- `DEP001` (ontbrekend) en `DEP003` (alleen transitief) blokkeren; `DEP002` (ongebruikt) en
  `DEP004` (dev-dependency in productiecode) niet.
- **Extras.** deptry leest `[project.optional-dependencies]` als gedeclareerd. Een import achter
  een `ImportError`-guard die in een extra staat telt dan als gedeclareerd: dat dekt AC 3. De docs
  bevestigen dit niet expliciet (alleen [openapi-core #574](https://github.com/python-openapi/openapi-core/issues/574)
  wijst die kant op). De test in §6 bewijst het; faalt hij, dan is dit een open punt voor de
  gebruiker, geen stille workaround.
- deptry's eigen uitvoer bevat `bestand:regel:kolom`; de binary herschrijft naar het formaat van
  §1.
- Geen `uv`, of deptry niet op te halen (geen netwerk en geen cache): `COULD NOT CHECK`.

### 3. Go: `go mod tidy -diff` plus plek

- Detectie: `go mod tidy -diff` in elke module (`go.mod`) van de repo. Lege uitvoer is schoon.
- Plek: voor elke module die in de diff van `// indirect` naar direct gaat, zoekt de binary met
  `go list -json ./...` de pakketten die een pakket uit die module importeren, en met `go/parser`
  het bestand en de regel van die import.
- Een diff zonder zo'n verschuiving (alleen een verouderde of ontbrekende regel) meldt
  `go.mod: <regel uit de diff> — go.mod not tidy`.
- Go ouder dan 1.23 (geen `-diff`): `COULD NOT CHECK`.
- In Go is het paia-probleem kleiner: een import uit een module buiten `go.mod` compileert niet, en
  een `// indirect`-module heeft toch een eigen `require`. Deze poort houdt vooral `go.mod` eerlijk.
  Dat is bewust: de milestone eist de check per pack.

### 4. Node en Expo: knip

- Aanroep: `npx --yes knip@<versie> --include unlisted --reporter json` in elke map met een
  `package.json` (of de `--dir`).
- knip vraagt een geïnstalleerde `node_modules`. Ontbreekt die: `COULD NOT CHECK — node_modules
  missing in <dir>`, niet installeren *(eigen keuze: installeren is traag en heeft netwerk nodig, en
  de poort hoort niets te veranderen)*.
- **Expo.** Een Expo-repo krijgt ook `pack/node` (die detecteert elke `package.json`), en knip zet
  zijn Expo-plugin automatisch aan als `expo` in de dependencies staat. `pack/expo` krijgt daarom
  geen eigen hook; de test in §6 bewijst dat een Expo-nep-repo via `pack/node` gevangen wordt
  *(eigen keuze)*.
- `pack/node/requires.toml` krijgt `npx` erbij.

### 5. Aansluiting: hook en testpoort

- **Stop-hook `deps-gate`** in `pack/python`, `pack/go` en `pack/node`, één dun script per pack
  dat `lighthouse deps check --lang <taal>` aanroept. Het volgt het patroon van `ty-gate.sh`: het
  kijkt alleen in checkouts waar de beurt een relevant pad raakte (`lighthouse turn-roots --match`),
  zegt `not checked` tijdens een usage-alarm, en zegt `COULD NOT CHECK` op exit 3 zonder te
  blokkeren. Exit 1 blokkeert (exit 2 naar Claude Code) met de bevindingen op stderr.
- Relevante paden: Python `*.py`, `pyproject.toml`, `uv.lock`; Go `*.go`, `go.mod`, `go.sum`; Node
  `*.js|jsx|ts|tsx|mjs|cjs`, `package.json`, de lockfile.
- **Testpoort.** De standaard-`[test_gate] command` van `pack/python` (`lighthouse.toml.fragment`)
  krijgt `&& lighthouse deps check --lang python` **achteraan**. Achteraan, omdat een exit 3 de
  `&&`-keten stopt: vooraan zou een offline machine de tests nooit laten draaien. Het fragment
  heeft 3 al in `unchecked_exit_codes`. Voor Go en Node is er geen fragment; daar noemt de
  pack-documentatie de aanroep *(eigen keuze: een gebruiker die zijn eigen command heeft, krijgt
  geen stille wijziging)*.
- **Canary.** De milestone eist een canary per blokkerende regel (K1). Voor deze poort is de canary
  de nep-repo van §6: een meeliftende import die moet falen.
- De pin staat als Go-constante naast de code die het gereedschap aanroept, want de binary roept
  `uv` en `npx` aan, niet het hook-script. De licentie staat in het commentaar bij de constante
  (zoals de kop van `shellcheck-bin.sh`). Er is één plek voor de pin; de documentatie noemt het
  gereedschap zonder versie, dus er is geen synchronisatietest nodig *(eigen keuze: `pack/node`
  heeft geen skill om de pin in te noemen)*.

### 6. Dogfood

Lighthouse installeert zelf `core`, `go` en `node` (`.lighthouse/manifest.toml`), niet `python`.
`lighthouse deps check` zonder `--lang` controleert hier dus Go (de module in de root) en Node
(`web/`). De Python van de pytest-suite valt erbuiten: geen pack, en de milestone *De repo zonder
Python* haalt hem weg *(eigen keuze)*. `lighthouse.toml` `[test_gate] command` krijgt
`&& lighthouse deps check` achteraan; `unchecked_exit_codes = [3]` staat er al. Bevindingen in
lighthouse zelf worden in deze PR opgelost door de dependency te declareren, niet door een regel
uit te zetten.

## Tests

Per pack een Go-test met een nep-repo in een `t.TempDir()` (deze faalt als de check niets meer
vangt):

| Pack | Faalt | Slaagt |
|---|---|---|
| Python | `import numpy` met alleen `scipy` gedeclareerd | `numpy` in `[project.dependencies]` |
| Python, extra | `try: import httpx except ImportError` zonder declaratie | `httpx` in een extra |
| Go | import uit een module die `// indirect` staat | dezelfde module direct |
| Node | `import x from "lodash"` met alleen een pakket dat lodash meebrengt | `lodash` in `dependencies` |
| Expo | idem in een repo met `expo` in `package.json` | idem |

De Python-nep-repo installeert geen echte `scipy`/`numpy` (netwerk, traag). De test maakt een
venv met `uv venv` en schrijft er twee nep-distributies in: `.dist-info`-mappen met `METADATA`
(`b` als `Requires-Dist` van `a`) en `top_level.txt`. deptry leest die via `importlib.metadata`,
dus de test draait offline. De bouwer bevestigt dat deptry `DEP003` daarop meldt; lukt dat niet,
dan is dat een open punt voor de gebruiker *(eigen keuze)*.

Plus:

- `COULD NOT CHECK` per pack (geen `uv`, geen `.venv`, geen `node_modules`, Go < 1.23 of
  nagebootst), met exit 3.
- De uitvoer noemt bestand, regel en import (AC 1).
- Exitcode 3 wint van 0, en 1 wint van 3 met beide meldingen in de uitvoer.
- De hook: `not checked` tijdens een usage-alarm, blokkeert op exit 1.

Tests die `uv`, `npx` of een Go-toolchain met netwerk nodig hebben, slaan over met een duidelijke
reden als het gereedschap ontbreekt, en draaien in CI waar `uv` en `node` er zijn.

## Wat aan de bouwer is

- De exacte deptry-, knip-flags en het JSON-formaat dat de binary parset.
- De pakketindeling onder `internal/` en de naam van het pakket.
- Hoe de taalset zonder `--lang` uit de geïnstalleerde packs volgt.
- De versies van deptry en knip (de laatste stabiele, minstens twee weken oud) en de licentietoets
  van knip (verwacht ISC, te bevestigen in de PR).

## Bronnen

- [deptry: rules and violations](https://fpgmaas.github.io/deptry/rules-violations/)
- [deptry: usage](https://fpgmaas.github.io/deptry/usage/)
- [knip vs depcheck](https://www.pkgpulse.com/guides/knip-vs-depcheck-2026)
- [Snyk: depcheck](https://snyk.io/advisor/npm-package/depcheck)
