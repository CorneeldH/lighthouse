"""Unit tests for `.lighthouse/scripts/worktree_audit.py`, taken from paia.

De ladder beslist of iets "safe to remove" heet. Test 2 is niet bijvangst: een
opruimscript zonder bewaakte protect-lijst is gevaarlijker dan handwerk. De 78
lokale `entire/*`-checkpointrefs en `worktrees/.port-registry` hebben geen PR en
zijn niet in `main` gemerged — elke regel van de vorm "weg als geen merged PR"
wist ze.

Alles draait op gefakete collector-output: geen netwerk, geen `gh`, geen echte
repo.
"""

from __future__ import annotations

import dataclasses
import importlib.util
import json
import re
import sys
from pathlib import Path

import pytest

from tests.pack._testkit import INSTALL_ROOT

SCRIPT = INSTALL_ROOT / ".lighthouse" / "scripts" / "worktree_audit.py"
_SPEC = importlib.util.spec_from_file_location("worktree_audit", SCRIPT)
assert _SPEC and _SPEC.loader
audit = importlib.util.module_from_spec(_SPEC)
# Registreren VOOR exec_module. `from __future__ import annotations` maakt elke
# annotatie een string, en @dataclass resolvet die via
# `sys.modules.get(cls.__module__).__dict__`. Staat de module daar niet, dan is
# dat None en crasht de klassedefinitie op AttributeError — bij collectie al.
# tests/unit/test_deploy_drift_sweep.py komt hier niet mee in aanraking: die
# laadt een NamedTuple, geen dataclass.
sys.modules[_SPEC.name] = audit
_SPEC.loader.exec_module(audit)


@pytest.fixture(autouse=True)
def _paia_protection(tmp_path, monkeypatch):
    """The paia tests assume its protect list; in lighthouse that comes from
    `[worktree_audit]`. A config without it is in `test_protected_from_config`."""
    monkeypatch.setattr(audit, "DEFAULT_BRANCH", "main")
    monkeypatch.setattr(audit, "PROTECTED_REFS", ("main", "entire/*"))
    monkeypatch.setattr(audit, "PROTECTED_PATHS", (".port-registry",))
    config = tmp_path / "lighthouse.toml"
    config.write_text(
        '[worktree_audit]\nprotected_refs = ["entire/*"]\nprotected_paths = [".port-registry"]\n',
        encoding="utf-8",
    )
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(config))


def row(name: str, **kwargs: object) -> object:
    """Een Row met veilige defaults; elke test zet alleen wat hij bedoelt."""
    defaults: dict[str, object] = {
        "local": True,
        "remote": True,
        "dirty": False,
        "unpushed": 0,
        "unique_files": (),
        "pr_lookup_ok": True,
    }
    defaults.update(kwargs)
    return audit.Row(name=name, **defaults)


def never_alive(pid: int) -> bool:
    return False


def always_alive(pid: int) -> bool:
    return True


class TestSquashMergedBranchIsSafe:
    """Test 1 — AC 3: merge-detectie via PR-headbranch, niet via --merged.

    De REST-kant (merged_at, niet state) zit in TestParsePulls in taak 2:
    parse_pulls bestaat hier nog niet."""

    def test_merged_pr_is_safe_even_though_commits_are_not_in_main(self):
        # Squash-merge: de originele commits zitten niet in main's historie, dus
        # `git branch --merged` zou hier "niet gemerged" zeggen. De Row draagt
        # bewust geen merged-into-main veld.
        verdict = audit.judge(
            row("worktree-issue-713-osor-artefacten", pr_number=721, pr_state="merged"),
            alive=never_alive,
        )
        assert verdict.rank == 6
        assert verdict.safe is True

    def test_row_has_no_merged_into_main_field(self):
        # De ladder mag nooit op een "zit het in main"-veld kunnen leunen.
        names = {f.name for f in dataclasses.fields(audit.Row)}
        assert "merged_into_main" not in names
        assert {"pr_state", "pr_number"} <= names


class TestProtectListBites:
    """Test 2 — de protect-lijst. Rood maken door entire/* uit PROTECTED_REFS te halen."""

    def test_entire_checkpoint_ref_is_never_safe(self):
        verdict = audit.judge(
            row("entire/checkpoints/v1", pr_number=1, pr_state="merged"),
            alive=never_alive,
        )
        assert verdict.rank == 1
        assert verdict.safe is False

    def test_entire_session_ref_without_pr_is_never_safe(self):
        # Zonder de protect-lijst zou dit rang 8 zijn: geen PR, niets uniek.
        verdict = audit.judge(
            row(
                "entire/9b8f294-400008", pr_number=None, pr_state=None, unique_files=()
            ),
            alive=never_alive,
        )
        assert verdict.rank == 1
        assert verdict.safe is False

    def test_main_is_never_safe(self):
        assert audit.judge(row("main"), alive=never_alive).rank == 1

    def test_full_ref_form_is_protected_too(self):
        # `%(refname:short)` levert vandaag "entire/…", maar een latere parser
        # die ooit de volle ref doorgeeft mag de guard niet stilletjes missen.
        verdict = audit.judge(
            row(
                "refs/heads/entire/checkpoints/v1",
                pr_number=None,
                pr_state=None,
            ),
            alive=never_alive,
        )
        assert verdict.rank == 1
        assert verdict.safe is False

    def test_dir_holding_the_port_registry_is_protected_not_orphan(self):
        verdict = audit.judge(
            audit.Row(
                name="worktrees",
                local=False,
                remote=False,
                orphan_dir="/repo/.claude/worktrees/worktrees",
                dir_entries=(".port-registry",),
            ),
            alive=never_alive,
        )
        assert verdict.rank == 1
        assert verdict.safe is False

    def test_plain_orphan_dir_is_manual_never_safe(self):
        verdict = audit.judge(
            audit.Row(
                name="stale-devcontainer-leftover",
                local=False,
                remote=False,
                orphan_dir="/repo/.claude/worktrees/stale-devcontainer-leftover",
                dir_entries=("README.md",),
            ),
            alive=never_alive,
        )
        assert verdict.rank == 9
        assert verdict.safe is False


class TestDirtyAndUnpushedNeverSafe:
    """Test 3 — AC 4: vuile tree of ongepushte commits krijgt nooit "safe to remove"."""

    def test_dirty_tree_beats_merged_pr(self):
        verdict = audit.judge(
            row(
                "fix-iets",
                worktree="/repo/.claude/worktrees/fix-iets",
                dirty=True,
                pr_number=700,
                pr_state="merged",
            ),
            alive=never_alive,
        )
        assert verdict.rank == 3
        assert verdict.safe is False

    def test_unpushed_commits_beat_merged_pr(self):
        # Het #544-geval: gemerged én toch werk dat nergens anders bestaat.
        verdict = audit.judge(
            row("feat-544", unpushed=2, pr_number=544, pr_state="merged"),
            alive=never_alive,
        )
        assert verdict.rank == 4
        assert verdict.safe is False
        assert "2" in verdict.text

    def test_unknown_dirty_state_on_a_worktree_blocks(self):
        verdict = audit.judge(
            row(
                "fix-iets",
                worktree="/repo/.claude/worktrees/fix-iets",
                dirty=None,
                pr_number=700,
                pr_state="merged",
            ),
            alive=never_alive,
        )
        assert verdict.rank == 3
        assert verdict.safe is False

    def test_unknown_unpushed_count_on_a_local_branch_blocks(self):
        verdict = audit.judge(
            row("feat-544", unpushed=None, pr_number=544, pr_state="merged"),
            alive=never_alive,
        )
        assert verdict.rank == 4
        assert verdict.safe is False


class TestLockHandling:
    """Test 4 — AC 5: gelockte worktrees worden gerapporteerd, nooit ontgrendeld."""

    def test_live_lock_pid_blocks(self):
        verdict = audit.judge(
            row(
                "issue-624",
                worktree="/repo/.claude/worktrees/issue-624",
                locked=True,
                lock_pid=472155,
                pr_number=1,
                pr_state="merged",
            ),
            alive=always_alive,
        )
        assert verdict.rank == 2
        assert "472155" in verdict.text

    def test_dead_lock_pid_falls_through_with_modifier(self):
        verdict = audit.judge(
            row(
                "issue-624",
                worktree="/repo/.claude/worktrees/issue-624",
                locked=True,
                lock_pid=472155,
                pr_number=1,
                pr_state="merged",
            ),
            alive=never_alive,
        )
        assert verdict.rank == 6
        assert verdict.modifiers == ("stale lock",)

    def test_lock_without_a_pid_blocks(self):
        verdict = audit.judge(
            row(
                "issue-624",
                worktree="/repo/.claude/worktrees/issue-624",
                locked=True,
                lock_pid=None,
                pr_number=1,
                pr_state="merged",
            ),
            alive=never_alive,
        )
        assert verdict.rank == 2

    @pytest.mark.parametrize(
        "argv",
        [
            ["worktree", "unlock", "/repo"],
            ["branch", "-D", "x"],
            ["push", "origin", "--delete", "x"],
            ["branch", "nieuwe-branch"],
            ["branch", "-f", "main", "deadbeef"],
            ["branch", "-u", "origin/main"],
            ["worktree", "prune"],
            ["worktree"],
            ["branch", "--merged"],
        ],
    )
    def test_assert_readonly_refuses_a_mutating_verb(self, argv):
        # Ontgrendelen, verwijderen en pushen doet de skill nooit. De weigering
        # zit in het uitvoerpad zelf, niet in een afspraak. Elke argv is een
        # eigen parameter zodat een falende regel de boosdoener bij naam noemt.
        # `--merged` hoort hier ook bij: squash-merge maakt `git branch --merged`
        # een leugen, dus de enige betrouwbare toets is de PR-headbranch — die
        # vlag mag de allowlist niet doorlaten, ook al is hij lezend.
        with pytest.raises(ValueError, match="read-only"):
            audit._assert_readonly(argv)

    @pytest.mark.parametrize(
        "argv",
        [
            ["worktree", "list", "--porcelain"],
            ["branch", "--format=%(refname:short)"],
            ["branch", "-r", "--format=%(refname:short)"],
            ["status", "--porcelain"],
            ["rev-list", "--count", "feat-x", "--not", "--remotes"],
            ["diff", "--name-only", "main...feat-x"],
            ["cat-file", "-e", "main:pad/naar/bestand.py"],
            ["remote", "get-url", "origin"],
        ],
    )
    def test_assert_readonly_accepts_the_real_read_only_calls(self, argv):
        # De allowlist mag de eigen productiecode niet blokkeren. Alle acht
        # argv's die latere taken daadwerkelijk gebruiken, in één parametrize —
        # geen echte shell-out nodig om dat te bewijzen: `_assert_readonly`
        # raakt `subprocess` nooit.
        try:
            audit._assert_readonly(argv)
        except ValueError:
            pytest.fail(f"_assert_readonly weigerde een legitiem argv: {argv}")


