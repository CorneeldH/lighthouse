"""`lighthouse_lib.config`: the config reader for hooks, scripts and skills.

The cases for `[forge]`, `[forge.board]` and `host.toml` follow the identity and
host readers of the source repository. The difference (besluit 16): the board is
optional, and the status option `Controller` is required only with `[controller]`.
"""

import errno
import os
import socket
import subprocess
import sys
from pathlib import Path
from unittest.mock import patch

import pytest

from lighthouse_lib import config as cfgmod
from lighthouse_lib.config import (
    REQUIRED_STATUS_OPTIONS,
    ConfigError,
    is_local,
    load,
    load_host,
    lookup,
    main,
)
from tests.pack._testkit import IMPORT_ROOT, INSTALL_ROOT, init_repo

VALID: dict = {
    "forge": {
        "kind": "github",
        "repo": "Acme/widgets",
        "board": {
            "project_number": 3,
            "project_id": "PVT_acme",
            "status_field_id": "FIELD_acme",
            "status_options": {
                "To do": "a",
                "In progress": "b",
                "Done": "c",
                "On hold": "d",
            },
        },
    },
}


def _scalar(value) -> str:
    return str(value) if isinstance(value, int) else '"' + str(value) + '"'


def _render(data: dict, prefix: str = "") -> str:
    """Mini TOML writer for this schema: scalars first, then the subtables."""
    lines = []
    if prefix:
        lines.append(f"[{prefix}]")
    for key, value in data.items():
        if not isinstance(value, dict):
            lines.append(f'"{key}" = {_scalar(value)}')
    text = "\n".join(lines) + "\n"
    for key, value in data.items():
        if isinstance(value, dict):
            text += "\n" + _render(value, f"{prefix}.{key}" if prefix else key)
    return text


def _copy(data: dict) -> dict:
    return {k: _copy(v) if isinstance(v, dict) else v for k, v in data.items()}


def _without(path: tuple[str, ...]) -> dict:
    data = _copy(VALID)
    node = data
    for part in path[:-1]:
        node = node[part]
    node.pop(path[-1], None)
    return data


def _with(path: tuple[str, ...], value) -> dict:
    data = _without(path)
    node = data
    for part in path[:-1]:
        node = node.setdefault(part, {})
    node[path[-1]] = value
    return data


def _write(tmp_path: Path, data: dict) -> Path:
    path = tmp_path / "lighthouse.toml"
    path.write_text(_render(data), encoding="utf-8")
    return path


@pytest.fixture
def cfg_env(tmp_path, monkeypatch):
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(_write(tmp_path, VALID)))


@pytest.fixture
def cfg_env_without_board(tmp_path, monkeypatch):
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(_write(tmp_path, _without(("forge", "board")))))


# --- lighthouse.toml ---------------------------------------------------------


def test_valid_baseline_loads(tmp_path):
    cfg = load(_write(tmp_path, VALID))
    assert cfg.repo == "Acme/widgets"
    assert (cfg.owner, cfg.name) == ("Acme", "widgets")
    assert cfg.board is not None and cfg.board.project_number == 3
    assert set(REQUIRED_STATUS_OPTIONS) <= set(cfg.board.status_options)


def test_load_rejects_missing_file(tmp_path):
    path = tmp_path / "lighthouse.toml"
    with pytest.raises(ConfigError) as exc:
        load(path)
    assert str(path) in str(exc.value)


def test_load_rejects_invalid_toml(tmp_path):
    path = tmp_path / "lighthouse.toml"
    path.write_text("[forge\n", encoding="utf-8")
    with pytest.raises(ConfigError) as exc:
        load(path)
    assert str(path) in str(exc.value)


def test_load_rejects_invalid_utf8(tmp_path):
    path = tmp_path / "lighthouse.toml"
    path.write_bytes(b'[forge]\nkind = "\xff"\n')
    with pytest.raises(ConfigError) as exc:
        load(path)
    assert str(path) in str(exc.value)


