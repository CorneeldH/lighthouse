PYTHON_SKILL = REPO / ".claude" / "skills" / "paia-python-schrijven" / "SKILL.md"


def test_python_skill_frontmatter_carries_binding_and_origin():
    """binding/origin — de twee velden waar een beslissing aan hangt.

    `upstream` hoort er hier NIET bij: `origin: own` heeft geen bron om naar te
    wijzen, en sinds #686 hoort een sleutel zonder waarde onder `metadata:` weg
    te blijven in plaats van leeg te staan.
    """
    front = _frontmatter(PYTHON_SKILL)
    assert "name: paia-python-schrijven" in front
    assert "binding: hard" in front, "er zijn twee hook-tegenhangers, dus hard geldt"
    assert "origin: own" in front
    assert "upstream:" not in front, "origin: own draagt geen upstream-sleutel"


def test_python_skill_description_has_a_negative_boundary():
    """Descriptions staan permanent in context en het model kiest op gelijkenis."""
    front = _frontmatter(PYTHON_SKILL)
    assert "LET OP" in front
    assert "paia-shell-gates" in front, "de grens naar shell ontbreekt"


def test_python_skill_kpi_rows_all_carry_a_command():
    """Geen commando → geen KPI → schrappen. Elke tabelrij draagt een commando."""
    lines = PYTHON_SKILL.read_text(encoding="utf-8").splitlines()
    start = next(i for i, line in enumerate(lines) if line.startswith("| Norm |"))
    rows = []
    for line in lines[start + 2 :]:
        if not line.startswith("|"):
            break
        rows.append(line)
    assert len(rows) >= 4, f"KPI-tabel heeft maar {len(rows)} rijen"
    for row in rows:
        assert "`" in row.split("|")[2], f"KPI-rij zonder meetcommando: {row}"


def test_instructies_no_longer_carry_the_moved_python_prose():
    """De verhuizing is pas af als de bron leeg is — anders staat de norm twee keer."""
    text = _geladen()
    moved = [
        "100.76.126.126, een lokaal interface hier",
        "NEVER run full suite manually",
        "Het oordeel hangt af van wélke extras er gesynct zijn",
    ]
    for fragment in moved:
        assert fragment not in text, f"nog niet verhuisd: {fragment!r}"


def test_agents_md_points_at_the_new_artefacts():
    """Een pointer moet blijven staan, anders laadt de skill nooit."""
    text = AGENTS_MD.read_text(encoding="utf-8")
    assert "paia-python-schrijven" in text
    assert ".claude/rules/python.md" in text


def test_python_skill_pins_match_their_source_of_truth():
    """Een KPI met een verlopen pin meet iets anders dan de gate."""
    text = PYTHON_SKILL.read_text(encoding="utf-8")
    ruff_pin = (REPO / ".ruff-version").read_text(encoding="utf-8").strip()
    assert f"ruff@{ruff_pin}" in text
    assert "ty@0.0.72" in text


# --- paia-wait-what (#745) --------------------------------------------------
# De norm *Uitleg is de uitzondering* verhuisde van root CLAUDE.md naar een
# /-only skill. Getoetst wordt precies wat een latere hand kan terugdraaien
