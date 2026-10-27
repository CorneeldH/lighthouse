"""Unit tests for `.lighthouse/scripts/measure_start_issue_kpi.py`, taken from paia.

The KPI of #663 is one number: how many tool calls sit between "pak 610 op" and the first
action that leads somewhere. That number is only worth something if the road to it is right, so
these tests pin the four places where a silent error tips the outcome:

- a tool_result is not a new human prompt (otherwise every lead-in is 0);
- subagent and harness records do not count (otherwise someone else's work counts);
- the count continues over consecutive assistant messages (otherwise it stops at the first);
- the route call itself does NOT count (exclusive), because the baseline has a minimum of 0.
"""

from __future__ import annotations

import importlib.util
import json
import os
import subprocess
import sys
from pathlib import Path

from tests.pack._testkit import GIT_ENV, INSTALL_ROOT, git, init_repo

SCRIPT = INSTALL_ROOT / ".lighthouse" / "scripts" / "measure_start_issue_kpi.py"
_SPEC = importlib.util.spec_from_file_location("measure_start_issue_kpi", SCRIPT)
assert _SPEC and _SPEC.loader
kpi = importlib.util.module_from_spec(_SPEC)
sys.modules[_SPEC.name] = kpi  # dataclasses looks the module up via __module__
_SPEC.loader.exec_module(kpi)


# --- fixture builders -------------------------------------------------------------------------


def user(text: str, **extra: object) -> dict:
    return {
        "type": "user",
        "isSidechain": False,
        "userType": "external",
        "message": {"content": [{"type": "text", "text": text}]},
        **extra,
    }


def tool_result() -> dict:
    """A tool return: same `type: user`, but no human prompt."""
    return {
        "type": "user",
        "isSidechain": False,
        "userType": "external",
        "message": {"content": [{"type": "tool_result", "tool_use_id": "t1"}]},
    }


def assistant(
    *tools: tuple[str, dict], text: str | None = None, **extra: object
) -> dict:
    blocks: list[dict] = []
    if text is not None:
        blocks.append({"type": "text", "text": text})
    blocks += [{"type": "tool_use", "name": n, "input": i} for n, i in tools]
    return {
        "type": "assistant",
        "isSidechain": False,
        "message": {"content": blocks},
        **extra,
    }


def bash(cmd: str = "gh api repos/x/y/issues/610") -> tuple[str, dict]:
    return ("Bash", {"command": cmd, "description": "lees issue"})


def write_session(directory, records) -> None:
    import json

    path = directory / "sessie.jsonl"
    path.write_text("\n".join(json.dumps(r) for r in records), encoding="utf-8")


# --- recognition of the pickup prompt ---------------------------------------------------------


def test_pickup_patterns_are_recognised() -> None:
    assert kpi.pickup_issue_number("pak 610 op") == 610
    assert (
        kpi.pickup_issue_number("pak issue #428 op, lees ook goed de comments") == 428
    )
    assert kpi.pickup_issue_number("begin aan issue 526") == 526
    assert kpi.pickup_issue_number("kijk eens naar 623") == 623
    assert kpi.pickup_issue_number("ga verder met #663") == 663


def test_non_pickup_prompts_are_not_counted() -> None:
    """The baseline counts only prompts that START the work on an issue."""
    assert kpi.pickup_issue_number("wat staat er in issue 610?") is None
    assert kpi.pickup_issue_number("sluit 610") is None
    assert kpi.pickup_issue_number("schrijf een issue over de gateway") is None
    assert kpi.pickup_issue_number("de rebase pakte 3 commits op") is None


def test_a_prompt_that_already_names_the_route() -> None:
    """The cohort that reaches the median of 1: the user routed by hand."""
    assert kpi.names_a_skill("pak 610 op /superpowers:writing-plans")
    assert kpi.names_a_skill("pak 610 op, gebruik /close-issue")
    assert not kpi.names_a_skill("pak 610 op")
    # An absolute path is not a slash command.
    assert not kpi.names_a_skill("kijk in /home/corneel/repositories voor 610")


# --- turn segmentation ------------------------------------------------------------------------


