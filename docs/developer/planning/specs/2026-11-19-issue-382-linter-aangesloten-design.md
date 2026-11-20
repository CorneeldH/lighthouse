# Ontwerp: per taalpack een check dat de linter echt is aangesloten

Datum: 2026-11-19 · Issue: [#382](https://github.com/CorneeldH/lighthouse/issues/382) · Milestone:
[Kwaliteitspoorten per taalpack](https://github.com/CorneeldH/lighthouse/milestone/13) (K2 in
[de milestone-spec](2026-11-16-zelflerende-lighthouse-design.md)) · Geblokkeerd door
[#381](https://github.com/CorneeldH/lighthouse/issues/381)
(spec `2026-11-19-canary-per-regel-design.md`, op branch `issue-381-canary-per-regel`)

## Aanleiding en doel

Een taalpack installeert een hook die een linter aanroept, maar niemand controleert of die linter in
de repo echt is aangesloten. In paia noemde de documentatie ruff, terwijl ruff niet gepind was, er
geen config was en geen CI-stap faalde op een bevinding
([paia #470](https://github.com/CorneeldH/personal_ai_agent/issues/470)). Agents meldden "ruff
ontbreekt" als bevinding; niets in de repo dwong het af.

Doel: per taalpack één check die drie dingen vaststelt, en elk ontbrekend onderdeel meldt:

1. **gepind**: de linterversie staat vast in de repo;
2. **selectie**: de linterconfig zet minstens de regels aan waarop de hook blokkeert;
3. **rood in CI**: een CI-stap draait die linter met die versie en die config, en faalt op een
   bevinding.

De regels die lokaal blokkeren, blokkeren dan ook in CI, op de paden die de CI-stap lint. De check
repareert niets in de gebruikersrepo.

## Doelen, niet-doelen, anti-doelen

**Doelen**

- Eén subcommand, `lighthouse lint check`, dat per geïnstalleerd taalpack de drie onderdelen
  controleert.
- `lighthouse init` en `init --upgrade` roepen het aan en tonen wat ontbreekt, zonder te stoppen.
- Lighthouse draait het in zijn eigen CI en haalt het.
- Per pack een Go-test: een nep-repo die alles heeft, en een die de CI-stap mist.

**Niet-doelen**

- Bevindingen van de linter in een gebruikersrepo wegwerken (no-go van de milestone).
- Bepalen of een selectie streng genoeg is. Welke regels aan moeten, is K4 en K7; deze check eist
  alleen de vloer die de hook al kent.
- De vloer zelf, `BLOCKING_RULES` in de hooks en hun canaries: dat is #381.
- Een CI-bestand of config repareren of genereren.
- Andere CI-systemen dan GitHub Actions en GitLab CI.

**Anti-doelen** (mislukt, ook als het technisch werkt)

- Een stille groene uitkomst: een lintstap die de check niet herkent, telt als ontbrekend, nooit
  als aanwezig.
- Een tweede, met de hand bijgehouden regellijst naast `BLOCKING_RULES`.
- Een uitweg in `lighthouse.toml` waarmee een repo beweert dat zijn CI lint, zonder dat de check
  het ziet.
- Een check die bij lighthouse zelf alleen slaagt doordat een config versoepeld is.

## Randvoorwaarden

- Go-code, geen Python (no-go van de milestone). Volgt `/lighthouse-go-standard`; tests volgen
  `/lighthouse-test-standard`.
- Nieuwe tests in Go, niet in pytest ([spec Repo zonder Python](2026-11-16-repo-zonder-python-design.md)).
- Hook-meldingen, uitvoer van het subcommand en instructies in het Engels; deze spec, de PR en het
  changelog-fragment in het Nederlands.
- Bouwt pas na de merge van #381: de vloer voor Go en shell bestaat pas dan.
- Een fragment in `changelog.d/`.

## Aanpakken

Alle keuzes hieronder zijn besluiten van de gebruiker, tenzij gemarkeerd als *(eigen keuze)*.

**Wat telt als "expliciete selectie".**

- a) Een config die een keuze maakt, ook als dat een preset is (`default: standard`,
  `js.configs.recommended`). Vangt het paia-geval, maar zegt niets over welke regels aan staan.
- b) Alleen een uitgeschreven lijst regelnamen. Lighthouse zelf zou dan falen tot K4 klaar is.
- **c) De config die CI gebruikt, zet minstens de vloer van de hook aan.** De vloer is
  `BLOCKING_RULES` uit de hook zelf, dezelfde bron als de canaries van #381. Packs zonder
  blokkerende linterhook (Node, Expo) vallen terug op a. Gekozen: koppelt K2 aan K1 en voegt geen
  tweede lijst toe.

**Waar de check draait.** Een eigen subcommand, aangeroepen door `init` als waarschuwing en door CI.
Geen Stop-hook: de check leest configbestanden, die zelden in een beurt veranderen *(eigen keuze,
voorgelegd en niet tegengesproken)*. De vorm volgt `lighthouse imports check` (`internal/importcheck`) uit
de spec van #383 (`2026-11-19-issue-383-directe-imports-design.md`, op branch
`issue-383-directe-imports-gedeclareerd`), zodat K2 en K3 dezelfde
conventie hebben.

