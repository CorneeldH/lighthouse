"""De 🤖-koppen van de controller (spec *Comment-typen*, *Vraag-format*; plan B5, B10)."""

import pytest

from src.controller.comments import (
    Comment,
    HandoffFout,
    _schoon,
    actieve_stapel,
    antwoord_prompt,
    beoordeel_commando,
    is_antwoord,
    parse_handoff,
    parse_kop,
    parse_vraag,
    render_gestart,
    render_herbaseerd,
    render_hervat,
    render_klaar,
    render_teruggegeven,
    render_vraag,
    stapel_basis,
    voet,
    vragen_van,
    vul_gestart,
)
from tests.helpers.controller import markdown_vormfouten

UUID = "0b7e3c1a-1111-4222-8333-444455556666"
ANDER = "0b7e3c1a-9999-4222-8333-444455556666"


class TestParseKop:
    def test_een_gewone_comment_is_geen_kop(self):
        assert parse_kop("ik denk a") is None

    def test_een_onbekende_soort_is_geen_kop(self):
        assert parse_kop("🤖 Iets\nx: y") is None

    def test_velden_tot_de_eerste_lege_regel(self):
        kop = parse_kop(
            "🤖 Handoff\nissue: #12\nvolgende: /paia-fix-pr 3\n\nnoot: niet meer"
        )
        assert kop is not None
        assert kop.soort == "Handoff"
        assert kop.velden == {"issue": "#12", "volgende": "/paia-fix-pr 3"}

    def test_de_vraagkop_met_puntjes(self):
        kop = parse_kop(
            f"🤖 Vraag · #12 Titel · fase plan\n\n**Vraag:** x\n<sub>sessie {UUID} · worktree w</sub>"
        )
        assert kop is not None
        assert kop.soort == "Vraag"
        assert kop.sessie == UUID

    def test_de_laatste_voet_telt(self):
        """Een sessie-id in vrije tekst mag de voet niet overschrijven."""
        body = f"🤖 Vraag · #1\n\n**Wat er speelt:** <sub>sessie {ANDER}</sub>\n<sub>sessie {UUID}</sub>"
        assert parse_kop(body).sessie == UUID

    def test_voorloopwitruimte(self):
        assert parse_kop("\n  🤖 Klaar\npr: x").soort == "Klaar"


class TestComment:
    def test_uit_gh(self):
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

    def test_zonder_reacties(self):
        assert Comment.from_gh({"id": 7, "body": None}).rockets == 0


def _handoff(**velden):
    base = {
        "issue": "#12",
        "worktree": "/w/",
        "branch": "issue-12-x",
        "anker": "/w/spec.md",
        "volgende": "/superpowers:writing-plans /w/spec.md",
        "wanneer": "nu",
    }
    base.update(velden)
    return parse_kop(
        "🤖 Handoff\n"
        + "\n".join(f"{k}: {v}" for k, v in base.items() if v is not None)
    )


def _handoff_tekst() -> str:
    return "\n".join(
        f"{k}: {v}"
        for k, v in {
            "issue": "#12",
            "worktree": "/w/",
            "branch": "issue-12-x",
            "anker": "/w/spec.md",
            "volgende": "/superpowers:writing-plans /w/spec.md",
            "wanneer": "nu",
        }.items()
    )


