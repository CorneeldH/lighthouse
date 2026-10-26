# Ontwerp: usage-guard in `pack/core`

Issue: #66 · Datum: 2026-10-25 · Milestone: M1 — Romp en zicht · Vervolg op #64 (de meter en het
alarm). De overstap in personal_ai_agent is CorneeldH/personal_ai_agent#1634.

## Aanleiding en doel

De usage-guard bewaakt het Claude-venster in een lopende sessie. Hij waarschuwt bij een prompt,
onderbreekt een taak die midden in een vol venster doorloopt, en dwingt bij `Stop` een
hervatbare afronding af. Nu leeft hij als `.claude/hooks/usage-guard.sh` (643 regels) in
personal_ai_agent en leest hij de cache en het alarm van de Python-meter.

Sinds #64 meet `lighthouse usage` het venster en schrijft alleen `lighthouse` het alarm. De guard
verhuist daarom naar `pack/core`, zodat elke lighthouse-repo hem krijgt, en hij leest alleen dat
alarm.

De paia-guard doet meer dan bewaken. Hij schrijft resume-markers voor een poller, zet een wekker
via `CronCreate` en parkeert controller-runs, ook bij een pauze van de controller (#1217 in paia).
Besloten in het gesprek: **al dat gedrag gaat naar lighthouse**. De basis staat altijd aan;
hervatten en de controller-tak zijn opties die een repo aanzet. paia draait daarna de
lighthouse-guard en houdt geen eigen usage-hook.

## Scope

#66 levert:

1. `pack/core/hooks/usage-guard.sh`: de basis-guard, geregistreerd op `UserPromptSubmit`,
   `PostToolUse`, `SubagentStop` en `Stop`, met de standen `prompt`, `tool`, `subagent` en `stop`;
2. twee modules naast de guard: *hervatten* en *controller* (zie *De modules*);
3. de tabel `[usage_guard]` in `lighthouse.toml`, met een booleaans type in `config.py`;
4. het contract dat de modules schrijven en lezen: markervorm, paden, pauzevlag en env-variabelen
   (zie *Contract*);
5. een pauzevorm van `lighthouse-handoff` die het blok naar een opgegeven pad schrijft (zie
   *Handoff naar een bestand*);
6. tests, documentatie in `docs/usage-guard.md`, en het pakket geïnstalleerd in lighthouse zelf
   (dogfood);
7. een herkomstregel in `pack/SOURCES.toml` voor de guard en elke module: `tests/test_sources.py`
   faalt op elk pakketbestand zonder regel. De guard krijgt `source_path` en `source_sha` van de
   paia-guard, de modules `new = true` *(eigen keuze na spec-review, r1-f5)*;
8. een regel voor `usage-guard.sh` in de lijst *Hooks* van `CLAUDE.md` *(eigen keuze na
   spec-review, r1-f17)*.

**Buiten scope:**

- de poller die resume-markers afhandelt, en het hervatten van een geparkeerde controller-run.
  Tot de DoD van M2 doen `usage_resume.py` en `controller_tick.sh` in paia dat, op het contract
  van dit ontwerp. In M2 neemt de lighthouse-controller het over;
- de meter en het alarmformaat (#64, vast);
- het venster van Codex (#65): dat stopt geen Claude-sessie;
- de overstap in paia zelf (#1634);
- blokkerende Stop-gates in andere pakketten (`go-check.sh` in `pack/go`, later `pack/python`)
  laten zwijgen zolang er een geldig alarm ligt. Besloten: een eigen vervolgissue, #74. Tot dan kunnen
  bij een vol venster twee Stop-hooks tegelijk blokkeren, met tegenstrijdige redenen.

## Non-goals en anti-goals

- **Geen** eigen poller in lighthouse in M1. Besloten in het gesprek: de poller is een taak van de
  controller, niet van een repo. De controller hoort bij een gebruiker en draait op één machine. Hij
  coördineert de repo's van die gebruiker en kan werk elders laten draaien (een andere machine,
  een container). Die scheiding tussen controller en runner is een vraag voor M2.
- **Geen** eigen meting in de guard. Hij leest het alarm, nooit de cache, en roept nooit `claude`
  aan.
- **Geen** drempels in de guard. De drempels (`halt_pct_session`, `halt_pct_week`) staan in
  `host.toml` en gelden in de meter. Het gevolg: waarschuwing en onderbreking vuren op dezelfde
  drempel. De vroege waarschuwing die paia had (90 % in de cache, onder de rem) vervalt.
- **Geen** escalatie van de verversfrequentie vlak onder de drempel (`escalate_pct` in paia).
  `lighthouse usage refresh` houdt zijn eigen TTL aan.
- **Geen** woord `paia` in een bestand onder `pack/core/hooks/` dat bij de guard hoort.
- **Anti-goal:** een guard die een sessie onterecht onderbreekt. Een verlopen of onleesbaar alarm
  telt als geen alarm.
- **Anti-goal:** een guard die een tool-call merkbaar trager maakt. Het pad zonder alarm is één
  `[ -f ]`.
- **Anti-goal:** twee tegenstrijdige blokkades voor dezelfde call. Eén proces beslist wie spreekt.

## Aanpakken

Drie manieren overwogen:

- **A. Eén script** met de basis en beide opties als takken. Voor: één bestand. Tegen: dezelfde 600+
  regels als in paia, opties lastig apart te testen of weg te halen.
- **B. Basis-guard met twee modules (gekozen).** Alleen `usage-guard.sh` is geregistreerd. De
  modules liggen ernaast, en de guard roept ze alleen aan als hun optie aan staat. De basis beslist
  wie spreekt. Elke module is los te testen en hangt aan één contract.
- **C. Losse hooks per optie**, alleen geregistreerd als de optie aan staat. Voor: een uitgezette
  optie kost niets. Tegen: `internal/packs` kent geen voorwaardelijke registratie, en drie hooks
  op hetzelfde event weten niets van elkaar. Bij een controller-run blokkeren basis en controller
  dan allebei, met tegenstrijdige redenen.

De gebruiker koos B.

## Configuratie

In `lighthouse.toml`, gecommit, per repo:

```toml
[usage_guard]
resume = false      # hervatten: resume-marker en wekker voor een gewone sessie
controller = false  # controller-tak: een controller-run parkeren, ook bij een pauze
```

Beide zijn standaard `false`. Zonder tabel draait alleen de basis.

`config.py` kent nu `str`, `int`, `list[str]` en `list[int]`. Het krijgt een booleaans type in
`_check_type`, de twee sleutels in `SCHEMA` en `false` in `DEFAULTS`. Afdrukken als `true` of
`false` doet de CLI al (`_scalar`) *(r1-f14)*. Een niet-booleaanse waarde is een
configuratiefout (rc 2), zoals bij de andere types.

De guard leest de opties pas als hij iets te zeggen heeft (een geldig alarm of een geldige
pauzevlag), nooit op het snelle pad. Een configuratiefout bij dat lezen telt als "optie uit": de
basis spreekt dan, met één regel `usage-guard: config: <fout>` op stderr.

## De basis-guard

Het eerste argument kiest de stand. Voor elke stand eerst:

1. `LIGHTHOUSE_USAGE_REFRESH=1` → exit 0, niets gedaan. Zo zwijgt de guard in de kindsessie van
   een meting (`internal/usage/meter.go`).
2. Het alarm is `${XDG_CACHE_HOME:-$HOME/.cache}/lighthouse/usage-window.alarm`. Een ander pad
   leest de guard niet.

### Wanneer telt het alarm

Het alarm telt alleen als alle vier gelden:

- het is een gewoon bestand en geen symlink (`[ -f ]` en niet `[ -L ]`);
- de eerste regel heeft vier velden: `<soort> <pct> <reset_local> <reset_at>`;
- `soort` is `venster` of `week`, en `pct` is een geheel getal;
- `reset_at` is een tijd in de vorm `2006-01-02T15:04:05Z` en ligt in de toekomst.

In alle andere gevallen telt het als geen alarm. Daaronder vallen een `?` als `reset_at`, een
verlopen `reset_at` en een onleesbaar bestand. Op `reset_at` volgt dat `PruneAlarm` in
`internal/usage/alarm.go`, dat een alarm met minder dan vier velden of een verlopen of
onleesbare `reset_at` weggooit. Op `soort` en `pct` is de guard bewust strenger dan
`PruneAlarm`, dat die niet toetst; zo'n alarm blijft liggen tot het verloopt *(r1-f7)*. De guard
verwijdert het alarm zelf niet, want alleen de meter schrijft en ruimt.

`reset_at` wordt epoch via `date -d` (GNU). Faalt dat, dan via
`date -j -u -f '%Y-%m-%dT%H:%M:%SZ'` (BSD). Faalt dat ook, dan telt het alarm niet.

### De standen

| Stand | Event | Zonder alarm | Met alarm |
|---|---|---|---|
| `prompt` | `UserPromptSubmit` | start de verversing, exit 0 | start de verversing; `additionalContext` met soort, pct, reset en de afrondopdracht; nooit blokkerend |
| `tool` | `PostToolUse`, matcher leeg | één `[ -f ]`, exit 0 | één keer per venster `{"decision":"block"}`: commit het werk-in-uitvoering, schrijf een handoff, begin geen nieuwe stap |
| `subagent` | `SubagentStop`, en `PostToolUse` met `agent_id` | één `[ -f ]`, exit 0 | één keer per subagent `{"decision":"block"}` met een eigen reden: stop, meld de hoofdthread wat je deed en wat er nog open staat; commit niets, geen wekker, geen handoff |
| `stop` | `Stop` | exit 0 | één keer per venster `{"decision":"block"}`: commit niets meer, schrijf de handoff, stop; `stop_hook_active` → exit 0 |

- **Snelle pad.** In `tool` en `subagent` is het pad zonder alarm één `[ -f ]`, zonder fork en
  zonder stdin-lezing. Alleen een controller-run toetst eerst de pauzevlag (zie *Controller*), in
  deze volgorde: de env-check op `LIGHTHOUSE_CONTROLLER_RUN`, dan stap 1–3 van de vlag, en pas bij
  een geldige vlag de optie `controller` via `config.py`. Tot de vlag geldig is dus geen fork
  *(r1-f11)*.
- **Subagent-calls.** Een `PostToolUse` met `agent_id` in de payload komt uit een subagent. De
  guard ziet dat pas op het trage pad, na een geldig alarm, en behandelt de call dan als stand
  `subagent`. Zo bereikt de reden de subagent die de call deed, en blijft de `tool`-sleutel voor
  de hoofdthread *(eigen keuze na spec-review, r1-f8)*.
- **Alleen de hoofdthread parkeert.** De stand `subagent` geeft altijd de eigen subagent-reden
  uit de tabel, ook met `resume` of `controller` aan. Hij schrijft geen marker en vraagt geen
  wekker en geen handoff. Parallelle subagents delen een `session_id` en een worktree: zouden ze
  elk parkeren, dan racen ze om `CronList`, het handoff-pad en de git-index. Commit, marker,
  wekker en handoff komen alleen uit `tool` en `stop` van de hoofdthread *(eigen keuze na
  spec-review, r2-f1)*.
- **Eén keer per venster.** De sleutel is `<soort>-<reset_at>.<stand>.<session_id>.done`, een
  leeg bestand in `${XDG_STATE_HOME:-$HOME/.local/state}/lighthouse/usage-guard/`. Zo stopt elke
  sessie één keer, ook als meerdere sessies hetzelfde alarm zien. Zonder `session_id` in de
  payload vervalt dat deel. Een controller-run voegt `.<run-id>` toe; `subagent` voegt
  `.<agent_id>` toe, zodat elke subagent één keer stopt. De guard leest de payload alleen op het
  trage pad, dus de sessie-id kost het snelle pad niets *(eigen keuze na spec-review, r1-f1,
  r1-f6)*. `tool` en `subagent` hebben elk een eigen sleutel: de reden van een
  `subagent`-blokkade bereikt alleen de subagent.
- **Claimen.** `tool` en `subagent` claimen de sleutel exclusief (`set -C`, of `mkdir`) vóór ze
  spreken. Lukt de claim niet, dan zwijgt de call: een parallelle call van dezelfde sessie sprak
  al. Faalt daarna de module, dan haalt de guard de sleutel weer weg, zodat de volgende call
  opnieuw kan spreken. `stop` zet de sleutel pas nadat de blokkade is uitgezonden *(eigen keuze
  na spec-review, r1-f2)*. Sleutels ouder dan 14 dagen ruimt `stop` best-effort op.
- **Verversen.** Alleen in `prompt` start de guard een losgekoppelde
  `LIGHTHOUSE_USAGE_REFRESH=1 lighthouse usage refresh` (subshell, `&`, stdout en stderr dicht).
  Hij wacht er niet op. Dat gebeurt alleen als `command -v lighthouse` slaagt. Zonder `lighthouse`
  op `PATH` ververst hij niets en geeft hij geen fout. Een stempel is niet nodig: de meter houdt
  zijn eigen TTL en lock aan (`busy`).
- **Afronden.** Zonder optie `resume` noemt de reden `/lighthouse-handoff <pad>` als afrondstap,
  met hetzelfde pad als bij *Hervatten*. Ook de basis gebruikt de padvorm: die schrijft en
  toont alleen het blok, zonder de bijwerkingen van een fase-overgang (zie *Handoff naar een
  bestand*) *(eigen keuze na spec-review, r1-f3)*. Met `resume` neemt de module de stappen over
  (zie *Hervatten*).
- **Uitvoer.** De guard eindigt altijd met exit 0. Een fout in een module of in Python geeft geen
  blokkade en geen rode hook. Meldingen zijn Engels (AGENTS.md).

## De modules

Twee bestanden naast de guard. Ze worden mee geïnstalleerd onder `.claude/hooks/lighthouse/`, maar
niet geregistreerd. De guard laadt een module alleen als zijn optie aan staat.

De rolverdeling: **de guard beslist of er gesproken wordt, de module levert wat er gezegd en
geschreven wordt.** Per call komt er hooguit één JSON-object op stdout. Een controller-run met de
optie `controller` krijgt de reden van de controller-module, nooit die van de basis of van
hervatten. De enige uitzondering is de stand `subagent`: die geeft altijd de subagent-reden van
de basis *(r2-f1)*.

### Hervatten (`resume = true`)

Voor een gewone sessie, dus geen controller-run, of een controller-run met `controller = false`:

- **Resume-marker.** In de standen `tool` en `stop` schrijft de module één marker per
  sessie (zie *Contract*), atomair met tmp + rename. Zonder `session_id` in de payload komt er geen
  marker.
- **Wekker.** Alleen voor `venster`, nooit voor `week`. De module rekent de cron-expressie uit
  `reset_at` plus 2 minuten, in lokale tijd: `M H D Mon *`. De reden vraagt de agent die te zetten
  met `CronCreate`, `recurring false`, prompt = de eerste stap uit de handoff. Elke reden zegt er
  bij: zette je in dit venster al een wekker, check dan met `CronList` en neem die job-ID over in
  plaats van een tweede te zetten. Elke stand heeft een eigen sleutel, dus zonder die zin komen er
  twee of drie wekkers *(eigen keuze na spec-review, r1-f9)*.
- **Handoff.** De reden noemt `/lighthouse-handoff` met het pad
  `<resume-map>/<session_id>.handoff.md` als argument. Zonder `session_id` is de naam
  `no-session-<epoch>.handoff.md`. Bij `stop` vraagt ze ook te noteren wat er ongecommit staat.
- **Prompt.** De waarschuwing noemt dezelfde stappen. Een marker komt er dan niet: een
  waarschuwing parkeert niets.

### Controller (`controller = true`)

Alleen als `LIGHTHOUSE_CONTROLLER_RUN` gezet is en de vorm van een uuid heeft (`[0-9a-f-]+`).
Anders gedraagt de sessie zich als een gewone.

- **Alarm.** In `tool` en `stop` parkeert de module de run per sessie; `subagent` geeft de
  subagent-reden van de basis. Bij het parkeren schrijft de module een marker met `"controller": true` en geeft een reden: commit het werk-in-uitvoering, stop, geen
  nieuwe stap, geen subagent, geen wekker, geen handoff. De controller hervat na de reset. De
  sleutel draagt de run-id, zodat drie gelijktijdige runs elk één keer stoppen. Zonder
  `session_id` in de payload neemt de module de run-id als `session_id`: die is de sessie-uuid,
  en zonder marker hervat de controller de run nooit (zoals in paia) *(r1-f16)*.
- **Prompt.** Met een alarm geeft `prompt` een eigen waarschuwing: soort, pct en reset, en dat de
  run bij de volgende call parkeert. Geen wekker en geen handoff, want die spreken de latere
  blokkade tegen.
- **Pauze.** Alleen in `tool`, en ook zonder alarm. De pauzevlag is
  `${LIGHTHOUSE_CONTROLLER_STATE:-${XDG_STATE_HOME:-$HOME/.local/state}/lighthouse/controller}/PAUSE`.
  De toetsvolgorde (zonder Python tot de vlag geldig is):
  1. de vlag is een gewoon bestand en geen symlink;
  2. de eerste regel is een ISO-tijd met offset (`2026-10-03T21:00:00+02:00` of `…Z`), alleen de
     tekens `0-9T:+Z-`;
  3. de tijd ligt in de toekomst, via GNU `date -d` en anders BSD
     `date -j -f '%Y-%m-%dT%H:%M:%S%z'` (dubbele punt uit de offset, `Z` → `+0000`). Onleesbaar
     telt als verlopen;
  4. de sleutel `pause-<tot>.<run-id>.done` in de map van de sleutels (`usage-guard/`)
     ontbreekt, of is ouder dan de vlag *(r1-f6)*;
  5. de payload heeft geen `agent_id`. Een call binnen een subagent parkeert niet; de run parkeert
     bij de volgende call van de hoofdthread.

  Gelden alle vijf, dan schrijft de module de sleutel en een marker met `"window": "pause"` en
  `"pause": true`, en blokkeert ze met: commit het werk-in-uitvoering, stop, de controller hervat
  na `<tot>`.
- **Volgorde.** De pauzetoets gaat vóór de alarmtoets. Een ongeldige vlag (stap 1–3) valt door
  naar het alarmpad, zonder stdin te lezen. Faalt stap 4 of 5, of staat de optie `controller`
  uit, dan is stdin al gelezen: de guard valt door naar het alarmpad met die payload en leest
  stdin geen tweede keer *(r1-f12)*.

### Handoff naar een bestand

`lighthouse-handoff` zet nu één blok in de chat en schrijft geen bestand. In paia schreef
`/paia-pauzeer` het handoff-bestand waar de wekker en de poller op leunen. Besloten: de
lighthouse-skill krijgt die vorm, zodat `/paia-pauzeer` weg kan.

- **Aanroep.** `/lighthouse-handoff <pad>`, met een absoluut pad dat eindigt op `.handoff.md`. De
  skill schrijft hetzelfde blok naar dat pad, atomair (Bash: `mkdir -p`, tmp-bestand in dezelfde
  map, `mv`), en zet het ook in de chat. Het pad ligt buiten de repo; de skill noemt dat de
  schrijfactie daar buiten de sandbox kan vallen *(r1-f15)*.
- **Alleen schrijven en tonen.** De padvorm is een pauze, geen fase-overgang. Ze slaat over: het
  posten van het artefact (`post_artefact.py`, en dus geen push), de *Controller mode* (geen
  `🤖`-comment, geen boardstatus), elke vraag aan de gebruiker en de `/clear`-tip. Dat geldt ook
  als `LIGHTHOUSE_CONTROLLER_RUN` gezet is *(eigen keuze na spec-review, r1-f3)*.
- **Inhoud.** Het gewone blok (worktree, branch, doel, eerste stap) plus wat er ongecommit staat
  en het hervattingsmoment. Een wekker-job-ID, als de agent er een zette, komt er ook in.
- **Zonder pad** werkt de skill als nu.
- De skill heeft één bron, `pack/core/skills/handoff/SKILL.md`, die voor beide harnassen wordt
  geïnstalleerd *(r1-f15)*. De bestaande skillcontracttests dekken haar; de contracttest pint ook
  dat de padvorm de bijwerkingen hierboven overslaat.

## Contract

Het contract is wat de modules schrijven en wat een poller of controller leest. Tot M2 lezen paia's
`usage_resume.py`, `controller_tick.sh` en `decide.py` het; vanaf M2 de lighthouse-controller.
`docs/usage-guard.md` legt het vast.

| Wat | Pad of naam |
|---|---|
| alarm (lezen) | `${XDG_CACHE_HOME:-$HOME/.cache}/lighthouse/usage-window.alarm` |
| sleutels "één keer per venster" | `${XDG_STATE_HOME:-$HOME/.local/state}/lighthouse/usage-guard/<soort>-<reset_at>.<stand>[.<session_id>][.<run-id>\|.<agent_id>].done` en `…/usage-guard/pause-<tot>.<run-id>.done` |
| resume-markers | `${XDG_DATA_HOME:-$HOME/.local/share}/lighthouse/resume/<session_id>.json` |
| handoff | `${XDG_DATA_HOME:-$HOME/.local/share}/lighthouse/resume/<session_id>.handoff.md` |
| pauzevlag | `${LIGHTHOUSE_CONTROLLER_STATE:-${XDG_STATE_HOME:-$HOME/.local/state}/lighthouse/controller}/PAUSE` |
| controller-run | env `LIGHTHOUSE_CONTROLLER_RUN=<session-uuid>` |
| meting (kind) | env `LIGHTHOUSE_USAGE_REFRESH=1` |

Markervorm (JSON, één object; het voorbeeld is een hervat-marker):

```json
{
  "session_id": "…",
  "cwd": "/pad/naar/repo",
  "window": "venster",
  "reset_at": "2026-09-06T12:30:00Z",
  "handoff_path": "…/resume/<session_id>.handoff.md",
  "created_at": "2026-09-06T11:58:00Z",
  "source": "tool"
}
```

- `window` is `venster`, `week` of `pause`. `source` is `tool` of `stop`: de stand `subagent` schrijft geen marker
  *(r2-f1)*.
- `controller` en `pause` staan er alleen als ze `true` zijn.
- Welke velden er staan, per soort marker *(eigen keuze na spec-review, r1-f4)*:

  | Marker | `session_id`, `cwd`, `window`, `reset_at`, `created_at`, `source` | `handoff_path` | `controller` | `pause` |
  |---|---|---|---|---|
  | hervatten (gewone sessie) | ja | ja | nee | nee |
  | controller, alarm | ja | nee | ja | nee |
  | controller, pauze | ja | nee | ja | ja |

  Een controller-marker heeft geen `handoff_path`: de controller-reden vraagt geen handoff.
- Bij een pauze is `reset_at` het eindmoment uit de vlag.
- Zonder `cwd` in de payload: de cwd van de hook.

## Gevolgen voor paia (#1634)

De keuze voor de controller-tak (een AC van dit issue) komt in de beschrijving van #1634: **de tak
woont in lighthouse, als optie `controller` van de core-guard.** paia houdt geen eigen
usage-hook. Wat paia daarvoor aanpast, in #1634:

- `lighthouse.toml` krijgt `[usage_guard]` met `resume = true` en `controller = true`;
- `controller_launch.sh` zet `LIGHTHOUSE_CONTROLLER_RUN` in plaats van `PAIA_CONTROLLER_RUN`, en
  `LIGHTHOUSE_CONTROLLER_STATE` naar paia's eigen controller-map;
- `paia-controller pauze aan` schrijft `PAUSE` in plaats van `PAUZE`;
- `usage_resume.py` en `decide.py` lezen `~/.local/share/lighthouse/resume/` en de sleutels
  `window: "pause"` en `pause` (nu `pauze`);
- `.claude/hooks/usage-guard.sh` en zijn registraties gaan weg; `lighthouse init --upgrade`
  registreert de core-guard;
- de skill `/paia-pauzeer` kan weg zodra de guard naar `/lighthouse-handoff` verwijst.

De vaste volgorde uit de milestone blijft gelden: eerst de meter van `lighthouse` op een timer,
dan de guard op het nieuwe pad, dan de Python-schrijver eruit.

## Registratie en installatie

- `pack/core/hooks/hooks.json` krijgt `UserPromptSubmit` (`prompt`), een `PostToolUse`-blok met
  lege matcher (`tool`), `SubagentStop` (`subagent`) en `Stop` (`stop`).
- De modules staan in `pack/core/hooks/` of een submap ervan. `internal/packs` installeert elk
  `hooks/<rel>` onder `.claude/hooks/lighthouse/<rel>`. Of een submap werkt, toetst de bouwer.
- `pack/core/requires.toml`: `usage-guard` onder `python3` (alleen het trage pad) en onder
  `tomllib` (de opties via `config.py`).
- Codex en OpenCode draaien geen hooks (AGENTS.md); de guard is alleen voor Claude Code.

## Tests

Pytest in `tests/pack/`, met de helpers van `tests/pack/_testkit.py`. De guard draait vanaf het
geïnstalleerde pad, met een nep-`HOME`, `XDG_CACHE_HOME`, `XDG_STATE_HOME` en `XDG_DATA_HOME`.

Dat geldt voor elke test die de guard draait, ook `tests/pack/test_hooks_minimal_config.py`, dat
nu `os.environ` erft. Elke zo'n test zet `HOME` en de drie `XDG_*` naar tmp, haalt
`LIGHTHOUSE_CONTROLLER_RUN`, `LIGHTHOUSE_CONTROLLER_STATE` en `LIGHTHOUSE_USAGE_REFRESH` uit de
env en houdt de echte `lighthouse` van `PATH`. Anders schrijft de test-gate in een sessie met een
vol venster echte sleutels, en zwijgt de guard daarna voor die sessie *(eigen keuze na
spec-review, r1-f10)*.

- **Per stand** (AC): geen alarm, een geldig alarm, een verlopen alarm en een onleesbaar alarm
  (te weinig velden, `?` als `reset_at`, een symlink).
- **Snelle pad:** stubs voor `python3`, `jq` en `date` op `PATH` die een spoorbestand schrijven. In
  `tool` en `subagent` zonder alarm blijft het spoor leeg.
- **BSD `date`:** een stub `date` die `-d` weigert en `-j -u -f` vertaalt. Daarmee telt een verlopen
  alarm niet en een geldig wel.
- **Verversen:** een stub `lighthouse` die zijn argumenten en `LIGHTHOUSE_USAGE_REFRESH` vastlegt.
  Zonder `lighthouse` op `PATH`: exit 0, geen stderr.
- **`LIGHTHOUSE_USAGE_REFRESH=1`:** elke stand doet niets, ook met geldig alarm.
- **Eén keer per venster:** de tweede `tool`-call met hetzelfde alarm blokkeert niet; `subagent`
  heeft een eigen sleutel; `stop` met `stop_hook_active` blokkeert niet. Een tweede sessie (een
  andere `session_id`) blokkeert wel. Een `tool`-call met `agent_id` gebruikt de
  `subagent`-sleutel en laat de `tool`-sleutel vrij. Een module die faalt laat geen sleutel
  achter (r1-f1, r1-f2, r1-f8). Met `resume` en met `controller` geeft `subagent` de
  subagent-reden, zonder marker en zonder wekker (r2-f1).
- **Opties:** zonder tabel geen marker; met `resume` een marker met de vorm uit *Contract* en een
  cron alleen voor `venster`; met `controller` en een run-id alleen de controller-reden; de
  pauzetak met een geldige, verlopen en onleesbare vlag, en met `agent_id` in de payload.
- **`git grep -n -i paia pack/core/hooks/`** voor de guard en de modules geeft niets (AC).
- **`config.py`:** het booleaanse type, met een geldige en een ongeldige waarde.
- **`lighthouse-handoff`:** een contracttest dat beide varianten de vorm met een pad beschrijven,
  en dat de guard precies die aanroep noemt.
- Bestaande tellers die mee moeten: `internal/packs/core_test.go` (de lijst geïnstalleerde
  scripts), `tests/pack/test_hooks_minimal_config.py` (een payload per registratie),
  `tests/pack/test_shell_style.py` (shellcheck), `tests/pack/test_code_language.py`.

Verificatie: `uv run pytest -q && go test -shuffle=on ./...`, daarna
`go run ./cmd/lighthouse init --upgrade </dev/null` met als uitvoer `No changes.`

Niet bewezen: een echte BSD-`date` (CI draait op Ubuntu), de echte event-JSON van Claude, en een
echte losgekoppelde verversing.

## Dogfood

Lighthouse draait zijn eigen `pack/core`. Na de wijziging draait `go run ./cmd/lighthouse init
--upgrade`, en de geïnstalleerde guard en registraties worden gecommit. Lighthouse zelf zet
`[usage_guard]` niet aan: het heeft geen controller.

## Aan de bouwer

- De exacte Engelse formulering van de redenen en waarschuwingen, binnen de stappen hierboven.
- De bestandsnamen van de modules, en of ze gesourced of aangeroepen worden. Eis: één JSON-object
  per call, en het snelle pad laadt niets.
- Of Python of `jq` de payload leest op het trage pad.
- Hoe de cron uit `reset_at` komt (Python of `date`), zolang het in lokale tijd is.
- De volgorde van de commits en de verdeling over PR's.

## Mijn keuzes

Niet door de gebruiker besloten, wel door mij gemaakt:

- de tabel heet `[usage_guard]`, met `resume` en `controller`, standaard `false`;
- een booleaans type in `config.py`, in plaats van `0`/`1` als int;
- een configuratiefout bij het lezen van de opties telt als "optie uit";
- de guard verwijdert een verlopen alarm niet;
- een `?` als `reset_at` telt als geen alarm, gelijk aan `PruneAlarm`;
- verversen bij elke prompt zonder stempel; de meter houdt zijn TTL en lock aan;
- een controller-run zonder optie `controller` telt als een gewone sessie;
- bij een controller-run geeft `prompt` een eigen waarschuwing, zonder wekker en handoff;
- sleutels onder `XDG_STATE_HOME`, resume-markers onder `XDG_DATA_HOME`. De sleutels zijn een
  wijziging: paia houdt ze onder `XDG_DATA_HOME` *(r1-f13)*;
- Engelse namen in het contract (`PAUSE`, `pause`), ook waar paia nu Nederlands schrijft;
- het pad van de pauzevlag is te overschrijven met `LIGHTHOUSE_CONTROLLER_STATE`;
- de wekker op `reset_at` plus 2 minuten;
- de documentatie in een nieuw `docs/usage-guard.md`;
- de pauzevorm van `lighthouse-handoff` als argument (een pad), niet als aparte skill.
