# --- shellcheck-gate (#650) ---------------------------------------------------
#
# De hook is met opzet NIET fail-open, anders dan de ruff- en ty-hooks: stil
# overslaan is hier de fout die de gate moet voorkomen.

SHELLCHECK_HOOK = HOOKS / "shellcheck.sh"
REPO = Path(__file__).resolve().parents[2]
SHELLCHECK_LIB = REPO / "scripts" / "hooks" / "lib" / "shellcheck-bin.sh"
WORKFLOW_AUDIT = REPO / "scripts" / "hooks" / "workflow-audit.sh"
GATE_ASSERT = REPO / "tests" / "integration" / "lib" / "gate-assert.sh"

CLEAN_SH = '#!/usr/bin/env bash\nset -euo pipefail\necho "hallo"\n'
# SC2034: `x` wordt nooit gebruikt. Warning-ernst, dus blokkeert overal.
BROKEN_SH = '#!/usr/bin/env bash\nf() {\n  local x=1\n  echo "hoi"\n}\nf\n'
# SC2001: alleen style-ernst, dus blokkeert alleen in de gate-mappen.
STYLE_ONLY_SH = '#!/usr/bin/env bash\nv="a b"\necho "$v" | sed \'s/a/c/\'\n'


def _run_shellcheck_hook(target: Path, **env_extra) -> subprocess.CompletedProcess:
    env = dict(os.environ, CLAUDE_PROJECT_DIR=str(REPO), **env_extra)
    return subprocess.run(
        [str(SHELLCHECK_HOOK)],
        input=json.dumps({"tool_input": {"file_path": str(target)}}),
        capture_output=True,
        text=True,
        env=env,
        check=False,
    )


def test_shellcheck_hook_passes_clean_script(tmp_path):
    target = tmp_path / "clean.sh"
    target.write_text(CLEAN_SH)
    result = _run_shellcheck_hook(target)
    assert result.returncode == 0, result.stderr


def test_shellcheck_hook_blocks_a_deliberately_broken_script(tmp_path):
    """Het bewijs dat de gate bijt: rood om de juiste reden, met de SC-code erbij."""
    target = tmp_path / "broken.sh"
    target.write_text(BROKEN_SH)
    result = _run_shellcheck_hook(target)
    assert result.returncode == 2, result.stdout
    assert "SC2034" in result.stderr


def test_shellcheck_hook_ignores_non_shell(tmp_path):
    target = tmp_path / "notes.md"
    target.write_text("# hoi\n")
    assert _run_shellcheck_hook(target).returncode == 0


def test_shellcheck_hook_lets_style_pass_outside_the_gate_dirs(tmp_path):
    """De blokkerende set is niet de gelinte set: buiten de gates telt warning."""
    target = tmp_path / "styleonly.sh"
    target.write_text(STYLE_ONLY_SH)
    assert _run_shellcheck_hook(target).returncode == 0


def test_shellcheck_hook_blocks_style_inside_the_gate_dirs(tmp_path):
    """In gate-shell is de lat hoger: daar staat de set op nul tot en met style."""
    target = tmp_path / "repo" / ".claude" / "hooks" / "styleonly.sh"
    target.parent.mkdir(parents=True)
    target.write_text(STYLE_ONLY_SH)
    result = _run_shellcheck_hook(target)
    assert result.returncode == 2, result.stdout
    assert "SC2001" in result.stderr


def test_shellcheck_hook_fails_loud_when_the_tool_is_missing(tmp_path):
    """Het hele punt van #650: geen tool is geen groen.

    De ruff-hook is hier bewust fail-open; deze niet. Een exit 0 zou "schoon"
    melden over een bestand waar niets naar gekeken heeft.
    """
    target = tmp_path / "clean.sh"
    target.write_text(CLEAN_SH)
    result = _run_shellcheck_hook(target, SHELLCHECK_BIN="/nergens/shellcheck")
    assert result.returncode == 2, result.stdout
    assert "NIET gecontroleerd" in result.stderr


def test_shellcheck_pin_matches_workflow_audit():
    """De pin staat op twee plekken; lopen ze uiteen, dan meet de hook iets
    anders dan de CI-lane."""
    lib = re.search(r"^SHELLCHECK_PIN=(\S+)", SHELLCHECK_LIB.read_text(), re.MULTILINE)
    audit = re.search(
        r"^SHELLCHECK_VERSION=(\S+)", WORKFLOW_AUDIT.read_text(), re.MULTILINE
    )
    assert lib and audit, (lib, audit)
    assert lib.group(1) == audit.group(1)


