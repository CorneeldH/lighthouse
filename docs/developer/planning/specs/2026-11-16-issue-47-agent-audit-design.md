# Ontwerp: `lighthouse agent-audit`, een check op alle agentische configuratie

Issue: #47 · Datum: 2026-11-16 · Bouwt op `internal/checkinstr` uit #282 (gemerged in #353) en
volgt de eis van
[Lighthouse zonder Python](2026-11-07-zonder-python-design.md): alles wat lighthouse installeert,
draait als de binary.

## Aanleiding en doel

Een projectrepo met lighthouse heeft twee bronnen van agentische configuratie: wat `lighthouse
init` installeert (vastgelegd in `.lighthouse/manifest.toml`) en wat het project, de gebruiker en
de plugins zelf meebrengen. Lighthouse kijkt nu alleen naar zijn eigen eenheden. Of die botsen met
wat er al staat, ziet niemand, tot er dubbele of tegenstrijdige meldingen komen. Voorbeeld:
personal_ai_agent had een eigen `main-commit-guard` naast `main-commit-gate` van lighthouse; dat
bleek pas toen de overstap de oude kopie expliciet weghaalde.

Daarnaast staat elke skill-description permanent in de systeemprompt. Of een skill toevoegen,
inkorten of verplaatsen dat oppervlak groter of kleiner maakt, is nu een indruk, geen getal.

**Doel:** één read-only subcommando, `lighthouse agent-audit`, dat de configuratie inventariseert,
conflicten meldt, kandidaten voor overlap in inhoud aanwijst en het altijd-geladen oppervlak meet.
Een skill laat het model van de sessie oordelen over de overlapkandidaten. Het is een dienst, geen
politie: de check meldt, het project beslist.

## Doelen, niet-doelen, anti-doelen

**Doelen**

- Een inventaris van hooks, skills, rules, agents, permissieregels en MCP-servers, met per regel de
  bron.
- Conflicten: twee blokkerende hooks op hetzelfde event en dezelfde matcher; een `allow` tegenover
  een `deny` of `ask`; een geregistreerde hook zonder script; een hookscript zonder registratie.
- Overlap in inhoud tussen een hook van het project (of gebruiker of plugin) en een lighthouse-hook,
  ook met een andere naam.
- Het skill-oppervlak per bron: aantal, tekens van naam plus description, een ruwe schatting in
  tokens, de grootste posten bovenaan.
- Draait altijd bij de eerste `lighthouse init`, op verzoek bij `--upgrade`, en los.

**Niet-doelen**

- Overlap in inhoud tussen skills. `check-instructions` dekt een dubbel geclaimde naam (check 1),
  overlap zonder afgrenzing (check 4) en een schaduwende personal skill (check 5). Het issue noemt
  skills bij overlap, maar de acceptatiecriteria vragen alleen hooks. *(eigen keuze)*
- Codex en OpenCode. De check leest de configuratie van Claude Code. `.agents/skills/` komt wel in
  de inventaris (bron en harness erbij), maar telt niet mee in het oppervlak van Claude Code.
  Een eigen check voor `~/.codex` en OpenCode is een vervolgissue. *(eigen keuze)*
- Iets repareren. De check schrijft niets, ook niet met een vlag.

**Anti-doelen**

- Een check die "schoon" zegt waar hij niet kon kijken. Een bron die ontbreekt of onleesbaar is,
  geeft `COULD NOT CHECK — <reden>` voor die bron (besluit 18), nooit stilte.
- Een check die standaard blokkeert, of init laat falen. De audit verandert de exitcode van `init`
  nooit.
- Een model-aanroep vanuit de binary. Het oordeel over overlap ligt bij de skill, in de sessie die
  er al is; de binary blijft deterministisch en offline.
- Ruis bij lighthouse zelf: wat lighthouse samen ontworpen heeft (bijvoorbeeld `main-commit-gate`
  en `pr-create-gate`, beide blokkerend op `PreToolUse Bash`) is geen conflict.

## De vier vragen uit het issue

1. **Hoe wordt overlap in inhoud herkend?** Een mix: de binary vindt kandidaten met een vaste
   signatuur per lighthouse-hook, de skill `/lighthouse-audit-agent-config` laat het model van de
   sessie per kandidaat oordelen (§3, §6). *(besluit gebruiker: optie C)*
