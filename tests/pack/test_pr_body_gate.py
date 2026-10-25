"""`pack/core/lib/pr_body_gate.py` (#1091): vormen, Evidence-tabel, AC-telling.

De lib is de pure logica onder `pr-create-gate.sh`: welke vorm (`Closes` met
Evidence-blok, `Refs` met reden, `No issue`) de PR-body heeft, of de tabel
klopt en of het aantal rijen het aantal AC van het issue dekt. Netwerkverbod:
`fetch_ac_count` krijgt hier een stub-`gh` vooraan op `PATH`.
"""

import io
import os
import subprocess
import sys
import time

import pytest

from tests.pack._testkit import (
    INSTALLED_LIB,
    ORIGIN,
    forge_worktree,
    git,
    init_repo,
    load_hook_lib,
    tool_bin,
)

gate = load_hook_lib("pr_body_gate")

ROWS1 = [("1. een", "`grep -F a f`", "a")]
ROWS2 = [*ROWS1, ("2. twee", "`ls`", "f")]
ROWS3 = [*ROWS2, ("3. drie", "`true`", "ok")]

ISSUE_MET_DRIE_AC = (
    "## Context\n\ntekst\n\n"
    "### Acceptatiecriteria\n- [ ] a\n- [x] b\n- [X] c\n\n"
    "### Definition of Done\n- [ ] d\n"
)


def closes_body(nr: int, rows: list[tuple[str, str, str]]) -> str:
    """Een body met `Closes #nr` en een Evidence-tabel met de gegeven rijen."""
    tabel = "\n".join(f"| {ac} | {cmd} | {res} |" for ac, cmd, res in rows)
    return (
        f"## Related issues\n\nCloses #{nr}\n\n"
        f"### Evidence #{nr}\n\n"
        f"| AC | Command | Result |\n|---|---|---|\n{tabel}\n"
    )


# --- Closes ---------------------------------------------------------------


@pytest.mark.parametrize(
    "woord",
    [
        "close",
        "Closes",
        "closed",
        "fix",
        "FIXES",
        "fixed",
        "resolve",
        "resolves",
        "Resolved",
        "Closes:",
    ],
)
def test_alle_sluitwoorden_tellen(woord):
    assert gate.closes_targets(f"{woord} #12") == [12]


@pytest.mark.parametrize(
    "doel",
    [
        "#12",
        "Acme/widgets#12",
        "https://github.com/Acme/widgets/issues/12",
    ],
)
def test_alle_doelvormen_tellen(doel):
    assert gate.closes_targets(f"Closes {doel}") == [12]


def test_andere_repo_telt_niet():
    assert gate.closes_targets("Closes other/repo#12") == []


@pytest.fixture
def other_identity(monkeypatch, tmp_path):
    toml = tmp_path / "other.toml"
    toml.write_text('[forge]\nkind = "github"\nrepo = "Other/repo"\n')
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(toml))


def test_repo_comes_from_lighthouse_toml(other_identity):
    """De repo komt uit `lighthouse.toml`, niet uit een literal (#1445), bij elke aanroep."""
    assert gate.repo() == "Other/repo"
    assert gate.closes_targets("Closes Other/repo#12") == [12]
    assert gate.closes_targets("Closes Acme/widgets#12") == []


def test_without_forge_repo(tmp_path, monkeypatch):
    """Zonder `forge.repo` telt alleen `#N`; de AC-telling meldt dat en de rest blijft toetsen."""
    toml = tmp_path / "lighthouse.toml"
    toml.write_text('[project]\nlanguage = "en"\n')
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(toml))
    assert gate.repo() is None
    assert gate.closes_targets("Closes #4 and Closes x/y#5") == [4]
    assert gate.fetch_ac_count(4) == (None, "no forge.repo in lighthouse.toml")
    assert gate.in_scope(_pc(str(tmp_path), "x/y"), str(tmp_path))[0] == "toetsen"


def test_missing_toml_fails_loud(tmp_path):
    """Zonder toml faalt de lib luid bij een `gh pr create`; `pr-create-gate.sh` meldt dat als blokkade."""
    lib = INSTALLED_LIB / "pr_body_gate.py"
    weg = tmp_path / "weg.toml"
    proc = subprocess.run(
        [sys.executable, str(lib), str(tmp_path)],
        env={**os.environ, "LIGHTHOUSE_TOML": str(weg)},
        input="gh pr create --repo Acme/widgets --body 'Closes #1'",
        capture_output=True,
        text=True,
        check=False,
    )
    assert proc.returncode not in (0, 2, 3)
    assert "weg.toml" in proc.stderr
    assert "Traceback" not in proc.stderr


