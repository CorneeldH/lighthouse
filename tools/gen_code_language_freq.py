"""Generate pack/core/lib/code_language_freq.tsv.gz from wordfreq.

Run from the repository root:

    uv run --with wordfreq==3.1.1 python tools/gen_code_language_freq.py

Data version: wordfreq 3.1.1 (CC BY-SA 4.0 data, see code_language_freq_NOTICE.txt).
Output: gzip of UTF-8 lines "word<TAB>en<TAB>nl", sorted by word, with en and nl as
round(zipf * 10) (0 = absent). Only English and Dutch, only all-letter words, and only
words whose unrounded zipf is at least 1.5 in either language.
"""

import gzip
import re
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
DENYLIST = REPO / "ci" / "literal-denylist.txt"
OUTPUT = REPO / "pack" / "core" / "lib" / "code_language_freq.tsv.gz"
MIN_ZIPF = 1.5
LEVELS = (9, 8, 7, 6)


def denied_patterns(path: Path) -> list[str]:
    patterns = []
    for line in path.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if line and not line.startswith("#"):
            patterns.append(line.lower())
    return patterns


def keep(word: str, patterns: list[str]) -> bool:
    lowered = word.lower()
    return not any(p in lowered for p in patterns)


def encode(rows: list[tuple[str, int, int]], level: int = 9) -> bytes:
    text = "".join(f"{w}\t{en}\t{nl}\n" for w, en, nl in sorted(rows))
    return gzip.compress(text.encode("utf-8"), compresslevel=level, mtime=0)


def build_rows(patterns: list[str]) -> list[tuple[str, int, int]]:
    from wordfreq import iter_wordlist, zipf_frequency

    words = set(iter_wordlist("en")) | set(iter_wordlist("nl"))
    rows = []
    for word in words:
        if not re.fullmatch(r"[^\W\d_]+", word):
            continue
        word = word.lower()
        en = zipf_frequency(word, "en")
        nl = zipf_frequency(word, "nl")
        if max(en, nl) < MIN_ZIPF or not keep(word, patterns):
            continue
        rows.append((word, round(en * 10), round(nl * 10)))
    return sorted(set(rows))


def main() -> int:
    patterns = denied_patterns(DENYLIST)
    rows = build_rows(patterns)
    for level in LEVELS:
        data = encode(rows, level)
        lowered = data.lower()
        if not any(p.encode("utf-8") in lowered for p in patterns):
            OUTPUT.write_bytes(data)
            print(f"wrote {len(rows)} rows, {len(data)} bytes, level {level}")
            return 0
    hit = next(p for p in patterns if p.encode("utf-8") in encode(rows, LEVELS[-1]).lower())
    print(f"every gzip level contains denied pattern {hit!r} as bytes", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
