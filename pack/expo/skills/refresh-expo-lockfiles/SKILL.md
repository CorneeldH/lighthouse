---
name: paia-refresh-expo-lockfiles
description: Refresh the Expo app dependencies so the `Expo apps` CI lane goes green again. Use when apps.yml fails on `npx expo-doctor` with "Check that packages match versions required by installed Expo SDK", or when patch-level Expo drift blocks an unrelated PR. Stays on the pinned SDK; never upgrades it.
metadata:
  domain: applicatie
---

# Refresh Expo lockfiles

Brengt `apps/*` weer in lijn met de versietabel van de **al gepinde** Expo SDK. Patchcijfers bewegen,
het SDK-major niet.

## When to Use

- `apps.yml` (`Expo apps`) valt om op `npx expo-doctor` → *Check that packages match versions
  required by installed Expo SDK*
- Een ongerelateerde PR staat rood omdat hij `apps/**` of `.github/workflows/apps.yml` raakt
- Je wilt niet wachten op de maandelijkse Dependabot-PR (`npm`, lockfile-only, zie
  [`.github/dependabot.yml`](../../../.github/dependabot.yml))

**Niet** hiervoor: een sprong naar een nieuwe SDK-major. Zie *Afbreekpunt* onderaan.

## Waarom dit terugkomt

`expo-doctor` haalt zijn "expected"-kolom **live bij Expo** op. Elke Expo-patchrelease maakt de
lockfiles achterstallig zonder dat er iets in de repo verandert — dezelfde commit is vandaag rood en
was vorige maand groen. Achtergrond:
[`docs/developer/planning/specs/2026-08-22-dependabot-lane-design.md`](../../../docs/developer/planning/specs/2026-08-22-dependabot-lane-design.md),
addendum onderaan.

## De valkuil: gebruik `npm update` niet

Dit is de eerste ingeving en hij is fout. Gemeten op page-capture-app, 2026-08-22:

```
npm update expo jest-expo
→ top-level expo 54.0.37 ✓, jest-expo 54.0.18 ✓   (de versiecheck slaagt)
→ maar óók: node_modules/expo/node_modules/expo@57.0.15
→ expo-doctor: Found duplicates for expo-font, expo-keep-awake, expo-modules-core → rood
```

Oorzaak: tientallen packages binnen de Expo-boom declareren `"expo": "*"` als peer, en `npm update`
leest `*` als *pak de nieuwste*. Je haalt dus SDK 57 genest naar binnen terwijl je op 54 blijft. Dat
is exact de duplicate-native-module-toestand waar deze CI-lane in juli voor is opgetuigd — je zou de
check kapotmaken met het commando dat hem moest repareren.

Gebruik `npx expo install --fix`. Dat is SDK-bewust: het leest de versietabel van de geïnstalleerde
SDK en zet precies die versies neer, zonder `*`-peers mee te trekken.

## Workflow

### 1. Worktree

Dit levert commits op, dus begin in een worktree (`superpowers:using-git-worktrees`). Branchnaam
`fix-expo-lockfile-drift` of `issue-<nr>-expo-drift`.

### 2. Stel de drift vast

Node 22 is vereist (`node -v`) — de CI-lane draait daarop, en het testscript van dictation-app geeft
`NODE_OPTIONS=--localstorage-file` mee, wat oudere Node afwijst.

```bash
for a in dictation-app photo-offload-app page-capture-app; do
  echo "=== $a ==="
  (cd "apps/$a" && npm ci --silent && npx expo-doctor)
done
```

`npm ci` eerst, niet `npm install`: je begint vanaf de gecommitte lockfile, niet vanaf wat er
toevallig staat. Noteer per app de `package / expected / found`-tabel — die heb je in stap 5 nodig
voor de commit-message, en in het *Afbreekpunt* om te beslissen of dit nog wel jouw taak is.

### 3. Repareer

Per app:

```bash
cd apps/<app>
npx expo install --fix
npm dedupe
```

