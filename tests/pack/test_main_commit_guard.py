"""pack/core main-commit-guard.sh, as installed: blokkeert hij `git commit`/`git push` op `main`?

De hook is bash en leest zijn payload als JSON op stdin. Deze tests draaien hem
als subprocess met echte tijdelijke paden, zodat het contract (exit code +
stderr/stdout) getest wordt en niet een herimplementatie ervan.
"""

import json
import os
import shutil
import subprocess
from pathlib import Path

from tests.pack._testkit import GUARD, bash_payload, init_repo, install_core, run_hook


def _run(
    hook: Path,
    payload: dict,
    cwd: Path,
    env: dict | None = None,
) -> subprocess.CompletedProcess:
    return subprocess.run(
        [str(hook)],
        input=json.dumps(payload),
        capture_output=True,
        text=True,
        cwd=str(cwd),
        env=dict(os.environ, **(env or {})),
        check=False,
    )


_GIT_ENV = {
    "GIT_AUTHOR_NAME": "t",
    "GIT_AUTHOR_EMAIL": "t@example.com",
    "GIT_COMMITTER_NAME": "t",
    "GIT_COMMITTER_EMAIL": "t@example.com",
}


def _git(repo: Path, *args: str) -> None:
    subprocess.run(
        ["git", "-C", str(repo), *args],
        check=True,
        capture_output=True,
        text=True,
        env=dict(os.environ, **_GIT_ENV),
    )


def _init_repo(path: Path, branch: str = "main") -> None:
    """Echte git-repo met één commit, zodat de hook echte plumbing ziet."""
    path.mkdir(parents=True, exist_ok=True)
    _git(path, "init", "-q", "-b", branch)
    (path / "README.md").write_text("# repo\n", encoding="utf-8")
    _git(path, "add", "README.md")
    _git(path, "commit", "-qm", "init")


COMMIT_GUARD = GUARD


def _bash(cmd: str, cwd: Path) -> dict:
    return {"tool_name": "Bash", "cwd": str(cwd), "tool_input": {"command": cmd}}


