# Systeemkaart: wat je hebt na installeren (K4, #102)

Uitwerking van K4 uit [het kaartenontwerp](2026-10-27-kaarten-design.md) (§4). Daar staat wat
voor alle kaarten geldt: niveaus, statussen (§3), tests (§7) en bouw (§8). Dit document vervangt
de eerste versie van deze spec (plattegrond met schillen plus *Over time*). Corneel keurde die
vorm af: alleen tekst, willekeurige blokjes, en een structuur die niet klopt met hoe je
lighthouse gebruikt.

## 1. Waarom

Een nieuwkomer weet niet wat er gebeurt als hij lighthouse installeert en in een repo gebruikt.
Wat moet je zelf doen, wat komt er waar, en hoe hangt het samen? De pagina beantwoordt dat in
één oogopslag, vanuit de gebruiker. Corneel: "leg uit wat je nu allemaal hebt als je Lighthouse
installeert en het in een repo gebruikt. Wat er dan allemaal gebeurt."

Twee plekken zijn voor lighthouse wezenlijk: je **werkstation** (de host) en je **repo**. De
pagina maakt duidelijk welke functionaliteit waar woont en hoe die zich tot elkaar verhoudt.

## 2. Doelen, niet-doelen, anti-doelen

**Doelen**

- Eén pagina, `system.html`, met een eenvoudig overzicht in drie stappen van links naar rechts:
  1. *Install once*: je werkstation;
  2. `lighthouse init`: wat je repo krijgt;
  3. *Run the loop*: wat er gebeurt als je werkt.
- Elk vak in het overzicht is een link, naar een stuk onder het overzicht of naar een andere
  pagina. Details staan niet in het overzicht zelf.
- Eén vaste set tekens en kleuren voor alle kaarten: het teken zegt **wat** iets is, de kleur
  **van wie** het komt (§5).
- De ketenkaart heet voortaan **The loop**.
- Eén referentiepagina die per soort onderdeel naar de plek in de repo linkt.

**Niet-doelen**

- Geen *Over time*, geen issues per milestone, geen roadmap op deze pagina. Hoe lighthouse er
  later uitziet (controller, omgevingen, SSH naar een andere machine) komt op een aparte pagina,
  later.
- Het dashboard (`serve`) en de controller (`tick`) staan niet op de pagina: ze zijn er niet
  voor gebruikers.
- Docs en site staan niet op de pagina.
- Geen uitgewerkte referentiepagina per skill, agent of hook. Voor nu linkt één pagina naar de
  mappen in de repo.
- Geen andere forge dan GitHub. Later moet dat een willekeurige git-forge kunnen zijn.

**Anti-doelen**

- Een overzicht vol detail. Corneel koos versie 1 van de mockup juist om het kale overzicht;
  versie 2, met installatiecommando's en toollijsten erin, was "veel minder duidelijk". Detail
  hoort onder het overzicht of op een andere pagina.
- Een teken dat op de ene kaart iets anders betekent dan op de andere.
- Iets tonen als werkend wat niet werkt. Alles op deze pagina bestaat op `main`.

## 3. De drie stappen

De namen op de site zijn Engels (kaartenontwerp §8). Pijlen ↓ en ↗ in de mockup gaven aan of
een vak naar een stuk eronder of naar een andere pagina linkt; de bouwer kiest hoe de pagina dat
toont.

### Stap 1: Install once (je werkstation)

- **lighthouse**: één programma. Het draagt de packs in zich (`pack/pack.go`, `go:embed`):
  skills, agents, hooks, rules, templates, workflows en hulpscripts, voor core en elke taal. Het
  vak toont dat de packs erin zitten. Link: de installatie op de site (`index.html#download`).
  Het overzicht noemt de curl-route; de installatiepagina noemt ook `go install` en een build uit
  een clone.
- **superpowers plugin**: extern, groen. Zit **niet** in lighthouse. Lighthouse installeert het
  niet; `init` controleert of het er is (`~/.claude/plugins/installed_plugins.json`,
  `internal/deps/deps.go`) en meldt anders `missing dependency: superpowers (needed by: …)`.
  De workflow skills geven eraan door.
- **You bring**: Claude Code, git, gh, jq, python3. Lighthouse installeert ze niet; `init`
  controleert ze (`pack/*/requires.toml`) en gaat bij een ontbrekende tool door met een melding.

Onder het overzicht staat een kort stuk "What you bring, and what lighthouse checks", in vier
groepen, afgeleid uit de kolom *Without it* van `docs/getting-started.md` (Requirements):

