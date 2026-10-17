# Bootstrap copy of personal_ai_agent@231f6e84 tests/unit/_hook_testkit.py; M0 (b) replaces it.
"""Gedeelde helpers voor de hook-tests (taak 1 t/m 5 van #884/#886).

Overgenomen uit `test_instructie_hooks.py` (de private `_run`, `_git`,
`_init_repo`, `_bash`, `_tool_bin` van vóór deze taak) zodat
`test_git_invocations.py` en latere taken (`test_pre_commit_gate.py`,
`test_md_links.py`, `test_hook_deadline.py`) dezelfde plumbing gebruiken in
plaats van hem te herimplementeren. `test_instructie_hooks.py` zelf blijft
ongewijzigd — dit bestand refactort geen bestaande tests, het geeft nieuwe
tests dezelfde bouwstenen.
"""

import importlib.util
import json
import os
import shutil
import subprocess
import time
import types
from pathlib import Path

HOOKS = Path(__file__).resolve().parents[2] / ".claude" / "hooks"

GIT_ENV = {
    "GIT_AUTHOR_NAME": "t",
    "GIT_AUTHOR_EMAIL": "t@example.com",
    "GIT_COMMITTER_NAME": "t",
    "GIT_COMMITTER_EMAIL": "t@example.com",
}


def run_hook(
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


def git(repo: Path, *args: str) -> None:
    subprocess.run(
        ["git", "-C", str(repo), *args],
        check=True,
        capture_output=True,
        text=True,
        env=dict(os.environ, **GIT_ENV),
    )


def init_repo(path: Path, branch: str = "main") -> None:
    """Echte git-repo met één commit, zodat de hook echte plumbing ziet."""
    path.mkdir(parents=True, exist_ok=True)
    git(path, "init", "-q", "-b", branch)
    (path / "README.md").write_text("# repo\n", encoding="utf-8")
    git(path, "add", "README.md")
    git(path, "commit", "-qm", "init")


def past_this_second() -> None:
    """Wacht tot de volgende hele seconde.

    Bron 2 van bash-write-gates wandelt ook de reflog sinds de start van pre,
    naar beneden afgerond op hele seconden: een setup-commit (bv. die van
    `init_repo`) uit dezelfde seconde telt dan mee (bewust overcontroleren).
    Een test die wil dat een setup-commit buiten de call valt, wacht eerst de
    seconde uit.
    """
    time.sleep(1.02 - time.time() % 1.0)


def bash_payload(cmd: str, cwd: Path) -> dict:
    return {"tool_name": "Bash", "cwd": str(cwd), "tool_input": {"command": cmd}}


def tool_bin(
    path: Path,
    tools: tuple[str, ...],
    *,
    drop: str = "",
    stubs: dict[str, str] | None = None,
) -> Path:
    """Een PATH-map met de echte tooling, op één na, plus eventuele stubs.

    Zo is per limb van de preflight te toetsen dat juist díé limb luid faalt —
    een lege PATH dekt ze alle drie tegelijk en dus geen enkele apart (M11).
    """
    path.mkdir(parents=True, exist_ok=True)
    for tool in tools:
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


def load_hook_lib(name: str) -> types.ModuleType:
    """Laad `.claude/hooks/lib/<name>.py` als module, zonder hem uit te voeren.

    `importlib.util.spec_from_file_location` geeft de module een eigen naam
    (niet `__main__`), dus een `if __name__ == "__main__":`-blok in het
    geladen bestand draait niet mee — de module leest bij het importeren dus
    geen stdin en heeft geen argv nodig.
    """
    path = HOOKS / "lib" / f"{name}.py"
    spec = importlib.util.spec_from_file_location(name, path)
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module
