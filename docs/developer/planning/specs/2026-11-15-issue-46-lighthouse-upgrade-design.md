# Skill `lighthouse-upgrade` — ontwerp (#46)

## Waarom

Een gebruiker-repo naar een nieuwe lighthouse-versie brengen is nu een recept van zo'n tien
commando's dat alleen in chatgeschiedenis staat. `/lighthouse-upgrade` maakt er één aanroep van:
kort en voorspelbaar, en altijd handmatig gestart.

## Wat sinds het issue veranderd is

- #267 leverde `lighthouse self-update` en `lighthouse-upgrade.timer`: de binary heeft een eigen,
  geverifieerd upgradepad (release plus checksum).
- `go install …@<tag>` levert een binary zonder web-UI (placeholder). Stap 3 van het issue zou
  dus een release-binary vervangen door een slechtere. Dat repareren is #349, los van dit issue.
- `lighthouse-verify-pr` heet nu `lighthouse-write-pr`.

## Doelen

- Eén skill `lighthouse-upgrade` in `pack/core` die de repo-kant van een upgrade doet: worktree,
  `init --upgrade`, bewijs, commit, PR na akkoord.
- Stoppen met een melding zodra iets niet klopt, nooit forceren of committen bij rood.

## Niet-doelen

- De skill installeert of downgradet zelf geen binary; geen `go install`.
- Geen willekeurige doeltag: het argument `[tag]` uit het issue vervalt.
- Geen trigger vanuit lighthouse, geen automatische repo-upgrade.

## Anti-doelen

- Een upgrade-commit met iets anders dan wat `init --upgrade` schreef.
- `init --upgrade` in de hoofdcheckout of op de default branch.

## Aanpak

Overwogen:

- **A — binary via `self-update`, doelversie = `lighthouse version`** (gekozen). Hergebruikt het
  geverifieerde pad van #267 en botst niet met de timer.
- **B — de skill haalt zelf de release-binary van een opgegeven tag.** Maakt downgrades mogelijk
  die de timer terugdraait, en dupliceert `self-update`.
- **C — alleen repo-werk, binary blijft helemaal bij de gebruiker.** Laat de meest gemaakte fout
  (vergeten de binary bij te werken) open.

## Werking

1. **Staat lezen.** `M` = `version` uit `.lighthouse/manifest.toml`, `B` = `lighthouse version`,
   `L` = de nieuwste release (`gh release view --repo <lighthouse-repo> --json tagName`).
   - `M` of `B` is `dev`: melden en stoppen (een checkout van lighthouse zelf dogfoodt met
     `go run ./cmd/lighthouse init --upgrade`).
   - `B` < `M`: melden en stoppen; de binary is ouder dan de repo.
2. **Binary.** `L` > `B`: stel `lighthouse self-update` voor, met de waarschuwing dat het de
   binary voor de hele host vervangt. Alleen na akkoord. Daarna `B` opnieuw lezen.
   `B` = `M` (en geen akkoord, of geen nieuwere release): melden "al actueel" en stoppen.
3. **Wat verandert.** De release-notes van elke tag in (`M`, `B`] via `gh release view`. Regels die
   vragen om eerst de binary of `lighthouse.toml` bij te werken (bijvoorbeeld "Update the binary …
   before `lighthouse init --upgrade`") apart tonen.
4. **Worktree.** Vanaf een verse default branch (`lighthouse config forge.default_branch`), onder
   `.claude/worktrees/`, branch volgens de branchregel van de repo als die er een heeft, anders
   `chore-lighthouse-<B>`.
5. **Upgrade.** `lighthouse init --upgrade` in de worktree; init toont de diff en vraagt akkoord.
   `refused: N locally modified unit(s)`: melden en stoppen, nooit `--force`. Afgewezen: stoppen.
6. **Bewijs**, in volgorde; bij het eerste rood stoppen, melden wat er staat en niet committen:
   1. `[test_gate] command` uit `lighthouse.toml`;
   2. `.lighthouse/scripts/check-instructions.sh`;
   3. als `[paths] mirror_exceptions` gezet is: `check-instructions.sh --print-mirror-exceptions`
      opnieuw schrijven;
   4. een tweede `lighthouse init --upgrade` geeft exact `No changes.`.
7. **Commit en PR.** `git status` bevat alleen bestanden die `init --upgrade` schreef, plus het
   spiegelbestand. Eén commit `chore(lighthouse): upgrade naar <B>`. Push en PR pas na akkoord van
   de gebruiker, via `/lighthouse-write-pr`.

## Vastgelegd

- Twee varianten, `pack/core/skills/upgrade/{claude,agents}/SKILL.md`, frontmatter
  `name: lighthouse-upgrade`. De `claude`-variant vraagt akkoord met `AskUserQuestion`, de
  `agents`-variant in de chat.
- `pack/core/requires.toml` noemt `upgrade` bij `git` en `gh`; er is geen `go`-sleutel en de skill
  gebruikt geen `go`.
- De lighthouse-repo staat als literal in de skill, met een exacte regel in
  `ci/literal-allowlist.txt`, een reden, en een test die hem vastpint, naar het voorbeeld van
  `pack/core/hooks/run.sh`. Het modulepad komt niet voor.
- `pack/SOURCES.toml`, `internal/packs/core_test.go`, een fragment in `changelog.d/` en de
  dogfood-kopie (`go run ./cmd/lighthouse init --upgrade`, daarna `No changes.`).

## Aan de bouwer

- Hoe versies vergeleken worden (`sort -V` of gelijkwaardig, met of zonder `v`-prefix).
- De exacte tekst van de meldingen en de beschrijving (die moet de overlap-check van
  `check-instructions.sh` doorstaan).
- Welke contracttests uit `tests/pack/test_skill_contracts.py` een regel nodig hebben.

## Bijgewerkte acceptatiecriteria van #46

- `requires.toml` noemt de skill bij `git` en `gh` (niet `go`).
- Het modulepad komt niet voor; de repo-naam staat op de allowlist van `ci/literal-grep.sh`, met
  reden. De literal-grep is groen.
- De PR gaat via `lighthouse-write-pr`.
- De overige AC blijven zoals ze staan.
