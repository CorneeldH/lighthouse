"""De gepaarde ronde-1-schaduwrun van de adversariële plan- en spec-review (#988).

In ronde 1 draait naast Codex een schaduwreviewer uit dezelfde modelfamilie, op
dezelfde SHA en met dezelfde prompt. De hoofdthread beslist blind op een A/B-tabel;
pas `merge` ontblindt. Opzet, grenzen en het besluit dat het paar blijft:
`experiments/codex-fallback-paren/RECEPT.md`. Drie subcommando's, aangeroepen
door de operator van `paia-plan-adversarial-review` of
`paia-spec-adversarial-review` (`tally` ook los):

    review_pair.py blind <ATTEMPT_DIR>
        Controleert eerst dat de werkboom sinds `start` niet veranderd is (`HEAD` is
        `reviewed_sha` uit `attempt.json`, `git status --porcelain` leeg), dan dat
        `round-1.codex.json` en `round-1.fallback.json` kloppen met het
        `findings.schema.json` van de skill voor `kind`. Lost één keer uit wie A is
        (`pair.json`, daarna hergebruikt) en schrijft en print `round-1.blind.md`:
        de bevindingen van beide als `A-f<i>`/`B-f<i>`, zonder bron.

    review_pair.py merge <ATTEMPT_DIR>
        Leest `pair.json`, de twee ruwe bestanden en de blinde beslissingen
        `round-1.blind-decisions.json` (`[{"finding_id", "also"?, "decision",
        "reason"}]`), en schrijft de canonieke `round-1.json` met
        `round-1.decisions.json` en `round-1.sources.json`: de sidecars die
        `plan_review_record.py write` meeneemt naar het record. Print de ontblinde
        tabel.

    review_pair.py tally [--data DIR]
        Print de telling als Markdown: per `kind` en totaal de paren, de terechte
        BLOCKER/MAJOR per reviewer en per bron, `codex_only_share`, de unieke
        vangsten en een verdict per `kind` (het totaal is context); daaronder alle records als ongepaarde context,
        gesplitst op de grens #1066. Default `DIR` is `lk_common.data_dir()`.

Exit `blind`/`merge`: 0 ok | 2 verkeerd gebruik | 4 `SCHADUWPAAR MISLUKT: <reden>`
— een gewijzigde werkboom, een ontbrekend of ongeldig bestand, of blinde
beslissingen die niet kloppen. Bij 4 schrijft het subcommando geen uitvoerbestand.
De review gaat dan door zonder paar: de review is het product, het paar
meetmateriaal.

Exit `tally`: 0 ok | 4 `TELLING MISLUKT: <reden>` (datamap ontbreekt, of datamap
of een record onleesbaar).

Een uitknop is er niet meer: de meting haalde zijn stopdrempel (plan 15/15, spec
8/8) en het besluit van 2026-10-04 maakt het paar in ronde 1 permanent (#988).
"""

from __future__ import annotations

import argparse
import importlib.util
import json
import pathlib
import re
import secrets
import subprocess
import sys
from collections import Counter, defaultdict
from typing import Any


def _record_module() -> Any:
    """`plan_review_record` laden zonder `sys.path` te buigen, net als `_lk_common()`."""
    path = pathlib.Path(__file__).resolve().parent / "plan_review_record.py"
    spec = importlib.util.spec_from_file_location("plan_review_record", path)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


_rec = _record_module()
ROOT = _rec.ROOT
REVIEWERS = ("codex", "fallback")
SEVERITY_RANK = {sev: rank for rank, sev in enumerate(_rec.SEVERITIES)}
MINOR_DECISIONS = ("minor_yes", "minor_no")
BLIND_DECISIONS = "round-1.blind-decisions.json"
JSON_TYPES = {"string": str, "array": list, "object": dict}
BLIND_HEADER = (
    "| ID | Sev | Cat | Section | Problem | Failure | Evidence | Proposed fix |\n"
    "|----|-----|-----|---------|---------|---------|----------|--------------|\n"
)
MERGE_HEADER = "| ID | Bron | Sev | Beslissing | Reden |\n|----|------|-----|------------|-------|\n"
# De enige schaduwreviewer die meetelt, na `canonical_model` (RECEPT.md).
SHADOW_MODEL = "claude-opus-5-5"
BM = ("BLOCKER", "MAJOR")
PAIR_SOURCES = ("both", "codex", "fallback")
TALLY_HEADER = (
    "| Groep | Paren | Codex terecht B/M | Fallback terecht B/M "
    "| both | codex | fallback | codex_only_share |\n"
    "|---|---|---|---|---|---|---|---|"
)
CONTEXT_HEADER = (
    "| Kind | Grens | review_mode | Records | Rondes: records | Outcome: records |\n"
    "|---|---|---|---|---|---|"
)
BOUNDARIES = ("voor #1066", "na #1066")


