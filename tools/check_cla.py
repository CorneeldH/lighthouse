#!/usr/bin/env python3
"""CLA gate: every identity in a pull request must be covered by the signature register.

Why this exists: without a CLA, what the project receives from an outside contributor is
only a licence under the repository's own terms — no right to sublicense or relicense.
From the first uncovered merge on, every licence change needs the consent of every
contributor, permanently. One forgotten pull request cannot be undone; hence a gate, not
an agreement.

STDLIB ONLY, ON PURPOSE
-----------------------
This script runs as `python3 tools/check_cla.py` on a fork pull request, without
`uv sync` and without any install. An import outside the standard library makes that
impossible and would bring back a dependency install on fork code.

THE REGISTER COMES FROM THE WORKING TREE
----------------------------------------
Not from the base branch. Otherwise signing in the same pull request cannot work by
definition, and that is exactly the route that keeps the threshold low. The price is that
the register itself has to be guarded: `check_append_only` compares it with the copy on
the base branch, so a removed or changed signature — a silently withdrawn grant — turns red.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import tomllib
from dataclasses import dataclass
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent

REQUIRED_AGREEMENT = "I have read the CLA document and I hereby sign the CLA."

_DATE = re.compile(r"^\d{4}-\d{2}-\d{2}$")

_SIGNATURE_FIELDS = ("name", "github_login", "date", "cla_revision", "agreement")


class RegisterError(Exception):
    """The register could not be read or parsed."""


# GitHub signs a merge commit made through the web interface with its own `web-flow`
# identity. That is no contributor and there is nothing to ask of it.
IGNORED_COMMITTER_EMAILS = frozenset({"noreply@github.com"})

# NUL as separator, for records and fields (`git log -z` plus `%x00` between fields).
# The whole stream then becomes one flat run of NUL-separated chunks, exactly
# `6 * number_of_commits` of them, and that count is an integrity check in itself.
#
# Why not `%x1f`: git's identity cleanup strips only `<`, `>`, newlines and leading or
# trailing control characters, so an embedded `\x1f` survives in an author name. It then
# silently shifts every field after it — `split` never complains — and the real email
# address lands in a name field, where nothing holds it against the register any more.
# Fail-open, exactly what this gate must prevent.
#
# In a git identity a NUL is structurally impossible: name and email come from
# NUL-terminated C strings (env/argv). For a commit *message* it is no absolute
# guarantee — an object made through plumbing can carry a NUL; what stops that in
# practice is the fsck of the receiving server, not this script. If such an object does
# land, the chunk boundaries shift; the `% 6` check usually catches that, but not when
# the number of extra NULs happens to be a multiple of six.
_LOG_FORMAT = "%h%x00%an%x00%ae%x00%cn%x00%ce%x00%B"

_FIELDS_PER_COMMIT = 6

_COAUTHOR = re.compile(
    r"^[ \t]*co-authored-by:[ \t]*(?P<name>.*?)[ \t]*<(?P<email>[^>]+)>[ \t]*$",
    re.IGNORECASE | re.MULTILINE,
)


class GitError(Exception):
    """A git call failed."""


@dataclass(frozen=True)
class Identity:
    """One person or bot as it appears in one commit."""

    email: str
    name: str
    role: str  # "author" | "committer" | "co-author"
    commit: str


def repo_url(root: Path = REPO_ROOT) -> str:
    """The web URL of this repository, from `[forge] repo` in `lighthouse.toml`."""
    config = tomllib.loads((root / "lighthouse.toml").read_text(encoding="utf-8"))
    return f"https://github.com/{config['forge']['repo']}"


def _git(repo: Path, *args: str) -> str:
    result = subprocess.run(
        ["git", "-C", str(repo), *args],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        raise GitError(
            f"git {' '.join(args)} failed (exit {result.returncode}): {result.stderr.strip()}"
        )
    return result.stdout


def collect_identities(repo: Path, base: str, head: str = "HEAD") -> list[Identity]:
    """Every identity in `base..head`: author, committer and co-authors."""
    raw = _git(repo, "log", "-z", f"--format={_LOG_FORMAT}", f"{base}..{head}")
    chunks = raw.split("\x00")
    # `git log -z` ends every record with a NUL, so the last split always yields one
    # empty tail. An empty range yields nothing at all.
    if chunks and chunks[-1] == "":
        chunks.pop()
    if len(chunks) % _FIELDS_PER_COMMIT:
        raise GitError(
            f"git log gave {len(chunks)} NUL-separated fields, not a multiple of "
            f"{_FIELDS_PER_COMMIT}; the output cannot be trusted"
        )

    identities: list[Identity] = []
    for start in range(0, len(chunks), _FIELDS_PER_COMMIT):
        sha, author_name, author_email, committer_name, committer_email, body = chunks[
            start : start + _FIELDS_PER_COMMIT
        ]
        identities.append(
            Identity(author_email.strip().lower(), author_name.strip(), "author", sha)
        )
        committer = committer_email.strip().lower()
        if committer not in IGNORED_COMMITTER_EMAILS:
            identities.append(
                Identity(committer, committer_name.strip(), "committer", sha)
            )
        for match in _COAUTHOR.finditer(body):
            identities.append(
                Identity(
                    match.group("email").strip().lower(),
                    match.group("name").strip(),
                    "co-author",
                    sha,
                )
            )
    return identities


def load_register(path: Path) -> dict:
    if not path.exists():
        raise RegisterError(f"{path} does not exist")
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise RegisterError(f"{path} is not valid JSON: {exc}") from exc


def _emails_of(entry: dict) -> list[str]:
    raw = entry.get("emails", [])
    if not isinstance(raw, list):
        return []
    return [e.strip().lower() for e in raw if isinstance(e, str) and e.strip()]


def _validate_signature_fields(
    where: str, signature: dict, revisions: list[str]
) -> list[str]:
    """Required fields, the agreement sentence, the date form and the revision of one signature."""
    errors: list[str] = []
    for field in _SIGNATURE_FIELDS:
        value = signature.get(field)
        if not isinstance(value, str) or not value.strip():
            errors.append(f"{where}: field `{field}` is missing or empty")
    if signature.get("agreement") != REQUIRED_AGREEMENT:
        errors.append(f"{where}: `agreement` must be exactly {REQUIRED_AGREEMENT!r}")
    date = signature.get("date")
    if isinstance(date, str) and not _DATE.match(date):
        errors.append(f"{where}: `date` {date!r} is not YYYY-MM-DD")
    revision = signature.get("cla_revision")
    if isinstance(revision, str) and revision not in revisions:
        errors.append(f"{where}: unknown `cla_revision` {revision!r}")
    return errors


def _validate_signature_emails(
    where: str, signature: dict, owner_emails: set[str], claimed: dict[str, str]
) -> list[str]:
    """Emails of one signature: present, not the owner's, not claimed elsewhere already."""
    errors: list[str] = []
    emails = _emails_of(signature)
    if not emails:
        errors.append(f"{where}: field `emails` is missing or empty")
    for email in emails:
        if email in owner_emails:
            errors.append(f"{where}: {email} is already an owner address")
        if email in claimed:
            errors.append(f"{where}: {email} is already claimed by {claimed[email]}")
        else:
            claimed[email] = where
    return errors