| Groep | Tools |
|---|---|
| Needed | Claude Code, git, python3 3.11+, jq, superpowers |
| For GitHub | gh |
| Per language | uv, go, gofmt, golangci-lint, node, npm, curl, … (per pack) |
| Optional | codex |

### Stap 2: `lighthouse init` (je repo)

De kop van de stap is het commando zelf, als terminalregel, zodat duidelijk is dat het een
commando is. Een vak per soort wat de repo krijgt:

| Vak | Waar | Link |
|---|---|---|
| Skills (workflow en standard) | `.claude/skills/`, `.agents/skills/` | referentiepagina |
| Agents | `.claude/agents/` | referentiepagina |
| Hooks | `.claude/hooks/lighthouse/`, geregistreerd in `.claude/settings.json` | referentiepagina |
| Rules | `AGENTS.md` (beheerd blok), `CLAUDE.md` | stuk eronder |
| Config | `lighthouse.toml`, `.lighthouse/` (manifest, headings, hulpcode) | stuk eronder |
| `.github/` | PR-template, workflows | referentiepagina |

`init` schrijft ook iets buiten de repo, op je werkstation: `~/.config/lighthouse/host.toml`
(alleen met Tailscale). Dat staat bij stap 2 als klein vak "on your workstation"
*(keuze van Claude: het komt uit `init`, niet uit de installatie)*.

Stap 2 is het korte overzicht. De uitgewerkte kaart van je repo na `init` (van wie elke plek is,
uit welk pack, wat `--upgrade` raakt) blijft #101 (Corneel). Als die kaart er is, wordt hij de
pagina achter stap 2; tot dan linken de vakken naar de referentiepagina en het stuk eronder. De
tegel van #101 in `index.html` blijft `planned`.

### Stap 3: Run the loop

*(Naam: keuze van Claude. Corneel noemde "feed / manage / trigger the loop"; "Run the loop" zegt
het kortst wat je doet.)*

