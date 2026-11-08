# Werkplek-adapter zonder kenn-forge — implementatieplan

> **Voor agentische uitvoerders:** VERPLICHTE SUB-SKILL: superpowers:subagent-driven-development
> (aanbevolen) of superpowers:executing-plans, taak voor taak. Stappen gebruiken checkboxen
> (`- [ ]`).

**Doel:** `lighthouse tick` start, meet, stopt en ruimt runs op een gewone Linux-server zonder
kenn-forge, met een tweede `workplace.Workplace`: de lokale adapter (git-worktree plus headless
`claude -p`).

**Architectuur:** eerst verhuizen de gedeelde stukken van `kennforge` naar `workplace`
(`FollowRemote`, `HostTranscriptDir`, `WriteFileAtomic`), en komt er een gedeelde contract-suite
`workplacetest` die kenn-forge meteen haalt. Daarna groeit `internal/workplace/local` methode voor
methode, met een lokale fixture die de suite-gevallen aanzet zodra hun methode er is. Processen van
een run vindt de adapter op het merk `LIGHTHOUSE_CONTROLLER_RUN=<sid>` in `/proc/<pid>/environ`,
achter het interface `Procs`. `host.toml` `[dispatch] workplace` kiest de adapter in `tickEnv.workplace`.

**Tech stack:** Go 1.26 (stdlib, `BurntSushi/toml`), git, POSIX `sh`, util-linux `setsid -f`,
systemd user units.

**Spec:** [2026-11-07-werkplek-zonder-kenn-forge-design.md](../specs/2026-11-07-werkplek-zonder-kenn-forge-design.md).
Issue: #253.

## Globale randvoorwaarden

- Linux met `/proc`, `git`, `setsid` (util-linux, met `-f`) en `claude` op het `PATH` van de tick.
- De `Workplace`-interface en de sentinels (`ErrNotAccepted`, `ErrUncertain`, `ErrDirty`, `ErrGone`)
  veranderen niet. Decide, apply, `Launch`, `AdoptSessions`, `Cleanup` en `Sweep` veranderen niet.
- Geen gedrag van `kennforge.Adapter` verandert, behalve de weigering van een `local-`-ID (taak 2).
- De productiecode bouwt op linux, windows en darwin (`CGO_ENABLED=0 GOOS=… go build ./...`, CI
  `ci.yml` r. 139-140). Alles wat `/proc` of `syscall.Kill` gebruikt, staat in een `_linux.go`-bestand
  met een `_other.go`-tegenhanger (`//go:build !linux`). Elk testbestand in
  `internal/workplace/local` begint met `//go:build linux`: de windows-meting draait `go test ./...`.
- Elk bestand dat de adapter schrijft: atomair (tempbestand en rename), bestanden `0600`, mappen `0700`.
- Git-timeout van de adapters: 60 s (`FollowRemote` en de eigen git-opdrachten); `Dirty` en
  `Unpushed` houden hun 30 s.
- Code, namen, commentaar en testnamen in het Engels; docs onder `docs/` en dit plan in het
  Nederlands; `docs/install.md` is Engels (zoals nu); commitberichten in het Nederlands als
  Conventional Commits.
- Kill-lus: SIGTERM, dan tot 5 keer `Sleep(1 s)` plus `Marked`; nog niet leeg: SIGKILL, nog één
  `Sleep(1 s)` en één `Marked`. Een gewone stop kost zo 1 `Sleep`, een escalatie 6.
- Spawn wacht tot 10 s op het pid-bestand: kijken, dan tot 50 keer `Sleep(200 ms)` plus kijken.
- Elke commit gaat door de test-gate (`[test_gate] command`); rood fix je in een nieuwe commit.
- Elke taak eindigt groen op `go test -race ./internal/...` en `golangci-lint run ./...`.

## Review Focus

1. **Een repo zonder `.claude/worktrees/` in `.gitignore`.** Een worktree onder de hoofdcheckout
   maakt `git status` daar vuil (`?? .claude/`). Verwacht: de checkout van de gebruiker blijft
   schoon. *Keuze in dit plan:* `Prepare` zet `/.claude/worktrees/` in
   `<git-common-dir>/info/exclude` als hij er nog niet staat. Test in taak 6.
2. **Een prompt met shell-tekens** (`'`, `"`, `$(…)`, `;`, een nieuwe regel). Verwacht: claude krijgt
   de prompt byte voor byte. Test in taak 11 (echte shell, nep-claude die zijn argv opschrijft).
3. **Een repo-pad met een spatie.** Verwacht: cwd van claude is de worktree, log en pid-bestand
   landen goed. Test in taak 11 (de repo staat in `t.TempDir()/repo met spatie`).
4. **Een tick die zelf het merk draagt** (gestart vanuit een run-sessie). Verwacht: `Kill` stuurt
   nooit een signaal naar de tick zelf. `Marked` slaat de eigen pid over. Test in taak 5.
5. **Een onschrijfbare statemap.** Verwacht: `Spawn` is `ErrNotAccepted` en start niets. Test in
   taak 7.

---

## Bestandsstructuur

| Bestand | Verantwoordelijkheid |
|---|---|
| `internal/workplace/remote.go` (nieuw) | `FollowRemote` en `checkoutRemote`, verhuisd uit `kennforge` |
| `internal/workplace/atomic.go` (nieuw) | `WriteFileAtomic`; `WriteCarrier` gebruikt hem |
| `internal/workplace/transcript.go` | + `HostTranscriptDir`, verhuisd uit `kennforge` |
| `internal/workplace/workplace.go` | + `LocalIDPrefix` |
| `internal/workplace/workplacetest/suite.go` (nieuw) | `Fixture`, `Case`, `Cases`, `Run` |
| `internal/workplace/kennforge/contract_test.go` (nieuw) | stateful nep-forge als `Fixture` |
| `internal/workplace/kennforge/{adapter,stop,transcript}.go` | gebruiken de verhuisde functies; weigeren `local-` |
| `internal/hostconfig/hostconfig.go` | `[dispatch] workplace` |
| `internal/workplace/local/procs.go` | interface `Procs`, `markVar` |
| `internal/workplace/local/procs_linux.go`, `procs_other.go` | `/proc`-implementatie; stub elders |
| `internal/workplace/local/store.go` | records, pid-bestanden, logs op schijf |
| `internal/workplace/local/worktree.go` | `git worktree prune/list`, parsen, `resolve` |
| `internal/workplace/local/adapter.go` | `Adapter`, ID-check, `Lookup`, helpers |
| `internal/workplace/local/prepare.go` | `Prepare` |
| `internal/workplace/local/spawn.go` | `Spawn`, `Sessions`, `launchScript` |
| `internal/workplace/local/stop.go` | `Stop`, `Kill` |
| `internal/workplace/local/remove.go` | `Remove`, `Discard`, `Transcript` |
| `internal/workplace/local/*_test.go` | eigen tests, fixture, integratie |
| `internal/cli/tick.go` | `tickEnv.workplace` kiest de adapter |
| `deploy/systemd/lighthouse-tick.{service,timer}` (nieuw) | tick als oneshot met `KillMode=process` |
| `docs/workplace.md`, `docs/tick.md`, `docs/install.md`, `changelog.d/issue-253-werkplek-zonder-kenn-forge.md` | documentatie |

---

### Taak 1: gedeelde stukken naar `workplace`

Klasse: refactor (`pure-logic`/`io`), regressie-oppervlak: Prepare, transcript en carrier van
kenn-forge. De bestaande tests bewaken het gedrag: `TestPrepare*` (de echte-git-gevallen),
`TestTranscriptHost`, de carrier-tests.

**Files:**
- Create: `internal/workplace/remote.go`, `internal/workplace/remote_test.go`,
  `internal/workplace/atomic.go`, `internal/workplace/atomic_test.go`
- Modify: `internal/workplace/transcript.go`, `internal/workplace/carrier.go` (`WriteCarrier`),
  `internal/workplace/kennforge/adapter.go` (weg: `git`, `gitOK`, `followRemote`, `checkoutRemote`,
  `hostTranscriptDir`), `internal/workplace/kennforge/transcript.go`

