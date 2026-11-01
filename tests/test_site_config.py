"""The configuration map (docs/site/data/config.json) stays in step with the code: every key that
pack/core/lib/config.py and internal/usage/config.go accept is on it, with the default and the
level they have, and every reader it names really names the key."""

import copy
import importlib.util
import json
import re
import subprocess
import sys
from pathlib import Path

from tests._site_node import run_map
from tests.test_site_chain import REPO_BLOB, REPO_TREE, ROOT, load

_spec = importlib.util.spec_from_file_location("lighthouse_site_config", ROOT / "pack/core/lib/config.py")
config = importlib.util.module_from_spec(_spec)
sys.modules[_spec.name] = config
_spec.loader.exec_module(config)

NO_DEFAULT = object()
# Keys nothing in the repo reads: only config.py checks them (spec section 2).
UNREAD = {"paths.history", "codex_usage.vraag_pct_week", "codex_usage.halt_pct_session", "host.name"}
SET = {"project.language", "forge.kind", "forge.repo", "test_gate.command", "host.name", "host.tailscale_ip"}
REQUIRED = {
    "forge.board.project_number": "with [forge.board]",
    "forge.board.project_id": "with [forge.board]",
    "forge.board.status_field_id": "with [forge.board]",
    "forge.board.status_options": "with [forge.board]",
    "code_language_llm.url": "with [code_language_llm]",
    "code_language_llm.models": "with [code_language_llm]",
    "host.name": "yes",
    "host.tailscale_ip": "yes",
    "forge.owner": "derived",
    "forge.name": "derived",
}
SKIPPED_READERS = {"pack/core/lib/config.py"}
_texts: dict[str, str] = {}
_files: list[str] = []


def level_of(key: str) -> str:
    if key in SET:
        return "set"
    return "optional" if key.startswith("forge.board.") else "advanced"


def usage_defaults(go: str) -> dict[str, int]:
    """The [usage] keys of internal/usage/config.go with the default DefaultConfig gives them."""
    block = re.search(r"Usage struct \{(.*?)\} `toml:\"usage\"`", go, re.S)
    assert block, "no Usage struct in internal/usage/config.go"
    fields = re.findall(r"(\w+)\s+\*int\s+`toml:\"(\w+)\"`", block.group(1))
    literal = re.search(r"return Config\{(.*?)\}", go, re.S)
    assert fields and literal, "no usage fields or DefaultConfig in internal/usage/config.go"
    values = {}
    for name, value in re.findall(r"(\w+):\s*(\d+)(?:\s*\*\s*time\.Second)?", literal.group(1)):
        values[name] = int(value)
    aliases = {"CacheTTLSeconds": "TTL"}
    return {f"usage.{tag}": values[aliases.get(name, name)] for name, tag in fields}


def expected_keys() -> dict[str, object]:
    """key -> default (or NO_DEFAULT) for every key on the map."""
    go = (ROOT / "internal/usage/config.go").read_text(encoding="utf-8")
    usage = usage_defaults(go)
    keys = {*config.SCHEMA, *config._BASE_KEYS, *config.HOST_KEYS, *usage}
    keys |= {p.rstrip(".") for p in config._OPTION_PREFIXES}
    found: dict[str, object] = {}
    for key in keys:
        if key in usage:
            found[key] = usage[key]
        elif key == "project.language":
            found[key] = "en"
        elif key in config.DEFAULTS:
            value = config.DEFAULTS[key]
            found[key] = list(value) if isinstance(value, tuple) else value
        else:
            found[key] = NO_DEFAULT
    return found


def reader_names_key(text: str, key: str, path: str) -> bool:
    table, _, name = key.rpartition(".")
    if key in text or f"[{table}] {name}" in text:
        return True
    if path.endswith(".go"):
        return f'toml:"{name}"' in text and f'toml:"{table.rpartition(".")[2]}"' in text
    if path.endswith(".py"):
        return f'f"{table}.{{' in text and f'"{name}"' in text
    return False


def repo_files(root: Path) -> list[str]:
    if not _files:
        out = subprocess.run(
            ["git", "ls-files", "pack", "internal", "cmd"], cwd=root, check=True, capture_output=True, text=True
        ).stdout
        _files.extend(
            p for p in out.splitlines()
            if p not in SKIPPED_READERS and not p.endswith("_test.go") and "testdata/" not in p
        )
    return _files


def reader_unit(path: str) -> str:
    m = re.match(r"(pack/[^/]+/skills/[^/]+)/", path)
    return m.group(1) if m else path


def found_readers(key: str, root: Path) -> list[str]:
    found = set()
    for path in repo_files(root):
        if path not in _texts:
            _texts[path] = (root / path).read_text(encoding="utf-8", errors="replace")
        if reader_names_key(_texts[path], key, path):
            found.add(reader_unit(path))
    return sorted(found)


