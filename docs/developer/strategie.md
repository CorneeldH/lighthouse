# Strategie

De kern van lighthouse als product, naar Rumelt: diagnose, leidend beleid, en wat bewust niet. De
samenhangende acties zijn de milestones. Dit document legt richting vast, geen status. Citeer een
punt als `strategie/S2`; de uitgangspunten staan in [uitgangspunten.md](uitgangspunten.md), de areas
in [areas/core.md](areas/core.md).

## Diagnose

Agents schrijven de code. Wat een engineer met agents begrenst, is niet de code maar:

- **intent**: weten wat je wilt en dat helder maken;
- **taste**: weten hoe het werk moet gebeuren;
- **context**: de juiste informatie op het juiste moment;
- **aandacht**: hoofdpunten eerst, inzoomen waar nodig;
- **telemetrie**: meten hoe modellen en harnesses worden ingezet, om dat te optimaliseren.

Lighthouse maakt die expliciet en zet ze in de keten. Wie taste kan uitleggen en in de keten
vastlegt, kan op het proces vertrouwen.

## Voor wie

In deze volgorde:

1. Corneel.
2. Een bevriende ontwikkelaar.
3. Collega's bij CEDA.
4. Iedereen.

## Leidend beleid

- Vertrouwen bouwen op gemeten, deterministisch bewijs, zodat de keten doorloopt tot de merge
  ([tick merget](planning/specs/2026-11-08-issue-294-tick-merget-design.md),
  [canary per regel](planning/specs/2026-11-19-canary-per-regel-design.md)).
- De mens houdt instructies, configuratie en promotie
  ([pins](planning/specs/2026-11-15-issue-325-pins-design.md),
  [zelflerende lighthouse](planning/specs/2026-11-16-zelflerende-lighthouse-design.md)).
- Het issue op de forge is de gedeelde staat: board, labels en comments, niet een eigen administratie
  ([statusmodel](planning/specs/2026-11-20-statusmodel-en-statusschema-design.md),
  [handoffs](planning/specs/2026-11-01-handoffs-write-pr-close-issue-design.md)).
- Lighthouse levert het vermogen, elke repo past het toe
  ([zelflerende lighthouse](planning/specs/2026-11-16-zelflerende-lighthouse-design.md),
  [gepland op de kaart](planning/specs/2026-11-22-issue-476-gepland-op-de-kaart-design.md)).

## Bewust niet

- **S1** Productmonitoring: gebruik, tevredenheid en product-SLO's blijven bij het ontwikkelde
  product; lighthouse meet zijn eigen leverproces
  ([M1](https://github.com/CorneeldH/lighthouse/milestone/2),
  [zelflerende lighthouse](planning/specs/2026-11-16-zelflerende-lighthouse-design.md)).
- **S2** Hosting op SDP of SRC: SRC is een werkplek waarin een run draait, geen plek waar lighthouse
  zelf draait ([M4](https://github.com/CorneeldH/lighthouse/milestone/5)).
- **S3** Een tweede database naast `lighthouse.db`
  ([romp van serve](planning/specs/2026-10-25-romp-serve-design.md),
  [M2](planning/specs/2026-10-27-m2-dispatcher-design.md)).
- **S4** Een tweede harness voor runs; Codex blijft reviewer
  ([M2](planning/specs/2026-10-27-m2-dispatcher-design.md),
  [Werkplek zonder kenn-forge](https://github.com/CorneeldH/lighthouse/milestone/8)).
- **S5** Een eigen sandbox: podman, systemd en Unix-rechten volstaan
  ([Isolatie](https://github.com/CorneeldH/lighthouse/milestone/15)).
- **S6** Automatische promotie van model, skill of configuratie: een wijziging volgt op een menselijk
  besluit ([zelflerende lighthouse](planning/specs/2026-11-16-zelflerende-lighthouse-design.md),
  [pins](planning/specs/2026-11-15-issue-325-pins-design.md)).
- **S7** Een derde forge naast GitHub en GitLab
  ([forge-interface](planning/specs/2026-11-08-forge-interface-design.md),
  [GitLab-forge](https://github.com/CorneeldH/lighthouse/milestone/9)).
- **S8** De dispatcher op Windows of macOS; de dispatcher draait op Linux
  ([werkplek zonder kenn-forge](planning/specs/2026-11-07-werkplek-zonder-kenn-forge-design.md),
  [hostaannames](../host-assumptions.md)).
- **S9** Werk upstream uit eigen beweging: een defect in een extern gereedschap wordt een issue hier,
  met de upstreamtekst als concept ([AGENTS.md](../../AGENTS.md),
  [Controller](https://github.com/CorneeldH/lighthouse/milestone/18)).

## Roadmap

De roadmap zijn de milestones in hun rangorde op de forge
([milestoneprioriteit](planning/specs/2026-11-20-milestone-prioriteit-design.md)); dit document houdt
er geen lijst van bij.