def validate_register(register: dict) -> list[str]:
    """Return every structural error in the register; an empty list means valid."""
    errors: list[str] = []

    revisions = register.get("cla_revisions")
    if not isinstance(revisions, list) or not revisions:
        return ["register: `cla_revisions` is missing or empty"]
    current = register.get("current_cla_revision")
    if current not in revisions:
        errors.append(
            f"register: `current_cla_revision` {current!r} is not in `cla_revisions`"
        )

    owner = register.get("owner")
    if not isinstance(owner, dict) or not _emails_of(owner):
        errors.append("register: `owner` is missing or has no `emails`")
    owner_emails = set(_emails_of(owner) if isinstance(owner, dict) else [])

    bots = register.get("bots")
    if not isinstance(bots, list):
        errors.append("register: `bots` is missing or not a list")

    signatures = register.get("signatures")
    if not isinstance(signatures, list):
        return [*errors, "register: `signatures` is missing or not a list"]

    claimed: dict[str, str] = {}
    for index, signature in enumerate(signatures):
        where = f"signatures[{index}]"
        if not isinstance(signature, dict):
            errors.append(f"{where}: not an object")
            continue
        errors.extend(_validate_signature_fields(where, signature, revisions))
        errors.extend(
            _validate_signature_emails(where, signature, owner_emails, claimed)
        )

    return errors


