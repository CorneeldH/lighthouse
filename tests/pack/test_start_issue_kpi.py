"""Unit tests voor scripts/measure_start_issue_kpi.py (issue #663).

De KPI van #663 is één getal: hoeveel tool-calls zitten er tussen "pak 610 op" en de eerste
handeling die ergens heen leidt. Dat getal is alleen iets waard als de weg ernaartoe klopt, dus
deze tests pinnen de vier plekken waar een stille fout de uitkomst kantelt:

- een tool_result is geen nieuwe mensprompt (anders is elke lead-in 0);
- subagent- en harness-records tellen niet mee (anders telt andermans werk mee);
- de telling loopt door over opeenvolgende assistant-berichten (anders stopt hij bij het eerste);
- de route-call zelf telt NIET mee (exclusief), want de nulmeting kent een minimum van 0.
"""

from __future__ import annotations

import importlib.util
import sys
from pathlib import Path

_SPEC = importlib.util.spec_from_file_location(
    "measure_start_issue_kpi",
    Path(__file__).resolve().parents[2] / "scripts" / "measure_start_issue_kpi.py",
)
assert _SPEC and _SPEC.loader
kpi = importlib.util.module_from_spec(_SPEC)
sys.modules[_SPEC.name] = kpi  # dataclasses zoekt de module op via __module__
_SPEC.loader.exec_module(kpi)


# --- fixturebouwers ---------------------------------------------------------------------------


def user(text: str, **extra: object) -> dict:
    return {
        "type": "user",
        "isSidechain": False,
        "userType": "external",
        "message": {"content": [{"type": "text", "text": text}]},
        **extra,
    }


def tool_result() -> dict:
    """Een tool-retour: zelfde `type: user`, maar geen mensprompt."""
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


# --- herkenning van de pickup-prompt ----------------------------------------------------------


def test_pickup_patterns_are_recognised() -> None:
    assert kpi.pickup_issue_number("pak 610 op") == 610
    assert (
        kpi.pickup_issue_number("pak issue #428 op, lees ook goed de comments") == 428
    )
    assert kpi.pickup_issue_number("begin aan issue 526") == 526
    assert kpi.pickup_issue_number("kijk eens naar 623") == 623
    assert kpi.pickup_issue_number("ga verder met #663") == 663


def test_non_pickup_prompts_are_not_counted() -> None:
    """De nulmeting telt alleen prompts die het werk aan een issue STARTEN."""
    assert kpi.pickup_issue_number("wat staat er in issue 610?") is None
    assert kpi.pickup_issue_number("sluit 610") is None
    assert kpi.pickup_issue_number("schrijf een issue over de gateway") is None
    assert kpi.pickup_issue_number("de rebase pakte 3 commits op") is None


def test_a_prompt_that_already_names_the_route() -> None:
    """Het cohort dat de mediaan van 1 haalt: de gebruiker routeerde met de hand."""
    assert kpi.names_a_skill("pak 610 op /superpowers:writing-plans")
    assert kpi.names_a_skill("pak 610 op, gebruik /close-issue")
    assert not kpi.names_a_skill("pak 610 op")
    # Een absoluut pad is geen slash-commando.
    assert not kpi.names_a_skill("kijk in /home/corneel/repositories voor 610")


# --- beurtsegmentatie -------------------------------------------------------------------------


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


# --- de lead-in-telling -----------------------------------------------------------------------


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
    """Exclusief tellen. De nulmeting kent een minimum van 0, en het doel <= 2 is 'twee
    gh-api-calls en dan pas de Skill' — inclusief tellen zou beide verschuiven."""
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
    """De skill die zichzelf ambient laadt, is de eerste tool_use van de beurt maar geen
    route-call en geen aanloop — meetellen zou de metric per constructie laten slagen."""
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
    """Na de split (#1115) komt de poortmelding pas na Skill{paia-route-issue}; die call is
    het routeren zelf, geen route en geen aanloop."""
    turn = _gate_turn(*_route_split_records())
    assert kpi.announced_gate(turn) == 4
    assert kpi.count_leadin(turn) == 0
    assert kpi.announced_subgate(turn) == "4b"


