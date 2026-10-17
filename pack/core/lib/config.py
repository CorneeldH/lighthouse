"""The lighthouse config reader: `lighthouse.toml` in the repo, `host.toml` on the host.

One reader for hooks, scripts and skills. It validates loudly: a configuration
error names the file and the key, and the CLI exits with rc 2. The board
(`[forge.board]`) is optional; the status option `Controller` is required only
when a `[controller]` table exists. Any other table passes through unvalidated.

Paths: `lighthouse.toml` in the repo root (`LIGHTHOUSE_REPO_ROOT`, otherwise
`git rev-parse --show-toplevel`), overridden by `LIGHTHOUSE_TOML`;
`~/.config/lighthouse/host.toml`, overridden by `LIGHTHOUSE_HOST_TOML`.

CLI, with `.lighthouse/lib` on `PYTHONPATH`:

    python3 -m lighthouse_lib.config <key>     rc 0 value, rc 1 not set, rc 2 config error
    python3 -m lighthouse_lib.config host-check rc 0 this host, rc 1 another host, rc 2 error

A key is a dotted path into `lighthouse.toml` (`forge.repo`,
`forge.board.status_options.In progress`), one of the derived keys
`forge.owner` and `forge.name`, or `host.<key>` from `host.toml`.

The syntax stays within Python 3.9, like the rest of the lib; reading TOML
needs `tomllib`, so Python 3.11 or newer.
"""

from __future__ import annotations

import errno
import ipaddress
import os
import re
import socket
import subprocess
import sys
from dataclasses import dataclass, field
from pathlib import Path
from types import MappingProxyType
from typing import Any, Mapping

# The statuses the skills name; the board maps each to an option ID.
REQUIRED_STATUS_OPTIONS = ("To do", "In progress", "Done", "On hold")
# Required only when the repo has a `[controller]` table (besluit 16).
CONTROLLER_STATUS_OPTION = "Controller"
LANGUAGES = ("nl", "en")

# The characters GitHub allows in owner and name. A quote or `<` never reaches a
# URL or a regex this way.
_REPO = re.compile(r"[A-Za-z0-9._-]+/[A-Za-z0-9._-]+")


class ConfigError(Exception):
    """A configuration error; the text always names the file."""


def _empty() -> Mapping[str, str]:
    return MappingProxyType({})


@dataclass(frozen=True)
class Board:
    project_number: int
    project_id: str
    status_field_id: str
    status_options: Mapping[str, str]
    priority_field_id: str | None = None
    priority_options: Mapping[str, str] = field(default_factory=_empty)


@dataclass(frozen=True)
class Config:
    kind: str
    repo: str
    language: str
    board: Board | None
    raw: Mapping[str, Any]

    @property
    def owner(self) -> str:
        return self.repo.split("/", 1)[0]

    @property
    def name(self) -> str:
        return self.repo.split("/", 1)[1]


@dataclass(frozen=True)
class Host:
    name: str
    tailscale_ip: str


def repo_root() -> Path:
    override = os.environ.get("LIGHTHOUSE_REPO_ROOT")
    if override:
        return Path(override)
    try:
        result = subprocess.run(
            ["git", "rev-parse", "--show-toplevel"],
            capture_output=True,
            text=True,
            check=False,
        )
    except OSError as exc:
        raise ConfigError(f"repository root not found: git did not run: {exc}") from exc
    if result.returncode != 0 or not result.stdout.strip():
        raise ConfigError(
            f"repository root not found: {result.stderr.strip() or 'git rev-parse failed'}"
        )
    return Path(result.stdout.strip())


def config_path() -> Path:
    override = os.environ.get("LIGHTHOUSE_TOML")
    if override:
        return Path(override)
    return repo_root() / "lighthouse.toml"


def host_path() -> Path:
    override = os.environ.get("LIGHTHOUSE_HOST_TOML")
    if override:
        return Path(override)
    return Path.home() / ".config/lighthouse/host.toml"


