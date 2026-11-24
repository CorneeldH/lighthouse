# De workflow-pusher pusht vanzelf: intent-toets, al gepusht, en een Vraag die zegt wat ontbreekt

Issue: #551. Voorgangers: de workflow-pusher (#396), de Herstel (#505). Verwant: #552 (het
antwoord `gepusht`).

## Aanleiding

Op #495 / PR #547 pushte de workflow-pusher geen enkele keer vanzelf. Het issue noemt drie
oorzaken: A (shadow-review zonder reviewers), B (een roborev-record `skip`) en C (de branch staat
al op origin).

Op deze branch staan al twee commits:

- `fix(pusher): een SHA die al op origin staat is groen, zonder push`. Bevat `origin/<branch>`
  de SHA (ook onder een met de hand gepushte merge), dan is de vloer groen met
  `Checked.AlreadyPushed`. Er volgt geen roborev-eis, geen review en geen `git push`. De pusher
  sluit zoals na een eigen push, en de rij noteert de SHA.
- `fix(pusher): roborev-rood geeft een Herstel in plaats van een Teruggegeven`. Een rode stap
  `roborev` (behalve `COULD NOT CHECK`) is te repareren, met de telling en het plafond van #505.
  De hervattingsprompt zegt de run V2b met `roborev-gate gate --push` te draaien.

Deze spec dekt de rest: C aan de kant van tick, A, en het deel van B in de skills.

## Doel

Een workflow-wijziging die past bij wat issue, spec en plan vragen, gaat zonder mens naar origin.
Bij twijfel blijft het een Vraag. Een Vraag die toch komt, zegt waarom en wat de gebruiker kan
instellen. Een handmatige push laat de keten vanzelf verder lopen.

## Niet-doelen

- Reviewers instellen of `enforce` aanzetten in `host.toml` van de gebruiker: dat doet de
  gebruiker.
