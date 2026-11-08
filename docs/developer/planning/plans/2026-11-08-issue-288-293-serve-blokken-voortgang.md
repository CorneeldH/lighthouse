# Serve in blokken, met fase, voortgang en duur per run — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** de pagina van `lighthouse serve` toont zeven gekleurde blokken met een overzicht erboven. Elke lopende run toont zijn fase, zijn taakvoortgang, zijn duur en een stand-badge.

**Architectuur:** de tick leest per run de fase uit het command en de taakvoortgang uit plan en SDD-ledger. Hij schrijft beide in `RunView`. `GET /api/runs` geeft daarnaast de kenn-forge-URL en het nachtvenster uit de hostconfig mee. De SPA splitst `runs` en `deferred` over de blokken Running, Parked, Queue en Night, en houdt het overzicht en de header bij uit de bestaande queries.

**Tech stack:** Go (huma, SQLite `tick_state`), React 19 + Tailwind 4 + lucide-react, vitest, orval.

**Spec:** issue #288 (inhoud van de run-rij) en issue #293 (paginaopbouw). Het ontwerp is de mock-up https://claude.ai/artifact/KRk2eXeHozv6W2VXXBZdwG (light en dark).

## Globale eisen

- UI-teksten zijn Engels. De Nederlandse AC-termen van #288 worden: `taak` → `task`, `bouwen` → `build`, `review` → `review`, `eind-review` → `final review`. Noem die vertaling in de PR.
- Code, namen en commentaar zijn Engels (`code_language_paths`). Docs (`docs/*.md`) zijn Nederlands, behalve `docs/install.md`, dat al Engels is. Changelog-fragmenten zijn Engels.
- Elke externe link heeft `target="_blank"`, `rel="noopener noreferrer"` en een lucide-`ExternalLink`-icoon met `aria-hidden`.
- Kleur is nooit de enige drager: elke badge en elk segment heeft ook tekst, of een `aria-label`/`title`.
- Palet uit `docs/site/map.css`. Light: navy `#14213d`, geel `#f4d74b`, link `#1f4fd1`, blauw `#2f5bd8`, paars `#7b3fc4`, groen `#087a5b`, oranje `#e07b4c`. Dark: achtergrond `#0f1420`, paneel `#171d2b`, rand `#2d3548`, tekst `#e8eaf0`.
- Na een wijziging aan een type in de API: `go test ./internal/server -run TestOpenAPISnapshot -update`, dan `cd web && npm run gen`. Commit `web/openapi.json` en `web/src/api/` mee; de test-gate controleert de drift.
- Elk commit is één logische wijziging, in Conventional Commits, in het Nederlands (`feat(serve): …`).

## Review-focus

1. **De worktree van een run is opgeruimd** terwijl de rij nog staat. Verwacht: geen voortgang, wel de fase, en geen fout of log-spam. → Taak 1, `TestReadProgressMissingWorktree`.
2. **Een tweede ledger van een ander plan in dezelfde worktree.** Verwacht: alleen de map waarvan `plan-path` dit plan noemt telt. → Taak 1, `TestReadProgressOtherPlanIgnored`.
3. **Een planpad zonder `.git` erboven** (bijvoorbeeld `/tmp/x.md`). Verwacht: de klim stopt bij `/`, er is geen voortgang en geen oneindige lus. → Taak 1, `TestReadProgressNoRepo`.
4. **Een deferred-reden die op `night` lijkt**, zoals `night window` of `Night`. Verwacht: alleen exact `night` gaat naar Night, de rest naar Queue. → Taak 4, test `splits deferred on the exact reason night`.
5. **Een lege `workspace_url` of een lege `workspace_id`.** Verwacht: de id als platte code zonder link, en zonder lege `href`. → Taak 4, test `shows the workspace without a link when there is no url`.

---

### Taak 1: Fase en taakvoortgang in `RunView` (#288)

**Bestanden:**
- Maak: `internal/dispatch/progress.go`, `internal/dispatch/progress_test.go`
- Wijzig: `internal/dispatch/view.go` (`RunView`, `RunViews`), `internal/dispatch/view_test.go`
- Wijzig: `web/openapi.json`, `web/src/api/` (geregenereerd)
- Wijzig: `docs/tick.md` (stap 6, de alinea over `tick_state`)

