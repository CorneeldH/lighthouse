# Installing the server

`lighthouse serve` runs the API and web UI of the development chain. One server per machine
serves all your repositories from one SQLite database.

## Requirements

- Linux with systemd (user units) for `lighthouse serve`; macOS is not supported there. The CLI
  runs on macOS, but the hooks are untested there (`usage-guard.sh` uses GNU `date -d`).
  Windows has a binary, but the hooks need WSL.
- Go, only to install with `go install` or to build from a clone.
- Node 22 with npm, to build the web UI. `web/.nvmrc` holds the exact version.
- Optional: Tailscale, to reach the server from other devices on your tailnet.

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
serves a placeholder page: it does not carry the web UI (#78).

## Install with Go

`go install` builds a tagged version from source. `@latest` resolves to the newest tag:

```bash
go install github.com/CorneeldH/lighthouse/cmd/lighthouse@latest
```

`go install` puts the binary in `~/go/bin`. Like the release binary, it serves only a
placeholder page.

## Build from a clone

To get the web UI, clone the repository and build it. The build writes the UI into
`internal/web/dist`, and `go install` embeds it.

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
binary or tag install replaces a UI build from a clone, and the systemd unit then serves the
placeholder page.

## Run it as a user service

From the clone, copy the unit, [lighthouse.service](../deploy/systemd/lighthouse.service), into your user
unit directory. If the binary is not at `~/go/bin/lighthouse`, edit the `ExecStart` path.

```bash
mkdir -p ~/.config/systemd/user
cp deploy/systemd/lighthouse.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now lighthouse.service
loginctl enable-linger "$USER"
```

`enable-linger` keeps the service running when you are not logged in.

The server listens on `127.0.0.1:8127` and stores its database in
`${XDG_STATE_HOME:-$HOME/.local/state}/lighthouse/lighthouse.db`.

## Check it

```bash
curl -s http://127.0.0.1:8127/api/health
curl -s http://127.0.0.1:8127/ | grep lighthouse-ui
journalctl --user -u lighthouse
```

The health endpoint answers `{"version":"…"}`. The page answers with
`<meta name="lighthouse-ui" content="spa" />`: the binary carries the built web UI.
`content="placeholder"` means the UI was not built before `go install`. The log shows
`lighthouse: listening on 127.0.0.1:8127`.

## Connect the forge

`GET /api/questions` and `GET /api/returned` read the open questions and returned issues of
one GitHub repository. Tell the server which repository and give it a token:

- Repository: `--repo owner/name` on `lighthouse serve`, or `LIGHTHOUSE_FORGE_REPO=owner/name`.
- Token: `GH_TOKEN` (else `GITHUB_TOKEN`) in the environment. Create a fine-grained personal
  access token for that repository with only the *Issues: read* permission. The server only
  reads.

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

A `503` means the repository or the token is not set. A `502` means GitHub could not be
reached or answered with an error; the response body says which.

## Reach it from your tailnet

[tailscale-serve.sh](../deploy/tailscale-serve.sh) exposes the local server over HTTPS on the
tailnet. It takes the port as an optional argument (default 8127).

One-time prerequisites:

```bash
sudo tailscale set --operator="$USER"
```

Also enable HTTPS certificates in the tailnet admin console. Then run the script from the
clone:

```bash
deploy/tailscale-serve.sh
```

Turn it off with `tailscale serve --https=<port> off`. Never use `tailscale funnel`: it
publishes the server on the public internet.

## Security

The server has no authentication, on purpose. The tailnet is the access boundary: any device on
the tailnet can do everything the API can. The server binds loopback, so nothing outside the
host and the tailnet sees it. Today the API is read-only; once lighthouse starts runs, any
device on the tailnet can start one.

`/api/questions` and `/api/returned` show the issue titles and question text of the configured
repository, private ones too (read with the server's token), to every device that can reach
the server.
