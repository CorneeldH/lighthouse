# Historie-analyse (`lighthouse history`) Implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `lighthouse history` leest de Claude Code-transcripten van de repo en print één JSON-verslag met bevindingen, voorstellen en een aanbevolen instapniveau; de agentgids krijgt de sectie History.

**Architecture:** Het vinden van transcripten verhuist uit `internal/cli/telemetry_collect.go` naar `internal/transcripts/` (eerst verhuizen, zonder gedragswijziging). `internal/history/` leest records regel voor regel, voert ze aan zes detectoren (één bestand elk) en past de niveauregel toe; `internal/cli/history.go` is alleen cobra en omgeving.

**Tech Stack:** Go, cobra, `encoding/json`, `bufio.Scanner`; geen nieuwe dependencies.

**Spec:** [`../specs/2026-11-24-issue-491-historie-analyse-design.md`](../specs/2026-11-24-issue-491-historie-analyse-design.md)

## Global Constraints

- Sleutels, `meaning`, `proposal.text`, `reason` en code in het Engels (`code_language = "en"`); plan, commits en changelog in het Nederlands.
- Verslag: `"schema": 1`; `status` ∈ `found|none|no_data`; `confidence` ∈ `high|low`; `proposal.kind` ∈ `knob|advice`.
- Exit 0 met verslag (ook zonder transcripten: `sessions: 0`, elk onderdeel `no_data`, niveau `light` met reden `no history`); exit 1 bij onleesbare projectenmap; exit 2 bij gebruiksfout, ook `lighthouse history: <dir> is not a git repository`.
- Geen netwerk, geen schrijfacties, geen `lighthouse.toml` lezen. Nooit een prompt, tool-uitvoer of bestandsinhoud in de uitvoer; `measured` bevat alleen getallen, branchnamen, padpatronen en vaste patroonnamen.
- `sources`: alleen met `--sources`; hoogstens 5 bestanden per bevinding, 10 regelnummers per bestand, meest recente eerst.
- Niveaudrempels als constanten in één bestand (`internal/history/level.go`): `standard` = ≥3 PR's, <20% pushes op de trunk, testcommando vóór ≥ de helft van de commits in dezelfde sessie; `strict` = `standard` plus ≥1 `codex`-aanroep.
- Leesgrens `MEMORY.md`: 200 regels. Scanner-buffer ruim genoeg voor regels van meerdere MB.
- Per commit: `[test_gate] command` en `lighthouse imports check`; atomaire Conventional Commits.

## Review Focus

- Projectmap van een vreemde repo zonder `origin` in de eigen repo: mag niet meetellen (lege slug mag nooit alles matchen).
- Een transcriptregel van enkele MB (een `Write` met een heel bestand): geen `bufio.ErrTooLong`, geen afbreken.
- Windows-cwd's (`D:\…`, `d:\…`) onder Linux: matchen via normalisatie en tellen als één project voor `memory`.
- Commando met `cd <wt> && git push` in een sessie op `main`: telt als onbekende branch, niet als push op de trunk.
- Een record zonder `timestamp`, of een ongeldige `--since`: geen panic; ongeldige datum is exit 2.

---

### Task 1: `internal/transcripts` — verhuizing

**Files:**
- Create: `internal/transcripts/transcripts.go`, `internal/transcripts/transcripts_test.go`
- Modify: `internal/cli/telemetry_collect.go` (verwijder `session`, `findTranscripts`, `firstCwd`, `cwdOf`, `belongsToRepo`, `cwdScanLines`; gebruik het nieuwe package)

