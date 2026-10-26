"""`pack/core/lib/code_language_llm.py` (#37): layer 2, the model chain behind a hard deadline.

The endpoint is a local fake server (`_fake_llm`); the key, the marker file and the clock
are real.
"""

import time
from pathlib import Path

import pytest

from lighthouse_lib import config
from tests.pack._fake_llm import FakeEndpoint
from tests.pack._testkit import load_hook_lib

llm = load_hook_lib("code_language_llm")

ITEMS = [("name", "werkboom")]


def _cfg(tmp_path, body):
    path = tmp_path / "lighthouse.toml"
    path.write_text(body)
    return config.load(path)


def _chain(url, models=("m1",), key_env="LLM_TEST_KEY", env_file=None):
    return llm.Chain(url, tuple(models), key_env, env_file)


@pytest.fixture(autouse=True)
def _key(monkeypatch):
    monkeypatch.setenv("LLM_TEST_KEY", "sekrit")


def test_chain_from(tmp_path, monkeypatch):
    assert llm.chain_from(_cfg(tmp_path, "")) is None
    env = tmp_path / "env"
    env.write_text("K=1\n")
    monkeypatch.setenv("HOME", str(tmp_path))
    full = (
        '[code_language_llm]\nurl = "http://h:8099/v1"\nmodels = ["a", "b"]\n'
        'key_env = "K"\nenv_file = "~/env"\n'
    )
    chain = llm.chain_from(_cfg(tmp_path, full))
    assert chain == llm.Chain("http://h:8099/v1", ("a", "b"), "K", env)
    for body in (
        "[code_language_llm]\n",
        '[code_language_llm]\nmodels = ["a"]\n',
        '[code_language_llm]\nurl = "u"\n',
        '[code_language_llm]\nurl = "u"\nmodels = []\n',
        '[code_language_llm]\nurl = "u"\nmodels = ["a"]\nenv_file = "rel/env"\n',
        '[code_language_llm]\nurl = "u"\nmodels = ["a"]\nenv_file = "/no/such/env"\n',
    ):
        with pytest.raises(config.ConfigError):
            llm.chain_from(_cfg(tmp_path, body))
    bare = llm.chain_from(_cfg(tmp_path, '[code_language_llm]\nurl = "u"\nmodels = ["a"]\n'))
    assert bare.key_env is None and bare.env_file is None


def test_prompt_and_parse():
    prompt = llm.build_prompt([("name", "a"), ("comment", "x\ny")])
    assert "1. name: a" in prompt
    assert "2. comment: x y" in prompt
    assert "abbreviations and code jargon" in prompt.lower()
    assert "NOT-EN" in prompt and "JSON" in prompt
    assert llm.parse_answer('{"1":"EN","2":"NOT-EN"}', 2) == [False, True]
    assert llm.parse_answer('```json\n{"1":"EN","2":"NOT-EN"}\n```', 2) == [False, True]
    assert llm.parse_answer('Sure! {"1": "EN", "2": "NOT-EN"} Done.', 2) == [False, True]
    for bad in ('{"1":"EN"}', '{"1":"maybe","2":"EN"}', "nee"):
        with pytest.raises(ValueError):
            llm.parse_answer(bad, 2)


def test_answer_used(tmp_path):
    with FakeEndpoint(modes={"m1": "answer"}) as fake:
        reply = llm.ask(_chain(fake.url, ("m1", "m2")), ITEMS, state_dir=tmp_path)
    assert reply.not_english == [True]
    assert reply.model == "m1"
    assert reply.failures == []
    body = fake.requests[0]
    assert body["model"] == "m1" and body["temperature"] == 0
    assert "1. name: werkboom" in body["messages"][0]["content"]
    assert fake.headers[0]["Authorization"] == "Bearer sekrit"
    assert len(fake.requests) == 1