# Vorm van een door `claude remote-control --spawn worktree` gemaakte worktree, gemeten
# in spike R1 (deploy/beelink/remote-surfaces-verification.md, sectie R1).
RC_BRANCH = "worktree-bridge-cse_013SdshNAm9nZXnDCpDXZdRg"
RC_PATH = (
    "/home/corneel/repositories/personal_ai_agent/.claude/worktrees/"
    "bridge-cse_013SdshNAm9nZXnDCpDXZdRg"
)
# De lock-reden die de sessie-server zet (R1-lock). De pid erin is van de SERVER, niet
# van de sessie — `_lock_state` mag die dus nooit als sessiesignaal lezen.
RC_LOCK_REASON = "claude agent bridge-cse_013SdshNAm9nZXnDCpDXZdRg"


class TestSessionServerWorktree:
    """#963: worktrees van de sessie-server krijgen nooit een PR.

    Levend zolang een sessie ze als cwd heeft; daarna opruimbaar zonder eigen commits,
    nakijken met eigen commits. Hun lock (`claude agent bridge-…`) draagt de pid van de
    server, niet van de sessie (spike R1) — die pid bewijst dus niets over of er nu
    iemand werkt.
    """

    def _rc_row(self, **kwargs: object) -> object:
        defaults: dict[str, object] = {
            "remote": False,
            "worktree": RC_PATH,
            "pr_number": None,
            "pr_state": None,
        }
        defaults.update(kwargs)
        return row(RC_BRANCH, **defaults)

    def test_live_session_in_a_clean_worktree_without_pr_blocks(self):
        verdict = audit.judge(self._rc_row(live=True), alive=never_alive)
        assert verdict.rank == 2
        assert verdict.text == "running session"
        assert verdict.safe is False

    def test_live_session_beats_a_merged_pr(self):
        verdict = audit.judge(
            self._rc_row(live=True, pr_number=1, pr_state="merged"), alive=never_alive
        )
        assert verdict.rank == 2

    def test_a_non_bridge_lock_still_wins_with_its_own_text(self):
        # De server-lock-uitzondering geldt alleen voor "claude agent bridge-…"; een
        # gewone sessielock (bv. van `claude remote-control --spawn session`) blijft
        # gewoon winnen met zijn eigen pid in de tekst.
        verdict = audit.judge(
            self._rc_row(
                live=True,
                locked=True,
                lock_pid=4242,
                lock_reason="claude session issue-1-x",
            ),
            alive=always_alive,
        )
        assert verdict.rank == 2
        assert "pid 4242" in verdict.text

    def test_bridge_lock_with_a_live_server_pid_and_ended_session_is_safe(self):
        # Gemeten in R1: de server-pid blijft weken levend na het archiveren van de
        # sessie. Zonder de server-lock-uitzondering zou dit rang 2 blijven, voor
        # altijd — de worktree zou nooit opruimbaar heten.
        verdict = audit.judge(
            self._rc_row(
                live=False, locked=True, lock_pid=272877, lock_reason=RC_LOCK_REASON
            ),
            alive=always_alive,
        )
        assert verdict.rank == 8
        assert verdict.safe is True
        assert "server-lock" in verdict.modifiers

    def test_bridge_lock_does_not_mask_a_live_session(self):
        verdict = audit.judge(
            self._rc_row(
                live=True, locked=True, lock_pid=272877, lock_reason=RC_LOCK_REASON
            ),
            alive=always_alive,
        )
        assert verdict.rank == 2
        assert verdict.text == "running session"
        # Reviewer minor: de server-lock-aantekening moet zichtbaar blijven, ook
        # als de live-sessie-trede het verdict overneemt van `_lock_state`.
        assert "server-lock" in verdict.modifiers

    def test_cleanup_keeps_a_server_locked_row_out_of_the_command_blocks(self):
        # Eindreview F1: rang 8 ✅ klopt inhoudelijk, maar de worktree is nog gelockt
        # door de sessie-server — git weigert `worktree remove` (exit 128) en
        # `branch -D` (exit 1). Geen plakbaar commando dus, wél een eigen sectie.
        rows = [
            self._rc_row(
                live=False, locked=True, lock_pid=272877, lock_reason=RC_LOCK_REASON
            )
        ]
        out = audit.render_cleanup(rows, alive=always_alive)
        assert "✅ safe to remove [server-lock]" in out  # het oordeel blijft staan
        assert "git worktree remove" not in out
        assert "git branch -D" not in out
        assert "Locked by the session server" in out
        assert f"{RC_PATH}   (server-pid 272877" in out
        assert "unlock" not in out

    def test_bridge_lock_with_own_commits_needs_review(self):
        verdict = audit.judge(
            self._rc_row(
                live=False,
                locked=True,
                lock_pid=272877,
                lock_reason=RC_LOCK_REASON,
                unpushed=2,
            ),
            alive=always_alive,
        )
        assert verdict.rank == 4
        assert verdict.safe is False

    def test_ended_session_without_own_commits_is_safe(self):
        verdict = audit.judge(self._rc_row(live=False), alive=never_alive)
        assert verdict.rank == 8
        assert verdict.safe is True

    def test_ended_session_with_own_commits_needs_review(self):
        verdict = audit.judge(self._rc_row(live=False, unpushed=2), alive=never_alive)
        assert verdict.rank == 4
        assert verdict.safe is False


class TestLadderOrderItself:
    """Test 5 — de volgorde is de veiligheid."""

    def test_rank_three_wins_over_rank_six(self):
        verdict = audit.judge(
            row(
                "beide",
                worktree="/repo/.claude/worktrees/beide",
                dirty=True,
                pr_number=99,
                pr_state="merged",
            ),
            alive=never_alive,
        )
        assert verdict.rank == 3

    def test_rank_one_wins_over_rank_two(self):
        verdict = audit.judge(
            row("main", worktree="/repo", locked=True, lock_pid=1),
            alive=always_alive,
        )
        assert verdict.rank == 1

    def test_open_pr_pauses_before_content_check(self):
        verdict = audit.judge(
            row(
                "worktree-issue-681",
                pr_number=722,
                pr_state="open",
                unique_files=("a.py",),
            ),
            alive=never_alive,
        )
        assert verdict.rank == 5
        assert "722" in verdict.text

    def test_no_pr_with_unique_files_is_a_warning(self):
        verdict = audit.judge(
            row(
                "oud-experiment",
                pr_number=None,
                pr_state=None,
                unique_files=("src/x.py", "src/y.py"),
            ),
            alive=never_alive,
        )
        assert verdict.rank == 7
        assert verdict.safe is False

    def test_no_pr_content_already_in_main_is_safe(self):
        verdict = audit.judge(
            row("oud-experiment", pr_number=None, pr_state=None, unique_files=()),
            alive=never_alive,
        )
        assert verdict.rank == 8
        assert verdict.safe is True

    def test_no_pr_without_own_commits_is_named_empty(self):
        # Een branch die nooit iets committe heeft ook "niets uniek" — maar dan
        # omdat er niets is, niet omdat het werk in main landde. Zo las
        # issue-941 (aangemaakt, nooit gecommit) als "inhoud in main".
        verdict = audit.judge(
            row(
                "issue-941-spike",
                pr_number=None,
                pr_state=None,
                unique_files=(),
                own_commits=0,
            ),
            alive=never_alive,
        )
        assert verdict.rank == 8
        assert verdict.safe is True
        assert verdict.text == "empty — no own commits"

    def test_own_commits_whose_content_is_in_main_keep_the_plain_verdict(self):
        verdict = audit.judge(
            row(
                "oud-experiment",
                pr_number=None,
                pr_state=None,
                unique_files=(),
                own_commits=3,
            ),
            alive=never_alive,
        )
        assert verdict.rank == 8
        assert verdict.text == "safe to remove"

    def test_failed_content_check_blocks_instead_of_reading_as_clean(self):
        # unique_files=None betekent "de inhoudstoets is mislukt", niet "niets
        # uniek". Zonder dit onderscheid zou een gefaalde `run_git` op rang 8
        # "safe to remove" lezen.
        verdict = audit.judge(
            row("oud-experiment", pr_number=None, pr_state=None, unique_files=None),
            alive=never_alive,
        )
        assert verdict.rank == 7
        assert verdict.safe is False

    def test_failed_pr_lookup_blocks_instead_of_reading_as_clean(self):
        # pr_lookup_ok=False betekent "de gh-call zelf faalde", dus pr_state is
        # geen informatie — niet "geen PR gevonden".
        verdict = audit.judge(
            row(
                "oud-experiment",
                pr_number=None,
                pr_state=None,
                pr_lookup_ok=False,
                unique_files=(),
            ),
            alive=never_alive,
        )
        assert verdict.rank == 7
        assert verdict.safe is False

    def test_merged_pr_still_wins_over_a_failed_content_check(self):
        # Pint de volgorde in `_branch_verdict`: de twee rang-7-blokken staan
        # bewust ná de `merged`-toets, dus een gemergede PR wint nog steeds van
        # een mislukte inhoudstoets — de PR-status is het sterkere signaal.
        # Verplaats de rang-7-blokken boven de `merged`-toets en deze test faalt.
        verdict = audit.judge(
            row("feat-merged", pr_number=700, pr_state="merged", unique_files=None),
            alive=never_alive,
        )
        assert verdict.rank == 6
        assert verdict.safe is True


