"""The map of the repo after init (docs/site/data/repo.json) stays in step with the code: every place
init writes (internal/initcmd/testdata/places.golden) is one row on it, except what OFF_THE_MAP
names, each row says whether init adds it or edits a file of yours as the golden implies, and every
link lands."""

import copy
import json

from tests._site_node import run_map
from tests.test_site_chain import ROOT, SITE, load

GOLDEN = ROOT / "internal/initcmd/testdata/places.golden"
DIRS = ["root", ".claude", ".agents", ".lighthouse", ".github"]
REQUIRED = "id label dir kind act".split()
ACTS = {"adds", "edits"}
OFF_THE_MAP = {
    "CLAUDE.md": "init writes it only when AGENTS.md and CLAUDE.md are both missing, as one line "
    "@AGENTS.md, and never touches one that exists",
}
INIT_ACTS = {"lighthouse.toml": "edits", ".lighthouse/manifest.toml": "adds"}
EDIT_KINDS = {"block", "hook", "permission"}
SEPARATORS = ("·", ",")
_sorts: list[str] = []


def parse_golden(text: str) -> dict[str, tuple[set[str], list[str]]]:
    """key -> (kinds, packs) of every non-comment line of places.golden."""
    found = {}
    for line in text.splitlines():
        if not line or line.startswith("#"):
            continue
        key, kinds, packs = line.split("\t")
        found[key] = (set(kinds.split(",")), packs.split(",") if packs else [])
    return found


def golden():
    return parse_golden(GOLDEN.read_text(encoding="utf-8"))


def sort_keys() -> list[str]:
    """The keys of SORTS in map.js: the kinds a row may have."""
    if not _sorts:
        _sorts.extend(run_map("Object.keys(m.SORTS)"))
    return _sorts


def listed(items) -> str:
    return "[" + ", ".join(items) + "]"


def room_of(place: str) -> str:
    first = place.split("/", 1)[0]
    return first if "/" in place and first.startswith(".") else "root"


def place_problems(data: dict, gold: dict) -> list[str]:
    problems = []
    placed = [r for r in data["rows"] if r.get("place")]
    for key in gold:
        if key not in OFF_THE_MAP and not any(r["place"] == key for r in placed):
            problems.append(f"not on the map: {key}")
    for place in {r["place"] for r in placed}:
        count = sum(r["place"] == place for r in placed)
        if count > 1:
            problems.append(f"{place}: on {count} rows")
    for r in placed:
        if r["place"] in OFF_THE_MAP:
            problems.append(f"{r['place']}: off the map, yet a row")
        elif r["place"] not in gold:
            problems.append(f"not in places.golden: {r['place']}")
    bare = [r for r in data["rows"] if not r.get("place")]
    problems += [f"{r['id']}: no place outside the workstation" for r in bare if r["dir"] != "workstation"]
    if len(bare) != 1:
        problems.append(f"expected one row without a place, found {len(bare)}")
    return sorted(problems)


def act_problems(data: dict, gold: dict) -> list[str]:
    problems = []
    for r in data["rows"]:
        if r.get("place") not in gold:
            continue
        kinds = gold[r["place"]][0]
        if kinds & EDIT_KINDS:
            want = "edits"
        elif kinds == {"file"}:
            want = "adds"
        else:
            want = INIT_ACTS[r["place"]]
        if r["act"] != want:
            problems.append(f"{r['id']}: act {r['act']}, expected {want}")
    return sorted(problems)


def shape_problems(data: dict, sorts: list[str]) -> list[str]:
    problems = []
    ids = [d["id"] for d in data["dirs"]]
    if ids != DIRS:
        problems.append(f"dirs are {listed(ids)}, not {listed(DIRS)}")
    seen = set()
    for r in data["rows"]:
        i = r.get("id", "?")
        problems += [f"{i}: lacks {k}" for k in REQUIRED if k not in r]
        if i in seen:
            problems.append(f"duplicate id {i}")
        seen.add(i)
        if "kind" in r and r["kind"] not in sorts:
            problems.append(f"{i}: unknown kind {r['kind']}")
        if "act" in r and r["act"] not in ACTS:
            problems.append(f"{i}: unknown act {r['act']}")
        if "dir" in r and r["dir"] not in DIRS + ["workstation"]:
            problems.append(f"{i}: unknown dir {r['dir']}")
        elif r.get("place") and r.get("dir") != room_of(r["place"]):
            problems.append(f"{i}: dir {r.get('dir')}, place says {room_of(r['place'])}")
    return sorted(problems)


