# Claude-gebruiksvenster

`lighthouse usage` meet hoeveel van het Claude-sessievenster en het weekvenster al verbruikt is.
De meting staat in een cachebestand en, op of boven een drempel, in een alarmbestand dat de shell-guard
leest.

## Commando's

- `lighthouse usage refresh [--force]`: meet nu. Zonder `--force` blijft een cache die jonger is
  dan `cache_ttl_seconds` staan en start `claude` niet.
- `lighthouse usage tick`: ruimt een verlopen alarm op en ververst alleen als er een
  `claude`-proces draait (`pgrep -x claude`). Bedoeld voor een timer. Alleen exitcode 1 van `pgrep` betekent "geen proces"; ontbreekt `pgrep`
  of eindigt het met 2 of 3, dan ververst `tick` toch.

Beide eindigen altijd met exitcode 0. Bij succes staat er één regel op stdout:
`usage: refreshed`, `fresh`, `busy` of `idle`. `busy` betekent dat een andere verversing de lock
houdt; deze aanroep meet dan niet. Bij een fout staat er één regel
`usage: failed: <fout>` op stderr en blijft de oude cache staan. Een kapotte `host.toml` geeft
`usage: config: <fout>` op stderr; de meting gaat door met de standaardwaarden.
Een sleutel met het verkeerde type (`halt_pct_session = "85"`) telt als een TOML-fout: alle
standaardwaarden plus dezelfde `usage: config:`-regel.

## Paden

Alles staat in `${XDG_CACHE_HOME:-$HOME/.cache}/lighthouse`. Een niet-lege `XDG_CACHE_HOME` wint,
ook een relatieve. Zonder beide eindigt het commando met `usage: failed: ...`.

| Pad | Inhoud |
|---|---|
| `usage-window.json` | de cache van de laatste meting |
| `usage-window.alarm` | het alarm, alleen aanwezig op of boven een drempel |
| `usage-window.lock` | lock die gelijktijdige verversingen uitsluit |
| `usage-refresh/` | werkmap van de `claude`-aanroep |

De meting is `claude -p /usage --output-format json`, met `usage-refresh/` als werkmap en een
timeout van 60 seconden. De kindsessie krijgt `LIGHTHOUSE_USAGE_REFRESH=1` in zijn omgeving, zodat
hooks kunnen zien dat het om een meting gaat en niet om echt werk.

## Alarmformaat

Eén regel: `<soort> <pct> <reset_local> <reset_at>`, afgesloten met een nieuwe regel.

```text
venster 99 14:30 2026-09-06T12:30:00Z
week 96 do_16:59 2026-09-10T14:59:00Z
```

- `soort` is `venster` (de sessie) of `week`. Staan beide op of boven hun drempel, dan komt alleen
  `venster`.
- Spaties in `reset_local` worden `_`. Een lege `reset_local` of `reset_at` wordt `?`.
- Het bestand verdwijnt zodra geen venster meer op of boven zijn drempel zit, of als `reset_at`
  verstreken is (dat ruimt `tick` op).

## Cacheschema

`usage-window.json` is het contract voor het lezen door andere onderdelen (#65).

```json
{
  "schema": 1,
  "generated_at": "2026-09-06T10:00:00Z",
  "session": {"pct": 99, "reset_at": "2026-09-06T12:30:00Z", "reset_local": "14:30"},
  "week": {"pct": 48, "reset_at": "2026-09-10T14:59:00Z", "reset_local": "do 16:59"}
}
```

- Tijdstempels zijn UTC in `2006-01-02T15:04:05Z`.
- `reset_local` staat in de lokale tijdzone: de sessie als `HH:MM`, de week als `<dag> HH:MM` met
  de dagen `zo ma di wo do vr za`.
- `reset_at` en `reset_local` zijn leeg (`""`) als het venster geen reset meldt.
- Een cache met onleesbare JSON, een ander schema of een kapotte `generated_at` telt als afwezig:
  de volgende verversing meet opnieuw.

## Instellingen

De tabel `[usage]` in `host.toml` (pad uit `LIGHTHOUSE_HOST_TOML`, anders
`~/.config/lighthouse/host.toml`):

```toml
[usage]
halt_pct_session = 85
halt_pct_week = 95
cache_ttl_seconds = 300
```

| Sleutel | Standaard | Geldig |
|---|---|---|
| `halt_pct_session` | 85 | 1 tot en met 100 |
| `halt_pct_week` | 95 | 1 tot en met 100 |
| `cache_ttl_seconds` | 300 | 0 of hoger |

Een waarde buiten het geldige bereik geeft voor die sleutel de standaardwaarde. Een bestand dat
niet als TOML te lezen is, geeft alle standaardwaarden.
