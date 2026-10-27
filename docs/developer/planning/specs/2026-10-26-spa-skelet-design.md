# Ontwerp: SPA-skelet met orval-client, ingebed in de binary

Issue: #68 · Milestone: M1 — Romp en zicht · Datum: 2026-10-26

## Aanleiding en doel

M1 maakt de ontwikkelketen zichtbaar in een web-UI. #63 zette de romp neer: Huma op
`lighthouse serve`, een OpenAPI-beschrijving op `/api/openapi.json`, en een ingebedde
placeholder met SPA-terugval (`internal/web`, `internal/server/spa.go`). #68 zet daar de lege
SPA op: de stack, de gegenereerde client en de inbedding. De inhoud (open vragen, teruggegeven
issues, usage-venster) is #69.

De stack ligt vast sinds de spec lighthouse-scope (CorneeldH/personal_ai_agent#1017): React,
Vite, TypeScript, Tailwind en shadcn/ui, via `go:embed`, met een orval-client. Een client die
uit Huma's beschrijving gegenereerd wordt, kan niet stil uit de pas lopen met de API.

Twee eisen van de gebruiker sturen het ontwerp:

1. **Alleen de eigenaar installeert**, uit een clone. Een binary met de echte UI via
   `go install …@main` is niet nodig. Release-binaries voor anderen zijn #78 (M3).
2. **Een check draait alleen als de wijziging hem raakt**, bij een commit en bij een PR. Een
   Go-check draait niet bij een wijziging die alleen frontend raakt, en andersom. Dat geldt ook voor
   de bestaande checks (pytest, `go test`, golangci-lint). #68 regelt dit lokaal voor deze repo.
   De generieke versie in de pakketten is #80.

## Scope

#68 levert:

1. een frontendmap `web/` met Vite, React, TypeScript, Tailwind, shadcn/ui en vitest, een
   `package-lock.json` en een vastgepinde Node-versie;
2. een orval-client in `web/`, gegenereerd uit een gecommitte `web/openapi.json`;
3. een Go-test die `web/openapi.json` gelijk houdt aan de echte OpenAPI-beschrijving;
4. de inbedding: Vite schrijft naar `internal/web/dist/`; zonder build serveert de binary een
   placeholder;
5. één pagina die `/api/health` via de orval-client ophaalt en de versie toont;
6. een indeler die per wijziging bepaalt welke domeinen geraakt zijn, met de test-gate en CI als
   afnemers;
7. CI met een job per domein, en een job die frontend en binary samen bouwt;
8. `docs/install.md` met de frontendbuild vóór `go install`.

**Buiten scope:**

- Inhoud van de UI, routing, tanstack-query of state-management (#69 en later).
- Release-binaries en `go install …@main` met echte UI (#78).
- Domeinbewuste pakketten: `pre-commit-tests.sh`, `go-check.sh`, `ty-check.sh` en een
  `pack/node` (#80). `go-check.sh` blijft dus in #68 bij elke Stop lintten, ook na een beurt
  met alleen frontend.
- Playwright en het screenshot uit de DoD van M1: dat bewijs hoort bij #69, als er iets te zien is.

## Non-goals en anti-goals

- **Geen gebouwde frontend in git.** Gehashte JS en CSS in elke diff, en een tweede driftcheck,
  zijn de prijs die we niet betalen (besluit gebruiker, optie B).
- **Geen `go build` die Node nodig heeft.** Een verse worktree zonder Node moet bouwen, testen en
  lintten; `go-check.sh` draait bij elke Stop en compileert `internal/web`.
- **Anti-goal: een check die ten onrechte overslaat.** Een overgeslagen check die wel geraakt
  was, is erger dan een check te veel. Bij twijfel draait hij.

## Besluiten

### 1. Mappen

- `web/` in de root: `package.json`, `package-lock.json`, `.nvmrc`, Vite-, TypeScript-, ESLint-,
  orval- en shadcn-config (`components.json`), `src/`, en de gecommitte `openapi.json`.
- `web/src/api/`: de gegenereerde client, gecommit. Lint slaat hem over; de typecheck niet.
- `internal/web/dist/`: de uitvoer van `vite build` (`build.outDir` = `../internal/web/dist`,
  `emptyOutDir: true`). In `.gitignore`, op een `.gitkeep` na. `emptyOutDir` gooit die
  `.gitkeep` bij elke build weg. Daarom staat er een kopie in `web/public/.gitkeep`, en Vite zet
  die terug. Na een build is `git status` schoon.
- `internal/web/placeholder/index.html`: de placeholder, gecommit. Hij verhuist uit `dist/`.
- `web/go.mod`: een lege module. `./...` stopt bij een geneste module, dus `go build`, `go test`
  en golangci-lint lopen niet door `web/node_modules`. Sommige npm-pakketten leveren zelf
  `.go`-bestanden mee.

### 2. Inbedding en `go build` zonder frontend

`internal/web` embedt beide mappen:

```go
//go:embed all:dist placeholder
```

`all:` is nodig: zonder dat prefix telt `.gitkeep` niet mee, en `go:embed` faalt op een map
zonder bestanden. `FS()` geeft `dist` terug als `dist/index.html` bestaat, anders `placeholder`.

Dit legt het laatste AC vast. **`go build` slaagt altijd, ook zonder gebouwde frontend.** De
binary serveert dan de placeholder op `/`. Die zegt in één regel dat de frontend niet gebouwd is,
met het commando: `cd web && npm ci && npm run build`. De SPA-terugval van #63 blijft gelijk. Ook
voor de placeholder krijgt elk pad buiten `/api/` `index.html` met 200.

De tekst van de placeholder is Engels, gelijk aan de installatiegids.

### 3. OpenAPI en orval

De keten loopt in twee stappen, en elke stap hoort bij één domein:

1. **Go → `web/openapi.json`.** Een Go-test in `internal/server` haalt `/api/openapi.json` op via
   `New(...)` en `httptest`, zonder netwerk (patroon: `TestOpenAPI`). Hij vergelijkt het
   antwoord met `web/openapi.json`. Wijkt het af, dan faalt hij, met de melding hoe je ververst.
   Met een vlag (`-update`, de naam is aan de bouwer) schrijft hij het bestand opnieuw. De test
   valt onder de bestaande `go test -race -shuffle=on ./...`. `ci.yml` krijgt dus geen derde
   `go test`-regel, en de pin in `tests/test_ci_go_test_pin.py` (`len(runs) == 2`) blijft staan.
2. **`web/openapi.json` → `web/src/api/`.** `npm run gen` draait orval. De frontendcheck draait
   orval opnieuw en eist `git diff --exit-code -- web/src/api`.

Wie de API verandert, ziet stap 1 rood. Daarna ververst hij `web/openapi.json`. Die wijziging
valt in het frontenddomein, dus stap 2 draait vanzelf.

orval genereert een `fetch`-client met relatieve paden (`/api/...`), zonder tanstack-query. #69
kiest of er een query-laag bij komt.

**Risico:** Huma levert standaard OpenAPI 3.1. Werkt orval daar niet goed mee, dan schrijft stap 1
de 3.0-versie die Huma ook levert (`/api/openapi-3.0.json`). Het plan meet dat vóór de keuze.

### 4. De pagina

Eén React-component haalt `/api/health` op via de gegenereerde functie (operationId
`get-health`). Hij toont `lighthouse <version>`, of een foutmelding als de fetch faalt. Gebruik
een shadcn-component, bijvoorbeeld `Card`, zodat de pijplijn van shadcn en Tailwind bewezen is.

De pagina rendert in de browser. Een smoke zonder browser ziet de versie dus niet. Het bewijs is
een componenttest met vitest en Testing Library, met een nep-fetch. Die twee horen bij de stack
van #68.

Ontwikkelen: `npm run dev` in `web/`, met een Vite-proxy van `/api` naar `http://127.0.0.1:8127`
(de standaard van `serve`). Met `changeOrigin: true` is de `Host` `127.0.0.1:8127`, en dat laat
`hostGuard` door.

### 5. Node

- `.nvmrc` pint Node 22 op een exacte versie. Lokaal staat `v22.22.2`; `engines.node` in
  `package.json` zegt hetzelfde. CI leest `.nvmrc` (`actions/setup-node` met
  `node-version-file`, vastgepind op een SHA zoals de andere actions).
- npm met `package-lock.json`; installeren met `npm ci`.

*Mijn keuze.* Node 22 is wat lokaal draait. Node 22 is in onderhoud tot april 2027. Een overstap
op een nieuwere LTS is één regel in `.nvmrc`.

### 6. Domeinen: welke check draait wanneer

Eén indeler bepaalt uit een lijst gewijzigde paden welke domeinen geraakt zijn. De test-gate
en CI gebruiken dezelfde indeler, zodat ze niet uit elkaar lopen.

| Domein | Checks | Geraakt als ten minste één pad … |
|---|---|---|
| `web` | `npm ci`, lint, typecheck, vitest, orval-drift, `vite build` | onder `web/` valt, of `.github/workflows/ci.yml` is, of de indeler of het gate-script zelf is |
| `go` | golangci-lint, `go build`, `go test`, e2e, pack-match | **niet** onder `web/` valt, of `web/openapi.json` is |
| `python` | `uv run pytest -q`, `ci/literal-grep.sh` | **niet** onder `web/` valt |

De regels voor `go` en `python` werken met uitsluiting: zo'n check slaat alleen over als elk pad
aantoonbaar buiten zijn domein ligt. Een nieuw pad dat nog niemand kent (`docs/`, `pack/`, een
nieuwe map) laat ze dus draaien. Dat volgt uit de anti-goal.

`python` sluit Go-bestanden niet uit:
`tests/pack/test_code_language_corpus.py` leest `internal/**/*.go` en `cmd/**/*.go`, en controleert
dat de code Engels is. Een commit met alleen Go draait dus ook pytest.

`web` werkt andersom: een check draait alleen als hij geraakt wordt, want de frontend leest
aantoonbaar niets buiten `web/`. Vite heeft `web/` als root, en `node_modules` staat daar ook in.
De uitzonderingen zijn de bestanden die de checks zelf vastleggen: `ci.yml`, de indeler en het
gate-script. Een wijziging daarin draait de `web`-checks mee.

Een smallere uitsluiting mag alleen met bewijs dat geen test van dat domein het pad leest. Een
voorbeeld: Markdown onder `docs/` buiten het Go-domein. De pytest-suite leest bijvoorbeeld via
`ROOT / …` uit `.claude/`, `pack/`, `ci/`, `docs/` en `.github/`. Het plan meet per uitsluiting
wat de tests lezen, of houdt de tabel zoals hij hier staat.

**Lokaal (de test-gate).** `[test_gate] command` in `lighthouse.toml` wordt een script in deze
repo. Het script vraagt de indeler om de domeinen en draait alleen die checks. De gate draait
vóór de `git commit` uit (PreToolUse). Op dat moment is een `git add` in dezelfde aanroep nog
niet gebeurd, en `-a` ook niet. Daarom zijn de gewijzigde paden de vereniging van drie bronnen:

- gestaged: `git diff --cached --name-only`;
- gewijzigd maar niet gestaged: `git diff --name-only`;
- nieuw en niet genegeerd: `git ls-files --others --exclude-standard`.

Dat is een overschatting: het kan een check te veel geven, nooit een te weinig. Lege lijst: geen
check, exit 0, met een melding. Het script meldt per overgeslagen domein één regel
(`gate: go overgeslagen — geen pad in het domein`). Het eindigt met de exitcode van de eerste
check die faalt, zodat `unchecked_exit_codes` van de hook blijft werken.

In #68 draait de lokale gate wat hij nu ook draait: pytest en `go test`. Daar komen voor het
`web`-domein lint, typecheck, vitest en de orval-drift bij. `vite build` hoort bij CI.

De gate draait zelf geen `npm ci`: dat vraagt netwerk, schrijft in de worktree en kost tijd van
de gedeelde `deadline_seconds`. Is `web` geraakt, maar ontbreekt `node`, of `web/node_modules`,
dan blokkeert de gate met `COULD NOT CHECK — <reden>`. Hij noemt dan `cd web && npm ci` als
oplossing. Dat volgt de regel van de hooks: wie niet kan controleren, blokkeert. Stil overslaan
mag niet.

**CI.** Een job `changes` draait de indeler op `git diff --name-only <base>...<head>`
(checkout met `fetch-depth: 0`) en zet per domein een output. De andere jobs hebben
`needs: changes` en `if: needs.changes.outputs.<domein> == 'true'`. Er komt geen `paths:` op
de workflow: een workflow die niet start, laat een verplichte check op "pending" staan. Een job
die via `if` wordt overgeslagen, telt als geslaagd.

Jobs:

- `python`: zoals nu, als `python` geraakt is.
- `go`: zoals nu (lint, `CGO_ENABLED=0 go build ./...`, `go test`, e2e, pack-match), als `go`
  geraakt is. Hij bouwt tegen de placeholder.
- `web`: `npm ci`, lint, typecheck, orval-drift en `vite build`, als `web` geraakt is. De build
  gaat als artefact naar `binary`.
- `binary`: als `web` of `go` geraakt is, na `web`. Hij pakt de frontendbuild uit in
  `internal/web/dist/` en draait `CGO_ENABLED=0 go build ./cmd/lighthouse`. Daarna start hij
  `serve` op een vrije poort, met een tijdelijke `--db`, en bewijst dat `/` de echte SPA serveert
  en niet de placeholder. Dit is het AC "CI bouwt de frontend vóór de Go-build".

  Draait alleen `go` en niet `web`, dan bouwt `binary` zelf de frontend. Dan bewijst ook een PR
  met alleen Go dat de ingebedde SPA nog werkt.

Twee pins lezen nu het commando uit `[test_gate] command`: `tests/test_ci_go_test_pin.py` en
`tests/test_dogfood.py::test_test_gate_shuffles_with_race`. Wordt dat commando een script, dan
lezen beide pins de `go test`-regel uit dat script. De eisen blijven
gelijk: dezelfde vlaggen in de gate en in CI, precies twee `go test`-stappen, en
`CGO_ENABLED=0 go build ./...`.

### 7. Documentatie

- `docs/install.md`: in de clone eerst `cd web && npm ci && npm run build`, dan
  `go install ./cmd/lighthouse`. De pagina vermeldt dat `go install …@main` alleen de
  placeholder levert, en verwijst naar #78.
- `web/README.md` (Engels): `npm run dev`, `npm run gen`, de verversstap van
  `web/openapi.json`, en de regel dat `web/src/api/` gegenereerd is.

## Teststrategie

Per AC de test die faalt als het gedrag breekt:

| AC | Test | Mutatie die hem rood maakt |
|---|---|---|
| frontendmap, lockfile, gepinde Node | CI-job `web` draait `npm ci` (faalt zonder of met verouderde lockfile); `setup-node` leest `.nvmrc` | `package-lock.json` verwijderen; een dependency in `package.json` wijzigen zonder lock |
| orval genereert een getypeerde client | de typecheck van `web/` compileert de pagina tegen `web/src/api/` | de functie voor `get-health` hernoemen in de client |
| CI faalt bij een achterlopende client | Go: de drifttest op `web/openapi.json`; web: `git diff --exit-code` na orval | een veld aan de health-respons toevoegen zonder te verversen; de client met de hand wijzigen |
| SPA via `go:embed`, op `/` | unit: `FS()`-keuze met een `fstest.MapFS`, met en zonder `dist/index.html`; CI-job `binary`: `/` bevat een merkteken van de Vite-build | `FS()` altijd `placeholder` laten geven |
| pagina toont de versie | vitest-componenttest met een nep-fetch die `{"version": "x.y.z"}` geeft: de gerenderde tekst bevat `x.y.z`; een tweede geval met een falende fetch toont de foutmelding | de component een vaste tekst laten tonen |
| CI bouwt frontend vóór Go, lint en typecheck | de jobs en hun `needs` in `ci.yml`; een pytest-pin naast `test_ci_go_test_pin.py` | de `needs` van `binary` weghalen; de lintstap weghalen |
| gedrag zonder gebouwde frontend | Go-test: met alleen `.gitkeep` in `dist/` geeft `/` de placeholder; de bestaande `TestEmbeddedSiteHasIndex` blijft groen in een verse checkout | de `all:` uit de embed-regel weghalen (dan faalt de build) |

De indeler en het gate-script krijgen eigen tests (pytest, tabelgedreven):

- alleen `web/x.tsx` → alleen `web`;
- alleen `internal/a.go` → `go` en `python`;
- `web/openapi.json` → `web` en `go` (geen `python`: het pad valt onder `web/`, zie de tabel in §6);
- `docs/x.md` → `go` en `python`;
- `.github/workflows/ci.yml` → `web`, `go` en `python`;
- `web` geraakt zonder `web/node_modules` → `COULD NOT CHECK`, met `npm ci` in de melding;
- een nieuw, niet-gestaged bestand telt mee;
- een lege lijst → niets, met exit 0 en een melding;
- een falende check → het script eindigt met zijn exitcode.

Mutatie: de uitsluiting van `go` verruimen tot alles met `web/` als voorvoegsel, `openapi.json`
inbegrepen. Dan faalt de rij `web/openapi.json`.

## Aan de bouwer

- De exacte versies van Vite, React, TypeScript, Tailwind (v4), shadcn, orval en ESLint.
- De naam en de taal (shell of Python, alleen stdlib) van de indeler en het gate-script. Eén
  script, met de test-gate en CI als afnemers.
- De naam van de verversvlag van de drifttest.
- Het merkteken waarmee `binary` de echte SPA van de placeholder onderscheidt.
- Hoe `binary` de artefactvorm van de `web`-build overneemt.
- De vorm en de opmaak van de pagina en de placeholder, binnen besluit 2 en 4.
