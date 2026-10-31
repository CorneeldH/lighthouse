"""The pages of docs/site/ hold together: every relative link resolves, the page names the local
server command without needing JS, the component never writes data as HTML, and every page is
marked as English."""

import re
from html.parser import HTMLParser
from pathlib import Path

from tests.test_site_chain import SITE

GUIDE = SITE.parent / "getting-started.md"

SERVE_COMMAND = "python3 -m http.server --directory docs/site"
HTML_SINKS = ["innerHTML", "outerHTML", "insertAdjacentHTML", "document.write"]

HTML_LINK = re.compile(r"""\b(?:href|src|data-src)\s*=\s*["']([^"']+)["']""")
JS_IMPORT = re.compile(r"""\bimport\b[^;]*?\bfrom\s*["']([^"']+)["']""")
JS_BARE_IMPORT = re.compile(r"""\bimport\s*["']([^"']+)["']""")
JS_LOAD_JSON = re.compile(r"""\bloadJson\(\s*["']([^"']+)["']\s*\)""")
MD_LINK = re.compile(r"""\]\(([^)\s]+)\)""")
EXTERNAL = ("http:", "https:", "mailto:", "#")


def relative_links(path: Path) -> list[str]:
    """The relative link targets in `path`, without fragment or query: `href`, `src` and
    `data-src` in HTML, `import ... from "..."` and `loadJson("...")` in JS, `](...)` in
    markdown. Links that start with http(s):, mailto: or # are left out."""
    text = path.read_text(encoding="utf-8")
    if path.suffix == ".html":
        found = HTML_LINK.findall(text)
    elif path.suffix in {".js", ".mjs"}:
        found = JS_IMPORT.findall(text) + JS_BARE_IMPORT.findall(text) + JS_LOAD_JSON.findall(text)
    elif path.suffix == ".md":
        found = MD_LINK.findall(text)
    else:
        found = []
    links = []
    for link in found:
        if link.startswith(EXTERNAL):
            continue
        link = re.split(r"[#?]", link, maxsplit=1)[0]
        if link:
            links.append(link)
    return links


def site_files(*suffixes: str) -> list[Path]:
    return sorted(p for p in SITE.rglob("*") if p.suffix in suffixes)


class ElementById(HTMLParser):
    """The start-tag attributes and the text of the first element with a given id."""

    def __init__(self, element_id: str):
        super().__init__()
        self.element_id = element_id
        self.attrs: dict[str, str | None] | None = None
        self.text = ""
        self.depth = 0

    def handle_starttag(self, tag, attrs):
        if self.depth:
            self.depth += 1
        elif self.attrs is None and dict(attrs).get("id") == self.element_id:
            self.attrs = dict(attrs)
            self.depth = 1

    def handle_endtag(self, tag):
        if self.depth:
            self.depth -= 1

    def handle_data(self, data):
        if self.depth:
            self.text += data


def test_site_links_resolve():
    pages = site_files(".html")
    assert pages, "docs/site has no HTML page"
    for path in pages + site_files(".js") + [GUIDE]:
        links = relative_links(path)
        if path.suffix == ".html":
            assert links, f"{path.name} links no stylesheet or script"
        for link in links:
            target = (path.parent / link).resolve()
            assert target.is_file(), f"{path.name}: {link} points nowhere"


def test_pages_name_the_local_server_command():
    parser = ElementById("serve-hint")
    parser.feed((SITE / "chain.html").read_text(encoding="utf-8"))
    assert parser.attrs is not None, 'chain.html has no element with id="serve-hint"'
    assert SERVE_COMMAND in " ".join(parser.text.split())
    assert "hidden" not in parser.attrs
    style = (parser.attrs.get("style") or "").replace(" ", "")
    assert "display:none" not in style


def test_component_never_writes_html_from_data():
    scripts = site_files(".js")
    assert scripts, "docs/site has no JS file"
    for path in scripts:
        text = path.read_text(encoding="utf-8")
        for sink in HTML_SINKS:
            assert sink not in text, f"{path.name} uses {sink}"


def test_page_lang_is_english():
    pages = site_files(".html")
    assert pages, "docs/site has no HTML page"
    for path in pages:
        assert '<html lang="en">' in path.read_text(encoding="utf-8"), path.name


def test_index_lists_the_six_maps():
    links = relative_links(SITE / "index.html")
    assert "chain.html" in links
    text = (SITE / "index.html").read_text(encoding="utf-8")
    for issue in ["#100", "#101", "#102", "#103", "#104"]:
        assert issue in text, f"index.html does not name {issue}"
    assert 'id="maps"' in text


def test_chain_links_back_to_index():
    assert 'href="index.html"' in (SITE / "chain.html").read_text(encoding="utf-8")


def test_guide_links_to_the_chain_map():
    text = GUIDE.read_text(encoding="utf-8")
    section = text.split("## Your first issue", 1)[1].split("\n## ", 1)[0]
    assert "](site/chain.html)" in section
    assert SERVE_COMMAND in section
