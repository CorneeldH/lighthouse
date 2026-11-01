"""The map of the repo after init (docs/site/data/repo.json) stays in step with the code: every place
init writes (internal/initcmd/testdata/places.golden) is an area on it with the owner and packs the
golden implies, every reader it names exists, every link lands, and --force is promised only where
--upgrade really refuses."""

import copy
import json
from pathlib import Path

from tests._site_node import run_map
from tests.test_site_chain import REPO_BLOB, REPO_TREE, ROOT, SITE, load

GOLDEN = ROOT / "internal/initcmd/testdata/places.golden"
OWNERS = {"lighthouse", "shared", "yours"}
ROOMS = ["root", ".claude", ".agents", ".lighthouse", ".github", "yours", "workstation"]
READER_NAMES = {"Claude Code", "Codex", "OpenCode", "GitHub", "GitHub Actions"}
READER_ROOTS = ("pack/", "internal/", "cmd/")
REQUIRED = "id label room owner about how upgrade packs readers more".split()
INIT_OWNERS = {"CLAUDE.md": "yours", "lighthouse.toml": "shared", ".lighthouse/manifest.toml": "lighthouse"}
MANIFEST_KINDS = {"file", "block", "hook", "permission"}
_texts: dict[Path, str] = {}


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


def named_token(area: dict) -> str:
    """The text a reader of this area must contain: the basename of its place, without * or a final /."""
    return (area.get("place") or area["label"]).rstrip("/").rsplit("/", 1)[-1].replace("*", "")


def listed(items) -> str:
    return "[" + ", ".join(items) + "]"


def room_of(place: str) -> str:
    first = place.split("/", 1)[0]
    return first if "/" in place and first.startswith(".") else "root"


def shape_problems(data: dict) -> list[str]:
    problems = []
    ids = [r["id"] for r in data["rooms"]]
    if ids != ROOMS:
        problems.append(f"rooms are {listed(ids)}, not {listed(ROOMS)}")
    seen = set()
    for a in data["areas"]:
        i = a.get("id", "?")
        problems += [f"{i}: lacks {k}" for k in REQUIRED if k not in a]
        if i in seen:
            problems.append(f"duplicate id {i}")
        seen.add(i)
        if a.get("owner") not in OWNERS | {None}:
            problems.append(f"{i}: unknown owner {a['owner']}")
        if a.get("room") not in set(ROOMS) | {None}:
            problems.append(f"{i}: unknown room {a['room']}")
        place = a.get("place")
        if place:
            if a.get("label") != place:
                problems.append(f"{i}: label {a.get('label')}, place {place}")
            if a.get("room") != room_of(place):
                problems.append(f"{i}: room {a.get('room')}, place says {room_of(place)}")
        if a.get("room") == "yours":
            problems.append("room yours has areas")
    return sorted(problems)


def place_problems(data: dict, gold: dict) -> list[str]:
    problems = []
    placed = [a for a in data["areas"] if a.get("place")]
    for key in gold:
        if not any(a["place"] == key for a in placed):
            problems.append(f"not on the map: {key}")
    for place in {a["place"] for a in placed}:
        count = sum(a["place"] == place for a in placed)
        if count > 1:
            problems.append(f"{place}: on {count} areas")
    for a in placed:
        if a["place"] not in gold:
            problems.append(f"not in places.golden: {a['place']}")
            continue
        kinds, packs = gold[a["place"]]
        if sorted(a["packs"]) != sorted(packs):
            problems.append(f"{a['id']}: packs {listed(sorted(a['packs']))}, places.golden says {listed(sorted(packs))}")
        if kinds == {"file"}:
            want = "lighthouse"
        elif kinds & {"block", "hook", "permission"}:
            want = "shared"
        else:
            want = INIT_OWNERS[a["place"]]
        if a["owner"] != want:
            problems.append(f"{a['id']}: owner {a['owner']}, expected {want}")
    bare = [a for a in data["areas"] if not a.get("place")]
    problems += [f"{a['id']}: no place outside the workstation" for a in bare if a["room"] != "workstation"]
    if len(bare) != 1:
        problems.append(f"expected one area without a place, found {len(bare)}")
    return sorted(problems)


def reader_text(path: Path) -> str:
    if path not in _texts:
        _texts[path] = path.read_text(encoding="utf-8", errors="replace")
    return _texts[path]


def names(path: Path, token: str) -> bool:
    if path.is_file():
        return token in reader_text(path)
    return any(token in reader_text(f) for f in sorted(path.rglob("*")) if f.is_file())


