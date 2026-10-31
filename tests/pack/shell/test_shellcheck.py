"""pack/shell: the shell-lint-check hook, `shellcheck-repo.sh` and `shellcheck-bin.sh`, as installed.

The hook is bash and reads its input as JSON on stdin. These tests run it as a
subprocess against a real temporary file, so the contract (exit code + stderr)
is what gets tested.

From paia's `test_quality_hooks.py:518-828`. Left out, because they read paia's own tree:
`test_shellcheck_pin_matches_workflow_audit`, `test_shellcheck_gate_set_is_clean`,
`test_every_hook_script_is_registered_in_settings`, `test_no_gate_shell_is_gitignored`
and the two `gate-assert.sh` tests (`tests/integration/lib/` stays in paia).
"""

import json
import os
import re
import shutil
import subprocess
from pathlib import Path

import pytest

from tests.pack._testkit import (
    GIT_ENV,
    INSTALL_ROOT,
    git,
    init_repo,
    install_pack,
    load_hook_lib,
)
from tests.pack.shell.conftest import SHELL_HOOKS, SHELL_SCRIPTS, SHELLCHECK_LIB

# The gate is the hook in pack/shell; it does not shellcheck itself, it runs
# shellcheck-repo.sh. Both come from the installation (conftest).
SHELLCHECK_HOOK = SHELL_HOOKS / "shell-lint-check.sh"
REPO_SCRIPT = SHELL_SCRIPTS / "shellcheck-repo.sh"

bw = load_hook_lib("bash_writes")

CLEAN_SH = '#!/usr/bin/env bash\nset -euo pipefail\necho "hallo"\n'
# SC2034: `x` is never used. Warning severity, so it blocks everywhere.
BROKEN_SH = '#!/usr/bin/env bash\nf() {\n  local x=1\n  echo "hoi"\n}\nf\n'
# SC2001: style severity only, so it blocks only in the strict directories.
STYLE_ONLY_SH = '#!/usr/bin/env bash\nv="a b"\necho "$v" | sed \'s/a/c/\'\n'


def _run_shellcheck_hook(
    target: Path, cwd: Path | None = None, **env_extra
) -> subprocess.CompletedProcess:
    env = dict(os.environ, **env_extra)
    return subprocess.run(
        [str(SHELLCHECK_HOOK)],
        input=json.dumps({"tool_input": {"file_path": str(target)}}),
        capture_output=True,
        text=True,
        cwd=str(cwd or target.parent),
        env=env,
        check=False,
    )


def _repo_file(tmp_path: Path, rel: str, text: str) -> Path:
    """`text` at `rel` in a fresh git repo under `tmp_path`."""
    repo = tmp_path / "repo"
    if not (repo / ".git").exists():
        init_repo(repo)
    target = repo / rel
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(text, encoding="utf-8")
    return target


def _toml(tmp_path: Path, body: str) -> str:
    path = tmp_path / "lighthouse.toml"
    path.write_text('[project]\nlanguage = "en"\n\n' + body, encoding="utf-8")
    return str(path)


def _first_line(result: subprocess.CompletedProcess) -> str:
    return (result.stderr.splitlines() or [""])[0]


def test_shellcheck_hook_passes_clean_script(tmp_path, pinned_shellcheck):
    target = _repo_file(tmp_path, "clean.sh", CLEAN_SH)
    result = _run_shellcheck_hook(target, SHELLCHECK_BIN=pinned_shellcheck)
    assert result.returncode == 0, result.stderr


def test_shellcheck_hook_blocks_a_deliberately_broken_script(tmp_path, pinned_shellcheck):
    """The proof that the gate bites: red for the right reason, with the SC code,
    and a finding for `bash_writes.classify`."""
    target = _repo_file(tmp_path, "broken.sh", BROKEN_SH)
    result = _run_shellcheck_hook(target, SHELLCHECK_BIN=pinned_shellcheck)
    assert result.returncode == 2, result.stdout
    assert "SC2034" in result.stderr
    assert _first_line(result).startswith("shell-lint-check: fix this in the code")
    assert bw.classify(result.returncode, result.stderr.encode()) == "finding"


def test_shellcheck_hook_ignores_non_shell(tmp_path):
    target = tmp_path / "notes.md"
    target.write_text("# hoi\n")
    assert _run_shellcheck_hook(target).returncode == 0