class TestHandoff:
    def test_een_complete_handoff(self):
        h = parse_handoff(_handoff())
        assert (h.issue, h.worktree, h.branch, h.wanneer) == (
            12,
            "/w",
            "issue-12-x",
            "nu",
        )

    @pytest.mark.parametrize(
        "velden",
        [
            {"volgende": None},
            {"wanneer": "morgen"},
            {"issue": "twaalf"},
            {"worktree": None},
        ],
    )
    def test_onvolledig_of_onleesbaar(self, velden):
        with pytest.raises(HandoffFout):
            parse_handoff(_handoff(**velden))

    def test_handoff_met_lane_native(self):
        native = "/superpowers:executing-plans /abs/plan.md"
        assert parse_handoff(_handoff(volgende=native, lane="native")).lane == "native"
        assert parse_handoff(_handoff(volgende=native)).lane == ""

    @pytest.mark.parametrize(
        ("volgende", "lane"),
        [
            ("/superpowers:subagent-driven-development /abs/plan.md", "native"),
            ("/superpowers:executing-plans /abs/plan.md", "snel"),
        ],
    )
    def test_lane_alleen_native_bij_executing_plans(self, volgende, lane):
        with pytest.raises(HandoffFout, match="lane"):
            parse_handoff(_handoff(volgende=volgende, lane=lane))

    def test_handoff_met_auto_regels_parset_ongewijzigd(self):
        auto = (
            "Context: gekozen zonder vraag\n"
            "Auto: technische-keuze — sqlite of json? → sqlite\n"
            "Auto: intentie-scope — ook de CLI? → ja"
        )
        zonder = parse_kop(
            "🤖 Handoff\n" + _handoff_tekst() + "\nContext: gekozen zonder vraag"
        )
        met = parse_kop("🤖 Handoff\n" + _handoff_tekst() + "\n" + auto)
        assert parse_handoff(met) == parse_handoff(zonder)

    def test_geen_handoff(self):
        with pytest.raises(HandoffFout):
            parse_handoff(parse_kop("🤖 Klaar\npr: x"))


class TestBeoordeelCommando:
    @pytest.mark.parametrize(
        "volgende",
        [
            "/paia-start-issue 12",
            "/paia-fix-pr 34",
            "/superpowers:writing-plans /abs/docs/spec.md",
            "/paia-spec-adversarial-review /abs/docs/spec.md",
            "/superpowers:subagent-driven-development /abs/docs/plan.md",
            "/paia-plan-adversarial-review /abs/docs/plan.md",
        ],
    )
    def test_op_de_allowlist(self, volgende):
        assert beoordeel_commando(volgende, 12) == ("start", "")

    @pytest.mark.parametrize(
        "volgende",
        [
            "/paia-start-issue 13",
            "/paia-fix-pr abc",
            "/paia-fix-pr ٣",
            "/superpowers:writing-plans spec.md",
            "/superpowers:writing-plans /a.md; rm -rf /",
            "/superpowers:writing-plans /a.md /b.md",
            "/superpowers:writing-plans /mijn spec.md",
            "/paia-merge 3",
            "",
        ],
    )
    def test_afgewezen(self, volgende):
        oordeel, reden = beoordeel_commando(volgende, 12)
        assert oordeel == "afgewezen"
        assert reden

    def test_close_issue_is_een_eindhandoff(self):
        assert beoordeel_commando("/paia-close-issue 12", 12) == ("eind", "")

    def test_brainstorm(self):
        assert beoordeel_commando("/superpowers:brainstorming", 12) == (
            "brainstorm",
            "",
        )

    def test_executing_plans_is_interactief(self):
        assert beoordeel_commando("/superpowers:executing-plans /a/plan.md", 12) == (
            "interactief",
            "",
        )

    def test_native_start(self):
        assert beoordeel_commando(
            "/superpowers:executing-plans /abs/plan.md", 1461, lane="native"
        ) == ("start", "")

    def test_native_eist_een_absoluut_planpad(self):
        oordeel, reden = beoordeel_commando(
            "/superpowers:executing-plans plan.md", 1461, lane="native"
        )
        assert oordeel == "afgewezen"
        assert "argument past niet" in reden


