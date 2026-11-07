# GitLab-forge: configuratie en init (#257) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `forge.kind = "gitlab"` en GitLab-paden met subgroepen zijn geldige configuratie in Python en Go, en `lighthouse init` herkent een GitLab-remote (gitlab.com of een self-hosted host uit de configuratie) en stelt `[forge]` voor.

**Architecture:** `config.py` kiest de repo-regex op `forge.kind` en krijgt een optionele sleutel `forge.host` voor een self-hosted GitLab. In Go vervangt `projectconfig.ForgeFromRemote` de functie `ForgeRepo`: hij geeft kind, repo en host terug; `CoreTables` schrijft die. `init` leest de bekende GitLab-hosts uit de bestaande `lighthouse.toml` en uit de nieuwe vlag `--forge-host`. Alleen parsen en herkennen: geen GitLab-client (delen 2 tot en met 4 van de milestone).

**Tech Stack:** Python 3.11 (`pack/core/lib/config.py`, pytest), Go (`internal/projectconfig`, `internal/initcmd`, `internal/repoconfig`, `internal/cli`, cobra).

**Spec:** [2026-11-07-m3-tweede-omgeving-design.md](../specs/2026-11-07-m3-tweede-omgeving-design.md), onderdeel *Nieuwe milestones*, *GitLab-forge*, deel 1.

## Global Constraints

- Artefacten (commits, changelog, docs, plan) in het Nederlands behalve waar de bestaande tekst Engels is; code, commentaar en namen in het Engels (`code_language = en`). `docs/getting-started.md` blijft Engels.
- Een wijziging onder `pack/` eindigt met `go run ./cmd/lighthouse init --upgrade` en een commit van de geïnstalleerde kopie; de tweede run zegt `No changes.`
- Atomaire Conventional Commits; nooit `--no-verify`. Elke PR krijgt een fragment in `changelog.d/`.
- Go volgens `/lighthouse-go-standard`, tests volgens `/lighthouse-test-standard`.
- Eén bron voor de tekenset van een padsegment: `[A-Za-z0-9._-]+` (Python `_REPO`, Go `repoPart`). Een aanhalingsteken of `<` bereikt nooit een URL of regex.
- Geen nieuwe GitHub-aanname; `internal/forge`, `internal/workplace/kennforge`, `internal/cli/triage.go` en de skills blijven buiten scope (latere delen van de milestone).

## Beslissingen

| Vraag | Keuze | Waarom |
|---|---|---|
| `owner`/`name` bij `groep/sub/repo` | `owner` = alles vóór de laatste `/` (`groep/sub`), `name` = laatste segment | Zo heet het ook in GitLab (namespace en projectnaam). Voor GitHub verandert niets. |
| Waar staat de self-hosted host | `[forge] host = "git.example.org"`, optioneel, alleen bij `kind = "gitlab"`, alleen een hostnaam | Staat in `lighthouse.toml`, niet in code. `gitlab.com` is de enige ingebakken host en krijgt geen `host`-sleutel. |
| Hoe kent `init` de host vóór de eerste run | Vlag `--forge-host <host>` (herhaalbaar), plus `forge.host` uit een bestaande `lighthouse.toml` | Zonder dat is een willekeurige SSH-host niet van een GitLab te onderscheiden. Geen host-gok op naam. |
| Controle op `kind` in Go | `repoconfig.Load` weigert een kind buiten `github`/`gitlab` | Nu accepteert Go elke tekenreeks; "onbekende waarde blijft een fout" geldt dan ook voor Go. Lege waarde blijft `github`. |
| `ForgeRepo` | Hernoemd en uitgebreid tot `ForgeFromRemote(url, gitlabHosts) (Forge, bool)`; enige aanroeper is `initcmd` | Een tweede functie naast de oude zou de regex-lijst dupliceren. |

## Review Focus

- Een GitHub-repo met drie segmenten (`a/b/c`) blijft een fout, ook zonder `kind` (standaard `github`).
- `forge.host` bij `kind = "github"`, of met een pad, poort, schema of spatie, is een fout.
- Een remote van een onbekende host (`git@git.example.org:g/s/r.git`) wordt niet als GitLab voorgesteld zolang de host niet in de configuratie of vlag staat.
- `https://gitlab.com/g/s/r.git/` met slash en `.git`, en `ssh://git@host:2222/g/s/r`, geven `g/s` + `r` zonder poort of `.git` in de repo.
- Een segment met `"` of `<` in een GitLab-pad wordt afgewezen.