def test_commit_guard_blocks_commit_on_main(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(COMMIT_GUARD, _bash('git commit -m "iets"', repo), cwd=repo)

    assert result.returncode == 2, result.stdout
    assert "main" in result.stderr


def test_commit_guard_allows_commit_on_feature_branch(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    _git(repo, "checkout", "-q", "-b", "issue-1-iets")

    result = _run(COMMIT_GUARD, _bash('git commit -m "iets"', repo), cwd=repo)

    assert result.returncode == 0, result.stderr


def test_commit_guard_blocks_commit_targeting_main_checkout_via_dash_c(tmp_path):
    """De gedocumenteerde ontsnapping: worktree-cwd, `git -C <hoofdboom> commit`."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    wt = repo / ".claude" / "worktrees" / "issue-1-iets"
    _git(repo, "worktree", "add", "-q", "-b", "issue-1-iets", str(wt))

    result = _run(COMMIT_GUARD, _bash(f'git -C {repo} commit -m "iets"', wt), cwd=wt)

    assert result.returncode == 2, result.stdout


def test_commit_guard_follows_cd_in_a_chain(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    wt = repo / ".claude" / "worktrees" / "issue-1-iets"
    _git(repo, "worktree", "add", "-q", "-b", "issue-1-iets", str(wt))

    result = _run(COMMIT_GUARD, _bash(f'cd {repo} && git commit -m "iets"', wt), cwd=wt)

    assert result.returncode == 2, result.stdout


def test_commit_guard_blocks_push_to_main_from_feature_branch(tmp_path):
    """`git push origin HEAD:main` omzeilt de branch-toets; de refspec niet."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    _git(repo, "checkout", "-q", "-b", "issue-1-iets")

    result = _run(COMMIT_GUARD, _bash("git push origin HEAD:main", repo), cwd=repo)

    assert result.returncode == 2, result.stdout


def test_commit_guard_ignores_heredoc_content(tmp_path):
    """De vals-positief van pre-commit-tests.sh mag hier niet optreden."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    cmd = "cat > plan.md <<'XEOF'\ngit commit -m \"voorbeeld\"\ngit push origin main\nXEOF"

    result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)

    assert result.returncode == 0, result.stderr


def test_commit_guard_ignores_commit_word_in_a_message(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    _git(repo, "checkout", "-q", "-b", "issue-1-iets")

    result = _run(COMMIT_GUARD, _bash('echo "git commit -m main"', repo), cwd=repo)

    assert result.returncode == 0, result.stderr


def test_commit_guard_sentinel_opens_the_gate(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    (repo / ".claude").mkdir(parents=True, exist_ok=True)
    (repo / ".claude" / "allow-main").touch()

    result = _run(COMMIT_GUARD, _bash('git commit -m "bewust op main"', repo), cwd=repo)

    assert result.returncode == 0, result.stderr


def test_commit_guard_fails_loud_without_tooling(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    empty_bin = tmp_path / "leeg-bin"
    empty_bin.mkdir()

    result = _run(
        COMMIT_GUARD,
        _bash('git commit -m "iets"', repo),
        cwd=repo,
        env={"PATH": str(empty_bin)},
    )

    assert result.returncode == 2
    assert "COULD NOT CHECK" in result.stderr


# --- gereedschapskist voor de scherpere gevallen hieronder -------------------

_REAL_TOOLS = ("jq", "python3", "git", "cat", "mktemp", "rm", "sh")


def _tool_bin(
    path: Path, *, drop: str = "", stubs: dict[str, str] | None = None
) -> Path:
    """Een PATH-map met de echte tooling, op één na, plus eventuele stubs.

    Zo is per limb van de preflight te toetsen dat juist díé limb luid faalt —
    een lege PATH dekt ze alle drie tegelijk en dus geen enkele apart (M11).
    """
    path.mkdir(parents=True, exist_ok=True)
    for tool in _REAL_TOOLS:
        if tool == drop:
            continue
        real = shutil.which(tool)
        if real:
            (path / tool).symlink_to(real)
    for name, script in (stubs or {}).items():
        stub = path / name
        if stub.exists():
            stub.unlink()
        stub.write_text(script, encoding="utf-8")
        stub.chmod(0o755)
    return path


def test_commit_guard_fails_loud_without_jq(tmp_path):
    """M11: de jq-limb apart, niet verstopt achter een lege PATH."""
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        COMMIT_GUARD,
        _bash('git commit -m "iets"', repo),
        cwd=repo,
        env={"PATH": str(_tool_bin(tmp_path / "zonder-jq", drop="jq"))},
    )

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr
    # Juist deze limb: de preflight zelf, niet het vangnet erachter.
    assert "missing tooling" in result.stderr
    assert "jq" in result.stderr


def test_commit_guard_fails_loud_without_python3(tmp_path):
    """M11: de python3-limb apart."""
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        COMMIT_GUARD,
        _bash('git commit -m "iets"', repo),
        cwd=repo,
        env={"PATH": str(_tool_bin(tmp_path / "zonder-py", drop="python3"))},
    )

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr
    assert "missing tooling" in result.stderr
    assert "python3" in result.stderr


def test_commit_guard_fails_loud_without_git(tmp_path):
    """C3: zonder git kan de hook de branch niet zien — dat is geen groen licht."""
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        COMMIT_GUARD,
        _bash('git commit -m "iets"', repo),
        cwd=repo,
        env={"PATH": str(_tool_bin(tmp_path / "zonder-git", drop="git"))},
    )

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr
    assert "missing tooling" in result.stderr
    assert "git" in result.stderr


def test_commit_guard_blocks_when_rev_parse_breaks(tmp_path):
    """C3/I8: een git die stukloopt is geen 'geen repo' — dus blokkeren.

    De preflight is tevreden (git bestaat), de parser vond een commit, maar
    `rev-parse` geeft een onbekende fout. Stil doorlaten zou hier het gat zijn.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)
    kapot = _tool_bin(
        tmp_path / "kapotte-git",
        stubs={"git": "#!/bin/sh\necho 'fatal: kapot object store' >&2\nexit 1\n"},
    )

    result = _run(
        COMMIT_GUARD,
        _bash('git commit -m "iets"', repo),
        cwd=repo,
        env={"PATH": str(kapot)},
    )

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr


def test_commit_guard_passes_when_target_is_simply_not_a_repo(tmp_path):
    """De tegenhanger van de vorige test: 'geen repo' is een echte uitkomst."""
    los = tmp_path / "geen-repo"
    los.mkdir()

    result = _run(COMMIT_GUARD, _bash('git commit -m "iets"', los), cwd=los)

    assert result.returncode == 0, result.stderr


def test_commit_guard_blocks_when_the_parser_crashes(tmp_path):
    """I8: een parser die niet met 0 of 3 eindigt mag geen stille pass worden."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    # Only the parser crashes; the config read before it uses the real python3.
    real = shutil.which("python3")
    stuk = _tool_bin(
        tmp_path / "kapotte-python",
        stubs={
            "python3": "#!/bin/sh\ncase \"$1\" in *git_invocations.py) "
            "echo 'Traceback: stuk' >&2; exit 1;; esac\n"
            f'exec {real} "$@"\n'
        },
    )

    result = _run(
        COMMIT_GUARD,
        _bash('git commit -m "iets"', repo),
        cwd=repo,
        env={"PATH": str(stuk)},
    )

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr
    assert "parser" in result.stderr


def test_commit_guard_blocks_unparseable_command(tmp_path):
    """I9: open quote = geen conclusie. Niet kunnen kijken is geen negatief resultaat."""
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        COMMIT_GUARD,
        _bash("echo 'unbalanced && git commit -m y", repo),
        cwd=repo,
    )

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr


def test_commit_guard_passes_unparseable_input_without_a_commit(tmp_path):
    """De tweestapsterugval: 'niet te parsen' is niet 'niet kunnen kijken'.

    Staat er in de ruwe tekst nergens `git` mét `commit` of `push`, dan valt
    hier niets te beschermen en niets te melden. Zonder die stap zette één
    onbalans buiten elke repo álle volgende commando's op slot — een lockout
    waar de norm niets voor terugkrijgt. Het tegenwicht staat in
    test_commit_guard_blocks_unparseable_command: kán er een commit in zitten,
    dan blokkeert het onverkort.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)
    los = tmp_path / "geen-repo"
    los.mkdir()

    for cmd, cwd in (
        ("echo 'unbalanced", los),
        ("echo 'unbalanced", repo),
        ('ls -la "open', repo),
        ("awk '{print $1}", repo),
        # Beide stappen apart: 'push' zonder `git` is geen push, en `git`
        # zonder 'commit'/'push' is niets om te bewaken.
        ("echo 'we push this door de brievenbus", repo),
        ("git status && echo 'unbalanced", repo),
    ):
        result = _run(COMMIT_GUARD, _bash(cmd, cwd), cwd=cwd)
        assert result.returncode == 0, f"{cmd!r} werd geblokkeerd: {result.stderr}"


