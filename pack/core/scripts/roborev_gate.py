"""Gate of the local roborev review that `write-pr` runs before `gh pr create` (#119).

    roborev_gate.py gate
        Prints one JSON object, exit 0, with a `verdict` the skill acts on:

        off       `[review] roborev` is not `local`: do nothing.
        done      a review of this branch is recorded (`sha`).
        skip      no review, with a `reason` the PR body repeats.
        classify  raw review output exists but is not classified yet (`raw`).
        run       run the review on `range` (`lines` code lines); output goes to `raw`.

        Every verdict except `off` carries `state` and `raw`, the absolute paths of
        `<stem>.json` and `<stem>.md`, so the skill and a restart never build a path.
        Exit 3 with `COULD NOT CHECK: <reason>` on stderr: no git repo or a config error.

Why a gate: a review costs Codex quota and minutes, so a small diff or a nearly empty
quota must skip it, and the same answer must come back after a restart. The skip is
therefore written to the state file at once: a second run repeats the reason instead of
deciding again on a budget that may have moved.

The state lives in `<git-dir>/lighthouse/roborev/`, never in the work tree: the git dir
is per worktree, so two worktrees on one repo do not share a verdict, and nothing can be
committed by accident. The file stem is the branch with `/` turned into `-`.

State json has two forms, both read by `read_state`:
`{"range", "uitkomst": "done", "sha", "bevindingen": [...]}` and
`{"range", "uitkomst": "skip", "reden"}`. The Dutch keys and reasons are literals: the
reasons end up in the PR body.

The size rule is that of paia's `Size` step: added plus removed lines of
`git diff --numstat --no-renames <base> HEAD`, without binary files, without `docs/`,
and without prose or data extensions (except under `.github/workflows/`).
"""

from __future__ import annotations

import json
import pathlib
import shutil
import subprocess
import sys
from collections.abc import Callable

# Run as a file on its path: put `.lighthouse/lib` first, so `lighthouse_lib` imports
# from this installation.
if not __package__:
    sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1] / "lib"))

from lighthouse_lib.config import ConfigError, load, lookup

THRESHOLD = 100
NON_CODE_SUFFIXES = (
    ".md", ".rst", ".txt", ".toml", ".yaml", ".yml", ".json", ".lock", ".csv", ".ini", ".cfg", ".cff",
)  # fmt: skip
CODEX_USAGE = pathlib.Path(__file__).resolve().parent / "codex_usage.py"


class GateError(Exception):
    """The gate could not check: the CLI turns it into `COULD NOT CHECK` and exit 3."""


def _git(root: pathlib.Path, *args: str) -> str:
    try:
        result = subprocess.run(
            ["git", "-C", str(root), *args], capture_output=True, text=True, check=False
        )
    except OSError as exc:
        raise GateError(f"git did not run: {exc}") from exc
    if result.returncode != 0:
        raise GateError(f"git {' '.join(args)}: {result.stderr.strip() or result.returncode}")
    return result.stdout.strip()


def state_dir(root: pathlib.Path) -> pathlib.Path:
    """`<git-dir>/lighthouse/roborev`, absolute and per worktree."""
    return pathlib.Path(_git(root, "rev-parse", "--absolute-git-dir")) / "lighthouse" / "roborev"


def branch_stem(branch: str) -> str:
    return branch.replace("/", "-")


def _counts(path: str) -> bool:
    if path.startswith("docs/"):
        return False
    if path.startswith(".github/workflows/"):
        return True
    return not path.endswith(NON_CODE_SUFFIXES)


def count_code_lines(numstat: str) -> int:
    total = 0
    for line in numstat.splitlines():
        parts = line.split("\t", 2)
        if len(parts) != 3 or parts[0] == "-" or parts[1] == "-":
            continue
        if _counts(parts[2]):
            total += int(parts[0]) + int(parts[1])
    return total


