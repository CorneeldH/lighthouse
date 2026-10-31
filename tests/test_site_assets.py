"""Every local asset reference in docs/site/ carries the version token, so a visitor with an old
cache never mixes a new page with an old module.

After any change to a `.js` or `.css` in docs/site/, run `uv run python -m tests.site_version`;
otherwise `test_every_local_asset_carries_the_site_version` is red."""

import re

from tests.site_version import SITE, asset_refs, site_version
from tests.test_site_pages import relative_links


def test_every_local_asset_carries_the_site_version():
    version = site_version()
    pages = sorted(SITE.glob("*.html")) + sorted(SITE.glob("*.js"))
    refs = [(p.name, r) for p in pages for r in asset_refs(p)]
    assert refs, "no asset references found"
    assert [(p, r) for p, r in refs if not r.endswith(f"?v={version}")] == []


def test_version_follows_the_assets_not_the_tokens(tmp_path):
    for p in [*SITE.glob("*.js"), *SITE.glob("*.css")]:
        (tmp_path / p.name).write_text(p.read_text(encoding="utf-8"), encoding="utf-8")
    before = site_version(tmp_path)
    css = tmp_path / "map.css"
    css.write_text(css.read_text(encoding="utf-8") + "\n/* x */\n", encoding="utf-8")
    changed = site_version(tmp_path)
    assert changed != before
    js = tmp_path / "chain.js"
    text = js.read_text(encoding="utf-8")
    js.write_text(re.sub(r"(\./map\.js)(\?v=[0-9a-f]{8})?", r"\1?v=00000000", text), encoding="utf-8")
    assert js.read_text(encoding="utf-8") != text
    assert site_version(tmp_path) == changed


def test_asset_refs_finds_the_map_import_in_chain():
    assert any(r.startswith("./map.js") for r in asset_refs(SITE / "chain.js"))


def test_asset_refs_misses_no_local_asset():
    for p in sorted(SITE.glob("*.html")) + sorted(SITE.glob("*.js")):
        expected = {link for link in relative_links(p) if link.endswith((".js", ".css"))}
        assert {r.split("?", 1)[0] for r in asset_refs(p)} == expected, p.name
