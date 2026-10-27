# SPA-skelet met orval-client Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** een lege SPA in `web/` (Vite, React, TypeScript, Tailwind v4, shadcn/ui, vitest) met een
orval-client uit een gecommitte `web/openapi.json`, ingebed via `go:embed` met een placeholder als
er niet gebouwd is, plus een indeler die per wijziging bepaalt welke checks draaien, lokaal én in
CI.

**Architecture:** `internal/web` embedt `all:dist` en `placeholder` en kiest met `Choose` wat hij
serveert. Een Go-test houdt `web/openapi.json` gelijk aan Huma's beschrijving; orval maakt daaruit
`web/src/api/lighthouse.ts`. Eén Python-script, `ci/domains.py`, deelt paden in bij de domeinen
`web`, `go` en `python`; de test-gate (`gate`) en de CI-job `changes` (`classify`) zijn de twee
afnemers.

**Tech Stack:** Go 1.26.4 met Huma v2; Node 22.22.2 met npm; Vite, React, TypeScript, Tailwind v4
(`@tailwindcss/vite`), shadcn/ui, ESLint (flat config met typescript-eslint), vitest met jsdom en
`@testing-library/react`, orval 8.45.0 (`client: 'fetch'`); Python 3.11+ stdlib voor het script;
GitHub Actions.

**Spec:** [2026-10-26-spa-skelet-design.md](../specs/2026-10-26-spa-skelet-design.md)

## Global Constraints

- Code, commentaar, namen en meldingen van scripts in het Engels; plan en commitberichten in het
  Nederlands, als Conventional Commits. De spec citeert de overslaanregel in het Nederlands; die
  wordt Engels (`gate: go skipped — no path in the domain`), want AGENTS.md wil hookmeldingen
  in het Engels en deze regel komt in de melding van de hook terecht.
- `go build` heeft nooit Node nodig. Een verse worktree zonder `npm ci` bouwt, test en lint.
- `CGO_ENABLED=0 go build ./...` blijft slagen.
- Geen gebouwde frontend in git: alleen `internal/web/dist/.gitkeep` is gevolgd. Na
  `npm run build` is `git status` schoon.
- Node: `web/.nvmrc` bevat `22.22.2`; `engines.node` in `web/package.json` is `"22.22.2"`.
  Installeren altijd met `npm ci`, nooit met `--legacy-peer-deps` of `--force`.
- Versies van de npm-pakketten kiest de bouwer: een set waarvan `npm ci` zonder peer-fouten
  slaagt. Gemeten op 2026-10-26: orval 8.45.0 werkt met Huma's 3.1. Laat typescript-eslint
  TypeScript 7 niet toe, pin dan de nieuwste TypeScript die hij wel toelaat.
- **Besluit 3 is gemeten: OpenAPI 3.1.** orval 8.45.0 maakt uit `/api/openapi.json` (3.1.0)
  dezelfde client als uit `/api/openapi-3.0.json` (3.0.3), `ErrorDetail[] | null` inbegrepen.
  `web/openapi.json` is dus de 3.1-versie.
- orval maakt een `fetch`-client met relatieve paden (`/api/health`), zonder tanstack-query.
- Merkteken van de build: `web/index.html` draagt `<meta name="lighthouse-ui" content="spa">`, de
  placeholder `<meta name="lighthouse-ui" content="placeholder">`.
- Exitcode `3` van `ci/domains.py gate` betekent `COULD NOT CHECK`. `lighthouse.toml` krijgt
  `unchecked_exit_codes = [3]` onder `[test_gate]`.
- Actions in `ci.yml` vastgepind op een volledige commit-SHA met de tag als commentaar, zoals nu.
- Gate-commando vanaf Task 5: `python3 ci/domains.py gate`. Tot dan:
  `uv run pytest -q && go test -race -shuffle=on ./...`.
- Klaar melden per KPI-rij van `/lighthouse-go-writing`, met omgeving (linux, tags, `-race`,
  `GOWORK`).

## Review Focus

1. **Een hernoeming over de domeingrens** (`git mv internal/a.go web/a.go`): de Go-checks
   moeten draaien, want `internal` verloor een bestand. `git diff --name-only` toont bij een
   hernoeming alleen het nieuwe pad. Alle drie de git-bronnen, en de diff in CI, draaien met
   `--no-renames`. → test in Task 5, pin in Task 6.
2. **Een pad met een spatie of niet-ASCII onder `web/`** (`web/src/café ü.tsx`): git citeert
   zo'n pad (`"web/src/caf\303\251 \303\274.tsx"`). Dan begint het met `"` en valt het buiten
   `web/`. Paden gaan dus altijd met `-z` (NUL-gescheiden). → test in Task 5, pin in Task 6.
