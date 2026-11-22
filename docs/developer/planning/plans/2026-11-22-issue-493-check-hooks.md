# Check dat de geïnstalleerde hooks draaien (#493) Implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `lighthouse check-hooks` bewijst op de eigen machine dat de geïnstalleerde hooks draaien; `init` draait hem vanzelf, houdt CRLF buiten de hookbestanden en repareert CRLF-only verschillen zonder `--force`.

**Architecture:** Een nieuwe package `internal/hookcheck` doet vier stappen (Git Bash, CRLF, registraties, smoketest) en geeft een exitcode, zoals `internal/checkinstr`. `internal/reconcile` normaliseert `\r\n` voor de vergelijking. `internal/initcmd` zet `.gitattributes`-regels en roept een injecteerbare `CheckHooks` aan na een run.

**Tech Stack:** Go, cobra, `os/exec` (`bash -c`), bestaande packages `settings`, `manifest`, `packs`, `orderedjson`.

**Spec:** [2026-11-22-issue-493-check-hooks-design.md](../specs/2026-11-22-issue-493-check-hooks-design.md)

## Global Constraints

- Uitvoer van het commando en hookteksten zijn Engels; plan, spec, PR en commitberichten Nederlands (`[project] language = nl`).
- Exit van `check-hooks`: `0` alles groen, `1` minstens één `FAIL`, `2` `COULD NOT CHECK` (geen repo-root, geen `.claude/settings.json`, onleesbare `settings.json`). Uitvoerregel `ok  <tekst>` of `FAIL  <tekst>` met herstelstap; `COULD NOT CHECK` op stderr als `check-hooks: COULD NOT CHECK — <reden>`.
- Een `COULD NOT CHECK` in stdout of stderr van een gesmoketeste hook is `FAIL`. Exit 0 of 2 zonder die tekst is `ok`; elke andere exit of een timeout is `FAIL`.
- De check kijkt alleen naar registraties waarvan het commando `.claude/hooks/lighthouse/` (`packs.HookDir`) noemt.
- De smoketest verandert niets in de repo of de echte state: state in een tempdir, `LIGHTHOUSE_USAGE_REFRESH=1`, geen `LIGHTHOUSE_CONTROLLER_RUN`, `CLAUDE_PROJECT_DIR` en cwd op de repo-root, de timeout van de registratie.
- `init` houdt exit en stdout gelijk (`No changes.`); de check schrijft naar stderr; niet na een afgewezen diff.
- `.gitattributes`-regels, exact: `*.sh eol=lf` (alleen als er geen regel met precies patroon `*.sh` is) en `.claude/hooks/lighthouse/** eol=lf` (altijd, tenzij de regel al staat). Alleen toevoegen, achteraan; nooit verwijderen, ook niet bij `--no-pack`.
- Reconcile: alleen `\r\n` → `\n`; een losse `\r` blijft een lokale wijziging. De manifest bewaart de sha256 van de LF-inhoud.
- Dogfood: na een wijziging aan `init` of `pack/` draait `go run ./cmd/lighthouse init --upgrade` en gaat de geïnstalleerde kopie mee in de commit. Vóór elke commit ook `lighthouse imports check` naast `[test_gate] command`.
- Elke PR krijgt een fragment in `changelog.d/`.

## Review Focus

- Repo-pad met spaties (`C:\Users\A B\repo`) of `D:` naast `d:`: de smoketest start de hooks nog steeds en `ok` blijft `ok`. Test in Task 3.
- `.gitattributes` zonder afsluitende newline of met CRLF-regeleinden: toegevoegde regels staan op een eigen regel, de bestaande regels blijven byte-gelijk. Test in Task 2.
- `bash` ontbreekt op Linux/macOS: `FAIL` met herstelstap, geen paniek en geen exit 0. Test in Task 3.
- Een gedeeld `.claude/settings.json` met projectregistraties zonder `.claude/hooks/lighthouse/`: die worden niet gesmoketest. Test in Task 3.
- Hook die na 0-exit `COULD NOT CHECK` op stdout zet, of de timeout overschrijdt: `FAIL`, niet `ok`. Test in Task 3.

