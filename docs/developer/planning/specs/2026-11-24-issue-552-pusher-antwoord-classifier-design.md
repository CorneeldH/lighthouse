# Een antwoord in gewone taal op een Vraag van de workflow-pusher (#552)

## Doel en waarom

Een 🤖 Vraag van de workflow-pusher heeft geen sessie. Tick leest het antwoord zelf, en alleen
het exacte woord `push` geeft toestemming. Sinds #545 wordt elk ander antwoord een
`KindPushRecover`. Tick vergelijkt dan met `git ls-remote` of `origin/<branch>` precies op de SHA
van de Vraag staat. Is dat zo, dan post tick de Handoff `next: <after>`. Zo niet, dan geeft tick
het issue terug met *de branch staat niet op de gevraagde commit*.

Een mens antwoordt in gewone taal: `gepusht`, `ja, push maar`, `ok`, of een wedervraag. Daar gaat
het nu mis:

- Een handmatige push waarbij origin een merge bovenop de SHA is (`9187eeda`/`18e3a81` op #495),
  geeft het issue terug. Dat moet de keten voortzetten.
- `ja, push maar` is toestemming. Toch gaat het issue terug, met een reden die niet klopt.
- Op een wedervraag zegt tick niet wat wel werkt. Tick geeft het issue terug met een verkeerde reden.

Het doel: een antwoord in gewone taal doet wat de mens bedoelt. Toestemming uit vrije tekst mag
uitdrukkelijk wel; dat is de kern van het issue.

## Doelen

- Tick zet de keten voort als origin de SHA bevat, als gelijke commit of als voorouder, ongeacht
  de tekst van het antwoord.
- Staat de SHA niet op origin, dan beslist een classifier (`claude -p`) of het antwoord
  toestemming is (`permit`), een melding dat er al gepusht is (`pushed`), of iets anders
  (`other`).
- `permit` start de pusher met `--permit <sha>` voor precies de SHA uit de Vraag, net als het
  antwoord `push`.
- `pushed`, `other`, een falende classifier of een time-out geven één 🤖-reactie die zegt wat er
  wel werkt. Tick start dan niets en geeft het issue niet terug. De Vraag blijft open voor een
  volgend antwoord.

## Geen doelen

- De pusher zelf verandert niet: geen nieuwe vloer, policy, lint of review. De *al
  gepusht*-uitkomst van de pusher en het voortzetten zonder antwoord horen bij #551 (geval C).
- Op de fix-pr-route (een Vraag zonder `after`) zet tick de keten niet zelf voort als de SHA al op
  origin staat. Dat geval gaat terug met `reason_pusher_no_after`, zoals nu; het hoort bij #551
  geval C. Toestemming en de reactie werken op die route wel (zie *Stroom per antwoord*).
- Vragen mét sessie veranderen niet: hun antwoord hervat de sessie.
- De triage-judge en zijn schema veranderen niet.

## Anti-doelen

- Een model laat nooit een andere SHA pushen dan die uit de Vraag. Het model kiest alleen tussen
  acties die er al zijn.
- Tick zwijgt nooit op een antwoord, en pusht nooit door een fout of een time-out.
- Een exact antwoord `push` roept nooit een LLM aan.

## Aanpakken

**A. Eerst git, dan de classifier (gekozen).** Voor elk antwoord dat niet exact `push` is, kijkt
tick eerst met git of origin de SHA bevat. Zo ja, dan gaat de keten verder zonder LLM. Zo nee,
dan beslist de classifier. Zo zijn er minder LLM-aanroepen, en `pushed` bepaalt alleen nog de
tekst van de reactie. Een antwoord als `ok` op een SHA die al op origin staat, start geen
overbodige pusher.

**B. Eerst de classifier, dan per uitkomst.** Dit is de letterlijke Aanpak in het issue. Het
nadeel: `ok` op een SHA die al op origin staat, wordt `permit`. De pusher draait dan voor niets
en faalt op `fast-forward`, zolang #551 geval C niet af is.

**C. De pusher leest het antwoord.** Valt af. De pusher heeft geen LLM, en het issue houdt de
pusher ongewijzigd.

De gebruiker koos A.

## Ontwerp

### Stroom per antwoord

Decide blijft puur. `pushAnswer` (`internal/dispatch/decide.go`) blijft splitsen:

1. **Exact `push`** (`permitAnswer`): `KindPush` met `PermitSHA`, zoals nu. Geen git-check vooraf
   en geen LLM.
2. **Elk ander antwoord**: `KindPushRecover`, met `AnswerText`, `After` (mag leeg zijn), `Lane`
   en de velden van de Vraag (SHA, branch, worktree, rode stap). De controle op `after` verhuist
   van `pushAnswer` naar het pad *origin bevat de SHA* in apply: een Vraag zonder `after` gaat
   niet meer vooraf terug.

Apply (`pushRecover` in `internal/dispatch/apply.go`) doet eerst de git-check. Die leest alleen,
dus mag vóór de claim. Daarna claimt apply de rij met de sleutel `pushanswer:<id>`, zoals nu, en
zet de 🚀 op het antwoord. De stappen:

1. **Bevat origin de SHA?** Eerst `git ls-remote origin refs/heads/<branch>`. Is het resultaat
   gelijk aan de SHA, dan is het antwoord ja. Is het resultaat leeg (de branch staat niet op
   origin, `Env.LsRemote` geeft dan `""` zonder fout), dan is het antwoord **nee**, zonder fetch
   *(eigen keuze na spec-review, r1-f3)*. Anders haalt tick alleen die commit op:
   `git fetch --no-write-fetch-head origin <origin-sha>` in de worktree, met dezelfde omgeving als
   de `ls-remote` nu (geen token, `GIT_TERMINAL_PROMPT=0`). Zo verandert geen ref in de worktree
   van de run (`origin/<branch>`, `FETCH_HEAD`), en ziet `workflow-push-gate` op een latere push
   hetzelfde als eerst. Daarna `git merge-base --is-ancestor <sha> <origin-sha>`.
   - **Ja, met `after`**: post de Handoff `next: <after>` (`RenderPushed`, zoals nu) en sluit de
     rij als `handoff`.
   - **Ja, zonder `after`** (fix-pr-route): het issue gaat terug met `reason_pusher_no_after`,
     zoals nu.
   - **Git faalt** (ls-remote, fetch of merge-base geeft een fout; een lege ls-remote is geen fout): ga verder naar de classifier,
     met de melding dat git het niet kon zien. Een classifier-`permit` start dan de pusher; de
     vloer van de pusher controleert alles opnieuw. Een andere uitkomst geeft de reactie, met
     `COULD NOT CHECK` erbij.
   - **Nee**: ga verder naar de classifier.
2. **Classifier.** Invoer: de rode stap, de SHA, de branch en de tekst van het antwoord. Uitvoer:
   precies één van `permit`, `pushed` of `other`, via een JSON-schema.
   - **`permit`**: start de pusher op dezelfde rij met `Permit: <sha>`, net als `ap.push` voor
     een antwoord `push`. Dat werkt ook zonder `after`: op de fix-pr-route neemt de pusher de PR
     na de push zelf over. Een mislukte start sluit de rij als `start_failed` (zie *Herhaling*).
   - **`pushed`**: reactie *de SHA `<sha>` staat niet op `origin/<branch>`*, plus wat wel werkt.
     Sluit de rij als `question`.
   - **`other`, een fout of een time-out**: reactie met wat wel werkt. Sluit de rij als
     `question`.

### De rem

Nu zit `KindPushRecover` in groep 0 (`planned.group()`): de actie mag langs een gepauzeerde rem
en een dichte usage-rem, omdat ze niets start. Straks kan ze `claude -p` draaien (dat kost usage)
en de pusher starten. Besluit van de gebruiker:

- De git-check mag langs de rem. Bevat origin de SHA, dan gaat de keten verder zoals nu: Handoff
  of `reason_pusher_no_after`. De start die de Handoff uitlokt, blijft geremd.
- Bevat origin de SHA niet en is de rem dicht, dan wachten de classifier en de pusher. Apply
  claimt de sleutel `pushanswer:<id>` dan niet en post niets. Het antwoord blijft liggen, en een
  tick met een open rem leest het opnieuw. Je hoeft dus niet opnieuw te antwoorden.

Een git-fout telt voor de rem als *bevat de SHA niet*: met een dichte rem claimt apply niets en
post niets *(eigen keuze na spec-review, r1-f4)*. Het antwoord wordt zo uitgesteld via
`Result.Deferred` (`DeferredActions`), zodat tick de fingerprint van de repository niet opslaat
en het antwoord bij de eerstvolgende tick opnieuw leest, niet pas na de periodieke volledige
lezing *(r1-f1)*.

Apply moet daarvoor de stand van de rem kennen, bijvoorbeeld als veld op de `Action` dat de
planner zet. Zolang de rem dicht is, herhaalt elke tick de git-check voor zo'n antwoord. Dat is
acceptabel: zulke antwoorden zijn zeldzaam, en de check is één `ls-remote` plus hooguit één fetch.

### De reactie

Tick post een korte pusher-Vraag. Die heeft dezelfde headervelden als de beantwoorde Vraag (`sha`,
`worktree`, `branch`, `after`, `lane`, `step`) en geen sessie, zodat `pusherQuestion` hem
herkent. `lastTurn` ziet hem dan als de open Vraag, en een volgend antwoord krijgt een nieuwe
sleutel `pushanswer:<id>`. Er is geen nieuw header-kind nodig.

De inhoud:

- Eén regel met wat tick van het antwoord maakte: *niet op origin*, *niet begrepen als
  toestemming*, of *kon het antwoord niet lezen* (fout of time-out).
- De antwoorden die werken: `push` of een andere duidelijke toestemming, of eerst zelf pushen en
  dan `gepusht` antwoorden. Daaronder het push-commando.
- Een link naar de oorspronkelijke Vraag voor de diff en het oordeel van de reviewers. De diff
  wordt niet opnieuw gepost.

De link naar de oorspronkelijke Vraag staat als extra headerveld (bijvoorbeeld `vraag`) in de
reactie; een volgende reactie neemt dat veld over, zodat de link altijd naar de Vraag met de diff
wijst en niet naar een eerdere reactie *(eigen keuze na spec-review, r1-f7)*.

De reactie geeft het issue niet terug: geen label en geen statuswissel.

**Crashvenster.** Apply claimt de rij en zet de 🚀 vóór de classifier (tot ca. 60 s). Een tick
die in dat venster sterft, laat een open push-rij zonder pusher achter; na `push_timeout` gaat
het issue terug met `reason_pusher_silent`. Hetzelfde venster bestaat al tussen
claim en start bij het antwoord `push`; het wordt aanvaard, het is een zeldzame Returned, geen
vastloper *(r1-f2 afgewezen, r1-f6)*.

### Herhaling na een mislukte start

`pushAgain` leest een `start_failed`-pushrij terug via `pushTargetOf`. Die eist nu
`permitAnswer(com.Body)` voor een `pushanswer`-sleutel. Een rij die `permit` kreeg van de
classifier, moet bij een tweede poging ook als toestemming tellen. Daarom noteert apply
`permit` op de rij (in `runs.Row.Verdict`, nu alleen gebruikt door geplande jobs, als
`{"answer":"permit"}`). `pushTargetOf` accepteert een `pushanswer`-sleutel dan bij
`permitAnswer(body)` óf bij die notitie op een rij met dezelfde trigger-sleutel (de eerste rij
draagt de notitie, de tweede poging claimt een nieuwe rij; `pushTargetOf` krijgt daarvoor de rij
of de rijen erbij) *(r1-f5)*. Lukt het schrijven van de notitie niet, dan start apply de pusher
niet en sluit de rij als `start_failed` *(r1-f11)*. De bestaande garantie blijft: een rij die geen
toestemming kreeg, wordt nooit een push.

### De classifier

- Een nieuw veld in `dispatch.Env`, bijvoorbeeld
  `ClassifyAnswer func(ctx, AnswerInput) (AnswerVerdict, error)`. Nil betekent: geen classifier,
  dus de reactie met *kon het antwoord niet lezen*. Bedraad in `internal/cli/tick.go`, op de
  plek waar `dispatch.Env` wordt opgebouwd.
- De productie-implementatie hergebruikt `execJudge` en `judgeConfig` uit
  `internal/cli/triage_judge.go`: settings zonder hooks, geen sessie, `--output-format json`, en
  een eigen `--json-schema` met één veld `verdict` (enum `permit`, `pushed`, `other`). Geen tools
  (`--tools ""`): de classifier leest alleen de prompt.
- De classifier draait in een lege, neutrale tempdir (`cmd.Dir`), niet in de worktree van de run,
  zodat geen `CLAUDE.md` of ander projectbestand van de run de context binnenkomt *(r1-f10)*.
- Een eigen time-out (kortere orde dan de triage-judge, bijvoorbeeld 60 s). Tests kunnen die
  inkorten, zoals `judgeTimeout`.
- De prompt zet de antwoordtekst in een duidelijk afgebakend blok, als data, en zegt dat
  instructies in dat blok niet gelden. Een antwoord dat twijfel laat (een vraag, een voorwaarde,
  een ontkenning zoals `niet pushen`) is `other`.

### De grens

Wat al gold: een run post met het account van de gebruiker, en kan dus ook `push` antwoorden.
Wat nieuw is: een model leest het antwoord. Een run of geciteerde tekst kan daardoor een `permit`
uitlokken met andere woorden dan `push`. Dat verruimt de grens niet: wie `push` kan posten, kon
al toestemming geven. Toestemming blijft gebonden aan de SHA uit de Vraag, en de pusher draait
de vloer, de policy en de lint altijd. `docs/tick.md` noemt dit.

### Teksten en docs

- De pusher-Vraag (`question_push_permit`, `question_push_retry` en `question_push_moved` in
  `pack/core/headings.toml`)
  noemt dat ook een andere duidelijke toestemming werkt, en dat `gepusht` na een eigen push de
  keten voortzet. Daarna `go run ./cmd/lighthouse init --upgrade` en het geïnstalleerde
  `.lighthouse/headings.toml` committen.
- Nieuwe headings voor de reactie, in nl en en, in beide headings-bestanden.
- `docs/tick.md`, *Het antwoord `push` is toestemming* en *Afloop van een push-rij*: de stroom
  hierboven, de classifier, wat hij leest, de time-out en de grens.
- `reason_pusher_answer_moved` en `reason_pusher_git_failed` blijven in `pack/core/headings.toml`
  staan, voor het teruglezen van oude Returned-comments door `ReasonKey`, ook al zendt apply ze
  niet meer uit *(r1-f8)*. `docs/tick.md` (regels rond 444-452) wordt herschreven.
- Een changelog-fragment `changelog.d/issue-552-<slug>.md`.

## Bewijs

- Dispatch-tests met een nep-`LsRemote`, een nep-git voor fetch en voorouder, en een
  nep-classifier:
  - exact `push`: `KindPush`, de classifier wordt niet aangeroepen;
  - origin gelijk aan de SHA, en origin een merge bovenop de SHA: Handoff, geen classifier;
  - niet op origin, met elk van de drie uitkomsten `permit`, `pushed` en `other`;
  - een classifier-fout en een time-out: één reactie, niets gestart, geen label;
  - een `start_failed`-rij na een classifier-`permit`: `pushAgain` geeft een tweede
    `KindPush` met `PermitSHA`.
- Een decide-test: na de reactie is die de open Vraag, en een tweede antwoord krijgt een nieuwe
  sleutel.
- Een Vraag zonder `after`: `permit` start de pusher, *origin bevat de SHA* gaat terug met
  `reason_pusher_no_after`, en `other` geeft de reactie.
- De rem dicht: *origin bevat de SHA* geeft de Handoff; *niet op origin* roept de classifier niet
  aan, claimt de sleutel niet en post niets. Met de rem weer open loopt hetzelfde antwoord door
  naar de classifier.
- De fetch verandert `refs/remotes/origin/<branch>` en `FETCH_HEAD` in de worktree niet.
- Een test op `execJudge` voor de classifier met een korte time-out, zoals
  `triage_judge_test.go:109`.

## Aan de bouwer

- `execJudge` heeft nu `judgeArgs`, `tri.Schema`, `tri.Model`, `judgeTimeout` en de volledige
  `os.Environ()` vast. Maak die parameters, zonder te veranderen wat de triage-judge doet.
- Een mislukte post van de reactie: behandel die als `recoverPostFailed` nu.
- Git faalde en de classifier zegt `pushed`: de reactie noemt `COULD NOT CHECK` in plaats van
  *niet op origin*.
- Welke renderer de korte pusher-Vraag schrijft (de renderer van de pusher exporteren, of een
  nieuwe in `internal/protocol`), mits de headervelden gelijk zijn.
- De exacte namen van de headings, het Env-veld en de time-outconstante.
- Of de git-check (ls-remote, fetch, merge-base) één Env-functie wordt of drie.
