# Contract per onderdeel als eigen milestone

Issue: [#499](https://github.com/CorneeldH/lighthouse/issues/499), ontwerpvraag 3 uit de triage.
Datum: 2026-11-25.

## Besluit

Ja: *Contract per onderdeel* wordt een eigen milestone. Die begint **geparkeerd** en start pas
als de flinch-proef uit #499 (ontwerpvraag 1) klaar is. Er is geen haast: het werk is groot, en de
uitkomst kan ook "opgeven" zijn.

## Intent en waarom

Software die uit haar contract opnieuw te genereren is, is toekomstbestendig. Die toekomst kent drie
vormen, en ze hangen samen:

- een beter model genereert hetzelfde onderdeel opnieuw, en beter;
- een onderdeel dat vervallen is, wordt vervangen in plaats van gerepareerd;
- het onderdeel overleeft een wissel van taal of platform.

Werkt het contract, dan zijn alle drie haalbaar. Lighthouse doet nu een lichte versie van hetzelfde:
spec, plan, review en checks leggen vast wat een wijziging moet doen. Het Contract is de expliciete
en blijvende versie daarvan. De milestone beantwoordt één vraag: levert werken vanuit een contract
iets op wat de huidige keten niet levert?

Het past bij `uitgangspunt A`: de mens bepaalt de intent (het contract), de keten doet de rest. En
bij de diagnose in de [strategie](../../strategie.md): intent helder maken is een van de grenzen van
werken met agents.

## Doelen

- Op één Go-onderdeel van lighthouse een **rondgang** maken: contract extraheren uit de code, het
  contract beoordelen en bijstellen, de code opnieuw genereren uit alleen het contract, en de
  uitkomst vergelijken met de bestaande code.
- Eén echte wijziging twee keer doen: via het contract, en via de huidige keten. De uitkomsten
  vergelijken.
- Eindigen met een besluit met bewijs: doorgaan, aanpassen of opgeven.

## Niet-doelen

- Contracten als levende documentatie. Contract en docs blijven voorlopig gescheiden; #622 loopt
  los hiervan.
- Integratie in `pack/go` of `pack/core`, of een taalneutraal contractformaat. Dat komt pas na een
  positief besluit, als vervolg.
- Meer dan één onderdeel van lighthouse een contract geven.
- De ideeën van qualm en exhale (#499, ontwerpvraag 4).

## Anti-doelen

Het experiment is mislukt, ook als alles technisch werkt, wanneer:

- er machinerie in een pack terechtkomt vóór het besluit (`uitgangspunt G`);
- de vergelijking alleen telt wat groen werd, en niet wat het kostte aan tokens en menswerk;
- de drempel voor "het werkt" pas na de meting wordt gekozen.

## Randvoorwaarden

- `uitgangspunt G`: weinig onderdelen die bewezen iets toevoegen. Er is geen gepubliceerd resultaat
  dat regeneratie betere code geeft; dit experiment levert dat bewijs zelf, of niet.
- `uitgangspunt C`: een meting met een eerlijk label. "Kon niet vergelijken" is een uitkomst.
- [packs](../../areas/packs.md): gereedschap gepind en met getoetste licentie, geen eigen parser.
  flinch is MIT en Go.
- `strategie`: lighthouse levert het vermogen, elke repo past het toe. Een vervolg in de packs moet
  dus taalneutraal kunnen, maar dat is een vraag voor na het besluit. De milestone raakt geen punt
  onder *Bewust niet*.

## Aanpakken

1. **Rondgang op één onderdeel** (gekozen). Contract extraheren uit bestaande code, beoordelen,
   regenereren zonder de oude code, vergelijken; daarna één wijziging via het contract tegenover
   de keten. Weinig nieuwe machinerie. Het test alle drie de toekomsten: regeneratie (beter model),
   vervanging (vervallen code) en een contract dat los van de code bestaat (andere taal).
2. **Eerst nieuw werk.** Een nieuw onderdeel twee keer bouwen: contract-first en via de keten. Een
   zuivere A/B, maar dubbel werk, afhankelijk van een geschikt nieuw onderdeel, en het test geen
   extractie of vervallen code.
3. **Breed uitrollen.** Alle Go-packages van lighthouse een contract met flinch-dekking, regeneratie
   later. Veel machinerie vóór het bewijs; botst met `uitgangspunt G`.

Gekozen: 1. Het dekt de drie toekomsten met de minste machinerie, en extractie maakt het contract ook
haalbaar voor code die al bestaat.

## Ontwerp van de milestone

De stappen van de rondgang, elk een issue:

1. **Keuze en nulmeting.** Kies één begrensd Go-package van lighthouse. Meet vooraf: de tests, de
   mutatiescore en de kill matrix met flinch. Leg vóór de rest de drempels vast waarop het besluit
   in stap 6 rust.
2. **Contract extraheren.** Een agent haalt het contract uit de code, in het formaat van flinch
   (`contract/<x>/README.md`, verplichtingen `**A1**`, tests met `// Contract: x/A1`). Een mens
   beoordeelt en stelt bij. `flinch contract` toont dat elke verplichting een test heeft.
3. **Log van afgewezen aanpakken.** Naast het contract een korte log: wat geprobeerd is, waarom het
   faalde, en de scope (paden). Bronnen zijn git-historie, issues en reviews van het package. Dit is
   wat volgens Salkini het contract niet kan dragen.
4. **Regenereren.** In een schone worktree, met alleen het contract, de log en de tests als
   invoer, zonder de oude code (zoals `mix covenant.regen` dat doet).
5. **Vergelijken.** Regeneratie tegenover de bestaande code: tests, mutatiescore en kill matrix,
   terugkeer van een afgewezen aanpak, review-bevindingen, tokens en menswerk.
6. **Wijziging via het contract.** Eén echte wijziging aan het package, twee keer: via het contract
   (contract aanpassen, code volgt) en via de keten (issue, spec, plan). Zelfde metingen als stap 5.
7. **Besluit.** Doorgaan, aanpassen of opgeven, met de metingen als bewijs. Doorgaan levert
   vervolg-issues op voor een pack of `pack/core`; opgeven sluit de milestone met de reden.

## Relaties

- **#499, ontwerpvraag 1** (flinch-proef): startvoorwaarde. De proef laat zien of flinch op
  lighthouse draait; deze milestone bouwt daarop.
- **#620** (mutatietests): blijft buiten de milestone. De kill matrix uit stap 1 en 5 is invoer
  voor het besluit in #620.
- **#622** (levende docs): bewust los. Een latere koppeling tussen contract en docs volgt pas na
  een positief besluit.

## Risico's

- **Code bevat meer dan het contract.** Margheim: "A working codebase holds countless decisions and
  fixes that no spec captures." De vergelijking in stap 5 moet dat verlies zichtbaar maken, niet
  verbergen.
- **Negatieve kennis.** Salkini: "The Contract holds the what. Teams learn the how." Daarom stap 3,
  en terugkeer van een afgewezen aanpak als meetpunt.
- **Jong gereedschap.** flinch is versie 0.1, van één bedrijf. Pinnen beperkt het risico.
- **Te klein om iets te zeggen.** Eén package en één wijziging geven een richting, geen statistiek.
  Het besluit zegt dat er ook bij.

## Wat bij het shapen of bouwen ligt

- Welk package. Criteria: begrensd, goede tests, een geschiedenis met afgewezen aanpakken, en een
  echte wijziging die in de wachtrij staat.
- De drempels voor het besluit, vóór de meting.
- Model en budget voor extractie en regeneratie, en hoe vaak er geregenereerd wordt.
- De omvang van de appetite: groot, zonder deadline.
