"""`lighthouse_lib.artefacts`: the metadata of artefact comments (besluit 15, 21).

Taken over from the source repository's `test_controller_artefacten.py`. The test on
that repository's own plan file now splits a plan of lighthouse itself.
"""

import dataclasses
import json
from pathlib import Path

import pytest

from lighthouse_lib.artefacts import (
    GITHUB_LIMIT,
    KINDS,
    Meta,
    content,
    current,
    hash_of,
    next_version,
    parse_meta,
    render,
    split,
)
from lighthouse_lib.config import ConfigError
from lighthouse_lib.protocol import Comment

REPO_ROOT = Path(__file__).resolve().parents[2]
PLAN = REPO_ROOT / "docs/developer/planning/plans/2026-10-18-m0b-pack-core.md"
SHA = "a" * 40
META = Meta(
    kind="plan",
    version=2,
    part=1,
    parts=4,
    file="docs/developer/planning/plans/2026-10-03-x.md",
    commit=SHA,
    hash="0123456789ab",
    replaces="https://github.com/Owner/repo/issues/1094#issuecomment-123",
)


@pytest.fixture
def config(tmp_path, monkeypatch):
    def write(body: str) -> None:
        path = tmp_path / "lighthouse.toml"
        path.write_text(body, encoding="utf-8")
        monkeypatch.setenv("LIGHTHOUSE_TOML", str(path))

    return write


@pytest.fixture(autouse=True)
def repo_config(config):
    config('[project]\nlanguage = "nl"\n[forge]\nkind = "github"\nrepo = "Owner/repo"\n')


def _json(body: str) -> dict:
    first = body.splitlines()[0]
    return json.loads(first[len("<!-- lighthouse: ") : -len(" -->")])


def test_public_names():
    assert KINDS == ("spec", "plan")
    assert GITHUB_LIMIT == 65_536


def test_roundtrip():
    body = render(META, "# Titel\n\ntekst\n")
    assert parse_meta(body) == META
    assert content(body) == "# Titel\n\ntekst\n"


def test_first_line_and_sub_line():
    lines = render(META, "x").splitlines()
    assert lines[0].startswith("<!-- lighthouse: {") and lines[0].endswith("} -->")
    assert lines[1] == (
        '<sub>plan · versie 2 · deel 1/4 · bron <a href="https://github.com/'
        f"Owner/repo/blob/{SHA}/docs/developer/planning/plans/"
        '2026-10-03-x.md">docs/developer/planning/plans/2026-10-03-x.md</a> @ aaaaaaa</sub>'
    )
    assert lines[2] == ""


def test_sub_line_follows_language(config):
    config('[project]\nlanguage = "en"\n[forge]\nrepo = "Owner/repo"\n')
    assert render(META, "x").splitlines()[1].startswith(
        "<sub>plan · version 2 · part 1/4 · source <a "
    )


def test_writes_new_marker_and_keys():
    out = render(Meta("plan", 1, 1, 1, "p.md", "a" * 40, "b" * 12, None), "x")
    assert out.startswith('<!-- lighthouse: {"kind":"plan"') and "soort" not in out
    assert list(_json(out)) == [
        "kind",
        "version",
        "part",
        "parts",
        "file",
        "commit",
        "hash",
        "replaces",
    ]


def test_reads_legacy_artefact_marker():
    meta = parse_meta(
        '<!-- paia: {"soort":"plan","versie":2,"deel":1,"delen":1,"bestand":"p.md",'
        '"commit":"' + "a" * 40 + '","hash":"' + "b" * 12 + '","vervangt":null} -->\nx'
    )
    assert meta.kind == "plan" and meta.version == 2 and meta.file == "p.md"
    assert meta.replaces is None and meta.extra == {}


def test_reads_new_keys_under_legacy_marker():
    body = render(META, "x").replace("<!-- lighthouse:", "<!-- paia:", 1)
    assert parse_meta(body) == META


def test_legacy_marker_without_replaces_is_unreadable():
    """The old writer always wrote `vervangt`; a missing key stays invalid."""
    body = (
        '<!-- paia: {"soort":"plan","versie":2,"deel":1,"delen":1,"bestand":"p.md",'
        '"commit":"' + "a" * 40 + '","hash":"' + "b" * 12 + '"} -->\nx'
    )
    assert parse_meta(body) is None


def test_unknown_fields_survive():
    meta = dataclasses.replace(META, extra={"future": [1, 2]})
    assert parse_meta(render(meta, "x")) == meta


def test_replaces_null():
    meta = dataclasses.replace(META, replaces=None)
    parsed = parse_meta(render(meta, "x"))
    assert parsed is not None
    assert parsed.replaces is None


@pytest.mark.parametrize(
    "field,value",
    [
        ("kind", "other"),
        ("version", 0),
        ("part", True),
        ("part", 5),
        ("commit", "x"),
        ("hash", "0123456789a"),
        ("hash", "0123456789AB"),
    ],
)
def test_invalid_value_gives_none(field, value):
    data = _json(render(META, "x"))
    data[field] = value
    assert parse_meta(f"<!-- lighthouse: {json.dumps(data)} -->\n<sub>x</sub>\n\ny") is None


@pytest.mark.parametrize(
    "body",
    [
        "<!-- lighthouse: {broken -->\nx",
        '<!-- lighthouse: {"kind":"plan"} -->\nx',  # required fields missing
        "<!-- paia: {broken -->\nx",
        "<!-- other: {} -->\nx",
        "ordinary comment",
        "",
    ],
)
def test_unreadable_gives_none(body):
    assert parse_meta(body) is None


def test_render_refuses_double_dash():
    with pytest.raises(ValueError):
        render(dataclasses.replace(META, file="docs/developer/planning/plans/a--b.md"), "x")