**Hoe de check ziet dat CI rood wordt.**

- **Statisch**: de CI-bestanden lezen en de linteraanroep herkennen. Gekozen.
- *Dynamisch*: een canary-bevinding in een kopie van de repo en de CI-stap lokaal draaien.
  Afgewezen: CI-stappen hangen af van runners, secrets en acties die lokaal niet draaien.
- *Alleen de aanwezigheid van een lintstap*: afgewezen, mist `continue-on-error` en `|| true`.

**Een lintstap die de check niet herkent** (`make lint`, `just lint`, pre-commit, een eigen script).

- **a) Exit 1, "lint step not recognised".** Gekozen: nooit een stille groene uitkomst.
- b) Exit 1, tenzij `lighthouse.toml` de stap aanwijst. Afgewezen: een bewering die niemand
  controleert.
- c) Exit 3, kon niet controleren. Afgewezen: `init` en de testpoort laten 3 door, dus zo'n repo
  blijft voorgoed onbekend.

**De pin voor Python en shell.**

- Python: `.ruff-version` in de repo-root, die `ruff-format-check.sh` al leest. Niet de dev-groep in
  `pyproject.toml`, zoals het issue noemde.
- Shell: de pin van het pack, `SHELLCHECK_PIN` in `pack/shell/scripts/lib/shellcheck-bin.sh`, met
  zijn sha256-hashes. Geen `.shellcheck-version`: een repo die een andere versie kiest, heeft geen
  hash om de download tegen te controleren. De check eist dat CI dezelfde versie draait.

**Welke bestanden de CI-stap lint.**

- a) De CI-stap moet de hele repo dekken, net als de hook, die elk bewerkt bestand lint.
- **b) Het is genoeg dat de linter in CI draait; welke paden hij dekt, telt niet.** Gekozen: CI-CPU
  is beperkt en moet zo efficiënt mogelijk gebruikt worden. Een repo mag zijn lintstap smal houden
  (`ruff check src/`, `shellcheck-repo.sh` op de strikte set). De check eist het bestaan van de
  stap, niet zijn bereik.

**Configbestand of effectieve selectie.**

- **a) Er moet een configbestand zijn.** Gekozen: het issue vraagt "een linterconfig met een
  expliciete regelselectie", een bestand is ook lokaal zichtbaar, en defaults verschuiven per versie.
  Een default die toevallig de vloer dekt (golangci-lint v2 zonder config draait `standard`) en een
  `--select` in CI zonder bestand tellen niet. CI-flags tellen wel mee als ze de vloer uitzetten of
  versmallen.
- b) Wat CI effectief draait, configbestand en flags samen.