---

### Task 1: Reconcile telt regeleinden niet als lokale wijziging

**Files:**
- Modify: `internal/reconcile/reconcile.go` (`decide`)
- Modify: `internal/initcmd/initcmd.go` (`writes`/buildPlan, kind `KindFile`)
- Test: `internal/reconcile/reconcile_test.go`, `internal/initcmd/initcmd_test.go`

**Interfaces:**
- Produces: `reconcile.decide` gedrag per spec §Reconcile; geen nieuwe exported namen.

- [ ] **Step 1: Schrijf de falende reconcile-tests** in `reconcile_test.go`, naast de bestaande tabel:
  - `TestDecideCRLFOnlyIsUntouched`: entry-sha = `Sum("a\nb\n")`, current `"a\r\nb\r\n"`, desired `"a\nc\n"` → `Replace`.
  - `TestDecideCRLFOnlyVersusDesiredIsNotUnchanged`: entry-sha = `Sum("a\nb\n")`, current `"a\r\nb\r\n"`, desired `"a\nb\n"` → `Replace` (niet `Unchanged`).
  - `TestDecideNoEntryCRLFOnlyAdopts`: geen entry, current `"a\r\n"`, desired `"a\n"` → `Adopt`.
  - `TestDecideLoneCRStaysConflict`: entry-sha = `Sum("a\nb\n")`, current `"a\rb\n"`, desired `"a\nc\n"` → `Conflict`.
  - `TestDecideRemoveCRLFOnlyIsRemove`: `Desired == nil`, current CRLF-variant van de entry-inhoud → `Remove`.
- [ ] **Step 2: Run** `go test ./internal/reconcile/ -run 'TestDecide' -v`. Verwacht: de nieuwe tests FAIL.
- [ ] **Step 3: Implementeer** in `decide`: `norm := bytes.ReplaceAll(cur.Content, []byte("\r\n"), []byte("\n"))`; `untouched` vergelijkt `manifest.Sum(norm)`; `same` blijft byte-gelijk met `cur.Content` (dus een CRLF-bestand is nooit `same`); het geval `c.Entry == nil && !same && bytes.Equal(norm, desired)` → `Adopt`.
- [ ] **Step 4: Run** dezelfde tests. Verwacht: PASS.
- [ ] **Step 5: Schrijf de falende init-test** `TestUpgradeRepairsCRLFHookWithoutForce` in `initcmd_test.go`: init in een tempdir-repo, herschrijf `.claude/hooks/lighthouse/run.sh` en `blocking` met `\r\n`, draai `--upgrade` zonder `--force` → exit 0, beide bestanden weer zonder `\r`, het diff noemt ze. Tweede test `TestInitAdoptsCRLFFileWithoutManifest`: bestand bestaat met CRLF, geen manifest-entry (`Adopt`) → na `init` LF.
- [ ] **Step 6: Run** `go test ./internal/initcmd/ -run 'CRLF' -v`. Verwacht: FAIL (bij Adopt wordt niet geschreven).
- [ ] **Step 7: Implementeer** in de `KindFile`-tak van `buildPlan`: schrijf ook als `c.Action == reconcile.Adopt && !bytes.Equal(c.Current.Content, c.Desired.Content)`. `writes()` blijft ongewijzigd.
- [ ] **Step 8: Run** `go test ./internal/reconcile/ ./internal/initcmd/`. Verwacht: PASS.
- [ ] **Step 9: Commit** `fix(reconcile): regeleinden tellen niet als lokale wijziging (#493)`.

### Task 2: `init` zet `.gitattributes`-regels

**Files:**
- Create: `internal/initcmd/gitattributes.go`
- Modify: `internal/initcmd/initcmd.go` (`buildPlan`, na `planConfig`)
- Test: `internal/initcmd/gitattributes_test.go`

