"""`pack/core/lib/md_links.py`, as installed (#886): shared link extraction for markdown.

One module for the norm "links in files are relative and point to an existing
target; absolute links are a problem". Used by the rewritten
`md-link-check.sh` (task 5, through the CLI) and by the docs link tests
(task 6, through `strip_code`, `relative_links` and `absolute_links`); the
latter replaces its own `_strip_code_fences` in `test_docs_planning.py` with
this module.
"""

import os
import subprocess
import sys

import pytest

from tests.pack._testkit import INSTALLED_LIB, load_hook_lib

ml = load_hook_lib("md_links")
FENCE = "`" * 3  # a literal three backticks in this plan breaks the old link hook


def test_strip_code_removes_both_fence_styles_and_inline_code():
    text = (
        f"a\n{FENCE}\n[x](f1.md)\n{FENCE}\n~~~markdown\n"
        f"[y](f2.md)\n~~~\n`[z](f3.md)` [w](ok.md)\n"
    )
    assert ml.relative_links(text) == ["ok.md"]


def test_fence_marker_inside_a_line_does_not_toggle():
    text = f's = "{FENCE}"\n[x](echt.md)\n'
    assert ml.relative_links(text) == ["echt.md"]


@pytest.mark.parametrize(
    "link,expected",
    [
        ("[a](b.md)", ["b.md"]),
        ("[a](b.md#kop)", ["b.md"]),
        ("[a](#kop)", []),
        ("[a](https://x.nl)", []),
        ("[a](obsidian://x)", []),
        ("[a](mailto:iemand)", []),
        ("[a](/abs.md)", []),
        ('[a](c.md "titel")', ["c.md"]),
        ("[a](b.md) [c](b.md)", ["b.md"]),
        ("[a](<pad met spaties.md>)", ["pad met spaties.md"]),
        ("[a](<p.md> 'titel')", ["p.md"]),
        ("[a](d.md 'titel')", ["d.md"]),
        ("[a](e.md (titel))", ["e.md"]),
        ('[a](f.md "titel") [b](g.md (t))', ["f.md", "g.md"]),
    ],
)
def test_relative_links(link, expected):
    assert ml.relative_links(link) == expected


def test_absolute_links_in_new_link_forms():
    text = "[a](</home/x y.md>) [b](/abs.md 'titel') [c](/p.md (titel))"
    assert ml.absolute_links(text) == ["/home/x y.md", "/abs.md", "/p.md"]


def test_find_problems_angle_link_with_spaces(tmp_path):
    (tmp_path / "a b.md").write_text("x")
    doc = tmp_path / "doc.md"
    doc.write_text("[ok](<a b.md>) [weg](<c d.md>) [t](weg.md 'titel')\n")
    assert ml.find_problems(doc, tmp_path) == [("dead", "c d.md"), ("dead", "weg.md")]


def test_absolute_links_outside_code_only():
    text = f"[a](/home/x.md)\n{FENCE}\n[b](/home/y.md)\n{FENCE}\n"
    assert ml.absolute_links(text) == ["/home/x.md"]


def test_find_problems(tmp_path):
    (tmp_path / "docs").mkdir()
    (tmp_path / "docs" / "er.md").write_text("x")
    md = tmp_path / "docs" / "n.md"
    md.write_text("[a](er.md) [b](weg.md) [c](/home/z.md) [d](../../buiten.md)\n")
    assert ml.find_problems(md, tmp_path) == [
        ("dead", "weg.md"),
        ("absolute", "/home/z.md"),
    ]


def test_find_problems_accepts_existing_directory(tmp_path):
    (tmp_path / "docs").mkdir()
    (tmp_path / "docs" / "sub").mkdir()
    md = tmp_path / "docs" / "n.md"
    md.write_text("[h](sub/)\n")
    assert ml.find_problems(md, tmp_path) == []


def test_find_problems_decodes_percent_escapes(tmp_path):
    (tmp_path / "docs").mkdir()
    (tmp_path / "docs" / "a b.md").write_text("x")
    md = tmp_path / "docs" / "n.md"
    md.write_text("[x](a%20b.md)\n")
    assert ml.find_problems(md, tmp_path) == []


def test_cli_prints_tab_lines_and_fails_on_unreadable(tmp_path):
    (tmp_path / "docs").mkdir()
    md = tmp_path / "docs" / "n.md"
    md.write_text("[b](weg.md)\n")
    result = subprocess.run(
        [sys.executable, str(INSTALLED_LIB / "md_links.py"), str(md), str(tmp_path)],
        capture_output=True,
        text=True,
        check=False,
    )
    assert result.returncode == 0
    assert "dead\tweg.md" in result.stdout

    if os.geteuid() == 0:
        pytest.skip("root leest ook chmod 000 — geen zinvolle toets")

    md.chmod(0o000)
    try:
        bad = subprocess.run(
            [
                sys.executable,
                str(INSTALLED_LIB / "md_links.py"),
                str(md),
                str(tmp_path),
            ],
            capture_output=True,
            text=True,
            check=False,
        )
        assert bad.returncode == 1
        assert "could not read" in bad.stderr
    finally:
        md.chmod(0o644)