def test_a_tool_result_does_not_start_a_new_turn() -> None:
    records = [user("pak 610 op"), assistant(bash()), tool_result(), assistant(bash())]
    turns = list(kpi.iter_turns(records))
    assert len(turns) == 1
    assert len(turns[0].records) == 3


def test_sidechain_and_meta_records_are_ignored() -> None:
    records = [
        user("pak 610 op"),
        user("subagent-opdracht", isSidechain=True),
        assistant(bash(), isSidechain=True),
        {
            "type": "user",
            "isMeta": True,
            "message": {"content": [{"type": "text", "text": "hook"}]},
        },
        assistant(("Skill", {"skill": "superpowers:writing-plans"})),
    ]
    turns = list(kpi.iter_turns(records))
    assert len(turns) == 1, "een subagent-prompt is geen nieuwe mensbeurt"
    assert kpi.count_leadin(turns[0]) == 0, "de subagent-Bash telt niet mee"


# --- the lead-in count ------------------------------------------------------------------------


def test_skill_as_first_call_is_zero() -> None:
    turn = next(
        kpi.iter_turns([user("pak 610 op"), assistant(("Skill", {"skill": "x"}))])
    )
    assert kpi.count_leadin(turn) == 0


def test_counting_continues_across_consecutive_assistant_messages() -> None:
    records = [
        user("pak 610 op"),
        assistant(bash(), text="ik lees het issue"),
        tool_result(),
        assistant(bash(), bash()),
        tool_result(),
        assistant(("Write", {"file_path": "/tmp/x.md", "content": "x"})),
    ]
    turn = next(kpi.iter_turns(records))
    assert kpi.count_leadin(turn) == 3


def test_the_route_call_itself_is_not_counted() -> None:
    """Count exclusively. The baseline has a minimum of 0, and the target <= 2 is 'two
    gh-api calls and only then the Skill' — counting inclusively would shift both."""
    turn = next(
        kpi.iter_turns(
            [user("pak 610 op"), assistant(bash(), ("Skill", {"skill": "x"}))]
        )
    )
    assert kpi.count_leadin(turn) == 1


def test_a_turn_without_any_route_returns_none() -> None:
    turn = next(kpi.iter_turns([user("pak 610 op"), assistant(bash(), bash())]))
    assert kpi.count_leadin(turn) is None


def test_own_skill_load_is_skipped_but_not_counted() -> None:
    """The skill that loads itself ambiently is the first tool_use of the turn but no
    route call and no lead-in — counting it would make the metric succeed by construction."""
    self_load = ("Skill", {"skill": kpi.SKILL_NAME})
    records = [
        user("pak 610 op"),
        assistant(
            self_load,
            bash(),
            bash(),
            ("Write", {"file_path": "/tmp/x.md", "content": "x"}),
        ),
    ]
    turn = next(kpi.iter_turns(records))
    assert kpi.count_leadin(turn) == 2

    other_skill_turn = next(
        kpi.iter_turns(
            [user("pak 610 op"), assistant(("Skill", {"skill": "andere-skill"}))]
        )
    )
    assert kpi.count_leadin(other_skill_turn) == 0


def _route_split_records() -> list[dict]:
    return [
        assistant(("Skill", {"skill": kpi.SKILL_NAME})),
        assistant(("Skill", {"skill": kpi.ROUTE_SKILL_NAME})),
        assistant(
            ("Skill", {"skill": "superpowers:writing-plans"}),
            text="Poort 4b: writing-plans",
        ),
    ]


def test_route_skill_is_skipped_like_the_start_skill() -> None:
    """After the split (#1115) the gate announcement comes only after Skill{lighthouse-route-issue}; that call is
    the routing itself, no route and no lead-in."""
    turn = _gate_turn(*_route_split_records())
    assert kpi.announced_gate(turn) == 4
    assert kpi.count_leadin(turn) == 0
    assert kpi.announced_subgate(turn) == "4b"


