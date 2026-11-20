package maprefresh

import "github.com/CorneeldH/lighthouse/internal/scheduled/safefs"

// The file operations that never follow a symlink live in safefs; the job uses them under its
// own short names.
var (
	cause          = safefs.Cause
	fileErr        = safefs.FileErr
	stat           = safefs.Stat
	unlinkSymlinks = safefs.UnlinkSymlinks
	writeFresh     = safefs.WriteFresh
)
