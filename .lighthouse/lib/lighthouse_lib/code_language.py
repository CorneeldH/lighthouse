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
import fnmatch
import io
import re
import sys
import tokenize
from collections.abc import Callable
from pathlib import Path
from typing import NamedTuple

# Run as a file on its path: put `.lighthouse/lib` first, so `lighthouse_lib`
# imports from this installation.
if not __package__:
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from lighthouse_lib import config


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
    return _dutch_blocks(blocks)


def _dutch_blocks(blocks: list[list[tuple[int, str]]]) -> list[Finding]:
    """A finding on the first line of each comment block with Dutch text outside marked lines."""
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
    except (tokenize.TokenError, SyntaxError, ValueError) as error:
        # IndentationError is a SyntaxError; Python 3.11 raises ValueError on a NUL byte.
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

    found += _name_findings(list(_name_nodes(tree)), lines)
    return sorted(found, key=lambda f: f.line)


_IDENT = r"[A-Za-z_]\w*"
_IDENT_LIST = rf"{_IDENT}(?:[ \t]*,[ \t]*{_IDENT})*"


def _name_findings(
    names: list[tuple[int, str]], source_lines: list[str]
) -> list[Finding]:
    """Findings for Dutch names; one per name per line, none on a marked line."""
    found = []
    seen = set()
    for line, name in names:
        if (line, name) in seen:
            continue
        seen.add((line, name))
        marked = 0 < line <= len(source_lines) and MARKER in source_lines[line - 1]
        if not marked and is_dutch_name(name):
            found.append(Finding(line, "name", name))
    return found


def _line_of(text: str, offset: int) -> int:
    return text.count("\n", 0, offset) + 1


# --- shell ---

_HEREDOC = re.compile(rf"<<(-?)[ \t]*(?:'([^']*)'|\"([^\"]*)\"|({_IDENT}))")
_SHELL_FUNCTION = re.compile(rf"^[ \t]*function[ \t]+({_IDENT})", re.MULTILINE)
_SHELL_FUNCTION_PARENS = re.compile(rf"^[ \t]*({_IDENT})[ \t]*\([ \t]*\)", re.MULTILINE)
_SHELL_ASSIGN = re.compile(rf"(?:^|[;&|(])[ \t]*({_IDENT})\+?=", re.MULTILINE)
_SHELL_DECLARE = re.compile(r"^[ \t]*(?:local|export|readonly|declare|typeset)\b", re.MULTILINE)
_SHELL_DECLARED = re.compile(rf"[ \t]+(?:-\S+|({_IDENT})(?=[ \t=\n]|$))(?:=\S*)?")


def _scan_shell_text(source: str) -> tuple[list[str], list[list[tuple[int, str]]]]:
    """Code lines with strings, comments and heredoc bodies blanked, plus comment blocks."""
    code_lines: list[str] = []
    blocks: list[list[tuple[int, str]]] = []
    last_comment = -1
    quote = ""
    heredoc: tuple[str, bool] | None = None
    pending: list[tuple[str, bool]] = []
    for number, line in enumerate(source.splitlines(), 1):
        if heredoc:
            body = line.rstrip("\r")
            if (body.lstrip("\t") if heredoc[1] else body) == heredoc[0]:
                heredoc = pending.pop(0) if pending else None
            code_lines.append("")
            continue
        out = []
        i = 0
        while i < len(line):
            char = line[i]
            if quote:
                if char == "\\" and quote == '"':
                    i += 1
                elif char == quote:
                    quote = ""
                    out.append(char)
            elif char == "\\":
                i += 1
            elif char in "'\"":
                quote = char
                out.append(char)
            elif char == "#" and (i == 0 or line[i - 1] in " \t;&|("):
                if not (number == 1 and line.startswith("#!")):
                    text = line[i + 1 :].strip()
                    if last_comment == number - 1 and blocks:
                        blocks[-1].append((number, text))
                    else:
                        blocks.append([(number, text)])
                    last_comment = number
                break
            elif line.startswith("<<", i) and "((" not in line[:i]:
                match = _HEREDOC.match(line, i)
                if match and not line.startswith("<<<", i):
                    delimiter = match[2] or match[3] or match[4]
                    pending.append((delimiter, bool(match[1])))
                    i = match.end() - 1
                out.append(char)
            else:
                out.append(char)
            i += 1
        code_lines.append("".join(out))
        if pending and not quote:
            heredoc = pending.pop(0)
    return code_lines, blocks


def scan_shell(source: str) -> list[Finding]:
    """Dutch comments and names in shell `source`, sorted by line.

    A heuristic reader, not a parser: it skips quoted strings and heredoc bodies and
    looks for function names and assigned variables.
    """
    code_lines, blocks = _scan_shell_text(source)
    code = "\n".join(code_lines)
    names = []
    for pattern in (_SHELL_FUNCTION, _SHELL_FUNCTION_PARENS, _SHELL_ASSIGN):
        names += [(_line_of(code, m.start(1)), m[1]) for m in pattern.finditer(code)]
    for declare in _SHELL_DECLARE.finditer(code):
        position = declare.end()
        while match := _SHELL_DECLARED.match(code, position):
            position = match.end()
            if match[1]:
                names.append((_line_of(code, match.start(1)), match[1]))
    found = _dutch_blocks(blocks) + _name_findings(names, source.splitlines())
    return sorted(found, key=lambda f: f.line)