def test_template_commentaar_telt_niet():
    assert gate.closes_targets("<!-- Closes #123\n -->") == []


def test_closes_uniek_en_in_volgorde():
    body = "Closes #9\nfixes #3\nCloses #9\nResolves Acme/widgets#4"
    assert gate.closes_targets(body) == [9, 3, 4]


def test_closes_nummer_wordt_niet_afgekapt():
    assert gate.closes_targets("Closes #123abc") == []


def test_strip_comments_crlf_en_commentaar():
    assert gate.strip_comments("a\r\n<!-- x\r\ny -->b") == "a\nb"


# --- Refs en No issue -----------------------------------------------------


def test_refs_met_reden():
    assert gate.refs_issues("Refs #7 — wacht op deploy Beelink") == [7]


@pytest.mark.parametrize("sep", ["—", "-", ":"])
def test_refs_scheidingstekens(sep):
    assert gate.refs_issues(f"Refs #7 {sep} meting over zeven dagen") == [7]


def test_refs_reden_te_kort():
    assert gate.refs_issues("Refs #7 — kort") == []


def test_refs_zonder_reden():
    assert gate.refs_issues("Refs #7") == []


def test_refs_in_commentaar_telt_niet():
    assert gate.refs_issues("<!-- Refs #7 — voorbeeld uit het template -->") == []


def test_refs_targets_zonder_redenvereiste():
    assert gate.refs_targets("Refs #7") == [7]
    assert gate.refs_targets("Refs #7 — kort\nRefs #8 — iets\nRefs #7") == [7, 8, 7]


def test_refs_targets_negeert_commentaar_en_andere_nummers():
    assert gate.refs_targets("<!-- Refs #7 — x -->\nzie #7") == []
    assert gate.refs_targets("Refs #77") == [77]


def test_no_issue():
    assert gate.no_issue("No issue: alleen een typo in docs")


def test_no_issue_te_kort():
    assert not gate.no_issue("No issue: typo")


# --- Evidence-tabel -------------------------------------------------------


def test_evidence_geldig():
    rows = gate.evidence_rows(closes_body(5, [("1. x", "`grep -F a f`", "a")]), 5)
    assert rows == [gate.Row("1. x", "`grep -F a f`", "a")]


def test_evidence_lege_cel():
    assert isinstance(gate.evidence_rows(closes_body(5, [("1", "`ls`", "")]), 5), str)


def test_evidence_command_zonder_backticks():
    assert isinstance(gate.evidence_rows(closes_body(5, [("1", "ls", "x")]), 5), str)


def test_evidence_verkeerde_kop():
    body = (
        "Closes #5\n\n### Evidence #5\n\n"
        "| AC | Cmd | Result |\n|---|---|---|\n| 1 | `ls` | x |\n"
    )
    assert isinstance(gate.evidence_rows(body, 5), str)


def test_evidence_geen_datarij():
    body = "Closes #5\n\n### Evidence #5\n\n| AC | Command | Result |\n|---|---|---|\n"
    assert isinstance(gate.evidence_rows(body, 5), str)


def test_evidence_kop_ontbreekt():
    assert isinstance(gate.evidence_rows("Closes #5", 5), str)


def test_evidence_kop_van_ander_issue():
    assert isinstance(gate.evidence_rows(closes_body(6, ROWS1), 5), str)


def test_evidence_blok_stopt_bij_eerste_niet_pipe_regel():
    body = closes_body(5, ROWS2) + "\ntekst\n| 3 | `ls` | x |\n"
    rows = gate.evidence_rows(body, 5)
    assert len(rows) == 2


def test_evidence_crlf():
    body = closes_body(5, ROWS1).replace("\n", "\r\n")
    assert len(gate.evidence_rows(body, 5)) == 1


def test_evidence_escaped_pipe_is_een_cel():  # Review Focus 3
    rows = gate.evidence_rows(
        closes_body(5, [("1", r"`grep -E 'a\|b' f`", "a")]),
        5,
    )
    assert len(rows) == 1
    assert rows[0].command == r"`grep -E 'a\|b' f`"


def test_evidence_te_veel_cellen():
    assert isinstance(
        gate.evidence_rows(closes_body(5, [("1", "`ls`", "a | b")]), 5), str
    )


# --- AC-telling -----------------------------------------------------------


def test_count_acs_telt_checkboxes():
    body = "### Acceptatiecriteria\n- [ ] a\n- [x] b\n### Definition of Done\n- [ ] c"
    assert gate.count_acs(body) == 2


