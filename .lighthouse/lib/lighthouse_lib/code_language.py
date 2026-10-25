# lighthouse: core@dev
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

import ast
import io
import re
import tokenize
from pathlib import Path
from typing import NamedTuple


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


MARKER = "lighthouse: allow-language"


class Finding(NamedTuple):
    line: int
    kind: str  # "comment", "docstring" or "name"
    text: str  # the name, or the first line of the block


class CannotCheck(Exception):
    """The file could not be read as source; the message is the reason."""


def _comment_findings(source: str) -> list[Finding]:
    blocks: list[list[tuple[int, str]]] = []
    for token in tokenize.generate_tokens(io.StringIO(source).readline):
        if token.type != tokenize.COMMENT:
            continue
        line, text = token.start[0], token.string.lstrip("#").strip()
        if blocks and blocks[-1][-1][0] == line - 1:
            blocks[-1].append((line, text))
        else:
            blocks.append([(line, text)])
    found = []
    for block in blocks:
        kept = [(line, text) for line, text in block if MARKER not in text]
        if any(is_dutch_text(text) for _, text in kept):
            found.append(Finding(block[0][0], "comment", block[0][1]))
    return found


def _name_nodes(tree: ast.AST):
    """Yield `(line, name)` for every place a name comes into being in this file."""
    for node in ast.walk(tree):
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)):
            yield node.lineno, node.name
        elif isinstance(node, ast.arg):
            yield node.lineno, node.arg
        elif isinstance(node, ast.Name) and isinstance(node.ctx, ast.Store):
            yield node.lineno, node.id
        elif isinstance(node, ast.Attribute) and isinstance(node.ctx, ast.Store):
            yield node.end_lineno or node.lineno, node.attr
        elif isinstance(node, ast.alias) and node.asname:
            yield node.lineno, node.asname
        elif isinstance(node, ast.ExceptHandler) and node.name:
            yield node.lineno, node.name


def scan_python(source: str) -> list[Finding]:
    """Dutch comments, docstrings and names in Python `source`, sorted by line."""
    try:
        tree = ast.parse(source)
        found = _comment_findings(source)
    except (tokenize.TokenError, SyntaxError) as error:  # IndentationError is a SyntaxError
        raise CannotCheck(f"cannot parse Python: {error}") from error
    lines = source.splitlines()

    for node in ast.walk(tree):
        if not isinstance(
            node, (ast.Module, ast.ClassDef, ast.FunctionDef, ast.AsyncFunctionDef)
        ):
            continue
        doc = ast.get_docstring(node)
        if doc is None or MARKER in doc or not is_dutch_text(doc):
            continue
        found.append(Finding(node.body[0].lineno, "docstring", doc.splitlines()[0]))

    seen = set()
    for line, name in _name_nodes(tree):
        if (line, name) in seen:
            continue
        seen.add((line, name))
        marked = 0 < line <= len(lines) and MARKER in lines[line - 1]
        if not marked and is_dutch_name(name):
            found.append(Finding(line, "name", name))
    return sorted(found, key=lambda f: f.line)
