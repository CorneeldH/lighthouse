"""Edge cases of the CLA gate.

The gate exists to prevent one mistake that cannot be undone: an outside merge without a
grant to sublicense. Every test below is a way that mistake could still slip through.
"""

from __future__ import annotations

import copy
import functools
import json
import os
import re
import subprocess
import tomllib
from pathlib import Path

import pytest

from tools.check_cla import (
    REQUIRED_AGREEMENT,
    GitError,
    Identity,
    RegisterError,
    collect_identities,
    load_register,
    main,
    render_report,
    repo_url,
    unsigned,
    validate_register,
)

REPO = Path(__file__).resolve().parents[1]
REGISTER = REPO / ".github" / "cla-signatures.json"


@pytest.fixture
def register() -> dict:
    """A valid register with one signature in it."""
    return {
        "cla_revisions": ["2026-08-24"],
        "current_cla_revision": "2026-08-24",
        "owner": {
            "name": "Corneel den Hartogh",
            "github_login": "CorneeldH",
            "emails": ["owner@example.com"],
        },
        "bots": [{"login": "somebot[bot]", "email": "bot@example.com"}],
        "signatures": [
            {
                "name": "Jane Doe",
                "github_login": "janedoe",
                "emails": ["jane@example.com"],
                "date": "2026-09-01",
                "cla_revision": "2026-08-24",
                "agreement": REQUIRED_AGREEMENT,
            }
        ],
    }


def test_the_shipped_register_is_valid() -> None:
    """The register in the repo must pass its own validation."""
    assert validate_register(load_register(REGISTER)) == []


def test_the_shipped_register_declares_its_current_revision() -> None:
    shipped = load_register(REGISTER)
    assert shipped["current_cla_revision"] in shipped["cla_revisions"]


def test_a_valid_register_produces_no_errors(register: dict) -> None:
    assert validate_register(register) == []


def test_a_missing_field_is_an_error(register: dict) -> None:
    del register["signatures"][0]["github_login"]
    errors = validate_register(register)
    assert any("github_login" in e for e in errors), errors


def test_an_empty_field_is_an_error(register: dict) -> None:
    register["signatures"][0]["name"] = ""
    errors = validate_register(register)
    assert any("name" in e for e in errors), errors


def test_a_wrong_agreement_sentence_is_an_error(register: dict) -> None:
    register["signatures"][0]["agreement"] = "I agree."
    errors = validate_register(register)
    assert any("agreement" in e for e in errors), errors


def test_a_near_miss_agreement_sentence_is_an_error(register: dict) -> None:
    """Byte for byte: a missing full stop counts too."""
    register["signatures"][0]["agreement"] = REQUIRED_AGREEMENT.rstrip(".")
    errors = validate_register(register)
    assert any("agreement" in e for e in errors), errors


def test_an_unknown_revision_is_an_error(register: dict) -> None:
    register["signatures"][0]["cla_revision"] = "1999-01-01"
    errors = validate_register(register)
    assert any("1999-01-01" in e for e in errors), errors


def test_a_malformed_date_is_an_error(register: dict) -> None:
    register["signatures"][0]["date"] = "1 september 2026"
    errors = validate_register(register)
    assert any("date" in e for e in errors), errors


def test_an_email_claimed_twice_is_an_error(register: dict) -> None:
    second = copy.deepcopy(register["signatures"][0])
    second["name"] = "John Roe"
    second["github_login"] = "johnroe"
    register["signatures"].append(second)
    errors = validate_register(register)
    assert any("jane@example.com" in e for e in errors), errors


def test_an_email_also_claimed_by_the_owner_is_an_error(register: dict) -> None:
    register["signatures"][0]["emails"] = ["owner@example.com"]
    errors = validate_register(register)
    assert any("owner@example.com" in e for e in errors), errors


def test_an_empty_emails_list_is_an_error(register: dict) -> None:
    register["signatures"][0]["emails"] = []
    errors = validate_register(register)
    assert any("emails" in e for e in errors), errors


def test_broken_json_raises_register_error(tmp_path: Path) -> None:
    path = tmp_path / "cla-signatures.json"
    path.write_text("{not json", encoding="utf-8")
    with pytest.raises(RegisterError, match="is not valid JSON"):
        load_register(path)


def test_a_missing_register_raises_register_error(tmp_path: Path) -> None:
    with pytest.raises(RegisterError, match="does not exist"):
        load_register(tmp_path / "absent.json")


