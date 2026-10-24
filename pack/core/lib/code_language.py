"""Recognise Dutch text and names in code (#26).

Detection only: callers decide where to look (comments, docstrings, names). A word
counts as Dutch when it is in one of two lists next to this module:

- `code_language_nl_function.txt`: function words (articles, pronouns, prepositions,
  conjunctions, auxiliary verbs);
- `code_language_nl_content.txt`: content words common in Dutch code.

Both lists skip words that are also English or common in English code, such as `map`,
`pad` and `is`; one such word would flag a correct file.
"""

from __future__ import annotations

import re
from pathlib import Path


def _read_list(filename: str) -> frozenset[str]:
    lines = Path(__file__).with_name(filename).read_text(encoding="utf-8").splitlines()
    entries = (line.strip().lower() for line in lines)
    return frozenset(e for e in entries if e and not e.startswith("#"))


FUNCTION_WORDS = _read_list("code_language_nl_function.txt")
CONTENT_WORDS = _read_list("code_language_nl_content.txt")
_ALL_WORDS = FUNCTION_WORDS | CONTENT_WORDS

# An acronym run ("HTTP" in "HTTPServer") or a capitalised or lower-case word.
_NAME_PART = re.compile(r"[A-Z]+(?![a-z])|[A-Z]?[a-z]+")
_WORD = re.compile(r"[^\W\d_]+")


def split_name(name: str) -> list[str]:
    """Split an identifier on `_`, digits and case changes; lower-case, no empty parts."""
    return [part.lower() for part in _NAME_PART.findall(name)]


def words(text: str) -> list[str]:
    """The words of `text`: runs of letters (accents included), lower-case."""
    return _WORD.findall(text.lower())


def is_dutch_text(text: str) -> bool:
    """True when at least one word of `text` is in a Dutch word list."""
    return any(word in _ALL_WORDS for word in words(text))


def is_dutch_name(name: str) -> bool:
    """True when at least one part of the identifier `name` is in a Dutch word list."""
    return any(part in _ALL_WORDS for part in split_name(name))