def test_measure_gate_counts_a_gate_reported_after_the_route_skill(tmp_path) -> None:
    """Without the exclusion of lighthouse-route-issue every pickup after the split counts as
    'not reported': the announcement comes only after that Skill call."""
    write_session(tmp_path, [user("pak 610 op"), *_route_split_records()])

    result = kpi.measure_gate(tmp_path, None)
    assert result["reported"] == 1
    assert result["not_reported"] == 0


def test_scout_call_is_skipped_but_not_counted() -> None:
    """The mandatory scout call on 4b/4c is the pre-exploration itself, not the lead-in to it —
    same argument as the ambient Skill call above (issue #927 fix 2)."""
    scout_call = ("Agent", {"subagent_type": kpi.SCOUT_AGENT, "prompt": "..."})
    records = [
        user("pak 610 op"),
        assistant(
            scout_call,
            bash(),
            bash(),
            ("Write", {"file_path": "/tmp/x.md", "content": "x"}),
        ),
    ]
    turn = next(kpi.iter_turns(records))
    assert kpi.count_leadin(turn) == 2


# --- worktree-first ---------------------------------------------------------------------------


def test_first_write_path_and_worktree_check() -> None:
    records = [
        user("pak 610 op"),
        assistant(bash()),
        assistant(
            ("Edit", {"file_path": "/home/x/repo/.claude/worktrees/issue-610/a.md"})
        ),
        assistant(("Write", {"file_path": "/home/x/repo/b.md"})),
    ]
    turn = next(kpi.iter_turns(records))
    path = kpi.first_write_path(turn)
    assert path is not None and kpi.is_worktree_path(path)
    assert not kpi.is_worktree_path("/home/x/repo/b.md")


def test_a_turn_without_write_tools_has_no_write_path() -> None:
    """Bash writes (`sed -i`, heredoc) are invisible by construction — the limit of
    this KPI, pinned explicitly so nobody later mistakes it for complete."""
    turn = next(
        kpi.iter_turns([user("pak 610 op"), assistant(bash("sed -i s/a/b/ x.md"))])
    )
    assert kpi.first_write_path(turn) is None


# --- mis-trigger ------------------------------------------------------------------------------


def test_mistrigger_needs_a_loaded_skill_without_an_issue_number() -> None:
    loaded = [
        user("schrijf een issue over de gateway"),
        assistant(("Skill", {"skill": kpi.SKILL_NAME})),
    ]
    turn = next(kpi.iter_turns(loaded))
    assert kpi.SKILL_NAME in kpi.loaded_skills(turn)
    assert not kpi.has_issue_number(turn.prompt)

    fine = [user("pak 610 op"), assistant(("Skill", {"skill": kpi.SKILL_NAME}))]
    assert kpi.has_issue_number(next(kpi.iter_turns(fine)).prompt)


# --- gate announcement ------------------------------------------------------------------------


def _gate_turn(*records: dict) -> object:
    return next(kpi.iter_turns([user("pak 610 op"), *records]))


def test_gate_is_read_from_text_before_the_route_call() -> None:
    turn = _gate_turn(
        assistant(("Skill", {"skill": kpi.SKILL_NAME})),
        assistant(bash()),
        assistant(
            ("Skill", {"skill": "superpowers:writing-plans"}),
            text="Poort 4: b — de AC leggen het HOE vast.",
        ),
    )
    assert kpi.announced_gate(turn) == 4


def test_gate_text_may_sit_in_an_earlier_message() -> None:
    turn = _gate_turn(
        assistant(bash(), text="Poort 2: systematic-debugging"),
        assistant(("Skill", {"skill": "superpowers:systematic-debugging"})),
    )
    assert kpi.announced_gate(turn) == 2


def test_text_after_the_route_call_does_not_count() -> None:
    """The announcement belongs before the routing. Afterwards it is an account, not a routing —
    and without this boundary every later mention of the word counts."""
    turn = _gate_turn(
        assistant(("Skill", {"skill": "superpowers:writing-plans"})),
        assistant(bash(), text="dit was Poort 4"),
    )
    assert kpi.announced_gate(turn) is None