**Interfaces:**
- Produces:
  - `type Session struct { ID, Path string; Subs []string }`
  - `type Options struct { FoldCase bool }` — `FoldCase` vergelijkt ook de roots-regel zonder hoofdletters; en normaliseert elke cwd met stationsletter (`X:\a\b` → `/mnt/x/a/b`), en splitst de slug-regel op `/` en `\`. Nul-waarde is het huidige gedrag.
  - `func Find(projects string, roots, slugSegs []string, o Options) ([]Session, error)`
  - `func BelongsToRepo(cwd string, roots, slugSegs []string, o Options) bool`
  - Een lege `slugSegs` (of een leeg segment) slaat de slug-regel over.

- [ ] **Step 1: Verhuis zonder wijziging.** Verplaats de functies, hernoem `session`→`Session` (`id`→`ID`, `path`→`Path`, `subs`→`Subs`) en pas `collectOne`/`runCollect`/tick aan. Zet `Options{}` en `Find`/`BelongsToRepo` op de plaats van de oude aanroepen. Draai `go test ./internal/cli/... -run 'Collect|Telemetry|Tick'`; verwacht PASS zonder testwijziging.
- [ ] **Step 2: Commit** `refactor(transcripts): transcripten vinden naar een eigen package (#491)`.
- [ ] **Step 3: Schrijf falende tests** in `transcripts_test.go` (tabeltest op `BelongsToRepo`, plus één `Find` op een tijdelijke projectenmap):
  - `TestBelongsToRepo_EmptySlugMatchesNothing`: `roots=[/r]`, `slugSegs=nil`, cwd `/elders/x` → `false`; cwd `/r/sub` → `true`.
  - `TestBelongsToRepo_FoldCase`: root `D:\Work\repo` genormaliseerd; cwd `d:\work\repo\wt` met `FoldCase` → `true`, zonder → `false`.
  - `TestBelongsToRepo_WindowsCwdOnLinux`: cwd `C:\Users\u\o\r` met slug `o/r` en `FoldCase` → `true`; root `/mnt/c/Users/u/repo` en cwd `C:\Users\u\repo\x` → `true`.
  - `TestFind_SubagentsBelongToParent`: `<id>.jsonl` plus `<id>/subagents/a.jsonl` geeft één `Session` met `Subs` van lengte 1.
- [ ] **Step 4: Run** `go test ./internal/transcripts/` — verwacht FAIL (vooral lege slug en Windows-cwd).
- [ ] **Step 5: Implementeer** de opties. Normalisatie: regex `^([A-Za-z]):[\\/]` → `/mnt/<kleine letter>/`, daarna `\`→`/`; alleen met `FoldCase`. Lege-slug-regel voor alle callers.
- [ ] **Step 6: Run** `go test ./internal/transcripts/ ./internal/cli/...` — verwacht PASS.
- [ ] **Step 7: Commit** `feat(transcripts): FoldCase, Windows-cwd en lege slug (#491)`.

### Task 2: `internal/history` — records, verslag, branch-bepaling

**Files:**
- Create: `internal/history/record.go`, `report.go`, `branch.go`, `analyze.go` en `_test.go` per bestand

**Interfaces:**
- Produces:
  - `type Record struct { File string; Line int; Type, Timestamp, GitBranch, Cwd, Entrypoint, SessionID string; Commands []string; WrittenPaths []string; PRNumber int; HookName, HookCommand string }` — `Commands` = `input.command` van `Bash`-tool_use, `WrittenPaths` = `input.file_path` van `Write`/`Edit`, `PRNumber` uit `pr-link`, `HookCommand` = `attachment.blockingError.command`. Nooit `blockingError.blockingError` of tekst van prompts of uitvoer.
  - `func ReadFile(path string, since time.Time, fn func(Record)) (skipped int, err error)` — `bufio.Scanner` met buffer ≥ 64 MB; onleesbare regel telt als `skipped`; record met `timestamp` vóór `since` wordt overgeslagen; geen timestamp telt als niet vóór `since`.
  - `type Detector interface { Add(Record); Finish(Env) Finding }`
  - `type Env struct { Repo string; TrunkNames []string; ProjectsDir string; ProjectDirs []string; RepoRoot string; AuditHooks []HookOverlap }` (zie taken 5 en 8 voor wie wat vult)
  - `type Finding struct { Key, Status, Confidence string; Measured map[string]any; Meaning string; Proposal Proposal; Sources []Source }`, `Proposal{Kind, Text, Flag string}`, `Source{File string; Lines []int}`, `Report{Schema int; Sessions int; Span Span; SkippedLines int; Level Level; Findings []Finding; Hint string}` met JSON-tags uit de spec (`omitempty` op `hint`, `flag`, `sources`).
  - `type Hits struct{}` met `Add(file string, line int)` en `Top() []Source`: houdt de 5 meest recente bestanden (op volgorde van aanroep, laatste = recentst) en per bestand de 10 laatste regels.
  - `func ResolveBranch(command, gitBranch string, knownCwds map[string]string) (branch string, known bool)` — volgorde uit de spec: refspec-doel (`HEAD:main`, `origin main`) → anders `cd <pad>`/`git -C <pad>` zonder match in `knownCwds` (cwd → gitBranch uit dezelfde sessie) = onbekend → anders `gitBranch`.
  - `func Analyze(files []SessionFiles, env Env, o AnalyzeOptions) Report` met `SessionFiles{ID string; Paths []string}` en `AnalyzeOptions{Since time.Time; Sources bool}`: telt `sessions` = aantal `SessionFiles`, vult `span` uit timestamps, voert records aan alle detectoren, bouwt `Level` (taak 9), laat `sources` weg zonder `Sources`.

- [ ] **Step 1: Failende tests.**
  - `TestReadFile_LongLine`: regel van 5 MB met `Write`-`file_path` → record gelezen, `skipped == 0`.
  - `TestReadFile_BadLineAndSince`: één kapotte regel → `skipped == 1`; `since` laat oudere record weg.
  - `TestResolveBranch`: `git push origin HEAD:main` + `gitBranch=x` → `main`; `git -C /wt push` + `gitBranch=main`, `/wt` onbekend → onbekend; zelfde met `knownCwds{"/wt":"issue-1"}` → `issue-1`; `git push` zonder refspec → `gitBranch`.
  - `TestHits_Caps`: 7 bestanden × 12 regels → 5 bestanden, 10 regels, laatste eerst.
  - `TestReadFile_NoContentKept`: record met `SECRET-PROMPT-1` in een `user`-bericht heeft die tekst in geen enkel veld.
- [ ] **Step 2: Run** `go test ./internal/history/` — verwacht FAIL.
- [ ] **Step 3: Implementeer** de typen en functies hierboven. `Analyze` mag detectoren registreren via een slice `detectors(env) []Detector` in `analyze.go` die in taken 3–8 groeit.
- [ ] **Step 4: Run** — verwacht PASS. Commit `feat(history): records lezen, verslagtypen en branch-bepaling (#491)`.

### Task 3: detector `trunk`

**Files:** Create `internal/history/trunk.go`, `trunk_test.go`

**Interfaces:**
- Consumes: `Record`, `ResolveBranch`, `Hits`, `Env.TrunkNames` (default branch uit `origin/HEAD`, indien aanwezig).
- Produces: `func newTrunk(env Env) Detector`; `func isTrunkName(name string, defaultBranch string) bool` — `main|master|develop|trunk|release/*` of de default branch.

- [ ] **Step 1: Tabeltest** `TestTrunk`: (a) 41 pushes `HEAD:release/r5.11` en 10 op `release/r1` → `found`, `measured.trunk_names == ["release/r1","release/r5.11"]` (volgorde eerste push), `pushes_by_branch["release/r5.11"] == 41`, voorstel `advice`; (b) `gitBranch=main` met `git -C <wt> push` → onbekend, telt in `unknown_branch` en niet in de verhouding; (c) >50% onbekend → `confidence: low`; (d) geen commits of pushes → `none`; (e) zonder records → `no_data` bij `sessions: 0`.
- [ ] **Step 2: Run** — FAIL. **Step 3: Implementeer** met vaste regex op `git push` / `git commit`; `measured`: `commits_by_branch`, `pushes_by_branch`, `trunk_names`, `trunk_push_ratio` (float, 0 bij noemer 0), `unknown_branch`. Meaning en proposal-tekst uit de spec-tabel.
- [ ] **Step 4: Run** — PASS. Commit `feat(history): detector trunk (#491)`.

### Task 4: detector `work_tracking`

**Files:** Create `internal/history/worktracking.go`, `worktracking_test.go`

**Interfaces:** Produces `func newWorkTracking() Detector`; levert ook de tellingen voor de niveauregel via `func (d *workTracking) PRs() int` (unieke `prNumber` uit `pr-link` plus unieke `pr create`/`mr create`-aanroepen per sessie; `gh pr view` telt niet) en `IssueCommands() int`.

- [ ] **Step 1: Tabeltest**: `gh issue`, `gh pr`, `lighthouse forge pr|issue`, `glab mr|issue` tellen; dezelfde `prNumber` in twee records telt één; `Write` naar `TODO.md`, `docs/tasks/x.md`, `a.sqlite`, `b.db`, `memory/x.md` telt als `file_tasks`; met issues/PR's → voorstel `knob` met `flag: "--labels"`, anders `advice`; geen van beide → `none`.
- [ ] **Step 2: Run** FAIL, **Step 3: Implementeer**, **Step 4: Run** PASS. Commit `feat(history): detector work_tracking (#491)`.

### Task 5: detector `memory`

**Files:** Create `internal/history/memory.go`, `memory_test.go`

**Interfaces:**
- Consumes: `Env.ProjectDirs []string` (de projectmappen onder `ProjectsDir` die bij de repo horen); records voor `Cwd`.
- Produces: `func newMemory(env Env) Detector`. Leest alleen bestandsnamen, aantallen en regeltellingen van `<projectdir>/memory/`; nooit inhoud.

- [ ] **Step 1: Test** met tijdelijke mappen: 213 bestanden verdeeld over `d--work-r` en `D--work-r` → `measured.files == 213`, `case_variant_dirs == 1`; `MEMORY.md` met 200 regels → `index_at_limit: true`, 199 → `false`; cwd's `D:\x` en `d:\x` in records → `case_variant_cwds == 1`; geen `memory/`-map → `none`; voorstel altijd `advice`. Test ook dat de tekst `SECRET-MEMORY-…` in een memory-bestand nergens in de uitvoer komt.
- [ ] **Step 2–4: Run FAIL / implementeer / run PASS.** Commit `feat(history): detector memory (#491)`.

### Task 6: detector `shell`

**Files:** Create `internal/history/shell.go`, `shell_test.go`

**Interfaces:** Produces `func newShell() Detector`; `measured`: `entrypoints` (naam→aantal), `cwd_forms` (`windows_drive`, `msys` voor `/c/`, `wsl_mnt` voor `/mnt/c/`, `posix_home`), `patterns` (`powershell`, `cmd_c`, `wsl`, naam→aantal), `crlf_signals` (aantal), `platform` ∈ `windows|wsl|linux|unknown`.

- [ ] **Step 1: Tabeltest**: cwd `C:\Users\u\r` + commando `powershell` → `platform: windows`; cwd `/mnt/c/…` → `wsl`; `/home/u/r` → `linux`; platform alleen uit padvorm → `confidence: low`, met een shell-commando-patroon → `high`; `dos2unix`, `core.autocrlf`, `git config … eol`, `\r` in een commando tellen als `crlf_signals`; voorstel `advice` met `.gitattributes` (`* text=auto eol=lf`) alleen bij `crlf_signals > 0`.
- [ ] **Step 2–4: Run FAIL / implementeer / run PASS.** Commit `feat(history): detector shell (#491)`.

### Task 7: detector `hooks`

**Files:** Create `internal/history/hooks.go`, `hooks_test.go`

**Interfaces:**
- Consumes: `agentaudit.Overlap`, `agentaudit.Collect`, `agentaudit.Env` en `packs.Guard` (niet gewijzigd).
- Produces: `type HookOverlap struct { Hook, Guard string }`; `func OverlapFromAudit(env agentaudit.Env, guards []packs.Guard) ([]HookOverlap, error)` — draait `Collect` en `Overlap`, houdt alleen findings met bron `project`, `lokaal` of `gebruiker` (niet `lighthouse`, niet `plugin:*`), en bouwt de Engelse namen uit `Finding.Key` (`overlap:<guard>|<bron>/<pad>`), nooit uit `Finding.Text`; `func newHooks(env Env) Detector` met `measured`: `overlapping` (lijst `{hook, guard}`), `blocking_by_hook` (basename van `HookCommand` → aantal).

- [ ] **Step 1: Tests**: `OverlapFromAudit` op een tijdelijke repo met een eigen `PreToolUse`-hook die `git commit` blokkeert → één overlap met guard `main-commit-gate`; pluginhook uitgesloten. `TestHooks`: `HookCommand=/x/y/run.sh` ×3 → `blocking_by_hook["run.sh"] == 3`; `blockingError.blockingError`-tekst nergens in `measured`; voorstel `advice` met naam van de hooks en verwijzing naar `/lighthouse-audit-agent-config`; geen overlap en geen blokkades → `none`.
- [ ] **Step 2–4: Run FAIL / implementeer / run PASS.** Commit `feat(history): detector hooks (#491)`.

### Task 8: detector `worktrees`

**Files:** Create `internal/history/worktrees.go`, `worktrees_test.go`

**Interfaces:** Produces `func newWorktrees() Detector`; `measured`: `adds` (aantal `git worktree add`), `distinct_worktrees`, `path_patterns` (patroonnaam→aantal: `~/.claude/worktrees`, `repo-sibling`, `sync_folder`), `sync_folders` (`OneDrive|Dropbox|iCloud`→aantal).

- [ ] **Step 1: Tabeltest**: `git worktree add C:\Users\u\OneDrive\wt` en cwd's onder dat pad → `sync_folders["OneDrive"] ≥ 1`, `found`, voorstel `advice`; alleen `.claude/worktrees` → `found` zonder sync-map; geen worktrees → `none`. Het padpatroon, niet het volledige pad, staat in `measured` (gebruikersnaam hoort er niet in).
- [ ] **Step 2–4: Run FAIL / implementeer / run PASS.** Commit `feat(history): detector worktrees (#491)`.

### Task 9: niveauregel

**Files:** Create `internal/history/level.go`, `level_test.go`

**Interfaces:**
- Consumes: tellingen uit `trunk` (`trunk_push_ratio`, bekende pushes), `workTracking.PRs()`, en een nieuwe `type chainCounts struct { Commits, CommitsAfterTest, Codex int }` die `Analyze` per sessie (ouder + subagents) bijhoudt: een testcommando (`go test`, `pytest`, `npm test`, `uv run pytest`, `cargo test`, `lighthouse gate`) eerder in dezelfde sessie dan de commit; `codex` = aanroep van `codex`.
- Produces: `type Level struct { Recommended, Reason string }`; `func decideLevel(c Counts) Level` met constanten `minPRs = 3`, `maxTrunkPushRatio = 0.20`, `minTestedCommitRatio = 0.5`.

- [ ] **Step 1: Tabeltest `TestDecideLevel`**: 3 PR's, 10% trunk, 60% getest → `standard`; plus 1 codex → `strict`; 2 PR's → `light` met reden die "PRs" noemt; 20,0% trunk → `light` (grens is "minder dan 20%"); noemer 0 (geen pushes met bekende branch / geen commits) → niet voldaan; `sessions: 0` → `light` met reden `no history`; `standard`-reden noemt waarom geen `strict`.
- [ ] **Step 2–4: Run FAIL / implementeer / run PASS.** Koppel in `Analyze`. Commit `feat(history): niveauregel (#491)`.

### Task 10: cobra-commando `lighthouse history`

**Files:**
- Create: `internal/cli/history.go`, `internal/cli/history_test.go`
- Modify: `internal/cli/root.go` (`root.AddCommand(newHistoryCommand(defaultHistoryEnv()))` naast regel 111)

**Interfaces:**
- Consumes: `transcripts.Find` met `Options{FoldCase: true}`, `history.Analyze`, `history.OverlapFromAudit`.
- Produces: `type historyEnv struct { Getenv func(string) string; Cwd func() (string, error); Stdout io.Writer }`; `func defaultHistoryEnv() historyEnv`; `func newHistoryCommand(env historyEnv) *cobra.Command` met `--sources`, `--since <ISO-datum>`, `--projects-dir <map>`; `func repoSlugFromRemote(dir string) string` die dezelfde ontleding als `initcmd` gebruikt (`projectconfig.ForgeFromRemote(url, nil)`), leeg zonder `origin` of bij een niet-ontleedbare remote.

- [ ] **Step 1: Failende tests** (met `execute`-helper en tijdelijke git-repo's zoals `telemetry_collect_test.go`):
  - `TestHistory_NotGitRepo`: exit 2, stderr `lighthouse history: <dir> is not a git repository`.
  - `TestHistory_NoTranscripts`: exit 0, `sessions: 0`, elk onderdeel `no_data`, niveau `light` / `no history`.
  - `TestHistory_ForeignProjectWithoutOrigin`: repo zonder `origin`, projectmap met cwd in een andere repo → `sessions: 0`.
  - `TestHistory_UnreadableProjectsDir`: exit 1.
  - `TestHistory_BadSince`: `--since morgen` → exit 2.
  - `TestHistory_WSLHint`: `/proc/version` met `microsoft` (via `historyEnv`-hook voor het bestandspad) en 0 sessies → `hint` noemt `/mnt/c/Users/<naam>/.claude/projects`.
  - `TestHistory_SessionOnWSLPathAndSlug`: fixture met Windows-cwd's vanaf repo-pad `/mnt/c/…` (via roots) en vanaf `/home/…` (alleen via slug).
- [ ] **Step 2: Run** — FAIL. **Step 3: Implementeer.** Roots = repo-root plus `git worktree list --porcelain` (hergebruik `gitWorktrees`); `Env.TrunkNames` uit `git symbolic-ref refs/remotes/origin/HEAD`; `ProjectDirs` uit de projectmappen die `transcripts.Find` aanwees (voeg daarvoor aan `Find` niets toe: leid ze af uit `filepath.Dir(Session.Path)`). Hooks: `agentaudit.DefaultEnv(root)` heeft vóór `init` geen manifest, dus zet `env.Packs` zelf uit alle mappen van `pack.Files` via `packs.Load`; fout daarbij laat `hooks` op `no_data`, geen exit 1. Druk JSON af met `json.MarshalIndent`; schrijf nergens.
- [ ] **Step 4: Run** — PASS. Commit `feat(cli): lighthouse history (#491)`.

### Task 11: fixtures, golden, privacy, geen schrijfacties

**Files:** Create `internal/history/testdata/{tweede-gebruiker,keten,keten-codex}/…` (synthetische `projects/`-bomen met `.jsonl`), `internal/history/testdata/golden/*.json`, `internal/cli/history_golden_test.go`

- [ ] **Step 1: Bouw de fixtures.** "tweede gebruiker": trunk schuift `release/r1`→`release/r5.11`, schrijfacties naar `TODO.md`, Windows-cwd's `D:\` en `d:\`, Git Bash + CRLF-signalen, OneDrive-worktrees → verwacht `light`. "keten": 3+ `pr-link`'s, weinig trunkpushes, testcommando's vóór commits → `standard`. "keten-codex": idem plus een `codex`-aanroep → `strict`. Prompts, tool-uitvoer en bestandsinhoud dragen `SECRET-PROMPT-1`, `SECRET-OUTPUT-1`, `SECRET-CONTENT-1`.
- [ ] **Step 2: Tests** in `history_golden_test.go`:
  - `TestHistory_Golden`: per set het hele commando, vergelijk met golden JSON (`-update`-vlag om te vernieuwen; golden handmatig nakijken).
  - `TestHistory_NoSecrets`: uitvoer met en zonder `--sources` bevat geen `SECRET-`.
  - `TestHistory_Sources`: met `--sources` dragen `found`-bevindingen `sources` binnen de limieten, zonder niet.
  - `TestHistory_Unreadable`: fixture met één kapotte regel → `skipped_lines: 1`.
  - `TestHistory_WritesNothing`: boomhash (paden + inhoud + mtimes) van tijdelijke home en repo vóór en na is gelijk.
  - `TestHistory_NoNetwork`: draait met `HTTP_PROXY` naar een afgesloten poort en een listener die faalt bij een verbinding.
- [ ] **Step 3: Run** `go test ./internal/history/... ./internal/cli/ -run History` — PASS; `go test ./...` — PASS. Commit `test(history): synthetische transcripten met golden JSON en privacytest (#491)`.

### Task 12: documentatie en changelog

**Files:** Modify `docs/site/agents/install.md`, `docs/install.md`; Create `changelog.d/issue-491-historie-analyse.md`

- [ ] **Step 1: `docs/site/agents/install.md`.** Voeg sectie **History** toe met de stappen 1–6 uit de spec (niveaus lokaal · diep · niet met de toestemmingstekst per harness; `lighthouse history`; `--sources` en `sed -n '<n>p' <file> | cut -c1-4000`, hoogstens 10 regels per bevinding en 50 samen; één bericht met voorstellen; foutafhandeling). Wijzig de volgorde: keuze 1 en de binaire installatie eerst, dan History, dan keuzes 2–5, 8 en 9; zonder `history` in `lighthouse --help` slaat de agent History over met een regel onder "Still open". Pas "Ground rules" aan voor die uitzondering.
- [ ] **Step 2: `docs/install.md`.** Korte alinea onder "Start small" over `lighthouse history`.
- [ ] **Step 3: Changelog-fragment** (`type: added`, Nederlands, noemt #491).
- [ ] **Step 4: Controleer** `go run ./cmd/lighthouse imports check`, links (`md-link-check` blokkeert), `go test ./...`, en `go run ./cmd/lighthouse init --upgrade` (verwacht `No changes.`; er verandert niets onder `pack/`). Commit `docs(history): agentgids en installatiepagina (#491)`.

---

## Zelfreview

- **Spec-dekking:** commando en flags (taak 10), verslag (2), zes onderdelen (3–8), trunk-/branch-regels (2, 3), niveauregel en randgevallen (9, 4), diep en toestemming (12), code-indeling (1, 2, 10), tests incl. privacy, geen schrijfacties, `skipped_lines` (11), documentatie en changelog (12). Het bijwerken van het issue (AC-regel) is een handeling op GitHub na goedkeuring van de spec, geen codetaak; de bouwer doet dat in het PR-verslag.
- **Typeconsistentie:** `Record`, `Detector`, `Env`, `Finding`, `Hits`, `ResolveBranch`, `Level`, `HookOverlap` zijn in taak 2 of de eigen taak vastgelegd en elders alleen gebruikt. De exacte `Counts`-vorm van taak 9 volgt uit wat `trunk` en `work_tracking` aanleveren; de bouwer houdt die in `level.go`.
- **Proportie:** geen codeblokken met bodies; tests noemen namen en asserties.