class TestAntwoord:
    @pytest.mark.parametrize(
        "body",
        [
            "a",
            "B",
            "antwoord: neem b",
            "Versleuteld",
            "ik kies a",
            "denk na",
            "z",
            "ab",
        ],
    )
    def test_wel(self, body):
        assert is_antwoord(body)

    @pytest.mark.parametrize(
        "body",
        [
            "",
            "   \n  ",
            "🤖 Hervat\nsessie: x",
            "🤖 Onbekend\nx: y",
            '<!-- paia: {"soort":"plan"} -->\n<sub>x</sub>\n\ninhoud',
            "  <!-- paia: {kapot\nx",
        ],
    )
    def test_niet(self, body):
        assert not is_antwoord(body)

    def test_antwoord_prompt_op_een_bundel_houdt_alle_vragen(self):  # Review Focus 4
        body = _vraag_body(
            _velden(
                vragen=[
                    {
                        "vraag": "eerst?",
                        "opties": [{"tekst": "x", "aanbevolen": False}],
                    },
                    {"vraag": "dan?", "opties": [{"tekst": "p", "aanbevolen": False}]},
                ]
            )
        )
        prompt = antwoord_prompt(body, "1a, en bij 2 liever X")
        assert "**Vraag 1:** eerst?" in prompt and "**Vraag 2:** dan?" in prompt
        assert prompt.rstrip().endswith("1a, en bij 2 liever X")
        assert "<sub>" not in prompt and "**Max 1 comment als reactie.**" not in prompt
        assert "Antwoord per vraag" not in prompt


