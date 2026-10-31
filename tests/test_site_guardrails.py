"""The guardrails map stays in step with pack/*/hooks/hooks.json: every hook entry is drawn on its
event with its matcher and command, and nothing is drawn that hooks.json does not register."""

import copy
import json
from pathlib import Path

from tests.test_site_chain import REPO_BLOB, ROOT, load

# The column of guardrails.json that each hook event belongs in.
EVENT_PHASE = {
    "UserPromptSubmit": "prompt",
    "PreToolUse": "pre-tool",
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
    """The pack named by a hook chip's source: pack/<pack>/hooks/<script>."""
    return chip["source"].removeprefix(REPO_BLOB).split("/")[1]


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
        script = c["command"].split()[0]
        if not c["source"].endswith(f"{source_pack(c)}/hooks/{script}"):
            problems.append(f"{c['id']}: source does not match {source_pack(c)}/hooks/{script}")
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
    assert hook_problems(load("guardrails.json"), ROOT / "pack") == []


def test_hook_problems_flags_a_removed_hook():
    data = copy.deepcopy(load("guardrails.json"))
    data["chips"] = [c for c in data["chips"] if c["id"] != "main-commit-guard"]
    assert hook_problems(data, ROOT / "pack") == ["not on the map: core PreToolUse Bash main-commit-guard.sh"]


def test_hook_problems_flags_another_matcher():
    data = copy.deepcopy(load("guardrails.json"))
    chip(data, "worktree-guard")["matcher"] = "Bash"
    assert hook_problems(data, ROOT / "pack") == [
        "not in hooks.json: core PreToolUse Bash worktree-guard.sh",
        "not on the map: core PreToolUse Edit|Write worktree-guard.sh",
    ]


def test_hook_problems_flags_another_column():
    data = copy.deepcopy(load("guardrails.json"))
    chip(data, "go-check")["phase"] = "subagent-stop"
    assert hook_problems(data, ROOT / "pack") == ["go-check: Stop belongs in column stop, not subagent-stop"]


def test_hook_problems_flags_a_dropped_event():
    data = copy.deepcopy(load("guardrails.json"))
    chip(data, "commit-tests-post")["events"] = ["PostToolUse"]
    assert hook_problems(data, ROOT / "pack") == ["not on the map: core PostToolUseFailure Bash commit-tests.sh post"]


def test_hook_problems_flags_a_source_of_another_script():
    data = copy.deepcopy(load("guardrails.json"))
    chip(data, "gofmt")["source"] = chip(data, "go-check")["source"]
    assert hook_problems(data, ROOT / "pack") == ["gofmt: source does not match go/hooks/gofmt.sh"]


def test_hook_problems_flags_a_hook_chip_without_effects():
    data = copy.deepcopy(load("guardrails.json"))
    del chip(data, "gofmt")["effects"]
    assert hook_problems(data, ROOT / "pack") == ["gofmt: hook chip lacks events or effects"]


def test_phases_are_the_hook_events():
    labels = [p["label"] for p in load("guardrails.json")["phases"]]
    assert labels == ["UserPromptSubmit", "PreToolUse", "The tool call",
                      "PostToolUse / PostToolUseFailure", "SubagentStop", "Stop"]


def test_routes_are_the_situations():
    routes = {r["id"]: r["label"] for r in load("guardrails.json")["routes"]}
    assert routes == {
        "main-commit": "I commit on main", "red-tests": "I commit with red tests",
        "pr-evidence": "I open a PR without evidence", "usage-limit": "My limit is almost reached",
        "dead-link": "I write a dead link", "write-gates": "I write code a gate rejects",
    }