class PairError(ValueError):
    """Een paar dat niet klopt; `main` maakt er exit 4 van."""


def _tree_state(root: pathlib.Path = ROOT) -> tuple[str, bool]:
    """`HEAD` en of de werkboom schoon is; ongevolgde bestanden tellen mee."""
    head = _rec._git(root, "rev-parse", "HEAD").strip()
    status = _rec._git(root, "status", "--porcelain", "--untracked-files=normal")
    return head, not status.strip()


def _coin() -> bool:
    """Waar = Codex is A."""
    return secrets.randbelow(2) == 1


def _cell(text: str) -> str:
    """Eén tabelcel: een pipe of regeleinde uit reviewertekst verschuift geen kolom."""
    return (
        text.replace("\\", "\\\\")
        .replace("|", "\\|")
        .replace("\r\n", " ")
        .replace("\r", " ")
        .replace("\n", " ")
    )


def _violations(obj: object, node: dict) -> list[str]:
    """Wat aan `obj` niet klopt met één objectknoop van het schema, zonder `jsonschema`.

    Alleen wat de findings-schema's gebruiken: `required`, `additionalProperties:
    false`, per property `type` en `enum`, en bij een array één niveau `items.type`.
    Veldnamen komen uit het schema.
    """
    if not isinstance(obj, dict):
        return [f"geen object maar {type(obj).__name__}"]
    props = node.get("properties", {})
    errors = [f"mist {key!r}" for key in node.get("required", []) if key not in obj]
    if node.get("additionalProperties") is False:
        errors += [f"onbekend veld {key!r}" for key in obj if key not in props]
    for key, value in obj.items():
        spec = props.get(key, {})
        want = JSON_TYPES.get(spec.get("type"))
        if want and not isinstance(value, want):
            errors.append(f"{key!r} is geen {spec['type']}")
        elif "enum" in spec and value not in spec["enum"]:
            errors.append(f"{key!r} = {value!r} staat niet in {spec['enum']}")
        elif isinstance(value, list):
            item_type = spec.get("items", {}).get("type")
            want_item = JSON_TYPES.get(item_type)
            if want_item and not all(isinstance(v, want_item) for v in value):
                errors.append(f"{key!r} bevat een item dat geen {item_type} is")
    return errors


def _raw_violations(doc: object, schema: dict) -> list[str]:
    errors = _violations(doc, schema)
    if errors or not isinstance(doc, dict):
        return errors
    items = schema["properties"]["findings"]["items"]
    for index, finding in enumerate(doc["findings"], start=1):
        errors += [f"bevinding {index}: {e}" for e in _violations(finding, items)]
    return errors


def _load_raw(attempt_dir: pathlib.Path, reviewer: str, schema: dict) -> dict:
    name = f"round-1.{reviewer}.json"
    path = attempt_dir / name
    if not path.is_file():
        raise PairError(f"{name} ontbreekt")
    try:
        doc = json.loads(path.read_text())
    except (OSError, ValueError) as exc:
        raise PairError(f"{name}: {exc}") from exc
    errors = _raw_violations(doc, schema)
    if errors:
        raise PairError(
            f"{name} klopt niet met findings.schema.json: {'; '.join(errors)}"
        )
    return doc


def _load_raws(attempt_dir: pathlib.Path, kind: str) -> dict[str, dict]:
    schema_path = ROOT / _rec.DEFAULT_SKILL_DIRS[kind] / "findings.schema.json"
    schema = json.loads(schema_path.read_text())
    return {r: _load_raw(attempt_dir, r, schema) for r in REVIEWERS}


def _check_tree(meta: dict) -> None:
    if "reviewed_sha" not in meta:
        raise PairError("attempt.json mist reviewed_sha")
    head, clean = _tree_state()
    if head != meta["reviewed_sha"] or not clean:
        raise PairError("werkboom veranderd sinds start of niet schoon")


