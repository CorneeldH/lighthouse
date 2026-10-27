#!/usr/bin/env python3
"""Sort changed paths into domains (web, python, go) and run only the checks they touch.

Two users share the rules in `classify`:

- the pre-commit test gate (`[test_gate] command` in lighthouse.toml) runs
  `python3 ci/domains.py gate` in the root of the tree being committed. It reads the changed
  paths from git (staged, unstaged and untracked), skips every domain no path touches, and runs
  the checks of the others in the order of `DOMAINS`. Exit 3 means COULD NOT CHECK (no node,
  no web/node_modules, a command not found); lighthouse.toml lists it in
  `unchecked_exit_codes`.
- CI runs `python3 ci/domains.py classify --github-output "$GITHUB_OUTPUT"` with the
  NUL-separated paths of the change on stdin, and gets `web=`, `python=` and `go=` as job
  outputs.

Stdlib only, Python 3.11.
"""

import argparse
import shutil
import subprocess
import sys
from collections.abc import Callable, Iterable
from pathlib import Path

DOMAINS = ("web", "python", "go")

# Outside web/, but a change here can break the web checks: the CI job, this script, and the
# gate command in lighthouse.toml.
_WEB_TOO = frozenset({".github/workflows/ci.yml", "ci/domains.py", "lighthouse.toml"})
# Inside web/, but read by Go: the API snapshot test and the module that keeps `./...` out of
# web/node_modules.
_GO_TOO = frozenset({"web/openapi.json", "web/go.mod"})

CHECKS: dict[str, list[list[str]]] = {
    "web": [
        ["npm", "run", "--silent", "lint"],
        ["npm", "run", "--silent", "typecheck"],
        ["npm", "test"],
    ],
    "python": [["uv", "run", "pytest", "-q"]],
    "go": [["go", "test", "-race", "-shuffle=on", "./..."]],
}

UNCHECKED = 3


def _say(line: str) -> None:
    print(f"gate: {line}", flush=True)


def classify(paths: Iterable[str]) -> set[str]:
    """Return the domains the paths touch."""
    touched: set[str] = set()
    for path in paths:
        in_web = path.startswith("web/")
        if in_web or path in _WEB_TOO:
            touched.add("web")
        if not in_web or path in _GO_TOO:
            touched.add("go")
        if not in_web:
            touched.add("python")
    return touched


def _snapshot(directory: Path) -> dict[str, bytes]:
    if not directory.is_dir():
        return {}
    return {
        path.relative_to(directory).as_posix(): path.read_bytes()
        for path in sorted(directory.rglob("*"))
        if path.is_file()
    }


def api_drift(web: Path, gen: list[str] = ["npm", "run", "--silent", "gen"]) -> int:  # noqa: B006
    """Regenerate the API client in web/src/api; 1 when that changed the worktree.

    It compares with the worktree, not with the index: a regenerated client that is not staged
    yet counts as up to date.
    """
    api = web / "src" / "api"
    before = _snapshot(api)
    status = subprocess.run(gen, cwd=web, check=False).returncode
    if status != 0:
        _say("npm run gen failed")
        return status
    if _snapshot(api) != before:
        _say("web/src/api was stale; npm run gen regenerated it — stage the result")
        return 1
    return 0


def _git_paths(root: Path, *args: str) -> list[str]:
    out = subprocess.run(
        ["git", *args], cwd=root, check=True, capture_output=True
    ).stdout
    return [p.decode("utf-8", "surrogateescape") for p in out.split(b"\0") if p]


def changed_paths(root: Path) -> list[str]:
    """Staged, unstaged and untracked paths in root; a rename gives both sides."""
    paths: set[str] = set()
    paths.update(_git_paths(root, "diff", "--cached", "--name-only", "--no-renames", "-z"))
    paths.update(_git_paths(root, "diff", "--name-only", "--no-renames", "-z"))
    paths.update(_git_paths(root, "ls-files", "--others", "--exclude-standard", "-z"))
    return sorted(paths)


def _web_missing(root: Path) -> str | None:
    for tool in ("node", "npm"):
        if shutil.which(tool) is None:
            return f"{tool} not on PATH"
    if not (root / "web" / "node_modules").is_dir():
        return "web/node_modules missing"
    return None


def gate(
    root: Path,
    checks: dict[str, list[list[str]]] = CHECKS,
    drift: Callable[[Path], int] = api_drift,
) -> int:
    """Run the checks of every domain the changed paths touch; stop at the first failure."""
    paths = changed_paths(root)
    if not paths:
        _say("no changed paths — nothing to check")
        return 0
    touched = [d for d in DOMAINS if d in classify(paths)]
    for domain in DOMAINS:
        if domain not in touched:
            _say(f"{domain} skipped — no path in the domain")

    if "web" in touched:
        reason = _web_missing(root)
        if reason is not None:
            _say(f"COULD NOT CHECK — {reason}; fix: cd web && npm ci")
            return UNCHECKED

    web = root / "web"
    for domain in touched:
        cwd = web if domain == "web" else root
        try:
            if domain == "web":
                _say("web — api drift (npm run --silent gen)")
                status = drift(web)
                if status != 0:
                    return status
            for command in checks[domain]:
                _say(f"{domain} — {' '.join(command)}")
                status = subprocess.run(command, cwd=cwd, check=False).returncode
                if status != 0:
                    return status
        except FileNotFoundError as exc:
            _say(f"COULD NOT CHECK — {exc.filename} not found")
            return UNCHECKED

    _say(f"passed — {', '.join(touched)}")
    return 0


def _classify_cli(github_output: str | None) -> int:
    raw = sys.stdin.buffer.read()
    paths = [p.decode("utf-8", "surrogateescape") for p in raw.split(b"\0") if p]
    touched = classify(paths)
    lines = [f"{d}={'true' if d in touched else 'false'}" for d in DOMAINS]
    for line in lines:
        print(line)
    if github_output:
        with open(github_output, "a", encoding="utf-8") as out:
            out.write("".join(f"{line}\n" for line in lines))
    return 0


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(prog="ci/domains.py", description=__doc__.splitlines()[0])
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("gate", help="run the checks of the touched domains in the current directory")
    classify_parser = sub.add_parser(
        "classify", help="NUL-separated paths on stdin; print web=, python= and go="
    )
    classify_parser.add_argument("--github-output", metavar="FILE")
    args = parser.parse_args(argv)
    if args.command == "gate":
        return gate(Path.cwd())
    return _classify_cli(args.github_output)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
