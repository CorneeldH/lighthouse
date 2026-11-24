# Lighthouse schoon verwijderen uit een repo — ontwerp

Issue: #91 · Milestone: Product: eerste versie voor anderen

## Doel en waarom

Wie lighthouse probeert, moet het ook weer kwijt kunnen, zonder handwerk en zonder dat er eigen
werk verdwijnt. Dat hoort bij een product voor anderen: een proefinstallatie die je niet schoon
terugdraait, houdt mensen weg. `lighthouse uninstall` draait terug wat `lighthouse init` schreef,
toont eerst per bestand de diff, en verwijdert pas na akkoord.

## Doelen

- `lighthouse uninstall` verwijdert alles wat in `.lighthouse/manifest.toml` staat: bestanden,
  de managed blokken in `AGENTS.md`, en de hookregistraties en permissieregels in
  `.claude/settings.json`. Andere inhoud van die bestanden blijft staan.
- Het draait ook terug wat `init` buiten de units schreef, voor zover `init` dat heeft
  vastgelegd (zie *Vastleggen buiten de units*).
- Een lokaal gewijzigde unit weigert het, tenzij `--force`. Dezelfde regel als `init --upgrade`.
- Na `init` en `uninstall` in een repo staat er dezelfde bestandsboom als vooraf.
- De startgids beschrijft het.

## Niet-doelen

- Labels op de forge (`--create-labels`) en `host.toml` in de homedirectory. Die horen niet bij
  één repo; uninstall meldt ze en laat ze staan.
- De lighthouse-binary zelf.
- Een volledige terugdraai van installaties van vóór deze versie. Wat daar niet is vastgelegd,
  meldt uninstall; het raakt het niet aan.
- Committen. Uninstall wijzigt de werkboom; de gebruiker commit.

## Anti-doelen

- Uninstall verwijdert iets wat de gebruiker zelf schreef of wijzigde, zonder dat de diff het
  toonde en de gebruiker akkoord gaf. Dat is een mislukking, ook als de repo daarna "schoon" is.
- Uninstall laat een halve toestand achter: een geweigerde run schrijft niets.
- Uninstall raadt op patronen in bestanden die van het project kunnen zijn. Wat niet is
  vastgelegd, raakt het niet. Eén uitzondering, omdat het al zo werkt en de diff het toont:
  `settings.Apply` haalt elke hookregistratie weg waarvan het commando in
  `.claude/hooks/lighthouse/` wijst, ook een die niet in het manifest staat. Die scripts gaan
  toch weg, dus zo'n registratie zou daarna nergens heen wijzen.

## Randvoorwaarden

- Milestone-no-go: lighthouse schrijft nooit zonder akkoord in `AGENTS.md`/`CLAUDE.md`. De diff
  en de vraag voldoen; `--yes` is een expliciete keuze van de gebruiker.
- De conflictregel is die van `init --upgrade` (`internal/reconcile`): gelijk aan de hash in het
  manifest is veilig, anders gewijzigd.
- Code, namen en commentaar in het Engels; meldingen van het commando in het Engels, zoals de
  rest van `init`.
- Dogfood: wijzigt dit wat `init` schrijft, dan `go run ./cmd/lighthouse init --upgrade` en de
  geïnstalleerde kopie committen; CI eist `No changes.`
- Elke PR een fragment in `changelog.d/`.

## Aanpakken

### Wat met wat `init` buiten het manifest schrijft

`init` schrijft ook `lighthouse.toml`, regels in `.gitattributes`, soms `CLAUDE.md` met
`@AGENTS.md`, de top-level sleutel `"language"` in `.claude/settings.json`, een gezaaid
labelsbestand en direction-docs. Niets daarvan staat in het manifest.

1. **Laten staan en melden.** Eenvoudig, maar AC 4 haalt het alleen als die bestanden vooraf al
   bestonden.
2. **`init` legt vast wat het buiten de units aanmaakte, uninstall draait dat terug.**
   Gekozen. Alleen wat vastligt, gaat weg, dus geen gok; installaties van vóór deze versie
   krijgen een melding.
3. **Op bekende patronen verwijderen.** Raakt eigen `.gitattributes`-regels en een eigen
   `CLAUDE.md`. Valt af op het anti-doel "raadt op patronen".