def _pair(attempt_dir: pathlib.Path) -> dict[str, str]:
    """De koppeling uit `pair.json`; nooit opnieuw geloot."""
    path = attempt_dir / "pair.json"
    if not path.is_file():
        raise PairError("pair.json ontbreekt: draai eerst `blind`")
    pair = json.loads(path.read_text())
    if not isinstance(pair, dict) or sorted(pair.items()) not in (
        [("A", "codex"), ("B", "fallback")],
        [("A", "fallback"), ("B", "codex")],
    ):
        raise PairError("pair.json ongeldig: geen geldige A/B-koppeling")
    return pair


def _ensure_pair(attempt_dir: pathlib.Path) -> dict[str, str]:
    if not (attempt_dir / "pair.json").exists():
        a, b = REVIEWERS if _coin() else REVIEWERS[::-1]
        (attempt_dir / "pair.json").write_text(json.dumps({"A": a, "B": b}) + "\n")
    return _pair(attempt_dir)


def _blind_index(pair: dict[str, str], raws: dict[str, dict]) -> dict[str, tuple]:
    """Blind ID → (reviewer, positie vanaf 1, bevinding), in volgorde A dan B."""
    return {
        f"{letter}-f{i}": (pair[letter], i, finding)
        for letter in ("A", "B")
        for i, finding in enumerate(raws[pair[letter]]["findings"], start=1)
    }


def _blind_table(index: dict[str, tuple], kind: str) -> str:
    def order(item: tuple) -> tuple:
        blind_id, (_, i, finding) = item
        return SEVERITY_RANK[finding["severity"]], blind_id[0], i

    rows = []
    for blind_id, (_, _, f) in sorted(index.items(), key=order):
        cells = [
            blind_id,
            f["severity"],
            f.get("cat", "-"),
            f[f"{kind}_section"],
            f["problem"],
            f["failure_scenario"],
            f["evidence"],
            f["proposed_fix"],
        ]
        rows.append("| " + " | ".join(_cell(c) for c in cells) + " |\n")
    return BLIND_HEADER + "".join(rows)


def blind(attempt_dir: pathlib.Path) -> str:
    meta = _rec._meta(attempt_dir)
    _check_tree(meta)
    raws = _load_raws(attempt_dir, meta["kind"])
    pair = _ensure_pair(attempt_dir)
    table = _blind_table(_blind_index(pair, raws), meta["kind"])
    (attempt_dir / "round-1.blind.md").write_text(table)
    return table


def _load_blind_decisions(attempt_dir: pathlib.Path) -> list[dict]:
    path = attempt_dir / BLIND_DECISIONS
    if not path.is_file():
        raise PairError(f"{BLIND_DECISIONS} ontbreekt")
    rows = json.loads(path.read_text())
    if not isinstance(rows, list) or not all(isinstance(r, dict) for r in rows):
        raise PairError(f"{BLIND_DECISIONS}: verwacht een lijst van objecten")
    return rows


def _covered(row: dict, index: dict[str, tuple]) -> list[str]:
    """De blinde ID's die één beslissing dekt; `also` wijst naar de andere letter."""
    fid, also = row.get("finding_id"), row.get("also")
    ids = [i for i in (fid, also) if i is not None]
    if fid is None or any(i not in index for i in ids):
        raise PairError(f"onbekend of ontbrekend blind ID in {row!r}")
    if len(ids) == 2 and ids[0][0] == ids[1][0]:
        raise PairError(f"{fid} also {also}: `also` wijst naar dezelfde reviewer")
    return sorted(ids, key=lambda i: (i[0], index[i][1]))


def _check_decision(row: dict, severity: str, kind: str) -> None:
    decision, fid = row.get("decision"), row.get("finding_id")
    allowed = [d for d in _rec.DECISIONS_BY_KIND[kind] if d != "accepted_open"]
    if decision not in allowed:
        raise PairError(f"{fid}: beslissing {decision!r} hoort niet bij {kind}")
    if (severity == "MINOR") != (decision in MINOR_DECISIONS):
        raise PairError(f"{fid}: beslissing {decision!r} past niet bij {severity}")
    if not isinstance(row.get("reason"), str):
        raise PairError(f"{fid}: `reason` ontbreekt of is geen tekst")