2. **Wanneer draait het?** Altijd bij de eerste `lighthouse init`, als korte samenvatting. Bij
   `init --upgrade` alleen met `--audit`. Los als `lighthouse agent-audit`, en via de skill (§5).
   Niet in de pre-push: de check blokkeert standaard toch niet, en kost daar alleen tijd.
   *(besluit gebruiker: init altijd, upgrade optioneel; pre-push eigen keuze)*
3. **Wat valt binnen?** De repo, `~/.claude` en de ingeschakelde plugins. Een ontbrekende of
   onleesbare bron geeft `COULD NOT CHECK` voor die bron. `--repo-only` beperkt de check tot de
   repo, voor CI en reproduceerbare tests (§2). *(voorstel, niet tegengesproken)*
4. **Verhouding tot `check-instructions`?** Een nieuw commando ernaast. `check-instructions` blijft
   de gate op commit-tijd over de instructielaag, met FAIL en exit 1. `agent-audit` is een audit
   die meldt. Beide delen de parsers uit `internal/checkinstr` (§8). *(besluit gebruiker: optie A,
   bouwen na de merge van #282)*

## Aanpakken die overwogen zijn

**Overlap herkennen**

- **A — signatuur per lighthouse-hook, alleen in Go.** Deterministisch, goedkoop, testbaar. Mist
  overlap die anders geformuleerd is.
- **B — een model als beoordelaar.** Vangt meer, maar kost tokens, is niet deterministisch en
  lastig te testen; de binary zou een API-sleutel of een CLI nodig hebben.
- **C — A vindt kandidaten, een skill laat het model oordelen.** Gekozen. De binary blijft
  deterministisch; het oordeel kost alleen iets in de sessie die er al is; zonder skill blijven de
  kandidaten bruikbaar.

**Bouwvolgorde**

- **A — spec en plan nu, bouwen na de merge van #282, `internal/checkinstr` hergebruiken.**
  Gekozen: geen dubbele parser en geen gestapelde branch. #282 is inmiddels gemerged (#353); de
  branch van dit issue staat erop.
- **B — stapelen op `issue-282-check-instructions`.** Meteen bouwen, maar elke wijziging aan #282
  moet hierin gemerged worden.
- **C — een eigen parser nu, later ontdubbelen.** Twee parsers die uit elkaar kunnen lopen.

## Ontwerp

### 1. Commando

```text
lighthouse agent-audit [--repo-only] [--only <deel>]... [--strict]
```

- `--only` beperkt de uitvoer tot `inventory`, `conflicts`, `overlap` of `surface` (herhaalbaar).
  `lighthouse agent-audit --only surface` vervangt de regel `measure-skill-surface` in de
  zonder-python-spec (§9).
- `--strict` geeft exit 2 als een bron of hook `COULD NOT CHECK` gaf, anders exit 1 als er een
  conflict of overlapkandidaat is die niet in `ignore` staat, anders 0. Niet kunnen kijken gaat
  voor een melding: in CI mag een onleesbare bron nooit als groen eindigen. Zonder `--strict` is
  de exitcode 0, ook met meldingen. *(eigen keuze na builder-check)*
- Exit 2 met `agent-audit: COULD NOT CHECK — <reden>` op stderr als de check als geheel niet kan
  meten: geen repo-root, een kapotte `lighthouse.toml`. Een enkele bron die ontbreekt, is geen
  exit 2 maar een regel in de uitvoer (§2).

Opbouw naar het voorbeeld van `lighthouse worktree-audit` (`internal/cli/worktreeaudit.go`): één
bestand `internal/cli/agentaudit.go` met cobra en de exitcodetabel in de lange help, de logica in
een eigen package `internal/agentaudit`, en een `Env` met de bestandssysteemwortels (repo-root,
config-dir van Claude Code, home) zodat een test een tijdelijke boom inzet.

### 2. Bronnen

| Bron | Wat de check leest |
|---|---|
| `lighthouse` | eenheden in `.lighthouse/manifest.toml`; hookcommando's met de hook-map (`packs.IsLighthouseCommand`); permissieregels via `packs.IsLighthouseRule` |
| `project` | `.claude/settings.json`, `.claude/skills/`, `.claude/agents/`, `.claude/rules/`, `.claude/hooks/`, `.mcp.json`, `.agents/skills/`, voor zover niet van lighthouse |
| `lokaal` | `.claude/settings.local.json`; in `~/.claude.json` de `mcpServers` onder `projects[<repo-root>]` |
| `gebruiker` | `~/.claude/settings.json`, `~/.claude/skills/`, `~/.claude/agents/`, `~/.claude/rules/`, `mcpServers` bovenin `~/.claude.json` |
| `plugin:<naam>` | elke plugin die ingeschakeld is (zie hieronder), met `installPath` uit `~/.claude/plugins/installed_plugins.json`; daarin `skills/`, `commands/`, `agents/`, `hooks/hooks.json`, `.mcp.json` |

`~/.claude` is de config-dir van Claude Code: `$CLAUDE_CONFIG_DIR` als die gezet is, anders
`os.UserHomeDir()` + `/.claude`. `lokaal` is een eigen bron naast de vier uit het issue: het
bestand wordt niet gecommit, en dat verschil telt bij een conflict. *(eigen keuze)*

**Ingeschakelde plugins.** `enabledPlugins` volgt de voorrang van Claude Code: lokaal gaat voor
project, project voor gebruiker. Een plugin staat aan als de bron met de hoogste voorrang die hem
noemt `true` zegt. Managed settings (beheerd door een organisatie) leest de check niet; dat staat
als één regel in de uitvoer, niet als `ok`. *(eigen keuze na builder-check)*

**Worktrees.** Lighthouse werkt in `.claude/worktrees/<naam>`, maar Claude Code legt
`projectPath` (in `installed_plugins.json`) en de sleutel onder `projects` (in `~/.claude.json`)
vast op de map waar de sessie startte. De check matcht daarom op twee paden: de root van de
worktree en de main checkout (de map boven `git rev-parse --git-common-dir`). Een plugin met
scope `user` past altijd; met scope `project` of `local` alleen bij een van die twee paden.
*(eigen keuze na builder-check)*

Per bron één statusregel bovenaan de uitvoer: `ok`, `afwezig` (bestaat niet, niets te melden) of
`COULD NOT CHECK — <reden>` (bestaat, maar onleesbaar of ongeldige JSON). Een plugin die
ingeschakeld is maar geen `installPath` heeft, is `COULD NOT CHECK`. Met `--repo-only` staan
`gebruiker` en `plugin:*` als `overgeslagen (--repo-only)`, niet als `ok`.

### 3. Signaturen van lighthouse-hooks

Lighthouse kent zijn eigen hooks. Per pakket legt een bestand `pack/<pakket>/hooks/guards.toml`
vast wat elke hook bewaakt:

```toml
[main-commit-gate]
events = ["PreToolUse"]
matcher = "Bash"
does = "Blocks a git commit or push on the default branch."
# Elke groep moet matchen; binnen een groep is één term genoeg.
terms = [
  ["git commit", "git push"],
  ["main", "master", "default branch", "allow-main"],
]
```

Het bestand zit in de binary (de pakketten zijn ingebed) en wordt niet geïnstalleerd. Een test
eist een signatuur voor elke naam in `pack/core/hooks/blocking` en voor elke bash-hook in een
taalpakket die blokkeert (`go-lint-gate`). *(eigen keuze: alleen blokkerende hooks verplicht)*

**Kandidaat:** een hook uit een andere bron dan `lighthouse`, op een event uit `events`, met een
matcher die dezelfde tool kan raken (gelijk, leeg, `*`, of een alternatie die de tool noemt), en
een inhoud die op elke `terms`-groep minstens één treffer heeft. Hoofdletters tellen niet mee. De
inhoud is het script waar het commando naar wijst (`$CLAUDE_PROJECT_DIR`, `~` en relatieve paden
opgelost), of anders de commandoregel zelf. Een script dat niet te lezen is, geeft
`COULD NOT CHECK` voor die hook en geen kandidaat.

### 4. Conflicten

| Soort | Regel | Niet gemeld |
|---|---|---|
| `blocking-pair` | twee hooks op hetzelfde event en dezelfde matcher (na normaliseren), die allebei blokkeren, en geen van beide van `lighthouse` | een paar met een lighthouse-hook: zie hieronder |
| `permission` | een `allow` in de ene bron tegenover een `deny` of `ask` in een andere, op dezelfde regel of waar de ene de andere dekt (`Bash(git push:*)` dekt `Bash(git push origin main)`) | twee regels in dezelfde bron |
| `missing-script` | een geregistreerde hook waarvan het script niet bestaat; voor `run.sh <naam>` ook: `run.sh` ontbreekt, of de binary kent `<naam>` niet (`hook`-register) | een commando zonder pad (inline) |
| `unregistered-script` | een bestand onder `.claude/hooks/` of `~/.claude/hooks/` dat geen commando noemt, en dat ook niet bij naam voorkomt in een geregistreerd script (een helper) | `.claude/hooks/lighthouse/`: die map is van lighthouse, `missing-script` dekt hem |

**Waarom geen `blocking-pair` met een lighthouse-hook.** Op `PreToolUse Bash` blokkeren al vier
lighthouse-hooks (`main-commit-gate`, `commit-test-gate`, `bash-write-check`, `pr-create-gate`).
Elke blokkerende projecthook daar zou vier meldingen geven, terwijl Claude Code beide hooks
gewoon draait: twee blokkerende hooks naast elkaar zijn op zich geen fout. Of een projecthook
hetzelfde doet als een lighthouse-hook, meldt `overlap` (§3), op inhoud. Tussen hooks van project,
lokaal, gebruiker en plugins blijft `blocking-pair` staan: daar is geen signatuur om op inhoud te
vergelijken. *(eigen keuze na builder-check)*

**Bron van een permissieregel.** Lighthouse- en projectregels staan in hetzelfde
`.claude/settings.json`. Een regel daarin is van `lighthouse` als hij voorkomt in de
`permissions.json` van een geïnstalleerd pakket, of als `packs.IsLighthouseRule` hem herkent;
anders van `project`. Lighthouse levert nu alleen `allow`-regels (`Bash(lighthouse
review-run:*)`), dus het conflict met lighthouse ontstaat als een andere bron daar een `deny` of
`ask` tegenover zet. *(eigen keuze na builder-check)*

**Blokkerend** betekent: voor een lighthouse-hook, de naam staat in `pack/<pakket>/hooks/blocking`
of de hook heeft een signatuur; voor elke andere hook, het script bevat `exit 2`,
`"decision": "block"` of `"permissionDecision": "deny"`. Een hook die niet te lezen is, telt als
`COULD NOT CHECK`, niet als niet-blokkerend.

### 5. Wanneer het draait

- **Eerste `lighthouse init`** (de repo had nog geen `.lighthouse/manifest.toml`): nadat `init`
  geschreven heeft, toont het een
  samenvatting op stdout. Die geeft de statusregels per bron, de conflicten, de overlapkandidaten
  en de vijf grootste posten van het oppervlak, met één regel eronder:
  `Draai /lighthouse-audit-agent-config voor een oordeel over de kandidaten.` De volledige
  inventaris staat er niet in.
- **`init --upgrade --audit`**: dezelfde samenvatting. Zonder `--audit` draait de check bij een
  upgrade niet.
- **Los**: `lighthouse agent-audit`, volledige uitvoer.

Faalt de audit binnen `init` (exit 2), dan schrijft `init` de regel `agent-audit: COULD NOT CHECK
— <reden>` naar stderr en eindigt met zijn eigen exitcode. Schreef `init` niets (de gebruiker zei
nee, of er was niets te doen), dan draait de audit niet: er is dan geen eerste installatie. Of er
een forge-remote is (`haveRepo`), maakt niet uit. *(eigen keuze na builder-check)*

### 6. Skill `/lighthouse-audit-agent-config`

`pack/core/skills/audit-agent-config/SKILL.md`, geïnstalleerd als
`.claude/skills/lighthouse-audit-agent-config/`. De skill:

1. draait `lighthouse agent-audit`;
2. leest per overlapkandidaat het projectscript en de `does`-zin van de lighthouse-hook uit de
   uitvoer;
3. oordeelt per paar: `overlap`, `geen overlap` of `twijfel`, met één zin waarom;
4. meldt de conflicten zoals de binary ze gaf, en stelt per oordeel `overlap` één concrete stap
   voor (de projecthook weghalen, of het paar in `ignore` zetten), zonder iets zelf te veranderen.

De description blijft kort, want de skill komt zelf in het oppervlak dat de check meet.
`pack/core/permissions.json` krijgt `Bash(lighthouse agent-audit:*)`.

### 7. Uitvoer

Tekst, in deze volgorde: bronnen, conflicten, overlap, oppervlak, inventaris. Elke melding krijgt
een stabiele sleutel, zodat `ignore` hem kan noemen:

```text
Bronnen
  lighthouse   ok
  project      ok
  lokaal       afwezig
  gebruiker    ok
  plugin:superpowers  COULD NOT CHECK — installPath /… bestaat niet

Conflicten (2)
  [blocking-pair:PreToolUse:Bash:project/.claude/hooks/check-branch.sh|gebruiker/~/.claude/hooks/guard.sh]
    PreToolUse Bash: project .claude/hooks/check-branch.sh en gebruiker ~/.claude/hooks/guard.sh blokkeren allebei
  [permission:Bash(lighthouse review-run:*)]
    lighthouse allow Bash(lighthouse review-run:*) tegenover gebruiker deny Bash(lighthouse review-run:*)

Overlap, kandidaten (1)
  [overlap:main-commit-gate|project/.claude/hooks/check-branch.sh]
    project .claude/hooks/check-branch.sh (PreToolUse Bash) lijkt op lighthouse main-commit-gate
    lighthouse: Blocks a git commit or push on the default branch.
    treffers: "git commit", "main"

Oppervlak (skills en agents, altijd geladen)
  bron            aantal   tekens   ~tokens
  plugin:superpowers   14    9 812    2 453
  lighthouse           13    7 044    1 761
  …
  Grootste posten
  plugin:superpowers  brainstorming   812 tekens
  …

Inventaris
  hooks: event, matcher, commando, script, bron, blokkerend
  …
```

Het **oppervlak** telt per skill, command of agent de tekens van `name` plus `description` uit de
frontmatter (`commands/*.md`: bestandsnaam plus `description`). Tokens zijn tekens / 4, naar boven
afgerond, en heten in de uitvoer `~tokens`. De tabel staat op tokens aflopend; daaronder de `top`
grootste posten. Agents tellen mee, want ook hun descriptions staan altijd in de systeemprompt.
*(eigen keuze; het issue vraagt minstens skills)*

De exacte opmaak van kolommen en scheidingstekens is aan de bouwer, zolang de sleutels stabiel zijn
en de volgorde van de secties vastligt.

### 8. Configuratie

Een nieuwe sectie in `lighthouse.toml`, geregistreerd in `internal/config/config.go`:

```toml
[agent_audit]
ignore = []                   # sleutels van meldingen, zoals "overlap:main-commit-gate|project/.claude/hooks/x.sh"
top = 10                      # aantal grootste posten in het oppervlak
description_max_chars = 1024  # een description daarboven is een melding in het oppervlak
```

Een sleutel in `ignore` die nergens meer bij hoort, geeft één regel `ignore: <sleutel> komt niet
meer voor`, zodat de lijst niet stil veroudert. Dat geldt alleen als elke bron die de sleutel
noemt deze keer gemeten is (`ok`). Een sleutel met een bron die `afwezig`, `overgeslagen` of
`COULD NOT CHECK` is, blijft stil: op een andere machine of met `--repo-only` is hij niet
verouderd, alleen niet te zien. *(eigen keuze na builder-check)*

`docs/site/data/config.json` krijgt de drie sleutels, als dat bestand de bekende sleutels opsomt.

### 9. Wat er verder verandert

- `internal/checkinstr`: de frontmatter-parser en de lezer van hookregistraties worden
  geëxporteerd, of verhuizen naar een gedeeld package; de keus is aan de bouwer, zolang
  `check-instructions` hetzelfde oordeel blijft geven.
- `internal/settings`: lezen gebeurt niet via `walk` (die is voor samenvoegen); de check krijgt een
  eigen lezer, of `walk` wordt geëxporteerd zonder het samenvoegen te raken.
- De zonder-python-spec: de regel `python3 scripts/measure_skill_surface.py` wijst naar
  `lighthouse agent-audit --only surface`.
- `CLAUDE.md`/managed block: de nieuwe skill in de skillijst; dogfood met
  `go run ./cmd/lighthouse init --upgrade`.
- Een pagina `docs/agent-audit.md` (Engels, zoals `docs/usage-gate.md`) en een fragment in
  `changelog.d/` (`type: added`).

## Bewijs

Elke regel is een Go-test op een tijdelijke boom, met `HOME` en `CLAUDE_CONFIG_DIR` naar een
tijdelijke map, zodat de host niet meetelt.

| Acceptatiecriterium | Test |
|---|---|
| Inventaris met per regel de bron | een boom met per bron één hook, skill, rule, agent, permissieregel en MCP-server; de inventaris noemt elk met de juiste bron |
| Twee blokkerende hooks op hetzelfde event en dezelfde matcher | twee projecthooks met `exit 2` op `PreToolUse Bash` → `blocking-pair`; een blokkerende projecthook naast `main-commit-gate` → geen `blocking-pair`; `main-commit-gate` naast `pr-create-gate` → geen melding |
| Tegenstrijdige permissieregel | lighthouse `allow Bash(lighthouse review-run:*)` in `.claude/settings.json` tegenover een project- of gebruikers-`deny` erop → `permission` met bron `lighthouse`; project `allow Bash(git push:*)` tegenover gebruiker `ask Bash(git push origin main)` → `permission` |
| Geregistreerde hook zonder script | registratie naar een ontbrekend bestand → `missing-script`; `run.sh onbekend` → `missing-script` |
| Hookscript zonder registratie | `.claude/hooks/oud.sh` zonder registratie → `unregistered-script`; een helper die een geregistreerd script noemt → geen melding |
| Overlap met de oude `main-commit-guard` | een fixture `main-commit-guard.sh`, geschreven naar de beschrijving in het issue (blokkeert `git commit` op `main`), op `PreToolUse Bash`; ook als `check-branch.sh` → kandidaat bij `main-commit-gate`. Een niet-gerelateerd script → geen kandidaat |
| Oppervlak per bron, grootste bovenaan | drie bronnen met bekende lengtes → de juiste aantallen, tekens en `~tokens`, en de volgorde |
| Schrijft niets, blokkeert standaard niet | een hash van de boom vóór en na is gelijk; exit 0 met meldingen; exit 1 alleen met `--strict` |
| `COULD NOT CHECK`, niet schoon | ongeldige JSON in `~/.claude/settings.json` → die bron `COULD NOT CHECK`, de rest gemeten; met `--strict` exit 2; geen repo-root → exit 2 |
| Plugins in een worktree | een plugin met scope `project` en `projectPath` op de main checkout, audit gedraaid vanuit een worktree → bron `plugin:<naam>` gemeten, niet `COULD NOT CHECK` |
| `ignore` veroudert niet stil, maar ook niet vals | een sleutel die verdwenen is → `ignore`-regel; een sleutel van `gebruiker` met `--repo-only` → geen regel |
| Draait bij eerste init, optioneel bij upgrade | init-test: eerste init toont de samenvatting; `--upgrade` zonder `--audit` niet, met `--audit` wel |

De fixture voor `main-commit-guard` is zelf geschreven: het origineel staat niet in deze repo.
Dat bewijst dat de signatuur een guard met dezelfde bedoeling vangt, niet dat hij precies dat ene
oude script vangt.

## Aan de bouwer

- De opmaak van de tekstuitvoer, binnen §7.
- Of de parsers uit `checkinstr` verhuizen of geëxporteerd worden (§9).
- De normalisatie van matchers (alternaties als `Edit|Write`), zolang de tests in *Bewijs* slagen.
- De exacte `terms` per signatuur, zolang de fixture-test en de negatieve test slagen.
