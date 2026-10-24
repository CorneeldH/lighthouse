"""Issue, handoff en board per PR: één per PR onder de drempel.

Voedt de nightly-arm van `ratchet.py`: voor elke PR met te lage diff-coverage
opent (of herstelt) deze module een GitHub-issue, plakt de `🤖 Handoff`-comment
van het comment-protocol (`lighthouse_lib.protocol`), en zet het issue op het
board met status `Controller`. Een issue doorloopt vier stappen, in deze
volgorde: (1) `issue create` met de labels uit `python.coverage_issue_labels`,
(2) de handoff-comment, (3) op het board, (4) status `Controller`. De comment
staat vóór de status: de controller kijkt alleen naar issues op `Controller`,
dus zo ziet hij het issue nooit zonder handoff. Een issue van een eerdere nacht
doorloopt dezelfde vier stappen, maar slaat elke stap over die al gedaan is
(reconcile, geen tweede issue, geen dubbele comment).

Het board is optioneel (besluit 16): zonder `[forge.board]` komen alleen het
issue en de handoff. Zonder statusoptie `Controller` blijft het item op `To do`.
Repo, taal, board en default branch komen uit `lighthouse.toml`.
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
import time
from collections.abc import Callable, Mapping, Sequence
from pathlib import Path

# `quality.diffcov` heeft de map scripts/ op sys.path nodig (één map boven deze),
# `lighthouse_lib` de map lib/ ernaast: geïnstalleerd `.lighthouse/scripts/quality/`
# naast `.lighthouse/lib/` (ontwerpkeuze 6 van (b), voor een script in een submap).
_SCRIPTS = Path(__file__).resolve().parents[1]
_LIB = Path(__file__).resolve().parents[2] / "lib"
for _path in (_SCRIPTS, _LIB):
    if _path.is_dir() and str(_path) not in sys.path:
        sys.path.insert(0, str(_path))

from lighthouse_lib import config, headings, protocol
from quality.diffcov import DiffCoverage

Gh = Callable[[Sequence[str], Mapping[str, str] | None, str | None], str]

BOARD_TOKEN_ENV = "LIGHTHOUSE_BOARD_TOKEN"
# De worktree in de handoff is een pad op de host waar de controller draait, niet
# op de runner: de nightly zet hem in deze env. Zonder env de hoofdcheckout van de
# repo waarin het script draait.
HANDOFF_WORKTREE_ENV = "LIGHTHOUSE_HANDOFF_WORKTREE"
START_COMMAND = "/lighthouse-start-issue"

# Status `None` (nog geen item) of `"To do"` (Auto-add zette hem daar net) —
# in beide gevallen heeft de controller of de gebruiker het issue nog niet
# overgenomen. Elke andere status (`Controller`, `In progress`, `On hold`,
# `Done`) blijft staan.
NIET_OVERGEDRAGEN = (None, "To do")

def _config() -> config.Config:
    """`lighthouse.toml`, bij elke aanroep opnieuw gelezen; `forge.repo` is verplicht."""
    cfg = config.load(config.config_path())
    if cfg.repo is None:
        raise config.ConfigError(f"{config.config_path()}: forge.repo is not set")
    return cfg


def _setting(key: str) -> object:
    return config.lookup(_config(), key)


def board() -> config.Board | None:
    """Het board uit `[forge.board]`, of `None` als de repo er geen heeft."""
    return _config().board


def default_branch() -> str:
    return str(_setting("forge.default_branch"))


def labels() -> list[str]:
    value = _setting("python.coverage_issue_labels")
    return [str(label) for label in value] if isinstance(value, list) else []


def nightly_workflow() -> str | None:
    """De workflow van het watermerk (`python.nightly_workflow`), of `None`."""
    try:
        return str(_setting("python.nightly_workflow"))
    except KeyError:
        return None


def handoff_worktree() -> str:
    """De worktree in de handoff: `LIGHTHOUSE_HANDOFF_WORKTREE`, anders de map boven
    de gedeelde `.git` van deze repo (de hoofdcheckout, ook vanuit een worktree)."""
    override = os.environ.get(HANDOFF_WORKTREE_ENV)
    if override:
        return override
    proc = subprocess.run(
        ["git", "rev-parse", "--path-format=absolute", "--git-common-dir"],
        cwd=config.repo_root(),
        capture_output=True,
        text=True,
        check=False,
    )
    if proc.returncode != 0 or not proc.stdout.strip():
        raise config.ConfigError(
            f"the main checkout is not found ({proc.stderr.strip() or 'git rev-parse failed'}); "
            f"set {HANDOFF_WORKTREE_ENV}"
        )
    return str(Path(proc.stdout.strip()).parent)


# Owner en naam gaan als GraphQL-variabelen mee: de query kent de repo niet.
_STATUS_QUERY = """
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


class _GhFout(subprocess.CalledProcessError):
    """Een `CalledProcessError` (`isinstance` blijft kloppen), maar met de stderr
    van `gh` in `str(fout)` — anders toont een naïeve `print(fout)` in de
    nightly-log alleen "returned non-zero exit status N", niet wat `gh` zelf zei."""

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

    `timeout=60`: een hangende `gh`-aanroep mag
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
            f"PR #{pr} is on `{default_branch()}` with {result.pct:.1f}% diff coverage on the lines "
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


