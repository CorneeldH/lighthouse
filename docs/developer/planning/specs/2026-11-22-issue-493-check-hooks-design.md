# Check dat de geïnstalleerde hooks op de eigen machine draaien (#493)

Status: ontwerp, wacht op goedkeuring.
Milestone: M3 — Tweede omgeving. Bouwt voort op de meting van #90 (`ci/windows-measure.sh`).

## Waarom

De hooks zijn het vangnet van lighthouse. Bij een gebruiker die met `bypassPermissions` werkt,
zijn ze zelfs het enige vangnet. Een hook die niet draait, blokkeert niets, en niemand merkt het.

`windows-measure.yml` bewijst dat de hooks onder Git Bash *kunnen* draaien. Het bewijst niet dat
ze op de machine van een gebruiker *draaien*. Claude Code draait hooks op native Windows in Git
Bash als het Git Bash vindt, en anders in PowerShell. Onder PowerShell faalt een `sh`-hook.

De transcripten van de tweede gebruiker (native Windows, VS Code-extensie) tonen drie risico's:
Git Bash raakt zijn `PATH` kwijt, Python schrijft CRLF, en paden hebben spaties en schijfletters
in wisselende hoofdletters.

Uit #19: een instapniveau telt pas als geïnstalleerd als deze check groen is. De DoD van M3 vraagt
"met de hooks actief".

## Doelen

- Eén commando, `lighthouse check-hooks`, toont op de eigen machine of de geïnstalleerde hooks
  draaien. Elk rood resultaat noemt een herstelstap.
- De check draait vanzelf na `lighthouse init` en `lighthouse init --upgrade`.
- `init` voorkomt CRLF in de geïnstalleerde hookbestanden via `.gitattributes`.
- `init --upgrade` repareert een bestand dat alleen in CRLF verschilt, zonder `--force`.
- De spec legt vast of de registraties naar de exec-vorm gaan: nee, met reden (§Besluit).
- De installatiegids noemt de check en wat je doet als hij rood is.

## Niet-doelen

- De check draait vanuit de terminal, niet vanuit een Claude Code-sessie. Dat Git Bash *binnen*
  een sessie zijn `PATH` kwijtraakt, ziet hij niet. Een bewijs vanuit de sessie zelf, zoals een
  heartbeat, is een eigen issue als het nodig blijkt.
- De smoketest test de keten van shell, `run.sh` en binary, niet de logica van elke hook. Die
  logica testen de bestaande unit- en e2e-tests.
- Geen wijziging aan de registratievorm in `pack/core/hooks/hooks.json`.
- Hooks van anderen, zoals `entire`, vallen buiten de check. De check kijkt alleen naar
  registraties onder `.claude/hooks/lighthouse/`.
- Het macOS-deel van #90 (AC 1 en 2) blijft in #90. Wel draait dezelfde check op macOS.

## Anti-doelen

Mislukt, ook als het technisch werkt, als:

- de check groen meldt terwijl een hook niet draait. Een `COULD NOT CHECK` uit de
  fallback van `run.sh` is dus rood, geen groen.
- de smoketest iets verandert in de repo of de state van de gebruiker. Denk aan de stop-key van
  `usage-gate`, een `permission.json`, of een testgate-run van minuten.
- `init` faalt of een andere exit krijgt door de check. `init` heeft dan al geschreven.

## Besluit: shell-vorm houden, geen exec-vorm

Claude Code kent een exec-vorm: met `args` start `command` direct, zonder shell. Daarmee is Git
Bash niet meer nodig. Toch blijven de registraties in de shell-vorm (`run.sh <hook>`). Drie
redenen:

1. **Fail-closed.** `run.sh` blokkeert met exit 2 als de binary ontbreekt of de hook niet kent
   (de hooks in `blocking`). In de exec-vorm start dan het proces niet, en wat Claude Code daarna
   doet, valt buiten onze hand. Een ontbrekende binary wordt dan precies het stille falen dat dit
   issue wil vangen.
2. **Eén registratie voor elk platform.** `.claude/settings.json` is gecommit en gedeeld tussen
   Linux, macOS en Windows. Een exec-vorm vraagt een uitvoerbaar bestand dat op elk platform
   dezelfde naam heeft en op de `PATH` staat. Een `.cmd`-shim mag op Windows niet.
3. **De dev-binary.** In de eigen checkout kiest `run.sh` `.lighthouse/bin/lighthouse`, gebouwd
   uit `HEAD`. De exec-vorm verliest dat.

Wat de exec-vorm oplost, dekt de check af: hij meldt het wanneer Git Bash ontbreekt.

