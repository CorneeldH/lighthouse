#!/usr/bin/env python3
"""The roadmap layer of the loop map matches the issues labelled `loop`.

The label marks an issue that adds or changes a step, a handoff or a route of the loop (see
docs/developer/labels.md); explaining the controller is not one. Two modes:

`python3 ci/loop_roadmap.py` (main mode) checks that every planned step or handoff in
docs/site/data/loop.json names an open issue with the label. An open issue missing from the map
is fine: the PR that delivers it adds it.

`python3 ci/loop_roadmap.py --pr <n>` (PR mode) checks that a PR closing a `loop` issue also
updates the map: no closed issue is still planned on it, and the map file is touched. A PR that
touches the map or this script also gets the main mode check, so a planned item naming a wrong or
closed issue, or a broken script, is red in the PR and not only later on `main` (the anti-goal of
the spec). That takes precedence over "a PR that closes no loop issue is green without further
check", which is about the closing-issue check only.

Both read GitHub through `gh`. Exit 0 is ok, 1 is a verdict, 2 is COULD NOT CHECK (gh failed or
the map does not parse).

Stdlib only, Python 3.11.
"""

import argparse
import json
import os
import subprocess
import sys
from pathlib import Path

LABEL = "loop"
LOOP = Path(__file__).resolve().parents[1] / "docs" / "site" / "data" / "loop.json"


def planned(chain: dict) -> dict[str, int | None]:
    """The planned items of the map by name, with the issue each one names (None: no issue)."""
    items = {c["id"]: c for c in chain["chips"]}
    items |= {f"{h['from']}->{h['to']}": h for h in chain["handoffs"]}
    return {name: item.get("issue") for name, item in items.items() if item["status"] != "works"}


HINT = "(see docs/developer/labels.md)"
MAP = "docs/site/data/loop.json"
SCRIPT = "ci/loop_roadmap.py"


def problems(chain: dict, open_issues: set[int]) -> list[str]:
    """Planned items of the map without an open `loop` issue."""
    items = planned(chain)
    found = [f"{name}: planned without an issue" for name, nr in items.items() if nr is None]
    found += [
        f"{name}: #{nr} is not an open issue labelled {LABEL} {HINT}"
        for name, nr in items.items()
        if nr is not None and nr not in open_issues
    ]
    return found


def pr_problems(chain: dict, closed: set[int], touched: set[str]) -> list[str]:
    """What a PR closing the `loop` issues `closed` leaves undone on the map."""
    found = [
        f"{name}: still planned, but this PR closes #{nr}; set status works and drop issue {HINT}"
        for name, nr in planned(chain).items()
        if nr in closed
    ]
    if MAP not in touched:
        reported = {nr for nr in planned(chain).values() if nr in closed}
        found += [
            f"#{nr}: this PR closes a loop issue but leaves {MAP} untouched {HINT}"
            for nr in sorted(closed - reported)
        ]
    return found


def open_loop_issues(repo: str) -> set[int]:
    """The open issues (no PRs) of `repo` with the label; raises CalledProcessError when gh fails."""
    out = subprocess.run(
        ["gh", "api", f"repos/{repo}/issues?labels={LABEL}&state=open&per_page=100", "--paginate",
         "--jq", ".[] | select(.pull_request | not) | .number"],
        check=True, capture_output=True, text=True,
    ).stdout
    return {int(line) for line in out.split()}


def closed_loop_issues(repo: str, pr: int) -> set[int]:
    """The `loop` issues PR `pr` closes; raises CalledProcessError when gh fails."""
    owner, name = repo.split("/", 1)
    query = (
        "query($owner: String!, $name: String!, $pr: Int!) { repository(owner: $owner, name: $name) "
        "{ pullRequest(number: $pr) { closingIssuesReferences(first: 100) "
        "{ nodes { number repository { nameWithOwner } labels(first: 50) { nodes { name } } } } } } }"
    )
    out = subprocess.run(
        ["gh", "api", "graphql", "-f", f"query={query}", "-f", f"owner={owner}", "-f", f"name={name}",
         "-F", f"pr={pr}"],
        check=True, capture_output=True, text=True,
    ).stdout
    try:
        nodes = json.loads(out)["data"]["repository"]["pullRequest"]["closingIssuesReferences"]["nodes"]
    except (KeyError, TypeError) as error:
        raise ValueError(f"unexpected gh graphql reply for PR #{pr}: {error!r}") from error
    return {
        n["number"] for n in nodes
        if n["repository"]["nameWithOwner"].lower() == repo.lower()
        and LABEL in {lab["name"] for lab in n["labels"]["nodes"]}
    }


def touched_files(repo: str, pr: int) -> set[str]:
    """The paths PR `pr` changes; raises CalledProcessError when gh fails."""
    out = subprocess.run(
        ["gh", "api", f"repos/{repo}/pulls/{pr}/files", "--paginate", "--jq", ".[].filename"],
        check=True, capture_output=True, text=True,
    ).stdout
    return {line for line in out.splitlines() if line}


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--pr", type=int, help="check this PR instead of the planned items")
    pr = parser.parse_args(argv).pr
    repo = os.environ.get("GITHUB_REPOSITORY", "CorneeldH/lighthouse")
    try:
        chain = json.loads(LOOP.read_text(encoding="utf-8"))
        if pr is None:
            issues = open_loop_issues(repo)
            found = problems(chain, issues)
            ok = f"loop roadmap: {len(issues)} open {LABEL} issues, planned items all open"
        else:
            closed = closed_loop_issues(repo, pr)
            touched = touched_files(repo, pr)
            found = pr_problems(chain, closed, touched) if closed else []
            if MAP in touched or SCRIPT in touched:
                found += problems(chain, open_loop_issues(repo))
            ok = f"loop roadmap: PR #{pr} closes {len(closed)} loop issues, map in step"
            if not closed:
                ok = f"loop roadmap: PR #{pr} closes no loop issue"
    except (OSError, ValueError, KeyError, subprocess.CalledProcessError) as error:
        print(f"COULD NOT CHECK: {error}", file=sys.stderr)
        return 2
    for line in found:
        print(line)
    if not found:
        print(ok)
    return 1 if found else 0


if __name__ == "__main__":
    sys.exit(main())
