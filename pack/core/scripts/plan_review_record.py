"""Attempt directory and measurement record of the adversarial plan and spec review.

Twee soorten (`kind`, `"plan"` of `"spec"`) delen één schrijfpad en één
recordformaat: alleen de documentsleutel (`plan`/`spec`) en de bijbehorende
hash-velden (`plan_sha256_*`/`spec_sha256_*`) volgen `kind`. Vier subcommando's,
aangeroepen door de operator van `plan-adversarial-review` of
`spec-adversarial-review`:

    plan_review_record.py start [--kind plan|spec] <DOC>
        Reserveert de ephemere attempt-map
        `${TMPDIR:-/tmp}/<kind>-review/<chain-key>/<attempt>/` en legt de commit
        vast die ronde 1 leest. Print het pad. Het document moet gecommit zijn.
        Op de host uit host.toml staat de attempt meteen ook als `.<attempt>.tmp/` in de
        datamap: de nieuwste attempt is gezaghebbend, ook als `write` nooit draait.

    plan_review_record.py write <ATTEMPT_DIR> --outcome ... --review-mode ... [...]
        Schrijft de ronde-JSON's, hun decisions-sidecars en één `record.json` naar
        `<datamap>/<kind>-review/<chain-key>/<attempt>/`, via een stagingmap en één
        rename. `kind` komt uit `attempt.json` (ontbreekt het, dan is het `plan`).
        Alleen op de host uit host.toml; elders geen record en een luide melding.

    plan_review_record.py accept-open <ATTEMPT_DIR> --doc <DOCPAD>
        Zet bij de rondegrens elk nog open BLOCKER/MAJOR van de laatste ronde op
        `accepted_open` in `round-<laatste>.decisions.json` en zet een blok
        `heading:open_after_review` achteraan in `<DOCPAD>` (vervangt een bestaand blok;
        elke variant telt). Print het blok, in `[project] language`. Draait op elke host.
        Exit 0 ok | 2 geen open punt | 4 lees-, parse- of schrijffout
        (`ACCEPT-OPEN FAILED`). Een fout vóór het
        schrijven laat sidecar en document onaangeroerd; faalt het schrijven van het
        document ná de sidecar, dan is de sidecar al herschreven: opnieuw draaien herstelt dat.

    plan_review_record.py pr-open-points <DOCPAD>...
        Print voor de PR-body de blokken `heading:open_after_review` van de documenten
        (en van de specs die de `**Spec:**`-regel van een plan noemt) onder `## Open
        review points`; geen blok, dan niets. Een ontbrekend pad wordt overgeslagen.
        Draait op elke host. Exit 0 ok | 4 andere lees- of configfout
        (`PR-OPEN-POINTS FAILED`).

`<chain-key>` is `PurePosixPath(docpad).stem`. De datamap komt uit
`datadir.data_dir()` (besluit 27), die een map binnen een git-werkboom weigert. De
spec- en planmappen komen uit `paths.specs` en `paths.plans`.

Exit: 0 ok | 2 verkeerd gebruik | 3 niet de host uit host.toml, of host onbekend/niet
meetbaar: geen record |
4 schrijf- of validatiefout (alleen de stagingmap blijft staan). De skill gaat bij
3 en 4 door met de handoff: de review is het product, het record meetmateriaal.

Vanaf #1066 is ronde 1 een review en is elke volgende ronde een verificatie
(`verify.sh`, herkenbaar aan de top-level sleutel `verified` in de ronde-JSON).
`round_kinds` legt dat per ronde vast voor de meting; `schema_version: 1`-records
hebben het veld niet en zijn allemaal reviewrondes.

`schema_version` gaat met #988 van `2` naar `3`: dat voegt `kind` toe aan het
record, en voor ronde 1 `paired`/`shadow` plus per bevinding `source` en
`severity_by_source` — of, en met welk model, `review_pair.py merge` (buiten dit
bestand) een gepaarde schaduwrun tegen Codex zag. v1- en v2-records missen die
velden: een lezer behandelt een ontbrekende `kind` als `plan` en ontbrekende
paarvelden als ongepaard.

`schema_version` gaat met #1093 van `3` naar `4`: `budget_checks` is een lijst met
per `round-<n>*.budget.json` (de budgetcheck die de runner vóór elke ronde doet,
ook `round-1.codex.json.budget.json` van de gepaarde ronde 1) één entry met
`round`, `bestand`, `besluit`, `reden`, `session_pct`, `week_pct`, `bron` en
`drempels`; een bestand dat geen JSON-object is wordt `besluit: "onleesbaar"`
met de rest `None`. De bestanden zelf worden naast het record bewaard. Een lezer
behandelt een ontbrekende `budget_checks` als "geen check gedaan" (v1-v3).

`schema_version` gaat bij de verhuizing naar lighthouse van `4` naar `5`: de velden
van een `budget_checks`-entry volgen de Engelse budget-JSON van `codex_usage.py`
(`round`, `file`, `decision`, `reason`, `session_pct`, `week_pct`, `source`,
`thresholds`), en een onleesbaar bestand wordt `decision: "unreadable"`.
"""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
import pathlib
import re
import secrets
import shutil
import subprocess
import sys