class TestRender:
    def test_gestart_leest_terug(self):
        kop = parse_kop(
            render_gestart(
                sessie=UUID,
                worktree="/w",
                commando="/paia-fix-pr 7",
                start="t",
                head_sha="b" * 40,
            )
        )
        assert kop.soort == "Gestart"
        assert kop.sessie == UUID
        assert kop.velden["commando"] == "/paia-fix-pr 7"
        assert kop.velden["head"] == "b" * 40

    def test_render_gestart_met_stapel(self):
        body = render_gestart(
            sessie=UUID,
            worktree="/w",
            commando="/paia-start-issue 12",
            start="t",
            stapel="#11 issue-11-x",
            basis="c" * 40,
            branch="issue-12-y",
        )
        kop = parse_kop(body)
        assert kop.velden["stapel"] == "#11 issue-11-x"
        assert kop.velden["basis"] == "c" * 40
        assert kop.velden["branch"] == "issue-12-y"
        assert actieve_stapel([body]) == kop

    def test_render_gestart_zonder_stapel_heeft_geen_stapelvelden(self):
        kop = parse_kop(
            render_gestart(sessie=UUID, worktree="/w", commando="c", start="t")
        )
        assert not {"stapel", "basis", "branch", "blokkade"} & kop.velden.keys()

    def test_render_gestart_met_blokkade(self):
        kop = parse_kop(
            render_gestart(
                sessie=UUID,
                worktree="/w",
                commando="c",
                start="t",
                blokkade="verrekend",
            )
        )
        assert kop.velden["blokkade"] == "verrekend"
        assert "basis" not in kop.velden

    def test_render_gestart_met_runner_workspace_en_terugval(self):
        kop = parse_kop(
            render_gestart(
                sessie=UUID,
                worktree="/w",
                commando="c",
                start="t",
                runner="forge",
                workspace="ws1",
                terugval="x",
            )
        )
        assert (
            kop.velden["runner"],
            kop.velden["workspace"],
            kop.velden["terugval"],
        ) == ("forge", "ws1", "x")

    def test_render_gestart_zonder_runner_heeft_geen_forgevelden(self):
        kop = parse_kop(
            render_gestart(sessie=UUID, worktree="/w", commando="c", start="t")
        )
        assert not {"runner", "workspace", "terugval"} & kop.velden.keys()

    def test_vul_gestart_zet_runner_workspace_en_terugval_en_houdt_de_voet(self):
        body = render_gestart(
            sessie=UUID,
            worktree="",
            commando="/paia-start-issue 12",
            start="t",
            runner="forge",
            workspace="ws1",
        )
        nieuw = vul_gestart(
            body,
            worktree="/home/u/.kenn/forge/worktrees/x",
            runner="headless",
            workspace="",
            terugval="forge niet beschikbaar:\nkeyring dicht",
        )
        kop = parse_kop(nieuw)
        assert kop.soort == "Gestart"
        assert kop.velden["worktree"] == "/home/u/.kenn/forge/worktrees/x"
        assert kop.velden["runner"] == "headless"
        assert "workspace" not in kop.velden
        # Een veld is één regel: een newline in de waarde breekt de kop niet open.
        assert kop.velden["terugval"] == "forge niet beschikbaar: keyring dicht"
        assert kop.velden["commando"] == "/paia-start-issue 12"
        assert kop.sessie == UUID
        assert nieuw.endswith("\n\n" + voet(UUID))
        # Een bestaand veld blijft op zijn plek.
        regels = nieuw.splitlines()
        assert regels[1:3] == [
            f"sessie: {UUID}",
            "worktree: /home/u/.kenn/forge/worktrees/x",
        ]

    def test_vul_gestart_zet_een_nieuw_veld_achteraan_de_kop(self):
        body = render_gestart(sessie=UUID, worktree="/w", commando="c", start="t")
        nieuw = vul_gestart(body, runner="forge", workspace="ws1")
        kop = parse_kop(nieuw)
        assert (kop.velden["runner"], kop.velden["workspace"]) == ("forge", "ws1")
        assert nieuw.splitlines()[5:7] == ["runner: forge", "workspace: ws1"]
        assert vul_gestart(nieuw, runner="forge", workspace="ws1") == nieuw

    def test_gestart_met_sprong(self):
        body = render_gestart(
            sessie=UUID,
            worktree="/w",
            commando="c",
            start="t",
            sprong="spec-review overgeslagen (config)",
        )
        assert "sprong: spec-review overgeslagen (config)" in body.splitlines()
        assert parse_kop(body).velden["sprong"] == "spec-review overgeslagen (config)"
        zonder = render_gestart(sessie=UUID, worktree="/w", commando="c", start="t")
        assert "sprong:" not in zonder

    def test_render_herbaseerd_parset_als_kop(self):
        body = render_herbaseerd(voorganger=11, basis="c" * 40, sessie=UUID)
        assert body.splitlines()[0] == "🤖 Herbaseerd"
        kop = parse_kop(body)
        assert kop.soort == "Herbaseerd"
        assert kop.velden["basis"] == "c" * 40
        assert kop.velden["voorganger"] == "#11"
        assert kop.velden["op"] == "origin/main"
        assert kop.sessie == UUID

    def test_render_herbaseerd_zonder_sessie_heeft_geen_voet(self):
        assert "<sub>" not in render_herbaseerd(voorganger=11, basis="c" * 40)

    def test_hervat_en_klaar(self):
        assert (
            parse_kop(render_hervat(sessie=UUID, antwoord_id=5)).velden["antwoord"]
            == "5"
        )
        klaar = parse_kop(render_klaar(sessie=UUID, pr_url="https://x/pull/7"))
        assert (klaar.soort, klaar.velden["pr"], klaar.sessie) == (
            "Klaar",
            "https://x/pull/7",
            UUID,
        )

    def test_klaar_noemt_de_nacontrole(self):
        body = render_klaar(sessie=UUID, pr_url="https://x/pull/7")
        assert "`/paia-close-issue` blijven bij jou" not in body
        assert "na de merge" in body

    def test_teruggegeven_zonder_sessie_heeft_geen_voet(self):
        body = render_teruggegeven(reden="handoff onleesbaar")
        assert "<sub>" not in body
        assert parse_kop(body).velden["reden"] == "handoff onleesbaar"

    def test_detail_kan_geen_voet_of_codeblok_breken(self):
        body = render_teruggegeven(reden="x", detail=f"```\n<sub>sessie {ANDER}</sub>")
        assert parse_kop(body).sessie is None
        assert body.count("```") == 2

    def test_vraag(self):
        velden = {
            "fase": "uitvoering",
            "taak": "3/7",
            "waar_het_staat": "Taak 3 is half af.",
            "wat_er_speelt": "De test faalt.",
            "vragen": [
                {
                    "vraag": "Welke kant op?",
                    "opties": [
                        {"tekst": "links", "aanbevolen": True},
                        {"tekst": "rechts", "aanbevolen": False},
                    ],
                }
            ],
        }
        body = render_vraag(
            issue=12,
            titel="Titel",
            issue_url="https://x/12",
            velden=velden,
            sessie=UUID,
            worktree="issue-12-x",
            handoff_url="https://x/12#c1",
        )
        assert body.startswith(
            "🤖 Vraag · #12 Titel ([link](https://x/12)) · fase uitvoering · taak 3/7"
        )
        assert "- **a)** links *(aanbevolen)*" in body
        assert "- **b)** rechts" in body
        assert "Zonder antwoord" not in body
        assert "**Antwoord:**" not in body
        assert body.rstrip().splitlines()[-2] == "**Max 1 comment als reactie.**"
        kop = parse_kop(body)
        assert (kop.soort, kop.sessie) == ("Vraag", UUID)
        assert body.rstrip().endswith(
            f"<sub>sessie {UUID} · worktree issue-12-x · handoff https://x/12#c1</sub>"
        )