@pytest.mark.parametrize("kop", ["## Acceptatiecriteria", "#### Acceptatiecriteria"])
def test_count_acs_kopniveaus(kop):
    assert gate.count_acs(f"{kop}\n- [ ] a") == 1


def test_count_acs_geen_kop():
    assert gate.count_acs("- [ ] a") is None


def test_count_acs_nul_checkboxes_is_none():  # Review Focus 2
    assert gate.count_acs("### Acceptatiecriteria\ntekst") is None


def test_count_acs_crlf():
    assert gate.count_acs(ISSUE_MET_DRIE_AC.replace("\n", "\r\n")) == 3


# --- check_body -----------------------------------------------------------


def test_check_body_closes_klopt():
    v = gate.check_body(closes_body(5, ROWS3), lambda n: (3, ""))
    assert v.blokkades == []
    assert v.meldingen == []


def test_check_body_ac_telling_wijkt_af():
    v = gate.check_body(closes_body(5, ROWS2), lambda n: (3, ""))
    assert v.blokkades
    assert "3" in v.blokkades[0]
    assert "2" in v.blokkades[0]


def test_check_body_terugval_meldt_en_laat_door():
    v = gate.check_body(closes_body(5, ROWS2), lambda n: (None, "timeout na 5 s"))
    assert v.blokkades == []
    assert v.meldingen == [
        (
            "pr-create-gate: AC count not measurable for #5 (timeout na 5 s)"
            " — structure checked only"
        )
    ]


def test_check_body_geen_vorm_blokkeert():
    assert gate.check_body("## Related issues\n\nzie titel").blokkades


def test_check_body_closes_zonder_blok_blokkeert():
    assert gate.check_body("Closes #5", lambda n: (1, "")).blokkades


def test_check_body_twee_closes_een_blok():  # Review Focus 5
    v = gate.check_body(closes_body(5, ROWS1) + "\nCloses #6", lambda n: (1, ""))
    assert any("#6" in b for b in v.blokkades)


def test_check_body_closes_plus_refs():
    body = closes_body(5, ROWS1) + "\nRefs #6 — volgt in vervolgissue"
    assert gate.check_body(body, lambda n: (1, "")).blokkades == []


def test_check_body_refs_met_reden_is_geldig():
    assert gate.check_body("Refs #6 — wacht op deploy Beelink").blokkades == []


def test_check_body_no_issue_is_geldig():
    assert gate.check_body("No issue: alleen een typo in docs").blokkades == []


def test_check_body_refs_zonder_reden_hint():
    v = gate.check_body("Refs #6")
    assert v.blokkades
    assert "reason" in v.blokkades[0]


def test_check_body_commentaar_vorm_telt_niet():
    body = "<!-- Closes #1\n Refs #2 — voorbeeld uit het template -->\nzie titel"
    assert gate.check_body(body).blokkades


def test_check_body_ongeldig_blok_noemt_reden():
    v = gate.check_body(closes_body(5, [("1", "ls", "x")]), lambda n: (1, ""))
    assert v.blokkades
    assert "#5" in v.blokkades[0]


# --- fetch_ac_count -------------------------------------------------------


def test_fetch_ac_count_zonder_gh(monkeypatch, tmp_path):  # Review Focus 4
    monkeypatch.setenv("PATH", str(tmp_path))
    assert gate.fetch_ac_count(5) == (None, "gh is missing")


def test_fetch_ac_count_gh_faalt(monkeypatch, tmp_path):
    bin_dir = tool_bin(tmp_path / "bin", (), stubs={"gh": "#!/bin/sh\nexit 1\n"})
    monkeypatch.setenv("PATH", str(bin_dir))
    assert gate.fetch_ac_count(5) == (None, "gh api failed (rc 1)")


def test_fetch_ac_count_telt(monkeypatch, tmp_path):
    aanroep = tmp_path / "aanroep.txt"
    stub = (
        "#!/bin/sh\n"
        f'echo "$@" > "{aanroep}"\n'
        "printf '%s\\n' '### Acceptatiecriteria' '- [ ] a' '- [x] b' '- [ ] c' "
        "'### Definition of Done' '- [ ] d'\n"
    )
    bin_dir = tool_bin(tmp_path / "bin", (), stubs={"gh": stub})
    monkeypatch.setenv("PATH", str(bin_dir))
    assert gate.fetch_ac_count(5) == (3, "")
    assert aanroep.read_text().split() == [
        "api",
        "repos/Acme/widgets/issues/5",
        "--jq",
        ".body",
    ]