def test_a_stopping_gate_needs_no_route_call() -> None:
    """Gate 0 stops: no more `Skill`/`Write` follows. Without a route call there is no boundary and
    the whole turn counts, otherwise the one gate that does nothing would never be measured."""
    turn = _gate_turn(
        assistant(("Skill", {"skill": kpi.SKILL_NAME})),
        assistant(bash()),
        assistant(text="Poort 0: gestopt, 3-blocked-issue wacht op #612."),
    )
    assert kpi.announced_gate(turn) == 0


def test_a_silent_route_reports_no_gate() -> None:
    turn = _gate_turn(
        assistant(bash(), text="ik lees het issue"),
        assistant(("Skill", {"skill": "superpowers:writing-plans"})),
    )
    assert kpi.announced_gate(turn) is None


def test_only_zero_through_four_are_gates() -> None:
    turn = _gate_turn(assistant(text="Poort 7: bestaat niet"))
    assert kpi.announced_gate(turn) is None


def test_measure_gate_splits_reported_unreported_and_skill_less(tmp_path) -> None:
    """The denominator is narrow: a pickup without the skill is a different failure mode (rows 1 and 3) and
    does not belong in this percentage."""
    import json

    records = [
        user("pak 610 op"),
        assistant(("Skill", {"skill": kpi.SKILL_NAME}), text="Poort 2: debuggen"),
        assistant(("Skill", {"skill": "superpowers:systematic-debugging"})),
        user("pak 611 op"),
        assistant(("Skill", {"skill": kpi.SKILL_NAME})),
        assistant(("Skill", {"skill": "superpowers:writing-plans"})),
        user("pak 612 op"),
        assistant(bash()),
    ]
    session = tmp_path / "sessie.jsonl"
    session.write_text(
        "\n".join(json.dumps(r) for r in records) + "\n", encoding="utf-8"
    )

    result = kpi.measure_gate(tmp_path, None)
    assert result["reported"] == 1
    assert result["not_reported"] == 1
    assert result["share"] == 0.5
    assert result["distribution"] == {"gate_2": 1}
    assert result["pickup_without_skill"] == 1


# --- scout (issue-impact-scout) ---------------------------------------------------------------


def assistant_with_scout(
    text: str, uid: str = "s1", stamp: str = "2026-09-11T10:00:00Z"
) -> dict:
    record = assistant(
        ("Agent", {"subagent_type": kpi.SCOUT_AGENT, "prompt": "..."}),
        text=text,
    )
    record["timestamp"] = stamp
    for block in record["message"]["content"]:
        if block.get("type") == "tool_use":
            block["id"] = uid
    return record


def scout_result(
    uid: str = "s1",
    stamp: str = "2026-09-11T10:01:30Z",
    tool_calls: int | None = None,
) -> dict:
    record = tool_result()
    record["message"]["content"][0]["tool_use_id"] = uid
    record["timestamp"] = stamp
    if tool_calls is not None:
        record["toolUseResult"] = {
            "agentType": kpi.SCOUT_AGENT,
            "totalToolUseCount": tool_calls,
        }
    return record


def test_a_scout_call_on_gate_four_b_counts_as_triggered(tmp_path) -> None:
    records = [
        user("pak 610 op"),
        assistant(("Skill", {"skill": kpi.SKILL_NAME})),
        assistant_with_scout("Poort 4b: writing-plans\nScout: issue-impact-scout"),
        scout_result(),
    ]
    write_session(tmp_path, records)
    result = kpi.measure_scout(tmp_path, None)
    assert result["triggered"] == 1
    assert result["silent"] == 0
    assert result["share"] == 1.0


def test_a_concrete_skip_reason_counts_as_compliant(tmp_path) -> None:
    records = [
        user("pak 611 op"),
        assistant(("Skill", {"skill": kpi.SKILL_NAME})),
        assistant(
            ("Write", {"file_path": "/x/.claude/worktrees/w/docs/a.md"}),
            text=(
                "Poort 4c: brainstorming\n"
                "Scout: overgeslagen — spec noemt de drie bestanden, het patroon en pytest-pad."
            ),
        ),
    ]
    write_session(tmp_path, records)
    result = kpi.measure_scout(tmp_path, None)
    assert result["skipped_with_reason"] == 1
    assert result["silent"] == 0
    assert result["share"] == 1.0