def test_max_tokens_leaves_room_to_reason(tmp_path):
    """A reasoning model spends tokens before its answer; the wall clock bounds the cost."""
    many = [("name", f"unit{i}") for i in range(100)]
    with FakeEndpoint() as fake:
        llm.ask(_chain(fake.url), ITEMS, state_dir=tmp_path)
        llm.ask(_chain(fake.url), many, state_dir=tmp_path)
    assert [r["max_tokens"] for r in fake.requests] == [512, 1220]


def test_key_from_env_file(tmp_path, monkeypatch):
    monkeypatch.delenv("LLM_TEST_KEY")
    env = tmp_path / "env"
    env.write_text('# comment\n\nOTHER=x\nLLM_TEST_KEY="abc"\n')
    with FakeEndpoint() as fake:
        reply = llm.ask(_chain(fake.url, env_file=env), ITEMS, state_dir=tmp_path)
    assert reply.not_english == [True]
    assert fake.headers[0]["Authorization"] == "Bearer abc"


def test_missing_key_fails_without_call(tmp_path, monkeypatch):
    monkeypatch.delenv("LLM_TEST_KEY")
    with FakeEndpoint() as fake:
        reply = llm.ask(_chain(fake.url), ITEMS, state_dir=tmp_path)
    assert reply.not_english is None
    assert len(reply.failures) == 1
    assert reply.failures[0].startswith("key: ") and "LLM_TEST_KEY" in reply.failures[0]
    assert fake.requests == []


def test_no_key_env_sends_no_authorization(tmp_path):
    with FakeEndpoint() as fake:
        reply = llm.ask(_chain(fake.url, key_env=None), ITEMS, state_dir=tmp_path)
    assert reply.not_english == [True]
    assert "Authorization" not in fake.headers[0]


def test_chain_falls_through(tmp_path):
    with FakeEndpoint(modes={"m1": "http500", "m2": "answer"}) as fake:
        reply = llm.ask(_chain(fake.url, ("m1", "m2")), ITEMS, state_dir=tmp_path)
    assert reply.model == "m2"
    assert reply.not_english == [True]
    assert reply.failures == ["m1: HTTP 500"]


@pytest.mark.parametrize("mode", ["silent", "drip", "http500", "garbage", "invalid"])
def test_every_failure_kind(tmp_path, mode):
    start = time.monotonic()
    if mode == "invalid":
        reply = llm.ask(
            _chain("http://lighthouse-test.invalid/v1"),
            ITEMS, state_dir=tmp_path, per_model=0.5, total=1.0,
        )  # fmt: skip
    else:
        with FakeEndpoint(modes={"m1": mode}) as fake:
            reply = llm.ask(
                _chain(fake.url), ITEMS, state_dir=tmp_path, per_model=0.5, total=1.0
            )
    assert time.monotonic() - start < 1.5
    assert reply.not_english is None and reply.model is None
    assert len(reply.failures) == 1 and reply.failures[0].startswith("m1: ")


def test_total_deadline(tmp_path):
    start = time.monotonic()
    with FakeEndpoint(default="silent") as fake:
        reply = llm.ask(
            _chain(fake.url, ("m1", "m2", "m3")),
            ITEMS, state_dir=tmp_path, per_model=0.5, total=0.8,
        )  # fmt: skip
    assert time.monotonic() - start < 1.2
    assert reply.not_english is None
    assert reply.failures[-1].startswith("m3: ") and "total time" in reply.failures[-1]
    assert len(fake.requests) == 2


def test_quiet_period(tmp_path):
    with FakeEndpoint(modes={"m1": "http500"}) as fake:
        chain = _chain(fake.url)
        assert llm.ask(chain, ITEMS, state_dir=tmp_path).not_english is None
        seen = len(fake.requests)
        again = llm.ask(chain, ITEMS, state_dir=tmp_path)
        assert again.not_english is None
        assert len(again.failures) == 1 and again.failures[0].startswith("skipped: ")
        assert len(fake.requests) == seen
    with FakeEndpoint() as other:  # another url is not skipped
        assert llm.ask(_chain(other.url), ITEMS, state_dir=tmp_path).not_english == [True]