def test_render_without_repo_is_a_config_error(config):
    config('[project]\nlanguage = "nl"\n')
    with pytest.raises(ConfigError, match="forge.repo"):
        render(META, "x")


def test_hash_of():
    assert hash_of(b"abc") == "ba7816bf8f01"


def test_fits_in_one_part():
    assert split("kort\n") == ["kort\n"]


def test_empty_file_is_one_empty_part():
    assert split("") == [""]


def test_cuts_before_an_h2():
    text = "## a\n" + "b\n" * 30 + "## c\n" + "d\n" * 30
    parts = split(text, limit=70)
    assert parts[0] == "## a\n" + "b\n" * 30
    assert all(parts) and "".join(parts) == text


def test_no_cut_at_offset_zero():
    text = "## a\n" + "b" * 10 + "\n" + "c" * 10 + "\n"
    parts = split(text, limit=20)
    assert all(parts) and "".join(parts) == text
    assert parts[0].startswith("## a\n")


def test_not_in_a_code_block_when_there_is_another_cut_point():
    block = "````md\n```\n" + "## binnen\n\n" * 5 + "```\n````\n"
    text = "intro\n\n" + block + "\n## na\n"
    parts = split(text, limit=len("intro\n\n") + len(block) - 5)
    assert parts[0] == "intro\n\n"
    assert "".join(parts) == text


def test_tilde_fence_and_indented_fence():
    block = "   ~~~\n## binnen\n~~~\n"
    text = "a\n\n" + block + "z\n"
    assert split(text, limit=len("a\n\n") + len(block) - 2)[0] == "a\n\n"


def test_line_longer_than_the_limit_cuts_mid_line():
    assert split("x" * 25, limit=10) == ["x" * 10, "x" * 10, "x" * 5]


@pytest.mark.parametrize("text", ["a\r\nb\r\n\r\n## c\r\n" * 20, "a\n\n## b\nzonder slot"])
def test_roundtrip_crlf_and_without_final_newline(text):
    parts = split(text, limit=15)
    assert all(parts) and "".join(parts) == text


def test_a_real_plan():
    text = PLAN.read_text(encoding="utf-8")
    parts = split(text, limit=len(text) // 4 + 1)
    assert len(parts) >= 4
    assert "".join(parts) == text
    assert all(parts)
    meta = Meta(
        kind="plan",
        version=99,
        part=99,
        parts=99,
        file=str(PLAN.relative_to(REPO_ROOT)),
        commit="a" * 40,
        hash="0" * 12,
        replaces="https://github.com/Owner/repo/issues/1#issuecomment-1234567890",
    )
    assert all(len(render(meta, p)) < GITHUB_LIMIT for p in split(text))


SPEC_A = "docs/developer/planning/specs/a.md"
SPEC_B = "docs/developer/planning/specs/b.md"


def part(cid, version, p, n, file=SPEC_A, kind="spec"):
    meta = Meta(
        kind=kind,
        version=version,
        part=p,
        parts=n,
        file=file,
        commit=SHA,
        hash="0" * 12,
        replaces=None,
    )
    return Comment(id=cid, body=render(meta, f"v{version}d{p}"), created_at="", url=f"u{cid}")


def test_highest_complete_version_beats_an_incomplete_higher_one():
    cs = [part(1, 1, 1, 2), part(2, 1, 2, 2), part(3, 2, 1, 2)]
    assert [c.id for c in current(cs, "spec", SPEC_A)] == [1, 2]
    assert next_version(cs, "spec", SPEC_A) == 3


def test_parts_in_order_even_when_mixed():
    cs = [part(2, 1, 2, 2), part(1, 1, 1, 2)]
    assert [c.id for c in current(cs, "spec", SPEC_A)] == [1, 2]


def test_double_part_makes_a_version_incomplete():
    cs = [part(1, 1, 1, 1), part(2, 2, 1, 2), part(3, 2, 1, 2), part(4, 2, 2, 2)]
    assert [c.id for c in current(cs, "spec", SPEC_A)] == [1]


def test_unequal_parts_make_a_version_incomplete():
    cs = [part(1, 1, 1, 2), part(2, 1, 2, 3)]
    assert current(cs, "spec", SPEC_A) == []


def test_two_chains_do_not_see_each_other():
    cs = [part(1, 1, 1, 1), part(2, 1, 1, 1, file=SPEC_B), part(3, 2, 1, 1, file=SPEC_B)]
    assert [c.id for c in current(cs, "spec", SPEC_A)] == [1]
    assert next_version(cs, "spec", SPEC_A) == 2
    assert next_version(cs, "spec", SPEC_B) == 3


def test_other_kind_and_ordinary_comments_do_not_count():
    cs = [part(1, 5, 1, 1, kind="plan"), Comment(id=2, body="hoi", created_at="")]
    assert current(cs, "spec", SPEC_A) == []
    assert next_version(cs, "spec", SPEC_A) == 1


def test_legacy_and_new_parts_form_one_chain():
    """A version posted before the move counts in the chain of the new writer."""
    legacy = Comment(
        id=1,
        body=part(1, 1, 1, 1).body.replace("<!-- lighthouse:", "<!-- paia:", 1),
        created_at="",
    )
    assert [c.id for c in current([legacy], "spec", SPEC_A)] == [1]
    assert next_version([legacy], "spec", SPEC_A) == 2


def test_render_source_link_follows_config(config):
    config('[forge]\nrepo = "Other/repo"\n')
    body = render(META, "x")
    assert 'source <a href="https://github.com/Other/repo/blob/' in body
    assert "Owner" not in body.splitlines()[1]
