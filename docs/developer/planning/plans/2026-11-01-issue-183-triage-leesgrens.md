# Triage-leesgrens Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Het triage-oordeel leest alleen de getrackte inhoud van `origin/<default_branch>`, en een
vrij veld dat zijn grens overschrijdt maakt het oordeel ongeldig.

**Architecture:** Per repo per ronde pakt Go `git archive origin/<default_branch>` uit in een
tijdelijke map; elk oordeel van die repo draait daar, met `--setting-sources ""` en alleen `env` en
`modelSettings` uit de gebruikersinstellingen. `Validate` controleert de vrije velden die `Choose`
publiceert, met de rijvolgorde van `Choose` uit één functie.

**Tech Stack:** Go 1.26 (`archive/tar`, `os.Root`), `git`, `claude` CLI.

**Spec:** [2026-11-01-triage-leesgrens-design.md](../specs/2026-11-01-triage-leesgrens-design.md)

## Global Constraints

- Artefacten (commits, docs, PR) in het Nederlands; het changelog-fragment in het Engels
  (`docs/releasing.md`, *Changelog fragments*); namen, comments en testnamen in Go in het Engels.
  Go volgt `/lighthouse-go-standard`.
- Commits als Conventional Commits, één logische wijziging per commit, met
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Nooit terug naar `--setting-sources user`, ook niet als een meting tegenvalt: meld het in de PR.
- Grenzen, in runes na `strings.TrimSpace`: route 80, reden-staart 200, `headless_reason` 300,
  ontwerpvraag 300, optie 150, `redundancy_evidence` 220; hoogstens 5 verwijzingen, `#<nr>` 1 tot 6
  cijfers, sha 7 tot 40 hex, tussen twee verwijzingen precies `, ` of ` `.
- Uit de gebruikersinstellingen alleen `env` en `modelSettings`; `permissions` en de rest nergens.
- Tijdelijke map: `os.MkdirTemp("", "lighthouse-triage-")`, weg na de ronde, ook na een fout of de
  rem. Geen signaalafhandeling.
- `Target.Worktree` (de handoff) blijft `repo.Dir`.

## Review Focus

1. Een sha in hoofdletters (`ABC1234`) is nog steeds een sha: vergelijk hoofdletterongevoelig
   (test in Task 2).
2. Een routeregel met multibyte-tekens: 80 × `é` is geldig, want de grens telt runes, geen bytes
   (test in Task 2).
3. Een getrackt bestand met een spatie of `é` in de naam, of met de uitvoerbit, komt ongeschonden in
   de export (test in Task 4).
4. De rem sluit midden in de ronde: de export van die repo is daarna toch weg (test in Task 6).
5. `CLAUDE_CONFIG_DIR` gezet: de instellingen komen uit `$CLAUDE_CONFIG_DIR/settings.json`, niet uit
   `~/.claude` (test in Task 5).

---

### Task 1: De rijvolgorde van `Choose` op één plek

**Files:**
- Modify: `internal/triage/outcome.go:42-101` (`Choose`)
- Test: `internal/triage/outcome_test.go`

**Interfaces:**
- Produces: `func judgedRow(j *Judgment, g0 *Gate0, authorTrusted bool) Row`: de rij voor een
  geldig oordeel (nooit `RowInvariant`). Volgorde: `RowRedundant` (`duplicate`/`done`),
  `RowStop` (`g0 != nil`, poort 0 of 1), `RowDesign` (poort 4c), `RowInteractive`
  (`!Headless`), `RowUntrusted`, `RowForward`.

- [ ] **Step 1: Write the failing test** `TestJudgedRow`, een tabel met per rij één geval, plus de
  voorrang: `done` + poort 4c → `RowRedundant`; poort 1 + `headless` false → `RowStop`; 4c +
  `headless` false → `RowDesign`; `headless` false + niet vertrouwd → `RowInteractive`; `g0` gezet
  met `redundancy` `none` → `RowStop`.
- [ ] **Step 2: Run** `go test ./internal/triage -run TestJudgedRow` — Expected: FAIL, `judgedRow`
  undefined.
