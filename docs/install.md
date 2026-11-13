# Installing the server

`lighthouse serve` runs the API and web UI of the development chain. One server per machine
serves all your repositories from one SQLite database.

## Requirements

- Linux with systemd (user units) for `lighthouse serve`; macOS is not supported there. The CLI
  runs on macOS, but the hooks are untested there.
  Windows has a binary, but the hooks need WSL.
- Go, only to install with `go install` or to build from a clone.
- Node 22 with npm, only to build from a clone (the web UI). `web/.nvmrc` holds the exact version.
  A release binary needs neither Node nor a clone.
- Optional: Tailscale, to reach the server from other devices on your tailnet.

Every assumption about the host, with what another host does: [host-assumptions.md](host-assumptions.md).

## Download a release binary

Each release carries a binary per platform, plus `SHA256SUMS`: `lighthouse_linux_amd64`,
`lighthouse_linux_arm64`, `lighthouse_darwin_amd64`, `lighthouse_darwin_arm64` and
`lighthouse_windows_amd64.exe`. This example takes Linux on amd64:

```bash
curl -fsSLO https://github.com/CorneeldH/lighthouse/releases/latest/download/lighthouse_linux_amd64
curl -fsSLO https://github.com/CorneeldH/lighthouse/releases/latest/download/SHA256SUMS
sha256sum --check --ignore-missing SHA256SUMS
chmod +x lighthouse_linux_amd64
mkdir -p ~/go/bin && mv lighthouse_linux_amd64 ~/go/bin/lighthouse
```

On macOS, download `lighthouse_darwin_arm64` (Apple silicon) or `lighthouse_darwin_amd64`, verify
with `shasum -a 256 -c --ignore-missing SHA256SUMS`, then `chmod +x` the file. On Windows,
download `lighthouse_windows_amd64.exe`, run `Get-FileHash -Algorithm SHA256
lighthouse_windows_amd64.exe` in PowerShell and compare the hash with its line in `SHA256SUMS`.

