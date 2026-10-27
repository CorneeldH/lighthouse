"""`lighthouse_lib.protocol`: the `🤖 <kind>` comments on an issue (decision 15, 21, 25).

Taken over from the source repository's `test_controller_comments.py`. Dropped there,
because they test controller policy that stays in the source repository (plan, taak
8): the allowlist (`beoordeel_commando`), `is_antwoord`, `antwoord_prompt`, the lane
check in `parse_handoff`, and the prose of `render_klaar`.

Most tests write with `[project] language = "nl"`, so the rendered text compares
literally with the comments that already stand on issues; `TestLanguage` covers `en`.
"""

import json
import re
from pathlib import Path
from collections.abc import Sequence

import pytest

from lighthouse_lib.protocol import (
    KINDS,
    LEGACY_MARKERS,
    LETTERS,
    MARKER,
    MAX_QUESTIONS,
    ROBOT,
    Comment,
    Handoff,
    HandoffError,
    _clean,
    active_stack,
    fill_started,
    footer,
    parse_handoff,
    parse_header,
    parse_question,
    questions_of,
    render_done,
    render_question,
    render_rebased,
    render_resumed,
    render_returned,
    render_started,
    stack_base,
)

UUID = "0b7e3c1a-1111-4222-8333-444455556666"
OTHER = "0b7e3c1a-9999-4222-8333-444455556666"


@pytest.fixture
def config(tmp_path, monkeypatch):
    """Write `lighthouse.toml` with `body` and point `LIGHTHOUSE_TOML` at it."""

    def write(body: str) -> None:
        path = tmp_path / "lighthouse.toml"
        path.write_text(body, encoding="utf-8")
        monkeypatch.setenv("LIGHTHOUSE_TOML", str(path))

    return write


@pytest.fixture(autouse=True)
def nl_env(config):
    config('[project]\nlanguage = "nl"\n')


def test_public_names():
    assert ROBOT == "🤖"
    assert MARKER == "<!-- lighthouse:"
    assert LEGACY_MARKERS == ("<!-- paia:",)
    assert LETTERS == "abcdef"
    assert MAX_QUESTIONS == 4
    assert KINDS == (
        "handoff",
        "started",
        "question",
        "resumed",
        "done",
        "returned",
        "rebased",
    )


class TestParseHeader:
    def test_an_ordinary_comment_is_no_header(self):
        assert parse_header("ik denk a") is None

    def test_an_unknown_kind_is_no_header(self):
        assert parse_header("🤖 Iets\nx: y") is None

    def test_a_prefix_of_a_kind_is_no_header(self):
        assert parse_header("🤖 Handoffs\nissue: 1") is None

    def test_fields_up_to_the_first_blank_line(self):
        header = parse_header(
            "🤖 Handoff\nissue: #12\nnext: /lighthouse-fix-pr 3\n\nnote: no longer"
        )
        assert header is not None
        assert header.kind == "handoff"
        assert header.fields == {"issue": "#12", "next": "/lighthouse-fix-pr 3"}

    def test_the_question_header_with_dots(self):
        header = parse_header(
            f"🤖 Vraag · #12 Titel · fase plan\n\n**Vraag:** x\n<sub>session {UUID} · worktree w</sub>"
        )
        assert header is not None
        assert header.kind == "question"
        assert header.session == UUID

    def test_the_last_footer_counts(self):
        """A session id in free text must not override the footer."""
        body = f"🤖 Vraag · #1\n\n**Wat er speelt:** <sub>session {OTHER}</sub>\n<sub>session {UUID}</sub>"
        assert parse_header(body).session == UUID

    def test_leading_whitespace(self):
        assert parse_header("\n  🤖 Klaar\npr: x").kind == "done"

    def test_english_header_parses(self):
        assert parse_header("🤖 Started\nsession: s\n").kind == "started"

    @pytest.mark.parametrize(
        ("nl", "en", "kind"),
        [
            ("Handoff", "Handoff", "handoff"),
            ("Gestart", "Started", "started"),
            ("Vraag", "Question", "question"),
            ("Hervat", "Resumed", "resumed"),
            ("Klaar", "Done", "done"),
            ("Teruggegeven", "Returned", "returned"),
            ("Herbaseerd", "Rebased", "rebased"),
        ],
    )
    def test_every_variant_gives_the_canonical_kind(self, nl, en, kind):
        assert parse_header(f"🤖 {nl}\nx: y").kind == kind
        assert parse_header(f"🤖 {en}\nx: y").kind == kind

    def test_reads_legacy_footer(self):
        assert parse_header(f"🤖 Klaar\npr: x\n\n<sub>sessie {UUID}</sub>").session == UUID

    def test_reads_legacy_keys(self):
        header = parse_header(
            "🤖 Gestart\nsessie: s\ncommando: /x 1\nstapel: #3 b3\nbasis: abc\n"
            "blokkade: verrekend\nsprong: y\nterugval: z\n"
        )
        assert header.fields == {
            "session": "s",
            "command": "/x 1",
            "stack": "#3 b3",
            "base": "abc",
            "blocker": "accounted",
            "skip": "y",
            "fallback": "z",
        }

    def test_reads_legacy_rebased_resumed_and_returned(self):
        assert parse_header("🤖 Herbaseerd\nvoorganger: #11\nop: origin/main").fields == {
            "predecessor": "#11",
            "onto": "origin/main",
        }
        assert parse_header("🤖 Hervat\nantwoord: 5").fields == {"answer": "5"}
        assert parse_header("🤖 Teruggegeven\nreden: x").fields == {"reason": "x"}