**Interfaces:**
- Produces:
  - `func FollowRemote(ctx context.Context, run Runner, timeout time.Duration, ws Workspace, created bool) error`
    — het gedrag van `kennforge.(*Adapter).followRemote`, ongewijzigd, met `timeout` op elke git-opdracht.
  - `func HostTranscriptDir(home, workspaceID, worktree string) (string, error)` — `home == ""`
    is `os.UserHomeDir()`; verder het gedrag van `hostTranscriptDir`.
  - `func WriteFileAtomic(path string, data []byte) error` — map `0700` (ook een bestaande),
    tempbestand `.<base>-*.tmp` in die map, `0600`, rename.

- [ ] **Stap 1: Schrijf de falende tests**

```go
// remote_test.go
func TestFollowRemotePassesTimeout(t *testing.T) {
	var got []time.Duration
	run := func(_ context.Context, d time.Duration, name string, args ...string) (string, int, error) {
		got = append(got, d)
		return "", 0, nil // ls-remote finds no branch: nothing more to do
	}
	ws := Workspace{ID: "w", Path: "/w", Branch: "issue-1-x"}
	if err := FollowRemote(context.Background(), run, 42*time.Second, ws, true); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != 42*time.Second {
		t.Errorf("timeouts = %v, want [42s]", got)
	}
}

// atomic_test.go
func TestWriteFileAtomicModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "d")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "f.json")
	if err := WriteFileAtomic(p, []byte("x")); err != nil {
		t.Fatal(err)
	}
	// assert: content "x", file mode 0600, dir mode 0700, no "*.tmp" left in dir
}
```

- [ ] **Stap 2: Draai ze en zie ze falen** — `go test ./internal/workplace/ -run 'FollowRemote|WriteFileAtomic'`:
  compileerfout, `FollowRemote` en `WriteFileAtomic` bestaan niet.

- [ ] **Stap 3: Verhuis en implementeer.** `followRemote`/`checkoutRemote` naar `remote.go` met
  een eigen `remoteGit(ctx, run, timeout, dir, args...)`-helper (vorm van de `kennforge`-helpers;
  niet `git`: `git_test.go:13` heeft al een testhelper `git` in package `workplace`);
  `kennforge.Prepare` roept `workplace.FollowRemote(ctx, a.Run, gitTimeout, ws, created)`.
  `hostTranscriptDir` naar `transcript.go`; `kennforge.Transcript` roept
  `workplace.HostTranscriptDir(a.Home, workspaceID, w.WorktreeDir)`. `WriteCarrier` schrijft via
  `WriteFileAtomic`.

- [ ] **Stap 4: Draai** `go test -race ./internal/workplace/...` — PASS, ook alle `kennforge`-tests.

- [ ] **Stap 5: Commit** — `refactor(workplace): followRemote, hostTranscriptDir en atomair schrijven naar workplace (#253)`

---

### Taak 2: kenn-forge weigert een `local-`-ID

Klasse: `pure-logic`; regressie: een lokaal ID dat kenn-forge als `ErrGone` zou zien.

**Files:**
- Modify: `internal/workplace/workplace.go`, `internal/workplace/kennforge/adapter.go`,
  `internal/workplace/kennforge/stop.go`, `internal/workplace/kennforge/transcript.go`
- Test: `internal/workplace/kennforge/adapter_test.go`

**Interfaces:**
- Produces: `const LocalIDPrefix = "local-"` in `workplace` (gebruikt door taak 6).
- In `kennforge`: `func notForge(id string) error` — `nil`, of
  `fmt.Errorf("workspace %s is a local workspace, not a kenn-forge one", id)`. Eerste regel van
  `Sessions`, `Stop`, `Kill`, `Remove`, `Lookup`, `Discard`, `Transcript`. In `Spawn` (op `ws.ID`)
  gewrapt met `workplace.ErrNotAccepted` *(keuze in dit plan: er is niets gestart)*.

- [ ] **Stap 1: Schrijf de falende test**

```go
func TestLocalIDIsNoForgeWorkspace(t *testing.T) {
	const id = "local-issue-1-x-a1b2c3"
	calls := map[string]func(*Adapter) error{
		"Sessions":   func(a *Adapter) error { _, err := a.Sessions(context.Background(), id); return err },
		"Stop":       func(a *Adapter) error { return a.Stop(context.Background(), id, []string{testSessionID}) },
		"Kill":       func(a *Adapter) error { return a.Kill(context.Background(), id, []string{testSessionID}) },
		"Remove":     func(a *Adapter) error { return a.Remove(context.Background(), id) },
		"Lookup":     func(a *Adapter) error { _, err := a.Lookup(context.Background(), id); return err },
		"Discard":    func(a *Adapter) error { return a.Discard(context.Background(), id) },
		"Transcript": func(a *Adapter) error { _, err := a.Transcript(context.Background(), id, testSessionID); return err },
	}
	for name, call := range calls {
		f, a := newFakeForge(t)
		ran := 0
		a.Run = func(context.Context, time.Duration, string, ...string) (string, int, error) { ran++; return "", 0, nil }
		err := call(a)
		if err == nil || errors.Is(err, workplace.ErrGone) {
			t.Errorf("%s: err = %v, want an error that is not ErrGone", name, err)
		}
		if f.total() != 0 || ran != 0 {
			t.Errorf("%s: requests = %d, commands = %d, want none", name, f.total(), ran)
		}
	}
	// Spawn: errors.Is(err, workplace.ErrNotAccepted), f.total() == 0, no carrier for id.
}
```

- [ ] **Stap 2: Draai** `go test ./internal/workplace/kennforge/ -run TestLocalIDIsNoForgeWorkspace` — FAIL (requests > 0).
- [ ] **Stap 3: Implementeer** `LocalIDPrefix` en `notForge`, en roep hem aan zoals hierboven.
- [ ] **Stap 4: Draai** `go test -race ./internal/workplace/...` — PASS.
- [ ] **Stap 5: Commit** — `feat(kennforge): weiger een workspace-ID van de lokale adapter (#253)`

---

### Taak 3: `[dispatch] workplace` in `host.toml`

Klasse: `config`; één test per waarde, de standaard en een ontbrekende sleutel.

**Files:**
- Modify: `internal/hostconfig/hostconfig.go`
- Test: `internal/hostconfig/hostconfig_test.go`

**Interfaces:**
- Produces:
  - `const WorkplaceKennForge = "kenn-forge"`, `const WorkplaceLocal = "local"`
  - `Dispatch.Workplace string`; `DefaultDispatch()` zet `WorkplaceKennForge`.
  - Raw: `Workplace *string \`toml:"workplace"\`` onder `Dispatch` (zo kent de check op onbekende
    sleutels hem).
  - Fout bij een andere waarde: `dispatch.workplace: %q is neither "kenn-forge" nor "local"`.

- [ ] **Stap 1: Schrijf de falende tests.** In `TestLoadDispatchDefaults` een regel:
  `Workplace == "kenn-forge"`. Nieuw:

```go
func TestLoadDispatchWorkplace(t *testing.T) {
	cases := []struct{ name, body, want, errHas string }{
		{"missing", "[dispatch]\nslots = 2\n", "kenn-forge", ""},
		{"existing forge table", "[dispatch.forge]\nurl = \"http://127.0.0.1:9\"\n", "kenn-forge", ""},
		{"kenn-forge", "[dispatch]\nworkplace = \"kenn-forge\"\n", "kenn-forge", ""},
		{"local", "[dispatch]\nworkplace = \"local\"\n", "local", ""},
		{"other", "[dispatch]\nworkplace = \"podman\"\n", "", "dispatch.workplace"},
	}
	// per case: write body to a temp host.toml, LoadDispatch; want d.Workplace or an error
	// containing errHas. "existing forge table" also asserts d.Forge.URL == "http://127.0.0.1:9".
}
```

