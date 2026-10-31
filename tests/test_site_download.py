"""The download on docs/site/install.html: platform.js picks the install block the visitor sees first,
the release label falls back to text when the API gives nothing usable, and every install block
names a built asset and checks it before the binary is used."""

import json
import re
import shutil
import subprocess
from html.parser import HTMLParser

import pytest

from tests.test_site_chain import ROOT, SITE
from tests.test_site_pages import ElementById

PLATFORM_JS = SITE / "platform.js"
INDEX = SITE / "index.html"
INSTALL = SITE / "install.html"
BUILD = ROOT / ".github" / "workflows" / "build.yml"
DOWNLOAD = "https://github.com/CorneeldH/lighthouse/releases/latest/download/"
TABS = ["linux-amd64", "linux-arm64", "darwin-arm64", "darwin-amd64", "windows-amd64"]
CHECKS = ["sha256sum --check", "shasum -a 256 -c", "Get-FileHash"]


def run_node(expression: str):
    node = shutil.which("node")
    assert node, "node is needed for the platform tests (no silent skip)"
    script = (
        f"import * as p from {json.dumps(PLATFORM_JS.as_uri())};\n"
        f"const {{ choosePlatform, releaseLabel }} = p;\n"
        f"console.log(JSON.stringify({expression}));"
    )
    out = subprocess.run(
        [node, "--input-type=module", "-e", script],
        capture_output=True, text=True, check=True, timeout=30,
    )
    return json.loads(out.stdout)


UA = {
    "linux": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/130 Safari/537.36",
    "linux_arm": "Mozilla/5.0 (X11; Linux aarch64; rv:131.0) Gecko/20100101 Firefox/131.0",
    "mac": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Safari/605.1.15",
    "windows": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/130",
    "android": "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 Chrome/130 Mobile",
    "iphone": "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15",
    "other": "Mozilla/5.0 (X11; FreeBSD amd64; rv:131.0) Gecko/20100101 Firefox/131.0",
}

CASES = [
    ({"platform": "Linux x86_64", "userAgent": UA["linux"], "architecture": ""}, "linux-amd64", None),
    ({"platform": "Linux aarch64", "userAgent": UA["linux_arm"], "architecture": ""}, "linux-arm64", None),
    ({"platform": "Linux", "userAgent": UA["linux"], "architecture": "arm"}, "linux-arm64", None),
    ({"platform": "MacIntel", "userAgent": UA["mac"], "architecture": ""}, "darwin-arm64", "intel"),
    ({"platform": "macOS", "userAgent": UA["mac"], "architecture": "arm"}, "darwin-arm64", "intel"),
    ({"platform": "macOS", "userAgent": UA["mac"], "architecture": "x86"}, "darwin-amd64", None),
    ({"platform": "Win32", "userAgent": UA["windows"], "architecture": ""}, "linux-amd64", "wsl"),
    ({"platform": "Windows", "userAgent": UA["windows"], "architecture": "arm"}, "linux-arm64", "wsl"),    ({"platform": "Linux armv8l", "userAgent": UA["android"], "architecture": ""}, None, None),
    ({"platform": "iPhone", "userAgent": UA["iphone"], "architecture": ""}, None, None),
    ({"platform": "", "userAgent": UA["other"], "architecture": ""}, None, None),
    ({"platform": "", "userAgent": "curl/8.9 (Darwin 24.0)", "architecture": ""}, None, None),
    ({"platform": "", "userAgent": "", "architecture": ""}, None, None),
]


@pytest.mark.parametrize(("info", "tab", "note"), CASES)
def test_choose_platform(info, tab, note):
    assert run_node(f"choosePlatform({json.dumps(info)})") == {"tab": tab, "note": note}


@pytest.mark.parametrize(
    ("data", "label"),
    [
        ({"tag_name": "v0.2.0"}, "v0.2.0"),
        ({"tag_name": "v1.10.3-rc.1"}, "v1.10.3-rc.1"),
        ({"message": "API rate limit exceeded"}, "latest release"),
        ({"tag_name": "<b>v1</b>"}, "latest release"),
        ({"tag_name": 2}, "latest release"),
        (None, "latest release"),
    ],
)
def test_release_label(data, label):
    assert run_node(f"releaseLabel({json.dumps(data)})") == label


class InstallBlocks(HTMLParser):
    """The command text of the <pre><code> in each element with data-platform."""

    def __init__(self):
        super().__init__()
        self.blocks: dict[str, str] = {}
        self.platform: str | None = None
        self.depth = 0
        self.in_pre = False
        self.in_code = False

    def handle_starttag(self, tag, attrs):
        if self.depth:
            self.depth += 1
        elif platform := dict(attrs).get("data-platform"):
            self.platform, self.depth = platform, 1
            self.blocks[platform] = ""
        if self.depth and tag == "pre":
            self.in_pre = True
        if self.in_pre and tag == "code":
            self.in_code = True

    def handle_endtag(self, tag):
        if tag == "code":
            self.in_code = False
        if tag == "pre":
            self.in_pre = False
        if self.depth:
            self.depth -= 1
            if not self.depth:
                self.platform = None

    def handle_data(self, data):
        if self.in_code and self.platform:
            self.blocks[self.platform] += data