- Het antwoord `gepusht` op een Vraag (#552).
- De security-review (`supply-chain`, `secrets`, `injection`) zelf veranderen.
- Een Teruggegeven van de pusher een `sha` geven (zie §2, *Teruggegeven*).

## Anti-doelen

- Een wijziging die nieuwe rechten geeft (een action, een permission, een event) gaat nooit
  zonder security-review naar origin, ook niet als de intent-toets goedkeurt.
- Een start door tick zonder antwoord geeft nooit toestemming (`permit`). Toestemming komt alleen
  van het antwoord `push` van een mens.
- Geen lus: tick start de pusher niet elke tick opnieuw voor dezelfde Vraag.
- Vloer, policy, `actionlint` en `zizmor` blijven altijd draaien. De intent-toets vervangt alleen
  de security-review, en alleen binnen de klasse van §1.

## Aanpakken

Voor de vraag "past de wijziging bij issue, spec en plan" zijn drie aanpakken overwogen:

1. **Een intent-reviewer in de pusher (LLM).** Eén reviewer met onderwerp `intent` krijgt de diff,
   de issue-body en de spec en het plan van de branch. Hij antwoordt `approve` of `reject`.
   *Gekozen door de gebruiker.* Dit meet echt of de wijziging bij de bedoeling past. "Bij twijfel
   een Vraag" wordt letterlijk `reject`.
2. **De run verklaart, de pusher controleert mechanisch.** De run zet per workflow een regel in de
   handoff, en de pusher controleert dat het bestand genoemd wordt. Afgewezen: "genoemd" is niet
   "in lijn", en de run beoordeelt zijn eigen werk.
3. **Puur mechanisch.** Het bestand moet genoemd staan in issue, spec of plan. Afgewezen: te grof.

Voor de klasse waarin de toets mag beslissen, koos de gebruiker **breed**: geen nieuwe `uses:`,
geen ruimere permissions en geen nieuw event. Twee andere opties vielen af. **Smal** (alleen
filters onder `on:`) was te nauw. **Geen klasse** liet alles zoals het is.

**Grens.** Spec en plan op de branch zijn door dezelfde run geschreven. De issue-body is het enige
anker van buiten de run. Dit is hygiëne tegen ongelukken, geen vertrouwensgrens. Dat stond al in
`docs/tick.md` over de roborev-records. De echte grens blijft dat GitHub een push van
`.github/workflows/` zonder `workflow`-scope weigert.

## Ontwerp

### 1. De klasse *intent* (A)

`Policy` deelt een wijziging nu in als `PostMerge` of `PreMerge`. Een pre-merge wijziging komt in
de klasse *intent* als elke toegevoegde of gewijzigde workflow aan alle eisen hieronder voldoet.
Een verwijderde workflow telt niet, net als nu. De pusher vergelijkt de geparste workflow op de
merge-base met die op de SHA, beide gelezen in de eigen kloon.

1. Het bestand bestond al op de merge-base. Een nieuwe workflow is geheel nieuw en valt er dus
   buiten.
2. **Geen nieuw event.** De eventnamen onder `on:` op de SHA zijn een deelverzameling van die op de
   merge-base. Een filter onder een bestaand event (`paths`, `branches`, `tags`, `types`) mag
   veranderen.
3. **Geen nieuwe action.** Elke `uses:`-waarde op de SHA (stap en job, met ref) stond al op de
   merge-base. Een andere pin van dezelfde action is ook nieuw.
4. **Geen ruimere permission.** Per scope, top-level en per job, is het niveau op de SHA niet
   hoger dan op de merge-base (`none` < `read` < `write`). `write-all` geldt als `write` op elke
   scope, en `read-all` als `read`. Een job zonder `permissions` erft de top-level. Wat een job
   effectief krijgt, telt.
5. *Keuze van deze spec, niet van de gebruiker:* **geen nieuw secret.** Elke
   `secrets.<naam>`, `secrets: inherit` en `github.token`/`GITHUB_TOKEN` op de SHA stond al op de
   merge-base. **Geen nieuwe runner.** De verzameling `runs-on`-waarden is een deelverzameling.
   Reden: dit zijn de twee andere manieren om met dezelfde actions meer macht te krijgen. Ze
   vallen onder "geen nieuwe rechten" zoals de gebruiker het bedoelde.

Een workflow die niet te parsen is, valt buiten de klasse. Dat is al een `RuleParse`-overtreding,
dus de policy is dan toch rood. `run:`-stappen, `env:`, `with:` en nieuwe jobs zonder nieuwe
action, permission, secret of runner vallen er wel binnen. Daar zijn de intent-toets en `zizmor`
de wacht.

### 2. De intent-toets

**Config.** Er komt één nieuwe tabel bij, `[dispatch.workflow_push.intent]`, met `command`,
`binds` en `env`, net als een reviewer maar zonder `topic`. Ontbreekt de tabel, dan is er geen
intent-toets, en een wijziging in de klasse gaat de gewone weg (§3). `hostconfig.WorkflowPush`
krijgt `Intent *Reviewer`. De validatie volgt die van een reviewer: hetzelfde placeholder-commando
en dezelfde binds-regels.

**Invoer.** De reviewer krijgt in zijn prompt, naast de workflow-diff:

- de titel en de body van het issue, gelezen met `Deps.Forge.Issue(ctx, req.Issue)`;
- elk bestand onder `paths.specs` en `paths.plans` dat in `c.Files` staat (dus op de branch
  toegevoegd of gewijzigd), gelezen in de kloon op `c.SHA`.

Elk stuk heeft een plafond (orde 30 000 tekens; de bouwer kiest het getal). Een afgekapt stuk
krijgt een regel die dat zegt. De paden komen uit de repo-config. De CLI zet ze in de `Request`,
zoals nu `Language`.

**Prompt.** Er komt een nieuw bestand `internal/pusher/prompts/intent.md`. Het onderwerp: past
elke toegevoegde of gewijzigde regel van de diff bij wat issue, spec of plan vraagt?
`approve` alleen als de wijziging aantoonbaar daaruit volgt. Bij twijfel, of als issue, spec en
plan de wijziging niet dragen, volgt `reject` met de regel die niet past. Het antwoord heeft
dezelfde JSON-vorm als de andere reviewers.

**Uitvoering.** Dezelfde `runReviewer` in `bwrap`, met dezelfde scrub van tokens, dezelfde guard
en hetzelfde budget. Een ongeldige JSON, een time-out of een exit ≠ 0 telt als `reject`.

**Uitkomst**, in `pushUnderLock`, op de plek waar nu `Review` draait (`class == PreMerge &&
Permit == ""`):

| Klasse | Intent | Wat volgt |
|---|---|---|
| intent | `approve` | geen security-review; pushen, in `shadow` en in `enforce` |
| intent | `reject` | een Vraag (`hintPermit`), met het oordeel van de intent-reviewer |
| intent | geen `[…intent]` of issue niet leesbaar | de gewone weg van §3, alsof de klasse er niet was |
| geen intent | — | de gewone weg van §3 |

De Vraag toont het oordeel onder *Oordeel van de reviewers*, met onderwerp `intent`. Een antwoord
`push` werkt zoals nu: het geeft toestemming voor die SHA.

### 3. De gewone weg en een Vraag zonder reviewers (A)

De gewone weg verandert niet: de security-reviewers, en in `shadow` altijd een Vraag. Wel krijgt
de Vraag een regel bij **zonder geconfigureerde reviewers** (`len(Reviewers) == 0`). Die zegt dat
er geen reviewers zijn, en dat de pusher daardoor nooit zelf pre-merge pusht. Ook noemt hij hoe je
ze instelt: `[[dispatch.workflow_push.reviewers]]` en `review = "enforce"` in `host.toml`, met een
verwijzing naar `docs/tick.md` *De review*. Is er geen `[…intent]`, dan noemt dezelfde regel ook
die tabel. Nieuwe heading-sleutels: `question_push_no_reviewers` en `question_push_no_intent`.

`lighthouse level` meldt hetzelfde in één regel, als `[dispatch.workflow_push]` bestaat zonder
reviewers of zonder intent. Het is een melding en geen fout. *Keuze van deze spec:* `level` leest
de standaard-`host.toml` (het pad dat `tick` ook gebruikt). Het meldt alleen als de huidige repo
in `[dispatch] repos` staat. Zonder `host.toml` volgt geen regel. `level` krijgt daarmee een
afhankelijkheid van `hostconfig`.

### 4. Tick zet de keten voort na een handmatige push (C)

**Wat tick ziet.** Een issue waarvan de laatste beurt een open pusher-Vraag is (`pusherQuestion`:
zonder sessie, met `sha`), zonder antwoord erna.

**Hoe tick weet dat origin de SHA heeft.** `decide` is puur, dus het feit komt uit `Prepare`.
Maar `Prepare` leest geen comments; dat doet pas `Complete`, na de vingerafdruk-check in
`gatherRepo` (`internal/cli/tick.go`). En de runs-rij van een Vraag bewaart de SHA niet.
*Keuze van deze spec:* `Prepare` leest de comments alleen van een item met status Controller
waarvan de laatste runs-rij een push-rij is die sloot als `question`. Dat is zeldzaam: zo'n issue
staat stil tot iemand antwoordt. Zo'n item kost per tick twee forge-calls (de comments en
`BranchContains`), en er is geen schemamigratie nodig. De andere opties vielen af. Een kolom met
de SHA in `runs` vraagt een migratie en een extra write in `ask`. Een repo met een open Vraag
altijd *due* maken, gaat tegen de vingerafdruk in. Is de laatste beurt in die comments een
pusher-Vraag zonder antwoord, dan vraagt `Prepare` aan de forge of `<branch>` de `sha` bevat. Dat
gaat met een nieuwe methode op `forge.Forge`:

```go
// BranchContains reports whether branch on the forge holds sha in its history. A branch that
// does not exist is false, nil.
BranchContains(ctx context.Context, branch, sha string) (bool, error)
```

- GitHub: `GET /repos/{o}/{r}/compare/{sha}...{branch}`. Bij `status` `identical` of `ahead`
  bevat de branch de SHA. Een 404 betekent false, nil.
- GitLab: `GET /projects/:id/repository/merge_base?refs[]={sha}&refs[]={branch}`. De branch bevat
  de SHA als de merge-base gelijk is aan `sha`. Een 404 of 400 betekent false, nil.
- In de forge-suite komt een test per forge bij (`forgesuite`).

Het antwoord komt op het `Item`, bijvoorbeeld als `PushedQuestion int64`: het id van de Vraag
waarvan origin de SHA heeft, anders 0. Het hoort in de vingerafdruk (`fpItem`). Anders slaat een
tick zonder board-wijziging de repo over, en ziet hij de handmatige push nooit. Een fout van de
forge betekent: geen start in deze tick, een log-regel, en de vingerafdruk niet bewaren (zoals een
mislukte actie). Zo probeert de volgende tick het opnieuw.

**De start.** Een nieuwe regel in `decide` geeft `KindPush` met trigger `pushed` en sleutel
`pushed:<vraag-id>`. Dat gebeurt alleen als de Vraag `PushedQuestion` is, de sleutel niet gebruikt
is, er geen open run is en `WorkflowPush` aan staat. De actie heeft **geen `PermitSHA`**. De plek
in `next` is direct vóór `answer`: een antwoord van de mens na de Vraag maakt de Vraag niet meer de
laatste beurt, dus de twee raken elkaar niet.

Naast `targetOf` in de pusher kennen ook `pushTargetOf`, `triggerComment` en `pushAgain` in
`internal/dispatch/decide.go` de sleutel `pushed:<id>`. Anders gaat een mislukte start van zo'n
rij terug als `reason_start_failed`. De paden `paths.specs` en `paths.plans` komen uit
`lighthouse.toml` (`internal/config`): `pushWorkflows` (`internal/cli/pushworkflows.go`) laadt ze
voor de `Request`.