class TestDetachedWorktreeVsOrphanDir:
    """Minor 7 — beide renderen rang 9 ("handmatig"), maar git kent de
    losgekoppelde worktree wél (hij staat in `worktree list`) en de wees-map
    niet. De wees-map is degene die iemand per ongeluk `rm -rf`'t — de tekst
    moet dat onderscheid dragen."""

    def test_detached_worktree_and_orphan_dir_get_different_text(self):
        detached = audit.Row(
            name="detached-spike",
            local=False,
            remote=False,
            worktree="/repo/.claude/worktrees/detached-spike",
            dirty=False,
        )
        orphan = audit.Row(
            name="stale-devcontainer-leftover",
            local=False,
            remote=False,
            orphan_dir="/repo/.claude/worktrees/stale-devcontainer-leftover",
        )
        detached_verdict = audit.judge(detached, alive=never_alive)
        orphan_verdict = audit.judge(orphan, alive=never_alive)
        assert detached_verdict.rank == 9
        assert orphan_verdict.rank == 9
        assert detached_verdict.text != orphan_verdict.text
        assert "detached worktree" in detached_verdict.text
        assert orphan_verdict.text == "orphan — manual"


class TestVerdictRender:
    """Test 6 — Verdict.render() is de enige plek waar modifiers in output landen."""

    def test_render_without_modifiers(self):
        verdict = audit.Verdict(6, "✅", "safe to remove")
        assert verdict.render() == "✅ safe to remove"

    def test_render_with_modifiers(self):
        verdict = audit.Verdict(6, "✅", "safe to remove", ("stale lock",))
        assert verdict.render() == "✅ safe to remove [stale lock]"


PORCELAIN = """worktree /home/corneel/repositories/personal_ai_agent
HEAD 403fde27bb494d3939b1c95238c07f11eb30b5ba
branch refs/heads/main

worktree /home/corneel/repositories/personal_ai_agent/.claude/worktrees/cla-before-public
HEAD 35b1d89e6fe88664b3123bb7f9d5fff47923b307
branch refs/heads/worktree-cla-before-public
locked claude session cla-before-public (pid 1900436 start 21227068)

worktree /home/corneel/repositories/personal_ai_agent/.claude/worktrees/detached-spike
HEAD d94950273bf1dda2a6df2dcb4fd03e5851b94e89
detached
locked
"""


class TestParseWorktrees:
    def test_parses_path_branch_and_lock_pid(self):
        infos = audit.parse_worktrees(PORCELAIN)
        assert [i.path.rsplit("/", 1)[-1] for i in infos] == [
            "personal_ai_agent",
            "cla-before-public",
            "detached-spike",
        ]
        assert infos[0].branch == "main"
        assert infos[0].locked is False
        assert infos[1].branch == "worktree-cla-before-public"
        assert infos[1].locked is True
        assert infos[1].lock_pid == 1900436

    def test_detached_worktree_has_no_branch(self):
        infos = audit.parse_worktrees(PORCELAIN)
        assert infos[2].branch is None

    def test_bare_locked_line_gives_no_pid(self):
        infos = audit.parse_worktrees(PORCELAIN)
        assert infos[2].locked is True
        assert infos[2].lock_pid is None

    def test_bridge_lock_reason_is_captured_verbatim(self):
        # #963: de reden na `locked ` is wat `_lock_state` toetst op het
        # "claude agent bridge-"-prefix van de sessie-server (spike R1).
        porcelain = (
            "worktree /repo/.claude/worktrees/bridge-cse_X\n"
            "HEAD deadbeef\n"
            "branch refs/heads/worktree-bridge-cse_X\n"
            "locked claude agent bridge-cse_X (pid 1 start 2)\n"
        )
        infos = audit.parse_worktrees(porcelain)
        assert infos[0].lock_reason == "claude agent bridge-cse_X (pid 1 start 2)"

    def test_bare_locked_line_gives_no_lock_reason(self):
        infos = audit.parse_worktrees(PORCELAIN)
        assert infos[2].lock_reason is None


class TestParseBranches:
    def test_local_branches_pass_through(self):
        assert audit.parse_branches("main\nissue-665-worktree-audit\n") == [
            "main",
            "issue-665-worktree-audit",
        ]

    def test_remote_branches_lose_the_origin_prefix(self):
        text = "origin/HEAD\norigin/main\norigin/worktree-issue-681\nupstream/x\n"
        assert audit.parse_branches(text, remote=True) == ["main", "worktree-issue-681"]


class TestParsePulls:
    def test_rest_payload_with_merged_at_counts_as_merged(self):
        # De REST-val: `state` is hier "closed", niet "merged". Deze test hoort
        # bij de squash-detectie van AC 3, maar kan pas draaien zodra
        # parse_pulls bestaat — vandaar hier en niet in taak 1.
        pulls = audit.parse_pulls(
            [
                {
                    "number": 721,
                    "state": "closed",
                    "merged_at": "2026-08-24T18:05:13Z",
                    "head": {"ref": "worktree-issue-713-osor-artefacten"},
                }
            ]
        )
        assert pulls["worktree-issue-713-osor-artefacten"].state == "merged"

    def test_highest_number_wins_within_the_same_state(self):
        # De tiebreak binnen dezelfde staat. Zonder de `-number`-term in de
        # sorteersleutel wint de eerst geziene PR en blijft alles groen — dus
        # moet er een geval zijn waarin de staten gelijk zijn.
        pulls = audit.parse_pulls(
            [
                {
                    "number": 300,
                    "state": "open",
                    "merged_at": None,
                    "head": {"ref": "feat-tie"},
                },
                {
                    "number": 400,
                    "state": "open",
                    "merged_at": None,
                    "head": {"ref": "feat-tie"},
                },
            ]
        )
        assert pulls["feat-tie"].number == 400

    def test_open_wins_over_an_older_merged_pr_on_the_same_head(self):
        pulls = audit.parse_pulls(
            [
                {
                    "number": 100,
                    "state": "closed",
                    "merged_at": "2026-01-01T00:00:00Z",
                    "head": {"ref": "feat-x"},
                },
                {
                    "number": 200,
                    "state": "open",
                    "merged_at": None,
                    "head": {"ref": "feat-x"},
                },
            ]
        )
        assert pulls["feat-x"].number == 200
        assert pulls["feat-x"].state == "open"

    def test_closed_unmerged_pr_is_not_merged(self):
        pulls = audit.parse_pulls(
            [
                {
                    "number": 5,
                    "state": "closed",
                    "merged_at": None,
                    "head": {"ref": "feat-y"},
                },
            ]
        )
        assert pulls["feat-y"].state == "closed"

    def test_merged_wins_over_a_closed_unmerged_pr(self):
        pulls = audit.parse_pulls(
            [
                {
                    "number": 5,
                    "state": "closed",
                    "merged_at": None,
                    "head": {"ref": "feat-z"},
                },
                {
                    "number": 6,
                    "state": "closed",
                    "merged_at": "2026-02-02T00:00:00Z",
                    "head": {"ref": "feat-z"},
                },
            ]
        )
        assert pulls["feat-z"].state == "merged"
        assert pulls["feat-z"].number == 6


class TestParsePortRegistry:
    def test_reads_key_value_pairs(self):
        assert audit.parse_port_registry("304=24280\n390=26860\n\n") == {
            "304": "24280",
            "390": "26860",
        }


