"""The quality ratchet: baseline validation, the comparator and the diff-coverage
arithmetic.

From the source repository's `test_quality_ratchet.py:1-1198`; the nightly cases are in
`test_ratchet_nightly.py`. Left out there: `test_the_selected_rules_match_pyproject`,
`test_the_repo_baseline_loads_and_matches_the_rules` and
`test_the_baseline_has_no_zero_rules` (they read the source repository's own
`pyproject.toml` and `quality-baseline.toml`). The tests that read its tree use a
fixture repository (`ROOT`) here.
"""

import atexit
import json
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

import pytest

# The scripts of pack/python are no installed package; `quality` imports from here.
_SCRIPTS = Path(__file__).resolve().parents[3] / "pack" / "python" / "scripts"
if str(_SCRIPTS) not in sys.path:
    sys.path.insert(0, str(_SCRIPTS))

from quality import (
    baseline as bl,
)

# A fixture repository with the files the source repository's tests read from its own
# tree: the two pins and the directory the exclude in GOOD points at.
ROOT = Path(tempfile.mkdtemp(prefix="ratchet-root-"))
atexit.register(shutil.rmtree, ROOT, True)
(ROOT / "src" / "generated").mkdir(parents=True)
(ROOT / ".ruff-version").write_text("0.16.4\n", encoding="utf-8")
(ROOT / ".complexipy-version").write_text("8.0.1\n", encoding="utf-8")

GOOD = """\
[complexipy]
cognitive_over_15 = 149

[ruff]
C901   = 64
TRY400 = 54
F401   = 19
F841   = 10
S110   = 10
PT011  = 3
S112   = 0

[coverage]
min_diff_pct = 70
min_total_pct = 64
exclude = ["src/generated/"]
"""


def write(tmp_path: Path, text: str) -> Path:
    target = tmp_path / "quality-baseline.toml"
    target.write_text(text, encoding="utf-8")
    return target


def test_a_valid_baseline_loads(tmp_path):
    result = bl.load(write(tmp_path, GOOD), ROOT)
    assert result.counts["C901"] == 64
    assert result.counts["S112"] == 0
    assert result.min_diff_pct == 70
    assert result.exclude == ("src/generated/",)
    assert result.cognitive_over_15 == 149


def test_a_baseline_without_the_complexipy_section_is_an_error(tmp_path):
    """Een ontbrekende sectie is een uitgeschakelde teller: fail closed."""
    text = GOOD.replace("[complexipy]\ncognitive_over_15 = 149\n\n", "")
    assert "complexipy" not in text
    with pytest.raises(bl.BaselineError, match="complexipy"):
        bl.load(write(tmp_path, text), ROOT)


@pytest.mark.parametrize(
    "replacement",
    [
        "cognitive_over_25 = 1",
        "cognitive_over_15 = true",
        "cognitive_over_15 = -1",
    ],
)
def test_the_complexipy_counter_rejects_unknown_keys_bools_and_negatives(
    tmp_path, replacement
):
    text = GOOD.replace("cognitive_over_15 = 149", replacement)
    with pytest.raises(bl.BaselineError):
        bl.load(write(tmp_path, text), ROOT)


def test_a_missing_file_is_an_error(tmp_path):
    with pytest.raises(bl.BaselineError, match="is missing"):
        bl.load(tmp_path / "absent.toml", ROOT)


def test_broken_toml_is_an_error(tmp_path):
    with pytest.raises(bl.BaselineError, match="is unreadable"):
        bl.load(write(tmp_path, "[ruff\nC901 = 1\n"), ROOT)


def test_a_missing_counter_rule_is_an_error(tmp_path):
    text = GOOD.replace("S112   = 0\n", "")
    with pytest.raises(bl.BaselineError, match="S112"):
        bl.load(write(tmp_path, text), ROOT)


def test_a_typo_in_a_rule_name_is_an_error(tmp_path):
    """`C902 = 64` zou de teller voor C901 op nul zetten en die regel uitschakelen."""
    text = GOOD.replace("C901   = 64", "C902   = 64")
    with pytest.raises(bl.BaselineError) as exc:
        bl.load(write(tmp_path, text), ROOT)
    assert "C902" in str(exc.value) and "C901" in str(exc.value)


def test_a_zero_rule_may_not_be_baselined(tmp_path):
    text = GOOD.replace("[coverage]", "E722   = 3\n\n[coverage]")
    with pytest.raises(bl.BaselineError, match="E722"):
        bl.load(write(tmp_path, text), ROOT)


def test_a_float_count_is_an_error(tmp_path):
    text = GOOD.replace("C901   = 64", "C901   = 64.0")
    with pytest.raises(bl.BaselineError, match="C901"):
        bl.load(write(tmp_path, text), ROOT)


def test_a_negative_count_is_an_error(tmp_path):
    text = GOOD.replace("C901   = 64", "C901   = -1")
    with pytest.raises(bl.BaselineError, match="negative"):
        bl.load(write(tmp_path, text), ROOT)


def test_a_boolean_count_is_an_error(tmp_path):
    """bool erft van int: `isinstance(True, int)` is True, dus dit moet op type()."""
    text = GOOD.replace("C901   = 64", "C901   = true")
    with pytest.raises(bl.BaselineError, match="C901"):
        bl.load(write(tmp_path, text), ROOT)


def test_a_boolean_threshold_is_an_error(tmp_path):
    text = GOOD.replace("min_diff_pct = 70", "min_diff_pct = true")
    with pytest.raises(bl.BaselineError, match="min_diff_pct"):
        bl.load(write(tmp_path, text), ROOT)


def test_a_threshold_out_of_range_is_an_error(tmp_path):
    text = GOOD.replace("min_diff_pct = 70", "min_diff_pct = 101")
    with pytest.raises(bl.BaselineError, match=r"\[0, 100\]"):
        bl.load(write(tmp_path, text), ROOT)


def test_an_exclude_pointing_nowhere_is_an_error(tmp_path):
    """Een uitsluiting die naar niets wijst doet stil niets — dat is de gevaarlijke vorm."""
    text = GOOD.replace('"src/generated/"', '"src/does_not_exist/"')
    with pytest.raises(bl.BaselineError, match="does_not_exist"):
        bl.load(write(tmp_path, text), ROOT)


def test_an_unknown_section_is_an_error(tmp_path):
    with pytest.raises(bl.BaselineError, match="sonar"):
        bl.load(write(tmp_path, GOOD + "\n[sonar]\nfoo = 1\n"), ROOT)


def test_an_unknown_coverage_key_is_an_error(tmp_path):
    text = GOOD + "max_diff_pct = 90\n"
    with pytest.raises(bl.BaselineError, match="max_diff_pct"):
        bl.load(write(tmp_path, text), ROOT)


from quality import ratchet


