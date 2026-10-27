"""post_artefact: a committed spec or plan file as a version chain of issue comments.

From paia's `test_post_artefact.py`; the script runs from the installed layout
(`install_core`). New: the spec and plan directories and the default branch come from the config.
"""

import importlib.util
import subprocess
import sys
from pathlib import Path
from types import SimpleNamespace

import pytest

from lighthouse_lib.artefacts import hash_of, parse_meta
from lighthouse_lib.protocol import Comment
from tests.pack._testkit import INSTALL_ROOT

SCRIPT = INSTALL_ROOT / ".lighthouse" / "scripts" / "post_artefact.py"
_SPEC = importlib.util.spec_from_file_location("post_artefact", SCRIPT)
assert _SPEC and _SPEC.loader
pa = importlib.util.module_from_spec(_SPEC)
sys.modules[_SPEC.name] = pa
_SPEC.loader.exec_module(pa)

PLAN = "docs/developer/planning/plans/x.md"
URL = "https://github.com/Owner/repo/issues/1094#issuecomment-{}"


@pytest.fixture
def config(tmp_path, monkeypatch):
    def write(body: str) -> Path:
        path = tmp_path / "lighthouse.toml"
        path.write_text(body, encoding="utf-8")
        monkeypatch.setenv("LIGHTHOUSE_TOML", str(path))
        return path

    return write


@pytest.fixture(autouse=True)
def repo_config(config):
    config('[forge]\nkind = "github"\nrepo = "Owner/repo"\n')


def _known(w, sha):
    if w.gh_error:
        raise pa.RunError(w.gh_error)
    return sha in w.known


@pytest.fixture
def world(tmp_path, monkeypatch):
    top = (tmp_path / "wt").resolve()
    path = top / PLAN
    path.parent.mkdir(parents=True)
    path.write_text("# Plan\n")
    w = SimpleNamespace(
        top=top,
        path=path,
        porcelain="",
        sha="c" * 40,
        branch="issue-1094-x",
        known={"c" * 40},
        pr=False,
        comments=[],
        fail_at_part=None,
        gh_error=None,
        git_calls=[],
        posts=[],
    )

    def git(cwd, *args):
        w.git_calls.append((cwd, args))
        if args == ("rev-parse", "--show-toplevel"):
            return str(w.top)
        if args[0] == "status":
            return w.porcelain
        if args[0] == "log":
            return w.sha
        if args == ("rev-parse", "--abbrev-ref", "HEAD"):
            return w.branch
        if args[0] == "push":
            w.known.add(w.sha)
            return ""
        raise AssertionError(args)

    def post_comment(nr, body):
        part = parse_meta(body).part
        if part == w.fail_at_part:
            raise pa.RunError("boem")
        k = len(w.posts) + 1
        w.posts.append((nr, body))
        url = URL.format(1000 + k)
        w.comments.append(Comment(id=1000 + k, body=body, created_at="", url=url))
        return 1000 + k, url

    monkeypatch.setattr(pa, "git", git)

    monkeypatch.setattr(pa, "commit_known", lambda sha: _known(w, sha))
    monkeypatch.setattr(pa, "open_pr", lambda branch: w.pr)
    monkeypatch.setattr(pa, "read_comments", lambda nr: list(w.comments))
    monkeypatch.setattr(pa, "post_comment", post_comment)
    return w


def test_first_post_is_version_1(world, capsys):
    assert pa.main(["1094", str(world.path)]) == 0
    [(nr, body)] = world.posts
    meta = parse_meta(body)
    assert (
        nr,
        meta.kind,
        meta.version,
        meta.part,
        meta.parts,
        meta.file,
        meta.replaces,
    ) == (1094, "plan", 1, 1, 1, PLAN, None)
    assert meta.hash == hash_of(b"# Plan\n") and meta.commit == "c" * 40
    assert capsys.readouterr().out.strip() == (
        "artefact: plan version 1 posted, 1 parts → "
        "https://github.com/Owner/repo/issues/1094#issuecomment-1001"
    )


def test_absolute_worktree_path_runs_git_at_the_toplevel(world):
    pa.main(["1094", str(world.path)])
    assert world.git_calls[0] == (world.path.parent, ("rev-parse", "--show-toplevel"))
    assert all(cwd == world.top for cwd, _ in world.git_calls[1:])


def test_relative_path_against_the_cwd(world, monkeypatch):
    monkeypatch.chdir(world.top / "docs")
    assert pa.main(["1094", "developer/planning/plans/x.md"]) == 0
    assert parse_meta(world.posts[0][1]).file == PLAN


def test_unchanged_posts_nothing(world, capsys):
    pa.main(["1094", str(world.path)])
    capsys.readouterr()
    assert pa.main(["1094", str(world.path)]) == 0
    assert len(world.posts) == 1
    assert capsys.readouterr().out.strip() == "artefact: plan unchanged (version 1)"


