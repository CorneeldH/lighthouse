# Ontwerp: kaarten van lighthouse, en de startgids

Datum: 2026-10-27 · Milestone: Kaarten: lighthouse in beeld (#99–#104) · De tekstgids (§5) is
#87, in milestone Product: eerste versie voor anderen · Hosting hangt af van #93 en #89. Mockup:
[`2026-10-27-kaarten-mockup.html`](2026-10-27-kaarten-mockup.html). Die laat de vorm van de
ketenkaart zien. De inhoud ervan is een schets, geen bron.

De zes kaarten heten hieronder K1 t/m K6:

| | Kaart | Issue |
|---|---|---|
| K1 | de keten, plus de site en de gedeelde component | #99 |
| K2 | binnen een sessie (eerst: de vangrails) | #100 |
| K3 | je repo na `init` | #101 |
| K4 | het systeem en de roadmap | #102 |
| K5 | per harness | #103 |
| K6 | de configuratie | #104 |

#100 t/m #104 wachten op #99, omdat ze op de component bouwen.

## 1. Waarom

Er is geen gids voor `lighthouse init`. [`docs/install.md`](../../../install.md) gaat alleen over
`serve`. De vereisten staan verspreid over `pack/*/requires.toml`. Een developer die niet Corneel
is, moet zonder hulp van installatie tot eerste PR komen (DoD van de milestone).

In de brainstorm van 2026-10-27 werd het breder:

- **De gids moet visueel zijn** (Corneel). Voorbeeld: de intake-verkenner van
  personal_ai_agent (CorneeldH/personal_ai_agent#1594). Die spec bouwt de verkenner bewust als
  sjabloon, ook voor "de lighthouse-keten".
- **De kaarten leggen heel lighthouse uit**: wat het biedt (skills, hooks, agents,
  GitHub-koppelingen), wat uit Superpowers komt, en waar en hoe elk onderdeel start.
- **Een deel werkt als roadmap** (Corneel). Wat er nog niet is, staat er gemarkeerd op. Op
  termijn blijft de roadmap vanzelf actueel (§9).

Het resultaat is een Engelse tekstgids plus een statische site met zes kaarten.

## 2. Doelen, niet-doelen, anti-doelen

**Doelen**

1. Een tekstgids in het Engels, `docs/getting-started.md`, die alle AC van #87 dekt.
2. Een statische site met zes kaarten (§4), met de drie niveaus uit §3 (#99–#104).
3. Elk onderdeel uit `pack/` staat op een kaart, met wie het levert en hoe het start.
4. Wat er nog niet is, staat er zichtbaar als "komt", nooit als werkend.
5. Tests falen als de kaarten en `pack/` uit elkaar lopen (§7).

**Niet-doelen**

- De ontbrekende handoffs bouwen. Ze komen vanzelf met M0–M5 (Corneel). Geen issue daarvoor
  aanmaken.
- Een generator die de kaartdata uit de repo afleidt. Dat is aanpak 2 (§6), voor later.
- Een live roadmap uit de GitHub-API (§9).
- De controller of `serve` bruikbaar maken voor gebruikers (No-Go van de milestone). Op de
  roadmapkaart staan ze alleen als "komt".
- De repo publiek zetten. Dat is #93.
- `init` laten stoppen op een ontbrekende tool. Het blijft een melding.

**Anti-doelen.** Het werk telt als mislukt, ook als het technisch werkt, als:

- een kaart stil veroudert. Een kaart die liegt is erger dan geen kaart;
- een kaart iets als werkend toont wat nog niet werkt;
- een nieuwe gebruiker de commando's niet kan kopiëren, omdat ze alleen op een kaart staan;
- het oogt als een technisch schema in plaats van een uitleg. De mockup is de ondergrens.

## 3. De vorm: drie niveaus

Elke kaart volgt de vorm van de intake-verkenner:

1. **Het geheel.** De kaart over de volle breedte.
2. **Een route of zone.** Klik een route (of zone). Die licht op, de rest dimt.
3. **Eén onderdeel.** Klik een chip. Eerst gewone taal: wat het doet. Daaronder:
   - wanneer het start;
   - wie het levert;
   - wat je mist zonder;
   - de status (hieronder).

**Kleur zegt van wie het is**: lighthouse-skill, Superpowers-skill, hook, GitHub. **Een teken
zegt hoe het start**:

- ⌨ jij vraagt erom;
- ↪ een andere skill roept het aan;
- ⚡ een tool-call;
- ⚙ een GitHub-event.

**Drie statussen**, op elke kaart dezelfde:

| Status | Betekenis | Uiterlijk |
|---|---|---|
| `works` | staat op `main` en draait | vol |
| `planned` | heeft een issue of milestone | gestippeld, met `#<nr>` of `M<n>` |
| `idea` | nog zonder issue | gestippeld, grijs |

Een chip zonder bewijs dat hij werkt, krijgt nooit `works`.

Iets wat op `main` draait maar niet voor gebruikers bedoeld is in deze milestone, zoals `serve`,
het SPA-skelet en de stukken van de controller die er al zijn, krijgt `works` plus het label
`not in first release`. Zo liegt de kaart niet de ene of de andere kant op (keuze van Claude).

## 4. De zes kaarten

Corneel koos voor alle zes, elk als eigen issue met een eigen PR, samen een milestone.

### K1 — De keten (stroom)

Dit is de mockup. Zeven kolommen:

1. Issue
2. Pick up
3. Spec
4. Plan
5. Build
6. PR
7. Close

Elk onderdeel staat in de fase waarin het meedoet. De band "Always on" met usage-guard,
instrument en worktree-audit is na #100 vervallen (Corneel). De keten toont de stroom van idee
naar main. usage-guard staat op K2. instrument en worktree-audit horen bij geen fase en krijgen
een plek in het referentiedeel van K4.

**Routes**: de uitgangen van `lighthouse-route-issue`, plus een rode PR:

| Route | Pad |
|---|---|
| spec first (4c) | brainstorming → spec-review → plan → plan-review → uitvoering |
| plan (4b) | writing-plans → plan-review → uitvoering |
| small job (4a) | direct naar Build |
| bug (2) | systematic-debugging |
| known recipe (1) | de skill die het issue sluit |
| red PR | fix-pr → receiving-code-review |

**Handoffs** zijn geen chip. Een handoff is een badge op elke sprong omhoog naar de volgende
fase (Corneel): daar begint een nieuwe sessie. Een volle badge staat waar lighthouse het al doet.
Dat is na de spec-review en na de plan-review. Alle andere badges zijn `planned`.

**Pakketten**: onder de kaart staat het lijstje taalpakketten. Per pakket:

- wanneer `init` het voorstelt;
- welke skill het geeft;
- welke hooks het zet.

| Pakket | Voorgesteld als | Skill | Hooks |
|---|---|---|---|
| core | altijd | 12 skills, 2 agents | 8 hooks, PR-template, workflow `unblock-dependents` |
| go | `go.mod` in de root | go-writing | gofmt (na edit), go-check (bij stop) |
| python | `pyproject.toml` in de root | python-writing | ruff-format (na edit), ty-check (bij stop); zet de test gate |
| shell | git houdt een `.sh`/`.bash` bij | shell-gates | shellcheck (na edit) |
| expo | `package.json` noemt `"expo"` | refresh-expo-lockfiles | — |

De bron is `pack/*/detect.toml`, `pack/*/hooks/hooks.json` en `pack/*/skills/`. De pagina
rendert de tabel uit `docs/site/data/packs.json`. Test 7 uit §7 houdt hem bij, ook de aantallen
(keuze van Claude).

### K2 — Binnen een sessie (stroom)

Na #100 heet K2 "Inside a session" (`session.html`), niet meer de vangrails (Corneel). De
kaarten zijn twee abstractieniveaus:

- K1 volgt het werk van idee naar main, fase na fase, over sessies heen.
- K2 zoomt in op één sessie.

Een losse referentie van alle onderdelen (skills, hooks, agents) hoort bij K4.

Dit is de levensloop van één beurt. De kolommen zijn de momenten van een beurt in Claude Code:

1. `UserPromptSubmit`
2. een skill laadt: de schrijfskills (go-writing, python-writing, shell-gates, testing) laden op
   hun beschrijving, voordat het model schrijft
3. `PreToolUse`
4. de tool-call
5. `PostToolUse` / `PostToolUseFailure`
6. `SubagentStop`
7. `Stop`

De koppen schrijven de eventnamen met spaties, omdat de kolomkop in hoofdletters staat.

Elke hook staat op zijn event, met de matcher (`Bash`, `Edit|Write`, of alles). Elke hook toont
ook zijn gedrag:

- **blokkeert**, met exit 2 (`usage-guard` met `{"decision": "block"}` en exit 0);
- **waarschuwt**;
- **formatteert**;
- **legt vast** voor een latere hook (`bash-write-gates pre`, toegevoegd in #100).

Routes zijn situaties, zoals:

- "ik commit op main";
- "ik commit met rode tests";
- "ik maak een PR zonder bewijs";
- "mijn limiet is bijna op";
- "ik schrijf een dode link";
- "ik schrijf code die een gate weigert".

De knoppen staan van de route met de meeste chips naar die met de minste. Een route over
schrijven begint bij de skill die de norm vooraf geeft. Daarna volgt de hook die achteraf
controleert.

Zo beantwoordt de kaart "waarom houdt hij me nu tegen?". De bron is `pack/*/hooks/hooks.json`.
De beschrijvingen staan in `CLAUDE.md` onder *Hooks*.

### K3 — Je repo na `init` (plattegrond)

Dit is geen stroom maar een echte kaart (Corneel). Het is een plattegrond van de repo van de
gebruiker na `lighthouse init`. Elk gebied is een plek waar `init` schrijft:

- `AGENTS.md` (managed block);
- `CLAUDE.md`;
- `lighthouse.toml`;
- `.claude/hooks/lighthouse/`;
- `.claude/skills/lighthouse-*/`;
- `.agents/skills/lighthouse-*/`;
- `.claude/agents/`;
- `.claude/settings.json` (samengevoegd);
- `.lighthouse/` (`lib`, `scripts`, `headings.toml`, `manifest.toml`);
- `.github/pull_request_template.md`;
- `.github/workflows/lighthouse-*.yml`.

Elk gebied krijgt drie kenmerken:

- **van wie**: van lighthouse (overschreven bij `--upgrade`), gedeeld (alleen het blok of de
  sleutels van lighthouse), of van jou;
- **welk pakket**;
- **wie het leest**: welke skill of hook.

Een zone toont wat `init --upgrade` raakt. De bron is `internal/packs/packs.go` (layout) en
`internal/initcmd/initcmd.go`. Een schets van deze kaart ontbreekt nog; de bouwer maakt hem in de
eerste stap en Corneel keurt hem (§10).

### K4 — Het systeem en de roadmap (plattegrond)

Lighthouse als geheel. De zones zijn:

- de binary (`init`, `serve`, `usage`);
- de pakketten;
- de controller en dispatcher;
- de SPA;
- de omgevingen.

"De omgevingen" zijn waar lighthouse draait: `deploy/` nu, en de omgevingen van M3 en M4.
Waar de grens tussen de zones precies ligt, beslist de schets (§10). De milestones liggen er als
gebieden over, M0–M5 en Product. De issues zijn de chips. De status
uit §3 doet het werk: `works` voor wat gesloten en gemerged is, `planned` voor open issues.

K4 krijgt ook een referentiedeel: elk onderdeel los, per pakket, zonder stroom (Corneel, na
#100). Daar staan ook de onderdelen die op geen stroomkaart passen, zoals instrument en
worktree-audit.

Dit is de roadmap. Voor nu schrijf je de data met de hand, uit de milestones en issues op het
moment van bouwen. Hij moet "leuk actief te houden" zijn (Corneel). Daarom krijgt de data per
chip een `issue`-veld, zodat een generator hem later kan bijwerken (§9).

### K5 — Per harness (tabelkaart)

Wat werkt in Claude Code, Codex en OpenCode:

| Onderdeel | Claude Code | Codex | OpenCode |
|---|---|---|---|
| skills | `.claude/skills` | `.agents/skills` | `.agents/skills` |
| `AGENTS.md` | via `@AGENTS.md` in `CLAUDE.md` | ja | ja |
| hooks | alleen hier | geen | geen |
| adversarial reviews | — | `codex` voert ze uit | — |

De hooks draaien alleen in Claude Code (`AGENTS.md`, *Outside Claude Code*). Zonder hooks zijn de
regels in `AGENTS.md` de enige vangrail. Dan draai je de test gate zelf.

### K6 — Configuratie (verbindingen)

Elke sleutel in `lighthouse.toml` met zijn verplichting en standaard, verbonden met de skills en
hooks die hem lezen. De bron is `pack/core/lib/config.py`: `SCHEMA` en `DEFAULTS`, plus de
sleutels die `config.py` buiten `SCHEMA` controleert: `project.language`, `forge.kind`,
`forge.repo` en `forge.board.*`.

De kaart markeert de sleutels die een nieuwe gebruiker moet of kan zetten:

- `[project] language`;
- `[forge] kind` en `repo`;
- `[test_gate] command`;
- optioneel `[forge.board]`.

De rest staat er als "geavanceerd". Dat geldt ook voor `usage_guard.controller`: dat is een
instelling van de usage-guard, geen controller-optie (keuze van Claude). Controller-opties staan
er niet op. Die komen later
(Corneel), als de controller er is.

## 5. De tekstgids (#87)

`docs/getting-started.md`, in het Engels. Hij linkt naar een kaart zodra die er is. De commando's staan hier als tekst om te kopiëren, niet
alleen op een kaart. Inhoud, in deze volgorde:

1. **Wat lighthouse is**, in drie zinnen, met een link naar de kaarten.
2. **Vereisten**, als tabel (AC2). De tabel is afgeleid uit `pack/*/requires.toml`, met Claude
   Code met de hand erbij (het is geen sleutel in `requires.toml`). Per rij staat wat er wegvalt
   zonder. `python3` en `tomllib` vallen samen in één rij "Python 3.11+".
3. **Installeren.** De download staat er als eerste route, met de status: release-binaries komen
   (milestone). Tot dan is `go install` de route, met `GOPRIVATE` zolang de repo private is (#93).
   Installeer een tag, want een build van `main` meldt `dev` en schrijft dat in
   `.lighthouse/manifest.toml`.
4. **`lighthouse init`** in je eigen repo:
   - het toont per bestand een diff en vraagt `Apply these changes? [y/N]`;
   - `--yes` slaat de vraag over;
   - `--upgrade` werkt een bestaande installatie bij;
   - het meldt een ontbrekende tool en gaat dan door.

   Wat `init` schrijft en waar staat als korte lijst, met een link naar K3.
5. **`lighthouse.toml`**: de sleutels uit K6 die je moet of kunt zetten, met een voorbeeld (AC3).
6. **Je eerste issue**: van `/lighthouse-write-issue` en `/lighthouse-start-issue` tot
   `/lighthouse-verify-pr`. Bij elke stap staat wat je ziet en welke hook kan ingrijpen, met een
   link naar K1.

De README linkt naar de gids. De status-alinea zegt wat zonder controller al bruikbaar is (AC4):
de pakketten, met worktrees, test gate, PR-bodies met bewijs en adversarial reviews.

## 6. Aanpak

| | Aanpak | Ruil |
|---|---|---|
| **1** | Statische site, data met de hand, dekkingstests | De inhoud schrijf je met de hand, maar hij kan niet stil verouderen. Pages bouwt niets. |
| 2 | Zoals 1, maar een generator leidt de inventaris af uit `pack/`; je schrijft alleen de laag eroverheen | Blijft vanzelf juist, maar je bouwt en onderhoudt een generator en een formaat voor die laag. |
| 3 | Een docs-sitegenerator (Zensical, MkDocs) met de kaarten als component | Navigatie en zoeken, maar een nieuwe toolchain en een buildstap, alleen voor wat extra navigatie. |

**Gekozen: 1** (Corneel). Aanpak 2 volgt op termijn, zeker voor de roadmap. De data krijgt daarom
nu al een vorm die een generator later kan vullen (§8).

## 7. Tests

In `tests/`, Python (`uv run pytest -q`), code en namen in het Engels. Het patroon is
`tests/pack/test_skill_contracts.py`: lees `requires.toml` met `tomllib`, parametriseer, en
assert met de lijst van wat ontbreekt.

Test 1 hoort bij #87. Tests 2–7 horen bij de kaart die ze raken: dekking van skills, agents en
workflows, en tests 5–7 bij #99; hooks en test 3 bij #100; de plekken van `init` bij #101;
test 4 bij #104.

1. **Vereistentabel (AC5 van #87).** Elke sleutel uit elke `pack/*/requires.toml` staat in de tabel van
   `docs/getting-started.md`. `python3` en `tomllib` tellen als één rij. Claude Code staat er ook.
   De test faalt als er een tool bijkomt in `requires.toml`.
2. **Dekking.** Elke skill (`pack/*/skills/*`), elke agent (`pack/core/agents/*`), elke hook-command
   uit `pack/*/hooks/hooks.json` en elke workflow (`pack/core/workflows/*`) staat in de data van
   K1, K2 of K3. Omgekeerd verwijst elke chip van het soort lighthouse-skill, hook of workflow naar
   iets wat bestaat. instrument en worktree-audit staan tot het referentiedeel van K4 op geen
   kaart. De test noemt ze bij naam.
3. **Hook-events.** Elke hook staat in K2 op het event en met de matcher die `hooks.json` noemt.
4. **Configuratie.** Elke sleutel uit `SCHEMA` in `config.py` staat in K6, plus
   `project.language`, `forge.kind`, `forge.repo` en de sleutels van `forge.board`.
5. **Status.** Een chip met `status: planned` heeft een `issue` of `milestone`. Een handoff-badge
   met `works` hoort bij een skill die `lighthouse-handoff` aanroept.
6. **Links.** Elke relatieve link in de site en de gids wijst naar een bestand dat bestaat. Voor
   `.md` doet `worktree-linkcheck` dat al bij schrijven; de test dekt ook de HTML. Een `source`
   is een GitHub-URL (§8); de test controleert dat het pad erin bestaat.
7. **Pakketten.** `packs.json` noemt elk pakket uit `pack/`, met de bestanden uit `detect.toml`, de
   skills uit `skills/` en de hooks uit `hooks.json`, en de aantallen kloppen.

Elke test moet falen als je de productie breekt. Een voorbeeld: haal een skill uit de K1-data, en
test 2 wordt rood (`lighthouse-testing`).

## 8. Bouw

- **Plaats**: `docs/site/`.
  - `index.html` is de ingang, met de zes kaarten als tegels.
  - Per kaart is er een pagina, bijvoorbeeld `chain.html`.
  - Er is één gedeelde component, bijvoorbeeld `map.js` en `map.css`.
  - De data staat in `docs/site/data/*.json`.
- **Dataformaat: JSON.** De browser leest het zonder bibliotheek, Python met `json`. Elke chip
  heeft minstens deze velden:
  - `id`;
  - `kind` (`lighthouse`, `superpowers`, `hook`, `github`);
  - `trigger` (`you`, `skill`, `tool`, `github`);
  - `label`;
  - `what`;
  - `when`;
  - `without`;
  - `status`;
  - optioneel `issue` / `milestone`;
  - `source`, het pad in `pack/` waar het vandaan komt.

  Routes en handoffs zijn lijsten van `id`'s.
- **Vanilla JS en SVG**, zoals de mockup. Geen framework, geen buildstap. Een externe
  bibliotheek alleen met een vaste versie.
- **Stijl**: een eigen, neutrale stijl, licht en donker. Geen Npuls: die huisstijl hoort niet
  bij een product voor buiten (paia-spec §3 en §7).
- **Taal**: alles op de site en in de gids is Engels.
- **Hosting**: GitHub Pages, zodra #93 dat toelaat. Pages vanuit een branch serveert alleen `/`
  of `/docs`. Voor `docs/site/` is dus een Pages-workflow nodig. Die hoort bij de website (#89),
  die al automatisch deployen vraagt, en niet bij een kaartissue (keuze van Claude). Tot dan open je de site lokaal
  (`python3 -m http.server --directory docs/site`). De tests draaien hoe dan ook.
- **Links naar buiten de site** (keuze van Claude). Een `source` is een GitHub-URL naar `main`
  (`https://github.com/CorneeldH/lighthouse/blob/main/<pad>`). Een relatieve link naar
  `../../pack/` is dood zodra de site los geserveerd wordt. De gids noemt voor de kaarten het
  commando om ze lokaal te openen. Het Pages-adres komt erbij met #93.
- **Dogfood**: dit raakt `pack/` en `internal/` niet. Er is dus geen `init --upgrade` nodig.

## 9. Later

- **Aanpak 2**: een generator die de inventaris uit `pack/` afleidt. Zo blijft K1–K3 en K6
  vanzelf juist.
- **Roadmap uit GitHub**: K4 halen uit de milestones en issues. Dat kan in CI, als momentopname,
  of in de browser. Een publieke repo laat de API zonder login toe, met een rate limit.
- **Controller-opties** in K6, als de controller er is.
- **Een eigen merkstijl**, als lighthouse een naam en site krijgt (open vraag van de milestone).

## 10. Voor de bouwer

- De posities en de layout per kaart. De mockup is richting, geen maat.
- Een schets van K3 en K4 vóór je ze bouwt. Corneel keurt elke schets, want die kaarten zijn
  plattegronden en geen stroom.
- De precieze teksten per chip. Haal ze uit de skillbeschrijvingen en uit `CLAUDE.md` *Hooks*, en
  verzin geen gedrag. `code-language.sh` staat in `hooks.json` van core, maar niet in
  `CLAUDE.md` *Hooks*: haal zijn tekst uit het script.
- De vorm van de index.

## 11. Risico's

- **De appetite.** Daarom zijn de kaarten een eigen milestone (Large), los van "Product: eerste
  versie voor anderen" (Medium). #87 blijft daar, als tekstgids.
- **#93 is nog niet beslist.** Zonder publieke repo of Pages heeft de site geen adres. De website
  met download is #89. De gids en
  de tests werken zonder.
- **Teksten uit de skills verouderen.** De dekkingstest ziet een chip die ontbreekt, maar niet
  een tekst die niet meer klopt. Daarom heeft elke chip een `source`, zodat een review hem kan
  nalopen.
