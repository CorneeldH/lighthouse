# Area: server

## Crux

De waarheid snel tonen, zonder verouderde data te verbergen. `lighthouse serve` laat zien wat de
keten doet: vragen, teruggegeven issues, runs, het usage-venster en de prioritering. Een snelle
pagina is verleidelijk met een cache, maar een pagina die oude data of een fout als leeg vlak toont,
verbergt precies de vraag waar iemand op wacht. Wat de UI toont, zegt hetzelfde als het board en de
controller.

## Leidend beleid

- Een fout gaat vóór oude data, en een fout of lege lijst is nooit een leeg vlak; elk blok toont data,
  een lege-melding of een foutmelding
  ([forge-blokken cache](../planning/specs/2026-11-13-issue-307-forge-blokken-cache-design.md),
  [UI voor M1](../planning/specs/2026-10-27-ui-m1-dod-design.md)).
- Wat niet meetbaar is, ziet er niet uit als 0%: de UI stuurt op de toestand, niet op een getal
  ([UI voor M1](../planning/specs/2026-10-27-ui-m1-dod-design.md)).
- De UI leidt geen eigen status af die afwijkt van wat tick en triage beslissen; regel en weergave
  delen hun testgevallen
  ([statusmodel](../planning/specs/2026-11-20-statusmodel-en-statusschema-design.md),
  [prioritering](../planning/specs/2026-11-16-prioritering-tabblad-design.md)).
- Eén bron voor de UI van een release: de binary bedt de gebouwde SPA in, en geen gebouwde bestanden
  op `main` ([SPA-skelet](../planning/specs/2026-10-26-spa-skelet-design.md),
  [UI in go install](../planning/specs/2026-11-16-issue-349-go-install-ui-design.md)).
- De milestone-titel linkt naar de URL die de forge-lezer levert (`forge.Milestone.URL`), nooit naar een
  in de SPA gebouwde URL; bij een lege URL blijft de titel platte tekst.

## Bewust niet

- **N1** Een tweede database of Postgres naast SQLite
  ([romp van serve](../planning/specs/2026-10-25-romp-serve-design.md),
  [M1](https://github.com/CorneeldH/lighthouse/milestone/2)).
- **N2** Authenticatie: de tailnet is de toegangsgrens, nooit `tailscale funnel`
  ([romp van serve](../planning/specs/2026-10-25-romp-serve-design.md)).
- **N3** Schrijven naar de forge vanuit de UI, op de rang van een milestone na
  ([statusmodel](../planning/specs/2026-11-20-statusmodel-en-statusschema-design.md),
  [milestoneprioriteit](../planning/specs/2026-11-20-milestone-prioriteit-design.md)).
- **N4** Modelsuggesties op het Priorities-tabblad zonder de richting van een repo als bron
  ([prioritering](../planning/specs/2026-11-16-prioritering-tabblad-design.md),
  [statusmodel](../planning/specs/2026-11-20-statusmodel-en-statusschema-design.md)).
- **N5** Wat de controller beslist en doet; de server toont het alleen → dispatcher
  ([labels](../labels.md)).
