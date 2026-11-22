# Ontwerp: instapniveaus (#19)

Issue: [#19](https://github.com/CorneeldH/lighthouse/issues/19) · Milestone: M3 — Tweede omgeving ·
Datum: 2026-11-22 · Bouwt op [de M3-spec](2026-11-07-m3-tweede-omgeving-design.md), §3 en §4.

## Aanleiding

Een nieuwe gebruiker moet met lighthouse kunnen beginnen zonder meteen alle kwaliteitseisen. Daarom
komen er benoemde instapniveaus. Elk niveau is een vaste set gates, checks en skills, zodat een repo
stap voor stap checks kan bijschakelen.

We hebben de Claude Code-transcripten van de tweede gebruiker van M3 geanalyseerd: ongeveer 570
sessies in oktober 2026, native Windows met de VS Code-extensie. Wat dat opleverde:

- **Niet weinig gereedschap, maar ander gereedschap.** De gebruiker is product owner en speltester
  en schrijft zelf geen code. Via Claude draait hij een systeem dat zwaarder is dan lighthouse:
  2.083 subagents, eigen guard-hooks, een eigen takenlijst in SQLite en 485 worktrees.
- **Botsingen met de keten.** Zijn opzet botst op vijf punten met lighthouse:
  - De trunk schoof in een maand van `release/r1` naar `release/r5.11`, met pushes rechtstreeks
    erop.
  - Er zijn geen GitHub-issues en geen PR's.
  - Er zijn 213 auto-memory-bestanden, gesplitst over `D:` en `d:`.
  - De worktrees staan in OneDrive.
  - `bypassPermissions` is de standaardmodus.
- **Wat hij al doet:** een worktree per branch, nooit `--no-verify`, en testen vóór het landen.

De conclusie van het gesprek: lighthouse leidt, de opzet van de gebruiker beweegt. Een niveau is
een oprit naar de werkwijze van lighthouse, geen aanpassing aan een andere werkwijze. De
kernregels gelden daarom op elk niveau. Alleen de kwaliteitschecks schalen.

## Doelen

- Minstens drie niveaus, `light`, `standard` en `strict`, elk vastgelegd als data. Per niveau staat
  erbij wat het overslaat en waarom.
- Een repo kiest een niveau met één regel in `lighthouse.toml`. Losse instellingen overschrijven
  het niveau.
- `lighthouse init` installeert alleen wat het niveau aanzet. `lighthouse init --upgrade --level <naam>`
  schakelt bij of af zonder handwerk.
- Een check kan op `log` staan. Hij blokkeert dan niet en meldt niets in de chat, maar telt wat hij
  had gemeld. Die telling helpt kiezen wanneer een repo opschaalt, voor de checks die gemeten zijn.
- Een niveau zet de vlag `--labels` van #95.

## Niet-doelen

- **Zelf niveaus bouwen.** Dat komt later, maar het formaat moet het al toelaten (zie "Formaat").
  Deze spec levert alleen de drie vaste niveaus en losse overrides per onderdeel.
- **Knoppen die nog niet bestaan.** Die worden eigen issues in M3. Het niveau zet ze zodra ze er
  zijn, zoals bij `--labels` van #95:
  - de test-gate per PR (#495);
  - de plek van de worktrees (#496);
  - `memory-gate` en auto-memory uit (#497);
  - hoe streng de issue-eis van `pr-create-gate` is (#498);
  - de check op hooks na de installatie (#493);
  - de analyse van de historie (#491).
- **De naamcontrole van #142 op de eigen hooks van een project.** Die komt op `strict`, maar bestaat
  nog niet. Hij wordt een eigen issue zodra `strict` meer nodig heeft.
- **Controller-knoppen.** Die zijn verhuisd naar #255. Daar hoort ook bij: de dispatcher roept skills
  hard aan (`internal/dispatch/judge.go:31,42-46`), en die skills kunnen op een niveau uit staan. #255
  moet dus `components.toml` lezen *(eigen keuze na spec-review, r1-f19)*.

## Anti-doelen

Het ontwerp is mislukt als een van deze dingen gebeurt, ook als het technisch werkt:

- Een vast niveau (`light`, `standard`, `strict`) zet een kernregel van de keten uit: nooit op de
  trunk, eerst een worktree, elke wijziging via een PR.
- Een repo werkt buiten de keten en niemand merkt het. Een override (en later een eigen niveau) mag
  een kerngate wel uitzetten. Lighthouse meldt dat dan bij elke Claude Code-sessie, en in het managed
  block in `AGENTS.md` voor de andere harnesses (zie "Kerngates uitzetten").
- Een niveau beantwoordt een permissie- of securityvraag automatisch, of voegt `permissions.allow`
  toe. Bron: besluit uit `CorneeldH/personal_ai_agent#1354`.
- Een bestaande repo zonder `level` verandert van gedrag na `init --upgrade`.
- Een check op `log` zet een melding in de chat. Dat is ruis voor wie hookmeldingen niet leest.

## Aanpakken

Er waren drie manieren:

- **A. Alleen installeren per niveau.** Een onderdeel is er, of het is er niet. Dit volgt de AC
  letterlijk, maar `log` kan dan niet: een check zou er óf niet zijn, óf blokkeren.
- **B. Alles installeren, met een stand per hook die tijdens het draaien gelezen wordt.** Opschalen
  is dan één regel aanpassen, en `log` komt vanzelf mee. Maar een uitgezette hook draait toch, en een
  kapotte hook op Windows breekt ook op `light`. Het buigt AC 3 ("installeert alleen …").
- **C. Combinatie — gekozen.** De stand `off` betekent: niet geïnstalleerd (A). De standen `log` en
  `on` leest de hook tijdens het draaien (B). De core-hooks zijn Go (`lighthouse hook <name>`) en
  lezen de configuratie al, dus de stand lezen kost weinig. De hooks van de taalpakketten zijn
  shellscripts. Die kennen alleen `off` en `on` (zie "Stand `log`").

De gebruiker koos C.

## Ontwerp

### Begrippen

- **Onderdeel** (*component*): een eenheid die het niveau aan- of uitzet. Er zijn twee soorten:
  - een hook, met al zijn registraties, bijvoorbeeld `commit-test-gate` (pre, post en postfailure);
  - een skill, met de bijbehorende agents, bijvoorbeeld `start-issue` met `issue-impact-scout`.
- **Basis**: units die buiten elk onderdeel vallen en altijd geïnstalleerd worden. Dat zijn:
  - het managed block in `AGENTS.md`;
  - `headings.toml`;
  - het PR-template;
  - de workflows;
  - de permissions;
  - `run.sh`, met `blocking`. Die lijst wordt per niveau gegenereerd (zie "Stand `log`");
  - de infrahooks `usage-gate`, `permission-check` en `workflow-push-gate`. `usage-gate` blokkeert
    één keer per venster, op elk niveau; de andere twee blokkeren nooit, of alleen in een
    controller-run *(eigen keuze na spec-review, r1-f13)*;
  - `level-check` (nieuw, zie "Kerngates uitzetten");
  - scripts in `scripts/` die meer dan één onderdeel gebruikt. Een script van één hook hoort bij het
    onderdeel van die hook, bijvoorbeeld `shellcheck-bin.sh` bij `shell-lint-check`
    *(eigen keuze na spec-review, r1-f15)*.
- **Stand**: `off`, `log` of `on`.
  - `off`: het onderdeel is niet geïnstalleerd.
  - `log`: de hook draait, blokkeert niet, zegt niets in de chat en telt wat hij had gemeld.
  - `on`: het gedrag van vandaag. Een gate blokkeert, en `worktree-check` waarschuwt.

  Een skill kent alleen `off` en `on`.
- **Niveau**: een naam met een stand per onderdeel en de waarde van de vlaggen, zoals `labels`.

### De ladder

| Onderdeel | `light` | `standard` | `strict` | Waarom op `light` |
|---|---|---|---|---|
| `main-commit-gate` | on | on | on | kernregel, de grootste lacune bij de tweede gebruiker |
| `worktree-check` | on | on | on | kernregel; waarschuwt alleen |
| `pr-create-gate` | on | on | on | kernregel; hoe streng de issue-eis is, regelt #498 |
| `bash-write-check` | on | on | on | drager; past de standen van de checks hieronder toe |
| `commit-test-gate` | off | on | on | PR's en de trunk dragen de kwaliteit eerst; #495 maakt `standard` "per PR" |
| `md-link-check` | log | on | on | veel `.md`-schrijfacties, veel ruis bij de eerste stap |
| `code-language-check` | log | on | on | doet alleen iets met `[project] code_language` |
| hooks van taalpakketten (`gofmt-check`, `go-lint-gate`, `ruff-format-check`, `ty-gate`, `shell-lint-check`, …) | off | on | on | lint en format komen na de werkwijze |
| skills `start-issue`, `route-issue`, `forge`, `write-pr`, `close-issue`, `write-handoff`, `place-instruction`, `upgrade` | on | on | on | de keten van issue tot PR en tot een gesloten issue, plus de weg omhoog. `write-pr` schrijft altijd `Refs #N` en laat het sluiten na de merge aan `close-issue` |
| skills `write-issue`, `fix-pr`, `audit-worktrees`, `audit-agent-config`, `test-standard` | off | on | on | onderhoud van de keten |
| skills `review-spec`, `review-plan` | off | off | on | vragen Codex en een tweede modelfamilie |
| standaardskills van taalpakketten (`go-standard`, …) | off | on | on | volgen hun hooks: hun tekst beschrijft `gofmt-check` en `go-lint-gate` als actief, en die staan op `light` uit |
| vlag `labels` (#95) | uit | aan | aan | labels horen bij het issuebeheer van `standard` |

**Skills die andere skills aanroepen** (*mijn keuze*). Alleen een echte overdracht telt: een stap
die de skill aanroept als vervolg, zoals `start-issue` → `route-issue`. Een vermelding telt niet,
bijvoorbeeld "zie ook `write-issue`". Een skill die de inhoud van een andere skill gebruikt, telt
wel: `write-pr` verwijst naar de ernstregels van `fix-pr` §6 *(eigen keuze na spec-review,
r1-f18)*. Zo'n verwijzing wordt een `calls`, of de regel komt in de tekst zelf. De overdrachten
staan expliciet in `levels.toml` (`calls`). Een test eist dat elke skill die aan staat, alleen skills aanroept die ook aan staan.
Bij een conflict wint de tabel. De bouwer loopt dan de tekst van de skill na: een harde overdracht
naar een skill die uit staat, wordt voorwaardelijk ("als aanwezig") of verhuist mee naar het lagere
niveau.

**`--no-verify` blijft op elk niveau geblokkeerd** (*mijn keuze*). Het managed block noemt het een
kernregel. Vandaag zit die blokkade in `commit-test-gate pre`. Op `light` staat de test-gate uit,
dus de blokkade moet ergens anders blijven. De bouwer kiest:
- de blokkade verhuist naar `main-commit-gate`;
- of `commit-test-gate` wordt gesplitst in twee onderdelen.

Lighthouse zelf draait op `strict`.

### Formaat

Een niveau is data. Elk pakket levert een `levels.toml` met de standen van zijn eigen onderdelen.
Het pakket kopieert dat bestand niet; het is pakketmetadata, net als `guards.toml`.

```toml
# pack/core/levels.toml
[light.components]
main-commit-gate = "on"
commit-test-gate = "off"
md-link-check = "log"
"skill:write-issue" = "off"

[light.flags]
labels = false
```

Een onderdeel dat het niveau niet noemt, staat `on`. Zo valt een nieuw onderdeel nooit stil weg.
`init` meldt het wel, zodat de levels-tabel wordt bijgewerkt. Die melding gaat naar stderr, net als
alle andere meldingen van dit ontwerp. De dogfood-CI eist dat stdout precies `No changes.` is
(`.github/workflows/ci.yml:183-185`) *(eigen keuze na spec-review, r1-f17)*.

De repo kiest in `lighthouse.toml`:

```toml
level = "light"

[components]            # optioneel: overrides, winnen van het niveau
md-link-check = "on"
```

Zelf niveaus bouwen (later, geen doel van deze spec) wordt dezelfde vorm in `lighthouse.toml`:
`[levels.<naam>]` met `base = "standard"` en eigen `components`. Het ontwerp hier mag dat niet
uitsluiten. Daarom komt een niveau overal uit één functie die naam plus overrides oplost tot een
stand per onderdeel.

### Gedrag van `init`

- **Nieuwe repo:** `lighthouse init` zonder `--level` kiest `light` en schrijft `level = "light"` in
  `lighthouse.toml`, in de diff met één `y`. De samenvatting noemt het niveau en hoe je opschaalt.
- **De sleutel `level` schrijven.** De bestaande schrijver `projectconfig.AppendMissing` werkt per
  tabel, zet een ontbrekende tabel achteraan en overschrijft nooit een waarde
  (`internal/projectconfig/projectconfig.go:1-6`). Daarmee kan `level` niet gezet of gewisseld worden.
  Er komt een eigen schrijver voor de sleutel `level` op het hoogste niveau. Die voegt hem in vóór
  de eerste tabelkop, of vervangt de bestaande waarde, en laat commentaar staan. Hij krijgt tests
  voor drie gevallen: zetten, vervangen, en een bestand zonder tabellen.
- **Bestaande repo zonder `level`:** die gedraagt zich als `strict`. `strict` met de knoppen die nog
  niet bestaan, is precies het gedrag van vandaag. `init --upgrade` schrijft de sleutel niet
  stilletjes bij.
- **Wisselen:** `lighthouse init --upgrade --level <naam>` zet de regel en installeert het verschil.
  Omlaag haalt units weg, met dezelfde reconcile als bij een weggehaald pakket. Wie de regel met de
  hand aanpast en daarna `init --upgrade` draait, krijgt hetzelfde resultaat.
- **Een wissel herkennen.** `components.toml` legt het opgeloste niveau vast, samen met het moment
  waarop het gezet is (`level`, `since`). `init` vergelijkt het niveau uit `lighthouse.toml` met dat
  vastgelegde niveau. Wijken ze af, dan is het een wissel, ook als niemand `--level` meegaf
  *(eigen keuze na spec-review, r1-f4, r1-f16)*.
- **De vlag `labels` van een niveau** (*mijn keuze*). Die werkt alleen bij een wissel zoals hierboven
  herkend, of bij de eerste `init`. Op dat moment werkt hij als `--labels`. Een `init --upgrade`
  zonder wissel doet niets met labels, en een repo zonder `level` ook niet. Zo blijft de dogfood-CI
  op `No changes.`, zonder netwerkaanroep. `--create-labels` blijft een apart akkoord, omdat het
  buiten de repo schrijft.
- **Waar de oplossing gebeurt:** `init` lost niveau en overrides op tot één geïnstalleerd bestand,
  `.lighthouse/components.toml`. Hooks lezen alleen dat bestand en kennen geen niveaulogica. Een
  wijziging in `level` of `[components]` werkt dus pas na `init --upgrade`. Zo werkt `off` (niet
  geïnstalleerd) ook al.
- **Het bestand als unit.** Geen pakket levert dit bestand; `init` maakt het zelf. Het is een
  `file`-unit met `Package = "core"`, en `.lighthouse/components.toml` komt bij de toegestane
  installatiepaden (`internal/packs/packs.go:202-223`). Het bestand krijgt geen marker: de
  markerfunctie laat `.toml` ongemoeid (`internal/packs/marker.go:15-37`). De SHA in het manifest
  is genoeg *(eigen keuze na spec-review, r1-f12)*.
- **Ontbreekt `components.toml`, dan staat alles `on`.** Dat is het gedrag van vandaag. Het geldt
  voor elke repo tussen een nieuwe binary (`lighthouse self-update`) en de volgende
  `init --upgrade`. Alleen een bestand dat er is maar niet te lezen is, geeft `COULD NOT CHECK`
  *(eigen keuze na spec-review, r1-f11)*.
- **Vereisten filteren.** De dependency-check telt alleen de vereisten van onderdelen die aan
  staan. Een verse `light`-installatie meldt dus geen ontbrekende `codex`
  (`pack/core/requires.toml`, `internal/initcmd/initcmd.go:184-186`)
  *(eigen keuze na spec-review, r1-f6)*.
- **Permissions:** die zijn op elk niveau gelijk. Een niveau raakt geen `permission`-units.

### Kerngates uitzetten

De kerngates zijn `main-commit-gate`, `worktree-check` en `pr-create-gate`. Een override in
`[components]` mag zo'n gate op `off` of `log` zetten, en later mag een eigen niveau dat ook. Het
besluit van de gebruiker: wie per se wil, kan dat. Maar de waarschuwing komt vaak terug, zodat de
repo niet ongemerkt buiten de keten werkt.

- **Bij elke sessie in Claude Code.** Er komt een nieuw basisonderdeel `level-check`, op SessionStart. Het blokkeert
  nooit en draait op elk niveau. Het leest `components.toml`. Staat een kerngate niet op `on`, dan
  geeft het twee dingen:
  - een `systemMessage` aan de gebruiker, bijvoorbeeld "lighthouse: main-commit-gate staat uit —
    deze repo werkt buiten de keten";
  - dezelfde regel als `additionalContext` aan het model, zodat de agent weet dat de regel niet
    bewaakt wordt.

  Kan `level-check` het bestand niet lezen, dan meldt hij `COULD NOT CHECK` en blokkeert niets.
- **In Codex en OpenCode.** Die harnesses draaien geen hooks uit `.claude/settings.json`
  (`AGENTS.md`, *Outside Claude Code*). Daar staat de waarschuwing vast in het managed block in
  `AGENTS.md`. Zolang een kerngate uit staat, schrijft `init` per uitgezette gate een regel in het
  block. Die regel staat er dus in elke sessie *(eigen keuze na spec-review, r1-f1)*.
- **Bij `init` en `lighthouse level`:** dezelfde waarschuwing, per uitgezette kerngate.
- **De validatie** accepteert de override, maar geeft bij `init` een waarschuwing en geen fout.
- **`--no-verify`** blijft ook dan geblokkeerd. Die blokkade is geen kerngate die je kunt uitzetten.

### Stand `log`

- Een hook op `log` voert zijn check uit. In plaats van te blokkeren of te waarschuwen schrijft hij
  één regel naar een lokaal logboek per repo, met onderdeel, tijd en de eerste regel van de melding.
- **Wat "per repo" is.** Het logboek hoort bij de gedeelde repository, niet bij de worktree. De
  sleutel is `git rev-parse --git-common-dir`. Alle worktrees schrijven dus in hetzelfde logboek,
  en de tellingen blijven staan als een worktree na de merge weg is
  *(eigen keuze na spec-review, r1-f10)*.
- Hij geeft geen `systemMessage` en geen stderr aan Claude Code, en de exit-code is 0.
- **Ook als de hook zelf niet kan draaien.** `run.sh` behandelt een hook uit de lijst `blocking` als
  blokkerend als de binary ontbreekt of te oud is (`pack/core/hooks/run.sh:37-60`). Daarom maakt
  `init` die lijst per niveau aan. De bron blijft `pack/core/hooks/blocking`. `init` filtert die
  lijst: een onderdeel op `off` of `log` valt eruit, de rest blijft staan. Basishooks zoals
  `workflow-push-gate` blijven er dus in. Een hook die nooit blokkeert, zoals `worktree-check`, komt
  er nooit bij *(eigen keuze na spec-review, r3-f1)*.

  Een onderdeel op `log` dat niet kan draaien, gaat zonder blokkade door, met exit-code 0
  *(eigen keuze na spec-review, r1-f2)*. Een test eist dat `strict` precies de lijst van vandaag
  oplevert (`pack/core/hooks/blocking`) *(eigen keuze na spec-review, r2-f1)*.
- `bash-write-check` past de standen van `md-link-check` en `code-language-check` toe op wat een
  Bash-aanroep schreef.
- `bash-write-check` geeft vandaag een melding als een gate ontbreekt: `"no " + GatePacks[gate] +
  " check installed"` (`internal/bashwrites/dispatch.go:57-58`, `runGate`). Staat een gate bewust op `off`, dan valt die melding weg
  (*mijn keuze*): het niveau heeft hem uitgezet, er ontbreekt niets.
- `log` geldt alleen voor de Go-hooks van core. De hooks van de taalpakketten zijn shellscripts en
  kennen alleen `off` en `on` (*mijn keuze*). De validatie weigert `log` voor die hooks. Ze naar
  `log` brengen is werk voor later.
- `lighthouse level` (alleen lezen) toont:
  - het niveau en de stand per onderdeel;
  - per onderdeel op `log`, hoe vaak het had gemeld sinds `since` uit `components.toml`;
  - welke onderdelen van het volgende niveau op dit niveau `off` staan en dus niet gemeten zijn.
    Op `light` zijn dat de test-gate en de hooks van de taalpakketten.
- Er komt geen oordeel "klaar voor `standard`". De tellingen gaan alleen over de checks op `log`.
  Het volgende niveau zet ook checks aan die niemand gemeten heeft, zoals de test-gate. Een kale
  nul zou dus beloven wat lighthouse niet weet *(eigen keuze na spec-review, r1-f3)*.

### Gevolgen voor bestaande onderdelen

- **De pakketlader** (`internal/packs`): `levels.toml` inlezen en elke unit aan één onderdeel of aan
  de basis koppelen. Een test eist dat elke unit van elk pakket precies één plek heeft.
- **De selectie in `init`** (`internal/initcmd`, naast `dropWorkflows`): units van onderdelen op
  `off` vallen weg, `components.toml` komt erbij, en de vlag `--level` komt erbij.
- **De configuratie** (`internal/config`): de sleutels `level` en `[components]`. `config.Load`
  controleert alleen de vorm en de standwaarden (`off`, `log`, `on`). Een onbekende naam is geen fout.
  `config.Load` draait in elke gate, en een fout daar blokkeert elke commit
  (`internal/hooks/maincommitgate.go:61-69`). De namen van niveaus en onderdelen hangen af van de
  pakketten. Die controleren alleen `init` en `lighthouse level`, met de lijst geldige namen, en
  alleen daar geeft een onbekende naam een fout *(eigen keuze na spec-review, r1-f9)*.
- **Hooks** (`internal/hooks`): de stand lezen uit `components.toml` (en nooit uit `lighthouse.toml`),
  en de stand `log`.
- **Het managed block in `AGENTS.md`** noemt alleen de skills en gates van het geïnstalleerde
  niveau. Het block is per pakket; de bouwer kiest of het block een niveau-variant krijgt of
  gefilterd wordt.
- **Dogfood:** lighthouse zet `level = "strict"` expliciet. CI eist nog steeds `No changes.`
- **Installatiegids (#252, Engels):** een korte instaproute. Begin op `light`, kijk met
  `lighthouse level` naar de tellingen, en schaal op met `init --upgrade --level standard`.

## Aan de bouwer

- Hoe de koppeling unit → onderdeel werkt. De voorkeur gaat naar een afleiding uit de naam:
  `run.sh <name>` of `<name>.sh` voor een hook, `skills/<name>/` voor een skill. Een expliciete lijst
  in `levels.toml` voor de agents. De test op "precies één plek" is verplicht.
- Waar het logboek van `log` staat. De eisen: lokaal, buiten de getrackte bestanden van de repo,
  één logboek per gedeelde repository (zie "Stand `log`"), en werkend op native Windows.
  `hookStateDir` (`internal/cli/hook.go:64`) is per gebruiker en heeft dus een sleutel per repo
  nodig.
- Of het managed block gefilterd wordt of een variant per niveau krijgt (zie hierboven).

## Bouwvolgorde

Dit ontwerp is meer dan één plan aan werk. Het plan bouwt het in drie delen, elk met een groene
PR-grens *(eigen keuze na spec-review, r1-f20)*:

1. **Niveaus en selectie:**
   - `levels.toml` en de pakketlader;
   - de sleutels `level` en `[components]`, met de schrijver voor `level`;
   - de selectie in `init`, `components.toml` en `--level`;
   - de vereisten filteren;
   - de dogfood op `strict`.
2. **De stand `log`:**
   - het logboek;
   - `blocking` per niveau;
   - `bash-write-check`;
   - `lighthouse level`.
3. **Teksten en de kerngate-waarschuwing:**
   - `level-check`;
   - de regel in het managed block;
   - de tekst van de skills (`calls`);
   - de verhuizing van `--no-verify`;
   - de installatiegids.

## Acceptatiecriteria van #19, gekoppeld

| AC | Waar in dit ontwerp |
|---|---|
| Minstens twee niveaus, met wat ze overslaan en waarom | De ladder |
| Eén regel in `lighthouse.toml`, overrides winnen | Formaat |
| `init` installeert alleen het niveau; `--upgrade` schakelt bij | Gedrag van `init` |
| Een niveau zet `--labels` | De ladder, vlag `labels` |
| Een instaproute in de installatiegids | Gevolgen, installatiegids |
| Geen niveau beantwoordt permissies automatisch | Anti-doelen; permissions gelijk op elk niveau |
| Een tweede repo start met het lichtste niveau | Gedrag van `init`: een nieuwe repo kiest `light` |

## Open na review

Ronde 3 haalde de rondegrens met deze punten open. Ze zijn aanvaard als `accepted_open`;
de volgende fase of de PR-review weegt ze opnieuw.

- **r3-f1** (MAJOR, Stand `log` — "Ook als de hook zelf niet kan draaien"): The generation rule ("de blokkerende basishooks" plus "de onderdelen op `on`") does not yield today's list on `strict`, so the new test and the rule contradict each other. `worktree-check` is a hook component, runs through `run.sh`, is `on` on every level, and is not in today's `blocking` list. It only warns.