---

### Task 1: `config.py` kent GitLab en `forge.host`

**Files:**
- Modify: `pack/core/lib/config.py:56-58` (regex's), `:227-231` (`owner`/`name`), `:65-67` (`SCHEMA`: niets, `host` wordt apart gecontroleerd), `:393-402` (kind-, repo- en host-check)
- Modify: `docs/site/data/config.json` (kaart: `forge.kind` opties `github`/`gitlab` en de `about`-teksten van `forge.kind` en `forge.repo`, die nu "only github" en `owner/name` zeggen; nieuwe sleutel `forge.host` met `level`, `required: false`, `example: "git.example.org"` en `readers: ["pack/core/lib/config.py", "internal/initcmd/initcmd.go"]`)
- Test: `tests/pack/test_config.py:143-145` en `:735-740`, `tests/test_site_config.py:26`, `:461-466`, `:475`

**Interfaces:**
- Produces: `forge.kind` ∈ {`github`, `gitlab`}; `forge.host` (str, optioneel) leesbaar met `config.py forge.host`, rc 1 als niet gezet; `Config.owner`/`name` splitsen op de laatste `/`; `forge.owner`/`forge.name` blijven afgeleide sleutels.

- [ ] **Step 1: Schrijf de falende tests** in `tests/pack/test_config.py`:
  - de rij `(_with(("forge", "kind"), "gitlab"), "kind")` verdwijnt uit de afwijzingen en wordt `(_with(("forge", "kind"), "forgejo"), "kind")`; `test_broken_forge_is_rc_2` gebruikt `kind = "forgejo"`.
  - `test_gitlab_subgroup_repo`: `kind = "gitlab"`, `repo = "groep/sub/repo"` laadt; `cfg.owner == "groep/sub"`, `cfg.name == "repo"`; `main(["forge.owner"])` en `forge.name` geven hetzelfde.
  - `test_gitlab_repo_needs_two_segments`: `kind = "gitlab"`, `repo = "alleen"` is een `ConfigError` met `repo`.
  - `test_github_keeps_one_slash`: `a/b/c` met `kind = "github"` en zonder `kind` blijft een fout.
  - parametrisch `test_gitlab_repo_rejects_unsafe_segments` met `g/s/<x>`, `g//r`, `g/s/r/`, `g/s"/r`.
  - `test_forge_host`: `kind = "gitlab"`, `host = "git.example.org"` geeft `config.py forge.host` → `git.example.org\n`; afgewezen met `forge.host` in de melding: `host` bij `kind = "github"`, en de waarden `https://x`, `x/y`, `x:22`, `a b`, `""`.
  - `forge.host` zonder sleutel: rc 1 (voeg `("forge.host", 1, "")` toe aan `test_forge_with_only_default_branch`).
- [ ] **Step 2: Draai ze** — `python3 -m pytest tests/pack/test_config.py -q` — verwacht FAIL op de nieuwe en gewijzigde gevallen.
- [ ] **Step 3: Implementeer** in `config.py`: `_SEGMENT = r"[A-Za-z0-9._-]+"`, `_REPO = {"github": re.compile(seg/seg), "gitlab": re.compile(seg(/seg)+)}` (kies de regex op de gelezen `kind`, standaard `github` als `kind` ontbreekt); `_HOST = re.compile(r"[A-Za-z0-9.-]+")`. Kindcontrole: `kind in ("github", "gitlab")`, melding noemt beide. Repo-melding noemt `owner/name` (github) of `group/…/name` (gitlab). `host`: `_get(forge, "host", str, "forge.", path)`, fout als `kind != "gitlab"` of niet `_HOST.fullmatch`. `Config` krijgt `host: str | None`; `owner`/`name` met `rpartition("/")`. Voeg `forge.host` toe aan `_BASE_KEYS` (rc 1 als afwezig, zoals `forge.repo`). Werk het commentaar bij `_REPO` bij: geldt niet langer alleen voor GitHub.
- [ ] **Step 4: Pas de pins aan:** `tests/test_site_config.py:26` (`SET` krijgt `forge.host`) en `:475` (`"forge.kind": ["github", "gitlab"]`); maak de kaartrijen in `config.json` hier af (zie Files). De kaart rendert elke sleutel zonder default als voorbeeldregel en `test_toml_examples_pass_config_py` (`:461-466`) haalt alle voorbeelden uit commentaar en laadt ze met `config.py`; het `forge.kind`-voorbeeld is `github`, dus `host = "git.example.org"` zou daar een `ConfigError` geven. Beslissing: de test vervangt na `uncommented(...)` de regel `kind = "github"` door `kind = "gitlab"` (assert dat de vervanging plaatsvond) en controleert `cfg.raw["forge"]["host"] == "git.example.org"`; het gepubliceerde voorbeeld blijft `github`. Het `readers`-veld van `forge.host` volgt de leesregel in `tests/test_site_config.py:121-130`: Python leest `host` in `config.py`; Go leest het in `initcmd.go` (Task 3, struct met `toml:"forge"` en `toml:"host"` in hetzelfde bestand).
- [ ] **Step 5: Draai** `python3 -m pytest tests/pack/test_config.py tests/test_site_config.py tests/test_getting_started.py -q` — verwacht PASS (de docs-rij in `docs/getting-started.md` hoort bij Task 4 stap 4; als `test_getting_started.py` of `test_site_config.py` die hier al eist, voeg alleen die rij nu toe).
- [ ] **Step 6: Dogfood en commit:** `go run ./cmd/lighthouse init --upgrade` (de kopie in `.lighthouse/lib/lighthouse_lib/config.py` verandert), nogmaals → `No changes.`

```bash
git add pack/core/lib/config.py .lighthouse tests docs/site/data/config.json
git commit -m "feat(config): forge.kind gitlab, subgroepen en forge.host (#257)"
```

---

### Task 2: `projectconfig` herkent GitLab-remotes

**Files:**
- Modify: `internal/projectconfig/projectconfig.go:134-163`
- Test: `internal/projectconfig/projectconfig_test.go:30,54,88,108` (aanroepen van `CoreTables`) en `:114-132` (`TestForgeRepo`)

**Interfaces:**
- Produces:
  - `type Forge struct{ Kind, Repo, Host string }` — `Host` is `""` voor `github.com` en `gitlab.com`.
  - `func ForgeFromRemote(remoteURL string, gitlabHosts []string) (Forge, bool)` — GitHub: https, scp, `ssh://` op `github.com`, precies `owner/name`. GitLab: dezelfde drie vormen op `gitlab.com` en op elke host in `gitlabHosts`, pad met ≥ 2 segmenten, elk segment `repoPart`; `ssh://` met poort; https met poort; `.git` en slotslash weg. `Host` is de eerste treffer uit `gitlabHosts` (kleine letters), niet `gitlab.com`. Hosts in `gitlabHosts` die geen hostnaam zijn (`[A-Za-z0-9.-]+`) worden genegeerd.
  - `func CoreTables(f Forge, haveRepo bool) []Table` — `[forge]` met `kind`, `repo` en, alleen bij `Host != ""`, `host`.
- Vervalt: `ForgeRepo`.
- Poort: een poort in een `https://`- of `ssh://`-remote wordt herkend maar niet bewaard (`forge.host` is alleen een hostnaam, bewuste keuze). De hostvergelijking is hoofdletteronafhankelijk (`(?i)`).

- [ ] **Step 1: Schrijf de falende tests.** `TestForgeFromRemote` (tabel in→Forge): de bestaande zes GitHub-gevallen met `Kind: "github"`; `https://gitlab.com/g/s/r.git/`, `git@gitlab.com:g/s/r.git`, `ssh://git@gitlab.com/g/s/r`, `https://gitlab.com/g/r` → `Kind: "gitlab"`, `Host: ""`; met `gitlabHosts = ["git.example.org"]`: `https://git.example.org/g/s/r`, `git@git.example.org:g/r.git`, `ssh://git@git.example.org:2222/g/s/r.git`, `https://git.example.org:8443/g/r` → `Host: "git.example.org"`. Afwijzingen: `https://gitlab.com/solo`, `https://github.com/a/b/c`, `git@git.example.org:g/s/r.git` zonder `gitlabHosts`, `https://gitlab.com/g/s"/r`, `git@gitlab.com:g/<x>/r`, `""`. De rij `https://gitlab.com/a/b` verdwijnt uit de afwijzingen (AC). `TestCoreTables…` passen aan op de nieuwe signatuur, plus een geval dat `host = "git.example.org"` verschijnt en bij `gitlab.com` niet.
- [ ] **Step 2: Draai** `go test ./internal/projectconfig/...` — verwacht FAIL (compileert niet).
- [ ] **Step 3: Implementeer** de signaturen hierboven. Bouw de GitLab-regexes per aanroep met `regexp.QuoteMeta(host)` voor `gitlab.com` plus elke bekende host; splits het pad op `/` en valideer elk segment met `repoPart`. Houd GitHub op precies twee segmenten.
- [ ] **Step 4: Draai** `go test ./internal/projectconfig/... && go vet ./internal/projectconfig/...` — PASS.
- [ ] **Step 5: Houd de boom compileerbaar.** `internal/initcmd/initcmd.go:457` en `:459` roepen `ForgeRepo` en `CoreTables(repo, haveRepo)` nog aan, en de testgate selecteert importers niet (`gate_go.py`). Pas die twee aanroepen minimaal aan (`f, ok := projectconfig.ForgeFromRemote(url, nil)`, `CoreTables(f, ok)`) en draai `go build ./...`; Task 3 bouwt daarop verder. Commit:

```bash
git add internal/projectconfig internal/initcmd
git commit -m "feat(projectconfig): GitLab-remotes herkennen met ForgeFromRemote (#257)"
```

---

### Task 3: `init` stelt `[forge]` voor bij een GitLab-remote

**Files:**
- Modify: `internal/initcmd/initcmd.go:48-62` (`Options.ForgeHosts []string`), `:452-461` (`configTables`), `:515-521` (waarschuwing), `internal/cli/init.go:40-44` (vlag), `docs/site/agents/install.md:258-259` (de aangehaalde waarschuwing en het advies)
- Test: `internal/initcmd/initcmd_test.go:675-697`

**Interfaces:**
- Consumes: `ForgeFromRemote`, `CoreTables`, `Forge` uit Task 2.
- Produces: `Options.ForgeHosts []string`; vlag `--forge-host <host>` (`StringArrayVar`, beschrijving: "host of a self-hosted GitLab (repeatable)"). `configTables(root string, chosen []packs.Pack, extraHosts []string)`: bekende hosts = `extraHosts` plus `forge.host` uit de bestaande `<root>/lighthouse.toml` (leesfout of ontbrekende sleutel = geen host). Lees die met een kleine struct in `initcmd.go` met de tags `toml:"forge"`, `toml:"kind"`, `toml:"host"` (dat maakt `initcmd.go` een reader voor de kaart).

- [ ] **Step 1: Schrijf de falende tests** naast `TestForgeFromGitHubRemote`: `TestForgeFromGitLabRemote` (remote `git@gitlab.com:groep/sub/repo.git` → `[forge]\nkind = "gitlab"\nrepo = "groep/sub/repo"\n`, geen `host`); `TestForgeFromSelfHostedGitLabFlag` (`Options.ForgeHosts = ["git.example.org"]`, remote `https://git.example.org/g/s/r` → bevat `kind = "gitlab"`, `repo = "g/s/r"`, `host = "git.example.org"`); `TestForgeHostFromExistingToml` (bestaande `lighthouse.toml` met `[forge]\nkind = "gitlab"\nhost = "git.example.org"` en zonder `repo`, remote `git@git.example.org:g/s/r.git`, geen `--forge-host`, `--upgrade` → `repo = "g/s/r"` wordt toegevoegd; zonder de toml-lezing in `configTables` is deze test rood); `TestNoHostIntoGitHubForge` (bestaande `[forge]\nkind = "github"\nrepo = "a/b"`, remote `https://git.example.org/g/r`, `ForgeHosts = ["git.example.org"]`, `--upgrade` → er komt geen `host`-regel in de tabel); `TestNoForgeForUnknownHost` (remote `git@git.example.org:g/s/r.git` zonder host → geen `[forge]`, waarschuwing). `TestNoForgeWithoutGitHubRemote` blijft groen op `"[forge]"` in de waarschuwing.
- [ ] **Step 2: Draai** `go test ./internal/initcmd/...` — FAIL.
- [ ] **Step 3: Implementeer** `configTables` en de vlag; hernoem de waarschuwing naar "no GitHub or GitLab remote 'origin'" (de substring `[forge]` blijft). Een bestaande `[forge]`-tabel wordt niet overschreven; `AppendMissing` vult per sleutel aan. Daarom stelt `configTables` `host` alleen voor als de bestaande `[forge]` geen `kind` heeft of `kind = "gitlab"`; bij een bestaand `kind = "github"` wordt `Host` voor `CoreTables` leeggemaakt, anders ontstaat een `host` bij `kind = "github"` die `config.py` afwijst. Pas `docs/site/agents/install.md:258-259` aan de nieuwe waarschuwing aan (GitLab erbij, advies: `[forge]` alleen met de `owner/name` of `group/…/name` van de gebruiker, of `--forge-host`).
- [ ] **Step 4: Draai** `go test ./... && go vet ./... && python3 -m pytest tests/test_agent_guide.py -q` — PASS (die laatste pint de waarschuwing uit `install.md` tegen de Go-bron).
- [ ] **Step 5: Commit:**

```bash
git add internal/initcmd internal/cli docs/site/agents/install.md
git commit -m "feat(init): GitLab-remote herkennen en [forge] voorstellen (#257)"
```

---

### Task 4: Go weigert een onbekend `kind`; documentatie en changelog

**Files:**
- Modify: `internal/repoconfig/repoconfig.go:120-135` (na het lezen van `Kind`), `docs/getting-started.md:147-148`
- Test: `internal/repoconfig/repoconfig_test.go:148-162`
- Create: `changelog.d/feat-gitlab-forge-config.md`

- [ ] **Step 1: Schrijf de falende tests.** `TestLoadKindAndLanguage` gebruikt `kind = "gitlab"` (nu `forgejo`); nieuwe `TestLoadRejectsUnknownKind`: `kind = "forgejo"` geeft een fout met `forge.kind` in de melding; `kind` leeg of afwezig blijft `github`.
- [ ] **Step 2: Draai** `go test ./internal/repoconfig/...` — FAIL.
- [ ] **Step 3: Implementeer** de controle `kind ∈ {github, gitlab}` in `Load`, met een fout die het pad noemt, in de stijl van de bestaande `missing [forge] repo`.
- [ ] **Step 4: Docs:** in `docs/getting-started.md` de rijen voor `forge.kind` (`github` or `gitlab`), `forge.repo` (`owner/name` on GitHub, `group/subgroup/name` on GitLab) en een nieuwe rij `forge.host` (hostname of a self-hosted GitLab; `lighthouse init --forge-host <host>` writes it). Vermeld de vlag ook waar `init` is beschreven. - [ ] **Step 5: Changelog:** `changelog.d/feat-gitlab-forge-config.md` met frontmatter `type: added` en één alinea: `forge.kind = "gitlab"`, repo-paden met subgroepen, `forge.host` en `lighthouse init` herkent GitLab-remotes; nog geen GitLab-client.
- [ ] **Step 6: Verifieer alles:** `python3 -m pytest -q`, `go test ./...`, `go vet ./...`, `go run ./cmd/lighthouse init --upgrade` (→ `No changes.` na Task 1) en het commando uit `[test_gate]` in `lighthouse.toml`.
- [ ] **Step 7: Commit:**

```bash
git add internal/repoconfig docs changelog.d tests
git commit -m "feat(config): Go weigert een onbekend forge.kind; docs voor GitLab (#257)"
```

## Dekking van de acceptatiecriteria

| AC | Task |
|---|---|
| `kind = "gitlab"` geldig in Python en Go, onbekend blijft fout | 1, 4 |
| Subgroepen voor GitLab, GitHub precies één `/` | 1 |
| `init` herkent https, scp, `ssh://`, gitlab.com en self-hosted; de test die gitlab.com afwees herkent hem nu | 2, 3 |
| Self-hosted host in de configuratie, niet in code | 1 (`forge.host`), 3 (vlag en bestaande toml) |
