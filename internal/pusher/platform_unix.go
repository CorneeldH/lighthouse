//go:build unix

package pusher

import (
	"os"
	"syscall"
)

// oNoFollow makes an open refuse a symlink at the last path element.
const oNoFollow = syscall.O_NOFOLLOW

// ownedByMe reports whether the file of info belongs to the user the pusher runs as.
func ownedByMe(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}
