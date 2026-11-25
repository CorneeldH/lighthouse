# Ontwerp: auto-memory uit, `memory-gate` en de migratie van bestaande memory (#497)

Issue: [#497](https://github.com/CorneeldH/lighthouse/issues/497) · Milestone: M3 — Tweede omgeving ·
Datum: 2026-11-24 · Bouwt op [de spec van de instapniveaus](2026-11-22-issue-19-instapniveaus-design.md),
die #497 noemt als knop die een niveau zet zodra hij bestaat.

## Aanleiding

`AGENTS.md` verbiedt harness-memory ("No harness memory"), maar niets dwingt dat af. Claude
Code-auto-memory staat standaard aan en schrijft naar `~/.claude/projects/<project>/memory/`. Die
map staat buiten de repo: niemand reviewt hem, en hij bestaat maar op één machine. Bij de tweede
gebruiker van M3 staan er 213 bestanden in twee mappen, omdat de schijfletter de ene keer als `D:`
en de andere keer als `d:` binnenkwam. Een groot deel van de werkafspraken, besluiten en status van
dat project staat daar, en niet in de repo.

## Doelen

- `lighthouse init` zet auto-memory per repo uit met `autoMemoryEnabled: false` in
  `.claude/settings.json`, en `init --upgrade` houdt dat bij.
- Een hook, `memory-gate`, vangt een schrijfactie naar een auto-memory-map op, via `Edit`, via
  `Write` en via Bash.
- Een migratie verdeelt de bestaande memory één keer over de lagen waar hij thuishoort. De
  gebruiker beslist per groep.

## Niet-doelen

- **Codex-memory.** Die krijgt een eigen issue.
- **De omgevingsvariabele `CLAUDE_CODE_DISABLE_AUTO_MEMORY=0`.** Die wint van de instelling en
  staat buiten de repo. Lighthouse zet of leest hem niet. `memory-gate` is het vangnet voor die
  situatie.
- **Automatisch opschalen.** Na de migratie springt de stand van de gate op light niet vanzelf van
  `log` naar `on`.
- **Een memory-map die via `autoMemoryDirectory` of `CLAUDE_CODE_PROJECT_DIR_NAME` verplaatst is.**
  De gate en `scan` vinden alleen de standaardplek onder de config-map. Met
  `autoMemoryEnabled: false` schrijft Claude Code er toch niet meer naartoe. `CLAUDE_CONFIG_DIR`
  valt wél binnen de scope (§2, §3) *(eigen keuze na builder-check)*.

## Anti-doelen

- Lighthouse schrijft zonder akkoord in `AGENTS.md` of `CLAUDE.md` (no-go van M3).
- De migratie verwijdert een memory-bestand zonder akkoord.
- De gate blokkeert het lezen van de memory-map: dan kan de migratie hem niet meer lezen.

## Aanpak

Er waren vier vragen. Per vraag de opties en de keuze:

1. **Waar woont de migratie?**
   - a) Een subcommando dat voorstellen doet, plus een skill die de keuzes afhandelt.
   - b) Alleen een skill.
   - c) Een stap in `init`.

   **Gekozen: a.** Het subcommando is deterministisch en daardoor te testen met een synthetische
   map (AC 5). De skill voert het gesprek per groep. b is lastig te testen. c draait headless en
   kan dus niet per groep vragen.
2. **Wat doet `init` bij een `autoMemoryEnabled: true` die er al staat?**
   - a) Altijd overschrijven.
   - b) Altijd laten staan en waarschuwen, zoals bij `language`.
   - c) Overschrijven op standard en strict; op light laten staan en waarschuwen.

   **Gekozen: c.** Dat volgt dezelfde ladder als de gate: op light alleen melden, daarboven
   afdwingen. In de uitwerking volgt het afdwingen de stand van `memory-gate`, niet het niveau
   zelf. Met de standaardstanden komt dat op c neer, maar een repo die de gate met een override
   uitzet, kan auto-memory dan ook aanhouden *(eigen keuze na builder-check)*.
3. **Hoe vangt de gate Bash op?**
   - Via `bash-write-check` kan het niet. Die ziet alleen bestanden in een repo (de git-ledger), en
     zijn `pre` blokkeert nooit (`internal/hooks/bashwritecheck.go`).
   - Alleen `Edit|Write` haalt AC 2 niet.

   **Gekozen:** `memory-gate` krijgt een eigen registratie op PreToolUse `Bash` en scant de doelen
   van de schrijfacties in het commando.
