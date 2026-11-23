# Prioriteit per milestone, met omhoog en omlaag in de UI

## Waarom

De weergave *Milestones* in de Prioritering-tab zet milestones in een **berekende** volgorde:
wat het eerst af kan (nu `sortMilestoneRows` in `web/src/lib/priorities/derive.ts`, straks de
vier groepen van #446). Die volgorde zegt wat dichtbij is, niet wat het belangrijkst is. De
gebruiker wil naast de berekende volgorde een **gekozen** volgorde: zelf bepalen welke milestone
bovenaan staat, en die keuze direct in de UI aanpassen.

Dit is stap 1: de volgorde in de UI. Of de dispatcher later werk kiest op deze prioriteit, is
een volgende stap en valt buiten dit ontwerp. De opslag is wel zo gekozen dat die stap kan
zonder migratie.

## Doelen

- Elke milestone kan een **rang binnen zijn eigen repo** hebben: 1 is het belangrijkst.
- De weergave *Milestones* krijgt een switch: **Berekend** (de volgorde van #446) of
  **Prioriteit**. De keuze blijft bewaard zoals de weergave nu (`prefs.ts`).
- In de stand *Prioriteit* heeft elke milestone een knop **omhoog** en **omlaag**. Een klik
  verandert de rang op de forge en de lijst toont de nieuwe volgorde.
- De rang staat op de forge, dus hij geldt op elke machine en is zichtbaar op GitHub.

## Geen doelen

- Een **totaalscore** over repo's heen. Die kan later als optionele override; het repo-rangnummer
  blijft dan bestaan.
- De dispatcher laten kiezen op milestone-prioriteit.
- Slepen. Omhoog en omlaag zijn genoeg voor het aantal milestones per repo (nu 11 in deze repo).
- Een rang weghalen vanuit de UI. Dat gaat met de hand, door de regel uit de beschrijving te
  halen.
- Inloggen of rechten in `lighthouse serve`.

## Anti-doelen

- De beschrijving van een milestone (de pitch) mag nooit beschadigd raken. Een schrijfactie
  verandert alleen de prioriteitsregel; elke andere regel blijft byte voor byte gelijk.
- Een stille overschrijving van een wijziging die iemand anders net deed. Klopt de huidige rang
  niet meer met wat de UI dacht, dan weigert de server en ververst de UI.
- Een webpagina in de browser van de gebruiker die ongevraagd milestones aanpast.

## Randvoorwaarden

- `lighthouse serve` luistert standaard op `127.0.0.1:8127`; `hostGuard`
  (`internal/server/host.go`) laat ook `*.ts.net` toe (tailscale serve). Er is geen login.
- De server heeft tot nu toe alleen `GET`-endpoints. Dit wordt het eerste dat schrijft.
- De prioriteitsdata komt uit een `Refresher` (`internal/server/refresher.go`) die elke 300 s
  bouwt (`PrioritiesInterval`). Na een schrijfactie mag de lijst niet 5 minuten terugspringen.
- Elke repo heeft een `priorities.Source` met een `forge.Forge`; die heeft al
  `EditMilestone(ctx, number, title, description *string)`, op GitHub en GitLab.
- Op GitLab kan een groepsmilestone niet via de project-API worden aangepast
  (`*forge.GroupMilestone`).
- Een milestone die niet geshaped is, herkent de UI aan een regel `Status: shaping` in de
  beschrijving (`isShaping` in `derive.ts`). De prioriteit volgt dat patroon.

## Aanpakken

**A. Een regel in de beschrijving van de milestone op de forge (gekozen).** De server schrijft
`Priority: N` in de beschrijving. De forge blijft de waarheid, de rang is overal zichtbaar, en de
dispatcher kan hem later lezen. Prijs: een omhoog of omlaag kost twee API-calls (twee milestones
wisselen), de server moet de beschrijving lezen en terugschrijven, en de cache moet na het
schrijven direct verversen.

**B. Lokaal in SQLite (`--db`).** Snel en zonder schrijfrechten op de forge, maar de rang bestaat
dan op één machine en de dispatcher op een andere machine kent hem niet. Afgevallen: dat werkt
tegen de volgende stap.

**C. In `lighthouse.toml` van de repo.** Afgevallen: een schrijfactie vanuit de UI wordt een
commit, en elke wijziging gaat via een branch en een PR.

De gebruiker koos A.

## Ontwerp

### De regel in de beschrijving

- Vorm: een eigen regel `Priority: N`, met `N` een geheel getal vanaf 1. *(Eigen keuze: Engels
  zoals `Status: shaping`, omdat de server ook in repo's met een andere taal schrijft.)*
- **De prioriteitsregel** is alleen een regel die, na weglaten van spaties aan begin en eind en
  hoofdletterongevoelig, past op `priority:\s*[1-9][0-9]*`. Elke andere regel, ook
  `Priority: high` of `Priority: 0`, is gewone tekst van de pitch: hij telt niet als rang en een
  schrijfactie raakt hem nooit aan. *(Eigen keuze, zodat het anti-doel "de pitch blijft heel"
  houdt.)*
- Een beschrijving zonder prioriteitsregel betekent: geen rang.
- Staat de prioriteitsregel er meer dan één keer, dan telt de eerste. *(Eigen keuze.)*
- Schrijven vervangt de eerste prioriteitsregel op zijn plek. Zonder regel komt
  `\n\nPriority: N` achter de beschrijving (een lege beschrijving wordt alleen `Priority: N`).
  Alle andere tekst blijft ongewijzigd, ook de regeleinden.
- Eén Go-functie leest en schrijft de regel. De UI leest dezelfde regel uit `description`, die
  `GET /api/priorities` al per milestone teruggeeft. De testgevallen van de Go-functie en de
  TypeScript-functie zijn gelijk, of spiegelen elkaar.

### De volgorde in de stand *Prioriteit*

1. Per repo krijgt elke milestone met een rang zijn **plaats in de repo**: op rang, bij gelijke
   rang in de berekende volgorde.
2. Over repo's heen: eerst alle plaats 1 uit alle repo's, dan alle plaats 2, enzovoort. Binnen
   dezelfde plaats in de berekende volgorde.
3. Milestones zonder rang staan daaronder, in de berekende volgorde.

De **berekende volgorde** is wat `sortMilestoneRows` op dat moment doet (na #446 de vier
groepen). Deze volgorde wacht niet op #446.

De repo-filter (de chips in de toolbar) werkt zoals nu: een verborgen repo verdwijnt uit de lijst.

### Omhoog en omlaag

De knoppen staan alleen in de stand *Prioriteit*. Ze werken **binnen de repo** van de milestone,
ook in de lijst waar repo's door elkaar staan. Een milestone kan daardoor visueel meer dan één
plek verspringen.

- **Omhoog** op een milestone met rang: hij wisselt van rang met de milestone direct boven hem in
  zijn repo. Op plaats 1 is de knop uit.
- **Omlaag** op een milestone met rang: hij wisselt met de milestone direct onder hem. De laatste
  milestone met rang in zijn repo heeft geen knop omlaag.
- **Omhoog** op een milestone zonder rang: hij krijgt de hoogste rang in zijn repo plus 1 en komt
  onder de laatste milestone met rang. **Omlaag** is uit.
- Gelijke rangen of gaten (`1, 1, 4`): eerst nummert de UI de milestones met rang in die repo
  opnieuw door tot `1..k`, in hun huidige volgorde, en dan pas wisselt hij. Die eerste klik
  schrijft dus meer dan twee milestones; daarna is het weer een wissel.

De UI berekent welke milestones een nieuwe rang krijgen. De server berekent geen volgorde: hij
krijgt de lijst van wijzigingen en voert die uit.

### Het schrijf-endpoint

`POST /api/priorities/milestone-ranks`, body:

```json
{ "repo": "CorneeldH/lighthouse",
  "changes": [ { "number": 6, "from": 2, "to": 1 }, { "number": 4, "from": 1, "to": 2 } ] }
```

`from` is `null` voor een milestone zonder rang. Per verzoek één repo.

De server, per verzoek:

1. Controleert de herkomst (zie *Beveiliging*) en dat de repo een van zijn bronnen is (anders 404).
2. Leest elke genoemde milestone vers (`MilestoneByNumber`) en controleert dat de huidige rang
   gelijk is aan `from`. Klopt één niet: **409**, er wordt niets geschreven.
3. Schrijft de wijzigingen een voor een met `EditMilestone`, alleen de beschrijving. Na elke
   geslaagde schrijfactie werkt hij de cache bij met de beschrijving die de forge teruggeeft
   (zie *Cache*).
4. Antwoordt met 200 en de nummers die geschreven zijn.

Faalt een schrijfactie halverwege, dan antwoordt de server met de fout en de nummers die al wel
zijn geschreven. Omdat de cache per geslaagde schrijfactie is bijgewerkt, toont een verversing
precies wat er op de forge staat. Er is geen rollback: de rang op de forge is op dat moment nog
steeds geldig, alleen mogelijk dubbel, en de volgende klik nummert opnieuw door (zie hierboven).

Foutcodes: herkomst fout 403, onbekende repo 404, rang verouderd 409, en elke forge-fout
(ook een groepsmilestone op GitLab) zoals `forgeError` die nu vertaalt (502/503). Groeps-
milestones staan niet in de lijst (`internal/forge/gitlab_planning.go` leest alleen
projectmilestones), dus dat pad komt vanuit de UI niet voor. *(r1-f6.)*

### Cache

De server bouwt na een schrijfactie **niet** opnieuw: hij past de beschrijving van de geschreven
milestone aan in de gecachte waarde van de `Refresher`. *(Eigen keuze.)* Een volledige bouw leest
elke repo achter elkaar, op GitLab met een call per open issue, en zou elke klik seconden laten
wachten. Bovendien geeft `Refresher.Latest` de fout van de laatste bouw terug, dus een mislukte
bouw na een geslaagde schrijfactie zou de hele tab een fout laten tonen. De `Refresher` krijgt
daarom een methode die de huidige waarde onder zijn lock aanpast. Heeft de laatste bouw een fout,
dan past hij niets aan. De gewone bouw van elke 300 s blijft de waarheid en overschrijft de
aanpassing.

Een bouw die al liep toen de aanpassing kwam, las de milestones van vóór de schrijfactie. De
`Refresher` mag zo'n resultaat niet opslaan: hij houdt een generatieteller bij, die de
aanpas-methode ophoogt, en `refresh` slaat een bouwresultaat alleen op als de teller sinds de
start van die bouw gelijk is gebleven (gecontroleerd onder de lock). Een weggegooide bouw
valt terug op de volgende ronde. Test: een bouw die over een aanpassing heen loopt laat de
aangepaste waarde staan. *(r1-f2.)*

Bij een 409 heeft de server in stap 2 de actuele beschrijvingen gelezen. Hij past daarmee de
cache bij (zelfde aanpas-methode) voordat hij 409 antwoordt, zodat de verversing van de UI de
actuele rangen toont en de volgende klik slaagt zonder op een bouw te wachten. Test: een klik
na een 409 slaagt. *(r1-f3.)*

De server heeft per bron-slug een forge en de aanpas-methode nodig. Dat verbreedt `server.Deps`
(naast `Priorities`) en `startPriorities`/`startRefresher` in `internal/cli/serve.go`; de plan
benoemt die aanpassing. *(r1-f4.)*
De UI werkt **optimistisch**: na een klik staat de nieuwe volgorde direct op het scherm. Komt er
een fout terug, dan vervangt de UI die volgorde door verse data en toont hij de melding. Tijdens
een lopend verzoek staan de knoppen van die repo uit, zodat twee klikken niet tegelijk dezelfde
rangen wisselen.

### Beveiliging

Dit is het eerste endpoint dat iets op de forge verandert, met het token van de server.

- Het schrijf-endpoint eist een header `Origin` waarvan host en poort gelijk zijn aan de header
  `Host` van het verzoek. Het schema telt niet mee. Zonder `Origin` of met een andere: 403. Dat
  houdt een pagina van een andere site in de browser van de gebruiker tegen (CSRF). *(Eigen keuze.)*
  - Achter `tailscale serve` komt het verzoek als HTTP binnen, terwijl de browser
    `Origin: https://<naam>.ts.net` stuurt. Daarom telt het schema niet mee. `tailscale serve`
    geeft de `Host` ongewijzigd door, dus host en poort komen overeen.
  - De Vite-devserver staat nu op `changeOrigin: true` (`web/vite.config.ts:22`), waardoor de
    proxy de `Host` herschrijft naar `127.0.0.1:8127` terwijl de browser `Origin:
    http://localhost:5173` stuurt: elke POST zou 403 geven. Deze stap zet `changeOrigin` daarom
    op `false`. Dan blijft de `Host` `localhost:5173`, gelijk aan de `Origin`; `hostGuard` laat
    `localhost` al toe. *(Eigen keuze na spec-review, r1-f1.)*
  - De vergelijking is letterlijk: de `host[:poort]`-tekst van de `Origin` tegen de header
    `Host`, zonder standaardpoorten (80, 443) in te vullen. `https://x.ts.net` en `x.ts.net`
    zijn dus gelijk. Tests: tailscale (zonder poort) en localhost met poort.
    *(Eigen keuze na spec-review, r1-f5.)*
- De controle geldt voor de schrijf-endpoints van de browser-API, niet voor `/api/node/`. De
  node-API krijgt POSTs van de Go-client van de controller, zonder `Origin`, en blijft zoals hij
  is.
- De body moet `application/json` zijn.
- Iedereen die de server nu kan lezen (localhost, en via tailscale het tailnet) kan ook de
  volgorde aanpassen. Dat is een bewuste keuze voor deze stap. Inloggen is geen doel.

### UI

- In de toolbar van *Milestones*: een switch **Berekend / Prioriteit**, bewaard in `prefs.ts`.
- In de stand *Prioriteit*: per rij een knop omhoog en omlaag, en de plaats in de repo als klein
  getal bij de titel. Milestones zonder rang tonen geen getal.
- Een melding bij een fout, met de tekst van de server. Bij 409: "De volgorde is net ergens anders
  veranderd; de lijst is ververst."

## Wat aan de bouwer is

- De precieze vorm van de aanpas-methode op de `Refresher`.
- De precieze vorm van het optimistische bijwerken in de query-cache (`web/src/lib/queries.ts`).
- De plek en het uiterlijk van de switch en de knoppen, binnen de bestaande toolbar en rij.
- Of de controle op `Origin` per schrijf-endpoint zit of als middleware die `/api/node/`
  overslaat.
