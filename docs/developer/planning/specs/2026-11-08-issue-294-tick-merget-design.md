# De tick merget een groene eigen PR, of start fix-pr (#294)

Issue: [#294](https://github.com/CorneeldH/lighthouse/issues/294), neemt
[#295](https://github.com/CorneeldH/lighthouse/issues/295) mee. Milestone: M2 — Dispatcher.

## Waarom

Een controllerrun eindigt nu bij een groene PR. De tick post 🤖 Klaar en beëindigt de claude van
de run (regel 7 van de afloop-tabel in [`docs/tick.md`](../../../tick.md), `runCtx.green` in
`internal/dispatch/decide.go`). Daarna staat de PR stil tot een mens hem merget. Pas na die merge
start de nacontrole (`closeCheck`, `/lighthouse-close-issue N`). De mens is zo de rem op een keten
die verder zonder hem loopt. Die mens toetst bij de merge niets wat de tick niet ook kan meten:
groene checks, geen conflict, de review verwerkt.

Een PR met een merge-conflict valt nu tussen `prRed` (alleen rode checks) en `greenPR` (eist
`MERGEABLE`) door. De run blijft open tot een tijdregel hem teruggeeft, met een reden die het
conflict niet noemt (#295).

## Doelen

1. De tick squash-merget een groene eigen PR van een klare run zelf, deterministisch in `apply`.
2. Bij een conflict, een onverwerkte roborev-review of een merge die faalt op de PR zelf, start de
   tick `/lighthouse-fix-pr <pr>` met dezelfde poorten als bij rode checks.
3. Na de merge loopt de bestaande nacontrole ongewijzigd.

## Niet-doelen

- **Geen merge-run.** Geen sessie die merget. De runs houden `--strict-mcp-config` en krijgen geen
  merge-rechten.
- **Geen merge van een PR die niet van de run is**, en geen fix-pr daarop.
- **Geen configuratieschakelaar** om het mergen per repo uit te zetten (eigen keuze, zie *Aan de
  builder*).
- **Geen opruimen van de head-branch** na de merge. Dat laat de tick aan de repo-instelling
  *Automatically delete head branches* over.
- **Geen andere merge-methode dan squash**, en geen eigen commit-titel: GitHub gebruikt de
  squash-titel uit de repo-instelling.

## Anti-doelen

- Een merge van werk dat de tick niet gemeten heeft: een push tussen meten en mergen.
- Een review die iemand anders dan een vertrouwde auteur als verwerkt kan markeren.
- Een fix-pr-lus op een PR die fix-pr niet kan repareren (branch protection, squash uitgezet).
- Een tick die bij elke race met een push rood eindigt.

## Randvoorwaarden

- **Decide blijft puur.** De merge is een bijwerking en hoort in `apply`. Een fix-pr-start komt
  altijd uit decide, zodat de rem (#296) erover beslist.
- **De sleutel en het plafond van #119 blijven.** Eén fix-pr-start per PR en head-SHA
  (`fixpr:<pr>:<sha>`), hoogstens `maxFixPRRuns` (3) fix-pr-runs per PR. De oorzaak verandert de
  sleutel niet.
- **#296 is gemerged** (`settleUnstartedFixPR`). Een fix-pr-start die de rem uitstelt, laat de run
  open. Dat moet ook gelden voor de nieuwe oorzaken.
- **De PR-body blijft `Refs #N`.** De merge sluit het issue dus niet, en `closeCheck` start de
  nacontrole.
- **Dogfood.** De wijziging in `pack/core` (skill, headings) draait
  `go run ./cmd/lighthouse init --upgrade`, en de geïnstalleerde kopie gaat mee.
- **Taal.** Code, comments en skilltekst in het Engels. Spec, docs, headings-`nl` en de changelog in
  het Nederlands.

## Aanpakken

Mergen in `apply` en niet in een sessie, en een mislukte merge naar fix-pr: dat zijn keuzes van de
gebruiker uit het issue. Er stonden nog twee bouwvragen open.

**Een onverwerkte roborev-review meten.** De snapshot leest nu geen PR-comments.

| | Aanpak | Afweging |
|---|---|---|
| **A** | **`gather` leest via REST de comments van een mergeklare open PR** van een item met een open run (`Client.Comments` op het PR-nummer) | Eén call per zo'n PR per gather. Meestal nul of één per tick. |
| B | `comments(last: n)` in `prFields` van GraphQL | Vermenigvuldigt de kosten van de board-query met elke PR van elk item. Afgewezen. |

**De mislukte merge doorgeven aan de volgende tick.** Een merge die in `apply` faalt, kan fix-pr
niet in dezelfde tick starten, want de rem beslist in decide.

| | Aanpak | Afweging |
|---|---|---|
| **A** | **`apply` noteert de SHA en de fout op de open run-rij** (nieuwe kolommen). De run blijft open. | Klein, en alleen de tick leest het. Een mens ziet de fout pas in de Teruggegeven (plafond of gebruikte SHA). |
| B | Een nieuwe 🤖-kop op het issue | Zichtbaar voor mensen, maar vraagt om een nieuw protocoltype, nieuwe headings en aanpassingen in de UI. |

**Gekozen door de gebruiker: A en A.**

## Ontwerp

### 1. Roborev-stand in de snapshot

`forge.PR` krijgt een veld `Roborev string` (`json:"roborev,omitempty"`), met de waarden:

- `""`: niet gemeten, of geen review;
- `"processed"`: een review met bevindingen waarvan de SHA een geldige marker heeft;
- `"open"`: een review met bevindingen zonder geldige marker.

`Complete` in `gather.go` meet dit alleen voor een PR die aan alle voorwaarden van een merge voldoet,
behalve de review. De PR is `OPEN`, geen draft, `MERGEABLE` en de checks zijn `SUCCESS`. Het item
heeft een open run. De andere PR's houden `""`. Meting:

1. `c.Comments(ctx, pr.Number)`. Het issues-comments-endpoint geeft ook de comments van een PR.
2. **De review** is de laatste comment van auteur `github-actions[bot]` of `github-actions` (dezelfde
   twee die fix-pr leest) waarvan de body `roborev-pr-comment` bevat. Paia post hem met
   `--upsert-comments`, dus het is meestal één comment die bijgewerkt wordt. Een comment van een
   andere auteur telt niet.
3. De review telt alleen mee als aan dezelfde voorwaarden is voldaan als in fix-pr §6, voor zover de
   tick die kan meten:
   - de repo heeft een `.roborev.toml` in de main checkout. Zonder dat bestand blijft de stand `""`,
     en dan leest de tick ook geen comments;
   - de kop is `` ## roborev: <kind> (`<sha>`) ``, met als kind `Review Complete` of
     `Combined Review`;
   - de body bevat `Changes require fixes`.

   Een andere kop, geen kop of een ander oordeel geeft `""`.

   Of de bevindingen leesbaar zijn (`roborev_gate.py classify`), meet de tick niet. Bij een
   onleesbare review post fix-pr geen marker. De sleutel is dan gebruikt, en de volgende tick geeft
   het issue terug met `reason_pr_roborev`. Dat kost één fix-pr-run (eigen keuze).
4. **De marker** is een regel `<!-- roborev-verwerkt: <sha> -->` in een comment van een
   **vertrouwde** auteur (`Trust.Trusted`, dezelfde `answer_permissions`). De SHA is gelijk aan die
   van de review. Bij een verschil in lengte (kort tegen lang) telt een prefix. Zonder deze
   vertrouwenscheck kan iedereen op een publieke repo een review wegstrepen en een merge uitlokken.

Een mislukte call laat de hele gather falen, zoals de comments van een issue nu.

Een review zonder marker telt als onverwerkt, ook als die review de vorige head beoordeelde. Fix-pr
verwerkt hem dan alsnog en zet de marker. Na een fix-pr-push reviewt paia niet opnieuw (geen
`synchronize`), dus de marker van de oude SHA blijft gelden.

**Waarom de comment er al staat bij groene checks.** In paia is de roborev-job een check van de PR,
en de stap `Review` heeft `continue-on-error`. De rollup is pas `SUCCESS` als die job klaar is, en de
job post de comment vóór hij eindigt. In lighthouse en bij een PR met `roborev-lokaal:`-marker
reviewt CI niet, en is er geen bot-comment. De stand blijft dan `""`. De roborev-stand staat niet in
de vingerafdruk van de precheck, want die leest geen comments. Elke nieuwe review komt met een
wijziging die wel in de vingerafdruk staat: een nieuwe head, of een heropende PR.

### 2. De afloop-regels in decide

De volgorde in `runCtx.outcome` wordt als volgt. Nieuw of gewijzigd is vet.

`parked`, `merged`, `closedIssue`, **`mergeReady`**, **`green`**, `gone`, `unknownCommand`,
`didNothing`, `prClosed`, `prDraft`, **`fixPR`** (was `prRed`), `noHandoff`, `uncertain`,
`permission`, `silent`, `silentForge`, `timeout`.

**`mergeReady`** (nieuwe regel 7). Er moet een open eigen PR zijn (`ownPR`), de eerste op nummer. Die
voldoet aan alle voorwaarden:

- de run is klaar (`c.done()`);
- de PR is `OPEN`, geen draft, `Mergeable == "MERGEABLE"` en `Checks == "SUCCESS"`;
- `Roborev != "open"`;
- er staat geen merge-fout op deze head: `row.MergeSHA != pr.HeadSHA` (zie §4);
- **de rem staat de merge toe** (keuze van de gebruiker): het item staat in `Controller`, en er
  geldt geen pauze (`in.Brake == nil || !in.Brake.Paused`).

Uitkomst: een nieuwe actie `KindMerge` met `RunID`, `SessionID` (die van de rij), `PR` (nummer),
`HeadSHA`, `PRURL` en `End: true`. `Action` krijgt de velden `PR int` en `HeadSHA string`.

**Een tegengehouden merge.** Voldoet de PR aan alles behalve de laatste voorwaarde, dan geeft
`mergeReady` niets. De run blijft open, en de regels na `mergeReady` gelden zoals altijd. Geen van die
regels pakt een klare run met een groene eigen PR op, behalve `timeout` (`max_runtime`). Een tick
na de pauze, of met het item terug in `Controller`, merget alsnog. Gevolgen:

- zo'n run houdt een plek bezet (`in_use`), en zijn claude blijft leven;
- een pauze die langer duurt dan `max_runtime` geeft de run terug met "time-out", zonder merge.

De mens die pauzeert of het item verplaatst, kiest daar zelf voor. Fix-pr-starten volgen tijdens
een pauze de regels van #296. Buiten `Controller` start de tick niets, zoals nu.

**`green`** (regel 7b, zoals nu, maar smaller). Een groene PR (`greenPR`) die niet van de run is,
terwijl de run klaar is en geen open eigen PR heeft. Uitkomst: 🤖 Klaar zonder merge (`KindDone`),
zoals nu. Heeft de run wel een open eigen PR, dan beslissen de regels voor die PR. Een groene
vreemde PR sluit de run dan niet meer af.

**`fixPR`** (regel 13, vervangt `prRed`). Hij geldt alleen als de run klaar is. Voor de open eigen PR
geldt de eerste oorzaak die opgaat:

| Oorzaak | Voorwaarde op de eigen PR | Reden bij teruggeven | Plafondreden |
|---|---|---|---|
| rood | `Checks` is `FAILURE` of `ERROR` | `reason_pr_red` | `reason_pr_red_ceiling` |
| conflict | `Mergeable == "CONFLICTING"` | `reason_pr_conflict` | `reason_pr_conflict_ceiling` |
| merge | `row.MergeSHA == pr.HeadSHA` | `reason_merge_failed` + `row.MergeError` | `reason_merge_failed_ceiling` |
| roborev | `MERGEABLE`, `SUCCESS` en `Roborev == "open"` | `reason_pr_roborev` | `reason_pr_roborev_ceiling` |

Per oorzaak gelden dezelfde poorten als nu in `prRed`, in dezelfde volgorde:

- `fixPRRuns` ≥ 3: teruggeven met de plafondreden;
- `spentKey`: teruggeven met de reden van de oorzaak;
- anders sluit de run als handoff met de fix-pr-sleutel, en geeft de regel de start terug.

`fixPRStart` krijgt de oorzaak, als de headings-sleutel van de reden (plus het argument van de
merge-fout).

**Een vreemde PR** (geen eigen open PR die onder een oorzaak valt):

- rood: teruggeven met `reason_pr_red`, zoals nu;
- `CONFLICTING`: teruggeven met `reason_pr_conflict` (#295);
- een vreemde PR met een open review: er gebeurt niets extra. Wordt hij groen en is er geen eigen
  PR, dan geldt `green`.

**`mergeable: UNKNOWN`** (GitHub rekent nog) valt onder geen enkele regel. De run blijft open en de
volgende tick meet opnieuw. Hetzelfde geldt voor checks `PENDING` of `""`. Dat is het gedrag van nu.

### 3. De merge in apply

`forge.Client` krijgt `MergePR(ctx, number int, sha string) error`. Die doet
`PUT /repos/{owner}/{repo}/pulls/{number}/merge` met `{"sha": sha, "merge_method": "squash"}`. De
fout onderscheidt twee gevallen:

- `ErrHeadMoved`: status 409. Volgens GitHub is dat alleen "head branch was modified" bij een
  meegegeven `sha`.
- `*MergeRefused` met de `message` van GitHub: status 405 of 422. Dat is "not mergeable", branch
  protection, of squash staat uit.

Ook een 403 zonder teken van een rate limit telt als `MergeRefused`. Een fine-grained token zonder
*Contents: write* en *Pull requests: write* geeft dan geen eindeloos rode tick. Na één fix-pr-run
gaat het issue terug met "merge mislukt: Resource not accessible …" (eigen keuze). Tekenen van een
rate limit: `x-ratelimit-remaining: 0`, `retry-after`, of "rate limit" in de `message`.
`docs/tick.md` stap 2 noemt de nieuwe eisen aan het token. Andere fouten (netwerk, 5xx, 401, 404, een
403 door een rate limit) blijven gewone fouten. Een 200 zonder `merged: true` telt
als `MergeRefused`. De commentaar bij de package (`It never merges`) gaat eruit.

`apply` voor `KindMerge`:

1. `MergePR(a.PR, a.HeadSHA)`.
2. **Gelukt:** post 🤖 Klaar (`RenderDone`) met `PRURL` en de nieuwe noot `note_merged`. Dan
   `release` (de claude beëindigen), dan `closeRun(..., runs.Done, "")`. Mislukt de post na een
   gelukte merge, dan is de actie mislukt (exitcode 1). De volgende tick sluit de run via
   `merged` (regel 6), zonder Klaar. Dat is aanvaardbaar.
3. **`ErrHeadMoved`:** er gebeurt niets op de forge en niets op de rij. `apply` schrijft
   `lighthouse: <repo>#<n>: merge of PR #<pr> skipped: head moved` naar de log. De actie telt als
   gelukt. De nieuwe head verandert de vingerafdruk, dus de volgende tick gathert en meet opnieuw.
4. **`MergeRefused`:** `runs.NoteMergeFailed(ctx, db, runID, sha, message)` schrijft de SHA en de
   fout op de open rij. Er volgt geen post, en de rij en de claude blijven zoals ze zijn. Lukt
   het noteren, dan telt de actie als gelukt, met een regel in de log. Lukt het noteren niet, dan
   is de actie mislukt.
5. **Andere fout:** de actie is mislukt (exitcode 1), en er wordt niets genoteerd. De vingerafdruk
   wordt niet bewaard, dus de volgende tick probeert het opnieuw.

### 4. De mislukte merge op de run-rij

Migratie `internal/store/migrations/0006_runs_merge.sql` (`0005` bestaat al):

```sql
ALTER TABLE runs ADD COLUMN merge_sha TEXT NOT NULL DEFAULT '';
ALTER TABLE runs ADD COLUMN merge_error TEXT NOT NULL DEFAULT '';
```

- `runs.Row` en `runs.OpenRun` krijgen de velden `MergeSHA` en `MergeError`. `History` en het lezen
  van de open runs vullen ze.
- `runs.NoteMergeFailed` schrijft alleen op een open rij.
- `fpRun` in de vingerafdruk krijgt `MergeSHA`. Een genoteerde fout verandert zo de vingerafdruk, en
  de volgende tick slaat de gather niet over.

De fout hoort bij de head: `mergeReady` en `fixPR` lezen hem alleen als `row.MergeSHA ==
pr.HeadSHA`. Na een push (door fix-pr of een mens) telt de oude fout niet meer, en probeert de tick
opnieuw te mergen. Een fix-pr-run is een nieuwe rij, met lege velden.

Gevolg voor een fout die fix-pr niet kan oplossen, zoals een verplichte review:

1. De merge faalt en de fout wordt genoteerd.
2. De fix-pr-start volgt (sleutel `fixpr:<pr>:<sha>`).
3. Fix-pr vindt de PR groen en stopt zonder push.
4. De volgende tick probeert de merge opnieuw. Die faalt weer, op dezelfde SHA.
5. De sleutel is gebruikt, dus het issue gaat terug met "merge mislukt: <fout van GitHub>".

Dat kost één fix-pr-run. Dit is de prijs van de keuze "falende merge naar fix-pr".

### 5. Fix-pr-starten die niet doorgaan (#296)

`settleUnstartedFixPR` zet nu een vaste `reason_pr_red` op een fix-pr-close zonder start (een actieve
blokkeerder). `apply.returnFixPR` doet hetzelfde voor een eerste fix-pr-start die stopt vóór zijn
claim. Beide nemen voortaan de reden van de oorzaak over:

- `planned` krijgt een veld `fixReason` (de headings-sleutel) en `fixReasonArg`, gezet door
  `decideIssue` uit `fixPRStart`;
- de fix-pr-startactie draagt de reden in `Reason` en `ReasonArg`. Op een `KindStart` zijn die
  velden nu leeg.

Het gedrag van #296 blijft verder gelijk. Stelt de rem de start uit, dan vervalt de close en blijft
de run open. Bij een merge-fout blijft ook de notitie op de rij, dus de volgende tick kiest dezelfde
route.

### 6. Headings

In `pack/core/headings.toml`, `nl` en `en`:

| Sleutel | nl |
|---|---|
| `reason_pr_conflict` | PR heeft een merge-conflict |
| `reason_pr_conflict_ceiling` | PR heeft een merge-conflict na 3 fix-pr-runs |
| `reason_pr_roborev` | PR heeft een onverwerkte roborev-review |
| `reason_pr_roborev_ceiling` | onverwerkte roborev-review na 3 fix-pr-runs |
| `reason_merge_failed` | merge mislukt |
| `reason_merge_failed_ceiling` | merge mislukt na 3 fix-pr-runs |
| `note_merged` | Checks groen, door de controller gesquasht en gemerged. Staat het issue nog open, dan toetst de controller het opnieuw. |

`note_done` blijft voor een groene PR die niet van de run is. De tekst "Mergen blijft bij jou" klopt
daar nog.

### 7. Documentatie en skill

- **M2-spec**
  ([2026-10-27-m2-dispatcher-design.md](2026-10-27-m2-dispatcher-design.md), *No-Gos*). "lighthouse
  merget nooit" gaat eruit. Op die plek komt de keuze met de reden:
  - de tick merget alleen een groene eigen PR van een klare run;
  - de merge is deterministisch, met de gemeten SHA;
  - geen sessie krijgt merge-rechten;
  - de nacontrole toetst het bewijs na de merge en heropent het issue als het niet houdt;
  - de mens was de enige rem op een keten die hij verder niet bijstuurt, en hij toetste niets wat de
    tick niet kan meten.

  Verwijs naar #294.
- **Roborev-spec van #119** (*Niet-doelen*: "Geen trigger op een onverwerkte roborev-comment"). Voeg
  een regel toe dat #294 dit omdraait. De oude tekst blijft als geschiedenis staan.
- **`docs/tick.md`**:
  - de afloop-tabel krijgt 7 (merge), 7b (groene vreemde PR) en 13 (fix-pr met vier oorzaken), en
    de vreemde PR met conflict;
  - stap 7 *Gather* noemt het lezen van de PR-comments;
  - *Apply* beschrijft de merge en de drie soorten fout;
  - *De rem* zegt dat een pauze en een item buiten `Controller` ook de merge tegenhouden;
  - stap 2 *Token* noemt de rechten om te mergen (*Contents: write*, *Pull requests: write*);
  - de alinea over `End` noemt de merge.
- **`docs/site/data/loop.json`**: de fix-pr-node en de tekst bij regel 580 ("fix-pr repairs a red
  PR …") noemen conflict, roborev en de mislukte merge, en noemen dat de tick merget.
- **`pack/core/skills/fix-pr/SKILL.md`**, de passages die zeggen dat de dispatcher alleen op rood
  start en dat de merge een menselijke beslissing is:
  - *Domain*: "The dispatcher … starts this skill only on a red PR … never for a roborev review on a
    green PR" en "the merge itself — that is a human decision";
  - §6 punt 4: "The dispatcher … does not read the marker".

  Nieuwe tekst: de dispatcher start fix-pr ook bij een conflict, een onverwerkte roborev-review en
  een merge die faalde, en leest de marker. Fix-pr merget zelf nog steeds niet. Bij een merge die
  faalde en een PR die groen is, rapporteert fix-pr de fout uit de Teruggegeven niet. Hij stopt bij
  groen zoals nu.
- **`changelog.d/`**: een fragment van het type `added`.

### 8. Tests

In `internal/dispatch`, met de bestaande tabel- en fixturestijl (`testdata/decide/*.json`). Elke test
moet falen als de genoemde regel wegvalt:

- `mergeReady`: één test per voorwaarde die faalt, waarbij de rest groen is:
  - niet klaar, draft, `CONFLICTING`, `UNKNOWN`, checks `PENDING`;
  - `Roborev == "open"`;
  - `MergeSHA` gelijk aan de head;
  - geen eigen PR (andere branch, fork, default branch);
  - pauze;
  - item niet in `Controller`.

  Een tegengehouden merge sluit de run niet.

  En één positieve test: een `KindMerge` met de head-SHA van de snapshot.
- `fixPR`: elke oorzaak geeft een start met de sleutel `fixpr:<pr>:<sha>`. Het plafond en de
  gebruikte SHA geven per oorzaak de juiste reden. `UNKNOWN` geeft niets.
- Een vreemde `CONFLICTING`-PR geeft `reason_pr_conflict`. Een vreemde groene PR naast een eigen
  `CONFLICTING`-PR geeft fix-pr en geen Klaar.
- `settleUnstartedFixPR` met een actieve blokkeerder geeft de reden van de oorzaak, niet `PR rood`.
- `apply` met een nep-forge: 200 geeft een merge-call met `sha` en `squash`, een Klaar en een rij
  `done`; 409 geeft geen post en geen notitie; 405 geeft een notitie en geen post; 500 geeft een
  mislukte actie.
- `gather`: de roborev-stand in deze gevallen:
  - een bot-review met en zonder marker;
  - een marker van een onvertrouwde auteur: blijft `open`;
  - een review van een andere auteur: blijft `""`;
  - `Review Passed`: blijft `""`;
  - een review zonder `Changes require fixes`: blijft `""`;
  - een repo zonder `.roborev.toml`: blijft `""`, zonder comment-call.
- `apply`: een 403 met `x-ratelimit-remaining: 0` geeft een mislukte actie, een 403 zonder dat
  teken een notitie.
- Na de merge van een `Refs`-PR start `closeCheck` `/lighthouse-close-issue N`. Dat dekken de
  bestaande tests al. Er komt een fixture bij met een gemergde PR en een gesloten rij `done`.

## Afwijkingen van het issue

- **Een groene PR die niet van de run is** krijgt Klaar zonder merge, zoals nu. Het issue zegt alleen
  "wordt niet gemerged en krijgt geen fix-pr".
- **409 (head veranderd)** is geen mislukte actie en geeft geen exitcode 1. Het issue zegt "de
  volgende tick meet opnieuw". Dat gebeurt via de vingerafdruk.
- **De #119-spec** sloot een trigger op een roborev-comment uit. Dit ontwerp draait dat om, met het
  meten in gather en de vertrouwde marker.
- **Pauze en boardstatus houden de merge tegen** (keuze van de gebruiker). Het issue noemt dat niet.
  Andere afloop, zoals Klaar en sluiten, gaat tijdens een pauze wel door.
- **Een eigen PR met `Closes #N`** krijgt geen aparte regel. Write-pr schrijft altijd `Refs`, en een
  mens die zelf een PR opent, haalt het issue uit `Controller`. Die merge houdt de tick dus al tegen.
  Opent een sessie toch een `Closes`-PR, dan merget de tick hem en sluit GitHub het issue zonder
  nacontrole. Dat risico is aanvaard.

## Aan de builder

Eigen keuzes in dit ontwerp, aan de gebruiker voorgelegd bij de overdracht:

- geen configuratieschakelaar voor het mergen;
- de headings-teksten;
- een 409 alleen loggen;
- een gelukte merge met een mislukte post laat de afsluiting aan regel 6 over;
- een prefix-match van de SHA in de marker;
- een onleesbare roborev-review kost één fix-pr-run;
- een 403 zonder rate limit gaat naar fix-pr;
- de tick leest PR-comments alleen in een repo met `.roborev.toml`.

Vrij voor de builder: de precieze namen van functies en velden, de vorm van de foutwaarden in
`forge`, en of `Roborev` een string of een eigen type is. Blijf wel bij de drie waarden.
