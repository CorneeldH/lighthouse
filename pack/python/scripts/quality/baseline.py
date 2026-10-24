"""Laden en valideren van quality-baseline.toml.

Streng met opzet. Een syntactisch geldige TOML met een tikfout erin is
gevaarlijker dan een kapotte: hij laadt, en de gate meet stilletjes iets anders
dan bedoeld. `C902 = 64` in plaats van `C901` zou de teller voor C901 op nul
zetten en die regel de facto uitschakelen.
"""

from __future__ import annotations

import tomllib
from dataclasses import dataclass
from pathlib import Path

# De acht nulregels. Ze staan in pyproject.toml [tool.ruff.lint] extend-select en
# horen NIET in de baseline: nul is een grens, geen stand.
ZERO_RULES: tuple[str, ...] = (
    "E722",
    "B006",
    "F403",
    "F811",
    "B023",
    "PT012",
    "PGH003",
    "PGH004",
)

# De zeven tellers. [ruff] moet er exact deze zeven bevatten — geen meer, geen
# minder, want een ontbrekende sleutel is een uitgeschakelde regel.
COUNTED_RULES: tuple[str, ...] = (
    "C901",
    "TRY400",
    "F401",
    "F841",
    "S110",
    "S112",
    "PT011",
)

# De ene teller in [complexipy]: het aantal functies boven cognitief 15.
COMPLEXIPY_KEY = "cognitive_over_15"


class BaselineError(ValueError):
    """De baseline is onbruikbaar. Fail closed: de gate gaat rood."""


@dataclass(frozen=True)
class Baseline:
    counts: dict[str, int]
    min_diff_pct: int
    # De gemeten globale src-dekking, vastgezet als vloer. Anders dan
    # min_diff_pct (beleid per PR) is dit een stand: hij komt uit een meting en
    # kruipt omhoog zodra nieuwe code de dekking optilt (#703).
    min_total_pct: int
    exclude: tuple[str, ...]
    # Het aantal functies met cognitieve complexiteit > 15 (complexipy, #1192).
    # Laatste veld, met default, zodat bestaande Baseline(...)-constructies
    # blijven werken; load() eist de sectie wél in het bestand.
    cognitive_over_15: int = 0


def load(path: Path, repo_root: Path) -> Baseline:
    """Lees en valideer de baseline, of gooi BaselineError met de reden erin."""
    try:
        text = path.read_text(encoding="utf-8")
    except FileNotFoundError as exc:
        raise BaselineError(f"{path} ontbreekt") from exc
    except OSError as exc:
        raise BaselineError(f"{path} is onleesbaar: {exc}") from exc
    try:
        raw = tomllib.loads(text)
    except tomllib.TOMLDecodeError as exc:
        raise BaselineError(f"{path} is onleesbaar: {exc}") from exc

    unknown = sorted(set(raw) - {"ruff", "coverage", "complexipy"})
    if unknown:
        raise BaselineError(f"onbekende secties in {path.name}: {unknown}")
    for section in ("ruff", "coverage", "complexipy"):
        if section not in raw:
            raise BaselineError(f"sectie [{section}] ontbreekt in {path.name}")

    coverage = _coverage_section(raw["coverage"])
    return Baseline(
        counts=_counts(raw["ruff"]),
        min_diff_pct=_percentage(coverage, "min_diff_pct"),
        min_total_pct=_percentage(coverage, "min_total_pct"),
        exclude=_exclude(coverage, repo_root),
        cognitive_over_15=_complexipy_counter(raw["complexipy"]),
    )


