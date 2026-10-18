"""`scripts/hooks/check-instructies.sh`: dertien checks over de instructielaag.

Het script draait als subprocess tegen tijdelijke bomen, zodat het contract
(exit code + stdout) getest wordt en niet een herimplementatie ervan. Geen
netwerk: `tests/unit/conftest.py` blokkeert uitgaande sockets en dit script
hoort daar sowieso niet te komen.
"""

import os
import subprocess
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT = REPO_ROOT / "scripts" / "hooks" / "check-instructies.sh"


def _skill(
    root: Path,
    name: str,
    description: str = "Doet iets nuttigs.",
    metadata: dict | None = None,
    body: str | None = None,
    domein: str | None = "factory",
    **fields,
):
    """Schrijf een minimale SKILL.md.

    `fields` gaat ongewijzigd top-level de frontmatter in (spec- en vendor-velden).
    `metadata` nest onder een `metadata:`-blok -- sinds #686 waar origin/upstream/
    binding thuishoren, conform de Agent Skills-spec. `body` is de tekst ná de
    frontmatter (default de oude kale kop); check 8 grept daarin naar een
    hook-pad. `domein` (niet `domain`: dat botst met `**fields`, waarmee een test
    juist een top-level `domain` schrijft) komt als laatste regel onder
    `metadata:` (check 13, #1161); het blok ontstaat ook als `metadata` leeg is.
    `None` laat het veld weg.
    """
    directory = root / name
    directory.mkdir(parents=True, exist_ok=True)
    lines = [f"name: {name}", f"description: {description}"]
    lines += [f"{key.replace('_', '-')}: {value}" for key, value in fields.items()]
    if metadata or domein is not None:
        lines.append("metadata:")
        lines += [
            f"  {key.replace('_', '-')}: {value}"
            for key, value in (metadata or {}).items()
        ]
        if domein is not None:
            lines.append(f"  domain: {domein}")
    if body is None:
        body = "# " + name + "\n"
    content = "---\n" + "\n".join(lines) + "\n---\n\n" + body
    (directory / "SKILL.md").write_text(content, encoding="utf-8")
    return directory


def _tree(tmp_path: Path) -> tuple[Path, Path, Path]:
    """Een lege repo-boom plus een lege personal-map. Alle drie bestaan."""
    claude = tmp_path / "repo" / ".claude" / "skills"
    agents = tmp_path / "repo" / ".agents" / "skills"
    personal = tmp_path / "personal" / "skills"
    for directory in (claude, agents, personal):
        directory.mkdir(parents=True)
    return claude, agents, personal


def _run(tmp_path: Path, personal: Path | None = None, extra_env: dict | None = None):
    env = dict(
        os.environ,
        CHECK_INSTRUCTIES_ROOT=str(tmp_path / "repo"),
        CHECK_INSTRUCTIES_PERSONAL=str(
            personal if personal is not None else tmp_path / "personal" / "skills"
        ),
        # Een map die `_tree` niet aanmaakt: zonder deze regel leest elke test
        # de ~/.claude/agents van de machine waarop hij draait.
        CHECK_INSTRUCTIES_PERSONAL_AGENTS=str(tmp_path / "personal" / "agents"),
    )
    # De ontsnapping van check 12 mag niet uit de omgeving van de aanroeper
    # lekken; een test die haar wil, geeft haar via `extra_env`.
    env.pop("CHECK_INSTRUCTIES_PERSONAL_OK", None)
    if extra_env:
        env.update(extra_env)
    # `timeout`: het script is deterministisch en draait hier standaard in
    # enkele seconden. Een hang hoort dus als test-failure te verschijnen,
    # niet als een oneindig wachtende testrun.
    return subprocess.run(
        [str(SCRIPT)], capture_output=True, text=True, env=env, check=False, timeout=30
    )


def _path_zonder_python3(tmp_path: Path) -> str:
    """Een PATH die alles bevat wat het script nodig heeft, behalve python3.

    Geen `command -v python3`-shadowing met een lege stub: `command -v` zoekt
    door naar de volgende PATH-map zodra een entry niet uitvoerbaar is, dus
    een stub verwijdert python3 niet betrouwbaar. In plaats daarvan een
    complete vervangende bin-map met symlinks naar alles wat wél op de echte
    PATH staat, op elke `python3*`-naam na.
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


# --- identiteit: dubbele naam, byte-identiek paar, spiegel ------------------


def test_lege_boom_is_groen(tmp_path):
    _tree(tmp_path)
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "FOUT" not in result.stdout


def test_dubbele_naam_faalt(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-een", description="Eerste ding.")
    _skill(agents, "paia-een", description="Eerste ding.")
    tweede = _skill(claude, "paia-twee", description="Tweede heel ander ding.")
    _skill(agents, "paia-twee", description="Tweede heel ander ding.")
    tekst = (tweede / "SKILL.md").read_text(encoding="utf-8")
    (tweede / "SKILL.md").write_text(
        tekst.replace("name: paia-twee", "name: paia-een"), encoding="utf-8"
    )
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "dubbele naam" in result.stdout
    assert "paia-een" in result.stdout


def test_byte_identiek_paar_faalt(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    # Zelfde inhoud, twee mapnamen -- het `generate-slides-retro`-geval.
    for naam in ("paia-retro", "paia_retro"):
        for wortel in (claude, agents):
            directory = wortel / naam
            directory.mkdir()
            (directory / "SKILL.md").write_text(
                "---\nname: paia-retro-gedeeld\ndescription: Retro.\n---\n",
                encoding="utf-8",
            )
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "byte-identiek" in result.stdout


def test_spiegelbreuk_faalt(tmp_path):
    claude, _agents, _ = _tree(tmp_path)
    _skill(claude, "paia-alleen-links")
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "spiegel" in result.stdout
    assert "paia-alleen-links" in result.stdout


def test_spiegelbreuk_ook_andersom(tmp_path):
    _claude, agents, _ = _tree(tmp_path)
    _skill(agents, "paia-alleen-rechts")
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "paia-alleen-rechts" in result.stdout


def test_verbatim_spiegel_is_geen_duplicaat(tmp_path):
    """`.claude/skills/x` en `.agents/skills/x` zijn per ontwerp een paar. Bij
    een gevendorde externe skill is de spiegel verbatim en dus letterlijk
    identiek; dat mag de duplicaatcheck nooit melden."""
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-gespiegeld")
    _skill(agents, "paia-gespiegeld")
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout
    assert "FOUT" not in result.stdout


# --- Check 11: spiegel-inhoud (#981) ----------------------------------------
# Check 3 ziet alleen mapnamen. Deze check vergelijkt de inhoud van elk paar;
# bewuste verschillen per harnas staan in scripts/hooks/spiegel-uitzonderingen.txt.


def _uitzonderingen(tmp_path: Path) -> Path:
    return tmp_path / "repo" / "scripts" / "hooks" / "spiegel-uitzonderingen.txt"


def _leg_huidige_verschillen_vast(tmp_path: Path) -> None:
    """Schrijf de huidige verschillen weg zoals het script ze zelf print."""
    result = subprocess.run(
        [str(SCRIPT), "--print-spiegel-uitzonderingen"],
        capture_output=True,
        text=True,
        env=dict(os.environ, CHECK_INSTRUCTIES_ROOT=str(tmp_path / "repo")),
        check=True,
        timeout=30,
    )
    pad = _uitzonderingen(tmp_path)
    pad.parent.mkdir(parents=True, exist_ok=True)
    pad.write_text(result.stdout, encoding="utf-8")


def test_gelijke_spiegel_meldt_inhoud_gelijk(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-gelijk")
    _skill(agents, "paia-gelijk")
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout
    assert "ok    spiegel-inhoud" in result.stdout


def test_eenzijdige_wijziging_faalt(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-scheef", body="# Nieuwe regel\n")
    _skill(agents, "paia-scheef", body="# Oude regel\n")
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "FOUT  spiegel-inhoud" in result.stdout
    assert "paia-scheef/SKILL.md" in result.stdout
    assert "Nieuwe regel" in result.stdout


def test_vastgelegde_uitzondering_is_groen(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-harnas", body="Draai in Claude Code.\n")
    _skill(agents, "paia-harnas", body="Draai in Codex.\n")
    _leg_huidige_verschillen_vast(tmp_path)
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout
    assert "ok    spiegel-inhoud" in result.stdout


def test_nieuwe_drift_naast_uitzondering_faalt(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-harnas", body="Draai in Claude Code.\n")
    _skill(agents, "paia-harnas", body="Draai in Codex.\n")
    _leg_huidige_verschillen_vast(tmp_path)
    skill = claude / "paia-harnas" / "SKILL.md"
    skill.write_text(
        skill.read_text(encoding="utf-8") + "Alleen links.\n", encoding="utf-8"
    )
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "Alleen links." in result.stdout


def test_dezelfde_wijziging_aan_beide_kanten_blijft_groen(tmp_path):
    """De uitzonderingen dragen geen regelnummers: een gelijke edit aan beide
    kanten verschuift de verschillen, maar verandert ze niet."""
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-harnas", body="Draai in Claude Code.\n")
    _skill(agents, "paia-harnas", body="Draai in Codex.\n")
    _leg_huidige_verschillen_vast(tmp_path)
    for wortel in (claude, agents):
        skill = wortel / "paia-harnas" / "SKILL.md"
        # Direct na de afsluitende `---`: staat de regel vóór de frontmatter, dan
        # ziet `skill_meta` (awk eist `---` op regel 1) geen veld meer.
        skill.write_text(
            skill.read_text(encoding="utf-8").replace(
                "\n---\n", "\n---\nBovenaan erbij.\n", 1
            ),
            encoding="utf-8",
        )
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout
    assert "ok    spiegel-inhoud" in result.stdout


def test_extra_bestand_aan_een_kant_faalt(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-bijlage")
    _skill(agents, "paia-bijlage")
    (claude / "paia-bijlage" / "referentie.md").write_text(
        "Alleen links.\n", encoding="utf-8"
    )
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "paia-bijlage/referentie.md" in result.stdout


def test_verouderde_uitzondering_faalt_ook(tmp_path):
    """Een vastgelegd verschil dat niet meer bestaat, moet uit het bestand. Anders
    groeit het uitzonderingenbestand stil aan dode regels."""
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-harnas", body="Draai in Claude Code.\n")
    _skill(agents, "paia-harnas", body="Draai in Codex.\n")
    _leg_huidige_verschillen_vast(tmp_path)
    skill = agents / "paia-harnas" / "SKILL.md"
    skill.write_text(
        skill.read_text(encoding="utf-8").replace("Codex", "Claude Code"),
        encoding="utf-8",
    )
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "FOUT  spiegel-inhoud" in result.stdout


def test_onleesbaar_bestand_is_niet_gemeten_en_niet_gelijk(tmp_path):
    """diff exit 2 is "kon niet vergelijken". Dat mag nooit als "gelijk" lezen."""
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-dicht")
    _skill(agents, "paia-dicht")
    dicht = claude / "paia-dicht" / "SKILL.md"
    dicht.chmod(0)
    try:
        if os.access(dicht, os.R_OK):  # root leest alles; dan valt er niets te toetsen
            return
        result = _run(tmp_path)
    finally:
        dicht.chmod(0o644)
    assert "OVERGESLAGEN  spiegel-inhoud" in result.stdout, result.stdout
    assert "ok    spiegel-inhoud" not in result.stdout


def test_onleesbare_skillmap_is_niet_gemeten_en_niet_gelijk(tmp_path):
    """Kan find een map niet lezen, dan is de bestandslijst leeg. Leeg mag nooit
    als "geen verschil" lezen."""
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-dicht")
    _skill(agents, "paia-dicht")
    mappen = [claude / "paia-dicht", agents / "paia-dicht"]
    for directory in mappen:
        directory.chmod(0)
    try:
        if os.access(
            mappen[0], os.R_OK
        ):  # root leest alles; dan valt er niets te toetsen
            return
        result = _run(tmp_path)
    finally:
        for directory in mappen:
            directory.chmod(0o755)
    assert "OVERGESLAGEN  spiegel-inhoud" in result.stdout, result.stdout
    assert "ok    spiegel-inhoud" not in result.stdout


# --- overlap zonder afgrenzende clausule ------------------------------------

BREED = (
    "Gebruik bij het schrijven aanmaken bijwerken en opschonen van github "
    "issues labels milestones binnen deze repository."
)


def test_overlappende_descriptions_zonder_clausule_falen(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    for naam in ("paia-issue-een", "paia-issue-twee"):
        _skill(claude, naam, description=BREED)
        _skill(agents, naam, description=BREED)
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "overlap" in result.stdout
    assert "paia-issue-een" in result.stdout


def test_afgrenzende_clausule_maakt_overlap_acceptabel(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-issue-een", description=BREED)
    _skill(agents, "paia-issue-een", description=BREED)
    afgegrensd = BREED + " LET OP - gaat het om sluiten, gebruik paia-issue-een."
    _skill(claude, "paia-issue-twee", description=afgegrensd)
    _skill(agents, "paia-issue-twee", description=afgegrensd)
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout
    assert "FOUT" not in result.stdout


def test_scope_woord_zonder_andere_skill_telt_niet_als_clausule(tmp_path):
    """ "gebruik dan deze skill" verbreedt de trigger juist -- geen vrijbrief."""
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-issue-een", description=BREED)
    _skill(agents, "paia-issue-een", description=BREED)
    zwak = BREED + " LET OP - twijfel je, gebruik dan deze skill."
    _skill(claude, "paia-issue-twee", description=zwak)
    _skill(agents, "paia-issue-twee", description=zwak)
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "overlap" in result.stdout


def test_engelse_not_for_clausule_maakt_overlap_acceptabel(tmp_path):
    """De vorm van upstream `eas-hosting`: een external skill grenst in het Engels af."""
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-issue-een", description=BREED)
    _skill(agents, "paia-issue-een", description=BREED)
    afgegrensd = BREED + " Not for closing - use the paia-issue-een skill for those."
    _skill(claude, "paia-issue-twee", description=afgegrensd)
    _skill(agents, "paia-issue-twee", description=afgegrensd)
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout
    assert "FOUT" not in result.stdout


def test_not_for_binnen_een_langer_woord_telt_niet(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-issue-een", description=BREED)
    _skill(agents, "paia-issue-een", description=BREED)
    onecht = BREED + " You cannot format paia-issue-een."
    _skill(claude, "paia-issue-twee", description=onecht)
    _skill(agents, "paia-issue-twee", description=onecht)
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "overlap" in result.stdout


def test_scope_woord_binnen_een_langer_woord_telt_niet(tmp_path):
    """Het `buiten`-in-`buitenwereld`-geval uit CEDA's validator."""
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-issue-een", description=BREED)
    _skill(agents, "paia-issue-een", description=BREED)
    onecht = BREED + " Dit raakt ook de buitenwereld van paia-issue-een."
    _skill(claude, "paia-issue-twee", description=onecht)
    _skill(agents, "paia-issue-twee", description=onecht)
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout


def _skill_met_gevouwen_description(root: Path, naam: str) -> None:
    """Schrijft een SKILL.md met `description: >` (YAML-gevouwen blokscalar) in
    plaats van de platte `description: <tekst>` die `_skill()` schrijft --
    dezelfde inhoud als BREED, alleen over twee ingesprongen vervolgregels."""
    directory = root / naam
    directory.mkdir(parents=True, exist_ok=True)
    content = (
        "---\n"
        f"name: {naam}\n"
        "description: >\n"
        "  Gebruik bij het schrijven aanmaken bijwerken en opschonen van github\n"
        "  issues labels milestones binnen deze repository.\n"
        "---\n\n"
        f"# {naam}\n"
    )
    (directory / "SKILL.md").write_text(content, encoding="utf-8")


def test_gevouwen_description_faalt_net_als_platte_description(tmp_path):
    """Eindfixronde (#686): `skill_field()` matchte `description: >` op de
    letterlijke ">" van diezelfde regel en las de ingesprongen vervolgregels
    nooit -- check 4 vergeleek dan tegen ">" in plaats van de echte tekst en
    vond nooit gedeelde triggerwoorden. Twee skills die tien woorden delen,
    een plat en een gevouwen, moeten dezelfde FOUT geven."""
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-issue-een", description=BREED)
    _skill(agents, "paia-issue-een", description=BREED)
    _skill_met_gevouwen_description(claude, "paia-issue-twee")
    _skill_met_gevouwen_description(agents, "paia-issue-twee")
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "overlap" in result.stdout
    assert "paia-issue-een" in result.stdout and "paia-issue-twee" in result.stdout


# --- schaduwende personal skill ---------------------------------------------


def test_schaduwende_personal_skill_faalt(tmp_path):
    claude, agents, personal = _tree(tmp_path)
    _skill(claude, "paia-dubbel")
    _skill(agents, "paia-dubbel")
    (personal / "paia-dubbel").mkdir()
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "schaduw" in result.stdout
    assert "paia-dubbel" in result.stdout


def test_schaduwcheck_meldt_dat_hij_niet_kan_meten(tmp_path):
    """Een runner heeft geen ~/.claude/skills/. Dan OVERGESLAGEN, exit 0 --
    niet stil, en niet rood."""
    _tree(tmp_path)
    result = _run(tmp_path, personal=tmp_path / "bestaat-niet")
    assert result.returncode == 0, result.stdout
    # Op het check-voorvoegsel, niet op los "OVERGESLAGEN" plus "schaduw":
    # pytest noemt tmp_path naar de testfunctie, dus die padnaam staat al in de
    # skip-regel van check_scoping en de test toetste zo zijn eigen naam.
    assert "OVERGESLAGEN  schaduwcheck:" in result.stdout
    assert "bestaat niet" in result.stdout


# --- aanwezigheid van personal skills en agents (check 12) ---


def test_lege_personal_mappen_zijn_groen(tmp_path):
    _tree(tmp_path)
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "ok    aanwezigheid: geen personal skills of agents" in result.stdout


def test_niet_bestaande_personal_map_is_groen(tmp_path):
    """Geen map = er laadt niets = gemeten nul. Anders dan bij schaduw is dit geen skip."""
    _tree(tmp_path)
    result = _run(tmp_path, personal=tmp_path / "bestaat-niet")
    assert result.returncode == 0, result.stdout
    assert "ok    aanwezigheid: geen personal skills of agents" in result.stdout


def test_personal_skill_zonder_schaduw_faalt(tmp_path):
    """De kern van #905: geen naamsbotsing met de projectset, toch rood."""
    _, _, personal = _tree(tmp_path)
    _skill(personal, "ceda-iets", description="x" * 40)
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "FOUT  aanwezigheid: 1 personal skill(s)" in result.stdout
    assert "ceda-iets" in result.stdout
    assert "~12 tokens" in result.stdout  # (9 + 40) // 4
    assert "skills-archief" in result.stdout
    assert "ok    geen personal skill schaduwt een project-skill" in result.stdout