## Aanpakken

| Aanpak | Wat het kost | Gekozen |
|---|---|---|
| A. Shell-vorm houden, plus een check (Git Bash, CRLF, smoketest) | Git Bash blijft nodig op Windows | **ja** (Corneel, 2026-11-22) |
| B. Exec-vorm, `lighthouse hook <naam>` via `args` | Fail-closed moet naar Go of verdwijnt; dev-binary weg; registraties en `e2e/registrations_test.go` en `ci/windows-measure.sh` breken | nee |
| C. Per platform een andere registratie | `settings.json` is gedeeld tussen platforms, dus één machine schrijft de vorm van de andere stuk | nee |

## Ontwerp

### Commando

`lighthouse check-hooks`, los op te roepen in de root van een repo met lighthouse. De naam volgt
`check-instructions` *(mijn keus)*.

Uitvoer: één regel per stap en per hook, `ok` of `FAIL`, met bij `FAIL` de herstelstap. Exit:

| Exit | Betekenis |
|---|---|
| 0 | alles groen |
| 1 | minstens één `FAIL` |
| 2 | `COULD NOT CHECK`: geen repo-root, geen `.claude/settings.json`, of een `settings.json` die niet leest |

Dezelfde exitcodes als `check-instructions` *(mijn keus)*.

### Stap 1: Git Bash (alleen op Windows)

De check zoekt Git Bash in de volgorde van Claude Code:

1. `CLAUDE_CODE_GIT_BASH_PATH`. Gezet maar het bestand bestaat niet: `FAIL`, met dat pad.
2. Git for Windows via `git.exe` op de `PATH`: de `bash.exe` in de installatie daarvan.
3. De standaardpaden van Git for Windows.

Gevonden: `ok`, met het pad. Niet gevonden: `FAIL` met de tekst dat de hooks onder PowerShell
draaien en niets blokkeren. De herstelstap: installeer Git for Windows, of zet
`CLAUDE_CODE_GIT_BASH_PATH` op `bash.exe`, en start een nieuwe sessie.

De exacte zoekvolgorde van Claude Code staat niet in het deel van de docs dat gelezen is. De
bouwer verifieert die tegen de hooks-docs (sectie over Windows) en de docs over
omgevingsvariabelen. De Windows-CI meet stap 1 op een runner met Git for Windows.

Op Linux en macOS valt stap 1 weg: geen regel.

### Stap 2: CRLF

De check leest elk bestand onder `.claude/hooks/lighthouse/`, niet alleen `*.sh`. Een bestand met
`\r\n` is `FAIL`, met het pad.

Waarom alles: `run.sh` leest `blocking` met `grep -qxF`. Met CRLF in `blocking` matcht geen enkele
regel meer, en dan laat elke hook alles door. Dat is fail-open. Een `run.sh` met CRLF start niet
eens (`bad interpreter`), en dat telt voor Claude Code als een niet-blokkerende fout.

De herstelstap is `lighthouse init --upgrade`: die schrijft de bestanden opnieuw met LF (zie
§Reconcile) en zet `.gitattributes`, zodat het niet terugkomt. Een test bewijst dat op een
checkout met CRLF.

### Reconcile: regeleinden tellen niet als lokale wijziging

Zonder deze wijziging zit de gebruiker vast. Git checkt de bestanden op Windows met CRLF uit
(`core.autocrlf=true`, ook na `git worktree add`). `reconcile.decide`
(`internal/reconcile/reconcile.go`) vergelijkt bytes en de sha256 uit de manifest exact. Een
bestand dat alleen in regeleinden verschilt, wordt dan een `Conflict`, en `init --upgrade` weigert
met `rerun with --force`. Dat gebeurt vóór `.gitattributes` er staat en vóór de check draait.

Besluit (Corneel, 2026-11-22): reconcile vergelijkt de huidige inhoud na het normaliseren van
`\r\n` naar `\n`, zowel tegen de nieuwe inhoud als tegen de sha256 uit de manifest. Een bestand
dat alleen in CRLF verschilt van wat lighthouse schreef, telt als niet aangeraakt. `--upgrade`
schrijft het dan opnieuw, met LF. Dat geldt voor elk bestand dat `init` beheert, niet alleen
voor de hooks.

Regels:

- Alleen `\r\n` naar `\n`. Een losse `\r` of een ander verschil blijft een lokale wijziging.
- Een bestand dat alleen in CRLF verschilt van de *nieuwe* inhoud, is niet `same`. `init` schrijft
  het met LF en toont het in de diff, zodat de reparatie zichtbaar is.