- [ ] **Stap 2: Draai** `go test ./internal/hostconfig/ -run Workplace` — FAIL ("local": unknown key).
- [ ] **Stap 3: Implementeer** veld, standaard, raw-sleutel en de check.
- [ ] **Stap 4: Draai** `go test -race ./internal/hostconfig/` — PASS.
- [ ] **Stap 5: Commit** — `feat(hostconfig): [dispatch] workplace kiest de werkplek-adapter (#253)`

---

### Taak 4: gedeelde contract-suite, eerst tegen kenn-forge

Klasse: `test-only`; regressie: elk contractgeval faalt als de adapter de sentinel of het effect
mist.

**Files:**
- Create: `internal/workplace/workplacetest/suite.go`, `internal/workplace/kennforge/contract_test.go`
- Modify: `internal/workplace/kennforge/adapter_test.go`, `internal/workplace/kennforge/stop_test.go`
  (dubbele tests weg, zie stap 5)

**Interfaces:**
- Produces (`package workplacetest`, alleen door tests geïmporteerd):

```go
// Fixture puts one adapter into a state and hands it out. Each method arranges the world
// before the call under test; the suite never looks inside the adapter.
type Fixture interface {
	Workplace() workplace.Workplace
	Workspace(t *testing.T, branch string) workplace.Workspace // prepared, clean
	Spec(t *testing.T) workplace.LaunchSpec                    // a valid start; the suite sets SessionID
	MakeDirty(t *testing.T, ws workplace.Workspace)
	Forget(t *testing.T, ws workplace.Workspace) // the workplace no longer knows it
	Live(t *testing.T, sid string)               // a process of sid runs: its session is working; a spawned sid without Live is done
	Survivor(t *testing.T, sid string)           // ... and outlives every signal
	Unmeasurable(t *testing.T)                   // processes cannot be measured
	SpawnTimesOut(t *testing.T)                  // the next launch has an unknown outcome
	Launches() int                               // launches sent so far
	ProcCalls() int                              // process measurements and signals so far
	Signals() int                                // signals sent so far
}

type Case struct {
	Name string
	Run  func(t *testing.T, f Fixture)
}

var Cases []Case // the 15 cases below, in this order

// Run runs every case as a subtest, each with a fresh fixture.
func Run(t *testing.T, newFixture func(t *testing.T) Fixture)
```

De suite maakt sessie-ID's met `runs.NewSessionID()`. De gevallen (naam — wat het assert):

| # | `Name` | Assert |
|---|---|---|
| 1 | `PrepareTwiceSameWorkspace` | twee keer `Prepare("issue-1-x")`: zelfde niet-lege ID, `filepath.IsAbs(Path)` |
| 2 | `LookupKnownAndForgotten` | `Lookup` geeft `Path == ws.Path`, `Branch == "issue-2-x"`; na `Forget`: `ErrGone` |
| 3 | `SessionsOfForgottenIsGone` | na `Forget`: `Sessions` is `ErrGone` |
| 4 | `SpawnInvalidNotAccepted` | subtests `SessionID = "not-a-uuid"` en `Prompt = "-x"`: `ErrNotAccepted`, `Sessions` leeg, `Launches() == 0` |
| 5 | `SpawnAcceptedShowsSession` | `s.Key != ""`, `s.SessionID == sid`, `s.WorkspaceID == ws.ID`; `Sessions` bevat `{Key: s.Key, SessionID: sid}`; `Launches() == 1` |
| 6 | `SpawnUncertainNeverTwice` | na `SpawnTimesOut`: `ErrUncertain`, `Launches() == 1` |
| 7 | `EmptySessionIDRejected` | `Kill` en `Stop` met `[sid, ""]`: fout; `ProcCalls() == 0`, `Signals() == 0` |
| 8 | `KillWithoutProcess` | `Kill([sid])` zonder `Live`: `nil`, `Signals() == 0` |
| 9 | `KillSurvivorIsError` | `Live`, `Survivor`: `Kill` geeft een fout |
| 10 | `KillUnmeasurableIsError` | `Live`, `Unmeasurable`: `Kill` geeft een fout, `Signals() == 0` |
| 11 | `KillLeavesOtherSession` | spawn `a` en `b`, `Live` beide, `Kill([a])`: `nil`; `Sessions`: `b` heeft `Stand() == Working`, `a` heeft `Done` |
| 12 | `RemoveDirtyKeeps` | `MakeDirty`, `Remove`: `ErrDirty`; `Lookup` geeft daarna `Path == ws.Path` |
| 13 | `RemoveForgottenAndTwice` | `Remove` na `Forget`: `nil`; op een tweede workspace `Remove` twee keer `nil`, daarna `Lookup` `ErrGone` |
| 14 | `DiscardIdempotent` | `Discard` twee keer op een schone workspace: `nil` |
| 15 | `EndedSessionIsDone` | spawn `a` en `b`, alleen `Live(b)`: `a` `Done`, `b` niet `Done` |

**De kenn-forge-fixture** (`forgeFixture` in `contract_test.go`, package `kennforge`): een
**stateful** `httptest`-forge, los van de gescripte `fakeForge`:
- `POST /api/v1/repo/github/CorneeldH/lighthouse/workspaces` kloont een bare `origin` (met `main`,
  zoals `newGitWorld`) naar een nieuwe map, geeft `ws<n>`, status meteen `ready`.
- `GET /api/v1/workspaces` en `GET /api/v1/workspaces/<id>` geven de bekende workspaces
  (`git_head_ref` de kale branch, `repo` als in `listWith`); een vergeten of verwijderde is 404
  (en ontbreekt in de lijst).
- `POST …/runtime/agent-handoffs` telt (`Launches`), leest `session_id` uit de carrier
  (`workplace.CarrierPath(StateDir, id)`), voegt sessie `k<n>` toe en antwoordt
  `{"session":{"key":"k<n>"}}`. Bij `SpawnTimesOut`
  blokkeert hij tot de request-context eindigt; de fixture zet `a.spawnTimeout = 100 * time.Millisecond`.
- `GET …/agent-sessions`: `state` is `"working"` als de sid live is, anders `"done"`.
- `DELETE`: 409 als `MakeDirty` liep, 404 als weg, anders 204 en weg.
- `Run`: `git` via `workplace.ExecRunner()`; `pgrep -f <sid>` exit 0 als live, 1 anders, een fout
  bij `Unmeasurable`; `pkill -f <sid>` telt een signaal en maakt de sid niet-live, behalve een
  survivor; `systemctl --user stop` exit 0, `is-active` geeft `inactive`. `ProcCalls` telt elke
  niet-git-opdracht.
- `Adapter`: `Profile: "infra"`, `Targets: {"infra": "paia-infra"}`, `Sleep` no-op.
  `Spec`: `Profile "infra"`, `Mode "start"`, `Prompt "do the thing"`, `InstructionsFile "/abs/instr.md"`.

- [ ] **Stap 1: Schrijf `suite.go` en `contract_test.go`** met
  `func TestContract(t *testing.T) { workplacetest.Run(t, newForgeFixture) }`.
- [ ] **Stap 2: Draai** `go test -race ./internal/workplace/kennforge/ -run TestContract -v` —
  verwacht PASS op alle 15. Faalt een geval, dan is óf de fixture óf de suite fout: kenn-forge
  verandert niet (anti-doel). Controleer daarna de mutatie: zet in `forgeFixture` de 409 van
  `DELETE` uit en zie geval 12 falen; zet `pkill` altijd op succes en zie 9 falen. Draai terug.
- [ ] **Stap 3: Haal de dubbelen weg.** Alleen tests waarvan elke assert nu in een geval staat:
  `TestSessions404Gone`, `TestLookup`, `TestLookupGone`, `TestRemove409Dirty`, `TestRemove404Gone`,
  `TestKillNothingAliveSendsNoSignal`, `TestKillSurvivorIsAnError`, `TestKillPgrepUnmeasurable`.
  `TestKillEmptySessionIDRejectedBeforeAnyCall` houdt alleen het profiel `software`. De rest blijft
  (HTTP-statussen, carrier, podman, systemd).
- [ ] **Stap 4: Draai** `go test -race ./internal/workplace/...` — PASS.
- [ ] **Stap 5: Commit** — `test(workplace): gedeelde contract-suite, kenn-forge haalt hem (#253)`