class TestProbeBranchRef:
    """Critical 1 — de kale naam uit `parse_branches(remote=True)` is voor een
    remote-only branch geen resolvebare ref (de `origin/`-prefix is al
    gestript). `probe_branch` moet meten tegen `ref`, niet tegen `name`."""

    def test_ref_is_used_never_the_bare_name(self, monkeypatch):
        calls: list[list[str]] = []

        def fake_run_git(args, *, cwd=None):
            calls.append(list(args))
            if args[0] == "rev-list":
                return "0\n"
            if args[0] == "diff":
                return ""
            return None

        monkeypatch.setattr(audit, "run_git", fake_run_git)

        probe = audit.probe_branch("feat-x", None, Path("/repo"), ref="origin/feat-x")

        assert probe.unpushed == 0
        rev_list_call = next(c for c in calls if c[0] == "rev-list")
        diff_call = next(c for c in calls if c[0] == "diff")
        assert "origin/feat-x" in rev_list_call
        assert any("origin/feat-x" in part for part in diff_call)
        assert "feat-x" not in rev_list_call
        assert not any(part == "feat-x" for part in diff_call)

    def test_skip_content_leaves_unique_files_none_and_skips_the_diff(
        self, monkeypatch
    ):
        calls: list[list[str]] = []

        def fake_run_git(args, *, cwd=None):
            calls.append(list(args))
            if args[0] == "rev-list":
                return "0\n"
            return ""

        monkeypatch.setattr(audit, "run_git", fake_run_git)

        probe = audit.probe_branch(
            "feat-merged", None, Path("/repo"), ref="feat-merged", skip_content=True
        )

        # None, nooit (): een lege tuple zou beweren dat er gemeten is.
        assert probe.unique_files is None
        assert probe.own_commits is None
        assert not any(c[0] == "diff" for c in calls)
        assert not any("main..feat-merged" in c for c in calls)

    def test_own_commits_are_counted_against_main_on_the_ref(self, monkeypatch):
        def fake_run_git(args, *, cwd=None):
            if args == ["rev-list", "--count", "main..origin/feat-x"]:
                return "4\n"
            if args[0] == "rev-list":
                return "0\n"
            return ""

        monkeypatch.setattr(audit, "run_git", fake_run_git)

        probe = audit.probe_branch("feat-x", None, Path("/repo"), ref="origin/feat-x")

        assert probe.own_commits == 4

    def test_failed_own_commit_count_stays_unknown(self, monkeypatch):
        def fake_run_git(args, *, cwd=None):
            if args[:2] == ["rev-list", "--count"] and args[2].startswith("main.."):
                return None
            if args[0] == "rev-list":
                return "0\n"
            return ""

        monkeypatch.setattr(audit, "run_git", fake_run_git)

        probe = audit.probe_branch("feat-x", None, Path("/repo"), ref="feat-x")

        assert probe.own_commits is None


class TestParseSessions:
    def test_keys_on_cwd_and_keeps_pid_and_session_id(self):
        sessions = audit.parse_sessions(
            [
                {
                    "pid": 1844413,
                    "sessionId": "aef024ab-8bef-4939-bba3-24b4e36ea94c",
                    "cwd": "/repo/.claude/worktrees/issue-625",
                    "startedAt": 1787583127360,
                },
                {"pid": 999, "cwd": "/repo"},  # geen sessionId: overslaan
            ]
        )
        assert set(sessions) == {"/repo/.claude/worktrees/issue-625"}
        assert sessions["/repo/.claude/worktrees/issue-625"].pid == 1844413


class TestTranscripts:
    def test_encoding_replaces_slash_underscore_and_dot(self):
        assert audit.encode_project_dir(
            "/home/corneel/repositories/personal_ai_agent/.claude/worktrees/cla-before-public"
        ) == (
            "-home-corneel-repositories-personal-ai-agent--claude-worktrees-cla-before-public"
        )

    def test_latest_transcript_picks_the_newest_jsonl(self, tmp_path):
        root = tmp_path / "projects"
        d = root / audit.encode_project_dir("/repo/wt")
        d.mkdir(parents=True)
        old = d / "1111.jsonl"
        new = d / "2222.jsonl"
        old.write_text("{}")
        new.write_text("{}")
        import os

        os.utime(old, (1_000_000, 1_000_000))
        os.utime(new, (2_000_000, 2_000_000))
        result = audit.latest_transcript(root, "/repo/wt")
        assert result is not None
        assert result[0] == 2_000_000
        assert result[1] == "2222"

    def test_missing_project_dir_gives_none(self, tmp_path):
        assert audit.latest_transcript(tmp_path, "/repo/nooit-geopend") is None


class TestBuildRows:
    def _rows(self, **overrides: object) -> list[object]:
        kwargs: dict[str, object] = {
            "worktrees": [
                audit.WorktreeInfo("/repo", "main", False, None),
                audit.WorktreeInfo(
                    "/repo/.claude/worktrees/wt-a", "feat-a", True, 4242
                ),
            ],
            "local_branches": ["main", "feat-a", "entire/checkpoints/v1"],
            "remote_branches": ["main", "feat-a", "feat-verweesd"],
            "pulls": {"feat-a": audit.PullInfo(700, "merged")},
            "probes": {"feat-a": audit.BranchProbe(dirty=False, unpushed=0)},
            "sessions": {
                "/repo/.claude/worktrees/wt-a": audit.SessionInfo(
                    4242,
                    "aef024ab-8bef-4939-bba3-24b4e36ea94c",
                    "/repo/.claude/worktrees/wt-a",
                )
            },
            "transcripts": {
                "/repo/.claude/worktrees/wt-a": (1_700_000.0, "oude-sessie-id")
            },
            "detached_dirty": {},
            "orphans": [
                audit.OrphanDir(
                    "worktrees",
                    "/repo/.claude/worktrees/worktrees",
                    (".port-registry",),
                ),
            ],
            "ports": {"700": "20700"},
            "alive": always_alive,
        }
        kwargs.update(overrides)
        return audit.build_rows(**kwargs)

    def test_every_branch_local_or_remote_becomes_a_row(self):
        names = [r.name for r in self._rows()]
        assert names == sorted(
            {"main", "feat-a", "entire/checkpoints/v1", "feat-verweesd", "worktrees"}
        )

    def test_a_remote_only_branch_has_no_worktree(self):
        row_ = next(r for r in self._rows() if r.name == "feat-verweesd")
        assert row_.worktree is None
        assert row_.local is False
        assert row_.remote is True

    def test_worktree_fields_are_joined_onto_the_branch(self):
        row_ = next(r for r in self._rows() if r.name == "feat-a")
        assert row_.worktree == "/repo/.claude/worktrees/wt-a"
        assert row_.locked is True
        assert row_.lock_pid == 4242
        assert row_.pr_number == 700
        assert row_.pr_state == "merged"

    def test_live_session_id_wins_over_the_transcript_id(self):
        row_ = next(r for r in self._rows() if r.name == "feat-a")
        assert row_.session_id == "aef024ab-8bef-4939-bba3-24b4e36ea94c"
        assert row_.live is True
        # De laatste-sessie-kolom komt altijd uit de jsonl-mtime (AC 7).
        assert row_.last_session == 1_700_000.0

    def test_dead_session_pid_is_not_live(self):
        rows = self._rows(alive=never_alive)
        row_ = next(r for r in rows if r.name == "feat-a")
        assert row_.live is False

    def test_orphan_dir_carries_its_entries_so_rank_one_can_see_them(self):
        row_ = next(r for r in self._rows() if r.name == "worktrees")
        assert row_.orphan_dir == "/repo/.claude/worktrees/worktrees"
        assert row_.dir_entries == (".port-registry",)
        assert audit.judge(row_, alive=never_alive).rank == 1

    def test_missing_probe_leaves_the_state_unknown_not_clean(self):
        rows = self._rows(probes={})
        row_ = next(r for r in rows if r.name == "feat-a")
        assert row_.dirty is None
        assert row_.unpushed is None

    def test_port_block_lands_on_the_matching_branch_not_on_an_unrelated_one(
        self,
    ):
        # Important 2: zonder een branch waarvan de naam op een
        # registry-sleutel matcht (het bestaande "700" in `ports` matcht
        # alleen het PR-nummer van feat-a, nooit een branchnaam) draait het
        # hit-pad van `_port_for` nooit.
        rows = self._rows(
            local_branches=[
                "main",
                "feat-a",
                "entire/checkpoints/v1",
                "issue-700-poort",
            ],
            remote_branches=["main", "feat-a", "feat-verweesd", "issue-700-poort"],
        )
        matched = next(r for r in rows if r.name == "issue-700-poort")
        assert matched.port_block == "20700"
        unmatched = next(r for r in rows if r.name == "feat-a")
        assert unmatched.port_block is None

    def test_unique_files_from_the_probe_lands_on_the_row(self):
        rows = self._rows(
            probes={
                "feat-a": audit.BranchProbe(
                    dirty=False, unpushed=0, unique_files=("src/x.py",)
                )
            }
        )
        row_ = next(r for r in rows if r.name == "feat-a")
        assert row_.unique_files == ("src/x.py",)

    def test_own_commits_from_the_probe_lands_on_the_row(self):
        rows = self._rows(
            probes={
                "feat-a": audit.BranchProbe(
                    dirty=False, unpushed=0, unique_files=(), own_commits=0
                )
            }
        )
        row_ = next(r for r in rows if r.name == "feat-a")
        assert row_.own_commits == 0

    def test_failed_pr_lookup_lands_on_every_branch_row(self):
        # Orphan- en detached-rijen hebben geen PR-lookup — alleen een
        # branchrij (local of remote) draagt `pr_lookup_ok`.
        rows = self._rows(pr_lookup_ok=False)
        branch_rows = [r for r in rows if r.local or r.remote]
        assert branch_rows
        assert all(r.pr_lookup_ok is False for r in branch_rows)

    def test_detached_head_worktree_gets_its_own_row(self):
        # Minor 6: een worktree zonder branchregel is geen wees (git kent
        # hem) en staat niet in local/remote — zonder een eigen rij
        # verdwijnt hij spoorloos uit de tabel.
        rows = self._rows(
            worktrees=[
                audit.WorktreeInfo("/repo", "main", False, None),
                audit.WorktreeInfo(
                    "/repo/.claude/worktrees/wt-a", "feat-a", True, 4242
                ),
                audit.WorktreeInfo(
                    "/repo/.claude/worktrees/detached-spike", None, False, None
                ),
            ],
            detached_dirty={"/repo/.claude/worktrees/detached-spike": True},
        )
        row_ = next(r for r in rows if r.name == "detached-spike")
        assert row_.worktree == "/repo/.claude/worktrees/detached-spike"
        assert row_.local is False
        assert row_.remote is False
        assert row_.dirty is True
        assert audit.judge(row_, alive=never_alive).rank == 3

    def test_clean_detached_head_worktree_is_an_orphan_to_hand(self):
        rows = self._rows(
            worktrees=[
                audit.WorktreeInfo("/repo", "main", False, None),
                audit.WorktreeInfo(
                    "/repo/.claude/worktrees/detached-spike", None, False, None
                ),
            ],
            detached_dirty={"/repo/.claude/worktrees/detached-spike": False},
        )
        row_ = next(r for r in rows if r.name == "detached-spike")
        assert audit.judge(row_, alive=never_alive).rank == 9