- De manifest bewaart de sha256 van de inhoud met LF, zoals nu.
- Een bestand zonder manifest-entry (eerste `init`) dat alleen in CRLF verschilt van de nieuwe
  inhoud, wordt `Adopt` in plaats van `Conflict`, ook zonder `--force` *(eigen keuze na
  spec-review, r1-f2)*.
- Beheerde blokken in een gedeeld bestand (zoals in `AGENTS.md`) volgen al dezelfde regel:
  `agentsmd.Find` normaliseert CRLF (`internal/agentsmd/agentsmd.go`) en `Set` schrijft het blok
  met de regeleinden van het document. De bouwer past daar niets aan.

### `.gitattributes` door `init`

`init` zorgt dat `.gitattributes` deze regels heeft:

```gitattributes
*.sh eol=lf
.claude/hooks/lighthouse/** eol=lf
```

Regels:

- De wijziging komt in de diff van `init`, onder dezelfde ene goedkeuring als de rest.
- `init` voegt alleen ontbrekende regels toe, achteraan. Bestaande regels van het project blijven
  staan. Een tweede run geeft `No changes.`
- De regel voor `.claude/hooks/lighthouse/**` komt er altijd: hij raakt alleen bestanden van
  lighthouse. Staat hij al eerder in het bestand, dan blijft hij staan; staat er daarna een
  projectregel die hem overschrijft, dan wint die en meldt `init` dat niet *(eigen keuze na
  spec-review, r1-f3)*. De CRLF-check van stap 2 blijft het vangnet.
- De regel `*.sh eol=lf` komt er alleen als het project nog geen regel met precies het patroon
  `*.sh` heeft. Het project houdt zo de baas over zijn eigen scripts. *(mijn keus)*
- Losse regels, geen beheerd blok: de HTML-commentaarmarkers van `agentsmd` zijn in
  `.gitattributes` geen geldig commentaar. `init` verwijdert de regels niet, ook niet bij
  `--no-pack` *(eigen keuze na spec-review, r1-f3)*.

### Stap 2b: registraties

Een hook die niet geregistreerd of uitgezet is, draait ook niet. De check meldt `FAIL` als
*(mijn keus)*:

- een hook-unit uit `.lighthouse/manifest.toml` geen registratie meer heeft in
  `.claude/settings.json`. Herstelstap: `lighthouse init --upgrade`.
- `disableAllHooks: true` staat in `.claude/settings.json`, `.claude/settings.local.json` of
  `~/.claude/settings.json`. Herstelstap: die regel weghalen. Managed settings leest de check niet.

### Stap 3: smoketest

De check leest de registraties uit `.claude/settings.json` met `settings.Registrations`. Hij neemt
alleen de registraties waarvan het commando een bestand onder `.claude/hooks/lighthouse/` noemt,
ook die van de taalpakketten (`gofmt-check.sh`, `go-lint-gate.sh` enzovoort).

Per registratie draait hij het commando één keer:

- via `bash -c "<command>"`, zoals Claude Code dat doet. Op Windows is dat de `bash.exe` van stap
  1. Zonder Git Bash slaat de check stap 3 over, met één regel die naar stap 1 verwijst.
- met `CLAUDE_PROJECT_DIR` op de repo-root en cwd op de repo-root.
- met de timeout van de registratie.
- met een payload per event die zo is gekozen dat een gezonde hook niets doet en niets schrijft:

| Event | Payload *(mijn keus, de bouwer verifieert per hook)* |
|---|---|
| `PreToolUse`, `PostToolUse`, `PostToolUseFailure` met matcher `Bash` | `tool_input.command = "true"` |
| idem met matcher `Edit\|Write` | `tool_input.file_path` op een niet-bestaand pad zonder `.md` |
| `PostToolUse` zonder matcher, `Stop`, `SubagentStop`, `UserPromptSubmit` | `transcript_path` op een bestand in een tempdir met één prompt-entry met `timestamp` en zonder tool-use *(eigen keuze na spec-review, r1-f1)* |
| `Notification` | `notification_type = "permission_prompt"`, buiten een controller-run |

- met de state in een tempdir. De check zet `LIGHTHOUSE_STATE_DIR`, `XDG_RUNTIME_DIR` en de andere
  paden die hooks voor state gebruiken naar een tempdir, en `LIGHTHOUSE_USAGE_REFRESH=1`. Met die
  variabele doet `usage-gate` niets (`docs/usage-gate.md`): geen meter-refresh en geen stop-key.
  De smoketest bewijst voor `usage-gate` dus alleen de keten, wat past bij de niet-doelen. Welke
  variabelen verder nodig zijn, zoekt de bouwer uit.
  Een test bewijst dat de echte state na de check onveranderd is.