def test_measure_gate_counts_a_gate_reported_after_the_route_skill(tmp_path) -> None:
    """Zonder de uitsluiting van paia-route-issue telt elke pickup ná de split als
    'niet gemeld': de melding staat pas na die Skill-call."""
    write_session(tmp_path, [user("pak 610 op"), *_route_split_records()])

    result = kpi.measure_gate(tmp_path, None)
    assert result["gemeld"] == 1
    assert result["niet_gemeld"] == 0


def test_scout_call_is_skipped_but_not_counted() -> None:
    """De verplichte scoutcall op 4b/4c is de voorverkenning zelf, geen aanloop ernaartoe —
    zelfde argument als de ambient Skill-call hierboven (issue #927 fix 2)."""
    scout_call = ("Agent", {"subagent_type": "issue-impact-scout", "prompt": "..."})
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
    """Bash-schrijfacties (`sed -i`, heredoc) zijn per constructie onzichtbaar — de grens van
    deze KPI, expliciet vastgepind zodat niemand hem later voor volledig aanziet."""
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


# --- poortmelding -----------------------------------------------------------------------------


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
    """De melding hoort vóór het doorroeren. Achteraf is het een verantwoording, geen routering —
    en zonder deze grens telt elke latere vermelding van het woord mee."""
    turn = _gate_turn(
        assistant(("Skill", {"skill": "superpowers:writing-plans"})),
        assistant(bash(), text="dit was Poort 4"),
    )
    assert kpi.announced_gate(turn) is None


def test_a_stopping_gate_needs_no_route_call() -> None:
    """Poort 0 stopt: er komt geen `Skill`/`Write` meer. Zonder route-call is er geen grens en
    telt de hele beurt, anders zou de enige poort die niets doet nooit gemeten worden."""
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
    """De noemer is smal: een pickup zonder de skill is een andere faalmodus (rij 1 en 3) en
    hoort niet in dit percentage."""
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
    assert result["gemeld"] == 1
    assert result["niet_gemeld"] == 1
    assert result["aandeel"] == 0.5
    assert result["verdeling"] == {"poort_2": 1}
    assert result["pickup_zonder_skill"] == 1


# --- scout (issue-impact-scout) ---------------------------------------------------------------


def assistant_with_scout(
    text: str, uid: str = "s1", stamp: str = "2026-09-11T10:00:00Z"
) -> dict:
    record = assistant(
        ("Agent", {"subagent_type": "issue-impact-scout", "prompt": "..."}),
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
            "agentType": "issue-impact-scout",
            "totalToolUseCount": tool_calls,
        }
    return record


def test_a_scout_call_on_gate_four_b_counts_as_triggered(tmp_path) -> None:
    records = [
        user("pak 610 op"),
        assistant(("Skill", {"skill": "paia-start-issue"})),
        assistant_with_scout("Poort 4b: writing-plans\nScout: issue-impact-scout"),
        scout_result(),
    ]
    write_session(tmp_path, records)
    result = kpi.measure_scout(tmp_path, None)
    assert result["getriggerd"] == 1
    assert result["stil"] == 0
    assert result["aandeel"] == 1.0


