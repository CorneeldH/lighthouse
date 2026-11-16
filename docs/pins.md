# Pins: wat lighthouse overneemt en of het nog klopt

Lighthouse neemt dingen over van andere repositories en tools. Een **pin** legt vast van welke
versie dat is, zodat achterstand zichtbaar is en een update een reviewbare diff wordt. Dit
document beschrijft de vier soorten pins, de meting (`lighthouse pins check`), het lokale signaal
bij `lighthouse init`, de wekelijkse lane en hoe je een pin ophoogt.

## Wat een pin is

| Soort | Wat | Waar de pin staat | Sleutel |
|---|---|---|---|
| skill | een skillmap onder `pack/*/skills/` die van elders komt | frontmatter van `SKILL.md`: `metadata.origin` (`extended` of `external`), `upstream`, `upstream-commit` (volledige SHA, tussen aanhalingstekens) en bij `external` `upstream-path` | de skillmap relatief aan `pack/`, bijvoorbeeld `core/skills/test-standard` |
| bestand | een overgenomen bestand in `pack/` | een `[[file]]`-entry in [`pack/SOURCES.toml`](../pack/SOURCES.toml) met `source_repo`, `source_sha` en `source_path` | het `path` uit `SOURCES.toml` |
| plugin | een Claude Code-plugin waar lighthouse op rekent | `[pins.<naam>]` in [`lighthouse.toml`](../lighthouse.toml) met `kind = "plugin"`, `repo` en `version` | de naam in `[pins.<naam>]` |
| tool | een commando op het pad, zoals `roborev` of `codex` | `[pins.<naam>]` met `kind = "tool"`, `repo`, `version` en optioneel `command`, `version_args` en `tag_prefix` | de naam in `[pins.<naam>]` |

Skillpins staan alleen onder `pack/*/skills/`, nooit onder `.claude/skills/` of `.agents/skills/`:
dat zijn de geïnstalleerde kopieën. De varianten `claude/` en `agents/` van één skillmap vormen
één pin. Een `pin = "<sleutel>"`-entry in `SOURCES.toml` hoort bij die skillpin en telt niet
dubbel. Welke velden een skill moet dragen, staat in
[`pack/core/skills/place-instruction/SKILL.md`](../pack/core/skills/place-instruction/SKILL.md).

Voorbeeld van een plugin- en een toolpin:

```toml
[pins.superpowers]
kind = "plugin"
repo = "obra/superpowers"
version = "7.0.2"

[pins.codex]
kind = "tool"
repo = "openai/codex"
version = "0.157.0"
tag_prefix = "rust-v"   # standaard "v"
```

Een lighthouse.toml zonder `[pins]` heeft geen plugin- of toolpins. Een pin zonder `version` is
een `error`.

## Meten: `lighthouse pins check`

```text
lighthouse pins check [--upstream] [--local] [--json]
```

Het commando draait in een git-repository en leest `lighthouse.toml` en `pack/` van die root.

| Vlag | Betekenis |
|---|---|
| `--upstream` | meet tegen upstream (netwerk) |
| `--local` | meet tegen wat hier is geïnstalleerd (geen netwerk) |
| `--json` | JSON in plaats van tekst |

Zonder `--upstream` en `--local` lopen beide.

**Wat elke soort meet.**

- Skill en bestand: alleen `upstream`. Is de HEAD van de remote nog dezelfde commit als de pin,
  of is het pad sindsdien gewijzigd? De meting haalt een kale partial clone
  (`--filter=blob:none`) en voert geen upstream-code uit: geen checkout, geen hooks, geen
  submodules. De default branch is de `HEAD` van de remote, geen vaste naam.
- Plugin en tool, `upstream`: de nieuwste release van `repo`, vergeleken met `version`. Alleen
  releases zonder draft en prerelease tellen, met een tag van `tag_prefix` plus een semver.
