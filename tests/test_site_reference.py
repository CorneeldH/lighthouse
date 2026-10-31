"""The reference page lists every skill, agent, hook and workflow of pack/, each linking to its place
in the repository."""

import json

from tests._site_node import run_map
from tests.test_site_chain import REPO_BLOB, REPO_TREE, ROOT, SITE, load

PARTS = ["skills", "agents", "hooks", "workflows"]


def pack_parts() -> set[str]:
    """Every skill, agent, workflow and hook script under pack/, repo-relative."""
    pack = ROOT / "pack"
    parts = [p for p in pack.glob("*/skills/*") if p.is_dir()]
    parts += [*pack.glob("*/agents/*.md"), *pack.glob("*/workflows/*"), *pack.glob("*/hooks/*.sh")]
    return {p.relative_to(ROOT).as_posix() for p in parts}


def reference_paths() -> set[str]:
    """The repo paths the reference page links, from referenceEntries over packs.json."""
    packs = json.dumps(load("packs.json"))
    found = set()
    for part in PARTS:
        for entry in run_map(f"m.referenceEntries({packs}, {json.dumps(part)})"):
            for item in entry["items"]:
                href = item["href"]
                prefix = next(p for p in (REPO_BLOB, REPO_TREE) if href.startswith(p))
                found.add(href[len(prefix):])
    return found


def test_every_pack_part_is_on_the_reference():
    assert pack_parts() - reference_paths() == set()


def test_every_reference_link_exists():
    assert [p for p in sorted(reference_paths()) if not (ROOT / p).exists()] == []


def test_reference_marks_standard_skills():
    entries = run_map(f"m.referenceEntries({json.dumps(load('packs.json'))}, 'skills')")
    sorts = {i["name"]: i["sort"] for e in entries for i in e["items"]}
    assert sorts["test-standard"] == "standard-skill"
    assert sorts["place-instruction"] == "workflow-skill"


def test_reference_page_has_the_four_anchors():
    html = (SITE / "reference.html").read_text(encoding="utf-8")
    for anchor in ["skills", "agents", "hooks", "github"]:
        assert f'id="{anchor}"' in html, anchor
    for target in ["tree/main/pack/core/templates", "tree/main/pack/core/workflows"]:
        assert target in html, target
