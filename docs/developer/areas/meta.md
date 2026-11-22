# Area: meta

## Crux

De repo als snelste lus voor agents, en als dogfood. Agents compileren, testen en lezen in deze repo
veel vaker dan een mens, dus elke seconde CI, elke overbodige test en elk woord instructie telt mee.
Tegelijk draait lighthouse zijn eigen `pack/core`: wat hier niet werkt, werkt nergens, en wat hier
alleen met de hand blijft kloppen, drijft af.

## Leidend beleid

- Snelheid van CI en van de ontwikkellus weegt zwaar: Go boven Python in de repo, en een PR zonder
  Go-wijziging start geen Go-jobs
  ([repo zonder Python](../planning/specs/2026-11-16-repo-zonder-python-design.md),
  [CI zonder Go-jobs](../planning/specs/2026-10-31-issue-151-ci-pytest-split-design.md)).
- Een test blijft alleen als een productiewijziging hem rood maakt; tests worden gesorteerd, niet
  vertaald ([repo zonder Python](../planning/specs/2026-11-16-repo-zonder-python-design.md),
  [gen_code_language_freq weg](../planning/specs/2026-11-20-gen-code-language-freq-weg-design.md)).
- Een regel in de instructies blijft alleen met een toetssteen: een hook die hem draagt, een feit dat
  het model niet kan afleiden, of een recente fout
  ([beoordelingstabel](../planning/specs/2026-11-21-issue-390-beoordelingstabel.md),
  [spec-adversary opgeheven](../planning/specs/2026-10-31-issue-143-spec-adversary-opheffen.md)).
- Lighthouse draait zijn eigen pack: een wijziging die verandert wat `init` schrijft, komt met de
  geïnstalleerde kopie, en CI eist `No changes.`
  ([AGENTS.md](../../../AGENTS.md), [zonder Python](../planning/specs/2026-11-07-zonder-python-design.md)).

## Bewust niet

- **N1** Richting of status in altijd-geladen context (`AGENTS.md`, `CLAUDE.md`)
  ([richting per repo](../planning/specs/2026-11-22-issue-372-richting-per-repo-design.md),
  [verslag](../planning/specs/2026-11-22-issue-372-richting-verslag.md)).
- **N2** Een roadmapdocument; de milestones in hun rangorde zijn de roadmap
  ([richting per repo](../planning/specs/2026-11-22-issue-372-richting-per-repo-design.md),
  [milestoneprioriteit](../planning/specs/2026-11-20-milestone-prioriteit-design.md)).
- **N3** Sneller door minder te testen of door betaalde runners
  ([CI zonder Go-jobs](../planning/specs/2026-10-31-issue-151-ci-pytest-split-design.md)).
- **N4** Harness-geheugen als drager van een regel; een regel die moet blijven, staat in de repo
  ([AGENTS.md](../../../AGENTS.md)).
- **N5** Documentatie voor wie lighthouse gebruikt → product ([labels](../labels.md)).
