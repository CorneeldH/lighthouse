"""The shared fixtures of `lighthouse config` under `fixtures/config/`.

Each directory there is one case: a `lighthouse.toml` and/or `host.toml`, and an
`expect.json` listing `{"args", "rc", "stdout", "stderr"}` per invocation.
`internal/config/parity_test.go` runs them against the Go reader; this file only checks that
together they ask for every key the reader knows.
"""

import json
from pathlib import Path

from tests.pack._testkit import config_schema

FIXTURES = Path(__file__).resolve().parent / "fixtures" / "config"
CASES = sorted(p.name for p in FIXTURES.iterdir() if p.is_dir())


def _expectations(case: str) -> list[dict]:
    return json.loads((FIXTURES / case / "expect.json").read_text(encoding="utf-8"))


def test_fixtures_cover_every_key():
    schema = config_schema()
    covered = {a for case in CASES for expect in _expectations(case) for a in expect["args"][:1]}
    known = set(schema.SCHEMA) | set(schema.DEFAULTS) | set(schema.BASE_KEYS) | set(schema.HOST_KEYS)
    assert known - covered == set()
    for prefix in schema.OPTION_PREFIXES:
        assert any(key.startswith(prefix) for key in covered), prefix
