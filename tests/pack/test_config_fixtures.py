"""The shared fixtures of `lighthouse config`: `config.py` is the reference.

Each directory under `fixtures/config/` is one case: a `lighthouse.toml` and/or `host.toml`, and
an `expect.json` listing `{"args", "rc", "stdout", "stderr"}` per invocation (`stderr_prefix`
instead of `stderr` where tomllib and the Go TOML reader word a parse error differently).
`internal/config/parity_test.go` runs the same directories against the Go port, so a
fixture that passes both sides proves the two readers agree.

Both sides run with the cwd set to the case directory and relative `LIGHTHOUSE_TOML` and
`LIGHTHOUSE_HOST_TOML`, so the messages that name a file hold a relative path. A case without
a `host.toml` points `LIGHTHOUSE_HOST_TOML` at a path that does not exist.
"""

import json
import os
import subprocess
import sys
from pathlib import Path

import pytest

from lighthouse_lib import config as lib_config
from tests.pack._testkit import IMPORT_ROOT

FIXTURES = Path(__file__).resolve().parent / "fixtures" / "config"
CASES = sorted(p.name for p in FIXTURES.iterdir() if p.is_dir())


def _expectations(case: str) -> list[dict]:
    return json.loads((FIXTURES / case / "expect.json").read_text(encoding="utf-8"))


def _env() -> dict[str, str]:
    env = {k: v for k, v in os.environ.items() if not k.startswith("LIGHTHOUSE_")}
    env.update(
        PYTHONPATH=str(IMPORT_ROOT),
        LIGHTHOUSE_TOML="lighthouse.toml",
        LIGHTHOUSE_HOST_TOML="host.toml",
    )
    return env


@pytest.mark.parametrize("case", CASES)
def test_python_matches_fixtures(case):
    for expect in _expectations(case):
        result = subprocess.run(
            [sys.executable, "-m", "lighthouse_lib.config", *expect["args"]],
            cwd=FIXTURES / case,
            env=_env(),
            capture_output=True,
            text=True,
            check=False,
        )
        where = f"{case} {expect['args']}"
        assert result.returncode == expect["rc"], where
        assert result.stdout == expect["stdout"], where
        if "stderr_prefix" in expect:
            assert result.stderr.startswith(expect["stderr_prefix"]), where
        else:
            assert result.stderr == expect["stderr"], where


def test_fixtures_cover_every_key():
    covered = {a for case in CASES for expect in _expectations(case) for a in expect["args"][:1]}
    known = (
        set(lib_config.SCHEMA)
        | set(lib_config.DEFAULTS)
        | set(lib_config._BASE_KEYS)
        | set(lib_config.HOST_KEYS)
    )
    assert known - covered == set()
    for prefix in lib_config._OPTION_PREFIXES:
        assert any(key.startswith(prefix) for key in covered), prefix


def test_schema_snapshot_matches_config_py():
    """`config-schema.json` is what `internal/config` is compared against; it must say what
    `config.py` says, in the same order (the first bad key in SCHEMA order is the one reported)."""
    snapshot = json.loads((FIXTURES.parent / "config-schema.json").read_text(encoding="utf-8"))
    assert snapshot["schema"] == [[k, v] for k, v in lib_config.SCHEMA.items()]
    assert snapshot["defaults"] == {
        k: list(v) if isinstance(v, tuple) else v for k, v in lib_config.DEFAULTS.items()
    }
    assert snapshot["base_keys"] == list(lib_config._BASE_KEYS)
    assert snapshot["host_keys"] == list(lib_config.HOST_KEYS)
    assert snapshot["option_prefixes"] == list(lib_config._OPTION_PREFIXES)