- [ ] **Step 3: Implement `judgedRow`** in `outcome.go`, en laat `Choose` erop schakelen: eerst
  `RowInvariant` als `len(devs) > 0`, anders `switch judgedRow(j, g0, f.AuthorTrusted)`. De
  inhoud per rij blijft gelijk.
- [ ] **Step 4: Run** `go test ./internal/triage` — Expected: PASS, ook alle bestaande
  `Choose`-tests.
- [ ] **Step 5: Commit** `refactor(triage): rijvolgorde van Choose in judgedRow (#183)`

### Task 2: Grenzen op de vrije velden

**Files:**
- Create: `internal/triage/limits.go`
- Modify: `internal/triage/facts.go:43` (`Facts`), `internal/triage/judgment.go` (`Validate`,
  `checkRoute`), `internal/triage/prompt.go:57-60`, `internal/cli/triage_gather.go:147`
- Test: `internal/triage/judgment_test.go`, `internal/triage/prompt_test.go`,
  `internal/cli/triage_test.go`

**Interfaces:**
- Consumes: `judgedRow` (Task 1).
- Produces: constanten `MaxRouteRunes = 80`, `MaxRouteReasonRunes = 200`,
  `MaxHeadlessReasonRunes = 300`, `MaxQuestionRunes = 300`, `MaxOptionRunes = 150`,
  `MaxEvidenceRunes = 220`, `MaxEvidenceRefs = 5`, `MaxIssueDigits = 6`, met één regel commentaar
  die naar de spec verwijst; veld `Facts.GitShas []string` (volledige sha's van de commits in
  `GitLog`).

- [ ] **Step 1: Write the failing tests** in `judgment_test.go`, rijen in `TestValidateInvalid`
  (verwachte substring tussen haakjes) en gevallen in `TestValidateValid`:
  - route van 81 runes (`route`), reden-staart van 201 (`reason`); geldig: route van 80 × `é`,
    staart van 200.
  - rij *Interactief*: `headless_reason` van 301 (`headless_reason`); geldig: 300, en 400 bij poort 1
    (rij *Stop*, niet gepubliceerd).
  - rij *Ontwerp*: vraag van 301 (`design_questions[0]`), optie van 151 (`options[1]`); geldig: een
    vraag van 300 en een optie van 150, en een oordeel met `redundancy` `done`, geldig bewijs en een
    vraag van 400 runes.
  - `redundancy_evidence` bij `done`, met `GitShas: {"abc1234def…"(40 hex)}` in de feiten:
    proza `"fixed in abc1234"` (`redundancy_evidence`); sha `"fff0000"` die niet in de log staat
    (`not in the git log`); `"abc1234ff"`, begint met de korte sha maar wijkt af
    (`not in the git log`); `"#1234567"` (`redundancy_evidence`); zes verwijzingen; `"#1 ,, ,#2"`;
    221 runes. Geldig: `"#12, abc1234"`, `"ABC1234"`, en leeg bij `duplicate`.
  - Pas de bestaande gevallen met proza als bewijs aan (`judgment_test.go:244` `"same title"`,
    `:262` `"abc123 fixes it"`) naar geldige verwijzingen, met `GitShas` in de feiten.
- [ ] **Step 2: Write the failing test** in `prompt_test.go`: `TestPrompt` verwacht ook
  `"at most 5"` en `"invalid"` in de prompt, en de regel met de lengtegrenzen als geheel, opgebouwd
  uit de constanten (bijvoorbeeld `fmt.Sprintf("route at most %d", tri.MaxRouteRunes)` per veld),
  niet de losse getallen.
- [ ] **Step 3: Run** `go test ./internal/triage` — Expected: FAIL op de nieuwe rijen.
- [ ] **Step 4: Implement** in `limits.go` de constanten en
  `checkEvidence(ev string, shas []string) error`: leeg is goed; > `MaxEvidenceRunes` fout; het
  hele veld past op één expressie, gebouwd uit de constanten:
  `^(#[0-9]{1,6}|[0-9a-fA-F]{7,40})((, | )(#[0-9]{1,6}|[0-9a-fA-F]{7,40})){0,4}$`; elke sha
  is, in kleine letters, een prefix van een sha uit `shas`.