class TestRenderers:
    def _mixed_rows(self) -> list[object]:
        return [
            row("main", worktree="/repo"),
            row("feat-merged", pr_number=700, pr_state="merged"),
            row("feat-merged-2", local=False, pr_number=701, pr_state="merged"),
            row("feat-open", pr_number=722, pr_state="open"),
            row(
                "feat-dirty", worktree="/repo/.claude/worktrees/feat-dirty", dirty=True
            ),
            row("lokaal-only", remote=False, pr_number=9, pr_state="merged"),
            audit.Row(
                name="worktrees",
                local=False,
                remote=False,
                orphan_dir="/repo/.claude/worktrees/worktrees",
                dir_entries=(".port-registry",),
            ),
        ]

    def test_cleanup_emits_the_table_header_from_the_issue(self):
        out = audit.render_cleanup(self._mixed_rows(), alive=never_alive)
        assert (
            "| worktree | branch | PR state | unpushed commits | dirty tree "
            "| last session | live? | verdict |"
        ) in out

    def test_cleanup_command_blocks_only_list_safe_rows(self):
        out = audit.render_cleanup(self._mixed_rows(), alive=never_alive)
        assert "git push origin --delete feat-merged feat-merged-2" in out
        assert "git branch -D feat-merged" in out
        for forbidden in ("main", "feat-open", "feat-dirty", "worktrees"):
            assert f"--delete {forbidden}" not in out
            assert f"branch -D {forbidden}" not in out

    def test_cleanup_never_emits_unlock_or_kill(self):
        out = audit.render_cleanup(self._mixed_rows(), alive=never_alive)
        assert "unlock" not in out
        assert "kill " not in out

    def test_local_only_safe_branch_is_not_pushed_for_deletion(self):
        rows = [row("feat-lokaal", remote=False, pr_number=1, pr_state="merged")]
        out = audit.render_cleanup(rows, alive=never_alive)
        assert "git push origin --delete" not in out
        assert "git branch -D feat-lokaal" in out

    def test_cleanup_emits_worktree_remove_before_branch_delete(self):
        """Belangrijk 1/3: een gepaste worktree-rij moet met `git worktree
        remove` vóór `git branch -D` in het commandoblok staan — anders
        weigert git de branch-delete halverwege een paste."""
        rows = [
            row(
                "feat-wt",
                worktree="/repo/.claude/worktrees/feat-wt",
                pr_number=5,
                pr_state="merged",
            ),
        ]
        out = audit.render_cleanup(rows, alive=never_alive)
        assert 'git worktree remove "/repo/.claude/worktrees/feat-wt"' in out
        assert out.index("git worktree remove") < out.index("git branch -D feat-wt")
        # Minor 5: in een eigen ```bash-fence, net als de andere blokken.
        assert "```bash\ngit worktree remove" in out

    def test_cleanup_fallback_when_nothing_is_safe(self):
        """Belangrijk 3: de fallback-tekst bij een verdictloze rij is
        ongetest gebleven; dit pint hem, en dat er geen enkel commando
        (van geen van de drie soorten) toch verschijnt."""
        rows = [row("feat-open", pr_number=1, pr_state="open")]
        out = audit.render_cleanup(rows, alive=never_alive)
        assert "Nothing with verdict ✅ — no commands." in out
        assert "git push origin --delete" not in out
        assert "git branch -D" not in out
        assert "git worktree remove" not in out

    def test_cleanup_excludes_stale_locked_row_from_every_command_block(self):
        """Belangrijk 1: een stale lock (dode pid) laat de rij doorzakken
        naar rang 6/8 — maar de worktree is nog steeds gelockt en git weigert
        `worktree remove`/`branch -D` erop. Zo'n rij mag in geen van de drie
        commandoblokken staan, alleen in de eigen sectie."""
        rows = [
            row(
                "issue-624",
                worktree="/repo/.claude/worktrees/issue-624",
                locked=True,
                lock_pid=472155,
                pr_number=5,
                pr_state="merged",
            ),
        ]
        out = audit.render_cleanup(rows, alive=never_alive)
        assert "| issue-624 |" in out  # blijft in de tabel staan
        assert "git worktree remove" not in out
        assert "git branch -D" not in out
        assert "git push origin --delete" not in out
        assert (
            "Locked with a dead pid — release the lock by hand first, then "
            "run again:" in out
        )
        assert "/repo/.claude/worktrees/issue-624" in out
        assert "lock-pid 472155" in out

    def test_cleanup_never_offers_an_unlock_command(self):
        # Aanvullend op test_cleanup_never_emits_unlock_or_kill: specifiek de
        # nieuwe verweesde-lock-sectie mag ook nooit `git worktree unlock`
        # aanbieden — naam noemen mag, ontgrendelen nooit.
        rows = [
            row(
                "issue-624",
                worktree="/repo/.claude/worktrees/issue-624",
                locked=True,
                lock_pid=472155,
                pr_number=5,
                pr_state="merged",
            ),
        ]
        out = audit.render_cleanup(rows, alive=never_alive)
        assert "unlock" not in out

    def test_cleanup_lists_port_blocks_after_the_command_blocks(self):
        """Belangrijk 2: `port_block` werd gemeten en getest maar nooit
        getoond. Een rij met een poortblok moet zichtbaar zijn, met de
        expliciete waarschuwing dat het houdende proces hier niet wordt
        opgezocht (#438)."""
        rows = [
            row("issue-700-poort", pr_number=1, pr_state="open", port_block="20700"),
        ]
        out = audit.render_cleanup(rows, alive=never_alive)
        assert "Port blocks" in out
        assert "20700" in out
        assert "issue-700-poort" in out

    def test_cleanup_omits_port_block_section_when_nothing_is_registered(self):
        out = audit.render_cleanup(self._mixed_rows(), alive=never_alive)
        assert "Port blocks" not in out

    def test_sessions_view_emits_a_resume_command(self):
        rows = [
            row(
                "feat-a",
                worktree="/repo/.claude/worktrees/wt-a",
                session_id="aef024ab",
                last_session=1_700_000.0,
                live=True,
            ),
        ]
        out = audit.render_sessions(rows, alive=never_alive)
        assert "claude --resume aef024ab" in out
        assert "LIVE" in out

    def test_sessions_view_skips_rows_without_a_session(self):
        out = audit.render_sessions([row("feat-a")], alive=never_alive)
        assert "claude --resume" not in out

    def test_sessions_view_marks_dead_sessions_as_dood(self):
        """Minor 7: de dode tak van de LIVE/dood-render was nooit geasserteerd."""
        rows = [
            row(
                "feat-b",
                worktree="/repo/.claude/worktrees/wt-b",
                session_id="deadbeef",
                last_session=1_700_000.0,
                live=False,
            ),
        ]
        out = audit.render_sessions(rows, alive=never_alive)
        assert "dead" in out
        assert "LIVE" not in out

    def test_sessions_view_sorts_newest_last_session_first(self):
        """Belangrijk 2: pint de sort — omdraaien of weglaten van
        `pairs.sort(...)` liet alle 8 renderer-tests groen."""
        rows = [
            row("feat-old", session_id="oldsession", last_session=1_000.0),
            row("feat-new", session_id="newsession", last_session=2_000.0),
        ]
        out = audit.render_sessions(rows, alive=never_alive)
        assert out.index("newsession") < out.index("oldsession")

    def test_sessions_view_puts_live_session_first_without_timestamp(self):
        """Minor 6: een live sessie zonder transcript (last_session=None)
        mag niet onder een oude dode sessie wegzakken."""
        rows = [
            row(
                "feat-dead",
                session_id="deadsession",
                last_session=2_000.0,
                live=False,
            ),
            row(
                "feat-live",
                session_id="livesession",
                last_session=None,
                live=True,
            ),
        ]
        out = audit.render_sessions(rows, alive=never_alive)
        assert out.index("livesession") < out.index("deadsession")

    def test_origin_view_lists_only_remote_rows(self):
        rows = self._mixed_rows()
        origin_out = audit.render_origin(rows, alive=never_alive)
        cleanup_out = audit.render_cleanup(rows, alive=never_alive)
        assert "feat-merged-2" in origin_out
        assert "worktrees" not in origin_out
        # lokaal-only heeft remote=False: hoort niet in de origin-view (die
        # is remote-only), maar wel gewoon in de volle cleanup-view.
        assert "lokaal-only" not in origin_out
        assert "lokaal-only" in cleanup_out

    def test_kpi_is_one_line_with_four_numbers(self):
        out = audit.render_kpi(self._mixed_rows(), alive=never_alive).strip()
        assert out.count("\n") == 0
        # Minor 6: alleen feat-dirty telt mee — main's worktree is de
        # hoofdcheckout ("/repo"), niet een map onder `.claude/worktrees/`.
        assert "worktrees=1" in out
        assert "safe=3" in out  # feat-merged + feat-merged-2 + lokaal-only
        assert "remote_branches=5" in out  # de wees-rij en lokaal-only zijn niet remote
        assert "warn=0" in out

    def test_kpi_worktrees_counts_only_paths_under_claude_worktrees(self):
        """Minor 6: het label zegt "Worktrees onder `.claude/worktrees/`" —
        de hoofdcheckout mag niet meetellen, ook al draagt hij ook een
        `worktree`-veld."""
        rows = [
            row("main", worktree="/repo"),
            row(
                "feat-a",
                worktree="/repo/.claude/worktrees/feat-a",
                pr_number=1,
                pr_state="open",
            ),
            row(
                "feat-b",
                worktree="/repo/.claude/worktrees/feat-b",
                pr_number=2,
                pr_state="open",
            ),
        ]
        out = audit.render_kpi(rows, alive=never_alive)
        assert "worktrees=2" in out


