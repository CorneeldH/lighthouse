# Lint check: is de linter echt aangesloten

Een linterhook blokkeert lokaal op een vaste set regels, de **vloer**. Dat helpt alleen als
dezelfde linter, op dezelfde versie en met die vloer aan, ook in CI draait en CI rood maakt.
`lighthouse lint check` controleert dat per taalpack: de pin, de config en de CI-stap.

```
lighthouse lint check [--lang python|go|node|shell]... [--dir <pad>]...
```

- Zonder `--lang` controleert de check de taalpacks uit `.lighthouse/manifest.toml`. Met `--lang`
  alleen die taal; een taal die niet geïnstalleerd is, is exit 3.
- `--dir` wijst bij Node de map met de `package.json` aan. Zonder `--dir` controleert de check
  elke workspace-root: een gevolgde `package.json` die geen workspace is van een `package.json`
  erboven. Een `package.json` onder een map `testdata` is een fixture en telt niet.
- Expo heeft geen eigen taal: een Expo-repo heeft ook `pack/node`, en de Node-check dekt hem.

`lighthouse init` en `init --upgrade` draaien de check na de installatie. Bij een aangesloten
repo printen ze niets extra. Bij exit 1 volgen de regels onder de kop
`lint check: some linters are not wired`, bij exit 3 onder `lint check: could not check`. `init`
faalt er nooit op. Lighthouse draait de check
zelf in de job `lint-check` van [`ci.yml`](../.github/workflows/ci.yml), op elke PR.

## Uitvoer en exitcodes

Bij succes één regel per linter op stdout: `<linter>: wired`. Bij een Node-root die niet de root
van de repo is, noemt de regel de map: `eslint: wired (web)`; een ontbrekende CI-stap daar ook:
`no CI step runs eslint in web`. Een linter met een ontbrekend onderdeel in een map heet daar nooit
`wired`. Anders één regel per ontbrekend onderdeel:

```
<bestand>[:<regel>]: <linter> — <wat ontbreekt>
```

Bijvoorbeeld:

```
.golangci-version: golangci-lint — pin missing
.github/workflows/ci.yml:151: golangci-lint — CI version v2.14.0, pin v2.15.0
.golangci.yml: golangci-lint — floor rule errcheck disabled
.github/workflows: ruff — no CI step runs ruff check
.github/workflows/ci.yml:40: ruff — step not recognised: || masks the exit status
```

| Exit | Betekenis |
|---|---|
| 0 | elke linter is aangesloten |
| 1 | er ontbreekt iets: een pin, een config, een CI-stap, of een stap die de check niet herkent |
| 3 | de check kon niet controleren: `lint check: COULD NOT CHECK — <reden>` op stderr |

Exit 3 komt alleen als de check de vloer of de CI niet kan lezen: een CI-bestand dat geen
geldige YAML is, een onleesbaar manifest of config, een ontbrekende geïnstalleerde linterhook of
zo'n hook zonder `BLOCKING_RULES`, of een gebruiksfout. Een ontbrekend CI-bestand, pin of config is
exit 1. Ontbrekend wint van 3, en 3 wint van 0.

Er is geen uitweg: geen sleutel in `lighthouse.toml` die een linter of regel uitzondert.

## De vloer

De vloer is de waarde van `BLOCKING_RULES` in de **geïnstalleerde** linterhook onder
`.claude/hooks/lighthouse/`, niet in de kopie in de binary: wat in de repo blokkeert, telt.

| Pack | Hook | Vloer |
|---|---|---|
| Python | `ruff-format-check.sh` | de ruff-codes uit `BLOCKING_RULES` |
| Go | `go-lint-gate.sh` | de golangci-linters uit `BLOCKING_RULES` |
| Shell | `shell-lint-check.sh` | de SC-codes uit `BLOCKING_RULES` |
| Node, Expo | geen | geen vloer: een config die een keuze maakt volstaat |