def test_fetch_ac_count_geen_ac_kop(monkeypatch, tmp_path):
    stub = "#!/bin/sh\nprintf 'geen kop hier\\n'\n"
    bin_dir = tool_bin(tmp_path / "bin", (), stubs={"gh": stub})
    monkeypatch.setenv("PATH", str(bin_dir))
    assert gate.fetch_ac_count(5) == (None, "no acceptance criteria heading")


def test_fetch_ac_count_timeout(monkeypatch, tmp_path):
    # Een FIFO zonder schrijver: `read` blokkeert zonder CPU te branden.
    fifo = tmp_path / "blok"
    os.mkfifo(fifo)
    monkeypatch.setattr(gate, "AC_TIMEOUT_S", 0.3)
    stub = f'#!/bin/sh\nread _ < "{fifo}"\n'
    bin_dir = tool_bin(tmp_path / "bin", (), stubs={"gh": stub})
    monkeypatch.setenv("PATH", str(bin_dir))
    assert gate.fetch_ac_count(5) == (None, "timeout after 0.3 s")


# --- gh pr create vinden, body-bron, scope -----------------------------------


def test_vindt_create_en_new(tmp_path):
    cmd = "gh pr create -F b.md; gh pr new -F b.md"
    assert len(gate.find_pr_creates(cmd, str(tmp_path))) == 2


def test_negeert_andere_gh_commandos(tmp_path):
    assert gate.find_pr_creates("gh pr view 5; git status", str(tmp_path)) == []


def test_body_file_relatief_na_cd(tmp_path):
    (tmp_path / "sub").mkdir()
    [pc] = gate.find_pr_creates("cd sub && gh pr create -F body.md", str(tmp_path))
    assert pc.body_file == str(tmp_path / "sub" / "body.md")


def test_subshell_cd_lekt_niet(tmp_path):  # Review Focus 1
    (tmp_path / "sub").mkdir()
    cmd = "(cd sub && gh pr create -F b.md); gh pr create -F b.md"
    a, b = gate.find_pr_creates(cmd, str(tmp_path))
    assert a.body_file == str(tmp_path / "sub" / "b.md")
    assert b.body_file == str(tmp_path / "b.md")


@pytest.mark.parametrize(
    ("cmd", "woord"),
    [
        ("gh pr create --fill", "--fill"),
        ("gh pr create --fill-first", "--fill-first"),
        ("gh pr create -f", "-f"),
        ("gh pr create --web", "--web"),
        ("gh pr create -e", "-e"),
        ("gh pr create -F -", "stdin"),
        ("gh pr create --body-file=-", "stdin"),
        ('gh pr create --body "$(cat b.md)"', "non-literal"),
        ("gh pr create --body $BODY", "non-literal"),
        ("gh pr create -F $F", "non-literal"),
        ("gh pr create -t x", "no body"),
        ("gh pr create --body x -F b.md", "choose one"),
    ],
)
def test_body_bron_problemen(tmp_path, cmd, woord):
    assert woord in gate.find_pr_creates(cmd, str(tmp_path))[0].probleem


@pytest.mark.parametrize(
    "cmd",
    [
        "gh pr create --body 'No issue: alleen docs gewijzigd'",
        "gh pr create -b 'No issue: alleen docs gewijzigd'",
        "gh pr create --body='No issue: alleen docs gewijzigd'",
    ],
)
def test_body_letterlijk(tmp_path, cmd):
    [pc] = gate.find_pr_creates(cmd, str(tmp_path))
    assert pc.probleem is None
    assert pc.body.startswith("No issue")


def test_body_file_is(tmp_path):
    [pc] = gate.find_pr_creates("gh pr create --body-file=/x/b.md", str(tmp_path))
    assert pc.body_file == "/x/b.md"


def test_titel_met_b_in_waarde_telt_niet_als_body(tmp_path):
    [pc] = gate.find_pr_creates("gh pr create -t -b -F b.md", str(tmp_path))
    assert pc.body_file == str(tmp_path / "b.md")


def test_relatief_pad_zonder_bekende_cwd(tmp_path):
    [pc] = gate.find_pr_creates("gh pr create -F body.md", None)
    assert "cannot be resolved" in pc.probleem
    [pc] = gate.find_pr_creates("gh pr create -F /abs/body.md", None)
    assert pc.probleem is None


def test_repo_vlag(tmp_path):
    cmd = "gh pr create -R Acme/widgets -F b"
    assert gate.find_pr_creates(cmd, str(tmp_path))[0].repo == "Acme/widgets"


