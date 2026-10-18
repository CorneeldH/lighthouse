---
name: paia-write-issue
description: Writing and maintaining GitHub issues for CorneeldH/personal_ai_agent. Use when creating new issues, editing issue titles/bodies, or cleaning up issue metadata in this repo. Distinct from write-issue (cedanl). LET OP — een PR-body volgt .github/pull_request_template.md, niet deze skill.
metadata:
  domain: factory
---

# Writing and maintaining GitHub issues

Standards for issues in the `CorneeldH/personal_ai_agent` repository. Pull requests follow
[`.github/pull_request_template.md`](../../../.github/pull_request_template.md).

Ontwerp erachter: [`docs/developer/planning/specs/2026-08-02-issue-taxonomie-design.md`](../../../docs/developer/planning/specs/2026-08-02-issue-taxonomie-design.md)
(area, milestone, board) en [`2026-08-03-blokkade-as-en-skill-sync-design.md`](../../../docs/developer/planning/specs/2026-08-03-blokkade-as-en-skill-sync-design.md)
(blokkade-as).

## Gereedschap: gebruik `gh api`, niet `gh issue list --json`

`gh issue list --json` loopt stuk onder de rtk-proxy:

```
Failed to parse gh issue list output / expected value at line 1 column 1
```

Gebruik voor alles wat je moet uitlezen
`R=$(uv run python -m src.controller.identity repo) && gh api "repos/$R/issues?..."`.
`gh issue create` en `gh issue edit` werken wél gewoon.

Tweede val: **een onbekend `--label`-filter wordt stil genegeerd** in plaats van te falen. Je krijgt
dan het volledige, ongefilterde resultaat terug en telt dus onzin. Verifieer een labelnaam met
`gh label list` voordat je erop filtert.

## Workflow

Bij `/paia-write-issue [beschrijving]`:

### 0. Bestaat dit al?

Vraag dit vóór je een template kiest: een dubbel issue kost later een sluitactie. Het is een
gerichte zoekopdracht op dít onderwerp, geen bulkscan. Kies 1-2 kerntermen uit de beschrijving
(functie, bestand, configsleutel, onderwerp) en draai dit met
`dangerouslyDisableSandbox: true` (`gh` heeft netwerk nodig).

Zoek in issues, open én gesloten, begrensd tot vijf:

```bash
R=$(uv run python -m src.controller.identity repo) || exit 2
T='<kernterm>'
gh api -X GET search/issues -f q="repo:$R is:issue $T" \
  --jq '.items[:5][] | "#\(.number) \(.state) \(.title)"'
```

Kijk daarna in codebase en docs, of het werk daar al staat:

```bash
git fetch origin main || echo 'FETCH FAALDE: origin/main kan verouderd zijn'
git grep -l '<kernterm>' origin/main -- src scripts deploy .claude docs
```

`git grep` geeft exit 1 bij nul treffers; pas exit ≥ 2 (bijv. 128) is een fout. Precisie gaat voor
recall: alleen een treffer die hetzelfde werk aantoonbaar dekt telt. "Lijkt erop" is geen treffer.

- Geen dekkende treffer: meld precies één regel `dubbel? nee — gezocht op <termen>`, ook zonder
  enige treffer, nooit stilte. Door naar stap 1, zonder vraag. Staat `FETCH FAALDE` in de
  output, dan is de codebase-blik op een mogelijk verouderde `origin/main` gedaan: meld dan
  `dubbel? niet meetbaar — fetch faalde` in plaats van `nee`, en ga door naar stap 1.
- Wél een dekkende treffer: meld hem en laat de gebruiker kiezen (`AskUserQuestion` in Claude Code,
  anders een vraag in chat) uit **niet aanmaken** · **aanvullen** op het bestaande issue (comment
  of bodywijziging via `gh api`) · **toch aanmaken**, met de reden in de body
  (`Verschilt van #<nr>: …`).

Deze stap rapporteert alleen: ze sluit niets en zet geen label.

### 1. Kies het template

Twee templates, gekozen op inhoud. Er is **geen** GitHub issue-type: `CorneeldH` is een persoonlijk
account, issue-types zijn een org-feature (`.type` is `null` op elk issue, `/issues/types` geeft 404).
Probeer geen `issueTypeId` te zetten.