@pytest.mark.parametrize(
    ("mutation", "needle"),
    [
        (_with(("forge", "kind"), "gitlab"), "kind"),
        (_with(("forge", "repo"), "no-slash"), "repo"),
        (_with(("forge", "repo"), "a/b/c"), "repo"),
        (_with(("forge", "repo"), "/name"), "repo"),
        (_with(("forge", "repo"), 'a"b/c'), "repo"),
        (_with(("forge", "repo"), "a/<x>"), "repo"),
        (_without(("forge", "board", "status_options", "Done")), "Done"),
        (_with(("forge", "board", "status_options", "To do"), ""), "To do"),
    ],
    ids=[
        "kind-gitlab",
        "repo-without-slash",
        "repo-too-many-parts",
        "repo-empty-owner",
        "repo-with-quote",
        "repo-with-markup",
        "status-option-missing",
        "status-option-empty",
    ],
)
def test_load_rejects(tmp_path, mutation, needle):
    path = _write(tmp_path, mutation)
    with pytest.raises(ConfigError) as exc:
        load(path)
    assert needle in str(exc.value)
    assert str(path) in str(exc.value)


@pytest.mark.parametrize(
    "missing",
    [
        ("forge", "board", "project_number"),
        ("forge", "board", "project_id"),
        ("forge", "board", "status_field_id"),
        ("forge", "board", "status_options"),
    ],
    ids=lambda p: ".".join(p),
)
def test_load_rejects_missing_key(tmp_path, missing):
    path = _write(tmp_path, _without(missing))
    with pytest.raises(ConfigError) as exc:
        load(path)
    assert str(path) in str(exc.value)
    assert missing[-1] in str(exc.value)


def test_load_rejects_wrong_type(tmp_path):
    path = _write(tmp_path, _with(("forge", "board", "project_number"), "one"))
    with pytest.raises(ConfigError) as exc:
        load(path)
    assert "project_number" in str(exc.value)


def test_load_rejects_bool_as_int(tmp_path):
    path = tmp_path / "lighthouse.toml"
    path.write_text(_render(VALID).replace("project_number\" = 3", "project_number\" = true"))
    with pytest.raises(ConfigError, match="project_number"):
        load(path)


def test_priority_optional(tmp_path):
    board = load(_write(tmp_path, VALID)).board
    assert board.priority_field_id is None
    assert dict(board.priority_options) == {}


def test_priority_loaded_when_present(tmp_path):
    data = _with(("forge", "board", "priority_field_id"), "PRIO")
    data["forge"]["board"]["priority_options"] = {"P0": "x", "P1": "y"}
    board = load(_write(tmp_path, data)).board
    assert board.priority_field_id == "PRIO"
    assert set(board.priority_options) == {"P0", "P1"}


def test_extra_status_options_allowed(tmp_path):
    data = _with(("forge", "board", "status_options", "In review"), "x")
    assert "In review" in load(_write(tmp_path, data)).board.status_options


def test_board_is_optional(tmp_path):
    assert load(_write(tmp_path, _without(("forge", "board")))).board is None


def test_controller_option_only_required_with_controller_table(tmp_path):
    load(_write(tmp_path, VALID))  # no [controller], no "Controller": valid
    data = _with(("controller",), {"state_dir": "x"})
    with pytest.raises(ConfigError, match="Controller"):
        load(_write(tmp_path, data))


def test_controller_table_is_not_validated(tmp_path):
    data = _with(("forge", "board", "status_options", "Controller"), "e")
    data["controller"] = {"plekken": "acht"}  # the source repo's table: only its presence counts
    load(_write(tmp_path, data))


@pytest.mark.parametrize("value", ['"de"', '""', "1"])
def test_language_must_be_nl_or_en(tmp_path, value):
    path = tmp_path / "lighthouse.toml"
    path.write_text(_render(VALID) + f"\n[project]\nlanguage = {value}\n")
    with pytest.raises(ConfigError, match="project.language"):
        load(path)


def test_language_defaults_to_en(tmp_path):
    assert load(_write(tmp_path, VALID)).language == "en"


def test_language_nl(tmp_path):
    path = tmp_path / "lighthouse.toml"
    path.write_text(_render(VALID) + '\n[project]\nlanguage = "nl"\n')
    assert load(path).language == "nl"


def test_lookup_walks_raw_and_derived_keys(tmp_path):
    cfg = load(_write(tmp_path, VALID))
    assert lookup(cfg, "forge.kind") == "github"
    assert lookup(cfg, "forge.name") == "widgets"
    assert lookup(cfg, "forge.board.project_number") == 3
    assert lookup(cfg, "forge.board.status_options.In progress") == "b"
    with pytest.raises(KeyError):
        lookup(cfg, "labels.bug")


# --- paths ---------------------------------------------------------------------