class TestRenderOnly:
    """#1422: de view voor paia-close-issue — één branch, één opruimen-regel.

    De skill leest alleen `cleanup: yes` plus exit 0 als toestemming om voor
    te stellen; elke andere uitkomst is nee."""

    WT = "/repo/.claude/worktrees/issue-9-x"
    ELDERS = Path("/elders")

    def _merged(self, **kwargs: object) -> object:
        defaults: dict[str, object] = {
            "worktree": self.WT,
            "pr_number": 9,
            "pr_state": "merged",
        }
        defaults.update(kwargs)
        return row("issue-9-x", **defaults)

    def test_merged_worktree_is_opruimbaar_in_volgorde(self):
        out, ok = audit.render_only(
            [self._merged()], "issue-9-x", cwd=self.ELDERS, alive=never_alive
        )
        assert ok is True
        assert "cleanup: yes" in out
        remove = out.index(f'git worktree remove "{self.WT}"')
        delete = out.index("git branch -D issue-9-x")
        assert remove < delete
        assert "push origin --delete" not in out

    def test_open_pr_is_niet_opruimbaar(self):
        out, ok = audit.render_only(
            [self._merged(pr_state="open")],
            "issue-9-x",
            cwd=self.ELDERS,
            alive=never_alive,
        )
        assert ok is False
        assert any(line.startswith("cleanup: no — ") for line in out.splitlines())
        assert "git " not in out

    def test_dirty_tree_is_niet_opruimbaar(self):
        out, ok = audit.render_only(
            [self._merged(dirty=True)], "issue-9-x", cwd=self.ELDERS, alive=never_alive
        )
        assert ok is False
        assert "git " not in out

    def test_onbekende_branch(self):
        out, ok = audit.render_only(
            [self._merged()], "weg", cwd=self.ELDERS, alive=never_alive
        )
        assert ok is False
        assert "cleanup: no — branch weg not found" in out

    def test_server_lock_is_niet_opruimbaar(self):
        locked = self._merged(locked=True, lock_reason="claude agent bridge-x")
        out, ok = audit.render_only(
            [locked], "issue-9-x", cwd=self.ELDERS, alive=never_alive
        )
        assert ok is False
        assert "(locked)" in out
        assert "git " not in out

    def test_eigen_cwd_in_submap(self):
        out, ok = audit.render_only(
            [self._merged()],
            "issue-9-x",
            cwd=Path(self.WT) / "src",
            alive=never_alive,
        )
        assert ok is False
        assert f"own cwd is in {self.WT}" in out
        assert "git " not in out

    def test_alleen_lokale_branch(self):
        out, ok = audit.render_only(
            [row("issue-9-x", remote=False)],
            "issue-9-x",
            cwd=self.ELDERS,
            alive=never_alive,
        )
        assert ok is True
        assert "git branch -D issue-9-x" in out
        assert "worktree remove" not in out

    def test_dubbele_naam_is_niet_opruimbaar(self):
        # build_rows geeft een losgekoppelde worktree de mapnaam als naam; die
        # kan gelijk zijn aan een branch. match[0] zou dan de branchrij nemen
        # en een lopende sessie in de worktree missen.
        branch = row("issue-9-x", remote=False)
        detached = row(
            "issue-9-x", local=False, remote=False, worktree=self.WT, live=True
        )
        out, ok = audit.render_only(
            [branch, detached], "issue-9-x", cwd=self.ELDERS, alive=never_alive
        )
        assert ok is False
        assert "cleanup: no — 2 rows named issue-9-x" in out
        assert "git " not in out

    def test_main_exitcode(self, monkeypatch, capsys):
        monkeypatch.setattr(
            audit, "collect", lambda repo=None: [self._merged(pr_state="open")]
        )
        assert audit.main(["--only", "issue-9-x"]) == 1
        monkeypatch.setattr(audit, "collect", lambda repo=None: [self._merged()])
        assert audit.main(["--only", "issue-9-x"]) == 0
        assert "cleanup: yes" in capsys.readouterr().out


class TestRenderIssue:
    """#32: `--issue <nr>` geeft per branch van een issue een blok met `cleanup:`."""

    WT = "/repo/.claude/worktrees/issue-9-x"
    ELDERS = Path("/elders")

    @staticmethod
    def _payload(closing=(), cross=()):
        return {
            "data": {
                "repository": {
                    "issue": {
                        "closedByPullRequestsReferences": {"nodes": list(closing)},
                        "timelineItems": {"nodes": [{"source": s} for s in cross]},
                    }
                }
            }
        }

    @staticmethod
    def _pr(head, *, merged=True, repo="o/r", body=None):
        node = {
            "headRefName": head,
            "merged": merged,
            "repository": {"nameWithOwner": repo},
        }
        if body is not None:
            node["body"] = body
        return node

    def test_parse_keeps_merged_same_repo_heads(self):
        payload = self._payload(
            closing=[self._pr("issue-9-x")],
            cross=[self._pr("fix-y", body="Refs #9 — deel 1")],
        )
        assert audit.parse_issue_pr_heads(payload, "o/r", 9) == ["issue-9-x", "fix-y"]

    def test_parse_cross_ref_needs_refs_line(self):
        for body in ("zie #9", "Refs #93", None):
            payload = self._payload(cross=[self._pr("fix-y", body=body)])
            assert audit.parse_issue_pr_heads(payload, "o/r", 9) == []

    def test_parse_skips_unmerged_and_non_pr(self):
        payload = self._payload(
            closing=[self._pr("issue-9-x", merged=False)],
            cross=[{}, self._pr("fix-y", merged=False, body="Refs #9")],
        )
        assert audit.parse_issue_pr_heads(payload, "o/r", 9) == []

    def test_parse_skips_other_repos(self):
        payload = self._payload(
            closing=[self._pr("issue-9-x", repo="x/fork")],
            cross=[self._pr("fix-y", repo="x/fork", body="Refs #9")],
        )
        assert audit.parse_issue_pr_heads(payload, "o/r", 9) == []

    def test_parse_null_issue_is_failure(self):
        null_issue = {"data": {"repository": {"issue": None}}}
        assert audit.parse_issue_pr_heads(null_issue, "o/r", 9) is None
        errors = {"errors": [{"message": "x"}]}
        assert audit.parse_issue_pr_heads(errors, "o/r", 9) is None

    def test_prefix_needs_the_dash(self):
        rows = [row("issue-3-a"), row("issue-32-b")]
        assert audit.issue_branches(rows, 3, []) == ["issue-3-a"]

    def test_pr_head_and_prefix_dedupe(self):
        got = audit.issue_branches([row("issue-9-x")], 9, ["issue-9-x", "fix-y"])
        assert got == ["fix-y", "issue-9-x"]

    def test_two_merged_branches_both_yes(self):
        rows = [
            row("issue-9-x", worktree=self.WT, pr_number=1, pr_state="merged"),
            row("issue-9-y", pr_number=2, pr_state="merged"),
        ]
        out = audit.render_issue(rows, 9, [], cwd=self.ELDERS, alive=never_alive)
        assert out.index("branch: issue-9-x") < out.index("branch: issue-9-y")
        assert out.splitlines().count("cleanup: yes") == 2
        remove = out.index(f'git worktree remove "{self.WT}"')
        assert remove < out.index("git branch -D issue-9-x")

    def test_local_branch_without_merged_pr_is_no(self, monkeypatch):
        out = audit.render_issue(
            [row("issue-9-x", pr_number=1, pr_state="open")],
            9,
            [],
            cwd=self.ELDERS,
            alive=never_alive,
        )
        assert "cleanup: no — " in out
        assert not any(line.startswith("git ") for line in out.splitlines())

        monkeypatch.setattr(
            audit,
            "render_only",
            lambda *a, **k: ("cleanup: yes\ngit branch -D issue-9-x\n", True),
        )
        out = audit.render_issue(
            [row("issue-9-x", pr_state=None)], 9, [], cwd=self.ELDERS, alive=never_alive
        )
        assert "cleanup: no — no merged PR" in out
        assert "git branch -D" not in out
        assert "cleanup: yes" not in out

    def test_own_cwd_blocks_only_its_branch(self):
        rows = [
            row("issue-9-x", worktree=self.WT, pr_number=1, pr_state="merged"),
            row(
                "issue-9-y",
                worktree="/repo/.claude/worktrees/issue-9-y",
                pr_number=2,
                pr_state="merged",
            ),
        ]
        out = audit.render_issue(
            rows, 9, [], cwd=Path(self.WT) / "src", alive=never_alive
        )
        first, second = out.split("branch: issue-9-y")
        assert f"own cwd is in {self.WT}" in first
        assert "cleanup: yes" in second

    def test_unknown_pr_head_is_reported(self):
        out = audit.render_issue([], 9, ["gone"], cwd=self.ELDERS, alive=never_alive)
        assert "branch: gone" in out
        assert "cleanup: no — branch gone not found" in out

    def test_failed_lookup_and_empty(self):
        rows = [row("issue-9-x")]
        failed = audit.render_issue(rows, 9, None, cwd=self.ELDERS, alive=never_alive)
        assert failed == "cleanup: no — PR lookup for issue #9 failed\n"
        empty = audit.render_issue([], 9, [], cwd=self.ELDERS, alive=never_alive)
        assert empty == "cleanup: no — no branches found for issue #9\n"

    def test_main_exit_2_on_failed_lookup(self, monkeypatch, capsys):
        monkeypatch.setattr(audit, "collect", lambda repo=None: [])
        monkeypatch.setattr(audit, "repo_slug", lambda: "o/r")
        monkeypatch.setattr(audit, "run_gh_issue_pr_heads", lambda slug, nr: None)
        assert audit.main(["--issue", "9"]) == 2
        assert "PR lookup for issue #9 failed" in capsys.readouterr().out

    def test_main_exit_0_with_blocks(self, monkeypatch, capsys):
        merged = row("issue-9-x", worktree=self.WT, pr_number=1, pr_state="merged")
        monkeypatch.setattr(audit, "collect", lambda repo=None: [merged])
        monkeypatch.setattr(audit, "repo_slug", lambda: "o/r")
        monkeypatch.setattr(audit, "run_gh_issue_pr_heads", lambda slug, nr: [])
        assert audit.main(["--issue", "9"]) == 0
        assert "cleanup: yes" in capsys.readouterr().out

    def test_run_gh_issue_pr_heads_argv(self, monkeypatch):
        import subprocess

        seen: list[list[str]] = []

        def make(result):
            def fake(argv, **kwargs):
                seen.append(argv)
                if isinstance(result, Exception):
                    raise result
                return subprocess.CompletedProcess(argv, result[0], result[1], "")

            return fake

        ok = json.dumps(self._payload())
        monkeypatch.setattr(audit.subprocess, "run", make((0, ok)))
        assert audit.run_gh_issue_pr_heads("o/r", 9) == []
        argv = seen[0]
        assert argv[:3] == ["gh", "api", "graphql"]
        for pair in (("-f", "owner=o"), ("-f", "name=r"), ("-F", "nr=9")):
            assert any(argv[i : i + 2] == list(pair) for i in range(len(argv) - 1))

        seen.clear()
        assert audit.run_gh_issue_pr_heads(None, 9) is None
        assert seen == []
        for result in (
            (1, ""),
            (0, "niet-json"),
            subprocess.TimeoutExpired(["gh"], 1),
        ):
            monkeypatch.setattr(audit.subprocess, "run", make(result))
            assert audit.run_gh_issue_pr_heads("o/r", 9) is None