**De pusher.** `targetOf` kent `pushed:<id>`. Het doel komt uit de Vraag met dat id, met
`worktree`, `branch`, `after` en `lane`, maar **zonder permit**. Daarna loopt de pusher als
gewoonlijk. Heeft origin de SHA, dan is de vloer groen met `AlreadyPushed` (commit `0269a3a9`), en
volgt de handoff of de push-notitie. Heeft origin de SHA toch niet (een race, of de forge loog),
dan loopt de hele weg met review, en eindigt die in een nieuwe Vraag. Die heeft een nieuw id. Tick
start daarvoor pas weer als `BranchContains` waar is. Dat is geen lus.

`apply` claimt de rij met `Trigger: "answer"` (net als `pushanswer`), zet 🚀 op de Vraag en start
de pusher. De runs-tabel kent geen trigger `pushed`. De sleutel draagt het onderscheid.

**Teruggegeven.** Het AC noemt ook een pusher-Teruggegeven. Een Teruggegeven draagt geen `sha`,
want hij komt van een rode stap vóór de vloer een SHA las. Het issue gaat dan al terug naar een
mens (`needs_hands`). Na B is de meest voorkomende Teruggegeven met een push erachter (roborev)
een Herstel geworden. *Besluit van de gebruiker (24-11):* dit deel van het AC vervalt. Tick zet de
keten alleen voort na een pusher-Vraag. Het PR-bewijs zegt dat.

### 5. Skills: geen `next: push-workflows` boven een skip-record (B)

`pack/core/skills/write-pr/SKILL.md` (V2b, workflow-route) en
`pack/core/skills/write-handoff/SKILL.md` (de uitzondering voor de workflow-route) krijgen één
regel. Is het push-record na V2b geen `done` (`gate --push` gaf `skip`, of `record --skip`), dan
volgt geen handoff met `next: push-workflows`. De run draait V2b opnieuw. Lukt dat niet (een
Codex-alarm, geen `claude` op PATH), dan volgt een handoff naar de mens met de reden, nooit naar de
pusher. Een packcontract-test in `pack/` legt beide zinnen vast. Daarna volgt
`go run ./cmd/lighthouse init --upgrade` en het geïnstalleerde exemplaar gaat mee in de commit.

### 6. Documentatie

- `docs/tick.md`, *De workflow-push*:
  - *De vloer*: al gepusht is groen, zonder roborev-eis.
  - *De review*: de klasse *intent* en het besluit van §1–2. Dit is het besluit dat het AC vraagt.
  - *De Herstel*: roborev erbij.
  - *De Vraag*: de regel zonder reviewers.
  - Een nieuw punt over de start `pushed` na een handmatige push.
- `docs/install.md` (de `host.toml`-voorbeelden): de tabel `[dispatch.workflow_push.intent]`.
- Een changelog-fragment in `changelog.d/`.

## Tests

- `internal/pusher` (policy): de klasse per eis van §1, met een literal workflow-paar per geval.
  In de klasse: alleen `paths`, een nieuwe `run:`-stap. Buiten de klasse: een nieuwe workflow,
  een nieuw event, een nieuwe `uses`, een nieuwe pin, `contents: read` → `write`, een job die
  `permissions` verliest bij een ruimere top-level, een nieuw `secrets.X`, een nieuwe `runs-on`.
- `internal/pusher` (Run): intent `approve` pusht in `shadow` zonder dat een security-reviewer
  draait (het `never`-script). Intent `reject` geeft een Vraag met het oordeel. Zonder
  `[…intent]` gaat het de gewone weg. Buiten de klasse draait de intent-reviewer niet. Spec en
  plan van de branch staan in de invoer van de reviewer (een fake die zijn stdin wegschrijft).
- `internal/pusher` (Vraag): zonder reviewers staat `question_push_no_reviewers` in de Vraag; met
  reviewers niet.
- `internal/hostconfig`: `[…intent]` wordt gelezen en gevalideerd zoals een reviewer.
- `internal/dispatch`: een Vraag met `PushedQuestion` geeft één `KindPush` onder `pushed:<id>`
  zonder `PermitSHA`. Een gebruikte sleutel, een antwoord na de Vraag of een open run geven niets.
  Een `PushedQuestion` verandert de vingerafdruk.
- `internal/pusher` (`targetOf`): `pushed:<id>` geeft het doel van de Vraag zonder permit.
- `forgesuite`: `BranchContains` per forge, met `identical`, `ahead`, `behind`/`diverged` en 404.
- `pack`: de contracttest van §5.

## Aan de bouwer

- Het plafond van de intent-invoer, en de exacte tekst van `intent.md` en van de nieuwe headings.
- Of de klassetoets in `policy.go` komt (die al parst) of in een eigen bestand.
- De naam van het `Item`-veld en van de trigger-constante.
- Hoe `Prepare` de push-rijen die als `question` sloten selecteert (de runs-historie is er al via
  `branches`/`open`, of via een eigen query).