| Vak | Wat | Link |
|---|---|---|
| The loop | van issue tot gesloten issue; een loop bestaat uit meerdere sessies | `chain.html` |
| Inside a session | één sessie in Claude Code: welke skill laadt en welke hook wanneer ingrijpt | `session.html` (uit PR #155) |
| Keep track on GitHub | issues, milestones, labels, PR's, CI | referentiepagina, deel GitHub |

De tests na elke commit draaien op je werkstation (`commit-tests`); CI draait op GitHub. De
pagina zegt niet dat CI alleen op GitHub kan.

## 4. De referentiepagina

Eén pagina, `reference.html`: het referentiedeel van K4 uit het kaartenontwerp (§4, na #100):
elk onderdeel los, per pack, zonder stroom. Een anker per soort (`#skills`, `#agents`, `#hooks`,
`#github`). Per soort: één zin wat het is, en per pack de namen uit `docs/site/data/packs.json`
(dat bestand bestaat al), elk met een link naar zijn plek op GitHub
(`https://github.com/CorneeldH/lighthouse/tree/main/pack/<pack>/<soort>/…`). Skills tonen hun
teken: workflow skill of standard skill. Het deel GitHub legt in een paar zinnen uit wat
lighthouse op GitHub gebruikt (issues, milestones, labels, PR's, CI) en linkt naar
`pack/core/templates` en `pack/core/workflows`.

Hier staan ook de onderdelen die op geen stroomkaart passen, zoals `instrument` en
`worktree-audit`. De dekkingstest in `tests/test_site_chain.py` noemt die twee nu bij naam als
uitzondering (`missing_parts`); met de referentiepagina vervalt die uitzondering: elk onderdeel
uit `pack/` staat op een stroomkaart of op de referentiepagina.

## 5. Tekens en kleuren

Het teken zegt wat iets is, op elke kaart hetzelfde. De kleur zegt van wie het komt. Het teken
toont niet meer wie iets start (jij, een skill, de beschrijving): Corneel verwacht dat "jij" en
"automatisch" in elkaar overgaan naarmate er meer automatisch gaat. De trigger blijft in de data
(`trigger` in `chain.json`) en in de lade.

| Soort | Teken | Kleur |
|---|---|---|
| lighthouse (het programma) | ⛯ | lighthouse-blauw |
| Workflow skill | ↪ | lighthouse-blauw of superpowers-groen |
| Standard skill | ✓ | lighthouse-blauw |
| Agent | ◆ | agent-paars |
| Hook | ⚡ | hook-oranje |
| Rules | § | config-geel (nieuw token) |
| Config | ⚙ | config-geel |
| GitHub | ⑂ | GitHub-grijs |
| The loop | ∞ | donker (badge-kleur) |
| A session | ❯ | donker |
| You bring it | ▣ | wit met rand |

- **Workflow skill**: een stap die je aanroept of die een andere skill aanroept (trigger `you`
  of `skill`), zoals start-issue, verify-pr, brainstorming. **Standard skill**: kennis die de
  agent laadt als de taak past (trigger `description`), zoals testing en go-writing. De afleiding
  uit `trigger` is een keuze van Claude, door Corneel bevestigd.
- Op de systeempagina is van de kleuren alleen superpowers-groen tegenover lighthouse-blauw van
  belang; superpowers draagt daar het teken van de workflow skill.
- **The loop** krijgt ∞, niet ⟳ uit de mockup: de ketenkaart gebruikt ↻ al voor een handoff, en
  ⟳ lijkt daar te veel op *(keuze van Claude)*.
- **Chips die geen skill zijn** volgen hun vorm (`form()` in `map.js`), niet hun trigger
  *(keuze van Claude, na de bouwerscheck)*:
  - een externe tool (`form: tool`, zoals `roborev`) krijgt ▣, net als wat je zelf meebrengt;
  - de dispatcher (`form: dispatcher`, `triage`) krijgt ⛯: het programma lighthouse doet het;
  - alles van GitHub (`kind: github`) en een GitHub Actions-workflow (`form: workflow`, zoals
    `unblock-dependents`) krijgt ⑂. Let op: `form: workflow` is een GitHub-workflow, geen
    workflow skill.
  - Alleen een chip met `form: skill` (of `kind: superpowers`) is een workflow skill of standard
    skill, naar zijn `trigger`.
- De tekens staan op één plek in `map.js`. De ketenkaart, de sessiekaart, de systeempagina en
  elke legenda lezen ze daar. De legenda "How it starts" op de ketenkaart vervalt; er komt een
  legenda per soort voor in de plaats.

## 6. The chain wordt The loop; de sessiekaart komt uit PR #155

- Titel, kop, ondertitel en "Wait, but why" van `chain.html`, de tegel in `index.html` en de
  linktekst in `docs/getting-started.md` heten voortaan "The loop".
- **De sessiekaart komt uit PR #155** (branch `fix-hookkaart`, Refs #100). Die hernoemt de
  vangrailkaart al: `guardrails.html` wordt `session.html`, `guardrails.json` wordt
  `session.json`, titel "Inside a session", met een kolom "A skill loads" voor de standard
  skills. #102 bouwt daarop en hernoemt hem niet opnieuw. Het vak in stap 3 heet "Inside a
  session", net als de pagina: één naam *(keuze van Claude; Corneel wilde "The session", #155
  koos al "Inside a session")*. De kaarten zijn twee niveaus: de loop volgt het werk van idee
  naar main over sessies heen, de sessiekaart zoomt in op één sessie (kaartenontwerp §4, K2).
- Op de sessiekaart vervalt de legenda "How it starts" ook; alle hooks dragen ⚡, het
  onderscheid tool-call/beurt-event staat in de kolommen van de kaart en in de lade.
- De bestandsnaam `chain.html` blijft: de site heeft geen redirects, en links van buiten (de
  getting-started op `main`, eerdere releases) zouden breken *(keuze van Claude)*.
- **Volgorde**: PR #155 merget eerst. Daarna rebaset deze branch op `main`, voordat het nieuwe
  plan wordt uitgevoerd.
- "Chain" als woord in de rest van lighthouse (`AGENTS.md`, "chain rules", de ondertitel van de
  site) blijft buiten deze wijziging.

## 7. Aanpak

Drie manieren overwogen:

1. **Data plus een kleine verhaalweergave** (gekozen). `system.json` beschrijft de stappen en
   per stap de vakken (soort, label, link). Een nieuwe `renderStory` in `map.js` tekent ze. Tekens
   en kleuren komen uit één plek in `map.js`. Plus: één woordenschat, de pagina volgt als een
   teken verandert. Min: iets meer code dan losse HTML.
2. **Handgeschreven HTML.** Minste code, maar de tekens staan dan twee keer (HTML en `map.js`), en
   een volgende wijziging mist er snel een.
3. **De plattegrond van PR #157 ombouwen.** De geneste schillen van `renderPlanMap` passen niet
   bij een verhaal van links naar rechts: verbouwen in plaats van bouwen. Afgevallen.

Corneel koos 1, omdat de vaste tekens dan ook technisch op één plek liggen.

## 8. De bouw

- **Data**: `docs/site/data/system.json` krijgt een nieuwe vorm: `title`, `subtitle`, `steps`
  (elk met `id`, `title`, eventueel `command`, en `items`) en `sections` (de stukken onder het
  overzicht). Een item heeft `form` (een soort uit §5), `kind` (van wie: `lighthouse`,
  `superpowers`, `hook`, `github` of `you`; bepaalt de kleur), `label`, optioneel `note`, en
  `href`. `kind` en `form` betekenen hetzelfde als in `chain.json` *(keuze van Claude, na de
  bouwerscheck)*. De
  oude sleutels `parts`, `links`, `milestones`, `issues` en `snapshot` vervallen. Geen sleutel
  `chips`, zodat `tests/test_site_chain.py` het bestand niet als stroomkaart oppakt.
- **Weergave**: `renderStory(root, data)` in `map.js`. Drie kolommen op een breed scherm, één
  kolom op een smal scherm. Een item is een link (`<a>`) met teken en label, in de kleur van zijn
  soort. Geen lade nodig: elk item linkt.
- **Wat uit PR #157 vervalt**: `renderPlanMap`, de *Over time*-weergave, de `.plan*`-stijlen die
  alleen daarvoor dienen, de schakelaar Now/Over time, `tests/fixtures/site/system.json` in de
  oude vorm en de oude tests in `tests/test_site_system.py`. De gedeelde lade-helper
  (commit `9710f6e`) blijft als de ketenkaart en de sessiekaart hem gebruiken.
- **Tekens**: `GLYPHS` per trigger wordt een tabel per soort. `glyphOf` leidt de soort af uit de
  chip (`form()` bestaat al; workflow of standard uit `trigger`). Hooks dragen allemaal ⚡,
  GitHub-chips ⑂.
- **Index**: de tegel "The system and the roadmap" wordt "The system", met een nieuwe
  ondertitel; de tegel "The chain" wordt "The loop".
- Geen `innerHTML` en verwante sinks (`test_component_never_writes_html_from_data`), en
  `<html lang="en">`.
- **AC van #102**: na goedkeuring van deze spec past Corneel (via `/lighthouse-write-issue`) de
  AC aan: weg met schillen, *Over time*, milestones, docs en site; erbij de drie stappen, de
  tekens, de rename en de referentiepagina.
- Het plan `docs/developer/planning/plans/2026-10-31-issue-102-systeemkaart.md` is achterhaald en
  wordt vervangen door een nieuw plan.

## 9. Tests

`tests/test_site_system.py` wordt herschreven, in het patroon van `tests/test_site_chain.py`.
Elke test moet falen als je de productie breekt.

1. **Drie stappen.** `system.json` heeft precies de stappen `install`, `init` en `loop`, in die
   volgorde.
2. **Bekende soorten.** Elk item heeft een `form` uit de tabel van §5 en een bekende `kind`; `map.js` heeft voor elke
   soort een teken. Een fixture met een onbekende soort wordt rood.
3. **Links bestaan.** Een relatieve `href` wijst naar een bestaand bestand in `docs/site/`, en
   een anker naar een bestaand `id` op die pagina. Een GitHub-URL onder `blob/main` of
   `tree/main` wijst naar een pad dat bestaat (zoals `source_problems`).
4. **Niets wat er niet voor gebruikers is.** Geen item noemt `serve`, `tick`, dashboard of
   controller.
5. **Eén woordenschat.** `chain.html` toont de legenda "How it starts" niet meer, en `glyphOf`
   geeft voor een standard skill ✓ en voor een workflow skill ↪ (test op de soortentabel in
   `map.js`).

6. **Referentie dekt de rest.** Elk onderdeel uit `pack/` (skill, agent, hook, workflow) staat
   op de referentiepagina; de uitzondering voor `instrument` en `worktree-audit` in
   `tests/test_site_chain.py` vervalt.

De linktest in `tests/test_site_pages.py` dekt de nieuwe referentiepagina vanzelf.

## 10. Later

- Een pagina met hoe lighthouse er straks uitziet: controller, omgevingen (werkstation of via
  SSH een andere machine), pijlen en meer onderdelen.
- Referentiepagina's per skill, agent en hook, en een uitgewerkte GitHub-pagina.
- Een willekeurige git-forge in plaats van alleen GitHub.
- Superpowers automatisch installeren, of `init` laten stoppen als het ontbreekt: een
  productwijziging, geen kaartwerk.

## 11. Voor de bouwer

Aan de bouwer: de precieze lay-out en afstanden binnen de stijl van `map.css`, de Engelse teksten
van de stukken onder het overzicht en van de referentiepagina, en het nieuwe kleurtoken voor
rules en config in licht en donker.