class TestCollectFailsLoudOnGitFailure:
    """Minor 4 — `run_git(...) or ""` verstopte een mislukte git-call als een
    lege, schone wereld: dan vuurt de rang-3-vuile-tree-guard voor niets meer.
    Een tool die de repo niet kan lezen moet dat zeggen (SystemExit met de
    naam van het mislukte commando), niet leeg en "clean" ogen."""

    def test_worktree_list_failure_is_reported_by_name(self, monkeypatch):
        def fake_run_git(args, *, cwd=None):
            if args[:2] == ["worktree", "list"]:
                return None
            return ""

        monkeypatch.setattr(audit, "run_git", fake_run_git)
        with pytest.raises(SystemExit, match="worktree list"):
            audit.collect(Path("/repo"))

    def test_local_branch_listing_failure_is_reported_by_name(self, monkeypatch):
        def fake_run_git(args, *, cwd=None):
            if args[:2] == ["worktree", "list"]:
                return ""
            if args == ["branch", "--format=%(refname:short)"]:
                return None
            return ""

        monkeypatch.setattr(audit, "run_git", fake_run_git)
        with pytest.raises(SystemExit, match="git branch --format"):
            audit.collect(Path("/repo"))

    def test_remote_branch_listing_failure_is_reported_by_name(self, monkeypatch):
        def fake_run_git(args, *, cwd=None):
            if args[:2] == ["worktree", "list"]:
                return ""
            if args == ["branch", "--format=%(refname:short)"]:
                return "main\n"
            if args[:2] == ["branch", "-r"]:
                return None
            return ""

        monkeypatch.setattr(audit, "run_git", fake_run_git)
        with pytest.raises(SystemExit, match="git branch -r"):
            audit.collect(Path("/repo"))


class TestSquashArtefactDoesNotMaskContent:
    """Rang 4 mat "commits die nergens op een remote staan". Bij een squash-merge
    is dat per constructie de hele branchhistorie: GitHub gooit de losse commits
    weg en zet één nieuwe commit op main. Op deze repo maakte dat 9 van de 14
    ⚠️-rijen vals — en omdat rang 4 bóven de inhoudstoets staat, werden die
    branches nooit op inhoud gemeten.

    De maat wordt daarom: is er een commit ná de merge? Zo nee, dan is de
    ongepushte telling het artefact en niet het signaal.
    """

    MERGED = 1_754_040_000.0  # merge-moment van de PR

    def test_tip_before_merge_is_the_squash_artefact_not_unpushed_work(self):
        verdict = audit.judge(
            row(
                "worktree-441-besluithistorie-docs",
                unpushed=12,
                pr_number=457,
                pr_state="merged",
                pr_merged_at=self.MERGED,
                tip_committed_at=self.MERGED - 3600,
            ),
            alive=never_alive,
        )
        assert verdict.rank == 6
        assert verdict.safe is True
        assert "squash-merge" in verdict.modifiers

    def test_commit_after_the_merge_still_blocks(self):
        # Het #544-geval blijft staan: ná de merge doorgecommit is echt werk.
        verdict = audit.judge(
            row(
                "feat-544",
                unpushed=2,
                pr_number=544,
                pr_state="merged",
                pr_merged_at=self.MERGED,
                tip_committed_at=self.MERGED + 3600,
            ),
            alive=never_alive,
        )
        assert verdict.rank == 4
        assert verdict.safe is False

    def test_missing_merge_timestamp_keeps_the_block(self):
        # Onbekend is nooit schoon — dezelfde norm als bij de inhoudstoets.
        verdict = audit.judge(
            row(
                "feat-544",
                unpushed=2,
                pr_number=544,
                pr_state="merged",
                pr_merged_at=None,
                tip_committed_at=self.MERGED - 3600,
            ),
            alive=never_alive,
        )
        assert verdict.rank == 4
        assert verdict.safe is False

    def test_missing_tip_timestamp_keeps_the_block(self):
        verdict = audit.judge(
            row(
                "feat-544",
                unpushed=2,
                pr_number=544,
                pr_state="merged",
                pr_merged_at=self.MERGED,
                tip_committed_at=None,
            ),
            alive=never_alive,
        )
        assert verdict.rank == 4
        assert verdict.safe is False

    def test_open_pr_is_never_excused_by_timestamps(self):
        # Alleen een gemergede PR kan het artefact verklaren. Bij een open PR
        # betekent een ongepushte commit nog steeds ongepusht.
        verdict = audit.judge(
            row(
                "feat-open",
                unpushed=2,
                pr_number=542,
                pr_state="open",
                pr_merged_at=self.MERGED,
                tip_committed_at=self.MERGED - 3600,
            ),
            alive=never_alive,
        )
        assert verdict.rank == 4
        assert verdict.safe is False

    def test_dirty_tree_still_beats_the_squash_excuse(self):
        # Rang 3 blijft boven rang 4; ongecommit werk wint van elke verklaring.
        verdict = audit.judge(
            row(
                "feat-vuil",
                worktree="/repo/.claude/worktrees/feat-vuil",
                dirty=True,
                unpushed=12,
                pr_number=457,
                pr_state="merged",
                pr_merged_at=self.MERGED,
                tip_committed_at=self.MERGED - 3600,
            ),
            alive=never_alive,
        )
        assert verdict.rank == 3
        assert verdict.safe is False


