# Git-paden naar de native vorm — implementatieplan

> **Voor agentische uitvoerders:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkbox-syntax (`- [ ]`).

**Doel:** Elk pad dat lighthouse uit git-uitvoer leest wordt via één helper naar de native vorm gezet, zodat vergelijken en tonen op Windows klopt.

**Architectuur:** Nieuw klein pakket `internal/gitpath` met `Native(out string) string` en `NativeLines`. Elke productieplek die `rev-parse --show-toplevel`, `--git-dir`, `--git-common-dir` of `worktree list --porcelain` leest roept de helper aan op de ruwe uitvoer. Een bewakingstest zorgt dat een nieuwe plek de helper niet kan vergeten.

**Tech Stack:** Go, `path/filepath`, `go test -race -shuffle=on ./...`.

**Spec:** geen spec; de acceptatiecriteria van issue #565 zijn het ontwerp. Besluit voor de tweede AC: **meldingen tonen het native pad** (`filepath.Clean`-vorm), in elke hook gelijk.

## Globale randvoorwaarden

- Linux en macOS: geen enkele bestaande testverwachting verandert (`filepath.FromSlash` is daar een no-op).
- Een leeg pad blijft leeg (`filepath.Clean("")` geeft `.`; de helper mag dat niet doorgeven).
- Een relatief antwoord (`.git`) blijft relatief; alleen scheidingstekens en spaties/regeleinden worden genormaliseerd.
- Code en commentaar in het Engels als `code_language` dat vraagt; artefacten (changelog, commits) in het Nederlands.
- Voor elke commit: `go test -race -shuffle=on ./...` én `lighthouse imports check`. Nooit `--no-verify`.
- Uitzondering, bewust: `internal/levellog/levellog.go:53` hasht de **ruwe** `--git-common-dir`-uitvoer en blijft dat doen, anders raken bestaande logboeken op Windows zoek. Leg dit vast in een commentaar naast de hash.

## Review Focus

- Leeg antwoord van git → helper geeft `""`, geen `.`.
- Uitvoer met `\r\n` of spaties rond het pad → getrimd.
- Relatief antwoord (`.git`) → blijft `.git`, wordt niet absoluut gemaakt.
- `worktree list --porcelain` met een pad met spaties → regel blijft heel, alleen het pad na `worktree ` wordt omgezet.
- De melding van `main-commit-gate` toont hetzelfde pad als waarmee het intern vergeleken wordt.

---

### Taak 1: Helper `internal/gitpath`

**Bestanden:**
- Maak: `internal/gitpath/gitpath.go`
- Test: `internal/gitpath/gitpath_test.go`

**Interfaces:**
- Produceert: `func Native(out string) string` en `func NativeLines(out string) []string`. Latere taken gebruiken beide.

- [ ] **Stap 1: Schrijf de falende test** `TestNative` (tabel): `"/a/b\n"`→`"/a/b"`, `"/a/b\r\n"`→`"/a/b"`, `"  /a/b "`→`"/a/b"`, `""`→`""`, `"\n"`→`""`, `".git\n"`→`".git"`, `"/a//b/../c"`→`filepath.Clean` daarvan. Plus `TestNativeWindowsForm` met `runtime.GOOS == "windows"` guard: `"C:/Users/x\n"`→`` `C:\Users\x` ``; op andere OS's `t.Skip`.
  `TestNativeLines`: `"/a\n\n/b\r\n"` → `["/a","/b"]`.
- [ ] **Stap 2: Draai** `go test ./internal/gitpath/` — verwacht FAIL (pakket bestaat niet).
- [ ] **Stap 3: Implementeer** `Native`: `s := strings.TrimSpace(out); if s == "" {return ""}; return filepath.Clean(filepath.FromSlash(s))`. `NativeLines`: splits op `\n`, `Native` per regel, sla lege over.
- [ ] **Stap 4: Draai** de test — verwacht PASS.
- [ ] **Stap 5: Commit** `feat(gitpath): helper zet git-paden om naar de native vorm (#565)`.

### Taak 2: De drie voorbeeldplekken en de hook-melding

**Bestanden:**
- Wijzig: `internal/commitgate/pre.go:156`, `:189`, `internal/commitgate/pr.go:84`
- Wijzig: `internal/hooks/mdlinkcheck.go:79`, `internal/hooks/maincommitgate.go:110`, `:192`, `internal/hooks/workflowpushgate.go:119`, `internal/hooks/worktreecheck.go:67`, `:74`
- Wijzig: `internal/initcmd/initcmd.go:445` (de scout noemt `:421` uit het issue; zoek `repoRoot`)
- Test: `internal/hooks/maincommitgate_test.go`, `internal/commitgate/roots_test.go`

**Interfaces:**
- Verbruikt: `gitpath.Native`.