@pytest.mark.parametrize(
    ("cmd", "repo"),
    [
        ("GH_REPO=x/y gh pr create --fill", "x/y"),
        ("env GH_REPO=x/y gh pr create --fill", "x/y"),
        # gh: `-R` wint van `GH_REPO`.
        ("GH_REPO=x/y gh pr create -R Acme/widgets -F b", "Acme/widgets"),
    ],
)
def test_gh_repo_toewijzing_telt_als_repo(tmp_path, cmd, repo):
    [pc] = gate.find_pr_creates(cmd, str(tmp_path))
    assert pc.repo == repo
    assert pc.repo_probleem is None


@pytest.mark.parametrize(
    "cmd",
    [
        'gh pr create -R "$R" -F b.md',
        "gh pr create --repo=$R -F b.md",
        'GH_REPO="$R" gh pr create -F b.md',
    ],
)
def test_niet_letterlijke_repo_is_een_repo_probleem(tmp_path, cmd):
    [pc] = gate.find_pr_creates(cmd, str(tmp_path))
    assert pc.repo is None
    assert "non-literal" in pc.repo_probleem


def test_open_quote(tmp_path):
    with pytest.raises(gate.Unparseable):
        gate.find_pr_creates("gh pr create --body 'x", str(tmp_path))


def _pc(cwd, repo=None):
    return gate.PrCreate(cwd, repo, "x", None, None)


@pytest.fixture
def project(tmp_path):
    proj = tmp_path / "project"
    init_repo(proj)
    return proj


def test_scope_eigen_repo(tmp_path, project):
    wt = tmp_path / "wt"
    git(project, "worktree", "add", "-q", str(wt), "-b", "feat")
    assert gate.in_scope(_pc(str(project)), str(project))[0] == "toetsen"
    assert gate.in_scope(_pc(str(wt)), str(project))[0] == "toetsen"
    # Ook een nog niet bestaande submap telt als de repo van zijn voorouder.
    assert gate.in_scope(_pc(str(wt / "nieuw")), str(project))[0] == "toetsen"


def test_scope_andere_repo_vlag(project):
    besluit, reden = gate.in_scope(_pc(str(project), "x/y"), str(project))
    assert besluit == "overslaan"
    assert "other repo x/y" in reden


@pytest.mark.parametrize(
    "repo",
    [
        "Acme/widgets",
        "github.com/Acme/widgets",
        "https://github.com/Acme/widgets",
        "http://github.com/Acme/widgets/",
        "https://github.com/Acme/widgets.git",
        "ssh://git@github.com/Acme/widgets.git",
        "git@github.com:Acme/widgets.git",
        "https://github.example:8443/Acme/widgets",
        "acme/WIDGETS",
    ],
)
def test_scope_repo_spellingen_van_deze_repo(tmp_path, project, repo):
    buiten = tmp_path / "buiten"
    buiten.mkdir()
    assert gate.in_scope(_pc(str(buiten), repo), str(project))[0] == "toetsen"


@pytest.mark.parametrize(
    "repo", ["github.com/x/y", "https://github.com/x/y.git", "git@github.com:x/y.git"]
)
def test_scope_repo_spellingen_van_andere_repo(project, repo):
    besluit, reden = gate.in_scope(_pc(str(project), repo), str(project))
    assert besluit == "overslaan"
    assert f"other repo {repo}" in reden


@pytest.mark.parametrize(
    "repo", ["", "   ", "x", "https://github.com/", "https://github.com/x"]
)
def test_scope_onherkenbare_repo_blokkeert(project, repo):
    besluit, reden = gate.in_scope(_pc(str(project), repo), str(project))
    assert besluit == "blokkeren"
    assert "--repo" in reden


def test_scope_buiten_repo(tmp_path, project):
    buiten = tmp_path / "buiten"
    buiten.mkdir()
    assert gate.in_scope(_pc(str(buiten)), str(project))[0] == "overslaan"


@pytest.mark.parametrize(
    "repo",
    [
        "https://github.com/Acme/widgets?tab=readme",
        "https://github.com/Acme/widgets#readme",
        "https://github.com/Acme/widgets.git?x=1#y",
    ],
)
def test_scope_repo_query_en_anker_vallen_weg(tmp_path, project, repo):
    buiten = tmp_path / "buiten"
    buiten.mkdir()
    assert gate.in_scope(_pc(str(buiten), repo), str(project))[0] == "toetsen"


