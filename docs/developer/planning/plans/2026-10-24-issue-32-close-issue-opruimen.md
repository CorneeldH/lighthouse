# close-issue ruimt alle worktrees van het issue automatisch op — implementatieplan (#32)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Na een sluiting vindt `close-issue` alle branches van het issue, laat per branch het audit-verdict beslissen en ruimt elke `cleanup: yes` zonder vraag op, ook headless (B7).

**Architecture:** `worktree_audit.py` krijgt een view `--issue <nr>`. Die haalt met één `gh api graphql` de headbranches van de gemergde PR's die naar het issue verwijzen (`Closes` en `Refs`), voegt de lokale branches met prefix `issue-<nr>-` toe, en geeft per branch een blok in de vorm van `--only`, op basis van één `collect`. Het script blijft alleen-lezen. De skill draait per blok met `cleanup: yes` de geleverde regels zelf, zonder vraag, en meldt elke andere uitkomst letterlijk.

**Tech Stack:** Python ≥ 3.11 (stdlib, `gh` CLI), pytest via `uv`, Go voor `lighthouse init --upgrade`.

**Spec:** issue #32 (body met acceptatiecriteria en DoD). Er is geen aparte spec: de AC leggen het HOE vast, op de bronkeuze na. Die staat hieronder als ontwerpkeuze 1.

## Ontwerpkeuzes

1. **Bron: `worktree_audit.py --issue <nr>`, geen GraphQL in de skill.** Drie redenen. (a) Unit-testbaar met gefakete invoer, net als de rest van het script. (b) Eén `collect` voor alle branches; N keer `--only` doet N keer de volledige `gh api --paginate` over alle PR's. (c) De skill houdt één commando en één regel om te lezen.
2. **`Refs`-PR's via `CROSS_REFERENCED_EVENT`.** `closedByPullRequestsReferences` (de query van stap 0) kent alleen PR's met een sluitwoord. Een PR-body met `Refs #32` maakt op issue 32 een `CrossReferencedEvent` met de PR als `source`. De query vraagt beide en houdt alleen PR's met `merged: true` uit dezelfde repo (`repository.nameWithOwner == forge.repo`).
3. **"Gemergde PR" zit in het script.** De oude skillregel "at A or B the PR column of the row counts: only `#<pr> merged` goes on" wordt een regel van `--issue`: een branch waarvan de rij geen `pr_state == "merged"` heeft, krijgt `cleanup: no — no merged PR`, ook als de ladder `safe to remove` zegt. Zo geldt het voor elke ingang gelijk, en kan een lokale `issue-<nr>-`-branch zonder PR nooit weg.
4. **Exitcode van `--issue`.** `0`: de PR-lookup lukte en elke branch kreeg een blok. `2`: de PR-lookup mislukte (`cleanup: no — PR lookup for issue #<nr> failed`). Dan ruimt de skill niets op, ook geen lokale `issue-<nr>-`-branch: de afwezigheid van een signaal is geen ja. Het verdict per branch staat in zijn blok, niet in de exitcode. Opruimbaar is dus: exit `0` én in het blok een regel `cleanup: yes`.
5. **B7 ruimt op vóór de close-comment** (*besluit Corneel, 2026-10-24*, AC 4). Alleen in de rij "all criteria demonstrably done". De regels per branch gaan als bewijsregels mee in die comment. Bij een mislukte comment is er al opgeruimd, en dat mag: elke opgeruimde branch had een gemergde PR en een schoon verdict.
6. **De paia-spiegel volgt via `lighthouse init`.** Paia draagt nog `paia-close-issue`. In de core-PR van M0 (d) ([plan](2026-10-18-m0d-overstap.md)) vervangt `lighthouse init` die kopie door `lighthouse-close-issue` uit `pack/core`. Daarmee komt deze wijziging mee, mits de tag die paia installeert na deze merge ligt.

## Global Constraints

- Nooit `--force`, `git worktree unlock` of `git push origin --delete`, ook niet als git weigert. Het script levert alleen `git worktree remove "<pad>"` en `git branch -D <branch>`, in die volgorde.
- De worktree waarin de sessie draait (cwd, ook een submap ervan), wordt nooit opgeruimd: `render_only` toetst dat al (`own cwd is in <wt>`).
- Het script muteert niets. `READONLY_GIT_VERBS` blijft ongewijzigd. `gh` loopt alleen via `gh api`, nooit `gh pr list --json`.
- Skilltekst in het Engels. Beide varianten (`claude/`, `agents/`) krijgen identieke wijzigingen: `test_variants_differ_in_the_instruction_file` staat precies één verschilregel toe.
- Commitberichten in het Nederlands, Conventional Commits.
- Dogfood: na elke wijziging onder `pack/` draai je `go run ./cmd/lighthouse init --upgrade` en commit je de geïnstalleerde kopie in dezelfde commit. Een tweede run zegt `No changes.`
- Testgate: `uv run pytest -q && go test ./...`.