def reader_problems(data: dict, root: Path) -> list[str]:
    problems = []
    for a in data["areas"]:
        if not a["readers"]:
            problems.append(f"{a['id']}: no readers")
        token = named_token(a)
        for r in a["readers"]:
            if r in READER_NAMES:
                continue
            if not r.startswith(READER_ROOTS):
                problems.append(f"{a['id']}: reader {r} outside pack/, internal/ or cmd/")
            elif not (root / r).exists():
                problems.append(f"{a['id']}: reader {r} does not exist")
            elif not names(root / r, token):
                problems.append(f"{a['id']}: reader {r} does not name {token}")
    return sorted(problems)


def link_problems(data: dict) -> list[str]:
    problems = []
    system_ids = {s["id"] for s in load("system.json")["sections"]}
    for a in data["areas"]:
        page, _, anchor = a["more"].partition("#")
        if not (SITE / page).is_file():
            problems.append(f"{a['id']}: {page} points nowhere")
        elif anchor:
            have = system_ids if page == "system.html" else None
            ok = anchor in have if have is not None else f'id="{anchor}"' in (SITE / page).read_text(encoding="utf-8")
            if not ok:
                problems.append(f"{a['id']}: {page} has no id {anchor}")
    return sorted(problems)


def upgrade_problems(data: dict, gold: dict) -> list[str]:
    problems = []
    for a in data["areas"]:
        refuses = bool(a.get("place") in gold and gold[a["place"]][0] & MANIFEST_KINDS)
        says = "--force" in a["upgrade"]
        if refuses and not says:
            problems.append(f"{a['id']}: upgrade does not say --force")
        if says and not refuses:
            problems.append(f"{a['id']}: upgrade names --force")
    return sorted(problems)


def area(data: dict, ident: str) -> dict:
    return next(a for a in data["areas"] if a["id"] == ident)


def mutated():
    return copy.deepcopy(load("repo.json"))


def test_the_map_matches_the_places_init_writes():
    assert place_problems(load("repo.json"), golden()) == []


def test_shape():
    assert shape_problems(load("repo.json")) == []


def test_readers():
    assert reader_problems(load("repo.json"), ROOT) == []


def test_links():
    assert link_problems(load("repo.json")) == []


def test_upgrade_says_force_only_where_it_refuses():
    assert upgrade_problems(load("repo.json"), golden()) == []


def test_named_token():
    assert named_token({"place": ".claude/skills/lighthouse-*/"}) == "lighthouse-"
    assert named_token({"place": ".lighthouse/headings.toml"}) == "headings.toml"
    assert named_token({"label": "~/.config/lighthouse/host.toml"}) == "host.toml"


def test_problems_flag_a_missing_area():
    data = mutated()
    data["areas"] = [a for a in data["areas"] if a["id"] != "claude-agents"]
    assert place_problems(data, golden()) == ["not on the map: .claude/agents/lighthouse-*.md"]


def test_problems_flag_a_new_place_in_the_golden():
    gold = golden() | {".claude/commands/lighthouse-*/": ({"file"}, ["core"])}
    assert place_problems(load("repo.json"), gold) == ["not on the map: .claude/commands/lighthouse-*/"]


def test_problems_flag_a_place_init_does_not_write():
    data = mutated()
    area(data, "headings")["place"] = area(data, "headings")["label"] = ".lighthouse/cache/"
    assert place_problems(data, golden()) == [
        "not in places.golden: .lighthouse/cache/",
        "not on the map: .lighthouse/headings.toml",
    ]


def test_problems_flag_other_packs():
    data = mutated()
    area(data, "agents-md")["packs"].remove("go")
    assert place_problems(data, golden()) == [
        "agents-md: packs [core, python, shell], places.golden says [core, go, python, shell]"
    ]


def test_problems_flag_a_wrong_owner():
    data = mutated()
    area(data, "claude-settings")["owner"] = "lighthouse"
    assert place_problems(data, golden()) == ["claude-settings: owner lighthouse, expected shared"]


def test_problems_flag_a_second_area_without_place():
    data = mutated()
    del area(data, "manifest")["place"]
    assert place_problems(data, golden()) == [
        "expected one area without a place, found 2",
        "manifest: no place outside the workstation",
        "not on the map: .lighthouse/manifest.toml",
    ]


def test_problems_flag_a_reader_that_does_not_name_it():
    data = mutated()
    area(data, "headings")["readers"] = ["internal/hostconfig"]
    assert reader_problems(data, ROOT) == ["headings: reader internal/hostconfig does not name headings.toml"]