- Plugin, `lokaal`: de versie in Claude Codes `installed_plugins.json`, voor deze werkmap.
- Tool, `lokaal`: de uitvoer van `<command> <version_args>` (met een time-out van 10 seconden).

**Statussen.** `current`, `behind` en `error`. Alleen de lokale meting kent ook `ahead`: hier
is een nieuwere versie geïnstalleerd dan de pin. Een pin die niet te meten is, is altijd `error`,
nooit `current`. De uitslagen van `upstream` en `lokaal` worden nooit samengevoegd.

**Uitvoer.** Eén regel per pin per meting, op volgorde van sleutel:

```text
<sleutel> upstream <status> <detail>
<sleutel> lokaal <status> <detail>
```

Met `--json` volgt één array met per pin `key`, `kind` en `upstream` en `local`, elk met
`status` en `detail`. Een meting die niet liep of niet van toepassing is, ontbreekt.

**Exitcode.** 1 zodra één uitslag `error` is, anders 0. `behind` en `ahead` zijn geen fout.

## Het lokale signaal bij `lighthouse init`

`lighthouse init` meet de plugin- en toolpins lokaal (zonder netwerk) en meldt elke afwijking
op **stderr**:

```text
pins: local installation differs from lighthouse.toml:
<sleutel> lokaal behind <detail>
```

De melding stopt niets. Exitcode en stdout blijven gelijk, dus `init --upgrade` zonder
wijzigingen schrijft nog steeds precies `No changes.` op stdout. De controller (tick, dispatch)
doet niets met pins.

## De lane: wekelijks, nooit mergen

`lighthouse pins lane` meet elke pin tegen upstream en schrijft wat de uitslag vraagt. De workflow
[`.github/workflows/pins.yml`](../.github/workflows/pins.yml) start de lane elke maandag om
05:17 UTC en op verzoek (`workflow_dispatch`), alleen vanaf de default branch. De lane roept
nooit een merge aan.

| Uitslag | Wat de lane schrijft |
|---|---|
| skill of bestand achter, 3-way-merge schoon | een **draft-PR** op de rollende branch `pins/<sleutel>`, titel `pins: <sleutel> volgt upstream` |
| skill of bestand achter, merge conflicteert (of raakt `.github/workflows/`) | een **issue**, titel `pins: <sleutel> botst met upstream` |
| plugin of tool achter | een **issue**, titel `pins: <naam> <versie> staat achter upstream` |
| pin niet te meten | een **issue**, titel `pins: <sleutel> is niet te meten`; de run eindigt rood |

De draft-PR heeft een body die met `No issue:` begint, heeft geen `Closes`, geen board-item en
geen milestone. Hij bevat een diffstat, de upstream-commits en een vergelijklink. De branch
bevat één commit van `github-actions[bot]` die de wijziging overneemt, `upstream-commit` (of
`source_sha`) ophoogt, `pack/SOURCES.toml` bijwerkt, een fragment in `changelog.d/` schrijft en
`init --upgrade` draait zodat de geïnstalleerde kopieën volgen. De branch wordt bij elke run
bijgewerkt, ook na een eerdere half mislukte run; er komt nooit een tweede PR.

Upstream-tekst (releasenotes, commitberichten) staat in een body altijd in een afgeschermd
codeblok, zodat er geen vermeldingen, issueverwijzingen of sluitwoorden uit werken. Lange
teksten worden afgekapt met een verwijzing naar de vergelijklink.

### Geen dubbel werk

Per pin loopt de lane op twee sporen. Elk spoor heeft hoogstens één open item.

- **Achterstandspoor**: draft-PR, conflict-issue of behind-issue. Staat er al een open item van
  dezelfde soort, dan werkt de lane het bij als de doelversie veranderd is, en laat het anders
  met rust. Past het open item niet meer (een conflict dat een schone merge werd), dan sluit de
  lane het en maakt een nieuw item.
- **Foutspoor**: het issue voor een pin die niet te meten is. Wordt de pin weer meetbaar, dan
  sluit de lane dat issue zelf.
