"""Shell under `pack/` must run on macOS (BSD userland, bash 3.2) as well as on Linux (#90).

A line that deliberately uses a banned construct next to a fallback carries
`# portable: <reason>`. A `command -v <tool>` probe is a fallback test, not a use.
"""

import re
from pathlib import Path

PACK = Path(__file__).resolve().parents[2] / "pack"

GNU_ONLY = {
    r"\bsed -i(\s|$)": "sed -i without a suffix (BSD needs -i '')",
    r"\bdate -d\b": "GNU date -d",
    r"\bstat -c\b": "GNU stat -c",
    r"\breadlink -f\b": "readlink -f",
    r"\bgrep -[a-zA-Z]*P": "grep -P",
    r"\bxargs -r\b": "xargs -r",
    r"(?<![\w-])sha256sum\b": "sha256sum (macOS has shasum -a 256)",
    r"-printf\b": "find -printf",
}
BASH4 = {
    r"\bdeclare -A\b": "associative array",
    r"\b(mapfile|readarray)\b": "mapfile/readarray",
    r"\$\{\w+(,,|\^\^)\}": "case conversion",
    r"&>>": "&>>",
    r"\|&": "|&",
    r"\blocal -n\b": "nameref",
}

MARKER = re.compile(r"#\s*portable:(.*)$")
PROBE = re.compile(r"\bcommand -v \S+")
TRAILING_COMMENT = re.compile(r"(^|\s)#.*$")


def _shell_files() -> list[Path]:
    files = []
    for path in sorted(PACK.rglob("*")):
        if not path.is_file():
            continue
        if path.suffix == ".sh":
            files.append(path)
        elif not path.suffix:
            try:
                first = path.open(encoding="utf-8").readline()
            except (UnicodeDecodeError, OSError):
                continue
            if re.match(r"#!.*\b(ba)?sh\b", first):
                files.append(path)
    return files


def _code(line: str) -> str:
    """The part of a line that runs: no comment, no `command -v <tool>` probe."""
    return PROBE.sub("", TRAILING_COMMENT.sub("", line))


def _hits(table: dict[str, str]) -> list[str]:
    found = []
    for path in _shell_files():
        for number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
            if MARKER.search(line):
                continue
            code = _code(line)
            for pattern, reason in table.items():
                if re.search(pattern, code):
                    found.append(f"{path.relative_to(PACK.parent)}:{number}: {reason}")
    return found


def test_pack_shell_has_no_gnu_only_constructs():
    hits = _hits(GNU_ONLY)
    assert not hits, "\n" + "\n".join(hits)


def test_pack_shell_has_no_bash4_constructs():
    hits = _hits(BASH4)
    assert not hits, "\n" + "\n".join(hits)


def test_portable_marker_needs_a_reason():
    bare = []
    for path in _shell_files():
        for number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
            match = MARKER.search(line)
            if match and not match.group(1).split():
                bare.append(f"{path.relative_to(PACK.parent)}:{number}")
    assert not bare, "`# portable:` without a reason: " + ", ".join(bare)
