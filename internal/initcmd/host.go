package initcmd

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/hostconfig"
)

// host.toml lives on the host, never in the repository (spec, init punt 3). init
// proposes it when it is missing; it is not a unit and not in the manifest.

// hostLabel names path in a diff: ~/... for the default location.
func hostLabel(path string) string {
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, path); err == nil && filepath.ToSlash(rel) == hostconfig.HomeRel {
			return "~/" + hostconfig.HomeRel
		}
	}
	return path
}

func tailscaleIP() (string, error) {
	out, err := exec.Command("tailscale", "ip", "-4").Output()
	if err != nil {
		return "", err
	}
	first, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return first, nil
}

// offerHost proposes host.toml after the repository changes, with a question of its
// own: a write into the home directory is never folded into the repository's approval.
// --upgrade only reports a missing host.toml, so it stays "No changes." on a host
// whose owner does not want one.
func offerHost(o Options, in *bufio.Reader) error {
	if o.Upgrade {
		if o.HostTOML != "" {
			if _, err := os.Stat(o.HostTOML); errors.Is(err, fs.ErrNotExist) {
				_, _ = fmt.Fprintf(o.Err, "note: host.toml is missing (%s); skills that check the host will report it\n", hostLabel(o.HostTOML))
			}
		}
		return nil
	}
	host := proposeHost(o)
	if host == nil {
		return nil
	}
	_, _ = fmt.Fprint(o.Out, unified(nil, host, "/dev/null", hostLabel(o.HostTOML)))
	if !o.Yes && !ask(o.Out, in, "Write "+o.HostTOML+"? [y/N] ") {
		return nil
	}
	if err := writeHost(o.HostTOML, host); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(o.Out, "Wrote %s.\n", hostLabel(o.HostTOML))
	return nil
}

// proposeHost returns the content of a new host.toml, or nil when there is one
// already or the host cannot be identified; the latter is reported.
func proposeHost(o Options) []byte {
	if o.HostTOML == "" {
		_, _ = fmt.Fprintln(o.Err, "warning: host.toml not proposed: no home directory")
		return nil
	}
	if _, err := os.Stat(o.HostTOML); err == nil || !errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	ip, err := o.TailscaleIP()
	if err == nil {
		if parsed := net.ParseIP(strings.TrimSpace(ip)); parsed == nil || parsed.To4() == nil {
			err = fmt.Errorf("not an IPv4 address: %q", ip)
		}
	}
	if err != nil {
		_, _ = fmt.Fprintln(o.Err, "warning: host.toml not proposed: tailscale ip unavailable; skills that check the host will report it")
		return nil
	}
	name, err := o.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		_, _ = fmt.Fprintln(o.Err, "warning: host.toml not proposed: hostname unavailable; skills that check the host will report it")
		return nil
	}
	return []byte(fmt.Sprintf("[host]\nname = %q\ntailscale_ip = %q\n", strings.TrimSpace(name), strings.TrimSpace(ip)))
}

func writeHost(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}