- [ ] **Stap 1: Schrijf de falende test** in `internal/commitgate/roots_test.go`: `stubGit` (`roots_test.go:81-98`) is een sh-script dat faalt of de echte git draait en kan dus geen vaste uitvoer leveren: gebruik een fake-git zoals het `hooks`-pakket (een script op het PATH dat `"/repo/root//\n"` echoot) zodat `Clean` op Linux zichtbaar is; assert `filepath.Clean("/repo/root")`. Zelfde idee in `maincommitgate_test.go`: assert dat de blokmelding `filepath.Clean(root)` bevat (op Linux gelijk aan nu; het pint de keuze).
- [ ] **Stap 2: Draai** de nieuwe tests — verwacht FAIL waar een plek nog rauw leest, anders alvast PASS en dan de pin als regressiebewaking laten staan.
- [ ] **Stap 3: Vervang** op elke genoemde regel `strings.TrimSpace(out)` (of rauw gebruik) door `gitpath.Native(out)`. Houd de lege-uitvoer-takken (`if root == ""`) intact; ze werken nu op de genormaliseerde waarde. Uitzondering, stdout en stderr delen één buffer: in `hooks/mdlinkcheck.go` (`:83`, `root := strings.TrimRight(out.String(), "\n")`) en `hooks/worktreecheck.go:67/74` (`gitCombined`) pas `Native` alleen toe ná de foutentak, op het succespad; de foutmelding (`cannotCheck`, `warnUncheckable`, de `not a git repository`-match) houdt de ruwe tekst. Zelfde voor `codelang/hook.go` in taak 3.
- [ ] **Stap 4: Draai** `go test -race -shuffle=on ./internal/commitgate/ ./internal/hooks/ ./internal/initcmd/` — verwacht PASS.
- [ ] **Stap 5: Commit** `fix(hooks): git-paden native in commitgate, hooks en init (#565)`.

### Taak 3: Overige `rev-parse`-leesplekken

**Bestanden (wijzig):**
- `--show-toplevel`: `internal/codelang/hook.go:189`, `internal/config/config.go:348`, `internal/cli/ask.go:202`, `internal/cli/pins.go:76`, `internal/nodeserve/resolve.go:41`, `internal/postartefact/postartefact.go:254`, `internal/checkpointscan/push.go:128`
- `--git-common-dir` / `--git-dir`: `internal/worktreeroot/worktreeroot.go:41` (`MainCheckout`), `internal/bashwrites/roots.go:106`, `internal/prbody/scope.go:90`, `internal/agentaudit/env.go:70`, `internal/pins/localplugin.go:100`, `internal/scheduled/start.go:90` (`--absolute-git-dir`) en `:94`, `internal/roborev/state.go:52` (`--absolute-git-dir`), `internal/scheduled/remote.go:103`, `internal/checkpointscan/ledger.go:23`, `internal/workplace/local/prepare.go:166`, `internal/workplace/kennforge/adapter.go:459`, `:468`, `:572`
- Test: `internal/worktreeroot/worktreeroot_test.go`, `internal/workplace/kennforge/adapter_test.go`

**Interfaces:**
- Verbruikt: `gitpath.Native`. `kennforge/adapter.go:468` en `telemetry_collect.go:255` doen al `filepath.Clean(strings.TrimSpace(..))`: vervang door de helper zodat er één vorm is.

- [ ] **Stap 1: Schrijf de falende test** `TestMainCheckoutTrailingNewline`-achtig in `worktreeroot_test.go` met een echte tijdelijke git-repo (`git init`): `MainCheckout(sub)` geeft het native pad van de repo, zonder newline. Lees hoe `adapter_test.go` zijn git-stub opzet en pin voor `:572` (clone-check; `:459` is de linked-worktree-check van git-dir tegen common-dir) dat het pad `filepath.Clean`-vorm heeft.
- [ ] **Stap 2: Draai** — verwacht PASS of FAIL naargelang; noteer welke.
- [ ] **Stap 3: Pas elke genoemde plek aan.** Waar stdout en stderr in één variabele staan (`hooks/mdlinkcheck.go:83`, `codelang/hook.go:193`, `hooks/worktreecheck.go:67/74`), pas `Native` alleen toe op het succespad, ná de foutentak: nooit op een git-foutmelding. `scheduled/remote.go:103`: houd de bestaande `TrimSuffix`-plus-`ContainsAny("\n\r")`-controle op de ruwe waarde en pas `Native` daarna toe (anders verdwijnt de afwijzing van een afsluitende `\r`). In `scheduled/start.go:90` hasht de digest nu gitDir en commonDir beide in de native vorm. Bij een plek die `--path-format=absolute` en `filepath.Dir(common)` combineert (`MainCheckout`): eerst `gitpath.Native`, dan `filepath.Dir`. `internal/levellog/levellog.go:53` **niet** aanpassen (zie randvoorwaarden); voeg daar alleen het uitleg-commentaar toe.
- [ ] **Stap 4: Draai** `go test -race -shuffle=on ./...` — verwacht PASS.
- [ ] **Stap 5: Commit** `fix(core): overige git-paden native lezen (#565)`.

### Taak 4: `worktree list --porcelain`