def _allowlisted_emails(register: dict) -> set[str]:
    """The addresses covered outside `signatures`: owner and bots.

    Separate from `_covered_emails` because exactly these two fields may never grow in
    the same pull request — see `check_append_only`.
    """
    covered: set[str] = set()
    owner = register.get("owner")
    if isinstance(owner, dict):
        covered.update(_emails_of(owner))
    for bot in register.get("bots", []):
        if isinstance(bot, dict) and isinstance(bot.get("email"), str):
            email = bot["email"].strip().lower()
            if email:
                covered.add(email)
    return covered


def _covered_emails(register: dict) -> set[str]:
    """Every address that needs no signature any more: owner, bots, signers."""
    covered = _allowlisted_emails(register)
    for signature in register.get("signatures", []):
        if isinstance(signature, dict):
            covered.update(_emails_of(signature))
    return covered


def unsigned(identities: list[Identity], register: dict) -> list[Identity]:
    """The identities without coverage, one per address, in order of appearance."""
    covered = _covered_emails(register)
    missing: list[Identity] = []
    seen: set[str] = set()
    for identity in identities:
        if identity.email in covered or identity.email in seen:
            continue
        seen.add(identity.email)
        missing.append(identity)
    return missing


def _cell(value: str) -> str:
    """Make a name or address safe for a markdown table cell.

    Name and email come from the commit and are thus chosen by the contributor. A `|`
    breaks the column count, a backtick breaks out of the code span, and a newline
    breaks the row — all in exactly the report the maintainer relies on to see who did
    *not* sign. Escape, do not trust.
    """
    escaped = value.replace("\\", "\\\\").replace("|", "\\|").replace("`", "'")
    return " ".join(escaped.split())


def render_report(missing: list[Identity]) -> str:
    """Markdown: who is missing, and what that person has to do."""
    lines = [
        "## CLA: not every contributor has signed",
        "",
        "| Address | Name | Role | Commit |",
        "| --- | --- | --- | --- |",
    ]
    for identity in missing:
        email = _cell(identity.email) or "*(empty)*"
        name = _cell(identity.name) or "*(empty)*"
        commit = _cell(identity.commit)
        lines.append(f"| `{email}` | `{name}` | {identity.role} | `{commit}` |")
    lines += [
        "",
        "Every address above must be covered before this pull request can be merged.",
        "Signing takes one commit: add your own entry to `.github/cla-signatures.json`.",
        "The format and the two routes are in",
        # A full URL, not a relative path: in a job summary that does not resolve.
        f"[`CLA.md`]({repo_url()}/blob/main/CLA.md),",
        "section 11 — you may sign in this pull request.",
        "",
        "If an address above is yours but you do not recognise it, it is probably the",
        "address in your git configuration (`git config user.email`).",
        "Add the address as it stands here, or change your commits.",
        "",
        "An empty address comes from a commit without an author email. It cannot be",
        "covered and must be rewritten (`git commit --amend --reset-author`).",
    ]
    return "\n".join(lines) + "\n"


def _emit(report: str) -> None:
    """To stdout, and to the job summary when running in Actions."""
    print(report)
    summary = os.environ.get("GITHUB_STEP_SUMMARY")
    if summary:
        with open(summary, "a", encoding="utf-8") as handle:
            handle.write(report)


def _signature_key(signature: dict) -> str:
    """A stable key over a whole signature, independent of the key order.

    `sort_keys` normalises only the order of the JSON keys, not that of a nested list: a
    reordered `emails` list therefore reads as a change. That is the safe direction — it
    fails closed — but it is not "a whole entry".
    """
    return json.dumps(signature, sort_keys=True, ensure_ascii=False)


def _lost_signatures(previous: dict, register: dict, base: str) -> list[str]:
    """Every signature on `base` that does not come back unchanged in HEAD."""
    errors: list[str] = []
    current = {
        _signature_key(s) for s in register.get("signatures", []) if isinstance(s, dict)
    }
    for signature in previous.get("signatures", []):
        if not isinstance(signature, dict):
            continue
        if _signature_key(signature) not in current:
            who = signature.get("github_login") or signature.get("name") or "?"
            errors.append(
                f"the signature of `{who}` was removed or changed compared to `{base}`"
            )
    return errors