def _read_toml(path: Path, missing: str) -> dict[str, Any]:
    try:
        import tomllib
    except ImportError as exc:
        raise ConfigError(
            f"{path}: python3 >= 3.11 is required (tomllib), this is {sys.version.split()[0]}"
        ) from exc
    try:
        with path.open("rb") as handle:
            return tomllib.load(handle)
    except FileNotFoundError as exc:
        raise ConfigError(missing) from exc
    except (OSError, UnicodeDecodeError, tomllib.TOMLDecodeError) as exc:
        raise ConfigError(f"{path}: unreadable or not valid TOML: {exc}") from exc


def _get(table: Mapping[str, Any], key: str, kind: type, where: str, path: Path) -> Any:
    """One required key of the expected type, or a `ConfigError` naming file and key."""
    if key not in table:
        raise ConfigError(f"{path}: missing key {where}{key}")
    value = table[key]
    if not isinstance(value, kind) or (kind is int and isinstance(value, bool)):
        raise ConfigError(f"{path}: key {where}{key} must be of type {kind.__name__}")
    if kind is str and not value:
        raise ConfigError(f"{path}: key {where}{key} is empty")
    return value


def _options(table: Mapping[str, Any], where: str, path: Path) -> dict[str, str]:
    for name, option_id in table.items():
        if not isinstance(option_id, str) or not option_id:
            raise ConfigError(f"{path}: option {where}{name} must be a non-empty string")
    return dict(table)


def _board(board: Mapping[str, Any], needs_controller: bool, path: Path) -> Board:
    where = "forge.board."
    status_options = _options(
        _get(board, "status_options", dict, where, path), where + "status_options.", path
    )
    required = REQUIRED_STATUS_OPTIONS + (
        (CONTROLLER_STATUS_OPTION,) if needs_controller else ()
    )
    for name in required:
        if name not in status_options:
            raise ConfigError(
                f"{path}: missing status option {name!r} in forge.board.status_options"
            )
    priority_field_id = None
    if "priority_field_id" in board:
        priority_field_id = _get(board, "priority_field_id", str, where, path)
    priority_table = board.get("priority_options", {})
    if not isinstance(priority_table, dict):
        raise ConfigError(f"{path}: key forge.board.priority_options must be a table")
    return Board(
        project_number=_get(board, "project_number", int, where, path),
        project_id=_get(board, "project_id", str, where, path),
        status_field_id=_get(board, "status_field_id", str, where, path),
        status_options=MappingProxyType(status_options),
        priority_field_id=priority_field_id,
        priority_options=MappingProxyType(
            _options(priority_table, where + "priority_options.", path)
        ),
    )


def load(path: Path) -> Config:
    """Read and validate one `lighthouse.toml`."""
    data = _read_toml(path, f"{path}: file is missing")

    forge = _get(data, "forge", dict, "", path)
    kind = _get(forge, "kind", str, "forge.", path)
    if kind != "github":
        raise ConfigError(f"{path}: key forge.kind must be 'github', not {kind!r}")
    repo = _get(forge, "repo", str, "forge.", path)
    if not _REPO.fullmatch(repo):
        raise ConfigError(f"{path}: key forge.repo must have the form owner/name: {repo!r}")

    board = None
    if "board" in forge:
        board = _board(
            _get(forge, "board", dict, "forge.", path), "controller" in data, path
        )

    language = "en"
    if "project" in data:
        project = _get(data, "project", dict, "", path)
        if "language" in project:
            language = project["language"]
            if language not in LANGUAGES:
                raise ConfigError(
                    f"{path}: key project.language must be one of "
                    f"{', '.join(LANGUAGES)}, not {language!r}"
                )

    return Config(
        kind=kind,
        repo=repo,
        language=language,
        board=board,
        raw=MappingProxyType(data),
    )