### Bouwwijze

- **A. Uninstall als modus van `internal/initcmd`, met een eigen commando
  `lighthouse uninstall`.** Gekozen. Eén pad voor verwijderen: `reconcile`, de conflictpoort,
  de diff, de vraag en `apply` met `removeEmptyParents` worden hergebruikt, dus uninstall en
  `--upgrade` weigeren precies hetzelfde.
- **B. Een apart pakket `internal/uninstall`.** Diff, vraag, apply en conflictmelding bestaan
  dan twee keer en lopen uiteen. Afgevallen.
- **C. `lighthouse init --remove`.** "init" dat verwijdert leest verkeerd in de startgids, en
  `execute` heeft al een rommelige modusmatrix (plain init weigert op een geïnitialiseerde repo).
  Afgevallen.

## Ontwerp

### Commando

```
lighthouse uninstall [--yes] [--force]
```

- Werkt op de repo van de huidige directory, net als `init`.
- Zonder `.lighthouse/manifest.toml` stopt het met een fout: lighthouse is hier niet
  geïnstalleerd. Niets geschreven.
- `--yes` slaat de vraag over; `--force` verwijdert ook lokaal gewijzigde units.
- Geregistreerd naast `newInitCommand()` in `internal/cli/root.go`, in een eigen bestand
  `internal/cli/uninstall.go`, met een functie `initcmd.Uninstall` erachter.

### Verloop

1. Lees het manifest. Elke unit krijgt een lege gewenste stand (`Desired == nil`); `reconcile`
   geeft dan per unit *Remove* (ongewijzigd of al weg) of *Conflict* (lokaal gewijzigd).
2. Is er een conflict en geen `--force`: toon per conflict de melding van `--upgrade`, stop met
   `refused: N locally modified unit(s)`. Niets geschreven.
3. Plan de verwijdering:
   - bestandsunits: het bestand weg;
   - blokunits: `agentsmd.Remove`;
   - hookunits en permissieunits: `settings.Apply` met een lege gewenste set, `ApplyAllow` met
     de te laten vallen regels; projectentries blijven staan (dat doen beide al). Een
     permissieregel valt alleen weg als `packs.IsLighthouseRule` hem herkent, zoals nu.
4. Plan het terugdraaien van wat buiten de units is vastgelegd (volgende paragraaf).
5. Plan het manifest zelf en `.lighthouse/created.toml` weg.
6. Toon per bestand de unified diff, en één vraag `Remove lighthouse? [y/N]`, zoals `init`.
   Nee is een nette stop, niets geschreven.
7. Pas toe. Ruim daarna lege mappen op die door het verwijderen leeg raakten, van de map van
   elk verwijderd bestand omhoog tot aan de repo-root; een map met nog iets erin stopt de
   klim. Dat geldt voor de installatiewortels (`.claude/skills`, `.agents/skills`,
   `.claude/agents`, `.claude/hooks/lighthouse`) en voor mappen erbuiten (`.lighthouse/`,
   `.lighthouse/scripts/`, `.github/workflows/`). `removeEmptyParents` doet dat niet: het
   laat de installatiewortels bewust staan en raakt niets buiten die wortels. Uninstall krijgt
   dus een eigen stopregel; `--upgrade` houdt de huidige.
8. Meld wat bleef staan (zie *Melding achteraf*).

Uninstall slaat alles over wat `init` vóór en na het plan doet en hier niet hoort:
`choosePacks`, `fillWorkflows`, `resolveLevels`, `planConfig`, `.gitattributes`, `CLAUDE.md`,
het zaaien, en daarna pins, hook-check, lint-check, audit, labelsync en `offerHost`. `execute`
moet daarvoor uit elkaar: de gedeelde stappen (diff tonen, vragen, toepassen) apart, zodat
uninstall ze aanroept zonder `buildPlan` te draaien.

### Vastleggen buiten de units