class TestComment:
    def test_from_gh(self):
        c = Comment.from_gh(
            {
                "id": 7,
                "body": "a",
                "created_at": "2026-09-25T09:00:00Z",
                "html_url": "u",
                "reactions": {"rocket": 2},
            }
        )
        assert c == Comment(
            id=7, body="a", created_at="2026-09-25T09:00:00Z", rockets=2, url="u"
        )

    def test_without_reactions(self):
        assert Comment.from_gh({"id": 7, "body": None}).rockets == 0


def _handoff(**fields):
    base = {
        "issue": "#12",
        "worktree": "/w/",
        "branch": "issue-12-x",
        "anchor": "/w/spec.md",
        "next": "/superpowers:writing-plans /w/spec.md",
        "when": "now",
    }
    base.update(fields)
    return parse_header(
        "🤖 Handoff\n" + "\n".join(f"{k}: {v}" for k, v in base.items() if v is not None)
    )


def _handoff_text() -> str:
    return "\n".join(
        f"{k}: {v}"
        for k, v in {
            "issue": "#12",
            "worktree": "/w/",
            "branch": "issue-12-x",
            "anchor": "/w/spec.md",
            "next": "/superpowers:writing-plans /w/spec.md",
            "when": "now",
        }.items()
    )


class TestHandoff:
    def test_a_complete_handoff(self):
        h = parse_handoff(_handoff())
        assert (h.issue, h.worktree, h.branch, h.anchor, h.when) == (
            12,
            "/w",
            "issue-12-x",
            "/w/spec.md",
            "now",
        )

    @pytest.mark.parametrize(
        "fields",
        [
            {"next": None},
            {"when": "tomorrow"},
            {"issue": "twelve"},
            {"worktree": None},
        ],
    )
    def test_incomplete_or_unreadable(self, fields):
        with pytest.raises(HandoffError):
            parse_handoff(_handoff(**fields))

    def test_lane_not_validated(self):
        header = parse_header(
            "🤖 Handoff\nissue: 1\nworktree: w\nbranch: b\nnext: /x\nwhen: now\nlane: anything\n"
        )
        assert parse_handoff(header).lane == "anything"

    def test_handoff_with_auto_lines_parses_unchanged(self):
        auto = (
            "Context: chosen without a question\n"
            "Auto: technical-choice — sqlite or json? → sqlite\n"
            "Auto: intent-scope — the CLI too? → yes"
        )
        without = parse_header(
            "🤖 Handoff\n" + _handoff_text() + "\nContext: chosen without a question"
        )
        with_auto = parse_header("🤖 Handoff\n" + _handoff_text() + "\n" + auto)
        assert parse_handoff(with_auto) == parse_handoff(without)

    def test_no_handoff(self):
        with pytest.raises(HandoffError):
            parse_handoff(parse_header("🤖 Klaar\npr: x"))

    def test_reads_legacy_handoff(self):
        body = "🤖 Handoff\nissue: #12\nworktree: /w/x/\nbranch: b\nvolgende: /x\nwanneer: nacht\nanker: a\n"
        assert parse_handoff(parse_header(body)) == Handoff(
            12, "/w/x", "b", "a", "/x", "night"
        )

    def test_reads_legacy_now(self):
        body = "🤖 Handoff\nissue: 1\nworktree: w\nbranch: b\nvolgende: /x\nwanneer: nu\n"
        assert parse_handoff(parse_header(body)).when == "now"