**Interfaces:**
- Produces: `func ensureGitattributes(old []byte) []byte` — geeft `old` terug met de ontbrekende regels achteraan; identiek aan `old` als niets ontbreekt.

- [ ] **Step 1: Schrijf de falende tests** in `gitattributes_test.go`, tabelgedreven op `ensureGitattributes`:
  - `nil` → `"*.sh eol=lf\n.claude/hooks/lighthouse/** eol=lf\n"`.
  - `"*.sh -text\n"` → alleen `.claude/hooks/lighthouse/** eol=lf\n` erachter; `*.sh` blijft ongemoeid.
  - `"* text=auto"` (zonder newline) → newline erbij, dan beide regels; de eerste regel byte-gelijk.
  - `"a\r\n"` (CRLF-document) → toegevoegde regels met `\r\n`, bestaande regel byte-gelijk.
  - beide regels aanwezig → uitkomst `==` invoer (tweede `init` geeft `No changes.`).
  - `.claude/hooks/lighthouse/** eol=lf` aanwezig gevolgd door `.claude/hooks/lighthouse/** -text` → niets toegevoegd.
  Plus `TestInitWritesGitattributes` (init in tempdir-repo: bestand bevat beide regels, diff noemt `.gitattributes`, `--upgrade` direct erna: `No changes.`) en `TestInitNoPackKeepsGitattributes`.
- [ ] **Step 2: Run** `go test ./internal/initcmd/ -run 'Gitattributes' -v`. Verwacht: FAIL (niet gedefinieerd).
- [ ] **Step 3: Implementeer** `ensureGitattributes`: split op regels, match per regel op patroon (eerste veld) en de regeleinde van het document; voeg alleen toe wat ontbreekt. In `buildPlan`: `p, _ := load(".gitattributes"); p.new = ensureGitattributes(p.old)`. Geen manifest-entry, geen beheerd blok.
- [ ] **Step 4: Run** `go test ./internal/initcmd/`. Verwacht: PASS.
- [ ] **Step 5: Commit** `feat(init): zet .gitattributes-regels tegen CRLF in de hooks (#493)`.

### Task 3: Package `internal/hookcheck`

**Files:**
- Create: `internal/hookcheck/hookcheck.go` (Env, Run, stappen 1, 2, 2b), `internal/hookcheck/smoke.go` (stap 3, payloads)
- Test: `internal/hookcheck/hookcheck_test.go`, `internal/hookcheck/smoke_test.go`

**Interfaces:**
- Produces:
  - `type Env struct { Root, Home, GOOS string; Getenv func(string) string; LookPath func(string) (string, error); Exists func(string) bool }`
  - `func DefaultEnv(root string) Env`
  - `func Run(env Env, out, errOut io.Writer) int` — exit 0/1/2 per Global Constraints.
  - `func FindGitBash(env Env) (path string, err error)` — alleen zinvol op `GOOS == "windows"`; volgorde: `CLAUDE_CODE_GIT_BASH_PATH` (gezet maar afwezig → fout met dat pad), `git.exe` via `LookPath` → `<git-installatie>\bin\bash.exe`, standaardpaden `C:\Program Files\Git\bin\bash.exe` en `C:\Program Files (x86)\Git\bin\bash.exe`.
  - `func Payload(event, matcher string, now time.Time, tmp string) ([]byte, error)` — payloads per spec-tabel.
- Consumes: `settings.Registrations`, `packs.IsLighthouseCommand`, `manifest.Read`, `orderedjson.Parse`.