# Run as a file on its path: put `.lighthouse/lib` first, so `lighthouse_lib` imports
# from this installation.
if not __package__:
    sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1] / "lib"))

from lighthouse_lib import headings
from lighthouse_lib.config import (
    ConfigError,
    Host,
    config_path,
    host_path,
    is_local,
    load,
    load_host,
    lookup,
    repo_root,
)
from lighthouse_lib.datadir import data_dir

# Een onbekende of niet meetbare host is een configfout van `host.toml`.
HostError = ConfigError

KINDS = ("plan", "spec")
DEFAULT_SKILL_DIRS = {
    "plan": ".claude/skills/lighthouse-plan-adversarial-review",
    "spec": ".claude/skills/lighthouse-spec-adversarial-review",
}
OUTCOMES = ("converged", "round_limit", "aborted")
REVIEW_MODES = ("codex", "codex_same_family", "fallback")
DECISIONS_BY_KIND = {
    "plan": ("accept", "reject", "spec", "accepted_open", "minor_yes", "minor_no"),
    "spec": ("accept", "reject", "defer", "accepted_open", "minor_yes", "minor_no"),
}
# "Terecht": plan-review accept/spec, spec-review accept/defer (Taak 4).
JUSTIFIED_BY_KIND = {"plan": ("accept", "spec"), "spec": ("accept", "defer")}
# Beslissingen die een BLOCKER/MAJOR-bevinding sluiten (build_record.still_open).
CLOSED_BY_KIND = {"plan": ("reject",), "spec": ("reject", "defer")}
ROUND_FILE = re.compile(r"^round-(\d+)\.json$")
BUDGET_FILE = re.compile(r"^round-(\d+)\..+\.budget\.json$")
# De velden van de budget-JSON van `codex_usage.py budget` die het record bewaart.
BUDGET_FIELDS = ("decision", "reason", "session_pct", "week_pct", "source", "thresholds")
SOURCES = ("codex", "fallback", "both")
SEVERITIES = ("BLOCKER", "MAJOR", "MINOR")
# Bestanden van het gepaarde ronde-1-experiment (#988, `review_pair.py`); `write`
# neemt ze mee naar de recordmap als ze bestaan, ronde 1 is de enige paarronde.
PAIR_FILES = (
    "pair.json",
    "round-1.codex.json",
    "round-1.fallback.json",
    "round-1.blind-decisions.json",
    "round-1.sources.json",
)


def recognize() -> Host | None:
    """De host uit `host.toml` als dit die machine is, anders `None`.

    `HostError` als `host.toml` ontbreekt of kapot is, of het adres niet meetbaar is.
    """
    host = load_host(host_path())
    return host if is_local(host.tailscale_ip) else None


def _git(repo: pathlib.Path, *args: str) -> str:
    return subprocess.run(
        ["git", "-C", str(repo), *args], capture_output=True, text=True, check=True
    ).stdout


def _sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def _blob_sha256(repo: pathlib.Path, sha: str, path: str) -> str:
    blob = subprocess.run(
        ["git", "-C", str(repo), "show", f"{sha}:{path}"],
        capture_output=True,
        check=True,
    ).stdout
    return _sha256(blob)


def chain_key(plan: str) -> str:
    return pathlib.PurePosixPath(plan).stem


def host_refusal(kind: str) -> str | None:
    """`None` op de herkende host (host.toml), anders de stderr-regel die de meting weigert."""
    try:
        if recognize() is not None:
            return None
        oorzaak = "not on the host from host.toml"
    except HostError as exc:
        oorzaak = str(exc)
    return (
        f"{kind.upper()}-REVIEW NOT RECORDED: {oorzaak} — this chain falls outside "
        "the measurement. The review itself is valid; carry on."
    )


def start(
    doc: str, repo: pathlib.Path, tmp_root: pathlib.Path, kind: str = "plan"
) -> pathlib.Path:
    """Reserveer een nieuwe attempt-map; atomisch via `mkdir`, nooit hergebruikt."""
    if not (repo / doc).is_file():
        raise ValueError(f"{kind} not found under the repository root: {repo / doc}")
    if _git(repo, "status", "--porcelain", "--", doc).strip():
        raise ValueError(f"{kind} has uncommitted changes, commit first: {doc}")
    base = tmp_root / f"{kind}-review" / chain_key(doc)
    base.mkdir(parents=True, exist_ok=True)
    stamp = dt.datetime.now(dt.UTC).strftime("%Y%m%dT%H%M%SZ")
    while True:
        attempt = base / f"{stamp}-{secrets.token_hex(3)}"
        try:
            attempt.mkdir()
            break
        except FileExistsError:
            continue
    (attempt / "attempt.json").write_text(
        json.dumps(
            {
                "kind": kind,
                "chain_key": chain_key(doc),
                "attempt": attempt.name,
                kind: doc,
                "reviewed_sha": _git(repo, "rev-parse", "HEAD").strip(),
            },
            indent=2,
        )
    )
    return attempt