def test_gate_four_b_without_any_scout_line_is_silent(tmp_path) -> None:
    records = [
        user("pak 612 op"),
        assistant(("Skill", {"skill": kpi.SKILL_NAME})),
        assistant(("Write", {"file_path": "/x/y.md"}), text="Poort 4b: writing-plans"),
    ]
    write_session(tmp_path, records)
    result = kpi.measure_scout(tmp_path, None)
    assert result["silent"] == 1
    assert result["share"] == 0.0


def test_an_empty_skip_reason_is_not_a_reason(tmp_path) -> None:
    records = [
        user("pak 613 op"),
        assistant(("Skill", {"skill": kpi.SKILL_NAME})),
        assistant(
            ("Write", {"file_path": "/x/y.md"}),
            text="Poort 4b: writing-plans\nScout: overgeslagen — .",
        ),
    ]
    write_session(tmp_path, records)
    assert kpi.measure_scout(tmp_path, None)["silent"] == 1


def test_a_scout_call_on_gate_two_is_a_mistrigger(tmp_path) -> None:
    records = [
        user("pak 614 op"),
        assistant(("Skill", {"skill": kpi.SKILL_NAME})),
        assistant_with_scout(
            "Poort 2: systematic-debugging\nScout: issue-impact-scout"
        ),
        scout_result(),
    ]
    write_session(tmp_path, records)
    result = kpi.measure_scout(tmp_path, None)
    assert result["mistrigger"] == 1
    assert result["triggered"] == 0, "poort 2 hoort niet in de noemer van de 4b/4c-rij"


def test_runtime_comes_from_the_timestamp_pair(tmp_path) -> None:
    records = [
        user("pak 615 op"),
        assistant(("Skill", {"skill": kpi.SKILL_NAME})),
        assistant_with_scout(
            "Poort 4b: writing-plans\nScout: issue-impact-scout",
            stamp="2026-09-11T10:00:00Z",
        ),
        scout_result(stamp="2026-09-11T10:01:30Z"),
    ]
    write_session(tmp_path, records)
    runtime = kpi.measure_scout(tmp_path, None)["runtime_seconds"]
    assert runtime["median"] == 90.0
    assert runtime["n"] == 1


def test_a_call_without_a_result_has_no_runtime(tmp_path) -> None:
    """An aborted run must not pull the median down to zero."""
    records = [
        user("pak 616 op"),
        assistant(("Skill", {"skill": kpi.SKILL_NAME})),
        assistant_with_scout("Poort 4b: writing-plans\nScout: issue-impact-scout"),
    ]
    write_session(tmp_path, records)
    result = kpi.measure_scout(tmp_path, None)
    assert result["triggered"] == 1
    assert result["runtime_seconds"]["n"] == 0


def test_a_bare_gate_four_without_subletter_is_unknown_not_mistrigger(tmp_path) -> None:
    """A bare `Poort 4` (without sub-letter) is unknown, not a mis-trigger — the gate format error
    that without this fix counted a correct 4b/4c run as a mistrigger and flipped the sign of the metric
    (issue #927 fix 1). Such a turn counts nowhere, not even in the denominator."""
    records = [
        user("pak 617 op"),
        assistant(("Skill", {"skill": kpi.SKILL_NAME})),
        assistant_with_scout("Poort 4: writing-plans\nScout: issue-impact-scout"),
        scout_result(),
    ]
    write_session(tmp_path, records)
    result = kpi.measure_scout(tmp_path, None)
    assert result["mistrigger"] == 0
    assert result["triggered"] == 0
    assert result["share"] is None