**Interfaces:**
- Produceert: `func Phase(command string) string`
- Produceert: `type Progress struct { Task, Tasks int; Step string }`. `Step` is `"build"`, `"review"` of `"final-review"`.
- Produceert: `func ReadProgress(command string) (Progress, bool)`
- Produceert: in `RunView` de velden `Phase string json:"phase"`, `Task int json:"task"`, `Tasks int json:"tasks"` en `Step string json:"step"`. Zonder voortgang zijn ze `0, 0, ""`.

**Regels voor `Phase`.** Neem het eerste veld van het command. Pas eerst `rewrite` toe (judge.go), dan deze tabel:

| command | fase |
|---|---|
| `/lighthouse-start-issue` | `start-issue` |
| `/lighthouse-review-spec` | `spec-review` |
| `/lighthouse-review-plan` | `plan-review` |
| `/superpowers:writing-plans` | `writing-plans` |
| `/superpowers:subagent-driven-development` | `sdd` |
| `/superpowers:executing-plans` | `executing-plans` |
| `/lighthouse-fix-pr` | `fix-pr` |
| `/lighthouse-write-pr` | `write-pr` |
| `/lighthouse-close-issue` | `close-issue` |
| `/superpowers:brainstorming` | `brainstorming` |
| een ander command | de naam zonder `/`, `lighthouse-` en `superpowers:` |
| een leeg command | `""` |

**Regels voor `ReadProgress`.** Alleen voor `/superpowers:subagent-driven-development <plan>` met een `pathArg`-pad; elk ander command geeft `false`.

1. Root: klim vanaf de map van het plan naar boven tot een map met een entry `.git`, een bestand of een map. Is er geen root tot `/`, dan `false`.
2. Plan-id: het pad relatief aan de root, als het plan onder de root ligt; anders het absolute pad. Zo schrijft `sdd-workspace` het in `plan-path`.
3. Werkmap: de map onder `<root>/.superpowers/sdd/` waarvan `plan-path` (getrimd) gelijk is aan het plan-id. Is er geen, of heeft die map geen `progress.md`, dan `false`.
4. `Tasks`: het aantal verschillende nummers in de koppen van het plan die passen op `(?m)^#{2,4} (?:Task|Taak) (\d+)\b`. Bij 0 geef je `false`.
5. Af: elk nummer in `progress.md` dat past op `(?m)^(?:Task|Taak) (\d+)\b[^:\n]*: complete`. De huidige taak is het hoogste afgeronde nummer + 1, of 1 als er geen is.
6. Is de huidige taak groter dan `Tasks`, dan `Progress{Task: Tasks, Tasks: Tasks, Step: "final-review"}`. Bestaat anders `task-<huidig>-report.md` in de werkmap, dan is `Step` `"review"`, en anders `"build"`.

Een leesfout geeft `false`, nooit een error. De tick mag niet stoppen op de werkmap van een run.

`RunViews` vult `Phase` voor elke run. `Task`, `Tasks` en `Step` vult het alleen als `ReadProgress` `true` geeft.

- [ ] **Stap 1: Schrijf de falende tests in `progress_test.go`.** Een helper bouwt in `t.TempDir()` een worktree met de map `.git`, een plan `docs/p/plan.md` met `### Task 1` t/m `### Task 4`, en een werkmap `.superpowers/sdd/plan/` met `plan-path` (`docs/p/plan.md`) en `progress.md`. Het command is `/superpowers:subagent-driven-development <root>/docs/p/plan.md`.
  - `TestPhase`: een tabeltest met elke rij hierboven, plus `/paia-start-issue 3` → `start-issue` en `/lighthouse-spec-adversarial-review x.md` → `spec-review`.
  - `TestReadProgressBuild`: ledger `Task 1: complete (…)`, `Task 2: complete` → `{3, 4, "build"}`.
  - `TestReadProgressReview`: dezelfde ledger plus `task-3-report.md` → `{3, 4, "review"}`.
  - `TestReadProgressFinalReview`: taken 1 t/m 4 af → `{4, 4, "final-review"}`.
  - `TestReadProgressDutchAndLateStart`: ledger met alleen `Taak 3: complete` → `{4, 4, "build"}`.
  - `TestReadProgressNoLedger`: werkmap zonder `progress.md` → `false`.
  - `TestReadProgressMissingWorktree`: de root is verwijderd → `false`.
  - `TestReadProgressOtherPlanIgnored`: een tweede werkmap `other/` met `plan-path` `docs/p/other.md` en alle taken af → het resultaat komt uit `plan/`.
  - `TestReadProgressNoRepo`: een plan in `t.TempDir()` zonder `.git` erboven → `false`.
  - `TestReadProgressOtherCommand`: `/lighthouse-review-plan <plan>` → `false`.