def _meta(attempt_dir: pathlib.Path) -> dict:
    """Lees `attempt.json`; ontbreekt `kind` (v1/v2-attempt), dan is het `plan`."""
    meta = json.loads((attempt_dir / "attempt.json").read_text())
    meta.setdefault("kind", "plan")
    return meta


def _staging(
    data_root: pathlib.Path, kind: str, chain: str, attempt: str
) -> pathlib.Path:
    return data_root / f"{kind}-review" / chain / f".{attempt}.tmp"


def reserve(attempt_dir: pathlib.Path) -> None:
    """Zet de nieuwe attempt meteen in de datamap (spec sectie 5).

    Per keten is de nieuwste attempt überhaupt gezaghebbend, ook zonder record.
    Zonder deze reservering zou een review die nooit bij `write` komt onzichtbaar
    zijn, en telde een oudere attempt ten onrechte. Een mislukte reservering
    blokkeert de review niet: `write` maakt de map dan alsnog.
    """
    try:
        meta = _meta(attempt_dir)
    except (OSError, ValueError) as exc:
        print(
            f"PLAN-REVIEW-RESERVATION FAILED: could not read attempt.json: {exc}",
            file=sys.stderr,
        )
        return
    kind = meta["kind"]
    if (reden := host_refusal(kind)) is not None:
        print(reden, file=sys.stderr)
        return
    try:
        staging = _staging(data_dir(), kind, meta["chain_key"], meta["attempt"])
        staging.mkdir(parents=True)
        shutil.copy2(attempt_dir / "attempt.json", staging / "attempt.json")
    except (OSError, ValueError, KeyError, RuntimeError) as exc:
        print(
            f"{kind.upper()}-REVIEW-RESERVATION FAILED: {exc}. The review carries on; "
            "`write` creates the staging directory after all.",
            file=sys.stderr,
        )


def _rounds(attempt_dir: pathlib.Path) -> dict[int, pathlib.Path]:
    found = {}
    for p in attempt_dir.iterdir():
        m = ROUND_FILE.match(p.name)
        if m:
            found[int(m.group(1))] = p
    return dict(sorted(found.items()))


def _budget_checks(attempt_dir: pathlib.Path) -> list[dict]:
    """Eén entry per budget-bestand van de runner; onleesbaar of geen object -> `unreadable`."""
    checks = []
    for p in attempt_dir.iterdir():
        m = BUDGET_FILE.match(p.name)
        if not m:
            continue
        try:
            doc = json.loads(p.read_text())
        except (OSError, ValueError):
            doc = None
        if not isinstance(doc, dict):
            doc = {"decision": "unreadable"}
        checks.append(
            {
                "round": int(m.group(1)),
                "file": p.name,
                **{k: doc.get(k) for k in BUDGET_FIELDS},
            }
        )
    return sorted(checks, key=lambda c: (c["round"], c["file"]))


def _decisions(
    attempt_dir: pathlib.Path, n: int, n_findings: int, kind: str
) -> dict[str, dict]:
    """De beslissingen van ronde `n`, per `finding_id`; elk ID bestaat en staat er één keer."""
    sidecar = attempt_dir / f"round-{n}.decisions.json"
    if not sidecar.is_file():
        return {}
    valid = {f"r{n}-f{i}" for i in range(1, n_findings + 1)}
    parsed = json.loads(sidecar.read_text())
    if not isinstance(parsed, list):
        # TRY004: bewust ValueError, geen TypeError — main() vangt alleen
        # ValueError/OSError/... af voor exit 4 (PLAN-REVIEW-RECORD FAILED).
        raise ValueError(  # noqa: TRY004
            f"{sidecar.name}: expected a list, got {type(parsed).__name__}"
        )
    by_id: dict[str, dict] = {}
    for row in parsed:
        if not isinstance(row, dict):
            raise ValueError(  # noqa: TRY004
                f"{sidecar.name}: row is no object but {type(row).__name__}"
            )
        fid = row.get("finding_id")
        if row.get("decision") not in DECISIONS_BY_KIND[kind]:
            raise ValueError(
                f"{sidecar.name}: unknown decision {row.get('decision')!r}"
            )
        if fid not in valid:
            raise ValueError(
                f"{sidecar.name}: finding_id {fid!r} does not exist in round {n}"
            )
        if fid in by_id:
            raise ValueError(f"{sidecar.name}: finding_id {fid!r} occurs twice")
        by_id[fid] = row
    return by_id


