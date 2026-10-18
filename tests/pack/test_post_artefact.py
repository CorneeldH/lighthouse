"""post_artefact: een gecommit spec- of plan-bestand als versie-keten van issue-comments."""

import importlib.util
import sys
from pathlib import Path
from types import SimpleNamespace

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
_SPEC = importlib.util.spec_from_file_location(
    "post_artefact", REPO_ROOT / "scripts" / "post_artefact.py"
)
assert _SPEC and _SPEC.loader
pa = importlib.util.module_from_spec(_SPEC)
sys.modules[_SPEC.name] = pa
_SPEC.loader.exec_module(pa)

from src.controller.artefacten import hash_van, parse_meta
from src.controller.comments import Comment

PLAN = "docs/developer/planning/plans/x.md"
URL = "https://github.com/CorneeldH/personal_ai_agent/issues/1094#issuecomment-{}"


def _bekend(w, sha):
    if w.gh_fout:
        raise pa.RunFout(w.gh_fout)
    return sha in w.bekend


@pytest.fixture
def wereld(tmp_path, monkeypatch):
    top = (tmp_path / "wt").resolve()
    pad = top / PLAN
    pad.parent.mkdir(parents=True)
    pad.write_text("# Plan\n")
    w = SimpleNamespace(
        top=top,
        pad=pad,
        porcelain="",
        sha="c" * 40,
        branch="issue-1094-x",
        bekend={"c" * 40},
        pr=False,
        comments=[],
        faal_bij_deel=None,
        gh_fout=None,
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
            w.bekend.add(w.sha)
            return ""
        raise AssertionError(args)

    def post_comment(nr, body):
        deel = parse_meta(body).deel
        if deel == w.faal_bij_deel:
            raise pa.RunFout("boem")
        k = len(w.posts) + 1
        w.posts.append((nr, body))
        url = URL.format(1000 + k)
        w.comments.append(Comment(id=1000 + k, body=body, created_at="", url=url))
        return 1000 + k, url

    monkeypatch.setattr(pa, "git", git)

    monkeypatch.setattr(pa, "commit_bekend", lambda sha: _bekend(w, sha))
    monkeypatch.setattr(pa, "open_pr", lambda branch: w.pr)
    monkeypatch.setattr(pa, "lees_comments", lambda nr: list(w.comments))
    monkeypatch.setattr(pa, "post_comment", post_comment)
    return w


def test_eerste_post_is_versie_1(wereld, capsys):
    assert pa.main(["1094", str(wereld.pad)]) == 0
    [(nr, body)] = wereld.posts
    meta = parse_meta(body)
    assert (
        nr,
        meta.soort,
        meta.versie,
        meta.deel,
        meta.delen,
        meta.bestand,
        meta.vervangt,
    ) == (1094, "plan", 1, 1, 1, PLAN, None)
    assert meta.hash == hash_van(b"# Plan\n") and meta.commit == "c" * 40
    assert capsys.readouterr().out.strip() == (
        "artefact: plan versie 1 gepost, 1 delen → "
        "https://github.com/CorneeldH/personal_ai_agent/issues/1094#issuecomment-1001"
    )


def test_absoluut_worktreepad_draait_git_op_de_toplevel(wereld):
    pa.main(["1094", str(wereld.pad)])
    assert wereld.git_calls[0] == (wereld.pad.parent, ("rev-parse", "--show-toplevel"))
    assert all(cwd == wereld.top for cwd, _ in wereld.git_calls[1:])


def test_relatief_pad_tegen_de_cwd(wereld, monkeypatch):
    monkeypatch.chdir(wereld.top / "docs")
    assert pa.main(["1094", "developer/planning/plans/x.md"]) == 0
    assert parse_meta(wereld.posts[0][1]).bestand == PLAN


def test_ongewijzigd_post_niets(wereld, capsys):
    pa.main(["1094", str(wereld.pad)])
    capsys.readouterr()
    assert pa.main(["1094", str(wereld.pad)]) == 0
    assert len(wereld.posts) == 1
    assert capsys.readouterr().out.strip() == "artefact: plan ongewijzigd (versie 1)"


def test_gewijzigd_post_versie_2_met_vervangt(wereld):
    pa.main(["1094", str(wereld.pad)])
    wereld.pad.write_text("# Plan v2\n")
    pa.main(["1094", str(wereld.pad)])
    meta = parse_meta(wereld.posts[-1][1])
    assert (meta.versie, meta.vervangt) == (
        2,
        "https://github.com/CorneeldH/personal_ai_agent/issues/1094#issuecomment-1001",
    )


def test_zelfde_hash_maar_commit_onbereikbaar_post_nieuwe_versie(wereld):
    pa.main(["1094", str(wereld.pad)])
    wereld.bekend.discard("c" * 40)
    wereld.sha = "d" * 40
    wereld.bekend.add("d" * 40)
    pa.main(["1094", str(wereld.pad)])
    assert parse_meta(wereld.posts[-1][1]).versie == 2


def test_ongecommit_faalt(wereld, capsys):
    wereld.porcelain = " M " + PLAN
    assert pa.main(["1094", str(wereld.pad)]) == 1
    assert capsys.readouterr().err.strip() == f"artefact: {PLAN} niet gecommit"
    assert wereld.posts == []


def test_verkeerde_map_faalt(wereld, capsys):
    ander = wereld.top / "docs/developer/x.md"
    ander.write_text("x")
    assert pa.main(["1094", str(ander)]) == 1
    assert (
        capsys.readouterr().err.strip()
        == "artefact: docs/developer/x.md is geen spec of plan"
    )


def test_onbekende_commit_zonder_pr_pusht(wereld):
    wereld.bekend.clear()
    assert pa.main(["1094", str(wereld.pad)]) == 0
    assert ("push", "-u", "--force-with-lease", "origin", "HEAD") in [
        a for _, a in wereld.git_calls
    ]


def test_onbekende_commit_met_pr_faalt_zonder_push(wereld, capsys):
    wereld.bekend.clear()
    wereld.pr = True
    assert pa.main(["1094", str(wereld.pad)]) == 1
    assert capsys.readouterr().err.strip() == "artefact: commit ccccccc niet op GitHub"
    assert not any(a[0] == "push" for _, a in wereld.git_calls)


def test_onbekende_commit_op_main_faalt_zonder_push(wereld):
    wereld.bekend.clear()
    wereld.branch = "main"
    assert pa.main(["1094", str(wereld.pad)]) == 1
    assert not any(a[0] == "push" for _, a in wereld.git_calls)


def test_delen_in_volgorde_en_halve_post(wereld, monkeypatch, capsys):
    monkeypatch.setattr(pa, "splits", lambda tekst: ["a", "b", "c"])
    wereld.faal_bij_deel = 3
    assert pa.main(["1094", str(wereld.pad)]) == 1
    assert [parse_meta(b).deel for _, b in wereld.posts] == [1, 2]
    assert capsys.readouterr().err.startswith(
        "artefact: deel 3/3 niet gepost (2 wel): "
    )


def test_dubbel_streepje_in_de_naam_faalt_netjes(wereld, capsys):
    pad = wereld.pad.with_name("a--b.md")
    pad.write_text("x")
    assert pa.main(["1094", str(pad)]) == 1
    assert capsys.readouterr().err.startswith("artefact: metadata onbruikbaar: ")


def test_lees_comments_pagineert(monkeypatch):
    gezien = []
    monkeypatch.setattr(pa, "gh", lambda *a, **k: gezien.append(a) or "")
    pa.lees_comments(1094)
    assert "--paginate" in gezien[0]


def _gh_faalt_met(monkeypatch, tekst):
    def gh(*args, **kwargs):
        raise pa.RunFout(tekst)

    monkeypatch.setattr(pa, "gh", gh)


@pytest.mark.parametrize(
    "tekst", ["gh: Not Found (HTTP 404)", "No commit found for SHA: x (HTTP 422)"]
)
def test_commit_bekend_onbekende_commit_is_false(monkeypatch, tekst):
    _gh_faalt_met(monkeypatch, tekst)
    assert pa.commit_bekend("c" * 40) is False


def test_commit_bekend_andere_gh_fout_stijgt(monkeypatch):
    _gh_faalt_met(monkeypatch, "connection refused")
    with pytest.raises(pa.RunFout):
        pa.commit_bekend("c" * 40)


def test_gh_fout_bij_commitcheck_post_en_pusht_niets(wereld, capsys):
    wereld.gh_fout = "connection refused"
    assert pa.main(["1094", str(wereld.pad)]) == 1
    assert capsys.readouterr().err.startswith("artefact: gh faalde: connection refused")
    assert wereld.posts == []
    assert not any(a[0] == "push" for _, a in wereld.git_calls)


def test_gh_fout_bij_commitcheck_van_ongewijzigd_post_niets(wereld, capsys):
    pa.main(["1094", str(wereld.pad)])
    capsys.readouterr()
    wereld.gh_fout = "timeout"
    assert pa.main(["1094", str(wereld.pad)]) == 1
    assert capsys.readouterr().err.startswith("artefact: gh faalde: ")
    assert len(wereld.posts) == 1


def test_runfout_bewaart_de_staart_van_stderr(monkeypatch):
    stderr = "ok: hook\n" * 100 + "! [rejected] HEAD -> x (stale info)\n"

    def run(*args, **kwargs):
        raise pa.subprocess.CalledProcessError(1, "git", stderr=stderr)

    monkeypatch.setattr(pa.subprocess, "run", run)
    with pytest.raises(pa.RunFout) as fout:
        pa.git(Path("."), "push")
    assert len(str(fout.value)) == 300
    assert str(fout.value).endswith("! [rejected] HEAD -> x (stale info)")


def test_andere_repo_post_naar_eigen_repo(other_identity, monkeypatch):
    calls: list[list[str]] = []

    def draai(commando, invoer=None):
        calls.append(commando)
        return '{"id": 5, "html_url": "u"}'

    monkeypatch.setattr(pa, "_draai", draai)
    pa.commit_bekend("c" * 40)
    pa.open_pr("branch")
    pa.lees_comments(12)
    pa.post_comment(12, "tekst")
    argv = [a for commando in calls for a in commando]
    assert "repos/Other/repo/commits/" + "c" * 40 in argv
    assert "Other/repo" in argv
    assert "repos/Other/repo/issues/12/comments?per_page=100" in argv
    assert "repos/Other/repo/issues/12/comments" in argv
    assert not any("personal_ai_agent" in a for a in argv)