def test_personal_agent_faalt(tmp_path):
    _tree(tmp_path)
    agents = tmp_path / "personal" / "agents"
    agents.mkdir()
    (agents / "recensent.md").write_text(
        "---\nname: recensent\ndescription: Leest mee.\n---\n", encoding="utf-8"
    )
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "1 personal agent(s)" in result.stdout
    assert "recensent" in result.stdout


def test_verborgen_entries_tellen_niet(tmp_path):
    """Review Focus 1: de Beelink heeft ~/.claude/skills/.trash/."""
    _, _, personal = _tree(tmp_path)
    _skill(personal / ".trash", "weggegooid")
    agents = tmp_path / "personal" / "agents"
    (agents / ".oud").mkdir(parents=True)
    (agents / ".oud" / "x.md").write_text("---\nname: x\n---\n", encoding="utf-8")
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout
    assert "ok    aanwezigheid: geen personal skills of agents" in result.stdout


def test_personal_map_onder_verborgen_pad_telt_wel(tmp_path):
    """Review Focus 2: het echte pad ligt onder ~/.claude/."""
    _tree(tmp_path)
    personal = tmp_path / "home" / ".claude" / "skills"
    _skill(personal, "ceda-iets")
    result = _run(tmp_path, personal=personal)
    assert result.returncode == 1, result.stdout
    assert "FOUT  aanwezigheid: 1 personal skill(s)" in result.stdout


def test_gesymlinkte_skill_telt(tmp_path):
    """Review Focus 3."""
    _, _, personal = _tree(tmp_path)
    bron = _skill(tmp_path / "elders", "gelinkt")
    (personal / "gelinkt").symlink_to(bron, target_is_directory=True)
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "gelinkt" in result.stdout


def test_ontsnapping_is_groen_maar_luid(tmp_path):
    _, _, personal = _tree(tmp_path)
    _skill(personal, "ceda-iets")
    result = _run(tmp_path, extra_env={"CHECK_INSTRUCTIES_PERSONAL_OK": "1"})
    assert result.returncode == 0, result.stdout
    assert "ok    aanwezigheid: 1 personal skill(s)" in result.stdout
    assert "toegestaan via CHECK_INSTRUCTIES_PERSONAL_OK=1" in result.stdout


def test_ontsnapping_alleen_met_exact_1(tmp_path):
    """Review Focus 4."""
    _, _, personal = _tree(tmp_path)
    _skill(personal, "ceda-iets")
    result = _run(tmp_path, extra_env={"CHECK_INSTRUCTIES_PERSONAL_OK": "ja"})
    assert result.returncode == 1, result.stdout
    assert "FOUT  aanwezigheid:" in result.stdout


@pytest.mark.skipif(os.geteuid() == 0, reason="root leest ook een map met mode 000")
def test_onleesbare_personal_map_is_overgeslagen(tmp_path):
    """Review Focus 5."""
    _, _, personal = _tree(tmp_path)
    personal.chmod(0)
    try:
        result = _run(tmp_path)
    finally:
        personal.chmod(0o755)
    assert "OVERGESLAGEN  aanwezigheid:" in result.stdout
    assert "FOUT  aanwezigheid:" not in result.stdout


def test_synced_map_van_claude_ai_telt_niet(tmp_path):
    """`skills/synced/<account>/<skill>/` is de claude.ai-sync, geen eigen kopie.

    De ok-regel meldt wel hoeveel skills en tokens daar liggen: `synced/` telt
    niet mee voor de uitkomst, maar de meting wordt niet weggegooid.
    """
    _, _, personal = _tree(tmp_path)
    _skill(personal / "synced" / "0000-uuid", "iets", description="x" * 40)
    _skill(personal / "synced" / "0000-uuid", "ander", description="y" * 40)
    # Een verborgen entry op beide niveaus telt niet mee.
    _skill(personal / "synced" / ".trash", "weg")
    _skill(personal / "synced" / "0000-uuid" / ".oud", "weg2")
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout
    assert "ok    aanwezigheid: geen personal skills of agents" in result.stdout
    assert (
        f"ok    aanwezigheid: {personal}/synced/ niet meegeteld: 2 skill(s), "
        "~22 tokens (claude.ai-sync, beheer via claude.ai)"
    ) in result.stdout  # (4 + 40) // 4 + (5 + 40) // 4 = 11 + 11


def test_echte_skill_naast_synced_faalt_zonder_synced_te_tellen(tmp_path):
    _, _, personal = _tree(tmp_path)
    _skill(personal / "synced" / "0000-uuid", "iets")
    _skill(personal, "ceda-iets")
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "FOUT  aanwezigheid: 1 personal skill(s)" in result.stdout
    assert "ceda-iets" in result.stdout
    assert "niet meegeteld: 1 skill(s)" in result.stdout


def test_gesymlinkte_personal_skillsmap_wordt_gevolgd(tmp_path):
    """`find` volgt een start-symlink niet zonder -H: dat gaf een valse `ok`."""
    _tree(tmp_path)
    echt = tmp_path / "elders" / "skills"
    _skill(echt, "ceda-iets")
    link = tmp_path / "home" / ".claude" / "skills"
    link.parent.mkdir(parents=True)
    link.symlink_to(echt, target_is_directory=True)
    result = _run(tmp_path, personal=link)
    assert result.returncode == 1, result.stdout
    assert "FOUT  aanwezigheid: 1 personal skill(s)" in result.stdout


def test_gesymlinkte_personal_agentsmap_wordt_gevolgd(tmp_path):
    _tree(tmp_path)
    echt = tmp_path / "elders" / "agents"
    echt.mkdir(parents=True)
    (echt / "recensent.md").write_text(
        "---\nname: recensent\ndescription: Leest mee.\n---\n", encoding="utf-8"
    )
    link = tmp_path / "personal" / "agents"
    link.symlink_to(echt, target_is_directory=True)
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "1 personal agent(s)" in result.stdout


@pytest.mark.skipif(os.geteuid() == 0, reason="root leest ook een map met mode 000")
def test_onleesbare_submap_in_agents_is_overgeslagen(tmp_path):
    """De fout van `find` mag niet verdwijnen: zonder meting geen `ok`."""
    _tree(tmp_path)
    agents = tmp_path / "personal" / "agents"
    (agents / "sub").mkdir(parents=True)
    (agents / "sub").chmod(0)
    try:
        result = _run(tmp_path)
    finally:
        (agents / "sub").chmod(0o755)
    assert "OVERGESLAGEN  aanwezigheid:" in result.stdout
    assert "ok    aanwezigheid: geen" not in result.stdout


def test_skilltokens_tellen_de_frontmatter_naam(tmp_path):
    """De naam in de frontmatter, niet de mapnaam, telt voor de tokens."""
    _, _, personal = _tree(tmp_path)
    directory = personal / "abc"
    directory.mkdir()
    (directory / "SKILL.md").write_text(
        "---\nname: " + "n" * 20 + "\ndescription: " + "d" * 40 + "\n---\n",
        encoding="utf-8",
    )
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "~15 tokens" in result.stdout  # (20 + 40) // 4, niet (3 + 40) // 4
    assert "abc" in result.stdout


# --- naamcontract met vrijstelling ------------------------------------------


def test_skill_zonder_paia_voorvoegsel_faalt(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "losse-skill")
    _skill(agents, "losse-skill")
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "naamcontract" in result.stdout
    assert "losse-skill" in result.stdout


