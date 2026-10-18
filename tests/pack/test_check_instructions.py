"""`pack/core/scripts/check-instructions.sh`: de generieke checks over de instructielaag.

Het script draait als subprocess tegen tijdelijke bomen, zodat het contract
(exit code + stdout) getest wordt en niet een herimplementatie ervan. Het draait
vanaf zijn geïnstalleerde plek (`_testkit.INSTALL_ROOT`), zodat het zijn configlezer
in `.lighthouse/lib` vindt zoals in een project. Checks 1, 2, 3, 4, 5, 7, 8, 10 en
11 dragen hun nummer uit de bron; 6, 9, 12 en 13 zijn projecteigen en zitten er niet in.
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
    """Schrijf een minimale SKILL.md.

    `fields` gaat ongewijzigd top-level de frontmatter in (spec- en vendor-velden).
    `metadata` nest onder een `metadata:`-blok, waar binding en de
    lighthouse-marker thuishoren, conform de Agent Skills-spec. `body` is de tekst
    ná de frontmatter (default de oude kale kop); check 8 grept daarin naar een
    hook-pad.
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
    """Een lege repo-boom plus een lege personal-map. Alle drie bestaan."""
    claude = tmp_path / "repo" / ".claude" / "skills"
    agents = tmp_path / "repo" / ".agents" / "skills"
    personal = tmp_path / "personal" / "skills"
    for directory in (claude, agents, personal):
        directory.mkdir(parents=True)
    return claude, agents, personal


def _plafond(n: int) -> str:
    return f"[project]\ninstructions_word_ceiling = {n}\n"