def test_commit_guard_reads_ansi_c_quoting_with_an_escaped_apostrophe(tmp_path):
    """F4: in `$'...'` sluit een backslash-escaped apostrof de string niet.

    De tokenizer las `$'it\\'s'` als een gewone quote, sloot hem op de escaped
    apostrof en liet de laatste openstaan — 'niet te parsen', dus een blokkade.

    De rc-0-gevallen alleen zouden ook groen blijven als de tokenizer nog
    struikelt, want de tweestapsterugval vangt ze op (geen `git` in de tekst).
    Wat de fix echt vastpint is het laatste geval: dáár moet er ná de
    ANSI-C-string nog een commit gelezen worden, en die staat op een
    feature-branch, dus doorlaten kan alleen als het commando écht geparsed is.
    """
    los = tmp_path / "geen-repo"
    los.mkdir()
    repo = tmp_path / "repo"
    _init_repo(repo)

    assert _run(COMMIT_GUARD, _bash("echo $'it\\'s'", los), cwd=los).returncode == 0
    assert _run(COMMIT_GUARD, _bash("echo $'don\\'t'", repo), cwd=repo).returncode == 0

    _git(repo, "checkout", "-q", "-b", "issue-1-iets")
    doorgeparsed = _run(
        COMMIT_GUARD, _bash("echo $'don\\'t' && git commit -m x", repo), cwd=repo
    )
    assert doorgeparsed.returncode == 0, doorgeparsed.stderr


def test_commit_guard_blocks_on_invalid_json_payload(tmp_path):
    """Onleesbare payload: geen commando, dus ook geen toets — en dus geen pass."""
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = subprocess.run(
        [str(COMMIT_GUARD)],
        input="dit is geen json",
        capture_output=True,
        text=True,
        cwd=str(repo),
        check=False,
    )

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr


def test_commit_guard_never_writes_to_stdout(tmp_path):
    """Contract: blokkeren gaat via exit 2 + stderr, nooit via een systemMessage."""
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(COMMIT_GUARD, _bash('git commit -m "iets"', repo), cwd=repo)

    assert result.returncode == 2
    assert result.stdout == ""


# --- C1: newline is een commandoscheiding -----------------------------------


def test_commit_guard_sees_a_commit_on_the_second_line(tmp_path):
    """C1: Claude Code stuurt routinematig meerregelige Bash. Dominante vorm."""
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        COMMIT_GUARD, _bash('git add -A\ngit commit -m "iets"', repo), cwd=repo
    )

    assert result.returncode == 2, result.stdout


def test_commit_guard_sees_a_commit_after_a_non_git_line(tmp_path):
    """Regel 1 begint met een niet-git-woord; dat mag regel 2 niet dekken."""
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        COMMIT_GUARD, _bash('echo hallo\ngit commit -m "iets"', repo), cwd=repo
    )

    assert result.returncode == 2, result.stdout


def test_commit_guard_sees_a_push_to_main_on_a_later_line(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    _git(repo, "checkout", "-q", "-b", "issue-1-iets")

    result = _run(
        COMMIT_GUARD, _bash("echo hallo\ngit push origin HEAD:main", repo), cwd=repo
    )

    assert result.returncode == 2, result.stdout


def test_commit_guard_follows_a_line_continuation(tmp_path):
    """`git add -A && \\<newline>git commit` is één commando, geen twee halve."""
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        COMMIT_GUARD, _bash('git add -A && \\\ngit commit -m "iets"', repo), cwd=repo
    )

    assert result.returncode == 2, result.stdout


def test_commit_guard_stays_silent_on_a_harmless_multiline_command(tmp_path):
    """Tegenwicht bij C1: meerregelig mag niet ineens alles blokkeren."""
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        COMMIT_GUARD, _bash("git status\ngit log --oneline -1\nls -la", repo), cwd=repo
    )

    assert result.returncode == 0, result.stderr


# --- C2: paden met ~ en variabelen ------------------------------------------


def test_commit_guard_expands_tilde_in_dash_c(tmp_path):
    """C2: `~/repositories/personal_ai_agent` is precies hoe CLAUDE.md het spelt."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    elders = tmp_path / "elders"
    elders.mkdir()

    result = _run(
        COMMIT_GUARD,
        _bash('git -C ~/repo commit -m "iets"', elders),
        cwd=elders,
        env={"HOME": str(tmp_path)},
    )

    assert result.returncode == 2, result.stdout


def test_commit_guard_expands_home_variable_in_cd(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    elders = tmp_path / "elders"
    elders.mkdir()

    result = _run(
        COMMIT_GUARD,
        _bash('cd "$HOME/repo" && git commit -m "iets"', elders),
        cwd=elders,
        env={"HOME": str(tmp_path)},
    )

    assert result.returncode == 2, result.stdout


def test_commit_guard_blocks_when_a_path_variable_is_unknown(tmp_path):
    """C2: een onbekende variabele mag niet stil naar iets onschuldigs resolven."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    _git(repo, "checkout", "-q", "-b", "issue-1-iets")

    result = _run(
        COMMIT_GUARD, _bash('git -C "$HOOFDBOOM" commit -m "iets"', repo), cwd=repo
    )

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr


def test_commit_guard_unresolvable_path_still_honours_the_sentinel(tmp_path):
    """Het ontsnappingsluik moet ook werken als het pad niet te herleiden is."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    (repo / ".claude").mkdir(parents=True, exist_ok=True)
    (repo / ".claude" / "allow-main").touch()

    result = _run(
        COMMIT_GUARD, _bash('git -C "$HOOFDBOOM" commit -m "iets"', repo), cwd=repo
    )

    assert result.returncode == 0, result.stderr


# --- I4/I5: wrappers, absolute paden, subshells -----------------------------


def test_commit_guard_recognises_git_by_basename_and_through_env(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    git_bin = shutil.which("git")
    assert git_bin

    for cmd in (
        f'{git_bin} commit -m "iets"',
        'env git commit -m "iets"',
        '/usr/bin/env git commit -m "iets"',
        'env GIT_PAGER=cat git commit -m "iets"',
    ):
        result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)
        assert result.returncode == 2, f"{cmd!r} kwam er langs: {result.stdout}"


def test_commit_guard_looks_inside_subshells_and_substitutions(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    wt = repo / ".claude" / "worktrees" / "issue-1-iets"
    _git(repo, "worktree", "add", "-q", "-b", "issue-1-iets", str(wt))

    for cmd in (
        f'(cd {repo} && git commit -m "iets")',
        f'{{ cd {repo}; git commit -m "iets"; }}',
        'echo "$(git -C %s commit -m iets)"' % repo,
        f"echo `git -C {repo} commit -m iets`",
    ):
        result = _run(COMMIT_GUARD, _bash(cmd, wt), cwd=wt)
        assert result.returncode == 2, f"{cmd!r} kwam er langs: {result.stdout}"


# --- I6: pushvormen die op main landen --------------------------------------


def test_commit_guard_blocks_push_all_when_a_local_main_exists(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    _git(repo, "checkout", "-q", "-b", "issue-1-iets")

    for cmd in ("git push --all origin", "git push --mirror origin"):
        result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)
        assert result.returncode == 2, f"{cmd!r} kwam er langs: {result.stdout}"


def test_commit_guard_allows_push_all_without_a_local_main(tmp_path):
    """Tegenwicht: zonder lokale main duwt `--all` niets naar main."""
    repo = tmp_path / "repo"
    _init_repo(repo, branch="issue-1-iets")

    result = _run(COMMIT_GUARD, _bash("git push --all origin", repo), cwd=repo)

    assert result.returncode == 0, result.stderr


def test_commit_guard_blocks_forced_and_full_ref_pushes_to_main(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    _git(repo, "checkout", "-q", "-b", "issue-1-iets")

    for cmd in (
        "git push origin +main",
        "git push origin +HEAD:refs/heads/main",
        "git push -u origin main",
    ):
        result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)
        assert result.returncode == 2, f"{cmd!r} kwam er langs: {result.stdout}"


def test_commit_guard_allows_a_push_to_a_main_prefixed_branch(tmp_path):
    """`main-fix` is geen `main` — de refspec wordt echt uit elkaar gehaald."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    _git(repo, "checkout", "-q", "-b", "issue-1-iets")

    result = _run(COMMIT_GUARD, _bash("git push origin HEAD:main-fix", repo), cwd=repo)

    assert result.returncode == 0, result.stderr


# --- I7/M10: heredocs -------------------------------------------------------


def test_commit_guard_does_not_treat_a_shift_or_string_as_a_heredoc(tmp_path):
    """I7: `<<` in een string of in `$(( ))` mag de rest niet wegslikken."""
    repo = tmp_path / "repo"
    _init_repo(repo)

    for cmd in (
        'echo "a << b"\ngit commit -m "iets"',
        'echo $((1 << 2))\ngit commit -m "iets"',
    ):
        result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)
        assert result.returncode == 2, f"{cmd!r} kwam er langs: {result.stdout}"


def test_commit_guard_matches_a_hyphenated_heredoc_delimiter(tmp_path):
    """I7: `<<'EOF-1'` moet compleet gematcht worden, anders eindigt het nooit."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    cmd = "cat > plan.md <<'EOF-1'\ngit commit -m voorbeeld\nEOF-1\ngit commit -m echt"

    result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)

    # De body is overgeslagen, de regel ná de terminator juist niet.
    assert result.returncode == 2, result.stdout


def test_commit_guard_strips_a_heredoc_body_containing_an_operator(tmp_path):
    """M10: pint het strippen zelf, niet het 'begint niet met git'-vangnet."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    cmd = "cat > plan.md <<'XEOF'\nblah && git commit -m voorbeeld\nXEOF"

    result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)

    assert result.returncode == 0, result.stderr


def test_commit_guard_ignores_a_tab_stripped_heredoc(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    cmd = 'cat > plan.md <<-EOF\n\tgit commit -m "voorbeeld"\n\tEOF'

    result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)

    assert result.returncode == 0, result.stderr


# --- N1/N2: `#`-commentaar --------------------------------------------------


def test_commit_guard_ignores_a_hash_comment(tmp_path):
    """N1: een losse apostrof in een commentaar blokkeerde élk commando.

    De tokenizer kende `#` niet, liep de regel af op zoek naar een sluitende
    quote en riep 'niet te parsen' — buiten elke repo, zonder git in het
    commando. Dat is de lockout die de I9-fix per ongeluk maakte.
    """
    los = tmp_path / "geen-repo"
    los.mkdir()

    for cmd in (
        "ls -la # dat 's genoeg",
        "uv run pytest tests/unit -q  # don't run the slow ones",
        'echo hoi # " losse dubbele quote',
    ):
        result = _run(COMMIT_GUARD, _bash(cmd, los), cwd=los)
        assert result.returncode == 0, f"{cmd!r} werd geblokkeerd: {result.stderr}"