def test_externe_skill_met_herkomstkop_is_vrijgesteld(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    meta = {
        "origin": "external",
        "upstream": "https://github.com/expo/skills",
        "attribution": "(c) 2025 650 Industries",
        "upstream_path": "expo-deployment",
        "upstream_commit": '"' + "a" * 40 + '"',
    }
    _skill(claude, "expo-deployment", license="MIT", metadata=meta)
    _skill(agents, "expo-deployment", license="MIT", metadata=meta)
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout


def test_externe_skill_zonder_licentie_faalt_alsnog(tmp_path):
    """Zonder aantoonbare herkomst is de vrijstelling een gat waar elke skill
    doorheen past."""
    claude, agents, _ = _tree(tmp_path)
    meta = {"origin": "external", "upstream": "https://github.com/expo/skills"}
    _skill(claude, "expo-deployment", metadata=meta)
    _skill(agents, "expo-deployment", metadata=meta)
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "herkomstkop" in result.stdout


def test_leeg_upstream_veld_telt_als_ontbrekend(tmp_path):
    """`upstream:` zonder waarde mag de herkomstkop niet kunnen omzeilen."""
    claude, agents, _ = _tree(tmp_path)
    for wortel in (claude, agents):
        directory = wortel / "expo-deployment"
        directory.mkdir(parents=True)
        (directory / "SKILL.md").write_text(
            "---\nname: expo-deployment\ndescription: Deploy.\nlicense: MIT\n"
            "metadata:\n  origin: external\n  upstream:\n---\n",
            encoding="utf-8",
        )
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "herkomstkop" in result.stdout


# --- blokgrens van skill_meta() ---------------------------------------------
# Deze twee dekken de blokgrens-mutaties waar de volledige suite eerder blind
# voor was: (1) het gate-hergebruik van skill_field() voor origin/upstream
# i.p.v. skill_meta(), (2) skill_meta() die na het metadata:-blok blijft
# doorlezen alsof hij er nog in zit.


def test_top_level_origin_zonder_metadata_blok_faalt(tmp_path):
    """origin/upstream/license top-level, geen `metadata:`-blok in zicht.

    Vangt een regressie waarbij `check_naamcontract()` weer `skill_field()`
    gebruikt voor origin/upstream in plaats van `skill_meta()`: met
    `skill_field()` zouden deze drie top-level velden de vrijstelling
    aantoonbaar maken en de check ten onrechte op returncode 0 laten landen.
    """
    claude, agents, _ = _tree(tmp_path)
    for wortel in (claude, agents):
        directory = wortel / "expo-deployment"
        directory.mkdir(parents=True)
        (directory / "SKILL.md").write_text(
            "---\nname: expo-deployment\ndescription: Deploy.\n"
            "origin: external\nupstream: https://github.com/expo/skills\n"
            "license: MIT\n---\n",
            encoding="utf-8",
        )
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "naamcontract" in result.stdout
    assert "expo-deployment" in result.stdout


def test_veld_na_metadata_blok_lekt_niet_mee(tmp_path):
    """Een indented sleutel ná het `metadata:`-blok (onder een ander top-level
    veld) mag niet als metadata-sleutel gelezen worden.

    Vangt de mutatie die de blokgrens-regel
    (`in_meta && NF && $0 !~ /^[ \t]/ { in_meta = 0 }`) uit `skill_meta()`
    verwijdert: zonder die regel blijft `in_meta` na `metadata:` voor altijd
    aan staan en leest de helper een latere geneste `upstream:` -- ook al hoort
    die bij een heel ander top-level blok (`allowed-tools:`) -- alsof hij nog
    onder `metadata:` viel. `metadata:` draagt hier zelf GEEN `upstream`, dus
    de correcte lezing is "ontbreekt" en de herkomstkop hoort te falen; leest
    de helper de gelekte waarde mee, dan telt de vrijstelling ten onrechte als
    aantoonbaar en slaagt de check.
    """
    claude, agents, _ = _tree(tmp_path)
    for wortel in (claude, agents):
        directory = wortel / "expo-deployment"
        directory.mkdir(parents=True)
        (directory / "SKILL.md").write_text(
            "---\nname: expo-deployment\ndescription: Deploy.\nlicense: MIT\n"
            "metadata:\n  origin: external\nallowed-tools:\n"
            "  - name: Bash\n    upstream: https://nep\n---\n",
            encoding="utf-8",
        )
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "herkomstkop" in result.stdout
    assert "https://nep" not in result.stdout


def test_lege_regel_in_metadata_blok_breekt_de_lezing_niet(tmp_path):
    """Een lege regel middenin een geldig `metadata:`-blok mag het lezen niet
    afbreken -- `index("", " ")` geeft 0 (!= 1), dus een naïeve check op
    "begint met witruimte" zag de lege regel eerder aan voor het einde van het
    blok."""
    claude, agents, _ = _tree(tmp_path)
    for wortel in (claude, agents):
        directory = wortel / "expo-deployment"
        directory.mkdir(parents=True)
        (directory / "SKILL.md").write_text(
            "---\nname: expo-deployment\ndescription: Deploy.\nlicense: MIT\n"
            "metadata:\n  origin: external\n\n  upstream: https://x\n"
            '  upstream-path: x\n  upstream-commit: "' + "a" * 40 + '"\n'
            "  domain: factory\n---\n",
            encoding="utf-8",
        )
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout
    assert "FOUT" not in result.stdout


def test_metadata_met_trailing_whitespace_wordt_toch_herkend(tmp_path):
    """`metadata: ` (spatie erachter) hoort het blok net zo goed te openen als
    `metadata:`. Voor een `paia-`-skill gebeurt een gemiste opening stil, want
    die is vrijgesteld van `check_naamcontract()` -- daarom hier op een
    niet-`paia-`-skill, waar het wél zichtbaar wordt."""
    claude, agents, _ = _tree(tmp_path)
    for wortel in (claude, agents):
        directory = wortel / "expo-deployment"
        directory.mkdir(parents=True)
        (directory / "SKILL.md").write_text(
            "---\nname: expo-deployment\ndescription: Deploy.\nlicense: MIT\n"
            "metadata: \n  origin: external\n  upstream: https://x\n"
            '  upstream-path: x\n  upstream-commit: "' + "a" * 40 + '"\n'
            "  domain: factory\n---\n",
            encoding="utf-8",
        )
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout
    assert "FOUT" not in result.stdout


def _schrijf_rule(root, naam, front):
    """Zet een rule-bestand met de opgegeven frontmatter neer."""
    rules = root / "repo" / ".claude" / "rules"
    rules.mkdir(parents=True, exist_ok=True)
    (rules / naam).write_text(f"---\n{front}\n---\n\ntekst\n", encoding="utf-8")


def test_rule_zonder_paths_faalt(tmp_path):
    _schrijf_rule(tmp_path, "los.md", "description: iets")
    result = _run(tmp_path)
    assert result.returncode == 1
    assert "los.md" in result.stdout
    assert "paths:" in result.stdout


def test_rule_met_paths_is_groen(tmp_path):
    _schrijf_rule(tmp_path, "goed.md", "description: iets\npaths:\n  - src/**")
    result = _run(tmp_path)
    assert result.returncode == 0
    assert "elke rule is pad-gescoped" in result.stdout


def test_geen_rules_map_wordt_overgeslagen_niet_groen_gemeld(tmp_path):
    """De afwezigheid van een signaal is geen negatief resultaat."""
    result = _run(tmp_path)
    assert result.returncode == 0
    # Op het check-voorvoegsel: zie de toelichting bij de schaduwcheck-variant.
    assert "OVERGESLAGEN  scoping:" in result.stdout


def test_kale_paths_zonder_waarde_faalt(tmp_path):
    """`paths:` zonder lijst eronder en zonder inline waarde matcht geen
    enkel pad -- net als `skill_field()`/`skill_meta()` elders behandelt dit
    script een leeg veld als afwezig."""
    _schrijf_rule(tmp_path, "kaal.md", "description: iets\npaths:")
    result = _run(tmp_path)
    assert result.returncode == 1
    assert "kaal.md" in result.stdout
    assert "paths:" in result.stdout


def test_lege_paths_lijst_faalt(tmp_path):
    _schrijf_rule(tmp_path, "leeg.md", "description: iets\npaths: []")
    result = _run(tmp_path)
    assert result.returncode == 1
    assert "leeg.md" in result.stdout


def test_paths_woord_in_andere_regel_telt_niet_als_scoping(tmp_path):
    """Regressie tegen `index($0, "paths:") == 1` -> `> 0`: het woord
    `paths:` mid-regel (hier in een description) mag de check niet laten
    denken dat de rule een paths:-veld draagt."""
    _schrijf_rule(tmp_path, "vals.md", "description: zie ook paths: elders in de tekst")
    result = _run(tmp_path)
    assert result.returncode == 1
    assert "vals.md" in result.stdout


def test_paths_alleen_in_body_telt_niet_als_scoping(tmp_path):
    """Regressie tegen het schrappen van de frontmatter-guard: `paths:` ná de
    sluitende `---` (in de body) mag niet meetellen."""
    rules = tmp_path / "repo" / ".claude" / "rules"
    rules.mkdir(parents=True, exist_ok=True)
    (rules / "vals2.md").write_text(
        "---\ndescription: iets\n---\n\npaths:\n  - src/**\n", encoding="utf-8"
    )
    result = _run(tmp_path)
    assert result.returncode == 1
    assert "vals2.md" in result.stdout
    assert "paths:" in result.stdout


def test_lege_regel_voor_paths_lijst_is_geldig(tmp_path):
    """(E) Een lege regel tussen `paths:` en de lijst is geldige YAML en mag
    niet als "geen waarde" gelezen worden."""
    _schrijf_rule(tmp_path, "leegregel.md", "description: iets\npaths:\n\n  - src/**")
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout
    assert "FOUT" not in result.stdout


def test_yaml_comment_voor_paths_lijst_is_geldig(tmp_path):
    """(E) Een YAML-commentaarregel tussen `paths:` en de lijst is geldig.
    Zonder deze fix zou iemand die een toelichting in zijn `paths:`-blok zet
    "heeft geen paths:-blok met een niet-lege waarde" krijgen en foutloze
    YAML gaan verbouwen om de checker te plezieren."""
    _schrijf_rule(
        tmp_path,
        "comment.md",
        "description: iets\npaths:\n  # de twee mappen\n  - src/**",
    )
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout
    assert "FOUT" not in result.stdout


def test_paths_als_laatste_regel_voor_sluiting_blijft_falen(tmp_path):
    """(E) Randgeval uit de vorige ronde moet overeind blijven: `paths:` als
    láátste regel vóór de sluitende `---` (geen lijst, geen lege/commentaar-
    regel ervoor) is nog steeds "geen waarde" en moet FOUT geven. Dekt
    hetzelfde als `test_kale_paths_zonder_waarde_faalt` nogmaals, expliciet
    als vangnet voor de fix van deze ronde aan de getline-lus."""
    rules = tmp_path / "repo" / ".claude" / "rules"
    rules.mkdir(parents=True, exist_ok=True)
    (rules / "randgeval.md").write_text(
        "---\ndescription: iets\npaths:\n---\n\ntekst\n", encoding="utf-8"
    )
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "randgeval.md" in result.stdout


#: Sinds #686 staan binding/origin/upstream onder `metadata:`, niet top-level.
#: `skill_meta()` leest niets anders, dus een top-level fixture toetst niets.
STRENG = {"binding": "hard", "origin": "own"}


def test_binding_hard_zonder_geregistreerde_hook_faalt(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    for wortel in (claude, agents):
        _skill(
            wortel,
            "paia-streng",
            metadata=STRENG,
            body="Deze skill noemt geen enkele hook.\n",
        )
    result = _run(tmp_path)
    assert result.returncode == 1
    assert "paia-streng" in result.stdout
    assert "tegenhanger" in result.stdout


def test_binding_hard_met_geregistreerde_hook_is_groen(tmp_path):
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
    for wortel in (claude, agents):
        _skill(
            wortel,
            "paia-streng",
            metadata=STRENG,
            body="De afdwinging zit in `.claude/hooks/streng.sh`.\n",
        )
    result = _run(tmp_path)
    assert result.returncode == 0
    assert "elke binding: hard heeft een geregistreerde hook" in result.stdout


def test_settings_ontbreekt_faalt_toch_op_skill_zonder_hookpad(tmp_path):
    """Regressie: een `return 0` in de lus brak eerder de hele check af zodra
    de eerste hard-skill mét hookpad de ontbrekende settings.json tegenkwam,
    waardoor een latere hard-skill zónder hookpad -- een op zichzelf al
    meetbare bevinding -- nooit meer bekeken werd. `paia-aaa` (mét hookpad)
    komt alfabetisch vóór `paia-zzz` (zonder); zonder settings.json moet
    `paia-zzz` alsnog falen.

    (D) Tegelijk moet `paia-aaa` zelf niet stilzwijgend verdwijnen: die is
    onmeetbaar (geen settings.json) en hoort altijd een OVERGESLAGEN-regel op
    te leveren, ook naast de FOUT op `paia-zzz`. Eerder onderdrukte een
    `elif` op `gevonden` die regel zodra er ook maar één FOUT viel."""
    claude, agents, _ = _tree(tmp_path)
    for wortel in (claude, agents):
        _skill(
            wortel,
            "paia-aaa",
            metadata=STRENG,
            body="Zie `.claude/hooks/streng.sh`.\n",
        )
        _skill(
            wortel,
            "paia-zzz",
            metadata=STRENG,
            body="Deze skill noemt geen enkele hook.\n",
        )
    result = _run(tmp_path)
    assert result.returncode == 1
    assert "paia-zzz" in result.stdout
    assert "tegenhanger" in result.stdout
    # Specifiek de OVERGESLAGEN-regel van check_pairing, niet zomaar "OVERGESLAGEN"
    # ergens in de output -- deze fixture heeft ook geen `.claude/rules/`, dus
    # check_scoping() drukt zijn EIGEN (irrelevante) OVERGESLAGEN-regel af,
    # en een kale substring-check op "OVERGESLAGEN" zou daar per ongeluk op
    # slagen ook als check_pairing zijn eigen regel juist zou onderdrukken.
    assert "OVERGESLAGEN  pairing:" in result.stdout


def test_hookpad_genoemd_maar_niet_in_settings_faalt(tmp_path):
    """(C) hookpad genoemd, settings.json aanwezig, maar het pad staat er
    niet in -- moet FOUT geven. Vangt het schrappen van de
    registratie-controle in `hook_geregistreerd()`."""
    repo = tmp_path / "repo"
    hooks = repo / ".claude" / "hooks"
    hooks.mkdir(parents=True, exist_ok=True)
    (hooks / "streng.sh").write_text("#!/usr/bin/env bash\nexit 0\n", encoding="utf-8")
    (repo / ".claude" / "settings.json").write_text('{"hooks":{}}', encoding="utf-8")
    claude, agents, _ = _tree(tmp_path)
    for wortel in (claude, agents):
        _skill(
            wortel,
            "paia-streng",
            metadata=STRENG,
            body="De afdwinging zit in `.claude/hooks/streng.sh`.\n",
        )
    result = _run(tmp_path)
    assert result.returncode == 1
    assert "paia-streng" in result.stdout
    assert "tegenhanger" in result.stdout


def test_hookpad_geregistreerd_maar_script_ontbreekt_faalt(tmp_path):
    """(E) hookpad genoemd en in settings.json geregistreerd, maar het
    script zelf bestaat niet op disk -- moet FOUT geven. Vangt het schrappen
    van de `[ -f "$ROOT/$hook" ]`-controle."""
    repo = tmp_path / "repo"
    (repo / ".claude" / "hooks").mkdir(parents=True, exist_ok=True)
    (repo / ".claude" / "settings.json").write_text(
        '{"hooks":{"PostToolUse":[{"matcher":"Edit",'
        '"hooks":[{"type":"command","command":"$CLAUDE_PROJECT_DIR/.claude/hooks/streng.sh"}]}]}}',
        encoding="utf-8",
    )
    claude, agents, _ = _tree(tmp_path)
    for wortel in (claude, agents):
        _skill(
            wortel,
            "paia-streng",
            metadata=STRENG,
            body="De afdwinging zit in `.claude/hooks/streng.sh`.\n",
        )
    result = _run(tmp_path)
    assert result.returncode == 1
    assert "paia-streng" in result.stdout
    assert "tegenhanger" in result.stdout


def test_hook_in_permissions_deny_telt_niet_als_registratie(tmp_path):
    """`grep -qF` op het hele bestand zag het pad in `permissions.deny` ook
    als "geregistreerd" -- terwijl dat blok niets met het draaien van hooks
    te maken heeft. Alleen het `hooks`-blok telt."""
    repo = tmp_path / "repo"
    hooks = repo / ".claude" / "hooks"
    hooks.mkdir(parents=True, exist_ok=True)
    (hooks / "streng.sh").write_text("#!/usr/bin/env bash\nexit 0\n", encoding="utf-8")
    (repo / ".claude" / "settings.json").write_text(
        '{"hooks":{},"permissions":{"deny":[".claude/hooks/streng.sh"]}}',
        encoding="utf-8",
    )
    claude, agents, _ = _tree(tmp_path)
    for wortel in (claude, agents):
        _skill(
            wortel,
            "paia-streng",
            metadata=STRENG,
            body="De afdwinging zit in `.claude/hooks/streng.sh`.\n",
        )
    result = _run(tmp_path)
    assert result.returncode == 1
    assert "paia-streng" in result.stdout
    assert "tegenhanger" in result.stdout


def test_disabled_variant_telt_niet_als_registratie(tmp_path):
    """Een `command` die naar `streng.sh.disabled` wijst mag niet meetellen
    als registratie van `streng.sh` -- `grep -qF` zag het wél (substring),
    de padgrens-regex in `hook_geregistreerd()` niet."""
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
    for wortel in (claude, agents):
        _skill(
            wortel,
            "paia-streng",
            metadata=STRENG,
            body="De afdwinging zit in `.claude/hooks/streng.sh`.\n",
        )
    result = _run(tmp_path)
    assert result.returncode == 1
    assert "paia-streng" in result.stdout
    assert "tegenhanger" in result.stdout


def test_pairing_zonder_python3_wordt_overgeslagen(tmp_path):
    """(A) Zonder python3 kan `hook_geregistreerd()` niet meten. Dat mag
    nooit als FOUT verschijnen voor een overigens correct gepaarde skill --
    de gate zou dan "gemeten, iets gevonden" claimen terwijl er helemaal
    niets gemeten is."""
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
    for wortel in (claude, agents):
        _skill(
            wortel,
            "paia-streng",
            metadata=STRENG,
            body="De afdwinging zit in `.claude/hooks/streng.sh`.\n",
        )
    result = _run(tmp_path, extra_env={"PATH": _path_zonder_python3(tmp_path)})
    assert result.returncode == 0, result.stdout + result.stderr
    # Specifiek de OVERGESLAGEN-regel van check_pairing (zie de toelichting bij
    # test_settings_ontbreekt_faalt_toch_op_skill_zonder_hookpad hierboven).
    assert "OVERGESLAGEN  pairing:" in result.stdout
    assert "python3" in result.stdout
    assert "FOUT" not in result.stdout


def test_settings_json_niet_een_object_faalt_zonder_traceback(tmp_path):
    """(B) Geldige JSON die geen object is (hier: een lijst) mag
    `hook_geregistreerd()` niet laten crashen op `data.get(...)` -- een
    lijst/string heeft geen `.get()`. Vóór de fix gaf dit een onafgevangen
    AttributeError met een traceback op stderr."""
    repo = tmp_path / "repo"
    hooks = repo / ".claude" / "hooks"
    hooks.mkdir(parents=True, exist_ok=True)
    (hooks / "streng.sh").write_text("#!/usr/bin/env bash\nexit 0\n", encoding="utf-8")
    (repo / ".claude" / "settings.json").write_text("[1, 2, 3]", encoding="utf-8")
    claude, agents, _ = _tree(tmp_path)
    for wortel in (claude, agents):
        _skill(
            wortel,
            "paia-streng",
            metadata=STRENG,
            body="De afdwinging zit in `.claude/hooks/streng.sh`.\n",
        )
    result = _run(tmp_path)
    assert "Traceback" not in result.stderr
    assert "AttributeError" not in result.stderr
    assert result.returncode == 1, result.stdout + result.stderr
    # (C) de boodschap moet settings.json als oorzaak noemen, niet de
    # generieke "skill mist een tegenhanger"-tekst.
    assert "settings.json" in result.stdout
    assert "geldige JSON" in result.stdout
    assert "noemt geen tegenhanger" not in result.stdout


def test_kapotte_settings_json_faalt_met_duidelijke_oorzaak(tmp_path):
    """(C) Syntactisch ongeldige JSON mag niet op de skill-boodschap
    collapsen -- de lezer zou dan de skill-body gaan repareren terwijl de
    oorzaak settings.json is."""
    repo = tmp_path / "repo"
    hooks = repo / ".claude" / "hooks"
    hooks.mkdir(parents=True, exist_ok=True)
    (hooks / "streng.sh").write_text("#!/usr/bin/env bash\nexit 0\n", encoding="utf-8")
    (repo / ".claude" / "settings.json").write_text(
        "dit is geen json {", encoding="utf-8"
    )
    claude, agents, _ = _tree(tmp_path)
    for wortel in (claude, agents):
        _skill(
            wortel,
            "paia-streng",
            metadata=STRENG,
            body="De afdwinging zit in `.claude/hooks/streng.sh`.\n",
        )
    result = _run(tmp_path)
    assert "Traceback" not in result.stderr
    assert result.returncode == 1, result.stdout + result.stderr
    assert "settings.json" in result.stdout
    assert "geldige JSON" in result.stdout
    assert "noemt geen tegenhanger" not in result.stdout


def test_lege_settings_json_faalt_met_duidelijke_oorzaak(tmp_path):
    """(C) Een leeg settings.json-bestand is net zo min bruikbaar als een
    ontbrekend bestand, maar het bestáát wél -- dus is dit meetbaar (FOUT),
    geen OVERGESLAGEN, en met dezelfde onderscheidende boodschap."""
    repo = tmp_path / "repo"
    hooks = repo / ".claude" / "hooks"
    hooks.mkdir(parents=True, exist_ok=True)
    (hooks / "streng.sh").write_text("#!/usr/bin/env bash\nexit 0\n", encoding="utf-8")
    (repo / ".claude" / "settings.json").write_text("", encoding="utf-8")
    claude, agents, _ = _tree(tmp_path)
    for wortel in (claude, agents):
        _skill(
            wortel,
            "paia-streng",
            metadata=STRENG,
            body="De afdwinging zit in `.claude/hooks/streng.sh`.\n",
        )
    result = _run(tmp_path)
    assert "Traceback" not in result.stderr
    assert result.returncode == 1, result.stdout + result.stderr
    assert "settings.json" in result.stdout
    assert "geldige JSON" in result.stdout


def test_dit_script_is_groen_op_deze_repo(tmp_path):
    """De enige test die de drempels afstelt. Vuurt de overlapcheck op een echt
    paar, dan is de fix een afgrenzende clausule in die description -- niet een
    hogere drempel."""
    # Check 5 en 12 lezen $HOME; zonder deze isolatie blokkeert de personal-map
    # van de machine (een Mac met eigen skills) elke commit. Alleen de repo-boom
    # is hier het onderwerp.
    env = dict(
        os.environ,
        CHECK_INSTRUCTIES_PERSONAL=str(tmp_path / "personal" / "skills"),
        CHECK_INSTRUCTIES_PERSONAL_AGENTS=str(tmp_path / "personal" / "agents"),
    )
    env.pop("CHECK_INSTRUCTIES_PERSONAL_OK", None)
    result = subprocess.run(
        [str(SCRIPT)], capture_output=True, text=True, check=False, env=env
    )
    assert result.returncode == 0, result.stdout + result.stderr


# --- Check 9: herkomst, repo-breed ------------------------------------------
# Check 6 toetst upstream+license alleen op skills ZONDER paia--voorvoegsel,
# als voorwaarde voor de naamcontract-vrijstelling. Een paia--skill met
# `origin: extended` glipt daar doorheen -- precies paia-reflect-skills en
# paia-wait-what, waar de licentie in de upstream-regel staat i.p.v. in een
# license-veld.
#
#: `license` is een spec-veld en blijft top-level; binding/origin/upstream
#: staan sinds #686 onder `metadata:`, en `skill_meta()` leest niets anders.
HERKOMST_LICENTIE = {
    "metadata": {
        "binding": "default",
        "origin": "extended",
        "upstream": "https://example.test/x",
        "upstream_commit": '"' + "a" * 40 + '"',
    },
    "fields": {"license": "MIT"},
}
HERKOMST_IN_UPSTREAM = {
    "metadata": {
        "binding": "default",
        "origin": "extended",
        "upstream": "owner/repo@abc123 (MIT, (c) Iemand)",
        "upstream_commit": '"' + "a" * 40 + '"',
    },
    "fields": {},
}
HERKOMST_KAAL = {
    "metadata": {
        "binding": "default",
        "origin": "extended",
        "upstream": "https://example.test/x",
        "upstream_commit": '"' + "a" * 40 + '"',
    },
    "fields": {},
}


def _spiegel_skill(tmp_path, naam, vorm):
    """Dezelfde skill in beide spiegels; de gate eist dat ze gelijk zijn."""
    claude, agents, _ = _tree(tmp_path)
    for wortel in (claude, agents):
        _skill(wortel, naam, metadata=vorm["metadata"], **vorm["fields"])


def test_paia_skill_met_extended_origin_zonder_licentie_faalt(tmp_path):
    _spiegel_skill(tmp_path, "paia-geleend", HERKOMST_KAAL)
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "FOUT  herkomst:" in result.stdout
    assert "paia-geleend" in result.stdout
    assert "licentie" in result.stdout


def test_licentie_als_apart_veld_volstaat(tmp_path):
    _spiegel_skill(tmp_path, "paia-geleend", HERKOMST_LICENTIE)
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout + result.stderr
    assert (
        "ok    elke overgenomen skill draagt herkomst, commit en licentie"
        in result.stdout
    )


def test_licentie_in_de_upstream_regel_volstaat_ook(tmp_path):
    """paia-reflect-skills en paia-wait-what dragen hem zo; die vorm is geldig."""
    _spiegel_skill(tmp_path, "paia-geleend", HERKOMST_IN_UPSTREAM)
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout + result.stderr


def test_extended_zonder_upstream_faalt(tmp_path):
    # Geen `upstream`-sleutel: onder `metadata:` eist de spec een string, dus een
    # ontbrekende sleutel is precies wat "geen upstream" betekent.
    _spiegel_skill(
        tmp_path,
        "paia-geleend",
        {"metadata": {"origin": "extended"}, "fields": {"license": "MIT"}},
    )
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "FOUT  herkomst:" in result.stdout
    # Niet los "upstream": dat woord staat ook in de andere check-9-melding
    # ("...niet in de upstream-regel"), en `tmp_path` heet hier letterlijk
    # "test_extended_zonder_upstream_0" en wordt door check 7's
    # OVERGESLAGEN-regel afgedrukt -- beide geven een los "upstream" nul
    # onderscheidend vermogen. De staart "zonder upstream" komt alleen in de
    # upstream-fail-tak voor.
    assert "zonder upstream" in result.stdout


# --- check 9: upstream-commit en upstream-path (#1139) ----------------------
# De commit pint de upstream-versie die de kopie volgt; het pad wijst binnen de
# upstream-repo naar de bron. Het pad is alleen verplicht bij `external`: bij
# `extended` draagt vendor/*/PROVENANCE.md de bron.


def _herkomst_vorm(origin="extended", **metadata):
    """Een geldige herkomstvorm, plus de opgegeven extra metadata-velden."""
    return {
        "metadata": {
            "binding": "default",
            "origin": origin,
            "upstream": "https://example.test/x",
            **metadata,
        },
        "fields": {"license": "MIT"},
    }


def test_overgenomen_skill_zonder_upstream_commit_faalt(tmp_path):
    zonder = {
        "metadata": {
            k: v
            for k, v in HERKOMST_LICENTIE["metadata"].items()
            if k != "upstream_commit"
        },
        "fields": HERKOMST_LICENTIE["fields"],
    }
    _spiegel_skill(tmp_path, "paia-geleend", zonder)
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "FOUT  herkomst:" in result.stdout
    assert "upstream-commit" in result.stdout


def test_korte_sha_in_upstream_commit_faalt(tmp_path):
    _spiegel_skill(
        tmp_path,
        "paia-geleend",
        _herkomst_vorm(upstream_commit='"8dc9db43c9bf"'),
    )
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "FOUT  herkomst:" in result.stdout
    # "40" alleen kan matchen op tmp_path; "40-tekens" komt alleen in de fout voor.
    assert "40-tekens" in result.stdout


def test_sha_in_hoofdletters_faalt(tmp_path):
    _spiegel_skill(
        tmp_path,
        "paia-geleend",
        _herkomst_vorm(upstream_commit='"' + "A" * 40 + '"'),
    )
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "40-tekens" in result.stdout


def test_ongequote_sha_volstaat(tmp_path):
    _spiegel_skill(
        tmp_path,
        "paia-geleend",
        _herkomst_vorm(upstream_commit="b" * 40),
    )
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout + result.stderr


def test_external_zonder_upstream_path_faalt(tmp_path):
    _spiegel_skill(
        tmp_path,
        "expo-deployment",
        _herkomst_vorm(origin="external", upstream_commit='"' + "a" * 40 + '"'),
    )
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "FOUT  herkomst:" in result.stdout
    assert "upstream-path" in result.stdout


def test_external_met_upstream_path_volstaat(tmp_path):
    _spiegel_skill(
        tmp_path,
        "expo-deployment",
        _herkomst_vorm(
            origin="external",
            upstream_commit='"' + "a" * 40 + '"',
            upstream_path="skills/x",
        ),
    )
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout + result.stderr


def test_external_met_lege_gequote_upstream_path_faalt(tmp_path):
    _spiegel_skill(
        tmp_path,
        "expo-deployment",
        _herkomst_vorm(
            origin="external",
            upstream_commit='"' + "a" * 40 + '"',
            upstream_path='""',
        ),
    )
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "FOUT  herkomst:" in result.stdout
    assert "upstream-path" in result.stdout


def test_upstream_path_met_voorloop_slash_faalt(tmp_path):
    _spiegel_skill(
        tmp_path,
        "expo-deployment",
        _herkomst_vorm(
            origin="external",
            upstream_commit='"' + "a" * 40 + '"',
            upstream_path="/skills/x",
        ),
    )
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "FOUT  herkomst:" in result.stdout
    assert "voorloop-/" in result.stdout


def test_extended_zonder_upstream_path_volstaat(tmp_path):
    _spiegel_skill(tmp_path, "paia-geleend", HERKOMST_LICENTIE)
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout + result.stderr


def test_licentie_patroon_matcht_niet_midden_in_woord(tmp_path):
    """`grep -qE 'MIT|...'` zonder woordgrenzen gaf zes gemeten valse groenen:
    COMMIT (COM-MIT), SUBMITTED, EXAMPLE, SIMPLE-fork, (MISC notes) en SAN
    FRANCISCO matchten allemaal een licentienaam middenin een woord. Deze
    upstream-waarde bevat 'MIT' als losse letters in COMMIT, geen los woord
    'MIT' -- zonder license:-veld hoort dit een FOUT te zijn, geen ok."""
    _spiegel_skill(
        tmp_path,
        "paia-geleend",
        {
            "metadata": {
                "binding": "default",
                "origin": "extended",
                "upstream": "owner/repo COMMIT abc123",
                "upstream_commit": '"' + "a" * 40 + '"',
            },
            "fields": {},
        },
    )
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "FOUT  herkomst:" in result.stdout
    assert "paia-geleend" in result.stdout
    assert "licentievermelding" in result.stdout


def test_licentie_patroon_matcht_niet_zonder_haakje_of_komma(tmp_path):
    """`github.com/foo/mit-tools` bevat 'mit' met een letter-grens ervoor en
    erna (`/` resp. `-`), maar geen `(` of `,` ervoor. Een licentievermelding
    staat achter een haakje of komma (zie de twee vendorde skills en de
    HERKOMST_IN_UPSTREAM-fixture); een licentienaam in een pad niet. Zonder
    dit structurele anker gaf een letter-grens hier alsnog 'ok' -- een
    licentienaam middenin een pad is geen licentievermelding."""
    _spiegel_skill(
        tmp_path,
        "paia-geleend",
        {
            "metadata": {
                "binding": "default",
                "origin": "extended",
                "upstream": "github.com/foo/mit-tools",
                "upstream_commit": '"' + "a" * 40 + '"',
            },
            "fields": {},
        },
    )
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "FOUT  herkomst:" in result.stdout
    assert "paia-geleend" in result.stdout
    assert "licentievermelding" in result.stdout


def test_licentie_patroon_heeft_ook_een_rechtergrens(tmp_path):
    """'(MITRE-fork)' heeft de linkergrens (een `(` vlak voor 'MIT') maar is
    geen licentienaam -- het woord loopt door naar 'RE-fork'. Zonder
    `([^A-Za-z]|$)` aan de rechterkant matcht het toch, want dan telt alleen
    of er een `(`/`,` vóór 'MIT' staat, niet wat erna komt."""
    _spiegel_skill(
        tmp_path,
        "paia-geleend",
        {
            "metadata": {
                "binding": "default",
                "origin": "extended",
                "upstream": "owner/repo (MITRE-fork)",
                "upstream_commit": '"' + "a" * 40 + '"',
            },
            "fields": {},
        },
    )
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "FOUT  herkomst:" in result.stdout
    assert "paia-geleend" in result.stdout
    assert "licentievermelding" in result.stdout


def test_licentie_patroon_is_hoofdletterongevoelig(tmp_path):
    """`(apache-2.0)` in kleine letters moet net zo goed tellen als
    `(Apache-2.0)` -- vandaar `grep -qiE` i.p.v. `-qE`. Met het structurele
    anker (`[(,]` vóór de naam) is dat veilig: een licentienaam in een pad
    staat nooit direct achter een `(` of `,`."""
    _spiegel_skill(
        tmp_path,
        "paia-geleend",
        {
            "metadata": {
                "binding": "default",
                "origin": "extended",
                "upstream": "owner/repo@abc123 (apache-2.0, (c) Iemand)",
                "upstream_commit": '"' + "a" * 40 + '"',
            },
            "fields": {},
        },
    )
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout + result.stderr
    # Zonder deze regel blijft de test ook groen als check 9 helemaal niet
    # draait -- dan bewijst hij niets over de hoofdletterongevoeligheid.
    assert (
        "ok    elke overgenomen skill draagt herkomst, commit en licentie"
        in result.stdout
    )


def test_origin_own_hoeft_geen_upstream(tmp_path):
    _spiegel_skill(
        tmp_path,
        "paia-eigen",
        {"metadata": {"binding": "default", "origin": "own"}, "fields": {}},
    )
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout + result.stderr
    assert (
        "ok    elke overgenomen skill draagt herkomst, commit en licentie"
        in result.stdout
    )


# --- check 10: omvang, het plafond op CLAUDE.md -----------------------------
# DoD van milestone #42, permanent gemaakt: zonder teller kruipt het
# woordental gewoon weer omhoog, zoals tussen 2026-08-22 en 2026-09-02
# gebeurde (3046 -> 5053). `CHECK_INSTRUCTIES_PLAFOND` laat de test een eigen
# drempel zetten in plaats van tegen de echte 3000 te meten.


def test_claude_md_boven_het_plafond_faalt(tmp_path):
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "CLAUDE.md").write_text("woord " * 50, encoding="utf-8")
    result = _run(tmp_path, extra_env={"CHECK_INSTRUCTIES_PLAFOND": "10"})
    assert result.returncode == 1, result.stdout
    assert "FOUT  omvang:" in result.stdout
    assert "plafond" in result.stdout


def test_claude_md_op_het_plafond_is_groen(tmp_path):
    """De grens zelf, niet alleen de kanten -- op 10 is 10 nog geen `> 10`."""
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "CLAUDE.md").write_text("woord " * 10, encoding="utf-8")
    result = _run(tmp_path, extra_env={"CHECK_INSTRUCTIES_PLAFOND": "10"})
    assert result.returncode == 0, result.stdout + result.stderr
    assert "ok    CLAUDE.md telt 10 woorden, binnen het plafond van 10" in result.stdout


