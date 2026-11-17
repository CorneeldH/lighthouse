package pusher

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// systemBinds are the paths every reviewer sees read-only, when they exist: the binaries and
// libraries of the system, and the few files of /etc that name resolution, TLS and the dynamic
// loader need. Never /etc as a whole, never /home, /root, /run or /tmp of the host. A reviewer
// installed elsewhere (under $HOME, /opt, /nix) names its install directory in binds.
var systemBinds = []string{
	"/usr", "/bin", "/sbin", "/lib", "/lib32", "/lib64",
	"/etc/resolv.conf", "/etc/hosts", "/etc/nsswitch.conf",
	"/etc/passwd", "/etc/group", "/etc/localtime",
	"/etc/ld.so.cache", "/etc/ld.so.conf", "/etc/ld.so.conf.d", "/etc/alternatives",
	"/etc/ssl", "/etc/pki", "/etc/ca-certificates", "/etc/crypto-policies",
}

// sandboxArgs is the bwrap argv (without "bwrap") that runs command in a sandbox whose only
// writable path is tmp, mounted at its own path and at /tmp. Every namespace is unshared except
// the network: a reviewer is a model CLI that calls a remote API, so without the network there
// is no review. What the network could leak is bounded by what the sandbox holds: the diff,
// which goes to the model anyway, and the read-only binds, never a token. binds come from
// host.toml and are mounted read-only and strict, so a missing one fails the reviewer.
// --die-with-parent kills the sandbox with the pusher; --new-session keeps it off the terminal.
func sandboxArgs(tmp string, binds, command []string) []string {
	args := []string{"--unshare-all", "--share-net", "--die-with-parent", "--new-session"}
	for _, p := range systemBinds {
		args = append(args, "--ro-bind-try", p, p)
	}
	args = append(args, "--proc", "/proc", "--dev", "/dev")
	for _, b := range binds {
		args = append(args, "--ro-bind", b, b)
	}
	// /tmp first: tmp itself usually lies under /tmp and must be mounted on top.
	args = append(args, "--bind", tmp, "/tmp", "--bind", tmp, tmp, "--chdir", tmp, "--")
	return append(args, command...)
}

// checkBinds refuses a bind that is relative, the root, or that holds the token file or the
// lighthouse config directory: host.toml cannot widen the sandbox to what it must keep out.
func checkBinds(binds []string, tokenFile string) error {
	var guarded []string
	if tokenFile != "" {
		guarded = append(guarded, filepath.Clean(tokenFile))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		guarded = append(guarded, filepath.Join(home, ".config", "lighthouse"))
	}
	for _, b := range binds {
		if !filepath.IsAbs(b) {
			return fmt.Errorf("bind %q is not an absolute path", b)
		}
		cb := filepath.Clean(b)
		for _, g := range guarded {
			if within(g, cb) || within(cb, g) {
				return fmt.Errorf("bind %s overlaps %s, which never enters a reviewer sandbox", b, g)
			}
		}
	}
	return nil
}

// within reports whether path is dir or lies under it; both are clean and absolute.
func within(path, dir string) bool {
	return path == dir || dir == "/" || strings.HasPrefix(path, dir+string(filepath.Separator))
}
