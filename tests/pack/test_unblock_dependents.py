"""Unit tests for the release lane (`.lighthouse/scripts/unblock_dependents.py`), from paia: welke afhankelijken komen vrij, en wat wordt er geschreven.

`run` draait tegen een `FakeGh` die `gh` vervangt. Geen netwerk en geen echte `gh`.
De fixtures volgen de gemeten vorm van `gh api .../dependencies/blocked_by` (#1354).
"""

from __future__ import annotations

import importlib.util
import json
import subprocess
import sys
from collections.abc import Sequence
from pathlib import Path
from typing import Any

import pytest

from tests.pack._testkit import INSTALL_ROOT

_SCRIPT = INSTALL_ROOT / ".lighthouse" / "scripts" / "unblock_dependents.py"
_spec = importlib.util.spec_from_file_location("unblock_dependents", _SCRIPT)
assert _spec is not None and _spec.loader is not None
ud = importlib.util.module_from_spec(_spec)
sys.modules[_spec.name] = ud
_spec.loader.exec_module(ud)

R = "repos/{owner}/{repo}"
LABEL = "blocked-issue"


def _issue(
    nr: int, state: str = "open", labels: Sequence[str] = (), **over: Any
) -> dict:
    issue = {
        "number": nr,
        "state": state,
        "state_reason": "completed" if state == "closed" else None,
        "labels": [{"name": n} for n in labels],
    }
    issue.update(over)
    return issue


class FakeGh:
    """Leest uit `reads` (pad zonder `?per_page`), noteert elke schrijfactie in `writes`."""

    def __init__(self, reads: dict[str, Any], faalt: set[str] = frozenset()) -> None:
        self.reads = reads
        self.faalt = faalt
        self.writes: list[tuple[str, ...]] = []

    def __call__(self, args: Sequence[str]) -> str:
        pad = next(a for a in args if a.startswith(R))
        if pad in self.faalt:
            raise subprocess.CalledProcessError(
                1, ["gh", *args], stderr="HTTP 403: nee\n"
            )
        if "-X" in args:
            self.writes.append(tuple(args))
            return "{}"
        return json.dumps(self.reads[pad.split("?")[0]])


def _wereld(gesloten: dict, afhankelijk: list[dict], blockers: dict[int, list[dict]]):
    nr = gesloten["number"]
    reads: dict[str, Any] = {
        f"{R}/issues/{nr}": gesloten,
        f"{R}/issues/{nr}/dependencies/blocking": afhankelijk,
    }
    for m, lijst in blockers.items():
        reads[f"{R}/issues/{m}/dependencies/blocked_by"] = lijst
    return reads


def _comments(gh: FakeGh) -> dict[int, str]:
    uit = {}
    for w in gh.writes:
        if w[2] == "POST":
            nr = int(w[3].split("/")[-2])
            uit[nr] = w[5].removeprefix("body=")
    return uit


def test_laatste_blocker_dicht_geeft_vrij_met_comments() -> None:
    gh = FakeGh(
        _wereld(
            _issue(1, "closed"),
            [_issue(2, labels=["2-area:x", LABEL])],
            {2: [_issue(1, "closed")]},
        )
    )
    code, regels = ud.run(gh, 1, label=LABEL)
    assert code == 0
    assert ("api", "-X", "DELETE", f"{R}/issues/2/labels/{LABEL}") in gh.writes
    comments = _comments(gh)
    assert "#1" in comments[2] and LABEL in comments[2]
    assert "#2 is released" in comments[1]
    assert any("#2: released" in r for r in regels)


def test_nog_een_open_blocker_houdt_het_label_en_meldt_het_terug() -> None:
    gh = FakeGh(
        _wereld(
            _issue(1, "closed"),
            [_issue(2, labels=[LABEL])],
            {2: [_issue(1, "closed"), _issue(7)]},
        )
    )
    code, _ = ud.run(gh, 1, label=LABEL)
    assert code == 0
    assert not any(w[2] == "DELETE" for w in gh.writes)
    assert _comments(gh) == {
        1: "Released lane after this close:\n\n- #2 still waits for #7"
    }