IDs die de hook als geen linterregel aanmerkt (zoals `typecheck` bij Go of de strikte
shell-drempel), eist de check niet in de config. Andere hooks met `BLOCKING_RULES` tellen niet.

## Per pack: wat de check eist

### Pin

| Pack | De pin | Hoe CI hem volgt |
|---|---|---|
| Python | `.ruff-version` | `uvx ruff@<pin>`, `ruff@"$(cat .ruff-version)"`, `pip install ruff==<pin>`, of `astral-sh/ruff-action` met `version: <pin>` |
| Go | `.golangci-version` | `golangci/golangci-lint-action` met `version: <pin>`, of een installatie van die versie |
| Node, Expo | `eslint` in `devDependencies`, met een exacte versie (geen `^`, `~`, bereik of tag) | een installatie uit de lockfile vóór de lintstap, in dezelfde job |
| Shell | `SHELLCHECK_PIN` in de geïnstalleerde `.lighthouse/scripts/lib/shellcheck-bin.sh` | `.lighthouse/scripts/shellcheck-repo.sh` (gebruikt de pin zelf), of een installatie van die versie |

Een CI-stap die de linter zonder versie installeert (`pip install ruff`, `apt install shellcheck`)
of op een andere versie, geeft `CI version not pinned` of `CI version <x>, pin <y>`.

Alleen een installatie vóór de lintaanroep telt: in een eerdere stap van dezelfde job, of in
dezelfde stap op een eerdere regel of eerder in de `&&`-keten. Een installatie na de aanroep, ook
in dezelfde stap (`ruff check && pip install ruff==0.9.0`), bewijst de pin niet en bepaalt ook
niet de versie die CI draait. De aanroep zelf mag de versie dragen (`uvx ruff@0.9.0 check`).

Bij Node betekent `CI version not pinned`: in dezelfde job gaat geen installatie uit de lockfile
aan de lintstap vooraf. Die installatie is `npm ci`, `pnpm install --frozen-lockfile`,
`yarn install --immutable` (of `--frozen-lockfile`) of `bun install --frozen-lockfile`. Een
gewone `npm install` telt niet: die mag een andere eslint kiezen dan de lockfile.

### Config: de selectie dekt de vloer

| Pack | Config | Wanneer de vloer aanstaat |
|---|---|---|
| Python | `ruff.toml`, `.ruff.toml` of `[tool.ruff.lint]` in `pyproject.toml` | elke vloercode is aan in ruff's eigen zin: geselecteerd via `select` of `extend-select` (exact, via prefix, of `ALL`) en niet uitgezet via `ignore` of `extend-ignore`, met ruff's voorrang van de meest specifieke code. Een `extend = "<pad>"` volgt de check |
| Go | `.golangci.yml` (of `.yaml`, `.toml`, `.json`) met `version: "2"` | elke vloerlinter zit in `linters.default` (`standard` of `all`) of `linters.enable`, en niet in `linters.disable`. Zonder `linters.default` is de vloer niet expliciet; een v1-config geeft `config not version 2` |
| Shell | `.shellcheckrc` (of `shellcheckrc`) in de root van de repo, alleen als die er is: geen rc-bestand is goed | geen `disable=` van een vloercode, ook niet via `disable=all` of een codebereik |
| Node, Expo | `eslint.config.{js,mjs,cjs,ts}` of `.eslintrc.*` | het bestand maakt een keuze: een `rules`-sleutel, een `extends` of een verwijzing naar een preset. Alleen `ignores` telt niet. De check parseert geen JavaScript; commentaar buiten strings telt niet mee |

Alleen het configbestand telt. Een linterdefault die de vloer toevallig dekt, of een selectie die
alleen op de opdrachtregel in CI staat, telt niet. Ontbreekt het bestand, dan meldt de check
`config missing`.

