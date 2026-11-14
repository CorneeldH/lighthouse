"""Run `fixtures/artefacts/cases.json` against `lighthouse_lib.artefacts`.

The proof that the fixture describes the Python module; the Go twin is
`TestArtefactCases` in `internal/protocol`. A case has `name`, `fn`, `input` and `expect`;
`fn` is one of `constants`, `parse_meta`, `render`, `roundtrip`, `parse_meta_roundtrip`,
`hash_of`, `split` and `chain`. With `FILL_EXPECT=1` a case whose expect is
`{"__python__": true}` is filled in from this module (review the diff).
"""

import json
import os
from pathlib import Path

import pytest

from lighthouse_lib.artefacts import (
    GITHUB_LIMIT,
    KINDS,
    Meta,
    current,
    hash_of,
    next_version,
    parse_meta,
    render,
    split,
)
from lighthouse_lib.protocol import Comment

FIXTURE = Path(__file__).parent / "fixtures" / "artefacts" / "cases.json"
CASES = json.loads(FIXTURE.read_text(encoding="utf-8"))
FIELDS = ("kind", "version", "part", "parts", "file", "commit", "hash", "replaces")


def _meta(raw: dict, extra: dict | None = None) -> Meta:
    return Meta(**{name: raw[name] for name in FIELDS}, extra=extra or {})


def _as_dict(meta: Meta | None):
    if meta is None:
        return None
    return {**{name: getattr(meta, name) for name in FIELDS}, "extra": meta.extra}


def _text(spec) -> str:
    if isinstance(spec, str):
        return spec
    return "".join(piece * count for piece, count in spec["repeat"])


def _config(tmp_path, monkeypatch, case_input: dict) -> None:
    lines = [f'[project]\nlanguage = "{case_input.get("language", "nl")}"\n']
    repo = case_input.get("repo", "Owner/repo")  # an explicit null means: no repo
    if repo:
        lines.append(f'[forge]\nkind = "github"\nrepo = "{repo}"\n')
    path = tmp_path / "lighthouse.toml"
    path.write_text("".join(lines), encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(path))


def _render(case_input: dict, content: str | None = None) -> str:
    meta = _meta(case_input["meta"], case_input.get("extra"))
    return render(meta, case_input["content"] if content is None else content)


def run(case: dict) -> object:
    inp, fn = case["input"], case["fn"]
    if fn == "constants":
        return {"KINDS": list(KINDS), "GITHUB_LIMIT": GITHUB_LIMIT}
    if fn == "parse_meta":
        return _as_dict(parse_meta(inp["body"]))
    if fn == "roundtrip":
        return {"meta": _as_dict(parse_meta(_render(inp)))}
    if fn == "parse_meta_roundtrip":
        return _as_dict(parse_meta(_render(inp)))
    if fn == "render":
        try:
            return {"body": _render(inp)}
        except Exception as err:  # noqa: BLE001 - the message is the expectation
            return {"error_contains": str(err)}
    if fn == "hash_of":
        return hash_of(inp["text"].encode("utf-8"))
    if fn == "split":
        text = _text(inp["text"])
        limit = len(text) // 4 + 1 if inp.get("limit_quarter") else inp.get("limit", 60_000)
        parts = split(text, limit)
        assert "".join(parts) == text
        assert all(parts) or text == ""
        out: dict = {"parts": parts}
        if len(text) > 4000:
            out = {"lengths": [len(p) for p in parts]}
        if case["expect"].get("fits_limit") or case["expect"].get("__python__") and inp.get("limit_quarter"):
            meta = Meta("plan", 99, 99, 99, "docs/x.md", "a" * 40, "0" * 12,
                        "https://github.com/Owner/repo/issues/1#issuecomment-1234567890")
            assert all(len(render(meta, p)) < GITHUB_LIMIT for p in parts)
            out["fits_limit"] = True
        return out
    if fn == "chain":
        comments = []
        for item in inp["comments"]:
            if "meta" in item:
                body = render(_meta(item["meta"]), f"v{item['meta']['version']}d{item['meta']['part']}")
                if item.get("legacy_marker"):
                    body = body.replace("<!-- lighthouse:", "<!-- paia:", 1)
            else:
                body = item["body"]
            comments.append(Comment(id=item["id"], body=body, created_at="", url=f"u{item['id']}"))
        return {
            "current": [c.id for c in current(comments, inp["kind"], inp["file"])],
            "next": next_version(comments, inp["kind"], inp["file"]),
        }
    raise AssertionError(fn)


def _params():
    return [pytest.param(case, id=case["name"]) for case in CASES]


@pytest.mark.parametrize("case", _params())
def test_artefact_case(case, tmp_path, monkeypatch):
    _config(tmp_path, monkeypatch, case["input"])
    actual = run(case)
    expect = case["expect"]
    if isinstance(expect, dict) and expect.get("__python__"):
        if os.environ.get("FILL_EXPECT") == "1":
            case["expect"] = actual
            FIXTURE.write_text(json.dumps(CASES, ensure_ascii=False, indent=1) + "\n", encoding="utf-8")
            return
        pytest.fail("expect is not filled in")
    if isinstance(expect, dict) and "error_contains" in expect:
        assert "error_contains" in actual
        assert expect["error_contains"] in actual["error_contains"]
        return
    if isinstance(expect, dict) and "fits_limit" in expect:
        assert actual == expect
        return
    assert actual == expect