def test_all_rules_is_the_eight_plus_the_seven():
    assert set(ratchet.ALL_RULES) == set(bl.ZERO_RULES) | set(bl.COUNTED_RULES)
    assert len(ratchet.ALL_RULES) == 15


def test_the_ruff_pin_is_read_from_one_file():
    pin = ratchet.ruff_pin(ROOT)
    assert pin == (ROOT / ".ruff-version").read_text(encoding="utf-8").strip()
    assert pin.count(".") == 2


def test_a_garbled_pin_is_an_error(tmp_path):
    (tmp_path / ".ruff-version").write_text("latest\n", encoding="utf-8")
    with pytest.raises(SystemExit, match="holds no version"):
        ratchet.ruff_pin(tmp_path)


# --- complexipy (#1192): de tweede meetbron ---------------------------------------


def test_the_complexipy_pin_has_exactly_one_source():
    """De complexipy-versie staat in .complexipy-version en nergens anders.

    Dezelfde vorm als de ruff-pin: twee plekken is drift, en drift in een
    teller is een rode CI op een PR die niets veranderde.
    """
    pin = (ROOT / ".complexipy-version").read_text(encoding="utf-8").strip()
    assert re.fullmatch(r"\d+\.\d+\.\d+", pin), pin
    assert ratchet.complexipy_pin(ROOT) == pin

    source = (_SCRIPTS / "quality" / "ratchet.py").read_text(encoding="utf-8")
    assert ".complexipy-version" in source
    assert not re.search(r"complexipy@\d", source)

    for path in (_SCRIPTS.parent).rglob("*"):
        if path.suffix not in {".py", ".sh"} or "__pycache__" in path.parts:
            continue
        assert not re.search(r"complexipy@\d", path.read_text(encoding="utf-8")), (
            f"{path} hardcodes the complexipy version next to .complexipy-version"
        )


def test_a_garbled_complexipy_pin_is_an_error(tmp_path):
    (tmp_path / ".complexipy-version").write_text("latest\n", encoding="utf-8")
    with pytest.raises(SystemExit, match="holds no version"):
        ratchet.complexipy_pin(tmp_path)


class _FakeComplexipy:
    """Een nep-`subprocess.run` die complexipy speelt: legt de aanroep vast en
    schrijft de JSON naar het pad na `--output`, zoals de echte tool."""

    def __init__(self, entries=None, *, returncode=0, stdout="", stderr="", write=True):
        self.entries = [] if entries is None else entries
        self.returncode = returncode
        self.stdout = stdout
        self.stderr = stderr
        self.write = write
        self.argv: list[str] = []
        self.cwd: Path | None = None

    def __call__(self, argv, **kwargs):
        self.argv = list(argv)
        self.cwd = kwargs.get("cwd")
        if self.write:
            target = Path(argv[argv.index("--output") + 1])
            target.write_text(json.dumps(self.entries), encoding="utf-8")
        return subprocess.CompletedProcess(
            argv, self.returncode, stdout=self.stdout, stderr=self.stderr
        )


def _entry(path: str, name: str, complexity: int) -> dict:
    return {
        "complexity": complexity,
        "file_name": Path(path).name,
        "function_name": name,
        "path": path,
        "refactor_plans": [],
    }


def _complexipy_root(tmp_path: Path) -> Path:
    (tmp_path / ".complexipy-version").write_text("8.0.1\n", encoding="utf-8")
    return tmp_path


def test_count_complexipy_runs_pinned_without_ignores_in_a_temp_dir(
    tmp_path, monkeypatch
):
    """Review Focus 2 en 3: geen `# noqa`-ontsnapping, en geen rommel in de werkboom."""
    root = _complexipy_root(tmp_path)
    fake = _FakeComplexipy([_entry("src/a.py", "f", 3)])
    monkeypatch.setattr(ratchet.subprocess, "run", fake)

    assert ratchet.count_complexipy(root) == [("src/a.py::f", 3)]

    assert fake.argv[:2] == ["uvx", "complexipy@8.0.1"]
    assert "--no-ignore" in fake.argv
    assert str(root / "src") in fake.argv and str(root / "tests") in fake.argv
    assert "--failed" not in fake.argv
    for flag in ("--output", "--cache-dir"):
        target = Path(fake.argv[fake.argv.index(flag) + 1])
        assert not target.is_relative_to(root), f"{flag} wijst naar de werkboom"


def test_count_complexipy_runs_outside_the_repo_so_its_config_cannot_exclude(
    tmp_path, monkeypatch
):
    """complexipy leest `[tool.complexipy] exclude` uit zijn cwd, en --no-ignore
    stopt dat niet: met de repo als cwd haalt één configregel een bestand stil uit
    de teller én de harde grens. Daarom draait hij elders, met absolute paden, en
    komen de paden relatief aan de repo terug."""
    root = _complexipy_root(tmp_path)
    fake = _FakeComplexipy(
        [_entry(str(root / "src" / "a.py"), "f", 30), _entry("/elders/b.py", "g", 1)]
    )
    monkeypatch.setattr(ratchet.subprocess, "run", fake)

    assert ratchet.count_complexipy(root) == [
        ("src/a.py::f", 30),
        ("/elders/b.py::g", 1),
    ]
    assert fake.cwd is not None
    assert not Path(fake.cwd).is_relative_to(root), "complexipy draait in de repo"
    assert "src/" not in fake.argv and "tests/" not in fake.argv


def test_count_complexipy_keeps_duplicate_names(tmp_path, monkeypatch):
    """Review Focus 5: de teller telt regels uit de JSON, geen unieke namen."""
    entries = [
        _entry("src/a.py", "C::run", 20),
        _entry("src/b.py", "C::run", 21),
        _entry("src/c.py", "C::run", 22),
        _entry("src/c.py", "C::run", 23),
    ]
    monkeypatch.setattr(ratchet.subprocess, "run", _FakeComplexipy(entries))
    result = ratchet.count_complexipy(_complexipy_root(tmp_path))
    assert result == [
        ("src/a.py::C::run", 20),
        ("src/b.py::C::run", 21),
        ("src/c.py::C::run", 22),
        ("src/c.py::C::run", 23),
    ]


def test_count_complexipy_is_red_when_a_file_failed(tmp_path, monkeypatch):
    """Review Focus 1: een bestand dat complexipy niet kon lezen telt niet mee,
    dus groen met minder functies zou een leugen zijn."""
    fake = _FakeComplexipy(
        [_entry("src/a.py", "f", 3)],
        returncode=1,
        stdout="Failed to process /x/src/broken.py\n",
    )
    monkeypatch.setattr(ratchet.subprocess, "run", fake)
    with pytest.raises(SystemExit, match=r"broken\.py"):
        ratchet.count_complexipy(_complexipy_root(tmp_path))


