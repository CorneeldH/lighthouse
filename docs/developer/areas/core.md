# Area: core

## Crux

Een gate moet onbemand te vertrouwen zijn: nooit stil groen, nooit te omzeilen. De hooks en
keten-skills van `pack/core` zijn wat een onbemande run tegenhoudt, in elke repo met lighthouse. Een
gate die stil stopt met kijken, ziet er van buiten uit als schone code, en een gate die met één vlag
opzij gaat, houdt een agent niet tegen. Elke gate blokkeert dus, of zegt luid dat hij niet kon
controleren, en geeft op elke machine hetzelfde oordeel.

## Leidend beleid

- Kan een gate niet controleren (geen binary, kapotte config, ontbrekend gereedschap), dan zegt hij
  `COULD NOT CHECK`; nooit stil groen
  ([zonder Python](../planning/specs/2026-11-07-zonder-python-design.md),
  [roborev lokaal](../planning/specs/2026-11-01-issue-119-roborev-lokaal-design.md)).
- Een blokkerende regel heeft een canary die bewijst dat de gate hem vangt
  ([canary per regel](../planning/specs/2026-11-19-canary-per-regel-design.md)).
- Een gate is niet te omzeilen: `--no-verify` blokkeert, en de test-gate draait na elke commit op wat
  die commit raakt ([gate na commit](../planning/specs/2026-10-27-gate-na-commit-design.md)).
  `[test_gate] on` kiest het moment: bij de commit (standaard), bij de PR-create, of beide
  ([test-gate](../../test-gate.md#wanneer-draait-de-gate-test_gate-on)).
- `pack/core` is generiek: geen pad, label of areanaam van één repo; wat per repo verschilt, staat in
  `lighthouse.toml` ([loop-kaart bij PR](../planning/specs/2026-11-01-loop-kaart-bij-pr-design.md),
  [gepland op de kaart](../planning/specs/2026-11-22-issue-476-gepland-op-de-kaart-design.md)).
- Eén keten voor elke forge, via `lighthouse forge`: geen twee skills per forge, en de GitHub-keten
  wordt er niet stiller of minder streng door
  ([forge-wrapper](../planning/specs/2026-11-16-issue-260-forge-wrapper-design.md),
  [close-issue op de forge](../planning/specs/2026-11-19-issue-406-close-issue-forge-design.md)).
- Git-paden (`--show-toplevel`, `--git-common-dir`, `--git-dir`, `worktree list`) gaan via
  `internal/gitpath.Native`, want git geeft op Windows schuine strepen en een CRLF-einde; meldingen
  tonen native paden. `TestEveryGitPathReadUsesGitpath` bewaakt dat elk bestand dat zo'n pad leest
  de helper importeert.

## Bewust niet

- **N1** Een model of externe detector in een hook die bij elke write draait: te traag, kost geld,
  werkt niet offline ([codetaal-hook](../planning/specs/2026-10-24-codetaal-hook-design.md)).
- **N2** Een merge-commando in `lighthouse forge`; alleen de tick merget, deterministisch → dispatcher
  ([close-issue op de forge](../planning/specs/2026-11-19-issue-406-close-issue-forge-design.md),
  [tick merget](../planning/specs/2026-11-08-issue-294-tick-merget-design.md)).
- **N3** De usage-gate, al is hij een hook in `pack/core` → usage ([labels](../labels.md)).
- **N4** Wat `init` in een repo schrijft → init; core gaat over wat de geschreven hook of skill doet
  ([labels](../labels.md)).
- **N5** Hooks op native Windows; de hooks vragen WSL → product
  ([releasebeleid](../planning/specs/2026-10-27-releasebeleid-design.md)).
