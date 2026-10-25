"""Every shell file a package installs is shellcheck-clean at the lowest severity.

A consumer may gate its hook directories at `style` (paia does, on `.claude/hooks/`), and
an installed file cannot be fixed there: `init --upgrade` refuses a local change. So the
package carries the strictest bar itself.
"""

import subprocess
from pathlib import Path

CHECKOUT = Path(__file__).resolve().parents[2]
SHELLCHECK_LIB = CHECKOUT / "pack" / "shell" / "scripts" / "lib" / "shellcheck-bin.sh"


def _pinned_shellcheck() -> str:
    """No pin is a failure, not a skip: the test would say nothing."""
    result = subprocess.run(
        ["bash", "-c", f'. "{SHELLCHECK_LIB}"\nresolve_shellcheck\n'],
        capture_output=True,
        text=True,
        cwd=str(CHECKOUT),
        check=False,
    )
    assert result.returncode == 0 and result.stderr == "", result.stderr
    return result.stdout.strip()


def test_package_shell_is_clean_at_style_severity(monkeypatch):
    monkeypatch.delenv("SHELLCHECK_BIN", raising=False)
    files = sorted(
        str(p.relative_to(CHECKOUT)) for p in (CHECKOUT / "pack").rglob("*.sh")
    )
    assert files
    result = subprocess.run(
        [_pinned_shellcheck(), "-x", "-S", "style", *files],
        capture_output=True,
        text=True,
        cwd=str(CHECKOUT),
        check=False,
    )
    assert result.returncode == 0, result.stdout + result.stderr
