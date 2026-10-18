#!/usr/bin/env python3
"""Zet een opgepakt issue van board-Status `To do` of `On hold` op `In progress` (#1007).

Aangeroepen door `paia-start-issue` direct na de worktree-stap. Wie een geparkeerd issue
bewust oppakt, heft de parkering op. `Controller` blijft staan omdat de controller daarop
selecteert en zelf `paia-start-issue` draait; `In progress` en `Done` blijven ook staan.

Faalt nooit hard: altijd exit 0 en precies één regel die met `board: ` begint, zodat
het oppakken van een issue er niet op blokkeert.

Modus `--heropen <nr>` (#1091): zet een heropend issue van `Done` terug op `In progress`.
Hier is het omgekeerde bedoeld: fail-closed. Een andere startstatus, een issue buiten het
board, een uitzondering of een teruggelezen status ≠ `In progress` geeft één regel
`board: FOUT — <oorzaak>` en exit 1; zo meldt `paia-close-issue` het board nooit stil als goed.

USAGE
    board_start.py <nr>
    board_start.py --heropen <nr>
"""

from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))

from src.controller.identity import IdentityError, identity

# Owner en naam gaan als GraphQL-variabelen mee: de query kent de repo niet.
QUERY = """
query($owner: String!, $name: String!, $nummer: Int!) {
  repository(owner: $owner, name: $name) {
    issue(number: $nummer) {
      projectItems(first: 20) {
        nodes {
          id
          project { id }
          fieldValueByName(name: "Status") {
            ... on ProjectV2ItemFieldSingleSelectValue { name }
          }
        }
      }
    }
  }
}
"""
OPPAKBAAR = ("To do", "On hold")
# Wat `gh` en de GraphQL-parsing kunnen gooien; gedeeld door het standaardpad en --heropen.
GH_FOUTEN = (
    IdentityError,
    subprocess.CalledProcessError,
    subprocess.TimeoutExpired,
    OSError,
    KeyError,
    TypeError,
    ValueError,
)


def foutnaam(fout: Exception) -> str:
    """Typenaam, bij een `IdentityError` ook de melding (pad en sleutel), op één regel."""
    if isinstance(fout, IdentityError):
        return f"{type(fout).__name__}: {' '.join(str(fout).split())}"
    return type(fout).__name__


def gh(*args: str) -> str:
    return subprocess.run(
        ["gh", *args],
        capture_output=True,
        text=True,
        timeout=60,
        check=True,
    ).stdout


def lees_item(nummer: int) -> tuple[str, str | None] | None:
    """`(item_id, status)` van het issue op het board, of `None` als het er niet op staat."""
    repo = identity()
    data = json.loads(
        gh(
            "api",
            "graphql",
            "-f",
            f"query={QUERY}",
            "-f",
            f"owner={repo.owner}",
            "-f",
            f"name={repo.name}",
            "-F",
            f"nummer={nummer}",
        )
    )
    nodes = data["data"]["repository"]["issue"]["projectItems"]["nodes"]
    for node in nodes:
        if node["project"]["id"] == repo.board.project_id:
            waarde = node.get("fieldValueByName") or {}
            return str(node["id"]), waarde.get("name")
    return None


def zet_in_progress(item_id: str) -> None:
    board = identity().board
    gh(
        "project",
        "item-edit",
        "--id",
        item_id,
        "--project-id",
        board.project_id,
        "--field-id",
        board.status_field_id,
        "--single-select-option-id",
        board.status_options["In progress"],
    )


def heropen(nummer: int) -> int:
    """Zet `Done` op `In progress` en leest terug; elke afwijking is exit 1 (fail-closed)."""
    try:
        item = lees_item(nummer)
        if item is None:
            print(f"board: FOUT — #{nummer} staat niet op het board")
            return 1
        item_id, status = item
        if status == "In progress":
            print(f"board: #{nummer} staat al op In progress")
            return 0
        if status != "Done":
            print(
                f"board: FOUT — #{nummer} staat "
                f"{'op ' + status if status else 'zonder status'}, niet op Done"
            )
            return 1
        zet_in_progress(item_id)
        teruggelezen = lees_item(nummer)
    except GH_FOUTEN as fout:
        print(f"board: FOUT — {foutnaam(fout)}")
        return 1
    na = teruggelezen[1] if teruggelezen else None
    if na != "In progress":
        print(f"board: FOUT — #{nummer} staat na de edit op {na or 'geen status'}")
        return 1
    print(f"board: #{nummer} Done → In progress")
    return 0


def main(argv: list[str]) -> int:
    if argv[:1] == ["--heropen"]:
        if len(argv) != 2 or not argv[1].isdigit():
            print("board: gebruik: board_start.py --heropen <nr>")
            return 1
        return heropen(int(argv[1]))
    if len(argv) != 1 or not argv[0].isdigit():
        print("board: gebruik: board_start.py <nr>")
        return 0
    nummer = int(argv[0])
    try:
        item = lees_item(nummer)
        if item is None:
            print(f"board: #{nummer} staat niet op het board")
            return 0
        item_id, status = item
        if status not in OPPAKBAAR:
            print(
                f"board: #{nummer} blijft {'op ' + status if status else 'zonder status'}"
            )
            return 0
        zet_in_progress(item_id)
    except GH_FOUTEN as fout:
        print(f"board: overgeslagen ({foutnaam(fout)})")
        return 0
    print(f"board: #{nummer} {status} → In progress")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