class TestRender:
    def test_started_reads_back(self):
        header = parse_header(
            render_started(
                session=UUID,
                worktree="/w",
                command="/lighthouse-fix-pr 7",
                start="t",
                head_sha="b" * 40,
            )
        )
        assert header.kind == "started"
        assert header.session == UUID
        assert header.fields["command"] == "/lighthouse-fix-pr 7"
        assert header.fields["head"] == "b" * 40

    def test_render_started_with_stack(self):
        body = render_started(
            session=UUID,
            worktree="/w",
            command="/lighthouse-start-issue 12",
            start="t",
            stack="#11 issue-11-x",
            base="c" * 40,
            branch="issue-12-y",
        )
        header = parse_header(body)
        assert header.fields["stack"] == "#11 issue-11-x"
        assert header.fields["base"] == "c" * 40
        assert header.fields["branch"] == "issue-12-y"
        assert active_stack([body]) == header

    def test_render_started_without_stack_has_no_stack_fields(self):
        header = parse_header(
            render_started(session=UUID, worktree="/w", command="c", start="t")
        )
        assert not {"stack", "base", "branch", "blocker"} & header.fields.keys()

    def test_render_started_with_blocker(self):
        header = parse_header(
            render_started(
                session=UUID, worktree="/w", command="c", start="t", blocker="accounted"
            )
        )
        assert header.fields["blocker"] == "accounted"
        assert "base" not in header.fields

    def test_render_started_with_runner_workspace_and_fallback(self):
        header = parse_header(
            render_started(
                session=UUID,
                worktree="/w",
                command="c",
                start="t",
                runner="forge",
                workspace="ws1",
                fallback="x",
            )
        )
        assert (
            header.fields["runner"],
            header.fields["workspace"],
            header.fields["fallback"],
        ) == ("forge", "ws1", "x")

    def test_render_started_without_runner_has_no_forge_fields(self):
        header = parse_header(
            render_started(session=UUID, worktree="/w", command="c", start="t")
        )
        assert not {"runner", "workspace", "fallback"} & header.fields.keys()

    def test_fill_started_sets_runner_workspace_and_fallback_and_keeps_the_footer(self):
        body = render_started(
            session=UUID,
            worktree="",
            command="/lighthouse-start-issue 12",
            start="t",
            runner="forge",
            workspace="ws1",
        )
        new = fill_started(
            body,
            worktree="/u/.kenn/forge/worktrees/x",
            runner="headless",
            workspace="",
            fallback="forge not available:\nkeyring closed",
        )
        header = parse_header(new)
        assert header.kind == "started"
        assert header.fields["worktree"] == "/u/.kenn/forge/worktrees/x"
        assert header.fields["runner"] == "headless"
        assert "workspace" not in header.fields
        # A field is one line: a newline in the value does not break the header open.
        assert header.fields["fallback"] == "forge not available: keyring closed"
        assert header.fields["command"] == "/lighthouse-start-issue 12"
        assert header.session == UUID
        assert new.endswith("\n\n" + footer(UUID))
        # An existing field keeps its place.
        assert new.splitlines()[1:3] == [
            f"session: {UUID}",
            "worktree: /u/.kenn/forge/worktrees/x",
        ]

    def test_fill_started_puts_a_new_field_at_the_end_of_the_header(self):
        body = render_started(session=UUID, worktree="/w", command="c", start="t")
        new = fill_started(body, runner="forge", workspace="ws1")
        header = parse_header(new)
        assert (header.fields["runner"], header.fields["workspace"]) == ("forge", "ws1")
        assert new.splitlines()[5:7] == ["runner: forge", "workspace: ws1"]
        assert fill_started(new, runner="forge", workspace="ws1") == new

    def test_started_with_skip(self):
        body = render_started(
            session=UUID,
            worktree="/w",
            command="c",
            start="t",
            skip="spec-review skipped (config)",
        )
        assert "skip: spec-review skipped (config)" in body.splitlines()
        assert parse_header(body).fields["skip"] == "spec-review skipped (config)"
        without = render_started(session=UUID, worktree="/w", command="c", start="t")
        assert "skip:" not in without

    def test_render_rebased_parses_as_header(self):
        body = render_rebased(predecessor=11, base="c" * 40, session=UUID)
        assert body.splitlines()[0] == "🤖 Herbaseerd"
        header = parse_header(body)
        assert header.kind == "rebased"
        assert header.fields["base"] == "c" * 40
        assert header.fields["predecessor"] == "#11"
        assert header.fields["onto"] == "origin/main"
        assert header.session == UUID

    def test_render_rebased_follows_default_branch(self, config):
        config('[project]\nlanguage = "nl"\n[forge]\ndefault_branch = "trunk"\n')
        body = render_rebased(predecessor=11, base="c" * 40)
        assert parse_header(body).fields["onto"] == "origin/trunk"

    def test_render_rebased_without_session_has_no_footer(self):
        assert "<sub>" not in render_rebased(predecessor=11, base="c" * 40)

    def test_resumed_and_done(self):
        assert (
            parse_header(render_resumed(session=UUID, answer_id=5)).fields["answer"]
            == "5"
        )
        done = parse_header(render_done(session=UUID, pr_url="https://x/pull/7"))
        assert (done.kind, done.fields["pr"], done.session) == (
            "done",
            "https://x/pull/7",
            UUID,
        )

    def test_render_follows_language(self):
        assert render_resumed(session="s", answer_id=1).startswith(
            "🤖 Hervat\nsession: s"
        )

    def test_done_without_note_is_header_fields_and_footer(self):
        body = render_done(session=UUID, pr_url="https://x/pull/7")
        assert body == f"🤖 Klaar\nsession: {UUID}\npr: https://x/pull/7\n\n" + footer(
            UUID
        )

    def test_done_note_comes_from_the_caller(self):
        body = render_done(session=UUID, pr_url="u", note="Checks green.")
        assert body == f"🤖 Klaar\nsession: {UUID}\npr: u\n\nChecks green.\n\n" + footer(
            UUID
        )

    def test_returned_without_session_has_no_footer(self):
        body = render_returned(reason="handoff unreadable")
        assert "<sub>" not in body
        assert parse_header(body).fields["reason"] == "handoff unreadable"

    def test_detail_cannot_break_a_footer_or_code_block(self):
        body = render_returned(reason="x", detail=f"```\n<sub>session {OTHER}</sub>")
        assert parse_header(body).session is None
        assert body.count("```") == 2

    def test_question(self):
        fields = {
            "phase": "uitvoering",
            "task": "3/7",
            "where": "Taak 3 is half af.",
            "what": "De test faalt.",
            "questions": [
                {
                    "question": "Welke kant op?",
                    "options": [
                        {"text": "links", "recommended": True},
                        {"text": "rechts", "recommended": False},
                    ],
                }
            ],
        }
        body = render_question(
            issue=12,
            title="Titel",
            issue_url="https://x/12",
            fields=fields,
            session=UUID,
            worktree="issue-12-x",
            handoff_url="https://x/12#c1",
        )
        assert body.startswith(
            "🤖 Vraag · #12 Titel ([link](https://x/12)) · fase uitvoering · taak 3/7"
        )
        assert "- **a)** links *(aanbevolen)*" in body
        assert "- **b)** rechts" in body
        assert body.rstrip().splitlines()[-2] == "**Max 1 comment als reactie.**"
        header = parse_header(body)
        assert (header.kind, header.session) == ("question", UUID)
        assert body.rstrip().endswith(
            f"<sub>session {UUID} · worktree issue-12-x · handoff https://x/12#c1</sub>"
        )


