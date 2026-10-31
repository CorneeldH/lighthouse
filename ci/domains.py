#!/usr/bin/env python3
"""Sort changed paths into the domains web, python and go, for the jobs of CI.

CI runs `python3 ci/domains.py classify --github-output "$GITHUB_OUTPUT"` with the
NUL-separated paths of the change on stdin, and gets `web=`, `python=` and `go=` as job
outputs.

Stdlib only, Python 3.11.
"""

import argparse
import sys
from collections.abc import Iterable

DOMAINS = ("web", "python", "go")

# Outside web/, but a change here can break the web checks: the CI job, this script, and the
# gate command in lighthouse.toml.
_WEB_TOO = frozenset({".github/workflows/ci.yml", "ci/domains.py", "lighthouse.toml"})
# Inside web/, but read by Go: the API snapshot test and the module that keeps `./...` out of
# web/node_modules.
_GO_TOO = frozenset({"web/openapi.json", "web/go.mod"})
# Outside web/, but read by no Go build or test, so they leave out go (python stays: pytest reads
# docs/, and ci/literal-grep.sh runs in that job). Every other unknown path still touches go.
# Measured on main (c72662c, #130): go:embed reads only internal/web (dist, placeholder),
# pack/*/* and internal/store/migrations; no .go file names docs/, changelog.d or ci/literal-
# (tests/test_domains.py fails when one does); the Go and e2e tests read outside their own
# package only tests/pack/fixtures/, internal/usage/testdata/ and temporary repos; the literal
# lists are read only by ci/literal-grep.sh and tools/gen_code_language_freq.py.
_NOT_GO_DIRS = ("docs/", "changelog.d/")
_NOT_GO = frozenset({"ci/literal-denylist.txt", "ci/literal-allowlist.txt"})


def classify(paths: Iterable[str]) -> set[str]:
    """Return the domains the paths touch."""
    touched: set[str] = set()
    for path in paths:
        in_web = path.startswith("web/")
        not_go = path.startswith(_NOT_GO_DIRS) or path in _NOT_GO
        if in_web or path in _WEB_TOO:
            touched.add("web")
        if (not in_web and not not_go) or path in _GO_TOO:
            touched.add("go")
        if not in_web:
            touched.add("python")
    return touched


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
    classify_parser = sub.add_parser(
        "classify", help="NUL-separated paths on stdin; print web=, python= and go="
    )
    classify_parser.add_argument("--github-output", metavar="FILE")
    args = parser.parse_args(argv)
    return _classify_cli(args.github_output)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
