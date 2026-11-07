# Triage

`lighthouse triage` routeert de kandidaat-issues van een repo vooraf. Per issue doet het één
read-only `claude -p`-oordeel, schrijft het een triage-comment met de routeregel, en zet het een
issue dat de dispatcher zonder gebruiker kan dragen op board-Status `Controller`. Zo start de
[tick](tick.md) het zonder dat iemand het eerst oppakt. Het ontwerp staat in
[de triage-spec](developer/planning/specs/2026-11-01-triage-design.md).

## Wat triage doet

De ronde heeft drie stappen. Go verzamelt, `claude` oordeelt, Go past toe. Het model wijzigt zelf
niets: het heeft alleen `Read`, `Grep` en `Glob`.

1. **Verzamelen.** Per repo met `[controller.triage]` de kandidaten, en per issue de vaste feiten:
   labels, `blocked_by`, de `git log` van `origin/<default_branch>`, de andere issues in dezelfde
   milestone en de skillcatalogus.
2. **Oordelen.** Eén `claude -p` per kandidaat, met een harde timeout van 300 seconden. Het draait
   in een tijdelijke map met de getrackte inhoud van `origin/<default_branch>`, die na de ronde
   verdwijnt. Het model leest alleen die map: geen ongetrackte of genegeerde bestanden, geen
   `.git`, geen worktrees. Van de gebruikersinstellingen (`~/.claude/settings.json`, of
   `$CLAUDE_CONFIG_DIR/settings.json` als die gezet is) neemt het alleen `env` en `modelSettings`
   over. Een inlog die alleen in die instellingen staat, zoals `apiKeyHelper`, bereikt het oordeel
   dus niet. Elke export laat bovendien een lege map `~/.claude/projects/<export>/memory/` achter,
   ongeveer één per repo per dag met de timer van 18:00; die mappen zijn onschuldig en mogen weg.
   De ronde slaat een repo over als de export mislukt, als `origin/<default_branch>` geen gewoon
   bestand `.claude/skills/lighthouse-route-issue/SKILL.md` heeft (of dat buiten de export
   uitkomt), als de skillcatalogus een fout geeft, of als het instellingenbestand onleesbaar of
   ongeldig is. Een timeout of een oordeel dat niet klopt met het contract telt als ongeldig:
   geen actie op dat issue, een foutregel op stderr, en het issue komt de volgende ronde terug.
3. **Toepassen.** Vlak voor de eerste schrijfactie leest de ronde het issue opnieuw. Is er iets
   veranderd, dan slaat ze het over zonder één schrijfactie. Anders schrijft ze de triage-comment,
   dan eventueel de `🤖 Handoff`, dan de status.

Een issue op poort 4a, 4b of 2 gaat naar `Controller`, ook met `needs-hands` of `needs-testing`.
Poort 0 en 1 houden hun status en krijgen alleen de comment. Poort 4c krijgt de comment met
ontwerpvragen, elk met opties en een aanbevolen optie. Een vermoedelijk dubbel of al afgerond
issue wordt gemeld, nooit gesloten of doorgezet. Al afgerond is ook werk dat een commit onder een
ander issuenummer leverde, zonder dit issue te noemen: het model weegt de code die het zelf leest,
niet alleen de git log van dit nummer. Triage zet geen labels en legt geen
`blocked_by`-relaties.

Vrije tekst van het model komt op één regel in de comment, zonder codefence, `<sub>` of `<!--`.
Dat geldt voor de routeregel na `#<nr> Poort <n>:`, het bewijs bij een dubbel of afgerond issue,
de reden voor interactief en de ontwerpvragen. Elk veld heeft een grens in tekens, geteld zonder
spaties aan begin en eind: de route 80, de reden na de route 200, de reden voor interactief 300,
een ontwerpvraag 300, een optie 150. De ronde toetst alleen de velden die het oordeel in de
comment zet. Een veld dat bij deze uitkomst niet in de comment komt, mag dus langer zijn.