def all_keys(data: dict):
    return [k for f in data["files"] for t in f["tables"] for k in t["keys"]]


def config_problems(data: dict, root: Path) -> list[str]:
    expected = expected_keys()
    problems = []
    on_map = {k["key"]: k for k in all_keys(data)}
    for key in expected.keys() - on_map.keys():
        problems.append(f"not on the map: {key}")
    for key in on_map.keys() - expected.keys():
        problems.append(f"not in config.py: {key}")
    for key, entry in on_map.items():
        if key not in expected:
            continue
        want = expected[key]
        if want is NO_DEFAULT:
            if "default" in entry:
                problems.append(f"{key}: default on the map, none in config.py")
        elif entry.get("default") != want:
            problems.append(f"{key}: default {entry.get('default')!r}, config.py says {want!r}")
        if entry["level"] != level_of(key):
            problems.append(f"{key}: level {entry['level']}, expected {level_of(key)}")
        if entry["required"] != REQUIRED.get(key, "no"):
            problems.append(f"{key}: required {entry['required']}, expected {REQUIRED.get(key, 'no')}")
        problems += reader_problems(key, entry["readers"], root)
    return sorted(problems)


def reader_problems(key: str, readers: list[str], root: Path) -> list[str]:
    problems = []
    if not readers and key not in UNREAD:
        problems.append(f"{key}: no readers")
    found = found_readers(key, root)
    for path in readers:
        if not path.startswith(("pack/", "internal/", "cmd/")):
            problems.append(f"{key}: reader {path} outside pack/, internal/ or cmd/")
        elif not (root / path).exists():
            problems.append(f"{key}: reader {path} does not exist")
        elif path not in found:
            problems.append(f"{key}: reader {path} does not name it")
    for path in found:
        if path not in readers:
            problems.append(f"{key}: reader {path} missing from the map")
    return problems


def entry_of(data: dict, key: str) -> dict:
    return next(k for k in all_keys(data) if k["key"] == key)


def test_every_key_is_on_the_map():
    assert config_problems(load("config.json"), ROOT) == []


def test_usage_defaults_come_from_go():
    go = (ROOT / "internal/usage/config.go").read_text(encoding="utf-8")
    assert usage_defaults(go) == {"usage.halt_pct_session": 85, "usage.halt_pct_week": 95,
                                  "usage.cache_ttl_seconds": 300}


def test_reader_names_key_accepts_the_table_notation():
    assert reader_names_key("run `[test_gate] command`", "test_gate.command", "a/SKILL.md")
    assert not reader_names_key("test_gate.slots", "test_gate.command", "a/SKILL.md")


def test_reader_names_key_needs_the_table_tag_in_go():
    go = (ROOT / "internal/usage/config.go").read_text(encoding="utf-8")
    assert reader_names_key(go, "usage.halt_pct_session", "internal/usage/config.go")
    assert not reader_names_key(go, "codex_usage.halt_pct_session", "internal/usage/config.go")


def test_reader_names_key_finds_a_dynamic_python_reader():
    text = (ROOT / "pack/core/scripts/codex_usage.py").read_text(encoding="utf-8")
    assert reader_names_key(text, "codex_usage.review_pct_week", "pack/core/scripts/codex_usage.py")
    assert not reader_names_key(text, "codex_usage.vraag_pct_week", "pack/core/scripts/codex_usage.py")


def test_found_readers_skips_go_tests():
    assert found_readers("host.name", ROOT) == []  # only internal/initcmd/initcmd_test.go names it


def test_unread_keys_have_no_reader():
    assert {k: found_readers(k, ROOT) for k in UNREAD} == {k: [] for k in UNREAD}


def test_no_controller_options():
    raw = (ROOT / "docs/site/data/config.json").read_text(encoding="utf-8")
    keys = {k["key"] for k in all_keys(load("config.json"))}
    assert not {k for k in keys if k.startswith(("controller.", "dispatch."))}
    assert "Controller" not in raw


def test_problems_flag_a_removed_key():
    data = copy.deepcopy(load("config.json"))
    for f in data["files"]:
        for t in f["tables"]:
            t["keys"] = [k for k in t["keys"] if k["key"] != "test_gate.slots"]
    assert config_problems(data, ROOT) == ["not on the map: test_gate.slots"]


def test_problems_flag_a_dispatch_key():
    data = copy.deepcopy(load("config.json"))
    data["files"][1]["tables"][0]["keys"].append(
        {"key": "dispatch.slots", "level": "advanced", "required": "no", "about": "x", "readers": []})
    assert config_problems(data, ROOT) == ["not in config.py: dispatch.slots"]