**Bestanden:**
- Wijzig: `internal/worktreeaudit/rows.go:246` (`ParseWorktrees`; hier gebeurt de omzetting, `collect.go:172` leest alleen de ruwe porcelain en geeft die aan `ParseWorktrees`: daar **geen** `Native` op de hele uitvoer, want dat maakt `refs/heads/x` tot `refs\heads\x` op Windows), `internal/workplace/local/worktree.go:36`, `internal/cli/triage_gather.go:221`, `internal/cli/telemetry_collect.go:248`/`:255`, `internal/telemetry/collect/plan.go:63`
- Test: `internal/worktreeaudit/twins_test.go` en een nieuwe tabeltest naast `ParseWorktrees`

**Interfaces:**
- Verbruikt: `gitpath.Native`. Zet alleen het deel na `worktree ` om, niet de hele regel (paden met spaties blijven heel).

- [ ] **Stap 1: Lees eerst** `twins_test.go` en bepaal of de Python-twin paden vergelijkt. Zo ja: houd de twin-uitvoer op Linux gelijk (de helper verandert daar niets) en noteer dat in het commit-bericht.
- [ ] **Stap 2: Schrijf de falende test** `TestParseWorktreesNative`: invoer `worktree /a b/c\nHEAD x\nbranch refs/heads/y\n\n` geeft pad `"/a b/c"` (Linux-gelijk), en op `windows` met `C:/a b/c` → `` `C:\a b\c` `` (guard met `runtime.GOOS`).
- [ ] **Stap 3: Pas** de leesplekken aan (de zes bestanden hierboven, `collect.go` uitgezonderd; `telemetry_collect.go:248/255` hoort bij deze taak, taak 3 raakt het niet aan); waar ze al `filepath.Clean(strings.TrimSpace(..))` doen, vervang door de helper.
- [ ] **Stap 4: Draai** `go test -race -shuffle=on ./...` — verwacht PASS.
- [ ] **Stap 5: Commit** `fix(worktreeaudit): worktree-paden native (#565)`.

### Taak 5: Bewakingstest, documentatie, changelog

**Bestanden:**
- Maak: `internal/gitpath/guard_test.go`
- Maak: `changelog.d/issue-565-git-paden-native.md` (volg het formaat van de bestanden in `changelog.d/`)
- Wijzig: de ontwikkelaarsdocumentatie waar paden/hooks beschreven staan (zoek met `grep -rn "show-toplevel" docs/`); één alinea: "git-paden gaan via `internal/gitpath.Native`; meldingen tonen native paden".

- [ ] **Stap 1: Schrijf** `TestEveryGitPathReadUsesGitpath`: loop over `internal/**/*.go` zonder `_test.go`, `testdata` en `internal/gitpath`; voor elk bestand met een van de strings `"--show-toplevel"`, `"--git-common-dir"`, `"--git-dir"`, `"--absolute-git-dir"` of `"worktree", "list"` moet het bestand `internal/gitpath` importeren, of op een allowlist staan met reden (`levellog.go`: ruwe hash; `worktreeaudit/collect.go`: leest alleen de ruwe porcelain, de omzetting staat in `rows.go` (dat de helper wel importeert); `gitinvocations/find.go`, `pusher/floor.go`, `scheduled/remote.go:151`: bouwen alleen argumenten; `direction.go`, `checkinstructions.go`, `checkhooks.go`: noemen het alleen in een melding).
- [ ] **Stap 2: Draai** — verwacht PASS als taken 2–4 volledig waren (met `collect.go` op de allowlist; geef `gitpath` nooit aan `collect.go` om de test groen te krijgen). De test controleert één import per bestand: een tweede ruwe leesplek in een bestand dat al importeert (`hooks/maincommitgate.go:110` naast `:192`) vang je met de pins uit taak 2, niet met deze test; een FAIL wijst een gemiste plek aan: repareer die.
- [ ] **Stap 3: Mutatiecheck:** haal tijdelijk de import uit één bestand, zie de test rood worden, zet terug.
- [ ] **Stap 4: Schrijf** changelog (Nederlands) en documentatie.
- [ ] **Stap 5: Draai** `go test -race -shuffle=on ./...` en `lighthouse imports check` — verwacht PASS.
- [ ] **Stap 6: Commit** `test(gitpath): bewaking dat elke git-pad-leesplek de helper gebruikt (#565)`.

## Verificatie op Windows

`windows-measure` (job `tests`) draait alleen op push naar `main` of met de hand, nooit op een PR. Bewijs voor de derde AC: na de merge de workflow met de hand starten en `TestPrOnPRRunsOnBranchDiff`, `TestLinkCheckTrace`, `TestFirstInitRunsAudit` controleren. Dit hoort in het PR-bewijs als "na merge te meten", niet als lokaal bewijs.

## Zelfreview

- Spec-dekking: AC1 → taken 1–4 plus bewaking in 5; AC2 → besluit bovenaan, pin in taak 2; AC3 → verificatieparagraaf; AC4 → globale randvoorwaarde.
- Naamconsistentie: `gitpath.Native` en `gitpath.NativeLines` overal gelijk.
- Open punt voor de uitvoerder: of `NativeLines` in taak 4 nodig is of dat `Native` per veld volstaat; schrap `NativeLines` als het ongebruikt blijft.