def test_load_register_round_trips(tmp_path: Path, register: dict) -> None:
    path = tmp_path / "cla-signatures.json"
    path.write_text(json.dumps(register), encoding="utf-8")
    assert load_register(path) == register


# --- Collecting identities ----------------------------------------------------
#
# On throwaway repos in tmp_path, not on this repo: the gate must also hold for commit
# shapes that do not (yet) occur here — a fork pull request with an unknown co-author is
# exactly the case it exists for.

ALICE = ("Alice Example", "alice@example.com")
BOB = ("Bob Example", "bob@example.com")

ANY_SHA = "<any>"


def _normalise(identities: list[Identity]) -> list[Identity]:
    """Compare without the SHA — that changes on every run."""
    return [Identity(i.email, i.name, i.role, ANY_SHA) for i in identities]


def _git(repo: Path, *args: str, **env: str) -> str:
    result = subprocess.run(
        ["git", "-C", str(repo), *args],
        capture_output=True,
        text=True,
        check=True,
        # Inherit the real environment (git must stay findable), but switch off the user
        # configuration: a global `user.email` or a `commit.gpgsign` of the developer must
        # not steer these tests.
        env={
            **os.environ,
            "HOME": str(repo),
            "GIT_CONFIG_GLOBAL": "/dev/null",
            "GIT_CONFIG_SYSTEM": "/dev/null",
            **env,
        },
    )
    return result.stdout


def _init_repo(tmp_path: Path) -> Path:
    repo = tmp_path / "repo"
    repo.mkdir()
    _git(repo, "init", "-q", "-b", "main")
    return repo


def _commit(
    repo: Path,
    message: str,
    *,
    author: tuple[str, str] = ALICE,
    committer: tuple[str, str] | None = None,
) -> None:
    committer = committer or author
    (repo / "file.txt").write_text(message, encoding="utf-8")
    _git(repo, "add", "file.txt")
    _git(
        repo,
        "-c",
        f"user.name={committer[0]}",
        "-c",
        f"user.email={committer[1]}",
        "commit",
        "-q",
        "--author",
        f"{author[0]} <{author[1]}>",
        "-m",
        message,
        GIT_COMMITTER_NAME=committer[0],
        GIT_COMMITTER_EMAIL=committer[1],
    )


def _range_repo(tmp_path: Path) -> Path:
    """A repo with one commit on `base` and a feature branch after it."""
    repo = _init_repo(tmp_path)
    _commit(repo, "base commit")
    _git(repo, "branch", "base")
    return repo


def test_the_author_of_each_commit_is_collected(tmp_path: Path) -> None:
    repo = _range_repo(tmp_path)
    _commit(repo, "work", author=BOB)
    found = collect_identities(repo, "base")
    assert Identity("bob@example.com", "Bob Example", "author", ANY_SHA) in _normalise(
        found
    )


def test_the_committer_of_each_commit_is_collected(tmp_path: Path) -> None:
    repo = _range_repo(tmp_path)
    _commit(repo, "work", author=ALICE, committer=BOB)
    emails = {i.email for i in collect_identities(repo, "base")}
    assert emails == {"alice@example.com", "bob@example.com"}


def test_co_authored_by_trailers_are_collected(tmp_path: Path) -> None:
    repo = _range_repo(tmp_path)
    (repo / "file.txt").write_text("trailer", encoding="utf-8")
    _git(repo, "add", "file.txt")
    _git(
        repo,
        "-c",
        f"user.name={ALICE[0]}",
        "-c",
        f"user.email={ALICE[1]}",
        "commit",
        "-q",
        "-m",
        "work\n\nCo-authored-by: Bob Example <bob@example.com>\n",
        GIT_COMMITTER_NAME=ALICE[0],
        GIT_COMMITTER_EMAIL=ALICE[1],
    )
    found = [i for i in collect_identities(repo, "base") if i.role == "co-author"]
    assert [(i.email, i.name) for i in found] == [("bob@example.com", "Bob Example")]


def test_the_co_authored_by_trailer_is_matched_case_insensitively(
    tmp_path: Path,
) -> None:
    repo = _range_repo(tmp_path)
    (repo / "file.txt").write_text("trailer", encoding="utf-8")
    _git(repo, "add", "file.txt")
    _git(
        repo,
        "-c",
        f"user.name={ALICE[0]}",
        "-c",
        f"user.email={ALICE[1]}",
        "commit",
        "-q",
        "-m",
        "work\n\nCo-Authored-By: Bob Example <BOB@example.com>\n",
        GIT_COMMITTER_NAME=ALICE[0],
        GIT_COMMITTER_EMAIL=ALICE[1],
    )
    emails = {
        i.email for i in collect_identities(repo, "base") if i.role == "co-author"
    }
    assert emails == {"bob@example.com"}