def test_problems_flag_another_default():
    data = copy.deepcopy(load("config.json"))
    entry_of(data, "labels.needs_hands")["default"] = "x"
    assert config_problems(data, ROOT) == ["labels.needs_hands: default 'x', config.py says 'needs-hands'"]


def test_problems_flag_a_default_without_one():
    data = copy.deepcopy(load("config.json"))
    entry_of(data, "paths.tenets")["default"] = "x"
    assert config_problems(data, ROOT) == ["paths.tenets: default on the map, none in config.py"]


def test_problems_flag_a_lowered_level():
    data = copy.deepcopy(load("config.json"))
    entry_of(data, "forge.repo")["level"] = "advanced"
    assert config_problems(data, ROOT) == ["forge.repo: level advanced, expected set"]


def test_problems_flag_a_reader_that_does_not_name_it():
    data = copy.deepcopy(load("config.json"))
    readers = entry_of(data, "shell.strict_dirs")["readers"]
    readers.append("pack/core/skills/close-issue")
    assert config_problems(data, ROOT) == [
        "shell.strict_dirs: reader pack/core/skills/close-issue does not name it"]


def test_problems_flag_a_missing_reader():
    data = copy.deepcopy(load("config.json"))
    entry_of(data, "test_gate.command")["readers"].remove("pack/core/hooks/commit-test-gate.sh")
    assert config_problems(data, ROOT) == [
        "test_gate.command: reader pack/core/hooks/commit-test-gate.sh missing from the map"]


def test_problems_flag_an_empty_reader_list():
    data = copy.deepcopy(load("config.json"))
    entry = entry_of(data, "test_gate.command")
    gone = entry["readers"]
    entry["readers"] = []
    assert config_problems(data, ROOT) == sorted(
        ["test_gate.command: no readers"] + [f"test_gate.command: reader {p} missing from the map" for p in gone])


def test_problems_flag_a_dogfood_reader():
    data = copy.deepcopy(load("config.json"))
    entry_of(data, "test_gate.command")["readers"].append(".claude/skills/lighthouse-start-issue")
    assert config_problems(data, ROOT) == [
        "test_gate.command: reader .claude/skills/lighthouse-start-issue outside pack/, internal/ or cmd/"]


def test_problems_flag_a_tuple_default_as_equal():
    data = copy.deepcopy(load("config.json"))
    assert entry_of(data, "paths.search_dirs")["default"] == ["."]
    assert config_problems(data, ROOT) == []


def test_config_reader_marks_the_sort():
    packs = json.dumps(load("packs.json"))
    read = lambda p: run_map(f"m.configReader({json.dumps(p)}, {packs})")
    assert read("pack/go/skills/go-standard")["sort"] == "standard-skill"
    assert read("pack/core/skills/write-issue") == {
        "name": "write-issue", "sort": "workflow-skill",
        "href": REPO_TREE + "pack/core/skills/write-issue"}
    assert read("pack/core/hooks/commit-test-gate.sh")["sort"] == "hook"
    assert read("pack/core/workflows/unblock-dependents.yml")["sort"] == "github"
    assert read("internal/usage/config.go") == {
        "name": "config.go", "sort": "program", "href": REPO_BLOB + "internal/usage/config.go"}


def test_every_reader_on_the_map_gets_a_link():
    packs = json.dumps(load("packs.json"))
    paths = sorted({r for f in load("config.json")["files"] for t in f["tables"]
                    for k in t["keys"] for r in k["readers"]})
    links = run_map(f"{json.dumps(paths)}.map((p) => m.configReader(p, {packs}).href)")
    assert all(h.startswith((REPO_BLOB, REPO_TREE)) for h in links)


def config_entries():
    data, packs = json.dumps(load("config.json")), json.dumps(load("packs.json"))
    return {e["key"]: e for e in run_map(f"m.configEntries({data}, {packs})")}


def test_config_entries_cover_every_key_with_its_label():
    entries = config_entries()
    keys = {k["key"] for f in load("config.json")["files"] for t in f["tables"] for k in t["keys"]}
    assert set(entries) == keys
    assert entries["forge.repo"]["levelLabel"] == "Set this"
    assert entries["forge.board.project_id"]["levelLabel"] == "Optional"
    assert entries["usage_guard.controller"]["levelLabel"] == "Advanced"


def test_config_entries_say_what_happens_without_the_key():
    entries = config_entries()
    assert entries["paths.search_dirs"]["without"] == "Without it: [.]"
    assert entries["usage_guard.resume"]["without"] == "Without it: false"  # config.py: False
    assert entries["paths.tenets"]["without"] == "Without it: not set"
    assert entries["paths.history"]["readers"] == []
