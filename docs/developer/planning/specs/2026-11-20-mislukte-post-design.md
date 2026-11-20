# Ontwerp: een mislukte post laat de keten niet stilstaan

Datum: 2026-11-20 · Issue: [#395](https://github.com/CorneeldH/lighthouse/issues/395), criteria 3 en 5

## Aanleiding en doel

Bij #121 (2026-11-16) had een controller-run zijn werk af, maar kon hij zijn handoff niet posten:
de GraphQL-limiet was op. De run stopte, de dispatcher zag *klaar, geen PR, geen Handoff of Vraag*
(regel 14 in [`docs/tick.md`](../../../tick.md)) en gaf het issue terug met *geen handoff of vraag*.
Het werk was gedaan, maar de keten stond stil, en niets liet zien dat een post was mislukt.

Criteria 1, 2 en 4 van #395 zijn af met #428: de controller leest en schrijft de forge via REST.
Dit ontwerp dekt de twee criteria die nog open staan:

- **AC3.** Een handoff en een spec- of planversie posten via REST, niet via GraphQL.
- **AC5.** Faalt een post toch, dan staat het werk niet stil zonder spoor. De keuze staat in
  `docs/tick.md`.

Succes:

- `lighthouse post-artefact` doet geen GraphQL-request meer, ook niet als het de branch moet
  pushen.
- Een controller-run waarvan de handoff- of vraagpost mislukt, eindigt niet in *geen handoff of
  vraag*. De controller post de handoff of vraag zelf, en de keten loopt verder.
- Kan de controller de mislukte post niet overnemen, dan geeft hij het issue terug met de fout van
  de run als reden, niet met *geen handoff of vraag*.

## Stand van nu

- **Handoff.** Stap 5 van `write-handoff` post al met `lighthouse forge issue comment`, dus via
  `forge.CreateComment` (REST). De issuetekst over `gh issue comment` is van vóór die wijziging.
- **Vraag.** `lighthouse ask` post via `forge.CreateComment` (REST).
- **Spec- en planversie.** `lighthouse post-artefact` (`internal/postartefact`) roept `gh` aan.
  `commitKnown`, `readComments` en `postComment` zijn `gh api` op REST-paden. Eén aanroep is
  GraphQL: `openPR` doet `gh pr list --head <branch> --state open`. Die loopt alleen als GitHub de
  commit nog niet kent, en dat was precies het geval bij #121 (`artefact: gh failed: GraphQL: API
  rate limit already exceeded`).
- **Spoor.** De dispatcher beslist *geen handoff* alleen op de forge: klaar, geen Handoff, geen
  Vraag en geen PR (`noHandoff` in `internal/dispatch/decide.go`). Een mislukte post en een agent
  die de handoff vergat, zijn voor hem hetzelfde.
- **Voorbeeld van een spoor dat de run achterlaat.** De `permission-check`-hook schrijft
  `<LIGHTHOUSE_STATE_DIR>/runs/<run>/permission.json`. De tick leest die lokaal
  (`dispatch.ReadPermission`), of op een node via `GET …/runs/{session}/permission` (`nodeapi`,
  `nodeserve`, `kennforge.nodeclient`).

## Doelen, niet-doelen, anti-doelen

**Doelen.**

- `openPR` in `post-artefact` via REST.
- Een mislukte post van een handoff of vraag in een controller-run laat een notitie achter met de
  volledige body.
- De controller leest die notitie en post de handoff of vraag zelf, of geeft het issue terug met de
  fout als reden.
- `docs/tick.md` beschrijft de nieuwe regel en de keuze voor deze aanpak.

**Niet-doelen.**

- `post-artefact` op `internal/forge` zetten, of GitLab-ondersteuning ervoor. Dat hoort bij #260.
- De `gh`-aanroepen in `close-issue`. Die liggen niet op het pad van een handoff.
- Een mislukte `post-artefact` overnemen. Die blokkeert de handoff al niet: de `artefact:`-regel
  komt in de handoff onder `Watch out:`, en dat is het spoor.
- Het GraphQL-gebruik dat nog over is in de GitHub-forge (`IssueItem`, `Planning`). Dat is
  [#440](https://github.com/CorneeldH/lighthouse/issues/440).
- Opnieuw proberen binnen de run (backoff). De run stopt na een mislukte handoffpost, zoals nu.

**Anti-doelen.**

- De controller post iets anders dan de run wilde posten. De body gaat letterlijk over, of
  helemaal niet.
- De controller post een handoff of vraag voor een ander issue dan dat van de run.
- Een tweede handoff op het issue omdat de tick de regel twee keer uitvoert.

## Aanpakken

Drie aanpakken voor AC5 zijn overwogen:

- **A. Alleen een reden.** De run schrijft een notitie met de fout. De controller geeft het issue
  terug met *handoff niet gepost: <fout>*. Dat is een duidelijk spoor, maar de keten staat nog
  steeds stil tot iemand de handoff met de hand post.
- **A+. De controller neemt de post over** (gekozen). Zoals A, maar de notitie bevat ook de
  volledige body. De controller post die zelf, via REST, en probeert het elke tick opnieuw tot het
  lukt. De posts van de run zijn al REST (`forge.CreateComment`); als zo'n post toch faalt, is het
  een REST-limiet, een netwerkfout of een 5xx. Dat gaat meestal over. De winst van A+ zit dus in
  het opnieuw proberen in de tijd, niet in een andere API. Aanpak A blijft over als vangnet, voor
  een notitie die de controller niet kan overnemen of die hij tot de limiet niet gepost krijgt.
- **B. Herkenning in het transcript.** De transcriptlezer herkent een mislukte
  `lighthouse forge issue comment`. Er komt geen nieuw bestand bij, maar het herkennen gebeurt op
  commandotekst en foutmeldingen, en dat is breekbaar. Bovendien heeft de controller dan geen body
  om te posten.

De gebruiker koos A+, en de controller mag zowel een Handoff als een Vraag overnemen.

## Ontwerp

### AC3: `openPR` via REST

`openPR` in `internal/postartefact/postartefact.go` wordt:

```
gh api "repos/<repo>/pulls?head=<owner>:<branch>&state=open&per_page=1" --jq length
```

`<owner>` is het deel van `forge.repo` vóór de `/`. Dat volgt de stijl van de rest van het pakket:
`gh api` via `procrun`. Een open PR betekent dat de lengte niet `0` is, net als nu. De replay-test
in `internal/postartefact/postartefact_test.go`, die nu op `pr list` matcht, gaat mee.

### De notitie: `post-failed.json`

**Wie schrijft.** `lighthouse forge issue comment` en `lighthouse ask`, alleen in een controller-run:
`LIGHTHOUSE_CONTROLLER_RUN` heeft de uuid-vorm, en `LIGHTHOUSE_STATE_DIR` is gezet. Beide komen al
mee bij elke launch (`internal/workplace/carrier.go`). Buiten een controller-run schrijft niemand
iets.

**Wanneer.** Bij elke fout nadat de bedoeling van de run vaststaat:

- `forge issue comment`: zodra het body-bestand gelezen is. Dus ook een fout bij het openen van de
  forge (token, config) en een fout van `CreateComment`. Een onleesbaar body-bestand of een
  ongeldig nummer schrijft niets.
- `ask`: zodra de invoer geldig is (nummer en velden). Dus ook een fout bij `c.Issue`, de GET die
  de titel ophaalt vóór de body bestaat; een REST-limiet raakt die GET het eerst. Is de body er
  nog niet, dan blijft `body` in de notitie leeg. Een vraag die anders terugleest, schrijft niets:
  dat is een fout in de vraag, geen mislukte post.
- `ask --dry-run` schrijft nooit.

*(Eigen keuze na de bouwercheck: een fout vóór de POST telt mee, anders eindigt de meest
waarschijnlijke fout van `ask` nog steeds in* geen handoff of vraag*.)*

**Waar.** `<LIGHTHOUSE_STATE_DIR>/runs/<run>/post-failed.json`, naast `permission.json`. Een
volgende mislukte post in dezelfde run overschrijft de notitie: alleen de nieuwste telt.

**Inhoud.** De volgorde van de velden is het contract:

```json
{
  "version": 1,
  "at": "2026-11-20T21:04:05.123Z",
  "issue": 121,
  "command": "forge issue comment",
  "error": "forge POST https://api.github.com/…: 403 …",
  "body": "🤖 Handoff\nissue: #121\n…"
}
```

`command` is `forge issue comment` of `ask`. `at` is in milliseconden, zoals `permission.json`.
`error` is de foutregel zoals de run hem op stderr kreeg, op één regel.

**Als schrijven mislukt.** Dan meldt het commando op stderr een regel met `COULD NOT CHECK` en de
reden, zoals de `permission-check`-hook dat doet. De exitcode blijft die van de mislukte post
(rood). Het schrijven van de notitie maakt een mislukte post nooit groen.

**Skill.** In stap 5 van `write-handoff` komt één zin bij: in een controller-run noteert
`lighthouse forge issue comment` een mislukte post zelf voor de controller; stop daarna zoals nu.
`lighthouse ask` heeft geen skilltekst nodig.

### De controller: lezen

De tick leest de notitie op dezelfde manier als `permission.json`:

- **Lokaal** uit `<stateDir>/runs/<sessionID>/post-failed.json`.
- **Op een node** via een nieuwe route `GET …/runs/{session}/post-failed`. Zonder notitie geeft die
  `404` met code `no_post_failed`, net als de permission-route. Een node die niet antwoordt, maakt
  de run onmeetbaar voor die tick, net als bij permission en transcript. Een `404` *zonder* die
  code komt van een node die de route nog niet kent: die telt als "geen notitie", en de run valt
  terug op het gedrag van vandaag (regel 14). `nodeapi.Version` blijft 1. *(Eigen keuze na de
  bouwercheck: de notitie is een aanvulling, dus een oude node mag nooit een run onmeetbaar
  maken.)*

Een notitie die niet te lezen is (geen JSON, een andere versie, geen `at`) is geen fout die de tick
stopt. Ze telt als een onbruikbare notitie (zie de regel hieronder), zodat de reden de gebruiker
bereikt. Kopieer het patroon van `ReadPermission` dus niet letterlijk: dat maakt van een
onleesbare notitie een fout, en dan blijft de run onmeetbaar. Een notitie met een `at` van vóór `started_at` van de run hoort bij een eerdere run in
dezelfde sessie en telt niet.

### De controller: de regel

Er komt een nieuwe regel, **14a**, vlak vóór regel 14. Hij geldt als de run klaar is, er geen PR van
deze run is, geen Handoff of Vraag van deze run, en er wel een notitie is van op of na
`started_at`. Regels 1-3 gaan voor: staat de Handoff of Vraag er inmiddels wel (de run probeerde
het opnieuw en het lukte), dan doet de notitie niet meer mee.

| Notitie | Uitkomst | Reden op de 🤖 Teruggegeven |
|---|---|---|
| De body leest als een 🤖 Handoff of 🤖 Vraag (`protocol`) voor het issue van de run, en de run is jonger dan `max_runtime` (of `max_runtime` is 0) | de controller post de body letterlijk, met `forge.CreateComment`. De run blijft open. | |
| Idem, maar de run is `max_runtime` of ouder | Teruggegeven, zoals regel 14 | *post mislukt: <error uit de notitie>* |
| Anders: een lege body, een andere soort comment, of een ander issue | Teruggegeven, zoals regel 14 | *post mislukt: <error uit de notitie>* |
| De notitie is onleesbaar | Teruggegeven, zoals regel 14 | *post mislukt: notitie onleesbaar* |

**Waarom de run open blijft.** De volgende tick ziet de Handoff of Vraag op het issue en sluit de
run met regel 2 of 3, zoals elke andere run. De bestaande vervolgregels (de start op de handoff,
regel 13c bij `next: /lighthouse-close-issue`) werken dan zonder aanpassing. Een geposte Vraag
draagt de sessie van de run al in zijn kop, en een Handoff draagt geen sessie. Beide tellen dus
voor de run.

**Idempotent.** Staat de post eenmaal, dan geldt regel 14a niet meer, want regel 2 of 3 gaat voor.

**Als de post van de controller mislukt.** Dan blijft de run open en probeert de volgende tick het
opnieuw. Apply logt de fout op één regel (repo, issue, run, fout). Het is **geen fout van de
tick**: de andere acties en de tickstaat gaan gewoon door. Anders zou een blijvende fout, zoals een
422 of een 403, elke tick rood maken. `Decide` blijft puur: het telt geen pogingen. De bovengrens
is de rij met `max_runtime` hierboven. Regel 14a staat vóór regel 19, dus 14a regelt die grens
zelf, met de fout van de run als reden. Met `max_runtime` op 0 probeert de controller het zonder
grens opnieuw, net zoals een run zonder `max_runtime` zonder grens open blijft. *(Eigen keuze na
de bouwercheck.)*

**Reden-sleutels.** Nieuw in `pack/core/headings.toml`: `reason_post_failed` (nl *post mislukt*,
en *post failed*). De fout of *notitie onleesbaar* komt erachter, zoals bij de andere redenen met
een argument.

### `docs/tick.md`

- Regel 14a in de tabel van de run-uitkomsten.
- Een alinea over `post-failed.json` naast die over `permission.json`: wie schrijft, waar, wanneer,
  en dat alleen de nieuwste telt.
- De keuze: A+ boven A en B, met de reden uit *Aanpakken*.

## Randvoorwaarden

- Code, namen, commentaar en foutmeldingen in het Engels. Spec, plan, commits, PR en comments in
  het Nederlands.
- Een wijziging onder `pack/` loopt ook `go run ./cmd/lighthouse init --upgrade`, en de
  geïnstalleerde kopie gaat mee in de commit.
- `/lighthouse-go-standard` en `/lighthouse-test-standard` gelden. Elke PR krijgt een fragment in
  `changelog.d/`.
- De PR schrijft `Closes #395`: met deze twee criteria zijn alle criteria van #395 af.

## Aan de bouwer

- De Go-namen en waar de schrijver van de notitie staat. Een voorstel: pad, lezer en parser in
  `internal/dispatch/runstate.go` naast `ReadPermission`, en één schrijffunctie die
  `forge issue comment` en `ask` delen.
- Hoe de tick de notitie doorgeeft aan `decide` (een veld naast `Permission` in `RunState` ligt
  voor de hand) en hoe `apply` de post uitvoert (een nieuw soort `Action`, of een bestaand veld).
- Of de node-lezer `workplace.PermissionReader` uitbreidt of een eigen interface krijgt.
- De testcases. Wel vast: per uitkomst van regel 14a een geval in de decide-testdata, een test dat
  een tweede tick na een geslaagde post niet opnieuw post, en een test dat een post buiten een
  controller-run geen notitie schrijft.
