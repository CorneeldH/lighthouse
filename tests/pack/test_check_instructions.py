"""`pack/core/scripts/check-instructions.sh`: the generic checks over the instruction layer.

The script runs as a subprocess against temporary trees, so the contract
(exit code + stdout) is tested and not a reimplementation of it. It runs
from its installed place (`_testkit.INSTALL_ROOT`), so it finds its config reader
in `.lighthouse/lib` as in a project. Checks 1, 2, 3, 4, 5, 7, 8, 10 and
11 carry their number from the source; 6, 9, 12 and 13 are project-specific and are not in here.
"""

import os
import subprocess
from pathlib import Path

import pytest

from tests.pack._testkit import INSTALL_ROOT

REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT = INSTALL_ROOT / ".lighthouse" / "scripts" / "check-instructions.sh"


def _skill(
    root: Path,
    name: str,
    description: str = "Doet iets nuttigs.",
    metadata: dict | None = None,
    body: str | None = None,
    **fields,
):
    """Write a minimal SKILL.md.

    `fields` goes into the frontmatter top-level unchanged (spec and vendor fields).
    `metadata` nests under a `metadata:` block, where binding and the
    lighthouse marker belong, per the Agent Skills spec. `body` is the text
    after the frontmatter (default the old bare heading); check 8 greps it for a
    hook path.
    """
    directory = root / name
    directory.mkdir(parents=True, exist_ok=True)
    lines = [f"name: {name}", f"description: {description}"]
    lines += [f"{key.replace('_', '-')}: {value}" for key, value in fields.items()]
    if metadata:
        lines.append("metadata:")
        lines += [
            f"  {key.replace('_', '-')}: {value}"
            for key, value in metadata.items()
        ]
    if body is None:
        body = "# " + name + "\n"
    content = "---\n" + "\n".join(lines) + "\n---\n\n" + body
    (directory / "SKILL.md").write_text(content, encoding="utf-8")
    return directory


def _tree(tmp_path: Path) -> tuple[Path, Path, Path]:
    """An empty repo tree plus an empty personal directory. All three exist."""
    claude = tmp_path / "repo" / ".claude" / "skills"
    agents = tmp_path / "repo" / ".agents" / "skills"
    personal = tmp_path / "personal" / "skills"
    for directory in (claude, agents, personal):
        directory.mkdir(parents=True)
    return claude, agents, personal


def _ceiling(n: int) -> str:
    return f"[project]\ninstructions_word_ceiling = {n}\n"


def _env(tmp_path: Path, toml: str) -> dict:
    """The environment of one run: the repo tree as root, and a `lighthouse.toml` of its own
    outside that tree, so a test never reads the config of the machine."""
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    config = tmp_path / "lighthouse.toml"
    config.write_text(toml, encoding="utf-8")
    return dict(os.environ, LIGHTHOUSE_REPO_ROOT=str(repo), LIGHTHOUSE_TOML=str(config))


def _run(
    tmp_path: Path,
    personal: Path | None = None,
    extra_env: dict | None = None,
    toml: str = "",
    args: tuple[str, ...] = (),
):
    env = _env(tmp_path, toml)
    env["CHECK_INSTRUCTIONS_PERSONAL"] = str(
        personal if personal is not None else tmp_path / "personal" / "skills"
    )
    if extra_env:
        env.update(extra_env)
    # `timeout`: the script is deterministic and by default runs here in
    # a few seconds. A hang should therefore show up as a test failure,
    # not as a test run that waits forever.
    return subprocess.run(
        [str(SCRIPT), *args],
        capture_output=True,
        text=True,
        env=env,
        check=False,
        timeout=30,
    )


def _path_without_python3(tmp_path: Path) -> str:
    """A PATH that contains everything the script needs, except python3.

    No `command -v python3` shadowing with an empty stub: `command -v` keeps
    searching the next PATH directory as soon as an entry is not executable, so
    a stub does not reliably remove python3. Instead a
    complete replacement bin directory with symlinks to everything that IS on the real
    PATH, except every `python3*` name.
    """
    bindir = tmp_path / "bin-zonder-python3"
    bindir.mkdir(exist_ok=True)
    seen: set[str] = set()
    for directory in os.environ.get("PATH", "").split(os.pathsep):
        if not directory or not os.path.isdir(directory):
            continue
        try:
            entries = os.listdir(directory)
        except OSError:
            continue
        for entry in entries:
            if entry in seen or entry.startswith("python3"):
                continue
            src = os.path.join(directory, entry)
            if not os.path.isfile(src) or not os.access(src, os.X_OK):
                continue
            seen.add(entry)
            try:
                (bindir / entry).symlink_to(src)
            except OSError:
                continue
    return str(bindir)


# --- identity: duplicate name, byte-identical pair, mirror ------------------


def test_empty_tree_is_green(tmp_path):
    _tree(tmp_path)
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "FAIL" not in result.stdout


