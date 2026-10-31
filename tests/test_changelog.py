"""ci/changelog.py: the fragment parser and the `check` command."""

import subprocess
from pathlib import Path

import pytest

from tests._changelog import load
from tests.pack._testkit import git

changelog = load()


def _head(root: Path) -> str:
    return subprocess.run(
        ["git", "-C", str(root), "rev-parse", "HEAD"],
        check=True, capture_output=True, text=True,
    ).stdout.strip()


def _commit(root: Path, message: str) -> str:
    git(root, "add", "-A")
    git(root, "commit", "-qm", message)
    return _head(root)


def _check(root: Path, base: str, head: str) -> int:
    return changelog.main(["check", "--base", base, "--head", head, "--root", str(root)])


@pytest.fixture
def git_repo(tmp_path: Path) -> tuple[Path, str]:
    """A repo whose commit 1 holds changelog.d/.lighthouse and changelog.d/old.md."""
    root = tmp_path / "repo"
    (root / "changelog.d").mkdir(parents=True)
    (root / "changelog.d" / ".lighthouse").write_text("marker\n", encoding="utf-8")
    (root / "changelog.d" / "old.md").write_text("---\ntype: fixed\n---\nOld.\n", encoding="utf-8")
    (root / "code.go").write_text("package a\n", encoding="utf-8")
    git(root, "init", "-q", "-b", "main")
    return root, _commit(root, "one")


GOOD = "---\ntype: added\n---\nNew skill.\n"


@pytest.mark.parametrize("body,typ,text", [
    ("---\ntype: fixed\n---\nNo more rewrites.\n", "fixed", "No more rewrites."),
    ("\ufeff---\r\ntype: added\r\n---\r\nNew skill.\r\n", "added", "New skill."),
])
def test_parse_fragment_accepts(tmp_path, body, typ, text):
    """Fails when a valid fragment (also CRLF with a BOM) is refused or parsed wrongly."""
    path = tmp_path / "x.md"
    path.write_bytes(body.encode("utf-8"))
    assert changelog.parse_fragment(path) == changelog.Fragment(path, typ, text)


@pytest.mark.parametrize("body,reason", [
    ("type: fixed\n---\nx\n", "first line must be exactly"),
    ("---\ntype: fixed\nx\n", "not closed"),
    ("---\ntype: fixed\n---\n   \n", "empty text"),
    ("---\ntype: bugfix\n---\nx\n", "unknown type"),
    ("---\ntype: fixed\ntype: added\n---\nx\n", "exactly one line"),
    ("---\ntype: fixed\nscope: cli\n---\nx\n", "exactly one line"),
    ("---\nscope: cli\n---\nx\n", "unknown front matter line"),
    ("---\n---\nx\n", "exactly one line"),
])
def test_parse_fragment_refuses(tmp_path, body, reason):
    """Fails when a malformed fragment is accepted, or refused for the wrong reason, or the error omits the path."""
    path = tmp_path / "broken.md"
    path.write_text(body, encoding="utf-8")
    with pytest.raises(changelog.FragmentError) as err:
        changelog.parse_fragment(path)
    assert "broken.md" in str(err.value)
    assert reason in str(err.value)


def test_read_fragments_skips_marker_and_sorts(tmp_path):
    """Fails when the marker is parsed or the order is not by file name."""
    for name in (".lighthouse", "b.md", "a.md"):
        (tmp_path / name).write_text("---\ntype: fixed\n---\nx\n", encoding="utf-8")
    (tmp_path / ".lighthouse").write_text("not a fragment\n", encoding="utf-8")
    assert [f.path.name for f in changelog.read_fragments(tmp_path)] == ["a.md", "b.md"]


def test_read_fragments_missing_directory(tmp_path):
    """Fails when a missing directory raises instead of giving no fragments."""
    assert changelog.read_fragments(tmp_path / "nope") == []


def test_read_fragments_refuses_other_files(tmp_path):
    """Fails when a stray non-markdown file is ignored."""
    (tmp_path / "notes.txt").write_text("x\n", encoding="utf-8")
    with pytest.raises(changelog.FragmentError) as err:
        changelog.read_fragments(tmp_path)
    assert "notes.txt" in str(err.value)