# --- Go ---

_GO_PACKAGE = re.compile(rf"^[ \t]*package[ \t]+({_IDENT})", re.MULTILINE)
_GO_FUNC_NAMED = re.compile(
    rf"^[ \t]*func[ \t]+(?:\(([^)]*)\)[ \t]*)?({_IDENT})[ \t]*(?:\[[^\]]*\][ \t]*)?\(([^)]*)\)",
    re.MULTILINE,
)
_GO_FUNC_PARAMS = re.compile(r"\bfunc[ \t]*\(([^)]*)\)")
_GO_DECLARATION = re.compile(rf"^[ \t]*(?:var|const|type)[ \t]+({_IDENT_LIST})", re.MULTILINE)
_GO_GROUP = re.compile(r"^[ \t]*(?:var|const|type)[ \t]*\(", re.MULTILINE)
_GO_DEFINE = re.compile(rf"(?<![\w.])({_IDENT_LIST})[ \t]*:=")
_GO_STRUCT = re.compile(r"\bstruct[ \t]*\{")
_GO_FIELD = re.compile(rf"^[ \t]*({_IDENT_LIST})[ \t]+\S")


def _scan_go_text(source: str) -> tuple[str, list[list[tuple[int, str]]]]:
    """Source with comments, strings and runes blanked (same offsets), plus comment blocks."""
    out = list(source)
    blocks: list[list[tuple[int, str]]] = []
    last_line_comment = -1  # line of the last `//` comment, when it ended the last block
    line = 1
    i = 0
    size = len(source)

    def blank(start: int, end: int) -> None:
        for k in range(start, end):
            if out[k] != "\n":
                out[k] = " "

    while i < size:
        char = source[i]
        if char == "\n":
            line += 1
        elif source.startswith("//", i):
            end = source.find("\n", i)
            end = size if end < 0 else end
            text = source[i + 2 : end].strip()
            if last_line_comment == line - 1 and blocks:
                blocks[-1].append((line, text))
            else:
                blocks.append([(line, text)])
            last_line_comment = line
            blank(i, end)
            i = end
            continue
        elif source.startswith("/*", i):
            end = source.find("*/", i + 2)
            end = size if end < 0 else end + 2
            body = source[i + 2 : max(i + 2, end - 2)]
            parts = [part.strip().lstrip("*").strip() for part in body.split("\n")]
            blocks.append([(line + n, part) for n, part in enumerate(parts)])
            last_line_comment = -1
            line += source.count("\n", i, end)
            blank(i, end)
            i = end
            continue
        elif char in "\"'`":
            end = i + 1
            while end < size and source[end] != char:
                if source[end] == "\n" and char != "`":
                    break
                end += 2 if source[end] == "\\" and char != "`" else 1
            end = min(end + 1, size)
            blank(i + 1, end - 1)
            line += source.count("\n", i, end)
            i = end
            continue
        i += 1
    return "".join(out), blocks


def _go_param_names(group: str, start: int, code: str) -> list[tuple[int, str]]:
    """Parameter names in the text of one parameter list that begins at offset `start`."""
    pieces = [(m.start(), m[0].split()) for m in re.finditer(r"[^,]+", group)]
    if not any(len(tokens) > 1 for _, tokens in pieces):
        return []  # only types, no names
    return [
        (_line_of(code, start + offset + group[offset:].find(tokens[0])), tokens[0])
        for offset, tokens in pieces
        if tokens and re.fullmatch(_IDENT, tokens[0])
    ]


def _go_names(code: str) -> list[tuple[int, str]]:
    names: list[tuple[int, str]] = []

    def add_list(match: re.Match[str], group: int = 1) -> None:
        for name in re.finditer(_IDENT, match[group]):
            names.append((_line_of(code, match.start(group) + name.start()), name[0]))

    for match in _GO_PACKAGE.finditer(code):
        add_list(match)
    for match in _GO_FUNC_NAMED.finditer(code):
        names.append((_line_of(code, match.start(2)), match[2]))
        if match[1] is not None:
            names += _go_param_names(match[1], match.start(1), code)
        names += _go_param_names(match[3], match.start(3), code)
    for match in _GO_FUNC_PARAMS.finditer(code):
        names += _go_param_names(match[1], match.start(1), code)
    for match in _GO_DECLARATION.finditer(code):
        add_list(match)
    for match in _GO_DEFINE.finditer(code):
        add_list(match)
    for group in _GO_GROUP.finditer(code):
        depth, end = 1, group.end()
        while end < len(code) and depth:
            depth += {"(": 1, ")": -1}.get(code[end], 0)
            end += 1
        for entry in re.finditer(rf"^[ \t]*({_IDENT_LIST})", code[group.end() : end], re.MULTILINE):
            base = group.end() + entry.start(1)
            for name in re.finditer(_IDENT, entry[1]):
                names.append((_line_of(code, base + name.start()), name[0]))
    for struct in _GO_STRUCT.finditer(code):
        depth, end = 1, struct.end()
        while end < len(code) and depth:
            depth += {"{": 1, "}": -1}.get(code[end], 0)
            end += 1
        body = code[struct.end() : end - 1]
        for field in re.finditer(r"[^\n;]+", body):
            match = _GO_FIELD.match(field[0])
            if match:
                base = struct.end() + field.start() + match.start(1)
                for name in re.finditer(_IDENT, match[1]):
                    names.append((_line_of(code, base + name.start()), name[0]))
    return names