def link_problems(data: dict) -> list[str]:
    problems = []
    system_ids = {s["id"] for s in load("system.json")["sections"]}
    for r in data["rows"]:
        if not r.get("href"):
            continue
        page, _, anchor = r["href"].partition("#")
        if not (SITE / page).is_file():
            problems.append(f"{r['id']}: {page} points nowhere")
        elif anchor:
            have = system_ids if page == "system.html" else None
            ok = anchor in have if have is not None else f'id="{anchor}"' in (SITE / page).read_text(encoding="utf-8")
            if not ok:
                problems.append(f"{r['id']}: {page} has no id {anchor}")
    return sorted(problems)


def label_problems(data: dict) -> list[str]:
    """A row is one thing: its label holds no separator such as `lib/ · scripts/`."""
    return sorted(
        f"{r['id']}: label {r['label']} holds two things"
        for r in data["rows"]
        if any(s in r["label"] for s in SEPARATORS)
    )


def row(data: dict, ident: str) -> dict:
    return next(r for r in data["rows"] if r["id"] == ident)


def mutated():
    return copy.deepcopy(load("repo.json"))


def test_the_map_matches_the_places_init_writes():
    assert place_problems(load("repo.json"), golden()) == []


def test_act_matches_the_golden():
    assert act_problems(load("repo.json"), golden()) == []


def test_shape():
    assert shape_problems(load("repo.json"), sort_keys()) == []


def test_links():
    assert link_problems(load("repo.json")) == []


def test_one_thing_per_row():
    assert label_problems(load("repo.json")) == []


def test_problems_flag_a_missing_row():
    data = mutated()
    data["rows"] = [r for r in data["rows"] if r["id"] != "claude-agents"]
    assert place_problems(data, golden()) == ["not on the map: .claude/agents/lighthouse-*.md"]


def test_problems_flag_a_new_place_in_the_golden():
    gold = golden() | {".claude/commands/lighthouse-*/": ({"file"}, ["core"])}
    assert place_problems(load("repo.json"), gold) == ["not on the map: .claude/commands/lighthouse-*/"]


def test_problems_flag_a_place_init_does_not_write():
    data = mutated()
    row(data, "headings")["place"] = ".lighthouse/cache/"
    assert place_problems(data, golden()) == [
        "not in places.golden: .lighthouse/cache/",
        "not on the map: .lighthouse/headings.toml",
    ]


def test_problems_flag_claude_md_on_the_map():
    data = mutated()
    data["rows"].append(
        {"id": "claude-md", "place": "CLAUDE.md", "label": "CLAUDE.md", "dir": "root", "kind": "rules", "act": "adds"}
    )
    assert place_problems(data, golden()) == ["CLAUDE.md: off the map, yet a row"]


def test_problems_flag_a_second_row_without_place():
    data = mutated()
    del row(data, "manifest")["place"]
    assert place_problems(data, golden()) == [
        "expected one row without a place, found 2",
        "manifest: no place outside the workstation",
        "not on the map: .lighthouse/manifest.toml",
    ]


def test_problems_flag_a_wrong_act():
    data = mutated()
    row(data, "claude-settings")["act"] = "adds"
    assert act_problems(data, golden()) == ["claude-settings: act adds, expected edits"]
    data = mutated()
    row(data, "manifest")["act"] = "edits"
    assert act_problems(data, golden()) == ["manifest: act edits, expected adds"]