def test_config_path_override(monkeypatch, tmp_path):
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(tmp_path / "x.toml"))
    assert cfgmod.config_path() == tmp_path / "x.toml"


def test_repo_root_from_env(monkeypatch, tmp_path):
    monkeypatch.setenv("LIGHTHOUSE_REPO_ROOT", str(tmp_path))
    assert cfgmod.repo_root() == tmp_path


def test_repo_root_from_git(monkeypatch, tmp_path):
    repo = tmp_path / "repo"
    init_repo(repo)
    (repo / "sub").mkdir()
    monkeypatch.delenv("LIGHTHOUSE_REPO_ROOT", raising=False)
    monkeypatch.delenv("LIGHTHOUSE_TOML", raising=False)
    monkeypatch.chdir(repo / "sub")
    assert cfgmod.repo_root() == repo.resolve()
    assert cfgmod.config_path() == repo.resolve() / "lighthouse.toml"


def test_repo_root_outside_git_is_config_error(monkeypatch, tmp_path):
    monkeypatch.delenv("LIGHTHOUSE_REPO_ROOT", raising=False)
    monkeypatch.chdir(tmp_path)
    monkeypatch.setenv("GIT_CEILING_DIRECTORIES", str(tmp_path.parent))
    with pytest.raises(ConfigError, match="repository root"):
        cfgmod.repo_root()


def test_host_path_follows_home(monkeypatch, tmp_path):
    monkeypatch.delenv("LIGHTHOUSE_HOST_TOML", raising=False)
    monkeypatch.setenv("HOME", str(tmp_path))
    assert cfgmod.host_path() == tmp_path / ".config/lighthouse/host.toml"


# --- host.toml -----------------------------------------------------------------


def _host(tmp_path: Path, text: str) -> Path:
    path = tmp_path / "host.toml"
    path.write_text(text, encoding="utf-8")
    return path


def test_load_host_valid(tmp_path):
    host = load_host(_host(tmp_path, '[host]\nname = "box"\ntailscale_ip = "100.64.0.1"\n'))
    assert (host.name, host.tailscale_ip) == ("box", "100.64.0.1")


def test_load_host_missing_file(tmp_path):
    with pytest.raises(ConfigError, match="host.toml"):
        load_host(tmp_path / "host.toml")


@pytest.mark.parametrize(
    ("text", "needle"),
    [
        ('[host]\ntailscale_ip = "100.64.0.1"\n', "missing key host.name"),
        ('[host]\nname = "box"\n', "missing key host.tailscale_ip"),
        ('[other]\nname = "box"\n', "missing key host"),
    ],
    ids=["name", "tailscale_ip", "host"],
)
def test_load_host_missing_key(tmp_path, text, needle):
    path = _host(tmp_path, text)
    with pytest.raises(ConfigError) as exc:
        load_host(path)
    assert str(exc.value) == f"{path}: {needle}"


@pytest.mark.parametrize(
    "value",
    ['"100.64.0"', '"::1"', '""', "42", '"0.0.0.0"', '"127.0.0.1"', '"224.0.0.1"'],
)
def test_load_host_rejects_invalid_ip(tmp_path, value):
    path = _host(tmp_path, f'[host]\nname = "box"\ntailscale_ip = {value}\n')
    with pytest.raises(ConfigError) as exc:
        load_host(path)
    assert str(path) in str(exc.value)
    assert "host.tailscale_ip" in str(exc.value)


def test_load_host_rejects_empty_name(tmp_path):
    path = _host(tmp_path, '[host]\nname = ""\ntailscale_ip = "100.64.0.1"\n')
    with pytest.raises(ConfigError, match="host.name"):
        load_host(path)


def test_is_local_loopback_true():
    assert is_local("127.0.0.1") is True


def test_is_local_testnet_false():
    # TEST-NET-1 (RFC 5737) is never on an interface.
    assert is_local("192.0.2.1") is False


def test_is_local_socket_creation_error_is_config_error():
    with (
        patch.object(cfgmod.socket, "socket", side_effect=OSError(errno.EMFILE, "too many")),
        pytest.raises(ConfigError, match="not measurable"),
    ):
        is_local("192.0.2.1")


def test_is_local_other_oserror_is_config_error():
    with (
        patch.object(socket.socket, "bind", side_effect=OSError(errno.EACCES, "no")),
        pytest.raises(ConfigError, match="not measurable"),
    ):
        is_local("127.0.0.1")


