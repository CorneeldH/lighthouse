# Ontwerp: M3 — Tweede omgeving

Milestone: [M3 — Tweede omgeving](https://github.com/CorneeldH/lighthouse/milestone/4) · Datum:
2026-11-07 · Bouwt op M0 (`lighthouse init`, de pakketten, `ci/literal-grep.sh`) en op
"Product: eerste versie voor anderen" (releasebinaries, startgids, agentgids).

## Aanleiding en doel

Lighthouse is gebouwd voor eigen gereedschap: de Beelink, personal_ai_agent en GitHub. Het tweede
doel uit de scope-spec is een tweede gebruiker: een bevriende developer met een eigen opzet. M3
maakt dat waar. De DoD blijft die van de pitch: **de tweede gebruiker brengt één echt issue tot
een PR op de eigen opzet.**

Deze spec beantwoordt eerst vier vragen. Daarna volgen de scope, de onderdelen en de nieuwe
milestones die uit de antwoorden volgen. De pitch van M3 noemt GitLab en een werkplekprofiel;
waar deze spec afwijkt, geldt de spec, en de pitch wordt erop bijgewerkt.

## De vier vragen

### 1. Welke opzet heeft de tweede gebruiker?

| Onderdeel | Antwoord | Bron |
|---|---|---|
| Forge | **GitHub** | het gesprek |
| OS | **Windows** op de laptop, **Linux** op de lokale server | het gesprek |
| Harness | Claude Code, te bevestigen met de sessies van de gebruiker | het gesprek: sessies komen 2026-11-07 of 2026-11-08 |
| kenn-forge | **Nee**, nog niet nodig | het gesprek |

**Nog open: waar draait Claude Code?** Native op Windows, in WSL op de laptop, of op de
Linux-server (via SSH of VS Code Remote). De sessies van de gebruiker beantwoorden dat. De spec
wacht er niet op; de regel staat bij *Onderdelen*, §7 (#90).

*Mijn keuze:* M3 gaat uit van Claude Code. Alleen daar draaien de hooks (`AGENTS.md`, *Outside
Claude Code*), en de DoD vraagt "met de hooks actief". Blijkt uit de sessies een andere harness,
dan gaat deze spec terug naar het gesprek vóór er een plan komt.

### 2. Wat is het minimum voor de DoD?

**De handmatige keten, zonder dispatcher.** `lighthouse init`, de hooks en de skills op de opzet
van de tweede gebruiker. De gebruiker brengt één issue tot een PR met `/lighthouse-start-issue`
tot `/lighthouse-write-pr`, met de hooks actief.

De dispatcher zonder kenn-forge krijgt een eigen milestone na M3 (*Nieuwe milestones*). Zie
*Aanpakken*, keuze 1.

### 3. Forge achter een abstractie, of een tweede pad naast `gh`?

**Geen van beide in M3.** De tweede gebruiker zit op GitHub, dus de DoD van M3 toetst geen tweede
forge. GitLab krijgt een eigen milestone (*Nieuwe milestones*). Zie *Aanpakken*, keuze 2.

Wel legt deze spec de richting vast, zodat M3 niets bouwt dat GitLab later blokkeert:

- **Go.** Een interface aan de kant van de afnemer (`internal/dispatch`, `internal/overview`,
  `internal/triage`), los van `forge.Client`. Nu is `internal/forge` één concrete GitHub-client
  over REST en GraphQL (`internal/forge/github.go:1`, `:38`), en ook de smalle interfaces
  (`dispatch.Poster`, `overview.Source`) dragen GitHub-begrippen: Projects v2, raket-reacties,
  issue-dependencies, collaborator-rollen.
- **Skills en scripts.** Eén wrapper in lighthouse (bijvoorbeeld `lighthouse forge issue view`)
  in plaats van `gh` en `glab` naast elkaar in elke skill. Nu vragen 15 units in
  `pack/core/requires.toml` om `gh`, en `pr-create-gate.sh` en `pr_body_gate.py` reageren op
  `gh pr create` met `Closes #N`.
- **Regel voor M3:** geen nieuwe GitHub-aanname buiten de plekken die er al een hebben
  (`internal/forge`, de `gh`-aanroepen in `pack/`). Een nieuw onderdeel in M3 dat de forge nodig
  heeft, gaat via die plekken. Voor #95 betekent dat: labels aanmaken gebeurt in een script in
  `pack/core/scripts/` met `gh`, dat `init` aanroept, niet met `gh` vanuit `internal/initcmd`
  (*mijn keuze*).

### 4. Welke host-aannames moeten uit `pack/core`?

De hooks zijn al bewust draagbaar: bash 3.2, Python-fallbacks voor `realpath -m`, `date -j` naast
`date -d`. Wat overblijft:

| Aanname | Plek | In M3 |
|---|---|---|
| `host.tailscale_ip` verplicht, IPv4 | `pack/core/lib/config.py:482-494` | Optioneel maken; zonder de sleutel is `host-check` "andere host" (§2) |
| `init` stelt `host.toml` alleen voor met `tailscale ip -4` | `internal/initcmd/host.go:30-37` | Voorstel ook zonder Tailscale (§2) |
| Een pid leeft als `/proc/<pid>` bestaat | `pack/core/scripts/worktree_audit.py:164`, `skills/audit-worktrees/SKILL.md:74` | Draagbaar maken (`os.kill(pid, 0)`) (§1) |
| `journalctl -k` voor een OOM-kill | `pack/core/skills/fix-pr/{claude,agents}/SKILL.md:162` | Als Linux-only markeren, met wat te doen elders (§1) |
| `systemd-run --user --scope … choom` in de test-gate | `pack/core/hooks/commit-test-gate.sh:518-546` | Blijft; staat al achter `host.gate_slice`. Expliciet maken in de docs (§1) |
| Python ≥ 3.11 (`tomllib`) | `pack/core/lib/config.py:27` | Blijft; de installatiegids zegt het per OS (§4) |
| `sha256sum` zonder `shasum`-fallback, geen Intel-Mac- of Windows-shellcheck | `pack/shell/scripts/lib/shellcheck-bin.sh:22-25`, `:67` | Alleen `pack/shell`; een regel in de audit. Een fix-issue alleen als de DoD-run (§6) `pack/shell` gebruikt (*mijn keuze*) |

Er staat geen Beelink-literal in `pack/` (`ci/literal-grep.sh`). Buiten `pack/core` nemen de
dispatcher en de werkplek Linux aan: `/proc/meminfo` in de rem (`internal/dispatch/brake.go:82`),
`systemctl --user` en `podman` in de kenn-forge-adapter (`internal/workplace/kennforge/stop.go`),
een systemd-unit voor `serve` (`deploy/systemd/lighthouse.service`). Die horen bij de dispatcher
en dus niet bij M3; de audit (§1) zet ze wel op de lijst.

## Aanpakken

### Keuze 1: wat de DoD van M3 vraagt

| | Aanpak | Gevolg |
|---|---|---|
| A | Handmatige keten | Medium. Overlapt met de DoD van "Product: eerste versie voor anderen" |
| B | Ook de dispatcher op de Linux-server | Large: een werkplek-adapter zonder kenn-forge, een Projects v2-board bij de gebruiker, `serve` op de server |
| **C** | **A in M3, B als eigen milestone daarna** | **Gekozen.** M3 levert snel iets op; de werkplek zonder kenn-forge verdwijnt niet uit beeld, en M4 (SRC) kan erop bouwen |

### Keuze 2: wat er met GitLab gebeurt

| | Aanpak | Gevolg |
|---|---|---|
| **A** | **GitLab uit M3, naar een eigen milestone** | **Gekozen.** M3 blijft Medium; geen abstractie zonder tweede afnemer |
| B | GitLab in M3, als voorbereiding op M4 | Large, en de DoD toetst GitLab niet |
| C | Alleen de abstractie in M3 | Een refactor zonder tweede implementatie; de interface valt dan vaak verkeerd uit |

Waarom A: de echte vorm van de abstractie blijkt pas naast de GitLab-implementatie (scoped labels
`status::…` in plaats van Projects v2, MR's, geen raket-reactie), en of M4 GitLab vraagt, staat
nog open in de pitch van M4.

## Scope

### In M3

1. De Beelink-audit: scheiden wat overal geldt van wat de Beelink is, en dat expliciet maken.
2. Het hostprofiel: `host.toml` voor een host zonder Tailscale en zonder systemd-slice.
3. Instapniveaus, alleen de helft guards en checks (#19, bijgesteld).
4. De labeltaxonomie als optie van `init` (#95, bijgesteld).
5. De installatiegids in het Engels, voor de opzet van de tweede gebruiker.
6. De DoD-run: de tweede gebruiker brengt één echt issue tot een PR.
7. Windows (#90), alleen als de sessies om native Windows vragen.

### Niet in M3

| Onderdeel | Waarom eruit | Waar het heen gaat |
|---|---|---|
| GitLab-forge | De tweede gebruiker zit op GitHub | Milestone "GitLab-forge" |
| Dispatcher, `tick`, `serve` bij de tweede gebruiker | Keuze 1, C | Milestone "Werkplek zonder kenn-forge" |
| Controller-knoppen van de instapniveaus (#19, eerste helft) | Leest `[controller]`, dus de dispatcher | Milestone "Werkplek zonder kenn-forge" |
| macOS-deel van #90 | De tweede gebruiker heeft geen Mac | Blijft in "Product: eerste versie voor anderen" |

## Onderdelen

Elk onderdeel wordt een issue, behalve de DoD-run (§6), die de milestone afsluit.

### §1 Beelink-audit

**Doel:** elke aanname over de host is expliciet en staat in één van vier klassen. Geen aanname
zit stil in code die voor iedereen bedoeld is.

| Klasse | Betekenis | Waar het mag staan |
|---|---|---|
| Overal | Werkt op Linux, macOS en Windows (WSL) | Overal |
| Hostkenmerk | Optioneel, achter een sleutel in `host.toml` (`gate_slice`, `tailscale_ip`, `gate_min_*`) | Code, met een pad zonder die sleutel |
| Linux-only | Werkt alleen op Linux (`/proc`, systemd, `journalctl`) | Code, met een melding of een fallback elders, en in de vereisten van de docs |
| Beelink | Alleen Corneels opzet (IP's, paden, unitnamen, paia-targets) | Alleen `host.toml`, `deploy/` als voorbeeld, of personal_ai_agent |

**Werk:**

- Een inventaris van `pack/`, `internal/`, `cmd/`, `deploy/` en `docs/`. Startpunt: de tabel bij
  vraag 4, plus de Go-kant (`internal/dispatch/brake.go`, `internal/workplace/kennforge`,
  `internal/hostconfig` met de defaults `http://127.0.0.1:8091` en `~/.kenn/forge/auth_token`,
  `internal/usage/meter.go` met `pgrep -x claude`).
- De inventaris komt als Engelse pagina in `docs/` (*mijn keuze*: `docs/host-assumptions.md`),
  gelinkt vanuit de installatiegids. Per regel: plek, klasse, en wat een andere host doet.
- Kleine fixes in hetzelfde issue: `pid_alive` in `worktree_audit.py`, de `journalctl`-regel in
  `fix-pr`, de verouderde zin over `date -d` in `docs/install.md:8-10` (er is een BSD-fallback in
  `usage-gate.sh:76`). Een groot geval wordt een eigen issue.
- `ci/literal-grep.sh` draait ook op `internal/` en `cmd/`, met een eigen denylist voor Go
  (*mijn keuze*). De denylist van `pack/` past daar niet: een proefscan gaf 938 treffers, waarvan
  280 het modulepad `github.com/CorneeldH/lighthouse` in imports. De Go-denylist bevat alleen
  Beelink-literals: hostnamen, Tailscale-IP's, `/home/<gebruiker>`, paia-unitnamen. `_test.go` en
  `testdata/` vallen buiten de scan.
- De `paia`-markers voor oude statuscommentaren (`internal/protocol/status.go:39`,
  `internal/protocol/triage.go:18,42`, `internal/dispatch/judge.go:71`) zijn klasse Overal: ze
  lezen commentaren die op issues staan, ongeacht de host. Ze komen op de allowlist, met een
  verwijzing naar de audit. `paia-forge-` in `internal/workplace/kennforge/stop.go:34` is klasse
  Beelink: de audit zet het op de lijst, en de milestone "Werkplek zonder kenn-forge" haalt het
  naar de configuratie. Tot dan staat het op de allowlist.

**Klaar als:** elke regel in de klasse Beelink staat op een plek die ervoor bedoeld is, of op de
allowlist met een issue erbij. De CI faalt als er een nieuwe Beelink-literal in `internal/` of
`cmd/` komt.

### §2 Hostprofiel

De pitch noemt dit "werkplekprofiel". Zonder dispatcher in M3 is er geen werkplek; wat de tweede
gebruiker nodig heeft, is een `host.toml` die op de eigen host past. *Mijn keuze:* de naam
"hostprofiel" in M3; het werkplekprofiel verhuist met de dispatcher.

- `host.tailscale_ip` wordt optioneel in `config.py`. `internal/hostconfig` leest de sleutel
  niet; `docs/test-gate.md:55` past mee.
- `host-check` (`config.py:604`) blijft betekenen: "dit is de host waar de controller altijd
  aan staat". `/lighthouse-write-handoff` leest exit 0 zo
  (`pack/core/skills/write-handoff/SKILL.md:168-170`): het post dan `next:` en zet de status op
  `Controller`. Zonder `tailscale_ip` geeft `host-check` exit 1 ("andere host"). Een host zonder
  dispatcher krijgt zo nooit een handoff naar een controller die er niet is (*mijn keuze*). De
  milestone "Werkplek zonder kenn-forge" vervangt deze toets door een expliciete sleutel.
- `lighthouse init` stelt `host.toml` ook voor zonder Tailscale, met alleen `name`. Nu waarschuwt
  het "host.toml not proposed".
- Een voorbeeld-`host.toml` voor een gewone Linux-server en voor een laptop (WSL), in de
  installatiegids. Zonder `gate_slice` en `gate_min_*`: de test-gate draait dan zonder rem, zoals
  nu op een Mac.

### §3 Instapniveaus (#19, bijgesteld)

#19 heeft twee helften. M3 houdt de tweede: welke guards en checks `init` installeert. De eerste,
controller-knoppen uit `[controller]`, gaat naar de milestone "Werkplek zonder kenn-forge", want
alleen de dispatcher leest die. De afhankelijkheid van M2 vervalt voor de helft in M3.

Bijstellingen in #19:

- Acceptatiecriteria over controller-knoppen eruit, naar een nieuw issue in "Werkplek zonder
  kenn-forge".
- De beschrijving noemt de tweede gebruiker "veel minder technisch"; de scope-spec zegt "een
  bevriende developer". *Mijn keuze:* de beschrijving volgt de scope-spec, met de minder
  technische gebruiker als reden voor het lichtste niveau.
- "Eerste stap: zodra M2 staat" wordt "eerste stap: een spec, met de inventaris van §1 als
  bron voor wat elk niveau installeert".

M3 bouwt #19, niet alleen de bijstelling. Het eigen ontwerp is een spec binnen het issue, niet een
aparte ronde voor de milestone. #19 hoort niet bij de DoD: de tweede gebruiker kan met het
volledige pakket beginnen. Haalt #19 M3 niet, dan schuift het door zonder dat M3 openblijft
(*mijn keuze*).

### §4 Labeltaxonomie (#95, bijgesteld)

Blijft in M3: `/lighthouse-write-issue` in de repo van de tweede gebruiker heeft labels nodig.
Bijstellingen:

- #95 komt los van #19: `init` stelt een ja/nee-vraag over de taxonomie. Zodra #19 er is, neemt
  het niveau die vraag over (*mijn keuze*). #95 wacht dus niet op #19.
- De labels worden aangemaakt met `gh label create`, in een script in `pack/core/scripts/` dat
  `init` aanroept (de regel bij vraag 3). Dat is GitHub, en dat past bij keuze 2. De
  GitLab-milestone neemt het aanmaken van labels voor GitLab over.

### §5 Installatiegids (Engels)

*Mijn keuze:* geen derde gids. M3 breidt de twee bestaande uit: `docs/getting-started.md` (voor
mensen) en `docs/site/agents/install.md` (voor agents), met een sectie per opzet:

- Linux-server, met Claude Code op de server;
- Windows-laptop, met de route die §7 kiest (WSL, of naar de server via SSH of VS Code Remote);
- zonder Tailscale, zonder kenn-forge, zonder dispatcher.

Daarbij:

- Per OS hoe je aan Python ≥ 3.11, `gh`, `jq` en `uv` komt.
- Het hostprofiel uit §2 met voorbeelden.
- Een link naar `docs/host-assumptions.md` (§1).
- Eén map voor de binary in alle docs. Nu zegt `docs/install.md` `~/go/bin` en de agentgids
  `~/.local/bin`.

**Klaar als:** de tweede gebruiker installeert met de gids, zonder hulp van Corneel buiten wat
als issue terugkomt.

### §6 DoD-run

De tweede gebruiker installeert lighthouse op de eigen opzet, met `lighthouse init` in een eigen
GitHub-repo, en brengt één echt issue tot een PR via de handmatige keten, met de hooks actief.

**Bewijs:** de link naar die PR, en wat de gebruiker onderweg terugmeldde, als issues in deze
repo.

*Mijn keuze:* lukt dat met een releasebinary en de agentgids, dan telt dezelfde run ook als bewijs
voor de DoD van "Product: eerste versie voor anderen". Die DoD noemt Linux of macOS; de Linux-server
van de tweede gebruiker voldoet.

### §7 Windows (#90)

#90 staat in "Product" en dekt macOS en Windows. De regel, zodra de sessies van de gebruiker er
zijn:

| Waar Claude Code draait | Wat er met #90 gebeurt |
|---|---|
| Native op Windows (Git Bash) | Het Windows-deel van #90 wordt een eigen issue in M3: de hooks onder Git Bash, Python op Windows. Kan Large worden; dan meten, en wat niet past, krijgt een eigen issue |
| WSL op de laptop | Geen Windows-issue in M3. De gids (§5) zegt "WSL vereist" |
| Op de Linux-server | Geen Windows-issue in M3. De gids (§5) beschrijft SSH of VS Code Remote |

Het macOS-deel van #90 blijft in "Product".

## Nieuwe milestones

Allebei zonder nummer (*mijn keuze*): de volgorde ten opzichte van M4 staat nog open.

### Werkplek zonder kenn-forge

De dispatcher op de opzet van de tweede gebruiker: een werkplek-adapter naast `kennforge.Adapter`
(bijvoorbeeld een lokale worktree met headless `claude`), `serve` en `tick` op een Linux-server
zonder kenn-forge, en de controller-knoppen van #19. Nu bouwt `cli/tick.go:91-107` altijd een
`kennforge.Adapter`. De eerste stap is een spec. Deze milestone is ook voorwerk voor M4: SRC is
ook een werkplek zonder kenn-forge.

### GitLab-forge

GitLab naast GitHub, in delen:

1. Config en `init`: `[forge] kind = "gitlab"`, repo's met subgroepen, remote herkennen. Nu weigert
   `config.py:395` alles behalve `github`, eist `config.py:58` precies één `/`, en herkent
   `projectconfig.go:135-152` alleen `github.com`.
2. De forge-interface in Go, met de GitHub-client als eerste implementatie (richting bij vraag 3).
3. De GitLab-client: issues, MR's, status als scoped label `status::…`.
4. De wrapper voor skills en hooks, met `pr-create-gate` en `pr_body_gate` voor MR's.

De eerste stap is een spec. Of M4 deze milestone nodig heeft, beslist M4.

## Risico's

- **Overlap met "Product".** Twee milestones met bijna dezelfde DoD. Tegenmaatregel: §6 laat één
  run voor beide tellen, en de scheiding staat in de pitches: Product is de download en de
  website, M3 de echte opzet van een tweede gebruiker.
- **Windows wordt groot.** Als de sessies om native Windows vragen, kan het Windows-deel van #90
  M3 naar Large duwen. Tegenmaatregel: eerst meten, en "WSL vereist" blijft een geldige uitkomst
  (zoals #90 al zegt).
- **De audit wordt eindeloos.** Tegenmaatregel: de audit fixt alleen wat klein is; de rest wordt
  een issue met een klasse, en "expliciet gemaakt" is genoeg om §1 af te sluiten.
- **Nieuwe GitHub-aannames in M3.** Tegenmaatregel: de regel bij vraag 3.

Het risico uit de pitch over leesrechten is weg: de repo is public.

## Wat er met de pitch van M3 gebeurt

De pitch wordt bijgewerkt (via `/lighthouse-write-issue`):

- **Solution:** de onderdelen §1 tot §7, zonder GitLab, met een link naar deze spec.
- **Appetite:** blijft Medium.
- **Risks:** zoals hierboven.
- **DoD:** de tweede gebruiker brengt één echt issue tot een PR op de eigen opzet, via de
  handmatige keten, met de hooks actief. Bewijs: de PR-link, en de terugmeldingen als issues.
- **No-Gos:** erbij komen geen GitLab en geen dispatcher bij de tweede gebruiker.

## Aan de builder

- De vorm van `docs/host-assumptions.md` en hoe de inventaris gemaakt wordt.
- De toets van `host-check` zonder `tailscale_ip`, binnen de grens in §2.
- Hoe `ci/literal-grep.sh` testbestanden in `internal/` overslaat.
- De volgorde van §1 tot §5. §1 eerst ligt voor de hand: de inventaris voedt §2, §3 en §5.
