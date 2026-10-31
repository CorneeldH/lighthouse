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
   `tick: no GitHub token: set GH_TOKEN or GITHUB_TOKEN`.
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

Tick haalt nog geen issues op, beslist nog niet en start nog geen run: gather, decide en apply volgen
in #112 en #114.

## Exitcodes

| Code | Wanneer |
|---|---|
| 0 | Alle repo's zijn bedienbaar, of een vorige tick draait nog |
| 1 | Minstens één repo overgeslagen, of een fout (geen `XDG_RUNTIME_DIR`, geen token, ongeldige `host.toml`, database niet te openen) |

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
