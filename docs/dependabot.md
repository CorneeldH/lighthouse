# Dependabot

`lighthouse init` zaait een `.github/dependabot.yml` met één blok per ecosysteem dat het in de repo
vindt. Het bestand is daarna van de repo: `init` en `init --upgrade` laten een bestaand bestand
staan (`dependabot: .github/dependabot.yml exists; left as it is`) en overschrijven het nooit, ook niet
met `--force`. Ook een bestaand `.github/dependabot.yaml` geldt als de config van de repo: daarnaast
zaait `init` niets, en de regel noemt dan dat pad.

## Welke ecosystemen

`init` kijkt naar de bestanden die git kent: de gevolgde bestanden en de nieuwe die niet in `.gitignore`
staan. De mappen die lighthouse zelf installeert (`.claude/`, `.agents/`, `.lighthouse/`) tellen niet
mee, en paden met `testdata/`, `node_modules/` of `vendor/` erin worden overgeslagen. Per ecosysteem met een manifest komt
één blok:

- `github-actions` in `/`, zodra er een eigen `.yml`- of `.yaml`-bestand onder `.github/workflows/` staat, of een
  `action.yml` of `action.yaml` in de root. De workflows die lighthouse installeert
  (`.github/workflows/lighthouse-*.yml`) tellen niet mee en staan in het blok onder `exclude-paths`:
  `init --upgrade` beheert die bestanden zelf.
- `gomod` per map met een `go.mod`.
- `npm` per map met een `package.json`.
- `uv` per map met een `uv.lock`.
- `pip` per map met een `requirements*.txt` (elk bestand dat met `requirements` begint), `Pipfile` of
  `setup.py`, en per map met een `pyproject.toml` zonder `uv.lock`.
- `docker` per map met een `Dockerfile` of `Dockerfile.*`.

Alle mappen van een ecosysteem staan onder één blok (`directories`).

Elk blok draait wekelijks en heeft één groep `all` die alle updates van het blok in één PR bundelt.

## Het bestand is van de repo

Pas cadans, groepen of ignores in het bestand zelf aan. Wil je opnieuw laten zaaien, verwijder het
bestand en draai `lighthouse init --upgrade`.

`exclude-paths` bij `github-actions` is gecontroleerd tegen de opties-referentie van Dependabot, die
geen beperking per ecosysteem noemt, maar niet tegen een draaiende Dependabot. Weigert Dependabot de
sleutel, haal dan het blok `exclude-paths` weg.

## Self-hosted workflows

Een workflow met `runs-on: self-hosted` waarin `dependabot[bot]` nergens voorkomt, stuurt ook een
Dependabot-PR naar de self-hosted runner. `init` waarschuwt daarvoor met
`runs on a self-hosted runner` en noemt de guard die je kunt toevoegen.

De waarschuwing heeft een grens:

- Alleen een `runs-on:`-regel met de letterlijke `self-hosted` telt, ook in een lijst tussen `[` en
  `]` of in een expressie die de letterlijke tekst bevat. De blokvorm (`runs-on:` en op de volgende
  regel `- self-hosted`), custom labels, runner-groepen en expressies zonder de letterlijke
  `self-hosted` worden niet herkend.
- Commentaar telt niet mee.
- De guard wordt per bestand gezocht: staat `dependabot[bot]` ergens in het bestand, dan geldt het
  bestand als bewaakt.

Voor lighthouse zelf borgt `TestOwnWorkflowsAreHosted` in
[`internal/initcmd/ownrepo_test.go`](../internal/initcmd/ownrepo_test.go) dat de eigen workflows
gehost zijn. Voor andere repos geeft alleen de waarschuwing dat inzicht.

## Detectie vóór `apply`

`init` leest de schijf voordat het schrijft. Alleen je eigen workflows tellen voor `github-actions`:
de workflows die lighthouse zelf installeert worden overgeslagen, want het blok sluit ze toch uit. Een
repo met alleen `lighthouse-*.yml` krijgt dus geen `github-actions`-blok; voeg je later een eigen
workflow toe, verwijder dan het bestand en laat het opnieuw zaaien, of voeg het blok met de hand toe.

## Bekende beperking: de changelog-job

Dependabot-PR's maken de `changelog`-job van CI rood, zolang die geen uitzondering voor
`dependabot/*` kent (`ci.yml`, regels 57-87): de job eist een fragment in `changelog.d/` dat een bot
niet schrijft. Dat is een wijziging in een workflow, voor de workflow-pusher, en valt buiten
issue 617. Een vervolg-issue legt dat vast.
