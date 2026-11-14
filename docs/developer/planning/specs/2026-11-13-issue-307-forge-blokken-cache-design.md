# Issue #307 — Open questions en Returned uit één verversend overzicht

Issue: [#307](https://github.com/CorneeldH/lighthouse/issues/307). Vervolg op de keuze
*Twee forge-rondes per verversing* in
[de spec van #69](2026-10-27-ui-m1-dod-design.md), die een cache doorschoof "als de meting dat
vraagt".

## Waarom

`/api/questions` en `/api/returned` doen er op de Beelink 17–20 s over. Beide roepen per request
`overview.BuildAll` aan, en die leest per repo live van GitHub: de open issues, de board-items en
de comments van elk Controller-issue. De twee endpoints doen hetzelfde werk elk apart. De pagina
moet bij een herlaad direct antwoorden, zonder dat een nieuwe vraag langer dan één tick-interval
(3 minuten, #306) onzichtbaar blijft.

## Doelen

- Beide endpoints antwoorden binnen 1 s, met dezelfde inhoud als nu.
- Eén verversing leest GitHub één keer voor beide blokken.
- Een nieuwe vraag of teruggave staat binnen 4 minuten na de comment op de pagina (2 minuten
  interval + ca. 20 s build + 1 minuut paginapoll, met marge) *(own choice after spec review, r1-f1)*.
- Faalt het lezen van de forge, dan toont de pagina een fout (502/503), geen lege lijst.

## Niet-doelen

- Geen wijziging aan de tick, `dispatch.Gather` of `tick_state`; geen migratie.
- Geen wijziging aan de vorm van de responses: `web/openapi.json` en de gegenereerde client
  blijven gelijk.
- `/api/runs`, `/api/cleanup` en `internal/server/latest.go` blijven zoals ze zijn.

## Anti-doelen

- Een lege lijst terwijl de forge onbereikbaar is: dat verbergt vragen.
- Een snelle pagina die oude data toont zonder dat iemand dat kan zien.

## Aanpakken

| Aanpak | Afweging | Besluit |
|---|---|---|
| **A.** De tick schrijft questions en returned naar `tick_state`; serve leest SQLite. | Altijd snel, ook na een herstart. Maar de tick leest dit niet al: comments alleen als de precheck *due* zegt, alleen Controller/On hold/open-run-items, en zonder onvertrouwde auteurs. Kost dus een volle `BuildAll` per tick, een migratie (nieuwe `kind`), een eigen fout- en versheidsmarkering, en serve zonder tick toont niets. | Afgevallen |
| **B.** Serve houdt één overzicht in het geheugen en ververst het op de achtergrond. | Eén `BuildAll` voedt beide endpoints; requests lezen het geheugen. Geen migratie, geen tickwijziging, werkt zonder tick. Kost: serve leest GitHub op een vast interval, ook zonder kijkers. | **Gekozen** door de gebruiker |
| **C.** Gecombineerd endpoint met een korte TTL-cache, alleen verversen op request. | Klein, maar de eerste herlaad na het verlopen van de TTL duurt weer ~17 s: haalt het eerste doel niet. | Afgevallen |

## Ontwerp

**Overzicht in het geheugen.** Een type in `internal/server` (naam aan de bouwer) houdt het laatste
resultaat van `overview.BuildAll` vast: het `Overview`, de fout van de laatste verversing en het
tijdstip. De handlers van `/api/questions` en `/api/returned` lezen alleen dat resultaat.

**Verversen.** Een goroutine, gestart bij `serve` en gestopt met zijn context, bouwt direct bij de
start en daarna elke **2 minuten**, gemeten van start tot start (een ticker, geen slaap na de build).
Trager dan het interval? Dan volgt de volgende build direct, nooit twee tegelijk. De goroutine
wordt in `serve.go` gestart met de context van serve en geeft het overzicht via een leesfunctie
in `Deps` door, zoals `Runs`/`Leftovers`; `server.New` zelf start niets, zodat de tests geen
goroutine lekken *(own choice after spec review, r1-f2)*.

**Vóór de eerste build.** Een request dat binnenkomt voordat de eerste build klaar is, wacht op
die build (of op zijn eigen context). Alleen die eerste keer na een start is traag.

**Fouten.** Faalde de laatste verversing, dan geven beide endpoints de fout zoals nu:
`forge.ErrNotConfigured` → 503, anders 502. Ook als er een ouder, wel geslaagd overzicht is: dat
houdt het anti-doel "oude data zonder dat iemand het ziet" buiten. Een mislukte build wordt na 20 s opnieuw geprobeerd in plaats van na 2 minuten, zodat één
storing van GitHub niet twee minuten blijft hangen *(own choice after spec review, r1-f4)*. Zonder repo's blijft het 503,
zonder goroutine die GitHub leest.

**Pagina.** `useQuestions` en `useReturned` pollen elke **1 minuut** in plaats van 5 (`refetchInterval` en `staleTime`); dat is nu
goedkoop. Daarvoor komt een eigen constante naast `FORGE_INTERVAL_MS`; `useCleanup` blijft op
`FORGE_INTERVAL_MS` *(own choice after spec review, r1-f7)*. Samen met de 2 minuten van serve en de buildduur staat een nieuwe vraag
uiterlijk na ca. 3 minuten 20 op de pagina.

**Documentatie.** `docs/install.md` (rond regel 157 en 306) beschrijft dat de twee endpoints een
overzicht tonen dat serve elke 2 minuten ververst, en wat er bij een fout gebeurt. Een fragment in
`changelog.d/`.

## Mijn keuzes (te controleren)

- **Altijd verversen, ook zonder kijkers.** Pauzeren als niemand kijkt maakt de eerste herlaad
  daarna weer 17 s en breekt het eerste doel. Twee repo's elke 2 minuten (30 builds per uur) blijft
  binnen de rate limit van GitHub zolang de repo's het Controller-filter van het bord hebben (alleen
  gelabelde issues worden gelezen); een repo zonder bord leest de comments van elk open issue met
  comments en telt daarom mee in de plan-fase (aantal calls per build × 30 tegen 5000 per uur, samen
  met de tick) *(own choice after spec review, r1-f5)*.
- **2 minuten serve + 1 minuut pagina** in plaats van 3 + 5: zo blijft de som, buildduur
  inbegrepen, onder 4 minuten (zie *Doelen*). Vaste waarden, geen configuratie.
- **Fout gaat vóór oude data**, zie *Fouten*.

## Aan de bouwer

- Naam en plaats van het type, en of de ververslogica in `internal/server` of `internal/overview`
  woont.
- Een time-out per build, zodat een hangende GitHub-call de volgende verversing niet blokkeert:
  langer dan een normale build (ruim boven 20 s, de forge-client heeft zelf 30 s per call) en korter
  dan het interval van 2 minuten, bijvoorbeeld 90 s *(own choice after spec review, r1-f3)*.
- Tests: een tellende stub-`Source` (er bestaan al `stubSource` in
  `internal/server/server_test.go` en `fakeSource` in `internal/server/forge_test.go`) bewijst dat twee requests samen één build kosten; een stub die
  na een geslaagde build faalt, bewijst 502 in plaats van de oude lijst; het interval wordt in de
  test injecteerbaar of handmatig getriggerd, zonder `time.Sleep` van minuten.
- De meting van het eerste doel (`curl -w '%{time_total}'` op de Beelink) is handwerk na de
  deploy; de tests bewijzen alleen dat de handler geen forge-call doet.
