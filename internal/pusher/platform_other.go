//go:build !unix

package pusher

import "os"

// oNoFollow has no counterpart here; the Lstat checks before each open still refuse a symlink.
const oNoFollow = 0

// ownedByMe cannot check the owner here, so it refuses: the pusher runs on unix only.
func ownedByMe(os.FileInfo) bool { return false }