3. **Een PR met alleen Go:** `web` slaat over. Een job met `needs: web` slaat dan standaard ook
   over. `binary` moet toch draaien en zelf de frontend bouwen. → `!cancelled()` in de `if`, pin in
   Task 6.
4. **Een wijziging aan `web/go.mod`:** die valt onder `web/`, maar het weghalen ervan laat
   `./...` door `web/node_modules` lopen. Het `go`-domein moet geraakt zijn. → tabelrij in Task 5.
5. **`/api/health` geeft geen 200** (bijvoorbeeld 500 met `application/problem+json`): de
   gegenereerde `getHealth` gooit dan geen fout maar geeft `status: 500` terug. De pagina toont
   een foutmelding, niet `lighthouse undefined`. → vitest-geval in Task 4.

---

### Task 1: Modulegrens en inbedding met placeholder

**Files:**
- Create: `web/go.mod`
- Create: `internal/web/placeholder/index.html` (opvolger van `internal/web/dist/index.html`)
- Create: `internal/web/dist/.gitkeep` (leeg), `internal/web/web_test.go`
- Delete: `internal/web/dist/index.html`
- Modify: `internal/web/web.go`, `internal/server/server_test.go`, `.gitignore`

**Interfaces:**
- Produces: `web.Choose(root fs.FS) fs.FS`: `fs.Sub(root, "dist")` als `dist/index.html` in `root`
  bestaat, anders `fs.Sub(root, "placeholder")`. `web.FS()` blijft bestaan en geeft
  `Choose(<embed>)`. De embedregel: `//go:embed all:dist placeholder`.

- [ ] **Step 1: Leg de modulegrens vóór alles.** `web/go.mod`, met een commentaarregel waarom
  hij bestaat:

```
// Empty module: stops ./... (go build, go test, golangci-lint) at web/, so the .go files
// some npm packages ship in web/node_modules are never compiled.
module github.com/CorneeldH/lighthouse/web

go 1.26.4
```

  Run: `! go list ./... | grep -q '^github.com/CorneeldH/lighthouse/web'` → exit 0 (geen
  treffer), en `go list ./internal/web` → exit 0. Geen `grep -c`: die geeft bij nul treffers
  exit 1.

- [ ] **Step 2: Schrijf de falende tests** in `internal/web/web_test.go`:
  - `TestChooseBuilt`: `fstest.MapFS{"dist/index.html": "spa", "placeholder/index.html": "ph"}`;
    `fs.ReadFile(Choose(m), "index.html")` is `"spa"`.
  - `TestChoosePlaceholder`: `fstest.MapFS{"dist/.gitkeep": "", "placeholder/index.html": "ph"}`
    geeft `"ph"`.
  - `TestPlaceholderNamesBuild`: de ingebedde `placeholder/index.html` bevat
    `cd web && npm ci && npm run build` en `name="lighthouse-ui" content="placeholder"`.

  En in `internal/server/server_test.go`:
  - `TestUnbuiltSiteServesPlaceholder`: `New(web.Choose(fstest.MapFS{"dist/.gitkeep": …,
    "placeholder/index.html": "<p>placeholder</p>"}))`; `/` en `/runs/42` geven 200 met
    `placeholder`.

- [ ] **Step 3: Run:** `go test ./internal/web ./internal/server` → FAIL (`undefined: Choose`).

- [ ] **Step 4: Implementeer.** `git mv internal/web/dist/index.html
  internal/web/placeholder/index.html`, maak de lege `internal/web/dist/.gitkeep`, en schrijf
  `Choose` en de nieuwe embedregel. De placeholder is één Engelse pagina met het merkteken, een
  `<title>lighthouse</title>` en één regel: `The web UI is not built. In the clone, run: cd web &&
  npm ci && npm run build`, gevolgd door een herinstallatie met `go install ./cmd/lighthouse`.
  `.gitignore` krijgt:

```
# Frontend: dependencies, and the Vite build (only the .gitkeep is tracked)
web/node_modules/
/internal/web/dist/*
!/internal/web/dist/.gitkeep
```

- [ ] **Step 5: Run:** `go test -race -shuffle=on ./...` → PASS, `TestEmbeddedSiteHasIndex`
  inbegrepen. `CGO_ENABLED=0 go build ./...` → exit 0.

- [ ] **Step 6: Mutatiecheck.** Laat `Choose` altijd `placeholder` geven → `TestChooseBuilt` rood.
  Haal `all:` weg → `go build ./internal/web` faalt met `contains no embeddable files`. Draai beide
  terug.

- [ ] **Step 7: Commit:** `feat(web): placeholder naast dist inbedden, met Choose (#68)`

### Task 2: `web/openapi.json` gelijk aan Huma

**Files:**
- Create: `internal/server/openapi_snapshot_test.go`, `web/openapi.json`

