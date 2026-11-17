# `lighthouse agent-audit` — implementatieplan

> **Voor agentische uitvoerders:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development
> (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen
> gebruiken checkboxes (`- [ ]`).

**Doel:** een read-only subcommando `lighthouse agent-audit` dat de agentische configuratie van een
projectrepo inventariseert, conflicten en overlapkandidaten meldt en het skill-oppervlak meet,
plus een skill die over de kandidaten oordeelt en een samenvatting bij de eerste `init`.

**Architectuur:** een package `internal/agentaudit` verzamelt per bron (lighthouse, project,
lokaal, gebruiker, plugin) een `Inventory` en leidt daaruit meldingen af; `internal/cli/agentaudit.go`
is de cobra-schil naar het voorbeeld van `worktree-audit`. Lighthouse-hooks krijgen een signatuur
in een nieuw pakketbestand `guards.toml`, dat `packs.Load` leest en niet installeert. `init` roept
de audit aan via een functie in `initcmd.Options`.

**Tech stack:** Go, cobra, `github.com/BurntSushi/toml`, `internal/orderedjson`, `internal/settings`,
`internal/packs`, `internal/manifest`, `internal/config`.

**Spec:** [2026-11-16-issue-47-agent-audit-design.md](../specs/2026-11-16-issue-47-agent-audit-design.md)

## Globale eisen

- Alles in Go; geen Python in wat `init` installeert. Go volgt `/lighthouse-go-standard`, tests
  `/lighthouse-test-standard`.
- Namen, commentaar en bestandsnamen in het Engels; de uitvoer van het commando volgt de spec (§7:
  Nederlandse kopjes `Bronnen`, `Conflicten`, `Overlap, kandidaten`, `Oppervlak`, `Inventaris`).
- De check schrijft niets. Elke test die `Run` aanroept, vergelijkt een hash van de boom vóór en na.
- `COULD NOT CHECK — <reden>` (em-dash) voor wat niet te meten is, nooit stilte.
- Exitcodes: 0; 1 alleen met `--strict` en een melding buiten `ignore`; 2 bij `COULD NOT CHECK`
  van het geheel, of met `--strict` bij een `COULD NOT CHECK` van een bron of hook, behalve de vaste
  bron `managed`.
- Tests zetten `HOME` en `CLAUDE_CONFIG_DIR` naar `t.TempDir()`; de host telt nooit mee.
- Bronnamen exact: `lighthouse`, `project`, `lokaal`, `gebruiker`, `plugin:<naam>`.
- Statussen exact: `ok`, `afwezig`, `overgeslagen (--repo-only)`, `COULD NOT CHECK — <reden>`.
- Sleutels van meldingen exact (spec §7): `blocking-pair:<event>:<matcher>:<bron>/<pad>|<bron>/<pad>`
  (paar gesorteerd), `permission:<regel>`, `missing-script:<bron>/<commando>`,
  `unregistered-script:<bron>/<pad>`, `overlap:<guard>|<bron>/<pad>`.
- Elke PR krijgt een fragment in `changelog.d/`; een wijziging onder `pack/` draait
  `go run ./cmd/lighthouse init --upgrade` en commit de geïnstalleerde kopie.

## Review-focus

1. **Een hook-commando met quotes, `&&` of een interpreter** (`bash "$CLAUDE_PROJECT_DIR"/x.sh`,
   `node ~/.claude/hooks/a.js`, `cd x && ./y.sh`): de check vindt het scriptpad, of valt terug op
   de commandoregel als inhoud; nooit een crash of een vals `missing-script`. Test in taak 4.
2. **Een settings-bestand met `hooks` maar zonder `permissions`, of met `permissions` als lijst in
   plaats van object**: de ene sectie wordt gemeten, de andere geeft `COULD NOT CHECK`, niet de
   hele bron. Test in taak 3.
3. **Een SKILL.md zonder frontmatter of met een lege description**: telt mee in het oppervlak met
   de naam van de map en 0 tekens description, en geeft geen crash. Test in taak 7.
4. **Een plugin in `enabledPlugins` die niet in `installed_plugins.json` staat**: `COULD NOT CHECK`
   voor `plugin:<naam>`, de rest gemeten. Test in taak 5.
5. **Dezelfde projecthook twee keer geregistreerd (twee events)**: één `unregistered-script`-oordeel
   per bestand, geen dubbele overlapkandidaat per event-paar met dezelfde guard. Test in taak 6.

---

### Taak 1: signaturen van lighthouse-hooks (`guards.toml`)

**Bestanden:**
- Wijzig: `internal/packs/packs.go` (doc-commentaar bovenaan, `Pack`, de top-level-tak in `Load`)
- Maak: `internal/packs/guards.go`, `internal/packs/guards_test.go`
- Maak: `pack/core/guards.toml`, `pack/go/guards.toml` *(bewust op pakketniveau en niet in `hooks/`
  zoals spec §3 zegt: `packs.go` kopieert elk bestand uit `hooks/` naar `HookDir`, en `guards.toml`
  mag niet geïnstalleerd worden)*

**Interfaces:**
- Produceert:
  ```go
  type Guard struct {
      Name    string     // hook name, the TOML table key
      Events  []string   `toml:"events"`
      Matcher string     `toml:"matcher"`
      Does    string     `toml:"does"`
      Terms   [][]string `toml:"terms"`
  }
  // Pack krijgt: Guards []Guard (gesorteerd op Name)
  ```

- [ ] **Stap 1: falende tests.** `TestLoadReadsGuards`: een `fstest.MapFS` met `guards.toml`
  (één tabel `[x]`, `events = ["PreToolUse"]`, `matcher = "Bash"`, `does = "d"`,
  `terms = [["a"],["b","c"]]`) → `p.Guards` is precies die ene `Guard`, en geen `Unit` heeft
  `Path` met `guards.toml`. `TestLoadRejectsGuardWithoutTerms`: een tabel zonder `terms` of met
  een lege groep → fout die `guards.toml` noemt. `TestEveryBlockingHookHasAGuard`: laad `core`
  uit `pack.Files`; elke regel van `pack/core/hooks/blocking` heeft een `Guard`; laad `go`: er is
  een `Guard` `go-lint-gate`.
- [ ] **Stap 2:** `go test ./internal/packs/ -run 'Guard'` → FAIL (onbekend bestand `guards.toml`).
- [ ] **Stap 3:** `parseGuards(name string, data []byte) ([]Guard, error)` in `guards.go`; in
  `Load` een top-level-tak `rel == "guards.toml"` naast `detect.toml`, niet gekopieerd; een regel
  in het doc-commentaar. Schrijf de twee `guards.toml`-bestanden: signaturen voor
  `main-commit-gate`, `commit-test-gate`, `pr-create-gate`, `bash-write-check`, `md-link-check`,
  `code-language-check` (core) en `go-lint-gate` (go). `events` en `matcher` volgen hun
  `hooks.json`; `main-commit-gate` krijgt de terms uit spec §3.
- [ ] **Stap 4:** `go test ./internal/packs/ ./internal/initcmd/` → PASS (ook de golden
  `places.golden`: `guards.toml` wordt niet geïnstalleerd).
- [ ] **Stap 5: commit** `feat(packs): guards.toml legt vast wat een lighthouse-hook bewaakt (#47)`

### Taak 2: frontmatter-lezer en hookregister delen

**Bestanden:**
- Maak: `internal/frontmatter/frontmatter.go`, `internal/frontmatter/frontmatter_test.go`
- Wijzig: `internal/checkinstr/skills.go` (de functie `frontmatterField` verhuist)
- Wijzig: `internal/hook/hook.go`, `internal/hook/hook_test.go`

**Interfaces:**
- Produceert: `frontmatter.Field(path, field string) string` (gedrag van het huidige
  `checkinstr.frontmatterField`, ongewijzigd: `""` bij ontbrekend of leeg veld, `>` en `|` gelezen);
  `hook.Names() []string` (gesorteerd).

- [ ] **Stap 1: falende tests.** `TestFieldFoldedAndLiteral` (een `description: >` over twee regels
  → één regel met spatie; `|` → met newline), `TestFieldMissingIsEmpty`, `TestFieldNoFrontmatter`.
  `TestNamesListsRegistered` (in `internal/hook/hook_test.go`, package `hook`; `internal/hooks`
  importeert `internal/hook`, dus niet importeren): registreer met `hook.Register` twee testnamen
  in omgekeerde volgorde en controleer dat `hook.Names()` ze gesorteerd bevat.
- [ ] **Stap 2:** `go test ./internal/frontmatter/ ./internal/hook/` → FAIL.
- [ ] **Stap 3:** verhuis `frontmatterField` (en `lines`, `isFile`, `startsBlank` voor zover alleen
  die het gebruikt) naar `frontmatter.Field`; `checkinstr` roept `frontmatter.Field` aan. Voeg
  `Names` toe onder de `mu`-lock.
- [ ] **Stap 4:** `go test ./internal/frontmatter/ ./internal/hook/ ./internal/checkinstr/ ./internal/cli/`
  → PASS; `check-instructions` geeft hetzelfde oordeel.
- [ ] **Stap 5: commit** `refactor(checkinstr): frontmatter-lezer in eigen package, hook.Names (#47)`

### Taak 3: bronnen en inventaris van de repo en de gebruiker

**Bestanden:**
- Maak: `internal/agentaudit/env.go`, `sources.go`, `inventory.go`, `testtree_test.go`
  (helper), `sources_test.go`

**Interfaces:**
- Consumeert: `settings.Registrations`, `orderedjson.Parse`, `manifest.Read`, `packs.Load`,
  `packs.IsLighthouseCommand`, `packs.IsLighthouseRule`, `frontmatter.Field`.
- Produceert:
  ```go
  type Env struct {
      Root      string       // repo- of worktree-root
      MainRoot  string       // main checkout (map boven --git-common-dir); == Root buiten een worktree
      ConfigDir string       // $CLAUDE_CONFIG_DIR, anders ~/.claude
      ClaudeJSON string      // $CLAUDE_CONFIG_DIR/.claude.json als die variabele gezet is, anders ~/.claude.json
      RepoOnly  bool
      Packs     []packs.Pack // de geïnstalleerde pakketten volgens het manifest
      HookNames []string     // hook.Names()
  }
  type Status struct{ Source, State, Reason string } // State: "ok"|"afwezig"|"overgeslagen (--repo-only)"|"COULD NOT CHECK"
  type Hook struct {
      Source, Event, Matcher, Command string
      Script   string // opgelost pad, "" bij een inline commando
      Content  []byte // script, of de commandoregel
      Blocking bool
      Unreadable string // reden als het script niet te lezen is
  }
  type Skill struct{ Source, Name, Kind, Path string; DescChars, NameChars int } // Kind: "skill"|"command"|"agent"
  type Rule struct{ Source, Path string; HasPaths bool }
  type Permission struct{ Source, Kind, Rule string } // Kind: "allow"|"deny"|"ask"
  type MCPServer struct{ Source, Name, Origin string } // Origin: bestand waar hij staat
  type Inventory struct {
      Statuses    []Status
      Hooks       []Hook
      Skills      []Skill
      Rules       []Rule
      Permissions []Permission
      MCP         []MCPServer
      Scripts     []string // bestanden onder .claude/hooks/ en <ConfigDir>/hooks/, buiten HookDir, met bron-prefix
  }
  func Collect(env Env) (*Inventory, error) // error alleen als Root niet bestaat
  ```

- [ ] **Stap 1: falende tests.** Met een helper `tree(t, files map[string]string) string`:
  `TestClaudeJSONHonoursConfigDir` (`DefaultEnv` met `CLAUDE_CONFIG_DIR` gezet → `<dir>/.claude.json`, zonder → `~/.claude.json`),
  `TestCollectAttributesSources` (een `.claude/settings.json` met één lighthouse-hook
  `.claude/hooks/lighthouse/run.sh main-commit-gate`, één projecthook en `allow
  Bash(lighthouse review-run:*)` plus `allow Bash(make:*)`; `settings.local.json` met `deny
  Bash(rm:*)`; `<ConfigDir>/settings.json` met één hook; skills in `.claude/skills/lighthouse-a/`,
  `.claude/skills/b/`, `<ConfigDir>/skills/c/`; een rule, een agent, `.mcp.json` met server `m`,
  `ClaudeJSON` met `mcpServers.u` en `projects[<MainRoot>].mcpServers.l`) → elk item met de
  verwachte `Source` (`review-run` → `lighthouse`, `make` → `project`, `rm` → `lokaal`, `u` →
  `gebruiker`, `l` → `lokaal`). `TestCollectInvalidUserSettings`: ongeldige JSON in
  `<ConfigDir>/settings.json` → `Status{gebruiker, "COULD NOT CHECK", …}`, project wel gemeten.
  `TestCollectRepoOnly`: `gebruiker` heeft `overgeslagen (--repo-only)`, geen items met bron
  `gebruiker`. `TestCollectPermissionsWrongShape` (review-focus 2).
  `TestCollectBlocking`: script met `exit 2` → `Blocking`; met `"permissionDecision": "deny"` →
  `Blocking`; zonder → niet; lighthouse-hook in `blocking` of met guard → `Blocking`.
- [ ] **Stap 2:** `go test ./internal/agentaudit/` → FAIL.
- [ ] **Stap 3:** implementeer `Collect` zonder plugins (taak 5). Een skill is van `lighthouse` als
  zijn pad in het manifest staat; een permissieregel als hij in een `permissions.json` van
  `env.Packs` staat of `packs.IsLighthouseRule` hem herkent. Scriptpad uit het commando: vervang
  `$CLAUDE_PROJECT_DIR`/`${CLAUDE_PROJECT_DIR}` door `Root`, `~` door de home, neem het eerste
  woord dat naar een bestaand bestand wijst; geen → inline (review-focus 1).
- [ ] **Stap 4:** `go test ./internal/agentaudit/` → PASS.
- [ ] **Stap 5: commit** `feat(agent-audit): inventaris van repo, lokaal en gebruiker (#47)`

### Taak 4: hookcommando's ontleden

**Bestanden:**
- Maak: `internal/agentaudit/command.go`, `command_test.go`

**Interfaces:**
- Produceert: `scriptPath(command, root, home string) string` (gebruikt door `Collect`).

- [ ] **Stap 1: falende tabeltest** `TestScriptPath`: `"$CLAUDE_PROJECT_DIR/.claude/hooks/a.sh"`,
  `"bash \"$CLAUDE_PROJECT_DIR\"/.claude/hooks/a.sh"`, `"node ~/.claude/hooks/b.js"`,
  `"cd x && ./y.sh"` (bestaand `x/y.sh` relatief aan root), `"echo hi"` → `""`; een pad dat niet
  bestaat → het pad toch (voor `missing-script`), maar alleen als het eruitziet als een pad (bevat
  `/` en eindigt op een extensie of staat onder `.claude/`).
- [ ] **Stap 2:** FAIL. **Stap 3:** implementeer, en laat `Collect` het gebruiken.
  **Stap 4:** `go test ./internal/agentaudit/` → PASS.
- [ ] **Stap 5: commit** `feat(agent-audit): scriptpad uit een hookcommando (#47)`

### Taak 5: plugins

**Bestanden:**
- Maak: `internal/agentaudit/plugins.go`, `plugins_test.go`

**Interfaces:**
- Produceert: `collectPlugins(env Env, inv *Inventory)`, door `Collect` aangeroepen tenzij
  `RepoOnly`.

- [ ] **Stap 1: falende tests.** `TestPluginPrecedence`: gebruiker `enabledPlugins.p@m = true`,
  project `false` → niet ingeschakeld; lokaal `true` boven project `false` → wel.
  `TestPluginProjectScopeFromWorktree`: `installed_plugins.json` met scope `project`,
  `projectPath = MainRoot`, `Root` is een andere map → bron `plugin:p` gemeten, met zijn skill,
  command, agent, hook (`hooks/hooks.json`) en `.mcp.json`-server.
  `TestPluginNotInstalled` (review-focus 4). `TestManagedSettingsNotRead`: er staat een
  `Status{"managed", "COULD NOT CHECK", "managed settings are not read"}`; deze status telt
  *niet* mee voor de exitcode (bron `managed` is altijd onleesbaar; spec §2 vraagt alleen een
  zichtbare regel), `TestRunExitCodes` krijgt een geval daarvoor.
  *(De statusnaam `managed` is een uitbreiding op de bronlijst; spec §2.)*
- [ ] **Stap 2:** FAIL. **Stap 3:** implementeer; `${CLAUDE_PLUGIN_ROOT}` in een plugin-hook wordt
  het `installPath`. **Stap 4:** PASS.
- [ ] **Stap 5: commit** `feat(agent-audit): ingeschakelde plugins als bron (#47)`

### Taak 6: conflicten en overlap

**Bestanden:**
- Maak: `internal/agentaudit/findings.go`, `conflicts.go`, `overlap.go`, `conflicts_test.go`,
  `overlap_test.go`
- Maak: `internal/agentaudit/testdata/main-commit-guard.sh`

**Interfaces:**
- Produceert:
  ```go
  type Finding struct {
      Key, Kind string   // Kind: blocking-pair|permission|missing-script|unregistered-script|overlap
      Text      []string // de regels onder de sleutel (spec §7)
      Sources   []string // bronnen die de sleutel noemt, voor de ignore-regel
  }
  func Conflicts(inv *Inventory, env Env) []Finding
  func Overlap(inv *Inventory, guards []packs.Guard) []Finding
  func matcherHits(a, b string) bool // gelijk, leeg, "*", of een alternatie die een gedeelde tool noemt
  ```

- [ ] **Stap 1: falende tests** (spec, *Bewijs*): `TestBlockingPairProjectOnly`,
  `TestNoBlockingPairWithLighthouse`, `TestPermissionLighthouseAllowVsUserDeny` (sleutel
  `permission:Bash(lighthouse review-run:*)`), `TestPermissionPrefixCovers` (`Bash(git push:*)`
  tegenover `Bash(git push origin main)`), `TestNoPermissionWithinOneSource`,
  `TestMissingScript`, `TestMissingRunShName` (`run.sh onbekend` met `HookNames` zonder
  `onbekend`), `TestUnregisteredScript`, `TestHelperIsNotUnregistered`,
  `TestOverlapMainCommitGuard` (fixture onder twee namen, `main-commit-guard.sh` en
  `check-branch.sh`, beide op `PreToolUse Bash` → elk een kandidaat `overlap:main-commit-gate|…`
  met de `does`-zin en de treffers), `TestNoOverlapUnrelated` (een lintscript op dezelfde event),
  `TestOverlapOnePerGuardAndScript` (review-focus 5), `TestUnreadableHookIsNotClean` (script
  zonder leesrecht → geen kandidaat, wel een `COULD NOT CHECK`-status voor die hook).
  De fixture blokkeert met `exit 2` een `git commit` als `git rev-parse --abbrev-ref HEAD` `main`
  is, en noemt de naam `main-commit-gate` nergens.
- [ ] **Stap 2:** FAIL. **Stap 3:** implementeer volgens spec §3 en §4; termen hoofdletterongevoelig.
  **Stap 4:** PASS.
- [ ] **Stap 5: commit** `feat(agent-audit): conflicten en overlapkandidaten (#47)`

### Taak 7: oppervlak

**Bestanden:**
- Maak: `internal/agentaudit/surface.go`, `surface_test.go`

**Interfaces:**
- Produceert:
  ```go
  type SourceSurface struct{ Source string; Count, Chars, Tokens int }
  type Surface struct{ PerSource []SourceSurface; Top []Skill; TooLong []Skill }
  func Measure(inv *Inventory, top, maxDesc int) Surface
  ```

- [ ] **Stap 1: falende tests.** `TestMeasurePerSource`: drie bronnen met bekende lengtes
  (bijvoorbeeld naam 5 + description 95 = 100 tekens → 25 tokens; 101 tekens → 26) → juiste
  `Count`, `Chars`, `Tokens`, `PerSource` aflopend op `Tokens`, gelijke tokens op bronnaam.
  `TestMeasureTop`: `top = 2` → de twee grootste. `TestMeasureTooLong`: description van 1025
  tekens bij `maxDesc = 1024` → in `TooLong`. `TestMeasureCodexSkillsNotCounted`: een skill onder
  `.agents/skills/` telt niet mee. `TestMeasureNoFrontmatter` (review-focus 3).
- [ ] **Stap 2:** FAIL. **Stap 3:** tokens = `(chars + 3) / 4`. **Stap 4:** PASS.
- [ ] **Stap 5: commit** `feat(agent-audit): skill-oppervlak per bron (#47)`

### Taak 8: uitvoer, `ignore`, exitcodes en het commando

**Bestanden:**
- Maak: `internal/agentaudit/run.go`, `report.go`, `run_test.go`
- Maak: `internal/cli/agentaudit.go`, `internal/cli/agentaudit_test.go`
- Wijzig: `internal/cli/root.go` (`root.AddCommand(newAgentAuditCommand())`)
- Wijzig: `internal/config/config.go` (drie sleutels), `internal/config/config_test.go`
- Wijzig: `tests/pack/fixtures/config-schema.json` (schema en defaults van de drie sleutels; de
  test `TestSchemaMatchesSnapshot` vergelijkt daarmee)
- Wijzig: `tests/pack/fixtures/config/defaults/expect.json` (één entry per nieuwe sleutel met de
  default-uitvoer, ook `lighthouse config` van een lijst; `test_fixtures_cover_every_key` eist elke
  sleutel van `SCHEMA` en `DEFAULTS` in een fixture) en, voor de niet-default waarden,
  `tests/pack/fixtures/config/full/lighthouse.toml` + `expect.json`
- Wijzig: `pack/core/lib/config.py` en de geïnstalleerde kopie `.lighthouse/lib/lighthouse_lib/config.py`
  (alleen de tabellen `SCHEMA` en `DEFAULTS`: `tests/pack/test_config_fixtures.py` eist dat ze
  gelijk zijn aan `config-schema.json`; dit is bestaande Python, geen nieuwe)
- Wijzig: `docs/site/data/config.json` (tabel `agent_audit`)
- Wijzig: `pack/core/permissions.json` (`Bash(lighthouse agent-audit:*)`)

**Interfaces:**
- Consumeert: `Collect`, `Conflicts`, `Overlap`, `Measure`.
- Produceert:
  ```go
  type Options struct {
      Only    []string // "inventory"|"conflicts"|"overlap"|"surface"; leeg = alles
      Strict  bool
      Ignore  []string
      Top, MaxDesc int
  }
  func Run(env Env, o Options, out, errOut io.Writer) int
  func Summary(env Env, o Options, out, errOut io.Writer) int // init: bronnen, conflicten, overlap, top 5, hintregel
  func DefaultEnv(root string) (Env, error) // MainRoot via git, ConfigDir, ClaudeJSON, Packs uit manifest, HookNames
  ```
  Config: `agent_audit.ignore` (kStrs, `[]any{}`), `agent_audit.top` (kInt, `10`),
  `agent_audit.description_max_chars` (kInt, `1024`).

- [ ] **Stap 1: falende tests.** `TestRunSectionOrder` (koppen in de volgorde van spec §7),
  `TestRunWritesNothing` (hash vóór en na), `TestRunExitCodes` (tabel: geen melding → 0; melding
  zonder `Strict` → 0; met `Strict` → 1; `COULD NOT CHECK` van een bron met `Strict` → 2, ook als
  er meldingen zijn; alleen de status `managed` met `Strict` en een melding → 1), `TestRunIgnore` (genegeerde sleutel → niet getoond, telt niet voor exit 1),
  `TestRunStaleIgnore` (verdwenen sleutel van bron `project` → regel `ignore: <sleutel> komt niet
  meer voor`; sleutel van `gebruiker` met `RepoOnly` → geen regel), `TestRunOnlySurface`,
  `TestSummaryHasHint` (laatste regel `Draai /lighthouse-audit-agent-config voor een oordeel over
  de kandidaten.`, geen `Inventaris`). In `internal/cli`: `TestAgentAuditNoRepoRoot` → exit 2 en
  `agent-audit: COULD NOT CHECK — ` op stderr; `TestAgentAuditBrokenConfig` → exit 2.
  `config_test`: de drie defaults.
- [ ] **Stap 2:** FAIL. **Stap 3:** implementeer; de cli leest config via `config.Path()` +
  `config.Load` zoals `worktree-audit`; vlaggen `--repo-only`, `--only` (herhaalbaar, onbekende
  waarde → `usageErr`), `--strict`; de lange help bevat de exitcodetabel.
- [ ] **Stap 4:** `go test ./internal/agentaudit/ ./internal/cli/ ./internal/config/` en
  `uv run pytest -q tests/test_site_config.py tests/pack/test_config_fixtures.py` → PASS.
- [ ] **Stap 5: commit** `feat(agent-audit): lighthouse agent-audit met uitvoer, ignore en exitcodes (#47)`

### Taak 9: audit bij `init`

**Bestanden:**
- Wijzig: `internal/initcmd/initcmd.go` (`Options.Audit`, aanroep na `warnIgnored`)
- Wijzig: `internal/cli/init.go` (vlag `--audit`, `Options.Audit` gevuld met `agentaudit.Summary`)
- Test: `internal/initcmd/initcmd_test.go`

**Interfaces:**
- Produceert: `Options.Audit func(root string, out, errOut io.Writer)` en `Options.AuditOnUpgrade bool`
  (vlag `--audit`). `nil` = geen audit (bestaande tests blijven ongewijzigd).

- [ ] **Stap 1: falende tests** met een opnemende `Audit`-functie: `TestFirstInitRunsAudit`
  (eerste init met `Yes` → één aanroep), `TestDeclinedInitSkipsAudit` (antwoord `n` → geen),
  `TestUpgradeWithoutAuditFlag` (geen), `TestUpgradeWithAuditFlag` (één, ook bij `No changes.`),
  `TestAuditFailureKeepsInitExit` (in `internal/cli`; `init.go` krijgt een package-variabele
  `auditSummary = agentaudit.Summary` die de test door een functie laat vervangen die 2 geeft: `Summary` geeft 2 → `init` eindigt 0 en
  stderr bevat `agent-audit: COULD NOT CHECK`).
- [ ] **Stap 2:** FAIL. **Stap 3:** roep `o.Audit` aan als `!initialized && len(paths) > 0`, of
  `o.Upgrade && o.AuditOnUpgrade`. `--audit` zonder `--upgrade` → fout `--audit needs --upgrade`.
- [ ] **Stap 4:** `go test ./internal/initcmd/ ./internal/cli/` → PASS.
- [ ] **Stap 5: commit** `feat(init): agent-audit bij de eerste init en met --upgrade --audit (#47)`

### Taak 10: skill, documentatie, dogfood

**Bestanden:**
- Maak: `pack/core/skills/audit-agent-config/SKILL.md`
- Wijzig: `pack/core/instructions/AGENTS.md` (skillijst, alfabetisch vóór `audit-worktrees`:
  `- \`/lighthouse-audit-agent-config\`: audit the agentic configuration for conflicts and overlap.`,
  10 woorden)
- Wijzig: `internal/packs/budget_test.go` (`blockCeilings["core"]` 363 → 373 en `blocksCeiling`
  521 → 531) en `tests/test_instruction_files.py` (355 → 365, ook in de docstring): een bewuste
  verhoging met precies de woorden van die ene regel, de skill is eis van spec §6
- Wijzig: `internal/packs/core_test.go` (de vaste bestandslijst krijgt de skill en zijn `.agents/`-
  tweeling) en `docs/site/data/packs.json` (skills van `core`)
- Maak: `docs/agent-audit.md` (Engels: wat, bronnen, uitvoer, `ignore`, exitcodes)
- Wijzig: `docs/developer/planning/specs/2026-11-07-zonder-python-design.md` (regel
  `measure_skill_surface.py` → `lighthouse agent-audit --only surface`)
- Maak: `changelog.d/issue-47-agent-config-check.md` (`type: added`)
- Geïnstalleerde kopie: `.claude/skills/lighthouse-audit-agent-config/`,
  `.agents/skills/lighthouse-audit-agent-config/`, `AGENTS.md`, `.claude/settings.json`,
  `.lighthouse/manifest.toml`

- [ ] **Stap 1:** schrijf de skill volgens spec §6. Description ≤ 300 tekens, met `NOTE —` naar
  `check-instructions` voor de instructielaag. Stappen: draai `lighthouse agent-audit`; per
  kandidaat het script lezen en oordelen `overlap`/`geen overlap`/`twijfel` met één zin; per
  `overlap` één concrete stap (projecthook weg of sleutel in `[agent_audit] ignore`); niets zelf
  veranderen.
- [ ] **Stap 2:** `go run ./cmd/lighthouse init --upgrade --yes`, daarna nogmaals → `No changes.`
- [ ] **Stap 3:** `go run ./cmd/lighthouse check-instructions` → geen FAIL;
  `go run ./cmd/lighthouse agent-audit` in deze repo → exit 0, en `go run ./cmd/lighthouse agent-audit --only inventory` noemt de nieuwe skill onder de bron
  `lighthouse`.
- [ ] **Stap 4:** `go test ./...` en `uv run pytest -q tests/pack tests/test_instruction_files.py tests/test_site_packs.py`
  → PASS.
- [ ] **Stap 5: commit** (twee commits: `feat(core): skill lighthouse-audit-agent-config (#47)` met
  pack en geïnstalleerde kopie; `docs(agent-audit): handleiding, changelog, zonder-python-regel (#47)`).

## Zelfcontrole

- Spec §1–§9 en elke rij van *Bewijs* hebben een taak: §1 → 8; §2 → 3, 5; §3 → 1, 6; §4 → 6;
  §5 → 9; §6 → 10; §7 → 8; §8 → 8; §9 → 2, 10.
- `Env`, `Inventory`, `Finding`, `Surface` en `Options` hebben dezelfde namen in elke taak.