def test_tool_calls_come_from_the_agent_result(tmp_path) -> None:
    """Seventh field: runtime says nothing about how widely it searched, `totalToolUseCount` does.
    The smoke run on #912 made 15 — NOT an overrun of `maxTurns: 12`, because that ceiling
    counts turns and one turn carries several parallel calls."""
    records = [
        user("pak 618 op"),
        assistant(("Skill", {"skill": kpi.SKILL_NAME})),
        assistant_with_scout("Poort 4b: writing-plans\nScout: issue-impact-scout"),
        scout_result(tool_calls=15),
    ]
    write_session(tmp_path, records)
    tool_calls = kpi.measure_scout(tmp_path, None)["tool_calls"]
    assert tool_calls == {"n": 1, "median": 15, "max": 15}


def test_a_result_without_tool_stats_is_not_counted_as_zero(tmp_path) -> None:
    """An older or aborted run lacks the field; zero tool calls would falsify the median."""
    records = [
        user("pak 619 op"),
        assistant(("Skill", {"skill": kpi.SKILL_NAME})),
        assistant_with_scout("Poort 4b: writing-plans\nScout: issue-impact-scout"),
        scout_result(),
    ]
    write_session(tmp_path, records)
    result = kpi.measure_scout(tmp_path, None)
    assert result["triggered"] == 1
    assert result["tool_calls"] == {"n": 0, "median": None, "max": None}


def test_a_run_over_the_search_budget_is_counted(tmp_path) -> None:
    """The budget from the persona is there as a search-action limit; here it becomes measurable. Without this
    counter the prompt norm is decoration — `maxTurns` counts turns and does not see it."""
    records = [
        user("pak 620 op"),
        assistant(("Skill", {"skill": kpi.SKILL_NAME})),
        assistant_with_scout("Poort 4b: writing-plans\nScout: issue-impact-scout"),
        scout_result(tool_calls=kpi.SCOUT_TOOL_CALL_BUDGET + 5),
    ]
    write_session(tmp_path, records)
    result = kpi.measure_scout(tmp_path, None)
    assert result["over_budget"] == 1


def test_a_run_on_the_budget_is_not_over_it(tmp_path) -> None:
    records = [
        user("pak 621 op"),
        assistant(("Skill", {"skill": kpi.SKILL_NAME})),
        assistant_with_scout("Poort 4b: writing-plans\nScout: issue-impact-scout"),
        scout_result(tool_calls=kpi.SCOUT_TOOL_CALL_BUDGET),
    ]
    write_session(tmp_path, records)
    assert kpi.measure_scout(tmp_path, None)["over_budget"] == 0


# --- pilot #937: three measurement gaps -------------------------------------------------------


def test_worktree_sessions_are_read_next_to_the_main_directory(tmp_path) -> None:
    """A session that starts in a worktree lands in `<dir>--claude-worktrees-<name>/`, not in
    the main directory. Without those sibling directories `scout` saw 0 of the ~35 real runs (#937)."""
    main = tmp_path / "-home-x-repo"
    sibling = tmp_path / "-home-x-repo--claude-worktrees-issue-610-iets"
    stranger = tmp_path / "-home-x-repo-ander"
    for directory in (main, sibling, stranger):
        directory.mkdir()
    records = [
        user("pak 610 op"),
        assistant(("Skill", {"skill": kpi.SKILL_NAME})),
        assistant_with_scout("Poort 4b: writing-plans\nScout: issue-impact-scout"),
        scout_result(),
    ]
    write_session(sibling, records)
    write_session(stranger, records)

    assert kpi.measure_scout(main, None)["triggered"] == 1, "andere repo telt niet mee"


def test_the_worktree_skill_is_not_the_route_boundary() -> None:
    """Step 3 of the skill puts the worktree before the route, so `using-git-worktrees` is always
    the first Skill call after the ambient load. As a boundary it would swallow every gate announcement after it."""
    turn = _gate_turn(
        assistant(("Skill", {"skill": kpi.SKILL_NAME})),
        assistant(("Skill", {"skill": "superpowers:using-git-worktrees"})),
        assistant(bash("git worktree add .claude/worktrees/x")),
        assistant(
            ("Skill", {"skill": "superpowers:writing-plans"}),
            text="Poort 4b: writing-plans",
        ),
    )
    assert kpi.announced_subgate(turn) == "4b"


