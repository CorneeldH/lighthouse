---
name: paia-handoff
description: Schrijf het startcommando voor de volgende sessie. Gebruik aan het einde van een fase, of wanneer de gebruiker vraagt om "een command voor een nieuwe sessie", "hoe start ik dit in een nieuw venster", "geef me het command", "hand-off", of /paia-handoff. Produceert één copy-pasteklaar blok met worktree, branch, doel, relevante achtergrond-informatie (of een verwijzing daarnaar) en de eerste stap met het slash-commando waarmee die sessie begint.
metadata:
  domain: factory
---

# Handoff Skill

Eén blok tekst dat de gebruiker plakt in een verse sessie, waarna die sessie zonder
verdere uitleg verder kan. Geen samenvatting van wat er gebeurd is — dat staat in
git en in het issue. Wél: waar het werk staat en wat de volgende stap is.

Sluit aan op de AGENTS.md-regel **Fresh Context Per Phase**: één afgeronde eenheid
werk = één venster. De hand-off is de brug tussen twee vensters.

## When to Use

- De gebruiker vraagt erom ("command voor nieuwe sessie", "hoe pak ik dit op", "hand-off").
- Een fase is af (plan geschreven, PR open, spec klaar) en het vervolg hoort in schone context.
- Je loopt tegen de contextlimiet aan en het werk is op een natuurlijke naad.

Niet gebruiken midden in een taak die in dit venster afgemaakt kan worden.

## Workflow

1. **Bepaal de plek.** In een worktree? Dan het absolute pad + de branch:
   ```bash
   git rev-parse --show-toplevel && git branch --show-current
   ```
   Op main zonder worktree: zeg dat de volgende sessie zélf een worktree opzet
   (AGENTS.md-regel: worktree vóór de eerste schrijfactie).
2. **Bepaal de staat.** Wat is gecommit, wat staat er nog open?
   ```bash
   git status --short && git log --oneline -3
   ```
   Ongecommit werk noem je expliciet — de volgende sessie mag er niet over struikelen.
3. **Bepaal het anker.** Precies één van: issuenummer, plan-/specpad, of PR-nummer.
   Bestaat er een plan in `docs/developer/planning/plans/`, gebruik dat pad; anders
   de spec in `docs/developer/planning/specs/`; anders het issue.

   **Is het anker een spec- of planpad en is het issuenummer bekend** (uit de prompt of de
   branch `issue-<nr>-…`), post het dan op het issue,
   ook in Controller-modus vóór de `🤖 Handoff`,
   met `dangerouslyDisableSandbox: true` (Go-TLS van `gh`):
   ```bash
   cd <worktree> && uv run python scripts/post_artefact.py <nr> <absoluut pad>
   ```
   Het script is idempotent: zonder inhoudswijziging post het niets. Kent GitHub de commit nog
   niet, dan pusht het de branch zelf (alleen zonder open PR, nooit `main`). Zet de
   `artefact:`-regel in het blok onder `Staat:`.
   Een fout blokkeert de handoff niet: de foutregel komt onder `Let op:`.
