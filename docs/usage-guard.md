# Usage-guard

De usage-guard bewaakt het Claude-venster in een lopende sessie. Hij leest alleen het alarm van
`lighthouse usage` (zie [usage-venster.md](usage-venster.md)), nooit de cache, en roept nooit
`claude` aan. Met een alarm waarschuwt hij bij een prompt, onderbreekt hij een taak die midden in
een vol venster doorloopt en dwingt hij bij `Stop` een afronding af. De drempels staan in
`host.toml` en gelden in de meter; de guard kent er zelf geen.

`pack/core/hooks/usage-guard.sh` is de enige geregistreerde hook. Hij eindigt altijd met exitcode 0
en zet hooguit één JSON-object op stdout. Een fout geeft geen blokkade en geen rode hook, alleen
een regel op stderr die met `usage-guard: ` begint.

## De standen

Het eerste argument kiest de stand. Met `LIGHTHOUSE_USAGE_REFRESH=1` in de omgeving (de kindsessie
van een meting) doet elke stand niets.

| Stand | Event | Zonder alarm | Met alarm |
|---|---|---|---|
| `prompt` | `UserPromptSubmit` | start de verversing, exit 0 | start de verversing; `additionalContext` met soort, pct, reset en de afrondopdracht; nooit blokkerend |
| `tool` | `PostToolUse`, matcher leeg | één `[ -f ]`, exit 0 | één keer per venster `{"decision":"block"}`: commit het werk-in-uitvoering, schrijf een handoff, begin geen nieuwe stap |
| `subagent` | `SubagentStop`, en `PostToolUse` met `agent_id` | één `[ -f ]`, exit 0 | één keer per subagent `{"decision":"block"}` met een eigen reden: stop, meld de hoofdthread wat je deed en wat er nog open staat; commit niets, geen wekker, geen handoff |
| `stop` | `Stop` | exit 0 | één keer per venster `{"decision":"block"}`: commit niets meer, schrijf de handoff, stop |

- **Wanneer telt het alarm.** Het alarm is `${XDG_CACHE_HOME:-$HOME/.cache}/lighthouse/usage-window.alarm`.
  Het telt alleen als het een gewoon bestand is (geen symlink), de eerste regel precies vier velden
  heeft (`<soort> <pct> <reset_local> <reset_at>`), `soort` `venster` of `week` is, `pct` alleen
  cijfers bevat en `reset_at` de vorm `2006-01-02T15:04:05Z` heeft en in de toekomst ligt. Een
  vijfde veld, een `\r` aan het eind, een `?` als `reset_at`, een verlopen tijd of een onleesbaar
  bestand telt als geen alarm. De guard verwijdert het alarm nooit: alleen de meter schrijft en
  ruimt. `_` in `reset_local` wordt een spatie.
- **Tijd.** `reset_at` wordt epoch via GNU `date -d`, en anders via BSD `date -j -u -f`. Faalt
  beide, dan telt het alarm niet.
- **Snelle pad.** In `tool` en `subagent` is het pad zonder alarm één `[ -f ]`, zonder fork en
  zonder stdin te lezen. Alleen een controller-run toetst eerst de pauzevlag. Pas bij een geldig
  alarm of een geldige vlag start `python3`; de payload gaat dan op stdin mee.
- **Subagent-calls.** Een `PostToolUse` met `agent_id` komt uit een subagent. De guard ziet dat pas
  op het trage pad en behandelt de call dan als stand `subagent`. De `tool`-sleutel blijft zo voor
  de hoofdthread.
- **Alleen de hoofdthread parkeert.** `subagent` geeft altijd de eigen subagent-reden, ook met
  `resume` of `controller` aan, en schrijft nooit een marker. Marker, wekker en handoff komen
  alleen uit `tool` en `stop`.
- **Eén keer per venster.** Zie *Sleutels*. `tool` en `subagent` claimen de sleutel exclusief
  vóór ze spreken; lukt dat niet, dan zwijgt de call. Faalt daarna de module, of geeft ze iets
  anders terug dan een JSON-object, dan haalt de guard de claim weg en zwijgt hij: de volgende
  call kan opnieuw spreken. `stop` zet de sleutel pas nadat de blokkade is uitgezonden.
- **`stop` zwijgt** bij `stop_hook_active`, bij een onleesbare payload (`not json`, `[]`) en
  als de sleutelmap niet te maken is (één regel `usage-guard: ` op stderr). Anders blokkeerde hij
  bij elke beurt opnieuw. `tool` zonder leesbare payload blokkeert wel, zonder sessiedeel in de
  sleutel.