def test_changed_posts_version_2_with_replaces(world):
    pa.main(["1094", str(world.path)])
    world.path.write_text("# Plan v2\n")
    pa.main(["1094", str(world.path)])
    meta = parse_meta(world.posts[-1][1])
    assert (meta.version, meta.replaces) == (
        2,
        "https://github.com/Owner/repo/issues/1094#issuecomment-1001",
    )


def test_same_hash_but_commit_unreachable_posts_new_version(world):
    pa.main(["1094", str(world.path)])
    world.known.discard("c" * 40)
    world.sha = "d" * 40
    world.known.add("d" * 40)
    pa.main(["1094", str(world.path)])
    assert parse_meta(world.posts[-1][1]).version == 2


def test_uncommitted_fails(world, capsys):
    world.porcelain = " M " + PLAN
    assert pa.main(["1094", str(world.path)]) == 1
    assert capsys.readouterr().err.strip() == f"artefact: {PLAN} not committed"
    assert world.posts == []


def test_wrong_directory_fails(world, capsys):
    other = world.top / "docs/developer/x.md"
    other.write_text("x")
    assert pa.main(["1094", str(other)]) == 1
    assert (
        capsys.readouterr().err.strip()
        == "artefact: docs/developer/x.md is no spec or plan"
    )


def test_unknown_commit_without_pr_pushes(world):
    world.known.clear()
    assert pa.main(["1094", str(world.path)]) == 0
    assert ("push", "-u", "--force-with-lease", "origin", "HEAD") in [
        a for _, a in world.git_calls
    ]


def test_unknown_commit_with_pr_fails_without_push(world, capsys):
    world.known.clear()
    world.pr = True
    assert pa.main(["1094", str(world.path)]) == 1
    assert capsys.readouterr().err.strip() == "artefact: commit ccccccc not on GitHub"
    assert not any(a[0] == "push" for _, a in world.git_calls)


def test_unknown_commit_on_main_fails_without_push(world):
    world.known.clear()
    world.branch = "main"
    assert pa.main(["1094", str(world.path)]) == 1
    assert not any(a[0] == "push" for _, a in world.git_calls)


def test_parts_in_order_and_half_post(world, monkeypatch, capsys):
    monkeypatch.setattr(pa, "split", lambda text: ["a", "b", "c"])
    world.fail_at_part = 3
    assert pa.main(["1094", str(world.path)]) == 1
    assert [parse_meta(b).part for _, b in world.posts] == [1, 2]
    assert capsys.readouterr().err.startswith(
        "artefact: part 3/3 not posted (2 were): "
    )


def test_double_dash_in_the_name_fails_cleanly(world, capsys):
    path = world.path.with_name("a--b.md")
    path.write_text("x")
    assert pa.main(["1094", str(path)]) == 1
    assert capsys.readouterr().err.startswith("artefact: metadata unusable: ")


def test_read_comments_paginates(monkeypatch):
    seen = []
    monkeypatch.setattr(pa, "gh", lambda *a, **k: seen.append(a) or "")
    pa.read_comments(1094)
    assert "--paginate" in seen[0]


def _gh_fails_with(monkeypatch, text):
    def gh(*args, **kwargs):
        raise pa.RunError(text)

    monkeypatch.setattr(pa, "gh", gh)


@pytest.mark.parametrize(
    "text", ["gh: Not Found (HTTP 404)", "No commit found for SHA: x (HTTP 422)"]
)
def test_commit_known_unknown_commit_is_false(monkeypatch, text):
    _gh_fails_with(monkeypatch, text)
    assert pa.commit_known("c" * 40) is False


def test_commit_known_other_gh_error_propagates(monkeypatch):
    _gh_fails_with(monkeypatch, "connection refused")
    with pytest.raises(pa.RunError):
        pa.commit_known("c" * 40)


def test_gh_error_at_commit_check_posts_and_pushes_nothing(world, capsys):
    world.gh_error = "connection refused"
    assert pa.main(["1094", str(world.path)]) == 1
    assert capsys.readouterr().err.startswith("artefact: gh failed: connection refused")
    assert world.posts == []
    assert not any(a[0] == "push" for _, a in world.git_calls)


def test_gh_error_at_commit_check_of_unchanged_posts_nothing(world, capsys):
    pa.main(["1094", str(world.path)])
    capsys.readouterr()
    world.gh_error = "timeout"
    assert pa.main(["1094", str(world.path)]) == 1
    assert capsys.readouterr().err.startswith("artefact: gh failed: ")
    assert len(world.posts) == 1


