# Triage: issues vooraf routeren en zelf naar de dispatcher zetten

Issue: #120 (het triage-deel) · Datum: 2026-11-01 · Milestone: M2 — Dispatcher · Area: dispatcher

**Status:** Ontwerp, keuzes van de gebruiker vastgelegd in de brainstorm van 2026-11-01.
**Bouwt voort op:** [M2-dispatcher](2026-10-27-m2-dispatcher-design.md) (de tick, de keten, de
rem, `when: night`).
**Bron:** de triage-ronde van paia (CorneeldH/personal_ai_agent#1138): de spec
`docs/developer/planning/specs/2026-10-11-triage-ronde-design.md`, de code
`src/controller/triage.py` en `scripts/controller_triage.py`, en het bewijs
`deploy/beelink/controller-triage-verification.md`.

#120 dekt twee dingen: triage en `auto_aanbevolen`. Deze spec gaat alleen over triage.
`auto_aanbevolen` krijgt een eigen spec.

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

Paia heeft ongeveer 14 issues met een triage-comment. Die markers gebruiken de oude sleutels
(`soort`, `versie`, `poort`, `blokkers`, `label3`).

## Doel

1. Een getrieerd issue op poort 4a, 4b of 2 staat na de ronde op `Controller`, ook als het
   `labels.needs_hands` of `labels.needs_testing` draagt. Standaard start de dispatcher het pas
   in het nachtvenster, zodat de gebruiker de avond heeft om te corrigeren.
2. Elk getrieerd issue heeft een triage-comment met de routeregel. Bij 4c staan daar ook de
   ontwerpvragen met opties in.
3. Een tweede ronde wijzigt niets aan een issue dat al getrieerd is. De enige uitzondering is
   een issue dat op een open blokkeerder of een blokkadelabel stond, als die is weggevallen.
4. Triage draait per repo, en alleen als die repo het aanzet.

## Buiten scope

- `auto_aanbevolen` (eigen spec, het andere deel van #120).
- Labels zetten of wijzigen, issues sluiten, of `blocked_by`-relaties leggen. De ronde meldt
  het en laat het aan de gebruiker.
- Wijzigingen aan decide en apply van de tick. Triage levert alleen invoer aan waar de tick al
  op draait: de status `Controller` en een `🤖 Handoff`.
- De timer zelf. Die hoort bij de host-deploy, net als `controller-tick.timer` (M2, *De
  timer*).
- De overstap van paia. Die gebeurt pas als deze functie draait. Pas daarna kunnen
  `controller_triage.py` en `triage.py` weg uit paia (AC 4 van #120).

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

Go hergebruikt `internal/forge` voor issues, comments, board en status, `internal/protocol`
voor de handoff en de marker-lezers, `internal/repoconfig` en `internal/hostconfig` voor de
configuratie, en de usage-rem van M2.

### Het commando

`lighthouse triage [--dry-run] [--issue <repo>#<nr>]...`

- Zonder argumenten loopt het over de repo's in `[dispatch] repos` van `host.toml` die een
  `[triage]`-tabel hebben. Per repo volgt het de stappen hieronder.
- `--dry-run` doet de oordelen, maar schrijft niets. De routeregels gaan naar stdout. Dit is
  het gereedschap om de prompt te ijken (zie *Testen en bewijs*).
- `--issue` mag alleen samen met `--dry-run`. Het beoordeelt precies die issues, ook als ze
  buiten het kandidaatfilter vallen.
- Exitcodes, zoals in paia:
  - `0`: de ronde heeft gedraaid.
  - `1`: de ronde is overgeslagen. Er was niets te doen, `[triage]` stond nergens aan, de rem
    stond aan of de pauzevlag was gezet.
  - `2`: fout. Ook een paniek wordt `2`, zodat de unit op `failed` komt.
- Een eigen `triage.lock` voorkomt dat twee rondes tegelijk lopen. De ronde neemt `tick.lock`
  niet: de tick mag doordraaien.
- Per ronde en per repo schrijft het commando één samenvattingsregel naar stderr, met de
  aantallen kandidaten, oordelen, doorgezet, ontwerp, overig, ongeldig, lopend werk,
  overgeslagen en fouten.

### Configuratie

Nieuwe tabel `[triage]` in `lighthouse.toml`. Staat de tabel er, dan staat triage aan voor die
repo. De repo moet ook `[controller]` hebben en in `[dispatch] repos` staan. Zonder die twee is
er geen afnemer, en dan geeft de ronde voor die repo een luide fout.

| Sleutel | Standaard | Doel |
|---|---|---|
| `milestones_only` | `true` | Alleen issues met een milestone zijn kandidaat. Met `false` doet elk open issue mee. |
| `wait_for_night` | `true` | Het correctievenster. Met `true` schrijft de ronde bij een doorgezet issue een `🤖 Handoff` met `when: night`. Met `false` zet ze alleen de status, en start de eerstvolgende tick het issue meteen. |
| `max_forwarded` | `5` | Hoogstens zoveel issues per ronde naar `Controller`. Is dat aantal bereikt, dan stopt de ronde voor die repo. |
| `max_judgments` | `15` | Hoogstens zoveel oordelen per ronde, om de usage te begrenzen. |

Een onbekende sleutel of een ongeldige waarde is een
fout, zoals bij `[dispatch]`. *Mijn keuze:* de tabel staat per repo, omdat de keuze om een
repo te trièren bij het project hoort. De limieten tellen per repo.

`init` schrijft `[triage]` niet. Triage staat pas aan als de gebruiker de tabel zelf toevoegt.

### Kandidaten

Een issue is kandidaat als het aan alles hieronder voldoet. Dit filter is deterministisch.

1. Het issue is open. Met `milestones_only = true` heeft het een milestone. Die milestone moet
   open zijn.
2. Het staat op board-Status `To do`, of het heeft nog geen status. Een issue dat nog niet op
   het board staat, telt als "geen status".
3. Het heeft geen geldige triage-marker (zie *Marker*).
4. Er staat geen 🤖-comment op. Zo'n issue zat al in de keten. Ook een handoff die de ronde
   eerder zelf schreef, telt hier mee.
5. Er loopt nog geen werk aan:
   - geen open PR die naar het issue verwijst;
   - geen lokale of remote branch `issue-<nr>-*`;
   - geen worktree op zo'n branch;
   - geen open rij in `runs` voor dit issue.

   Een issue dat op regel 5 afvalt, krijgt een regel op stderr met wat er gevonden is. Het telt
   in de samenvatting onder "lopend werk".

Volgorde: eerst `Priority`, dan het issuenummer. Dat is dezelfde volgorde als de rij
`Controller` in de tick.

### Vaste regels vóór het model

Go beslist zelf, zonder model:

- **Poort 0.** Het issue draagt `labels.blocked_issue` of `labels.blocked_external`. Of het
  heeft een open `blocked_by` zonder `labels.needs_shaping`, `labels.needs_decision` of
  `labels.blocked_issue`. Dat is de regel van `route-issue`, poort 0. Het model beoordeelt dan
  alleen de redundantie. Go schrijft de routeregel zelf, en de reden noemt de open
  blokkeerders. Zegt het model poort 0 terwijl Go geen blokkade vindt, dan is het oordeel
  ongeldig.
- **Gebroken invariant.** `docs/developer/labels.md` eist precies één area-label en hoogstens
  één blokkadelabel. Breekt een issue dat al, dan corrigeert de ronde niets en zet ze het
  issue niet door. De comment meldt de afwijking. Het issue krijgt wel een marker, zodat de
  volgende ronde het niet opnieuw meldt. Paia vulde ontbrekende labels aan. Lighthouse doet
  dat niet: een ontbrekend area-label is hier ook een gebroken invariant.
- **Feiten die Go meegeeft als data:**
  - `git log --oneline origin/<default_branch> -E --grep '#<nr>([^0-9]|$)'`;
  - de `blocked_by`-lijst;
  - titel en nummer van de andere open issues in dezelfde milestone, en van de issues die daar
    in de laatste 30 dagen zijn gesloten;
  - de beschrijving van de milestone. Poort 4c leest daarin de open vragen
    (`heading:milestone_not_yet_decided`).

### Het oordeel

Per issue één `claude -p`, in de main checkout van de repo (het pad uit `[dispatch] repos`).
De vlaggen zijn die van paia, gemeten met `claude` 2.1.296:

```text
--tools Read,Grep,Glob --strict-mcp-config --setting-sources user
--settings '{"disableAllHooks": true}' --output-format json --json-schema <schema>
```

Met deze vlaggen heeft het model alleen `Read`, `Grep` en `Glob`. Er vuren geen hooks, en
`CLAUDE.md`/`AGENTS.md` laden niet. De invoer van een oordeel daalde daarmee van 40k naar 18k
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
- het locatielabel als het issue er een draagt (`labels.location_prefix`, als de repo die
  sleutel heeft, zoals paia). Het model beoordeelt of het werk een andere host vraagt
  (`headless: no`). Een ontbrekend locatielabel is geen gebroken invariant: lighthouse kent
  geen regel die het verplicht. *Mijn keuze:* lighthouse weet niet welke locatie bij de host
  van de dispatcher hoort, dus een vaste `1-loc:mac`-regel zoals in paia past hier niet.

Issuebody, comments en milestonetekst gaan mee als DATA. Een instructie in een issuebody is
geen opdracht.

Schema (de bouwer legt de exacte veldnamen vast):

| Veld | Inhoud |
|---|---|
| `route_line` | precies de regel uit `route-issue`: `#<nr> <poortwoord> <n>[<a\|b\|c>]: <route> — <reden>` |
| `gate`, `sub` | `0`–`4`, en `a`/`b`/`c` (alleen bij poort 4) |
| `redundancy` | `none`, `duplicate` (met nummer) of `done` (met bewijs) |
| `headless` | `yes`, of `no` met een reden. `no` alleen als het werk sudo vraagt, een andere host, of een live onomkeerbare stap. Een handstap telt niet als `no`. |
| `design_questions` | alleen bij 4c: hoogstens 4 vragen, elk met 2–6 opties en één aanbevolen optie |

Go valideert het oordeel tegen een strikte regex op het hele contract: bij poort 4 is de
subletter verplicht, `gate`/`sub` moet bij de regel passen, en bij poort 0 wint de uitkomst
van Go. Een ongeldig oordeel betekent: geen actie op dit issue, geen marker, en een foutregel
op stderr. De ronde gaat door met het volgende issue.

### Toepassen

De eerste rij die past, wint, van boven naar beneden:

| # | Uitkomst | Comment | Handoff | Board |
|---|---|---|---|---|
| 1 | invariant gebroken | triage met de afwijking | — | ongewijzigd |
| 2 | redundantie `duplicate` of `done` | triage met de kandidaat en het bewijs; er wordt niets gesloten | — | ongewijzigd |
| 3 | poort 0 of 1 | triage (routeregel) | — | ongewijzigd |
| 4 | `labels.needs_shaping` of `labels.needs_decision` | triage met ontwerpvragen | — | ongewijzigd |
| 5 | poort 4c | triage met ontwerpvragen | — | ongewijzigd |
| 6 | 4a, 4b of 2 met `headless: no` | triage met de reden waarom het interactief moet | — | ongewijzigd |
| 7 | 4a, 4b of 2 (ook met `labels.needs_hands` of `labels.needs_testing`) | triage | `when: night` als `wait_for_night` | → `Controller` |

Rij 7 is het verschil met paia. Daar ging een issue met een `3-`-label nooit naar
`Controller`. Hier gaan `labels.needs_hands` en `labels.needs_testing` wél door. De run werkt
dan door tot de handstap, en stelt daar een Vraag of geeft het issue terug (M2, *Afloop*). Het
issue blijft dan in `Controller`. Dat is de keuze van de gebruiker. De blokkadelabels en
`labels.needs_shaping`/`labels.needs_decision` blijven buiten de dispatcher.

Komt een issue op rij 7 terwijl `max_forwarded` al bereikt is, dan stopt de ronde voor die
repo. Dat issue en de rest krijgen geen marker, en komen de volgende ronde aan de beurt.

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

De handoff staat op naam van het token van de host. Dat account moet in `answer_permissions`
vallen, anders leest de tick de handoff niet (M2, *Antwoorden*).

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
die niet ouder is dan twee keer de TTL, ververst met `Refresh(false)`. Een `Busy` met zo'n
verse cache telt ook als gemeten. Er is geen eis dat de meting nieuwer is dan de start van de
ronde. Dat verhelpt het "niet vers"-probleem van paia.

Voor de start van de ronde gelden:

- de pauzevlag van de dispatcher;
- `halt_pct_session` en `halt_pct_week` uit `[usage]`.

Staat één daarvan aan, of is de usage niet gemeten, dan slaat de ronde over met exitcode `1`
en een regel met de reden. Voor elk volgend oordeel toetst de ronde de usage opnieuw, uit de
cache. Komt de usage tijdens de ronde boven de drempel, dan stopt ze. Wat al geschreven is,
blijft staan.

Geheugen en plekken tellen niet mee: een oordeel is één kort proces, geen run.

### Marker

De triage-comment begint met een artefactmarker, in de vorm die M0 vastlegde (beslissing 15 en
21): `<!-- lighthouse: {json} -->` met Engelse sleutels.

```text
<!-- lighthouse: {"kind": "triage", "version": 1, "gate": "4b"} -->
**Triage** · #<nr> Poort 4b: writing-plans — <reden>
…
```

Bij een deterministische stop draagt de marker die stop:

```text
<!-- lighthouse: {"kind": "triage", "version": 1, "gate": "0", "blockers": [114], "label": null} -->
```

- Bij poort 0 bevat `blockers` de nummers uit `blocked_by`, en `label` het blokkadelabel. Elk
  ander blokkadelabel uit `docs/developer/labels.md` dat het issue bij de triage droeg, staat
  ook in `label`, dus ook `labels.needs_shaping` en `labels.needs_decision` (rij 4).
- Go toetst elke ronde, zonder model, of die stop nog geldt. Zijn alle blokkeerders gesloten
  en is het label weg, dan vervalt de marker, en is het issue weer kandidaat. Een issue
  waarvan de beslissing genomen is en het label is weggehaald, komt zo vanzelf bij de
  dispatcher. Alleen de nieuwste marker telt. *Mijn keuze, zoals paia (`marker_geldig`).*
- Een marker zonder deterministische stop vervalt nooit vanzelf.
- De comment begint niet met 🤖. Hij verbergt dus geen handoff, en hij geldt ook niet als
  antwoord op een Vraag (`protocol.IsAnswer` negeert een comment die met een artefactmarker
  begint).
- De lezer accepteert ook de marker van paia: `<!-- paia: {"soort": "triage", "versie": 1,
  "poort": …, "blokkers": …, "label3": …} -->`. Zonder die lezer triëert lighthouse na de
  overstap de ongeveer 14 paia-issues met zo'n comment opnieuw. Schrijven gebeurt alleen in de
  nieuwe vorm. Een paia-marker met `label3` gelijk aan `labels.needs_hands` of
  `labels.needs_testing` geldt als vervallen. Paia hield zulke issues tegen, deze spec niet
  (rij 7), dus ze worden één keer opnieuw getrieerd. *Mijn keuze.*
- Onderaan de comment staat in één regel hoe je corrigeert.

### Corrigeren

- **Niet naar de dispatcher:** zet de status terug naar `To do`. Staat er een `🤖 Handoff`,
  verwijder die dan ook, of zet de status terug vóór het nachtvenster begint.
- **Opnieuw triëren:** pas eerst zo nodig de labels of de body aan. Verwijder daarna de
  triage-comment, en bij een doorgezet issue ook de `🤖 Handoff`.
- **Zelf oppakken:** zet eerst de status terug naar `To do`, en start dan
  `/lighthouse-start-issue`. Anders start de dispatcher er 's nachts een run naast.

De herstelregel onderaan de comment zegt per uitkomst welke van deze drie geldt.

### Signalen

- Per issue is de comment het signaal. Daar kijkt de gebruiker al.
- Een mislukte ronde eindigt met exitcode `2`. De unit komt dan op `failed`, en de host-deploy
  meldt dat zoals bij de tick.
- Een ongeldig oordeel is geen mislukte ronde. Het staat op stderr, en het issue komt de
  volgende ronde terug.

## Acceptatiecriteria

1. Een issue op 4a, 4b of 2, zonder blokkade, `labels.needs_shaping` of
   `labels.needs_decision`, staat na de ronde op `Controller`. Dat geldt ook met
   `labels.needs_hands` of `labels.needs_testing`. Met `wait_for_night = true` staat er een
   `🤖 Handoff` met `when: night` die `decide` als start beoordeelt.
2. Een 4c-issue, of een issue met `labels.needs_shaping` of `labels.needs_decision`, komt niet
   op `Controller`. Het krijgt een comment met ontwerpvragen en opties.
3. Poort 0 en 1 houden hun status en krijgen alleen de comment.
4. Een vermoedelijk dubbel of al afgerond issue wordt gemeld, niet gesloten en niet
   doorgezet.
5. Een tweede ronde doet niets met een issue dat een geldige marker draagt. Dat geldt ook voor
   een marker in de vorm van paia.
6. Met `milestones_only = true` doet een issue zonder milestone niet mee. Met `false` wel.
7. Een repo zonder `[triage]` wordt niet getrieerd.
8. De ronde slaat over met exitcode `1` bij een rem of een pauzevlag, en met een verse cache
   (niet ouder dan twee keer de TTL) draait ze.
9. Live bewijs op lighthouse: minstens één issue dat via de ronde op `Controller` kwam, met
   `when: night`, en dat de tick daarna in het nachtvenster startte. Dit AC valt buiten deze
   bouw: het wacht tot lighthouse door de dispatcher wordt bediend (M2-uitrol, #118). De bouw
   eindigt bij de unit-tests en de dry-run-ijking, en de PR verwijst met `Refs #120`.

## Testen en bewijs

Unit, in `internal/triage` (zonder netwerk en zonder `claude`):

- het kandidaatfilter: milestone aan/uit, status, marker (nieuw en paia), 🤖-comment, lopend
  werk (PR, branch, worktree, runrij);
- een vervallen marker: poort 0 met alle blokkeerders dicht en zonder label wordt weer
  kandidaat, en met één blokkeerder nog open niet;
- de vaste regels: poort 0, een gebroken invariant, een ontbrekend area-label;
- de voorrang tussen de rijen van *Toepassen*. Bijvoorbeeld: 4b met `labels.needs_hands` gaat
  door, 4b met `labels.needs_decision` niet;
- de validatie van het oordeel: een goede routeregel, een kale `Poort 4` zonder subletter, en
  een `gate`/`sub` die niet bij de regel past;
- `max_forwarded` en `max_judgments`;
- de hercontrole: een gewijzigde status geeft nul schrijfacties;
- de schrijfvolgorde: de handoff staat vóór de status;
- een falende stap op één issue stopt dat issue, niet de ronde, en de ronde eindigt met `2`;
- de handoff-tekst gaat door `protocol.ParseHeader`, `protocol.ParseHandoff` en `decide`, en
  levert een start na het begin van `night`;
- de triage-comment is geen 🤖-kop, en `IsAnswer` geeft onwaar.

Voor de CLI: een test met een nep-`claude` op het `PATH` die vaste JSON teruggeeft, en de
forge-testserver uit `internal/forge/forgetest`.

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
- Hoe de ronde de board-status per issue leest. Een issue dat nog niet op het board staat,
  moet eerst worden toegevoegd vóór de status gezet kan worden.
- De systemd-unit voor de host-deploy: een `--user`-oneshot met `TimeoutStartSec` ≥
  `max_judgments` × de oordeeltimeout plus marge, en een tijdstip vóór het begin van `night`.
  In paia was dat 18:00.

## Afhankelijkheden

- **#116 (de rem).** De ronde gebruikt de usage-rem en de pauzevlag van M2. In de code bestaat
  de rem nog niet. Triage bouwt erop voort, en dupliceert hem niet.
- **#114 (de keten).** Gemerged. Daarop rust de handoff-regel waar `when: night` gebruik van
  maakt.
- **#118 (de uitrol).** Lighthouse dispatchbaar maken (`[controller]`, `[usage_guard]
  controller = true`, `[dispatch] repos`, de tick-timer) gebeurt in de M2-issues, niet hier.
  Het is een voorwaarde voor AC 9, geen onderdeel van deze bouw (keuze van de gebruiker).