def run_budget() -> tuple[int, str]:
    """Exit code and stdout of `codex_usage.py budget` (exit 6 means `fallback`)."""
    result = subprocess.run(
        [sys.executable, str(CODEX_USAGE), "budget"], capture_output=True, text=True, check=False
    )
    return result.returncode, result.stdout


def read_state(state: pathlib.Path) -> dict | None:
    """The verdict a recorded state file stands for, or None when there is no file."""
    if not state.is_file():
        return None
    try:
        data = json.loads(state.read_text())
    except (OSError, json.JSONDecodeError) as exc:
        raise GateError(f"{state}: unreadable: {exc}") from exc
    if not isinstance(data, dict):
        raise GateError(f"{state}: not a JSON object")
    if data.get("uitkomst") == "done":
        return {"verdict": "done", "sha": data.get("sha")}
    if data.get("uitkomst") == "skip":
        return {"verdict": "skip", "reason": data.get("reden")}
    raise GateError(f"{state}: unknown uitkomst {data.get('uitkomst')!r}")


def write_state(state: pathlib.Path, data: dict) -> None:
    state.parent.mkdir(parents=True, exist_ok=True)
    state.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n")


def _budget_skip(budget: Callable[[], tuple[int, str]]) -> str | None:
    """The skip reason when Codex may not be used, else None."""
    code, out = budget()
    try:
        data = json.loads(out)
        decision, reason = data["decision"], data["reason"]
    except (json.JSONDecodeError, KeyError, TypeError):
        return f"Codex-budget: onleesbaar - exit {code}"
    if code not in (0, 6):
        return f"Codex-budget: onleesbaar - exit {code}"
    if decision == "codex" and code == 0:
        return None
    return f"Codex-budget: {decision} - {reason}"


def gate(root: pathlib.Path, budget: Callable[[], tuple[int, str]] = run_budget) -> dict:
    try:
        cfg = load(root / "lighthouse.toml")
        setting = lookup(cfg, "review.roborev")
        default_branch = str(lookup(cfg, "forge.default_branch"))
    except (ConfigError, KeyError) as exc:
        raise GateError(f"config: {exc}") from exc
    if setting != "local":
        return {"verdict": "off"}

    stem = branch_stem(_git(root, "rev-parse", "--abbrev-ref", "HEAD"))
    sdir = state_dir(root)
    paths = {"state": str(sdir / f"{stem}.json"), "raw": str(sdir / f"{stem}.md")}

    recorded = read_state(sdir / f"{stem}.json")
    if recorded is not None:
        return {**recorded, **paths}
    if (sdir / f"{stem}.md").is_file():
        return {"verdict": "classify", **paths}

    head = _git(root, "rev-parse", "HEAD")
    base = _git(root, "merge-base", f"origin/{default_branch}", "HEAD")
    lines = count_code_lines(_git(root, "diff", "--numstat", "--no-renames", base, "HEAD"))

    reason: str | None = None
    if not (root / ".roborev.toml").is_file():
        reason = "geen .roborev.toml"
    elif not (shutil.which("roborev") and shutil.which("codex")):
        reason = "geen roborev of codex op PATH"
    elif lines < THRESHOLD:
        reason = f"{lines} coderegels (drempel {THRESHOLD})"
    else:
        reason = _budget_skip(budget)

    if reason is not None:
        write_state(sdir / f"{stem}.json", {"range": f"{base}..{head}", "uitkomst": "skip", "reden": reason})
        return {"verdict": "skip", "reason": reason, **paths}
    return {"verdict": "run", "range": f"{base}..{head}", "lines": lines, **paths}


def main(argv: list[str] | None = None) -> int:
    args = sys.argv[1:] if argv is None else argv
    if args != ["gate"]:
        print("usage: roborev_gate.py gate", file=sys.stderr)
        return 2
    try:
        print(json.dumps(gate(pathlib.Path(_git(pathlib.Path.cwd(), "rev-parse", "--show-toplevel")))))
    except GateError as exc:
        print(f"COULD NOT CHECK: {exc}", file=sys.stderr)
        return 3
    return 0


if __name__ == "__main__":
    sys.exit(main())