`~/go/bin/lighthouse` is where the systemd unit below looks for the binary; any directory on
your `PATH` works for the CLI. The release binary reports its tag (`lighthouse version`) and
carries the built web UI: `lighthouse serve` serves it at once, with no Node and no clone. To
upgrade, download the new binary the same way and follow the steps in
[Releasing](releasing.md#upgrading). Restart the service afterwards.

## Install with Go

`go install` builds a tagged version from source. `@latest` resolves to the newest tag:

```bash
go install github.com/CorneeldH/lighthouse/cmd/lighthouse@latest
```

`go install` puts the binary in `~/go/bin`. It serves only a placeholder page, because the web
UI is not part of the module source. The release binary above is the recommended route without
a clone.

## Build from a clone

This route is for developers. To build the web UI yourself, clone the repository and build it.
The build writes the UI into `internal/web/dist`, and `go install` embeds it.

```bash
git clone https://github.com/CorneeldH/lighthouse.git
cd lighthouse
cd web && npm ci && npm run build && cd ..
go install ./cmd/lighthouse
```

A build from a clone reports version `dev`, and `lighthouse init` writes that version into
`.lighthouse/manifest.toml`. For a repository you run `init` in, use a release binary or a
tag instead.

All routes write `~/go/bin/lighthouse` (or your chosen path), so the last one wins: a release
binary replaces a build from a clone and still serves the UI. Only a `go install` of a tag
replaces it with the placeholder page, and the systemd unit then serves that.

## Run it as a user service

Fetch the unit, [lighthouse.service](../deploy/systemd/lighthouse.service), into your user
unit directory. Use the tag of the binary you downloaded (`lighthouse version`) for `<tag>`. If
the binary is not at `~/go/bin/lighthouse`, edit the `ExecStart` path.

```bash
mkdir -p ~/.config/systemd/user
curl -fsSLo ~/.config/systemd/user/lighthouse.service https://raw.githubusercontent.com/CorneeldH/lighthouse/<tag>/deploy/systemd/lighthouse.service
systemctl --user daemon-reload
systemctl --user enable --now lighthouse.service
loginctl enable-linger "$USER"
```

With a clone, copy the unit instead of the `curl` line:
`cp deploy/systemd/lighthouse.service ~/.config/systemd/user/`.

`enable-linger` keeps the service running when you are not logged in.

The server listens on `127.0.0.1:8127` and stores its database in
`${XDG_STATE_HOME:-$HOME/.local/state}/lighthouse/lighthouse.db`.

## Upgrade automatically

A timer can keep the binary current. [lighthouse-upgrade.service](../deploy/systemd/lighthouse-upgrade.service)
runs `lighthouse self-update --restart lighthouse.service`, and
[lighthouse-upgrade.timer](../deploy/systemd/lighthouse-upgrade.timer) starts it daily. The
binary is replaced only when the newest release is newer and its checksum matches, and the
server restarts only after a replacement. A `dev` build is skipped.

```bash
cp deploy/systemd/lighthouse-upgrade.{service,timer} ~/.config/systemd/user/
systemctl --user daemon-reload && systemctl --user enable --now lighthouse-upgrade.timer
```

Without a clone, fetch both files with `curl` as for `lighthouse.service` above. The unit
reuses the forge token in `~/.config/lighthouse/forge.env` (`GH_TOKEN`) when present, and sends
it to `api.github.com` for the release lookup only; keep that file mode 0600.

The installed binary must already be a release that has `self-update`: upgrade once by hand
first. Under the unit's hardening `~/go/bin/lighthouse` must not be a symlink to a path outside
`~/go/bin`, because that directory is the only one the unit may write.

Every release goes live without a review. To stop:
`systemctl --user disable --now lighthouse-upgrade.timer`.

Which version runs, and what the last run did:

```bash
lighthouse version
curl -s 127.0.0.1:8127/api/health
journalctl --user -u lighthouse-upgrade.service -n 5
```

## Check it

```bash
curl -s http://127.0.0.1:8127/api/health
curl -s http://127.0.0.1:8127/ | grep lighthouse-ui
journalctl --user -u lighthouse
```

The health endpoint answers `{"version":"…"}`. The page answers with
`<meta name="lighthouse-ui" content="spa" />`: the binary carries the built web UI.
`content="placeholder"` means the binary was built without the UI (`go install`, or a clone
without `npm run build`). The log shows
`lighthouse: listening on 127.0.0.1:8127`.

The API is read-only. Its routes:

| Route | What it reads |
|---|---|
| `GET /api/health` | the version of the binary |
| `GET /api/usage` | the Claude and Codex usage windows |
| `GET /api/runs` | the open and parked runs and the deferred starts, from the tick state |
| `GET /api/questions` | the open questions, from the forge |
| `GET /api/returned` | the returned issues, from the forge |
| `GET /api/cleanup` | the workspaces to clean up, from the database |
| `GET /api/latest-comment?repo=owner/name&issue=N` | the comments of one issue, from the forge; redirects (302) to the newest comment, or to the issue page when it has none or the read fails. A repo `serve` does not serve gets 404 |

## Connect the forge

`GET /api/questions` and `GET /api/returned` read the open questions and returned issues of
the served GitHub repositories. Every item carries its `repo` (`owner/name`), and the UI shows
it as `owner/name#number`. Every `owner/name#number` in the UI links to
`/api/latest-comment`, so a click lands on the newest comment of the issue. Which repositories `serve` reads:

- With `[dispatch] repos` in `host.toml`: every repository listed there, in that order. A repo
  whose `lighthouse.toml` does not load is skipped with a line on stderr. `--repo` is then
  ignored, with `lighthouse: --repo <owner/name> ignored: serving the repos of [dispatch]` when
  it is not among them.
- Without `[dispatch] repos`: the one repository of `--repo owner/name` on `lighthouse serve`, or
  `LIGHTHOUSE_FORGE_REPO=owner/name`.

Give it a token: `GH_TOKEN` (else `GITHUB_TOKEN`) in the environment. Create a fine-grained
personal access token for those repositories with only the *Issues: read* permission. The
server only reads. With the Controller filter described below it also needs the `project` scope.

Under systemd, put both in `~/.config/lighthouse/forge.env`, which the unit loads through
`EnvironmentFile=`. The token is a secret, so keep the file private:

```bash
mkdir -p ~/.config/lighthouse
install -m 0600 /dev/null ~/.config/lighthouse/forge.env
cat > ~/.config/lighthouse/forge.env <<'EOF'
LIGHTHOUSE_FORGE_REPO=owner/name
GH_TOKEN=<your fine-grained token>
EOF
systemctl --user restart lighthouse.service
```

A unit you copied before this change lacks the `EnvironmentFile=` line. Copy the unit again,
run `systemctl --user daemon-reload`, then restart the service.

Check it:

```bash
curl -s http://127.0.0.1:8127/api/questions
```

For a repository in `[dispatch] repos` whose `lighthouse.toml` has a `[forge.board]` with a
`Controller` option, the overview shows only the issues in Controller. Issues the dispatcher
parked (On hold, see [tick.md](tick.md)) then drop out. Otherwise it shows every open issue of
that repository. With the filter `serve` reads the project board, so the token needs the
`project` scope as well. A `host.toml` that does not load (an invalid value or an unknown key under
`[dispatch]`) stops `serve`; a missing one counts as no `[dispatch]`.

A `503` means no repository or no token is set. A `502` means GitHub could not be reached or
answered with an error for one of the repositories; the response body names it.

## Running runs

`GET /api/runs` and the page blocks *Running runs*, *Parked runs*, *Queue* and *Night* show the open
and parked runs and the deferred starts of the served repositories. A row of summary tiles above the
blocks counts each block and links to it; a tile shows `–` while its data is loading or has never
loaded (a failed refresh keeps the last number, like the panels). A deferred start with reason
`night` goes to *Night*, every other one to *Queue*. `serve` reads them from the tick state in its
database, the one `lighthouse tick` writes (see [tick.md](tick.md)): no forge call and no token, so
the blocks refresh every 30 seconds. Per run: `repo`, `issue`, `title`, `state` (`open` or
`parked`), `command`, `phase` (the skill the command starts, empty for an empty command), `task`,
`tasks` and `step` (the current plan task, the number of tasks and `build`, `review` or
`final-review` for a subagent-driven run; `0`, `0` and empty without progress), `trigger`,
`profile`, `workspace_id`, `started_at`, `stand` (`working`, `waiting` or `done`; empty when the
tick could not measure the session, shown as *not measured*) and `at`, the tick that wrote it. Per
deferred start: `repo`, `issue`, `title`, `reason` and `at`. The rows do not show `at`; the Controller
section heading shows the age of the newest tick as *tick … ago*, so a row is at most one tick behind.
Without a tick both lists are empty. Next to `runs` and `deferred` the body carries two settings
of host.toml: `workspace_url`, the kenn-forge address a browser opens, without a trailing slash.
It is `[dispatch.forge] web_url`, else `[dispatch.forge] url` (`http://127.0.0.1:8091` without a
host.toml). Set `web_url` to the tailscale serve address when you open the page from another
machine; lighthouse itself keeps talking to the forge API on `url`. The page links the workspace id in
a run row and in *Workspaces to clean up* to `<workspace_url>/workspaces/<id>`, in a new tab. The other setting is `night`,
the night window from `[dispatch] night` as `HH:MM-HH:MM`. A `500` means the database could not be read.

## Run the tick

`lighthouse tick` is the dispatcher. With `[dispatch] workplace = "local"` in `host.toml` it needs no
kenn-forge: each run gets a git worktree and a detached `claude -p` on this host (see
[workplace.md](workplace.md#lokaal) and [tick.md](tick.md)). The host needs Linux with `/proc`, `git`,
`setsid` from util-linux and `claude` on the `PATH` of the tick.

Fetch the unit, [lighthouse-tick.service](../deploy/systemd/lighthouse-tick.service), and its timer,
[lighthouse-tick.timer](../deploy/systemd/lighthouse-tick.timer), the way you fetched `lighthouse.service`:
the tag of your binary for `<tag>`, and an `ExecStart` path that matches where the binary lives.

```bash
curl -fsSLo ~/.config/systemd/user/lighthouse-tick.service https://raw.githubusercontent.com/CorneeldH/lighthouse/<tag>/deploy/systemd/lighthouse-tick.service
curl -fsSLo ~/.config/systemd/user/lighthouse-tick.timer https://raw.githubusercontent.com/CorneeldH/lighthouse/<tag>/deploy/systemd/lighthouse-tick.timer
systemctl --user daemon-reload
systemctl --user enable --now lighthouse-tick.timer
loginctl enable-linger "$USER"
```

With a clone, copy both files from `deploy/systemd/` instead. Enable the timer, not the service; it runs
the tick two minutes after boot and every three minutes after. `enable-linger` keeps the timer running
when you are not logged in.

Two choices in the unit are on purpose:

- `KillMode=process`. A detached run lives in the cgroup of the unit. With the default
  `KillMode=control-group` systemd kills every process of the cgroup when the tick ends, so each run
  would die with the tick that started it. While runs live, systemd logs `Found left-over process … in
  control group while starting unit. Ignoring.` on each tick; that is expected.
- No hardening (`ProtectHome=`, `ProtectSystem=`, `NoNewPrivileges=`). The runs inherit the unit's
  namespace and must write the worktree, `~/.claude` and the state directory, and run git, go and the
  test gate.

Put `GH_TOKEN` in `~/.config/lighthouse/forge.env` (mode `0600`), the file `lighthouse.service` also
reads (see *Connect the forge*). The runs inherit the token, so a run has the rights of the dispatcher.
`claude` must be on the unit's `PATH`; the unit adds `%h/.local/bin`, where the claude installer puts it.
It also adds `/usr/local/go/bin`, where the official Go distribution puts `go`, which runs and the test
gate need.

Without systemd, run the tick from cron. Cron sets no `XDG_RUNTIME_DIR`, which the tick needs, so the
line sets it:

```text
*/5 * * * * export XDG_RUNTIME_DIR=/run/user/$(id -u) PATH="$HOME/.local/bin:$HOME/go/bin:/usr/local/bin:/usr/local/go/bin:/usr/bin:/bin"; set -a; . "$HOME/.config/lighthouse/forge.env"; set +a; lighthouse tick
```

## Reach it from your tailnet

[tailscale-serve.sh](../deploy/tailscale-serve.sh) exposes the local server over HTTPS on the
tailnet. It takes the port as an optional argument (default 8127).

One-time prerequisites:

```bash
sudo tailscale set --operator="$USER"
```

Also enable HTTPS certificates in the tailnet admin console. Then fetch the script for your
release tag (see `<tag>` above) and run it:

```bash
curl -fsSLO https://raw.githubusercontent.com/CorneeldH/lighthouse/<tag>/deploy/tailscale-serve.sh
chmod +x tailscale-serve.sh
./tailscale-serve.sh
```

With a clone, run `deploy/tailscale-serve.sh` instead.

Turn it off with `tailscale serve --https=<port> off`. Never use `tailscale funnel`: it
publishes the server on the public internet.

## Security

The server has no authentication, on purpose. The tailnet is the access boundary: any device on
the tailnet can do everything the API can. The server binds loopback, so nothing outside the
host and the tailnet sees it. Today the API is read-only; once lighthouse starts runs, any
device on the tailnet can start one.

`/api/questions` and `/api/returned` show the issue titles and question text of the served
repositories, private ones too (read with the server's token), to every device that can reach
the server. `/api/runs` shows their issue titles and the commands of their runs.