# --- CLI -------------------------------------------------------------------------


def test_cli_prints_value(cfg_env, capsys):
    assert main(["forge.repo"]) == 0
    assert capsys.readouterr().out == "Acme/widgets\n"
    assert main(["forge.owner"]) == 0
    assert capsys.readouterr().out == "Acme\n"
    assert main(["forge.board.status_options.In progress"]) == 0
    assert capsys.readouterr().out == "b\n"
    assert main(["forge.board.project_number"]) == 0
    assert capsys.readouterr().out == "3\n"


def test_cli_absent_key_is_rc_1(cfg_env_without_board, capsys):
    assert main(["forge.board.project_id"]) == 1
    assert "not set" in capsys.readouterr().err


def test_cli_table_is_rc_2(cfg_env, capsys):
    assert main(["forge.board"]) == 2
    assert "table" in capsys.readouterr().err


def test_cli_broken_config_is_rc_2_in_process(tmp_path, monkeypatch, capsys):
    path = tmp_path / "lighthouse.toml"
    path.write_text("[forge\n")
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(path))
    assert main(["forge.repo"]) == 2
    assert capsys.readouterr().err.startswith("config: ")


def test_cli_broken_config_is_rc_2_without_traceback(tmp_path):
    path = tmp_path / "lighthouse.toml"
    path.write_text("[forge\n")
    result = subprocess.run(
        [sys.executable, "-m", "lighthouse_lib.config", "forge.repo"],
        capture_output=True,
        text=True,
        env=dict(os.environ, LIGHTHOUSE_TOML=str(path), PYTHONPATH=str(IMPORT_ROOT)),
        check=False,
    )
    assert result.returncode == 2, result.stderr
    assert "Traceback" not in result.stderr
    assert str(path) in result.stderr


def test_missing_tomllib_is_rc_2(monkeypatch, cfg_env, capsys):
    monkeypatch.setitem(sys.modules, "tomllib", None)
    assert main(["forge.repo"]) == 2
    assert "3.11" in capsys.readouterr().err


def test_cli_host_key(tmp_path, monkeypatch, capsys):
    monkeypatch.setenv(
        "LIGHTHOUSE_HOST_TOML",
        str(_host(tmp_path, '[host]\nname = "box"\ntailscale_ip = "100.64.0.1"\n')),
    )
    assert main(["host.name"]) == 0
    assert capsys.readouterr().out == "box\n"


@pytest.mark.parametrize(
    ("ip", "rc"),
    [("127.0.0.1", 0), ("192.0.2.1", 1)],
    ids=["recognized", "not-local"],
)
def test_host_check_rc(tmp_path, monkeypatch, capsys, ip, rc):
    # 127.0.0.1 is rejected by load_host; test recognition through is_local instead.
    monkeypatch.setenv(
        "LIGHTHOUSE_HOST_TOML",
        str(_host(tmp_path, '[host]\nname = "box"\ntailscale_ip = "100.64.0.1"\n')),
    )
    with patch.object(cfgmod, "is_local", return_value=(rc == 0)):
        assert main(["host-check"]) == rc
    captured = capsys.readouterr()
    assert ("host: box" in captured.out) if rc == 0 else ("not on this machine" in captured.err)


def test_host_check_missing_is_rc_2(tmp_path, monkeypatch, capsys):
    monkeypatch.setenv("LIGHTHOUSE_HOST_TOML", str(tmp_path / "host.toml"))
    assert main(["host-check"]) == 2


# --- defaults, types and the optional [forge] (taak 6) ---------------------------


@pytest.mark.parametrize(
    ("key", "value"),
    [
        ("paths.specs", "docs/developer/planning/specs"),
        ("paths.plans", "docs/developer/planning/plans"),
        ("test_gate.slots", "2"),
        ("test_gate.deadline_seconds", "270"),
        ("ci.runs_on", "ubuntu-latest"),
        ("codex_usage.review_pct_week", "95"),
        ("forge.default_branch", "main"),
        ("labels.blocked_issue", "blocked-issue"),
        ("project.instructions_word_ceiling", "3000"),
        ("project.language", "en"),
    ],
)
def test_defaults_for_absent_keys(cfg_env, capsys, key, value):
    assert main([key]) == 0
    assert capsys.readouterr().out == value + "\n"