def _groups(rows: list[dict], index: dict[str, tuple], kind: str) -> list[dict]:
    """Eén groep per samengevoegde bevinding, in de volgorde van `round-1.json`."""
    seen: set[str] = set()
    groups = []
    for row in rows:
        ids = _covered(row, index)
        double = seen.intersection(ids)
        if double:
            raise PairError(f"{min(double)} is meer dan één keer gedekt")
        seen.update(ids)
        by_source = {index[i][0]: (index[i][1], index[i][2]) for i in ids}
        severity = min(
            (f["severity"] for _, f in by_source.values()),
            key=SEVERITY_RANK.__getitem__,
        )
        _check_decision(row, severity, kind)
        groups.append(
            {"ids": ids, "by_source": by_source, "severity": severity, "row": row}
        )
    missing = [i for i in index if i not in seen]
    if missing:
        raise PairError(f"geen beslissing voor {', '.join(missing)}")
    return sorted(groups, key=_merge_order)


def _merge_order(group: dict) -> tuple[int, int]:
    """Eerst Codex in eigen volgorde, dan fallback zonder Codex-partner."""
    if "codex" in group["by_source"]:
        return 0, group["by_source"]["codex"][0]
    return 1, group["by_source"]["fallback"][0]


def _status(findings: list[dict]) -> str:
    severities = {f["severity"] for f in findings}
    if "BLOCKER" in severities:
        return "Blockers found"
    return "Majors only" if "MAJOR" in severities else "Clean"


def _merged_round(raws: dict[str, dict], groups: list[dict]) -> dict:
    findings = []
    for g in groups:
        source = "codex" if "codex" in g["by_source"] else "fallback"
        findings.append({**g["by_source"][source][1], "severity": g["severity"]})
    codex, fallback = raws["codex"], raws["fallback"]
    return {
        "review_markdown": (
            f"## Codex\n\n{codex['review_markdown']}\n\n"
            f"## Fallback\n\n{fallback['review_markdown']}"
        ),
        "status": _status(findings),
        "findings": findings,
        "top3": codex["top3"],
        "verdict": codex["verdict"],
        "could_not_verify": list(
            dict.fromkeys(codex["could_not_verify"] + fallback["could_not_verify"])
        ),
    }


def _source_rows(groups: list[dict]) -> list[dict]:
    return [
        {
            "finding_id": f"r1-f{n}",
            "source": "both"
            if len(g["by_source"]) == 2
            else next(iter(g["by_source"])),
            "blind_ids": g["ids"],
            "severity_by_source": {
                r: g["by_source"][r][1]["severity"]
                for r in REVIEWERS
                if r in g["by_source"]
            },
        }
        for n, g in enumerate(groups, start=1)
    ]


def _write_json(path: pathlib.Path, data: object) -> None:
    path.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n")


def merge(attempt_dir: pathlib.Path) -> str:
    kind = _rec._meta(attempt_dir)["kind"]
    pair = _pair(attempt_dir)
    raws = _load_raws(attempt_dir, kind)
    groups = _groups(_load_blind_decisions(attempt_dir), _blind_index(pair, raws), kind)
    sources = _source_rows(groups)
    decisions = [
        {
            "finding_id": s["finding_id"],
            "decision": g["row"]["decision"],
            "reason": g["row"]["reason"],
        }
        for s, g in zip(sources, groups, strict=True)
    ]
    # Eerst alles bouwen, dan schrijven: faalt `_merged_round`, dan blijft er geen
    # sidecar achter die `plan_review_record.py write` voor een paar aanziet.
    merged = _merged_round(raws, groups)
    _write_json(attempt_dir / "round-1.decisions.json", decisions)
    _write_json(attempt_dir / "round-1.sources.json", sources)
    _write_json(attempt_dir / "round-1.json", merged)
    rows = [
        f"| {s['finding_id']} | {s['source']} | {g['severity']} | "
        f"{_cell(g['row']['decision'])} | {_cell(g['row']['reason'])} |\n"
        for s, g in zip(sources, groups, strict=True)
    ]
    return MERGE_HEADER + "".join(rows)


def canonical_model(name: str) -> str:
    """Haal een contextsuffix tussen blokhaken van het eind: `x[1m]` wordt `x`."""
    return re.sub(r"\[[^\[\]]*\]$", "", name)


