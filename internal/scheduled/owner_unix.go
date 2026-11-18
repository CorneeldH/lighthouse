//go:build unix

package scheduled

import (
	"errors"
	"os"
	"syscall"
)

// checkOwner refuses a directory that is not owned by uid or that others can write to.
func checkOwner(fi os.FileInfo, uid int) error {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != uid {
		return errors.New("remote: the state directory is not owned by this user")
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return errors.New("remote: the state directory is writable by others")
	}
	return nil
}
