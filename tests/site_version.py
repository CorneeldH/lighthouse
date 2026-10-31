"""The version token of the static site: `?v=<8 hex>` on every local `.js`/`.css` reference.

The token is the first 8 hex characters of a sha256 over the names and contents of the top-level
`.js` and `.css` files of docs/site/. Tokens are stripped before hashing, so writing them does
not change the version. `python -m tests.site_version` rewrites every reference."""

import hashlib
import re
from pathlib import Path

SITE = Path(__file__).resolve().parent.parent / "docs" / "site"

TOKEN = re.compile(r"\?v=[0-9a-f]{8}")
HTML_REF = re.compile(r"""(\b(?:href|src)\s*=\s*["'])([^"':#]+\.(?:js|css)(?:\?[^"']*)?)(["'])""")
JS_REF = re.compile(r"""(\bfrom\s*["'])(\./[^"']+\.js(?:\?[^"']*)?)(["'])""")


def site_version(site: Path = SITE) -> str:
    digest = hashlib.sha256()
    for path in sorted([*site.glob("*.js"), *site.glob("*.css")], key=lambda p: p.name):
        digest.update(path.name.encode("utf-8"))
        digest.update(TOKEN.sub("", path.read_text(encoding="utf-8")).encode("utf-8"))
    return digest.hexdigest()[:8]


def _pattern(path: Path) -> re.Pattern[str]:
    return HTML_REF if path.suffix == ".html" else JS_REF


def asset_refs(path: Path) -> list[str]:
    """Every local `.js`/`.css` reference in `path` as written, query included."""
    return [m.group(2) for m in _pattern(path).finditer(path.read_text(encoding="utf-8"))]


def write(site: Path = SITE) -> list[Path]:
    """Set every reference to `<name>?v=<site_version()>`; return the files that changed."""
    version = site_version(site)
    changed = []
    for path in sorted([*site.glob("*.html"), *site.glob("*.js")]):
        text = path.read_text(encoding="utf-8")
        new = _pattern(path).sub(
            lambda m: f"{m.group(1)}{m.group(2).split('?', 1)[0]}?v={version}{m.group(3)}", text
        )
        if new != text:
            path.write_text(new, encoding="utf-8")
            changed.append(path)
    return changed


if __name__ == "__main__":
    for changed_path in write():
        print(changed_path)