4. **Bepaal de eerste stap, en die draagt altijd een commando.** Eén concrete
   handeling, geen agenda van vijf punten — maar wél met het slash-commando erbij
   waarmee de verse sessie begint. Een verse sessie kent de gewoontes van dit venster
   niet: zonder commando begint hij zonder skill en doet het werk op de vrije hand.
   Route op wat er al ligt:

   | Wat er ligt | Eerste stap | Controller |
   |---|---|---|
   | een plan in `docs/developer/planning/plans/`, nog niet adversarieel gereviewd | `/paia-plan-adversarial-review` op dat plan | `keuze` |
   | een plan, review geconvergeerd, open punten aanvaard, of overgeslagen op besluit van de gebruiker | `/superpowers:subagent-driven-development` op dat plan; bij *Ja, native* (Controller-modus stap 2, alleen een plan dat het native-criterium haalt) `/superpowers:executing-plans` met `lane: native`; bij antwoord *interactief* (Controller-modus stap 2, *Uitzondering*) `/superpowers:executing-plans` | `aanbieden` |
   | een spec, nog niet adversarieel gereviewd | `/paia-spec-adversarial-review` op die spec | `standaard` |
   | een spec, review geconvergeerd of rondegrens met open punten vastgelegd, nog geen plan | `/superpowers:writing-plans` op die spec | `nee` |
   | een issue, nog geen spec, meerdere ontwerpen mogelijk | `/superpowers:brainstorming` | `nee` |
   | een issue dat het HOE al vastlegt | `/paia-start-issue <nr>` | `nee` |
   | een bug of falende test | `/superpowers:systematic-debugging` | `nee` |
   | werk af, issue kan dicht | `/paia-close-issue <nr>` | `nee` |
   | PR open, review nodig | `/code-review` | `nee` |
   | PR rood | `/paia-fix-pr <pr>` | `aanbieden` |
   | context vol, zelfde fase | hetzelfde commando als deze fase; draagt de vorige handoff `lane: native`, dan draagt de nieuwe hem ook | als die fase, maar `standaard` wordt `nee` (bij een lopende plan-review: `aanbieden`) |

   De kolom Controller zegt wat Controller-modus stap 2 doet: `nee` is alleen het gewone
   blok, zonder vraag; `keuze` is de keuze in chattekst (stap 2, *Uitzondering*);
   `aanbieden` is de `AskUserQuestion` uit stap 2; `standaard` is posten en Status `Controller` zonder vraag (stap 2, *`standaard`*). Een controller-run kijkt niet naar de
   kolom: die post altijd. Bij `keuze` print je in stap 6 twee blokken vóór de vraag: eerst *review*, dan *interactief*.

   Past geen enkele rij, kies dan alsnog expliciet: noem het commando dat je zelf zou
   draaien. "Ga verder waar je gebleven was" is geen eerste stap.
5. **Noem de valkuilen** die deze sessie geleerd heeft en die de volgende sessie
   opnieuw zou raken: een harde poort, een dienst die plat ligt, een test die
   alleen op de Mac meet. Maximaal drie regels, alleen als ze echt bestaan.
6. **Print het blok** in het format hieronder, gevolgd door de suggestie om
   `/clear` te draaien.
7. **Lees je eigen blok terug** vóór je het afgeeft. Staat er een slash-commando op de
   `Eerste stap:`-regel? Zo niet: terug naar stap 4. Dit is de fout die deze skill het
   vaakst maakt.

## Controller-modus

Het blok gaat óók als comment `🤖 Handoff` op het issue, zodat `scripts/controller_tick.sh` de
volgende fase zelf start. Twee ingangen:

- **Controller-run** (de prompt eindigde op `(issue #<nr>, controller-run)`, of `PAIA_CONTROLLER_RUN`
  staat in de omgeving: na een hervatting draagt de prompt het achtervoegsel niet): altijd posten.
- **Interactief, na een fase**: bied het aan (zie de kolom Controller in Workflow-stap 4). De gebruiker zet het issue niet zelf op het board en plakt geen velden;
  dat doet deze skill na één vraag.

1. **Issuenummer.** Uit de prompt, anders uit de branch (`issue-<nr>-…`). Lukt geen van
   beide: geen comment, alleen het gewone blok.