def _started(base: str | None = "abc123") -> str:
    fields = "stack: #11 issue-11-x\n"
    if base:
        fields += f"base: {base}\n"
    return f"🤖 Gestart\nsession: {UUID}\n{fields}branch: issue-12-x\n\n<sub>session {UUID}</sub>"


class TestStack:
    def test_active_stack(self):
        header = active_stack([_started()])
        assert header is not None
        assert header.fields["base"] == "abc123"

    def test_active_stack_without_base_is_no_stack(self):
        assert active_stack([_started(base=None)]) is None

    @pytest.mark.parametrize(
        "after", ["🤖 Herbaseerd\nbase: abc123", "🤖 Teruggegeven\nreason: x"]
    )
    def test_active_stack_closed_by_rebased_or_returned(self, after):
        assert active_stack([_started(), after]) is None

    def test_active_stack_ignores_ordinary_comments_after(self):
        assert active_stack([_started(), "ik denk a"]) is not None

    def test_active_stack_empty(self):
        assert active_stack([]) is None

    @pytest.mark.parametrize(
        "after", ["🤖 Herbaseerd\nbase: abc123", "🤖 Teruggegeven\nreason: x"]
    )
    def test_stack_base_whatever_comes_after(self, after):
        assert stack_base([_started(), after]) == "abc123"

    def test_stack_base_without_started_with_base(self):
        assert stack_base([_started(base=None), "ik denk a"]) is None

    def test_stack_base_last_started_with_base_wins(self):
        assert stack_base([_started("aaa"), _started(base=None), _started("bbb")]) == "bbb"

    def test_rebased_is_a_kind(self):
        assert parse_header("🤖 Herbaseerd\nbase: abc").kind == "rebased"

    def test_reads_legacy_started_stack(self):
        assert stack_base(["🤖 Gestart\nsessie: s\nstapel: #3 b3\nbasis: abc\n"]) == "abc"

    def test_reads_english_started_stack(self):
        assert stack_base(["🤖 Started\nsession: s\nstack: #3 b3\nbase: abc\n"]) == "abc"