- zonder `LIGHTHOUSE_CONTROLLER_RUN`.

Oordeel per registratie:

- Exit 0 of 2, en geen `COULD NOT CHECK` in stdout of stderr: `ok`.
- Al het andere is `FAIL`: een andere exit, een timeout, of `COULD NOT CHECK`. De regel noemt de
  hook en de eerste niet-lege regel van stderr (of stdout als stderr leeg is).

Een leeg transcript volstaat niet: `internal/turnroots/transcript.go` geeft dan `Undetermined` ("no prompt in the transcript") en de Stop-hooks van de taalpakketten (`go-lint-gate`, `go-imports-gate`, `node-imports-gate`) melden `COULD NOT CHECK`. Met één prompt en zonder tool-use meldt turn-roots `untouched` en geven die gates `not checked` met exit 0.

Een `COULD NOT CHECK` van een hook die bij deze payload terecht niet kan meten, is een fout in de
payload, niet in de hook. De bouwer kiest de payloads zo dat elke hook uit de pakketten op Linux
`ok` geeft. Een test pint dat.

De logica komt uit `ci/windows-measure.sh` (`hook` en `ok`), geport naar Go.

### Na `init` en `init --upgrade`

`init` draait de check na elke run waarin de hooks er staan: na schrijven, en ook bij
`No changes.`. Niet als de gebruiker de diff afwijst.

- De uitvoer gaat naar stderr, zodat de stdout van `init` (`No changes.`, waar de dogfood-CI op
  let) gelijk blijft *(mijn keus)*. Dat wijkt af van `auditAfterInit`, dat naar stdout schrijft.
- De exit van `init` blijft gelijk, net als bij `auditAfterInit`. Een rode check geeft een regel
  dat `lighthouse check-hooks` de details geeft.
- De aanroep krijgt een injecteerbare variabele, zoals `auditSummary`, zodat tests van `init` de
  hooks niet echt draaien.

In de eigen checkout (`version = "dev"`) start `run.sh` bij elke hook-aanroep een losgekoppelde
`go build` naar `.lighthouse/bin` als de stamp niet `HEAD` is (`pack/core/hooks/run.sh`). Die build
draait door na de check en schrijft in de repo. Dat is een bekende uitzondering op het anti-doel
"state onveranderd" en geldt alleen voor `dev`; de test voor onveranderde state draait met een
niet-dev-binary *(eigen keuze na spec-review, r1-f4)*. Een rode
check in de dogfood-CI faalt de CI niet, want de exit van `init` blijft gelijk. De bouwer kijkt of
de check daar groen is, en noteert het als hij dat niet is.

### Windows-CI

`ci/windows-measure.sh` krijgt één rij: `lighthouse check-hooks` in de repo van de meting,
verwacht rc 0 *(mijn keus)*. Zo meet de Windows-runner stap 1 en de smoketest onder Git Bash, met
een Windows-`CLAUDE_PROJECT_DIR`.

### Gids

`docs/install.md` noemt `lighthouse check-hooks`: wanneer hij draait, wat elke `FAIL` betekent en
de herstelstap. De zin "Windows has a binary, but the hooks need WSL" gaat eruit zodra stap 1 en
de smoketest op de Windows-runner groen zijn. Zo ook de passage over native Windows in
`docs/site/agents/install.md`. Is de runner niet groen, dan blijven beide zinnen staan en noemt de
gids de check als manier om het te meten.

## Wat aan de bouwer is

- De Go-package van de check (nieuw of naast `internal/hook`) en de cobra-registratie.
- Het commando voor de CRLF-herstelstap.
- Het mechanisme voor `.gitattributes` in `init`.
- De exacte payloads en state-variabelen, binnen de regels hierboven.
- Hoe de paden van Windows (`C:\…`, `D:` naast `d:`, spaties) in `CLAUDE_PROJECT_DIR` en
  `bash -c` terechtkomen. De Windows-CI meet het.

## Regels uit de repo

- Dogfood: een wijziging aan `init` draait `go run ./cmd/lighthouse init --upgrade` en commit de
  geïnstalleerde kopie. CI wil `No changes.`
- `changelog.d/` krijgt een fragment.
- Uitvoer van het commando is Engels (instructies en hookteksten zijn Engels); deze spec en de PR
  zijn Nederlands.
- Bewijs: `go test ./...`, `go test -tags e2e ./e2e/`, `lighthouse imports check`, en een run van
  `windows-measure.yml` via `workflow_dispatch` met de nieuwe rij.
