# Host assumptions

Lighthouse runs on more than one machine, so every place that assumes something about the host is
listed here, with what happens on another host. The list comes from a scan of `pack/`, `internal/`,
`cmd/`, `deploy/` and `docs/` for host-specific tools, paths and addresses (`/proc`, `systemd`,
`pgrep`, `tailscale`, `sha256sum`, `tomllib`, `127.0.0.1`, `~/.kenn` and the name of the first
host). A new assumption gets a row here in the same change that adds it.

## Classes

| Class | Meaning | Where it may stand |
|---|---|---|
| `Everywhere` | Works on Linux, macOS and WSL. Where it cannot measure, the result is defined and safe: it blocks, reports `COULD NOT CHECK`, or assumes the cautious case, never a silent guess. | Anywhere. |
| `Host feature` | Needs something the host may lack (Tailscale, a systemd slice, `choom`). It sits behind a key or an optional step and has a defined result when it is missing. | Behind a configuration key, or in an optional step of the docs. |
| `Linux-only` | Needs Linux (`/proc`, systemd, `journalctl`). Another host gets a documented limit. | Where the limit is documented. |
| `Beelink` | A value or default that only holds on the first host. | Only in `host.toml`, in `deploy/` as an example, or in personal_ai_agent. Never as a default in code or in `docs/`. |

## Inventory

One row per file and assumption. The `Where` column links to the file and names the lines. A cell
`—` in `Issue` means no issue: the case is documented or does not need one.