## Review Focus

1. **Prefixbotsing `issue-3-` tegen `issue-32-`.** Bij `--issue 3` mag `issue-32-x` niet meekomen. Test: `test_prefix_needs_the_dash` (taak 1).
2. **Een cross-reference uit een andere repo** met een gemergde PR op een headbranch die hier ook bestaat. Die telt niet. Test: `test_parse_skips_other_repos` (taak 1).
3. **GraphQL zonder issue** (verkeerd nummer, `issue: null`) of met `errors`. Dat is een mislukte lookup (exit 2), geen "geen PR's". Test: `test_parse_null_issue_is_failure` en `test_main_exit_2_on_failed_lookup` (taak 1).
4. **Een lokale `issue-<nr>-`-branch met een open PR** die de ladder toch veilig noemt. Die krijgt `cleanup: no — no merged PR`. Test: `test_local_branch_without_merged_pr_is_no` (taak 1).
5. **De sessie zit in één van de worktrees van het issue.** Dat blok zegt `own cwd`, de andere blokken blijven `yes`. Test: `test_own_cwd_blocks_only_its_branch` (taak 1).

---

### Taak 1: `worktree_audit.py --issue <nr>`

**Files:**
- Modify: `pack/core/scripts/worktree_audit.py` (bij `run_gh_pulls` :764, `render_only` :1273, `main` :1344)
- Test: `tests/pack/test_worktree_audit.py` (nieuwe klasse `TestRenderIssue` na `TestRenderOnly` :1318)
- Installed: `.lighthouse/scripts/worktree_audit.py`, `.lighthouse/manifest.toml`

**Interfaces:**
- Produces:
  - `ISSUE_PRS_QUERY: str`, de GraphQL-tekst (hieronder)
  - `parse_issue_pr_heads(payload: dict, slug: str) -> list[str] | None`: puur
  - `run_gh_issue_pr_heads(slug: str | None, nr: int) -> list[str] | None`: `None` = lookup mislukt of geen `forge.repo`
  - `issue_branches(rows: list[Row], nr: int, pr_heads: list[str]) -> list[str]`: gesorteerd, zonder dubbelen
  - `render_issue(rows: list[Row], nr: int, pr_heads: list[str] | None, *, cwd: Path, alive: Callable[[int], bool] = pid_alive) -> str`
  - CLI: `--issue NR` (int) in de bestaande mutually exclusive groep; exit 0 of 2 (ontwerpkeuze 4)

De query; `owner` en `name` komen uit `slug.split("/", 1)`:

```graphql
query($owner:String!,$name:String!,$nr:Int!){repository(owner:$owner,name:$name){issue(number:$nr){
  closedByPullRequestsReferences(first:50,includeClosedPrs:true){nodes{headRefName merged repository{nameWithOwner}}}
  timelineItems(itemTypes:[CROSS_REFERENCED_EVENT],first:100){nodes{... on CrossReferencedEvent{source{... on PullRequest{headRefName merged repository{nameWithOwner}}}}}}}}}
```

Blokvorm van `render_issue`. Per branch een blok: de eerste regel is `branch: <branch>`, daarna de uitvoer van `render_only` voor die branch. Tussen twee blokken staat één lege regel. Elk blok bevat precies één regel die met `cleanup: ` begint. Zegt `render_only` `yes` terwijl de rij geen `pr_state == "merged"` heeft, dan vervang je de `cleanup: yes`-regel en de commando's eronder door `cleanup: no — no merged PR`. Geen branches: `cleanup: no — no branches found for issue #<nr>\n`. Als `pr_heads is None`: `cleanup: no — PR lookup for issue #<nr> failed\n`.