def test_count_complexipy_is_red_without_output(tmp_path, monkeypatch):
    monkeypatch.setattr(ratchet.subprocess, "run", _FakeComplexipy(write=False))
    with pytest.raises(SystemExit, match="complexipy"):
        ratchet.count_complexipy(_complexipy_root(tmp_path))


def test_count_complexipy_is_red_on_a_tool_failure_without_a_failed_file(
    tmp_path, monkeypatch
):
    fake = _FakeComplexipy(returncode=2, stderr="boem")
    monkeypatch.setattr(ratchet.subprocess, "run", fake)
    with pytest.raises(SystemExit, match=r"rc=2"):
        ratchet.count_complexipy(_complexipy_root(tmp_path))


@pytest.mark.parametrize(
    "entry",
    [
        {"function_name": "f", "complexity": 3},
        {"path": "src/a.py", "complexity": 3},
        {"path": "src/a.py", "function_name": "f"},
        {"path": "src/a.py", "function_name": "f", "complexity": "3"},
        {"path": "src/a.py", "function_name": "f", "complexity": True},
    ],
)
def test_count_complexipy_is_red_on_a_malformed_entry(tmp_path, monkeypatch, entry):
    monkeypatch.setattr(ratchet.subprocess, "run", _FakeComplexipy([entry]))
    with pytest.raises(SystemExit, match="complexipy"):
        ratchet.count_complexipy(_complexipy_root(tmp_path))


def _cognitive_baseline(cognitive_over_15: int) -> bl.Baseline:
    return bl.Baseline(
        counts=dict.fromkeys(bl.COUNTED_RULES, 0),
        min_diff_pct=70,
        min_total_pct=0,
        exclude=(),
        cognitive_over_15=cognitive_over_15,
    )


def test_judge_complexity_flags_a_rising_cognitive_counter():
    scores = [("src/a.py::f", 16), ("src/a.py::g", 25), ("src/b.py::h", 18)]
    scores.append(("src/b.py::i", 15))  # precies 15 telt niet mee: strikt groter
    failures, notes = ratchet.judge_complexity(_cognitive_baseline(2), [], scores)
    assert len(failures) == 1
    assert "cognitive_over_15: 2 -> 3" in failures[0]
    assert "src/b.py::i" not in failures[0]
    assert notes == []


def test_judge_complexity_is_green_on_an_equal_counter():
    scores = [("src/a.py::f", 16), ("src/a.py::g", 16)]
    assert ratchet.judge_complexity(_cognitive_baseline(2), [], scores) == ([], [])


def test_judge_complexity_suggests_lowering_on_a_drop():
    scores = [("src/a.py::f", 16), ("src/a.py::g", 16), ("src/a.py::h", 16)]
    failures, notes = ratchet.judge_complexity(_cognitive_baseline(5), [], scores)
    assert failures == []
    assert len(notes) == 1
    assert "cognitive_over_15: 5 -> 3" in notes[0]
    assert "can go down" in notes[0]


def _stub_measurements(monkeypatch, *, cognitive_scores, cyclomatic_scores=()):
    """Zet beide meetbronnen vast; geen subprocess, dus ook geen netwerk."""
    counts = dict.fromkeys(ratchet.ALL_RULES, 0)
    where = {code: [] for code in ratchet.ALL_RULES}
    cyclomatic = list(cyclomatic_scores)
    monkeypatch.setattr(ratchet, "count_ruff", lambda root: (counts, where, cyclomatic))
    monkeypatch.setattr(ratchet, "count_complexipy", lambda root: cognitive_scores)


def _repo_with_baseline(tmp_path: Path, cognitive_over_15: int) -> Path:
    text = GOOD.replace(
        "cognitive_over_15 = 149", f"cognitive_over_15 = {cognitive_over_15}"
    )
    text = text.replace('exclude = ["src/generated/"]\n', "")
    write(tmp_path, text)
    return tmp_path


def test_cmd_ruff_names_both_counters_when_green(tmp_path, monkeypatch, capsys):
    _stub_measurements(monkeypatch, cognitive_scores=[("src/a.py::f", 16)])
    root = _repo_with_baseline(tmp_path, 1)
    assert ratchet.cmd_ruff(root) == 0
    out = capsys.readouterr().out
    assert "complexipy ok (1 functions cognitive > 15)" in out
    assert "ruff ok (" in out


def test_cmd_ruff_is_red_with_its_own_heading_when_the_counter_rises(
    tmp_path, monkeypatch, capsys
):
    _stub_measurements(monkeypatch, cognitive_scores=[("src/a.py::f", 16)])
    root = _repo_with_baseline(tmp_path, 0)
    assert ratchet.cmd_ruff(root) == 1
    err = capsys.readouterr().err
    assert "complexity above the limit or the counter rises." in err
    assert "cognitive_over_15: 0 -> 1" in err
    assert "the ruff counters rise." not in err


def test_cmd_tighten_refuses_when_the_cognitive_counter_rose(
    tmp_path, monkeypatch, capsys
):
    _stub_measurements(monkeypatch, cognitive_scores=[("src/a.py::f", 16)])
    root = _repo_with_baseline(tmp_path, 0)
    before = (root / "quality-baseline.toml").read_text(encoding="utf-8")
    assert ratchet.cmd_tighten(root) == 1
    assert "cognitive_over_15" in capsys.readouterr().err
    assert (root / "quality-baseline.toml").read_text(encoding="utf-8") == before


def test_cmd_show_prints_the_complexipy_counter(tmp_path, monkeypatch, capsys):
    scores = [("src/a.py::f", 16), ("src/a.py::g", 15), ("src/a.py::h", 99)]
    _stub_measurements(monkeypatch, cognitive_scores=scores)
    assert ratchet.cmd_show(tmp_path) == 0
    out = capsys.readouterr().out
    assert "[complexipy]\ncognitive_over_15 = 2\n" in out


def test_judge_complexity_flags_a_function_above_cyclomatic_20():
    over = [("src/a.py:3:1 f", 21)]
    failures, _ = ratchet.judge_complexity(_cognitive_baseline(0), over, [])
    assert len(failures) == 1
    assert "src/a.py:3:1 f" in failures[0]
    assert "21 > 20" in failures[0]
    assert "no bump" in failures[0]
    exact = [("src/a.py:3:1 f", 20)]
    assert ratchet.judge_complexity(_cognitive_baseline(0), exact, []) == ([], [])


def test_judge_complexity_flags_a_function_above_cognitive_25():
    over = [("src/a.py::f", 26)]
    failures, _ = ratchet.judge_complexity(_cognitive_baseline(1), [], over)
    assert len(failures) == 1
    assert "src/a.py::f" in failures[0]
    assert "26 > 25" in failures[0]
    assert "no bump" in failures[0]
    exact = [("src/a.py::f", 25)]
    assert ratchet.judge_complexity(_cognitive_baseline(1), [], exact) == ([], [])


