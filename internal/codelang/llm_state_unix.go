//go:build unix

package codelang

import (
	"os"
	"syscall"
)

// noFollow makes an open fail on a symlink.
const noFollow = syscall.O_NOFOLLOW

// private reports whether dir is a real directory of this user that nobody else can write.
func private(dir string) bool {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid() && info.Mode().Perm()&0o022 == 0
}