- [ ] **Stap 1: Schrijf de falende tests** in `TestRenderIssue`. Gebruik de bestaande helpers `row(...)`, `never_alive` en `ELDERS = Path("/elders")`, en `WT = "/repo/.claude/worktrees/issue-9-x"`:
  - `test_parse_keeps_merged_same_repo_heads`: een payload met een closing-node (`issue-9-x`, merged, `o/r`) en een cross-ref-node (`fix-y`, merged, `o/r`) geeft `["issue-9-x", "fix-y"]` (de volgorde zoals ze binnenkomen, zonder dubbelen).
  - `test_parse_skips_unmerged_and_non_pr`: een node met `merged: false` en een cross-ref met `source: {}` (een issue) leveren niets op.
  - `test_parse_skips_other_repos`: een node met `nameWithOwner: "x/fork"` levert niets op, bij `slug="o/r"`.
  - `test_parse_null_issue_is_failure`: `{"data": {"repository": {"issue": None}}}` → `None`, en `{"errors": [...]}` → `None`.
  - `test_prefix_needs_the_dash`: rows `issue-3-a`, `issue-32-b`; `issue_branches(rows, 3, [])` → `["issue-3-a"]`.
  - `test_pr_head_and_prefix_dedupe`: `issue_branches([row("issue-9-x")], 9, ["issue-9-x", "fix-y"])` → `["fix-y", "issue-9-x"]`.
  - `test_two_merged_branches_both_yes`: rows `issue-9-x` (worktree `WT`, merged) en `issue-9-y` (zonder worktree, merged); de uitvoer heeft twee blokken, `branch: issue-9-x` staat vóór `branch: issue-9-y`, er zijn twee regels `cleanup: yes`, en `git worktree remove "{WT}"` staat vóór `git branch -D issue-9-x`.
  - `test_local_branch_without_merged_pr_is_no`: `issue-9-x` met `pr_state="open"` geeft in zijn blok `cleanup: no — ` en geen regel die met `git ` begint. Monkeypatch `audit.render_only` zodat het `("cleanup: yes\ngit branch -D issue-9-x\n", True)` teruggeeft; een rij met `pr_state=None` geeft dan `cleanup: no — no merged PR` en geen `git branch -D`.
  - `test_own_cwd_blocks_only_its_branch`: twee gemergde worktrees, `cwd=Path(WT) / "src"`; het blok van `issue-9-x` zegt `own cwd is in {WT}`, dat van de andere `cleanup: yes`.
  - `test_unknown_pr_head_is_reported`: `pr_heads=["gone"]` zonder rij geeft het blok `branch: gone` met `cleanup: no — branch gone not found`.
  - `test_failed_lookup_and_empty`: `render_issue(rows, 9, None, ...)` == `"cleanup: no — PR lookup for issue #9 failed\n"`; `render_issue([], 9, [], ...)` == `"cleanup: no — no branches found for issue #9\n"`.
  - `test_main_exit_2_on_failed_lookup` en `test_main_exit_0_with_blocks`: monkeypatch `audit.collect` en `audit.run_gh_issue_pr_heads` (en `audit.repo_slug` op `lambda: "o/r"`); `main(["--issue", "9"])` geeft 2 bij `None` en 0 bij `[]` met een gemergde `issue-9-x`-rij, en dan staat `cleanup: yes` in stdout.
  - In `test_runs_as_installed_file` (:1863) komt erbij: `"--issue" in result.stdout`.

- [ ] **Stap 2: Draai ze, verwacht FAIL**

  Run: `uv run pytest -q tests/pack/test_worktree_audit.py -k "RenderIssue or runs_as_installed"`
  Expected: FAIL met `AttributeError: module 'worktree_audit' has no attribute 'parse_issue_pr_heads'`.

- [ ] **Stap 3: Implementeer** de vijf namen uit *Interfaces*. `run_gh_issue_pr_heads` volgt `run_gh_pulls`: `gh api graphql -f owner=… -f name=… -F nr=<nr> -f query=<ISSUE_PRS_QUERY>`, `timeout=300`, en `None` bij een `OSError`/`SubprocessError`, rc ≠ 0 of een `JSONDecodeError`. Leg in de docstring uit waarom cross-references nodig zijn (ontwerpkeuze 2). `render_issue` roept per branch `render_only` aan; geen tweede ladder. Help-tekst van `--issue`: `all branches of an issue, one block each with \`cleanup: yes|no\` (exit 2 if the PR lookup fails), for lighthouse-close-issue`. In `main`: `pr_heads = run_gh_issue_pr_heads(repo_slug(), args.issue)` na `collect`; geef `2` terug als `pr_heads is None`, anders `0`.

- [ ] **Stap 4: Draai de tests, verwacht PASS**

  Run: `uv run pytest -q tests/pack/test_worktree_audit.py tests/pack/test_worktree_audit_skill.py`
  Expected: alles groen.