- Sluit jij een item voor een bepaalde doelversie (PR of issue, zonder dat de lane het deed),
  dan maakt de lane voor die doelversie geen nieuw item. Pas bij een nieuwere versie komt er
  weer een. Zo is "dit nemen we bewust niet over" een geldig antwoord.
- Een open item blijft staan als de pin intussen weer `current` is: dat sluit jij.
- Een pin die faalt stopt de andere pins niet. Het commando eindigt met exit 1 en noemt per
  mislukte pin de oorzaak.

### Eenmalig: de rechten van de GitHub App

De lane schrijft met de GitHub App uit `[ci]` in `lighthouse.toml`. De workflow vraagt een token
met `contents`, `pull-requests` en `issues` op write; `GITHUB_TOKEN` krijgt niets. Controleer
vóór de eerste echte run met de hand, want de workflow kan dit niet voor zichzelf nagaan:

1. De App heeft in de installatie voor deze repository `Contents`, `Pull requests` en `Issues`
   op *Read and write*.
2. Een push naar `pins/*` mag: geen branch-ruleset of beveiligde-branchregel blokkeert de
   App voor `pins/*`.
3. Start de workflow één keer met `workflow_dispatch` en lees het logboek: een weigering van de
   push komt als `error`-issue voor de betreffende pin terug.

De App heeft de `workflows`-permissie niet; een wijziging die `.github/workflows/` raakt wordt
daarom een conflict-issue, geen PR.

## Een pin ophogen

### Skill of bestand: een draft-PR van de lane

1. Open de draft-PR `pins: <sleutel> volgt upstream`. Lees de diffstat en de upstream-commits en
   open de vergelijklink bij twijfel.
2. Lees de diff zelf. De lane heeft de upstream-wijziging 3-way gemerged met onze lokale
   aanpassingen; of die samen nog kloppen, beoordeelt alleen jij.
3. Pas aan op de branch `pins/<sleutel>` waar nodig, met nieuwe commits. Werk je lokaal, haal
   dan eerst de branch binnen; de lane forceert de branch pas bij een volgende run, en alleen
   als er een nieuwe doelversie is.
4. Zet de PR op *ready for review*, laat CI groen worden en merge hem zelf. De lane merget
   nooit.
5. Wil je het niet overnemen: sluit de PR. Voor die upstream-commit komt er dan geen nieuwe.

**Bij een conflict** (issue `pins: <sleutel> botst met upstream`): neem de wijziging met de hand
over in een gewone branch en PR. Zet daarbij `upstream-commit` (skill) of `source_sha` (bestand)
op de HEAD die het issue noemt, dus de laatste commit uit de lijst met upstream-commits en niet
op wat je toevallig lokaal hebt. Zonder die stap meet de volgende run dezelfde achterstand
opnieuw. Werk `pack/SOURCES.toml` bij als er bestanden bijkwamen of verdwenen, draai
`go run ./cmd/lighthouse init --upgrade` en voeg een fragment toe aan `changelog.d/`.

### Plugin of tool: een issue

1. Lees de releasenotes in het issue `pins: <naam> <versie> staat achter upstream`. De
   nieuwste releases staan er, de oudste ontbrekende kun je via de vergelijklink zien.
2. Probeer de nieuwe versie lokaal: installeer hem en draai wat erop leunt (bij een tool
   `<command> <version_args>`, daarna de stappen die ervan afhangen).
3. Verhoog `version` in `[pins.<naam>]` van `lighthouse.toml`, in een PR waarvan de body
   `Closes #<issuenummer>` met bewijs bevat. Na de merge sluit het issue; de volgende run meet
   `current`.
4. Past de nieuwe versie niet, sluit het issue dan met een reden. De lane maakt voor die versie
   geen nieuw issue.

### Controle achteraf

```text
go run ./cmd/lighthouse pins check
```

Alles op `current` (en lokaal `current` of `ahead`) betekent dat de pin klopt.
