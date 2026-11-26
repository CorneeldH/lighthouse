# init zaait een dependabot.yml Implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `lighthouse init` zaait een `.github/dependabot.yml` met precies de ecosystemen die de repo bevat, laat een bestaand bestand ongemoeid en meldt dat in één regel.

**Architecture:** Een nieuw bestand `internal/initcmd/dependabot.go` detecteert ecosystemen via de bestaande `lsFiles` (git ls-files, `select.go`), rendert het bestand in Go en plant het in `buildPlan` naast de labels- en richtingsdocumenten: alleen zaaien als het ontbreekt, niet in het manifest, één stderr-regel als het bestaat. De self-hosted-les van paia #608 wordt een waarschuwing van `init` plus een test op lighthouses eigen workflows; `init` schrijft zelf geen workflow.

**Tech Stack:** Go (`internal/initcmd`), Dependabot-configuratie `version: 2`.

**Spec:** geen aparte spec; de issue (#617) is de spec. Het HOE volgt uit de acceptatiecriteria en de scout-bevindingen (`initcmd.go:637-653`, `direction.go:166-180`, `select.go:lsFiles`).

## Global Constraints

- Het bestand staat op `.github/dependabot.yml`, het is van de repo en komt niet in het manifest (`initcmd.go:637-648`).
- Bestaat het bestand: één regel op stderr `dependabot: .github/dependabot.yml exists; left as it is`, nooit overschrijven, ook niet met `--force`.
- Alleen bij forge-kind `github` (de variabele `kind` uit `configTables`, `initcmd.go:220`); anders niets, ook geen regel.
- Cadans `weekly`; één groep `all` met `patterns: ["*"]` per ecosysteem.
- Detectie op door git gevolgde (of nieuwe, niet genegeerde) bestanden, zoals `lsFiles`; paden onder `testdata/`, `node_modules/` en `vendor/` tellen niet mee (lighthouse heeft fixtures met `go.mod` en `package.json`).
- Voor elke commit: `lighthouse imports check` naast `[test_gate] command`; Conventional Commits in het Nederlands; nooit `--no-verify`.
- Een wijziging in `internal/` die bepaalt wat `init` schrijft vraagt `go run ./cmd/lighthouse init --upgrade` en een commit van de installatie (CI eist `No changes.`).

## Review Focus

- Een repo zonder enig herkend ecosysteem: geen bestand, één regel `dependabot: no ecosystems found; nothing seeded`.
- Monorepo met `go.mod` in `/` en `/web`: één `gomod`-blok met `directories: ["/", "/web"]`, geen twee blokken.
- Python met `uv.lock` naast `pyproject.toml` wordt `uv`, zonder `uv.lock` wordt het `pip`.
- Fixtures onder `testdata/` mogen geen ecosysteem opleveren.
- Tweede `init --upgrade` op een repo met het bestand: stdout `No changes.`, bestand byte-voor-byte gelijk.

---

### Task 1: Ecosystemen detecteren

**Files:**
- Create: `internal/initcmd/dependabot.go`
- Create: `internal/initcmd/dependabot_test.go`

**Interfaces:**
- Produces: `type ecosystem struct { Name string; Dirs []string }` (`Dirs` zijn Dependabot-paden: `/` of `/web`, gesorteerd, uniek) en `func detectEcosystems(root string) ([]ecosystem, error)`, gesorteerd op `Name`.
- Consumes: `lsFiles(root string, pathspecs []string) ([]string, error)` uit `select.go`.

Ecosysteemnamen en herkenning (bestandsnaam in een map):

| Name | Herkenning |
|---|---|
| `github-actions` | `.github/workflows/*.yml` of `*.yaml` (map `/`), of `action.yml`/`action.yaml` in de root |
| `gomod` | `go.mod` |
| `uv` | `uv.lock` |
| `pip` | `requirements*.txt`, `Pipfile`, `setup.py`, of `pyproject.toml` zonder `uv.lock` in dezelfde map |
| `npm` | `package.json` |
| `docker` | `Dockerfile`, `Dockerfile.*` |

- [ ] **Step 1: Schrijf de falende tests** in `dependabot_test.go` (hulpfuncties `newRepo`, `write` uit de bestaande tests; bestanden moeten door git gezien worden, volg hoe `select_test.go` dat doet)
  - `TestDetectEcosystemsMonorepo`: bestanden `go.mod`, `web/go.mod`, `web/package.json`, `.github/workflows/ci.yml` geven `[github-actions /]`, `[gomod / /web]`, `[npm /web]`.
  - `TestDetectEcosystemsUvVersusPip`: map `a` met `pyproject.toml` en `uv.lock` → `uv` met `/a`; map `b` met alleen `pyproject.toml` → `pip` met `/b`.
  - `TestDetectEcosystemsIgnoresFixtures`: `internal/x/testdata/go.mod`, `node_modules/p/package.json`, `vendor/m/go.mod` geven geen ecosystemen (lege lijst, nil error).
  - `TestDetectEcosystemsDocker`: `Dockerfile` en `svc/Dockerfile.dev` geven `docker` met `/` en `/svc`.
- [ ] **Step 2: Run** `go test ./internal/initcmd -run TestDetectEcosystems` — verwacht: FAIL (`detectEcosystems` bestaat niet).
- [ ] **Step 3: Implementeer** `detectEcosystems` in `dependabot.go`: één `lsFiles`-aanroep met pathspecs (`**/go.mod`, `**/uv.lock`, `**/pyproject.toml`, `**/requirements*.txt`, `**/Pipfile`, `**/setup.py`, `**/package.json`, `**/Dockerfile`, `**/Dockerfile.*`, `.github/workflows/*.yml`, `.github/workflows/*.yaml`, `action.yml`, `action.yaml`), filter paden met een segment `testdata`, `node_modules` of `vendor`, klassificeer per bestandsnaam en groepeer mappen per ecosysteem. De map `.` wordt `/`, andere mappen `/<pad>`.
- [ ] **Step 4: Run** dezelfde tests — verwacht: PASS.
- [ ] **Step 5: Commit** `feat(init): detecteer de ecosystemen voor dependabot.yml (#617)`.

### Task 2: Renderen en zaaien in buildPlan

**Files:**
- Modify: `internal/initcmd/dependabot.go`, `internal/initcmd/initcmd.go` (`seeding` rond l.482, aanroep l.288, `buildPlan` na het richtingsblok l.649-653)
- Test: `internal/initcmd/dependabot_test.go`

**Interfaces:**
- Consumes: `detectEcosystems` (Task 1), `load func(rel string) (*planned, error)` zoals `directionPlan` het krijgt.
- Produces: `func renderDependabot(eco []ecosystem) []byte` en `func dependabotPlan(root string, o Options, load func(string) (*planned, error)) error`; constante `dependabotPath = ".github/dependabot.yml"`; veld `seeding.dependabot bool` (true bij `kind == "github"`).

`renderDependabot` levert `version: 2`, `updates:` met per ecosysteem (alfabetisch) `package-ecosystem`, `directory: "/"` voor `github-actions` en anders `directories:` met de paden, `schedule: {interval: weekly}` en `groups: {all: {patterns: ["*"]}}`. Bovenaan een commentaarregel dat `lighthouse init` het bestand zaaide en dat het van de repo is.

- [ ] **Step 1: Schrijf de falende tests**
  - `TestRenderDependabot`: voor `[{gomod,[/ /web]},{github-actions,[/]}]` bevat de uitvoer `package-ecosystem: "github-actions"` vóór `package-ecosystem: "gomod"`, `directories:` met `"/"` en `"/web"`, `interval: "weekly"` en `patterns:` met `"*"`; de uitvoer parseert als YAML (`gopkg.in/yaml.v3` als het al in `go.mod` staat, anders alleen de stringchecks).
  - `TestInitSeedsDependabot`: repo met `go.mod` en forge-kind github → `.github/dependabot.yml` bestaat, bevat `gomod`, niet in `manifest.Path`.
  - `TestInitSeedsNothingWithoutEcosystems`: lege repo → geen bestand, stderr bevat `dependabot: no ecosystems found; nothing seeded`.
  - `TestUpgradeLeavesExistingDependabot`: schrijf `# mijn\n` naar het bestand, draai `init --upgrade` en `--upgrade --force`; inhoud ongewijzigd, stderr bevat `dependabot: .github/dependabot.yml exists; left as it is` precies één keer per run.
  - `TestDependabotSkippedOnGitLab`: forge-kind gitlab → geen bestand en geen `dependabot:`-regel.
  - `TestSecondUpgradeNoChanges`: na seeden geeft een tweede `--upgrade` stdout `No changes.\n` en een gelijke snapshot (patroon van `direction_test.go:150-162`).
- [ ] **Step 2: Run** `go test ./internal/initcmd -run 'Dependabot|SeedsNothing|SecondUpgrade'` — verwacht: FAIL.
- [ ] **Step 3: Implementeer** `renderDependabot`, `dependabotPlan` (bestand bestaat → stderr-regel; geen ecosystemen → stderr-regel; anders `p.new, p.mode = body, 0o644`), voeg `dependabot bool` toe aan `seeding`, zet het op `kind == "github"` bij de constructie, en roep `dependabotPlan` aan in `buildPlan` direct na het `seeds.direction`-blok. Zonder `--force`-uitzondering: het `existed`-pad schrijft nooit.
- [ ] **Step 4: Run** `go test ./internal/initcmd/...` — verwacht: PASS, inclusief de bestaande tests (`created_test.go`, budget-tests: controleer dat het nieuwe bestand de budgetrapportage niet breekt).
- [ ] **Step 5: Commit** `feat(init): zaai een dependabot.yml per gevonden ecosysteem (#617)`.

### Task 3: Waarschuwing voor self-hosted runners

**Files:**
- Modify: `internal/initcmd/dependabot.go`, `internal/initcmd/initcmd.go` (aanroep naast `dependabotPlan`)
- Test: `internal/initcmd/dependabot_test.go`

**Interfaces:**
- Produces: `func selfHostedWorkflows(root string) ([]string, error)` die de gevolgde `.github/workflows/*.yml|yaml` teruggeeft waarvan de inhoud `self-hosted` bevat en `dependabot[bot]` niet.

`dependabotPlan` print, nadat het bestand bestaat of gezaaid wordt, per zo'n workflow op stderr: `dependabot: <pad> runs on a self-hosted runner; guard it so Dependabot PRs use a hosted runner (runs-on: ${{ github.actor == 'dependabot[bot]' && 'ubuntu-latest' || 'self-hosted' }})`.

- [ ] **Step 1: Schrijf de falende tests**
  - `TestWarnsSelfHostedWorkflow`: workflow met `runs-on: self-hosted` → stderr bevat `dependabot: .github/workflows/ci.yml runs on a self-hosted runner`.
  - `TestNoWarningWhenGuarded`: workflow met `self-hosted` en `github.actor == 'dependabot[bot]'` → geen waarschuwing.
  - `TestNoWarningHostedOnly`: workflow met `runs-on: ubuntu-latest` → geen waarschuwing.
- [ ] **Step 2: Run** `go test ./internal/initcmd -run 'SelfHosted|Guarded|HostedOnly'` — verwacht: FAIL.
- [ ] **Step 3: Implementeer** `selfHostedWorkflows` (via `lsFiles`, tekstzoek) en de aanroep in `dependabotPlan`, alleen bij `seeds.dependabot`.
- [ ] **Step 4: Run** `go test ./internal/initcmd/...` — verwacht: PASS.
- [ ] **Step 5: Commit** `feat(init): waarschuw voor self-hosted workflows naast dependabot (#617)`.

### Task 4: Lighthouse zelf, test, docs en changelog

**Files:**
- Create: `.github/dependabot.yml` (door `init`, niet met de hand)
- Create: `internal/initcmd/ownrepo_test.go` (of toevoegen aan `dependabot_test.go`)
- Create: `docs/dependabot.md`, `changelog.d/issue-617-dependabot-yml.md`
- Modify: `docs/getting-started.md` (één verwijzing)

- [ ] **Step 1: Schrijf de falende test** `TestOwnWorkflowsAreHosted`: leest `../../.github/workflows/*.yml` en eist dat geen enkele `self-hosted` bevat (borgt AC 3 voor lighthouse zelf, en faalt zodra iemand er een toevoegt zonder guard). Run — verwacht: PASS direct (er is geen self-hosted); toets met een tijdelijke `runs-on: self-hosted` in een kopie dat de test faalt, en draai dat terug.
- [ ] **Step 2: Genereer lighthouses eigen bestand:** `go run ./cmd/lighthouse init --upgrade` (bevestig met `y`); verwacht dat `.github/dependabot.yml` ontstaat met `github-actions` `/`, `gomod` `/` en `/web`, `npm` `/web`, `uv` `/`, en geen `docker`. Controleer dat niets onder `testdata/` is meegenomen; wijkt het af, los dat op in Task 1.
- [ ] **Step 3: Controleer idempotentie:** `go run ./cmd/lighthouse init --upgrade` geeft `No changes.` en één regel `dependabot: .github/dependabot.yml exists; left as it is`.
- [ ] **Step 4: Documenteer** in `docs/dependabot.md` (Nederlands, relatieve links): welke ecosystemen, cadans en groepen, dat het bestand van de repo is, de self-hosted-waarschuwing; verwijs ernaar vanuit `docs/getting-started.md`. Changelog-fragment met `type: added` volgens het formaat van de andere fragmenten in `changelog.d/`.
- [ ] **Step 5: Run** het volledige gate: `[test_gate] command` uit `lighthouse.toml`, `lighthouse imports check`, `go vet ./...`; verwacht groen.
- [ ] **Step 6: Commit** in twee commits: `feat(init): lighthouse heeft zelf een dependabot.yml (#617)` (bestand + test) en `docs(init): dependabot.yml en changelog (#617)`.
