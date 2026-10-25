"""`pack/core/lib/code_language_score.py`, as installed (#37): layer 1, the score per unit."""

import pytest

from tests.pack._testkit import load_hook_lib

score = load_hook_lib("code_language_score")

DUTCH_NAMES = [
    "lees_bestand", "schrijf_uitvoer", "verwerk_transcript", "maak_handoff", "controleer_poort",
    "opschonen", "vergadering_notities", "zoek_issue", "haal_labels_op", "bewaar_resultaat",
    "bereken_dekking", "wachtrij_leegmaken", "controleerTaal", "leesConfiguratie",
    "VerwerkerFout", "BestandNietGevonden", "aantal_regels", "huidige_tak", "standaard_tak",
    "gebruiker_naam", "taak_lijst", "sluit_issue", "stuur_bericht", "nieuwe_versie",
    "oude_kopie", "werkboom_audit", "bord_status", "sleutel_waarde", "invoer_controle",
    "uitzonderingen_lijst", "blokkade_label", "schrijfpoorten", "hookmelding_tekst",
    "padpatronen", "geinstalleerde_bestanden", "afhankelijkheden", "laatste_wijziging",
    "voorganger_pr", "opvolger", "tijdslimiet", "terugval_woordenlijst", "vondsten",
    "kon_niet_controleren", "verzamelbump_container_images", "legacy_processed_lijst_weg",
    "codetaal_hook", "vergaderingen_als_data", "dagelijkse_samenvatting", "notulen_verwerken",
    "spraak_naar_tekst", "agenda_bijwerken",
]  # fmt: skip

ENGLISH_NAMES = [
    "read_file", "write_output", "process_transcript", "make_handoff", "check_gate", "cleanup",
    "meeting_notes", "find_issue", "fetch_labels", "save_result", "compute_coverage",
    "drain_queue", "checkLanguage", "readConfig", "ParserError", "FileNotFound", "line_count",
    "current_branch", "default_branch", "user_name", "task_list", "close_issue", "send_message",
    "new_version", "old_copy", "worktree_audit", "board_status", "key_value", "input_check",
    "exceptions_list", "blocked_label", "write_gates", "hook_message", "path_patterns",
    "installed_files", "dependencies", "last_change", "predecessor_pr", "successor", "timeout",
    "fallback_wordlist", "findings", "could_not_check",
]  # fmt: skip

# Measured against the spec appendix: these are not found with the given constants.
NOT_FOUND = {
    "legacy_processed_lijst_weg": "doubt (measured)",
    "verzamelbump_container_images": "missed (measured)",
    "codetaal_hook": "missed (measured)",
}


@pytest.fixture
def table(monkeypatch):
    def install(entries, jargon=frozenset()):
        monkeypatch.setattr(score, "frequencies", lambda: entries)
        monkeypatch.setattr(score, "jargon", lambda: frozenset(jargon))

    return install


def test_threshold_is_strict(table):
    table({"alpha": (10, 20), "beta": (10, 21)})
    assert score.judge_name("alpha") == "clean"  # sum exactly 1.0
    assert score.judge_name("beta") == "finding"  # sum 1.1


def test_doubt_needs_a_strongly_dutch_word(table):
    table({"legacy": (40, 10), "lijst": (20, 50)})
    assert score.judge_name("legacy_lijst") == "doubt"
    table({"legacy": (40, 10), "lijst": (20, 35)})
    assert score.judge_name("legacy_lijst") == "clean"


def test_short_parts_skipped_in_names_not_in_prose(table):
    table({"val": (10, 60), "typ": (10, 60), "ver": (10, 60), "de": (10, 60), "van": (10, 60), "een": (10, 60)})
    assert score.judge_name("val_typ_ver") == "clean"
    assert score.judge_text("de van een") == "finding"


def test_jargon_counts_as_english(table):
    table({"gids": (10, 40)})
    assert score.judge_name("gids") == "finding"
    table({"gids": (10, 40)}, jargon={"gids"})
    assert score.judge_name("gids") == "clean"


def test_dutch_split(table):
    table({"werk": (30, 50), "boom": (25, 40)})
    assert score.dutch_split("werkboom")
    assert score.word_score("werkboom") == 3.0
    table({"werk": (30, 50), "boom": (45, 40)})
    assert not score.dutch_split("werkboom")
    assert score.word_score("werkboom") == 0.0
    table({})
    assert not score.dutch_split("abcdef")


def test_prose_drops_inline_code_and_urls(table):
    table({"see": (60, 10), "at": (60, 10)})
    assert score.judge_text("see `lees_bestand` at https://x.nl/van") == "clean"


def test_spec_compounds():
    for word in ("schrijfpoorten", "padpatronen", "werkboom"):
        assert score.dutch_split(word), word
    for word in ("maxsize", "worktree", "dataclass"):
        assert not score.dutch_split(word), word


def test_spec_false_alarms_are_clean():
    for name in ("gids", "opname", "getint", "anno", "arg_vals", "SCHEMA"):
        assert score.judge_name(name) == "clean", name


def test_spec_doubt():
    assert score.judge_name("legacy_processed_lijst_weg") == "doubt"


def test_prose():
    assert score.judge_text("Het bestand wordt niet gelezen.") == "finding"
    assert score.judge_text("Read the file and return its lines.") == "clean"


def test_dutch_names_found():
    assert len(DUTCH_NAMES) == 51
    for name in DUTCH_NAMES:
        verdict = score.judge_name(name)
        if name in NOT_FOUND:
            assert verdict != "finding", f"{name} is found now: shrink NOT_FOUND"
        else:
            assert verdict == "finding", f"{name}: {verdict}"


def test_english_names_not_found():
    assert len(ENGLISH_NAMES) == 43
    for name in ENGLISH_NAMES:
        assert score.judge_name(name) != "finding", name


def test_missing_data_is_data_error(monkeypatch):
    score.frequencies.cache_clear()
    monkeypatch.setattr(score, "FREQ_FILE", "nope.tsv.gz")
    try:
        with pytest.raises(score.DataError):
            score.frequencies()
    finally:
        monkeypatch.undo()
        score.frequencies.cache_clear()


def test_corrupt_data_is_data_error(monkeypatch, tmp_path):
    import gzip
    import zlib

    rows = "".join(f"w{i}\t{i % 70}\t{i % 50}\n" for i in range(3000))
    body = bytearray(gzip.compress(rows.encode()))
    body[20] ^= 0xFF  # one corrupted byte early in the deflate stream
    with pytest.raises(zlib.error):  # guards the fixture: it must trigger zlib.error
        gzip.decompress(bytes(body))
    (tmp_path / "bad.tsv.gz").write_bytes(bytes(body))
    monkeypatch.setattr(score, "_data_path", lambda filename: tmp_path / "bad.tsv.gz")
    score.frequencies.cache_clear()
    try:
        with pytest.raises(score.DataError):
            score.frequencies()
    finally:
        monkeypatch.undo()
        score.frequencies.cache_clear()