Een eigen bestand, `.lighthouse/created.toml`, legt vast wat `init` buiten de units aanmaakte.
Niet in het manifest: `manifest.Read` weigert onbekende sleutels, en een oudere binary leest het
manifest in hooks (`internal/commitgate/imports.go`, `internal/hookcheck`, `internal/cli/hook.go`).
Een nieuwe sectie daar zou een teamgenoot met een oudere binary de commit blokkeren. Een apart
bestand leest een oudere binary niet.

```toml
# Managed by lighthouse init. Do not edit by hand.

files = ["lighthouse.toml", "CLAUDE.md"]
gitattributes = ["*.sh eol=lf", ".claude/hooks/lighthouse/** eol=lf"]
settings_keys = ["language", "hooks", "permissions", "permissions.allow"]
settings_language = "dutch"
```

- `files`: bestanden die er niet waren en die `init` schreef: `lighthouse.toml`, `CLAUDE.md`,
  `AGENTS.md`, `.claude/settings.json`, `.gitattributes`.
- `gitattributes`: regels die `init` toevoegde aan een bestaande `.gitattributes`.
- `settings_keys`: sleutels en containers die `init` in een bestaande `.claude/settings.json`
  aanmaakte: `"language"`, `"hooks"`, `"permissions"`, `"permissions.allow"`.
- `settings_language`: de waarde van `"language"` toen `init` de sleutel toevoegde.

Zowel een verse `init` als `--upgrade` vult het bestand aan: `buildPlan` weet per pad of het
bestond (`existed`), `ensureGitattributes` welke regels ontbraken, en `ApplyLanguage` of het
de sleutel toevoegde. Dat is nodig, want `"language"` komt in de praktijk pas bij een
`--upgrade` (na het zetten van `[project] language`), en `--upgrade` voegt ook ontbrekende
`.gitattributes`-regels opnieuw toe. Het bestand groeit alleen: een `--upgrade` haalt er niets
uit. Een run die niets aanmaakt, schrijft het niet; zonder iets vastgelegd bestaat het niet. Zo
verandert het voor lighthouse zelf en andere bestaande installaties niets, tot een `--upgrade`
iets aanmaakt.

### Terugdraaien van het vastgelegde

| Vastgelegd | Wat uninstall doet |
|---|---|
| `lighthouse.toml` in `files` | weg, ook als hij later bewerkt is; de diff toont het. Zonder lighthouse heeft hij geen lezer. |
| `AGENTS.md` in `files` | na het verwijderen van de blokken weg als er alleen witruimte over is; anders blijft de rest staan |
| `.claude/settings.json` in `files` | na het verwijderen van registraties, regels en `"language"` weg als er een leeg object over is (lege `hooks`, `permissions` of `allow` tellen als leeg); anders blijft de rest staan |
| `CLAUDE.md` in `files` | weg als de inhoud nog precies `@AGENTS.md` is en `AGENTS.md` weggaat; anders blijft hij staan en volgt een melding |
| `.gitattributes` in `files` | na het verwijderen van de lighthouse-regels weg als er alleen witruimte over is |
| regels in `gitattributes` | elke regel één keer weg (de laatste exacte treffer); een regel die er niet meer is, wordt overgeslagen |
| `settings_language` / `"language"` in `settings_keys` | de sleutel weg als de waarde nog gelijk is; anders blijft hij staan |
| containers in `settings_keys` | weg als ze na het verwijderen leeg zijn (`"hooks": {}`, `"allow": []`, `"permissions": {}`); `settings.Apply` en `ApplyAllow` laten ze nu staan |

Het vastgelegde valt niet onder de conflictpoort: wat afwijkt, blijft staan en wordt gemeld,
behalve `lighthouse.toml`.

### Melding achteraf

Na een geslaagde run meldt uninstall op stderr, één regel per geval:

- gezaaide bestanden die het project houdt: het labelsbestand, `docs/developer/labels.md`,
  direction-docs (`kept: <pad> (project file after seeding)`);
- runtime-mappen onder `.lighthouse/` die bleven staan, zoals `.lighthouse/run/` en
  `.lighthouse/bin/` (`kept: <pad> (runtime state, not installed by init)`);
- zonder `.lighthouse/created.toml`, voor elk van `lighthouse.toml`, `CLAUDE.md` met
  `@AGENTS.md`, de lighthouse-regels in `.gitattributes` en `"language"` in `settings.json` dat
  er staat: `left: <pad> — init may have written it; remove it yourself if so`;
