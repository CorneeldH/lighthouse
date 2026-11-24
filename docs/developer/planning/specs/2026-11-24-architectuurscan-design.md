# Architectuurscan periodiek en onbemand — ontwerp

Issue: [#386](https://github.com/CorneeldH/lighthouse/issues/386). Milestone: *Kwaliteitspoorten per
taalpack* (K6 in [`2026-11-16-zelflerende-lighthouse-design.md`](2026-11-16-zelflerende-lighthouse-design.md)).
Geblokkeerd door [#375](https://github.com/CorneeldH/lighthouse/issues/375) voor één criterium
(tokenverbruik); de rest van dit ontwerp staat daar los van. Het plan wacht op de merge van #375.

## Bedoeling

Een architectuurscan zoekt kandidaten voor verbetering: hotspots, ondiepe modules, dubbele bronnen
van waarheid. Zo'n scan loont (in paia leverde de eerste vijf issues en drie aanvullingen op), maar
draait alleen als iemand eraan denkt. Dit werk brengt de scan-skill van paia naar `pack/core` en laat
haar op een schema onbemand draaien, in elke repo die dat aanzet. Het resultaat is één beslisissue:
de gebruiker kiest daaruit welke kandidaten echte issues worden en wijst de rest af.

## Doelen

- De skill staat in `pack/core` en werkt interactief zoals in paia: kandidaten in de chat.
- Een geplande run (job, `[[schedule]]`) draait de skill headless, standaard eens per kwartaal.
- Er staat per repo hoogstens één open scanissue. Een nieuwe scan werkt dat issue bij in plaats van
  een tweede te openen.
- Elke claim in het issue is gemarkeerd als *nagemeten*, *niet nagemeten* of *niet meetbaar vanaf
  hier*.
- Een kandidaat die onder een open issue valt, staat met dat nummer onder *nog open uit eerdere
  scans* en niet opnieuw als kandidaat.
- Een afgewezen kandidaat komt niet elk kwartaal terug.
- Een mislukte run maakt geen issue en geen comment.

## Niet-doelen

- Kandidaten automatisch tot issues maken. Dat doet de gebruiker, via `/lighthouse-write-issue`.
- Code wijzigen. De skill scant alleen.
- Tokenmeting bouwen. Dat doet #375; dit werk zorgt alleen dat een scanrun er net zo onder valt als
  elke andere geplande run.
- Een generieke job "draai een willekeurige skill en publiceer een issue" (zie *Aanpakken*).
- Het exitcontract van de tick veranderen (zie *Buiten scope*).

## Anti-doelen

- Een stapel scanissues die niemand leest. Daarom één issue dat bijgewerkt wordt.
- Een half issue na een mislukte run, of een tweede issue na een retry.
- Een onbemande agent met schrijfrechten op de forge, of met vrije `Bash`.
- Een meting die stilletjes een poort wordt: de scan blokkeert nooit iets.

## Randvoorwaarden

- Geen nieuwe Python buiten `pack/python/`: de job is Go (`internal/scheduled/archscan/`).
- Instructies (de skill) zijn Engels; het issue staat in `[project] language`. Vaste koppen komen uit
  `.lighthouse/headings.toml`, zodat elke taalvariant leesbaar blijft.
- Dogfood: na de wijziging onder `pack/` volgt `go run ./cmd/lighthouse init --upgrade` en een commit
  van de geïnstalleerde kopie.
- De paia-skill komt van `mattpocock-skills` (MIT). De herkomst blijft in de front matter en in
  `pack/SOURCES.toml`.

## Aanpakken

**A. De Go-job schrijft het issue (gekozen).** De agent schrijft één markdownbestand. `Check`
controleert vorm, markeringen en nummers; pas daarna publiceert Go het, in `Finish`. Een mislukte run
komt zo nooit bij issue of comment, en "precies één issue" is een eigenschap van code, niet van het
model. `map-planned` schrijft al comments en labels vanuit een job, maar doet dat in `Cleanup`, vóór
`Check`; publiceren ná `Check` is hier nieuw.

**B. De agent schrijft het issue zelf** via `lighthouse forge issue create`. Minder Go, maar "precies
één" en "mislukt = geen issue" hangen dan af van het model, en de onbemande agent heeft
forge-schrijfrechten nodig. Afgevallen.

**C. Een generieke job die elke skill headless draait en het resultaat als beslisissue publiceert.**
Herbruikbaar voor latere metingen van de milestone, maar die schrijven vooral een trend naar
`lighthouse.db`. Nu speculatief. Afgevallen; wel komt het publiceren als kleine helper in
`internal/scheduled`, zodat een latere meting die kan hergebruiken.

## Besloten met de gebruiker

1. **Aanpak A**: de Go-job publiceert, niet de agent.
2. **Eén open scanissue, bijgewerkt.** Staat er al een open scanissue, dan plaatst de run de vorige
   body als comment en herschrijft de body met de nieuwe scan. De geschiedenis blijft in de comments.
3. **Waarschuwing in het issue zelf.** Na de eerste herscan staat bovenaan de body een regel
   *Onbeslist sinds <datum eerste scan>, <n> scans*. Elke herscan plaatst een comment, zodat de
   forge-melding vanzelf komt. Geen waarschuwing buiten het issue.
4. **Afwijzen met een vinkje.** Elke kandidaat heeft in de body een vinkje `- [ ] Afgewezen`; een
   reden in een comment is optioneel. Een gekozen kandidaat wordt een issue. Is alles beslist, dan
   sluit de gebruiker het scanissue.
5. **Een mislukte run sluit als `failed`.** Het criterium *eindigt niet-nul* betekent: de run-rij
   sluit als `failed` met de reden. De tick houdt zijn exitcode (0), zoals bij elke geplande job.
   Een fout in de gate is geen mislukte run maar een mislukte tick (zie *Mislukken*).

## Ontwerp

### De skill

`pack/core/skills/architecture-scan/SKILL.md`, geïnstalleerd als `lighthouse-architecture-scan`
(Claude- en agents-variant). Basis: paia's `paia-improve-codebase-architecture`: vocabulaire
(module, interface, depth, seam, locality, leverage, deletion test), afbakening via recency, de vijf
vragen, rangschikking `Strong` / `Worth exploring` / `Speculative`.

Ontdaan van paia:

- Hotspots over de paden van `paths.search_dirs` (`lighthouse config`), niet over `src/*.py`.
  Interactief via `git log --since=… --name-only`, zoals in paia. Headless rekent `Stage` de
  hotspotlijst in Go uit en zet hem in het corpus: de headless agent heeft geen `Bash`.
  *(own choice after spec review, r1-f2)*
- Kaartdocumenten zijn optioneel: wat de repo in `[paths]` heeft staan (`tenets`, `areas`,
  `strategy`), alleen als het bestaat. Geen vaste paia-paden.
- Een gekozen kandidaat wordt een issue via `/lighthouse-write-issue`.

**Twee modi.**

- *Interactief* (geen `--out`): zoals paia, kandidaten in de chat, afsluiten met één topaanbeveling
  en de vraag welke de gebruiker wil uitwerken.
- *Headless* (`--corpus <pad> --out <pad>`): stelt geen vragen, leest issues en hotspots alleen uit
  het corpus, schrijft precies één bestand op `--out` in het vaste formaat hieronder, maakt geen
  issues, geen commit, geen push. Nameten gaat met `Read`, `Grep` en `Glob`; het *commando* van een
  nagemeten claim is die toolaanroep, of het getal uit het corpus met zijn bron.
  *(own choice after spec review, r1-f2)*

Nieuw in beide modi: elke claim draagt een van de drie markeringen, en een kandidaat die onder een
open issue valt, gaat naar *nog open uit eerdere scans*.

**Eerdere beslissingen** (headless, uit het corpus):

- Een kandidaat uit het open scanissue zonder vinkje is nog onbeslist en komt terug als volledige
  kandidaat, bijgewerkt. Het scanissue zelf telt nooit als "open issue waar de kandidaat onder valt".
- Een kandidaat met een vinkje, in het open of het laatst gesloten scanissue, komt alleen terug met
  nieuw bewijs, gemarkeerd als *eerder afgewezen, opnieuw omdat …*. Een afgewezen kandidaat blijft
  met vinkje staan, in het bijgewerkte open issue én in een nieuw scanissue na een gesloten issue,
  zodat de beslissing nooit verder terug hoeft dan het laatst gesloten issue.
  *(own choice after spec review, r1-f4)*
- Een kandidaat die een issue werd, valt onder dat issue: *nog open* zolang het openstaat.

### Het resultaatformaat

Eén markdownbestand, in `[project] language`, met vaste koppen uit `headings.toml` (nieuwe sleutels,
bijvoorbeeld `scan_candidates`, `scan_still_open`, `scan_method`):

- **Kandidaten.** Per kandidaat een `###`-kop met de naam en een stabiele sleutel (een slug, zodat een
  volgende scan dezelfde kandidaat herkent), het vinkje `- [ ] Afgewezen` (of `- [x]` voor een
  overgenomen afwijzing), dan de velden van paia (bestanden, probleem, voorstel, winst,
  rangschikking) en een lijst *claims*. Elke claim is één bullet die eindigt op precies één
  markering:
  - `[nagemeten: \`<commando>\` → <uitkomst>]`
  - `[niet nagemeten]`
  - `[niet meetbaar vanaf hier]`
- **Nog open uit eerdere scans.** Bullets `#<nr> — <kandidaat>`.
- **Methode.** Afbakening (venster, paden), basiscommit, wat niet bekeken is.
- Een topaanbeveling.

Nul kandidaten is een geldige uitkomst, met een regel die dat zegt; een leeg bestand niet.

### De job

Nieuw package `internal/scheduled/archscan/`, naam `architecture-scan`, geregistreerd in
`scheduleJobs` (`internal/repoconfig/schedule.go`) én via `Register` en de blank import in
`internal/cli/tick_scheduled.go`.

**Configuratie.** Opt-in via een `[[schedule]]`-blok. Standaardschema per kwartaal, bijvoorbeeld
`cron = "0 6 1 1,4,7,10 *"`. Lighthouse zelf zet het blok aan in zijn eigen `lighthouse.toml`, met
`params.labels = ["2-area:core"]` (een `labels` buiten `params` is een onbekende sleutel en breekt het
laden van `lighthouse.toml`). `params` naar het patroon van `reflect`:

| Param | Standaard |
|---|---|
| `command` | `/lighthouse-architecture-scan` |
| `window` | `3 months` (recency) |
| `labels` | geen; extra labels naast `needs_decision` |
| `tools` | `Read Glob Grep Write`; `Configure` weigert elke tool buiten die lijst, dus nooit `Bash` of `Agent` |
| `max_runtime` | `60m` |

**Het scanissue.** Een issue telt als scanissue als het label `needs_decision` draagt, op regel 1
van de body de marker heeft, en geschreven is door de login van het token (zoals `map-planned`
antwoorden controleert). Zijn er meerdere open, dan telt het oudste. De marker:
`<!-- lighthouse: {"kind":"architecture-scan","version":1,"run":"…","base":"<sha>","first":"<datum>","scans":<n>} -->`.
De marker staat op regel 1, de waarschuwingsregel op regel 2. *(own choice after spec review, r1-f10)*

**Gate.** Zonder forge: fout. HEAD van `origin/<standaardbranch>` gelijk aan de basiscommit in de
marker van het open scanissue: `skip` (`no commits since last scan`). De gate zet een run-id in een
nieuw veld van de `Verdict`, afgeleid van `Env.Now`; `Finish` krijgt de `Verdict` mee. Het id hoeft
alleen per run uniek te zijn. *(own choice after spec review, r1-f6)*

**Stage.** Schrijft `.lighthouse/run/architecture-scan-corpus.json`: taal, `paths.search_dirs` en de
paden uit `[paths]`, de hotspotlijst (gewijzigde bestanden per pad binnen `window`, uit `git log`),
de basiscommit, alle open issues (nummer, titel, labels, body afgekapt), en apart het open scanissue
en het laatst gesloten scanissue, elk met body (vinkjes) en comments.

**Launch.** Onbemande instructies naar het patroon van `reflect`: geen vragen, alleen het corpus
lezen, precies één bestand op `--out`, geen issue, geen commit, geen push.

**Het resultaatbestand** heeft een vast pad in de werkplek, `architecture-scan-result.md` in de root,
niet onder een map die een doelrepo kan negeren: `Postcheck` ziet alleen wat `git status` toont
(`internal/scheduled/postcheck.go:308`). *(own choice after spec review, r1-f8)*

**Cleanup.** Toont `git status` het resultaatbestand niet, of is het leeg, dan faalt de run hier:
zonder gewijzigd pad slaat `Postcheck` `Check` over (`internal/scheduled/postcheck.go:117-120`) en zou
de run anders `done` eindigen. Uitzondering: draagt het open scanissue al het run-id van deze run, dan
publiceerde een eerdere `Postcheck` al, en is een ontbrekend bestand goed. *(own choice after spec
review, r1-f7)* Daarna gaat het corpus weg. `Allowed` laat alleen het resultaatbestand toe.

**Check.** Faalt de run (en er volgt geen issue of comment) bij:

- meer dan één bestand;
- een ontbrekende verplichte kop;
- een claim zonder of met meer dan één markering, of een *nagemeten* zonder commando of uitkomst;
- een nummer onder *nog open uit eerdere scans* dat niet bestaat, niet open is, of het scanissue zelf
  is, nagelezen op de forge, nooit in het corpus;
- een vinkje dat in het resultaat verdwenen is, vergeleken met de **live** body van het open scanissue
  op de forge (of, zonder open issue, van het laatst gesloten), niet met het corpus;
  *(own choice after spec review, r1-f5)*
- een body die met marker en waarschuwing langer wordt dan 60 000 tekens (onder de GitHub-grens van
  65 536); *(own choice after spec review, r1-f11)*
- een treffer van `scheduled.ScanSecrets` op het resultaatbestand
  (`internal/scheduled/secrets.go:95`). Die scan loopt hier expliciet: de gewone secret-scan komt pas
  na `Finish` en ziet een bestand dat niet gecommit wordt niet. *(own choice after spec review,
  r1-f9)*

**Finish (publiceren).** Via de helper in `internal/scheduled`. Elke route eindigt met het
verwijderen van het resultaatbestand, ook de vroege uitgangen: blijft het staan, dan commit, pusht en
merget `Postcheck` het naar de standaardbranch (`internal/scheduled/postcheck.go:129-157`).
*(own choice after spec review, r1-f1)*

1. Draagt de marker van het open scanissue al het run-id van deze run, dan publiceert `Finish` niets:
   een herhaalde `Postcheck` (een run die openbleef) publiceert niet dubbel.
2. Geen kandidaat zonder vinkje en geen open scanissue: niets posten. Overgenomen afwijzingen tellen
   hier niet als kandidaat; ze blijven in het laatst gesloten scanissue staan.
   *(own choice after spec review, r2-f1)*
3. Geen open scanissue en minstens één kandidaat zonder vinkje: maak er een, met `needs_decision`, de
   `labels` uit de params en de marker (`scans` 1).
4. Wel een open scanissue: lees de body opnieuw van de forge. Een vinkje dat na `Check` gezet is,
   wordt in de nieuwe body overgenomen (op de sleutel van de kandidaat). Plaats dan de huidige body als
   comment (*Scan van <datum>*) en herschrijf de body met de nieuwe scan, `scans` + 1, `first`
   ongewijzigd, en de waarschuwingsregel op regel 2. *(own choice after spec review, r1-f5)*
5. Verwijder het resultaatbestand. Er blijft niets te committen: de run eindigt `done` zonder PR.

Comment eerst, body daarna: een verloren body is erger dan een dubbel comment.

**Mislukken.** Twee soorten, met een ander contract *(own choice after spec review, r1-f3)*:

- **In de gate** (geen forge, verlopen token, een forge-fout bij het lezen van het scanissue): er komt
  geen run-rij. De slot blijft open, de repo wordt vastgehouden en de tick eindigt met exitcode 1,
  zoals bij `map-planned` (`internal/cli/tick_scheduled.go:96-100`). De volgende tick probeert opnieuw.
- **Vanaf `Stage`** (lege uitvoer, een `Check`-fout, een forge-fout in `Finish`): de run sluit als
  `failed` met de reden, zonder issue of comment, en de tick houdt exitcode 0. Een forge-fout in
  `Finish` kost de scan van dat kwartaal; de volgende slot probeert opnieuw.

Een time-out volgt de gewone levenscyclus: eerst `retry`, dan een tweede poging.

### Telemetrie

Een scanrun is een gewone geplande run met run-rij en transcript. #375 leest daaruit model, tokens
en kosten per run; dit werk voegt daar niets aan toe, maar het criterium *tokenverbruik per run staat
in de telemetrie* wordt pas aangetoond als #375 gemerged is.

## Buiten scope

- **Een mislukte geplande run is niet luid zichtbaar.** Voor elke job eindigt de tick met 0 en staat de
  mislukking alleen in de run-rij. Dat is gangbaar voor een scheduler, maar wie de rij niet leest, ziet
  het niet. Een eigen issue, voor alle jobs.

## Aan de bouwer

- De exacte namen van de nieuwe `headings.toml`-sleutels en hun `en`-varianten.
- Hoe de afkapping van issuebodies in het corpus heet en hoe groot ze is (naar `max_corpus_bytes` van
  `reflect`).
- Of `skill:architecture-scan` een niveau in `pack/core/levels.toml` krijgt, naar `map-planned`.
- De docs: een sectie *Job `architecture-scan`* in `docs/tick.md`, het schemavoorbeeld in
  `docs/install.md`, een fragment in `changelog.d/`.
- Tests met een nep-forge en nep-runner naar `mapplanned/job_test.go`: nieuw issue, bijwerken met
  comment, herhaalde `Finish` zonder dubbele post, overgenomen vinkje, elke `Check`-fout zonder
  issue of comment.
