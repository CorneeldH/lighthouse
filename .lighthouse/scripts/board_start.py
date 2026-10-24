#!/usr/bin/env python3
# lighthouse: core@dev
"""Move a picked-up issue from board status `To do` or `On hold` to `In progress`.

Aangeroepen door `start-issue` direct na de worktree-stap. Wie een geparkeerd issue
bewust oppakt, heft de parkering op. `Controller` blijft staan omdat de controller daarop
selecteert en zelf `start-issue` draait; `In progress` en `Done` blijven ook staan.

Faalt nooit hard: altijd exit 0 en precies één regel die met `board: ` begint, zodat
het oppakken van een issue er niet op blokkeert.

Modus `--reopen <nr>`: zet een heropend issue van `Done` terug op `In progress`.
Hier is het omgekeerde bedoeld: fail-closed. Een andere startstatus, een issue buiten het
board, een uitzondering of een teruggelezen status ≠ `In progress` geeft één regel
`board: ERROR — <oorzaak>` en exit 1; zo meldt `close-issue` het board nooit stil als goed.

Zonder `[forge.board]` geven beide modi `board: skipped — no [forge.board] configured` en
exit 0 (besluit 16, 24). Een kapotte config geeft in beide modi `board: ERROR — <oorzaak>`
en exit 2.

USAGE
    board_start.py <nr>
    board_start.py --reopen <nr>
"""

from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

# Run as a file on its path: put `.lighthouse/lib` first, so `lighthouse_lib` imports
# from this installation.
if not __package__:
    sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))

from lighthouse_lib.config import Config, ConfigError, config_path, load

# Owner en naam gaan als GraphQL-variabelen mee: de query kent de repo niet.
QUERY = """
query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    issue(number: $number) {
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
OVERGESLAGEN = "board: skipped — no [forge.board] configured"
# Wat `gh` en de GraphQL-parsing kunnen gooien; gedeeld door het standaardpad en --reopen.
GH_FOUTEN = (
    ConfigError,
    subprocess.CalledProcessError,
    subprocess.TimeoutExpired,
    OSError,
    KeyError,
    TypeError,
    ValueError,
)


def foutnaam(fout: Exception) -> str:
    """Typenaam, bij een `ConfigError` ook de melding (pad en sleutel), op één regel."""
    if isinstance(fout, ConfigError):
        return f"{type(fout).__name__}: {' '.join(str(fout).split())}"
    return type(fout).__name__


def identity() -> Config:
    """De config van de repo; een board zonder `forge.repo` is een configfout."""
    cfg = load(config_path())
    if cfg.board is not None and cfg.repo is None:
        raise ConfigError(f"{config_path()}: [forge.board] needs forge.repo")
    return cfg


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
            f"number={nummer}",
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
            print(f"board: ERROR — #{nummer} is not on the board")
            return 1
        item_id, status = item
        if status == "In progress":
            print(f"board: #{nummer} is already In progress")
            return 0
        if status != "Done":
            print(
                f"board: ERROR — #{nummer} is "
                f"{'at ' + status if status else 'without status'}, not at Done"
            )
            return 1
        zet_in_progress(item_id)
        teruggelezen = lees_item(nummer)
    except GH_FOUTEN as fout:
        print(f"board: ERROR — {foutnaam(fout)}")
        return 1
    na = teruggelezen[1] if teruggelezen else None
    if na != "In progress":
        print(f"board: ERROR — #{nummer} is at {na or 'no status'} after the edit")
        return 1
    print(f"board: #{nummer} Done → In progress")
    return 0


def _geen_board() -> int | None:
    """`None` met een board; zonder board de melding en exit 0, bij een configfout exit 2."""
    try:
        cfg = identity()
    except ConfigError as fout:
        print(f"board: ERROR — {foutnaam(fout)}")
        return 2
    if cfg.board is None:
        print(OVERGESLAGEN)
        return 0
    return None


def main(argv: list[str]) -> int:
    if argv[:1] == ["--reopen"]:
        if len(argv) != 2 or not argv[1].isdigit():
            print("board: usage: board_start.py --reopen <nr>")
            return 1
        code = _geen_board()
        return heropen(int(argv[1])) if code is None else code
    if len(argv) != 1 or not argv[0].isdigit():
        print("board: usage: board_start.py <nr>")
        return 0
    code = _geen_board()
    if code is not None:
        return code
    nummer = int(argv[0])
    try:
        item = lees_item(nummer)
        if item is None:
            print(f"board: #{nummer} is not on the board")
            return 0
        item_id, status = item
        if status not in OPPAKBAAR:
            print(
                f"board: #{nummer} stays {'at ' + status if status else 'without status'}"
            )
            return 0
        zet_in_progress(item_id)
    except GH_FOUTEN as fout:
        print(f"board: skipped ({foutnaam(fout)})")
        return 0
    print(f"board: #{nummer} {status} → In progress")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