def _shellcheck_pin() -> str:
    match = re.search(
        r"^SHELLCHECK_PIN=(\S+)", SHELLCHECK_LIB.read_text(), re.MULTILINE
    )
    assert match, "SHELLCHECK_PIN niet gevonden in de lib"
    return match.group(1)


def _fake_shellcheck(path: Path, version: str) -> None:
    path.write_text(
        "#!/usr/bin/env bash\n"
        'if [ "${1:-}" = "--version" ]; then\n'
        '  echo "ShellCheck - shell script analysis tool"\n'
        f'  echo "version: {version}"\n'
        "  exit 0\n"
        "fi\n"
        "exit 0\n"
    )
    path.chmod(0o755)


def _resolve_shellcheck(repo: Path, stub_dir: Path) -> subprocess.CompletedProcess:
    """Draai resolve_shellcheck in `repo`, met `stub_dir` vooraan PATH."""
    env = dict(os.environ, PATH=f"{stub_dir}{os.pathsep}{os.environ['PATH']}")
    env.pop("SHELLCHECK_BIN", None)
    return subprocess.run(
        ["bash", "-c", f'. "{SHELLCHECK_LIB}"\nresolve_shellcheck\n'],
        capture_output=True,
        text=True,
        cwd=str(repo),
        env=env,
        check=False,
    )


def _fresh_repo(tmp_path: Path) -> Path:
    repo = tmp_path / "repo"
    repo.mkdir()
    subprocess.run(["git", "init", "-q"], cwd=str(repo), check=True)
    return repo


def test_resolve_prefers_the_pinned_cache_over_a_divergent_path_version(tmp_path):
    """De gate-set staat op nul bevindingen ONDER DE PIN. ubuntu-latest heeft
    shellcheck 0.9.0 voorgeïnstalleerd, en die meldt SC2317 op precies de
    trap-cleanup die 0.11.0 via `disable=SC2329` onderdrukt. Een afwijkende
    PATH-versie mag de pin dus nooit verdringen: dezelfde boom gaf anders rood
    op de ene runner en groen op de andere.
    """
    repo = _fresh_repo(tmp_path)
    cache = repo / ".git" / "workflow-audit-tools"
    cache.mkdir(parents=True)
    _fake_shellcheck(cache / "shellcheck", _shellcheck_pin())
    stub_dir = tmp_path / "bin"
    stub_dir.mkdir()
    _fake_shellcheck(stub_dir / "shellcheck", "0.9.0")

    result = _resolve_shellcheck(repo, stub_dir)
    assert result.returncode == 0, result.stderr
    assert result.stdout.strip() == str(cache / "shellcheck"), result.stdout


def test_resolve_uses_a_pinned_path_version_without_downloading(tmp_path):
    """Staat de pin zélf op PATH, dan is dat het antwoord — geen download."""
    repo = _fresh_repo(tmp_path)
    stub_dir = tmp_path / "bin"
    stub_dir.mkdir()
    _fake_shellcheck(stub_dir / "shellcheck", _shellcheck_pin())

    result = _resolve_shellcheck(repo, stub_dir)
    assert result.returncode == 0, result.stderr
    assert result.stdout.strip() == str(stub_dir / "shellcheck"), result.stdout
    assert result.stderr == "", result.stderr


def test_resolve_falls_back_to_a_divergent_path_version_when_the_pin_is_unreachable(
    tmp_path,
):
    """Offline met alleen een oude shellcheck: kijken met de verkeerde versie is
    beter dan niet kijken — maar dan mét melding, want de uitkomst is niet die
    van de gate. `curl` is hier een stub die faalt: unit-tests gaan niet het net op.
    """
    repo = _fresh_repo(tmp_path)
    stub_dir = tmp_path / "bin"
    stub_dir.mkdir()
    _fake_shellcheck(stub_dir / "shellcheck", "0.9.0")
    curl = stub_dir / "curl"
    curl.write_text("#!/usr/bin/env bash\nexit 1\n")
    curl.chmod(0o755)

    result = _resolve_shellcheck(repo, stub_dir)
    assert result.returncode == 0, result.stderr
    assert result.stdout.strip() == str(stub_dir / "shellcheck"), result.stdout
    assert "wijkt af van de pin" in result.stderr, result.stderr


def test_shellcheck_gate_set_is_clean():
    """KPI-rij 1 van de skill paia-shell-gates, als test."""
    result = subprocess.run(
        ["bash", str(REPO / "scripts" / "hooks" / "shellcheck-repo.sh")],
        capture_output=True,
        text=True,
        cwd=str(REPO),
        check=False,
    )
    assert result.returncode == 0, result.stdout + result.stderr