Bij shell leest de check alleen het rc-bestand in de root. Een `.shellcheckrc` in een submap,
waar shellcheck voor bestanden in die map ook naar kijkt, ziet de check niet. `severity=` is geen
geldige sleutel in een rc-bestand en telt dus niet als versmalling; `--severity` in CI wel.

### Paden tellen niet

Welke bestanden de linter in CI ziet, telt niet: alleen de regelselectie. Een smalle stap
(`ruff check src/`, `shellcheck-repo.sh` op de strikte set) is aangesloten; CI-tijd is beperkt.
Zo tellen ook niet mee: `--exclude`, `--extend-exclude`, `per-file-ignores`, `exclusions` in
`.golangci.yml`, `--skip-dirs`-achtige padfilters en `--ignore-pattern`.

Wat wel telt, is een vlag die de vloer uitzet of versmalt:

| Linter | Zet de vloer uit of versmalt |
|---|---|
| ruff | `--ignore`, `--extend-ignore`, `--select` zonder een vloercode, `--isolated`, `--config` naar een bestand dat de vloer mist |
| golangci-lint | `--disable`, `--enable-only`, `--default none`, `--config` naar een bestand dat de vloer mist |
| shellcheck | `-e`/`--exclude` of `-i`/`--include` zonder een vloercode, `--severity` boven een vloercode, `SHELLCHECK_OPTS` in `env:` van workflow, job of stap met een van die vlaggen |
| eslint | `--no-eslintrc`/`--no-config-lookup` zonder eigen `--config`, `--config` naar een bestand zonder keuze |

## De CI-stap

### Welke CI-bestanden

Bij `forge.kind = "github"` (of zonder `forge.kind`) leest de check `.github/workflows/*.yml` en
`*.yaml`, bij `"gitlab"` `.gitlab-ci.yml`.

- GitHub: de workflow heeft `push`, `pull_request`, `pull_request_target` of `merge_group` als
  trigger. Een herbruikbare workflow (`on: workflow_call`) telt als een lokale workflow met zo'n
  trigger hem aanroept (`uses: ./.github/workflows/<bestand>`), één niveau diep, en de aanroepende
  job niet letterlijk `if: false` heeft. Een `if:` op job of stap mag, behalve letterlijk `false`.
- GitLab: een job telt niet bij `allow_failure` met een ware waarde (ook de vorm met
  `exit_codes`), op de job of in een regel van `rules:`, en niet bij `when: manual` of
  `when: never`, ook niet via `rules:`. De check volgt `extends:` binnen `.gitlab-ci.yml` en voegt
  de sjablonen samen zoals GitLab dat doet, ook hun `allow_failure`, `when` en `rules`. Een
  `extends:` naar een job die niet in het bestand staat, geeft `step not recognised`. `include:`
  volgt de check niet: een lintjob die alleen in een ingevoegd bestand bestaat, vindt hij niet, en
  dat is exit 1. De `before_script` uit `default:` telt alleen als de job hem erft: niet bij
  `inherit: default: false` of een `inherit: default:`-lijst zonder `before_script`.
- Beide: YAML-ankers, aliassen (`*naam`) en merge-sleutels (`<<:`) leest de check zoals YAML ze
  bedoelt.

### Herkende aanroepen

| Linter | Herkend als |
|---|---|
| ruff | `ruff check`, of de actie `astral-sh/ruff-action` |
| golangci-lint | `golangci-lint run`, of de actie `golangci/golangci-lint-action` |
| shellcheck | `shellcheck`, of `.lighthouse/scripts/shellcheck-repo.sh` |
| eslint | `eslint` of `expo lint`, ook via `npm run <script>`, `npm test`, `pnpm [run] <script>`, `yarn [run] <script>` of `bun run <script>`: de check leest dat script in `package.json`, één niveau diep |