def scan_go(source: str) -> list[Finding]:
    """Dutch comments and names in Go `source`, sorted by line.

    A heuristic reader, not a parser: it blanks comments, strings, raw strings and runes
    first, then looks for the declaration forms of Go.
    """
    code, blocks = _scan_go_text(source)
    found = _dutch_blocks(blocks) + _name_findings(_go_names(code), source.splitlines())
    return sorted(found, key=lambda f: f.line)


# --- checking a file ---

EXTENSIONS: dict[str, Callable[[str], list[Finding]]] = {
    ".py": scan_python,
    ".sh": scan_shell,
    ".bash": scan_shell,
    ".go": scan_go,
}

_MAX_LINES = 20
_TEXT_LIMIT = 60


def _managed_paths(root: Path) -> set[str]:
    import tomllib  # lazily, like config._read_toml: Python < 3.11 must still import this module

    manifest = root / ".lighthouse" / "manifest.toml"
    if not manifest.is_file():
        return set()
    try:
        data = tomllib.loads(manifest.read_text(encoding="utf-8"))
        return {unit["path"] for unit in data.get("unit", [])}
    except (OSError, UnicodeDecodeError, tomllib.TOMLDecodeError, KeyError, TypeError) as error:
        raise CannotCheck(f"cannot read {manifest}: {error}") from error


def check_file(path: Path, root: Path) -> list[Finding]:
    """Dutch comments, docstrings and names in the file at `path` (absolute) in repo `root`.

    Empty when the file is not checked: another extension, no `lighthouse.toml`, no
    `code_language`, an exempt glob or a file `lighthouse init` manages.
    """
    scan = EXTENSIONS.get(path.suffix)
    toml = root / "lighthouse.toml"
    if scan is None or not toml.is_file():
        return []
    try:
        cfg = config.load(toml)
        try:
            config.lookup(cfg, "project.code_language")
        except KeyError:
            return []
        try:
            exempt = config.lookup(cfg, "project.code_language_exempt")
        except KeyError:
            exempt = []
    except config.ConfigError as error:
        raise CannotCheck(str(error)) from error
    relative = path.relative_to(root).as_posix()
    if any(fnmatch.fnmatchcase(relative, glob) for glob in exempt):
        return []
    if relative in _managed_paths(root):
        return []
    try:
        source = path.read_text(encoding="utf-8-sig")  # a BOM is not content
    except UnicodeDecodeError as error:
        raise CannotCheck(f"{path} is not valid UTF-8: {error}") from error
    found = scan(source)
    if _name_findings([(1, path.stem)], []):
        found = sorted([*found, Finding(1, "name", path.stem)], key=lambda f: f.line)
    return found


def _report(path: Path, found: list[Finding]) -> str:
    lines = [f"code-language: {path} is not English"]
    for finding in found[:_MAX_LINES]:
        text = finding.text
        if finding.kind != "name" and len(text) > _TEXT_LIMIT:
            text = text[:_TEXT_LIMIT]
        lines.append(f"{path}:{finding.line}: {finding.kind}: {text}")
    if len(found) > _MAX_LINES:
        lines.append(f"… and {len(found) - _MAX_LINES} more")
    lines.append(
        "The whole file must be English. A line that is deliberately not English "
        f'gets "{MARKER}".'
    )
    if any(f.line == 1 and f.kind == "name" and f.text == path.stem for f in found):
        lines.append(
            f"The module name '{path.stem}' is not English either; the marker cannot clear it: "
            "rename the file or add it to `code_language_exempt`."
        )
    return "\n".join(lines)


def main(argv: list[str]) -> int:
    """`code_language.py <abs-path> <root>`: 0 clean, 1 findings on stdout, 2 could not check."""
    try:
        path, root = Path(argv[0]), Path(argv[1])
        found = check_file(path, root)
    except Exception as error:  # a crash must never read as findings (rc 1)
        print(f"code-language: COULD NOT CHECK — {error}", file=sys.stderr)
        return 2
    if not found:
        return 0
    print(_report(path, found))
    return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