2. **Aanbieden** (alleen interactief, en alleen als de kolom Controller in Workflow-stap 4
   niet `nee` zegt). Bij `nee` lees je de board-status niet; staat het issue toch op `Controller`, dan blijft het daar tot de gebruiker het verzet. Lees de board-status en
   het item-ID (`gh` met `dangerouslyDisableSandbox: true`):
   ```bash
   set -o pipefail
   O=$(uv run python -m src.controller.identity owner) || exit 2
   NAME=$(uv run python -m src.controller.identity name) || exit 2
   N=$(uv run python -m src.controller.identity project-number) || exit 2
   gh api graphql -f owner="$O" -f name="$NAME" -F nr=<nr> -f query='query($owner: String!, $name: String!, $nr: Int!) {
     repository(owner: $owner, name: $name) { issue(number: $nr) {
       projectItems(first: 20) { nodes { id project { number }
         status: fieldValueByName(name: "Status") {
           ... on ProjectV2ItemFieldSingleSelectValue { name } } } } } } }' \
     | jq -r --argjson n "$N" '.data.repository.issue.projectItems.nodes[] | select(.project.number == $n) | "\(.id) \(.status.name)"'
   ```
   Niet `gh project item-list`: dat haalt het hele board op met alle velden en kost honderden
   GraphQL-punten van het gedeelde uurbudget.

   **`standaard`: een spec, net gecommit.** Bij `standaard` stel je geen vraag: lees de
   board-status en het item-ID (query hierboven), post met
   `volgende: /paia-spec-adversarial-review <absoluut specpad>` (stap 5) en zet de Status op
   `Controller` (stap 6). Meld in één regel: de worktree is nu van de controller, een edit daar
   breekt de gepaarde ronde 1 (`blind` weigert een werkboom die niet schoon is). Dit geldt alleen
   in het venster dat de spec net committe (de brainstorm); een ander venster dat een ongereviewde
   spec overdraagt (pauze, context vol) valt onder `nee`. Gaf de gebruiker in dit gesprek vóór de
   commit een opt-out ("interactief", "zelf reviewen"), dan is het `nee`: alleen het gewone blok,
   geen post, geen Status-wijziging. Het geldt ook alleen op de Beelink
   (`uv run python -m src.controller.host check`, exit 0). Elders: het gewone blok plus één
   regel waarom: de worktree bestaat daar niet voor de controller, en `_start_handoff` zou het
   issue teruggeven. Terug na de commit: de gebruiker zet de Status van `Controller` af. Dat
   houdt alleen de volgende fase tegen; een lopende run maakt zijn fase af
   (`decide.py` stopt geen actieve unit).

   **Uitzondering: een plan, niet gereviewd.** Hier stel je geen `AskUserQuestion`. Je schrijft
   in gewone chattekst, in deze volgorde, en eindigt de beurt daarna:

   1. Het blok voor **review** (format van stap 6), met
      `Eerste stap: /paia-plan-adversarial-review <plan>`.
   2. Het blok voor **interactief**, met `Eerste stap: /superpowers:executing-plans <plan>`.
   3. Planlink, advies en vraag. De planlink is klikbaar: `[<bestandsnaam>](<absoluut pad>)`,
      geen kaal pad. Vorm:
      `Plan staat in [<bestandsnaam>](<absoluut pad>). Mijn advies: <optie>, omdat <één criterium>.`
      `Review, controller of interactief?`

   Kiest de gebruiker *review* of *interactief*, dan plakt die het blok en hoeft niet terug.
   Alleen *controller* vraagt nog een antwoord in dit venster. Dat geldt ook voor het vierde
   antwoord, *controller (native)*; de vraagzin hierboven noemt het niet, het staat in de
   adviesregel en in de bullet hieronder.

   Criteria voor het advies, één noemen: **review** bij meerdere taken met interfaces tussen
   elkaar, bij code of diensten, of bij een onomkeerbare stap. **Overslaan** bij één taak die
   alleen markdown raakt en waarvan de tests de tekst pinnen. Dan **controller** als er tijdens
   de uitvoering geen besluit van de gebruiker te verwachten valt, anders **interactief**. Bij twijfel: **review**.
   **Controller (native)** adviseer je alleen bij hoogstens vier taken met weinig interfaces
   tussen de taken, en als de uitvoering geen besluit van de gebruiker vraagt; je noemt het in
   de adviesregel (`Mijn advies: controller (native), omdat …`).

   - Antwoord *review*: het blok staat er al. Geen comment. Wil de gebruiker de plan-review zelf via de controller, dan `volgende: /paia-plan-adversarial-review <plan>` en stap 5 en 6.
   - Antwoord *controller*: `volgende: /superpowers:subagent-driven-development <plan>`, dan
     stap 5 (posten) en stap 6 (Status `Controller`). Dit geldt ook als het issue al op `Controller` staat.
   - Antwoord *interactief*: het blok staat er al; de agent voert zelf uit, in een vers
     venster. Geen comment, geen statuswijziging.
   - Antwoord *controller (native)*: `volgende: /superpowers:executing-plans <plan>` plus het
     veld `lane: native`, dan stap 5 (posten) en stap 6 (Status `Controller`). De controller
     start dit op Sonnet; alleen ná dit antwoord, nooit op eigen houtje.

   Een controller-run vraagt dit niet: daar blijft `volgende: /paia-plan-adversarial-review
   <plan>`, want overslaan vraagt een besluit van de gebruiker. Zonder issuenummer (stap 1) is er niets te posten: stel de vraag dan alleen met *review* of *interactief*, zonder *controller*.

   Alleen bij `aanbieden` (bij `standaard` hierboven nooit): staat hij al op `Controller`, meteen posten (stap 5), behalve bij de route *interactief*
   (hierboven). Anders vraag je met
   `AskUserQuestion`: "Zal ik de handoff aan #<nr> toevoegen en het issue op Controller
   zetten?", met de opties **Ja, nu** (`wanneer: nu`), **Ja, 's nachts** (`wanneer: nacht`) en
   **Nee, ik plak zelf** (alleen het gewone blok). Haalt het gereviewde plan het native-criterium
   (hoogstens vier taken, weinig interfaces tussen de taken), dan komt er een vierde optie bij:
   **Ja, native** (`volgende: /superpowers:executing-plans <plan>` plus `lane: native`,
   `wanneer: nu`, dan stap 5 en 6). De drie andere opties blijven. Bij ja: eerst posten (stap 5), dán de status
   zetten (stap 6). Andersom kan een tick ertussen een issue op `Controller` zonder handoff zien
   en zelf `/paia-start-issue` starten.
