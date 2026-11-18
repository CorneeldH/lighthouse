//go:build !unix

package scheduled

import "os"

// checkOwner accepts every directory: there is no owner to read from a file mode here.
func checkOwner(os.FileInfo, int) error { return nil }