4. **Komt `memory-gate` op de `blocking`-lijst van `run.sh`?**
   - a) Ja: fail-closed. Met een oudere binary dan het pack blokkeert elke `Edit` en `Write` op
     standard en strict.
   - b) Nee: fail-open. Met een oude binary valt de gate stil weg.

   **Gekozen: b.** `autoMemoryEnabled: false` is de eerste verdediging. Een vangnet mag niet al het
   schrijfwerk blokkeren.

## Ontwerp

### 1. `autoMemoryEnabled: false` via `init`

Er komt een nieuwe functie `settings.ApplyAutoMemory(doc, stand string) (changed, kept any)`, naast
`ApplyLanguage` in `internal/settings/settings.go`. `init` roept hem bij elke run aan in het
settings-blok van `internal/initcmd/initcmd.go`. Het blok draait nu alleen bij een wijziging in de
hooks, de allow-regels of de taal; die voorwaarde gaat dus open. `stand` is de stand van
`memory-gate`, uit `levels.Resolved.Stand("memory-gate")`. Met de standaardstanden is dat `log`
op light en `on` op standard en strict.

| Situatie in `.claude/settings.json` | stand `on` | stand `log` | stand `off` |
|---|---|---|---|
| sleutel ontbreekt | schrijft `false`, als laatste sleutel | schrijft `false`, als laatste sleutel | schrijft `false`, als laatste sleutel |
| `false` | niets | niets | niets |
| een andere waarde | zet op `false`, meldt het op stderr | laat staan, waarschuwt op stderr | laat staan, zonder melding |

Een repo zonder `.claude/settings.json` krijgt het bestand, net als nu bij een hook-registratie.
`init --upgrade` geeft op een repo die al `false` heeft `No changes.`. Er komt geen nieuwe soort
unit in het manifest: de sleutel wordt bij elke run opnieuw berekend uit het niveau, zoals
`language`.

De tekst van de waarschuwing (Engels, zoals alle hookmeldingen):
`warning: .claude/settings.json has autoMemoryEnabled <value>; memory-gate is at log, so it stays and auto-memory stays on`.

### 2. `memory-gate`

Een Go-hook in `internal/hooks/memorygate.go`, geregistreerd met `hook.Register("memory-gate", …)`
in `internal/hooks/hooks.go`. Hij volgt het patroon van `worktree-check`.