def test_judge_complexity_caps_the_hard_limit_lines_per_axis():
    over = [(f"src/a.py:{n}:1 f{n}", 30) for n in range(ratchet.MAX_LOCATIONS + 3)]
    failures, _ = ratchet.judge_complexity(_cognitive_baseline(0), over, [])
    assert len(failures) == ratchet.MAX_LOCATIONS + 1
    assert failures[-1] == "… and 3 more"


def _fake_ruff(monkeypatch, tmp_path, findings):
    (tmp_path / ".ruff-version").write_text("0.16.4\n", encoding="utf-8")

    class FakeCompletedProcess:
        returncode = 1
        stdout = json.dumps(findings)
        stderr = ""

    monkeypatch.setattr(
        ratchet.subprocess, "run", lambda *args, **kwargs: FakeCompletedProcess()
    )


def _c901(tmp_path, message):
    return {
        "code": "C901",
        "filename": str(tmp_path / "src" / "a.py"),
        "location": {"row": 3, "column": 1},
        "message": message,
    }


def test_count_ruff_reads_the_cyclomatic_score_from_the_message(tmp_path, monkeypatch):
    finding = _c901(tmp_path, "`f` is too complex (22 > 10)")
    _fake_ruff(monkeypatch, tmp_path, [finding])
    counts, where, cyclomatic = ratchet.count_ruff(tmp_path)
    assert counts["C901"] == 1
    assert where["C901"] == ["src/a.py:3:1"]
    assert cyclomatic == [("src/a.py:3:1 f", 22)]


def test_ruff_runs_pinned_with_the_mccabe_threshold_fixed_at_10(tmp_path, monkeypatch):
    """ruff meldt C901 alleen boven max-complexity. Zonder de --config-pin zou
    `max-complexity = 25` in pyproject.toml cyclomatisch 21-25 verbergen voor de
    harde grens."""
    (tmp_path / ".ruff-version").write_text("0.16.4\n", encoding="utf-8")
    seen: dict = {}

    def fake_run(argv, **kwargs):
        seen["argv"] = list(argv)
        return subprocess.CompletedProcess(argv, 0, stdout="[]", stderr="")

    monkeypatch.setattr(ratchet.subprocess, "run", fake_run)
    ratchet.count_ruff(tmp_path)

    argv = seen["argv"]
    assert argv[:3] == ["uvx", "ruff@0.16.4", "check"]
    assert "--ignore-noqa" in argv
    assert argv[argv.index("--select") + 1] == ",".join(ratchet.ALL_RULES)
    assert argv[argv.index("--config") + 1] == "lint.mccabe.max-complexity=10"
    assert argv[-2:] == ["src", "tests"]


def test_count_ruff_is_red_when_a_c901_message_has_no_score(tmp_path, monkeypatch):
    """Review Focus 4: een ruff-bump die het bericht verandert, mag de harde
    grens niet stil uitzetten."""
    _fake_ruff(monkeypatch, tmp_path, [_c901(tmp_path, "`f` is complex")])
    with pytest.raises(SystemExit, match="C901"):
        ratchet.count_ruff(tmp_path)


def test_cmd_ruff_is_red_on_a_hard_limit_hit_with_its_own_heading(
    tmp_path, monkeypatch, capsys
):
    _stub_measurements(
        monkeypatch,
        cognitive_scores=[("src/a.py::f", 26)],
        cyclomatic_scores=[("src/a.py:3:1 f", 21)],
    )
    root = _repo_with_baseline(tmp_path, 1)
    assert ratchet.cmd_ruff(root) == 1
    err = capsys.readouterr().err
    assert "complexity above the limit or the counter rises." in err
    assert "cyclomatic 21 > 20" in err
    assert "cognitive 26 > 25" in err


def test_cmd_ruff_footer_on_only_a_hard_limit_hit_gives_no_bump_advice(
    tmp_path, monkeypatch, capsys
):
    _stub_measurements(monkeypatch, cognitive_scores=[("src/a.py::f", 26)])
    root = _repo_with_baseline(tmp_path, 1)
    assert ratchet.cmd_ruff(root) == 1
    err = capsys.readouterr().err
    assert "cognitive 26 > 25" in err
    assert "adjust quality-baseline.toml" not in err
    assert "on purpose" not in err
    assert "must come down in the code" in err
    assert "## Reaching a complexity limit: deepen, don't split" in err
    assert ".claude/skills/lighthouse-python-writing/SKILL.md" in err


def test_cmd_ruff_footer_on_a_rising_counter_gives_bump_advice(
    tmp_path, monkeypatch, capsys
):
    _stub_measurements(monkeypatch, cognitive_scores=[("src/a.py::f", 16)])
    root = _repo_with_baseline(tmp_path, 0)
    assert ratchet.cmd_ruff(root) == 1
    err = capsys.readouterr().err
    assert "adjust quality-baseline.toml" in err
    assert "on purpose" in err
    assert "must come down in the code" not in err


def test_the_footer_names_both_routes_when_a_counter_and_a_hard_limit_fail():
    footer = ratchet._failure_footer(
        [
            "S110: 0 -> 1 (+1)\n    src/a.py:3:1",
            "cyclomatic 21 > 20: src/a.py:3:1 f — hard limit, no bump possible",
        ]
    )
    assert "on purpose" in footer
    assert "must come down in the code" in footer


def test_the_footer_on_only_a_zero_rule_hit_gives_no_bump_advice():
    footer = ratchet._failure_footer(
        ["E722: 1 hit(s) on a zero rule — no bump possible\n    src/a.py:3:1"]
    )
    assert "on purpose" not in footer
    assert "a zero rule has no bump" in footer


def test_tighten_refuses_while_a_function_is_above_a_hard_limit(
    tmp_path, monkeypatch, capsys
):
    _stub_measurements(monkeypatch, cognitive_scores=[("src/a.py::f", 26)])
    root = _repo_with_baseline(tmp_path, 1)
    before = (root / "quality-baseline.toml").read_bytes()
    assert ratchet.cmd_tighten(root) == 1
    assert "above a hard limit: 1" in capsys.readouterr().err
    assert (root / "quality-baseline.toml").read_bytes() == before


def test_an_invalid_syntax_finding_makes_the_arm_red(tmp_path, monkeypatch):
    """Ruff meldt een onparseerbaar bestand als eigen `code: invalid-syntax`
    finding. Die zit niet in ALL_RULES, dus `if code not in counts: continue`
    gooide 'm weg — en met hem elke échte hit in dat bestand, met een stil
    groene arm tot gevolg. De arm kon dat bestand niet beoordelen en moet
    daarom rood zijn, niet groen."""
    (tmp_path / ".ruff-version").write_text("0.16.4\n", encoding="utf-8")
    findings = [
        {
            "code": "invalid-syntax",
            "filename": str(tmp_path / "src" / "broken.py"),
            "location": {"row": 1, "column": 1},
        }
    ]

    class FakeCompletedProcess:
        returncode = 0
        stdout = json.dumps(findings)
        stderr = ""

    monkeypatch.setattr(
        ratchet.subprocess, "run", lambda *args, **kwargs: FakeCompletedProcess()
    )
    with pytest.raises(SystemExit, match="invalid-syntax"):
        ratchet.count_ruff(tmp_path)