def test_claude_md_een_boven_het_plafond_faalt(tmp_path):
    """De andere kant van de grens: 11 op een plafond van 10 is al FOUT."""
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "CLAUDE.md").write_text("woord " * 11, encoding="utf-8")
    result = _run(tmp_path, extra_env={"CHECK_INSTRUCTIES_PLAFOND": "10"})
    assert result.returncode == 1, result.stdout
    assert "FOUT  omvang: CLAUDE.md telt 11 woorden, plafond is 10" in result.stdout


def test_claude_md_onder_het_plafond_is_groen(tmp_path):
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "CLAUDE.md").write_text("woord " * 5, encoding="utf-8")
    result = _run(tmp_path, extra_env={"CHECK_INSTRUCTIES_PLAFOND": "10"})
    assert result.returncode == 0, result.stdout + result.stderr
    assert "ok    CLAUDE.md telt 5 woorden, binnen het plafond van 10" in result.stdout


def test_geen_claude_md_wordt_overgeslagen(tmp_path):
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "OVERGESLAGEN  omvang:" in result.stdout


def test_onleesbare_claude_md_wordt_overgeslagen_niet_ok(tmp_path):
    """Eindfixronde (#686): mode 000 liet `wc -w` een lege stdout teruggeven,
    waarna `[ "" -gt "$PLAFOND" ]` op stderr faalde en de if-tak stil doorviel
    naar `ok "CLAUDE.md telt  woorden, binnen het plafond"` -- exit 0, geen
    FOUT, geen OVERGESLAGEN. Onleesbaar moet zich hetzelfde gedragen als
    afwezig: OVERGESLAGEN, nooit een stille ok."""
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    bestand = repo / "CLAUDE.md"
    bestand.write_text("woord " * 5, encoding="utf-8")
    bestand.chmod(0o000)
    try:
        result = _run(tmp_path)
    finally:
        bestand.chmod(0o644)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "OVERGESLAGEN  omvang:" in result.stdout
    assert "niet leesbaar" in result.stdout
    assert "ok    CLAUDE.md telt" not in result.stdout