def check_append_only(
    repo: Path, base: str, register_path: Path, register: dict
) -> list[str]:
    """Every signature and revision on the base branch must stand unchanged in HEAD.

    If the register does not exist on the base branch yet, this is the pull request that
    introduces it and there is nothing to compare.
    """
    try:
        relative = register_path.resolve().relative_to(repo.resolve()).as_posix()
    except ValueError:
        # The register lies outside the repository (tests only). Nothing to compare.
        return []

    try:
        raw = _git(repo, "show", f"{base}:{relative}")
    except GitError as exc:
        if "does not exist" in str(exc) or "exists on disk, but not in" in str(exc):
            return []
        raise

    try:
        previous = json.loads(raw)
    except json.JSONDecodeError as exc:
        return [f"the register on `{base}` is not valid JSON: {exc}"]

    errors = _lost_signatures(previous, register, base)

    current_revisions = set(register.get("cla_revisions", []))
    for revision in previous.get("cla_revisions", []):
        if revision not in current_revisions:
            errors.append(
                f"revision `{revision}` was removed from `cla_revisions`; that "
                "invalidates every signature on that revision"
            )

    # `owner` and `bots` give coverage *without* a signature, and the gate reads the
    # register from the working tree. Without this check a contributor can add
    # themselves to `owner.emails` or to `bots` in the same pull request and be covered
    # at once — no agreement sentence, no validation, nothing. Only `signatures` may
    # grow in a pull request; that route is the intended one. Shrinking is fine:
    # removing a bot or correcting a name covers nobody extra.
    for email in sorted(_allowlisted_emails(register) - _allowlisted_emails(previous)):
        errors.append(
            f"`{email}` was added to `owner` or `bots` compared to `{base}`; coverage "
            "outside `signatures` belongs in a pull request of its own by the "
            "maintainer, not in this one"
        )

    return errors


def parse_args(argv: list[str] | None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--repo", type=Path, default=Path("."))
    parser.add_argument("--base", default="origin/main")
    parser.add_argument("--head", default="HEAD")
    parser.add_argument(
        "--register",
        type=Path,
        default=None,
        help="default: <repo>/.github/cla-signatures.json",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    repo: Path = args.repo
    register_path: Path = args.register or repo / ".github" / "cla-signatures.json"

    # An empty `--base` or `--head` makes the range `git log ..` — that is `HEAD..HEAD`,
    # so empty, so "zero identities, all covered" and exit 0. A green run that measured
    # nothing is exactly the outcome this gate must not be able to give. Unreachable
    # today (only `pull_request` fills in the SHAs), but the rest of this script fails
    # closed everywhere and this was the one place where that did not hold by itself.
    if not args.base or not args.head:
        _emit(
            "## CLA: no range to measure\n\n"
            f"`--base` is {args.base!r} and `--head` is {args.head!r}; without a range a "
            "green outcome says nothing.\n"
        )
        return 2

    try:
        register = load_register(register_path)
    except RegisterError as exc:
        _emit(f"## CLA: the register is unreadable\n\n```\n{exc}\n```\n")
        return 2

    errors = validate_register(register)
    if errors:
        body = "\n".join(f"- {e}" for e in errors)
        _emit(f"## CLA: the register is invalid\n\n{body}\n")
        return 2

    try:
        errors = check_append_only(repo, args.base, register_path, register)
        if errors:
            body = "\n".join(f"- {e}" for e in errors)
            _emit(
                "## CLA: the register was changed, not appended to\n\n"
                "A signature that disappears or changes is a silently withdrawn grant. "
                "Only additions are allowed.\n\n"
                f"{body}\n"
            )
            return 2
        identities = collect_identities(repo, args.base, args.head)
    except GitError as exc:
        _emit(f"## CLA: git failed\n\n```\n{exc}\n```\n")
        return 2

    missing = unsigned(identities, register)
    if missing:
        _emit(render_report(missing))
        return 1

    print(f"CLA: {len(identities)} identities in {args.base}..{args.head}, all covered.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