def test_compare_flags_a_rising_counter():
    base = bl.Baseline(
        counts={r: 0 for r in bl.COUNTED_RULES},
        min_diff_pct=70,
        min_total_pct=0,
        exclude=(),
    )
    counts = dict.fromkeys(ratchet.ALL_RULES, 0)
    counts["C901"] = 1
    failures, notes = ratchet.compare(base, counts, {"C901": ["src/a.py:3:1"]})
    assert any("C901" in f and "0 -> 1" in f for f in failures)
    assert notes == []


def test_compare_is_green_on_an_equal_counter():
    base = bl.Baseline(
        counts={r: 5 for r in bl.COUNTED_RULES},
        min_diff_pct=70,
        min_total_pct=0,
        exclude=(),
    )
    counts = dict.fromkeys(ratchet.ALL_RULES, 0) | {r: 5 for r in bl.COUNTED_RULES}
    failures, notes = ratchet.compare(base, counts, {})
    assert failures == [] and notes == []


def test_compare_suggests_lowering_the_baseline_on_a_drop():
    """Zonder deze melding zakt de streep nooit en roest de ratel vast."""
    base = bl.Baseline(
        counts={r: 5 for r in bl.COUNTED_RULES},
        min_diff_pct=70,
        min_total_pct=0,
        exclude=(),
    )
    counts = dict.fromkeys(ratchet.ALL_RULES, 0) | {r: 5 for r in bl.COUNTED_RULES}
    counts["F841"] = 3
    failures, notes = ratchet.compare(base, counts, {})
    assert failures == []
    assert any("F841" in n and "5 -> 3" in n for n in notes)


def test_compare_flags_a_zero_rule_hit_without_a_bump_option():
    base = bl.Baseline(
        counts={r: 0 for r in bl.COUNTED_RULES},
        min_diff_pct=70,
        min_total_pct=0,
        exclude=(),
    )
    counts = dict.fromkeys(ratchet.ALL_RULES, 0)
    counts["E722"] = 2
    failures, _ = ratchet.compare(
        base, counts, {"E722": ["src/a.py:9:5", "src/b.py:2:1"]}
    )
    assert any("E722" in f and "no bump" in f for f in failures)


from quality import diffcov

SOURCE = '''\
"""Fixture."""


def handle(payload):
    result = some_call(
        first_argument,
        second_argument,
    )
    # commentaar

    if payload:
        return result
    return None
'''
# regel 5 = het Assign-statement, 6 en 7 vervolgregels, 9 comment, 10 leeg,
# 11 = de if-kop, 12 = return, 13 = return


def read(_path: str) -> str:
    return SOURCE


def report_for(executed: list[int], missing: list[int], path: str = "src/a.py") -> dict:
    return {"files": {path: {"executed_lines": executed, "missing_lines": missing}}}


def test_added_lines_numbers_multiple_hunks():
    diff = (
        "+++ b/src/a.py\n"
        "@@ -1,0 +5,2 @@\n"
        "+one\n"
        "+two\n"
        "@@ -20,0 +30,1 @@\n"
        "+three\n"
        "+++ b/src/b.py\n"
        "@@ -1,0 +2,1 @@\n"
        "+four\n"
    )
    assert diffcov.added_lines(diff) == {"src/a.py": [5, 6, 30], "src/b.py": [2]}


def test_a_continuation_line_maps_back_onto_its_statement():
    """Zonder deze mapping valt een wijziging binnen een multiline-aanroep stil
    uit de noemer — ongedekte code die de gate passeert."""
    assert diffcov.statement_lines(SOURCE, [7]) == {5}


def test_three_changed_lines_in_one_statement_count_once():
    assert diffcov.statement_lines(SOURCE, [5, 6, 7]) == {5}


def test_a_comment_or_blank_line_maps_to_nothing():
    """Ze horen bij geen statement en kún je niet dekken."""
    assert diffcov.statement_lines(SOURCE, [9, 10]) == set()


MATCH_SOURCE = """\
def handle(command):
    match command:
        case "a":
            return 1
        case "b":
            return 2
        case _:
            return 3
"""
# regel 2 = de match-kop, 3/5/7 = de case-koppen, 4/6/8 = hun bodies


def test_each_case_header_maps_to_its_own_line_not_the_match_line():
    """Zonder match_case in _HEADER_NODES/_BODY_FIELDS valt de hele match/case
    samen tot één span op de `match`-regel: coverage.py rapporteert `case`-
    regels los, en de `match`-regel telt als uitgevoerd zodra één willekeurige
    arm draait — dus een nooit-geraakte `case`-arm zou vals-groen zijn."""
    assert diffcov.statement_lines(MATCH_SOURCE, [3]) == {3}
    assert diffcov.statement_lines(MATCH_SOURCE, [5]) == {5}
    assert diffcov.statement_lines(MATCH_SOURCE, [7]) == {7}


def test_the_match_line_itself_still_maps_to_itself():
    assert diffcov.statement_lines(MATCH_SOURCE, [2]) == {2}


def test_an_uncovered_continuation_line_is_red():
    result = diffcov.diff_coverage(
        {"src/a.py": [7]}, report_for(executed=[11, 12], missing=[5]), read_source=read
    )
    assert result.total == 1
    assert result.uncovered == {"src/a.py": [5]}


def test_a_covered_change_is_green():
    result = diffcov.diff_coverage(
        {"src/a.py": [11, 12]},
        report_for(executed=[11, 12], missing=[5]),
        read_source=read,
    )
    assert result.total == 2
    assert result.uncovered == {}
    assert result.pct == 100.0


def test_a_missing_report_entry_is_an_error_not_all_missed():
    """`scripts/opencode-lane/lane/gates.py` nam vroeger 'dus alles gemist' aan.
    Op een 100%-gate viel dat niet op; op een drempel wel."""
    with pytest.raises(
        diffcov.CoverageReportError, match="no report entry for src/a.py"
    ):
        diffcov.diff_coverage({"src/a.py": [11]}, {"files": {}}, read_source=read)


def test_a_non_python_file_is_ignored():
    """De browserextensie kan per definitie niet in een Python-coverage.json staan."""
    result = diffcov.diff_coverage(
        {"src/browser/extension/background.js": [3]}, {"files": {}}, read_source=read
    )
    assert result.total == 0
    assert result.uncovered == {}


