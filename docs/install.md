# Installing the server

`lighthouse serve` runs the API and web UI of the development chain. One server per machine
serves all your repositories from one SQLite database.

## Requirements

- Linux with systemd (user units). macOS is not supported.
- Go, to install the binary.
- Optional: Tailscale, to reach the server from other devices on your tailnet.

## Install the binary

```bash
go install github.com/CorneeldH/lighthouse/cmd/lighthouse@main
```

Use `@main`, not `@latest`: `@latest` resolves to the newest tag, and v0.1.5 has no `serve`.
`@latest` only works after a release that contains `serve`.

Both `go install` forms put the binary in `~/go/bin`. `go install` does not leave a checkout.
The steps below use files from the repository, so clone it and run them from the clone:

```bash
git clone https://github.com/CorneeldH/lighthouse.git
cd lighthouse
```

In the clone, `go install ./cmd/lighthouse` installs the same binary.

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
journalctl --user -u lighthouse
```

The health endpoint answers `{"version":"…"}`. The log shows `lighthouse: listening on
127.0.0.1:8127`.

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