def _cleaned(fields: dict) -> dict:
    """What parse_question returns: the input as render_question cleans it."""
    return {
        "phase": _clean(str(fields.get("phase") or "?")),
        "task": _clean(str(fields.get("task") or "")),
        "where": _clean(fields["where"]),
        "what": _clean(fields["what"]),
        "questions": [
            {
                "question": _clean(q["question"]),
                "options": [
                    {"text": _clean(o["text"]), "recommended": bool(o.get("recommended"))}
                    for o in q["options"][:6]
                ],
            }
            for q in fields["questions"][:4]
        ],
        "commands": list(fields.get("commands") or []),
    }


def _question_body(fields: dict, language: str | None = None) -> str:
    return render_question(
        issue=12,
        title="Titel",
        issue_url="https://x/12",
        fields=fields,
        session=UUID,
        worktree="issue-12-x",
        language=language,
    )


def _fields(**extra) -> dict:
    """Base fields with one question; `question=`/`options=` adjust that one question,
    `questions=` replaces the whole bundle."""
    question = {
        "question": extra.pop("question", "welke?"),
        "options": extra.pop("options", [{"text": "links", "recommended": False}]),
    }
    base = {"where": "in de plan-fase", "what": "twee wegen", "questions": [question]}
    return base | extra


QUESTION = (
    "🤖 Vraag · #12 Titel ([link](https://x/12)) · fase plan\n\n"
    "**Waar het staat:** in de plan-fase\n**Wat er speelt:** twee wegen\n"
    "**Vraag:** welke?\n- **a)** links *(aanbevolen)*\n- **b)** rechts\n\n"
    f"**Max 1 comment als reactie.**\n<sub>session {UUID} · worktree issue-12-x</sub>"
)
# The same question as it stands on issues from before the move.
LEGACY_QUESTION = QUESTION.replace("<sub>session", "<sub>sessie")

ONE_QUESTION = [
    {
        "question": "welke?",
        "options": [
            {"text": "links", "recommended": True},
            {"text": "rechts", "recommended": False},
        ],
    }
]


def test_questions_of_reads_old_classification():
    old = {"vraag": "z?", "opties": [{"tekst": "een", "aanbevolen": True}]}
    assert questions_of(old) == [
        {"question": "z?", "options": [{"text": "een", "recommended": True}]}
    ]


