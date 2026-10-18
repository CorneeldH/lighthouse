"""post_artefact: een gecommit spec- of plan-bestand als versie-keten van issue-comments.

Uit paia's `test_post_artefact.py`; het script draait uit de geïnstalleerde indeling
(`install_core`). Nieuw: de spec- en planmappen en de default branch komen uit de config.
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
    def schrijf(body: str) -> Path:
        pad = tmp_path / "lighthouse.toml"
        pad.write_text(body, encoding="utf-8")
        monkeypatch.setenv("LIGHTHOUSE_TOML", str(pad))
        return pad

    return schrijf


@pytest.fixture(autouse=True)
def repo_config(config):
    config('[forge]\nkind = "github"\nrepo = "Owner/repo"\n')


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
        deel = parse_meta(body).part
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


def test_absoluut_worktreepad_draait_git_op_de_toplevel(wereld):
    pa.main(["1094", str(wereld.pad)])
    assert wereld.git_calls[0] == (wereld.pad.parent, ("rev-parse", "--show-toplevel"))
    assert all(cwd == wereld.top for cwd, _ in wereld.git_calls[1:])


def test_relatief_pad_tegen_de_cwd(wereld, monkeypatch):
    monkeypatch.chdir(wereld.top / "docs")
    assert pa.main(["1094", "developer/planning/plans/x.md"]) == 0
    assert parse_meta(wereld.posts[0][1]).file == PLAN


def test_ongewijzigd_post_niets(wereld, capsys):
    pa.main(["1094", str(wereld.pad)])
    capsys.readouterr()
    assert pa.main(["1094", str(wereld.pad)]) == 0
    assert len(wereld.posts) == 1
    assert capsys.readouterr().out.strip() == "artefact: plan unchanged (version 1)"


def test_gewijzigd_post_versie_2_met_vervangt(wereld):
    pa.main(["1094", str(wereld.pad)])
    wereld.pad.write_text("# Plan v2\n")
    pa.main(["1094", str(wereld.pad)])
    meta = parse_meta(wereld.posts[-1][1])
    assert (meta.version, meta.replaces) == (
        2,
        "https://github.com/Owner/repo/issues/1094#issuecomment-1001",
    )


def test_zelfde_hash_maar_commit_onbereikbaar_post_nieuwe_versie(wereld):
    pa.main(["1094", str(wereld.pad)])
    wereld.bekend.discard("c" * 40)
    wereld.sha = "d" * 40
    wereld.bekend.add("d" * 40)
    pa.main(["1094", str(wereld.pad)])
    assert parse_meta(wereld.posts[-1][1]).version == 2


def test_ongecommit_faalt(wereld, capsys):
    wereld.porcelain = " M " + PLAN
    assert pa.main(["1094", str(wereld.pad)]) == 1
    assert capsys.readouterr().err.strip() == f"artefact: {PLAN} not committed"
    assert wereld.posts == []


def test_verkeerde_map_faalt(wereld, capsys):
    ander = wereld.top / "docs/developer/x.md"
    ander.write_text("x")
    assert pa.main(["1094", str(ander)]) == 1
    assert (
        capsys.readouterr().err.strip()
        == "artefact: docs/developer/x.md is no spec or plan"
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
    assert capsys.readouterr().err.strip() == "artefact: commit ccccccc not on GitHub"
    assert not any(a[0] == "push" for _, a in wereld.git_calls)


def test_onbekende_commit_op_main_faalt_zonder_push(wereld):
    wereld.bekend.clear()
    wereld.branch = "main"
    assert pa.main(["1094", str(wereld.pad)]) == 1
    assert not any(a[0] == "push" for _, a in wereld.git_calls)


def test_delen_in_volgorde_en_halve_post(wereld, monkeypatch, capsys):
    monkeypatch.setattr(pa, "split", lambda tekst: ["a", "b", "c"])
    wereld.faal_bij_deel = 3
    assert pa.main(["1094", str(wereld.pad)]) == 1
    assert [parse_meta(b).part for _, b in wereld.posts] == [1, 2]
    assert capsys.readouterr().err.startswith(
        "artefact: part 3/3 not posted (2 were): "
    )


def test_dubbel_streepje_in_de_naam_faalt_netjes(wereld, capsys):
    pad = wereld.pad.with_name("a--b.md")
    pad.write_text("x")
    assert pa.main(["1094", str(pad)]) == 1
    assert capsys.readouterr().err.startswith("artefact: metadata unusable: ")


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
    assert capsys.readouterr().err.startswith("artefact: gh failed: connection refused")
    assert wereld.posts == []
    assert not any(a[0] == "push" for _, a in wereld.git_calls)


def test_gh_fout_bij_commitcheck_van_ongewijzigd_post_niets(wereld, capsys):
    pa.main(["1094", str(wereld.pad)])
    capsys.readouterr()
    wereld.gh_fout = "timeout"
    assert pa.main(["1094", str(wereld.pad)]) == 1
    assert capsys.readouterr().err.startswith("artefact: gh failed: ")
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


def test_andere_repo_post_naar_eigen_repo(config, monkeypatch):
    config('[forge]\nrepo = "Other/repo"\n')
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
    assert not any("Owner" in a for a in argv)


def test_ongewijzigd_tegen_een_oude_versie(wereld, capsys):
    """Een versie van vóór de verhuizing telt mee: zelfde hash, niets te posten."""
    pa.main(["1094", str(wereld.pad)])
    oud = wereld.comments[0]
    wereld.comments[0] = Comment(
        id=oud.id,
        body=oud.body.replace("<!-- lighthouse:", "<!-- paia:", 1),
        created_at="",
        url=oud.url,
    )
    capsys.readouterr()
    assert pa.main(["1094", str(wereld.pad)]) == 0
    assert capsys.readouterr().out.strip() == "artefact: plan unchanged (version 1)"


def test_post_artefact_paths_from_config(wereld, config):
    config('[forge]\nrepo = "Owner/repo"\n[paths]\nplans = "p/"\nspecs = "s"\n')
    plan = wereld.top / "p" / "a.md"
    plan.parent.mkdir()
    plan.write_text("# A\n")
    assert pa.main(["1094", str(plan)]) == 0
    meta = parse_meta(wereld.posts[-1][1])
    assert (meta.kind, meta.file) == ("plan", "p/a.md")
    spec = wereld.top / "s" / "b.md"
    spec.parent.mkdir()
    spec.write_text("# B\n")
    assert pa.main(["1094", str(spec)]) == 0
    assert parse_meta(wereld.posts[-1][1]).kind == "spec"
    # De default-map is geen planmap meer.
    assert pa.main(["1094", str(wereld.pad)]) == 1


def test_mapprefix_is_een_hele_map(wereld, config):
    """`plans = "p"` maakt `pp/a.md` geen plan."""
    config('[forge]\nrepo = "Owner/repo"\n[paths]\nplans = "p"\n')
    ander = wereld.top / "pp" / "a.md"
    ander.parent.mkdir()
    ander.write_text("x")
    assert pa.main(["1094", str(ander)]) == 1


def test_onbekende_commit_op_default_branch_faalt_zonder_push(wereld, config):
    config('[forge]\nrepo = "Owner/repo"\ndefault_branch = "trunk"\n')
    wereld.bekend.clear()
    wereld.branch = "trunk"
    assert pa.main(["1094", str(wereld.pad)]) == 1
    assert not any(a[0] == "push" for _, a in wereld.git_calls)


def test_zonder_repo_een_artefactfout(wereld, config, capsys):
    config('[project]\nlanguage = "en"\n')
    assert pa.main(["1094", str(wereld.pad)]) == 1
    err = capsys.readouterr().err.strip()
    assert err.startswith("artefact: ") and "forge.repo" in err
    assert wereld.posts == []


def test_kapotte_config_een_artefactfout(wereld, config, capsys):
    kapot = config("geen toml [")
    assert pa.main(["1094", str(wereld.pad)]) == 1
    err = capsys.readouterr().err.strip()
    assert err.startswith("artefact: config: ") and str(kapot) in err
    assert wereld.posts == []


def test_draait_als_geinstalleerd_bestand(tmp_path):
    """Het geïnstalleerde script vindt `lighthouse_lib` naast zich, vanuit elke cwd."""
    result = subprocess.run(
        [sys.executable, str(SCRIPT), "1", str(tmp_path / "weg.md")],
        capture_output=True,
        text=True,
        cwd="/",
        check=False,
    )
    assert result.returncode == 1, result.stderr
    assert result.stderr.strip() == f"artefact: {tmp_path / 'weg.md'} does not exist"