- vastgelegde dingen die afwijken en bleven staan;
- labels op de forge en `host.toml`: `not touched: ...`;
- dat een lopende Claude Code-sessie de hooks pas na een herstart kwijt is.

## Tests

In `internal/initcmd`, met de helpers van `TestUpgradeRightAfterInitIsEmpty` (`newRepo`,
`initYes`, `snapshot`, `sameSnapshot`):

- **Rondreis met eigen inhoud:** een repo met eigen tekst in `AGENTS.md`, een eigen hook en een
  eigen permissieregel in `.claude/settings.json`, en een eigen `.gitattributes`.
  `init --yes`, dan `uninstall --yes`: dezelfde snapshot als vooraf.
- **Rondreis in een lege repo:** geen `AGENTS.md`, `CLAUDE.md`, `settings.json`,
  `.gitattributes` of `lighthouse.toml` vooraf: na de rondreis staan ze er ook niet.
- **Weigering:** de gevallen van `TestUpgradeRefusesLocalEdit` (scriptregel, blokregel,
  hook-timeout, verwijderd script) weigeren zonder `--force` en schrijven niets; met `--force`
  gaan ze weg.
- **Installatie zonder `.lighthouse/created.toml`:** units weg, `lighthouse.toml` en de rest
  blijven staan en worden gemeld.
- **Afwijkend vastgelegd:** een gewijzigde `"language"` en een uitgebreide `CLAUDE.md` blijven
  staan en worden gemeld.
- **Rondreis via `--upgrade`:** `init`, `[project] language` zetten, `--upgrade` (voegt
  `"language"` toe), `uninstall`: dezelfde snapshot als na het zetten van de taal, zonder
  `lighthouse.toml`.
- **`--upgrade` vult `created.toml` aan en haalt er niets uit**, en schrijft het niet als er
  niets is aangemaakt.
- **Niet geïnstalleerd:** zonder manifest een fout, niets geschreven.
- **Nee op de vraag:** niets geschreven.

Voor `created.toml`: lezen, schrijven en aanvullen (alleen groeien, geen dubbele items).

De fixtures eindigen op precies één `\n`. Een bestand zonder slotnewline, of met een lege
regel aan het eind, komt niet byte-gelijk terug: `agentsmd.Set` en `ensureGitattributes`
vullen de rand aan, en `agentsmd.Remove` haalt één lege regel weg. Dat is aanvaard en staat
in de startgids.

In `internal/repopins/docs_test.go`: de startgids noemt `lighthouse uninstall`.

## Documentatie

- `docs/getting-started.md`: een sectie "Remove lighthouse" na de upgrade-bullets: het commando,
  de vraag, `--force`, wat blijft staan, en dat installaties van vóór deze versie
  `lighthouse.toml` en de rest zelf moeten opruimen.
- Een fragment in `changelog.d/`, met de compatibiliteitsnoot hieronder.

## Risico's

- **Oudere binary.** Die kent `created.toml` niet en negeert het; het manifest verandert niet
  van vorm. Een `--upgrade` met een oudere binary vult het niet aan, dus wat die aanmaakt,
  blijft bij uninstall staan en wordt gemeld.
- **Opmaak van `settings.json`.** `init` herschrijft het bestand via `orderedjson.Marshal`
  zodra er iets verandert (`initcmd.go:541-547`); een niet-canonieke opmaak gaat dan verloren.
  Besloten: aanvaard. Voor `settings.json` betekent "dezelfde bestandsboom" dezelfde
  JSON-inhoud; de diff van `init` toont de nieuwe opmaak al. De rondreistest gebruikt een
  fixture in de canonieke opmaak, zodat hij toch bytes vergelijkt, en een test met afwijkende
  opmaak vergelijkt de JSON-inhoud. De startgids noemt het.

## Aan de bouwer

- Hoe `execute` precies uit elkaar gaat, en de naam en vorm van de gedeelde stappen.
- De precieze tekst van de meldingen.
- Hoe `CLAUDE.md` vastgelegd wordt als `init` hem onleesbaar aantrof (dan niet in `files`).