- [ ] **Stap 5: Dogfood en commit**

  ```bash
  go run ./cmd/lighthouse init --upgrade
  go run ./cmd/lighthouse init --upgrade   # verwacht: No changes.
  git add pack/core/scripts/worktree_audit.py tests/pack/test_worktree_audit.py .lighthouse/
  git commit -m "feat(worktree-audit): --issue geeft per branch van een issue een opruimverdict"
  ```

### Taak 2: close-issue ruimt alles op, zonder vraag, ook headless

**Files:**
- Modify: `pack/core/skills/close-issue/claude/SKILL.md` en `pack/core/skills/close-issue/agents/SKILL.md`: de B7-rij (:187), de hele sectie `## Clean up the worktree — the last step` (:436-492)
- Modify: `pack/core/skills/worktree-audit/SKILL.md:10-21`
- Test: `tests/pack/test_close_issue_skill.py` (:164-168 plus nieuwe tests), `tests/pack/test_worktree_audit_skill.py`
- Installed: `.claude/skills/lighthouse-close-issue/SKILL.md`, `.agents/skills/lighthouse-close-issue/SKILL.md`, `.claude/skills/lighthouse-worktree-audit/SKILL.md`, `.agents/skills/lighthouse-worktree-audit/SKILL.md`, `.lighthouse/manifest.toml`

**Interfaces:**
- Consumes: `worktree_audit.py --issue <nr>`, de blokvorm en de exitcodes uit taak 1.

- [ ] **Stap 1: Pas de tests aan en schrijf de nieuwe.** In `test_close_issue_skill.py`, over beide varianten (`for body in skill()`) en met `cleanup = _section(body, "## Clean up the worktree")`:
  - `test_cleanup_reads_the_audit_token`: `--only <branch>` wordt `python3 .lighthouse/scripts/worktree_audit.py --issue <nr>`; de asserts op `` `cleanup: yes` `` en `` `cleanup: no — <reason>` `` blijven.
  - `test_cleanup_asks_no_question`: `"AskUserQuestion" not in cleanup` en `"after confirmation" not in cleanup`.
  - `test_cleanup_runs_headless_too`: `"never clean up" not in body`, en in de B7-rij van entry B staat niet meer `you do not clean up yourself` (assert op de hele body).
  - `test_cleanup_keeps_the_forbidden_lines`: `` "❌ `--force`, `git worktree unlock` or `git push origin --delete`" in cleanup ``.
  - `test_cleanup_goes_on_after_a_refusal`: `"go on with the next branch" in cleanup`.
  - `test_cleanup_shows_evidence_per_branch`: `"git -C <main checkout> branch --list <branch>" in cleanup` en `"git -C <main checkout> worktree list" in cleanup`.
  - `test_cleanup_never_removes_own_cwd`: `"own cwd" in cleanup`.

  In `test_worktree_audit_skill.py` komt erbij: de regel `python3 .lighthouse/scripts/worktree_audit.py --issue <nr>` staat in de skill.

- [ ] **Stap 2: Draai ze, verwacht FAIL**

  Run: `uv run pytest -q tests/pack/test_close_issue_skill.py tests/pack/test_worktree_audit_skill.py`
  Expected: FAIL op onder meer `test_cleanup_reads_the_audit_token` en `test_cleanup_asks_no_question`.

- [ ] **Stap 3: Herschrijf de B7-rij** (:187), in beide varianten. De zin "The `cleanup:` line of *Clean up the worktree* goes along as an evidence line in that close comment; you do not clean up yourself." wordt: "Before that close comment, run *Clean up the worktree*; its result per branch goes along as evidence lines in the close comment."