def test_commit_guard_only_treats_a_hash_at_word_start_as_comment(tmp_path):
    """Tegenwicht bij N1: niet élke `#` opent een commentaar.

    `foo#bar` en `$#` staan midden in een woord, `'a # b'` staat in een quote —
    daar mag de hook niets wegknippen, anders verdwijnt er commando-tekst.

    F1: de rc-0-helft hierónder ziet dat verschil NIET (die geeft 0 of de regel
    nu afgekapt wordt of niet). Wat `word_start` echt vastpint is de rc-2-helft:
    knipt een `#` midden in een woord de regel af, dan verdwijnt de commit
    erachter in het weggegooide restant en gaat een commit op main er stil
    langs. Dat is fail-open, dus dat is wat hier moet vastzitten.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)

    # In een quote of midden in een woord: geen commentaar, dus de commit
    # erachter wordt gewoon gezien.
    for cmd in (
        'echo "a # b" && git commit -m x',
        "echo $# && git commit -m x",
        "echo foo#bar && git commit -m x",
        "curl https://x.dev/a#b && git commit -m x",
    ):
        blokkeert = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)
        assert blokkeert.returncode == 2, f"{cmd!r} kwam er langs: {blokkeert.stdout}"

    _git(repo, "checkout", "-q", "-b", "issue-1-iets")
    for cmd in ("echo foo#bar", "set -- a b; echo $#", "echo 'a # b'"):
        result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)
        assert result.returncode == 0, f"{cmd!r} werd geblokkeerd: {result.stderr}"


def test_commit_guard_does_not_arm_a_heredoc_from_a_comment(tmp_path):
    """N2: dezelfde blinde vlek, andere kant op.

    `<<` in een commentaar armeerde de heredoc-overslag, waarna de commit op de
    volgende regel als 'body' werd weggeslikt en main gewoon gehaald werd.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        COMMIT_GUARD, _bash("echo x # zie << EOF\ngit commit -m x", repo), cwd=repo
    )

    assert result.returncode == 2, result.stdout


# --- N3: een sub-shell geeft zijn cwd niet door -----------------------------


def test_commit_guard_restores_the_directory_after_a_subshell(tmp_path):
    """N3: `(cd elders)` en `$(cd elders)` gelden alleen bínnen de haakjes.

    Bash herstelt de cwd, de hook niet — dus een doodgewone commit op de
    feature-branch werd geblokkeerd omdat de `cd` was blijven hangen.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)
    wt = repo / ".claude" / "worktrees" / "issue-1-iets"
    _git(repo, "worktree", "add", "-q", "-b", "issue-1-iets", str(wt))

    for cmd in (
        f"(cd {repo} && git rev-parse HEAD)\ngit add -A\ngit commit -m x",
        f'SHA=$(cd {repo} && git rev-parse HEAD)\ngit commit -m "ref $SHA"',
        f"`cd {repo} && git rev-parse HEAD`\ngit commit -m x",
    ):
        result = _run(COMMIT_GUARD, _bash(cmd, wt), cwd=wt)
        assert result.returncode == 0, f"{cmd!r} werd geblokkeerd: {result.stderr}"


# --- N4: een push-bestemming die niet te herleiden is -----------------------


def test_commit_guard_blocks_an_unresolvable_push_destination(tmp_path):
    """N4: `git push origin $BR` kán main zijn — en dat is niet te zien.

    Bewuste afruil van de planeigenaar: dit blokkeert ook een onschuldige
    `git push origin "$BRANCH"`. De sentinel is het luik, niet een uitzondering
    in de code.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)
    _git(repo, "checkout", "-q", "-b", "issue-1-iets")

    for cmd in ('git push origin "$(echo main)"', "git push origin $BR"):
        result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)
        assert result.returncode == 2, f"{cmd!r} kwam er langs: {result.stdout}"
        assert "COULD NOT CHECK" in result.stderr


def test_commit_guard_ignores_an_unresolvable_remote(tmp_path):
    """F2: het eerste positionele argument is de REMOTE, niet de bestemming.

    `git push "$REMOTE" issue-1-iets` gaat aantoonbaar niet naar main — de
    branch staat er letterlijk. Die toch blokkeren viel buiten de afruil van N4
    en maakte van elke gescripte push een blokkade.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)
    _git(repo, "checkout", "-q", "-b", "issue-1-iets")

    for cmd in ('git push "$REMOTE" issue-1-iets', "git push -u $REMOTE issue-1-iets"):
        result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)
        assert result.returncode == 0, f"{cmd!r} werd geblokkeerd: {result.stderr}"

    # Tegenwicht: een onherleidbare BESTEMMING blijft blokkeren (N4), en een
    # letterlijke main achter een onherleidbare remote net zo goed.
    for cmd in ('git push origin "issue-$NR"', "git push $REMOTE main"):
        result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)
        assert result.returncode == 2, f"{cmd!r} kwam er langs: {result.stdout}"


def test_commit_guard_unresolvable_push_honours_the_sentinel(tmp_path):
    """Het luik moet ook hier opengaan, anders is de afruil van N4 een val."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    _git(repo, "checkout", "-q", "-b", "issue-1-iets")
    (repo / ".claude").mkdir(parents=True, exist_ok=True)
    (repo / ".claude" / "allow-main").touch()

    result = _run(COMMIT_GUARD, _bash("git push origin $BR", repo), cwd=repo)

    assert result.returncode == 0, result.stderr


# --- N5: de vierde preflight-limb -------------------------------------------


def test_commit_guard_fails_loud_without_cat(tmp_path):
    """N5: `cat` leest de payload van stdin; zonder is er geen commando.

    Dezelfde eis als bij jq/python3/git — anders pint niets deze limb en kan hij
    stilletjes weg (I8 opnieuw).
    """
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        COMMIT_GUARD,
        _bash('git commit -m "iets"', repo),
        cwd=repo,
        env={"PATH": str(_tool_bin(tmp_path / "zonder-cat", drop="cat"))},
    )

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr
    assert "missing tooling" in result.stderr
    assert "cat" in result.stderr


# --- N6: cd die faalt, cd -, pushd/popd -------------------------------------


