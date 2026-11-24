package bashwrites

import "os"

// statSignature on Windows: os.FileInfo has no change time and no inode there. The mtime stands
// in for the ctime: every write sets it, so seedRoot still leaves out a file written at or
// after the start of pre (#559). The inode stays 0.
func statSignature(info os.FileInfo) Signature {
	mtime := info.ModTime().UnixNano()
	return Signature{mtime, mtime, 0, info.Size()}
}