def _env(tmp_path: Path, toml: str) -> dict:
    """De omgeving van één run: de repo-boom als root, en een eigen `lighthouse.toml`
    buiten die boom, zodat een test nooit de config van de machine leest."""
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
    # `timeout`: het script is deterministisch en draait hier standaard in
    # enkele seconden. Een hang hoort dus als test-failure te verschijnen,
    # niet als een oneindig wachtende testrun.
    return subprocess.run(
        [str(SCRIPT), *args],
        capture_output=True,
        text=True,
        env=env,
        check=False,
        timeout=30,
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
    assert "FAIL" not in result.stdout


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
    assert "duplicate name" in result.stdout
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
    assert "byte-identical" in result.stdout


def test_spiegelbreuk_faalt(tmp_path):
    claude, _agents, _ = _tree(tmp_path)
    _skill(claude, "paia-alleen-links")
    result = _run(tmp_path)
    assert result.returncode == 1, result.stdout
    assert "mirror" in result.stdout
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
    assert "FAIL" not in result.stdout


# --- Check 11: spiegel-inhoud (#981) ----------------------------------------
# Check 3 ziet alleen mapnamen. Deze check vergelijkt de inhoud van elk paar;
# bewuste verschillen per harnas staan in het bestand van `[paths] mirror_exceptions`.

SPIEGEL_TOML = '[paths]\nmirror_exceptions = "mirror-exceptions.txt"\n'


def _uitzonderingen(tmp_path: Path) -> Path:
    return tmp_path / "repo" / "mirror-exceptions.txt"


def _leg_huidige_verschillen_vast(tmp_path: Path) -> None:
    """Schrijf de huidige verschillen weg zoals het script ze zelf print."""
    result = subprocess.run(
        [str(SCRIPT), "--print-mirror-exceptions"],
        capture_output=True,
        text=True,
        env=_env(tmp_path, SPIEGEL_TOML),
        check=True,
        timeout=30,
    )
    _uitzonderingen(tmp_path).write_text(result.stdout, encoding="utf-8")


def test_gelijke_spiegel_meldt_inhoud_gelijk(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-gelijk")
    _skill(agents, "paia-gelijk")
    result = _run(tmp_path, toml=SPIEGEL_TOML)
    assert result.returncode == 0, result.stdout
    assert "ok    mirror-content" in result.stdout


def test_eenzijdige_wijziging_faalt(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-scheef", body="# Nieuwe regel\n")
    _skill(agents, "paia-scheef", body="# Oude regel\n")
    result = _run(tmp_path, toml=SPIEGEL_TOML)
    assert result.returncode == 1, result.stdout
    assert "FAIL  mirror-content" in result.stdout
    assert "paia-scheef/SKILL.md" in result.stdout
    assert "Nieuwe regel" in result.stdout


def test_vastgelegde_uitzondering_is_groen(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-harnas", body="Draai in Claude Code.\n")
    _skill(agents, "paia-harnas", body="Draai in Codex.\n")
    _leg_huidige_verschillen_vast(tmp_path)
    result = _run(tmp_path, toml=SPIEGEL_TOML)
    assert result.returncode == 0, result.stdout
    assert "ok    mirror-content" in result.stdout


def test_nieuwe_drift_naast_uitzondering_faalt(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-harnas", body="Draai in Claude Code.\n")
    _skill(agents, "paia-harnas", body="Draai in Codex.\n")
    _leg_huidige_verschillen_vast(tmp_path)
    skill = claude / "paia-harnas" / "SKILL.md"
    skill.write_text(
        skill.read_text(encoding="utf-8") + "Alleen links.\n", encoding="utf-8"
    )
    result = _run(tmp_path, toml=SPIEGEL_TOML)
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
    result = _run(tmp_path, toml=SPIEGEL_TOML)
    assert result.returncode == 0, result.stdout
    assert "ok    mirror-content" in result.stdout


def test_extra_bestand_aan_een_kant_faalt(tmp_path):
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-bijlage")
    _skill(agents, "paia-bijlage")
    (claude / "paia-bijlage" / "referentie.md").write_text(
        "Alleen links.\n", encoding="utf-8"
    )
    result = _run(tmp_path, toml=SPIEGEL_TOML)
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
    result = _run(tmp_path, toml=SPIEGEL_TOML)
    assert result.returncode == 1, result.stdout
    assert "FAIL  mirror-content" in result.stdout


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
        result = _run(tmp_path, toml=SPIEGEL_TOML)
    finally:
        dicht.chmod(0o644)
    assert "SKIPPED  mirror-content" in result.stdout, result.stdout
    assert "ok    mirror-content" not in result.stdout


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
        result = _run(tmp_path, toml=SPIEGEL_TOML)
    finally:
        for directory in mappen:
            directory.chmod(0o755)
    assert "SKIPPED  mirror-content" in result.stdout, result.stdout
    assert "ok    mirror-content" not in result.stdout


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
    assert "FAIL" not in result.stdout


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
    assert "FAIL" not in result.stdout


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
    assert "shadow" in result.stdout
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
    assert "SKIPPED  shadow:" in result.stdout
    assert "does not exist" in result.stdout



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
    assert "every rule is path-scoped" in result.stdout


def test_geen_rules_map_wordt_overgeslagen_niet_groen_gemeld(tmp_path):
    """De afwezigheid van een signaal is geen negatief resultaat."""
    result = _run(tmp_path)
    assert result.returncode == 0
    # Op het check-voorvoegsel: zie de toelichting bij de schaduwcheck-variant.
    assert "SKIPPED  scoping:" in result.stdout


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
    assert "FAIL" not in result.stdout


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
    assert "FAIL" not in result.stdout


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
    assert "counterpart" in result.stdout


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
    assert "every binding: hard has a registered hook" in result.stdout


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
    assert "counterpart" in result.stdout
    # Specifiek de OVERGESLAGEN-regel van check_pairing, niet zomaar "OVERGESLAGEN"
    # ergens in de output -- deze fixture heeft ook geen `.claude/rules/`, dus
    # check_scoping() drukt zijn EIGEN (irrelevante) OVERGESLAGEN-regel af,
    # en een kale substring-check op "OVERGESLAGEN" zou daar per ongeluk op
    # slagen ook als check_pairing zijn eigen regel juist zou onderdrukken.
    assert "SKIPPED  pairing:" in result.stdout


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
    assert "counterpart" in result.stdout


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
    assert "counterpart" in result.stdout


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
    assert "counterpart" in result.stdout


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
    assert "counterpart" in result.stdout


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
    assert "SKIPPED  pairing:" in result.stdout
    assert "python3" in result.stdout
    assert "FAIL" not in result.stdout


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
    assert "valid JSON" in result.stdout
    assert "names no counterpart" not in result.stdout


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
    assert "valid JSON" in result.stdout
    assert "names no counterpart" not in result.stdout


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
    assert "valid JSON" in result.stdout


def test_dit_script_is_groen_op_deze_repo(tmp_path):
    """De enige test die de drempels afstelt. Vuurt de overlapcheck op een echt
    paar, dan is de fix een afgrenzende clausule in die description -- niet een
    hogere drempel."""
    # Check 5 leest $HOME; zonder deze isolatie blokkeert de personal-map van de
    # machine elke commit. Alleen de repo-boom is hier het onderwerp.
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


# --- check 10: omvang, het plafond op CLAUDE.md -----------------------------
# DoD van milestone #42, permanent gemaakt: zonder teller kruipt het
# woordental gewoon weer omhoog, zoals tussen 2026-08-22 en 2026-09-02
# gebeurde (3046 -> 5053). `[project] instructions_word_ceiling` laat de test een
# eigen drempel zetten in plaats van tegen de default 3000 te meten.


def test_claude_md_boven_het_plafond_faalt(tmp_path):
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "CLAUDE.md").write_text("woord " * 50, encoding="utf-8")
    result = _run(tmp_path, toml=_plafond(10))
    assert result.returncode == 1, result.stdout
    assert "FAIL  size:" in result.stdout
    assert "ceiling" in result.stdout


def test_claude_md_op_het_plafond_is_groen(tmp_path):
    """De grens zelf, niet alleen de kanten -- op 10 is 10 nog geen `> 10`."""
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "CLAUDE.md").write_text("woord " * 10, encoding="utf-8")
    result = _run(tmp_path, toml=_plafond(10))
    assert result.returncode == 0, result.stdout + result.stderr
    assert "ok    CLAUDE.md counts 10 words, within the ceiling of 10" in result.stdout


def test_claude_md_een_boven_het_plafond_faalt(tmp_path):
    """De andere kant van de grens: 11 op een plafond van 10 is al FOUT."""
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "CLAUDE.md").write_text("woord " * 11, encoding="utf-8")
    result = _run(tmp_path, toml=_plafond(10))
    assert result.returncode == 1, result.stdout
    assert "FAIL  size: CLAUDE.md counts 11 words, ceiling is 10" in result.stdout


def test_claude_md_onder_het_plafond_is_groen(tmp_path):
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "CLAUDE.md").write_text("woord " * 5, encoding="utf-8")
    result = _run(tmp_path, toml=_plafond(10))
    assert result.returncode == 0, result.stdout + result.stderr
    assert "ok    CLAUDE.md counts 5 words, within the ceiling of 10" in result.stdout


def test_geen_claude_md_wordt_overgeslagen(tmp_path):
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "SKIPPED  size:" in result.stdout


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
    assert "SKIPPED  size:" in result.stdout
    assert "not readable" in result.stdout
    assert "ok    CLAUDE.md counts" not in result.stdout


def test_agents_md_telt_mee_naast_claude_md(tmp_path):
    """CLAUDE.md is een `@AGENTS.md`-import plus de Claude-rest: het plafond
    geldt voor wat Claude Code werkelijk laadt, dus voor beide samen."""
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "CLAUDE.md").write_text("woord " * 4, encoding="utf-8")
    (repo / "AGENTS.md").write_text("woord " * 6, encoding="utf-8")
    result = _run(tmp_path, toml=_plafond(10))
    assert result.returncode == 0, result.stdout + result.stderr
    assert (
        "ok    CLAUDE.md + AGENTS.md counts 10 words, within the ceiling of 10"
        in result.stdout
    )


