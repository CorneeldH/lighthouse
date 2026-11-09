"""The session map stays in step with pack/*/hooks/hooks.json: every hook entry is drawn on its
event with its matcher and command, and nothing is drawn that hooks.json does not register."""

import copy
import json
import re
from pathlib import Path

from tests.test_site_loop import REPO_BLOB, ROOT, SITE, load

# The column of the session map (docs/site/data/session.json) that each hook event belongs in.
EVENT_PHASE = {
    "UserPromptSubmit": "prompt",
    "PreToolUse": "pre-tool",
    # A permission prompt comes before the tool runs.
    "Notification": "pre-tool",
    "PostToolUse": "post-tool",
    "PostToolUseFailure": "post-tool",
    "SubagentStop": "subagent-stop",
    "Stop": "stop",
}
HOOKS_DIR = "${CLAUDE_PLUGIN_ROOT}/hooks/"

Entry = tuple[str, str, str, str]


def chip(data: dict, chip_id: str) -> dict:
    return next(c for c in data["chips"] if c["id"] == chip_id)


def hook_entries(pack_root: Path) -> set[Entry]:
    """(pack, event, matcher, command) of every hook in pack_root/*/hooks/hooks.json.
    A missing matcher counts as the empty one."""
    found = set()
    for path in sorted(pack_root.glob("*/hooks/hooks.json")):
        pack = path.parent.parent.name
        for event, groups in json.loads(path.read_text(encoding="utf-8"))["hooks"].items():
            for group in groups:
                for hook in group["hooks"]:
                    command = hook["command"].removeprefix(HOOKS_DIR)
                    found.add((pack, event, group.get("matcher", ""), command))
    return found


def source_pack(chip: dict) -> str:
    """The pack named by a hook chip's source: pack/<pack>/hooks/<script>. A hook in Go
    (internal/hooks/) runs as `run.sh <name>` from pack/core, so it counts as core."""
    path = chip["source"].removeprefix(REPO_BLOB)
    if path.startswith("internal/hooks/"):
        return "core"
    return path.split("/")[1]


def expected_source(chip: dict) -> str:
    """Where a hook chip's source must end: internal/hooks/<name without dashes>.go for
    `run.sh <name>`, <pack>/hooks/<script> for a script."""
    script, *args = chip["command"].split()
    if script == "run.sh" and args:
        return f"internal/hooks/{args[0].replace('-', '')}.go"
    return f"{source_pack(chip)}/hooks/{script}"


def drawn_entries(data: dict) -> set[Entry]:
    """One tuple per event of every chip that has a command."""
    return {
        (source_pack(c), event, c.get("matcher", ""), c["command"])
        for c in data["chips"]
        if "command" in c
        for event in c.get("events", [])
    }


def describe(entry: Entry) -> str:
    pack, event, matcher, command = entry
    return f"{pack} {event} {matcher or 'all'} {command}"


def hook_problems(data: dict, pack_root: Path) -> list[str]:
    real, drawn = hook_entries(pack_root), drawn_entries(data)
    problems = [f"not on the map: {describe(e)}" for e in real - drawn]
    problems += [f"not in hooks.json: {describe(e)}" for e in drawn - real]
    for c in data["chips"]:
        if "command" not in c:
            continue
        if not c.get("events") or not c.get("effects"):
            problems.append(f"{c['id']}: hook chip lacks events or effects")
            continue
        for event in c["events"]:
            if c["phase"] != EVENT_PHASE[event]:
                problems.append(f"{c['id']}: {event} belongs in column {EVENT_PHASE[event]}, not {c['phase']}")
        expected = expected_source(c)
        if not c["source"].endswith(expected):
            problems.append(f"{c['id']}: source does not match {expected}")
    return sorted(problems)


def test_hook_entries_reads_every_pack(tmp_path):
    for pack, matcher in [("a", None), ("b", "")]:
        group = {"hooks": [{"type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/hooks/x.sh run"}]}
        if matcher is not None:
            group["matcher"] = matcher
        hooks = tmp_path / pack / "hooks"
        hooks.mkdir(parents=True)
        (hooks / "hooks.json").write_text(json.dumps({"hooks": {"Stop": [group]}}))
    assert hook_entries(tmp_path) == {("a", "Stop", "", "x.sh run"), ("b", "Stop", "", "x.sh run")}


def test_every_hook_is_on_its_event():
    assert hook_problems(load("session.json"), ROOT / "pack") == []


