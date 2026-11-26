# Dependabot

`lighthouse init` zaait een `.github/dependabot.yml` met één blok per ecosysteem dat het in de repo
vindt. Het bestand is daarna van de repo: `init` en `init --upgrade` laten een bestaand bestand
staan (`dependabot: .github/dependabot.yml exists; left as it is`) en overschrijven het nooit, ook niet
met `--force`.

## Welke ecosystemen

`init` kijkt naar de bestanden die git kent (`git ls-files`, dus niets uit `.gitignore` en niets onder
`testdata/`) en maakt een blok voor elk ecosysteem met een manifest:

- `github-actions` in `/`, zodra er een workflow staat of `init` er een schrijft (`[ci]`).
  `.github/workflows/lighthouse-*.yml` valt buiten het blok (`exclude-paths`): `init --upgrade`
  beheert die bestanden zelf.
- `gomod`, `npm` en `uv` per map met een `go.mod`, `package.json` of `pyproject.toml`/`uv.lock`;
  in een monorepo staan alle mappen onder één blok (`directories`).
- `docker` per map met een `Dockerfile`.

Elk blok draait wekelijks en heeft één groep `all` die alle updates van het blok in één PR bundelt.

## Het bestand is van de repo

Pas cadans, groepen of ignores in het bestand zelf aan. Wil je opnieuw laten zaaien, verwijder het
bestand en draai `lighthouse init --upgrade`.

`exclude-paths` bij `github-actions` is gecontroleerd tegen de opties-referentie van Dependabot, die
geen beperking per ecosysteem noemt, maar niet tegen een draaiende Dependabot. Weigert Dependabot de
sleutel, haal dan het blok `exclude-paths` weg.

## Self-hosted workflows

Een Dependabot-PR draait niet met de secrets en runners van een gewone PR. Een workflow met
`runs-on: self-hosted` waarin `dependabot[bot]` nergens voorkomt, laat zo'n PR wachten op een runner
die hij niet kan gebruiken. `init` waarschuwt daarvoor met `runs on a self-hosted runner`.

De waarschuwing heeft een grens:

- Alleen de letterlijke `self-hosted` in een `runs-on:`-regel telt (ook in een lijst tussen
  `[` en `]`). De blokvorm (`runs-on:` en op de volgende regel `- self-hosted`), custom labels,
  runner-groepen en expressies worden niet herkend.
- Commentaar telt niet mee.
- De guard wordt per bestand gezocht: staat `dependabot[bot]` ergens in het bestand, dan geldt het
  bestand als bewaakt.

Voor lighthouse zelf borgt `TestOwnWorkflowsAreHosted` in
[`internal/initcmd/ownrepo_test.go`](../internal/initcmd/ownrepo_test.go) dat de eigen workflows
gehost zijn. Voor andere repos geeft alleen de waarschuwing dat inzicht.

## Detectie vóór `apply`

`init` leest de schijf voordat het schrijft. Een repo die nog geen workflow heeft en alleen `[ci]`
krijgt, ziet in de eerste run geen `github-actions`-blok, want de workflow bestaat dan nog niet. De
tweede `lighthouse init --upgrade` ziet hem wel; omdat het bestand dan al bestaat, voeg je dat blok
met de hand toe, of je verwijdert het bestand en laat het opnieuw zaaien.

## Bekende beperking: de changelog-job

Dependabot-PR's maken de `changelog`-job van CI rood, zolang die geen uitzondering voor
`dependabot/*` kent (`ci.yml`, regels 57-87): de job eist een fragment in `changelog.d/` dat een bot
niet schrijft. Dat is een wijziging in een workflow, voor de workflow-pusher, en valt buiten
[issue 617](https://github.com/CorneeldH/lighthouse/issues/617). Een vervolg-issue legt dat vast.
