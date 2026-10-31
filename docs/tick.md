# Tick

`lighthouse tick` is de dispatcher-tick: een eigen proces dat een timer of cron periodiek start.
Het is een korte run, geen daemon. Het ontwerp staat in
[de dispatcher-spec](developer/planning/specs/2026-10-27-m2-dispatcher-design.md) (§2 *Configuratie*,
§3 *De tick*).

## Wat tick nu doet

1. **Slot.** Neemt `${XDG_RUNTIME_DIR}/lighthouse/tick.lock`. Staat het slot nog vast, dan schrijft
   tick `lighthouse: previous tick still running` naar stderr en eindigt met exitcode 0. Zonder
   `XDG_RUNTIME_DIR` stopt tick met `tick: XDG_RUNTIME_DIR is not set: cannot take tick.lock`; bij een
   relatief pad met `tick: XDG_RUNTIME_DIR is not an absolute path: cannot take tick.lock`.
2. **Token.** Vereist `GH_TOKEN` of `GITHUB_TOKEN`. Zonder stopt tick met
   `tick: no GitHub token: set GH_TOKEN or GITHUB_TOKEN`. Het token heeft de scopes `repo` en
   `project` nodig: lighthouse leest en schrijft er comments, reacties en het board mee.
3. **Configuratie.** Leest `[dispatch]` uit `host.toml` (`$LIGHTHOUSE_HOST_TOML`, anders
   `~/.config/lighthouse/host.toml`). Een ongeldige waarde stopt tick met een fout die de sleutel noemt. Een
   onbekende sleutel onder `[dispatch]` (een typfout als `max_runtme`) stopt tick ook, met een fout die
   de sleutel noemt; sleutels buiten `[dispatch]` blijven genegeerd. Ontbreekt `host.toml`, dan gelden de
   standaardwaarden en zijn er geen repos: tick schrijft dan `lighthouse: no repos in [dispatch] of <pad>`
   (zonder pad: `lighthouse: no host.toml: no repos to check`) naar stderr en eindigt met exitcode 0.
4. **Database.** Opent de SQLite-database naast `serve`
   (`${XDG_STATE_HOME:-~/.local/state}/lighthouse/lighthouse.db`, of `--db <pad>`). Daar staat de
   runstaat: claim, pogingen en vrijgeven.
5. **Controle per repo.** Leest het `lighthouse.toml` van elke repo in `[dispatch] repos`. Voldoet
   een repo niet, dan slaat tick hem luid over met `lighthouse: skip <pad>: <redenen>` op stderr en
   gaat door met de rest.
6. **Precheck.** Per bedienbare repo leest tick het board en de open runs, en rekent daaruit een
   vingerafdruk: per item in Controller of On hold de Status, Priority en de `updatedAt` van item en
   issue, de voorgangers van een On hold-item, de PR's van een item met een open run, en de open runs
   zelf. Een nieuwe comment telt mee via de `updatedAt` van het issue. Tick slaat de volledige gather
   alleen over als de vingerafdruk gelijk is aan de vorige, de vorige volledige gather jonger is dan
   30 minuten, en er sinds die gather geen tijdgrens viel. Tijdgrenzen zijn per open run
   `started_at + max_runtime` en `started_at + max_wait`, en het begin van het `night`-venster.
   Vingerafdruk en tijd van de laatste volledige gather staan in de tabel `tick_state` van de database.
7. **Gather.** Is de repo aan de beurt, dan leest tick de comments van elk item en bouwt de snapshot:
   de items in Controller of On hold met hun velden, voorgangers en PR's (de PR's die het issue sluiten
   plus de PR's op de branch van een open run, met hun checks), hun comments en de open runs. Alleen
   comments van vertrouwde auteurs komen erin: hun rol op de repo (`role_name` van de
   collaborator-permissie) staat in `[dispatch] answer_permissions`. Een 🤖-comment van een andere
   auteur valt weg met een regel op stderr. Een 🚀 telt alleen als de login van het token hem zette;
   die login vraagt tick hoogstens één keer per tick op, en niet als geen repo aan de beurt is. Na de
   gather bewaart tick de vingerafdruk en schrijft
   `lighthouse: gathered <repo>: <n> items (<reden>)` naar stderr; de reden is `first`, `changed`,
   `stale` of `boundary`. Een fout van GitHub (ook een GraphQL-fout in een antwoord met status 200)
   slaat de repo luid over met `lighthouse: skip <pad>: <fout>` en bewaart niets, zodat de volgende
   tick opnieuw kijkt.

Tick beslist nog niet en start nog geen run: decide en apply volgen in #114.

## Exitcodes

| Code | Wanneer |
|---|---|
| 0 | Alle repo's zijn bedienbaar, of een vorige tick draait nog |
| 1 | Minstens één repo overgeslagen (niet bedienbaar, of de gather mislukte), of een fout (geen `XDG_RUNTIME_DIR`, geen token, ongeldige `host.toml`, database niet te openen) |

## `lighthouse gather`

```bash
lighthouse gather <repo-dir> [--db <pad>]
```

Bouwt de snapshot van één repo zoals tick dat doet, en schrijft hem als JSON naar stdout (twee
spaties inspringing, `"version": 1`, tijden in UTC). Het gebruikt hetzelfde token, dezelfde
`host.toml` en dezelfde database als tick, maar neemt geen `tick.lock`, draait geen precheck en
bewaart geen stand in `tick_state`. De repo moet bedienbaar zijn (zie *Vereisten per repo*).

De uitvoer op een nep-GitHub is de fixture
[`internal/dispatch/testdata/snapshot.json`](../internal/dispatch/testdata/snapshot.json): daarop
wordt decide getest.

## `[dispatch]` in `host.toml`

Elke sleutel is optioneel; dit zijn de standaarden.

```toml
[dispatch]
repos = ["/home/me/repositories/lighthouse"]  # absolute paden van de main checkouts; standaard []
slots = 4                    # lopende plus geparkeerde runs op de host
max_software = 2             # runs tegelijk in het Software-profiel
min_mem_available_gb = 8     # rem: MemAvailable - n x reserve >= min
reserve_per_run_gb = 3       # reserve per lopende run
night = "21:00-08:00"        # venster voor `when: night`, lokale tijd
max_runtime = "8h"           # bovengrens per run
max_wait = "1h"              # grens voor een stille run
answer_permissions = ["admin", "maintain"]

[dispatch.forge]
url = "http://127.0.0.1:8091"
token_file = "/home/me/.kenn/forge/auth_token"   # standaard: <home>/.kenn/forge/auth_token
```

`slots` en `max_software` zijn minstens 1, `min_mem_available_gb` en `reserve_per_run_gb` minstens 0.
`repos` mag alleen absolute paden bevatten. Per rechtenprofiel kan `[dispatch.profiles.<naam>]` staan;
de spec beschrijft de sleutels.

## Vereisten per repo

Het `lighthouse.toml` van elke repo in `repos` heeft:

- een `[controller]`-tabel;
- `[usage_guard]` met `controller = true`;
- `[forge.board]` met `project_number` (positief), `project_id`, `status_field_id` en
  `priority_field_id`, en onder `status_options` minstens `Controller` en `On hold`.

Ontbreekt iets, dan staan alle ontbrekende sleutels, bij naam, in één `skip`-regel.