def _sources(attempt_dir: pathlib.Path, n_findings: int) -> dict[str, dict] | None:
    """De bron per ronde-1-bevinding uit `round-1.sources.json`, `None` zonder paar.

    `review_pair.py merge` (Taak 3) schrijft de sidecar; ontbreekt hij, dan was
    ronde 1 geen paar (Codex-limiet, of gewoon geen gepaarde run) en levert deze
    functie niets. `blind_ids` staat op elke rij maar is hier meetmateriaal voor
    Taak 3/4, niet iets wat dit record zelf gebruikt.
    """
    sidecar = attempt_dir / "round-1.sources.json"
    if not sidecar.is_file():
        return None
    valid = {f"r1-f{i}" for i in range(1, n_findings + 1)}
    parsed = json.loads(sidecar.read_text())
    if not isinstance(parsed, list):
        raise ValueError(  # noqa: TRY004
            f"{sidecar.name}: expected a list, got {type(parsed).__name__}"
        )
    by_id: dict[str, dict] = {}
    for row in parsed:
        if not isinstance(row, dict):
            raise ValueError(  # noqa: TRY004
                f"{sidecar.name}: row is no object but {type(row).__name__}"
            )
        fid = row.get("finding_id")
        source = row.get("source")
        if source not in SOURCES:
            raise ValueError(f"{sidecar.name}: unknown source {source!r} at {fid!r}")
        severity_by_source = row.get("severity_by_source") or {}
        if not isinstance(severity_by_source, dict) or any(
            k not in ("codex", "fallback") or v not in SEVERITIES
            for k, v in severity_by_source.items()
        ):
            raise ValueError(
                f"{sidecar.name}: invalid severity_by_source at {fid!r}: "
                f"{severity_by_source!r}"
            )
        if fid not in valid:
            raise ValueError(
                f"{sidecar.name}: finding_id {fid!r} does not exist in round 1"
            )
        if fid in by_id:
            raise ValueError(f"{sidecar.name}: finding_id {fid!r} occurs twice")
        by_id[fid] = {"source": source, "severity_by_source": severity_by_source}
    missing = valid - set(by_id)
    if missing:
        raise ValueError(
            f"{sidecar.name}: misses finding_id(s) {', '.join(sorted(missing))}"
        )
    return by_id


def _build_findings(
    attempt_dir: pathlib.Path, rounds: dict[int, pathlib.Path], kind: str
) -> tuple[list[dict], list[str], dict[str, dict] | None]:
    """Alle bevindingen over alle rondes, plus `round_kinds` en de ronde-1-bron.

    `sources` is `None` zonder `round-1.sources.json` (geen paar); alleen ronde-1-
    bevindingen krijgen dan een niet-`None` `source`/`severity_by_source`.
    """
    findings = []
    round_kinds = []
    sources: dict[str, dict] | None = None
    for n, path in rounds.items():
        round_doc = json.loads(path.read_text())
        round_kinds.append("verify" if "verified" in round_doc else "review")
        by_id = _decisions(attempt_dir, n, len(round_doc["findings"]), kind)
        if n == 1:
            sources = _sources(attempt_dir, len(round_doc["findings"]))
        for index, f in enumerate(round_doc["findings"], start=1):
            fid = f"r{n}-f{index}"
            row = by_id.get(fid, {})
            src_row = sources.get(fid) if sources is not None and n == 1 else None
            findings.append(
                {
                    "finding_id": fid,
                    "round": n,
                    "severity": f["severity"],
                    "cat": f.get("cat"),
                    "decision": row.get("decision"),
                    "same_as": row.get("same_as"),
                    "source": src_row["source"] if src_row else None,
                    "severity_by_source": (
                        src_row["severity_by_source"] if src_row else None
                    ),
                }
            )
    return findings, round_kinds, sources


def _check_shadow_model(
    sources: dict[str, dict] | None, shadow_model: str | None
) -> None:
    """`--shadow-model` gaat exact samen met een gepaarde ronde 1, nooit los."""
    if sources is not None and not shadow_model:
        raise ValueError(
            "--shadow-model is required: round-1.sources.json exists "
            "(round 1 is paired)"
        )
    if sources is None and shadow_model:
        raise ValueError(
            "--shadow-model belongs only to a paired round 1 "
            "(round-1.sources.json is missing)"
        )


def _codex_config(codex_home: pathlib.Path) -> dict[str, str]:
    config = codex_home / "config.toml"
    values: dict[str, str] = {}
    if config.is_file():
        for line in config.read_text().splitlines():
            m = re.match(r'^(model|model_reasoning_effort)\s*=\s*"([^"]*)"', line)
            if m:
                values[m.group(1)] = m.group(2)
    return values


def codex_version() -> str | None:
    try:
        out = subprocess.run(
            ["codex", "--version"], capture_output=True, text=True, check=False
        )
    except FileNotFoundError:
        return None
    return out.stdout.strip() or None