def test_check_requires_an_added_fragment(git_repo, capsys):
    """Fails when a PR without a new fragment passes."""
    root, c1 = git_repo
    (root / "code.go").write_text("package b\n", encoding="utf-8")
    assert _check(root, c1, _commit(root, "two")) == 1
    assert "no new fragment" in capsys.readouterr().err


def test_check_ignores_modified_fragment(git_repo):
    """Fails when editing an existing fragment counts as a new one."""
    root, c1 = git_repo
    (root / "changelog.d" / "old.md").write_text("---\ntype: added\n---\nEdited.\n", encoding="utf-8")
    assert _check(root, c1, _commit(root, "two")) == 1


def test_check_counts_renamed_fragment(git_repo):
    """Fails when a renamed fragment is not counted as added under its new name."""
    root, c1 = git_repo
    git(root, "mv", "changelog.d/old.md", "changelog.d/new.md")
    assert _check(root, c1, _commit(root, "two")) == 0


def test_check_ignores_added_marker(tmp_path):
    """Fails when adding only the marker file counts as a fragment."""
    root = tmp_path / "repo"
    root.mkdir()
    (root / "code.go").write_text("package a\n", encoding="utf-8")
    git(root, "init", "-q", "-b", "main")
    c1 = _commit(root, "one")
    (root / "changelog.d").mkdir()
    (root / "changelog.d" / ".lighthouse").write_text("marker\n", encoding="utf-8")
    assert _check(root, c1, _commit(root, "two")) == 1


def test_check_refuses_bad_fragment_anywhere(git_repo, capsys):
    """Fails when a broken fragment passes because another one is fine."""
    root, c1 = git_repo
    (root / "changelog.d" / "old.md").write_text("garbage\n", encoding="utf-8")
    (root / "changelog.d" / "new.md").write_text(GOOD, encoding="utf-8")
    assert _check(root, c1, _commit(root, "two")) == 1
    assert "old.md" in capsys.readouterr().err


def test_check_passes(git_repo):
    """Fails when a PR with a valid new fragment is refused."""
    root, c1 = git_repo
    (root / "changelog.d" / "new.md").write_text(GOOD, encoding="utf-8")
    assert _check(root, c1, _commit(root, "two")) == 0


def test_check_bad_base_cannot_check(git_repo, capsys):
    """Fails when a git failure is reported as a verdict instead of COULD NOT CHECK."""
    root, c1 = git_repo
    assert _check(root, "deadbeef", c1) == 2
    assert "COULD NOT CHECK" in capsys.readouterr().err


def test_added_fragments_non_utf8_name_cannot_check(git_repo, capsys):
    """Fails when an undecodable file name escapes as a traceback instead of COULD NOT CHECK."""
    root, c1 = git_repo
    bad = bytes(root / "changelog.d") + b"/\xff.md"
    with open(bad, "w", encoding="utf-8") as fh:
        fh.write(GOOD)
    c2 = _commit(root, "two")
    assert _check(root, c1, c2) == 2
    assert "COULD NOT CHECK" in capsys.readouterr().err


# --- release: bump, version, CHANGELOG.md, notes ---


def _frag(typ: str, text: str = "Entry.", name: str = "f.md"):
    return changelog.Fragment(Path(name), typ, text)


@pytest.mark.parametrize("types,event,forced,want", [
    ([], "push", "auto", None),
    (["internal"], "push", "auto", None),
    (["fixed"], "push", "auto", "patch"),
    (["fixed", "internal"], "push", "auto", "patch"),
    (["added"], "push", "auto", None),
    (["fixed", "added"], "push", "auto", "minor"),
    (["added"], "schedule", "auto", "minor"),
    (["fixed"], "schedule", "auto", "patch"),
    (["internal"], "schedule", "auto", None),
    (["changed"], "workflow_dispatch", "auto", "minor"),
    (["fixed"], "workflow_dispatch", "minor", "minor"),
    (["added"], "workflow_dispatch", "major", "major"),
])
def test_decide_bump(types, event, forced, want):
    """Fails when the bump rule for an event or a forced bump is wrong."""
    assert changelog.decide_bump([_frag(t) for t in types], event, forced) == want