| Input | Template |
|---|---|
| Kapot gedrag, reproduceerbaar | Bug |
| Al het andere | Task |

Een **pitch** is geen issue-vorm. Grotere brokken werk worden een milestone; de pitch staat in de
milestone-beschrijving (zie "Milestones").

### 2. Formatteer de body

Gebruik het template uit "Issue Templates". Neem de input van de user als basis en vul aan met context
uit de codebase waar nodig.

### 3. Zet de verplichte assen

Elk issue draagt **drie assen**, in deze volgorde te bepalen. Het cijferprefix is de leesvolgorde in
de labellijst.

| As | Cardinaliteit | Vraag |
|---|---|---|
| `1-loc:*` | precies 1 | Waar pak je het op? |
| `2-area:*` | precies 1 | Waar hoort het bij? |
| `3-*` | 0 of 1 | Waarom kan dit nu niet doorlopen? |

Daarnaast optioneel domeinlabels (`bug`, `security`, `hardware`, `architecture`, `notes`, `ceda`,
`wbso`, `money`, …). Die as is vrij.

#### 3a. `1-loc:` — waar pak je het op

| Label | Kleur | Wanneer |
|---|---|---|
| `1-loc:mac` | `0a3069` (donker) | iOS/Mac device-laag |
| `1-loc:beelink` | `2563eb` (midden) | Beelink-server: config, services, hardware |
| `1-loc:container` | `93c5fd` (licht) | app/dashboard, research, overig — **default** |

Bij impact op meerdere plekken: **kies de laagste, meest fundamentele.** Raakt het alles of de
Mac-laag → `1-loc:mac`. Raakt het Beelink én de app → `1-loc:beelink`. Alleen container → `1-loc:container`.

De kleuren vormen een blauw-ramp: donkerder = lager en fundamenteler.

#### 3b. `2-area:` — waar hoort het bij

| Area | Vuistregel |
|---|---|
| `2-area:intake` | welke inhoud komt binnen en wordt een Document — óók verbatim doorgegeven aan een tweede bak |
| `2-area:nexus` | verwerken, linken, terugvinden |
| `2-area:flow` | eruit én **veredeld**: pulse, update, publish — samengesteld, gecontroleerd, omgevormd |
| `2-area:infra` | waar draait het en blijft het draaien — hardware + config |
| `2-area:models` | inferentie en agent-uitvoering |
| `2-area:clients` | de apps en interfaces waarmee de gebruiker het bedient |
| `2-area:product` | productisering, licenties, geld |
| `2-area:meta` | de repo zelf: docs, CI, planning, en lint en typecheck hier op nul houden |
| `2-area:lighthouse` | het werkmodel van de agents: controller, review-keten, hooks, keten-skills, usage-venster |

Precies één. Passen er twee, kies het zwaartepunt — met twee labels telt het issue in twee as-views
en is de as niet meer te auditen.

Grens lighthouse/meta: **heeft een onbemande run het nodig om te starten of vertrouwd te
worden?** Zo ja, dan `lighthouse`: het vak *Kern* en het gereedschap van *Taalpakket*
(ruff- en ty-gates, kwaliteitsratel, shellcheck-gate). Zo nee, dan `meta` (docs,
CI-workflows, de schuld hier wegwerken, planning) of het vak *Project* (board-model en
vault-skills, in hun inhoudelijke area). Host-units blijven `infra`. Tot de verhuizing loopt
lighthouse-werk in deze repo; het label bestaat vanaf het vervolgissue van #1361 (#1378).
Bestaat het label nog niet (`gh label list --search 2-area:lighthouse`), gebruik dan
`2-area:meta`.

Grens intake/infra: **inhoud van de stroom** versus **config van de machine**. Een dienst draaiend en
gehard krijgen is `infra`; "bron X landt als Document" is `intake`.

Grens intake/flow: **kopieert het, of maakt het iets nieuws?** `flow` is niet "naar buiten" maar
*veredeld* naar buiten — inhoud die uit meerdere bronnen komt, gecombineerd, geauditeerd of
gevalideerd is, en een vorm krijgt die het in de vault nooit had. Verbatim distributie van bestaand
materiaal naar een ander publiek is géén flow: dat is dezelfde inhoud die een tweede intake-bassin
in stroomt, en dus `intake`.

