"""`lighthouse_lib.headings`: the heading table `headings.toml` (besluit 8).

A heading that code or a skill reads as a contract has one variant per language.
Writing uses `[project] language`; reading accepts every variant.
"""

import os
import re
import subprocess
import sys
import tomllib

import pytest

from lighthouse_lib import headings
from lighthouse_lib.config import LANGUAGES, ConfigError
from tests.pack._testkit import CORE, INSTALL_ROOT, init_repo

TABLE = CORE / "headings.toml"

# The `nl` values, literally as the source repository writes them (plan, taak 7).
NL = {
    "acceptance_criteria": "Acceptatiecriteria",
    "kind_handoff": "Handoff",
    "kind_started": "Gestart",
    "kind_question": "Vraag",
    "kind_resumed": "Hervat",
    "kind_done": "Klaar",
    "kind_returned": "Teruggegeven",
    "kind_rebased": "Herbaseerd",
    "question_where": "Waar het staat",
    "question_what": "Wat er speelt",
    "question_label": "Vraag",
    "question_close": "**Max 1 comment als reactie.**",
    "question_bundle_hint": "Antwoord per vraag, bijvoorbeeld `1a, 2c`.",
    "question_run_yourself": "**Zelf draaien:**",
    "question_recommended": "*(aanbevolen)*",
    "question_phase": "fase",
    "question_task": "taak",
    "artefact_version": "versie",
    "artefact_part": "deel",
    "artefact_source": "bron",
    "gate_line": "Poort",
    "scout_skipped": "Scout: overgeslagen",
    "impact_pack": "IMPACT PACK",
    "evidence_holds": "klopt",
    "evidence_fails": "klopt niet",
    "evidence_invalid": "ongeldig",
    "evidence_unmeasurable": "niet meetbaar",
    "evidence_after_pr": "na PR: ",
    "evidence_after_pr_unmeasured": "na PR, niet gemeten",
    "evidence_table_head": "| AC | Uitkomst | Detail |",
    "evidence_closes_without_block": "Closes zonder bewijsblok:",
    "open_after_review": "## Open na review",
    "milestone_dod": "Definition of Done",
    "milestone_no_gos": "No-Gos",
    "milestone_not_yet_decided": "Nog niet vastgelegd",
}

# The same word in another role (plan, taak 7).
ALLOWED_SHARED = {frozenset({"question_label", "kind_question"})}


def _table() -> dict:
    with TABLE.open("rb") as handle:
        return tomllib.load(handle)


@pytest.fixture(autouse=True)
def _pack_table(monkeypatch):
    monkeypatch.setenv("LIGHTHOUSE_HEADINGS", str(TABLE))


@pytest.fixture
def language(tmp_path, monkeypatch):
    def set_language(value: str) -> None:
        path = tmp_path / "lighthouse.toml"
        path.write_text(f'[project]\nlanguage = "{value}"\n', encoding="utf-8")
        monkeypatch.setenv("LIGHTHOUSE_TOML", str(path))

    return set_language


def test_every_key_has_nl_and_en():
    for key, variants in _table().items():
        assert set(variants) == set(LANGUAGES), key
        for value in variants.values():
            assert isinstance(value, str) and value.strip(), key


def test_nl_values_are_the_source_literals():
    table = _table()
    assert {key: table[key]["nl"] for key in NL} == NL


def test_variants_nl_first():
    assert headings.variants("kind_started") == ("Gestart", "Started")


def test_variants_without_duplicates():
    assert headings.variants("kind_handoff") == ("Handoff",)


@pytest.mark.parametrize("value", ["nl", "en"])
def test_heading_follows_project_language(language, value):
    language(value)
    assert headings.heading("kind_started") == _table()["kind_started"][value]


def test_heading_explicit_language_wins(language):
    language("nl")
    assert headings.heading("kind_started", "en") == _table()["kind_started"]["en"]


def test_any_of_escapes():
    pattern = re.compile(headings.any_of("question_close"))
    assert pattern.fullmatch("**Max 1 comment als reactie.**")
    assert pattern.fullmatch(_table()["question_close"]["en"])
    assert not pattern.fullmatch("xxMax 1 comment als reactie.xx")


def test_any_of_is_one_group():
    pattern = re.compile("^### " + headings.any_of("acceptance_criteria") + "$")
    assert pattern.match("### Acceptatiecriteria")
    assert pattern.match("### Acceptance criteria")
    assert not pattern.match("Acceptance criteria")


def test_unknown_key_raises():
    with pytest.raises(KeyError):
        headings.variants("no_such_key")


def test_no_cross_key_collision():
    seen: dict[str, str] = {}
    for key, variants in _table().items():
        for value in set(variants.values()):
            if value in seen:
                assert frozenset({key, seen[value]}) in ALLOWED_SHARED, (value, key, seen[value])
            seen[value] = key


def test_path_defaults_to_repo_root(monkeypatch, tmp_path):
    monkeypatch.delenv("LIGHTHOUSE_HEADINGS")
    monkeypatch.setenv("LIGHTHOUSE_REPO_ROOT", str(tmp_path))
    assert headings.path() == tmp_path / ".lighthouse/headings.toml"


@pytest.mark.parametrize(
    "text",
    ["[kind_started\n", '[kind_started]\nnl = "Gestart"\n', 'kind_started = "Gestart"\n'],
    ids=["invalid-toml", "missing-en", "not-a-table"],
)
def test_broken_table_is_config_error(tmp_path, monkeypatch, text):
    path = tmp_path / "headings.toml"
    path.write_text(text, encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_HEADINGS", str(path))
    with pytest.raises(ConfigError, match=re.escape(str(path))):
        headings.variants("kind_started")


def test_missing_table_is_config_error(tmp_path, monkeypatch):
    monkeypatch.setenv("LIGHTHOUSE_HEADINGS", str(tmp_path / "headings.toml"))
    with pytest.raises(ConfigError, match="headings.toml"):
        headings.variants("kind_started")


def _cli(args, tmp_path, **extra):
    other = tmp_path / "other"
    init_repo(other)
    lh = tmp_path / "lighthouse.toml"
    lh.write_text('[project]\nlanguage = "nl"\n', encoding="utf-8")
    env = {k: v for k, v in os.environ.items() if not k.startswith(("PYTHON", "LIGHTHOUSE_"))}
    env.update(LIGHTHOUSE_REPO_ROOT=str(INSTALL_ROOT), LIGHTHOUSE_TOML=str(lh))
    env.update(extra)
    return subprocess.run(
        [sys.executable, str(INSTALL_ROOT / ".lighthouse/lib/lighthouse_lib/headings.py"), *args],
        capture_output=True,
        text=True,
        cwd=str(other),
        env=env,
        check=False,
    )


def test_cli_prints_heading_as_file(tmp_path):
    result = _cli(["kind_started"], tmp_path)
    assert (result.returncode, result.stdout) == (0, "Gestart\n"), result.stderr


def test_unknown_key_cli_rc_2(tmp_path):
    result = _cli(["no_such_key"], tmp_path)
    assert result.returncode == 2
    assert "no_such_key" in result.stderr
    assert "Traceback" not in result.stderr


def test_broken_config_cli_rc_2(tmp_path):
    broken = tmp_path / "broken.toml"
    broken.write_text("[project\n", encoding="utf-8")
    result = _cli(["kind_started"], tmp_path, LIGHTHOUSE_TOML=str(broken))
    assert result.returncode == 2
    assert "Traceback" not in result.stderr
