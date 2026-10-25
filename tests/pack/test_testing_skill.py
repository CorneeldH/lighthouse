from tests.pack._testkit import CORE

SKILL = (CORE / "skills" / "testing" / "SKILL.md").read_text(encoding="utf-8")
FRONT = SKILL.split("---\n", 2)[1]
CLASSES = ("pure-logic", "io/filesystem", "cli-output", "hook/gate", "config", "test-only")

def test_frontmatter():
    assert "name: lighthouse-testing\n" in FRONT
    assert "origin: own" in FRONT and 'source: "#22"' in FRONT
    assert "disable-model-invocation" not in FRONT  # model-invoked, triggert op werk

def test_description_triggers_on_the_work_and_on_planning():
    for word in ("writing", "fixing", "reviewing", "mock", "test strategy"):
        assert word in FRONT, word

def test_description_bounds_tdd_and_the_language_skill():
    assert "superpowers:test-driven-development" in FRONT
    assert "lighthouse-python-writing" in FRONT
    assert "lighthouse-go-writing" in FRONT

def test_core_question_and_mutation_check():
    assert "What production change should make this test fail?" in SKILL
    assert "## Mutation check" in SKILL

def test_contract_tests_are_a_kind_of_their_own():
    assert "## Contract tests and code tests" in SKILL
    assert "lasting invariant" in SKILL

def test_classification_sentence_and_classes():
    assert "This is a <class> change; the likely regression surface is <observable behaviour>." in SKILL
    for c in CLASSES:
        assert f"`{c}`" in SKILL, c

def test_language_neutral():
    for term in ("require.", "Playwright", "PostgreSQL", "pgtest", "Svelte", "Huma", "SSE", "FTS"):
        assert term not in SKILL, term

def test_english():
    text = SKILL.lower()
    for word in (" het ", " niet ", " een ", " wordt ", " dat ", " je "):
        assert word not in text, word

def _section(heading):
    return " ".join(SKILL.split(heading, 1)[1].split("\n## ", 1)[0].split())

def test_inspiration_says_where_the_classification_sentence_comes_from():
    inspiration = _section("## Inspiration")
    assert "classification sentence" in inspiration
    assert "forge" in inspiration.split("classification sentence", 1)[1]

def test_red_flags_catch_hidden_tautologies():
    flags = _section("## Red flags")
    for phrase in ("helper, loop or builder", "equal by construction", "only fails by crashing",
                   "line by line", "only for coverage"):
        assert phrase in flags, phrase

def test_core_question_says_where_the_answer_goes():
    assert "in the chat" in _section("## The core question")

def test_classification_sentence_place_is_said_once():
    assert _section("## Classify first").lower().count("before the first edit") == 1

def test_called_mock_is_fine_when_the_call_is_the_contract():
    assert "unless the call itself is the contract" in _section("## Red flags")

def test_hook_messages_are_asserted_from_a_run():
    assert "from the output of a run" in _section("## Contract tests and code tests")

def test_description_makes_tdd_a_companion():
    assert "alongside superpowers:test-driven-development" in FRONT
