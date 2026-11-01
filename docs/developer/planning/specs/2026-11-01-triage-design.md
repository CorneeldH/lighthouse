# Triage: issues vooraf routeren en zelf naar de dispatcher zetten

Issue: #120 · Datum: 2026-11-01 · Milestone: M2 — Dispatcher · Area: dispatcher

**Status:** Ontwerp. De keuzes van de gebruiker zijn vastgelegd in de brainstorm van 2026-11-01,
de spec-review is verwerkt.
**Bouwt voort op:** [M2-dispatcher](2026-10-27-m2-dispatcher-design.md) (de tick, de keten, de
rem, `when: night`).
**Bron:** de triage-ronde van paia (CorneeldH/personal_ai_agent#1138): de spec
`docs/developer/planning/specs/2026-10-11-triage-ronde-design.md`, de code
`src/controller/triage.py` en `scripts/controller_triage.py`, en het bewijs
`deploy/beelink/controller-triage-verification.md`.

`auto_aanbevolen` is afgesplitst naar #168. Deze spec gaat alleen over triage.

## Probleem

De dispatcher van M2 start alleen issues die de gebruiker zelf op board-Status `Controller`
zet. Routeren gebeurt pas als iemand een issue oppakt. Een issue dat de dispatcher zonder
gebruiker kan dragen, blijft daardoor liggen tot de gebruiker er tijd voor maakt.

De gebruiker wil vooral dat zulke issues vanzelf naar de dispatcher gaan. Daarnaast moeten de
route en de ontwerpvragen als comment klaarstaan voor wanneer de gebruiker een issue oppakt.

## Wat paia ons leert

De triage-ronde draait in paia sinds 2026-10-11, elke dag om 18:00. De cijfers komen uit het
journal (`journalctl --user -u controller-triage`, 2026-10-17 t/m 2026-10-31) en het
verificatiebestand:

- **De routekeuze is goed.** In de dry-run van 11 issues week de poort nergens af van een
  interactieve `route-issue` die ook de comments en de `git log` meelas.
- **Er is niets vanzelf doorgezet.** Elke live ronde gaf `doorgezet=0`. Het enige issue dat
  via triage-logica op `Controller` kwam (#725), zette de gebruiker met de hand door.
- **De oorzaak zit in de regels, niet in het oordeel.** Over de hele looptijd zijn er ongeveer
  16 oordelen op 15 issues. Daarvan waren er 9 poort 4c, 3 poort 0 en 1 dubbel. Er waren 3 een
  4b met `3-needs-hands` of `3-needs-testing` (#1209, #1312, #194). Die drie stopten alleen op
  de regel "een `3-`-label gaat nooit naar `Controller`".
- **De usage-toets slaat te vaak over.** Van de 15 rondes sloegen er 5 over op "usage boven de
  drempel". Nog eens 5 sloegen over op "usage-meting niet vers", en vanaf 2026-10-27 is dat
  elke dag gebeurd. Paia eist een meting die nieuwer is dan de start van de ronde. Om 18:00
  draait ook de tick, die het meetlock houdt. De ronde leest dan de oude cache en geeft het na
  twee minuten op.
- **De hercontrole werkt.** Op 2026-10-22 sloeg de ronde 2 van de 4 issues over omdat ze
  tussen oordeel en schrijven waren gewijzigd.

Paia heeft ongeveer 14 issues met een triage-comment, in twee vormen. Comments van vóór
2026-10-24 gebruiken `<!-- paia: {"soort", "versie", "poort", "blokkers", "label3"} -->`.
Daarna schrijft paia `<!-- lighthouse: {"kind", "version", "gate", "blockers", "label3"} -->`
(`render_marker` in `src/controller/triage.py`).

## Doel

1. Een getrieerd issue op poort 4a, 4b of 2 staat na de ronde op `Controller`, ook als het
   `labels.needs_hands` of `labels.needs_testing` draagt. Standaard start de dispatcher het pas
   in het nachtvenster, zodat de gebruiker de avond heeft om te corrigeren.
2. Elk getrieerd issue heeft een triage-comment met de routeregel. Bij 4c staan daar ook de
   ontwerpvragen met opties in.
3. Een tweede ronde wijzigt niets aan een issue dat al getrieerd is. De uitzondering is een
   issue dat op een vaste stop stond (een open blokkeerder, een blokkadelabel of een gebroken
   invariant), als die stop is weggevallen.
4. Triage draait per repo, en alleen als die repo het aanzet.

## Buiten scope

- `auto_aanbevolen` (#168).
- Labels zetten of wijzigen, issues sluiten, of `blocked_by`-relaties leggen. De ronde meldt
  het en laat het aan de gebruiker.
- Wijzigingen aan decide en apply van de tick. Triage levert alleen invoer aan waar de tick al
  op draait: de status `Controller` en een `🤖 Handoff`.
- De timer zelf. Die hoort bij de host-deploy, net als `controller-tick.timer` (M2, *De
  timer*).
- Lighthouse dispatchbaar maken. Dat doen de M2-issues (#118). Het is een voorwaarde voor AC 9.
- De overstap van paia. Die gebeurt pas als deze functie draait. Pas daarna kan
  `controller_triage.py` weg uit paia (AC 3 van #120).

## Approaches

| | Aanpak | Wat het kost |
|---|---|---|
| **A** (gekozen) | Een eigen commando `lighthouse triage` met een eigen timer. Per issue doet het één `claude -p`-aanroep met alleen leestools en `--json-schema`. Go verzamelt, valideert en schrijft. | Er komt een headless `claude -p` terug, terwijl M2 de headless runner schrapte. Het verschil is dat dit één oordeel zonder schrijftools is, geen run. De aanroep hangt af van CLI-vlaggen, en Go moet de skillcatalogus als data meegeven. |
| B | Het oordeel als Forge-run. De dispatcher start een werkplek die `/lighthouse-route-issue` over de kandidaten draait. Go past daarna de uitkomst toe. | Elke ronde neemt een plek in die anders een run zou krijgen. Een werkplek is zwaarder dan een losse aanroep. Gestructureerde uitvoer is lastiger af te dwingen dan met `--json-schema`. |
| C | Een stap in `lighthouse tick`, één keer per dag, met het oordeel van A. | 15 oordelen duren 5 tot 8 minuten. Al die tijd houdt de stap `tick.lock` vast, en dan start of hervat de dispatcher niets. |

De gebruiker koos A. Paia heeft deze aanpak bewezen: ongeveer 18k invoertokens per oordeel, en
de gekozen poort was gelijk aan een interactieve lezing. A blokkeert de dispatcher ook niet.

## Ontwerp

### Vorm: verzamelen, oordelen, toepassen

| Stap | Wie | Wat |
|---|---|---|
| verzamelen | Go | de kandidaten, en per issue de vaste feiten |
| oordelen | `claude -p`, één aanroep per issue | de routeregel, redundantie, `headless` en de ontwerpvragen, als JSON volgens een schema |
| toepassen | Go | de triage-comment, en eventueel een handoff en de board-status |

Het model wijzigt zelf niets. Wat een vaste regel kan beslissen, beslist Go.

Het nieuwe package `internal/triage` bevat de pure logica: het kandidaatfilter, de vaste
regels, het schema en de prompt, de validatie, de uitkomst en de comment-teksten. Het is te
testen zonder netwerk en zonder `claude`. Het commando staat in `internal/cli/triage.go`.

**Wat er al is:** `internal/forge` (`OpenIssues`, `Issue`, `Comments`, `CreateComment`,
`BoardItems`, `BranchPRs`, `SetStatus`, `RoleName`), `internal/protocol` (`ParseHeader`,
`ParseHandoff`, `IsAnswer`), de `Trust` van `internal/dispatch`, `repoconfig.Dispatchable`,
`internal/hostconfig` en de usage-rem van M2.

**Wat er bij moet** *(eigen keuze na spec-review, r1-f14)*:

- in `internal/forge`:
  - per issue de labels, de body, de auteur, `updatedAt`, en de milestone met status en
    beschrijving;
  - de issues die in de laatste 30 dagen in een milestone zijn gesloten;
  - de open PR's die naar een issue verwijzen;
  - een issue aan het board toevoegen (voor een issue zonder board-item);
- in `internal/repoconfig`: het lezen van `[labels]` en `[controller.triage]`;
- in `internal/protocol`: een lezer voor de triage-marker. Er bestaat nu alleen een lijst met
  prefixen (`artefactMarkers`).

### Het commando

`lighthouse triage [--dry-run] [--issue <repo>#<nr>]...`

- Het loopt over de repo's in `[dispatch] repos` van `host.toml` die een tabel
  `[controller.triage]` hebben. Die lijst is de enige bron. Een repo die er niet in staat,
  bestaat voor de ronde niet *(eigen keuze na spec-review, r1-f4)*.
- `--dry-run` doet de oordelen, maar schrijft niets. De routeregels gaan naar stdout. Dit is
  het gereedschap om de prompt te ijken (zie *Testen en bewijs*).
- `--issue` mag alleen samen met `--dry-run`. Het beoordeelt precies die issues, ook als ze
  buiten het kandidaatfilter vallen.
- Exitcodes:
  - `0`: de ronde heeft gedraaid. Dat geldt ook bij nul kandidaten, en bij een ronde die
    halverwege stopt op de rem.
  - `1`: de ronde is overgeslagen. Er stond in geen enkele repo triage aan, de rem stond bij de
    start aan, of de pauzevlag was gezet.
  - `2`: fout. Ook een paniek wordt `2`, zodat de unit op `failed` komt.

  Nu geeft `cli.Execute` elke fout exitcode `1` (`internal/cli/root.go`). Daarom krijgt de CLI
  een getypeerde exitfout die `execute` naar zijn code vertaalt. Een gewone fout van
  `triage` wordt `2` *(eigen keuze na spec-review, r1-f2)*.
- Een eigen `triage.lock` voorkomt dat twee rondes tegelijk lopen. De ronde neemt `tick.lock`
  niet: de tick mag doordraaien.
- Per ronde en per repo schrijft het commando één samenvattingsregel naar stderr, met de
  aantallen kandidaten, oordelen, doorgezet, ontwerp, overig, ongeldig, lopend werk,
  overgeslagen en fouten.

### Configuratie

Nieuwe subtabel `[controller.triage]` in `lighthouse.toml`, zoals AC 2 van #120 vraagt
(configuratie per repo in `[controller]`) *(eigen keuze na spec-review, r1-f17)*. Staat de
tabel er, dan staat triage aan voor die repo.

| Sleutel | Standaard | Doel |
|---|---|---|
| `milestones_only` | `true` | Alleen issues met een open milestone zijn kandidaat. Met `false` doet elk open issue mee. |
| `wait_for_night` | `true` | Het correctievenster. Met `true` schrijft de ronde bij een doorgezet issue een `🤖 Handoff` met `when: night`. Met `false` zet ze alleen de status, en start de eerstvolgende tick het issue meteen. |
| `max_forwarded` | `5` | Na zoveel doorgezette issues stopt de ronde voor die repo. |
| `max_judgments` | `15` | Hoogstens zoveel oordelen per ronde en per repo, om de usage te begrenzen. |

Een onbekende sleutel of een ongeldige waarde is een fout, zoals bij `[dispatch]`. *Mijn
keuze:* de tabel staat per repo, omdat de keuze om een repo te triëren bij het project hoort.

**Voorwaarden per repo**, getoetst vóór het eerste oordeel. Faalt er één, dan slaat de ronde
die repo over met een luide fout, en eindigt ze met exitcode `2`:

- `repoconfig.Dispatchable(r)` geeft `nil`. Dat dekt `[controller]`,
  `[usage_guard] controller = true` en een compleet `[forge.board]`.
- `[forge.board] status_options` heeft ook de optie `To do`. Triage leest en zet die
  *(eigen keuze na spec-review, r1-f3)*.
- Bij `wait_for_night = true` valt het account van het token in `answer_permissions`. Anders
  leest de tick de handoff niet. Hij ziet dan geen 🤖-comment, en start het issue meteen,
  zonder venster (`internal/dispatch/decide.go`, de start van een issue zonder comments)
  *(eigen keuze na spec-review, r1-f5)*.

`init` schrijft `[controller.triage]` niet. Triage staat pas aan als de gebruiker de tabel zelf
toevoegt.

### Kandidaten

Een issue is kandidaat als het aan alles hieronder voldoet. Dit filter is deterministisch.

1. Het issue is open. Met `milestones_only = true` heeft het een milestone, en die milestone
   is open.
2. Het staat op board-Status `To do`, of het heeft nog geen status. Een issue dat nog niet op
   het board staat, telt als "geen status".
3. Het heeft geen geldige triage-marker (zie *Marker*).
4. Er staat geen 🤖-comment op die de dispatcher als protocolkop leest. Dat is een comment van
   een auteur die de `Trust` van de dispatcher vertrouwt, en die door `protocol.ParseHeader`
   komt. Zo'n issue zat al in de keten. Ook een handoff die de ronde eerder zelf schreef, telt
   hier mee. Een 🤖-comment van een vreemde auteur telt niet *(eigen keuze na spec-review,
   r1-f5)*.
5. Er loopt nog geen werk aan:
   - geen open PR die naar het issue verwijst;
   - geen lokale of remote branch `issue-<nr>-*`;
   - geen worktree op zo'n branch;
   - geen open rij in `runs` voor dit issue.

   Een issue dat op regel 5 afvalt, krijgt een regel op stderr met wat er gevonden is. Het telt
   in de samenvatting onder "lopend werk".

Volgorde: eerst `Priority`, dan het issuenummer *(eigen keuze na spec-review, r1-f6)*.

### Vaste regels vóór het model

Go beslist zelf, zonder model:

- **Poort 0.** Het issue draagt `labels.blocked_issue` of `labels.blocked_external`. Of het
  heeft een open `blocked_by` zonder `labels.needs_shaping`, `labels.needs_decision` of
  `labels.blocked_issue`. Dat is de regel van `route-issue`, poort 0. Het model beoordeelt dan
  alleen de redundantie. Go schrijft de routeregel zelf, en de reden noemt de open
  blokkeerders. Zegt het model poort 0 terwijl Go geen blokkade vindt, dan is het oordeel
  ongeldig.
- **Gebroken invariant.** De invariant volgt uit `[labels]`, zoals `lighthouse-write-issue`
  stap 3a en 3b *(eigen keuze na spec-review, r1-f13)*:
  - met `labels.location_prefix`: precies één label met dat prefix;
  - met `labels.area_prefix`: precies één label met dat prefix;
  - hoogstens één blokkadelabel (de zes `labels.blocked_*`/`labels.needs_*`).

  Een sleutel die de repo niet heeft, levert geen eis. Breekt een issue de invariant, dan
  corrigeert de ronde niets en zet ze het issue niet door. De comment meldt de afwijking.
  Het issue krijgt een marker die vervalt zodra de invariant weer klopt (zie *Marker*). Paia
  vulde ontbrekende labels aan; lighthouse doet dat niet.
- **Feiten die Go meegeeft als data.** Eén keer per repo per ronde doet Go eerst
  `git fetch origin <default_branch>` in de main checkout *(eigen keuze na spec-review,
  r1-f18)*. Daarna per issue:
  - `git log --oneline origin/<default_branch> -E --grep '#<nr>([^0-9]|$)'`;
  - de `blocked_by`-lijst;
  - titel en nummer van de andere open issues in dezelfde milestone, en van de issues die daar
    in de laatste 30 dagen zijn gesloten;
  - de beschrijving van de milestone. Poort 4c leest daarin de open vragen
    (`heading:milestone_not_yet_decided`).

### Het oordeel

Per issue één `claude -p`, in een export van `origin/<default_branch>` van de repo (het pad uit
`[dispatch] repos` levert de repo; de export is een tijdelijke map met alleen de getrackte
inhoud, zie [de leesgrens](2026-11-01-triage-leesgrens-design.md)). De vlaggen zijn die van paia,
gemeten met `claude` 2.1.296, met `--setting-sources ""` en `--no-session-persistence` erbij:

```text
--tools Read,Grep,Glob --strict-mcp-config --setting-sources ""
--settings '{"disableAllHooks": true, ...}' --no-session-persistence
--output-format json --json-schema <schema>
```

Met deze vlaggen heeft het model alleen `Read`, `Grep` en `Glob`. Er vuren geen hooks, en
`CLAUDE.md`/`AGENTS.md` laden niet. Uit de gebruikersinstellingen komen alleen `env` en
`modelSettings` mee, via `--settings`. De invoer van een oordeel daalde daarmee van 40k naar 18k
tokens. De bouwer meet de vlaggen opnieuw met de `claude`-versie op de host (zie *Aan de
bouwer*).

Het oordeel heeft een harde timeout. Een timeout telt als een ongeldig oordeel.

De poorten staan op één plek: `.claude/skills/lighthouse-route-issue/SKILL.md` in de repo. De
prompt verwijst naar dat bestand en kopieert de poorten niet. De skill leest labelnamen en het
poortwoord via `config.py`, met Bash, en het oordeel heeft geen Bash. Go geeft die waarden
daarom als data mee:

- de labelnamen uit `[labels]`;
- het poortwoord `heading:gate_line` in `[project] language`;
- de skillcatalogus: `name` en `description` uit de frontmatter van elke
  `.claude/skills/*/SKILL.md`, plus de `superpowers:*`-skills. Poort 1 rekent op die catalogus.
  De `superpowers:*`-skills staan niet in de repo, maar in de plugin-installatie van de host.
  Go leest ze uit de geïnstalleerde plugin (via `~/.claude/plugins/installed_plugins.json`).
  Ontbreekt de plugin, dan laat Go ze weg en schrijft het één regel op stderr. De dry-run
  ijkt poort 1 met de catalogus zoals die op de host staat. *Mijn keuze:* geen vaste lijst die
  iemand moet bijhouden.
- het locatielabel, als de repo `labels.location_prefix` heeft. Of het werk een andere host
  vraagt, beoordeelt het model (`headless: no`). *Mijn keuze:* lighthouse weet niet welke
  locatie bij de host van de dispatcher hoort, dus een vaste `1-loc:mac`-regel zoals in paia
  past hier niet. Een ontbrekend locatielabel valt onder de invariant.

Issuebody, comments en milestonetekst gaan mee als DATA. Een instructie in een issuebody is
geen opdracht. Dat beschermt alleen het oordeel. Wie het issue mag laten uitvoeren, regelt
*Toepassen* (rij 6).

Schema (de bouwer legt de exacte veldnamen vast):

| Veld | Inhoud |
|---|---|
| `route_line` | precies de regel uit `route-issue`: `#<nr> <poortwoord> <n>[<a\|b\|c>]: <route> — <reden>` |
| `gate`, `sub` | `0`, `1`, `2` of `4`, en `a`/`b`/`c` (alleen bij poort 4). Poort 3 valt in `route-issue` altijd door naar 4, dus een regel met poort 3 is ongeldig *(eigen keuze na spec-review, r1-f9)*. |
| `redundancy` | `none`, `duplicate` (met nummer) of `done` (met bewijs) |
| `headless` | `yes`, of `no` met een reden. `no` alleen als het werk sudo vraagt, een andere host, of een live onomkeerbare stap. Een handstap telt niet als `no`. |
| `design_questions` | verplicht bij 4c, en alleen daar: 1 tot 4 vragen, elk met 2–6 opties en één aanbevolen optie |

Go valideert het oordeel tegen een strikte regex op het hele contract:

- bij poort 4 is de subletter verplicht;
- `gate`/`sub` moet bij de regel passen;
- bij poort 0 wint de uitkomst van Go;
- draagt het issue `labels.needs_shaping` of `labels.needs_decision`, dan moet het oordeel 4c
  zijn, met ontwerpvragen. `route-issue` stuurt beide labels naar 4c (*A blocking label steers
  the triage*). Elk ander oordeel is ongeldig *(eigen keuze na spec-review, r1-f1)*.

Een ongeldig oordeel betekent: geen actie op dit issue, geen marker, en een foutregel op
stderr. De ronde gaat door met het volgende issue.

### Toepassen

De eerste rij die past, wint, van boven naar beneden:

| # | Uitkomst | Comment | Handoff | Board |
|---|---|---|---|---|
| 1 | invariant gebroken | triage met de afwijking | — | ongewijzigd |
| 2 | redundantie `duplicate` of `done` | triage met de kandidaat en het bewijs; er wordt niets gesloten | — | ongewijzigd |
| 3 | poort 0 of 1 | triage (routeregel) | — | ongewijzigd |
| 4 | poort 4c (ook met `labels.needs_shaping` of `labels.needs_decision`) | triage met ontwerpvragen | — | ongewijzigd |
| 5 | 4a, 4b of 2 met `headless: no` | triage met de reden waarom het interactief moet | — | ongewijzigd |
| 6 | 4a, 4b of 2, maar de auteur van het issue valt niet in `answer_permissions` | triage met de reden: een vertrouwde gebruiker zet het zelf door | — | ongewijzigd |
| 7 | 4a, 4b of 2 (ook met `labels.needs_hands` of `labels.needs_testing`) | triage | `when: night` als `wait_for_night` | → `Controller` |

Rij 7 is het verschil met paia. Daar ging een issue met een `3-`-label nooit naar
`Controller`. Hier gaan `labels.needs_hands` en `labels.needs_testing` wél door. De run werkt
dan door tot de handstap, en stelt daar een Vraag of geeft het issue terug (M2, *Afloop*). Het
issue blijft dan in `Controller`. Dat is de keuze van de gebruiker.

Rij 6 bestaat omdat de body de opdracht wordt van een run met schrijftools. M2 vertrouwt een
comment alleen via `answer_permissions` (`internal/dispatch/gather.go`, `Complete`). Zonder
triage bepaalde een mens welke issues een run kregen. Een buitenstaander die in een publieke
repo een issue opent, zou met triage anders 's nachts een run op die tekst krijgen. Rij 6 legt
dezelfde grens op de auteur van het issue, met dezelfde `Trust` *(eigen keuze na spec-review,
r1-f8)*.

Na het doorzetten van issue nummer `max_forwarded` stopt de ronde voor die repo. De issues die
nog niet aan de beurt waren, krijgen geen marker, en komen de volgende ronde aan de beurt
*(eigen keuze na spec-review, r1-f10)*.

**De handoff** bij `wait_for_night = true`, in het formaat dat `protocol.ParseHandoff` leest:

```text
🤖 Handoff
issue: #<nr>
worktree: <main checkout>
branch: <default_branch>
next: /lighthouse-start-issue <nr>
when: night
```

Waarom dit zonder wijziging aan de tick werkt (`internal/dispatch/decide.go`, `handoff`):

- `/lighthouse-start-issue` staat op de allowlist.
- Een handoff op de default branch krijgt de branch `issue-<nr>-<slug>`.
- `when: night` stelt de start uit tot het venster `night` van `[dispatch]` begint.
- Een doorgezet issue heeft geen open blokkeerder, want poort 0 zet niet door.
- Het token valt in `answer_permissions` (een voorwaarde per repo, zie *Configuratie*).

Bij `wait_for_night = false` schrijft de ronde geen handoff. Dan geldt M2-regel 6: een issue in
`Controller` zonder 🤖-comment start met `/lighthouse-start-issue <nr>`.

**Volgorde per issue:** eerst de triage-comment met de marker, dan de handoff, dan de status.
De handoff gaat vóór de status. De tick kijkt alleen naar `Controller` en `On hold`. Zou de
status eerst staan, dan kan een tick die daartussen valt het issue meteen starten, zonder
venster.

**Hercontrole.** Tussen verzamelen en schrijven zitten minuten. Vlak voor de eerste
schrijfactie op een issue leest de ronde daarom het issue opnieuw: board-status, labels,
comments, `updatedAt` en lopend werk (kandidaatregels 2–5). Is er iets veranderd, dan slaat ze
het issue over zonder één schrijfactie. Op stderr komt dan een regel.

**Een stap die faalt.** Elke stap krijgt een regel op stderr, met het issuenummer en de stap.
Faalt een stap halverwege, dan stopt dat issue en gaat de ronde door met het volgende. De
ronde eindigt dan met exitcode `2`. Een half afgemaakt issue maakt de ronde niet zelf af.

### De rem

De ronde gebruikt de rem van M2 (#116), met dezelfde definitie van "gemeten": een usage-cache
die niet ouder is dan twee keer de TTL. Er is geen eis dat de meting nieuwer is dan de start
van de ronde. Dat verhelpt het "niet vers"-probleem van paia.

Bij de start van de ronde gelden:

- de pauzevlag van de dispatcher;
- `halt_pct_session` en `halt_pct_week` uit `[usage]`.

Staat één daarvan aan, of is de usage niet gemeten, dan slaat de ronde over met exitcode `1`
en een regel met de reden.

Voor elk oordeel roept de ronde `Refresh(false)` aan, en toetst ze de usage opnieuw. Een
`Busy` met een cache die niet ouder is dan twee keer de TTL telt als gemeten. Staat de rem
aan, of is de usage niet meer gemeten, dan stopt de ronde met een regel op stderr. Wat al
geschreven is, blijft staan. Het commando eindigt dan met exitcode `0`, want de ronde heeft
gedraaid *(eigen keuze na spec-review, r1-f16)*.

Geheugen en plekken tellen niet mee: een oordeel is één kort proces, geen run.

### Marker

De triage-comment begint met een artefactmarker, in de vorm die M0 vastlegde (beslissing 15 en
21): `<!-- lighthouse: {json} -->` met Engelse sleutels.

```text
<!-- lighthouse: {"kind": "triage", "version": 1, "gate": "4b"} -->
**Triage** · #<nr> Poort 4b: writing-plans — <reden>
…
```

Bij een vaste stop draagt de marker die stop:

```text
<!-- lighthouse: {"kind": "triage", "version": 1, "gate": "0", "blockers": [114], "label": null} -->
<!-- lighthouse: {"kind": "triage", "version": 1, "gate": "4c", "blockers": [], "label": "3-needs-decision"} -->
<!-- lighthouse: {"kind": "triage", "version": 1, "gate": "invariant"} -->
```

- Bij poort 0 bevat `blockers` de nummers uit `blocked_by`, en `label` het label
  `labels.blocked_issue` of `labels.blocked_external` als het issue dat droeg.
- Bij poort 4c op een issue met `labels.needs_shaping` of `labels.needs_decision` (rij 4) staat
  dat label in `label`. Zo'n marker is ook een vaste stop: haalt de gebruiker het label weg na
  de beslissing, dan vervalt de marker *(eigen keuze na spec-review, r2-f1)*.
- `labels.needs_hands` en `labels.needs_testing` komen niet in de marker, want die houden een
  issue niet tegen (rij 7) *(eigen keuze na spec-review, r1-f11)*.
- Een marker met `"gate": "invariant"` vervalt zodra Go de invariant weer ziet kloppen
  *(eigen keuze na spec-review, r1-f12)*.
- Go toetst elke ronde, zonder model, of de stop nog geldt. Zijn alle blokkeerders gesloten
  en is het label weg, dan vervalt de marker, en is het issue weer kandidaat. Een issue
  waarvan de beslissing genomen is en het label is weggehaald, komt zo vanzelf bij de
  dispatcher. Alleen de nieuwste marker telt. *Mijn keuze, zoals paia (`marker_geldig`).*
- Een marker zonder vaste stop vervalt nooit vanzelf.
- De comment begint niet met 🤖. Hij verbergt dus geen handoff, en hij geldt ook niet als
  antwoord op een Vraag (`protocol.IsAnswer` negeert een comment die met een artefactmarker
  begint).
- Onderaan de comment staat in één regel hoe je corrigeert.

**De markers van paia.** De lezer accepteert beide vormen die paia schreef (zie *Wat paia ons
leert*). Zonder die lezer triëert lighthouse na de overstap de ongeveer 14 paia-issues met zo'n
comment opnieuw:

- `<!-- paia: {"soort", "versie", "poort", "blokkers", "label3"} -->`;
- `<!-- lighthouse: {"kind", "version", "gate", "blockers", "label3"} -->`: hier is `label3`
  een alias van `label` *(eigen keuze na spec-review, r1-f7)*.

In beide vormen geldt een marker met `label3` gelijk aan `labels.needs_hands` of
`labels.needs_testing` als vervallen. Paia hield zulke issues tegen, deze spec niet (rij 7),
dus ze worden één keer opnieuw getrieerd. Schrijven gebeurt alleen in de nieuwe vorm, met
`label`.

### Corrigeren

- **Niet naar de dispatcher:** zet de status terug naar `To do`. Staat er een `🤖 Handoff`,
  verwijder die dan ook, of zet de status terug vóór het nachtvenster begint.
- **Opnieuw triëren:** pas eerst zo nodig de labels of de body aan. Zet een doorgezet issue
  eerst terug op `To do`: zonder 🤖-comment start de volgende tick het anders meteen, ook buiten
  het nachtvenster. Verwijder daarna de triage-comment, en bij `wait_for_night = true` ook de
  `🤖 Handoff`.
- **Zelf oppakken:** zet eerst de status terug naar `To do`, en start dan
  `/lighthouse-start-issue`. Anders start de dispatcher er 's nachts een run naast.

De herstelregel onderaan de comment zegt per uitkomst welke van deze drie geldt. Bij
`wait_for_night = false` noemt die regel geen `🤖 Handoff`: die is er dan niet *(eigen keuze na
de eindreview)*.

### Signalen

- Per issue is de comment het signaal. Daar kijkt de gebruiker al.
- Een mislukte ronde eindigt met exitcode `2`. De unit komt dan op `failed`, en de host-deploy
  meldt dat zoals bij de tick.
- Een ongeldig oordeel is geen mislukte ronde. Het staat op stderr, en het issue komt de
  volgende ronde terug.

## Acceptatiecriteria

1. Een issue op 4a, 4b of 2 komt na de ronde op `Controller`, op twee voorwaarden: het heeft
   geen blokkade, `labels.needs_shaping` of `labels.needs_decision`, en de auteur valt in
   `answer_permissions`. Dat geldt ook met `labels.needs_hands` of `labels.needs_testing`. Met
   `wait_for_night = true` staat er een `🤖 Handoff` met `when: night`, die `decide` als start
   beoordeelt.
2. Een 4c-issue, of een issue met `labels.needs_shaping` of `labels.needs_decision`, komt niet
   op `Controller`. Het krijgt een comment met ontwerpvragen en opties.
3. Poort 0 en 1 houden hun status en krijgen alleen de comment.
4. Een vermoedelijk dubbel of al afgerond issue wordt gemeld, niet gesloten en niet
   doorgezet.
5. Een tweede ronde doet niets met een issue dat een geldige marker draagt. Dat geldt ook voor
   een marker in een van beide vormen van paia.
6. Met `milestones_only = true` doet een issue zonder milestone niet mee. Met `false` wel.
7. Een repo zonder `[controller.triage]` wordt niet getrieerd. Een repo die niet aan de
   voorwaarden voldoet, geeft een luide fout en exitcode `2`.
8. De ronde slaat over met exitcode `1` bij een rem of een pauzevlag, en met een verse cache
   (niet ouder dan twee keer de TTL) draait ze.
9. Live bewijs op lighthouse: minstens één issue dat via de ronde op `Controller` kwam, met
   `when: night`, en dat de tick daarna in het nachtvenster startte. Dit AC valt buiten deze
   bouw: het wacht tot lighthouse door de dispatcher wordt bediend (M2-uitrol, #118). De bouw
   eindigt bij de unit-tests en de dry-run-ijking, en de PR verwijst met `Refs #120`.

## Testen en bewijs

Unit, in `internal/triage` (zonder netwerk en zonder `claude`):

- het kandidaatfilter: milestone aan/uit, status, marker (nieuw en beide paia-vormen),
  🤖-comment van een vertrouwde en van een vreemde auteur, en lopend werk (PR, branch,
  worktree, runrij);
- een vervallen marker:
  - poort 0 met alle blokkeerders dicht en zonder label wordt weer kandidaat, en met één
    blokkeerder nog open niet;
  - een invariantmarker op een issue dat nu klopt;
  - een 4c-marker met `labels.needs_decision`, waarvan het label is weggehaald;
  - een paia-marker met `label3` `3-needs-hands`;
- de vaste regels: poort 0, en de invariant met en zonder `location_prefix`/`area_prefix`;
- de voorrang tussen de rijen van *Toepassen*. Bijvoorbeeld: 4b met `labels.needs_hands` gaat
  door, 4b van een vreemde auteur niet;
- de validatie van het oordeel:
  - een goede routeregel;
  - een kale `Poort 4` zonder subletter;
  - een regel met poort 3;
  - een `gate`/`sub` die niet bij de regel past;
  - 4b op een issue met `labels.needs_decision`;
  - 4c zonder ontwerpvragen;
- `max_forwarded` en `max_judgments`;
- de hercontrole: een gewijzigde status geeft nul schrijfacties;
- de schrijfvolgorde: de handoff staat vóór de status;
- een falende stap op één issue stopt dat issue, niet de ronde, en de ronde eindigt met `2`;
- de handoff-tekst gaat door `protocol.ParseHeader`, `protocol.ParseHandoff` en `decide`, en
  levert een start na het begin van `night`;
- de triage-comment is geen 🤖-kop, en `IsAnswer` geeft onwaar.

Voor de CLI:

- een test met een nep-`claude` op het `PATH` die vaste JSON teruggeeft, en de
  forge-testserver uit `internal/forge/forgetest`;
- de exitcodes `0`, `1` en `2` door `cli.Execute` heen, ook voor een gewone fout en een paniek.

**Vóór de eerste timer: de routekwaliteit.** Het headless oordeel mist de sessiecontext waar
`route-issue` interactief op rekent. Schema en regex zeggen niets over de gekozen poort.
Daarom draait eerst `lighthouse triage --dry-run`, met minstens één issue per uitkomst (poort
0, 1, 2, 4a, 4b, 4c). Die regels komen naast de interactieve `/lighthouse-route-issue` op
dezelfde issues. Wijkt de poort af, dan gaat de timer pas aan als de prompt is bijgesteld.

## Aan de bouwer

- De exacte veldnamen van het schema, en de prompttekst.
- De vlaggen opnieuw meten met de `claude`-versie op de host. Toon met ruwe uitvoer dat het
  model alleen `Read`/`Grep`/`Glob` heeft, dat er geen hook-events zijn, en dat
  `CLAUDE.md`/`AGENTS.md` niet laden.
- De oordeeltimeout (in paia `300` s), en het model (in paia `opus`). Ze worden constanten in
  `internal/triage`, met één regel commentaar die naar deze spec verwijst.
- De opmaak van de triage-comment en de ontwerpvragen: gewone markdown, geen omsluitend
  codeblok, en de headings uit `headings.toml` in `[project] language`. De vragen volgen
  dezelfde limieten als een Vraag (`MAX_QUESTIONS`, de letters `a`–`f`), maar de comment is
  geen 🤖 Vraag.
- Hoe de ronde de board-status per issue leest, en hoe ze een issue zonder board-item
  toevoegt voordat ze de status zet.
- De systemd-unit voor de host-deploy *(eigen keuze na spec-review, r1-f15)*:
  - een `--user`-oneshot met `SuccessExitStatus=1`;
  - `TimeoutStartSec` ≥ het aantal getrieerde repo's × `max_judgments` × de oordeeltimeout,
    plus marge;
  - een tijdstip vóór het begin van `night`. In paia was dat 18:00.

## Afhankelijkheden

- **#116 (de rem).** De ronde gebruikt de usage-rem en de pauzevlag van M2. In de code bestaat
  de rem nog niet. Triage bouwt erop voort, en dupliceert hem niet.
- **#114 (de keten).** Gemerged. Daarop rust de handoff-regel waar `when: night` gebruik van
  maakt.
- **#118 (de uitrol).** Lighthouse dispatchbaar maken (`[controller]`, `[usage_guard]
  controller = true`, `[dispatch] repos`, de tick-timer) gebeurt in de M2-issues, niet hier.
  Het is een voorwaarde voor AC 9, geen onderdeel van deze bouw (keuze van de gebruiker).
