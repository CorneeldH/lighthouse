"""Issue, handoff en board per PR (#1016 taak 5): één per PR onder de drempel.

Voedt de nightly-arm van `ratchet.py` (taak 6): voor elke PR met te lage
diff-coverage opent (of herstelt) deze module een GitHub-issue, plakt de
vaste `🤖 Handoff`-comment die de controller parseert, en zet het issue op
het board met Status `Controller`. Een issue doorloopt vier stappen, in deze
volgorde: (1) `issue create` met beide labels, (2) de handoff-comment, (3) op
het board, (4) Status `Controller`. De comment staat vóór de Status: de
controller kijkt alleen naar issues op `Controller`, dus zo ziet hij het
issue nooit zonder handoff. Een issue van een eerdere nacht doorloopt
dezelfde vier stappen, maar slaat elke stap over die al gedaan is
(reconcile, geen tweede issue, geen dubbele comment).

Ontwerp: docs/developer/planning/specs/2026-09-26-ci-doorlooptijd-design.md
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
import time
from collections.abc import Callable, Mapping, Sequence
from pathlib import Path

# scripts/quality/ ligt twee mappen onder de repo-root; `quality.diffcov` heeft
# scripts/ op sys.path nodig, `src.controller.*` de repo-root zelf. Zelfde vorm
# als scripts/board_start.py (repo-root) en scripts/quality/ratchet.py:29
# (scripts/), hier samen omdat deze module beide nodig heeft.
REPO_ROOT = Path(__file__).resolve().parents[2]
_SCRIPTS = REPO_ROOT / "scripts"
for _path in (_SCRIPTS, REPO_ROOT):
    if str(_path) not in sys.path:
        sys.path.insert(0, str(_path))

from quality.diffcov import DiffCoverage
from src.controller.comments import KOP
from src.controller.identity import identity

Gh = Callable[[Sequence[str], Mapping[str, str] | None, str | None], str]

BOARD_TOKEN_ENV = "PAIA_BOARD_TOKEN"
LABELS = ("1-loc:container", "2-area:meta")

# De controller start `/paia-start-issue` alleen in een worktree die hij kent
# (`decide._start_handoff` toetst cwd + branch); voor dat commando is dat de
# hoofdcheckout op `main`. De nightly draait in de `_work`-map van een runner
# en kan die niet afleiden — vandaar deze constante, bewaakt door de
# round-trip-test hieronder.
HOOFDCHECKOUT = "/home/corneel/repositories/personal_ai_agent"

# Status `None` (nog geen item) of `"To do"` (Auto-add zette hem daar net) —
# in beide gevallen heeft de controller of de gebruiker het issue nog niet
# overgenomen. Elke andere status (`Controller`, `In progress`, `On hold`,
# `Done`) blijft staan.
NIET_OVERGEDRAGEN = (None, "To do")

# Owner en naam gaan als GraphQL-variabelen mee: de query kent de repo niet.
_STATUS_QUERY = """
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


class _GhFout(subprocess.CalledProcessError):
    """Zelfde uitzondering als de brief eist (`isinstance` blijft kloppen), maar
    met de stderr van `gh` in `str(fout)` — anders toont een naïeve `print(fout)`
    in de nightly-log alleen "returned non-zero exit status N", niet wat `gh`
    zelf zei (review-ronde 1, bevinding 5)."""

    def __str__(self) -> str:
        basis = super().__str__()
        stderr = (self.stderr or "").strip()
        return f"{basis}\n{stderr}" if stderr else basis


def run_gh(
    args: Sequence[str],
    env: Mapping[str, str] | None = None,
    input: str | None = None,
) -> str:
    """De echte `Gh`: roept `gh` aan, met `env` boven op de bestaande omgeving.

    `timeout=60` zoals `scripts/board_start.py`: een hangende `gh`-aanroep mag
    de nightly niet vasthouden. Bij rc ≠ 0 een `subprocess.CalledProcessError`
    (via `_GhFout`, zie daar) — nooit stil doorlopen op een fout van `gh`.
    """
    full_env = {**os.environ, **env} if env else None
    try:
        proc = subprocess.run(
            ["gh", *args],
            input=input,
            capture_output=True,
            text=True,
            env=full_env,
            timeout=60,
            check=True,
        )
    except subprocess.CalledProcessError as fout:
        raise _GhFout(fout.returncode, fout.cmd, fout.output, fout.stderr) from fout
    return proc.stdout


def title(pr: int, pct: float, threshold: int) -> str:
    return f"Diff-coverage #{pr} {pct:.1f}% < {threshold}%"


def ranges(lines: Sequence[int]) -> str:
    """`[3, 4, 5, 9]` → `"3-5, 9"` — samenhangende regelnummers als bereik."""
    ordered = sorted(set(lines))
    if not ordered:
        return ""
    stukken: list[str] = []
    start = vorige = ordered[0]
    for regel in ordered[1:]:
        if regel == vorige + 1:
            vorige = regel
            continue
        stukken.append(str(start) if start == vorige else f"{start}-{vorige}")
        start = vorige = regel
    stukken.append(str(start) if start == vorige else f"{start}-{vorige}")
    return ", ".join(stukken)