def test_the_github_web_flow_committer_is_ignored(tmp_path: Path) -> None:
    repo = _range_repo(tmp_path)
    _commit(repo, "web merge", author=ALICE, committer=("GitHub", "noreply@github.com"))
    emails = {i.email for i in collect_identities(repo, "base")}
    assert emails == {"alice@example.com"}


def test_commits_already_on_the_base_are_not_collected(tmp_path: Path) -> None:
    repo = _range_repo(tmp_path)
    assert collect_identities(repo, "base") == []


def test_an_email_is_lowercased(tmp_path: Path) -> None:
    repo = _range_repo(tmp_path)
    _commit(repo, "work", author=("Bob Example", "BOB@example.com"))
    emails = {i.email for i in collect_identities(repo, "base")}
    assert emails == {"bob@example.com"}


def test_a_literal_x1e_byte_in_the_commit_message_does_not_break_parsing(
    tmp_path: Path,
) -> None:
    """`%x1e` was once the record separator; a commit message may legally contain that
    byte (git forbids only NUL). `-F` instead of `-m`, because getting the byte across the
    command line reliably is not guaranteed."""
    repo = _range_repo(tmp_path)
    (repo / "file.txt").write_text("x1e", encoding="utf-8")
    _git(repo, "add", "file.txt")
    message_path = tmp_path / "message.txt"
    message_path.write_bytes(
        b"work \x1e work\n\nCo-authored-by: Bob Example <bob@example.com>\n"
    )
    _git(
        repo,
        "-c",
        f"user.name={ALICE[0]}",
        "-c",
        f"user.email={ALICE[1]}",
        "commit",
        "-q",
        "-F",
        str(message_path),
        GIT_COMMITTER_NAME=ALICE[0],
        GIT_COMMITTER_EMAIL=ALICE[1],
    )
    emails = {
        i.email for i in collect_identities(repo, "base") if i.role == "co-author"
    }
    assert emails == {"bob@example.com"}


def test_a_x1f_byte_in_the_author_name_cannot_hide_the_real_address(
    tmp_path: Path,
) -> None:
    """`%x1f` was once the field separator, and a git ident may contain that byte.

    An embedded `\x1f` in the author name then shifted every field after it one place —
    and `str.split(sep, maxsplit)` never complains about that. The result was fail-open:
    the parser read a covered address as both author and committer email, while the real
    address stayed stuck in a name field and so was never held against the register.
    Precisely the contributor who does not want to sign has a reason to make such a
    commit.
    """
    repo = _range_repo(tmp_path)
    (repo / "file.txt").write_text("sneaky", encoding="utf-8")
    _git(repo, "add", "file.txt")
    _git(
        repo,
        "commit",
        "-q",
        "-m",
        "sneaky",
        GIT_AUTHOR_NAME="X\x1fowner@example.com",
        GIT_AUTHOR_EMAIL="evil@example.com",
        GIT_COMMITTER_NAME="owner@example.com",
        GIT_COMMITTER_EMAIL="alice@example.com",
    )
    emails = {i.email for i in collect_identities(repo, "base")}
    assert "evil@example.com" in emails, emails


def test_an_empty_commit_message_still_yields_the_author(tmp_path: Path) -> None:
    """An empty message makes the last field empty; the six-chunk count must survive that,
    or the integrity check breaks on a legitimate commit."""
    repo = _range_repo(tmp_path)
    (repo / "file.txt").write_text("empty", encoding="utf-8")
    _git(repo, "add", "file.txt")
    empty_message = tmp_path / "empty.txt"
    empty_message.write_text("", encoding="utf-8")
    _git(
        repo,
        "commit",
        "-q",
        "--allow-empty-message",
        "-F",
        str(empty_message),
        GIT_AUTHOR_NAME=BOB[0],
        GIT_AUTHOR_EMAIL=BOB[1],
        GIT_COMMITTER_NAME=BOB[0],
        GIT_COMMITTER_EMAIL=BOB[1],
    )
    emails = {i.email for i in collect_identities(repo, "base")}
    assert emails == {"bob@example.com"}


