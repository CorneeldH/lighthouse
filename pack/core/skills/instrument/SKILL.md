---
name: paia-instrumenteren
description: Beslis op welke laag een nieuwe instructie hoort — CLAUDE.md, een pad-gescopete .claude/rules/-rule, een skill of een hook — en dwing de pairing-regel af (geen hook zonder tegenhanger). Aanroepen met /paia-instrumenteren. LET OP — gaat het om het vakwerk van een skill zelf schrijven (frontmatter-syntax, voorbeelden, structuur), gebruik dan superpowers:writing-skills.
disable-model-invocation: true
metadata:
  binding: default
  origin: own
  source: docs/developer/planning/specs/2026-09-11-herkomst-gate-instrumenteren-design.md
  domain: factory
---

# Instrumenteren

Van "dit moet vastgelegd worden" naar één laag, één bestand, één meetcommando.
Ontwerp: `docs/developer/planning/specs/2026-08-22-instructielagen-rules-skills-hooks-design.md`.

## Domein

Hieronder valt: kiezen wáár een instructie woont, en het bestand in de juiste vorm neerzetten.

Hier niet: het vakwerk van een skill schrijven — dat delegeert deze skill aan
`superpowers:writing-skills`. En niet het bewaken achteraf: dat is
`scripts/hooks/check-instructies.sh`.

## Stap 1 — Extern eerst. Schrijf niets tot dit klaar is.

De goedkoopste skill is die van iemand anders. Kijk vóór je iets schrijft:

```bash
npx skills find "<onderwerp>"
npx skills add -l <owner>/<repo>     # inhoud tonen, niets installeren
```

Vaste plekken: `anthropics/skills`, `obra/superpowers`, de officiële marketplace.

Vind je iets bruikbaars: **vet de licentie vóór adoptie.** Dit wordt een gehost product en
kopiëren is de ene handeling die copyleft daadwerkelijk laat overslaan — de API-uitzondering
uit `AGENTS.md` geldt hier dus niet. Permissief (MIT/Apache-2.0/BSD/ISC/HPND) mag; geen
licentie te vinden → niet overnemen. Overgenomen materiaal krijgt `origin: extended` of
`origin: external` plus `upstream` met de bron én de licentie — zet de licentienaam achter
een `(` of een `,` (`(MIT, © Iemand)` of `, MIT`): een kale naam telt niet, want anders
matcht een licentienaam die toevallig in een pad voorkomt (`github.com/apache/airflow`,
`foo/mit-tools`) ook mee.

Overgenomen materiaal draagt ook `upstream-commit`: de volledige SHA (40 hex, dubbel gequote),
van de commit waar de kopie van **afstamt** — niet de HEAD van vandaag. Bij `origin: external`
komt daar `upstream-path` bij: het pad binnen de upstream-repo naar de skillmap of het
bronbestand, zonder voorloop-`/`. Zonder pin is achterlopen onzichtbaar, is een update geen
reviewbare diff en kan de bumpjob (#1140) niets vergelijken.

**Vendor-besluit:** `vendor/*/PROVENANCE.md` blijft mensentekst en de bron voor vendor — geen
tweede machinevorm. De `| Commit |`-regel is al regex-leesbaar, en
`test_upstream_commit_gelijk_aan_provenance` borgt dat skill en PROVENANCE niet uiteenlopen.

## Stap 2 — De herkomst-gate. Geen bron, geen instructie.

**De vraag:** waar komt de inhoud van deze instructie vandaan?

Geldige bronnen:

- een sessie waarin het daadwerkelijk misging, mét de correctie van de gebruiker — verreweg
  het meest voorkomende geval hier
- een gemeten defect: een rode CI-run, een hook die vuurde, een outage, een OOM
- git-historie van fixes, of een gesloten issue
- upstream- of leveranciersdocumentatie (Agent Skills-spec, systemd, Anthropic, een library)
- een spec of ADR onder `docs/developer/`
- de gebruiker die het in dit gesprek vertelt

**Het enige geval dat blokkeert: algemene modelkennis.** Geen bron betekent niet "dan schrijven
we het maar op" — dat levert tekst op die klínkt als kennis ("ga zorgvuldig om met fouten",
"documenteer je aannames") en die bij élke sessie context kost zonder ooit gedrag te veranderen.
Schrijf dan niets, en zeg dit:

> Er is nog geen materiaal om deze instructie op te baseren. Doe het één keer echt — ik noteer
> je correcties — en daarna destilleren we de norm daaruit. Anders krijg je een instructie die
> klinkt als kennis maar het niet is.

**Geen ceremonie.** Is de bron evident — de gebruiker corrigeerde je twee berichten geleden —
dan is één regel die 'm benoemt genoeg. De gate stopt alléén bij *géén* bron. Een gate die bij
elke triviale `CLAUDE.md`-regel een verhoor start, wordt de eerste stap die iedereen overslaat.