def test_an_excluded_prefix_falls_out_of_both_numerator_and_denominator():
    result = diffcov.diff_coverage(
        {"src/generated/x.py": [11]},
        {"files": {}},
        read_source=read,
        exclude=("src/generated/",),
    )
    assert result.total == 0


def test_a_pragma_no_cover_line_falls_out_of_the_denominator():
    """coverage.py noemt zo'n regel in geen van beide sets; wij dus ook niet."""
    result = diffcov.diff_coverage(
        {"src/a.py": [12]}, report_for(executed=[11], missing=[]), read_source=read
    )
    assert result.total == 0


def test_an_empty_denominator_is_green_without_dividing_by_zero():
    result = diffcov.diff_coverage(
        {"src/a.py": [9]}, report_for([], []), read_source=read
    )
    assert result.total == 0
    assert result.pct == 100.0


def test_an_unparseable_source_is_an_error():
    with pytest.raises(diffcov.CoverageReportError, match="could not be parsed"):
        diffcov.diff_coverage(
            {"src/a.py": [1]},
            report_for([1], []),
            read_source=lambda _p: "def f(:\n",
        )


def test_judge_is_green_above_the_threshold():
    base = bl.Baseline(
        counts=dict.fromkeys(bl.COUNTED_RULES, 0),
        min_diff_pct=70,
        min_total_pct=0,
        exclude=(),
    )
    result = diffcov.DiffCoverage(total=10, uncovered={"src/a.py": [5, 6]})
    failures = ratchet.judge_coverage(base, result)
    assert failures == []


def test_judge_is_red_below_the_threshold_and_names_the_lines():
    """#623 AC 3: de ongedekte regelnummers per bestand, niet alleen het percentage."""
    base = bl.Baseline(
        counts=dict.fromkeys(bl.COUNTED_RULES, 0),
        min_diff_pct=70,
        min_total_pct=0,
        exclude=(),
    )
    result = diffcov.DiffCoverage(total=10, uncovered={"src/a.py": [1, 2, 3, 4]})
    failures = ratchet.judge_coverage(base, result)
    assert failures
    joined = "\n".join(failures)
    assert "60.0%" in joined and "70%" in joined
    assert "src/a.py" in joined and "1, 2, 3, 4" in joined


def test_judge_is_green_on_an_exact_hit():
    """70/100 mag er precies in; een drempel is een ondergrens, geen mesrand."""
    base = bl.Baseline(
        counts=dict.fromkeys(bl.COUNTED_RULES, 0),
        min_diff_pct=70,
        min_total_pct=0,
        exclude=(),
    )
    result = diffcov.DiffCoverage(total=10, uncovered={"src/a.py": [1, 2, 3]})
    assert ratchet.judge_coverage(base, result) == []


def test_judge_reads_the_threshold_from_the_baseline_not_a_hardcoded_70():
    """Alle andere judge_coverage-tests gebruiken min_diff_pct=70 — dezelfde
    waarde als de echte quality-baseline.toml. Een regressie die 70 hardcodeert
    in judge_coverage in plaats van baseline.min_diff_pct te lezen, zou al die
    tests laten slagen. Deze test zet de drempel op 50 en een percentage
    daartussenin (60%), zodat het verdict alleen klopt als het echt de
    baseline volgt."""
    base = bl.Baseline(
        counts=dict.fromkeys(bl.COUNTED_RULES, 0),
        min_diff_pct=50,
        min_total_pct=0,
        exclude=(),
    )
    # 6/10 gedekt = 60%: boven de 50%-drempel van deze baseline, onder de
    # hardcoded 70 die de andere tests niet zouden onderscheiden.
    result = diffcov.DiffCoverage(total=10, uncovered={"src/a.py": [1, 2, 3, 4]})
    assert ratchet.judge_coverage(base, result) == []


def test_judge_is_green_on_an_empty_denominator():
    base = bl.Baseline(
        counts=dict.fromkeys(bl.COUNTED_RULES, 0),
        min_diff_pct=70,
        min_total_pct=0,
        exclude=(),
    )
    assert (
        ratchet.judge_coverage(base, diffcov.DiffCoverage(total=0, uncovered={})) == []
    )


def test_the_suite_env_drops_gits_repo_scoping_variables():
    """`ratchet.py coverage` kan de suite van binnen een git-hook draaien.

    git zet dan GIT_DIR c.s., en git leest die vóór de cwd: met GIT_DIR gezet
    negeert zelfs `git -C /tmp/fixture` die -C. Elke test die een fixture-repo
    bouwt en git aanroept werkt dan op de ECHTE repo. Concreet gebeurd:
    scripts/opencode-lane/lane/cli.py doet `git add -A` + `git commit`, en dat
    verving de branchtip door een fixture-commit die 702 bestanden uit
    .planning/ als verwijderd optekende — twee pushes lang.

    De pre-push-arm die dit veroorzaakte is met #806 verdwenen. De schoonmaak
    blijft: ratchet.py weet niet wie hem aanroept, en een handmatige aanroep met
    GIT_DIR c.s. in de omgeving (vanuit een andere hook, of `git rebase -x`)
    opent exact hetzelfde gat.
    """
    hookish = {
        "PATH": "/usr/bin",
        "HOME": "/home/someone",
        "GIT_DIR": "/repo/.git",
        "GIT_WORK_TREE": "/repo",
        "GIT_INDEX_FILE": "/repo/.git/index",
        "GIT_QUARANTINE_PATH": "/repo/.git/incoming",
        "GIT_PREFIX": "sub/",
    }
    cleaned = ratchet.suite_env(hookish)
    assert cleaned == {"PATH": "/usr/bin", "HOME": "/home/someone"}, (
        "een repo-bindende git-variabele overleeft de schoonmaak; de suite zou "
        "dan in de echte repo committen in plaats van in zijn fixture"
    )


def test_the_coverage_suite_is_spawned_with_the_cleaned_env(monkeypatch, tmp_path):
    """Niet alleen de helper bestaat — de aanroep gebruikt hem ook echt."""
    seen: dict[str, object] = {}

    def fake_run(argv, **kwargs):
        seen["argv"] = argv
        seen["env"] = kwargs.get("env")
        raise SystemExit("stopped after the spawn")

    monkeypatch.setenv("GIT_DIR", "/repo/.git")
    monkeypatch.setattr(ratchet.subprocess, "run", fake_run)
    with pytest.raises(SystemExit):
        ratchet._acquire_report(tmp_path, None)

    assert seen["argv"] == ratchet.coverage_argv()
    assert seen["env"] is not None, "de suite erft de hook-omgeving ongefilterd"
    assert "GIT_DIR" not in seen["env"]


# --- de neerwaartse ratel (#703) ---------------------------------------------

