"""docs/site/agents/install.md stays in step with the repo: flags, counts, tools, languages."""

import ast
import re
import tomllib

import pytest

from tests.test_getting_started import init_flags, required_tools, section
from tests.test_site_chain import ROOT, SITE
from tests.test_site_download import DOWNLOAD, INDEX, INSTALL, TABS
from tests.test_site_pages import ElementById

GUIDE = SITE / "agents" / "install.md"
PASTE = "Read https://corneeldh.github.io/lighthouse/agents/install.md and install lighthouse in this repo."
SITE_URL = "https://corneeldh.github.io/lighthouse/"

HEADINGS = [
    "What this is", "Ground rules", "Asking the user", "Look first", "Choices",
    "Install the binary", "Run lighthouse init", "Check", "Final report", "When something fails",
]


def guide_text() -> str:
    return GUIDE.read_text(encoding="utf-8")


def guide_section(title: str) -> str:
    """The text between `## <title>` and the next `## ` heading of the agent guide."""
    match = re.search(rf"^## {re.escape(title)}\n(.*?)(?=^## |\Z)", guide_text(), re.M | re.S)
    assert match, f"docs/site/agents/install.md lacks the heading '## {title}'"
    return match.group(1)


FENCE = r"^[ \t]*```[^\n]*\n(.*?)^[ \t]*```"


def fenced_blocks(text: str) -> list[str]:
    """The fenced blocks of `text`, also those indented under a list item."""
    return re.findall(FENCE, text, re.M | re.S)


def code_spans(text: str) -> list[str]:
    """The inline backtick spans of `text`, outside its fenced blocks."""
    outside = re.sub(FENCE, "", text, flags=re.M | re.S)
    return re.findall(r"`([^`\n]+)`", outside)


def test_headings_in_order():
    assert re.findall(r"^## (.+)$", guide_text(), re.M) == HEADINGS


def test_paste_url_is_this_file():
    url = re.search(r"https://\S+\.md", PASTE).group(0)
    assert url.removeprefix(SITE_URL) == GUIDE.relative_to(SITE).as_posix()


def test_init_flags_exist():
    """Every `--flag` in code outside the download blocks is a flag of `lighthouse init`;
    `--help` is the one cobra adds itself."""
    text = re.sub(r"^## Install the binary\n.*?(?=^## )", "", guide_text(), flags=re.M | re.S)
    code = code_spans(text) + fenced_blocks(text)
    used = {flag for chunk in code for flag in re.findall(r"(?<![\w-])--(\w[\w-]*)", chunk)}
    assert {"pack", "no-pack", "upgrade"} <= used
    known = init_flags() | {"help"}
    assert used <= known, f"guide names unknown init flags {sorted(used - known)}"


def test_approval_goes_through_stdin():
    assert "printf 'y\\n' | lighthouse init" in guide_section("Run lighthouse init")
    with_yes = [block for block in fenced_blocks(guide_text()) if "--yes" in block]
    assert not with_yes, f"a code block approves with --yes: {with_yes}"


def test_no_changes_is_what_init_prints():
    assert "No changes." in guide_section("Check")
    src = (ROOT / "internal/initcmd/initcmd.go").read_text(encoding="utf-8")
    assert 'Fprintln(o.Out, "No changes.")' in src


def test_help_rule():
    rules = guide_section("Ground rules")
    assert "lighthouse init --help" in rules and "sudo" in rules
    with_sudo = [block for block in fenced_blocks(guide_text()) if "sudo" in block]
    assert not with_sudo, f"a code block uses sudo: {with_sudo}"


def test_superpowers_count():
    needs = tomllib.loads((ROOT / "pack/core/requires.toml").read_text(encoding="utf-8"))
    count = len(needs["superpowers"])
    named = re.findall(r"Without Superpowers, (\d+) lighthouse skills", guide_section("Choices"))
    assert named == [str(count)], (
        f"Choices names {named} skills without Superpowers; pack/core/requires.toml has {count}"
    )


def test_every_required_tool_is_named():
    named = set(code_spans(guide_section("Look first")))
    missing = sorted(required_tools() - named)
    assert not missing, f"Look first lacks {missing}"


def config_languages() -> set[str]:
    tree = ast.parse((ROOT / "pack/core/lib/config.py").read_text(encoding="utf-8"))
    for node in tree.body:
        if isinstance(node, ast.Assign) and any(
            isinstance(t, ast.Name) and t.id == "LANGUAGES" for t in node.targets
        ):
            return set(ast.literal_eval(node.value))
    raise AssertionError("pack/core/lib/config.py has no LANGUAGES")


def test_languages_are_the_config_languages():
    lines = [line for line in guide_section("Choices").splitlines() if "`[project] language`" in line]
    assert len(lines) == 1, f"want one Choices line on `[project] language`, got {len(lines)}"
    offered = set(re.findall(r"`([a-z]{2})`", lines[0]))
    assert offered == config_languages()
    assert "any other language" in lines[0]


def test_harness_notes():
    text = guide_text()
    for needle in ["AskUserQuestion", ".agents/skills", "missing dependency: superpowers", "CODEX_MISSING"]:
        assert needle in text, f"guide lacks {needle!r}"