| Where | Assumption | Class | On another host | Issue |
|---|---|---|---|---|
| [`config.py:468-490`](../pack/core/lib/config.py#L468), [`plan_review_record.py:149-151`](../pack/core/scripts/plan_review_record.py#L149) | `host.tailscale_ip` is an optional IPv4 key of `host.toml`; when set, `config` and `plan_review_record` check that this address is bound on the machine, to decide whether this is the host | Host feature | Without Tailscale, leave `tailscale_ip` out; `host-check` and `plan_review_record` then report another host | — |
| [`host.go:77-93`](../internal/initcmd/host.go#L77), [`initcmd.go:59,97`](../internal/initcmd/initcmd.go#L59) | `init` proposes `host.toml` from `tailscale ip -4` | Host feature | Without Tailscale `init` warns that the chain works without `host.toml` and writes nothing | — |
| [`getting-started.md:116-121`](getting-started.md#L116), [`site/repo.html`](site/repo.html), [`site/config.html`](site/config.html), [`site/data/config.json`](site/data/config.json), [`site/data/repo.json`](site/data/repo.json), [`site/data/system.json`](site/data/system.json) | Docs that describe the `host.toml` proposal as "only with Tailscale" | Host feature | Same as the two rows above | — |
| [`worktree_audit.py:170`](../pack/core/scripts/worktree_audit.py#L170) | `pid_alive` signals the pid with `os.kill(pid, 0)`, no `/proc` | Everywhere | On native Windows, and for a pid it cannot measure, the result is "not measurable" and removal is blocked | — |
| [`fix-pr/claude/SKILL.md:160-167`](../pack/core/skills/fix-pr/claude/SKILL.md#L160), [`fix-pr/agents/SKILL.md:160-167`](../pack/core/skills/fix-pr/agents/SKILL.md#L160) | `journalctl -k` reads OOM kills from the kernel journal | Linux-only | The skill names it Linux only and gives `log show` for macOS | — |
| [`commit-test-gate.sh:518-546`](../pack/core/hooks/commit-test-gate.sh#L518) | `systemd-run --user --scope` and `choom` run the test gate in `host.gate_slice` | Host feature | Without the key the gate runs without a memory brake; without `systemctl`, `systemd-run`, `choom` or the slice it says so and runs unbraked | — |
| [`commit-test-gate.sh:459`](../pack/core/hooks/commit-test-gate.sh#L459), [`host_memory.py`](../pack/core/lib/host_memory.py) | The memory thresholds read `/proc/meminfo` | Linux-only | With a threshold set and no `/proc/meminfo` the commit blocks with `COULD NOT CHECK`; see [test-gate.md](test-gate.md) | — |
| [`usage-gate.sh:75-76`](../pack/core/hooks/usage-gate.sh#L75), [`alarm.sh:33-34`](../pack/core/hooks/usage-gate/alarm.sh#L33), [`usage-gate.md:33,173`](usage-gate.md#L33) | `date -d` (GNU) with a BSD `date -j -u -f` fallback | Everywhere | macOS takes the BSD branch; an unreadable time means the alarm does not count and the pause counts as expired | — |
| [`config.py:27,289-299`](../pack/core/lib/config.py#L27), [`headings.py`](../pack/core/lib/headings.py#L41), [`bash_writes.py`](../pack/core/lib/bash_writes.py#L266), [`usage_guard.py`](../pack/core/lib/usage_guard.py#L20), [`baseline.py`](../pack/python/scripts/quality/baseline.py#L11), [`ratchet.py`](../pack/python/scripts/quality/ratchet.py#L854), [`deps.go:36-59`](../internal/deps/deps.go#L36), `requires.toml` of [core](../pack/core/requires.toml), [go](../pack/go/requires.toml), [python](../pack/python/requires.toml) and [shell](../pack/shell/requires.toml) | `tomllib` needs Python 3.11 or newer | Everywhere | Older Python: the hooks report `COULD NOT CHECK` and say which version is needed | — |
| [`shellcheck-bin.sh:22-25,67`](../pack/shell/scripts/lib/shellcheck-bin.sh#L22) | The pinned `shellcheck` download exists for Linux and macOS on x86_64 and arm64, verified with `sha256sum` or `shasum -a 256` | Everywhere (native Windows and other architectures: not measurable) | On native Windows or another architecture: "no release", the shell hook reports that it cannot check. Not needed unless the repo uses `pack/shell` | — |
| [`brake.go:42-47`](../internal/cli/brake.go#L42), [`tick.go:47`](../internal/cli/tick.go#L47), [`dispatch/brake.go`](../internal/dispatch/brake.go), [`meminfo.go`](../internal/meminfo/meminfo.go#L1) | The dispatcher memory brake reads `/proc/meminfo` | Linux-only | Where the file is unreadable the free memory is "not measured" (`mem_available_gib` is nil); `internal/dispatch/brake.go:82-84` then closes the brake, so every non-manual run is deferred with `memory: MemAvailable not measured` and on macOS the dispatcher starts nothing | milestone "Werkplek zonder kenn-forge" |
| [`hostconfig.go:69,72`](../internal/hostconfig/hostconfig.go#L69), [`tick.md:413-415`](tick.md#L413) | The default forge URL `http://127.0.0.1:8091` and token file `~/.kenn/forge/auth_token` | Host feature | These are kenn-forge's own defaults; they apply only with the kenn-forge adapter and are overridable through `[dispatch.forge] url` and `token_file` | — |
| [`stop.go:34`](../internal/workplace/kennforge/stop.go#L34), [`workplace.md:209`](workplace.md#L209) | The unit name `paia-forge-<profile>-<basename>`, as `forge_agent.sh` of the first host makes it | Beelink | Stop on another host looks for a unit that does not exist and ends nothing | #254 |
| [`stop.go`](../internal/workplace/kennforge/stop.go#L57), [`transcript.go:65`](../internal/workplace/kennforge/transcript.go#L65), [`lifecycle.go:204`](../internal/workplace/lifecycle.go#L204), [`workplace.md`](workplace.md#L21) | The kenn-forge adapter stops sessions with `systemctl --user`, `podman`, `pgrep` and `pkill` | Linux-only | No systemd or podman: the adapter errors on that call | #253 |
| [`procs_linux.go`](../internal/workplace/local/procs_linux.go), [`spawn.go`](../internal/workplace/local/spawn.go) | The local adapter reads `/proc/<pid>/environ` and `/proc/<pid>/stat`, and starts runs through `sh` and util-linux `setsid -f` | Linux-only | Elsewhere the methods of `Procs` return `local workplace: needs /proc, which only linux has`; `workplace = "local"` only works on Linux | #253 |
| [`lighthouse-tick.service`](../deploy/systemd/lighthouse-tick.service) | The tick as a user unit with `KillMode=process` and no hardening | Host feature | Without systemd the tick runs from cron (see [install.md](install.md)); a unit with `KillMode=control-group` kills every local run after the tick | #253 |
| [`meter.go:158-170`](../internal/usage/meter.go#L158), [`usage-venster.md:12`](usage-venster.md#L12) | `pgrep -x claude` decides whether `usage tick` measures | Everywhere | `pgrep` exists on Linux and macOS. A missing `pgrep` or an exit code other than 1 counts as "cannot measure", read as "claude may run", so the tick measures. Native Windows has no `pgrep` and takes that path | — |
| [`lighthouse.service`](../deploy/systemd/lighthouse.service#L1) | A systemd user unit for `lighthouse serve` | Linux-only | Example only; on macOS run `lighthouse serve` yourself or with `launchd` | — |
| [`tailscale-serve.sh`](../deploy/tailscale-serve.sh#L25) | Exposes the server on the tailnet with `tailscale serve` | Host feature | Optional; without Tailscale the server stays on loopback | — |
| [`test-gate.md:14-62`](test-gate.md#L14) | The example `host.toml` with `gates.slice`, thresholds and `tailscale_ip` | Host feature | Every key is optional except `name`; see the rows above | — |
| [`install.md`](install.md#L8) | `lighthouse serve` as a systemd user unit, `journalctl --user`, `systemctl --user` | Linux-only | Already a stated requirement; macOS runs the CLI but not the server unit | — |
| [`releasing.md:101`](releasing.md#L101), [`triage.md:191-220`](triage.md#L191) | `systemctl --user` restart of the service, a systemd user timer for triage | Linux-only | Examples; use the scheduler of the host | — |
| [`secret-scan.md:25,91-108`](secret-scan.md#L91) | The secret scan runs `gitleaks` in a `systemd-run` scope with `choom` | Host feature | Without systemd, the user bus or `choom` the scan runs without a ceiling and says so | — |
| [`install.md`](install.md#L8) (hooks) | Hooks on native Windows | Linux-only | The hooks need WSL; the binary itself runs on Windows | #90 |

## Scanned, no host assumption

| File | Why |
|---|---|
| [`serve.go:32`](../internal/cli/serve.go#L32) | `127.0.0.1:8127` is the loopback default of `serve`, valid on every host |
| [`server/host.go`](../internal/server/host.go#L10) | A comment about the Host header check; it assumes no tool |
| [`check-instructions.sh`](../pack/core/scripts/check-instructions.sh#L51) | `sha256sum` with a `shasum -a 256` fallback |
| [`site/install.html`](site/install.html), [`site/agents/install.md`](site/agents/install.md) | `sha256sum` and `shasum` as alternatives, per platform |
| [`ask.md`](ask.md), [`dispatch/instructions/run.md`](../internal/dispatch/instructions/run.md) | The example installs `ripgrep`; the scan matched `pgrep` inside the word |
| [`place-instruction/SKILL.md`](../pack/core/skills/place-instruction/SKILL.md#L57) | Names systemd as an example of vendor documentation |