def test_a_scout_call_after_a_silent_route_is_unknown_not_mistrigger(tmp_path) -> None:
    """No gate announcement is no evidence for gate 0/1/2/4a. Such a run is unknown — same
    rule as the bare `Poort 4` — but it is counted, otherwise it disappears from view."""
    records = [
        user("pak 618 op"),
        assistant(("Skill", {"skill": kpi.SKILL_NAME})),
        assistant_with_scout("Worktree staat, scout parallel."),
        scout_result(),
    ]
    write_session(tmp_path, records)
    result = kpi.measure_scout(tmp_path, None)
    assert result["mistrigger"] == 0
    assert result["triggered"] == 0
    assert result["gate_unknown"] == 1


def test_an_async_launch_or_rejection_is_not_a_finished_run(tmp_path) -> None:
    """A scout in the background returns immediately `status: async_launched`; a rejected
    call returns a string. Neither is a finished run: their 1-10 s pulled the runtime
    median down (#937). The trigger does count — the skill fired it."""
    launched = scout_result(uid="s1", stamp="2026-09-11T10:00:02Z")
    launched["toolUseResult"] = {"isAsync": True, "status": "async_launched"}
    rejected = scout_result(uid="s2", stamp="2026-09-11T11:00:10Z")
    rejected["toolUseResult"] = "User rejected tool use"
    records = [
        user("pak 619 op"),
        assistant(("Skill", {"skill": kpi.SKILL_NAME})),
        assistant_with_scout("Poort 4c: brainstorming", uid="s1"),
        launched,
        user("pak 620 op"),
        assistant(("Skill", {"skill": kpi.SKILL_NAME})),
        assistant_with_scout(
            "Poort 4b: writing-plans", uid="s2", stamp="2026-09-11T11:00:00Z"
        ),
        rejected,
    ]
    write_session(tmp_path, records)
    result = kpi.measure_scout(tmp_path, None)
    assert result["triggered"] == 2
    assert result["runtime_seconds"]["n"] == 0


# --- lighthouse: the transcript directory, --since, both languages ----------------------------


def test_transcript_dir_from_git_root(tmp_path, monkeypatch) -> None:
    """The project directory under `~/.claude/projects/` is the repo root with every character
    outside `[A-Za-z0-9]` replaced by `-`, as Claude Code names it."""
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.setenv("LIGHTHOUSE_REPO_ROOT", "/srv/my_repo.d")
    expected = tmp_path / ".claude" / "projects" / "-srv-my-repo-d"
    assert kpi.default_transcript_dir() == expected

    expected.mkdir(parents=True)
    write_session(expected, [user("pak 610 op"), assistant(("Skill", {"skill": kpi.SKILL_NAME}))])
    assert kpi.main(["mistrigger"]) == 0


def test_missing_transcript_dir_is_rc_2(tmp_path, capsys) -> None:
    assert kpi.main(["leadin", "--dir", str(tmp_path / "gone")]) == 2
    assert "transcript directory does not exist" in capsys.readouterr().err


def _commit_file(repo: Path, rel: str, date: str) -> None:
    path = repo / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("x\n", encoding="utf-8")
    subprocess.run(
        ["git", "-C", str(repo), "add", rel], check=True, capture_output=True
    )
    subprocess.run(
        ["git", "-C", str(repo), "commit", "-qm", rel],
        check=True,
        capture_output=True,
        env=dict(os.environ, **GIT_ENV, GIT_AUTHOR_DATE=date, GIT_COMMITTER_DATE=date),
    )


def test_since_option(tmp_path, monkeypatch) -> None:
    """Without `--since` the whole history counts; there is no built-in convention date."""
    repo = tmp_path / "repo"
    init_repo(repo)
    _commit_file(repo, "docs/superpowers/plans/old.md", "2026-01-01T12:00:00Z")
    _commit_file(repo, "docs/developer/planning/specs/new.md", "2026-09-01T12:00:00Z")
    monkeypatch.setenv("LIGHTHOUSE_REPO_ROOT", str(repo))
    config = tmp_path / "lighthouse.toml"
    config.write_text('[project]\nlanguage = "en"\n', encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(config))

    whole = kpi.measure_paths(None)
    assert (whole["since"], whole["right_dir"], whole["superpowers_default_dir"]) == (None, 1, 1)
    window = kpi.measure_paths("2026-08-01")
    assert (window["since"], window["right_dir"], window["superpowers_default_dir"]) == (
        "2026-08-01",
        1,
        0,
    )