def test_scope_niet_letterlijke_repo_blokkeert_ook_buiten_de_repo(tmp_path, project):
    """`-R "$R"` vanuit een map buiten de repo: welke repo het wordt, zien we niet."""
    buiten = tmp_path / "buiten"
    buiten.mkdir()
    [pc] = gate.find_pr_creates('gh pr create -R "$R" -F b.md', str(buiten))
    besluit, reden = gate.in_scope(pc, str(project))
    assert besluit == "blokkeren"
    assert "non-literal" in reden


def test_scope_buiten_repo_met_R(tmp_path, project):
    buiten = tmp_path / "buiten"
    buiten.mkdir()
    pc = _pc(str(buiten), "Acme/widgets")
    assert gate.in_scope(pc, str(project))[0] == "toetsen"


def test_scope_andere_git_repo(tmp_path, project):
    ander = tmp_path / "ander"
    init_repo(ander)
    assert gate.in_scope(_pc(str(ander)), str(project))[0] == "overslaan"


def test_scope_forge_worktree_van_dezelfde_repo(tmp_path, project):
    """Een worktree van een aparte clone met dezelfde `origin` is deze repo (#36)."""
    git(project, "remote", "add", "origin", ORIGIN)
    work = forge_worktree(project, tmp_path)
    assert gate.in_scope(_pc(str(work)), str(project)) == ("toetsen", "")


def test_scope_andere_origin_noemt_common_dirs_en_remotes(tmp_path, project):
    git(project, "remote", "add", "origin", ORIGIN)
    work = forge_worktree(project, tmp_path, "https://github.com/Acme/gadgets.git")
    common = os.path.realpath(tmp_path / "clone.git")
    ref = os.path.realpath(project / ".git")
    assert gate.in_scope(_pc(str(work)), str(project)) == (
        "overslaan",
        f"skipped — outside this repo (git-common-dir {common} vs {ref};"
        f" origin https://github.com/Acme/gadgets.git vs {ORIGIN})",
    )


def test_scope_onbekende_cwd_blokkeert(project):
    assert gate.in_scope(_pc(None), str(project))[0] == "blokkeren"


def test_scope_andere_git_fout_blokkeert(tmp_path, project, monkeypatch):
    # Een git die faalt zonder "not a git repository" is geen bewijs van "buiten".
    bin_dir = tool_bin(
        tmp_path / "bin", (), stubs={"git": "#!/bin/sh\necho kapot >&2\nexit 128\n"}
    )
    monkeypatch.setenv("PATH", str(bin_dir))
    assert gate.in_scope(_pc(str(project)), str(project))[0] == "blokkeren"


def _run_main(monkeypatch, project, command, cwd):
    monkeypatch.setenv("CLAUDE_PROJECT_DIR", str(project))
    monkeypatch.setattr("sys.stdin", io.StringIO(command))
    return gate.main([str(cwd)])


def test_main_body_file_bestaat_niet(project, monkeypatch, capsys):
    rc = _run_main(monkeypatch, project, "gh pr create -F nope.md", project)
    err = capsys.readouterr().err
    assert rc == 2
    assert str(project / "nope.md") in err
    assert "pr-create-gate: BLOCKED" in err
    assert "/lighthouse-verify-pr" in err


def test_main_geen_gh_pr_create(project, monkeypatch, capsys):
    assert _run_main(monkeypatch, project, "git status", project) == 0
    assert capsys.readouterr().out == ""


def test_main_open_quote(project, monkeypatch):
    assert _run_main(monkeypatch, project, "gh pr create --body 'x", project) == 3


def test_main_probleem_blokkeert(project, monkeypatch, capsys):
    assert _run_main(monkeypatch, project, "gh pr create --fill", project) == 2
    assert "--fill gives no body to check" in capsys.readouterr().err


def test_main_no_issue_doorlaat(project, monkeypatch, capsys):
    cmd = "gh pr create --body 'No issue: alleen docs gewijzigd'"
    assert _run_main(monkeypatch, project, cmd, project) == 0
    assert capsys.readouterr().out == "pr-create-gate: body checked — No issue\n"


def test_main_niet_letterlijke_repo_buiten_repo_blokkeert(
    tmp_path, project, monkeypatch, capsys
):
    buiten = tmp_path / "buiten"
    buiten.mkdir()
    cmd = "GH_REPO=\"$R\" gh pr create --body 'No issue: alleen docs gewijzigd'"
    assert _run_main(monkeypatch, project, cmd, buiten) == 2
    assert "non-literal" in capsys.readouterr().err