def test_agents_md_telt_mee_naast_claude_md(tmp_path):
    """CLAUDE.md is een `@AGENTS.md`-import plus de Claude-rest: het plafond
    geldt voor wat Claude Code werkelijk laadt, dus voor beide samen."""
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "CLAUDE.md").write_text("woord " * 4, encoding="utf-8")
    (repo / "AGENTS.md").write_text("woord " * 6, encoding="utf-8")
    result = _run(tmp_path, extra_env={"CHECK_INSTRUCTIES_PLAFOND": "10"})
    assert result.returncode == 0, result.stdout + result.stderr
    assert (
        "ok    CLAUDE.md + AGENTS.md telt 10 woorden, binnen het plafond van 10"
        in result.stdout
    )


def test_agents_md_duwt_de_som_over_het_plafond(tmp_path):
    """Bewezen placebo zonder deze test: een dunne CLAUDE.md blijft altijd
    onder het plafond, hoe groot het geïmporteerde AGENTS.md ook wordt."""
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "CLAUDE.md").write_text("@AGENTS.md\n", encoding="utf-8")
    (repo / "AGENTS.md").write_text("woord " * 10, encoding="utf-8")
    result = _run(tmp_path, extra_env={"CHECK_INSTRUCTIES_PLAFOND": "10"})
    assert result.returncode == 1, result.stdout
    assert (
        "FOUT  omvang: CLAUDE.md + AGENTS.md telt 11 woorden, plafond is 10"
        in result.stdout
    )