Deze gate geldt voor **alle vijf de lagen** van stap 3, niet alleen voor skills. Hij staat vóór
de laagkeuze omdat je zonder bron geen laag kiest: dan schrijf je niets. Wordt het een skill,
dan landt het antwoord in het `source`-veld (stap 5); voor een `CLAUDE.md`-regel of een rule
landt het in de commitmelding of het issue — `CLAUDE.md` heeft geen frontmatter per regel en
een rule draagt één `paths:`-blok voor een heel onderwerp.

## Stap 3 — Kies de laag

Vijf plaatsen, met wezenlijk verschillende triggers en kosten:

| Plaats | Trigger | Altijd-aan kosten |
|---|---|---|
| root `AGENTS.md` | sessiestart (Claude Code via `@AGENTS.md`) | volledige body |
| root `CLAUDE.md` | sessiestart, alleen Claude Code | volledige body |
| nested `CLAUDE.md` | Claude leest een bestand in die map | geen |
| `.claude/rules/*.md` + `paths:` | Claude leest een bestand dat de glob matcht | geen |
| skill (ambient) | model beoordeelt relevantie op de description | alleen de description |
| hook | tool-call matcht | geen |

```
Moet dit afgedwongen worden, ook als het model anders besluit?
├── ja  → hook  (+ verplichte tegenhanger, zie stap 4)
└── nee
    │
    Hangt het aan een pad of bestandstype?
    ├── ja  → .claude/rules/<onderwerp>.md met paths:
    └── nee
        │
        Is het een procedure (stappen, beslislogica, meetcommando's)?
        ├── ja  → skill
        └── nee → root AGENTS.md  (alleen Claude Code? → root CLAUDE.md; houd het kort)
```

Eén extra tak, en die is het makkelijkst te missen: **moet het vuren terwijl er nog geen bestand
open is** — tijdens brainstormen, plannen, of het bedenken van een nieuwe dienst? Dan kan een rule
het niet, want die wacht op een file-read. Dat is het enige geval waarin een ambient skill wint van
een rule, en het is een echt geval: het patroon voor een nieuwe Beelink-dienst moet je kennen vóór
je het eerste bestand aanmaakt.

**Een rule zonder `paths:` is `CLAUDE.md` met extra stappen.** De winst zit volledig in de
scoping. Check 7 van `scripts/hooks/check-instructies.sh` blokkeert hierop.

## Stap 4 — Bij een hook: weiger zonder tegenhanger

**Hook en skill/rule zijn één regel op twee momenten.** De skill of rule draagt de norm, de
rationale en het meetcommando — vooraf, tijdens het schrijven. De hook draagt de afdwinging —
achteraf, bij de tool-call.

**Lever geen hook op zonder tegenhanger.** Weiger dat, ook als erom gevraagd wordt: zonder
tegenhanger leert de agent de regel uitsluitend door straf — retry-loops, verspilde tokens, en
bij een zwakker model een lus die niet convergeert omdat hij niet wéét wat de hook wil.
Omgekeerd is een norm "hard" noemen zonder hook een norm met een mooie titel.

Concreet: de skill of rule krijgt `binding: hard` en **noemt het hookpad letterlijk in zijn
body** (`.claude/hooks/<naam>.sh`). Check 8 toetst dat dat pad bestaat én in
`.claude/settings.json` geregistreerd staat — harnas-onafhankelijk: de check draait via
`scripts/hooks/check-instructies.sh`, niet via Claude Code zelf. Een hookscript dat nergens
geregistreerd is, draait nooit.

`.claude/settings.json` registreert de hook. **OpenCode draait die hooks niet** — de norm moet
daar dus in `AGENTS.md` als proza staan — dat laadt elk harnas, Claude Code via `@AGENTS.md`.

## Stap 5 — Het frontmatter-contract: vijf velden

Elf velden is audit-machinerie voor een org met meerdere bijdragers en een skill-PR-review.
Hier is één persoon en geen review-poort. Vijf velden verdienen het wél, elk omdat er een
**beslissing** aan hangt die anders zoekraakt:

| Veld | Waarden | Waarom dit veld bestaat |
|---|---|---|
| `binding` | `hard` \| `default` | `hard` is alleen geldig mét hook-tegenhanger. Machinaal te toetsen, en het maakt de pairing-regel controleerbaar in plaats van goedbedoeld |
| `origin` | `own` \| `extended` \| `external` | Bepaalt of er upstream-onderhoud en een licentieverplichting aan hangt |
| `upstream` | bron-URL; laat de sleutel weg als er geen is | Verplicht bij `extended`/`external`. Zonder dit veld is een overgenomen skill over drie maanden niet bij te werken en niet te attribueren |
| `upstream-commit` | volledige SHA, dubbel gequote | Hoort bij `upstream`, geen eigen beslissing: de commit waar de kopie van afstamt. Quote 'm — een SHA van alleen cijfers parseert anders als getal |
| `upstream-path` | pad in de upstream-repo, zonder voorloop-`/` | Hoort bij `upstream`; verplicht bij `external`, bij `extended` draagt `vendor/*/PROVENANCE.md` de bron |
| `source` | `"#914"` \| commit-sha \| pad in de repo \| publieke URL \| `self` | Het antwoord op de gate van stap 2. Zonder veld verdampt dat antwoord zodra het gesprek voorbij is, en is over drie maanden niet meer na te gaan of een norm ergens op stoelt |
| `domain` | `applicatie` \| `inhoud` \| `factory` \| `infra` | Milestone #58 wijst elk pad aan precies één domein toe. Het domein is niet af te leiden uit de locatie, want alle skills staan in dezelfde map. De indeling volgt wat de skill aanroept, niet zijn onderwerp |

`source` kent vijf vormen, en het verschil zit in wie erbij kan:

| Vorm | Voorbeeld | Consequentie |
|---|---|---|
| issue of PR | `source: "#914"` | de discussie is de bron en blijft leesbaar; quote 'm, anders leest YAML `#` als commentaar en is de waarde leeg |
| commit | `source: 68af7e22` | een gemeten fix; `git show` is het bewijs |
| pad in de repo | `source: docs/developer/planning/specs/2026-09-11-herkomst-gate-instrumenteren-design.md` | wijzigt dat bestand, dan hoort de skill mee te bewegen — en het pad is machinaal te toetsen |
| publieke URL | `source: https://agentskills.io/specification` | iedereen kan 'm nalezen; de skill mag samenvatten en doorverwijzen |
| `self` | `source: self` | de gebruiker vertelde het, nergens anders vastgelegd |

`self` is de ontsnapping en draagt daarom de zwaarste eis: er is **geen origineel** om op terug
te vallen, dus de skill moet de substantie zelf volledig dragen. Wie 'm gebruikt zonder die
substantie uit te schrijven, laat een verwijzing achter die nergens heen wijst.

Check 9 toetst de vorm repo-breed: een aanwezige `source` moet een van deze vijf zijn, en een
pad-vormige waarde moet bestaan op schijf. **Afwezigheid is geen fout** — bestaande skills
krijgen het veld niet alsnog. Dat "vanaf hier verplicht" is geen datumgrens maar het
`CONTRACT`-register in `tests/unit/test_instructielagen.py`: nieuw werk voegt daar zijn regel
toe, inclusief `source`.

`domain` is als enige veld **verplicht voor elke skill**, ook een overgenomen. Check 13 toetst dat
in beide spiegels een geldige waarde onder `metadata:` staat. Dat is de uitzondering op
"Afwezigheid is geen fout" en op "Geen terugwerkende migratie": #1161 heeft alle bestaande skills
ingedeeld. De maatstaf: roept de skill PKM-code uit `src/` of de apps in `apps/` aan, dan is het
`applicatie`; draagt hij alleen het vault-formaat, dan `inhoud`; is het gereedschap voor de
ontwikkelketen of de instructielaag, dan `factory`; hoort hij bij de Beelink-ruggengraat, dan
`infra`.