def build_record(
    attempt_dir: pathlib.Path,
    *,
    repo: pathlib.Path,
    outcome: str,
    review_mode: str,
    specs: list[str],
    final_sha: str,
    level: str | None,
    reviewer_model: str | None,
    skill_dir: str,
    codex_home: pathlib.Path,
    issue: dict | None,
    shadow_model: str | None = None,
) -> dict:
    meta = _meta(attempt_dir)
    kind = meta["kind"]
    doc_path, reviewed_sha = meta[kind], meta["reviewed_sha"]
    rounds = _rounds(attempt_dir)
    if not rounds:
        raise ValueError(f"no round-<n>.json in {attempt_dir}")
    missing = sorted(set(range(1, max(rounds) + 1)) - set(rounds))
    if missing:
        raise ValueError(
            f"missing round(s) in {attempt_dir}: "
            + ", ".join(str(m) for m in missing)
        )

    findings, round_kinds, sources = _build_findings(attempt_dir, rounds, kind)
    _check_shadow_model(sources, shadow_model)
    undecided = [f["finding_id"] for f in findings if f["decision"] is None]
    if undecided and outcome != "aborted":
        raise ValueError(
            f"no decision for {', '.join(undecided)}; at {outcome} every finding "
            "has one (one sidecar per round, the last one too)"
        )
    last = max(rounds)
    last_round_open = [
        {
            "finding_id": f["finding_id"],
            "severity": f["severity"],
            "decision": f["decision"],
        }
        for f in findings
        if f["round"] == last and f["severity"] in ("BLOCKER", "MAJOR")
    ]
    still_open = [
        o["finding_id"]
        for o in last_round_open
        if o["decision"] not in CLOSED_BY_KIND[kind]
    ]
    if outcome == "converged" and still_open:
        raise ValueError(
            f"converged, but {', '.join(still_open)} is still open: a fix after the "
            "last round is unreviewed (skill step 8, *Open*)"
        )
    if outcome == "round_limit":
        unaccepted = [
            o["finding_id"]
            for o in last_round_open
            if o["finding_id"] in still_open and o["decision"] != "accepted_open"
        ]
        if unaccepted:
            raise ValueError(
                f"round_limit, but {', '.join(unaccepted)} carries no accepted_open: "
                "run 'plan_review_record.py accept-open <ATTEMPT> --doc <DOC>' first"
            )

    config = _codex_config(codex_home)
    if review_mode == "fallback":
        if not reviewer_model:
            raise ValueError("--reviewer-model is required with --review-mode fallback")
        reviewer = {"model": reviewer_model, "codex_version": None, "same_family": True}
    else:
        reviewer = {
            "model": config.get("model", "unknown"),
            "codex_version": codex_version(),
            "same_family": review_mode == "codex_same_family",
        }

    skill = repo / skill_dir
    verifier_prompt = skill / "verifier-prompt.md"
    verification_schema = skill / "verification.schema.json"
    return {
        "schema_version": 5,
        "kind": kind,
        "chain_key": meta["chain_key"],
        "attempt": meta["attempt"],
        "review_mode": review_mode,
        kind: doc_path,
        "reviewed_sha": reviewed_sha,
        "final_sha": final_sha,
        f"{kind}_sha256_reviewed": _blob_sha256(repo, reviewed_sha, doc_path),
        f"{kind}_sha256_final": _blob_sha256(repo, final_sha, doc_path),
        "specs": [
            {"path": s, "sha256": _blob_sha256(repo, final_sha, s)} for s in specs
        ],
        "issue_spec": issue,
        "prompt_sha256": _sha256((skill / "reviewer-prompt.md").read_bytes()),
        "verifier_prompt_sha256": (
            _sha256(verifier_prompt.read_bytes()) if verifier_prompt.is_file() else None
        ),
        "schema_sha256": _sha256((skill / "findings.schema.json").read_bytes()),
        "verification_schema_sha256": (
            _sha256(verification_schema.read_bytes())
            if verification_schema.is_file()
            else None
        ),
        "reviewer": reviewer,
        "level": level
        or (
            None if review_mode == "fallback" else config.get("model_reasoning_effort")
        ),
        "rounds": len(rounds),
        "round_kinds": round_kinds,
        "outcome": outcome,
        "paired": sources is not None,
        "shadow": (
            {"model": shadow_model, "same_family": True}
            if sources is not None
            else None
        ),
        "budget_checks": _budget_checks(attempt_dir),
        "findings": findings,
        "last_round_open": last_round_open,
        "written_at": dt.datetime.now(dt.UTC).isoformat(timespec="seconds"),
    }


def write(
    attempt_dir: pathlib.Path,
    record: dict,
    data_root: pathlib.Path,
    issue_body: pathlib.Path | None,
) -> pathlib.Path:
    """Vul de stagingmap van `reserve`, record.json als laatste, dan één rename."""
    kind = record["kind"]
    final = data_root / f"{kind}-review" / record["chain_key"] / record["attempt"]
    staging = _staging(data_root, kind, record["chain_key"], record["attempt"])
    if final.exists():
        raise FileExistsError(f"attempt already exists in the data directory: {final}")
    staging.mkdir(parents=True, exist_ok=True)
    for path in sorted(attempt_dir.iterdir()):
        if (
            ROUND_FILE.match(path.name)
            or BUDGET_FILE.match(path.name)
            or path.name.endswith(".decisions.json")
            or path.name == "attempt.json"
            or path.name in PAIR_FILES
        ):
            shutil.copy2(path, staging / path.name)
    if issue_body is not None:
        shutil.copy2(issue_body, staging / record["issue_spec"]["file"])
    (staging / "record.json").write_text(
        json.dumps(record, indent=2, ensure_ascii=False) + "\n"
    )
    os.rename(staging, final)
    return final


