"""Tests voor src/controller/artefacten.py: metadata van artefact-comments."""

import dataclasses
import json
from pathlib import Path

import pytest

from src.controller.artefacten import (
    LIMIET_GITHUB,
    Meta,
    hash_van,
    huidige,
    inhoud,
    parse_meta,
    render,
    splits,
    volgende_versie,
)
from src.controller.comments import Comment

REPO_ROOT = Path(__file__).resolve().parents[2]
ROMP = REPO_ROOT / "docs/developer/planning/plans/2026-09-25-controller-romp.md"
SHA = "a" * 40
META = Meta(
    soort="plan",
    versie=2,
    deel=1,
    delen=4,
    bestand="docs/developer/planning/plans/2026-10-03-x.md",
    commit=SHA,
    hash="0123456789ab",
    vervangt="https://github.com/CorneeldH/personal_ai_agent/issues/1094#issuecomment-123",
)


def test_rondreis():
    body = render(META, "# Titel\n\ntekst\n")
    assert parse_meta(body) == META
    assert inhoud(body) == "# Titel\n\ntekst\n"


def test_eerste_regel_en_sub_regel():
    regels = render(META, "x").splitlines()
    assert regels[0].startswith("<!-- paia: {") and regels[0].endswith("} -->")
    assert regels[1] == (
        '<sub>plan · versie 2 · deel 1/4 · bron <a href="https://github.com/'
        f"CorneeldH/personal_ai_agent/blob/{SHA}/docs/developer/planning/plans/"
        '2026-10-03-x.md">docs/developer/planning/plans/2026-10-03-x.md</a> @ aaaaaaa</sub>'
    )
    assert regels[2] == ""


def test_onbekende_velden_overleven():
    meta = dataclasses.replace(META, extra={"toekomst": [1, 2]})
    assert parse_meta(render(meta, "x")) == meta


def test_vervangt_null():
    meta = dataclasses.replace(META, vervangt=None)
    parsed = parse_meta(render(meta, "x"))
    assert parsed is not None
    assert parsed.vervangt is None


@pytest.mark.parametrize(
    "veld,waarde",
    [
        ("soort", "anders"),
        ("versie", 0),
        ("deel", True),
        ("deel", 5),
        ("commit", "x"),
        ("hash", "0123456789a"),
        ("hash", "0123456789AB"),
    ],
)
def test_ongeldige_waarde_geeft_none(veld, waarde):
    data = json.loads(
        render(META, "x").splitlines()[0][len("<!-- paia: ") : -len(" -->")]
    )
    data[veld] = waarde
    assert parse_meta(f"<!-- paia: {json.dumps(data)} -->\n<sub>x</sub>\n\ny") is None


@pytest.mark.parametrize(
    "body",
    [
        "<!-- paia: {kapot -->\nx",
        '<!-- paia: {"soort":"plan"} -->\nx',  # verplichte velden ontbreken
        "gewone comment",
        "",
    ],
)
def test_onleesbaar_geeft_none(body):
    assert parse_meta(body) is None


def test_render_weigert_dubbel_streepje():
    with pytest.raises(ValueError):
        render(
            dataclasses.replace(META, bestand="docs/developer/planning/plans/a--b.md"),
            "x",
        )


def test_hash_van():
    assert hash_van(b"abc") == "ba7816bf8f01"


def test_past_in_een_deel():
    assert splits("kort\n") == ["kort\n"]


def test_leeg_bestand_is_een_leeg_deel():
    assert splits("") == [""]


def test_knipt_voor_een_h2():
    tekst = "## a\n" + "b\n" * 30 + "## c\n" + "d\n" * 30
    delen = splits(tekst, limiet=70)
    assert delen[0] == "## a\n" + "b\n" * 30
    assert all(delen) and "".join(delen) == tekst


def test_geen_knip_op_offset_nul():
    tekst = "## a\n" + "b" * 10 + "\n" + "c" * 10 + "\n"
    delen = splits(tekst, limiet=20)
    assert all(delen) and "".join(delen) == tekst
    assert delen[0].startswith("## a\n")


def test_niet_in_een_codeblok_als_er_een_ander_knippunt_is():
    blok = "````md\n```\n" + "## binnen\n\n" * 5 + "```\n````\n"
    tekst = "intro\n\n" + blok + "\n## na\n"
    delen = splits(tekst, limiet=len("intro\n\n") + len(blok) - 5)
    assert delen[0] == "intro\n\n"
    assert "".join(delen) == tekst