- [ ] **Stap 2:** `go test ./internal/dispatch -run 'TestPhase|TestReadProgress'`. Verwacht: FAIL, undefined `Phase` en `ReadProgress`.
- [ ] **Stap 3:** Schrijf `Phase` en `ReadProgress` in `progress.go` volgens de regels hierboven. Hergebruik `pathArg` en `rewrite` uit judge.go.
- [ ] **Stap 4:** `go test ./internal/dispatch -run 'TestPhase|TestReadProgress'`. Verwacht: PASS.
- [ ] **Stap 5: Breid `TestRunViews` uit.** Run 3 krijgt het command van de helper-worktree en verwacht `Phase "sdd"`, `Task 3`, `Tasks 4`, `Step "build"`. Run 5 krijgt `/lighthouse-review-plan /x/y.md` en verwacht `Phase "plan-review"`, `Tasks 0`. Draai de test; verwacht FAIL. Voeg dan de velden toe, vul ze in `RunViews` en draai opnieuw; verwacht PASS. `TestSaveRunViewRoundTrip` moet de nieuwe velden heen en terug dragen; voeg ze toe aan zijn fixture.
- [ ] **Stap 6:** Regenereer de OpenAPI-snapshot en de client (zie Globale eisen). Werk in `docs/tick.md` stap 6 de opsomming bij: na het commando de fase, en voor een SDD-run taak, aantal taken en stap uit plan en ledger.
- [ ] **Stap 7:** `go test -race ./internal/dispatch ./internal/server` en `cd web && npm run typecheck`. Verwacht: PASS.
- [ ] **Stap 8: Commit.** `feat(dispatch): fase en taakvoortgang per lopende run (#288)`.

### Taak 2: `/api/runs` geeft de kenn-forge-URL en het nachtvenster (#293)

**Bestanden:**
- Wijzig: `internal/server/server.go` (`Deps`), `internal/server/forge.go` (`runsOutput`, `registerRuns`), `internal/server/forge_test.go`
- Wijzig: `internal/cli/serve.go` (`serve`, de `RunE` van `newServeCommand`), `internal/cli/serve_test.go`
- Wijzig: `web/openapi.json`, `web/src/api/` (geregenereerd), `docs/install.md` (*Running runs*)

**Interfaces:**
- Consumeert: `hostconfig.LoadDispatch(path) (hostconfig.Dispatch, error)`, met de velden `.Forge.URL` en `.Night`.
- Produceert: `type RunsMeta struct { WorkspaceURL, Night string }` in `internal/server`, met `WorkspaceURL` de kenn-forge-basis-URL zonder `/` aan het eind en `Night` als `"HH:MM-HH:MM"`. `server.Deps` krijgt het veld `RunsMeta RunsMeta`.
- Produceert: in de body van `/api/runs` de velden `workspace_url` en `night`, naast `runs` en `deferred`.

De naam is `workspace_url`, niet `forge_url`: in serve betekent *forge* GitHub (`--forge-url`).

- [ ] **Stap 1: Schrijf de falende test in `forge_test.go`.** Met `Deps{RunsMeta: RunsMeta{WorkspaceURL: "http://127.0.0.1:8091", Night: "21:00-08:00"}}` heeft de body `"workspace_url":"http://127.0.0.1:8091"` en `"night":"21:00-08:00"`. Pas de bestaande lege-body-test aan naar `{"runs":[],"deferred":[],"workspace_url":"","night":""}`.
- [ ] **Stap 2:** `go test ./internal/server -run TestRuns`. Verwacht: FAIL.
- [ ] **Stap 3:** Voeg `RunsMeta` toe aan `Deps` en de twee velden aan `runsOutput`. `registerRuns` krijgt `d.RunsMeta` als argument. In `serve.go` laadt `RunE` `hostconfig.LoadDispatch(hostconfig.Path())`. `serve` krijgt een extra parameter `meta server.RunsMeta`, die `RunE` vult met `strings.TrimRight(cfg.Forge.URL, "/")` en `cfg.Night`. Werk de aanroep in `serve_test.go` bij.
- [ ] **Stap 4:** `go test -race ./internal/server ./internal/cli`. Verwacht: PASS, ook nadat de snapshot is bijgewerkt.
- [ ] **Stap 5:** Regenereer de snapshot en de client. Noem in `docs/install.md` onder *Running runs* de twee velden: `workspace_url` uit `[dispatch.forge] url` en `night` uit `[dispatch] night`. Noem daar ook de vier velden uit taak 1.
- [ ] **Stap 6: Commit.** `feat(serve): /api/runs geeft workspace-URL en nachtvenster mee (#293)`.