def test_commit_guard_stays_put_when_cd_cannot_succeed(tmp_path):
    """N6: `cd <bestaat niet>` verplaatst bash niet — de hook nam het pad over.

    Zo landde `cd typo; git commit` op main zonder dat de hook het zag.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        COMMIT_GUARD,
        _bash(f"cd {tmp_path / 'bestaat-niet'}; git commit -m x", repo),
        cwd=repo,
    )

    assert result.returncode == 2, result.stdout


def test_commit_guard_passes_a_commit_in_a_directory_this_command_creates(tmp_path):
    """F3: de motie die CLAUDE.md voorschrijft, blokkeerde zichzelf.

    `git worktree add X && cd X && git commit` — X bestaat bij hook-tijd nog
    niet. De hook bleef staan en erfde de branch van de OUDE map: main. Dat
    kent een branch toe aan een plek waar bash niet is. Achter `&&` draait de
    commit alleen als de map er wél kwam, en een map die BUITEN elke repo
    ontstaat heeft geen branch die deze hook te beschermen heeft.

    De grens ligt bij die voorouder, niet bij 'bestaat nog niet': een verse map
    ONDER een checkout erft diens branch wél. Zie
    test_commit_guard_blocks_a_commit_in_a_new_dir_inside_a_checkout.

    Tegenwicht staat in test_commit_guard_stays_put_when_cd_cannot_succeed:
    zonder `&&` draait de rest ook als de `cd` mislukte, en dan blokkeert het
    nog steeds.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)
    wt = tmp_path / "wt"
    nieuw = tmp_path / "nieuw"

    motie = _run(
        COMMIT_GUARD,
        _bash(f"git worktree add {wt} -b br2 && cd {wt} && git commit -m x", repo),
        cwd=repo,
    )
    assert motie.returncode == 0, motie.stderr

    verse_repo = _run(
        COMMIT_GUARD,
        _bash(
            f"mkdir -p {nieuw} && cd {nieuw} && git init && git commit -m init", repo
        ),
        cwd=repo,
    )
    assert verse_repo.returncode == 0, verse_repo.stderr

    # De asymmetrie is bewust: een onherleidbaar `-C`-pad NOEMT een bestaande
    # repo en blijft blokkeren. Harmoniseer die twee niet.
    dash_c = _run(COMMIT_GUARD, _bash('git -C "$ELDERS" commit -m x', repo), cwd=repo)
    assert dash_c.returncode == 2, dash_c.stdout
    assert "COULD NOT CHECK" in dash_c.stderr

    # En een `cd` naar een ONHERLEIDBAAR pad is iets anders dan een map die er
    # nog niet is: van een variabele weten we niets, dus die blokkeert ook.
    variabele = _run(
        COMMIT_GUARD, _bash('cd "$ELDERS" && git commit -m x', repo), cwd=repo
    )
    assert variabele.returncode == 2, variabele.stdout
    assert "COULD NOT CHECK" in variabele.stderr


def test_commit_guard_blocks_a_commit_in_a_new_dir_inside_a_checkout(tmp_path):
    """C1: 'de map bestaat nog niet' was een te ruime vrijbrief.

    `mkdir -p sub && cd sub && git commit` commit écht op main — sub is een
    submap van dezelfde checkout, met dezelfde branch. De vrijstelling kijkt
    daarom naar de dichtstbijzijnde map die er wél is: ligt die in een repo,
    dan geldt die repo. Alleen een voorouder buiten elke repo is vrij.

    Rijen 5 en 6 volgen daaruit: nu de verse map een echt pad houdt in plaats
    van 'onbekend', werken `cd ..` en `git -C ..` er weer relatief vanaf.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)

    submap = _run(
        COMMIT_GUARD, _bash("mkdir -p sub && cd sub && git commit -m x", repo), cwd=repo
    )
    assert submap.returncode == 2, submap.stdout

    terug = _run(
        COMMIT_GUARD,
        _bash("mkdir -p q3 && cd q3 && cd .. && git commit -m x", repo),
        cwd=repo,
    )
    assert terug.returncode == 2, terug.stdout

    dash_c_terug = _run(
        COMMIT_GUARD,
        _bash("mkdir -p q4 && cd q4 && git -C .. commit -m x", repo),
        cwd=repo,
    )
    assert dash_c_terug.returncode == 2, dash_c_terug.stdout

    # Tegenwicht: een verse map BUITEN elke repo blijft vrij — dat is de
    # voorgeschreven worktree-motie en die moet blijven werken.
    buiten = _run(
        COMMIT_GUARD,
        _bash(
            f"mkdir -p {tmp_path / 'los'} && cd {tmp_path / 'los'} && git commit -m x",
            repo,
        ),
        cwd=repo,
    )
    assert buiten.returncode == 0, buiten.stderr


def test_commit_guard_blocks_a_push_to_main_from_a_dir_this_command_creates(tmp_path):
    """C1: een LETTERLIJKE main-bestemming heeft geen mapkennis nodig.

    `git worktree add X -b br && cd X && git push origin main` duwt naar main,
    waar X ook ligt. De vrijstelling voor een verse map buiten elke repo mag
    daar niet overheen; die geldt alleen als de bestemming niet main is.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)
    wt = tmp_path / "wtq"

    naar_main = _run(
        COMMIT_GUARD,
        _bash(f"git worktree add {wt} -b brq && cd {wt} && git push origin main", repo),
        cwd=repo,
    )
    assert naar_main.returncode == 2, naar_main.stdout

    # Tegenwicht: dezelfde vorm met een gewone branch blijft vrij.
    naar_branch = _run(
        COMMIT_GUARD,
        _bash(f"git worktree add {wt} -b brq && cd {wt} && git push origin brq", repo),
        cwd=repo,
    )
    assert naar_branch.returncode == 0, naar_branch.stderr


