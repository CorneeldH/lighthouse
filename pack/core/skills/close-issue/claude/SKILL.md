---
name: paia-close-issue
description: Close GitHub issues with proper verification of Acceptance Criteria and Definition of Done. Links commits correctly with full URLs. Na de merge van een `Closes`-PR — nacontrole van het bewijs, en heropenen als het niet klopt (ingang C).
metadata:
  domain: factory
---

# Close Issue Skill

Sluit GitHub issues met verificatie van acceptatiecriteria en kwaliteitsstandaarden.

## When to Use

- Na voltooiing van een taak (**ingang A**)
- Bij "kan X niet dicht", "is X niet al gedaan?", "pak X op als het nog relevant is" —
  het werk is niet in deze sessie gedaan (**ingang B**, zie hieronder)
- `/paia-close-issue 58` - Sluit issue #58
- `/paia-close-issue 58 --commit abc123` - Sluit met specifieke commit referentie
- Na de merge van een `Closes`-PR: het issue is dan al dicht, en de nacontrole van het
  PR-bewijs is **ingang C**
- Prompt `/paia-close-issue <nr> (issue #<nr>, controller-run)`: de controller startte deze
  run na de merge van een `Refs`-PR. Dat is ingang B in controller-modus (**B7**)

## Stap 0 — vóór de merge

Eerst één query, voor stap 0 én C1: staat het issue open, verwijst er een PR naar met `Closes`,
en wat sloot het de laatste keer (`closer`, voor C1)?

```bash
O=$(uv run python -m src.controller.identity owner) || exit 2
NAME=$(uv run python -m src.controller.identity name) || exit 2
gh api graphql -f owner="$O" -f name="$NAME" -F nr=<nr> -f query='query($owner:String!,$name:String!,$nr:Int!){repository(owner:$owner,name:$name){issue(number:$nr){state closedByPullRequestsReferences(first:10,includeClosedPrs:true){nodes{number state}} timelineItems(itemTypes:[CLOSED_EVENT],last:1){nodes{... on ClosedEvent{closer{__typename ... on PullRequest{number}}}}}}}}'
```

`gh` draait buiten de sandbox (`dangerouslyDisableSandbox: true`).

| Uitkomst | Wat nu |
|---|---|
| issue `state` `OPEN` én een node met `state` `OPEN` | meld letterlijk `wacht op merge van #<pr>; draai /paia-close-issue <nr> opnieuw na de merge` en **stop**: geen verificatie, geen `gh issue close`. De merge van die PR sluit het issue zelf. |
| issue `state` `CLOSED` | **ingang C** |
| anders (geen open `Closes`-PR) | ingang A of B |

## Drie ingangen

| Ingang | Wanneer | Waar het bewijs vandaan komt |
|---|---|---|
| **A — je deed het werk zelf** | de commits staan in deze sessie | je context |
| **B — sluit-check zonder sessiecontext** | een oud issue komt langs met de vraag *is dit inmiddels al af?* | opgraven uit `git log`, `gh pr list`, de bestanden zelf |
| **C — nacontrole na de merge** | het issue is al dicht (merge van een `Closes`-PR of de gebruiker) | de Evidence-tabel van de sluitende PR, opnieuw gemeten op verse `origin/main` |

Ingang A is de `## Workflow` hieronder, ongewijzigd. Ingang B is een **extra stap vooraf**: hij
graaft het bewijs op dat ingang A uit context haalt, en gaat daarna door dezelfde stappen 4 t/m 6.
Er is geen snellere route naar `gh issue close` — B maakt de verificatie zwaarder, niet lichter.
Ingang C sluit nooit: het issue is al dicht, C bevestigt het of heropent het.
Elke ingang eindigt bij *Worktree opruimen — de laatste stap*; daar staat wanneer opruimen
wel en niet mag.

## Ingang B — sluit-check zonder sessiecontext

**Triggers.** "kan 650 niet dicht na recente pr's", "is 786 niet al gedaan?", "603 is toch
opgelost?", "pak 569 op als het nog relevant is", "kan 737 niet dicht met recente commit". Het
patroon: de vraag is niet *bouw dit*, maar *is dit inmiddels af geraakt door ander werk?*

Meldde `/paia-start-issue` dat een issue mogelijk al af is, dan begint de sluitactie hier.

### B1. Lees het issue en pak de ankers eruit

```bash
R=$(uv run python -m src.controller.identity repo) || exit 2
gh api "repos/$R/issues/<nr>" --jq '{created_at, title, body}'
```

Noteer drie dingen: de **aanmaakdatum** (de ondergrens van het zoekvenster), de
**acceptatiecriteria** (elk criterium wordt straks één tabelregel), en de **ankers** — de paden,
bestandsnamen, symbolen, commando's en labels die het issue letterlijk noemt. Op die ankers zoek
je. Het onderwerp van het issue is geen anker.

### B2. Graaf het bewijs op

Drie bewegingen, in deze volgorde. De eerste twee leveren kandidaten, de derde levert bewijs.

