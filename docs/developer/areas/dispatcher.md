# Area: dispatcher

## Crux

Onbemand betrouwbaar draaien binnen de rem, zonder dubbele start. De tick start, hervat, merget en
geeft terug zonder dat iemand meekijkt, op een host met een beperkt venster, beperkt geheugen en
beperkte plekken. Een run die twee keer start, een keten die stilvalt zonder spoor, of een rem die
meer doorlaat dan hij zegt, kost meer menswerk dan de controller bespaart.

## Leidend beleid

- De claim is de open runrij: één claimfunctie, één tick tegelijk, en een open run zonder runstaat
  wordt teruggegeven, nooit dubbel gestart
  ([M2](../planning/specs/2026-10-27-m2-dispatcher-design.md),
  [plek vrij in dezelfde tick](../planning/specs/2026-11-16-issue-359-plek-vrij-zelfde-tick-design.md)).
- Elke start, ook een geplande run, gaat door dezelfde rem: usage, geheugen, plekken
  ([geplande runs](../planning/specs/2026-11-16-issue-121-geplande-runs-design.md),
  [M2](../planning/specs/2026-10-27-m2-dispatcher-design.md)).
- Mergen is een deterministische stap van de tick op de gemeten SHA; geen sessie krijgt merge-rechten
  of een token met `workflow`-scope
  ([tick merget](../planning/specs/2026-11-08-issue-294-tick-merget-design.md),
  [workflow-pusher](../planning/specs/2026-11-16-issue-396-workflow-pusher-design.md)).
- Een keten die niet verder kan, stopt met een eerlijke reden, niet met een stille of verkeerde
  ([mislukte post](../planning/specs/2026-11-20-mislukte-post-design.md),
  [forge via REST](../planning/specs/2026-11-17-forge-github-rest-design.md)).
- De werkplek is een adapter achter één contract, met één gedeelde contract-suite
  ([werkplek zonder kenn-forge](../planning/specs/2026-11-07-werkplek-zonder-kenn-forge-design.md),
  [forge-nodes](../planning/specs/2026-11-13-issue-187-forge-nodes-design.md)).

## Bewust niet

- **N1** Een tweede harness voor runs; Codex blijft reviewer
  ([M2](../planning/specs/2026-10-27-m2-dispatcher-design.md),
  [Werkplek zonder kenn-forge](https://github.com/CorneeldH/lighthouse/milestone/8)).
- **N2** De dispatcher op Windows of macOS
  ([Werkplek zonder kenn-forge](https://github.com/CorneeldH/lighthouse/milestone/8),
  [werkplek zonder kenn-forge](../planning/specs/2026-11-07-werkplek-zonder-kenn-forge-design.md)).
- **N3** Een merge van een PR die niet van de run is, of een schakelaar die mergen per repo uitzet
  ([tick merget](../planning/specs/2026-11-08-issue-294-tick-merget-design.md)).
- **N4** Een eigen sandbox-techniek; podman, systemd en Unix-rechten volstaan
  ([Isolatie](https://github.com/CorneeldH/lighthouse/milestone/15)).
- **N5** Fixes in kenn-forge zelf; een defect daar wordt een issue hier
  ([Controller](https://github.com/CorneeldH/lighthouse/milestone/18),
  [AGENTS.md](../../../AGENTS.md)).
- **N6** Wat je van de controller ziet in de SPA → server ([labels](../labels.md)).
