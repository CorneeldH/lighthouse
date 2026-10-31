# Installatiehandleiding voor agents (#88)

## Doel en waarom

Installeren gaat in de praktijk via een agent. Iemand zegt "installeer lighthouse in deze repo"
tegen Claude Code, Codex of OpenCode. Die agent heeft dan een handleiding nodig die voor hem
geschreven is. De handleiding zegt wat hij zelf doet en wat hij aan de gebruiker vraagt, elke keuze
met een aanbevolen antwoord.

De gebruiker plakt één zin in de agent:

```text
Read https://corneeldh.github.io/lighthouse/agents/install.md and install lighthouse in this repo.
```

Daarna doet de agent de rest. Dit is de tweede helft van de DoD van de milestone *Product: eerste
versie voor anderen*: een developer die niet Corneel is, installeert lighthouse "met de
handleiding voor agents" in een eigen GitHub-repo.

Het zijn docs voor agents, niet voor mensen. De startgids (`docs/getting-started.md`) blijft de
gids voor mensen.

## Voorbeelden van buiten

Recente voorbeelden, juli tot oktober 2026, in twee patronen:

- **De hele instructie in de plaktekst**: Praetore (README, PR #65), wp-coding-agents (PR #810)
  en Axelix (PR #1889). Daar komen de ground rules van Praetore vandaan: leg elke stap uit, wacht
  op een expliciet ja, nooit `sudo`, gebruik alleen flags die `--help` toont en stop als er een
  ontbreekt.
- **Een korte plakzin met een URL naar een markdown-bestand**: Rustwright `mcp/AGENT_SETUP.md`
  (PR #104) en Cloudflare `developers.cloudflare.com/agent-setup/prompt.md`. Daar komen deze
  dingen vandaan: de tweede persoon met gebiedende wijs, eerst de vereisten checken, een
  verificatiestap met de verwachte output, en een vast eindrapport. Cloudflare is ook een
  tegenvoorbeeld. Er staat "Do not ask the user" in, terwijl een andere stap de gebruiker toch
  iets vraagt.

Lighthouse volgt het tweede patroon.

## Doelen

- Eén Engelse markdown-pagina voor agents, op een vaste URL op de site. Een agent kan de pagina
  volgen zonder andere pagina's te lezen.
- Elke keuze is een vraag met opties en een aanbevolen antwoord. De vorm past bij
  `AskUserQuestion` in Claude Code. Zonder vraagtool wordt het een genummerde lijst in tekst.
- De agent controleert eerst de vereisten en installeert niets zonder akkoord: geen plugin, geen
  tool, geen binary.
- De agent laat de diff van `lighthouse init` zien en vraagt akkoord voordat er iets wordt
  geschreven. Tot slot moet `lighthouse init --upgrade` `No changes.` geven.
- Op de site staat een plakblok met copy-knop. `getting-started.md` linkt naar de handleiding.
- Tests pinnen de feiten in de handleiding vast aan de bron, zodat drift in CI rood wordt.

## Non-goals

- Geen plakzin voor bijwerken of verwijderen. Alleen installeren.
- Geen `llms.txt` in de root van de site. Dat komt pas als er een tweede doc voor agents is.
- Geen generator die de handleiding uit `requires.toml` of `init.go` opbouwt.
- Geen wijziging aan `lighthouse init`: niet in de flags, niet in de prompts, niet in de
  voorstellen. De handleiding beschrijft het gedrag dat `init` nu heeft.
- Geen eigen pad per harness. Er is één pad, en een korte opmerking waar Claude Code verschilt
  van Codex of OpenCode.
- Geen serverinstallatie (`docs/install.md`: serve, systemd, tailnet).

## Anti-goals

- Een agent die iets installeert zonder dat de gebruiker ja zei. Dit is de no-go van de milestone.
- Een agent die zonder akkoord in `AGENTS.md` of `CLAUDE.md` schrijft. Dit is ook een no-go van de
  milestone.
- Een handleiding die flags of tellingen noemt die niet meer kloppen, terwijl CI groen blijft.
- `sudo`, of iets buiten het account van de gebruiker.

## Randvoorwaarden

- **De site**: statisch, `docs/site/`, gepubliceerd door `.github/workflows/pages.yml` vanaf
  `main`. Pages serveert elk bestand onder `docs/site/`, dus ook een `.md`-bestand. De repo is
  publiek. De eerste release is `v0.2.0`.
- **De tekst kan voorlopen op de binary**: Pages publiceert vanaf `main`, de agent downloadt de
  laatste release. Daarom staat de regel in de handleiding: gebruik alleen flags die
  `lighthouse init --help` toont.
- **Het gedrag van `init`** (`internal/initcmd/initcmd.go`):
  - Zonder `--yes` toont `init` de diff en vraagt `Apply these changes? [y/N]`. Zonder TTY telt
    een lege stdin als nee: er wordt niets geschreven en de exitcode is 1.
  - `--upgrade` zonder iets te doen geeft precies `No changes.`.
  - Een tweede `init` zonder `--upgrade` weigert.
  - `--upgrade` op een unit die lokaal is gewijzigd weigert zonder `--force`.
  - `--pack` en `--no-pack` mogen herhaald worden. `init` print `Packages: …` vóór de diff.
- **Wat `init` voorstelt**:
  - `[project] language` = `en`.
  - `[forge]` uit de `origin`-remote. Zonder GitHub-remote waarschuwt `init`.
  - Een `[test_gate]` stelt `init` alleen voor via het python-pakket.
  - `[forge.board]` stelt `init` nooit voor.
- **Vereisten**: de tabel in `docs/getting-started.md` (*Requirements*) en de
  `pack/*/requires.toml`'s. Negen units van `core` vragen `superpowers`
  (`pack/core/requires.toml`). `codex` is optioneel, voor de twee adversarial reviews en
  `lighthouse usage codex`.
- **Harnesses**: `init` schrijft skills voor Claude Code (`.claude/skills`) en voor Codex/OpenCode
  (`.agents/skills`). Hooks draaien alleen in Claude Code.
- **Platforms**: Linux is getest. Op macOS zijn de hooks nog niet getest (`docs/install.md`).
  Native Windows is niet getest.
- **Siteregels**: elke relatieve link moet resolven (`tests/test_site_pages.py`). Elke pagina
  heeft `lang="en"`. Site-JS gebruikt geen `innerHTML`. Lokale js/css hebben het `?v=`-token
  (`python -m tests.site_version`).
- **Repo-regels**: links in een `.md`-bestand zijn relatief of `https://`, nooit een absoluut
  pad. Elke PR heeft een fragment in `changelog.d/`. Een wijziging in alleen `docs/` raakt de
  dogfood-check niet.

## Aanpakken

1. **Een handgeschreven `docs/site/agents/install.md` met tests die de feiten vastpinnen.**
   Gekozen. Het volgt het patroon dat `tests/test_getting_started.py` al heeft voor de
   startgids. De tekst blijft goed leesbaar voor een agent, en drift wordt rood in CI.
2. **De handleiding genereren** uit `requires.toml` en `init.go`. Afgewezen. De vragen en
   aanbevelingen blijven handwerk, dus je krijgt een sjabloon plus een generator voor een paar
   namen en getallen. Dat is meer machinerie dan het oplevert.
3. **Optie 1 plus een `llms.txt`**. Uitgesteld. Geen AC vraagt erom. Het is pas zinvol als er een
   tweede doc voor agents is.

Corneel koos optie 1.

## Besluiten

### 1. Plek en URL

Het bestand is `docs/site/agents/install.md`. De URL is
`https://corneeldh.github.io/lighthouse/agents/install.md`. Platte markdown, zonder HTML-pagina
eromheen.

### 2. De plakzin

De plakzin staat precies zo op de site:

```text
Read https://corneeldh.github.io/lighthouse/agents/install.md and install lighthouse in this repo.
```

Het plakblok staat alleen op `install.html`, bovenaan en vóór de handmatige installatie, met een
copy-knop. Hergebruik de bestaande copy-knop uit `download.js`. `index.html` krijgt geen
plakblok: sinds #156 zet die pagina het waarom eerst, en `test_landing_page_puts_the_why_first`
verbiedt een `<pre>`. De knop "Install lighthouse" op `index.html` leidt al naar `install.html`.

De footerlink "Agent guide (coming, #88)" wordt op beide pagina's een link naar
`agents/install.md`. De zin "Next: run `lighthouse init`" op `install.html` wijst ook naar de
handleiding.

### 3. De opbouw van de handleiding

De handleiding is geschreven aan de agent, in de tweede persoon en in de gebiedende wijs. Ze heeft
deze volgorde:

1. **Wat dit is**, in één alinea, en de bron. De officiële URL is de URL hierboven. De binaries
   komen van `github.com/CorneeldH/lighthouse/releases`.
2. **Ground rules**:
   - Leg elke stap uit voordat je hem uitvoert.
   - Installeer, download of schrijf niets zonder een expliciet ja. Geen antwoord betekent stop.
   - Nooit `sudo`. Alles komt in het account van de gebruiker (`~/.local/bin`).
   - Gebruik alleen subcommands en flags die `lighthouse --help` of `lighthouse init --help`
     tonen. Ontbreekt er een, stop en meld het.
   - Wijzig `AGENTS.md` en `CLAUDE.md` alleen via de goedgekeurde diff van `init`.
3. **Vragen stellen**: in Claude Code met `AskUserQuestion`, het aanbevolen antwoord als eerste
   optie met "(Recommended)" erachter. Zonder vraagtool als genummerde lijst in tekst, met het
   aanbevolen antwoord eerst. Stel één keuze tegelijk.
4. **Eerst kijken**, alleen lezen en zonder te vragen:
   - OS en architectuur;
   - staat `lighthouse` al op het `PATH`, en welke versie;
   - is dit een git-repo met een GitHub-`origin`;
   - is lighthouse hier al geïnstalleerd (`lighthouse.toml`, `.lighthouse/manifest.toml`);
   - welke vereisten ontbreken (de tabel uit de startgids, ingekort tot naam, pakket en wat je mist
     zonder);
   - welke talen de repo gebruikt.
   Meld de bevindingen in één blok.
5. **Keuzes**, zie besluit 4.
6. **Binary**: download en controleer tegen `SHA256SUMS`, met hetzelfde blok als de startgids,
   per platform.
7. **`init`**: zie besluit 5.
8. **Controle**: `lighthouse init --upgrade` geeft `No changes.`.
9. **Eindrapport**: zie besluit 6.
10. **Bij een fout**: de bekende meldingen en wat je dan doet. Bijvoorbeeld
    `missing dependency: …`, een weigering omdat `init` al gedraaid heeft, een lokaal gewijzigde
    unit, en geen GitHub-remote.

Is lighthouse al geïnstalleerd? Dan stopt de agent en meldt dat. Bijwerken valt buiten deze
handleiding.

### 4. De keuzes

Elke keuze heeft opties en een aanbevolen antwoord. Een keuze mag uit een paar vragen bestaan.

| # | Keuze | Opties (aanbevolen eerst) | Opmerking |
|---|---|---|---|
| 1 | Binary downloaden naar `~/.local/bin` | ja · nee, ik installeer zelf | Bij "nee" wacht de agent tot `lighthouse` op het `PATH` staat. |
| 2 | Ontbrekende vereisten | per tool: de gebruiker installeert zelf · overslaan | De agent noemt het installatiecommando, maar voert het alleen uit na een ja. |
| 3 | Superpowers | ja · nee | Zonder Superpowers werken negen skills niet, ook de versies in `.agents/skills`. De agent noemt het commando; de gebruiker voert het uit, of geeft akkoord. |
| 4 | Codex voor adversarial reviews | ja, als je een abonnement hebt · nee | Zonder Codex stoppen de twee reviews met `CODEX_MISSING`. |
| 5a | Taal van de artefacten (`[project] language`) | `nl` of `en`: aanbevolen is de taal waarin de gebruiker nu met de agent praat, en `en` bij elke andere taal | Issues, PR's en commits komen in deze taal. `pack/core/lib/config.py` kent alleen `nl` en `en` (`LANGUAGES`); een andere waarde is een `ConfigError`. |
| 5b | Code in het Engels (`[project] code_language = "en"`) | ja · nee (niet zetten) | Alleen vragen als 5a `nl` is. `en` is de enige waarde die de config accepteert. |
| 6 | Pakketten | wat `init` kiest · per pakket aan of uit (`--pack`/`--no-pack`) | De agent leest eerst `Packages: …` en vraagt per afwijking. |
| 7 | Test-gate (`[test_gate] command`) | een commando uit de repo afgeleid · een ander · uit (`command = ""`) | Zonder gate komt elke commit door met "nothing was tested". |
| 8 | Board (`[forge.board]`) | nee, voor de eerste keer · ja | Bij "ja" verwijst de agent naar de startgids; hij vult de vier ID's niet zelf in. |

Codex en OpenCode krijgen dezelfde keuzes, met twee opmerkingen:

- Bij keuze 3 verwijst de agent naar de eigen installatie-instructies van Superpowers voor die
  harness. `internal/deps/deps.go` herkent Superpowers alleen via
  `~/.claude/plugins/installed_plugins.json`. Daarom meldt `init` daar altijd
  `missing dependency: superpowers`, ook als het geïnstalleerd is. De handleiding zegt dat die
  melding daar verwacht is.
- Bij keuze 7 meldt de agent dat de gate niet vanzelf draait. Zonder hooks moet de gebruiker het
  commando zelf draaien.

### 5. `init` draaien

1. Bouw het commando met de `--pack`/`--no-pack` uit keuze 6.
2. Draai het één keer zonder `--yes`, met een lege stdin. De diff komt in beeld, er wordt niets
   geschreven, en de exitcode is 1.
3. Toon de diff aan de gebruiker, of vat hem per bestand samen en bied de volledige diff aan. Vraag
   akkoord.
4. Na het ja: hetzelfde commando, met één `y` op stdin (`printf 'y\n' | lighthouse init …`), en
   niet met `--yes`. Reden: na de diff vraagt `init` op een machine met Tailscale ook
   `Write ~/.config/lighthouse/host.toml? [y/N]` (`offerHost` in `internal/initcmd/host.go`). De
   lege run uit stap 2 komt daar nooit, dus de agent heeft die vraag niet getoond. `--yes` zou
   `host.toml` dan zonder akkoord schrijven. Met één `y` leest de tweede vraag EOF, en dat telt
   als nee. `host.toml` hoort bij `lighthouse serve`, en dat valt buiten deze milestone.
5. Zet de antwoorden van keuze 5, 7 en 8 in `lighthouse.toml`.
6. Draai `lighthouse init --upgrade` tot de uitvoer `No changes.` is. Toon elke diff die daar
   tussenzit en vraag opnieuw akkoord.

### 6. Eindrapport

Het eindrapport is een vast blok dat de agent tot slot laat zien:

- de versie (`lighthouse version`);
- de pakketten;
- de gestelde vragen met de gekozen antwoorden;
- de bestanden die zijn geschreven;
- de uitvoer van de laatste `init --upgrade`;
- wat er nog openstaat (vereisten die zijn overgeslagen, het board);
- de volgende stap: `/lighthouse-start-issue <nr>` op een eerste issue (zie de startgids,
  *Your first issue*).

Dit blok is ook het bewijs voor de PR: de vragen en antwoorden staan erin.

### 7. Tests

Een nieuw `tests/test_agent_guide.py`, in de stijl van `tests/test_getting_started.py`:

- Elke `--flag` in de handleiding bestaat in `internal/cli/init.go`. Gebruik dezelfde regex als
  `init_flags()`. `yes` en `upgrade` moeten erin staan.
- Het aantal skills dat Superpowers nodig heeft klopt met `pack/core/requires.toml`.
- Elke tool uit een `pack/*/requires.toml` staat in de handleiding.
- `No changes.` staat letterlijk in de handleiding én in `initcmd.go`.
- De plakzin staat op `install.html`. Haar URL wijst naar een bestand dat bestaat onder
  `docs/site/`. `getting-started.md` noemt dezelfde zin.
- De taalopties in de handleiding zijn precies `LANGUAGES` uit `pack/core/lib/config.py`.
- De handleiding noemt `--yes` niet als stap: `init` krijgt zijn akkoord via stdin (besluit 5).
- De handleiding is in het Engels en heeft geen relatieve links die nergens heen wijzen.

Aan te passen: `test_footer_links` in `tests/test_site_download.py` pint nu `issues/88`. Daar komt
`agents/install.md` voor in de plaats.

### 8. Bewijs in een schone repo

De handleiding wordt bewezen met één run (AC 5), zoals Corneel besloot:

- **Wie en waar**: Corneel, op macOS, in een nieuwe sessie van Claude Code. Hij beantwoordt de
  vragen live. De run test zo ook de darwin-binary en de hooks op macOS, die nog niet getest zijn.
- **Repo**: een bestaande eigen GitHub-repo van Corneel, met `origin`, waarin lighthouse nog niet
  geïnstalleerd is. "Schoon" betekent hier: zonder lighthouse. Dat wijkt af van de letter van
  AC 5 ("een schone repo"); de PR noemt dat.
- **Binary**: release `v0.2.0`. Er komt geen nieuwe release vooraf. De repo gebruikt geen Node,
  want `pack/node` zit niet in `v0.2.0`.
- **URL**: Pages publiceert pas na de merge. De run gebruikt daarom de raw-URL van de branch:
  `https://raw.githubusercontent.com/CorneeldH/lighthouse/issue-88-agent-installatiehandleiding/docs/site/agents/install.md`.
  Die URL staat alleen in de plakzin van de run, niet in de handleiding.
- **Bewijs**: het eindrapport en een samenvatting van de vragen gaan in de PR.

Wat er in die run misgaat, wordt eerst in de handleiding gerepareerd. Daarna volgt een nieuwe run.
De PR is pas klaar na een groene run. Deze stap komt in het plan als laatste taak, door Corneel.

### 9. Startgids

`docs/getting-started.md` krijgt bovenaan, bij *Install*, één zin: liever via je agent? Plak de
plakzin, en een link naar de handleiding.

## Aan de bouwer

- Lees de exacte stdin- en exitcode-afhandeling van `init` na in `initcmd.go`, voordat stap 2 van
  besluit 5 vastligt.
- Controleer dat `ask` bij EOF `false` geeft, zodat stap 4 van besluit 5 `host.toml` echt
  overslaat. Pin dat in een test als die er nog niet is.
- Kijk of `init` taalafhankelijke inhoud schrijft. Zo ja, dan maakt stap 6 van besluit 5 een
  echte diff. Dat mag, mits de gebruiker die ziet.
- Zet in de handleiding per platform het downloadblok uit `install.html`. Native Windows: meld dat
  de hooks bash nodig hebben en raad WSL aan.
- Hergebruik `addCopyButtons` uit `download.js` voor het plakblok. Voeg geen tweede
  copy-implementatie toe.
- Draai `python -m tests.site_version` na een wijziging aan een html- of js-bestand.
- Voeg een fragment toe in `changelog.d/`.

## Keuzes van Claude (niet door Corneel besloten)

- De URL `agents/install.md`, en platte markdown zonder HTML-pagina eromheen.
- Het plakblok alleen op `install.html`; `index.html` krijgt alleen de footerlink.
- Akkoord voor `init` via één `y` op stdin, niet via `--yes`, zodat `host.toml` niet ongevraagd
  geschreven wordt.
- Taal alleen `nl` of `en`, met `en` als aanbeveling bij elke andere taal.
- Superpowers ook vragen onder Codex en OpenCode, met uitleg bij de vaste melding
  `missing dependency: superpowers`.
- De exacte tekst van de plakzin.
- De opties en aanbevelingen bij keuze 2, 5a, 5b, 7 en 8. Corneel bevestigde de lijst als geheel,
  plus dat taal en pakketten meer dan één vraag mogen zijn.
- Stop als lighthouse al geïnstalleerd is, in plaats van bij te werken.
- Native Windows: meld en raad WSL aan, in plaats van te weigeren.
- De testlijst in besluit 7.
