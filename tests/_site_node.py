"""Evaluate an expression against docs/site/map.js in node, for the site tests."""

import json
import shutil
import subprocess

from tests.test_site_chain import SITE

MAP_JS = SITE / "map.js"


def run_map(expression: str):
    """The JSON value of `expression`, evaluated with map.js imported as `m`."""
    node = shutil.which("node")
    assert node, "node is needed for the map tests (no silent skip)"
    script = (
        f"import * as m from {json.dumps(MAP_JS.as_uri())};\n"
        f"console.log(JSON.stringify({expression}));"
    )
    out = subprocess.run(
        [node, "--input-type=module", "-e", script],
        capture_output=True, text=True, check=True, timeout=30,
    )
    return json.loads(out.stdout)