def test_shellcheck_hook_lets_style_pass_outside_the_strict_dirs(tmp_path, pinned_shellcheck):
    """The blocking set is not the linted set: outside the strict dirs, warning counts."""
    target = _repo_file(tmp_path, "styleonly.sh", STYLE_ONLY_SH)
    assert _run_shellcheck_hook(target, SHELLCHECK_BIN=pinned_shellcheck).returncode == 0


def test_strict_default_covers_lighthouse_hooks(tmp_path, pinned_shellcheck):
    """The default `shell.strict_dirs` is `.claude/hooks`, and a pathspec on it
    reaches `.claude/hooks/lighthouse/` too."""
    target = _repo_file(tmp_path, ".claude/hooks/lighthouse/styleonly.sh", STYLE_ONLY_SH)
    result = _run_shellcheck_hook(target, SHELLCHECK_BIN=pinned_shellcheck)
    assert result.returncode == 2, result.stdout
    assert "SC2001" in result.stderr


def test_strict_dirs_from_config(tmp_path, pinned_shellcheck):
    """`strict_dirs = ["ops"]`: style blocks in `ops/`, and warning is the bar outside it,
    also in `.claude/hooks/`."""
    toml = _toml(tmp_path, '[shell]\nstrict_dirs = ["ops"]\n')
    inside = _repo_file(tmp_path, "ops/styleonly.sh", STYLE_ONLY_SH)
    outside = _repo_file(tmp_path, ".claude/hooks/styleonly.sh", STYLE_ONLY_SH)
    env = {"SHELLCHECK_BIN": pinned_shellcheck, "LIGHTHOUSE_TOML": toml}
    result = _run_shellcheck_hook(inside, **env)
    assert result.returncode == 2, result.stdout
    assert "SC2001" in result.stderr
    assert _run_shellcheck_hook(outside, **env).returncode == 0


def test_strict_dirs_does_not_match_a_prefix(tmp_path, pinned_shellcheck):
    """`ops` is a directory, not a prefix: `opsx/` is outside it."""
    toml = _toml(tmp_path, '[shell]\nstrict_dirs = ["ops"]\n')
    target = _repo_file(tmp_path, "opsx/styleonly.sh", STYLE_ONLY_SH)
    result = _run_shellcheck_hook(target, SHELLCHECK_BIN=pinned_shellcheck, LIGHTHOUSE_TOML=toml)
    assert result.returncode == 0, result.stderr


def test_broken_config_is_unchecked(tmp_path):
    toml = _toml(tmp_path, '[shell]\nstrict_dirs = "ops"\n')
    target = _repo_file(tmp_path, "clean.sh", CLEAN_SH)
    result = _run_shellcheck_hook(target, LIGHTHOUSE_TOML=toml)
    assert result.returncode == 2, result.stdout
    assert _first_line(result).startswith("shell-lint-check: COULD NOT CHECK — "), result.stderr


def test_shellcheck_hook_fails_loud_when_the_tool_is_missing(tmp_path):
    """No tool is not green. An exit 0 would report "clean" about a file nobody looked at."""
    target = _repo_file(tmp_path, "clean.sh", CLEAN_SH)
    result = _run_shellcheck_hook(target, SHELLCHECK_BIN="/nowhere/shellcheck")
    assert result.returncode == 2, result.stdout
    assert _first_line(result).startswith("shell-lint-check: COULD NOT CHECK — "), result.stderr
    assert bw.classify(result.returncode, result.stderr.encode()) == "unchecked"


def test_unchecked_first_line(tmp_path):
    """Only core installed: no `shellcheck-repo.sh` under the lighthouse root."""
    target = _repo_file(tmp_path, "clean.sh", CLEAN_SH)
    result = _run_shellcheck_hook(target, LIGHTHOUSE_REPO_ROOT=str(INSTALL_ROOT))
    assert result.returncode == 2, result.stdout
    assert _first_line(result).startswith("shell-lint-check: COULD NOT CHECK — "), result.stderr
    assert ".lighthouse/scripts/shellcheck-repo.sh" in result.stderr
    assert bw.classify(result.returncode, result.stderr.encode()) == "unchecked"