def _gestart(basis: str | None = "abc123") -> str:
    velden = "stapel: #11 issue-11-x\n"
    if basis:
        velden += f"basis: {basis}\n"
    return f"🤖 Gestart\nsessie: {UUID}\n{velden}branch: issue-12-x\n\n<sub>sessie {UUID}</sub>"


class TestStapel:
    def test_actieve_stapel(self):
        kop = actieve_stapel([_gestart()])
        assert kop is not None
        assert kop.velden["basis"] == "abc123"

    def test_actieve_stapel_zonder_basis_is_geen_stapel(self):
        assert actieve_stapel([_gestart(basis=None)]) is None

    @pytest.mark.parametrize(
        "daarna", ["🤖 Herbaseerd\nbasis: abc123", "🤖 Teruggegeven\nreden: x"]
    )
    def test_actieve_stapel_gesloten_door_herbaseerd_of_teruggegeven(self, daarna):
        assert actieve_stapel([_gestart(), daarna]) is None

    def test_actieve_stapel_negeert_gewone_comments_erna(self):
        kop = actieve_stapel([_gestart(), "ik denk a"])
        assert kop is not None

    def test_actieve_stapel_leeg(self):
        assert actieve_stapel([]) is None

    @pytest.mark.parametrize(
        "daarna", ["🤖 Herbaseerd\nbasis: abc123", "🤖 Teruggegeven\nreden: x"]
    )
    def test_stapel_basis_ongeacht_wat_erna_komt(self, daarna):
        assert stapel_basis([_gestart(), daarna]) == "abc123"

    def test_stapel_basis_zonder_gestart_met_basis(self):
        assert stapel_basis([_gestart(basis=None), "ik denk a"]) is None

    def test_stapel_basis_laatste_gestart_met_basis_wint(self):
        assert (
            stapel_basis([_gestart("aaa"), _gestart(basis=None), _gestart("bbb")])
            == "bbb"
        )

    def test_herbaseerd_is_een_soort(self):
        assert parse_kop("🤖 Herbaseerd\nbasis: abc").soort == "Herbaseerd"


def _geschoond(velden: dict) -> dict:
    """Wat parse_vraag teruggeeft: de invoer zoals render_vraag hem schoont."""
    return {
        "fase": _schoon(str(velden.get("fase") or "?")),
        "taak": _schoon(str(velden.get("taak") or "")),
        "waar_het_staat": _schoon(velden["waar_het_staat"]),
        "wat_er_speelt": _schoon(velden["wat_er_speelt"]),
        "vragen": [
            {
                "vraag": _schoon(v["vraag"]),
                "opties": [
                    {
                        "tekst": _schoon(o["tekst"]),
                        "aanbevolen": bool(o.get("aanbevolen")),
                    }
                    for o in v["opties"][:6]
                ],
            }
            for v in velden["vragen"][:4]
        ],
        "commandos": list(velden.get("commandos") or []),
    }


