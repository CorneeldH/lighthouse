# Controller-knoppen in de instapniveaus (#255)

## Waarom

Een instapniveau (`light`, `standard`, `strict`) zet nu alleen de guards, checks en skills die
`init` installeert. De dispatcher leest daarnaast de knoppen uit `[controller]` in
`lighthouse.toml`, los van het niveau. Op `light` en `standard` staan de skills `review-spec` en
`review-plan` uit, maar `spec_review` en `plan_review` staan standaard op `true`
([`repoconfig.go`](../../../../internal/repoconfig/repoconfig.go), `Load`). De dispatcher stuurt een
run dan naar een skill die niet geïnstalleerd is. De tweede gebruiker draait de dispatcher op een
eigen server, met één `level`: daar moet het niveau ook de controller samenhangend zetten.

## Doelen

- Elk niveau heeft een set controller-knoppen. De docs zeggen per niveau wat de set overslaat en
  waarom.
- Een losse sleutel in `[controller]` wint van het niveau.
- Geen niveau beantwoordt een permissie- of securityvraag automatisch.

## Niet-doelen

- `plekken`. Dat is `slots` in `host.toml [dispatch]`: een instelling per host, niet per repo. Een
  niveau van één repo zet geen host-waarde. De docs zeggen dat.
- `[controller.auto_aanbevolen]` of een andere knop voor automatisch antwoorden. Die bestaat niet
  in Go, alleen als proza in de write-handoff-skill; het ontwerp ervan hoort bij #168.
- `profile`, `[controller.locations]` en `[controller.triage]`: geen niveau-knoppen.
- Een waarschuwing of blokkade wanneer een losse `spec_review = true` botst met een uitgezette
  `skill:review-spec`. De gebruiker koos dat expliciet; de docs noemen het gevolg.

## Aanpakken

1. **Afleiden uit wat geïnstalleerd is** (gekozen). `spec_review` en `plan_review` volgen de stand
   van `skill:review-spec` en `skill:review-plan` in `.lighthouse/components.toml`. Knop en skill
   lopen nooit uit elkaar, en een `[components]`-override telt vanzelf mee.
2. Een `[<level>.controller]`-tabel in `pack/core/levels.toml`. Alles op één plek, maar `spec_review`
   en `skill:review-spec` worden twee bronnen die met de hand gelijk moeten blijven.
3. Een vaste tabel per niveau in Go. Het kleinst, maar de set is onzichtbaar voor wie
   `levels.toml` leest, en het heeft hetzelfde probleem met twee bronnen als 2.

De gebruiker koos 1: het niveau beslist al welke review-skills er staan, en de knop volgt dat.

## Ontwerp

`repoconfig.Load` bepaalt per knop de waarde, in deze volgorde (de eerste die er is, wint):

| Knop | 1. Losse sleutel | 2. Uit het niveau | 3. Zonder `components.toml` |
|---|---|---|---|
| `spec_review` | `[controller] spec_review` | `skill:review-spec` is niet `off` | `true` |
| `plan_review` | `[controller] plan_review` | `skill:review-plan` is niet `off` | `true` |
| `bij_rondegrens` | `[controller] bij_rondegrens` | `"doorgaan"`, op elk niveau | `"doorgaan"` |

- `components.toml` lezen gaat via `levels.ReadInstalled(dir)`. Ontbreekt het bestand, dan staat
  alles aan (zoals `ReadInstalled` al zegt): een repo van vóór de niveaus houdt het gedrag van nu.
- Is `components.toml` er maar onleesbaar, dan geeft `Load` een fout met het pad. De tick slaat de
  repo dan over met die fout, zoals bij een kapotte `lighthouse.toml`. Nooit stil terugvallen op
  `true`: dat is precies de botsing die dit issue oplost.
- `bij_rondegrens` telt alleen als er een review draait. Daarom is `"doorgaan"` op elk niveau de
  standaard; een leeg veld wordt `"doorgaan"`. Alleen `"vraag"` verandert gedrag
  (`internal/dispatch/instructions.go`).

Resultaat per niveau, met de pack van nu:

| Niveau | `spec_review` | `plan_review` | `bij_rondegrens` | Waarom |
|---|---|---|---|---|
| `light` | `false` | `false` | `doorgaan` | De review-skills staan uit: de review vraagt Codex, een tweede modelfamilie. |
| `standard` | `false` | `false` | `doorgaan` | Idem. |
| `strict` | `true` | `true` | `doorgaan` | De reviews draaien; bij de rondegrens gaat de run door met de open punten als `accepted_open`. |

Permissie en security: de set heeft geen knop die een vraag automatisch beantwoordt, op geen enkel
niveau. Komt er later zo'n knop (#168), dan staat hij in elk meegeleverd niveau uit.

## Docs

- `docs/levels.md`: een sectie met de knoppen per niveau (de tabel hierboven), de volgorde losse
  sleutel → niveau → standaard, waarom `plekken` er niet in zit, en de zin over permissie en
  security uitgebreid naar de controller.
- `docs/tick.md`, *Vereisten per repo*: één regel dat de standaard van de review-knoppen uit het
  niveau komt, met een link naar `docs/levels.md`.
- Een fragment in `changelog.d/`.

## Tests

- `repoconfig`: per niveau (`light`, `standard`, `strict`, plus geen `components.toml`) de afgeleide
  `SpecReview`, `PlanReview` en `AtRoundLimit`; een losse sleutel wint in beide richtingen (`true`
  op `light`, `false` op `strict`); een onleesbare `components.toml` geeft een fout.
- Een test die de niveaus uit de echte `pack/core/levels.toml` oplost en de tabel *Resultaat per
  niveau* vastlegt. Gaat een review-skill op `standard` aan, dan faalt hij en moet de doc-tabel mee.

## Aan de bouwer

- Of `internal/repoconfig` `internal/levels` mag importeren volgens `lighthouse imports check`;
  zo niet, dan een kleine lezer van `components.toml` in `repoconfig` of een omgekeerde aanroep
  vanuit `cli`. Geen gedragsverschil.
- Waar de tick een `Load`-fout van één repo nu meldt; de nieuwe fout volgt dezelfde weg.
- *Mijn keuze:* `AtRoundLimit` wordt `"doorgaan"` in plaats van `""` wanneer niets gezet is. Tests
  die `""` vastleggen gaan mee.
