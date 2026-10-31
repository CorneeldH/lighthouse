"""ci/domains.py: which domains a change touches, and a gate that runs only their checks."""

import io
import shutil
import subprocess
import sys
from pathlib import Path

import pytest

from tests._domains import load
from tests.pack._testkit import git

ROOT = Path(__file__).resolve().parents[1]
domains = load()


@pytest.fixture
def repo(tmp_path: Path) -> Path:
    """A git repo with one commit: internal/a.go, web/x.tsx and README.md."""
    root = tmp_path / "repo"
    (root / "internal").mkdir(parents=True)
    (root / "web").mkdir()
    (root / "internal" / "a.go").write_text("package a\n", encoding="utf-8")
    (root / "web" / "x.tsx").write_text("export {}\n", encoding="utf-8")
    (root / "README.md").write_text("# repo\n", encoding="utf-8")
    git(root, "init", "-q", "-b", "main")
    git(root, "add", ".")
    git(root, "commit", "-qm", "init")
    return root


@pytest.fixture
def web_ready(repo: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """web/node_modules exists, and node and npm are on PATH."""
    (repo / "web" / "node_modules").mkdir()
    real_which = shutil.which

    def which(name, *args, **kwargs):
        if name in ("node", "npm"):
            return f"/fake/bin/{name}"
        return real_which(name, *args, **kwargs)

    monkeypatch.setattr(shutil, "which", which)


@pytest.fixture
def markers(tmp_path: Path) -> Path:
    path = tmp_path / "markers"
    path.mkdir()
    return path


def _touching(markers: Path) -> dict[str, list[list[str]]]:
    """The web check leaves a marker named after the domain."""
    return {"web": [["sh", "-c", f"touch '{markers / 'web'}'"]]}


def _ran(markers: Path) -> set[str]:
    return {p.name for p in markers.iterdir()}


def _no_drift(web: Path) -> int:
    return 0


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


def test_checks_hold_only_web():
    assert set(domains.CHECKS) == {"web"}


def test_gate_untracked_non_web_file_skips(repo, markers, capsys):
    (repo / "internal" / "new.go").write_text("package a\n", encoding="utf-8")
    assert domains.gate(repo, checks=_touching(markers), drift=_no_drift) == 0
    assert "gate: skipped — the commit touches no web path" in capsys.readouterr().out
    assert _ran(markers) == set()


def test_gate_unstaged_web_change_counts(repo, markers, web_ready):
    (repo / "web" / "x.tsx").write_text("export const x = 1\n", encoding="utf-8")
    assert domains.gate(repo, checks=_touching(markers), drift=_no_drift) == 0
    assert _ran(markers) == {"web"}


def test_gate_rename_into_web_runs_web(repo, markers, web_ready):
    git(repo, "mv", "internal/a.go", "web/a.go")
    assert domains.gate(repo, checks=_touching(markers), drift=_no_drift) == 0
    assert _ran(markers) == {"web"}


def test_gate_quoted_path_is_web(repo, markers, web_ready):
    (repo / "web" / "café ü.tsx").write_text("export {}\n", encoding="utf-8")
    assert domains.gate(repo, checks=_touching(markers), drift=_no_drift) == 0
    assert _ran(markers) == {"web"}


def test_gate_nothing_changed(repo, markers, capsys):
    assert domains.gate(repo, checks=_touching(markers), drift=_no_drift) == 0
    assert "gate: no changed paths — nothing to check" in capsys.readouterr().out
    assert _ran(markers) == set()


def test_gate_reads_files_from_env(repo, markers, tmp_path, monkeypatch, capsys):
    # The worktree holds a web change, but the commit's list names only a Go file.
    (repo / "web" / "x.tsx").write_text("export const x = 1\n", encoding="utf-8")
    listing = tmp_path / "run-1.files"
    listing.write_text("internal/x.go\n", encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_GATE_FILES", str(listing))
    assert domains.gate(repo, checks=_touching(markers), drift=_no_drift) == 0
    assert "gate: skipped — the commit touches no web path" in capsys.readouterr().out
    assert _ran(markers) == set()



def test_gate_unreadable_files_list_is_unchecked(repo, markers, tmp_path, monkeypatch, capsys):
    monkeypatch.setenv("LIGHTHOUSE_GATE_FILES", str(tmp_path / "nope.files"))
    assert domains.gate(repo, checks=_touching(markers), drift=_no_drift) == domains.UNCHECKED
    out = capsys.readouterr().out
    assert "gate: COULD NOT CHECK — cannot read the changed files" in out
    assert _ran(markers) == set()

def test_gate_runs_only_web(repo, markers, web_ready, tmp_path, monkeypatch, capsys):
    listing = tmp_path / "run-1.files"
    listing.write_text("web/src/a.ts\ninternal/x.go\n", encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_GATE_FILES", str(listing))
    assert domains.gate(repo, checks=_touching(markers), drift=_no_drift) == 0
    assert _ran(markers) == {"web"}
    assert capsys.readouterr().out.splitlines()[-1] == "gate: passed — web"


def test_gate_web_without_node_modules(repo, markers, monkeypatch, capsys):
    (repo / "web" / "x.tsx").write_text("export const x = 1\n", encoding="utf-8")
    # node and npm present, so only the missing web/node_modules can stop the gate.
    monkeypatch.setattr(shutil, "which", lambda name, *a, **k: f"/fake/bin/{name}")
    assert domains.gate(repo, checks=_touching(markers), drift=_no_drift) == 3
    out = capsys.readouterr().out
    assert "COULD NOT CHECK" in out
    assert "web/node_modules" in out
    assert "cd web && npm ci" in out
    assert _ran(markers) == set()


def test_gate_web_without_node(repo, markers, monkeypatch, capsys):
    (repo / "web" / "node_modules").mkdir()
    (repo / "web" / "x.tsx").write_text("export const x = 1\n", encoding="utf-8")
    monkeypatch.setattr(
        shutil, "which", lambda name, *a, **k: None if name == "node" else f"/fake/bin/{name}"
    )
    assert domains.gate(repo, checks=_touching(markers), drift=_no_drift) == 3
    out = capsys.readouterr().out
    assert "COULD NOT CHECK" in out
    assert "node" in out
    assert _ran(markers) == set()


def test_gate_stops_at_first_failure(repo, markers, web_ready, capsys):
    (repo / "web" / "x.tsx").write_text("export const x = 1\n", encoding="utf-8")
    checks = {"web": [["sh", "-c", "exit 7"], ["sh", "-c", f"touch '{markers / 'web'}'"]]}
    assert domains.gate(repo, checks=checks, drift=_no_drift) == 7
    assert _ran(markers) == set()
    assert "gate: passed" not in capsys.readouterr().out


def test_gate_missing_command(repo, markers, web_ready, capsys):
    (repo / "web" / "x.tsx").write_text("export const x = 1\n", encoding="utf-8")
    checks = {"web": [["lighthouse-no-such-cmd"]]}
    assert domains.gate(repo, checks=checks, drift=_no_drift) == 3
    out = capsys.readouterr().out
    assert "gate: COULD NOT CHECK — lighthouse-no-such-cmd not found" in out


def test_gate_drift_runs_first_and_stops_web(repo, markers, web_ready):
    (repo / "web" / "x.tsx").write_text("export const x = 1\n", encoding="utf-8")
    seen = []

    def drift(web: Path) -> int:
        seen.append(web)
        return 1

    assert domains.gate(repo, checks=_touching(markers), drift=drift) == 1
    assert seen == [repo / "web"]
    assert _ran(markers) == set()


def test_api_drift(tmp_path, capsys):
    web = tmp_path / "web"
    (web / "src" / "api").mkdir(parents=True)
    (web / "src" / "api" / "a.ts").write_text("export const a = 1\n", encoding="utf-8")

    same = ["sh", "-c", "printf 'export const a = 1\\n' > src/api/a.ts"]
    assert domains.api_drift(web, gen=same) == 0

    changed = ["sh", "-c", "printf 'export const a = 2\\n' > src/api/a.ts"]
    assert domains.api_drift(web, gen=changed) == 1
    assert (
        "gate: web/src/api was stale; npm run gen regenerated it — stage the result"
        in capsys.readouterr().out
    )

    added = ["sh", "-c", "printf 'x' > src/api/b.ts"]
    assert domains.api_drift(web, gen=added) == 1
    capsys.readouterr()

    failed = ["sh", "-c", "exit 5"]
    assert domains.api_drift(web, gen=failed) == 5
    out = capsys.readouterr().out
    assert "gate: npm run gen failed" in out
    assert "stale" not in out


def test_classify_cli_github_output(tmp_path, monkeypatch, capsys):
    out_file = tmp_path / "github_output"
    out_file.write_text("earlier=1\n", encoding="utf-8")
    monkeypatch.setattr(sys, "stdin", io.TextIOWrapper(io.BytesIO(b"web/x.tsx\0internal/y.go\0")))
    assert domains.main(["classify", "--github-output", str(out_file)]) == 0
    lines = out_file.read_text(encoding="utf-8").splitlines()
    assert lines[0] == "earlier=1"
    assert {"web=true", "go=true", "python=true"} <= set(lines)
    assert "web=true" in capsys.readouterr().out.splitlines()


def test_classify_cli_false(monkeypatch, capsys):
    monkeypatch.setattr(sys, "stdin", io.TextIOWrapper(io.BytesIO(b"web/x.tsx\0")))
    assert domains.main(["classify"]) == 0
    assert capsys.readouterr().out.splitlines() == ["web=true", "python=false", "go=false"]