def _vraag_body(velden: dict) -> str:
    return render_vraag(
        issue=12,
        titel="Titel",
        issue_url="https://x/12",
        velden=velden,
        sessie=UUID,
        worktree="issue-12-x",
    )


def _velden(**extra) -> dict:
    """Basisvelden met één vraag; `vraag=`/`opties=` passen die ene vraag aan,
    `vragen=` vervangt de hele bundel."""
    vraag = {
        "vraag": extra.pop("vraag", "welke?"),
        "opties": extra.pop("opties", [{"tekst": "links", "aanbevolen": False}]),
    }
    basis = {
        "waar_het_staat": "in de plan-fase",
        "wat_er_speelt": "twee wegen",
        "vragen": [vraag],
    }
    return basis | extra


OUDE_VRAAG = (
    f"🤖 Vraag · #12 Titel ([link](https://x/12)) · fase plan\n\n"
    "**Waar het staat:** in de plan-fase\n**Wat er speelt:** twee wegen\n"
    "**Vraag:** welke?\n- **a)** links *(aanbevolen)*\n- **b)** rechts\n\n"
    f"**Max 1 comment als reactie.**\n<sub>sessie {UUID} · worktree issue-12-x</sub>"
)


def test_vragen_van_leest_oude_classificatie():
    oud = {"vraag": "z?", "opties": [{"tekst": "een", "aanbevolen": True}]}
    assert vragen_van(oud) == [
        {"vraag": "z?", "opties": [{"tekst": "een", "aanbevolen": True}]}
    ]


