# Area: usage

## Crux

Het gebruiksvenster bewaken over sessies en runs. Een interactieve sessie en een controller-run delen
hetzelfde abonnement; wie het venster leegtrekt, stopt de ander midden in het werk. De meter moet het
venster dus op de echte bron meten, en elke sessie en elke run moet het alarm op tijd zien om netjes
af te ronden.

## Leidend beleid

- Eén meter schrijft het alarm; de usage-gate leest alleen dat alarm, nooit de cache, en meet niet
  zelf ([usage-guard](../planning/specs/2026-10-25-usage-guard-design.md),
  [M1](https://github.com/CorneeldH/lighthouse/milestone/2)).
- De usage-gate onderbreekt één keer per venster en dwingt een hervatbare afronding af; hij faalt
  nooit rood ([usage-guard](../planning/specs/2026-10-25-usage-guard-design.md),
  [usage-gate](../../usage-gate.md)).
- Een venster dat niet meetbaar is, telt niet als 0%: de UI toont geen getal, en de rem laat niets
  starten ([UI voor M1](../planning/specs/2026-10-27-ui-m1-dod-design.md),
  [M2](../planning/specs/2026-10-27-m2-dispatcher-design.md)).

## Bewust niet

- **N1** Een tweede schrijver van het alarm naast `lighthouse usage`
  ([M1](https://github.com/CorneeldH/lighthouse/milestone/2),
  [usage-guard](../planning/specs/2026-10-25-usage-guard-design.md)).
- **N2** Een Claude-sessie stoppen op het venster van Codex; dat venster gaat naar de UI en de
  dispatcher ([M1](https://github.com/CorneeldH/lighthouse/milestone/2),
  [usage-guard](../planning/specs/2026-10-25-usage-guard-design.md)).
- **N3** De rem die starts tegenhoudt en runs hervat na de reset → dispatcher
  ([M2](../planning/specs/2026-10-27-m2-dispatcher-design.md),
  [geplande runs](../planning/specs/2026-11-16-issue-121-geplande-runs-design.md)).