def test_questions_of_reads_old_bundle():
    old = {"vragen": [{"vraag": "z?", "opties": [{"tekst": "een", "aanbevolen": False}]}]}
    assert questions_of(old) == [
        {"question": "z?", "options": [{"text": "een", "recommended": False}]}
    ]


def test_render_question_reads_old_fields():
    """A classification on disk from before the move keeps rendering."""
    old = {
        "fase": "plan",
        "waar_het_staat": "in de plan-fase",
        "wat_er_speelt": "twee wegen",
        "vragen": [
            {
                "vraag": "welke?",
                "opties": [
                    {"tekst": "links", "aanbevolen": True},
                    {"tekst": "rechts", "aanbevolen": False},
                ],
            }
        ],
    }
    assert _question_body(old) == QUESTION


class TestParseQuestion:
    def test_legacy_question_parses_as_bundle_of_one(self):
        assert parse_question(LEGACY_QUESTION)["questions"] == ONE_QUESTION

    def test_one_question_renders_as_before(self):
        assert _question_body(_fields(phase="plan", questions=ONE_QUESTION)) == QUESTION

    def test_bundle_numbers_questions_and_options(self):
        body = _question_body(
            _fields(
                questions=[
                    {
                        "question": "eerst?",
                        "options": [
                            {"text": "x", "recommended": True},
                            {"text": "y", "recommended": False},
                        ],
                    },
                    {
                        "question": "dan?",
                        "options": [
                            {"text": "p", "recommended": False},
                            {"text": "q", "recommended": True},
                        ],
                    },
                ]
            )
        )
        assert "**Vraag 1:** eerst?" in body and "**Vraag 2:** dan?" in body
        # Without a blank line, Question 2 hangs as a lazy continuation in the last option.
        assert "\n\n**Vraag 2:**" in body
        assert "\n\n**Vraag 1:**" not in body
        assert "- **1a)** x *(aanbevolen)*" in body and "- **2b)** q *(aanbevolen)*" in body
        assert "Antwoord per vraag, bijvoorbeeld `1a, 2c`." in body
        assert "**Vraag:**" not in body

    def test_roundtrip_maximal_bundle(self):
        questions = [
            {
                "question": f"vraag {n}\n\nmet lege regel",
                "options": [
                    {"text": f"optie {n}{letter}\ntweede regel", "recommended": letter == "c"}
                    for letter in "abcdef"
                ],
            }
            for n in range(1, 5)
        ]
        self._roundtrip(_fields(phase="uitvoering", task="2/5", questions=questions))

    def _roundtrip(self, fields: dict, language: str | None = None) -> None:
        assert parse_question(_question_body(fields, language)) == _cleaned(fields)

    def test_roundtrip_one_option(self):
        self._roundtrip(_fields(phase="plan"))

    def test_roundtrip_six_options_with_recommended(self):
        options = [
            {"text": f"optie {letter}", "recommended": letter == "b"} for letter in "abcdef"
        ]
        self._roundtrip(_fields(phase="uitvoering", task="3/7", options=options))

    def test_roundtrip_without_phase(self):
        fields = _fields()
        assert parse_question(_question_body(fields))["phase"] == "?"
        self._roundtrip(fields)

    def test_free_text_with_asterisks(self):
        self._roundtrip(
            _fields(
                question="kies **a** of **b**?",
                options=[{"text": "**vet** bedoeld", "recommended": False}],
            )
        )

    def test_code_block_is_cleaned(self):
        fields = _fields(what="zie\n```\nx\n```")
        result = parse_question(_question_body(fields))
        assert result["what"] == "zie\n'''\nx\n'''"
        assert result == _cleaned(fields)

    def test_multiline_option(self):
        self._roundtrip(
            _fields(
                options=[
                    {"text": "eerste regel\ntweede regel", "recommended": True},
                    {"text": "andere", "recommended": False},
                ]
            )
        )

    def test_blank_line_in_free_text(self):
        """A blank line in the free text does not end the block."""
        fields = _fields(what="a\n\nb")
        result = parse_question(_question_body(fields))
        assert result["what"] == "a\n\nb"
        assert result == _cleaned(fields)

    def test_blank_line_in_last_option(self):
        self._roundtrip(_fields(options=[{"text": "a\n\nb", "recommended": True}]))

    def test_no_question_gives_none(self):
        assert parse_question("ik denk a") is None
        assert parse_question(render_done(session=UUID, pr_url="u")) is None

    def test_roundtrip_in_english(self):
        fields = _fields(phase="plan", task="1/2", questions=ONE_QUESTION, commands=[RPM])
        self._roundtrip(fields, "en")

    def test_english_question_parses_in_a_dutch_repo(self):
        """Reading accepts every variant, whatever `[project] language` says."""
        body = _question_body(_fields(phase="plan", questions=ONE_QUESTION), "en")
        assert body.startswith("🤖 Question · #12 Titel ([link](https://x/12)) · phase plan")
        assert "**Where it stands:** in de plan-fase" in body
        assert "- **a)** links *(recommended)*" in body
        assert "**At most 1 comment in reply.**" in body
        assert parse_question(body) == _cleaned(_fields(phase="plan", questions=ONE_QUESTION))