def _kind(record: dict) -> str:
    """Een v1/v2-record heeft geen `kind`: dat is een plan-review."""
    return str(record.get("kind") or "plan")


def _round1(record: dict) -> list[dict]:
    return [f for f in record.get("findings") or [] if f.get("round") == 1]


def _load_records(data_root: pathlib.Path) -> list[tuple[pathlib.Path, dict]]:
    """Elk `record.json` onder `{plan,spec}-review/<keten>/<attempt>/`, pad relatief.

    Mappen die met `.` beginnen (stagingmappen van `reserve`/`write`) tellen niet.
    """
    found = []
    for kind in _rec.KINDS:
        for path in sorted(data_root.glob(f"{kind}-review/*/*/record.json")):
            rel = path.relative_to(data_root)
            if any(part.startswith(".") for part in rel.parts):
                continue
            record = json.loads(path.read_text())
            if not isinstance(record, dict):
                raise TypeError(f"{rel}: geen JSON-object")
            found.append((rel, record))
    return found


def _is_paired(record: dict) -> bool:
    """Een telbaar paar: v3+, gepaard, `SHADOW_MODEL`, elke ronde-1-bevinding beslist."""
    shadow = record.get("shadow")
    model = shadow.get("model") if isinstance(shadow, dict) else None
    version = record.get("schema_version")
    return (
        isinstance(version, int)
        and version >= 3
        and record.get("paired") is True
        and isinstance(model, str)
        and canonical_model(model) == SHADOW_MODEL
        and all(f.get("decision") for f in _round1(record))
    )


def paired_records(data_root: pathlib.Path) -> list[dict]:
    return [r for _, r in _load_records(data_root) if _is_paired(r)]


def verdict(codex_only_share: float, justified_total: int) -> str:
    """De besluitregel uit RECEPT.md."""
    if justified_total < 10:
        return "te weinig"
    if codex_only_share <= 0.10:
        return "houden"
    if codex_only_share <= 0.30:
        return "aanscherpen"
    return "terugdraaien"


def _count(pairs: list[tuple[pathlib.Path, dict]]) -> dict[str, Any]:
    """Terechte B/M per reviewer (eigen severity) en per bron (samengevoegde severity)."""
    c: dict[str, Any] = dict.fromkeys(
        (*REVIEWERS, *(f"bron_{s}" for s in PAIR_SOURCES)), 0
    )
    c.update(paren=len(pairs), uniek=[])
    for rel, record in pairs:
        justified = _rec.JUSTIFIED_BY_KIND.get(_kind(record), ())
        for f in _round1(record):
            if f.get("decision") not in justified:
                continue
            own = f.get("severity_by_source") or {}
            for reviewer in REVIEWERS:
                c[reviewer] += own.get(reviewer) in BM
            source = f.get("source")
            if f.get("severity") in BM and source in PAIR_SOURCES:
                c[f"bron_{source}"] += 1
                if source != "both":
                    c["uniek"].append((source, f"{rel}#{f.get('finding_id')}"))
    return c


def _justified_total(c: dict) -> int:
    return sum(c[f"bron_{s}"] for s in PAIR_SOURCES)


def _share(c: dict) -> float | None:
    """Onafgerond: het verdict rekent hierop, alleen de tabel rondt af (RECEPT.md)."""
    total = _justified_total(c)
    return c["bron_codex"] / total if total else None


def _tally_row(label: str, c: dict) -> str:
    share = _share(c)
    cells = [
        label,
        c["paren"],
        c["codex"],
        c["fallback"],
        *(c[f"bron_{s}"] for s in PAIR_SOURCES),
        "-" if share is None else f"{share:.2f}",
    ]
    return "| " + " | ".join(str(x) for x in cells) + " |"


def _prompt_groups(pairs: list) -> dict[tuple[str, str], list]:
    groups: dict[tuple[str, str], list] = defaultdict(list)
    for rel, record in pairs:
        groups[(_kind(record), str(record.get("prompt_sha256") or "?"))].append(
            (rel, record)
        )
    return groups