def test_script_found_in_the_file_root_without_env(tmp_path, pinned_shellcheck):
    """Without `LIGHTHOUSE_REPO_ROOT`, the root is the git root of the edited file:
    an edit in a worktree is checked with that worktree's script."""
    target = _repo_file(tmp_path, "broken.sh", BROKEN_SH)
    install_pack("shell", tmp_path / "repo")
    env = dict(os.environ, SHELLCHECK_BIN=pinned_shellcheck)
    env.pop("LIGHTHOUSE_REPO_ROOT")
    hook = tmp_path / "repo" / ".claude" / "hooks" / "lighthouse" / "shell-lint-check.sh"
    result = subprocess.run(
        [str(hook)],
        input=json.dumps({"tool_input": {"file_path": str(target)}}),
        capture_output=True,
        text=True,
        cwd=str(tmp_path),
        env=env,
        check=False,
    )
    assert result.returncode == 2, result.stderr
    assert "SC2034" in result.stderr


def _shellcheck_pin() -> str:
    match = re.search(r"^SHELLCHECK_PIN=(\S+)", SHELLCHECK_LIB.read_text(), re.MULTILINE)
    assert match, "SHELLCHECK_PIN not found in the lib"
    return match.group(1)


def test_pin_and_hashes_unchanged():
    lib = SHELLCHECK_LIB.read_text()
    assert _shellcheck_pin() == "0.11.0"
    for sha in (
        "8c3be12b05d5c177a04c29e3c78ce89ac86f1595681cab149b65b97c4e227198",
        "12b331c1d2db6b9eb13cfca64306b1b157a86eb69db83023e261eaa7e7c14588",
        "56affdd8de5527894dca6dc3d7e0a99a873b0f004d7aabc30ae407d3f48b0a79",
    ):
        assert sha in lib


def _fake_shellcheck(path: Path, version: str) -> None:
    path.write_text(
        "#!/usr/bin/env bash\n"
        'if [ "${1:-}" = "--version" ]; then\n'
        '  echo "ShellCheck - shell script analysis tool"\n'
        f'  echo "version: {version}"\n'
        "  exit 0\n"
        "fi\n"
        "exit 0\n"
    )
    path.chmod(0o755)


def _failing_curl(stub_dir: Path) -> None:
    curl = stub_dir / "curl"
    curl.write_text("#!/usr/bin/env bash\nexit 1\n")
    curl.chmod(0o755)


def _resolve_shellcheck(repo: Path, stub_dir: Path, fn: str = "resolve_shellcheck") -> subprocess.CompletedProcess:
    """Run `fn` from the lib in `repo`, with `stub_dir` first on PATH."""
    env = dict(os.environ, PATH=f"{stub_dir}{os.pathsep}{os.environ['PATH']}")
    env.pop("SHELLCHECK_BIN", None)
    return subprocess.run(
        ["bash", "-c", f'. "{SHELLCHECK_LIB}"\n{fn}\n'],
        capture_output=True,
        text=True,
        cwd=str(repo),
        env=env,
        check=False,
    )


def _fresh_repo(tmp_path: Path) -> Path:
    repo = tmp_path / "repo"
    repo.mkdir()
    subprocess.run(["git", "init", "-q"], cwd=str(repo), check=True)
    return repo


def test_cache_dir_is_lighthouse_tools(tmp_path):
    """One cache per clone, shared by its worktrees: `<git-common-dir>/lighthouse-tools`."""
    repo = _fresh_repo(tmp_path)
    git(repo, "commit", "-q", "--allow-empty", "-m", "init")
    git(repo, "worktree", "add", "-q", str(tmp_path / "wt"))
    (repo / "sub" / "deep").mkdir(parents=True)
    (tmp_path / "wt" / "sub").mkdir()
    stub_dir = tmp_path / "bin"
    stub_dir.mkdir()
    # From a subdirectory too: there git prints the common dir relative to the cwd.
    for where in (repo, repo / "sub" / "deep", tmp_path / "wt", tmp_path / "wt" / "sub"):
        result = _resolve_shellcheck(where, stub_dir, "_sc_tool_dir")
        assert result.returncode == 0, result.stderr
        assert Path(result.stdout.strip()).resolve() == (repo / ".git" / "lighthouse-tools").resolve()