3. **`volgende:` is precies één commando plus één argument.** Iets anders geeft de
   controller terug.

   | Wat er ligt | `volgende:` |
   |---|---|
   | een plan, niet gereviewd | `/paia-plan-adversarial-review <absoluut planpad>` |
   | een plan, review geconvergeerd of overgeslagen op besluit van de gebruiker | `/superpowers:subagent-driven-development <absoluut planpad>` |
   | een spec, niet gereviewd | `/paia-spec-adversarial-review <absoluut specpad>` |
   | een spec, review geconvergeerd | `/superpowers:writing-plans <absoluut specpad>` |
   | PR open | `/paia-close-issue <nr>` — eindhandoff: de controller start dit nooit, hij wacht op groen |
   | PR rood | `/paia-fix-pr <pr>` |
   | issue zonder spec, ontwerp open | `/superpowers:brainstorming` — de controller geeft het issue terug |
   | plan dat de gebruiker zelf moet begeleiden (alleen controller-run) | `/superpowers:executing-plans <absoluut planpad>` zonder `lane:` — de controller start dit nooit: hij geeft het terug, de status blijft `Controller` (zichtbaar in `/paia-controller`) |
   | een plan, Native op besluit van de gebruiker | `/superpowers:executing-plans <absoluut planpad>`, met veld `lane: native` — de controller start dit op Sonnet |
   | context vol, zelfde fase | hetzelfde commando als deze fase, en `lane: native` als de vorige handoff dat droeg |

   Een pad is absoluut en zonder spaties. Bij `/paia-start-issue <nr>` zijn `worktree:` de
   hoofdcheckout en `branch:` `main`. Uitzondering: de geblokkeerde herstart uit
   `paia-start-issue` (controller-run, de poort-0-regel draagt `— blokkeerder #<x> open`) —
   dan zijn `worktree:` en `branch:` de eigen worktree en branch van het issue. Vanuit `main`
   zet de controller een uitvoerende handoff op wacht en stapelt hij nooit.
4. **`wanneer:`** is `nu`, tenzij de gebruiker `nacht` kiest of vraagt.
5. **Post** het blok:
   ```bash
   R=$(uv run python -m src.controller.identity repo) || exit 2
   gh issue comment <nr> --repo "$R" --body-file - <<'EOF'
   🤖 Handoff
   issue: #<nr>
   worktree: <absoluut pad>
   branch: <branch>
   anker: <anker>
   volgende: <commando> <argument>
   wanneer: nu

   ```
   <het blok uit Output-format>
   ```
   EOF
   ```
   Tussen de kop en de laatste veldregel staat geen lege regel: de parser stopt bij de eerste.
   Bij Native staat er een extra regel `lane: native` tussen `volgende:` en `wanneer:` (zonder
   lege regel); anders laat je hem weg. Het veld is alleen geldig naast
   `volgende: /superpowers:executing-plans <absoluut planpad>`; elders geeft de controller het terug.
6. **Status op `Controller`** (alleen interactief, ook bij `standaard`, na de comment). Zonder item-ID in stap 2
   eerst
   `N=$(uv run python -m src.controller.identity project-number) && O=$(uv run python -m src.controller.identity owner) && gh project item-add "$N" --owner "$O" --url <issue-url> --format json --jq .id`.
   ```bash
   P=$(uv run python -m src.controller.identity project-id) || exit 2
   F=$(uv run python -m src.controller.identity status-field-id) || exit 2
   S=$(uv run python -m src.controller.identity status-option Controller) || exit 2
   gh project item-edit --id <item-id> --project-id "$P" --field-id "$F" --single-select-option-id "$S"
   ```
   Meld daarna in één regel dat de controller het binnen tien minuten oppakt; de
   `/clear`-suggestie vervalt dan.