**Shell zonder `.shellcheckrc`** (besluit gebruiker na de spec-review, r1-f8). Uitzondering op de
vorige keuze: shellcheck kent geen regelselectie, zonder `.shellcheckrc` staan alle regels aan, en
de pin van het pack ligt vast, dus de default verschuift niet. Geen `.shellcheckrc` is goed; een
`.shellcheckrc` die er wel is, mag geen vloercode uitzetten.

**Volgorde met #381.** #382 wacht op #381 (blocked-by staat). Afgewezen: nu bouwen met een
terugval voor hooks zonder `BLOCKING_RULES` (code die later weg moet), en de regels zelf in de hooks
zetten (botst met #381).

## Ontwerp

### 1. `lighthouse lint check`

```
lighthouse lint check [--lang python|go|node|shell] [--dir <pad>]...
```

- Zonder `--lang` controleert het de taalpacks uit `.lighthouse/manifest.toml`. Met `--lang` alleen
  die taal. `--dir` wijst bij Node de map met de `package.json` aan. Zonder `--dir` controleert de
  check elke **workspace-root**: een gevolgde `package.json` die zelf geen workspace is van een
  `package.json` erboven (via `workspaces` in `package.json`, of `pnpm-workspace.yaml`), dezelfde
  regel als `imports check` in #383. Pin, config en CI-stap van de root dekken zijn workspaces
  *(eigen keuze na spec-review, r1-f7)*. Een `package.json` met een padsegment `testdata` is geen
  project maar een fixture, zoals de Go-toolchain `testdata/` overslaat; na #383 staan daar de
  Node-fixtures van `imports-gate-node` *(eigen keuze na spec-review, r2-f2)*.
- Expo heeft geen eigen taal: een Expo-repo krijgt ook `pack/node`, en de Node-check dekt hem
  *(eigen keuze, zoals #383)*.
- Uitvoer: één regel per ontbrekend onderdeel,
  `<bestand>[:<regel>]: <linter> — <wat ontbreekt>`. Voorbeelden:
  - `.golangci-version: golangci-lint — pin missing`
  - `.github/workflows/ci.yml:151: golangci-lint — CI version v2.14.0, pin v2.15.0`
  - `.golangci.yml: golangci-lint — floor rule errcheck disabled`
  - `.github/workflows: ruff — no CI step runs ruff check`
  - `.github/workflows/ci.yml:40: ruff — step has continue-on-error`
- Bij succes: één regel per linter, `<linter>: wired`, op stdout.
- Exitcodes: 0 alles aangesloten, 1 iets ontbreekt, 3 kon niet controleren
  (`lint check: COULD NOT CHECK — <reden>` op stderr). 3 is `gate.Unchecked`, net als bij
  `imports check`. Ontbrekend wint van 3, 3 wint van 0. Voor een gebruiksfout volgt de check de
  conventie van `imports check` *(eigen keuze na spec-review, r1-f9)*.
- Exit 3 alleen als de check de vloer of de CI niet kan lezen: een CI-bestand dat geen geldige YAML
  is, een onleesbaar manifest, een ontbrekende geïnstalleerde hook uit de tabel in §2, of zo'n hook
  zonder `BLOCKING_RULES`. Een ontbrekend CI-bestand, een ontbrekende pin of een ontbrekende config
  is exit 1 *(r1-f10)*.
- Het subcommand komt in `internal/cli`, met de logica in een eigen pakket onder `internal/`
  *(eigen keuze)*. `internal/checkinstr` en `internal/cli/checkinstructions.go` zijn het model voor
  vorm en uitvoer; `internal/pusher/policy.go` leest al workflow-YAML met `gopkg.in/yaml.v3`;
  `github.com/BurntSushi/toml` staat al in `go.mod`.

### 2. De vloer

De vloer is de waarde van `BLOCKING_RULES="…"` in de blokkerende linterhook van het pack:

| Pack | Hook | Vloer |
|---|---|---|
| Python | `ruff-format-check.sh` | de 8 ruff-codes, bestaand |
| Go | `go-lint-gate.sh` | `errcheck,govet,ineffassign,staticcheck,unused`, uit #381 |
| Shell | `shell-lint-check.sh` | SC-codes, uit #381 |
| Node, Expo | geen | geen vloer: een config die een keuze maakt volstaat |

De check leest de vloer uit de **geïnstalleerde** hook in de repo (onder `.claude/hooks/lighthouse/`),
niet uit de pack-kopie in de binary *(eigen keuze: wat in die repo blokkeert, is de geïnstalleerde
versie)*. Geen hook, of een hook zonder `BLOCKING_RULES`: exit 3.

Alleen de hook uit deze tabel levert de vloer. Andere hooks met `BLOCKING_RULES` (na #381 en #383
bijvoorbeeld `ty-gate`, `gofmt-check` en de `imports-gate-*`-hooks) tellen niet: hun IDs zijn geen
linterregels *(eigen keuze na spec-review, r1-f17)*.

Niet elke ID in `BLOCKING_RULES` is een linterregel: de spec van #381 noemt `typecheck` (Go, een
blokkeerreden van golangci-lint, geen linter) en een eigen ID voor de strikte shell-drempel. De
check eist alleen IDs die een linterregel zijn in de config. #381 moet dat onderscheid in de hook
machineleesbaar maken; het plan van #382 volgt het formaat dat #381 merget. Een uitzonderingslijst
in Go komt er niet: dat is de tweede lijst die de anti-doelen verbieden *(eigen keuze)*.

### 3. Per pack: wat telt

**Pin**

| Pack | De pin | Waar CI hem moet volgen |
|---|---|---|
| Python | `.ruff-version` | `uvx ruff@<pin>`, `ruff@"$(cat .ruff-version)"`, `pip install ruff==<pin>`, of `astral-sh/ruff-action` met `version: <pin>` |
| Go | `.golangci-version` | `golangci/golangci-lint-action` met `version: <pin>`, of een installatie van die versie |
| Node, Expo | `eslint` in `devDependencies` met een exacte versie (geen `^`, `~`, bereik of tag) | een installatie uit de lockfile vóór de lintstap: `npm ci`, `pnpm install --frozen-lockfile`, `yarn install --immutable` (of `--frozen-lockfile`) of `bun install --frozen-lockfile` *(r1-f14)* |
| Shell | `SHELLCHECK_PIN` uit de geïnstalleerde `.lighthouse/scripts/lib/shellcheck-bin.sh`, net als de vloer uit de geïnstalleerde kopie *(r1-f12)* | `.lighthouse/scripts/shellcheck-repo.sh` (gebruikt de pin zelf), of een installatie van die versie |

Een CI-stap die de linter zonder versie installeert (`pip install ruff`, `apt install shellcheck`),
meldt: CI-versie niet gepind.

**Selectie**

| Pack | Config | Telt als de vloer aanstaat |
|---|---|---|
| Python | `ruff.toml`, `.ruff.toml` of `[tool.ruff.lint]` in `pyproject.toml` | elke vloercode is effectief aan in ruff's eigen zin: geselecteerd via `select` of `extend-select` (exact of via prefix, zoals `B` voor `B006`, of `ALL`), en niet uitgezet via `ignore` of `extend-ignore`, met ruff's voorrang van de meest specifieke code (`select = ["PT012"]` met `ignore = ["PT"]` laat PT012 aan). Een `extend = "<pad>"` volgt de check naar dat bestand *(r1-f3)* |
| Go | `.golangci.yml` (of `.yaml`, `.toml`, `.json`) met `version: "2"` | elke vloerlinter zit in `linters.default` (`standard` of `all`) of `linters.enable`, en niet in `linters.disable`. Een config zonder `linters.default` maakt de vloer niet expliciet, ook al draait golangci-lint dan `standard` (besluit 2a); een v1-config meldt `config not version 2` *(eigen keuze na spec-review, r1-f13)* |
| Shell | `.shellcheckrc`, alleen als die er is: geen `.shellcheckrc` is goed (besluit gebruiker, r1-f8) | geen `disable=` van een vloercode, ook niet via `disable=all` of een codebereik (`SC2000-SC2999`); of `severity=` een geldige rc-sleutel is, bevestigt de bouwer *(r1-f16)* |
| Node, Expo | `eslint.config.{js,mjs,cjs,ts}` of `.eslintrc.*` | het bestand bevat een tekstueel teken van een keuze: een `rules`-sleutel, een `extends`, of een verwijzing naar een preset (`configs.`, `eslint-config-…`, `eslint-config-expo`). Een config met alleen `ignores` telt niet. De check parseert geen JavaScript: flat configs kiezen een preset op te veel manieren *(eigen keuze na spec-review, r1-f6)* |

Alleen een configbestand telt, behalve bij shell (zie boven). Een linterdefault die de vloer
toevallig dekt, of een selectie die alleen op de opdrachtregel in CI staat, telt niet (besluit
gebruiker). Ontbreekt het bestand, dan meldt de check `config missing`.

Een CI-stap die de vloer uitzet of versmalt, telt als niet aangesloten. Per linter
*(r1-f4: paden uitsluiten is bereik, en bereik telt niet, besluit 1b)*:

| Linter | Zet de vloer uit of versmalt | Telt niet mee (bereik) |
|---|---|---|
| ruff | `--ignore`, `--extend-ignore`, `--select` zonder een vloercode, `--config` naar een bestand dat de vloer mist | `--exclude`, `--extend-exclude`, paden |
| golangci-lint | `--disable`, `--enable-only`, `--default none`, `--config` naar een bestand dat de vloer mist | paden, `--skip-dirs`-achtige padfilters |
| shellcheck | `-e`/`--exclude` of `-i`/`--include` zonder een vloercode, `--severity` boven een vloercode, `SHELLCHECK_OPTS` in `env:` van workflow, job of stap met een van die flags | bestanden |
| eslint | `--no-eslintrc`/`--no-config-lookup` zonder eigen `--config`, `--config` naar een bestand zonder keuze | paden, `--ignore-pattern` |

Exit-zero-flags (`--exit-zero` bij ruff, `--issues-exit-code=0` bij golangci-lint) maken de stap
ongeldig via de regel voor exitstatus hieronder.

**Rood in CI**

- CI-bestanden: `.github/workflows/*.yml` en `*.yaml` bij `forge.kind = "github"`, `.gitlab-ci.yml`
  bij `"gitlab"`. Zonder `forge.kind` is de repo een GitHub-repo, zoals
  `internal/config/config.go:649-650` *(r1-f15)*.
- De stap draait bij een wijziging: in GitHub Actions een workflow met `push` of `pull_request` als
  trigger. Een herbruikbare workflow (`on: workflow_call`) telt als een lokale workflow met zo'n
  trigger hem aanroept (`uses: ./.github/workflows/<bestand>`), één niveau diep *(r1-f14)*. Een
  `if:` op job of stap is toegestaan, behalve letterlijk `false`: lintjobs staan vaak achter een
  padfilter, ook in lighthouse zelf *(eigen keuze; een voorwaarde verder ontleden valt buiten de
  check, r1-f1 afgewezen)*.
- In GitLab CI telt een job niet bij `allow_failure` met een ware waarde (ook de vorm met
  `exit_codes`), en niet bij `when: manual` of `when: never`, ook niet via `rules:` *(r1-f2)*.
- Herkende aanroepen: `golangci-lint run` of de golangci-lint-actie; `ruff check` of de ruff-actie;
  `shellcheck` of `shellcheck-repo.sh`; `eslint` of `expo lint`, ook via `npm run <script>` (de
  check leest dat script in `package.json`, één niveau diep), in de map van de `package.json`. Die
  map volgt uit `working-directory` op de stap, of uit `defaults.run.working-directory` op job of
  workflow *(r1-f11)*.
- Welke paden de stap lint, telt niet: een smalle stap (`ruff check src/`, `shellcheck-repo.sh` op
  de strikte set) is aangesloten (besluit gebruiker, CI-CPU is beperkt).
- **Exitstatus: een allowlist, geen denylist** *(r1-f5)*. Een herkende aanroep telt alleen als zijn
  exitstatus aantoonbaar de status van de stap is. Dat is zo als:
  - de stap draait in een shell die met errexit start. De effectieve shell is `shell:` op de stap,
    anders `defaults.run.shell` op de job, anders op de workflow, anders de standaard van de
    runner. Telt: `shell: bash`, `shell: sh`, of geen shell op een Linux- of macOS-runner (GitHub
    draait dan `bash -e {0}`), en GitLab's standaard-shell. Elke andere waarde, zoals `bash {0}`,
    `sh {0}` of `pwsh`, een Windows-runner zonder shell, of een `runs-on` die de check niet kan
    lezen (een expressie), is `step not recognised` *(r2-f1, r3-f1)*;
  - de aanroep een eigen regel is of met `&&` aan de rest hangt, er na de aanroep geen `|`, `||`
    of `;`-keten volgt die zijn status kan maskeren (een pipe alleen met `set -o pipefail` of
    `shell: bash`, die pipefail zet), en er geen `set +e` voor staat;
  - `continue-on-error` op stap **en** job ontbreekt of letterlijk `false` is *(r2-f1)*.

  Elke andere vorm, ook `continue-on-error: ${{ … }}`, is `step not recognised`, in lijn met keuze
  a voor niet herkende stappen. Een actie (`uses:`) telt zonder `continue-on-error` op stap of job
  en zonder exit-zero-input.
- Geen herkende stap: `no CI step runs <linter>` (exit 1). Een ratel alleen (`ratchet.py`) telt niet
  als lintstap.

### 4. Aansluiting

- **`init` en `init --upgrade`** draaien de check na de installatie, in-process, en tonen de
  regels van wat ontbreekt onder een kop. Exit 1 en 3 laten `init` niet falen, zoals `deps.Check`.
  Bij een volledig aangesloten repo print `init` niets extra: de CI-stap "installed pack/core matches
  pack/" eist exact `No changes.` (`.github/workflows/ci.yml:165`).
- **CI van lighthouse** krijgt een kleine eigen job met `go run ./cmd/lighthouse lint check`, zonder
  padfilter: de Go-job draait niet als alleen `web/` verandert (`ci/domains.py`), en juist een
  wijziging aan `web/package.json` of `web/eslint.config.js` moet de check raken *(eigen keuze, uit
  de builder-check)*.
- **Documentatie**: `docs/lint-check.md`, naast `docs/test-gate.md` en `docs/pins.md` *(eigen
  keuze)*: wat de check eist per pack, de herkende aanroepen, en dat een niet-herkende stap exit 1
  geeft.

### 5. Lighthouse zelf

Lighthouse installeert `core`, `go` en `node`. Voor de stand van nu, plus de vloer uit #381:

- Go: `.golangci-version` is `v2.15.0`, de actie in `ci.yml:151-153` heeft `version: v2.15.0`, en
  `.golangci.yml` heeft `default: standard`, wat precies de Go-vloer uit #381 is.
- Node: `eslint` staat op `10.13.0` (exact), `web/eslint.config.js` heeft `extends`, en CI draait
  `npm ci` en `npm run lint` (`eslint .`) in `web/`.

Lighthouse zou dus moeten slagen zonder configwijziging. Faalt het toch, dan lost deze PR het op
door aan te sluiten, nooit door de config te versoepelen.

## Tests

Per pack een Go-test met een nep-repo in `t.TempDir()`:

| Pack | Slaagt | Faalt (minstens) |
|---|---|---|
| Python | pin, ruff-config met de vloer, CI-stap met `ruff@<pin> check` | zonder CI-stap; met een vloercode in `ignore`; met een vloercode in `extend-ignore` |
| Go | pin, `.golangci.yml` met `default: standard`, golangci-lint-actie met de pin | zonder CI-stap; CI-versie ≠ pin; config zonder `linters.default` |
| Node | exacte `eslint`, `eslint.config.js`, `npm ci` en `npm run lint` met `defaults.run.working-directory` | zonder CI-stap; `eslint` met `^`; een config met alleen `ignores` |
| Node, workspace | root met `workspaces`, eslint en lintstap alleen in de root | — (een child-`package.json` krijgt geen eigen melding) |
| Node, fixture | een kale `testdata/x/package.json` naast een aangesloten root | — (de fixture krijgt geen melding) |
| Expo | idem, met `expo` in `package.json` en `"lint": "expo lint"` | zonder CI-stap |
| Shell | geen `.shellcheckrc`, CI met `shellcheck-repo.sh`; ook `.shellcheckrc` zonder vloer-disable | zonder CI-stap; `.shellcheckrc` met `disable=` van een vloercode; `SHELLCHECK_OPTS: -e <vloercode>` |
| GitLab | `.gitlab-ci.yml` met een lintjob | dezelfde job met `allow_failure: true`; met `when: manual` |

Plus:

- `continue-on-error: true` op stap of job, `|| true`, `| tee` zonder pipefail, `set +e`,
  `shell: bash {0}` (ook via `defaults.run.shell`), `runs-on: windows-latest` zonder shell en
  `--exit-zero` maken een herkende stap ongeldig;
  `ruff check --exclude migrations/` blijft geldig.
- `make lint` alleen geeft `no CI step runs …` en exit 1.
- Go zonder `.golangci.yml`, met een verder volledige opzet, geeft `config missing` en exit 1.
- Een smallere selectie op de opdrachtregel in CI (`ruff check --select E9`) maakt de stap ongeldig.
- Een stap met een padfilter-`if:` telt; een stap met `if: false` niet.
- Exit 3 bij ongeldige YAML; 1 wint van 3, met beide meldingen in de uitvoer.
- De vloer komt uit de hook: een nep-hook met een extra code in `BLOCKING_RULES` laat een config
  zonder die code falen.
- `init` print niets extra bij een aangesloten repo.

AC 5 bewijst de CI-stap van lighthouse zelf.

## Wat aan de bouwer is

- Hoe de vloercodes van shellcheck aan een ernst gekoppeld worden, voor de `severity`-regel.
- Het precieze patroon per herkende aanroep, en hoe ver de check een shell-regel in een `run:`
  ontleedt.
- Hoe de check bij GitLab "draait bij een wijziging" vaststelt (`rules`, `only`).
- De naam en indeling van het pakket onder `internal/`.
- Hoe de taalset uit `.lighthouse/manifest.toml` volgt; deel de code (taalset, workspace-roots) met
  `imports check` als die er dan al is.

## Open na review

Ronde 3 haalde de rondegrens met deze punten open. Ze zijn aanvaard als `accepted_open`;
de volgende fase of de PR-review weegt ze opnieuw.

- **r3-f1** (MAJOR, §3 Rood in CI — Exitstatus: een allowlist, geen denylist, first sub-bullet): The errexit rule reads the shell only from the step's own `shell:` key. "geen `shell:`" counts as `bash -e {0}`, but GitHub takes the shell from `defaults.run.shell` on the job or workflow first, and on a Windows runner the default shell is `pwsh`, not `bash -e`. The spec resolves `working-directory` from `defaults.run` on job or workflow (r1-f11), but it does not do the same for `shell`. So the masking that r2-f1 named still passes when the non-errexit shell comes from defaults or from the runner OS. Job-level `continue-on-error` is restored correctly.