```bash
R=$(uv run python -m src.controller.identity repo) || exit 2
# 1. gemergde PR's sinds de aanmaakdatum van het issue
gh pr list --repo "$R" --state merged --limit 100 \
  --json number,title,mergedAt,url \
  --jq '.[] | select(.mergedAt > "<created_at>") | "\(.number)\t\(.mergedAt[:10])\t\(.title)"'

# 2. commits sinds die datum die de ankers raken
git log --since=<YYYY-MM-DD> --oneline -- <pad uit het issue>
git log --since=<YYYY-MM-DD> --oneline -S'<symbool uit het issue>'
git log --since=<YYYY-MM-DD> --oneline --grep '#<nr>'

# 3. de huidige staat van de boom op die ankers
ls <pad>; grep -rn '<symbool>' <pad>
```

Beweging 3 is de belangrijkste en wordt het vaakst overgeslagen. Een criterium als "bestand X
bestaat en bevat Y" bewijs je door X te openen, niet door een PR te vinden die erover ging. Staat
er een commando in het criterium, draai het dan — *Wat telt hier als bewijs* (`AGENTS.md`) geldt
onverkort, inclusief de derde uitkomst **niet meetbaar vanaf hier**.

Zoek je zonder ankers, dan vind je het tijdvak in plaats van het werk — zie B4.

### B3. Vul de tabel: één regel per acceptatiecriterium

Niet één oordeel over het issue. Eén regel per criterium, elk met zijn eigen bewijsregel:

| # | Acceptatiecriterium | Verdict | Bewijs |
|---|---|---|---|
| 1 | … | `aantoonbaar af` | `docs/developer/planning/history/README.md:3` — kop staat er |
| 2 | … | `deels` | PR #747 dekt het eerste deel, niet het tweede |
| 3 | … | `niet` | `grep -rn 'foo' src/` → 0 treffers |

Drie verdicts, parallel aan de drie uitkomsten in *Wat telt hier als bewijs*:

| Verdict | Betekenis |
|---|---|
| `aantoonbaar af` | er is een bewijsregel die **dit** criterium raakt |
| `deels` | een deel van het criterium is gedekt, de rest niet |
| `niet` | geen bewijs gevonden — **ook als het waarschijnlijk wel af is** |

Een bewijsregel is precies één van deze vier, nooit proza:

- volledige commit-URL (`https://github.com/CorneeldH/personal_ai_agent/commit/<hash>`)
- PR-nummer (`#747`)
- bestandspad met regelnummer (`docs/claude-workflows.md:63`)
- commando-output (het commando **én** wat het teruggaf)

### B4. Wat níét als bewijs telt

De makkelijkste fout die er is: iets uit het juiste tijdvak voelt als bewijs en is het niet.
`AGENTS.md` eist *"verifieer eerst dat de commits er daadwerkelijk bij horen; sluit nooit op
ongerelateerd werk"* — dit is die eis, uitgeschreven.

| ❌ Telt niet | Waarom |
|---|---|
| een commit in hetzelfde tijdvak | tijd is geen causaliteit |
| een PR die hetzelfde bestand raakt | een bestand heeft meer regels dan dit criterium |
| een titel die op het onderwerp lijkt | een titel is geen implementatie |
| "dit is vast meegenomen in die refactor" | een gok, geen bewijsregel |
| geen bewijs gevonden | **geen bewijs = `niet`** |

Bewijs moet het **criterium** raken, niet de **periode**.

### B5. Drie uitkomsten

Toon de tabel uit B3 **eerst aan de gebruiker**. Er gaat geen `gh issue close` overheen voordat
die tabel gezien is — ook niet als alles `aantoonbaar af` is.

| Verdict over alle AC | Uitkomst |
|---|---|
| alle criteria `aantoonbaar af` | door naar stap 4 (DoD), 5 en 6 hieronder; de bewijsregels uit B3 gaan mee in de close-comment, met volledige commit-URL's |
| gemengd | **stel voor het issue in te korten** tot de open criteria: vink de afgeronde criteria af in de body, laat het issue open. Niet sluiten. |
| overwegend `niet` | `/paia-start-issue <nr>`; het issue blijft staan zoals het is |

Inkorten doe je via `/paia-write-issue`, met een comment die de tabel draagt — zo ziet de
volgende sessie wat er al af was en waarom.

### B6. Afbakening

| Moment | Skill | Vraag |
|---|---|---|
| schrijven | `paia-write-issue` | bestaat dit al? |
| oppakken | `paia-start-issue` | is dit inmiddels af? *(melding, geen sluitactie)* |
| periodiek | `paia-backlog-stand` | welke open issues zijn stilletjes af? *(alleen top-3 en controller-klaar, geen bulkscan)* |
| **sluiten** | **deze skill, ingang B** | **is dit af — per criterium, met bewijs?** |

### B7. Controller-modus — nacontrole na een merge (#1247)

**Wanneer.** De prompt is `/paia-close-issue <nr> (issue #<nr>, controller-run)`: de controller
startte deze run nadat een `Refs`-PR voor het issue gemerged werd. Stap 0 en B1–B4 gelden
ongewijzigd.