def body(pr: int, result: DiffCoverage, threshold: int, run_url: str | None) -> str:
    regels = [
        (
            f"PR #{pr} is on `main` with {result.pct:.1f}% diff coverage on the lines "
            f"it still owns, below the {threshold}% floor in `quality-baseline.toml`."
        ),
        "",
        "Uncovered lines:",
    ]
    for pad in sorted(result.uncovered):
        regels.append(f"- `{pad}`: {ranges(result.uncovered[pad])}")
    regels.append("")
    if run_url:
        regels.append(f"Found by the nightly run: {run_url}")
    regels.append(
        "Cover these lines with a test, or mark code that cannot run with "
        "`# pragma: no cover` and a reason."
    )
    return "\n".join(regels)


def last_success_sha(gh: Gh) -> str | None:
    """`headSha` van de laatste geslaagde nightly-run op `main`, of `None`.

    `--event schedule` sluit een `workflow_dispatch` uit: een handmatige run met
    `since=`/een lage `min_diff_pct` is anders ook `success` en verschuift het
    watermerk, waarna PR's die ertussen liggen nooit beoordeeld worden (I1,
    #1016-review).
    """
    output = gh(
        [
            "run",
            "list",
            "--workflow",
            "nightly-tests.yml",
            "--branch",
            "main",
            "--status",
            "success",
            "--event",
            "schedule",
            "-L",
            "1",
            "--json",
            "headSha",
        ],
        None,
        None,
    )
    runs = json.loads(output)
    if not runs:
        return None
    return str(runs[0]["headSha"])


def ensure_issue(
    gh: Gh, pr: int, result: DiffCoverage, threshold: int, run_url: str | None
) -> tuple[str, int, str] | None:
    """Stap 1: zoek, reconcile of maak het issue voor deze PR.

    `(actie, nummer, url)` met actie `"created"` of `"reconciled"`; `None` als
    er alleen een gesloten issue voor deze PR bestaat (al afgehandeld). Open
    wint van gesloten. Reconcile voegt alleen ontbrekende labels toe, nooit
    een tweede issue.
    """
    prefix = f"Diff-coverage #{pr} "
    output = gh(
        [
            "issue",
            "list",
            "--state",
            "all",
            "--search",
            f'"{prefix}" in:title',
            "--json",
            "number,state,url,title,labels",
        ],
        None,
        None,
    )
    kandidaten = json.loads(output)
    matches = [c for c in kandidaten if str(c.get("title", "")).startswith(prefix)]
    open_matches = [c for c in matches if str(c.get("state", "")).upper() == "OPEN"]
    if open_matches:
        issue = open_matches[0]
        nummer = int(issue["number"])
        url = str(issue["url"])
        aanwezig = {label["name"] for label in issue.get("labels", [])}
        ontbreekt = [label for label in LABELS if label not in aanwezig]
        if ontbreekt:
            edit_args = ["issue", "edit", str(nummer)]
            for label in ontbreekt:
                edit_args += ["--add-label", label]
            gh(edit_args, None, None)
        return ("reconciled", nummer, url)
    if matches:
        return None  # alleen gesloten — al afgehandeld
    nieuwe_titel = title(pr, result.pct, threshold)
    nieuwe_body = body(pr, result, threshold, run_url)
    create_args = [
        "issue",
        "create",
        "--title",
        nieuwe_titel,
        "--body",
        nieuwe_body,
    ]
    for label in LABELS:
        create_args += ["--label", label]
    url = gh(create_args, None, None).strip()
    nummer = int(url.rstrip("/").rsplit("/", 1)[-1])
    return ("created", nummer, url)


def handoff_comment(issue_number: int, pr: int, threshold: int) -> str:
    """De vaste `🤖 Handoff`-comment die de controller parseert en start."""
    return (
        f"{KOP} Handoff\n"
        f"issue: #{issue_number}\n"
        f"worktree: {HOOFDCHECKOUT}\n"
        "branch: main\n"
        f"anker: #{issue_number}\n"
        f"volgende: /paia-start-issue {issue_number}\n"
        "wanneer: nu\n"
        "\n"
        "```\n"
        f"Werk in de hoofdcheckout {HOOFDCHECKOUT} (branch main). Doel: dek de "
        f"regels van PR #{pr} die de nightly onder {threshold}% vond.\n"
        "\n"
        f"Anker: issue #{issue_number} (de ongedekte regels staan in de body)\n"
        "Staat: nieuw issue van de nightly-run; nog geen worktree, niets gestart.\n"
        f"Eerste stap: /paia-start-issue {issue_number}\n"
        "```"
    )