| Kopieert → `intake` | Maakt iets nieuws → `flow` |
|---|---|
| #548 CEDA-repo vullen met verbatim transcript + verslag + prompt | #323 vault → publieke site + syndicatie |
| Transcripten automatisch naar deelnemers sturen (ruw materiaal) | #123 draft JSON → CEDA-mailbox draft (opgesteld bericht, geen doorgegeven bestand) |
| Een bestaand Document in een tweede bak zetten | #203 proactieve agent via Teams/telefoon (bepaalt zélf wát het meldt) |

Het conceptuele ongemak hoort erbij: "intake" suggereert *naar mij toe*, terwijl #548 bij CEDA naar
binnen gaat. De as klopt, de naam dekt hem net niet. Dat is bewust, geen slordigheid — label #548
niet terug naar `flow`.

#### 3c. `3-` — de blokkade

Een `3-`-label markeert **wat de eerstvolgende stap blokkeert**, niet wat ergens in het issue
voorkomt. Het is een toestand, geen eigenschap: het komt erop zodra het werk de mens-stap bereikt en
gaat er weer af.

De test: **kan ik dit gewoon tegen de agent zeggen?** Zo ja → geen label, ongeacht hoeveel werk het is.
Geen label betekent oppakbaar; dat is de ready-queue.

Hoogstens één. Gelden er twee, dan wint de **laagste rang** — je kunt niet aansluiten wat nog niet
geleverd is, en niet beoordelen wat nog niet gebouwd is.

| Rang | Label | Kleur | Bindt wanneer |
|---|---|---|---|
| 1 | `3-needs-shaping` | `FBCA04` | Wat/waarom nog niet scherp. Alles erna is prematuur |
| 2 | `3-needs-decision` | `FBCA04` | Eén keuze van Corneel blokkeert, de rest is helder |
| 3 | `3-blocked-issue` | `D93F0B` | Wacht op een ander issue in deze repo |
| 4 | `3-blocked-external` | `D93F0B` | Wacht op derde partij, API, vendor, levering |
| 5 | `3-needs-hands` | `FBCA04` | Hij moet instellen, invoeren, aansluiten, aanschaffen |
| 6 | `3-needs-testing` | `FBCA04` | Bouw staat; hij moet oordelen of het doet wat bedoeld is |

Amber = de bal ligt bij Corneel. Rood = de bal ligt buiten.

Grens hands/testing: hands is **instellen, invoeren, aansluiten**; testing is **verifiëren dat iets
werkt zoals bedoeld**. De technische test heeft de agent al gedaan; wat overblijft is menselijk
oordeel. `3-needs-testing` kan nooit vóór een implementatie staan en is dus ook het "bijna
klaar"-signaal.

Niet te verwarren met board-Status `On hold`: dat is *bewust geparkeerd*, een `3-`-label is *er zit
iets in de weg*. Een issue kan tegelijk geparkeerd én ongeshaped zijn.

`3-blocked-issue` gaat nooit mee in `gh issue create`: het leunt op de `blocked_by`-relatie, en die
komt eerst. Zie 3e.

#### 3d. Milestone — past het thematisch in een lopend blok?

Haal de open milestones op, met beschrijving:

```bash
R=$(uv run python -m src.controller.identity repo) || exit 2
gh api "repos/$R/milestones?state=open&per_page=100" \
  --jq '.[] | "\(.title)\t\(.open_issues) open\n\(.description)\n---"'
```

Begin bij de milestones met dezelfde area-prefix als het `2-area:`-label, maar kijk ook naar de
rest. De vraag is **thematisch**: helpt dit issue het probleem uit `### Problem / Opportunity` op
te lossen, en brengt het de `### Definition of Done` dichterbij? Een issue dat alleen in hetzelfde domein valt,
hoort er nog niet bij.

Wees kritisch. Geen milestone is de default, geen noodgreep:

- Raakt het issue de DoD niet, dan hoort het er niet bij, ook al valt het in dezelfde area.
- Valt het onder de `### No-Gos`, dan hoort het er niet bij.
- Rekt het de appetite op, dan hoort het er niet bij. Een blok groeit niet stil.
- Passen er twee milestones, of is het een randgeval, leg het dan met één zin motivatie aan
  Corneel voor. Kies niet zelf.

Past het niet: laat `--milestone` in stap 4 weg. Het issue gaat naar de backlog. Noem in stap 5
altijd de uitkomst: welke milestone en waarom, of "backlog" met de dichtstbijzijnde milestone en
waarom die niet paste.

#### 3e. Blokkade leggen

Wacht het issue op een ander issue in deze repo, dan leunt `3-blocked-issue` op de GitHub-relatie
`blocked_by`. De relatie komt **vóór** het label, zodat een halve mislukking nooit een label zonder
relatie achterlaat. Dit draait ná stap 4; voor een bestaand issue dat een blokkade krijgt vervalt
stap 1 hieronder.

De invariant, per open issue:

| Label | open `blocked_by` | Oordeel |
|---|---|---|
| `3-blocked-issue` | ≥ 1 | **klopt** |
| `3-blocked-issue` | 0, wel gesloten relaties | **vervallen**: label eraf |
| `3-blocked-issue` | geen relatie | **ongekoppeld**: relatie leggen, of label eraf |
| `3-needs-shaping` of `3-needs-decision` | ≥ 1 | **klopt**: het lagere label wint, de relatie wacht |
| geen `3-`-label, of een label met hogere rang dan `3-blocked-issue` | ≥ 1 | **label ontbreekt**: `3-blocked-issue` erop (het hogere label eraf) |
| geen of ander label | 0 | **klopt**, buiten deze invariant |

`3-blocked-external` valt erbuiten: die wacht op iets buiten de repo. Herstel: label eraf is
`gh issue edit <nr> --remove-label 3-blocked-issue`, relatie leggen is stap 2 hieronder.

