package pusher

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/hostconfig"
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
// writable path is tmp, mounted at /tmp and at its own path. Every namespace is unshared except
// the network: a reviewer is a model CLI that calls a remote API, so without the network there
// is no review. What the network could leak is bounded by what the sandbox holds: the diff,
// which goes to the model anyway, and the read-only binds, never a token. binds come from
// host.toml and are mounted read-only and strict, after the tempdir mounts, so a mount on /tmp
// never hides a bind under it. --die-with-parent kills the sandbox with the pusher;
// --new-session keeps it off the terminal.
func sandboxArgs(tmp string, binds, command []string) []string {
	args := []string{"--unshare-all", "--share-net", "--die-with-parent", "--new-session"}
	for _, p := range systemBinds {
		args = append(args, "--ro-bind-try", p, p)
	}
	args = append(args, "--proc", "/proc", "--dev", "/dev")
	// /tmp first: tmp itself usually lies under /tmp and must be mounted on top.
	args = append(args, "--bind", tmp, "/tmp", "--bind", tmp, tmp)
	for _, b := range binds {
		args = append(args, "--ro-bind", b, b)
	}
	args = append(args, "--chdir", tmp, "--")
	return append(args, command...)
}

// guardedPaths are the paths no bind may equal, contain or lie under: the workflow token file,
// the lighthouse config, the gh config, the kenn-forge root (its auth token and the worktrees),
// the default forge token, and extra (the forge token file, the pusher's state and clone dir).
func guardedPaths(tokenFile string, extra []string) []string {
	guard := append([]string{tokenFile}, extra...)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		guard = append(guard,
			filepath.Join(home, ".config", "lighthouse"),
			filepath.Join(home, ".config", "gh"),
			filepath.Join(home, ".kenn", "forge"))
	}
	if f := hostconfig.DefaultDispatch().Forge.TokenFile; f != "" {
		guard = append(guard, f)
	}
	return guard
}

// checkBinds refuses a bind that is relative, does not exist, or overlaps a guarded path or the
// tempdir mounts (tmp and /tmp): host.toml cannot widen the sandbox to what it must keep out.
// Both sides are compared as written and with symlinks resolved, so /srv/x -> ~/.config/lighthouse
// is refused too. A missing bind is refused here, with its name, rather than left to bwrap.
func checkBinds(binds, guard []string, tmp string) error {
	var guards []string
	for _, g := range guard {
		if g != "" && filepath.IsAbs(g) {
			guards = append(guards, filepath.Clean(g), resolvePath(g))
		}
	}
	mounts := []string{filepath.Clean(tmp), resolvePath(tmp), "/tmp", resolvePath("/tmp")}
	for _, b := range binds {
		if !filepath.IsAbs(b) {
			return fmt.Errorf("bind %q is not an absolute path", b)
		}
		real, err := filepath.EvalSymlinks(b)
		if err != nil {
			return fmt.Errorf("bind %s cannot be resolved: %w", b, err)
		}
		for _, side := range []string{filepath.Clean(b), real} {
			for _, g := range guards {
				if within(g, side) || within(side, g) {
					return fmt.Errorf("bind %s overlaps %s, which never enters a reviewer sandbox", b, g)
				}
			}
			for _, m := range mounts {
				if within(m, side) {
					return fmt.Errorf("bind %s holds %s, the writable tempdir of the sandbox", b, m)
				}
			}
		}
	}
	return nil
}

// resolvePath is p with symlinks resolved; a missing tail is kept as written under the resolved
// part that exists.
func resolvePath(p string) string {
	p = filepath.Clean(p)
	real, err := filepath.EvalSymlinks(p)
	if err == nil {
		return real
	}
	parent := filepath.Dir(p)
	if !errors.Is(err, fs.ErrNotExist) || parent == p {
		return p
	}
	return filepath.Join(resolvePath(parent), filepath.Base(p))
}

// within reports whether path is dir or lies under it; both are clean and absolute.
func within(path, dir string) bool {
	return path == dir || dir == "/" || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// removeTree removes dir after making every directory in it writable again, so a reviewer that
// chmods its tempdir cannot leave it behind.
func removeTree(dir string) error {
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if d != nil && d.IsDir() {
			_ = os.Chmod(path, 0o700)
		}
		return nil
	})
	return os.RemoveAll(dir)
}