BASELINE_WITH_COMMENTS = """\
[ruff]
C901   = 64   # omvangs-as, niet gesampled
TRY400 = 54   # ~12% FP (n=8)
F401   = 19   # 0% FP (n=9) — 11 hiervan staan achter een # noqa
F841   = 10   # ~27% FP (n=11)
S110   = 10   # ~45% FP (n=10)
PT011  = 3    # ~0% FP (n=3)
S112   = 0    # gemeten 0

[complexipy]
cognitive_over_15 = 149   # gemeten stand, #1192

[coverage]
min_diff_pct = 70
"""


def _counts(**overrides):
    base = {
        "C901": 64,
        "TRY400": 54,
        "F401": 19,
        "F841": 10,
        "S110": 10,
        "PT011": 3,
        "S112": 0,
    }
    return base | overrides


def test_lowering_rewrites_only_the_dropped_rule():
    text, changes = ratchet.rewrite_baseline(BASELINE_WITH_COMMENTS, _counts(F401=14))
    assert changes == ["F401: 19 -> 14"]
    assert "F401   = 14" in text
    assert "C901   = 64" in text, "een ongewijzigde teller mag niet worden aangeraakt"


def test_lowering_keeps_the_trailing_comment():
    """De FP-schattingen zijn het enige geheugen van de steekproef; die mogen niet weg."""
    text, _ = ratchet.rewrite_baseline(BASELINE_WITH_COMMENTS, _counts(F841=6))
    assert "F841   = 6    # ~27% FP (n=11)" in text or "# ~27% FP (n=11)" in text
    assert text.count("# ~27% FP (n=11)") == 1


def test_lowering_never_raises_a_counter():
    """Een stijging is het werk van de gate, niet van de verlager.

    Zou dit getal wél omhoog gaan, dan schrijft de automatisering stilletjes een
    bump die niemand heeft besloten — precies de bypass die er niet mag zijn.
    """
    text, changes = ratchet.rewrite_baseline(BASELINE_WITH_COMMENTS, _counts(S110=99))
    assert changes == []
    assert "S110   = 10" in text
    assert text == BASELINE_WITH_COMMENTS


def test_nothing_to_lower_returns_the_text_unchanged():
    text, changes = ratchet.rewrite_baseline(BASELINE_WITH_COMMENTS, _counts())
    assert changes == []
    assert text == BASELINE_WITH_COMMENTS


def test_lowering_several_rules_at_once_reports_each():
    _, changes = ratchet.rewrite_baseline(
        BASELINE_WITH_COMMENTS, _counts(C901=60, S112=0, PT011=1)
    )
    assert changes == ["C901: 64 -> 60", "PT011: 3 -> 1"]


def test_lowering_rewrites_the_cognitive_counter_and_keeps_its_comment():
    text, changes = ratchet.rewrite_baseline(
        BASELINE_WITH_COMMENTS, _counts(), cognitive_over_15=140
    )
    assert changes == ["cognitive_over_15: 149 -> 140"]
    assert "cognitive_over_15 = 140   # gemeten stand, #1192" in text
    assert "C901   = 64" in text


def test_lowering_never_raises_the_cognitive_counter():
    text, changes = ratchet.rewrite_baseline(
        BASELINE_WITH_COMMENTS, _counts(), cognitive_over_15=150
    )
    assert changes == []
    assert text == BASELINE_WITH_COMMENTS


def test_lowering_the_cognitive_counter_keeps_the_column_of_its_comment():
    text, _ = ratchet.rewrite_baseline(
        BASELINE_WITH_COMMENTS, _counts(), cognitive_over_15=7
    )
    assert "cognitive_over_15 = 7     # gemeten stand, #1192" in text


def test_lowering_leaves_the_coverage_section_alone():
    """min_diff_pct is beleid, geen meting — de verlager blijft eraf (#703)."""
    text, _ = ratchet.rewrite_baseline(BASELINE_WITH_COMMENTS, _counts(C901=1))
    assert "min_diff_pct = 70" in text


def test_a_rewritten_baseline_still_loads():
    """De uitvoer moet door de eigen validatie komen, anders is de PR onbruikbaar."""
    text, _ = ratchet.rewrite_baseline(GOOD, dict.fromkeys(bl.COUNTED_RULES, 0))
    assert bl.load(write(Path(tempfile.mkdtemp()), text), ROOT).counts["C901"] == 0


# --- de globale dekkingsvloer (#703) -----------------------------------------

BASELINE_WITH_TOTAL = BASELINE_WITH_COMMENTS.replace(
    "min_diff_pct = 70\n", "min_diff_pct = 70\nmin_total_pct = 64\n"
)


def _bl(min_total_pct=64, min_diff_pct=70):
    return bl.Baseline(
        counts=dict.fromkeys(bl.COUNTED_RULES, 0),
        min_diff_pct=min_diff_pct,
        min_total_pct=min_total_pct,
        exclude=(),
    )


def test_the_total_floor_is_required_in_the_file():
    """Ontbreekt de sleutel, dan is er geen vloer — dat moet rood zijn, niet 0."""
    text = GOOD.replace("min_total_pct = 64\n", "")
    with pytest.raises(bl.BaselineError, match="min_total_pct"):
        bl.load(write(Path(tempfile.mkdtemp()), text), ROOT)


def test_a_total_floor_out_of_range_is_an_error():
    text = GOOD.replace("min_total_pct = 64", "min_total_pct = 101")
    with pytest.raises(bl.BaselineError, match=r"\[0, 100\]"):
        bl.load(write(Path(tempfile.mkdtemp()), text), ROOT)


def test_judge_total_is_green_above_the_floor():
    assert ratchet.judge_total(_bl(64), 64.72) == []


def test_judge_total_is_green_exactly_on_the_floor():
    assert ratchet.judge_total(_bl(64), 64.0) == []


def test_judge_total_is_red_below_the_floor_and_names_both_numbers():
    failures = ratchet.judge_total(_bl(64), 63.4)
    assert failures
    joined = "\n".join(failures)
    assert "63.4" in joined and "64" in joined


def test_tighten_raises_the_total_floor_when_coverage_rose():
    text, changes = ratchet.rewrite_baseline(
        BASELINE_WITH_TOTAL, _counts(), total_pct=71.9
    )
    assert changes == ["min_total_pct: 64 -> 71"]
    assert "min_total_pct = 71" in text


def test_tighten_never_lowers_the_total_floor():
    """Een dip mag de vloer niet meeslepen; dan is het geen ratel meer."""
    text, changes = ratchet.rewrite_baseline(
        BASELINE_WITH_TOTAL, _counts(), total_pct=50.0
    )
    assert changes == []
    assert "min_total_pct = 64" in text


def test_tighten_ignores_a_rise_inside_the_same_whole_percent():
    """64.7 -> 64.9 is ruis; een PR per week daarvoor is onleesbaar."""
    _, changes = ratchet.rewrite_baseline(
        BASELINE_WITH_TOTAL, _counts(), total_pct=64.98
    )
    assert changes == []