Een `blocked_by`-relatie tussen twee Controller-issues is ook hun stapelvolgorde (stacking-light,
#1099): de controller laat het geblokkeerde issue spec en plan doen en stapelt de uitvoering op de
groene PR van de voorganger. De invariant en de stappen hieronder blijven gelijk.

1. **Aanmaken zonder `3-blocked-issue`** (stap 4). Noteer vooraf de starttijd met
   `date -u +%Y-%m-%dT%H:%M:%SZ` en neem de letterlijke ISO-waarde op in je antwoord: een
   shellvariabele (`START=...`) overleeft geen nieuwe Bash-call, dus de zoekcall bij een mislukte
   create (faalpuntentabel hieronder) gebruikt die letterlijke tekst, nooit `$START`. Het nummer
   `<nr>` komt uit de URL.
2. **Relatie leggen.** `issue_id` is het numerieke id van de blocker, niet zijn issuenummer:

   ```bash
   R=$(uv run python -m src.controller.identity repo) || exit 2
   ID=$(gh api "repos/$R/issues/<blocker-nr>" --jq .id)
   gh api -X POST "repos/$R/issues/<nr>/dependencies/blocked_by" -F issue_id="$ID"
   ```

3. **Verifiëren met de lijst, niet uit de summary**: `issue_dependencies_summary` loopt één write
   achter en meldt een geslaagde POST dan als mislukt. De blocker moet hier staan:

   ```bash
   R=$(uv run python -m src.controller.identity repo) || exit 2
   gh api "repos/$R/issues/<nr>/dependencies/blocked_by" --jq '.[].number'
   ```

4. **Pas dan het label.** Staat er al `3-needs-shaping` of `3-needs-decision`, dan wint dat label
   en komt `3-blocked-issue` er niet op. Een `3-`-label met hogere rang gaat er in dezelfde edit af
   (`--remove-label <label>`):

   ```bash
   gh issue edit <nr> --add-label 3-blocked-issue
   ```

5. **Eindcontrole**: de lijst uit stap 3 én
   `R=$(uv run python -m src.controller.identity repo) && gh api "repos/$R/issues/<nr>" --jq '[.labels[].name]'`. Pas als
   relatie en winnend `3-`-label er samen staan, meld je succes.

Bij elke fout leid je de toestand nooit af uit welke stap faalde: je leest hem uit en meldt wat er
wérkelijk staat, met het commando dat de invariant herstelt. Nooit een relatie weghalen ter
compensatie; een relatie zonder label vangt de sweep als **label ontbreekt**.

| Faalpunt | Uitlezen | Melding |
|---|---|---|
| `gh issue create` faalt of geeft geen URL | `R=$(uv run python -m src.controller.identity repo) && T='<titel>' && gh api "search/issues?q=repo:$R+in:title+\"$T\""` | precies één treffer met `created_at` ná de genoteerde starttijd (de letterlijke ISO-waarde uit stap 1, niet `$START`) én dezelfde body: ga door met dat nummer. Geen treffer: fout, stop. Meer dan één, een oudere, een andere body, of de zoekcall faalt: **onbekend** + de zoekcall, stop vóór elke mutatie. Nooit blind opnieuw aanmaken |
| POST faalt | de lijst uit stap 3 | relatie staat: door naar het label. Staat niet: één retry, dan "#N staat zonder relatie en zonder label" + het POST-commando |
| verificatie mist de blocker | idem | zoals hierboven; de summary telt niet |
| label zetten faalt | het labelcommando uit stap 5 | label staat: door. Staat niet: "relatie staat, label ontbreekt" + `gh issue edit <nr> --add-label 3-blocked-issue` |
| eindcontrole wijkt af, of een GET faalt | — | de werkelijke toestand, of **onbekend** met het uitleescommando. Nooit succes |

### 4. Maak de issue aan

`--label` bevat altijd minimaal een `1-loc:` en een `2-area:`.

```bash
gh issue create \
  --title "<titel>" \
  --label "1-loc:container,2-area:nexus" \
  --milestone "<milestone>" \
  --body "$(cat <<'EOF'
<geformatteerde body>
EOF
)"
```

Het board vult zichzelf: `Auto-add to project` pakt elk open issue op en zet Status op `To do`. Je
hoeft niets aan het board toe te voegen.

Faalt dit commando of geeft het geen URL: ga naar 3e, faalpuntentabel, rij `gh issue create faalt
of geeft geen URL`.

### 5. Rapporteer het resultaat

Toon de issue-URL die `gh issue create` teruggeeft, plus de milestone-uitkomst uit stap 3d.
Wacht het issue op een ander issue: voer eerst 3e stap 2–5 uit en rapporteer de eindcontrole (of
**onbekend**), niet alleen de URL.

### 6. Controleer de invarianten

Na bulkwerk aan labels:

```bash
R=$(uv run python -m src.controller.identity repo) || exit 2
gh api "repos/$R/issues?state=open&per_page=100" --paginate \
  --jq '.[] | select(.pull_request|not) |
    {n:.number,
     loc:[.labels[].name|select(startswith("1-loc"))]|length,
     area:[.labels[].name|select(startswith("2-area"))]|length,
     blk:[.labels[].name|select(startswith("3-"))]|length} |
    select(.loc != 1 or .area != 1 or .blk > 1)'
```

Lege uitvoer = goed.

## Milestones

**De milestone-beschrijving ís de pitch.** Er is geen apart parent-issue en geen `Milestone: `-prefix
in issue-titels; die constructie is afgeschaft.

- **Naamgeving**: `<area>: <beschrijvende naam>`, bijvoorbeeld `infra: Beelink-monitoring afronden`.
  Prefix wel (de milestone-dropdown is één platte lijst; alfabetisch sorteren geeft de as-groepering
  gratis), cijfer niet (een area eindigt nooit, dus `infra-04` zegt niets over voortgang).
- **Bij sluiten**: titel wordt `<area>: <naam> - DONE` en `state=closed` in dezelfde PATCH. Het
  board onderscheidt open en gesloten milestones niet; het achtervoegsel doet dat wel. Zie de
  `paia-close-issue`-skill, sectie "Milestone sluiten".
- **Beschrijving**: de pitch uit "Pitch (Shape Up)" hieronder, met zijn zeven kopjes in die volgorde.
  De ID van een milestone is zijn nummer in de URL.
  De pitch noemt onder *Problem / Opportunity* de crux van de hoofd-area, zie
  [`docs/developer/tenets.md`](../../../docs/developer/tenets.md) (*Koppeling*).
- **Harde regel**: *een milestone zonder DoD-tekst in de beschrijving bestaat niet.* Zonder die regel
  groeien blokken weer uit tot eeuwige bakken.
- De beschrijving is tegelijk de **poortwachter**. Hoort een nieuwe task op grond van DoD en no-gos
  niet in het blok, dan gaat die naar de backlog (`To do`, geen milestone) — niet alsnog erin.
- De bestaande `2.x`-nummerreeks blijft historie en wordt niet hernoemd.

Aanmaken gaat via de REST-API; `gh` heeft geen `milestone create`:

```bash
R=$(uv run python -m src.controller.identity repo) || exit 2
gh api --method POST "repos/$R/milestones" \
  -f title='infra: <naam>' -f description="$(cat <<'EOF'
<pitch volgens het template "Pitch (Shape Up)">
EOF
)"
```

Sub-issues blijven bestaan, maar alleen waar één taak natuurlijk uiteenvalt in deelstappen — niet als
structuurlaag boven de milestone. Koppelen gaat via de REST-API; `sub_issue_id` is de database-`id`
van het child, niet het nummer:

```bash
R=$(uv run python -m src.controller.identity repo) || exit 2
CHILD_DBID=$(gh api "repos/$R/issues/<child-nr>" --jq '.id')
gh api --method POST "repos/$R/issues/<parent-nr>/sub_issues" -F sub_issue_id="$CHILD_DBID"
```

Verifieer met
`R=$(uv run python -m src.controller.identity repo) && gh api "repos/$R/issues/<parent-nr>/sub_issues" --jq '.[].number'`.

## Het board

Eén GitHub Projects v2-board, met Status `To do` · `On hold` · `In progress` · `Controller` ·
`Done`. Labels en milestones zijn de bron van waarheid over wát een issue is; het board toont
ze. De Status is de enige plek waar het board zelf iets betekent: `Controller` draagt een
issue over aan de controller, die de keten spec → plan → uitvoering → groene PR dan zelf
loopt (`scripts/controller_tick.sh`, overzicht via `/paia-controller`). Mergen blijft bij
jou; na de merge van een `Refs`-PR toetst de controller het issue zelf opnieuw
(nacontrole, #1247). Verder sluit je met de hand via `/paia-close-issue`.

Noem het in documentatie en skills consequent **"het board"**, nooit "project" — anders botst het met
het omgangstaalgebruik van "project".

Het board heeft ook een veld **Priority**: `P0` · `P1` · `P2`, geen waarde = achteraan. De
controller kiest op Priority en dan op het laagste issuenummer. Zetten gaat in de board-UI, of zo
(de ids staan in `lighthouse.toml`, `[forge.board]`, en komen uit `src.controller.identity`):

```bash
P=$(uv run python -m src.controller.identity project-id) || exit 2
F=$(uv run python -m src.controller.identity priority-field-id) || exit 2
S=$(uv run python -m src.controller.identity priority-option <P0|P1|P2>) || exit 2
gh project item-edit --id <item-id> --project-id "$P" --field-id "$F" --single-select-option-id "$S"
```

Een item-id lees je met
`N=$(uv run python -m src.controller.identity project-number) && O=$(uv run python -m src.controller.identity owner) && gh project item-list "$N" --owner "$O" --format json --jq '.items[] | select(.content.number==<nr>) | .id'`.

`On hold` is de plek voor bewust geparkeerd werk. Er is geen `on-hold`-label. Zetten gaat in de
board-UI, of via `gh project item-edit` (vereist `project`-scope plus project-, item-, field- en
optie-id); er is geen `gh issue`-vlag voor.

## Acceptance Criteria vs Definition of Done

| Aspect | Acceptance Criteria (AC) | Definition of Done (DoD) |
|--------|--------------------------|--------------------------|
| **Scope** | Per item uniek | Universeel voor ALLE items |
| **Vraag** | "Wanneer is DEZE taak klaar?" | "Welke kwaliteitsstandaarden gelden voor ALLES?" |
| **Voorbeelden** | User kan inloggen, Error bij fout wachtwoord | Code reviewed, Tests passing, Docs updated |

AC = WAT deze specifieke taak moet doen. DoD = HOE we kwaliteit borgen.

## Issue Templates

**BELANGRIJK**: elk template heeft vaste secties. Meng ze niet.

### Bug

```markdown
### Beschrijving

**Wat gaat er mis?**
...

**Stappen om te reproduceren:**
1. ...

**Screenshots / Logs (optioneel):**
...
```

### Task

```markdown
### Beschrijving

Wat moet er gedaan worden?

### Acceptatiecriteria

Specifieke voorwaarden waaraan DEZE taak moet voldoen:
- [ ] ...
- [ ] ...

### Definition of Done

Standaard kwaliteitseisen (vink af wat van toepassing is):
- [ ] Code reviewed door teamlid
- [ ] Tests geschreven en passing
- [ ] Documentatie bijgewerkt
- [ ] Geen bekende bugs
```

### Pitch (Shape Up) — voor de milestone-beschrijving

Dit is **geen issue-template**. Gebruik het voor de beschrijving van een nieuwe milestone.

```markdown
### Problem / Opportunity

Wat is het echte probleem? Voor wie? Evidence, voorbeelden, context.

### Appetite (timebox)

Kies uit: Small (1-2 dagen) | Medium (3-4 dagen) | Large (5-6 dagen)

### Solution

High-level aanpak, misschien enkele key elements.

### Risks / Rabbit holes

Welke valkuilen moeten we vermijden?

### Definition of Done

Wanneer is dit blok af? Verplicht — zonder DoD bestaat de milestone niet.

### No-Gos

Wat doen we expliciet NIET in deze versie?

### Nog niet vastgelegd

Open vragen die binnen het blok vallen maar nog niet scherp zijn. Eén bullet per vraag. Geen?
Schrijf "Geen.".
```

**Nog niet vastgelegd** — drie punten:
- **Wat erin hoort**, afgezet tegen DoD (wanneer af) en No-Gos (wat niet): een vraag hier hoort wél
  bij het blok, alleen het HOE ligt nog niet vast. Dupliceer daarom geen DoD-punt of no-go.
- **Uitstroom**: zodra een vraag scherp genoeg is voor acceptatiecriteria wordt hij een eigen issue
  in deze milestone via `/paia-write-issue`, en de bullet wordt `→ #<nr>`. Een bullet zonder
  `→ #<nr>` is open. Dat eigen issue mag het huidige issue zijn, als zijn spec de vraag
  beantwoordde. Bijwerken: schrijf de héle pitch, alleen die bullet gewijzigd, met je schrijftool
  (niet via `echo` of een heredoc) naar een tijdelijk bestand, en stuur dat bestand mee:
  `R=$(uv run python -m src.controller.identity repo) && gh api -X PATCH "repos/$R/milestones/<n>" -F description=@<bestand>`.
  Nooit de pitch zelf als shell-argument: backticks en `$` worden dan uitgevoerd.
- **Timebox**: de fog krijgt geen eigen timebox. Past het antwoord niet in de appetite,
  dan wordt het een no-go of een volgende milestone. Geen label en geen issue boven de milestone.

**LET OP**: gebruik geen "Acceptatiecriteria" in een pitch — dat hoort bij Tasks.

## Title Standards

- Wees specifiek en beschrijvend
- Sentence case
- Geen prefixes als `[FEATURE]` of `[BUG]` — daar zijn labels voor. Ook geen `Milestone: `-prefix.

### Good titles

- `Add real-time streaming transcription during meetings`
- `Fix MacWhisper database connection timeout`
- `Verbeter journal skill met automatische linking`

### Bad titles

- `Bug` (vaag)
- `Fix thing` (vaag)
- `NEW FEATURE` (vaag, all caps)

## Important

- Never include "Generated with Codex" in issues unless directly relevant
- Link related issues using `#issue-number` syntax
- Provide context — explain the "why" not just the "what"
- Gebruik `@username` voor validatie en sparring partners in pitches