def _counts(section: object) -> dict[str, int]:
    if not isinstance(section, dict):
        raise BaselineError("[ruff] is geen tabel")
    keys = set(section)

    baselined_zero = sorted(keys & set(ZERO_RULES))
    if baselined_zero:
        raise BaselineError(
            f"nulregels horen niet in de baseline: {baselined_zero} — "
            "nul is een grens, geen stand, en er is dus geen bump mogelijk"
        )
    # extra vóór missing: bij een tikfout (C902 i.p.v. C901) moet de melding
    # zowel de onbekende naam als de verwachte naam noemen. Zou missing eerst
    # gecontroleerd worden, dan raakt het ontbrekende C901 gemeld zonder ooit
    # te laten zien dat C902 de (foutieve) oorzaak is.
    extra = sorted(keys - set(COUNTED_RULES))
    if extra:
        raise BaselineError(
            f"[ruff] noemt onbekende regels: {extra} — verwacht exact {list(COUNTED_RULES)}"
        )
    missing = sorted(set(COUNTED_RULES) - keys)
    if missing:
        raise BaselineError(f"[ruff] mist telregels: {missing}")

    counts: dict[str, int] = {}
    for rule in COUNTED_RULES:
        value = section[rule]
        # type() en niet isinstance(): bool erft van int, dus `C901 = true` zou
        # een isinstance-check passeren en daarna als teller 1 gaan gelden.
        if type(value) is not int:
            raise BaselineError(f"{rule} is geen geheel getal: {value!r}")
        if value < 0:
            raise BaselineError(f"{rule} is negatief: {value}")
        counts[rule] = value
    return counts


def _complexipy_counter(section: object) -> int:
    """De ene teller van [complexipy]: `cognitive_over_15`, een geheel getal >= 0.

    Verplicht, net als de ruff-tellers: een ontbrekende sleutel zou een
    uitgeschakelde teller zijn.
    """
    if not isinstance(section, dict):
        raise BaselineError("[complexipy] is geen tabel")
    extra = sorted(set(section) - {COMPLEXIPY_KEY})
    if extra:
        raise BaselineError(
            f"[complexipy] noemt onbekende sleutels: {extra} — verwacht exact "
            f"[{COMPLEXIPY_KEY!r}]"
        )
    if COMPLEXIPY_KEY not in section:
        raise BaselineError(f"[complexipy] mist {COMPLEXIPY_KEY}")
    value = section[COMPLEXIPY_KEY]
    # type() en niet isinstance(): bool erft van int.
    if type(value) is not int:
        raise BaselineError(f"{COMPLEXIPY_KEY} is geen geheel getal: {value!r}")
    if value < 0:
        raise BaselineError(f"{COMPLEXIPY_KEY} is negatief: {value}")
    return value


def _coverage_section(section: object) -> dict:
    if not isinstance(section, dict):
        raise BaselineError("[coverage] is geen tabel")
    unknown = sorted(set(section) - {"min_diff_pct", "min_total_pct", "exclude"})
    if unknown:
        raise BaselineError(f"onbekende sleutels in [coverage]: {unknown}")
    return section


def _percentage(section: dict, key: str) -> int:
    """Een verplicht geheel percentage in [0, 100].

    Verplicht en niet defaulted: een ontbrekende sleutel zou een uitgeschakelde
    vloer worden, en dat is precies de stille pass die deze gate nergens mag
    hebben.
    """
    if key not in section:
        raise BaselineError(f"coverage.{key} ontbreekt")
    value = section[key]
    # type() en niet isinstance(): bool erft van int.
    if type(value) is not int:
        raise BaselineError(f"coverage.{key} is geen geheel getal: {value!r}")
    if not 0 <= value <= 100:
        raise BaselineError(f"coverage.{key} ligt buiten [0, 100]: {value}")
    return value


def _exclude(section: dict, repo_root: Path) -> tuple[str, ...]:
    value = section.get("exclude", [])
    if not isinstance(value, list) or not all(isinstance(item, str) for item in value):
        raise BaselineError(f"coverage.exclude is geen lijst strings: {value!r}")
    for prefix in value:
        if not (repo_root / prefix).exists():
            raise BaselineError(
                f"coverage.exclude wijst naar niets: {prefix} — een uitsluiting "
                "die nergens op slaat doet stil niets"
            )
    return tuple(value)