def last_success_sha(gh: Gh, workflow: str) -> str | None:
    """`headSha` van de laatste geslaagde run van `workflow` op de default branch,
    of `None`.

    `--event schedule` sluit een `workflow_dispatch` uit: een handmatige run met
    `since=`/een lage `min_diff_pct` is anders ook `success` en verschuift het
    watermerk, waarna PR's die ertussen liggen nooit beoordeeld worden.
    """
    output = gh(
        [
            "run",
            "list",
            "--workflow",
            workflow,
            "--branch",
            default_branch(),
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
    wanted = labels()
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
        ontbreekt = [label for label in wanted if label not in aanwezig]
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
    for label in wanted:
        create_args += ["--label", label]
    url = gh(create_args, None, None).strip()
    nummer = int(url.rstrip("/").rsplit("/", 1)[-1])
    return ("created", nummer, url)


def handoff_comment(issue_number: int, pr: int, threshold: int) -> str:
    """De `🤖 Handoff`-comment van het protocol: de kop in `[project] language`, de
    sleutels in het Engels (besluit 21), zodat de controller hem parseert en start."""
    cfg = _config()
    worktree = handoff_worktree()
    branch = str(config.lookup(cfg, "forge.default_branch"))
    start = f"{START_COMMAND} {issue_number}"
    return (
        f"{protocol.ROBOT} {headings.heading('kind_handoff', cfg.language)}\n"
        f"issue: #{issue_number}\n"
        f"worktree: {worktree}\n"
        f"branch: {branch}\n"
        f"anchor: #{issue_number}\n"
        f"next: {start}\n"
        "when: now\n"
        "\n"
        "```\n"
        f"Work in the main checkout {worktree} (branch {branch}). Goal: cover the "
        f"lines of PR #{pr} that the nightly found below {threshold}%.\n"
        "\n"
        f"Anchor: issue #{issue_number} (the uncovered lines are in the body)\n"
        "State: new issue from the nightly run; no worktree yet, nothing started.\n"
        f"First step: {start}\n"
        "```"
    )


def ensure_handoff(gh: Gh, issue_number: int, pr: int, threshold: int) -> bool:
    """Stap 2: plak de handoff-comment, tenzij hij er al staat.

    `--jq '.[].body | @json'` geeft per comment één regel: zijn hele body als
    JSON-string. `protocol.parse_header` leest daarvan de eerste regel, dus een
    comment die begint met `🤖 Handoff · …` telt mee, en ook een handoff van vóór
    de verhuizing (`volgende:`, `wanneer:`), maar een comment die het kopje ergens
    halverwege citeert telt niet. `--paginate`: een issue met veel comments geeft
    anders alleen de eerste pagina terug.
    """
    output = gh(
        [
            "api",
            "--paginate",
            f"repos/{_config().repo}/issues/{issue_number}/comments",
            "--jq",
            ".[].body | @json",
        ],
        None,
        None,
    )
    for regel in output.splitlines():
        if not regel.strip():
            continue
        header = protocol.parse_header(str(json.loads(regel)))
        if header is not None and header.kind == "handoff":
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
    repo = _config()
    assert repo.board is not None
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
            f"number={issue_number}",
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


def _zet_controller(gh: Gh, token: str, item_id: str, board: config.Board) -> None:
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
            board.status_options[config.CONTROLLER_STATUS_OPTION],
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
    terugvindt — `project item-add` levert dezelfde node-id, alleen synchroon.
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
    """Stap 3 en 4: op het board, en status `Controller` als niemand het al overnam.

    Auto-add zet een nieuw item asynchroon op `To do`; een `item-edit` vóór die
    stap wordt overschreven. Daarom eerst herlezen zolang de Status `None` is
    (hoogstens 5 keer, met `sleep(3)` ertussen). Staat hij daarna in
    `NIET_OVERGEDRAGEN`, dan naar `Controller`, daarna `sleep(3)` en één keer
    opnieuw lezen: staat hij dan op `To do` (een late Auto-add), dan nog één
    `item-edit`. Elke andere status blijft staan.

    Zonder `[forge.board]` geen enkele board-aanroep; zonder statusoptie
    `Controller` wel op het board, maar de status blijft staan. Beide gevallen
    staan in de teruggegeven regel, geen van beide is een fout.
    """
    repo = _config()
    if repo.board is None:
        return "board: skipped — no [forge.board] configured"
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

    if status in NIET_OVERGEDRAGEN and config.CONTROLLER_STATUS_OPTION not in repo.board.status_options:
        return (
            f"#{issue_number} on board, status {status or 'To do'}; "
            "board: status Controller skipped — no such status option"
        )
    if status in NIET_OVERGEDRAGEN:
        _zet_controller(gh, token, item_id, repo.board)
        status = "Controller"
        sleep(3)
        gevonden = _lees_status(gh, token, issue_number)
        if gevonden is not None:
            gevonden_id, status = gevonden
            if gevonden_id:
                item_id = gevonden_id
        if status == "To do":
            _zet_controller(gh, token, item_id, repo.board)
            status = "Controller"

    return f"#{issue_number} on board, status {status}"