`redundancy_evidence` bevat alleen verwijzingen, hoogstens 5 en hoogstens 220 tekens. Een
verwijzing is `#` met 1 tot 6 cijfers, of een commit-sha van 7 tot 40 hexadecimale tekens die het
begin is van een sha in de log. Tussen twee verwijzingen staat precies `, ` of een spatie. Een sha
komt in kleine letters in de comment. Een overschrijding, of proza in dat veld, maakt het oordeel
ongeldig: geen actie, een foutregel `invalid judgment`, en het issue komt terug.

Na de `@` of `#` van een mention of
issueverwijzing in die tekst staat een zero-width joiner. Zo notificeert `@naam` niemand, en maken
`#N` en `owner/repo#N` geen kruisverwijzing. De verwijzingen die Go zelf schrijft, de
blokkeerders van poort 0 en het issue achter `Mogelijk dubbel van`, blijven klikbaar.

## Het commando

```bash
lighthouse triage [--dry-run] [--issue <owner>/<naam>#<nr>]... [--db <pad>]
```

- `--dry-run` doet de oordelen, maar schrijft niets. De routeregels gaan naar stdout. Gebruik het
  om de prompt te ijken, vóór de eerste timer: draai het met minstens één issue per uitkomst
  (poort 0, 1, 2, 4a, 4b, 4c) en leg de regels naast `/lighthouse-route-issue` op dezelfde
  issues.
- `--issue` mag alleen samen met `--dry-run` en mag vaker voorkomen. Het beoordeelt precies die
  issues, ook als ze buiten het kandidaatfilter vallen. Staat de repo er niet met
  `[controller.triage]` in `[dispatch] repos`, dan is dat een foutregel en exitcode 2.
- Het commando loopt over de repo's in `[dispatch] repos` van `host.toml` die `[controller.triage]`
  hebben. Een repo die er niet in staat, bestaat voor de ronde niet.
- Het vereist `GH_TOKEN` of `GITHUB_TOKEN`, zoals de tick.
- Een eigen `triage.lock` (`${XDG_RUNTIME_DIR}/lighthouse/triage.lock`) voorkomt twee rondes
  tegelijk. De ronde neemt `tick.lock` niet: de tick draait door.

Per repo schrijft het commando één samenvattingsregel naar stderr:

```text
lighthouse: triage <owner/naam>: candidates 6, judgments 5, forwarded 2, design 1, other 2, invalid 0, running work 1, skipped 0, errors 0
```

Elke stap die faalt krijgt een regel `lighthouse: triage <repo>#<nr>: <stap>: <fout>`. Faalt een
stap halverwege, dan stopt dat issue en gaat de ronde door met het volgende.

### Exitcodes

| Code | Wanneer |
|---|---|
| 0 | De ronde draaide. Dat geldt ook bij nul kandidaten, en bij een ronde die halverwege stopt omdat de rem sloot (`lighthouse: triage stopped: <reden>`). Wat al geschreven is, blijft staan. |
| 1 | De ronde is overgeslagen: geen repo met `[controller.triage]`, de rem of de pauzevlag stond aan bij de start (`lighthouse: triage skipped: <reden>`), of een vorige ronde draait nog (`lighthouse: previous triage still running`). |
| 2 | Fout: een repo die niet aan de voorwaarden voldoet, een ongeldig `[controller.triage]`, een issue waarvan een stap faalde, een ongeldige `host.toml`, een ontbrekend token, een onbekende vlag of een los argument, of een paniek. De unit komt dan op `failed`. |

Een ongeldig oordeel is geen fout en geeft geen `2`.

## `[controller.triage]`

De tabel staat in het `lighthouse.toml` van de repo. Staat ze er, dan staat triage aan voor die
repo. `lighthouse init` schrijft haar niet.

```toml
[controller.triage]
milestones_only = true
wait_for_night = true
max_forwarded = 5
max_judgments = 15
milestone_title_exclude = "(ON-HOLD|TO-?DO|DONE)$"
```