@pytest.mark.parametrize("types,forced", [(["added"], "patch"), ([], "major"), (["internal"], "minor")])
def test_decide_bump_refuses(types, forced):
    """Fails when a too-low or empty forced bump releases anyway."""
    with pytest.raises(changelog.ReleaseError):
        changelog.decide_bump([_frag(t) for t in types], "workflow_dispatch", forced)


def test_latest_tag_ignores_prerelease_and_sorts_numerically():
    """Fails when a prerelease tag wins or tags sort as strings."""
    tags = ["v0.1.6", "v0.1.10", "v0.1.9", "v0.2.0-rc.1", "vfoo"]
    assert changelog.latest_tag(tags) == "v0.1.10"


@pytest.mark.parametrize("tags", [[], ["v0.2.0-rc.1"]])
def test_latest_tag_without_tags(tags):
    """Fails when a repo without a release tag does not raise TagError."""
    with pytest.raises(changelog.TagError):
        changelog.latest_tag(tags)


@pytest.mark.parametrize("bump,want", [("patch", "v0.1.7"), ("minor", "v0.2.0"), ("major", "v1.0.0")])
def test_next_version(bump, want):
    """Fails when a bump resets the wrong components."""
    assert changelog.next_version("v0.1.6", bump) == want


def test_render_section_groups_in_order_and_drops_internal():
    """Fails when the group order is wrong or an internal fragment leaks in."""
    frags = [_frag("removed", "Gone."), _frag("internal", "Secret."), _frag("added", "New."),
             _frag("fixed", "Fix.")]
    assert changelog.render_section("v0.2.0", "2026-10-27", frags) == (
        "## [v0.2.0] - 2026-10-27\n\n### Added\n\n- New.\n\n### Fixed\n\n- Fix.\n\n"
        "### Removed\n\n- Gone.\n"
    )


def test_render_section_indents_continuation_lines():
    """Fails when a multi-line entry breaks out of its list item."""
    assert "- a\n  b\n" in changelog.render_section("v0.2.0", "2026-10-27", [_frag("added", "a\nb")])


PREAMBLE = "# Changelog\n\nIntro.\n\n"
OLD = "## [v0.1.6] - 2026-01-01\n\n### Fixed\n\n- Old.\n"


def test_insert_section_goes_above_latest():
    """Fails when the section lands below the latest release or inside the preamble."""
    section = changelog.render_section("v0.2.0", "2026-10-27", [_frag("added", "New.")])
    out = changelog.insert_section(PREAMBLE + OLD, section)
    assert out.index("Intro.") < out.index("## [v0.2.0]") < out.index("## [v0.1.6]")


def test_insert_section_without_release_goes_last():
    """Fails when a changelog without a release heading loses the section."""
    out = changelog.insert_section("# Changelog\n", "## [v0.2.0] - d\n\n- x\n")
    assert out.startswith("# Changelog\n") and out.endswith("- x\n")


def test_extract_section_roundtrip():
    """Fails when extract does not return exactly the body of the inserted section."""
    section = changelog.render_section("v0.2.0", "2026-10-27", [_frag("added", "New.")])
    out = changelog.insert_section(PREAMBLE + OLD, section)
    assert changelog.extract_section(out, "v0.2.0") == "### Added\n\n- New."
    assert changelog.extract_section(out, "v0.1.6") == "### Fixed\n\n- Old."


def test_extract_section_missing():
    """Fails when a tag without a section returns text instead of None."""
    assert changelog.extract_section(PREAMBLE + OLD, "v0.2.0") is None


def _release_root(tmp_path: Path, fragments: dict[str, str], changelog_text: str | None = PREAMBLE + OLD) -> Path:
    root = tmp_path / "proj"
    (root / "changelog.d").mkdir(parents=True)
    (root / "changelog.d" / ".lighthouse").write_text("marker\n", encoding="utf-8")
    for name, typ in fragments.items():
        (root / "changelog.d" / name).write_text(f"---\ntype: {typ}\n---\n{name} text.\n", encoding="utf-8")
    if changelog_text is not None:
        (root / "CHANGELOG.md").write_text(changelog_text, encoding="utf-8")
    return root