class TestParseVraag:
    def test_oude_vraag_parset_als_bundel_van_een(self):  # Review Focus 1
        assert parse_vraag(OUDE_VRAAG)["vragen"] == [
            {
                "vraag": "welke?",
                "opties": [
                    {"tekst": "links", "aanbevolen": True},
                    {"tekst": "rechts", "aanbevolen": False},
                ],
            }
        ]

    def test_een_vraag_rendert_als_voorheen(self):
        body = _vraag_body(
            _velden(
                fase="plan",
                vragen=[
                    {
                        "vraag": "welke?",
                        "opties": [
                            {"tekst": "links", "aanbevolen": True},
                            {"tekst": "rechts", "aanbevolen": False},
                        ],
                    }
                ],
            )
        )
        assert body == OUDE_VRAAG

    def test_bundel_nummert_vragen_en_opties(self):
        body = _vraag_body(
            _velden(
                vragen=[
                    {
                        "vraag": "eerst?",
                        "opties": [
                            {"tekst": "x", "aanbevolen": True},
                            {"tekst": "y", "aanbevolen": False},
                        ],
                    },
                    {
                        "vraag": "dan?",
                        "opties": [
                            {"tekst": "p", "aanbevolen": False},
                            {"tekst": "q", "aanbevolen": True},
                        ],
                    },
                ]
            )
        )
        assert "**Vraag 1:** eerst?" in body and "**Vraag 2:** dan?" in body
        # Zonder lege regel hangt Vraag 2 als lazy continuation in de laatste optie.
        assert "\n\n**Vraag 2:**" in body
        assert "\n\n**Vraag 1:**" not in body
        assert (
            "- **1a)** x *(aanbevolen)*" in body
            and "- **2b)** q *(aanbevolen)*" in body
        )
        assert "Antwoord per vraag, bijvoorbeeld `1a, 2c`." in body
        assert "**Vraag:**" not in body

    def test_roundtrip_maximale_bundel(self):  # Review Focus 5
        vragen = [
            {
                "vraag": f"vraag {n}\n\nmet lege regel",
                "opties": [
                    {
                        "tekst": f"optie {n}{letter}\ntweede regel",
                        "aanbevolen": letter == "c",
                    }
                    for letter in "abcdef"
                ],
            }
            for n in range(1, 5)
        ]
        self._roundtrip(_velden(fase="uitvoering", taak="2/5", vragen=vragen))

    def _roundtrip(self, velden: dict) -> None:
        assert parse_vraag(_vraag_body(velden)) == _geschoond(velden)

    def test_roundtrip_een_optie(self):
        self._roundtrip(_velden(fase="plan"))

    def test_roundtrip_zes_opties_met_aanbevolen(self):
        opties = [
            {"tekst": f"optie {letter}", "aanbevolen": letter == "b"}
            for letter in "abcdef"
        ]
        self._roundtrip(_velden(fase="uitvoering", taak="3/7", opties=opties))

    def test_roundtrip_zonder_fase(self):
        velden = _velden()
        assert parse_vraag(_vraag_body(velden))["fase"] == "?"
        self._roundtrip(velden)

    def test_vrije_tekst_met_sterretjes(self):
        self._roundtrip(
            _velden(
                vraag="kies **a** of **b**?",
                opties=[{"tekst": "**vet** bedoeld", "aanbevolen": False}],
            )
        )

    def test_codeblok_wordt_geschoond(self):
        velden = _velden(wat_er_speelt="zie\n```\nx\n```")
        resultaat = parse_vraag(_vraag_body(velden))
        assert resultaat["wat_er_speelt"] == "zie\n'''\nx\n'''"
        assert resultaat == _geschoond(velden)

    def test_meerregelige_optie(self):
        self._roundtrip(
            _velden(
                opties=[
                    {"tekst": "eerste regel\ntweede regel", "aanbevolen": True},
                    {"tekst": "andere", "aanbevolen": False},
                ]
            )
        )

    def test_lege_regel_in_vrije_tekst(self):
        """Een lege regel in de vrije tekst breekt het blok niet af (Review Focus 3)."""
        velden = _velden(wat_er_speelt="a\n\nb")
        resultaat = parse_vraag(_vraag_body(velden))
        assert resultaat["wat_er_speelt"] == "a\n\nb"
        assert resultaat == _geschoond(velden)

    def test_lege_regel_in_laatste_optie(self):
        self._roundtrip(_velden(opties=[{"tekst": "a\n\nb", "aanbevolen": True}]))

    def test_geen_vraag_geeft_none(self):
        assert parse_vraag("ik denk a") is None
        assert parse_vraag(render_klaar(sessie=UUID, pr_url="u")) is None


RPM = "sudo dnf install -y /tmp/orca-ide-1.0.rpm"
FIREWALL = "sudo bash deploy/beelink/firewall.sh &&\n  sudo firewall-cmd --list-rich-rules | grep 6768"


class TestVraagvorm:
    """#1351: de vraag rendert als markdown, en elk zelf-te-draaien commando als codeblok."""

    def test_zonder_commandos_geen_codeblok_en_geldige_vorm(self):
        body = _vraag_body(_velden())
        assert "```" not in body
        assert "**Zelf draaien:**" not in body
        assert markdown_vormfouten(body) == []

    def test_elk_commando_staat_letterlijk_als_eigen_codeblok(self):
        body = _vraag_body(_velden(commandos=[RPM, FIREWALL]))
        assert f"**Zelf draaien:**\n\n```bash\n{RPM}\n```" in body
        assert markdown_vormfouten(body, [RPM, FIREWALL]) == []

    def test_commandos_staan_tussen_vraag_en_opties(self):
        body = _vraag_body(_velden(commandos=[RPM]))
        assert body.index("**Vraag:**") < body.index(RPM) < body.index("- **a)**")

    def test_een_commando_met_backticks_krijgt_een_langere_fence(self):
        cmd = "echo '```' && printf '%s' \"$(date)\""
        body = _vraag_body(_velden(commandos=[cmd]))
        assert f"````bash\n{cmd}\n````" in body
        assert markdown_vormfouten(body, [cmd]) == []

    def test_vrije_tekst_kan_geen_tilde_codeblok_openen(self):
        body = _vraag_body(_velden(wat_er_speelt="zie\n~~~\nx"))
        assert "~~~" not in body
        assert markdown_vormfouten(body) == []

    def test_roundtrip_met_commandos(self):
        self_ = TestParseVraag()
        self_._roundtrip(_velden(commandos=[RPM, FIREWALL]))

    def test_een_commando_dat_op_een_optie_lijkt_blijft_een_commando(self):
        cmd = "- **a)** geen optie"
        resultaat = parse_vraag(_vraag_body(_velden(commandos=[cmd])))
        assert resultaat["commandos"] == [cmd]
        assert [o["tekst"] for o in resultaat["vragen"][0]["opties"]] == ["links"]