def test_run_error_keeps_the_tail_of_stderr(monkeypatch):
    stderr = "ok: hook\n" * 100 + "! [rejected] HEAD -> x (stale info)\n"

    def run(*args, **kwargs):
        raise pa.subprocess.CalledProcessError(1, "git", stderr=stderr)

    monkeypatch.setattr(pa.subprocess, "run", run)
    with pytest.raises(pa.RunError) as err:
        pa.git(Path("."), "push")
    assert len(str(err.value)) == 300
    assert str(err.value).endswith("! [rejected] HEAD -> x (stale info)")


def test_other_repo_posts_to_own_repo(config, monkeypatch):
    config('[forge]\nrepo = "Other/repo"\n')
    calls: list[list[str]] = []

    def run(command, stdin_text=None):
        calls.append(command)
        return '{"id": 5, "html_url": "u"}'

    monkeypatch.setattr(pa, "_run", run)
    pa.commit_known("c" * 40)
    pa.open_pr("branch")
    pa.read_comments(12)
    pa.post_comment(12, "tekst")
    argv = [a for command in calls for a in command]
    assert "repos/Other/repo/commits/" + "c" * 40 in argv
    assert "Other/repo" in argv
    assert "repos/Other/repo/issues/12/comments?per_page=100" in argv
    assert "repos/Other/repo/issues/12/comments" in argv
    assert not any("Owner" in a for a in argv)


def test_unchanged_against_an_old_version(world, capsys):
    """A version from before the move counts: same hash, nothing to post."""
    pa.main(["1094", str(world.path)])
    old = world.comments[0]
    world.comments[0] = Comment(
        id=old.id,
        body=old.body.replace("<!-- lighthouse:", "<!-- paia:", 1),
        created_at="",
        url=old.url,
    )
    capsys.readouterr()
    assert pa.main(["1094", str(world.path)]) == 0
    assert capsys.readouterr().out.strip() == "artefact: plan unchanged (version 1)"


def test_post_artefact_paths_from_config(world, config):
    config('[forge]\nrepo = "Owner/repo"\n[paths]\nplans = "p/"\nspecs = "s"\n')
    plan = world.top / "p" / "a.md"
    plan.parent.mkdir()
    plan.write_text("# A\n")
    assert pa.main(["1094", str(plan)]) == 0
    meta = parse_meta(world.posts[-1][1])
    assert (meta.kind, meta.file) == ("plan", "p/a.md")
    spec = world.top / "s" / "b.md"
    spec.parent.mkdir()
    spec.write_text("# B\n")
    assert pa.main(["1094", str(spec)]) == 0
    assert parse_meta(world.posts[-1][1]).kind == "spec"
    # The default directory is no longer a plan directory.
    assert pa.main(["1094", str(world.path)]) == 1


def test_directory_prefix_is_a_whole_directory(world, config):
    """`plans = "p"` does not make `pp/a.md` a plan."""
    config('[forge]\nrepo = "Owner/repo"\n[paths]\nplans = "p"\n')
    other = world.top / "pp" / "a.md"
    other.parent.mkdir()
    other.write_text("x")
    assert pa.main(["1094", str(other)]) == 1


def test_unknown_commit_on_default_branch_fails_without_push(world, config):
    config('[forge]\nrepo = "Owner/repo"\ndefault_branch = "trunk"\n')
    world.known.clear()
    world.branch = "trunk"
    assert pa.main(["1094", str(world.path)]) == 1
    assert not any(a[0] == "push" for _, a in world.git_calls)


def test_without_repo_an_artefact_error(world, config, capsys):
    config('[project]\nlanguage = "en"\n')
    assert pa.main(["1094", str(world.path)]) == 1
    err = capsys.readouterr().err.strip()
    assert err.startswith("artefact: ") and "forge.repo" in err
    assert world.posts == []


def test_broken_config_an_artefact_error(world, config, capsys):
    broken = config("geen toml [")
    assert pa.main(["1094", str(world.path)]) == 1
    err = capsys.readouterr().err.strip()
    assert err.startswith("artefact: config: ") and str(broken) in err
    assert world.posts == []


def test_runs_as_installed_file(tmp_path):
    """The installed script finds `lighthouse_lib` next to itself, from any cwd."""
    result = subprocess.run(
        [sys.executable, str(SCRIPT), "1", str(tmp_path / "weg.md")],
        capture_output=True,
        text=True,
        cwd="/",
        check=False,
    )
    assert result.returncode == 1, result.stderr
    assert result.stderr.strip() == f"artefact: {tmp_path / 'weg.md'} does not exist"


def test_help_is_english(capsys):
    with pytest.raises(SystemExit) as exc:
        pa.main(["-h"])
    assert exc.value.code == 0
    usage = capsys.readouterr().out.splitlines()[0]
    assert usage.endswith("nr file"), usage
