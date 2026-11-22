# Repokaart: wat `init --upgrade` met elk gebied doet (#192)

Status: ontwerp, wacht op goedkeuring van de schets in #192.
Bouwt voort op [de repokaart als boom](2026-11-01-plattegrond-na-init-design.md) (#101).

## Waarom

De repokaart (`docs/site/repo.html`) toont alleen wat `lighthouse init` neerzet. Bij een upgrade
wil je twee dingen weten: wat je zelf mag bewerken zonder dat het terugkomt, en waar een eigen
wijziging de upgrade tegenhoudt tot je `--force` geeft. De eerste versie liet dat zien met zones
en een legenda; dat was te druk en herhaalde de indeling (Corneel, 2026-11-01).

## Doelen

- Per rij van de boom staat wat `init --upgrade` ermee doet, gecontroleerd tegen
  `internal/initcmd/` en `internal/reconcile/`.
- `--force` staat precies bij de rijen waar een lokale wijziging de run laat weigeren, en niet
  bij `lighthouse.toml`, `.lighthouse/manifest.toml` en `host.toml`.
- Een test bewaakt dat in beide richtingen.

## Niet-doelen en anti-doelen

- Geen wijziging aan het gedrag van `init` of `reconcile`; de kaart beschrijft, hij stuurt niet.
- Geen legenda, geen zones, geen rij met twee dingen (anti-doelen uit het ontwerp van #101).
- Geen nieuwe rijen: `CLAUDE.md` en het uitgefaseerde `.lighthouse/lib/` blijven van de kaart.
- Mislukt als: de stand Upgrade de boom net zo druk maakt als de zones-versie, of de kaart
  `--force` noemt waar de code niet weigert (of andersom).

## Aanpakken

| | Aanpak | Afweging |
|---|---|---|
| **A (gekozen)** | Een vierde stand van de schakelaar, **Upgrade**, die naast elke rij een korte tekst zet | Elke rij draagt zijn eigen antwoord; de test leest data. Drukker, maar alleen in die stand. |
| B | Eén alinea onder de kaart in `#repo-notes` die de gebieden groepeert | Geen JS-wijziging, maar de lezer zoekt de namen zelf in de boom en de test moet proza lezen. |

Corneel koos A (2026-11-22).

## Wat de code doet (geverifieerd)

- Een eenheid die lokaal afwijkt van zowel het pack als de hash in het manifest, of die lokaal
  verwijderd is, is een `Conflict` (`internal/reconcile/reconcile.go:79-105`).
- Eén conflict laat de **hele** run weigeren; `--force` overschrijft dan elk gewijzigd deel tegelijk
  (`internal/initcmd/initcmd.go:196-211`).
- Eenheden zijn bestanden, blokken in `AGENTS.md`, hook-registraties en `permissions.allow`-regels
  in `.claude/settings.json` (`internal/initcmd/initcmd.go:370-453`). De rest van `settings.json`
  en de tekst buiten de blokken in `AGENTS.md` zijn van het project.
- `lighthouse.toml` is geen eenheid: `planConfig` vult alleen ontbrekende sleutels en tabellen aan
  en herschrijft bij `--upgrade` twee oude waarden van `[test_gate] command`
  (`internal/initcmd/initcmd.go:618-653`, `internal/initcmd/gatecommand.go:11-14`). Hij weigert nooit.
- `.lighthouse/manifest.toml` is geen eenheid en wordt elke run opnieuw geschreven
  (`internal/initcmd/initcmd.go:490-503`). Hij weigert nooit.
- `host.toml`: `--upgrade` schrijft hem niet; ontbreekt hij, dan een notitie
  (`internal/initcmd/host.go:43-51`).
- `internal/initcmd/testdata/places.golden` (gegenereerd door `TestPlacesGolden`) legt per plek de
  soort eenheid vast. Een plek kan weigeren precies als die soort `file`, `block`, `hook` of
  `permission` bevat; `init` betekent: geen eenheid.

## Schets

Stand **Upgrade** van de schakelaar. De rijen dimmen niet; elke rij krijgt rechts een korte tekst.
`--force` staat als losse code-chip achter de tekst. Boven de boom, alleen in deze stand, één regel
over de run als geheel. De teksten zijn Engels, zoals de rest van de site.

```text
[Everything] [Adds] [Edits your file] [Upgrade]

One edited part stops the whole upgrade; --force overwrites every edited part at once.

your repo/
  ▤ AGENTS.md                    lighthouse's blocks replaced, your text stays   --force
  ⚙ lighthouse.toml              missing keys added, your values stay
  .claude/
    ⚓ hooks/lighthouse/          replaced, stale ones removed                    --force
    ◆ skills/lighthouse-*/       replaced, stale ones removed                    --force
    ◇ agents/lighthouse-*.md     replaced, stale ones removed                    --force
    ⚙ settings.json              lighthouse's hooks and rules replaced,          --force
                                 the rest stays
    your own skills, agents and settings
  .agents/
    ◆ skills/lighthouse-*/       replaced, stale ones removed                    --force
    your own skills
  .lighthouse/
    ⚙ scripts/                   replaced, stale ones removed                    --force
    ⚙ headings.toml              replaced                                        --force
    ⚙ manifest.toml              rewritten on every run
  .github/
    ⎇ pull_request_template.md   replaced                                        --force
    ⎇ workflows/lighthouse-*.yml replaced, stale ones removed                    --force
    your own workflows
  your code, your tests, your config, …

On your workstation
  ⚙ ~/.config/lighthouse/host.toml   left alone
```

De glyphs in de schets staan er ter oriëntatie; de echte komen uit `SORTS` in `docs/site/map.js`.
De chip `--force` krijgt voor schermlezers de tekst "refuses while you edited it; --force
overwrites".

## Wat er gebouwd wordt

- `docs/site/data/repo.json`: elke rij krijgt `upgrade` (de tekst) en `force` (`true`/`false`).
  Ook `upgradeNote` op het hoogste niveau voor de regel boven de boom.
- `docs/site/map.js`: `REPO_MODES` krijgt `{ id: "upgrade", label: "Upgrade" }`. `repoEntries`
  geeft per rij `upgrade` en `force` door en dimt in deze stand niets. `renderRepo` toont de tekst
  en de chip alleen in de stand Upgrade, en de regel boven de boom ook.
- `docs/site/map.css`: opmaak van tekst en chip; op telefoonbreedte valt de tekst onder de rij.
- `docs/site/repo.html`: ondertitel en "Wait, but why?" noemen ook de upgrade. De cache-busting
  `?v=` gaat omhoog.
- `docs/site/test/repo.test.mjs`:
  - `force` van elke rij is `true` precies als de soorten van zijn `place` in `places.golden` een
    van `file`, `block`, `hook`, `permission` bevatten; een rij zonder `place` (`host.toml`) heeft
    `force: false`. Gevallen in de `cases`-tabel: `force` aan op `lighthouse-toml` en op
    `manifest` geeft een probleem, `force` uit op `workflows` en op `claude-settings` ook.
  - Elke rij heeft een niet-lege `upgrade`.
  - Het bestaande geval "entries refuse an unknown mode" gebruikt nu `upgrade` als ongeldige
    stand; dat wordt een andere naam.
- Een fragment in `changelog.d/`.

## Aan de bouwer

- De precieze CSS en de plaats van de chip op smalle schermen.
- De exacte Engelse formulering, zolang elke tekst te herleiden blijft tot een regel onder
  "Wat de code doet".