def load_host(path: Path) -> Host:
    """Read and validate one `host.toml`."""
    data = _read_toml(path, f"host unknown: no host.toml ({path})")

    if "host" not in data:
        raise ConfigError(f"{path}: missing key host")
    host = data["host"]
    if not isinstance(host, dict):
        raise ConfigError(f"{path}: key host must be a table")
    if "name" not in host:
        raise ConfigError(f"{path}: missing key host.name")
    name = host["name"]
    if not isinstance(name, str) or not name:
        raise ConfigError(f"{path}: key host.name must be a non-empty string")
    if "tailscale_ip" not in host:
        raise ConfigError(f"{path}: missing key host.tailscale_ip")
    tailscale_ip = host["tailscale_ip"]
    try:
        if not isinstance(tailscale_ip, str):
            raise TypeError("not a string")
        address = ipaddress.IPv4Address(tailscale_ip)
        if address.is_unspecified or address.is_loopback or address.is_multicast:
            raise ValueError("not a host address")
    except (TypeError, ValueError) as exc:
        raise ConfigError(
            f"{path}: key host.tailscale_ip must be an IPv4 address: {tailscale_ip!r}"
        ) from exc
    return Host(name=name, tailscale_ip=tailscale_ip)


def is_local(ip: str) -> bool:
    """Is `ip` on an interface of this machine? A `bind` measures that; a connect
    or ping says nothing about locality. With `net.ipv4.ip_nonlocal_bind=1` every
    bind succeeds, and every machine recognises itself as the host."""
    try:
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
            sock.bind((ip, 0))
    except OSError as exc:
        if exc.errno == errno.EADDRNOTAVAIL:
            return False
        raise ConfigError(f"local address {ip} not measurable: {exc}") from exc
    return True


def _walk(table: Mapping[str, Any], key: str) -> Any:
    """Walk a dotted key; per level an exact key wins, otherwise the key `k` for
    which the rest starts with `k + "."`. Option names may hold spaces or dots."""
    if key in table:
        return table[key]
    for name, value in table.items():
        if key.startswith(name + ".") and isinstance(value, Mapping):
            return _walk(value, key[len(name) + 1 :])
    raise KeyError(key)


def lookup(cfg: Config, key: str) -> object:
    """The value of `key`; `KeyError` when it is not set."""
    if key == "forge.owner":
        return cfg.owner
    if key == "forge.name":
        return cfg.name
    if key.startswith("host."):
        return _host_value(key)
    return _walk(cfg.raw, key)


def _host_value(key: str) -> str:
    host = load_host(host_path())
    values = {"host.name": host.name, "host.tailscale_ip": host.tailscale_ip}
    if key not in values:
        raise KeyError(key)
    return values[key]


def _format(key: str, value: object) -> str:
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, (str, int, float)):
        return str(value)
    raise ConfigError(f"{key} is a table or a list, not a value")


def _host_check() -> int:
    path = host_path()
    try:
        host = load_host(path)
        local = is_local(host.tailscale_ip)
    except ConfigError as exc:
        print(f"config: {exc}", file=sys.stderr)
        return 2
    if not local:
        print(
            f"config: host {host.name} ({path}) has tailscale_ip {host.tailscale_ip}, "
            "but that address is not on this machine",
            file=sys.stderr,
        )
        return 1
    print(f"host: {host.name}")
    return 0


def main(argv: list[str] | None = None) -> int:
    import argparse

    parser = argparse.ArgumentParser(prog="python3 -m lighthouse_lib.config")
    parser.add_argument("key", help="a dotted key, or host-check")
    args = parser.parse_args(argv)
    if args.key == "host-check":
        return _host_check()
    try:
        if args.key.startswith("host."):
            value: object = _host_value(args.key)
        else:
            value = lookup(load(config_path()), args.key)
        text = _format(args.key, value)
    except KeyError:
        print(f"config: {args.key} is not set", file=sys.stderr)
        return 1
    except ConfigError as exc:
        print(f"config: {exc}", file=sys.stderr)
        return 2
    print(text)
    return 0


if __name__ == "__main__":
    # Under `-m` this file is `__main__`, while other modules import it as
    # `lighthouse_lib.config`, with a ConfigError class of its own. Delegate.
    import importlib

    sys.exit(importlib.import_module("lighthouse_lib.config").main())