- **Verversen.** Alleen in `prompt`, en alleen als `lighthouse` op `PATH` staat, start de guard een
  losgekoppelde `LIGHTHOUSE_USAGE_REFRESH=1 lighthouse usage refresh` met stdin, stdout en stderr
  naar `/dev/null`. Hij wacht er niet op; de meter houdt zijn eigen TTL en lock aan.
- **Afronden.** Zonder optie `resume` noemt de reden `/lighthouse-handoff <pad>` als afrondstap,
  met het pad `<resume-map>/<session_id>.handoff.md` (zonder `session_id`:
  `no-session-<epoch>.handoff.md`). Dat schrijft en toont alleen het blok. Met `resume` neemt de
  module de stappen over.

## Opties

In `lighthouse.toml`, per repo:

```toml
[usage_guard]
resume = false      # hervatten: resume-marker en wekker voor een gewone sessie
controller = false  # controller-tak: een controller-run parkeren, ook bij een pauze
```

| Sleutel | Standaard | Effect |
|---|---|---|
| `resume` | `false` | in `tool` en `stop` een resume-marker; voor `venster` ook een wekker via `CronCreate` |
| `controller` | `false` | een controller-run parkeert met een marker, en ook bij de pauzevlag |

Zonder tabel draait alleen de basis. Een waarde die geen boolean is, is een configuratiefout (rc 2
in `lighthouse config`). De guard leest de opties pas als hij iets te zeggen heeft; een
configuratiefout daarbij telt als "beide uit", met één regel `usage-guard: config: <fout>` op
stderr. Lighthouse zelf zet ze niet aan: het heeft geen controller.

### Hervatten (`resume = true`)

Voor een gewone sessie, en voor een controller-run met `controller = false`:

- **Marker.** In `tool` en `stop` één marker per sessie, atomair (tmp + rename). Zonder
  `session_id` in de payload komt er geen marker. `prompt` schrijft er geen: een waarschuwing
  parkeert niets.
- **Wekker.** Alleen voor `venster`, nooit voor `week`. De reden vraagt `CronCreate` met de
  cron-expressie `M H D Mon *` van `reset_at` plus 2 minuten in lokale tijd, `recurring false`,
  prompt = de eerste stap uit de handoff. Staat er al een wekker in dit venster, dan zegt de reden
  die via `CronList` over te nemen in plaats van een tweede te zetten.
- **Handoff.** De reden noemt `/lighthouse-handoff` met het handoff-pad als argument en vraagt
  de job-ID van de wekker erin te noteren (bij `venster`).

### Controller (`controller = true`)

Alleen als `LIGHTHOUSE_CONTROLLER_RUN` gezet is en volledig overeenkomt met `[0-9a-f-]+`. Anders
gedraagt de sessie zich als een gewone.

- **Alarm.** In `tool` en `stop` parkeert de module de run: een marker met `"controller": true` en
  een reden die zegt: commit het werk-in-uitvoering, stop, geen nieuwe stap, geen subagent, geen
  wekker, geen handoff. De controller hervat na de reset. Zonder `session_id` in de payload is
  de run-id de `session_id`. `prompt` waarschuwt dat de run bij de volgende call parkeert.
- **Pauze.** Alleen in `tool`, en ook zonder alarm; zie *De pauzevlag*.

## Contract

Dit is wat de modules schrijven en wat een poller of controller leest.

| Wat | Pad of naam |
|---|---|
| alarm (lezen) | `${XDG_CACHE_HOME:-$HOME/.cache}/lighthouse/usage-window.alarm` |
| sleutels "één keer per venster" | `${XDG_STATE_HOME:-$HOME/.local/state}/lighthouse/usage-guard/` |
| resume-markers | `${XDG_DATA_HOME:-$HOME/.local/share}/lighthouse/resume/<session_id>.json` |
| handoff | `${XDG_DATA_HOME:-$HOME/.local/share}/lighthouse/resume/<session_id>.handoff.md` |
| pauzevlag | `${LIGHTHOUSE_CONTROLLER_STATE:-${XDG_STATE_HOME:-$HOME/.local/state}/lighthouse/controller}/PAUSE` |
| controller-run | env `LIGHTHOUSE_CONTROLLER_RUN=<session-uuid>` |
| meting (kind) | env `LIGHTHOUSE_USAGE_REFRESH=1` |

Een `session_id` of `agent_id` die niet past op `[A-Za-z0-9_-]{1,128}` (`../x`, `a/b`, 300 tekens)
telt als afwezig: de waarden belanden in bestandsnamen.

### Sleutels