def test_a_desynchronised_git_log_stream_raises(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    """The `% 6` check is the safety net under the chunk grouping; it needs a test of its
    own too. A stream that is not a multiple of six fields cannot be trusted and must fail
    loudly, not group silently and wrongly."""
    repo = _range_repo(tmp_path)
    monkeypatch.setattr(
        "tools.check_cla._git", lambda *args, **kwargs: "aaa\x00Name\x00mail\x00"
    )
    with pytest.raises(GitError, match="not a multiple of"):
        collect_identities(repo, "base")


# --- Matching, report and exit codes ------------------------------------------

OWNER = ("Corneel den Hartogh", "owner@example.com")
BOT = ("Some Bot", "bot@example.com")


def _write_register(repo: Path, register: dict) -> Path:
    path = repo / ".github" / "cla-signatures.json"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(register, indent=2) + "\n", encoding="utf-8")
    return path


def _run(repo: Path, register_path: Path, base: str = "base") -> int:
    return main(
        [
            "--repo",
            str(repo),
            "--base",
            base,
            "--register",
            str(register_path),
        ]
    )


def test_an_owner_commit_passes(tmp_path: Path, register: dict) -> None:
    repo = _range_repo(tmp_path)
    _commit(repo, "work", author=OWNER)
    path = _write_register(repo, register)
    assert _run(repo, path) == 0


def test_a_bot_commit_passes(tmp_path: Path, register: dict) -> None:
    repo = _range_repo(tmp_path)
    _commit(repo, "bump", author=BOT)
    path = _write_register(repo, register)
    assert _run(repo, path) == 0


def test_a_signed_contributor_passes(tmp_path: Path, register: dict) -> None:
    repo = _range_repo(tmp_path)
    _commit(repo, "work", author=("Jane Doe", "jane@example.com"))
    path = _write_register(repo, register)
    assert _run(repo, path) == 0


def test_an_unknown_contributor_fails(tmp_path: Path, register: dict) -> None:
    repo = _range_repo(tmp_path)
    _commit(repo, "work", author=("Mallory", "mallory@example.com"))
    path = _write_register(repo, register)
    assert _run(repo, path) == 1


def test_an_unknown_co_author_fails(tmp_path: Path, register: dict) -> None:
    """The committer is covered, the co-author is not — that must still be red."""
    repo = _range_repo(tmp_path)
    (repo / "file.txt").write_text("x", encoding="utf-8")
    _git(repo, "add", "file.txt")
    _git(
        repo,
        "-c",
        f"user.name={OWNER[0]}",
        "-c",
        f"user.email={OWNER[1]}",
        "commit",
        "-q",
        "-m",
        "work\n\nCo-authored-by: Mallory <mallory@example.com>\n",
        GIT_COMMITTER_NAME=OWNER[0],
        GIT_COMMITTER_EMAIL=OWNER[1],
    )
    path = _write_register(repo, register)
    assert _run(repo, path) == 1


def test_an_empty_author_email_is_never_covered(register: dict) -> None:
    """An empty ident is no wildcard.

    Directly on `unsigned()` and not through a real commit: git refuses an empty author
    email on the command line and falls back to the configured ident, so such a commit
    cannot be fabricated here. The point is that the register cannot cover it by accident
    either.
    """
    missing = unsigned([Identity("", "Nobody", "author", "abc1234")], register)
    assert [i.email for i in missing] == [""]


def test_an_empty_address_gets_its_own_explanation() -> None:
    report = render_report([Identity("", "Nobody", "author", "abc1234")])
    assert "*(empty)*" in report
    assert "--reset-author" in report, "the report must say how to fix this"


def test_an_invalid_register_exits_two(tmp_path: Path, register: dict) -> None:
    repo = _range_repo(tmp_path)
    _commit(repo, "work", author=OWNER)
    register["signatures"][0]["agreement"] = "nope"
    path = _write_register(repo, register)
    assert _run(repo, path) == 2


def test_a_missing_register_exits_two(tmp_path: Path) -> None:
    repo = _range_repo(tmp_path)
    _commit(repo, "work", author=OWNER)
    assert _run(repo, repo / ".github" / "absent.json") == 2


def test_an_unknown_base_ref_exits_two(tmp_path: Path, register: dict) -> None:
    repo = _range_repo(tmp_path)
    path = _write_register(repo, register)
    assert _run(repo, path, base="origin/does-not-exist") == 2


def test_the_report_names_the_missing_address(tmp_path: Path) -> None:
    missing = [Identity("mallory@example.com", "Mallory", "author", "abc1234")]
    report = render_report(missing)
    assert "mallory@example.com" in report
    assert "Mallory" in report
    assert "CLA.md" in report, "the report must say how to sign"
    assert ".github/cla-signatures.json" in report