### Taak 3: Palet, externe links, paneel en header (#293)

**Bestanden:**
- Wijzig: `web/src/index.css` (tokens), `web/src/components/Panel.tsx`, `web/src/components/Header.tsx`, `web/src/components/IssueLink.tsx`, `web/src/components/QuestionList.tsx`, `web/src/components/ReturnedList.tsx`, `web/src/components/CleanupList.tsx`
- Maak: `web/src/components/ExternalLink.tsx`
- Test: `web/src/components/lists.test.tsx`, `web/src/components/Header.test.tsx`

**Interfaces:**
- Produceert: `ExternalLink({ href, label?, children }): JSX.Element`. Het is een `<a>` met `target="_blank"` en `rel="noopener noreferrer"`, en het `ExternalLink`-icoon van lucide met `aria-hidden`. `label` wordt de `aria-label` als die gezet is.
- Produceert: `Panel` krijgt `id: string` (anker voor het overzicht), `icon: LucideIcon`, `tone: Tone` en `count?: number`. `type Tone = 'purple' | 'orange' | 'blue' | 'grey' | 'yellow' | 'navy' | 'green'`.
- Produceert: CSS-tokens `--lh-navy`, `--lh-yellow`, `--lh-link`, `--lh-blue`, `--lh-purple`, `--lh-green`, `--lh-orange`, `--lh-grey`, met dark-waarden onder `.dark`, en Tailwind-kleuren `lh-*` via `@theme inline`.

`Panel` toont geen *updated*-regel meer. Het toont een gekleurde icoontegel, de titel, een navy telpil met `count` (alleen als `count` gezet is) en de foutmelding of *Loading…*, zoals nu. De header toont de merknaam en de versie op navy met een gele onderrand. Rechts staan `updated <age> ago` en `tick <age> ago`, en een gele Refresh-knop.

- `updated`: de leeftijd van de oudste `dataUpdatedAt > 0` over alle queries in de cache. Je leest ze met `useQueryClient().getQueryCache().getAll()`. Tijdens een refresh staat er `refreshing…` naast.
- `tick`: de leeftijd van de nieuwste `at` over `runs` en `deferred` van `useRuns`. Zonder rijen vervalt deze regel.

- [ ] **Stap 1: Pas de tests aan.**
  - In `lists.test.tsx` vervallen de asserts op `updated < 1 min ago` in een paneel. In plaats daarvan: het paneel heeft `id` `questions` en toont de telpil `1` bij één vraag.
  - De Answer-link heeft `target` `_blank` en `rel` `noopener noreferrer`.
  - De issuelink van elke lijst heeft `target` `_blank`.
  - In `Header.test.tsx`: met een runs-rij waarvan `at` 2 minuten oud is, staat er `tick 2 min ago`. Zonder rijen staat er geen `tick`.
- [ ] **Stap 2:** `cd web && npm test`. Verwacht: FAIL op de nieuwe asserts.
- [ ] **Stap 3:** Schrijf de tokens, `ExternalLink`, het nieuwe `Panel` en de nieuwe `Header`. `IssueLink` en de Answer-link gebruiken `ExternalLink`. Geef de vier bestaande panelen hun `id`, icoon, tint en telling:
  - Open questions: `questions`, `MessageCircleQuestion`, purple.
  - Returned: `returned`, `Undo2`, orange.
  - Workspaces to clean up: `cleanup`, `Trash2`, green.
  - Running blijft tot taak 4 zoals het is, op de nieuwe props na: `running`, `Play`, blue.
- [ ] **Stap 4:** `cd web && npm run lint && npm run typecheck && npm test`. Verwacht: PASS.
- [ ] **Stap 5: Commit.** `feat(serve): palet, externe links in nieuw tabblad en gekleurde panelen (#293)`.

### Taak 4: Running, Parked, Queue en Night met de nieuwe run-rij (#288, #293)

**Bestanden:**
- Maak: `web/src/components/RunRow.tsx`, `web/src/components/RunBlocks.tsx` (vervangt `RunsList.tsx`; verwijder `RunsList.tsx`)
- Wijzig: `web/src/lib/format.ts` (`formatDuration`), `web/src/lib/format.test.ts`
- Test: `web/src/components/lists.test.tsx` (de `RunsList`-tests worden `RunBlocks`-tests)

