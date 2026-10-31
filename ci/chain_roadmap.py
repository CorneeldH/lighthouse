#!/usr/bin/env python3
"""The roadmap layer of the chain map matches the open issues labelled `chain`.

Every planned step or handoff in docs/site/data/chain.json names an open issue with the label
`chain`, and every such issue is on the map. The label marks an issue that adds or changes a
step, a handoff or a route of the chain (see docs/developer/labels.md); explaining the
controller is not one.

`python3 ci/chain_roadmap.py` reads the open issues through `gh api`. Exit 0 is ok, 1 is a
verdict (a mismatch), 2 is COULD NOT CHECK (gh failed or the map does not parse).

Stdlib only, Python 3.11.
"""

import json
import os
import subprocess
import sys
from pathlib import Path

LABEL = "chain"
CHAIN = Path(__file__).resolve().parents[1] / "docs" / "site" / "data" / "chain.json"


def planned(chain: dict) -> dict[str, int | None]:
    """The planned items of the map by name, with the issue each one names (None: no issue)."""
    items = {c["id"]: c for c in chain["chips"]}
    items |= {f"{h['from']}->{h['to']}": h for h in chain["handoffs"]}
    return {name: item.get("issue") for name, item in items.items() if item["status"] != "works"}


def problems(chain: dict, open_issues: set[int]) -> list[str]:
    """Mismatches between the planned items of the map and the open `chain` issues."""
    items = planned(chain)
    found = [f"{name}: planned without an issue" for name, nr in items.items() if nr is None]
    found += [
        f"{name}: #{nr} is not an open issue labelled {LABEL}"
        for name, nr in items.items()
        if nr is not None and nr not in open_issues
    ]
    named = {nr for nr in items.values() if nr is not None}
    found += [f"#{nr}: labelled {LABEL}, but not on the map" for nr in sorted(open_issues - named)]
    return found


def open_chain_issues(repo: str) -> set[int]:
    """The open issues (no PRs) of `repo` with the label; raises CalledProcessError when gh fails."""
    out = subprocess.run(
        ["gh", "api", f"repos/{repo}/issues?labels={LABEL}&state=open&per_page=100", "--paginate",
         "--jq", ".[] | select(.pull_request | not) | .number"],
        check=True, capture_output=True, text=True,
    ).stdout
    return {int(line) for line in out.split()}


def main() -> int:
    repo = os.environ.get("GITHUB_REPOSITORY", "CorneeldH/lighthouse")
    try:
        chain = json.loads(CHAIN.read_text(encoding="utf-8"))
        issues = open_chain_issues(repo)
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f"COULD NOT CHECK: {error}", file=sys.stderr)
        return 2
    found = problems(chain, issues)
    for line in found:
        print(line)
    if not found:
        print(f"chain roadmap: {len(issues)} open {LABEL} issues, all on the map")
    return 1 if found else 0


if __name__ == "__main__":
    sys.exit(main())
