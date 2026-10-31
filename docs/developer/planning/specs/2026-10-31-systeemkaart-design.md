# Systeemkaart en roadmap (K4, #102)

Uitwerking van K4 uit [het kaartenontwerp](2026-10-27-kaarten-design.md) (§4). Daar staat
wat voor alle kaarten geldt: drie niveaus, de statussen (§3), de tests (§7) en de bouw (§8).
Dit document legt vast wat de schets voor K4 besliste, en wat daarvan afwijkt.

## 1. Waarom

Lighthouse bestaat uit delen die op twee plekken draaien: in de binary en in de repo van de
gebruiker. Nergens staat in één beeld wat die delen zijn, hoe ze elkaar raken en wat er nog komt.
K4 is dat beeld. Een nieuwkomer ziet hoe lighthouse in elkaar zit. Corneel ziet wat er af is en wat
gepland staat. De kaart moet "leuk actief te houden" zijn (Corneel).

## 2. Doelen, niet-doelen, anti-doelen

**Doelen**

- Eén pagina met twee standen, *Now* en *Over time*, te wisselen met een schakelaar.
- *Now* is een plattegrond van de logische onderdelen, als schillen: wat in de binary zit en wat
  in jouw repo zit.
- *Over time* toont de issues, gegroepeerd per milestone, in de volgorde van de milestones.
- De twee standen hangen samen: elk issue hoort bij één onderdeel.
- Een nieuwe, gedeelde plattegrondweergave in `map.js` die K3 (#101) later kan hergebruiken.

**Niet-doelen**

- Geen live data uit de GitHub-API. De data is een momentopname met datum (milestone-no-go).
- Geen generator die de data afleidt. Die komt later (kaartenontwerp §9).
- Geen animatie of "film" van de groei over tijd. Die komt later (§7 hieronder).

**Anti-doelen**

- Een kaart die een onderdeel als werkend toont wat niet werkt. Status `works` bij een onderdeel
  vraagt bewijs op `main`. Bij een issue betekent `works`: gesloten als *completed* (§5)
  *(keuze na specreview, r1-f11)*.
- Milestonenummers als betekenisdrager. "M2" zegt een nieuwkomer niets (Corneel).
- Pijlen en versiering die de aandacht trekken. De lijnen tussen onderdelen zijn dun en rustig
  (Corneel: de pijlen van de schets waren "te goofy").

## 3. Wat de schets besliste

Corneel koos uit drie schetsen (plattegrond met eilanden, raster tijd × zone, stad met een weg) en
twee verhoudingen (lagen, schillen). De schetsen staan niet in de repo; de keuzes wel:

- **Schillen boven lagen.** Lagen ("binary onderop, pakketten erboven") verbergt dat de keten ook
  zonder binary in je repo draait. Schillen tonen twee plekken en hoe ze elkaar raken. Dat klopt
  met de code: de binary draagt de pakketten (`pack/pack.go`, `go:embed`) en het dashboard
  (`internal/web/web.go`, `go:embed`) in zich, en `init` zet de pakketten neer in de repo.
- **De controller en de skills-keten zijn even belangrijk.** Ze staan even groot, elk in hun eigen
  schil.
- **Een schakelaar tussen *Now* en *Over time*.** Het raster uit schets B leeft voort als *Over
  time*, maar dan als groepen per milestone in plaats van een raster.
- **Geen milestonenummers.** Een groep in *Over time* heet naar de titel van de milestone, zonder
  het voorvoegsel `M<n> — `: "Pakket", "Romp en zicht", "Dispatcher", "Tweede omgeving",
  "SURF Research Cloud als werkplek", "Product: eerste versie voor anderen", "Kaarten: lighthouse in beeld".
  De titels volgen GitHub op het moment van bouwen; milestone 5 heette eerst "SDP-tenant als
  werkplek" *(verduidelijking na planreview, r1-f4)*. Issuetitels blijven letterlijk, ook als ze
  een `M<n>` noemen: de regel geldt voor de namen van groepen en markeringen *(r1-f5)*.
- **De stad met een weg (schets C) is voor later**, als film van hoe lighthouse groeide.
- **De standen zijn gekoppeld.** Klik een onderdeel in *Now*: de lade toont de issues van dat
  onderdeel. *Over time* kan filteren op één onderdeel.

### Afwijking van de issue

De AC van #102 zegt "de milestones liggen als gebieden op de kaart" en "hun issues zijn de chips".
Na de schets liggen milestones alleen in *Over time*, als groepen. In *Now* zijn de onderdelen de
chips. Na de goedkeuring van Corneel wordt de AC van #102 hierop aangepast
(`/lighthouse-write-issue`).

## 4. De onderdelen

De namen op de site zijn Engels (kaartenontwerp §8). Tussen haakjes de Nederlandse naam van de
schets.

| id | Label | Schil | Status | Bron |
|---|---|---|---|---|
| `binary` | Binary | — (schil) | `works` | `cmd/lighthouse` |
| `installer` | Installer (`init`) | binary | `works` | `internal/initcmd` |
| `usage-meter` | Usage meter (`usage`) | binary | `works` | `internal/usage` |
| `dashboard` | Dashboard (`serve`) | binary | `works`, not in first release | `internal/server` |
| `controller` | Controller (`tick`) | binary | `planned`, milestone "Dispatcher", not in first release | `internal/cli/tick.go` |
| `packs` | Packs (pakketten, meegedragen) | binary | `works` | `pack/` |
| `repo` | Your repo | — (schil) | `works` | — |
| `chain` | Skills chain (skills-keten) | repo | `works` | `pack/core/skills` |
| `guardrails` | Guardrails (vangrails) | repo | `works` | `pack/core/hooks` |
| `environments` | Environments | — (rij) | `works` | — |
| `env-now` | Environment today | environments | `works`, not in first release | `deploy/` |
| `env-second` | Second environment | environments | `planned`, milestone "Tweede omgeving" | — |
| `env-sdp` | Research Cloud workspace | environments | `planned`, milestone "SURF Research Cloud als werkplek" | — |
| `docs` | Docs and site | — (los) | `works` | `docs/` |

- Een onderdeel zonder `works` tekent grijs gearceerd, zoals in de schets.
- De controller is `planned`, niet `works`. `tick` bestaat op `main` als skelet: het controleert
  de repo's in `[dispatch]`, maar haalt nog geen issues op en start nog geen sessie
  ([`docs/tick.md`](../../../tick.md), "Tick haalt nog geen issues op"; #112, #114). De
  `what` van de controller zegt dat in gewone taal (anti-doel §2, r1-f1).
- Een onderdeel heeft één `source`. Bij twee kandidaten noemt de tabel het ingangspunt
  *(keuze na specreview, r1-f10)*.
- Een schil (`binary`, `repo`) is een onderdeel met eigen issues én een vak met onderdelen
  erin. Op de kaart is een schil daarom een container met een eigen kopknop; de onderdelen zijn
  broers van die knop, niet zijn kinderen. Geen knop in een knop *(keuze na specreview, r1-f6)*.
- Een schil toont in de lade en in het filter van *Over time* ook de issues van de onderdelen erin
  *(keuze na specreview, r1-f9)*.
- `docs` is een keuze van Claude. Product en Kaarten hebben issues die in geen schil passen
  (startgids, website, kaarten). Zonder dit onderdeel zouden ze nergens horen.
- De omgevingen zijn een rij onder de twee schillen: een groepsonderdeel `environments` zonder
  eigen chip, zodat elke `parent` een bestaand onderdeel noemt (keuze van Claude). Het heeft ook
  geen knop, dus er hangt nooit een issue aan `environments` (r1-f6).

Twee lijnen, dun en met een kort label:

- `installer` → `repo`: "installs" (de lijn eindigt aan de schil, keuze van Claude);
- `controller` → `chain`: "starts sessions".

Een lijn met een `planned` uiteinde tekent als planned: grijs en gestreept. Zo toont "starts
sessions" wat komt, niet wat werkt *(keuze na specreview, r1-f1)*.

## 5. De data

Eén bestand, `docs/site/data/system.json`. Het gebruikt **geen** sleutel `chips`. Zo pikken de
stroomtests in `tests/test_site_chain.py` (`data_maps()` zoekt een dict met `chips`) het niet op,
want die eisen fases, rijen en routes. Het plattegrondformaat is ook bedoeld voor K3.

```json
{
  "title": "The system",
  "subtitle": "…",
  "snapshot": "2026-10-31",
  "parts": [
    {"id": "controller", "label": "Controller", "parent": "binary",
     "what": "…", "when": "…", "without": "…",
     "status": "planned", "milestone": 3, "not_in_first_release": true,
     "source": "https://github.com/CorneeldH/lighthouse/blob/main/internal/cli/tick.go"}
  ],
  "links": [{"from": "controller", "to": "chain", "label": "starts sessions"}],
  "milestones": [{"number": 3, "title": "Dispatcher", "state": "open"}],
  "issues": [{"issue": 113, "title": "…", "milestone": 3, "part": "controller",
              "status": "planned"}]
}
```

- `milestones` staan in de volgorde van tonen. `title` is de titel zonder `M<n> — ` en zonder
  het achtervoegsel ` - DONE`.
- Een `milestone` is overal het milestonenummer, ook bij een onderdeel. De plattegrond toont bij
  een planned onderdeel de titel van die milestone, nooit `M<n>`. `plannedMark` in `map.js` blijft
  voor de ketenkaart zoals hij is; de plattegrond krijgt zijn eigen markering (keuze van Claude).
- Een issue is `works` als het gesloten is met `state_reason: completed`, met of zonder PR: de
  overkoepelende issues van de afgeronde milestones sloten zonder PR. Open is `planned`. Gesloten
  als *not planned* staat er niet in (keuze van Claude).
- De telling per groep (`3/17`) komt uit de issues in de data, niet uit de tellers van GitHub: die
  tellen ook PR's mee.
- **Welk onderdeel**: een issue hoort bij het onderdeel waar zijn hoofdwijziging landt. Een
  ontwerp- of overkoepelend issue hoort bij het onderdeel dat het ontwerpt. Een issue dat
  meerdere onderdelen in één schil raakt, hangt aan die schil (`binary` of `repo`). Afspraken
  voor anderen (release, templates, startgids) horen bij `docs`. De bouwer zet de toewijzing als
  tabel in de PR-body, zodat Corneel hem in de review ziet (keuze van Claude).
- Issues zonder milestone staan er niet in (keuze van Claude: de roadmap is wat in een milestone
  zit).
- M5 bestaat niet op GitHub en staat er dus niet in (keuze van Claude).
- `source` volgt kaartenontwerp §8: een GitHub-URL naar `main`.
- Je schrijft de data met de hand uit `gh api` op het moment van bouwen, en zet de datum in
  `snapshot`.

## 6. De bouw

- **Pagina**: `docs/site/system.html` en `docs/site/system.js`, naar het patroon van
  `chain.html` en `chain.js`. De tegel van #102 in `index.html` linkt ernaar en gaat over naar
  de werkende vorm van de tegel van de ketenkaart. `test_index_lists_the_six_maps` eist dan
  `system.html` en verwacht `#102` niet meer *(keuze na specreview, r1-f4)*.
- **Weergave *Now***: een nieuwe `renderPlanMap(root, data, detail)` in `map.js`. De onderdelen
  zijn geneste HTML-vakken in een CSS-grid. Een `parent` nest een vak in zijn schil. Een onderdeel
  zonder kinderen is een knop, met de vorm van de chips van de ketenkaart. Een schil (`binary`,
  `repo`) is een container zonder klik, met een kopknop die naast de knoppen van zijn onderdelen
  staat, niet eromheen. `environments` is een container zonder knop (§4, r1-f6, r2-f1). De lijnen staan in een SVG-laag erboven,
  zoals de routes in `renderFlowMap`. Anders dan daar meet de plattegrond de vakken: de lijnen
  worden opnieuw berekend uit een `ResizeObserver` op de *Now*-container en telkens als *Now*
  zichtbaar wordt; terwijl *Now* verborgen is, meet hij niet *(keuze na specreview, r1-f7)*. Op
  een smal scherm vouwt het grid tot één kolom.
- **Eigen klassen.** De plattegrond gebruikt niet `.map` en `.chip` van de stroomkaart: `.map`
  verbergt planned zonder roadmapschakelaar (`map.css`, "Planned and idea items show only with
  the roadmap switch on"), `.chip` staat absoluut en `.map` heeft een `min-width` van 1260px. Beide
  standen krijgen een eigen wortel- en onderdeelklasse (bijvoorbeeld `.plan`, `.plan-part`) en
  delen alleen de kleurtokens en de `kind-*`- en `status-*`-kleuren
  *(keuze na specreview, r1-f5)*.
- **Weergave *Over time***: een tweede functie in `map.js` over dezelfde data. Eén groep per
  milestone, met de titel, de telling (`3/17`) en de issues als chips. Een filter op onderdeel
  dimt de rest. Een milestone zonder issues ("SURF Research Cloud als werkplek") toont een grijs vak met
  "no issues yet".
- **De lade**: dezelfde lade als op de ketenkaart. Bij een onderdeel: wat, wanneer, zonder, status,
  bron en de lijst van zijn issues. Bij een issue: titel, status, milestone en een link naar het
  issue op GitHub. Openen, sluiten, de markering van het huidige vak en Escape zitten nu als
  closures in `renderFlowMap`; die gaan naar één gedeelde helper in `map.js`, die de stroomkaart en
  beide standen gebruiken *(keuze na specreview, r1-f8)*.
- **De datum**: de pagina toont "Snapshot of <datum>" uit `snapshot`.
- **De bestaande roadmapschakelaar** ("Show roadmap", #145) hoort bij de stroomkaart en komt niet
  op deze pagina. Hier zijn `planned` en grijs juist het punt.
- Geen `innerHTML` en verwante sinks (`test_component_never_writes_html_from_data`), en
  `<html lang="en">`.

## 7. Tests

Een nieuw bestand, `tests/test_site_system.py`, in het patroon van `tests/test_site_chain.py`
(fixture-gevallen in `tests/fixtures/site/`). Elke test moet falen als je de productie breekt.

1. **Planned heeft een anker** (AC van #102). Een onderdeel of issue met `status: planned` heeft
   een `issue` of `milestone`. Een fixture met een planned onderdeel zonder beide wordt rood.
2. **Elke verwijzing bestaat.** Elke `parent`, elke `part` van een issue, elke `from` en `to` van
   een lijn en elke `milestone` van een issue of onderdeel noemt iets wat in de data staat. Elk
   issue heeft een `part` en een `milestone`, en geen issue hangt aan `environments`.
3. **Uniek en zonder kringen.** `parts.id`, `milestones.number` en `issues.issue` zijn uniek, en de
   `parent`-ketens bevatten geen kring. Fixtures met een dubbel id, een dubbel issue en een kring
   worden rood (r1-f3).
4. **Vaste woorden.** `status` is `works`, `planned` of `idea`; een issue is nooit `idea`.
5. **Bronnen bestaan.** Elke `source` onder de GitHub-URL wijst naar een pad dat bestaat (zoals
   `source_problems`).
6. **Datum.** `snapshot` is een ISO-datum en `system.js` leest het veld.

De linktest in `tests/test_site_pages.py` dekt de nieuwe pagina vanzelf.

## 8. Later

- **De film** (schets C): de stad met een weg langs de milestones, die groeit over tijd.
- **Een generator** die `issues` en `milestones` uit GitHub haalt (kaartenontwerp §9).
- **K3 (#101)** gebruikt `renderPlanMap` voor de plattegrond van je repo na `init`.

## 9. Voor de bouwer

Aan de bouwer zijn: de precieze verdeling van het grid, de kleuren binnen de bestaande stijl van
`map.css`, en de teksten `what`, `when` en `without` per onderdeel, in het Engels en in gewone
taal.