7. **In een controller-run stop je na de comment.** Geen `/clear`-suggestie, geen verdere
   tool-call: de tick ziet de nieuwe handoff en start de volgende fase.

## Output-format

Altijd één fenced block, zodat het in één klik te kopiëren is:

```
Werk in worktree /home/corneel/repositories/personal_ai_agent/.claude/worktrees/<naam>
(branch <branch>). Doel: <één zin>.

Anker: <issue #nr | pad naar plan/spec | PR #nr>
Context: <besluiten en bevindingen die niet in het anker staan, of een verwijzing ernaar>
Auto: <categorie> — <vraag in één zin> → <gekozen optie>   ← alleen in een controller-run
Staat: <wat er gecommit is; expliciet "niets ongecommit" of wat er open staat>
artefact: <de regel uit post_artefact.py>   ← alleen bij een spec- of plananker
Eerste stap: <slash-commando> <waarop>, <wat daarna>
Let op: <valkuil>   ← alleen als die er is
```

De regel `Eerste stap:` begint met een slash-commando. Staat er geen `/` in, dan is het
blok niet af — zie de regels hieronder.

Daaronder, buiten het blok, één regel: `Draai eerst /clear, plak dan het blok.`

## Regels

- **Absolute paden.** Een link of pad naar een bestand dat alleen op de worktree-branch
  bestaat moet het volledige `/home/corneel/.../.claude/worktrees/<naam>/...`-pad zijn —
  de verse sessie start in de hoofd-checkout en een relatief pad wijst daar naar niets.
- **Geen geschiedenis.** Niet navertellen wat er is gebeurd; de volgende sessie leest
  het issue en `git log`. Hand-off = vooruit, niet terug.
- **Context is wat het anker niet draagt.** Besluiten van de gebruiker, bevindingen en
  afwegingen uit deze sessie die de volgende sessie nodig heeft, maar die (nog) niet in het
  issue, de spec of het plan staan. Kort, of een verwijzing (issuecomment, pad). Staat alles
  al in het anker, schrijf dan `Context: staat in het anker.` Het is geen verslag: wat er
  gebeurd is hoort niet in deze regel, wat de volgende sessie moet weten wel.
- **Spoor van automatische keuzes.** Alleen in een controller-run: onder `Context:` één
  regel per keuze die de run zelf maakte (`[controller]` `auto_aanbevolen` in `lighthouse.toml`), in de vorm
  `Auto: <categorie> — <vraag in één zin> → <gekozen optie>`. Geen keuzes = geen regels.
- **Eén doel per hand-off.** Twee doelen = twee blokken = twee sessies.
- **Geen aannames over de tak.** Staat de branch achter op `origin/main`, zeg dat
  erbij met het rebase-commando; verzin niet dat het wel goed komt.
- **Ongecommit werk is een blokkade.** Meld het, of commit het eerst — een verse
  sessie die een vuile tree aantreft gaat raden.
- **Elke hand-off draagt een commando.** De `Eerste stap:`-regel bevat een slash-commando
  (meestal een `superpowers:`-skill, zie de tabel in stap 4). Een hand-off zonder
  commando levert een sessie op die het werk zonder skill begint — precies wat de
  hand-off moest voorkomen. Geen uitzonderingen: weet je het commando niet, dan is de
  eerste stap nog niet bepaald.

## Voorbeeld

Input: `/paia-handoff`

Output:

```
Werk in worktree /home/corneel/repositories/personal_ai_agent/.claude/worktrees/issue-537-miniflux-broncuratie
(branch worktree-issue-537-miniflux-broncuratie). Doel: de spec van #537 uitvoeren.

Anker: /home/corneel/repositories/personal_ai_agent/.claude/worktrees/issue-537-miniflux-broncuratie/docs/developer/planning/specs/2026-08-17-miniflux-broncuratie-design.md
Context: gebruiker koos in de review voor per-feed curatie, niet per categorie; dat staat nog niet in de spec.
Staat: spec gecommit (a1b2c3d), adversarial review geconvergeerd na 2 rondes (fixes in d4e5f6a),
verder niets ongecommit.
Eerste stap: /superpowers:writing-plans op die spec, daarna uitvoeren.
Let op: ik zit op de Beelink — Miniflux (:8092) direct benaderen, geen SSH. Token in ~/.config/personal-ai-agent/.
```

Draai eerst /clear, plak dan het blok.