class TestOrphanRootedBranchIsNamedNotGuessed:
    """Testlekkage kwam binnen als "N local commits — check", precies zoals
    echt ongepusht werk. `tests/integration/test_pre_push_checkpoint_scan.sh`
    documenteert de oorzaak: als `mktemp -d` faalt (sandbox), landen zijn
    `git checkout --orphan`-fixtures in de ECHTE repo. Zo ontstonden
    `branch-dirty` en `branch-allowlisted`.

    De handtekening is scherp: één commit die zijn eigen root is. Pre-rewrite
    branches als `v2.1-mac-thin-client` hebben óók een losse historie maar wel
    ouders, dus die vallen er niet onder.
    """

    def test_parentless_local_branch_is_named_as_leakage(self):
        verdict = audit.judge(
            row(
                "branch-dirty",
                local=True,
                remote=False,
                unpushed=1,
                root_commit=True,
                pr_number=None,
                pr_state=None,
            ),
            alive=never_alive,
        )
        assert verdict.rank == 4
        assert "test leakage" in verdict.text

    def test_leakage_is_never_safe(self):
        # Het blijft handwerk. Een heuristiek mag nooit zelf "safe to remove" zeggen —
        # dat is precies de non-negotiable van deze skill.
        verdict = audit.judge(
            row("branch-dirty", local=True, remote=False, unpushed=1, root_commit=True),
            alive=never_alive,
        )
        assert verdict.safe is False

    def test_branch_with_a_parent_is_not_leakage(self):
        verdict = audit.judge(
            row(
                "v2.1-mac-thin-client",
                local=True,
                remote=False,
                unpushed=27,
                root_commit=False,
            ),
            alive=never_alive,
        )
        assert verdict.rank == 4
        assert "test leakage" not in verdict.text
        assert "27" in verdict.text

    def test_a_pushed_orphan_branch_is_not_leakage(self):
        # Op origin gepusht is geen ongelukje meer — dan is het bedoeld werk.
        verdict = audit.judge(
            row("gh-pages", local=True, remote=True, unpushed=1, root_commit=True),
            alive=never_alive,
        )
        assert "test leakage" not in verdict.text

    def test_orphan_with_a_pr_is_not_leakage(self):
        verdict = audit.judge(
            row(
                "orphan-met-pr",
                local=True,
                remote=False,
                unpushed=1,
                root_commit=True,
                pr_number=99,
                pr_state="open",
            ),
            alive=never_alive,
        )
        assert "test leakage" not in verdict.text

    def test_unknown_root_state_is_not_called_leakage(self):
        verdict = audit.judge(
            row("onbekend", local=True, remote=False, unpushed=1, root_commit=None),
            alive=never_alive,
        )
        assert "test leakage" not in verdict.text


class TestEpochParsing:
    """De tijdzoneval die de squash-toets zou omkeren."""

    def test_github_utc_iso_becomes_epoch(self):
        assert audit._epoch("2026-08-02T08:08:31Z") == 1_785_658_111.0

    def test_a_local_offset_is_honoured_not_ignored(self):
        # 08:24+02:00 is 06:24Z — VÓÓR 08:08Z, niet erna. Precies andersom dan
        # een tekstvergelijking leest.
        assert audit._epoch("2026-08-02T08:24:00+02:00") < audit._epoch(
            "2026-08-02T08:08:31Z"
        )

    def test_none_and_garbage_stay_none(self):
        assert audit._epoch(None) is None
        assert audit._epoch("") is None
        assert audit._epoch("gisteren") is None
        assert audit._epoch(12345) is None


class TestTipShape:
    """`rev-list -1 --timestamp --parents` levert `<epoch> <sha> [<parent>...]`."""

    def _stub(self, monkeypatch, output):
        monkeypatch.setattr(audit, "run_git", lambda *a, **k: output)

    def test_commit_with_a_parent(self, monkeypatch):
        self._stub(monkeypatch, "1787932083 3d7145fc dc6f198c\n")
        assert audit._tip_shape("main", Path("/repo")) == (1_787_932_083.0, False)

    def test_root_commit_has_no_parent_field(self, monkeypatch):
        self._stub(monkeypatch, "1787900000 254decd7\n")
        assert audit._tip_shape("branch-dirty", Path("/repo")) == (
            1_787_900_000.0,
            True,
        )

    def test_merge_commit_has_two_parents(self, monkeypatch):
        self._stub(monkeypatch, "1787932083 aaaaaaa bbbbbbb ccccccc\n")
        assert audit._tip_shape("main", Path("/repo")) == (1_787_932_083.0, False)

    def test_a_failed_call_measures_nothing(self, monkeypatch):
        for output in (None, "", "   ", "geen-getal abc"):
            self._stub(monkeypatch, output)
            assert audit._tip_shape("x", Path("/repo")) == (None, None)


class TestPullInfoCarriesMergedAt:
    def test_merged_at_reaches_pullinfo_as_epoch(self):
        pulls = audit.parse_pulls(
            [
                {
                    "number": 457,
                    "state": "closed",
                    "merged_at": "2026-08-02T08:08:31Z",
                    "head": {"ref": "worktree-441-besluithistorie-docs"},
                }
            ]
        )
        info = pulls["worktree-441-besluithistorie-docs"]
        assert info.state == "merged"
        assert info.merged_at == 1_785_658_111.0

    def test_an_open_pr_has_no_merge_moment(self):
        pulls = audit.parse_pulls(
            [
                {
                    "number": 542,
                    "state": "open",
                    "merged_at": None,
                    "head": {"ref": "spike"},
                }
            ]
        )
        assert pulls["spike"].merged_at is None


def test_repo_slug_leest_lighthouse_en_geen_origin(tmp_path, monkeypatch) -> None:
    config = tmp_path / "other.toml"
    config.write_text('[forge]\nkind = "github"\nrepo = "Other/repo"\n', encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(config))
    monkeypatch.setattr(
        audit, "run_git", lambda *a, **k: "https://github.com/someone/else.git"
    )
    assert audit.repo_slug() == "Other/repo"


def test_without_forge_repo_the_pr_lookup_is_unknown(tmp_path, monkeypatch) -> None:
    """No `forge.repo`: no PR lookup, so every PR state is unknown, never "no PR"."""
    config = tmp_path / "bare.toml"
    config.write_text('[project]\nlanguage = "en"\n', encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(config))
    assert audit.repo_slug() is None
    assert audit.run_gh_pulls(None) is None


def test_broken_config_stops_collect_loudly(tmp_path, monkeypatch, capsys) -> None:
    config = tmp_path / "broken.toml"
    config.write_text('[worktree_audit]\nprotected_refs = "x"\n', encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(config))
    monkeypatch.setattr(
        audit,
        "run_git",
        lambda args, *, cwd=None: "worktree /repo\nbranch refs/heads/main\n"
        if args[:2] == ["worktree", "list"]
        else "",
    )
    with pytest.raises(SystemExit) as stop:
        audit.collect(Path("/repo"))
    # rc 2, like every other script: under `--only` rc 1 already means "not cleanable".
    assert stop.value.code == 2
    assert re.match(r"worktree-audit: .*protected_refs", capsys.readouterr().err)


def test_without_forge_repo_collect_says_it_skips_the_pr_lookup(
    tmp_path, monkeypatch, capsys
) -> None:
    config = tmp_path / "bare.toml"
    config.write_text('[project]\nlanguage = "en"\n', encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(config))
    monkeypatch.setattr(
        audit,
        "run_git",
        lambda args, *, cwd=None: "worktree /repo\nbranch refs/heads/main\n"
        if args[:2] == ["worktree", "list"]
        else "",
    )
    audit.collect(Path("/repo"))
    assert (
        "worktree-audit: PR lookup skipped — [forge] repo is not set, so every PR state is unknown"
        in capsys.readouterr().err
    )


class TestProtectedFromConfig:
    """`entire/*` and `.port-registry` are protected only when the project lists them;
    the default branch always is."""

    def _apply(self, tmp_path, monkeypatch, body: str) -> None:
        config = tmp_path / "p.toml"
        config.write_text(body, encoding="utf-8")
        monkeypatch.setenv("LIGHTHOUSE_TOML", str(config))
        audit.apply_config()

    def _rank(self, name: str, **kw) -> int:
        return audit.judge(
            row(name, pr_number=1, pr_state="merged", **kw), alive=never_alive
        ).rank

    def test_defaults_protect_only_main(self, tmp_path, monkeypatch):
        self._apply(tmp_path, monkeypatch, '[project]\nlanguage = "en"\n')
        assert self._rank("main") == 1
        assert self._rank("entire/checkpoints/v1") == 6
        assert self._rank("x", dir_entries=(".port-registry",)) == 6

    def test_protected_from_config(self, tmp_path, monkeypatch):
        self._apply(
            tmp_path,
            monkeypatch,
            '[worktree_audit]\nprotected_refs = ["entire/*"]\n'
            'protected_paths = [".port-registry"]\n',
        )
        assert self._rank("main") == 1
        assert self._rank("entire/checkpoints/v1") == 1
        assert self._rank("x", dir_entries=(".port-registry",)) == 1

    def test_default_branch_from_config(self, tmp_path, monkeypatch):
        self._apply(tmp_path, monkeypatch, '[forge]\ndefault_branch = "trunk"\n')
        assert self._rank("trunk") == 1
        assert self._rank("main") == 6
        assert audit.DEFAULT_BRANCH == "trunk"

    def test_content_check_measures_against_the_default_branch(
        self, tmp_path, monkeypatch
    ):
        self._apply(tmp_path, monkeypatch, '[forge]\ndefault_branch = "trunk"\n')
        calls = []

        def fake_run_git(args, *, cwd=None):
            calls.append(args)
            return "0\n" if args[0] == "rev-list" else ""

        monkeypatch.setattr(audit, "run_git", fake_run_git)
        audit.probe_branch("feat-x", None, Path("/repo"))
        assert ["rev-list", "--count", "trunk..feat-x"] in calls
        assert ["diff", "--name-only", "trunk...feat-x"] in calls


def test_only_ends_on_a_fixed_cleanup_token() -> None:
    merged = row("issue-9-x", pr_number=9, pr_state="merged", remote=False)
    out, ok = audit.render_only([merged], "issue-9-x", cwd=Path("/elders"), alive=never_alive)
    assert ok is True
    assert "cleanup: yes" in out.splitlines()
    out, ok = audit.render_only([], "gone", cwd=Path("/elders"), alive=never_alive)
    assert out == "cleanup: no — branch gone not found\n"


def test_runs_as_installed_file(tmp_path) -> None:
    import subprocess

    result = subprocess.run(
        [sys.executable, str(SCRIPT), "--help"], capture_output=True, text=True, check=False
    )
    assert result.returncode == 0, result.stderr
    assert "cleanup: yes|no" in result.stdout
    assert "--issue" in result.stdout