# De tekst onder de kop van het blok, per `[project] language`.
OPEN_TEXT = {
    "nl": (
        "Ronde {n} haalde de rondegrens met deze punten open. "
        "Ze zijn aanvaard als `accepted_open`;",
        "de volgende fase of de PR-review weegt ze opnieuw.",
    ),
    "en": (
        "Round {n} reached the round limit with these points open. "
        "They are accepted as `accepted_open`;",
        "the next phase or the PR review weighs them again.",
    ),
}
CLOSING_KEYWORD = re.compile(
    r"(?i)\b(close[sd]?|fix(?:e[sd])?|resolve[sd]?)(:?\s+)#(\d+)"
)
FENCE = re.compile(r"^\s{0,3}(`{3,}|~{3,})")


def open_points(attempt_dir: pathlib.Path) -> list[dict]:
    """De BLOCKER/MAJOR van de laatste ronde zonder sluitende beslissing, in ID-volgorde."""
    kind = _meta(attempt_dir)["kind"]
    rounds = _rounds(attempt_dir)
    if not rounds:
        raise ValueError(f"no round-<n>.json in {attempt_dir}")
    last = max(rounds)
    findings = json.loads(rounds[last].read_text())["findings"]
    by_id = _decisions(attempt_dir, last, len(findings), kind)
    points = []
    for index, f in enumerate(findings, start=1):
        fid = f"r{last}-f{index}"
        decision = by_id.get(fid, {}).get("decision")
        if (
            f["severity"] in ("BLOCKER", "MAJOR")
            and decision not in CLOSED_BY_KIND[kind]
        ):
            points.append(
                {
                    "finding_id": fid,
                    "severity": f["severity"],
                    "section": f.get("plan_section") or f.get("spec_section") or "?",
                    "problem": f.get("problem", ""),
                    "decision": decision,
                }
            )
    return points


def _accepted_rows(attempt_dir: pathlib.Path) -> tuple[int, list[dict], list[dict]]:
    """Plan van `accept_open` zonder te schrijven: (ronde, nieuwe sidecar-regels, open punten erna)."""
    points = open_points(attempt_dir)
    last = max(_rounds(attempt_dir))
    sidecar = attempt_dir / f"round-{last}.decisions.json"
    rows: list[dict] = json.loads(sidecar.read_text()) if sidecar.is_file() else []
    by_id = {r["finding_id"]: r for r in rows}
    for point in points:
        row = by_id.get(point["finding_id"])
        if row is None:
            rows.append(
                {
                    "finding_id": point["finding_id"],
                    "decision": "accepted_open",
                    "reason": "round_limit; without an earlier decision",
                }
            )
        elif row["decision"] != "accepted_open":
            old = f"was {row['decision']}" + (
                f": {row['reason']}" if row.get("reason") else ""
            )
            row["decision"] = "accepted_open"
            row["reason"] = f"round_limit; {old}"
    return last, rows, [{**p, "decision": "accepted_open"} for p in points]


def _write_sidecar(attempt_dir: pathlib.Path, n: int, rows: list[dict]) -> None:
    (attempt_dir / f"round-{n}.decisions.json").write_text(
        json.dumps(rows, indent=2, ensure_ascii=False) + "\n"
    )


def accept_open(attempt_dir: pathlib.Path) -> tuple[int, list[dict]]:
    """Herschrijf de sidecar van de laatste ronde: elk open punt wordt `accepted_open`."""
    last, rows, points = _accepted_rows(attempt_dir)
    if points:
        _write_sidecar(attempt_dir, last, rows)
    return last, points


def render_open(n_round: int, points: list[dict], language: str | None = None) -> str:
    """Het markdownblok `heading:open_after_review` in `language` (default `[project]
    language`); sluitwoorden met `#N` zijn geneutraliseerd."""
    if language is None:
        language = load(config_path()).language
    first, second = OPEN_TEXT[language]
    lines = [
        headings.heading("open_after_review", language),
        "",
        first.format(n=n_round),
        second,
        "",
    ]
    for p in points:
        problem = CLOSING_KEYWORD.sub(r"\1\2issue \3", " ".join(p["problem"].split()))
        lines.append(
            f"- **{p['finding_id']}** ({p['severity']}, {p['section']}): {problem}"
        )
    return "\n".join(lines) + "\n"