def test_tilde_fence_en_ingesprongen_fence():
    blok = "   ~~~\n## binnen\n~~~\n"
    tekst = "a\n\n" + blok + "z\n"
    assert splits(tekst, limiet=len("a\n\n") + len(blok) - 2)[0] == "a\n\n"


def test_regel_langer_dan_de_limiet_knipt_midden_in_de_regel():
    delen = splits("x" * 25, limiet=10)
    assert delen == ["x" * 10, "x" * 10, "x" * 5]


@pytest.mark.parametrize(
    "tekst", ["a\r\nb\r\n\r\n## c\r\n" * 20, "a\n\n## b\nzonder slot"]
)
def test_rondreis_crlf_en_zonder_slot_newline(tekst):
    delen = splits(tekst, limiet=15)
    assert all(delen) and "".join(delen) == tekst


def test_het_echte_romp_plan():
    tekst = ROMP.read_text(encoding="utf-8")
    delen = splits(tekst)
    assert len(delen) >= 4
    assert "".join(delen) == tekst
    assert all(delen)
    meta = Meta(
        soort="plan",
        versie=99,
        deel=99,
        delen=99,
        bestand=str(ROMP.relative_to(REPO_ROOT)),
        commit="a" * 40,
        hash="0" * 12,
        vervangt="https://github.com/CorneeldH/personal_ai_agent/issues/1#issuecomment-1234567890",
    )
    assert all(len(render(meta, d)) < LIMIET_GITHUB for d in delen)


SPEC_A = "docs/developer/planning/specs/a.md"
SPEC_B = "docs/developer/planning/specs/b.md"


def deel(cid, versie, d, n, bestand=SPEC_A, soort="spec"):
    meta = Meta(
        soort=soort,
        versie=versie,
        deel=d,
        delen=n,
        bestand=bestand,
        commit=SHA,
        hash="0" * 12,
        vervangt=None,
    )
    return Comment(
        id=cid, body=render(meta, f"v{versie}d{d}"), created_at="", url=f"u{cid}"
    )


def test_hoogste_complete_versie_wint_van_een_onvolledige_hogere():
    cs = [deel(1, 1, 1, 2), deel(2, 1, 2, 2), deel(3, 2, 1, 2)]
    assert [c.id for c in huidige(cs, "spec", SPEC_A)] == [1, 2]
    assert volgende_versie(cs, "spec", SPEC_A) == 3


def test_delen_op_volgorde_ook_als_ze_door_elkaar_staan():
    cs = [deel(2, 1, 2, 2), deel(1, 1, 1, 2)]
    assert [c.id for c in huidige(cs, "spec", SPEC_A)] == [1, 2]


def test_dubbel_deel_maakt_een_versie_onvolledig():
    cs = [deel(1, 1, 1, 1), deel(2, 2, 1, 2), deel(3, 2, 1, 2), deel(4, 2, 2, 2)]
    assert [c.id for c in huidige(cs, "spec", SPEC_A)] == [1]


def test_ongelijke_delen_maakt_een_versie_onvolledig():
    cs = [deel(1, 1, 1, 2), deel(2, 1, 2, 3)]
    assert huidige(cs, "spec", SPEC_A) == []


def test_twee_ketens_zien_elkaar_niet():
    cs = [
        deel(1, 1, 1, 1),
        deel(2, 1, 1, 1, bestand=SPEC_B),
        deel(3, 2, 1, 1, bestand=SPEC_B),
    ]
    assert [c.id for c in huidige(cs, "spec", SPEC_A)] == [1]
    assert volgende_versie(cs, "spec", SPEC_A) == 2
    assert volgende_versie(cs, "spec", SPEC_B) == 3


def test_andere_soort_en_gewone_comments_tellen_niet():
    cs = [deel(1, 5, 1, 1, soort="plan"), Comment(id=2, body="hoi", created_at="")]
    assert huidige(cs, "spec", SPEC_A) == []
    assert volgende_versie(cs, "spec", SPEC_A) == 1


def test_render_bronlink_volgt_identity(other_identity):
    body = render(META, "x")
    assert 'bron <a href="https://github.com/Other/repo/blob/' in body
    assert "CorneeldH" not in body.splitlines()[1]