def test_old_marker_is_not_quiet(tmp_path):
    with FakeEndpoint(modes={"m1": "http500"}) as fake:
        chain = _chain(fake.url)
        llm.ask(chain, ITEMS, state_dir=tmp_path)
        (marker,) = tmp_path.glob("layer2-failed-*")
        marker.write_text(str(time.time() - llm.QUIET_S - 5))
        fake.requests.clear()
        llm.ask(chain, ITEMS, state_dir=tmp_path)
        assert len(fake.requests) == 1


def test_marker_removed_after_success(tmp_path):
    with FakeEndpoint(modes={"m1": "http500"}) as fake:
        llm.ask(_chain(fake.url), ITEMS, state_dir=tmp_path)
    (marker,) = tmp_path.glob("layer2-failed-*")
    marker.write_text(str(time.time() - llm.QUIET_S - 5))
    with FakeEndpoint() as fake:
        marker.rename(llm._marker(tmp_path, fake.url))  # the marker of this url
        assert llm.ask(_chain(fake.url), ITEMS, state_dir=tmp_path).not_english == [True]
    assert list(tmp_path.glob("layer2-failed-*")) == []


def test_unusable_marker_is_ignored(tmp_path):
    blocker = tmp_path / "file"
    blocker.write_text("x")
    with FakeEndpoint() as fake:
        reply = llm.ask(_chain(fake.url), ITEMS, state_dir=blocker / "sub")
    assert reply.not_english == [True]
    with FakeEndpoint(modes={"m1": "http500"}) as fake:
        reply = llm.ask(_chain(fake.url), ITEMS, state_dir=blocker / "sub")
        assert reply.failures == ["m1: HTTP 500"]
        llm._marker(tmp_path, fake.url).write_text("x")
        reply = llm.ask(_chain(fake.url), ITEMS, state_dir=tmp_path)
        assert reply.failures == ["m1: HTTP 500"]
        assert len(fake.requests) == 2


def test_default_state_dir(monkeypatch):
    monkeypatch.setattr(llm.os, "getuid", lambda: 4242)
    monkeypatch.setenv("XDG_RUNTIME_DIR", "/run/user/1")
    assert llm.default_state_dir() == Path("/run/user/1/lighthouse-code-language-4242")
    monkeypatch.delenv("XDG_RUNTIME_DIR")
    assert llm.default_state_dir() == Path("/tmp/lighthouse-code-language-4242")


def test_new_state_dir_is_private(tmp_path):
    state = tmp_path / "state"
    with FakeEndpoint(modes={"m1": "http500"}) as fake:
        llm.ask(_chain(fake.url), ITEMS, state_dir=state)
    assert state.stat().st_mode & 0o777 == 0o700
    (marker,) = state.glob("layer2-failed-*")
    assert marker.stat().st_mode & 0o777 == 0o600


def test_symlinked_marker_is_not_followed(tmp_path):
    """Another user's symlink in the state dir must not truncate the victim's file (R8)."""
    state = tmp_path / "state"
    state.mkdir(mode=0o700)
    victim = tmp_path / "victim"
    victim.write_text("precious")
    with FakeEndpoint(modes={"m1": "http500"}) as fake:
        llm._marker(state, fake.url).symlink_to(victim)
        reply = llm.ask(_chain(fake.url), ITEMS, state_dir=state)
    assert reply.failures == ["m1: HTTP 500"]
    assert victim.read_text() == "precious"


def test_shared_state_dir_is_not_used(tmp_path):
    """A group- or world-writable state dir is neither read nor written (R8)."""
    state = tmp_path / "state"
    state.mkdir()
    state.chmod(0o777)
    with FakeEndpoint(modes={"m1": "http500"}) as fake:
        llm.ask(_chain(fake.url), ITEMS, state_dir=state)
        assert list(state.iterdir()) == []
        llm._marker(state, fake.url).write_text(str(time.time()))
        again = llm.ask(_chain(fake.url), ITEMS, state_dir=state)
    assert again.failures == ["m1: HTTP 500"]
    assert len(fake.requests) == 2