---

### Taak 5: `Procs` op `/proc`

Klasse: `io/filesystem`; regressie: een gemist of vals merk, een verkeerde starttijd.

**Files:**
- Create: `internal/workplace/local/procs.go`, `procs_linux.go`, `procs_other.go`,
  `procs_linux_test.go`

**Interfaces:**
- Produces:

```go
// markVar marks every process of a run; the carrier sets it.
const markVar = "LIGHTHOUSE_CONTROLLER_RUN"

type Procs interface {
	// Marked lists, sorted, the pids of this user whose environment holds markVar=<sid>, never
	// the calling process. A process that ends during the scan, or an environ that is not ours
	// to read, is skipped; an unreadable /proc, or an empty sid, is an error.
	Marked(sid string) ([]int, error)
	// StartTime is field 22 of /proc/<pid>/stat; ok is false when pid is gone or a zombie (state Z).
	StartTime(pid int) (start uint64, ok bool, err error)
	// Signal sends sig to pid; a pid that is gone is no error.
	Signal(pid int, sig syscall.Signal) error
}

func newProcFS() Procs // linux: procFS{root: "/proc", uid: os.Getuid(), self: os.Getpid()}
                       // other: every method errors "local workplace: needs /proc, which only linux has"
```

  `procFS.Marked`: per numerieke map onder `root`: `os.Stat` met `Sys().(*syscall.Stat_t).Uid`
  gelijk aan `uid`, anders overslaan; `environ` lezen, `fs.ErrNotExist`/`fs.ErrPermission`/`ESRCH`
  overslaan, een andere leesfout is een fout; splitsen op NUL, exacte vergelijking met
  `markVar+"="+sid`. `StartTime`: alles na de laatste `") "`, dan het 20e veld (veld 3 is het eerste).

- [ ] **Stap 1: Schrijf de falende tests** (nep-`/proc` in `t.TempDir()`, behalve waar "echt" staat):
  - `TestMarkedFindsExactMark`: pid 10 environ `"A=1\x00LIGHTHOUSE_CONTROLLER_RUN=sid1\x00"`,
    pid 11 `"LIGHTHOUSE_CONTROLLER_RUN=sid10\x00"`, pid 12 zonder merk, map `self`, pid 13 zonder
    `environ` → `Marked("sid1") == []int{10}`.
  - `TestMarkedSkipsSelf`: zelfde boom, `self: 10` → `[]int{}` (Review Focus 4).
  - `TestMarkedSkipsOtherUser`: `uid: os.Getuid()+1` → leeg.
  - `TestMarkedUnreadableProcIsError`: `root` bestaat niet → fout. `Marked("")` → fout.
  - `TestStartTimeParsesField22`: stat `"42 (we ird) x) S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 777 19 20"`
    → `777, true, nil`. Weg → `0, false, nil`. Dezelfde regel met stand `Z` in plaats van `S`
    → `0, false, nil`. `"42 (x) S 1"` → fout.
  - `TestMarkedRealProcess` (echt): `exec.Command("sleep", "60")` met `Env` plus
    `LIGHTHOUSE_CONTROLLER_RUN=<uuid>` → `newProcFS().Marked(uuid)` bevat zijn pid.
  - `TestSignalRealAndGone` (echt): `Signal(pid, SIGTERM)` op die `sleep` → `Wait` meldt het
    signaal; daarna nog eens `Signal` → `nil`.
- [ ] **Stap 2: Draai** `go test ./internal/workplace/local/` — compileerfout.
- [ ] **Stap 3: Implementeer** `procs.go`, `procs_linux.go`, `procs_other.go`.
- [ ] **Stap 4: Draai** `go test -race ./internal/workplace/local/` — PASS; en
  `GOOS=windows CGO_ENABLED=0 go build ./...` en `GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build ./...` — rc 0.
- [ ] **Stap 5: Commit** — `feat(local): processen van een run vinden op hun merk (#253)`

---

### Taak 6: lokale adapter — records, `Prepare`, `Lookup`, en de fixture

Klasse: `io/filesystem` plus echte git; regressie: een verkeerde worktree, verloren commits, een
verdwenen worktree die niet `ErrGone` is (of juist te vroeg).

**Files:**
- Create: `internal/workplace/local/{adapter,store,worktree,prepare}.go`,
  `store_test.go`, `worktree_test.go`, `prepare_test.go`, `fake_test.go` (nep-`Procs` en
  fixture-runner), `contract_test.go`

**Interfaces:**
- Consumes: `workplace.FollowRemote`, `workplace.WriteFileAtomic`, `workplace.LocalIDPrefix`, `Procs`.
- Produces:

```go
type Adapter struct {
	RepoDir       string // main checkout of the repo (repoconfig.Repo.Dir)
	DefaultBranch string // [forge] default_branch
	Repo          string // [forge] repo, owner/name: records live under StateDir/local/<owner>/<name>
	StateDir      string // ${XDG_STATE_HOME}/lighthouse/dispatch
	Run           workplace.Runner
	Procs         Procs               // nil: newProcFS()
	Home          string              // "": os.UserHomeDir
	Sleep         func(time.Duration) // nil: time.Sleep
	Now           func() time.Time    // nil: time.Now
}
var _ workplace.Workplace = (*Adapter)(nil)
var _ workplace.Transcripts = (*Adapter)(nil)
```

  - `func checkLocal(id string) error` — fout `workspace %s is not a local workspace` als `id` niet
    op `^local-[A-Za-z0-9._-]+$` past. Eerste regel van elke methode met een ID (alle acht en
    `Transcript`; `Spawn` op `ws.ID`, gewrapt met `ErrNotAccepted`). Methoden die nog niet gebouwd
    zijn, geven na de check `errors.New("local: not built yet")`; taak 7-9 vervangen dat.
  - `func newID(branch string) string` — `local-<slug>-<6 hex>`, slug: elk teken buiten
    `[A-Za-z0-9._-]` wordt `-`; hex uit `crypto/rand`.
  - `store` (`store.go`), map `StateDir/local/<owner>/<name>`; `a.store()` weigert een `Repo`
    die niet `<a>/<b>` is met delen uit `[A-Za-z0-9._-]+` (en niet `.`/`..`) of een relatieve `StateDir`:

```go
type wsRecord struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	Branch    string    `json:"branch"`
	Adopted   bool      `json:"adopted"`
	CreatedAt time.Time `json:"created_at"`
}
type sessRecord struct {
	Key       string    `json:"key"`
	SessionID string    `json:"session_id"`
	Mode      string    `json:"mode"`
	StartedAt time.Time `json:"started_at"`
}
func (s store) workspace(id string) (wsRecord, bool, error)   // workspaces/<id>.json
func (s store) workspaces() ([]wsRecord, error)
func (s store) putWorkspace(r wsRecord) error
func (s store) sessions(id string) ([]sessRecord, error)      // sessions/<id>/*.json
func (s store) putSession(id string, r sessRecord) error
func (s store) pidPath(id, key string) string                 // sessions/<id>/<key>.pid
func (s store) logPath(key string) string                     // logs/<key>.log
func (s store) pid(id, key string) (pid int, start uint64, ok bool, err error) // "<pid> <start>\n"; missing: ok false; malformed: error
func (s store) dropSession(id, key string) error              // .json, .pid, .log; missing is fine
func (s store) dropWorkspace(id string) error                 // every session, sessions/<id>, workspaces/<id>.json
```

  - `worktree.go`:

```go
type worktree struct{ Path, Branch string } // Branch "" when detached
func parseWorktrees(porcelain string) []worktree // "branch refs/heads/<b>"; first entry is the main checkout
func (a *Adapter) worktrees(ctx context.Context) ([]worktree, error) // git worktree prune, then list --porcelain
// resolve reads the record and its worktree. No record, or a vanished worktree whose sessions
// have no marked process: ErrGone. A vanished worktree with marked processes is returned with
// wt == nil. Marked that cannot measure is an error, never ErrGone.
func (a *Adapter) resolve(ctx context.Context, id string) (rec wsRecord, wt *worktree, err error)
```

  - `Lookup`: `resolve`; `Workspace{ID, Path: rec.Path, Branch: wt.Branch}`, of `rec.Branch` als
    `wt == nil`.
  - `Prepare`: de stappen van de spec (*Prepare(branch)*). Na een `worktree add` leest hij de
    lijst opnieuw en bewaart hij het pad zoals git het geeft. Daarvoor zet hij
    `/.claude/worktrees/` in `<common>/info/exclude` als die regel er nog niet staat (Review
    Focus 1). `<common>` is de uitvoer van `git -C <RepoDir> rev-parse --git-common-dir`; in de
    hoofdcheckout is die relatief (`.git`), dus `filepath.Join(RepoDir, out)` als hij niet absoluut is. Daarna `workplace.FollowRemote(ctx, a.Run, 60*time.Second, ws, created)`.

- [ ] **Stap 1: Schrijf de falende tests.** Gedeelde helper in `prepare_test.go`:
  `newRepo(t) (repoDir, other string)` — bare `origin` met `main` (commit `m1`), `repoDir` een
  clone, `other` een tweede clone om branches te pushen (vorm van `newGitWorld` in kennforge).
  `newAdapter(t, repoDir) (*Adapter, *fakeProcs, *int)` met `Repo: "o/r"`, `DefaultBranch: "main"`,
  `Run: fixtureRunner`, `Procs` de `fakeProcs`, `Home` en `StateDir` elk een `t.TempDir()`, en een
  `Sleep` die alleen telt (de `*int`).
  - `TestParseWorktrees`: porcelain met hoofdcheckout op `main`, een worktree op `issue-1-x`, een
    `detached` met `locked`-regel → letterlijke `[]worktree`.
  - `TestStoreFileModes`: na `putWorkspace` en `putSession`: bestanden `0600`, mappen `0700`.
  - `TestStoreDropWorkspaceRemovesAll`: na `putSession` plus een pid- en logbestand: `dropWorkspace`
    laat geen bestand van die ID achter.
  - `TestStorePidMalformed`: `"abc"` → fout; `"12 34\n"` → `12, 34, true`.
  - `TestPrepareNewBranchFromDefault`: geen branch → pad `<repoDir>/.claude/worktrees/<id>`,
    `HEAD == m1`, geen upstream (`rev-parse --abbrev-ref issue-1-x@{upstream}` faalt), record
    `Adopted == false`, ID past op `^local-issue-1-x-[0-9a-f]{6}$`.
  - `TestPrepareTracksRemoteBranch`: `origin/issue-1-x` één commit `b` voor → `HEAD == b`, upstream
    `origin/issue-1-x`.
  - `TestPrepareKeepsLocalBranchCommits`: lokale branch `issue-1-x` in `repoDir` met eigen commit
    `c`, niet uitgecheckt, niet gepusht → `HEAD == c`.
  - `TestPrepareRefusesMainCheckoutBranch`: `Prepare("main")` → fout met
    `is checked out in the main checkout`; geen record.
  - `TestPrepareAdoptsForeignWorktree`: `git worktree add <tmp>/mine -b issue-1-x` met de hand →
    `Path == <tmp>/mine`, record `Adopted == true`, `.claude/worktrees/` leeg.
  - `TestPrepareSlashAndDashBranches`: `a/b` en `a-b` → twee verschillende ID's en paden.
  - `TestPrepareKeepsMainCheckoutClean`: repo zonder `.gitignore` → na `Prepare`:
    `git -C repoDir status --porcelain` leeg; tweede `Prepare` op een andere branch voegt de
    exclude-regel niet nog eens toe (precies één regel `/.claude/worktrees/`).
  - `TestPrepareVanishedWorktreeFreshID`: `Prepare`, `os.RemoveAll(path)`, `Lookup` → `ErrGone`;
    `git worktree list` noemt het pad niet meer; weer `Prepare` → ander ID en ander pad.
  - `TestLookupBranchFromGit`: `git -C path checkout -b other` → `Lookup().Branch == "other"`.
  - `TestLookupVanishedButMarked`: `Prepare`, `putSession(id, {Key: sid+".0a1b2c3d", SessionID: sid})`,
    `procs.marked[sid] = []int{4242}`, `RemoveAll(path)` → `Lookup` geeft `Path == rec.Path`; met
    `procs.err` gezet: een fout die niet `ErrGone` is; met `marked` leeg: `ErrGone`.
  - `TestForeignIDIsNoLocalWorkspace`: ID `ws1` op alle acht methoden en `Transcript`: een fout die
    niet `ErrGone` is, nul aanroepen van `Run` en `Procs`; `Spawn` is `ErrNotAccepted`.
  - `TestRecordsSurviveNewAdapter`: `Prepare` met adapter A; een nieuwe `Adapter` met dezelfde
    velden: `Lookup(id)` geeft hetzelfde pad.
  - `contract_test.go`: `localFixture` (zie hieronder) en

```go
// pending lists the cases whose methods are not built yet; task 9 deletes it.
var pending = map[string]bool{ /* every Cases name except the two below */ }

func TestContract(t *testing.T) {
	for _, c := range workplacetest.Cases {
		t.Run(c.Name, func(t *testing.T) {
			if pending[c.Name] {
				t.Skip("method not built yet")
			}
			c.Run(t, newLocalFixture(t))
		})
	}
}
```

  Aan in deze taak: `PrepareTwiceSameWorkspace`, `LookupKnownAndForgotten`.

  **`localFixture`** (`fake_test.go`/`contract_test.go`):
  - `fakeProcs`: `marked map[string][]int`, `start map[int]uint64`, `survivors map[int]bool`,
    `termProof map[int]bool` (SIGTERM doet niets), `err error`, `calls int`, `sent []sentSignal`,
    met mutex. `Marked`/`StartTime` tellen `calls` en geven `err` als die gezet is. `Signal` telt
    `calls`, voegt toe aan `sent`, en haalt de pid uit `marked` en `start`, behalve een survivor,
    of een `termProof` bij SIGTERM.
  - `fixtureRunner`: `git` via `workplace.ExecRunner()`; elke andere opdracht is `t.Errorf`.
    Taak 7 voegt toe: `sh -c "command -v claude"` → exit 0; en de launch
    (`name == "sh" && args[1] == launchScript`), die `Launches` telt, en
    (behalve na `SpawnTimesOut`, dan `"", -1, context.DeadlineExceeded`) `"<1000+n> 5000\n"` naar
    `args[5]`, en onthoudt pid per sid (uit `args[7]`, `LIGHTHOUSE_CONTROLLER_RUN=<sid>`).
  - `Live(sid)`: de pid van die sid (of een nieuwe) in `marked[sid]`, `start[pid] = 5000`.
    `Survivor`: elke pid van `marked[sid]` in `survivors`. `Unmeasurable`: `err` zetten.
    `Forget`: `os.RemoveAll(ws.Path)`. `MakeDirty`: een untracked bestand in `ws.Path`.
    `Spec`: `Profile "infra"`, `Mode "start"`, `Prompt "do it"`, `InstructionsFile "/abs/instr.md"`.
    `ProcCalls`/`Signals`: `calls`/`len(sent)`.

- [ ] **Stap 2: Draai** `go test ./internal/workplace/local/` — FAIL/compileerfout.
- [ ] **Stap 3: Implementeer** `adapter.go`, `store.go`, `worktree.go`, `prepare.go` zoals hierboven.
- [ ] **Stap 4: Draai** `go test -race ./internal/workplace/local/` — PASS (twee contractgevallen
  aan, de rest `SKIP`).
- [ ] **Stap 5: Commit** — `feat(local): worktree per branch, records op schijf, Prepare en Lookup (#253)`

---

### Taak 7: lokale adapter — `Spawn` en `Sessions`

Klasse: `io/filesystem` plus subprocess; regressie: een tweede launch, een launch die `done` lijkt,
de verkeerde argv.

**Files:**
- Create: `internal/workplace/local/spawn.go`, `spawn_test.go`
- Modify: `internal/workplace/local/store.go` (`ensureLogs`), `internal/workplace/local/contract_test.go` (pending)