def test_tighten_does_both_axes_in_one_pass():
    text, changes = ratchet.rewrite_baseline(
        BASELINE_WITH_TOTAL, _counts(F401=1), total_pct=70.2
    )
    assert changes == ["F401: 19 -> 1", "min_total_pct: 64 -> 70"]
    assert "F401   = 1" in text and "min_total_pct = 70" in text


# --- lighthouse: the directories, the command and the base from the config -------

from tests.pack._testkit import MINIMAL_TOML


def _config(tmp_path: Path, monkeypatch, extra: str) -> None:
    path = tmp_path / "lighthouse.toml"
    path.write_text(MINIMAL_TOML + extra, encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(path))


DIRS = '\n[python]\nsource_dirs = ["lib"]\nlint_dirs = ["lib", "t"]\n'


def test_dirs_from_config(tmp_path, monkeypatch):
    _config(tmp_path, monkeypatch, DIRS)
    root = _complexipy_root(tmp_path)
    (root / ".ruff-version").write_text("0.16.4\n", encoding="utf-8")
    seen: list[list[str]] = []

    def fake_run(argv, **kwargs):
        seen.append(list(argv))
        if "--output" in argv:
            Path(argv[argv.index("--output") + 1]).write_text("[]", encoding="utf-8")
        return subprocess.CompletedProcess(argv, 0, stdout="[]", stderr="")

    monkeypatch.setattr(ratchet.subprocess, "run", fake_run)
    ratchet.count_ruff(root)
    ratchet.count_complexipy(root)
    ruff, complexipy = seen
    assert ruff[-2:] == ["lib", "t"]
    assert str(root / "lib") in complexipy and str(root / "t") in complexipy
    assert str(root / "src") not in complexipy
    argv = ratchet.coverage_argv()
    assert "--cov=lib" in argv and "--cov=src" not in argv


def test_coverage_argv_default():
    assert ratchet.coverage_argv() == [
        "uv",
        "run",
        "pytest",
        "-q",
        "--cov=src",
        "--cov-report=json:coverage.json",
    ]


def test_coverage_command_from_config(tmp_path, monkeypatch):
    _config(
        tmp_path,
        monkeypatch,
        DIRS + "coverage_command = \"uv run pytest tests/unit -m 'not slow'\"\n",
    )
    assert ratchet.coverage_argv() == [
        "uv",
        "run",
        "pytest",
        "tests/unit",
        "-m",
        "not slow",
        "--cov=lib",
        "--cov-report=json:coverage.json",
    ]


def test_coverage_diff_follows_source_dirs(tmp_path, monkeypatch):
    _config(tmp_path, monkeypatch, DIRS)
    root = _repo_with_baseline(tmp_path, 0)
    seen: dict = {}

    def fake_git(_root, args):
        seen["args"] = args
        raise SystemExit("stopped after the diff")

    monkeypatch.setattr(ratchet, "_merge_base", lambda _root, _ref: "abc")
    monkeypatch.setattr(ratchet, "_git", fake_git)
    with pytest.raises(SystemExit):
        ratchet.cmd_coverage(root, "origin/main", None)
    assert seen["args"] == ["diff", "--unified=0", "abc..HEAD", "--", "lib"]


@pytest.mark.parametrize(
    ("extra", "base"),
    [("", "origin/main"), ('default_branch = "trunk"\n', "origin/trunk")],
)
def test_base_follows_default_branch(tmp_path, monkeypatch, extra, base):
    path = tmp_path / "lighthouse.toml"
    path.write_text(MINIMAL_TOML + extra, encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(path))
    seen: dict = {}
    monkeypatch.setattr(ratchet, "repo_root", lambda: tmp_path)
    monkeypatch.setattr(
        ratchet, "cmd_coverage", lambda root, ref, report: seen.setdefault("ref", ref) and 0
    )
    ratchet.main(["coverage"])
    assert seen["ref"] == base


def test_an_explicit_base_wins(tmp_path, monkeypatch):
    seen: dict = {}
    monkeypatch.setattr(ratchet, "repo_root", lambda: tmp_path)
    monkeypatch.setattr(
        ratchet, "cmd_coverage", lambda root, ref, report: seen.setdefault("ref", ref) and 0
    )
    ratchet.main(["coverage", "--base", "upstream/dev"])
    assert seen["ref"] == "upstream/dev"


def test_a_broken_config_is_red(tmp_path, monkeypatch):
    _config(tmp_path, monkeypatch, '\n[python]\nsource_dirs = "src"\n')
    with pytest.raises(SystemExit, match="ratchet: .*python.source_dirs"):
        ratchet.coverage_argv()


@pytest.mark.parametrize("key", ["source_dirs", "lint_dirs"])
def test_an_empty_dir_list_is_red(tmp_path, monkeypatch, key):
    """`[]` would run ruff, complexipy and `git diff --` over the whole repository and
    pytest without `--cov`: stop instead, and name the key."""
    _config(tmp_path, monkeypatch, f"\n[python]\n{key} = []\n")
    with pytest.raises(SystemExit, match=f"ratchet: python.{key} is empty"):
        ratchet._dirs(f"python.{key}")


def test_complexity_skill_path():
    path, _, rest = ratchet.COMPLEXITY_SKILL.partition(", section ")
    assert path == ".claude/skills/lighthouse-python-writing/SKILL.md"
    heading = rest.strip("`")
    skill = _SCRIPTS.parent / "skills" / "python-writing" / "claude" / "SKILL.md"
    assert heading in skill.read_text(encoding="utf-8").splitlines()


def _messages(path: Path) -> list[str]:
    """Every string literal in `path` except docstrings, which stay Dutch (ontwerpkeuze 3
    van (b))."""
    import ast

    tree = ast.parse(path.read_text(encoding="utf-8"))
    docstrings = set()
    for node in ast.walk(tree):
        if isinstance(node, (ast.Module, ast.FunctionDef, ast.ClassDef)) and node.body:
            first = node.body[0]
            if isinstance(first, ast.Expr) and isinstance(first.value, ast.Constant):
                docstrings.add(id(first.value))
    return [
        node.value
        for node in ast.walk(tree)
        if isinstance(node, ast.Constant)
        and isinstance(node.value, str)
        and id(node) not in docstrings
    ]


@pytest.mark.parametrize("name", ["ratchet.py", "baseline.py", "diffcov.py"])
def test_output_is_english(name):
    """Every message and help text a person or a skill reads is English (V4)."""
    strings = _messages(_SCRIPTS / "quality" / name)
    for word in (" de ", " het ", " een ", " geen ", " niet ", " zou ", " ontbreekt", " onleesbaar"):
        hits = [t for t in strings if word in f" {t} "]
        assert not hits, (word, hits[:3])