def test_agents_md_duwt_de_som_over_het_plafond(tmp_path):
    """Bewezen placebo zonder deze test: een dunne CLAUDE.md blijft altijd
    onder het plafond, hoe groot het geïmporteerde AGENTS.md ook wordt."""
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "CLAUDE.md").write_text("@AGENTS.md\n", encoding="utf-8")
    (repo / "AGENTS.md").write_text("woord " * 10, encoding="utf-8")
    result = _run(tmp_path, toml=_plafond(10))
    assert result.returncode == 1, result.stdout
    assert (
        "FAIL  size: CLAUDE.md + AGENTS.md counts 11 words, ceiling is 10"
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
    assert "SKIPPED  size:" in result.stdout
    assert "not readable" in result.stdout
    assert "counts" not in result.stdout.split("size")[-1]


def test_plafond_default_is_3000_zonder_override(tmp_path):
    """Zonder `[project] instructions_word_ceiling` geldt de default van 3000."""
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "CLAUDE.md").write_text("woord " * 5, encoding="utf-8")
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout + result.stderr
    assert (
        "ok    CLAUDE.md counts 5 words, within the ceiling of 3000" in result.stdout
    )




# --- lighthouse: root, config, marker, hooksubmappen -------------------------


def test_checks_1_2_without_skills_dir_say_skipped(tmp_path):
    """Zonder `.claude/skills` is er niets gemeten; een `ok` zou dat verzwijgen."""
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
    """`lighthouse init` legt zijn hooks in `.claude/hooks/lighthouse/`."""
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
    for wortel in (claude, agents):
        _skill(
            wortel,
            "paia-streng",
            metadata=STRENG,
            body="De afdwinging zit in `.claude/hooks/lighthouse/streng.sh`.\n",
        )
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout
    assert "every binding: hard has a registered hook" in result.stdout