- [ ] **Step 5: Implement** in `judgment.go`: `checkRoute` meet `m[4]` (route) en `m[5]`
  (staart) in runes; `Validate` roept na `checkRedundancy` `checkPublished(j Judgment, f Facts,
  c Context) error` aan, dat op `judgedRow(&j, c.Gate0, f.AuthorTrusted)` schakelt: *Redundant*
  → `checkEvidence(j.RedundancyEvidence, f.GitShas)`; *Ontwerp* → vraag- en optielengtes;
  *Interactief* → `headless_reason`. Foutteksten via `invalid(...)`, met veld, lengte en grens.
- [ ] **Step 6: Implement** in `prompt.go`: de zin over `redundancy_evidence` noemt de vorm
  ("only references, at most 5, separated by \", \" or \" \": #<number> of at most 6 digits, or a
  commit sha of 7 to 40 hex from the git log below"), en een regel noemt de lengtegrenzen in
  tekens uit de constanten, met "Prose in redundancy_evidence or a longer field makes the whole
  judgment invalid."
- [ ] **Step 7: Run** `go test ./internal/triage` — Expected: PASS.
- [ ] **Step 8: Write the failing test** `TestTriageEvidenceFromLog` in `internal/cli/triage_test.go`:
  een commit `fix: thing (#7)` op origin; het oordeel van #7 is `done` met als bewijs de eerste 12
  tekens van die sha → de comment bevat dat bewijs. Een tweede ronde-variant met `"deadbeef"` →
  stderr bevat `invalid judgment` en er is geen comment.
- [ ] **Step 9: Run** `go test ./internal/cli -run TestTriageEvidenceFromLog` — Expected: FAIL
  (`GitShas` leeg, dus `not in the git log`).
- [ ] **Step 10: Implement** in `gatherFacts`: dezelfde log-argumenten als `GitLog`, nu met
  `--format=%H`, via `lines(...)` in `f.GitShas`. Zet de gedeelde argumenten in één slice.
- [ ] **Step 11: Run** `go test ./internal/cli ./internal/triage` — Expected: PASS.
- [ ] **Step 12: Commit** `fix(triage): grenzen op de vrije velden van het oordeel (#183)`

### Task 3: `Catalog` met skillmap en projectpad apart

**Files:**
- Modify: `internal/triage/judgment.go:67-140` (`Catalog`, `pluginInstall`)
- Test: `internal/triage/judgment_test.go` (`TestCatalog`)

**Interfaces:**
- Produces: `func Catalog(skillDir, projectDir, pluginsFile string) (skills []Skill, warning
  string, err error)`: repo-skills uit `skillDir`, de plugin-entry op `projectDir`;
  `func CheckRouteSkill(dir string) error`: fout als `dir/.claude/skills/lighthouse-route-issue/SKILL.md`
  ontbreekt, geen gewoon bestand is, of na `filepath.EvalSymlinks` buiten `dir` uitkomt.

- [ ] **Step 1: Write the failing tests**: `TestCatalog` roept `Catalog(export, repo, plugins)` aan
  met de skills in `export` en de project-entry op `repo`, en verwacht nog steeds
  `superpowers:brainstorming` uit de project-installatie. Nieuw geval: `export/.claude/skills/x/SKILL.md`
  is een symlink naar een bestand met geldige frontmatter buiten `export` → `x` staat niet in de
  catalogus. `TestCheckRouteSkill`: ontbreekt → fout; gewoon bestand → nil; symlink binnen de map
  → nil; symlink naar buiten → fout.
- [ ] **Step 2: Run** `go test ./internal/triage -run 'TestCatalog|TestCheckRouteSkill'` —
  Expected: FAIL (signatuur, `CheckRouteSkill` undefined).
- [ ] **Step 3: Implement** een helper `inside(root, path string) bool` (beide door
  `filepath.EvalSymlinks`, dan `filepath.Rel` zonder `..`-begin), `Catalog` met de nieuwe
  signatuur (slaat een pad over waarvoor `inside` false is) en `CheckRouteSkill`. Pas de aanroep
  in `internal/cli/triage.go:205` voorlopig aan naar `tri.Catalog(repo.Dir, repo.Dir, …)`.
- [ ] **Step 4: Run** `go test ./internal/triage ./internal/cli` — Expected: PASS.
- [ ] **Step 5: Commit** `fix(triage): catalogus scheidt skillmap en projectpad (#183)`

### Task 4: De export van `origin/<default_branch>`

**Files:**
- Create: `internal/cli/triage_export.go`, `internal/cli/triage_export_test.go`
- Modify: `internal/cli/triage_test.go:101-110` (helper `isolateGit` uit `triageFixture` halen)

**Interfaces:**
- Produces: `func exportTree(ctx context.Context, repoDir, ref string) (dir string, remove func(),
  err error)`; `func isolateGit(t *testing.T)` (de `GIT_*`-variabelen van `triageFixture`).

- [ ] **Step 1: Write the failing test** `TestExportTree`: een repo met bare origin; op origin
  getrackt `a.txt`, `sub/b c é.txt`, `run.sh` (0755), `secret.txt`, een symlink `link` →
  `/etc/hostname`, `.gitignore` met `.env`, `.gitattributes` met `secret.txt export-ignore`. Daarna
  lokaal: een commit met `local.txt` (niet gepusht), ongetrackt `u.txt`, genegeerd `.env`.
  `exportTree(ctx, dir, "origin/main")`: de bestanden (WalkDir, alleen niet-mappen, relatief) zijn
  precies `.gitattributes`, `.gitignore`, `a.txt`, `link`, `run.sh`, `sub/b c é.txt`; geen `.git`;
  `link` is een symlink met `Readlink` `/etc/hostname`; `run.sh` heeft de uitvoerbit; de naam van
  `dir` begint met `lighthouse-triage-`; de inhoud van `a.txt`, `sub/b c é.txt` en `run.sh` is
  byte voor byte die van origin. Na `remove()` geeft `os.Stat(dir)` `fs.ErrNotExist`.
  `TestExportTreeBadRef`: `ref` `origin/nope` → fout, en geen `lighthouse-triage-*`-map blijft
  achter in een eigen `TMPDIR` (`t.Setenv("TMPDIR", t.TempDir())`).
- [ ] **Step 2: Run** `go test ./internal/cli -run TestExportTree` — Expected: FAIL, undefined.
- [ ] **Step 3: Implement** `exportTree`: `os.MkdirTemp("", "lighthouse-triage-")`, `os.OpenRoot`
  erop, `git -C repoDir archive --format=tar ref` via `exec.CommandContext` met een eigen
  `context.WithCancel(ctx)`, stdout als pipe in `archive/tar`. Per header:
  `TypeDir` → `root.MkdirAll`; `TypeReg` → `root.MkdirAll` van de ouder, `root.OpenFile` met
  `O_CREATE|O_EXCL|O_WRONLY` en `fs.FileMode(h.Mode).Perm()`, `io.Copy` van de tar-reader erin en
  `Close` met foutcontrole; `TypeSymlink` →
  `root.Symlink(h.Linkname, h.Name)`; `TypeXGlobalHeader` → overslaan; anders een fout. `os.Root`
  weigert namen die buiten de map uitkomen. Bij een fout tijdens het uitpakken annuleert hij de
  eigen context vóór `cmd.Wait`, zodat `git` niet blijft hangen op een volle pipe. Bij elke fout,
  ook een mislukte `git`, haalt hij de map weg en geeft hij de fout met de start van stderr terug
  (zoals `gitOut`).
- [ ] **Step 4: Run** `go test ./internal/cli -run TestExportTree` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(triage): export van origin/<default_branch> naar een tijdelijke map (#183)`

### Task 5: Oordeel met `--setting-sources ""` en de overgenomen instellingen

**Files:**
- Modify: `internal/cli/triage_judge.go`, `internal/cli/triage.go:26-41` (`judgeFunc`,
  `triageEnv`)
- Test: `internal/cli/triage_judge_test.go` (ook de bestaande `execJudge`-aanroepen op `:46`, `:74`
  en `:94`), `internal/cli/triage_brake_test.go:154` (`fx.env.Judge`), `internal/cli/triage_test.go:186`
  (`fakeJudge.judge`)

**Interfaces:**
- Produces:
  - `type judgeConfig struct { Env []string; Settings string }`: `Env` is `KEY=value` uit `env`,
    gesorteerd op sleutel; `Settings` is de `--settings`-JSON.
  - `func loadJudgeConfig(path string) (judgeConfig, error)`: ontbrekend bestand →
    `Settings` `{"disableAllHooks":true}`, geen `Env`; geen geldige JSON, of een `env` die geen
    object van strings is → fout met het pad; anders `Settings` =
    `json.Marshal` van `disableAllHooks: true` plus `modelSettings` (als `json.RawMessage`) als die
    er is.
  - `func judgeSettingsPath(getenv func(string) string) string`: `$CLAUDE_CONFIG_DIR/settings.json`
    als die gezet is, anders `$HOME/.claude/settings.json`.
  - `type judgeFunc func(ctx context.Context, dir, prompt string, cfg judgeConfig) ([]byte, error)`.
  - `triageEnv.SettingsFile string` (`""` is `judgeSettingsPath(Getenv)`).
  - `func judgeArgs(settings string) []string`.

- [ ] **Step 1: Write the failing tests**:
  - `TestExecJudgeFlags`: `execJudge(ctx, work, "the prompt\n", judgeConfig{Settings: s})`
    verwacht argv `-p --model opus --tools Read,Grep,Glob --strict-mcp-config --setting-sources ""
    --settings <s> --no-session-persistence --output-format json --json-schema <schema>` (de lege
    string als eigen argument).
  - `TestExecJudgeEnv`: `fakeClaude` met body `env > <rec>/env`; met `Env: {"LH_T=1"}` staat
    `LH_T=1` in dat bestand, naast `PATH`.
  - `TestLoadJudgeConfig`: een bestand met `env` `{"B":"2","A":"1"}`, `modelSettings`
    `{"effort":"high"}` en `permissions` `{"allow":["Read(/**)"]}` → `Env` `{"A=1","B=2"}`,
    `Settings` `{"disableAllHooks":true,"modelSettings":{"effort":"high"}}`, en `permissions`
    staat in geen van beide. Ontbrekend → `{"disableAllHooks":true}`. `{` → fout met het pad.
    `env` `{"A":1}` → fout.
  - `TestJudgeSettingsPath`: met `CLAUDE_CONFIG_DIR=/c` → `/c/settings.json`; zonder, met
    `HOME=/h` → `/h/.claude/settings.json`.
- [ ] **Step 2: Run** `go test ./internal/cli -run 'TestExecJudge|TestLoadJudgeConfig|TestJudgeSettingsPath'`
  — Expected: FAIL.
- [ ] **Step 3: Implement** `judgeConfig`, `loadJudgeConfig`, `judgeSettingsPath`, `judgeArgs` en
  `execJudge` met `cmd.Env = append(os.Environ(), cfg.Env...)`. Werk het comment van
  `judgeArgs` bij (spec-verwijzing naar de leesgrens). Geef `judgeFunc` de nieuwe parameter en
  laat `round.issue` voorlopig `judgeConfig{Settings: `{"disableAllHooks":true}`}` meegeven;
  pas `fakeJudge.judge` aan op de signatuur.
- [ ] **Step 4: Run** `go test ./internal/cli` — Expected: PASS.
- [ ] **Step 5: Commit** `fix(triage): oordeel zonder gebruikersinstellingen, behalve env en modelSettings (#183)`

### Task 6: De ronde oordeelt in de export

**Files:**
- Modify: `internal/cli/triage.go` (`round.repo`, `round.issue`, nieuw `round.settingsFile`),
  `internal/cli/triage_gather.go:21-32` (`repoGather`)
- Test: `internal/cli/triage_test.go`

**Interfaces:**
- Consumes: `exportTree` (Task 4), `loadJudgeConfig`, `judgeSettingsPath`, `judgeConfig` (Task 5),
  `tri.Catalog`, `tri.CheckRouteSkill` (Task 3).
- Produces: velden `repoGather.export string` (de cwd van elk oordeel) en
  `repoGather.judge judgeConfig`.

- [ ] **Step 1: Pas de fixture aan**: `triageFixture` commit en pusht ook
  `.claude/skills/lighthouse-route-issue/SKILL.md` (frontmatter `name: lighthouse-route-issue`), en
  zet `SettingsFile` op een niet-bestaand pad in `t.TempDir()`. `fakeJudge.judge` eist niet meer
  `dir == j.dir`, maar dat `filepath.Base(dir)` met `lighthouse-triage-` begint en `dir != j.dir`;
  per issue bewaart hij `dirs[n]` en `files[n]` (de niet-mappen onder `dir`, relatief,
  gesorteerd) en `cfg`.
- [ ] **Step 2: Write the failing tests**:
  - `TestTriageJudgesInExport`: na de fixture een getrackt `.gitattributes` met
    `secret.txt export-ignore` en `secret.txt` op origin; daarna lokaal een niet-gepushte commit
    met `local.txt`, ongetrackt `u.txt`, genegeerd `.env` (via een gepushte `.gitignore`). Het
    oordeel van #7 ziet precies `.claude/skills/lighthouse-route-issue/SKILL.md`,
    `.gitattributes`, `.gitignore`, `lighthouse.toml`; na de ronde bestaat `dirs[7]` niet meer; de
    handoff noemt nog `worktree: <fx.dir>`.
  - `TestTriageSkipsWithoutRouteSkill`: de route-skill van origin weg (`git rm`, commit, push) →
    exit 2, stderr bevat `skip` en `lighthouse-route-issue`, geen oordeel.
  - `TestTriageSettingsInvalid`: `SettingsFile` bevat `{` → exit 2, stderr bevat `skip` en het pad,
    geen oordeel.
  - `TestTriageSettingsPassed`: `SettingsFile` met `env` `{"A":"1"}` en `modelSettings`
    `{"effort":"high"}` → `cfg.Env` van het oordeel is `{"A=1"}` en `cfg.Settings` is
    `{"disableAllHooks":true,"modelSettings":{"effort":"high"}}`.
  - `TestTriageBrakeMidRound`: daarnaast bestaat `dirs[<eerste issue>]` na de ronde niet meer.
  - Twee issues in één repo: `dirs` van beide gelijk (één export per repo per ronde).
- [ ] **Step 3: Run** `go test ./internal/cli -run TestTriage` — Expected: FAIL (de ronde oordeelt
  nog in `repo.Dir`).
- [ ] **Step 4: Implement** in `round.repo`, na `prepare`, in deze volgorde, elk met de gewone
  `lighthouse: skip <path>: <fout>`-regel: `loadJudgeConfig(r.settingsFile())`;
  `exportTree(ctx, repo.Dir, "origin/"+repo.DefaultBranch)` met `defer remove()`;
  `tri.CheckRouteSkill(export)` (fout met `origin/<branch>: `-voorvoegsel);
  `tri.Catalog(export, repo.Dir, r.pluginsFile())`. Zet `g.export` en `g.judge`; `round.issue`
  roept `r.env.Judge(ctx, g.export, prompt, g.judge)` aan.
- [ ] **Step 5: Run** `go test ./internal/cli ./internal/triage` en
  `golangci-lint run ./...` — Expected: PASS, geen bevindingen.
- [ ] **Step 6: Commit** `fix(triage): oordeel in een export van origin/<default_branch> (#183)`

### Task 7: Documentatie en changelog

**Files:**
- Modify: `docs/developer/planning/specs/2026-11-01-triage-design.md:234-247` (*Het oordeel*),
  `docs/triage.md:17-20` (stap 2) en `:31-36` (vrije tekst)
- Create: `changelog.d/issue-183-triage-leesbereik.md` (naar de branch)

- [ ] **Step 1: Triage-spec**: "in de main checkout van de repo" wordt "in een export van
  `origin/<default_branch>`", met een relatieve link naar
  `2026-11-01-triage-leesgrens-design.md` in dezelfde map; het
  vlaggenblok krijgt `--setting-sources ""` en `--no-session-persistence`.
- [ ] **Step 2: `docs/triage.md` stap 2**: waar het oordeel draait (een tijdelijke map met de
  getrackte inhoud van `origin/<default_branch>`, weg na de ronde), wat het kan lezen (alleen die
  map; geen ongetrackte of genegeerde bestanden, geen `.git`, geen worktrees), welke instellingen
  het overneemt (`env`, `modelSettings`), en dat de ronde een repo overslaat zonder route-skill of
  met kapotte instellingen. Bij de alinea over vrije tekst: de grenzen uit de tabel van de spec, en
  dat een overschrijding het oordeel ongeldig maakt.
- [ ] **Step 3: Changelog**: `type: fixed`, één tot drie Engelse zinnen: het oordeel leest alleen
  de getrackte inhoud van de default branch, en vrije velden met proza of te veel tekst maken het
  oordeel ongeldig.
- [ ] **Step 4: Check** `md-link-check` blokkeert niet (gebeurt bij de Write); `go test ./...` —
  Expected: PASS.
- [ ] **Step 5: Commit** `docs(triage): leesgrens en grenzen op de vrije velden (#183)`

### Task 8: Metingen op de host, voor de PR

Geen code, tenzij een meting dat afdwingt. Elke uitkomst gaat met de ruwe uitvoer in de PR-body.

- [ ] **Step 1: AC 1, meting.** Pak `git archive origin/main` van deze repo uit in een tijdelijke map
  en draai daar `claude` met de argv van `judgeArgs` (`--model opus`), met een prompt die vraagt om
  `Read` van een bestand buiten de map, `Grep` en `Glob` op een map erbuiten, `Read` via een
  symlink naar een bestand erbuiten, en `Grep`/`Glob` via een symlink naar een map erbuiten.
  Expected: elk in `permission_denials`.
- [ ] **Step 2: `modelSettings` en `env`.** Draai `claude` direct in de export van Step 1, met de
  argv van `judgeArgs`, de `env` `OTEL_METRICS_EXPORTER=console` en
  `CLAUDE_CODE_ENABLE_TELEMETRY=1` op het proces, en een `--settings`-JSON zoals
  `loadJudgeConfig` die bouwt, met een `modelSettings`. Raak `~/.claude/settings.json` niet aan en
  zet geen `CLAUDE_CONFIG_DIR` (dat verplaatst ook de login). Expected: de telemetrie verschijnt;
  het effort-niveau is dat uit `modelSettings`. Werkt `modelSettings` niet, meld het in de PR.
- [ ] **Step 3: Paden en vlaggen.** Meet welk instellingenbestand `claude` op de host leest
  (`CLAUDE_CONFIG_DIR` gezet of niet); of `disableAllHooks` met `--setting-sources ""` nog iets
  doet (laat hem staan tenzij niet); en of `--no-session-persistence` voorkomt dat er een map
  onder `~/.claude/projects/` voor de export komt. Voorkomt hij dat niet, meld het in de PR.
- [ ] **Step 4: De getallen.** Lighthouse heeft geen `[controller.triage]` (`lighthouse.toml:54`
  heeft alleen `[controller]`), en zonder die tabel slaat de ronde de repo over en eindigt met
  `no repo with [controller.triage]` (`internal/cli/triage.go:109-127`). Daarom, alleen voor de
  meting en nooit gecommit:
  1. Voeg onderaan de `lighthouse.toml` van deze worktree een lege tabel `[controller.triage]` toe.
  2. Schrijf in de scratchpad een `host.toml` met `[dispatch]` `repos = ["<absoluut pad van deze
     worktree>"]` en verder de `[dispatch]`-sleutels uit `~/.config/lighthouse/host.toml`.
  3. Draai `LIGHTHOUSE_HOST_TOML=<die host.toml> go run ./cmd/lighthouse triage --dry-run`, met
     dezelfde forge-token als de tick. Ontbreekt de token of faalt de forge, meld het in de PR en
     sla de meting over; verander niets in de main checkout.
  4. `git checkout -- lighthouse.toml`; `git status --porcelain` is daarna leeg.

  Leg de routeregels, redenen en ontwerpvragen naast 80/200/300/150 en naast de vormen van de
  route-skill (`— touches open question "<question>" from the milestone`, `· layer: <layer>
  (<file>)`). Valt een gewoon oordeel erboven: hoog de constante op in `limits.go`, pas de test,
  de grens in `docs/triage.md` en de tabel in
  `docs/developer/planning/specs/2026-11-01-triage-leesgrens-design.md` mee aan, draai
  `go test ./...`, commit `fix(triage): grens <veld> naar <n> na meting (#183)`, en noem het in de
  PR.
