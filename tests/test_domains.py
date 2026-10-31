"""ci/domains.py: which domains a change touches."""

import io
import re
import subprocess
import sys
from pathlib import Path

import pytest

from tests._domains import load

ROOT = Path(__file__).resolve().parents[1]
domains = load()

# A string literal that climbs out of its package (`../`, once or more) into tests/ (not the
# fixtures), README.md or CHANGELOG.md.
_REPO_READ = re.compile(
    r"[\"`]((?:\.\./)+(?:tests/(?!pack/fixtures/)[^\"`]*|README\.md|CHANGELOG\.md))[\"`]"
)


def _repo_reads(text: str) -> list[str]:
    """The paths of this repo that Go source `text` reads from above its package directory."""
    return _REPO_READ.findall(text)


@pytest.mark.parametrize(
    ("paths", "expected"),
    [
        (["web/src/x.tsx"], {"web"}),
        (["internal/a.go"], {"go", "python"}),
        (["web/openapi.json"], {"web", "go"}),
        (["web/go.mod"], {"web", "go"}),
        (["docs/x.md"], {"python"}),
        (["changelog.d/x.md"], {"python"}),
        (["ci/literal-denylist.txt"], {"python"}),
        (["ci/literal-allowlist.txt"], {"python"}),
        (["ci/x.sh"], {"go", "python"}),
        (["docsx/x.md"], {"go", "python"}),
        (["docs/x.md", "internal/a.go"], {"go", "python"}),
        ([".github/workflows/ci.yml"], {"web", "go", "python"}),
        (["ci/domains.py"], {"web", "go", "python"}),
        (["lighthouse.toml"], {"web", "go", "python"}),
        (["webapp/x.ts"], {"go", "python"}),
        (["tests/test_x.py"], {"python"}),
        (["README.md"], {"python"}),
        (["CHANGELOG.md"], {"python"}),
        (["tests/test_x.py", "README.md"], {"python"}),
        (["tests/pack/fixtures/protocol/headers.json"], {"go", "python"}),
        (["pack/core/README.md"], {"go", "python"}),
        (["testsx/a.py"], {"go", "python"}),
        ([], set()),
    ],
)
def test_classify(paths, expected):
    assert domains.classify(paths) == expected


def test_no_go_file_names_a_path_outside_go():
    """Fails when Go code starts to read a path classify keeps out of go."""
    out = subprocess.run(
        ["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", "*.go"],
        cwd=ROOT,
        check=True,
        capture_output=True,
        text=True,
    ).stdout
    files = [rel for rel in out.split("\0") if rel]
    assert files, "git ls-files found no .go file"
    named = [
        f"{rel}: {needle}"
        for rel in files
        for needle in ("docs/", "changelog.d", "ci/literal-")
        if needle in (ROOT / rel).read_text(encoding="utf-8", errors="replace")
    ]
    assert not named, f"Go names a path outside the go domain in ci/domains.py: {named}"


def test_repo_reads_finds_a_read_above_the_package():
    text = 'os.ReadFile("../../tests/x.py")\nos.ReadFile(`../README.md`)\n"../../CHANGELOG.md"'
    assert _repo_reads(text) == ["../../tests/x.py", "../README.md", "../../CHANGELOG.md"]


def test_repo_reads_skips_fixtures_temp_repos_and_inner_dotdot():
    text = (
        'os.ReadFile("../../tests/pack/fixtures/protocol/headers.json")\n'
        'writeFile(t, repo, "tests/test_x.py", "")\n'
        'filepath.Join(repo, "README.md")\n'
        'unitText("file", ".claude/hooks/lighthouse/../../../README.md", "")\n'
    )
    assert _repo_reads(text) == []


def test_no_go_file_reads_tests_or_root_md():
    """Fails when Go code starts to read tests/ (outside the fixtures), README.md or CHANGELOG.md.

    A Go test reads this repo from its own package directory, so through `../`; go:embed cannot
    reach above its module. A path built from parts (`filepath.Join(root, "tests", ...)`) is not
    seen, as for docs/ above.
    """
    out = subprocess.run(
        ["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", "*.go"],
        cwd=ROOT,
        check=True,
        capture_output=True,
        text=True,
    ).stdout
    files = [rel for rel in out.split("\0") if rel]
    assert files, "git ls-files found no .go file"
    reads = [
        f"{rel}: {path}"
        for rel in files
        for path in _repo_reads((ROOT / rel).read_text(encoding="utf-8", errors="replace"))
    ]
    assert not reads, f"Go reads a path outside the go domain in ci/domains.py: {reads}"


def test_classify_cli_github_output(tmp_path, monkeypatch, capsys):
    out_file = tmp_path / "github_output"
    out_file.write_text("earlier=1\n", encoding="utf-8")
    monkeypatch.setattr(sys, "stdin", io.TextIOWrapper(io.BytesIO(b"web/x.tsx\0internal/y.go\0")))
    assert domains.main(["classify", "--github-output", str(out_file)]) == 0
    lines = out_file.read_text(encoding="utf-8").splitlines()
    assert lines[0] == "earlier=1"
    assert {"web=true", "go=true", "python=true"} <= set(lines)
    assert "web=true" in capsys.readouterr().out.splitlines()


def test_classify_cli_docs_only_pr_skips_go(monkeypatch, capsys):
    """The paths of #127: docs, a changelog fragment and the denylist."""
    paths = (
        b"changelog.d/fix-labels-kleine-punten.md\0ci/literal-denylist.txt\0"
        b"docs/developer/labels.md\0"
    )
    monkeypatch.setattr(sys, "stdin", io.TextIOWrapper(io.BytesIO(paths)))
    assert domains.main(["classify"]) == 0
    assert capsys.readouterr().out.splitlines() == ["web=false", "python=true", "go=false"]


def test_classify_cli_tests_only_pr_skips_go(monkeypatch, capsys):
    """A PR of #151's kind: a pytest test, the root README and a fragment."""
    paths = b"tests/test_site_pages.py\0README.md\0changelog.d/x.md\0"
    monkeypatch.setattr(sys, "stdin", io.TextIOWrapper(io.BytesIO(paths)))
    assert domains.main(["classify"]) == 0
    assert capsys.readouterr().out.splitlines() == ["web=false", "python=true", "go=false"]


def test_classify_cli_false(monkeypatch, capsys):
    monkeypatch.setattr(sys, "stdin", io.TextIOWrapper(io.BytesIO(b"web/x.tsx\0")))
    assert domains.main(["classify"]) == 0
    assert capsys.readouterr().out.splitlines() == ["web=true", "python=false", "go=false"]


def test_gate_subcommand_is_gone():
    with pytest.raises(SystemExit) as exc:
        domains.main(["gate"])
    assert exc.value.code == 2