def test_resolve_prefers_the_pinned_cache_over_a_divergent_path_version(tmp_path):
    """The strict set is clean UNDER THE PIN. ubuntu-latest has shellcheck 0.9.0
    preinstalled, and that reports SC2317 on exactly the trap cleanup 0.11.0
    suppresses via `disable=SC2329`. A divergent PATH version must never push the
    pin aside: the same tree would be red on one runner and green on the other.
    """
    repo = _fresh_repo(tmp_path)
    cache = repo / ".git" / "lighthouse-tools"
    cache.mkdir(parents=True)
    _fake_shellcheck(cache / "shellcheck", _shellcheck_pin())
    stub_dir = tmp_path / "bin"
    stub_dir.mkdir()
    _fake_shellcheck(stub_dir / "shellcheck", "0.9.0")

    result = _resolve_shellcheck(repo, stub_dir)
    assert result.returncode == 0, result.stderr
    assert result.stdout.strip() == str(cache / "shellcheck"), result.stdout


def test_resolve_uses_a_pinned_path_version_without_downloading(tmp_path):
    """When the pin itself is on PATH, that is the answer: no download."""
    repo = _fresh_repo(tmp_path)
    stub_dir = tmp_path / "bin"
    stub_dir.mkdir()
    _fake_shellcheck(stub_dir / "shellcheck", _shellcheck_pin())

    result = _resolve_shellcheck(repo, stub_dir)
    assert result.returncode == 0, result.stderr
    assert result.stdout.strip() == str(stub_dir / "shellcheck"), result.stdout
    assert result.stderr == "", result.stderr


def test_resolve_falls_back_to_a_divergent_path_version_when_the_pin_is_unreachable(tmp_path):
    """Offline with only an old shellcheck: looking with the wrong version beats not
    looking, but with a notice, because the outcome is not the gate's. `curl` is a
    stub that fails: unit tests do not go on the network.
    """
    repo = _fresh_repo(tmp_path)
    stub_dir = tmp_path / "bin"
    stub_dir.mkdir()
    _fake_shellcheck(stub_dir / "shellcheck", "0.9.0")
    _failing_curl(stub_dir)

    result = _resolve_shellcheck(repo, stub_dir)
    assert result.returncode == 0, result.stderr
    assert result.stdout.strip() == str(stub_dir / "shellcheck"), result.stdout
    assert "differs from the pin" in result.stderr, result.stderr


def test_divergent_version_notice_reaches_the_session(tmp_path):
    """A clean result from a divergent version is no silent pass: the hook passes the
    notice on as a `systemMessage`."""
    target = _repo_file(tmp_path, "clean.sh", CLEAN_SH)
    stub_dir = tmp_path / "bin"
    stub_dir.mkdir()
    _fake_shellcheck(stub_dir / "shellcheck", "0.9.0")
    _failing_curl(stub_dir)
    path = f"{stub_dir}{os.pathsep}{os.environ['PATH']}"
    result = _run_shellcheck_hook(target, PATH=path)
    assert result.returncode == 0, result.stderr
    message = json.loads(result.stdout)["systemMessage"]
    assert message.startswith("shell-lint-check: ")
    assert "differs from the pin" in message


def test_shellcheck_hook_reads_the_path_without_jq(tmp_path, pinned_shellcheck):
    """The hook runs on EVERY Edit/Write. Without jq it must not block the whole
    session on a .py or .md edit; python3 is the second parser.

    Removing jq cannot be done by cutting its directory from PATH: bash lives there
    too. So a PATH of symlinks only, with everything except jq.
    """
    target = _repo_file(tmp_path, "broken.sh", BROKEN_SH)
    shim = tmp_path / "bin"
    shim.mkdir()
    needed = ["env", "bash", "python3", "git", "dirname", "sed", "grep", "cut", "uname", "cat", "mktemp", "rm"]
    for name in needed:
        found = shutil.which(name)
        assert found, name
        (shim / name).symlink_to(found)
    assert shutil.which("jq", path=str(shim)) is None

    result = _run_shellcheck_hook(target, PATH=str(shim), SHELLCHECK_BIN=pinned_shellcheck)
    assert result.returncode == 2, result.stdout + result.stderr
    assert "SC2034" in result.stderr


# --- shellcheck-repo.sh -------------------------------------------------------