- [ ] **Step 1: Verifieer Git Bash-volgorde.** Lees de hooks-docs (sectie Windows) en de env-var-docs van Claude Code; leg de gevonden volgorde vast in de doc-comment van `FindGitBash`. Wijkt hij af van spec §Stap 1, pas de volgorde aan en noteer het in de PR.
- [ ] **Step 2: Schrijf de falende tests voor stappen 1, 2, 2b** (`hookcheck_test.go`, fixture-repo in `t.TempDir()` met `.claude/settings.json` en `.lighthouse/manifest.toml` van een echte `init`):
  - `TestFindGitBash`: `GOOS:"windows"` met gefakete `Getenv/LookPath/Exists` — env-pad gezet en aanwezig → dat pad; gezet en afwezig → fout met pad; via `git.exe`; via standaardpad; nergens → fout; `GOOS:"linux"` → `Run` print geen Git Bash-regel.
  - `TestRunCRLFIsFail`: `blocking` met `\r\n` → exit 1, regel `FAIL` noemt het pad en `lighthouse init --upgrade`. Ook een niet-`.sh`-bestand telt.
  - `TestRunUnregisteredHookIsFail`: een hook-unit uit de manifest zonder registratie in `settings.json` → exit 1; herstelstap `lighthouse init --upgrade`.
  - `TestRunDisableAllHooksIsFail`: `disableAllHooks: true` in `.claude/settings.json`, in `.claude/settings.local.json` en in `Home/.claude/settings.json` → elk exit 1.
  - `TestRunCouldNotCheck`: geen `settings.json` → exit 2, `check-hooks: COULD NOT CHECK — …` op stderr; onleesbare JSON → exit 2.
- [ ] **Step 3: Run** `go test ./internal/hookcheck/ -v`. Verwacht: FAIL (package bestaat niet).
- [ ] **Step 4: Implementeer** `Env`, `DefaultEnv`, `FindGitBash`, `Run` (stappen 1, 2, 2b; stap 3 roept `smoke(env, regs, bash, rep)` aan, nu leeg) en de `report` zoals `checkinstr` (`ok`/`FAIL`, teller).
- [ ] **Step 5: Run** dezelfde tests. Verwacht: PASS.
- [ ] **Step 6: Commit** `feat(hookcheck): Git Bash, CRLF en registraties (#493)`.
- [ ] **Step 7: Schrijf de falende smoketests** (`smoke_test.go`), elk met een nep-`run.sh` in een fixture-repo, behalve waar de echte pack nodig is:
  - `TestSmokeOKAndExit2`: exit 0 → `ok`; exit 2 zonder `COULD NOT CHECK` → `ok`.
  - `TestSmokeCouldNotCheckOnExit0IsFail`: stdout bevat `COULD NOT CHECK` bij exit 0 → `FAIL`, regel noemt hook en eerste niet-lege regel van stderr (stdout als stderr leeg is).
  - `TestSmokeOtherExitAndTimeoutAreFail`: exit 1 en een `sleep` die de `timeout` van de registratie overschrijdt → `FAIL`.
  - `TestSmokeSkipsForeignRegistrations`: een registratie zonder `.claude/hooks/lighthouse/` wordt niet gedraaid (nep-commando schrijft een marker; marker ontbreekt).
  - `TestSmokeEnvAndCwd`: nep-hook schrijft `$CLAUDE_PROJECT_DIR`, `pwd`, `$LIGHTHOUSE_USAGE_REFRESH` en `${LIGHTHOUSE_CONTROLLER_RUN-unset}` naar een tempbestand → root, root, `1`, `unset`; ook met een root met spaties.
  - `TestPayloadPerEvent`: `Payload("PreToolUse","Bash",…)` bevat `tool_input.command == "true"`; `"Edit|Write"` een `file_path` zonder `.md` die niet bestaat; `Stop`/`UserPromptSubmit`/`SubagentStop`/`PostToolUse` zonder matcher een `transcript_path` naar een bestand met één prompt-entry zonder tool-use en `timestamp` = `now` naar boven afgerond op de hele seconde (UTC, RFC3339); `Notification` heeft `notification_type: "permission_prompt"`.
  - `TestSmokeRealPackAllOKOnLinux` (skip zonder `bash`/`git`): fixture-repo met `init` van pack `core` plus `go`, met een eerdere commit en een vuil `.go`-bestand, en een niet-dev-binary op de `PATH` → elke hook `ok`, exit 0. Pint de timestamp-afronding: met een vaste oudere timestamp zou `go-lint-gate` echt lopen.
  - `TestSmokeLeavesStateUntouched`: snapshot (bestandenlijst + mtimes) van de repo, `LIGHTHOUSE_STATE_DIR`-origineel en `XDG_RUNTIME_DIR`-origineel vóór en na → gelijk (niet-dev-binary).
  - `TestSmokeNoBashIsFail`: `PATH` leeg op Linux → `FAIL` met herstelstap, exit 1.
  - `TestSmokeWindowsWithoutGitBashSkips`: `GOOS:"windows"`, `FindGitBash` faalt → stap 3 geeft één regel die naar stap 1 verwijst, exit 1 (stap 1 is `FAIL`).