**Interfaces:**
- Consumes: `New(site fs.FS, report func(context.Context) usage.Report, src overview.Source) http.Handler` (sinds #65 en #67 op main), `get(h, path)`, `testSite()`, `fakeReport` en `stubSource{}` uit `server_test.go`.
- Produces: `web/openapi.json`, het 3.1-document met `info.version` op `"dev"`, ingesprongen met
  twee spaties, sleutels gesorteerd, met een afsluitende newline. Verversen:
  `go test ./internal/server -run TestOpenAPISnapshot -update`.

- [ ] **Step 1: Schrijf `TestOpenAPISnapshot`** met `var update = flag.Bool("update", false,
  "rewrite web/openapi.json from the served description")`. Hij haalt `/api/openapi.json` via
  `get`, decodeert naar `map[string]any`, zet `info.version` op `"dev"` en marshalt met
  `json.MarshalIndent(doc, "", "  ")` plus `"\n"`. `"dev"` maakt hem onafhankelijk van een versie
  uit VCS of ldflags. Met `-update` schrijft hij `../../web/openapi.json`. Zonder `-update`
  vergelijkt hij bytes en faalt hij met:
  `web/openapi.json is stale: run go test ./internal/server -run TestOpenAPISnapshot -update, then cd web && npm run gen`.
  Ontbreekt het bestand, dan faalt hij met dezelfde melding.

- [ ] **Step 2: Run:** `go test ./internal/server -run TestOpenAPISnapshot` → FAIL (bestand
  ontbreekt).

- [ ] **Step 3: Run:** `go test ./internal/server -run TestOpenAPISnapshot -update`, daarna
  zonder `-update` → PASS. `grep '"openapi": "3.1.0"' web/openapi.json` → treffer.

- [ ] **Step 4: Mutatiecheck.** Voeg een veld `Name string` toe aan `healthOutput.Body` →
  `go test ./internal/server` rood met de melding. Draai terug.

- [ ] **Step 5: Run:** `go test -race -shuffle=on ./...` → PASS. `-shuffle` en `-race`
  veranderen het document niet.

- [ ] **Step 6: Commit:** `feat(server): web/openapi.json gelijk houden aan de API-beschrijving (#68)`

### Task 3: Frontendstack die naar `internal/web/dist` bouwt

**Files:**
- Create in `web/`: `package.json`, `package-lock.json`, `.nvmrc`, `index.html`, `vite.config.ts`,
  `tsconfig.json` (met de tsconfig's die de bouwer erbij nodig heeft), `eslint.config.js`,
  `components.json`, `public/.gitkeep` (leeg), `src/main.tsx`, `src/App.tsx`, `src/index.css`,
  `src/lib/utils.ts`, `src/components/ui/card.tsx`
- Modify: `docs/install.md`

**Interfaces:**
- Produces: npm-scripts met vaste namen, die Task 4, 5 en 6 aanroepen: `dev` (`vite`), `build`
  (`vite build`), `lint` (ESLint over `web/`, `src/api/**` genegeerd), `typecheck` (`tsc` zonder
  emit, over `src/` inclusief `src/api/` en de tests), `test` (`vitest run`; Task 4 vult hem) en
  `gen` (Task 4). De alias `@/` wijst naar `web/src/`, in tsconfig en in Vite.
- Produces: `vite.config.ts` met `build.outDir: '../internal/web/dist'`, `build.emptyOutDir: true`
  en `server.proxy: { '/api': { target: 'http://127.0.0.1:8127', changeOrigin: true } }`.

- [ ] **Step 1: Zet de stack op.** Vite met React en TypeScript, Tailwind v4 via
  `@tailwindcss/vite`, en shadcn via `npx shadcn@<gepinde versie> init`, gevolgd door `add card`
  (schrijft `components.json`, `src/lib/utils.ts` en `src/components/ui/card.tsx`). `index.html`
  heeft `<div id="root">` en het merkteken `content="spa"`. `App.tsx` toont voorlopig een `Card`
  met de titel `lighthouse`, zodat de Tailwind- en shadcn-keten in de build zit. `web/` is niet
  leeg (`go.mod` uit Task 1, `openapi.json` uit Task 2): scaffold Vite in een tijdelijke map in de
  scratchpad en kopieer de bestanden naar `web/`. Kies nooit "remove existing files"; `go.mod` en
  `openapi.json` blijven staan.

- [ ] **Step 2: Run** in `web/`: `npm ci && npm run lint && npm run typecheck && npm run build`
  → alle vier exit 0.

- [ ] **Step 3: Run:** `ls -A internal/web/dist` → `.gitkeep`, `assets` en `index.html`.
  `grep -c 'content="spa"' internal/web/dist/index.html` → `1`. `git status --porcelain` → leeg
  op de nieuwe bronbestanden na: niets onder `internal/web/dist/` en niets onder
  `web/node_modules/`. Zet Vite de `.gitkeep` niet terug, dan is de `public/`-kopie stuk. Herstel
  dat vóór je verder gaat.

- [ ] **Step 4: Run:** `go test -race -shuffle=on ./... && golangci-lint run ./...` → PASS en
  `0 issues`, ook nu `web/node_modules` bestaat. `! go list ./... | grep -q node_modules` → exit 0.

- [ ] **Step 5: `docs/install.md`.** Requirements: Node 22 (de versie in `web/.nvmrc`) met npm.
  "Install the binary" wordt: eerst clonen, dan `cd web && npm ci && npm run build && cd ..`, dan
  `go install ./cmd/lighthouse`. Eén alinea: `go install …@main` levert alleen de placeholder;
  release-binaries met de UI volgen in #78. Laat de alinea over `@main` en `@latest` alleen staan
  waar hij nog klopt. "Check it" krijgt `curl -s http://127.0.0.1:8127/ | grep lighthouse-ui`,
  met `content="spa"` als goed teken. Links blijven relatief.

- [ ] **Step 6: Commit:** `feat(web): Vite-, React-, Tailwind- en shadcn-skelet dat in de binary landt (#68)`

### Task 4: orval-client en de health-pagina

**Files:**
- Create: `web/orval.config.ts`, `web/src/api/lighthouse.ts` (gegenereerd),
  `web/src/components/HealthCard.tsx`, `web/src/components/HealthCard.test.tsx`, `web/README.md`
- Modify: `web/package.json` (`gen`, `test`, devDependencies `orval`, `vitest`, `jsdom`,
  `@testing-library/react`), `web/package-lock.json`, `web/vite.config.ts` (`test.environment:
  'jsdom'`), `web/eslint.config.js`, `web/src/App.tsx`

**Interfaces:**
- Consumes: `web/openapi.json` (Task 2); de npm-scripts en `@/` uit Task 3.
- Produces: `npm run gen` = orval met input `./openapi.json`, output `./src/api/lighthouse.ts`,
  `client: 'fetch'`, `mode: 'single'`, zonder prettier en zonder query-laag. Uit die client:
  `getHealth(options?: RequestInit): Promise<getHealthResponse>`, met
  `{ data: HealthOutputBody, status: 200 }` of `{ data: ErrorModel, status: <ander> }`.
- Produces: `export function HealthCard(): JSX.Element` in `web/src/components/HealthCard.tsx`.

- [ ] **Step 1: Genereer de client.** `orval.config.ts` en `npm run gen` → `src/api/lighthouse.ts`
  bevat `export const getHealth` en `` `/api/health` ``. Noteer `sha256sum src/api/*`, draai
  `npm run gen` nog eens en vergelijk → gelijk (deterministisch; `git diff` ziet een nieuw,
  ongevolgd bestand niet).

- [ ] **Step 2: Schrijf de falende tests** in `HealthCard.test.tsx` (vitest, jsdom). Gebruik
  `vi.stubGlobal('fetch', …)` en ruim op met `afterEach(() => { cleanup(); vi.unstubAllGlobals(); })`:
  - `shows the version`: fetch geeft `new Response('{"version":"1.2.3"}', { status: 200,
    headers: { 'Content-Type': 'application/json' } })` →
    `await screen.findByText('lighthouse 1.2.3')`.
  - `shows an error when fetch fails`: fetch verwerpt met `new TypeError('Failed to fetch')` →
    `(await screen.findByRole('alert')).textContent` bevat `API unreachable`.
  - `shows an error on a non-200 status`: fetch geeft status 500 met een `problem+json`-body →
    alert met `API unreachable`, en `screen.queryByText(/lighthouse undefined/)` is `null`.

- [ ] **Step 3: Run:** `npm test` → FAIL (`HealthCard` bestaat niet).

- [ ] **Step 4: Implementeer `HealthCard`.** Hij roept `getHealth()` aan in een `useEffect` en
  toont een shadcn-`Card`. Bij `status === 200` staat er `lighthouse <version>`. Bij een andere
  status staat er een element met `role="alert"`: `API unreachable (HTTP <status>)`. Bij een
  verworpen fetch: `API unreachable: <message>`. Tijdens het laden: `Loading…`. `App.tsx`
  rendert `<HealthCard />`. ESLint negeert `src/api/**`.

- [ ] **Step 5: Run** in `web/`: `npm test && npm run lint && npm run typecheck && npm run build`
  → alles exit 0.

- [ ] **Step 6: Mutatiechecks.** Laat `HealthCard` een vaste tekst tonen → `shows the version`
  rood. Hernoem `getHealth` in `src/api/lighthouse.ts` → `npm run typecheck` rood. Draai beide
  terug; `npm run gen` geeft daarna een schone `git diff`.

- [ ] **Step 7: `web/README.md`** (Engels): `npm ci`, `npm run dev` (de proxy naar
  `127.0.0.1:8127`, dus `lighthouse serve` moet draaien), `npm run build` (schrijft naar
  `../internal/web/dist`, herinstalleer daarna de binary) en de keten bij een API-wijziging:
  `go test ./internal/server -run TestOpenAPISnapshot -update`, dan `npm run gen`. En de regel:
  `src/api/` is gegenereerd, nooit met de hand wijzigen.

- [ ] **Step 8: Commit:** `feat(web): orval-client en health-pagina met componenttests (#68)`

### Task 5: Indeler en domeinbewuste test-gate

**Files:**
- Create: `ci/domains.py`, `tests/_domains.py`, `tests/test_domains.py`
- Modify: `lighthouse.toml`, `tests/test_ci_go_test_pin.py`, `tests/test_dogfood.py:154-164`

**Interfaces:**
- Produces in `ci/domains.py` (stdlib, Python 3.11; module-docstring met het doel):
  - `DOMAINS = ("web", "python", "go")`. Dat is ook de volgorde waarin de gate ze draait.
  - `classify(paths: Iterable[str]) -> set[str]`, de regels van de spec:
    - `web`: een pad begint met `web/`, of is `.github/workflows/ci.yml`, `ci/domains.py` of
      `lighthouse.toml`. `lighthouse.toml` staat erbij omdat daar het gate-commando staat.
    - `go`: een pad begint niet met `web/`, of is `web/openapi.json` of `web/go.mod`.
    - `python`: een pad begint niet met `web/`. Ook `web/openapi.json` en `web/go.mod` niet:
      geen pytest leest onder `web/` (de spec, §6 en de testlijst, is hierop rechtgezet).
  - `CHECKS: dict[str, list[list[str]]]`:
    - `web`: `npm run --silent lint`, `npm run --silent typecheck`, `npm test`, gedraaid in
      `<root>/web`;
    - `python`: `["uv", "run", "pytest", "-q"]`;
    - `go`: `["go", "test", "-race", "-shuffle=on", "./..."]`.
  - `api_drift(web: Path, gen: list[str] = ["npm", "run", "--silent", "gen"]) -> int`: maakt een
    momentopname van `web/src/api` (relatieve paden en bytes), draait `gen` en vergelijkt. Geeft
    `gen` een exitcode die niet 0 is, dan geeft hij die terug, met `gate: npm run gen failed`,
    zonder te vergelijken. Anders `0` als niets veranderde, en `1` bij een verschil, met
    `gate: web/src/api was stale; npm run gen regenerated it — stage the result`. Hij vergelijkt met de worktree, niet met de index: een
    ververste client die nog niet gestaged is, geldt als bij.
  - `changed_paths(root: Path) -> list[str]`: de vereniging van `git diff --cached --name-only
    --no-renames -z`, `git diff --name-only --no-renames -z` en `git ls-files --others
    --exclude-standard -z`, in `root`.
  - `gate(root: Path, checks=CHECKS, drift=api_drift) -> int` en `main(argv: list[str]) -> int`
    met de subcommando's `gate` (root = cwd) en `classify [--github-output FILE]` (NUL-gescheiden
    paden op stdin; schrijft `web=true|false`, `go=…` en `python=…`, één per regel, naar stdout
    en, met de vlag, achter `FILE`).
- Produces in `tests/_domains.py`: `load() -> ModuleType`, die `ci/domains.py` laadt via
  `importlib.util.spec_from_file_location`.

Gedrag van `gate`, in deze volgorde:
1. Geen gewijzigde paden: print `gate: no changed paths — nothing to check` en geef `0`.
2. Per niet-geraakt domein één regel: `gate: <domein> skipped — no path in the domain`.
3. Is `web` geraakt maar ontbreekt `node` of `npm` op PATH, of `web/node_modules`: print
   `gate: COULD NOT CHECK — <reden>; fix: cd web && npm ci` en geef `3`. Dit gebeurt vóór er een
   check draait.
4. Draai per geraakt domein, in de volgorde van `DOMAINS`, de checks. Bij `web` komt eerst
   `drift(web)`. Print vóór elke check `gate: <domein> — <commando>`, met `flush=True`. Stop bij de
   eerste exitcode die niet 0 is en geef die terug. Een commando dat niet bestaat
   (`FileNotFoundError`) wordt `gate: COULD NOT CHECK — <naam> not found`, met exitcode `3`.
5. Alles groen: de laatste regel is `gate: passed — <geraakte domeinen>`. De hook toont die
   laatste regel.

- [ ] **Step 1: Schrijf de falende tests** in `tests/test_domains.py`. Eén fixture, `repo`, maakt
  in `tmp_path` een git-repo met een eerste commit (`internal/a.go`, `web/x.tsx`, `README.md`).
  Elke git-aanroep krijgt een eigen identiteit via `GIT_AUTHOR_*`/`GIT_COMMITTER_*` in `env`,
  zoals `GIT_ENV` in `tests/pack/_testkit.py`: een CI-runner heeft geen globale `user.name`.
  Tabeltest `test_classify` (geparametriseerd):

| paden | domeinen |
|---|---|
| `web/src/x.tsx` | `{"web"}` |
| `internal/a.go` | `{"go", "python"}` |
| `web/openapi.json` | `{"web", "go"}` |
| `web/go.mod` | `{"web", "go"}` |
| `docs/x.md` | `{"go", "python"}` |
| `.github/workflows/ci.yml` | `{"web", "go", "python"}` |
| `ci/domains.py` | `{"web", "go", "python"}` |
| `lighthouse.toml` | `{"web", "go", "python"}` |
| `webapp/x.ts` | `{"go", "python"}` |
| (leeg) | `set()` |

  Gate-tests. Ze roepen `gate(repo, checks=…, drift=lambda web: 0)` aan met nepchecks
  (`["sh", "-c", "exit 7"]`, of `["sh", "-c", "touch <marker>"]` om te zien dat iets draaide).
  Een fixture `web_ready` maakt `web/node_modules/` aan en monkeypatcht `shutil.which` zodat
  `node` en `npm` een pad geven. Elke test die `web` raakt en verwacht dat een check draait,
  gebruikt hem; anders geeft de preflight (gedrag 3) `3` vóór er iets draait. Alleen de twee
  `web_without`-tests gebruiken hem niet.
  - `test_gate_untracked_file_counts`: een nieuw `internal/new.go`, niet gestaged → de go-check
    draait.
  - `test_gate_unstaged_change_counts`: `internal/a.go` gewijzigd, niet gestaged → de go-check
    draait.
  - `test_gate_rename_across_border_runs_go` (met `web_ready`): `git mv internal/a.go web/a.go`
    → de go-check draait.
  - `test_gate_quoted_path_is_web` (met `web_ready`): een nieuw `web/café ü.tsx` → de web-check
    draait.
  - `test_gate_nothing_changed`: een schone repo → `0`, de uitvoer bevat `nothing to check`, en er
    draaide geen check.
  - `test_gate_reports_skipped`: alleen `web/x.tsx` gewijzigd →
    `gate: go skipped — no path in the domain` en dezelfde regel voor `python`.
  - `test_gate_web_without_node_modules`: `web/x.tsx` gewijzigd, geen `web/node_modules` → `3`;
    de uitvoer bevat `COULD NOT CHECK` en `cd web && npm ci`, en er draaide geen check.
  - `test_gate_web_without_node`: idem met `node_modules`, maar `shutil.which` (monkeypatch)
    geeft `None` voor `node` → `3`.
  - `test_gate_stops_at_first_failure`: python-check `exit 7`, go-check met marker → `7`, en de
    marker ontbreekt.
  - `test_gate_missing_command`: python-check `["lighthouse-no-such-cmd"]` → `3`, met `not found`.
  - `test_api_drift`: een `web/src/api/a.ts`; `gen` als `sh -c` die het bestand gelijk laat → `0`.
    `gen` die het wijzigt → `1`. `gen` die `b.ts` toevoegt → `1`. `gen` als `exit 5` die niets
    wijzigt → `5`, met `npm run gen failed`.
  - `test_classify_cli_github_output`: `main(["classify", "--github-output", f])` met stdin
    `b"web/x.tsx\0docs/y.md\0"` → het bestand bevat `web=true`, `go=true` en `python=true`.

- [ ] **Step 2: Run:** `uv run pytest tests/test_domains.py -q` → FAIL (`ci/domains.py`
  ontbreekt).

- [ ] **Step 3: Implementeer `ci/domains.py`** volgens de Interfaces hierboven.

- [ ] **Step 4: Run:** `uv run pytest tests/test_domains.py -q` → PASS.

- [ ] **Step 5: Mutatiecheck.** Verruim de uitsluiting van `go` tot alles met `web/` als
  voorvoegsel, `openapi.json` inbegrepen → de rij `web/openapi.json` rood. Haal `--no-renames`
  weg → `test_gate_rename_across_border_runs_go` rood. Haal `-z` weg →
  `test_gate_quoted_path_is_web` rood. Draai alles terug.

- [ ] **Step 6: Schakel de gate om en verhuis de pins.** In `lighthouse.toml`:

```toml
[test_gate]
# ci/domains.py runs only the checks of the domains a change touches (see the script).
command = "python3 ci/domains.py gate"
# 3: ci/domains.py could not check (no node, no web/node_modules, a command not found).
unchecked_exit_codes = [3]
```

  `tests/test_ci_go_test_pin.py::_gate_go_command` leest voortaan `tests._domains.load().CHECKS`.
  Precies één check begint met `["go", "test"]`, en die geeft hij terug als string. De andere twee
  tests daar blijven ongewijzigd, `len(runs) == 2` inbegrepen.
  `tests/test_dogfood.py::test_test_gate_shuffles_with_race` stelt twee dingen vast: de uitvoer van
  `config test_gate.command` is `python3 ci/domains.py gate`, en `" ".join(load().CHECKS["go"][0])`
  is `go test -race -shuffle=on ./...`.

- [ ] **Step 7: Run:** `uv run pytest -q` → PASS. Draai `python3 ci/domains.py gate` uit de
  worktree-root met de wijzigingen van deze task. Verwacht: drie geraakte domeinen, want
  `ci/domains.py` en `lighthouse.toml` raken `web`. Dan web-checks, pytest en `go test`, en als
  laatste regel `gate: passed — web, python, go`. Meet de duur (`time`) en vergelijk met
  `python3 .lighthouse/lib/lighthouse_lib/config.py test_gate.deadline_seconds` (standaard 270).
  Ligt hij boven 80 % daarvan, zet dan `deadline_seconds = 280` onder `[test_gate]` (het maximum
  van de hook); ligt hij erboven, meld dat als open punt.

- [ ] **Step 8: Commit:** `feat(ci): indeler per domein, en een test-gate die alleen geraakte checks draait (#68)`.
  Deze commit gaat al door de nieuwe gate.

### Task 6: CI met een job per domein en een `binary`-job

**Files:**
- Modify: `.github/workflows/ci.yml`
- Create: `ci/spa-smoke.sh`, `tests/test_ci_domains_pin.py`

**Interfaces:**
- Consumes: `python3 ci/domains.py classify --github-output "$GITHUB_OUTPUT"` (Task 5); de
  npm-scripts (Task 3, 4); de merktekens (Global Constraints).
- Produces: `ci/spa-smoke.sh <binary>`. Start `<binary> serve` op een vrije poort (gekozen via
  `python3`, met `socket.bind(("127.0.0.1", 0))`) met `--db` in een `mktemp -d`. Wacht tot 10 s op
  `/api/health`. Eist dat `/` `name="lighthouse-ui" content="spa"` bevat en dat het eerste
  `/assets/…js` uit die pagina 200 geeft. Ruimt via `trap` op. Exit 0 als alles klopt, anders 1
  met een Engelse regel op stderr.

Vorm van `ci.yml` (alleen `on: pull_request`, geen `paths:`):
- `changes`: checkout met `fetch-depth: 0`. Stap `id: domains` met `env` `BASE: ${{
  github.event.pull_request.base.sha }}` en `HEAD: ${{ github.event.pull_request.head.sha }}`;
  `run: git diff -z --name-only --no-renames "$BASE...$HEAD" | python3 ci/domains.py classify
  --github-output "$GITHUB_OUTPUT"`. `outputs:` `web`, `go` en `python` uit `steps.domains`.
- `python` en `go`: hun huidige stappen ongewijzigd, met `needs: changes` en
  `if: needs.changes.outputs.<domein> == 'true'`.
- `web`: `needs: changes`, `if: needs.changes.outputs.web == 'true'`, `defaults.run.working-directory:
  web`. Stappen: `actions/setup-node` met `node-version-file: web/.nvmrc`; `npm ci`;
  `npm run lint`; `npm run typecheck`; `npm test`; drift:
  `npm run gen && git status --porcelain -- src/api && test -z "$(git status --porcelain -- src/api)"`;
  `npm run build`; `actions/upload-artifact` met `name: web-dist` en `path: internal/web/dist`.
  `git status` vangt ook een nieuw bestand, `git diff` niet.
- `binary`: `needs: [changes, web]`, met
  `if: ${{ !cancelled() && needs.changes.result == 'success' && (needs.changes.outputs.web == 'true' || needs.changes.outputs.go == 'true') && needs.web.result != 'failure' }}`.
  Stappen: checkout; setup-go. Als `needs.web.result == 'success'`: `actions/download-artifact`
  (`web-dist` naar `internal/web/dist`). Anders: setup-node en `npm ci && npm run build` in `web`.
  Dan `CGO_ENABLED=0 go build ./cmd/lighthouse` en `ci/spa-smoke.sh ./lighthouse`. Geen
  `go test` in deze job: de pin telt precies twee `go test`-stappen.

- [ ] **Step 1: Schrijf de falende pins** in `tests/test_ci_domains_pin.py`. Een helper
  `_jobs() -> dict[str, str]` knipt `ci.yml` per job: de regels met twee spaties inspringing onder
  `jobs:`. Tekstchecks, geen YAML-pakket:
  - `test_no_paths_filter`: het `on:`-blok bevat geen `paths`.
  - `test_domain_jobs_wait_for_changes`: `python`, `go` en `web` bevatten elk `needs: changes` en
    `needs.changes.outputs.<job> == 'true'`.
  - `test_changes_job_classifies`: `changes` bevat `fetch-depth: 0`, `--no-renames`, `-z` en
    `ci/domains.py classify`.
  - `test_web_job_runs_every_check`: `web` bevat `node-version-file: web/.nvmrc`, `npm ci`,
    `npm run lint`, `npm run typecheck`, `npm test`, `npm run gen`,
    `git status --porcelain -- src/api` en `npm run build`.
  - `test_binary_builds_after_web`: in `binary` staat `web` in `needs`, en de `if` bevat
    `!cancelled()`, `outputs.go == 'true'` en `needs.web.result != 'failure'`. De job draait
    `CGO_ENABLED=0 go build ./cmd/lighthouse` en `ci/spa-smoke.sh`.
  - `test_actions_pinned_by_sha`: elke `uses:` matcht `@[0-9a-f]{40}(\s|$)`; de bestaande
    regels dragen een `# vX.Y.Z`-commentaar achter de SHA.
  - `test_smoke_script_executable`: `git ls-files -s ci/spa-smoke.sh` begint met `100755`.

- [ ] **Step 2: Run:** `uv run pytest tests/test_ci_domains_pin.py -q` → FAIL.

- [ ] **Step 3: Schrijf `ci.yml` en `ci/spa-smoke.sh`.** De SHA's zijn opgezocht op 2026-10-26
  (nieuwste release-tag; geen `gh` nodig):
  - `actions/setup-node@949feb2413d6458794dcd2491c4babbbce0c15c1 # v7.1.0`
  - `actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7.0.2`
  - `actions/download-artifact@9000827ccba6bdab643e8b6fd33ac0654aef8333 # v8.0.2`

  `chmod +x ci/spa-smoke.sh` en stage hem, zodat git hem als `100755` volgt. Werk de kopregel van
  `ci.yml` bij: die noemt nu twee lanes.

- [ ] **Step 4: Run:** `uv run pytest -q` → PASS, `tests/test_ci_go_test_pin.py` inbegrepen. Als
  `actionlint` er is: `actionlint .github/workflows/ci.yml` → geen bevindingen.

- [ ] **Step 5: Bewijs de smoke beide kanten op.** Bouw de frontend (`cd web && npm run build`),
  dan `CGO_ENABLED=0 go build -o <scratch>/lh ./cmd/lighthouse` en `ci/spa-smoke.sh <scratch>/lh`
  → exit 0. Bouw opnieuw met alleen de placeholder: verplaats `internal/web/dist` tijdelijk naar
  de scratchpad en zet een lege `dist/.gitkeep` terug. Dan geeft `ci/spa-smoke.sh` → exit 1 met
  de melding over het merkteken. Zet `dist` terug en controleer: `git status --porcelain` is leeg.

- [ ] **Step 6: Mutatiecheck.** Haal `web` uit `needs` van `binary` →
  `test_binary_builds_after_web` rood. Haal de lintstap weg → `test_web_job_runs_every_check`
  rood. Draai terug.

- [ ] **Step 7: Commit:** `ci: job per domein via ci/domains.py, en een binary-job met de echte SPA (#68)`

## Afronding

- [ ] `go run ./cmd/lighthouse init --upgrade </dev/null` → als laatste regel `No changes.`
  (#68 raakt `pack/` niet).
- [ ] De KPI-tabel van `/lighthouse-go-writing`, per rij met de omgeving. Rij 6 (de test-gate)
  zegt op een schone boom `nothing to check` en bewijst dan niets: meld hem met die uitvoerregel,
  en draai als eigen bewijs `cd web && npm run lint && npm run typecheck && npm test` en
  `uv run pytest -q`.
- [ ] Een verse worktree zonder `web/node_modules`
  (`git worktree add --detach <scratch>/fresh HEAD`; de branch zelf staat hier al uit):
  `CGO_ENABLED=0 go build ./... && go test ./... && golangci-lint run ./...` → groen.
  Daarna de worktree weghalen.
- [ ] De CI-run op de PR: alle vijf jobs groen. De log van `binary` toont de smoke-regel. Dat
  bewijs gaat in de Evidence-tabel van `/lighthouse-verify-pr`.
