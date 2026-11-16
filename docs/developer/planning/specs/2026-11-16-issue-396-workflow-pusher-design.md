# Ontwerp: workflow-wijzigingen pusht een deterministische stap van de controller

Datum: 2026-11-16 · Issue: [#396](https://github.com/CorneeldH/lighthouse/issues/396)

## Aanleiding en doel

Een controller-run pusht met het `GH_TOKEN` uit de omgeving van tick (scopes `repo` en
`project`). Raakt de branch `.github/workflows/`, dan weigert GitHub de push (`refusing to allow
a Personal Access Token to create or update workflow … without workflow scope`). Gezien bij #285
(in een fix-pr-run, op `ci.yml`) en #325 (in een bouwrun, op `pins.yml`). De run improviseert dan
een 🤖 Vraag met het advies de scope te verruimen. Dat advies is fout en gevaarlijk: wie het
run-token `workflow`-scope geeft, geeft elke run de macht om workflows te wijzigen die bij de push
meteen met de secrets van de repo draaien.

Het doel: een workflow-wijziging komt op origin zonder dat een LLM-sessie ooit een token met
`workflow`-scope in handen krijgt. Een deterministisch proces van de controller, de **pusher**,
doet die ene push, na roborev, harde checks, een policy op de workflow-diff en, voor workflows die
vóór de merge draaien, een security-review door onafhankelijke reviewers. Lukt dat niet, dan komt
er een 🤖 Vraag aan de gebruiker, zonder advies over token-scopes.

Succes:

- Een controller-run die een workflow wijzigt, eindigt niet meer op een geïmproviseerde Vraag,
  maar gaat via de pusher verder met de keten (`/lighthouse-write-pr`, merge, close).
- Geen sessie-omgeving bevat ooit het workflow-token.
- Wat de pusher weigert, ziet de gebruiker als één Vraag met de diff en het push-commando.

## Doelen, niet-doelen, anti-doelen

**Doelen**

- De run pusht geen commit die `.github/workflows/` raakt; hij draagt over met een
  `🤖 Handoff` met `next: push-workflows`.
- `lighthouse push-workflows`: een subcommando zonder LLM, dat tick als eigen proces start.
- Een deterministische vloer (harde checks, policy, `actionlint`, `zizmor`), dan voor
  pre-merge-triggers een review door twee of drie smalle reviewers, unaniem.
- Shadow mode: de review draait, maar beslist pas als de gebruiker hem op scherp zet.
- `docs/tick.md` en `docs/site/data/loop.json` beschrijven de stap.

**Niet-doelen**

- Een harde grens tegen een run die het token bewust zoekt (mounts van de devcontainer, een eigen
  Unix-gebruiker). Dat is een eigen issue, zie het issue.
- De oorzaak van "geen roborev of codex op PATH" in een run (zie *Vervolg*). Tot die opgelost is,
  eindigt deze route bij elke run zonder roborev veilig in de Vraag.
- Wijzigingen onder `.github/` buiten `workflows/` (composite actions, `dependabot.yml`): daar
  vraagt GitHub geen `workflow`-scope voor.
- Pushen voor een run op een andere node (`node_id` ≠ self): de worktree staat niet op deze host.
  Zo'n handoff gaat naar de Vraag.

**Anti-doelen** (mislukt, ook als het werkt)

- Het workflow-token belandt in `os.Environ()` van tick, van een run, of van een reviewer.
- Een LLM beslist over de push zonder deterministische vloer eronder.
- Instructies voor de reviewers die uit de branch komen die gepusht wordt.
- Een Vraag die adviseert een token-scope te verruimen.

## Randvoorwaarden

- Tick is een oneshot elke 5 minuten (`docs/tick.md`, *De tick onder systemd of cron*). Een stap
  die minuten duurt, mag tick niet blokkeren.
- Tick en runs delen nu `GH_TOKEN` uit `~/.config/lighthouse/forge.env`: runs erven de omgeving van
  tick. Het workflow-token mag daarom niet in die omgeving staan, ook niet in die van tick.
- Het dreigingsmodel van het issue: ongelukken en manipulatie (een run die door de inhoud die hij
  leest wordt gestuurd), niet een run die het token bewust zoekt.
- Regel 7 (merge) is het patroon: een pure beslissing in `decide`, een uitvoering in `apply`
  (`internal/dispatch/decide.go` `mergeReady`, `internal/dispatch/apply.go` `merge`).
- Lessen uit Anthropic's eigen review-opzet (bronnen onderaan): smalle reviewers met een eigen
  context; instructies nooit uit de wijziging zelf; agentic review naast deterministische SAST;
  findings met bewijs; prompt injection inperken door insluiting, niet door detectie; nieuwe
  reviewers eerst in shadow mode.

## Aanpakken

**Overdracht.** Gekozen: **expliciete handoff**. De run post `🤖 Handoff` met
`next: push-workflows`. Alternatief was dat tick een Klaar-run zonder PR maar met ongepushte
workflow-commits zelf herkent; dat lijkt op regel 14 (geen handoff of vraag), is lastiger te testen
en laat tick in de worktree raden. De expliciete handoff is zichtbaar op het issue en past in
`protocol.ParseHandoff`.

**Wie roborev draait.** Gekozen: **de run, in V2b**, vóór de handoff. De pusher controleert
deterministisch dat de review er is en over precies de HEAD gaat die hij pusht. Alternatief was
roborev in de pusher op de host: onafhankelijker, maar findings moeten dan alsnog terug naar een
run, en het codex-budget loopt via tick.

**Welke workflows de pusher pusht.** Gekozen: **breed, met review**. Workflows die alleen ná de
merge draaien, pusht de pusher na de vloer. Workflows met een trigger vóór de merge pusht hij pas
na een unanieme security-review. Alternatief was streng (pre-merge-triggers altijd naar de Vraag);
dat is minder werk, maar laat elke `ci.yml`-wijziging bij de gebruiker.

**Waar de pusher draait.** Gekozen: **een eigen subcommando dat tick als proces start**
(`lighthouse push-workflows`), losgekoppeld met `setsid` zoals een run. Alleen dit kortlevende
proces leest het token; het is los van tick te testen en de gebruiker kan het met de hand draaien.
Alternatieven: alles inline in tick met een timeout (één review blokkeert tick tot minuten, voor
alle repo's), of inline met alleen de review als los proces (push-logica verdeeld over twee ticks,
en het token in het tick-proces dat ook runs start).

## Ontwerp

### 1. De run pusht geen workflow-commits

- **Een hook, geen regel in een skill.** `pack/core/hooks/` krijgt een PreToolUse-check
  `workflow-push-gate` op `Bash`: alleen in een controller-run blokkeert hij een `git push` waarvan
  de range (`origin/<branch>..HEAD`, of `merge-base(origin/<default>, HEAD)..HEAD` als de branch
  nog niet op origin staat) een pad onder `.github/workflows/` raakt. De melding zegt: draai V2b,
  post de handoff met `next: push-workflows`, en noemt geen token-scope. Buiten een controller-run
  doet de hook niets. Kan hij niet meten, dan `COULD NOT CHECK` en blokkeren (zoals de andere
  gates). Hoe de hook een controller-run herkent, laat ik aan de bouwer: er is al een marker
  (`permission-check` schrijft alleen "inside a controller run").
- **`post-artefact`.** `lighthouse post-artefact` pusht zelf `HEAD` (`internal/postartefact/postartefact.go:322`)
  en valt dus buiten de hook; het weigert met dezelfde melding als de range `.github/workflows/`
  raakt. *(own choice after spec review, r1-f11)*
- **Skills.** `write-handoff` en `write-pr` krijgen één regel: raakt de range
  `.github/workflows/`, dan geen `git push`, maar V2b en de handoff hieronder. Ook de fix-pr-route:
  #285 weigerde daar.
- **De handoff.**

  ```text
  🤖 Handoff
  issue: #396
  worktree: <pad>
  branch: issue-396-…
  next: push-workflows
  after: /lighthouse-write-pr 396
  when: now
  roborev: <body-line van lighthouse roborev-gate>
  ```

  `after` is het commando waarmee de keten na een geslaagde push doorgaat, alleen op de bouwroute
  (nog geen PR): `/lighthouse-write-pr <nr>`. Na een fix-pr-run bestaat de PR al; die handoff heeft
  geen `after` (zie 5). `after` moet zelf door `Judge` komen (allowlist), anders gaat de handoff
  terug (`reason_bad_handoff`). De regel `roborev:` is de roborev-uitkomst over de range als
  comment op het issue (AC 2); de pusher vertrouwt die regel niet, maar leest het record (zie 3).
- **Roborev op deze route.** Een skip telt als rood, dus de drempel op coderegels geldt hier niet:
  ook een fix van drie regels wordt gereviewd. De bouwrun reviewt in V2b zoals nu. De fix-pr-run
  reviewt na het verwerken van de CI-review lokaal de ongepushte commits
  (`origin/<branch>..HEAD`) en schrijft dat record met `--out` naar
  `<git-dir>/lighthouse/roborev/push-<head>.json`. Hoe de gate de drempel overslaat en een tweede
  record naast het branch-record schrijft, laat ik aan de bouwer.

### 2. Tick: de beslissing en de start

- `Judge` herkent `push-workflows` als eigen verdict (`VerdictPush`); het staat niet in de
  allowlist van slash-commando's.
- `decide` maakt van zo'n handoff een `KindPush`-actie, onder dezelfde voorwaarden als een
  gewone handoff-start (de rem open, geen actieve blokkade, de trigger niet gebruikt) plus:
  `[dispatch.workflow_push]` is geconfigureerd en de run die de handoff schreef, draaide op deze
  host. Anders: een 🤖 Vraag (zie 5), geen stille skip.
- `apply` claimt een rij in `runs` (command `push-workflows`, zonder sessie, de trigger-sleutel
  van de handoff, 🚀 op de handoff) en start `lighthouse push-workflows --repo <r> --issue <n>
  --worktree <pad> --run-id <id>` losgekoppeld met `setsid`, met een **opgeschoonde omgeving**:
  `PATH`, `HOME`, `XDG_RUNTIME_DIR`, het forge-token voor comments, en verder niets, behalve het
  tick-pad naar de state-map en `host.toml`, dat als vlag wordt meegegeven (`--state-dir`,
  `--host-toml`), zodat de pusher dezelfde database en config opent als tick en zijn rij kan
  sluiten. *(own choice after spec review, r1-f8)*
- Een push-rij telt niet mee voor `slots` en `max_software` en houdt geen geheugenreserve vast: er
  draait geen sessie, ook niet terwijl de rij op de fix-pr-route op CI wacht. *(own choice after
  spec review, r1-f9)* Een push-rij die met `start_failed` sluit, krijgt geen sessie-retry
  (`secondAttempt` slaat `KindPush` over): de eerstvolgende tick start de pusher opnieuw, en na
  een tweede mislukking komt de Vraag (zie 5). *(own choice after spec review, r1-f10)*
- Een push-rij heeft geen werkplek en geen transcript. De uitkomstregels die daarop meten (8–10,
  14–18) gelden er niet voor; de pusher sluit zijn eigen rij (`done` of `question`). Een push-rij
  die na `[dispatch.workflow_push] timeout` (standaard 30 min) nog open staat, gaat terug
  (`Teruggegeven`, reden *pusher reageerde niet*). De timeout geldt alleen tot de pusher zijn
  push-fase heeft afgesloten (een marker op de rij: de gepushte SHA); daarna is de rij op de
  fix-pr-route eigenaar van de PR (zie 5) en gelden regel 19 (`max_runtime`) en deze timeout niet
  meer: alleen de regels 6, 7 en 13. Op de bouwroute sluit de rij zelf bij de push. *(own choice
  after spec review, r1-f1)* Hoe dit in de regeltabel van `outcome` past
  (een vroege uitsplitsing op het commando, of een eigen tabel), laat ik aan de bouwer.

### 3. `lighthouse push-workflows`: de vloer

In deze volgorde; de eerste rode stap stopt alles en gaat naar de Vraag.

**De worktree van de run is onbetrouwbare invoer.** De run beheert de git-config van die worktree
(`core.hooksPath`, een `pre-push`-hook, `core.fsmonitor`, `url.*.insteadOf`,
`credential.helper`) en ook `~/.gitconfig`. Daarom:

- Het pad komt niet uit de handoff maar uit de werkplek van de run die de handoff schreef: de
  laatste rij op dit issue die met uitkomst `handoff` sloot en vóór de handoff-comment begon, en
  daarvan de `workspace_id` opgezocht via de Workplace-adapter (`runs` bewaart geen pad). Wijkt
  het pad in de handoff daarvan af, dan rood. *(own choice after spec review, r1-f7)*
- In de worktree van de run draait de pusher twee git-leesbewerkingen: `rev-parse HEAD` en
  `status --porcelain`, beide met `-c core.hooksPath=/dev/null -c core.fsmonitor=false`, zonder
  token in de omgeving.
- Alles daarna gebeurt in een **eigen kloon van de pusher**
  (`$XDG_STATE_HOME/lighthouse/push/<repo>.git`, bare): `git fetch <worktree> <sha>` haalt precies
  de gelezen SHA op, en alle checks, de policy en de diff voor de review lezen die SHA daar. Vóór
  de checks haalt de pusher vers `<default>` en `<branch>` op van de URL uit `forge.repo`, met het
  forge-token (alleen lezen) voor dat ene commando, op dezelfde wijze als de push zijn token krijgt
  (`GIT_CONFIG_GLOBAL=/dev/null` verwijdert de credential helper). Mislukt die fetch, dan rood.
  Twee pushers voor dezelfde repo nemen een `flock` op de kloon; wie hem niet krijgt, wacht tot de
  timeout. *(own choice after spec review, r1-f4, r1-f14)*
- Elk git-commando van de pusher draait met `GIT_CONFIG_GLOBAL=/dev/null` en
  `GIT_CONFIG_NOSYSTEM=1`, en pusht naar een expliciete URL uit `forge.repo`, nooit naar een
  remote-naam. De push is `git push <url> <sha>:refs/heads/<branch>`.

**Harde checks**

1. De worktree bestaat, staat op `branch` uit de handoff, en die branch is niet de default branch.
2. De worktree is schoon (`git status --porcelain` leeg, ook geen untracked bestanden).
3. In de eigen kloon: staat de branch op origin, dan is `origin/<branch>` een voorouder van de SHA
   (alleen fast-forward).
4. Alleen commits van deze branch: elke commit in `merge-base(origin/<default>, SHA)..SHA` is
   een gewone commit, of een merge waarvan de tweede ouder in `origin/<default>` zit (de
   update-merge van fix-pr).
5. Roborev: een record met `uitkomst: done` dekt de SHA. Dat is het branch-record
   (`<git-dir>/lighthouse/roborev/<stem>.json`) of het push-record
   (`push-<sha>.json`, de fix-pr-route). Dekken betekent: het veld `sha` van het record (de
   gereviewde SHA, niet de kop van `range`: een record zonder `--range` krijgt `merge-base..HEAD`
   van het moment van schrijven) is de SHA, of elke commit na die `sha` staat als `commit` van een `fixed`-beslissing in het record (de
   fix-commits van V2b stap 3). Een `skip`, geen record of een ongedekte commit is rood (AC 2:
   "zonder roborev-review pusht de pusher niet"). Het record is een JSON-bestand dat de run zelf
   schrijft in zijn git-dir: dat is een derde, bewust genoemde leesbewerking op de worktree
   (naast `rev-parse` en `status`), als bestandslezing zonder git. Check 5 is daarom
   geen vloer tegen een gemanipuleerde run (die kan een record vervalsen), alleen tegen
   ongelukken: een vergeten of verouderde review. Dat hoort bij de bekende grens in 5; AC 2 geldt
   dus voor ongelukken, niet voor een run die bewust liegt. *(own choice after spec review, r1-f5)*
6. De rem staat niet op pauze.

**Policy op de workflow-diff** (`git diff merge-base..HEAD -- .github/workflows/`, alleen
toegevoegde en gewijzigde bestanden; een verwijderd bestand is geen risico)

- Elke `uses:` van een externe action is gepind op een SHA van 40 hex (`./…` lokaal en
  `docker://…@sha256:…` mogen).
- Er is een expliciet `permissions`-blok op workflow-niveau, niet `write-all`.
- Geen trigger `pull_request_target` of `workflow_run`.
- `actionlint` en `zizmor` draaien op de gewijzigde bestanden, zowel op de SHA als op de
  merge-base; rood is een finding die op de merge-base niet bestond (zo blokkeert een bestaande
  bevinding, zoals `artipacked` in `loop-roadmap.yml`, niet elke wijziging van dat bestand).
  `zizmor` draait offline. *(own choice after spec review, r1-f3)* Ontbreekt een van beide op
  `PATH`, dan rood (`COULD NOT CHECK`), nooit groen.

**Indeling op trigger**

- *Post-merge*: elke gewijzigde workflow heeft alleen `schedule`, of `push` met `branches`
  precies de default branch (en geen `tags`). Dan pusht de pusher na de vloer, zonder review.
- *Pre-merge*: al het andere (`pull_request`, `push` zonder filter of op andere branches,
  `issue_comment`, …). Dan eerst de review (4). Ook `workflow_dispatch` en `workflow_call` zijn
  pre-merge: het run-token (`repo`) kan een workflow op de gepushte branch dispatchen, en dan
  draait de gewijzigde versie met de secrets vóór de merge (`macos.yml` wordt zo gebruikt). Een
  reusable workflow (`workflow_call`) draait in de context van zijn aanroeper.

### 4. De security-review (alleen pre-merge)

- **Reviewers.** Twee of drie, elk met één smal onderwerp:
  1. *supply chain*: actions van derden, pins, downloads en `curl | sh` in `run:`;
  2. *secrets en rechten*: `permissions`, gebruik van `secrets.*`, `GITHUB_TOKEN`, de triggers en
     wie ze kan afvuren;
  3. *injectie*: `${{ github.event.* }}` en andere aanvaller-gestuurde expressies in `run:`,
     `env:` en `with:`.
- **Insluiting.** Elke reviewer is een eigen proces zonder tools en zonder bestandstoegang buiten
  een lege tijdelijke map (geen leesrechten op `$HOME`, dus ook niet op het tokenbestand), met
  een omgeving zonder forge- en workflow-token. De pusher dwingt dat zelf af en vertrouwt de
  vlaggen van het commando niet: elke reviewer draait onder `bwrap` (of gelijkwaardig) met een
  lege tijdelijke map als enige schrijfbare bind, de binary en zijn bibliotheken alleen-lezen, en
  geen bind van `$HOME`, behalve wat het reviewer-blok in `host.toml` expliciet toestaat: `binds`
  (alleen-lezen paden, bijvoorbeeld het ene authenticatiebestand van de CLI en zijn installatiemap,
  nooit `~/.config/lighthouse`) en `env` (namen van variabelen die doorgaan, bijvoorbeeld een
  API-sleutel; nooit het forge- of workflow-token). *(own choice after spec review, r3-f1)* Ontbreekt `bwrap` op `PATH`, dan rood (`COULD NOT CHECK`), nooit een
  reviewer zonder sandbox. Een test laat een fake reviewer het tokenbestand proberen te lezen. De
  pusher scrubt de waarden van het forge- en het
  workflow-token uit alle reviewer-tekst voor die in een comment komt. *(own choice after spec
  review, r1-f2)* De pusher geeft de prompt en de diff op stdin; de
  diff staat afgebakend als data. De prompts zijn ingebakken in de lighthouse-binary (`embed`),
  dus nooit uit de branch.
- **Uitvoer.** Eén JSON-object per reviewer:
  `{"verdict": "approve"|"reject", "findings": [{"file", "line", "issue", "evidence"}]}`. De pusher
  neemt het oordeel uit het laatste bericht of het uitvoerbestand van de reviewer, niet uit een
  eventstroom (`codex exec --json` volstaat dus niet zonder `--output-last-message`). Een
  `reject` zonder finding met bewijs, ongeldige JSON, een timeout of een exit ≠ 0 telt als
  `reject`.
- **Besluit.** Alleen als elke geconfigureerde reviewer `approve` geeft, gaat de push door. Een
  YAML-truc moet dus alle reviewers tegelijk omdraaien, bovenop een vloer die hij niet kan sturen.
- **Modelkeuze.** De reviewer-commando's staan in `host.toml`; waar het kan, een andere
  modelfamilie dan de bouwende run, en de reviewers onderling verschillend.
- **Shadow mode.** `review = "shadow"` (standaard): de review draait en zijn oordeel staat in de
  comment, maar ook bij unaniem `approve` komt de Vraag; de gebruiker beslist met het antwoord
  `push` (zie 5). Pas met `review = "enforce"` beslist de review. Zo kan de gebruiker de oordelen
  eerst naast de eigen leggen.

### 5. Uitkomst: comment en vervolg

- **Geslaagd.** De pusher pusht vanuit zijn eigen kloon met het workflow-token, alleen aan dat
  ene `git push` meegegeven (bijvoorbeeld via `GIT_ASKPASS` of `-c http.extraHeader` in de
  omgeving van dat commando). Wat daarna gebeurt, hangt af van de route:
  - *Bouwroute* (nog geen PR): een `🤖 Handoff` met `next: <after>`, plus de velden
    `range: <oud>..<nieuw>` en `workflows: <bestanden>`. De rij sluit als `handoff`. Eén comment
    dekt zo AC 4 (range en bestanden) en het vervolg via de bestaande handoff-regel.
  - *Fix-pr-route* (de PR bestaat): een 🤖-comment met `range` en `workflows`, en de push-rij
    **blijft open als eigenaar van de PR**. Voor die rij gelden dan de regels 6, 7 en 13, alsof
    het een Klaar-run met een eigen PR is: gemerged wordt `done`, groen wordt gemerged, rood
    wordt een fix-pr-start. Zo kost een workflow-fix geen extra fix-pr-run (plafond 3), en wacht
    niets op CI in een sessie. Hoe de push-rij "Klaar" en "eigen PR" meet (de head-branch is
    `branch`), laat ik aan de bouwer.
- **Rood.** De pusher post een `🤖 Vraag` met: welke stap rood was en waarom, de workflow-diff in
  een codeblok met een hek langer dan elke reeks backticks in de diff, afgekapt onder de limiet van
  een GitHub-comment (65536 tekens) met de rest als bijlage in de worktree, het oordeel van de reviewers als die draaiden, en het commando
  `cd <worktree> && git push -u origin HEAD`. Nooit een advies over token-scopes. De rij sluit als
  `question`.
- **Een antwoord is toestemming.** Antwoordt de gebruiker `push`, dan start tick de pusher
  opnieuw met die toestemming voor precies de SHA uit de Vraag. De pusher draait de vloer
  opnieuw; is die groen en is de SHA niet veranderd, dan pusht hij zonder review en gaat verder
  zoals bij *Geslaagd*. Toestemming heft alleen de review op (een `reject` of shadow mode), nooit
  een rode vloer: dan zegt de Vraag dat de gebruiker met de hand pusht of het werk laat
  repareren, en een antwoord `push` geeft opnieuw de Vraag met de rode stap. Elk ander antwoord
  hervat niets: er is geen sessie. Hoe dat in de antwoord-regel van `decide` past, laat ik aan de
  bouwer.
- **Bekende grens.** Een run post comments met hetzelfde account als de gebruiker, dus een
  gemanipuleerde run kan `push` antwoorden. De rest van de vloer blijft dan staan (git-checks, policy,
  `actionlint`, `zizmor`), alleen de review valt weg; check 5 (roborev) valt voor een
  gemanipuleerde run ook weg, zie 3.
  Dit hoort bij de harde grens (een eigen issue): een antwoordkanaal dat een run niet kan
  gebruiken.
- **Token.** `[dispatch.workflow_push] token_file` in `host.toml`, naar het patroon van
  `[dispatch.forge] token_file` (`internal/hostconfig/hostconfig.go`). Het bestand moet mode
  `0600` hebben, anders rood. De pusher leest het pas na een groene vloer en review.

### 6. Configuratie

```toml
[dispatch.workflow_push]
token_file = "~/.config/lighthouse/workflow-push.token"
review     = "shadow"   # shadow | enforce
timeout    = "30m"

[[dispatch.workflow_push.reviewers]]
topic   = "supply-chain"            # supply-chain | secrets | injection
command = ["codex", "exec", "--sandbox", "read-only", "--output-last-message", "{out}", "-"]
binds   = ["~/.codex/auth.json"]

[[dispatch.workflow_push.reviewers]]
topic   = "injection"
command = ["claude", "-p", "--tools", ""]
```

Zonder `[dispatch.workflow_push]` is de route uit: een handoff `next: push-workflows` geeft de
Vraag. De exacte vlaggen die een reviewer tool-loos maken, laat ik aan de bouwer.

### 7. Eerst te verifiëren: de merge van regel 7

Tick merget via de API met het run-token. Of GitHub die merge weigert voor een PR die
`.github/workflows/` wijzigt, is niet geverifieerd. De eerste taak van het plan test het op een
wegwerp-PR. Weigert GitHub hem, dan merget een apart proces zo'n PR met het workflow-token:
`lighthouse push-workflows --merge --pr <n> --sha <head>`, door tick gestart zoals de push, en
alleen als de gepushte SHA uit de push-comment een voorouder is van de PR-head en `gepusht..head`
geen pad onder `.github/workflows/` raakt (op de bouwroute volgt na de push nog een
changelog-fragment van `write-pr`, dus de head is nooit precies de gepushte). *(own choice after
spec review, r1-f6)* Tick leest het
token nooit, ook niet in het geheugen. Weigert GitHub de merge niet, dan blijft regel 7
ongewijzigd.

### 8. Docs

- `docs/tick.md`: de nieuwe stap bij *Wat tick doet* en *Afloop* (de push-rij, zijn uitkomsten,
  de reden *pusher reageerde niet*), en `[dispatch.workflow_push]` bij *`[dispatch]` in
  `host.toml`*.
- `docs/site/data/loop.json`: een node `push-workflows` met de handoffs vanuit de bouw- en
  fix-pr-route en naar `write-pr` en `fix-pr`.
- `docs/install.md`: het tokenbestand (fine-grained, alleen *Contents* en *Workflows* op de repo's
  van de controller).
- Een fragment in `changelog.d/`.

## Tests

De pusher weigert, elk in een eigen test (AC 6): de default branch; geen fast-forward; een
ontbrekende of verouderde roborev-review; een policy-overtreding (ongepinde action,
`pull_request_target`, geen `permissions`). Daarnaast: een pre-merge-workflow met één `reject`
gaat naar de Vraag; shadow mode geeft de Vraag ook bij unaniem `approve`; het token staat in geen
enkele omgeving van een reviewer (een fake reviewer dumpt zijn `os.Environ`); een `pre-push`-hook,
een `core.fsmonitor` en een `url.*.insteadOf` in de worktree van de run of in `~/.gitconfig`
zien het token niet en sturen de push niet om; een workflow met alleen `workflow_dispatch` gaat
langs de review; een fix-commit na de gereviewde head telt alleen als hij in het record staat;
`decide` geeft
`KindPush` voor de handoff en de Vraag zonder config. Git-checks draaien op een echte tijdelijke
repo met een bare remote; reviewers zijn fake commando's op `PATH`, zoals
`internal/cli/roborev_fixture_test.go`.

## Wat aan de bouwer is

- Hoe de hook een controller-run herkent.
- Hoe push-rijen door de regeltabel van `outcome` gaan, en hoe een antwoord op de pusher-Vraag
  `after` start.
- De parsing van workflow-YAML (een YAML-library, geen regex) en de exacte vlaggen van de
  reviewer-commando's.
- De tekst van de drie reviewer-prompts, binnen de onderwerpen van 4.
- Nieuwe headings in `.lighthouse/headings.toml` voor de reden en de comment-teksten.

## Vervolg (eigen issues)

- Waarom een run `roborev`/`codex` niet op `PATH` vindt, terwijl tick `~/.local/bin` in zijn
  `PATH` heeft (`docs/install.md`): waarschijnlijk de devcontainer van profiel `software`.
- De harde grens: mounts van de devcontainer of een eigen Unix-gebruiker voor runs.

## Bronnen

- [Code Review — Claude Code docs](https://code.claude.com/docs/en/code-review.md)
- [anthropics/claude-code-security-review](https://github.com/anthropics/claude-code-security-review)
- [How Anthropic secures its AI-native software development lifecycle](https://claude.com/blog/how-anthropic-secures-its-ai-native-software-development-lifecycle)

## Open na review

Ronde 3 haalde de rondegrens met deze punten open. Ze zijn aanvaard als `accepted_open`;
de volgende fase of de PR-review weegt ze opnieuw.

- **r3-f1** (MAJOR, 4 Security-review (Insluiting) / 6 Configuratie): The bwrap sandbox that the r2-f1 fix adds binds only an empty writable temp dir and the reviewer binary plus its libraries (read-only), with no $HOME. The spec does not say how a reviewer gets its model credentials or config. The shipped example reviewers (codex exec, claude -p) authenticate from files under $HOME, and claude also needs a node runtime and its install tree, which go beyond 'binary en bibliotheken'.