def test_commit_guard_reads_ansi_c_quoting_while_scanning_for_heredocs(tmp_path):
    """F4, tweede aanroepplek: scan_line, niet alleen split_segments.

    Zonder de `$'...'`-tak sluit de scanner de string op de ge-escapete
    apostrof, leest `<<EOF` daarna als een echte heredoc-opener en slikt de
    commit op de volgende regel op als heredoc-body.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        COMMIT_GUARD,
        _bash("echo $'x\\'<<EOF'\ngit commit -m x", repo),
        cwd=repo,
    )

    assert result.returncode == 2, result.stdout


def test_commit_guard_does_not_read_a_wrapped_subshell_as_a_function(tmp_path):
    """F5-guard: `time (git commit -m x)` is geen functiedefinitie.

    Vorm en woordtelling kloppen wél — één woord vóór een `( ... )`-blok — dus
    zonder de eis dat dat blok LEEG is (`()`, de haakjes van de definitie)
    haalt de functie-isolatie de commit uit de wachtrij en ziet niemand hem.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(COMMIT_GUARD, _bash("time (git commit -m x)", repo), cwd=repo)

    assert result.returncode == 2, result.stdout


def test_commit_guard_forgets_an_and_after_the_segment_that_used_it(tmp_path):
    """F3-separator: `&&` geldt voor het EERSTVOLGENDE segment, niet verder.

    Bij `... && cd <verse map>; git commit` staat er geen `&&` vóór de commit,
    dus die draait ook als de `cd` mislukte — en dan staat bash nog in de oude
    map. Blijft de `&&`-vlag van het vorige segment hangen, dan telt de commit
    als 'in de verse map' en glipt hij door de vrijstelling.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)
    buiten = tmp_path / "buiten"

    zonder_and = _run(
        COMMIT_GUARD, _bash(f"echo hoi && cd {buiten}; git commit -m x", repo), cwd=repo
    )
    assert zonder_and.returncode == 2, zonder_and.stdout

    # Tegenwicht: mét `&&` vóór de commit blijft de vrijstelling gelden.
    met_and = _run(
        COMMIT_GUARD,
        _bash(f"echo hoi && cd {buiten} && git commit -m x", repo),
        cwd=repo,
    )
    assert met_and.returncode == 0, met_and.stderr


def test_commit_guard_drops_the_new_dir_flag_on_a_following_cd(tmp_path):
    """apply_cd wist de vlag: een tweede `cd` maakt de vorige verse map irrelevant.

    `mkdir -p n3 && cd n3 && cd "$ELDERS" && git commit` — bash staat na de
    tweede `cd` op een plek die de hook niet kent, en dat is een blokkade. Blijft
    de vlag van de eerste `cd` staan, dan erft die onbekende map de vrijstelling.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)
    buiten = tmp_path / "buiten"

    result = _run(
        COMMIT_GUARD,
        _bash(
            f'mkdir -p {buiten} && cd {buiten} && cd "$ELDERS" && git commit -m x', repo
        ),
        cwd=repo,
    )

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr


def test_commit_guard_does_not_follow_a_cd_in_a_function_body(tmp_path):
    """F5: een functiebody draait pas bij een AANROEP.

    `foo() { cd <main-checkout>; }; git commit` verplaatst bash niet — bash
    definieert alleen. De `cd` volgen alsof hij uitgevoerd werd, blokkeerde een
    commit die gewoon in de worktree landt.
    """
    hoofd = tmp_path / "hoofd"
    _init_repo(hoofd)
    feat = tmp_path / "feat"
    _init_repo(feat)
    _git(feat, "checkout", "-q", "-b", "issue-1-iets")

    definitie = _run(
        COMMIT_GUARD,
        _bash("foo() { cd %s; }; git commit -m x" % hoofd, feat),
        cwd=feat,
    )
    assert definitie.returncode == 0, definitie.stderr

    # Tegenwicht: de commando's ín de body blijven zichtbaar. Alleen de
    # mapstand wordt geïsoleerd — `f() { git commit; }; f` commit écht.
    aanroep = _run(COMMIT_GUARD, _bash("f() { git commit -m x; }; f", hoofd), cwd=hoofd)
    assert aanroep.returncode == 2, aanroep.stdout


def test_commit_guard_follows_cd_dash_back(tmp_path):
    """N6: `cd -` springt terug naar de vorige map."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    wt = repo / ".claude" / "worktrees" / "issue-1-iets"
    _git(repo, "worktree", "add", "-q", "-b", "issue-1-iets", str(wt))

    heen_en_terug = _run(
        COMMIT_GUARD, _bash(f"cd {wt}\ncd -\ngit commit -m x", repo), cwd=repo
    )
    assert heen_en_terug.returncode == 2, heen_en_terug.stdout

    # Tegenwicht: zonder terugsprong blijft de commit in de worktree.
    blijft = _run(COMMIT_GUARD, _bash(f"cd {wt}\ngit commit -m x", repo), cwd=repo)
    assert blijft.returncode == 0, blijft.stderr


def test_commit_guard_follows_pushd_and_popd(tmp_path):
    """N6: `pushd` verplaatst net zo goed als `cd`, `popd` keert terug."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    wt = repo / ".claude" / "worktrees" / "issue-1-iets"
    _git(repo, "worktree", "add", "-q", "-b", "issue-1-iets", str(wt))

    heen = _run(COMMIT_GUARD, _bash(f"pushd {repo} && git commit -m x", wt), cwd=wt)
    assert heen.returncode == 2, heen.stdout

    terug = _run(
        COMMIT_GUARD, _bash(f"pushd {repo} && popd && git commit -m x", wt), cwd=wt
    )
    assert terug.returncode == 0, terug.stderr