def test_duplicate_name_fails(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-een", description="Eerste ding.")
    _skill(agents, "paia-een", description="Eerste ding.")
    second = _skill(claude, "paia-twee", description="Tweede heel ander ding.")
    _skill(agents, "paia-twee", description="Tweede heel ander ding.")
    text = (second / "SKILL.md").read_text(encoding="utf-8")
    (second / "SKILL.md").write_text(
        text.replace("name: paia-twee", "name: paia-een"), encoding="utf-8"
    )
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "duplicate name" in result.stdout
    assert "paia-een" in result.stdout


def test_byte_identical_pair_fails(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    # Same content, two directory names -- the `generate-slides-retro` case.
    for name in ("paia-retro", "paia_retro"):
        for skill_root in (claude, agents):
            directory = skill_root / name
            directory.mkdir()
            (directory / "SKILL.md").write_text(
                "---\nname: paia-retro-gedeeld\ndescription: Retro.\n---\n",
                encoding="utf-8",
            )
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "byte-identical" in result.stdout


def test_mirror_break_fails(tmp_path):
    claude, _agents, _ = _tree(tmp_path)
    _skill(claude, "paia-alleen-links")
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "mirror" in result.stdout
    assert "paia-alleen-links" in result.stdout


def test_mirror_break_also_the_other_way(tmp_path):
    _claude, agents, _ = _tree(tmp_path)
    _skill(agents, "paia-alleen-rechts")
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "paia-alleen-rechts" in result.stdout


def test_verbatim_mirror_is_not_a_duplicate(tmp_path):
    """`.claude/skills/x` and `.agents/skills/x` are a pair by design. For
    a vendored external skill the mirror is verbatim and thus literally
    identical; the duplicate check must never report that."""
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-gespiegeld")
    _skill(agents, "paia-gespiegeld")
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout
    assert "FAIL" not in result.stdout


# --- Check 11: mirror content (#981) ----------------------------------------
# Check 3 sees only directory names. This check compares the content of each pair;
# deliberate differences per harness are in the file of `[paths] mirror_exceptions`.

MIRROR_TOML = '[paths]\nmirror_exceptions = "mirror-exceptions.txt"\n'


def _exceptions_file(tmp_path: Path) -> Path:
    return tmp_path / "repo" / "mirror-exceptions.txt"


def _record_current_differences(tmp_path: Path) -> None:
    """Write the current differences away as the script prints them itself."""
    result = subprocess.run(
        [str(SCRIPT), "--print-mirror-exceptions"],
        capture_output=True,
        text=True,
        env=_env(tmp_path, MIRROR_TOML),
        check=True,
        timeout=30,
    )
    _exceptions_file(tmp_path).write_text(result.stdout, encoding="utf-8")


def test_equal_mirror_reports_content_equal(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-gelijk")
    _skill(agents, "paia-gelijk")
    result = _run(tmp_path, toml=MIRROR_TOML)
    assert result.returncode == 0, result.stdout
    assert "ok    mirror-content" in result.stdout


def test_one_sided_change_fails(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-scheef", body="# Nieuwe regel\n")
    _skill(agents, "paia-scheef", body="# Oude regel\n")
    result = _run(tmp_path, toml=MIRROR_TOML)
    assert result.returncode == 1, result.stdout
    assert "FAIL  mirror-content" in result.stdout
    assert "paia-scheef/SKILL.md" in result.stdout
    assert "Nieuwe regel" in result.stdout


def test_recorded_exception_is_green(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-harnas", body="Draai in Claude Code.\n")
    _skill(agents, "paia-harnas", body="Draai in Codex.\n")
    _record_current_differences(tmp_path)
    result = _run(tmp_path, toml=MIRROR_TOML)
    assert result.returncode == 0, result.stdout
    assert "ok    mirror-content" in result.stdout


def test_new_drift_next_to_exception_fails(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-harnas", body="Draai in Claude Code.\n")
    _skill(agents, "paia-harnas", body="Draai in Codex.\n")
    _record_current_differences(tmp_path)
    skill = claude / "paia-harnas" / "SKILL.md"
    skill.write_text(
        skill.read_text(encoding="utf-8") + "Alleen links.\n", encoding="utf-8"
    )
    result = _run(tmp_path, toml=MIRROR_TOML)
    assert result.returncode == 1, result.stdout
    assert "Alleen links." in result.stdout


def test_same_change_on_both_sides_stays_green(tmp_path):
    """The exceptions carry no line numbers: an equal edit on both
    sides shifts the differences, but does not change them."""
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-harnas", body="Draai in Claude Code.\n")
    _skill(agents, "paia-harnas", body="Draai in Codex.\n")
    _record_current_differences(tmp_path)
    for skill_root in (claude, agents):
        skill = skill_root / "paia-harnas" / "SKILL.md"
        # Right after the closing `---`: if the line is before the frontmatter, then
        # `skill_meta` (awk demands `---` on line 1) no longer sees a field.
        skill.write_text(
            skill.read_text(encoding="utf-8").replace(
                "\n---\n", "\n---\nBovenaan erbij.\n", 1
            ),
            encoding="utf-8",
        )
    result = _run(tmp_path, toml=MIRROR_TOML)
    assert result.returncode == 0, result.stdout
    assert "ok    mirror-content" in result.stdout


def test_extra_file_on_one_side_fails(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-bijlage")
    _skill(agents, "paia-bijlage")
    (claude / "paia-bijlage" / "referentie.md").write_text(
        "Alleen links.\n", encoding="utf-8"
    )
    result = _run(tmp_path, toml=MIRROR_TOML)
    assert result.returncode == 1, result.stdout
    assert "paia-bijlage/referentie.md" in result.stdout


def test_stale_exception_also_fails(tmp_path):
    """A recorded difference that no longer exists must leave the file. Otherwise
    the exceptions file silently grows dead lines."""
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-harnas", body="Draai in Claude Code.\n")
    _skill(agents, "paia-harnas", body="Draai in Codex.\n")
    _record_current_differences(tmp_path)
    skill = agents / "paia-harnas" / "SKILL.md"
    skill.write_text(
        skill.read_text(encoding="utf-8").replace("Codex", "Claude Code"),
        encoding="utf-8",
    )
    result = _run(tmp_path, toml=MIRROR_TOML)
    assert result.returncode == 1, result.stdout
    assert "FAIL  mirror-content" in result.stdout


def test_unreadable_file_is_not_measured_and_not_equal(tmp_path):
    """diff exit 2 is "could not compare". That must never read as "equal"."""
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-dicht")
    _skill(agents, "paia-dicht")
    closed = claude / "paia-dicht" / "SKILL.md"
    closed.chmod(0)
    try:
        if os.access(closed, os.R_OK):  # root reads everything; then there is nothing to test
            return
        result = _run(tmp_path, toml=MIRROR_TOML)
    finally:
        closed.chmod(0o644)
    assert "SKIPPED  mirror-content" in result.stdout, result.stdout
    assert "ok    mirror-content" not in result.stdout


def test_unreadable_skill_dir_is_not_measured_and_not_equal(tmp_path):
    """If find cannot read a directory, the file list is empty. Empty must never
    read as "no difference"."""
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-dicht")
    _skill(agents, "paia-dicht")
    dirs = [claude / "paia-dicht", agents / "paia-dicht"]
    for directory in dirs:
        directory.chmod(0)
    try:
        if os.access(
            dirs[0], os.R_OK
        ):  # root reads everything; then there is nothing to test
            return
        result = _run(tmp_path, toml=MIRROR_TOML)
    finally:
        for directory in dirs:
            directory.chmod(0o755)
    assert "SKIPPED  mirror-content" in result.stdout, result.stdout
    assert "ok    mirror-content" not in result.stdout


# --- overlap without a bounding clause --------------------------------------

BROAD = (
    "Gebruik bij het schrijven aanmaken bijwerken en opschonen van github "
    "issues labels milestones binnen deze repository."
)


def test_overlapping_descriptions_without_clause_fail(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    for name in ("paia-issue-een", "paia-issue-twee"):
        _skill(claude, name, description=BROAD)
        _skill(agents, name, description=BROAD)
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "overlap" in result.stdout
    assert "paia-issue-een" in result.stdout


def test_bounding_clause_makes_overlap_acceptable(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-issue-een", description=BROAD)
    _skill(agents, "paia-issue-een", description=BROAD)
    bounded = BROAD + " LET OP - gaat het om sluiten, gebruik paia-issue-een."
    _skill(claude, "paia-issue-twee", description=bounded)
    _skill(agents, "paia-issue-twee", description=bounded)
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout
    assert "FAIL" not in result.stdout


def test_scope_word_without_other_skill_does_not_count_as_clause(tmp_path):
    """A widening phrase enlarges the trigger instead -- no free pass."""
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-issue-een", description=BROAD)
    _skill(agents, "paia-issue-een", description=BROAD)
    weak = BROAD + " LET OP - twijfel je, gebruik dan deze skill."
    _skill(claude, "paia-issue-twee", description=weak)
    _skill(agents, "paia-issue-twee", description=weak)
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "overlap" in result.stdout


def test_english_not_for_clause_makes_overlap_acceptable(tmp_path):
    """The form of upstream `eas-hosting`: an external skill bounds itself in English."""
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-issue-een", description=BROAD)
    _skill(agents, "paia-issue-een", description=BROAD)
    bounded = BROAD + " Not for closing - use the paia-issue-een skill for those."
    _skill(claude, "paia-issue-twee", description=bounded)
    _skill(agents, "paia-issue-twee", description=bounded)
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout
    assert "FAIL" not in result.stdout


def test_not_for_inside_a_longer_word_does_not_count(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-issue-een", description=BROAD)
    _skill(agents, "paia-issue-een", description=BROAD)
    bogus = BROAD + " You cannot format paia-issue-een."
    _skill(claude, "paia-issue-twee", description=bogus)
    _skill(agents, "paia-issue-twee", description=bogus)
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "overlap" in result.stdout


def test_scope_word_inside_a_longer_word_does_not_count(tmp_path):
    """The `buiten`-in-`buitenwereld` case from CEDA's validator."""
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-issue-een", description=BROAD)
    _skill(agents, "paia-issue-een", description=BROAD)
    bogus = BROAD + " Dit raakt ook de buitenwereld van paia-issue-een."
    _skill(claude, "paia-issue-twee", description=bogus)
    _skill(agents, "paia-issue-twee", description=bogus)
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout


def _skill_with_folded_description(root: Path, name: str) -> None:
    """Writes a SKILL.md with `description: >` (YAML folded block scalar) instead of
    the flat `description: <text>` that `_skill()` writes --
    the same content as BROAD, only over two indented continuation lines."""
    directory = root / name
    directory.mkdir(parents=True, exist_ok=True)
    content = (
        "---\n"
        f"name: {name}\n"
        "description: >\n"
        "  Gebruik bij het schrijven aanmaken bijwerken en opschonen van github\n"
        "  issues labels milestones binnen deze repository.\n"
        "---\n\n"
        f"# {name}\n"
    )
    (directory / "SKILL.md").write_text(content, encoding="utf-8")


def test_folded_description_fails_like_flat_description(tmp_path):
    """Final fix round (#686): `skill_field()` matched `description: >` on the
    literal ">" of that same line and never read the indented continuation lines
    -- check 4 then compared against ">" instead of the real text and
    never found shared trigger words. Two skills that share ten words,
    one flat and one folded, must give the same FAIL."""
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-issue-een", description=BROAD)
    _skill(agents, "paia-issue-een", description=BROAD)
    _skill_with_folded_description(claude, "paia-issue-twee")
    _skill_with_folded_description(agents, "paia-issue-twee")
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "overlap" in result.stdout
    assert "paia-issue-een" in result.stdout and "paia-issue-twee" in result.stdout


# --- shadowing personal skill -----------------------------------------------


def test_shadowing_personal_skill_fails(tmp_path):
    claude, agents, personal = _tree(tmp_path)
    _skill(claude, "paia-dubbel")
    _skill(agents, "paia-dubbel")
    (personal / "paia-dubbel").mkdir()
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "shadow" in result.stdout
    assert "paia-dubbel" in result.stdout


def test_shadow_check_reports_it_cannot_measure(tmp_path):
    """A runner has no ~/.claude/skills/. Then SKIPPED, exit 0 --
    not silent, and not red."""
    _tree(tmp_path)
    result = _run(tmp_path, personal=tmp_path / "bestaat-niet")
    assert result.returncode == 0, result.stdout
    # On the check prefix, not on a bare "SKIPPED" plus "shadow":
    # pytest names tmp_path after the test function, so that path name is already in the
    # skip line of check_scoping and the test would test its own name.
    assert "SKIPPED  shadow:" in result.stdout
    assert "does not exist" in result.stdout



def _write_rule(root, name, front):
    """Put down a rule file with the given frontmatter."""
    rules = root / "repo" / ".claude" / "rules"
    rules.mkdir(parents=True, exist_ok=True)
    (rules / name).write_text(f"---\n{front}\n---\n\ntekst\n", encoding="utf-8")


def test_rule_without_paths_fails(tmp_path):
    _write_rule(tmp_path, "los.md", "description: iets")
    result = _run(tmp_path)
    assert result.returncode == 1
    assert "los.md" in result.stdout
    assert "paths:" in result.stdout


def test_rule_with_paths_is_green(tmp_path):
    _write_rule(tmp_path, "goed.md", "description: iets\npaths:\n  - src/**")
    result = _run(tmp_path)
    assert result.returncode == 0
    assert "every rule is path-scoped" in result.stdout


def test_no_rules_dir_is_skipped_not_reported_green(tmp_path):
    """The absence of a signal is not a negative result."""
    result = _run(tmp_path)
    assert result.returncode == 0
    # On the check prefix: see the note at the shadow check variant.
    assert "SKIPPED  scoping:" in result.stdout


def test_bare_paths_without_value_fails(tmp_path):
    """`paths:` without a list under it and without an inline value matches no
    path at all -- like `skill_field()`/`skill_meta()` elsewhere, this
    script treats an empty field as absent."""
    _write_rule(tmp_path, "kaal.md", "description: iets\npaths:")
    result = _run(tmp_path)
    assert result.returncode == 1
    assert "kaal.md" in result.stdout
    assert "paths:" in result.stdout


def test_empty_paths_list_fails(tmp_path):
    _write_rule(tmp_path, "leeg.md", "description: iets\npaths: []")
    result = _run(tmp_path)
    assert result.returncode == 1
    assert "leeg.md" in result.stdout


def test_paths_word_in_other_line_does_not_count_as_scoping(tmp_path):
    """Regression against `index($0, "paths:") == 1` -> `> 0`: the word
    `paths:` mid-line (here in a description) must not make the check
    think the rule carries a paths: field."""
    _write_rule(tmp_path, "vals.md", "description: zie ook paths: elders in de tekst")
    result = _run(tmp_path)
    assert result.returncode == 1
    assert "vals.md" in result.stdout


def test_paths_only_in_body_does_not_count_as_scoping(tmp_path):
    """Regression against dropping the frontmatter guard: `paths:` after the
    closing `---` (in the body) must not count."""
    rules = tmp_path / "repo" / ".claude" / "rules"
    rules.mkdir(parents=True, exist_ok=True)
    (rules / "vals2.md").write_text(
        "---\ndescription: iets\n---\n\npaths:\n  - src/**\n", encoding="utf-8"
    )
    result = _run(tmp_path)
    assert result.returncode == 1
    assert "vals2.md" in result.stdout
    assert "paths:" in result.stdout


def test_empty_line_before_paths_list_is_valid(tmp_path):
    """(E) An empty line between `paths:` and the list is valid YAML and must
    not be read as "no value"."""
    _write_rule(tmp_path, "leegregel.md", "description: iets\npaths:\n\n  - src/**")
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout
    assert "FAIL" not in result.stdout


def test_yaml_comment_before_paths_list_is_valid(tmp_path):
    """(E) A YAML comment line between `paths:` and the list is valid.
    Without this fix someone who puts an explanation in their `paths:` block
    would get "has no paths: block with a non-empty value" and start rebuilding
    flawless YAML to please the checker."""
    _write_rule(
        tmp_path,
        "comment.md",
        "description: iets\npaths:\n  # de twee mappen\n  - src/**",
    )
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout
    assert "FAIL" not in result.stdout


def test_paths_as_last_line_before_closing_still_fails(tmp_path):
    """(E) Edge case from the previous round must stay standing: `paths:` as the
    LAST line before the closing `---` (no list, no empty/comment
    line before it) is still "no value" and must give FAIL. Covers
    the same as `test_bare_paths_without_value_fails` once more, explicitly
    as a safety net for this round's fix to the getline loop."""
    rules = tmp_path / "repo" / ".claude" / "rules"
    rules.mkdir(parents=True, exist_ok=True)
    (rules / "randgeval.md").write_text(
        "---\ndescription: iets\npaths:\n---\n\ntekst\n", encoding="utf-8"
    )
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "randgeval.md" in result.stdout


#: Since #686 binding/origin/upstream are under `metadata:`, not top-level.
#: `skill_meta()` reads nothing else, so a top-level fixture tests nothing.
STRICT = {"binding": "hard", "origin": "own"}


def test_binding_hard_without_registered_hook_fails(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    for skill_root in (claude, agents):
        _skill(
            skill_root,
            "paia-streng",
            metadata=STRICT,
            body="Deze skill noemt geen enkele hook.\n",
        )
    result = _run(tmp_path)
    assert result.returncode == 1
    assert "paia-streng" in result.stdout
    assert "counterpart" in result.stdout


def test_binding_hard_with_registered_hook_is_green(tmp_path):
    repo = tmp_path / "repo"
    hooks = repo / ".claude" / "hooks"
    hooks.mkdir(parents=True, exist_ok=True)
    (hooks / "streng.sh").write_text("#!/usr/bin/env bash\nexit 0\n", encoding="utf-8")
    (repo / ".claude" / "settings.json").write_text(
        '{"hooks":{"PostToolUse":[{"matcher":"Edit",'
        '"hooks":[{"type":"command","command":"$CLAUDE_PROJECT_DIR/.claude/hooks/streng.sh"}]}]}}',
        encoding="utf-8",
    )
    claude, agents, _ = _tree(tmp_path)
    for skill_root in (claude, agents):
        _skill(
            skill_root,
            "paia-streng",
            metadata=STRICT,
            body="De afdwinging zit in `.claude/hooks/streng.sh`.\n",
        )
    result = _run(tmp_path)
    assert result.returncode == 0
    assert "every binding: hard has a registered hook" in result.stdout


def test_settings_missing_still_fails_on_skill_without_hook_path(tmp_path):
    """Regression: a `return 0` in the loop used to break off the whole check as soon as
    the first hard skill WITH a hook path met the missing settings.json,
    so that a later hard skill WITHOUT a hook path -- a finding that is
    measurable in itself -- was never looked at. `paia-aaa` (with hook path)
    comes alphabetically before `paia-zzz` (without); without settings.json
    `paia-zzz` must still fail.

    (D) At the same time `paia-aaa` itself must not silently disappear: it is
    unmeasurable (no settings.json) and must always yield a SKIPPED line,
    also next to the FAIL on `paia-zzz`. An `elif` on `found` used to suppress that line
    as soon as even one FAIL fell."""
    claude, agents, _ = _tree(tmp_path)
    for skill_root in (claude, agents):
        _skill(
            skill_root,
            "paia-aaa",
            metadata=STRICT,
            body="Zie `.claude/hooks/streng.sh`.\n",
        )
        _skill(
            skill_root,
            "paia-zzz",
            metadata=STRICT,
            body="Deze skill noemt geen enkele hook.\n",
        )
    result = _run(tmp_path)
    assert result.returncode == 1
    assert "paia-zzz" in result.stdout
    assert "counterpart" in result.stdout
    # Specifically the SKIPPED line of check_pairing, not just "SKIPPED"
    # somewhere in the output -- this fixture also has no `.claude/rules/`, so
    # check_scoping() prints its OWN (irrelevant) SKIPPED line,
    # and a bare substring check on "SKIPPED" would pass on that by accident
    # even if check_pairing suppressed its own line.
    assert "SKIPPED  pairing:" in result.stdout


def test_hook_path_named_but_not_in_settings_fails(tmp_path):
    """(C) hook path named, settings.json present, but the path is not in it
    -- must give FAIL. Catches the removal of the registration check in
    `hook_registered()`."""
    repo = tmp_path / "repo"
    hooks = repo / ".claude" / "hooks"
    hooks.mkdir(parents=True, exist_ok=True)
    (hooks / "streng.sh").write_text("#!/usr/bin/env bash\nexit 0\n", encoding="utf-8")
    (repo / ".claude" / "settings.json").write_text('{"hooks":{}}', encoding="utf-8")
    claude, agents, _ = _tree(tmp_path)
    for skill_root in (claude, agents):
        _skill(
            skill_root,
            "paia-streng",
            metadata=STRICT,
            body="De afdwinging zit in `.claude/hooks/streng.sh`.\n",
        )
    result = _run(tmp_path)
    assert result.returncode == 1
    assert "paia-streng" in result.stdout
    assert "counterpart" in result.stdout


def test_hook_path_registered_but_script_missing_fails(tmp_path):
    """(E) hook path named and registered in settings.json, but the
    script itself does not exist on disk -- must give FAIL. Catches the removal
    of the `[ -f "$ROOT/$hook" ]` check."""
    repo = tmp_path / "repo"
    (repo / ".claude" / "hooks").mkdir(parents=True, exist_ok=True)
    (repo / ".claude" / "settings.json").write_text(
        '{"hooks":{"PostToolUse":[{"matcher":"Edit",'
        '"hooks":[{"type":"command","command":"$CLAUDE_PROJECT_DIR/.claude/hooks/streng.sh"}]}]}}',
        encoding="utf-8",
    )
    claude, agents, _ = _tree(tmp_path)
    for skill_root in (claude, agents):
        _skill(
            skill_root,
            "paia-streng",
            metadata=STRICT,
            body="De afdwinging zit in `.claude/hooks/streng.sh`.\n",
        )
    result = _run(tmp_path)
    assert result.returncode == 1
    assert "paia-streng" in result.stdout
    assert "counterpart" in result.stdout


def test_hook_in_permissions_deny_does_not_count_as_registration(tmp_path):
    """`grep -qF` on the whole file also saw the path in `permissions.deny`
    as "registered" -- while that block has nothing to do with running hooks.
    Only the `hooks` block counts."""
    repo = tmp_path / "repo"
    hooks = repo / ".claude" / "hooks"
    hooks.mkdir(parents=True, exist_ok=True)
    (hooks / "streng.sh").write_text("#!/usr/bin/env bash\nexit 0\n", encoding="utf-8")
    (repo / ".claude" / "settings.json").write_text(
        '{"hooks":{},"permissions":{"deny":[".claude/hooks/streng.sh"]}}',
        encoding="utf-8",
    )
    claude, agents, _ = _tree(tmp_path)
    for skill_root in (claude, agents):
        _skill(
            skill_root,
            "paia-streng",
            metadata=STRICT,
            body="De afdwinging zit in `.claude/hooks/streng.sh`.\n",
        )
    result = _run(tmp_path)
    assert result.returncode == 1
    assert "paia-streng" in result.stdout
    assert "counterpart" in result.stdout


def test_disabled_variant_does_not_count_as_registration(tmp_path):
    """A `command` that points to `streng.sh.disabled` must not count
    as registration of `streng.sh` -- `grep -qF` did see it (substring),
    the path-boundary regex in `hook_registered()` did not."""
    repo = tmp_path / "repo"
    hooks = repo / ".claude" / "hooks"
    hooks.mkdir(parents=True, exist_ok=True)
    (hooks / "streng.sh").write_text("#!/usr/bin/env bash\nexit 0\n", encoding="utf-8")
    (repo / ".claude" / "settings.json").write_text(
        '{"hooks":{"PostToolUse":[{"matcher":"Edit",'
        '"hooks":[{"type":"command",'
        '"command":"$CLAUDE_PROJECT_DIR/.claude/hooks/streng.sh.disabled"}]}]}}',
        encoding="utf-8",
    )
    claude, agents, _ = _tree(tmp_path)
    for skill_root in (claude, agents):
        _skill(
            skill_root,
            "paia-streng",
            metadata=STRICT,
            body="De afdwinging zit in `.claude/hooks/streng.sh`.\n",
        )
    result = _run(tmp_path)
    assert result.returncode == 1
    assert "paia-streng" in result.stdout
    assert "counterpart" in result.stdout


def test_pairing_without_python3_is_skipped(tmp_path):
    """(A) Without python3 `hook_registered()` cannot measure. That must
    never show up as FAIL for an otherwise correctly paired skill --
    the gate would then claim "measured, found something" while nothing
    was measured at all."""
    repo = tmp_path / "repo"
    hooks = repo / ".claude" / "hooks"
    hooks.mkdir(parents=True, exist_ok=True)
    (hooks / "streng.sh").write_text("#!/usr/bin/env bash\nexit 0\n", encoding="utf-8")
    (repo / ".claude" / "settings.json").write_text(
        '{"hooks":{"PostToolUse":[{"matcher":"Edit",'
        '"hooks":[{"type":"command","command":"$CLAUDE_PROJECT_DIR/.claude/hooks/streng.sh"}]}]}}',
        encoding="utf-8",
    )
    claude, agents, _ = _tree(tmp_path)
    for skill_root in (claude, agents):
        _skill(
            skill_root,
            "paia-streng",
            metadata=STRICT,
            body="De afdwinging zit in `.claude/hooks/streng.sh`.\n",
        )
    result = _run(tmp_path, extra_env={"PATH": _path_without_python3(tmp_path)})
    assert result.returncode == 0, result.stdout + result.stderr
    # Specifically the SKIPPED line of check_pairing (see the note at
    # test_settings_missing_still_fails_on_skill_without_hook_path above).
    assert "SKIPPED  pairing:" in result.stdout
    assert "python3" in result.stdout
    assert "FAIL" not in result.stdout


def test_settings_json_not_an_object_fails_without_traceback(tmp_path):
    """(B) Valid JSON that is not an object (here: a list) must not make
    `hook_registered()` crash on `data.get(...)` -- a
    list/string has no `.get()`. Before the fix this gave an uncaught
    AttributeError with a traceback on stderr."""
    repo = tmp_path / "repo"
    hooks = repo / ".claude" / "hooks"
    hooks.mkdir(parents=True, exist_ok=True)
    (hooks / "streng.sh").write_text("#!/usr/bin/env bash\nexit 0\n", encoding="utf-8")
    (repo / ".claude" / "settings.json").write_text("[1, 2, 3]", encoding="utf-8")
    claude, agents, _ = _tree(tmp_path)
    for skill_root in (claude, agents):
        _skill(
            skill_root,
            "paia-streng",
            metadata=STRICT,
            body="De afdwinging zit in `.claude/hooks/streng.sh`.\n",
        )
    result = _run(tmp_path)
    assert "Traceback" not in result.stderr
    assert "AttributeError" not in result.stderr
    assert result.returncode == 1, result.stdout + result.stderr
    # (C) the message must name settings.json as the cause, not the
    # generic "skill lacks a counterpart" text.
    assert "settings.json" in result.stdout
    assert "valid JSON" in result.stdout
    assert "names no counterpart" not in result.stdout


def test_broken_settings_json_fails_with_clear_cause(tmp_path):
    """(C) Syntactically invalid JSON must not collapse onto the skill message
    -- the reader would then repair the skill body while the
    cause is settings.json."""
    repo = tmp_path / "repo"
    hooks = repo / ".claude" / "hooks"
    hooks.mkdir(parents=True, exist_ok=True)
    (hooks / "streng.sh").write_text("#!/usr/bin/env bash\nexit 0\n", encoding="utf-8")
    (repo / ".claude" / "settings.json").write_text(
        "dit is geen json {", encoding="utf-8"
    )
    claude, agents, _ = _tree(tmp_path)
    for skill_root in (claude, agents):
        _skill(
            skill_root,
            "paia-streng",
            metadata=STRICT,
            body="De afdwinging zit in `.claude/hooks/streng.sh`.\n",
        )
    result = _run(tmp_path)
    assert "Traceback" not in result.stderr
    assert result.returncode == 1, result.stdout + result.stderr
    assert "settings.json" in result.stdout
    assert "valid JSON" in result.stdout
    assert "names no counterpart" not in result.stdout


def test_empty_settings_json_fails_with_clear_cause(tmp_path):
    """(C) An empty settings.json file is just as unusable as a
    missing file, but it DOES exist -- so this is measurable (FAIL),
    not SKIPPED, and with the same distinguishing message."""
    repo = tmp_path / "repo"
    hooks = repo / ".claude" / "hooks"
    hooks.mkdir(parents=True, exist_ok=True)
    (hooks / "streng.sh").write_text("#!/usr/bin/env bash\nexit 0\n", encoding="utf-8")
    (repo / ".claude" / "settings.json").write_text("", encoding="utf-8")
    claude, agents, _ = _tree(tmp_path)
    for skill_root in (claude, agents):
        _skill(
            skill_root,
            "paia-streng",
            metadata=STRICT,
            body="De afdwinging zit in `.claude/hooks/streng.sh`.\n",
        )
    result = _run(tmp_path)
    assert "Traceback" not in result.stderr
    assert result.returncode == 1, result.stdout + result.stderr
    assert "settings.json" in result.stdout
    assert "valid JSON" in result.stdout


def test_this_script_is_green_on_this_repo(tmp_path):
    """The only test that tunes the thresholds. If the overlap check fires on a real
    pair, the fix is a bounding clause in that description -- not a
    higher threshold."""
    # Check 5 reads $HOME; without this isolation the personal directory of the
    # machine blocks every commit. Only the repo tree is the subject here.
    config = tmp_path / "lighthouse.toml"
    config.write_text("", encoding="utf-8")
    env = dict(
        os.environ,
        LIGHTHOUSE_REPO_ROOT=str(REPO_ROOT),
        LIGHTHOUSE_TOML=str(config),
        CHECK_INSTRUCTIONS_PERSONAL=str(tmp_path / "personal" / "skills"),
    )
    result = subprocess.run(
        [str(SCRIPT)], capture_output=True, text=True, check=False, env=env
    )
    assert result.returncode == 0, result.stdout + result.stderr


# --- check 10: size, the ceiling on CLAUDE.md -------------------------------
# DoD of milestone #42, made permanent: without a counter the
# word count just creeps up again, as happened between 2026-08-22 and 2026-09-02
# (3046 -> 5053). `[project] instructions_word_ceiling` lets the test set its
# own threshold instead of measuring against the default 3000.


def test_claude_md_above_the_ceiling_fails(tmp_path):
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "CLAUDE.md").write_text("woord " * 50, encoding="utf-8")
    result = _run(tmp_path, toml=_ceiling(10))
    assert result.returncode == 1, result.stdout
    assert "FAIL  size:" in result.stdout
    assert "ceiling" in result.stdout


def test_claude_md_at_the_ceiling_is_green(tmp_path):
    """The boundary itself, not just the sides -- at 10, 10 is not yet `> 10`."""
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "CLAUDE.md").write_text("woord " * 10, encoding="utf-8")
    result = _run(tmp_path, toml=_ceiling(10))
    assert result.returncode == 0, result.stdout + result.stderr
    assert "ok    CLAUDE.md counts 10 words, within the ceiling of 10" in result.stdout


def test_claude_md_one_above_the_ceiling_fails(tmp_path):
    """The other side of the boundary: 11 on a ceiling of 10 is already FAIL."""
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "CLAUDE.md").write_text("woord " * 11, encoding="utf-8")
    result = _run(tmp_path, toml=_ceiling(10))
    assert result.returncode == 1, result.stdout
    assert "FAIL  size: CLAUDE.md counts 11 words, ceiling is 10" in result.stdout


def test_claude_md_below_the_ceiling_is_green(tmp_path):
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "CLAUDE.md").write_text("woord " * 5, encoding="utf-8")
    result = _run(tmp_path, toml=_ceiling(10))
    assert result.returncode == 0, result.stdout + result.stderr
    assert "ok    CLAUDE.md counts 5 words, within the ceiling of 10" in result.stdout


def test_no_claude_md_is_skipped(tmp_path):
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "SKIPPED  size:" in result.stdout


def test_unreadable_claude_md_is_skipped_not_ok(tmp_path):
    """Final fix round (#686): mode 000 made `wc -w` return an empty stdout,
    after which `[ "" -gt "$CEILING" ]` failed on stderr and the if branch silently fell through
    to `ok "CLAUDE.md counts  words, within the ceiling"` -- exit 0, no
    FAIL, no SKIPPED. Unreadable must behave the same as
    absent: SKIPPED, never a silent ok."""
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    file = repo / "CLAUDE.md"
    file.write_text("woord " * 5, encoding="utf-8")
    file.chmod(0o000)
    try:
        result = _run(tmp_path)
    finally:
        file.chmod(0o644)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "SKIPPED  size:" in result.stdout
    assert "not readable" in result.stdout
    assert "ok    CLAUDE.md counts" not in result.stdout


def test_agents_md_counts_along_with_claude_md(tmp_path):
    """CLAUDE.md is an `@AGENTS.md` import plus the Claude remainder: the ceiling
    applies to what Claude Code actually loads, so to both together."""
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "CLAUDE.md").write_text("woord " * 4, encoding="utf-8")
    (repo / "AGENTS.md").write_text("woord " * 6, encoding="utf-8")
    result = _run(tmp_path, toml=_ceiling(10))
    assert result.returncode == 0, result.stdout + result.stderr
    assert (
        "ok    CLAUDE.md + AGENTS.md counts 10 words, within the ceiling of 10"
        in result.stdout
    )


def test_agents_md_pushes_the_sum_over_the_ceiling(tmp_path):
    """Proven placebo without this test: a thin CLAUDE.md always stays
    under the ceiling, however large the imported AGENTS.md becomes."""
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "CLAUDE.md").write_text("@AGENTS.md\n", encoding="utf-8")
    (repo / "AGENTS.md").write_text("woord " * 10, encoding="utf-8")
    result = _run(tmp_path, toml=_ceiling(10))
    assert result.returncode == 1, result.stdout
    assert (
        "FAIL  size: CLAUDE.md + AGENTS.md counts 11 words, ceiling is 10"
        in result.stdout
    )


def test_unreadable_agents_md_is_skipped_not_ok(tmp_path):
    """An unreadable AGENTS.md is not measured: SKIPPED, no silent ok
    on only the words of CLAUDE.md."""
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "CLAUDE.md").write_text("woord " * 5, encoding="utf-8")
    file = repo / "AGENTS.md"
    file.write_text("woord " * 5, encoding="utf-8")
    file.chmod(0o000)
    try:
        result = _run(tmp_path)
    finally:
        file.chmod(0o644)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "SKIPPED  size:" in result.stdout
    assert "not readable" in result.stdout
    assert "counts" not in result.stdout.split("size")[-1]


def test_ceiling_default_is_3000_without_override(tmp_path):
    """Without `[project] instructions_word_ceiling` the default of 3000 applies."""
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "CLAUDE.md").write_text("woord " * 5, encoding="utf-8")
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout + result.stderr
    assert (
        "ok    CLAUDE.md counts 5 words, within the ceiling of 3000" in result.stdout
    )




# --- lighthouse: root, config, marker, hook subdirectories -------------------


def test_checks_1_2_without_skills_dir_say_skipped(tmp_path):
    """Without `.claude/skills` nothing was measured; an `ok` would hide that."""
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "SKIPPED  duplicate names: no .claude/skills" in result.stdout
    assert "SKIPPED  byte-identical: no .claude/skills" in result.stdout
    assert "ok    no names claimed twice" not in result.stdout
    assert "ok    no byte-identical pairs" not in result.stdout


def test_checks_1_2_with_skills_dir_say_ok(tmp_path):
    _tree(tmp_path)
    result = _run(tmp_path)
    assert "ok    no names claimed twice" in result.stdout
    assert "ok    no byte-identical pairs within a skill root" in result.stdout


def test_check_8_sees_hook_subdirs(tmp_path):
    """`lighthouse init` puts its hooks in `.claude/hooks/lighthouse/`."""
    repo = tmp_path / "repo"
    hooks = repo / ".claude" / "hooks" / "lighthouse"
    hooks.mkdir(parents=True)
    (hooks / "streng.sh").write_text("#!/usr/bin/env bash\nexit 0\n", encoding="utf-8")
    (repo / ".claude" / "settings.json").write_text(
        '{"hooks":{"PostToolUse":[{"matcher":"Edit","hooks":[{"type":"command",'
        '"command":"$CLAUDE_PROJECT_DIR/.claude/hooks/lighthouse/streng.sh"}]}]}}',
        encoding="utf-8",
    )
    claude, agents, _ = _tree(tmp_path)
    for skill_root in (claude, agents):
        _skill(
            skill_root,
            "paia-streng",
            metadata=STRICT,
            body="De afdwinging zit in `.claude/hooks/lighthouse/streng.sh`.\n",
        )
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout
    assert "every binding: hard has a registered hook" in result.stdout


MARKER = {"lighthouse": '"core@0.1.0"'}


def test_check_11_skips_marked_skills(tmp_path):
    """A package skill differs per harness and belongs to lighthouse, not to the
    project: check 11 skips the whole directory if its `SKILL.md` carries the marker."""
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "lighthouse-x", metadata=MARKER, body="Draai in Claude Code.\n")
    _skill(agents, "lighthouse-x", metadata=MARKER, body="Draai in Codex.\n")
    (claude / "lighthouse-x" / "extra.md").write_text("Alleen links.\n", encoding="utf-8")
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout
    assert "ok    mirror-content" in result.stdout


def test_check_11_marker_skips_only_that_skill(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "lighthouse-x", metadata=MARKER, body="Draai in Claude Code.\n")
    _skill(agents, "lighthouse-x", metadata=MARKER, body="Draai in Codex.\n")
    _skill(claude, "paia-scheef", body="# Nieuwe regel\n")
    _skill(agents, "paia-scheef", body="# Oude regel\n")
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "paia-scheef/SKILL.md" in result.stdout
    assert "lighthouse-x" not in result.stdout


def test_print_mirror_exceptions_leaves_out_marked_skills(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "lighthouse-x", metadata=MARKER, body="Draai in Claude Code.\n")
    _skill(agents, "lighthouse-x", metadata=MARKER, body="Draai in Codex.\n")
    result = _run(tmp_path, args=("--print-mirror-exceptions",))
    assert result.returncode == 0, result.stderr
    assert result.stdout == ""


def test_check_11_without_exceptions_key_has_no_exceptions(tmp_path):
    """`[paths] mirror_exceptions` is optional (rc 1): then every difference is a
    finding, and the hint says where the exceptions are recorded."""
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-harnas", body="Draai in Claude Code.\n")
    _skill(agents, "paia-harnas", body="Draai in Codex.\n")
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "FAIL  mirror-content" in result.stdout
    assert "[paths] mirror_exceptions" in result.stdout
    assert ".lighthouse/scripts/check-instructions.sh --print-mirror-exceptions" in result.stdout


def test_check_11_exceptions_path_in_hint(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-scheef", body="# Nieuwe regel\n")
    _skill(agents, "paia-scheef", body="# Oude regel\n")
    result = _run(tmp_path, toml=MIRROR_TOML)
    assert result.returncode == 1, result.stdout
    assert (
        ".lighthouse/scripts/check-instructions.sh --print-mirror-exceptions"
        " > mirror-exceptions.txt" in result.stdout
    )


def test_check_10_ceiling_from_config(tmp_path):
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "CLAUDE.md").write_text("woord " * 8, encoding="utf-8")
    result = _run(tmp_path, toml=_ceiling(7))
    assert result.returncode == 1, result.stdout
    assert "FAIL  size: CLAUDE.md counts 8 words, ceiling is 7" in result.stdout


@pytest.mark.parametrize(
    "stopwords",
    [
        "gebruik wanneer welke worden binnen altijd",
        "about after before which where their",
    ],
    ids=["nl", "en"],
)
def test_check_4_stopwords_both_languages(tmp_path, stopwords):
    """Six shared words, all six stopwords: no overlap, in both languages."""
    claude, agents, _ = _tree(tmp_path)
    for name, own in (("paia-een", "appels"), ("paia-twee", "peren")):
        for skill_root in (claude, agents):
            _skill(skill_root, name, description=f"{stopwords} {own}.")
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout
    assert "ok    no unbounded overlap between descriptions" in result.stdout


def test_root_from_git_without_env(tmp_path):
    repo = tmp_path / "repo"
    repo.mkdir()
    subprocess.run(["git", "init", "-q", str(repo)], check=True)
    (repo / "CLAUDE.md").write_text("woord " * 5, encoding="utf-8")
    env = _env(tmp_path, "")
    del env["LIGHTHOUSE_REPO_ROOT"]
    result = subprocess.run(
        [str(SCRIPT)], capture_output=True, text=True, env=env, cwd=repo, timeout=30
    )
    assert "ok    CLAUDE.md counts 5 words" in result.stdout, result.stdout + result.stderr


def test_no_root_is_could_not_check(tmp_path):
    env = _env(tmp_path, "")
    del env["LIGHTHOUSE_REPO_ROOT"]
    env["GIT_CEILING_DIRECTORIES"] = str(tmp_path)
    plain = tmp_path / "geen-repo"
    plain.mkdir()
    result = subprocess.run(
        [str(SCRIPT)], capture_output=True, text=True, env=env, cwd=plain, timeout=30
    )
    assert result.returncode == 2, result.stdout + result.stderr
    assert result.stderr.startswith("check-instructions: COULD NOT CHECK — ")


def test_missing_root_dir_is_could_not_check(tmp_path):
    env = _env(tmp_path, "")
    env["LIGHTHOUSE_REPO_ROOT"] = str(tmp_path / "bestaat-niet")
    result = subprocess.run(
        [str(SCRIPT)], capture_output=True, text=True, env=env, timeout=30
    )
    assert result.returncode == 2, result.stdout + result.stderr
    assert result.stderr.startswith("check-instructions: COULD NOT CHECK — ")


def test_broken_config_is_could_not_check(tmp_path):
    _tree(tmp_path)
    result = _run(tmp_path, toml="[project\n")
    assert result.returncode == 2, result.stdout + result.stderr
    assert result.stderr.startswith("check-instructions: COULD NOT CHECK — config: ")
    assert "Traceback" not in result.stderr


def test_without_python3_config_checks_skip(tmp_path):
    """Without python3 `lighthouse.toml` cannot be read: checks 10 and 11 then measure
    nothing, and say so."""
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-gelijk")
    _skill(agents, "paia-gelijk")
    (tmp_path / "repo" / "CLAUDE.md").write_text("woord " * 5, encoding="utf-8")
    result = _run(tmp_path, extra_env={"PATH": _path_without_python3(tmp_path)})
    assert result.returncode == 0, result.stdout + result.stderr
    assert "SKIPPED  size: python3" in result.stdout
    assert "SKIPPED  mirror-content: python3" in result.stdout


def test_final_line_counts_findings(tmp_path):
    claude, _agents, _ = _tree(tmp_path)
    _skill(claude, "paia-alleen-links")
    result = _run(tmp_path)
    assert result.returncode == 1
    assert result.stdout.rstrip().endswith("finding(s).")