def test_main_andere_repo_overgeslagen(project, monkeypatch, capsys):
    assert _run_main(monkeypatch, project, "gh pr create -R x/y --fill", project) == 0
    assert capsys.readouterr().out == "pr-create-gate: skipped — other repo x/y\n"


def test_main_closes_doorlaat_met_telling(project, monkeypatch, capsys, tmp_path):
    stub = "#!/bin/sh\nprintf '%s\\n' '### Acceptatiecriteria' '- [ ] a' '- [ ] b'\n"
    bin_dir = tool_bin(tmp_path / "bin", (), stubs={"gh": stub})
    monkeypatch.setenv("PATH", f"{bin_dir}:{os.environ['PATH']}")
    (project / "body.md").write_text(closes_body(9, ROWS2), encoding="utf-8")
    rc = _run_main(monkeypatch, project, "gh pr create -F body.md", project)
    assert rc == 0
    assert capsys.readouterr().out == (
        "pr-create-gate: body checked — Closes #9 (2 AC, 2 rows)\n"
    )


def test_main_closes_ac_mismatch_blokkeert(project, monkeypatch, capsys, tmp_path):
    stub = "#!/bin/sh\nprintf '%s\\n' '### Acceptatiecriteria' '- [ ] a' '- [ ] b' '- [ ] c'\n"
    bin_dir = tool_bin(tmp_path / "bin", (), stubs={"gh": stub})
    monkeypatch.setenv("PATH", f"{bin_dir}:{os.environ['PATH']}")
    (project / "body.md").write_text(closes_body(9, ROWS2), encoding="utf-8")
    rc = _run_main(monkeypatch, project, "gh pr create -F body.md", project)
    assert rc == 2
    assert "3 acceptance criteria" in capsys.readouterr().err


def test_main_meerdere_creates_een_blokkade_blokkeert(project, monkeypatch, capsys):
    cmd = "gh pr create --body 'No issue: alleen docs gewijzigd'; gh pr create -t x"
    assert _run_main(monkeypatch, project, cmd, project) == 2
    assert "no body" in capsys.readouterr().err


def test_main_terugval_zonder_gh_meldt_en_laat_door(
    project, monkeypatch, capsys, tmp_path
):
    bin_dir = tool_bin(tmp_path / "bin", ("git",))
    monkeypatch.setenv("PATH", str(bin_dir))
    (project / "body.md").write_text(closes_body(9, ROWS1), encoding="utf-8")
    assert _run_main(monkeypatch, project, "gh pr create -F body.md", project) == 0
    uit = capsys.readouterr().out
    assert "body checked — Closes #9 (AC not counted, 1 rows)" in uit
    assert "AC count not measurable for #9 (gh is missing)" in uit


@pytest.fixture(autouse=True)
def _deadline_terug(monkeypatch):
    """`main` zet de moduleglobal `_deadline`; zet hem na elke test terug op `None`."""
    monkeypatch.setattr(gate, "_deadline", None)


def _trage_gh(monkeypatch, tmp_path, slaap=3):
    stub = f"#!/bin/sh\nsleep {slaap}\n"
    bin_dir = tool_bin(tmp_path / "bin", (), stubs={"gh": stub})
    monkeypatch.setenv("PATH", f"{bin_dir}:{os.environ['PATH']}")


def _drie_closes(met_evidence: tuple[int, ...]) -> str:
    kop = "Closes #5\nCloses #6\nCloses #7\n\n"
    blokken = "".join(
        closes_body(nr, ROWS1).split("\n\n", 2)[2] + "\n" for nr in met_evidence
    )
    return kop + blokken


def test_main_deadline_begrenst_trage_gh_terugval(
    project, monkeypatch, capsys, tmp_path
):
    # Drie Closes-doelen, gh hangt: zonder totale deadline kost dit 3 x timeout.
    monkeypatch.setattr(gate, "DEADLINE_S", 1)
    _trage_gh(monkeypatch, tmp_path)
    (project / "body.md").write_text(_drie_closes((5, 6, 7)), encoding="utf-8")
    start = time.monotonic()
    rc = _run_main(monkeypatch, project, "gh pr create -F body.md", project)
    verstreken = time.monotonic() - start
    uit = capsys.readouterr().out
    assert rc == 0
    assert verstreken < 2.5
    for nr in (5, 6, 7):
        assert f"AC count not measurable for #{nr} (deadline reached)" in uit