def test_set_value_wins_over_default(tmp_path, monkeypatch, capsys):
    path = tmp_path / "lighthouse.toml"
    path.write_text(_render(VALID).replace('"repo"', '"default_branch" = "trunk"\n"repo"'))
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(path))
    assert main(["forge.default_branch"]) == 0
    assert capsys.readouterr().out == "trunk\n"


@pytest.mark.parametrize(
    "key", ["ci.bot_login", "ci.bot_client_id_var", "test_gate.command", "paths.labels"]
)
def test_keys_without_default_are_rc_1(cfg_env, capsys, key):
    assert main([key]) == 1
    assert "not set" in capsys.readouterr().err


@pytest.mark.parametrize(
    ("table", "line"),
    [
        ("test_gate", 'slots = "2"'),
        ("test_gate", "slots = true"),
        ("paths", 'search_dirs = "src"'),
        ("test_gate", 'unchecked_exit_codes = ["3"]'),
        ("ci", "runs_on = 1"),
        ("paths", 'labels = ""'),
        ("project", "instructions_word_ceiling = 3.5"),
    ],
)
def test_wrong_type_is_rc_2(tmp_path, monkeypatch, capsys, table, line):
    path = tmp_path / "lighthouse.toml"
    path.write_text(_render(VALID) + f"\n[{table}]\n{line}\n")
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(path))
    assert main([f"{table}.{line.split()[0]}"]) == 2
    err = capsys.readouterr().err
    assert str(path) in err and f"{table}.{line.split()[0]}" in err


def test_wrong_default_branch_type_is_rc_2(tmp_path, monkeypatch, capsys):
    path = tmp_path / "lighthouse.toml"
    path.write_text(_render(VALID).replace('"repo"', '"default_branch" = 1\n"repo"'))
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(path))
    assert main(["forge.repo"]) == 2
    assert "forge.default_branch" in capsys.readouterr().err


def test_list_value_prints_one_per_line(tmp_path, monkeypatch, capsys):
    path = tmp_path / "lighthouse.toml"
    path.write_text(_render(VALID) + '\n[paths]\nsearch_dirs = ["src", "lib"]\n')
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(path))
    assert main(["paths.search_dirs"]) == 0
    assert capsys.readouterr().out == "src\nlib\n"


def test_empty_list_default_prints_nothing(cfg_env, capsys):
    assert main(["test_gate.unchecked_exit_codes"]) == 0
    assert capsys.readouterr().out == ""


def test_lookup_returns_default(tmp_path):
    cfg = load(_write(tmp_path, VALID))
    assert lookup(cfg, "paths.search_dirs") == ["."]
    assert lookup(cfg, "test_gate.unchecked_exit_codes") == []


def test_defaults_are_not_shared(tmp_path):
    cfg = load(_write(tmp_path, VALID))
    lookup(cfg, "paths.search_dirs").append("x")
    assert lookup(cfg, "paths.search_dirs") == ["."]


@pytest.mark.parametrize(
    ("key", "known"),
    [
        ("forge.repo", True),
        ("forge.owner", True),
        ("forge.default_branch", True),
        ("forge.board.status_options.In progress", True),
        ("forge.board.priority_options.P0", True),
        ("host.name", True),
        ("host.gate_slice", True),
        ("paths.labels", True),
        ("ci.bot_login", True),
        ("project.skill_prefix", True),
        ("project.milestone_prefix", True),
        ("paths.label_taxonomy", False),
        ("test_gate.slice", False),
        ("host.other", False),
        ("forge.board", False),
        ("labels.bug", False),
    ],
)
def test_is_known_key(key, known):
    assert cfgmod.is_known_key(key) is known


def _without_forge(tmp_path, monkeypatch) -> Path:
    path = tmp_path / "lighthouse.toml"
    path.write_text('[project]\nlanguage = "nl"\n')
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(path))
    return path


@pytest.mark.parametrize("key", ["forge.repo", "forge.owner", "forge.name", "forge.kind"])
def test_without_forge_table_forge_keys_are_rc_1(tmp_path, monkeypatch, capsys, key):
    _without_forge(tmp_path, monkeypatch)
    assert main([key]) == 1
    assert "not set" in capsys.readouterr().err


@pytest.mark.parametrize(
    ("key", "value"),
    [("test_gate.slots", "2"), ("forge.default_branch", "main"), ("project.language", "nl")],
)
def test_without_forge_table_rest_works(tmp_path, monkeypatch, capsys, key, value):
    _without_forge(tmp_path, monkeypatch)
    assert main([key]) == 0
    assert capsys.readouterr().out == value + "\n"