Een leeg bestand `<soort>-<reset_at>.<stand>[.<session_id>][.<run-id>|.<agent_id>].done`, en voor
de pauze `pause-<tot>.<run-id>.done`, in de sleutelmap. `<stand>` is `tool`, `subagent` of
`stop`. Zonder `session_id` vervalt dat deel. Een controller-run voegt `.<run-id>` toe,
`subagent` voegt `.<agent_id>` toe. `stop` ruimt sleutels ouder dan 14 dagen op, best-effort. De
lib schrijft de sleutels, ook die van de pauze; geen module doet dat.

### Marker

Eén JSON-object per sessie, atomair geschreven:

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

- `window` is `venster`, `week` of `pause`. `source` is `tool` of `stop`: de stand `subagent`
  schrijft nooit een marker.
- `cwd` is de `cwd` uit de payload, anders de cwd van de hook. `created_at` is UTC.
- `controller` en `pause` staan er alleen als ze `true` zijn.
- Bij een pauze is `reset_at` het eindmoment uit de vlag.

| Marker | `session_id`, `cwd`, `window`, `reset_at`, `created_at`, `source` | `handoff_path` | `controller` | `pause` |
|---|---|---|---|---|
| hervatten (gewone sessie) | ja | ja | nee | nee |
| controller, alarm | ja | nee | ja | nee |
| controller, pauze | ja | nee | ja | ja |

Een controller-marker heeft geen `handoff_path`: de controller-reden vraagt geen handoff.

## De pauzevlag

De controller kan een run laten parkeren zonder alarm, met het bestand `PAUSE` (pad in *Contract*).
Alleen `tool` toetst het, alleen in een controller-run, in deze volgorde. De stappen 1 tot en met
3 draaien in shell, zonder Python:

1. de vlag is een gewoon bestand en geen symlink;
2. de eerste regel is een ISO-tijd met offset (`2026-10-03T21:00:00+02:00` of `…Z`), alleen de
   tekens `0-9T:+Z-`;
3. de tijd ligt in de toekomst (GNU `date -d`, anders BSD `date -j -f`); onleesbaar telt als
   verlopen;
4. de sleutel `pause-<tot>.<run-id>.done` ontbreekt, of is ouder dan de vlag;
5. de payload heeft geen `agent_id`: een call in een subagent parkeert niet, de run parkeert bij de
   volgende call van de hoofdthread.

Gelden alle vijf en staat `controller` aan, dan schrijft de lib de sleutel, schrijft de module een
marker met `"window": "pause"` en `"pause": true`, en blokkeert ze: commit het werk-in-uitvoering,
stop, de controller hervat na `<tot>`. De pauzetoets gaat vóór de alarmtoets. Een ongeldige vlag
(stap 1 tot en met 3) valt door naar het alarmpad; faalt stap 4 of 5, of staat `controller` uit,
dan valt de guard met dezelfde payload door naar het alarmpad.

Gelden een geldige vlag en een geldig alarm tegelijk, dan blijft er één marker per sessie: de
module voegt samen met de bestaande marker en het latere hervatmoment (`reset_at`) wint, met
zijn `window` en zijn `pause`-veld. `cwd`, `created_at` en `source` volgen de laatste schrijver.
De reden noemt het winnende hervatmoment. Een onleesbare of ongeldige marker wordt overschreven.

## Omgevingsvariabelen

| Variabele | Betekenis |
|---|---|
| `LIGHTHOUSE_USAGE_REFRESH=1` | kindsessie van een meting: de guard zwijgt |
| `LIGHTHOUSE_CONTROLLER_RUN` | run-id van een controller-run; telt alleen bij een volle match op `[0-9a-f-]+` |
| `LIGHTHOUSE_CONTROLLER_STATE` | map van de pauzevlag; anders `${XDG_STATE_HOME:-$HOME/.local/state}/lighthouse/controller` |
| `XDG_CACHE_HOME`, `XDG_STATE_HOME`, `XDG_DATA_HOME` | verplaatsen alarm, sleutels en markers; een lege waarde telt als niet gezet |

## Handoff naar een bestand

`/lighthouse-handoff <pad>`, met een absoluut pad dat eindigt op `.handoff.md`, schrijft het blok
atomair naar dat pad en toont het ook in de chat. Het is een pauze en geen fase-overgang: het slaat
het posten van het artefact, de controller-modus, elke vraag en de `/clear`-tip over, ook met
`LIGHTHOUSE_CONTROLLER_RUN` gezet. Zonder pad werkt de skill als altijd.

## Grenzen

- Er is geen poller in lighthouse in M1: het afhandelen van markers en het hervatten van een
  geparkeerde run is een taak van de controller.
- Andere blokkerende Stop-hooks (`go-check.sh`) zwijgen niet bij een alarm; zie #74.