| Sleutel | Standaard | Doel |
|---|---|---|
| `milestones_only` | `true` | Alleen issues met een open milestone zijn kandidaat. Met `false` doet elk open issue mee. |
| `wait_for_night` | `true` | Het correctievenster. Met `true` schrijft de ronde bij een doorgezet issue een `🤖 Handoff` met `when: night`, zodat de tick het pas start als het `night`-venster van `[dispatch]` begint. Met `false` zet ze alleen de status, en start de eerstvolgende tick het issue meteen. |
| `max_forwarded` | `5` | Na zoveel doorgezette issues stopt de ronde voor die repo. De rest krijgt geen marker en komt de volgende ronde aan de beurt. |
| `max_judgments` | `15` | Hoogstens zoveel oordelen per ronde en per repo, om de usage te begrenzen. Een issue met een gebroken invariant kost geen oordeel en krijgt ook na die grens zijn comment. |
| `milestone_title_exclude` | leeg | Een Go-RE2-regex, getoetst tegen de titel van de milestone. Past hij, dan is het issue geen kandidaat, ook met `milestones_only = false`. Hoofdlettergevoelig; `(?i)` vooraan maakt hem ongevoelig. Leeg of afwezig: geen filter. |

Een onbekende sleutel, een waarde onder 1 of een ongeldige regex is een fout die de sleutel noemt. Die fout stopt
alleen triage, voor die repo: `lighthouse tick` leest `[controller]` en negeert de subtabel, dus
de tick van die repo blijft draaien. De triage-ronde slaat de repo over met
`lighthouse: skip <pad>: <fout>` en eindigt met exitcode 2, ook bij `--dry-run`.

## Voorwaarden per repo

Vóór het eerste oordeel toetst de ronde per repo. Faalt er één, dan slaat ze die repo over met een
luide fout en eindigt ze met exitcode 2.

- De repo is bedienbaar voor de dispatcher: een `[controller]`-tabel, `[usage_guard] controller =
  true` en een compleet `[forge.board]` (zie *Vereisten per repo* in [tick.md](tick.md)).
- `[forge.board] status_options` heeft ook de optie `To do`. Triage leest en zet die.
- Bij `wait_for_night = true` valt de rol van het account van het token in
  `[dispatch] answer_permissions`. Anders leest de tick de handoff niet, ziet hij geen
  🤖-comment en start hij het issue meteen, zonder venster.

Met `--dry-run` wordt alles hierboven behalve het board en de optie `To do` een waarschuwing
(`lighthouse: warning: ...`) in plaats van een fout. Het kandidaatfilter leest het board, dus dat
blijft verplicht. Zo kan de prompt geijkt worden vóór de repo dispatchbaar is.

## Kandidaten

Een issue is kandidaat als het open is (met `milestones_only = true` ook met een open milestone),
op board-Status `To do` staat of nog geen status heeft, geen geldige triage-marker draagt, en geen
🤖-comment heeft die de dispatcher als protocolkop leest. Dat laatste telt alleen voor een
auteur met een rol in `answer_permissions`. Met `milestone_title_exclude` valt een issue af waarvan de milestone-titel op die regex past, bijvoorbeeld een geparkeerde milestone die op `ON-HOLD` eindigt. Daarnaast mag er geen werk lopen: geen open PR die
naar het issue verwijst, geen lokale of remote branch `issue-<nr>-*`, geen worktree op zo'n branch
en geen open rij in `runs`. Zo'n issue krijgt een regel op stderr en telt als `running work`. De
volgorde is eerst `Priority`, dan het issuenummer.

Een kolom die niet in `status_options` staat, zoals `In progress` of `Done`, telt niet als "geen
status": zo'n issue is geen kandidaat. Een issue met een gearchiveerd board-item slaat de ronde
over, met een regel op stderr; het telt als `skipped`. De tick ziet dat item niet, dus de ronde
oordeelt er niet over en schrijft er niets.

Een marker met een vaste stop (een open blokkeerder, een blokkadelabel of een gebroken invariant)
vervalt zodra de stop is weggevallen; dan is het issue weer kandidaat. Een marker zonder vaste
stop vervalt nooit vanzelf.