def open_sectie(text: str) -> tuple[int, int] | None:
    """De span van het blok `heading:open_after_review` (elke variant), als het de laatste
    `## `-sectie is.

    Regels binnen een code fence (``` of ~~~) tellen niet als kop. De span loopt van
    de kop tot het einde van `text`.
    """
    fence: str | None = None
    last_heading: tuple[int, str] | None = None
    offset = 0
    for line in text.splitlines(keepends=True):
        m = FENCE.match(line)
        if fence is not None:
            if m and m.group(1)[0] == fence[0] and len(m.group(1)) >= len(fence):
                fence = None
        elif m:
            fence = m.group(1)
        elif line.startswith("## "):
            last_heading = (offset, line.rstrip())
        offset += len(line)
    if last_heading is None or last_heading[1] not in headings.variants("open_after_review"):
        return None
    return last_heading[0], len(text)


def _with_open_block(text: str, block: str) -> str:
    span = open_sectie(text)
    if span is not None:
        return text[: span[0]] + block
    return text.rstrip() + "\n\n" + block if text.strip() else block


def upsert_open_block(doc: pathlib.Path, block: str) -> None:
    """Vervang het bestaande blok in `doc`, of plak het met één lege regel erachter."""
    doc.write_text(_with_open_block(doc.read_text(), block))


def _plans_dir() -> str:
    """`paths.plans`, met één `/` erachter."""
    return str(lookup(load(config_path()), "paths.plans")).strip("/") + "/"


def _spec_path() -> re.Pattern[str]:
    """Een pad onder `paths.specs` naar een `.md`-bestand."""
    specs = str(lookup(load(config_path()), "paths.specs")).strip("/")
    return re.compile(re.escape(specs) + r"/[^\s)\]>`'\"#]+\.md")


SPEC_LINK = re.compile(r"\]\((?!https?:)([^)\s]+\.md)\)")


def _spec_docs(plan: pathlib.Path, text: str, repo: pathlib.Path) -> list[pathlib.Path]:
    """De specs die de `**Spec:**`-regel van een plan noemt en die onder `repo` bestaan."""
    regel = next((r for r in text.splitlines() if r.startswith("**Spec:**")), "")
    kandidaten = [repo / m for m in _spec_path().findall(regel)]
    kandidaten += [plan.parent / m for m in SPEC_LINK.findall(regel)]
    found: list[pathlib.Path] = []
    for pad in kandidaten:
        pad = pad.resolve()
        if pad.is_relative_to(repo.resolve()) and pad.exists() and pad not in found:
            found.append(pad)
    return found


def _rel(doc: pathlib.Path, repo: pathlib.Path) -> str:
    try:
        return doc.resolve().relative_to(repo.resolve()).as_posix()
    except ValueError:
        return str(doc)


def pr_open_punten(docs: list[pathlib.Path], repo: pathlib.Path) -> str:
    """De blokken `heading:open_after_review` van `docs` (en van de specs van hun plannen)
    voor de PR-body.

    Een ontbrekend document wordt overgeslagen; een andere `OSError`, een
    `UnicodeDecodeError` (geen tekstbestand) of een `ConfigError` stijgt op.
    Geen blok: lege string.
    """
    plans_dir = _plans_dir()
    secties: list[str] = []
    gezien: set[pathlib.Path] = set()
    queue = list(docs)
    while queue:
        doc = queue.pop(0)
        sleutel = doc.resolve()
        if sleutel in gezien:
            continue
        gezien.add(sleutel)
        try:
            text = doc.read_text()
        except FileNotFoundError:
            continue
        span = open_sectie(text)
        if span is not None:
            bullets = [r for r in text[span[0] :].splitlines() if r.startswith("- ")]
            secties.append("\n".join([f"From {_rel(doc, repo)}:", *bullets]))
        if _rel(doc, repo).startswith(plans_dir):
            queue[:0] = _spec_docs(doc, text, repo)
    if not secties:
        return ""
    return "## Open review points\n\n" + "\n\n".join(secties) + "\n"


def _parse(argv: list[str] | None) -> argparse.Namespace:
    p = argparse.ArgumentParser(prog="plan_review_record.py")
    sub = p.add_subparsers(dest="cmd", required=True)
    s = sub.add_parser("start")
    s.add_argument("--kind", choices=KINDS, default="plan")
    s.add_argument("doc")
    a = sub.add_parser("accept-open")
    a.add_argument("attempt_dir", type=pathlib.Path)
    a.add_argument("--doc", required=True, type=pathlib.Path)
    o = sub.add_parser("pr-open-points")
    o.add_argument("docs", nargs="+", type=pathlib.Path)
    w = sub.add_parser("write")
    w.add_argument("attempt_dir", type=pathlib.Path)
    w.add_argument("--outcome", required=True, choices=OUTCOMES)
    w.add_argument("--review-mode", required=True, choices=REVIEW_MODES)
    w.add_argument("--spec", action="append", default=[])
    w.add_argument("--issue", type=int)
    w.add_argument("--issue-body", type=pathlib.Path)
    w.add_argument("--issue-fetched-at")
    w.add_argument("--final-sha")
    w.add_argument("--level")
    w.add_argument("--reviewer-model")
    w.add_argument("--shadow-model")
    w.add_argument("--skill-dir", default=None)
    args = p.parse_args(argv)
    if args.cmd == "write":
        issue_args = (args.issue, args.issue_body, args.issue_fetched_at)
        if any(a is not None for a in issue_args) and None in issue_args:
            p.error("--issue, --issue-body and --issue-fetched-at go together")
    return args