`expo install --fix` werkt `package.json` **en** `package-lock.json` bij: de ranges schuiven op naar
wat SDK 54 verwacht (`~54.0.36` → `~54.0.37`, `~54.0.0` → `~54.0.18`). Dat is bedoeld en blijft
binnen SDK 54 — `expo install` kan je per constructie niet naar een volgende SDK tillen, want het
resolvet tegen de tabel van de SDK die al geïnstalleerd is.

`npm dedupe` erachteraan is geen ritueel. Op dictation-app (2026-08-22) liet `--fix` een duplicaat
staan: `expo-constants@18.0.13` naast `18.0.14` onder `expo/`. De versiecheck was dan groen en de
duplicate-native-module-check rood — je zou denken dat de fix mislukt is terwijl er alleen nog
opgeruimd moest worden. Draai het altijd, ook als `--fix` niets lijkt te hebben gedaan; op de andere
twee apps is het een no-op.

Dat de manifests hier wél bewegen is het verschil met de Dependabot-entry, die op
`versioning-strategy: lockfile-only` staat en de ranges met rust laat. Beide komen uit op een
geïnstalleerde 54.0.37 en dus op een groene versiecheck; deze route is alleen explicieter.

### 4. Verifieer — allebei, per app

```bash
cd apps/<app>
npx expo-doctor          # moet "18/18 checks passed" melden
npm test --if-present    # exact wat de CI-lane draait
```

Claim niets over groen zonder deze twee uitvoeren gezien te hebben. Beide moeten exit 0 geven.
Referentie-uitvoer van de eerste volledige ronde (2026-08-22, node 22.22.2), alle drie
`18/18 checks passed`: dictation-app 15 tests, photo-offload-app 10, page-capture-app 17.

### 5. Commit

Eén commit per app houdt de diff leesbaar en maakt één app terugdraaibaar:

```
chore(deps): align <app> with SDK 54 patch versions

expo 54.0.36 → 54.0.37, jest-expo 54.0.17 → 54.0.18. expo-doctor 18/18,
17 tests groen. Geen SDK-wissel: alleen de patchtabel van 54 ingehaald.
```

Volg daarna de commit-flow uit `AGENTS.md`. Simplify is n.v.t. — er is geen code gewijzigd;
`/caveman-review` over de diff en `/caveman-commit` wel.

### 6. Controleer dat de diff is wat je denkt

```bash
git diff --stat                                   # alleen apps/*/package{,-lock}.json
git diff -- 'apps/*/package.json' | grep '^+.*"' # elke bump moet binnen dezelfde major blijven
```

Ziet één van die regels er uit als een major-sprong (`~55.`, `^6.`), dan is stap 3 niet gelopen zoals
bedoeld — terugdraaien en het *Afbreekpunt* lezen.

## Afbreekpunt: dit is een SDK-upgrade geworden

Vergelijk de `expected`-kolom uit stap 2 met de major in `package.json`:

| Situatie | Betekenis | Actie |
|---|---|---|
| verwacht `~54.0.37`, manifest `~54.0.36` | patchdrift | deze skill lost het op |
| verwacht `~55.x` of hoger | de SDK-pin zelf is aan de beurt | **stop** |

Het tweede geval is geen lockfile-refresh maar een SDK-migratie: nieuwe react-native (nu 0.81.5),
native-module-churn, dev-client opnieuw bouwen. Alle drie de apps staan bewust op SDK 54
([`apps/CLAUDE.md`](../../../apps/CLAUDE.md)). Schrijf er een issue voor via `/paia-write-issue`
(`2-area:clients`) en gebruik de `upgrading-expo`-skill; smokkel het niet mee in deze fix.

## Bekende uitzondering

`page-capture-app/package.json` sluit `react-native-shared-group-preferences` en
`expo-share-extension` uit van de React Native Directory-check
(`expo.doctor.reactNativeDirectoryCheck.exclude`). Dat is bestaand en bedoeld — voeg er niets aan toe
om een refresh groen te krijgen; zie [`apps/CLAUDE.md`](../../../apps/CLAUDE.md).