def _run_repo_script(repo: Path, *args: str, **env_extra) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["bash", str(REPO_SCRIPT), *args],
        capture_output=True,
        text=True,
        cwd=str(repo),
        env=dict(os.environ, **GIT_ENV, **env_extra),
        check=False,
    )


def test_repo_script_strict_set_from_config(tmp_path, pinned_shellcheck):
    """Without files, the script checks `shell.strict_dirs`, at style, with new files too."""
    toml = _toml(tmp_path, '[shell]\nstrict_dirs = ["ops"]\n')
    _repo_file(tmp_path, "ops/clean.sh", CLEAN_SH)
    _repo_file(tmp_path, "elsewhere/styleonly.sh", STYLE_ONLY_SH)
    repo = tmp_path / "repo"
    env = {"SHELLCHECK_BIN": pinned_shellcheck, "LIGHTHOUSE_TOML": toml}
    assert _run_repo_script(repo, **env).returncode == 0
    _repo_file(tmp_path, "ops/sub/styleonly.sh", STYLE_ONLY_SH)
    result = _run_repo_script(repo, **env)
    assert result.returncode == 1, result.stderr
    assert "SC2001" in result.stdout


def test_repo_script_empty_strict_set_is_not_green(tmp_path, pinned_shellcheck):
    toml = _toml(tmp_path, "[shell]\nstrict_dirs = []\n")
    _repo_file(tmp_path, "ops/clean.sh", CLEAN_SH)
    result = _run_repo_script(tmp_path / "repo", SHELLCHECK_BIN=pinned_shellcheck, LIGHTHOUSE_TOML=toml)
    assert result.returncode == 2
    assert "this is not green" in result.stderr


def test_repo_script_unchecked_is_rc_4(tmp_path):
    target = _repo_file(tmp_path, "clean.sh", CLEAN_SH)
    result = _run_repo_script(tmp_path / "repo", "--", str(target), SHELLCHECK_BIN="/nowhere/shellcheck")
    assert result.returncode == 4
    assert "is not executable" in result.stderr


# --- through bash-write-check.sh ---------------------------------------------


def test_bash_write_through_gate(tmp_path, pinned_shellcheck):
    """With core and shell installed, a Bash write of a broken `.sh` is a finding via
    `bash-write-check post`, not `no shell gate installed`."""
    from tests.pack.test_bash_write_check import BROKEN_SH as GATES_BROKEN_SH
    from tests.pack.test_bash_write_check import _Session, heredoc

    session = _Session(tmp_path, shellcheck=pinned_shellcheck, language_gates=False)
    install_pack("shell", session.root)
    out = session.call(heredoc("x.sh", GATES_BROKEN_SH))
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr
    assert "no shell gate installed" not in out.stderr + out.stdout


# --- a shellcheck that fails is no finding ----------------------------------


def _crashing_shellcheck(tmp_path: Path, rc: int) -> str:
    stub = tmp_path / f"shellcheck-rc{rc}"
    stub.write_text(f"#!/usr/bin/env bash\necho boom >&2\nexit {rc}\n")
    stub.chmod(0o755)
    return str(stub)


@pytest.mark.parametrize("rc", [2, 3, 4, 126])
def test_repo_script_shellcheck_failure_is_rc_4(tmp_path, rc):
    """shellcheck's own rc 2-4 (a file it could not read, bad options) or a crash is
    "not checked", not "findings"."""
    target = _repo_file(tmp_path, "clean.sh", CLEAN_SH)
    result = _run_repo_script(tmp_path / "repo", "--", str(target), SHELLCHECK_BIN=_crashing_shellcheck(tmp_path, rc))
    assert result.returncode == 4, result.stderr
    assert f"shellcheck exited with rc={rc}" in result.stderr
    assert "boom" in result.stderr


def test_hook_shellcheck_failure_is_unchecked(tmp_path):
    target = _repo_file(tmp_path, "clean.sh", CLEAN_SH)
    result = _run_shellcheck_hook(target, SHELLCHECK_BIN=_crashing_shellcheck(tmp_path, 3))
    assert result.returncode == 2, result.stdout
    assert _first_line(result).startswith("shell-lint-check: COULD NOT CHECK — "), result.stderr
    assert bw.classify(result.returncode, result.stderr.encode()) == "unchecked"