RPM = "sudo dnf install -y /tmp/orca-ide-1.0.rpm"
FIREWALL = "sudo bash deploy/host/firewall.sh &&\n  sudo firewall-cmd --list-rich-rules | grep 6768"

_FENCE = re.compile(r"^( *)(`{3,}|~{3,})(.*)$")


def markdown_shape_errors(text: str, commands: Sequence[str] = ()) -> list[str]:
    """The shape of a question text: no enclosing code block, no indented code block
    outside a fence, every fence closed, and every command literally as the content of
    a fenced code block. Empty = the text renders as markdown with live links.

    Taken over from the source repository's `tests/helpers/controller.py`."""
    errors: list[str] = []
    lines = text.splitlines()
    first = next((line for line in lines if line.strip()), "")
    if _FENCE.match(first):
        errors.append("the text starts with a code block")
    blocks: list[str] = []
    open_: tuple[int, str] | None = None
    content: list[str] = []
    for line in lines:
        if open_ is not None:
            indent, char = open_
            bare = line.strip()
            if bare and set(bare) == {char[0]} and len(bare) >= len(char):
                blocks.append("\n".join(content))
                open_, content = None, []
            else:
                content.append(line[indent:] if line[:indent].isspace() else line)
            continue
        match = _FENCE.match(line)
        if match:
            open_ = (len(match.group(1)), match.group(2))
        elif line.startswith("    ") and line.strip():
            errors.append(f"indented code block: {line!r}")
    if open_ is not None:
        errors.append("a code block does not close")
    errors += [f"command not as code block: {c!r}" for c in commands if c not in blocks]
    return errors


class TestQuestionShape:
    """The question renders as markdown, and every command to run as a code block."""

    def test_without_commands_no_code_block_and_valid_shape(self):
        body = _question_body(_fields())
        assert "```" not in body
        assert "**Zelf draaien:**" not in body
        assert markdown_shape_errors(body) == []

    def test_every_command_stands_literally_as_its_own_code_block(self):
        body = _question_body(_fields(commands=[RPM, FIREWALL]))
        assert f"**Zelf draaien:**\n\n```bash\n{RPM}\n```" in body
        assert markdown_shape_errors(body, [RPM, FIREWALL]) == []

    def test_commands_stand_between_question_and_options(self):
        body = _question_body(_fields(commands=[RPM]))
        assert body.index("**Vraag:**") < body.index(RPM) < body.index("- **a)**")

    def test_a_command_with_backticks_gets_a_longer_fence(self):
        cmd = "echo '```' && printf '%s' \"$(date)\""
        body = _question_body(_fields(commands=[cmd]))
        assert f"````bash\n{cmd}\n````" in body
        assert markdown_shape_errors(body, [cmd]) == []

    def test_free_text_cannot_open_a_tilde_code_block(self):
        body = _question_body(_fields(what="zie\n~~~\nx"))
        assert "~~~" not in body
        assert markdown_shape_errors(body) == []

    def test_roundtrip_with_commands(self):
        TestParseQuestion()._roundtrip(_fields(commands=[RPM, FIREWALL]))

    def test_a_command_that_looks_like_an_option_stays_a_command(self):
        cmd = "- **a)** geen optie"
        result = parse_question(_question_body(_fields(commands=[cmd])))
        assert result["commands"] == [cmd]
        assert [o["text"] for o in result["questions"][0]["options"]] == ["links"]