def _accepteer_open(attempt_dir: pathlib.Path, doc: pathlib.Path) -> int:
    """Eerst lezen en renderen, dan de sidecar, dan het document; een fout ervoor schrijft niets."""
    try:
        last, rows, points = _accepted_rows(attempt_dir)
        if not points:
            print(
                "no open BLOCKER/MAJOR in the last round: nothing to accept",
                file=sys.stderr,
            )
            return 2
        block = render_open(last, points)
        new_text = _with_open_block(doc.read_text(), block)
        _write_sidecar(attempt_dir, last, rows)
        doc.write_text(new_text)
    except (OSError, ValueError, KeyError, ConfigError) as exc:
        print(f"ACCEPT-OPEN FAILED: {exc}", file=sys.stderr)
        return 4
    sys.stdout.write(block)
    return 0


def _pr_open_punten(docs: list[pathlib.Path], repo: pathlib.Path) -> int:
    try:
        sectie = pr_open_punten(docs, repo)
    except (OSError, UnicodeDecodeError, ConfigError) as exc:
        print(f"PR-OPEN-POINTS FAILED: {exc}", file=sys.stderr)
        return 4
    sys.stdout.write(sectie)
    return 0


def _start_attempt(args: argparse.Namespace, repo: pathlib.Path) -> int:
    tmp_root = pathlib.Path(os.environ.get("TMPDIR") or "/tmp")
    try:
        attempt = start(args.doc, repo, tmp_root, kind=args.kind)
    except ValueError as exc:
        print(f"start failed: {exc}", file=sys.stderr)
        return 2
    reserve(attempt)
    print(attempt)
    return 0


def _documentcommando(args: argparse.Namespace, repo: pathlib.Path) -> int:
    """De subcommando's die alleen documenten lezen of schrijven: elke host, geen host-check."""
    if args.cmd == "accept-open":
        return _accepteer_open(args.attempt_dir, args.doc)
    return _pr_open_punten(args.docs, repo)


def main(argv: list[str] | None = None) -> int:
    args = _parse(argv)
    try:
        repo = repo_root()
    except ConfigError as exc:
        print(f"plan_review_record: {exc}", file=sys.stderr)
        return 2

    if args.cmd == "start":
        return _start_attempt(args, repo)

    if args.cmd in ("accept-open", "pr-open-points"):
        return _documentcommando(args, repo)

    try:
        kind = _meta(args.attempt_dir).get("kind", "plan")
    except (OSError, ValueError, KeyError):
        kind = "plan"

    if kind == "plan":
        if not args.spec and args.issue is None:
            print("give at least one --spec or --issue", file=sys.stderr)
            return 2
    elif args.spec or args.issue is not None:
        print(
            f"--spec/--issue do not belong to --kind spec: {args.attempt_dir} is a "
            "spec review, which has no separate specs or issue",
            file=sys.stderr,
        )
        return 2

    if (reden := host_refusal(kind)) is not None:
        print(reden, file=sys.stderr)
        return 3

    try:
        issue = None
        if args.issue is not None:
            issue = {
                "issue": args.issue,
                "fetched_at": args.issue_fetched_at,
                "sha256": _sha256(args.issue_body.read_bytes()),
                "file": f"issue-{args.issue}.md",
            }
        final_sha = args.final_sha or _git(repo, "rev-parse", "HEAD").strip()
        codex_home = pathlib.Path(
            os.environ.get("CODEX_SPECREVIEW_HOME")
            or pathlib.Path.home() / ".codex-specreview"
        )
        record = build_record(
            args.attempt_dir,
            repo=repo,
            outcome=args.outcome,
            review_mode=args.review_mode,
            specs=args.spec,
            final_sha=final_sha,
            level=args.level,
            reviewer_model=args.reviewer_model,
            skill_dir=args.skill_dir or DEFAULT_SKILL_DIRS[kind],
            codex_home=codex_home,
            issue=issue,
            shadow_model=args.shadow_model,
        )
        data_root = data_dir()
        final = write(args.attempt_dir, record, data_root, args.issue_body)
    except (
        OSError,
        ValueError,
        KeyError,
        subprocess.CalledProcessError,
        RuntimeError,
    ) as exc:
        print(
            f"{kind.upper()}-REVIEW-RECORD FAILED: {exc}. Only the staging directory "
            "remains; the handoff carries on.",
            file=sys.stderr,
        )
        return 4
    print(final)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