- [ ] **Stap 4: Herschrijf de sectie** `## Clean up the worktree — the last step` in beide varianten met deze inhoud. De heading blijft. Binnen de sectie komt geen `## `-heading: `_section` knipt daarop.

  - Inleiding: blijft (de ladder van `/lighthouse-worktree-audit`, geen tweede set regels).
  - **When.** Een tabel:

    | Entry | Clean up? |
    |---|---|
    | C, exit `0` in C3 | yes, the main moment |
    | A or B, after the close | yes |
    | B7, all criteria `demonstrably done` | yes, before the close comment |
    | C with reopening, `COULD NOT CHECK` or `closed without a closing PR`; A, B or B7 without a close | no |

  - **Branches.** "All branches of the issue: the head branch of every merged PR that refers to it (`Closes` and `Refs`), plus every local branch `issue-<nr>-*`. The script finds them itself:" en daaronder het blok `python3 .lighthouse/scripts/worktree_audit.py --issue <nr>; echo "exit=$?"`. Behoud de alinea over `dangerouslyDisableSandbox` en "Run it without `cd`". Vervang "At A or B the PR column … goes on" door: "A branch without a merged PR gets `cleanup: no — no merged PR`."
  - **Verdict.** "Only exit `0` plus, in a branch's block, a line `cleanup: yes` can be cleaned up. Every other outcome is `no`: exit ≠ 0 (exit `2`: the PR lookup failed, clean up nothing at all), a block without a `cleanup:` line, a traceback, an empty output. The absence of a signal is no yes."
  - Een tabel per blok:

    | Block | What now |
    |---|---|
    | `cleanup: yes` | run the lines below it yourself, in that order, each as `git -C <main checkout> …`. No question: the verdict is the safety check |
    | `cleanup: no — <reason>` | report the line literally with its branch; clean up nothing for that branch |
    | everything else | report `cleanup: no — audit gave no verdict` with the raw output; clean up nothing |

  - Behoud de alinea over `<main checkout>` en de terugval zonder `-C`. Vervang "`--only` already checked" door "`--issue` already checked". Toevoegen: "The worktree that holds your own cwd is never cleaned up; its block says `own cwd is in …`."
  - **Evidence**, na het opruimen, één keer `git -C <main checkout> worktree list` en per opgeruimde branch `git -C <main checkout> branch --list <branch>`. De branch staat in geen van beide.
  - **Refusal.** "If git refuses a line (for example `contains modified or untracked files`), report the error line, skip the rest of that branch's lines, and go on with the next branch."
  - **Report.** Per branch één regel: `<branch>: cleanup: …`, en na `yes` daarachter `— removed` of `— git refused: <error line>`. Headless (B7) gaan diezelfde regels als bewijsregels in de close-comment. Er komt geen vraag, in geen enkele modus.
  - ✅/❌-lijst: "✅ only the lines `--issue` delivers, without a question" en de ❌-regel letterlijk zoals hij nu is.

- [ ] **Stap 5: Werk de worktree-audit-skill bij** (`pack/core/skills/worktree-audit/SKILL.md:10-21`). Voeg aan het commandoblok de regel `python3 .lighthouse/scripts/worktree_audit.py --issue <nr>` toe, na `--only`. De alinea wordt: "`--only` and `--issue` are the views for `lighthouse-close-issue`: one branch, or every branch of an issue, each with a line `cleanup: yes` or `cleanup: no — <reason>`. That skill runs the delivered lines of every `yes` itself, without a question; this skill and the script still change nothing." Geen issuenummer met drie of meer cijfers erin (test :87).

- [ ] **Stap 6: Draai de tests, verwacht PASS**

  Run: `uv run pytest -q tests/pack/test_close_issue_skill.py tests/pack/test_worktree_audit_skill.py`
  Expected: alles groen, ook `test_variants_differ_in_the_instruction_file`.

- [ ] **Stap 7: Dogfood, volledige gate, commit**

  ```bash
  go run ./cmd/lighthouse init --upgrade
  go run ./cmd/lighthouse init --upgrade   # verwacht: No changes.
  uv run pytest -q && go test ./...
  git add pack/core/skills/close-issue pack/core/skills/worktree-audit tests/pack/test_close_issue_skill.py tests/pack/test_worktree_audit_skill.py .claude/skills .agents/skills .lighthouse/
  git commit -m "feat(close-issue): ruim alle branches van het issue op, zonder vraag en ook headless"
  ```

### Taak 3: de paia-spiegel en de PR

**Files:** geen code. Dit gaat in de PR-body.

- [ ] **Stap 1: Meet de tagstand.** Run `git tag --list 'v*'` en `git -C /home/corneel/repositories/personal_ai_agent grep -n "lighthouse/cmd/lighthouse@" -- . ':!docs'`. Is `v0.1.0` nog niet gezet, dan neemt de tag deze merge mee en volgt de spiegel in de core-PR van M0 (d) (ontwerpkeuze 6). Is hij al gezet, dan is er een nieuwe tag nodig vóór die core-PR. Dat meld je in de PR-body; de tag zet Corneel.
- [ ] **Stap 2: Schrijf de PR** met `/lighthouse-verify-pr`. AC 7 krijgt als bewijs de regel uit stap 1 en de verwijzing naar het M0 (d)-plan. Stelt de verifier dat AC 7 pas na de paia-overstap meetbaar is, dan wordt de body `Refs #32 — <reden>`, en niet `Closes`.