QUESTION_WITH_COMMAND = (
    "🤖 Vraag · #12 Titel ([link](https://x/12)) · fase plan\n\n"
    "**Waar het staat:** in de plan-fase\n**Wat er speelt:** twee wegen\n"
    f"**Vraag:** welke?\n\n**Zelf draaien:**\n\n```bash\n{RPM}\n```\n\n"
    "- **a)** links *(aanbevolen)*\n- **b)** rechts\n\n"
    f"**Max 1 comment als reactie.**\n<sub>session {UUID} · worktree issue-12-x</sub>"
)


def _two_questions() -> list[dict]:
    return [
        {
            "question": "eerst?",
            "options": [
                {"text": "x", "recommended": True},
                {"text": "y", "recommended": False},
            ],
        },
        {
            "question": "dan?",
            "options": [
                {"text": "p", "recommended": False},
                {"text": "q", "recommended": False},
            ],
        },
    ]


class TestBundleWithCommands:
    """One `commands` per question comment, shared by the bundle."""

    def test_one_question_with_command(self):
        fields = _fields(phase="plan", questions=ONE_QUESTION, commands=[RPM])
        assert _question_body(fields) == QUESTION_WITH_COMMAND

    def test_question_with_command_parses_as_bundle_of_one(self):
        result = parse_question(QUESTION_WITH_COMMAND)
        assert result["commands"] == [RPM]
        assert result["questions"] == ONE_QUESTION

    def test_bundle_puts_the_block_before_question_1(self):
        body = _question_body(_fields(questions=_two_questions(), commands=[RPM, FIREWALL]))
        assert (
            body.index("**Wat er speelt:**")
            < body.index("**Zelf draaien:**")
            < body.index(RPM)
            < body.index("**Vraag 1:**")
            < body.index("**Vraag 2:**")
        )
        assert body.count("**Zelf draaien:**") == 1
        close = body.index("**Max 1 comment als reactie.**")
        assert body.index("Antwoord per vraag, bijvoorbeeld `1a, 2c`.") > close

    def test_bundle_with_commands_roundtrips(self):
        TestParseQuestion()._roundtrip(
            _fields(phase="plan", questions=_two_questions(), commands=[RPM, FIREWALL])
        )

    def test_bundle_with_commands_has_valid_markdown_shape(self):
        body = _question_body(_fields(questions=_two_questions(), commands=[RPM, FIREWALL]))
        assert markdown_shape_errors(body, [RPM, FIREWALL]) == []


class TestLanguage:
    def test_english_repo_writes_english_headers(self, config):
        config('[project]\nlanguage = "en"\n')
        assert render_resumed(session="s", answer_id=1).startswith("🤖 Resumed\nsession: s")
        assert render_returned(reason="x").startswith("🤖 Returned\nreason: x")

    def test_explicit_language_wins(self):
        assert render_done(session="s", pr_url="u", language="en").startswith("🤖 Done\n")

    def test_without_config_an_explicit_language_still_renders(self, monkeypatch, tmp_path):
        monkeypatch.setenv("LIGHTHOUSE_TOML", str(tmp_path / "missing.toml"))
        assert render_started(
            session="s", worktree="w", command="c", start="t", language="en"
        ).startswith("🤖 Started\n")

    def test_missing_heading_table_is_loud(self, monkeypatch, tmp_path):
        from lighthouse_lib.config import ConfigError

        monkeypatch.setenv("LIGHTHOUSE_HEADINGS", str(tmp_path / "missing.toml"))
        with pytest.raises(ConfigError):
            parse_header("🤖 Klaar\npr: x")


def test_shared_header_fixture():
    """The Go parser (`internal/protocol`) reads the same fixture."""
    path = Path(__file__).parent / "fixtures/protocol/headers.json"
    for case in json.loads(path.read_text(encoding="utf-8")):
        header = parse_header(case["body"])
        if case["kind"] is None:
            assert header is None, case["name"]
            continue
        assert header is not None, case["name"]
        assert header.kind == case["kind"], case["name"]
        if "fields" in case:
            assert dict(header.fields) == case["fields"], case["name"]
