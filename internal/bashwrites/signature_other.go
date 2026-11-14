//go:build !linux && !darwin

package bashwrites

import "os"

// statSignature has no ctime or inode here: both are 0.
func statSignature(info os.FileInfo) Signature {
	return Signature{info.ModTime().UnixNano(), 0, 0, info.Size()}
}