Wrappers die alleen een ander commando met zijn exitstatus draaien, ziet de check door: `env`,
`sudo`, `time`, `timeout`, `uvx`, `uv run`, `npx`, `pnpm exec`, `yarn exec`, `bunx`,
`poetry run`, `python -m`, `go run` en een shell die een scriptbestand draait. De map van de
aanroep volgt uit `working-directory` op de stap, `defaults.run.working-directory` op job of
workflow, een `cd` in het script en `--prefix`, `-C`, `--dir` of `--cwd` van de package manager.

Een aanroep die alleen informatie print (`--help`, `--version`, `--show-settings`,
`--print-config` en dergelijke) is geen lintstap. Een script dat de bevindingen alleen telt of met
een eerdere stand vergelijkt (een ratel, zoals `ratchet.py`) is ook geen lintstap: CI moet op elke
bevinding rood worden, niet alleen op nieuwe. Daarom telt ook een aanroep met een ratelvlag niet:
`only-new-issues` op `golangci/golangci-lint-action` en `--new`, `--new-from-rev`,
`--new-from-merge-base` en `--new-from-patch` bij golangci-lint.

Geen herkende stap: `no CI step runs <linter>` (exit 1). Bij meerdere aanroepen is de linter
aangesloten als één ervan geldig is; anders meldt de check de eerste.

### Rood in CI: een allowlist

Een herkende aanroep telt alleen als zijn exitstatus aantoonbaar de status van de stap is. Elke
andere vorm is exit 1, nooit groen. Meestal luidt de melding `step not recognised: <reden>`; een
stap of job die mag falen, noemt de oorzaak zelf: `step has continue-on-error`,
`job has continue-on-error`, `job has allow_failure` of
`job does not run on every change (when, rules or only)`. Een aanroep telt als:

- de stap draait in een shell die met errexit start. De effectieve shell is `shell:` op de stap,
  anders `defaults.run.shell` op job of workflow, anders de standaard van de runner. Telt:
  `shell: bash`, `shell: sh`, geen shell op een Linux- of macOS-runner, en de standaardshell van
  GitLab. `bash {0}`, `sh {0}`, `pwsh`, een Windows-runner zonder shell of een `runs-on` die een
  expressie is, telt niet;
- de aanroep een eigen regel is, of in een `&&`-keten staat als laatste lid van die keten, of in
  een keten die de laatste regel van het script is. Bash negeert errexit voor elk lid van een
  `&&`-keten behalve het laatste: als laatste lid stopt een falende aanroep het script, dus mogen
  er nog regels volgen (`cd src && ruff check .`). Een eerder lid (`ruff check . && echo ok`)
  telt alleen als de keten de laatste regel is, want dan is de status van de keten de status van
  het script. Een `||` in de keten telt nooit;
- er na de aanroep geen `|`, `||` of `;` op dezelfde regel volgt. Een pipe alleen met
  `set -o pipefail` of `shell: bash` (die pipefail zet);
- er geen `set +e` voor staat, en de aanroep niet in een `if`, lus, `{ … }`-groep, achter `!` of
  op de achtergrond (`&`) draait;
- er nergens in het script een `trap` staat: een handler als `trap 'exit 0' EXIT` vervangt de
  exitstatus;
- `continue-on-error` op stap en job ontbreekt of letterlijk `false` is. Ook
  `continue-on-error: ${{ … }}` telt niet.

In een `package.json`-script, dat npm met `sh -c` zonder errexit draait, telt de aanroep alleen
als laatste commando.

Vlaggen die bevindingen uit de exitstatus halen, maken de stap ook niet herkend: `--exit-zero`,
`-e`, `--fix`, `--fix-only`, `--diff`, `--add-noqa` en `--watch` (`-w`) bij ruff,
`--issues-exit-code=0` en de ratelvlaggen `--new` (`-n`), `--new-from-rev`, `--new-from-merge-base`
en `--new-from-patch` bij golangci-lint, en `--fix` bij eslint. Een actie (`uses:`) telt zonder
`continue-on-error`, zonder zo'n vlag in `args:` en, bij `golangci/golangci-lint-action`, zonder
`only-new-issues` (alleen letterlijk `false` mag).