def test_a_concrete_skip_reason_counts_as_compliant(tmp_path) -> None:
    records = [
        user("pak 611 op"),
        assistant(("Skill", {"skill": "paia-start-issue"})),
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
    assert result["overgeslagen_met_reden"] == 1
    assert result["stil"] == 0
    assert result["aandeel"] == 1.0


def test_gate_four_b_without_any_scout_line_is_silent(tmp_path) -> None:
    records = [
        user("pak 612 op"),
        assistant(("Skill", {"skill": "paia-start-issue"})),
        assistant(("Write", {"file_path": "/x/y.md"}), text="Poort 4b: writing-plans"),
    ]
    write_session(tmp_path, records)
    result = kpi.measure_scout(tmp_path, None)
    assert result["stil"] == 1
    assert result["aandeel"] == 0.0


def test_an_empty_skip_reason_is_not_a_reason(tmp_path) -> None:
    records = [
        user("pak 613 op"),
        assistant(("Skill", {"skill": "paia-start-issue"})),
        assistant(
            ("Write", {"file_path": "/x/y.md"}),
            text="Poort 4b: writing-plans\nScout: overgeslagen — .",
        ),
    ]
    write_session(tmp_path, records)
    assert kpi.measure_scout(tmp_path, None)["stil"] == 1


def test_a_scout_call_on_gate_two_is_a_mistrigger(tmp_path) -> None:
    records = [
        user("pak 614 op"),
        assistant(("Skill", {"skill": "paia-start-issue"})),
        assistant_with_scout(
            "Poort 2: systematic-debugging\nScout: issue-impact-scout"
        ),
        scout_result(),
    ]
    write_session(tmp_path, records)
    result = kpi.measure_scout(tmp_path, None)
    assert result["mistrigger"] == 1
    assert result["getriggerd"] == 0, "poort 2 hoort niet in de noemer van de 4b/4c-rij"


def test_runtime_comes_from_the_timestamp_pair(tmp_path) -> None:
    records = [
        user("pak 615 op"),
        assistant(("Skill", {"skill": "paia-start-issue"})),
        assistant_with_scout(
            "Poort 4b: writing-plans\nScout: issue-impact-scout",
            stamp="2026-09-11T10:00:00Z",
        ),
        scout_result(stamp="2026-09-11T10:01:30Z"),
    ]
    write_session(tmp_path, records)
    looptijd = kpi.measure_scout(tmp_path, None)["looptijd_seconden"]
    assert looptijd["mediaan"] == 90.0
    assert looptijd["n"] == 1


def test_a_call_without_a_result_has_no_runtime(tmp_path) -> None:
    """Een afgebroken run mag de mediaan niet naar nul trekken."""
    records = [
        user("pak 616 op"),
        assistant(("Skill", {"skill": "paia-start-issue"})),
        assistant_with_scout("Poort 4b: writing-plans\nScout: issue-impact-scout"),
    ]
    write_session(tmp_path, records)
    result = kpi.measure_scout(tmp_path, None)
    assert result["getriggerd"] == 1
    assert result["looptijd_seconden"]["n"] == 0


def test_a_bare_gate_four_without_subletter_is_unknown_not_mistrigger(tmp_path) -> None:
    """Een kale `Poort 4` (zonder subletter) is onbekend, geen mis-trigger — de gate-formatfout
    die zonder deze fix een correcte 4b/4c-run als mistrigger telde en het teken van de metriek
    omkeerde (issue #927 fix 1). Zo'n beurt telt nergens in mee, ook niet in de noemer."""
    records = [
        user("pak 617 op"),
        assistant(("Skill", {"skill": "paia-start-issue"})),
        assistant_with_scout("Poort 4: writing-plans\nScout: issue-impact-scout"),
        scout_result(),
    ]
    write_session(tmp_path, records)
    result = kpi.measure_scout(tmp_path, None)
    assert result["mistrigger"] == 0
    assert result["getriggerd"] == 0
    assert result["aandeel"] is None


def test_tool_calls_come_from_the_agent_result(tmp_path) -> None:
    """Zevende veld: looptijd zegt niets over hoe breed er gezocht is, `totalToolUseCount` wel.
    De smoke-run op #912 deed er 15 — géén overschrijding van `maxTurns: 12`, want dat plafond
    telt beurten en één beurt draagt meerdere parallelle calls."""
    records = [
        user("pak 618 op"),
        assistant(("Skill", {"skill": "paia-start-issue"})),
        assistant_with_scout("Poort 4b: writing-plans\nScout: issue-impact-scout"),
        scout_result(tool_calls=15),
    ]
    write_session(tmp_path, records)
    tool_calls = kpi.measure_scout(tmp_path, None)["tool_calls"]
    assert tool_calls == {"n": 1, "mediaan": 15, "max": 15}


def test_a_result_without_tool_stats_is_not_counted_as_zero(tmp_path) -> None:
    """Een oudere of afgebroken run mist het veld; nul tool-calls zou de mediaan vervalsen."""
    records = [
        user("pak 619 op"),
        assistant(("Skill", {"skill": "paia-start-issue"})),
        assistant_with_scout("Poort 4b: writing-plans\nScout: issue-impact-scout"),
        scout_result(),
    ]
    write_session(tmp_path, records)
    result = kpi.measure_scout(tmp_path, None)
    assert result["getriggerd"] == 1
    assert result["tool_calls"] == {"n": 0, "mediaan": None, "max": None}


def test_a_run_over_the_search_budget_is_counted(tmp_path) -> None:
    """Het budget uit de persona staat daar als zoekactiegrens; hier wordt het meetbaar. Zonder deze
    teller is de prompt-norm decoratie — `maxTurns` telt beurten en ziet hem niet."""
    records = [
        user("pak 620 op"),
        assistant(("Skill", {"skill": "paia-start-issue"})),
        assistant_with_scout("Poort 4b: writing-plans\nScout: issue-impact-scout"),
        scout_result(tool_calls=kpi.SCOUT_TOOL_CALL_BUDGET + 5),
    ]
    write_session(tmp_path, records)
    result = kpi.measure_scout(tmp_path, None)
    assert result["boven_budget"] == 1


def test_a_run_on_the_budget_is_not_over_it(tmp_path) -> None:
    records = [
        user("pak 621 op"),
        assistant(("Skill", {"skill": "paia-start-issue"})),
        assistant_with_scout("Poort 4b: writing-plans\nScout: issue-impact-scout"),
        scout_result(tool_calls=kpi.SCOUT_TOOL_CALL_BUDGET),
    ]
    write_session(tmp_path, records)
    assert kpi.measure_scout(tmp_path, None)["boven_budget"] == 0


# --- pilot #937: drie meetgaten ---------------------------------------------------------------


def test_worktree_sessions_are_read_next_to_the_main_directory(tmp_path) -> None:
    """Een sessie die in een worktree start, landt in `<map>--claude-worktrees-<naam>/`, niet in
    de hoofdmap. Zonder die sibling-mappen zag `scout` 0 van de ~35 echte runs (#937)."""
    main = tmp_path / "-home-x-repo"
    sibling = tmp_path / "-home-x-repo--claude-worktrees-issue-610-iets"
    stranger = tmp_path / "-home-x-repo-ander"
    for directory in (main, sibling, stranger):
        directory.mkdir()
    records = [
        user("pak 610 op"),
        assistant(("Skill", {"skill": "paia-start-issue"})),
        assistant_with_scout("Poort 4b: writing-plans\nScout: issue-impact-scout"),
        scout_result(),
    ]
    write_session(sibling, records)
    write_session(stranger, records)

    assert kpi.measure_scout(main, None)["getriggerd"] == 1, "andere repo telt niet mee"


def test_the_worktree_skill_is_not_the_route_boundary() -> None:
    """Stap 3 van de skill zet de worktree vóór de route, dus `using-git-worktrees` is altijd
    de eerste Skill-call na de ambient load. Als grens slikte hij elke poortmelding daarna."""
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
    """Geen poortmelding is geen bewijs voor poort 0/1/2/4a. Zo'n run is onbekend — zelfde
    regel als de kale `Poort 4` — maar wordt wel geteld, anders verdwijnt hij uit beeld."""
    records = [
        user("pak 618 op"),
        assistant(("Skill", {"skill": "paia-start-issue"})),
        assistant_with_scout("Worktree staat, scout parallel."),
        scout_result(),
    ]
    write_session(tmp_path, records)
    result = kpi.measure_scout(tmp_path, None)
    assert result["mistrigger"] == 0
    assert result["getriggerd"] == 0
    assert result["poort_onbekend"] == 1


def test_an_async_launch_or_rejection_is_not_a_finished_run(tmp_path) -> None:
    """Een scout op de achtergrond retourneert meteen `status: async_launched`; een afgewezen
    call retourneert een string. Beide zijn geen afgeronde run: hun 1-10 s trok de mediaan
    van de looptijd omlaag (#937). De trigger telt wel — de skill vuurde hem."""
    launched = scout_result(uid="s1", stamp="2026-09-11T10:00:02Z")
    launched["toolUseResult"] = {"isAsync": True, "status": "async_launched"}
    rejected = scout_result(uid="s2", stamp="2026-09-11T11:00:10Z")
    rejected["toolUseResult"] = "User rejected tool use"
    records = [
        user("pak 619 op"),
        assistant(("Skill", {"skill": "paia-start-issue"})),
        assistant_with_scout("Poort 4c: brainstorming", uid="s1"),
        launched,
        user("pak 620 op"),
        assistant(("Skill", {"skill": "paia-start-issue"})),
        assistant_with_scout(
            "Poort 4b: writing-plans", uid="s2", stamp="2026-09-11T11:00:00Z"
        ),
        rejected,
    ]
    write_session(tmp_path, records)
    result = kpi.measure_scout(tmp_path, None)
    assert result["getriggerd"] == 2
    assert result["looptijd_seconden"]["n"] == 0