**Interfaces:**
- Consumeert: uit taak 1 `phase`, `task`, `tasks` en `step` op een run; uit taak 2 `workspace_url` en `night`; uit taak 3 `Panel`, `ExternalLink` en `Tone`.
- Produceert: `RunningBlock`, `ParkedBlock`, `QueueBlock` en `NightBlock`, elk zonder props en elk een `Panel` met id `running`, `parked`, `queue` of `night`.
- Produceert: `formatDuration(ageS: number): string`.
- Produceert: `splitDeferred(deferred: DeferredView[]): { queue: DeferredView[]; night: DeferredView[] }` (geëxporteerd voor het overzicht in taak 5).

**`formatDuration`:** onder 60 s `< 1 min`; onder een uur `N min`; anders `H h M min`, met `M` weggelaten als het 0 is.

**De run-rij** (`RunRow`). Bovenaan staan de issuelink en de titel. Daaronder staan:

1. **Stand-badge.**

   | `stand` | tekst | kleur | icoon |
   |---|---|---|---|
   | `working` | `working` | geel | `Activity` |
   | `done` | `done · next tick decides` | groen | `Check` |
   | `waiting` | `waiting` | oranje | `Pause` |
   | `""` | `not measured` | grijs | `Minus` |

   De `done`-badge heeft de `title` `Claude finished its turn; the next tick decides: handoff, done, question or returned`.
2. **Voortgang.** Met `tasks > 0` staat er de tekst `task/tasks`; bij `final-review` is dat `tasks/tasks`. Daarnaast staat een segmentbalk van `tasks` streepjes:
   - een taak onder `task`: `done` (blauw);
   - de taak `task`: `build` (geel gestreept) of `review` (paars);
   - bij `final-review` alle streepjes `final` (groen).

   Het element heeft de `title` en `aria-label` `task 3 of 7 · build`, `task 5 of 6 · review` of `final review`. Zonder taken staat alleen `phase`, als die niet leeg is.
3. **Duur.** Een `Clock`-icoon en `formatDuration((now - started_at) / 1000)`, met de `title` `running since HH:MM`.
4. **Workspace.** Met `workspace_url` en `workspace_id` niet leeg: een `ExternalLink` naar `${workspace_url}/workspaces/${encodeURIComponent(workspace_id)}`, met de `label` `${workspace_id}, opens kenn-forge in a new tab`. Is een van beide leeg, dan de id als `<code>`, of niets als de id leeg is.
5. **ⓘ-knop** met de `aria-label` `Run details` en `aria-describedby` naar een `role="tooltip"`-element. De tooltip toont `command`, `trigger`, `profile` en `started HH:MM`. Hij is zichtbaar bij hover en bij focus (`group-hover`/`group-focus-within`), en staat altijd in de DOM.

De rij toont geen `open`/`parked`-chip en geen tick-tijd.

**De blokken.**
- Running: de runs met `state === 'open'`. Leeg: `No running runs`.
- Parked: `state === 'parked'`, met de uitleg `Stopped mid-run by the usage brake or lighthouse pause. Resumes by itself after the reset or the pause.` Leeg: `No parked runs`.
- Queue: deferred met `reason !== 'night'`. De uitleg is `Ready to start, held by a brake. The next tick with room starts it.` en de reden staat als gele badge. Leeg: `Nothing in the queue`.
- Night: deferred met `reason === 'night'`. De uitleg is `Handed off for the night window. Starts from <begin>.`, met `<begin>` het deel van `night` vóór `-`. Bij een lege `night` staat er `Handed off for the night window.` Leeg: `Nothing waiting for the night`.

Tinten en iconen: Running blue `Play`; Parked grey `PauseCircle`; Queue yellow `Hourglass`; Night navy `Moon`. Een fout van `/api/runs` toont de bestaande `runsErrorText` in Running; de andere drie blokken tonen dan alleen *Runs unavailable*.