def test_onleesbare_agents_md_wordt_overgeslagen_niet_ok(tmp_path):
    """Een onleesbaar AGENTS.md is niet gemeten: OVERGESLAGEN, geen stille ok
    op alleen de woorden van CLAUDE.md."""
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "CLAUDE.md").write_text("woord " * 5, encoding="utf-8")
    bestand = repo / "AGENTS.md"
    bestand.write_text("woord " * 5, encoding="utf-8")
    bestand.chmod(0o000)
    try:
        result = _run(tmp_path)
    finally:
        bestand.chmod(0o644)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "OVERGESLAGEN  omvang:" in result.stdout
    assert "niet leesbaar" in result.stdout
    assert "telt" not in result.stdout.split("omvang")[-1]


def test_plafond_default_is_3000_zonder_override(tmp_path):
    """Zonder `CHECK_INSTRUCTIES_PLAFOND` geldt de repo-default van 3000."""
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "CLAUDE.md").write_text("woord " * 5, encoding="utf-8")
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout + result.stderr
    assert (
        "ok    CLAUDE.md telt 5 woorden, binnen het plafond van 3000" in result.stdout
    )


# --- Check 9, tweede lus: de vorm van `source` ------------------------------
# Anders dan upstream hierboven geldt deze lus voor ALLE skills, niet alleen
# voor `origin: extended|external`. Afwezigheid is geen fout (geen backfill,
# #686-lijn); een aanwezige waarde moet een van de vijf vormen zijn.