# --- N7: wrappers mét argumenten --------------------------------------------


def test_commit_guard_sees_through_wrappers_with_arguments(tmp_path):
    """N7: de wrapperlijst kende alleen argumentloze vormen.

    `bash -c '<script>'` is de opvallendste: het commando zat in een string en
    werd nooit als commando gelezen.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)

    for cmd in (
        "bash -c 'git commit -m x'",
        'sh -c "git commit -m x"',
        "timeout 5 git commit -m x",
        "timeout -k 5 10 git commit -m x",
        "echo x | xargs git commit -m",
        "xargs -n1 git commit -m",
        "nice -n 10 git commit -m x",
    ):
        result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)
        assert result.returncode == 2, f"{cmd!r} kwam er langs: {result.stdout}"


def test_commit_guard_keeps_a_shell_c_script_in_its_own_directory(tmp_path):
    """Tegenwicht bij N7: `bash -c 'cd elders'` verplaatst de ouder niet."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    wt = repo / ".claude" / "worktrees" / "issue-1-iets"
    _git(repo, "worktree", "add", "-q", "-b", "issue-1-iets", str(wt))

    result = _run(
        COMMIT_GUARD,
        _bash(f"bash -c 'cd {repo} && git status'\ngit commit -m x", wt),
        cwd=wt,
    )

    assert result.returncode == 0, result.stderr


# --- N8: een tilde tussen quotes --------------------------------------------


def test_commit_guard_does_not_expand_a_quoted_tilde(tmp_path):
    """N8: `'~/repo'` is voor bash een relatief pad, geen thuismap.

    Het tegenbeeld (`~/repo` zonder quotes) blijft expanderen — zie
    test_commit_guard_expands_tilde_in_dash_c.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)
    elders = tmp_path / "elders"
    _init_repo(elders, branch="issue-1-iets")

    result = _run(
        COMMIT_GUARD,
        _bash("git -C '~/repo' commit -m \"iets\"", elders),
        cwd=elders,
        env={"HOME": str(tmp_path)},
    )

    assert result.returncode == 0, result.stderr


def test_guard_finds_its_lib_via_git_without_env(tmp_path):
    home = tmp_path / "project"
    init_repo(home)
    guard = install_core(home)
    env = {k: v for k, v in os.environ.items() if k != "LIGHTHOUSE_REPO_ROOT"}
    feature = tmp_path / "feat"
    init_repo(feature, branch="issue-1-x")
    result = subprocess.run(
        [str(guard)],
        input=json.dumps(bash_payload("git commit -m x", feature)),
        capture_output=True,
        text=True,
        cwd=feature,
        env=env,
        check=False,
    )
    assert result.returncode == 0, result.stderr


def test_guard_blocks_when_its_lib_is_missing(tmp_path):
    repo = tmp_path / "repo"
    init_repo(repo)
    result = run_hook(
        COMMIT_GUARD,
        bash_payload("git commit -m x", repo),
        repo,
        env={"LIGHTHOUSE_REPO_ROOT": str(tmp_path / "nowhere")},
    )
    assert result.returncode == 2
    assert "COULD NOT CHECK" in result.stderr


def _trunk_toml(tmp_path: Path) -> dict:
    toml = tmp_path / "lighthouse.toml"
    toml.write_text('[project]\nlanguage = "en"\n\n[forge]\ndefault_branch = "trunk"\n')
    return {"LIGHTHOUSE_TOML": str(toml)}


def test_guard_follows_default_branch(tmp_path):
    env = _trunk_toml(tmp_path)
    trunk = tmp_path / "trunk"
    _init_repo(trunk, branch="trunk")
    on_main = tmp_path / "on-main"
    _init_repo(on_main, branch="main")
    feature = tmp_path / "feature"
    _init_repo(feature, branch="issue-1-x")

    blocked = _run(COMMIT_GUARD, _bash("git commit -m x", trunk), trunk, env)
    assert blocked.returncode == 2 and "'trunk'" in blocked.stderr
    pushed = _run(COMMIT_GUARD, _bash("git push origin HEAD:trunk", feature), feature, env)
    assert pushed.returncode == 2 and "'trunk'" in pushed.stderr

    assert _run(COMMIT_GUARD, _bash("git commit -m x", on_main), on_main, env).returncode == 0
    assert (
        _run(COMMIT_GUARD, _bash("git push origin HEAD:main", feature), feature, env).returncode
        == 0
    )


def test_guard_push_all_looks_for_the_default_branch(tmp_path):
    env = _trunk_toml(tmp_path)
    repo = tmp_path / "repo"
    _init_repo(repo, branch="issue-1-x")
    _git(repo, "branch", "trunk")
    result = _run(COMMIT_GUARD, _bash("git push --all origin", repo), repo, env)
    assert result.returncode == 2


def test_guard_with_broken_config_blocks_a_commit_but_not_a_plain_command(tmp_path):
    toml = tmp_path / "lighthouse.toml"
    toml.write_text("[forge\n")
    env = {"LIGHTHOUSE_TOML": str(toml)}
    repo = tmp_path / "repo"
    _init_repo(repo, branch="issue-1-x")
    commit = _run(COMMIT_GUARD, _bash("git commit -m x", repo), repo, env)
    assert commit.returncode == 2
    assert "COULD NOT CHECK" in commit.stderr and "lighthouse.toml" in commit.stderr
    assert _run(COMMIT_GUARD, _bash("ls -la", repo), repo, env).returncode == 0