**Genest onder `metadata:`, nooit top-level.** De Agent Skills-specificatie
(https://agentskills.io/specification) staat top-level exact zes velden toe — `name`,
`description`, `license`, `compatibility`, `metadata`, `allowed-tools` — en wijst `metadata` aan
voor eigenschappen die de spec zelf niet kent: *"A map from string keys to string values.
Clients can use this to store additional properties not defined by the Agent Skills spec."* De
gate leest ze met `skill_meta()`, niet met `skill_field()`; die laatste eist kolom 1 en is er
voor de spec-velden. Onder `metadata:` zijn de waarden strings, en een sleutel zonder waarde
hoort **weg te blijven**, niet leeg te staan — `upstream` dat er niet is, is een ontbrekende
sleutel, geen lege regel `upstream:`.

Twee velden blijven wél top-level: `disable-model-invocation` en `argument-hint`. Claude Code
leest die daar en nesten breekt de werking. Dat is buiten de spec, bewust, en dat hoort er
expliciet bij te staan — niet stilzwijgend.

Bewust níét overgenomen uit het CEDA-schema: `type`, `subtype`, `scope`, `activation`,
`execution`, `verifies`, `id`, `version`. Die zijn af te leiden uit de locatie van het bestand
of uit de frontmatter die Claude Code zelf al kent, en een afgeleid veld dat met de hand wordt
bijgehouden loopt uiteen.

Dat argument geldt níét voor herkomst: die is uit niets af te leiden, en daarom is `source` het
enige veld dat alsnog is overgenomen.

Geen terugwerkende migratie: skills zónder de velden krijgen ze niet alsnog (behalve `domain`, zie
stap 5 hierboven). De vorm is wél
eenmalig rechtgetrokken (#686) — een top-level contractveld schendt de spec en is dus een
defect, geen legacy.

## Stap 6 — Craft delegeren

Het schrijven zelf gaat naar `superpowers:writing-skills`. Niet overschrijven — dat is generiek
vakwerk dat elders beter onderhouden wordt. Wat deze skill toevoegt is wat daar niet in staat:
de vijf plaatsen, de herkomst-gate, de pairing-regel, het naamcontract en de vijf velden.

Twee eisen die hier bovenop komen:

- **`paia-`-voorvoegsel**, altijd. Personal skills in `~/.claude/skills/` overschaduwen
  gelijknamige project-skills — omgekeerd aan `settings.json` — en dat gebeurt stil.
  Uitzondering: een gevendorde `origin: external`-skill, want hernoemen breekt daar de
  updateroute en de attributie.
- **Een negatieve grens in de description.** Descriptions staan permanent in context en het
  model kiest op gelijkenis. Vorm: `LET OP — gaat het om X, gebruik dan Y`. Twee eisen tegelijk:
  een scope-woord als los woord, én een andere skill bij naam. Check 4 toetst dit zodra twee
  descriptions vijf triggerwoorden delen.
- **Spiegel naar `.agents/skills/`**, in dezelfde gereviewde wijziging. Eigen skills nooit automatisch; een `origin: external`-skill mag via de bumplane-PR (`.github/workflows/skills-upstream-bump.yml`, #1140), beide spiegels in die ene PR.

## Stap 7 — Valideren

```bash
bash scripts/hooks/check-instructies.sh
uv run pytest tests/unit/test_instructielagen.py tests/unit/test_check_instructies.py tests/unit/test_skill_mirror.py -q
```

## KPIs

| Metric | Target | Meting |
|---|---:|---|
| Rules zonder `paths:` | 0 | `bash scripts/hooks/check-instructies.sh` (check 7) |
| Skills met `binding: hard` zonder geregistreerde hook | 0 | `bash scripts/hooks/check-instructies.sh` (check 8) |
| Overgenomen skills zonder upstream, upstream-commit of licentie | 0 | `bash scripts/hooks/check-instructies.sh` (check 9) |
| Woorden in `CLAUDE.md` + `AGENTS.md` | ≤ 3000 | `wc -w CLAUDE.md AGENTS.md` (regel `total`) |
| Ambient skill-oppervlak (tekens naam+description) | dalend | `uv run python scripts/measure_skill_surface.py --ambient .claude/skills` |
| Ambient skill-oppervlak per domein (welk domein laadt de context het meest) | dalend | `uv run python scripts/measure_skill_surface.py --ambient --per-domein .claude/skills` |
| Spiegelbreuk `.claude/skills` ↔ `.agents/skills` | 0 | `bash scripts/hooks/check-instructies.sh` (check 3) |
| Personal skills en agents in ~/.claude | 0 | `bash scripts/hooks/check-instructies.sh` (check 12) |

## Non-negotiable

- ✅ Extern zoeken vóór de eerste regel · ❌ schrijven en daarna ontdekken dat het bestond
- ✅ Elke rule met `paths:` · ❌ een ongescopete rule
- ✅ Hook mét tegenhanger · ❌ een hook opleveren omdat erom gevraagd werd
- ✅ Contractvelden onder `metadata:` · ❌ top-level, buiten de zes velden die de spec toestaat
- ✅ `paia-`-voorvoegsel · ❌ een kale naam die een personal skill kan overschaduwen
- ✅ Negatieve grens in de description · ❌ een description die alleen zegt wat hij wél doet
- ✅ Beide spiegels in dezelfde gereviewde wijziging · ❌ een ongereviewde kopie, of een eigen skill die automatisch bijwerkt

## Bekende valkuilen

- **Een rule vuurt op een *read*, niet op een *write*.** Een nieuw bestand heeft geen pad om te
  matchen. Voor het geval "nieuwe Beelink-dienst" draagt de skill het, niet de rule (#685).
- **`disable-model-invocation: true` haalt de description uit context** maar laat de skill met
  `/` bereikbaar. Alleen zetten op wat je toch bij naam aanroept; een skill die op natuurlijke
  taal moet vuren verliest daarmee zijn enige trigger.
- **De vault heeft geen `.git`.** Een instructie over vault-bestanden hoort nooit een
  git-commando te bevatten.