- [ ] **Step 8: Run** `go test ./internal/hookcheck/ -run 'Smoke|Payload' -v`. Verwacht: FAIL.
- [ ] **Step 9: Implementeer** `smoke` en `Payload`. Per registratie: `exec.CommandContext` met `bash -c <command>` (op Windows `bash.exe` uit stap 1), `Dir` en `CLAUDE_PROJECT_DIR` = root, timeout uit het `timeout`-veld van de entry (default 60 s), env gebaseerd op de huidige env met `LIGHTHOUSE_STATE_DIR`, `XDG_RUNTIME_DIR`, `TMPDIR`, `LIGHTHOUSE_HOST_TOML` naar de tempdir, `LIGHTHOUSE_USAGE_REFRESH=1`, `LIGHTHOUSE_CONTROLLER_RUN` verwijderd; payload op stdin. Port van `hook` en `ok` uit `ci/windows-measure.sh`. Verifieer per hook uit `pack/*/hooks/` dat de payload op Linux `ok` geeft; vind zo welke state-variabelen nog nodig zijn.
- [ ] **Step 10: Run** `go test ./internal/hookcheck/`. Verwacht: PASS.
- [ ] **Step 11: Commit** `feat(hookcheck): smoketest van de geregistreerde hooks (#493)`.

### Task 4: Commando `check-hooks` en aanroep na `init`

**Files:**
- Create: `internal/cli/checkhooks.go`
- Modify: `internal/cli/root.go` (`root.AddCommand(newCheckHooksCommand())`), `internal/cli/init.go`, `internal/initcmd/initcmd.go` (`Options.CheckHooks`, aanroep in `execute`)
- Test: `internal/cli/checkhooks_test.go`, `internal/cli/init_test.go`, `internal/initcmd/initcmd_test.go`

**Interfaces:**
- Consumes: `hookcheck.Run`, `hookcheck.DefaultEnv`, `config.RepoRoot`.
- Produces: `newCheckHooksCommand() *cobra.Command` (via `scriptCommand`, fout → `&exitError{code}`); `initcmd.Options.CheckHooks func(root string, errOut io.Writer)`; in `cli`: `var hooksCheck = ...` die een test vervangt, en `checkHooksAfterInit(root string, errOut io.Writer)`.

- [ ] **Step 1: Schrijf de falende tests.**
  - `checkhooks_test.go`: `check-hooks` buiten een repo → exit 2 en `check-hooks: COULD NOT CHECK` op stderr; commando staat in `lighthouse --help`.
  - `initcmd_test.go` met een nep-`CheckHooks`: wordt aangeroepen na schrijven; wordt aangeroepen bij `No changes.`; wordt **niet** aangeroepen als de gebruiker de diff afwijst (`n`); schrijft de nep naar `o.Err` dan blijft `o.Out` byte-gelijk aan `No changes.\n`.
  - `init_test.go`: `hooksCheck` die exit 1 geeft → `init` geeft nog steeds exit 0 en stderr bevat de regel `lighthouse check-hooks` voor de details.