**Interfaces:**
- Consumes: `store`, `resolve`, `workplace.NewCarrier`, `Procs`.
- Produces: `const launchScript`, de argv-vorm die de fixture-runner herkent:

  `sh -c <launchScript> sh <ws.Path> <logPath> <pidPath> env LIGHTHOUSE_CONTROLLER_RUN=<sid> LIGHTHOUSE_STATE_DIR=<StateDir> claude -p <Carrier.Args…>`

  (`args[3]` pad, `args[4]` log, `args[5]` pid-bestand, `args[7]` het merk.) Het script, exact:

```go
// launchScript detaches claude from the tick: setsid -f gives it a session of its own, and the
// redirections free the Runner's pipes. The child writes its own pid and start time (field 22
// of /proc/<pid>/stat) before exec, so the pid file describes claude. $$ inside $(...) is the
// pid of the shell that execs.
const launchScript = `cd "$1" || exit 97
log=$2; pidf=$3; shift 3
umask 077
exec setsid -f sh -c '
st=$(sed "s/.*) //" /proc/$$/stat | cut -d" " -f20) && [ -n "$st" ] || exit 98
printf "%s %s\n" "$$" "$st" >"$0.tmp" && mv "$0.tmp" "$0" || exit 98
exec "$@"' "$pidf" "$@" </dev/null >>"$log" 2>&1`
```

  Constanten: `pidWait = 10 * time.Second`, `pidPoll = 200 * time.Millisecond`,
  `launchTimeout = 30 * time.Second`.
  `Spawn`-volgorde (spec *Spawn* 1-6): carrier → `sh -c "command -v claude"` → `Marked(sid)` leeg →
  de map `logs/` maken (`0700`, via een `store`-methode `ensureLogs() error`; `>>"$log"` maakt
  geen map, en een mislukte redirect op `exec` beëindigt `sh`) → `putSession` (sleutel
  `<sid>.<8 hex>`) → launch → uitkomst. Elke stap vóór de launch die faalt,
  is `ErrNotAccepted` zonder launch. `Sessions` (spec *Sessions*): `resolve`; records zonder
  pid-bestand overslaan; `State` `"working"` als `StartTime(pid)` ok is en gelijk aan de start uit
  het bestand, anders `"done"`; `UpdatedAt = StartedAt`; een `Procs`-fout is een fout voor de lijst.

- [ ] **Stap 1: Schrijf de falende tests** (met `newAdapter` en een opnemende runner rond de fixture-runner):
  - `TestSpawnLaunchCommand`: het launch-argv is letterlijk
    `["-c", launchScript, "sh", ws.Path, <StateDir>/local/o/r/logs/<key>.log, <StateDir>/local/o/r/sessions/<id>/<key>.pid, "env", "LIGHTHOUSE_CONTROLLER_RUN=" + sid, "LIGHTHOUSE_STATE_DIR=" + StateDir, "claude", "-p", "--permission-mode", "auto", "--strict-mcp-config", "--allowedTools=Read(~/.claude/plugins/cache/**)", "--append-system-prompt-file", "/abs/instr.md", "--session-id", sid, "do it"]`;
    `s.Key` past op `^` + sid + `\.[0-9a-f]{8}$`; de map van het logpad bestaat met mode `0700`.
  - `TestSpawnLogsDirUnmakeableNotAccepted`: `StateDir/local/o/r/logs` is een gewoon bestand →
    `ErrNotAccepted`, geen launch, geen sessie-record.
  - `TestSpawnNoClaudeNotAccepted`: `command -v` exit 1 → `ErrNotAccepted`, geen launch, `store.sessions` leeg.
  - `TestSpawnMarkedSessionNotAccepted`: `marked[sid]` gezet → `ErrNotAccepted`, geen launch.
  - `TestSpawnUnwritableStateNotAccepted`: `StateDir/local/o/r/sessions` bestaat met mode `0500`
    en `sessions/<id>` nog niet (`WriteFileAtomic` zet alleen zijn directe map op `0700`) →
    `ErrNotAccepted`, geen launch (Review Focus 5).
  - `TestSpawnLaunchExitNonZeroNotAccepted`: launch exit 97 → `ErrNotAccepted`, sessie-record weg.
  - `TestSpawnRunErrorWithPidFileAccepted`: runner schrijft het pid-bestand en geeft een fout → geaccepteerd.
  - `TestSpawnNoPidFileUncertain`: exit 0 zonder pid-bestand → `ErrUncertain`, `Sleep` 50 keer,
    `store.sessions` heeft 1 record, `Sessions` is leeg.
  - `TestSpawnKeyFreshPerLaunch`: twee spawns met dezelfde sid (de eerste is dood) → twee
    verschillende sleutels, `Sessions` heeft er twee.
  - `TestSessionsStartTimeMismatchIsDone`: pid-bestand `"1001 5000"`, `start[1001] = 6000` → `Done`.
  - `TestSessionsUnmeasurableIsError`: `procs.err` → fout, niet `ErrGone`.
  - `contract_test.go`: haal `SessionsOfForgottenIsGone`, `SpawnInvalidNotAccepted`,
    `SpawnAcceptedShowsSession`, `SpawnUncertainNeverTwice`, `EndedSessionIsDone` uit `pending`.
- [ ] **Stap 2: Draai** `go test ./internal/workplace/local/ -run 'Spawn|Sessions|TestContract'` — FAIL.
- [ ] **Stap 3: Implementeer** `spawn.go`.
- [ ] **Stap 4: Draai** `go test -race ./internal/workplace/local/` — PASS.
- [ ] **Stap 5: Commit** — `feat(local): claude -p losgekoppeld starten en zijn stand meten (#253)`

---

### Taak 8: lokale adapter — `Stop` en `Kill`

Klasse: `pure-logic` over `Procs`; regressie: een proces dat blijft, een signaal aan een vreemde.

**Files:**
- Create: `internal/workplace/local/stop.go`, `stop_test.go`
- Modify: `internal/workplace/local/contract_test.go`

**Interfaces:**
- Produces: `func (a *Adapter) end(ctx context.Context, workspaceID string, sessionIDs []string) error`;
  `Stop` en `Kill` zijn `checkLocal` plus `end`. `end`: een lege sid is een fout vóór elke
  `Procs`-aanroep; per sid de kill-lus uit de randvoorwaarden. Foutteksten:
  `session %s: processes %v still run after SIGKILL`, en een `Marked`-fout gewrapt met de sid.
  Het workspace-record is niet nodig.

- [ ] **Stap 1: Schrijf de falende tests**
  - `TestKillTermEndsAll`: `marked[sid] = [1001, 2002]` → `nil`; `sent` is SIGTERM aan 1001 en
    2002, `Sleep` 1 keer.
  - `TestKillEscalatesToSigkill`: 2002 `termProof` → `nil`; `sent` eindigt met SIGKILL aan 2002;
    `Sleep` 6 keer.
  - `TestKillSurvivorNamesPids`: 2002 survivor → fout met `2002`.
  - `TestKillFindsLeftoverWithoutClaude`: geen sessie-record, `marked[sid] = [3003]` (de
    dev-server van een geëindigde claude) → SIGTERM aan 3003.
  - `TestStopEqualsKill`: zelfde opzet als `TestKillTermEndsAll` via `Stop` → zelfde `sent`.
  - `contract_test.go`: haal `EmptySessionIDRejected`, `KillWithoutProcess`, `KillSurvivorIsError`,
    `KillUnmeasurableIsError`, `KillLeavesOtherSession` uit `pending`.
- [ ] **Stap 2: Draai** `go test ./internal/workplace/local/ -run 'Kill|Stop|TestContract'` — FAIL.
- [ ] **Stap 3: Implementeer** `stop.go`.
- [ ] **Stap 4: Draai** `go test -race ./internal/workplace/local/` — PASS.
- [ ] **Stap 5: Commit** — `feat(local): Stop en Kill beëindigen elk proces met het merk van de run (#253)`

---

### Taak 9: lokale adapter — `Remove`, `Discard`, `Transcript`; de volle suite

Klasse: `io/filesystem` plus echte git; regressie: weggegooid werk, een geadopteerde worktree die
verdwijnt, records die blijven hangen.

**Files:**
- Create: `internal/workplace/local/remove.go`, `remove_test.go`
- Modify: `internal/workplace/local/contract_test.go` (`pending` en de lus weg)

**Interfaces:**
- Consumes: `resolve`, `store`, `end`, `workplace.Dirty`, `workplace.HostTranscriptDir`,
  `workplace.ReadTranscript`.
- Produces: `Remove`, `Discard` en `Transcript` volgens de spec (*Remove*, *Discard*,
  *Transcript*). `Remove` stap 2 (geadopteerd met processen) roept `end` op de sids van zijn
  sessie-records met processen. `Transcript` weigert een sid buiten `^[A-Za-z0-9-]+$` vóór elke
  lees-actie. `git worktree remove` loopt met 60 s.

- [ ] **Stap 1: Schrijf de falende tests**
  - `TestRemoveDeletesWorktreeKeepsBranch`: `Prepare`, `Spawn` (fixture-runner), dan `Remove` →
    `nil`; pad weg; `git -C repoDir branch --list issue-1-x` niet leeg; onder
    `StateDir/local/o/r` geen `workspaces/<id>.json`, geen `sessions/<id>`, geen `logs/<key>.log`.
  - `TestRemoveThenPrepareFreshID`: na `Remove` weer `Prepare` op dezelfde branch → ander ID en pad.
  - `TestRemoveAdoptedLeavesWorktree`: geadopteerde worktree → `nil`; map bestaat nog; `Lookup` `ErrGone`.
  - `TestRemoveAdoptedKillsMarkedFirst`: geadopteerd, sessie-record, `marked[sid] = [1001]` →
    SIGTERM aan 1001 en records weg; met 1001 survivor → fout, `Lookup` vindt hem nog.
  - `TestRemoveVanishedDropsRecords`: `RemoveAll(path)` → `nil`, records weg.
  - `TestRemoveRefusedIsNotDirty`: `git worktree lock <path>` op een schone worktree → fout, niet
    `ErrDirty`; `Lookup` vindt hem nog.
  - `TestDiscardDropsOnlyPidlessSessions`: twee sessie-records, één met pid-bestand → na `Discard`
    alleen die met pid-bestand.
  - `TestDiscardKeepsVanishedButLive`: `RemoveAll(path)`, `marked[sid]` → `Discard` laat de records
    staan; na `Kill` (marked leeg): `Lookup` `ErrGone`, en `Discard` laat geen record achter.
  - `TestTranscriptReadsHostProjectDir`: `Home = t.TempDir()`, transcript `unknownTranscript`
    (vorm uit `kennforge/transcript_test.go`) onder `workplace.ProjectDir(home, ws.Path)` →
    `UnknownCommand == "/superpowers:subagent-driven-development"`; geen projectmap → `ErrNotMeasurable`.
  - `TestTranscriptUnsafeSessionID`: sid `"../x"` → fout, geen leesactie.
  - `contract_test.go`: verwijder `pending` en de lus; `TestContract` wordt
    `workplacetest.Run(t, newLocalFixture)`.
- [ ] **Stap 2: Draai** `go test ./internal/workplace/local/ -run 'Remove|Discard|Transcript|TestContract'` — FAIL.
- [ ] **Stap 3: Implementeer** `remove.go`; vervang de laatste `not built yet`.
- [ ] **Stap 4: Draai** `go test -race ./internal/workplace/local/ -run TestContract -v` — 15 PASS, 0 SKIP;
  daarna `go test -race ./internal/workplace/...` — PASS.
- [ ] **Stap 5: Commit** — `feat(local): Remove, Discard en Transcript; de lokale adapter haalt de contract-suite (#253)`

---

### Taak 10: `tick` kiest de adapter

Klasse: `config`; regressie: een bestaande `host.toml` die opeens een andere adapter krijgt.

**Files:**
- Modify: `internal/cli/tick.go` (`tickEnv.workplace` en het commentaar bij `tickEnv.Workplace`)
- Test: `internal/cli/tick_test.go`

**Interfaces:**
- Consumes: `hostconfig.WorkplaceLocal`, `local.Adapter`.
- Produces: in `tickEnv.workplace`, na de `e.Workplace`-override:
  `cfg.Workplace == hostconfig.WorkplaceLocal` →
  `&local.Adapter{RepoDir: repo.Dir, DefaultBranch: repo.DefaultBranch, Repo: repo.Slug, StateDir: stateDir, Run: e.runner(), Home: e.Getenv("HOME")}`;
  elke andere waarde (ook `""`) → de `kennforge.Adapter` van nu, ongewijzigd.

- [ ] **Stap 1: Schrijf de falende test**

```go
func TestTickWorkplaceFollowsHostToml(t *testing.T) {
	repo := repoconfig.Repo{Dir: "/srv/r", Slug: "o/r", Kind: "github", DefaultBranch: "trunk"}
	env := tickEnv{Getenv: func(k string) string { return map[string]string{"HOME": "/home/u"}[k] }}
	// "local" via LoadDispatch on a temp host.toml with [dispatch] workplace = "local":
	// env.workplace(cfg, repo, "/state") is *local.Adapter with RepoDir "/srv/r",
	// DefaultBranch "trunk", Repo "o/r", StateDir "/state", Home "/home/u", Run non-nil.
	// A host.toml with only [dispatch.forge] url, and a zero Dispatch{}: *kennforge.Adapter.
}
```

- [ ] **Stap 2: Draai** `go test ./internal/cli/ -run TestTickWorkplaceFollowsHostToml` — FAIL.
- [ ] **Stap 3: Implementeer** de keuze.
- [ ] **Stap 4: Draai** `go test -race ./internal/cli/ ./internal/hostconfig/` — PASS.
- [ ] **Stap 5: Commit** — `feat(tick): [dispatch] workplace = "local" start runs zonder kenn-forge (#253)`

---

### Taak 11: echte processen, de tick-unit en zijn cgroup

Klasse: integratie (`io` plus subprocess) en `config` (de unit); regressie: een launch-script dat in
een echte shell anders doet dan de fixture aanneemt, en een unit die de run doodt.

**Files:**
- Create: `internal/workplace/local/process_linux_test.go`, `internal/workplace/local/cgroup_systemd_test.go`,
  `deploy/systemd/lighthouse-tick.service`, `deploy/systemd/lighthouse-tick.timer`

**Interfaces:**
- Consumes: de volledige `Adapter` met `Run: workplace.ExecRunner()` en `Procs: nil` (echte `/proc`).

`deploy/systemd/lighthouse-tick.service` (kopregels-commentaar in de stijl van
`lighthouse.service`: waarom `KillMode=process`, waarom geen hardening, waar `HOME` en
`XDG_RUNTIME_DIR` vandaan komen — de user-manager zet ze —, waarom `forge.env`):

```ini
[Unit]
Description=lighthouse — dispatcher tick

[Service]
Type=oneshot
KillMode=process
EnvironmentFile=%h/.config/lighthouse/forge.env
Environment=PATH=%h/.local/bin:%h/go/bin:/usr/local/bin:/usr/bin:/bin
ExecStart=%h/go/bin/lighthouse tick
SyslogIdentifier=lighthouse-tick
```

`deploy/systemd/lighthouse-tick.timer` *(keuze in dit plan: elke 5 minuten)*:

```ini
[Unit]
Description=lighthouse — dispatcher tick, every 5 minutes

[Timer]
OnBootSec=2min
OnUnitActiveSec=5min

[Install]
WantedBy=timers.target
```

- [ ] **Stap 1: Schrijf de tests**
  - `TestRealRunKillReachesGrandchild` (`process_linux_test.go`): sla over zonder `setsid`
    (`exec.LookPath`). Repo in `t.TempDir()/repo met spatie` (Review Focus 3). Een nep-`claude` in
    een eigen `bin`-map voorop `PATH` (`t.Setenv`):

```sh
#!/bin/sh
printf '%s\0' "$@" >"$LH_TEST_ARGV"
pwd >"$LH_TEST_CWD"
setsid -f sh -c 'echo $$ >"$LH_TEST_GRANDCHILD.tmp" && mv "$LH_TEST_GRANDCHILD.tmp" "$LH_TEST_GRANDCHILD"; exec sleep 600'
exit 0
```

    `Run` is `workplace.ExecRunner()` in een omhulsel dat de uitkomst van de launch-aanroep
    (`name == "sh"`, `args[1] == launchScript`) bewaart; assert daarop exit 0 en geen fout (zo
    houdt het script de pipes van de `Runner` niet vast).
    `Prepare`, `Spawn` met prompt `"it's \"$HOME\" $(id); echo\nnext"` → geaccepteerd. Wacht (tot
    10 s) op het kleinkind-bestand. Assert: de argv eindigt op exact die prompt, met `-p` als
    eerste argument (Review Focus 2); `LH_TEST_CWD` is `ws.Path`; `Sessions` wordt binnen 10 s
    `Done`. Dan `Kill(ws.ID, [sid])` → `nil`, en binnen 5 s is `/proc/<kleinkind>` weg of in
    stand `Z`. `t.Cleanup` stuurt SIGKILL aan het kleinkind.
  - `TestTickUnitKeepsDetachedRun` (`cgroup_systemd_test.go`): sla over zoals
    `kennforge/stop_systemd_test.go` (geen user-bus of geen `systemd-run`). Lees de
    `KillMode=`-regel uit `../../../deploy/systemd/lighthouse-tick.service` (zo faalt de test als
    de unit verandert), en draai
    `systemd-run --user --wait --collect -p KillMode=<waarde> --unit=lh-tick-test-<pid>-<ns> sh -c 'setsid -f sh -c "echo \$\$ > <bestand>; exec sleep 600"'`.
    Na terugkeer: poll tot 5 s op `<bestand>` (`setsid -f` keert terug voordat het kind schrijft;
    geen bestand na 5 s is een fout). Assert: de pid uit `<bestand>` leeft (`/proc/<pid>/stat`
    bestaat, stand niet `Z`).
    `t.Cleanup` doodt hem.
- [ ] **Stap 2: Draai** `go test -race ./internal/workplace/local/ -run 'RealRun|TickUnit' -v` —
  eerst zonder unit-bestand: `TestTickUnitKeepsDetachedRun` FAIL (bestand ontbreekt).
  Schrijf de twee unit-bestanden; draai opnieuw — PASS (of SKIP met de reden; noem die in het verslag).
- [ ] **Stap 3: Mutatie** — zet in de unit tijdelijk `KillMode=control-group`: de cgroup-test faalt.
  Zet het terug. Haal in `launchScript` tijdelijk `>>"$log" 2>&1` weg: `TestRealRunKillReachesGrandchild`
  faalt op de assert dat de launch-aanroep zonder fout terugkwam (`ExecRunner` geeft na
  `WaitDelay` een fout). Zet het terug. (`</dev/null` muteren bewijst niets: `ExecRunner` geeft
  al `/dev/null` als stdin.)
- [ ] **Stap 4: Draai** `go test -race ./internal/workplace/...` — PASS.
- [ ] **Stap 5: Commit** — `feat(deploy): tick-unit met KillMode=process, en echte procestests van de lokale adapter (#253)`

---

### Taak 12: documentatie en changelog

Klasse: docs; geen code. `md-link-check` bewaakt de links.

**Files:**
- Modify: `docs/workplace.md`, `docs/tick.md`, `docs/install.md`, `docs/host-assumptions.md`
- Create: `changelog.d/issue-253-werkplek-zonder-kenn-forge.md`

- [ ] **Stap 0: `docs/host-assumptions.md`** (Engels; r. 7: "A new assumption gets a row here in
  the same change that adds it"). Twee rijen in *Inventory*, in de vorm van de bestaande:
  1. `internal/workplace/local/procs_linux.go`, `spawn.go` — de lokale adapter leest
     `/proc/<pid>/environ` en `/proc/<pid>/stat`, en start via `sh` en util-linux `setsid -f` —
     `Linux-only` — elders geven de methoden van `Procs` de fout
     `local workplace: needs /proc, which only linux has`; `workplace = "local"` werkt alleen op
     Linux — `#253`.
  2. `deploy/systemd/lighthouse-tick.service` — de tick als user-unit met `KillMode=process`,
     zonder hardening — `Host feature` — zonder systemd draait de tick uit cron (`docs/install.md`);
     een unit met `KillMode=control-group` doodt elke lokale run na de tick — `#253`.
  En in de bestaande rij van de forge-standaarden (`hostconfig.go:69,72`) niets wijzigen: die
  geldt nog steeds alleen voor de kenn-forge-adapter.

- [ ] **Stap 1: `docs/workplace.md`.** De huidige kenn-forge-tekst onder een kop *kenn-forge*; een
  nieuwe kop *Lokaal*: de acht methoden kort, de bestanden onder
  `StateDir/local/<owner>/<name>/`, het merk `LIGHTHOUSE_CONTROLLER_RUN` en zijn grens (`env -i`,
  een andere gebruiker of container ontsnapt), de exclude-regel uit `Prepare`, de omgeving van de
  run (erft die van de tick, met `GH_TOKEN`: de run heeft de rechten van de dispatcher), de
  overstap tussen adapters (`not a local workspace`), en de gedeelde contract-suite.
- [ ] **Stap 2: `docs/tick.md`.** In *`[dispatch]` in `host.toml`*: `workplace = "kenn-forge"` met
  commentaar `# of "local"; met local geen [dispatch.forge] en geen target_key`. In *Afloop*: een
  alinea met de tabel *Wat er aan de afloop verandert* uit de spec. Een alinea over de cgroup:
  onder systemd alleen met `KillMode=process`, en de cron-regel.
- [ ] **Stap 3: `docs/install.md`** (Engels): een sectie *Run the tick* na *Running runs*: de unit
  en de timer ophalen zoals `lighthouse.service`, `systemctl --user enable --now lighthouse-tick.timer`;
  waarom `KillMode=process` en geen hardening; `GH_TOKEN` in `forge.env`; `claude` op het `PATH`.
  Met cron:

```text
*/5 * * * * export XDG_RUNTIME_DIR=/run/user/$(id -u) PATH="$HOME/.local/bin:$HOME/go/bin:/usr/bin:/bin"; set -a; . "$HOME/.config/lighthouse/forge.env"; set +a; lighthouse tick
```

- [ ] **Stap 4: Changelog-fragment**

```markdown
---
type: added
---
`lighthouse tick` can run without kenn-forge: with `[dispatch] workplace = "local"` in `host.toml`
each run gets a git worktree and a detached `claude -p` on the host. `deploy/systemd/` has a tick
unit and timer for it.
```

- [ ] **Stap 5: Verifieer en commit.** Draai de KPI-rijen van `lighthouse-go-standard` en noteer per rij
  uitkomst en omgeving (GOOS, tags, `-race`, `GOWORK`). Commit —
  `docs(workplace): lokale adapter, tick-unit en cron beschreven (#253)`

---

## Dekking van de spec

| Spec | Taak |
|---|---|
| Configuratie `host.toml` | 3, 10 |
| Package en vorm, bestanden, ID, sessiesleutel | 6, 7 |
| Bestaat de worktree nog (met de `Marked`-uitzondering) | 6 (`resolve`), 9 |
| Processen van een run, `Procs` | 5 |
| Prepare (incl. `followRemote`-verhuizing) | 1, 6 |
| Spawn, omgeving van de run | 7, 11, 12 |
| Sessions | 7 |
| Stop en Kill | 8, 11 |
| Remove, Lookup, Discard, Transcript | 6, 9 |
| Overstap tussen adapters | 2, 6 |
| Afloopverschillen | 12 (docs; decide verandert niet) |
| De cgroup van de tick | 11, 12 |
| Gedeelde contract-suite (AC 3) | 4, 6-9 |
| Documentatie | 12 |