def test_the_report_lands_in_the_step_summary(
    tmp_path: Path, register: dict, monkeypatch: pytest.MonkeyPatch
) -> None:
    repo = _range_repo(tmp_path)
    _commit(repo, "work", author=("Mallory", "mallory@example.com"))
    path = _write_register(repo, register)
    summary = tmp_path / "summary.md"
    monkeypatch.setenv("GITHUB_STEP_SUMMARY", str(summary))
    assert _run(repo, path) == 1
    assert "mallory@example.com" in summary.read_text(encoding="utf-8")


def test_one_address_is_reported_once(tmp_path: Path) -> None:
    """Three commits by the same unknown person give one line, not three."""
    missing = unsigned(
        [
            Identity("mallory@example.com", "Mallory", "author", "aaa"),
            Identity("mallory@example.com", "Mallory", "committer", "aaa"),
            Identity("mallory@example.com", "Mallory", "author", "bbb"),
        ],
        {
            "cla_revisions": ["2026-08-24"],
            "current_cla_revision": "2026-08-24",
            "owner": {"name": "O", "emails": ["owner@example.com"]},
            "bots": [],
            "signatures": [],
        },
    )
    assert [i.email for i in missing] == ["mallory@example.com"]


def _has_origin_main() -> bool:
    result = subprocess.run(
        ["git", "-C", str(REPO), "rev-parse", "--verify", "--quiet", "origin/main"],
        capture_output=True,
        check=False,
    )
    return result.returncode == 0


@pytest.mark.skipif(
    not _has_origin_main(), reason="checkout without remote-tracking ref origin/main"
)
def test_this_repo_passes_its_own_gate() -> None:
    """The gate on this branch: every identity since origin/main is covered."""
    assert main(["--repo", str(REPO), "--base", "origin/main"]) == 0


def test_a_crafted_name_cannot_break_the_report_table() -> None:
    """Name and address come from the commit, so the contributor chooses them.

    A `|` would break the column count, a backtick the code span and a newline the row;
    markdown such as `**bold**` would make the table lie. The report is exactly the thing
    the maintainer relies on to see who did not sign. The row must therefore keep four
    columns, and name and address both sit in a code span, so markdown in them does
    nothing any more.
    """
    report = render_report(
        [
            Identity(
                "evil`@example.com",
                "Mallory | **nothing to see here**\nextra line",
                "author",
                "abc1234",
            )
        ]
    )
    row = next(line for line in report.splitlines() if "Mallory" in line)
    assert row.count("|") - row.count("\\|") == 5, row
    assert row.count("`") == 6, f"three code spans expected: {row}"
    assert "extra line" in row, "the content must stay visible, only disarmed"


def test_repo_url_reads_the_forge_repo_from_lighthouse_toml(tmp_path: Path) -> None:
    """The URL comes from `[forge] repo`, not from a hard-coded owner and name."""
    (tmp_path / "lighthouse.toml").write_text(
        '[forge]\nrepo = "Other/repo"\n', encoding="utf-8"
    )
    assert repo_url(tmp_path) == "https://github.com/Other/repo"


def test_repo_url_defaults_to_this_repo() -> None:
    config = tomllib.loads((REPO / "lighthouse.toml").read_text(encoding="utf-8"))
    assert repo_url() == f"https://github.com/{config['forge']['repo']}"


