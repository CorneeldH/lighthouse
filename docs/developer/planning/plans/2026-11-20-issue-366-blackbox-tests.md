# Triage van de black-box-tests in tests/pack — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Elke black-box-test van een hook of geïnstalleerd script in `tests/pack/` (24 bestanden, 576
testfuncties) krijgt één bestemming — weg of naar Go — en `tests/pack/python/` draait groen met zijn
helpers op een nieuwe plek.

**Architecture:** De Go-implementaties van de hooks hebben al unit-tests in `internal/`, vaak op
dezelfde gedeelde fixtures (`tests/pack/fixtures/`). Per Python-testfunctie zoeken we de Go-tweeling;
bij twijfel beslist een mutatie van de productiecode. Wat niets dekt en iets bewijst, wordt een Go-test:
in-process naast de bestaande tests in `internal/<pakket>/` als het gedrag in Go-code zit, in `e2e/`
(build tag `e2e`) alleen als hij de registratie in `.claude/settings.json`, `run.sh` of de door `init`
geïnstalleerde vorm nodig heeft.

**Tech Stack:** Go (`testing`, `t.TempDir`, echte `git`), pytest (alleen nog `tests/pack/python/`).

**Spec:** [2026-11-16-repo-zonder-python-design.md](../specs/2026-11-16-repo-zonder-python-design.md), §1 en §3. Issue #366.

## Global Constraints

- Vier bestemmingen, uit spec §1: **weg — onderwerp weg**, **weg — gedekt** (noem de Go-test),
  **weg — beschermt niets**, **naar Go**. Per testfunctie, niet per bestand.
- Kernvraag (`lighthouse-test-standard`): *welke productiewijziging laat deze test falen?*
  Geparametriseerde gevallen tellen als één test als ze op dezelfde mutatie falen.
- Een test die je wilt porten maar niet kunt laten falen met een mutatie, port je niet (spec, *Aan de bouwer*).
- Twijfel tussen gedekt en naar Go: muteer de productiecode, draai de Go-test. Faalt hij, dan is het gedekt.
- Geen test die na deze PR zowel in pytest als in Go bestaat (anti-doel).
- Geen ander gedrag van een hook of script (No-Go): alleen `_test.go`, testhelpers en Python-tests wijzigen.
  Raakt een taak toch productiecode, stop en meld het.
- `tests/pack/fixtures/` blijft staan en blijft op zijn plek: Go-tests in `internal/<pkg>/` lezen het via `../../tests/pack/fixtures/`, die in `e2e/` via `../tests/pack/fixtures/`. Commentaren in `_test.go` die `tests/pack/_testkit.py` noemen, werk je in taak 1 bij; die in productiecode (`fixturesetup.go:3`, `scriptcase/forge.go:16`) blijven als open punt.
- `tests/pack/python/` en `pack/python/` blijven Python (No-Go).
- Go volgt `/lighthouse-go-standard`: literal `want`, `t.TempDir`, `t.Setenv`, classificatiezin in de chat vóór de eerste edit.
- Bestanden die bij #367 horen (de overige 18 `tests/pack/test_*.py`) en bij #368
  (`tests/pack/{go,shell,node,expo}/`, `fixtures/code_language/`) blijven inhoudelijk ongemoeid; alleen hun
  `_testkit`-import verandert (taak 1).
- Artefacten (PR-body, commitberichten, dit plan) in het Nederlands; Go-namen en -commentaar in het Engels.

## Review Focus

1. **Helper verhuist, anker verschuift.** `_testkit.py:34` zet `REPO = Path(__file__).resolve().parents[2]`;
   één map dieper wordt dat `parents[3]`. Een vergeten anker laat `install_core` stil een verkeerde boom
   installeren. Taak 1 bewijst het met een collect- en een volledige run van `tests/pack/python`.