def test_problems_flag_a_dogfood_reader():
    data = mutated()
    area(data, "pr-template")["readers"] = [".claude/skills/lighthouse-write-pr/SKILL.md"]
    assert reader_problems(data, ROOT) == [
        "pr-template: reader .claude/skills/lighthouse-write-pr/SKILL.md outside pack/, internal/ or cmd/"
    ]


def test_problems_flag_an_unknown_reader_name():
    data = mutated()
    area(data, "claude-md")["readers"] = ["Cursor"]
    assert reader_problems(data, ROOT) == ["claude-md: reader Cursor outside pack/, internal/ or cmd/"]


def test_problems_flag_force_on_lighthouse_toml():
    data = mutated()
    area(data, "lighthouse-toml")["upgrade"] += " Pass --force to overwrite."
    assert upgrade_problems(data, golden()) == ["lighthouse-toml: upgrade names --force"]


def test_problems_flag_a_missing_force():
    data = mutated()
    area(data, "claude-skills")["upgrade"] = "Replaces them."
    assert upgrade_problems(data, golden()) == ["claude-skills: upgrade does not say --force"]


def test_problems_flag_an_anchor_that_does_not_exist():
    data = mutated()
    area(data, "claude-skills")["more"] = "reference.html#nope"
    assert link_problems(data) == ["claude-skills: reference.html has no id nope"]


def repo_entries():
    return run_map(f"m.repoEntries({json.dumps(load('repo.json'))})")


def test_upgrade_zone_is_lighthouse_and_shared():
    areas = [a for room in repo_entries() for a in room["areas"]]
    assert {a["id"] for a in areas if "upgrade" in a["zones"]} == {
        a["id"] for a in load("repo.json")["areas"] if a["owner"] in {"lighthouse", "shared"}}


def test_your_code_lights_only_in_yours():
    room = next(r for r in repo_entries() if r["id"] == "yours")
    assert room["zones"] == ["all", "yours"] and room["areas"] == []


def test_entries_say_where_it_comes_from():
    facts = {a["id"]: dict(a["facts"]) for r in repo_entries() for a in r["areas"]}
    assert facts["claude-md"]["Comes from"] == "lighthouse init itself"
    assert facts["agents-md"]["Comes from"] == "core, go, python, shell"
    assert facts["claude-settings"]["Owner"] == "shared: only lighthouse's block or keys"


def test_repo_reader_links():
    read = lambda r: run_map(f"m.repoReader({json.dumps(r)})")
    assert read("Claude Code") == {"name": "Claude Code", "href": None}
    assert read("internal/hostconfig") == {"name": "internal/hostconfig", "href": REPO_TREE + "internal/hostconfig"}
    assert read("pack/core/lib/config.py")["href"] == REPO_BLOB + "pack/core/lib/config.py"


def test_every_path_reader_links_tree_for_a_dir():
    paths = sorted({r for a in load("repo.json")["areas"] for r in a["readers"] if r not in READER_NAMES})
    links = run_map(f"{json.dumps(paths)}.map((p) => m.repoReader(p).href)")
    for path, href in zip(paths, links):
        assert href == (REPO_TREE if (ROOT / path).is_dir() else REPO_BLOB) + path


LEGEND_LINES = [
    "lighthouse's: --upgrade replaces it",
    "shared: only lighthouse's block or keys",
    "yours: init leaves it alone",
    "dashed line: the zone --upgrade touches",
    "changed or deleted a file, block or hook of lighthouse's? --upgrade stops and writes nothing until you pass --force",
]
SWATCHED = {LEGEND_LINES[0]: "owner-lighthouse", LEGEND_LINES[1]: "owner-shared",
            LEGEND_LINES[2]: "owner-yours", LEGEND_LINES[3]: "touched"}


def legend_items() -> list[str]:
    html = (SITE / "repo.html").read_text(encoding="utf-8")
    legend = html.split('<section class="legend"', 1)[1].split("</section>", 1)[0]
    return [item.split("</li>")[0] for item in legend.split("<li>")[1:]]


def test_legend_lines_are_verbatim_and_each_sample_has_its_swatch():
    items = legend_items()
    assert [i.split("</span>")[-1] for i in items] == LEGEND_LINES
    for item in items[:4]:
        text = item.split("</span>")[-1]
        assert item.startswith(f'<span class="swatch {SWATCHED[text]}"></span>')
    assert "swatch" not in items[4]