**B5 vervalt**: er is geen gebruiker die de tabel ziet. De tabel uit B3 gaat altijd als comment
op het issue, via `--body-file` uit een `mktemp -d`-map. Kopregel: `Nacontrole na de merge van
#<pr>`, zonder 🤖, want het is geen controllerkop. Daarna de tabel, en per AC dat niet
`aantoonbaar af` is een regel *wat ontbreekt*.

| Verdict over alle AC | Uitkomst |
|---|---|
| alle criteria `aantoonbaar af` | stap 4–6 van ingang A. Voor de DoD telt de merge als review; een DoD-regel die vanaf hier niet meetbaar is, gaat zo in de close-comment en blokkeert niet. `gh issue close` kent geen bestandsflag (alleen `-c, --comment string`): post eerst de close-comment met de bewijsregels via `gh issue comment <nr> --body-file <bestand>` (uit de `mktemp -d`-map) en sluit daarna met `gh issue close <nr> --reason completed`. Faalt de comment, dan niet sluiten. De `opruimen:`-regel van *Worktree opruimen* gaat als bewijsregel mee in die close-comment; opruimen zelf doe je niet. |
| anders: `deels`, `niet`, of een AC `niet meetbaar vanaf hier` | niet sluiten, niet inkorten via `paia-write-issue`, het board niet aanraken (de controller zet `In progress`), geen vraag, geen `/paia-handoff`. Eindig na de comment. |