def test_the_report_links_cla_md_of_the_repo_from_lighthouse_toml(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    """The report links `CLA.md` of the repository in `lighthouse.toml`, not of this one."""
    (tmp_path / "lighthouse.toml").write_text(
        '[forge]\nrepo = "Other/repo"\n', encoding="utf-8"
    )
    monkeypatch.setattr(
        "tools.check_cla.repo_url", functools.partial(repo_url, tmp_path)
    )
    text = render_report([])
    assert "https://github.com/Other/repo/blob/main/CLA.md" in text
    assert "CorneeldH/lighthouse" not in text


# --- The register guard -------------------------------------------------------
#
# The register comes from the working tree, so that signing in the same pull request
# works. Precisely for that reason the same pull request must not be able to quietly
# remove an existing signature: that is a withdrawn grant without anyone noticing.


def _repo_with_committed_register(tmp_path: Path, register: dict) -> tuple[Path, Path]:
    repo = _init_repo(tmp_path)
    path = _write_register(repo, register)
    _git(repo, "add", ".github/cla-signatures.json")
    _git(
        repo,
        "-c",
        f"user.name={OWNER[0]}",
        "-c",
        f"user.email={OWNER[1]}",
        "commit",
        "-q",
        "-m",
        "register",
        GIT_COMMITTER_NAME=OWNER[0],
        GIT_COMMITTER_EMAIL=OWNER[1],
    )
    _git(repo, "branch", "base")
    return repo, path


def test_adding_a_signature_passes(tmp_path: Path, register: dict) -> None:
    repo, path = _repo_with_committed_register(tmp_path, register)
    register["signatures"].append(
        {
            "name": "John Roe",
            "github_login": "johnroe",
            "emails": ["john@example.com"],
            "date": "2026-09-02",
            "cla_revision": "2026-08-24",
            "agreement": REQUIRED_AGREEMENT,
        }
    )
    _write_register(repo, register)
    _commit(repo, "work", author=("John Roe", "john@example.com"))
    assert _run(repo, path) == 0


def test_removing_a_signature_fails(tmp_path: Path, register: dict) -> None:
    repo, path = _repo_with_committed_register(tmp_path, register)
    register["signatures"] = []
    _write_register(repo, register)
    _commit(repo, "work", author=OWNER)
    assert _run(repo, path) == 2


def test_editing_a_signature_fails(tmp_path: Path, register: dict) -> None:
    repo, path = _repo_with_committed_register(tmp_path, register)
    register["signatures"][0]["emails"] = ["jane@example.com", "sneaky@example.com"]
    _write_register(repo, register)
    _commit(repo, "work", author=OWNER)
    assert _run(repo, path) == 2


def test_reordering_signatures_passes(tmp_path: Path, register: dict) -> None:
    """Order is not content; only disappearing or changing counts."""
    register["signatures"].append(
        {
            "name": "John Roe",
            "github_login": "johnroe",
            "emails": ["john@example.com"],
            "date": "2026-09-02",
            "cla_revision": "2026-08-24",
            "agreement": REQUIRED_AGREEMENT,
        }
    )
    repo, path = _repo_with_committed_register(tmp_path, register)
    register["signatures"].reverse()
    _write_register(repo, register)
    _commit(repo, "work", author=OWNER)
    assert _run(repo, path) == 0


def test_dropping_a_known_revision_fails(tmp_path: Path, register: dict) -> None:
    """Removing a revision invalidates every signature on it.

    The removed revision is deliberately one that no signature uses. Otherwise
    `validate_register` would already fire on "unknown cla_revision" and this test would
    prove nothing about the append-only guard.
    """
    register["cla_revisions"] = ["2026-08-24", "2027-01-01"]
    repo, path = _repo_with_committed_register(tmp_path, register)
    register["cla_revisions"] = ["2026-08-24"]
    _write_register(repo, register)
    _commit(repo, "work", author=OWNER)
    assert validate_register(register) == [], (
        "the failure must come from the guard, not from the validation"
    )
    assert _run(repo, path) == 2


def test_a_register_absent_on_the_base_passes(tmp_path: Path, register: dict) -> None:
    """The pull request that introduces the register has nothing to compare against."""
    repo = _range_repo(tmp_path)
    path = _write_register(repo, register)
    _git(repo, "add", ".github/cla-signatures.json")
    _git(
        repo,
        "-c",
        f"user.name={OWNER[0]}",
        "-c",
        f"user.email={OWNER[1]}",
        "commit",
        "-q",
        "-m",
        "introduce register",
        GIT_COMMITTER_NAME=OWNER[0],
        GIT_COMMITTER_EMAIL=OWNER[1],
    )
    assert _run(repo, path) == 0


def test_adding_yourself_to_the_owner_emails_fails(
    tmp_path: Path, register: dict
) -> None:
    """`owner` covers without a signature, and the register comes from the working tree.

    Without a guard on this field the whole gate can be bypassed with one line of JSON:
    put your own address in `owner.emails` and `_covered_emails` covers you at once.
    """
    repo, path = _repo_with_committed_register(tmp_path, register)
    register["owner"]["emails"].append("mallory@example.com")
    _write_register(repo, register)
    _commit(repo, "work", author=("Mallory", "mallory@example.com"))
    assert _run(repo, path) == 2


def test_adding_yourself_as_a_bot_fails(tmp_path: Path, register: dict) -> None:
    """The same route through `bots`; a bot entry covers just as well."""
    repo, path = _repo_with_committed_register(tmp_path, register)
    register["bots"].append({"login": "notreal[bot]", "email": "mallory@example.com"})
    _write_register(repo, register)
    _commit(repo, "work", author=("Mallory", "mallory@example.com"))
    assert _run(repo, path) == 2


def test_removing_a_bot_passes(tmp_path: Path, register: dict) -> None:
    """Shrinking covers nobody extra, so the guard need not stop it."""
    repo, path = _repo_with_committed_register(tmp_path, register)
    register["bots"] = []
    _write_register(repo, register)
    _commit(repo, "work", author=OWNER)
    assert _run(repo, path) == 0


def test_an_empty_range_is_not_a_green_result(tmp_path: Path, register: dict) -> None:
    """`--base ""` makes the range `git log ..`, that is empty, that is "all covered".

    A green outcome that measured nothing is exactly what this gate must not be able to
    give; it has to fail closed.
    """
    repo = _range_repo(tmp_path)
    path = _write_register(repo, register)
    assert main(["--repo", str(repo), "--base", "", "--register", str(path)]) == 2
    assert (
        main(
            [
                "--repo",
                str(repo),
                "--base",
                "base",
                "--head",
                "",
                "--register",
                str(path),
            ]
        )
        == 2
    )


# --- The workflow -------------------------------------------------------------
#
# No yaml dependency in this repository, so the workflow is read as lines: comments
# stripped, and a block is the run of lines indented deeper than its key. That is enough
# for the plain block style the workflow uses; a shape it does not recognise makes a test
# fail, not pass.

CLA_WORKFLOW = REPO / ".github" / "workflows" / "cla.yml"


def _indent(line: str) -> int:
    return len(line) - len(line.lstrip())


def _code_lines(text: str) -> list[str]:
    """The workflow without comments and blank lines."""
    lines = []
    for line in text.splitlines():
        line = re.sub(r"(^|\s)#.*$", "", line).rstrip()
        if line.strip():
            lines.append(line)
    return lines


def _children(lines: list[str], index: int) -> list[str]:
    """The lines after `lines[index]` that are indented deeper than it."""
    own = _indent(lines[index])
    children = []
    for line in lines[index + 1 :]:
        if _indent(line) <= own:
            break
        children.append(line)
    return children


def _blocks(lines: list[str], key: str) -> list[tuple[int, list[str]]]:
    """Every `key:` without an inline value, as (indent, child lines)."""
    pattern = re.compile(rf"^\s*(?:- )?{re.escape(key)}:$")
    return [
        (_indent(line), _children(lines, i))
        for i, line in enumerate(lines)
        if pattern.match(line)
    ]


def _stripped(lines: list[str]) -> list[str]:
    return [line.strip() for line in lines]


def _workflow_lines() -> list[str]:
    return _code_lines(CLA_WORKFLOW.read_text(encoding="utf-8"))


def _jobs() -> dict[str, list[str]]:
    """Every job under the top-level `jobs:`, by name."""
    lines = _workflow_lines()
    top = [children for indent, children in _blocks(lines, "jobs") if indent == 0]
    assert len(top) == 1, "no single top-level `jobs:`"
    body = top[0]
    job_indent = _indent(body[0])
    jobs = {}
    for i, line in enumerate(body):
        if _indent(line) == job_indent:
            jobs[line.strip().rstrip(":")] = _children(body, i)
    return jobs


def _steps(job: list[str]) -> list[list[str]]:
    """The steps of one job, each as its lines with the leading `- ` turned into spaces."""
    blocks = _blocks(job, "steps")
    assert len(blocks) == 1, "no single `steps:` in the job"
    body = blocks[0][1]
    step_indent = _indent(body[0])
    steps: list[list[str]] = []
    for line in body:
        if _indent(line) == step_indent and line.lstrip().startswith("- "):
            line = line.replace("- ", "  ", 1)
            steps.append([])
        steps[-1].append(line)
    return steps


def _value(step: list[str], key: str) -> str:
    """The inline value of `key:` at the top level of a step, or a block scalar joined."""
    top = _indent(step[0])
    for i, line in enumerate(step):
        match = re.match(rf"^\s*{re.escape(key)}:\s*(.*)$", line)
        if match and _indent(line) == top:
            value = match.group(1)
            if value[:1] in ("|", ">"):
                return " ".join(_stripped(_children(step, i)))
            return value
    return ""


def _checkout(steps: list[list[str]]) -> list[str]:
    return next(
        step for step in steps if _value(step, "uses").startswith("actions/checkout@")
    )


def _cla_steps() -> list[list[str]]:
    return _steps(_jobs()["cla"])


def test_the_cla_workflow_never_runs_self_hosted() -> None:
    """On a public repo a fork pull request runs here; that may never be a self-hosted
    machine."""
    jobs = _jobs()
    assert jobs, "no jobs found"
    for name, job in jobs.items():
        runs_on = [
            line.split(":", 1)[1].strip()
            for line in job
            if _indent(line) == _indent(job[0]) and line.strip().startswith("runs-on:")
        ]
        assert len(runs_on) == 1, f"job {name} has no single inline `runs-on`"
        assert "self-hosted" not in runs_on[0].lower(), f"job {name} runs self-hosted"
        assert runs_on[0] == "ubuntu-latest", f"job {name} runs on {runs_on[0]!r}"


def test_the_cla_workflow_is_read_only() -> None:
    top = [
        children
        for indent, children in _blocks(_workflow_lines(), "permissions")
        if indent == 0
    ]
    assert [_stripped(children) for children in top] == [["contents: read"]]
    for name, job in _jobs().items():
        permissions = [_stripped(children) for _, children in _blocks(job, "permissions")]
        assert permissions == [["contents: read"]], f"job {name}"


def test_the_cla_workflow_reaches_no_secrets() -> None:
    """Not only `secrets.`: `secrets[...]`, `toJSON(secrets)` and `github.token` give the
    same access through another spelling, and a test that only knows the dot form does
    not cover a later edit."""
    text = CLA_WORKFLOW.read_text(encoding="utf-8")
    assert "pull_request_target" not in text
    for form in ("secrets.", "secrets[", "secrets)", "github.token"):
        assert form not in text, form


def test_the_cla_workflow_runs_on_every_pull_request() -> None:
    top = [children for indent, children in _blocks(_workflow_lines(), "on") if indent == 0]
    assert len(top) == 1, "no single top-level `on:`"
    pull_request = _blocks(top[0], "pull_request")
    assert len(pull_request) == 1, "no `pull_request:` trigger"
    settings = _stripped(pull_request[0][1])
    assert "branches: [main]" in settings, settings
    assert not any(line.startswith(("paths:", "paths-ignore:")) for line in settings)


def test_the_cla_workflow_installs_nothing() -> None:
    """No uv, no pip: a fork pull request may not trigger a dependency install."""
    scripts = " ".join(_value(step, "run") for step in _cla_steps())
    for installer in ("uv", "uvx", "pip", "pipx", "apt-get", "npm", "curl", "wget"):
        assert not re.search(rf"\b{installer}\b", scripts), f"{installer}: {scripts}"
    assert "python3 tools/check_cla.py" in scripts


def test_the_cla_workflow_checks_out_full_history() -> None:
    blocks = _blocks(_checkout(_cla_steps()), "with")
    assert len(blocks) == 1, "the checkout step has no `with:`"
    settings = _stripped(blocks[0][1])
    assert "fetch-depth: 0" in settings, settings
    assert "persist-credentials: false" in settings, settings


def test_the_cla_workflow_needs_no_credentials_after_checkout() -> None:
    """`persist-credentials: false` removes the checkout credential, so every later git
    call to the remote is anonymous — and that fails on a private repo. The lane must
    therefore not depend on one: no `ref:` override, no second fetch, but the merge ref
    plus two explicit SHAs, both already local."""
    steps = _cla_steps()
    settings = [
        line
        for _, children in _blocks(_checkout(steps), "with")
        for line in _stripped(children)
    ]
    assert not any(line.startswith("ref:") for line in settings), (
        "an explicit ref checks out only that ancestry; the base is then not local"
    )
    scripts = " ".join(_value(step, "run") for step in steps)
    assert "git fetch" not in scripts, scripts


def test_the_cla_workflow_measures_the_pull_request_range() -> None:
    """The merge commit itself carries `GitHub <noreply@github.com>` as author. By
    measuring `base.sha..head.sha` it falls outside the range by definition."""
    step = next(s for s in _cla_steps() if "check_cla.py" in _value(s, "run"))
    env_blocks = _blocks(step, "env")
    assert len(env_blocks) == 1, "the CLA step has no `env:`"
    env = _stripped(env_blocks[0][1])
    assert "BASE_SHA: ${{ github.event.pull_request.base.sha }}" in env, env
    assert "HEAD_SHA: ${{ github.event.pull_request.head.sha }}" in env, env
    run = _value(step, "run")
    assert '--base "$BASE_SHA"' in run
    assert '--head "$HEAD_SHA"' in run