## Corrigeren

De ronde laat een correctievenster: de triage-comment staat er, en bij `wait_for_night = true`
start de tick pas in het nachtvenster. De herstelregel onderaan de comment zegt per uitkomst welke
van deze drie geldt. Bij `wait_for_night = false` noemt die regel geen `🤖 Handoff`: die is er dan
niet.

- **Niet naar de dispatcher:** zet de status terug naar `To do`. Staat er een `🤖 Handoff`,
  verwijder die dan ook, of zet de status terug vóór het nachtvenster begint.
- **Opnieuw triëren:** pas eerst zo nodig de labels of de body aan. Zet een doorgezet issue
  eerst terug op `To do`: zonder 🤖-comment start de volgende tick het anders meteen, ook buiten
  het nachtvenster. Verwijder daarna de triage-comment, en bij `wait_for_night = true` ook de
  `🤖 Handoff`.
- **Zelf oppakken:** zet eerst de status terug naar `To do`, en start dan
  `/lighthouse-start-issue`. Anders start de dispatcher er 's nachts een run naast.

## De rem

De ronde gebruikt dezelfde rem als de tick: de pauzevlag en `halt_pct_session`/`halt_pct_week` uit
`[usage]`, zie [usage-venster.md](usage-venster.md). Staat de rem aan bij de start, of is de usage
niet gemeten, dan slaat de ronde over met exitcode 1. Voor elk oordeel toetst ze opnieuw; sluit de
rem halverwege, dan stopt de ronde voor alle repo's met exitcode 0.

## Host-deploy

De timer en de unit horen bij de host-deploy, net als die van de tick, en staan niet in de repo.
Dit is het voorbeeld.

```ini
# ~/.config/systemd/user/lighthouse-triage.service
[Unit]
Description=Lighthouse triage

[Service]
Type=oneshot
EnvironmentFile=%h/.config/lighthouse/triage.env
Environment=PATH=%h/go/bin:%h/.local/bin:/usr/local/bin:/usr/bin:/bin
ExecStart=%h/go/bin/lighthouse triage
SuccessExitStatus=1
TimeoutStartSec=80min
```

```ini
# ~/.config/systemd/user/lighthouse-triage.timer
[Unit]
Description=Lighthouse triage, voor het nachtvenster

[Timer]
OnCalendar=*-*-* 18:00

[Install]
WantedBy=timers.target
```

- `--user`: de unit draait onder het account dat het token en de `claude`-login heeft.
- `EnvironmentFile`: een bestand met `GH_TOKEN` (`chmod 600`), of hetzelfde bestand als dat van
  de tick. Het token heeft dezelfde rechten nodig als dat van de tick (zie *Token* in
  [tick.md](tick.md)): de ronde schrijft comments en zet de board-status.
- `Environment=PATH`: systemd geeft een user-unit een kaal `PATH`. De ronde start `claude` en
  `git`, en `claude` staat vaak in `~/.local/bin`.
- `ExecStart`: `~/go/bin/lighthouse` is waar [install.md](install.md) de binary neerzet. Staat hij
  elders, pas dan het pad aan.
- `SuccessExitStatus=1`: exitcode 1 is overgeslagen (rem, pauze, vorige ronde nog bezig), geen
  fout. Alleen exitcode 2 zet de unit op `failed`.
- `TimeoutStartSec`: het aantal repo's maal `max_judgments` maal 300 seconden (de timeout van één
  oordeel), plus marge. Bij één repo en de standaarden is dat 15 x 300 s = 75 minuten, dus
  `TimeoutStartSec=80min`. Bij meer repo's of een hogere `max_judgments` verandert de som.
- `OnCalendar=*-*-* 18:00`: ruim vóór het begin van het `night`-venster (standaard 21:00), zodat
  de gebruiker de avond heeft om te corrigeren.

## Zie ook

- [tick.md](tick.md): de dispatcher-tick die de doorgezette issues start.
- [usage-venster.md](usage-venster.md): de usage-meting achter de rem.