def _skill_met_source(tmp_path, bron: str | None):
    """Eén paia-skill in beide spiegels, met of zonder `source` onder metadata."""
    claude, agents, _ = _tree(tmp_path)
    metadata = {"binding": "default", "origin": "own"}
    if bron is not None:
        metadata["source"] = bron
    for wortel in (claude, agents):
        _skill(wortel, "paia-nieuw", metadata=metadata)
    return claude


def test_skill_zonder_source_is_groen(tmp_path):
    """Geen backfill: de 33 bestaande skills dragen `source` niet, en dat mag
    geen rode gate opleveren."""
    _skill_met_source(tmp_path, None)
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "ok    elke source wijst ergens heen" in result.stdout


def test_source_self_is_groen(tmp_path):
    """`self` is de ontsnapping; de zwaardere eis erop (substantie zelf dragen)
    is proza in de skill, niet machinaal te toetsen."""
    _skill_met_source(tmp_path, "self")
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout + result.stderr


def test_source_issuenummer_is_groen(tmp_path):
    """Geciteerd, want ongequote `#914` leest YAML als commentaar."""
    _skill_met_source(tmp_path, '"#914"')
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout + result.stderr


def test_source_commit_sha_is_groen(tmp_path):
    _skill_met_source(tmp_path, "68af7e22")
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout + result.stderr


def test_source_url_is_groen(tmp_path):
    _skill_met_source(tmp_path, "https://agentskills.io/specification")
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout + result.stderr


def test_source_bestaand_pad_is_groen(tmp_path):
    """Een pad wordt tegen de repo-wortel geresolvet, niet tegen de cwd."""
    _skill_met_source(tmp_path, "docs/developer/planning/specs/ontwerp.md")
    spec = tmp_path / "repo" / "docs" / "developer" / "planning" / "specs"
    spec.mkdir(parents=True)
    (spec / "ontwerp.md").write_text("# ontwerp\n", encoding="utf-8")
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout + result.stderr


def test_source_naar_verplaatst_bestand_faalt(tmp_path):
    """Dit is de fout die anders stil blijft: het ontwerp wordt hernoemd of
    verplaatst en de skill blijft naar het oude pad wijzen. Niemand merkt dat,
    want niets leest dat pad ooit."""
    _skill_met_source(tmp_path, "docs/developer/planning/specs/weg.md")
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "FOUT  herkomst:" in result.stdout
    assert "paia-nieuw" in result.stdout
    assert "weg.md" in result.stdout


def test_source_met_onzinwaarde_faalt(tmp_path):
    """Een vrije tekst is geen van de vijf vormen; zonder deze tak wordt
    `source` een veld waar iemand "uit mijn hoofd" in kan zetten."""
    _skill_met_source(tmp_path, "uit mijn hoofd")
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "FOUT  herkomst:" in result.stdout
    assert "paia-nieuw" in result.stdout


def test_source_lus_draait_ook_op_skills_zonder_overgenomen_origin(tmp_path):
    """De upstream-lus filtert op `origin: extended|external`; deze lus mag dat
    NIET doen. Een `origin: own`-skill met een kapotte `source` hoort te falen
    -- zonder deze test is een vroege `continue` op origin niet te zien."""
    claude, agents, _ = _tree(tmp_path)
    for wortel in (claude, agents):
        _skill(
            wortel,
            "paia-eigen",
            metadata={"origin": "own", "source": "docs/bestaat-niet.md"},
        )
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "paia-eigen" in result.stdout


# --- domein (check 13, #1161) ---


def _domein_boom(tmp_path: Path, claude_domein, agents_domein, naam="paia-x"):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, naam, domein=claude_domein)
    _skill(agents, naam, domein=agents_domein)


def test_skill_zonder_domein_faalt(tmp_path):
    _domein_boom(tmp_path, None, None)
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "domein" in result.stdout
    assert "paia-x" in result.stdout
    assert ".claude/skills" in result.stdout


def test_onbekend_domein_faalt(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    for naam, domein in (("paia-een", "Factory"), ("paia-twee", "fabriek")):
        _skill(claude, naam, description=f"Doet {naam}.", domein=domein)
        _skill(agents, naam, description=f"Doet {naam}.", domein=domein)
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "'Factory'" in result.stdout
    assert "'fabriek'" in result.stdout
    assert "applicatie, inhoud, factory, infra" in result.stdout


def test_gequote_domein_is_geldig(tmp_path):
    _domein_boom(tmp_path, '"inhoud"', '"inhoud"')
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout
    assert "FOUT" not in result.stdout


@pytest.mark.parametrize("domein", ["\"'factory'\"", "'\"factory\"'"])
def test_genest_gequote_domein_faalt(tmp_path, domein):
    """Eén quotepaar is YAML-quoting; het tweede paar hoort bij de waarde."""
    _domein_boom(tmp_path, domein, domein)
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "geen van applicatie, inhoud, factory, infra" in result.stdout


@pytest.mark.parametrize("domein", ["applicatie", "inhoud", "factory", "infra"])
def test_elk_domein_is_geldig(tmp_path, domein):
    _domein_boom(tmp_path, domein, domein)
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout
    assert "ok    elke skill in beide spiegels draagt een domein" in result.stdout


def test_leeg_domein_telt_als_ontbrekend(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    for wortel in (claude, agents):
        directory = wortel / "paia-x"
        directory.mkdir()
        (directory / "SKILL.md").write_text(
            "---\nname: paia-x\ndescription: Doet iets nuttigs.\n"
            "metadata:\n  domain:\n---\n",
            encoding="utf-8",
        )
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "mist metadata.domain" in result.stdout


def test_top_level_domein_telt_als_ontbrekend(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-x", domein=None, domain="factory")
    _skill(agents, "paia-x", domein=None, domain="factory")
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "mist metadata.domain" in result.stdout


def test_alleen_agents_spiegel_mist_domein(tmp_path):
    _domein_boom(tmp_path, "factory", None)
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    regels = [r for r in result.stdout.splitlines() if r.startswith("FOUT  domein")]
    assert len(regels) == 1, result.stdout
    assert ".agents/skills" in regels[0]
    assert ".claude/skills" not in regels[0]


def test_ander_domein_per_spiegel_faalt(tmp_path):
    """Beide waarden zijn geldig, dus check 13 keurt ze goed; check 11 vangt het."""
    _domein_boom(tmp_path, "factory", "inhoud")
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "FOUT  domein" not in result.stdout
    assert "FOUT  spiegel-inhoud" in result.stdout


@pytest.mark.skipif(os.geteuid() == 0, reason="root leest ook een bestand met mode 000")
def test_onleesbare_skill_is_overgeslagen_niet_ok(tmp_path):
    """Kon het domein niet gelezen worden, dan is dat geen negatief resultaat en
    ook geen geslaagde meting: OVERGESLAGEN, nooit de ok-regel."""
    _domein_boom(tmp_path, "factory", "factory")
    dicht = tmp_path / "repo" / ".claude" / "skills" / "paia-x" / "SKILL.md"
    dicht.chmod(0)
    try:
        result = _run(tmp_path)
    finally:
        dicht.chmod(0o644)
    assert "OVERGESLAGEN  domein: '.claude/skills/paia-x' niet leesbaar" in (
        result.stdout
    ), result.stdout
    assert "ok    elke skill in beide spiegels draagt een domein" not in result.stdout
    assert "FOUT  domein" not in result.stdout