def test_release_writes_changelog_and_removes_fragments(tmp_path):
    """Fails when release skips the changelog, keeps a fragment, or removes the marker."""
    root = _release_root(tmp_path, {"a.md": "fixed", "b.md": "added", "c.md": "internal"})
    assert changelog.release(root, "push", "auto", "2026-10-27", ["v0.1.6"]) == "v0.2.0"
    text = (root / "CHANGELOG.md").read_text(encoding="utf-8")
    assert "## [v0.2.0] - 2026-10-27" in text
    assert "c.md text." not in text
    assert [p.name for p in (root / "changelog.d").iterdir()] == [".lighthouse"]


def test_release_nothing_leaves_tree_untouched(tmp_path):
    """Fails when a no-release run still writes or deletes."""
    root = _release_root(tmp_path, {"a.md": "added"})
    before = (root / "CHANGELOG.md").read_bytes()
    assert changelog.release(root, "push", "auto", "2026-10-27", ["v0.1.6"]) is None
    assert (root / "CHANGELOG.md").read_bytes() == before
    assert (root / "changelog.d" / "a.md").exists()


def test_release_without_changelog_refuses(tmp_path):
    """Fails when release without CHANGELOG.md succeeds or drops the fragment."""
    root = _release_root(tmp_path, {"a.md": "fixed"}, changelog_text=None)
    with pytest.raises(changelog.ReleaseError):
        changelog.release(root, "push", "auto", "2026-10-27", ["v0.1.6"])
    assert (root / "changelog.d" / "a.md").exists()


def test_release_without_tags_raises_tag_error(tmp_path):
    """Fails when the tag lookup does not come before the other checks."""
    root = _release_root(tmp_path, {"a.md": "fixed"}, changelog_text=None)
    with pytest.raises(changelog.TagError):
        changelog.release(root, "push", "auto", "2026-10-27", [])


def test_cli_release_without_tags_exits_2(tmp_path, capsys):
    """Fails when a checkout without v* tags releases v0.0.1 or exits other than 2."""
    root = _release_root(tmp_path, {"a.md": "fixed"})
    git(root, "init", "-q", "-b", "main")
    assert changelog.main(["release", "--event", "push", "--root", str(root)]) == 2
    assert capsys.readouterr().err
    assert (root / "changelog.d" / "a.md").exists()


def test_cli_release_prints_tag(tmp_path, capsys):
    """Fails when the CLI does not print the new tag on stdout."""
    root = _release_root(tmp_path, {"a.md": "fixed"})
    git(root, "init", "-q", "-b", "main")
    _commit(root, "one")
    git(root, "tag", "v0.1.6")
    assert changelog.main(["release", "--event", "push", "--date", "2026-10-27", "--root", str(root)]) == 0
    assert capsys.readouterr().out.strip() == "v0.1.7"


def test_cli_release_error_exits_1(tmp_path):
    """Fails when a forced bump without a releasable fragment does not exit 1."""
    root = _release_root(tmp_path, {})
    git(root, "init", "-q", "-b", "main")
    _commit(root, "one")
    git(root, "tag", "v0.1.6")
    assert changelog.main(["release", "--event", "workflow_dispatch", "--bump", "minor",
                           "--root", str(root)]) == 1


def test_cli_notes_prints_section(tmp_path, capsys):
    """Fails when notes does not print the section body."""
    root = _release_root(tmp_path, {})
    assert changelog.main(["notes", "v0.1.6", "--root", str(root)]) == 0
    assert capsys.readouterr().out.strip() == "### Fixed\n\n- Old."


def test_cli_notes_missing_section_exits_1(tmp_path):
    """Fails when a tag without a section exits other than 1."""
    root = _release_root(tmp_path, {})
    assert changelog.main(["notes", "v0.2.0", "--root", str(root)]) == 1


def test_read_fragments_orders_numbered_suffixes_after_the_base(tmp_path):
    """Fails when `x-2.md` sorts before `x.md`, or `x-10.md` before `x-3.md`."""
    names = ["x-10.md", "x-2.md", "y.md", "x.md", "x-3.md"]
    for name in names:
        (tmp_path / name).write_text("---\ntype: fixed\n---\nx\n", encoding="utf-8")
    assert [f.path.name for f in changelog.read_fragments(tmp_path)] == [
        "x.md", "x-2.md", "x-3.md", "x-10.md", "y.md",
    ]