def test_zonder_label_niet_gelezen_en_geen_schrijfactie() -> None:
    """Geen blocked_by in de wereld: een read zou KeyError geven. Een herstart is dus stil."""
    gh = FakeGh(_wereld(_issue(1, "closed"), [_issue(2)], {}))
    code, regels = ud.run(gh, 1, label=LABEL)
    assert code == 0
    assert gh.writes == []
    assert any("does not carry" in r for r in regels)


def test_eerst_comment_dan_label() -> None:
    gh = FakeGh(
        _wereld(
            _issue(1, "closed"),
            [_issue(2, labels=[LABEL])],
            {2: [_issue(1, "closed")]},
        )
    )
    ud.run(gh, 1, label=LABEL)
    volgorde = [(w[2], w[3].split("/issues/")[1].split("/")[0]) for w in gh.writes]
    assert volgorde == [("POST", "1"), ("POST", "2"), ("DELETE", "2")]


def test_falende_terugcomment_raakt_niets_aan_en_herstart_herhaalt_alles() -> None:
    reads = _wereld(
        _issue(1, "closed"), [_issue(2, labels=[LABEL])], {2: [_issue(1, "closed")]}
    )
    gh = FakeGh(reads, faalt={f"{R}/issues/1/comments"})
    code, regels = ud.run(gh, 1, label=LABEL)
    assert code == 1
    assert gh.writes == []
    assert regels[-1].endswith("nothing touched")
    herstart = FakeGh(reads)
    assert ud.run(herstart, 1, label=LABEL)[0] == 0
    assert set(_comments(herstart)) == {1, 2}


def test_falende_comment_laat_het_label_staan() -> None:
    reads = _wereld(
        _issue(1, "closed"), [_issue(2, labels=[LABEL])], {2: [_issue(1, "closed")]}
    )
    gh = FakeGh(reads, faalt={f"{R}/issues/2/comments"})
    code, regels = ud.run(gh, 1, label=LABEL)
    assert code == 1
    assert not any(w[2] == "DELETE" for w in gh.writes)
    assert any("#2: **Error:**" in r for r in regels)


def test_label_404_is_al_weg() -> None:
    reads = _wereld(
        _issue(1, "closed"), [_issue(2, labels=[LABEL])], {2: [_issue(1, "closed")]}
    )

    class Gh404(FakeGh):
        def __call__(self, args: Sequence[str]) -> str:
            if "DELETE" in args:
                raise subprocess.CalledProcessError(
                    1, ["gh"], stderr="HTTP 404: Label does not exist\n"
                )
            return super().__call__(args)

    gh = Gh404(reads)
    code, regels = ud.run(gh, 1, label=LABEL)
    assert code == 0
    assert any("#2: released" in r for r in regels)


@pytest.mark.parametrize("reden", ["not_planned", "duplicate"])
def test_niet_completed_geeft_niets_vrij(reden: str) -> None:
    gh = FakeGh(_wereld(_issue(1, "closed", state_reason=reden), [], {}))
    code, regels = ud.run(gh, 1, label=LABEL)
    assert code == 0
    assert gh.writes == []
    assert reden in regels[-1]


def test_open_issue_geeft_niets_vrij() -> None:
    gh = FakeGh(_wereld(_issue(1), [], {}))
    assert ud.run(gh, 1, label=LABEL) == (
        0,
        ["## Release lane for #1", "", "#1 is open: nothing to release."],
    )
    assert gh.writes == []


def test_gesloten_afhankelijke_telt_niet_mee() -> None:
    gh = FakeGh(
        _wereld(_issue(1, "closed"), [_issue(2, "closed", labels=[LABEL])], {})
    )
    code, regels = ud.run(gh, 1, label=LABEL)
    assert code == 0
    assert gh.writes == []
    assert regels[-1] == "#1 blocked no open issues."


