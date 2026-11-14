//go:build linux

package bashwrites

import (
	"os"
	"syscall"
)

func statSignature(info os.FileInfo) Signature {
	sig := Signature{info.ModTime().UnixNano(), 0, 0, info.Size()}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		sig[1] = int64(st.Ctim.Sec)*1e9 + int64(st.Ctim.Nsec)
		sig[2] = int64(st.Ino)
	}
	return sig
}