2. **Importfouten buiten de scope.** Root-`tests/` (#365), #367- en #368-bestanden importeren `_testkit`.
   Een gemiste importer breekt pas in hun shard. Taak 1 draait `uv run pytest --collect-only -q tests` op de hele boom.
3. **Tweeling met dezelfde naam, andere assert.** Een Go-test met een gelijke naam kan alleen de exitcode
   checken waar de Python-test ook de melding pinde. Per *gedekt*-rij één mutatie die de Go-test rood maakt;
   anders is het geen *gedekt*.
4. **Registratie alleen in Python bewezen.** Testen als `test_hooks_registered_pre_post_and_failure`,
   `test_hook_registered_and_executable`, `test_hooks_json_registers_pre_and_post` pinnen `hooks.json`.
   `ci/no-python.sh` draait elke registratie, maar is geen `_test.go`. Taak 11 port dit naar `e2e/`.
5. **Omgeving lekt in Go-tests.** Een controller-run zet `LIGHTHOUSE_CONTROLLER_RUN`, `LIGHTHOUSE_REPO_ROOT`
   en `LIGHTHOUSE_TOML`. Elke geporte test die een proces start, gebruikt `cleanEnv()`/`hookEnv()` (e2e) of
   `t.Setenv` met lege waarden; anders is hij groen op de host en rood in een controller-run.

---

## Werkwijze per testfunctie (geldt voor taak 2–12)

Elke taak triëert zijn bestanden met dezelfde vier stappen, en schrijft per functie of groep één rij in
**Triagetabel** onderaan dit plan. Die tabel gaat ongewijzigd de PR-body in.

1. **Tweeling zoeken.** `grep -rn '<kernwoord>' internal/<pakket>/*_test.go e2e/` en de case-namen in de
   fixture die beide lezen (`jq -r '.[].name' tests/pack/fixtures/<map>/cases.json`).
2. **Mutatie bij twijfel.** Wijzig één regel in de productiecode die de Python-test beschermt, draai
   `go test -run <Naam> ./internal/<pakket>/`, zet de regel terug (`git checkout -- <bestand>`).
   Rood = *gedekt*, noem test en mutatie. Groen = verder met stap 3.
3. **Porten of niet.** Beantwoordt de Python-test de kernvraag? Ja: schrijf de Go-test (red met de
   mutatie, green zonder), noteer de mutatie. Nee: *weg — beschermt niets*, met de rode vlag uit
   `lighthouse-test-standard`.
4. **Verwijderen.** Als alle functies van een bestand een rij hebben: `git rm tests/pack/test_<naam>.py`.

Plaats van een geporte test:

| Wat de test nodig heeft | Plaats |
|---|---|
| Logica van een Go-pakket, eventueel met echte `git` in `t.TempDir()` | `internal/<pakket>/<naam>_test.go`, naast de tweelingen |
| De registratie in `.claude/settings.json`, `run.sh`, de geïnstalleerde boom van `init` | `e2e/<naam>_test.go`, `//go:build e2e`, helpers uit `e2e/e2e_test.go` (`buildLighthouse`, `freshRepo`, `bashHookCommand`, `eventHookCommand`, `runGate`, `runGateEvent`, `hookEnv`) |

---

### Task 1: Helpers naar `tests/pack/python/`

**Files:**
- Move: `tests/pack/_testkit.py` → `tests/pack/python/_testkit.py`
- Move: `tests/pack/_fakegh.py` → `tests/pack/python/_fakegh.py` (`_testkit.py:161` vindt hem via `Path(__file__).with_name`)
- Create: `tests/pack/python/_bash_session.py` met `heredoc`, `_Session`, `context`, `managed_script` uit `tests/pack/test_bash_write_check.py:175-432`
- Modify: elke importer van `tests.pack._testkit` (60 bestanden, lijst: `grep -rlE 'tests\.pack(\._testkit| import _testkit)' --include=*.py tests`), `tests/pack/conftest.py`, `tests/pack/python/test_ruff_format.py:181,196`, `tests/pack/test_bash_write_check.py`

**Interfaces:**
- Produces: module `tests.pack.python._testkit` (zelfde namen als nu), module `tests.pack.python._bash_session` (`heredoc`, `_Session`, `context`, `managed_script`). Taak 5 verwijdert `test_bash_write_check.py`; `_bash_session.py` blijft voor `test_ruff_format.py`.

- [ ] **Step 1: Nulmeting** — `uv run pytest -q tests/pack/python` en `uv run pytest --collect-only -q tests | tail -1`. Noteer beide aantallen.
- [ ] **Step 2: Verplaatsen** — `git mv` van beide bestanden; in `_testkit.py` `parents[2]` → `parents[3]`.
- [ ] **Step 3: Imports** — `sed -i 's/tests\.pack\._testkit/tests.pack.python._testkit/g'` over de importerlijst; `tests/pack/conftest.py` idem.
- [ ] **Step 4: `_bash_session.py`** — knip de vier namen (met wat ze zelf nodig hebben) uit `test_bash_write_check.py` naar het nieuwe bestand; beide gebruikers importeren eruit.
- [ ] **Step 5: Verifiëren** — `uv run pytest -q tests/pack/python`: zelfde aantal als step 1, groen. `uv run pytest --collect-only -q tests | tail -1`: zelfde aantal, geen `ERROR`.
- [ ] **Step 6: Stringpaden** — controleer `tests/pack/python/test_gate_python.py:23-125`: de paden daar zijn bomen die de test zelf in `tmp_path` bouwt (`tree`), geen verwijzing naar echte bestanden. Bevestig dat door de test te lezen; wijzig niets als dat klopt.
- [ ] **Step 7: Commit** — `test(pack): _testkit en _fakegh verhuizen naar tests/pack/python (#366)`.

Niet in deze taak: de opmerking in `.github/workflows/ci.yml:102` noemt `tests/pack/_testkit.py`. Die laat
deze PR staan; #370 herschrijft de jobs. Zo raakt deze PR geen workflow (geen `push-workflows` nodig).

### Task 2: Bestanden met directe Go-tweelingen

**Files:** verwijder na triage `tests/pack/test_{evidence,evidence_fixtures,script_cases,permission_check,usage_gate,usage_gate_controller,usage_gate_resume,classify,skill_scripts_gone,no_python_lib}.py`.

Voorlopige triage (bevestigen met de werkwijze; elke *gedekt*-rij één mutatie):

| Bestand | Tests | Voorlopig | Go-test |
|---|---:|---|---|
| `test_evidence.py` | 14 | weg — gedekt | `internal/evidence/run_test.go` (`TestRunBeforePRSkipsAfterPR`, `TestRunWithoutBeforePRRunsAfterPR`, `TestRunGHNotReachableIsExit4`, `TestRunNoTable`, `TestRunMix`, `TestRunDetailDoesNotBreakTheTable`, `TestRunOutcomesFollowLanguage`, `TestCheckCellReadsConfigOnlyForPytest`), CLI-bedrading `internal/cli/evidence_test.go` |
| `test_evidence_fixtures.py` | 1 (112 cases) | weg — gedekt | `internal/evidence/fixtures_test.go` `TestFixtures`, tak `check` |
| `test_script_cases.py` | 3 | weg — gedekt; `_self` weg — beschermt niets (test de Python-runner) | `TestPostArtefactCases`, `TestWorktreeAuditCases`, `TestUnblockCases`, `internal/scriptcase` `TestSelfCasesLoad` |
| `test_permission_check.py` | 12 | weg — gedekt | `internal/hooks/permissioncheck_test.go` (9 tests, o.a. `TestPermissionKinds` voor niet-uuid, worker, ander type) |
| `test_usage_gate*.py` | 33 | weg — gedekt; `no_paia_in_guard_files` weg — beschermt niets | `internal/usageguard/decide_test.go`, `resume_marker_test.go`, `internal/hooks/usagegate_test.go` |
| `test_classify.py` | 1 | weg — beschermt niets: test een testhelper; de productie-`classify` dekt `internal/bashwrites` `TestFixtures` (kind `classify`) | — |
| `test_skill_scripts_gone.py` | 2 | weg — beschermt niets: negative-existence na migratie | — |
| `test_no_python_lib.py` | 1 | weg — beschermt niets: negative-existence na #274; `ci/no-python.sh` grept de installatie op `python3` | — |

- [ ] **Step 1:** Per bestand de werkwijze; rijen in de Triagetabel. Een functie zonder tweeling die de kernvraag beantwoordt, port je naast de tweelingen (in-process).
- [ ] **Step 2:** `go test -race ./internal/evidence/ ./internal/usageguard/ ./internal/hooks/ ./internal/cli/ ./internal/scriptcase/` groen.
- [ ] **Step 3:** `git rm` van de tien bestanden; `uv run pytest --collect-only -q tests` zonder `ERROR`.
- [ ] **Step 4: Commit** — `test(pack): black-box-tests met Go-tweelingen weg (#366)`.

### Task 3: `main-commit-gate`

**Files:** `tests/pack/test_main_commit_gate.py` (64 tests) weg; ports naar `internal/hooks/maincommitgate_test.go` of `internal/gitinvocations/`.

Voorlopig: de parsergevallen (heredoc, `cd`, `-C`, subshell, `pushd`, ANSI-C-quoting, commentaar, wrappers,
`push --all`, refspecs) zijn *gedekt* als hun case in `tests/pack/fixtures/git_invocations/cases.json`
(59 `find`-cases) staat — `internal/gitinvocations/fixtures_test.go` `TestFixtures`. Gevallen met echte
git-toestand zijn kandidaat **naar Go** als `maincommitgate_test.go` (5 tests) ze niet heeft:
`sentinel_opens_the_gate`, `unresolvable_path_still_honours_the_sentinel`, `blocks_push_all_when_a_local_main_exists`,
`guard_follows_default_branch`, `push_all_looks_for_the_default_branch`, `with_broken_config_blocks_a_commit_but_not_a_plain_command`,
`reads_not_a_repo_under_other_locale`, `passes_a_commit_in_a_directory_this_command_creates`,
`blocks_a_commit_in_a_new_dir_inside_a_checkout`, `blocks_when_rev_parse_breaks`, `never_writes_to_stdout`,
`payload_shapes`, `reads_payload_keys_exactly`. `finds_its_lib_via_git_without_env` en
`fails_loud_without_tooling` gaan over `run.sh` → taak 10.
`e2e` `TestFreshRepoBlocksCommitOnMain` dekt registratie + `run.sh` + blok op `main`.

- [ ] **Step 1:** Classificatiezin: `Class: hook/gate. At risk: welke git-commando's op main geblokkeerd worden.`
- [ ] **Step 2:** Per parsergeval de case-naam in `cases.json` zoeken; ontbreekt hij, voeg de case toe aan `cases.json` (dat is de gedeelde plek, geen nieuwe test) en draai `go test -run TestFixtures ./internal/gitinvocations/`.
- [ ] **Step 3:** Per git-toestandgeval: test in `maincommitgate_test.go`, tabelgedreven, één `want` (exitcode + kernzin van stderr) per geval, repo via `git init` in `t.TempDir()`. Rood met de genoemde mutatie (bijv. sentinelcheck `.claude/allow-main` overslaan), groen zonder.
- [ ] **Step 4:** `go test -race ./internal/hooks/ ./internal/gitinvocations/` groen; `git rm tests/pack/test_main_commit_gate.py`.
- [ ] **Step 5: Commit** — `test(hooks): main-commit-gate-tests naar Go (#366)`.

### Task 4: `commit-test-gate`

**Files:** `tests/pack/test_commit_test_gate.py` (116), `tests/pack/test_commit_test_gate_no_verify.py` (16) weg.

Voorlopig *gedekt*: slots, geheugen, slice/OOM, deadline, meldingen, pre/post-markers —
`internal/commitgate/{gate,prepost,message,slot,memory,brake,base}_test.go` (81 tests) en
`internal/hooks/committestgate_test.go`; `--no-verify` via `fixtures/no_verify/cases.json` (32 cases) in
`internal/commitgate/fixtures_test.go` `TestFixtures/no_verify`. Registratie pre+post:
`e2e` `TestRedTestBlocksCommit` en `TestRedCommitComesBackAfter`.
Kandidaat **naar Go** (git-interplay zonder tweeling): `gate_tests_the_tree_after_cd`, `gate_tests_the_dash_c_target_not_cwd`,
`gate_skips_foreign_repo_visibly`, `gate_tests_forge_worktree_of_same_repo`, `gate_names_common_dirs_and_origins_when_skipping`,
`gate_loud_when_common_dir_fails_for_*`, `pre_records_instead_of_testing_on_feature_branch`,
`pre_tests_on_configured_default_branch`, `pre_detached_head_is_not_the_default_branch`, `post_two_commits_in_one_call`,
`post_tests_commit_when_call_failed`, `gate_handles_space_in_repo_path`.
Kandidaat *beschermt niets*: ~~`real_pytest_leaves_nothing_in_tmp`~~ is **naar Go**, niet *beschermt niets*: het pint dat de gate zijn eigen `GATE_TMP` ook na een deadline-kill opruimt (#171; `gate.go:409`, geen Go-twin). Commitgate-test met `TMPDIR`=`t.TempDir()`, één groene en één deadline-run, map leeg erna; mutatie: de `RemoveAll`-defer weghalen. Kandidaat *beschermt niets*:, `gate_loud_without_uv` als de gate `uv` niet zelf aanroept.

- [ ] **Step 1:** Classificatiezin; werkwijze per functie; tweelingen in `gate_test.go` hebben vaak een andere naam (bijv. `no_command_is_loud_and_allows` ↔ `TestNoCommandPassesWithANote`).
- [ ] **Step 2:** Ports in `internal/commitgate/prepost_test.go` (pre/post met echte git) of een nieuw `internal/commitgate/roots_test.go` voor de cd/`-C`/vreemde-repo-gevallen.
- [ ] **Step 3:** `go test -race ./internal/commitgate/ ./internal/hooks/` groen; beide Python-bestanden `git rm`.
- [ ] **Step 4: Commit** — `test(commitgate): commit-test-gate-tests naar Go (#366)`.

### Task 5: `bash-write-check`

**Files:** `tests/pack/test_bash_write_check.py` (68) weg (helpers zijn in taak 1 verhuisd).

Voorlopig *gedekt*: reflog, ledger, marker, pending, deadline, dispatch — `internal/bashwrites/{post,pre,state,fixtures}_test.go` (84 tests, `fixtures/bash_writes/cases.json`: `commits` 13, `derive_roots` 6, `reflog` 2, …).
De twee `linkcheck_*`-tests: `internal/hooks/mdlinkcheck_test.go` `TestLinkCheckCouldNotCheck`.
Kandidaat **naar Go**: `hooks_registered_pre_post_and_failure` → `e2e` (taak 11 doet alle registraties);
scenario's met meerdere worktrees of een gitignored geneste worktree als `bashwrites` ze niet heeft
(`cd_into_a_second_worktree`, `worktree_add_then_commit_inside`, `relative_path_into_a_gitignored_nested_worktree`,
`removed_worktree_is_named_not_silent`, `two_roots_with_the_same_rel_path_get_distinct_headers`).

- [ ] **Step 1:** Classificatiezin; werkwijze per functie. Een scenario dat echte gates nodig heeft (shellcheck, ruff) port je met een stubgate zoals `TestRunShGateRunsThroughRunSh` doet.
- [ ] **Step 2:** Ports in `internal/bashwrites/post_test.go` of een nieuw `internal/bashwrites/roots_test.go`.
- [ ] **Step 3:** `go test -race ./internal/bashwrites/` groen; `git rm tests/pack/test_bash_write_check.py`; `uv run pytest -q tests/pack/python/test_ruff_format.py` groen.
- [ ] **Step 4: Commit** — `test(bashwrites): bash-write-check-tests naar Go (#366)`.

### Task 6: `md-link-check`, `worktree-check`, `pr-create-gate`

**Files:** `tests/pack/test_{md_link_check,worktree_check,pr_create_gate}.py` (19 + 15 + 19) weg.

Voorlopig: md-link-check *gedekt* door `internal/mdlinks` `TestFixtures` (`fixtures/md_links/links.json`)
en `internal/hooks/mdlinkcheck_test.go` (7); kandidaat **naar Go** `checks_main_checkout_too`,
`checks_other_project_too`, `allows_link_to_branch_only_spec` als `TestLinkCheckResolvesThePath` ze niet raakt.
worktree-check: `internal/hooks/worktreecheck_test.go` (5) dekt de kern; kandidaat **naar Go**
`silent_for_worktree_branch_literally_named_main`, `warns_when_cwd_is_worktree_but_target_is_main_checkout`,
`guard_follows_default_branch`, `guard_with_broken_config_speaks`, `guard_silent_outside_git_repo_under_other_locale`.
pr-create-gate: `internal/prbody` (`fixtures/pr_body_gate/cases.json`, 153 cases), `internal/hooks/prcreategate_test.go` (11),
`internal/cli/prbodygate_test.go` (3); kandidaat **naar Go** `ac_count_differs_blocks`, `slow_forge_passes_loudly_within_the_deadline`,
`fork_clone_without_repo_blocks_with_the_exact_flag` (forge via `stubBin`/`prependPath` van `internal/prbody/gate_test.go:606`: de gate draait `gh` als subprocess, geen HTTP; check eerst of daar al een twin staat); `hook_registered_and_executable` → taak 11.

- [ ] **Step 1:** Werkwijze per functie, per bestand een commit.
- [ ] **Step 2:** `go test -race ./internal/hooks/ ./internal/mdlinks/ ./internal/prbody/ ./internal/cli/` groen; drie bestanden `git rm`.
- [ ] **Step 3: Commits** — `test(hooks): md-link-check-tests naar Go (#366)`, idem `worktree-check`, `pr-create-gate`.

### Task 7: `code-language-check`

**Files:** `tests/pack/test_code_language_hook.py` (20) weg; `tests/pack/_fake_llm.py` weg als niets anders het importeert (`grep -rln _fake_llm tests`).

Voorlopig: oordeel en scope *gedekt* door `internal/codelang/{check,score,hook,llm}_test.go`. Kandidaat
**naar Go**: `ends_within_deadline` (3 modi van de LLM: stil, druppel, onbereikbaar → `httptest`),
`foreign_chain_is_never_used`, `notice_without_findings`, `notice_with_findings`, `under_gates`,
`relative_path_from_cwd`, `other_repo_without_toml`.

- [ ] **Step 1:** Werkwijze; ports in `internal/codelang/hook_test.go`.
- [ ] **Step 2:** `go test -race ./internal/codelang/` groen; `git rm` (en `_fake_llm.py` als vrij).
- [ ] **Step 3: Commit** — `test(codelang): code-language-hook-tests naar Go (#366)`.

### Task 8: `lighthouse turn-roots`

**Files:** `tests/pack/test_turn_roots.py` (29) weg; ports in `internal/turnroots/turnroots_test.go`.

Voorlopig grotendeels **naar Go**: `internal/turnroots` heeft 5 tests (fnmatch, payloadfouten), geen
transcriptscenario's. Gedekt-kandidaten: `invalid_json_cannot_tell` (`TestPayloadErrors`), de `match_*`-patronen
die alleen `fnmatch` raken (`TestFnmatchCase`, `TestMatchesAnyCompiled`).

- [ ] **Step 1:** Classificatiezin: `Class: io/filesystem. At risk: welke checkouts een Stop-hook controleert.`
- [ ] **Step 2:** Eén tabelgedreven test over transcripten (helper die een JSONL in `t.TempDir()` schrijft met prompt-, tool- en resultregels, zoals `_testkit.write_transcript`), git-repos en worktrees echt. Per geval een literal `want` (lijst roots of de `cannot tell`-uitkomst).
- [ ] **Step 3:** Mutaties om te tonen: compactiesamenvatting als nieuwe prompt lezen; `cwd` van de payload i.p.v. de entry gebruiken; commit vóór de prompt meetellen.
- [ ] **Step 4:** `go test -race ./internal/turnroots/` groen; `git rm`.
- [ ] **Step 5: Commit** — `test(turnroots): turn-roots-tests naar Go (#366)`.

### Task 9: `lighthouse check-instructions`

**Files:** `tests/pack/test_check_instructions.py` (82) weg; ports in `internal/checkinstr/` (per check een `_test.go` naast `mirror.go`, `overlap.go`, `rules.go`, `size.go`, `skills.go`, `hooknames.go`, `pairing.go`).

Voorlopig grotendeels **naar Go**: `internal/checkinstr` heeft 9 tests, `internal/cli/checkinstructions_test.go` 1.
*Gedekt*-kandidaten: de mirrorgevallen in `fixtures/check_instructions/` (`TestMirrorFixtures`), `hook_name_*` (`hooknames_test.go`).
Kandidaat *beschermt niets*: varianten die op dezelfde mutatie falen (bijv. `claude_md_above_the_ceiling_fails` en
`claude_md_one_above_the_ceiling_fails` → één grensgeval-tabel). `this_script_is_green_on_this_repo` **naar Go**:
`Run` op de repo-root moet groen zijn (dogfood); mutatie: een bestaand skillpaar laten afwijken.

- [ ] **Step 1:** Classificatiezin: `Class: cli-output. At risk: welke instructieboom check-instructions rood of groen noemt.`
- [ ] **Step 2:** Per check (1, 2, 3, 4, 5, 7, 8, 10, 11, 14) één tabeltest met een boom in `t.TempDir()` en literal exitcode + bevindingregel. Een helper die een boom uit een `map[string]string` schrijft, één keer, in `internal/checkinstr/helpers_test.go`.
- [ ] **Step 3:** Per check één mutatie in de bron van die check; noteer haar.
- [ ] **Step 4:** `go test -race ./internal/checkinstr/ ./internal/cli/` groen; `git rm`.
- [ ] **Step 5: Commit(s)** — één per check-groep, `test(checkinstr): … naar Go (#366)`.

### Task 10: `run.sh`

**Files:** `tests/pack/test_run_sh.py` (35) weg; port naar `e2e/runsh_test.go` (`//go:build e2e`).

`pack/core/hooks/run.sh` is shell: geen Go-test raakt hem buiten e2e. Voorlopig **naar Go** alles behalve:
`shellcheck_clean` (**naar Go** als e2e-test die `shellcheck` op `run.sh` draait en overslaat als het niet bestaat; of, als dat te veel is, open punt van #367 — niet *gedekt* door een Python-bestand: de spec laat alleen een Go-test als gedekt toe) en varianten op dezelfde mutatie.
Ook `test_main_commit_gate.py::finds_its_lib_via_git_without_env` en `fails_loud_without_tooling` landen hier.

- [ ] **Step 1:** Classificatiezin: `Class: hook/gate. At risk: welke hook run.sh blokkeert als de binary ontbreekt, en of hij bouwt zonder te wachten.`
- [ ] **Step 2:** Helper `runShEnv(t)`: een `bin/`-map met symlinks naar `sh dirname grep sed awk cat mktemp rm head mkdir rmdir find touch date git` plus `setsid` of `nohup` (de lijst van wat `run.sh` aanroept: de tweede `RUN_SH_TOOLS` in `_testkit.py:676` plus `git`, `rmdir`, `setsid`/`nohup`; niet het 8-tool-tupel op `:553`) plus een fake `go` en/of `lighthouse` die hun argv en cwd loggen. Geen host-`go` of -`lighthouse` op PATH.
- [ ] **Step 3:** Tabeltests voor de groepen: ontbrekende binary (blokkerend vs. niet, `dev` vs. release), onbekende hook (JSON-melding, ook met CR), bouwen (één build bij parallelle aanroepen, stempel, stale lock, 60 s pauze na mislukte build door de mtime van `build.failed` met `os.Chtimes` op nu−30 s (geen build) en nu−90 s (wel build) te zetten; `run.sh:98-103` vergelijkt met `pause.ref`, een klokseam is niet nodig), `LIGHTHOUSE_REPO_ROOT`.
- [ ] **Step 4:** `go test -race -tags e2e -run RunSh ./e2e/ -v` groen; mutaties: blokkeerlijst-lookup omdraaien, de detach weghalen.
- [ ] **Step 5:** `git rm tests/pack/test_run_sh.py`. **Commit** — `test(e2e): run.sh-tests naar Go (#366)`.

### Task 11: Registraties, minimale config, `config`-fixtures

**Files:** `tests/pack/test_hooks_minimal_config.py` (3), `tests/pack/test_config_fixtures.py` (1) weg; create `e2e/registrations_test.go`; modify `internal/config/parity_test.go`.

- `test_hooks_with_minimal_config` (geparametriseerd over `hooks.json`) **naar Go** als `e2e` `TestEveryRegistrationRunsUnderMinimalConfig(t *testing.T)`: `init --yes` in een
  `freshRepo` met alleen `[project]` en `[forge]`, dan per registratie uit `.claude/settings.json` het
  commando op een featurebranch met een payload die de config-read écht raakt, per hook uit
  `tests/pack/test_hooks_minimal_config.py::_payload`: een commit op een featurebranch (`main-commit-gate`, `commit-test-gate`),
  `gh pr create --repo …` (`pr-create-gate`), een push met `LIGHTHOUSE_CONTROLLER_RUN` gezet (`workflow-push-gate`), een Edit in de
  main checkout (`worktree-check`). De `git status`-payloads uit `tests/pack/fixtures/hooks/` keren vóór de config-read terug en volstaan niet.
  Na `init --yes` wordt `lighthouse.toml` overschreven met alleen `[project]` en `[forge]` (init voegt anders de core-tabellen toe).
  Assert exit 0 én geen `COULD NOT CHECK` in stdout+stderr (`run.sh:57-63` laat een niet-blokkerende hook anders stil slagen). Mutatie: een hook die op een ontbrekende optionele sleutel
  exit 2 geeft.
- `test_every_registration_has_a_payload` **naar Go** in dezelfde test: een registratie zonder payloadbestand faalt.
- `test_guarded_covers_every_blocking_bash_hook`: werkwijze; vermoedelijk gedekt door de blokkeerlijst in `pack/core/hooks/blocking` en taak 10.
- De registratietests uit taak 4–6 (`hooks_registered_pre_post_and_failure`, `hook_registered_and_executable`, `hooks_json_registers_pre_and_post`) zijn **niet** gedekt door `TestEveryRegistrationRunsUnderMinimalConfig` (die pint geen event, matcher of timeout). **Naar Go** als één tabeltest `TestHookRegistrations` in `e2e/registrations_test.go`: per hook event, matcher, commando en timeout uit `pack/core/hooks/hooks.json` / het geïnstalleerde `settings.json` (`commit-test-gate` pre 300; `pr-create-gate` één Bash-blok, 15, uitvoerbaar; `bash-write-check` Pre 5 / Post 120 / PostToolUseFailure 120). Mutatie: een registratie droppen of een timeout verlagen. De bestaande `eventHookCommand` leest alleen matcher `Bash` en faalt bij ≠1 treffer: schrijf een nieuwe `settings.json`-walker in `registrations_test.go`; fixturepad vanuit `e2e/` is `../tests/pack/fixtures/`.
- `test_fixtures_cover_every_key` **naar Go**: in `internal/config/parity_test.go` een test die alle sleutels van het schema (`SCHEMA`, `DEFAULTS`, `BASE_KEYS`, `HOST_KEYS`, prefixen) vergelijkt met de eerste `args` van alle `expect.json`. Mutatie: een sleutel aan het schema toevoegen zonder fixture.

- [ ] **Step 1–3:** Schrijf beide tests, toon rood met de mutatie, groen zonder.
- [ ] **Step 4:** `go test -race -tags e2e -run Registration ./e2e/ -v` en `go test -race ./internal/config/` groen; beide Python-bestanden `git rm`.
- [ ] **Step 5: Commit** — `test(e2e): elke registratie onder minimale config (#366)`.

### Task 12: `lighthouse review-run`

**Files:** `tests/pack/test_adversarial_review_sh.py` (22) weg; ports in `internal/review/`.

Voorlopig *gedekt*: `spec_review_prompt_is_byte_identical_without_new_flags` (`TestBuildPromptByteIdentical`),
`context_on_a_template_without_the_placeholder_fails` (`TestBuildPromptContextNeedsPlaceholder`),
`budget_fallback_stops_before_codex` (`TestBudgetFallbackStopsBeforeCodex`),
`broken_budget_check_is_not_measurable_and_codex_runs` (`TestBudgetPanicIsNotMeasurable`),
`missing_context_path_is_a_usage_error` (`TestMissingDocumentIsAUsageErrorAfterTheCodexCheck`, bevestig met mutatie).
De tests die een schema of prompttekst in de skillmap pinnen (`plan_schema_requires_one_of_seven_categories`,
`plan_prompt_asks_executability_not_improvement`, `plan_verification_schema_is_scoped`,
`spec_blocker_needs_a_failure_scenario`) zijn contracten op skilltekst: **naar Go** in `internal/review`
(dat de templates al leest), niet in het contractpakket van #367, zodat beide PR's onafhankelijk mergen.
`paired_round_one_without_python3`: werkwijze; *beschermt niets* als het alleen bewijst dat Python ontbreekt.

- [ ] **Step 1:** Werkwijze per functie; fake `codex` op PATH in `t.TempDir()` zoals `TestRoundRunsCodexAndSummarises`.
- [ ] **Step 2:** `go test -race ./internal/review/` groen; `git rm`.
- [ ] **Step 3: Commit** — `test(review): review-run-tests naar Go (#366)`.

### Task 13: Afronden

**Files:** create `changelog.d/issue-366-blackbox-tests.md`.

- [ ] **Step 1: Fragment**

```markdown
---
type: internal
---
De black-box-tests van hooks en scripts in `tests/pack/` zijn getrieerd: wat Go nog niet dekte, is een Go-test (`internal/` of `e2e/`), de rest is weg. `_testkit.py` staat in `tests/pack/python/`.
```

- [ ] **Step 2: Volledigheid** — `ls tests/pack/test_*.py` bevat geen van de 24 bestanden meer; de Triagetabel heeft voor elke testfunctie (of groep) een rij; tel: tests voor, geport, weg (drie soorten).
- [ ] **Step 3: KPI's** (rapporteer per rij met GOOS, tags, `-race`, `GOWORK`):
  - `git ls-files -z '*.go' | xargs -0r gofmt -l` → leeg
  - `go vet ./... && go vet -tags e2e ./e2e/` → rc 0
  - `golangci-lint run ./...` → rc 0
  - `go test -race -shuffle=on ./...` → rc 0
  - `go test -race -shuffle=on -tags e2e ./e2e/` → rc 0
  - `uv run pytest -q tests/pack/python` → groen
  - `uv run pytest --collect-only -q tests` → geen `ERROR`
- [ ] **Step 4: Commit** — `docs(changelog): fragment voor #366`.
- [ ] **Step 5: macOS** — na de Step 4-commit `git push -u origin HEAD`, dan `gh workflow run macos.yml --ref issue-366-blackbox-tests` (workflow_dispatch draait de ref op origin, niet de lokale HEAD; de e2e-suite met `setsid`/`nohup` en symlink-PATH draait alleen daar op macOS en `release.yml` eist het). Noteer het run-id en controleer met `gh run view <id> --json headSha` dat het gelijk is aan `git rev-parse HEAD`. Niet op het resultaat wachten; meld het run-id als open punt in de PR.

---

## Triagetabel

Vul aan per taak. Gaat ongewijzigd naar de PR-body.

| Bestand | Test(s) | Bestemming | Go-test / mutatie | Reden |
|---|---|---|---|---|
| `test_evidence.py` | `test_cli_run_before_pr_skips_after_pr`, `..._without_before_pr_runs_after_pr`, `..._without_before_pr_holds` | weg — gedekt | `TestRunBeforePRSkipsAfterPR`, `TestRunWithoutBeforePRRunsAfterPR`, `TestRunWithoutBeforePRHolds` (`internal/evidence/run_test.go`); mutatie `if beforePR {` → `if false {` / `if true {` en `ghOK = ghReachable()` → `ghOK = false` in `run.go`: rood | Zelfde uitvoer, exitcode en gh-marker, nu met de hele tabel als literal |
| `test_evidence.py` | `test_cli_run_before_pr_after_pr_outside_allowlist_is_invalid`, `..._empty_after_pr_result_is_invalid` | weg — gedekt | `TestRunBeforePRAfterPROutsideAllowlistIsInvalid`, `TestRunBeforePREmptyAfterPRResultIsInvalid`; mutatie outcome `Invalid` → `Holds` in `run.go`: beide rood | |
| `test_evidence.py` | `test_cli_run_gh_not_reachable_is_exit_4`, `test_cli_run_no_table`, `test_cli_run_mix`, `test_cli_run_detail_does_not_break_the_table` | weg — gedekt | `TestRunGHNotReachableIsExit4`, `TestRunNoTable`, `TestRunMix`, `TestRunDetailDoesNotBreakTheTable`; mutaties `return 4` → `return 1`, `return 2` → `return 1`, `seen[Fails] \|\| seen[Invalid]` → `seen[Fails]`, `\|`-escape weg in `cell()`: elk rood | |
| `test_evidence.py` | `test_cli_check`, `test_run_accepts_a_refs_body` | weg — gedekt | `TestEvidenceCheckCommand`, `TestEvidenceRunCommand` (Refs-body, `internal/cli/evidence_test.go`); mutaties `evidence.CheckCell(..)` → `0` en `beforePR` → `false` in `cli/evidence.go`: rood | `run` leest `Closes`/`Refs` niet, dus de Refs-body bewijst alleen dat `run` hem aanneemt; die dekking zit in de CLI-test |
| `test_evidence.py` | `test_pytest_path_from_config`, `test_pytest_path_default_is_tests` | weg — gedekt | `TestCheckCell` (`evidence_test_dir = "t/"`), `TestCheckCellReadsConfigOnlyForPytest`; de default `tests/` staat vast in `internal/config` `TestParityWithConfigPy/defaults`; mutaties `"test_gate.evidence_test_dir"` → `…dirx` in `run.go` en default `"tests/"` → `"src/"` in `config.go`: rood | De default is niet in één Go-test met de allowlist verbonden; de twee helften zijn elk gepind |
| `test_evidence.py` | `test_outcomes_follow_language` | weg — gedekt | `TestRunOutcomesFollowLanguage`; mutatie `c.Language` → altijd `en` in `run.go`: rood | |
| `test_evidence_fixtures.py` | `test_fixture_check_case` (112 gevallen) | weg — gedekt | `TestFixtures` (tak `check`, `internal/evidence/fixtures_test.go`); mutatie `-l` uit `WC_FLAGS` in `allowlist.go`: rood | Python liep via de CLI-bedrading; die dekt `TestEvidenceCheckCommand` |
| `test_script_cases.py` | `test_script_case` | weg — gedekt | `TestPostArtefactCases`, `TestWorktreeAuditCases`, `TestUnblockCases` (`internal/cli`); mutaties `tableRule` in `worktreeaudit/render.go`, `return 1` → `return 0` in `unblock.go`, prefix `artefact:` in `postartefact.go`: elk rood | Dezelfde `cases.json`; Go draait het commando in-process met echte `git` |
| `test_script_cases.py` | `test_fake_gh_rejects_a_reversed_order`, `test_forge_replay_rejects_a_wrong_order_and_a_wrong_body` | weg — beschermt niets | — (Go-tweeling voor de Go-replay: `TestReplayFailsOnWrongOrderExtraAndMissingCalls`, `TestReplayForgeFailsOnWrongBodyExtraAndMissingCalls`) | Testen van de Python-testhelpers `_fakegh`/`ForgeReplay`, geen geleverd gedrag; met `test_script_case` weg is de `_self`-fixture alleen nog voor `TestSelfCasesLoad` |
| `test_permission_check.py` | alle 12 | weg — gedekt | `TestPermissionOutsideARunWritesNothing`, `TestPermissionWritesToolAndPath`, `TestPermissionNewestToolUse`, `TestPermissionBashPathIsFirst80Chars`, `TestPermissionKinds`, `TestPermissionFallsBackToMessage`, `TestPermissionSecondPromptOverwrites`, `TestPermissionCouldNotCheck`, `TestPermissionAtHasMilliseconds` (`internal/hooks/permissioncheck_test.go`); mutaties in `permissioncheck.go`: run-id-check, `path:` + `"x"`, chmod `0o644`, `commandChars` 81, `!at.Before` → `false`/`true`, worker-soort weg, `idle_prompt` erbij, fallback `m != nil` → `false`, `os.Rename` → `nil`, COULD-NOT-CHECK-tekst, `.000Z` → `Z`: elk rood | Registratie in `settings.json` blijft taak 11 |
| `test_usage_gate.py` | `test_alarm_cases`, `test_refresh_is_detached`, `test_refresh_only_at_prompt`, `test_usage_refresh_env_silences`, `test_once_per_window`, `test_no_table_no_marker`, `test_unknown_stand` | weg — gedekt | `TestReadAlarmFixtures` (`internal/usage`), `TestUsageGateRefreshIsDetached`, `TestUsageGateRefreshOnlyAtPrompt`, `TestUsageGateSilent`, `TestUsageGateSpeaksOncePerWindow`, `TestStop`, `TestUsageGateStopWritesItsKey`; mutaties `len(f) != 4` → `< 4` in `alarm.go`, `cmd.Start()` → `cmd.Run()`, `stand == "prompt"` → `true`, env-waarde `"1"` → `"2"`, `gateStands` uit, `claim` geeft `true` bij bestaande sleutel, `c.StopHookActive \|\|` en stop-sleutelcheck weg, `case options().Resume` → `case true`: elk rood | |
| `test_usage_gate.py` | `test_unreadable_pause_flag_is_silent` | naar Go | `TestReadPauseUnreadableIsInactive` (`internal/park/pause_test.go`); mutatie `return time.Time{}, false` → `return time.Time{}, true` na de `ReadFile`-fout in `ReadPause`: rood; groen zonder | Een mode-000-vlag met toekomstige datum is geen pauze. Een mutatie die de foutcheck weglaat is gelijkwaardig (lege regel faalt de regex), maar fail-closed is dat niet; geen Go-test dekte het |
| `test_usage_gate.py` | `test_alarm_fields_are_not_globbed` | weg — onderwerp weg | — | Het bugpad zat in de shell-`alarm.sh`; Go leest het alarm zonder shell. De gevallen `valid question mark local` en `valid glob character in local` in `usage_alarm/cases.json` staan in `TestReadAlarmFixtures` |
| `test_usage_gate.py` | `test_no_paia_in_guard_files` | weg — beschermt niets | — | `git grep` op een woord in drie paden: bewaakt een naam, geen gedrag |
| `test_usage_gate_controller.py` | alle 16 (`test_alarm_parks_with_controller_reason`, `..._without_session_id_run_id_is_session`, `..._three_runs_each_once`, `..._option_off_is_plain_session`, `..._invalid_run_id_is_plain_session`, `..._prompt_warning`, `..._subagent_ignores_controller`, `..._pause_*`, `..._alarm_then_earlier_pause...`, `..._unreadable_marker_is_overwritten`) | weg — gedekt | `TestAlarmParksWithControllerReason`, `TestWithoutSessionIDRunIDIsSession`, `TestThreeRunsEachOnce`, `TestOptionOffIsPlainSession`, `TestInvalidRunIDIsPlainSession`, `TestPromptWarning`, `TestSubagentIgnoresController`, `TestPauseParksOnce`, `TestPauseWithAgentIDDoesNotPark`, `TestPauseOptionOffFallsThrough`, `TestPauseOnlyInTool`, `TestPauseThenAlarmKeepsTheLaterPauseEnd`, `TestAlarmThenEarlierPauseKeepsTheAlarmEnd`, `TestUnreadableMarkerIsOverwritten` (`internal/usageguard/decide_test.go`), `TestUsageGatePause` (`internal/hooks`), `TestReadPauseInvalid` (`internal/park`); mutaties in `decide.go`, `speak.go`, `context.go`: `Controller = false`, controller-tak achter resume, `sessionID = "x"`, run-id uit de sleutel, `options().Controller` weg, ongeldig run-id behouden, prompt-tak weg, subagent-tak weg, `pauseDue` altijd `true`/`false`, `AgentID == ""` weg, `Stand == "tool"` weg, `oldAt.After(newAt)` → `&& false`, `os.Rename` → `os.Link`, `Lstat` → `Stat` en tekenset-regex weg in `pause.go`: elk rood | Ongeldige vlag (verlopen, rommel, naief, symlink) met en zonder alarm: `TestReadPauseInvalid` en de rijen van `TestUsageGatePause`. De wake-richting `|| true` bij `oldAt.After(newAt)` faalt geen Go-test, en de Python-test pinde die ook niet |
| `test_usage_gate_resume.py` | `test_tool_window_writes_marker`, `test_week_has_no_wake`, `test_stop_notes_uncommitted`, `test_prompt_names_steps_without_marker`, `test_no_session_no_marker`, `test_subagent_ignores_resume` | weg — gedekt | `TestResumeMarkerFields`, `TestResumeWritesNoMarker`, `TestWakeCronFixtures`, `TestSpeechFixtures`, `TestSubagentNeverParks`, `TestKeysFixtures`; mutaties `wakeDelay` 3 min, `recurring true`, `HandoffPath = ""`, `Kind == "venster"` → `true`, stop-tekst `uncommitted` gebroken, marker ook bij prompt/zonder sessie, `no-session-` → `nosession-`, subagent achter resume: elk rood | |
| `test_usage_gate_resume.py` | `test_config_error_falls_back_to_base` | naar Go | `TestUsageGateConfigErrorFallsBackToBase` (`internal/hooks/usagegate_test.go`); mutaties `ReadOptions(warn)` → `ReadOptions(func(string) {})` in `usagegate.go` en `return Options{}` → `Options{Resume: true}` in `context.go`: rood; groen zonder | `TestOptionsConfigError` pint alleen `ReadOptions`; dat de hook de waarschuwing als één `usage-gate: config:`-regel op stderr zet stond in geen Go-test |
| `test_classify.py` | `test_classify_matches_the_shared_fixtures` | weg — beschermt niets | — (productie-`classify`: `internal/bashwrites` `TestFixtures`, soort `classify`) | Test een Python-testhelper (`_testkit.classify`), geen geleverd gedrag |
| `test_skill_scripts_gone.py` | `test_removed_unit_stays_gone`, `test_removed_skill_file_stays_gone` | weg — beschermt niets | — | Negative-existence na de migratie; `ci/no-python.sh` grept de installatie op `python3` |
| `test_no_python_lib.py` | `test_no_installed_skill_calls_config_or_headings_py` | weg — beschermt niets | — | Negative-existence na #274: `config.py`/`headings.py` bestaan niet meer; `ci/no-python.sh` grept de installatie op `python3`. Een skilltekst die de bestandsnaam noemt valt hierna niet meer op |

Task 2: 67 tests → naar Go 2 / gedekt 57 / beschermt niets 7 / onderwerp weg 1

**Totalen:** tests voor: 576 · naar Go: … · weg — gedekt: … · weg — beschermt niets: … · weg — onderwerp weg: …