- [ ] **Stap 1: Schrijf de falende tests.**
  - `format.test.ts`: `formatDuration` met 30 → `< 1 min`, 300 → `5 min`, 7200 → `2 h`, 7500 → `2 h 5 min`.
  - In `lists.test.tsx`, voor een SDD-run met `task 3, tasks 7, step 'build'`: de tekst `3/7`, een element met de `aria-label` `task 3 of 7 · build`, de badge `working` en de duur `2 h 5 min`. Zet `now` met `vi.setSystemTime`.
  - Een run met `step 'final-review', task 5, tasks 5`: `5/5` en de `aria-label` `final review`.
  - Een run met `tasks 0, phase 'plan-review'`: de tekst `plan-review` en geen `/`.
  - `stand 'done'` toont `done · next tick decides`; `stand ''` toont `not measured`.
  - De workspace-link heeft de `href` `http://127.0.0.1:8091/workspaces/ws-3` en de `target` `_blank`.
  - `shows the workspace without a link when there is no url`: met `workspace_url ''` staat `ws-3` er, maar niet als link.
  - Het command staat in een `tooltip` en niet in de zichtbare rij-tekst buiten de tooltip.
  - Geen tekst `open` of `parked` in een rij.
  - `splits deferred on the exact reason night`: met de redenen `night`, `night window` en `slots: 4 of 4 in use` staat er één rij in Night en staan er twee in Queue.
  - Night toont `Starts from 21:00.` bij `night '21:00-08:00'`.
  - Een geparkeerde run staat in Parked en niet in Running.
- [ ] **Stap 2:** `cd web && npm test`. Verwacht: FAIL.
- [ ] **Stap 3:** Schrijf `formatDuration`, `RunRow`, `splitDeferred` en de vier blokken. Verwijder `RunsList.tsx`. Zet in `App.tsx` voorlopig de vier blokken op de plek van `RunsList`; de volgorde komt in taak 5.
- [ ] **Stap 4:** `cd web && npm run lint && npm run typecheck && npm test`. Verwacht: PASS.
- [ ] **Stap 5: Commit.** `feat(serve): run-rij met stand, voortgang en duur, en blokken Parked, Queue en Night (#288, #293)`.

### Taak 5: Overzichtstegels, volgorde van de blokken, docs en changelog (#293)

**Bestanden:**
- Maak: `web/src/components/Summary.tsx`
- Wijzig: `web/src/App.tsx`, `docs/install.md` (*Running runs* en de beschrijving van de pagina), `web/src/components/lists.test.tsx`
- Maak: `changelog.d/feat-serve-blokken-voortgang.md`

**Interfaces:**
- Consumeert: `useQuestions`, `useReturned`, `useRuns` en `useCleanup` uit `lib/queries.ts`, en `splitDeferred` uit taak 4.
- Produceert: `Summary(): JSX.Element`, een `<nav aria-label="Summary">` met zeven tegels.

De zeven tegels heten `Questions`, `Returned`, `Running`, `Parked`, `Queue`, `Night` en `To clean up`, in die volgorde. Elke tegel is een `<a href="#<id>">` (interne link, geen nieuw tabblad) met het getal in de tint van zijn blok. Laadt de query nog of faalt hij, dan staat er `–` in plaats van het getal.

De volgorde in `App.tsx`:
1. `Header`
2. `UsagePanel`
3. `Summary`
4. Open questions
5. Returned
6. Running
7. Parked
8. Queue
9. Night
10. Workspaces to clean up

De container blijft `max-w-3xl`. Op telefoonbreedte lopen de tegels in een grid van 4 kolommen door (`grid-cols-4 sm:grid-cols-7`), zonder horizontale scroll.

- [ ] **Stap 1: Schrijf de falende test** `shows a summary tile per block`. Bij 1 vraag, 0 teruggegeven issues, 2 open runs, 1 geparkeerde run, 1 queue-start, 1 night-start en 0 opruimingen heeft elke tegel het goede getal en de `href` `#questions` … `#cleanup`. Een tweede test toont `–` bij een falende `/api/cleanup`.
- [ ] **Stap 2:** `cd web && npm test`. Verwacht: FAIL.
- [ ] **Stap 3:** Schrijf `Summary` en zet de volgorde in `App.tsx`.
- [ ] **Stap 4:** `cd web && npm run lint && npm run typecheck && npm test && npm run build`. Verwacht: PASS.
- [ ] **Stap 5:** Werk in `docs/install.md` de kaart *Running runs* bij tot de blokken Running, Parked, Queue en Night en het overzicht. Schrijf het changelog-fragment (`type: added`, Engels, twee zinnen: de blokken en het overzicht, en de fase, voortgang, duur en stand per run).
- [ ] **Stap 6:** Start `go run ./cmd/lighthouse serve` en bekijk de pagina op 375 px en op 1280 px, in light en in dark. Vergelijk hem met de mock-up. Een afwijking die je bewust laat staan noteer je in het taakrapport.
- [ ] **Stap 7: Commit.** `feat(serve): overzichtstegels en volgorde van de blokken (#293)`.