def install_blocks() -> dict[str, str]:
    parser = InstallBlocks()
    parser.feed(INSTALL.read_text(encoding="utf-8"))
    return parser.blocks


def built_assets() -> set[str]:
    pairs = re.findall(r"goos:\s*(\w+)\s*\n\s*goarch:\s*(\w+)", BUILD.read_text(encoding="utf-8"))
    assert pairs, "no goos/goarch matrix in build.yml"
    return {f"lighthouse_{os}_{arch}" + (".exe" if os == "windows" else "") for os, arch in pairs}


def test_site_assets_are_the_built_assets():
    texts = [p.read_text(encoding="utf-8") for p in [INDEX, INSTALL, *SITE.glob("*.js")]]
    named = {a for t in texts for a in re.findall(re.escape(DOWNLOAD) + r"([\w.]+)", t)}
    assert named == built_assets() | {"SHA256SUMS"}


def test_every_platform_has_an_install_block():
    assert list(install_blocks()) == TABS


def test_checksum_comes_before_the_binary_is_used():
    for tab, block in install_blocks().items():
        lines = [l for l in block.strip().splitlines() if l.strip()]
        check = next(i for i, l in enumerate(lines) if any(c in l for c in CHECKS))
        use = next(i for i, l in enumerate(lines) if " mv " in f" {l}" or l.rstrip().endswith("version"))
        assert check < use, tab
        assert "| sh" not in block and "| bash" not in block, tab


def test_bash_blocks_stop_on_the_first_failure():
    for tab, block in install_blocks().items():
        if tab == "windows-amd64":
            # A paste that arrives line by line runs each finished line at once; inside one
            # `& { }` block PowerShell waits for the closing brace, so `throw` stops the rest.
            lines = [l.strip() for l in block.strip().splitlines() if l.strip()]
            assert (lines[0], lines[-1]) == ("& {", "}"), tab
            assert '$ErrorActionPreference = "Stop"' in block and "throw" in block, tab
            continue
        lines = [l.rstrip() for l in block.strip().splitlines() if l.strip()]
        assert all(l.endswith("&&") for l in lines[:-1]), tab
        assert lines[-1] == "~/.local/bin/lighthouse version", tab


def test_windows_block_works_in_windows_powershell():
    """Windows PowerShell 5.1, the default shell, needs -UseBasicParsing where the IE engine is
    missing, and crawls through a download while it draws the progress bar."""
    block = install_blocks()["windows-amd64"]
    calls = re.findall(r"Invoke-WebRequest[^\n]*", block)
    assert len(calls) == 2 and all("-UseBasicParsing" in c for c in calls)
    assert '$ProgressPreference = "SilentlyContinue"' in block


def test_install_page_works_without_js():
    text = INSTALL.read_text(encoding="utf-8")
    assert re.findall(r"<[^>]*\bhidden\b[^>]*>", text) == ['<p id="platform-note" hidden>']
    panels = re.findall(r"<(\w+)([^>]*\bdata-platform=[^>]*)>", text)
    assert [tag for tag, _ in panels] == ["details"] * len(TABS)
    assert not [attrs for _, attrs in panels if re.search(r"\bopen\b", attrs)]
    assert text.count("<summary>") == len(TABS)


def test_install_page_title():
    assert "<title>Install lighthouse</title>" in INSTALL.read_text(encoding="utf-8")


def test_footer_links():
    text = INDEX.read_text(encoding="utf-8")
    base = "https://github.com/CorneeldH/lighthouse/"
    for url in ["blob/main/docs/getting-started.md", "issues/88", "blob/main/LICENSE", "blob/main/CHANGELOG.md"]:
        assert f'href="{base}{url}"' in text, url
    assert "<title>Lighthouse</title>" in text


def test_landing_page_puts_the_why_first():
    text = INDEX.read_text(encoding="utf-8")
    assert "<pre" not in text
    assert re.search(r'<a\b[^>]*\bid="download"[^>]*\bhref="install.html"', text)
    assert re.search(r'<section\b[^>]*\bid="why"', text)
    assert re.search(r'<details\b[^>]*\bid="who"', text)
    order = [text.index(f'id="{i}"') for i in ["download", "why", "what", "who", "maps"]]
    assert order == sorted(order)


def test_old_download_link_redirects_to_the_install_page():
    text = (SITE / "index.js").read_text(encoding="utf-8")
    assert 'location.hash === "#download"' in text
    assert 'location.replace("install.html")' in text


def test_install_page_names_the_other_routes():
    parser = ElementById("install")
    parser.feed(INSTALL.read_text(encoding="utf-8"))
    assert parser.attrs is not None, 'install.html has no element with id="install"'
    text = " ".join(parser.text.split())
    assert "go install github.com/CorneeldH/lighthouse/cmd/lighthouse@latest" in text
    assert "clone" in text
