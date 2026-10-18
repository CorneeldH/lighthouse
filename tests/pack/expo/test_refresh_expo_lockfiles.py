"""The skill `refresh-expo-lockfiles` of `pack/expo`: it finds the Expo apps itself,
guards what a project may not have, and links only to lighthouse skills."""

import re
from pathlib import Path

from lighthouse_lib import config

SKILL = (
    Path(__file__).resolve().parents[3]
    / "pack"
    / "expo"
    / "skills"
    / "refresh-expo-lockfiles"
    / "SKILL.md"
)


def _text() -> str:
    return SKILL.read_text()


def _paragraphs() -> list[str]:
    return [p for p in re.split(r"\n\s*\n", _text()) if p.strip()]


def test_frontmatter_name():
    assert _text().startswith("---\nname: lighthouse-refresh-expo-lockfiles\n")


def test_finds_apps_itself():
    text = _text()
    for name in ("dictation-app", "photo-offload-app", "page-capture-app", "apps/"):
        assert name not in text, name
    assert "git ls-files" in text
    assert "package.json" in text
    assert "'\"expo\"'" in text


def test_optional_refs_guarded():
    text = _text()
    for ref in ("dependabot.yml", "upgrading-expo", ".github/workflows"):
        assert ref in text, ref
    for ref in ("apps.yml", "dependabot.yml", "upgrading-expo", ".github/workflows"):
        for p in _paragraphs():
            if ref in p:
                assert "if present" in p, f"{ref} without 'if present':\n{p}"


def test_write_issue_link():
    text = _text()
    assert "/lighthouse-write-issue" in text
    assert "python3 .lighthouse/lib/lighthouse_lib/config.py labels.area_prefix" in text
    assert config.is_known_key("labels.area_prefix")
    assert "2-area:" not in text


def test_skill_links_are_lighthouse_or_external():
    for link in re.findall(r"(?<![\w.])/([a-z][\w-]*)", _text()):
        assert not link.startswith("paia-"), link
        assert not link.startswith("caveman-"), link


def test_checks_node_and_npm():
    text = _text()
    assert "command -v node" in text
    assert "command -v npm" in text


def test_english():
    text = _text().lower()
    for word in (" het ", " niet ", " een ", " wordt ", " dat ", " je "):
        assert word not in text, word


def test_no_paia_measurements():
    text = _text()
    for literal in ("2026-08-22", "18/18", "54.0.37", "SDK 54", "dictation", "rumps"):
        assert literal not in text, literal
