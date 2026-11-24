# Ontwerp: analyse van de Claude Code-historie bij de installatie (#491)

Issue: [#491](https://github.com/CorneeldH/lighthouse/issues/491) · Milestone: M3 — Tweede omgeving ·
Datum: 2026-11-24 · Bouwt op [de spec van #19](2026-11-22-issue-19-instapniveaus-design.md) en
[de M3-spec](2026-11-07-m3-tweede-omgeving-design.md).

## Aanleiding

Bij #19 hebben we de Claude Code-transcripten van de tweede gebruiker met de hand geanalyseerd:
ongeveer 570 sessies, 2,5 GB `.jsonl`. Dat leverde in een uur meer op dan een intakegesprek: een
trunk die in een maand van `release/r1` naar `release/r5.11` schoof, werk op vier plekken buiten
GitHub, 213 auto-memory-bestanden gesplitst over `D:` en `d:`, worktrees in OneDrive, en Git Bash
met CRLF-problemen. Zulke bevindingen bepalen hoe iemand op lighthouse instapt.

Deze spec maakt die analyse een herhaalbare stap bij de installatie. De lezer van het verslag is
**de installerende agent** uit [de agentgids](../../../site/agents/install.md). Die doet de
gebruiker daarna een voorstel, met per punt een korte uitleg.

De milestone laat open waar Claude Code bij de tweede gebruiker draait (native Windows, WSL of de
Linux-server). Dit verslag beantwoordt die vraag voor elke nieuwe gebruiker: het onderdeel `shell`
meet het.

## Doelen

- Een eigen subcommando `lighthouse history`, los van `init`. Het leest de transcripten van de repo
  waarin het draait en print één JSON-verslag op stdout.
- Drie toestemmingsniveaus in de agentgids: **niet**, **lokaal** en **diep**. Zonder toestemming
  draait de agent het commando niet.
- Het verslag noemt per onderdeel wat er gemeten is, wat dat voor lighthouse betekent en een
  voorstel. Een voorstel is een **knop** (een niveau, een `init`-vlag, een keuze uit de gids) of
  **advies** over de eigen opzet van de gebruiker.
- Bovenaan staat een aanbevolen instapniveau (`light`, `standard` of `strict`, uit #19) met de
  reden.
- Getest op synthetische transcripten met een bekende uitkomst per onderdeel.

## Niet-doelen

- **`init` aanpassen.** `init` leest het verslag niet en kiest geen niveau op basis ervan. De agent
  past een knop toe via de bestaande vlaggen (`--level`, `--labels`, …).
- **Knoppen die nog niet bestaan.** De plek van de worktrees (#496) en auto-memory uit (#497) zijn
  eigen issues. Tot ze er zijn, is het voorstel voor die bevindingen `advice`. Zodra een knop
  bestaat, wordt het voorstel een `knob`; dat is een wijziging in de detector, niet in het formaat.
- **Andere harnesses lezen.** Alleen Claude Code-transcripten (`~/.claude/projects/`). Codex- en
  OpenCode-historie valt buiten deze spec.
- **Een skill voor de analyse.** De interpretatie staat vast in Go, niet in een skill.
- **Iets wegschrijven.** Het commando schrijft geen bestand, geen `lighthouse.db`, geen config.

## Anti-doelen

Het ontwerp is mislukt als een van deze dingen gebeurt, ook als het technisch werkt:

- `lighthouse history` print een letterlijke prompt, een stuk tool-uitvoer of bestandsinhoud.
- `lighthouse history` stuurt iets over het netwerk.
- Het leest transcripten van een andere repo.
- De agent draait het commando, of leest transcripten, zonder dat de gebruiker daar op dat niveau
  ja op zei.
- Het voorstel past lighthouse aan de werkwijze van de gebruiker aan: een kernregel uit, of een
  niveau onder `light`. #19 legde vast: lighthouse leidt, de opzet van de gebruiker beweegt.

## Benaderingen

| | Benadering | Afweging | Uitkomst |
|---|---|---|---|
| A | Go-subcommando met vaste detectoren; betekenis, voorstel en niveauregel in Go | deterministisch en toetsbaar per onderdeel (AC5); geen tekst verlaat het proces; elk nieuw patroon kost code | **gekozen** als basis |
| B | De agent leest de transcripten zelf, zoals de handanalyse | flexibel; 2,5 GB past in geen context; inhoud gaat opnieuw naar een model | **als aanvulling**: niveau "diep", gericht op fragmenten die A aanwijst |
| C | Go levert ruwe tellingen, de gids beschrijft de interpretatie | minder Go; niveau en voorstellen niet toetsbaar, elke agent adviseert anders | afgewezen |

De gebruiker koos: drie toestemmingsniveaus, niet, A, en A plus B ("diep").

## Toestemming en privacy

De transcripten zijn opnames van gesprekken die al naar Anthropic gingen, met de tool-uitvoer erbij.
Leest de agent in Claude Code onder hetzelfde account fragmenten opnieuw, dan lekt er niets nieuws.
Het verschil tussen "lokaal" en "diep" is daar vooral tokens en diepgang. Het wordt wél nieuw in
drie gevallen:

- een andere harness (Codex, OpenCode): de fragmenten gaan naar een andere aanbieder;
- een ander account: een sessie onder zakelijke voorwaarden, opnieuw verstuurd onder een privé-account;
- de bewaartermijn: wat de aanbieder al verwijderde, komt er opnieuw.

Daarom:

- **AC2 en AC4 gelden voor `lighthouse` zelf**: het binaire bestand stuurt niets en print geen
  tekst uit de transcripten. Ze gelden ook voor het niveau "lokaal".
- **Wat de agent bij "diep" leest**, valt onder de toestemming van dat niveau. Het issue krijgt
  daar een regel bij (zie "Issue bijwerken").
- **De toestemmingsvraag past zich aan de harness aan.** In Claude Code: "Diep leest fragmenten
  van eerdere sessies opnieuw in; dat kost tokens." In Codex en OpenCode komt erbij: "De fragmenten
  gaan naar <aanbieder>, niet naar Anthropic."

## Ontwerp

### Plek in de agentgids

Een nieuwe sectie **History** in [`docs/site/agents/install.md`](../../../site/agents/install.md),
na keuze 1 (het binaire bestand moet er zijn) en vóór de overige keuzes. De resultaten sturen die
keuzes: het niveau, keuze 9 (labels), en het advies.

1. De agent vraagt met de vraagtool, aanbevolen eerst: **lokaal** · diep · niet. De tekst noemt
   wat elk niveau leest en, buiten Claude Code, naar welke aanbieder diep gaat.
2. **niet**: de agent draait niets en gaat door naar de keuzes.
3. **lokaal**: `lighthouse history`.
4. **diep**: `lighthouse history --sources`, daarna leest de agent per bevinding met `confidence:
   "low"` of per `unknown`-signaal hoogstens de aangewezen regels (zie "Diep").
5. De agent legt alle voorstellen in één bericht voor, elk met een korte uitleg in de taal van de
   gebruiker: eerst het niveau met de reden, dan per bevinding de betekenis en het voorstel. Een
   `knob` wordt een keuze die de gebruiker aanneemt of afwijst. Een `advice` is uitleg zonder
   handeling. De gekozen knoppen gaan mee in het `init`-commando; het niveau via `--level`.
6. Bij een fout (exit 1 of 2) meldt de agent de stderr-regel en gaat door zonder verslag.

De gids blijft de enige plek met die instructies. `docs/install.md` (voor mensen) krijgt een
korte alinea onder "Start small" die het commando noemt.

### Het commando

```text
lighthouse history [--sources] [--since <datum>] [--projects-dir <map>]
```

- Draait in de root van een git-repo. Buiten een git-repo: exit 2, `lighthouse history: <dir> is
  not a git repository`.
- Draait vóór `init`, dus zonder `lighthouse.toml`. Het leest geen repoconfig. *(Eigen keuze, na de
  builder-check.)*
  - **Slug** uit `git remote get-url origin`, met dezelfde ontleding als `initcmd` (`internal/initcmd/initcmd.go`
    rond r. 679). Zonder `origin`, of met een remote die niet te ontleden is: geen slug, alleen roots.
  - **Roots**: de repo-root, en elk pad uit `git worktree list --porcelain`. Wie ooit worktrees
    elders maakte (OneDrive), wordt zo alleen gevonden als die worktrees nog bestaan; verdwenen
    worktrees vindt de slug-regel, als er een slug is.
- Vindt de transcripten met de logica van `lighthouse telemetry collect`: `findTranscripts` en
  `belongsToRepo` (`internal/cli/telemetry_collect.go`). De gedeelde stukken gaan naar een eigen
  package (zie "Code-indeling"), zodat `history` en `telemetry collect` dezelfde code gebruiken.
- **Hoofdletters.** Vandaag vergelijkt alleen de slug-regel zonder hoofdletters (`EqualFold`); de
  roots-regel vergelijkt exact. Het gedeelde package krijgt een optie `FoldCase`. `history` zet die
  aan, zodat projectmappen voor `D:\…` en `d:\…` allebei gevonden worden. `telemetry collect` laat
  hem uit en blijft zo ongewijzigd. *(Eigen keuze, na de builder-check.)*
- **Een andere home.** Het commando leest `$HOME/.claude/projects`, of `--projects-dir` als dat is
  gegeven (en dan voor `memory` de `memory/`-mappen daaronder). Vindt het geen sessies in WSL
  (`/proc/version` noemt `microsoft`), dan zegt het verslag dat in `hint` en noemt het
  `/mnt/c/Users/<naam>/.claude/projects` als kandidaat. De agent vraagt de gebruiker vóór hij met
  `--projects-dir` opnieuw draait: dat valt onder dezelfde toestemming, maar het is een andere map.
  *(Eigen keuze, na de builder-check.)*
- Leest elk `.jsonl`-bestand regel voor regel, met een `bufio.Scanner` met een ruime buffer
  (regels van meerdere MB komen voor). Een onleesbare regel telt als `skipped_lines`, het commando
  breekt niet af.
- `--since` (ISO-datum) beperkt tot records met een latere `timestamp`; standaard alles.
- Exit 0 met verslag, ook als er geen transcripten zijn (dan `sessions: 0` en elk onderdeel
  `status: "no_data"`). Exit 1 als de projectenmap onleesbaar is. Exit 2 bij een gebruiksfout.
- Geen netwerk, geen schrijfacties.

### Het verslag

JSON op stdout, sleutels in het Engels (`code_language = "en"`). De teksten `meaning`, `proposal.text`
en `reason` zijn Engels: de agent vertaalt naar de taal van de gebruiker, die bij de installatie nog
niet vastligt (keuze 5a komt later). *(Eigen keuze.)*

```json
{
  "schema": 1,
  "sessions": 571,
  "span": {"first": "2026-10-01", "last": "2026-10-31"},
  "skipped_lines": 0,
  "level": {"recommended": "light", "reason": "no issues or PRs used; 78% of pushes went to the trunk"},
  "findings": [
    {
      "key": "trunk",
      "status": "found",
      "confidence": "high",
      "measured": {"pushes_by_branch": {"release/r5.11": 41}, "trunk_names": ["release/r1", "release/r5.11"]},
      "meaning": "Pushes go straight to the trunk; main-commit-gate blocks that from day one.",
      "proposal": {"kind": "advice", "text": "Pick one fixed trunk (main) and work on a branch per issue."},
      "sources": [{"file": "<pad>.jsonl", "lines": [118, 402]}]
    }
  ]
}
```

- `hint` (optioneel, op het hoogste niveau): een aanwijzing voor de agent, nu alleen de WSL-kandidaat
  bij nul sessies (zie "Een andere home").
- `status`: `found` (signaal gezien), `none` (gemeten, niets gevonden) of `no_data` (niet te meten).
- `confidence`: `high` of `low`. `low` zet de detector als het signaal op een heuristiek rust die
  vaak mist, bijvoorbeeld een shell die alleen uit padvormen is afgeleid.
- `measured`: alleen getallen, branchnamen, padpatronen en vaste patroonnamen. Branchnamen en
  bestandsnamen zijn metadata, geen inhoud; ze mogen erin. *(Eigen keuze: zonder branchnamen is de
  trunkverschuiving niet te tonen.)*
- `proposal.kind`: `knob` of `advice`. Een `knob` draagt ook `flag` (bijvoorbeeld `--level standard`
  of `--labels`).
- `sources`: alleen met `--sources`. Per bevinding hoogstens 5 bestanden en per bestand hoogstens
  10 regelnummers, de meest recente eerst.

### Onderdelen

Elk onderdeel is één detector: een functie die records ontvangt en aan het eind een `finding`
levert. De velden die ze lezen, staan in de transcripten van Claude Code 2.1.x (vastgesteld op
echte transcripten van deze repo):

- `type: "assistant"`, met `message.content[]` van `type: "tool_use"` (`name`, `input.command` bij
  `Bash`, `input.file_path` bij `Write`/`Edit`);
- `gitBranch`, `cwd`, `timestamp`, `entrypoint` op elk `user`- en `assistant`-record;
- `type: "pr-link"` (`prNumber`, `prRepository`);
- `type: "attachment"` met `attachment.type: "hook_blocking_error"` en `attachment.hookName`.

Een detector leest uit `input.command` alleen het commando en de vlaggen die hij zoekt, met een
vaste regex. Hij bewaart nooit de tekst zelf.

**De trunk** *(eigen keuze, na de builder-check)*. De trunk is per record vastgesteld, niet als
"branch met de meeste pushes":

- de default branch uit `git symbolic-ref refs/remotes/origin/HEAD`, als die er is;
- anders, en daarnaast, elke branch waarvan de naam past op `main`, `master`, `develop`, `trunk` of
  `release/*`.

Een **push op de trunk** is een `git push` terwijl `gitBranch` een trunk is, of een push waarvan de
refspec een trunk als doel noemt (`HEAD:main`, `origin main`). Een **verschuivende trunk** is een
reeks van verschillende `release/*`-namen die over de tijd pushes op de trunk krijgen; `trunk_names`
noemt ze in volgorde van eerste push.

**Ketencommando's tellen mee** *(eigen keuze, na de builder-check)*. Voor PR's en issues tellen
naast `gh pr`/`gh issue` ook `lighthouse forge pr`/`lighthouse forge issue` en
`glab mr`/`glab issue`. Als test vóór een commit telt ook `lighthouse gate`. De test die
`commit-test-gate` in een hook draait, is in het transcript niet als testrun te herkennen; een repo
waar lighthouse al draait, valt buiten de agentgids (die stopt bij een bestaande installatie).

| Key | Meet | Betekenis en voorstel |
|---|---|---|
| `trunk` | `git commit` en `git push` per `gitBranch`; pushes op de trunk (zie "De trunk"); de reeks van `release/*`-trunknamen | Pushes op de trunk botsen met `main-commit-gate`. Een schuivende trunk (`release/rN`) botst met één vaste default branch. Voorstel: `advice` (één vaste trunk, een branch per issue). |
| `work_tracking` | `gh issue`- en `gh pr`-aanroepen, `pr-link`-records; schrijfacties naar `TODO.md`, `docs/tasks/**`, `*.sqlite`/`*.db` en `memory/**` als takenbestand | Werk buiten issues past niet bij de keten van issue naar PR. Voorstel: bij issues en PR's een `knob` (`--labels`, keuze 9); anders `advice` (werk naar GitHub-issues). |
| `memory` | in `~/.claude/projects/<enc>/memory/`: aantal bestanden, regels van `MEMORY.md` tegen de leesgrens van 200 regels; projectmappen die alleen in hoofdletters verschillen (`D:` tegen `d:`) | Een index op de leesgrens valt deels weg. Lighthouse schrijft niets naar harness-memory. Voorstel: `advice` (memory opruimen; een regel die moet blijven gaat in de repo); later een `knob` via #497. Dit onderdeel leest alleen bestandsnamen, aantallen en regeltellingen, geen inhoud. |
| `shell` | `entrypoint`; padvorm van `cwd` (`C:\`, `/c/`, `/mnt/c/`, `/home/`); patroonnamen in `Bash`-commando's (`powershell`, `cmd /c`, `wsl`); CRLF-signalen (`\r`, `dos2unix`, `core.autocrlf`, `git config … eol`) | Bepaalt native Windows, WSL of Linux, en daarmee welk installatieblok van de gids geldt. Voorstel: `advice` (bijvoorbeeld `.gitattributes` met `* text=auto eol=lf`). `confidence: low` als alleen padvormen het bepalen. |
| `hooks` | de huidige project- en gebruikershooks (niet die van plugins) via `agentaudit.Overlap` (hergebruikt, niet gewijzigd), tegen de guards van alle packs die in het binaire bestand zitten: vóór `init` is er geen manifest *(eigen keuze, na de builder-check)*; `hook_blocking_error` per `hookName` in de historie | Overlappende eigen hooks blokkeren dubbel. Voorstel: `advice` met de namen van de overlappende hooks, en de verwijzing naar `/lighthouse-audit-agent-config` na de installatie. |
| `worktrees` | `git worktree add` per padpatroon; `cwd`-waarden die op een worktree-pad lijken; het aantal verschillende worktrees; padpatronen van synchronisatiemappen (`OneDrive`, `Dropbox`, `iCloud`) | Worktrees in een synchronisatiemap geven lock- en kopieerproblemen. Voorstel: `advice`; later een `knob` via #496. |

### Niveauregel

Alles begint op `light`, net als een eerste `init` (#19). De regel kijkt alleen naar
`trunk`, `work_tracking` en twee extra tellingen.

| Niveau | Voorwaarden (allemaal) |
|---|---|
| `standard` | minstens 3 PR's (`pr-link`, of een PR-commando uit "Ketencommando's"); minder dan 20% van de pushes op de trunk; testcommando's vóór minstens de helft van de commits in dezelfde sessie (`go test`, `pytest`, `npm test`, `uv run pytest`, `cargo test`, `lighthouse gate`) |
| `strict` | alles van `standard`, plus minstens één `codex`-aanroep (de reviews van `strict` vragen Codex) |

*(Eigen keuze: de drempels. Ze staan als constanten in één bestand, zodat een volgende meting ze
kan bijstellen.)*

`reason` noemt bij `light` welke voorwaarden van `standard` ontbraken, en bij `standard` waarom het
geen `strict` werd. Is er geen data (`sessions: 0`), dan `light` met reden `no history`.

### Diep

Bij "diep" draait de agent `lighthouse history --sources`. Daarna leest hij per bevinding met
`confidence: "low"`, of met `status: "no_data"`, hoogstens de regels uit `sources`, met
`sed -n '<n>p' <file>` of de leestool van de harness. Per bevinding hoogstens 10 regels, samen
hoogstens 50. Hij vult daarmee de uitleg aan; hij past de `measured`-waarden niet aan. In het
voorstel aan de gebruiker zegt hij welke punten hij zo heeft aangevuld.

### Code-indeling

- `internal/history/`: het package met de detectoren, de niveauregel en het verslag. Elke
  detector in een eigen bestand (`trunk.go`, `worktracking.go`, …). Een `Analyze(records iter, env)`
  die het verslag geeft, los van het bestandssysteem, zodat de tests records voeren.
- `internal/transcripts/` (nieuw): het vinden van de transcripten van een repo, verhuisd uit
  `internal/cli/telemetry_collect.go` (`findTranscripts`, `firstCwd`, `cwdOf`, `belongsToRepo`).
  `telemetry collect` en tick gebruiken dan het nieuwe package. Dat is een verhuizing zonder
  gedragswijziging; de bestaande tests van `telemetry_collect_test.go` en `tick_collect_test.go`
  blijven groen.
- `internal/cli/history.go`: het cobra-commando, met een `historyEnv` (home, cwd, stdout) zoals
  `collectEnv`, geregistreerd in `internal/cli/root.go`.

### Tests

- Per detector een tabeltest met synthetische records en de verwachte `finding`.
- Een synthetische set transcripten onder `internal/history/testdata/` met een bekende uitkomst
  per onderdeel en voor het niveau. Minstens drie sets: "tweede gebruiker" (verwacht `light`, trunk
  schuift, werk in `TODO.md`, Windows met Git Bash, OneDrive), "keten" (verwacht `standard`) en
  "keten met Codex" (verwacht `strict`). Eén test draait het hele commando op elke set en vergelijkt
  met een golden JSON.
- **Privacytest**: de fixtures bevatten herkenbare tekens (`SECRET-PROMPT-…`, `SECRET-OUTPUT-…`) in
  prompts, tool-uitvoer en bestandsinhoud. De test eist dat geen ervan in de uitvoer staat, ook niet
  met `--sources`.
- Een test zonder transcripten (`no_data`, exit 0) en een met een onleesbare regel
  (`skipped_lines: 1`).
- Een test dat het commando niets schrijft: de tijdelijke home en repo zijn na afloop gelijk aan
  ervoor.

### Documentatie

- [`docs/site/agents/install.md`](../../../site/agents/install.md): de sectie History (zie boven).
- [`docs/install.md`](../../../install.md): een alinea onder "Start small".
- Een fragment in `changelog.d/`.

### Issue bijwerken

Na goedkeuring van deze spec krijgt #491 bij de acceptatiecriteria één regel: "AC2 en AC4 gelden
voor `lighthouse` en het niveau 'lokaal'. Bij 'diep' leest de agent met uitdrukkelijke toestemming
gerichte fragmenten; de vraag noemt buiten Claude Code de aanbieder."

## Wat aan de bouwer is

- De exacte regexen per detector, binnen de vaste lijst patroonnamen hierboven.
- De vorm van `measured` per onderdeel, zolang het alleen getallen, branchnamen en padpatronen zijn.
- Hoe de verhuizing naar `internal/transcripts/` in commits valt (eerst verhuizen, dan bouwen).