def _pairs_section(pairs: list) -> list[str]:
    """Paren en verdict per `kind` (per prompt bij een gemengde `kind`); totaal als context."""
    groups = _prompt_groups(pairs)
    rows, verdicts = [], []
    for kind in _rec.KINDS:
        keys = sorted(k for k in groups if k[0] == kind)
        if len(keys) > 1:
            rows += [
                _tally_row(f"{kind} (prompt {p[:12]})", _count(groups[(kind, p)]))
                for _, p in keys
            ]
            verdicts.append(
                f"Verdict {kind}: gemengd: {len(keys)} prompts, geen verdict"
            )
        else:
            c = _count([x for k in keys for x in groups[k]])
            rows.append(_tally_row(kind, c))
            verdicts.append(
                f"Verdict {kind}: {verdict(_share(c) or 0.0, _justified_total(c))}"
            )
    total = _count(pairs)
    rows.append(_tally_row("totaal (context)", total))
    unique = [f"- {source}: {ref}" for source, ref in total["uniek"]] or ["- geen"]
    return [
        "## Paren (ronde 1)",
        "",
        TALLY_HEADER,
        *rows,
        "",
        *verdicts,
        "",
        "Unieke terechte B/M-vangsten:",
        "",
        *unique,
    ]


def _boundary(record: dict) -> str:
    """Grens #1066: `schema_version` 1 (of geen) is ervoor, 2 en hoger erna."""
    version = record.get("schema_version")
    return BOUNDARIES[isinstance(version, int) and version >= 2]


def _dist(values: list) -> str:
    counts = sorted(Counter(values).items(), key=lambda kv: str(kv[0]))
    return ", ".join(f"{value}: {n}" for value, n in counts)


def _context_section(records: list[tuple[pathlib.Path, dict]]) -> list[str]:
    """Alle records als context; een regel mengt nooit twee kanten van #1066."""
    buckets: dict[tuple, list[dict]] = defaultdict(list)
    for _, r in records:
        key = (_kind(r), BOUNDARIES.index(_boundary(r)), str(r.get("review_mode")))
        buckets[key].append(r)
    rows = [
        f"| {kind} | {BOUNDARIES[side]} | {mode} | {len(rs)} "
        f"| {_dist([r.get('rounds') for r in rs])} "
        f"| {_dist([r.get('outcome') for r in rs])} |"
        for (kind, side, mode), rs in sorted(buckets.items())
    ]
    return [
        "## Context, ongepaard (geen deel van het verdict)",
        "",
        "Alle records, alleen absolute aantallen.",
        "",
        CONTEXT_HEADER,
        *rows,
    ]


def tally(data_root: pathlib.Path) -> str:
    if not data_root.is_dir():
        raise FileNotFoundError(f"datamap bestaat niet: {data_root}")
    records = _load_records(data_root)
    pairs = [(rel, r) for rel, r in records if _is_paired(r)]
    lines = [
        "# Codex-fallbackmeting",
        "",
        *_pairs_section(pairs),
        "",
        *_context_section(records),
    ]
    return "\n".join(lines) + "\n"


def _run_pair(args: argparse.Namespace) -> int:
    try:
        table = args.func(args.attempt_dir)
    except (
        OSError,
        ValueError,
        KeyError,
        TypeError,
        subprocess.CalledProcessError,
    ) as exc:
        print(f"SCHADUWPAAR MISLUKT: {exc}", file=sys.stderr)
        return 4
    print(table, end="")
    return 0


def _run_tally(args: argparse.Namespace) -> int:
    try:
        out = tally(args.data or _rec._lk_common().data_dir())
    except (OSError, ValueError, TypeError, RuntimeError) as exc:
        print(f"TELLING MISLUKT: {exc}", file=sys.stderr)
        return 4
    print(out, end="")
    return 0


def _parse(argv: list[str] | None) -> argparse.Namespace:
    p = argparse.ArgumentParser(prog="review_pair.py")
    sub = p.add_subparsers(dest="cmd", required=True)
    for name, func in (("blind", blind), ("merge", merge)):
        s = sub.add_parser(name)
        s.add_argument("attempt_dir", type=pathlib.Path)
        s.set_defaults(func=func, run=_run_pair)
    t = sub.add_parser("tally")
    t.add_argument("--data", type=pathlib.Path)
    t.set_defaults(run=_run_tally)
    return p.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = _parse(argv)
    return args.run(args)


if __name__ == "__main__":
    raise SystemExit(main())