def test_paths_dirs_from_config(tmp_path, monkeypatch) -> None:
    repo = tmp_path / "repo"
    init_repo(repo)
    _commit_file(repo, "s/a.md", "2026-09-01T12:00:00Z")
    _commit_file(repo, "docs/developer/planning/specs/b.md", "2026-09-01T12:00:00Z")
    monkeypatch.setenv("LIGHTHOUSE_REPO_ROOT", str(repo))
    config = tmp_path / "lighthouse.toml"
    config.write_text('[paths]\nspecs = "s"\nplans = "p/"\n', encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(config))
    assert kpi.measure_paths(None)["right_dir"] == 1


def test_pickup_verbs_both_languages() -> None:
    assert kpi.pickup_issue_number("pick up 610") == 610
    assert kpi.pickup_issue_number("pick up issue #428, read the comments too") == 428
    assert kpi.pickup_issue_number("pick 610 up") == 610
    assert kpi.pickup_issue_number("start on issue 526") == 526
    assert kpi.pickup_issue_number("work on #663") == 663
    assert kpi.pickup_issue_number("continue with 663") == 663
    assert kpi.pickup_issue_number("look at 623") == 623
    assert kpi.pickup_issue_number("pak 610 op") == 610
    assert kpi.pickup_issue_number("what is in issue 610?") is None
    assert kpi.pickup_issue_number("close 610") is None


def test_gate_line_any_language() -> None:
    english = _gate_turn(
        assistant(("Skill", {"skill": kpi.SKILL_NAME})),
        assistant(("Skill", {"skill": "superpowers:writing-plans"}), text="Gate 4b: writing-plans"),
    )
    assert kpi.announced_gate(english) == 4
    assert kpi.announced_subgate(english) == "4b"
    dutch = _gate_turn(assistant(bash(), text="Poort 2: systematic-debugging"))
    assert kpi.announced_gate(dutch) == 2


def test_scout_skip_any_language(tmp_path) -> None:
    records = [
        user("pick up 611"),
        assistant(("Skill", {"skill": kpi.SKILL_NAME})),
        assistant(
            ("Write", {"file_path": "/x/.claude/worktrees/w/docs/a.md"}),
            text=(
                "Gate 4c: brainstorming\n"
                "Scout: skipped — the spec names the three files, the pattern and the test path."
            ),
        ),
    ]
    write_session(tmp_path, records)
    assert kpi.measure_scout(tmp_path, None)["skipped_with_reason"] == 1


def test_skill_names_are_lighthouse() -> None:
    assert kpi.SKILL_NAME == "lighthouse-start-issue"
    assert kpi.ROUTE_SKILL_NAME == "lighthouse-route-issue"
    assert kpi.SCOUT_AGENT == "lighthouse-issue-impact-scout"


def test_help_is_english(capsys) -> None:
    import pytest

    with pytest.raises(SystemExit):
        kpi.main(["--help"])
    out = capsys.readouterr().out
    assert "Review measurement for the skill lighthouse-start-issue" in out
    assert "KPI-meting" not in out


def test_git_failure_is_rc_2_without_traceback(tmp_path, monkeypatch, capsys) -> None:
    """A repo without commits: `git log` fails. That is a loud rc 2, not a traceback."""
    repo = tmp_path / "repo"
    repo.mkdir()
    subprocess.run(["git", "init", "-q", str(repo)], check=True)
    monkeypatch.setenv("LIGHTHOUSE_REPO_ROOT", str(repo))
    config = tmp_path / "lighthouse.toml"
    config.write_text("", encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(config))
    transcripts = tmp_path / "t"
    transcripts.mkdir()
    assert kpi.main(["paths", "--dir", str(transcripts)]) == 2
    assert capsys.readouterr().err.startswith("measure: git log failed: ")
