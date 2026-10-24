"""The skill `refresh-expo-lockfiles` of `pack/expo`: it finds the Expo apps itself,
guards what a project may not have, and links only to lighthouse skills."""

import re
import subprocess
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
    for ref in ("dependabot.yml", "expo-upgrade", ".github/workflows"):
        assert ref in text, ref
    for ref in ("apps.yml", "dependabot.yml", "expo-upgrade", ".github/workflows"):
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


def _bash_blocks() -> list[str]:
    return re.findall(r"```bash\n(.*?)```", _text(), re.S)


def test_diff_checked_before_commit():
    text = _text()
    check = text.index("Check that the diff is what you think")
    commit = re.search(r"^### \d+\. Commit$", text, re.M).start()
    assert check < commit, "the diff check runs after the commit, so it sees an empty diff"


def test_cd_in_subshell():
    for block in _bash_blocks():
        for line in block.splitlines():
            assert not line.lstrip().startswith("cd "), f"cd outside a subshell: {line}"


def test_app_loop_finds_only_apps_with_a_lockfile(tmp_path):
    loop = next(b for b in _bash_blocks() if "git ls-files" in b)
    git = ["git", "-C", str(tmp_path)]
    subprocess.run([*git, "init", "-q"], check=True)
    files = {
        "package.json": '{"keywords": ["expo"]}\n',
        "apps/a/package.json": '{"dependencies": {"expo": "~1.0.0"}}\n',
        "apps/a/package-lock.json": "{}\n",
        "lib/b/package.json": '{"peerDependencies": {"expo": "*"}}\n',
        "web/package.json": '{"dependencies": {"react": "1"}}\n',
        "web/package-lock.json": "{}\n",
    }
    for rel, content in files.items():
        (tmp_path / rel).parent.mkdir(parents=True, exist_ok=True)
        (tmp_path / rel).write_text(content)
    subprocess.run([*git, "add", "-A"], check=True)
    out = subprocess.run(
        ["bash", "-c", loop], cwd=tmp_path, capture_output=True, text=True, check=True
    ).stdout
    assert out.split() == ["apps/a"]