def ensure_handoff(gh: Gh, issue_number: int, pr: int, threshold: int) -> bool:
    """Stap 2: plak de handoff-comment, tenzij hij er al staat.

    `--jq '.[].body | split("\\n")[0]'` geeft per comment alleen zíjn eigen eerste
    regel terug, niet alle bodies achter elkaar geplakt. Dat onderscheid is nodig
    (review-ronde 1, bevinding 2): een comment die begint met `🤖 Handoff · …`
    telt dan nog mee (`startswith`, geen exacte match), maar een comment die het
    kopje ergens halverwege citeert — bijvoorbeeld in een quote — telt niet, want
    dat kopje staat niet op de eerste regel van *die* comment. `--paginate`: een
    issue met veel comments geeft anders alleen de eerste pagina terug.
    """
    output = gh(
        [
            "api",
            "--paginate",
            f"repos/{identity().repo}/issues/{issue_number}/comments",
            "--jq",
            r'.[].body | split("\n")[0]',
        ],
        None,
        None,
    )
    kop = f"{KOP} Handoff"
    if any(regel.startswith(kop) for regel in output.splitlines()):
        return False
    gh(
        ["issue", "comment", str(issue_number), "--body-file", "-"],
        None,
        handoff_comment(issue_number, pr, threshold),
    )
    return True


def _lees_status(
    gh: Gh, token: str, issue_number: int
) -> tuple[str, str | None] | None:
    """`(item_id, status)` van dit issue op óns board, of `None` als het er niet op staat."""
    repo = identity()
    output = gh(
        [
            "api",
            "graphql",
            "-f",
            f"query={_STATUS_QUERY}",
            "-f",
            f"owner={repo.owner}",
            "-f",
            f"name={repo.name}",
            "-F",
            f"nummer={issue_number}",
        ],
        {"GH_TOKEN": token},
        None,
    )
    data = json.loads(output)
    nodes = data["data"]["repository"]["issue"]["projectItems"]["nodes"]
    for node in nodes:
        if node["project"]["id"] == repo.board.project_id:
            waarde = node.get("fieldValueByName") or {}
            return str(node["id"]), waarde.get("name")
    return None


def _zet_controller(gh: Gh, token: str, item_id: str) -> None:
    board = identity().board
    gh(
        [
            "project",
            "item-edit",
            "--id",
            item_id,
            "--project-id",
            board.project_id,
            "--field-id",
            board.status_field_id,
            "--single-select-option-id",
            board.status_options["Controller"],
        ],
        {"GH_TOKEN": token},
        None,
    )


def _item_id_uit_add(output: str) -> str:
    """`id` uit `project item-add --format json`, of `""` als het antwoord leeg
    of onleesbaar is.

    Zaadwaarde voor `item_id`: de GraphQL-statuslezing hierna overschrijft hem
    zodra ze zelf een node op ons project vindt. Zonder dit zaad draait een
    `item-edit` met een lege `--id` als die lezing het item onverwacht niet
    terugvindt — `project item-add` levert dezelfde node-id, alleen synchroon
    (review-ronde 1, bevinding 3).
    """
    try:
        data = json.loads(output)
    except json.JSONDecodeError:
        return ""
    return str(data.get("id") or "") if isinstance(data, dict) else ""


def ensure_on_board(
    gh: Gh,
    issue_number: int,
    issue_url: str,
    token: str,
    sleep: Callable[[float], None] = time.sleep,
) -> str:
    """Stap 3 en 4: op het board, en Status `Controller` als niemand het al overnam.

    Auto-add zet een nieuw item asynchroon op `To do`; een `item-edit` vóór die
    stap wordt overschreven. Daarom eerst herlezen zolang de Status `None` is
    (hoogstens 5 keer, met `sleep(3)` ertussen). Staat hij daarna in
    `NIET_OVERGEDRAGEN`, dan naar `Controller`, daarna `sleep(3)` en één keer
    opnieuw lezen: staat hij dan op `To do` (een late Auto-add), dan nog één
    `item-edit`. Elke andere status blijft staan.
    """
    repo = identity()
    env = {"GH_TOKEN": token}
    toegevoegd = gh(
        [
            "project",
            "item-add",
            str(repo.board.project_number),
            "--owner",
            repo.owner,
            "--url",
            issue_url,
            "--format",
            "json",
        ],
        env,
        None,
    )
    item_id = _item_id_uit_add(toegevoegd)

    status: str | None = None
    for poging in range(5):
        gevonden = _lees_status(gh, token, issue_number)
        if gevonden is not None:
            gevonden_id, status = gevonden
            if gevonden_id:
                item_id = gevonden_id
        if status is not None:
            break
        if poging < 4:
            sleep(3)

    if status in NIET_OVERGEDRAGEN:
        _zet_controller(gh, token, item_id)
        status = "Controller"
        sleep(3)
        gevonden = _lees_status(gh, token, issue_number)
        if gevonden is not None:
            gevonden_id, status = gevonden
            if gevonden_id:
                item_id = gevonden_id
        if status == "To do":
            _zet_controller(gh, token, item_id)
            status = "Controller"

    return f"#{issue_number} op board, Status {status}"
