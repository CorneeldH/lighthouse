# Auto-memory uit, `memory-gate` en migratie — implementatieplan (#497)

> **Voor agentische uitvoerders:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development
> (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken
> checkboxen (`- [ ]`).

**Doel:** `init` zet Claude Code-auto-memory uit, een hook `memory-gate` vangt schrijfacties naar een
auto-memory-map op, en `lighthouse memory scan` met de skill `/lighthouse-migrate-memory` verdeelt de
bestaande memory over de juiste lagen.

**Architectuur:** De logica voor paden, scannen en Bash-doelen staat in één nieuw pakket,
`internal/automemory`. Daarop bouwen drie onderdelen:
- de hook `internal/hooks/memorygate.go`;
- het subcommando `internal/cli/memory.go`;
- het settings-blok van `init`, met een nieuwe `settings.ApplyAutoMemory`.

De stand (`on`/`log`/`off`) komt uit `components.toml` via het bestaande hookframework. `init` leest
dezelfde stand via `levels.Resolved.Stand("memory-gate")`.

**Tech stack:** Go (cobra, `internal/hook`, `internal/levels`, `internal/settings`, orderedjson),
een skill in Markdown.

**Spec:** [2026-11-24-issue-497-memory-gate-design.md](../specs/2026-11-24-issue-497-memory-gate-design.md)

## Globale eisen

- Hookmeldingen, skilltekst, waarschuwingen en Go-namen en -commentaar zijn in het Engels.
  Artefacten (commits, PR, changelog) zijn in het Nederlands.
- `memory-gate` staat **niet** in `pack/core/hooks/blocking` (spec, keuze 4).
- De stand van `memory-gate`: `log` op light, `on` op standard en strict, te overschrijven met
  `[components] memory-gate = "…"`.
- `autoMemoryEnabled` volgt de stand van de gate (spec §1). Een ontbrekende sleutel wordt altijd
  `false`. Een andere waarde: `on` zet hem op `false`, `log` laat hem staan en waarschuwt, `off`
  laat hem staan zonder melding.
- Lezen van een memory-map wordt nooit geblokkeerd.
- Lighthouse schrijft niets in `AGENTS.md`/`CLAUDE.md` en verwijdert geen memory zonder akkoord.
- Voor elke commit: `[test_gate] command` (doet de hook `commit-test-gate`) en `lighthouse imports check`.
- Dogfood: na een wijziging die verandert wat `init` schrijft, draai je
  `go run ./cmd/lighthouse init --upgrade` en commit je de geïnstalleerde kopie. Een tweede run geeft
  `No changes.`.
- Een changelog-fragment in `changelog.d/issue-497-memory-gate.md` (`type: added`).
- Werk in de worktree `/home/corneel/repositories/lighthouse/.claude/worktrees/issue-497-memory-gate`.

## Review-focus

1. **Windows-paden in een `Write`.** `D:\Users\x\.claude\projects\D--repo\memory\a.md`, ook met `d:`
   en met `/`, moet blokkeren. De tests staan in taak 3.
2. **Een gequote memory-pad in Bash** (`echo x > "$HOME/.claude/projects/p/memory/a.md"`) moet
   blokkeren. Is het doel `$HOME/...` of `~/...`, dan normaliseert de check dat. De tests staan in
   taak 2.
3. **Een tweede `init --upgrade` geeft `No changes.`**, ook als er memory gevonden is: de melding
   staat alleen op stderr. De test staat in taak 6.
4. **`scan` vanuit een worktree** vindt dezelfde mappen als vanuit de main checkout. De test staat
   in taak 4.
5. **Een repo met een naam die als prefix van een andere geldt** (`D--x-repo` naast
   `D--x-repo-other`): `scan` neemt alleen de exacte match. De test staat in taak 4.

---

### Taak 1: `internal/automemory` — paden

**Bestanden:**
- Maken: `internal/automemory/paths.go`
- Test: `internal/automemory/paths_test.go`

**Interfaces:**
- Produceert:
  - `func ConfigDir(getenv func(string) string, home string) string`: geeft `CLAUDE_CONFIG_DIR`
    terug als die niet leeg is, anders `filepath.Join(home, ".claude")`.
  - `func Normalize(p, home string) string`: maakt van `\` een `/`. Begint `p` met `~/`, `~`,
    `$HOME/` of `${HOME}/`, dan wordt dat `home`. Het resultaat is in kleine letters.
  - `func IsMemoryPath(p, home, configDir string) bool`: waar als `Normalize(p)` past op
    `(^|/)\.claude/projects/[^/]+/memory(/|$)`, of als het begint met
    `Normalize(configDir) + "/projects/<seg>/memory"` gevolgd door het einde of een `/`.
  - `func EncodeProject(path string) string`: vervangt elk teken dat geen ASCII-letter of -cijfer is
    door `-`.

- [ ] **Stap 1: Schrijf de falende tests** (tabelgedreven):
  - `TestIsMemoryPath`. Waar voor:
    - `/home/u/.claude/projects/-home-u-r/memory/a.md`
    - `/home/u/.claude/projects/-home-u-r/memory`
    - `D:\Users\u\.claude\projects\D--r\memory\a.md`
    - `d:/users/u/.claude/projects/d--r/memory/a.md`
    - `~/.claude/projects/p/memory/x.md`
    - `$HOME/.claude/projects/p/memory/x.md`
    - `/cfg/projects/p/memory/a.md` met `configDir="/cfg"`

    Onwaar voor:
    - `/home/u/.claude/projects/p/memoryx/a.md`
    - `/home/u/.claude/projects/p/notes/a.md`
    - `/home/u/repo/memory/a.md`
    - `/home/u/.claude/settings.json`
  - `TestEncodeProject`:
    - `/home/x/repo` → `-home-x-repo`
    - `D:\x\repo` → `D--x-repo`
    - `/home/corneel/.cache/lighthouse` → `-home-corneel--cache-lighthouse`
  - `TestConfigDir`: met en zonder `CLAUDE_CONFIG_DIR`.
- [ ] **Stap 2:** `go test ./internal/automemory/...`. Verwacht: FAIL (pakket ontbreekt).
- [ ] **Stap 3:** Implementeer de vier functies in `paths.go`.
- [ ] **Stap 4:** `go test ./internal/automemory/...`. Verwacht: PASS.
- [ ] **Stap 5:** Commit `feat(automemory): herken auto-memory-paden en codeer projectpaden (#497)`.

### Taak 2: `internal/automemory` — schrijfdoelen in een Bash-commando

**Bestanden:**
- Maken: `internal/automemory/bashtargets.go`
- Test: `internal/automemory/bashtargets_test.go`

**Interfaces:**
- Produceert: `func WriteTargets(command string) []string`. Geeft de woorden die het commando
  beschrijft, ontdaan van quotes en niet genormaliseerd.

**Algoritme** (de signatuur legt dit niet vast):
1. **Tokenizer.** Splits op witruimte. Respecteer `'…'` en `"…"`: de quotes gaan eraf, de inhoud
   blijft één woord. `;`, `&&`, `||`, `|` en een newline worden scheidingstokens, ook als ze vast
   aan een woord zitten. `>`, `>>`, `2>`, `&>` en `2>>` worden operatortokens, ook als ze vast aan
   een woord zitten (`>file` wordt `>` plus `file`).
2. **Segmenten.** Elk stuk tussen scheidingstokens is een segment. Het commando van een segment is
   het eerste woord dat geen toewijzing `X=y` is. Een voorafgaand `sudo` of `command` wordt
   overgeslagen.
3. **Doelen per segment:**
   - het woord na `>` of `>>`, en het woord na `2>`, `&>` of `2>>`, tenzij dat `/dev/null` is of
     met `&` begint;
   - `tee`: alle argumenten die niet met `-` beginnen;
   - `cp`, `mv`, `ln`, `install`: het laatste argument dat niet met `-` begint;
   - `rm`, `touch`, `mkdir`: alle argumenten die niet met `-` beginnen;
   - `sed` met een argument `-i` of `-i…`: alle argumenten na het script (het eerste argument dat
     niet met `-` begint).

- [ ] **Stap 1: Schrijf `TestWriteTargets`** als tabel van commando's met verwachte doelen.
  Memory-pad `M=/h/.claude/projects/p/memory/a.md`:
  - `echo x > M` → `[M]`
  - `echo x >> M` → `[M]`
  - `echo x >M` → `[M]`
  - `echo x > "M"` → `[M]`
  - `printf x | tee -a M` → `[M]`
  - `cp a.md M` → `[M]`
  - `mv a M` → `[M]`
  - `rm -f M` → `[M]`
  - `touch M` → `[M]`
  - `mkdir -p /h/.claude/projects/p/memory` → `[/h/.claude/projects/p/memory]`
  - `sed -i 's/a/b/' M` → `[M]`
  - `cat > M <<'EOF'\nx\nEOF` → `[M]`
  - `cat M 2>/dev/null` → `[]`
  - `ls /h/.claude/projects/p/memory 2>&1` → `[]`
  - `grep foo M > /tmp/out` → `[/tmp/out]`
  - `cp M docs/a.md` → `[docs/a.md]`
  - `cd x && echo y > M` → `[M]`
  - `git status` → `[]`
- [ ] **Stap 2:** `go test ./internal/automemory/ -run TestWriteTargets`. Verwacht: FAIL.
- [ ] **Stap 3:** Implementeer `WriteTargets` volgens het algoritme.
- [ ] **Stap 4:** Dezelfde run. Verwacht: PASS.
- [ ] **Stap 5:** Commit `feat(automemory): lees de schrijfdoelen uit een Bash-commando (#497)`.

### Taak 3: hook `memory-gate`

**Bestanden:**
- Maken: `internal/hooks/memorygate.go`
- Test: `internal/hooks/memorygate_test.go`
- Wijzigen:
  - `internal/hooks/hooks.go:14-21`: `hook.Register("memory-gate", MemoryGate)`;
  - `pack/core/hooks/hooks.json`: in de groep PreToolUse `Edit|Write` (bij `worktree-check`) en in
    de groep PreToolUse `Bash` komt elk een `${CLAUDE_PLUGIN_ROOT}/hooks/run.sh memory-gate` met
    timeout 15;
  - `pack/core/levels.toml`: `"memory-gate"` in `on`, plus `memory-gate = "log"` onder
    `[light.components]`;
  - `pack/core/requires.toml`: niets (de hook heeft geen git nodig);
  - `pack/core/guards.toml`: `[memory-gate]` met `events = ["PreToolUse"]`, `matcher = "Edit|Write|Bash"`,
    `does = "Blocks a write into a Claude Code auto-memory directory."`, en
    `terms = [["memory"], [".claude/projects", "auto-memory", "autoMemory"]]`;
  - `e2e/registrations_test.go`: `memory-gate` in `wantHooks`, twee rijen in de registratietabel,
    en een `minimalPayload`-geval (een `Write` naar een pad buiten memory, verwacht exit 0);
  - `docs/levels.md`: een rij `memory-gate` (light `log`, standard `on`, strict `on`);
  - `CLAUDE.md`: in de hooklijst een bullet `memory-gate` (`run.sh memory-gate`). Tekst: PreToolUse
    `Edit|Write` and `Bash`. Blocks a write into a Claude Code auto-memory directory; reading
    stays free. Not in `blocking`.

**Interfaces:**
- Gebruikt: `automemory.IsMemoryPath`, `automemory.Normalize`, `automemory.ConfigDir` en
  `automemory.WriteTargets` (taak 1 en 2), en `readWritePayload` en `readGatePayload` uit
  `internal/hooks`.
- Produceert: `func MemoryGate(ctx context.Context, root, arg string, p hook.Payload) hook.Result`.

**Gedrag:**
- `tool_name` is `Bash`: controleer `WriteTargets(command)`.
- Anders: controleer `tool_input.file_path`. Is dat pad relatief, zet er dan `cwd` voor.
- Het eerste doel waarvoor `IsMemoryPath` waar is, geeft
  `hook.Result{Code: 2, Stderr: <melding>}`.
- Geen treffer: `hook.Result{}`.
- Een payload die geen JSON is: `hook.CouldNotCheck("memory-gate: COULD NOT CHECK — the payload is not valid JSON.\n")`.
- De stand `log` en `off` regelt `hook.Run` zelf: `log` schrijft de eerste regel van de melding naar
  het logboek.
- `home` komt uit `os.UserHomeDir()`. Lukt dat niet, dan is `home` leeg en geldt alleen het
  `.claude/projects`-patroon.

De melding, letterlijk (`<path>` is het doel zoals het in de payload stond):

```text
memory-gate: auto-memory write blocked (<path>).
AGENTS.md forbids harness memory: it lives on one machine and nobody reviews it.
Put a lasting rule in the repo with /lighthouse-place-instruction; move existing memory with /lighthouse-migrate-memory.
```

- [ ] **Stap 1: Schrijf `TestMemoryGate`** (tabel, in het patroon van `worktreecheck_test.go`):
  - `Write` naar `<home>/.claude/projects/p/memory/a.md` → Code 2, Stderr begint met
    `memory-gate: auto-memory write blocked`;
  - `Edit` naar `D:\u\.claude\projects\D--r\memory\a.md` → Code 2;
  - `Write` naar `<tmp>/repo/doc.md` → Code 0, leeg;
  - relatieve `file_path` `memory/a.md` met `cwd` `<home>/.claude/projects/p` → Code 2;
  - `Bash` `echo x > ~/.claude/projects/p/memory/a.md` → Code 2;
  - `Bash` `cat ~/.claude/projects/p/memory/a.md` → Code 0;
  - `Bash` `cp ~/.claude/projects/p/memory/a.md docs/a.md` → Code 0;
  - payload `not json` → Code 2 met `COULD NOT CHECK`.

  Een test via `hook.Run` met een `components.toml` die `memory-gate = "log"` zet: de blokkerende
  `Write` geeft exit 0 en één logboekregel. Volg daarbij het bestaande patroon van een log-test in
  `internal/hook`.
- [ ] **Stap 2:** `go test ./internal/hooks/ -run TestMemoryGate`. Verwacht: FAIL.
- [ ] **Stap 3:** Implementeer `MemoryGate`, registreer hem, en pas `hooks.json`, `levels.toml`,
  `guards.toml`, `docs/levels.md` en `CLAUDE.md` aan.
- [ ] **Stap 4:** `go test ./internal/hooks/... ./internal/hook/... ./internal/initcmd/... ./e2e/...`.
  Verwacht: PASS, met de e2e-aanpassingen.
- [ ] **Stap 5:** `go run ./cmd/lighthouse init --upgrade`, en daarna nog eens. Verwacht bij de
  tweede run: `No changes.`. `git status` toont `.claude/settings.json`, `.claude/hooks/lighthouse/`
  en `.lighthouse/components.toml`, of wat `init` installeert.
- [ ] **Stap 6:** Commit `feat(hooks): memory-gate blokkeert schrijven naar auto-memory (#497)`,
  met de geïnstalleerde kopie.

### Taak 4: `automemory.Scan` — voorstellen per bestand

**Bestanden:**
- Maken: `internal/automemory/scan.go`
- Test: `internal/automemory/scan_test.go`

**Interfaces:**
- Gebruikt: `EncodeProject` (taak 1).
- Produceert:

```go
type Proposal struct {
	Path        string `json:"path"`
	Dir         string `json:"dir"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Destination string `json:"destination"` // instruction|issue|docs|host|drop|undecided
	Reason      string `json:"reason"`
}
type ScanResult struct {
	Dirs  []string   `json:"dirs"`
	Files []Proposal `json:"files"`
}
func MainCheckout(dir string) (string, error) // parent of `git rev-parse --path-format=absolute --git-common-dir`
func Scan(configDir, repoMain string) (ScanResult, error)
```

**Gedrag van `Scan`:**
- Mappen zijn de entries van `<configDir>/projects` waarvan
  `strings.EqualFold(name, EncodeProject(repoMain))` geldt en waarin een `memory/` staat,
  gesorteerd.
- Bestaat `<configDir>/projects` niet, dan volgt een leeg resultaat zonder fout.
- Is de map niet leesbaar, dan volgt een fout.
- Per `*.md` in `memory/`, gesorteerd: `name` en `type` uit de YAML-frontmatter, met `type:`
  bovenaan of anders `metadata.type:`. Lees ze met `gopkg.in/yaml.v3`.
- Bestemming:

  | Geval | Bestemming |
  |---|---|
  | `feedback` | `instruction` |
  | `project` | `issue` |
  | `reference` met in de body een absoluut pad buiten `repoMain` (regex `(^|\s)(/|[A-Za-z]:\\)\S+`, waarbij `repoMain` niet de prefix is) | `host` |
  | `reference`, overig | `docs` |
  | `user` | `drop` |
  | `MEMORY.md` | `drop`, met `reason` `index` |
  | overig | `undecided` |

- `Reason` is `type <t>`, `index` of `no type`.

- [ ] **Stap 1: Schrijf `TestScan`** met een synthetische `configDir` in `t.TempDir()`:
  - `projects/D--x-repo/memory/` met `feedback_a.md` (top-level `type: feedback`), `proj.md`
    (`metadata:\n  type: project`) en `MEMORY.md`;
  - `projects/d--x-repo/memory/` met `ref_host.md` (reference, body met `/srv/data`), `ref_doc.md`
    (reference, zonder pad), `user.md` en `raw.md` (zonder frontmatter);
  - `projects/D--x-repo-other/memory/x.md`, dat niet mee mag doen;
  - `projects/D--x-repo2/` zonder `memory/`.

  Aanroep met `repoMain="D:\\x\\repo"`. Verwacht: precies twee `Dirs`, en per bestand de bestemming:
  - `instruction`
  - `issue`
  - `drop` (index)
  - `host`
  - `docs`
  - `drop`
  - `undecided`
- [ ] **Stap 2: `TestScanMissingProjects`**: geen `projects/` → leeg resultaat, `err == nil`.
- [ ] **Stap 3: `TestMainCheckoutFromWorktree`**: `git init` in een tempdir, één commit, dan
  `git worktree add`. `MainCheckout(worktree)` geeft de main checkout terug.
- [ ] **Stap 4:** `go test ./internal/automemory/...`. Verwacht: FAIL.
- [ ] **Stap 5:** Implementeer `scan.go`.
- [ ] **Stap 6:** Dezelfde run. Verwacht: PASS.
- [ ] **Stap 7:** Commit `feat(automemory): stel per memory-bestand een bestemming voor (#497)`.

### Taak 5: subcommando `lighthouse memory scan`

**Bestanden:**
- Maken: `internal/cli/memory.go`
- Test: `internal/cli/memory_test.go`
- Wijzigen: `internal/cli/root.go:70-109`, met `root.AddCommand(newMemoryCommand())`.

**Interfaces:**
- Gebruikt: `automemory.ConfigDir`, `automemory.MainCheckout` en `automemory.Scan`.
- Produceert: `lighthouse memory scan [--format json|text] [--config-dir <dir>]`.
  - Zonder `--config-dir` geldt `ConfigDir(os.Getenv, home)`.
  - De repo is `MainCheckout(cwd)`.
  - `json` (standaard) is `ScanResult`, ingesprongen.
  - `text` is een tabel met de kolommen `destination`, `type`, `name` en `path`, gesorteerd op
    bestemming.
  - Exit 0 bij succes, ook bij een lege lijst. Exit 2 met `memory scan: <fout>` op stderr als
    `Scan` of `MainCheckout` faalt.

- [ ] **Stap 1: Schrijf de tests.**
  - `TestMemoryScanJSON`: een tempdir-repo (`git init`), een synthetische config-map met één
    `feedback`-bestand onder de codering van die repo. Verwacht: exit 0, en JSON met één bestand
    met `destination` `instruction`.
  - `TestMemoryScanUnreadable`: `projects` met mode `0o000` (overslaan als root). Verwacht: exit 2.
- [ ] **Stap 2:** `go test ./internal/cli/ -run TestMemoryScan`. Verwacht: FAIL.
- [ ] **Stap 3:** Implementeer het commando in het patroon van de bestaande subcommando's in
  `internal/cli`.
- [ ] **Stap 4:** Dezelfde run. Verwacht: PASS.
- [ ] **Stap 5:** Commit `feat(cli): lighthouse memory scan (#497)`.

### Taak 6: `init` — `autoMemoryEnabled` en de melding over gevonden memory

**Bestanden:**
- Wijzigen:
  - `internal/settings/settings.go`: na `ApplyLanguage` komt `ApplyAutoMemory`;
  - `internal/initcmd/initcmd.go`: `seeding` krijgt `memoryStand levels.Stand`, gevuld uit
    `resolved.Stand("memory-gate")` bij regel 263. Het settings-blok (rond 511-546) draait
    voortaan ook als `ApplyAutoMemory` iets verandert. Na de repo-wijzigingen volgt de melding over
    gevonden memory.
- Test:
  - `internal/settings/settings_test.go`, of het bestaande testbestand van dat pakket;
  - `internal/initcmd/automemory_test.go`.

**Interfaces:**
- Produceert:
  `func ApplyAutoMemory(doc *orderedjson.Object, stand string) (changed bool, kept any)`.
  - Ontbreekt de sleutel: zet `false` als laatste sleutel, `changed=true`.
  - De waarde is `false`: niets.
  - Een andere waarde bij stand `on`: zet `false`, `changed=true`, `kept=<oude waarde>`.
  - Een andere waarde bij `log`: niets, `kept=<oude waarde>`.
  - Een andere waarde bij `off`: niets, `kept=nil`.
- Meldingen van `init` op stderr, letterlijk:
  - `on` met `kept`:
    `note: .claude/settings.json had autoMemoryEnabled <v>; memory-gate is on, so it is now false`
  - `log` met `kept`:
    `warning: .claude/settings.json has autoMemoryEnabled <v>; memory-gate is at log, so it stays and auto-memory stays on`
  - gevonden memory (`len(Files) > 0`, ook bij `--upgrade`):
    `note: <n> auto-memory files found for this repo; move them with /lighthouse-migrate-memory`

    Faalt de scan, dan zwijgt `init`: de melding is een hulp, geen check.

**Volgorde in het settings-blok:** roep `ApplyAutoMemory` aan op het geladen document, vóór de
voorwaarde die bepaalt of het blok schrijft. Het blok draait dus altijd. Het bestand wordt alleen
geschreven als het resultaat afwijkt van het oude (`jsonEqual`), zoals nu.

- [ ] **Stap 1: Schrijf `TestApplyAutoMemory`**, een tabel met de negen cellen: ontbreekt, `false`
  en `true`, telkens bij `on`, `log` en `off`. Controleer de sleutelvolgorde: een nieuwe sleutel
  staat laatst.
- [ ] **Stap 2: Schrijf de init-tests** in het patroon van `levels_test.go`:
  - `TestInitAutoMemoryLight`: een repo zonder settings, op `--level light`. Verwacht:
    `.claude/settings.json` bevat `"autoMemoryEnabled": false`.
  - `TestInitAutoMemoryStrictOverwrites`: settings met `true`, op strict. Verwacht: `false` en de
    `note:`-regel.
  - `TestInitAutoMemoryLightKeeps`: `true` op light. Verwacht: `true` en de `warning:`-regel.
  - `TestInitAutoMemoryOffOverride`: strict met `[components] memory-gate = "off"` en `true`.
    Verwacht: `true`, zonder melding.
  - `TestInitUpgradeNoChangesWithMemory`: een tweede `init --upgrade`. Verwacht: stdout bevat
    `No changes.`, ook als een synthetische memory-map gevonden wordt. Zet daarvoor
    `CLAUDE_CONFIG_DIR` naar een tempdir met `projects/<codering>/memory/a.md`. Verwacht ook de
    `note: 1 auto-memory files found`-regel op stderr.
- [ ] **Stap 3:** `go test ./internal/settings/... ./internal/initcmd/...`. Verwacht: FAIL.
- [ ] **Stap 4:** Implementeer.
- [ ] **Stap 5:** Dezelfde run. Verwacht: PASS.
- [ ] **Stap 6:** `go run ./cmd/lighthouse init --upgrade`, twee keer. De tweede run geeft
  `No changes.`. `.claude/settings.json` van deze repo bevat nu `"autoMemoryEnabled": false`.
- [ ] **Stap 7:** Commit `feat(init): zet autoMemoryEnabled op false volgens de stand van memory-gate (#497)`,
  met de geïnstalleerde kopie.

### Taak 7: skill `/lighthouse-migrate-memory`, docs en changelog

**Bestanden:**
- Maken:
  - `pack/core/skills/migrate-memory/SKILL.md`;
  - `changelog.d/issue-497-memory-gate.md`.
- Wijzigen:
  - `pack/core/levels.toml`: `"skill:migrate-memory"` in `on`, en in `[calls]`
    `"skill:migrate-memory" = ["skill:place-instruction"]`;
  - `pack/core/requires.toml`: `migrate-memory` in de lijst `git` (`scan` gebruikt
    `git rev-parse`);
  - `docs/install.md`: na de stap `init` één stap "Bestaande auto-memory? Draai
    `/lighthouse-migrate-memory`" (in de taal van dat document);
  - `docs/levels.md`: de skill in de tabel.

**De skill** (Engels). Frontmatter in de vorm van `place-instruction/SKILL.md`: `name: lighthouse-migrate-memory`,
`disable-model-invocation: true`, `metadata.binding: default` en `origin: own`. De description
noemt de triggers: "migrate memory", "auto-memory", "/lighthouse-migrate-memory". Daarbij één NOTE:
niet voor een nieuwe regel, daarvoor `/lighthouse-place-instruction`. De stappen staan in spec §3,
"Skill `/lighthouse-migrate-memory`", punt 1-5. Neem die over, met deze vaste regels:
- de eerste stap is `lighthouse memory scan --format json`;
- per groep één akkoordvraag (`AskUserQuestion`) met de bestanden erbij;
- `instruction` gaat altijd via `/lighthouse-place-instruction` en nooit rechtstreeks in
  `AGENTS.md`/`CLAUDE.md`;
- verwijderen doet de skill nooit zelf. Hij geeft per akkoord het `rm`-commando aan de gebruiker,
  want `memory-gate` blokkeert het;
- het eindrapport: verwerkt, over, en verwijderd door de gebruiker.

De changelog, in het Nederlands, `type: added`: één alinea over `autoMemoryEnabled`, `memory-gate`
(light `log`), `lighthouse memory scan` en `/lighthouse-migrate-memory` (#497).

- [ ] **Stap 1:** Schrijf de skill, de changelog en de docs-wijzigingen.
- [ ] **Stap 2:** `go run ./cmd/lighthouse init --upgrade`, twee keer. De tweede run geeft
  `No changes.`. De skill staat nu in `.claude/skills/lighthouse-migrate-memory/`.
- [ ] **Stap 3:** `go test ./internal/initcmd/... ./e2e/... ./internal/agentaudit/...` en
  `lighthouse imports check`. Verwacht: PASS. Een test die de skills of de `on`-lijst telt, werk je
  bij.
- [ ] **Stap 4:** Commit `feat(skills): lighthouse-migrate-memory verdeelt bestaande auto-memory (#497)`.

### Taak 8: controle met de hand

- [ ] **Stap 1:** Start in deze repo, die nu `autoMemoryEnabled: false` heeft, een nieuwe Claude
  Code-sessie (`claude -p "Which memory directory do you have, if any? Answer in one line."`).
  Controleer dat de systeemprompt geen sectie voor auto-memory meer heeft. Leg het antwoord vast
  voor de PR-body.
- [ ] **Stap 2:** `lighthouse memory scan --format text` in deze repo. Leg de uitvoer vast voor de
  PR-body (bewijs van AC 3).