def test_problems_flag_an_unknown_kind_and_dir():
    data = mutated()
    row(data, "headings")["kind"] = "skill"
    row(data, "headings")["dir"] = ".claude"
    row(data, "host-toml")["dir"] = "home"
    assert shape_problems(data, sort_keys()) == [
        "headings: dir .claude, place says .lighthouse",
        "headings: unknown kind skill",
        "host-toml: unknown dir home",
    ]


def test_problems_flag_an_anchor_that_does_not_exist():
    data = mutated()
    row(data, "claude-skills")["href"] = "reference.html#nope"
    row(data, "agents-md")["href"] = "system.html#nope"
    assert link_problems(data) == [
        "agents-md: system.html has no id nope",
        "claude-skills: reference.html has no id nope",
    ]


def test_problems_flag_two_things_in_one_row():
    data = mutated()
    row(data, "lighthouse-lib")["label"] = "lib/ · scripts/"
    assert label_problems(data) == ["lighthouse-lib: label lib/ · scripts/ holds two things"]


def entries(mode: str, data: dict | None = None):
    return run_map(f"m.repoEntries({json.dumps(data or load('repo.json'))}, {json.dumps(mode)})")


def entry_rows(mode: str) -> dict[str, dict]:
    """place (or label, for the workstation) -> row, from repoEntries in `mode`."""
    return {r["place"] or r["label"]: r for e in entries(mode) for r in e["rows"]}


def refusal(mode: str, data: dict) -> str | None:
    return run_map(
        f"(() => {{ try {{ m.repoEntries({json.dumps(data)}, {json.dumps(mode)}); return null; }}"
        " catch (e) { return e.message; } })()"
    )


def test_entries_keep_the_order_of_the_tree():
    found = entries("everything")
    assert [e["id"] for e in found] == ["root", ".claude", ".agents", ".lighthouse", ".github", "workstation"]
    by_id = {e["id"]: e for e in found}
    assert by_id["root"]["yours"] == "your code, your tests, your config, …"
    assert by_id[".lighthouse"]["yours"] is None
    assert by_id["workstation"]["note"] == load("repo.json")["workstation"]["note"]


def test_everything_dims_nothing():
    assert [p for p, r in entry_rows("everything").items() if r["dim"]] == []


def test_edits_leaves_three():
    rows = entry_rows("edits")
    assert {p for p, r in rows.items() if not r["dim"]} == {"AGENTS.md", "lighthouse.toml", ".claude/settings.json"}
    assert rows["~/.config/lighthouse/host.toml"]["dim"] is True


def test_adds_leaves_what_init_adds():
    lit = {r["id"] for e in entries("adds") for r in e["rows"] if not r["dim"]}
    assert lit == {r["id"] for r in load("repo.json")["rows"] if r["act"] == "adds"}
    assert "host-toml" in lit


def test_rows_carry_glyph_and_colour():
    rows = entry_rows("everything")
    assert rows[".claude/agents/lighthouse-*.md"]["glyph"] == "◆"
    assert rows[".claude/agents/lighthouse-*.md"]["className"] == "kind-lighthouse form-agent"
    assert rows["AGENTS.md"]["glyph"] == "§"
    assert rows["AGENTS.md"]["className"] == "kind-rules form-rules"


def test_rows_say_what_init_does():
    rows = entry_rows("everything")
    assert rows[".claude/settings.json"]["actLabel"] == "init edits your file"
    assert rows[".lighthouse/scripts/"]["actLabel"] == "init adds it"


def test_entries_refuse_an_unknown_mode():
    assert refusal("upgrade", load("repo.json")) == "unknown mode: upgrade"


def test_entries_refuse_an_unknown_kind():
    data = mutated()
    row(data, "headings")["kind"] = "skill"
    assert refusal("everything", data) == "unknown kind: skill"


def test_links_have_an_accessible_name():
    """No DOM in node: this only holds that repoRow, which renderRepo draws each row with, names
    each ↗ after its row."""
    text = (SITE / "map.js").read_text(encoding="utf-8")
    body = text.split("function repoRow", 1)[1].split("\nfunction ", 1)[0]
    assert '"aria-label"' in body
    assert "`More about ${" in body