def test_stops_when_already_installed():
    text = guide_section("Look first")
    assert ".lighthouse/manifest.toml" in text and "stop" in text


def test_binary_blocks():
    text = guide_section("Install the binary")
    assets = [f"lighthouse_{tab.replace('-', '_')}" for tab in TABS if tab != "windows-amd64"]
    for needle in [DOWNLOAD + "SHA256SUMS", *(DOWNLOAD + a for a in assets)]:
        assert needle in text, f"Install the binary lacks {needle}"
    for needle in ["sha256sum --check", "shasum -a 256 -c", "WSL"]:
        assert needle in text, f"Install the binary lacks {needle!r}"


def test_final_report_fields():
    text = guide_section("Final report")
    for needle in ["lighthouse version", "init --upgrade", "/lighthouse-start-issue"]:
        assert needle in text, f"Final report lacks {needle!r}"


def test_paste_block_on_install_page():
    text = INSTALL.read_text(encoding="utf-8")
    block = ElementById("agent")
    block.feed(text)
    assert block.attrs is not None, "install.html has no element with id=agent"
    assert PASTE in block.text
    assert text.index('id="agent"') < text.index("data-platform")


def test_paste_block_has_copy_button():
    js = (SITE / "download.js").read_text(encoding="utf-8")
    assert re.search(r"addCopyButtons\(\[[^\]]*\.agent-prompt", js)


def test_guide_is_linked():
    for page in [INSTALL, INDEX]:
        text = page.read_text(encoding="utf-8")
        assert 'href="agents/install.md"' in text, page.name
        assert "issues/88" not in text, page.name


def test_getting_started_names_the_paste():
    text = section("Install")
    assert PASTE in text
    assert "](https://corneeldh.github.io/lighthouse/agents/install.md)" in text


def test_check_reads_stdout_only():
    """`--upgrade` prints notes on stderr on a healthy install; only stdout is `No changes.`."""
    text = guide_section("Check")
    assert "stdout" in text and "stderr" in text


def test_upgrade_notes_are_named():
    src = "".join(p.read_text(encoding="utf-8") for p in (ROOT / "internal/initcmd").glob("*.go"))
    text = guide_section("When something fails")
    for note in ["note: host.toml is missing", "fits this repository (found "]:
        assert note in src, f"init no longer prints {note!r}"
        assert note in text, f"When something fails lacks {note!r}"


def test_packages_are_asked_after_the_dry_run():
    """Choice 6 needs the `Packages: …` line, so the dry run comes before it."""
    text = guide_section("Run lighthouse init")
    assert text.index("</dev/null") < text.index("choice 6")


CLAUDE_MD_WARNING = "warning: CLAUDE.md does not import AGENTS.md"


def test_claude_md_import_is_asked_not_left_open():
    """`init` never touches an existing `CLAUDE.md`, so the agent asks for the
    `@AGENTS.md` line itself and goes on to Check, instead of stopping with it open."""
    src = (ROOT / "internal/initcmd/initcmd.go").read_text(encoding="utf-8")
    assert CLAUDE_MD_WARNING in src, f"init no longer prints {CLAUDE_MD_WARNING!r}"
    run = guide_section("Run lighthouse init")
    assert CLAUDE_MD_WARNING in run
    step = run[run.index(CLAUDE_MD_WARNING) :]
    assert "`@AGENTS.md`" in step and "recommended" in step
    assert step.index("`@AGENTS.md`") < step.index("[Check](#check)")
    assert "`@AGENTS.md`" in guide_section("Ground rules")


def test_stops_outside_a_git_repository():
    text = guide_section("Look first")
    assert "not a git repository" in text and "stop" in text


# Printed by cobra through `lighthouse: %v` in internal/cli/root.go, not by lighthouse itself.
COBRA_MESSAGES = {"unknown flag: --<name>"}
PREFIXES = {
    "lighthouse init: ": ("internal/initcmd/initcmd.go", '"lighthouse init: %v\\n"'),
    "lighthouse: ": ("internal/cli/root.go", '"lighthouse: %v\\n"'),
}


def failure_messages() -> list[str]:
    """The message that opens each item of When something fails, in its code span."""
    found = re.findall(r"^- (`+) ?(.*?) ?\1", guide_section("When something fails"), re.M)
    assert found, "When something fails quotes no message"
    return [message for _, message in found]


@pytest.mark.parametrize("message", failure_messages())
def test_failure_message_is_printed(message):
    """Each quoted message, placeholders aside, is text that lighthouse prints."""
    for prefix, (path, printf) in PREFIXES.items():
        if message.startswith(prefix):
            assert printf in (ROOT / path).read_text(encoding="utf-8"), f"{path} lacks {printf}"
            message = message.removeprefix(prefix)
            break
    if message in COBRA_MESSAGES:
        return
    sources = sorted(p for p in (ROOT / "internal").rglob("*.go") if not p.name.endswith("_test.go"))
    src = "".join(p.read_text(encoding="utf-8") for p in sources)
    pieces = [piece for piece in re.split(r"<[^>]*>|…", message) if piece.strip(" ()")]
    missing = [piece for piece in pieces if piece not in src]
    assert not missing, f"lighthouse prints none of {missing}"
