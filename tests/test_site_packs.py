"""docs/site/data/packs.json stays in step with pack/: every pack is listed, with the files of its
detect.toml, its skills, agents, workflows and hooks."""

import json
import re
import tomllib

from tests.test_site_chain import ROOT, load

PACK = ROOT / "pack"

# The `when` text per set of (event, matcher) pairs a hook is registered under.
WHEN = {
    frozenset({("PreToolUse", "Bash")}): "before every shell command",
    frozenset(
        {("PreToolUse", "Bash"), ("PostToolUse", "Bash"), ("PostToolUseFailure", "Bash")}
    ): "around every shell command",
    frozenset({("PreToolUse", "Edit|Write")}): "before every edit",
    frozenset({("PostToolUse", "Edit|Write")}): "after every edit",
    frozenset({("Stop", "")}): "when the session stops",
    frozenset(
        {("PostToolUse", ""), ("UserPromptSubmit", ""), ("SubagentStop", ""), ("Stop", "")}
    ): "at every prompt, tool call, subagent stop and session stop",
}


def packs() -> list[dict]:
    return load("packs.json")


def registered(name: str) -> dict[str, set[tuple[str, str]]]:
    """script stem -> {(event, matcher)} from pack/<name>/hooks/hooks.json."""
    path = PACK / name / "hooks" / "hooks.json"
    if not path.exists():
        return {}
    found: dict[str, set[tuple[str, str]]] = {}
    for event, groups in json.loads(path.read_text(encoding="utf-8"))["hooks"].items():
        for group in groups:
            for hook in group["hooks"]:
                stem = re.search(r"hooks/([^/\s]+)\.sh", hook["command"]).group(1)
                found.setdefault(stem, set()).add((event, group.get("matcher", "")))
    return found


def test_every_pack_is_listed():
    """Every pack is on the map, also one without a skill."""
    on_disk = {p.name for p in PACK.iterdir() if p.is_dir()}
    assert {p["name"] for p in packs()} == on_disk


def test_detect_matches_detect_toml():
    for entry in packs():
        path = PACK / entry["name"] / "detect.toml"
        if not path.exists():
            assert entry["detect"] == [], entry["name"]
            assert entry["proposed_when"] == "always", entry["name"]
            continue
        spec = tomllib.loads(path.read_text(encoding="utf-8"))
        expected = list(spec.get("files", []))
        for rule in spec.get("contains", []):
            expected += [f for f in rule["files"] if f not in expected]
            assert rule["text"].strip('"') in entry["proposed_when"], entry["name"]
        assert entry["detect"] == expected, entry["name"]


def skills_of(base) -> list[str]:
    skills = base / "skills"
    return sorted(p.name for p in skills.iterdir()) if skills.is_dir() else []


def test_skills_agents_workflows_match():
    for entry in packs():
        base = PACK / entry["name"]
        assert entry["skills"] == skills_of(base), entry["name"]
        assert entry["agents"] == sorted(p.stem for p in (base / "agents").glob("*.md")), entry["name"]
        assert entry["workflows"] == sorted(p.stem for p in (base / "workflows").glob("*")), entry["name"]


def test_hooks_match_hooks_json():
    for entry in packs():
        found = registered(entry["name"])
        assert {h["name"] for h in entry["hooks"]} == set(found), entry["name"]
        for hook in entry["hooks"]:
            events = {event for event, _ in found[hook["name"]]}
            assert sorted(hook["events"]) == sorted(events), hook["name"]
    core = next(p for p in packs() if p["name"] == "core")
    assert len(core["hooks"]) == 8


def test_hook_when_follows_its_events():
    for entry in packs():
        found = registered(entry["name"])
        for hook in entry["hooks"]:
            pairs = frozenset(found[hook["name"]])
            assert pairs in WHEN, f"{hook['name']}: unknown set of event/matcher pairs {set(pairs)}"
            assert hook["when"] == WHEN[pairs], hook["name"]


def test_standard_skills_follow_the_maps():
    by_pack = {p["name"]: p for p in packs()}
    for entry in packs():
        assert set(entry["standard_skills"]) <= set(entry["skills"]), entry["name"]
    for name in ["chain.json", "session.json"]:
        for chip in load(name)["chips"]:
            m = re.search(r"/blob/main/pack/([^/]+)/skills/([^/]+)", chip.get("source", ""))
            if m:
                standard = m.group(2) in by_pack[m.group(1)]["standard_skills"]
                assert standard == (chip["trigger"] == "description"), chip["id"]
