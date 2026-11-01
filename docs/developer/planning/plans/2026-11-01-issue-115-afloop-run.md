# Afloop van een run — implementatieplan

> **Voor agents die dit uitvoeren:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. De stappen gebruiken checkboxes (`- [ ]`).

**Doel:** elke open run krijgt per tick een uitkomst uit de afloop-tabel, met een eerlijke reden op 🤖 Teruggegeven, inclusief startfout (#34) en wacht op permissie (#35).

**Architectuur:** de tick meet per open run meer dan alleen de forge-stand: de laatste
transcript-activiteit (hoofdthread en `subagents/*.jsonl`), een startfout in het transcript, een
permissiebestand dat een nieuwe `Notification`-hook in de statemap schrijft, een resume-marker en of
de werkplek nog bestaat. `Decide` blijft puur en beslist met die metingen elke regel uit de tabel;
`Apply` post eerst de Teruggegeven en stopt of vergeet daarna de sessie en sluit de rij. De
I/O-beslissingen die nu in `workplace.CheckOpen` zitten (onzekere start, `max_runtime`), en die geen
productie-aanroeper hebben, verhuizen naar `Decide`; alleen het opzoeken van de sessie van een
onzekere start blijft meten.

**Tech stack:** Go (`internal/dispatch`, `internal/workplace`, `internal/cli`), bash plus Python
voor de hook (`pack/core/hooks/`), pytest voor de hooktests.

**Spec:** [2026-10-27-m2-dispatcher-design.md](../specs/2026-10-27-m2-dispatcher-design.md), §7
*Afloop van een run*, plus *Teststrategie* en *Wat aan de bouwer is*.

## Vaste eisen

- Go volgt `/lighthouse-go-standard`, tests volgen `/lighthouse-test-standard`.
- Een hookbericht en code zijn Engels; docs, changelog en commitberichten Nederlands.
- `pack/`-wijziging: `go run ./cmd/lighthouse init --upgrade` en de geïnstalleerde kopie committen;
  CI vereist `No changes.`. Nooit `.claude/hooks/lighthouse/*` of hun registratie in
  `.claude/settings.json` met de hand wijzigen.
- Een hook die niet kan checken blokkeert nooit: exit 0, en `COULD NOT CHECK` op stderr.
- Een reden op 🤖 Teruggegeven is een headings-sleutel (nl en en) in `pack/core/headings.toml`, met
  hoogstens één letterlijk argument (`Action.ReasonArg`); `apply` toont `<kop>: <argument>`.
- `max_wait` en `max_runtime` komen uit `host.toml [dispatch]` (standaard `1h` en `8h`).
- Gate: `[test_gate] command` uit `lighthouse.toml`; smal tijdens het werk:
  `go test -race ./internal/dispatch/... ./internal/workplace/... ./internal/cli/... ./internal/packs/...`
  en `uv run pytest -q tests/pack/test_permission_check.py`.
- Elke PR voegt `changelog.d/issue-115-afloop-run.md` toe.

## Review-focus

1. **Transcriptmap ontbreekt** (Software-profiel in een devcontainer: de map staat niet op de host).
   Verwacht: niet meetbaar, geen fout in de tick; stil valt terug op de forge-stand en de reden zegt
   dat. Test in taak 1 (`TestTranscriptDirMissing`) en taak 4 (fixture `silent-forge-fallback`).
2. **Een verouderd permissiebestand.** De prompt is al beantwoord en de sessie werkte daarna door.
   Verwacht: geen "wacht op permissie"; alleen activiteit die niet nieuwer is dan de prompt telt als
   wachten. Fixture `permission-answered` in taak 4.
3. **Een kapotte of afgebroken laatste transcriptregel** (de agent schrijft op dat moment). Verwacht:
   die regel telt niet mee, de rest wel, en de tick faalt niet. Test `TestReadTranscriptTornLine` in
   taak 1.
4. **Teruggegeven gepost, maar stoppen of vergeten mislukt.** Verwacht: de volgende tick post niet
   opnieuw, maar stopt (of, bij een rij zonder sessie, vergeet) en sluit alleen. Fixtures
   `returned-already-posted` en `returned-already-posted-uncertain` in taak 4, apply-test in taak 5.
5. **Limieten op nul in een bestaande fixture.** Verwacht: de tijdregels (stil, permissie, onzeker,
   time-out) staan uit, zodat elke bestaande fixture groen blijft. Eén uitzondering, bewust:
   `settle-red-pr.json` (klaar, checks `FAILURE`) geeft nu "PR rood" (regel 13). Taak 4, stap 4.
6. **Een PR van vóór de run.** Een gemergde of gesloten PR van een eerdere run (nacontrole, een
   verlaten PR) telt niet. Verwacht: geen `done` en geen "PR gesloten" bij de eerste tick. Fixtures
   `pr-merged-before-run` en `pr-closed-before-run` in taak 4.
7. **Een lange aanloop in het transcript.** Echte transcripten hebben honderden KiB aan
   attachment-regels vóór de eerste `assistant`-regel (558: byte 438524). Verwacht: `AssistantTurn`
   klopt toch. Test `TestReadTranscriptLongPrelude` in taak 1.

---

### Taak 1: transcripten lezen

**Bestanden:**
- Nieuw: `internal/workplace/transcript.go`
- Nieuw: `internal/workplace/transcript_test.go`
- Nieuw: `internal/workplace/testdata/transcript/` (fixtures, zie stap 1)

**Interfaces:**
- Levert:
  - `func ProjectDir(home, cwd string) string`: `<home>/.claude/projects/<cwd waarin elk teken
    buiten [A-Za-z0-9] een '-' is>`. Gemeten op deze host:
    `/home/corneel/.kenn/forge/worktrees/github/github.com/corneeldh/personal_ai_agent/work-issue-558-heavy-claude-foundry-98090489`
    wordt `-home-corneel--kenn-forge-worktrees-github-github-com-corneeldh-personal-ai-agent-work-issue-558-heavy-claude-foundry-98090489`.
  - `type Transcript struct { LastEvent time.Time; Threads map[string]time.Time; UnknownCommand string; AssistantTurn bool }`,
    met JSON-tags `last_event`, `threads,omitempty`, `unknown_command,omitempty` en `assistant_turn`
    (de decide-fixtures van taak 4 serialiseren hem; een gewone run zet `assistant_turn: true`).
    `Threads` is het laatste event per draad: sleutel `""` voor het hoofdtranscript, de bestandsnaam
    zonder `.jsonl` voor een subagent (`agent-a`). `LastEvent` is het maximum daarvan.
  - `func ReadTranscript(dir, sessionID string) (Transcript, error)`: geeft een fout die
    `errors.Is(err, fs.ErrNotExist)` is als `<dir>/<sessionID>.jsonl` ontbreekt, en
    `ErrNotMeasurable` als geen enkele draad een regel met `timestamp` heeft.
  - `var ErrNotMeasurable = errors.New("workplace: not measurable")`
  - `type Transcripts interface { TranscriptDir(ctx context.Context, workspaceID string) (string, error) }`:
    een optionele interface naast `Workplace`; een werkplek zonder transcript op de host geeft
    `ErrNotMeasurable`.

- [ ] **Stap 1: meet hoe een startfout in een transcript staat.** In een lege scratchmap:
  `claude -p --session-id "$(uuidgen)" "/lighthouse-bestaat-niet"`. Zoek daarna het transcript onder
  `~/.claude/projects/<ProjectDir>/` op en noteer bij welk `type` (user, system) en in welk veld de
  tekst `Unknown command: /lighthouse-bestaat-niet` staat, en of er een `assistant`-regel is. Leg
  dat transcript, ingekort tot de regels met `type`, `timestamp` en die tekst, vast als
  `testdata/transcript/unknown-command.jsonl`. Maak daarnaast met de hand:
  - `main-ended-subagent-working/`: `<s>.jsonl` met als laatste event `10:00:00Z`, en
    `<s>/subagents/agent-a.jsonl` met als laatste event `10:40:00Z`;
  - `no-assistant/`: alleen `user`- en `system`-regels;
  - `torn-line/`: een geldige regel met `10:00:00Z` en daarna een afgebroken JSON-regel;
  - `case-558/`: de `type`/`timestamp`-regels van het einde van
    `~/.claude/projects/-home-corneel--kenn-forge-worktrees-github-github-com-corneeldh-personal-ai-agent-work-issue-558-heavy-claude-foundry-98090489/62b1fdd2-7894-4e1e-a0a1-fc81a5d6ef39.jsonl`
    (laatste event met `timestamp`; let op: de staart bevat ook `bridge-session`- en `cost-state`-regels
    zonder `timestamp`, die tellen niet);
  - `case-818/`: de `type`/`timestamp`-regels van
    `~/.claude/projects/-home-corneel--kenn-forge-worktrees-github-github-com-corneeldh-personal-ai-agent-work-issue-818-processed-marker-per-88c1e196/73270a65-d89d-4763-9fef-3b363058df04.jsonl`
    en van `73270a65-d89d-4763-9fef-3b363058df04/subagents/agent-a1370a2b6f1c5edda.jsonl` en
    `agent-a82f84926776ea6ae.jsonl` daarnaast. Gemeten (2026-11-01): geen draad heeft een
    `tool_use` zonder `tool_result`. In `agent-a1370a2b6f1c5edda` kreeg een `Bash`-aanroep om
    `10:45:33.339Z` binnen 78 ms `is_error` "Permission to use Bash with command … has been
    denied." en werkte de subagent door tot `10:46:52.562Z`; `agent-a82f84926776ea6ae` eindigt op
    `10:50:35.404Z`, het hoofdtranscript op `10:51:30.716Z`. Er stond dus geen prompt open. Neem
    die regels (alleen `type`, `timestamp`, `is_error`) op en controleer de laatste tijden per
    draad tegen deze waarden.

  Geen inhoud van berichten in de fixtures, behalve de `Unknown command`-tekst. Meet stap 1 in
  plaats van `Unknown command:` de tekst `Unknown skill:`, herken dan beide en noteer dat in de
  commit.

- [ ] **Stap 2: schrijf de falende tests.**

```go
func TestProjectDir(t *testing.T) // de gemeten 558-map hierboven, en "/a/b.c" → "<home>/.claude/projects/-a-b-c"
func TestReadTranscriptSubagentCounts(t *testing.T)   // main-ended-subagent-working: LastEvent == 10:40:00Z
func TestReadTranscriptUnknownCommand(t *testing.T)   // UnknownCommand == "/lighthouse-bestaat-niet"
func TestReadTranscriptNoAssistant(t *testing.T)      // AssistantTurn == false; UnknownCommand == ""
func TestReadTranscriptTornLine(t *testing.T)         // geen fout, LastEvent == 10:00:00Z
func TestReadTranscript558(t *testing.T)              // LastEvent == het gemeten laatste timestamp; regels zonder timestamp tellen niet
func TestReadTranscript818(t *testing.T)              // Threads per draad == de gemeten tijden; LastEvent == hun maximum
func TestReadTranscriptLongPrelude(t *testing.T)      // in de test gegenereerd: 500 KiB attachment-regels waaronder één regel van 90 KiB (zoals regel 34, api-request-shape, in 558 en 818), dan één assistant-regel → AssistantTurn == true
func TestReadTranscriptHugeLastLine(t *testing.T)     // laatste regel > 64 KiB zonder timestamp, daarvoor een regel met 10:00:00Z → LastEvent == 10:00:00Z
func TestReadTranscriptNoTimestamp(t *testing.T)      // alleen regels zonder timestamp → errors.Is(err, ErrNotMeasurable)
func TestReadTranscriptUnknownCommandSuggestion(t *testing.T) // "Unknown command: /x. Did you mean /y?" → UnknownCommand == "/x"
func TestTranscriptDirMissing(t *testing.T)           // errors.Is(err, fs.ErrNotExist)
```

  Een `Unknown command:`-tekst in een `assistant`-regel telt niet (een agent die erover praat,
  zoals in de paia-transcripten op deze host): voeg die regel toe aan `no-assistant/` met
  `type: assistant` in een aparte test `TestUnknownCommandInAssistantDoesNotCount`.

- [ ] **Stap 3: draai ze en zie ze falen.** `go test ./internal/workplace/ -run 'Transcript|ProjectDir'`:
  FAIL, `undefined: ReadTranscript`.

- [ ] **Stap 4: implementeer.** Per draad (het hoofdtranscript en elk
  `<sessionID>/subagents/*.jsonl`) is het laatste event het grootste `timestamp` in de laatste
  64 KiB; een regel die niet parseert of geen `timestamp` heeft, telt niet. Geeft dat venster geen
  `timestamp`, verdubbel het dan tot er een is of het begin van het bestand bereikt is; een draad
  zonder enige `timestamp` krijgt geen sleutel in `Threads`. `AssistantTurn` en `UnknownCommand`
  komen uit een stream van het hoofdtranscript van voor af, regel voor regel, tot de eerste
  `assistant`-regel (geen bovengrens in bytes, ook niet per regel: lees met
  `bufio.Reader.ReadBytes('\n')`, niet met een `bufio.Scanner`, die bij een regel boven 64 KiB
  stopt met `bufio.ErrTooLong`; een leesfout is een fout van `ReadTranscript`, nooit
  `AssistantTurn == false`): `AssistantTurn` is of die regel er is,
  `UnknownCommand` komt uit de niet-`assistant`-regels daarvóór, op de plek die stap 1 mat. Het
  commando is het eerste woord na `Unknown command: `, zonder afsluitende `.`, `?` of `,`.

- [ ] **Stap 5: draai ze en zie ze slagen.** Zelfde commando: PASS.

- [ ] **Stap 6: commit.** `feat(workplace): transcripten lezen voor de afloop (#115)`

### Taak 2: de `Notification`-hook schrijft het permissiebestand

**Bestanden:**
- Nieuw: `pack/core/hooks/permission-check.sh`
- Nieuw: `pack/core/hooks/permission-check/note.py`
- Wijzig: `pack/core/hooks/hooks.json` (nieuw event `Notification`)
- Wijzig: `internal/packs/core_test.go:30-66` (twee bestanden in de lijst) en `:132`
  (`kinds[KindHook] != 15` wordt `16`)
- Wijzig: `tests/pack/test_hooks_minimal_config.py` (een `Notification`-payload bij `:94` en
  `permission-check.sh` in de set van `test_every_registration_has_a_payload`)
- Nieuw: `tests/pack/test_permission_check.py`
- Geïnstalleerd (door `init --upgrade`): `.claude/hooks/lighthouse/permission-check.sh`,
  `.claude/hooks/lighthouse/permission-check/note.py`, `.claude/settings.json`

**Interfaces:**
- Levert het bestand `<LIGHTHOUSE_STATE_DIR>/runs/<LIGHTHOUSE_CONTROLLER_RUN>/permission.json`
  (mode 0600, atomair via tmp plus rename, elke prompt overschrijft):

```json
{"version": 1, "at": "2026-11-01T10:00:00.339Z", "tool": "Write", "path": "/tmp/x", "thread": "agent-a", "message": "Claude needs your permission to use Write"}
```

  `at` is RFC 3339 UTC met milliseconden, zoals de regels van het transcript.
  `path` is `file_path`, `path` of `notebook_path` uit de input van het tool; bij `Bash` de eerste
  80 tekens van `command`; anders `""`. `tool` en `path` komen van de nieuwste `tool_use` (op
  `timestamp`) in `transcript_path` en in elk `<transcript zonder .jsonl>/subagents/*.jsonl`,
  zoals `pack/core/lib/turn_roots.py:16-19` die map al leest. `thread` is de draad van die
  `tool_use`, met dezelfde sleutel als `Transcript.Threads` (taak 1): `""` voor het
  hoofdtranscript, anders de bestandsnaam zonder `.jsonl`. Lukt dat niet, dan `tool` uit
  `message` (`... to use <Tool>`), `path` `""` en `thread` `""`. De hook schrijft bij
  `notification_type` `permission_prompt` en `worker_permission_prompt` (beide staan in de
  geïnstalleerde `claude.exe` 2.1.316; welke een achtergrond-subagent geeft, meet de handstap na
  taak 6).

- [ ] **Stap 1: schrijf de falende tests**, op het patroon van
  `tests/pack/test_usage_gate_controller.py` (omgeving zoals daar op regel 55 en 139, payload zoals
  `tests/pack/_testkit.py:167`):

```python
def test_no_controller_run_writes_nothing(tmp_path): ...   # zonder LIGHTHOUSE_CONTROLLER_RUN: geen runs/-map, exit 0, stdout leeg
def test_run_id_not_uuid_shaped_writes_nothing(tmp_path): ...  # "../x": niets, exit 0
def test_permission_prompt_writes_tool_and_path(tmp_path): ...  # tool_use Write {file_path: /tmp/x} in het transcript → tool "Write", path "/tmp/x"
def test_subagent_tool_use_wins_when_newer(tmp_path): ...  # nieuwere tool_use in subagents/agent-a.jsonl → die tool, dat pad en thread "agent-a" (#818)
def test_worker_permission_prompt_writes(tmp_path): ...  # notification_type "worker_permission_prompt" → bestand zoals bij permission_prompt
def test_other_notification_type_writes_nothing(tmp_path): ...  # notification_type "idle_prompt"
def test_unreadable_transcript_falls_back_to_message(tmp_path): ...  # tool uit message, path ""
def test_no_state_dir_could_not_check(tmp_path): ...  # LIGHTHOUSE_STATE_DIR leeg: exit 0, stderr bevat "COULD NOT CHECK"
```

- [ ] **Stap 2: draai ze en zie ze falen.** `uv run pytest -q tests/pack/test_permission_check.py`:
  FAIL, script bestaat niet.

- [ ] **Stap 3: implementeer de hook.** `permission-check.sh` volgt `usage-gate.sh`: snelle weg
  zonder fork en zonder stdin te lezen als `LIGHTHOUSE_CONTROLLER_RUN` leeg of niet in de vorm
  `[0-9a-f-]` is; daarna één `python3 permission-check/note.py` met de payload op stdin. Altijd exit 0,
  nooit stdout. Registratie in `hooks.json`:

```json
"Notification": [
  {"matcher": "permission_prompt|worker_permission_prompt", "hooks": [
    {"type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/hooks/permission-check.sh", "timeout": 10}
  ]}
]
```

  De hook weigert de prompt niet: de spec laat dat aan de bouwer na een meting, en de
  `Notification`-hook kan niet weigeren. Een weigering zou een `PermissionRequest`-hook zijn, buiten
  dit issue.

- [ ] **Stap 4: draai de tests en zie ze slagen.** Zelfde commando: PASS.

- [ ] **Stap 5: installeer en controleer.** `go run ./cmd/lighthouse init --upgrade`, daarna
  `go test ./internal/packs/ ./internal/initcmd/` (PASS, met de twee nieuwe bestanden en de telling
  16 in `core_test.go`), `uv run pytest -q tests/pack/test_hooks_minimal_config.py` (PASS) en nog een
  `init --upgrade` die `No changes.` meldt.

- [ ] **Stap 6: commit** de pack-bestanden, de tests en de geïnstalleerde kopie samen.
  `feat(pack): Notification-hook noteert de permissie-prompt van een controller-run (#115)`

Het bewijs met een echte prompt is geen stap van deze taak: het vraagt mensenhanden en staat in
*Na taak 6*. Taak 3 tot en met 6 hangen er niet van af.

### Taak 3: de tick meet de afloop

**Bestanden:**
- Nieuw: `internal/dispatch/runstate.go` (de meet-typen en de lezers van de statemap)
- Nieuw: `internal/dispatch/runstate_test.go`
- Wijzig: `internal/dispatch/decide.go:26-34` (`RepoInput.Runs`) en `:19-23` (`Input`)
- Wijzig: `internal/dispatch/precheck.go:78-90` (`Boundaries`)
- Wijzig: `internal/cli/tick.go:356-384` (`stands` wordt `measure`), `:229` (limieten in `Input`)
- Wijzig: `internal/cli/run.go:93` (zelfde `Input`)
- Wijzig: `internal/workplace/lifecycle.go:79-151` (`CheckOpen` wordt `AdoptSessions`)
- Wijzig: `internal/workplace/kennforge/adapter.go` (`TranscriptDir`)
- Wijzig: `internal/forge/board.go:11-23,48` (`closedAt` in `prFields`, `PR.ClosedAt`
  `json:"closed_at"`, nultijd voor een open PR, zoals `MergedAt`)
- Wijzig: `internal/dispatch/fingerprint.go` (`Parked` per open run)
- Tests: `internal/workplace/lifecycle_test.go`, `internal/workplace/kennforge/adapter_test.go`,
  `internal/cli/tick_test.go`, `internal/dispatch/precheck_test.go`,
  `internal/dispatch/fingerprint_test.go`, `internal/forge/board_test.go`

**Interfaces:**
- Gebruikt: `workplace.ReadTranscript`, `workplace.ProjectDir`, `workplace.Transcripts`,
  `workplace.ErrNotMeasurable` (taak 1); het bestandsformaat van taak 2.
- Levert:

```go
// RunState is what a tick measured of one open run besides its stand.
type RunState struct {
	Gone          bool                `json:"gone,omitempty"`           // the workplace no longer knows the workspace
	SessionAt     time.Time           `json:"session_at,omitzero"`      // forge updated_at of the run's session
	Transcript    *workplace.Transcript `json:"transcript,omitempty"`   // nil: not measurable
	Permission    *Permission         `json:"permission,omitempty"`
	Parked        bool                `json:"parked,omitempty"`         // a resume marker for the run's session
}
type Permission struct{ At time.Time; Tool, Path, Thread string } // JSON-tags at, tool, path, thread
type Duration time.Duration // MarshalText/UnmarshalText via time.ParseDuration ("1h")

// RepoInput krijgt: Runs map[int64]RunState `json:"runs,omitempty"` (per open run id)
// Input krijgt:     MaxWait, MaxRuntime Duration `json:"max_wait,omitempty"`, `json:"max_runtime,omitempty"`

func ReadPermission(stateDir, sessionID string) (*Permission, error) // nil, nil zonder bestand; een onleesbaar bestand is een fout
func Parked(resumeDir, sessionID string) (bool, error)               // <resumeDir>/<sessionID>.json bestaat als gewoon bestand
func Boundaries(d hostconfig.Dispatch, open []runs.OpenRun, states map[int64]RunState, now time.Time) []time.Time

// workplace:
func AdoptSessions(ctx context.Context, db *sql.DB, w Workplace, repo string) error // het findSession-deel van CheckOpen, zonder Release en zonder Stop
func (a *Adapter) TranscriptDir(ctx context.Context, workspaceID string) (string, error) // GET wsPath(id) → worktree_path → ProjectDir($HOME); map bestaat niet → ErrNotMeasurable
```

  De resume-map is `${XDG_DATA_HOME:-~/.local/share}/lighthouse/resume`, gelijk aan
  `resume_dir()` in `pack/core/lib/usage_guard.py:144`.

- [ ] **Stap 1: schrijf de falende tests.**

```go
func TestReadPermission(t *testing.T)            // het JSON van taak 2 → At, Tool, Path; geen bestand → nil, nil; kapot JSON → fout
func TestParked(t *testing.T)                    // bestand → true; symlink of map → false
func TestDurationText(t *testing.T)              // "1h" heen en terug; "soon" → fout
func TestBoundariesActivity(t *testing.T)        // met Transcript.LastEvent L: L+MaxWait in de lijst; met Permission.At P: P+MaxWait; zonder transcript met SessionAt S: S+MaxWait
func TestFingerprintParked(t *testing.T)         // een resume-marker die verschijnt verandert de vingerafdruk; LastEvent en Permission niet
func TestAdoptSessionsRecordsKey(t *testing.T)   // de bestaande CheckOpen-test voor het vinden van de sessie, nu zonder Release
func TestAdapterTranscriptDir(t *testing.T)      // httptest: worktree_path → ProjectDir; map ontbreekt → ErrNotMeasurable
func TestTickMeasuresRunState(t *testing.T)      // fake werkplek: ErrGone → Gone; sessie met updated_at → SessionAt; transcript, permissie en marker op schijf → gevuld
func TestTickAdoptsBeforePrepare(t *testing.T)   // onzekere run waarvan de sessie in deze tick verschijnt: de snapshot heeft zijn WorkplaceSession, geen "start onzeker"
func TestBoardPRClosedAt(t *testing.T)           // closedAt uit de GraphQL-respons in PR.ClosedAt; null → nultijd
```

  De `CheckOpen`-tests voor `ReasonUncertain` en `ReasonTimeout` vervallen hier: die regels toetst
  taak 4 als fixture.

- [ ] **Stap 2: draai ze en zie ze falen.**
  `go test ./internal/dispatch/ ./internal/workplace/... ./internal/cli/`: FAIL op de nieuwe namen.

- [ ] **Stap 3: implementeer.** `gatherRepo` in `tick.go` roept `AdoptSessions` aan **vóór**
  `prepare` (`tick.go:310`), zodat `runs.Open` in `prepare` de zojuist gevonden sessie al leest en
  `Snapshot.OpenRuns` niet verouderd is. Daarna vult `measure` per open run met workspace de sessies
  (zoals `stands` nu) en vult `Stands` en `Runs`. Een meetfout van één run schrijft een
  `lighthouse: measure <repo>: <fout>`-regel en laat die run zonder meting; hij stopt de tick niet.
  `Transcript` alleen als de werkplek `workplace.Transcripts` is en een map geeft;
  `ErrNotMeasurable` uit `ReadTranscript` laat `Transcript` nil. `Fingerprint` neemt van `Runs`
  alleen `Parked` op (spec §3: de resume-markers); activiteit en permissie niet, die veranderen
  elke tick. De grenzen in `Boundaries` laten de tick op tijd kijken: `LastEvent + MaxWait`,
  `Permission.At + MaxWait` en, zonder transcript, `SessionAt + MaxWait`.

- [ ] **Stap 4: draai ze en zie ze slagen.** Zelfde commando, plus `go test ./...`: PASS. De
  bestaande decide-fixtures blijven groen: `Runs` en de limieten zijn leeg.

- [ ] **Stap 5: commit.** `feat(dispatch): de tick meet activiteit, permissie, marker en werkplek per run (#115)`

### Taak 4: `Decide` beslist elke afloopregel

**Bestanden:**
- Wijzig: `internal/dispatch/decide.go:217-255` (`settle`) en `:50-73` (`Action`)
- Wijzig: `pack/core/headings.toml:41-67` (nieuwe redenen); geïnstalleerd `.lighthouse/headings.toml`
- Nieuw: fixtures in `internal/dispatch/testdata/decide/` (zie stap 1)

**Interfaces:**
- Gebruikt: `RunState`, `Permission`, `Input.MaxWait`, `Input.MaxRuntime` (taak 3).
- Levert aan taak 5: `Action.Stop bool` (`json:"stop,omitempty"`: stop de sessie van de run) en
  `Action.Discard bool` (`json:"discard,omitempty"`: vergeet de launch); een `KindReturn` met
  `RunID > 0` sluit ook de rij als `returned`; een `KindClose` draagt bij `start_failed` de oorzaak
  in `ReasonArg`.

De volgorde in `settle`, eerste treffer wint. "Klaar" is `Stands[row.ID] == workplace.Done`;
"activiteit" is `Transcript.LastEvent`, en zonder transcript `SessionAt`. De sessie van de run
(`WorkplaceSession`, `WorkspaceID`) leest `settle` uit `Snapshot.OpenRuns` op `row.ID`. Een PR
telt alleen als hij van deze run is: `MERGED` met `MergedAt` niet vóór `row.StartedAt`, `CLOSED`
met `ClosedAt` niet vóór `row.StartedAt`, en voor regels 12 en 13 alleen een PR met state `OPEN`.
Elke `KindReturn` uit `settle` draagt `SessionID = row.SessionID`, zodat de Teruggegeven de
sessievoet krijgt die regel 4 herkent.

| # | Situatie | Actie | Reden (sleutel, nl) |
|---|---|---|---|
| 1-3 | Klaar-comment, Handoff (niet einde keten), Vraag | bestaand | — |
| 4 | Eigen 🤖 Teruggegeven van deze sessie na `StartedAt` | `KindClose` `returned`; zonder `WorkplaceSession` `Discard`, anders `Stop` als niet klaar | — |
| 5 | `Parked` | `KindClose` `parked` | — |
| 6 | Een PR van deze run `MERGED` | `KindClose` `done` | — |
| 6b | Klaar en het issue is `CLOSED` | `KindClose` `done` | — (een close-check-run die zijn issue sloot, laat geen Handoff, Vraag of PR achter) |
| 7 | Groene PR en klaar | bestaand `KindDone` | — |
| 8 | `Gone` | `KindReturn` | `reason_workplace_gone` "werkplek verdwenen" |
| 9 | `Transcript.UnknownCommand != ""` | `KindClose` `start_failed`, `Stop` als niet klaar, `ReasonArg` "unknown command `<cmd>`" | — (de retry-regel geeft later "start mislukt: …") |
| 10 | Klaar en `Transcript` gemeten zonder `AssistantTurn` | `KindClose` `start_failed`, `ReasonArg` "session did nothing" | — |
| 11 | Een PR van deze run `CLOSED` zonder merge | `KindReturn`, `Stop` als niet klaar | `reason_pr_closed` "PR gesloten" |
| 12 | Een open PR draft | `KindReturn`, `Stop` als niet klaar | `reason_pr_draft` "PR is draft" |
| 13 | Klaar en een open PR met checks `FAILURE` of `ERROR` | `KindReturn` | `reason_pr_red` "PR rood" |
| 14 | Klaar, geen PR, geen Handoff | `KindReturn` | `reason_no_handoff` "geen handoff of vraag" |
| 15 | `MaxWait > 0`, run zonder `WorkplaceSession`, leeftijd ≥ `MaxWait` | `KindReturn`, `Discard` | `reason_start_uncertain` "start onzeker: geen sessie gevonden" |
| 16 | `MaxWait > 0`, `Permission`, activiteit van draad `Permission.Thread` (`Threads[Thread]`, zonder die sleutel `LastEvent`) niet na `Permission.At`, `now - At ≥ MaxWait` | `KindReturn` | `reason_permission` "wacht op permissie", `ReasonArg` "`<tool>` `<pad>`" |
| 17 | `MaxWait > 0`, niet klaar, transcript gemeten, `now - LastEvent ≥ MaxWait` | `KindReturn`, `Stop` | `reason_silent` "geen activiteit in max_wait" |
| 18 | `MaxWait > 0`, niet klaar, geen transcript, stand Waiting, `now - SessionAt ≥ MaxWait` | `KindReturn`, `Stop` | `reason_silent_forge` "geen activiteit in max_wait (forge-stand; transcript niet meetbaar)" |
| 19 | `MaxRuntime > 0`, leeftijd ≥ `MaxRuntime` | `KindReturn`, `Stop` | `reason_timeout` "time-out (max_runtime)" |

Afwijkingen van de spec-tabel, bewust: "wacht op permissie" stopt de sessie niet, zoals de spec;
de startfout-tekst gebruikt het bestaande paar `reason_start_failed` plus argument in plaats van
twee nieuwe koppen. Geen afwijking: regel 10 vraagt "klaar" zoals de spec, regel 9 niet (een
`Unknown command:`-regel is beslissend); "PR is draft" geldt zoals de spec ook voor een sessie die
nog werkt (geen skill in `pack/core` maakt een draft-PR), en stopt die sessie dan.

- [ ] **Stap 1: schrijf de fixtures (falend)**, één per regel 4-19, met `max_wait: "1h"` en
  `max_runtime: "8h"` in de input. Namen: `returned-already-posted`,
  `returned-already-posted-uncertain` (rij zonder `WorkplaceSession` → `Discard`, geen `Stop`),
  `parked-marker`, `pr-merged`, `pr-merged-before-run` (`MergedAt` vóór `StartedAt`: geen actie),
  `workplace-gone`, `start-failed-unknown-command`, `start-failed-unknown-command-waiting` (stand
  Waiting → `start_failed`, `Stop`), `start-failed-no-assistant`, `pr-closed`,
  `pr-closed-before-run` (`ClosedAt` vóór `StartedAt`: geen actie), `pr-draft` (stand Waiting →
  `KindReturn` `reason_pr_draft`, `Stop`), `done-no-handoff`, `start-uncertain`,
  `permission-wait`, `permission-answered` (activiteit van de draad na de prompt, nog geen
  `max_wait` stil: geen actie), `silent`, `silent-forge-fallback`, `timeout`. Regel 13 krijgt geen
  nieuwe fixture: de bestaande `settle-red-pr.json` verwacht nu
  `[{"kind": KindReturn, "reason": "reason_pr_red", "run_id": 5, "session_id": <sessie van run 5>}]`
  (de enige bestaande fixture die verandert). Plus de drie echte gevallen uit het issue:
  - `case-558-abandoned-idle`: run met sessie `62b1fdd2-7894-4e1e-a0a1-fc81a5d6ef39`, laatste 🤖
    Hervat `2026-10-24T08:46:47Z` en Vraag `09:03:54Z` van een **eerdere** run, stand Waiting
    (`idle`), `Transcript.LastEvent` het gemeten laatste timestamp uit taak 1, `now` meer dan een uur
    later → `KindReturn` `reason_silent`, `Stop`.
  - `case-1463-unknown-command`: sessie `28577747-50e3-4668-8e70-bdb89c1e345c`, Gestart
    `2026-10-24T14:39:38Z`, stand Done, `UnknownCommand` het commando uit de Gestart
    (`/superpowers:writing-plans`) → `KindClose` `start_failed` met dat commando in `ReasonArg`.
  - `case-818-subagent-denied`: sessie `73270a65-d89d-4763-9fef-3b363058df04`, Gestart
    `2026-10-18T10:36:45Z`, stand Waiting, `Transcript.Threads` de gemeten tijden per draad uit
    taak 1 (`case-818/`), geen `Permission` (de prompt werd meteen geweigerd, zie taak 1 stap 1),
    `now` `2026-10-18T11:52:30Z` (laatste event + 61 min) → `KindReturn` `reason_silent`, `Stop`.
    Dat is de echte toestand; "wacht op permissie" toetst de volgende fixture.
  - `permission-wait-subagent` (gemaakt, niet echt): stand Waiting, `Threads` `{"": 10:00, "agent-a":
    10:05}`, `Permission` `{At: 10:05:01, Tool: Write, Path: /tmp/x, Thread: agent-a}`, hoofdthread
    daarna nog actief tot `10:30` (`Threads[""]` `10:30`), `now` `11:06` → `KindReturn`
    `reason_permission` met `ReasonArg` "`Write` `/tmp/x`": regel 16 kijkt alleen naar de draad van
    de prompt.
  - `subagent-keeps-run-alive`: stand Waiting (de hoofdthread beëindigde zijn beurt),
    `SessionAt` twee uur oud, `Transcript.LastEvent` vijf minuten oud (van een subagent) → geen
    actie (AC 3).

- [ ] **Stap 2: draai ze en zie ze falen.** `go test ./internal/dispatch/ -run 'TestDecide'`: FAIL op
  elke nieuwe fixture; `TestDecideKeysAreHeadings` FAIL op de nieuwe sleutels.

- [ ] **Stap 3: implementeer** de regels in `settle` in de volgorde van de tabel, en de sleutels in
  `pack/core/headings.toml` (nl zoals de tabel, en een Engelse vertaling), daarna
  `go run ./cmd/lighthouse init --upgrade` voor `.lighthouse/headings.toml`.

- [ ] **Stap 4: draai alle tests.** `go test -race ./internal/dispatch/...`: PASS, en de bestaande
  fixtures ongewijzigd op `settle-red-pr.json` na (Review-focus 5). Moet een andere bestaande
  fixture veranderen om groen te worden, dan valt een regel verkeerd: herstel de regel, niet de
  fixture.

- [ ] **Stap 5: commit**, fixtures, `decide.go` en beide headings-kopieën samen.
  `feat(dispatch): elke afloopregel in decide, met een eerlijke reden (#115)`

### Taak 5: `Apply` stopt, vergeet en sluit

**Bestanden:**
- Wijzig: `internal/dispatch/apply.go:80-120` (`KindReturn`, `KindClose`, `closeRun`)
- Wijzig: `internal/workplace/lifecycle.go` (`ReasonUncertain`, `ReasonTimeout` en `Closed` weg als
  niets ze meer gebruikt)
- Test: `internal/dispatch/apply_test.go`

**Interfaces:**
- Gebruikt: `Action.Stop`, `Action.Discard`, `Action.RunID`, `Action.ReasonArg` (taak 4).
- Levert: `closeRun(ctx, runID int64, o runs.Outcome, reason string) error`.

Volgorde van een `KindReturn` met `RunID > 0`: post de Teruggegeven, dan `Stop` (sessie van de rij)
of `Discard` (werkplek van de rij), dan `closeRun(returned, reasonText(a))`. Mislukt stoppen, dan
blijft de rij open en is het een fout; de volgende tick ziet de eigen Teruggegeven (regel 4) en
post niet opnieuw. De sessie en workspace komen uit `runs.Open` op `RunID`.

- [ ] **Stap 1: schrijf de falende tests.**

```go
func TestApplyReturnRunPostsThenStopsThenCloses(t *testing.T) // volgorde: CreateComment, Stop(ws, [session]), rij gesloten met outcome returned en reason "geen activiteit in max_wait"
func TestApplyReturnRunStopFailsKeepsRowOpen(t *testing.T)   // Stop geeft fout → rij open, fout in Result.Failed; de geposte body draagt de sessievoet van de run
func TestApplyReturnRunDiscard(t *testing.T)                  // Discard(ws) aangeroepen, Stop niet
func TestApplyCloseDiscard(t *testing.T)                      // KindClose returned met Discard (regel 4, rij zonder sessie) → Discard en sluiten, geen post
func TestApplyCloseStartFailedKeepsCause(t *testing.T)        // KindClose start_failed met ReasonArg → runs-rij reason == ReasonArg
func TestApplyCloseStop(t *testing.T)                         // KindClose returned met Stop → Stop en sluiten, geen post
```

- [ ] **Stap 2: draai ze en zie ze falen.** `go test ./internal/dispatch/ -run TestApply`: FAIL.

- [ ] **Stap 3: implementeer** volgens de volgorde hierboven. `lighthouse run` voert `KindClose`
  al uit; met `Stop` mag dat (de beslissing is gelijk aan die van de tick). Het voert `KindReturn`
  nog steeds niet uit.

- [ ] **Stap 4: draai alle tests.** `go test -race ./...`: PASS.

- [ ] **Stap 5: commit.** `feat(dispatch): apply stopt of vergeet de sessie en sluit de teruggegeven run (#115)`

### Taak 6: docs en changelog

**Bestanden:**
- Wijzig: `docs/tick.md` (stap 6 *Precheck*: de nieuwe metingen; een nieuwe sectie *Afloop* met de
  tabel van taak 4 en de verwijzing naar spec §7; `max_wait` op regel 156 noemt stil en permissie)
- Wijzig: `docs/workplace.md` (`Transcripts` en `ErrNotMeasurable`; `AdoptSessions` in plaats van
  `CheckOpen`; de omgeving op regel 60 noemt het permissiebestand)
- Wijzig: `CLAUDE.md` (*Hooks*: één regel voor `permission-check.sh`, Notification `permission_prompt`,
  schrijft alleen in een controller-run, blokkeert nooit)
- Nieuw: `changelog.d/issue-115-afloop-run.md`

- [ ] **Stap 1: schrijf de docs** in het Nederlands, met relatieve links.
- [ ] **Stap 2: controleer.** De volledige gate uit `lighthouse.toml` (`[test_gate] command`): groen;
  `go run ./cmd/lighthouse init --upgrade`: `No changes.`
- [ ] **Stap 3: commit.** `docs(dispatch): afloop van een run en de permissie-hook (#115)`

### Na taak 6: het bewijs met mensenhanden (geen SDD-taak)

SDD voert dit niet uit en stopt na taak 6. De mens (of de operator met de mens erbij) doet het
vóór `/lighthouse-write-pr`; de PR verwijst naar het resultaat.

Een interactieve sessie is nodig: `claude -p` toont geen permissie-prompt. In een scratchmap met
deze repo geïnstalleerd:
`LIGHTHOUSE_CONTROLLER_RUN=$(uuidgen) LIGHTHOUSE_STATE_DIR=$PWD/state claude --permission-mode default`,
vraag een **achtergrond**-subagent om een bestand buiten de map te schrijven, en laat de prompt
staan. Bewijs voor de PR: de inhoud van `state/runs/<id>/permission.json` met de tool, het pad en
de `thread` van de subagent. Leg ook vast welke velden de echte payload had (`notification_type`:
`permission_prompt` of `worker_permission_prompt`; `transcript_path` van hoofdthread of subagent).
Wijkt dat af van de aanname in taak 2 stap 3, pas dan de hook en de test aan in een nieuwe commit.

Weigert Claude Code de prompt van een achtergrond-subagent meteen, zoals in #818 (taak 1 stap 1),
en ontstaat er dus geen `permission.json`, dan haalt de PR het bewijs uit spec §7 ("een
achtergrond-subagent zo'n bestand oplevert") niet. Leg dat dan aan de gebruiker voor vóór de PR:
het is een spec-punt, geen bouwkeuze.