**Herkenning van een memory-pad.** Een pad is een memory-pad als het, genormaliseerd, een segment
`.claude/projects/<één segment>/memory` bevat, gevolgd door het einde van het pad of een `/`. Is
`CLAUDE_CONFIG_DIR` gezet, dan telt ook `<CLAUDE_CONFIG_DIR>/projects/<één segment>/memory`.
Normaliseren betekent:
- `\` wordt `/`;
- een `~` of `$HOME` vooraan wordt de home-map;
- de vergelijking negeert hoofdletters.

Het patroon geldt voor elk project, niet alleen voor deze repo. Een schrijfactie vanuit deze sessie
naar de memory van een ander project is net zo goed harness-memory.

**Registraties** (in `pack/core/hooks/hooks.json`, via `run.sh memory-gate`):

- **PreToolUse `Edit|Write`.** Leest `tool_input.file_path` en blokkeert een memory-pad.
- **PreToolUse `Bash`.** Leest `tool_input.command` en blokkeert als een **schrijfdoel** een
  memory-pad is. Een kleine eigen tokenizer splitst het commando in woorden. Hij houdt rekening met
  enkele en dubbele quotes en met de scheidingstekens `;`, `&&`, `||` en `|`. De repo heeft geen
  shell-parser, en een nieuwe dependency is het niet waard. Als schrijfdoel telt:
  - het woord na `>` of `>>`, ook vastgeplakt (`>…/memory/a.md`), maar niet na `2>` of `&>` als
    het doel `/dev/null` is;
  - de argumenten van `tee`;
  - het laatste argument van `cp`, `mv`, `ln` en `install`;
  - de argumenten van `rm`, `touch`, `mkdir` en `sed -i`.

  Een heredoc schrijft via de `>` ervoor en valt daar dus al onder. Lezen blijft vrij: `cat`, `ls`,
  `head`, `grep` en `find` op de memory-map, een redirect van stderr, of een `cp` *uit* de
  memory-map. Dat is een benadering, geen shell-semantiek. Een `cd` naar de map gevolgd door een
  relatief doel ontsnapt; dat is aanvaard, omdat de gate een vangnet is en geen grens tegen opzet.
  `rm` telt mee omdat verwijderen via de migratie loopt, en die vraagt eerst akkoord.

**Stand.** Uit `components.toml`, zoals elke Go-hook (`internal/hook/hook.go`):
- `on`: een `permissionDecision` `deny` (`hookSpecificOutput`) met een melding, exit 0; de hook staat
  niet in `blocking`;
- `log`: exit 0 met één regel in het logboek;
- `off`: niets.

`pack/core/levels.toml` krijgt `memory-gate` in de lijst van onderdelen, en
`[light.components] memory-gate = "log"`. Op standard en strict staat hij op `on`. Een repo kan de
stand overschrijven met `[components] memory-gate = "…"` in `lighthouse.toml`.

**Melding** bij een blokkade (Engels):

```text
memory-gate: auto-memory write blocked (<path>).
AGENTS.md forbids harness memory: it lives on one machine and nobody reviews it.
Put a lasting rule in the repo with /lighthouse-place-instruction; move existing memory with /lighthouse-migrate-memory.
```

**Kan de hook niet checken** (bijvoorbeeld een payload die geen JSON is), dan meldt hij
`COULD NOT CHECK`, net als de andere gates.

**Overige plekken die de gate erbij krijgen:**
- `pack/core/guards.toml`: een entry `[memory-gate]` (events, matcher, does, terms) voor
  `agent-audit`;
- **niet** in `pack/core/hooks/blocking` (keuze 4): met een ontbrekende of oudere binary valt de gate
  stil weg;
- `e2e/registrations_test.go`: `wantHooks`, de registratietabel en een `minimalPayload`-geval;
- de hooklijst in `CLAUDE.md`: een bullet `memory-gate`;
- `docs/levels.md`: de tabel van niveaus.

### 3. Migratie

#### `lighthouse memory scan`

Een subcommando in `internal/cli`, met de logica in het pakket `internal/automemory`. Het
commando leest alleen.

**Welke mappen.** Claude Code codeert het projectpad tot een mapnaam door elk teken dat geen letter
of cijfer is te vervangen door `-` (`/home/x/repo` wordt `-home-x-repo`, `D:\x\repo` wordt
`D--x-repo`). Auto-memory hoort bij de git-repo en wordt gedeeld over de worktrees. Daarom codeert
`scan` de **main checkout**, de map boven `git rev-parse --git-common-dir`, ook als hij in een
worktree draait. Van de 86 worktree-mappen van deze repo onder `~/.claude/projects/` heeft er geen
één een `memory/` *(eigen keuze na builder-check)*.

`scan` neemt elke map onder `<config>/projects/` waarvan de naam, zonder hoofdlettergevoeligheid,
gelijk is aan die codering. Daardoor vallen `D:` en `d:` samen. `<config>` is `CLAUDE_CONFIG_DIR`
als die gezet is, anders `<home>/.claude`. Een map zonder `memory/` slaat hij over.
`--config-dir <dir>` overschrijft `<config>`, voor tests.

**Wat hij voorstelt.** Per `.md`-bestand in `memory/`, behalve `MEMORY.md`, één bestemming. De
bestemming volgt uit het memory-type in de frontmatter: `type:` bovenaan, of `metadata.type:`.

| Type | Bestemming | Waarom |
|---|---|---|
| `feedback` | `instruction` — een regel via `/lighthouse-place-instruction` | een werkafspraak |
| `project` | `issue` | lopend werk of een besluit dat nog uitgevoerd moet worden |
| `reference` | `docs`, of `host` als de tekst een absoluut pad buiten de repo of een hostnaam noemt | een verwijzing |
| `user` | `drop` | persoonlijk, hoort niet in de repo |
| ontbreekt of onbekend | `undecided` | de skill beslist met de gebruiker |

`MEMORY.md` krijgt altijd `drop`: het is alleen een index.

**Uitvoer.** JSON op stdout, standaard, met `--format text` als leesbare tabel:

```json
{"dirs": ["<abs>"], "files": [
  {"path": "<abs>", "dir": "<abs>", "name": "<name uit frontmatter>", "type": "feedback",
   "destination": "instruction", "reason": "type feedback"}
]}
```

Exitcodes:
- `0` met een lege lijst als er geen memory is;
- `0` met een lege lijst als `<config>/projects` niet bestaat;
- `2` als `<config>/projects` bestaat maar niet leesbaar is.

#### Skill `/lighthouse-migrate-memory`

In `pack/core/skills/migrate-memory/SKILL.md`, in het Engels zoals alle skills. Op elk niveau `on`,
en vermeld in `[calls]` van `levels.toml` (hij roept `skill:place-instruction` aan). Stappen:

1. Draai `lighthouse memory scan`. Is de lijst leeg, meld dat en stop.
2. Groepeer op bestemming en toon per groep de bestanden, met naam en één regel inhoud.
3. Vraag per groep akkoord. De gebruiker mag een bestand naar een andere groep verplaatsen. Bij
   `undecided` stelt de skill per bestand een bestemming voor.
4. Voer per groep uit:
   - `instruction`: via `/lighthouse-place-instruction`, die zelf de laag kiest en akkoord vraagt
     voor elke schrijfactie in `AGENTS.md` of `CLAUDE.md`;
   - `issue`: een concept via `/lighthouse-write-issue`, als die geïnstalleerd is, anders een
     concepttekst in de chat;
   - `docs`: een voorstel voor bestand en tekst onder `docs/`, geschreven na akkoord;
   - `host`: een voorstel voor `host.toml`, geschreven na akkoord;
   - `drop`: niets, behalve markeren.
5. Meld welke bestanden over zijn: niet verwerkt, of verwerkt maar nog op schijf. Vraag per bestand
   of het weg mag. Verwijder alleen wat de gebruiker noemt.

De skill schrijft nooit in `~/.claude/projects/` zonder dat akkoord. Met `memory-gate` op `on`
blokkeert de gate dat `rm` zelf. Daarom laat de skill het verwijderen aan de gebruiker over: hij
geeft het commando, en de gebruiker voert het uit.

#### Waar de gebruiker de migratie tegenkomt

- `init` draait na de repo-wijzigingen `memory scan`. Vindt hij memory, dan meldt hij één regel op
  stderr: `note: <n> auto-memory files found for this repo; move them with /lighthouse-migrate-memory`.
  Dat blijft een melding en verandert niets, zodat `init --upgrade` `No changes.` kan geven.
- De installatiegids (#252, `docs/install.md`) noemt de skill als stap na `init`.

## Tests

- **`internal/automemory`.** Een synthetische config-map in een tempdir met een bekende verdeling:
  - twee mappen voor dezelfde repo, die alleen in hoofdletters verschillen (`D--x-repo` en
    `d--x-repo`);
  - een aanroep vanuit een worktree van de repo, die dezelfde mappen vindt als de main checkout;
  - een map van een ander project, die niet mee mag doen, ook niet als de naam met die van de repo
    begint (`D--x-repo-other`);
  - van elk type een bestand, plus een bestand zonder frontmatter, en een `MEMORY.md`.

  De test controleert de exacte bestemming per bestand en de lijst van mappen.
- **`internal/hooks/memorygate_test.go`.** Per stand (`on`, `log`, `off`):
  - een `Write` naar een Unix-pad, een Windows-pad met `D:\` en `d:\`, een `~`-pad en een pad buiten
    een memory-map;
  - een pad onder `CLAUDE_CONFIG_DIR`;
  - Bash, als tabel van commando's met het verwachte oordeel. Blokkeren: `> …/memory/a.md`,
    `>>`, een vastgeplakte `>`, `tee`, `cp x …/memory/`, `mv`, `rm`, `touch`, `mkdir`, `sed -i`, en
    een heredoc met `>`. Vrij: `cat … 2>/dev/null`, `ls … 2>&1`, `grep foo …/memory/* > /tmp/out`,
    `cp …/memory/a.md docs/a.md`, en een commando zonder memory-pad.
- **`internal/settings`.** `ApplyAutoMemory` voor de negen cellen van de tabel in §1.
- **`internal/initcmd`.** Een `init` op light en op strict met een bestaande `true`, en een `init` op
  strict met de override `memory-gate = "off"`. Een tweede `init --upgrade` geeft `No changes.`.
- **e2e.** De registratie en `wantHooks`.
- **Dogfood.** `go run ./cmd/lighthouse init --upgrade` in deze repo; de geïnstalleerde kopie wordt
  meegecommit.

Wat de tests niet bewijzen: dat Claude Code `autoMemoryEnabled: false` echt respecteert. Dat staat
in [de documentatie](https://code.claude.com/docs/en/memory) en wordt eenmalig met de hand
gecontroleerd in een sessie, met het resultaat in de PR.

## Open voor de bouwer

- De precieze vorm van het commando in `internal/cli` (naam van het pakket, `--format`) volgt de
  bestaande subcommando's.
- De regels voor schrijfdoelen van de Bash-scan mogen groeien als een test een gemist geval vindt.
  Een lezend commando mag daarbij nooit geblokkeerd raken.