def _run_gate_assert(rc: int, want: str) -> subprocess.CompletedProcess:
    script = f'. "{GATE_ASSERT}"\ncheck "proef" {want} {rc}\nexit "$failures"\n'
    return subprocess.run(
        ["bash", "-c", script], capture_output=True, text=True, check=False
    )


def test_gate_assert_rejects_command_not_found_as_a_finding():
    """Defect 1 van #610: `nonzero` slikte rc=127, dus een ontbrekend script
    telde als een geslaagde blokkade."""
    for rc in (126, 127):
        result = _run_gate_assert(rc, "nonzero")
        assert result.returncode != 0, result.stdout
        assert "FAIL" in result.stdout


def test_gate_assert_still_accepts_a_real_nonzero():
    result = _run_gate_assert(1, "nonzero")
    assert result.returncode == 0, result.stdout
    assert "PASS" in result.stdout


def test_every_hook_script_is_registered_in_settings():
    """De pairing-regel op zijn smalst: een hook die nergens geregistreerd staat,
    draait niet en meldt dus niets — precies de stille nul uit #650.

    `statusline.sh` (#669) is uitgezonderd: het is geen PreToolUse/PostToolUse/Stop-
    hook maar een statusLine-renderer, en die wordt niet onder `hooks` in dít
    repo-bestand geregistreerd maar als `statusLine` in het gebruikersbestand
    `~/.claude/settings.json` (Task 8 van
    docs/developer/planning/plans/2026-08-23-usage-window-fase1.md). Deze test kan
    dat gebruikersbestand niet lezen, dus zou voor `statusline.sh` per ontwerp nooit
    kunnen slagen.
    """
    settings = json.loads((REPO / ".claude" / "settings.json").read_text())
    registered = json.dumps(settings["hooks"])
    exempt = {"statusline.sh"}
    for script in sorted(HOOKS.glob("*.sh")):
        if script.name in exempt:
            continue
        assert script.name in registered, f"{script.name} staat in geen enkele hook"


def test_no_gate_shell_is_gitignored():
    """Een genegeerd bestand valt buiten elke `git ls-files`-gate en wordt nooit
    gecommit — de gate meldt dan groen over een set waar het niet in zat.
    Gevonden op scripts/hooks/lib/ en tests/integration/lib/: het onaangelijnde
    `lib/` uit de standaard-Python-.gitignore matchte elke map met die naam.
    """
    targets = [
        str(p.relative_to(REPO))
        for d in (".claude/hooks", "scripts/hooks", "tests/integration")
        for p in (REPO / d).rglob("*.sh")
    ]
    assert targets
    result = subprocess.run(
        ["git", "check-ignore", *targets],
        capture_output=True,
        text=True,
        cwd=str(REPO),
        check=False,
    )
    assert result.returncode == 1, f"genegeerd: {result.stdout}"


def test_shellcheck_hook_reads_the_path_without_jq(tmp_path):
    """De hook draait op ELKE Edit/Write. Zonder jq mag hij niet de hele sessie
    blokkeren op een .py- of .md-edit; python3 is de tweede parser.

    jq weghalen kan niet door zijn map uit PATH te knippen — daar staat ook bash.
    Daarom een PATH die alleen uit symlinks bestaat, met alles erin behalve jq.
    """
    target = tmp_path / "broken.sh"
    target.write_text(BROKEN_SH)
    shim = tmp_path / "bin"
    shim.mkdir()
    needed = [
        "env",
        "bash",
        "python3",
        "git",
        "dirname",
        "sed",
        "grep",
        "cut",
        "uname",
        "mktemp",
        "sha256sum",
        "curl",
        # mkdir en xz horen bij het downloadpad van resolve_shellcheck: op een
        # koude toolcache pakt die de gepinde release uit met `tar -xJf`, en tar
        # roept daarvoor xz aan. Zonder deze twee faalt de hook in deze shim met
        # "mkdir: command not found" in plaats van met de SC2034 waar de test op
        # kijkt.
        "mkdir",
        "xz",
        "tar",
        "rm",
        "chmod",
        "cat",
    ]
    for name in needed:
        found = shutil.which(name)
        assert found, name
        (shim / name).symlink_to(found)
    assert shutil.which("jq", path=str(shim)) is None

    result = _run_shellcheck_hook(target, PATH=str(shim))
    assert result.returncode == 2, result.stdout + result.stderr
    assert "SC2034" in result.stderr
