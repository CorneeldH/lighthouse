# Een plek die een tick vrijmaakt, telt al in die tick — ontwerp

Issue: #359

## Doel en waarom

De rem telt `in_use`, `software_in_use` en de plekken per node één keer, vóór decide. Een run die
in dezelfde tick sluit, telt dus nog mee, en een start die op zijn plek wacht, verliest een
tickinterval (nu 3 minuten). Op 2026-11-16 sloot run 207 (#47) om 17:39 en werd #285 (P0) in
dezelfde tick nog uitgesteld met `slots: 4 of 4 in use`; hij startte pas de tick erna.

Doel: een sluiting die een plek vrijmaakt, geeft die plek al in dezelfde tick aan de starts.

## Doelen

- De rem trekt bij het toetsen van de starts de plekken af die deze tick vrijkomen: van `in_use`,
  van `software_in_use` en van de plekken van de node van de sluitende run.
- De vrijgekomen plek gaat naar de eerstvolgende start in de bestaande volgorde (prioriteit,
  triggertijd, repo, issue); geen aparte toewijzing.

## Niet-doelen

- De geheugenrem. `MemAvailable` is een meting vooraf; een sluiting verandert die meting niet.
  `n` bij Geheugen blijft het aantal starts dat deze tick doorkwam.
- `lighthouse run` (`Manual`): die slaat plekken en software al over.
- De usage-rem van de triageronde.

## Anti-doelen

- Een plek die dubbel vrijkomt: een fix-pr-start met `holdsSlot` erft de plek van zijn sluitende
  run al, en die sluiting mag niet nog eens een plek geven.
- Een plek die vrijkomt voor een run die de volgende tick nog meetelt. Dan zou de rem structureel
  boven `slots` toelaten in plaats van hooguit één tick na een mislukte stop.

## Aanpakken

- **A. Vrijgave in decide (gekozen).** `Brake.apply` telt de vrijgekomen plekken uit de
  sluitingen in `ps` en trekt ze af bij het toetsen. Klein, puur, te testen in `internal/dispatch`.
  Mislukt de stop van een sessie, dan draait er één tick een run boven `slots` of `max_software`;
  de volgende tick telt beide runs, laat geen start door en regel 4 stopt de sessie alsnog.
- **B. Vrijgave in apply, pas na een geslaagde stop.** Decide merkt elke start die op een
  vrijgekomen plek leunt, apply stelt die start uit als de sluiting van zijn run mislukt. Nooit
  boven de grens, maar een koppeling start→run in `Action` en extra logica in apply.

Gekozen: A (gebruiker). Een mislukte stop is zeldzaam en herstelt zich binnen één tick; B kost
daarvoor een koppeling die decide en apply aan elkaar vastmaakt.

## Ontwerp

### Welke sluiting maakt een plek vrij

Regel: een sluiting maakt een plek vrij als de volgende tick de run niet meer telt in
`readHostFacts` (open runs plus geparkeerde runs).

| Actie | Maakt vrij? | Waarom |
|---|---|---|
| `KindClose`, uitkomst `parked` | nee | de usage-guard of de pauzevlag parkeerde de run; zijn marker (`controller: true`) houdt hem in `in_use` tot de usage-hervatting, die met `holdsSlot` op dezelfde plek komt |
| `KindClose`, handoff met `ReasonArg` `fixpr:…` (regel 13) | nee | of de fix-pr-start erft de plek (`holdsSlot`), of `settleUnstartedFixPR` laat de sluiting na de rem nog vallen; in beide gevallen komt er geen plek vrij voor een ander |
| `KindClose`, elke andere uitkomst (`done`, `handoff`, `question`, `returned`, `start_failed`, …) | ja | de run is daarna niet meer open |
| `KindReturn` met `RunID` ≠ 0 | ja | sluit de run als `returned` |
| `KindReturn` zonder `RunID` | nee | sluit geen run |
| `KindDone` | ja | sluit de run als `done` |
| `KindMerge` | nee | een merge die GitHub weigert, laat de run zonder fout open (`NoteMergeFailed`); dat is geen zeldzaamheid, dus de plek komt pas de tick na de merge vrij |

Verder, gelijk aan `CountRun`: een run op een node die niet in `Nodes` staat, telde niet in
`in_use` van die node en maakt daar niets vrij; een run met profiel `software` maakt één
`software_in_use` vrij, een andere niet; een run zonder node maakt een plek van de host vrij.

### Wat er verandert

- `planned` krijgt voor een sluiting de node en het profiel van de run (van de geschiedenisrij,
  zoals `holdsSlot` ze al leest) en een vlag die zegt of de sluiting een plek vrijmaakt. Decide
  zet die vlag volgens de tabel hierboven; de rem leest alleen de vlag.
- `Brake.apply` telt vóór de lus over de starts de vrijgekomen plekken: `freed` (host),
  `freedSoftware` en `freedOn[node]`. `closed` en `nodeClosed` toetsen
  `InUse+admitted-freed >= Slots`, `SoftwareInUse+admittedSoftware-freedSoftware >= MaxSoftware`
  en `n.InUse+admitted-freedOn[node] >= n.Slots`. De defer-tekst noemt de telling na aftrek.
- `holdsSlot` blijft zoals hij is: de start telt niet in `admitted`, en zijn sluitende run maakt
  niets vrij (tabel). Samen geven ze precies één plek.
- `comparePlanned` zet sluitingen (groep 0) al vóór starts (groep 2), dus apply stopt de sessie
  vóór de start. Geen wijziging nodig.

### Mislukte stop

Apply voert een start ook uit als de stop ervoor mislukte. De run blijft dan open en er draait één
tick een run boven de grens. De volgende tick telt beide in `in_use`, laat geen nieuwe start door
en ziet regel 4 voor de run die had moeten sluiten: die stopt hem alsnog en sluit de rij.

### `docs/tick.md`

De zin "Een run die in deze tick sluit, telt nog mee; de volgende tick ziet de plek vrij." wordt:
een run die in deze tick sluit, geeft zijn plek al aan de starts van deze tick, behalve een
geparkeerde run, een fix-pr-handoff en een merge; met wat er bij een mislukte stop gebeurt
(bovenstaande alinea).

## Tests (acceptatiecriteria)

In `internal/dispatch`, met volle plekken (fixture of `openBrake()` plus `row5`):

1. Een sluiting plus een start voor een ander issue: de start gaat door in dezelfde tick.
2. Hetzelfde voor `max_software` (sluitende softwarerun, wachtende softwarestart) en voor de
   plekken van een node (sluitende run op die node, wachtende start op die node).
3. Fix-pr-start met `holdsSlot` op een sluitende run plus een andere wachtende start: alleen de
   fix-pr-start gaat door.
4. Een sluiting zonder vrije plek maakt niets vrij: een `parked`-sluiting, een merge, en een run
   op een node die niet in `Nodes` staat.

## Aan de bouwer

- Of de vlag en node/profiel op `planned` of op `Action` staan: `planned`, tenzij de JSON van de
  acties ze nodig heeft (niet voorzien).
- Of bestaande fixtures in `testdata/decide/*brake*.json` een nieuwe uitkomst krijgen: elke
  fixture met een sluiting naast een uitgestelde start kan nu een start geven; pas de verwachting
  aan waar het nieuwe gedrag dat vraagt, en noem die fixtures in de PR.
- Een fragment in `changelog.d/`.