**Meldingen.** Elke melding die stap 0, 4 of 6 aan de gebruiker richt (bv. "DoD niet voldaan →
rapporteer", "meld #m is vrij", "wacht op merge") gaat in B7 in de nacontrole-comment: er is
niemand om tegen te praten. Een DoD-regel die aantoonbaar níét klopt (anders dan niet meetbaar)
behandel je als de "Anders"-tak: niet sluiten, reden in de comment.

- ✅ sluiten alleen met een bewijsregel per AC
- ❌ sluiten op een waarschijnlijk-af

## Ingang C — nacontrole na de merge

**Triggers.** Stap 0 gaf `CLOSED`; "is 1091 goed gesloten?", "klopt het bewijs van die PR
nog?". Het issue is dicht; de vraag is of het bewijs in de PR ook op `main` standhoudt. Ontwerp:
[spec sectie 4](../../../docs/developer/planning/specs/2026-10-03-pr-verifieert-issue-design.md).

### C1. Zoek de sluitende PR

Wat telt is wat het issue **werkelijk** sloot: de `closer` van de laatste `ClosedEvent` uit de
query van stap 0. Niet de nodes van `closedByPullRequestsReferences`: die houden elke gemergde PR
met een sluitwoord vast, ook een eerdere die al tot een heropening leidde, en ook na een
handmatige sluiting.

| `closer` | Wat nu |
|---|---|
| `__typename` `PullRequest`, `number` `<pr>` | `<pr>` is de sluitende PR: door naar C2 |
| al het andere (`null`, `Commit`, `ProjectV2`) | meld `gesloten zonder sluitende PR — geen nacontrole` en ga door naar *Milestone sluiten* |

### C2. Meet de Evidence-tabel opnieuw op verse `origin/main`

Vrije commandotekst uit de body draait **nooit**. Alleen de runner draait rijen, en alleen
rijen op de allowlist. Hij meet in een wegwerp-worktree op `origin/main`, niet in jouw checkout,
in een verse scratchmap buiten de repo. Eén shell-aanroep, want `scratch` leeft niet door naar
de volgende; elke stap vóór de runner is met `&&` geketend, dus een mislukte `gh pr view`,
`fetch` of `worktree add` draait de runner niet:

```bash
scratch=$(mktemp -d) && echo "scratch=$scratch" &&
gh pr view <pr> --json body --jq .body > "$scratch/body.md" &&
git fetch origin main &&
git worktree add --detach "$scratch/na-merge-<nr>" origin/main &&
{ python3 .claude/hooks/lib/evidence.py run --issue <nr> --body-file "$scratch/body.md" --cwd "$scratch/na-merge-<nr>" > "$scratch/run.txt" 2> "$scratch/run.err"; rc=$?; echo "rc=$rc"; git worktree remove --force "$scratch/na-merge-<nr>"; }
```

`<scratch>` is hieronder het pad dat de eerste regel print; neem het letterlijk over. Een verse
`mktemp -d` per run botst nooit met een `na-merge-<nr>` die een eerdere, gecrashte run liet staan.
Staat er geen `rc=`-regel in de uitvoer, dan is de runner niet gedraaid: dat is
`KON NIET CONTROLEREN` (zie C3), geen oordeel over het issue.

De exitcode wordt **vóór** de `git worktree remove` in `rc` gevangen, anders gaat `$?` verloren.
Stdout en stderr gaan naar aparte bestanden, zodat het begin van `run.txt` zuiver de
runneruitvoer is. Zonder `--voor-pr`: hier wordt een `na PR`-rij voor het eerst gemeten. Het
blok draait buiten de sandbox (`dangerouslyDisableSandbox: true`); anders is elke `gh`-rij
`niet meetbaar` (exit 4).

**De runneruitvoer bevat vrije tekst uit de PR-body** (de AC-cel, commandodetails met backticks of
`$(…)`). Plak nooit runner-uitvoer of body-tekst in een gequote commandoregel: in `"..."` voert de
shell die uit. Elk comment met runneruitvoer gaat via het bestand: `--body-file`, of
`"$(cat <scratch>/run.txt)"` (de uitvoer van een command substitution wordt niet opnieuw
geëvalueerd).

### C3. Handel per exitcode én uitvoer

De exitcode alleen is niet eenduidig: een argparse-gebruiksfout geeft ook `2` (lege stdout), een
ongevangen Python-exception (traceback) ook `1`. Daarom telt de combinatie van `rc` en `run.txt`.

| Exit | Uitvoer in `run.txt` | Betekenis | Wat nu |
|---|---|---|---|
| `0` | tabel | alle rijen `klopt` | `gh issue comment <nr> --body "na-merge-check: klopt"`, dan stap 6 (vrijgekomen issues) en *Milestone sluiten*, daarna *Worktree opruimen* |
| `4` | tabel | `niet meetbaar` vanaf hier | comment via `--body-file` (zie hieronder); **niet heropenen**; meld de rijen aan de gebruiker, dan stap 6 (vrijgekomen issues) en *Milestone sluiten* |
| `1` | de tabelkop `\| AC \| Uitkomst \| Detail \|` staat erin | `klopt niet` / `ongeldig` | heropenen, zie hieronder |
| `2` | begint met `Closes zonder bewijsblok` | geen bewijsblok in de PR-body | heropenen, zie hieronder |
| `3` | `KON NIET CONTROLEREN` (body of `--cwd` onbruikbaar) | niet meetbaar | zie hieronder |
| al het andere | `1` zonder tabelkop (traceback), `2` zonder `Closes zonder bewijsblok` (argparse), een onbekende exitcode | de runner is zelf stuk | zie hieronder |

De tabelkop die een echte `1` altijd print is letterlijk `| AC | Uitkomst | Detail |`.

**Exit `4` — het commentbestand.** Eerst de kopregel, dan de runnertabel, samen in één bestand;
de niet-meetbare rijen tellen bij *Milestone sluiten* niet als bewezen:

```bash
{ printf '%s\n\n' 'na-merge-check: klopt, op rijen na die vanaf hier niet meetbaar zijn'; cat <scratch>/run.txt; } > <scratch>/comment.md
gh issue comment <nr> --body-file <scratch>/comment.md
```

**Exit `1` of `2` met herkende uitvoer — heropenen, fail-closed.** Eerst het issue, dan het board:

```bash
gh issue reopen <nr> --comment "$(cat <scratch>/run.txt)"
uv run python scripts/board_start.py --heropen <nr>
```

Bij `2` begint de comment (`run.txt`) met `Closes zonder bewijsblok`; bij `ongeldig`-rijen staat
`Evidence buiten allowlist` in de tabel. Zet nooit zelf een voorvoegsel in de commandoregel: wat
erbij hoort staat al in het bestand. Eindigt `board_start.py --heropen` met exit 1 (uitvoer
`board: FOUT — <oorzaak>`), meld dan luid `issue heropend, board niet bijgewerkt` met het
handcommando, en wacht niet tot het vanzelf goed komt:

```bash
gh project item-edit --id <item> --project-id <PROJECT_ID> --field-id <STATUS_FIELD_ID> --single-select-option-id <In progress>
```

**Exit `3`, een onbekende exitcode, een niet-herkende uitvoer of geen `rc=`-regel.** Niet meetbaar is geen geslaagde
check en geen bewezen falen. Dus: geen `na-merge-check: klopt`-comment, **geen** `gh issue
reopen`, geen board-mutatie, en meld luid aan de gebruiker `KON NIET CONTROLEREN` met de
volledige inhoud van `run.txt` en `run.err`.

Ingang C sluit zelf niets: het issue is al dicht, en een heropend issue sluit weer door de merge
van de volgende `Closes`-PR (of door de gebruiker).

## Workflow — ingang A (je deed het werk zelf)

### 1. Haal issue op en analyseer

Is het issue al gesloten? Dan ingang C.

```bash
R=$(uv run python -m src.controller.identity repo) || exit 2
gh issue view <nummer> --repo "$R"
```

### 2. Identificeer issue type

| Type | Verificatie |
|------|-------------|
| **Bug** | Is het probleem opgelost? Reproduceerbaar? |
| **Task** | Check Acceptatiecriteria + Definition of Done |

Een Pitch is geen issue meer maar een milestone-beschrijving — zie "Milestone sluiten".

### 3. Verifieer Acceptatiecriteria (voor Tasks)

Ga door elk AC item:
- [ ] Is dit criterium aantoonbaar voldaan?
- [ ] Is er bewijs (code, test, output)?

**Als AC niet voldaan**: NIET sluiten, rapporteer wat mist.

### 4. Verifieer Definition of Done (voor Tasks)

Standaard DoD items:
- [ ] Code reviewed door teamlid (of self-review gedocumenteerd)
- [ ] Tests geschreven en passing (indien van toepassing)
- [ ] Documentatie bijgewerkt (README, CLAUDE.md, etc.)
- [ ] Geen bekende bugs

**Als DoD niet voldaan**: NIET sluiten, rapporteer wat mist.

### 5. Sluit met correcte commit link

**BELANGRIJK**: Gebruik VOLLEDIGE GitHub URL voor commits, niet alleen de hash!

```bash
# Repo uit lighthouse.toml, nooit uit de git-remote van de cwd
R=$(uv run python -m src.controller.identity repo) || exit 2

# Sluit met volledige commit URL
# printf zet de URL; de rest in een gequote heredoc, zodat backticks en $ in de AC-tekst niet draaien
gh issue close <nummer> --repo "$R" --comment "$(printf 'Completed in commit: https://github.com/%s/commit/%s\n\n' "$R" "<full-commit-hash>"; cat <<'EOF'
## Verificatie

### Acceptatiecriteria
- [x] Criterium 1 - Gedaan in [file](link)
- [x] Criterium 2 - Getest met ...

### Definition of Done
- [x] Code reviewed
- [x] Tests passing
- [x] Documentatie bijgewerkt

## Samenvatting
Korte beschrijving van wat gedaan is.
EOF
)"
```

### 6. Vrijgekomen issues

Vrijgeven doet de vrijgeef-lane (`.github/workflows/unblock-dependents.yml`), als paia-bot, bij
elke sluiting als `completed`: elk open issue dat op dit issue wachtte en geen andere open blocker
meer heeft, verliest `3-blocked-issue` en krijgt een comment. Het gesloten issue krijgt één comment
terug (`Vrijgeef-lane na deze sluiting:`) met wat vrijkwam en wat nog wacht. Een sessie geeft
niet zelf vrij: het paia-bot-token bestaat alleen in Actions. Jij controleert achteraf, ook in
ingang C (na `klopt` of `niet meetbaar`) en in B7.

Welke open issues wachtten op dit issue?

```bash
R=$(uv run python -m src.controller.identity repo) || exit 2
gh api "repos/$R/issues/<nr>/dependencies/blocking" \
  --jq '.[] | select(.state=="open") | .number'
```

Geen treffers: klaar. Anders telt alleen wat bij **deze** sluiting hoort: een eerdere sluiting
(heropend en weer dicht) heeft een eigen run en een eigen terugcomment. Eerst het sluitmoment,
dan de nieuwste run van de lane voor dit issue (de runnaam draagt `#<nr>`) die daarna startte, de
terugcomments na dat moment, en per treffer `<m>` de labels:

```bash
R=$(uv run python -m src.controller.identity repo) || exit 2
gh issue view <nr> --repo "$R" --json closedAt -q .closedAt
gh run list --workflow unblock-dependents.yml --limit 20 --json databaseId,conclusion,displayTitle,createdAt \
  --jq '[.[] | select((.displayTitle | endswith(" #<nr>")) and .createdAt >= "<closedAt>")] | first'
gh api "repos/$R/issues/<nr>/comments" \
  --jq '.[] | select(.created_at >= "<closedAt>" and (.body | startswith("Vrijgeef-lane na deze sluiting:"))) | .body'
gh issue view <m> --repo "$R" --json labels -q '[.labels[].name]|join(",")'
```

Een run of comment van vóór `<closedAt>` hoort bij een eerdere sluiting en telt niet.

| Wat je ziet | Wat nu |
|---|---|
| run `success` en de terugcomment staat op `<nr>` | meld per `<m>` letterlijk wat die comment zegt (vrij of wacht nog op …) |
| run `success`, geen terugcomment | geen `<m>` droeg `3-blocked-issue`: niets vrijgekomen, niets te doen |
| run `skipped` | gesloten als `not_planned` of `duplicate`: blokkades blijven staan, een mens beslist |
| `<m>` heeft `0` open blockers maar draagt nog `3-blocked-issue` | twee blockers sloten tegelijk en elke run zag de ander nog open: herstart, zie hieronder |
| run nog bezig | wacht tot hij klaar is; meld niets als vrij voordat de comment er staat |
| run `failure`, of geen run na de sluiting | meld luid `vrijgeef-lane niet gelopen voor #<nr>` met het herstartcommando hieronder; voer het **niet** zelf uit, de gebruiker beslist |

```bash
gh workflow run unblock-dependents.yml -f issue=<nr>
```

Werkt de lane niet (paia-bot zonder Issues: write, workflow uitgeschakeld), dan is de terugval met
de hand, per `<m>` met `0` open blockers, ook door de gebruiker:

```bash
gh issue edit <m> --remove-label 3-blocked-issue
```

Faalt een call, meld dan "vrijgekomen issues niet uitgelezen" met dat commando, nooit "geen".

## Worktree opruimen — de laatste stap

Na de sluiting blijven de worktree en de lokale branch van het issue anders staan (#1422). Het
verdict komt uit `scripts/worktree_audit.py`, dezelfde ladder als `/paia-worktree-audit`: geen
tweede set regels hier.

**Wanneer.**

| Ingang | Opruimen? |
|---|---|
| C, exit `0` in C3 | ja, het hoofdmoment: de PR is gemerged en C meet in een wegwerp-worktree |
| A of B, PR van de headbranch gemerged | ja |
| A of B, PR open of geen PR | nee — geen voorstel, ook niet bij een ✅ zonder PR |
| C met heropenen, `KON NIET CONTROLEREN` of `gesloten zonder sluitende PR` | nee |
| B7 (controller-run) | nooit — zie *Headless* hieronder |

**Branch.** Ingang C: de headbranch van de sluitende PR uit C1 (`<pr>`). B7: die van de
gemergde `Refs`-PR waarna de controller deze run startte. Ingang A en B:
de branch van de worktree van het issue (`issue-<nr>-<slug>`).

```bash
gh pr view <pr> --json headRefName -q .headRefName
uv run python scripts/worktree_audit.py --only <branch>; echo "exit=$?"
```

Het script draait buiten de sandbox (`dangerouslyDisableSandbox: true`): het roept `gh` aan voor
de PR-status. Draai het zonder `cd`, vanuit de map waar de sessie startte: de cwd-toets meet de
cwd van het script, en Codex en OpenCode schrijven geen sessiebestand dat `live` zet. Bij A of B
telt de PR-kolom van de rij: alleen `#<pr> merged` gaat door.

**Alleen exit `0` én een regel `opruimen: ja` is opruimbaar.** Elke andere uitkomst is `nee`:
exit ≠ 0, geen `opruimen:`-regel, een traceback, een lege uitvoer. De afwezigheid van een
signaal is geen ja.

| Uitvoer | Wat nu |
|---|---|
| `opruimen: ja` + exit `0` | één voorstel: het pad en de branch uit de regels eronder, via `AskUserQuestion` (Claude Code) of een vraag in chat. Na bevestiging die regels zelf draaien, in die volgorde, elk als `git -C <hoofdcheckout> …` |
| `opruimen: nee — <reden>` | meld de regel letterlijk; ruim niets op. De sluiting is al gebeurd en blijft staan |
| al het andere | meld `opruimen: nee — audit gaf geen uitslag` met de ruwe uitvoer; ruim niets op |

`<hoofdcheckout>` is het eerste pad uit `git worktree list --porcelain`. Weigert het harnas
`-C` (een worktree-geïsoleerde Claude Code-sessie), draai dan dezelfde regels zonder `-C`
vanuit de eigen cwd: het is dezelfde repo, en `--only` toetste al dat die cwd niet in de
worktree staat. Na het opruimen het bewijs tonen; de branch staat in geen van beide:

```bash
git -C <hoofdcheckout> worktree list
git -C <hoofdcheckout> branch --list <branch>
```

Weigert git een regel (bijvoorbeeld `contains modified or untracked files`), dan stop je daar en
meld je de foutregel. Draai dan niet de volgende regel.

**Headless (B7, controller-run).** Er is niemand om te bevestigen: nooit opruimen, geen vraag.
De `opruimen:`-regel gaat als bewijsregel in de close-comment.

- ✅ alleen de regels die `--only` levert, na bevestiging
- ❌ `--force`, `git worktree unlock` of `git push origin --delete` — ook niet als git weigert

## Commit Reference Format

| Methode | Voorbeeld | Resultaat |
|---------|-----------|-----------|
| ❌ Hash only | `26948cd` | Geen klikbare link |
| ❌ Short ref | `Completed in commit 26948cd` | Geen klikbare link |
| ✅ Full URL | `https://github.com/CorneeldH/personal_ai_agent/commit/26948cd...` | Klikbare link |

## Template: Close Comment

`<repo>` is de uitvoer van `uv run python -m src.controller.identity repo`.

```markdown
Completed in commit: https://github.com/<repo>/commit/<hash>

## Verificatie

### Acceptatiecriteria
- [x] AC item 1
- [x] AC item 2

### Definition of Done
- [x] Code reviewed
- [x] Tests passing (of N/A)
- [x] Documentatie bijgewerkt
- [x] Geen bekende bugs

## Samenvatting
Wat is er gedaan en waarom.
```

## Voorbeeld: Issue #58 correct sluiten

```bash
# Haal volledige commit hash
COMMIT=$(git rev-parse HEAD)
# Output: 26948cd1234567890abcdef...

# Sluit met volledige URL
R=$(uv run python -m src.controller.identity repo) || exit 2
gh issue close 58 --repo "$R" --comment "$(printf 'Completed in commit: https://github.com/%s/commit/%s\n\n' "$R" "$COMMIT"; cat <<'EOF'
## Verificatie

### Acceptatiecriteria
- [x] Claude Code plugins geïnstalleerd (claude-md-management, pyright-lsp)
- [x] Community skills verkend en relevante geïnstalleerd
- [x] Marketplaces toegevoegd (superpowers-dev, mhattingpete)

### Definition of Done
- [x] Documentatie bijgewerkt (CLAUDE.md, dotfiles README)
- [x] Geen bekende bugs
- [x] Settings correct geconfigureerd

## Samenvatting
Geïnstalleerde plugins en skills voor verbeterde Claude Code workflow.
EOF
)"
```

## Milestone sluiten

Sluit je het **laatste open issue** van een milestone, dan sluit je het blok in dezelfde beweging.
Een blok dat op 100% blijft staan zonder gesloten te worden is precies de eeuwige bak die de
taxonomie opruimt.

> **Volgorde:** doe stap 1 en 2 hieronder **vóórdat** je het issue in sectie 5 sluit. Na het
> sluiten toont de teller `open=0` in plaats van `open=1` en mis je het moment. Stap 3 (de
> milestone `PATCH … state=closed`) draait pas **ná** sectie 5 — het DoD-bewijs uit stap 2 hoort in
> diezelfde close-comment van sectie 5, niet in een los bericht.

### Stap 1. Kijk of de milestone leeg raakt

Heeft het issue geen milestone? Sla deze hele sectie ("Milestone sluiten") over.

```bash
R=$(uv run python -m src.controller.identity repo) || exit 2
gh issue view <nummer> --repo "$R" --json milestone -q '.milestone.number'
gh api "repos/$R/milestones/<nr>" -q '"open=\(.open_issues)"'
```

`open=1` betekent: dit issue is de laatste. Sloot je het issue al? Dan staat er `open=0` — controleer met `R=$(uv run python -m src.controller.identity repo) && gh issue view <nummer> --repo "$R" --json milestone -q '.milestone.number'` of het issue tot dit blok hoorde en ga gewoon door met stap 2.

### Stap 2. Loop de DoD van de milestone na

```bash
R=$(uv run python -m src.controller.identity repo) || exit 2
gh api "repos/$R/milestones/<nr>" -q '.description'
```

Ga elk DoD-punt langs en zoek bewijs (commit, test, output, runbook). **Niet elk punt aantoonbaar
→ milestone NIET sluiten.** Rapporteer wat mist; het issue mag wel dicht.

### Stap 3. Sluit de milestone én hernoem de titel naar ` - DONE`

Sluiten is **twee** wijzigingen in één PATCH: `state=closed` en de titel met ` - DONE` erachter.
Het board toont een gesloten milestone niet anders dan een open — zonder dat achtervoegsel blijft
het blok op de kanban meedoen alsof het nog loopt.

```bash
R=$(uv run python -m src.controller.identity repo) || exit 2
# huidige titel ophalen
gh api "repos/$R/milestones/<nr>" -q '.title'
# sluiten + hernoemen (titel exact overnemen, dan " - DONE" erachter)
gh api --method PATCH "repos/$R/milestones/<nr>" \
  -f state=closed -f title='<area>: <naam> - DONE'
```

Eindigt de titel al op ` - DONE`, laat hem dan staan (geen tweede suffix).

Controleer daarna:

```bash
R=$(uv run python -m src.controller.identity repo) || exit 2
gh api "repos/$R/milestones/<nr>" -q '"\(.state) — \(.title)"'
```

Noem in de close-comment van het laatste issue expliciet: `Milestone <titel> gesloten — DoD:` en
per punt één regel bewijs.

### Stap 4. Tag en publiceer de release

Een milestone is een release (`GOVERNANCE.md`). Er valt hier geen versienummer te
verzinnen — `--draft` leidt het af uit de sluitdatum van de milestone plus de bestaande
tags, en `--cut` leest het daarna terug uit `CHANGELOG.md`.

**Heeft de milestone nul gesloten issues?** Sla deze stap over. Een release met lege
notes is precies het artefact waar `release.py` op weigert; het script zegt dat zelf ook
en stopt met exit 0.

In de worktree, meteen na het sluiten van de milestone:

```bash
uv run python scripts/release.py --draft --milestone <nr>
```

Dat genereert de CHANGELOG-sectie uit de DoD en de gesloten issues, zet
`publiccode.yml` in dezelfde commit op die versie plus de sluitdatum van de milestone,
commit alles op een `release-<versie>`-branch die van `origin/main` vertakt, en opent
een PR. Lezen mag, hoeft niet: de chore-merge-lane merget de PR zodra `ci` en `cla` groen
zijn (afweging aanvaard 2026-10-10, #1344). De issuenummers zijn machinaal gecontroleerd, de
proza niet. Waarschuwingen over DoD-punten die niet terugkomen staan in de output van het
commando.

Je hoeft er niet voor op `main` te staan: het commando fetcht zelf en vertakt van
`origin/main`, juist omdat deze repo squash-merget en de zojuist gemergede issue-branch
dus geen voorouder van `main` is. Dat gebeurt in een eigen tijdelijke worktree, die na de PR
weer verdwijnt: je eigen checkout blijft op zijn branch staan.

Daarna doet de lane de rest. Hij draait elk uur (:41), merget de release-PR (alleen
`CHANGELOG.md` en `publiccode.yml` mogen erin zitten) en voert op de merge-commit
`release.py --cut` uit: tag, push en GitHub Release. Wie niet wil wachten, start hem met de hand:

```bash
gh workflow run chore-merge.yml
```

Bewijs dat het gelukt is:

```bash
R=$(uv run python -m src.controller.identity repo) || exit 2
gh run list --workflow chore-merge.yml --limit 1
git ls-remote --tags origin | grep <versie>
gh release view <versie> --repo "$R" --json tagName,publishedAt
```

#### Fallback met de hand

Dit geldt wanneer de lane rood is met een `--cut na merge van #…`-regel in de summary, of
wanneer de release-PR meer raakt dan `CHANGELOG.md` + `publiccode.yml` (dan slaat de lane hem
over). Een kale `--cut` werkt alleen zolang er nog geen tag is: het weigert een bestaande tag,
en zet de tag vóór de push en de publicatie. Bepaal daarom eerst de toestand:

```bash
git ls-remote --tags origin <versie>
```

```bash
R=$(uv run python -m src.controller.identity repo) || exit 2
gh release view <versie> --repo "$R"
```

- **Geen tag op de remote** (de cut weigerde, of de tag-push faalde): `--cut` met de hand, na
  de PR-merge, op een verse `main`. Staat de tag nog lokaal, verwijder die eerst:

  ```bash
  git tag -d <versie>
  ```

  ```bash
  git checkout main && git pull
  ```

  ```bash
  uv run python scripts/release.py --cut
  ```

  Dat tagt `HEAD`, pusht de tag en publiceert de GitHub Release. Is er na de release-PR nog
  een andere PR gemerged (de lane meldde dan `origin/main staat op X`), dan is `HEAD` niet
  meer de merge-commit van de release-PR, maar een latere commit. De tag komt dan op die
  latere commit. Meer schrijft het niet: `publiccode.yml` is al bijgewerkt op de release-PR.
  Het weigert vóór elke mutatie wanneer er geen `## v…`-kop staat, de sectie leeg is, de tag al
  bestaat, `main` niet gelijkloopt met `origin/main`, of `publiccode.yml` een andere versie
  noemt (dan is de release-PR niet gemerged). Sta dus echt op een verse `main`.
- **Tag wél op de remote, geen GitHub Release** (de publicatie faalde): publiceer alleen de
  Release bij de bestaande tag. `--cut` zou hier weigeren. Draai het op een verse `main`.

  ```bash
  uv run python scripts/release.py --tag <versie>
  ```

  Dit heeft twee bijwerkingen. Het schrijft `publiccode.yml` in je checkout om, met de
  datum van de tag-commit. En de Release-tekst zegt dat hij achteraf is gemaakt, uit een
  bestaande tag (`backfilled`). Controleer daarom daarna de checkout, en zet het bestand
  terug als het veranderd is:

  ```bash
  git status --short
  ```

  ```bash
  git checkout -- publiccode.yml
  ```
- **Tag en Release bestaan allebei**: niets te doen, de lane-run was alleen rood na de
  publicatie.

### Stap 5. Werk het nabij doel van de area bij

Sloot je een milestone in deze repo, dan werk je *Nabij doel* bij in
`docs/developer/areas/<area>.md`, met `<area>` het titelprefix van de milestone: de
gesloten milestone eruit, nieuwe open milestones van die area die niet op `ON-HOLD`
of `DONE` eindigen erin. Dit hoort niet in de release-PR: `scripts/release.py` vertakt van
`origin/main` en staged alleen `CHANGELOG.md` en `publiccode.yml`. Neem een eigen
worktree en branch vanaf `origin/main`:

```bash
git fetch origin main
git worktree add .claude/worktrees/areas-<area>-nabij-doel -b areas-<area>-nabij-doel origin/main
```

Commit als `docs(areas): nabij doel <area> na <milestonetitel>`, in een eigen PR. Dit
geldt ook als stap 4 is overgeslagen (nul gesloten issues). De crux werk je niet
automatisch bij.

## Wanneer NIET sluiten

- Een open `Closes`-PR verwijst naar het issue → stap 0: wacht op merge
- AC items niet afgevinkt → Vraag om clarificatie of werk af
- DoD niet voldaan → Voltooi ontbrekende stappen eerst
- Onduidelijk of het klaar is → ingang B: graaf het bewijs op, tabel per criterium
- Een criterium zonder aanwijsbare bewijsregel → verdict `niet`, dus niet sluiten
- Gemengde verdicts → voorstel het issue in te korten, niet sluiten
- Gebruiker heeft de verdict-tabel nog niet gezien → eerst tonen

## Related Skills

- `/paia-write-issue` - Maak issues met AC en DoD (deze repo)
- `/commit` - Commit changes
