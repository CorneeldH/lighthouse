# Area: packs

## Crux

Poorten per taal die bewezen vangen, zonder ze te versoepelen. Een taalpakket installeert in elke
repo linters, typecheckers en selectors voor de test-gate, en een onbemande run vertrouwt erop dat
die poorten vangen wat ze beloven. Een regel die stil uit een config valt, een linter die niet echt
is aangesloten of een import die alleen meelift, maakt een poort tot schijn. De poort wordt
betrouwbaar door bewijs, niet door hem soepeler te zetten tot hij groen is.

## Leidend beleid

- Elke blokkerende regel van een pack-hook heeft een canary; een regel zonder canary of een canary
  zonder regel laat de test falen
  ([canary per regel](../planning/specs/2026-11-19-canary-per-regel-design.md),
  [Kwaliteitspoorten](https://github.com/CorneeldH/lighthouse/milestone/13)).
- Een poort zonder gereedschap, omgeving of passende versie geeft `COULD NOT CHECK`, nooit groen
  ([directe imports](../planning/specs/2026-11-19-issue-383-directe-imports-design.md)).
- Per taal het gereedschap dat de taal kent, gepind en met getoetste licentie; geen eigen parser
  ([directe imports](../planning/specs/2026-11-19-issue-383-directe-imports-design.md),
  [Kwaliteitspoorten](https://github.com/CorneeldH/lighthouse/milestone/13)).
- Een poort blokkeert op wat de beurt of de commit raakte, zodat oude schuld een repo niet stilzet
  ([directe imports](../planning/specs/2026-11-19-issue-383-directe-imports-design.md),
  [gate na commit](../planning/specs/2026-10-27-gate-na-commit-design.md)).

## Bewust niet

- **N1** Lint- of typeconfiguratie versoepelen om een poort groen te krijgen, ook niet in lighthouse
  zelf ([Kwaliteitspoorten](https://github.com/CorneeldH/lighthouse/milestone/13),
  [pack/go](../planning/specs/2026-10-25-pack-go-design.md)).
- **N2** Bevindingen in een gebruikersrepo wegwerken; dat is toepassingswerk in die repo
  ([Kwaliteitspoorten](https://github.com/CorneeldH/lighthouse/milestone/13),
  [directe imports](../planning/specs/2026-11-19-issue-383-directe-imports-design.md)).
- **N3** Nieuwe Python buiten `pack/python`; de bash-hooks van de taalpakketten blijven bash rond hun
  linter ([zonder Python](../planning/specs/2026-11-07-zonder-python-design.md)).
- **N4** De taalneutrale testskill en de hooks van `pack/core` → core; een pack zet daar geen
  taalspecifieke inhoud in
  ([testskill](../planning/specs/2026-10-24-testskill-design.md),
  [pack/go](../planning/specs/2026-10-25-pack-go-design.md)).