def test_without_forge_table_load(tmp_path, monkeypatch):
    cfg = load(_without_forge(tmp_path, monkeypatch))
    assert (cfg.repo, cfg.board, cfg.language) == (None, None, "nl")


def test_broken_forge_is_rc_2(tmp_path, monkeypatch, capsys):
    path = tmp_path / "lighthouse.toml"
    path.write_text('[forge]\nkind = "gitlab"\n')
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(path))
    assert main(["test_gate.slots"]) == 2
    assert "forge.kind" in capsys.readouterr().err


def _forge_only_branch(tmp_path, monkeypatch) -> Path:
    path = tmp_path / "lighthouse.toml"
    path.write_text('[forge]\ndefault_branch = "master"\n')
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(path))
    return path


@pytest.mark.parametrize(
    ("key", "rc", "out"),
    [
        ("forge.default_branch", 0, "master\n"),
        ("paths.specs", 0, "docs/developer/planning/specs\n"),
        ("forge.repo", 1, ""),
        ("forge.owner", 1, ""),
        ("forge.kind", 1, ""),
    ],
)
def test_forge_with_only_default_branch(tmp_path, monkeypatch, capsys, key, rc, out):
    # A repo without a GitHub remote on `master` (review PR 2, minor 2).
    _forge_only_branch(tmp_path, monkeypatch)
    assert main([key]) == rc
    assert capsys.readouterr().out == out


def test_forge_with_only_default_branch_load(tmp_path, monkeypatch):
    cfg = load(_forge_only_branch(tmp_path, monkeypatch))
    assert (cfg.kind, cfg.repo, cfg.board) == (None, None, None)


def test_gate_slice_from_host(tmp_path, monkeypatch, capsys):
    monkeypatch.setenv(
        "LIGHTHOUSE_HOST_TOML",
        str(
            _host(
                tmp_path,
                '[host]\nname = "box"\ntailscale_ip = "100.64.0.1"\ngate_slice = "x.slice"\n',
            )
        ),
    )
    assert main(["host.gate_slice"]) == 0
    assert capsys.readouterr().out == "x.slice\n"


def test_gate_slice_absent_is_rc_1(tmp_path, monkeypatch, capsys):
    monkeypatch.setenv(
        "LIGHTHOUSE_HOST_TOML",
        str(_host(tmp_path, '[host]\nname = "box"\ntailscale_ip = "100.64.0.1"\n')),
    )
    assert main(["host.gate_slice"]) == 1
    assert "not set" in capsys.readouterr().err


def test_gate_slice_without_host_toml_is_rc_1(tmp_path, monkeypatch, capsys):
    monkeypatch.setenv("LIGHTHOUSE_HOST_TOML", str(tmp_path / "host.toml"))
    assert main(["host.gate_slice"]) == 1
    assert "not set" in capsys.readouterr().err


def test_host_name_without_host_toml_stays_rc_2(tmp_path, monkeypatch):
    monkeypatch.setenv("LIGHTHOUSE_HOST_TOML", str(tmp_path / "host.toml"))
    assert main(["host.name"]) == 2


@pytest.mark.parametrize("text", ["[host\n", '[host]\nname = "box"\ntailscale_ip = "100.64.0.1"\ngate_slice = 1\n'])
def test_gate_slice_broken_host_toml_is_rc_2(tmp_path, monkeypatch, text):
    monkeypatch.setenv("LIGHTHOUSE_HOST_TOML", str(_host(tmp_path, text)))
    assert main(["host.gate_slice"]) == 2


def test_runs_as_file(tmp_path):
    other = tmp_path / "other"
    init_repo(other)
    path = _write(tmp_path, VALID)
    env = {k: v for k, v in os.environ.items() if not k.startswith("PYTHON")}
    env.pop("LIGHTHOUSE_REPO_ROOT", None)
    env["LIGHTHOUSE_TOML"] = str(path)
    result = subprocess.run(
        [sys.executable, str(INSTALL_ROOT / ".lighthouse/lib/lighthouse_lib/config.py"), "forge.repo"],
        capture_output=True,
        text=True,
        cwd=str(other),
        env=env,
        check=False,
    )
    assert (result.returncode, result.stdout) == (0, "Acme/widgets\n"), result.stderr