- [ ] **Step 2: Run** `go test ./internal/cli/ ./internal/initcmd/ -run 'CheckHooks' -v`. Verwacht: FAIL.
- [ ] **Step 3: Implementeer** het commando (Long-tekst met exitcodes, Engels) en `checkHooksAfterInit`: draait `hookcheck.Run` met uitvoer naar `errOut`; bij exit ≠ 0 de regel `check-hooks: some hooks do not run; see `lighthouse check-hooks` for details`. In `execute`: na `reportPins`, vóór de audit, `if o.CheckHooks != nil { o.CheckHooks(root, o.Err) }`. `errDeclined` keert eerder terug, dus daar draait hij niet.
- [ ] **Step 4: Run** `go test ./internal/cli/ ./internal/initcmd/`. Verwacht: PASS.
- [ ] **Step 5: Commit** `feat(cli): lighthouse check-hooks en aanroep na init (#493)`.

### Task 5: Windows-CI, gids, changelog, dogfood

**Files:**
- Modify: `ci/windows-measure.sh`, `docs/install.md`, `docs/site/agents/install.md`
- Create: `changelog.d/issue-493-check-hooks.md`
- Modify (dogfood): `.gitattributes` (nieuw), `.lighthouse/manifest.toml` en wat `init --upgrade` verder wijzigt

**Interfaces:**
- Consumes: het commando uit Task 4.

- [ ] **Step 1: Voeg een rij toe aan `ci/windows-measure.sh`** na rij 11: `(cd "$REPO" && PATH="$BIN_DIR:$PATH" "$BIN" check-hooks) >"$OUT" 2>"$ERR"; RC=$?`, `row "12. \`lighthouse check-hooks\`" "rc 0" "$RC" "$(mark ok 0)" "$ERR"`. Pas de kopregel-uitleg in de header aan indien nodig.
- [ ] **Step 2: Run lokaal** `ci/windows-measure.sh "$(go env GOPATH)/bin/lighthouse" "$(mktemp -d)"` na `go install ./cmd/lighthouse`. Verwacht: elke rij `pass`, ook rij 12.
- [ ] **Step 3: Gids.** `docs/install.md`: sectie over `lighthouse check-hooks` (wanneer: na `init`, na een nieuwe machine, bij twijfel; per `FAIL` de betekenis en herstelstap; Git Bash alleen op Windows). De zin "Windows has a binary, but the hooks need WSL" en de passage in `docs/site/agents/install.md` (r. 156-158) gaan eruit zodra de Windows-run groen is; is hij niet groen, dan blijven ze staan en verwijzen ze naar de check. Alleen relatieve links naar bestaande doelen.
- [ ] **Step 4: Fragment** `changelog.d/issue-493-check-hooks.md` met frontmatter `type: feature` (zoals de andere fragmenten) en één alinea (Engels of Nederlands volgens de bestaande fragmenten): `check-hooks`, `.gitattributes`, CRLF-reparatie.
- [ ] **Step 5: Dogfood.** `go run ./cmd/lighthouse init --upgrade` (zonder `--force`), dan nogmaals: `No changes.`; bekijk of `check-hooks` daar groen is en noteer het in de PR als het dat niet is (dev-build van `run.sh` schrijft in `.lighthouse/bin`, een bekende uitzondering).
- [ ] **Step 6: Volledige verificatie.** `go test ./...`, `go test -tags e2e ./e2e/`, `lighthouse imports check`. Verwacht: alles groen.
- [ ] **Step 7: Commit** `docs(install): check-hooks, windows-meting en changelog (#493)` en daarna apart `chore(init): dogfood-kopie na check-hooks (#493)`.
- [ ] **Step 8: Bewijs op Windows.** Na de push: `windows-measure.yml` via `workflow_dispatch` (de push van workflowbestanden doet de pusher; dit plan wijzigt er geen). Noteer in de PR rij 12.