def test_main_deadline_laat_blokkade_van_eerder_doel_staan(
    project, monkeypatch, capsys, tmp_path
):
    # #5 mist zijn Evidence-blok: dat blijft een blokkade, ook als de deadline
    # verloopt tijdens de AC-fetch van #6 en #7.
    monkeypatch.setattr(gate, "DEADLINE_S", 1)
    _trage_gh(monkeypatch, tmp_path)
    (project / "body.md").write_text(_drie_closes((6, 7)), encoding="utf-8")
    start = time.monotonic()
    rc = _run_main(monkeypatch, project, "gh pr create -F body.md", project)
    verstreken = time.monotonic() - start
    err = capsys.readouterr().err
    assert rc == 2
    assert verstreken < 2.5
    assert "### Evidence #5" in err


def test_main_deadline_git_blokkeert_luid(project, monkeypatch, capsys, tmp_path):
    # Een hangende git bij de scopetoets is geen bewijs van "binnen" of "buiten".
    monkeypatch.setattr(gate, "DEADLINE_S", 1)
    bin_dir = tool_bin(tmp_path / "bin", (), stubs={"git": "#!/bin/sh\nsleep 3\n"})
    monkeypatch.setenv("PATH", f"{bin_dir}:{os.environ['PATH']}")
    (project / "body.md").write_text(closes_body(9, ROWS1), encoding="utf-8")
    start = time.monotonic()
    rc = _run_main(monkeypatch, project, "gh pr create -F body.md", project)
    verstreken = time.monotonic() - start
    assert rc == 2
    assert verstreken < 2.5
    assert "COULD NOT CHECK" in capsys.readouterr().err


def test_main_deadline_op_voor_git_blokkeert(project, monkeypatch, capsys):
    monkeypatch.setattr(gate, "DEADLINE_S", -1)
    rc = _run_main(monkeypatch, project, "gh pr create --body 'zie titel'", project)
    assert rc == 2
    assert "COULD NOT CHECK: deadline reached" in capsys.readouterr().err


def test_fetch_ac_count_deadline_op(monkeypatch):
    monkeypatch.setattr(gate, "_deadline", time.monotonic() - 1)
    assert gate.fetch_ac_count(5) == (None, "deadline reached")


def test_cli_als_script(project, tmp_path):
    # Zoals de hook hem aanroept: een eigen proces onder python3.
    import subprocess
    import sys

    proc = subprocess.run(
        [sys.executable, str(INSTALLED_LIB / "pr_body_gate.py"), str(project)],
        input="gh pr create --body 'No issue: alleen docs gewijzigd'",
        capture_output=True,
        text=True,
        env=dict(os.environ, CLAUDE_PROJECT_DIR=str(project)),
        check=False,
    )
    assert proc.returncode == 0, proc.stderr
    assert proc.stdout == "pr-create-gate: body checked — No issue\n"


# --- lighthouse: headings and locale --------------------------------------------


@pytest.mark.parametrize("kop", ["### Acceptatiecriteria", "### Acceptance criteria"])
def test_ac_heading_any_language(kop, tmp_path, monkeypatch):
    """Een Nederlands issue in een Engelse repo telt zijn AC zoals een Engels issue."""
    toml = tmp_path / "lighthouse.toml"
    toml.write_text('[project]\nlanguage = "en"\n\n[forge]\nkind = "github"\nrepo = "Acme/widgets"\n')
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(toml))
    assert gate.count_acs(f"{kop}\n- [ ] a\n- [x] b\n### Next\n- [ ] c") == 2


def _git_speaks_german(where) -> bool:
    proc = subprocess.run(
        ["git", "-C", str(where), "rev-parse", "--git-common-dir"],
        capture_output=True,
        text=True,
        env={**os.environ, "LANG": "de_DE.UTF-8", "LC_ALL": "", "LANGUAGE": ""},
        check=False,
    )
    return proc.returncode != 0 and "not a git repository" not in proc.stderr


def test_not_a_git_repository_under_other_locale(tmp_path, project, monkeypatch):
    """Onder een andere taal blijft een map buiten git `buiten deze repo` (inventaris §2.12).

    Git levert geen Nederlandse vertaling; Duits laat dezelfde fout zien.
    """
    buiten = tmp_path / "buiten"
    buiten.mkdir()
    if not _git_speaks_german(buiten):
        pytest.skip("git spreekt hier geen Duits (geen de_DE-locale of vertaling)")
    monkeypatch.setenv("LANG", "de_DE.UTF-8")
    monkeypatch.setenv("LC_ALL", "")
    monkeypatch.setenv("LANGUAGE", "")
    assert gate.in_scope(_pc(str(buiten)), str(project)) == (
        "overslaan",
        "skipped — outside this repo",
    )