POST_1351_VRAAG = (
    f"🤖 Vraag · #12 Titel ([link](https://x/12)) · fase plan\n\n"
    "**Waar het staat:** in de plan-fase\n**Wat er speelt:** twee wegen\n"
    f"**Vraag:** welke?\n\n**Zelf draaien:**\n\n```bash\n{RPM}\n```\n\n"
    "- **a)** links *(aanbevolen)*\n- **b)** rechts\n\n"
    f"**Max 1 comment als reactie.**\n<sub>sessie {UUID} · worktree issue-12-x</sub>"
)


def _twee_vragen() -> list[dict]:
    return [
        {
            "vraag": "eerst?",
            "opties": [
                {"tekst": "x", "aanbevolen": True},
                {"tekst": "y", "aanbevolen": False},
            ],
        },
        {
            "vraag": "dan?",
            "opties": [
                {"tekst": "p", "aanbevolen": False},
                {"tekst": "q", "aanbevolen": False},
            ],
        },
    ]


class TestBundelMetCommandos:
    """R11 (#1361 x #1351): één `commandos` per Vraag, gedeeld door de bundel."""

    def test_een_vraag_met_commando_rendert_als_na_1351(self):
        velden = _velden(
            fase="plan",
            vragen=[
                {
                    "vraag": "welke?",
                    "opties": [
                        {"tekst": "links", "aanbevolen": True},
                        {"tekst": "rechts", "aanbevolen": False},
                    ],
                }
            ],
            commandos=[RPM],
        )
        assert _vraag_body(velden) == POST_1351_VRAAG

    def test_vraag_van_na_1351_parset_als_bundel_van_een(self):
        uit = parse_vraag(POST_1351_VRAAG)
        assert uit["commandos"] == [RPM]
        assert uit["vragen"] == [
            {
                "vraag": "welke?",
                "opties": [
                    {"tekst": "links", "aanbevolen": True},
                    {"tekst": "rechts", "aanbevolen": False},
                ],
            }
        ]

    def test_bundel_zet_het_blok_voor_vraag_1(self):
        body = _vraag_body(_velden(vragen=_twee_vragen(), commandos=[RPM, FIREWALL]))
        assert (
            body.index("**Wat er speelt:**")
            < body.index("**Zelf draaien:**")
            < body.index(RPM)
            < body.index("**Vraag 1:**")
            < body.index("**Vraag 2:**")
        )
        assert body.count("**Zelf draaien:**") == 1
        slot = body.index("**Max 1 comment als reactie.**")
        assert body.index("Antwoord per vraag, bijvoorbeeld `1a, 2c`.") > slot

    def test_bundel_met_commandos_roundtript(self):
        TestParseVraag()._roundtrip(
            _velden(fase="plan", vragen=_twee_vragen(), commandos=[RPM, FIREWALL])
        )

    def test_bundel_met_commandos_heeft_geldige_markdownvorm(self):
        body = _vraag_body(_velden(vragen=_twee_vragen(), commandos=[RPM, FIREWALL]))
        assert markdown_vormfouten(body, [RPM, FIREWALL]) == []
