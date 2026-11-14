package bashwrites

import "os"

// Signature is (mtime_ns, ctime_ns, ino, size) of a file.
type Signature [4]int64

// signatureOf is the Signature of path, false if it is not there.
func signatureOf(path string) (Signature, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return Signature{}, false
	}
	return statSignature(info), true
}
