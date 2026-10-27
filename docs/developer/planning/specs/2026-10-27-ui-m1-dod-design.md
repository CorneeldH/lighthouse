# Ontwerp: UI voor de DoD van M1

Issue: #69 · Milestone: M1 — Romp en zicht · Datum: 2026-10-27

## Aanleiding en doel

#69 sluit de DoD van M1: de UI toont voor CorneeldH/personal_ai_agent de open vragen, de
teruggegeven issues en het usage-venster van Claude en Codex. Het bewijs is een
Playwright-screenshot van de draaiende server.

De API staat al op `main`: `GET /api/questions` en `GET /api/returned` (#67) en `GET /api/usage`
(#65). Het SPA-skelet met de orval-client is #68, nog open. Dit ontwerp gaat uit van het plan van
#68 (`docs/developer/planning/plans/2026-10-26-issue-68-spa-skelet.md` op de branch
`issue-68-spa-skelet`): de map `web/` met Vite, React, TypeScript, Tailwind, shadcn/ui en vitest,
een orval-`fetch`-client in `web/src/api/` zonder query-laag, een gecommitte `web/openapi.json`,
de indeler met de domeinen `web`, `go` en `python`, en de CI-jobs `changes`, `web` en `binary`.
Wijkt #68 bij de merge af, dan past het plan van #69 zich aan; dit ontwerp niet.

Wie de pagina gebruikt: de eigenaar, met een tabblad dat open blijft naast de sessies. Hij ziet
in één blik welke issues op hem wachten en hoeveel venster er nog is.

## Scope

#69 levert:

1. één pagina met drie blokken: **Usage**, **Open questions**, **Returned**;
2. verversen zonder herladen, en een knop die alles nu ververst;
3. een naad in `lighthouse serve` voor de basis-URL van de forge;
4. een Playwright-test die de gebouwde binary start tegen nep-data, in CI;
5. een screenshot tegen CorneeldH/lighthouse, in de PR en in `README.md` (zie §9: niet tegen
   personal_ai_agent, want lighthouse is publiek).

**Buiten scope:**

- Lopende runs (M2).
- Schrijven naar de forge: antwoorden, labels, sluiten.
- Een cache in de server voor de forge-endpoints (zie Risico's).
- Routing of meer dan één pagina.
- Wijzigingen aan `internal/overview`, `internal/usage` of de schema's van de drie endpoints.

## Non-goals en anti-goals

- **Anti-goal: een venster dat niet meetbaar is en eruitziet als 0%.** De UI stuurt op `state`,
  nooit op `pct`. Bij `not_measurable` is `pct` null; de UI toont dan geen getal en geen balk.
- **Anti-goal: een fout of lege lijst als leeg vlak.** Elk blok toont altijd tekst: data, een
  lege-melding of een foutmelding.
- **Anti-goal: verversen dat oude data wist.** Een mislukte verversing laat de laatste goede data
  staan en zet de fout erbij.
- Geen testmodus in de productiebinary. De naad is een gewone instelling die ook buiten tests
  zin heeft (GitHub Enterprise).

## Besluiten

### 1. Taal van de UI

Alle zichtbare tekst is Engels (besluit gebruiker). De Nederlandse teksten in de AC ("geen open
vragen", "forge niet bereikbaar") worden:

| Toestand | Tekst |
|---|---|
| lege lijst vragen | `No open questions` |
| lege lijst teruggegeven | `No returned issues` |
| 503 van een forge-endpoint | `Forge not configured — set GH_TOKEN and --repo` |
| 502 of een andere fout | `Forge unreachable` met de `detail` uit het antwoord |

### 2. 503 en 502 apart

Besluit gebruiker. De API maakt het onderscheid al (`internal/server/forge.go:48-54`):

- **503** `forge not configured`: geen repo of geen token. De gebruiker moet iets instellen.
- **502**: GitHub gaf een fout of was niet bereikbaar.

De orval-client geeft `{data, status}` terug en gooit niet bij een niet-200. De UI vertakt op
`status`. Een netwerkfout naar `lighthouse serve` zelf (de server is weg) toont
`Server unreachable`.

### 3. Verversen

De pagina ververst zichzelf; daarnaast staat een knop **Refresh**. De twee bronnen kosten
verschillend, dus ze krijgen een eigen interval:

| Blok | Interval | Waarom |
|---|---|---|
| Usage | 30 s | goedkoop: een bestand op schijf, en de server hergebruikt een Codex-meting 300 s (`internal/usage/report.go:13`) |
| Open questions, Returned | 5 min | duur: elke aanroep leest alle open issues en hun comments van GitHub, zonder cache |

**Refresh** ververst alle drie direct.

*Mijn keuze:* elk blok toont zijn eigen tijd van de laatste geslaagde verversing (`updated 3 min
ago`), niet één tijd in de kop. Eén tijd voor de hele pagina schuift elke 30 s op met usage, ook
als de forge al een uur faalt; de lijsten lijken dan vers terwijl ze oud zijn.

*Mijn keuze:* TanStack Query (v5) als query-laag. `refetchInterval` geeft het interval, en bij een
fout houdt een query zijn laatste data en zet hij `error` erbij; dat dekt de anti-goal
"verversen wist geen oude data". De orval-generator blijft zoals #68 hem instelt (`client:
'fetch'`). De `queryFn` roept de gegenereerde functie aan en gooit bij een niet-200 een fout die
de status bewaart, zodat besluit 2 werkt. Op een tabblad op de achtergrond pauzeert TanStack de
intervallen standaard; dat is goed.

### 4. De pagina

Eén kolom, van boven naar onder:

1. **Kop**: `lighthouse <version>` (uit `/api/health`, zoals #68) en de knop **Refresh**.
2. **Usage**: vier vensters in twee rijen, Claude en Codex, elk met `session` en `week`.
3. **Open questions**: per issue `#<nummer>`, de titel als link naar het issue, de vraagtekst, en
   een link `Answer` naar `answer_url` (de vraagcomment).
4. **Returned**: per issue `#<nummer>`, de titel als link naar het issue, en de reden.

Lijsten in de volgorde van de API (oudste comment eerst). De vraagtekst toont de UI als platte
tekst met behouden regeleinden; geen Markdown-rendering.

*Mijn keuze:* usage bovenaan. Het venster bepaalt of de eigenaar nu een review kan starten; de
lijsten veranderen trager.

### 5. Een venster weergeven

Per venster, naar `state`:

| `state` | Weergave |
|---|---|
| `ok` | het percentage met een balk, `resets <tijd>`, `measured <leeftijd> ago` |
| `stale` | als `ok`, maar gedimd, met een badge `stale` en de leeftijd in de badge |
| `not_measurable` | geen getal en geen balk; een badge `not measurable` en de `reason` |

- `lower_bound: true` toont het percentage als `≥ 42%`, met de tooltip `lower bound`.
- `reset_at` leeg: geen reset-regel.
- `reset_at` in de lokale tijd van de browser: `14:30` als het vandaag is, anders `Thu 16:59`.
- De leeftijd komt uit `age_s` bij de laatste verversing, in minuten (`< 1 min`, `12 min`) of
  uren. Om de 30 s komt er een nieuwe.

De drempels voor `stale` en `not_measurable` liggen in de server (`StaleAfter` = 900 s). De UI
rekent ze niet na.

### 6. De naad: basis-URL van de forge

`lighthouse serve` krijgt `--forge-url`, met als standaard `$LIGHTHOUSE_FORGE_URL`, anders
`https://api.github.com`. Dat volgt het patroon van `--repo` en `$LIGHTHOUSE_FORGE_REPO`. De
waarde gaat naar `forge.Client.BaseURL`; dat veld bestaat al (`internal/forge/github.go:30-40`).

Het token gaat mee naar die URL. Dat is bedoeld: wie de URL zet, zet ook het token. De help-tekst
noemt dat.

Geen naad voor usage: `XDG_CACHE_HOME` stuurt de Claude-cache al
(`internal/usage/state.go:17-26`). Codex is `not_measurable` met een lege tijdelijke `HOME` en
een `PATH` zonder `codex`: `CodexReader.Read` valt na de app-server terug op de rollout-logs in
`$HOME/.codex` en `$HOME/.codex-specreview` (`internal/usage/codex.go:80-98`). Alleen `PATH`
leeghalen is dus niet genoeg; op een machine met echte Codex-logs zou Codex dan `ok` zijn.

### 7. De Playwright-test

De test draait tegen de **gebouwde binary met de ingebedde SPA**, niet tegen de Vite-devserver:
de AC zegt `lighthouse serve`, en alleen zo bewijst de test dat de inbedding de pagina levert.

Per scenario start de test:

- een **nep-GitHub**: een kleine Node-HTTP-server in de Playwright-fixture die
  `GET /repos/<repo>/issues` en `GET /repos/<repo>/issues/<n>/comments` beantwoordt;
- **`lighthouse serve`** met `--addr 127.0.0.1:0`, een tijdelijke `--db`,
  `--forge-url <nep-GitHub>`, `--repo test/repo`, `GH_TOKEN=test`, een tijdelijke
  `XDG_CACHE_HOME`, een lege tijdelijke `HOME` en een `PATH` zonder `codex` (zie besluit 6).

De scenario's:

| Scenario | Nep-GitHub | Claude-cache | Wat de test ziet |
|---|---|---|---|
| vol | twee issues met een vraagcomment, één met een teruggegeven-comment | vers | beide lijsten met nummer, titel, tekst en links; Claude `ok` met pct en reset; Codex `not measurable` met de reden, zonder `%` |
| leeg | geen issues | oud (> 900 s) | `No open questions`, `No returned issues`; Claude `stale` |
| 502 | 500 op `/issues` | geen | `Forge unreachable` in beide blokken; Claude `not measurable` |
| 503 | — (serve zonder `GH_TOKEN` en `GITHUB_TOKEN`; CI zet die laatste wel) | vers | `Forge not configured — set GH_TOKEN and --repo` in beide blokken |
| verversen | eerst leeg, dan een vraag erbij | vers | na een klik op **Refresh** staat de vraag er, zonder herladen |
| oude data blijft | eerst vol, dan 500 | vers | na **Refresh**: de oude vraag staat er nog, met `Forge unreachable` erbij |

De comments in de nep-GitHub zijn echte protocol-headers (`🤖 Vraag`, `🤖 Teruggegeven` met
`reden:`), zodat de test ook de classificatie in de server raakt. Ze komen uit of volgen
`tests/pack/fixtures/protocol/headers.json`.

Een Playwright-test is per definitie traag; hij controleert het gedrag op het scherm. De
weergaveregels van besluit 5 (`≥`, tijdnotatie, badges) krijgen daarnaast vitest-componenttests
met vaste antwoorden. Die zijn snel en dekken de randgevallen die Playwright niet hoeft te
herhalen.

### 8. CI en de lokale gate

- Playwright draait in CI op de binary die de job `binary` van #68 bouwt: dezelfde job, of een
  job daarna met de binary als artefact. Hij draait als `web` of `go` geraakt is, net als
  `binary`. Alleen Chromium, geïnstalleerd met `npx playwright install --with-deps chromium`.
- De lokale test-gate draait Playwright **niet**: browserinstallatie en een `go build` per commit
  kosten te veel van de `deadline_seconds`. De vitest-tests draaien wel lokaal, via het
  `web`-domein van #68.
- Een Go-test voor de naad: `serve` met `--forge-url` naar een `httptest`-server levert een vraag
  op `/api/questions`. Hij valt onder de bestaande `go test`-stap; er komt geen derde
  `go test`-stap in `ci.yml` (de pin in `tests/test_ci_go_test_pin.py`).

### 9. Het bewijs voor de DoD

Een script in `web/` (`npm run screenshot`) maakt met Playwright een screenshot van een
draaiende `lighthouse serve`. De agent mag serve starten met het token van de eigenaar
(besluit gebruiker):

```bash
GH_TOKEN=$(gh auth token) lighthouse serve --repo CorneeldH/lighthouse
```

Het script draait niet in CI en heeft zelf geen token nodig; het opent alleen de pagina.

Het screenshot blijft in de repo (besluit gebruiker):

- `docs/images/ui-m1.png`, gecommit op de branch, en via de squash op `main`;
- `README.md` toont het onder `## Running the server`, met een relatieve link;
- de PR-body toont het bij de rij voor dat AC in de Evidence-tabel, met een absolute link naar
  het bestand op de commit-SHA (`https://github.com/CorneeldH/lighthouse/blob/<sha>/docs/images/ui-m1.png?raw=true`).
  `gh` kan geen afbeelding uploaden, en een relatieve link werkt niet in een PR-body.

Het screenshot toont titels en vragen van issues uit CorneeldH/lighthouse zelf (besluit
gebruiker, 2026-10-27). lighthouse is publiek en personal_ai_agent privé; een screenshot tegen
personal_ai_agent in de README en de PR zou titels en vragen uit een privé-repo publiceren. De
UI werkt per `--repo` gelijk, dus het bewijs tegen lighthouse dekt dezelfde code.

## Teststrategie

| AC | Test | Mutatie die hem rood maakt |
|---|---|---|
| Open questions met nummer, titel, vraag, link | Playwright "vol"; vitest op de lijstcomponent | de link naar `url` vervangen door `answer_url`; de vraagtekst weglaten |
| Returned met nummer, titel, reden, link | Playwright "vol" | `reason` niet renderen |
| Usage: vensters, pct, reset, leeftijd | Playwright "vol"; vitest per `state` | `session` en `week` verwisselen; de leeftijd weglaten |
| Niet-meetbaar lijkt niet op 0% | vitest: `not_measurable` met `pct: null` rendert geen `%`; Playwright "vol" (Codex) | `pct ?? 0` renderen |
| Verouderd valt op | vitest: `stale` heeft de badge; Playwright "leeg" | de badge alleen bij `not_measurable` tonen |
| Data via de orval-client | de typecheck: de componenten importeren alleen uit `web/src/api/` voor `/api/*`; ESLint verbiedt `fetch('/api` buiten `src/api/` | een blok met een eigen `fetch` |
| Leeg en fout als tekst | Playwright "leeg", "502", "503" | de lege-melding weghalen; 503 en 502 op één tekst zetten |
| Playwright in CI | de job in `ci.yml` en een pytest-pin naast die van #68 | de Playwright-stap weghalen |
| Verversen | Playwright "verversen" en "oude data blijft" | `refetchInterval` en de knop weghalen; de data wissen bij een fout |
| Naad forge-URL | Go-test in `internal/cli` met `httptest` | `--forge-url` niet doorgeven aan `forge.Client` |

## Risico's

- **#68 is nog open.** *Mijn keuze:* de uitvoering van #69 wacht tot #68 op `main` staat, en
  begint dan met een rebase van deze branch op `main`. Niet stapelen op `issue-68-spa-skelet`:
  daar ontbreken nog de indeler, de gate en de CI-jobs `changes`, `web` en `binary`, en precies
  daarop bouwen de Playwright-job en zijn pin. Het plan van #69 noemt per taak welke aanname uit
  #68 hij gebruikt; de eerste taak controleert die aannames tegen `main`.
- **Twee forge-rondes per verversing.** `/api/questions` en `/api/returned` bouwen elk het hele
  overzicht (`overview.Build`). Om de 5 minuten is dat twee keer alle open issues met hun
  comments. Bij ~50 issues blijft dat ver onder de GitHub-limiet van 5000 per uur. Een cache of
  een gecombineerd endpoint is een later issue, als de meting dat vraagt.
- **Protocolformaat in de nep-GitHub.** Verandert het header-formaat, dan moet de fixture mee.
  Dat is gewenst: de test hoort dan rood te worden.

## Aan de bouwer

- De exacte versies van TanStack Query en Playwright.
- Hoe de Playwright-fixture de nep-GitHub en serve per scenario opzet (per test of per
  `describe`), en hoe hij het adres van serve uit de startregel leest.
- Of Playwright in de job `binary` draait of in een eigen job na `binary`.
- De opmaak binnen besluit 4 en 5: kleuren, shadcn-componenten, de vorm van de balk.
- De naam van de ESLint-regel of het patroon dat `fetch('/api` buiten `src/api/` verbiedt.
