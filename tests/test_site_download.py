"""docs/site/platform.js picks the install tab the visitor sees first, and the release label
falls back to text when the API gives nothing usable."""

import json
import shutil
import subprocess

import pytest

from tests.test_site_chain import SITE

PLATFORM_JS = SITE / "platform.js"


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
    ({"platform": "Linux armv8l", "userAgent": UA["android"], "architecture": ""}, None, None),
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