def test_onleesbare_blockers_is_rood_en_laat_het_label_staan() -> None:
    reads = _wereld(
        _issue(1, "closed"),
        [_issue(2, labels=[LABEL]), _issue(3, labels=[LABEL])],
        {3: [_issue(1, "closed")]},
    )
    gh = FakeGh(reads, faalt={f"{R}/issues/2/dependencies/blocked_by?per_page=100"})
    code, regels = ud.run(gh, 1, label=LABEL)
    assert code == 1
    assert ("api", "-X", "DELETE", f"{R}/issues/2/labels/{LABEL}") not in gh.writes
    assert ("api", "-X", "DELETE", f"{R}/issues/3/labels/{LABEL}") in gh.writes
    assert any("#2: **Error:** HTTP 403: nee" in r for r in regels)


def test_gesloten_issue_onleesbaar_is_rood() -> None:
    gh = FakeGh({}, faalt={f"{R}/issues/1"})
    code, regels = ud.run(gh, 1, label=LABEL)
    assert code == 1
    assert regels[-1] == "**Error:** #1 not read: HTTP 403: nee"


def test_dry_run_schrijft_niets() -> None:
    gh = FakeGh(
        _wereld(
            _issue(1, "closed"),
            [_issue(2, labels=[LABEL])],
            {2: [_issue(1, "closed")]},
        )
    )
    code, regels = ud.run(gh, 1, label=LABEL, dry_run=True)
    assert code == 0
    assert gh.writes == []
    assert regels[-1] == "_dry-run: nothing written._"


def test_release_line_token() -> None:
    """`close-issue` reads the reply on the closed issue by this fixed line."""
    gh = FakeGh(
        _wereld(_issue(1, "closed"), [_issue(2, labels=[LABEL])], {2: [_issue(1, "closed")]})
    )
    ud.run(gh, 1, label=LABEL)
    assert _comments(gh)[1].splitlines()[0] == "Released lane after this close:"
    assert ud.RELEASE_LINE == "Released lane after this close:"


def test_blocked_label_from_config(tmp_path, monkeypatch) -> None:
    config = tmp_path / "lighthouse.toml"
    config.write_text('[labels]\nblocked_issue = "x"\n', encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(config))
    assert ud.blocked_label() == "x"
    gh = FakeGh(_wereld(_issue(1, "closed"), [_issue(2, labels=["x"])], {2: [_issue(1, "closed")]}))
    monkeypatch.setattr(ud, "run_gh", gh)
    assert ud.main(["--issue", "1"]) == 0
    assert ("api", "-X", "DELETE", f"{R}/issues/2/labels/x") in gh.writes


def test_blocked_label_default(tmp_path, monkeypatch) -> None:
    config = tmp_path / "lighthouse.toml"
    config.write_text('[project]\nlanguage = "en"\n', encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(config))
    assert ud.blocked_label() == "blocked-issue"


def test_broken_config_is_rc_2(tmp_path, monkeypatch, capsys) -> None:
    config = tmp_path / "lighthouse.toml"
    config.write_text("[labels]\nblocked_issue = 3\n", encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(config))
    assert ud.main(["--issue", "1"]) == 2
    assert "labels.blocked_issue" in capsys.readouterr().err


def test_help_is_english(capsys) -> None:
    with pytest.raises(SystemExit):
        ud.main(["--help"])
    out = capsys.readouterr().out
    assert "Release the issues that waited on a just-closed issue" in out
    assert "Zet issues vrij" not in out


def test_label_with_a_space_is_url_encoded(tmp_path, monkeypatch) -> None:
    """The label is configurable now; a space or a slash must not break the DELETE path."""
    config = tmp_path / "lighthouse.toml"
    config.write_text('[labels]\nblocked_issue = "blocked / issue"\n', encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(config))
    label = "blocked / issue"
    gh = FakeGh(_wereld(_issue(1, "closed"), [_issue(2, labels=[label])], {2: [_issue(1, "closed")]}))
    monkeypatch.setattr(ud, "run_gh", gh)
    assert ud.main(["--issue", "1"]) == 0
    assert ("api", "-X", "DELETE", f"{R}/issues/2/labels/blocked%20%2F%20issue") in gh.writes