def test_hook_problems_flags_a_removed_hook():
    data = copy.deepcopy(load("session.json"))
    data["chips"] = [c for c in data["chips"] if c["id"] != "main-commit-gate"]
    assert hook_problems(data, ROOT / "pack") == ["not on the map: core PreToolUse Bash run.sh main-commit-gate"]


def test_hook_problems_flags_another_matcher():
    data = copy.deepcopy(load("session.json"))
    chip(data, "worktree-check")["matcher"] = "Bash"
    assert hook_problems(data, ROOT / "pack") == [
        "not in hooks.json: core PreToolUse Bash run.sh worktree-check",
        "not on the map: core PreToolUse Edit|Write run.sh worktree-check",
    ]


def test_hook_problems_flags_another_column():
    data = copy.deepcopy(load("session.json"))
    chip(data, "go-lint-gate")["phase"] = "subagent-stop"
    assert hook_problems(data, ROOT / "pack") == ["go-lint-gate: Stop belongs in column stop, not subagent-stop"]


def test_hook_problems_flags_a_dropped_event():
    data = copy.deepcopy(load("session.json"))
    chip(data, "commit-test-gate-post")["events"] = ["PostToolUse"]
    assert hook_problems(data, ROOT / "pack") == ["not on the map: core PostToolUseFailure Bash commit-test-gate.sh post"]


def test_hook_problems_flags_a_source_of_another_script():
    data = copy.deepcopy(load("session.json"))
    chip(data, "gofmt-check")["source"] = chip(data, "go-lint-gate")["source"]
    assert hook_problems(data, ROOT / "pack") == ["gofmt-check: source does not match go/hooks/gofmt-check.sh"]


def test_hook_problems_flags_a_go_hook_with_the_source_of_another():
    data = copy.deepcopy(load("session.json"))
    chip(data, "md-link-check")["source"] = chip(data, "permission-check")["source"]
    assert hook_problems(data, ROOT / "pack") == [
        "md-link-check: source does not match internal/hooks/mdlinkcheck.go"
    ]


def test_hook_problems_flags_a_hook_chip_without_effects():
    data = copy.deepcopy(load("session.json"))
    del chip(data, "gofmt-check")["effects"]
    assert hook_problems(data, ROOT / "pack") == ["gofmt-check: hook chip lacks events or effects"]


def test_phases_are_the_moments_of_a_turn():
    # The column head is upper case, so an event name gets spaces: USERPROMPTSUBMIT does not read.
    labels = [p["label"] for p in load("session.json")["phases"]]
    assert labels == ["User prompt submit", "Pre tool use", "The tool or skill call",
                      "Post tool use / failure", "Subagent stop", "Stop"]


def test_writing_skills_load_at_the_skill_call():
    # A skill loads through the Skill tool, so it sits in the column of the call, between its hooks.
    chips = load("session.json")["chips"]
    loaded = {c["id"]: c["kind"] for c in chips if c["phase"] == "tool-call" and c["trigger"] == "description"}
    assert loaded == {
        skill: "lighthouse" for skill in ["test-standard", "go-standard", "python-standard", "shell-standard"]
    }


def test_the_agent_runs_at_the_agent_call_not_on_the_loop():
    # An agent runs as a subagent through the Agent tool: a moment inside a session, not a step of the loop.
    scout = chip(load("session.json"), "issue-impact-scout")
    assert (scout["phase"], scout["source"]) == ("tool-call", REPO_BLOB + "pack/core/agents/issue-impact-scout.md")
    assert "issue-impact-scout" not in {c["id"] for c in load("loop.json")["chips"]}


def test_routes_light_up_the_most_hooks_first():
    sizes = [len(r["chips"]) for r in load("session.json")["routes"]]
    assert sizes == sorted(sizes, reverse=True)


def test_routes_are_the_situations():
    routes = {r["id"]: r["label"] for r in load("session.json")["routes"]}
    assert routes == {
        "main-commit": "I commit on main", "red-tests": "I commit with red tests",
        "pr-evidence": "I open a PR without evidence", "usage-limit": "My limit is almost reached",
        "dead-link": "I write a dead link", "write-gates": "I write code a gate rejects",
        "agent": "I send an agent to scout",
        "permission-prompt": "A controller run waits on a permission",
    }


def test_session_why_names_the_loop_not_the_chain():
    html = (SITE / "session.html").read_text(encoding="utf-8")
    why = html.split('<details class="why">', 1)[1].split("</details>", 1)[0]
    assert not re.search(r"\bchain\b", why.replace("loop.html", ""), re.IGNORECASE)