MARKER = {"lighthouse": '"core@0.1.0"'}


def test_check_11_skips_marked_skills(tmp_path):
    """Een pakketskill verschilt per harnas en is van lighthouse, niet van het
    project: check 11 slaat de hele map over als zijn `SKILL.md` de marker draagt."""
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
    """`[paths] mirror_exceptions` is optioneel (rc 1): dan is elk verschil een
    bevinding, en de hint zegt waar de uitzonderingen vastgelegd worden."""
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
    result = _run(tmp_path, toml=SPIEGEL_TOML)
    assert result.returncode == 1, result.stdout
    assert (
        ".lighthouse/scripts/check-instructions.sh --print-mirror-exceptions"
        " > mirror-exceptions.txt" in result.stdout
    )


def test_check_10_ceiling_from_config(tmp_path):
    repo = tmp_path / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "CLAUDE.md").write_text("woord " * 8, encoding="utf-8")
    result = _run(tmp_path, toml=_plafond(7))
    assert result.returncode == 1, result.stdout
    assert "FAIL  size: CLAUDE.md counts 8 words, ceiling is 7" in result.stdout


@pytest.mark.parametrize(
    "stopwoorden",
    [
        "gebruik wanneer welke worden binnen altijd",
        "about after before which where their",
    ],
    ids=["nl", "en"],
)
def test_check_4_stopwords_both_languages(tmp_path, stopwoorden):
    """Zes gedeelde woorden, alle zes stopwoorden: geen overlap, in beide talen."""
    claude, agents, _ = _tree(tmp_path)
    for naam, eigen in (("paia-een", "appels"), ("paia-twee", "peren")):
        for wortel in (claude, agents):
            _skill(wortel, naam, description=f"{stopwoorden} {eigen}.")
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
    """Zonder python3 is `lighthouse.toml` niet te lezen: checks 10 en 11 meten
    dan niets, en zeggen dat."""
    claude, agents, _ = _tree(tmp_path)
    _skill(claude, "paia-gelijk")
    _skill(agents, "paia-gelijk")
    (tmp_path / "repo" / "CLAUDE.md").write_text("woord " * 5, encoding="utf-8")
    result = _run(tmp_path, extra_env={"PATH": _path_zonder_python3(tmp_path)})
    assert result.returncode == 0, result.stdout + result.stderr
    assert "SKIPPED  size: python3" in result.stdout
    assert "SKIPPED  mirror-content: python3" in result.stdout


def test_final_line_counts_findings(tmp_path):
    claude, _agents, _ = _tree(tmp_path)
    _skill(claude, "paia-alleen-links")
    result = _run(tmp_path)
    assert result.returncode == 1
    assert result.stdout.rstrip().endswith("finding(s).")
